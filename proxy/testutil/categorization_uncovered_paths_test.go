package testutil

import (
	"strings"
	"testing"
)

// TestCategorizeFailure_UncoveredPaths tests code paths in CategorizeFailure
// that have low coverage or are edge cases not hit by other tests.
func TestCategorizeFailure_UncoveredPaths(t *testing.T) {
	tests := []struct {
		name              string
		failure           TestFailure
		wantCategory      FailureCategory
		wantMinConfidence float64
		wantMaxConfidence float64
		wantUncertain     bool
		description       string // What scenario this tests
	}{
		{
			name: "empty_error_message_and_empty_stack",
			failure: TestFailure{
				ErrorMessage: "",
				StackTrace:   "",
			},
			wantCategory:      CategoryUnknown,
			wantMinConfidence: 0.00,
			wantMaxConfidence: 0.00,
			wantUncertain:     true,
			description:       "Tests CategorizeFailure with completely empty input - should return unknown with 0 confidence and uncertain flag",
		},
		{
			name: "stack_trace_only_no_error_message",
			failure: TestFailure{
				ErrorMessage: "",
				StackTrace:   "goroutine 1 [running]:\ngithub.com/pkg/webapp.Handler()\n\t/path/to/handler.go:123",
			},
			wantCategory:      CategoryGoroutinePanic, // Stack trace contains "goroutine [running]" pattern
			wantMinConfidence: 0.85,
			wantMaxConfidence: 0.95,
			wantUncertain:     false,
			description:       "Tests CategorizeFailure with only stack trace containing 'goroutine [running]' - matches goroutine_panic pattern",
		},
		{
			name: "error_message_only_no_stack_trace",
			failure: TestFailure{
				ErrorMessage: "timeout: operation timed out after 5 seconds",
				StackTrace:   "",
			},
			wantCategory:      CategoryTimeout,
			wantMinConfidence: 0.90,
			wantMaxConfidence: 0.95,
			wantUncertain:     false,
			description:       "Tests CategorizeFailure with only error message, no stack trace - standard timeout categorization",
		},
		{
			name: "very_short_gibberish_error",
			failure: TestFailure{
				ErrorMessage: "xyz",
				StackTrace:   "",
			},
			wantCategory:      CategoryUnknown,
			wantMinConfidence: 0.00,
			wantMaxConfidence: 0.00,
			wantUncertain:     true,
			description:       "Tests very short gibberish error that doesn't match any patterns - should be unknown with 0 confidence",
		},
		{
			name: "error_with_only_whitespace",
			failure: TestFailure{
				ErrorMessage: "   \n\t  ",
				StackTrace:   "",
			},
			wantCategory:      CategoryUnknown,
			wantMinConfidence: 0.00,
			wantMaxConfidence: 0.00,
			wantUncertain:     true,
			description:       "Tests error message containing only whitespace - should be treated as unknown",
		},
		{
			name: "multiple_errors_in_same_message_with_distinct_patterns",
			failure: TestFailure{
				ErrorMessage: "panic: nil pointer dereference\ntimeout: connection timeout",
				StackTrace:   "",
			},
			wantCategory:      CategoryNilPointer, // Higher priority (65) than http_error (40) - panic pattern matches "panic:"
			wantMinConfidence: 0.70,
			wantMaxConfidence: 0.85,
			wantUncertain:     false, // Confidence 0.75 > 0.7 threshold, so not uncertain despite ambiguity
			description:       "Tests message with panic and HTTP error patterns - nil pointer wins, ambiguity reduces confidence to 0.75",
		},
		{
			name: "error_message_exactly_20_chars_boundary",
			failure: TestFailure{
				ErrorMessage: "12345678901234567890",
				StackTrace:   "",
			},
			wantCategory:      CategoryUnknown,
			wantMinConfidence: 0.00,
			wantMaxConfidence: 0.00,
			wantUncertain:     true,
			description:       "Tests edge case of exactly 20 character unknown message - boundary for short message adjustment",
		},
		{
			name: "error_message_19_chars_just_below_boundary",
			failure: TestFailure{
				ErrorMessage: "1234567890123456789",
				StackTrace:   "",
			},
			wantCategory:      CategoryUnknown,
			wantMinConfidence: 0.00,
			wantMaxConfidence: 0.00,
			wantUncertain:     true,
			description:       "Tests edge case of 19 character unknown message - just below short message threshold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CategorizeFailure(tt.failure)

			if got.Category != tt.wantCategory {
				t.Errorf("Category: got %s, want %s (%s)", got.Category, tt.wantCategory, tt.description)
			}

			confidence := got.Confidence.Float64()
			if confidence < tt.wantMinConfidence || confidence > tt.wantMaxConfidence {
				t.Errorf("Confidence: got %.2f, want [%.2f, %.2f] (%s)", confidence, tt.wantMinConfidence, tt.wantMaxConfidence, tt.description)
			}

			if got.Uncertain != tt.wantUncertain {
				t.Errorf("Uncertain: got %v, want %v (%s)", got.Uncertain, tt.wantUncertain, tt.description)
			}
		})
	}
}

// TestGetMatchingCategoriesForFailure_UncoveredPaths tests edge cases in
// GetMatchingCategoriesForFailure that may not be covered by other tests.
func TestGetMatchingCategoriesForFailure_UncoveredPaths(t *testing.T) {
	tests := []struct {
		name        string
		failure     TestFailure
		wantCount   int    // Expected number of matching categories
		wantEmpty   bool   // Whether result should be empty
		description string // What scenario this tests
	}{
		{
			name: "empty_error_and_stack",
			failure: TestFailure{
				ErrorMessage: "",
				StackTrace:   "",
			},
			wantCount:   0,
			wantEmpty:   true,
			description: "Tests GetMatchingCategoriesForFailure with completely empty input - should return empty slice",
		},
		{
			name: "error_only_no_stack",
			failure: TestFailure{
				ErrorMessage: "timeout: operation timed out",
				StackTrace:   "",
			},
			wantCount:   1,
			wantEmpty:   false,
			description: "Tests GetMatchingCategoriesForFailure with error message only - should match timeout pattern",
		},
		{
			name: "stack_only_no_error",
			failure: TestFailure{
				ErrorMessage: "",
				StackTrace:   "panic: nil pointer dereference in handler",
			},
			wantCount:   2, // panic and nil_pointer patterns both match
			wantEmpty:   false,
			description: "Tests GetMatchingCategoriesForFailure with stack trace only - should match panic and nil_pointer patterns",
		},
		{
			name: "gibberish_no_patterns_match",
			failure: TestFailure{
				ErrorMessage: "xyz123 abc456 def789",
				StackTrace:   "",
			},
			wantCount:   0,
			wantEmpty:   true,
			description: "Tests GetMatchingCategoriesForFailure with gibberish that matches no patterns - should return empty slice",
		},
		{
			name: "whitespace_only_no_patterns_match",
			failure: TestFailure{
				ErrorMessage: "   \n\t   ",
				StackTrace:   "",
			},
			wantCount:   0,
			wantEmpty:   true,
			description: "Tests GetMatchingCategoriesForFailure with only whitespace - should return empty slice",
		},
		{
			name: "multiple_patterns_same_category",
			failure: TestFailure{
				ErrorMessage: "nil pointer dereference followed by panic on nil pointer",
				StackTrace:   "",
			},
			wantCount:   1, // Only nil_pointer category matches (panic doesn't match without "panic:" prefix)
			wantEmpty:   false,
			description: "Tests GetMatchingCategoriesForFailure with multiple patterns in same category - should return unique categories only",
		},
		{
			name: "patterns_in_both_error_and_stack",
			failure: TestFailure{
				ErrorMessage: "timeout occurred",
				StackTrace:   "dial tcp 127.0.0.1:8080: connection refused",
			},
			wantCount:   1, // Only timeout pattern matches (dial tcp is part of timeout pattern)
			wantEmpty:   false,
			description: "Tests GetMatchingCategoriesForFailure with timeout in error message - dial tcp is part of timeout pattern",
		},
		{
			name: "highly_ambiguous_many_patterns_match",
			failure: TestFailure{
				ErrorMessage: "panic during HTTP connection timeout",
				StackTrace:   "",
			},
			wantCount:   1, // Only http_error matches (due to "connection timeout")
			wantEmpty:   false,
			description: "Tests GetMatchingCategoriesForFailure with 'connection timeout' - matches http_error pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetMatchingCategoriesForFailure(tt.failure)

			if tt.wantEmpty && len(got) != 0 {
				t.Errorf("GetMatchingCategoriesForFailure() returned %d categories, want 0 (%s)", len(got), tt.description)
			}

			if !tt.wantEmpty && len(got) == 0 {
				t.Errorf("GetMatchingCategoriesForFailure() returned empty slice, want %d categories (%s)", tt.wantCount, tt.description)
			}

			if tt.wantCount > 0 && len(got) != tt.wantCount {
				t.Errorf("GetMatchingCategoriesForFailure() returned %d categories, want %d (%s)", len(got), tt.wantCount, tt.description)
			}
		})
	}
}

// TestApplyEdgeCaseAdjustments_UncoveredPaths tests edge case adjustments
// that may have incomplete coverage.
func TestApplyEdgeCaseAdjustments_UncoveredPaths(t *testing.T) {
	tests := []struct {
		name              string
		fullText          string
		category          FailureCategory
		baseConfidence    float64
		wantMinConfidence float64
		wantMaxConfidence float64
		description       string // What scenario this tests
	}{
		{
			name:              "empty_text_with_nil_pointer_category",
			fullText:          "",
			category:          CategoryNilPointer,
			baseConfidence:    0.9,
			wantMinConfidence: 0.05,
			wantMaxConfidence: 1.0,
			description:       "Tests applyEdgeCaseAdjustments with empty text - should apply minimum confidence floor",
		},
		{
			name:              "nil_pointer_with_test_mock_context_reduces_confidence",
			fullText:          "nil pointer dereference in mock setup during test",
			category:          CategoryNilPointer,
			baseConfidence:    1.0,
			wantMinConfidence: 0.8,
			wantMaxConfidence: 0.9,
			description:       "Tests nil pointer in test with mock context - should reduce confidence by 0.1",
		},
		{
			name:              "nil_pointer_with_test_fake_context_reduces_confidence",
			fullText:          "nil pointer dereference in fake handler during test",
			category:          CategoryNilPointer,
			baseConfidence:    1.0,
			wantMinConfidence: 0.8,
			wantMaxConfidence: 0.9,
			description:       "Tests nil pointer in test with fake context - should reduce confidence by 0.1",
		},
		{
			name:              "interface_conversion_panic_reduces_confidence",
			fullText:          "panic: interface conversion: interface {} is string, not int",
			category:          CategoryPanic,
			baseConfidence:    1.0,
			wantMinConfidence: 0.8,
			wantMaxConfidence: 0.85,
			description:       "Tests panic with interface conversion - should reduce confidence by 0.15 with floor at 0.5",
		},
		{
			name:              "multiple_panics_reduces_confidence_slightly",
			fullText:          "panic: first panic\npanic: second panic\npanic: third panic",
			category:          CategoryPanic,
			baseConfidence:    1.0,
			wantMinConfidence: 0.9,
			wantMaxConfidence: 0.95,
			description:       "Tests multiple panics in same error - should reduce confidence by 0.05 with floor at 0.7",
		},
		{
			name:              "very_short_unknown_message_minimum_confidence",
			fullText:          "err",
			category:          CategoryUnknown,
			baseConfidence:    0.5,
			wantMinConfidence: 0.05,
			wantMaxConfidence: 0.05,
			description:       "Tests unknown category with very short message (<20 chars) - should set confidence to 0.05",
		},
		{
			name:              "confidence_below_minimum_floor_is_clamped",
			fullText:          "some error text",
			category:          CategoryAssertionError,
			baseConfidence:    0.01,
			wantMinConfidence: 0.05,
			wantMaxConfidence: 0.05,
			description:       "Tests confidence below minimum floor (0.05) - should clamp to minimum",
		},
		{
			name:              "confidence_above_maximum_is_clamped",
			fullText:          "some error text",
			category:          CategoryAssertionError,
			baseConfidence:    1.5,
			wantMinConfidence: 1.0,
			wantMaxConfidence: 1.0,
			description:       "Tests confidence above maximum (1.0) - should clamp to 1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyEdgeCaseAdjustments(tt.fullText, tt.category, tt.baseConfidence)

			if got < tt.wantMinConfidence || got > tt.wantMaxConfidence {
				t.Errorf("applyEdgeCaseAdjustments() = %.2f, want [%.2f, %.2f] (%s)", got, tt.wantMinConfidence, tt.wantMaxConfidence, tt.description)
			}
		})
	}
}

// TestCategorizeFailure_AmbiguityDetection tests the ambiguity detection
// and confidence adjustment logic when multiple patterns match.
func TestCategorizeFailure_AmbiguityDetection(t *testing.T) {
	tests := []struct {
		name              string
		failure           TestFailure
		wantCategory      FailureCategory
		wantMinConfidence float64
		wantMaxConfidence float64
		wantAmbiguous     bool
		description       string // What scenario this tests
	}{
		{
			name: "timeout_and_http_error_ambiguous",
			failure: TestFailure{
				ErrorMessage: "connection timeout while dialing tcp 127.0.0.1:8080",
				StackTrace:   "",
			},
			wantCategory:      CategoryHTTPError, // Pattern "connection timeout" matches HTTPError, not Timeout
			wantMinConfidence: 0.80,
			wantMaxConfidence: 0.95,
			wantAmbiguous:     false, // Only HTTPError matches
			description:       "Tests case with 'connection timeout' which matches HTTPError pattern, not Timeout pattern",
		},
		{
			name: "panic_and_nil_pointer_ambiguous",
			failure: TestFailure{
				ErrorMessage: "panic: nil pointer dereference",
				StackTrace:   "",
			},
			wantCategory:      CategoryNilPointer, // Higher priority than general panic
			wantMinConfidence: 0.80,
			wantMaxConfidence: 0.95,
			wantAmbiguous:     true,
			description:       "Tests ambiguous case matching both nil pointer and panic - should detect ambiguity",
		},
		{
			name: "http_error_and_timeout_ambiguous",
			failure: TestFailure{
				ErrorMessage: "HTTP GET failed: dial tcp 127.0.0.1:8080: connection timeout",
				StackTrace:   "",
			},
			wantCategory:      CategoryHTTPError, // Matches "dial tcp" pattern
			wantMinConfidence: 0.80,
			wantMaxConfidence: 0.95,
			wantAmbiguous:     false, // Only HTTPError matches (Timeout pattern doesn't match "connection timeout")
			description:       "Tests case with 'dial tcp' and 'connection timeout' - only HTTPError pattern matches",
		},
		{
			name: "three_patterns_match_highly_ambiguous",
			failure: TestFailure{
				ErrorMessage: "panic during HTTP connection timeout",
				StackTrace:   "",
			},
			wantCategory:      CategoryHTTPError, // "connection timeout" matches HTTPError pattern
			wantMinConfidence: 0.80,
			wantMaxConfidence: 0.95,
			wantAmbiguous:     false, // Only HTTPError matches
			description:       "Tests case with 'connection timeout' - matches HTTPError pattern, not Timeout or Panic patterns",
		},
		{
			name: "single_pattern_match_no_ambiguity",
			failure: TestFailure{
				ErrorMessage: "assignment to entry in nil map",
				StackTrace:   "",
			},
			wantCategory:      CategoryNilPointer, // "assignment to entry in nil map" matches nil pointer pattern
			wantMinConfidence: 1.00,
			wantMaxConfidence: 1.00,
			wantAmbiguous:     false,
			description:       "Tests case with 'assignment to entry in nil map' - matches nil_pointer pattern with high confidence",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CategorizeFailure(tt.failure)

			if got.Category != tt.wantCategory {
				t.Errorf("Category: got %s, want %s (%s)", got.Category, tt.wantCategory, tt.description)
			}

			confidence := got.Confidence.Float64()
			if confidence < tt.wantMinConfidence || confidence > tt.wantMaxConfidence {
				t.Errorf("Confidence: got %.2f, want [%.2f, %.2f] (%s)", confidence, tt.wantMinConfidence, tt.wantMaxConfidence, tt.description)
			}

			isAmbiguous := IsAmbiguous(got)
			if isAmbiguous != tt.wantAmbiguous {
				t.Errorf("Ambiguous detection: got %v, want %v (%s)", isAmbiguous, tt.wantAmbiguous, tt.description)
			}

			if tt.wantAmbiguous && !strings.Contains(got.Reasoning, "Ambiguity detected") {
				t.Errorf("Reasoning should contain 'Ambiguity detected' for ambiguous case (%s)", tt.description)
			}
		})
	}
}

// TestGetAmbiguousCountUncoveredPaths tests the GetAmbiguousCount function with various scenarios for uncovered paths.
func TestGetAmbiguousCountUncoveredPaths(t *testing.T) {
	tests := []struct {
		name        string
		failure     TestFailure
		wantCount   int
		description string
	}{
		{
			name: "no_ambiguity_single_pattern",
			failure: TestFailure{
				ErrorMessage: "assignment to entry in nil map",
			},
			wantCount:   1,
			description: "Tests GetAmbiguousCount with single pattern match - should return 1",
		},
		{
			name: "ambiguous_two_patterns",
			failure: TestFailure{
				ErrorMessage: "panic: nil pointer dereference",
			},
			wantCount:   2,
			description: "Tests GetAmbiguousCount with two pattern matches - should return 2",
		},
		{
			name: "ambiguous_three_patterns",
			failure: TestFailure{
				ErrorMessage: "panic: nil pointer dereference in test that timed out",
			},
			wantCount:   3, // panic, nil_pointer, http_error
			description: "Tests GetAmbiguousCount with three pattern matches - should return 3",
		},
		{
			name: "no_pattern_match_unknown",
			failure: TestFailure{
				ErrorMessage: "xyz123 this doesn't match anything",
			},
			wantCount:   1,
			description: "Tests GetAmbiguousCount with no pattern matches (unknown) - should return 1 as default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cat := CategorizeFailure(tt.failure)
			got := GetAmbiguousCount(cat)

			if got != tt.wantCount {
				t.Errorf("GetAmbiguousCount() = %d, want %d (%s)", got, tt.wantCount, tt.description)
			}
		})
	}
}
