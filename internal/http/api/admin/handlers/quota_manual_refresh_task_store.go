package handlers

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	quotaManualRefreshTaskStatusRunning = "running"
	quotaManualRefreshTaskStatusSuccess = "success"
	quotaManualRefreshTaskStatusFailed  = "failed"

	quotaManualRefreshTaskRecentErrorsLimit = 20
)

type quotaManualRefreshTaskResultType string

const (
	quotaManualRefreshTaskResultTypeSuccess quotaManualRefreshTaskResultType = "success"
	quotaManualRefreshTaskResultTypeFailed  quotaManualRefreshTaskResultType = "failed"
	quotaManualRefreshTaskResultTypeSkipped quotaManualRefreshTaskResultType = "skipped"
)

type quotaManualRefreshFilter struct {
	Key         string
	Type        string
	AuthGroupID string
}

type quotaManualRefreshTask struct {
	TaskID         string
	CreatedBy      string
	Filter         quotaManualRefreshFilter
	Total          int
	Status         string
	CreatedAt      time.Time
	FinishedAt     *time.Time
	ProcessedCount int
	SuccessCount   int
	FailedCount    int
	SkippedCount   int
	LastError      string
	RecentErrors   []string
}

type quotaManualRefreshTaskStore struct {
	mu              sync.Mutex
	tasks           map[string]*quotaManualRefreshTask
	order           []string
	ttl             time.Duration
	maxTasks        int
	maxRecentErrors int
	nextID          uint64
	now             func() time.Time
}

func NewQuotaManualRefreshTaskStore(ttl time.Duration, maxTasks int) *quotaManualRefreshTaskStore {
	return &quotaManualRefreshTaskStore{
		tasks:           make(map[string]*quotaManualRefreshTask),
		order:           make([]string, 0),
		ttl:             ttl,
		maxTasks:        maxTasks,
		maxRecentErrors: quotaManualRefreshTaskRecentErrorsLimit,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (s *quotaManualRefreshTaskStore) Create(createdBy string, filter quotaManualRefreshFilter, total int) quotaManualRefreshTask {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	taskID := s.newTaskIDLocked(now)
	task := &quotaManualRefreshTask{
		TaskID:     taskID,
		CreatedBy:  strings.TrimSpace(createdBy),
		Filter:     filter,
		Total:      total,
		Status:     quotaManualRefreshTaskStatusRunning,
		CreatedAt:  now,
		FinishedAt: nil,
	}
	s.tasks[taskID] = task
	s.order = append(s.order, taskID)
	s.cleanupExpiredLocked(now)
	s.enforceMaxTasksLocked()

	return cloneQuotaManualRefreshTask(task)
}

func (s *quotaManualRefreshTaskStore) Get(taskID string) (quotaManualRefreshTask, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupExpiredLocked(s.now())
	s.enforceMaxTasksLocked()

	task, ok := s.tasks[taskID]
	if !ok {
		return quotaManualRefreshTask{}, false
	}
	return cloneQuotaManualRefreshTask(task), true
}

func (s *quotaManualRefreshTaskStore) RecordResult(taskID string, resultType quotaManualRefreshTaskResultType, recentError string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return false
	}
	if task.FinishedAt != nil {
		return false
	}
	if !isValidQuotaManualRefreshResultType(resultType) {
		return false
	}
	task.ProcessedCount++
	switch resultType {
	case quotaManualRefreshTaskResultTypeSuccess:
		task.SuccessCount++
	case quotaManualRefreshTaskResultTypeFailed:
		task.FailedCount++
	case quotaManualRefreshTaskResultTypeSkipped:
		task.SkippedCount++
	}
	if errMsg := strings.TrimSpace(recentError); errMsg != "" {
		task.LastError = errMsg
		task.RecentErrors = append(task.RecentErrors, errMsg)
		if len(task.RecentErrors) > s.maxRecentErrors {
			start := len(task.RecentErrors) - s.maxRecentErrors
			trimmed := make([]string, s.maxRecentErrors)
			copy(trimmed, task.RecentErrors[start:])
			task.RecentErrors = trimmed
		}
	}

	return true
}

func (s *quotaManualRefreshTaskStore) Finish(taskID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return false
	}
	finishedAt := s.now()
	task.FinishedAt = &finishedAt
	if task.FailedCount > 0 {
		task.Status = quotaManualRefreshTaskStatusFailed
	} else {
		task.Status = quotaManualRefreshTaskStatusSuccess
	}
	s.cleanupExpiredLocked(finishedAt)
	s.enforceMaxTasksLocked()

	return true
}

func (s *quotaManualRefreshTaskStore) CleanupExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupExpiredLocked(s.now())
	s.enforceMaxTasksLocked()
}

func (s *quotaManualRefreshTaskStore) cleanupExpiredLocked(now time.Time) {
	if s.ttl <= 0 || len(s.order) == 0 {
		return
	}

	kept := make([]string, 0, len(s.order))
	for _, taskID := range s.order {
		task, ok := s.tasks[taskID]
		if !ok {
			continue
		}
		if task.FinishedAt != nil && now.Sub(*task.FinishedAt) >= s.ttl {
			delete(s.tasks, taskID)
			continue
		}
		kept = append(kept, taskID)
	}
	s.order = kept
}

func (s *quotaManualRefreshTaskStore) enforceMaxTasksLocked() {
	if s.maxTasks <= 0 {
		return
	}
	for len(s.tasks) > s.maxTasks {
		index := s.oldestFinishedIndexLocked()
		if index < 0 {
			// Keep running tasks even when over maxTasks.
			return
		}
		taskID := s.order[index]
		delete(s.tasks, taskID)
		s.order = append(s.order[:index], s.order[index+1:]...)
	}
}

func (s *quotaManualRefreshTaskStore) oldestFinishedIndexLocked() int {
	for i, taskID := range s.order {
		task, ok := s.tasks[taskID]
		if ok && task.FinishedAt != nil {
			return i
		}
	}
	return -1
}

func (s *quotaManualRefreshTaskStore) newTaskIDLocked(now time.Time) string {
	s.nextID++
	return fmt.Sprintf("quota-manual-refresh-%d-%d", now.UnixNano(), s.nextID)
}

func isValidQuotaManualRefreshResultType(resultType quotaManualRefreshTaskResultType) bool {
	switch resultType {
	case quotaManualRefreshTaskResultTypeSuccess, quotaManualRefreshTaskResultTypeFailed, quotaManualRefreshTaskResultTypeSkipped:
		return true
	default:
		return false
	}
}

func cloneQuotaManualRefreshTask(src *quotaManualRefreshTask) quotaManualRefreshTask {
	if src == nil {
		return quotaManualRefreshTask{}
	}
	cloned := *src
	if src.FinishedAt != nil {
		finishedAt := *src.FinishedAt
		cloned.FinishedAt = &finishedAt
	}
	if len(src.RecentErrors) > 0 {
		cloned.RecentErrors = make([]string, len(src.RecentErrors))
		copy(cloned.RecentErrors, src.RecentErrors)
	}
	return cloned
}
