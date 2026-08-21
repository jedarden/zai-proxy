package testutil

import (
	"fmt"
	"math"
	"testing"
)

// TestNeedsManualReviewFunction tests the standalone NeedsManualReview function
// which checks if a categorization needs manual review based on confidence threshold.
func TestNeedsManualReviewFunction(t *testing.T) {
	testCases := []struct {
		name      string
		failure   CategorizedFailure
		threshold float64
		expected  bool
	}{
		// Test with default threshold (0.5)
		{
			name: "very low confidence with default threshold",
			failure: CategorizedFailure{
				TestFailure: TestFailure{TestName: "Test1"},
				Category:    CategoryAssertionError,
				Confidence:  Confidence(0.1),
			},
			threshold: 0.0, // Use default
			expected:  true,
		},
		{
			name: "confidence exactly at low threshold",
			failure: CategorizedFailure{
				TestFailure: TestFailure{TestName: "Test1"},
				Category:    CategoryAssertionError,
				Confidence:  ConfidenceLow, // 0.5
			},
			threshold: 0.0, // Use default
			expected:  true,
		},
		{
			name: "confidence just above low threshold",
			failure: CategorizedFailure{
				TestFailure: TestFailure{TestName: "Test1"},
				Category:    CategoryAssertionError,
				Confidence:  Confidence(0.51),
			},
			threshold: 0.0, // Use default
			expected:  false,
		},
		{
			name: "high confidence with default threshold",
			failure: CategorizedFailure{
				TestFailure: TestFailure{TestName: "Test1"},
				Category:    CategoryAssertionError,
				Confidence:  ConfidenceHigh,
			},
			threshold: 0.0, // Use default
			expected:  false,
		},

		// Test with custom threshold
		{
			name: "custom threshold 0.7 with confidence below",
			failure: CategorizedFailure{
				TestFailure: TestFailure{TestName: "Test1"},
				Category:    CategoryAssertionError,
				Confidence:  Confidence(0.6),
			},
			threshold: 0.7,
			expected:  true,
		},
		{
			name: "custom threshold 0.7 with confidence above",
			failure: CategorizedFailure{
				TestFailure: TestFailure{TestName: "Test1"},
				Category:    CategoryAssertionError,
				Confidence:  Confidence(0.8),
			},
			threshold: 0.7,
			expected:  false,
		},
		{
			name: "custom threshold 0.7 with confidence exactly at threshold",
			failure: CategorizedFailure{
				TestFailure: TestFailure{TestName: "Test1"},
				Category:    CategoryAssertionError,
				Confidence:  Confidence(0.7),
			},
			threshold: 0.7,
			expected:  true,
		},

		// Test unknown category always needs review
		{
			name: "unknown category with any confidence",
			failure: CategorizedFailure{
				TestFailure: TestFailure{TestName: "Test1"},
				Category:    CategoryUnknown,
				Confidence:  Confidence(0.9),
			},
			threshold: 0.0,
			expected:  true,
		},
		{
			name: "unknown category with zero confidence",
			failure: CategorizedFailure{
				TestFailure: TestFailure{TestName: "Test1"},
				Category:    CategoryUnknown,
				Confidence:  ConfidenceMin,
			},
			threshold: 0.0,
			expected:  true,
		},

		// Test boundary values
		{
			name: "zero confidence",
			failure: CategorizedFailure{
				TestFailure: TestFailure{TestName: "Test1"},
				Category:    CategoryAssertionError,
				Confidence:  ConfidenceMin,
			},
			threshold: 0.0,
			expected:  true,
		},
		{
			name: "maximum confidence",
			failure: CategorizedFailure{
				TestFailure: TestFailure{TestName: "Test1"},
				Category:    CategoryDataRace,
				Confidence:  ConfidenceMax,
			},
			threshold: 0.0,
			expected:  false,
		},

		// Test with very high custom threshold
		{
			name: "very high threshold 0.95",
			failure: CategorizedFailure{
				TestFailure: TestFailure{TestName: "Test1"},
				Category:    CategoryAssertionError,
				Confidence:  Confidence(0.94),
			},
			threshold: 0.95,
			expected:  true,
		},
		{
			name: "very high threshold 0.95 with very high confidence",
			failure: CategorizedFailure{
				TestFailure: TestFailure{TestName: "Test1"},
				Category:    CategoryDataRace,
				Confidence:  ConfidenceVeryHigh,
			},
			threshold: 0.95,
			expected:  true,
		},

		// Test with very low custom threshold
		{
			name: "very low threshold 0.1",
			failure: CategorizedFailure{
				TestFailure: TestFailure{TestName: "Test1"},
				Category:    CategoryAssertionError,
				Confidence:  Confidence(0.05),
			},
			threshold: 0.1,
			expected:  true,
		},
		{
			name: "very low threshold 0.1 with higher confidence",
			failure: CategorizedFailure{
				TestFailure: TestFailure{TestName: "Test1"},
				Category:    CategoryAssertionError,
				Confidence:  Confidence(0.2),
			},
			threshold: 0.1,
			expected:  false,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			var result bool
			if tt.threshold == 0.0 {
				result = NeedsManualReview(tt.failure)
			} else {
				result = NeedsManualReview(tt.failure, tt.threshold)
			}
			if result != tt.expected {
				t.Errorf("NeedsManualReview() = %v, want %v (confidence=%.2f, category=%s, threshold=%.2f)",
					result, tt.expected, tt.failure.Confidence, tt.failure.Category, tt.threshold)
			}
		})
	}
}

// TestConfidence_Precision tests that confidence values maintain precision
// through calculations and conversions.
func TestConfidence_Precision(t *testing.T) {
	testCases := []struct {
		name     string
		input    float64
		expected float64
	}{
		{"high precision value", 0.123456789, 0.123456789},
		{"very small value", 1e-10, 1e-10},
		{"very large value", 0.999999999, 0.999999999},
		{"mid range value", 0.555555555, 0.555555555},
		{"exactly 0.7", 0.7, 0.7},
		{"exactly 0.5", 0.5, 0.5},
		{"exactly 0.8", 0.8, 0.8},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			conf := NewConfidence(tt.input)
			result := conf.Float64()
			if math.Abs(result-tt.expected) > 1e-9 {
				t.Errorf("NewConfidence(%.9f).Float64() = %.9f, want %.9f", tt.input, result, tt.expected)
			}
		})
	}
}

// TestConfidence_ThresholdInvariants tests invariants around confidence thresholds.
func TestConfidence_ThresholdInvariants(t *testing.T) {
	t.Run("IsCertain and IsUncertain are inverses above threshold", func(t *testing.T) {
		for i := 71; i <= 100; i++ {
			conf := NewConfidence(float64(i) / 100.0)
			if !conf.IsCertain() {
				t.Errorf("Confidence(%.2f) should be certain (>= 0.7)", conf.Float64())
			}
			if conf.IsUncertain() {
				t.Errorf("Confidence(%.2f) should not be uncertain (> 0.7)", conf.Float64())
			}
		}
	})

	t.Run("IsCertain and IsUncertain are inverses below threshold", func(t *testing.T) {
		for i := 0; i <= 69; i++ {
			conf := NewConfidence(float64(i) / 100.0)
			if conf.IsCertain() {
				t.Errorf("Confidence(%.2f) should not be certain (< 0.7)", conf.Float64())
			}
			if !conf.IsUncertain() {
				t.Errorf("Confidence(%.2f) should be uncertain (< 0.7)", conf.Float64())
			}
		}
	})

	t.Run("At threshold 0.7, IsCertain is true and IsUncertain is false", func(t *testing.T) {
		conf := ConfidenceModerate // Exactly 0.7
		if !conf.IsCertain() {
			t.Errorf("Confidence(%.2f) should be certain (>= 0.7)", conf.Float64())
		}
		if conf.IsUncertain() {
			t.Errorf("Confidence(%.2f) should not be uncertain (< 0.7)", conf.Float64())
		}
	})
}

// TestConfidence_SpecialFloatValues tests confidence behavior with special float values.
func TestConfidence_SpecialFloatValues(t *testing.T) {
	testCases := []struct {
		name     string
		input    float64
		expected Confidence
	}{
		{"negative infinity", math.Inf(-1), ConfidenceMin},
		{"positive infinity", math.Inf(1), ConfidenceMax},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result := NewConfidence(tt.input)
			if result != tt.expected {
				t.Errorf("NewConfidence(%v) = %.2f, want %.2f", tt.input, result, tt.expected)
			}
		})
	}

	// Test NaN separately since NaN != NaN
	t.Run("not a number", func(t *testing.T) {
		result := NewConfidence(math.NaN())
		if result != ConfidenceMin {
			t.Errorf("NewConfidence(NaN) = %.2f, want %.2f", result, ConfidenceMin)
		}
	})
}

// TestConfidence_MethodChaining tests that confidence methods can be chained
// and provide consistent results.
func TestConfidence_MethodChaining(t *testing.T) {
	conf := NewConfidence(0.85)

	// Test that Float64() can be called multiple times with same result
	first := conf.Float64()
	second := conf.Float64()
	if first != second {
		t.Errorf("Float64() returned different values: %.2f vs %.2f", first, second)
	}

	// Test that methods don't modify the underlying value
	conf1 := NewConfidence(0.75)
	_ = conf1.IsCertain()
	_ = conf1.IsUncertain()
	_ = conf1.Level()
	_ = conf1.NeedsManualReview()

	if conf1.Float64() != 0.75 {
		t.Errorf("Confidence value changed after method calls, got %.2f", conf1.Float64())
	}
}

// TestConfidence_LevelConsistency tests that Level() returns consistent
// results for the same confidence value.
func TestConfidence_LevelConsistency(t *testing.T) {
	testCases := []struct {
		confidence    Confidence
		expectedLevel string
	}{
		{Confidence(0.0), "Very Low"},
		{Confidence(0.01), "Very Low"},
		{Confidence(0.49), "Very Low"},
		{Confidence(0.5), "Low"},
		{Confidence(0.69), "Low"},
		{Confidence(0.7), "Moderate"},
		{Confidence(0.79), "Moderate"},
		{Confidence(0.8), "High"},
		{Confidence(0.94), "High"},
		{Confidence(0.95), "Very High"},
		{Confidence(0.99), "Very High"},
		{Confidence(1.0), "Very High"},
	}

	for _, tt := range testCases {
		t.Run(tt.expectedLevel, func(t *testing.T) {
			level := tt.confidence.Level()
			if level != tt.expectedLevel {
				t.Errorf("Confidence(%.2f).Level() = %q, want %q", tt.confidence, level, tt.expectedLevel)
			}
			// Test consistency - calling Level() multiple times returns same result
			secondLevel := tt.confidence.Level()
			if secondLevel != tt.expectedLevel {
				t.Errorf("Confidence(%.2f).Level() inconsistent: %q then %q", tt.confidence, level, secondLevel)
			}
		})
	}
}

// TestConfidence_CalculationWithNilSignals tests that CalculateConfidence handles
// nil and empty signal arrays correctly.
func TestConfidence_CalculationWithNilSignals(t *testing.T) {
	// Test with nil signals
	params := ConfidenceCalculationParams{
		BaseConfidence: 0.5,
		Signals:        nil,
	}
	result := CalculateConfidence(params)
	if result != Confidence(0.5) {
		t.Errorf("CalculateConfidence with nil signals = %.2f, want 0.5", result)
	}

	// Test with empty signals
	params = ConfidenceCalculationParams{
		BaseConfidence: 0.7,
		Signals:        []MatchSignal{},
	}
	result = CalculateConfidence(params)
	if result != Confidence(0.7) {
		t.Errorf("CalculateConfidence with empty signals = %.2f, want 0.7", result)
	}
}

// TestConfidence_ExtremeValues tests confidence calculation with extreme
// combinations of parameters.
func TestConfidence_ExtremeValues(t *testing.T) {
	testCases := []struct {
		name        string
		params      ConfidenceCalculationParams
		minExpected float64
		maxExpected float64
	}{
		{
			name: "maximum base with maximum boost and no penalty",
			params: ConfidenceCalculationParams{
				BaseConfidence: 1.0,
				Signals: []MatchSignal{
					{Type: SignalExactMatch, Pattern: "test", Strength: 1.0},
				},
				ContextBoost: 1.0,
			},
			minExpected: 0.95,
			maxExpected: 1.0,
		},
		{
			name: "minimum base with maximum penalty",
			params: ConfidenceCalculationParams{
				BaseConfidence: 0.0,
				Signals: []MatchSignal{
					{Type: SignalInferred, Pattern: "test", Strength: 0.1},
				},
				AmbiguityPenalty: 1.0,
			},
			minExpected: 0.0,
			maxExpected: 0.05,
		},
		{
			name: "moderate base with maximum penalty and boost",
			params: ConfidenceCalculationParams{
				BaseConfidence: 0.5,
				Signals: []MatchSignal{
					{Type: SignalExactMatch, Pattern: "test", Strength: 1.0},
				},
				AmbiguityPenalty: 0.9,
				ContextBoost:     0.9,
			},
			minExpected: 0.70,
			maxExpected: 0.95,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateConfidence(tt.params).Float64()
			if result < tt.minExpected || result > tt.maxExpected {
				t.Errorf("CalculateConfidence() = %.2f, want [%.2f, %.2f]", result, tt.minExpected, tt.maxExpected)
			}
		})
	}
}

// TestConfidence_SignalWeightSummation tests that multiple signals of the same
// type are weighted correctly.
func TestConfidence_SignalWeightSummation(t *testing.T) {
	params := ConfidenceCalculationParams{
		BaseConfidence: 0.5,
		Signals: []MatchSignal{
			{Type: SignalExactMatch, Pattern: "p1", Strength: 1.0},
			{Type: SignalExactMatch, Pattern: "p2", Strength: 1.0},
			{Type: SignalExactMatch, Pattern: "p3", Strength: 1.0},
		},
	}

	result := CalculateConfidence(params).Float64()

	// Three exact matches with base 0.5 should give high confidence
	// but not reach 1.0 without boost due to normalization
	if result < 0.85 {
		t.Errorf("Three exact matches should result in high confidence, got %.2f", result)
	}
	if result >= 0.99 {
		t.Errorf("Three exact matches should have diminishing returns, got %.2f", result)
	}
}

// TestConfidence_AmbiguityPenaltyBounds tests that ambiguity penalty is
// properly bounded and doesn't produce invalid results.
func TestConfidence_AmbiguityPenaltyBounds(t *testing.T) {
	testCases := []struct {
		name             string
		base             float64
		ambiguityPenalty float64
		minResult        float64
		maxResult        float64
	}{
		{"100% penalty", 0.8, 1.0, 0.0, 0.1},
		{"invalid penalty > 1.0", 0.8, 1.5, 0.0, 0.1},
		{"zero penalty", 0.8, 0.0, 0.95, 1.0},
		{"small penalty", 0.5, 0.1, 0.58, 0.72},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			params := ConfidenceCalculationParams{
				BaseConfidence:   tt.base,
				Signals:          []MatchSignal{{Type: SignalExactMatch, Pattern: "test"}},
				AmbiguityPenalty: tt.ambiguityPenalty,
			}
			result := CalculateConfidence(params).Float64()
			if result < tt.minResult || result > tt.maxResult {
				t.Errorf("With penalty %.2f, result = %.2f, want [%.2f, %.2f]",
					tt.ambiguityPenalty, result, tt.minResult, tt.maxResult)
			}
		})
	}
}

// TestConfidence_ContextBoostBounds tests that context boost is properly
// bounded and doesn't exceed 1.0.
func TestConfidence_ContextBoostBounds(t *testing.T) {
	testCases := []struct {
		name         string
		base         float64
		contextBoost float64
		expectedMax  float64
	}{
		{"maximum boost", 0.9, 1.0, 1.0},
		{"invalid boost > 1.0", 0.5, 1.5, 1.0},
		{"moderate boost", 0.6, 0.5, 0.95},
		{"small boost", 0.4, 0.1, 0.75},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			params := ConfidenceCalculationParams{
				BaseConfidence: tt.base,
				Signals:        []MatchSignal{{Type: SignalExactMatch, Pattern: "test"}},
				ContextBoost:   tt.contextBoost,
			}
			result := CalculateConfidence(params).Float64()
			if result > tt.expectedMax {
				t.Errorf("With boost %.2f, result = %.2f, should not exceed %.2f",
					tt.contextBoost, result, tt.expectedMax)
			}
			if result > 1.0 {
				t.Errorf("Result should never exceed 1.0, got %.2f", result)
			}
		})
	}
}

// TestConfidence_SignalStrengthBounds tests that signal strength modifiers
// are properly bounded.
func TestConfidence_SignalStrengthBounds(t *testing.T) {
	testCases := []struct {
		name      string
		strength  float64
		minResult float64
		maxResult float64
	}{
		{"zero strength (defaults to 1.0)", 0.0, 0.70, 0.80},
		{"negative strength", -0.5, 0.0, 0.1},
		{"very high strength", 2.0, 0.80, 0.85},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			params := ConfidenceCalculationParams{
				BaseConfidence: 0.5,
				Signals: []MatchSignal{
					{Type: SignalExactMatch, Pattern: "test", Strength: tt.strength},
				},
			}
			result := CalculateConfidence(params).Float64()
			if result < tt.minResult || result > tt.maxResult {
				t.Errorf("With strength %.2f, result = %.2f, want [%.2f, %.2f]",
					tt.strength, result, tt.minResult, tt.maxResult)
			}
		})
	}
}

// TestConfidence_CombinedPenaltyAndBoost tests that combined penalty and boost
// interact correctly without producing invalid results.
func TestConfidence_CombinedPenaltyAndBoost(t *testing.T) {
	testCases := []struct {
		name             string
		base             float64
		ambiguityPenalty float64
		contextBoost     float64
		minExpected      float64
		maxExpected      float64
	}{
		{
			name:             "high penalty, high boost",
			base:             0.7,
			ambiguityPenalty: 0.8,
			contextBoost:     0.8,
			minExpected:      0.80,
			maxExpected:      0.90,
		},
		{
			name:             "balanced penalty and boost",
			base:             0.6,
			ambiguityPenalty: 0.3,
			contextBoost:     0.3,
			minExpected:      0.70,
			maxExpected:      0.75,
		},
		{
			name:             "zero penalty, maximum boost",
			base:             0.5,
			ambiguityPenalty: 0.0,
			contextBoost:     1.0,
			minExpected:      0.75,
			maxExpected:      1.0,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			params := ConfidenceCalculationParams{
				BaseConfidence:   tt.base,
				Signals:          []MatchSignal{{Type: SignalKeywordMatch, Pattern: "test"}},
				AmbiguityPenalty: tt.ambiguityPenalty,
				ContextBoost:     tt.contextBoost,
			}
			result := CalculateConfidence(params).Float64()
			if result < tt.minExpected || result > tt.maxExpected {
				t.Errorf("Result = %.2f, want [%.2f, %.2f]", result, tt.minExpected, tt.maxExpected)
			}
		})
	}
}

// TestConfidence_BoundaryConsistency tests consistency of confidence calculations
// at exact boundary values.
func TestConfidence_BoundaryConsistency(t *testing.T) {
	boundaries := []float64{0.0, 0.5, 0.7, 0.8, 0.95, 1.0}

	for _, boundary := range boundaries {
		t.Run(fmt.Sprintf("boundary_%.2f", boundary), func(t *testing.T) {
			conf := NewConfidence(boundary)

			// Test that creating confidence with boundary value is consistent
			if conf.Float64() != boundary {
				t.Errorf("NewConfidence(%.2f).Float64() = %.2f", boundary, conf.Float64())
			}

			// Test that creating again produces same result
			conf2 := NewConfidence(boundary)
			if conf != conf2 {
				t.Errorf("NewConfidence(%.2f) != NewConfidence(%.2f)", boundary, boundary)
			}
		})
	}
}

// TestConfidence_DefaultSignalStrength tests that signals with zero/missing
// strength default to 1.0.
func TestConfidence_DefaultSignalStrength(t *testing.T) {
	params := ConfidenceCalculationParams{
		BaseConfidence: 0.5,
		Signals: []MatchSignal{
			{Type: SignalExactMatch, Pattern: "test"}, // Strength defaults to 1.0
		},
	}

	result := CalculateConfidence(params).Float64()

	// Should use default strength of 1.0
	if result < 0.7 {
		t.Errorf("Signal with default strength should contribute meaningfully, got %.2f", result)
	}
}

// TestConfidence_SignalTypeCoverage tests that all signal types are properly
// weighted in calculations.
func TestConfidence_SignalTypeCoverage(t *testing.T) {
	signalTypes := []MatchSignalType{
		SignalExactMatch,
		SignalKeywordMatch,
		SignalContextualMatch,
		SignalPartialMatch,
		SignalInferred,
	}

	for _, signalType := range signalTypes {
		t.Run(string(signalType), func(t *testing.T) {
			params := ConfidenceCalculationParams{
				BaseConfidence: 0.5,
				Signals: []MatchSignal{
					{Type: signalType, Pattern: "test", Strength: 1.0},
				},
			}

			result := CalculateConfidence(params).Float64()

			// All signal types should contribute to confidence
			if result <= 0.5 {
				t.Errorf("Signal type %s should contribute to confidence, got %.2f", signalType, result)
			}

			// But none should exceed reasonable bounds with single signal
			if result >= 0.9 {
				t.Errorf("Single signal of type %s should not reach 0.9+, got %.2f", signalType, result)
			}
		})
	}
}

// TestIsUncertain_ThresholdBehavior tests the IsUncertain() method behavior
// at and around the uncertainty threshold (0.7).
//
// Implementation: IsUncertain() returns true when confidence < 0.7.
// This means:
// - Values below 0.7: returns true (uncertain)
// - Value at 0.7: returns false (certain)
// - Values above 0.7: returns false (certain)
//
// This test documents the exact threshold boundary behavior.
func TestIsUncertain_ThresholdBehavior(t *testing.T) {
	testCases := []struct {
		name              string
		confidence        Confidence
		expectedUncertain bool
		description       string
	}{
		// Values below threshold should return true (uncertain)
		{
			name:              "well_below_threshold_0.0",
			confidence:        NewConfidence(0.0),
			expectedUncertain: true,
			description:       "Zero confidence is uncertain",
		},
		{
			name:              "below_threshold_0.5",
			confidence:        NewConfidence(0.5),
			expectedUncertain: true,
			description:       "0.5 is below threshold, should be uncertain",
		},
		{
			name:              "just_below_threshold_0.69",
			confidence:        NewConfidence(0.69),
			expectedUncertain: true,
			description:       "0.69 is below threshold, should be uncertain",
		},
		{
			name:              "just_below_threshold_0.6999",
			confidence:        NewConfidence(0.6999),
			expectedUncertain: true,
			description:       "0.6999 is below threshold, should be uncertain",
		},

		// Exact threshold value is certain (implementation uses <).
		{
			name:              "exact_threshold_0.7",
			confidence:        NewConfidence(0.7),
			expectedUncertain: false,
			description:       "0.7 is at threshold, so it is not uncertain",
		},

		// Values above threshold should return false (certain)
		{
			name:              "just_above_threshold_0.7001",
			confidence:        NewConfidence(0.7001),
			expectedUncertain: false,
			description:       "0.7001 is above threshold, should be certain (not uncertain)",
		},
		{
			name:              "just_above_threshold_0.71",
			confidence:        NewConfidence(0.71),
			expectedUncertain: false,
			description:       "0.71 is above threshold, should be certain (not uncertain)",
		},
		{
			name:              "well_above_threshold_0.8",
			confidence:        NewConfidence(0.8),
			expectedUncertain: false,
			description:       "0.8 is above threshold, should be certain (not uncertain)",
		},
		{
			name:              "maximum_confidence_1.0",
			confidence:        NewConfidence(1.0),
			expectedUncertain: false,
			description:       "1.0 is maximum confidence, should be certain (not uncertain)",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.confidence.IsUncertain()
			if result != tt.expectedUncertain {
				t.Errorf("IsUncertain() = %v, want %v. %s (confidence=%.4f)",
					result, tt.expectedUncertain, tt.description, tt.confidence.Float64())
			}

			// Document the relationship with IsCertain at threshold
			if tt.confidence.Float64() == 0.7 {
				certainResult := tt.confidence.IsCertain()
				if !certainResult {
					t.Errorf("At threshold 0.7, IsCertain() should return true (uses >=)")
				}
				// The predicates are complementary at the exact threshold.
				t.Logf("At exact threshold 0.7: IsUncertain()=%v (uses <), IsCertain()=%v (uses >=)",
					result, certainResult)
			}
		})
	}
}

// TestIsUncertain_ThresholdInvariants tests invariant properties around
// the uncertainty threshold to ensure consistent behavior.
func TestIsUncertain_ThresholdInvariants(t *testing.T) {
	t.Run("all_values_below_threshold_are_uncertain", func(t *testing.T) {
		// Test values from 0.0 through 0.69.
		for i := 0; i < 70; i++ {
			conf := NewConfidence(float64(i) / 100.0)
			if !conf.IsUncertain() {
				t.Errorf("Confidence(%.2f) should be uncertain (< 0.7), got false", conf.Float64())
			}
		}
	})

	t.Run("all_values_above_threshold_are_not_uncertain", func(t *testing.T) {
		// Test values from 0.71 to 1.0
		for i := 71; i <= 100; i++ {
			conf := NewConfidence(float64(i) / 100.0)
			if conf.IsUncertain() {
				t.Errorf("Confidence(%.2f) should not be uncertain (> 0.7), got true", conf.Float64())
			}
		}
	})

	t.Run("threshold_boundary_is_certain", func(t *testing.T) {
		// Test that exactly 0.7 is not uncertain (< excludes the boundary).
		conf := NewConfidence(0.7)
		if conf.IsUncertain() {
			t.Errorf("Confidence(0.7) should not be uncertain (threshold uses <)")
		}
	})
}
