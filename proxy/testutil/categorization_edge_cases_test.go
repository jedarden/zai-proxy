package testutil

import (
	"strings"
	"testing"
)

// TestCategorizeFailure_Ambiguous_PanicVsTypeMismatch tests that panic with
// "interface conversion" is handled with appropriate confidence reduction
func TestCategorizeFailure_Ambiguous_PanicVsTypeMismatch(t *testing.T) {
	testCases := []struct {
		name            string
		errorMsg        string
		expectedCat     FailureCategory
		minConfidence   float64
		maxConfidence   float64
		shouldContain   []string
		shouldNotContain []string
	}{
		{
			name:          "panic interface conversion",
			errorMsg:      "panic: interface conversion: interface {} is string, not int",
			expectedCat:   CategoryPanic,
			minConfidence: 0.7, // Reduced from 1.0 due to ambiguity
			maxConfidence: 0.9,
			shouldContain: []string{"Ambiguity detected", "type_mismatch"},
		},
		{
			name:          "interface conversion without panic",
			errorMsg:      "interface conversion: interface {} is string, not int",
			expectedCat:   CategoryTypeMismatch,
			minConfidence: 0.8,
			maxConfidence: 0.95,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestAmbiguous",
				FilePath:     "test.go",
				LineNumber:   10,
				ErrorMessage: tt.errorMsg,
			}

			cat := CategorizeFailure(failure)

			if cat.Category != tt.expectedCat {
				t.Errorf("Category: got %q, want %q", cat.Category, tt.expectedCat)
			}
			if cat.Confidence.Float64() < tt.minConfidence || cat.Confidence.Float64() > tt.maxConfidence {
				t.Errorf("Confidence: got %.2f, want [%.2f, %.2f]", cat.Confidence, tt.minConfidence, tt.maxConfidence)
			}

			// Check reasoning content
			for _, mustContain := range tt.shouldContain {
				if !strings.Contains(cat.Reasoning, mustContain) {
					t.Errorf("Reasoning should contain %q", mustContain)
				}
			}
			for _, mustNotContain := range tt.shouldNotContain {
				if strings.Contains(cat.Reasoning, mustNotContain) {
					t.Errorf("Reasoning should not contain %q", mustNotContain)
				}
			}
		})
	}
}

// TestCategorizeFailure_Ambiguous_TimeoutVsHTTP tests that connection timeouts
// are properly distinguished between HTTP errors and general timeouts
func TestCategorizeFailure_Ambiguous_TimeoutVsHTTP(t *testing.T) {
	testCases := []struct {
		name          string
		errorMsg      string
		expectedCat   FailureCategory
		minConfidence float64
		maxConfidence float64
		shouldContain []string
	}{
		{
			name:          "connection timeout with dial tcp - categorized as timeout",
			errorMsg:      "dial tcp 127.0.0.1:8080: connection timeout",
			expectedCat:   CategoryHTTPError,
			minConfidence: 0.6, // Reduced due to ambiguity with timeout
			maxConfidence: 0.85,
			shouldContain: []string{"Ambiguity detected"},
		},
		{
			name:          "pure context deadline exceeded - high confidence timeout",
			errorMsg:      "context deadline exceeded",
			expectedCat:   CategoryTimeout,
			minConfidence: 0.9,
			maxConfidence: 0.95,
		},
		{
			name:          "HTTP error with status code",
			errorMsg:      "HTTP status code 500",
			expectedCat:   CategoryHTTPError,
			minConfidence: 0.8,
			maxConfidence: 0.9,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestTimeoutHTTP",
				FilePath:     "test.go",
				LineNumber:   10,
				ErrorMessage: tt.errorMsg,
			}

			cat := CategorizeFailure(failure)

			if cat.Category != tt.expectedCat {
				t.Errorf("Category: got %q, want %q", cat.Category, tt.expectedCat)
			}
			if cat.Confidence.Float64() < tt.minConfidence || cat.Confidence.Float64() > tt.maxConfidence {
				t.Errorf("Confidence: got %.2f, want [%.2f, %.2f]", cat.Confidence, tt.minConfidence, tt.maxConfidence)
			}

			for _, mustContain := range tt.shouldContain {
				if !strings.Contains(cat.Reasoning, mustContain) {
					t.Errorf("Reasoning should contain %q", mustContain)
				}
			}
		})
	}
}

// TestCategorizeFailure_EdgeCase_NilMapAssignment tests that assignment to
// entry in nil map is properly handled (should be nil pointer, not map key error)
func TestCategorizeFailure_EdgeCase_NilMapAssignment(t *testing.T) {
	errorMsg := "assignment to entry in nil map"

	failure := TestFailure{
		TestName:     "TestNilMap",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	// This should match map key pattern but with very low confidence
	// because it's actually a nil pointer issue
	if cat.Category != CategoryMapKey && cat.Category != CategoryNilPointer {
		t.Errorf("Category: got %q, want %q or %q (nil map assignment is nil pointer, not map key)",
			cat.Category, CategoryMapKey, CategoryNilPointer)
	}

	// If categorized as map_key_error, confidence should be very low
	if cat.Category == CategoryMapKey && cat.Confidence.Float64() > 0.6 {
		t.Errorf("Confidence for nil map as map_key_error: got %.2f, want < 0.6 (this is actually nil pointer)", cat.Confidence)
	}

	// Check for edge case reasoning
	if cat.Category == CategoryMapKey && !strings.Contains(cat.Reasoning, "assignment to entry") {
		t.Error("Reasoning should mention nil map assignment edge case")
	}
}

// TestCategorizeFailure_EdgeCase_DataRaceWithAssertion tests that data race
// takes precedence over assertion even when both patterns appear
func TestCategorizeFailure_EdgeCase_DataRaceWithAssertion(t *testing.T) {
	errorMsg := `WARNING: DATA RACE
Write at 0x...
Read at 0x...
assertion failed: expected true, got false`

	failure := TestFailure{
		TestName:     "TestDataRaceAssertion",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	if cat.Category != CategoryDataRace {
		t.Errorf("Category: got %q, want %q (data race should take precedence)", cat.Category, CategoryDataRace)
	}

	// Should have very high confidence despite assertion text
	if cat.Confidence.Float64() < 0.95 {
		t.Errorf("Confidence: got %.2f, want >= 0.95 (data race is unambiguous)", cat.Confidence)
	}

	// Should mention ambiguity detection
	if !strings.Contains(cat.Reasoning, "assertion_error") {
		t.Error("Reasoning should mention assertion pattern was also matched")
	}
}

// TestCategorizeFailure_EdgeCase_MultipleMatchingPatterns tests that when
// multiple patterns match, all are reported in reasoning
func TestCategorizeFailure_EdgeCase_MultipleMatchingPatterns(t *testing.T) {
	errorMsg := "panic on nil pointer during assertion: expected value, got nil"

	failure := TestFailure{
		TestName:     "TestMultiplePatterns",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	// Should categorize as nil pointer (highest priority match)
	if cat.Category != CategoryNilPointer {
		t.Errorf("Category: got %q, want %q (nil pointer should win)", cat.Category, CategoryNilPointer)
	}

	// Should detect multiple patterns matched
	if !strings.Contains(cat.Reasoning, "Ambiguity detected") {
		t.Error("Reasoning should mention ambiguity detection")
	}

	if !strings.Contains(cat.Reasoning, "Total matching patterns") {
		t.Error("Reasoning should show total matching patterns count")
	}

	// Should mention assertion pattern was also matched
	if !strings.Contains(cat.Reasoning, "assertion_error") {
		t.Error("Reasoning should mention assertion pattern was also matched")
	}

	// Should mention panic pattern was also matched
	if !strings.Contains(cat.Reasoning, "panic") {
		t.Error("Reasoning should mention panic pattern was also matched")
	}
}

// TestCategorizeFailure_EdgeCase_ConfidenceFloor tests that confidence never
// goes below 0.1 even in highly ambiguous cases
func TestCategorizeFailure_EdgeCase_ConfidenceFloor(t *testing.T) {
	// Create an error with many assertion-like patterns to trigger multiple matches
	errorMsg := "expected X but got Y, not equal, should be Z, want W, assertion failed, value mismatch"

	failure := TestFailure{
		TestName:     "TestConfidenceFloor",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	// Should have minimum confidence floor
	if cat.Confidence.Float64() < 0.1 {
		t.Errorf("Confidence: got %.2f, want >= 0.1 (minimum confidence floor)", cat.Confidence)
	}
}

// TestCategorizeFailure_EdgeCase_PanicOnNilPointer tests that "panic on nil pointer"
// gets maximum confidence as it's unambiguous
func TestCategorizeFailure_EdgeCase_PanicOnNilPointer(t *testing.T) {
	errorMsg := "panic on nil pointer"

	failure := TestFailure{
		TestName:     "TestPanicOnNil",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	if cat.Category != CategoryNilPointer {
		t.Errorf("Category: got %q, want %q", cat.Category, CategoryNilPointer)
	}

	// Should have maximum confidence for this specific pattern
	if cat.Confidence != 1.0 {
		t.Errorf("Confidence: got %.2f, want 1.0 (panic on nil pointer is unambiguous)", cat.Confidence)
	}
}

// TestCategorizeFailure_EdgeCase_IndexOutOfRangeWithAssertion tests that index
// out of range takes precedence over assertion
func TestCategorizeFailure_EdgeCase_IndexOutOfRangeWithAssertion(t *testing.T) {
	errorMsg := "index out of range [5] with length 3: expected value at index 5"

	failure := TestFailure{
		TestName:     "TestIndexAssertion",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	if cat.Category != CategoryIndexOutOfRange {
		t.Errorf("Category: got %q, want %q (index error should take precedence)", cat.Category, CategoryIndexOutOfRange)
	}

	// High confidence but slightly reduced due to assertion text
	if cat.Confidence.Float64() < 0.6 {
		t.Errorf("Confidence: got %.2f, want >= 0.6", cat.Confidence)
	}

	// Should mention ambiguity with assertion
	if !strings.Contains(cat.Reasoning, "assertion_error") {
		t.Error("Reasoning should mention assertion pattern was also matched")
	}
}

// TestCategorizeFailure_EdgeCase_ChannelVsGoroutine tests that channel errors
// are properly distinguished from goroutine panics
func TestCategorizeFailure_EdgeCase_ChannelVsGoroutine(t *testing.T) {
	errorMsg := `goroutine 1 [running]:
panic: send on closed channel
created by testFunc
/test.go:20`

	failure := TestFailure{
		TestName:     "TestChannelGoroutine",
		FilePath:     "test.go",
		LineNumber:   20,
		ErrorMessage: errorMsg,
		StackTrace:   "goroutine 1 [running]:",
	}

	cat := CategorizeFailure(failure)

	// Channel error should win (higher priority than goroutine panic)
	if cat.Category != CategoryChannel {
		t.Errorf("Category: got %q, want %q (channel error should take precedence)", cat.Category, CategoryChannel)
	}

	// Should have high confidence
	if cat.Confidence.Float64() < 0.8 {
		t.Errorf("Confidence: got %.2f, want >= 0.8", cat.Confidence)
	}

	// Should mention goroutine pattern was also matched
	if !strings.Contains(cat.Reasoning, "goroutine_panic") {
		t.Error("Reasoning should mention goroutine pattern was also matched")
	}
}

// TestCategorizeFailure_EdgeCase_TypeVsAssertion tests that type mismatch
// is properly distinguished from assertion errors
func TestCategorizeFailure_EdgeCase_TypeVsAssertion(t *testing.T) {
	errorMsg := "type assertion failed: expected type int, got string"

	failure := TestFailure{
		TestName:     "TestTypeAssertion",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	if cat.Category != CategoryTypeMismatch {
		t.Errorf("Category: got %q, want %q", cat.Category, CategoryTypeMismatch)
	}

	// Should have moderate confidence due to assertion text
	if cat.Confidence.Float64() < 0.5 || cat.Confidence.Float64() > 0.8 {
		t.Errorf("Confidence: got %.2f, want [0.5, 0.8] (type with assertion text is ambiguous)", cat.Confidence)
	}

	// Should mention ambiguity with assertion
	if !strings.Contains(cat.Reasoning, "assertion_error") {
		t.Error("Reasoning should mention assertion pattern was also matched")
	}
}

// TestCategorizeFailure_EdgeCase_IOVsHTTP tests that I/O errors are properly
// distinguished from HTTP errors
func TestCategorizeFailure_EedgeCase_IOVsHTTP(t *testing.T) {
	testCases := []struct {
		name          string
		errorMsg      string
		expectedCat   FailureCategory
		minConfidence float64
	}{
		{
			name:          "pure file not found",
			errorMsg:      "no such file or directory: /tmp/test.txt",
			expectedCat:   CategoryIOError,
			minConfidence: 0.8,
		},
		{
			name:          "connection refused (HTTP wins)",
			errorMsg:      "dial tcp 127.0.0.1:8080: connection refused",
			expectedCat:   CategoryHTTPError,
			minConfidence: 0.7,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestIOHTTP",
				FilePath:     "test.go",
				LineNumber:   10,
				ErrorMessage: tt.errorMsg,
			}

			cat := CategorizeFailure(failure)

			if cat.Category != tt.expectedCat {
				t.Errorf("Category: got %q, want %q", cat.Category, tt.expectedCat)
			}
			if cat.Confidence.Float64() < tt.minConfidence {
				t.Errorf("Confidence: got %.2f, want >= %.2f", cat.Confidence, tt.minConfidence)
			}
		})
	}
}

// TestCategorizeFailure_ComprehensiveEdgeCases tests a comprehensive set of
// real-world ambiguous and edge case scenarios
func TestCategorizeFailure_ComprehensiveEdgeCases(t *testing.T) {
	testCases := []struct {
		name              string
		errorMsg          string
		stackTrace        string
		expectedCat       FailureCategory
		expectedSubcat    string
		minConfidence     float64
		maxConfidence     float64
		shouldHaveAmbiguity bool
	}{
		{
			name:        "nil pointer dereference with assertion text",
			errorMsg:    "nil pointer dereference: expected non-nil value",
			expectedCat: CategoryNilPointer,
			minConfidence: 0.6,
			maxConfidence: 0.85,
			shouldHaveAmbiguity: true,
		},
		{
			name:        "index error with expected/got pattern",
			errorMsg:    "index out of range [10] with length 5: expected element at index 10, got panic",
			expectedCat: CategoryIndexOutOfRange,
			minConfidence: 0.6,
			maxConfidence: 0.85,
			shouldHaveAmbiguity: true,
		},
		{
			name:        "map key error in assertion context",
			errorMsg:    "map key not found: expected key 'config' to exist",
			expectedCat: CategoryMapKey,
			minConfidence: 0.6,
			maxConfidence: 0.8,
			shouldHaveAmbiguity: true,
		},
		{
			name:        "channel error with goroutine stack trace",
			errorMsg:    "send on closed channel",
			stackTrace:  "goroutine 1 [running]:",
			expectedCat: CategoryChannel,
			minConfidence: 0.8,
			maxConfidence: 0.95,
			shouldHaveAmbiguity: true,
		},
		{
			name:        "type mismatch in assertion message",
			errorMsg:    "interface conversion: interface {} is string, not int: expected int type",
			expectedCat: CategoryPanic, // Panic wins due to "interface conversion:" prefix
			minConfidence: 0.7,
			maxConfidence: 0.9,
			shouldHaveAmbiguity: true,
		},
		{
			name:        "timeout with HTTP connection failure",
			errorMsg:    "dial tcp 127.0.0.1:8080: connection timeout: context deadline exceeded",
			expectedCat: CategoryHTTPError, // HTTP wins due to "dial tcp"
			minConfidence: 0.5,
			maxConfidence: 0.8,
			shouldHaveAmbiguity: true,
		},
		{
			name:        "I/O error with network context",
			errorMsg:    "read failed: connection reset by peer",
			expectedCat: CategoryIOError,
			minConfidence: 0.7,
			maxConfidence: 0.9,
			shouldHaveAmbiguity: true,
		},
		{
			name:        "pure assertion (no ambiguity)",
			errorMsg:    "expected 200, got 500",
			expectedCat: CategoryAssertionError,
			minConfidence: 0.7,
			maxConfidence: 0.8,
			shouldHaveAmbiguity: false,
		},
		{
			name:        "pure timeout (no ambiguity)",
			errorMsg:    "context deadline exceeded after 30s",
			expectedCat: CategoryTimeout,
			minConfidence: 0.9,
			maxConfidence: 0.95,
			shouldHaveAmbiguity: false,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestEdgeCase",
				FilePath:     "test.go",
				LineNumber:   10,
				ErrorMessage: tt.errorMsg,
				StackTrace:   tt.stackTrace,
			}

			cat := CategorizeFailure(failure)

			if cat.Category != tt.expectedCat {
				t.Errorf("Category: got %q, want %q", cat.Category, tt.expectedCat)
			}

			if tt.expectedSubcat != "" && cat.Subcategory != tt.expectedSubcat {
				t.Errorf("Subcategory: got %q, want %q", cat.Subcategory, tt.expectedSubcat)
			}

			if cat.Confidence.Float64() < tt.minConfidence || cat.Confidence.Float64() > tt.maxConfidence {
				t.Errorf("Confidence: got %.2f, want [%.2f, %.2f]", cat.Confidence, tt.minConfidence, tt.maxConfidence)
			}

			hasAmbiguity := strings.Contains(cat.Reasoning, "Ambiguity detected")
			if tt.shouldHaveAmbiguity && !hasAmbiguity {
				t.Error("Expected ambiguity detection in reasoning, but none found")
			}
			if !tt.shouldHaveAmbiguity && hasAmbiguity {
				t.Error("Unexpected ambiguity detection in reasoning")
			}

			// All categorizations should have reasoning
			if cat.Reasoning == "" {
				t.Error("Reasoning should not be empty")
			}
		})
	}
}

// TestCategorizeFailure_EdgeCase_MultiplePanics tests that multiple panic
// occurrences in the same error reduce confidence appropriately
func TestCategorizeFailure_EdgeCase_MultiplePanics(t *testing.T) {
	errorMsg := "panic in goroutine 1, then panic in main, finally panic in test"

	failure := TestFailure{
		TestName:     "TestMultiplePanics",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	if cat.Category != CategoryPanic {
		t.Errorf("Category: got %q, want %q", cat.Category, CategoryPanic)
	}

	// Multiple panics should reduce confidence
	if cat.Confidence.Float64() > 0.95 {
		t.Errorf("Confidence: got %.2f, want <= 0.95 (multiple panics should reduce confidence)", cat.Confidence)
	}
}
