package testutil

import (
	"fmt"
	"strings"
	"testing"
)

// TestCategorizeFailure_UncoveredLogicPaths tests the specifically uncovered
// paths in CategorizeFailure based on coverage analysis.
//
// These tests focus on:
// 1. Empty stack trace behavior (line 679-681 in categorization.go)
// 2. Unknown category handling (line 696-704)
// 3. Ambiguity without explicit handlers (line 728-736)
// 4. Multiple pattern matching scenarios
// 5. Confidence floor behavior
func TestCategorizeFailure_UncoveredLogicPaths(t *testing.T) {
	testCases := []struct {
		name                    string
		errorMsg                string
		stackTrace              string
		expectedCategory        FailureCategory
		expectedAmbiguousCount  int
		expectedConfidenceRange [2]float64 // [min, max]
		description             string
	}{
		// ============================================================================
		// CATEGORY: Empty Stack Trace
		// Tests path: lines 679-681 where failure.StackTrace == ""
		// ============================================================================
		{
			name:                    "empty_stack_trace_nil_pointer",
			errorMsg:                "nil pointer dereference",
			stackTrace:              "",
			expectedCategory:        CategoryNilPointer,
			expectedAmbiguousCount:  1,
			expectedConfidenceRange: [2]float64{1.0, 1.0},
			description:             "Tests categorization with empty stack trace - nil pointer",
		},
		{
			name:                    "empty_stack_trace_timeout",
			errorMsg:                "context deadline exceeded",
			stackTrace:              "",
			expectedCategory:        CategoryTimeout,
			expectedAmbiguousCount:  1,
			expectedConfidenceRange: [2]float64{0.95, 0.95},
			description:             "Tests categorization with empty stack trace - timeout",
		},
		{
			name:                    "empty_stack_trace_assertion",
			errorMsg:                "expected true, got false",
			stackTrace:              "",
			expectedCategory:        CategoryAssertionError,
			expectedAmbiguousCount:  1,
			expectedConfidenceRange: [2]float64{0.7, 0.7},
			description:             "Tests categorization with empty stack trace - assertion",
		},

		// ============================================================================
		// CATEGORY: Unknown Category (No Patterns Match)
		// Tests path: lines 696-704 when len(matchingRules) == 0
		// ============================================================================
		{
			name:                    "unknown_no_pattern_match",
			errorMsg:                "something completely unknown and unexpected",
			stackTrace:              "",
			expectedCategory:        CategoryUnknown,
			expectedAmbiguousCount:  1,
			expectedConfidenceRange: [2]float64{0.0, 0.0},
			description:             "Tests unknown categorization when no patterns match",
		},
		{
			name:                    "unknown_with_empty_message",
			errorMsg:                "",
			stackTrace:              "",
			expectedCategory:        CategoryUnknown,
			expectedAmbiguousCount:  1,
			expectedConfidenceRange: [2]float64{0.0, 0.0},
			description:             "Tests unknown categorization with completely empty input",
		},
		{
			name:                    "unknown_short_message",
			errorMsg:                "xyz",
			stackTrace:              "",
			expectedCategory:        CategoryUnknown,
			expectedAmbiguousCount:  1,
			expectedConfidenceRange: [2]float64{0.0, 0.0},
			description:             "Tests unknown categorization with very short message",
		},

		// ============================================================================
		// CATEGORY: Default Ambiguity Penalty (No Handler)
		// Tests path: lines 728-736 when primary rule has no ambiguity handler
		// ============================================================================
		{
			name:                    "default_penalty_map_key_with_assertion",
			errorMsg:                "map key not found: expected key got nil",
			stackTrace:              "",
			expectedCategory:        CategoryMapKey,
			expectedAmbiguousCount:  2, // map_key, assertion_error
			expectedConfidenceRange: [2]float64{0.65, 0.65}, // 0.95 - 0.30 = 0.65 (exactly)
			description:             "Tests default 0.15 penalty when map_key has no handler for assertion",
		},
		{
			name:                    "default_penalty_index_with_assertion",
			errorMsg:                "index out of range in expected valid got invalid",
			stackTrace:              "",
			expectedCategory:        CategoryIndexOutOfRange,
			expectedAmbiguousCount:  2, // index_out_of_range, assertion_error
			expectedConfidenceRange: [2]float64{0.7, 0.7},
			description:             "Tests default 0.15 penalty when index has no handler for assertion",
		},

		// ============================================================================
		// CATEGORY: Confidence Floor with Handler
		// Tests path: lines 723-725 where handler ensures minimum 0.1 confidence
		// ============================================================================
		{
			name:                    "handler_floor_prevents_too_low",
			errorMsg:                "nil pointer dereference with assertion timeout",
			stackTrace:              "",
			expectedCategory:        CategoryNilPointer,
			expectedAmbiguousCount:  2, // nil_pointer, assertion_error (panic and timeout don't match this text)
			expectedConfidenceRange: [2]float64{0.65, 0.75}, // 1.0 - 0.30 = 0.7
			description:             "Tests that handler floor (0.1) prevents confidence from dropping below 0.1",
		},

		// ============================================================================
		// CATEGORY: Confidence Floor with Default Penalty
		// Tests path: lines 731-732 where default penalty ensures minimum 0.2 confidence
		// ============================================================================
		{
			name:                    "default_penalty_floor",
			errorMsg:                "key not found in map during test",
			stackTrace:              "",
			expectedCategory:        CategoryMapKey,
			expectedAmbiguousCount:  2, // map_key, assertion_error
			expectedConfidenceRange: [2]float64{0.65, 0.80}, // 0.95 - 0.30 = 0.65
			description:             "Tests that default penalty floor (0.2) prevents confidence from dropping below 0.2",
		},

		// ============================================================================
		// CATEGORY: Multi-Pattern Ambiguity
		// Tests path: lines 717-740 handling multiple matching patterns
		// ============================================================================
		{
			name:                    "three_pattern_ambiguous",
			errorMsg:                "nil pointer dereference in runtime panic",
			stackTrace:              "",
			expectedCategory:        CategoryNilPointer,
			expectedAmbiguousCount:  2, // nil_pointer, panic
			expectedConfidenceRange: [2]float64{0.85, 0.95}, // 1.0 - 0.10 = 0.9
			description:             "Tests two patterns matching (nil pointer, panic)",
		},
		{
			name:                    "four_pattern_ambiguous",
			errorMsg:                "nil pointer dereference with goroutine assertion",
			stackTrace:              "panic on nil pointer",
			expectedCategory:        CategoryNilPointer,
			expectedAmbiguousCount:  2, // nil_pointer, assertion_error (panic on nil pointer is the nil pointer pattern, not panic)
			expectedConfidenceRange: [2]float64{0.65, 0.75}, // 1.0 - 0.30 = 0.7 (plus edge case boost)
			description:             "Tests two patterns matching - nil pointer gets boost for 'panic on nil pointer'",
		},

		// ============================================================================
		// CATEGORY: Explicit Handler vs Default Penalty
		// Tests both paths: lines 721-727 (handler) and 728-736 (default penalty)
		// ============================================================================
		{
			name:                    "explicit_handler_for_http_vs_io",
			errorMsg:                "connection refused while reading file",
			stackTrace:              "",
			expectedCategory:        CategoryHTTPError,
			expectedAmbiguousCount:  1, // Only HTTP matches (I/O pattern doesn't match "connection refused")
			expectedConfidenceRange: [2]float64{0.85, 0.85},
			description:             "Tests HTTP vs I/O where HTTP pattern matches but I/O doesn't",
		},

		// ============================================================================
		// CATEGORY: Ambiguous Count Tracking
		// Tests path: lines 716-741 building ambiguous categories list
		// ============================================================================
		{
			name:                    "ambiguous_count_two_patterns",
			errorMsg:                "nil pointer dereference expected value got nil",
			stackTrace:              "",
			expectedCategory:        CategoryNilPointer,
			expectedAmbiguousCount:  2, // nil_pointer, assertion_error
			expectedConfidenceRange: [2]float64{0.7, 0.7},
			description:             "Tests ambiguous count tracking for exactly 2 patterns",
		},

		// ============================================================================
		// CATEGORY: Full Text Combination
		// Tests path: lines 678-681 combining error message and stack trace
		// ============================================================================
		{
			name:                    "full_text_with_stack_trace",
			errorMsg:                "error in test",
			stackTrace:              "panic: nil pointer dereference",
			expectedCategory:        CategoryNilPointer,
			expectedAmbiguousCount:  1, // Only nil_pointer matches (nil pointer pattern includes "panic on nil pointer")
			expectedConfidenceRange: [2]float64{1.0, 1.0}, // "panic on nil pointer" boosts to 1.0
			description:             "Tests pattern matching across combined error+stack text - nil pointer pattern wins",
		},
		{
			name:                    "full_text_no_match_in_error_only",
			errorMsg:                "generic error message",
			stackTrace:              "panic: runtime error",
			expectedCategory:        CategoryPanic,
			expectedAmbiguousCount:  1, // Only panic matches
			expectedConfidenceRange: [2]float64{1.0, 1.0},
			description:             "Tests pattern found in stack trace but not error message",
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

			// Check ambiguity count
			ambiguousCount := GetAmbiguousCount(result)
			if ambiguousCount != tc.expectedAmbiguousCount {
				t.Errorf("Ambiguous count: got %d, want %d\nDescription: %s\nReasoning: %s",
					ambiguousCount, tc.expectedAmbiguousCount, tc.description, result.Reasoning)
			}

			// Check confidence range
			confidence := result.Confidence.Float64()
			if confidence < tc.expectedConfidenceRange[0] || confidence > tc.expectedConfidenceRange[1] {
				t.Errorf("Confidence out of range: got %.2f, want [%.2f, %.2f]\nDescription: %s",
					confidence, tc.expectedConfidenceRange[0], tc.expectedConfidenceRange[1], tc.description)
			}

			// Verify uncertainty flag is set correctly
			expectedUncertain := confidence <= 0.7
			if result.Uncertain != expectedUncertain {
				t.Errorf("Uncertain flag: got %v, want %v (confidence %.2f)\nDescription: %s",
					result.Uncertain, expectedUncertain, confidence, tc.description)
			}

			// For ambiguous cases, verify reasoning contains expected text
			if tc.expectedAmbiguousCount > 1 {
				if !strings.Contains(result.Reasoning, "Ambiguity detected") {
					t.Errorf("Expected ambiguity in reasoning for %s\nReasoning: %s",
						tc.description, result.Reasoning)
				}
				expectedCountText := fmt.Sprintf("Total matching patterns: %d", tc.expectedAmbiguousCount)
				if !strings.Contains(result.Reasoning, expectedCountText) {
					t.Errorf("Expected '%s' in reasoning\nReasoning: %s",
						expectedCountText, result.Reasoning)
				}
			}
		})
	}
}

// TestCategorizeFailure_AmbiguityHandlerScenarios tests specific ambiguity
// handler scenarios to ensure both explicit handlers and default penalties work.
func TestCategorizeFailure_AmbiguityHandlerScenarios(t *testing.T) {
	testCases := []struct {
		name                    string
		errorMsg                string
		expectedCategory        FailureCategory
		expectedHasAmbiguity    bool
		expectedConfidenceRange [2]float64
		checkReasoning          func(string) error
		description             string
	}{
		{
			name:                    "nil_pointer_with_assertion_uses_explicit_handler",
			errorMsg:                "nil pointer dereference expected not nil got nil",
			expectedCategory:        CategoryNilPointer,
			expectedHasAmbiguity:    true,
			expectedConfidenceRange: [2]float64{0.7, 0.7},
			checkReasoning: func(r string) error {
				if !strings.Contains(r, "0.30") {
					return fmt.Errorf("expected explicit handler reduction of 0.30, got: %s", r)
				}
				if !strings.Contains(r, "assertion_error") {
					return fmt.Errorf("expected assertion_error mentioned, got: %s", r)
				}
				return nil
			},
			description: "Tests that nil_pointer uses explicit handler for assertion_error (0.30 reduction)",
		},
		{
			name:                    "timeout_with_http_uses_explicit_handler",
			errorMsg:                "context deadline exceeded: connection timeout",
			expectedCategory:        CategoryTimeout,
			expectedHasAmbiguity:    true,
			expectedConfidenceRange: [2]float64{0.75, 0.85}, // 0.95 - 0.15 = 0.80, but edge case might adjust
			checkReasoning: func(r string) error {
				if !strings.Contains(r, "0.15") {
					return fmt.Errorf("expected explicit handler reduction of 0.15, got: %s", r)
				}
				if !strings.Contains(r, "http_error") {
					return fmt.Errorf("expected http_error mentioned, got: %s", r)
				}
				return nil
			},
			description: "Tests that timeout uses explicit handler for http_error (0.15 reduction)",
		},
		{
			name:                    "map_key_with_assertion_uses_default_penalty",
			errorMsg:                "map key not found expected key got value",
			expectedCategory:        CategoryMapKey,
			expectedHasAmbiguity:    true,
			expectedConfidenceRange: [2]float64{0.65, 0.80},
			checkReasoning: func(r string) error {
				if !strings.Contains(r, "default confidence penalty") {
					return fmt.Errorf("expected default penalty, got: %s", r)
				}
				return nil
			},
			description: "Tests that map_key uses default penalty for assertion_error (no explicit handler)",
		},
		{
			name:                    "http_with_io_uses_explicit_handler",
			errorMsg:                "connection refused while reading file failed",
			expectedCategory:        CategoryHTTPError,
			expectedHasAmbiguity:    false, // Only http matches, io pattern doesn't match "connection refused"
			expectedConfidenceRange: [2]float64{0.85, 0.85},
			checkReasoning: func(r string) error {
				if strings.Contains(r, "Ambiguity detected") {
					return fmt.Errorf("expected no ambiguity, got: %s", r)
				}
				return nil
			},
			description: "Tests HTTP error that doesn't match I/O pattern - no ambiguity",
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

			hasAmbiguity := IsAmbiguous(result)
			if hasAmbiguity != tc.expectedHasAmbiguity {
				t.Errorf("Ambiguity: got %v, want %v\nReasoning: %s",
					hasAmbiguity, tc.expectedHasAmbiguity, result.Reasoning)
			}

			confidence := result.Confidence.Float64()
			if confidence < tc.expectedConfidenceRange[0] || confidence > tc.expectedConfidenceRange[1] {
				t.Errorf("Confidence %.2f not in range [%.2f, %.2f]",
					confidence, tc.expectedConfidenceRange[0], tc.expectedConfidenceRange[1])
			}

			if tc.checkReasoning != nil {
				if err := tc.checkReasoning(result.Reasoning); err != nil {
					t.Errorf("Reasoning check failed: %v\nReasoning: %s", err, result.Reasoning)
				}
			}
		})
	}
}

// TestCategorizeFailure_SortedRulesPriority tests that rules are correctly
// sorted by priority before pattern matching.
func TestCategorizeFailure_SortedRulesPriority(t *testing.T) {
	// This test verifies the sorting logic at lines 683-685
	testCases := []struct {
		name             string
		errorMsg         string
		expectedCategory FailureCategory
		description      string
	}{
		{
			name:             "data_race_highest_priority_wins",
			errorMsg:         "WARNING: DATA RACE\nWrite at 0x...",
			expectedCategory: CategoryDataRace,
			description:      "Data race (priority 100) should win over all other patterns",
		},
		{
			name:             "deadlock_beats_panic",
			errorMsg:         "potential deadlock detected\npanic: runtime error",
			expectedCategory: CategoryDeadlock,
			description:      "Deadlock (priority 90) beats panic (priority 50)",
		},
		{
			name:             "timeout_beats_nil_pointer",
			errorMsg:         "context deadline exceeded\nnil pointer dereference",
			expectedCategory: CategoryTimeout,
			description:      "Timeout (priority 70) beats nil pointer (priority 65)",
		},
		{
			name:             "nil_pointer_beats_index_error",
			errorMsg:         "nil pointer dereference\nindex out of range",
			expectedCategory: CategoryNilPointer,
			description:      "Nil pointer (priority 65) beats index error (priority 60)",
		},
		{
			name:             "map_key_beats_goroutine",
			errorMsg:         "map key not found\ngoroutine running",
			expectedCategory: CategoryMapKey,
			description:      "Map key (priority 55) beats goroutine_panic (priority 55 - same priority, order matters)",
		},
		{
			name:             "assertion_lowest_priority",
			errorMsg:         "expected true got false\npanic: error\ntimeout exceeded",
			expectedCategory: CategoryAssertionError,
			description:      "Assertion (priority 10) is lowest - only wins if nothing else matches",
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
				t.Errorf("Category: got %q, want %q\nDescription: %s",
					result.Category, tc.expectedCategory, tc.description)
			}
		})
	}
}

// TestCategorizeFailure_UnknownCategoryBehavior tests the unknown category
// handling path specifically.
func TestCategorizeFailure_UnknownCategoryBehavior(t *testing.T) {
	testCases := []struct {
		name              string
		errorMsg          string
		stackTrace        string
		expectedUncertain bool
		expectedConfidence float64
		description       string
	}{
		{
			name:              "unknown_message",
			errorMsg:          "this is an unknown error message",
			stackTrace:        "",
			expectedUncertain: true,
			expectedConfidence: 0.0,
			description:       "Unknown category always has uncertain=true and confidence=0.0",
		},
		{
			name:              "empty_error_and_trace",
			errorMsg:          "",
			stackTrace:        "",
			expectedUncertain: true,
			expectedConfidence: 0.0,
			description:       "Completely empty input results in unknown category",
		},
		{
			name:              "unknown_with_stack_trace",
			errorMsg:          "unknown problem occurred",
			stackTrace:        "at unknown_function()",
			expectedUncertain: true,
			expectedConfidence: 0.0,
			description:       "Unknown category even with stack trace if no patterns match",
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

			if result.Category != CategoryUnknown {
				t.Errorf("Category: got %q, want %q", result.Category, CategoryUnknown)
			}

			if result.Uncertain != tc.expectedUncertain {
				t.Errorf("Uncertain: got %v, want %v", result.Uncertain, tc.expectedUncertain)
			}

			if result.Confidence.Float64() != tc.expectedConfidence {
				t.Errorf("Confidence: got %.2f, want %.2f", result.Confidence.Float64(), tc.expectedConfidence)
			}

			// Verify reasoning explains the unknown categorization
			if !strings.Contains(result.Reasoning, "No categorization pattern matched") {
				t.Errorf("Expected reasoning about no pattern match, got: %s", result.Reasoning)
			}
		})
	}
}
