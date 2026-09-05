package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"git.ardenone.com/jedarden/zai-proxy/proxy/config"
	"git.ardenone.com/jedarden/zai-proxy/proxy/quota"
)

// pollerTestVariant keeps this file's metric series separate from any other
// test's, so label lookups below never race another test's writes.
const pollerTestVariant = "quota-poller-test"

// fakeCredential is deliberately fabricated. It exists only so the leak
// assertions can grep for it; it is not a real credential.
const fakeCredential = "fake-key-zai-quota-poller-tests"

// pollerTestStart is the fixed instant the deterministic clocks start from.
var pollerTestStart = time.Date(2026, 9, 5, 14, 0, 0, 0, time.UTC)

// validQuotaPayload is a one-window CREDIT_LIMIT body shaped like the
// provider's current schema. The poller only needs a body Normalize accepts.
const validQuotaPayload = `{"code":200,"msg":"Operation successful","success":true,` +
	`"data":{"level":"pro","limits":[{"type":"CREDIT_LIMIT","unit":3,"number":5,` +
	`"usage":2000,"currentValue":740,"remaining":1259,"percentage":37,` +
	`"nextResetTime":1788541838272}]}}`

// truncatedQuotaPayload is the documented malformed case: the endpoint
// answered 200 with a body that is not complete JSON.
const truncatedQuotaPayload = `{"code": 200, "msg": "Operation successful", "data": {"limits": [`

// providerRejectedPayload mirrors the provider's structured business error.
// The message deliberately carries an account marker to prove neither the
// credential nor provider message text reaches poller output.
const providerRejectedPayload = `{"code":401,"msg":"token expired or incorrect (acct:marker-7f3a9)",` +
	`"data":null,"success":false}`

// fakeQuotaClock is a deterministic clock: the tests decide what "now" is, so
// staleness is exercised by moving time instead of sleeping.
type fakeQuotaClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeQuotaClock() *fakeQuotaClock { return &fakeQuotaClock{t: pollerTestStart} }

func (c *fakeQuotaClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeQuotaClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// quotaScriptStep is one scripted Fetch reply. The last step repeats once the
// script is exhausted, so a poll loop keeps running against it.
type quotaScriptStep struct {
	snapshot quota.Snapshot
	err      error
}

// scriptedQuotaFetcher is a pollFetcher that replays a script.
type scriptedQuotaFetcher struct {
	mu     sync.Mutex
	calls  int
	script []quotaScriptStep
}

func newScriptedFetcher(steps ...quotaScriptStep) *scriptedQuotaFetcher {
	return &scriptedQuotaFetcher{script: steps}
}

func successStep(fetchedAt time.Time) quotaScriptStep {
	return quotaScriptStep{snapshot: testQuotaSnapshot(fetchedAt)}
}

func failureStep(err error) quotaScriptStep { return quotaScriptStep{err: err} }

func (f *scriptedQuotaFetcher) Fetch(context.Context) (quota.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	step := f.script[len(f.script)-1]
	if f.calls < len(f.script) {
		step = f.script[f.calls-1]
	}
	return step.snapshot, step.err
}

func (f *scriptedQuotaFetcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// replaceLastStep swaps the repeating tail of the script, which is how a test
// turns a failing endpoint back into a healthy one.
func (f *scriptedQuotaFetcher) replaceLastStep(step quotaScriptStep) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.script[len(f.script)-1] = step
}

// capturingLogger records everything the poller and its client log, for the
// leak assertions.
type capturingLogger struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *capturingLogger) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(&l.buf, format, args...)
}

func (l *capturingLogger) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// waitForQuotaCondition polls condition until it holds or the one-second
// deadline passes. It is the same bounded-wait idiom the fairness tests use.
func waitForQuotaCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for quota poller condition")
}

// unsetQuotaEnv restores any of the named variables a test unsets, so env
// tests cannot leak state into the rest of the package.
func unsetQuotaEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		original, wasSet := os.LookupEnv(key)
		_ = os.Unsetenv(key)
		t.Cleanup(func() {
			if wasSet {
				_ = os.Setenv(key, original)
			}
		})
	}
}

func testQuotaSnapshot(fetchedAt time.Time) quota.Snapshot {
	return quota.Snapshot{
		FetchedAt: fetchedAt,
		PlanTier:  "pro",
		Windows: []quota.WindowState{
			{
				Window:       quota.WindowFiveHour,
				LimitType:    quota.LimitTypeCredit,
				ResetTime:    time.Unix(1788541838, 272000000).UTC(),
				Used:         740,
				Limit:        2000,
				Remaining:    1259,
				HasUsage:     true,
				UsedFraction: 0.37,
			},
			{
				Window:       quota.WindowWeekly,
				LimitType:    quota.LimitTypeTokens,
				UsedFraction: 0.11,
			},
		},
	}
}

func newTestPoller(t *testing.T, fetcher pollFetcher, clock *fakeQuotaClock, interval, staleAfter time.Duration) *QuotaPoller {
	t.Helper()
	poller, err := NewQuotaPoller(fetcher, interval, staleAfter, pollerTestVariant)
	if err != nil {
		t.Fatalf("NewQuotaPoller: %v", err)
	}
	poller.now = clock.Now
	return poller
}

// TestNewQuotaPollerRejectsInvalidSetup pins the constructor guards: a poller
// without a fetch source, or with a duration that cannot bound a loop, must
// not be built.
func TestNewQuotaPollerRejectsInvalidSetup(t *testing.T) {
	fetcher := newScriptedFetcher(successStep(pollerTestStart))

	tests := []struct {
		name       string
		fetcher    pollFetcher
		interval   time.Duration
		staleAfter time.Duration
		wantErr    string
	}{
		{name: "no fetch source", fetcher: nil, interval: time.Minute, staleAfter: 15 * time.Minute, wantErr: "fetch source"},
		{name: "zero interval", fetcher: fetcher, interval: 0, staleAfter: 15 * time.Minute, wantErr: "interval"},
		{name: "negative interval", fetcher: fetcher, interval: -time.Second, staleAfter: 15 * time.Minute, wantErr: "interval"},
		{name: "zero stale-after", fetcher: fetcher, interval: time.Minute, staleAfter: 0, wantErr: "stale-after"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			poller, err := NewQuotaPoller(tc.fetcher, tc.interval, tc.staleAfter, pollerTestVariant)
			if err == nil {
				t.Fatal("NewQuotaPoller accepted an invalid setup")
			}
			if poller != nil {
				t.Error("NewQuotaPoller returned a poller alongside its error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// TestQuotaHealthSectionReportsDisabled covers the disabled case: no poller
// is wired, and /health says so instead of omitting the section.
func TestQuotaHealthSectionReportsDisabled(t *testing.T) {
	arl := NewAdaptiveRateLimiter(8.0, 0.5, 40.0)

	rec := httptest.NewRecorder()
	newHealthHandler(arl, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("/health status = %d, want 200", rec.Code)
	}

	var payload struct {
		RateLimit RateLimitHealth `json:"rate_limit"`
		Quota     QuotaHealth     `json:"quota"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Failed to decode /health body %q: %v", rec.Body.String(), err)
	}
	if payload.Quota.Enabled {
		t.Error("/health quota.enabled = true without a poller, want false")
	}
	if payload.Quota.Fresh {
		t.Error("/health quota.fresh = true without a poller, want false")
	}
	if len(payload.Quota.Windows) != 0 {
		t.Errorf("/health quota carried %d windows without a poller, want none", len(payload.Quota.Windows))
	}
	// The disabled section must not crowd out the state probes already read.
	if payload.RateLimit.CurrentRate != 8.0 {
		t.Errorf("/health rate_limit.current_rate = %.2f, want 8.00", payload.RateLimit.CurrentRate)
	}
}

// TestQuotaPollerPublishesFreshSample covers the enabled-and-fresh case: one
// successful poll populates the health payload and the observation gauges.
func TestQuotaPollerPublishesFreshSample(t *testing.T) {
	resetQuotaMetrics()
	clock := newFakeQuotaClock()
	fetcher := newScriptedFetcher(successStep(clock.Now()))
	poller := newTestPoller(t, fetcher, clock, time.Minute, 15*time.Minute)

	poller.pollOnce(context.Background())

	health := poller.HealthState()
	if !health.Enabled {
		t.Error("HealthState() enabled = false, want true")
	}
	if !health.Fresh {
		t.Error("HealthState() fresh = false right after a successful poll")
	}
	if health.LastOutcome != quotaPollOutcomeSuccess {
		t.Errorf("HealthState() last_outcome = %q, want %q", health.LastOutcome, quotaPollOutcomeSuccess)
	}
	if health.PlanTier != "pro" {
		t.Errorf("HealthState() plan_tier = %q, want \"pro\"", health.PlanTier)
	}
	if health.Interval != time.Minute.String() {
		t.Errorf("HealthState() interval = %q, want %q", health.Interval, time.Minute.String())
	}
	if health.StaleAfter != (15 * time.Minute).String() {
		t.Errorf("HealthState() stale_after = %q, want %q", health.StaleAfter, (15 * time.Minute).String())
	}
	if want := pollerTestStart.UTC().Format(time.RFC3339); health.LastSuccessAt != want {
		t.Errorf("HealthState() last_success_at = %q, want %q", health.LastSuccessAt, want)
	}
	if health.SampleAgeSeconds != 0 {
		t.Errorf("HealthState() sample_age_seconds = %v, want 0", health.SampleAgeSeconds)
	}
	if len(health.Windows) != 2 {
		t.Fatalf("HealthState() carried %d windows, want 2", len(health.Windows))
	}

	fiveHour, weekly := health.Windows[0], health.Windows[1]
	if fiveHour.Window != string(quota.WindowFiveHour) || fiveHour.LimitType != string(quota.LimitTypeCredit) {
		t.Errorf("five-hour window = %s/%s, want five_hour/credit_limit", fiveHour.Window, fiveHour.LimitType)
	}
	if fiveHour.UsedFraction != 0.37 {
		t.Errorf("five-hour used_fraction = %v, want 0.37", fiveHour.UsedFraction)
	}
	if want := time.Unix(1788541838, 272000000).UTC().Format(time.RFC3339); fiveHour.ResetAt != want {
		t.Errorf("five-hour reset_at = %q, want %q", fiveHour.ResetAt, want)
	}
	if weekly.Window != string(quota.WindowWeekly) || weekly.LimitType != string(quota.LimitTypeTokens) {
		t.Errorf("weekly window = %s/%s, want weekly/tokens_limit", weekly.Window, weekly.LimitType)
	}
	if weekly.ResetAt != "" {
		t.Errorf("weekly reset_at = %q, want empty for a window with no advertised reset", weekly.ResetAt)
	}

	if got := testutil.ToFloat64(quotaUsageRatio.WithLabelValues("five_hour", "credit_limit", pollerTestVariant)); got != 0.37 {
		t.Errorf("usage ratio gauge = %v, want 0.37", got)
	}
	if got := testutil.ToFloat64(quotaRemainingRatio.WithLabelValues("five_hour", "credit_limit", pollerTestVariant)); got != 0.63 {
		t.Errorf("remaining ratio gauge = %v, want 0.63", got)
	}
	if got := testutil.ToFloat64(quotaResetTimeSeconds.WithLabelValues("five_hour", "credit_limit", pollerTestVariant)); got != 1788541838 {
		t.Errorf("reset time gauge = %v, want 1788541838", got)
	}
	if got := testutil.ToFloat64(quotaPollsTotal.WithLabelValues(quotaPollOutcomeSuccess, pollerTestVariant)); got != 1 {
		t.Errorf("success polls = %v, want 1", got)
	}
	if got := testutil.ToFloat64(quotaSampleAgeSeconds.WithLabelValues(pollerTestVariant)); got != 0 {
		t.Errorf("sample age gauge = %v, want 0 right after a successful poll", got)
	}
	if got := testutil.CollectAndCount(quotaSampleAgeSeconds); got != 1 {
		t.Errorf("one poll created %d sample-age series, want 1", got)
	}
	if got := testutil.CollectAndCount(quotaPollsTotal); got != 1 {
		t.Errorf("one poll created %d outcome series, want 1", got)
	}

	// Observe-only: a fresh, healthy sample is not an admission decision. The
	// rate-cap and gate gauges describe enforcement, which is a separately
	// gated change, so a successful observation must not create their series.
	for name, collector := range map[string]prometheus.Collector{
		"quota_rate_cap":  quotaRateCap,
		"quota_gate_open": quotaGateOpen,
	} {
		if got := testutil.CollectAndCount(collector); got != 0 {
			t.Errorf("%s recorded %d series from an observe-only sample, want none", name, got)
		}
	}
}

// TestQuotaPollerKeepsLastKnownGoodAcrossFailures verifies a failed poll
// never destroys the retained sample: health keeps describing it, the sample
// ages, and the window gauges stay at the last-known-good values.
func TestQuotaPollerKeepsLastKnownGoodAcrossFailures(t *testing.T) {
	resetQuotaMetrics()
	clock := newFakeQuotaClock()
	fetcher := newScriptedFetcher(
		successStep(clock.Now()),
		failureStep(errors.New("quota poll returned unexpected status 503")),
		failureStep(errors.New("quota poll failed: connection refused")),
	)
	poller := newTestPoller(t, fetcher, clock, time.Minute, 15*time.Minute)

	poller.pollOnce(context.Background())
	clock.Advance(3 * time.Minute)
	poller.pollOnce(context.Background())
	clock.Advance(4 * time.Minute)
	poller.pollOnce(context.Background())

	health := poller.HealthState()
	if !health.Fresh {
		t.Error("HealthState() fresh = false while the retained sample is inside stale-after")
	}
	if health.LastOutcome != quotaPollOutcomeError {
		t.Errorf("HealthState() last_outcome = %q, want %q", health.LastOutcome, quotaPollOutcomeError)
	}
	if want := pollerTestStart.UTC().Format(time.RFC3339); health.LastSuccessAt != want {
		t.Errorf("HealthState() last_success_at = %q, want the first success %q", health.LastSuccessAt, want)
	}
	if health.SampleAgeSeconds != (7 * time.Minute).Seconds() {
		t.Errorf("HealthState() sample_age_seconds = %v, want %v", health.SampleAgeSeconds, (7 * time.Minute).Seconds())
	}
	if len(health.Windows) != 2 {
		t.Errorf("HealthState() carried %d windows after failures, want the retained 2", len(health.Windows))
	}

	if got := testutil.ToFloat64(quotaUsageRatio.WithLabelValues("five_hour", "credit_limit", pollerTestVariant)); got != 0.37 {
		t.Errorf("usage ratio gauge = %v, want the retained 0.37", got)
	}
	if got := testutil.ToFloat64(quotaPollsTotal.WithLabelValues(quotaPollOutcomeSuccess, pollerTestVariant)); got != 1 {
		t.Errorf("success polls = %v, want 1", got)
	}
	if got := testutil.ToFloat64(quotaPollsTotal.WithLabelValues(quotaPollOutcomeError, pollerTestVariant)); got != 2 {
		t.Errorf("error polls = %v, want 2", got)
	}
	if got := testutil.CollectAndCount(quotaPollsTotal); got != 2 {
		t.Errorf("success and error polls created %d outcome series, want 2", got)
	}
}

// TestQuotaPollerReportsStaleAfterDuration verifies the stale boundary: past
// stale-after the sample is no longer fresh, the transition is counted once,
// and the retained sample survives for operators to read.
func TestQuotaPollerReportsStaleAfterDuration(t *testing.T) {
	resetQuotaMetrics()
	clock := newFakeQuotaClock()
	fetcher := newScriptedFetcher(
		successStep(clock.Now()),
		failureStep(errors.New("quota poll failed: dial timeout")),
	)
	poller := newTestPoller(t, fetcher, clock, time.Minute, 15*time.Minute)

	poller.pollOnce(context.Background())
	// The stale-after boundary itself is still fresh (inclusive); one second
	// past it the retained sample is no longer trusted.
	clock.Advance(15*time.Minute + time.Second)
	poller.pollOnce(context.Background())
	if health := poller.HealthState(); health.Fresh {
		t.Error("HealthState() fresh = true past stale-after, want false")
	}

	// Further failed polls must not count staleness again.
	for i := 0; i < 3; i++ {
		clock.Advance(time.Minute)
		poller.pollOnce(context.Background())
	}

	if got := testutil.ToFloat64(quotaPollsTotal.WithLabelValues(quotaPollOutcomeStale, pollerTestVariant)); got != 1 {
		t.Errorf("stale polls = %v, want the transition counted once", got)
	}
	if got := testutil.ToFloat64(quotaPollsTotal.WithLabelValues(quotaPollOutcomeError, pollerTestVariant)); got != 4 {
		t.Errorf("error polls = %v, want 4", got)
	}

	health := poller.HealthState()
	if health.Fresh {
		t.Error("HealthState() fresh = true after going stale, want false")
	}
	if len(health.Windows) != 2 {
		t.Errorf("HealthState() carried %d windows while stale, want the retained 2", len(health.Windows))
	}

	// The next success must bring the sample back to fresh.
	fetcher.replaceLastStep(successStep(clock.Now()))
	poller.pollOnce(context.Background())
	if health := poller.HealthState(); !health.Fresh {
		t.Error("HealthState() fresh = false after a recovery poll, want true")
	}
	if got := testutil.ToFloat64(quotaPollsTotal.WithLabelValues(quotaPollOutcomeStale, pollerTestVariant)); got != 1 {
		t.Errorf("stale polls after recovery = %v, want still 1", got)
	}
}

// TestQuotaPollerClassifiesMalformedPayload drives the real client against a
// 200-with-garbage response and pins the outcome to "malformed" rather than
// "error", so payload rot is distinguishable from an endpoint outage.
func TestQuotaPollerClassifiesMalformedPayload(t *testing.T) {
	resetQuotaMetrics()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(truncatedQuotaPayload))
	}))
	defer server.Close()

	client, err := quota.NewClient(fakeCredential, server.URL, quota.WithTimeout(time.Second))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	poller, err := NewQuotaPoller(client, time.Minute, 15*time.Minute, pollerTestVariant)
	if err != nil {
		t.Fatalf("NewQuotaPoller: %v", err)
	}

	poller.pollOnce(context.Background())

	if got := testutil.ToFloat64(quotaPollsTotal.WithLabelValues(quotaPollOutcomeMalformed, pollerTestVariant)); got != 1 {
		t.Errorf("malformed polls = %v, want 1", got)
	}
	// No successful poll has ever happened, so the poller is also reporting
	// that it has no trusted sample: exactly the stale and malformed series.
	if got := testutil.ToFloat64(quotaPollsTotal.WithLabelValues(quotaPollOutcomeStale, pollerTestVariant)); got != 1 {
		t.Errorf("stale polls = %v, want 1 with no sample yet", got)
	}
	if got := testutil.CollectAndCount(quotaPollsTotal); got != 2 {
		t.Errorf("a malformed payload created %d outcome series, want 2 (malformed and stale)", got)
	}
	if got := testutil.CollectAndCount(quotaUsageRatio); got != 0 {
		t.Errorf("a malformed payload produced %d usage-ratio series, want none", got)
	}
	// The age gauge describes the last valid sample; with no sample yet it
	// must stay absent rather than read as a fresh sample at age zero.
	if got := testutil.CollectAndCount(quotaSampleAgeSeconds); got != 0 {
		t.Errorf("a malformed payload produced %d sample-age series, want none", got)
	}

	health := poller.HealthState()
	if health.LastOutcome != quotaPollOutcomeMalformed {
		t.Errorf("HealthState() last_outcome = %q, want %q", health.LastOutcome, quotaPollOutcomeMalformed)
	}
	if health.Fresh {
		t.Error("HealthState() fresh = true with no sample at all, want false")
	}
	if len(health.Windows) != 0 {
		t.Errorf("HealthState() carried %d windows with no sample, want none", len(health.Windows))
	}
}

// TestQuotaPollerClassifiesProviderRejectionAndTransportFailure pins the rest
// of the outcome enum: a structured business rejection and a transport
// failure are both "error", never "malformed".
func TestQuotaPollerClassifiesProviderRejectionAndTransportFailure(t *testing.T) {
	resetQuotaMetrics()
	var requests int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(providerRejectedPayload))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := quota.NewClient(fakeCredential, server.URL, quota.WithTimeout(time.Second))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	poller, err := NewQuotaPoller(client, time.Minute, 15*time.Minute, pollerTestVariant)
	if err != nil {
		t.Fatalf("NewQuotaPoller: %v", err)
	}

	poller.pollOnce(context.Background())
	poller.pollOnce(context.Background())

	if got := testutil.ToFloat64(quotaPollsTotal.WithLabelValues(quotaPollOutcomeError, pollerTestVariant)); got != 2 {
		t.Errorf("error polls = %v, want 2", got)
	}
	if got := testutil.ToFloat64(quotaPollsTotal.WithLabelValues(quotaPollOutcomeMalformed, pollerTestVariant)); got != 0 {
		t.Errorf("malformed polls = %v, want 0 for a credential rejection and a 5xx", got)
	}
}

// TestClassifyQuotaPollError pins the outcome mapping. The missing-limits
// case matters most: the quota package's shape error carries no sentinel, so
// the classifier matches its message prefix — checked here against the error
// that package actually produces, so a wording change upstream fails here
// first.
func TestClassifyQuotaPollError(t *testing.T) {
	_, missingLimitsErr := quota.Normalize([]byte(`{"success":true,"code":200,"data":{}}`), pollerTestStart)
	if missingLimitsErr == nil {
		t.Fatal("Normalize accepted a body with no data.limits array")
	}
	_, jsonErr := quota.Normalize([]byte(truncatedQuotaPayload), pollerTestStart)
	if jsonErr == nil {
		t.Fatal("Normalize accepted a truncated body")
	}

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "no error", err: nil, want: quotaPollOutcomeSuccess},
		{name: "provider rejection", err: &quota.ProviderError{Code: 401}, want: quotaPollOutcomeError},
		{name: "read budget refused", err: fmt.Errorf("wrapped: %w", quota.ErrResponseTooLarge), want: quotaPollOutcomeError},
		{name: "truncated body", err: jsonErr, want: quotaPollOutcomeMalformed},
		{name: "body with no limits array", err: missingLimitsErr, want: quotaPollOutcomeMalformed},
		{name: "transport timeout", err: fmt.Errorf("quota poll failed: %w", context.DeadlineExceeded), want: quotaPollOutcomeError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyQuotaPollError(tc.err); got != tc.want {
				t.Errorf("classifyQuotaPollError() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestQuotaPollerTimeoutDoesNotBlockAdmission is the observe-only contract:
// while every poll times out against a hung endpoint, requests are admitted
// at the configured rate and the limiter's state never moves.
func TestQuotaPollerTimeoutDoesNotBlockAdmission(t *testing.T) {
	resetQuotaMetrics()
	// Accepts the connection and never answers within the poll budget.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	client, err := quota.NewClient(fakeCredential, server.URL, quota.WithTimeout(15*time.Millisecond))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	poller, err := NewQuotaPoller(client, 2*time.Millisecond, time.Minute, pollerTestVariant)
	if err != nil {
		t.Fatalf("NewQuotaPoller: %v", err)
	}

	pollCtx, cancelPolling := context.WithCancel(context.Background())
	defer cancelPolling()
	pollErrors := make(chan error, 1)
	go func() { pollErrors <- poller.Run(pollCtx) }()

	waitForQuotaCondition(t, func() bool {
		return testutil.ToFloat64(quotaPollsTotal.WithLabelValues(quotaPollOutcomeError, pollerTestVariant)) >= 5
	})

	arl := NewAdaptiveRateLimiter(25, 1, 50)
	rateBefore := arl.GetCurrentRate()
	ceilingBefore := arl.HealthState().Ceiling

	start := time.Now()
	arl.Wait(pollerTestVariant)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("admission took %s while quota polls were failing, want it unblocked", elapsed)
	}
	if arl.GetCurrentRate() != rateBefore {
		t.Errorf("current rate moved to %.2f while polls failed, want %.2f", arl.GetCurrentRate(), rateBefore)
	}
	if ceiling := arl.HealthState().Ceiling; ceiling != ceilingBefore {
		t.Errorf("estimated ceiling moved to %.2f while polls failed, want %.2f", ceiling, ceilingBefore)
	}

	cancelPolling()
	select {
	case err := <-pollErrors:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run() returned %v after cancellation, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not return after cancellation")
	}
}

// TestQuotaPollerStopsOnContextCancel verifies the lifecycle: Run polls
// immediately, keeps the cadence, and stops fetching for good once its
// context is cancelled.
func TestQuotaPollerStopsOnContextCancel(t *testing.T) {
	fetcher := newScriptedFetcher(successStep(pollerTestStart))
	poller := newTestPoller(t, fetcher, newFakeQuotaClock(), 5*time.Millisecond, 15*time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- poller.Run(ctx) }()

	waitForQuotaCondition(t, func() bool { return fetcher.callCount() >= 3 })
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run() returned %v after cancellation, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not return after cancellation")
	}

	callsAtCancel := fetcher.callCount()
	// One interval is long gone by now; a poller that kept its ticker alive
	// would have fetched again in this window.
	time.Sleep(20 * time.Millisecond)
	if got := fetcher.callCount(); got != callsAtCancel {
		t.Errorf("Fetch ran %d times after cancellation, want %d", got, callsAtCancel)
	}
}

// TestQuotaPollerNeverLogsCredential drives a successful poll, a provider
// rejection, and a malformed payload through the real client with a
// capturing logger, and asserts neither the credential nor the
// account-identifying provider message text appears in the output.
func TestQuotaPollerNeverLogsCredential(t *testing.T) {
	resetQuotaMetrics()
	var requests int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests++
		switch requests {
		case 1:
			_, _ = w.Write([]byte(validQuotaPayload))
		case 2:
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(providerRejectedPayload))
		default:
			_, _ = w.Write([]byte(truncatedQuotaPayload))
		}
	}))
	defer server.Close()

	logger := &capturingLogger{}
	client, err := quota.NewClient(fakeCredential, server.URL, quota.WithTimeout(time.Second), quota.WithLogger(logger))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	poller, err := NewQuotaPoller(client, time.Minute, 15*time.Minute, pollerTestVariant)
	if err != nil {
		t.Fatalf("NewQuotaPoller: %v", err)
	}

	for i := 0; i < 3; i++ {
		poller.pollOnce(context.Background())
	}

	logged := logger.String()
	if logged == "" {
		t.Fatal("the poll produced no log output; the leak check verified nothing")
	}
	if strings.Contains(logged, fakeCredential) {
		t.Error("the credential reached the poll's log output")
	}
	if strings.Contains(logged, "acct:marker-7f3a9") {
		t.Error("provider message text reached the poll's log output")
	}
	if strings.Contains(logged, "Authorization") {
		t.Error("the authenticated request reached the poll's log output")
	}
}

// TestQuotaPollConfigDefaults verifies the configuration defaults: polling is
// off, and every duration lands on a value that can bound its loop.
func TestQuotaPollConfigDefaults(t *testing.T) {
	unsetQuotaEnv(t,
		"QUOTA_POLL_ENABLED",
		"QUOTA_POLL_INTERVAL",
		quota.EnvTimeout,
		"QUOTA_STALE_AFTER",
		quota.EnvBaseURL,
	)

	if config.GetQuotaPollEnabled() {
		t.Error("GetQuotaPollEnabled() = true by default, want false until a deployment opts in")
	}
	if got, want := config.GetQuotaPollInterval(), config.DefaultQuotaPollInterval; got != want {
		t.Errorf("GetQuotaPollInterval() = %s, want %s", got, want)
	}
	if got, want := config.GetQuotaPollTimeout(), config.DefaultQuotaPollTimeout; got != want {
		t.Errorf("GetQuotaPollTimeout() = %s, want %s", got, want)
	}
	if got, want := config.GetQuotaStaleAfter(), config.DefaultQuotaStaleAfter; got != want {
		t.Errorf("GetQuotaStaleAfter() = %s, want %s", got, want)
	}
	if got, want := config.GetQuotaBaseURL(), quota.DefaultBaseURL; got != want {
		t.Errorf("GetQuotaBaseURL() = %q, want %q", got, want)
	}
}

// TestQuotaPollConfigOverrides verifies the documented overrides, and that an
// unusable duration falls back to its default instead of reaching the poller.
func TestQuotaPollConfigOverrides(t *testing.T) {
	t.Setenv("QUOTA_POLL_ENABLED", "true")
	t.Setenv("QUOTA_POLL_INTERVAL", "45s")
	t.Setenv(quota.EnvTimeout, "2s")
	t.Setenv("QUOTA_STALE_AFTER", "1h")
	t.Setenv(quota.EnvBaseURL, "https://quota.example.internal")

	if !config.GetQuotaPollEnabled() {
		t.Error("GetQuotaPollEnabled() = false with QUOTA_POLL_ENABLED=true")
	}
	if got := config.GetQuotaPollInterval(); got != 45*time.Second {
		t.Errorf("GetQuotaPollInterval() = %s, want 45s", got)
	}
	if got := config.GetQuotaPollTimeout(); got != 2*time.Second {
		t.Errorf("GetQuotaPollTimeout() = %s, want 2s", got)
	}
	if got := config.GetQuotaStaleAfter(); got != time.Hour {
		t.Errorf("GetQuotaStaleAfter() = %s, want 1h", got)
	}
	if got := config.GetQuotaBaseURL(); got != "https://quota.example.internal" {
		t.Errorf("GetQuotaBaseURL() = %q, want the override", got)
	}

	// A non-positive duration would spin the poll loop or empty its budget.
	t.Setenv("QUOTA_POLL_INTERVAL", "0s")
	t.Setenv(quota.EnvTimeout, "-3s")
	t.Setenv("QUOTA_STALE_AFTER", "not-a-duration")
	if got := config.GetQuotaPollInterval(); got != config.DefaultQuotaPollInterval {
		t.Errorf("GetQuotaPollInterval() = %s for a zero override, want the default", got)
	}
	if got := config.GetQuotaPollTimeout(); got != config.DefaultQuotaPollTimeout {
		t.Errorf("GetQuotaPollTimeout() = %s for a negative override, want the default", got)
	}
	if got := config.GetQuotaStaleAfter(); got != config.DefaultQuotaStaleAfter {
		t.Errorf("GetQuotaStaleAfter() = %s for an unparsable override, want the default", got)
	}
}

// TestNewQuotaPollerFromConfigWiresKnobs verifies the configuration-to-poller
// wiring: the built poller carries the configured cadence and staleness.
func TestNewQuotaPollerFromConfigWiresKnobs(t *testing.T) {
	t.Setenv("QUOTA_POLL_INTERVAL", "30s")
	t.Setenv("QUOTA_STALE_AFTER", "10m")

	poller, err := newQuotaPollerFromConfig(fakeCredential, pollerTestVariant)
	if err != nil {
		t.Fatalf("newQuotaPollerFromConfig: %v", err)
	}
	if poller.interval != 30*time.Second {
		t.Errorf("poller interval = %s, want 30s", poller.interval)
	}
	if poller.staleAfter != 10*time.Minute {
		t.Errorf("poller stale-after = %s, want 10m", poller.staleAfter)
	}
	if poller.variant != pollerTestVariant {
		t.Errorf("poller variant = %q, want %q", poller.variant, pollerTestVariant)
	}
	if poller.fetcher == nil {
		t.Error("poller has no fetch source")
	}

	// An empty credential must be refused, not silently polled.
	if _, err := newQuotaPollerFromConfig("", pollerTestVariant); err == nil {
		t.Error("newQuotaPollerFromConfig accepted an empty credential")
	}
}
