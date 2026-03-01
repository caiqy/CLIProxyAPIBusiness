package watcher

import (
	"testing"
	"time"
)

func TestSynthesizeAuthFromDBRow_IncludesExcludedModelsAttribute(t *testing.T) {
	now := time.Now().UTC()
	a := synthesizeAuthFromDBRow(
		"",
		"auth-a",
		[]byte(`{"type":"claude","email":"demo@example.com"}`),
		0,
		false,
		now,
		now,
		[]string{"claude-sonnet-4-6", "claude-opus-4-1"},
	)
	if a == nil {
		t.Fatal("expected auth not nil")
	}
	if a.Attributes["excluded_models"] != "claude-opus-4-1,claude-sonnet-4-6" {
		t.Fatalf("unexpected excluded_models attribute: %q", a.Attributes["excluded_models"])
	}
}

func TestNormalizeExcludedModelNames_DedupAndSort(t *testing.T) {
	input := []string{" claude-sonnet-4-6 ", "CLAUDE-SONNET-4-6", "", "claude-opus-4-1"}
	got := normalizeExcludedModelNames(input)
	want := []string{"claude-opus-4-1", "claude-sonnet-4-6"}
	if len(got) != len(want) {
		t.Fatalf("len(got)=%d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}
