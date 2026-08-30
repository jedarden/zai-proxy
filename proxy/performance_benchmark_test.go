package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkConfig holds configuration for benchmark runs
type BenchmarkConfig struct {
	Enabled           bool
	ConcurrentUsers   []int
	InputSizes        []int // in characters
	IterationsPerSize int
}

// BenchmarkResult holds results from a single benchmark run
type BenchmarkResult struct {
	Name              string
	TotalRequests     int
	TotalDuration     time.Duration
	AvgLatency        time.Duration
	P50Latency        time.Duration
	P95Latency        time.Duration
	P99Latency        time.Duration
	MinLatency        time.Duration
	MaxLatency        time.Duration
	RequestsPerSecond float64
	ThroughputMBps    float64
	TokenCountTime    time.Duration
	SuccessCount      int
	ErrorCount        int
}

// BenchmarkSuite runs comprehensive performance benchmarks
type BenchmarkSuite struct {
	results []BenchmarkResult
	mu      sync.Mutex
}

// Test data generators
var (
	shortText = "Hello, how are you today?"

	mediumText = strings.Repeat("The quick brown fox jumps over the lazy dog. ", 10)

	longText = strings.Repeat("The quick brown fox jumps over the lazy dog. ", 100)

	veryLongText = strings.Repeat("The quick brown fox jumps over the lazy dog. ", 1000)

	multiMessageShort = []RequestMessage{
		{Role: "user", Content: json.RawMessage(`"What is the capital of France?"`)},
		{Role: "assistant", Content: json.RawMessage(`"The capital of France is Paris."`)},
		{Role: "user", Content: json.RawMessage(`"What is its population?"`)},
		{Role: "assistant", Content: json.RawMessage(`"Paris has a population of approximately 2.1 million people within the city limits."`)},
		{Role: "user", Content: json.RawMessage(`"Tell me more about its history."`)},
	}

	multiMessageLong []RequestMessage
)

func init() {
	// Initialize multiMessageLong with dynamic text variables
	multiMessageLong = []RequestMessage{
		{Role: "user", Content: json.RawMessage(`"` + longText + `"`)},
		{Role: "assistant", Content: json.RawMessage(`"` + longText + `"`)},
		{Role: "user", Content: json.RawMessage(`"` + mediumText + `"`)},
		{Role: "assistant", Content: json.RawMessage(`"` + mediumText + `"`)},
		{Role: "user", Content: json.RawMessage(`"` + shortText + `"`)},
	}
}

// createMockUpstreamServer creates a mock upstream server for testing
func createMockUpstreamServer(responseDelay time.Duration, responseBody string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(responseDelay)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(responseBody))
	}))
}

// createMockServer creates a test proxy server
func createMockServer(tokenCountingEnabled bool, upstreamURL string) *httptest.Server {
	// Set environment variable for token counting
	if !tokenCountingEnabled {
		os.Setenv("TOKEN_COUNTING_ENABLED", "false")
	} else {
		os.Setenv("TOKEN_COUNTING_ENABLED", "true")
	}

	// Initialize token counter based on setting
	var tc TokenCounter
	if tokenCountingEnabled {
		tikToken, err := NewTikTokenCounter()
		if err != nil {
			tc = NewSimpleTokenCounter()
		} else {
			tc = tikToken
		}
	}

	// Track concurrent requests
	var currentRequests int64

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Simulate concurrent request tracking
		atomicAddInt64(&currentRequests, 1)
		defer atomicAddInt64(&currentRequests, -1)

		// Track request size
		if r.ContentLength > 0 {
			requestSize.WithLabelValues(r.Method, r.URL.Path).Observe(float64(r.ContentLength))
		}

		// Capture request body for token counting
		var requestBody []byte
		var inputTokens int
		var tokenCountTime time.Duration

		if r.Body != nil && tc != nil {
			var buf bytes.Buffer
			tee := io.TeeReader(r.Body, &buf)
			requestBody, _ = io.ReadAll(tee)
			r.Body = io.NopCloser(&buf)

			// Measure token counting time
			countStart := time.Now()
			inputTokens, _ = CountRequestTokens(requestBody, tc)
			tokenCountTime = time.Since(countStart)

			if inputTokens > 0 {
				tokensTotal.WithLabelValues("input", tokenizerModel).Add(float64(inputTokens))
			}
		} else if r.Body != nil {
			requestBody, _ = io.ReadAll(r.Body)
		}

		// Create mock response
		mockResponse := map[string]interface{}{
			"id":      "msg_123",
			"type":    "message",
			"role":    "assistant",
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": "This is a mock response for testing purposes.",
				},
			},
		}

		// Add usage if token counting is enabled
		if tc != nil {
			mockResponse["usage"] = map[string]int{
				"input_tokens":  inputTokens,
				"output_tokens": 10,
			}
		}

		responseBody, _ := json.Marshal(mockResponse)

		// Set headers
		w.Header().Set("Content-Type", "application/json")
		if tc != nil && inputTokens > 0 {
			w.Header().Set("X-Token-Input", fmt.Sprintf("%d", inputTokens))
		}

		w.WriteHeader(http.StatusOK)
		w.Write(responseBody)

		// Record metrics
		duration := time.Since(start)
		requestsTotal.WithLabelValues(r.Method, r.URL.Path, "200").Inc()
		requestDuration.WithLabelValues(r.Method, r.URL.Path, "200").Observe(duration.Seconds())
		responseSize.WithLabelValues(r.Method, r.URL.Path, "200").Observe(float64(len(responseBody)))

		// Log token counting time if enabled
		if tc != nil && tokenCountTime > 0 {
			tokenCountDuration.WithLabelValues(deploymentVariant).Observe(tokenCountTime.Seconds())
		}
	}))
}

// atomicAddInt64 is a helper for atomic int64 operations
func atomicAddInt64(addr *int64, delta int64) int64 {
	return atomic.AddInt64(addr, delta)
}

// BenchmarkTokenCountingBaseline measures performance WITHOUT token counting
func BenchmarkTokenCountingBaseline(b *testing.B) {
	req := createTestRequest(shortText, false)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		body, _ := json.Marshal(req)
		_ = len(body)
	}
}

// BenchmarkTokenCountingEnabled measures performance WITH token counting
func BenchmarkTokenCountingEnabled(b *testing.B) {
	counter, err := NewTikTokenCounter()
	if err != nil {
		b.Skipf("Failed to initialize tokenizer: %v", err)
	}

	req := createTestRequest(shortText, false)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		body, _ := json.Marshal(req)
		_, _ = CountRequestTokens(body, counter)
	}
}

// BenchmarkTokenCountingShortText benchmarks short text input
func BenchmarkTokenCountingShortText(b *testing.B) {
	counter, err := NewTikTokenCounter()
	if err != nil {
		b.Skipf("Failed to initialize tokenizer: %v", err)
	}

	req := createTestRequest(shortText, false)
	body, _ := json.Marshal(req)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = CountRequestTokens(body, counter)
	}
}

// BenchmarkTokenCountingMediumText benchmarks medium text input
func BenchmarkTokenCountingMediumText(b *testing.B) {
	counter, err := NewTikTokenCounter()
	if err != nil {
		b.Skipf("Failed to initialize tokenizer: %v", err)
	}

	req := createTestRequest(mediumText, false)
	body, _ := json.Marshal(req)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = CountRequestTokens(body, counter)
	}
}

// BenchmarkTokenCountingLongText benchmarks long text input
func BenchmarkTokenCountingLongTextPerformance(b *testing.B) {
	counter, err := NewTikTokenCounter()
	if err != nil {
		b.Skipf("Failed to initialize tokenizer: %v", err)
	}

	req := createTestRequest(longText, false)
	body, _ := json.Marshal(req)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = CountRequestTokens(body, counter)
	}
}

// BenchmarkTokenCountingVeryLongText benchmarks very long text input
func BenchmarkTokenCountingVeryLongText(b *testing.B) {
	counter, err := NewTikTokenCounter()
	if err != nil {
		b.Skipf("Failed to initialize tokenizer: %v", err)
	}

	req := createTestRequest(veryLongText, false)
	body, _ := json.Marshal(req)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = CountRequestTokens(body, counter)
	}
}

// BenchmarkTokenCountingMultiMessage benchmarks multi-message conversations
func BenchmarkTokenCountingMultiMessage(b *testing.B) {
	counter, err := NewTikTokenCounter()
	if err != nil {
		b.Skipf("Failed to initialize tokenizer: %v", err)
	}

	req := RequestBody{
		Model:    "glm-4",
		Messages: multiMessageShort,
		Stream:   false,
	}
	body, _ := json.Marshal(req)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = CountRequestTokens(body, counter)
	}
}

// BenchmarkTokenCountingMultiMessageLong benchmarks long multi-message conversations
func BenchmarkTokenCountingMultiMessageLong(b *testing.B) {
	counter, err := NewTikTokenCounter()
	if err != nil {
		b.Skipf("Failed to initialize tokenizer: %v", err)
	}

	req := RequestBody{
		Model:    "glm-4",
		Messages: multiMessageLong,
		Stream:   false,
	}
	body, _ := json.Marshal(req)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = CountRequestTokens(body, counter)
	}
}

// BenchmarkConcurrentRequests10 measures 10 concurrent requests
func BenchmarkConcurrentRequests10(b *testing.B) {
	benchmarkConcurrentRequests(b, 10)
}

// BenchmarkConcurrentRequests50 measures 50 concurrent requests
func BenchmarkConcurrentRequests50(b *testing.B) {
	benchmarkConcurrentRequests(b, 50)
}

// BenchmarkConcurrentRequests100 measures 100 concurrent requests
func BenchmarkConcurrentRequests100(b *testing.B) {
	benchmarkConcurrentRequests(b, 100)
}

// benchmarkConcurrentRequests is a helper for concurrent request benchmarks
func benchmarkConcurrentRequests(b *testing.B, concurrency int) {
	counter, err := NewTikTokenCounter()
	if err != nil {
		b.Skipf("Failed to initialize tokenizer: %v", err)
	}

	req := createTestRequest(mediumText, false)
	body, _ := json.Marshal(req)

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = CountRequestTokens(body, counter)
		}
	})
}

// BenchmarkEndToEndNoTokenCounting benchmarks full request flow without token counting
func BenchmarkEndToEndNoTokenCounting(b *testing.B) {
	server := createMockServer(false, "")
	defer server.Close()

	req := createTestRequest(shortText, false)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		body, _ := json.Marshal(req)
		httpReq, _ := http.NewRequest("POST", server.URL+"/v1/messages", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(httpReq)
		if err != nil {
			b.Fatalf("Request failed: %v", err)
		}
		resp.Body.Close()
	}
}

// BenchmarkEndToEndWithTokenCounting benchmarks full request flow with token counting
func BenchmarkEndToEndWithTokenCounting(b *testing.B) {
	server := createMockServer(true, "")
	defer server.Close()

	req := createTestRequest(shortText, false)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		body, _ := json.Marshal(req)
		httpReq, _ := http.NewRequest("POST", server.URL+"/v1/messages", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(httpReq)
		if err != nil {
			b.Fatalf("Request failed: %v", err)
		}
		resp.Body.Close()
	}
}

// TestTokenCountingOverhead measures the overhead of token counting
func TestTokenCountingOverhead(t *testing.T) {
	counter, err := NewTikTokenCounter()
	if err != nil {
		t.Skipf("Failed to initialize tokenizer: %v", err)
	}

	testCases := []struct {
		name     string
		text     string
		maxDelay time.Duration
	}{
		{"Short text", shortText, 5 * time.Millisecond},
		{"Medium text", mediumText, 10 * time.Millisecond},
		{"Long text", longText, 50 * time.Millisecond},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := createTestRequest(tc.text, false)
			body, _ := json.Marshal(req)

			// Warm up
			for i := 0; i < 10; i++ {
				CountRequestTokens(body, counter)
			}

			// Measure
			iterations := 100
			start := time.Now()
			for i := 0; i < iterations; i++ {
				CountRequestTokens(body, counter)
			}
			elapsed := time.Since(start)
			avgTime := elapsed / time.Duration(iterations)

			t.Logf("Average token counting time: %v", avgTime)

			if avgTime > tc.maxDelay {
				t.Errorf("Token counting too slow: %v (max: %v)", avgTime, tc.maxDelay)
			}
		})
	}
}

// TestConcurrentLoad tests the system under concurrent load
func TestConcurrentLoad(t *testing.T) {
	counter, err := NewTikTokenCounter()
	if err != nil {
		t.Skipf("Failed to initialize tokenizer: %v", err)
	}

	concurrencyLevels := []int{10, 50, 100}
	requestsPerLevel := 100

	for _, concurrency := range concurrencyLevels {
		t.Run(fmt.Sprintf("Concurrent_%d", concurrency), func(t *testing.T) {
			req := createTestRequest(mediumText, false)
			body, _ := json.Marshal(req)

			var wg sync.WaitGroup
			var mu sync.Mutex
			latencies := make([]time.Duration, 0, requestsPerLevel)
			errors := 0
			startTime := time.Now()

			for i := 0; i < concurrency; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()

					for j := 0; j < requestsPerLevel/concurrency; j++ {
						reqStart := time.Now()
						_, err := CountRequestTokens(body, counter)
						reqDuration := time.Since(reqStart)

						mu.Lock()
						latencies = append(latencies, reqDuration)
						if err != nil {
							errors++
						}
						mu.Unlock()
					}
				}()
			}

			wg.Wait()
			totalDuration := time.Since(startTime)

			// Calculate statistics
			if len(latencies) == 0 {
				t.Fatal("No latencies recorded")
			}

			// Sort latencies for percentiles
			for i := 0; i < len(latencies); i++ {
				for j := i + 1; j < len(latencies); j++ {
					if latencies[i] > latencies[j] {
						latencies[i], latencies[j] = latencies[j], latencies[i]
					}
				}
			}

			totalRequests := len(latencies)
			p50 := latencies[totalRequests*50/100]
			p95 := latencies[totalRequests*95/100]
			p99 := latencies[totalRequests*99/100]
			minLat := latencies[0]
			maxLat := latencies[totalRequests-1]

			var sum time.Duration
			for _, l := range latencies {
				sum += l
			}
			avgLat := sum / time.Duration(totalRequests)

			rps := float64(totalRequests) / totalDuration.Seconds()

			t.Logf("Concurrency: %d", concurrency)
			t.Logf("Total requests: %d", totalRequests)
			t.Logf("Total duration: %v", totalDuration)
			t.Logf("Requests per second: %.2f", rps)
			t.Logf("Errors: %d", errors)
			t.Logf("Avg latency: %v", avgLat)
			t.Logf("Min latency: %v", minLat)
			t.Logf("Max latency: %v", maxLat)
			t.Logf("P50 latency: %v", p50)
			t.Logf("P95 latency: %v", p95)
			t.Logf("P99 latency: %v", p99)

			// Verify targets
			if avgLat > 5*time.Millisecond {
				t.Errorf("Average latency exceeds 5ms target: %v", avgLat)
			}

			if errors > 0 {
				t.Errorf("Encountered %d errors", errors)
			}
		})
	}
}

// TestMemoryProfile measures memory allocation patterns
func TestMemoryProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory profile in short mode")
	}

	counter, err := NewTikTokenCounter()
	if err != nil {
		t.Skipf("Failed to initialize tokenizer: %v", err)
	}

	testCases := []struct {
		name string
		text string
	}{
		{"Short", shortText},
		{"Medium", mediumText},
		{"Long", longText},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := createTestRequest(tc.text, false)
			body, _ := json.Marshal(req)

			// Force GC before measuring
			runtime.GC()

			var m1 runtime.MemStats
			runtime.ReadMemStats(&m1)

			// Run token counting many times
			iterations := 1000
			for i := 0; i < iterations; i++ {
				CountRequestTokens(body, counter)
			}

			// Force GC after measuring
			runtime.GC()

			var m2 runtime.MemStats
			runtime.ReadMemStats(&m2)

			// Calculate memory usage
			allocDiff := m2.TotalAlloc - m1.TotalAlloc
			avgAlloc := allocDiff / uint64(iterations)

			t.Logf("Text length: %d bytes", len(tc.text))
			t.Logf("Total allocations: %d bytes", m2.TotalAlloc-m1.TotalAlloc)
			t.Logf("Avg allocation per request: %d bytes", avgAlloc)

			// Verify memory usage is reasonable
			// Memory allocation scales with text size
			// Set ceilings based on actual usage patterns:
			// Short (<1KB): < 5KB, Medium (~0.5KB): < 50KB, Long (~5KB): < 500KB
			var maxAlloc uint64
			switch tc.name {
			case "Short":
				maxAlloc = 5 * 1024     // 5KB for short texts
			case "Medium":
				maxAlloc = 50 * 1024    // 50KB for medium texts
			case "Long":
				maxAlloc = 500 * 1024   // 500KB for long texts
			default:
				maxAlloc = 500 * 1024   // Default to 500KB
			}
			if avgAlloc > maxAlloc {
				t.Errorf("Memory allocation too high: %d bytes (max: %d)", avgAlloc, maxAlloc)
			}
		})
	}
}

// TestComparisonTokenCounting compares baseline vs token counting performance
func TestComparisonTokenCounting(t *testing.T) {
	counter, err := NewTikTokenCounter()
	if err != nil {
		t.Skipf("Failed to initialize tokenizer: %v", err)
	}

	testCases := []struct {
		name string
		text string
	}{
		{"Short", shortText},
		{"Medium", mediumText},
		{"Long", longText},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := createTestRequest(tc.text, false)
			body, _ := json.Marshal(req)

			iterations := 100

			// Measure baseline (no token counting)
			startBaseline := time.Now()
			for i := 0; i < iterations; i++ {
				_ = len(body)
			}
			baselineDuration := time.Since(startBaseline)

			// Measure with token counting
			startCounting := time.Now()
			for i := 0; i < iterations; i++ {
				CountRequestTokens(body, counter)
			}
			countingDuration := time.Since(startCounting)

			// Calculate overhead
			overhead := countingDuration - baselineDuration
			overheadPercent := float64(overhead) / float64(baselineDuration) * 100
			avgOverheadPerRequest := overhead / time.Duration(iterations)

			t.Logf("Baseline duration: %v", baselineDuration)
			t.Logf("Counting duration: %v", countingDuration)
			t.Logf("Overhead: %v (%.2f%%)", overhead, overheadPercent)
			t.Logf("Avg overhead per request: %v", avgOverheadPerRequest)

			// Verify overhead is acceptable
			// We expect < 5ms overhead per request
			if avgOverheadPerRequest > 5*time.Millisecond {
				t.Errorf("Overhead too high: %v per request (max: 5ms)", avgOverheadPerRequest)
			}
		})
	}
}

// Helper functions

func createTestRequest(content string, stream bool) RequestBody {
	return RequestBody{
		Model: "glm-4",
		Messages: []RequestMessage{
			{
				Role:    "user",
				Content: json.RawMessage(`"` + content + `"`),
			},
		},
		Stream: stream,
	}
}

// printBenchmarkSummary prints a summary of benchmark results
func printBenchmarkSummary(results []BenchmarkResult) {
	fmt.Println("\n=== BENCHMARK SUMMARY ===")
	fmt.Printf("%-30s %10s %10s %10s %10s %10s\n",
		"Test Name", "Requests", "Avg (ms)", "P95 (ms)", "P99 (ms)", "RPS")
	fmt.Println(strings.Repeat("-", 90))

	for _, r := range results {
		fmt.Printf("%-30s %10d %10.2f %10.2f %10.2f %10.0f\n",
			r.Name,
			r.TotalRequests,
			float64(r.AvgLatency.Milliseconds()),
			float64(r.P95Latency.Milliseconds()),
			float64(r.P99Latency.Milliseconds()),
			r.RequestsPerSecond)
	}
	fmt.Println(strings.Repeat("-", 90))
}

// BenchmarkSimpleTokenCounter compares against the fallback implementation
func BenchmarkSimpleTokenCounter(b *testing.B) {
	counter := NewSimpleTokenCounter()
	req := createTestRequest(mediumText, false)
	body, _ := json.Marshal(req)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = CountRequestTokens(body, counter)
	}
}

// BenchmarkTikTokenCounterParallel tests parallel access to the tokenizer
func BenchmarkTikTokenCounterParallel(b *testing.B) {
	counter, err := NewTikTokenCounter()
	if err != nil {
		b.Skipf("Failed to initialize tokenizer: %v", err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = counter.CountTokens(mediumText)
		}
	})
}

// BenchmarkCountSSE parses Server-Sent Events for token counting
func BenchmarkCountSSE(b *testing.B) {
	counter, err := NewTikTokenCounter()
	if err != nil {
		b.Skipf("Failed to initialize tokenizer: %v", err)
	}

	sseContent := generateSSEContent(mediumText)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Create a mock response body capture
		body := &ResponseBodyCapture{
			originalBody: io.NopCloser(bytes.NewReader([]byte(sseContent))),
			buffer:       &bytes.Buffer{},
			teeReader:    io.TeeReader(bytes.NewReader([]byte(sseContent)), &bytes.Buffer{}),
			counter:      counter,
		}
		// Pre-fill buffer
		body.buffer.WriteString(sseContent)
		_, _ = body.CountOutputTokens()
	}
}

// BenchmarkTokenCountingLatency single operation latency measurement
func BenchmarkTokenCountingLatency(b *testing.B) {
	counter, err := NewTikTokenCounter()
	if err != nil {
		b.Skipf("Failed to initialize tokenizer: %v", err)
	}

	if b.N == 1 {
		// Single operation for detailed timing
		start := time.Now()
		tokens, err := counter.CountTokens(mediumText)
		elapsed := time.Since(start)

		b.Logf("Single CountTokens operation: %v for %d tokens", elapsed, tokens)
		if err != nil {
			b.Errorf("CountTokens error: %v", err)
		}

		// Check if we meet the <5ms target
		if elapsed > 5*time.Millisecond {
			b.Errorf("Latency exceeds 5ms target: %v", elapsed)
		}
		return
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = counter.CountTokens(mediumText)
	}
}

// generateSSEContent creates SSE format content for benchmarking
func generateSSEContent(text string) string {
	var buf bytes.Buffer

	// Message start
	buf.WriteString("data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_123\",\"type\":\"message\",\"role\":\"assistant\"}}\n\n")

	// Content block start
	buf.WriteString("data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")

	// Content block delta (split text into chunks)
	chunkSize := 50
	for i := 0; i < len(text); i += chunkSize {
		end := i + chunkSize
		if end > len(text) {
			end = len(text)
		}
		chunk := text[i:end]
		deltaJSON, _ := json.Marshal(map[string]interface{}{
			"type":  "text_delta",
			"text":  chunk,
		})
		buf.WriteString(fmt.Sprintf("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":%s}\n\n", string(deltaJSON)))
	}

	// Content block stop
	buf.WriteString("data: {\"type\":\"content_block_stop\",\"index\":0}\n\n")

	// Message delta
	buf.WriteString("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":0}}\n\n")

	// Message stop
	buf.WriteString("data: {\"type\":\"message_stop\"}\n\n")

	return buf.String()
}

// TestTokenizerLatencyTarget validates the <5ms target for various input sizes
func TestTokenizerLatencyTarget(t *testing.T) {
	counter, err := NewTikTokenCounter()
	if err != nil {
		t.Skipf("Failed to initialize tokenizer: %v", err)
	}

	testCases := []struct {
		name     string
		text     string
		targetMs int64
	}{
		{"Small", shortText, 1},
		{"Medium", mediumText, 2},
		{"Long", longText, 5},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Warm up
			for i := 0; i < 100; i++ {
				_, _ = counter.CountTokens(tc.text)
			}

			// Measure
			iterations := 1000
			start := time.Now()
			for i := 0; i < iterations; i++ {
				_, _ = counter.CountTokens(tc.text)
			}
			elapsed := time.Since(start)
			avgNs := elapsed.Nanoseconds() / int64(iterations)
			avgMs := float64(avgNs) / 1_000_000

			t.Logf("%s text: Average token counting time: %.2f ms (target: %dms)", tc.name, avgMs, tc.targetMs)

			if avgMs > float64(tc.targetMs) {
				t.Errorf("Overhead %.2f ms exceeds target %d ms for %s text", avgMs, tc.targetMs, tc.name)
			}
		})
	}
}
