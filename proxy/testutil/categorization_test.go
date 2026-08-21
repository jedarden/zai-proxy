package testutil

import (
	"strings"
	"testing"
)

// TestCategorizeFailure_AssertionError tests assertion failure categorization
func TestCategorizeFailure_AssertionError(t *testing.T) {
	testCases := []struct {
		name        string
		errorMsg    string
		stackTrace  string
		expectedCat FailureCategory
		minConf     float64
	}{
		{
			name:        "standard assertion",
			errorMsg:    "assertion failed: expected 200, got 500",
			stackTrace:  "",
			expectedCat: CategoryAssertionError,
			minConf:     0.5,
		},
		{
			name:        "expected but got",
			errorMsg:    "expected success, got error",
			stackTrace:  "",
			expectedCat: CategoryAssertionError,
			minConf:     0.5,
		},
		{
			name:        "not equal",
			errorMsg:    "values not equal: 42 != 43",
			stackTrace:  "",
			expectedCat: CategoryAssertionError,
			minConf:     0.5,
		},
		{
			name:        "should be",
			errorMsg:    "response should be OK",
			stackTrace:  "",
			expectedCat: CategoryAssertionError,
			minConf:     0.5,
		},
		{
			name:        "want got pattern",
			errorMsg:    "want true, got false",
			stackTrace:  "",
			expectedCat: CategoryAssertionError,
			minConf:     0.5,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestAssertion",
				FilePath:     "test.go",
				LineNumber:   10,
				ErrorMessage: tt.errorMsg,
				StackTrace:   tt.stackTrace,
			}

			cat := CategorizeFailure(failure)

			if cat.Category != tt.expectedCat {
				t.Errorf("Category: got %q, want %q", cat.Category, tt.expectedCat)
			}
			if cat.Confidence.Float64() < tt.minConf {
				t.Errorf("Confidence: got %.2f, want >= %.2f", cat.Confidence, tt.minConf)
			}
			if cat.Reasoning == "" {
				t.Error("Reasoning should not be empty")
			}
		})
	}
}

// TestCategorizeFailure_Timeout tests timeout categorization
func TestCategorizeFailure_Timeout(t *testing.T) {
	testCases := []struct {
		name        string
		errorMsg    string
		expectedCat FailureCategory
		minConf     float64
	}{
		{
			name:        "context deadline exceeded",
			errorMsg:    "context deadline exceeded",
			expectedCat: CategoryTimeout,
			minConf:     0.9,
		},
		{
			name:        "test timed out",
			errorMsg:    "test timed out after 5s",
			expectedCat: CategoryTimeout,
			minConf:     0.9,
		},
		{
			name:        "timeout waiting for",
			errorMsg:    "timeout waiting for response",
			expectedCat: CategoryTimeout,
			minConf:     0.9,
		},
		{
			name:        "exceeded timeout",
			errorMsg:    "operation exceeded 30s timeout",
			expectedCat: CategoryTimeout,
			minConf:     0.9,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestTimeout",
				FilePath:     "test.go",
				LineNumber:   10,
				ErrorMessage: tt.errorMsg,
			}

			cat := CategorizeFailure(failure)

			if cat.Category != tt.expectedCat {
				t.Errorf("Category: got %q, want %q", cat.Category, tt.expectedCat)
			}
			if cat.Confidence.Float64() < tt.minConf {
				t.Errorf("Confidence: got %.2f, want >= %.2f", cat.Confidence, tt.minConf)
			}
		})
	}
}

// TestCategorizeFailure_Panic tests panic categorization
func TestCategorizeFailure_Panic(t *testing.T) {
	testCases := []struct {
		name        string
		errorMsg    string
		stackTrace  string
		expectedCat FailureCategory
		minConf     float64
	}{
		{
			name:        "runtime panic in error",
			errorMsg:    "runtime panic: division by zero",
			stackTrace:  "",
			expectedCat: CategoryPanic,
			minConf:     0.9,
		},
		{
			name:        "panic in stack trace",
			errorMsg:    "test failed",
			stackTrace:  "panic: interface conversion",
			expectedCat: CategoryPanic,
			minConf:     0.6,
		},
		{
			name:        "panic() call",
			errorMsg:    "call to panic()",
			stackTrace:  "",
			expectedCat: CategoryPanic,
			minConf:     0.9,
		},
		{
			name:        "goroutine panic",
			errorMsg:    "panic in goroutine",
			stackTrace:  "goroutine 1 [running]:",
			expectedCat: CategoryGoroutinePanic,
			minConf:     0.7,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestPanic",
				FilePath:     "test.go",
				LineNumber:   10,
				ErrorMessage: tt.errorMsg,
				StackTrace:   tt.stackTrace,
			}

			cat := CategorizeFailure(failure)

			if cat.Category != tt.expectedCat {
				t.Errorf("Category: got %q, want %q", cat.Category, tt.expectedCat)
			}
			if cat.Confidence.Float64() < tt.minConf {
				t.Errorf("Confidence: got %.2f, want >= %.2f", cat.Confidence, tt.minConf)
			}
		})
	}
}

// TestCategorizeFailure_DataRace tests data race categorization
func TestCategorizeFailure_DataRace(t *testing.T) {
	errorMsg := `WARNING: DATA RACE
Write at 0x... by goroutine 7:
  previous test
  /path/to/test.go:45
Read at 0x... by main goroutine:
  current test
  /path/to/test.go:50`

	failure := TestFailure{
		TestName:     "TestDataRace",
		FilePath:     "test.go",
		LineNumber:   50,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	if cat.Category != CategoryDataRace {
		t.Errorf("Category: got %q, want %q", cat.Category, CategoryDataRace)
	}
	if cat.Confidence.Float64() < 0.9 {
		t.Errorf("Confidence: got %.2f, want >= 0.9", cat.Confidence)
	}
}

// TestCategorizeFailure_NilPointer tests nil pointer categorization
func TestCategorizeFailure_NilPointer(t *testing.T) {
	testCases := []struct {
		name        string
		errorMsg    string
		expectedCat FailureCategory
	}{
		{
			name:        "nil pointer dereference",
			errorMsg:    "nil pointer dereference",
			expectedCat: CategoryNilPointer,
		},
		{
			name:        "null pointer",
			errorMsg:    "null pointer access",
			expectedCat: CategoryNilPointer,
		},
		{
			name:        "panic on nil",
			errorMsg:    "panic on nil pointer",
			expectedCat: CategoryNilPointer,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestNilPointer",
				FilePath:     "test.go",
				LineNumber:   10,
				ErrorMessage: tt.errorMsg,
			}

			cat := CategorizeFailure(failure)

			if cat.Category != tt.expectedCat {
				t.Errorf("Category: got %q, want %q", cat.Category, tt.expectedCat)
			}
			if cat.Confidence.Float64() < 0.9 {
				t.Errorf("Confidence: got %.2f, want >= 0.9", cat.Confidence)
			}
		})
	}
}

// TestCategorizeFailure_TypeMismatch tests type mismatch categorization
func TestCategorizeFailure_TypeMismatch(t *testing.T) {
	testCases := []struct {
		name        string
		errorMsg    string
		expectedCat FailureCategory
	}{
		{
			name:        "interface conversion",
			errorMsg:    "interface conversion: interface {} is string, not int",
			expectedCat: CategoryTypeMismatch,
		},
		{
			name:        "type mismatch",
			errorMsg:    "type mismatch: cannot convert string to int",
			expectedCat: CategoryTypeMismatch,
		},
		{
			name:        "type assertion",
			errorMsg:    "type assertion failed",
			expectedCat: CategoryTypeMismatch,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestTypeMismatch",
				FilePath:     "test.go",
				LineNumber:   10,
				ErrorMessage: tt.errorMsg,
			}

			cat := CategorizeFailure(failure)

			if cat.Category != tt.expectedCat {
				t.Errorf("Category: got %q, want %q", cat.Category, tt.expectedCat)
			}
			if cat.Confidence.Float64() < 0.5 {
				t.Errorf("Confidence: got %.2f, want >= 0.5", cat.Confidence)
			}
		})
	}
}

// TestCategorizeFailure_IndexOutOfRange tests index out of range categorization
func TestCategorizeFailure_IndexOutOfRange(t *testing.T) {
	testCases := []struct {
		name        string
		errorMsg    string
		expectedCat FailureCategory
	}{
		{
			name:        "index out of range",
			errorMsg:    "index out of range [5] with length 3",
			expectedCat: CategoryIndexOutOfRange,
		},
		{
			name:        "slice bounds",
			errorMsg:    "slice bounds out of range",
			expectedCat: CategoryIndexOutOfRange,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestIndexOutOfRange",
				FilePath:     "test.go",
				LineNumber:   10,
				ErrorMessage: tt.errorMsg,
			}

			cat := CategorizeFailure(failure)

			if cat.Category != tt.expectedCat {
				t.Errorf("Category: got %q, want %q", cat.Category, tt.expectedCat)
			}
			if cat.Confidence.Float64() < 0.9 {
				t.Errorf("Confidence: got %.2f, want >= 0.9", cat.Confidence)
			}
		})
	}
}

// TestCategorizeFailure_MapKey tests map key error categorization
func TestCategorizeFailure_MapKey(t *testing.T) {
	testCases := []struct {
		name        string
		errorMsg    string
		expectedCat FailureCategory
	}{
		{
			name:        "key not found",
			errorMsg:    "map key not found",
			expectedCat: CategoryMapKey,
		},
		{
			name:        "zero map key",
			errorMsg:    "zero map key in map access",
			expectedCat: CategoryMapKey,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestMapKey",
				FilePath:     "test.go",
				LineNumber:   10,
				ErrorMessage: tt.errorMsg,
			}

			cat := CategorizeFailure(failure)

			if cat.Category != tt.expectedCat {
				t.Errorf("Category: got %q, want %q", cat.Category, tt.expectedCat)
			}
			if cat.Confidence.Float64() < 0.8 {
				t.Errorf("Confidence: got %.2f, want >= 0.8", cat.Confidence)
			}
		})
	}
}

// TestCategorizeFailure_Channel tests channel error categorization
func TestCategorizeFailure_Channel(t *testing.T) {
	testCases := []struct {
		name        string
		errorMsg    string
		expectedCat FailureCategory
	}{
		{
			name:        "send on closed",
			errorMsg:    "send on closed channel",
			expectedCat: CategoryChannel,
		},
		{
			name:        "close of closed",
			errorMsg:    "close of closed channel",
			expectedCat: CategoryChannel,
		},
		{
			name:        "receive on closed",
			errorMsg:    "receive on closed channel",
			expectedCat: CategoryChannel,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestChannel",
				FilePath:     "test.go",
				LineNumber:   10,
				ErrorMessage: tt.errorMsg,
			}

			cat := CategorizeFailure(failure)

			if cat.Category != tt.expectedCat {
				t.Errorf("Category: got %q, want %q", cat.Category, tt.expectedCat)
			}
			if cat.Confidence.Float64() < 0.9 {
				t.Errorf("Confidence: got %.2f, want >= 0.9", cat.Confidence)
			}
		})
	}
}

// TestCategorizeFailure_IOError tests I/O error categorization
func TestCategorizeFailure_IOError(t *testing.T) {
	testCases := []struct {
		name        string
		errorMsg    string
		expectedCat FailureCategory
	}{
		{
			name:        "file not found",
			errorMsg:    "no such file or directory: /tmp/test.txt",
			expectedCat: CategoryIOError,
		},
		{
			name:        "permission denied",
			errorMsg:    "permission denied reading file",
			expectedCat: CategoryIOError,
		},
		{
			name:        "i/o error",
			errorMsg:    "i/o error: read failed",
			expectedCat: CategoryIOError,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestIOError",
				FilePath:     "test.go",
				LineNumber:   10,
				ErrorMessage: tt.errorMsg,
			}

			cat := CategorizeFailure(failure)

			if cat.Category != tt.expectedCat {
				t.Errorf("Category: got %q, want %q", cat.Category, tt.expectedCat)
			}
			if cat.Confidence.Float64() < 0.8 {
				t.Errorf("Confidence: got %.2f, want >= 0.8", cat.Confidence)
			}
		})
	}
}

// TestCategorizeFailure_HTTPError tests HTTP error categorization
func TestCategorizeFailure_HTTPError(t *testing.T) {
	testCases := []struct {
		name        string
		errorMsg    string
		expectedCat FailureCategory
	}{
		{
			name:        "status code",
			errorMsg:    "HTTP status code 500",
			expectedCat: CategoryHTTPError,
		},
		{
			name:        "connection refused",
			errorMsg:    "dial tcp 127.0.0.1:8080: connection refused",
			expectedCat: CategoryHTTPError,
		},
		{
			name:        "connection timeout",
			errorMsg:    "dial tcp: connection timeout",
			expectedCat: CategoryHTTPError,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestHTTPError",
				FilePath:     "test.go",
				LineNumber:   10,
				ErrorMessage: tt.errorMsg,
			}

			cat := CategorizeFailure(failure)

			if cat.Category != tt.expectedCat {
				t.Errorf("Category: got %q, want %q", cat.Category, tt.expectedCat)
			}
			if cat.Confidence.Float64() < 0.8 {
				t.Errorf("Confidence: got %.2f, want >= 0.8", cat.Confidence)
			}
		})
	}
}

// TestCategorizeFailure_Unknown tests unknown failure categorization
func TestCategorizeFailure_Unknown(t *testing.T) {
	errorMsg := "something completely unexpected happened"

	failure := TestFailure{
		TestName:     "TestUnknown",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	if cat.Category != CategoryUnknown {
		t.Errorf("Category: got %q, want %q", cat.Category, CategoryUnknown)
	}
	if cat.Confidence != 0.0 {
		t.Errorf("Confidence: got %.2f, want 0.0", cat.Confidence)
	}
	if cat.Subcategory != fallbackSubcategoryUnclassified {
		t.Errorf("Subcategory: got %q, want %q", cat.Subcategory, fallbackSubcategoryUnclassified)
	}
	if label := GetCategoryLabel(cat); label != "Other: unclassified failure" {
		t.Errorf("GetCategoryLabel() = %q, want %q", label, "Other: unclassified failure")
	}
	if cat.Reasoning == "" {
		t.Error("Reasoning should not be empty")
	}
}

// TestCategorizeFailures_Multiple tests categorizing multiple failures
func TestCategorizeFailures_Multiple(t *testing.T) {
	failures := []TestFailure{
		{
			TestName:     "TestAssertion",
			FilePath:     "test.go",
			LineNumber:   10,
			ErrorMessage: "expected 200, got 500",
		},
		{
			TestName:     "TestTimeout",
			FilePath:     "test.go",
			LineNumber:   20,
			ErrorMessage: "context deadline exceeded",
		},
		{
			TestName:     "TestPanic",
			FilePath:     "test.go",
			LineNumber:   30,
			ErrorMessage: "panic: division by zero",
		},
		{
			TestName:     "TestUnknown",
			FilePath:     "test.go",
			LineNumber:   40,
			ErrorMessage: "something weird",
		},
	}

	categorized, stats := CategorizeFailures(failures)

	if len(categorized) != 4 {
		t.Errorf("Expected 4 categorized failures, got %d", len(categorized))
	}

	if stats.Total != 4 {
		t.Errorf("Stats total: got %d, want 4", stats.Total)
	}

	// Check that we have different categories
	categoriesFound := make(map[FailureCategory]bool)
	for _, cat := range categorized {
		categoriesFound[cat.Category] = true
	}

	// Should have at least these categories
	expectedCategories := []FailureCategory{
		CategoryAssertionError,
		CategoryTimeout,
		CategoryPanic,
		CategoryUnknown,
	}

	for _, expected := range expectedCategories {
		if !categoriesFound[expected] {
			t.Errorf("Expected to find category %s in results", expected)
		}
	}
}

// TestCategorizeFailures_Empty tests categorizing empty failure list
func TestCategorizeFailures_Empty(t *testing.T) {
	failures := []TestFailure{}

	categorized, stats := CategorizeFailures(failures)

	if len(categorized) != 0 {
		t.Errorf("Expected 0 categorized failures, got %d", len(categorized))
	}

	if stats.Total != 0 {
		t.Errorf("Stats total: got %d, want 0", stats.Total)
	}
}

// TestGetCategoryDescription tests category descriptions
func TestGetCategoryDescription(t *testing.T) {
	testCases := []struct {
		cat         FailureCategory
		shouldExist bool
	}{
		{CategoryAssertionError, true},
		{CategoryTimeout, true},
		{CategoryPanic, true},
		{CategoryDataRace, true},
		{CategoryNilPointer, true},
		{CategoryTypeMismatch, true},
		{CategoryIndexOutOfRange, true},
		{CategoryMapKey, true},
		{CategoryChannel, true},
		{CategoryGoroutinePanic, true},
		{CategoryDeadlock, true},
		{CategoryIOError, true},
		{CategoryHTTPError, true},
		{CategoryUnknown, true},
	}

	for _, tt := range testCases {
		desc := GetCategoryDescription(tt.cat)
		if tt.shouldExist && desc == "No description available" {
			t.Errorf("Category %s should have a description", tt.cat)
		}
		if !tt.shouldExist && desc != "No description available" {
			t.Errorf("Category %s should not have a description", tt.cat)
		}
	}
}

// TestPrintCategorizationReport tests the report generation
func TestPrintCategorizationReport(t *testing.T) {
	categorized := []CategorizedFailure{
		{
			TestFailure: TestFailure{
				TestName:     "TestAssertion",
				FilePath:     "test.go",
				LineNumber:   10,
				ErrorMessage: "expected 200, got 500",
			},
			Category:   CategoryAssertionError,
			Confidence: 0.7,
			Reasoning:  "Matched pattern for category 'assertion_error'",
		},
		{
			TestFailure: TestFailure{
				TestName:     "TestTimeout",
				FilePath:     "test.go",
				LineNumber:   20,
				ErrorMessage: "context deadline exceeded",
			},
			Category:   CategoryTimeout,
			Confidence: 0.95,
			Reasoning:  "Matched pattern for category 'timeout'",
		},
	}

	stats := CategorizationStats{
		Total:         2,
		ByCategory:    map[FailureCategory]int{CategoryAssertionError: 1, CategoryTimeout: 1},
		LowConfidence: 0,
	}

	report := PrintCategorizationReport(categorized, stats)

	// Check that report contains expected sections
	expectedStrings := []string{
		"Test Failure Categorization Report",
		"Total failures: 2",
		"Failures by category:",
		"assertion_error",
		"timeout",
		"Individual failures:",
		"TestAssertion",
		"TestTimeout",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(report, expected) {
			t.Errorf("Report should contain %q", expected)
		}
	}
}

func TestPrintCategorizationReport_FormatsFallbackAsOther(t *testing.T) {
	categorized := []CategorizedFailure{{
		TestFailure: TestFailure{TestName: "TestUnknownPanic", ErrorMessage: "panic report unavailable"},
		Category:    CategoryUnknown,
		Subcategory: fallbackSubcategoryUnknownPanicMessage,
		Confidence:  ConfidenceMin,
	}}
	stats := CategorizationStats{
		Total:      1,
		ByCategory: map[FailureCategory]int{CategoryUnknown: 1},
	}

	report := PrintCategorizationReport(categorized, stats)
	if !strings.Contains(report, "Other: 1") {
		t.Errorf("report = %q, want Other category group", report)
	}
	if !strings.Contains(report, "[Other: unknown panic message]") {
		t.Errorf("report = %q, want descriptive Other label", report)
	}
}

// TestCategorizeFailure_PriorityOrder tests that higher priority patterns are checked first
func TestCategorizeFailure_PriorityOrder(t *testing.T) {
	// This error message could match both data race and assertion patterns,
	// but data race should win because it has higher priority
	errorMsg := `WARNING: DATA RACE
Write at 0x...
assertion failed: expected true, got false`

	failure := TestFailure{
		TestName:     "TestPriority",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	// Should categorize as data race (higher priority) not assertion error
	if cat.Category != CategoryDataRace {
		t.Errorf("Category: got %q, want %q (higher priority pattern should match first)", cat.Category, CategoryDataRace)
	}
	if cat.Confidence.Float64() < 0.9 {
		t.Errorf("Confidence: got %.2f, want >= 0.9", cat.Confidence)
	}
}

// TestCategorizeFailure_Subcategory tests that subcategories are set correctly
func TestCategorizeFailure_Subcategory(t *testing.T) {
	// HTTP error should have "network" subcategory
	errorMsg := "dial tcp 127.0.0.1:8080: connection refused"

	failure := TestFailure{
		TestName:     "TestHTTPSubcategory",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	if cat.Category != CategoryHTTPError {
		t.Errorf("Category: got %q, want %q", cat.Category, CategoryHTTPError)
	}
	if cat.Subcategory != "network" {
		t.Errorf("Subcategory: got %q, want 'network'", cat.Subcategory)
	}
}

// TestCategorizeFailure_CaseInsensitive tests that pattern matching is case-insensitive
func TestCategorizeFailure_CaseInsensitive(t *testing.T) {
	testCases := []string{
		"CONTEXT DEADLINE EXCEEDED",
		"Context Deadline Exceeded",
		"context deadline exceeded",
		"CoNtExT dEaDlInE eXcEeDeD",
	}

	for _, errorMsg := range testCases {
		t.Run(errorMsg, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestCaseInsensitive",
				FilePath:     "test.go",
				LineNumber:   10,
				ErrorMessage: errorMsg,
			}

			cat := CategorizeFailure(failure)

			if cat.Category != CategoryTimeout {
				t.Errorf("Category for %q: got %q, want %q", errorMsg, cat.Category, CategoryTimeout)
			}
		})
	}
}

// TestCategorizeFailure_StackTraceAndErrorMessage tests that both error message and stack trace are analyzed
func TestCategorizeFailure_StackTraceAndErrorMessage(t *testing.T) {
	// Error message doesn't contain panic, but stack trace does
	errorMsg := "test failed"
	stackTrace := `goroutine 1 [running]:
panic("something went wrong")`

	failure := TestFailure{
		TestName:     "TestStackTraceAnalysis",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
		StackTrace:   stackTrace,
	}

	cat := CategorizeFailure(failure)

	// Should categorize as panic based on stack trace
	if cat.Category != CategoryPanic && cat.Category != CategoryGoroutinePanic {
		t.Errorf("Category: got %q, want panic or goroutine_panic (should analyze stack trace)", cat.Category)
	}
}

// TestCategorizeFailure_ComprehensiveRealWorld tests categorization of real-world failures
func TestCategorizeFailure_ComprehensiveRealWorld(t *testing.T) {
	testCases := []struct {
		name        string
		failure     TestFailure
		expectedCat FailureCategory
		minConf     float64
	}{
		{
			name: "HTTP 500 error with assertion",
			failure: TestFailure{
				TestName:     "TestHTTPResponse",
				FilePath:     "api_test.go",
				LineNumber:   42,
				ErrorMessage: "expected status 200, got 500",
			},
			expectedCat: CategoryAssertionError,
			minConf:     0.5,
		},
		{
			name: "Database connection timeout",
			failure: TestFailure{
				TestName:     "TestDBConnection",
				FilePath:     "db_test.go",
				LineNumber:   15,
				ErrorMessage: "dial tcp 127.0.0.1:5432: connection timeout",
			},
			expectedCat: CategoryHTTPError,
			minConf:     0.8,
		},
		{
			name: "Map iteration with nil map",
			failure: TestFailure{
				TestName:     "TestMapIteration",
				FilePath:     "map_test.go",
				LineNumber:   23,
				ErrorMessage: "assignment to entry in nil map",
			},
			expectedCat: CategoryNilPointer,
			minConf:     0.8,
		},
		{
			name: "Concurrent map writes",
			failure: TestFailure{
				TestName:     "TestConcurrentMap",
				FilePath:     "concurrent_test.go",
				LineNumber:   56,
				ErrorMessage: "WARNING: DATA RACE\nWrite at 0x...\nconcurrent map writes",
			},
			expectedCat: CategoryDataRace,
			minConf:     0.9,
		},
		{
			name: "Type assertion on interface",
			failure: TestFailure{
				TestName:     "TestTypeAssertion",
				FilePath:     "types_test.go",
				LineNumber:   78,
				ErrorMessage: "interface conversion: interface {} is string, not int",
			},
			expectedCat: CategoryTypeMismatch,
			minConf:     0.8,
		},
		{
			name: "Slice index out of bounds",
			failure: TestFailure{
				TestName:     "TestSliceAccess",
				FilePath:     "slice_test.go",
				LineNumber:   91,
				ErrorMessage: "panic: runtime error: index out of range [10] with length 5",
			},
			expectedCat: CategoryIndexOutOfRange,
			minConf:     0.9,
		},
		{
			name: "Channel close race",
			failure: TestFailure{
				TestName:     "TestChannelClose",
				FilePath:     "channel_test.go",
				LineNumber:   33,
				ErrorMessage: "panic: send on closed channel",
			},
			expectedCat: CategoryChannel,
			minConf:     0.9,
		},
		{
			name: "Test context timeout",
			failure: TestFailure{
				TestName:     "TestLongOperation",
				FilePath:     "timeout_test.go",
				LineNumber:   67,
				ErrorMessage: "context deadline exceeded after 30s timeout",
			},
			expectedCat: CategoryTimeout,
			minConf:     0.9,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			cat := CategorizeFailure(tt.failure)

			if cat.Category != tt.expectedCat {
				t.Errorf("Category: got %q, want %q", cat.Category, tt.expectedCat)
			}
			if cat.Confidence.Float64() < tt.minConf {
				t.Errorf("Confidence: got %.2f, want >= %.2f", cat.Confidence, tt.minConf)
			}
			if cat.Reasoning == "" {
				t.Error("Reasoning should not be empty")
			}
		})
	}
}

func TestCategorizeFailure_Contract(t *testing.T) {
	tests := []struct {
		name            string
		failure         Failure
		wantType        FailureCategory
		wantSubcategory string
	}{
		{
			name:     "assertion error",
			failure:  Failure{ErrorMessage: "assertion failed: expected 200, got 500"},
			wantType: CategoryAssertionError,
		},
		{
			name:            "timeout",
			failure:         Failure{ErrorMessage: "context deadline exceeded"},
			wantType:        CategoryTimeout,
			wantSubcategory: "deadline_exceeded",
		},
		{
			name:            "explicit panic takes priority over nil pointer",
			failure:         Failure{ErrorMessage: "panic: runtime error: nil pointer dereference"},
			wantType:        CategoryPanic,
			wantSubcategory: "runtime_panic",
		},
		{
			name:            "unrecognized failure is other",
			failure:         Failure{ErrorMessage: "florp zibble 42"},
			wantType:        CategoryUnknown,
			wantSubcategory: fallbackSubcategoryUnclassified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Category = CategorizeFailure(tt.failure)

			if got.Type != tt.wantType {
				t.Fatalf("Type = %q, want %q", got.Type, tt.wantType)
			}
			if got.Category != got.Type {
				t.Fatalf("Category = %q, want compatibility value %q", got.Category, got.Type)
			}
			if got.Subcategory != tt.wantSubcategory {
				t.Fatalf("Subcategory = %q, want %q", got.Subcategory, tt.wantSubcategory)
			}
			if tt.wantType == CategoryPanic && !strings.Contains(got.Reasoning, "nil_pointer_dereference") {
				t.Fatalf("Reasoning = %q, want the overridden nil-pointer match", got.Reasoning)
			}
		})
	}
}
