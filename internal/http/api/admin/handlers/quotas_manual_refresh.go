package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	dbutil "github.com/router-for-me/CLIProxyAPIBusiness/internal/db"
	internalsettings "github.com/router-for-me/CLIProxyAPIBusiness/internal/settings"
	log "github.com/sirupsen/logrus"
)

const quotaManualRefreshMaxConcurrency = 5

type quotaManualRefresher interface {
	RefreshByAuthKey(ctx context.Context, authKey string) error
}

type quotaManualRefreshCreateRequest struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	AuthGroupID string `json:"auth_group_id"`
}

func (h *QuotaHandler) CreateManualRefresh(c *gin.Context) {
	if h == nil || h.db == nil || h.taskStore == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "quota handler unavailable"})
		return
	}
	if h.manualRefresher == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "quota refresher unavailable"})
		return
	}

	var req quotaManualRefreshCreateRequest
	if errBind := c.ShouldBindJSON(&req); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	keys, filter, errFilter := h.listManualRefreshAuthKeys(c.Request.Context(), req)
	if errFilter != nil {
		if errors.Is(errFilter, errInvalidAuthGroupID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": errFilter.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": errFilter.Error()})
		return
	}

	createdBy := ""
	if rawAdmin, ok := c.Get("adminUsername"); ok {
		if username, okCast := rawAdmin.(string); okCast {
			createdBy = username
		}
	}
	task := h.taskStore.Create(createdBy, filter, len(keys))

	go h.runManualRefreshTask(task.TaskID, keys)

	c.JSON(http.StatusOK, gin.H{
		"task_id": task.TaskID,
		"status":  task.Status,
		"total":   task.Total,
	})
}

func (h *QuotaHandler) GetManualRefresh(c *gin.Context) {
	if h == nil || h.taskStore == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "quota handler unavailable"})
		return
	}
	taskID := strings.TrimSpace(c.Param("task_id"))
	if taskID == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}

	task, ok := h.taskStore.Get(taskID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"task_id":       task.TaskID,
		"status":        task.Status,
		"total":         task.Total,
		"processed":     task.ProcessedCount,
		"success_count": task.SuccessCount,
		"failed_count":  task.FailedCount,
		"skipped_count": task.SkippedCount,
		"started_at":    task.CreatedAt,
		"finished_at":   task.FinishedAt,
		"last_error":    task.LastError,
		"recent_errors": task.RecentErrors,
	})
}

func (h *QuotaHandler) listManualRefreshAuthKeys(ctx context.Context, req quotaManualRefreshCreateRequest) ([]string, quotaManualRefreshFilter, error) {
	key := strings.TrimSpace(req.Key)
	typ := strings.TrimSpace(req.Type)
	group := strings.TrimSpace(req.AuthGroupID)
	var groupID uint64
	if group != "" {
		parsed, errParse := strconv.ParseUint(group, 10, 64)
		if errParse != nil || parsed == 0 {
			return nil, quotaManualRefreshFilter{}, errInvalidAuthGroupID
		}
		groupID = parsed
	}

	query := h.db.WithContext(ctx).
		Table("quota").
		Joins("JOIN auths ON auths.id = quota.auth_id").
		Where("auths.is_available")
	if key != "" {
		pattern := dbutil.NormalizeLikePattern(h.db, "%"+key+"%")
		query = query.Where(dbutil.CaseInsensitiveLikeExpr(h.db, "auths.key"), pattern)
	}
	if typ != "" {
		query = query.Where("quota.type = ?", typ)
	}
	if groupID > 0 {
		query = query.Where(dbutil.JSONArrayContainsExpr(h.db, "auths.auth_group_id"), dbutil.JSONArrayContainsValue(h.db, groupID))
	}

	var keys []string
	if errPluck := query.Distinct("auths.key").Order("auths.key ASC").Pluck("auths.key", &keys).Error; errPluck != nil {
		return nil, quotaManualRefreshFilter{}, fmt.Errorf("list quota auths failed: %w", errPluck)
	}
	cleaned := make([]string, 0, len(keys))
	for _, keyValue := range keys {
		keyValue = strings.TrimSpace(keyValue)
		if keyValue != "" {
			cleaned = append(cleaned, keyValue)
		}
	}

	return cleaned, quotaManualRefreshFilter{Key: key, Type: typ, AuthGroupID: group}, nil
}

func (h *QuotaHandler) runManualRefreshTask(taskID string, authKeys []string) {
	if h == nil || h.taskStore == nil {
		return
	}
	if h.manualRefresher == nil {
		h.taskStore.Finish(taskID)
		return
	}
	concurrency := resolveQuotaManualRefreshConcurrency()
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, authKey := range authKeys {
		authKey := strings.TrimSpace(authKey)
		if authKey == "" {
			h.taskStore.RecordResult(taskID, quotaManualRefreshTaskResultTypeSkipped, "")
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(key string) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if recovered := recover(); recovered != nil {
					stack := strings.TrimSpace(string(debug.Stack()))
					log.Errorf("quota manual refresh panic (task=%s auth_key=%s): %v\n%s", taskID, key, recovered, stack)
					h.taskStore.RecordResult(taskID, quotaManualRefreshTaskResultTypeFailed, fmt.Sprintf("panic: %v", recovered))
				}
			}()

			errRefresh := h.manualRefresher.RefreshByAuthKey(context.Background(), key)
			if errRefresh != nil {
				h.taskStore.RecordResult(taskID, quotaManualRefreshTaskResultTypeFailed, errRefresh.Error())
				return
			}
			h.taskStore.RecordResult(taskID, quotaManualRefreshTaskResultTypeSuccess, "")
		}(authKey)
	}

	wg.Wait()
	h.taskStore.Finish(taskID)
}

func resolveQuotaManualRefreshConcurrency() int {
	maxConcurrency := internalsettings.DefaultQuotaPollMaxConcurrency
	if raw, ok := internalsettings.DBConfigValue(internalsettings.QuotaPollMaxConcurrencyKey); ok {
		if parsed, okParse := parseQuotaManualRefreshInt(raw); okParse && parsed > 0 {
			maxConcurrency = parsed
		}
	}
	if maxConcurrency > quotaManualRefreshMaxConcurrency {
		maxConcurrency = quotaManualRefreshMaxConcurrency
	}
	if maxConcurrency <= 0 {
		return 1
	}
	return maxConcurrency
}

func parseQuotaManualRefreshInt(raw json.RawMessage) (int, bool) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var n int
	if errUnmarshal := json.Unmarshal(raw, &n); errUnmarshal == nil {
		return n, true
	}
	var s string
	if errUnmarshal := json.Unmarshal(raw, &s); errUnmarshal == nil {
		parsed, errParse := strconv.Atoi(strings.TrimSpace(s))
		if errParse == nil {
			return parsed, true
		}
	}
	var wrapper struct {
		Value json.RawMessage `json:"value"`
	}
	if errUnmarshal := json.Unmarshal(raw, &wrapper); errUnmarshal == nil {
		return parseQuotaManualRefreshInt(wrapper.Value)
	}
	return 0, false
}

var errInvalidAuthGroupID = errors.New("invalid auth_group_id")
