package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"golang.org/x/time/rate"
)

func TestRateLimitClientBucketUsesPeerHostAndIsBounded(t *testing.T) {
	first := rateLimitClientBucket("10.42.0.7:31001")
	second := rateLimitClientBucket("10.42.0.7:31002")
	if first != second {
		t.Fatalf("same peer IP with different ports produced different buckets: %q and %q", first, second)
	}

	buckets := make(map[string]struct{})
	for _, peer := range []string{"", "10.42.0.8:8080", "[fd00::1]:8080", "malformed-peer"} {
		bucket := rateLimitClientBucket(peer)
		buckets[bucket] = struct{}{}
		if len(bucket) != len("source-00") || bucket[:7] != "source-" {
			t.Errorf("bucket for %q = %q, want source-00 through source-63", peer, bucket)
		}
	}
	if len(buckets) > rateLimitClientBucketCount {
		t.Fatalf("created %d buckets, limit is %d", len(buckets), rateLimitClientBucketCount)
	}
}

func TestAdaptiveRateLimiterRoundRobinsQueuedSourceBuckets(t *testing.T) {
	arl := NewAdaptiveRateLimiter(5, 1, 10)
	// A burst of one makes the first request immediate and then leaves a long
	// enough refill interval to queue work from both sources deterministically.
	arl.mu.Lock()
	arl.limiter = rate.NewLimiter(rate.Limit(5), 1)
	arl.mu.Unlock()

	completed := make(chan string, 4)
	wait := func(name, source string) {
		go func() {
			arl.waitForClient("test", source)
			completed <- name
		}()
	}
	receive := func(want string) {
		t.Helper()
		select {
		case got := <-completed:
			if got != want {
				t.Fatalf("request completion = %q, want %q", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %q", want)
		}
	}

	wait("first-a", "source-01")
	receive("first-a") // consume the initial burst token

	wait("second-a", "source-01")
	waitForFairDispatch(t, arl, "source-01", 1)

	// Queue another A request before B. A is currently waiting for a global
	// token, so completing it must rotate to B rather than grant A again.
	wait("third-a", "source-01")
	waitForFairQueueLength(t, arl, "source-01", 2)
	wait("first-b", "source-02")
	waitForFairQueueLength(t, arl, "source-02", 1)

	receive("second-a")
	receive("first-b")
	receive("third-a")

	if got := arl.GetCurrentRate(); got != 5 {
		t.Errorf("fair scheduler changed global rate to %v, want 5", got)
	}
}

func TestAdaptiveRateLimiterFairQueueWakesWhenZeroRateIsReset(t *testing.T) {
	arl := NewAdaptiveRateLimiter(0, 0, 1)
	done := make(chan struct{})
	go func() {
		arl.waitForClient("test", "source-01")
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("zero-rate fair queue admitted a request before reset")
	case <-time.After(20 * time.Millisecond):
	}

	arl.Reset(1)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("fair queue did not resume after reset")
	}
}

func waitForFairDispatch(t *testing.T, arl *AdaptiveRateLimiter, source string, queueLength int) {
	t.Helper()
	waitForFairCondition(t, arl, func() bool {
		return arl.fairDispatching && len(arl.fairQueues[source]) == queueLength
	})
}

func waitForFairQueueLength(t *testing.T, arl *AdaptiveRateLimiter, source string, queueLength int) {
	t.Helper()
	waitForFairCondition(t, arl, func() bool {
		return len(arl.fairQueues[source]) == queueLength
	})
}

func waitForFairCondition(t *testing.T, arl *AdaptiveRateLimiter, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		arl.fairMu.Lock()
		ok := condition()
		arl.fairMu.Unlock()
		if ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for fair queue state")
}

func TestProxyHandlerRecordsCapacityRejectionBySourceBucket(t *testing.T) {
	handler := NewProxyHandler("key", "http://example.invalid", 0, 1, "fairness-test", nil, "", 1, 1, 1)
	handler.currentRequests.Store(1) // Make this request exceed maxWorkers.

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.42.0.9:8080"
	bucket := rateLimitClientBucket(req.RemoteAddr)
	before := testutil.ToFloat64(rateLimitRejections.WithLabelValues("fairness-test", bucket))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if got := testutil.ToFloat64(rateLimitRejections.WithLabelValues("fairness-test", bucket)); got != before+1 {
		t.Errorf("capacity rejections for %s = %v, want %v", bucket, got, before+1)
	}
}
