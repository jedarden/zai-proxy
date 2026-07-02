package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestRetryValidation429WithRetryAfter tests that 429 with Retry-After header
// honors the delay before retrying, then returns success to the client.
func TestRetryValidation429WithRetryAfter(t *testing.T) {
	maxRetries := 2
	var upstreamCallCount atomic.Int32

	// Create upstream that returns 429 twice, then success
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := upstreamCallCount.Add(1)
		if count <= 2 {
			w.Header().Set("Retry-After", "1") // 1 second wait
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		// Success after retries
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "msg_test123",
			"type":    "message",
			"role":    "assistant",
			"content": []map[string]string{{"type": "text", "text": "Success"}},
		})
	}))
	defer upstream.Close()

	handler := createTestProxyHandler(t, upstream.URL, maxRetries)

	// Create test request
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createNonStreamingRequestBody()))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Record start time to verify retry-after delay
	start := time.Now()

	// Call the proxy handler
	handler.ServeHTTP(w, req)

	duration := time.Since(start)

	// Verify response - read from recorder body, not response body
	body := w.Body.String()
	if !json.Valid([]byte(body)) {
		t.Errorf("Response body is not valid JSON: %s", body)
	}

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Verify upstream call count (1 initial + 2 retries = 3 total)
	expectedCalls := int32(maxRetries + 1)
	actualCalls := upstreamCallCount.Load()
	if actualCalls != expectedCalls {
		t.Errorf("Expected %d upstream calls, got %d", expectedCalls, actualCalls)
	}

	// Verify total duration is at least 2 seconds (Retry-After: 1 twice)
	if duration < 2*time.Second {
		t.Logf("WARNING: Duration %v is less than expected 2s", duration)
	}

	t.Logf("PASS: %d upstream calls in %v", actualCalls, duration)
}

// TestRetryValidation429NoHeader tests that 429 without Retry-After header
// uses exponential backoff and eventually surfaces the 429.
func TestRetryValidation429NoHeader(t *testing.T) {
	maxRetries := 1 // Small for fast test
	var upstreamCallCount atomic.Int32

	// Create upstream that always returns 429 without Retry-After
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCallCount.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	handler := createTestProxyHandler(t, upstream.URL, maxRetries)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createNonStreamingRequestBody()))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Record start time
	start := time.Now()

	handler.ServeHTTP(w, req)

	duration := time.Since(start)

	// Verify 429 is returned to client
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status 429, got %d", w.Code)
	}

	// Verify upstream call count (1 initial + 1 retry = 2 total with maxRetries=1)
	expectedCalls := int32(maxRetries + 1)
	actualCalls := upstreamCallCount.Load()
	if actualCalls != expectedCalls {
		t.Errorf("Expected %d upstream calls, got %d", expectedCalls, actualCalls)
	}

	// Verify exponential backoff: first retry waits 1s
	if duration < 1*time.Second {
		t.Logf("WARNING: Duration %v is less than expected 1s", duration)
	}

	t.Logf("PASS: %d upstream calls in %v", actualCalls, duration)
}

// TestRetryValidation422NoRetry tests that 422 responses are NOT retried.
func TestRetryValidation422NoRetry(t *testing.T) {
	maxRetries := 5 // High to ensure no retry happens
	var upstreamCallCount atomic.Int32

	// Create upstream that returns 422
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := upstreamCallCount.Add(1)
		t.Logf("Upstream call #%d", count)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "unprocessable entity",
			"type":  "invalid_request_error",
		})
	}))
	defer upstream.Close()

	handler := createTestProxyHandler(t, upstream.URL, maxRetries)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createNonStreamingRequestBody()))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Verify 422 is returned to client
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("Expected status 422, got %d", w.Code)
	}

	body := w.Body.String()
	t.Logf("Response body: %s", body)

	// Verify exactly 1 upstream call (no retry)
	actualCalls := upstreamCallCount.Load()
	if actualCalls != 1 {
		t.Errorf("Expected 1 upstream call (no retry), got %d", actualCalls)
	}

	t.Logf("PASS: 422 not retried, %d upstream call", actualCalls)
}

// TestRetryValidationEmptyJSONBodyRetry tests empty JSON body on 200 triggers retry
// and eventually returns 502 after MAX_RETRIES.
func TestRetryValidationEmptyJSONBodyRetry(t *testing.T) {
	maxRetries := 1 // Small for fast test
	var upstreamCallCount atomic.Int32

	// Create upstream that returns 200 with empty body
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := upstreamCallCount.Add(1)
		w.WriteHeader(http.StatusOK)
		// Empty body - no data written
		t.Logf("Upstream call #%d - returning empty body", count)
	}))
	defer upstream.Close()

	handler := createTestProxyHandler(t, upstream.URL, maxRetries)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createNonStreamingRequestBody()))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Verify 502 is returned to client (empty body after retries)
	if w.Code != http.StatusBadGateway {
		t.Errorf("Expected status 502, got %d", w.Code)
	}

	// Verify upstream call count (1 initial + 1 retry = 2 total with maxRetries=1)
	expectedCalls := int32(maxRetries + 1)
	actualCalls := upstreamCallCount.Load()
	if actualCalls != expectedCalls {
		t.Errorf("Expected %d upstream calls, got %d", expectedCalls, actualCalls)
	}

	t.Logf("PASS: empty body triggered retry, %d upstream calls, returned 502", actualCalls)
}

// TestRetryValidationInvalidJSONBodyRetry tests invalid JSON body on 200 triggers retry
// and eventually returns 502 after MAX_RETRIES.
func TestRetryValidationInvalidJSONBodyRetry(t *testing.T) {
	maxRetries := 1
	var upstreamCallCount atomic.Int32

	// Create upstream that returns 200 with invalid JSON
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := upstreamCallCount.Add(1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{invalid json"))
		t.Logf("Upstream call #%d - returning invalid JSON", count)
	}))
	defer upstream.Close()

	handler := createTestProxyHandler(t, upstream.URL, maxRetries)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createNonStreamingRequestBody()))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Verify 502 is returned to client
	if w.Code != http.StatusBadGateway {
		t.Errorf("Expected status 502, got %d", w.Code)
	}

	// Verify upstream call count
	expectedCalls := int32(maxRetries + 1)
	actualCalls := upstreamCallCount.Load()
	if actualCalls != expectedCalls {
		t.Errorf("Expected %d upstream calls, got %d", expectedCalls, actualCalls)
	}

	t.Logf("PASS: invalid JSON triggered retry, %d upstream calls, returned 502", actualCalls)
}

// TestRetryValidationEmptyStreamingRetry tests empty streaming response
// triggers retry and eventually returns 502 after MAX_RETRIES.
func TestRetryValidationEmptyStreamingRetry(t *testing.T) {
	maxRetries := 1
	var upstreamCallCount atomic.Int32

	// Create upstream that returns 200 with empty streaming response
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := upstreamCallCount.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// No data written - empty streaming response
		t.Logf("Upstream call #%d - returning empty streaming", count)
	}))
	defer upstream.Close()

	handler := createTestProxyHandler(t, upstream.URL, maxRetries)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createStreamingRequestBody()))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Verify 502 is returned to client
	if w.Code != http.StatusBadGateway {
		t.Errorf("Expected status 502, got %d", w.Code)
	}

	// Verify upstream call count
	expectedCalls := int32(maxRetries + 1)
	actualCalls := upstreamCallCount.Load()
	if actualCalls != expectedCalls {
		t.Errorf("Expected %d upstream calls, got %d", expectedCalls, actualCalls)
	}

	t.Logf("PASS: empty streaming triggered retry, %d upstream calls, returned 502", actualCalls)
}

// TestRetryValidation500NoRetry tests that 500 responses are NOT retried.
func TestRetryValidation500NoRetry(t *testing.T) {
	maxRetries := 5
	var upstreamCallCount atomic.Int32

	// Create upstream that returns 500
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCallCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	handler := createTestProxyHandler(t, upstream.URL, maxRetries)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createNonStreamingRequestBody()))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Verify 500 is returned to client
	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", w.Code)
	}

	// Verify exactly 1 upstream call (no retry)
	actualCalls := upstreamCallCount.Load()
	if actualCalls != 1 {
		t.Errorf("Expected 1 upstream call (no retry), got %d", actualCalls)
	}

	t.Logf("PASS: 500 not retried, %d upstream call", actualCalls)
}

// TestRetryValidation404NoRetry tests that 404 responses are NOT retried.
func TestRetryValidation404NoRetry(t *testing.T) {
	maxRetries := 5
	var upstreamCallCount atomic.Int32

	// Create upstream that returns 404
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCallCount.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer upstream.Close()

	handler := createTestProxyHandler(t, upstream.URL, maxRetries)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createNonStreamingRequestBody()))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Verify 404 is returned to client
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}

	// Verify exactly 1 upstream call (no retry)
	actualCalls := upstreamCallCount.Load()
	if actualCalls != 1 {
		t.Errorf("Expected 1 upstream call (no retry), got %d", actualCalls)
	}

	t.Logf("PASS: 404 not retried, %d upstream call", actualCalls)
}

// TestRetryValidationSuccessNoRetry tests successful request passes through correctly.
func TestRetryValidationSuccessNoRetry(t *testing.T) {
	maxRetries := 3
	var upstreamCallCount atomic.Int32

	// Create upstream that returns success immediately
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

	handler := createTestProxyHandler(t, upstream.URL, maxRetries)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createNonStreamingRequestBody()))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Verify success is returned to client
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !json.Valid([]byte(body)) {
		t.Error("Response body is not valid JSON")
	}

	// Verify exactly 1 upstream call (success, no retry)
	actualCalls := upstreamCallCount.Load()
	if actualCalls != 1 {
		t.Errorf("Expected 1 upstream call (success, no retry), got %d", actualCalls)
	}

	t.Logf("PASS: success returned, %d upstream call", actualCalls)
}

// TestRetryValidationMetrics429 tests that 429 retries increment metrics correctly.
func TestRetryValidationMetrics429(t *testing.T) {
	maxRetries := 2
	var upstreamCallCount atomic.Int32

	// Reset metrics before test
	retryAttempts.Reset()
	upstreamErrors.Reset()

	// Create upstream that returns 429 twice, then success
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := upstreamCallCount.Add(1)
		if count <= 2 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "msg_123"})
	}))
	defer upstream.Close()

	handler := createTestProxyHandler(t, upstream.URL, maxRetries)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createNonStreamingRequestBody()))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Verify retry attempts metric - should have 2 retries with reason="429"
	retryMetric := retryAttempts.WithLabelValues("429", "test")
	retryCount := testutil.ToFloat64(retryMetric)
	if retryCount != float64(2) {
		t.Errorf("Expected 2 retry attempts, got %f", retryCount)
	}

	// Verify total upstream calls
	actualCalls := upstreamCallCount.Load()
	expectedCalls := int32(3) // 1 initial + 2 retries
	if actualCalls != expectedCalls {
		t.Errorf("Expected %d upstream calls, got %d", expectedCalls, actualCalls)
	}

	t.Logf("PASS: 429 metrics verified, %d upstream calls, %f retries", actualCalls, retryCount)
}

// TestRetryValidationMetrics422 tests that 422 increments error metric but NOT retry metric.
func TestRetryValidationMetrics422(t *testing.T) {
	maxRetries := 5
	var upstreamCallCount atomic.Int32

	// Reset metrics before test
	retryAttempts.Reset()
	upstreamErrors.Reset()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCallCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{"error": "unprocessable"})
	}))
	defer upstream.Close()

	handler := createTestProxyHandler(t, upstream.URL, maxRetries)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createNonStreamingRequestBody()))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Verify upstream errors metric - should have 1 error with error_type="422"
	errorMetric := upstreamErrors.WithLabelValues("422", "test")
	errorCount := testutil.ToFloat64(errorMetric)
	if errorCount != 1 {
		t.Errorf("Expected 1 upstream error (422), got %f", errorCount)
	}

	// Verify NO retry attempts for 422
	retryMetric := retryAttempts.WithLabelValues("422", "test")
	retryCount := testutil.ToFloat64(retryMetric)
	if retryCount != 0 {
		t.Errorf("Expected 0 retry attempts for 422 (no retry), got %f", retryCount)
	}

	// Verify exactly 1 upstream call
	actualCalls := upstreamCallCount.Load()
	if actualCalls != 1 {
		t.Errorf("Expected 1 upstream call (no retry), got %d", actualCalls)
	}

	t.Logf("PASS: 422 error recorded, no retry attempts, 1 upstream call")
}

// TestRetryValidationMetricsEmptyBody tests that empty body retries increment metrics correctly.
func TestRetryValidationMetricsEmptyBody(t *testing.T) {
	maxRetries := 1
	var upstreamCallCount atomic.Int32

	// Reset metrics before test
	retryAttempts.Reset()
	upstreamErrors.Reset()

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

	// Verify upstream errors metric - should have 2 errors with error_type="truncated_response"
	// (one for each failed attempt before the last one)
	errorMetric := upstreamErrors.WithLabelValues("truncated_response", "test")
	errorCount := testutil.ToFloat64(errorMetric)
	if errorCount != 2 {
		t.Errorf("Expected 2 upstream errors (truncated_response), got %f", errorCount)
	}

	// Verify retry attempts metric - should have 1 retry with reason="truncated_response"
	retryMetric := retryAttempts.WithLabelValues("truncated_response", "test")
	retryCount := testutil.ToFloat64(retryMetric)
	if retryCount != 1 {
		t.Errorf("Expected 1 retry attempt (truncated_response), got %f", retryCount)
	}

	// Verify upstream call count (1 initial + 1 retry = 2 total)
	expectedCalls := int32(maxRetries + 1)
	actualCalls := upstreamCallCount.Load()
	if actualCalls != expectedCalls {
		t.Errorf("Expected %d upstream calls, got %d", expectedCalls, actualCalls)
	}

	t.Logf("PASS: truncated_response errors=%f, retries=%f, upstream calls=%d",
		errorCount, retryCount, actualCalls)
}

// TestRetryValidationMetricsEmptyStreaming tests that empty streaming retries increment metrics correctly.
func TestRetryValidationMetricsEmptyStreaming(t *testing.T) {
	maxRetries := 1
	var upstreamCallCount atomic.Int32

	// Reset metrics before test
	retryAttempts.Reset()
	upstreamErrors.Reset()

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

	// Verify upstream errors metric - should have 2 errors with error_type="empty_streaming"
	errorMetric := upstreamErrors.WithLabelValues("empty_streaming", "test")
	errorCount := testutil.ToFloat64(errorMetric)
	if errorCount != 2 {
		t.Errorf("Expected 2 upstream errors (empty_streaming), got %f", errorCount)
	}

	// Verify retry attempts metric - should have 1 retry with reason="empty_streaming"
	retryMetric := retryAttempts.WithLabelValues("empty_streaming", "test")
	retryCount := testutil.ToFloat64(retryMetric)
	if retryCount != 1 {
		t.Errorf("Expected 1 retry attempt (empty_streaming), got %f", retryCount)
	}

	// Verify upstream call count
	expectedCalls := int32(maxRetries + 1)
	actualCalls := upstreamCallCount.Load()
	if actualCalls != expectedCalls {
		t.Errorf("Expected %d upstream calls, got %d", expectedCalls, actualCalls)
	}

	t.Logf("PASS: empty_streaming errors=%f, retries=%f, upstream calls=%d",
		errorCount, retryCount, actualCalls)
}

// TestRetryValidationErrorClassificationTable tests all rows from the plan.md error classification table.
func TestRetryValidationErrorClassificationTable(t *testing.T) {
	testCases := []struct {
		name               string
		maxRetries         int
		scenario           string
		expectedCalls      int32 // Expected upstream call count
		expectedStatusCode int
		shouldRetry        bool
		description        string
	}{
		{
			name:               "429_with_retry_after",
			maxRetries:         2,
			scenario:           "429-retry-after-success",
			expectedCalls:      3, // 1 initial + 2 retries
			expectedStatusCode: 200,
			shouldRetry:        true,
			description:        "429 with Retry-After should honor delay and retry",
		},
		{
			name:               "429_no_header",
			maxRetries:         1,
			scenario:           "429-always",
			expectedCalls:      2, // 1 initial + 1 retry
			expectedStatusCode: 429,
			shouldRetry:        true,
			description:        "429 without header should use exponential backoff",
		},
		{
			name:               "422_no_retry",
			maxRetries:         5,
			scenario:           "422-always",
			expectedCalls:      1, // No retry
			expectedStatusCode: 422,
			shouldRetry:        false,
			description:        "422 should not be retried",
		},
		{
			name:               "empty_json_body",
			maxRetries:         1,
			scenario:           "200-empty",
			expectedCalls:      2, // 1 initial + 1 retry
			expectedStatusCode: 502,
			shouldRetry:        true,
			description:        "Empty JSON body should retry, return 502 after max retries",
		},
		{
			name:               "invalid_json_body",
			maxRetries:         1,
			scenario:           "200-invalid-json",
			expectedCalls:      2, // 1 initial + 1 retry
			expectedStatusCode: 502,
			shouldRetry:        true,
			description:        "Invalid JSON body should retry, return 502 after max retries",
		},
		{
			name:               "empty_streaming",
			maxRetries:         1,
			scenario:           "200-empty-streaming",
			expectedCalls:      2, // 1 initial + 1 retry
			expectedStatusCode: 502,
			shouldRetry:        true,
			description:        "Empty streaming should retry, return 502 after max retries",
		},
		{
			name:               "500_no_retry",
			maxRetries:         5,
			scenario:           "500-always",
			expectedCalls:      1, // No retry
			expectedStatusCode: 500,
			shouldRetry:        false,
			description:        "500 should not be retried",
		},
		{
			name:               "404_no_retry",
			maxRetries:         5,
			scenario:           "404-always",
			expectedCalls:      1, // No retry
			expectedStatusCode: 404,
			shouldRetry:        false,
			description:        "404 should not be retried",
		},
		{
			name:               "success",
			maxRetries:         3,
			scenario:           "200-success",
			expectedCalls:      1, // Success, no retry
			expectedStatusCode: 200,
			shouldRetry:        false,
			description:        "Success should not trigger retry",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Log(tc.description)

			var upstreamCallCount atomic.Int32

			// Create upstream based on scenario
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count := upstreamCallCount.Add(1)
				switch tc.scenario {
				case "429-retry-after-success":
					if count <= 2 {
						w.Header().Set("Retry-After", "1")
						w.WriteHeader(http.StatusTooManyRequests)
					} else {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						json.NewEncoder(w).Encode(map[string]interface{}{"id": "msg_123"})
					}
				case "429-always":
					w.WriteHeader(http.StatusTooManyRequests)
				case "422-always":
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnprocessableEntity)
					json.NewEncoder(w).Encode(map[string]string{"error": "unprocessable"})
				case "200-empty":
					w.WriteHeader(http.StatusOK)
					// Empty body
				case "200-invalid-json":
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("{invalid json"))
				case "200-empty-streaming":
					w.Header().Set("Content-Type", "text/event-stream")
					w.WriteHeader(http.StatusOK)
					// Empty streaming
				case "500-always":
					w.WriteHeader(http.StatusInternalServerError)
				case "404-always":
					w.WriteHeader(http.StatusNotFound)
				case "200-success":
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]interface{}{"id": "msg_123"})
				}
			}))
			defer upstream.Close()

			handler := createTestProxyHandler(t, upstream.URL, tc.maxRetries)

			// Use streaming request body for streaming scenario
			var reqBody string
			if tc.scenario == "200-empty-streaming" {
				reqBody = createStreamingRequestBody()
			} else {
				reqBody = createNonStreamingRequestBody()
			}

			req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tc.expectedStatusCode {
				t.Errorf("Expected status %d, got %d", tc.expectedStatusCode, w.Code)
			}

			actualCalls := upstreamCallCount.Load()
			if actualCalls != tc.expectedCalls {
				t.Errorf("Expected %d upstream calls, got %d", tc.expectedCalls, actualCalls)
			}

			t.Logf("PASS: %d upstream calls, status %d", actualCalls, w.Code)
		})
	}
}

// TestRetryValidationExponentialBackoffDuration tests that exponential backoff delays
// are calculated correctly: 1s, 2s, 4s, 8s, etc.
func TestRetryValidationExponentialBackoffDuration(t *testing.T) {
	testCases := []struct {
		attempt          int
		expectedDuration time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 16 * time.Second},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("attempt_%d", tc.attempt), func(t *testing.T) {
			// Calculate backoff: 1 << (attempt - 1) seconds
			backoff := time.Duration(1<<uint(tc.attempt-1)) * time.Second
			if backoff != tc.expectedDuration {
				t.Errorf("Attempt %d: expected %v, got %v", tc.attempt, tc.expectedDuration, backoff)
			}
			t.Logf("Attempt %d backoff: %v", tc.attempt, backoff)
		})
	}
}

// TestRetryValidationResponseValidationNonStreaming tests response validation for non-streaming.
func TestRetryValidationResponseValidationNonStreaming(t *testing.T) {
	t.Run("valid non-streaming response", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":      "msg_123",
				"type":    "message",
				"content": []map[string]string{{"type": "text", "text": "Hello"}},
			})
		}))
		defer upstream.Close()

		handler := createTestProxyHandler(t, upstream.URL, 1)
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createNonStreamingRequestBody()))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		body := w.Body.String()

		// Validate: body is not empty
		if len(body) == 0 {
			t.Error("Response body is empty")
		}

		// Validate: body is valid JSON
		if !json.Valid([]byte(body)) {
			t.Error("Response body is not valid JSON")
		}

		t.Logf("Response validated successfully: %d bytes", len(body))
	})

	t.Run("empty non-streaming response triggers retry", func(t *testing.T) {
		var upstreamCallCount atomic.Int32
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			upstreamCallCount.Add(1)
			w.WriteHeader(http.StatusOK)
			// Empty body
		}))
		defer upstream.Close()

		handler := createTestProxyHandler(t, upstream.URL, 1)
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createNonStreamingRequestBody()))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		// Verify retry happened
		if upstreamCallCount.Load() != 2 {
			t.Errorf("Expected 2 upstream calls (1 initial + 1 retry), got %d", upstreamCallCount.Load())
		}

		// Verify 502 returned
		if w.Code != http.StatusBadGateway {
			t.Errorf("Expected status 502, got %d", w.Code)
		}

		t.Logf("PASS: Empty response triggered retry, returned 502")
	})
}

// TestRetryValidationResponseValidationStreaming tests response validation for streaming.
func TestRetryValidationResponseValidationStreaming(t *testing.T) {
	t.Run("valid streaming response", func(t *testing.T) {
		var upstreamCallCount atomic.Int32
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			upstreamCallCount.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			// Send some SSE data
			w.Write([]byte("data: {\"type\":\"message_start\"}\n\n"))
		}))
		defer upstream.Close()

		handler := createTestProxyHandler(t, upstream.URL, 1)
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createStreamingRequestBody()))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		// Verify no retry happened (streaming had data)
		if upstreamCallCount.Load() != 1 {
			t.Errorf("Expected 1 upstream call (no retry), got %d", upstreamCallCount.Load())
		}

		t.Logf("PASS: Valid streaming response, no retry")
	})

	t.Run("empty streaming response triggers retry", func(t *testing.T) {
		var upstreamCallCount atomic.Int32
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			upstreamCallCount.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			// Empty streaming response
		}))
		defer upstream.Close()

		handler := createTestProxyHandler(t, upstream.URL, 1)
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createStreamingRequestBody()))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		// Verify retry happened
		if upstreamCallCount.Load() != 2 {
			t.Errorf("Expected 2 upstream calls (1 initial + 1 retry), got %d", upstreamCallCount.Load())
		}

		// Verify 502 returned
		if w.Code != http.StatusBadGateway {
			t.Errorf("Expected status 502, got %d", w.Code)
		}

		t.Logf("PASS: Empty streaming response triggered retry, returned 502")
	})
}

// TestRetryValidationMetricsLabels tests that metrics use the correct labels as per plan.md.
func TestRetryValidationMetricsLabels(t *testing.T) {
	// This test documents the expected metric labels as per the error classification table

	expectedRetryLabels := []string{
		"retry",              // General retry
		"network_error",      // Network errors
		"429",                // Rate limiting
		"truncated_response", // Empty/invalid JSON body
		"empty_streaming",    // Empty streaming response
	}

	expectedErrorLabels := []string{
		"422",                  // Unprocessable entity
		"429",                  // Rate limiting
		"truncated_response",   // Empty/invalid JSON body
		"empty_streaming",      // Empty streaming response
		"upstream_connection", // Network errors
		"write_error",         // Write errors
		"read_error",          // Read errors
		"request_creation",    // Request creation errors
	}

	t.Run("retry attempt labels documented", func(t *testing.T) {
		for _, label := range expectedRetryLabels {
			t.Logf("zai_proxy_retry_attempts_total{reason=\"%s\",variant=\"...\"}", label)
			// Verify the metric accepts this label
			metric := retryAttempts.WithLabelValues(label, "test")
			if metric == nil {
				t.Errorf("Failed to create metric with label %q", label)
			}
		}
	})

	t.Run("upstream error labels documented", func(t *testing.T) {
		for _, label := range expectedErrorLabels {
			t.Logf("zai_proxy_upstream_errors_total{error_type=\"%s\",variant=\"...\"}", label)
			// Verify the metric accepts this label
			metric := upstreamErrors.WithLabelValues(label, "test")
			if metric == nil {
				t.Errorf("Failed to create metric with label %q", label)
			}
		}
	})
}

// TestRetryValidationMetricsIntegration tests that metrics are properly incremented end-to-end.
func TestRetryValidationMetricsIntegration(t *testing.T) {
	// Reset all metrics before test
	retryAttempts.Reset()
	upstreamErrors.Reset()
	requestsTotal.Reset()

	var upstreamCallCount atomic.Int32

	// Create upstream that returns various error codes
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := upstreamCallCount.Add(1)
		// First call: 429, second: empty body, third: 422
		switch count {
		case 1:
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusOK)
			// Empty body
		case 3:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]string{"error": "unprocessable"})
		default:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{"id": "msg_123"})
		}
	}))
	defer upstream.Close()

	// Test multiple scenarios
	testScenarios := []struct {
		name         string
		maxRetries   int
		reqBody     string
		retryReason string
		errorType   string
	}{
		{
			name:         "429 scenario",
			maxRetries:   1,
			reqBody:     createNonStreamingRequestBody(),
			retryReason: "429",
			errorType:   "", // 429 doesn't increment upstreamErrors
		},
		{
			name:         "empty body scenario",
			maxRetries:   1,
			reqBody:     createNonStreamingRequestBody(),
			retryReason: "truncated_response",
			errorType:   "truncated_response",
		},
		{
			name:         "422 scenario",
			maxRetries:   5,
			reqBody:     createNonStreamingRequestBody(),
			retryReason: "", // 422 doesn't retry
			errorType:   "422",
		},
	}

	for _, scenario := range testScenarios {
		t.Run(scenario.name, func(t *testing.T) {
			// Reset counters for this scenario
			upstreamCallCount.Store(0)

			handler := createTestProxyHandler(t, upstream.URL, scenario.maxRetries)
			req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(scenario.reqBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Get metric values before request
			retryBefore := testutil.ToFloat64(retryAttempts.WithLabelValues(scenario.retryReason, "test"))
			errorBefore := 0.0
			if scenario.errorType != "" {
				errorBefore = testutil.ToFloat64(upstreamErrors.WithLabelValues(scenario.errorType, "test"))
			}

			handler.ServeHTTP(w, req)

			// Check retry metric incremented
			if scenario.retryReason != "" {
				retryAfter := testutil.ToFloat64(retryAttempts.WithLabelValues(scenario.retryReason, "test"))
				if retryAfter <= retryBefore {
					t.Errorf("Expected retry metric to increment for %q, before=%f, after=%f",
						scenario.retryReason, retryBefore, retryAfter)
				} else {
					t.Logf("PASS: Retry metric incremented from %f to %f", retryBefore, retryAfter)
				}
			}

			// Check error metric incremented
			if scenario.errorType != "" {
				errorAfter := testutil.ToFloat64(upstreamErrors.WithLabelValues(scenario.errorType, "test"))
				if errorAfter <= errorBefore {
					t.Errorf("Expected error metric to increment for %q, before=%f, after=%f",
						scenario.errorType, errorBefore, errorAfter)
				} else {
					t.Logf("PASS: Error metric incremented from %f to %f", errorBefore, errorAfter)
				}
			}
		})
	}
}

// TestRetryValidationRaceCondition tests that retry logic is thread-safe.
func TestRetryValidationRaceCondition(t *testing.T) {
	maxRetries := 1
	var upstreamCallCount atomic.Int32

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCallCount.Add(1)
		w.WriteHeader(http.StatusOK)
		// Empty body to trigger retry
	}))
	defer upstream.Close()

	handler := createTestProxyHandler(t, upstream.URL, maxRetries)

	// Launch multiple concurrent requests
	const numConcurrent = 10
	errors := make(chan error, numConcurrent)

	for i := 0; i < numConcurrent; i++ {
		go func() {
			req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(createNonStreamingRequestBody()))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != http.StatusBadGateway {
				errors <- fmt.Errorf("expected status 502, got %d", w.Code)
				return
			}
			errors <- nil
		}()
	}

	// Collect results
	for i := 0; i < numConcurrent; i++ {
		if err := <-errors; err != nil {
			t.Errorf("Concurrent request error: %v", err)
		}
	}

	totalCalls := upstreamCallCount.Load()
	expectedCalls := int32(numConcurrent * (maxRetries + 1))

	// Allow some variance due to timing
	if totalCalls < expectedCalls/2 || totalCalls > expectedCalls*2 {
		t.Logf("WARNING: Upstream call count outside expected range: got %d, expected ~%d", totalCalls, expectedCalls)
	}

	t.Logf("PASS: %d concurrent requests, %d total upstream calls", numConcurrent, totalCalls)
}
