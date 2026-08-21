package testutil

import (
	"fmt"
	"math"
	"testing"
)

// TestNewConfidence_ExactBoundaryValues tests the exact boundary values specified in acceptance criteria.
func TestNewConfidence_ExactBoundaryValues(t *testing.T) {
	testCases := []struct {
		name     string
		input    float64
		expected Confidence
	}{
		{"exact minimum 0.0", 0.0, ConfidenceMin},
		{"exact threshold 0.7", 0.7, ConfidenceModerate},
		{"exact maximum 1.0", 1.0, ConfidenceMax},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result := NewConfidence(tt.input)
			if result != tt.expected {
				t.Errorf("NewConfidence(%.1f) = %.1f, want %.1f", tt.input, result, tt.expected)
			}

			// Verify Float64() returns exact value
			if result.Float64() != tt.input {
				t.Errorf("NewConfidence(%.1f).Float64() = %.1f, want %.1f", tt.input, result.Float64(), tt.input)
			}

			// Verify consistency - creating again should produce same result
			result2 := NewConfidence(tt.input)
			if result != result2 {
				t.Errorf("NewConfidence(%.1f) != NewConfidence(%.1f) - inconsistent results", tt.input, tt.input)
			}
		})
	}
}

// TestNewConfidence_ThresholdBoundaryValues tests values just below and above the uncertainty threshold (0.7).
func TestNewConfidence_ThresholdBoundaryValues(t *testing.T) {
	testCases := []struct {
		name              string
		input             float64
		expectedCertain   bool
		expectedUncertain bool
		expectedLevel     string
	}{
		{
			name:              "just below threshold 0.69",
			input:             0.69,
			expectedCertain:   false,
			expectedUncertain: true,
			expectedLevel:     "Low",
		},
		{
			name:              "just above threshold 0.71",
			input:             0.71,
			expectedCertain:   true,
			expectedUncertain: false,
			expectedLevel:     "Moderate",
		},
		{
			name:              "exactly at threshold 0.70",
			input:             0.70,
			expectedCertain:   true,  // IsCertain uses >=
			expectedUncertain: false, // IsUncertain uses <
			expectedLevel:     "Moderate",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			conf := NewConfidence(tt.input)

			// Verify value is preserved
			if conf.Float64() != tt.input {
				t.Errorf("NewConfidence(%.2f).Float64() = %.2f, want %.2f", tt.input, conf.Float64(), tt.input)
			}

			// Verify IsCertain
			if conf.IsCertain() != tt.expectedCertain {
				t.Errorf("NewConfidence(%.2f).IsCertain() = %v, want %v", tt.input, conf.IsCertain(), tt.expectedCertain)
			}

			// Verify IsUncertain
			if conf.IsUncertain() != tt.expectedUncertain {
				t.Errorf("NewConfidence(%.2f).IsUncertain() = %v, want %v", tt.input, conf.IsUncertain(), tt.expectedUncertain)
			}

			// Verify Level
			if conf.Level() != tt.expectedLevel {
				t.Errorf("NewConfidence(%.2f).Level() = %q, want %q", tt.input, conf.Level(), tt.expectedLevel)
			}

			// At threshold 0.70, certainty begins and uncertainty ends.
			if tt.input == 0.70 {
				if !conf.IsCertain() || conf.IsUncertain() {
					t.Errorf("At exact threshold 0.70, IsCertain() should be true and IsUncertain() false; got certain=%v, uncertain=%v",
						conf.IsCertain(), conf.IsUncertain())
				}
			}
		})
	}
}

// TestNewConfidence_InvalidNegativeValues tests that negative values are clamped to 0.0.
func TestNewConfidence_InvalidNegativeValues(t *testing.T) {
	testCases := []struct {
		name          string
		input         float64
		expected      Confidence
		expectedFloat float64
	}{
		{"negative zero", -0.0, ConfidenceMin, 0.0},
		{"small negative -0.1", -0.1, ConfidenceMin, 0.0},
		{"moderate negative -0.5", -0.5, ConfidenceMin, 0.0},
		{"large negative -1.0", -1.0, ConfidenceMin, 0.0},
		{"very large negative -10.0", -10.0, ConfidenceMin, 0.0},
		{"negative infinity", math.Inf(-1), ConfidenceMin, 0.0},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result := NewConfidence(tt.input)

			// Should clamp to minimum
			if result != tt.expected {
				t.Errorf("NewConfidence(%v) = %v, want %v (negative values should clamp to ConfidenceMin)", tt.input, result, tt.expected)
			}

			// Float64 should return 0.0 for all negative inputs
			if result.Float64() != tt.expectedFloat {
				t.Errorf("NewConfidence(%v).Float64() = %v, want %v (negative values should clamp to 0.0)", tt.input, result.Float64(), tt.expectedFloat)
			}

			// Clamped negative values should be uncertain (at threshold boundary)
			if !result.IsUncertain() {
				t.Errorf("NewConfidence(%v).IsUncertain() = false, want true (0.0 is at/below threshold)", tt.input)
			}

			// Level should be "Very Low" for 0.0
			if result.Level() != "Very Low" {
				t.Errorf("NewConfidence(%v).Level() = %q, want \"Very Low\"", tt.input, result.Level())
			}

			// Should need manual review
			if !result.NeedsManualReview() {
				t.Errorf("NewConfidence(%v).NeedsManualReview() = false, want true", tt.input)
			}
		})
	}
}

// TestNewConfidence_InvalidAboveMaximumValues tests that values > 1.0 are clamped to 1.0.
func TestNewConfidence_InvalidAboveMaximumValues(t *testing.T) {
	testCases := []struct {
		name          string
		input         float64
		expected      Confidence
		expectedFloat float64
	}{
		{"just above maximum 1.1", 1.1, ConfidenceMax, 1.0},
		{"moderate above 1.5", 1.5, ConfidenceMax, 1.0},
		{"double value 2.0", 2.0, ConfidenceMax, 1.0},
		{"very large 10.0", 10.0, ConfidenceMax, 1.0},
		{"positive infinity", math.Inf(1), ConfidenceMax, 1.0},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result := NewConfidence(tt.input)

			// Should clamp to maximum
			if result != tt.expected {
				t.Errorf("NewConfidence(%v) = %v, want %v (values > 1.0 should clamp to ConfidenceMax)", tt.input, result, tt.expected)
			}

			// Float64 should return 1.0 for all inputs > 1.0
			if result.Float64() != tt.expectedFloat {
				t.Errorf("NewConfidence(%v).Float64() = %v, want %v (values > 1.0 should clamp to 1.0)", tt.input, result.Float64(), tt.expectedFloat)
			}

			// Clamped maximum values should be certain (above threshold)
			if !result.IsCertain() {
				t.Errorf("NewConfidence(%v).IsCertain() = false, want true (1.0 is above threshold)", tt.input)
			}

			// Should not be uncertain
			if result.IsUncertain() {
				t.Errorf("NewConfidence(%v).IsUncertain() = true, want false (1.0 is above threshold)", tt.input)
			}

			// Level should be "Very High" for 1.0
			if result.Level() != "Very High" {
				t.Errorf("NewConfidence(%v).Level() = %q, want \"Very High\"", tt.input, result.Level())
			}

			// Should not need manual review
			if result.NeedsManualReview() {
				t.Errorf("NewConfidence(%v).NeedsManualReview() = true, want false", tt.input)
			}
		})
	}
}

// TestNewConfidence_ZeroValueEdgeCase tests the zero value edge case comprehensively.
func TestNewConfidence_ZeroValueEdgeCase(t *testing.T) {
	t.Run("zero from explicit 0.0", func(t *testing.T) {
		conf := NewConfidence(0.0)

		// Verify it's exactly minimum
		if conf != ConfidenceMin {
			t.Errorf("NewConfidence(0.0) = %v, want ConfidenceMin", conf)
		}

		// Verify Float64 returns 0.0
		if conf.Float64() != 0.0 {
			t.Errorf("NewConfidence(0.0).Float64() = %v, want 0.0", conf.Float64())
		}

		// Verify method behaviors
		if !conf.IsUncertain() {
			t.Errorf("NewConfidence(0.0).IsUncertain() = false, want true")
		}

		if conf.IsCertain() {
			t.Errorf("NewConfidence(0.0).IsCertain() = true, want false")
		}

		if !conf.NeedsManualReview() {
			t.Errorf("NewConfidence(0.0).NeedsManualReview() = false, want true")
		}

		if conf.Level() != "Very Low" {
			t.Errorf("NewConfidence(0.0).Level() = %q, want \"Very Low\"", conf.Level())
		}
	})

	t.Run("zero from negative clamping", func(t *testing.T) {
		conf := NewConfidence(-1.0)

		// Should behave identically to explicit 0.0
		if conf != ConfidenceMin {
			t.Errorf("NewConfidence(-1.0) = %v, want ConfidenceMin", conf)
		}

		if conf.Float64() != 0.0 {
			t.Errorf("NewConfidence(-1.0).Float64() = %v, want 0.0", conf.Float64())
		}

		// All method behaviors should match explicit 0.0
		zeroConf := NewConfidence(0.0)
		if conf.IsUncertain() != zeroConf.IsUncertain() {
			t.Errorf("NewConfidence(-1.0).IsUncertain() != NewConfidence(0.0).IsUncertain()")
		}

		if conf.IsCertain() != zeroConf.IsCertain() {
			t.Errorf("NewConfidence(-1.0).IsCertain() != NewConfidence(0.0).IsCertain()")
		}

		if conf.NeedsManualReview() != zeroConf.NeedsManualReview() {
			t.Errorf("NewConfidence(-1.0).NeedsManualReview() != NewConfidence(0.0).NeedsManualReview()")
		}

		if conf.Level() != zeroConf.Level() {
			t.Errorf("NewConfidence(-1.0).Level() != NewConfidence(0.0).Level()")
		}
	})

	t.Run("zero in calculations", func(t *testing.T) {
		// Test that zero base with no signals returns zero
		params := ConfidenceCalculationParams{
			BaseConfidence: 0.0,
			Signals:        []MatchSignal{},
		}
		result := CalculateConfidence(params)

		if result != ConfidenceMin {
			t.Errorf("CalculateConfidence with zero base and no signals = %v, want ConfidenceMin", result)
		}

		// Test that zero in calculations with signals still contributes
		params = ConfidenceCalculationParams{
			BaseConfidence: 0.0,
			Signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "test", Strength: 1.0},
			},
		}
		result = CalculateConfidence(params)

		// Should have some contribution from signal
		if result.Float64() <= 0.0 {
			t.Errorf("CalculateConfidence with zero base and strong signal = %v, should be > 0.0", result)
		}

		// But should not exceed reasonable bounds with single signal from zero base
		if result.Float64() >= 0.6 {
			t.Errorf("CalculateConfidence with zero base and single signal = %v, should be < 0.6", result)
		}
	})
}

// TestConfidence_ClampingBehavior tests the clamping behavior comprehensively.
func TestConfidence_ClampingBehavior(t *testing.T) {
	t.Run("clamping preserves precision for valid values", func(t *testing.T) {
		validValues := []float64{0.0, 0.1, 0.5, 0.7, 0.9, 1.0}
		for _, v := range validValues {
			conf := NewConfidence(v)
			if conf.Float64() != v {
				t.Errorf("NewConfidence(%.1f) should preserve value, got %.1f", v, conf.Float64())
			}
		}
	})

	t.Run("clamping handles edge cases gracefully", func(t *testing.T) {
		edgeCases := []struct {
			input    float64
			expected float64
			reason   string
		}{
			{math.SmallestNonzeroFloat64, math.SmallestNonzeroFloat64, "smallest positive float preserved"},
			{-math.SmallestNonzeroFloat64, 0.0, "smallest negative float clamped to 0"},
			{math.Nextafter(1.0, 2.0), 1.0, "value just above 1.0 clamped to 1"},
			{math.Nextafter(0.0, -1.0), 0.0, "value just below 0.0 clamped to 0"},
		}

		for _, tc := range edgeCases {
			conf := NewConfidence(tc.input)
			if conf.Float64() != tc.expected {
				t.Errorf("%s: NewConfidence(%g) = %g, want %g", tc.reason, tc.input, conf.Float64(), tc.expected)
			}
		}
	})
}

// TestConfidence_ThresholdTransitions tests behavior at and around the uncertainty threshold.
func TestConfidence_ThresholdTransitions(t *testing.T) {
	t.Run("transition at exactly 0.7", func(t *testing.T) {
		conf := NewConfidence(0.7)

		// At exactly 0.7, IsCertain returns true and IsUncertain returns false.
		// IsCertain: c >= 0.7 (true)
		// IsUncertain: c < 0.7 (false)
		if !conf.IsCertain() {
			t.Errorf("At threshold 0.7, IsCertain() should be true (uses >=)")
		}

		if conf.IsUncertain() {
			t.Errorf("At threshold 0.7, IsUncertain() should be false (uses <)")
		}

		// The threshold value is certain enough for automation and not flagged for review.
	})

	t.Run("transition just below threshold", func(t *testing.T) {
		conf := NewConfidence(0.699999999)

		if conf.IsCertain() {
			t.Errorf("Just below threshold, IsCertain() should be false")
		}

		if !conf.IsUncertain() {
			t.Errorf("Just below threshold, IsUncertain() should be true")
		}
	})

	t.Run("transition just above threshold", func(t *testing.T) {
		conf := NewConfidence(0.700000001)

		if !conf.IsCertain() {
			t.Errorf("Just above threshold, IsCertain() should be true")
		}

		if conf.IsUncertain() {
			t.Errorf("Just above threshold, IsUncertain() should be false")
		}
	})
}

// TestConfidence_ErrorMessageBehavior documents the behavior for invalid inputs.
// Note: NewConfidence does not return errors - it silently clamps values to [0.0, 1.0].
// This test documents and validates that design decision.
func TestConfidence_ErrorMessageBehavior(t *testing.T) {
	t.Run("invalid inputs are silently clamped, no errors returned", func(t *testing.T) {
		// This test documents the design: invalid inputs are clamped, no errors
		invalidInputs := []float64{-1.0, -10.0, 1.5, 2.0, math.Inf(-1), math.Inf(1)}

		for _, input := range invalidInputs {
			// NewConfidence should never panic or return an error
			conf := NewConfidence(input)

			// Should always return a valid confidence in [0.0, 1.0]
			if conf.Float64() < 0.0 || conf.Float64() > 1.0 {
				t.Errorf("NewConfidence(%v) = %v, which is outside [0.0, 1.0]", input, conf.Float64())
			}

			// All methods should work without panicking
			_ = conf.IsCertain()
			_ = conf.IsUncertain()
			_ = conf.Level()
			_ = conf.NeedsManualReview()
		}
	})

	t.Run("NaN handling", func(t *testing.T) {
		// NaN is a special case - comparisons with NaN are always false
		// So NaN < 0.0 is false, and NaN > 1.0 is false
		// Therefore NaN falls through to the final return statement
		conf := NewConfidence(math.NaN())

		// The result should be a valid Confidence value
		// Check if it's NaN
		if !math.IsNaN(float64(conf)) {
			// If not NaN, verify it's in valid range
			if conf.Float64() < 0.0 || conf.Float64() > 1.0 {
				t.Errorf("NewConfidence(NaN) = %v, which is outside [0.0, 1.0]", conf.Float64())
			}
		}
		// If it is NaN, that's also acceptable behavior - it documents that NaN passes through
	})
}

// TestConfidence_BoundaryValueFormatting tests string representation and formatting of boundary values.
func TestConfidence_BoundaryValueFormatting(t *testing.T) {
	testCases := []struct {
		confidence Confidence
		expected   string
	}{
		{ConfidenceMin, "Very Low"},
		{Confidence(0.5), "Low"},
		{ConfidenceModerate, "Moderate"},
		{ConfidenceHigh, "High"},
		{ConfidenceMax, "Very High"},
	}

	for _, tt := range testCases {
		t.Run(tt.expected, func(t *testing.T) {
			level := tt.confidence.Level()
			if level != tt.expected {
				t.Errorf("Confidence(%v).Level() = %q, want %q", tt.confidence, level, tt.expected)
			}

			// Test that formatting works correctly
			formatted := fmt.Sprintf("%.0f%%", tt.confidence.Float64()*100)
			expectedPercent := fmt.Sprintf("%.0f%%", float64(tt.confidence)*100)
			if formatted != expectedPercent {
				t.Errorf("Format %%v = %q, want %q", formatted, expectedPercent)
			}
		})
	}
}
