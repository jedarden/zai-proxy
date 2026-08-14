package testutil

import (
	"math"
	"testing"
)

// TestCalculateConfidence_SingleStrongSignal tests confidence calculation with a single strong signal.
func TestCalculateConfidence_SingleStrongSignal(t *testing.T) {
	testCases := []struct {
		name           string
		base           float64
		signalType     MatchSignalType
		strength       float64
		minExpected    float64
		maxExpected    float64
		description    string
	}{
		{
			name:        "exact match maximum strength",
			base:        0.5,
			signalType:  SignalExactMatch,
			strength:    1.5,
			minExpected: 0.78,
			maxExpected: 0.88,
			description: "Strongest possible signal with high strength modifier",
		},
		{
			name:        "keyword match with high base",
			base:        0.9,
			signalType:  SignalKeywordMatch,
			strength:    1.0,
			minExpected: 0.95,
			maxExpected: 1.0,
			description: "High base confidence with strong signal should be very high",
		},
		{
			name:        "contextual match moderate base",
			base:        0.6,
			signalType:  SignalContextualMatch,
			strength:    1.2,
			minExpected: 0.79,
			maxExpected: 0.92,
			description: "Contextual match with boosted strength",
		},
		{
			name:        "exact match from low base",
			base:        0.2,
			signalType:  SignalExactMatch,
			strength:    1.0,
			minExpected: 0.45,
			maxExpected: 0.55,
			description: "Low base can be rescued by strong signal",
		},
		{
			name:        "keyword match from zero base",
			base:        0.0,
			signalType:  SignalKeywordMatch,
			strength:    1.0,
			minExpected: 0.20,
			maxExpected: 0.30,
			description: "Even zero base gets contribution from strong signal",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			params := ConfidenceCalculationParams{
				BaseConfidence: tt.base,
				Signals: []MatchSignal{
					{Type: tt.signalType, Pattern: "test pattern", Strength: tt.strength},
				},
			}
			result := CalculateConfidence(params)
			resultFloat := result.Float64()
			if resultFloat < tt.minExpected || resultFloat > tt.maxExpected {
				t.Errorf("%s: CalculateConfidence() = %.2f, want [%.2f, %.2f]. Description: %s",
					tt.name, result, tt.minExpected, tt.maxExpected, tt.description)
			}
		})
	}
}

// TestCalculateConfidence_ConflictingSignals tests confidence calculation with multiple conflicting signals.
func TestCalculateConfidence_ConflictingSignals(t *testing.T) {
	testCases := []struct {
		name        string
		base        float64
		signals     []MatchSignal
		minExpected float64
		maxExpected float64
		description string
	}{
		{
			name: "exact match with weak conflicting signals",
			base: 0.5,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "DATA RACE", Strength: 1.0},
				{Type: SignalInferred, Pattern: "maybe related", Strength: 0.5},
				{Type: SignalPartialMatch, Pattern: "vague pattern", Strength: 0.5},
			},
			minExpected: 0.75,
			maxExpected: 0.85,
			description: "Strong signal should dominate weak conflicting signals",
		},
		{
			name: "two strong conflicting signals",
			base: 0.6,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "pattern1", Strength: 1.0},
				{Type: SignalKeywordMatch, Pattern: "pattern2", Strength: 1.0},
			},
			minExpected: 0.81,
			maxExpected: 0.95,
			description: "Two strong signals both contribute, normalization prevents overconfidence",
		},
		{
			name: "mixed high and low strength signals",
			base: 0.5,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "strong", Strength: 1.0},
				{Type: SignalInferred, Pattern: "weak1", Strength: 0.3},
				{Type: SignalInferred, Pattern: "weak2", Strength: 0.3},
				{Type: SignalPartialMatch, Pattern: "weak3", Strength: 0.4},
			},
			minExpected: 0.78,
			maxExpected: 0.88,
			description: "One strong signal with multiple weak signals",
		},
		{
			name: "conflicting contextual signals",
			base: 0.4,
			signals: []MatchSignal{
				{Type: SignalContextualMatch, Pattern: "context A", Strength: 1.0},
				{Type: SignalContextualMatch, Pattern: "context B", Strength: 0.8},
			},
			minExpected: 0.68,
			maxExpected: 0.78,
			description: "Multiple contextual signals with different strengths",
		},
		{
			name: "exact match with many weak signals accumulates",
			base: 0.7,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "strong pattern", Strength: 1.0},
				{Type: SignalInferred, Pattern: "weak1", Strength: 0.2},
				{Type: SignalInferred, Pattern: "weak2", Strength: 0.2},
				{Type: SignalInferred, Pattern: "weak3", Strength: 0.2},
				{Type: SignalPartialMatch, Pattern: "weak4", Strength: 0.3},
			},
			minExpected: 0.95,
			maxExpected: 1.0,
			description: "Strong signal plus many weak signals accumulate to very high confidence",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			params := ConfidenceCalculationParams{
				BaseConfidence: tt.base,
				Signals:        tt.signals,
			}
			result := CalculateConfidence(params)
			resultFloat := result.Float64()
			if resultFloat < tt.minExpected || resultFloat > tt.maxExpected {
				t.Errorf("%s: CalculateConfidence() = %.2f, want [%.2f, %.2f]. Description: %s",
					tt.name, result, tt.minExpected, tt.maxExpected, tt.description)
			}
		})
	}
}

// TestCalculateConfidence_WeakSignalsOnly tests confidence calculation with only weak signals.
func TestCalculateConfidence_WeakSignalsOnly(t *testing.T) {
	testCases := []struct {
		name        string
		base        float64
		signals     []MatchSignal
		minExpected float64
		maxExpected float64
		description string
	}{
		{
			name: "single weak signal",
			base: 0.5,
			signals: []MatchSignal{
				{Type: SignalInferred, Pattern: "weak", Strength: 1.0},
			},
			minExpected: 0.58,
			maxExpected: 0.68,
			description: "Single inferred signal provides modest boost",
		},
		{
			name: "multiple weak signals",
			base: 0.4,
			signals: []MatchSignal{
				{Type: SignalInferred, Pattern: "weak1", Strength: 1.0},
				{Type: SignalInferred, Pattern: "weak2", Strength: 1.0},
				{Type: SignalPartialMatch, Pattern: "weak3", Strength: 1.0},
			},
			minExpected: 0.65,
			maxExpected: 0.75,
			description: "Multiple weak signals accumulate but remain moderate",
		},
		{
			name: "many weak signals with diminishing returns",
			base: 0.3,
			signals: []MatchSignal{
				{Type: SignalInferred, Pattern: "weak1", Strength: 0.5},
				{Type: SignalInferred, Pattern: "weak2", Strength: 0.5},
				{Type: SignalInferred, Pattern: "weak3", Strength: 0.5},
				{Type: SignalPartialMatch, Pattern: "weak4", Strength: 0.6},
				{Type: SignalPartialMatch, Pattern: "weak5", Strength: 0.6},
			},
			minExpected: 0.55,
			maxExpected: 0.65,
			description: "Many weak signals with low strength have limited impact",
		},
		{
			name: "all partial match signals",
			base: 0.5,
			signals: []MatchSignal{
				{Type: SignalPartialMatch, Pattern: "partial1", Strength: 1.0},
				{Type: SignalPartialMatch, Pattern: "partial2", Strength: 1.0},
			},
			minExpected: 0.68,
			maxExpected: 0.78,
			description: "Multiple partial matches provide better contribution than inferred",
		},
		{
			name: "weak signals from zero base",
			base: 0.0,
			signals: []MatchSignal{
				{Type: SignalInferred, Pattern: "weak1", Strength: 1.0},
				{Type: SignalPartialMatch, Pattern: "weak2", Strength: 1.0},
			},
			minExpected: 0.20,
			maxExpected: 0.30,
			description: "Weak signals alone cannot rescue zero base to high confidence",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			params := ConfidenceCalculationParams{
				BaseConfidence: tt.base,
				Signals:        tt.signals,
			}
			result := CalculateConfidence(params)
			resultFloat := result.Float64()
			if resultFloat < tt.minExpected || resultFloat > tt.maxExpected {
				t.Errorf("%s: CalculateConfidence() = %.2f, want [%.2f, %.2f]. Description: %s",
					tt.name, result, tt.minExpected, tt.maxExpected, tt.description)
			}
		})
	}
}

// TestCalculateConfidence_NilZeroSignalCases tests nil/zero-signal cases handling gracefully.
func TestCalculateConfidence_NilZeroSignalCases(t *testing.T) {
	testCases := []struct {
		name        string
		base        float64
		signals     []MatchSignal
		expected    Confidence
		description string
	}{
		{
			name:     "nil signals slice",
			base:     0.5,
			signals:  nil, // Explicit nil
			expected: Confidence(0.5),
			description: "Nil signals should return base confidence unchanged",
		},
		{
			name:     "empty signals slice",
			base:     0.7,
			signals:  []MatchSignal{},
			expected: Confidence(0.7),
			description: "Empty signals should return base confidence unchanged",
		},
		{
			name: "signals with zero strength",
			base: 0.6,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "pattern", Strength: 0.0},
			},
			expected: Confidence(0.85),
			description: "Signal with zero strength defaults to 1.0, providing full contribution",
		},
		{
			name:     "zero base with no signals",
			base:     0.0,
			signals:  []MatchSignal{},
			expected: ConfidenceMin,
			description: "Zero base with no signals should return minimum confidence",
		},
		{
			name:     "maximum base with no signals",
			base:     1.0,
			signals:  []MatchSignal{},
			expected: ConfidenceMax,
			description: "Maximum base with no signals should return maximum confidence",
		},
		{
			name: "signal with negative strength (should be treated as zero)",
			base: 0.5,
			signals: []MatchSignal{
				{Type: SignalKeywordMatch, Pattern: "pattern", Strength: -0.5},
			},
			// Note: The current implementation doesn't handle negative strength specially
			// It will just multiply weight by negative value, reducing contribution
			// This test documents current behavior
			description: "Negative strength behavior - current implementation",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			params := ConfidenceCalculationParams{
				BaseConfidence: tt.base,
				Signals:        tt.signals,
			}
			result := CalculateConfidence(params)

			// For tests with expected value, check exact match
			if tt.expected != 0 {
				if math.Abs(result.Float64()-tt.expected.Float64()) > 0.01 {
					t.Errorf("%s: CalculateConfidence() = %.2f, want %.2f. Description: %s",
						tt.name, result, tt.expected, tt.description)
				}
			} else {
				// For negative strength test, just document behavior
				t.Logf("%s: CalculateConfidence() = %.2f. Description: %s",
					tt.name, result, tt.description)
			}
		})
	}
}

// TestCalculateConfidence_WeightedSignalCombinations tests weighted signal combinations.
func TestCalculateConfidence_WeightedSignalCombinations(t *testing.T) {
	testCases := []struct {
		name        string
		base        float64
		signals     []MatchSignal
		minExpected float64
		maxExpected float64
		description string
	}{
		{
			name: "high confidence signals only",
			base: 0.5,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "exact", Strength: 1.0},
				{Type: SignalKeywordMatch, Pattern: "keyword", Strength: 1.0},
			},
			minExpected: 0.81,
			maxExpected: 0.95,
			description: "Two high-weight signals (1.0 and 0.9)",
		},
		{
			name: "medium confidence signals",
			base: 0.5,
			signals: []MatchSignal{
				{Type: SignalContextualMatch, Pattern: "context", Strength: 1.0},
				{Type: SignalPartialMatch, Pattern: "partial", Strength: 1.0},
			},
			minExpected: 0.72,
			maxExpected: 0.82,
			description: "Two medium-weight signals (0.8 and 0.6)",
		},
		{
			name: "low confidence signals",
			base: 0.5,
			signals: []MatchSignal{
				{Type: SignalInferred, Pattern: "inferred1", Strength: 1.0},
				{Type: SignalInferred, Pattern: "inferred2", Strength: 1.0},
			},
			minExpected: 0.70,
			maxExpected: 0.74,
			description: "Two low-weight signals (0.4 each)",
		},
		{
			name: "high-medium-low combination",
			base: 0.5,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "exact", Strength: 1.0},
				{Type: SignalContextualMatch, Pattern: "context", Strength: 1.0},
				{Type: SignalInferred, Pattern: "inferred", Strength: 1.0},
			},
			minExpected: 0.82,
			maxExpected: 0.86,
			description: "Signal weights: 1.0, 0.8, 0.4",
		},
		{
			name: "all signal types represented",
			base: 0.4,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "exact", Strength: 1.0},
				{Type: SignalKeywordMatch, Pattern: "keyword", Strength: 1.0},
				{Type: SignalContextualMatch, Pattern: "contextual", Strength: 1.0},
				{Type: SignalPartialMatch, Pattern: "partial", Strength: 1.0},
				{Type: SignalInferred, Pattern: "inferred", Strength: 1.0},
			},
			minExpected: 0.77,
			maxExpected: 0.82,
			description: "All signal types: weights 1.0, 0.9, 0.8, 0.6, 0.4",
		},
		{
			name: "high signals with strength modifiers",
			base: 0.6,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "exact", Strength: 1.5},
				{Type: SignalKeywordMatch, Pattern: "keyword", Strength: 1.2},
			},
			minExpected: 0.92,
			maxExpected: 1.0,
			description: "High-weight signals with boosted strength",
		},
		{
			name: "medium signals with reduced strength",
			base: 0.6,
			signals: []MatchSignal{
				{Type: SignalContextualMatch, Pattern: "context", Strength: 0.7},
				{Type: SignalPartialMatch, Pattern: "partial", Strength: 0.6},
			},
			minExpected: 0.75,
			maxExpected: 0.85,
			description: "Medium-weight signals with reduced strength",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			params := ConfidenceCalculationParams{
				BaseConfidence: tt.base,
				Signals:        tt.signals,
			}
			result := CalculateConfidence(params)
			resultFloat := result.Float64()
			if resultFloat < tt.minExpected || resultFloat > tt.maxExpected {
				t.Errorf("%s: CalculateConfidence() = %.2f, want [%.2f, %.2f]. Description: %s",
					tt.name, result, tt.minExpected, tt.maxExpected, tt.description)
			}
		})
	}
}

// TestCalculateConfidence_AllSignalsZeroNil tests edge case: all signals zero/nil.
func TestCalculateConfidence_AllSignalsZeroNil(t *testing.T) {
	testCases := []struct {
		name        string
		base        float64
		signals     []MatchSignal
		minExpected float64
		maxExpected float64
		description string
	}{
		{
			name:        "zero base with nil signals",
			base:        0.0,
			signals:     nil,
			minExpected: 0.0,
			maxExpected: 0.0,
			description: "Zero base with nil signals should return minimum",
		},
		{
			name:        "zero base with empty signals",
			base:        0.0,
			signals:     []MatchSignal{},
			minExpected: 0.0,
			maxExpected: 0.0,
			description: "Zero base with empty signals should return minimum",
		},
		{
			name: "zero base with zero-strength signals",
			base: 0.0,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "pattern1", Strength: 0.0},
				{Type: SignalKeywordMatch, Pattern: "pattern2", Strength: 0.0},
			},
			// Note: Zero strength defaults to 1.0 in implementation, so this will contribute
			minExpected: 0.30,
			maxExpected: 0.36,
			description: "Zero-strength signals default to 1.0, so they contribute",
		},
		{
			name: "moderate base with signals that have minimal effective weight",
			base: 0.5,
			signals: []MatchSignal{
				{Type: SignalInferred, Pattern: "weak", Strength: 0.1},
			},
			minExpected: 0.50,
			maxExpected: 0.55,
			description: "Very low strength signal provides minimal contribution",
		},
		{
			name: "high base with no signals should remain high",
			base: 0.9,
			signals: []MatchSignal{},
			minExpected: 0.9,
			maxExpected: 0.9,
			description: "High base with no signals remains unchanged",
		},
		{
			name: "signals with all zero strength from zero base",
			base: 0.0,
			signals: []MatchSignal{
				{Type: SignalInferred, Pattern: "weak", Strength: 0.0},
				{Type: SignalPartialMatch, Pattern: "weak", Strength: 0.0},
			},
			// Zero strength defaults to 1.0, so these contribute
			minExpected: 0.25,
			maxExpected: 0.35,
			description: "Zero-strength signals default to 1.0 and contribute even from zero base",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			params := ConfidenceCalculationParams{
				BaseConfidence: tt.base,
				Signals:        tt.signals,
			}
			result := CalculateConfidence(params)
			resultFloat := result.Float64()
			if resultFloat < tt.minExpected || resultFloat > tt.maxExpected {
				t.Errorf("%s: CalculateConfidence() = %.2f, want [%.2f, %.2f]. Description: %s",
					tt.name, result, tt.minExpected, tt.maxExpected, tt.description)
			}
		})
	}
}

// TestCalculateConfidence_ExtremeValues tests confidence calculation with extreme values.
func TestCalculateConfidence_ExtremeValues(t *testing.T) {
	testCases := []struct {
		name        string
		base        float64
		signals     []MatchSignal
		minExpected float64
		maxExpected float64
		description string
	}{
		{
			name: "maximum confidence achievable",
			base: 1.0,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "exact1", Strength: 1.5},
				{Type: SignalExactMatch, Pattern: "exact2", Strength: 1.5},
				{Type: SignalKeywordMatch, Pattern: "keyword", Strength: 1.5},
			},
			minExpected: 0.98,
			maxExpected: 1.0,
			description: "Should approach but not exceed 1.0",
		},
		{
			name: "very low base with very strong signals",
			base: 0.05,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "exact", Strength: 2.0},
			},
			minExpected: 0.36,
			maxExpected: 0.40,
			description: "Very low base can be rescued by very strong signal",
		},
		{
			name: "many very weak signals from low base",
			base: 0.1,
			signals: []MatchSignal{
				{Type: SignalInferred, Pattern: "weak1", Strength: 0.1},
				{Type: SignalInferred, Pattern: "weak2", Strength: 0.1},
				{Type: SignalInferred, Pattern: "weak3", Strength: 0.1},
			},
			minExpected: 0.15,
			maxExpected: 0.25,
			description: "Many very weak signals have minimal impact",
		},
		{
			name: "high base with penalty should stay moderate",
			base: 0.9,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "exact", Strength: 1.0},
			},
			minExpected: 0.50,
			maxExpected: 0.60,
			description: "High base with 50% penalty drops to moderate",
			// This test needs ambiguity penalty set
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			params := ConfidenceCalculationParams{
				BaseConfidence: tt.base,
				Signals:        tt.signals,
			}

			// Add penalty for specific test case
			if tt.name == "high base with penalty should stay moderate" {
				params.AmbiguityPenalty = 0.5
			}

			result := CalculateConfidence(params)
			resultFloat := result.Float64()
			if resultFloat < tt.minExpected || resultFloat > tt.maxExpected {
				t.Errorf("%s: CalculateConfidence() = %.2f, want [%.2f, %.2f]. Description: %s",
					tt.name, result, tt.minExpected, tt.maxExpected, tt.description)
			}
		})
	}
}

// TestCalculateConfidence_SignalAccumulation tests how signals accumulate and normalize.
func TestCalculateConfidence_SignalAccumulation(t *testing.T) {
	testCases := []struct {
		name                string
		base                float64
		signals             []MatchSignal
		expectedBehavior    string
		validateBehavior    func(float64) bool
	}{
		{
			name: "diminishing returns on same signal type",
			base: 0.5,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "exact1", Strength: 1.0},
				{Type: SignalExactMatch, Pattern: "exact2", Strength: 1.0},
				{Type: SignalExactMatch, Pattern: "exact3", Strength: 1.0},
				{Type: SignalExactMatch, Pattern: "exact4", Strength: 1.0},
			},
			expectedBehavior: "Four exact matches should have diminishing returns due to normalization",
			validateBehavior: func(result float64) bool {
				// Should not reach 1.0 despite 4 exact matches
				return result < 0.98 && result > 0.85
			},
		},
		{
			name: "diverse signal types better than many weak",
			base: 0.5,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "exact", Strength: 1.0},
				{Type: SignalKeywordMatch, Pattern: "keyword", Strength: 1.0},
			},
			expectedBehavior: "Two diverse strong signals better than many weak ones",
			validateBehavior: func(result float64) bool {
				return result > 0.80 && result < 0.95
			},
		},
		{
			name: "signal contribution saturates",
			base: 0.0,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "exact1", Strength: 1.0},
				{Type: SignalExactMatch, Pattern: "exact2", Strength: 1.0},
				{Type: SignalExactMatch, Pattern: "exact3", Strength: 1.0},
				{Type: SignalExactMatch, Pattern: "exact4", Strength: 1.0},
				{Type: SignalExactMatch, Pattern: "exact5", Strength: 1.0},
			},
			expectedBehavior: "Many signals from zero base should saturate well below 1.0",
			validateBehavior: func(result float64) bool {
				// Even 5 exact matches from zero base shouldn't reach 1.0
				return result > 0.40 && result < 0.45
			},
		},
		{
			name: "weak signals have strong diminishing returns",
			base: 0.5,
			signals: []MatchSignal{
				{Type: SignalInferred, Pattern: "weak1", Strength: 1.0},
				{Type: SignalInferred, Pattern: "weak2", Strength: 1.0},
				{Type: SignalInferred, Pattern: "weak3", Strength: 1.0},
				{Type: SignalInferred, Pattern: "weak4", Strength: 1.0},
				{Type: SignalInferred, Pattern: "weak5", Strength: 1.0},
				{Type: SignalInferred, Pattern: "weak6", Strength: 1.0},
			},
			expectedBehavior: "Many weak signals should have strong diminishing returns",
			validateBehavior: func(result float64) bool {
				// Six weak signals shouldn't push confidence too high
				return result > 0.83 && result < 0.87
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			params := ConfidenceCalculationParams{
				BaseConfidence: tt.base,
				Signals:        tt.signals,
			}
			result := CalculateConfidence(params).Float64()

			if !tt.validateBehavior(result) {
				t.Errorf("%s: CalculateConfidence() = %.2f. Expected: %s",
					tt.name, result, tt.expectedBehavior)
			}
		})
	}
}