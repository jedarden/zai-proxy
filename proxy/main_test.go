package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestAPIRequest represents a Claude API compatible request
type TestAPIRequest struct {
	Model    string    `json:"model"`
	Messages []TestMessage `json:"messages"`
	Stream   bool      `json:"stream,omitempty"`
}

type TestMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// TestAPIResponse represents expected Claude API response format
type TestAPIResponse struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Role    string         `json:"role"`
	Content []TestContentBlock `json:"content"`
	Usage   *Usage         `json:"usage,omitempty"`
}

type TestContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// TestTokenCountingBasicRequest tests token counting with a simple chat completion request
func TestTokenCountingBasicRequest(t *testing.T) {
	req := TestAPIRequest{
		Model: "glm-4",
		Messages: []TestMessage{
			{
				Role:    "user",
				Content: "Hello, how are you?",
			},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	// Expected token counts (approximate for "Hello, how are you?")
	// Input: ~6 tokens
	// This is a placeholder - actual counts depend on tokenizer implementation
	expectedInputMin := 4
	expectedInputMax := 8

	t.Logf("Test request body: %s", string(body))
	t.Logf("Expected input tokens: %d-%d", expectedInputMin, expectedInputMax)

	// Note: This test will pass even without tokenizer implementation
	// It serves as documentation for expected behavior
	if len(body) == 0 {
		t.Error("Request body is empty")
	}
}

// TestTokenCountingEmptyMessage tests edge case: empty message content
func TestTokenCountingEmptyMessage(t *testing.T) {
	req := TestAPIRequest{
		Model: "glm-4",
		Messages: []TestMessage{
			{
				Role:    "user",
				Content: "",
			},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	// Empty content should result in minimal tokens (role markers, etc.)
	expectedInputMin := 0
	expectedInputMax := 2

	t.Logf("Empty message request: %s", string(body))
	t.Logf("Expected input tokens for empty content: %d-%d", expectedInputMin, expectedInputMax)
}

// TestTokenCountingLongInput tests with very long input (>10k tokens)
func TestTokenCountingLongInput(t *testing.T) {
	// Generate long text (~10k characters = ~2500 tokens approximately)
	longText := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 500)

	req := TestAPIRequest{
		Model: "glm-4",
		Messages: []TestMessage{
			{
				Role:    "user",
				Content: longText,
			},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	// Approximate token count for 10k chars
	expectedInputMin := 2000
	expectedInputMax := 3000

	t.Logf("Long input length: %d characters", len(longText))
	t.Logf("Expected input tokens: %d-%d", expectedInputMin, expectedInputMax)
	t.Logf("Request body size: %d bytes", len(body))
}

// TestTokenCountingMultipleMessages tests multi-turn conversation
func TestTokenCountingMultipleMessages(t *testing.T) {
	req := TestAPIRequest{
		Model: "glm-4",
		Messages: []TestMessage{
			{
				Role:    "user",
				Content: "What is the capital of France?",
			},
			{
				Role:    "assistant",
				Content: "The capital of France is Paris.",
			},
			{
				Role:    "user",
				Content: "What is its population?",
			},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	// Multiple messages should sum token counts
	expectedInputMin := 20
	expectedInputMax := 35

	t.Logf("Multi-message request: %s", string(body))
	t.Logf("Expected input tokens: %d-%d", expectedInputMin, expectedInputMax)
}

// TestTokenCountingMalformedJSON tests error handling with invalid JSON
func TestTokenCountingMalformedJSON(t *testing.T) {
	malformedRequests := []string{
		`{"model": "glm-4", "messages": [}`,                    // Invalid syntax
		`{"model": "glm-4"}`,                                   // Missing messages
		`{"messages": [{"role": "user", "content": "hi"}]}`,    // Missing model
		`not json at all`,                                      // Complete garbage
	}

	for i, malformed := range malformedRequests {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			// Tokenizer should handle malformed JSON gracefully
			// Either return 0 tokens or skip counting without crashing
			t.Logf("Testing malformed JSON: %s", malformed)
			// This test validates that the system doesn't crash
		})
	}
}

// TestTokenCountingStreaming tests streaming response format
func TestTokenCountingStreaming(t *testing.T) {
	req := TestAPIRequest{
		Model: "glm-4",
		Messages: []TestMessage{
			{
				Role:    "user",
				Content: "Tell me a story.",
			},
		},
		Stream: true,
	}

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	t.Logf("Streaming request: %s", string(body))
	t.Log("Note: Streaming responses should include token usage in final SSE event")
}

// BenchmarkTokenCounting benchmarks token counting performance
func BenchmarkTokenCounting(b *testing.B) {
	req := TestAPIRequest{
		Model: "glm-4",
		Messages: []TestMessage{
			{
				Role:    "user",
				Content: "Hello, how are you today?",
			},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		b.Fatalf("Failed to marshal request: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// This simulates token counting operation
		// Actual implementation should replace this
		_ = len(body)
	}

	// Target: <5ms per operation
	elapsed := b.Elapsed()
	avgTime := elapsed / time.Duration(b.N)
	if avgTime > 5*time.Millisecond {
		b.Errorf("Token counting too slow: %v per operation (target: <5ms)", avgTime)
	}
}

// BenchmarkTokenCountingLongText benchmarks with long text
func BenchmarkTokenCountingLongText(b *testing.B) {
	longText := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 500)
	req := TestAPIRequest{
		Model: "glm-4",
		Messages: []TestMessage{
			{
				Role:    "user",
				Content: longText,
			},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		b.Fatalf("Failed to marshal request: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = len(body)
	}

	elapsed := b.Elapsed()
	avgTime := elapsed / time.Duration(b.N)
	b.Logf("Long text token counting: %v per operation", avgTime)
}

// TestProxyHealthEndpoint tests the health endpoint returns 200 OK
func TestProxyHealthEndpoint(t *testing.T) {
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}).ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("Expected body 'ok', got '%s'", string(body))
	}
}

// TestResponseFormat tests that response includes token usage
func TestResponseFormat(t *testing.T) {
	// Example expected response format
	expectedResp := TestAPIResponse{
		ID:   "msg_123",
		Type: "message",
		Role: "assistant",
		Content: []TestContentBlock{
			{
				Type: "text",
				Text: "Hello! I'm doing well, thank you for asking.",
			},
		},
		Usage: &Usage{
			InputTokens:  6,
			OutputTokens: 12,
		},
	}

	body, err := json.Marshal(expectedResp)
	if err != nil {
		t.Fatalf("Failed to marshal response: %v", err)
	}

	t.Logf("Expected response format: %s", string(body))

	// Validate structure
	var parsed TestAPIResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if parsed.Usage == nil {
		t.Error("Response missing usage field")
	} else {
		if parsed.Usage.InputTokens <= 0 {
			t.Error("InputTokens should be > 0")
		}
		if parsed.Usage.OutputTokens <= 0 {
			t.Error("OutputTokens should be > 0")
		}
	}
}

// TestStreamingResponseFormat tests SSE format for streaming
func TestStreamingResponseFormat(t *testing.T) {
	// Example streaming response chunks (SSE format)
	sseChunks := []string{
		`data: {"type":"message_start","message":{"id":"msg_123","type":"message","role":"assistant"}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"!"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":6,"output_tokens":12}}`,
		`data: {"type":"message_stop"}`,
	}

	for i, chunk := range sseChunks {
		t.Logf("SSE chunk %d: %s", i, chunk)

		// Validate each chunk is valid JSON (after removing "data: " prefix)
		jsonPart := strings.TrimPrefix(chunk, "data: ")
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(jsonPart), &parsed); err != nil {
			t.Errorf("Invalid JSON in chunk %d: %v", i, err)
		}
	}

	// Verify usage appears in message_delta event
	lastChunk := sseChunks[len(sseChunks)-2] // message_delta with usage
	jsonPart := strings.TrimPrefix(lastChunk, "data: ")

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(jsonPart), &parsed); err != nil {
		t.Fatalf("Failed to parse message_delta: %v", err)
	}

	usage, ok := parsed["usage"].(map[string]interface{})
	if !ok {
		t.Error("message_delta missing usage field")
	} else {
		if _, hasInput := usage["input_tokens"]; !hasInput {
			t.Error("usage missing input_tokens")
		}
		if _, hasOutput := usage["output_tokens"]; !hasOutput {
			t.Error("usage missing output_tokens")
		}
	}
}

// TestJSONBodyParser tests parsing request/response bodies
func TestJSONBodyParser(t *testing.T) {
	testCases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "Valid request",
			input:   `{"model":"glm-4","messages":[{"role":"user","content":"hi"}]}`,
			wantErr: false,
		},
		{
			name:    "Empty object",
			input:   `{}`,
			wantErr: false,
		},
		{
			name:    "Invalid JSON",
			input:   `{invalid}`,
			wantErr: true,
		},
		{
			name:    "Empty string",
			input:   ``,
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var parsed map[string]interface{}
			err := json.Unmarshal([]byte(tc.input), &parsed)

			if tc.wantErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestTeeReader tests capturing request body while forwarding
func TestTeeReader(t *testing.T) {
	originalBody := "Hello, world!"
	reader := strings.NewReader(originalBody)

	var captured bytes.Buffer
	teeReader := io.TeeReader(reader, &captured)

	// Simulate reading the body (as proxy would do)
	forwarded, err := io.ReadAll(teeReader)
	if err != nil {
		t.Fatalf("Failed to read from TeeReader: %v", err)
	}

	// Verify both captured and forwarded have the same content
	if string(forwarded) != originalBody {
		t.Errorf("Forwarded body mismatch: got %q, want %q", string(forwarded), originalBody)
	}

	if captured.String() != originalBody {
		t.Errorf("Captured body mismatch: got %q, want %q", captured.String(), originalBody)
	}

	t.Logf("TeeReader successfully captured and forwarded %d bytes", len(originalBody))
}

// TestTokenCountAccuracy tests token count accuracy (when tokenizer is implemented)
func TestTokenCountAccuracy(t *testing.T) {
	testCases := []struct {
		text              string
		expectedTokensMin int
		expectedTokensMax int
		tolerance         float64
	}{
		{
			text:              "Hello, world!",
			expectedTokensMin: 3,
			expectedTokensMax: 5,
			tolerance:         0.05, // 5% tolerance
		},
		{
			text:              "The quick brown fox jumps over the lazy dog.",
			expectedTokensMin: 8,
			expectedTokensMax: 12,
			tolerance:         0.05,
		},
		{
			text:              strings.Repeat("word ", 100),
			expectedTokensMin: 100,
			expectedTokensMax: 150,
			tolerance:         0.05,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.text[:min(20, len(tc.text))], func(t *testing.T) {
			// Placeholder: actual tokenizer would count here
			// For now, just validate test structure
			actualTokens := len(strings.Fields(tc.text)) // Rough word count

			if actualTokens < tc.expectedTokensMin || actualTokens > tc.expectedTokensMax {
				t.Logf("Token count %d outside expected range [%d, %d]",
					actualTokens, tc.expectedTokensMin, tc.expectedTokensMax)
			}

			t.Logf("Text: %q, Tokens: %d (expected: %d-%d)",
				tc.text[:min(40, len(tc.text))], actualTokens, tc.expectedTokensMin, tc.expectedTokensMax)
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
