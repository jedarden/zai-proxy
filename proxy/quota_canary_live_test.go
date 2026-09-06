package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"git.ardenone.com/jedarden/zai-proxy/proxy/config"
)

// This file is a credential-safe live canary for the observe-only quota
// telemetry path (docs/notes/zai-proxy-quota-observability.md). It is skipped
// unless both QUOTA_CANARY=true and ZAI_API_KEY are set, so `go test ./...`
// stays hermetic: no test here touches the network by default.
//
// What it does with a real credential:
//
//   1. Drives the production poller (quota_poller.go) against the configured
//      quota origin, through the same newQuotaPollerFromConfig wiring main
//      uses, in three phases — a slow cadence, a rapid burst, and a second
//      slow cadence.
//   2. Records, per sample, the /health view and the /metrics view of the
//      same retained snapshot, plus absolute credit amounts read straight
//      from the normalized snapshot at phase boundaries. ZCode's usage view
//      renders the same provider endpoint, so the recorded percentages and
//      reset stamps are the values to diff against it.
//   3. Compares the provider-reported usage drift across the burst window
//      with the drift across the cadence windows. If polling were metered,
//      the burst window would burn visibly faster than the cadence windows;
//      if it is unmetered, all three drift at the same rate.
//   4. Asserts the credential never reaches the report, the metrics output,
//      or the test log, and that the canary issued zero model requests.
//
// Everything written is derived and non-secret: percentages, reset
// timestamps, poll outcomes, and ages. Raw response bodies are never
// retained — the client discards them before normalization.

const (
	canaryFlagEnv   = "QUOTA_CANARY"
	canaryKeyEnv    = "ZAI_API_KEY"
	canaryOutEnv    = "QUOTA_CANARY_OUT"
	canaryVariant   = "canary"
	canaryMaxReport = 64 << 10
)

// Canary cadence knobs. The defaults keep the whole run under three minutes
// and under a hundred endpoint calls, which is what a monitor endpoint
// should be asked for by a one-off validation.
const (
	canaryCadenceSamples = 8
	canaryCadencePeriod  = 10 * time.Second
	canaryBurstPolls     = 40
	canaryPostSamples    = 4
)

// canarySample is one observation of the retained snapshot as both surfaces
// report it. Health and gauges are read back-to-back with no poll in
// between, so they must agree exactly.
type canarySample struct {
	At           string              `json:"at"`
	Phase        string              `json:"phase"`
	EndpointPoll int                 `json:"endpoint_polls_so_far"`
	Fresh        bool                `json:"fresh"`
	LastOutcome  string              `json:"last_outcome"`
	SampleAgeSec float64             `json:"sample_age_seconds"`
	PlanTier     string              `json:"plan_tier,omitempty"`
	Windows      []QuotaWindowHealth `json:"windows"`
	Gauges       map[string]float64  `json:"gauges"`
}

// canaryCredits is the absolute-amount view of one window, read from the
// normalized snapshot rather than from /health, which reports only ratios.
// These are the numbers ZCode's usage view shows directly.
type canaryCredits struct {
	Window        string  `json:"window"`
	LimitType     string  `json:"limit_type"`
	HasUsage      bool    `json:"has_usage"`
	Used          float64 `json:"used"`
	Limit         float64 `json:"limit"`
	Remaining     float64 `json:"remaining"`
	UsedPercent   float64 `json:"used_percent"`
	ResetAt       string  `json:"reset_at,omitempty"`
	ResetInSecond float64 `json:"reset_in_seconds,omitempty"`
}

// canaryReport is the whole normalized, secret-free artifact. It is written
// to QUOTA_CANARY_OUT (default the OS temp dir) and is the evidence an
// operator diffs against ZCode's usage view.
type canaryReport struct {
	StartedAt      string          `json:"started_at"`
	BaseURL        string          `json:"base_url"`
	Interval       string          `json:"interval"`
	StaleAfter     string          `json:"stale_after"`
	Variant        string          `json:"variant"`
	EndpointPolls  int             `json:"endpoint_polls_total"`
	ModelRequests  int             `json:"model_requests"`
	Phases         []string        `json:"phases"`
	Samples        []canarySample  `json:"samples"`
	CreditReadings []canaryReading `json:"credit_readings"`
	Analysis       canaryAnalysis  `json:"analysis"`
}

// canaryReading is one phase-boundary capture of absolute amounts.
type canaryReading struct {
	At      string          `json:"at"`
	Phase   string          `json:"phase"`
	Windows []canaryCredits `json:"windows"`
}

// canaryAnalysis is the derived comparison. Everything here is computed from
// the samples above, never from raw payloads.
type canaryAnalysis struct {
	HealthGaugeMaxDelta float64            `json:"health_gauge_max_delta"`
	ResetTimeDeltasSec  map[string]float64 `json:"reset_time_deltas_seconds"`
	BurnPerSecond       map[string]float64 `json:"usage_fraction_burn_per_second"`
	BurstWindowSeconds  float64            `json:"burst_window_seconds"`
	BurstDelta          float64            `json:"burst_usage_fraction_delta"`
	BurstExpectedDrift  float64            `json:"burst_expected_drift"`
	BurstExcess         float64            `json:"burst_excess"`
	ZeroBurn            string             `json:"zero_burn_verdict"`
}

// TestLiveQuotaObservationCanary is the entry point. See the file comment.
func TestLiveQuotaObservationCanary(t *testing.T) {
	if os.Getenv(canaryFlagEnv) != "true" {
		t.Skipf("observe-only live canary: set %s=true (with %s in the environment) to run it", canaryFlagEnv, canaryKeyEnv)
	}
	apiKey := os.Getenv(canaryKeyEnv)
	if apiKey == "" {
		t.Fatalf("%s=true requires %s in the environment; fetch it from OpenBao by pipe, never as a literal", canaryFlagEnv, canaryKeyEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	poller, err := newQuotaPollerFromConfig(apiKey, canaryVariant)
	if err != nil {
		t.Fatalf("newQuotaPollerFromConfig: %v", err)
	}
	// The configured interval governs production's cadence and is recorded so
	// the report states the wiring it exercised; the canary drives pollOnce
	// itself so one run can hold both a slow phase and a burst.

	report := &canaryReport{
		StartedAt:  time.Now().UTC().Format(time.RFC3339),
		BaseURL:    config.GetQuotaBaseURL(),
		Interval:   config.GetQuotaPollInterval().String(),
		StaleAfter: config.GetQuotaStaleAfter().String(),
		Variant:    canaryVariant,
		Phases:     []string{"baseline", "burst", "post_burst"},
	}

	count := func() {
		report.EndpointPolls++
	}

	// baseline: the poller's own cadence, one sample per period.
	report.Samples = append(report.Samples, canaryPhase(t, ctx, poller, report, count,
		"baseline", canaryCadenceSamples, canaryCadencePeriod)...)
	report.CreditReadings = append(report.CreditReadings, canaryReadCredits(ctx, poller, report, count, "baseline"))

	// burst: back-to-back polls with no dwell, the shape that would move the
	// counters if the monitor endpoint were metered.
	burstStart := time.Now()
	before := snapshotUsage(t, poller)
	for i := 0; i < canaryBurstPolls; i++ {
		poller.pollOnce(ctx)
		count()
	}
	burstSeconds := time.Since(burstStart).Seconds()
	after := snapshotUsage(t, poller)
	report.CreditReadings = append(report.CreditReadings, canaryReadCredits(ctx, poller, report, count, "burst"))

	// post_burst: a second cadence window to separate the burst's own drift
	// from whatever the account was already doing.
	report.Samples = append(report.Samples, canaryPhase(t, ctx, poller, report, count,
		"post_burst", canaryPostSamples, canaryCadencePeriod)...)
	report.CreditReadings = append(report.CreditReadings, canaryReadCredits(ctx, poller, report, count, "post_burst"))

	report.ModelRequests = countModelRequests(t)
	report.Analysis = analyseCanary(report, before, after, burstSeconds)

	path := writeCanaryReport(t, apiKey, report)
	t.Logf("canary report: %s", path)
	t.Logf("endpoint polls=%d, model requests=%d, health/gauge max delta=%.6g",
		report.EndpointPolls, report.ModelRequests, report.Analysis.HealthGaugeMaxDelta)
	t.Logf("zero-burn verdict: %s", report.Analysis.ZeroBurn)
	for window, rate := range report.Analysis.BurnPerSecond {
		t.Logf("burn rate %s: %.3g usage-fraction/second", window, rate)
	}
	t.Logf("burst: %.3fs, delta=%.6g, expected drift=%.6g, excess=%.6g",
		report.Analysis.BurstWindowSeconds, report.Analysis.BurstDelta,
		report.Analysis.BurstExpectedDrift, report.Analysis.BurstExcess)

	// The canary is observe-only by construction, and the assertion below is
	// what makes that observable rather than assumed: a canary that had sent
	// model traffic would show it here.
	if report.ModelRequests != 0 {
		t.Errorf("canary issued %d model requests; the observation path must send none", report.ModelRequests)
	}
	// /health and /metrics are two views of one retained snapshot, so they
	// can only disagree if a record path diverges from the other.
	if report.Analysis.HealthGaugeMaxDelta != 0 {
		t.Errorf("/health and /metrics disagree by %.6g on the same snapshot", report.Analysis.HealthGaugeMaxDelta)
	}
}

// canaryPhase runs one cadence phase and returns its samples. Each sample
// records the /health view and then the /metrics view with no poll in
// between, so the two are provably the same snapshot.
func canaryPhase(t *testing.T, ctx context.Context, poller *QuotaPoller, report *canaryReport,
	count func(), phase string, samples int, period time.Duration) []canarySample {
	t.Helper()

	out := make([]canarySample, 0, samples)
	for i := 0; i < samples; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				t.Fatalf("context expired during %s phase: %v", phase, ctx.Err())
			case <-time.After(period):
			}
		}
		poller.pollOnce(ctx)
		count()

		health := poller.HealthState()
		out = append(out, canarySample{
			At:           time.Now().UTC().Format(time.RFC3339Nano),
			Phase:        phase,
			EndpointPoll: report.EndpointPolls,
			Fresh:        health.Fresh,
			LastOutcome:  health.LastOutcome,
			SampleAgeSec: health.SampleAgeSeconds,
			PlanTier:     health.PlanTier,
			Windows:      health.Windows,
			Gauges:       quotaGauges(t),
		})
	}
	return out
}

// canaryReadCredits captures the absolute-amount view at a phase boundary.
// The extra fetch is deliberate and counted: /health reports ratios only,
// and ZCode's usage view shows the same absolute numbers the snapshot holds.
func canaryReadCredits(ctx context.Context, poller *QuotaPoller, report *canaryReport,
	count func(), phase string) canaryReading {
	// A failed boundary read is reported as an empty reading rather than
	// fatal: the retained sample still describes the phase that just ended.
	snapshot, _ := poller.fetcher.Fetch(ctx)
	count()
	now := time.Now().UTC()

	reading := canaryReading{At: now.Format(time.RFC3339Nano), Phase: phase}
	for _, w := range snapshot.Windows {
		window := canaryCredits{
			Window:      w.Window.String(),
			LimitType:   string(w.LimitType),
			HasUsage:    w.HasUsage,
			Used:        w.Used,
			Limit:       w.Limit,
			Remaining:   w.Remaining,
			UsedPercent: w.UsedFraction * 100,
		}
		if !w.ResetTime.IsZero() {
			window.ResetAt = w.ResetTime.UTC().Format(time.RFC3339)
			window.ResetInSecond = time.Until(w.ResetTime).Seconds()
		}
		reading.Windows = append(reading.Windows, window)
	}
	return reading
}

// snapshotUsage reads the retained per-window usage ratios straight out of
// the poller's health view, for the burst delta arithmetic.
func snapshotUsage(t *testing.T, poller *QuotaPoller) map[string]float64 {
	t.Helper()

	health := poller.HealthState()
	out := make(map[string]float64, len(health.Windows))
	for _, w := range health.Windows {
		out[w.Window] = w.UsedFraction
	}
	return out
}

// quotaGathers gathers the default registry and returns the quota gauges as
// the /metrics endpoint would render them, keyed the way the documentation
// names them. Only quota families are returned: this is the telemetry path
// under test, not the whole registry.
func quotaGauges(t *testing.T) map[string]float64 {
	t.Helper()

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gathering metrics: %v", err)
	}
	out := make(map[string]float64)
	for _, mf := range families {
		name := mf.GetName()
		if !strings.HasPrefix(name, "zai_proxy_quota_") {
			continue
		}
		for _, m := range mf.GetMetric() {
			out[renderMetricName(name, m)] = metricValue(m)
		}
	}
	return out
}

// renderMetricName renders one collected metric in Prometheus text form,
// with labels in sorted order so report keys are stable.
func renderMetricName(name string, m *dto.Metric) string {
	labels := make([]string, 0, len(m.GetLabel()))
	for _, l := range m.GetLabel() {
		labels = append(labels, l.GetName()+"=\""+l.GetValue()+"\"")
	}
	sort.Strings(labels)
	if len(labels) == 0 {
		return name
	}
	return name + "{" + strings.Join(labels, ",") + "}"
}

func metricValue(m *dto.Metric) float64 {
	switch {
	case m.GetGauge() != nil:
		return m.GetGauge().GetValue()
	case m.GetCounter() != nil:
		return m.GetCounter().GetValue()
	case m.GetUntyped() != nil:
		return m.GetUntyped().GetValue()
	default:
		return 0
	}
}

// countModelRequests reports how many model requests this process has
// counted. The canary starts no handler and sends no inference, so a
// non-zero value means the observation path leaked into the data path.
func countModelRequests(t *testing.T) int {
	t.Helper()

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gathering metrics: %v", err)
	}
	total := 0.0
	for _, mf := range families {
		if mf.GetName() != "zai_proxy_requests_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			total += m.GetCounter().GetValue()
		}
	}
	return int(total)
}

// analyseCanary derives the comparison an operator acts on: whether the two
// surfaces agree, whether reset stamps held still, and whether the burst
// burned faster than the cadence windows did.
func analyseCanary(report *canaryReport, before, after map[string]float64, burstSeconds float64) canaryAnalysis {
	analysis := canaryAnalysis{
		ResetTimeDeltasSec: map[string]float64{},
		BurnPerSecond:      map[string]float64{},
		BurstWindowSeconds: burstSeconds,
	}

	// /health and /metrics must agree on every sample they were taken with.
	for _, s := range report.Samples {
		for _, w := range s.Windows {
			for key, value := range s.Gauges {
				if !strings.Contains(key, "zai_proxy_quota_usage_ratio") ||
					!strings.Contains(key, `window="`+w.Window+`"`) {
					continue
				}
				if d := canaryAbs(value - w.UsedFraction); d > analysis.HealthGaugeMaxDelta {
					analysis.HealthGaugeMaxDelta = d
				}
			}
		}
	}

	// Reset stamps across the whole run. The provider's countdown should move
	// only by the wall clock between readings, never by polling, so the
	// recorded delta is what an operator compares against the elapsed time.
	for _, window := range report.CreditReadings {
		for _, w := range window.Windows {
			if w.ResetAt == "" {
				continue
			}
			analysis.ResetTimeDeltasSec[w.Window] = w.ResetInSecond
		}
	}

	// Burn per second on each cadence phase, from the phase's first and last
	// sample. A five-hour window that resets mid-run shows as a negative rate
	// and is reported rather than hidden.
	for _, phase := range []string{"baseline", "post_burst"} {
		first, last, ok := phaseSpan(report.Samples, phase)
		if !ok {
			continue
		}
		for _, w := range first.Windows {
			var end *QuotaWindowHealth
			for i := range last.Windows {
				if last.Windows[i].Window == w.Window {
					end = &last.Windows[i]
				}
			}
			if end == nil {
				continue
			}
			seconds := phaseSeconds(first.At, last.At)
			if seconds <= 0 {
				continue
			}
			analysis.BurnPerSecond[phase+"/"+w.Window] = (end.UsedFraction - w.UsedFraction) / seconds
		}
	}

	analysis.BurstDelta = usageDelta(after, before)
	rate := cadenceRate(analysis.BurnPerSecond)
	analysis.BurstExpectedDrift = rate * burstSeconds
	analysis.BurstExcess = analysis.BurstDelta - analysis.BurstExpectedDrift
	analysis.ZeroBurn = zeroBurnVerdict(analysis)
	return analysis
}

// phaseSpan returns the first and last sample of one phase.
func phaseSpan(samples []canarySample, phase string) (canarySample, canarySample, bool) {
	var first, last canarySample
	found := false
	for _, s := range samples {
		if s.Phase != phase {
			continue
		}
		if !found {
			first, found = s, true
		}
		last = s
	}
	return first, last, found
}

func phaseSeconds(startRFC3339, endRFC3339 string) float64 {
	start, err1 := time.Parse(time.RFC3339Nano, startRFC3339)
	end, err2 := time.Parse(time.RFC3339Nano, endRFC3339)
	if err1 != nil || err2 != nil {
		return 0
	}
	return end.Sub(start).Seconds()
}

func usageDelta(after, before map[string]float64) float64 {
	delta := 0.0
	for window, start := range before {
		if end, ok := after[window]; ok {
			delta += end - start
		}
	}
	return delta
}

// cadenceRate is the largest burn rate any cadence phase observed, i.e. the
// drift the burst window should have shown had polling contributed nothing.
// Taking the maximum makes the verdict conservative: a burst is only counted
// as excess when it outburns the fastest cadence window.
func cadenceRate(burn map[string]float64) float64 {
	rate := 0.0
	for _, v := range burn {
		if v > rate {
			rate = v
		}
	}
	return rate
}

// zeroBurnVerdict states the conclusion in words so the report cannot be
// misread as a measurement it is not. Provider percentages move in coarse
// steps, so an excess smaller than one step is unresolvable rather than
// clean, and the verdict says so.
func zeroBurnVerdict(a canaryAnalysis) string {
	const unresolved = "unresolved: burst excess is below provider reporting granularity"
	switch {
	case a.BurstExcess <= 0:
		return "confirmed: burst window burned no faster than the cadence windows"
	case a.BurstExcess < 0.001:
		return unresolved
	default:
		return fmt.Sprintf("falsified: burst window burned %.6g more usage fraction than the cadence rate predicts", a.BurstExcess)
	}
}

// canaryAbs is named for this file: the package already has an abs helper
// in another test file, and test files share one namespace.
func canaryAbs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// writeCanaryReport persists the normalized evidence and asserts it carries
// nothing secret. The credential check runs against the file's bytes, so a
// future edit that adds a field holding the key fails here rather than
// leaking from a report an operator copies elsewhere.
func writeCanaryReport(t *testing.T, apiKey string, report *canaryReport) string {
	t.Helper()

	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("encoding report: %v", err)
	}
	if len(body) > canaryMaxReport {
		t.Fatalf("report grew to %d bytes; the cap keeps a canary artifact small", len(body))
	}
	if strings.Contains(string(body), apiKey) {
		t.Fatal("the canary report contains the credential; it must never be written anywhere")
	}

	dir := os.Getenv(canaryOutEnv)
	if dir == "" {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, "zai-quota-canary-"+strconv.FormatInt(time.Now().Unix(), 10)+".json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("writing report: %v", err)
	}
	return path
}
