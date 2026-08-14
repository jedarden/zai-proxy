package testutil

import (
	"strings"
	"testing"
)

// TestCategorizeFailure_CoveredPaths provides table-driven tests for categorization
// logic paths that need comprehensive coverage. These tests document the core
// categorization behavior and ensure all decision tree branches are exercised.
//
// Test categories covered:
// - Empty stack trace handling
// - Unknown category (no pattern match)
// - Ambiguity detection and resolution
// - Priority-based rule selection
// - Confidence floor behavior
func TestCategorizeFailure_CoveredPaths(t *testing.T) {
	testCases := []struct {
		name                    string
		errorMsg                string
		stackTrace              string
		expectedCategory        FailureCategory
		expectedAmbiguous       bool
		expectedConfidenceMin   float64
		expectedConfidenceMax   float64
		description             string
	}{
		// Empty stack trace scenarios
		{
			name:                  "empty_stack_nil_pointer",
			errorMsg:              "nil pointer dereference",
			stackTrace:            "",
			expectedCategory:      CategoryNilPointer,
			expectedAmbiguous:     false,
			expectedConfidenceMin: 1.0,
			expectedConfidenceMax: 1.0,
			description:           "Tests empty stack trace with nil pointer - should categorize correctly",
		},
		{
			name:                  "empty_stack_timeout",
			errorMsg:              "context deadline exceeded",
			stackTrace:            "",
			expectedCategory:      CategoryTimeout,
			expectedAmbiguous:     false,
			expectedConfidenceMin: 0.95,
			expectedConfidenceMax: 0.95,
			description:           "Tests empty stack trace with timeout - should categorize correctly",
		},
		{
			name:                  "empty_stack_assertion",
			errorMsg:              "expected true, got false",
			stackTrace:            "",
			expectedCategory:      CategoryAssertionError,
			expectedAmbiguous:     false,
			expectedConfidenceMin: 0.7,
			expectedConfidenceMax: 0.7,
			description:           "Tests empty stack trace with assertion - should categorize correctly",
		},

		// Unknown category (no patterns match)
		{
			name:                  "unknown_no_patterns",
			errorMsg:              "something completely unknown",
			stackTrace:            "",
			expectedCategory:      CategoryUnknown,
			expectedAmbiguous:     false,
			expectedConfidenceMin: 0.0,
			expectedConfidenceMax: 0.0,
			description:           "Tests unknown categorization when no patterns match",
		},
		{
			name:                  "unknown_empty_input",
			errorMsg:              "",
			stackTrace:            "",
			expectedCategory:      CategoryUnknown,
			expectedAmbiguous:     false,
			expectedConfidenceMin: 0.0,
			expectedConfidenceMax: 0.0,
			description:           "Tests unknown categorization with completely empty input",
		},

		// Ambiguity scenarios
		{
			name:                  "ambiguous_nil_pointer_with_assertion",
			errorMsg:              "nil pointer dereference expected not nil",
			stackTrace:            "",
			expectedCategory:      CategoryNilPointer,
			expectedAmbiguous:     true,
			expectedConfidenceMin: 0.65,
			expectedConfidenceMax: 0.75,
			description:           "Tests nil pointer ambiguous with assertion - nil pointer wins with reduced confidence",
		},
		{
			name:                  "ambiguous_index_with_assertion",
			errorMsg:              "index out of range expected valid",
			stackTrace:            "",
			expectedCategory:      CategoryIndexOutOfRange,
			expectedAmbiguous:     true,
			expectedConfidenceMin: 0.65,
			expectedConfidenceMax: 0.75,
			description:           "Tests index error ambiguous with assertion - index wins with reduced confidence",
		},
		{
			name:                  "ambiguous_map_key_with_assertion",
			errorMsg:              "map key not found expected key",
			stackTrace:            "",
			expectedCategory:      CategoryMapKey,
			expectedAmbiguous:     true,
			expectedConfidenceMin: 0.60,
			expectedConfidenceMax: 0.70,
			description:           "Tests map key error ambiguous with assertion - map key wins with reduced confidence",
		},
		{
			name:                  "ambiguous_timeout_with_assertion",
			errorMsg:              "context deadline exceeded expected success",
			stackTrace:            "",
			expectedCategory:      CategoryTimeout,
			expectedAmbiguous:     true,
			expectedConfidenceMin: 0.60,
			expectedConfidenceMax: 0.70,
			description:           "Tests timeout ambiguous with assertion - timeout wins with reduced confidence",
		},

		// Complex ambiguity (multiple patterns)
		{
			name:                  "complex_nil_pointer_with_panic_and_assertion",
			errorMsg:              "nil pointer dereference in panic expected",
			stackTrace:            "",
			expectedCategory:      CategoryNilPointer,
			expectedAmbiguous:     true,
			expectedConfidenceMin: 0.60,
			expectedConfidenceMax: 0.75,
			description:           "Tests nil pointer ambiguous with panic and assertion",
		},

		// Priority-based selection
		{
			name:                  "priority_data_race_beats_all",
			errorMsg:              "WARNING: DATA RACE\npanic: runtime error",
			stackTrace:            "",
			expectedCategory:      CategoryDataRace,
			expectedAmbiguous:     false,
			expectedConfidenceMin: 1.0,
			expectedConfidenceMax: 1.0,
			description:           "Tests data race (priority 100) beats panic (priority 50)",
		},
		{
			name:                  "priority_deadlock_beats_panic",
			errorMsg:              "potential deadlock detected\npanic: error",
			stackTrace:            "",
			expectedCategory:      CategoryDeadlock,
			expectedAmbiguous:     false,
			expectedConfidenceMin: 1.0,
			expectedConfidenceMax: 1.0,
			description:           "Tests deadlock (priority 90) beats panic (priority 50)",
		},
		{
			name:                  "priority_timeout_beats_nil_pointer",
			errorMsg:              "context deadline exceeded\nnil pointer",
			stackTrace:            "",
			expectedCategory:      CategoryTimeout,
			expectedAmbiguous:     false,
			expectedConfidenceMin: 0.95,
			expectedConfidenceMax: 0.95,
			description:           "Tests timeout (priority 70) beats nil pointer (priority 65)",
		},
		{
			name:                  "priority_nil_pointer_beats_index",
			errorMsg:              "nil pointer dereference\nindex out of range",
			stackTrace:            "",
			expectedCategory:      CategoryNilPointer,
			expectedAmbiguous:     false,
			expectedConfidenceMin: 1.0,
			expectedConfidenceMax: 1.0,
			description:           "Tests nil pointer (priority 65) beats index error (priority 60)",
		},

		// Full text combination (error + stack trace)
		{
			name:                  "full_text_pattern_in_stack_only",
			errorMsg:              "generic error",
			stackTrace:            "panic: nil pointer dereference",
			expectedCategory:      CategoryNilPointer,
			expectedAmbiguous:     false,
			expectedConfidenceMin: 1.0,
			expectedConfidenceMax: 1.0,
			description:           "Tests pattern found in stack trace but not error message",
		},
		{
			name:                  "full_text_pattern_in_error_only",
			errorMsg:              "nil pointer dereference",
			stackTrace:            "at main.test()",
			expectedCategory:      CategoryNilPointer,
			expectedAmbiguous:     false,
			expectedConfidenceMin: 1.0,
			expectedConfidenceMax: 1.0,
			description:           "Tests pattern found in error message but not stack trace",
		},
		{
			name:                  "full_text_pattern_in_both",
			errorMsg:              "nil pointer dereference",
			stackTrace:            "panic: nil pointer dereference",
			expectedCategory:      CategoryNilPointer,
			expectedAmbiguous:     false,
			expectedConfidenceMin: 1.0,
			expectedConfidenceMax: 1.0,
			description:           "Tests pattern found in both error message and stack trace",
		},

		// Edge cases
		{
			name:                  "edge_short_unknown_message",
			errorMsg:              "xyz",
			stackTrace:            "",
			expectedCategory:      CategoryUnknown,
			expectedAmbiguous:     false,
			expectedConfidenceMin: 0.0,
			expectedConfidenceMax: 0.0,
			description:           "Tests very short unknown message",
		},
		{
			name:                  "edge_whitespace_only",
			errorMsg:              "   ",
			stackTrace:            "",
			expectedCategory:      CategoryUnknown,
			expectedAmbiguous:     false,
			expectedConfidenceMin: 0.0,
			expectedConfidenceMax: 0.0,
			description:           "Tests whitespace-only input",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:    "TestExample",
				FilePath:    "example_test.go",
				LineNumber:  42,
				ErrorMessage: tc.errorMsg,
				StackTrace:  tc.stackTrace,
			}

			result := CategorizeFailure(failure)

			// Check category
			if result.Category != tc.expectedCategory {
				t.Errorf("Category mismatch: got %q, want %q\nDescription: %s\nError: %s\nStackTrace: %s",
					result.Category, tc.expectedCategory, tc.description, tc.errorMsg, tc.stackTrace)
			}

			// Check ambiguity
			isAmbiguous := IsAmbiguous(result)
			if isAmbiguous != tc.expectedAmbiguous {
				t.Errorf("Ambiguity mismatch: got %v, want %v\nDescription: %s\nReasoning: %s",
					isAmbiguous, tc.expectedAmbiguous, tc.description, result.Reasoning)
			}

			// Check confidence range
			confidence := result.Confidence.Float64()
			if confidence < tc.expectedConfidenceMin || confidence > tc.expectedConfidenceMax {
				t.Errorf("Confidence out of range: got %.2f, want [%.2f, %.2f]\nDescription: %s",
					confidence, tc.expectedConfidenceMin, tc.expectedConfidenceMax, tc.description)
			}

			// Verify uncertainty flag
			expectedUncertain := confidence <= 0.7
			if result.Uncertain != expectedUncertain {
				t.Errorf("Uncertain flag: got %v, want %v (confidence %.2f)\nDescription: %s",
					result.Uncertain, expectedUncertain, confidence, tc.description)
			}

			// For unknown category, verify reasoning
			if tc.expectedCategory == CategoryUnknown {
				if !strings.Contains(result.Reasoning, "No categorization pattern matched") {
					t.Errorf("Expected reasoning about no pattern match, got: %s", result.Reasoning)
				}
			}

			// For ambiguous cases, verify reasoning mentions ambiguity
			if tc.expectedAmbiguous {
				if !strings.Contains(result.Reasoning, "Ambiguity detected") {
					t.Errorf("Expected ambiguity in reasoning\nDescription: %s\nReasoning: %s",
						tc.description, result.Reasoning)
				}
			}
		})
	}
}

// TestCategorizeFailure_AmbiguityHandling tests specific ambiguity handler
// configurations to ensure both explicit handlers and default penalties work correctly.
func TestCategorizeFailure_AmbiguityHandling(t *testing.T) {
	testCases := []struct {
		name                  string
		errorMsg              string
		expectedCategory      FailureCategory
		expectedAmbiguous     bool
		checkReasoning        func(string) bool
		description           string
	}{
		{
			name:              "explicit_handler_nil_pointer_vs_assertion",
			errorMsg:          "nil pointer dereference expected value",
			expectedCategory:  CategoryNilPointer,
			expectedAmbiguous: true,
			checkReasoning: func(r string) bool {
				return strings.Contains(r, "0.30") && // Explicit handler reduction
					strings.Contains(r, "assertion_error")
			},
			description: "Tests nil pointer uses explicit 0.30 handler for assertion",
		},
		{
			name:              "explicit_handler_timeout_vs_http",
			errorMsg:          "context deadline exceeded with connection",
			expectedCategory:  CategoryTimeout,
			expectedAmbiguous: true,
			checkReasoning: func(r string) bool {
				return strings.Contains(r, "0.15") && // Explicit handler reduction
					strings.Contains(r, "http_error")
			},
			description: "Tests timeout uses explicit 0.15 handler for HTTP error",
		},
		{
			name:              "explicit_handler_timeout_vs_io",
			errorMsg:          "context deadline exceeded reading file",
			expectedCategory:  CategoryTimeout,
			expectedAmbiguous: true,
			checkReasoning: func(r string) bool {
				return strings.Contains(r, "0.20") && // Explicit handler reduction
					strings.Contains(r, "io_error")
			},
			description: "Tests timeout uses explicit 0.20 handler for I/O error",
		},
		{
			name:              "default_penalty_map_key_vs_assertion",
			errorMsg:          "map key not found expected value",
			expectedCategory:  CategoryMapKey,
			expectedAmbiguous: true,
			checkReasoning: func(r string) bool {
				return strings.Contains(r, "default confidence penalty") &&
					strings.Contains(r, "assertion_error")
			},
			description: "Tests map key uses default 0.15 penalty for assertion (no explicit handler)",
		},
		{
			name:              "default_penalty_index_vs_assertion",
			errorMsg:          "index out of range expected valid",
			expectedCategory:  CategoryIndexOutOfRange,
			expectedAmbiguous: true,
			checkReasoning: func(r string) bool {
				return strings.Contains(r, "default confidence penalty") &&
					strings.Contains(r, "assertion_error")
			},
			description: "Tests index error uses default 0.15 penalty for assertion",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:    "TestExample",
				FilePath:    "example_test.go",
				LineNumber:  42,
				ErrorMessage: tc.errorMsg,
				StackTrace:  "",
			}

			result := CategorizeFailure(failure)

			if result.Category != tc.expectedCategory {
				t.Errorf("Category: got %q, want %q", result.Category, tc.expectedCategory)
			}

			isAmbiguous := IsAmbiguous(result)
			if isAmbiguous != tc.expectedAmbiguous {
				t.Errorf("Ambiguity: got %v, want %v", isAmbiguous, tc.expectedAmbiguous)
			}

			if tc.checkReasoning != nil && !tc.checkReasoning(result.Reasoning) {
				t.Errorf("Reasoning check failed\nReasoning: %s", result.Reasoning)
			}
		})
	}
}

// TestCategorizeFailure_CategorySpecificPaths tests category-specific
// code paths and edge cases.
func TestCategorizeFailure_CategorySpecificPaths(t *testing.T) {
	testCases := []struct {
		name                  string
		errorMsg              string
		stackTrace            string
		expectedCategory      FailureCategory
		expectedSubcategory    string
		expectedConfidence    float64
		description           string
	}{
		{
			name:               "http_error_with_network_subcategory",
			errorMsg:           "connection refused",
			stackTrace:         "",
			expectedCategory:   CategoryHTTPError,
			expectedSubcategory: "network",
			expectedConfidence: 0.85,
			description:        "Tests HTTP error gets 'network' subcategory",
		},
		{
			name:               "panic_with_runtime_subcategory",
			errorMsg:           "panic: runtime error",
			stackTrace:         "",
			expectedCategory:   CategoryPanic,
			expectedSubcategory: "runtime_panic",
			expectedConfidence: 1.0,
			description:        "Tests panic gets 'runtime_panic' subcategory",
		},
		{
			name:               "data_race_no_subcategory",
			errorMsg:           "WARNING: DATA RACE",
			stackTrace:         "",
			expectedCategory:   CategoryDataRace,
			expectedSubcategory: "",
			expectedConfidence: 1.0,
			description:        "Tests data race has no subcategory",
		},
		{
			name:               "deadlock_no_subcategory",
			errorMsg:           "potential deadlock detected",
			stackTrace:         "",
			expectedCategory:   CategoryDeadlock,
			expectedSubcategory: "",
			expectedConfidence: 1.0,
			description:        "Tests deadlock has no subcategory",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:    "TestExample",
				FilePath:    "example_test.go",
				LineNumber:  42,
				ErrorMessage: tc.errorMsg,
				StackTrace:  tc.stackTrace,
			}

			result := CategorizeFailure(failure)

			if result.Category != tc.expectedCategory {
				t.Errorf("Category: got %q, want %q", result.Category, tc.expectedCategory)
			}

			if result.Subcategory != tc.expectedSubcategory {
				t.Errorf("Subcategory: got %q, want %q", result.Subcategory, tc.expectedSubcategory)
			}

			confidence := result.Confidence.Float64()
			if confidence != tc.expectedConfidence {
				t.Errorf("Confidence: got %.2f, want %.2f", confidence, tc.expectedConfidence)
			}
		})
	}
}

// TestCategorizeFailure_UncertaintyFlag tests the uncertainty flag calculation
// to ensure it correctly identifies categorizations at or below the 0.7 threshold.
func TestCategorizeFailure_UncertaintyFlag(t *testing.T) {
	testCases := []struct {
		name               string
		errorMsg           string
		expectedUncertain  bool
		description        string
	}{
		{
			name:              "certain_high_confidence",
			errorMsg:          "nil pointer dereference",
			expectedUncertain: false, // 1.0 confidence > 0.7
			description:       "High confidence (1.0) should not be uncertain",
		},
		{
			name:              "certain_medium_confidence",
			errorMsg:          "context deadline exceeded",
			expectedUncertain: false, // 0.95 confidence > 0.7
			description:       "Medium-high confidence (0.95) should not be uncertain",
		},
		{
			name:              "uncertain_at_threshold",
			errorMsg:          "expected value got different",
			expectedUncertain: true, // 0.7 confidence <= 0.7 (exactly at threshold)
			description:       "Pure assertion error at exactly 0.7 should be uncertain",
		},
		{
			name:              "uncertain_low_confidence",
			errorMsg:          "expected true got false", // Pure assertion error = 0.7 exactly
			expectedUncertain: true, // 0.7 confidence <= 0.7 (threshold is inclusive)
			description:       "Confidence at exactly 0.7 should be uncertain (inclusive threshold)",
		},
		{
			name:              "uncertain_unknown_category",
			errorMsg:          "unknown error message",
			expectedUncertain: true, // 0.0 confidence <= 0.7
			description:       "Unknown category (0.0 confidence) should always be uncertain",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:    "TestExample",
				FilePath:    "example_test.go",
				LineNumber:  42,
				ErrorMessage: tc.errorMsg,
				StackTrace:  "",
			}

			result := CategorizeFailure(failure)

			if result.Uncertain != tc.expectedUncertain {
				t.Errorf("Uncertain: got %v, want %v\nDescription: %s\nConfidence: %.2f",
					result.Uncertain, tc.expectedUncertain, tc.description, result.Confidence.Float64())
			}

			// Also verify via IsUncertain method
			if result.Confidence.IsUncertain() != tc.expectedUncertain {
				t.Errorf("IsUncertain(): got %v, want %v\nDescription: %s",
					result.Confidence.IsUncertain(), tc.expectedUncertain, tc.description)
			}
		})
	}
}
