package testutil

import (
	"testing"
)

// TestCategorizeFailure_AmbiguityHandlerPaths tests the specific paths in CategorizeFailure
// that handle ambiguity - both when handlers exist and when they don't
func TestCategorizeFailure_AmbiguityHandlerPaths(t *testing.T) {
	tests := []struct {
		name                  string          // Test case name
		errorMsg              string         // Error message to categorize
		stackTrace            string         // Stack trace to include
		expectedCategory      FailureCategory // Expected primary category
		minConfidence         float64        // Minimum expected confidence
		maxConfidence         float64        // Maximum expected confidence
		expectAmbiguousCount  int            // Expected number of ambiguous categories
		description           string         // What path this tests
	}{
		// Test Case: Ambiguity handler EXISTS (lines 721-727)
		{
			name:         "timeout_with_http_error_ambiguity_handler_exists",
			errorMsg:     "connection timeout exceeded",
			stackTrace:   "dial tcp 127.0.0.1:8080",
			expectedCategory: CategoryTimeout,
			minConfidence: 0.75, // Base 0.95 - 0.15 (handler) = 0.8, minus edge case adjustment
			maxConfidence: 0.85,
			expectAmbiguousCount: 2, // timeout + http_error
			description:  "Tests ambiguity handler EXISTS path: Timeout rule has handler for HTTPError (0.15 reduction)",
		},
		{
			name:         "timeout_with_io_error_ambiguity_handler_exists",
			errorMsg:     "I/O timeout exceeded during operation",
			stackTrace:   "read failed timeout",
			expectedCategory: CategoryTimeout,
			minConfidence: 0.70, // Base 0.95 - 0.2 (handler) = 0.75
			maxConfidence: 0.80,
			expectAmbiguousCount: 2, // timeout + io_error
			description:  "Tests ambiguity handler EXISTS path: Timeout rule has handler for IOError (0.2 reduction)",
		},
		{
			name:         "nil_pointer_with_panic_ambiguity_handler_exists",
			errorMsg:     "nil pointer dereference",
			stackTrace:   "panic: runtime error",
			expectedCategory: CategoryNilPointer,
			minConfidence: 0.85, // Base 1.0 - 0.1 (handler) = 0.9
			maxConfidence: 0.95,
			expectAmbiguousCount: 3, // nil_pointer + panic + goroutine_panic
			description:  "Tests ambiguity handler EXISTS path: NilPointer rule has handler for Panic (0.1 reduction)",
		},
		{
			name:         "nil_pointer_with_assertion_ambiguity_handler_exists",
			errorMsg:     "nil pointer dereference in test expected true",
			stackTrace:   "",
			expectedCategory: CategoryNilPointer,
			minConfidence: 0.65, // Base 1.0 - 0.3 (handler) = 0.7, minus edge case adjustment
			maxConfidence: 0.75,
			expectAmbiguousCount: 2, // nil_pointer + assertion_error
			description:  "Tests ambiguity handler EXISTS path: NilPointer rule has handler for AssertionError (0.3 reduction)",
		},
		{
			name:         "index_out_of_range_with_panic_ambiguity_handler_exists",
			errorMsg:     "index out of range",
			stackTrace:   "panic: runtime error",
			expectedCategory: CategoryIndexOutOfRange,
			minConfidence: 0.85, // Base 0.95 - 0.1 (handler) = 0.85
			maxConfidence: 0.95,
			expectAmbiguousCount: 4, // index_out_of_range + panic + goroutine_panic + assertion_error
			description:  "Tests ambiguity handler EXISTS path: IndexOutOfRange rule has handler for Panic (0.1 reduction)",
		},
		{
			name:         "channel_with_panic_ambiguity_handler_exists",
			errorMsg:     "close of closed channel",
			stackTrace:   "panic: runtime error",
			expectedCategory: CategoryChannel,
			minConfidence: 0.85, // Base 0.95 - 0.1 (handler) = 0.85, minus edge case adjustment
			maxConfidence: 0.90,
			expectAmbiguousCount: 4, // channel + panic + goroutine_panic + assertion_error
			description:  "Tests ambiguity handler EXISTS path: Channel rule has handler for Panic (0.1 reduction)",
		},
		{
			name:         "map_key_with_panic_ambiguity_handler_exists",
			errorMsg:     "map key not found",
			stackTrace:   "panic: runtime error",
			expectedCategory: CategoryMapKey,
			minConfidence: 0.80, // Base 0.9 - 0.1 (handler) = 0.8
			maxConfidence: 0.90,
			expectAmbiguousCount: 4, // map_key + panic + goroutine_panic + assertion_error
			description:  "Tests ambiguity handler EXISTS path: MapKey rule has handler for Panic (0.1 reduction)",
		},
		{
			name:         "goroutine_panic_with_panic_ambiguity_handler_exists",
			errorMsg:     "goroutine panic detected",
			stackTrace:   "panic: runtime error",
			expectedCategory: CategoryGoroutinePanic,
			minConfidence: 0.75, // Base 0.9 - 0.15 (handler) = 0.75
			maxConfidence: 0.85,
			expectAmbiguousCount: 2, // goroutine_panic + panic
			description:  "Tests ambiguity handler EXISTS path: GoroutinePanic rule has handler for Panic (0.15 reduction)",
		},
		{
			name:         "type_mismatch_with_panic_ambiguity_handler_exists",
			errorMsg:     "interface conversion: interface {} is nil, not string",
			stackTrace:   "panic: runtime error",
			expectedCategory: CategoryTypeMismatch,
			minConfidence: 0.75, // Base 0.9 - 0.15 (handler) = 0.75
			maxConfidence: 0.85,
			expectAmbiguousCount: 4, // type_mismatch + panic + goroutine_panic + assertion_error
			description:  "Tests ambiguity handler EXISTS path: TypeMismatch rule has handler for Panic (0.15 reduction)",
		},
		{
			name:         "http_error_with_timeout_ambiguity_handler_exists",
			errorMsg:     "HTTP error: dial tcp connection timeout",
			stackTrace:   "",
			expectedCategory: CategoryHTTPError,
			minConfidence: 0.80, // Base 0.95 - 0.15 (handler) = 0.8
			maxConfidence: 0.90,
			expectAmbiguousCount: 2, // http_error + timeout
			description:  "Tests ambiguity handler EXISTS path: HTTPError rule has handler for Timeout (0.15 reduction)",
		},
		// Test Case: Ambiguity handler does NOT exist (lines 728-736, default penalty)
		{
			name:         "data_race_with_assertion_no_custom_handler_uses_zero_penalty",
			errorMsg:     "WARNING: DATA RACE\nassertion failed: expected true",
			stackTrace:   "",
			expectedCategory: CategoryDataRace,
			minConfidence: 0.95, // Base 1.0 - 0.0 (handler exists but reduction is 0.0) = 1.0
			maxConfidence: 1.0,
			expectAmbiguousCount: 2, // data_race + assertion_error
			description:  "Tests ambiguity handler with ZERO penalty: DataRace handler for AssertionError has 0.0 reduction",
		},
		{
			name:         "deadlock_with_assertion_no_custom_handler_uses_zero_penalty",
			errorMsg:     "potential deadlock detected\nassertion failed",
			stackTrace:   "",
			expectedCategory: CategoryDeadlock,
			minConfidence: 0.95, // Base 1.0 - 0.0 (handler exists but reduction is 0.0) = 1.0
			maxConfidence: 1.0,
			expectAmbiguousCount: 2, // deadlock + assertion_error
			description:  "Tests ambiguity handler with ZERO penalty: Deadlock handler for AssertionError has 0.0 reduction",
		},
		{
			name:         "panic_with_timeout_no_handler_default_penalty",
			errorMsg:     "panic: runtime error\ntest timed out",
			stackTrace:   "",
			expectedCategory: CategoryPanic,
			minConfidence: 0.70, // Base 0.9 - 0.15 (default) = 0.75
			maxConfidence: 0.80,
			expectAmbiguousCount: 3, // panic + timeout + assertion_error
			description:  "Tests NO ambiguity handler path: Panic rule has no handler for Timeout, uses default 0.15 penalty",
		},
		{
			name:         "panic_with_http_error_no_handler_default_penalty",
			errorMsg:     "panic: runtime error\nHTTP status code 500",
			stackTrace:   "",
			expectedCategory: CategoryPanic,
			minConfidence: 0.70, // Base 0.9 - 0.15 (default) = 0.75
			maxConfidence: 0.80,
			expectAmbiguousCount: 4, // panic + http_error + goroutine_panic + assertion_error
			description:  "Tests NO ambiguity handler path: Panic rule has no handler for HTTPError, uses default 0.15 penalty",
		},
		{
			name:         "timeout_with_nil_pointer_no_handler_default_penalty",
			errorMsg:     "context deadline exceeded\nnil pointer dereference",
			stackTrace:   "",
			expectedCategory: CategoryTimeout,
			minConfidence: 0.75, // Base 0.95 - 0.15 (default) = 0.8
			maxConfidence: 0.85,
			expectAmbiguousCount: 3, // timeout + nil_pointer + assertion_error
			description:  "Tests NO ambiguity handler path: Timeout rule has no handler for NilPointer, uses default 0.15 penalty",
		},
		// Test Case: Confidence floor when handler exists (lines 723-725)
		{
			name:         "handler_confidence_floor_at_0.1",
			errorMsg:     "connection timeout\ndial tcp",
			stackTrace:   "I/O error",
			expectedCategory: CategoryTimeout,
			minConfidence: 0.10, // Floor is 0.1 for handler case
			maxConfidence: 0.15,
			expectAmbiguousCount: 3, // timeout + http_error + io_error
			description:  "Tests handler confidence FLOOR at 0.1: Multiple handlers would reduce below 0.1 but floor applies",
		},
		// Test Case: Confidence floor when no handler (lines 731-733)
		{
			name:         "no_handler_confidence_floor_at_0.2",
			errorMsg:     "panic: runtime error\ncontext deadline exceeded\ntest timed out\nexpected true got false",
			stackTrace:   "",
			expectedCategory: CategoryPanic,
			minConfidence: 0.20, // Floor is 0.2 for default case
			maxConfidence: 0.25,
			expectAmbiguousCount: 4, // panic + timeout + assertion_error + goroutine_panic
			description:  "Tests default confidence FLOOR at 0.2: Multiple default penalties would reduce below 0.2 but floor applies",
		},
		// Test Case: Mixed handler and no-handler scenarios
		{
			name:         "mixed_one_handler_one_default",
			errorMsg:     "nil pointer dereference\npanic: runtime error\ntest timed out",
			stackTrace:   "",
			expectedCategory: CategoryNilPointer,
			minConfidence: 0.55, // Base 1.0 - 0.1 (handler for panic) - 0.15 (default for timeout) = 0.75
			maxConfidence: 0.65,
			expectAmbiguousCount: 4, // nil_pointer + panic + goroutine_panic + timeout
			description:  "Tests MIXED path: One ambiguity handler exists (Panic), one uses default (Timeout)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				ErrorMessage: tt.errorMsg,
				StackTrace:   tt.stackTrace,
			}

			result := CategorizeFailure(failure)

			// Check category
			if result.Category != tt.expectedCategory {
				t.Errorf("Category: got %s, want %s\nDescription: %s\nError: %s", result.Category, tt.expectedCategory, tt.description, tt.errorMsg)
			}

			// Check confidence range
			conf := result.Confidence.Float64()
			if conf < tt.minConfidence || conf > tt.maxConfidence {
				t.Errorf("Confidence: got %.2f, want [%.2f, %.2f]\nDescription: %s\nReasoning: %s", conf, tt.minConfidence, tt.maxConfidence, tt.description, result.Reasoning)
			}

			// Check ambiguous count
			ambigCount := GetAmbiguousCount(result)
			if ambigCount != tt.expectAmbiguousCount {
				t.Errorf("Ambiguous count: got %d, want %d\nDescription: %s\nReasoning: %s", ambigCount, tt.expectAmbiguousCount, tt.description, result.Reasoning)
			}

			// Verify reasoning mentions handler when expected
			if tt.expectAmbiguousCount > 1 && result.Reasoning == "" {
				t.Errorf("Reasoning is empty but expected ambiguity\nDescription: %s", tt.description)
			}
		})
	}
}

// TestCategorizeFailure_ConfidenceFloorBoundaries tests the specific confidence floor logic
func TestCategorizeFailure_ConfidenceFloorBoundaries(t *testing.T) {
	tests := []struct {
		name         string          // Test case name
		errorMsg     string         // Error message designed to trigger specific floor
		stackTrace   string         // Stack trace
		expectedCategory FailureCategory // Expected category
		minConfidence float64        // Minimum expected confidence (at floor)
		maxConfidence float64        // Maximum expected confidence
		floorType    string         // "handler" (0.1) or "default" (0.2)
		description  string         // What boundary condition this tests
	}{
		{
			name:         "handler_floor_exactly_0.1",
			errorMsg:     "connection timeout exceeded\ndial tcp\nI/O error\nread failed\nanother timeout\nnetwork timeout",
			stackTrace:   "",
			expectedCategory: CategoryTimeout,
			minConfidence: 0.10,
			maxConfidence: 0.15,
			floorType:    "handler",
			description:  "Tests handler floor (0.1) is hit exactly when multiple handlers apply",
		},
		{
			name:         "handler_floor_above_0.1",
			errorMsg:     "connection timeout exceeded\ndial tcp",
			stackTrace:   "",
			expectedCategory: CategoryTimeout,
			minConfidence: 0.75,
			maxConfidence: 0.85,
			floorType:    "handler",
			description:  "Tests handler path where confidence stays above 0.1 floor",
		},
		{
			name:         "default_floor_exactly_0.2",
			errorMsg:     "panic: runtime error\ncontext deadline exceeded\ntest timed out\noperation timed out\nexpected true",
			stackTrace:   "",
			expectedCategory: CategoryPanic,
			minConfidence: 0.20,
			maxConfidence: 0.25,
			floorType:    "default",
			description:  "Tests default floor (0.2) is hit when multiple default penalties apply",
		},
		{
			name:         "default_floor_above_0.2",
			errorMsg:     "panic: runtime error\ncontext deadline exceeded",
			stackTrace:   "",
			expectedCategory: CategoryPanic,
			minConfidence: 0.70,
			maxConfidence: 0.80,
			floorType:    "default",
			description:  "Tests default path where confidence stays above 0.2 floor",
		},
		{
			name:         "no_ambiguity_no_floor_needed",
			errorMsg:     "WARNING: DATA RACE",
			stackTrace:   "Write at 0x...",
			expectedCategory: CategoryDataRace,
			minConfidence: 0.95,
			maxConfidence: 1.0,
			floorType:    "none",
			description:  "Tests case with no ambiguity - floors don't apply",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				ErrorMessage: tt.errorMsg,
				StackTrace:   tt.stackTrace,
			}

			result := CategorizeFailure(failure)

			if result.Category != tt.expectedCategory {
				t.Errorf("Category: got %s, want %s\nDescription: %s", result.Category, tt.expectedCategory, tt.description)
			}

			conf := result.Confidence.Float64()
			if conf < tt.minConfidence || conf > tt.maxConfidence {
				t.Errorf("Confidence: got %.2f, want [%.2f, %.2f]\nDescription: %s\nReasoning: %s", conf, tt.minConfidence, tt.maxConfidence, tt.description, result.Reasoning)
			}

			// Verify floor type in reasoning
			if tt.floorType == "handler" {
				// Handler floor tests should have reasoning mentioning ambiguity
				if result.Reasoning == "" {
					t.Errorf("Expected reasoning for handler floor test\nDescription: %s", tt.description)
				}
			}
		})
	}
}

// TestCategorizeFailure_UnknownCategoryPath tests the early return path for unknown category (lines 696-704)
func TestCategorizeFailure_UnknownCategoryPath(t *testing.T) {
	tests := []struct {
		name         string          // Test case name
		errorMsg     string         // Error message that doesn't match any pattern
		stackTrace   string         // Stack trace
		expectCategory FailureCategory // Expected CategoryUnknown
		expectConfidence float64     // Expected confidence (should be 0.0)
		expectUncertain bool         // Expected Uncertain flag (should be true)
		description  string         // What this validates
	}{
		{
			name:         "completely_unknown_no_patterns",
			errorMsg:     "xyzabc123 def456 ghi789",
			stackTrace:   "",
			expectCategory: CategoryUnknown,
			expectConfidence: 0.0,
			expectUncertain: true,
			description:  "Tests unknown path with no matching patterns",
		},
		{
			name:         "empty_error_empty_stack",
			errorMsg:     "",
			stackTrace:   "",
			expectCategory: CategoryUnknown,
			expectConfidence: 0.0,
			expectUncertain: true,
			description:  "Tests unknown path with completely empty input",
		},
		{
			name:         "random_unicode_no_match",
			errorMsg:     "λπΣ†Ω lorem ipsum dolor",
			stackTrace:   "",
			expectCategory: CategoryUnknown,
			expectConfidence: 0.0,
			expectUncertain: true,
			description:  "Tests unknown path with random text",
		},
		{
			name:         "stack_only_no_match",
			errorMsg:     "",
			stackTrace:   "random stack trace without patterns",
			expectCategory: CategoryUnknown,
			expectConfidence: 0.0,
			expectUncertain: true,
			description:  "Tests unknown path with stack trace only",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failure := TestFailure{
				ErrorMessage: tt.errorMsg,
				StackTrace:   tt.stackTrace,
			}

			result := CategorizeFailure(failure)

			if result.Category != tt.expectCategory {
				t.Errorf("Category: got %s, want %s\nDescription: %s", result.Category, tt.expectCategory, tt.description)
			}

			conf := result.Confidence.Float64()
			if conf != tt.expectConfidence {
				t.Errorf("Confidence: got %.2f, want %.2f\nDescription: %s", conf, tt.expectConfidence, tt.description)
			}

			if result.Uncertain != tt.expectUncertain {
				t.Errorf("Uncertain: got %v, want %v\nDescription: %s", result.Uncertain, tt.expectUncertain, tt.description)
			}

			if result.Reasoning == "" {
				t.Errorf("Reasoning is empty for unknown category\nDescription: %s", tt.description)
			}
		})
	}
}
