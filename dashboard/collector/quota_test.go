package collector

import (
	"testing"
	"time"
)

func quotaSeries(metric, window, limitType string, value float64) MetricValue {
	return MetricValue{
		Value:  value,
		Labels: map[string]string{"window": window, "limit_type": limitType},
	}
}

func fullQuotaScrape() map[string][]MetricValue {
	return map[string][]MetricValue{
		quotaUsageMetric: {
			quotaSeries(quotaUsageMetric, "five_hour", "CREDIT_LIMIT", 0.42),
			quotaSeries(quotaUsageMetric, "weekly", "CREDIT_LIMIT", 0.12),
		},
		quotaRemainMetric: {
			quotaSeries(quotaRemainMetric, "five_hour", "CREDIT_LIMIT", 0.58),
			quotaSeries(quotaRemainMetric, "weekly", "CREDIT_LIMIT", 0.88),
		},
		quotaResetMetric: {
			quotaSeries(quotaResetMetric, "five_hour", "CREDIT_LIMIT", 1900000000),
		},
		quotaAgeMetric: {
			{Value: 30, Labels: map[string]string{"variant": "production"}},
		},
	}
}

func TestBuildQuotaState_NoSeriesYieldsNil(t *testing.T) {
	if state := buildQuotaState(map[string][]MetricValue{}); state != nil {
		t.Fatalf("expected nil quota state with no series, got %+v", state)
	}

	// A window label the dashboard does not display must not count as quota
	// telemetry on its own: the proxy collapses unknown windows there.
	unknownOnly := map[string][]MetricValue{
		quotaUsageMetric: {quotaSeries(quotaUsageMetric, "unknown", "CREDIT_LIMIT", 0.5)},
	}
	if state := buildQuotaState(unknownOnly); state != nil {
		t.Fatalf("unknown-window series should not produce quota state, got %+v", state)
	}
}

func TestBuildQuotaState_ReadsBothWindows(t *testing.T) {
	state := buildQuotaState(fullQuotaScrape())
	if state == nil {
		t.Fatal("expected quota state from a full scrape")
	}

	if state.FiveHour == nil || state.Weekly == nil {
		t.Fatalf("both windows should be present, got %+v", state)
	}
	if state.FiveHour.UsageRatio == nil || *state.FiveHour.UsageRatio != 0.42 {
		t.Errorf("five-hour usage = %v, want 0.42", state.FiveHour.UsageRatio)
	}
	if state.FiveHour.RemainingRatio == nil || *state.FiveHour.RemainingRatio != 0.58 {
		t.Errorf("five-hour remaining = %v, want 0.58", state.FiveHour.RemainingRatio)
	}
	if state.FiveHour.ResetTimeUnix == nil || *state.FiveHour.ResetTimeUnix != 1900000000 {
		t.Errorf("five-hour reset = %v, want 1900000000", state.FiveHour.ResetTimeUnix)
	}
	if state.Weekly.UsageRatio == nil || *state.Weekly.UsageRatio != 0.12 {
		t.Errorf("weekly usage = %v, want 0.12", state.Weekly.UsageRatio)
	}
	// The provider advertised no weekly reset: the field must stay nil, not
	// inherit the five-hour stamp.
	if state.Weekly.ResetTimeUnix != nil {
		t.Errorf("weekly reset = %v, want nil", state.Weekly.ResetTimeUnix)
	}

	if state.SampleAgeSeconds == nil || *state.SampleAgeSeconds != 30 {
		t.Errorf("sample age = %v, want 30", state.SampleAgeSeconds)
	}

	// Gate and rate-cap series absent: observe-only, with gate state unknown
	// rather than reported closed.
	if state.Enforcement {
		t.Error("enforcement should be false without gate or rate-cap series")
	}
	if state.GateOpen != nil {
		t.Errorf("gate open = %v, want nil", state.GateOpen)
	}
}

func TestBuildQuotaState_MissingFieldsStayNil(t *testing.T) {
	// Only usage is advertised for the weekly window: remaining and reset must
	// remain nil so the dashboard can distinguish missing from zero.
	scrape := map[string][]MetricValue{
		quotaUsageMetric: {quotaSeries(quotaUsageMetric, "weekly", "CREDIT_LIMIT", 0)},
	}
	state := buildQuotaState(scrape)
	if state == nil || state.Weekly == nil {
		t.Fatalf("expected weekly window state, got %+v", state)
	}
	if state.Weekly.UsageRatio == nil || *state.Weekly.UsageRatio != 0 {
		t.Errorf("weekly usage = %v, want explicit zero", state.Weekly.UsageRatio)
	}
	if state.Weekly.RemainingRatio != nil {
		t.Errorf("weekly remaining = %v, want nil", state.Weekly.RemainingRatio)
	}
	if state.Weekly.ResetTimeUnix != nil {
		t.Errorf("weekly reset = %v, want nil", state.Weekly.ResetTimeUnix)
	}

	// The proxy never records a non-positive reset stamp; such a series must
	// read as absent rather than as an epoch countdown.
	scrape[quotaResetMetric] = []MetricValue{
		quotaSeries(quotaResetMetric, "weekly", "CREDIT_LIMIT", 0),
	}
	state = buildQuotaState(scrape)
	if state.Weekly.ResetTimeUnix != nil {
		t.Errorf("non-positive reset = %v, want nil", state.Weekly.ResetTimeUnix)
	}
}

func TestBuildQuotaState_SelectsPreferredLimitType(t *testing.T) {
	scrape := map[string][]MetricValue{
		quotaUsageMetric: {
			quotaSeries(quotaUsageMetric, "five_hour", "CREDIT_LIMIT", 0.42),
			quotaSeries(quotaUsageMetric, "five_hour", "TOKENS_LIMIT", 0.90),
		},
		quotaRemainMetric: {
			quotaSeries(quotaRemainMetric, "five_hour", "CREDIT_LIMIT", 0.58),
			quotaSeries(quotaRemainMetric, "five_hour", "TOKENS_LIMIT", 0.10),
		},
	}
	state := buildQuotaState(scrape)
	if state == nil || state.FiveHour == nil {
		t.Fatalf("expected five-hour state, got %+v", state)
	}
	if state.FiveHour.LimitType != "CREDIT_LIMIT" {
		t.Fatalf("limit type = %q, want CREDIT_LIMIT", state.FiveHour.LimitType)
	}
	if *state.FiveHour.UsageRatio != 0.42 || *state.FiveHour.RemainingRatio != 0.58 {
		t.Errorf("fields must come from the selected schema, got usage %v remaining %v",
			*state.FiveHour.UsageRatio, *state.FiveHour.RemainingRatio)
	}

	// Legacy schema alone must still be read.
	legacy := map[string][]MetricValue{
		quotaUsageMetric: {quotaSeries(quotaUsageMetric, "five_hour", "TOKENS_LIMIT", 0.90)},
	}
	state = buildQuotaState(legacy)
	if state == nil || state.FiveHour == nil || state.FiveHour.LimitType != "TOKENS_LIMIT" {
		t.Fatalf("legacy schema not selected, got %+v", state)
	}
	if state.FiveHour.UsageRatio == nil || *state.FiveHour.UsageRatio != 0.90 {
		t.Errorf("legacy usage = %v, want 0.90", state.FiveHour.UsageRatio)
	}

	// Unknown schemas fall back to the lexicographically smallest label so the
	// choice stays stable across scrapes.
	future := map[string][]MetricValue{
		quotaUsageMetric: {
			quotaSeries(quotaUsageMetric, "five_hour", "BETA_LIMIT", 1),
			quotaSeries(quotaUsageMetric, "five_hour", "ALPHA_LIMIT", 0.5),
		},
	}
	state = buildQuotaState(future)
	if state == nil || state.FiveHour == nil || state.FiveHour.LimitType != "ALPHA_LIMIT" {
		t.Fatalf("fallback schema not stable, got %+v", state)
	}
}

func TestBuildQuotaState_SampleAgeIsOldestSeries(t *testing.T) {
	scrape := map[string][]MetricValue{
		quotaAgeMetric: {
			{Value: 12, Labels: map[string]string{"variant": "production"}},
			{Value: 95, Labels: map[string]string{"variant": "canary"}},
		},
	}
	state := buildQuotaState(scrape)
	if state == nil || state.SampleAgeSeconds == nil || *state.SampleAgeSeconds != 95 {
		t.Fatalf("sample age = %+v, want the oldest series (95)", state)
	}
}

func TestBuildQuotaState_EnforcementSignals(t *testing.T) {
	// A closed gate still proves enforcement is wired.
	scrape := map[string][]MetricValue{
		quotaGateMetric: {
			{Value: 0, Labels: map[string]string{"window": "five_hour", "variant": "production"}},
		},
	}
	state := buildQuotaState(scrape)
	if state == nil || !state.Enforcement {
		t.Fatalf("gate presence must imply enforcement, got %+v", state)
	}
	if state.GateOpen == nil || *state.GateOpen {
		t.Errorf("gate open = %v, want explicit false", state.GateOpen)
	}

	// Any open gate flips the aggregate.
	scrape[quotaGateMetric] = append(scrape[quotaGateMetric],
		MetricValue{Value: 1, Labels: map[string]string{"window": "weekly", "variant": "production"}})
	state = buildQuotaState(scrape)
	if state.GateOpen == nil || !*state.GateOpen {
		t.Errorf("gate open = %v, want true with one open gate", state.GateOpen)
	}

	// A rate cap without a gate also proves enforcement.
	capOnly := map[string][]MetricValue{
		quotaRateCapMetric: {
			{Value: 0.5, Labels: map[string]string{"variant": "production"}},
		},
	}
	state = buildQuotaState(capOnly)
	if state == nil || !state.Enforcement {
		t.Fatalf("rate-cap presence must imply enforcement, got %+v", state)
	}
	if state.GateOpen != nil {
		t.Errorf("gate open = %v, want nil without gate series", state.GateOpen)
	}
}

func TestBuildSnapshotAttachesQuotaState(t *testing.T) {
	collector := NewCollector(Config{})
	now := time.Now()

	snapshot := collector.buildSnapshot(fullQuotaScrape(), nil, now, time.Time{}, "production")
	if snapshot.Quota == nil {
		t.Fatal("snapshot should carry the quota block parsed from the scrape")
	}
	if snapshot.Quota.FiveHour == nil || snapshot.Quota.FiveHour.UsageRatio == nil ||
		*snapshot.Quota.FiveHour.UsageRatio != 0.42 {
		t.Errorf("unexpected five-hour block: %+v", snapshot.Quota.FiveHour)
	}

	// A scrape without quota series leaves the block nil so missing telemetry
	// stays explicit through storage and the API.
	empty := collector.buildSnapshot(map[string][]MetricValue{}, nil, now, time.Time{}, "production")
	if empty.Quota != nil {
		t.Errorf("quota block = %+v, want nil for a scrape without quota series", empty.Quota)
	}
}
