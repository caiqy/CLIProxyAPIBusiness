package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func setupAuthFilesWhitelistDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:auth_files_whitelist_%d?mode=memory&cache=shared", time.Now().UnixNano())
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

func decodeModelNamesJSON(raw datatypes.JSON) []string {
	if len(raw) == 0 {
		return nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	return values
}

func buildAuthFilesImportRequest(t *testing.T, route string, files map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, content := range files {
		part, errCreate := writer.CreateFormFile("files", name)
		if errCreate != nil {
			t.Fatalf("create form file: %v", errCreate)
		}
		if _, errWrite := part.Write([]byte(content)); errWrite != nil {
			t.Fatalf("write form file: %v", errWrite)
		}
	}
	if errClose := writer.Close(); errClose != nil {
		t.Fatalf("close multipart writer: %v", errClose)
	}
	req := httptest.NewRequest(http.MethodPost, route, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func decodeImportAuthFilesResponse(t *testing.T, body []byte) importAuthFilesResponse {
	t.Helper()
	var resp importAuthFilesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode import response failed: %v body=%s", err, string(body))
	}
	return resp
}

func TestAuthFiles_Create_WhitelistEmptyMeansBlockAll(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stubProviderUniverseLoader(t, map[string][]string{providerClaude: []string{"claude-sonnet-4-6", "claude-opus-4-1"}})

	db := setupAuthFilesWhitelistDB(t)
	h := NewAuthFileHandler(db)
	router := gin.New()
	router.POST("/v0/admin/auth-files", h.Create)

	body := map[string]any{
		"key":               "auth-whitelist-empty",
		"content":           map[string]any{"type": "claude"},
		"whitelist_enabled": true,
		"allowed_models":    []string{},
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v0/admin/auth-files", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d body=%s", w.Code, w.Body.String())
	}

	var row models.Auth
	if errFind := db.Where("key = ?", "auth-whitelist-empty").First(&row).Error; errFind != nil {
		t.Fatalf("query auth row failed: %v", errFind)
	}
	if !row.WhitelistEnabled {
		t.Fatalf("expected whitelist_enabled=true in db")
	}
	gotExcluded := decodeModelNamesJSON(row.ExcludedModels)
	wantExcluded := []string{"claude-opus-4-1", "claude-sonnet-4-6"}
	if !reflect.DeepEqual(gotExcluded, wantExcluded) {
		t.Fatalf("excluded_models=%v, want %v", gotExcluded, wantExcluded)
	}
}

func TestAuthFiles_Update_WhitelistUnknownModelReturnsBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stubProviderUniverseLoader(t, map[string][]string{providerClaude: []string{"claude-sonnet-4-6", "claude-opus-4-1"}})

	db := setupAuthFilesWhitelistDB(t)
	now := time.Now().UTC()
	row := models.Auth{
		Key:         "auth-whitelist-update",
		Name:        "auth-whitelist-update",
		Content:     datatypes.JSON([]byte(`{"type":"claude"}`)),
		IsAvailable: true,
		RateLimit:   0,
		Priority:    0,
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
		"whitelist_enabled": true,
		"allowed_models":    []string{"not-exists"},
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/v0/admin/auth-files/%d", row.ID), bytes.NewReader(data))
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

func TestAuthFiles_Get_ReturnsWhitelistFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAuthFilesWhitelistDB(t)
	now := time.Now().UTC()
	row := models.Auth{
		Key:              "auth-whitelist-get",
		Name:             "auth-whitelist-get",
		Content:          datatypes.JSON([]byte(`{"type":"claude"}`)),
		WhitelistEnabled: true,
		AllowedModels:    datatypes.JSON([]byte(` ["claude-sonnet-4-6"] `)),
		ExcludedModels:   datatypes.JSON([]byte(` ["claude-opus-4-1"] `)),
		IsAvailable:      true,
		RateLimit:        0,
		Priority:         0,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create auth row: %v", errCreate)
	}

	h := NewAuthFileHandler(db)
	router := gin.New()
	router.GET("/v0/admin/auth-files/:id", h.Get)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v0/admin/auth-files/%d", row.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if errDecode := json.Unmarshal(w.Body.Bytes(), &resp); errDecode != nil {
		t.Fatalf("decode response failed: %v", errDecode)
	}
	if enabled, ok := resp["whitelist_enabled"].(bool); !ok || !enabled {
		t.Fatalf("expected whitelist_enabled=true, got %v", resp["whitelist_enabled"])
	}
	if _, ok := resp["allowed_models"]; !ok {
		t.Fatalf("missing allowed_models in response")
	}
	if _, ok := resp["excluded_models"]; !ok {
		t.Fatalf("missing excluded_models in response")
	}
}

func TestAuthFiles_Create_WhitelistDisabledClearsAllowedAndExcluded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stubProviderUniverseLoader(t, map[string][]string{providerClaude: []string{"claude-sonnet-4-6", "claude-opus-4-1"}})

	db := setupAuthFilesWhitelistDB(t)
	h := NewAuthFileHandler(db)
	router := gin.New()
	router.POST("/v0/admin/auth-files", h.Create)

	body := map[string]any{
		"key":               "auth-whitelist-disabled",
		"content":           map[string]any{"type": "claude"},
		"whitelist_enabled": false,
		"allowed_models":    []string{"claude-sonnet-4-6"},
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v0/admin/auth-files", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d body=%s", w.Code, w.Body.String())
	}

	var row models.Auth
	if errFind := db.Where("key = ?", "auth-whitelist-disabled").First(&row).Error; errFind != nil {
		t.Fatalf("query auth row failed: %v", errFind)
	}
	if row.WhitelistEnabled {
		t.Fatalf("expected whitelist_enabled=false in db")
	}
	if gotAllowed := decodeModelNamesJSON(row.AllowedModels); len(gotAllowed) != 0 {
		t.Fatalf("expected allowed_models empty when whitelist disabled, got %v", gotAllowed)
	}
	if gotExcluded := decodeModelNamesJSON(row.ExcludedModels); len(gotExcluded) != 0 {
		t.Fatalf("expected excluded_models empty when whitelist disabled, got %v", gotExcluded)
	}
}

func TestAuthFiles_Update_DisableWhitelistClearsAllowedAndExcluded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stubProviderUniverseLoader(t, map[string][]string{providerClaude: []string{"claude-sonnet-4-6", "claude-opus-4-1"}})

	db := setupAuthFilesWhitelistDB(t)
	now := time.Now().UTC()
	row := models.Auth{
		Key:              "auth-whitelist-disable-update",
		Name:             "auth-whitelist-disable-update",
		Content:          datatypes.JSON([]byte(`{"type":"claude"}`)),
		WhitelistEnabled: true,
		AllowedModels:    datatypes.JSON([]byte(`["claude-sonnet-4-6"]`)),
		ExcludedModels:   datatypes.JSON([]byte(`["claude-opus-4-1"]`)),
		IsAvailable:      true,
		RateLimit:        0,
		Priority:         0,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create auth row: %v", errCreate)
	}

	h := NewAuthFileHandler(db)
	router := gin.New()
	router.PUT("/v0/admin/auth-files/:id", h.Update)

	body := map[string]any{
		"whitelist_enabled": false,
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
	if saved.WhitelistEnabled {
		t.Fatalf("expected whitelist_enabled=false after disabling")
	}
	if gotAllowed := decodeModelNamesJSON(saved.AllowedModels); len(gotAllowed) != 0 {
		t.Fatalf("expected allowed_models empty after disabling, got %v", gotAllowed)
	}
	if gotExcluded := decodeModelNamesJSON(saved.ExcludedModels); len(gotExcluded) != 0 {
		t.Fatalf("expected excluded_models empty after disabling, got %v", gotExcluded)
	}
}

func TestAuthFiles_ModelPresets_ByType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stubProviderUniverseLoader(t, map[string][]string{providerClaude: []string{"claude-sonnet-4-6", "claude-opus-4-1"}})

	db := setupAuthFilesWhitelistDB(t)
	h := NewAuthFileHandler(db)
	router := gin.New()
	router.GET("/v0/admin/auth-files/model-presets", h.ListModelPresets)

	req := httptest.NewRequest(http.MethodGet, "/v0/admin/auth-files/model-presets?type=anthropic", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Provider   string   `json:"provider"`
		Supported  bool     `json:"supported"`
		Reason     string   `json:"reason"`
		ReasonCode string   `json:"reason_code"`
		Models     []string `json:"models"`
	}
	if errDecode := json.Unmarshal(w.Body.Bytes(), &resp); errDecode != nil {
		t.Fatalf("decode response failed: %v", errDecode)
	}
	if resp.Provider != providerClaude {
		t.Fatalf("provider=%q, want %q", resp.Provider, providerClaude)
	}
	if !resp.Supported {
		t.Fatalf("expected supported=true, reason=%q", resp.Reason)
	}
	if resp.ReasonCode != "" {
		t.Fatalf("reason_code=%q, want empty", resp.ReasonCode)
	}
	want := []string{"claude-opus-4-1", "claude-sonnet-4-6"}
	if !reflect.DeepEqual(resp.Models, want) {
		t.Fatalf("models=%v, want %v", resp.Models, want)
	}
}

func TestAuthFiles_ModelPresets_UnsupportedType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAuthFilesWhitelistDB(t)
	h := NewAuthFileHandler(db)
	router := gin.New()
	router.GET("/v0/admin/auth-files/model-presets", h.ListModelPresets)

	req := httptest.NewRequest(http.MethodGet, "/v0/admin/auth-files/model-presets?type=unknown", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Supported  bool     `json:"supported"`
		Reason     string   `json:"reason"`
		ReasonCode string   `json:"reason_code"`
		Models     []string `json:"models"`
	}
	if errDecode := json.Unmarshal(w.Body.Bytes(), &resp); errDecode != nil {
		t.Fatalf("decode response failed: %v", errDecode)
	}
	if resp.Supported {
		t.Fatalf("expected supported=false")
	}
	if strings.TrimSpace(resp.Reason) == "" {
		t.Fatalf("expected non-empty reason")
	}
	if resp.ReasonCode != "unsupported_auth_type" {
		t.Fatalf("reason_code=%q, want %q", resp.ReasonCode, "unsupported_auth_type")
	}
	if len(resp.Models) != 0 {
		t.Fatalf("expected empty models for unsupported type, got %v", resp.Models)
	}
}

func TestSupportsAuthFileWhitelistProvider_ProviderMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider string
		want     bool
	}{
		{name: "gemini", provider: providerGemini, want: true},
		{name: "codex", provider: providerCodex, want: true},
		{name: "claude", provider: providerClaude, want: true},
		{name: "qwen", provider: "qwen", want: true},
		{name: "kiro", provider: "kiro", want: true},
		{name: "unknown", provider: "unknown", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := supportsAuthFileWhitelistProvider(tc.provider); got != tc.want {
				t.Fatalf("supportsAuthFileWhitelistProvider(%q)=%v, want %v", tc.provider, got, tc.want)
			}
		})
	}
}

func TestResolveAuthFileProviderFromContent_FallbackToProviderField(t *testing.T) {
	provider, err := resolveAuthFileProviderFromContent(map[string]any{"provider": "anthropic"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if provider != providerClaude {
		t.Fatalf("provider=%q, want %q", provider, providerClaude)
	}
}

func TestAuthFiles_ImportConflict_WhitelistRecomputedWhenIntersectionExists(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stubProviderUniverseLoader(t, map[string][]string{providerClaude: []string{"claude-opus-4-1", "claude-sonnet-4-6"}})

	db := setupAuthFilesWhitelistDB(t)
	now := time.Now().UTC()
	row := models.Auth{
		Key:              "import-whitelist-intersection",
		Name:             "import-whitelist-intersection",
		Content:          datatypes.JSON([]byte(`{"type":"claude","access_token":"old"}`)),
		WhitelistEnabled: true,
		AllowedModels:    datatypes.JSON([]byte(` ["claude-opus-4-1","old-only-model"] `)),
		ExcludedModels:   datatypes.JSON([]byte(` ["claude-sonnet-4-6"] `)),
		IsAvailable:      true,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create auth row: %v", errCreate)
	}

	h := NewAuthFileHandler(db)
	router := gin.New()
	router.POST("/v0/admin/auth-files/import", h.Import)

	req := buildAuthFilesImportRequest(t, "/v0/admin/auth-files/import", map[string]string{
		"conflict.json": `{"id":"import-whitelist-intersection","type":"claude","access_token":"new"}`,
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var saved models.Auth
	if errFind := db.Where("key = ?", "import-whitelist-intersection").First(&saved).Error; errFind != nil {
		t.Fatalf("query saved row failed: %v", errFind)
	}
	if !saved.WhitelistEnabled {
		t.Fatalf("expected whitelist_enabled=true after conflict import")
	}
	if got := decodeModelNamesJSON(saved.AllowedModels); !reflect.DeepEqual(got, []string{"claude-opus-4-1"}) {
		t.Fatalf("allowed_models=%v, want [claude-opus-4-1]", got)
	}
	if got := decodeModelNamesJSON(saved.ExcludedModels); !reflect.DeepEqual(got, []string{"claude-sonnet-4-6"}) {
		t.Fatalf("excluded_models=%v, want [claude-sonnet-4-6]", got)
	}
}

func TestAuthFiles_ImportConflict_WhitelistKeepsBlockAllWhenNoIntersection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stubProviderUniverseLoader(t, map[string][]string{providerClaude: []string{"claude-opus-4-1", "claude-sonnet-4-6"}})

	db := setupAuthFilesWhitelistDB(t)
	now := time.Now().UTC()
	row := models.Auth{
		Key:              "import-whitelist-empty-intersection",
		Name:             "import-whitelist-empty-intersection",
		Content:          datatypes.JSON([]byte(`{"type":"claude","access_token":"old"}`)),
		WhitelistEnabled: true,
		AllowedModels:    datatypes.JSON([]byte(` ["legacy-only"] `)),
		ExcludedModels:   datatypes.JSON([]byte(` ["claude-opus-4-1","claude-sonnet-4-6"] `)),
		IsAvailable:      true,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create auth row: %v", errCreate)
	}

	h := NewAuthFileHandler(db)
	router := gin.New()
	router.POST("/v0/admin/auth-files/import", h.Import)

	req := buildAuthFilesImportRequest(t, "/v0/admin/auth-files/import", map[string]string{
		"conflict.json": `{"id":"import-whitelist-empty-intersection","type":"claude","access_token":"new"}`,
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var saved models.Auth
	if errFind := db.Where("key = ?", "import-whitelist-empty-intersection").First(&saved).Error; errFind != nil {
		t.Fatalf("query saved row failed: %v", errFind)
	}
	if !saved.WhitelistEnabled {
		t.Fatalf("expected whitelist_enabled=true after empty intersection")
	}
	if got := decodeModelNamesJSON(saved.AllowedModels); len(got) != 0 {
		t.Fatalf("expected allowed_models empty when preserving block-all semantics, got %v", got)
	}
	if got := decodeModelNamesJSON(saved.ExcludedModels); !reflect.DeepEqual(got, []string{"claude-opus-4-1", "claude-sonnet-4-6"}) {
		t.Fatalf("excluded_models=%v, want [claude-opus-4-1 claude-sonnet-4-6]", got)
	}
}

func TestAuthFiles_ImportConflict_WhitelistKeepsBlockAllWhenOldAllowlistEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stubProviderUniverseLoader(t, map[string][]string{providerClaude: []string{"claude-opus-4-1", "claude-sonnet-4-6"}})

	db := setupAuthFilesWhitelistDB(t)
	now := time.Now().UTC()
	row := models.Auth{
		Key:              "import-whitelist-block-all",
		Name:             "import-whitelist-block-all",
		Content:          datatypes.JSON([]byte(`{"type":"claude","access_token":"old"}`)),
		WhitelistEnabled: true,
		AllowedModels:    datatypes.JSON([]byte(`[]`)),
		ExcludedModels:   datatypes.JSON([]byte(` ["claude-opus-4-1","claude-sonnet-4-6"] `)),
		IsAvailable:      true,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create auth row: %v", errCreate)
	}

	h := NewAuthFileHandler(db)
	router := gin.New()
	router.POST("/v0/admin/auth-files/import", h.Import)

	req := buildAuthFilesImportRequest(t, "/v0/admin/auth-files/import", map[string]string{
		"conflict.json": `{"id":"import-whitelist-block-all","type":"claude","access_token":"new"}`,
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var saved models.Auth
	if errFind := db.Where("key = ?", "import-whitelist-block-all").First(&saved).Error; errFind != nil {
		t.Fatalf("query saved row failed: %v", errFind)
	}
	if !saved.WhitelistEnabled {
		t.Fatalf("expected whitelist_enabled=true when old allowlist is empty")
	}
	if got := decodeModelNamesJSON(saved.AllowedModels); len(got) != 0 {
		t.Fatalf("expected allowed_models empty when preserving block-all semantics, got %v", got)
	}
	if got := decodeModelNamesJSON(saved.ExcludedModels); !reflect.DeepEqual(got, []string{"claude-opus-4-1", "claude-sonnet-4-6"}) {
		t.Fatalf("excluded_models=%v, want [claude-opus-4-1 claude-sonnet-4-6]", got)
	}
}

func TestAuthFiles_ImportConflict_FailsWhenUniverseUnavailableAndKeepsOriginalRecord(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stubProviderUniverseLoader(t, map[string][]string{})

	db := setupAuthFilesWhitelistDB(t)
	now := time.Now().UTC()
	row := models.Auth{
		Key:              "import-whitelist-no-universe",
		Name:             "import-whitelist-no-universe",
		Content:          datatypes.JSON([]byte(`{"type":"claude","access_token":"old"}`)),
		WhitelistEnabled: true,
		AllowedModels:    datatypes.JSON([]byte(` ["claude-opus-4-1"] `)),
		ExcludedModels:   datatypes.JSON([]byte(` ["claude-sonnet-4-6"] `)),
		IsAvailable:      true,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create auth row: %v", errCreate)
	}

	h := NewAuthFileHandler(db)
	router := gin.New()
	router.POST("/v0/admin/auth-files/import", h.Import)

	req := buildAuthFilesImportRequest(t, "/v0/admin/auth-files/import", map[string]string{
		"conflict.json": `{"id":"import-whitelist-no-universe","type":"claude","access_token":"new"}`,
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := decodeImportAuthFilesResponse(t, w.Body.Bytes())
	if resp.Imported != 0 {
		t.Fatalf("imported=%d, want 0", resp.Imported)
	}
	if len(resp.Failed) != 1 {
		t.Fatalf("failed count=%d, want 1", len(resp.Failed))
	}
	if resp.Failed[0].File != "conflict.json" {
		t.Fatalf("failed[0].file=%q, want conflict.json", resp.Failed[0].File)
	}

	var saved models.Auth
	if errFind := db.Where("key = ?", "import-whitelist-no-universe").First(&saved).Error; errFind != nil {
		t.Fatalf("query saved row failed: %v", errFind)
	}
	if !saved.WhitelistEnabled {
		t.Fatalf("expected whitelist_enabled=true to preserve existing policy")
	}
	if got := decodeModelNamesJSON(saved.AllowedModels); !reflect.DeepEqual(got, []string{"claude-opus-4-1"}) {
		t.Fatalf("allowed_models=%v, want [claude-opus-4-1]", got)
	}
	if got := decodeModelNamesJSON(saved.ExcludedModels); !reflect.DeepEqual(got, []string{"claude-sonnet-4-6"}) {
		t.Fatalf("excluded_models=%v, want [claude-sonnet-4-6]", got)
	}

	var content map[string]any
	if errDecode := json.Unmarshal(saved.Content, &content); errDecode != nil {
		t.Fatalf("decode content failed: %v", errDecode)
	}
	if accessToken, _ := content["access_token"].(string); accessToken != "old" {
		t.Fatalf("access_token=%q, want old", accessToken)
	}
}

func TestAuthFiles_ImportConflict_FailsWhenProviderCannotBeResolvedAndKeepsOriginalRecord(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stubProviderUniverseLoader(t, map[string][]string{providerClaude: []string{"claude-opus-4-1", "claude-sonnet-4-6"}})

	db := setupAuthFilesWhitelistDB(t)
	now := time.Now().UTC()
	row := models.Auth{
		Key:              "import-whitelist-unresolvable-provider",
		Name:             "import-whitelist-unresolvable-provider",
		Content:          datatypes.JSON([]byte(`{"type":"claude","access_token":"old"}`)),
		WhitelistEnabled: true,
		AllowedModels:    datatypes.JSON([]byte(` ["claude-opus-4-1"] `)),
		ExcludedModels:   datatypes.JSON([]byte(` ["claude-sonnet-4-6"] `)),
		IsAvailable:      true,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create auth row: %v", errCreate)
	}

	h := NewAuthFileHandler(db)
	router := gin.New()
	router.POST("/v0/admin/auth-files/import", h.Import)

	req := buildAuthFilesImportRequest(t, "/v0/admin/auth-files/import", map[string]string{
		"conflict.json": `{"id":"import-whitelist-unresolvable-provider","access_token":"new"}`,
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := decodeImportAuthFilesResponse(t, w.Body.Bytes())
	if resp.Imported != 0 {
		t.Fatalf("imported=%d, want 0", resp.Imported)
	}
	if len(resp.Failed) != 1 {
		t.Fatalf("failed count=%d, want 1", len(resp.Failed))
	}
	if resp.Failed[0].File != "conflict.json" {
		t.Fatalf("failed[0].file=%q, want conflict.json", resp.Failed[0].File)
	}

	var saved models.Auth
	if errFind := db.Where("key = ?", "import-whitelist-unresolvable-provider").First(&saved).Error; errFind != nil {
		t.Fatalf("query saved row failed: %v", errFind)
	}
	if !saved.WhitelistEnabled {
		t.Fatalf("expected whitelist_enabled=true to preserve existing policy")
	}
	if got := decodeModelNamesJSON(saved.AllowedModels); !reflect.DeepEqual(got, []string{"claude-opus-4-1"}) {
		t.Fatalf("allowed_models=%v, want [claude-opus-4-1]", got)
	}
	if got := decodeModelNamesJSON(saved.ExcludedModels); !reflect.DeepEqual(got, []string{"claude-sonnet-4-6"}) {
		t.Fatalf("excluded_models=%v, want [claude-sonnet-4-6]", got)
	}

	var content map[string]any
	if errDecode := json.Unmarshal(saved.Content, &content); errDecode != nil {
		t.Fatalf("decode content failed: %v", errDecode)
	}
	if accessToken, _ := content["access_token"].(string); accessToken != "old" {
		t.Fatalf("access_token=%q, want old", accessToken)
	}
}
