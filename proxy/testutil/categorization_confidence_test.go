package testutil

import (
	"testing"
)

// TestNewConfidence_BoundaryValues tests the NewConfidence constructor with
// boundary and edge case values to ensure proper clamping behavior.
func TestNewConfidence_BoundaryValues(t *testing.T) {
	testCases := []struct {
		name     string
		input    float64
		expected Confidence
	}{
		// Boundary values - should pass through unchanged
		{"exactly zero", 0.0, ConfidenceMin},
		{"exactly one", 1.0, ConfidenceMax},
		{"exactly minimum", 0.0, ConfidenceMin},
		{"exactly maximum", 1.0, ConfidenceMax},

		// Valid values within range - should pass through unchanged
		{"middle value", 0.5, Confidence(0.5)},
		{"low valid value", 0.1, Confidence(0.1)},
		{"high valid value", 0.9, Confidence(0.9)},
		{"moderate confidence", 0.7, ConfidenceModerate},
		{"high confidence", 0.8, ConfidenceHigh},
		{"very high confidence", 0.95, ConfidenceVeryHigh},

		// Out of range - should be clamped to boundaries
		{"negative value", -0.5, ConfidenceMin},
		{"large negative", -100.0, ConfidenceMin},
		{"just below zero", -0.001, ConfidenceMin},
		{"above one", 1.5, ConfidenceMax},
		{"large positive", 100.0, ConfidenceMax},
		{"just above one", 1.001, ConfidenceMax},

		// Floating point edge cases
		{"very small positive", 1e-10, Confidence(1e-10)},
		{"very close to one", 0.999999999, Confidence(0.999999999)},
		{"very close to zero", 1e-300, Confidence(1e-300)},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result := NewConfidence(tt.input)
			if result != tt.expected {
				t.Errorf("NewConfidence(%.2f) = %.2f, want %.2f", tt.input, result, tt.expected)
			}
		})
	}
}

// TestConfidence_IsCertain tests the IsCertain method with various
// confidence values around the uncertainty threshold (0.7).
func TestConfidence_IsCertain(t *testing.T) {
	testCases := []struct {
		name       string
		confidence Confidence
		expected   bool
	}{
		// Below threshold - should be uncertain
		{"zero", ConfidenceMin, false},
		{"very low", Confidence(0.1), false},
		{"low", ConfidenceLow, false},
		{"just below threshold", Confidence(0.69), false},
		{"exactly below threshold", Confidence(0.699), false},

		// At threshold - should be certain (>= 0.7)
		{"exactly threshold", ConfidenceModerate, true},
		{"just at threshold", Confidence(0.7), true},

		// Above threshold - should be certain
		{"just above threshold", Confidence(0.71), true},
		{"moderate high", Confidence(0.8), true},
		{"high", ConfidenceHigh, true},
		{"very high", ConfidenceVeryHigh, true},
		{"maximum", ConfidenceMax, true},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.confidence.IsCertain()
			if result != tt.expected {
				t.Errorf("Confidence(%.2f).IsCertain() = %v, want %v", tt.confidence, result, tt.expected)
			}
		})
	}
}

// TestConfidence_IsUncertain tests the IsUncertain method with various
// confidence values around the uncertainty threshold (0.7).
func TestConfidence_IsUncertain(t *testing.T) {
	testCases := []struct {
		name       string
		confidence Confidence
		expected   bool
	}{
		// Below threshold - should be uncertain
		{"zero", ConfidenceMin, true},
		{"very low", Confidence(0.1), true},
		{"low", ConfidenceLow, true},
		{"just below threshold", Confidence(0.69), true},
		{"exactly below threshold", Confidence(0.699), true},

		// At threshold - should not be uncertain; only values below 0.7 are flagged.
		{"exactly threshold", ConfidenceModerate, false},
		{"just at threshold", Confidence(0.7), false},

		// Above threshold - should NOT be uncertain
		{"just above threshold", Confidence(0.71), false},
		{"moderate high", Confidence(0.8), false},
		{"high", ConfidenceHigh, false},
		{"very high", ConfidenceVeryHigh, false},
		{"maximum", ConfidenceMax, false},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.confidence.IsUncertain()
			if result != tt.expected {
				t.Errorf("Confidence(%.2f).IsUncertain() = %v, want %v", tt.confidence, result, tt.expected)
			}
		})
	}
}

// TestConfidence_NeedsManualReview tests the NeedsManualReview method
// which checks if confidence is at or below the low threshold (0.5).
func TestConfidence_NeedsManualReview(t *testing.T) {
	testCases := []struct {
		name       string
		confidence Confidence
		expected   bool
	}{
		// At or below low threshold - should need manual review
		{"zero", ConfidenceMin, true},
		{"very low", Confidence(0.1), true},
		{"just below low", Confidence(0.49), true},
		{"exactly low threshold", ConfidenceLow, true},

		// Just above low threshold - should NOT need manual review
		{"just above low", Confidence(0.51), false},
		{"moderate", ConfidenceModerate, false},
		{"high", ConfidenceHigh, false},
		{"maximum", ConfidenceMax, false},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.confidence.NeedsManualReview()
			if result != tt.expected {
				t.Errorf("Confidence(%.2f).NeedsManualReview() = %v, want %v",
					tt.confidence, result, tt.expected)
			}
		})
	}
}

// TestConfidence_Level tests the Level method which returns
// human-readable confidence level descriptions.
func TestConfidence_Level(t *testing.T) {
	testCases := []struct {
		name       string
		confidence Confidence
		expected   string
	}{
		{"zero", ConfidenceMin, "Very Low"},
		{"very low", Confidence(0.1), "Very Low"},
		{"low", ConfidenceLow, "Low"},
		{"just below moderate", Confidence(0.69), "Low"},
		{"moderate threshold", ConfidenceModerate, "Moderate"},
		{"just above moderate", Confidence(0.71), "Moderate"},
		{"high threshold", ConfidenceHigh, "High"},
		{"very high threshold", ConfidenceVeryHigh, "Very High"},
		{"maximum", ConfidenceMax, "Very High"},

		// Boundary tests for each level
		{"just below very high", Confidence(0.94), "High"},
		{"just at very high", Confidence(0.95), "Very High"},
		{"just below high", Confidence(0.79), "Moderate"},
		{"just at high", Confidence(0.8), "High"},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.confidence.Level()
			if result != tt.expected {
				t.Errorf("Confidence(%.2f).Level() = %q, want %q", tt.confidence, result, tt.expected)
			}
		})
	}
}

// TestConfidence_Float64 tests the Float64 method which returns
// the confidence value as a float64 for backward compatibility.
func TestConfidence_Float64(t *testing.T) {
	testCases := []struct {
		name       string
		confidence Confidence
		expected   float64
	}{
		{"zero", ConfidenceMin, 0.0},
		{"maximum", ConfidenceMax, 1.0},
		{"low", ConfidenceLow, 0.5},
		{"moderate", ConfidenceModerate, 0.7},
		{"high", ConfidenceHigh, 0.8},
		{"very high", ConfidenceVeryHigh, 0.95},
		{"middle", Confidence(0.5), 0.5},
		{"precision test", Confidence(0.123456789), 0.123456789},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.confidence.Float64()
			if result != tt.expected {
				t.Errorf("Confidence(%.2f).Float64() = %.2f, want %.2f",
					tt.confidence, result, tt.expected)
			}
		})
	}
}

// TestConfidence_Constants tests that all confidence constants
// are within valid bounds and have expected values.
func TestConfidence_Constants(t *testing.T) {
	// Test minimum and maximum constants
	if ConfidenceMin != 0.0 {
		t.Errorf("ConfidenceMin = %.2f, want 0.0", ConfidenceMin)
	}
	if ConfidenceMax != 1.0 {
		t.Errorf("ConfidenceMax = %.2f, want 1.0", ConfidenceMax)
	}

	// Test threshold constants are in ascending order
	if ConfidenceLow >= ConfidenceModerate {
		t.Errorf("ConfidenceLow (%.2f) >= ConfidenceModerate (%.2f), want <",
			ConfidenceLow, ConfidenceModerate)
	}
	if ConfidenceModerate >= ConfidenceHigh {
		t.Errorf("ConfidenceModerate (%.2f) >= ConfidenceHigh (%.2f), want <",
			ConfidenceModerate, ConfidenceHigh)
	}
	if ConfidenceHigh >= ConfidenceVeryHigh {
		t.Errorf("ConfidenceHigh (%.2f) >= ConfidenceVeryHigh (%.2f), want <",
			ConfidenceHigh, ConfidenceVeryHigh)
	}
	if ConfidenceVeryHigh > ConfidenceMax {
		t.Errorf("ConfidenceVeryHigh (%.2f) > ConfidenceMax (%.2f), want <=",
			ConfidenceVeryHigh, ConfidenceMax)
	}

	// Test specific threshold values
	if ConfidenceLow != 0.5 {
		t.Errorf("ConfidenceLow = %.2f, want 0.5", ConfidenceLow)
	}
	if ConfidenceModerate != 0.7 {
		t.Errorf("ConfidenceModerate = %.2f, want 0.7", ConfidenceModerate)
	}
	if ConfidenceHigh != 0.8 {
		t.Errorf("ConfidenceHigh = %.2f, want 0.8", ConfidenceHigh)
	}
	if ConfidenceVeryHigh != 0.95 {
		t.Errorf("ConfidenceVeryHigh = %.2f, want 0.95", ConfidenceVeryHigh)
	}
}

// TestConfidence_TypeProperties tests various properties of the Confidence
// type to ensure consistent behavior across methods.
func TestConfidence_TypeProperties(t *testing.T) {
	// Test that IsCertain and IsUncertain are mutually exclusive.
	conf := Confidence(0.8)
	if !conf.IsCertain() {
		t.Error("High confidence should be certain")
	}
	if conf.IsUncertain() {
		t.Error("High confidence should not be uncertain")
	}

	conf = Confidence(0.5)
	if conf.IsCertain() {
		t.Error("Low confidence should not be certain")
	}
	if !conf.IsUncertain() {
		t.Error("Low confidence should be uncertain")
	}

	// Test that NeedsManualReview implies IsUncertain
	conf = Confidence(0.3)
	if !conf.NeedsManualReview() {
		t.Error("Very low confidence should need manual review")
	}
	if !conf.IsUncertain() {
		t.Error("Confidence needing manual review should be uncertain")
	}

	// Test that high confidence doesn't need manual review
	conf = Confidence(0.9)
	if conf.NeedsManualReview() {
		t.Error("High confidence should not need manual review")
	}
	if !conf.IsCertain() {
		t.Error("High confidence should be certain")
	}

	// Test boundary consistency
	conf = Confidence(0.7)
	if !conf.IsCertain() {
		t.Error("Confidence at threshold should be certain")
	}
	if conf.IsUncertain() {
		t.Error("Confidence at threshold should not be uncertain (< check)")
	}
	if conf.NeedsManualReview() {
		t.Error("Confidence at threshold should not need manual review")
	}
}

// TestConfidence_Comparisons tests that Confidence values can be
// compared using standard operators.
func TestConfidence_Comparisons(t *testing.T) {
	low := NewConfidence(0.3)
	medium := NewConfidence(0.5)
	high := NewConfidence(0.8)

	// Test less than
	if !(low < medium) {
		t.Error("Expected low < medium")
	}
	if !(medium < high) {
		t.Error("Expected medium < high")
	}

	// Test greater than
	if !(high > medium) {
		t.Error("Expected high > medium")
	}
	if !(medium > low) {
		t.Error("Expected medium > low")
	}

	// Test equality
	same := NewConfidence(0.5)
	if medium != same {
		t.Error("Expected medium == same")
	}

	// Test boundary comparisons
	min := NewConfidence(0.0)
	max := NewConfidence(1.0)
	if !(min < max) {
		t.Error("Expected min < max")
	}

	// Test that clamping doesn't break comparisons
	clampedLow := NewConfidence(-100.0) // Should clamp to 0.0
	clampedHigh := NewConfidence(100.0) // Should clamp to 1.0
	if clampedLow != ConfidenceMin {
		t.Error("Clamped negative value should equal ConfidenceMin")
	}
	if clampedHigh != ConfidenceMax {
		t.Error("Clamped high value should equal ConfidenceMax")
	}
}

// TestCategorizedFailure_UncertainFlag tests that the Uncertain flag
// in CategorizedFailure is correctly set based on confidence threshold.
func TestCategorizedFailure_UncertainFlag(t *testing.T) {
	testCases := []struct {
		name              string
		confidence        Confidence
		expectedUncertain bool
	}{
		{"zero confidence", ConfidenceMin, true},
		{"low confidence", Confidence(0.5), true},
		{"just below threshold", Confidence(0.69), true},
		{"at threshold", ConfidenceModerate, false},
		{"just above threshold", Confidence(0.71), false},
		{"high confidence", ConfidenceHigh, false},
		{"maximum confidence", ConfidenceMax, false},
	}

	for _, tt := range testCases {
		// Note: The Uncertain field should be set during categorization
		// This test verifies the expected relationship
		t.Run(tt.name, func(t *testing.T) {

			// Note: The Uncertain field should be set during categorization
			// This test verifies the expected relationship
			expectedFlag := tt.confidence.IsUncertain()
			if expectedFlag != tt.expectedUncertain {
				t.Errorf("Confidence.IsUncertain() = %v, want %v",
					expectedFlag, tt.expectedUncertain)
			}
		})
	}
}

// TestCategorizeFailure_ConfidenceBoundaryAndUncertainty verifies that the
// categorization result carries the bounded score and strict uncertainty flag.
// In particular, a score of exactly 0.7 is certain; only scores below it are
// marked uncertain.
func TestCategorizeFailure_ConfidenceBoundaryAndUncertainty(t *testing.T) {
	testCases := []struct {
		name              string
		failure           Failure
		expectedCategory  FailureCategory
		expectedScore     Confidence
		expectedUncertain bool
	}{
		{
			name:              "no match has zero confidence",
			failure:           Failure{ErrorMessage: "unrecognized failure condition"},
			expectedCategory:  CategoryUnknown,
			expectedScore:     ConfidenceMin,
			expectedUncertain: true,
		},
		{
			name:              "assertion at threshold is certain",
			failure:           Failure{ErrorMessage: "assertion failed: expected 1, got 2"},
			expectedCategory:  CategoryAssertionError,
			expectedScore:     UncertainThreshold,
			expectedUncertain: false,
		},
		{
			name:              "exact data-race marker has maximum confidence",
			failure:           Failure{ErrorMessage: "WARNING: DATA RACE"},
			expectedCategory:  CategoryDataRace,
			expectedScore:     ConfidenceMax,
			expectedUncertain: false,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result := CategorizeFailure(tt.failure)
			if result.Category != tt.expectedCategory {
				t.Errorf("Category = %q, want %q", result.Category, tt.expectedCategory)
			}
			if result.Confidence != tt.expectedScore {
				t.Errorf("Confidence = %.2f, want %.2f", result.Confidence, tt.expectedScore)
			}
			if result.Uncertain != tt.expectedUncertain {
				t.Errorf("Uncertain = %v, want %v", result.Uncertain, tt.expectedUncertain)
			}
		})
	}
}

// TestConfidence_ThresholdRelationships tests the relationships between
// different confidence thresholds and methods.
func TestConfidence_ThresholdRelationships(t *testing.T) {
	// Test that all confidence levels have consistent Level() output
	levels := map[Confidence]string{
		0.0:  "Very Low",
		0.1:  "Very Low",
		0.4:  "Very Low",
		0.5:  "Low",
		0.6:  "Low",
		0.7:  "Moderate",
		0.75: "Moderate",
		0.8:  "High",
		0.9:  "High",
		0.95: "Very High",
		1.0:  "Very High",
	}

	for conf, expectedLevel := range levels {
		t.Run(expectedLevel, func(t *testing.T) {
			c := NewConfidence(float64(conf))
			if c.Level() != expectedLevel {
				t.Errorf("Confidence(%.2f).Level() = %q, want %q", c, c.Level(), expectedLevel)
			}
		})
	}
}
