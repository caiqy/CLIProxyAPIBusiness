package main

import (
	"path/filepath"
	"testing"
)

func TestRunGateCopilotDualRun_ExitCodes(t *testing.T) {
	passPath := filepath.Join("..", "..", "internal", "copilotgate", "testdata", "copilot_dualrun_7day_pass.json")
	if code := run([]string{"gate", "copilot-dualrun", "--report", passPath}); code != 0 {
		t.Fatalf("run(pass) exit code = %d, want 0", code)
	}

	failPath := filepath.Join("..", "..", "internal", "copilotgate", "testdata", "copilot_dualrun_7day_fail.json")
	if code := run([]string{"gate", "copilot-dualrun", "--report", failPath}); code != 2 {
		t.Fatalf("run(fail) exit code = %d, want 2", code)
	}

	if code := run([]string{"gate", "copilot-dualrun", "--report", filepath.Join(t.TempDir(), "missing.json")}); code != 1 {
		t.Fatalf("run(missing report) exit code = %d, want 1", code)
	}
}
