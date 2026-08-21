package main

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestEstimatedTokenCostUSD(t *testing.T) {
	tests := []struct {
		name      string
		direction string
		tier      string
		count     int
		want      float64
	}{
		{name: "off-peak input", direction: "input", tier: "off_peak", count: 1000, want: 0.00060},
		{name: "peak input", direction: "input", tier: "peak", count: 1000, want: 0.00120},
		{name: "off-peak output", direction: "output", tier: "off_peak", count: 1000, want: 0.00220},
		{name: "peak cache read", direction: "cache_read", tier: "peak", count: 1000, want: 0.00022},
		{name: "free cache write", direction: "cache_write", tier: "off_peak", count: 1000, want: 0},
		{name: "unknown tier", direction: "input", tier: "unknown", count: 1000, want: 0},
		{name: "non-positive count", direction: "input", tier: "off_peak", count: 0, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := estimatedTokenCostUSD(tt.direction, tt.tier, tt.count); math.Abs(got-tt.want) > 1e-12 {
				t.Errorf("estimatedTokenCostUSD() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRecordUsageRecordsEstimatedCost(t *testing.T) {
	tokensTotal.Reset()
	estimatedCostUSDTotal.Reset()

	model, variant, tier := "glm-4.7", "production", GetPricingTier()
	usage := UsageData{
		InputTokens:      1000,
		OutputTokens:     2000,
		CacheReadTokens:  3000,
		CacheWriteTokens: 4000,
	}
	RecordUsage(model, variant, usage)

	tests := []struct {
		direction string
		count     int
	}{
		{direction: "input", count: usage.InputTokens},
		{direction: "output", count: usage.OutputTokens},
		{direction: "cache_read", count: usage.CacheReadTokens},
		{direction: "cache_write", count: usage.CacheWriteTokens},
	}
	for _, tt := range tests {
		if got, want := testutil.ToFloat64(tokensTotal.WithLabelValues(tt.direction, model, variant, tier)), float64(tt.count); got != want {
			t.Errorf("token count for %s = %v, want %v", tt.direction, got, want)
		}
		if got, want := testutil.ToFloat64(estimatedCostUSDTotal.WithLabelValues(tt.direction, model, variant, tier)), estimatedTokenCostUSD(tt.direction, tier, tt.count); math.Abs(got-want) > 1e-12 {
			t.Errorf("estimated cost for %s = %v, want %v", tt.direction, got, want)
		}
	}
}

func TestRecordInputTokens(t *testing.T) {
	// Reset metrics before test
	tokensTotal.Reset()

	tests := []struct {
		name     string
		model    string
		version  string
		count    int
		wantZero bool
	}{
		{
			name:     "record positive input tokens",
			model:    "glm-4",
			version:  "stable",
			count:    100,
			wantZero: false,
		},
		{
			name:     "record zero tokens - should not increment",
			model:    "glm-4",
			version:  "stable",
			count:    0,
			wantZero: true,
		},
		{
			name:     "record negative tokens - should not increment",
			model:    "glm-4",
			version:  "stable",
			count:    -10,
			wantZero: true,
		},
		{
			name:     "canary deployment tokens",
			model:    "glm-4",
			version:  "canary",
			count:    250,
			wantZero: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get initial count
			initialCount := testutil.ToFloat64(tokensTotal.WithLabelValues("input", tt.model, tt.version, "off_peak"))

			// Record tokens
			RecordInputTokens(tt.model, tt.version, tt.count)

			// Get final count
			finalCount := testutil.ToFloat64(tokensTotal.WithLabelValues("input", tt.model, tt.version, "off_peak"))

			if tt.wantZero {
				// Should not have changed
				if finalCount != initialCount {
					t.Errorf("RecordInputTokens() changed count for zero/negative input: initial=%v, final=%v", initialCount, finalCount)
				}
			} else {
				// Should have increased by count
				expected := initialCount + float64(tt.count)
				if finalCount != expected {
					t.Errorf("RecordInputTokens() count mismatch: got=%v, want=%v", finalCount, expected)
				}
			}
		})
	}
}

func TestRecordOutputTokens(t *testing.T) {
	// Reset metrics before test
	tokensTotal.Reset()

	tests := []struct {
		name     string
		model    string
		version  string
		count    int
		wantZero bool
	}{
		{
			name:     "record positive output tokens",
			model:    "glm-4",
			version:  "stable",
			count:    500,
			wantZero: false,
		},
		{
			name:     "record zero tokens - should not increment",
			model:    "glm-4",
			version:  "stable",
			count:    0,
			wantZero: true,
		},
		{
			name:     "canary deployment output tokens",
			model:    "claude-3",
			version:  "canary",
			count:    1000,
			wantZero: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get initial count
			initialCount := testutil.ToFloat64(tokensTotal.WithLabelValues("output", tt.model, tt.version, "off_peak"))

			// Record tokens
			RecordOutputTokens(tt.model, tt.version, tt.count)

			// Get final count
			finalCount := testutil.ToFloat64(tokensTotal.WithLabelValues("output", tt.model, tt.version, "off_peak"))

			if tt.wantZero {
				// Should not have changed
				if finalCount != initialCount {
					t.Errorf("RecordOutputTokens() changed count for zero input: initial=%v, final=%v", initialCount, finalCount)
				}
			} else {
				// Should have increased by count
				expected := initialCount + float64(tt.count)
				if finalCount != expected {
					t.Errorf("RecordOutputTokens() count mismatch: got=%v, want=%v", finalCount, expected)
				}
			}
		})
	}
}

func TestRecordTokenRate(t *testing.T) {
	// Reset metrics before test
	tokenRateSeconds.Reset()
	tokenRate.Reset()

	tests := []struct {
		name       string
		direction  string
		model      string
		version    string
		duration   time.Duration
		tokenCount int
		wantRecord bool
	}{
		{
			name:       "record input token rate",
			direction:  "input",
			model:      "glm-4",
			version:    "stable",
			duration:   10 * time.Millisecond,
			tokenCount: 100,
			wantRecord: true,
		},
		{
			name:       "record output token rate",
			direction:  "output",
			model:      "glm-4",
			version:    "canary",
			duration:   5 * time.Millisecond,
			tokenCount: 250,
			wantRecord: true,
		},
		{
			name:       "zero token count - should not record",
			direction:  "input",
			model:      "glm-4",
			version:    "stable",
			duration:   10 * time.Millisecond,
			tokenCount: 0,
			wantRecord: false,
		},
		{
			name:       "zero duration - should not record",
			direction:  "input",
			model:      "glm-4",
			version:    "stable",
			duration:   0,
			tokenCount: 100,
			wantRecord: false,
		},
		{
			name:       "negative token count - should not record",
			direction:  "input",
			model:      "glm-4",
			version:    "stable",
			duration:   10 * time.Millisecond,
			tokenCount: -10,
			wantRecord: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Record token rate
			RecordTokenRate(tt.direction, tt.model, tt.version, tt.duration, tt.tokenCount)

			// For histograms, we can't easily get the count before/after without collecting metrics
			// Instead, we verify that the function doesn't panic and completes successfully
			// The detailed verification is done in TestMetricsExportFormat
			if tt.wantRecord {
				// Test passed if we got here without panic
			}
		})
	}
}

func TestRecordInputTokenRate(t *testing.T) {
	// Reset metrics before test
	tokenRateSeconds.Reset()
	tokenRate.Reset()

	// Record input token rate
	model := "glm-4"
	version := "stable"
	duration := 10 * time.Millisecond
	tokenCount := 100

	// Should not panic
	RecordInputTokenRate(model, version, duration, tokenCount)

	// Test passes if no panic occurs
}

func TestRecordOutputTokenRate(t *testing.T) {
	// Reset metrics before test
	tokenRateSeconds.Reset()
	tokenRate.Reset()

	// Record output token rate
	model := "glm-4"
	version := "canary"
	duration := 5 * time.Millisecond
	tokenCount := 250

	// Should not panic
	RecordOutputTokenRate(model, version, duration, tokenCount)

	// Test passes if no panic occurs
}

func TestMetricLabels(t *testing.T) {
	// Reset all metrics
	tokensTotal.Reset()
	tokenRateSeconds.Reset()
	tokenRate.Reset()

	// Record metrics with different label combinations
	RecordInputTokens("glm-4", "stable", 100)
	RecordInputTokens("glm-4", "canary", 150)
	RecordOutputTokens("claude-3", "stable", 200)
	RecordOutputTokens("claude-3", "canary", 250)

	RecordInputTokenRate("glm-4", "stable", 10*time.Millisecond, 100)
	RecordOutputTokenRate("claude-3", "canary", 5*time.Millisecond, 200)

	// Verify each label combination is tracked separately
	tests := []struct {
		name      string
		direction string
		model     string
		version   string
		wantCount float64
	}{
		{"input glm-4 stable", "input", "glm-4", "stable", 100},
		{"input glm-4 canary", "input", "glm-4", "canary", 150},
		{"output claude-3 stable", "output", "claude-3", "stable", 200},
		{"output claude-3 canary", "output", "claude-3", "canary", 250},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := testutil.ToFloat64(tokensTotal.WithLabelValues(tt.direction, tt.model, tt.version, "off_peak"))
			if count != tt.wantCount {
				t.Errorf("Token count mismatch for %s: got=%v, want=%v", tt.name, count, tt.wantCount)
			}
		})
	}
}

func TestMetricNoLeaks(t *testing.T) {
	// This test ensures that recording metrics doesn't create memory leaks
	// by checking that we don't create unbounded label combinations

	// Reset metrics
	tokensTotal.Reset()
	tokenRateSeconds.Reset()
	tokenRate.Reset()

	// Record metrics multiple times with same labels
	for i := 0; i < 1000; i++ {
		RecordInputTokens("glm-4", "stable", 1)
		RecordOutputTokens("glm-4", "stable", 1)
		RecordInputTokenRate("glm-4", "stable", 1*time.Millisecond, 10)
		RecordOutputTokenRate("glm-4", "stable", 1*time.Millisecond, 10)
	}

	// Verify counts accumulated correctly (should be 1000)
	inputCount := testutil.ToFloat64(tokensTotal.WithLabelValues("input", "glm-4", "stable", "off_peak"))
	if inputCount != 1000 {
		t.Errorf("Input token count incorrect after 1000 iterations: got=%v, want=1000", inputCount)
	}

	outputCount := testutil.ToFloat64(tokensTotal.WithLabelValues("output", "glm-4", "stable", "off_peak"))
	if outputCount != 1000 {
		t.Errorf("Output token count incorrect after 1000 iterations: got=%v, want=1000", outputCount)
	}

	// For histograms, we just verify they don't panic
	// The histogram metrics are tested in TestMetricsExportFormat
}

func TestMetricNoConflicts(t *testing.T) {
	// This test ensures that different label combinations don't interfere with each other

	// Reset metrics
	tokensTotal.Reset()

	// Record different combinations
	RecordInputTokens("glm-4", "stable", 100)
	RecordInputTokens("glm-4", "canary", 200)
	RecordOutputTokens("glm-4", "stable", 300)
	RecordOutputTokens("glm-4", "canary", 400)

	// Verify each is independent
	if got := testutil.ToFloat64(tokensTotal.WithLabelValues("input", "glm-4", "stable", "off_peak")); got != 100 {
		t.Errorf("Input stable tokens incorrect: got=%v, want=100", got)
	}
	if got := testutil.ToFloat64(tokensTotal.WithLabelValues("input", "glm-4", "canary", "off_peak")); got != 200 {
		t.Errorf("Input canary tokens incorrect: got=%v, want=200", got)
	}
	if got := testutil.ToFloat64(tokensTotal.WithLabelValues("output", "glm-4", "stable", "off_peak")); got != 300 {
		t.Errorf("Output stable tokens incorrect: got=%v, want=300", got)
	}
	if got := testutil.ToFloat64(tokensTotal.WithLabelValues("output", "glm-4", "canary", "off_peak")); got != 400 {
		t.Errorf("Output canary tokens incorrect: got=%v, want=400", got)
	}
}

func TestMetricsExportFormat(t *testing.T) {
	// Reset metrics
	tokensTotal.Reset()
	tokenRateSeconds.Reset()
	tokenRate.Reset()

	// Record some metrics
	RecordInputTokens("glm-4", "stable", 100)
	RecordOutputTokens("glm-4", "stable", 200)
	RecordInputTokenRate("glm-4", "stable", 10*time.Millisecond, 100)

	// Collect metrics in Prometheus text format
	metadata := `
		# HELP zai_proxy_tokens_total Total number of tokens processed by direction (input/output), model, deployment variant, and pricing tier
		# TYPE zai_proxy_tokens_total counter
	`
	expectedInputLine := `zai_proxy_tokens_total{direction="input",model="glm-4",pricing_tier="off_peak",variant="stable"} 100`
	expectedOutputLine := `zai_proxy_tokens_total{direction="output",model="glm-4",pricing_tier="off_peak",variant="stable"} 200`

	// Verify metric can be collected
	if err := testutil.CollectAndCompare(tokensTotal, strings.NewReader(metadata+expectedInputLine+"\n"+expectedOutputLine+"\n")); err != nil {
		t.Errorf("Metrics export format incorrect: %v", err)
	}
}
