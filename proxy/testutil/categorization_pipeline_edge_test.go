package testutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func writeRawCategorizationOutput(t *testing.T, contents []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "go-test.out")
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write raw test output: %v", err)
	}
	return path
}

func TestCategorizationPipeline_EmptyAndNilInputs(t *testing.T) {
	parsed, err := ParseTestFailures(nil)
	if err != nil {
		t.Fatalf("ParseTestFailures(nil) error: %v", err)
	}
	if parsed == nil || len(parsed) != 0 {
		t.Errorf("ParseTestFailures(nil) = %#v, want non-nil empty slice", parsed)
	}

	for _, input := range [][]TestFailure{nil, {}} {
		categorized, stats := CategorizeFailures(input)
		if categorized == nil || len(categorized) != 0 {
			t.Errorf("CategorizeFailures(%#v) = %#v, want non-nil empty slice", input, categorized)
		}
		if stats.Total != 0 || len(stats.ByCategory) != 0 || stats.ByCategory == nil {
			t.Errorf("CategorizeFailures(%#v) stats = %#v, want initialized empty statistics", input, stats)
		}
	}

	report := CategorizeParsedFailures(nil)
	if report.Failures == nil || report.Statistics.ByCategory == nil {
		t.Errorf("CategorizeParsedFailures(nil) left collections nil: %#v", report)
	}
	if err := VerifyCategorizedFailures(report); err != nil {
		t.Errorf("VerifyCategorizedFailures(empty report) error: %v", err)
	}

	fallback := CategorizeFailure(TestFailure{})
	if fallback.Category != CategoryUnknown || fallback.Subcategory != fallbackSubcategoryEmptyFailure {
		t.Errorf("CategorizeFailure(zero TestFailure) = %q/%q, want unknown/%q", fallback.Category, fallback.Subcategory, fallbackSubcategoryEmptyFailure)
	}
	if !fallback.Uncertain {
		t.Error("zero TestFailure fallback must require manual review")
	}

	emptyPath := writeRawCategorizationOutput(t, nil)
	if _, err := CategorizeTestOutput(emptyPath); !errors.Is(err, ErrEmptyFile) {
		t.Errorf("CategorizeTestOutput(empty file) error = %v, want ErrEmptyFile", err)
	}
}

func TestCategorizationPipeline_MalformedOutputUsesFallback(t *testing.T) {
	invalidUTF8Output := append([]byte(`=== RUN   TestInvalidUTF8
    runner_test.go:27: malformed external test-runner diagnostic: `), 0xff, 0xfe)
	invalidUTF8Output = append(invalidUTF8Output, []byte(`
--- FAIL: TestInvalidUTF8 (0.00s)
FAIL
`)...)

	tests := []struct {
		name        string
		output      []byte
		invalidUTF8 bool
	}{
		{
			name: "truncated after completed failure",
			output: []byte(`=== RUN   TestTruncatedDiagnostic
    runner_test.go:14: truncated external test-runner diagnostic
--- FAIL: TestTruncatedDiagnostic (0.00s)
`),
		},
		{
			name:        "invalid UTF-8 in diagnostic",
			output:      invalidUTF8Output,
			invalidUTF8: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.invalidUTF8 && utf8.Valid(tt.output) {
				t.Fatal("invalid UTF-8 fixture unexpectedly contains valid UTF-8")
			}

			inputPath := writeRawCategorizationOutput(t, tt.output)
			outputPath := filepath.Join(t.TempDir(), "categorized.json")
			report, err := ProcessTestOutput(inputPath, outputPath)
			if !errors.Is(err, ErrUncategorizedFailures) {
				t.Fatalf("ProcessTestOutput() error = %v, want ErrUncategorizedFailures", err)
			}
			if len(report.Failures) != 1 {
				t.Fatalf("failure count = %d, want 1", len(report.Failures))
			}

			failure := report.Failures[0]
			if failure.Category != CategoryUnknown || failure.Subcategory != fallbackSubcategoryMalformedOutput {
				t.Errorf("fallback = %q/%q, want unknown/%q", failure.Category, failure.Subcategory, fallbackSubcategoryMalformedOutput)
			}
			if !failure.Uncertain {
				t.Error("malformed output fallback must require manual review")
			}
			if report.Statistics.Total != 1 || report.Statistics.Uncategorized != 1 {
				t.Errorf("statistics = %#v, want one uncategorized failure", report.Statistics)
			}

			persisted, readErr := os.ReadFile(outputPath)
			if readErr != nil {
				t.Fatalf("read persisted report: %v", readErr)
			}
			if !utf8.Valid(persisted) {
				t.Error("persisted JSON must remain valid UTF-8 when input diagnostic is not")
			}
		})
	}

	emptyPath := writeRawCategorizationOutput(t, []byte{})
	if _, err := CategorizeTestOutput(emptyPath); !errors.Is(err, ErrEmptyFile) {
		t.Errorf("CategorizeTestOutput(empty malformed output) error = %v, want ErrEmptyFile", err)
	}
}

func TestCategorizationPipeline_ConcurrentFailureOutput(t *testing.T) {
	inputPath := writeRawCategorizationOutput(t, []byte(`=== RUN   TestConcurrentDataRace
=== PAUSE TestConcurrentDataRace
=== RUN   TestClosedChannel
=== PAUSE TestClosedChannel
=== CONT  TestConcurrentDataRace
    worker_test.go:41: WARNING: DATA RACE between producer and consumer
--- FAIL: TestConcurrentDataRace (0.00s)
=== CONT  TestClosedChannel
    worker_test.go:77: send on closed channel
--- FAIL: TestClosedChannel (0.00s)
=== RUN   TestConcurrentTimeout
    worker_test.go:101: context deadline exceeded while waiting for workers
--- FAIL: TestConcurrentTimeout (0.00s)
FAIL
`))
	outputPath := filepath.Join(t.TempDir(), "categorized.json")

	report, err := ProcessTestOutput(inputPath, outputPath)
	if err != nil {
		t.Fatalf("ProcessTestOutput() error: %v", err)
	}

	wantCategories := []FailureCategory{CategoryDataRace, CategoryChannel, CategoryTimeout}
	if len(report.Failures) != len(wantCategories) {
		t.Fatalf("failure count = %d, want %d", len(report.Failures), len(wantCategories))
	}
	for i, want := range wantCategories {
		if got := report.Failures[i].Category; got != want {
			t.Errorf("failure %d category = %q, want %q", i, got, want)
		}
	}
	if report.Statistics.Total != len(wantCategories) || report.Statistics.Uncategorized != 0 {
		t.Errorf("statistics = %#v, want three fully categorized failures", report.Statistics)
	}
	if report.Statistics.ByCategory[CategoryDataRace] != 1 || report.Statistics.ByCategory[CategoryChannel] != 1 || report.Statistics.ByCategory[CategoryTimeout] != 1 {
		t.Errorf("category distribution = %#v, want one of each concurrent failure pattern", report.Statistics.ByCategory)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Errorf("categorized concurrent report was not persisted: %v", err)
	}
}

func TestCategorizationPipeline_VeryLongSpecialCharacterDiagnostic(t *testing.T) {
	const specialSegment = "path=\"C:\\tmp\\[edge]\" value='&<>$' unicode=雪 emoji=🧪 tab=\t; "
	message := strings.Repeat(specialSegment, 10_000) + "context deadline exceeded — request=☃"
	if len(message) < 500_000 {
		t.Fatalf("test fixture is not sufficiently long: %d bytes", len(message))
	}

	inputPath := writeRawCategorizationOutput(t, []byte("=== RUN   TestLongDiagnostic\n    long_test.go:99: "+message+"\n--- FAIL: TestLongDiagnostic (0.00s)\nFAIL\n"))
	outputPath := filepath.Join(t.TempDir(), "categorized.json")
	report, err := ProcessTestOutput(inputPath, outputPath)
	if err != nil {
		t.Fatalf("ProcessTestOutput() error for long diagnostic: %v", err)
	}
	if len(report.Failures) != 1 {
		t.Fatalf("failure count = %d, want 1", len(report.Failures))
	}

	failure := report.Failures[0]
	if failure.Category != CategoryTimeout {
		t.Errorf("category = %q, want %q", failure.Category, CategoryTimeout)
	}
	if failure.ErrorMessage != message {
		t.Errorf("error message was not preserved exactly: got %d bytes, want %d", len(failure.ErrorMessage), len(message))
	}
	if !strings.Contains(failure.ErrorMessage, "unicode=雪 emoji=🧪") || !strings.Contains(failure.ErrorMessage, `value='&<>$'`) {
		t.Errorf("special characters were not preserved in %q", failure.ErrorMessage[:min(len(failure.ErrorMessage), 200)])
	}
	if _, err := os.ReadFile(outputPath); err != nil {
		t.Errorf("read persisted long-diagnostic report: %v", err)
	}
}

func TestCategorizeFailures_ConcurrentCallersKeepResultsIndependent(t *testing.T) {
	failures := []TestFailure{
		{TestName: "TestRace", ErrorMessage: "WARNING: DATA RACE"},
		{TestName: "TestChannel", ErrorMessage: "send on closed channel"},
		{TestName: "TestTimeout", ErrorMessage: "context deadline exceeded"},
	}
	wantCategories := []FailureCategory{CategoryDataRace, CategoryChannel, CategoryTimeout}

	const workers = 24
	const iterations = 10
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for iteration := 0; iteration < iterations; iteration++ {
				categorized, stats := CategorizeFailures(failures)
				if len(categorized) != len(wantCategories) || stats.Total != len(wantCategories) {
					errs <- fmt.Errorf("got %d categorized failures and total %d", len(categorized), stats.Total)
					return
				}
				for i, want := range wantCategories {
					if categorized[i].Category != want {
						errs <- fmt.Errorf("failure %d category = %q, want %q", i, categorized[i].Category, want)
						return
					}
				}
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
