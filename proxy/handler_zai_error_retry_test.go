package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// These tests pin how ServeHTTP turns each bounded Z.AI business-error class
// into upstream attempts, rate-limiter admissions, and a final response. The
// contract under test, per docs/plan/plan.md:
//
//	frequency (1303, 1305) and unrecognized 429 bodies feed the
//	requests-per-second controller; everything else must leave the learned
//	ceiling exactly where it was.
//	concurrency (1302) retries on the plain curve without feeding that
//	controller; model congestion (1312) retries on a jittered curve, also
//	without feeding it.
//	quota (1308, 1310) never retries: the final upstream 429, its body, and
//	its reset metadata go back to the caller untouched.
//	every retry reacquires admission before its upstream attempt, and every
//	pre-retry wait is cancellable.

// classRetryFixture wires one ProxyHandler against one counting upstream.
type classRetryFixture struct {
	upstreamCalls atomic.Int32
	server        *httptest.Server
	handler       *ProxyHandler
	sleeps        []time.Duration
}

// newClassRetryFixture builds a handler whose limiter starts and stays at
// 100 req/s unless something feeds the controller: initialRate == maxRate
// means any Record429 walks the rate strictly below 100, and nothing else
// moves it. adjustmentWindow is zero so each Record429 adjusts immediately
// and deterministically inside a single-request test. retrySleep records the
// chosen delays instead of waiting them out.
func newClassRetryFixture(t *testing.T, name string, maxRetries int, respond func(w http.ResponseWriter, call int)) *classRetryFixture {
	t.Helper()
	f := &classRetryFixture{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respond(w, int(f.upstreamCalls.Add(1)))
	}))
	t.Cleanup(f.server.Close)

	handler := NewProxyHandler(
		"test-key", f.server.URL, maxRetries, 0, "class-"+name, nil, "glm-4",
		100, 1, 100,
	)
	handler.rateLimiter.adjustmentWindow = 0
	handler.retryJitter = func() float64 { return 0 }
	handler.retrySleep = func(delay time.Duration) {
		f.sleeps = append(f.sleeps, delay)
	}
	f.handler = handler
	return f
}

func (f *classRetryFixture) do(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(createNonStreamingRequestBody()))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

// rateLimitAdmissions counts the admissions the limiter granted to variant:
// one observation on zai_proxy_rate_limit_wait_seconds per waitForClient.
func rateLimitAdmissions(t *testing.T, variant string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gathering metrics: %v", err)
	}
	var count float64
	for _, mf := range families {
		if mf.GetName() != "zai_proxy_rate_limit_wait_seconds" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "variant" && lp.GetValue() == variant {
					count += float64(m.GetHistogram().GetSampleCount())
					break
				}
			}
		}
	}
	return count
}

func assertClassRetrySleeps(t *testing.T, got, want []time.Duration) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("retry sleeps = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("retry sleep %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// zaiQuotaBody builds a 1308-style body with a structured reset_time epoch.
func zaiQuotaBody(code int, resetAt time.Time) string {
	return fmt.Sprintf(`{"code":%d,"msg":"bandwidth limit exceeded","reset_time":%d}`, code, resetAt.Unix())
}

// zaiQuotaMessageBody builds a 1310-style body whose reset is only advertised
// inside the message text, as Z.AI's documented template does.
func zaiQuotaMessageBody(code int, resetAt time.Time) string {
	stamp := resetAt.In(zaiProviderLocation).Format("2006-01-02 15:04:05")
	return fmt.Sprintf(`{"code":%d,"msg":"Your limit will reset at %s"}`, code, stamp)
}

func TestZaiErrorClassRetryContract(t *testing.T) {
	hourAhead := time.Now().Add(time.Hour).Truncate(time.Second)

	tests := []struct {
		name        string
		upstreamBod func() string
		// retryAfterHeader, when set, rides on every upstream 429.
		retryAfterHeader string
		wantAttempts     int32
		wantAdmissions   int32
		wantSleeps       []time.Duration
		wantClass        string
		wantCode         string
		wantRateDropped  bool
		// wantRetryAfterRange bounds a Retry-After the proxy derives for the
		// caller; zero bounds mean no derived header is expected.
		wantRetryAfterMin int
		wantRetryAfterMax int
	}{
		{
			name: "frequency_1303_feeds_rps_controller_on_jittered_curve",
			upstreamBod: func() string {
				return `{"error":{"code":"1303","message":"Too many requests"}}`
			},
			wantAttempts:    3,
			wantAdmissions:  3,
			wantSleeps:      []time.Duration{500 * time.Millisecond, time.Second},
			wantClass:       "frequency",
			wantCode:        "1303",
			wantRateDropped: true,
		},
		{
			name: "frequency_1305_feeds_rps_controller_on_jittered_curve",
			upstreamBod: func() string {
				return `{"code":1305,"msg":"Too many requests"}`
			},
			wantAttempts:    3,
			wantAdmissions:  3,
			wantSleeps:      []time.Duration{500 * time.Millisecond, time.Second},
			wantClass:       "frequency",
			wantCode:        "1305",
			wantRateDropped: true,
		},
		{
			name: "concurrency_1302_retries_on_plain_curve_without_feeding_rps",
			upstreamBod: func() string {
				return `{"error":{"code":"1302","message":"Concurrent slots full"}}`
			},
			wantAttempts:   3,
			wantAdmissions: 3,
			// The plain 1s/2s curve, un-jittered: 1302 holds concurrency, it
			// is not per-second pressure.
			wantSleeps: []time.Duration{time.Second, 2 * time.Second},
			wantClass:  "concurrency",
			wantCode:   "1302",
		},
		{
			name: "model_congestion_1312_jitters_without_feeding_rps",
			upstreamBod: func() string {
				return `{"code":1312,"msg":"Model busy, try again later"}`
			},
			wantAttempts:   3,
			wantAdmissions: 3,
			wantSleeps:     []time.Duration{500 * time.Millisecond, time.Second},
			wantClass:      "model_congestion",
			wantCode:       "1312",
		},
		{
			name: "quota_1308_never_retries_and_derives_retry_after_from_reset",
			upstreamBod: func() string {
				return zaiQuotaBody(1308, hourAhead)
			},
			wantAttempts:   1,
			wantAdmissions: 1,
			wantClass:      "quota",
			wantCode:       "1308",
			// The advertised reset is a full hour out; the derived Retry-After
			// must sit just under it (ceil of the remaining time, which the
			// round-trips shave by a little).
			wantRetryAfterMin: 3500,
			wantRetryAfterMax: 3600,
		},
		{
			name: "quota_1310_never_retries_and_reads_reset_from_message_text",
			upstreamBod: func() string {
				return zaiQuotaMessageBody(1310, hourAhead)
			},
			wantAttempts:      1,
			wantAdmissions:    1,
			wantClass:         "quota",
			wantCode:          "1310",
			wantRetryAfterMin: 3500,
			wantRetryAfterMax: 3600,
		},
		{
			name: "unknown_429_conservatively_feeds_rps_controller",
			upstreamBod: func() string {
				return `{"error":{"code":"9999","message":"Something else"}}`
			},
			wantAttempts:    3,
			wantAdmissions:  3,
			wantSleeps:      []time.Duration{time.Second, 2 * time.Second},
			wantClass:       "unknown",
			wantCode:        "9999",
			wantRateDropped: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.upstreamBod()
			f := newClassRetryFixture(t, tc.name, 2, func(w http.ResponseWriter, _ int) {
				if tc.retryAfterHeader != "" {
					w.Header().Set("Retry-After", tc.retryAfterHeader)
				}
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(body))
			})

			admissionsBefore := rateLimitAdmissions(t, f.handler.deploymentVariant)

			rec := f.do(t)

			if got := f.upstreamCalls.Load(); got != tc.wantAttempts {
				t.Errorf("upstream attempts = %d, want %d", got, tc.wantAttempts)
			}
			if got := rateLimitAdmissions(t, f.handler.deploymentVariant) - admissionsBefore; got != float64(tc.wantAdmissions) {
				t.Errorf("limiter admissions = %v, want %d (every attempt, and only an attempt, may be admitted)", got, tc.wantAdmissions)
			}
			assertClassRetrySleeps(t, f.sleeps, tc.wantSleeps)

			if rec.Code != http.StatusTooManyRequests {
				t.Errorf("final status = %d, want 429; body=%q", rec.Code, rec.Body.String())
			}
			if got := rec.Body.String(); got != body {
				t.Errorf("final body = %q, want the upstream body preserved verbatim: %q", got, body)
			}
			if got := rec.Header().Get("X-Zai-Error-Class"); got != tc.wantClass {
				t.Errorf("X-Zai-Error-Class = %q, want %q", got, tc.wantClass)
			}
			if got := rec.Header().Get("X-Zai-Error-Code"); got != tc.wantCode {
				t.Errorf("X-Zai-Error-Code = %q, want %q", got, tc.wantCode)
			}

			rate := f.handler.rateLimiter.GetCurrentRate()
			if tc.wantRateDropped {
				if rate >= 100 {
					t.Errorf("rate = %v after frequency feedback, want it walked below the 100 req/s start", rate)
				}
			} else {
				if rate != 100 {
					t.Errorf("rate = %v, want exactly the 100 req/s start (class must not feed the RPS controller)", rate)
				}
				if ceiling := f.handler.rateLimiter.estimatedCeiling; ceiling != 100 {
					t.Errorf("estimated ceiling = %v, want the unpoisoned 100 req/s", ceiling)
				}
			}

			if tc.wantRetryAfterMax > 0 {
				header := rec.Header().Get("Retry-After")
				seconds, err := strconv.Atoi(header)
				if err != nil {
					t.Fatalf("Retry-After = %q, want derived seconds: %v", header, err)
				}
				if seconds < tc.wantRetryAfterMin || seconds > tc.wantRetryAfterMax {
					t.Errorf("Retry-After = %d, want within [%d, %d]", seconds, tc.wantRetryAfterMin, tc.wantRetryAfterMax)
				}
				reset := rec.Header().Get("X-Zai-Rate-Limit-Reset")
				if _, err := time.Parse(time.RFC3339, reset); err != nil {
					t.Errorf("X-Zai-Rate-Limit-Reset = %q, want RFC3339: %v", reset, err)
				}
			}
		})
	}
}

// TestZaiErrorClassHonorsAdvertisedRetryAfter pins that a classified retry
// waits once, for the largest advertised hint, instead of stacking a header
// wait on top of the backoff curve — and that the final response still hands
// the caller the upstream's own Retry-After rather than a derived one.
func TestZaiErrorClassHonorsAdvertisedRetryAfter(t *testing.T) {
	body := `{"error":{"code":"1303","message":"Too many requests","retry_after":9}}`
	f := newClassRetryFixture(t, "advertised-retry-after", 1, func(w http.ResponseWriter, _ int) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(body))
	})

	rec := f.do(t)

	if got := f.upstreamCalls.Load(); got != 2 {
		t.Errorf("upstream attempts = %d, want 2", got)
	}
	if got := rateLimitAdmissions(t, f.handler.deploymentVariant); got != 2 {
		t.Errorf("limiter admissions = %v, want 2", got)
	}
	// max(jittered 1s backoff = 0.5s, header 7s, body 9s): one 9s wait.
	assertClassRetrySleeps(t, f.sleeps, []time.Duration{9 * time.Second})

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("final status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "7" {
		t.Errorf("final Retry-After = %q, want the upstream header preserved", got)
	}
}

// TestZaiErrorQuotaResponsePreservesUpstreamRetryAfter pins that an explicit
// upstream Retry-After on a non-retryable quota 429 reaches the caller
// untouched instead of being replaced by a reset-derived value.
func TestZaiErrorQuotaResponsePreservesUpstreamRetryAfter(t *testing.T) {
	body := zaiQuotaBody(1308, time.Now().Add(time.Hour))
	f := newClassRetryFixture(t, "quota-explicit-retry-after", 2, func(w http.ResponseWriter, _ int) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(body))
	})

	rec := f.do(t)

	if got := f.upstreamCalls.Load(); got != 1 {
		t.Errorf("upstream attempts = %d, want 1 (quota windows outlive any retry budget)", got)
	}
	if got := rec.Header().Get("Retry-After"); got != "5" {
		t.Errorf("final Retry-After = %q, want the upstream header preserved", got)
	}
	if got := rec.Body.String(); got != body {
		t.Errorf("final body = %q, want the upstream body preserved verbatim: %q", got, body)
	}
}

// TestZaiErrorOversizedBodyClassifiesUnknownAndReplaysBoundedBytes pins the
// boundary between classification and replay. A 429 body past the parse
// budget is unreadable to the classifier, so it must take the conservative
// unknown-class path (feed the controller, retry) -- and the caller must
// receive exactly the bytes that were classified, not the extra sentinel byte
// the reader used only to detect oversize.
func TestZaiErrorOversizedBodyClassifiesUnknownAndReplaysBoundedBytes(t *testing.T) {
	// The code sits past the budget, so truncation genuinely loses it and the
	// body stops being valid JSON at exactly the budget boundary.
	pad := DefaultMaxZaiErrorBodyBytes + 64
	body := fmt.Sprintf(`{"pad":"%s","code":"1303"}`, strings.Repeat("x", pad))
	if len(body) <= DefaultMaxZaiErrorBodyBytes {
		t.Fatalf("test body is %d bytes, want past the %d-byte budget", len(body), DefaultMaxZaiErrorBodyBytes)
	}

	f := newClassRetryFixture(t, "oversized-body", 2, func(w http.ResponseWriter, _ int) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(body))
	})

	rateBefore := f.handler.rateLimiter.GetCurrentRate()
	rec := f.do(t)

	if got := f.upstreamCalls.Load(); got != 3 {
		t.Errorf("upstream attempts = %d, want 3 (oversize is unreadable, so the class is conservative and retries)", got)
	}
	if got := f.handler.rateLimiter.GetCurrentRate(); got >= rateBefore {
		t.Errorf("rate = %v before %v, want the unknown class to have fed the controller", got, rateBefore)
	}
	if got := rec.Header().Get("X-Zai-Error-Class"); got != "unknown" {
		t.Errorf("X-Zai-Error-Class = %q, want %q", got, "unknown")
	}
	if got := rec.Body.Len(); got != DefaultMaxZaiErrorBodyBytes {
		t.Errorf("replayed body = %d bytes, want exactly the %d classified bytes (no oversize sentinel)", got, DefaultMaxZaiErrorBodyBytes)
	}
	if rec.Body.String() == body {
		t.Errorf("replayed body equals the oversize upstream body; want it bounded to the classified prefix")
	}
}

// TestZaiErrorRetryWaitAbandonsOnClientCancellation pins that pre-retry
// waits are cancellable: a caller that goes away during a wait must not get
// another upstream attempt, another admission, or a handler stuck waiting.
func TestZaiErrorRetryWaitAbandonsOnClientCancellation(t *testing.T) {
	t.Run("unknown_429_during_retry_after_wait", func(t *testing.T) {
		f := newClassRetryFixture(t, "cancel-retry-after", 2, func(w http.ResponseWriter, _ int) {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":"9999","message":"slow down"}}`))
		})
		// Production wait path: no injected sleep, the cancellable timer.
		f.handler.retrySleep = nil

		assertCancelledWaitAbandons(t, f, 150*time.Millisecond, 1)
	})

	t.Run("frequency_429_during_classified_wait", func(t *testing.T) {
		f := newClassRetryFixture(t, "cancel-classified", 2, func(w http.ResponseWriter, _ int) {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":"1303","message":"Too many requests"}}`))
		})
		f.handler.retrySleep = nil

		assertCancelledWaitAbandons(t, f, 150*time.Millisecond, 1)
	})

	t.Run("unknown_429_during_later_backoff_wait", func(t *testing.T) {
		f := newClassRetryFixture(t, "cancel-backoff", 2, func(w http.ResponseWriter, _ int) {
			w.WriteHeader(http.StatusTooManyRequests)
		})
		f.handler.retrySleep = nil

		// Real waits: 1s, then 2s. Cancelling at 1.2s lands inside the second
		// wait, so exactly one retry happens and the third attempt never does.
		assertCancelledWaitAbandons(t, f, 1200*time.Millisecond, 2)
	})
}

// assertCancelledWaitAbandons runs one request whose context is cancelled
// after delay and asserts the handler gave up at wantAttempts: no admission
// and no upstream attempt beyond that point, well before the waits elapse.
func assertCancelledWaitAbandons(t *testing.T, f *classRetryFixture, cancelAfter time.Duration, wantAttempts int32) {
	t.Helper()

	attemptsBefore := f.upstreamCalls.Load()
	admissionsBefore := rateLimitAdmissions(t, f.handler.deploymentVariant)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(createNonStreamingRequestBody()))
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)

	timer := time.AfterFunc(cancelAfter, cancel)
	defer timer.Stop()

	start := time.Now()
	done := make(chan struct{})
	rec := httptest.NewRecorder()
	go func() {
		defer close(done)
		f.handler.ServeHTTP(rec, req)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("ServeHTTP did not return after client cancellation")
	}
	elapsed := time.Since(start)

	if got := f.upstreamCalls.Load() - attemptsBefore; got != wantAttempts {
		t.Errorf("upstream attempts after cancellation = %d, want %d (the caller's own attempts, none further)", got, wantAttempts)
	}
	if got := rateLimitAdmissions(t, f.handler.deploymentVariant) - admissionsBefore; got != float64(wantAttempts) {
		t.Errorf("limiter admissions after cancellation = %v, want %d (one per attempt, none further)", got, wantAttempts)
	}
	// The waits being cut short (60s, or 1s+2s) must show up as an early
	// return, not a completed retry sequence.
	if elapsed > 2500*time.Millisecond {
		t.Errorf("ServeHTTP returned after %v, want abandonment well before the waits elapsed", elapsed)
	}
}
