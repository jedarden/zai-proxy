package testutil

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeTestOutput(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "go-test.out")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write test output: %v", err)
	}
	return path
}

func TestProcessTestOutput_CategorizesAndPersistsReport(t *testing.T) {
	inputPath := writeTestOutput(t, `=== RUN   TestWithoutStackTrace
    handler_test.go:10: assertion failed: expected 200, got 500
--- FAIL: TestWithoutStackTrace (0.00s)
=== RUN   TestTimeout
    client_test.go:20: context deadline exceeded
--- FAIL: TestTimeout (0.00s)
=== RUN   TestMultipleMatches
    request_test.go:30: HTTP status code 503 after timeout waiting for response
--- FAIL: TestMultipleMatches (0.00s)
FAIL
`)
	outputPath := filepath.Join(t.TempDir(), "categorized.json")

	report, err := ProcessTestOutput(inputPath, outputPath)
	if err != nil {
		t.Fatalf("ProcessTestOutput returned error: %v", err)
	}

	if report.Source != inputPath {
		t.Errorf("Source: got %q, want %q", report.Source, inputPath)
	}
	if len(report.Failures) != 3 {
		t.Fatalf("failures: got %d, want 3", len(report.Failures))
	}
	if report.Failures[0].Category != CategoryAssertionError {
		t.Errorf("first category: got %q, want %q", report.Failures[0].Category, CategoryAssertionError)
	}
	if report.Failures[0].StackTrace != "" {
		t.Errorf("failure without a stack trace should remain empty, got %q", report.Failures[0].StackTrace)
	}
	if report.Failures[1].Category != CategoryTimeout {
		t.Errorf("timeout category: got %q, want %q", report.Failures[1].Category, CategoryTimeout)
	}
	if report.Failures[2].Category != CategoryTimeout {
		t.Errorf("ambiguous category: got %q, want %q", report.Failures[2].Category, CategoryTimeout)
	}
	if !IsAmbiguous(report.Failures[2]) {
		t.Error("multiple matching patterns should be recorded as ambiguous")
	}

	stats := report.Statistics
	if stats.Total != 3 || stats.Categorized != 3 || stats.Uncategorized != 0 {
		t.Errorf("statistics: got total=%d categorized=%d uncategorized=%d, want 3, 3, 0", stats.Total, stats.Categorized, stats.Uncategorized)
	}
	if stats.ByCategory[CategoryTimeout] != 2 || stats.ByCategory[CategoryAssertionError] != 1 {
		t.Errorf("category counts: got %#v", stats.ByCategory)
	}
	if len(stats.Distribution) != 2 {
		t.Fatalf("distribution: got %d entries, want 2", len(stats.Distribution))
	}
	if stats.Distribution[0] != (CategoryDistribution{Category: CategoryTimeout, Count: 2, Percentage: 200.0 / 3.0}) {
		t.Errorf("most frequent distribution: got %#v", stats.Distribution[0])
	}
	if len(stats.MostCommonFailureTypes) != 1 || stats.MostCommonFailureTypes[0].Category != CategoryTimeout {
		t.Errorf("most common failure types: got %#v", stats.MostCommonFailureTypes)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read persisted report: %v", err)
	}
	var persisted CategorizationReport
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("decode persisted report: %v", err)
	}
	if persisted.Statistics.Total != 3 || len(persisted.Failures) != 3 {
		t.Errorf("persisted report: got total=%d failures=%d", persisted.Statistics.Total, len(persisted.Failures))
	}
	if err := VerifyCategorizedFailures(persisted); err != nil {
		t.Errorf("persisted report should have no unknown failures: %v", err)
	}
}

func TestCategorizeTestOutput_HandlesTruncatedOutput(t *testing.T) {
	// The final package-level FAIL line and any stack trace were lost, but the
	// completed per-test failure still has enough evidence to categorize.
	inputPath := writeTestOutput(t, `=== RUN   TestTruncatedOutput
    client_test.go:20: context deadline exceeded
--- FAIL: TestTruncatedOutput (0.00s)
`)

	report, err := CategorizeTestOutput(inputPath)
	if err != nil {
		t.Fatalf("CategorizeTestOutput returned error for truncated output: %v", err)
	}
	if len(report.Failures) != 1 {
		t.Fatalf("failures: got %d, want 1", len(report.Failures))
	}
	if report.Failures[0].Category != CategoryTimeout {
		t.Errorf("category: got %q, want %q", report.Failures[0].Category, CategoryTimeout)
	}
	if report.Failures[0].StackTrace != "" {
		t.Errorf("truncated stack trace: got %q, want empty", report.Failures[0].StackTrace)
	}
	if err := VerifyCategorizedFailures(report); err != nil {
		t.Errorf("truncated report should be fully categorized: %v", err)
	}
}

func TestProcessTestOutput_WritesUnknownFailuresAndReturnsVerificationError(t *testing.T) {
	inputPath := writeTestOutput(t, `=== RUN   TestUnclassified
    handler_test.go:10: unexpected protocol wobble
--- FAIL: TestUnclassified (0.00s)
FAIL
`)
	outputPath := filepath.Join(t.TempDir(), "categorized.json")

	report, err := ProcessTestOutput(inputPath, outputPath)
	if !errors.Is(err, ErrUncategorizedFailures) {
		t.Fatalf("ProcessTestOutput error: got %v, want ErrUncategorizedFailures", err)
	}
	if report.Statistics.Uncategorized != 1 {
		t.Errorf("uncategorized: got %d, want 1", report.Statistics.Uncategorized)
	}
	if report.Failures[0].Category != CategoryUnknown {
		t.Errorf("category: got %q, want %q", report.Failures[0].Category, CategoryUnknown)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Errorf("unknown result should still be persisted for inspection: %v", err)
	}
}

func TestCategorizeTestOutput_ReportsReadErrors(t *testing.T) {
	_, err := CategorizeTestOutput(filepath.Join(t.TempDir(), "missing.out"))
	if !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("CategorizeTestOutput error: got %v, want ErrFileNotFound", err)
	}
}

func TestCategorizeParsedFailures_Empty(t *testing.T) {
	report := CategorizeParsedFailures(nil)
	if report.Failures == nil {
		t.Error("Failures should be an empty slice, not nil")
	}
	if report.Statistics.ByCategory == nil {
		t.Error("ByCategory should be initialized")
	}
	if report.Statistics.Distribution == nil || report.Statistics.MostCommonFailureTypes == nil {
		t.Error("empty statistics should serialize as empty arrays")
	}
	if err := VerifyCategorizedFailures(report); err != nil {
		t.Errorf("empty report should be valid: %v", err)
	}
}
