package main

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestMockUpstream_AllScenarios verifies that all MockUpstream scenarios work correctly.
func TestMockUpstream_AllScenarios(t *testing.T) {
	t.Run("429-with-retry-after-then-success", func(t *testing.T) {
		mock := NewMockUpstream("429-with-retry-after-then-success")
		defer mock.Close()

		// Configure to fail for first 2 requests, then succeed
		mock.SetFailUntilCount(2)
		mock.SetRetryAfter("5")

		// Make 3 requests - first 2 should return 429, third should succeed
		for i := 0; i < 3; i++ {
			req, _ := http.NewRequest("GET", mock.URL()+"/test", nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Request %d failed: %v", i, err)
			}

			if i < 2 {
				// First 2 requests should return 429 with Retry-After
				if resp.StatusCode != http.StatusTooManyRequests {
					t.Errorf("Request %d: expected status 429, got %d", i, resp.StatusCode)
				}
				if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "5" {
					t.Errorf("Request %d: expected Retry-After=5, got %s", i, retryAfter)
				}
			} else {
				// Third request should succeed
				if resp.StatusCode != http.StatusOK {
					t.Errorf("Request 2: expected status 200, got %d", resp.StatusCode)
				}
				// Verify response body is valid JSON
				if !jsonValid(t, resp) {
					t.Error("Request 2: response body should be valid JSON")
				}
			}
			resp.Body.Close()
		}

		// Verify request count is 3
		if count := mock.GetRequestCount(); count != 3 {
			t.Errorf("Expected 3 requests, got %d", count)
		}
	})

	t.Run("429-no-header-then-429", func(t *testing.T) {
		mock := NewMockUpstream("429-no-header-then-429")
		defer mock.Close()

		// All requests should return 429 without Retry-After header
		for i := 0; i < 3; i++ {
			resp, err := http.Get(mock.URL() + "/test")
			if err != nil {
				t.Fatalf("Request %d failed: %v", i, err)
			}

			if resp.StatusCode != http.StatusTooManyRequests {
				t.Errorf("Request %d: expected status 429, got %d", i, resp.StatusCode)
			}
			if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
				t.Errorf("Request %d: expected no Retry-After header, got %s", i, retryAfter)
			}
			resp.Body.Close()
		}

		if count := mock.GetRequestCount(); count != 3 {
			t.Errorf("Expected 3 requests, got %d", count)
		}
	})

	t.Run("empty-json-body", func(t *testing.T) {
		mock := NewMockUpstream("empty-json-body")
		defer mock.Close()

		resp, err := http.Get(mock.URL() + "/test")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		if len(body) != 0 {
			t.Errorf("Expected empty body, got %d bytes: %s", len(body), string(body))
		}
	})

	t.Run("invalid-json-body", func(t *testing.T) {
		mock := NewMockUpstream("invalid-json-body")
		defer mock.Close()

		resp, err := http.Get(mock.URL() + "/test")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		if string(body) != "{invalid json" {
			t.Errorf("Expected invalid JSON body, got %s", string(body))
		}
	})

	t.Run("empty-streaming", func(t *testing.T) {
		mock := NewMockUpstream("empty-streaming")
		defer mock.Close()

		resp, err := http.Get(mock.URL() + "/test")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
			t.Errorf("Expected Content-Type text/event-stream, got %s", ct)
		}

		body, _ := io.ReadAll(resp.Body)
		if len(body) != 0 {
			t.Errorf("Expected empty streaming body, got %d bytes", len(body))
		}
	})

	t.Run("422-no-retry", func(t *testing.T) {
		mock := NewMockUpstream("422-no-retry")
		defer mock.Close()

		resp, err := http.Get(mock.URL() + "/test")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("Expected status 422, got %d", resp.StatusCode)
		}

		// Should have JSON body with error message
		var result map[string]string
		body, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatalf("Failed to parse JSON: %v", err)
		}
		if result["error"] != "unprocessable entity" {
			t.Errorf("Expected error='unprocessable entity', got %s", result["error"])
		}
	})

	t.Run("500-no-retry", func(t *testing.T) {
		mock := NewMockUpstream("500-no-retry")
		defer mock.Close()

		resp, err := http.Get(mock.URL() + "/test")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", resp.StatusCode)
		}
	})

	t.Run("404-no-retry", func(t *testing.T) {
		mock := NewMockUpstream("404-no-retry")
		defer mock.Close()

		resp, err := http.Get(mock.URL() + "/test")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", resp.StatusCode)
		}
	})

	t.Run("network-error-then-success", func(t *testing.T) {
		mock := NewMockUpstream("network-error-then-success")
		defer mock.Close()

		// Configure to fail for first request, then succeed
		mock.SetFailUntilCount(1)

		// First request will succeed but with empty body (httptest.Server limitation)
		// The real network simulation would fail, but in httptest the handler just returns
		resp1, err := http.Get(mock.URL() + "/test")
		if err != nil {
			t.Logf("First request failed (acceptable): %v", err)
		} else {
			// In httptest, the request succeeds but body is empty
			resp1.Body.Close()
			if resp1.StatusCode != http.StatusOK {
				t.Logf("First request status: %d (may not be 200 due to early return)", resp1.StatusCode)
			}
		}

		// Second request should succeed with full response
		resp2, err := http.Get(mock.URL() + "/test")
		if err != nil {
			t.Fatalf("Second request failed: %v", err)
		}
		defer resp2.Body.Close()

		if resp2.StatusCode != http.StatusOK {
			t.Errorf("Second request: expected status 200, got %d", resp2.StatusCode)
		}
		if !jsonValid(t, resp2) {
			t.Error("Second request: response should be valid JSON")
		}

		// Request count should be 2
		if count := mock.GetRequestCount(); count != 2 {
			t.Errorf("Expected 2 requests, got %d", count)
		}
	})

	t.Run("success", func(t *testing.T) {
		mock := NewMockUpstream("success")
		defer mock.Close()

		resp, err := http.Get(mock.URL() + "/test")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		// Read and verify response structure
		var result map[string]interface{}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if !json.Valid(body) {
			t.Error("Response should be valid JSON")
		}

		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatalf("Failed to parse JSON: %v", err)
		}
		if result["id"] != "msg_test123" {
			t.Errorf("Expected id=msg_test123, got %s", result["id"])
		}
		if result["type"] != "message" {
			t.Errorf("Expected type=message, got %s", result["type"])
		}
	})

	t.Run("malformed-streaming", func(t *testing.T) {
		mock := NewMockUpstream("malformed-streaming")
		defer mock.Close()

		resp, err := http.Get(mock.URL() + "/test")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
			t.Errorf("Expected Content-Type text/event-stream, got %s", ct)
		}

		body, _ := io.ReadAll(resp.Body)
		if string(body) != "not valid sse format\n" {
			t.Errorf("Expected malformed SSE data, got %s", string(body))
		}
	})
}

// TestMockUpstream_Methods verifies all MockUpstream methods work correctly.
func TestMockUpstream_Methods(t *testing.T) {
	t.Run("GetRequestCount", func(t *testing.T) {
		mock := NewMockUpstream("success")
		defer mock.Close()

		// Initially 0
		if count := mock.GetRequestCount(); count != 0 {
			t.Errorf("Initial count should be 0, got %d", count)
		}

		// Make 3 requests
		for i := 0; i < 3; i++ {
			http.Get(mock.URL() + "/test")
		}

		// Should be 3
		if count := mock.GetRequestCount(); count != 3 {
			t.Errorf("Expected 3 requests, got %d", count)
		}
	})

	t.Run("SetFailUntilCount", func(t *testing.T) {
		mock := NewMockUpstream("429-with-retry-after-then-success")
		defer mock.Close()
		mock.SetRetryAfter("1")

		// Test with fail count of 2
		mock.SetFailUntilCount(2)

		// First request should fail
		resp1, _ := http.Get(mock.URL() + "/test")
		if resp1.StatusCode != http.StatusTooManyRequests {
			t.Errorf("First request should fail with 429, got %d", resp1.StatusCode)
		}
		resp1.Body.Close()

		// Second request should fail
		resp2, _ := http.Get(mock.URL() + "/test")
		if resp2.StatusCode != http.StatusTooManyRequests {
			t.Errorf("Second request should fail with 429, got %d", resp2.StatusCode)
		}
		resp2.Body.Close()

		// Third request should succeed
		resp3, _ := http.Get(mock.URL() + "/test")
		if resp3.StatusCode != http.StatusOK {
			t.Errorf("Third request should succeed, got %d", resp3.StatusCode)
		}
		resp3.Body.Close()
	})

	t.Run("SetRetryAfter", func(t *testing.T) {
		mock := NewMockUpstream("429-with-retry-after-then-success")
		defer mock.Close()
		mock.SetFailUntilCount(1)

		// Set custom retry-after value
		mock.SetRetryAfter("10")

		resp, _ := http.Get(mock.URL() + "/test")
		defer resp.Body.Close()

		if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "10" {
			t.Errorf("Expected Retry-After=10, got %s", retryAfter)
		}
	})

	t.Run("SetDelayMs", func(t *testing.T) {
		mock := NewMockUpstream("success")
		defer mock.Close()

		// Set 100ms delay
		mock.SetDelayMs(100)

		start := time.Now()
		resp, err := http.Get(mock.URL() + "/test")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		resp.Body.Close()
		elapsed := time.Since(start)

		// Should take at least 100ms (with some tolerance for test execution)
		if elapsed < 90*time.Millisecond {
			t.Errorf("Expected delay >= 100ms, got %v", elapsed)
		}
		t.Logf("Delay test: %v elapsed", elapsed)
	})

	t.Run("SetCustomHandler", func(t *testing.T) {
		mock := NewMockUpstream("success")
		defer mock.Close()

		// Set custom handler that returns 418
		mock.SetCustomHandler(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(418)
			w.Write([]byte("I'm a teapot"))
		})

		resp, err := http.Get(mock.URL() + "/test")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 418 {
			t.Errorf("Expected status 418, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		if string(body) != "I'm a teapot" {
			t.Errorf("Expected 'I'm a teapot', got %s", string(body))
		}
	})

	t.Run("ResetCounters", func(t *testing.T) {
		mock := NewMockUpstream("success")
		defer mock.Close()

		// Make some requests
		for i := 0; i < 3; i++ {
			http.Get(mock.URL() + "/test")
		}

		// Verify count is 3
		if count := mock.GetRequestCount(); count != 3 {
			t.Errorf("Expected 3 requests, got %d", count)
		}

		// Reset counters
		mock.ResetCounters()

		// Should be 0 now
		if count := mock.GetRequestCount(); count != 0 {
			t.Errorf("After reset, count should be 0, got %d", count)
		}

		// Make another request
		http.Get(mock.URL() + "/test")

		// Should be 1
		if count := mock.GetRequestCount(); count != 1 {
			t.Errorf("After reset and 1 request, expected 1, got %d", count)
		}
	})
}

// TestMockUpstream_ProxyIntegration tests MockUpstream scenarios through the proxy.
func TestMockUpstream_ProxyIntegration(t *testing.T) {
	ConfigureTestEnv(t)
	t.Setenv(TestMaxRetriesEnv, "2")

	t.Run("success scenario through proxy", func(t *testing.T) {
		mock := NewMockUpstream("success")
		defer mock.Close()

		handler := CreateTestProxyHandler(t, mock.URL(), 2)
		req := CreateNonStreamingMessagesRequest()

		start := time.Now()
		resp := ExecuteProxyRequest(t, handler, req)
		duration := time.Since(start)

		// Should succeed immediately without retries
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		// Should only call upstream once
		if count := mock.GetRequestCount(); count != 1 {
			t.Errorf("Expected 1 upstream call, got %d", count)
		}

		// Should complete quickly (no retry delays)
		if duration > 500*time.Millisecond {
			t.Errorf("Expected fast completion, got %v", duration)
		}

		t.Logf("Success scenario: %v, %d calls", duration, mock.GetRequestCount())
	})

	t.Run("429 with retry after through proxy", func(t *testing.T) {
		mock := NewMockUpstream("429-with-retry-after-then-success")
		defer mock.Close()

		mock.SetFailUntilCount(1)
		mock.SetRetryAfter("1")

		// Reset metrics
		retryAttempts.Reset()
		upstreamErrors.Reset()

		handler := CreateTestProxyHandler(t, mock.URL(), 2)
		var observedDelays []time.Duration
		handler.retrySleep = func(delay time.Duration) {
			observedDelays = append(observedDelays, delay)
		}
		req := CreateNonStreamingMessagesRequest()

		resp := ExecuteProxyRequest(t, handler, req)

		// Should succeed after retry
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		// Should call upstream twice (initial + 1 retry)
		if count := mock.GetRequestCount(); count != 2 {
			t.Errorf("Expected 2 upstream calls, got %d", count)
		}

		// The header delay and exponential delay are both selected without
		// adding real-time sleeps to the suite.
		wantDelays := []time.Duration{time.Second, time.Second}
		if len(observedDelays) != len(wantDelays) {
			t.Fatalf("Expected %d retry delays, got %d: %v", len(wantDelays), len(observedDelays), observedDelays)
		}
		for i, want := range wantDelays {
			if got := observedDelays[i]; got != want {
				t.Errorf("Retry delay %d: got %v, want %v", i, got, want)
			}
		}

		t.Logf("429 retry: %d calls", mock.GetRequestCount())
	})

	t.Run("empty json body through proxy triggers retry", func(t *testing.T) {
		mock := NewMockUpstream("empty-json-body")
		defer mock.Close()

		// Reset metrics
		retryAttempts.Reset()
		upstreamErrors.Reset()

		handler := CreateTestProxyHandler(t, mock.URL(), 1)
		req := CreateNonStreamingMessagesRequest()

		start := time.Now()
		resp := ExecuteProxyRequest(t, handler, req)
		duration := time.Since(start)

		// Should return 502 after exhausting retries
		if resp.StatusCode != http.StatusBadGateway {
			t.Errorf("Expected status 502, got %d", resp.StatusCode)
		}

		// Should call upstream twice (initial + 1 retry)
		if count := mock.GetRequestCount(); count != 2 {
			t.Errorf("Expected 2 upstream calls, got %d", count)
		}

		// Verify error metric
		errorMetric := upstreamErrors.WithLabelValues("truncated_response", "test")
		errorCount := testutil.ToFloat64(errorMetric)
		if errorCount != 2.0 {
			t.Errorf("Expected upstream_errors=2, got %f", errorCount)
		}

		t.Logf("Empty JSON: %v, %d calls, errors=%f", duration, mock.GetRequestCount(), errorCount)
	})

	t.Run("invalid json body through proxy triggers retry", func(t *testing.T) {
		mock := NewMockUpstream("invalid-json-body")
		defer mock.Close()

		// Reset metrics
		retryAttempts.Reset()
		upstreamErrors.Reset()

		handler := CreateTestProxyHandler(t, mock.URL(), 1)
		req := CreateNonStreamingMessagesRequest()

		resp := ExecuteProxyRequest(t, handler, req)

		// Should return 502 after exhausting retries
		if resp.StatusCode != http.StatusBadGateway {
			t.Errorf("Expected status 502, got %d", resp.StatusCode)
		}

		// Should call upstream twice (initial + 1 retry)
		if count := mock.GetRequestCount(); count != 2 {
			t.Errorf("Expected 2 upstream calls, got %d", count)
		}

		t.Logf("Invalid JSON: %d calls", mock.GetRequestCount())
	})

	t.Run("empty streaming through proxy triggers retry", func(t *testing.T) {
		mock := NewMockUpstream("empty-streaming")
		defer mock.Close()

		// Reset metrics
		retryAttempts.Reset()
		upstreamErrors.Reset()

		handler := CreateTestProxyHandler(t, mock.URL(), 1)
		req := CreateStreamingMessagesRequest()

		resp := ExecuteProxyRequest(t, handler, req)

		// Should return 502 after exhausting retries
		if resp.StatusCode != http.StatusBadGateway {
			t.Errorf("Expected status 502, got %d", resp.StatusCode)
		}

		// Should call upstream twice (initial + 1 retry)
		if count := mock.GetRequestCount(); count != 2 {
			t.Errorf("Expected 2 upstream calls, got %d", count)
		}

		// Verify error metric with correct error_type
		errorMetric := upstreamErrors.WithLabelValues("empty_streaming", "test")
		errorCount := testutil.ToFloat64(errorMetric)
		if errorCount != 2.0 {
			t.Errorf("Expected upstream_errors=2 for empty_streaming, got %f", errorCount)
		}

		t.Logf("Empty streaming: %d calls, errors=%f", mock.GetRequestCount(), errorCount)
	})

	t.Run("422 no retry through proxy", func(t *testing.T) {
		mock := NewMockUpstream("422-no-retry")
		defer mock.Close()

		handler := CreateTestProxyHandler(t, mock.URL(), 2)
		req := CreateNonStreamingMessagesRequest()

		resp := ExecuteProxyRequest(t, handler, req)

		// Should return 422 without retry
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("Expected status 422, got %d", resp.StatusCode)
		}

		// Should only call upstream once (no retry)
		if count := mock.GetRequestCount(); count != 1 {
			t.Errorf("Expected 1 upstream call (no retry), got %d", count)
		}

		t.Logf("422 no retry: %d calls", mock.GetRequestCount())
	})

	t.Run("500 no retry through proxy", func(t *testing.T) {
		mock := NewMockUpstream("500-no-retry")
		defer mock.Close()

		handler := CreateTestProxyHandler(t, mock.URL(), 2)
		req := CreateNonStreamingMessagesRequest()

		resp := ExecuteProxyRequest(t, handler, req)

		// Should return 500 without retry
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", resp.StatusCode)
		}

		// Should only call upstream once (no retry)
		if count := mock.GetRequestCount(); count != 1 {
			t.Errorf("Expected 1 upstream call (no retry), got %d", count)
		}

		t.Logf("500 no retry: %d calls", mock.GetRequestCount())
	})

	t.Run("404 no retry through proxy", func(t *testing.T) {
		mock := NewMockUpstream("404-no-retry")
		defer mock.Close()

		handler := CreateTestProxyHandler(t, mock.URL(), 2)
		req := CreateNonStreamingMessagesRequest()

		resp := ExecuteProxyRequest(t, handler, req)

		// Should return 404 without retry
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", resp.StatusCode)
		}

		// Should only call upstream once (no retry)
		if count := mock.GetRequestCount(); count != 1 {
			t.Errorf("Expected 1 upstream call (no retry), got %d", count)
		}

		t.Logf("404 no retry: %d calls", mock.GetRequestCount())
	})
}

// jsonValid checks if a response body contains valid JSON
func jsonValid(t *testing.T, resp *http.Response) bool {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Failed to read body: %v", err)
		return false
	}
	return json.Valid(body)
}
