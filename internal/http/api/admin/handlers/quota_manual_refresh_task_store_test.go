package handlers

import (
	"fmt"
	"testing"
	"time"
)

func TestQuotaManualRefreshTaskStoreCreateRunning(t *testing.T) {
	store := NewQuotaManualRefreshTaskStore(time.Minute, 10)
	filter := quotaManualRefreshFilter{Key: "k1", Type: "antigravity", AuthGroupID: "7"}

	task := store.Create("admin-1", filter, 42)

	if task.Status != quotaManualRefreshTaskStatusRunning {
		t.Fatalf("expected running status, got %s", task.Status)
	}
	if task.FinishedAt != nil {
		t.Fatalf("expected finished_at nil on create")
	}
	if task.Total != 42 {
		t.Fatalf("expected total 42, got %d", task.Total)
	}
	if task.CreatedBy != "admin-1" {
		t.Fatalf("expected created by admin-1, got %s", task.CreatedBy)
	}
	if task.Filter != filter {
		t.Fatalf("expected filter copied")
	}
	if task.TaskID == "" {
		t.Fatalf("expected generated task id")
	}
}

func TestQuotaManualRefreshTaskStoreRecordResultAccumulatesCounts(t *testing.T) {
	store := NewQuotaManualRefreshTaskStore(time.Minute, 10)
	task := store.Create("admin-1", quotaManualRefreshFilter{}, 100)

	if ok := store.RecordResult(task.TaskID, quotaManualRefreshTaskResultTypeSuccess, ""); !ok {
		t.Fatalf("expected record result success")
	}
	if ok := store.RecordResult(task.TaskID, quotaManualRefreshTaskResultTypeFailed, "first error"); !ok {
		t.Fatalf("expected record result success")
	}
	if ok := store.RecordResult(task.TaskID, quotaManualRefreshTaskResultTypeSkipped, ""); !ok {
		t.Fatalf("expected record result success")
	}

	task, ok := store.Get(task.TaskID)
	if !ok {
		t.Fatalf("expected task exists")
	}
	if task.ProcessedCount != 3 || task.SuccessCount != 1 || task.FailedCount != 1 || task.SkippedCount != 1 {
		t.Fatalf("unexpected counts: processed=%d success=%d failed=%d skipped=%d", task.ProcessedCount, task.SuccessCount, task.FailedCount, task.SkippedCount)
	}
	if task.LastError != "first error" {
		t.Fatalf("expected last error saved, got %s", task.LastError)
	}
}

func TestQuotaManualRefreshTaskStoreFinishSetsFinishedAtAndStatus(t *testing.T) {
	store := NewQuotaManualRefreshTaskStore(time.Minute, 10)

	successTask := store.Create("admin-1", quotaManualRefreshFilter{}, 1)
	if ok := store.Finish(successTask.TaskID); !ok {
		t.Fatalf("expected finish success")
	}
	successTask, ok := store.Get(successTask.TaskID)
	if !ok {
		t.Fatalf("expected success task exists")
	}
	if successTask.Status != quotaManualRefreshTaskStatusSuccess {
		t.Fatalf("expected success status, got %s", successTask.Status)
	}
	if successTask.FinishedAt == nil {
		t.Fatalf("expected finished_at set")
	}

	failedTask := store.Create("admin-2", quotaManualRefreshFilter{}, 1)
	store.RecordResult(failedTask.TaskID, quotaManualRefreshTaskResultTypeFailed, "bad row")
	if ok := store.Finish(failedTask.TaskID); !ok {
		t.Fatalf("expected finish failed task success")
	}
	failedTask, ok = store.Get(failedTask.TaskID)
	if !ok {
		t.Fatalf("expected failed task exists")
	}
	if failedTask.Status != quotaManualRefreshTaskStatusFailed {
		t.Fatalf("expected failed status, got %s", failedTask.Status)
	}
	if failedTask.FinishedAt == nil {
		t.Fatalf("expected finished_at set")
	}
}

func TestQuotaManualRefreshTaskStoreGetReturnsSnapshot(t *testing.T) {
	store := NewQuotaManualRefreshTaskStore(time.Minute, 10)
	filter := quotaManualRefreshFilter{Key: "k1", Type: "t1", AuthGroupID: "g1"}

	created := store.Create("admin-1", filter, 9)
	store.RecordResult(created.TaskID, quotaManualRefreshTaskResultTypeFailed, "error-1")
	task, ok := store.Get(created.TaskID)
	if !ok {
		t.Fatalf("expected task exists")
	}

	task.Status = quotaManualRefreshTaskStatusSuccess
	task.RecentErrors[0] = "mutated"
	task.Filter.Key = "mutated"

	latest, ok := store.Get(created.TaskID)
	if !ok {
		t.Fatalf("expected task exists")
	}
	if latest.Status != quotaManualRefreshTaskStatusRunning {
		t.Fatalf("expected stored task status unchanged, got %s", latest.Status)
	}
	if latest.RecentErrors[0] != "error-1" {
		t.Fatalf("expected recent errors unchanged, got %s", latest.RecentErrors[0])
	}
	if latest.Filter.Key != "k1" {
		t.Fatalf("expected filter snapshot unchanged, got %s", latest.Filter.Key)
	}
}

func TestQuotaManualRefreshTaskStoreRecentErrorsLimit(t *testing.T) {
	store := NewQuotaManualRefreshTaskStore(time.Minute, 10)
	task := store.Create("admin-1", quotaManualRefreshFilter{}, 100)

	for i := 1; i <= 25; i++ {
		errMsg := fmt.Sprintf("err-%d", i)
		store.RecordResult(task.TaskID, quotaManualRefreshTaskResultTypeFailed, errMsg)
	}

	task, ok := store.Get(task.TaskID)
	if !ok {
		t.Fatalf("expected task exists")
	}
	if len(task.RecentErrors) != 20 {
		t.Fatalf("expected 20 recent errors, got %d", len(task.RecentErrors))
	}
	if task.RecentErrors[0] != "err-6" || task.RecentErrors[len(task.RecentErrors)-1] != "err-25" {
		t.Fatalf("unexpected recent errors window: first=%s last=%s", task.RecentErrors[0], task.RecentErrors[len(task.RecentErrors)-1])
	}
}

func TestQuotaManualRefreshTaskStoreCleanupExpired(t *testing.T) {
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	store := NewQuotaManualRefreshTaskStore(30*time.Second, 10)
	store.now = func() time.Time { return base }

	task := store.Create("admin-1", quotaManualRefreshFilter{}, 10)
	store.Finish(task.TaskID)

	store.now = func() time.Time { return base.Add(31 * time.Second) }
	store.CleanupExpired()

	if _, ok := store.Get(task.TaskID); ok {
		t.Fatalf("expected task cleaned up by ttl")
	}
}

func TestQuotaManualRefreshTaskStoreGetCleansUpExpiredTasks(t *testing.T) {
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	store := NewQuotaManualRefreshTaskStore(30*time.Second, 10)
	store.now = func() time.Time { return base }

	task := store.Create("admin-1", quotaManualRefreshFilter{}, 10)
	store.Finish(task.TaskID)

	store.now = func() time.Time { return base.Add(31 * time.Second) }
	if _, ok := store.Get(task.TaskID); ok {
		t.Fatalf("expected expired task to be invisible via Get")
	}
}

func TestQuotaManualRefreshTaskStoreMaxTasksEvictionPrefersFinished(t *testing.T) {
	store := NewQuotaManualRefreshTaskStore(time.Hour, 2)

	t1 := store.Create("admin-1", quotaManualRefreshFilter{}, 1)
	store.Finish(t1.TaskID)
	t2 := store.Create("admin-1", quotaManualRefreshFilter{}, 1)
	t3 := store.Create("admin-1", quotaManualRefreshFilter{}, 1)

	if _, ok := store.Get(t1.TaskID); ok {
		t.Fatalf("expected oldest finished task evicted first")
	}
	if _, ok := store.Get(t2.TaskID); !ok {
		t.Fatalf("expected t2 remains")
	}
	if _, ok := store.Get(t3.TaskID); !ok {
		t.Fatalf("expected t3 remains")
	}
}

func TestQuotaManualRefreshTaskStoreMaxTasksKeepsAllRunningTasks(t *testing.T) {
	store := NewQuotaManualRefreshTaskStore(time.Hour, 2)

	t1 := store.Create("admin-1", quotaManualRefreshFilter{}, 1)
	t2 := store.Create("admin-1", quotaManualRefreshFilter{}, 1)
	t3 := store.Create("admin-1", quotaManualRefreshFilter{}, 1)

	if _, ok := store.Get(t1.TaskID); !ok {
		t.Fatalf("expected t1 remains when all running")
	}
	if _, ok := store.Get(t2.TaskID); !ok {
		t.Fatalf("expected t2 remains")
	}
	if _, ok := store.Get(t3.TaskID); !ok {
		t.Fatalf("expected t3 remains")
	}
}

func TestQuotaManualRefreshTaskStoreRecordResultRejectsUnknownType(t *testing.T) {
	store := NewQuotaManualRefreshTaskStore(time.Minute, 10)
	task := store.Create("admin-1", quotaManualRefreshFilter{}, 100)

	if ok := store.RecordResult(task.TaskID, quotaManualRefreshTaskResultType("unknown"), "boom"); ok {
		t.Fatalf("expected unknown result type rejected")
	}
	latest, ok := store.Get(task.TaskID)
	if !ok {
		t.Fatalf("expected task exists")
	}
	if latest.ProcessedCount != 0 || latest.LastError != "" {
		t.Fatalf("expected no mutation for unknown type")
	}
}

func TestQuotaManualRefreshTaskStoreRecordResultDoesNotMutateFinishedTask(t *testing.T) {
	store := NewQuotaManualRefreshTaskStore(time.Minute, 10)
	task := store.Create("admin-1", quotaManualRefreshFilter{}, 100)

	if ok := store.RecordResult(task.TaskID, quotaManualRefreshTaskResultTypeSuccess, ""); !ok {
		t.Fatalf("expected initial record result success")
	}
	if ok := store.Finish(task.TaskID); !ok {
		t.Fatalf("expected finish success")
	}
	before, ok := store.Get(task.TaskID)
	if !ok {
		t.Fatalf("expected task exists")
	}

	if ok := store.RecordResult(task.TaskID, quotaManualRefreshTaskResultTypeFailed, "late error"); ok {
		t.Fatalf("expected record result false for finished task")
	}
	after, ok := store.Get(task.TaskID)
	if !ok {
		t.Fatalf("expected task exists")
	}

	if after.ProcessedCount != before.ProcessedCount || after.SuccessCount != before.SuccessCount || after.FailedCount != before.FailedCount || after.SkippedCount != before.SkippedCount {
		t.Fatalf("expected counters unchanged after finished task record")
	}
	if after.LastError != before.LastError {
		t.Fatalf("expected last error unchanged after finished task record")
	}
	if len(after.RecentErrors) != len(before.RecentErrors) {
		t.Fatalf("expected recent errors unchanged after finished task record")
	}
}
