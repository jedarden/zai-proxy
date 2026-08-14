package testutil

import (
	"strings"
	"testing"
)

// TestCategorizeFailure_AllCategoriesTableDriven is a comprehensive table-driven test
// that verifies categorization behavior across all category types with documented scenarios.
// This test ensures each category pattern correctly identifies its failure type.
func TestCategorizeFailure_AllCategoriesTableDriven(t *testing.T) {
	tests := []struct {
		name              string   // Test name describing the scenario
		errorMsg          string   // Error message to categorize
		stackTrace        string   // Optional stack trace
		expectedCategory  FailureCategory // Expected category
		minConfidence     float64 // Minimum acceptable confidence
		maxConfidence     float64 // Maximum acceptable confidence
		shouldHaveAmbiguity bool   // Whether ambiguity should be detected
		description       string   // Detailed explanation of what scenario tests
	}{
		// DATA RACE - Highest priority (100)
		{
			name: "data_race_warning_explicit",
			errorMsg: `WARNING: DATA RACE
Write at 0x7f8a1b2c3d4e by goroutine 7:
  previous write at 0x7f8a1b2c3d4e by goroutine 6`,
			expectedCategory: CategoryDataRace,
			minConfidence:    1.0,
			maxConfidence:    1.0,
			shouldHaveAmbiguity: false,
			description: "Tests explicit data race detector output - highest confidence as unambiguous",
		},
		{
			name: "data_race_with_assertion_text",
			errorMsg: `WARNING: DATA RACE
Read at 0x...
assertion failed: expected true, got false`,
			expectedCategory: CategoryDataRace,
			minConfidence:    1.0,
			maxConfidence:    1.0,
			shouldHaveAmbiguity: true,
			description: "Tests data race takes precedence over assertion - should detect ambiguity but remain high confidence",
		},

		// DEADLOCK - Priority 90
		{
			name: "deadlock_explicit_detection",
			errorMsg: "potential deadlock detected",
			expectedCategory: CategoryDeadlock,
			minConfidence:    1.0,
			maxConfidence:    1.0,
			shouldHaveAmbiguity: false,
			description: "Tests explicit deadlock detection message - unambiguous",
		},
		{
			name: "deadlock_with_channel_context",
			errorMsg: "potential deadlock detected in channel operation",
			expectedCategory: CategoryDeadlock,
			minConfidence:    1.0,
			maxConfidence:    1.0,
			shouldHaveAmbiguity: false,
			description: "Tests deadlock detection with channel context - should boost to 1.0 confidence",
		},

		// TIMEOUT - Priority 70
		{
			name: "context_deadline_exceeded",
			errorMsg: "context deadline exceeded",
			expectedCategory: CategoryTimeout,
			minConfidence:    0.9,
			maxConfidence:    0.95,
			shouldHaveAmbiguity: false,
			description: "Tests pure context deadline - high confidence timeout",
		},
		{
			name: "context_canceled_specific",
			errorMsg: "context canceled",
			expectedCategory: CategoryTimeout,
			minConfidence:    1.0,
			maxConfidence:    1.0,
			shouldHaveAmbiguity: false,
			description: "Tests context cancellation - specific pattern, maximum confidence",
		},
		{
			name: "operation_timed_out",
			errorMsg: "operation timed out after 30 seconds",
			expectedCategory: CategoryTimeout,
			minConfidence:    0.9,
			maxConfidence:    0.95,
			shouldHaveAmbiguity: false,
			description: "Tests generic operation timeout - high confidence",
		},
		{
			name: "test_timed_out",
			errorMsg: "test timed out after 10s",
			expectedCategory: CategoryTimeout,
			minConfidence:    0.9,
			maxConfidence:    0.95,
			shouldHaveAmbiguity: false,
			description: "Tests explicit test timeout - high confidence",
		},

		// NIL POINTER - Priority 65
		{
			name: "nil_pointer_dereference_explicit",
			errorMsg: "nil pointer dereference",
			expectedCategory: CategoryNilPointer,
			minConfidence:    1.0,
			maxConfidence:    1.0,
			shouldHaveAmbiguity: false,
			description: "Tests explicit nil pointer message - unambiguous",
		},
		{
			name: "panic_on_nil_pointer",
			errorMsg: "panic on nil pointer",
			expectedCategory: CategoryNilPointer,
			minConfidence:    1.0,
			maxConfidence:    1.0,
			shouldHaveAmbiguity: false,
			description: "Tests panic on nil pointer - maximum confidence for explicit pattern",
		},
		{
			name: "assignment_to_nil_map",
			errorMsg: "assignment to entry in nil map",
			expectedCategory: CategoryNilPointer,
			minConfidence:    0.6,
			maxConfidence:    0.8,
			shouldHaveAmbiguity: false,
			description: "Tests nil map assignment - should match nil pointer pattern, not map key error",
		},
		{
			name: "nil_pointer_with_test_mock",
			errorMsg: "nil pointer dereference in mock setup during test",
			expectedCategory: CategoryNilPointer,
			minConfidence:    0.8,
			maxConfidence:    0.95,
			shouldHaveAmbiguity: false,
			description: "Tests nil pointer in test with mock context - confidence reduced by 0.1",
		},

		// INDEX OUT OF RANGE - Priority 60
		{
			name: "index_out_of_range_explicit",
			errorMsg: "index out of range [5] with length 3",
			expectedCategory: CategoryIndexOutOfRange,
			minConfidence:    1.0,
			maxConfidence:    1.0,
			shouldHaveAmbiguity: false,
			description: "Tests explicit index out of range - unambiguous",
		},
		{
			name: "slice_bounds_out_of_range",
			errorMsg: "slice bounds out of range [10:15] with length 5",
			expectedCategory: CategoryIndexOutOfRange,
			minConfidence:    1.0,
			maxConfidence:    1.0,
			shouldHaveAmbiguity: false,
			description: "Tests slice bounds error - very specific pattern, maximum confidence",
		},

		// MAP KEY - Priority 55
		{
			name: "map_key_not_found",
			errorMsg: "map key not found: 'config'",
			expectedCategory: CategoryMapKey,
			minConfidence:    0.9,
			maxConfidence:    0.95,
			shouldHaveAmbiguity: false,
			description: "Tests map key not found - high confidence",
		},
		{
			name: "zero_map_key",
			errorMsg: "zero map key in map access",
			expectedCategory: CategoryMapKey,
			minConfidence:    1.0,
			maxConfidence:    1.0,
			shouldHaveAmbiguity: false,
			description: "Tests zero map key - very specific pattern, maximum confidence",
		},

		// GOROUTINE PANIC - Priority 55
		{
			name: "goroutine_running_pattern",
			errorMsg: "goroutine 1 [running]:",
			stackTrace: "goroutine 19 [running]:\nmain.main()\n\t/main.go:10",
			expectedCategory: CategoryGoroutinePanic,
			minConfidence:    0.85,
			maxConfidence:    0.95,
			shouldHaveAmbiguity: false,
			description: "Tests goroutine stack trace pattern - high confidence",
		},
		{
			name: "leaked_goroutine",
			errorMsg: "leaked goroutine detected",
			expectedCategory: CategoryGoroutinePanic,
			minConfidence:    0.85,
			maxConfidence:    0.95,
			shouldHaveAmbiguity: false,
			description: "Tests leaked goroutine message - high confidence",
		},

		// CHANNEL - Priority 50
		{
			name: "send_on_closed_channel",
			errorMsg: "send on closed channel",
			expectedCategory: CategoryChannel,
			minConfidence:    1.0,
			maxConfidence:    1.0,
			shouldHaveAmbiguity: false,
			description: "Tests send on closed channel - unambiguous",
		},
		{
			name: "close_of_closed_channel",
			errorMsg: "close of closed channel",
			expectedCategory: CategoryChannel,
			minConfidence:    0.85,
			maxConfidence:    0.95,
			shouldHaveAmbiguity: false,
			description: "Tests close of closed channel - high confidence, reduced by 0.1 for race possibility",
		},
		{
			name: "receive_on_closed_channel",
			errorMsg: "receive on closed channel",
			expectedCategory: CategoryChannel,
			minConfidence:    1.0,
			maxConfidence:    1.0,
			shouldHaveAmbiguity: false,
			description: "Tests receive on closed channel - unambiguous",
		},

		// PANIC - Priority 50
		{
			name: "panic_colon_explicit",
			errorMsg: "panic: runtime error",
			expectedCategory: CategoryPanic,
			minConfidence:    1.0,
			maxConfidence:    1.0,
			shouldHaveAmbiguity: false,
			description: "Tests explicit panic: marker - unambiguous",
		},
		{
			name: "runtime_panic",
			errorMsg: "runtime panic: invalid memory address",
			expectedCategory: CategoryPanic,
			minConfidence:    1.0,
			maxConfidence:    1.0,
			shouldHaveAmbiguity: false,
			description: "Tests runtime panic - unambiguous",
		},
		{
			name: "panic_function_call",
			errorMsg: "panic() called",
			expectedCategory: CategoryPanic,
			minConfidence:    1.0,
			maxConfidence:    1.0,
			shouldHaveAmbiguity: false,
			description: "Tests panic() function - unambiguous",
		},

		// TYPE MISMATCH - Priority 45
		{
			name: "interface_conversion",
			errorMsg: "interface conversion: interface {} is string, not int",
			expectedCategory: CategoryTypeMismatch,
			minConfidence:    0.85,
			maxConfidence:    0.95,
			shouldHaveAmbiguity: false,
			description: "Tests interface conversion - high confidence",
		},
		{
			name: "cannot_convert_type",
			errorMsg: "cannot convert string to int",
			expectedCategory: CategoryTypeMismatch,
			minConfidence:    0.85,
			maxConfidence:    0.95,
			shouldHaveAmbiguity: false,
			description: "Tests cannot convert - high confidence",
		},
		{
			name: "type_assertion_failed",
			errorMsg: "type assertion failed",
			expectedCategory: CategoryTypeMismatch,
			minConfidence:    0.85,
			maxConfidence:    0.95,
			shouldHaveAmbiguity: false,
			description: "Tests type assertion failure - high confidence",
		},
		{
			name: "safe_type_assertion_with_ok",
			errorMsg: "type assertion failed, ok = false",
			expectedCategory: CategoryTypeMismatch,
			minConfidence:    1.0,
			maxConfidence:    1.0,
			shouldHaveAmbiguity: false,
			description: "Tests safe type assertion with ,ok pattern - maximum confidence",
		},

		// HTTP ERROR - Priority 40
		{
			name: "http_status_code",
			errorMsg: "HTTP status code 500",
			expectedCategory: CategoryHTTPError,
			minConfidence:    0.8,
			maxConfidence:    0.95,
			shouldHaveAmbiguity: false,
			description: "Tests HTTP status code - high confidence",
		},
		{
			name: "connection_refused",
			errorMsg: "dial tcp 127.0.0.1:8080: connection refused",
			expectedCategory: CategoryHTTPError,
			minConfidence:    0.8,
			maxConfidence:    0.95,
			shouldHaveAmbiguity: false,
			description: "Tests connection refused - high confidence",
		},
		{
			name: "connection_reset",
			errorMsg: "connection reset by peer",
			expectedCategory: CategoryHTTPError,
			minConfidence:    0.8,
			maxConfidence:    0.95,
			shouldHaveAmbiguity: false,
			description: "Tests connection reset - high confidence",
		},
		{
			name: "connection_timeout_with_dial_tcp",
			errorMsg: "dial tcp 127.0.0.1:8080: connection timeout",
			expectedCategory: CategoryHTTPError,
			minConfidence:    0.8,
			maxConfidence:    0.95,
			shouldHaveAmbiguity: false,
			description: "Tests dial tcp with connection timeout - matches HTTP error pattern",
		},

		// I/O ERROR - Priority 35
		{
			name: "no_such_file",
			errorMsg: "no such file or directory: /tmp/test.txt",
			expectedCategory: CategoryIOError,
			minConfidence:    0.85,
			maxConfidence:    0.95,
			shouldHaveAmbiguity: false,
			description: "Tests file not found - high confidence",
		},
		{
			name: "permission_denied",
			errorMsg: "permission denied: /etc/hosts",
			expectedCategory: CategoryIOError,
			minConfidence:    0.85,
			maxConfidence:    0.95,
			shouldHaveAmbiguity: false,
			description: "Tests permission denied - high confidence",
		},
		{
			name: "i_o_error_read_failed",
			errorMsg: "i/o error: read failed",
			expectedCategory: CategoryIOError,
			minConfidence:    0.85,
			maxConfidence:    0.95,
			shouldHaveAmbiguity: false,
			description: "Tests I/O error message - high confidence",
		},
		{
			name: "broken_pipe",
			errorMsg: "broken pipe",
			expectedCategory: CategoryIOError,
			minConfidence:    0.7,
			maxConfidence:    0.85,
			shouldHaveAmbiguity: false,
			description: "Tests broken pipe - moderate confidence due to possible HTTP connection issue",
		},

		// ASSERTION ERROR - Priority 10 (lowest, checked last)
		{
			name: "expected_but_got",
			errorMsg: "expected 200, got 500",
			expectedCategory: CategoryAssertionError,
			minConfidence:    0.7,
			maxConfidence:    0.8,
			shouldHaveAmbiguity: false,
			description: "Tests expected/got pattern - baseline confidence for assertion",
		},
		{
			name: "not_equal",
			errorMsg: "values not equal: expected true, got false",
			expectedCategory: CategoryAssertionError,
			minConfidence:    0.7,
			maxConfidence:    0.8,
			shouldHaveAmbiguity: false,
			description: "Tests not equal pattern - baseline confidence",
		},
		{
			name: "assertion_failed",
			errorMsg: "assertion failed: condition not met",
			expectedCategory: CategoryAssertionError,
			minConfidence:    0.7,
			maxConfidence:    0.8,
			shouldHaveAmbiguity: false,
			description: "Tests assertion failed pattern - baseline confidence",
		},
		{
			name: "should_be",
			errorMsg: "result should be true but was false",
			expectedCategory: CategoryAssertionError,
			minConfidence:    0.7,
			maxConfidence:    0.8,
			shouldHaveAmbiguity: false,
			description: "Tests should be pattern - baseline confidence",
		},

		// UNKNOWN - No pattern matches
		{
			name: "gibberish_no_match",
			errorMsg: "xyz123 abc456 def789",
			expectedCategory: CategoryUnknown,
			minConfidence:    0.0,
			maxConfidence:    0.0,
			shouldHaveAmbiguity: false,
			description: "Tests no pattern match - unknown category with 0 confidence",
		},
		{
			name: "very_short_unknown",
			errorMsg: "err",
			expectedCategory: CategoryUnknown,
			minConfidence:    0.0,
			maxConfidence:    0.1,
			shouldHaveAmbiguity: false,
			description: "Tests very short unknown message - very low confidence",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestCategory",
				FilePath:     "test.go",
				LineNumber:   1,
				ErrorMessage: tt.errorMsg,
				StackTrace:   tt.stackTrace,
			}

			result := CategorizeFailure(failure)

			// Check category
			if result.Category != tt.expectedCategory {
				t.Errorf("Category mismatch: got %q, want %q\nDescription: %s\nError: %s",
					result.Category, tt.expectedCategory, tt.description, tt.errorMsg)
			}

			// Check confidence range
			confidence := result.Confidence.Float64()
			if confidence < tt.minConfidence || confidence > tt.maxConfidence {
				t.Errorf("Confidence out of range: got %.2f, want [%.2f, %.2f]\nDescription: %s",
					confidence, tt.minConfidence, tt.maxConfidence, tt.description)
			}

			// Check ambiguity detection
			hasAmbiguity := strings.Contains(result.Reasoning, "Ambiguity detected")
			if hasAmbiguity != tt.shouldHaveAmbiguity {
				t.Errorf("Ambiguity detection mismatch: got %v, want %v\nDescription: %s\nReasoning: %s",
					hasAmbiguity, tt.shouldHaveAmbiguity, tt.description, result.Reasoning)
			}

			// Verify reasoning exists
			if result.Reasoning == "" {
				t.Errorf("Reasoning should not be empty for test: %s", tt.name)
			}
		})
	}
}

// TestCategorizeFailure_AmbiguousCasesTableDriven tests complex scenarios where
// multiple patterns could match, verifying correct priority and confidence adjustment.
func TestCategorizeFailure_AmbiguousCasesTableDriven(t *testing.T) {
	tests := []struct {
		name              string
		errorMsg          string
		stackTrace        string
		expectedCategory  FailureCategory
		minConfidence     float64
		maxConfidence     float64
		expectedAmbiguousCount int // Number of patterns that should match
		description       string
	}{
		{
			name: "panic_with_nil_pointer_both_match",
			errorMsg: "panic: nil pointer dereference",
			expectedCategory: CategoryNilPointer, // Higher priority (65) than panic (50)
			minConfidence:    0.85,
			maxConfidence:    0.95,
			expectedAmbiguousCount: 2, // Both nil_pointer and panic match
			description: "Tests both panic and nil pointer patterns - nil pointer wins by priority",
		},
		{
			name: "index_error_with_assertion_text",
			errorMsg: "index out of range [5] with length 3: expected value",
			expectedCategory: CategoryIndexOutOfRange, // Higher priority than assertion
			minConfidence:    0.65,
			maxConfidence:    0.8,
			expectedAmbiguousCount: 2, // index_out_of_range and assertion_error
			description: "Tests index error with assertion text - index wins, confidence reduced for ambiguity",
		},
		{
			name: "channel_error_with_goroutine_stack",
			errorMsg: "send on closed channel",
			stackTrace: "goroutine 1 [running]:",
			expectedCategory: CategoryChannel, // Higher priority than goroutine_panic
			minConfidence:    0.8,
			maxConfidence:    0.95,
			expectedAmbiguousCount: 2, // channel and goroutine_panic
			description: "Tests channel error with goroutine trace - channel wins, confidence slightly reduced",
		},
		{
			name: "type_error_with_assertion_and_panic",
			errorMsg: "panic: interface conversion: expected int, got string",
			expectedCategory: CategoryPanic, // Explicit "panic:" wins
			minConfidence:    0.7,
			maxConfidence:    0.9,
			expectedAmbiguousCount: 2, // panic and type_mismatch
			description: "Tests type conversion panic - panic wins, confidence reduced for ambiguity",
		},
		{
			name: "http_error_with_timeout_patterns",
			errorMsg: "dial tcp: connection timeout",
			expectedCategory: CategoryHTTPError, // Matches "dial tcp" pattern
			minConfidence:    0.8,
			maxConfidence:    0.95,
			expectedAmbiguousCount: 1, // Only http_error matches (timeout pattern doesn't match "connection timeout")
			description: "Tests HTTP error that looks like timeout - HTTP pattern matches, no ambiguity",
		},
		{
			name: "nil_pointer_in_test_assertion",
			errorMsg: "nil pointer dereference: expected non-nil value",
			expectedCategory: CategoryNilPointer,
			minConfidence:    0.65,
			maxConfidence:    0.8,
			expectedAmbiguousCount: 2, // nil_pointer and assertion_error
			description: "Tests nil pointer with assertion text - nil pointer wins, confidence reduced",
		},
		{
			name: "map_key_error_with_assertion",
			errorMsg: "map key not found: expected key to exist",
			expectedCategory: CategoryMapKey,
			minConfidence:    0.6,
			maxConfidence:    0.75,
			expectedAmbiguousCount: 2, // map_key and assertion_error
			description: "Tests map key error with assertion - map key wins, confidence reduced",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestAmbiguity",
				FilePath:     "test.go",
				LineNumber:   1,
				ErrorMessage: tt.errorMsg,
				StackTrace:   tt.stackTrace,
			}

			result := CategorizeFailure(failure)

			// Check category
			if result.Category != tt.expectedCategory {
				t.Errorf("Category: got %q, want %q\nDescription: %s",
					result.Category, tt.expectedCategory, tt.description)
			}

			// Check confidence range
			confidence := result.Confidence.Float64()
			if confidence < tt.minConfidence || confidence > tt.maxConfidence {
				t.Errorf("Confidence: got %.2f, want [%.2f, %.2f]\nDescription: %s",
					confidence, tt.minConfidence, tt.maxConfidence, tt.description)
			}

			// Check ambiguous count
			ambiguousCount := GetAmbiguousCount(result)
			if ambiguousCount != tt.expectedAmbiguousCount {
				t.Errorf("Ambiguous count: got %d, want %d\nDescription: %s\nReasoning: %s",
					ambiguousCount, tt.expectedAmbiguousCount, tt.description, result.Reasoning)
			}

			// If expecting ambiguity, verify reasoning mentions it
			if tt.expectedAmbiguousCount > 1 && !strings.Contains(result.Reasoning, "Ambiguity detected") {
				t.Errorf("Expected ambiguity in reasoning, but not found\nDescription: %s\nReasoning: %s",
					tt.description, result.Reasoning)
			}
		})
	}
}

// TestCategorizeFailure_EdgeCaseAdjustmentsTableDriven tests specific edge case
// confidence adjustments implemented in applyEdgeCaseAdjustments.
func TestCategorizeFailure_EdgeCaseAdjustmentsTableDriven(t *testing.T) {
	tests := []struct {
		name              string
		errorMsg          string
		category          FailureCategory
		baseConfidence    float64 // Expected base confidence before adjustments
		minFinalConfidence float64 // Minimum after edge case adjustments
		maxFinalConfidence float64 // Maximum after edge case adjustments
		description       string
	}{
		{
			name: "interface_conversion_panic_penalty",
			errorMsg: "panic: interface conversion: interface {} is string, not int",
			category: CategoryPanic,
			baseConfidence: 1.0,
			minFinalConfidence: 0.8,
			maxFinalConfidence: 0.9,
			description: "Tests panic with interface conversion - confidence reduced by 0.15, floor 0.5",
		},
		{
			name: "close_of_closed_channel_race_penalty",
			errorMsg: "close of closed channel",
			category: CategoryChannel,
			baseConfidence: 1.0,
			minFinalConfidence: 0.85,
			maxFinalConfidence: 0.95,
			description: "Tests close of closed channel - confidence reduced by 0.1 for race possibility",
		},
		{
			name: "context_cancellation_boost",
			errorMsg: "context canceled",
			category: CategoryTimeout,
			baseConfidence: 0.95,
			minFinalConfidence: 1.0,
			maxFinalConfidence: 1.0,
			description: "Tests context cancellation - boosted to maximum confidence",
		},
		{
			name: "nil_pointer_in_test_context_reduction",
			errorMsg: "nil pointer dereference in mock setup",
			category: CategoryNilPointer,
			baseConfidence: 1.0,
			minFinalConfidence: 0.85,
			maxFinalConfidence: 0.95,
			description: "Tests nil pointer in test mock context - confidence reduced by 0.1",
		},
		{
			name: "safe_type_assertion_boost",
			errorMsg: "type assertion failed, ok = false",
			category: CategoryTypeMismatch,
			baseConfidence: 0.9,
			minFinalConfidence: 1.0,
			maxFinalConfidence: 1.0,
			description: "Tests safe type assertion with ,ok - boosted to maximum confidence",
		},
		{
			name: "slice_bounds_specific_boost",
			errorMsg: "slice bounds out of range",
			category: CategoryIndexOutOfRange,
			baseConfidence: 1.0,
			minFinalConfidence: 1.0,
			maxFinalConfidence: 1.0,
			description: "Tests slice bounds error - already at maximum, stays maximum",
		},
		{
			name: "multiple_goroutines_panic_reduction",
			errorMsg: "goroutine 1 [running]\ngoroutine 2 [running]\ngoroutine 3 [running]\ngoroutine 4 [running]",
			category: CategoryGoroutinePanic,
			baseConfidence: 0.9,
			minFinalConfidence: 0.75,
			maxFinalConfidence: 0.85,
			description: "Tests many goroutines (>3) - confidence reduced by 0.1",
		},
		{
			name: "deadlock_with_channel_boost",
			errorMsg: "potential deadlock detected in channel operation",
			category: CategoryDeadlock,
			baseConfidence: 1.0,
			minFinalConfidence: 1.0,
			maxFinalConfidence: 1.0,
			description: "Tests deadlock with channel context - boosted to maximum confidence",
		},
		{
			name: "broken_pipe_connection_ambiguity_reduction",
			errorMsg: "broken pipe",
			category: CategoryIOError,
			baseConfidence: 0.9,
			minFinalConfidence: 0.7,
			maxFinalConfidence: 0.85,
			description: "Tests broken pipe - confidence reduced by 0.15 for possible HTTP error",
		},
		{
			name: "zero_map_key_specific_boost",
			errorMsg: "zero map key",
			category: CategoryMapKey,
			baseConfidence: 0.95,
			minFinalConfidence: 1.0,
			maxFinalConfidence: 1.0,
			description: "Tests zero map key - very specific pattern, boosted to maximum",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestEdgeCase",
				FilePath:     "test.go",
				LineNumber:   1,
				ErrorMessage: tt.errorMsg,
			}

			result := CategorizeFailure(failure)

			// Check category
			if result.Category != tt.category {
				t.Errorf("Category: got %q, want %q", result.Category, tt.category)
			}

			// Check final confidence after edge case adjustments
			confidence := result.Confidence.Float64()
			if confidence < tt.minFinalConfidence || confidence > tt.maxFinalConfidence {
				t.Errorf("Confidence after edge case adjustments: got %.2f, want [%.2f, %.2f]\nDescription: %s",
					confidence, tt.minFinalConfidence, tt.maxFinalConfidence, tt.description)
			}
		})
	}
}

// TestCategorizeFailure_BoundaryConditionsTableDriven tests boundary and edge
// conditions like empty inputs, very long messages, and confidence limits.
func TestCategorizeFailure_BoundaryConditionsTableDriven(t *testing.T) {
	tests := []struct {
		name              string
		errorMsg          string
		stackTrace        string
		expectedCategory  FailureCategory
		minConfidence     float64
		maxConfidence     float64
		description       string
	}{
		{
			name: "empty_error_and_stack",
			errorMsg: "",
			stackTrace: "",
			expectedCategory: CategoryUnknown,
			minConfidence: 0.0,
			maxConfidence: 0.0,
			description: "Tests completely empty input - unknown with 0 confidence",
		},
		{
			name: "error_only_no_stack",
			errorMsg: "timeout occurred",
			stackTrace: "",
			expectedCategory: CategoryTimeout,
			minConfidence: 0.9,
			maxConfidence: 0.95,
			description: "Tests error message only, no stack trace - normal categorization",
		},
		{
			name: "stack_only_no_error",
			errorMsg: "",
			stackTrace: "goroutine 1 [running]:",
			expectedCategory: CategoryGoroutinePanic,
			minConfidence: 0.85,
			maxConfidence: 0.95,
			description: "Tests stack trace only, no error message - uses stack for categorization",
		},
		{
			name: "whitespace_only",
			errorMsg: "   \n\t   ",
			stackTrace: "",
			expectedCategory: CategoryUnknown,
			minConfidence: 0.0,
			maxConfidence: 0.0,
			description: "Tests whitespace-only input - unknown with 0 confidence",
		},
		{
			name: "very_long_gibberish",
			errorMsg: strings.Repeat("abc", 1000), // 3000 chars of gibberish
			stackTrace: "",
			expectedCategory: CategoryUnknown,
			minConfidence: 0.0,
			maxConfidence: 0.0,
			description: "Tests very long gibberish message - unknown with 0 confidence",
		},
		{
			name: "confidence_minimum_floor",
			errorMsg: "some error",
			stackTrace: "",
			expectedCategory: CategoryAssertionError,
			minConfidence: 0.05,
			maxConfidence: 0.8,
			description: "Tests confidence never goes below 0.05 floor",
		},
		{
			name: "confidence_maximum_ceiling",
			errorMsg: "assignment to entry in nil map",
			stackTrace: "",
			expectedCategory: CategoryNilPointer,
			minConfidence: 0.6,
			maxConfidence: 1.0,
			description: "Tests confidence never exceeds 1.0 ceiling",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestBoundary",
				FilePath:     "test.go",
				LineNumber:   1,
				ErrorMessage: tt.errorMsg,
				StackTrace:   tt.stackTrace,
			}

			result := CategorizeFailure(failure)

			if result.Category != tt.expectedCategory {
				t.Errorf("Category: got %q, want %q\nDescription: %s",
					result.Category, tt.expectedCategory, tt.description)
			}

			confidence := result.Confidence.Float64()
			if confidence < tt.minConfidence || confidence > tt.maxConfidence {
				t.Errorf("Confidence: got %.2f, want [%.2f, %.2f]\nDescription: %s",
					confidence, tt.minConfidence, tt.maxConfidence, tt.description)
			}
		})
	}
}

// TestGetMatchingCategoriesForFailure_TableDriven tests the utility function
// that returns all matching categories for a failure, useful for ambiguity analysis.
func TestGetMatchingCategoriesForFailure_TableDriven(t *testing.T) {
	tests := []struct {
		name              string
		errorMsg          string
		stackTrace        string
		expectedMatchCount int
		description       string
	}{
		{
			name: "no_patterns_match",
			errorMsg: "xyz123 gibberish",
			stackTrace: "",
			expectedMatchCount: 0,
			description: "Tests no patterns match - returns empty slice",
		},
		{
			name: "single_pattern_timeout",
			errorMsg: "context deadline exceeded",
			stackTrace: "",
			expectedMatchCount: 1,
			description: "Tests single pattern match - returns one category",
		},
		{
			name: "panic_and_nil_pointer_both_match",
			errorMsg: "panic: nil pointer dereference",
			stackTrace: "",
			expectedMatchCount: 2,
			description: "Tests two patterns match - returns both categories",
		},
		{
			name: "three_patterns_match",
			errorMsg: "panic: index out of range in test",
			stackTrace: "",
			expectedMatchCount: 3,
			description: "Tests three patterns match - panic, index_out_of_range, assertion_error",
		},
		{
			name: "stack_trace_contributes",
			errorMsg: "error occurred",
			stackTrace: "goroutine 1 [running]:",
			expectedMatchCount: 1,
			description: "Tests stack trace contributes to matching - goroutine_panic",
		},
		{
			name: "error_and_stack_combined",
			errorMsg: "timeout:",
			stackTrace: "dial tcp connection refused",
			expectedMatchCount: 2,
			description: "Tests error and stack combined - http_error (dial tcp) and timeout",
		},
		{
			name: "data_race_unambiguous",
			errorMsg: "WARNING: DATA RACE",
			stackTrace: "",
			expectedMatchCount: 1,
			description: "Tests data race is unambiguous - only data_race matches",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestMatching",
				FilePath:     "test.go",
				LineNumber:   1,
				ErrorMessage: tt.errorMsg,
				StackTrace:   tt.stackTrace,
			}

			matches := GetMatchingCategoriesForFailure(failure)

			if len(matches) != tt.expectedMatchCount {
				t.Errorf("Match count: got %d, want %d\nDescription: %s\nMatches: %v",
					len(matches), tt.expectedMatchCount, tt.description, matches)
			}
		})
	}
}

// TestResolveAmbiguity_TableDriven tests the ambiguity resolution function
// that attempts to re-categorize ambiguous cases based on additional context.
func TestResolveAmbiguity_TableDriven(t *testing.T) {
	tests := []struct {
		name              string
		errorMsg          string
		stackTrace        string
		initialCategory   FailureCategory
		expectedResolvedCategory FailureCategory
		minConfidence     float64
		shouldHaveAmbiguity bool
		description       string
	}{
		{
			name: "timeout_with_dial_tcp_resolves_to_http",
			errorMsg: "dial tcp: connection timeout",
			stackTrace: "",
			initialCategory: CategoryTimeout,
			expectedResolvedCategory: CategoryHTTPError,
			minConfidence: 0.7,
			shouldHaveAmbiguity: true,
			description: "Tests timeout with dial tcp resolves to HTTP error",
		},
		{
			name: "no_ambiguity_returns_unchanged",
			errorMsg: "assignment to entry in nil map",
			stackTrace: "",
			initialCategory: CategoryNilPointer,
			expectedResolvedCategory: CategoryNilPointer,
			minConfidence: 0.95,
			shouldHaveAmbiguity: false,
			description: "Tests non-ambiguous case returns unchanged",
		},
		{
			name: "nil_pointer_in_setup_context",
			errorMsg: "nil pointer in test setup",
			stackTrace: "",
			initialCategory: CategoryNilPointer,
			expectedResolvedCategory: CategoryNilPointer,
			minConfidence: 0.95,
			shouldHaveAmbiguity: false,
			description: "Tests nil pointer in setup gets subcategory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				TestName:     "TestResolution",
				FilePath:     "test.go",
				LineNumber:   1,
				ErrorMessage: tt.errorMsg,
				StackTrace:   tt.stackTrace,
			}

			// First categorize
			categorized := CategorizeFailure(failure)

			// Then resolve ambiguity
			resolved := ResolveAmbiguity(categorized)

			// If we expect a resolution, check it happened
			if tt.expectedResolvedCategory != tt.initialCategory {
				// This should be an ambiguous case that got resolved
				if resolved.Category != tt.expectedResolvedCategory {
					t.Errorf("Resolved category: got %q, want %q\nDescription: %s",
						resolved.Category, tt.expectedResolvedCategory, tt.description)
				}
			}

			// Check confidence
			if resolved.Confidence.Float64() < tt.minConfidence {
				t.Errorf("Confidence after resolution: got %.2f, want >= %.2f",
					resolved.Confidence.Float64(), tt.minConfidence)
			}

			// Check ambiguity in reasoning
			hasAmbiguityResolution := strings.Contains(resolved.Reasoning, "Ambiguity resolution")
			if tt.shouldHaveAmbiguity && !hasAmbiguityResolution {
				t.Errorf("Expected ambiguity resolution in reasoning\nDescription: %s\nReasoning: %s",
					tt.description, resolved.Reasoning)
			}
		})
	}
}
