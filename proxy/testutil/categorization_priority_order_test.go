package testutil

import (
	"strings"
	"testing"
)

// TestCategorizeFailure_ChannelOverMapKeyPriority asserts that
// CategorizeFailure resolves by rule priority, not by the decision-tree
// document order. docs/notes/CATEGORIZATION_DECISION_TREE.md formerly listed
// map key errors (priority 55) at Step 6 before channel errors (priority 56)
// at Step 7, while docs/categorization-rules.md (the contract) places
// channel_error above map_key_error. A message matching both must classify as
// channel_error.
func TestCategorizeFailure_ChannelOverMapKeyPriority(t *testing.T) {
	errorMsg := "key not found while handling send on closed channel"

	failure := TestFailure{
		TestName:     "TestPriorityOverDocumentOrder",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: errorMsg,
	}

	// Guard against a vacuous pass: both rules must actually match, so this
	// test exercises exactly the overlap that made the document order wrong.
	matched := GetMatchingCategoriesForFailure(failure)
	matchedSet := map[FailureCategory]bool{}
	for _, category := range matched {
		matchedSet[category] = true
	}
	if !matchedSet[CategoryChannel] || !matchedSet[CategoryMapKey] {
		t.Fatalf("precondition failed: got matches %v, want both %q and %q", matched, CategoryChannel, CategoryMapKey)
	}

	cat := CategorizeFailure(failure)

	// channel_error (priority 56) must win over map_key_error (priority 55)
	// even though the old decision-tree document order checked map keys first.
	if cat.Category != CategoryChannel {
		t.Errorf("Category: got %q, want %q (priority 56 must outrank map_key_error priority 55)", cat.Category, CategoryChannel)
	}

	// The competing match is retained in the reasoning.
	if !strings.Contains(cat.Reasoning, "map_key_error") {
		t.Error("Reasoning should mention map_key_error was also matched")
	}

	if cat.Confidence.Float64() < 0.8 {
		t.Errorf("Confidence: got %.2f, want >= 0.8", cat.Confidence)
	}
}

// TestCategorizeFailure_MapKeyOverGoroutineTieBreak asserts the documented
// tie-break for the shared priority 55: map_key_error resolves before
// goroutine_panic regardless of where each pattern's text appears.
func TestCategorizeFailure_MapKeyOverGoroutineTieBreak(t *testing.T) {
	failure := TestFailure{
		TestName:     "TestTieBreakDeclarationOrder",
		FilePath:     "test.go",
		LineNumber:   10,
		ErrorMessage: "key not found",
		StackTrace:   "goroutine 7 [running]:",
	}

	matched := GetMatchingCategoriesForFailure(failure)
	matchedSet := map[FailureCategory]bool{}
	for _, category := range matched {
		matchedSet[category] = true
	}
	if !matchedSet[CategoryMapKey] || !matchedSet[CategoryGoroutinePanic] {
		t.Fatalf("precondition failed: got matches %v, want both %q and %q", matched, CategoryMapKey, CategoryGoroutinePanic)
	}

	cat := CategorizeFailure(failure)

	if cat.Category != CategoryMapKey {
		t.Errorf("Category: got %q, want %q (map_key_error wins the priority-55 tie over goroutine_panic)", cat.Category, CategoryMapKey)
	}

	if !strings.Contains(cat.Reasoning, "goroutine_panic") {
		t.Error("Reasoning should mention goroutine_panic was also matched")
	}
}

// TestSortedCategorizationRules_DescendingPriority asserts the invariant that
// makes the result independent of declaration (document) order: the sorted
// rule list is non-increasing by priority, covers every declared rule exactly
// once, and places channel_error above the priority-55 pair in the
// contract's tie-break order.
func TestSortedCategorizationRules_DescendingPriority(t *testing.T) {
	rules := sortedCategorizationRules()

	if len(rules) != len(categorizationRules) {
		t.Fatalf("sorted rules length: got %d, want %d", len(rules), len(categorizationRules))
	}

	seen := map[FailureCategory]int{}
	for i, rule := range rules {
		seen[rule.Category]++
		if seen[rule.Category] > 1 {
			t.Errorf("category %q appears %d times in sorted rules", rule.Category, seen[rule.Category])
		}
		if i == 0 {
			continue
		}
		if rules[i-1].Priority < rule.Priority {
			t.Errorf("rules not priority-sorted at index %d: %q (priority %d) before %q (priority %d)",
				i, rules[i-1].Category, rules[i-1].Priority, rule.Category, rule.Priority)
		}
	}

	channelIdx, mapKeyIdx, goroutineIdx := -1, -1, -1
	for i, rule := range rules {
		switch rule.Category {
		case CategoryChannel:
			channelIdx = i
		case CategoryMapKey:
			mapKeyIdx = i
		case CategoryGoroutinePanic:
			goroutineIdx = i
		}
	}

	if channelIdx < 0 || mapKeyIdx < 0 || goroutineIdx < 0 {
		t.Fatalf("missing rules in sorted list: channel=%d mapKey=%d goroutine=%d", channelIdx, mapKeyIdx, goroutineIdx)
	}

	// The exact conflict called out in docs/categorization-rules.md and the
	// decision tree: channel_error resolves above map_key_error, and the
	// priority-55 tie resolves map_key_error before goroutine_panic.
	if rules[channelIdx].Priority <= rules[mapKeyIdx].Priority {
		t.Errorf("channel_error priority %d must be greater than map_key_error priority %d",
			rules[channelIdx].Priority, rules[mapKeyIdx].Priority)
	}
	if mapKeyIdx > goroutineIdx {
		t.Errorf("map_key_error (index %d) must resolve before goroutine_panic (index %d) at equal priority %d",
			mapKeyIdx, goroutineIdx, rules[mapKeyIdx].Priority)
	}
}
