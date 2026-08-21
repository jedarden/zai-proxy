package testutil

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// TestCategorizeFailure_AmbiguityConfidenceFloors verifies the defensive
// confidence floors used when a primary category overlaps another category.
// Custom rules keep these otherwise unreachable low-confidence combinations
// deterministic while exercising the same public categorization path.
func TestCategorizeFailure_AmbiguityConfidenceFloors(t *testing.T) {
	tests := []struct {
		name              string
		scenario          string
		rules             []CategorizationRule
		wantCategory      FailureCategory
		wantConfidence    Confidence
		reasoningFragment string
	}{
		{
			name:     "handled ambiguity cannot drop below the explicit 0.10 floor",
			scenario: "category scenario: a low-confidence timeout has a configured I/O ambiguity penalty",
			rules: []CategorizationRule{
				{
					Category:       CategoryTimeout,
					Pattern:        regexp.MustCompile(`fixture`),
					Priority:       2,
					BaseConfidence: 0.05,
					AmbiguityHandlers: map[FailureCategory]ConfidenceAdjustment{
						CategoryIOError: {ReduceBaseBy: 0.10, Reason: "fixture ambiguity"},
					},
				},
				{
					Category:       CategoryIOError,
					Pattern:        regexp.MustCompile(`fixture`),
					Priority:       1,
					BaseConfidence: 0.90,
				},
			},
			wantCategory:      CategoryTimeout,
			wantConfidence:    0.10,
			reasoningFragment: "confidence reduced by 0.10",
		},
		{
			name:     "unhandled ambiguity cannot drop below the default 0.20 floor",
			scenario: "category scenario: a data race overlaps a category without a dedicated ambiguity handler",
			rules: []CategorizationRule{
				{
					Category:       CategoryDataRace,
					Pattern:        regexp.MustCompile(`fixture`),
					Priority:       2,
					BaseConfidence: 0.10,
				},
				{
					Category:       CategoryDeadlock,
					Pattern:        regexp.MustCompile(`fixture`),
					Priority:       1,
					BaseConfidence: 1.00,
				},
			},
			wantCategory:      CategoryDataRace,
			wantConfidence:    0.20,
			reasoningFragment: "default confidence penalty applied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Log(tt.scenario)

			originalRules := categorizationRules
			categorizationRules = tt.rules
			t.Cleanup(func() { categorizationRules = originalRules })

			categorized := CategorizeFailure(Failure{ErrorMessage: "fixture"})
			if categorized.Category != tt.wantCategory {
				t.Fatalf("category = %q, want %q", categorized.Category, tt.wantCategory)
			}
			if categorized.Confidence != tt.wantConfidence {
				t.Errorf("confidence = %.2f, want %.2f", categorized.Confidence, tt.wantConfidence)
			}
			if !strings.Contains(categorized.Reasoning, tt.reasoningFragment) {
				t.Errorf("reasoning = %q, want it to contain %q", categorized.Reasoning, tt.reasoningFragment)
			}
		})
	}
}

// TestCategorizeFailure_NilPointerTestSetupSubcategory verifies that nil-pointer
// failures in setup code retain the test_setup distinction for review tools.
func TestCategorizeFailure_NilPointerTestSetupSubcategory(t *testing.T) {
	tests := []struct {
		name            string
		scenario        string
		errorMessage    string
		stackTrace      string
		wantSubcategory string
	}{
		{
			name:            "mock setup failure",
			scenario:        "category scenario: a nil pointer in mock test setup is marked test_setup",
			errorMessage:    "nil pointer dereference",
			stackTrace:      "mock setup failed",
			wantSubcategory: "test_setup",
		},
		{
			name:            "ordinary production failure",
			scenario:        "category scenario: a nil pointer without test setup context has no subcategory",
			errorMessage:    "nil pointer dereference in request handler",
			wantSubcategory: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Log(tt.scenario)

			categorized := CategorizeFailure(Failure{
				ErrorMessage: tt.errorMessage,
				StackTrace:   tt.stackTrace,
			})
			if categorized.Category != CategoryNilPointer {
				t.Fatalf("category = %q, want %q", categorized.Category, CategoryNilPointer)
			}
			if categorized.Subcategory != tt.wantSubcategory {
				t.Errorf("subcategory = %q, want %q", categorized.Subcategory, tt.wantSubcategory)
			}
		})
	}
}

// TestGetMatchingCategoriesForFailure_StackTraceScenarios confirms matching is
// based on both the parser error message and any stack trace it captured.
func TestGetMatchingCategoriesForFailure_StackTraceScenarios(t *testing.T) {
	tests := []struct {
		name     string
		scenario string
		failure  Failure
		want     []FailureCategory
	}{
		{
			name:     "stack trace only nil pointer",
			scenario: "category scenario: a parser summary is generic but the stack trace identifies a nil pointer",
			failure: Failure{
				ErrorMessage: "test failed",
				StackTrace:   "runtime error: nil pointer dereference",
			},
			want: []FailureCategory{CategoryNilPointer, CategoryPanic},
		},
		{
			name:     "no category in either source",
			scenario: "category scenario: neither error message nor stack trace contains a categorization signal",
			failure: Failure{
				ErrorMessage: "opaque harness failure",
				StackTrace:   "worker exited",
			},
			want: []FailureCategory{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Log(tt.scenario)

			got := GetMatchingCategoriesForFailure(tt.failure)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("matching categories = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestApplyEdgeCaseAdjustments_DefensiveBounds verifies defensive clamping for
// callers that provide confidence outside the supported range.
func TestApplyEdgeCaseAdjustments_DefensiveBounds(t *testing.T) {
	tests := []struct {
		name           string
		scenario       string
		fullText       string
		category       FailureCategory
		baseConfidence float64
		want           float64
	}{
		{
			name:           "overconfident input is capped at one",
			scenario:       "category scenario: defensive input above the maximum confidence is normalized",
			fullText:       "ordinary assertion failure",
			category:       CategoryAssertionError,
			baseConfidence: 1.25,
			want:           1.0,
		},
		{
			name:           "underconfident input is raised to the minimum",
			scenario:       "category scenario: defensive input below the minimum confidence is normalized",
			fullText:       "ordinary assertion failure",
			category:       CategoryAssertionError,
			baseConfidence: 0.01,
			want:           0.05,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Log(tt.scenario)

			if got := applyEdgeCaseAdjustments(tt.fullText, tt.category, tt.baseConfidence); got != tt.want {
				t.Errorf("adjusted confidence = %.2f, want %.2f", got, tt.want)
			}
		})
	}
}
