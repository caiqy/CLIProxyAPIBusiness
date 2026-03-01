package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/gorm"
)

func setupAuthFileProviderImportDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:auth_provider_import_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, errOpen := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if errOpen != nil {
		t.Fatalf("open db: %v", errOpen)
	}
	if errMigrate := db.AutoMigrate(&models.AuthGroup{}, &models.Auth{}); errMigrate != nil {
		t.Fatalf("migrate db: %v", errMigrate)
	}
	defaultGroup := models.AuthGroup{Name: fmt.Sprintf("default-group-%d", time.Now().UnixNano()), IsDefault: true}
	if errCreate := db.Create(&defaultGroup).Error; errCreate != nil {
		t.Fatalf("create default auth group: %v", errCreate)
	}
	return db
}

func TestImportByProvider_RejectsInvalidPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAuthFileProviderImportDB(t)
	handler := NewAuthFileHandler(db)

	router := gin.New()
	router.POST("/v0/admin/auth-files/import-by-provider", handler.ImportByProvider)

	req := httptest.NewRequest(http.MethodPost, "/v0/admin/auth-files/import-by-provider", bytes.NewBufferString(`{"provider":"","entries":[]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestImportByProvider_AllowsPartialSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAuthFileProviderImportDB(t)
	handler := NewAuthFileHandler(db)

	router := gin.New()
	router.POST("/v0/admin/auth-files/import-by-provider", handler.ImportByProvider)

	body := map[string]any{
		"provider": "kiro",
		"source":   "text",
		"entries": []map[string]any{
			{
				"access_token":  "acc-1",
				"refresh_token": "ref-1",
				"email":         "Demo@Example.com",
			},
			{
				"extra": "bad-without-required-token",
			},
		},
	}
	raw, errMarshal := json.Marshal(body)
	if errMarshal != nil {
		t.Fatalf("marshal request: %v", errMarshal)
	}

	req := httptest.NewRequest(http.MethodPost, "/v0/admin/auth-files/import-by-provider", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Imported int `json:"imported"`
		Failed   []struct {
			Index int    `json:"index"`
			Error string `json:"error"`
		} `json:"failed"`
	}
	if errDecode := json.Unmarshal(w.Body.Bytes(), &resp); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if resp.Imported != 1 {
		t.Fatalf("expected imported=1, got %d", resp.Imported)
	}
	if len(resp.Failed) != 1 {
		t.Fatalf("expected 1 failed item, got %d", len(resp.Failed))
	}
	if resp.Failed[0].Index != 2 {
		t.Fatalf("expected failed index=2, got %d", resp.Failed[0].Index)
	}

	var rows []models.Auth
	if errFind := db.Find(&rows).Error; errFind != nil {
		t.Fatalf("query auth rows: %v", errFind)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 auth row, got %d", len(rows))
	}
	if rows[0].Key != "kiro-demo@example.com" {
		t.Fatalf("expected auto-generated key kiro-demo@example.com, got %q", rows[0].Key)
	}
}

func TestImportByProvider_UsesFallbackKeyWhenEmailMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAuthFileProviderImportDB(t)
	handler := NewAuthFileHandler(db)

	router := gin.New()
	router.POST("/v0/admin/auth-files/import-by-provider", handler.ImportByProvider)

	body := map[string]any{
		"provider": "qwen",
		"source":   "text",
		"entries": []map[string]any{
			{
				"access_token": "qwen-token-a",
			},
		},
	}
	raw, errMarshal := json.Marshal(body)
	if errMarshal != nil {
		t.Fatalf("marshal request: %v", errMarshal)
	}

	req := httptest.NewRequest(http.MethodPost, "/v0/admin/auth-files/import-by-provider", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var rows []models.Auth
	if errFind := db.Find(&rows).Error; errFind != nil {
		t.Fatalf("query auth rows: %v", errFind)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 auth row, got %d", len(rows))
	}
	if len(rows[0].Key) == 0 || rows[0].Key[:7] != "qwen-h-" {
		t.Fatalf("expected fallback key prefix qwen-h-, got %q", rows[0].Key)
	}
}

func TestImportByProvider_GitHubCopilot_SucceedsWithAccessTokenOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAuthFileProviderImportDB(t)
	handler := NewAuthFileHandler(db)

	router := gin.New()
	router.POST("/v0/admin/auth-files/import-by-provider", handler.ImportByProvider)

	body := map[string]any{
		"provider": "github-copilot",
		"source":   "text",
		"entries": []map[string]any{
			{
				"access_token": "gh-token-a",
				"email":        "copilot@example.com",
			},
		},
	}
	raw, errMarshal := json.Marshal(body)
	if errMarshal != nil {
		t.Fatalf("marshal request: %v", errMarshal)
	}

	req := httptest.NewRequest(http.MethodPost, "/v0/admin/auth-files/import-by-provider", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var rows []models.Auth
	if errFind := db.Find(&rows).Error; errFind != nil {
		t.Fatalf("query auth rows: %v", errFind)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 auth row, got %d", len(rows))
	}
	if rows[0].Key != "github-copilot-copilot@example.com" {
		t.Fatalf("expected auto-generated key github-copilot-copilot@example.com, got %q", rows[0].Key)
	}
}
