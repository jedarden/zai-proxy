package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"git.ardenone.com/jedarden/zai-proxy/proxy/quota"
)

// TestQuotaObserveOnlyCanary is the credential-safe canary for the
// observe-only quota integration (docs/notes/zai-proxy-quota-observability.md).
// It runs the production wiring -- env-configured poller construction, the
// poll loop, /health, /metrics, and the full inference request path -- against
// synthetic endpoints only: the quota endpoint is a local fixture serving the
// committed payloads in testdata/quota_canary/, the inference upstream is a
// local mock, and the credential is a fixed fake string. Nothing here contacts
// Z.AI and nothing here is a real account.
//
// The canary pins four properties end to end, in order:
//
//  1. Telemetry: the normalized five-hour and weekly windows reach /health
//     and /metrics with the percentages and reset stamps the fixture
//     (standing in for the provider's own usage view) reports, and a change
//     in usage flows through with an advanced last-success timestamp.
//  2. Failure isolation: endpoint errors, malformed payloads, and a refused
//     connection leave the last-known-good sample in place, count bounded
//     outcomes, age the sample into staleness exactly once, and never touch
//     the inference path.
//  3. No enforcement: the admission rate in /health is identical while quota
//     is healthy, failing, stale, and recovered, and the enforcement gauges
//     (quota_rate_cap, quota_gate_open) never move.
//  4. Zero burn: every quota request is a GET on the fixed monitor path, the
//     inference upstream receives exactly the requests the canary itself
//     sent, and the synthetic credential appears on no observable surface.
//
// Run with: go test ./proxy -run TestQuotaObserveOnlyCanary -v
// The "canary:" log lines are the evidence record the operations doc cites.
func TestQuotaObserveOnlyCanary(t *testing.T) {
	ConfigureTestEnv(t)

	// The canary credential is a fixed synthetic string; the secret-safety
	// phase asserts it never reaches /health, /metrics, or the captured logs.
	const canaryAPIKey = "canary-synthetic-key-not-a-credential-0123456789abcdef"
	const canaryVariant = "canary"

	// Fast cadences so every phase completes in milliseconds, with a
	// stale-after short enough to observe a full staleness transition inside
	// the outage phase but long enough that healthy phases never go stale.
	t.Setenv("QUOTA_POLL_INTERVAL", "100ms")
	t.Setenv("QUOTA_POLL_TIMEOUT", "800ms")
	t.Setenv("QUOTA_STALE_AFTER", "900ms")

	// Capture the process log so the credential-safety phase can scan every
	// line the poller, the quota client, and the proxy handler produced.
	var capturedLogs syncBuffer
	log.SetOutput(&capturedLogs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	// ---- Synthetic endpoints ------------------------------------------------
	fixture := newCanaryQuotaFixture(t, canaryAPIKey)
	t.Setenv("ZAI_QUOTA_BASE_URL", "http://"+fixture.addr)
	upstream := newCanaryInferenceUpstream(t, canaryAPIKey)
	defer upstream.Close()

	// ---- Production wiring --------------------------------------------------
	t.Setenv("QUOTA_POLL_ENABLED", "true")
	poller, err := newQuotaPollerFromConfig(canaryAPIKey, canaryVariant)
	if err != nil {
		t.Fatalf("env-configured poller construction failed: %v", err)
	}
	pollCtx, cancelPolling := context.WithCancel(context.Background())
	defer cancelPolling()
	go func() { _ = poller.Run(pollCtx) }()

	proxyHandler := NewProxyHandler(
		canaryAPIKey,
		upstream.URL,
		1, // maxRetries
		100,
		canaryVariant,
		nil, // token counting disabled
		"glm-4",
		1000, 1000, 1000, // fixed admission rate: any movement is enforcement
	)
	proxyHandler.retrySleep = func(time.Duration) {}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", newHealthHandler(proxyHandler.rateLimiter, poller))
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/", proxyHandler)
	surface := httptest.NewServer(mux)
	defer surface.Close()

	// The enforcement gauges are pre-created at zero so the verdict can pin
	// "never moved" for this variant; that the observe-only poller creates no
	// series at all is pinned in isolation by TestQuotaPollerPublishesFreshSample.
	quotaRateCap.WithLabelValues(canaryVariant)
	quotaGateOpen.WithLabelValues("five_hour", canaryVariant)
	quotaGateOpen.WithLabelValues("weekly", canaryVariant)

	// ---- Observability helpers ---------------------------------------------
	getHealth := func() canaryHealth {
		t.Helper()
		resp, err := http.Get(surface.URL + "/health")
		if err != nil {
			t.Fatalf("GET /health failed: %v", err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("reading /health failed: %v", err)
		}
		var health canaryHealth
		if err := json.Unmarshal(raw, &health); err != nil {
			t.Fatalf("decoding /health failed: %v\npayload: %s", err, raw)
		}
		return health
	}

	getMetrics := func() string {
		t.Helper()
		resp, err := http.Get(surface.URL + "/metrics")
		if err != nil {
			t.Fatalf("GET /metrics failed: %v", err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("reading /metrics failed: %v", err)
		}
		return string(raw)
	}

	pollCounter := func(result string) float64 {
		t.Helper()
		return testutil.ToFloat64(quotaPollsTotal.WithLabelValues(result, canaryVariant))
	}

	waitFor := func(what string, timeout time.Duration, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			if cond() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("canary: timed out after %s waiting for %s", timeout, what)
	}

	logQuotaMetricLines := func(stage string) {
		for _, line := range strings.Split(getMetrics(), "\n") {
			if strings.HasPrefix(line, "zai_proxy_quota_") && strings.Contains(line, `variant="canary"`) {
				canaryLogf(t, "%s metrics | %s", stage, line)
			}
		}
	}

	sendInference := func(stage string) {
		t.Helper()
		resp := ExecuteMessagesRequest(t, proxyHandler, createNonStreamingRequestBody())
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("%s: reading inference response failed: %v", stage, err)
		}
		if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "{}" {
			t.Fatalf("%s: inference through the proxy = %d %q, want 200 {}", stage, resp.StatusCode, body)
		}
	}

	assertAdmissionRate := func(stage string, want float64) {
		t.Helper()
		if got := getHealth().RateLimit.CurrentRate; math.Abs(got-want) > 1e-9 {
			t.Fatalf("%s: admission rate moved to %v during observe-only quota activity, want %v", stage, got, want)
		}
	}

	// ---- Phase 1: healthy telemetry ----------------------------------------
	const admissionRate = 1000.0
	fixture.serveFile(readCanaryFixture(t, "healthy.json"))
	waitFor("the first healthy sample on /health", 5*time.Second, func() bool {
		h := getHealth()
		return h.Quota.Fresh &&
			h.Quota.LastOutcome == quotaPollOutcomeSuccess &&
			len(h.Quota.Windows) == 2 &&
			math.Abs(h.Quota.Windows[0].UsedFraction-0.37) < 1e-9 &&
			math.Abs(h.Quota.Windows[1].UsedFraction-0.52) < 1e-9
	})
	healthy := getHealth()
	// The fixture stands in for the provider's own usage view: its
	// provider-reported percentages are 37 and 52, and the normalized
	// telemetry must agree with that view exactly (the real-account version
	// of this comparison is the operator procedure in the operations doc).
	fiveHourReset := time.UnixMilli(1900000000000).UTC().Format(time.RFC3339)
	weeklyReset := time.UnixMilli(1950000000000).UTC().Format(time.RFC3339)
	if healthy.Quota.Windows[0].Window != "five_hour" || healthy.Quota.Windows[0].ResetAt != fiveHourReset {
		t.Fatalf("five-hour window = %+v, want five_hour reset %s", healthy.Quota.Windows[0], fiveHourReset)
	}
	if healthy.Quota.Windows[1].Window != "weekly" || healthy.Quota.Windows[1].ResetAt != weeklyReset {
		t.Fatalf("weekly window = %+v, want weekly reset %s", healthy.Quota.Windows[1], weeklyReset)
	}
	if healthy.Quota.PlanTier != "canary" || !healthy.Quota.Enabled {
		t.Fatalf("quota health = plan_tier %q enabled %v, want canary/true", healthy.Quota.PlanTier, healthy.Quota.Enabled)
	}
	assertAdmissionRate("healthy phase", admissionRate)
	sendInference("healthy phase")
	canaryLogf(t, "healthy | /health=%s", mustJSON(t, healthy))
	logQuotaMetricLines("healthy")

	successAfterHealthy := pollCounter(quotaPollOutcomeSuccess)
	canaryLogf(t, "healthy | success_polls=%.0f total_polls=%d all_get_on_monitor_path=%v",
		successAfterHealthy, fixture.pollCount(), fixture.allPollsAreMonitorGets())

	// ---- Phase 2: percentage/reset delta ------------------------------------
	// /health renders last_success_at at whole-second precision (RFC3339) and
	// the canary polls every 100ms, so the next success usually lands inside
	// the same wall-clock second as the healthy sample and would render an
	// identical stamp no matter what the fixture serves. Wait past the
	// captured stamp's second boundary first: every success after that point
	// is stamped a whole second or more later, so "advanced" below becomes a
	// deterministic comparison rather than a race on the clock.
	if healthyAt, err := time.Parse(time.RFC3339, healthy.Quota.LastSuccessAt); err == nil {
		if wait := time.Until(healthyAt.Add(time.Second)); wait > 0 {
			time.Sleep(wait)
		}
	}
	fixture.serveFile(readCanaryFixture(t, "usage_increased.json"))
	waitFor("the raised five-hour usage to reach /health", 5*time.Second, func() bool {
		h := getHealth()
		return h.Quota.Fresh && len(h.Quota.Windows) == 2 &&
			math.Abs(h.Quota.Windows[0].UsedFraction-0.41) < 1e-9
	})
	raised := getHealth()
	if math.Abs(raised.Quota.Windows[1].UsedFraction-0.52) > 1e-9 {
		t.Fatalf("weekly window moved to %v, want unchanged 0.52", raised.Quota.Windows[1].UsedFraction)
	}
	canaryLogf(t, "delta | five_hour used_pct 37.0->41.0 (+4.0pp) weekly 52.0->52.0 (+0.0pp) "+
		"reset_delta five_hour=0s weekly=0s last_success_at %s->%s poll_interval=%s",
		healthy.Quota.LastSuccessAt, raised.Quota.LastSuccessAt,
		raised.Quota.Interval)
	if !canaryTimeAdvanced(healthy.Quota.LastSuccessAt, raised.Quota.LastSuccessAt) {
		t.Fatalf("last_success_at did not advance across the usage change: %s -> %s",
			healthy.Quota.LastSuccessAt, raised.Quota.LastSuccessAt)
	}
	assertAdmissionRate("delta phase", admissionRate)

	// ---- Phase 3: failure isolation -----------------------------------------
	successBeforeOutage := pollCounter(quotaPollOutcomeSuccess)
	malformedBefore := pollCounter(quotaPollOutcomeMalformed)
	staleBefore := pollCounter(quotaPollOutcomeStale)

	fixture.serveStatus(http.StatusInternalServerError)
	waitFor("the 500 outcome on /health", 5*time.Second, func() bool {
		return getHealth().Quota.LastOutcome == quotaPollOutcomeError
	})
	failing := getHealth()
	if !failing.Quota.Fresh || len(failing.Quota.Windows) != 2 ||
		math.Abs(failing.Quota.Windows[0].UsedFraction-0.41) > 1e-9 {
		t.Fatalf("a failing poll must keep the last-known-good sample, got %+v", failing.Quota)
	}
	sendInference("upstream-500 phase")
	assertAdmissionRate("upstream-500 phase", admissionRate)
	canaryLogf(t, "failure_500 | outcome=error last_known_good five_hour=41.0 fresh=%v inference=200", failing.Quota.Fresh)

	fixture.serveMalformed()
	waitFor("the malformed outcome on /health", 5*time.Second, func() bool {
		return getHealth().Quota.LastOutcome == quotaPollOutcomeMalformed
	})
	sendInference("malformed-payload phase")
	assertAdmissionRate("malformed-payload phase", admissionRate)
	canaryLogf(t, "failure_malformed | outcome=malformed last_known_good kept inference=200")

	fixture.refuseConnections()
	waitFor("the refused-connection outcome on /health", 5*time.Second, func() bool {
		return getHealth().Quota.LastOutcome == quotaPollOutcomeError
	})
	waitFor("the sample to age into staleness", 6*time.Second, func() bool {
		return !getHealth().Quota.Fresh
	})
	stale := getHealth()
	if len(stale.Quota.Windows) != 2 || math.Abs(stale.Quota.Windows[1].UsedFraction-0.52) > 1e-9 {
		t.Fatalf("a stale sample must still be exported last-known-good, got %+v", stale.Quota)
	}
	sendInference("stale-sample phase")
	assertAdmissionRate("stale-sample phase", admissionRate)
	// The stale outcome is recorded by the poll loop, not by the /health read
	// above, so give the next tick (interval 100ms) a moment to land before
	// pinning the count. staleSeen latches the transition, so once it is
	// counted the count cannot grow again before a success resets it.
	waitFor("the stale transition to be counted once", 3*time.Second, func() bool {
		return pollCounter(quotaPollOutcomeStale) > staleBefore
	})
	staleAfter := pollCounter(quotaPollOutcomeStale)
	if staleAfter-staleBefore != 1 {
		t.Fatalf("stale outcome counted %v times across one outage, want exactly 1", staleAfter-staleBefore)
	}
	canaryLogf(t, "failure_refused | outcome=error fresh=false stale_transitions=1 last_known_good weekly=52.0 inference=200")

	if got := pollCounter(quotaPollOutcomeSuccess); got != successBeforeOutage {
		t.Fatalf("success polls advanced from %v to %v during the outage, want unchanged",
			successBeforeOutage, got)
	}
	if got := pollCounter(quotaPollOutcomeMalformed) - malformedBefore; got < 1 {
		t.Fatalf("malformed outcome counted %v times, want at least 1", got)
	}
	if got := pollCounter(quotaPollOutcomeError); got < 2 {
		t.Fatalf("error outcome counted %v times across a 500 and a refused connection, want at least 2", got)
	}

	// ---- Phase 4: recovery ---------------------------------------------------
	fixture.acceptConnectionsAgain()
	fixture.serveFile(readCanaryFixture(t, "healthy.json"))
	waitFor("recovery on /health", 6*time.Second, func() bool {
		h := getHealth()
		return h.Quota.Fresh && h.Quota.LastOutcome == quotaPollOutcomeSuccess &&
			math.Abs(h.Quota.Windows[0].UsedFraction-0.37) < 1e-9
	})
	recovered := getHealth()
	sendInference("recovery phase")
	assertAdmissionRate("recovery phase", admissionRate)
	if got := pollCounter(quotaPollOutcomeStale); got != staleAfter {
		t.Fatalf("stale transitions advanced from %v to %v across recovery, want unchanged", staleAfter, got)
	}
	if got := pollCounter(quotaPollOutcomeSuccess); got <= successBeforeOutage {
		t.Fatalf("success polls = %v after recovery, want growth beyond %v", got, successBeforeOutage)
	}
	canaryLogf(t, "recovery | outcome=success five_hour=37.0 last_success_at=%s inference=200 stale_transitions_still=1",
		recovered.Quota.LastSuccessAt)

	// ---- Phase 5: verdict ----------------------------------------------------
	if !fixture.allPollsAreMonitorGets() {
		t.Fatalf("quota fixture received non-monitor traffic: %+v", fixture.pollRecords())
	}
	if upstream.requestCount() != 5 {
		t.Fatalf("inference upstream saw %d requests, want exactly the 5 the canary sent", upstream.requestCount())
	}
	for name, value := range map[string]float64{
		"quota_rate_cap":             testutil.ToFloat64(quotaRateCap.WithLabelValues(canaryVariant)),
		"quota_gate_open[five_hour]": testutil.ToFloat64(quotaGateOpen.WithLabelValues("five_hour", canaryVariant)),
		"quota_gate_open[weekly]":    testutil.ToFloat64(quotaGateOpen.WithLabelValues("weekly", canaryVariant)),
	} {
		if value != 0 {
			t.Fatalf("enforcement gauge %s = %v after the full canary, want 0 (observe-only)", name, value)
		}
	}

	secretHits := strings.Count(getMetrics(), canaryAPIKey) +
		strings.Count(mustJSON(t, getHealth()), canaryAPIKey) +
		strings.Count(capturedLogs.String(), canaryAPIKey)
	if secretHits != 0 {
		t.Fatalf("the synthetic credential appeared on %d observable surfaces, want 0", secretHits)
	}

	canaryLogf(t, "verdict | zero_burn=confirmed quota_polls=%d all_get_on_monitor_path=true "+
		"inference_upstream_calls=5/5 unexpected_upstream_calls=0 enforcement_metrics_written=false "+
		"admission_rate_constant=%.1f credential_surfaces=clean",
		fixture.pollCount(), admissionRate)
	canaryLogf(t, "done | captured_log_bytes=%d (scanned, credential-free)", capturedLogs.Len())
}

// TestQuotaCanaryProxyViewFixtures pins the committed proxy-quota-view
// fixtures (proxy_quota_view_*.json) to what the production poller actually
// renders from the matching provider fixture. proxy/scripts/quota_canary.py
// samples those fixtures as the proxy-side surface of its hermetic run, so a
// rendering change that outdates them must fail here, in the proxy, and not
// surface later as a canary that compares a stale view against a live one.
//
// Only the deterministic subset of the /health quota section is pinned: the
// window list and the poller's own state. last_success_at, sample_age_seconds,
// interval, and stale_after move with the clock and the wiring and are
// deliberately absent from the fixtures.
//
// The case table must stay identical to the script's HERMETIC_ROUNDS rotation,
// and the rotation cross-check below is what keeps that true: a pair added to
// one side only is either a proxy view the script serves with no pin to the
// renderer, or a pin the script never observes.
func TestQuotaCanaryProxyViewFixtures(t *testing.T) {
	cases := []struct{ provider, view string }{
		{"healthy.json", "proxy_quota_view_healthy.json"},
		{"usage_increased.json", "proxy_quota_view_increased.json"},
		{"reset_shifted.json", "proxy_quota_view_reset_shifted.json"},
	}

	rotation := canaryScriptRotationPairs(t)
	if len(rotation) != len(cases) {
		t.Fatalf("scripts/quota_canary.py rotates through %d fixture pairs, this test pins %d -- a pair added to one must be added to the other",
			len(rotation), len(cases))
	}
	for i, pair := range rotation {
		if pair[0] != cases[i].provider || pair[1] != cases[i].view {
			t.Errorf("script rotation[%d] = (%q, %q), this test pins (%q, %q)",
				i, pair[0], pair[1], cases[i].provider, cases[i].view)
		}
	}

	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			resetQuotaMetrics()
			clock := newFakeQuotaClock()
			endpoint := newQuotaFixtureEndpoint(t, http.StatusOK, readCanaryFixture(t, tc.provider))
			poller := newFixtureQuotaPoller(t, endpoint.URL, clock)

			poller.pollOnce(context.Background())
			health, _ := fetchHealthPayload(t, NewAdaptiveRateLimiter(50, 5, 100), poller)

			var want struct {
				Enabled     bool                `json:"enabled"`
				Fresh       bool                `json:"fresh"`
				LastOutcome string              `json:"last_outcome"`
				PlanTier    string              `json:"plan_tier"`
				Windows     []QuotaWindowHealth `json:"windows"`
			}
			if err := json.Unmarshal(readCanaryFixture(t, tc.view), &want); err != nil {
				t.Fatalf("decoding proxy view fixture %s: %v", tc.view, err)
			}

			if health.Enabled != want.Enabled {
				t.Errorf("enabled = %v, fixture says %v", health.Enabled, want.Enabled)
			}
			if health.Fresh != want.Fresh {
				t.Errorf("fresh = %v, fixture says %v", health.Fresh, want.Fresh)
			}
			if health.LastOutcome != want.LastOutcome {
				t.Errorf("last_outcome = %q, fixture says %q", health.LastOutcome, want.LastOutcome)
			}
			if health.PlanTier != want.PlanTier {
				t.Errorf("plan_tier = %q, fixture says %q", health.PlanTier, want.PlanTier)
			}
			if len(health.Windows) != len(want.Windows) {
				t.Fatalf("rendered %d windows (%+v), fixture models %d -- unmodelled provider entries must stay absent",
					len(health.Windows), health.Windows, len(want.Windows))
			}
			for i, got := range health.Windows {
				if got != want.Windows[i] {
					t.Errorf("window[%d] = %+v, fixture says %+v", i, got, want.Windows[i])
				}
			}
		})
	}
}

// TestQuotaCanaryScriptHermeticRun pins proxy/scripts/quota_canary.py end to
// end. The script is the operations-facing half of the canary pair -- the Go
// canaries above exercise production wiring, the script re-samples the same
// committed fixtures the way an operator runs it -- so its two guarantees are
// pinned here rather than trusted from its own exit code:
//
//  1. Schema: the emitted artifact carries only the fields the script's
//     allowlist permits (timestamps, percentages, reset measures, and the
//     structural fields that say what a number belongs to), never an absolute
//     amount, a plan field, or a raw provider payload key. The allowlist is
//     restated here on purpose: the script enforces it at run time, this
//     enforces it from outside, so widening the schema requires editing both.
//  2. Secret safety: the child runs with ZAI_API_KEY removed from its
//     environment -- the hermetic path must need no credential -- and neither
//     its output nor its artifact carries the credential it minted, while its
//     own scanner is proven not-blind by --self-test before its clean bill of
//     health counts for anything.
//
// --no-go-canary keeps this from recursing: the hermetic Go canaries already
// ran in this binary, and a test that shells out to `go test` would re-enter
// the package it is running in.
func TestQuotaCanaryScriptHermeticRun(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not on PATH; the script-side canary cannot run here")
	}

	outDir := t.TempDir()
	stdout, _ := runQuotaCanaryScript(t, python, "run", "--no-go-canary", "--out", outDir)

	artifactPath := canaryArtifactPath(t, outDir)
	artifactBytes, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("reading emitted artifact %s: %v", artifactPath, err)
	}
	artifactText := string(artifactBytes)

	var artifact map[string]any
	if err := json.Unmarshal(artifactBytes, &artifact); err != nil {
		t.Fatalf("emitted artifact %s is not valid JSON: %v", artifactPath, err)
	}
	assertCanarySchemaKeys(t, "artifact", artifact, canaryArtifactTopKeys)

	if got := artifact["schema_version"]; got != float64(1) {
		t.Errorf("schema_version = %v, want 1", got)
	}
	if got := artifact["mode"]; got != "hermetic" {
		t.Errorf("mode = %v, want hermetic (the pinned run must not need a credential)", got)
	}
	if got := artifact["verdict"]; got != "agree" {
		t.Errorf("verdict = %v, want agree -- the fixtures render the same payload on both surfaces", got)
	}

	samples, _ := artifact["samples"].([]any)
	comparisons, _ := artifact["comparisons"].([]any)
	if len(samples) == 0 || len(comparisons) == 0 {
		t.Fatalf("artifact carries %d samples and %d comparisons; both must be non-empty",
			len(samples), len(comparisons))
	}
	for i, entry := range samples {
		sample, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("sample[%d] is not an object: %v", i, entry)
		}
		assertCanarySchemaKeys(t, fmt.Sprintf("sample[%d]", i), sample, canarySampleKeys)
		if sample["surface"] != "proxy" && sample["surface"] != "zcode" {
			t.Errorf("sample[%d] surface = %v, want proxy or zcode", i, sample["surface"])
		}
		if sample["window"] != "five_hour" && sample["window"] != "weekly" {
			t.Errorf("sample[%d] window = %v, want five_hour or weekly", i, sample["window"])
		}
	}
	for i, entry := range comparisons {
		comparison, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("comparison[%d] is not an object: %v", i, entry)
		}
		assertCanarySchemaKeys(t, fmt.Sprintf("comparison[%d]", i), comparison, canaryComparisonKeys)
		if comparison["within_tolerance"] != true {
			t.Errorf("comparison[%d] within_tolerance = %v, want true", i, comparison["within_tolerance"])
		}
	}

	// The provider's own field names may stand in for its bytes in a fixture,
	// but a projection of that payload may not carry them out.
	for _, key := range canaryRawPayloadKeys {
		if strings.Contains(artifactText, `"`+key+`"`) {
			t.Errorf("artifact carries raw provider payload key %q", key)
		}
	}

	// The synthetic credential is minted inside the child and must die there:
	// neither the artifact nor anything the child printed may carry it.
	credentialShape := regexp.MustCompile(`canary-[0-9a-f]{48}`)
	for _, surface := range []string{artifactText, stdout} {
		if loc := credentialShape.FindString(surface); loc != "" {
			t.Errorf("canary output carries credential-shaped material: %q", loc[:8]+"...")
		}
	}

	// A clean bill from a scanner that was never proven to see anything is
	// worth nothing, so the self-test plants one secret per pattern class and
	// asserts detection before the same scan reports the canary's own files.
	selfTestOut, _ := runQuotaCanaryScript(t, python, "secret-scan", "--self-test")
	if !strings.Contains(selfTestOut, "self-test passed") || !strings.Contains(selfTestOut, "secret_scan=clean") {
		t.Errorf("secret-scan --self-test did not report detection plus clean files:\n%s", selfTestOut)
	}
}

// ---- Canary script plumbing --------------------------------------------------

// canaryArtifactTopKeys, canarySampleKeys, and canaryComparisonKeys restate
// the script's output-schema allowlist (proxy/scripts/quota_canary.py
// ALLOWED_TOP / ALLOWED_SAMPLE / ALLOWED_COMPARISON). The script enforces them
// at run time; this enforces them from the outside.
var (
	canaryArtifactTopKeys = []string{
		"schema_version", "generated_at", "mode", "tolerance_pp",
		"reset_tolerance_seconds", "samples", "comparisons", "verdict",
	}
	canarySampleKeys = []string{
		"at", "surface", "round", "window", "used_percent",
		"used_percent_delta_pp", "reset_at", "reset_delta_seconds",
		"reset_in_seconds",
	}
	canaryComparisonKeys = []string{
		"at", "round", "window", "proxy_used_percent", "zcode_used_percent",
		"delta_pp", "proxy_reset_at", "zcode_reset_at", "reset_delta_seconds",
		"within_tolerance",
	}

	// Raw provider payload keys that may exist in a fixture and may never
	// reach the artifact the script emits.
	canaryRawPayloadKeys = []string{
		"currentValue", "nextResetTime", "percentage", "remaining",
		"limits", "level", "msg", "success",
	}
)

// runQuotaCanaryScript runs one invocation of the script from the proxy
// package directory -- the working directory of a package test -- with
// ZAI_API_KEY stripped from the environment, so a hermetic run is never
// silently riding on a credential the machine happened to have.
func runQuotaCanaryScript(t *testing.T, python string, args ...string) (string, string) {
	t.Helper()

	command := exec.Command(python, append([]string{"scripts/quota_canary.py"}, args...)...)
	command.Env = canaryCredentialFreeEnv()
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("quota_canary.py %s: %v\nstdout:\n%s\nstderr:\n%s",
			strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

func canaryCredentialFreeEnv() []string {
	env := os.Environ()
	kept := env[:0]
	for _, entry := range env {
		if strings.HasPrefix(entry, "ZAI_API_KEY=") {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

func canaryArtifactPath(t *testing.T, dir string) string {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(dir, "zai-quota-canary-*.json"))
	if err != nil {
		t.Fatalf("globbing canary artifacts in %s: %v", dir, err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly one emitted canary artifact in %s, found %d", dir, len(matches))
	}
	return matches[0]
}

func assertCanarySchemaKeys(t *testing.T, what string, obj map[string]any, allowed []string) {
	t.Helper()

	permitted := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		permitted[key] = true
	}
	for key := range obj {
		if !permitted[key] {
			t.Errorf("%s carries field %q outside the canary output schema", what, key)
		}
	}
}

// ---- Canary plumbing ---------------------------------------------------------

// canaryScriptRotationPairs reads the fixture pairs the script's hermetic run
// rotates through (HERMETIC_ROUNDS in proxy/scripts/quota_canary.py), so
// TestQuotaCanaryProxyViewFixtures can require its own case table to name
// exactly those pairs. Reading the table rather than trusting the two
// restatements to stay in step is the same discipline as the schema allowlist:
// widening the rotation has to edit both sides, and this fails when only one
// side changed.
func canaryScriptRotationPairs(t *testing.T) [][2]string {
	t.Helper()

	script, err := os.ReadFile(filepath.Join("scripts", "quota_canary.py"))
	if err != nil {
		t.Fatalf("reading the canary script: %v", err)
	}
	block := regexp.MustCompile(`(?s)HERMETIC_ROUNDS\s*=\s*\[(.*?)\]`).FindSubmatch(script)
	if block == nil {
		t.Fatalf("HERMETIC_ROUNDS not found in scripts/quota_canary.py")
	}
	var rotation [][2]string
	pair := regexp.MustCompile(`\("([A-Za-z0-9_.]+\.json)",\s*"([A-Za-z0-9_.]+\.json)"\)`)
	for _, match := range pair.FindAllSubmatch(block[1], -1) {
		rotation = append(rotation, [2]string{string(match[1]), string(match[2])})
	}
	if len(rotation) == 0 {
		t.Fatalf("HERMETIC_ROUNDS in scripts/quota_canary.py holds no fixture pairs")
	}
	return rotation
}

func canaryLogf(t *testing.T, format string, args ...any) {
	t.Logf("%s | %s", time.Now().UTC().Format(time.RFC3339Nano), fmt.Sprintf(format, args...))
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshalling canary evidence failed: %v", err)
	}
	return string(raw)
}

func canaryTimeAdvanced(before, after string) bool {
	prev, err := time.Parse(time.RFC3339, before)
	if err != nil {
		return false
	}
	next, err := time.Parse(time.RFC3339, after)
	if err != nil {
		return false
	}
	return next.After(prev)
}

// canaryHealth mirrors the /health payload the canary reads.
type canaryHealth struct {
	Status    string `json:"status"`
	RateLimit struct {
		CurrentRate float64 `json:"current_rate"`
		Ceiling     float64 `json:"ceiling"`
	} `json:"rate_limit"`
	Quota struct {
		Enabled          bool    `json:"enabled"`
		Fresh            bool    `json:"fresh"`
		Interval         string  `json:"interval"`
		StaleAfter       string  `json:"stale_after"`
		LastSuccessAt    string  `json:"last_success_at"`
		SampleAgeSeconds float64 `json:"sample_age_seconds"`
		LastOutcome      string  `json:"last_outcome"`
		PlanTier         string  `json:"plan_tier"`
		Windows          []struct {
			Window       string  `json:"window"`
			LimitType    string  `json:"limit_type"`
			UsedFraction float64 `json:"used_fraction"`
			ResetAt      string  `json:"reset_at"`
		} `json:"windows"`
	} `json:"quota"`
}

// syncBuffer is a concurrency-safe log capture.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

// readCanaryFixture loads a committed, synthetic payload from
// testdata/quota_canary. The working directory of a package test is the
// package directory.
func readCanaryFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/quota_canary/" + name)
	if err != nil {
		t.Fatalf("reading canary fixture %s: %v", name, err)
	}
	return raw
}

// canaryQuotaFixture is the synthetic quota endpoint. It serves the committed
// payloads, provider-style rejections, or malformed bytes, and it records one
// non-secret fact per poll: when it arrived, that it was a GET on the fixed
// monitor path, and that it carried the synthetic credential. The raw
// Authorization value is compared, never stored or printed.
type canaryQuotaFixture struct {
	t      *testing.T
	addr   string
	apiKey string

	mu       sync.Mutex
	mode     canaryFixtureMode
	status   int
	body     []byte
	listener net.Listener
	server   *http.Server
	requests []canaryPollRecord
}

type canaryFixtureMode int

const (
	canaryServeFile canaryFixtureMode = iota
	canaryServeStatus
	canaryServeMalformed
)

type canaryPollRecord struct {
	at        time.Time
	method    string
	path      string
	authMatch bool
}

func newCanaryQuotaFixture(t *testing.T, apiKey string) *canaryQuotaFixture {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("canary fixture could not bind: %v", err)
	}
	f := &canaryQuotaFixture{t: t, addr: listener.Addr().String(), listener: listener, apiKey: apiKey}
	f.server = &http.Server{Handler: http.HandlerFunc(f.handle)}
	go func() { _ = f.server.Serve(listener) }()
	t.Cleanup(func() {
		f.mu.Lock()
		ln := f.listener
		f.mu.Unlock()
		if ln != nil {
			_ = ln.Close()
		}
	})
	return f
}

func (f *canaryQuotaFixture) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	mode, status, body := f.mode, f.status, f.body
	f.requests = append(f.requests, canaryPollRecord{
		at:        time.Now().UTC(),
		method:    r.Method,
		path:      r.URL.Path,
		authMatch: r.Header.Get("Authorization") == f.apiKey,
	})
	f.mu.Unlock()

	switch mode {
	case canaryServeStatus:
		w.WriteHeader(status)
	case canaryServeMalformed:
		// Truncated mid-envelope: a body the endpoint did send but nothing
		// can parse, which is exactly the malformed outcome class.
		_, _ = io.WriteString(w, `{"code":200,"success":true,"data":{"limits":[{"type":"CRED`)
	default:
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
}

func (f *canaryQuotaFixture) serveFile(body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mode, f.status, f.body = canaryServeFile, 0, body
}

func (f *canaryQuotaFixture) serveStatus(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mode, f.status = canaryServeStatus, status
}

func (f *canaryQuotaFixture) serveMalformed() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mode = canaryServeMalformed
}

// refuseConnections closes the listener and every connection the fixture
// holds, so polls fail at the transport layer exactly as when the monitor
// origin is unreachable. Closing the listener alone does not do that: the
// poller's client pools its keep-alive connection, and an idle pooled
// connection keeps answering polls across a dead listener, so the refused
// outcome -- and with it this whole phase of the canary -- becomes a race.
func (f *canaryQuotaFixture) refuseConnections() {
	f.mu.Lock()
	ln, srv := f.listener, f.server
	f.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	// Server.Close tears down established StateIdle and StateActive
	// connections as well as the listener; no pooled connection survives it.
	if srv != nil {
		_ = srv.Close()
	}
}

// acceptConnectionsAgain reopens the same address on a fresh http.Server --
// Server.Close latches the old one into shutdown, so serving it again would
// return ErrServerClosed immediately -- exactly as an origin recovering from
// an outage would; the poller's configured base URL stays valid.
func (f *canaryQuotaFixture) acceptConnectionsAgain() {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	listener, err := net.Listen("tcp", f.addr)
	if err != nil {
		f.t.Fatalf("canary fixture could not rebind %s: %v", f.addr, err)
	}
	f.listener = listener
	f.server = &http.Server{Handler: http.HandlerFunc(f.handle)}
	go func() { _ = f.server.Serve(listener) }()
}

func (f *canaryQuotaFixture) pollCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *canaryQuotaFixture) allPollsAreMonitorGets() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.requests {
		if r.method != http.MethodGet || r.path != quota.QuotaLimitPath || !r.authMatch {
			return false
		}
	}
	return true
}

func (f *canaryQuotaFixture) pollRecords() []canaryPollRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]canaryPollRecord(nil), f.requests...)
}

// canaryInferenceUpstream is the model-traffic stand-in. It counts every
// request so the verdict can prove the quota machinery never generated
// inference traffic, and it checks -- by equality only -- that the proxy
// injected the proxy-held credential it is designed to hold.
type canaryInferenceUpstream struct {
	*httptest.Server

	mu    sync.Mutex
	count int
}

func newCanaryInferenceUpstream(t *testing.T, apiKey string) *canaryInferenceUpstream {
	t.Helper()
	u := &canaryInferenceUpstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.count++
		u.mu.Unlock()
		if got := r.Header.Get("x-api-key"); got != apiKey {
			t.Errorf("inference upstream saw an unexpected proxy credential")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{}")
	}))
	return u
}

func (u *canaryInferenceUpstream) requestCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.count
}
