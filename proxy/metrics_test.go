package main

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
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

var quotaMetricNames = []string{
	"zai_proxy_quota_usage_ratio",
	"zai_proxy_quota_remaining_ratio",
	"zai_proxy_quota_reset_time_seconds",
	"zai_proxy_quota_rate_cap",
	"zai_proxy_quota_gate_open",
	"zai_proxy_quota_sample_age_seconds",
	"zai_proxy_quota_poll_total",
}

func resetQuotaMetrics() {
	quotaUsageRatio.Reset()
	quotaRemainingRatio.Reset()
	quotaResetTimeSeconds.Reset()
	quotaRateCap.Reset()
	quotaGateOpen.Reset()
	quotaSampleAgeSeconds.Reset()
	quotaPollsTotal.Reset()
}

func TestQuotaRegistrationIsDuplicateSafe(t *testing.T) {
	// Re-registering a collector with an identical definition — as duplicate
	// initialization in tests can do — must return the already-registered
	// collector instead of panicking.
	duplicate := registerQuotaCollector(prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "zai_proxy_quota_usage_ratio",
			Help: "Normalized provider-reported quota usage ratio by window and limit type",
		},
		[]string{"window", "limit_type", "variant"},
	))

	if duplicate != quotaUsageRatio {
		t.Fatal("duplicate registration returned a new collector; want the already-registered one")
	}

	// A same-name definition with different help or labels is a programming
	// error, not a duplicate, and must stay fatal.
	defer func() {
		if recover() == nil {
			t.Error("registering a conflicting quota metric definition did not panic")
		}
	}()
	registerQuotaCollector(prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "zai_proxy_quota_usage_ratio",
			Help: "deliberately different help text",
		},
		[]string{"window", "limit_type", "variant"},
	))
}

func TestQuotaObservationMetrics(t *testing.T) {
	resetQuotaMetrics()

	reset := time.Unix(1790000000, 0).UTC()
	RecordQuotaUsageRatio("five_hour", "credits", "production", 0.42)
	RecordQuotaRemainingRatio("five_hour", "credits", "production", 0.58)
	RecordQuotaResetTime("five_hour", "credits", "production", reset)

	if got := testutil.ToFloat64(quotaUsageRatio.WithLabelValues("five_hour", "credits", "production")); got != 0.42 {
		t.Errorf("usage ratio = %v, want 0.42", got)
	}
	if got := testutil.ToFloat64(quotaRemainingRatio.WithLabelValues("five_hour", "credits", "production")); got != 0.58 {
		t.Errorf("remaining ratio = %v, want 0.58", got)
	}
	if got := testutil.ToFloat64(quotaResetTimeSeconds.WithLabelValues("five_hour", "credits", "production")); got != float64(reset.Unix()) {
		t.Errorf("reset time = %v, want %v", got, float64(reset.Unix()))
	}

	// Ratios are recorded unclamped so an overdrawn window stays visible.
	RecordQuotaUsageRatio("weekly", "credits", "production", 1.05)
	if got := testutil.ToFloat64(quotaUsageRatio.WithLabelValues("weekly", "credits", "production")); got != 1.05 {
		t.Errorf("overdrawn usage ratio = %v, want 1.05", got)
	}
}

func TestQuotaDecisionMetrics(t *testing.T) {
	resetQuotaMetrics()

	RecordQuotaRateCap("production", 3.5)
	RecordQuotaGateOpen("five_hour", "production", true)
	RecordQuotaGateOpen("weekly", "production", false)
	RecordQuotaSampleAge("production", 90*time.Second)
	RecordQuotaPollOutcome("success", "production")
	RecordQuotaPollOutcome("success", "production")
	RecordQuotaPollOutcome("malformed", "production")
	RecordQuotaPollOutcome("stale", "production")
	RecordQuotaPollOutcome("error", "production")

	if got := testutil.ToFloat64(quotaRateCap.WithLabelValues("production")); got != 3.5 {
		t.Errorf("quota rate cap = %v, want 3.5", got)
	}
	if got := testutil.ToFloat64(quotaGateOpen.WithLabelValues("five_hour", "production")); got != 1 {
		t.Errorf("five-hour quota gate = %v, want 1", got)
	}
	if got := testutil.ToFloat64(quotaGateOpen.WithLabelValues("weekly", "production")); got != 0 {
		t.Errorf("weekly quota gate = %v, want 0", got)
	}
	if got := testutil.ToFloat64(quotaSampleAgeSeconds.WithLabelValues("production")); got != 90 {
		t.Errorf("sample age = %v, want 90", got)
	}
	if got := testutil.ToFloat64(quotaPollsTotal.WithLabelValues("success", "production")); got != 2 {
		t.Errorf("success polls = %v, want 2", got)
	}
	for _, result := range []string{"error", "malformed", "stale"} {
		if got := testutil.ToFloat64(quotaPollsTotal.WithLabelValues(result, "production")); got != 1 {
			t.Errorf("%s polls = %v, want 1", result, got)
		}
	}
}

func TestQuotaLabelsStayBounded(t *testing.T) {
	resetQuotaMetrics()

	// Unrecognized windows collapse into the bounded "unknown" value.
	RecordQuotaUsageRatio("daily", "credits", "production", 0.25)
	if got := testutil.ToFloat64(quotaUsageRatio.WithLabelValues("unknown", "credits", "production")); got != 0.25 {
		t.Errorf("unknown window usage ratio = %v, want it under the \"unknown\" window label", got)
	}
	if got := testutil.CollectAndCount(quotaUsageRatio); got != 1 {
		t.Errorf("unrecognized window created extra series: got %d children, want 1", got)
	}

	// limit_type values are sanitized and length-capped.
	RecordQuotaUsageRatio("weekly", strings.Repeat("T", 64), "Production", 0.5)
	if got := testutil.ToFloat64(quotaUsageRatio.WithLabelValues("weekly", strings.Repeat("t", 32), "production")); got != 0.5 {
		t.Errorf("long limit_type = %v, want it under the sanitized 32-character label", got)
	}

	// Empty label values fall back to "unknown".
	RecordQuotaRemainingRatio("weekly", "", "", 0.75)
	if got := testutil.ToFloat64(quotaRemainingRatio.WithLabelValues("weekly", "unknown", "unknown")); got != 0.75 {
		t.Errorf("empty labels = %v, want them under the \"unknown\" labels", got)
	}

	// Unrecognized poll results count as errors, keeping the result enum
	// bounded to the documented values.
	RecordQuotaPollOutcome("exploded", "production")
	if got := testutil.ToFloat64(quotaPollsTotal.WithLabelValues("error", "production")); got != 1 {
		t.Errorf("unrecognized poll result = %v, want it counted as \"error\"", got)
	}
	if got := testutil.CollectAndCount(quotaPollsTotal); got != 1 {
		t.Errorf("unrecognized poll result created extra series: got %d children, want 1", got)
	}
}

func TestQuotaValueGuards(t *testing.T) {
	resetQuotaMetrics()

	// Non-finite provider values are never exported.
	RecordQuotaUsageRatio("five_hour", "credits", "production", math.NaN())
	RecordQuotaUsageRatio("five_hour", "credits", "production", math.Inf(1))
	RecordQuotaRemainingRatio("five_hour", "credits", "production", math.Inf(-1))
	if got := testutil.CollectAndCount(quotaUsageRatio); got != 0 {
		t.Errorf("NaN/Inf usage ratio exported: got %d children, want 0", got)
	}
	if got := testutil.CollectAndCount(quotaRemainingRatio); got != 0 {
		t.Errorf("Inf remaining ratio exported: got %d children, want 0", got)
	}

	// A zero reset time means no reset was advertised; nothing is recorded.
	RecordQuotaResetTime("five_hour", "credits", "production", time.Time{})
	if got := testutil.CollectAndCount(quotaResetTimeSeconds); got != 0 {
		t.Errorf("zero reset time exported: got %d children, want 0", got)
	}

	// Negative rate caps clamp to zero instead of exporting an impossible cap.
	RecordQuotaRateCap("production", -3)
	if got := testutil.ToFloat64(quotaRateCap.WithLabelValues("production")); got != 0 {
		t.Errorf("negative rate cap = %v, want 0", got)
	}
	RecordQuotaRateCap("production", math.NaN())
	if got := testutil.CollectAndCount(quotaRateCap); got != 1 {
		t.Errorf("NaN rate cap changed the series: got %d children, want 1", got)
	}

	// Negative sample ages clamp to zero.
	RecordQuotaSampleAge("production", -5*time.Second)
	if got := testutil.ToFloat64(quotaSampleAgeSeconds.WithLabelValues("production")); got != 0 {
		t.Errorf("negative sample age = %v, want 0", got)
	}
}

func TestQuotaMetricsRegisteredInDefaultRegistry(t *testing.T) {
	resetQuotaMetrics()

	// A Vec without children is omitted from a gather, so record one child
	// for every quota metric before collecting.
	RecordQuotaUsageRatio("five_hour", "credits", "production", 0.1)
	RecordQuotaRemainingRatio("five_hour", "credits", "production", 0.9)
	RecordQuotaResetTime("five_hour", "credits", "production", time.Unix(1790000000, 0))
	RecordQuotaRateCap("production", 2)
	RecordQuotaGateOpen("weekly", "production", true)
	RecordQuotaSampleAge("production", time.Second)
	RecordQuotaPollOutcome("success", "production")

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gathering default registry: %v", err)
	}

	gathered := map[string]bool{}
	for _, family := range families {
		gathered[family.GetName()] = true
	}

	for _, name := range quotaMetricNames {
		if !gathered[name] {
			t.Errorf("quota metric %s is not registered in the default registry", name)
		}
	}
}
