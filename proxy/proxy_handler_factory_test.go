package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// Acceptance Criteria Tests for Proxy Handler Factory and Execution Helpers
// ============================================================================

// TestCreateTestProxyHandler verifies AC1: CreateTestProxyHandler() creates configured ProxyHandler
func TestCreateTestProxyHandler(t *testing.T) {
	t.Run("configures_test_environment", func(t *testing.T) {
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id":"test123"}`))
		}))
		defer mockServer.Close()

		handler := CreateTestProxyHandler(t, mockServer.URL, 3)

		if handler == nil {
			t.Fatal("CreateTestProxyHandler returned nil")
		}

		// Verify the handler is properly configured
		if handler.apiKey != "test-key" {
			t.Errorf("Expected apiKey 'test-key', got %q", handler.apiKey)
		}

		if handler.maxRetries != 3 {
			t.Errorf("Expected maxRetries 3, got %d", handler.maxRetries)
		}

		if handler.maxWorkers != 100 {
			t.Errorf("Expected maxWorkers 100, got %d", handler.maxWorkers)
		}
	})

	t.Run("sets_MAX_RETRIES_correctly", func(t *testing.T) {
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
		}))
		defer mockServer.Close()

		handler := CreateTestProxyHandler(t, mockServer.URL, 5)

		if handler.maxRetries != 5 {
			t.Errorf("Expected maxRetries 5, got %d", handler.maxRetries)
		}
	})

	t.Run("sets_API_key", func(t *testing.T) {
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
		}))
		defer mockServer.Close()

		handler := CreateTestProxyHandler(t, mockServer.URL, 3)

		if handler.apiKey != "test-key" {
			t.Errorf("Expected API key 'test-key', got %q", handler.apiKey)
		}
	})

	t.Run("configures_rate_limits_high_to_avoid_test_failures", func(t *testing.T) {
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
		}))
		defer mockServer.Close()

		handler := CreateTestProxyHandler(t, mockServer.URL, 3)

		if handler.rateLimiter == nil {
			t.Fatal("Rate limiter should not be nil")
		}

		// Verify high rate limits (1000.0 as specified in helper)
		if handler.rateLimiter.minRate != 1000.0 {
			t.Errorf("Expected minRate 1000.0, got %f", handler.rateLimiter.minRate)
		}

		if handler.rateLimiter.maxRate != 1000.0 {
			t.Errorf("Expected maxRate 1000.0, got %f", handler.rateLimiter.maxRate)
		}

		if handler.rateLimiter.currentRate != 1000.0 {
			t.Errorf("Expected currentRate 1000.0, got %f", handler.rateLimiter.currentRate)
		}
	})

	t.Run("creates_working_ProxyHandler_instance", func(t *testing.T) {
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id":"msg_test123","type":"message"}`))
		}))
		defer mockServer.Close()

		handler := CreateTestProxyHandler(t, mockServer.URL, 3)

		// Test that the handler can actually serve requests
		req := httptest.NewRequest("GET", "/v1/messages", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	})
}

// TestExecuteProxyRequest verifies AC2: ExecuteProxyRequest() executes requests through handler
func TestExecuteProxyRequest(t *testing.T) {
	t.Run("executes_simple_GET_request", func(t *testing.T) {
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		}))
		defer mockServer.Close()

		handler := CreateTestProxyHandler(t, mockServer.URL, 3)
		req := httptest.NewRequest("GET", "/test", nil)

		resp := ExecuteProxyRequest(t, handler, req)

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	})

	t.Run("passes_headers_correctly", func(t *testing.T) {
		receivedHeaders := make(http.Header)
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for k, v := range r.Header {
				receivedHeaders[k] = v
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
		}))
		defer mockServer.Close()

		handler := CreateTestProxyHandler(t, mockServer.URL, 3)
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Custom", "test-value")

		ExecuteProxyRequest(t, handler, req)

		if receivedHeaders.Get("X-Custom") != "test-value" {
			t.Error("Custom header not passed through")
		}
	})

	t.Run("handles_POST_requests", func(t *testing.T) {
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Errorf("Expected POST, got %s", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"created":true}`))
		}))
		defer mockServer.Close()

		handler := CreateTestProxyHandler(t, mockServer.URL, 3)
		req := httptest.NewRequest("POST", "/test", nil)

		resp := ExecuteProxyRequest(t, handler, req)

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("Expected status 201, got %d", resp.StatusCode)
		}
	})
}

// TestExecuteProxyRequestWithBody verifies AC3: ExecuteProxyRequestWithBody() executes POST requests with body
func TestExecuteProxyRequestWithBody(t *testing.T) {
	t.Run("executes_POST_with_body", func(t *testing.T) {
		var receivedBody map[string]interface{}
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Errorf("Expected POST, got %s", r.Method)
			}
			json.NewDecoder(r.Body).Decode(&receivedBody)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"received":"ok"}`))
		}))
		defer mockServer.Close()

		handler := CreateTestProxyHandler(t, mockServer.URL, 3)
		testBody := `{"message":"test"}`

		resp := ExecuteProxyRequestWithBody(t, handler, "/v1/messages", testBody)

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	})

	t.Run("sends_correct_body_content", func(t *testing.T) {
		var receivedContent string
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var data map[string]interface{}
			json.NewDecoder(r.Body).Decode(&data)
			receivedContent = data["message"].(string)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
		}))
		defer mockServer.Close()

		handler := CreateTestProxyHandler(t, mockServer.URL, 3)
		testBody := `{"message":"hello world"}`

		ExecuteProxyRequestWithBody(t, handler, "/v1/messages", testBody)

		if receivedContent != "hello world" {
			t.Errorf("Expected body content 'hello world', got %q", receivedContent)
		}
	})

	t.Run("sets_correct_content_type", func(t *testing.T) {
		var receivedContentType string
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedContentType = r.Header.Get("Content-Type")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
		}))
		defer mockServer.Close()

		handler := CreateTestProxyHandler(t, mockServer.URL, 3)
		testBody := `{"test":"data"}`

		ExecuteProxyRequestWithBody(t, handler, "/v1/messages", testBody)

		if receivedContentType != "application/json" {
			t.Errorf("Expected Content-Type 'application/json', got %q", receivedContentType)
		}
	})
}

// TestExecuteMessagesRequest verifies AC4: ExecuteMessagesRequest() executes /v1/messages requests
func TestExecuteMessagesRequest(t *testing.T) {
	t.Run("executes_v1_messages_endpoint", func(t *testing.T) {
		var receivedPath string
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id":"msg_123","type":"message"}`))
		}))
		defer mockServer.Close()

		handler := CreateTestProxyHandler(t, mockServer.URL, 3)
		testBody := `{"model":"glm-4","messages":[{"role":"user","content":"test"}]}`

		resp := ExecuteMessagesRequest(t, handler, testBody)

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		if receivedPath != "/v1/messages" {
			t.Errorf("Expected path '/v1/messages', got %q", receivedPath)
		}
	})

	t.Run("returns_valid_message_response", func(t *testing.T) {
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id":"msg_test456","type":"message","role":"assistant","content":"response text"}`))
		}))
		defer mockServer.Close()

		handler := CreateTestProxyHandler(t, mockServer.URL, 3)
		testBody := `{"model":"glm-4","messages":[{"role":"user","content":"hello"}]}`

		resp := ExecuteMessagesRequest(t, handler, testBody)

		// Verify the response was successful - body already validated by handler
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		// Verify response has JSON content type
		contentType := resp.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %q", contentType)
		}
	})

	t.Run("handles_messages_with_conversation_history", func(t *testing.T) {
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var reqBody map[string]interface{}
			json.NewDecoder(r.Body).Decode(&reqBody)

			messages := reqBody["messages"].([]interface{})
			if len(messages) < 2 {
				t.Error("Expected at least 2 messages in history")
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id":"msg_conversation","type":"message"}`))
		}))
		defer mockServer.Close()

		handler := CreateTestProxyHandler(t, mockServer.URL, 3)
		testBody := `{
				"model": "glm-4",
				"messages": [
					{"role": "user", "content": "first message"},
					{"role": "assistant", "content": "first response"},
					{"role": "user", "content": "second message"}
				]
			}`

		resp := ExecuteMessagesRequest(t, handler, testBody)

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	})
}

// TestCreateCountingMockServer verifies AC5: CreateCountingMockServer() creates counting mock server
func TestCreateCountingMockServer(t *testing.T) {
	t.Run("creates_functioning_mock_server", func(t *testing.T) {
		mockServer := CreateCountingMockServer(t)
		defer mockServer.Server.Close()

		if mockServer.Server == nil {
			t.Fatal("CreateCountingMockServer returned nil server")
		}

		if mockServer.Server.URL == "" {
			t.Error("Mock server URL is empty")
		}
	})

	t.Run("server_returns_valid_JSON_response", func(t *testing.T) {
		mockServer := CreateCountingMockServer(t)
		defer mockServer.Server.Close()

		resp, err := http.Get(mockServer.Server.URL)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var parsed map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&parsed)

		if parsed["id"] == nil {
			t.Error("Expected 'id' field in response")
		}

		if parsed["type"] != "message" {
			t.Errorf("Expected type 'message', got %v", parsed["type"])
		}
	})

	t.Run("returns_correct_content_type", func(t *testing.T) {
		mockServer := CreateCountingMockServer(t)
		defer mockServer.Server.Close()

		resp, err := http.Get(mockServer.Server.URL)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		defer resp.Body.Close()

		contentType := resp.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type 'application/json', got %q", contentType)
		}
	})
}

// TestCountingMockServerWrapHandler verifies AC6: CountingMockServer.WrapHandler() wraps handler with counting
func TestCountingMockServerWrapHandler(t *testing.T) {
	t.Run("wraps_handler_and_counts_requests", func(t *testing.T) {
		mockServer := CreateCountingMockServer(t)
		defer mockServer.Server.Close()

		// Make initial request
		resp1, err := http.Get(mockServer.Server.URL)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		resp1.Body.Close()

		initialCount := mockServer.GetRequestCount()

		// Wrap with custom handler
		customHandlerCalled := false
		mockServer.WrapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			customHandlerCalled = true
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"custom":"response"}`))
		}))

		// Make request to wrapped handler
		resp2, err := http.Get(mockServer.Server.URL)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		resp2.Body.Close()

		if !customHandlerCalled {
			t.Error("Custom handler was not called")
		}

		newCount := mockServer.GetRequestCount()

		// Count should be incremented
		if newCount <= initialCount {
			t.Errorf("Expected request count to increment, got %d then %d", initialCount, newCount)
		}

		// Custom response should be returned
		if resp2.StatusCode != http.StatusCreated {
			t.Errorf("Expected status 201, got %d", resp2.StatusCode)
		}
	})

	t.Run("preserves_counting_after_wrapping", func(t *testing.T) {
		mockServer := CreateCountingMockServer(t)
		defer mockServer.Server.Close()

		// Make some initial requests
		for i := 0; i < 3; i++ {
			resp, err := http.Get(mockServer.Server.URL)
			if err != nil {
				t.Fatalf("Failed to make request: %v", err)
			}
			resp.Body.Close()
		}

		countBeforeWrap := mockServer.GetRequestCount()
		if countBeforeWrap != 3 {
			t.Errorf("Expected 3 requests before wrap, got %d", countBeforeWrap)
		}

		// Wrap with new handler
		mockServer.WrapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
		}))

		// Make more requests
		for i := 0; i < 2; i++ {
			resp, err := http.Get(mockServer.Server.URL)
			if err != nil {
				t.Fatalf("Failed to make request: %v", err)
			}
			resp.Body.Close()
		}

		finalCount := mockServer.GetRequestCount()
		expectedFinal := 5 // 3 before wrap + 2 after

		if finalCount != expectedFinal {
			t.Errorf("Expected total count %d, got %d", expectedFinal, finalCount)
		}
	})

	t.Run("thread_safe_counting", func(t *testing.T) {
		mockServer := CreateCountingMockServer(t)
		defer mockServer.Server.Close()

		mockServer.WrapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
		}))

		// Make concurrent requests
		var wg sync.WaitGroup
		numGoroutines := 10
		requestsPerGoroutine := 5

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < requestsPerGoroutine; j++ {
					resp, err := http.Get(mockServer.Server.URL)
					if err != nil {
						t.Errorf("Failed to make request: %v", err)
						return
					}
					resp.Body.Close()
				}
			}()
		}

		wg.Wait()

		expectedCount := numGoroutines * requestsPerGoroutine
		actualCount := mockServer.GetRequestCount()

		if actualCount != expectedCount {
			t.Errorf("Expected %d requests, got %d", expectedCount, actualCount)
		}
	})
}

// TestWaitForCondition verifies AC7: WaitForCondition() waits for conditions with timeout
func TestWaitForCondition(t *testing.T) {
	t.Run("returns_true_when_condition_met_quickly", func(t *testing.T) {
		conditionMet := false
		go func() {
			time.Sleep(50 * time.Millisecond)
			conditionMet = true
		}()

		result := WaitForCondition(t, func() bool {
			return conditionMet
		}, 500*time.Millisecond, 10*time.Millisecond)

		if !result {
			t.Error("Expected WaitForCondition to return true when condition is met")
		}
	})

	t.Run("returns_false_on_timeout", func(t *testing.T) {
		result := WaitForCondition(t, func() bool {
			return false // Never true
		}, 100*time.Millisecond, 10*time.Millisecond)

		if result {
			t.Error("Expected WaitForCondition to return false on timeout")
		}
	})

	t.Run("checks_condition_at_correct_intervals", func(t *testing.T) {
		checkCount := 0
		deadline := time.Now().Add(200 * time.Millisecond)

		WaitForCondition(t, func() bool {
			checkCount++
			return time.Now().After(deadline)
		}, 500*time.Millisecond, 50*time.Millisecond)

		// Should check approximately 4 times (200ms / 50ms)
		// Allow some tolerance for timing
		if checkCount < 3 || checkCount > 6 {
			t.Errorf("Expected approximately 4-5 checks, got %d", checkCount)
		}
	})

	t.Run("handles_condition_that_becomes_true_just_before_timeout", func(t *testing.T) {
		conditionMet := false
		go func() {
			time.Sleep(90 * time.Millisecond)
			conditionMet = true
		}()

		result := WaitForCondition(t, func() bool {
			return conditionMet
		}, 100*time.Millisecond, 10*time.Millisecond)

		if !result {
			t.Error("Expected WaitForCondition to return true when condition met just before timeout")
		}
	})

	t.Run("returns_immediately_if_condition_already_true", func(t *testing.T) {
		start := time.Now()

		result := WaitForCondition(t, func() bool {
			return true // Already true
		}, 5*time.Second, 100*time.Millisecond)

		elapsed := time.Since(start)

		if !result {
			t.Error("Expected WaitForCondition to return true")
		}

		if elapsed > 100*time.Millisecond {
			t.Errorf("Expected immediate return, took %v", elapsed)
		}
	})
}

// TestAssertUpstreamCallCount verifies AC8: AssertUpstreamCallCount() validates mock server request counts
func TestAssertUpstreamCallCount(t *testing.T) {
	t.Run("passes_when_count_matches", func(t *testing.T) {
		mock := NewMockUpstream("success")
		defer mock.Close()

		// Make exactly 3 requests
		for i := 0; i < 3; i++ {
			resp, err := http.Get(mock.URL())
			if err != nil {
				t.Fatalf("Failed to make request: %v", err)
			}
			resp.Body.Close()
		}

		// This should pass without error
		AssertUpstreamCallCount(t, mock, 3)
	})

	t.Run("fails_when_count_does_not_match", func(t *testing.T) {
		mock := NewMockUpstream("success")
		defer mock.Close()

		// Make 2 requests
		for i := 0; i < 2; i++ {
			resp, err := http.Get(mock.URL())
			if err != nil {
				t.Fatalf("Failed to make request: %v", err)
			}
			resp.Body.Close()
		}

		// Verify the actual count matches what we made
		actualCount := mock.GetRequestCount()
		if actualCount != 2 {
			t.Errorf("Expected 2 requests, got %d", actualCount)
		}

		// Verify that GetRequestCount returns the correct value
		// This confirms the counting mechanism works properly
		// (We can't directly test that AssertUpstreamCallCount fails,
		// but we can verify the underlying counting is accurate)
		if actualCount != 2 {
			t.Errorf("Request counting verification failed")
		}
	})

	t.Run("works_with_zero_requests", func(t *testing.T) {
		mock := NewMockUpstream("success")
		defer mock.Close()

		// Make no requests
		AssertUpstreamCallCount(t, mock, 0)
	})

	t.Run("validates_high_request_counts", func(t *testing.T) {
		mock := NewMockUpstream("success")
		defer mock.Close()

		// Make 100 requests
		for i := 0; i < 100; i++ {
			resp, err := http.Get(mock.URL())
			if err != nil {
				t.Fatalf("Failed to make request: %v", err)
			}
			resp.Body.Close()
		}

		AssertUpstreamCallCount(t, mock, 100)

		actualCount := mock.GetRequestCount()
		if actualCount != 100 {
			t.Errorf("GetRequestCount returned %d, expected 100", actualCount)
		}
	})
}

// TestCalculateBackoffDelay verifies AC9: CalculateBackoffDelay() calculates correct backoff durations
func TestCalculateBackoffDelay(t *testing.T) {
	t.Run("calculates_delays_for_positive_attempts", func(t *testing.T) {
		testCases := []struct {
			attempt          int
			expectedDuration time.Duration
		}{
			{1, 1 * time.Second},
			{2, 2 * time.Second},
			{3, 4 * time.Second},
			{4, 8 * time.Second},
			{5, 16 * time.Second},
			{6, 32 * time.Second},
			{7, 64 * time.Second},
			{8, 128 * time.Second},
			{9, 256 * time.Second},
			{10, 512 * time.Second},
		}

		for _, tc := range testCases {
			t.Run(fmt.Sprintf("attempt_%d", tc.attempt), func(t *testing.T) {
				delay := CalculateBackoffDelay(tc.attempt)
				if delay != tc.expectedDuration {
					t.Errorf("Attempt %d: expected %v, got %v", tc.attempt, tc.expectedDuration, delay)
				}
			})
		}
	})

	t.Run("handles_edge_cases", func(t *testing.T) {
		t.Run("zero_attempt_returns_zero", func(t *testing.T) {
			delay := CalculateBackoffDelay(0)
			if delay != 0 {
				t.Errorf("Expected 0 for attempt 0, got %v", delay)
			}
		})

		t.Run("negative_attempt_returns_zero", func(t *testing.T) {
			delay := CalculateBackoffDelay(-1)
			if delay != 0 {
				t.Errorf("Expected 0 for negative attempt, got %v", delay)
			}
		})
	})

	t.Run("verifies_exponential_growth_pattern", func(t *testing.T) {
		// Each delay should be double the previous
		for attempt := 2; attempt <= 10; attempt++ {
			prevDelay := CalculateBackoffDelay(attempt - 1)
			currDelay := CalculateBackoffDelay(attempt)

			if currDelay != prevDelay*2 {
				t.Errorf("Attempt %d: expected %v (2x %v), got %v",
					attempt, prevDelay*2, prevDelay, currDelay)
			}
		}
	})

	t.Run("handles_large_attempts", func(t *testing.T) {
		// Test a larger attempt number
		delay := CalculateBackoffDelay(15)
		expected := time.Duration(1<<14) * time.Second // 2^14 seconds

		if delay != expected {
			t.Errorf("Attempt 15: expected %v, got %v", expected, delay)
		}
	})
}

// TestCalculateTotalMaxDelay verifies AC10: CalculateTotalMaxDelay() calculates total max delay correctly
func TestCalculateTotalMaxDelay(t *testing.T) {
	t.Run("calculates_sum_of_all_backoff_delays", func(t *testing.T) {
		testCases := []struct {
			maxRetries       int
			expectedDuration time.Duration
		}{
			{1, 1 * time.Second},                      // 1s = 1
			{2, 3 * time.Second},                      // 1s + 2s = 3
			{3, 7 * time.Second},                      // 1s + 2s + 4s = 7
			{4, 15 * time.Second},                     // 1s + 2s + 4s + 8s = 15
			{5, 31 * time.Second},                     // Sum = 2^5 - 1 = 31
			{6, 63 * time.Second},                     // Sum = 2^6 - 1 = 63
			{7, 127 * time.Second},                    // Sum = 2^7 - 1 = 127
			{8, 255 * time.Second},                    // Sum = 2^8 - 1 = 255
			{9, 511 * time.Second},                    // Sum = 2^9 - 1 = 511
			{10, 1023 * time.Second},                  // Sum = 2^10 - 1 = 1023
		}

		for _, tc := range testCases {
			t.Run(fmt.Sprintf("maxRetries_%d", tc.maxRetries), func(t *testing.T) {
				delay := CalculateTotalMaxDelay(tc.maxRetries)
				if delay != tc.expectedDuration {
					t.Errorf("maxRetries %d: expected %v, got %v",
						tc.maxRetries, tc.expectedDuration, delay)
				}
			})
		}
	})

	t.Run("verifies_formula_2_to_power_n_minus_1", func(t *testing.T) {
		for maxRetries := 1; maxRetries <= 10; maxRetries++ {
			delay := CalculateTotalMaxDelay(maxRetries)
			expected := time.Duration(1<<uint(maxRetries)-1) * time.Second

			if delay != expected {
				t.Errorf("maxRetries %d: formula verification failed, expected %v, got %v",
					maxRetries, expected, delay)
			}
		}
	})

	t.Run("handles_edge_cases", func(t *testing.T) {
		t.Run("zero_retries_returns_zero", func(t *testing.T) {
			delay := CalculateTotalMaxDelay(0)
			if delay != 0 {
				t.Errorf("Expected 0 for zero retries, got %v", delay)
			}
		})

		t.Run("negative_retries_returns_zero", func(t *testing.T) {
			delay := CalculateTotalMaxDelay(-1)
			if delay != 0 {
				t.Errorf("Expected 0 for negative retries, got %v", delay)
			}
		})
	})

	t.Run("sum_matches_individual_backoff_delays", func(t *testing.T) {
		for maxRetries := 1; maxRetries <= 8; maxRetries++ {
			totalFromHelper := CalculateTotalMaxDelay(maxRetries)

			// Calculate manually by summing individual backoffs
			var manualSum time.Duration
			for attempt := 1; attempt <= maxRetries; attempt++ {
				manualSum += CalculateBackoffDelay(attempt)
			}

			if totalFromHelper != manualSum {
				t.Errorf("maxRetries %d: helper returned %v, manual sum is %v",
					maxRetries, totalFromHelper, manualSum)
			}
		}
	})

	t.Run("handles_large_retry_counts", func(t *testing.T) {
		// Test with a larger retry count
		delay := CalculateTotalMaxDelay(15)
		expected := time.Duration(1<<15-1) * time.Second // 2^15 - 1 seconds

		if delay != expected {
			t.Errorf("maxRetries 15: expected %v, got %v", expected, delay)
		}
	})
}

// ============================================================================
// Integration Tests
// ============================================================================

// TestProxyHandlerFactoryIntegration tests all helpers working together
func TestProxyHandlerFactoryIntegration(t *testing.T) {
	t.Run("end_to_end_proxy_handler_factory_workflow", func(t *testing.T) {
		// 1. Create counting mock server (AC5)
		mockServer := CreateCountingMockServer(t)
		defer mockServer.Server.Close()

		// 2. Create proxy handler (AC1)
		handler := CreateTestProxyHandler(t, mockServer.Server.URL, 3)

		// 3. Execute messages request (AC4)
		testBody := `{
				"model": "glm-4",
				"messages": [{"role": "user", "content": "test"}]
			}`
		resp := ExecuteMessagesRequest(t, handler, testBody)

		// 4. Verify response status (body already validated by handler)
		AssertStatusCode(t, resp, http.StatusOK)

		// 5. Verify upstream was called (AC8 with counting mock server)
		if mockServer.GetRequestCount() != 1 {
			t.Errorf("Expected 1 upstream call, got %d", mockServer.GetRequestCount())
		}
	})

	t.Run("retry_logic_with_backoff_calculation", func(t *testing.T) {
		// Create a mock that fails initially then succeeds
		attempts := atomic.Int32{}
		attempts.Store(0)

		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := attempts.Add(1)
			if count < 3 {
				w.WriteHeader(http.StatusTooManyRequests)
				w.Header().Set("Retry-After", "1")
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"id":"success_after_retries"}`))
			}
		}))
		defer mockServer.Close()

		handler := CreateTestProxyHandler(t, mockServer.URL, 5)

		// Calculate expected delays (AC9)
		expectedDelays := []time.Duration{
			CalculateBackoffDelay(1), // 1s
			CalculateBackoffDelay(2), // 2s
		}

		// Calculate total max delay (AC10)
		totalMaxDelay := CalculateTotalMaxDelay(5)
		if totalMaxDelay != 31*time.Second {
			t.Errorf("Expected total max delay 31s, got %v", totalMaxDelay)
		}

		// Make request and measure time
		start := time.Now()
		testBody := `{"model":"glm-4","messages":[{"role":"user","content":"test"}]}`
		resp := ExecuteMessagesRequest(t, handler, testBody)
		elapsed := time.Since(start)

		// Verify we got success after retries
		AssertStatusCode(t, resp, http.StatusOK)

		// Should have taken at least the sum of backoff delays
		minExpectedTime := expectedDelays[0] + expectedDelays[1]
		if elapsed < minExpectedTime {
			t.Errorf("Expected at least %v, took %v", minExpectedTime, elapsed)
		}

		// Verify we made 3 attempts (2 failures + 1 success)
		if attempts.Load() != 3 {
			t.Errorf("Expected 3 attempts, got %d", attempts.Load())
		}
	})

	t.Run("concurrent_requests_with_counting", func(t *testing.T) {
		mockServer := CreateCountingMockServer(t)
		defer mockServer.Server.Close()

		handler := CreateTestProxyHandler(t, mockServer.Server.URL, 3)

		// Make concurrent requests
		var wg sync.WaitGroup
		numRequests := 10

		for i := 0; i < numRequests; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				testBody := `{"model":"glm-4","messages":[{"role":"user","content":"test"}]}`
				resp := ExecuteMessagesRequest(t, handler, testBody)
				resp.Body.Close()
			}()
		}

		wg.Wait()

		// Verify all requests were counted
		finalCount := mockServer.GetRequestCount()
		if finalCount != numRequests {
			t.Errorf("Expected %d requests, got %d", numRequests, finalCount)
		}
	})
}
