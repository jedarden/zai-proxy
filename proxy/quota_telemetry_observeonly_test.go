package main

// Hermetic coverage for the observe-only quota telemetry surface
// (docs/plan/plan.md, "Quota-aware throttling plan").
//
// Each test drives the production path end to end: a provider payload from
// proxy/testdata/quota_telemetry through the real quota client, the poller,
// and both published surfaces (/health and /metrics). It pins two things —
// that the observation is normalized and recorded exactly (window, limit
// type, used fraction, reset timestamp), and that no quota signal, however
// extreme, ever turns into an enforcement action. Every endpoint here is a
// local httptest server, so this file needs neither network access nor
// credentials.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"git.ardenone.com/jedarden/zai-proxy/proxy/quota"
)

// quotaTelemetryVariant keeps this file's metric series separate from the
// other quota tests', so its series reads never see another test's writes.
const quotaTelemetryVariant = "quota-telemetry-fixture-test"

// quotaTelemetryFixtureDir holds the provider payloads these tests replay.
const quotaTelemetryFixtureDir = "testdata/quota_telemetry"

// Provider values the fixtures carry, named here so a fixture edit fails here,
// loudly, instead of silently changing what these tests pin.
const (
	fixtureFiveHourResetMillis = int64(1788541838272)
	fixtureWeeklyResetMillis   = int64(1789128084993)

	fixtureFiveHourUsedFraction = 0.37
	fixtureWeeklyUsedFraction   = 0.5207
	fixtureLegacyWeeklyFraction = 0.07
)

// loadQuotaTelemetryFixture reads one provider payload from the fixture
// directory.
func loadQuotaTelemetryFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(quotaTelemetryFixtureDir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return body
}

// quotaFixtureEndpoint is a local quota origin whose reply a test swaps
// between phases, so one poller can be driven through healthy, exhausted,
// failing, and recovered polls without rebuilding the client.
type quotaFixtureEndpoint struct {
	*httptest.Server

	mu     sync.Mutex
	status int
	body   []byte
}

func newQuotaFixtureEndpoint(t *testing.T, status int, body []byte) *quotaFixtureEndpoint {
	t.Helper()
	endpoint := &quotaFixtureEndpoint{status: status, body: body}
	endpoint.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint.mu.Lock()
		status, body := endpoint.status, endpoint.body
		endpoint.mu.Unlock()
		if status != http.StatusOK {
			w.WriteHeader(status)
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(endpoint.Close)
	return endpoint
}

// reply points the endpoint at a new phase.
func (e *quotaFixtureEndpoint) reply(status int, body []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.status, e.body = status, body
}

// newFixtureQuotaPoller builds the production poller against a local endpoint,
// stamped by the test's clock so every reported timestamp is deterministic.
func newFixtureQuotaPoller(t *testing.T, endpointURL string, clock *fakeQuotaClock) *QuotaPoller {
	t.Helper()
	client, err := quota.NewClient(fakeCredential, endpointURL,
		quota.WithTimeout(2*time.Second), quota.WithNow(clock.Now))
	if err != nil {
		t.Fatalf("quota.NewClient: %v", err)
	}
	poller, err := NewQuotaPoller(client, time.Minute, 15*time.Minute, quotaTelemetryVariant)
	if err != nil {
		t.Fatalf("NewQuotaPoller: %v", err)
	}
	poller.now = clock.Now
	return poller
}

// fetchHealthPayload renders /health through the production handler and
// decodes both of its sections.
func fetchHealthPayload(t *testing.T, arl *AdaptiveRateLimiter, poller *QuotaPoller) (QuotaHealth, RateLimitHealth) {
	t.Helper()
	rec := httptest.NewRecorder()
	newHealthHandler(arl, poller).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/health status = %d, want 200", rec.Code)
	}
	var payload struct {
		Status    string          `json:"status"`
		RateLimit RateLimitHealth `json:"rate_limit"`
		Quota     QuotaHealth     `json:"quota"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decoding /health body %q: %v", rec.Body.String(), err)
	}
	if payload.Status != "ok" {
		t.Errorf("/health status = %q, want %q", payload.Status, "ok")
	}
	return payload.Quota, payload.RateLimit
}

// renderedQuotaValues reads the real /metrics endpoint and returns every
// series it currently exports for one quota family, keyed by the series'
// canonicalized label list, e.g.
// `limit_type="credit_limit",variant="x",window="five_hour"`. A family with no
// series — a Vec with no children is omitted from the exposition — returns an
// empty map, which is how these tests assert that enforcement never exported
// anything.
func renderedQuotaValues(t *testing.T, family string) map[string]float64 {
	t.Helper()
	rec := httptest.NewRecorder()
	promhttp.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics status = %d, want 200", rec.Code)
	}

	prefix := family + "{"
	values := map[string]float64{}
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		rest := strings.TrimPrefix(line, prefix)
		separator := strings.LastIndex(rest, "} ")
		if separator < 0 {
			t.Fatalf("unparseable %s line %q", family, line)
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(rest[separator+2:]), 64)
		if err != nil {
			t.Fatalf("unparseable %s value in %q: %v", family, line, err)
		}
		values[canonicalQuotaLabels(rest[:separator])] = value
	}
	return values
}

// canonicalQuotaLabels name-sorts a rendered label list, so series lookups do
// not depend on the order the exporter happens to render labels in. Quota
// label values are bounded to a charset with no commas, so a split on comma is
// exact.
func canonicalQuotaLabels(rendered string) string {
	pairs := strings.Split(rendered, ",")
	sort.Strings(pairs)
	return strings.Join(pairs, ",")
}

// quotaSeriesLabels renders the canonical label list for a quota observation
// series.
func quotaSeriesLabels(limitType, window string) string {
	return canonicalQuotaLabels(strings.Join([]string{
		`limit_type="` + strings.ToLower(limitType) + `"`,
		`variant="` + quotaTelemetryVariant + `"`,
		`window="` + window + `"`,
	}, ","))
}

// assertQuotaSeries pins one family's whole export: every series it must carry
// at exactly the given value, and no series beyond them.
func assertQuotaSeries(t *testing.T, family string, want map[string]float64) {
	t.Helper()
	got := renderedQuotaValues(t, family)
	if len(got) != len(want) {
		t.Errorf("%s exported %d series (%v), want %d", family, len(got), got, len(want))
	}
	for labels, wantValue := range want {
		gotValue, ok := got[labels]
		if !ok {
			t.Errorf("%s exported no series for %s; got %v", family, labels, got)
			continue
		}
		if gotValue != wantValue {
			t.Errorf("%s{%s} = %v, want %v", family, labels, gotValue, wantValue)
		}
	}
}

// assertNoQuotaEnforcement pins the observe-only contract at every place it
// could break: neither enforcement gauge may carry a series, the rate limiter
// must still be at the state it was constructed with, and admission must not
// have learned to wait.
func assertNoQuotaEnforcement(t *testing.T, arl *AdaptiveRateLimiter, baseline RateLimitHealth, baselineRate float64) {
	t.Helper()
	for _, family := range []string{"zai_proxy_quota_rate_cap", "zai_proxy_quota_gate_open"} {
		if got := renderedQuotaValues(t, family); len(got) != 0 {
			t.Errorf("%s exported %v from observe-only quota telemetry, want no series", family, got)
		}
	}

	if got := arl.GetCurrentRate(); got != baselineRate {
		t.Errorf("rate limiter moved to %.2f req/s from a quota signal, want %.2f", got, baselineRate)
	}
	if got := arl.HealthState(); !reflect.DeepEqual(got, baseline) {
		t.Errorf("rate limiter health moved to %+v from a quota signal, want %+v", got, baseline)
	}

	start := time.Now()
	arl.Wait(quotaTelemetryVariant)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("admission took %s after a quota signal, want it unblocked", elapsed)
	}
}

// fixtureWindow is one window's normalized expectation, shared by the /health
// and /metrics assertions so both surfaces are pinned against one value.
type fixtureWindow struct {
	window    string
	limitType string // the provider's own schema name, as /health reports it
	used      float64
	resetAt   time.Time // zero when the provider advertised no reset
}

// assertWindowsOnBothSurfaces checks a whole snapshot against /health and
// /metrics: each window's usage, remaining, and reset values, and no series
// beyond them. The derived values — remaining = 1 - usage, and the reset stamp
// truncated to Unix seconds — are computed exactly as production computes
// them, so the pin is exact rather than approximate.
func assertWindowsOnBothSurfaces(t *testing.T, windows []fixtureWindow, health QuotaHealth) {
	t.Helper()

	wantUsage := map[string]float64{}
	wantRemaining := map[string]float64{}
	wantReset := map[string]float64{}
	for _, w := range windows {
		labels := quotaSeriesLabels(w.limitType, w.window)
		wantUsage[labels] = w.used
		wantRemaining[labels] = 1 - w.used
		if !w.resetAt.IsZero() {
			wantReset[labels] = float64(w.resetAt.Unix())
		}
	}
	assertQuotaSeries(t, "zai_proxy_quota_usage_ratio", wantUsage)
	assertQuotaSeries(t, "zai_proxy_quota_remaining_ratio", wantRemaining)
	assertQuotaSeries(t, "zai_proxy_quota_reset_time_seconds", wantReset)

	if len(health.Windows) != len(windows) {
		t.Fatalf("/health carried %d windows (%+v), want the %d the fixture models",
			len(health.Windows), health.Windows, len(windows))
	}
	for _, w := range windows {
		var reported *QuotaWindowHealth
		for i := range health.Windows {
			if health.Windows[i].Window == w.window {
				reported = &health.Windows[i]
				break
			}
		}
		if reported == nil {
			t.Fatalf("/health carried no %s window; got %+v", w.window, health.Windows)
		}
		wantWindow := QuotaWindowHealth{
			Window:       w.window,
			LimitType:    w.limitType,
			UsedFraction: w.used,
		}
		if !w.resetAt.IsZero() {
			wantWindow.ResetAt = w.resetAt.UTC().Format(time.RFC3339)
		}
		if !reflect.DeepEqual(*reported, wantWindow) {
			t.Errorf("/health %s window = %+v, want %+v", w.window, *reported, wantWindow)
		}
	}
}

// TestQuotaTelemetryRendersFixturesOnBothSurfaces replays the two payload
// schemas the provider is known to send and pins the normalized observation on
// both surfaces: /health carries the window, limit type, used fraction, and
// RFC3339 reset stamp; /metrics carries the same values as gauges with the
// bounded label vocabulary. A payload entry this proxy does not model — the
// monthly TIME_LIMIT — must reach neither surface.
func TestQuotaTelemetryRendersFixturesOnBothSurfaces(t *testing.T) {
	tests := []struct {
		name         string
		fixture      string
		wantPlanTier string
		wantWindows  []fixtureWindow
	}{
		{
			name:         "current credit schema",
			fixture:      "healthy_credit_windows.json",
			wantPlanTier: "max",
			wantWindows: []fixtureWindow{
				{
					window:    string(quota.WindowFiveHour),
					limitType: string(quota.LimitTypeCredit),
					used:      fixtureFiveHourUsedFraction,
					resetAt:   time.UnixMilli(fixtureFiveHourResetMillis).UTC(),
				},
				{
					window:    string(quota.WindowWeekly),
					limitType: string(quota.LimitTypeCredit),
					used:      fixtureWeeklyUsedFraction,
					resetAt:   time.UnixMilli(fixtureWeeklyResetMillis).UTC(),
				},
			},
		},
		{
			name:         "legacy percentage-only schema",
			fixture:      "legacy_percentage_windows.json",
			wantPlanTier: "pro",
			wantWindows: []fixtureWindow{
				{
					window:    string(quota.WindowFiveHour),
					limitType: string(quota.LimitTypeTokens),
					used:      fixtureFiveHourUsedFraction,
					resetAt:   time.UnixMilli(fixtureFiveHourResetMillis).UTC(),
				},
				// This entry advertises no reset, so no reset telemetry may
				// appear for it: an absent stamp must not render as an epoch.
				{
					window:    string(quota.WindowWeekly),
					limitType: string(quota.LimitTypeTokens),
					used:      fixtureLegacyWeeklyFraction,
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetQuotaMetrics()
			arl := NewAdaptiveRateLimiter(50, 5, 100)
			baselineRate := arl.GetCurrentRate()
			clock := newFakeQuotaClock()
			endpoint := newQuotaFixtureEndpoint(t, http.StatusOK, loadQuotaTelemetryFixture(t, tc.fixture))
			poller := newFixtureQuotaPoller(t, endpoint.URL, clock)

			poller.pollOnce(context.Background())

			health, rateLimit := fetchHealthPayload(t, arl, poller)

			if !health.Enabled {
				t.Error("/health quota.enabled = false with a wired poller, want true")
			}
			if !health.Fresh {
				t.Error("/health quota.fresh = false right after a successful poll")
			}
			if health.LastOutcome != quotaPollOutcomeSuccess {
				t.Errorf("/health quota.last_outcome = %q, want %q", health.LastOutcome, quotaPollOutcomeSuccess)
			}
			if health.PlanTier != tc.wantPlanTier {
				t.Errorf("/health quota.plan_tier = %q, want %q", health.PlanTier, tc.wantPlanTier)
			}
			if want := time.Minute.String(); health.Interval != want {
				t.Errorf("/health quota.interval = %q, want the poller's own cadence %q", health.Interval, want)
			}
			if want := (15 * time.Minute).String(); health.StaleAfter != want {
				t.Errorf("/health quota.stale_after = %q, want %q", health.StaleAfter, want)
			}
			if want := pollerTestStart.UTC().Format(time.RFC3339); health.LastSuccessAt != want {
				t.Errorf("/health quota.last_success_at = %q, want the poll's stamp %q", health.LastSuccessAt, want)
			}
			if health.SampleAgeSeconds != 0 {
				t.Errorf("/health quota.sample_age_seconds = %v, want 0 at the poll instant", health.SampleAgeSeconds)
			}
			if len(health.Windows) != len(tc.wantWindows) {
				t.Fatalf("/health carried %d windows (%+v), want the %d the fixture models",
					len(health.Windows), health.Windows, len(tc.wantWindows))
			}

			assertWindowsOnBothSurfaces(t, tc.wantWindows, health)

			assertQuotaSeries(t, "zai_proxy_quota_sample_age_seconds", map[string]float64{
				`variant="` + quotaTelemetryVariant + `"`: 0,
			})
			assertQuotaSeries(t, "zai_proxy_quota_poll_total", map[string]float64{
				`result="success",variant="` + quotaTelemetryVariant + `"`: 1,
			})

			// The sample ages on the poller's clock and expires on schedule,
			// and the reported stamp stays the poll's own.
			clock.Advance(90 * time.Second)
			health, _ = fetchHealthPayload(t, arl, poller)
			if !health.Fresh {
				t.Error("/health quota.fresh = false 90s into a 15m stale-after, want true")
			}
			if health.SampleAgeSeconds != (90 * time.Second).Seconds() {
				t.Errorf("/health quota.sample_age_seconds = %v, want 90", health.SampleAgeSeconds)
			}
			if want := pollerTestStart.UTC().Format(time.RFC3339); health.LastSuccessAt != want {
				t.Errorf("/health quota.last_success_at = %q, want the unchanged poll stamp %q", health.LastSuccessAt, want)
			}
			clock.Advance(15 * time.Minute)
			if health := poller.HealthState(); health.Fresh {
				t.Error("/health quota.fresh = true past stale-after, want false")
			}

			assertNoQuotaEnforcement(t, arl, rateLimit, baselineRate)
		})
	}
}

// TestQuotaExhaustionIsRecordedButEnforcesNothing drives the most provocative
// payload the provider can send — a five-hour window past 100% and a weekly
// window exactly at it — through the production path. The overdraw must still
// be recorded (normalized to the [0,1] fraction the schema documents), and it
// must remain a fact for operators to read: no rate cap, no gate, no movement
// in the limiter, no slowed admission.
func TestQuotaExhaustionIsRecordedButEnforcesNothing(t *testing.T) {
	resetQuotaMetrics()
	arl := NewAdaptiveRateLimiter(50, 5, 100)
	baselineRate := arl.GetCurrentRate()
	clock := newFakeQuotaClock()
	endpoint := newQuotaFixtureEndpoint(t, http.StatusOK, loadQuotaTelemetryFixture(t, "exhausted_and_overdrawn_windows.json"))
	poller := newFixtureQuotaPoller(t, endpoint.URL, clock)

	_, rateLimitBefore := fetchHealthPayload(t, arl, poller)
	poller.pollOnce(context.Background())
	health, rateLimitAfter := fetchHealthPayload(t, arl, poller)

	if !health.Fresh {
		t.Error("/health quota.fresh = false after polling an exhausted account, want true")
	}
	if health.LastOutcome != quotaPollOutcomeSuccess {
		t.Errorf("/health quota.last_outcome = %q, want %q: exhaustion is an observation, not a failure",
			health.LastOutcome, quotaPollOutcomeSuccess)
	}
	assertWindowsOnBothSurfaces(t, []fixtureWindow{
		{
			window:    string(quota.WindowFiveHour),
			limitType: string(quota.LimitTypeCredit),
			used:      1, // clamped from the provider's 137%
			resetAt:   time.UnixMilli(fixtureFiveHourResetMillis).UTC(),
		},
		{
			window:    string(quota.WindowWeekly),
			limitType: string(quota.LimitTypeCredit),
			used:      1,
			resetAt:   time.UnixMilli(fixtureWeeklyResetMillis).UTC(),
		},
	}, health)

	assertQuotaSeries(t, "zai_proxy_quota_poll_total", map[string]float64{
		`result="success",variant="` + quotaTelemetryVariant + `"`: 1,
	})

	if !reflect.DeepEqual(rateLimitBefore, rateLimitAfter) {
		t.Errorf("/health rate_limit moved from %+v to %+v on an exhausted quota sample", rateLimitBefore, rateLimitAfter)
	}
	assertNoQuotaEnforcement(t, arl, rateLimitBefore, baselineRate)
}

// TestQuotaSignalPhasesNeverTriggerEnforcement walks one poller through the
// phases an account actually goes through — healthy, exhausted, outage, and
// recovery — and pins that no phase, including the exhausted and stale ones
// where enforcement would be most tempting, produces any admission action.
// The observation itself must keep working throughout: outcomes counted, the
// retained sample kept across the outage, and staleness counted once.
func TestQuotaSignalPhasesNeverTriggerEnforcement(t *testing.T) {
	resetQuotaMetrics()
	arl := NewAdaptiveRateLimiter(50, 5, 100)
	baselineRate := arl.GetCurrentRate()
	baselineHealth := arl.HealthState()
	clock := newFakeQuotaClock()
	endpoint := newQuotaFixtureEndpoint(t, http.StatusOK, loadQuotaTelemetryFixture(t, "healthy_credit_windows.json"))
	poller := newFixtureQuotaPoller(t, endpoint.URL, clock)

	healthyWindows := []fixtureWindow{
		{
			window:    string(quota.WindowFiveHour),
			limitType: string(quota.LimitTypeCredit),
			used:      fixtureFiveHourUsedFraction,
			resetAt:   time.UnixMilli(fixtureFiveHourResetMillis).UTC(),
		},
		{
			window:    string(quota.WindowWeekly),
			limitType: string(quota.LimitTypeCredit),
			used:      fixtureWeeklyUsedFraction,
			resetAt:   time.UnixMilli(fixtureWeeklyResetMillis).UTC(),
		},
	}
	exhaustedWindows := []fixtureWindow{
		{
			window:    string(quota.WindowFiveHour),
			limitType: string(quota.LimitTypeCredit),
			used:      1,
			resetAt:   time.UnixMilli(fixtureFiveHourResetMillis).UTC(),
		},
		{
			window:    string(quota.WindowWeekly),
			limitType: string(quota.LimitTypeCredit),
			used:      1,
			resetAt:   time.UnixMilli(fixtureWeeklyResetMillis).UTC(),
		},
	}

	// phaseCheck is one point in the sequence: what the observation reports
	// and what the poll outcome counters have accumulated.
	type phaseCheck struct {
		name        string
		lastOutcome string
		fresh       bool
		ageSeconds  float64
		polls       map[string]float64
		windows     []fixtureWindow
	}
	assertPhase := func(check phaseCheck) {
		t.Helper()

		health, _ := fetchHealthPayload(t, arl, poller)
		if health.LastOutcome != check.lastOutcome {
			t.Errorf("%s: /health quota.last_outcome = %q, want %q", check.name, health.LastOutcome, check.lastOutcome)
		}
		if health.Fresh != check.fresh {
			t.Errorf("%s: /health quota.fresh = %t, want %t", check.name, health.Fresh, check.fresh)
		}
		if health.SampleAgeSeconds != check.ageSeconds {
			t.Errorf("%s: /health quota.sample_age_seconds = %v, want %v", check.name, health.SampleAgeSeconds, check.ageSeconds)
		}
		assertWindowsOnBothSurfaces(t, check.windows, health)
		assertQuotaSeries(t, "zai_proxy_quota_poll_total", check.polls)
		assertQuotaSeries(t, "zai_proxy_quota_sample_age_seconds", map[string]float64{
			`variant="` + quotaTelemetryVariant + `"`: check.ageSeconds,
		})
		assertNoQuotaEnforcement(t, arl, baselineHealth, baselineRate)
	}

	// Phase 1: a healthy account.
	poller.pollOnce(context.Background())
	assertPhase(phaseCheck{
		name:        "healthy",
		lastOutcome: quotaPollOutcomeSuccess,
		fresh:       true,
		polls:       map[string]float64{`result="success",variant="` + quotaTelemetryVariant + `"`: 1},
		windows:     healthyWindows,
	})

	// Phase 2: the account burns through both windows.
	clock.Advance(2 * time.Minute)
	endpoint.reply(http.StatusOK, loadQuotaTelemetryFixture(t, "exhausted_and_overdrawn_windows.json"))
	poller.pollOnce(context.Background())
	assertPhase(phaseCheck{
		name:        "exhausted",
		lastOutcome: quotaPollOutcomeSuccess,
		fresh:       true,
		polls:       map[string]float64{`result="success",variant="` + quotaTelemetryVariant + `"`: 2},
		windows:     exhaustedWindows,
	})

	// Phase 3: the endpoint goes away long enough for the sample to go stale.
	// The last-known-good windows stay on both surfaces.
	clock.Advance(20 * time.Minute)
	endpoint.reply(http.StatusServiceUnavailable, []byte(`service unavailable`))
	poller.pollOnce(context.Background())
	assertPhase(phaseCheck{
		name:        "stale outage",
		lastOutcome: quotaPollOutcomeError,
		fresh:       false,
		ageSeconds:  (20 * time.Minute).Seconds(),
		polls: map[string]float64{
			`result="success",variant="` + quotaTelemetryVariant + `"`: 2,
			`result="error",variant="` + quotaTelemetryVariant + `"`:   1,
			`result="stale",variant="` + quotaTelemetryVariant + `"`:   1,
		},
		windows: exhaustedWindows,
	})

	// Phase 4: the outage continues; the transition into staleness is not
	// counted a second time.
	clock.Advance(time.Minute)
	poller.pollOnce(context.Background())
	assertPhase(phaseCheck{
		name:        "outage continues",
		lastOutcome: quotaPollOutcomeError,
		fresh:       false,
		ageSeconds:  (21 * time.Minute).Seconds(),
		polls: map[string]float64{
			`result="success",variant="` + quotaTelemetryVariant + `"`: 2,
			`result="error",variant="` + quotaTelemetryVariant + `"`:   2,
			`result="stale",variant="` + quotaTelemetryVariant + `"`:   1,
		},
		windows: exhaustedWindows,
	})

	// Phase 5: recovery, which is also the observation of a recovered account.
	clock.Advance(30 * time.Second)
	endpoint.reply(http.StatusOK, loadQuotaTelemetryFixture(t, "healthy_credit_windows.json"))
	poller.pollOnce(context.Background())
	assertPhase(phaseCheck{
		name:        "recovered",
		lastOutcome: quotaPollOutcomeSuccess,
		fresh:       true,
		polls: map[string]float64{
			`result="success",variant="` + quotaTelemetryVariant + `"`: 3,
			`result="error",variant="` + quotaTelemetryVariant + `"`:   2,
			`result="stale",variant="` + quotaTelemetryVariant + `"`:   1,
		},
		windows: healthyWindows,
	})
}

// TestQuotaEnforcementRecordersHaveNoLiveCallSite pins the structural half of
// observe-only: the enforcement recorders exist only as definitions in
// metrics.go and are called from nowhere in production code. A quota signal
// cannot reach request admission without introducing a call site, and this
// fails the moment one lands.
func TestQuotaEnforcementRecordersHaveNoLiveCallSite(t *testing.T) {
	dir, err := packageSourceDir()
	if err != nil {
		t.Skipf("cannot verify call sites without this package's sources: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("cannot verify call sites without this package's sources: %v", err)
	}

	// metrics.go is where the recorders are defined; a definition is not a
	// call site.
	const (
		rateCapRecorder = "RecordQuotaRateCap"
		gateRecorder    = "RecordQuotaGateOpen"
		definitions     = "metrics.go"
	)
	defined := map[string]bool{}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		for _, recorder := range []string{rateCapRecorder, gateRecorder} {
			if !strings.Contains(string(body), recorder) {
				continue
			}
			if name != definitions {
				t.Errorf("%s references %s: quota telemetry reached production code that may enforce it", name, recorder)
			}
			defined[recorder] = true
		}
	}

	for _, recorder := range []string{rateCapRecorder, gateRecorder} {
		if !defined[recorder] {
			t.Errorf("%s is no longer defined in %s; update this tripwire along with it", recorder, definitions)
		}
	}
}

// packageSourceDir resolves the directory holding this package's sources, so
// the scan does not depend on the working directory a test binary was started
// from.
func packageSourceDir() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("runtime.Caller could not locate this test file")
	}
	return filepath.Dir(thisFile), nil
}
