package testutil

import (
	"fmt"
	"math"
	"testing"
)

// TestIsUncertain_ComprehensiveEdgeCases provides comprehensive table-driven tests
// for the IsUncertain() method, covering all edge cases, boundary behaviors,
// floating-point precision issues, and mathematical invariants.
//
// Implementation: IsUncertain() returns true when confidence < 0.7.
// The threshold is exclusive, so exactly 0.7 returns false.
//
// These tests serve as the complete reference for IsUncertain behavior and
// document all edge cases that should be considered when using this method.
func TestIsUncertain_ComprehensiveEdgeCases(t *testing.T) {
	testCases := []struct {
		name              string
		confidence        Confidence
		expectedUncertain bool
		description       string
		category          string // For grouping: "below", "at", "above", "edge"
	}{
		// ============================================================================
		// CATEGORY: Well Below Threshold (definitely uncertain)
		// ============================================================================
		{
			name:              "absolute_zero",
			confidence:        NewConfidence(0.0),
			expectedUncertain: true,
			description:       "Zero confidence is minimum possible value, definitely uncertain",
			category:          "below",
		},
		{
			name:              "very_low_0.1",
			confidence:        NewConfidence(0.1),
			expectedUncertain: true,
			description:       "10% confidence is well below threshold",
			category:          "below",
		},
		{
			name:              "low_threshold_0.5",
			confidence:        ConfidenceLow,
			expectedUncertain: true,
			description:       "50% confidence (ConfidenceLow constant) is below 70% threshold",
			category:          "below",
		},
		{
			name:              "moderate_below_0.6",
			confidence:        NewConfidence(0.6),
			expectedUncertain: true,
			description:       "60% confidence is below threshold",
			category:          "below",
		},
		{
			name:              "just_below_0.69",
			confidence:        NewConfidence(0.69),
			expectedUncertain: true,
			description:       "69% confidence is just below threshold",
			category:          "below",
		},

		// ============================================================================
		// CATEGORY: Threshold Boundary (critical precision tests)
		// ============================================================================
		{
			name:              "exact_threshold_constant",
			confidence:        UncertainThreshold,
			expectedUncertain: false,
			description:       "Exactly UncertainThreshold (0.7) is certain because the operator is <",
			category:          "at",
		},
		{
			name:              "exact_threshold_0.7",
			confidence:        ConfidenceModerate,
			expectedUncertain: false,
			description:       "Exactly 0.7 (ConfidenceModerate) is certain because the operator is <",
			category:          "at",
		},
		{
			name:              "just_below_precision_0.6999",
			confidence:        NewConfidence(0.6999),
			expectedUncertain: true,
			description:       "0.6999 is below threshold with 4-decimal precision",
			category:          "at",
		},
		{
			name:              "just_below_high_precision_0.6999999",
			confidence:        NewConfidence(0.6999999),
			expectedUncertain: true,
			description:       "0.6999999 is below threshold with 7-decimal precision",
			category:          "at",
		},
		{
			name:              "just_above_precision_0.7001",
			confidence:        NewConfidence(0.7001),
			expectedUncertain: false,
			description:       "0.7001 is above threshold with 4-decimal precision",
			category:          "at",
		},
		{
			name:              "just_above_high_precision_0.7000001",
			confidence:        NewConfidence(0.7000001),
			expectedUncertain: false,
			description:       "0.7000001 is above threshold with 7-decimal precision",
			category:          "at",
		},

		// ============================================================================
		// CATEGORY: Well Above Threshold (definitely certain)
		// ============================================================================
		{
			name:              "just_above_0.71",
			confidence:        NewConfidence(0.71),
			expectedUncertain: false,
			description:       "71% confidence is just above threshold",
			category:          "above",
		},
		{
			name:              "high_threshold_0.8",
			confidence:        ConfidenceHigh,
			expectedUncertain: false,
			description:       "80% confidence (ConfidenceHigh constant) is above threshold",
			category:          "above",
		},
		{
			name:              "very_high_0.9",
			confidence:        NewConfidence(0.9),
			expectedUncertain: false,
			description:       "90% confidence is well above threshold",
			category:          "above",
		},
		{
			name:              "very_high_threshold_0.95",
			confidence:        ConfidenceVeryHigh,
			expectedUncertain: false,
			description:       "95% confidence (ConfidenceVeryHigh constant) is well above threshold",
			category:          "above",
		},
		{
			name:              "absolute_maximum",
			confidence:        ConfidenceMax,
			expectedUncertain: false,
			description:       "100% confidence is maximum possible value, definitely certain",
			category:          "above",
		},

		// ============================================================================
		// CATEGORY: Edge Cases and Boundary Conditions
		// ============================================================================
		{
			name:              "minimum_possible",
			confidence:        ConfidenceMin,
			expectedUncertain: true,
			description:       "Minimum possible confidence (0.0) is uncertain",
			category:          "edge",
		},
		{
			name:              "maximum_possible",
			confidence:        ConfidenceMax,
			expectedUncertain: false,
			description:       "Maximum possible confidence (1.0) is certain",
			category:          "edge",
		},
		{
			name:              "epsilon_below_threshold",
			confidence:        NewConfidence(0.7 - 1e-10),
			expectedUncertain: true,
			description:       "Threshold minus epsilon (machine precision) should be uncertain",
			category:          "edge",
		},
		{
			name:              "epsilon_above_threshold",
			confidence:        NewConfidence(0.7 + 1e-10),
			expectedUncertain: false,
			description:       "Threshold plus epsilon (machine precision) should be certain",
			category:          "edge",
		},

		// ============================================================================
		// CATEGORY: Clamped Values (testing NewConfidence behavior)
		// ============================================================================
		{
			name:              "negative_clamped_to_zero",
			confidence:        NewConfidence(-0.5),
			expectedUncertain: true,
			description:       "Negative values are clamped to 0.0, which is uncertain",
			category:          "edge",
		},
		{
			name:              "large_negative_clamped_to_zero",
			confidence:        NewConfidence(-100.0),
			expectedUncertain: true,
			description:       "Large negative values are clamped to 0.0, which is uncertain",
			category:          "edge",
		},
		{
			name:              "above_one_clamped_to_max",
			confidence:        NewConfidence(1.5),
			expectedUncertain: false,
			description:       "Values > 1.0 are clamped to 1.0, which is certain",
			category:          "edge",
		},
		{
			name:              "large_above_one_clamped_to_max",
			confidence:        NewConfidence(100.0),
			expectedUncertain: false,
			description:       "Large values > 1.0 are clamped to 1.0, which is certain",
			category:          "edge",
		},

		// ============================================================================
		// CATEGORY: Special Floating-Point Values
		// ============================================================================
		{
			name:              "very_small_positive",
			confidence:        NewConfidence(1e-10),
			expectedUncertain: true,
			description:       "Very small positive value (1e-10) is uncertain",
			category:          "edge",
		},
		{
			name:              "very_close_to_one_below",
			confidence:        NewConfidence(0.999999999),
			expectedUncertain: false,
			description:       "Value very close to 1.0 from below is certain",
			category:          "edge",
		},
		{
			name:              "very_close_to_threshold_from_below",
			confidence:        NewConfidence(0.699999999999),
			expectedUncertain: true,
			description:       "Value very close to threshold from below is uncertain",
			category:          "edge",
		},
		{
			name:              "very_close_to_threshold_from_above",
			confidence:        NewConfidence(0.700000000001),
			expectedUncertain: false,
			description:       "Value very close to threshold from above is certain",
			category:          "edge",
		},

		// ============================================================================
		// CATEGORY: Mid-Range Values
		// ============================================================================
		{
			name:              "mid_range_0.35",
			confidence:        NewConfidence(0.35),
			expectedUncertain: true,
			description:       "35% confidence is in low-mid range, below threshold",
			category:          "below",
		},
		{
			name:              "mid_range_0.75",
			confidence:        NewConfidence(0.75),
			expectedUncertain: false,
			description:       "75% confidence is in mid-high range, above threshold",
			category:          "above",
		},
		{
			name:              "mid_range_0.85",
			confidence:        NewConfidence(0.85),
			expectedUncertain: false,
			description:       "85% confidence is in high range, above threshold",
			category:          "above",
		},
	}

	// Run all test cases
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.confidence.IsUncertain()

			// Check the main result
			if result != tt.expectedUncertain {
				t.Errorf("IsUncertain() = %v, want %v. %s (confidence=%.10f)",
					result, tt.expectedUncertain, tt.description, tt.confidence.Float64())
			}

			// Document the threshold behavior at exact boundary
			if tt.confidence.Float64() == 0.7 {
				certainResult := tt.confidence.IsCertain()
				if !certainResult {
					t.Errorf("At exact threshold 0.7, IsCertain() should return true (uses >=)")
				}
				// The predicates are complementary at the exact threshold.
				if result || !certainResult {
					t.Errorf("At exact threshold 0.7, IsUncertain() (uses <) should be false and IsCertain() (uses >=) true")
				}
				t.Logf("At exact threshold 0.7: IsUncertain()=%v, IsCertain()=%v (complementary by design)",
					result, certainResult)
			}

			// Log category information for documentation
			t.Logf("Category: %s, Description: %s", tt.category, tt.description)
		})
	}
}

// TestIsUncertain_MathematicalInvariants tests mathematical properties
// and invariants that should always hold true for the IsUncertain method.
func TestIsUncertain_MathematicalInvariants(t *testing.T) {
	t.Run("monotonic_decreasing_property", func(t *testing.T) {
		// Test that IsUncertain is monotonically decreasing:
		// If a < b, then IsUncertain(a) >= IsUncertain(b)
		// (Lower confidence should be more likely to be uncertain)

		testValues := []float64{
			0.0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.65, 0.69, 0.699,
			0.7, 0.701, 0.71, 0.75, 0.8, 0.85, 0.9, 0.95, 1.0,
		}

		for i := 0; i < len(testValues)-1; i++ {
			a := NewConfidence(testValues[i])
			b := NewConfidence(testValues[i+1])

			aUncertain := a.IsUncertain()
			bUncertain := b.IsUncertain()

			// Monotonic decreasing: as confidence increases, uncertainty should not increase
			if aUncertain && !bUncertain {
				// This is valid: transitioning from uncertain to certain
				continue
			}
			if !aUncertain && bUncertain {
				// This violates monotonicity: higher confidence is uncertain when lower is not
				t.Errorf("Monotonicity violated: IsUncertain(%.3f)=%v, IsUncertain(%.3f)=%v",
					a.Float64(), aUncertain, b.Float64(), bUncertain)
			}
		}
	})

	t.Run("threshold_exclusivity", func(t *testing.T) {
		// Test that exactly 0.7 is certain (< excludes the boundary).
		threshold := UncertainThreshold
		if threshold.IsUncertain() {
			t.Errorf("UncertainThreshold (%.2f) should not be uncertain (operator is <)", threshold.Float64())
		}

		// Also test with ConfidenceModerate (same value, different constant)
		moderate := ConfidenceModerate
		if moderate.IsUncertain() {
			t.Errorf("ConfidenceModerate (%.2f) should not be uncertain (operator is <)", moderate.Float64())
		}
	})

	t.Run("boundary_completeness", func(t *testing.T) {
		// Test that all values from 0.0 to 1.0 are covered
		// (no gaps in the range)

		step := 0.01
		for v := 0.0; v <= 1.0; v += step {
			conf := NewConfidence(v)
			result := conf.IsUncertain()

			// Verify it's either true or false (not panic or error)
			if v < 0.7 && !result {
				t.Errorf("Confidence(%.2f) should be uncertain (< 0.7), got certain", v)
			}
			if v > 0.7 && result {
				t.Errorf("Confidence(%.2f) should be certain (> 0.7), got uncertain", v)
			}
		}
	})

	t.Run("inverse_relationship_with_IsCertain", func(t *testing.T) {
		// Test the relationship between IsUncertain and IsCertain
		// At exactly 0.7, IsUncertain is false and IsCertain is true.
		// Below 0.7: IsUncertain=true, IsCertain=false
		// Above 0.7: IsUncertain=false, IsCertain=true

		testValues := []struct {
			value             float64
			uncertainExpected bool
			certainExpected   bool
			note              string
		}{
			{0.0, true, false, "below threshold"},
			{0.5, true, false, "below threshold"},
			{0.69, true, false, "below threshold"},
			{0.7, false, true, "at threshold (certain by design)"},
			{0.71, false, true, "above threshold"},
			{0.8, false, true, "above threshold"},
			{1.0, false, true, "at maximum"},
		}

		for _, tt := range testValues {
			conf := NewConfidence(tt.value)
			uncertain := conf.IsUncertain()
			certain := conf.IsCertain()

			if uncertain != tt.uncertainExpected {
				t.Errorf("IsUncertain(%.2f) = %v, want %v (%s)",
					tt.value, uncertain, tt.uncertainExpected, tt.note)
			}
			if certain != tt.certainExpected {
				t.Errorf("IsCertain(%.2f) = %v, want %v (%s)",
					tt.value, certain, tt.certainExpected, tt.note)
			}

			// Document the relationship at threshold
			if tt.value == 0.7 {
				t.Logf("At threshold 0.7: IsUncertain()=%v, IsCertain()=%v (%s)",
					uncertain, certain, tt.note)
			}
		}
	})
}

// TestIsUncertain_PrecisionBoundaryTests tests floating-point precision
// around the threshold boundary to ensure consistent behavior.
func TestIsUncertain_PrecisionBoundaryTests(t *testing.T) {
	threshold := UncertainThreshold.Float64()

	t.Run("epsilon_series_below_threshold", func(t *testing.T) {
		// Test values approaching threshold from below with decreasing epsilon
		epsilons := []float64{1e-1, 1e-2, 1e-3, 1e-4, 1e-5, 1e-6, 1e-7, 1e-8, 1e-9, 1e-10}

		for _, eps := range epsilons {
			value := threshold - eps
			conf := NewConfidence(value)

			if !conf.IsUncertain() {
				t.Errorf("IsUncertain(%.10f) = false, want true (threshold - %.1e)",
					value, eps)
			}
		}
	})

	t.Run("epsilon_series_above_threshold", func(t *testing.T) {
		// Test values approaching threshold from above with decreasing epsilon
		epsilons := []float64{1e-1, 1e-2, 1e-3, 1e-4, 1e-5, 1e-6, 1e-7, 1e-8, 1e-9, 1e-10}

		for _, eps := range epsilons {
			value := threshold + eps
			conf := NewConfidence(value)

			if conf.IsUncertain() {
				t.Errorf("IsUncertain(%.10f) = true, want false (threshold + %.1e)",
					value, eps)
			}
		}
	})

	t.Run("machine_epsilon_boundary", func(t *testing.T) {
		// Test at machine epsilon precision
		machineEpsilon := math.Nextafter(threshold, 0)

		confBelow := NewConfidence(machineEpsilon)
		if !confBelow.IsUncertain() {
			t.Errorf("IsUncertain(machine epsilon below threshold) should be true")
		}

		machineEpsilonAbove := math.Nextafter(threshold, 1)
		confAbove := NewConfidence(machineEpsilonAbove)
		if confAbove.IsUncertain() {
			t.Errorf("IsUncertain(machine epsilon above threshold) should be false")
		}
	})
}

// TestIsUncertain_ConstantConsistency tests that all related constants
// behave consistently with the threshold.
func TestIsUncertain_ConstantConsistency(t *testing.T) {
	t.Run("all_uncertain_constants", func(t *testing.T) {
		// Test that constants below 0.7 are uncertain.
		uncertainConstants := []struct {
			name       string
			confidence Confidence
			shouldBe   bool
		}{
			{"ConfidenceMin", ConfidenceMin, true},
			{"ConfidenceLow", ConfidenceLow, true},
		}

		for _, c := range uncertainConstants {
			result := c.confidence.IsUncertain()
			if result != c.shouldBe {
				t.Errorf("IsUncertain(%s) = %v, want %v", c.name, result, c.shouldBe)
			}
			t.Logf("Constant %s (%.2f): IsUncertain()=%v", c.name, c.confidence.Float64(), result)
		}
	})

	t.Run("all_certain_constants", func(t *testing.T) {
		// Test that constants at or above 0.7 are certain (not uncertain).
		certainConstants := []struct {
			name       string
			confidence Confidence
			shouldBe   bool
		}{
			{"ConfidenceModerate", ConfidenceModerate, false},
			{"UncertainThreshold", UncertainThreshold, false},
			{"ConfidenceHigh", ConfidenceHigh, false},
			{"ConfidenceVeryHigh", ConfidenceVeryHigh, false},
			{"ConfidenceMax", ConfidenceMax, false},
		}

		for _, c := range certainConstants {
			result := c.confidence.IsUncertain()
			if result != c.shouldBe {
				t.Errorf("IsUncertain(%s) = %v, want %v", c.name, result, c.shouldBe)
			}
			t.Logf("Constant %s (%.2f): IsUncertain()=%v", c.name, c.confidence.Float64(), result)
		}
	})

	t.Run("threshold_constants_match", func(t *testing.T) {
		// Test that UncertainThreshold and ConfidenceModerate are the same value
		if UncertainThreshold != ConfidenceModerate {
			t.Errorf("UncertainThreshold (%.2f) != ConfidenceModerate (%.2f)",
				UncertainThreshold.Float64(), ConfidenceModerate.Float64())
		}

		// Both should behave identically
		thresholdUncertain := UncertainThreshold.IsUncertain()
		moderateUncertain := ConfidenceModerate.IsUncertain()

		if thresholdUncertain != moderateUncertain {
			t.Errorf("IsUncertain(UncertainThreshold)=%v != IsUncertain(ConfidenceModerate)=%v",
				thresholdUncertain, moderateUncertain)
		}
	})
}

// TestIsUncertain_PropertiesAsPredicate tests IsUncertain as a predicate
// function, verifying it behaves correctly as a boolean classifier.
func TestIsUncertain_PropertiesAsPredicate(t *testing.T) {
	t.Run("total_predicate_function", func(t *testing.T) {
		// Test that IsUncertain is a total function (defined for all inputs in [0,1])

		// Test a comprehensive set of values
		for i := 0; i <= 100; i++ {
			value := float64(i) / 100.0
			conf := NewConfidence(value)

			// Should always return true or false (never panic)
			uncertain := conf.IsUncertain()

			// Verify the result makes sense
			if value < 0.7 && !uncertain {
				t.Errorf("Predicate error: IsUncertain(%.2f)=false for value < threshold", value)
			}
			if value > 0.7 && uncertain {
				t.Errorf("Predicate error: IsUncertain(%.2f)=true for value > threshold", value)
			}
		}
	})

	t.Run("predicate_partition", func(t *testing.T) {
		// Test that IsUncertain partitions the [0,1] range into two sets:
		// - [0, 0.7): returns true
		// - [0.7, 1]: returns false

		// Count elements in each partition
		trueCount := 0
		falseCount := 0

		for i := 0; i <= 100; i++ {
			value := float64(i) / 100.0
			conf := NewConfidence(value)

			if conf.IsUncertain() {
				trueCount++
			} else {
				falseCount++
			}
		}

		// Values 0.0 through 0.69 are true (70 values).
		// Values 0.70 through 1.0 are false (31 values).
		expectedTrueCount := 70
		expectedFalseCount := 31

		if trueCount != expectedTrueCount {
			t.Errorf("Partition error: true count = %d, want %d", trueCount, expectedTrueCount)
		}
		if falseCount != expectedFalseCount {
			t.Errorf("Partition error: false count = %d, want %d", falseCount, expectedFalseCount)
		}

		t.Logf("Partition: %d values return true (uncertain), %d values return false (certain)",
			trueCount, falseCount)
	})
}

// TestIsUncertain_RealWorldScenarios tests IsUncertain with realistic
// confidence values that would occur in actual usage.
func TestIsUncertain_RealWorldScenarios(t *testing.T) {
	testScenarios := []struct {
		name              string
		confidence        Confidence
		expectedUncertain bool
		scenario          string
	}{
		{
			name:              "exact_match_high_confidence",
			confidence:        NewConfidence(0.95),
			expectedUncertain: false,
			scenario:          "Exact pattern match with clear indicators - high confidence, not uncertain",
		},
		{
			name:              "strong_match_good_confidence",
			confidence:        NewConfidence(0.85),
			expectedUncertain: false,
			scenario:          "Strong match with multiple signals - good confidence, not uncertain",
		},
		{
			name:              "moderate_match_borderline",
			confidence:        NewConfidence(0.72),
			expectedUncertain: false,
			scenario:          "Moderate match just above threshold - not uncertain, but close",
		},
		{
			name:              "weak_match_at_threshold",
			confidence:        NewConfidence(0.70),
			expectedUncertain: false,
			scenario:          "Weak match at threshold - certain by design (< operator)",
		},
		{
			name:              "weak_match_below_threshold",
			confidence:        NewConfidence(0.68),
			expectedUncertain: true,
			scenario:          "Weak match below threshold - uncertain, needs manual review",
		},
		{
			name:              "ambiguous_match_low_confidence",
			confidence:        NewConfidence(0.55),
			expectedUncertain: true,
			scenario:          "Ambiguous patterns with low confidence - uncertain, needs review",
		},
		{
			name:              "very_uncertain_match",
			confidence:        NewConfidence(0.35),
			expectedUncertain: true,
			scenario:          "Very uncertain with conflicting signals - uncertain, high review priority",
		},
		{
			name:              "no_match_minimum_confidence",
			confidence:        NewConfidence(0.0),
			expectedUncertain: true,
			scenario:          "No pattern match - completely uncertain, requires manual categorization",
		},
	}

	for _, tt := range testScenarios {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.confidence.IsUncertain()

			if result != tt.expectedUncertain {
				t.Errorf("IsUncertain() = %v, want %v. Scenario: %s (confidence=%.2f)",
					result, tt.expectedUncertain, tt.scenario, tt.confidence.Float64())
			}

			action := "automate (certain)"
			if result {
				action = "review (uncertain)"
			}
			t.Logf("Confidence %.0f%% → %s - %s",
				tt.confidence.Float64()*100, action, tt.scenario)
		})
	}
}

// TestIsUncertain_ComparisonAndConsistency tests consistency between
// IsUncertain and other confidence-related methods.
func TestIsUncertain_ComparisonAndConsistency(t *testing.T) {
	t.Run("consistency_with_NeedsManualReview", func(t *testing.T) {
		// Test relationship between IsUncertain and NeedsManualReview
		// NeedsManualReview uses 0.5 threshold, IsUncertain uses 0.7
		// So: IsUncertain implies NeedsManualReview is not always true
		// But: NeedsManualReview implies IsUncertain should be true

		testValues := []float64{0.0, 0.3, 0.5, 0.6, 0.7, 0.8, 1.0}

		for _, v := range testValues {
			conf := NewConfidence(v)
			uncertain := conf.IsUncertain()
			needsReview := conf.NeedsManualReview()

			// If needs manual review (<= 0.5), it is definitely uncertain (< 0.7).
			if needsReview && !uncertain {
				t.Errorf("Inconsistency at %.2f: NeedsManualReview=true but IsUncertain=false", v)
			}

			t.Logf("Confidence %.2f: IsUncertain=%v, NeedsManualReview=%v", v, uncertain, needsReview)
		}
	})

	t.Run("consistency_with_Level", func(t *testing.T) {
		// Test relationship between IsUncertain and Level()
		// Levels below "Moderate" should be uncertain
		// "Moderate" and above should be certain

		testCases := []struct {
			confidence    Confidence
			isUncertain   bool
			expectedLevel string
		}{
			{ConfidenceMin, true, "Very Low"},
			{NewConfidence(0.3), true, "Very Low"}, // Fixed: values <0.5 return "Very Low"
			{ConfidenceLow, true, "Low"},
			{NewConfidence(0.6), true, "Low"},
			{ConfidenceModerate, false, "Moderate"}, // at threshold, certain
			{NewConfidence(0.75), false, "Moderate"},
			{ConfidenceHigh, false, "High"},
			{ConfidenceVeryHigh, false, "Very High"},
		}

		for _, tt := range testCases {
			level := tt.confidence.Level()
			uncertain := tt.confidence.IsUncertain()

			if level != tt.expectedLevel {
				t.Errorf("Level(%.2f) = %q, want %q", tt.confidence.Float64(), level, tt.expectedLevel)
			}
			if uncertain != tt.isUncertain {
				t.Errorf("IsUncertain(%.2f) = %v, want %v", tt.confidence.Float64(), uncertain, tt.isUncertain)
			}

			t.Logf("Confidence %.2f: Level=%q, IsUncertain=%v",
				tt.confidence.Float64(), level, uncertain)
		}
	})
}

// TestIsUncertain_ThresholdSemantics documents the semantic meaning
// of the threshold and the decision boundary.
func TestIsUncertain_ThresholdSemantics(t *testing.T) {
	t.Run("threshold_semantics", func(t *testing.T) {
		// Document what the threshold means semantically
		threshold := UncertainThreshold.Float64()

		semantics := fmt.Sprintf(
			"IsUncertain Threshold Semantics:\n"+
				"  Threshold Value: %.2f (%.0f%%)\n"+
				"  Operator: < (exclusive)\n"+
				"  Meaning: Confidence below %.2f is considered uncertain\n"+
				"  Implication: Categorizations with < %.0f%% confidence may need manual review\n"+
				"  Design Rationale: 70%% threshold balances false positives vs missed issues",
			threshold, threshold*100, threshold, threshold*100,
		)

		t.Log(semantics)

		// Verify the semantic behavior
		confExactly := NewConfidence(threshold)
		confBelow := NewConfidence(threshold - 0.01)
		confAbove := NewConfidence(threshold + 0.01)

		if confExactly.IsUncertain() {
			t.Errorf("Semantic violation: exact threshold should not be uncertain (exclusive)")
		}
		if !confBelow.IsUncertain() {
			t.Errorf("Semantic violation: below threshold should be uncertain")
		}
		if confAbove.IsUncertain() {
			t.Errorf("Semantic violation: above threshold should be certain (not uncertain)")
		}

		t.Logf("Semantic verification passed: below=%v, exact=%v, above=%v",
			confBelow.IsUncertain(), confExactly.IsUncertain(), confAbove.IsUncertain())
	})

	t.Run("threshold_decision_boundary", func(t *testing.T) {
		// Document the decision boundary
		decisionPoints := []struct {
			value     float64
			uncertain bool
			action    string
		}{
			{0.0, true, "Manual review required"},
			{0.5, true, "Manual review required"},
		}

		for i := 60; i <= 80; i++ {
			value := float64(i) / 100.0
			uncertain := NewConfidence(value).IsUncertain()
			action := "Manual review required"
			if !uncertain {
				action = "Can automate"
			}
			decisionPoints = append(decisionPoints, struct {
				value     float64
				uncertain bool
				action    string
			}{value, uncertain, action})
		}

		t.Log("Decision Boundary Analysis:")
		for _, dp := range decisionPoints {
			t.Logf("  %.2f → IsUncertain=%v → %s", dp.value, dp.uncertain, dp.action)
		}
	})
}
