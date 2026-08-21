package testutil

import (
	"strings"
	"testing"
)

// TestCategorizeFailure_EdgeCase_ContextCancellation tests that context cancellation
// is distinguished from general timeout
func TestCategorizeFailure_EdgeCase_ContextCancellation(t *testing.T) {
	errorMsg := "context canceled"

	failure := TestFailure{
		TestName:     "TestContextCancel",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	if cat.Category != CategoryTimeout {
		t.Errorf("Category: got %q, want %q", cat.Category, CategoryTimeout)
	}

	// Context cancellation should have maximum confidence
	if cat.Confidence != 1.0 {
		t.Errorf("Confidence: got %.2f, want 1.0 (context cancellation is unambiguous)", cat.Confidence)
	}
}

// TestCategorizeFailure_EdgeCase_CloseOfClosedChannel tests that close of closed
// channel error is properly handled with confidence adjustment
func TestCategorizeFailure_EdgeCase_CloseOfClosedChannel(t *testing.T) {
	errorMsg := "close of closed channel"

	failure := TestFailure{
		TestName:     "TestChannelClose",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	if cat.Category != CategoryChannel {
		t.Errorf("Category: got %q, want %q", cat.Category, CategoryChannel)
	}

	// Should have high but slightly reduced confidence due to race condition implication
	if cat.Confidence < 0.8 || cat.Confidence > 0.95 {
		t.Errorf("Confidence: got %.2f, want [0.8, 0.95]", cat.Confidence)
	}
}

// TestCategorizeFailure_EdgeCase_NilPointerInTestContext tests that nil pointer
// errors in test context get appropriate categorization
func TestCategorizeFailure_EdgeCase_NilPointerInTestContext(t *testing.T) {
	errorMsg := "nil pointer dereference in test setup: mock not initialized"

	failure := TestFailure{
		TestName:     "TestMockSetup",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	if cat.Category != CategoryNilPointer {
		t.Errorf("Category: got %q, want %q", cat.Category, CategoryNilPointer)
	}

	// Should have moderate confidence due to test context
	if cat.Confidence < 0.7 || cat.Confidence > 0.9 {
		t.Errorf("Confidence: got %.2f, want [0.7, 0.9]", cat.Confidence)
	}
}

// TestCategorizeFailure_EdgeCase_SafeTypeAssertion tests that safe type assertion
// (with ok pattern) gets maximum confidence
func TestCategorizeFailure_EdgeCase_SafeTypeAssertion(t *testing.T) {
	errorMsg := "type assertion failed in safe check: value.(int), ok = false"

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

	// Safe type assertion should have maximum confidence
	if cat.Confidence != 1.0 {
		t.Errorf("Confidence: got %.2f, want 1.0 (safe type assertion is unambiguous)", cat.Confidence)
	}
}

// TestCategorizeFailure_EdgeCase_SliceBoundsError tests that slice bounds error
// gets maximum confidence
func TestCategorizeFailure_EdgeCase_SliceBoundsError(t *testing.T) {
	errorMsg := "slice bounds out of range [10:15] with length 5"

	failure := TestFailure{
		TestName:     "TestSliceBounds",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	if cat.Category != CategoryIndexOutOfRange {
		t.Errorf("Category: got %q, want %q", cat.Category, CategoryIndexOutOfRange)
	}

	// Slice bounds error should have maximum confidence
	if cat.Confidence != 1.0 {
		t.Errorf("Confidence: got %.2f, want 1.0 (slice bounds is unambiguous)", cat.Confidence)
	}
}

// TestCategorizeFailure_EdgeCase_MultipleGoroutinesPanic tests that multiple
// goroutine panics reduce confidence appropriately
func TestCategorizeFailure_EdgeCase_MultipleGoroutinesPanic(t *testing.T) {
	errorMsg := `goroutine 1 [running]:
goroutine 2 [running]:
goroutine 3 [running]:
goroutine 4 [running]:
panic in multiple goroutines detected`

	failure := TestFailure{
		TestName:     "TestMultipleGoroutines",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	if cat.Category != CategoryGoroutinePanic {
		t.Errorf("Category: got %q, want %q", cat.Category, CategoryGoroutinePanic)
	}

	// Multiple goroutines should reduce confidence
	if cat.Confidence < 0.7 || cat.Confidence > 0.85 {
		t.Errorf("Confidence: got %.2f, want [0.7, 0.85]", cat.Confidence)
	}
}

// TestCategorizeFailure_EdgeCase_ChannelDeadlock tests that channel deadlock
// gets maximum confidence
func TestCategorizeFailure_EdgeCase_ChannelDeadlock(t *testing.T) {
	errorMsg := "potential deadlock detected in channel operation"

	failure := TestFailure{
		TestName:     "TestChannelDeadlock",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	if cat.Category != CategoryDeadlock {
		t.Errorf("Category: got %q, want %q", cat.Category, CategoryDeadlock)
	}

	// Channel deadlock should have maximum confidence
	if cat.Confidence != 1.0 {
		t.Errorf("Confidence: got %.2f, want 1.0 (channel deadlock is unambiguous)", cat.Confidence)
	}
}

// TestCategorizeFailure_EdgeCase_BrokenPipe tests that broken pipe error
// is properly categorized with confidence adjustment
func TestCategorizeFailure_EdgeCase_BrokenPipe(t *testing.T) {
	errorMsg := "write failed: broken pipe"

	failure := TestFailure{
		TestName:     "TestBrokenPipe",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	if cat.Category != CategoryIOError {
		t.Errorf("Category: got %q, want %q", cat.Category, CategoryIOError)
	}

	// Broken pipe should have reduced confidence due to connection ambiguity
	if cat.Confidence < 0.7 || cat.Confidence > 0.9 {
		t.Errorf("Confidence: got %.2f, want [0.7, 0.9]", cat.Confidence)
	}
}

// TestCategorizeFailure_EdgeCase_HTTPStatusInAssertion tests that HTTP status
// code in assertion context gets appropriate categorization
func TestCategorizeFailure_EdgeCase_HTTPStatusInAssertion(t *testing.T) {
	errorMsg := "expected status code 200, got 500 Internal Server Error"

	failure := TestFailure{
		TestName:     "TestHTTPStatus",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	if cat.Category != CategoryHTTPError && cat.Category != CategoryAssertionError {
		t.Errorf("Category: got %q, want %q or %q", cat.Category, CategoryHTTPError, CategoryAssertionError)
	}

	// Should have moderate confidence due to assertion ambiguity
	if cat.Confidence < 0.5 || cat.Confidence > 0.8 {
		t.Errorf("Confidence: got %.2f, want [0.5, 0.8]", cat.Confidence)
	}
}

// TestCategorizeFailure_EdgeCase_ZeroMapKey tests that zero map key error
// gets maximum confidence
func TestCategorizeFailure_EdgeCase_ZeroMapKey(t *testing.T) {
	errorMsg := "zero map key in map access"

	failure := TestFailure{
		TestName:     "TestZeroMapKey",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	if cat.Category != CategoryMapKey {
		t.Errorf("Category: got %q, want %q", cat.Category, CategoryMapKey)
	}

	// Zero map key should have maximum confidence
	if cat.Confidence != 1.0 {
		t.Errorf("Confidence: got %.2f, want 1.0 (zero map key is unambiguous)", cat.Confidence)
	}
}

// TestCategorizeFailure_EdgeCase_ShortUnknownMessage tests that very short
// unknown messages get very low confidence
func TestCategorizeFailure_EdgeCase_ShortUnknownMessage(t *testing.T) {
	errorMsg := "weird error"

	failure := TestFailure{
		TestName:     "TestShortUnknown",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	cat := CategorizeFailure(failure)

	if cat.Category != CategoryUnknown {
		t.Errorf("Category: got %q, want %q", cat.Category, CategoryUnknown)
	}

	// Short unknown message should have very low confidence
	if cat.Confidence != 0.05 {
		t.Errorf("Confidence: got %.2f, want 0.05 (short unknown message)", cat.Confidence)
	}
}

// TestGetUncertainFailures tests filtering failures by confidence threshold
func TestGetUncertainFailures(t *testing.T) {
	categorized := []CategorizedFailure{
		{
			TestFailure: TestFailure{TestName: "HighConfidence"},
			Category:    CategoryAssertionError,
			Confidence:  0.9,
		},
		{
			TestFailure: TestFailure{TestName: "ModerateConfidence"},
			Category:    CategoryTimeout,
			Confidence:  0.7,
		},
		{
			TestFailure: TestFailure{TestName: "LowConfidence"},
			Category:    CategoryPanic,
			Confidence:  0.4,
		},
		{
			TestFailure: TestFailure{TestName: "VeryLowConfidence"},
			Category:    CategoryUnknown,
			Confidence:  0.1,
		},
	}

	// Test default threshold (0.7)
	uncertain := GetUncertainFailures(categorized)
	if len(uncertain) != 2 {
		t.Errorf("GetUncertainFailures() returned %d items, want 2", len(uncertain))
	}

	// Test custom threshold (0.5)
	uncertain = GetUncertainFailures(categorized, 0.5)
	if len(uncertain) != 2 {
		t.Errorf("GetUncertainFailures(threshold=0.5) returned %d items, want 2", len(uncertain))
	}
}

// TestGetMatchingCategoriesForFailure tests extracting all matching categories
func TestGetMatchingCategoriesForFailure(t *testing.T) {
	failure := TestFailure{
		TestName:     "TestMultiplePatterns",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: "expected 200, got 500",
	}

	matched := GetMatchingCategoriesForFailure(failure)

	// Should match assertion error at minimum
	if len(matched) == 0 {
		t.Error("GetMatchingCategoriesForFailure() returned no matches, expected at least 1")
	}

	// Check that assertion_error is in the matches
	found := false
	for _, cat := range matched {
		if cat == CategoryAssertionError {
			found = true
			break
		}
	}
	if !found {
		t.Error("Assertion error should be in matching categories")
	}
}

// TestGetConfidenceLevel tests human-readable confidence levels
func TestGetConfidenceLevel(t *testing.T) {
	testCases := []struct {
		confidence    float64
		expectedLevel string
	}{
		{1.0, "Very High"},
		{0.95, "Very High"},
		{0.9, "High"},
		{0.8, "High"},
		{0.7, "Moderate"},
		{0.6, "Moderate"},
		{0.5, "Low"},
		{0.4, "Low"},
		{0.3, "Very Low"},
		{0.2, "Very Low"},
		{0.1, "Uncertain"},
		{0.0, "Uncertain"},
	}

	for _, tt := range testCases {
		t.Run(tt.expectedLevel, func(t *testing.T) {
			level := GetConfidenceLevel(tt.confidence)
			if level != tt.expectedLevel {
				t.Errorf("GetConfidenceLevel(%.2f) = %q, want %q", tt.confidence, level, tt.expectedLevel)
			}
		})
	}
}

// TestNeedsManualReview tests the manual review detection logic
func TestNeedsManualReview(t *testing.T) {
	testCases := []struct {
		name        string
		confidence  float64
		category    FailureCategory
		needsReview bool
	}{
		{"high confidence assertion", 0.9, CategoryAssertionError, false},
		{"low confidence assertion", 0.4, CategoryAssertionError, true},
		{"unknown category", 0.8, CategoryUnknown, true},
		{"moderate confidence timeout", 0.7, CategoryTimeout, false},
		{"borderline confidence", 0.5, CategoryPanic, true},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			cat := CategorizedFailure{
				TestFailure: TestFailure{TestName: "TestReview"},
				Category:    tt.category,
				Confidence:  NewConfidence(tt.confidence),
			}

			needsReview := NeedsManualReview(cat)
			if needsReview != tt.needsReview {
				t.Errorf("NeedsManualReview() = %v, want %v", needsReview, tt.needsReview)
			}
		})
	}
}

// TestGetSuggestedSubcategory tests subcategory suggestion logic
func TestGetSuggestedSubcategory(t *testing.T) {
	testCases := []struct {
		name           string
		errorMsg       string
		category       FailureCategory
		expectedSubcat string
	}{
		{
			name:           "HTTP timeout",
			errorMsg:       "dial tcp: connection timeout",
			category:       CategoryHTTPError,
			expectedSubcat: "timeout",
		},
		{
			name:           "HTTP connection refused",
			errorMsg:       "dial tcp 127.0.0.1:8080: connection refused",
			category:       CategoryHTTPError,
			expectedSubcat: "connection",
		},
		{
			name:           "HTTP status code",
			errorMsg:       "HTTP status code 500",
			category:       CategoryHTTPError,
			expectedSubcat: "status",
		},
		{
			name:           "I/O permission denied",
			errorMsg:       "permission denied reading file",
			category:       CategoryIOError,
			expectedSubcat: "permission",
		},
		{
			name:           "I/O file not found",
			errorMsg:       "no such file or directory",
			category:       CategoryIOError,
			expectedSubcat: "not_found",
		},
		{
			name:           "Panic nil pointer",
			errorMsg:       "panic: nil pointer dereference",
			category:       CategoryPanic,
			expectedSubcat: "nil_pointer",
		},
		{
			name:           "Panic index bounds",
			errorMsg:       "panic: index out of range",
			category:       CategoryPanic,
			expectedSubcat: "bounds",
		},
		{
			name:           "Timeout context",
			errorMsg:       "context deadline exceeded",
			category:       CategoryTimeout,
			expectedSubcat: "context",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			cat := CategorizedFailure{
				TestFailure: TestFailure{
					ErrorMessage: tt.errorMsg,
				},
				Category: tt.category,
			}

			subcat := GetSuggestedSubcategory(cat)
			if subcat != tt.expectedSubcat {
				t.Errorf("GetSuggestedSubcategory() = %q, want %q", subcat, tt.expectedSubcat)
			}
		})
	}
}

// TestResolveAmbiguity tests the ambiguity resolution logic
func TestResolveAmbiguity(t *testing.T) {
	testCases := []struct {
		name             string
		errorMsg         string
		initialCategory  FailureCategory
		expectedCategory FailureCategory
		checkReasoning   string
	}{
		{
			name:             "timeout with dial tcp",
			errorMsg:         "dial tcp 127.0.0.1:8080: connection timeout",
			initialCategory:  CategoryTimeout,
			expectedCategory: CategoryHTTPError,
			checkReasoning:   "Ambiguity resolution",
		},
		{
			name:             "interface conversion without panic",
			errorMsg:         "interface conversion: interface {} is string, not int",
			initialCategory:  CategoryPanic,
			expectedCategory: CategoryTypeMismatch,
			checkReasoning:   "Ambiguity resolution",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			cat := CategorizedFailure{
				TestFailure: TestFailure{
					ErrorMessage: tt.errorMsg,
					StackTrace:   "",
				},
				Category:   tt.initialCategory,
				Confidence: 0.6,
				Reasoning:  "Ambiguity detected: multiple patterns matched",
			}

			// First verify it's ambiguous
			if !IsAmbiguous(cat) {
				// Make it ambiguous for testing
				cat.Reasoning = "Ambiguity detected: multiple patterns matched"
			}

			resolved := ResolveAmbiguity(cat)

			if resolved.Category != tt.expectedCategory {
				t.Errorf("ResolveAmbiguity() category = %q, want %q", resolved.Category, tt.expectedCategory)
			}

			if !strings.Contains(resolved.Reasoning, tt.checkReasoning) {
				t.Errorf("ResolveAmbiguity() reasoning should contain %q", tt.checkReasoning)
			}
		})
	}
}

// TestCategorizeFailure_ComprehensiveAdvancedEdgeCases tests a comprehensive set
// of advanced edge cases with detailed validation
func TestCategorizeFailure_ComprehensiveAdvancedEdgeCases(t *testing.T) {
	testCases := []struct {
		name                string
		errorMsg            string
		stackTrace          string
		expectedCat         FailureCategory
		minConfidence       float64
		maxConfidence       float64
		shouldHaveAmbiguity bool
		expectedSubcat      string
	}{
		{
			name:                "context cancellation in timeout test",
			errorMsg:            "context canceled during test execution",
			expectedCat:         CategoryTimeout,
			minConfidence:       0.95,
			maxConfidence:       1.0,
			shouldHaveAmbiguity: false,
		},
		{
			name:                "close of closed channel in concurrent test",
			errorMsg:            "close of closed channel in goroutine 2",
			stackTrace:          "goroutine 2 [running]:",
			expectedCat:         CategoryChannel,
			minConfidence:       0.8,
			maxConfidence:       0.95,
			shouldHaveAmbiguity: true,
		},
		{
			name:                "nil pointer in mock setup",
			errorMsg:            "nil pointer dereference in test mock setup",
			expectedCat:         CategoryNilPointer,
			minConfidence:       0.7,
			maxConfidence:       0.9,
			shouldHaveAmbiguity: false,
			expectedSubcat:      "test_setup",
		},
		{
			name:                "safe type assertion failure",
			errorMsg:            "type assertion failed in safe check: value.(int), ok = false",
			expectedCat:         CategoryTypeMismatch,
			minConfidence:       0.95,
			maxConfidence:       1.0,
			shouldHaveAmbiguity: false,
		},
		{
			name:                "slice bounds with specific range",
			errorMsg:            "slice bounds out of range [10:15] with length 5",
			expectedCat:         CategoryIndexOutOfRange,
			minConfidence:       0.95,
			maxConfidence:       1.0,
			shouldHaveAmbiguity: false,
		},
		{
			name:                "multiple goroutines with panics",
			errorMsg:            "panic in goroutine 1, panic in goroutine 2, panic in goroutine 3",
			stackTrace:          "goroutine 1 [running]:\ngoroutine 2 [running]:\ngoroutine 3 [running]:",
			expectedCat:         CategoryGoroutinePanic,
			minConfidence:       0.7,
			maxConfidence:       0.85,
			shouldHaveAmbiguity: true,
		},
		{
			name:                "channel deadlock detected",
			errorMsg:            "potential deadlock detected in channel operation between goroutines",
			expectedCat:         CategoryDeadlock,
			minConfidence:       0.95,
			maxConfidence:       1.0,
			shouldHaveAmbiguity: false,
		},
		{
			name:                "broken pipe in I/O operation",
			errorMsg:            "write operation failed: broken pipe",
			expectedCat:         CategoryIOError,
			minConfidence:       0.7,
			maxConfidence:       0.9,
			shouldHaveAmbiguity: false,
		},
		{
			name:                "zero map key in concurrent access",
			errorMsg:            "zero map key in map access during iteration",
			expectedCat:         CategoryMapKey,
			minConfidence:       0.95,
			maxConfidence:       1.0,
			shouldHaveAmbiguity: false,
		},
		{
			name:                "short unknown error message",
			errorMsg:            "unknown error",
			expectedCat:         CategoryUnknown,
			minConfidence:       0.0,
			maxConfidence:       0.1,
			shouldHaveAmbiguity: false,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestAdvancedEdgeCase",
				FilePath:     "test.go",
				LineNumber:   10,
				ErrorMessage: tt.errorMsg,
				StackTrace:   tt.stackTrace,
			}

			cat := CategorizeFailure(failure)

			if cat.Category != tt.expectedCat {
				t.Errorf("Category: got %q, want %q", cat.Category, tt.expectedCat)
			}

			if cat.Confidence.Float64() < tt.minConfidence || cat.Confidence.Float64() > tt.maxConfidence {
				t.Errorf("Confidence: got %.2f, want [%.2f, %.2f]", cat.Confidence, tt.minConfidence, tt.maxConfidence)
			}

			hasAmbiguity := strings.Contains(cat.Reasoning, "Ambiguity detected")
			if tt.shouldHaveAmbiguity && !hasAmbiguity {
				t.Error("Expected ambiguity detection in reasoning")
			}
			if !tt.shouldHaveAmbiguity && hasAmbiguity {
				t.Error("Unexpected ambiguity detection in reasoning")
			}

			if tt.expectedSubcat != "" && cat.Subcategory != tt.expectedSubcat {
				t.Errorf("Subcategory: got %q, want %q", cat.Subcategory, tt.expectedSubcat)
			}

			if cat.Reasoning == "" {
				t.Error("Reasoning should not be empty")
			}
		})
	}
}

// TestCategorizeFailure_ResolutionIntegration tests that ambiguity resolution
// integrates properly with the main categorization function
func TestCategorizeFailure_ResolutionIntegration(t *testing.T) {
	// This tests the full integration: categorize -> detect ambiguity -> resolve
	errorMsg := "dial tcp 127.0.0.1:8080: connection timeout"

	failure := TestFailure{
		TestName:     "TestResolution",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	// First, categorize normally
	cat := CategorizeFailure(failure)

	// Apply resolution
	resolved := ResolveAmbiguity(cat)

	// Resolution should maintain or improve categorization
	if resolved.Confidence < cat.Confidence-0.1 {
		t.Errorf("Resolution reduced confidence too much: %.2f -> %.2f", cat.Confidence, resolved.Confidence)
	}

	// If resolution changed category, reasoning should explain why
	if resolved.Category != cat.Category && !strings.Contains(resolved.Reasoning, "Ambiguity resolution") {
		t.Error("Category changed without explanation in reasoning")
	}
}
