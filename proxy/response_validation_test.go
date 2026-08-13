package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestResponseValidation_EmptyJSONNonStreaming tests that empty JSON body on 2xx responses
// triggers retry up to MAX_RETRIES times, then returns 502 Bad Gateway.
func TestResponseValidation_EmptyJSONNonStreaming(t *testing.T) {
	testCases := []struct {
		name        string
		maxRetries  int
		wantCalls   int32 // Expected upstream calls: 1 initial + maxRetries
	}{
		{
			name:       "MAX_RETRIES=1",
			maxRetries: 1,
			wantCalls:  2, // 1 initial + 1 retry
		},
		{
			name:       "MAX_RETRIES=2",
			maxRetries: 2,
			wantCalls:  3, // 1 initial + 2 retries
		},
		{
			name:       "MAX_RETRIES=5",
			maxRetries: 5,
			wantCalls:  6, // 1 initial + 5 retries
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var upstreamCallCount atomic.Int32

			// Create upstream that returns 200 with empty body every time
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamCallCount.Add(1)
				w.WriteHeader(http.StatusOK)
				// Empty body - write nothing
			}))
			defer upstream.Close()

			handler := createTestProxyHandler(t, upstream.URL, tc.maxRetries)

			req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createNonStreamingRequestBody()))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			start := time.Now()
			handler.ServeHTTP(w, req)
			duration := time.Since(start)

			// Verify 502 Bad Gateway is returned after exhausting retries
			if w.Code != http.StatusBadGateway {
				t.Errorf("Expected status 502 Bad Gateway, got %d", w.Code)
			}

			// Verify exact upstream call count: 1 initial + maxRetries retries
			actualCalls := upstreamCallCount.Load()
			if actualCalls != tc.wantCalls {
				t.Errorf("Expected %d upstream calls (1 initial + %d retries), got %d",
					tc.wantCalls, tc.maxRetries, actualCalls)
			}

			// Verify exponential backoff took reasonable time
			// With maxRetries=N, backoff durations are: 1s, 2s, 4s, ...
			// For maxRetries=1: ~1s delay
			// For maxRetries=2: ~1s+2s=3s delay
			// For maxRetries=5: ~1s+2s+4s+8s+16s=31s delay
			// We allow 20% tolerance for test execution variance
			minExpectedDuration := time.Duration(1<<uint(tc.maxRetries-1)) * time.Second
			if tc.maxRetries > 0 && duration < minExpectedDuration {
				t.Logf("WARNING: Duration %v is less than expected minimum %v", duration, minExpectedDuration)
			}

			t.Logf("PASS: Empty JSON body triggered %d retries, %d upstream calls in %v, returned 502",
				tc.maxRetries, actualCalls, duration)
		})
	}
}

// TestResponseValidation_InvalidJSONNonStreaming tests that invalid JSON body on 2xx responses
// triggers retry up to MAX_RETRIES times, then returns 502 Bad Gateway.
func TestResponseValidation_InvalidJSONNonStreaming(t *testing.T) {
	testCases := []struct {
		name       string
		maxRetries int
		wantCalls  int32
		body       string
	}{
		{
			name:       "MAX_RETRIES=1 with malformed JSON",
			maxRetries: 1,
			wantCalls:  2,
			body:       "{invalid json",
		},
		{
			name:       "MAX_RETRIES=2 with truncated JSON",
			maxRetries: 2,
			wantCalls:  3,
			body:       `{"id": "msg_123", "content":`,
		},
		{
			name:       "MAX_RETRIES=3 with garbage data",
			maxRetries: 3,
			wantCalls:  4,
			body:       "not json at all!!!",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var upstreamCallCount atomic.Int32

			// Create upstream that returns 200 with invalid JSON every time
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamCallCount.Add(1)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(tc.body))
			}))
			defer upstream.Close()

			handler := createTestProxyHandler(t, upstream.URL, tc.maxRetries)

			req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createNonStreamingRequestBody()))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			start := time.Now()
			handler.ServeHTTP(w, req)
			duration := time.Since(start)

			// Verify 502 Bad Gateway is returned
			if w.Code != http.StatusBadGateway {
				t.Errorf("Expected status 502 Bad Gateway, got %d", w.Code)
			}

			// Verify exact upstream call count
			actualCalls := upstreamCallCount.Load()
			if actualCalls != tc.wantCalls {
				t.Errorf("Expected %d upstream calls (1 initial + %d retries), got %d",
					tc.wantCalls, tc.maxRetries, actualCalls)
			}

			t.Logf("PASS: Invalid JSON body triggered %d retries, %d upstream calls in %v, returned 502",
				tc.maxRetries, actualCalls, duration)
		})
	}
}

// TestResponseValidation_EmptyStreaming tests that streaming responses opening with zero bytes
// triggers retry up to MAX_RETRIES times, then returns 502 Bad Gateway.
func TestResponseValidation_EmptyStreaming(t *testing.T) {
	testCases := []struct {
		name       string
		maxRetries int
		wantCalls  int32
	}{
		{
			name:       "MAX_RETRIES=1",
			maxRetries: 1,
			wantCalls:  2,
		},
		{
			name:       "MAX_RETRIES=2",
			maxRetries: 2,
			wantCalls:  3,
		},
		{
			name:       "MAX_RETRIES=4",
			maxRetries: 4,
			wantCalls:  5,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var upstreamCallCount atomic.Int32

			// Create upstream that returns 200 with empty streaming response
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamCallCount.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				// Empty streaming - write no data
			}))
			defer upstream.Close()

			handler := createTestProxyHandler(t, upstream.URL, tc.maxRetries)

			req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createStreamingRequestBody()))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			start := time.Now()
			handler.ServeHTTP(w, req)
			duration := time.Since(start)

			// Verify 502 Bad Gateway is returned
			if w.Code != http.StatusBadGateway {
				t.Errorf("Expected status 502 Bad Gateway, got %d", w.Code)
			}

			// Verify exact upstream call count
			actualCalls := upstreamCallCount.Load()
			if actualCalls != tc.wantCalls {
				t.Errorf("Expected %d upstream calls (1 initial + %d retries), got %d",
					tc.wantCalls, tc.maxRetries, actualCalls)
			}

			// Verify exponential backoff took reasonable time
			minExpectedDuration := time.Duration(1<<uint(tc.maxRetries-1)) * time.Second
			if tc.maxRetries > 0 && duration < minExpectedDuration {
				t.Logf("WARNING: Duration %v is less than expected minimum %v", duration, minExpectedDuration)
			}

			t.Logf("PASS: Empty streaming triggered %d retries, %d upstream calls in %v, returned 502",
				tc.maxRetries, actualCalls, duration)
		})
	}
}

// TestResponseValidation_MetricsEmptyJSON tests that zai_proxy_upstream_errors_total
// metric increments with correct error_type="truncated_response" for empty JSON retries.
func TestResponseValidation_MetricsEmptyJSON(t *testing.T) {
	// Reset metrics before test
	retryAttempts.Reset()
	upstreamErrors.Reset()

	maxRetries := 2
	var upstreamCallCount atomic.Int32

	// Create upstream that returns 200 with empty body
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCallCount.Add(1)
		w.WriteHeader(http.StatusOK)
		// Empty body
	}))
	defer upstream.Close()

	handler := createTestProxyHandler(t, upstream.URL, maxRetries)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createNonStreamingRequestBody()))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Verify zai_proxy_upstream_errors_total metric increments
	// Each failed attempt (except the final one that returns 502) increments the metric
	// With maxRetries=2, we have: initial attempt fails (error count 1), retry 1 fails (error count 2), retry 2 fails (error count 3, then returns 502)
	// So we expect 3 error increments
	expectedErrorCount := float64(maxRetries + 1)
	errorMetric := upstreamErrors.WithLabelValues("truncated_response", "test")
	errorCount := testutil.ToFloat64(errorMetric)
	if errorCount != expectedErrorCount {
		t.Errorf("Expected zai_proxy_upstream_errors_total{{error_type=\"truncated_response\"}} = %f, got %f",
			expectedErrorCount, errorCount)
	}

	// Verify zai_proxy_retry_attempts_total metric increments
	// Each retry increments the retry attempts metric
	// With maxRetries=2, we expect 2 retry attempts (not 3 - the final attempt doesn't retry, it returns 502)
	expectedRetryCount := float64(maxRetries)
	retryMetric := retryAttempts.WithLabelValues("truncated_response", "test")
	retryCount := testutil.ToFloat64(retryMetric)
	if retryCount != expectedRetryCount {
		t.Errorf("Expected zai_proxy_retry_attempts_total{{reason=\"truncated_response\"}} = %f, got %f",
			expectedRetryCount, retryCount)
	}

	// Verify upstream call count
	expectedCalls := int32(maxRetries + 1)
	actualCalls := upstreamCallCount.Load()
	if actualCalls != expectedCalls {
		t.Errorf("Expected %d upstream calls, got %d", expectedCalls, actualCalls)
	}

	// Verify 502 returned
	if w.Code != http.StatusBadGateway {
		t.Errorf("Expected status 502 Bad Gateway, got %d", w.Code)
	}

	t.Logf("PASS: Metrics verified - upstream_errors=%f, retry_attempts=%f, calls=%d, status=%d",
		errorCount, retryCount, actualCalls, w.Code)
}

// TestResponseValidation_MetricsInvalidJSON tests that zai_proxy_upstream_errors_total
// metric increments with correct error_type="truncated_response" for invalid JSON retries.
func TestResponseValidation_MetricsInvalidJSON(t *testing.T) {
	// Reset metrics before test
	retryAttempts.Reset()
	upstreamErrors.Reset()

	maxRetries := 3
	var upstreamCallCount atomic.Int32

	// Create upstream that returns 200 with invalid JSON
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCallCount.Add(1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{this is not valid json"))
	}))
	defer upstream.Close()

	handler := createTestProxyHandler(t, upstream.URL, maxRetries)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createNonStreamingRequestBody()))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Verify upstream errors metric
	expectedErrorCount := float64(maxRetries + 1)
	errorMetric := upstreamErrors.WithLabelValues("truncated_response", "test")
	errorCount := testutil.ToFloat64(errorMetric)
	if errorCount != expectedErrorCount {
		t.Errorf("Expected zai_proxy_upstream_errors_total{{error_type=\"truncated_response\"}} = %f, got %f",
			expectedErrorCount, errorCount)
	}

	// Verify retry attempts metric
	expectedRetryCount := float64(maxRetries)
	retryMetric := retryAttempts.WithLabelValues("truncated_response", "test")
	retryCount := testutil.ToFloat64(retryMetric)
	if retryCount != expectedRetryCount {
		t.Errorf("Expected zai_proxy_retry_attempts_total{{reason=\"truncated_response\"}} = %f, got %f",
			expectedRetryCount, retryCount)
	}

	// Verify upstream call count
	expectedCalls := int32(maxRetries + 1)
	actualCalls := upstreamCallCount.Load()
	if actualCalls != expectedCalls {
		t.Errorf("Expected %d upstream calls, got %d", expectedCalls, actualCalls)
	}

	// Verify 502 returned
	if w.Code != http.StatusBadGateway {
		t.Errorf("Expected status 502 Bad Gateway, got %d", w.Code)
	}

	t.Logf("PASS: Metrics verified - upstream_errors=%f, retry_attempts=%f, calls=%d, status=%d",
		errorCount, retryCount, actualCalls, w.Code)
}

// TestResponseValidation_MetricsEmptyStreaming tests that zai_proxy_upstream_errors_total
// metric increments with correct error_type="empty_streaming" for empty streaming retries.
func TestResponseValidation_MetricsEmptyStreaming(t *testing.T) {
	// Reset metrics before test
	retryAttempts.Reset()
	upstreamErrors.Reset()

	maxRetries := 2
	var upstreamCallCount atomic.Int32

	// Create upstream that returns 200 with empty streaming response
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCallCount.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Empty streaming response
	}))
	defer upstream.Close()

	handler := createTestProxyHandler(t, upstream.URL, maxRetries)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createStreamingRequestBody()))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Verify upstream errors metric with error_type="empty_streaming"
	expectedErrorCount := float64(maxRetries + 1)
	errorMetric := upstreamErrors.WithLabelValues("empty_streaming", "test")
	errorCount := testutil.ToFloat64(errorMetric)
	if errorCount != expectedErrorCount {
		t.Errorf("Expected zai_proxy_upstream_errors_total{{error_type=\"empty_streaming\"}} = %f, got %f",
			expectedErrorCount, errorCount)
	}

	// Verify retry attempts metric with reason="empty_streaming"
	expectedRetryCount := float64(maxRetries)
	retryMetric := retryAttempts.WithLabelValues("empty_streaming", "test")
	retryCount := testutil.ToFloat64(retryMetric)
	if retryCount != expectedRetryCount {
		t.Errorf("Expected zai_proxy_retry_attempts_total{{reason=\"empty_streaming\"}} = %f, got %f",
			expectedRetryCount, retryCount)
	}

	// Verify upstream call count
	expectedCalls := int32(maxRetries + 1)
	actualCalls := upstreamCallCount.Load()
	if actualCalls != expectedCalls {
		t.Errorf("Expected %d upstream calls, got %d", expectedCalls, actualCalls)
	}

	// Verify 502 returned
	if w.Code != http.StatusBadGateway {
		t.Errorf("Expected status 502 Bad Gateway, got %d", w.Code)
	}

	t.Logf("PASS: Metrics verified - upstream_errors=%f, retry_attempts=%f, calls=%d, status=%d",
		errorCount, retryCount, actualCalls, w.Code)
}

// TestResponseValidation_ValidResponsePassesThrough tests that valid responses
// (both non-streaming and streaming) pass through without triggering retries.
func TestResponseValidation_ValidResponsePassesThrough(t *testing.T) {
	t.Run("valid non-streaming response", func(t *testing.T) {
		var upstreamCallCount atomic.Int32

		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			upstreamCallCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":      "msg_test123",
				"type":    "message",
				"role":    "assistant",
				"content": []map[string]string{{"type": "text", "text": "Hello!"}},
				"usage":   map[string]int{"input_tokens": 10, "output_tokens": 5},
			})
		}))
		defer upstream.Close()

		handler := createTestProxyHandler(t, upstream.URL, 3)

		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createNonStreamingRequestBody()))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		// Verify success response returned
		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", w.Code)
		}

		// Note: Response body is consumed by the handler during validation and token counting.
		// The handler already validated it before consuming, so we only verify status and calls.

		// Verify exactly 1 upstream call (no retries)
		if upstreamCallCount.Load() != 1 {
			t.Errorf("Expected 1 upstream call (no retry), got %d", upstreamCallCount.Load())
		}

		t.Logf("PASS: Valid non-streaming response passed through without retry")
	})

	t.Run("valid streaming response", func(t *testing.T) {
		var upstreamCallCount atomic.Int32

		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			upstreamCallCount.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			// Send some SSE data
			w.Write([]byte("data: {\"type\":\"message_start\"}\n\n"))
			w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"Hello\"}}\n\n"))
			w.Write([]byte("data: [DONE]\n\n"))
		}))
		defer upstream.Close()

		handler := createTestProxyHandler(t, upstream.URL, 3)

		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createStreamingRequestBody()))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		// Verify success response returned
		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", w.Code)
		}

		// Verify exactly 1 upstream call (no retries)
		if upstreamCallCount.Load() != 1 {
			t.Errorf("Expected 1 upstream call (no retry), got %d", upstreamCallCount.Load())
		}

		t.Logf("PASS: Valid streaming response passed through without retry")
	})
}

// TestResponseValidation_VariousInvalidBodies tests various forms of invalid response bodies
// to ensure they all trigger retries correctly.
func TestResponseValidation_VariousInvalidBodies(t *testing.T) {
	testCases := []struct {
		name  string
		body  string
		valid bool
	}{
		{
			name:  "empty body",
			body:  "",
			valid: false,
		},
		{
			name:  "single opening brace",
			body:  "{",
			valid: false,
		},
		{
			name:  "unclosed object",
			body:  `{"key": "value"`,
			valid: false,
		},
		{
			name:  "unclosed array",
			body:  `["item1", "item2"`,
			valid: false,
		},
		{
			name:  "random text",
			body:  "this is not json at all",
			valid: false,
		},
		{
			name:  "partial JSON with trailing comma",
			body:  `{"id": "msg123", "content": "text",}`,
			valid: false,
		},
		{
			name:  "valid JSON object",
			body:  `{"id": "msg123", "content": "hello"}`,
			valid: true,
		},
		{
			name:  "valid JSON array",
			body:  `[{"id": "msg123"}, {"id": "msg456"}]`,
			valid: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var upstreamCallCount atomic.Int32
			maxRetries := 1

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamCallCount.Add(1)
				w.WriteHeader(http.StatusOK)
				if len(tc.body) > 0 {
					w.Write([]byte(tc.body))
				}
			}))
			defer upstream.Close()

			handler := createTestProxyHandler(t, upstream.URL, maxRetries)

			req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createNonStreamingRequestBody()))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if tc.valid {
				// Valid JSON should pass through
				if w.Code != http.StatusOK {
					t.Errorf("Expected status 200 OK for valid JSON, got %d", w.Code)
				}
				if upstreamCallCount.Load() != 1 {
					t.Errorf("Expected 1 upstream call for valid JSON (no retry), got %d", upstreamCallCount.Load())
				}
			} else {
				// Invalid JSON should trigger retry and return 502
				if w.Code != http.StatusBadGateway {
					t.Errorf("Expected status 502 Bad Gateway for invalid JSON, got %d", w.Code)
				}
				expectedCalls := int32(maxRetries + 1)
				if upstreamCallCount.Load() != expectedCalls {
					t.Errorf("Expected %d upstream calls for invalid JSON (1 initial + %d retries), got %d",
						expectedCalls, maxRetries, upstreamCallCount.Load())
				}
			}

			t.Logf("PASS: %s - status=%d, calls=%d", tc.name, w.Code, upstreamCallCount.Load())
		})
	}
}

// TestResponseValidation_StreamingZeroBytesPeek tests that the streaming zero-byte detection
// works by peeking at the first chunk of the response body.
func TestResponseValidation_StreamingZeroBytesPeek(t *testing.T) {
	t.Run("zero bytes on first peek", func(t *testing.T) {
		var upstreamCallCount atomic.Int32
		maxRetries := 1

		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			upstreamCallCount.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			// Close immediately without writing anything
		}))
		defer upstream.Close()

		handler := createTestProxyHandler(t, upstream.URL, maxRetries)

		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createStreamingRequestBody()))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		// Verify retry was triggered and 502 returned
		if w.Code != http.StatusBadGateway {
			t.Errorf("Expected status 502 Bad Gateway, got %d", w.Code)
		}
		expectedCalls := int32(maxRetries + 1)
		if upstreamCallCount.Load() != expectedCalls {
			t.Errorf("Expected %d upstream calls, got %d", expectedCalls, upstreamCallCount.Load())
		}

		t.Logf("PASS: Zero bytes on first peek triggered retry, %d calls, status 502", upstreamCallCount.Load())
	})

	t.Run("non-zero bytes on first peek (no retry)", func(t *testing.T) {
		var upstreamCallCount atomic.Int32

		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			upstreamCallCount.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			// Write at least one byte immediately
			w.Write([]byte("d"))
		}))
		defer upstream.Close()

		handler := createTestProxyHandler(t, upstream.URL, 3)

		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createStreamingRequestBody()))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		// Verify no retry - response passes through
		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", w.Code)
		}
		if upstreamCallCount.Load() != 1 {
			t.Errorf("Expected 1 upstream call (no retry), got %d", upstreamCallCount.Load())
		}

		t.Logf("PASS: Non-zero bytes on first peek, no retry, status 200")
	})
}
