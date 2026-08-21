package testutil

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
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
	if report.Failures[0].Subcategory != fallbackSubcategoryUnclassified {
		t.Errorf("subcategory: got %q, want %q", report.Failures[0].Subcategory, fallbackSubcategoryUnclassified)
	}
	data, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		t.Fatalf("unknown result should still be persisted for inspection: %v", readErr)
	}
	var persisted CategorizedFailureOutput
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("decode persisted unknown result: %v", err)
	}
	if len(persisted.Failures) != 1 || persisted.Failures[0].Label != "Other: unclassified failure" {
		t.Errorf("persisted unknown labels: got %#v", persisted.Failures)
	}
}

func TestCategorizeTestOutput_FallbacksForMalformedFailureOutput(t *testing.T) {
	inputPath := writeTestOutput(t, `=== RUN   TestMalformedOutput
    parser_test.go:abc: malformed external test-runner response
--- FAIL: TestMalformedOutput (0.00s)
FAIL
`)

	report, err := CategorizeTestOutput(inputPath)
	if err != nil {
		t.Fatalf("CategorizeTestOutput returned error: %v", err)
	}
	if len(report.Failures) != 1 {
		t.Fatalf("failures: got %d, want 1", len(report.Failures))
	}

	fallback := report.Failures[0]
	if fallback.Category != CategoryUnknown {
		t.Errorf("category: got %q, want %q", fallback.Category, CategoryUnknown)
	}
	if fallback.Subcategory != fallbackSubcategoryMalformedOutput {
		t.Errorf("subcategory: got %q, want %q", fallback.Subcategory, fallbackSubcategoryMalformedOutput)
	}
	if label := GetCategoryLabel(fallback); label != "Other: malformed output" {
		t.Errorf("GetCategoryLabel() = %q, want %q", label, "Other: malformed output")
	}
}

func TestCategorizeTestOutput_ReportsReadErrors(t *testing.T) {
	_, err := CategorizeTestOutput(filepath.Join(t.TempDir(), "missing.out"))
	if !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("CategorizeTestOutput error: got %v, want ErrFileNotFound", err)
	}
}

func TestExportCategorizationReportJSON_ReportsWriteErrors(t *testing.T) {
	outputPath := t.TempDir()

	err := ExportCategorizationReportJSON(CategorizeParsedFailures(nil), outputPath)
	if err == nil {
		t.Fatal("ExportCategorizationReportJSON error: got nil, want write error")
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

func TestOutputCategorizedFailures_ParsedFailureWorkflowFormatsAndFilters(t *testing.T) {
	parsed, err := ParseTestFailures([]byte(`=== RUN   TestAssertion
    handler_test.go:10: assertion failed: expected 200, got 500
--- FAIL: TestAssertion (0.00s)
=== RUN   TestTimeout
    client_test.go:20: context deadline exceeded
--- FAIL: TestTimeout (0.00s)
=== RUN   TestAnotherTimeout
    worker_test.go:30: timeout waiting for worker shutdown
--- FAIL: TestAnotherTimeout (0.00s)
FAIL
`))
	if err != nil {
		t.Fatalf("ParseTestFailures() error: %v", err)
	}

	var jsonBuffer bytes.Buffer
	jsonOutput, err := OutputCategorizedFailures(&jsonBuffer, parsed, CategorizedFailureOutputOptions{
		Format: CategorizedFailureOutputJSON,
	})
	if err != nil {
		t.Fatalf("OutputCategorizedFailures(JSON) error: %v", err)
	}
	if jsonOutput.Statistics.Total != 3 {
		t.Fatalf("JSON total: got %d, want 3", jsonOutput.Statistics.Total)
	}
	if jsonOutput.Statistics.ByCategory[CategoryTimeout] != 2 || jsonOutput.Statistics.ByCategory[CategoryAssertionError] != 1 {
		t.Errorf("JSON category counts: got %#v", jsonOutput.Statistics.ByCategory)
	}
	if len(jsonOutput.Statistics.Distribution) != 2 {
		t.Fatalf("JSON distribution: got %#v", jsonOutput.Statistics.Distribution)
	}
	if got := jsonOutput.Statistics.Distribution[0]; got.Category != CategoryTimeout || got.Count != 2 || math.Abs(got.Percentage-200.0/3.0) > 0.000001 {
		t.Errorf("JSON timeout distribution: got %#v, want timeout with count 2 and 66.67%%", got)
	}
	if got := jsonOutput.Failures[0].Label; got != "assertion_error" {
		t.Errorf("JSON failure label: got %q, want assertion_error", got)
	}

	var decoded CategorizedFailureOutput
	if err := json.Unmarshal(jsonBuffer.Bytes(), &decoded); err != nil {
		t.Fatalf("decode JSON output: %v", err)
	}
	if len(decoded.Failures) != 3 || decoded.Failures[1].Label != "timeout" {
		t.Errorf("decoded labels: got %#v", decoded.Failures)
	}

	var textBuffer bytes.Buffer
	textOutput, err := OutputCategorizedFailures(&textBuffer, parsed, CategorizedFailureOutputOptions{
		Format:     CategorizedFailureOutputText,
		Categories: []FailureCategory{CategoryTimeout},
	})
	if err != nil {
		t.Fatalf("OutputCategorizedFailures(text) error: %v", err)
	}
	if textOutput.Statistics.Total != 2 || textOutput.Statistics.Categorized != 2 || textOutput.Statistics.Uncategorized != 0 {
		t.Errorf("filtered statistics: got %#v", textOutput.Statistics)
	}
	if len(textOutput.Statistics.Distribution) != 1 || textOutput.Statistics.Distribution[0].Percentage != 100 {
		t.Errorf("filtered distribution: got %#v, want one timeout category at 100%%", textOutput.Statistics.Distribution)
	}

	text := textBuffer.String()
	for _, want := range []string{
		"Category filter: timeout",
		"Total failures: 2",
		"timeout: 2 (100.00%)",
		"[timeout] TestTimeout",
		"[timeout] TestAnotherTimeout",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "TestAssertion") {
		t.Errorf("text output includes a filtered failure:\n%s", text)
	}
}

func TestFormatCategorizedFailureOutput_RejectsUnsupportedFormat(t *testing.T) {
	_, err := FormatCategorizedFailureOutput(CategorizeParsedFailures(nil), CategorizedFailureOutputOptions{
		Format: CategorizedFailureOutputFormat("yaml"),
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported categorized failure output format") {
		t.Fatalf("FormatCategorizedFailureOutput() error = %v, want unsupported format error", err)
	}
}
