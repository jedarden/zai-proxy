package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Test mode configuration
const (
	// TestModeEnv is the environment variable that enables test mode
	TestModeEnv = "ZAI_PROXY_TEST_MODE"

	// TestMaxRetriesEnv is the environment variable for MAX_RETRIES in tests
	TestMaxRetriesEnv = "MAX_RETRIES"

	// Default test retries for fast execution
	DefaultTestMaxRetries = 1
)

// IsTestMode returns true if we're running in test mode
func IsTestMode() bool {
	return os.Getenv(TestModeEnv) == "true" || testing.Testing()
}

// GetTestMaxRetries returns the MAX_RETRIES value for tests
// Uses DefaultTestMaxRetries (1) for fast test execution unless overridden
func GetTestMaxRetries() int {
	if val := os.Getenv(TestMaxRetriesEnv); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil && parsed >= 0 {
			return parsed
		}
	}
	return DefaultTestMaxRetries
}

// ConfigureTestEnv sets up environment variables for fast test execution
// This should be called at the beginning of tests that need fast retry behavior
func ConfigureTestEnv(t *testing.T) {
	t.Setenv(TestMaxRetriesEnv, strconv.Itoa(DefaultTestMaxRetries))
	t.Setenv(TestModeEnv, "true")
	t.Setenv("DEPLOYMENT_VARIANT", "test")
	t.Setenv("TOKEN_COUNTING_ENABLED", "false")

	// Set high rate limits to avoid rate limiting in tests
	t.Setenv("RATE_LIMIT_INITIAL", "1000")
	t.Setenv("RATE_LIMIT_MIN", "1000")
	t.Setenv("RATE_LIMIT_MAX", "1000")
}

// ============================================================================
// Mock Upstream Server
// ============================================================================

// MockUpstream is a configurable HTTP server that simulates the Z.AI upstream
// for testing retry logic and response validation.
type MockUpstream struct {
	server         *httptest.Server
	requestCount   atomic.Int32
	responseCount  atomic.Int32
	scenario      string
	failUntilCount atomic.Int32
	retryAfter    atomic.Value // stores string
	delayMs       atomic.Int32
	customHandler atomic.Value // stores optional custom http.HandlerFunc
}

// NewMockUpstream creates a new mock upstream server with the given scenario.
//
// Available scenarios:
//   - "429-with-retry-after-then-success": Return 429 with Retry-After for first N requests, then success
//   - "429-no-header-then-429": Always return 429 without Retry-After
//   - "empty-json-body": Return 200 OK but with empty body
//   - "invalid-json-body": Return 200 OK but with invalid JSON
//   - "empty-streaming": Return 200 OK but empty streaming response
//   - "422-no-retry": Return 422 (should not be retried)
//   - "500-no-retry": Return 500 (should not be retried)
//   - "404-no-retry": Return 404 (should not be retried)
//   - "network-error-then-success": Simulate network error for first N requests, then success
//   - "success": Return successful response immediately
//   - "malformed-streaming": Return streaming response with invalid SSE data
func NewMockUpstream(scenario string) *MockUpstream {
	mu := &MockUpstream{
		scenario: scenario,
	}
	mu.retryAfter.Store("")
	mu.delayMs.Store(0)

	mu.server = httptest.NewServer(http.HandlerFunc(mu.handler))
	return mu
}

// handler implements the mock upstream logic
func (mu *MockUpstream) handler(w http.ResponseWriter, r *http.Request) {
	// Check for custom handler override
	if customHandler := mu.customHandler.Load(); customHandler != nil {
		customHandler.(http.HandlerFunc)(w, r)
		return
	}

	count := mu.requestCount.Add(1)
	mu.responseCount.Add(1)

	// Add configured delay if any
	if delay := mu.delayMs.Load(); delay > 0 {
		time.Sleep(time.Duration(delay) * time.Millisecond)
	}

	switch mu.scenario {
	case "429-with-retry-after-then-success":
		// Return 429 with Retry-After for first N requests, then success
		if count <= mu.failUntilCount.Load() {
			w.Header().Set("Retry-After", mu.retryAfter.Load().(string))
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		mu.sendSuccessResponse(w)

	case "429-no-header-then-429":
		// Always return 429 without Retry-After
		w.WriteHeader(http.StatusTooManyRequests)

	case "empty-json-body":
		// Return 200 OK but with empty body
		w.WriteHeader(http.StatusOK)

	case "invalid-json-body":
		// Return 200 OK but with invalid JSON
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{invalid json"))

	case "empty-streaming":
		// Return 200 OK but empty streaming response
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Immediately close with no data

	case "422-no-retry":
		// Return 422 and log (should not be retried)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "unprocessable entity",
		})

	case "500-no-retry":
		// Return 500 internal server error (should not be retried)
		w.WriteHeader(http.StatusInternalServerError)

	case "404-no-retry":
		// Return 404 not found (should not be retried)
		w.WriteHeader(http.StatusNotFound)

	case "network-error-then-success":
		// Simulate network error by closing connection immediately
		if count <= mu.failUntilCount.Load() {
			// Hijack not available in httptest, so we just close
			// This will cause a client read error
			return
		}
		mu.sendSuccessResponse(w)

	case "success":
		mu.sendSuccessResponse(w)

	case "malformed-streaming":
		// Return streaming response with invalid SSE data
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not valid sse format\n"))

	default:
		mu.sendSuccessResponse(w)
	}
}

// sendSuccessResponse sends a successful API response
func (mu *MockUpstream) sendSuccessResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      "msg_test123",
		"type":    "message",
		"role":    "assistant",
		"content": []map[string]string{
			{"type": "text", "text": "Hello! How can I help you?"},
		},
		"usage": map[string]int{
			"input_tokens":  10,
			"output_tokens": 5,
		},
	})
}

// Close closes the mock server
func (mu *MockUpstream) Close() {
	if mu.server != nil {
		mu.server.Close()
	}
}

// URL returns the mock server's URL
func (mu *MockUpstream) URL() string {
	return mu.server.URL
}

// GetRequestCount returns the number of requests received
func (mu *MockUpstream) GetRequestCount() int {
	return int(mu.requestCount.Load())
}

// SetFailUntilCount configures the mock to fail until N requests have been made
func (mu *MockUpstream) SetFailUntilCount(count int32) {
	mu.failUntilCount.Store(count)
}

// SetRetryAfter sets the Retry-After header value for 429 responses
func (mu *MockUpstream) SetRetryAfter(seconds string) {
	mu.retryAfter.Store(seconds)
}

// SetDelayMs sets a delay in milliseconds before responding
func (mu *MockUpstream) SetDelayMs(ms int32) {
	mu.delayMs.Store(ms)
}

// SetCustomHandler sets a custom handler function for the mock server
// This overrides the default scenario-based behavior
func (mu *MockUpstream) SetCustomHandler(handler http.HandlerFunc) {
	mu.customHandler.Store(handler)
}

// ResetCounters resets all request counters
func (mu *MockUpstream) ResetCounters() {
	mu.requestCount.Store(0)
	mu.responseCount.Store(0)
}

// ============================================================================
// Test Request Builders
// ============================================================================

// CreateProxyRequest creates a test HTTP request for the proxy
func CreateProxyRequest(method, url string, body string) *http.Request {
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, url, bodyReader)
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", "application/json")
	return req
}

// CreateMessagesRequest creates a test POST /v1/messages request
func CreateMessagesRequest(body string) *http.Request {
	return CreateProxyRequest("POST", "/v1/messages", body)
}

// CreateStreamingRequestBody creates a streaming request body
func CreateStreamingRequestBody() string {
	return `{"model":"glm-4","messages":[{"role":"user","content":"Hello"}],"stream":true}`
}

// CreateNonStreamingRequestBody creates a non-streaming request body
func CreateNonStreamingRequestBody() string {
	return `{"model":"glm-4","messages":[{"role":"user","content":"Hello"}]}`
}

// CreateStreamingMessagesRequest creates a test streaming request
func CreateStreamingMessagesRequest() *http.Request {
	return CreateMessagesRequest(CreateStreamingRequestBody())
}

// CreateNonStreamingMessagesRequest creates a test non-streaming request
func CreateNonStreamingMessagesRequest() *http.Request {
	return CreateMessagesRequest(CreateNonStreamingRequestBody())
}

// ============================================================================
// Response Assertions
// ============================================================================

// AssertStatusCode asserts that the response has the expected status code
func AssertStatusCode(t *testing.T, resp *http.Response, expectedCode int) {
	t.Helper()
	if resp.StatusCode != expectedCode {
		t.Errorf("Expected status code %d, got %d", expectedCode, resp.StatusCode)
	}
}

// AssertJSONBody asserts that the response body is valid JSON
func AssertJSONBody(t *testing.T, resp *http.Response) bool {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Failed to read response body: %v", err)
		return false
	}
	if !json.Valid(body) {
		t.Errorf("Response body is not valid JSON: %s", string(body))
		return false
	}
	return true
}

// AssertResponseField asserts that a JSON response contains a field with the expected value
func AssertResponseField(t *testing.T, resp *http.Response, field string, expectedValue interface{}) {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("Failed to parse response JSON: %v", err)
	}

	actualValue, exists := parsed[field]
	if !exists {
		t.Errorf("Response missing field '%s'", field)
		return
	}

	if actualValue != expectedValue {
		t.Errorf("Field '%s': expected %v, got %v", field, expectedValue, actualValue)
	}
}

// AssertEmptyBody asserts that the response body is empty
func AssertEmptyBody(t *testing.T, resp *http.Response) {
	t.Helper()
	body, _ := io.ReadAll(resp.Body)
	if len(body) != 0 {
		t.Errorf("Expected empty body, got %d bytes: %s", len(body), string(body))
	}
}

// AssertHeader asserts that the response has a specific header value
func AssertHeader(t *testing.T, resp *http.Response, header, expectedValue string) {
	t.Helper()
	actualValue := resp.Header.Get(header)
	if actualValue != expectedValue {
		t.Errorf("Header '%s': expected %q, got %q", header, expectedValue, actualValue)
	}
}

// AssertContentType asserts that the response has a specific Content-Type
func AssertContentType(t *testing.T, resp *http.Response, contentType string) {
	t.Helper()
	AssertHeader(t, resp, "Content-Type", contentType)
}

// ============================================================================
// Proxy Handler Factory
// ============================================================================

// CreateTestProxyHandler creates a ProxyHandler instance configured for testing
func CreateTestProxyHandler(t *testing.T, upstreamURL string, maxRetries int) *ProxyHandler {
	t.Helper()

	// Configure test environment
	ConfigureTestEnv(t)
	t.Setenv(TestMaxRetriesEnv, strconv.Itoa(maxRetries))
	t.Setenv("ZAI_API_KEY", "test-key")

	return NewProxyHandler(
		"test-key",
		upstreamURL,
		maxRetries,
		100,     // maxWorkers
		"test",  // deploymentVariant
		nil,     // tokenCounter (disabled)
		"glm-4", // tokenizerModel
		1000.0,  // initialRate (high to avoid rate limiting)
		1000.0,  // minRate
		1000.0,  // maxRate
	)
}

// ============================================================================
// Test Execution Helpers
// ============================================================================

// ExecuteProxyRequest executes a request through the proxy handler and returns the response
func ExecuteProxyRequest(t *testing.T, handler *ProxyHandler, req *http.Request) *http.Response {
	t.Helper()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w.Result()
}

// ExecuteProxyRequestWithBody executes a POST request with a body through the proxy
func ExecuteProxyRequestWithBody(t *testing.T, handler *ProxyHandler, url, body string) *http.Response {
	t.Helper()
	req := CreateProxyRequest("POST", url, body)
	return ExecuteProxyRequest(t, handler, req)
}

// ExecuteMessagesRequest executes a /v1/messages request through the proxy
func ExecuteMessagesRequest(t *testing.T, handler *ProxyHandler, body string) *http.Response {
	t.Helper()
	return ExecuteProxyRequestWithBody(t, handler, "/v1/messages", body)
}

// ============================================================================
// Mock Server Helpers
// ============================================================================

// CreateCountingMockServer creates a mock server that counts requests and returns success
func CreateCountingMockServer(t *testing.T) *CountingMockServer {
	t.Helper()
	return &CountingMockServer{
		Server: httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":   "msg_test123",
				"type": "message",
			})
		})),
	}
}

// CountingMockServer wraps httptest.Server with request counting
type CountingMockServer struct {
	*httptest.Server
	requestCount atomic.Int32
}

// WrapHandler wraps the existing handler with counting
func (cms *CountingMockServer) WrapHandler(handler http.HandlerFunc) {
	cms.Server.Close()
	cms.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cms.requestCount.Add(1)
		handler(w, r)
	}))
}

// GetRequestCount returns the number of requests received
func (cms *CountingMockServer) GetRequestCount() int {
	return int(cms.requestCount.Load())
}

// ============================================================================
// Response Reader Helpers
// ============================================================================

// ReadResponseBody reads and returns the response body
func ReadResponseBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	return body
}

// ReadJSONResponse reads and parses the response body as JSON
func ReadJSONResponse(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	body := ReadResponseBody(t, resp)
	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("Failed to parse response JSON: %v", err)
	}
	return parsed
}

// ============================================================================
// Duration Assertions
// ============================================================================

// AssertDurationAtLeast asserts that a duration is at least the expected minimum
func AssertDurationAtLeast(t *testing.T, actual, minimum time.Duration, msg string) {
	t.Helper()
	if actual < minimum {
		t.Errorf("%s: expected duration >= %v, got %v", msg, minimum, actual)
	}
}

// AssertDurationInRange asserts that a duration is within the expected range
func AssertDurationInRange(t *testing.T, actual, min, max time.Duration, msg string) {
	t.Helper()
	if actual < min || actual > max {
		t.Errorf("%s: expected duration in range [%v, %v], got %v", msg, min, max, actual)
	}
}

// ============================================================================
// Metrics Test Helpers
// ============================================================================

// ResetMetrics resets Prometheus metrics before a test
func ResetMetrics(t *testing.T) {
	t.Helper()
	// This would reset metrics if we had a global reset function
	// For now, tests can use testutil.CollectAndCount() to verify metrics
}

// ============================================================================
// Backoff Delay Helpers for Tests
// ============================================================================

// CalculateBackoffDelay calculates the backoff delay for a given attempt
// This matches the production logic: 1<<uint(attempt-1) seconds
func CalculateBackoffDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	return time.Duration(1<<uint(attempt-1)) * time.Second
}

// CalculateTotalMaxDelay calculates the total maximum delay for a given maxRetries
// This is the sum of all backoff delays: 1s + 2s + 4s + ... = 2^maxRetries - 1 seconds
func CalculateTotalMaxDelay(maxRetries int) time.Duration {
	if maxRetries <= 0 {
		return 0
	}
	return time.Duration(1<<uint(maxRetries)-1) * time.Second
}

// ============================================================================
// Integration Test Helpers
// ============================================================================

// WaitForCondition waits for a condition to become true, with timeout
func WaitForCondition(t *testing.T, condition func() bool, timeout time.Duration, checkInterval time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(checkInterval)
	}
	return false
}

// AssertUpstreamCallCount asserts that the mock server received the expected number of requests
func AssertUpstreamCallCount(t *testing.T, mock *MockUpstream, expectedCount int) {
	t.Helper()
	actualCount := mock.GetRequestCount()
	if actualCount != expectedCount {
		t.Errorf("Expected %d upstream requests, got %d", expectedCount, actualCount)
	}
}

// ============================================================================
// Body Parser Test Helpers
// ============================================================================

// CreateTestRequestBody creates a test request body with the given parameters
func CreateTestRequestBody(model string, messages []map[string]string, stream bool) (string, error) {
	reqBody := map[string]interface{}{
		"model":    model,
		"messages": messages,
	}
	if stream {
		reqBody["stream"] = true
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}
	return string(bodyBytes), nil
}

// MustCreateTestRequestBody creates a test request body, panicking on error
func MustCreateTestRequestBody(model string, messages []map[string]string, stream bool) string {
	body, err := CreateTestRequestBody(model, messages, stream)
	if err != nil {
		panic(err)
	}
	return body
}

// ============================================================================
// Test Helpers for Helper Validation
// ============================================================================

// TestHelperFunctions validates that the helper functions work correctly
func TestHelperFunctions(t *testing.T) {
	t.Run("IsTestMode", func(t *testing.T) {
		// When called from a test, should return true
		if !IsTestMode() {
			t.Error("IsTestMode should return true during test execution")
		}
	})

	t.Run("GetTestMaxRetries", func(t *testing.T) {
	 retries := GetTestMaxRetries()
		if retries != DefaultTestMaxRetries {
			t.Errorf("Expected DefaultTestMaxRetries=%d, got %d", DefaultTestMaxRetries, retries)
		}
	})

	t.Run("CalculateBackoffDelay", func(t *testing.T) {
		testCases := []struct {
			attempt   int
			expected  time.Duration
		}{
			{1, 1 * time.Second},
			{2, 2 * time.Second},
			{3, 4 * time.Second},
			{4, 8 * time.Second},
		}
		for _, tc := range testCases {
			t.Run(fmt.Sprintf("attempt_%d", tc.attempt), func(t *testing.T) {
				actual := CalculateBackoffDelay(tc.attempt)
				if actual != tc.expected {
					t.Errorf("Expected %v, got %v", tc.expected, actual)
				}
			})
		}
	})

	t.Run("CreateNonStreamingMessagesRequest", func(t *testing.T) {
		req := CreateNonStreamingMessagesRequest()
		if req == nil {
			t.Error("Request should not be nil")
		}
		if req.Method != "POST" {
			t.Errorf("Expected POST, got %s", req.Method)
		}
		if req.URL.Path != "/v1/messages" {
			t.Errorf("Expected path /v1/messages, got %s", req.URL.Path)
		}
	})

	t.Run("CreateStreamingMessagesRequest", func(t *testing.T) {
		req := CreateStreamingMessagesRequest()
		if req == nil {
			t.Error("Request should not be nil")
		}
		body, _ := io.ReadAll(req.Body)
		if !strings.Contains(string(body), `"stream":true`) {
			t.Error("Streaming request should contain stream:true")
		}
	})
}

// TestMockUpstreamBasic tests basic mock upstream functionality
func TestMockUpstreamBasic(t *testing.T) {
	t.Run("success scenario", func(t *testing.T) {
		mock := NewMockUpstream("success")
		defer mock.Close()

		resp, err := http.Get(mock.URL() + "/v1/messages")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		AssertStatusCode(t, resp, http.StatusOK)
		AssertJSONBody(t, resp)

		if count := mock.GetRequestCount(); count != 1 {
			t.Errorf("Expected 1 request, got %d", count)
		}
	})

	t.Run("429 scenario", func(t *testing.T) {
		mock := NewMockUpstream("429-no-header-then-429")
		defer mock.Close()

		resp, err := http.Get(mock.URL() + "/v1/messages")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		AssertStatusCode(t, resp, http.StatusTooManyRequests)
	})
}
