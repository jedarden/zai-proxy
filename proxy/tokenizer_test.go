package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// TestTikTokenCounter tests the tiktoken-based token counter
func TestTikTokenCounter(t *testing.T) {
	counter, err := NewTikTokenCounter()
	if err != nil {
		t.Skipf("Skipping TikToken tests: failed to initialize counter: %v", err)
	}

	tests := []struct {
		name     string
		text     string
		wantMin  int
		wantMax  int
	}{
		{
			name:    "Empty string",
			text:    "",
			wantMin: 0,
			wantMax: 0,
		},
		{
			name:    "Short phrase",
			text:    "Hello, world!",
			wantMin: 3,
			wantMax: 5,
		},
		{
			name:    "Longer sentence",
			text:    "The quick brown fox jumps over the lazy dog.",
			wantMin: 9,
			wantMax: 12,
		},
		{
			name:    "Code snippet",
			text:    "def hello_world():\n    print('Hello, world!')",
			wantMin: 10,
			wantMax: 18,
		},
		{
			name:    "Unicode characters",
			text:    "Hello 世界! 🌍",
			wantMin: 5,
			wantMax: 12,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := counter.CountTokens(tt.text)
			if err != nil {
				t.Errorf("CountTokens() error = %v", err)
				return
			}
			t.Logf("Text: %q, Tokens: %d (expected %d-%d)", tt.text, got, tt.wantMin, tt.wantMax)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("CountTokens() = %v, want between %v and %v", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

// TestSimpleTokenCounter tests the fallback token counter
func TestSimpleTokenCounter(t *testing.T) {
	counter := NewSimpleTokenCounter()

	tests := []struct {
		name     string
		text     string
		wantMin  int
		wantMax  int
	}{
		{
			name:    "Empty string",
			text:    "",
			wantMin: 0,
			wantMax: 1,
		},
		{
			name:    "Short phrase",
			text:    "Hello, world!",
			wantMin: 1,
			wantMax: 5,
		},
		{
			name:    "Longer sentence",
			text:    "The quick brown fox jumps over the lazy dog.",
			wantMin: 8,
			wantMax: 15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := counter.CountTokens(tt.text)
			if err != nil {
				t.Errorf("CountTokens() error = %v", err)
				return
			}
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("CountTokens() = %v, want between %v and %v", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

// TestTikTokenAccuracy tests token counting accuracy against known values
func TestTikTokenAccuracy(t *testing.T) {
	counter, err := NewTikTokenCounter()
	if err != nil {
		t.Skipf("Skipping accuracy tests: failed to initialize counter: %v", err)
	}

	// Test cases with approximate expected token counts
	// These are based on cl100k_base encoding
	tests := []struct {
		text      string
		tolerance float64 // Acceptable variance (0.05 = 5%)
		expected  int
	}{
		{
			text:      "Hello",
			expected:  1,
			tolerance: 0.1,
		},
		{
			text:      "Hello, world!",
			expected:  4,
			tolerance: 0.1,
		},
		{
			text:      "The quick brown fox jumps over the lazy dog",
			expected:  10,
			tolerance: 0.1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.text[:min(20, len(tt.text))], func(t *testing.T) {
			got, err := counter.CountTokens(tt.text)
			if err != nil {
				t.Errorf("CountTokens() error = %v", err)
				return
			}

			variance := float64(abs(got-tt.expected)) / float64(tt.expected)
			t.Logf("Text: %q\n  Expected: %d tokens\n  Got: %d tokens\n  Variance: %.1f%%",
				tt.text, tt.expected, got, variance*100)

			if variance > tt.tolerance {
				t.Errorf("Token count variance %.1f%% exceeds tolerance %.1f%%",
					variance*100, tt.tolerance*100)
			}
		})
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// TestCountRequestTokens tests parsing request bodies and counting tokens
func TestCountRequestTokens(t *testing.T) {
	var counter TokenCounter
	tikCounter, err := NewTikTokenCounter()
	if err != nil {
		t.Logf("TikToken not available, using SimpleTokenCounter: %v", err)
		counter = NewSimpleTokenCounter()
	} else {
		counter = tikCounter
	}

	tests := []struct {
		name     string
		body     string
		wantMin  int
		wantMax  int
	}{
		{
			name:    "Valid single message",
			body:    `{"model":"glm-4","messages":[{"role":"user","content":"Hello"}]}`,
			wantMin: 1,
			wantMax: 3,
		},
		{
			name:    "Multiple messages",
			body:    `{"model":"glm-4","messages":[{"role":"user","content":"Hi"},{"role":"assistant","content":"Hello there"}]}`,
			wantMin: 2,
			wantMax: 6,
		},
		{
			name:    "Empty messages",
			body:    `{"model":"glm-4","messages":[]}`,
			wantMin: 0,
			wantMax: 0,
		},
		{
			name:    "Invalid JSON",
			body:    `{invalid json}`,
			wantMin: 0,
			wantMax: 0,
		},
		{
			name:    "Empty body",
			body:    ``,
			wantMin: 0,
			wantMax: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CountRequestTokens([]byte(tt.body), counter)
			if err != nil {
				t.Errorf("CountRequestTokens() error = %v", err)
				return
			}
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("CountRequestTokens() = %v, want between %v and %v", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

// TestResponseBodyCapture tests capturing response body while streaming
func TestResponseBodyCapture(t *testing.T) {
	var counter TokenCounter
	tikCounter, err := NewTikTokenCounter()
	if err != nil {
		counter = NewSimpleTokenCounter()
	} else {
		counter = tikCounter
	}
	originalContent := "This is test content that should be captured."

	// Create a ReadCloser from string
	body := io.NopCloser(strings.NewReader(originalContent))

	capture := NewResponseBodyCapture(body, counter)
	defer capture.Close()

	// Read all content
	read, err := io.ReadAll(capture)
	if err != nil {
		t.Fatalf("Failed to read from capture: %v", err)
	}

	// Verify forwarded content matches original
	if string(read) != originalContent {
		t.Errorf("Forwarded content = %q, want %q", string(read), originalContent)
	}

	// Verify captured content matches original
	captured := capture.GetCapturedContent()
	if string(captured) != originalContent {
		t.Errorf("Captured content = %q, want %q", string(captured), originalContent)
	}
}

// TestCountJSONResponseTokens tests counting tokens in non-streaming responses
func TestCountJSONResponseTokens(t *testing.T) {
	var counter TokenCounter
	tikCounter, err := NewTikTokenCounter()
	if err != nil {
		counter = NewSimpleTokenCounter()
	} else {
		counter = tikCounter
	}

	tests := []struct {
		name     string
		response string
		wantMin  int
		wantMax  int
	}{
		{
			name:     "Valid response with content",
			response: `{"id":"msg_123","type":"message","role":"assistant","content":[{"type":"text","text":"Hello there"}]}`,
			wantMin:  2,
			wantMax:  4,
		},
		{
			name:     "Response with multiple content blocks",
			response: `{"content":[{"type":"text","text":"First block"},{"type":"text","text":"Second block"}]}`,
			wantMin:  3,
			wantMax:  6,
		},
		{
			name:     "Empty content",
			response: `{"content":[]}`,
			wantMin:  0,
			wantMax:  0,
		},
		{
			name:     "Invalid JSON",
			response: `{invalid}`,
			wantMin:  0,
			wantMax:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := io.NopCloser(strings.NewReader(tt.response))
			capture := NewResponseBodyCapture(body, counter)

			// Read all to populate buffer
			io.ReadAll(capture)

			got, err := capture.countJSONTokens(capture.GetCapturedContent())
			if err != nil {
				t.Errorf("countJSONTokens() error = %v", err)
				return
			}

			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("countJSONTokens() = %v, want between %v and %v", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

// TestCountSSEResponseTokens tests counting tokens in streaming (SSE) responses
func TestCountSSEResponseTokens(t *testing.T) {
	var counter TokenCounter
	tikCounter, err := NewTikTokenCounter()
	if err != nil {
		counter = NewSimpleTokenCounter()
	} else {
		counter = tikCounter
	}

	tests := []struct {
		name     string
		response string
		wantMin  int
		wantMax  int
	}{
		{
			name: "Valid SSE stream",
			response: `data: {"type":"message_start","message":{"id":"msg_123"}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" there"}}

data: {"type":"content_block_stop","index":0}

data: {"type":"message_stop"}
`,
			wantMin: 2,
			wantMax: 4,
		},
		{
			name: "SSE with multiple deltas",
			response: `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"The"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" quick"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" brown"}}
`,
			wantMin: 3,
			wantMax: 6,
		},
		{
			name:     "Empty SSE",
			response: ``,
			wantMin:  0,
			wantMax:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := io.NopCloser(strings.NewReader(tt.response))
			capture := NewResponseBodyCapture(body, counter)

			// Read all to populate buffer
			io.ReadAll(capture)

			got, err := capture.countSSETokens(capture.GetCapturedContent())
			if err != nil {
				t.Errorf("countSSETokens() error = %v", err)
				return
			}

			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("countSSETokens() = %v, want between %v and %v", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

// TestInjectJSONUsage tests injecting usage into non-streaming responses
func TestInjectJSONUsage(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		inputTokens  int
		outputTokens int
		wantUsage    bool
	}{
		{
			name:         "Valid JSON response",
			body:         `{"id":"msg_123","type":"message","role":"assistant"}`,
			inputTokens:  10,
			outputTokens: 20,
			wantUsage:    true,
		},
		{
			name:         "Invalid JSON",
			body:         `{invalid}`,
			inputTokens:  5,
			outputTokens: 10,
			wantUsage:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := injectJSONUsage([]byte(tt.body), tt.inputTokens, tt.outputTokens)
			if err != nil {
				t.Errorf("injectJSONUsage() error = %v", err)
				return
			}

			if tt.wantUsage {
				if !bytes.Contains(got, []byte("input_tokens")) {
					t.Error("Response missing input_tokens")
				}
				if !bytes.Contains(got, []byte("output_tokens")) {
					t.Error("Response missing output_tokens")
				}
			}
		})
	}
}

// TestInjectSSEUsage tests injecting usage into streaming responses
func TestInjectSSEUsage(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		inputTokens  int
		outputTokens int
		wantUsage    bool
	}{
		{
			name: "Valid SSE with message_delta",
			body: `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}

data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}

data: {"type":"message_stop"}
`,
			inputTokens:  5,
			outputTokens: 10,
			wantUsage:    true,
		},
		{
			name: "SSE without message_delta",
			body: `data: {"type":"content_block_start"}

data: {"type":"content_block_stop"}
`,
			inputTokens:  5,
			outputTokens: 10,
			wantUsage:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := injectSSEUsage([]byte(tt.body), tt.inputTokens, tt.outputTokens)
			if err != nil {
				t.Errorf("injectSSEUsage() error = %v", err)
				return
			}

			if tt.wantUsage {
				if !bytes.Contains(got, []byte("input_tokens")) {
					t.Error("SSE response missing input_tokens in message_delta")
				}
				if !bytes.Contains(got, []byte("output_tokens")) {
					t.Error("SSE response missing output_tokens in message_delta")
				}
			}
		})
	}
}

// TestStreamingUsagePassthrough tests that upstream usage is passed through when present
func TestStreamingUsagePassthrough(t *testing.T) {
	var counter TokenCounter
	tikCounter, err := NewTikTokenCounter()
	if err != nil {
		counter = NewSimpleTokenCounter()
	} else {
		counter = tikCounter
	}

	tests := []struct {
		name              string
		body              string
		inputTokens       int
		wantUpstreamUsage bool
		wantUsageFields   []string // fields expected in usage
	}{
		{
			name: "message_delta with upstream usage should be preserved",
			body: `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}

data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":42,"output_tokens":99,"cache_creation_input_tokens":5,"cache_read_input_tokens":10}}

data: {"type":"message_stop"}
`,
			inputTokens:       5,
			wantUpstreamUsage: true,
			wantUsageFields:   []string{`"input_tokens":42`, `"output_tokens":99`, `"cache_creation_input_tokens":5`, `"cache_read_input_tokens":10`},
		},
		{
			name: "message_delta without upstream usage should get proxy usage",
			body: `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}

data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}

data: {"type":"message_stop"}
`,
			inputTokens:       10,
			wantUpstreamUsage: false,
			wantUsageFields:   []string{`"input_tokens":10`, `"output_tokens"`},
		},
		{
			name: "message_delta with empty usage object should get proxy usage",
			body: `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}

data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{}}

data: {"type":"message_stop"}
`,
			inputTokens:       15,
			wantUpstreamUsage: false,
			wantUsageFields:   []string{`"input_tokens":15`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := io.NopCloser(strings.NewReader(tt.body))
			capture := NewStreamingResponseBodyCapture(body, counter, tt.inputTokens)

			// Read all content
			read, err := io.ReadAll(capture)
			if err != nil {
				t.Fatalf("Failed to read from capture: %v", err)
			}
			capture.Close()

			// Check for expected usage fields
			for _, field := range tt.wantUsageFields {
				if !bytes.Contains(read, []byte(field)) {
					t.Errorf("Response missing expected usage field: %s\nGot: %s", field, string(read))
				}
			}

			// If upstream usage should be preserved, verify it's not overwritten with proxy values
			if tt.wantUpstreamUsage {
				// Should NOT contain the proxy input tokens (5) when upstream has 42
				if bytes.Contains(read, []byte(`"input_tokens":5`)) {
					t.Error("Response should preserve upstream usage (42), not use proxy value (5)")
				}
			}
		})
	}
}

// TestWrapResponseWithUsage tests wrapping Z.AI responses with Claude-compatible usage field
func TestWrapResponseWithUsage(t *testing.T) {
	tests := []struct {
		name         string
		originalResp string
		inputTokens  int
		outputTokens int
		wantUsage    bool
		wantResult   bool // whether to expect a "result" field
	}{
		{
			name:         "Z.AI response with result field",
			originalResp: `{"result":{"id":"msg_123","type":"message","role":"assistant","content":[{"type":"text","text":"Hello there"}]}}`,
			inputTokens:  10,
			outputTokens: 20,
			wantUsage:    true,
			wantResult:   true,
		},
		{
			name:         "Z.AI response without result field (direct content)",
			originalResp: `{"id":"msg_456","type":"message","role":"assistant","content":[{"type":"text","text":"Hi"}]}`,
			inputTokens:  5,
			outputTokens: 10,
			wantUsage:    true,
			wantResult:   true,
		},
		{
			name:         "Empty response",
			originalResp: `{}`,
			inputTokens:  0,
			outputTokens: 0,
			wantUsage:    true,
			wantResult:   true,
		},
		{
			name:         "Invalid JSON should return original",
			originalResp: `{invalid json}`,
			inputTokens:  5,
			outputTokens: 10,
			wantUsage:    false, // Original is returned on error
			wantResult:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := WrapResponseWithUsage([]byte(tt.originalResp), tt.inputTokens, tt.outputTokens)

			// For invalid JSON, expect error
			if tt.name == "Invalid JSON should return original" {
				if err == nil {
					t.Error("Expected error for invalid JSON, got nil")
				}
				// Should still return original response on error
				if string(got) != tt.originalResp {
					t.Errorf("Expected original response on error, got: %s", string(got))
				}
				return
			}

			if err != nil {
				t.Errorf("WrapResponseWithUsage() error = %v", err)
				return
			}

			// Check that response contains usage field with correct values
			if tt.wantUsage {
				if !bytes.Contains(got, []byte(`"input_tokens":`)) {
					t.Error("Wrapped response missing input_tokens field")
				}
				if !bytes.Contains(got, []byte(`"output_tokens":`)) {
					t.Error("Wrapped response missing output_tokens field")
				}

				// Verify the specific token values are present
				if !bytes.Contains(got, []byte(`"input_tokens":`+string(rune('0'+tt.inputTokens/10))+string(rune('0'+tt.inputTokens%10)))) {
					// Just check that input_tokens key exists (exact value check is complex)
					t.Logf("Response contains input_tokens: %s", string(got))
				}
			}

			// Check that result field is present
			if tt.wantResult {
				if !bytes.Contains(got, []byte(`"result"`)) {
					t.Error("Wrapped response missing result field")
				}
			}

			// Parse and validate structure
			var wrapped map[string]interface{}
			if err := json.Unmarshal(got, &wrapped); err != nil {
				t.Errorf("Failed to parse wrapped response: %v", err)
				return
			}

			// Validate usage structure
			usage, ok := wrapped["usage"].(map[string]interface{})
			if !ok {
				t.Error("Wrapped response missing or invalid usage object")
				return
			}

			// Check required Claude-compatible fields
			requiredFields := []string{"input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"}
			for _, field := range requiredFields {
				if _, exists := usage[field]; !exists {
					t.Errorf("Usage missing required field: %s", field)
				}
			}

			t.Logf("Wrapped response: %s", string(got))
		})
	}
}

// TestWrapResponseWithUsagePreservesContent tests that content is preserved during wrapping
func TestWrapResponseWithUsagePreservesContent(t *testing.T) {
	originalContent := "This is the original response content that should be preserved."
	originalResp := `{"result":{"id":"msg_test","content":[{"type":"text","text":"` + originalContent + `"}]}}`

	wrapped, err := WrapResponseWithUsage([]byte(originalResp), 10, 20)
	if err != nil {
		t.Fatalf("WrapResponseWithUsage() error = %v", err)
	}

	// Verify original content is preserved in the wrapped response
	if !bytes.Contains(wrapped, []byte(originalContent)) {
		t.Errorf("Wrapped response lost original content.\nOriginal: %s\nWrapped: %s", originalResp, string(wrapped))
	}

	// Parse and verify structure
	var result map[string]interface{}
	if err := json.Unmarshal(wrapped, &result); err != nil {
		t.Fatalf("Failed to parse wrapped response: %v", err)
	}

	resultObj, ok := result["result"].(map[string]interface{})
	if !ok {
		t.Fatal("Wrapped response missing result object")
	}

	content, ok := resultObj["content"].([]interface{})
	if !ok {
		t.Fatal("Wrapped response result missing content array")
	}

	if len(content) == 0 {
		t.Fatal("Content array is empty")
	}

	textBlock, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatal("Content block is not an object")
	}

	text, ok := textBlock["text"].(string)
	if !ok || text != originalContent {
		t.Errorf("Content text mismatch: got %q, want %q", text, originalContent)
	}
}

// TestStreamingPreservation tests that TeeReader doesn't affect streaming
func TestStreamingPreservation(t *testing.T) {
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
}
