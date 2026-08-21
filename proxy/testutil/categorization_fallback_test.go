package testutil

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestCategorizeFailure_FallbackSubcategories(t *testing.T) {
	testCases := []struct {
		name        string
		failure     Failure
		subcategory string
		label       string
	}{
		{
			name:        "valid but unrecognized failure",
			failure:     Failure{ErrorMessage: "dependency emitted an opaque status"},
			subcategory: fallbackSubcategoryUnclassified,
			label:       "Other: unclassified failure",
		},
		{
			name:        "unrecognized panic signal",
			failure:     Failure{ErrorMessage: "panic report unavailable from external runner"},
			subcategory: fallbackSubcategoryUnknownPanicMessage,
			label:       "Other: unknown panic message",
		},
		{
			name:        "unrecognized fatal signal",
			failure:     Failure{ErrorMessage: "fatal diagnostic unavailable from external runner"},
			subcategory: fallbackSubcategoryUnknownFatalMessage,
			label:       "Other: unknown fatal message",
		},
		{
			name:        "empty failure",
			failure:     Failure{},
			subcategory: fallbackSubcategoryEmptyFailure,
			label:       "Other: empty failure",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			categorized := CategorizeFailure(tt.failure)
			if categorized.Category != CategoryUnknown || categorized.Type != CategoryUnknown {
				t.Fatalf("category/type = %q/%q, want unknown fallback", categorized.Category, categorized.Type)
			}
			if categorized.Subcategory != tt.subcategory {
				t.Errorf("Subcategory = %q, want %q", categorized.Subcategory, tt.subcategory)
			}
			if !categorized.Uncertain {
				t.Error("Uncertain = false, want true for fallback")
			}
			if label := GetCategoryLabel(categorized); label != tt.label {
				t.Errorf("GetCategoryLabel() = %q, want %q", label, tt.label)
			}
			if !strings.Contains(categorized.Reasoning, "fallback category") {
				t.Errorf("Reasoning = %q, want fallback explanation", categorized.Reasoning)
			}
		})
	}
}

func TestCategorizeFailure_FallbackLogsSanitizedMetadata(t *testing.T) {
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	previousPrefix := log.Prefix()
	var logs bytes.Buffer
	log.SetFlags(0)
	log.SetPrefix("")
	log.SetOutput(&logs)
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	})

	failure := Failure{
		TestName:     "TestOpaquePanic",
		FilePath:     "runner_test.go",
		LineNumber:   27,
		ErrorMessage: "panic report unavailable: sensitive value should stay out of logs",
	}
	CategorizeFailure(failure)

	output := logs.String()
	if !strings.Contains(output, `category=Other subcategory="unknown_panic_message"`) {
		t.Errorf("fallback log = %q, want category and subcategory", output)
	}
	if !strings.Contains(output, `test="TestOpaquePanic"`) {
		t.Errorf("fallback log = %q, want test metadata", output)
	}
	if strings.Contains(output, failure.ErrorMessage) {
		t.Errorf("fallback log must not contain raw failure text: %q", output)
	}
}
