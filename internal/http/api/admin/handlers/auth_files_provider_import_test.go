package handlers

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeProviderEntry_Codex_AutoKeyTypeFromEmail(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"access_token":  "token-123",
		"refresh_token": "refresh-456",
		"email":         "User@Example.COM ",
		"proxy_url":     "http://127.0.0.1:7890",
		"unused":        "drop-me",
	}

	normalized, err := normalizeProviderEntry("codex", raw)
	if err != nil {
		t.Fatalf("normalizeProviderEntry returned error: %v", err)
	}

	if got, _ := normalized["type"].(string); got != "codex" {
		t.Fatalf("expected type codex, got %q", got)
	}
	if got, _ := normalized["key"].(string); got != "codex-user@example.com" {
		t.Fatalf("expected key codex-user@example.com, got %q", got)
	}
	if got, _ := normalized["access_token"].(string); got != "token-123" {
		t.Fatalf("expected access_token token-123, got %q", got)
	}
	if got, _ := normalized["email"].(string); got != "User@Example.COM " {
		t.Fatalf("expected to keep raw email value, got %q", got)
	}
	if _, ok := normalized["unused"]; ok {
		t.Fatalf("expected unused field to be dropped")
	}

	if _, errMarshal := json.Marshal(normalized); errMarshal != nil {
		t.Fatalf("expected normalized payload to be json serializable: %v", errMarshal)
	}
}

func TestGenerateImportKey_FallbackToCredentialHashWhenNoEmail(t *testing.T) {
	t.Parallel()

	key1 := generateImportKey("kiro", map[string]any{"access_token": "at-1", "refresh_token": "rt-1"})
	key2 := generateImportKey("kiro", map[string]any{"access_token": "at-1", "refresh_token": "rt-1"})
	key3 := generateImportKey("kiro", map[string]any{"access_token": "at-2", "refresh_token": "rt-1"})

	if !strings.HasPrefix(key1, "kiro-h-") {
		t.Fatalf("expected fallback key to start with kiro-h-, got %q", key1)
	}
	if key1 != key2 {
		t.Fatalf("expected stable fallback key, got %q and %q", key1, key2)
	}
	if key1 == key3 {
		t.Fatalf("expected different credential inputs to generate different fallback key")
	}
}

func TestNormalizeProviderEntry_UnknownProvider(t *testing.T) {
	t.Parallel()

	raw := map[string]any{"key": "demo"}
	_, err := normalizeProviderEntry("unknown-provider", raw)
	if err == nil {
		t.Fatalf("expected error for unknown provider")
	}
}

func TestNormalizeProviderEntry_IFlow_ThreeModes(t *testing.T) {
	t.Parallel()

	entries := []map[string]any{
		{"api_key": "iflow-key-only"},
		{"cookie": "BXAuth=demo", "email": "iflow@example.com"},
		{"refresh_token": "iflow-refresh-only"},
	}

	for i, entry := range entries {
		normalized, err := normalizeProviderEntry("iflow-cookie", entry)
		if err != nil {
			t.Fatalf("mode %d should pass, got error: %v", i+1, err)
		}
		if got, _ := normalized["type"].(string); got != "iflow" {
			t.Fatalf("expected canonical type iflow, got %q", got)
		}
		if _, ok := normalized["key"].(string); !ok {
			t.Fatalf("expected auto-generated key in mode %d", i+1)
		}
	}
}

func TestNormalizeProviderEntry_Gemini_AllowsTokenAccessToken(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"email": "gem@example.com",
		"token": map[string]any{
			"access_token": "nested-token",
		},
		"extra_a": true,
	}

	normalized, err := normalizeProviderEntry("gemini-cli", raw)
	if err != nil {
		t.Fatalf("normalizeProviderEntry returned error: %v", err)
	}

	if got, _ := normalized["type"].(string); got != "gemini" {
		t.Fatalf("expected canonical type gemini, got %q", got)
	}
	if _, ok := normalized["token"].(map[string]any); !ok {
		t.Fatalf("expected nested token map to be preserved")
	}
	if _, ok := normalized["extra_a"]; ok {
		t.Fatalf("expected extra field to be dropped")
	}
}

func TestNormalizeProviderEntry_Kimi_AccessTokenRequired(t *testing.T) {
	t.Parallel()

	if _, err := normalizeProviderEntry("kimi", map[string]any{"refresh_token": "rt-only"}); err == nil {
		t.Fatalf("expected kimi import to require access_token")
	}

	normalized, err := normalizeProviderEntry("kimi", map[string]any{
		"access_token": "kimi-at",
		"email":        "kimi@example.com",
	})
	if err != nil {
		t.Fatalf("expected kimi entry with access_token to pass, got error: %v", err)
	}
	if got, _ := normalized["type"].(string); got != "kimi" {
		t.Fatalf("expected canonical type kimi, got %q", got)
	}
}

func TestNormalizeProviderEntry_GitHubCopilot_AccessTokenRequired(t *testing.T) {
	t.Parallel()

	if _, err := normalizeProviderEntry("github-copilot", map[string]any{"refresh_token": "rt-only"}); err == nil {
		t.Fatalf("expected github-copilot import to require access_token")
	}

	normalized, err := normalizeProviderEntry("github-copilot", map[string]any{
		"access_token": "gh-at",
		"email":        "gh@example.com",
	})
	if err != nil {
		t.Fatalf("expected github-copilot entry with access_token to pass, got error: %v", err)
	}
	if got, _ := normalized["type"].(string); got != "github-copilot" {
		t.Fatalf("expected canonical type github-copilot, got %q", got)
	}
}

func TestNormalizeProviderEntry_Kilo_AccessTokenRequired(t *testing.T) {
	t.Parallel()

	if _, err := normalizeProviderEntry("kilo", map[string]any{"organization_id": "org-1"}); err == nil {
		t.Fatalf("expected kilo import to require access_token")
	}

	normalized, err := normalizeProviderEntry("kilo", map[string]any{
		"access_token":    "kilo-at",
		"organization_id": "org-1",
	})
	if err != nil {
		t.Fatalf("expected kilo entry with access_token to pass, got error: %v", err)
	}
	if got, _ := normalized["type"].(string); got != "kilo" {
		t.Fatalf("expected canonical type kilo, got %q", got)
	}
}
