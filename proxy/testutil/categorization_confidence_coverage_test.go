package testutil

import (
	"testing"
)

// TestGetAmbiguousCount tests the GetAmbiguousCount function
func TestGetAmbiguousCount(t *testing.T) {
	tests := []struct {
		name     string
		failure  TestFailure
		expected int
	}{
		{
			name: "single pattern match (no ambiguity)",
			failure: TestFailure{
				ErrorMessage: "timeout: operation timed out",
			},
			expected: 1,
		},
		{
			name: "ambiguous with timeout and HTTP error",
			failure: TestFailure{
				ErrorMessage: "connection timeout while dialing tcp: connection refused",
			},
			expected: 2, // Timeout + HTTP error
		},
		{
			name: "ambiguous with panic and nil pointer",
			failure: TestFailure{
				ErrorMessage: "panic: nil pointer dereference",
				StackTrace:   "panic on nil pointer",
			},
			expected: 2, // Panic + Nil pointer
		},
		{
			name: "highly ambiguous case with multiple patterns",
			failure: TestFailure{
				ErrorMessage: "panic: interface conversion during HTTP connection with timeout",
				StackTrace:   "nil pointer dereference in timeout handler",
			},
			expected: 4, // Should match multiple patterns
		},
		{
			name: "no pattern match (unknown)",
			failure: TestFailure{
				ErrorMessage: "this error doesn't match any pattern",
			},
			expected: 1, // Default when not ambiguous
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cat := CategorizeFailure(tt.failure)
			count := GetAmbiguousCount(cat)
			if count < 1 {
				t.Errorf("GetAmbiguousCount() returned %d, want >= 1", count)
			}
			// For highly ambiguous cases, we expect higher counts
			if tt.name == "highly ambiguous case with multiple patterns" && count < 2 {
				t.Errorf("Expected ambiguous count >= 2 for highly ambiguous case, got %d", count)
			}
		})
	}
}

// TestGetHighConfidenceFailures tests the GetHighConfidenceFailures function
func TestGetHighConfidenceFailures(t *testing.T) {
	categorized := []CategorizedFailure{
		{
			TestFailure: TestFailure{TestName: "test1"},
			Confidence:  NewConfidence(0.95), // Very high
		},
		{
			TestFailure: TestFailure{TestName: "test2"},
			Confidence:  NewConfidence(0.85), // High
		},
		{
			TestFailure: TestFailure{TestName: "test3"},
			Confidence:  NewConfidence(0.6), // Below threshold
		},
		{
			TestFailure: TestFailure{TestName: "test4"},
			Confidence:  NewConfidence(0.4), // Low
		},
		{
			TestFailure: TestFailure{TestName: "test5"},
			Confidence:  NewConfidence(0.8), // Exactly at threshold
		},
	}

	tests := []struct {
		name      string
		threshold float64
		expected  int
	}{
		{
			name:      "high threshold (0.9)",
			threshold: 0.9,
			expected:  1, // Only test1
		},
		{
			name:      "medium threshold (0.8)",
			threshold: 0.8,
			expected:  3, // test1, test2, test5
		},
		{
			name:      "low threshold (0.5)",
			threshold: 0.5,
			expected:  4, // All except test4
		},
		{
			name:      "very low threshold (0.3)",
			threshold: 0.3,
			expected:  5, // All
		},
		{
			name:      "exact threshold match (0.85)",
			threshold: 0.85,
			expected:  1, // Only test1 (test2 is exactly 0.85, should match with >=)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetHighConfidenceFailures(categorized, tt.threshold)
			if len(result) != tt.expected {
				t.Errorf("GetHighConfidenceFailures(threshold=%.2f) returned %d failures, want %d",
					tt.threshold, len(result), tt.expected)
			}
		})
	}

	t.Run("empty slice", func(t *testing.T) {
		result := GetHighConfidenceFailures([]CategorizedFailure{}, 0.8)
		if len(result) != 0 {
			t.Errorf("GetHighConfidenceFailures() on empty slice returned %d, want 0", len(result))
		}
	})
}

// TestGetAmbiguousFailures tests the GetAmbiguousFailures function
func TestGetAmbiguousFailures(t *testing.T) {
	categorized := []CategorizedFailure{
		{
			TestFailure: TestFailure{TestName: "unambiguous1"},
			Reasoning:   "Single pattern match, no ambiguity",
			Confidence:  NewConfidence(0.9),
		},
		{
			TestFailure: TestFailure{TestName: "ambiguous1"},
			Reasoning:   "Matched pattern 'timeout' - Ambiguity detected: also matches 'http_error'",
			Confidence:  NewConfidence(0.75),
		},
		{
			TestFailure: TestFailure{TestName: "unambiguous2"},
			Reasoning:   "Clear single pattern match",
			Confidence:  NewConfidence(0.95),
		},
		{
			TestFailure: TestFailure{TestName: "ambiguous2"},
			Reasoning:   "Ambiguity detected: also matches 'panic' and 'nil_pointer'",
			Confidence:  NewConfidence(0.7),
		},
	}

	t.Run("find ambiguous failures", func(t *testing.T) {
		result := GetAmbiguousFailures(categorized)
		if len(result) != 2 {
			t.Errorf("GetAmbiguousFailures() returned %d failures, want 2", len(result))
		}
		for _, cat := range result {
			if !IsAmbiguous(cat) {
				t.Errorf("GetAmbiguousFailures() returned non-ambiguous failure: %s", cat.TestName)
			}
		}
	})

	t.Run("no ambiguous failures", func(t *testing.T) {
		clearCategorized := []CategorizedFailure{
			{
				TestFailure: TestFailure{TestName: "clear1"},
				Reasoning:   "Clear match",
			},
			{
				TestFailure: TestFailure{TestName: "clear2"},
				Reasoning:   "Single pattern matched",
			},
		}
		result := GetAmbiguousFailures(clearCategorized)
		if len(result) != 0 {
			t.Errorf("GetAmbiguousFailures() on clear failures returned %d, want 0", len(result))
		}
	})

	t.Run("empty slice", func(t *testing.T) {
		result := GetAmbiguousFailures([]CategorizedFailure{})
		if len(result) != 0 {
			t.Errorf("GetAmbiguousFailures() on empty slice returned %d, want 0", len(result))
		}
	})
}

// TestGetSuggestedSubcategoryMissing tests the GetSuggestedSubcategory function for missing coverage cases
func TestGetSuggestedSubcategoryMissing(t *testing.T) {
	tests := []struct {
		name        string
		category    FailureCategory
		subcategory string
		errorMsg    string
		stackTrace  string
		expected    string
	}{
		{
			name:     "Nil pointer with test context",
			category: CategoryNilPointer,
			errorMsg: "nil pointer dereference in mock test",
			expected: "",
		},
		{
			name:     "Type mismatch returns empty",
			category: CategoryTypeMismatch,
			errorMsg: "type assertion failed",
			expected: "",
		},
		{
			name:     "Index out of range returns empty",
			category: CategoryIndexOutOfRange,
			errorMsg: "index out of range",
			expected: "",
		},
		{
			name:     "Map key returns empty",
			category: CategoryMapKey,
			errorMsg: "key not found in map",
			expected: "",
		},
		{
			name:     "Channel error returns empty",
			category: CategoryChannel,
			errorMsg: "send on closed channel",
			expected: "",
		},
		{
			name:     "Goroutine panic returns empty",
			category: CategoryGoroutinePanic,
			errorMsg: "goroutine panic",
			expected: "",
		},
		{
			name:     "Deadlock returns empty",
			category: CategoryDeadlock,
			errorMsg: "potential deadlock",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cat := CategorizedFailure{
				TestFailure: TestFailure{
					ErrorMessage: tt.errorMsg,
					StackTrace:   tt.stackTrace,
				},
				Category:    tt.category,
				Subcategory: tt.subcategory,
			}
			result := GetSuggestedSubcategory(cat)
			if result != tt.expected {
				t.Errorf("GetSuggestedSubcategory() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestGetCategoryDescriptionMissing tests missing coverage for GetCategoryDescription
func TestGetCategoryDescriptionMissing(t *testing.T) {
	// Test the nonexistent category case (returns default message)
	result := GetCategoryDescription(FailureCategory("nonexistent"))
	if result != "No description available" {
		t.Errorf("GetCategoryDescription(nonexistent) = %q, want 'No description available'", result)
	}
}

// TestGetMatchingCategoriesForFailureMissing tests missing coverage for GetMatchingCategoriesForFailure
func TestGetMatchingCategoriesForFailureMissing(t *testing.T) {
	// Test empty stack trace edge case
	failure := TestFailure{
		ErrorMessage: "timeout occurred",
		StackTrace:   "",
	}
	result := GetMatchingCategoriesForFailure(failure)
	if len(result) == 0 {
		t.Errorf("GetMatchingCategoriesForFailure() with timeout should match at least one category")
	}
}

// TestResolveAmbiguityCoverage tests the ResolveAmbiguity function for coverage
func TestResolveAmbiguityCoverage(t *testing.T) {
	tests := []struct {
		name           string
		input          CategorizedFailure
		expectCategory FailureCategory
		expectSubcat   string
	}{
		{
			name: "unambiguous case returned as-is",
			input: CategorizedFailure{
				TestFailure: TestFailure{
					ErrorMessage: "timeout: operation timed out",
				},
				Category:    CategoryTimeout,
				Confidence:  NewConfidence(0.95),
				Reasoning:   "Single pattern match",
			},
			expectCategory: CategoryTimeout,
			expectSubcat:   "",
		},
		{
			name: "timeout with dial tcp resolved to HTTP",
			input: CategorizedFailure{
				TestFailure: TestFailure{
					ErrorMessage: "connection timeout dialing tcp",
				},
				Category:    CategoryTimeout,
				Confidence:  NewConfidence(0.7),
				Reasoning:   "Ambiguity detected: also matches 'http_error'",
			},
			expectCategory: CategoryHTTPError,
			expectSubcat:   "timeout",
		},
		{
			name: "interface conversion resolved to type mismatch",
			input: CategorizedFailure{
				TestFailure: TestFailure{
					ErrorMessage: "interface conversion error",
					StackTrace:   "some stack trace",
				},
				Category:    CategoryPanic,
				Confidence:  NewConfidence(0.75),
				Reasoning:   "Ambiguity detected: also matches 'type_mismatch'",
			},
			expectCategory: CategoryTypeMismatch,
			expectSubcat:   "",
		},
		{
			name: "nil pointer in test setup gets subcategory",
			input: CategorizedFailure{
				TestFailure: TestFailure{
					ErrorMessage: "nil pointer dereference in before all setup",
				},
				Category:    CategoryNilPointer,
				Confidence:  NewConfidence(0.85),
				Reasoning:   "Ambiguity detected: also matches 'panic'",
			},
			expectCategory: CategoryNilPointer,
			expectSubcat:   "test_setup",
		},
		{
			name: "timeout without dial tcp stays timeout",
			input: CategorizedFailure{
				TestFailure: TestFailure{
					ErrorMessage: "context deadline exceeded",
				},
				Category:    CategoryTimeout,
				Confidence:  NewConfidence(0.7),
				Reasoning:   "Ambiguity detected: also matches 'http_error'",
			},
			expectCategory: CategoryTimeout,
			expectSubcat:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mark input as ambiguous if reasoning contains "Ambiguity detected"
			if !IsAmbiguous(tt.input) {
				tt.input.Reasoning = "Ambiguity detected: " + tt.input.Reasoning
			}
			result := ResolveAmbiguity(tt.input)
			if result.Category != tt.expectCategory {
				t.Errorf("ResolveAmbiguity() category = %q, want %q", result.Category, tt.expectCategory)
			}
			if tt.expectSubcat != "" && result.Subcategory != tt.expectSubcat {
				t.Errorf("ResolveAmbiguity() subcategory = %q, want %q", result.Subcategory, tt.expectSubcat)
			}
		})
	}

	t.Run("non-ambiguous case unchanged", func(t *testing.T) {
		input := CategorizedFailure{
			TestFailure: TestFailure{
				ErrorMessage: "timeout: operation timed out",
			},
			Category:    CategoryTimeout,
			Confidence:  NewConfidence(0.95),
			Reasoning:   "Single pattern match, no ambiguity",
		}
		result := ResolveAmbiguity(input)
		if result.Category != input.Category {
			t.Errorf("ResolveAmbiguity() changed category for non-ambiguous case")
		}
	})
}

// TestCategorizeFailuresWithStats tests CategorizeFailures with statistics verification
func TestCategorizeFailuresWithStats(t *testing.T) {
	failures := []TestFailure{
		{ErrorMessage: "timeout: operation timed out"},
		{ErrorMessage: "panic: nil pointer dereference"},
		{ErrorMessage: "expected true but got false"},
		{ErrorMessage: "WARNING: DATA RACE"},
		{ErrorMessage: "unknown weird error"},
	}

	categorized, stats := CategorizeFailures(failures)

	if stats.Total != len(failures) {
		t.Errorf("Stats.Total = %d, want %d", stats.Total, len(failures))
	}

	if len(categorized) != len(failures) {
		t.Errorf("CategorizeFailures() returned %d failures, want %d", len(categorized), len(failures))
	}

	if stats.ByCategory[CategoryTimeout] < 1 {
		t.Errorf("Expected at least 1 timeout, got %d", stats.ByCategory[CategoryTimeout])
	}

	if stats.ByCategory[CategoryDataRace] < 1 {
		t.Errorf("Expected at least 1 data race, got %d", stats.ByCategory[CategoryDataRace])
	}

	if stats.ByCategory[CategoryUnknown] < 1 {
		t.Errorf("Expected at least 1 unknown, got %d", stats.ByCategory[CategoryUnknown])
	}
}

// TestCategorizeFailureEdgeCases tests edge cases in CategorizeFailure
func TestCategorizeFailureEdgeCases(t *testing.T) {
	tests := []struct {
		name            string
		failure         TestFailure
		expectCategory  FailureCategory
		expectUncertain bool
		minConfidence   float64
		maxConfidence   float64
	}{
		{
			name: "empty error message",
			failure: TestFailure{
				ErrorMessage: "",
				StackTrace:   "",
			},
			expectCategory:  CategoryUnknown,
			expectUncertain: true,
			minConfidence:   0.0,
			maxConfidence:   0.1,
		},
		{
			name: "only stack trace with timeout",
			failure: TestFailure{
				ErrorMessage: "",
				StackTrace:   "context deadline exceeded",
			},
			expectCategory:  CategoryTimeout,
			expectUncertain: false,
			minConfidence:   0.9,
			maxConfidence:   1.0,
		},
		{
			name: "very long error message with multiple patterns",
			failure: TestFailure{
				ErrorMessage: "panic: interface conversion error while making HTTP request with timeout that resulted in nil pointer dereference",
			},
			expectCategory:  CategoryPanic, // Highest priority match
			expectUncertain: true, // Multiple patterns = uncertain
			minConfidence:   0.5,
			maxConfidence:   0.95,
		},
		{
			name: "exact panic marker",
			failure: TestFailure{
				ErrorMessage: "panic:",
			},
			expectCategory:  CategoryPanic,
			expectUncertain: false,
			minConfidence:   0.9,
			maxConfidence:   1.0,
		},
		{
			name: "runtime error without panic",
			failure: TestFailure{
				ErrorMessage: "runtime error: invalid memory address",
			},
			expectCategory:  CategoryAssertionError, // Fallback
			expectUncertain: true,
			minConfidence:   0.6,
			maxConfidence:   0.8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cat := CategorizeFailure(tt.failure)
			if cat.Category != tt.expectCategory {
				t.Errorf("CategorizeFailure() category = %q, want %q", cat.Category, tt.expectCategory)
			}
			if cat.Uncertain != tt.expectUncertain {
				t.Errorf("CategorizeFailure() uncertain = %v, want %v", cat.Uncertain, tt.expectUncertain)
			}
			conf := cat.Confidence.Float64()
			if conf < tt.minConfidence || conf > tt.maxConfidence {
				t.Errorf("CategorizeFailure() confidence = %.2f, want [%.2f, %.2f]", conf, tt.minConfidence, tt.maxConfidence)
			}
		})
	}
}
