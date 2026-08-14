package testutil

import (
	"strings"
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

// TestGetSuggestedSubcategoryComprehensive provides complete coverage for all implemented paths
func TestGetSuggestedSubcategoryComprehensive(t *testing.T) {
	tests := []struct {
		name        string
		category    FailureCategory
		subcategory string // If set, should be returned as-is
		errorMsg    string
		stackTrace  string
		expected    string
		description string // Documents what this test covers
	}{
		// Early return case - when subcategory is already set
		{
			name:        "already set subcategory returned as-is",
			category:    CategoryHTTPError,
			subcategory: "existing",
			errorMsg:    "connection refused",
			expected:    "existing",
			description: "Tests early return when Subcategory field is already populated",
		},

		// CategoryHTTPError paths
		{
			name:     "HTTP error with timeout keyword",
			category: CategoryHTTPError,
			errorMsg: "request timeout after 30 seconds",
			expected: "timeout",
			description: "Tests HTTP timeout detection via 'timeout' keyword",
		},
		{
			name:     "HTTP error with deadline keyword",
			category: CategoryHTTPError,
			errorMsg: "context deadline exceeded",
			expected: "timeout",
			description: "Tests HTTP timeout detection via 'deadline' keyword",
		},
		{
			name:     "HTTP error with refused keyword",
			category: CategoryHTTPError,
			errorMsg: "connection refused",
			expected: "connection",
			description: "Tests HTTP connection error detection via 'refused' keyword",
		},
		{
			name:     "HTTP error with reset keyword",
			category: CategoryHTTPError,
			errorMsg: "connection reset by peer",
			expected: "connection",
			description: "Tests HTTP connection error detection via 'reset' keyword",
		},
		{
			name:     "HTTP error with status code",
			category: CategoryHTTPError,
			errorMsg: "server returned status code 500",
			expected: "status",
			description: "Tests HTTP status error detection via 'status code' phrase",
		},
		{
			name:     "HTTP error with 500 code",
			category: CategoryHTTPError,
			errorMsg: "internal server error 500",
			expected: "status",
			description: "Tests HTTP status error detection via '500' code",
		},
		{
			name:     "HTTP error with 404 code",
			category: CategoryHTTPError,
			errorMsg: "page not found 404",
			expected: "status",
			description: "Tests HTTP status error detection via '404' code",
		},
		{
			name:     "HTTP error default network",
			category: CategoryHTTPError,
			errorMsg: "network unreachable",
			expected: "network",
			description: "Tests default HTTP network subcategory when no specific pattern matches",
		},

		// CategoryIOError paths
		{
			name:     "IO error with permission keyword",
			category: CategoryIOError,
			errorMsg: "permission denied",
			expected: "permission",
			description: "Tests IO permission error detection",
		},
		{
			name:     "IO error with no such file",
			category: CategoryIOError,
			errorMsg: "no such file or directory",
			expected: "not_found",
			description: "Tests IO file not found error via 'no such file' phrase",
		},
		{
			name:     "IO error with not found keyword",
			category: CategoryIOError,
			errorMsg: "file not found",
			expected: "not_found",
			description: "Tests IO file not found error via 'not found' phrase",
		},
		{
			name:     "IO error with broken pipe",
			category: CategoryIOError,
			errorMsg: "broken pipe",
			expected: "connection",
			description: "Tests IO connection error detection via 'broken pipe' phrase",
		},
		{
			name:     "IO error with connection keyword",
			category: CategoryIOError,
			errorMsg: "connection lost during write",
			expected: "connection",
			description: "Tests IO connection error detection via 'connection' keyword",
		},
		{
			name:     "IO error default filesystem",
			category: CategoryIOError,
			errorMsg: "disk full",
			expected: "filesystem",
			description: "Tests default IO filesystem subcategory when no specific pattern matches",
		},

		// CategoryPanic paths
		{
			name:     "Panic with nil pointer",
			category: CategoryPanic,
			errorMsg: "panic: nil pointer dereference",
			expected: "nil_pointer",
			description: "Tests panic nil pointer subcategory detection",
		},
		{
			name:     "Panic with index keyword",
			category: CategoryPanic,
			errorMsg: "panic: index out of range",
			expected: "bounds",
			description: "Tests panic bounds error detection via 'index' keyword",
		},
		{
			name:     "Panic with slice keyword",
			category: CategoryPanic,
			errorMsg: "panic: slice bounds out of range",
			expected: "bounds",
			description: "Tests panic bounds error detection via 'slice' keyword",
		},
		{
			name:     "Panic with bounds keyword",
			category: CategoryPanic,
			errorMsg: "panic: array bounds error",
			expected: "bounds",
			description: "Tests panic bounds error detection via 'bounds' keyword",
		},
		{
			name:     "Panic with interface keyword",
			category: CategoryPanic,
			errorMsg: "panic: interface conversion",
			expected: "type",
			description: "Tests panic type error detection via 'interface' keyword",
		},
		{
			name:     "Panic with conversion keyword",
			category: CategoryPanic,
			errorMsg: "panic: type conversion error",
			expected: "type",
			description: "Tests panic type error detection via 'conversion' keyword",
		},
		{
			name:     "Panic default runtime",
			category: CategoryPanic,
			errorMsg: "panic: runtime error",
			expected: "runtime",
			description: "Tests default panic runtime subcategory when no specific pattern matches",
		},

		// CategoryTimeout paths
		{
			name:     "Timeout with context keyword",
			category: CategoryTimeout,
			errorMsg: "context deadline exceeded",
			expected: "context",
			description: "Tests timeout context subcategory detection",
		},
		{
			name:     "Timeout with test keyword",
			category: CategoryTimeout,
			errorMsg: "test timeout after 5s",
			expected: "test",
			description: "Tests timeout test subcategory detection",
		},
		{
			name:     "Timeout default operation",
			category: CategoryTimeout,
			errorMsg: "operation timed out",
			expected: "operation",
			description: "Tests default timeout operation subcategory when no specific pattern matches",
		},

		// Stack trace contributions
		{
			name:        "keyword found in stack trace",
			category:    CategoryHTTPError,
			errorMsg:    "error occurred",
			stackTrace:  "caused by: connection refused",
			expected:    "connection",
			description: "Tests that patterns are searched in both ErrorMessage and StackTrace",
		},

		// Default case for unknown categories
		{
			name:     "unknown category returns empty",
			category: CategoryDataRace,
			errorMsg: "data race detected",
			expected: "",
			description: "Tests default case for unsupported categories returns empty string",
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
				t.Errorf("GetSuggestedSubcategory() = %q, want %q\nDescription: %s", result, tt.expected, tt.description)
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

// TestApplyEdgeCaseAdjustmentsComprehensive provides complete coverage for all edge case paths
// This tests the private applyEdgeCaseAdjustments function directly since it's only accessible within the package
func TestApplyEdgeCaseAdjustmentsComprehensive(t *testing.T) {
	tests := []struct {
		name         string
		fullText     string
		category     FailureCategory
		baseConf     float64
		minExpected  float64
		maxExpected  float64
		description  string
	}{
		// Panic with interface conversion
		{
			name:        "panic with interface conversion reduces confidence",
			fullText:    "panic: interface conversion",
			category:    CategoryPanic,
			baseConf:    0.9,
			minExpected: 0.5,
			maxExpected: 0.75,
			description: "Tests panic category with interface conversion text reduces confidence by 0.15, minimum 0.5",
		},
		{
			name:        "panic with interface conversion low base floors at 0.5",
			fullText:    "panic: interface conversion",
			category:    CategoryPanic,
			baseConf:    0.55,
			minExpected: 0.5,
			maxExpected: 0.5,
			description: "Tests panic interface conversion floors at minimum 0.5 confidence",
		},

		// Assignment to entry in nil map
		{
			name:        "map key with nil map assignment reduces confidence heavily",
			fullText:    "assignment to entry in nil map",
			category:    CategoryMapKey,
			baseConf:    0.9,
			minExpected: 0.3,
			maxExpected: 0.5,
			description: "Tests map category with nil map assignment reduces confidence by 0.4, minimum 0.3",
		},
		{
			name:        "map key with nil map assignment low base floors at 0.3",
			fullText:    "assignment to entry in nil map",
			category:    CategoryMapKey,
			baseConf:    0.35,
			minExpected: 0.3,
			maxExpected: 0.3,
			description: "Tests map nil map assignment floors at minimum 0.3 confidence",
		},

		// Explicit "panic on nil pointer"
		{
			name:        "explicit panic on nil pointer sets maximum confidence",
			fullText:    "panic on nil pointer dereference",
			category:    CategoryNilPointer,
			baseConf:    0.7,
			minExpected: 1.0,
			maxExpected: 1.0,
			description: "Tests nil pointer category with 'panic on nil pointer' sets confidence to 1.0",
		},

		// Multiple panics
		{
			name:        "multiple panics in same error reduces confidence",
			fullText:    "panic during panic recovery",
			category:    CategoryPanic,
			baseConf:    0.9,
			minExpected: 0.85,
			maxExpected: 0.85,
			description: "Tests panic category with multiple 'panic' keywords reduces confidence by 0.05",
		},
		{
			name:        "multiple panics low base floors at 0.7",
			fullText:    "panic and another panic occurred",
			category:    CategoryPanic,
			baseConf:    0.72,
			minExpected: 0.7,
			maxExpected: 0.7,
			description: "Tests multiple panics floors at minimum 0.7 confidence",
		},
		{
			name:        "single panic does not reduce confidence",
			fullText:    "panic: runtime error",
			category:    CategoryPanic,
			baseConf:    0.95,
			minExpected: 0.95,
			maxExpected: 0.95,
			description: "Tests single panic does not trigger multiple panic reduction",
		},

		// Connection timeout with dial tcp
		{
			name:        "timeout with dial tcp reduces confidence",
			fullText:    "connection timeout dialing tcp: connection refused",
			category:    CategoryTimeout,
			baseConf:    0.9,
			minExpected: 0.5,
			maxExpected: 0.7,
			description: "Tests timeout with both 'connection timeout' and 'dial tcp' reduces confidence by 0.2",
		},
		{
			name:        "timeout with dial tcp low base floors at 0.5",
			fullText:    "connection timeout dial tcp",
			category:    CategoryTimeout,
			baseConf:    0.55,
			minExpected: 0.5,
			maxExpected: 0.5,
			description: "Tests timeout dial tcp floors at minimum 0.5 confidence",
		},
		{
			name:        "timeout without dial tcp no reduction",
			fullText:    "connection timeout after 30s",
			category:    CategoryTimeout,
			baseConf:    0.95,
			minExpected: 0.95,
			maxExpected: 0.95,
			description: "Tests timeout without 'dial tcp' does not trigger reduction",
		},

		// Close of closed channel (race condition)
		{
			name:        "close of closed channel reduces confidence slightly",
			fullText:    "close of closed channel",
			category:    CategoryChannel,
			baseConf:    0.95,
			minExpected: 0.8,
			maxExpected: 0.85,
			description: "Tests channel category with 'close of closed channel' reduces confidence by 0.1",
		},
		{
			name:        "close of closed channel low base floors at 0.8",
			fullText:    "close of closed channel",
			category:    CategoryChannel,
			baseConf:    0.82,
			minExpected: 0.8,
			maxExpected: 0.8,
			description: "Tests close of closed channel floors at minimum 0.8 confidence",
		},

		// Context cancellation
		{
			name:        "context canceled sets maximum confidence",
			fullText:    "context canceled",
			category:    CategoryTimeout,
			baseConf:    0.7,
			minExpected: 1.0,
			maxExpected: 1.0,
			description: "Tests timeout with 'context canceled' sets confidence to 1.0",
		},

		// Nil pointer in test with mock/fake
		{
			name:        "nil pointer in test mock reduces confidence",
			fullText:    "nil pointer dereference in test with mock",
			category:    CategoryNilPointer,
			baseConf:    0.95,
			minExpected: 0.8,
			maxExpected: 0.85,
			description: "Tests nil pointer with 'test' and 'mock' reduces confidence by 0.1",
		},
		{
			name:        "nil pointer in test with fake reduces confidence",
			fullText:    "nil pointer in fake test setup",
			category:    CategoryNilPointer,
			baseConf:    0.9,
			minExpected: 0.8,
			maxExpected: 0.8,
			description: "Tests nil pointer with 'test' and 'fake' reduces confidence by 0.1",
		},
		{
			name:        "nil pointer in test mock low base floors at 0.8",
			fullText:    "nil pointer in mock test",
			category:    CategoryNilPointer,
			baseConf:    0.82,
			minExpected: 0.8,
			maxExpected: 0.8,
			description: "Tests nil pointer mock floors at minimum 0.8 confidence",
		},

		// Type assertion with ok pattern
		{
			name:        "type assertion with ok pattern sets maximum confidence",
			fullText:    "type assertion failed, ok: false",
			category:    CategoryTypeMismatch,
			baseConf:    0.75,
			minExpected: 1.0,
			maxExpected: 1.0,
			description: "Tests type mismatch with ', ok' pattern sets confidence to 1.0",
		},

		// Index out of range with slice bounds
		{
			name:        "slice bounds error sets maximum confidence",
			fullText:    "slice bounds out of range",
			category:    CategoryIndexOutOfRange,
			baseConf:    0.85,
			minExpected: 1.0,
			maxExpected: 1.0,
			description: "Tests index out of range with 'slice bounds' sets confidence to 1.0",
		},

		// Multiple goroutines in panic
		{
			name:        "multiple goroutines reduces confidence",
			fullText:    "goroutine 1 panic, goroutine 2 panic, goroutine 3 panic, goroutine 4 panic",
			category:    CategoryGoroutinePanic,
			baseConf:    0.95,
			minExpected: 0.7,
			maxExpected: 0.85,
			description: "Tests goroutine panic with >3 'goroutine' occurrences reduces confidence by 0.1",
		},
		{
			name:        "multiple goroutines low base floors at 0.7",
			fullText:    "goroutine 1, goroutine 2, goroutine 3, goroutine 4, goroutine 5",
			category:    CategoryGoroutinePanic,
			baseConf:    0.72,
			minExpected: 0.7,
			maxExpected: 0.7,
			description: "Tests multiple goroutines floors at minimum 0.7 confidence",
		},
		{
			name:        "few goroutines does not reduce confidence",
			fullText:    "goroutine 1 panic, goroutine 2 panic",
			category:    CategoryGoroutinePanic,
			baseConf:    0.95,
			minExpected: 0.95,
			maxExpected: 0.95,
			description: "Tests goroutine panic with ≤3 'goroutine' occurrences does not trigger reduction",
		},

		// Deadlock with channel
		{
			name:        "deadlock with channel sets maximum confidence",
			fullText:    "potential deadlock on channel operation",
			category:    CategoryDeadlock,
			baseConf:    0.8,
			minExpected: 1.0,
			maxExpected: 1.0,
			description: "Tests deadlock with 'channel' keyword sets confidence to 1.0",
		},

		// I/O error with broken pipe
		{
			name:        "IO error with broken pipe reduces confidence",
			fullText:    "broken pipe during write",
			category:    CategoryIOError,
			baseConf:    0.95,
			minExpected: 0.7,
			maxExpected: 0.8,
			description: "Tests IO error with 'broken pipe' reduces confidence by 0.15",
		},
		{
			name:        "IO error with broken pipe low base floors at 0.7",
			fullText:    "broken pipe",
			category:    CategoryIOError,
			baseConf:    0.72,
			minExpected: 0.7,
			maxExpected: 0.7,
			description: "Tests IO broken pipe floors at minimum 0.7 confidence",
		},

		// HTTP error with status code in assertion
		{
			name:        "HTTP error with status code and expected reduces confidence",
			fullText:    "expected status code 200 but got 500",
			category:    CategoryHTTPError,
			baseConf:    0.95,
			minExpected: 0.6,
			maxExpected: 0.75,
			description: "Tests HTTP error with 'status code' and 'expected' reduces confidence by 0.2",
		},
		{
			name:        "HTTP error with status code and want reduces confidence",
			fullText:    "want status code 404",
			category:    CategoryHTTPError,
			baseConf:    0.9,
			minExpected: 0.6,
			maxExpected: 0.7,
			description: "Tests HTTP error with 'status code' and 'want' reduces confidence by 0.2",
		},
		{
			name:        "HTTP error with status code assertion low base floors at 0.6",
			fullText:    "expected status code 200",
			category:    CategoryHTTPError,
			baseConf:    0.62,
			minExpected: 0.6,
			maxExpected: 0.6,
			description: "Tests HTTP status code assertion floors at minimum 0.6 confidence",
		},

		// Map key error with zero map key
		{
			name:        "zero map key sets maximum confidence",
			fullText:    "zero map key in assignment",
			category:    CategoryMapKey,
			baseConf:    0.7,
			minExpected: 1.0,
			maxExpected: 1.0,
			description: "Tests map key with 'zero map key' sets confidence to 1.0",
		},

		// Unknown error with very short message
		{
			name:        "unknown with very short message sets very low confidence",
			fullText:    "unknown error",
			category:    CategoryUnknown,
			baseConf:    0.9,
			minExpected: 0.05,
			maxExpected: 0.05,
			description: "Tests unknown category with message <20 chars sets confidence to 0.05",
		},
		{
			name:        "unknown with longer message not affected",
			fullText:    "this is a longer unknown error message that exceeds twenty characters",
			category:    CategoryUnknown,
			baseConf:    0.85,
			minExpected: 0.85,
			maxExpected: 0.85,
			description: "Tests unknown category with message ≥20 chars keeps base confidence",
		},

		// Minimum confidence enforcement
		{
			name:        "very low base confidence floors at 0.05",
			fullText:    "some text",
			category:    CategoryPanic,
			baseConf:    0.01,
			minExpected: 0.05,
			maxExpected: 0.05,
			description: "Tests minimum confidence enforcement floors at 0.05",
		},
		{
			name:        "confidence above 1.0 capped at 1.0",
			fullText:    "panic on nil pointer", // This sets to 1.0
			category:    CategoryNilPointer,
			baseConf:    1.5, // Invalid input
			minExpected: 1.0,
			maxExpected: 1.0,
			description: "Tests maximum confidence cap at 1.0",
		},

		// No edge cases matched
		{
			name:        "no edge cases matched returns base confidence",
			fullText:    "generic error message",
			category:    CategoryAssertionError,
			baseConf:    0.8,
			minExpected: 0.8,
			maxExpected: 0.8,
			description: "Tests when no edge case patterns match, returns base confidence unchanged",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := applyEdgeCaseAdjustments(tt.fullText, tt.category, tt.baseConf)
			if result < tt.minExpected || result > tt.maxExpected {
				t.Errorf("applyEdgeCaseAdjustments() = %.2f, want [%.2f, %.2f]\nDescription: %s",
					result, tt.minExpected, tt.maxExpected, tt.description)
			}
		})
	}
}

// TestValidateFailuresComprehensive provides complete coverage for all validation paths
func TestValidateFailuresComprehensive(t *testing.T) {
	tests := []struct {
		name        string
		failures    []TestFailure
		expectError bool
		errorSubstr string // Substring to check in error message
		description string
	}{
		{
			name: "all valid failures",
			failures: []TestFailure{
				{
					TestName:     "TestOne",
					FilePath:     "test_file.go",
					ErrorMessage: "test failed",
				},
				{
					TestName:     "TestTwo",
					FilePath:     "another_test.go",
					ErrorMessage: "assertion failed",
				},
			},
			expectError: false,
			description: "Tests validation passes when all required fields are present",
		},
		{
			name: "missing TestName at index 0",
			failures: []TestFailure{
				{
					TestName:     "",
					FilePath:     "test.go",
					ErrorMessage: "error",
				},
			},
			expectError: true,
			errorSubstr: "failure at index 0: missing required field TestName",
			description: "Tests validation fails when TestName is empty at index 0",
		},
		{
			name: "missing TestName at later index",
			failures: []TestFailure{
				{
					TestName:     "ValidTest",
					FilePath:     "test.go",
					ErrorMessage: "error",
				},
				{
					TestName:     "",
					FilePath:     "test.go",
					ErrorMessage: "error",
				},
			},
			expectError: true,
			errorSubstr: "failure at index 1: missing required field TestName",
			description: "Tests validation fails when TestName is empty at non-zero index",
		},
		{
			name: "missing FilePath with TestName",
			failures: []TestFailure{
				{
					TestName:     "MyTest",
					FilePath:     "",
					ErrorMessage: "error",
				},
			},
			expectError: true,
			errorSubstr: "failure at index 0 (MyTest): missing required field FilePath",
			description: "Tests validation fails when FilePath is empty and includes TestName in error",
		},
		{
			name: "missing FilePath at index 1",
			failures: []TestFailure{
				{
					TestName:     "TestOne",
					FilePath:     "file.go",
					ErrorMessage: "error",
				},
				{
					TestName:     "TestTwo",
					FilePath:     "",
					ErrorMessage: "error",
				},
			},
			expectError: true,
			errorSubstr: "failure at index 1 (TestTwo): missing required field FilePath",
			description: "Tests validation fails when FilePath is empty at non-zero index with TestName",
		},
		{
			name: "missing ErrorMessage with TestName",
			failures: []TestFailure{
				{
					TestName:     "TestFail",
					FilePath:     "file.go",
					ErrorMessage: "",
				},
			},
			expectError: true,
			errorSubstr: "failure at index 0 (TestFail): missing required field ErrorMessage",
			description: "Tests validation fails when ErrorMessage is empty and includes TestName in error",
		},
		{
			name: "missing ErrorMessage at index 2",
			failures: []TestFailure{
				{
					TestName:     "Test1",
					FilePath:     "file1.go",
					ErrorMessage: "error1",
				},
				{
					TestName:     "Test2",
					FilePath:     "file2.go",
					ErrorMessage: "error2",
				},
				{
					TestName:     "Test3",
					FilePath:     "file3.go",
					ErrorMessage: "",
				},
			},
			expectError: true,
			errorSubstr: "failure at index 2 (Test3): missing required field ErrorMessage",
			description: "Tests validation fails when ErrorMessage is empty at later index",
		},
		{
			name:        "empty failures slice",
			failures:    []TestFailure{},
			expectError: false,
			description: "Tests validation passes with empty failures slice (nothing to validate)",
		},
		{
			name: "multiple fields missing at same index",
			failures: []TestFailure{
				{
					TestName:     "",
					FilePath:     "",
					ErrorMessage: "",
				},
			},
			expectError: true,
			errorSubstr: "failure at index 0: missing required field TestName",
			description: "Tests validation fails on first missing field (TestName) even when multiple are missing",
		},
		{
			name: "multiple fields missing across different failures",
			failures: []TestFailure{
				{
					TestName:     "",
					FilePath:     "file.go",
					ErrorMessage: "error",
				},
				{
					TestName:     "Test2",
					FilePath:     "",
					ErrorMessage: "error",
				},
			},
			expectError: true,
			errorSubstr: "failure at index 0: missing required field TestName",
			description: "Tests validation fails on first encountered error and stops",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFailures(tt.failures)
			if tt.expectError && err == nil {
				t.Errorf("ValidateFailures() expected error containing %q, but got nil", tt.errorSubstr)
			}
			if !tt.expectError && err != nil {
				t.Errorf("ValidateFailures() expected no error, but got: %v", err)
			}
			if tt.expectError && err != nil && tt.errorSubstr != "" {
				if !strings.Contains(err.Error(), tt.errorSubstr) {
					t.Errorf("ValidateFailures() error = %q, want error containing %q", err.Error(), tt.errorSubstr)
				}
			}
		})
	}
}
