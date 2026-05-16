package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// TestRegressionSuite contains golden test cases validated during development
// These tests ensure no regression in token counting accuracy or behavior
// Reference: BD-2E9_TEST_IMPLEMENTATION.md, TOKENIZATION.md

// GoldenTestCase represents a validated test case with known good output
type GoldenTestCase struct {
	name          string
	text          string
	expectedMin   int
	expectedMax   int
	description   string
}

// TestRegression_BasicTokenCounts validates fundamental token counting accuracy
// These are golden values confirmed during development
func TestRegression_BasicTokenCounts(t *testing.T) {
	counter, err := NewTikTokenCounter()
	if err != nil {
		t.Skipf("Skipping regression tests: TikToken not available: %v", err)
	}

	goldenCases := []GoldenTestCase{
		{
			name:        "Empty string",
			text:        "",
			expectedMin: 0,
			expectedMax: 0,
			description: "Empty input must return exactly 0 tokens",
		},
		{
			name:        "Simple greeting",
			text:        "Hello, world!",
			expectedMin: 3,
			expectedMax: 5,
			description: "Basic greeting - validated in BD-2E9",
		},
		{
			name:        "Question phrase",
			text:        "Hello, how are you?",
			expectedMin: 5,
			expectedMax: 8,
			description: "Question format - API test baseline",
		},
		{
			name:        "Standard sentence",
			text:        "The quick brown fox jumps over the lazy dog.",
			expectedMin: 9,
			expectedMax: 12,
			description: "Pangram sentence - accuracy baseline",
		},
		{
			name:        "Single word",
			text:        "Hello",
			expectedMin: 1,
			expectedMax: 1,
			description: "Single word tokenization",
		},
		{
			name:        "Code snippet",
			text:        "def hello_world():\n    print('Hello, world!')",
			expectedMin: 10,
			expectedMax: 18,
			description: "Python code with formatting - validated edge case",
		},
		{
			name:        "Unicode mixed",
			text:        "Hello 世界! 🌍",
			expectedMin: 5,
			expectedMax: 12,
			description: "Unicode characters - Chinese + emoji",
		},
		{
			name:        "Chinese sentence",
			text:        "你好，今天天气怎么样？",
			expectedMin: 5,
			expectedMax: 15,
			description: "Pure Chinese text - GLM-4 specific test",
		},
		{
			name:        "JSON content",
			text:        `{"name": "test", "value": 123}`,
			expectedMin: 8,
			expectedMax: 15,
			description: "JSON data in message content",
		},
		{
			name:        "Long paragraph",
			text:        strings.Repeat("The quick brown fox jumps over the lazy dog. ", 10),
			expectedMin: 90,
			expectedMax: 120,
			description: "Longer text ~100 tokens",
		},
	}

	for _, tc := range goldenCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := counter.CountTokens(tc.text)
			if err != nil {
				t.Errorf("CountTokens() error = %v", err)
				return
			}

			if got < tc.expectedMin || got > tc.expectedMax {
				t.Errorf("%s\nGot %d tokens, expected %d-%d\nText: %q",
					tc.description, got, tc.expectedMin, tc.expectedMax,
					truncateString(tc.text, 50))
			} else {
				t.Logf("✅ %s: %d tokens (expected %d-%d)",
					tc.name, got, tc.expectedMin, tc.expectedMax)
			}
		})
	}
}

// TestRegression_EdgeCases validates all edge cases that previously failed or were problematic
func TestRegression_EdgeCases(t *testing.T) {
	counter, err := NewTikTokenCounter()
	if err != nil {
		t.Skipf("Skipping regression tests: TikToken not available: %v", err)
	}

	edgeCases := []struct {
		name        string
		text        string
		shouldError bool
		description string
	}{
		{
			name:        "Whitespace only",
			text:        "   \n\t  ",
			shouldError: false,
			description: "Only whitespace characters - must not crash",
		},
		{
			name:        "Special characters",
			text:        "!@#$%^&*()_+-=[]{}|;':\",./<>?",
			shouldError: false,
			description: "All special characters - validated handling",
		},
		{
			name:        "Very long string",
			text:        strings.Repeat("a", 50000),
			shouldError: false,
			description: "50k character string - performance test baseline",
		},
		{
			name:        "Newlines only",
			text:        "\n\n\n\n\n",
			shouldError: false,
			description: "Multiple newlines - edge case",
		},
		{
			name:        "Mixed formatting",
			text:        "Hello\tworld\n\nNew paragraph here.",
			shouldError: false,
			description: "Tabs and newlines mixed with text",
		},
		{
			name:        "Emoji sequence",
			text:        "👍👎🔥💯🎉",
			shouldError: false,
			description: "Multiple emoji characters",
		},
		{
			name:        "Mixed language",
			text:        "Hello 世界 مرحبا κόσμος",
			shouldError: false,
			description: "Multiple scripts in one string",
		},
	}

	for _, tc := range edgeCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := counter.CountTokens(tc.text)

			if tc.shouldError && err == nil {
				t.Errorf("Expected error but got none")
			}

			if !tc.shouldError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if err == nil {
				t.Logf("✅ %s: %d tokens (no crash, no error)", tc.name, got)
			}
		})
	}
}

// TestRegression_RequestParsing validates request body parsing edge cases
func TestRegression_RequestParsing(t *testing.T) {
	var counter TokenCounter
	tikCounter, err := NewTikTokenCounter()
	if err != nil {
		counter = NewSimpleTokenCounter()
	} else {
		counter = tikCounter
	}

	testCases := []struct {
		name        string
		body        string
		expectError bool
		expectedMin int
		expectedMax int
		description string
	}{
		{
			name:        "Valid single message",
			body:        `{"model":"glm-4","messages":[{"role":"user","content":"Hello"}]}`,
			expectError: false,
			expectedMin: 1,
			expectedMax: 3,
			description: "Baseline API request format",
		},
		{
			name:        "Multiple messages",
			body:        `{"model":"glm-4","messages":[{"role":"user","content":"Hi"},{"role":"assistant","content":"Hello there"}]}`,
			expectError: false,
			expectedMin: 2,
			expectedMax: 6,
			description: "Multi-turn conversation",
		},
		{
			name:        "Empty messages array",
			body:        `{"model":"glm-4","messages":[]}`,
			expectError: false,
			expectedMin: 0,
			expectedMax: 0,
			description: "Empty messages - must handle gracefully",
		},
		{
			name:        "Missing messages field",
			body:        `{"model":"glm-4"}`,
			expectError: false,
			expectedMin: 0,
			expectedMax: 0,
			description: "Missing required field - graceful degradation",
		},
		{
			name:        "Malformed JSON",
			body:        `{invalid json}`,
			expectError: false, // Graceful degradation, returns 0
			expectedMin: 0,
			expectedMax: 0,
			description: "Invalid JSON - must not crash",
		},
		{
			name:        "Empty body",
			body:        ``,
			expectError: false,
			expectedMin: 0,
			expectedMax: 0,
			description: "Empty request body",
		},
		{
			name:        "Incomplete JSON",
			body:        `{"model":"glm-4","messages":[{"role":"user"`,
			expectError: false,
			expectedMin: 0,
			expectedMax: 0,
			description: "Truncated JSON - must not crash",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CountRequestTokens([]byte(tc.body), counter)

			if tc.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}

			if !tc.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if got < tc.expectedMin || got > tc.expectedMax {
				t.Errorf("%s\nGot %d tokens, expected %d-%d",
					tc.description, got, tc.expectedMin, tc.expectedMax)
			} else {
				t.Logf("✅ %s: %d tokens", tc.name, got)
			}
		})
	}
}

// TestRegression_StreamingResponses validates SSE response token counting
func TestRegression_StreamingResponses(t *testing.T) {
	var counter TokenCounter
	tikCounter, err := NewTikTokenCounter()
	if err != nil {
		counter = NewSimpleTokenCounter()
	} else {
		counter = tikCounter
	}

	streamingCases := []struct {
		name        string
		response    string
		expectedMin int
		expectedMax int
		description string
	}{
		{
			name: "Simple SSE stream",
			response: `data: {"type":"message_start","message":{"id":"msg_123"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}

data: {"type":"message_stop"}
`,
			expectedMin: 2,
			expectedMax: 4,
			description: "Basic SSE stream - Hello world",
		},
		{
			name: "Multi-sentence stream",
			response: `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"The"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" quick"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" brown"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" fox"}}
`,
			expectedMin: 3,
			expectedMax: 8,
			description: "Multiple deltas forming sentence",
		},
		{
			name: "Empty stream",
			response: `data: {"type":"message_start"}

data: {"type":"message_stop"}
`,
			expectedMin: 0,
			expectedMax: 0,
			description: "Stream with no content deltas",
		},
		{
			name: "Unicode in stream",
			response: `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"你好"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"世界"}}
`,
			expectedMin: 2,
			expectedMax: 8,
			description: "Chinese characters in streaming response",
		},
	}

	for _, tc := range streamingCases {
		t.Run(tc.name, func(t *testing.T) {
			body := io.NopCloser(strings.NewReader(tc.response))
			capture := NewResponseBodyCapture(body, counter)

			// Read all to populate buffer
			io.ReadAll(capture)

			got, err := capture.countSSETokens(capture.GetCapturedContent())
			if err != nil {
				t.Errorf("countSSETokens() error = %v", err)
				return
			}

			if got < tc.expectedMin || got > tc.expectedMax {
				t.Errorf("%s\nGot %d tokens, expected %d-%d",
					tc.description, got, tc.expectedMin, tc.expectedMax)
			} else {
				t.Logf("✅ %s: %d tokens", tc.name, got)
			}
		})
	}
}

// TestRegression_JSONResponses validates non-streaming response token counting
func TestRegression_JSONResponses(t *testing.T) {
	var counter TokenCounter
	tikCounter, err := NewTikTokenCounter()
	if err != nil {
		counter = NewSimpleTokenCounter()
	} else {
		counter = tikCounter
	}

	jsonCases := []struct {
		name        string
		response    string
		expectedMin int
		expectedMax int
		description string
	}{
		{
			name:        "Simple response",
			response:    `{"id":"msg_123","type":"message","role":"assistant","content":[{"type":"text","text":"Hello there"}]}`,
			expectedMin: 2,
			expectedMax: 4,
			description: "Basic JSON response with single content block",
		},
		{
			name:        "Multiple content blocks",
			response:    `{"content":[{"type":"text","text":"First block"},{"type":"text","text":"Second block"}]}`,
			expectedMin: 3,
			expectedMax: 6,
			description: "Response with multiple text blocks",
		},
		{
			name:        "Empty content",
			response:    `{"content":[]}`,
			expectedMin: 0,
			expectedMax: 0,
			description: "Response with no content blocks",
		},
		{
			name:        "Long response",
			response:    `{"content":[{"type":"text","text":"` + strings.Repeat("word ", 50) + `"}]}`,
			expectedMin: 40,
			expectedMax: 80,
			description: "Long response content",
		},
	}

	for _, tc := range jsonCases {
		t.Run(tc.name, func(t *testing.T) {
			body := io.NopCloser(strings.NewReader(tc.response))
			capture := NewResponseBodyCapture(body, counter)

			// Read all to populate buffer
			io.ReadAll(capture)

			got, err := capture.countJSONTokens(capture.GetCapturedContent())
			if err != nil {
				t.Errorf("countJSONTokens() error = %v", err)
				return
			}

			if got < tc.expectedMin || got > tc.expectedMax {
				t.Errorf("%s\nGot %d tokens, expected %d-%d",
					tc.description, got, tc.expectedMin, tc.expectedMax)
			} else {
				t.Logf("✅ %s: %d tokens", tc.name, got)
			}
		})
	}
}

// TestRegression_UsageInjection validates token usage injection into responses
func TestRegression_UsageInjection(t *testing.T) {
	testCases := []struct {
		name         string
		body         string
		inputTokens  int
		outputTokens int
		isSSE        bool
		description  string
	}{
		{
			name:         "JSON response injection",
			body:         `{"id":"msg_123","type":"message","role":"assistant"}`,
			inputTokens:  10,
			outputTokens: 20,
			isSSE:        false,
			description:  "Inject usage into JSON response",
		},
		{
			name: "SSE response injection",
			body: `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}

data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}

data: {"type":"message_stop"}
`,
			inputTokens:  5,
			outputTokens: 10,
			isSSE:        true,
			description:  "Inject usage into SSE message_delta event",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var got []byte
			var err error

			if tc.isSSE {
				got, err = injectSSEUsage([]byte(tc.body), tc.inputTokens, tc.outputTokens)
			} else {
				got, err = injectJSONUsage([]byte(tc.body), tc.inputTokens, tc.outputTokens)
			}

			if err != nil {
				t.Errorf("Injection failed: %v", err)
				return
			}

			if !bytes.Contains(got, []byte("input_tokens")) {
				t.Errorf("Response missing input_tokens field")
			}
			if !bytes.Contains(got, []byte("output_tokens")) {
				t.Errorf("Response missing output_tokens field")
			}

			// Validate injected values
			if tc.isSSE {
				// For SSE, check in message_delta event
				if !bytes.Contains(got, []byte(`"input_tokens":`)) {
					t.Errorf("SSE response missing input_tokens in message_delta")
				}
			} else {
				// For JSON, verify it's valid JSON
				var parsed map[string]interface{}
				if err := json.Unmarshal(got, &parsed); err != nil {
					t.Errorf("Injected response is not valid JSON: %v", err)
				} else {
					usage, ok := parsed["usage"].(map[string]interface{})
					if !ok {
						t.Errorf("Usage field not found or wrong type")
					} else {
						inputVal := int(usage["input_tokens"].(float64))
						outputVal := int(usage["output_tokens"].(float64))

						if inputVal != tc.inputTokens {
							t.Errorf("input_tokens = %d, want %d", inputVal, tc.inputTokens)
						}
						if outputVal != tc.outputTokens {
							t.Errorf("output_tokens = %d, want %d", outputVal, tc.outputTokens)
						}
					}
				}
			}

			t.Logf("✅ %s: Usage injected successfully", tc.name)
		})
	}
}

// TestRegression_ConcurrentAccess validates thread-safety of token counter
func TestRegression_ConcurrentAccess(t *testing.T) {
	counter, err := NewTikTokenCounter()
	if err != nil {
		t.Skipf("Skipping concurrency test: TikToken not available: %v", err)
	}

	const numGoroutines = 20
	const numOperations = 100

	testTexts := []string{
		"Hello, world!",
		"The quick brown fox jumps over the lazy dog.",
		"你好，世界！",
		"Testing concurrent token counting.",
		strings.Repeat("word ", 100),
	}

	// Run concurrent token counting
	done := make(chan bool)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numOperations; j++ {
				text := testTexts[j%len(testTexts)]
				_, err := counter.CountTokens(text)
				if err != nil {
					t.Errorf("Goroutine %d operation %d failed: %v", id, j, err)
				}
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	t.Logf("✅ Concurrent access test passed: %d goroutines × %d operations = %d total operations",
		numGoroutines, numOperations, numGoroutines*numOperations)
}

// TestRegression_FallbackCounter validates SimpleTokenCounter fallback behavior
func TestRegression_FallbackCounter(t *testing.T) {
	counter := NewSimpleTokenCounter()

	testCases := []struct {
		name string
		text string
		description string
	}{
		{
			name: "Empty string",
			text: "",
			description: "Fallback must handle empty string",
		},
		{
			name: "Short phrase",
			text: "Hello, world!",
			description: "Fallback basic test",
		},
		{
			name: "Longer sentence",
			text: "The quick brown fox jumps over the lazy dog.",
			description: "Fallback sentence test",
		},
		{
			name: "Very long text",
			text: strings.Repeat("word ", 1000),
			description: "Fallback performance test",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := counter.CountTokens(tc.text)
			if err != nil {
				t.Errorf("Fallback counter error: %v", err)
			}

			// Fallback should never crash, but counts are approximate
			if got < 0 {
				t.Errorf("Fallback returned negative token count: %d", got)
			}

			t.Logf("✅ %s: %d tokens (approximate, fallback mode)", tc.name, got)
		})
	}
}

// Helper function to truncate strings for error messages
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// TestRegression_StreamingPreservation validates that streaming is not affected by token counting
func TestRegression_StreamingPreservation(t *testing.T) {
	var counter TokenCounter
	tikCounter, err := NewTikTokenCounter()
	if err != nil {
		counter = NewSimpleTokenCounter()
	} else {
		counter = tikCounter
	}

	// Simulate streaming data in chunks
	chunks := []string{
		"data: {\"type\":\"message_start\"}\n",
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"Hello\"}}\n",
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\" world\"}}\n",
		"data: {\"type\":\"message_stop\"}\n",
	}

	// Create a reader that emits chunks
	pipeReader, pipeWriter := io.Pipe()

	go func() {
		defer pipeWriter.Close()
		for _, chunk := range chunks {
			pipeWriter.Write([]byte(chunk))
		}
	}()

	capture := NewResponseBodyCapture(io.NopCloser(pipeReader), counter)
	defer capture.Close()

	// Read in chunks to simulate streaming
	buf := make([]byte, 64)
	var received []string

	for {
		n, err := capture.Read(buf)
		if n > 0 {
			received = append(received, string(buf[:n]))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read error: %v", err)
		}
	}

	// Verify all chunks were received
	fullReceived := strings.Join(received, "")
	expected := strings.Join(chunks, "")

	if fullReceived != expected {
		t.Errorf("Streaming content mismatch:\ngot:  %q\nwant: %q", fullReceived, expected)
	}

	// Verify captured content is complete
	captured := string(capture.GetCapturedContent())
	if captured != expected {
		t.Errorf("Captured content incomplete:\ngot:  %q\nwant: %q", captured, expected)
	}

	t.Logf("✅ Streaming preservation validated: %d bytes streamed without corruption", len(expected))
}
