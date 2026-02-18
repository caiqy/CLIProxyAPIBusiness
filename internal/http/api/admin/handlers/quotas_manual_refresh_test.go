package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type manualRefreshTestRefresher struct {
	mu         sync.Mutex
	errByKey   map[string]error
	panicByKey map[string]bool
	calls      []string
}

func (r *manualRefreshTestRefresher) RefreshByAuthKey(_ context.Context, authKey string) error {
	r.mu.Lock()
	r.calls = append(r.calls, authKey)
	r.mu.Unlock()
	if r.panicByKey != nil && r.panicByKey[authKey] {
		panic("manual refresh test panic")
	}
	if r.errByKey == nil {
		return nil
	}
	if errCall, ok := r.errByKey[authKey]; ok {
		return errCall
	}
	return nil
}

func (r *manualRefreshTestRefresher) callsSnapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.calls))
	copy(out, r.calls)
	return out
}

type quotaManualRefreshCreateResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	Total  int    `json:"total"`
}

type quotaManualRefreshStatusResponse struct {
	TaskID       string     `json:"task_id"`
	Status       string     `json:"status"`
	Total        int        `json:"total"`
	Processed    int        `json:"processed"`
	SuccessCount int        `json:"success_count"`
	FailedCount  int        `json:"failed_count"`
	SkippedCount int        `json:"skipped_count"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
	LastError    string     `json:"last_error"`
	RecentErrors []string   `json:"recent_errors"`
}

func setupQuotaManualRefreshDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:quotamanualrefresh_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, errOpen := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if errOpen != nil {
		t.Fatalf("open db: %v", errOpen)
	}
	if errMigrate := db.AutoMigrate(&models.Auth{}, &models.Quota{}); errMigrate != nil {
		t.Fatalf("migrate db: %v", errMigrate)
	}
	return db
}

func seedQuotaManualRefreshRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	group1 := uint64(1)
	group2 := uint64(2)
	authRows := []models.Auth{
		{
			Key:         "auth-key-1",
			AuthGroupID: models.AuthGroupIDs{&group1},
			Content:     datatypes.JSON([]byte(`{"type":"antigravity"}`)),
		},
		{
			Key:         "auth-key-2",
			AuthGroupID: models.AuthGroupIDs{&group2},
			Content:     datatypes.JSON([]byte(`{"type":"codex"}`)),
		},
	}
	if errCreate := db.Create(&authRows).Error; errCreate != nil {
		t.Fatalf("create auth rows: %v", errCreate)
	}
	quotaRows := []models.Quota{
		{AuthID: authRows[0].ID, Type: "antigravity", Data: datatypes.JSON([]byte(`{"ok":true}`))},
		{AuthID: authRows[1].ID, Type: "codex", Data: datatypes.JSON([]byte(`{"ok":true}`))},
	}
	if errCreate := db.Create(&quotaRows).Error; errCreate != nil {
		t.Fatalf("create quota rows: %v", errCreate)
	}
}

func TestQuotaManualRefreshCreateThenGetSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupQuotaManualRefreshDB(t)
	seedQuotaManualRefreshRows(t, db)

	refresher := &manualRefreshTestRefresher{}
	store := NewQuotaManualRefreshTaskStore(time.Minute, 100)
	handler := NewQuotaHandler(db, refresher, store)

	router := gin.New()
	router.POST("/v0/admin/quotas/manual-refresh", handler.CreateManualRefresh)
	router.GET("/v0/admin/quotas/manual-refresh/:task_id", handler.GetManualRefresh)

	body := bytes.NewBufferString(`{"key":"auth-key-1","type":"antigravity"}`)
	postReq := httptest.NewRequest(http.MethodPost, "/v0/admin/quotas/manual-refresh", body)
	postReq.Header.Set("Content-Type", "application/json")
	postW := httptest.NewRecorder()
	router.ServeHTTP(postW, postReq)

	if postW.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", postW.Code)
	}

	var created quotaManualRefreshCreateResponse
	if errDecode := json.Unmarshal(postW.Body.Bytes(), &created); errDecode != nil {
		t.Fatalf("decode create response: %v", errDecode)
	}
	if created.TaskID == "" {
		t.Fatalf("expected non-empty task_id")
	}
	if created.Status != quotaManualRefreshTaskStatusRunning {
		t.Fatalf("expected running status, got %s", created.Status)
	}
	if created.Total != 1 {
		t.Fatalf("expected total=1, got %d", created.Total)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		getReq := httptest.NewRequest(http.MethodGet, "/v0/admin/quotas/manual-refresh/"+created.TaskID, nil)
		getW := httptest.NewRecorder()
		router.ServeHTTP(getW, getReq)
		if getW.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", getW.Code)
		}

		var status quotaManualRefreshStatusResponse
		if errDecode := json.Unmarshal(getW.Body.Bytes(), &status); errDecode != nil {
			t.Fatalf("decode status response: %v", errDecode)
		}

		if status.Status == quotaManualRefreshTaskStatusSuccess {
			if status.TaskID != created.TaskID {
				t.Fatalf("expected task id %s, got %s", created.TaskID, status.TaskID)
			}
			if status.Total != 1 || status.Processed != 1 || status.SuccessCount != 1 {
				t.Fatalf("unexpected counters: total=%d processed=%d success=%d", status.Total, status.Processed, status.SuccessCount)
			}
			if status.FailedCount != 0 || status.SkippedCount != 0 {
				t.Fatalf("unexpected failed/skipped count: failed=%d skipped=%d", status.FailedCount, status.SkippedCount)
			}
			if status.StartedAt.IsZero() {
				t.Fatalf("expected started_at")
			}
			if status.FinishedAt == nil {
				t.Fatalf("expected finished_at")
			}
			calls := refresher.callsSnapshot()
			if len(calls) != 1 || calls[0] != "auth-key-1" {
				t.Fatalf("unexpected refresher calls: %#v", calls)
			}
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting task success")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestQuotaManualRefreshGetNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupQuotaManualRefreshDB(t)
	handler := NewQuotaHandler(db, &manualRefreshTestRefresher{errByKey: map[string]error{"x": errors.New("x")}}, NewQuotaManualRefreshTaskStore(time.Minute, 10))

	router := gin.New()
	router.GET("/v0/admin/quotas/manual-refresh/:task_id", handler.GetManualRefresh)

	req := httptest.NewRequest(http.MethodGet, "/v0/admin/quotas/manual-refresh/missing-task", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", w.Code)
	}
}

func TestQuotaManualRefreshCreateReturns503WhenRefresherUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupQuotaManualRefreshDB(t)
	seedQuotaManualRefreshRows(t, db)

	handler := NewQuotaHandler(db, nil, NewQuotaManualRefreshTaskStore(time.Minute, 10))
	router := gin.New()
	router.POST("/v0/admin/quotas/manual-refresh", handler.CreateManualRefresh)

	req := httptest.NewRequest(http.MethodPost, "/v0/admin/quotas/manual-refresh", bytes.NewBufferString(`{"key":"auth-key-1"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", w.Code)
	}
}

func TestQuotaManualRefreshPartialFailureContinuesProcessing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupQuotaManualRefreshDB(t)
	seedQuotaManualRefreshRows(t, db)

	refresher := &manualRefreshTestRefresher{errByKey: map[string]error{"auth-key-1": errors.New("refresh failed")}}
	handler := NewQuotaHandler(db, refresher, NewQuotaManualRefreshTaskStore(time.Minute, 100))

	router := gin.New()
	router.POST("/v0/admin/quotas/manual-refresh", handler.CreateManualRefresh)
	router.GET("/v0/admin/quotas/manual-refresh/:task_id", handler.GetManualRefresh)

	postReq := httptest.NewRequest(http.MethodPost, "/v0/admin/quotas/manual-refresh", bytes.NewBufferString(`{}`))
	postReq.Header.Set("Content-Type", "application/json")
	postW := httptest.NewRecorder()
	router.ServeHTTP(postW, postReq)
	if postW.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", postW.Code)
	}

	var created quotaManualRefreshCreateResponse
	if errDecode := json.Unmarshal(postW.Body.Bytes(), &created); errDecode != nil {
		t.Fatalf("decode create response: %v", errDecode)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		getReq := httptest.NewRequest(http.MethodGet, "/v0/admin/quotas/manual-refresh/"+created.TaskID, nil)
		getW := httptest.NewRecorder()
		router.ServeHTTP(getW, getReq)
		if getW.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", getW.Code)
		}

		var status quotaManualRefreshStatusResponse
		if errDecode := json.Unmarshal(getW.Body.Bytes(), &status); errDecode != nil {
			t.Fatalf("decode status response: %v", errDecode)
		}

		if status.Status == quotaManualRefreshTaskStatusFailed {
			if status.Total != 2 || status.Processed != 2 {
				t.Fatalf("expected total=2 and processed=2, got total=%d processed=%d", status.Total, status.Processed)
			}
			if status.FailedCount != 1 || status.SuccessCount != 1 {
				t.Fatalf("expected failed=1 success=1, got failed=%d success=%d", status.FailedCount, status.SuccessCount)
			}
			calls := refresher.callsSnapshot()
			if len(calls) != 2 {
				t.Fatalf("expected both keys processed, calls=%#v", calls)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting task completion")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestQuotaManualRefreshPanicCountsAsFailed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupQuotaManualRefreshDB(t)
	seedQuotaManualRefreshRows(t, db)

	refresher := &manualRefreshTestRefresher{panicByKey: map[string]bool{"auth-key-1": true}}
	handler := NewQuotaHandler(db, refresher, NewQuotaManualRefreshTaskStore(time.Minute, 100))

	router := gin.New()
	router.POST("/v0/admin/quotas/manual-refresh", handler.CreateManualRefresh)
	router.GET("/v0/admin/quotas/manual-refresh/:task_id", handler.GetManualRefresh)

	postReq := httptest.NewRequest(http.MethodPost, "/v0/admin/quotas/manual-refresh", bytes.NewBufferString(`{"key":"auth-key-1"}`))
	postReq.Header.Set("Content-Type", "application/json")
	postW := httptest.NewRecorder()
	router.ServeHTTP(postW, postReq)
	if postW.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", postW.Code)
	}

	var created quotaManualRefreshCreateResponse
	if errDecode := json.Unmarshal(postW.Body.Bytes(), &created); errDecode != nil {
		t.Fatalf("decode create response: %v", errDecode)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		getReq := httptest.NewRequest(http.MethodGet, "/v0/admin/quotas/manual-refresh/"+created.TaskID, nil)
		getW := httptest.NewRecorder()
		router.ServeHTTP(getW, getReq)

		var status quotaManualRefreshStatusResponse
		if errDecode := json.Unmarshal(getW.Body.Bytes(), &status); errDecode != nil {
			t.Fatalf("decode status response: %v", errDecode)
		}
		if status.Status == quotaManualRefreshTaskStatusFailed {
			if status.FailedCount != 1 || status.Processed != 1 {
				t.Fatalf("expected failed=1 processed=1, got failed=%d processed=%d", status.FailedCount, status.Processed)
			}
			if status.LastError != "panic: manual refresh test panic" {
				t.Fatalf("expected concise panic error, got %q", status.LastError)
			}
			if len(status.RecentErrors) != 1 || status.RecentErrors[0] != "panic: manual refresh test panic" {
				t.Fatalf("expected concise recent error, got %#v", status.RecentErrors)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting task completion")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
