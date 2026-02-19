package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestListProviderCatalog_ReturnsStableOrder(t *testing.T) {
	t.Parallel()

	got := listProviderCatalog()
	want := []string{
		"gemini",
		"vertex",
		"gemini-cli",
		"aistudio",
		"antigravity",
		"claude",
		"codex",
		"qwen",
		"iflow",
		"kimi",
		"github-copilot",
		"kiro",
		"kilo",
		"openai-compatibility",
	}

	if len(got) != len(want) {
		t.Fatalf("expected %d providers, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("provider order mismatch at %d: expected %q got %q", i, want[i], got[i].ID)
		}
	}
}

func TestListProviderCatalog_IncludesKiroAndKilo(t *testing.T) {
	t.Parallel()

	got := listProviderCatalog()
	seen := make(map[string]bool, len(got))
	for _, item := range got {
		seen[item.ID] = true
	}
	if !seen["kiro"] {
		t.Fatalf("expected provider catalog to include kiro")
	}
	if !seen["kilo"] {
		t.Fatalf("expected provider catalog to include kilo")
	}
}

func TestListProviderCatalog_NoDuplicates(t *testing.T) {
	t.Parallel()

	got := listProviderCatalog()
	seen := make(map[string]struct{}, len(got))
	for _, item := range got {
		if _, exists := seen[item.ID]; exists {
			t.Fatalf("duplicate provider id %q", item.ID)
		}
		seen[item.ID] = struct{}{}
	}
}

func TestAvailableProviders_ReturnsCatalogShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewModelMappingHandler(nil)

	router := gin.New()
	router.GET("/v0/admin/model-mappings/providers", handler.AvailableProviders)

	req := httptest.NewRequest(http.MethodGet, "/v0/admin/model-mappings/providers", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Providers []providerCatalogItem `json:"providers"`
	}
	if errDecode := json.Unmarshal(w.Body.Bytes(), &resp); errDecode != nil {
		t.Fatalf("decode response failed: %v", errDecode)
	}
	if len(resp.Providers) == 0 {
		t.Fatalf("expected providers in response")
	}
	first := resp.Providers[0]
	if first.ID == "" || first.Label == "" || first.Category == "" {
		t.Fatalf("expected provider shape fields id/label/category non-empty, got %+v", first)
	}
	if !first.SupportsModels {
		t.Fatalf("expected supports_models=true for provider %q", first.ID)
	}
}
