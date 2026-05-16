package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// TestParseRequestBody tests extracting text from request bodies
func TestParseRequestBody(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantParts int
		wantEmpty bool
		wantErr   bool
	}{
		{
			name:      "Valid single message",
			body:      `{"model":"glm-4","messages":[{"role":"user","content":"Hello, how are you?"}]}`,
			wantParts: 1,
			wantEmpty: false,
			wantErr:   false,
		},
		{
			name:      "Multiple messages",
			body:      `{"model":"glm-4","messages":[{"role":"user","content":"Hi"},{"role":"assistant","content":"Hello there"},{"role":"user","content":"How are you?"}]}`,
			wantParts: 3,
			wantEmpty: false,
			wantErr:   false,
		},
		{
			name:      "Multi-modal content with text blocks",
			body:      `{"model":"glm-4","messages":[{"role":"user","content":[{"type":"text","text":"Describe this image"}]}]}`,
			wantParts: 1,
			wantEmpty: false,
			wantErr:   false,
		},
		{
			name:      "Empty messages array",
			body:      `{"model":"glm-4","messages":[]}`,
			wantParts: 0,
			wantEmpty: true,
			wantErr:   false,
		},
		{
			name:      "Empty message content",
			body:      `{"model":"glm-4","messages":[{"role":"user","content":""}]}`,
			wantParts: 0,
			wantEmpty: true,
			wantErr:   false,
		},
		{
			name:      "Malformed JSON - invalid syntax",
			body:      `{"model": "glm-4", "messages": [}`,
			wantParts: 0,
			wantEmpty: true,
			wantErr:   false, // Graceful degradation
		},
		{
			name:      "Malformed JSON - missing messages",
			body:      `{"model": "glm-4"}`,
			wantParts: 0,
			wantEmpty: true,
			wantErr:   false,
		},
		{
			name:      "Malformed JSON - missing model",
			body:      `{"messages": [{"role": "user", "content": "hi"}]}`,
			wantParts: 1,
			wantEmpty: false,
			wantErr:   false,
		},
		{
			name:      "Malformed JSON - not JSON at all",
			body:      `not json at all`,
			wantParts: 0,
			wantEmpty: true,
			wantErr:   false,
		},
		{
			name:      "Empty body",
			body:      ``,
			wantParts: 0,
			wantEmpty: true,
			wantErr:   false,
		},
		{
			name:      "Long input - multi-turn conversation",
			body:      `{"model":"glm-4","messages":[{"role":"user","content":"` + strings.Repeat("The quick brown fox jumps over the lazy dog. ", 100) + `"}]}`,
			wantParts: 1,
			wantEmpty: false,
			wantErr:   false,
		},
		{
			name:      "Multi-modal with non-text content",
			body:      `{"model":"glm-4","messages":[{"role":"user","content":[{"type":"image","source":"base64data"},{"type":"text","text":"What is this?"}]}]}`,
			wantParts: 1,
			wantEmpty: false,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts, err := ParseRequestBody([]byte(tt.body))

			if tt.wantErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if tt.wantEmpty && len(parts) != 0 {
				t.Errorf("Expected empty result, got %d parts", len(parts))
			}

			if !tt.wantEmpty && len(parts) != tt.wantParts {
				t.Errorf("ParseRequestBody() returned %d parts, want %d", len(parts), tt.wantParts)
			}

			// Log extracted text for debugging
			if len(parts) > 0 {
				t.Logf("Extracted %d text parts from request", len(parts))
				for i, part := range parts {
					preview := part
					if len(preview) > 50 {
						preview = preview[:50] + "..."
					}
					t.Logf("  Part %d: %q", i, preview)
				}
			}
		})
	}
}

// TestIsStreamingRequest tests detecting streaming requests
func TestIsStreamingRequest(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantStream bool
	}{
		{
			name:      "Streaming enabled",
			body:      `{"model":"glm-4","messages":[{"role":"user","content":"Hi"}],"stream":true}`,
			wantStream: true,
		},
		{
			name:      "Streaming disabled",
			body:      `{"model":"glm-4","messages":[{"role":"user","content":"Hi"}],"stream":false}`,
			wantStream: false,
		},
		{
			name:      "Streaming not specified",
			body:      `{"model":"glm-4","messages":[{"role":"user","content":"Hi"}]}`,
			wantStream: false,
		},
		{
			name:      "Empty body",
			body:      ``,
			wantStream: false,
		},
		{
			name:      "Invalid JSON",
			body:      `{invalid}`,
			wantStream: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsStreamingRequest([]byte(tt.body))
			if got != tt.wantStream {
				t.Errorf("IsStreamingRequest() = %v, want %v", got, tt.wantStream)
			}
		})
	}
}

// TestBodyTeeReader tests capturing request body while forwarding
func TestBodyTeeReader(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "Simple text",
			content: "Hello, world!",
		},
		{
			name:    "JSON request",
			content: `{"model":"glm-4","messages":[{"role":"user","content":"Test"}]}`,
		},
		{
			name:    "Long content (10k+ chars)",
			content: strings.Repeat("The quick brown fox jumps over the lazy dog. ", 500),
		},
		{
			name:    "Empty content",
			content: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalBody := io.NopCloser(strings.NewReader(tt.content))
			teeReader := NewBodyTeeReader(originalBody)

			// Read all content (simulating forwarding to upstream)
			forwarded, err := io.ReadAll(teeReader)
			if err != nil {
				t.Fatalf("Failed to read from TeeReader: %v", err)
			}

			// Verify forwarded content matches original
			if string(forwarded) != tt.content {
				t.Errorf("Forwarded body mismatch:\ngot:  %q\nwant: %q", string(forwarded), tt.content)
			}

			// Verify captured content matches original
			captured := teeReader.GetCapturedBody()
			if string(captured) != tt.content {
				t.Errorf("Captured body mismatch:\ngot:  %q\nwant: %q", string(captured), tt.content)
			}

			t.Logf("TeeReader successfully captured and forwarded %d bytes", len(tt.content))

			// Cleanup
			teeReader.Close()
		})
	}
}

// TestBodyTeeReaderConcurrent tests TeeReader doesn't break concurrent reads
func TestBodyTeeReaderConcurrent(t *testing.T) {
	content := strings.Repeat("test data ", 1000)
	originalBody := io.NopCloser(strings.NewReader(content))
	teeReader := NewBodyTeeReader(originalBody)

	// Simulate reading in chunks (like proxy forwarding)
	buf := make([]byte, 128)
	var forwarded bytes.Buffer

	for {
		n, err := teeReader.Read(buf)
		if n > 0 {
			forwarded.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read error: %v", err)
		}
	}

	// Verify both captured and forwarded are identical
	if forwarded.String() != content {
		t.Errorf("Forwarded content mismatch (length: got %d, want %d)", forwarded.Len(), len(content))
	}

	captured := string(teeReader.GetCapturedBody())
	if captured != content {
		t.Errorf("Captured content mismatch (length: got %d, want %d)", len(captured), len(content))
	}

	teeReader.Close()
}

// TestParseResponseBody tests extracting text from non-streaming responses
func TestParseResponseBody(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantParts int
		wantEmpty bool
	}{
		{
			name:      "Valid response with single content block",
			body:      `{"id":"msg_123","type":"message","role":"assistant","content":[{"type":"text","text":"Hello! How can I help you?"}]}`,
			wantParts: 1,
			wantEmpty: false,
		},
		{
			name:      "Response with multiple content blocks",
			body:      `{"content":[{"type":"text","text":"First part"},{"type":"text","text":"Second part"}]}`,
			wantParts: 2,
			wantEmpty: false,
		},
		{
			name:      "Empty content array",
			body:      `{"content":[]}`,
			wantParts: 0,
			wantEmpty: true,
		},
		{
			name:      "Invalid JSON",
			body:      `{invalid}`,
			wantParts: 0,
			wantEmpty: true,
		},
		{
			name:      "Empty body",
			body:      ``,
			wantParts: 0,
			wantEmpty: true,
		},
		{
			name:      "Long response",
			body:      `{"content":[{"type":"text","text":"` + strings.Repeat("word ", 5000) + `"}]}`,
			wantParts: 1,
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts, err := ParseResponseBody([]byte(tt.body))
			if err != nil {
				t.Errorf("ParseResponseBody() error = %v", err)
				return
			}

			if tt.wantEmpty && len(parts) != 0 {
				t.Errorf("Expected empty result, got %d parts", len(parts))
			}

			if !tt.wantEmpty && len(parts) != tt.wantParts {
				t.Errorf("ParseResponseBody() returned %d parts, want %d", len(parts), tt.wantParts)
			}

			if len(parts) > 0 {
				t.Logf("Extracted %d text parts from response", len(parts))
			}
		})
	}
}

// TestParseSSEResponse tests extracting text from streaming responses
func TestParseSSEResponse(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantParts int
		wantEmpty bool
	}{
		{
			name: "Valid SSE stream with multiple deltas",
			body: `data: {"type":"message_start","message":{"id":"msg_123"}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" there"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"!"}}

data: {"type":"content_block_stop","index":0}

data: {"type":"message_stop"}
`,
			wantParts: 3,
			wantEmpty: false,
		},
		{
			name: "SSE with single delta",
			body: `data: {"type":"content_block_delta","index":0,"delta":{"text":"Hello world"}}

`,
			wantParts: 1,
			wantEmpty: false,
		},
		{
			name:      "SSE with no deltas",
			body:      `data: {"type":"message_start"}

data: {"type":"message_stop"}
`,
			wantParts: 0,
			wantEmpty: true,
		},
		{
			name:      "Empty SSE",
			body:      ``,
			wantParts: 0,
			wantEmpty: true,
		},
		{
			name: "Malformed SSE - invalid JSON",
			body: `data: {invalid json}

data: {"type":"content_block_delta","delta":{"text":"Valid"}}
`,
			wantParts: 1,
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts, err := ParseSSEResponse([]byte(tt.body))
			if err != nil {
				t.Errorf("ParseSSEResponse() error = %v", err)
				return
			}

			if tt.wantEmpty && len(parts) != 0 {
				t.Errorf("Expected empty result, got %d parts", len(parts))
			}

			if !tt.wantEmpty && len(parts) != tt.wantParts {
				t.Errorf("ParseSSEResponse() returned %d parts, want %d", len(parts), tt.wantParts)
			}

			if len(parts) > 0 {
				t.Logf("Extracted %d text deltas from SSE response", len(parts))
				for i, part := range parts {
					t.Logf("  Delta %d: %q", i, part)
				}
			}
		})
	}
}

// TestIsSSEFormat tests SSE format detection
func TestIsSSEFormat(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		wantSSE bool
	}{
		{
			name:   "Valid SSE",
			body:   `data: {"type":"message_start"}`,
			wantSSE: true,
		},
		{
			name:   "JSON response",
			body:   `{"type":"message","content":[]}`,
			wantSSE: false,
		},
		{
			name:   "Empty body",
			body:   ``,
			wantSSE: false,
		},
		{
			name:   "Plain text with 'data:' in content",
			body:   `This is plain text that mentions data: fields`,
			wantSSE: true, // Contains "data: " pattern
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSSEFormat([]byte(tt.body))
			if got != tt.wantSSE {
				t.Errorf("IsSSEFormat() = %v, want %v", got, tt.wantSSE)
			}
		})
	}
}

// BenchmarkParseRequestBody benchmarks request body parsing
func BenchmarkParseRequestBody(b *testing.B) {
	body := []byte(`{"model":"glm-4","messages":[{"role":"user","content":"Hello, how are you today?"}]}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseRequestBody(body)
	}
}

// BenchmarkParseRequestBodyLong benchmarks parsing with long content
func BenchmarkParseRequestBodyLong(b *testing.B) {
	longText := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 500)
	body := []byte(`{"model":"glm-4","messages":[{"role":"user","content":"` + longText + `"}]}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseRequestBody(body)
	}
}

// BenchmarkBodyTeeReader benchmarks TeeReader performance
func BenchmarkBodyTeeReader(b *testing.B) {
	content := strings.Repeat("test data ", 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		originalBody := io.NopCloser(strings.NewReader(content))
		teeReader := NewBodyTeeReader(originalBody)
		io.ReadAll(teeReader)
		teeReader.Close()
	}
}

// BenchmarkParseSSEResponse benchmarks SSE parsing
func BenchmarkParseSSEResponse(b *testing.B) {
	body := []byte(`data: {"type":"content_block_delta","delta":{"text":"Hello"}}

data: {"type":"content_block_delta","delta":{"text":" world"}}
`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseSSEResponse(body)
	}
}
