package testutil

import (
	"math"
	"testing"
)

// TestCalculateConfidence_NoSignals tests confidence calculation with no signals.
func TestCalculateConfidence_NoSignals(t *testing.T) {
	testCases := []struct {
		name     string
		base     float64
		expected Confidence
	}{
		{"zero base", 0.0, ConfidenceMin},
		{"moderate base", 0.5, Confidence(0.5)},
		{"high base", 0.8, Confidence(0.8)},
		{"maximum base", 1.0, ConfidenceMax},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			params := ConfidenceCalculationParams{
				BaseConfidence: tt.base,
				Signals:        []MatchSignal{},
			}
			result := CalculateConfidence(params)
			if result != tt.expected {
				t.Errorf("CalculateConfidence() = %.2f, want %.2f", result, tt.expected)
			}
		})
	}
}

// TestCalculateConfidence_SingleSignal tests confidence calculation with a single signal.
func TestCalculateConfidence_SingleSignal(t *testing.T) {
	testCases := []struct {
		name           string
		base           float64
		signalType     MatchSignalType
		strength       float64
		minExpected    float64
		maxExpected    float64
	}{
		// Exact match signals (weight: 1.0)
		{
			name:        "exact match with moderate base",
			base:        0.5,
			signalType:  SignalExactMatch,
			strength:    1.0,
			minExpected: 0.75,
			maxExpected: 0.85,
		},
		{
			name:        "exact match with high base",
			base:        0.8,
			signalType:  SignalExactMatch,
			strength:    1.0,
			minExpected: 0.90,
			maxExpected: 1.0,
		},
		{
			name:        "exact match with low strength",
			base:        0.5,
			signalType:  SignalExactMatch,
			strength:    0.5,
			minExpected: 0.65,
			maxExpected: 0.75,
		},

		// Keyword match signals (weight: 0.9)
		{
			name:        "keyword match with moderate base",
			base:        0.5,
			signalType:  SignalKeywordMatch,
			strength:    1.0,
			minExpected: 0.72,
			maxExpected: 0.82,
		},
		{
			name:        "keyword match with high base",
			base:        0.7,
			signalType:  SignalKeywordMatch,
			strength:    1.0,
			minExpected: 0.85,
			maxExpected: 0.95,
		},

		// Contextual match signals (weight: 0.8)
		{
			name:        "contextual match with moderate base",
			base:        0.5,
			signalType:  SignalContextualMatch,
			strength:    1.0,
			minExpected: 0.68,
			maxExpected: 0.78,
		},

		// Partial match signals (weight: 0.6)
		{
			name:        "partial match with moderate base",
			base:        0.5,
			signalType:  SignalPartialMatch,
			strength:    1.0,
			minExpected: 0.65,
			maxExpected: 0.75,
		},

		// Inferred signals (weight: 0.4)
		{
			name:        "inferred with moderate base",
			base:        0.5,
			signalType:  SignalInferred,
			strength:    1.0,
			minExpected: 0.60,
			maxExpected: 0.70,
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
				t.Errorf("CalculateConfidence() = %.2f, want [%.2f, %.2f]", result, tt.minExpected, tt.maxExpected)
			}
		})
	}
}

// TestCalculateConfidence_MultipleSignals tests confidence calculation with multiple signals.
func TestCalculateConfidence_MultipleSignals(t *testing.T) {
	testCases := []struct {
		name        string
		base        float64
		signals     []MatchSignal
		minExpected float64
		maxExpected float64
	}{
		{
			name: "two exact matches",
			base: 0.5,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "pattern1", Strength: 1.0},
				{Type: SignalExactMatch, Pattern: "pattern2", Strength: 1.0},
			},
			minExpected: 0.83,
			maxExpected: 0.93,
		},
		{
			name: "three strong signals",
			base: 0.6,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "pattern1", Strength: 1.0},
				{Type: SignalKeywordMatch, Pattern: "pattern2", Strength: 1.0},
				{Type: SignalContextualMatch, Pattern: "pattern3", Strength: 1.0},
			},
			minExpected: 0.90,
			maxExpected: 1.0,
		},
		{
			name: "mixed strong and weak signals",
			base: 0.5,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "pattern1", Strength: 1.0},
				{Type: SignalPartialMatch, Pattern: "pattern2", Strength: 1.0},
				{Type: SignalInferred, Pattern: "pattern3", Strength: 1.0},
			},
			minExpected: 0.80,
			maxExpected: 0.90,
		},
		{
			name: "all weak signals",
			base: 0.3,
			signals: []MatchSignal{
				{Type: SignalPartialMatch, Pattern: "pattern1", Strength: 1.0},
				{Type: SignalInferred, Pattern: "pattern2", Strength: 1.0},
			},
			minExpected: 0.50,
			maxExpected: 0.60,
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
				t.Errorf("CalculateConfidence() = %.2f, want [%.2f, %.2f]", result, tt.minExpected, tt.maxExpected)
			}
		})
	}
}

// TestCalculateConfidence_AmbiguityPenalty tests confidence calculation with ambiguity penalties.
func TestCalculateConfidence_AmbiguityPenalty(t *testing.T) {
	testCases := []struct {
		name            string
		base            float64
		signals         []MatchSignal
		ambiguityPenalty float64
		minExpected     float64
		maxExpected     float64
	}{
		{
			name: "no penalty",
			base: 0.7,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "pattern1", Strength: 1.0},
			},
			ambiguityPenalty: 0.0,
			minExpected:      0.85,
			maxExpected:      0.95,
		},
		{
			name: "light penalty (10%)",
			base: 0.7,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "pattern1", Strength: 1.0},
			},
			ambiguityPenalty: 0.1,
			minExpected:      0.76,
			maxExpected:      0.86,
		},
		{
			name: "moderate penalty (25%)",
			base: 0.7,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "pattern1", Strength: 1.0},
			},
			ambiguityPenalty: 0.25,
			minExpected:      0.64,
			maxExpected:      0.74,
		},
		{
			name: "heavy penalty (50%)",
			base: 0.7,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "pattern1", Strength: 1.0},
			},
			ambiguityPenalty: 0.5,
			minExpected:      0.42,
			maxExpected:      0.52,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			params := ConfidenceCalculationParams{
				BaseConfidence:   tt.base,
				Signals:          tt.signals,
				AmbiguityPenalty: tt.ambiguityPenalty,
			}
			result := CalculateConfidence(params)
			resultFloat := result.Float64()
			if resultFloat < tt.minExpected || resultFloat > tt.maxExpected {
				t.Errorf("CalculateConfidence() = %.2f, want [%.2f, %.2f]", result, tt.minExpected, tt.maxExpected)
			}
		})
	}
}

// TestCalculateConfidence_ContextBoost tests confidence calculation with context boosts.
func TestCalculateConfidence_ContextBoost(t *testing.T) {
	testCases := []struct {
		name        string
		base        float64
		signals     []MatchSignal
		contextBoost float64
		minExpected float64
		maxExpected float64
	}{
		{
			name: "no boost",
			base: 0.6,
			signals: []MatchSignal{
				{Type: SignalKeywordMatch, Pattern: "pattern1", Strength: 1.0},
			},
			contextBoost: 0.0,
			minExpected:  0.74,
			maxExpected:  0.84,
		},
		{
			name: "light boost (10%)",
			base: 0.6,
			signals: []MatchSignal{
				{Type: SignalKeywordMatch, Pattern: "pattern1", Strength: 1.0},
			},
			contextBoost: 0.1,
			minExpected:  0.77,
			maxExpected:  0.87,
		},
		{
			name: "moderate boost (25%)",
			base: 0.6,
			signals: []MatchSignal{
				{Type: SignalKeywordMatch, Pattern: "pattern1", Strength: 1.0},
			},
			contextBoost: 0.25,
			minExpected:  0.80,
			maxExpected:  0.90,
		},
		{
			name: "high boost with multiple signals",
			base: 0.7,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "pattern1", Strength: 1.0},
				{Type: SignalContextualMatch, Pattern: "pattern2", Strength: 1.0},
			},
			contextBoost: 0.3,
			minExpected:  0.88,
			maxExpected:  1.0,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			params := ConfidenceCalculationParams{
				BaseConfidence: tt.base,
				Signals:        tt.signals,
				ContextBoost:   tt.contextBoost,
			}
			result := CalculateConfidence(params)
			resultFloat := result.Float64()
			if resultFloat < tt.minExpected || resultFloat > tt.maxExpected {
				t.Errorf("CalculateConfidence() = %.2f, want [%.2f, %.2f]", result, tt.minExpected, tt.maxExpected)
			}
		})
	}
}

// TestCalculateConfidence_CombinedEffects tests confidence calculation with combined effects.
func TestCalculateConfidence_CombinedEffects(t *testing.T) {
	testCases := []struct {
		name            string
		base            float64
		signals         []MatchSignal
		ambiguityPenalty float64
		contextBoost    float64
		minExpected     float64
		maxExpected     float64
	}{
		{
			name: "high base with penalty and boost",
			base: 0.8,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "DATA RACE", Strength: 1.0},
			},
			ambiguityPenalty: 0.15,
			contextBoost:     0.2,
			minExpected:      0.82,
			maxExpected:      0.92,
		},
		{
			name: "low base with multiple weak signals and boost",
			base: 0.3,
			signals: []MatchSignal{
				{Type: SignalPartialMatch, Pattern: "error", Strength: 1.0},
				{Type: SignalInferred, Pattern: "test", Strength: 1.0},
			},
			ambiguityPenalty: 0.0,
			contextBoost:     0.3,
			minExpected:      0.65,
			maxExpected:      0.75,
		},
		{
			name: "moderate base with strong signals, penalty, and boost",
			base: 0.5,
			signals: []MatchSignal{
				{Type: SignalKeywordMatch, Pattern: "timeout", Strength: 1.0},
				{Type: SignalContextualMatch, Pattern: "context", Strength: 0.8},
			},
			ambiguityPenalty: 0.2,
			contextBoost:     0.15,
			minExpected:      0.61,
			maxExpected:      0.71,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			params := ConfidenceCalculationParams{
				BaseConfidence:   tt.base,
				Signals:          tt.signals,
				AmbiguityPenalty: tt.ambiguityPenalty,
				ContextBoost:     tt.contextBoost,
			}
			result := CalculateConfidence(params)
			resultFloat := result.Float64()
			if resultFloat < tt.minExpected || resultFloat > tt.maxExpected {
				t.Errorf("CalculateConfidence() = %.2f, want [%.2f, %.2f]", result, tt.minExpected, tt.maxExpected)
			}
		})
	}
}

// TestCalculateConfidence_Boundaries tests confidence calculation boundary conditions.
func TestCalculateConfidence_Boundaries(t *testing.T) {
	testCases := []struct {
		name     string
		params   ConfidenceCalculationParams
		expected Confidence
	}{
		{
			name: "maximum confidence with strong signals",
			params: ConfidenceCalculationParams{
				BaseConfidence: 1.0,
				Signals: []MatchSignal{
					{Type: SignalExactMatch, Pattern: "pattern1", Strength: 1.0},
					{Type: SignalExactMatch, Pattern: "pattern2", Strength: 1.0},
					{Type: SignalExactMatch, Pattern: "pattern3", Strength: 1.0},
				},
				ContextBoost: 1.0,
			},
			expected: ConfidenceMax,
		},
		{
			name: "minimum confidence with zero base and no signals",
			params: ConfidenceCalculationParams{
				BaseConfidence: 0.0,
				Signals:        []MatchSignal{},
			},
			expected: ConfidenceMin,
		},
		{
			name: "low base with weak signals stays low",
			params: ConfidenceCalculationParams{
				BaseConfidence: 0.1,
				Signals: []MatchSignal{
					{Type: SignalInferred, Pattern: "pattern", Strength: 0.5},
				},
				AmbiguityPenalty: 0.5,
			},
			expected: Confidence(0.1), // Should stay at minimum
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateConfidence(tt.params)
			if result != tt.expected && math.Abs(result.Float64()-tt.expected.Float64()) > 0.01 {
				t.Errorf("CalculateConfidence() = %.2f, want %.2f", result, tt.expected)
			}
		})
	}
}

// TestCalculateConfidence_SignalStrengthModifier tests signal strength modifiers.
func TestCalculateConfidence_SignalStrengthModifier(t *testing.T) {
	testCases := []struct {
		name        string
		base        float64
		signal      MatchSignal
		minExpected float64
		maxExpected float64
	}{
		{
			name: "reduced strength (0.5)",
			base: 0.5,
			signal: MatchSignal{
				Type:     SignalExactMatch,
				Pattern:  "pattern",
				Strength: 0.5,
			},
			minExpected: 0.62,
			maxExpected: 0.72,
		},
		{
			name: "increased strength (1.5)",
			base: 0.5,
			signal: MatchSignal{
				Type:     SignalExactMatch,
				Pattern:  "pattern",
				Strength: 1.5,
			},
			minExpected: 0.78,
			maxExpected: 0.88,
		},
		{
			name: "very low strength (0.3)",
			base: 0.6,
			signal: MatchSignal{
				Type:     SignalKeywordMatch,
				Pattern:  "pattern",
				Strength: 0.3,
			},
			minExpected: 0.63,
			maxExpected: 0.73,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			params := ConfidenceCalculationParams{
				BaseConfidence: tt.base,
				Signals:        []MatchSignal{tt.signal},
			}
			result := CalculateConfidence(params)
			resultFloat := result.Float64()
			if resultFloat < tt.minExpected || resultFloat > tt.maxExpected {
				t.Errorf("CalculateConfidence() = %.2f, want [%.2f, %.2f]", result, tt.minExpected, tt.maxExpected)
			}
		})
	}
}

// TestCalculateConfidence_Normalization tests the normalization behavior.
func TestCalculateConfidence_Normalization(t *testing.T) {
	testCases := []struct {
		name             string
		base             float64
		signals          []MatchSignal
		expectedBehavior string
	}{
		{
			name: "many weak signals contribute meaningfully",
			base: 0.5,
			signals: []MatchSignal{
				{Type: SignalInferred, Pattern: "p1", Strength: 1.0},
				{Type: SignalInferred, Pattern: "p2", Strength: 1.0},
				{Type: SignalInferred, Pattern: "p3", Strength: 1.0},
				{Type: SignalInferred, Pattern: "p4", Strength: 1.0},
				{Type: SignalInferred, Pattern: "p5", Strength: 1.0},
			},
			expectedBehavior: "many weak signals should accumulate but with diminishing returns",
		},
		{
			name: "multiple strong signals have diminishing returns",
			base: 0.5,
			signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "p1", Strength: 1.0},
				{Type: SignalExactMatch, Pattern: "p2", Strength: 1.0},
				{Type: SignalExactMatch, Pattern: "p3", Strength: 1.0},
			},
			expectedBehavior: "should approach but not reach 1.0 despite 3 exact matches",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			params := ConfidenceCalculationParams{
				BaseConfidence: tt.base,
				Signals:        tt.signals,
			}
			result := CalculateConfidence(params).Float64()

			// Test that many weak signals have diminishing returns
			if len(tt.signals) > 3 && tt.signals[0].Type == SignalInferred {
				// 5 inferred signals with normalization should not exceed reasonable bounds
				if result >= 0.95 {
					t.Errorf("Many weak signals should have diminishing returns, got %.2f", result)
				}
				// But should still contribute meaningfully
				if result <= 0.6 {
					t.Errorf("Many weak signals should contribute meaningfully, got %.2f", result)
				}
			}

			// Test that multiple strong signals don't reach 1.0 without boost
			if len(tt.signals) == 3 && tt.signals[0].Type == SignalExactMatch && result >= 0.99 {
				t.Errorf("Multiple strong signals should have diminishing returns, got %.2f", result)
			}
			// But should still be high
			if len(tt.signals) == 3 && tt.signals[0].Type == SignalExactMatch && result < 0.85 {
				t.Errorf("Multiple strong signals should still result in high confidence, got %.2f", result)
			}
		})
	}
}
