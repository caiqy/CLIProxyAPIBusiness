package handlers

import (
	"encoding/json"
	"testing"
)

func TestNormalizeProviderEntry_Codex_KeepRequiredFieldsOnly(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"key":           "codex-main",
		"type":          "codex",
		"access_token":  "token-123",
		"refresh_token": "refresh-456",
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
	if got, _ := normalized["key"].(string); got != "codex-main" {
		t.Fatalf("expected key codex-main, got %q", got)
	}
	if got, _ := normalized["access_token"].(string); got != "token-123" {
		t.Fatalf("expected access_token token-123, got %q", got)
	}
	if _, ok := normalized["unused"]; ok {
		t.Fatalf("expected unused field to be dropped")
	}

	if _, errMarshal := json.Marshal(normalized); errMarshal != nil {
		t.Fatalf("expected normalized payload to be json serializable: %v", errMarshal)
	}
}

func TestNormalizeProviderEntry_Kiro_MissingRequiredField(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"key":  "kiro-main",
		"type": "kiro",
	}

	_, err := normalizeProviderEntry("kiro", raw)
	if err == nil {
		t.Fatalf("expected error for missing kiro credential field")
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

func TestNormalizeProviderEntry_IgnoreExtraFields(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"id":            "qwen-main",
		"access_token":  "qwen-token",
		"refresh_token": "qwen-refresh",
		"extra_a":       true,
		"extra_b":       123,
	}

	normalized, err := normalizeProviderEntry("qwen", raw)
	if err != nil {
		t.Fatalf("normalizeProviderEntry returned error: %v", err)
	}

	if got, _ := normalized["key"].(string); got != "qwen-main" {
		t.Fatalf("expected key qwen-main from id fallback, got %q", got)
	}
	if _, ok := normalized["extra_a"]; ok {
		t.Fatalf("expected extra_a to be dropped")
	}
	if _, ok := normalized["extra_b"]; ok {
		t.Fatalf("expected extra_b to be dropped")
	}
}
