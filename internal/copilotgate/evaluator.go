package copilotgate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const requiredWindowDays = 7
const dualRunSummaryReportType = "copilot_dualrun_7day_summary"

var validDiffTypes = map[string]struct{}{
	"missing":         {},
	"extra":           {},
	"value_mismatch":  {},
	"source_mismatch": {},
}

var validSources = map[string]struct{}{
	"computed":      {},
	"auth_metadata": {},
	"config":        {},
	"constant":      {},
}

type DualRunReport struct {
	ReportType string              `json:"report_type"`
	Days       []DualRunDaySummary `json:"days"`
}

type DualRunDaySummary struct {
	Date                  string       `json:"date"`
	CriticalDiffNew       int          `json:"critical_diff_new"`
	UndeclaredPassthrough int          `json:"undeclared_passthrough"`
	Diffs                 []DiffRecord `json:"diffs"`
}

type DiffRecord struct {
	Header    string   `json:"header"`
	DiffType  string   `json:"diff_type"`
	Legacy    DiffSide `json:"legacy"`
	Candidate DiffSide `json:"candidate"`
}

type DiffSide struct {
	Source          string `json:"source"`
	NormalizedValue string `json:"normalized_value"`
	ValueHash       string `json:"value_hash"`
}

type Result struct {
	WindowDays            int
	DaysInWindow          int
	CriticalDiffNew       int
	UndeclaredPassthrough int
	Pass                  bool
}

func EvaluateReportFile(path string) (Result, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}

	report, err := decodeReport(raw)
	if err != nil {
		return Result{}, fmt.Errorf("decode report: %w", err)
	}
	return EvaluateReport(report)
}

func decodeReport(raw []byte) (DualRunReport, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var report DualRunReport
	if err := decoder.Decode(&report); err != nil {
		return DualRunReport{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return DualRunReport{}, fmt.Errorf("unexpected extra JSON tokens")
		}
		return DualRunReport{}, err
	}
	return report, nil
}

func EvaluateReport(report DualRunReport) (Result, error) {
	if strings.TrimSpace(report.ReportType) != dualRunSummaryReportType {
		return Result{}, fmt.Errorf("invalid report_type: %q", report.ReportType)
	}
	if len(report.Days) == 0 {
		return Result{WindowDays: requiredWindowDays}, nil
	}

	timeline, err := buildTimeline(report.Days)
	if err != nil {
		return Result{}, err
	}

	for _, day := range timeline {
		if day.summary.CriticalDiffNew < 0 {
			return Result{}, fmt.Errorf("date %s: critical_diff_new must be >= 0", day.summary.Date)
		}
		if day.summary.UndeclaredPassthrough < 0 {
			return Result{}, fmt.Errorf("date %s: undeclared_passthrough must be >= 0", day.summary.Date)
		}
		if err := validateDiffContract(day.summary.Diffs); err != nil {
			return Result{}, fmt.Errorf("date %s: %w", day.summary.Date, err)
		}
	}

	window := timeline
	if len(window) > requiredWindowDays {
		window = window[len(window)-requiredWindowDays:]
	}

	windowComplete := len(window) == requiredWindowDays && hasContinuousDays(window)
	result := Result{
		WindowDays:   requiredWindowDays,
		DaysInWindow: len(window),
	}

	for _, day := range window {
		result.CriticalDiffNew += day.summary.CriticalDiffNew
		result.UndeclaredPassthrough += day.summary.UndeclaredPassthrough
	}

	result.Pass = windowComplete && result.CriticalDiffNew == 0 && result.UndeclaredPassthrough == 0
	return result, nil
}

type dayPoint struct {
	time    time.Time
	summary DualRunDaySummary
}

func buildTimeline(days []DualRunDaySummary) ([]dayPoint, error) {
	timeline := make([]dayPoint, 0, len(days))
	seen := make(map[string]struct{}, len(days))
	for i, day := range days {
		parsed, err := time.Parse("2006-01-02", strings.TrimSpace(day.Date))
		if err != nil {
			return nil, fmt.Errorf("invalid date at days[%d]: %w", i, err)
		}
		key := parsed.Format("2006-01-02")
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate date: %s", key)
		}
		seen[key] = struct{}{}
		timeline = append(timeline, dayPoint{time: parsed, summary: day})
	}

	sort.Slice(timeline, func(i, j int) bool {
		return timeline[i].time.Before(timeline[j].time)
	})
	return timeline, nil
}

func hasContinuousDays(window []dayPoint) bool {
	if len(window) < 2 {
		return len(window) == requiredWindowDays
	}
	for i := 1; i < len(window); i++ {
		if window[i].time.Sub(window[i-1].time) != 24*time.Hour {
			return false
		}
	}
	return true
}

func validateDiffContract(diffs []DiffRecord) error {
	for i, diff := range diffs {
		if err := validateDiffRecord(diff); err != nil {
			return fmt.Errorf("diffs[%d]: %w", i, err)
		}
	}
	return nil
}

func validateDiffRecord(diff DiffRecord) error {
	header := strings.TrimSpace(diff.Header)
	if header == "" {
		return fmt.Errorf("header is required")
	}
	if header != http.CanonicalHeaderKey(header) {
		return fmt.Errorf("header must be canonical MIME-style: %q", header)
	}

	diffType := strings.TrimSpace(diff.DiffType)
	if _, ok := validDiffTypes[diffType]; !ok {
		return fmt.Errorf("unsupported diff_type: %q", diffType)
	}

	if err := validateDiffSide(header, "legacy", diff.Legacy); err != nil {
		return err
	}
	if err := validateDiffSide(header, "candidate", diff.Candidate); err != nil {
		return err
	}

	switch diffType {
	case "missing":
		if diff.Legacy.NormalizedValue == "" {
			return fmt.Errorf("missing diff requires non-empty legacy.normalized_value")
		}
		if diff.Candidate.NormalizedValue != "" || diff.Candidate.ValueHash != "" {
			return fmt.Errorf("missing diff requires empty candidate normalized_value/value_hash")
		}
	case "extra":
		if diff.Candidate.NormalizedValue == "" {
			return fmt.Errorf("extra diff requires non-empty candidate.normalized_value")
		}
		if diff.Legacy.NormalizedValue != "" || diff.Legacy.ValueHash != "" {
			return fmt.Errorf("extra diff requires empty legacy normalized_value/value_hash")
		}
	case "value_mismatch":
		if diff.Legacy.NormalizedValue == "" || diff.Candidate.NormalizedValue == "" {
			return fmt.Errorf("value_mismatch requires both normalized values")
		}
		if diff.Legacy.NormalizedValue == diff.Candidate.NormalizedValue {
			return fmt.Errorf("value_mismatch requires different normalized values")
		}
	case "source_mismatch":
		if diff.Legacy.NormalizedValue == "" || diff.Candidate.NormalizedValue == "" {
			return fmt.Errorf("source_mismatch requires both normalized values")
		}
		if diff.Legacy.NormalizedValue != diff.Candidate.NormalizedValue {
			return fmt.Errorf("source_mismatch requires equal normalized values")
		}
	}

	return nil
}

func validateDiffSide(header, sideName string, side DiffSide) error {
	source := strings.TrimSpace(side.Source)
	if source == "" {
		return fmt.Errorf("%s.source is required", sideName)
	}
	if _, ok := validSources[source]; !ok {
		return fmt.Errorf("unsupported %s.source: %q", sideName, source)
	}

	normalized := normalizeValue(side.NormalizedValue)
	if side.NormalizedValue != normalized {
		return fmt.Errorf("%s.normalized_value is not normalized", sideName)
	}

	expectedHash := hashValue(header, side.NormalizedValue)
	if side.ValueHash != expectedHash {
		return fmt.Errorf("%s.value_hash mismatch", sideName)
	}

	return nil
}

func normalizeValue(value string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(value), " "))
}

func hashValue(header, normalizedValue string) string {
	if normalizedValue == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(header)) + "\n" + normalizedValue))
	return hex.EncodeToString(sum[:])
}
