package testutil

import (
	"strings"
	"testing"
)

func TestResolveAmbiguity_PanicNilPointerVsAssertion(t *testing.T) {
	t.Run("explicit runtime panic keeps panic and records nil-pointer subtype", func(t *testing.T) {
		categorized := CategorizeFailure(Failure{ErrorMessage: `panic: runtime error: invalid memory address or nil pointer dereference
assertion failed: expected response, got nil`})
		resolved := ResolveAmbiguity(categorized)

		if resolved.Category != CategoryPanic {
			t.Fatalf("Category = %q, want %q", resolved.Category, CategoryPanic)
		}
		if resolved.Subcategory != "nil_pointer_dereference" {
			t.Fatalf("Subcategory = %q, want nil_pointer_dereference", resolved.Subcategory)
		}
		if !resolved.Uncertain || resolved.Confidence.Float64() >= ConfidenceModerate.Float64() {
			t.Fatalf("confidence = %.2f, want an uncertain score below %.2f", resolved.Confidence, ConfidenceModerate)
		}
		if !strings.Contains(resolved.Reasoning, "explicit panic marker is the primary event") {
			t.Fatalf("Reasoning = %q, want panic decision criterion", resolved.Reasoning)
		}
	})

	t.Run("direct nil pointer remains more specific than generic assertion text", func(t *testing.T) {
		categorized := CategorizeFailure(Failure{ErrorMessage: "nil pointer dereference; assertion failed: expected value, got nil"})
		resolved := ResolveAmbiguity(categorized)

		if resolved.Category != CategoryNilPointer {
			t.Fatalf("Category = %q, want %q", resolved.Category, CategoryNilPointer)
		}
		if resolved.Subcategory != "assertion_context" {
			t.Fatalf("Subcategory = %q, want assertion_context", resolved.Subcategory)
		}
		if !resolved.Uncertain {
			t.Fatalf("Uncertain = false, want true for competing nil-pointer and assertion signals")
		}
		if !strings.Contains(resolved.Reasoning, "nil-pointer diagnostic outranks generic assertion text") {
			t.Fatalf("Reasoning = %q, want nil-pointer decision criterion", resolved.Reasoning)
		}
	})
}

func TestCategorizeFailure_TimeoutSubcategory(t *testing.T) {
	testCases := []struct {
		name        string
		errorText   string
		subcategory string
	}{
		{
			name:        "deadline",
			errorText:   "context deadline exceeded",
			subcategory: "deadline_exceeded",
		},
		{
			name:        "cancellation",
			errorText:   "request aborted: context canceled",
			subcategory: "context_canceled",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			categorized := CategorizeFailure(Failure{ErrorMessage: tt.errorText})
			if categorized.Category != CategoryTimeout {
				t.Fatalf("Category = %q, want %q", categorized.Category, CategoryTimeout)
			}
			if categorized.Subcategory != tt.subcategory {
				t.Fatalf("Subcategory = %q, want %q", categorized.Subcategory, tt.subcategory)
			}
		})
	}
}

func TestResolveAmbiguity_DeadlineVsContextCancellation(t *testing.T) {
	testCases := []struct {
		name        string
		errorText   string
		subcategory string
	}{
		{
			name:        "cancellation is final marker",
			errorText:   "context deadline exceeded; cleanup returned context canceled",
			subcategory: "context_canceled",
		},
		{
			name:        "deadline is final marker",
			errorText:   "context canceled while retrying; final result: context deadline exceeded",
			subcategory: "deadline_exceeded",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			categorized := CategorizeFailure(Failure{ErrorMessage: tt.errorText})
			if IsAmbiguous(categorized) {
				t.Fatal("base categorization should not report two matches from a single timeout regex")
			}

			resolved := ResolveAmbiguity(categorized)
			if resolved.Category != CategoryTimeout {
				t.Fatalf("Category = %q, want %q", resolved.Category, CategoryTimeout)
			}
			if resolved.Subcategory != tt.subcategory {
				t.Fatalf("Subcategory = %q, want %q", resolved.Subcategory, tt.subcategory)
			}
			if !resolved.Uncertain || resolved.Confidence.Float64() >= ConfidenceModerate.Float64() {
				t.Fatalf("confidence = %.2f, want an uncertain score below %.2f", resolved.Confidence, ConfidenceModerate)
			}
			if !strings.Contains(resolved.Reasoning, "both context deadline and cancellation markers matched") {
				t.Fatalf("Reasoning = %q, want timeout decision criterion", resolved.Reasoning)
			}
		})
	}
}

func TestResolveAmbiguity_AssertionQuotedDiagnosticRegexOverlap(t *testing.T) {
	categorized := CategorizeFailure(Failure{ErrorMessage: "assertion failed: expected panic: nil pointer, got no panic"})
	if categorized.Category != CategoryPanic {
		t.Fatalf("base Category = %q, want %q before regex-overlap resolution", categorized.Category, CategoryPanic)
	}

	resolved := ResolveAmbiguity(categorized)
	if resolved.Category != CategoryAssertionError {
		t.Fatalf("Category = %q, want %q", resolved.Category, CategoryAssertionError)
	}
	if !resolved.Uncertain || resolved.Confidence.Float64() >= ConfidenceModerate.Float64() {
		t.Fatalf("confidence = %.2f, want an uncertain score below %.2f", resolved.Confidence, ConfidenceModerate)
	}
	if !strings.Contains(resolved.Reasoning, "assertion expectation quotes a diagnostic") {
		t.Fatalf("Reasoning = %q, want regex-priority decision criterion", resolved.Reasoning)
	}
}

func TestResolveAmbiguity_ConfidencePenaltyForCompetingSignals(t *testing.T) {
	testCases := []struct {
		name            string
		failure         Failure
		baseCategory    FailureCategory
		category        FailureCategory
		subcategory     string
		confidenceFloor float64
		confidenceCeil  float64
		matches         []FailureCategory
	}{
		{
			name: "runtime panic and assertion signals split across output sources",
			failure: Failure{
				ErrorMessage: "panic: runtime error: invalid memory address or nil pointer dereference",
				StackTrace:   "assertion failed: expected initialized response, got nil",
			},
			baseCategory:    CategoryPanic,
			category:        CategoryPanic,
			subcategory:     "nil_pointer_dereference",
			confidenceFloor: 0.64,
			confidenceCeil:  0.66,
			matches:         []FailureCategory{CategoryPanic, CategoryNilPointer, CategoryAssertionError},
		},
		{
			name:            "nil pointer diagnostic overlaps a generic assertion",
			failure:         Failure{ErrorMessage: "nil pointer dereference; assertion failed: expected value, got nil"},
			baseCategory:    CategoryNilPointer,
			category:        CategoryNilPointer,
			subcategory:     "assertion_context",
			confidenceFloor: 0.57,
			confidenceCeil:  0.59,
			matches:         []FailureCategory{CategoryNilPointer, CategoryAssertionError},
		},
		{
			name: "deadline and cancellation markers use their final combined-text occurrence",
			failure: Failure{
				ErrorMessage: "request context deadline exceeded",
				StackTrace:   "cleanup returned context canceled",
			},
			baseCategory:    CategoryTimeout,
			category:        CategoryTimeout,
			subcategory:     "context_canceled",
			confidenceFloor: 0.59,
			confidenceCeil:  0.60,
			matches:         []FailureCategory{CategoryTimeout},
		},
		{
			name:            "assertion quoting competing context diagnostics stays an assertion",
			failure:         Failure{ErrorMessage: "assertion failed: expected context deadline exceeded, got context canceled"},
			baseCategory:    CategoryTimeout,
			category:        CategoryAssertionError,
			confidenceFloor: 0.50,
			confidenceCeil:  0.51,
			matches:         []FailureCategory{CategoryTimeout, CategoryAssertionError},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			categorized := CategorizeFailure(tt.failure)
			if categorized.Category != tt.baseCategory {
				t.Fatalf("base category = %q, want %q", categorized.Category, tt.baseCategory)
			}

			matched := GetMatchingCategoriesForFailure(tt.failure)
			for _, want := range tt.matches {
				found := false
				for _, got := range matched {
					if got == want {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("matching categories = %v, want to include %q", matched, want)
				}
			}

			resolved := ResolveAmbiguity(categorized)
			if resolved.Category != tt.category || resolved.Type != tt.category {
				t.Fatalf("resolved category/type = %q/%q, want %q", resolved.Category, resolved.Type, tt.category)
			}
			if resolved.Subcategory != tt.subcategory {
				t.Fatalf("resolved subcategory = %q, want %q", resolved.Subcategory, tt.subcategory)
			}
			if resolved.Confidence.Float64() < tt.confidenceFloor || resolved.Confidence.Float64() > tt.confidenceCeil {
				t.Fatalf("resolved confidence = %.3f, want [%.2f, %.2f]", resolved.Confidence, tt.confidenceFloor, tt.confidenceCeil)
			}
			if !resolved.Uncertain {
				t.Fatal("resolved ambiguity must remain flagged for review")
			}
		})
	}
}
