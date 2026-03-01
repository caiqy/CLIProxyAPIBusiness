package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func setupProviderAPIKeyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:provider_api_key_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, errOpen := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if errOpen != nil {
		t.Fatalf("open db: %v", errOpen)
	}
	if errMigrate := db.AutoMigrate(&models.ProviderAPIKey{}, &models.ModelMapping{}); errMigrate != nil {
		t.Fatalf("migrate db: %v", errMigrate)
	}
	return db
}

func TestProviderAPIKeys_NormalizeAndValidateVertex(t *testing.T) {
	t.Parallel()

	if got := normalizeProvider("vertex"); got != "vertex" {
		t.Fatalf("normalizeProvider(vertex) = %q, want %q", got, "vertex")
	}
	if got := normalizeProvider("vertex-api-key"); got != "vertex" {
		t.Fatalf("normalizeProvider(vertex-api-key) = %q, want %q", got, "vertex")
	}

	row := &models.ProviderAPIKey{
		Provider: "vertex",
		Name:     "vertex-main",
		BaseURL:  "https://vertex.example.com",
	}
	err := validateProviderRow(row)
	if err == nil {
		t.Fatalf("expected validation error for missing api_key")
	}
	if !strings.Contains(err.Error(), "api_key is required") {
		t.Fatalf("expected api_key required error, got %v", err)
	}
}

func TestProviderAPIKeys_NormalizeFields_Vertex(t *testing.T) {
	t.Parallel()

	row := &models.ProviderAPIKey{
		Provider: "vertex",
		ExcludedModels: datatypes.JSON(`[
			"gemini-2.5-pro"
		]`),
		APIKeyEntries: datatypes.JSON(`[
			{"api_key":"k1"}
		]`),
	}

	normalizeProviderFields(row)

	if row.ExcludedModels != nil {
		t.Fatalf("expected excluded_models to be cleared for vertex provider")
	}
	if row.APIKeyEntries != nil {
		t.Fatalf("expected api_key_entries to be cleared for vertex provider")
	}
}

func TestProviderAPIKeys_SyncSDKConfig_Vertex(t *testing.T) {
	t.Parallel()

	db := setupProviderAPIKeyTestDB(t)
	now := time.Now().UTC()
	row := models.ProviderAPIKey{
		Provider:  "vertex",
		Name:      "vertex-main",
		APIKey:    "vk-test-key",
		BaseURL:   "https://vertex.example.com/api",
		ProxyURL:  "http://localhost:8080",
		Priority:  3,
		IsEnabled: true,
		Models:    datatypes.JSON(`[{"name":"gemini-2.5-flash","alias":"vertex-flash"}]`),
		Headers:   datatypes.JSON(`{"X-Test":"1"}`),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create provider row: %v", errCreate)
	}

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if errWrite := os.WriteFile(configPath, []byte("api-keys: []\n"), 0o644); errWrite != nil {
		t.Fatalf("write config file: %v", errWrite)
	}

	h := NewProviderAPIKeyHandler(db, configPath)
	if errSync := h.syncSDKConfig(context.Background()); errSync != nil {
		t.Fatalf("sync config failed: %v", errSync)
	}

	cfg, errLoad := sdkconfig.LoadConfig(configPath)
	if errLoad != nil {
		t.Fatalf("load config failed: %v", errLoad)
	}
	if len(cfg.VertexCompatAPIKey) != 1 {
		t.Fatalf("expected 1 vertex-api-key entry, got %d", len(cfg.VertexCompatAPIKey))
	}
	entry := cfg.VertexCompatAPIKey[0]
	if entry.APIKey != "vk-test-key" {
		t.Fatalf("unexpected api_key: %q", entry.APIKey)
	}
	if entry.BaseURL != "https://vertex.example.com/api" {
		t.Fatalf("unexpected base_url: %q", entry.BaseURL)
	}
	if entry.ProxyURL != "http://localhost:8080" {
		t.Fatalf("unexpected proxy_url: %q", entry.ProxyURL)
	}
	if entry.Priority != 3 {
		t.Fatalf("unexpected priority: %d", entry.Priority)
	}
	if len(entry.Models) != 1 || entry.Models[0].Alias != "vertex-flash" {
		t.Fatalf("unexpected models: %+v", entry.Models)
	}
}

func TestProviderAPIKeys_BuildExcludedFromWhitelist(t *testing.T) {
	t.Parallel()

	universe := []string{"claude-sonnet-4-6", "claude-opus-4-1", "claude-3-7-sonnet"}

	t.Run("allowlist subset", func(t *testing.T) {
		excluded, err := buildExcludedFromWhitelist(universe, []string{"claude-sonnet-4-6"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := []string{"claude-3-7-sonnet", "claude-opus-4-1"}
		if !reflect.DeepEqual(excluded, want) {
			t.Fatalf("excluded = %v, want %v", excluded, want)
		}
	})

	t.Run("empty allowlist means block all", func(t *testing.T) {
		excluded, err := buildExcludedFromWhitelist(universe, nil)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := []string{"claude-3-7-sonnet", "claude-opus-4-1", "claude-sonnet-4-6"}
		if !reflect.DeepEqual(excluded, want) {
			t.Fatalf("excluded = %v, want %v", excluded, want)
		}
	})

	t.Run("unknown allowlist model", func(t *testing.T) {
		_, err := buildExcludedFromWhitelist(universe, []string{"not-exists"})
		if err == nil || !strings.Contains(err.Error(), "unknown model") {
			t.Fatalf("want unknown model error, got %v", err)
		}
	})
}

func TestProviderAPIKeys_Create_WithWhitelistEnabled_AutoGeneratesExcluded(t *testing.T) {
	gin.SetMode(gin.TestMode)

	universe := []string{"claude-sonnet-4-6", "claude-opus-4-1", "claude-3-7-sonnet"}
	stubProviderUniverseLoader(t, map[string][]string{providerClaude: universe})
	allowed := universe[0]
	expectedExcluded, errExpected := buildExcludedFromWhitelist(universe, []string{allowed})
	if errExpected != nil {
		t.Fatalf("build expected excluded: %v", errExpected)
	}

	db := setupProviderAPIKeyTestDB(t)
	h := NewProviderAPIKeyHandler(db, "")
	router := gin.New()
	router.POST("/v0/admin/provider-api-keys", h.Create)

	body := map[string]any{
		"provider":          "claude",
		"name":              "claude-main",
		"api_key":           "sk-test",
		"whitelist_enabled": true,
		"models": []map[string]string{
			{"name": allowed, "alias": ""},
		},
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v0/admin/provider-api-keys", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if errDecode := json.Unmarshal(w.Body.Bytes(), &resp); errDecode != nil {
		t.Fatalf("decode response failed: %v", errDecode)
	}
	if enabled, ok := resp["whitelist_enabled"].(bool); !ok || !enabled {
		t.Fatalf("expected response whitelist_enabled=true, got %v", resp["whitelist_enabled"])
	}

	var row models.ProviderAPIKey
	if errFind := db.Order("id DESC").First(&row).Error; errFind != nil {
		t.Fatalf("query saved row failed: %v", errFind)
	}
	gotExcluded := decodeExcludedModels(row.ExcludedModels)
	if !reflect.DeepEqual(gotExcluded, expectedExcluded) {
		t.Fatalf("excluded_models = %v, want %v", gotExcluded, expectedExcluded)
	}
}

func TestProviderAPIKeys_Create_WithWhitelistEnabled_UnsupportedProvider_ReturnsBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupProviderAPIKeyTestDB(t)
	h := NewProviderAPIKeyHandler(db, "")
	router := gin.New()
	router.POST("/v0/admin/provider-api-keys", h.Create)

	body := map[string]any{
		"provider":          "vertex",
		"name":              "vertex-whitelist",
		"api_key":           "vk-test",
		"base_url":          "https://vertex.example.com",
		"whitelist_enabled": true,
		"models": []map[string]string{
			{"name": "gemini-2.5-flash", "alias": ""},
		},
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v0/admin/provider-api-keys", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(strings.ToLower(w.Body.String()), "whitelist") {
		t.Fatalf("expected whitelist unsupported error body, got %s", w.Body.String())
	}
}

func TestProviderAPIKeys_Create_WithWhitelistEnabled_EmptyModels_BlocksAll(t *testing.T) {
	gin.SetMode(gin.TestMode)
	universe := []string{"claude-sonnet-4-6", "claude-opus-4-1", "claude-3-7-sonnet"}
	stubProviderUniverseLoader(t, map[string][]string{providerClaude: universe})

	db := setupProviderAPIKeyTestDB(t)
	h := NewProviderAPIKeyHandler(db, "")
	router := gin.New()
	router.POST("/v0/admin/provider-api-keys", h.Create)

	body := map[string]any{
		"provider":          "claude",
		"name":              "claude-empty-whitelist",
		"api_key":           "sk-test",
		"whitelist_enabled": true,
		"models":            []map[string]string{},
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v0/admin/provider-api-keys", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d body=%s", w.Code, w.Body.String())
	}

	var row models.ProviderAPIKey
	if errFind := db.Order("id DESC").First(&row).Error; errFind != nil {
		t.Fatalf("query saved row failed: %v", errFind)
	}
	gotExcluded := decodeExcludedModels(row.ExcludedModels)
	if !reflect.DeepEqual(gotExcluded, normalizeModelNames(universe)) {
		t.Fatalf("excluded_models = %v, want universe %v", gotExcluded, normalizeModelNames(universe))
	}
}

func TestProviderAPIKeys_Create_WithWhitelistEnabled_UnknownModel_ReturnsBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stubProviderUniverseLoader(t, map[string][]string{providerClaude: []string{"claude-sonnet-4-6", "claude-opus-4-1"}})

	db := setupProviderAPIKeyTestDB(t)
	h := NewProviderAPIKeyHandler(db, "")
	router := gin.New()
	router.POST("/v0/admin/provider-api-keys", h.Create)

	body := map[string]any{
		"provider":          "claude",
		"name":              "claude-invalid-whitelist",
		"api_key":           "sk-test",
		"whitelist_enabled": true,
		"models": []map[string]string{
			{"name": "not-exists", "alias": ""},
		},
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v0/admin/provider-api-keys", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(strings.ToLower(w.Body.String()), "unknown model") {
		t.Fatalf("expected unknown model error body, got %s", w.Body.String())
	}
}

func TestProviderAPIKeys_Update_WithWhitelistEnabled_RecomputesExcluded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	universe := []string{"claude-sonnet-4-6", "claude-opus-4-1", "claude-3-7-sonnet"}
	stubProviderUniverseLoader(t, map[string][]string{providerClaude: universe})

	db := setupProviderAPIKeyTestDB(t)
	now := time.Now().UTC()
	row := models.ProviderAPIKey{
		Provider:       providerClaude,
		Name:           "claude-updatable",
		APIKey:         "sk-test",
		Models:         datatypes.JSON(`[]`),
		ExcludedModels: datatypes.JSON(`[]`),
		IsEnabled:      true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create row: %v", errCreate)
	}

	h := NewProviderAPIKeyHandler(db, "")
	router := gin.New()
	router.PUT("/v0/admin/provider-api-keys/:id", h.Update)

	body := map[string]any{
		"whitelist_enabled": true,
		"models": []map[string]string{
			{"name": "claude-sonnet-4-6", "alias": ""},
		},
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/v0/admin/provider-api-keys/%d", row.ID), bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var saved models.ProviderAPIKey
	if errFind := db.First(&saved, "id = ?", row.ID).Error; errFind != nil {
		t.Fatalf("query saved row failed: %v", errFind)
	}

	gotExcluded := decodeExcludedModels(saved.ExcludedModels)
	wantExcluded, errWant := buildExcludedFromWhitelist(universe, []string{"claude-sonnet-4-6"})
	if errWant != nil {
		t.Fatalf("build wanted excluded failed: %v", errWant)
	}
	if !reflect.DeepEqual(gotExcluded, wantExcluded) {
		t.Fatalf("excluded_models = %v, want %v", gotExcluded, wantExcluded)
	}
}

func TestProviderAPIKeys_Update_WithWhitelistEnabled_UnknownModel_ReturnsBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stubProviderUniverseLoader(t, map[string][]string{providerClaude: []string{"claude-sonnet-4-6", "claude-opus-4-1"}})

	db := setupProviderAPIKeyTestDB(t)
	now := time.Now().UTC()
	row := models.ProviderAPIKey{
		Provider:  providerClaude,
		Name:      "claude-updatable-invalid",
		APIKey:    "sk-test",
		IsEnabled: true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create row: %v", errCreate)
	}

	h := NewProviderAPIKeyHandler(db, "")
	router := gin.New()
	router.PUT("/v0/admin/provider-api-keys/:id", h.Update)

	body := map[string]any{
		"whitelist_enabled": true,
		"models": []map[string]string{
			{"name": "not-exists", "alias": ""},
		},
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/v0/admin/provider-api-keys/%d", row.ID), bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(strings.ToLower(w.Body.String()), "unknown model") {
		t.Fatalf("expected unknown model error body, got %s", w.Body.String())
	}
}

func TestProviderAPIKeys_Update_SwitchUnsupportedProvider_DisablesWhitelist(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupProviderAPIKeyTestDB(t)
	now := time.Now().UTC()
	row := models.ProviderAPIKey{
		Provider:         providerClaude,
		Name:             "switch-provider",
		APIKey:           "sk-test",
		WhitelistEnabled: true,
		Models:           datatypes.JSON(`[{"name":"claude-sonnet-4-6","alias":""}]`),
		ExcludedModels:   datatypes.JSON(`[{"invalid":true}]`),
		IsEnabled:        true,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create row: %v", errCreate)
	}

	h := NewProviderAPIKeyHandler(db, "")
	router := gin.New()
	router.PUT("/v0/admin/provider-api-keys/:id", h.Update)

	body := map[string]any{
		"provider":          "vertex",
		"base_url":          "https://vertex.example.com",
		"api_key":           "vk-test",
		"whitelist_enabled": false,
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/v0/admin/provider-api-keys/%d", row.ID), bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var saved models.ProviderAPIKey
	if errFind := db.First(&saved, "id = ?", row.ID).Error; errFind != nil {
		t.Fatalf("query saved row failed: %v", errFind)
	}
	if saved.Provider != providerVertex {
		t.Fatalf("provider = %s, want %s", saved.Provider, providerVertex)
	}
	if saved.WhitelistEnabled {
		t.Fatalf("expected whitelist disabled after switching to unsupported provider")
	}
}

func TestProviderAPIKeys_FormatProviderRow_IncludesWhitelistEnabled(t *testing.T) {
	row := &models.ProviderAPIKey{WhitelistEnabled: true}
	formatted := formatProviderRow(row)
	if enabled, ok := formatted["whitelist_enabled"].(bool); !ok || !enabled {
		t.Fatalf("expected whitelist_enabled=true in formatted row, got %v", formatted["whitelist_enabled"])
	}
}

func TestLoadProviderUniverse_UsesStaticUniverse(t *testing.T) {
	models := loadProviderUniverse("antigravity")
	if len(models) == 0 {
		t.Fatal("expected non-empty static universe for antigravity")
	}
	found := false
	for _, model := range models {
		if model == "gemini-2.5-flash" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected static model gemini-2.5-flash, got %v", models)
	}
}

func TestLoadProviderUniverse_OpenAIAlias(t *testing.T) {
	got := loadProviderUniverse("openai-compatibility")
	if len(got) == 0 {
		t.Fatal("expected non-empty static universe for openai alias")
	}
}

func TestBuildExcludedFromCreateWhitelist_StaticUniverseAvailable(t *testing.T) {
	models := []modelAlias{{Name: "claude-sonnet-4-6"}}
	excluded, err := buildExcludedFromCreateWhitelist("claude", models)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(excluded) == 0 {
		t.Fatal("expected excluded models computed from static universe")
	}
}

func stubProviderUniverseLoader(t *testing.T, universeByProvider map[string][]string) {
	t.Helper()
	prev := providerUniverseLoader
	providerUniverseLoader = func(provider string) []string {
		provider = normalizeProvider(provider)
		models := universeByProvider[provider]
		if len(models) == 0 {
			return nil
		}
		return append([]string(nil), models...)
	}
	t.Cleanup(func() {
		providerUniverseLoader = prev
	})
}
