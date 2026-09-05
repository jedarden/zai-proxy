// Package config tests configuration parsing and helpers.
package config

import (
	"reflect"
	"testing"
)

// TestSplitTargets verifies comma-splitting behavior with various inputs.
func TestSplitTargets(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string returns nil",
			input:    "",
			expected: nil,
		},
		{
			name:     "single value",
			input:    "http://example.com:8080/metrics",
			expected: []string{"http://example.com:8080/metrics"},
		},
		{
			name:  "multiple comma-separated values",
			input: "http://proxy1:8080/metrics,http://proxy2:8080/metrics,http://proxy3:8080/metrics",
			expected: []string{
				"http://proxy1:8080/metrics",
				"http://proxy2:8080/metrics",
				"http://proxy3:8080/metrics",
			},
		},
		{
			name:  "two values",
			input: "http://zai-proxy:8080/metrics,http://zai-proxy-canary:8080/metrics",
			expected: []string{
				"http://zai-proxy:8080/metrics",
				"http://zai-proxy-canary:8080/metrics",
			},
		},
		{
			name:  "empty strings between commas are skipped",
			input: "http://proxy1:8080/metrics,,http://proxy2:8080/metrics",
			expected: []string{
				"http://proxy1:8080/metrics",
				"http://proxy2:8080/metrics",
			},
		},
		{
			name:     "multiple consecutive commas - all empties skipped",
			input:    "a,,,b",
			expected: []string{"a", "b"},
		},
		{
			name:     "trailing comma - empty suffix skipped",
			input:    "http://proxy:8080/metrics,",
			expected: []string{"http://proxy:8080/metrics"},
		},
		{
			name:     "leading comma - empty prefix skipped",
			input:    ",http://proxy:8080/metrics",
			expected: []string{"http://proxy:8080/metrics"},
		},
		{
			name:     "only commas returns nil",
			input:    ",,,",
			expected: nil,
		},
		{
			name:  "whitespace is preserved in values",
			input: " http://proxy1:8080/metrics , http://proxy2:8080/metrics ",
			expected: []string{
				" http://proxy1:8080/metrics ",
				" http://proxy2:8080/metrics ",
			},
		},
		{
			name:     "single value with surrounding whitespace",
			input:    "  http://proxy:8080/metrics  ",
			expected: []string{"  http://proxy:8080/metrics  "},
		},
		{
			name:  "whitespace between commas is treated as non-empty",
			input: "http://proxy1:8080/metrics,  ,http://proxy2:8080/metrics",
			expected: []string{
				"http://proxy1:8080/metrics",
				"  ",
				"http://proxy2:8080/metrics",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SplitTargets(tt.input)

			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("SplitTargets(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
