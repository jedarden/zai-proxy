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
}
