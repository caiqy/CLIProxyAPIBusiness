package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
