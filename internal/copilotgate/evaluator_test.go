package copilotgate

import (
	"path/filepath"
	"testing"
)

func TestDualRunGateEvaluator_RequiresSevenDayCleanWindow(t *testing.T) {
	passPath := filepath.Join("testdata", "copilot_dualrun_7day_pass.json")
	passResult, err := EvaluateReportFile(passPath)
	if err != nil {
		t.Fatalf("EvaluateReportFile(pass) error = %v", err)
	}
	if !passResult.Pass {
		t.Fatalf("pass report should pass gate, got %+v", passResult)
	}
	if passResult.CriticalDiffNew != 0 || passResult.UndeclaredPassthrough != 0 {
		t.Fatalf("pass report should have clean window counts, got %+v", passResult)
	}

	failPath := filepath.Join("testdata", "copilot_dualrun_7day_fail.json")
	failResult, err := EvaluateReportFile(failPath)
	if err != nil {
		t.Fatalf("EvaluateReportFile(fail) error = %v", err)
	}
	if failResult.Pass {
		t.Fatalf("fail report should block gate, got %+v", failResult)
	}
	if failResult.CriticalDiffNew == 0 && failResult.UndeclaredPassthrough == 0 {
		t.Fatalf("fail report should have non-zero blocker counts, got %+v", failResult)
	}
}

func TestDualRunGateEvaluator_ValidatesDiffContract(t *testing.T) {
	report := DualRunReport{
		ReportType: "copilot_dualrun_7day_summary",
		Days: []DualRunDaySummary{
			{
				Date:                  "2026-03-01",
				CriticalDiffNew:       1,
				UndeclaredPassthrough: 0,
				Diffs: []DiffRecord{
					{
						Header:   "x-request-id",
						DiffType: "value_mismatch",
						Legacy: DiffSide{
							Source:          "computed",
							NormalizedValue: "legacy-id",
							ValueHash:       "bad-hash",
						},
						Candidate: DiffSide{
							Source:          "config",
							NormalizedValue: "candidate-id",
							ValueHash:       "bad-hash",
						},
					},
				},
			},
		},
	}

	if _, err := EvaluateReport(report); err == nil {
		t.Fatal("EvaluateReport should reject invalid diff contract")
	}
}

func TestDualRunGateEvaluator_RequiresExpectedReportType(t *testing.T) {
	report := DualRunReport{ReportType: "unexpected", Days: []DualRunDaySummary{{Date: "2026-03-01"}}}
	if _, err := EvaluateReport(report); err == nil {
		t.Fatal("EvaluateReport should reject unknown report_type")
	}
}

func TestDualRunGateEvaluator_RejectsNegativeCounters(t *testing.T) {
	report := DualRunReport{
		ReportType: "copilot_dualrun_7day_summary",
		Days: []DualRunDaySummary{{
			Date:                  "2026-03-01",
			CriticalDiffNew:       -1,
			UndeclaredPassthrough: 0,
		}},
	}
	if _, err := EvaluateReport(report); err == nil {
		t.Fatal("EvaluateReport should reject negative counters")
	}
}
