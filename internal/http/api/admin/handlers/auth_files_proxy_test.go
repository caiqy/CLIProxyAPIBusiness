package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/datatypes"
)

func TestAuthFiles_Create_RejectsUnsupportedProxyScheme(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAuthFilesWhitelistDB(t)
	h := NewAuthFileHandler(db)
	router := gin.New()
	router.POST("/v0/admin/auth-files", h.Create)

	body := map[string]any{
		"key":       "auth-create-invalid-proxy",
		"proxy_url": "ftp://127.0.0.1:21",
		"content":   map[string]any{"type": "claude"},
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v0/admin/auth-files", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAuthFiles_Update_RejectsUnsupportedProxyScheme(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAuthFilesWhitelistDB(t)
	now := time.Now().UTC()
	row := models.Auth{
		Key:         "auth-update-invalid-proxy",
		Name:        "auth-update-invalid-proxy",
		ProxyURL:    "http://127.0.0.1:7000",
		Content:     datatypes.JSON([]byte(`{"type":"claude"}`)),
		IsAvailable: true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create auth row: %v", errCreate)
	}

	h := NewAuthFileHandler(db)
	router := gin.New()
	router.PUT("/v0/admin/auth-files/:id", h.Update)

	body := map[string]any{
		"proxy_url": "ftp://127.0.0.1:21",
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/v0/admin/auth-files/%d", row.ID), bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAuthFiles_Update_EmptyProxyURLClearsProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAuthFilesWhitelistDB(t)
	now := time.Now().UTC()
	row := models.Auth{
		Key:         "auth-update-clear-proxy",
		Name:        "auth-update-clear-proxy",
		ProxyURL:    "http://127.0.0.1:7000",
		Content:     datatypes.JSON([]byte(`{"type":"claude"}`)),
		IsAvailable: true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create auth row: %v", errCreate)
	}

	h := NewAuthFileHandler(db)
	router := gin.New()
	router.PUT("/v0/admin/auth-files/:id", h.Update)

	body := map[string]any{
		"proxy_url": "   ",
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/v0/admin/auth-files/%d", row.ID), bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var saved models.Auth
	if errFind := db.First(&saved, "id = ?", row.ID).Error; errFind != nil {
		t.Fatalf("query saved row failed: %v", errFind)
	}
	if saved.ProxyURL != "" {
		t.Fatalf("expected proxy_url to be cleared, got %q", saved.ProxyURL)
	}
}

func TestAuthFiles_Create_RejectsUnsupportedProxySchemeInContent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAuthFilesWhitelistDB(t)
	h := NewAuthFileHandler(db)
	router := gin.New()
	router.POST("/v0/admin/auth-files", h.Create)

	body := map[string]any{
		"key": "auth-create-invalid-content-proxy",
		"content": map[string]any{
			"type":      "claude",
			"proxy_url": "ftp://127.0.0.1:21",
		},
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v0/admin/auth-files", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAuthFiles_Update_RejectsUnsupportedProxySchemeInContent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAuthFilesWhitelistDB(t)
	now := time.Now().UTC()
	row := models.Auth{
		Key:         "auth-update-invalid-content-proxy",
		Name:        "auth-update-invalid-content-proxy",
		ProxyURL:    "http://127.0.0.1:7000",
		Content:     datatypes.JSON([]byte(`{"type":"claude","proxy_url":"http://127.0.0.1:7000"}`)),
		IsAvailable: true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create auth row: %v", errCreate)
	}

	h := NewAuthFileHandler(db)
	router := gin.New()
	router.PUT("/v0/admin/auth-files/:id", h.Update)

	body := map[string]any{
		"content": map[string]any{
			"type":      "claude",
			"proxy_url": "ftp://127.0.0.1:21",
		},
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/v0/admin/auth-files/%d", row.ID), bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAuthFiles_Update_EmptyProxyURLAlsoClearsContentProxyURL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAuthFilesWhitelistDB(t)
	now := time.Now().UTC()
	row := models.Auth{
		Key:         "auth-update-clear-content-proxy",
		Name:        "auth-update-clear-content-proxy",
		ProxyURL:    "http://127.0.0.1:7000",
		Content:     datatypes.JSON([]byte(`{"type":"claude","proxy_url":"http://127.0.0.1:7000"}`)),
		IsAvailable: true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create auth row: %v", errCreate)
	}

	h := NewAuthFileHandler(db)
	router := gin.New()
	router.PUT("/v0/admin/auth-files/:id", h.Update)

	body := map[string]any{"proxy_url": ""}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/v0/admin/auth-files/%d", row.ID), bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var saved models.Auth
	if errFind := db.First(&saved, "id = ?", row.ID).Error; errFind != nil {
		t.Fatalf("query saved row failed: %v", errFind)
	}
	if saved.ProxyURL != "" {
		t.Fatalf("expected proxy_url to be cleared, got %q", saved.ProxyURL)
	}
	var content map[string]any
	if errDecode := json.Unmarshal(saved.Content, &content); errDecode != nil {
		t.Fatalf("decode saved content failed: %v", errDecode)
	}
	if _, exists := content["proxy_url"]; exists {
		t.Fatalf("expected content.proxy_url removed when clearing proxy, got %v", content["proxy_url"])
	}
}

func TestAuthFiles_Update_EmptyProxyURLWithContentPayloadClearsContentProxyURL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAuthFilesWhitelistDB(t)
	now := time.Now().UTC()
	row := models.Auth{
		Key:         "auth-update-clear-content-proxy-with-payload",
		Name:        "auth-update-clear-content-proxy-with-payload",
		ProxyURL:    "http://127.0.0.1:7000",
		Content:     datatypes.JSON([]byte(`{"type":"claude","proxy_url":"http://127.0.0.1:7000","email":"demo@example.com"}`)),
		IsAvailable: true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create auth row: %v", errCreate)
	}

	h := NewAuthFileHandler(db)
	router := gin.New()
	router.PUT("/v0/admin/auth-files/:id", h.Update)

	body := map[string]any{
		"proxy_url": "",
		"content": map[string]any{
			"type":      "claude",
			"proxy_url": "http://127.0.0.1:7000",
			"email":     "demo@example.com",
		},
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/v0/admin/auth-files/%d", row.ID), bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var saved models.Auth
	if errFind := db.First(&saved, "id = ?", row.ID).Error; errFind != nil {
		t.Fatalf("query saved row failed: %v", errFind)
	}
	if saved.ProxyURL != "" {
		t.Fatalf("expected proxy_url to be cleared, got %q", saved.ProxyURL)
	}
	var content map[string]any
	if errDecode := json.Unmarshal(saved.Content, &content); errDecode != nil {
		t.Fatalf("decode saved content failed: %v", errDecode)
	}
	if _, exists := content["proxy_url"]; exists {
		t.Fatalf("expected content.proxy_url removed when clearing proxy, got %v", content["proxy_url"])
	}
}

func TestAuthFiles_Import_RejectsUnsupportedProxyScheme(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAuthFilesWhitelistDB(t)
	h := NewAuthFileHandler(db)
	router := gin.New()
	router.POST("/v0/admin/auth-files/import", h.Import)

	req := buildAuthFilesImportRequest(t, "/v0/admin/auth-files/import", map[string]string{
		"invalid.json": `{"id":"auth-import-invalid-proxy","type":"claude","proxy_url":"ftp://127.0.0.1:21"}`,
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := decodeImportAuthFilesResponse(t, w.Body.Bytes())
	if resp.Imported != 0 {
		t.Fatalf("expected imported=0, got %d", resp.Imported)
	}
	if len(resp.Failed) != 1 {
		t.Fatalf("expected 1 failed item, got %d", len(resp.Failed))
	}
	if !strings.Contains(resp.Failed[0].Error, "invalid proxy_url") {
		t.Fatalf("expected invalid proxy_url failure, got %q", resp.Failed[0].Error)
	}
}

func TestAuthFiles_Import_AcceptsSupportedProxyScheme(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAuthFilesWhitelistDB(t)
	h := NewAuthFileHandler(db)
	router := gin.New()
	router.POST("/v0/admin/auth-files/import", h.Import)

	req := buildAuthFilesImportRequest(t, "/v0/admin/auth-files/import", map[string]string{
		"valid.json": `{"id":"auth-import-valid-proxy","type":"claude","proxy_url":"http://127.0.0.1:7890"}`,
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := decodeImportAuthFilesResponse(t, w.Body.Bytes())
	if resp.Imported != 1 || len(resp.Failed) != 0 {
		t.Fatalf("unexpected import response: %+v", resp)
	}

	var saved models.Auth
	if errFind := db.Where("key = ?", "auth-import-valid-proxy").First(&saved).Error; errFind != nil {
		t.Fatalf("query saved row failed: %v", errFind)
	}
	if saved.ProxyURL != "http://127.0.0.1:7890/" {
		t.Fatalf("expected normalized proxy_url, got %q", saved.ProxyURL)
	}
}
