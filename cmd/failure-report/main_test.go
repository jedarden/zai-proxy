package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildReportOmitsLogDiagnosticsFromFailedTest(t *testing.T) {
	repoRoot := t.TempDir()
	packageDir := filepath.Join(repoRoot, "proxy")
	if err := os.Mkdir(packageDir, 0o755); err != nil {
		t.Fatalf("create source directory: %v", err)
	}
	sourcePath := filepath.Join(packageDir, "sample_test.go")
	source := strings.Join([]string{
		"package proxy",
		"",
		"import \"testing\"",
		"",
		"func TestExample(t *testing.T) {",
		"\tt.Logf(\"context only\")",
		"\tt.Errorf(\"assertion failed: expected 1, got 2\")",
		"}",
	}, "\n")
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}

	outputPath := filepath.Join(repoRoot, "go-test.out")
	output := "=== RUN   TestExample\n" +
		"    sample_test.go:6: context only\n" +
		"    sample_test.go:7: assertion failed: expected 1, got 2\n" +
		"--- FAIL: TestExample (0.00s)\nFAIL\n"
	if err := os.WriteFile(outputPath, []byte(output), 0o644); err != nil {
		t.Fatalf("write test-output fixture: %v", err)
	}

	diagnostics, failedTests, failureMarkers, err := parseOutput(outputPath)
	if err != nil {
		t.Fatalf("parseOutput: %v", err)
	}
	report, err := buildReport(diagnostics, failedTests, failureMarkers, packageDir, filepath.Join(repoRoot, "failures.md"), "test-revision", "2026-08-21T00:00:00Z", sourceOutput{})
	if err != nil {
		t.Fatalf("buildReport: %v", err)
	}

	if report.Summary.TotalFailures != 1 || report.Summary.FailedTestCases != 1 {
		t.Fatalf("summary = %#v, want one source failure", report.Summary)
	}
	if len(report.Failures) != 1 {
		t.Fatalf("failures = %#v, want only the t.Errorf diagnostic", report.Failures)
	}
	failure := report.Failures[0]
	if failure.LineNumber != 7 || failure.Category != "assertion_error" {
		t.Errorf("failure = %#v, want line 7 assertion_error", failure)
	}
	if len(report.Summary.ByTest) != 1 || report.Summary.ByTest[0].Name != "TestExample" || report.Summary.ByTest[0].Count != 1 || report.Summary.ByTest[0].Percentage != 100 {
		t.Errorf("ByTest = %#v, want TestExample with one failure at 100%%", report.Summary.ByTest)
	}
	if len(report.Summary.ByFile) != 1 || report.Summary.ByFile[0].Count != 1 || report.Summary.ByFile[0].Percentage != 100 {
		t.Errorf("ByFile = %#v, want one file with one failure at 100%%", report.Summary.ByFile)
	}
	if len(report.Summary.ByPattern) != 1 || report.Summary.ByPattern[0].Pattern != "Other failure message" || report.Summary.ByPattern[0].Count != 1 {
		t.Errorf("ByPattern = %#v, want one fallback pattern", report.Summary.ByPattern)
	}
	if report.Summary.ByCategory[0].Percentage != 100 {
		t.Errorf("ByCategory = %#v, want 100%% assertion_error", report.Summary.ByCategory)
	}
	markdown := renderMarkdown(report)
	for _, heading := range []string{
		"## Most Frequently Failing Tests",
		"## Files with Highest Failure Density",
		"## Most Common Failure Patterns",
		"## Rate-limiter Impact",
	} {
		if !strings.Contains(markdown, heading) {
			t.Errorf("rendered Markdown does not contain %q:\n%s", heading, markdown)
		}
	}
}

func TestFailurePattern(t *testing.T) {
	tests := map[string]string{
		"Memory allocation too high: 999 bytes (max: 1)":        "Memory-allocation limit exceeded",
		"expected changed=true when thinking is stripped":       "Transformation did not report a change",
		"'thinking' should have been removed":                   "Thinking field was not removed",
		"'system' should be a string after translation":         "System field was not converted to a string",
		"content block still has cache_control after stripping": "cache_control was not removed",
		"expected error for invalid JSON":                       "Invalid JSON did not return an error",
		"a diagnostic without a recognized recurring pattern":   "Other failure message",
	}
	for message, want := range tests {
		if got := failurePattern(message); got != want {
			t.Errorf("failurePattern(%q) = %q, want %q", message, got, want)
		}
	}
}
