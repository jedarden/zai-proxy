package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"strings"
	"sync"

	"github.com/tiktoken-go/tokenizer"
)

// TokenCounter interface for counting tokens in text
type TokenCounter interface {
	CountTokens(text string) (int, error)
}

// TikTokenCounter uses tiktoken-go with cl100k_base encoding (Claude 3 compatible)
type TikTokenCounter struct {
	encoder tokenizer.Codec
	mu      sync.Mutex // Protect encoder access
}

// NewTikTokenCounter creates a new tiktoken-based token counter with cl100k_base encoding
func NewTikTokenCounter() (*TikTokenCounter, error) {
	enc, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		return nil, err
	}

	return &TikTokenCounter{
		encoder: enc,
	}, nil
}

// CountTokens counts tokens in text using tiktoken cl100k_base encoding
func (tc *TikTokenCounter) CountTokens(text string) (int, error) {
	if text == "" {
		return 0, nil
	}

	tc.mu.Lock()
	defer tc.mu.Unlock()

	// Encode text to token IDs
	ids, _, err := tc.encoder.Encode(text)
	if err != nil {
		return 0, err
	}

	return len(ids), nil
}

// SimpleTokenCounter is a fallback tokenizer that uses word count approximation
// Used only if TikToken initialization fails
type SimpleTokenCounter struct{}

func NewSimpleTokenCounter() *SimpleTokenCounter {
	return &SimpleTokenCounter{}
}

// CountTokens approximates token count using word count * 1.3
// This is a rough approximation for fallback scenarios
func (tc *SimpleTokenCounter) CountTokens(text string) (int, error) {
	if text == "" {
		return 0, nil
	}

	// Rough approximation: ~1.3 tokens per word on average
	words := len(text) / 4 // Average word length ~4 chars
	if words == 0 {
		words = 1
	}

	return words, nil
}

// UsageData holds token usage counts from an API response.
type UsageData struct {
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	FromAPI          bool // true = upstream API counts, false = tiktoken estimate
}

// ExtractUsageFromJSON reads the usage block from a non-streaming Anthropic-format response.
// Returns (usage, true) when a usage block with non-zero token counts is present.
func ExtractUsageFromJSON(body []byte) (UsageData, bool) {
	var resp struct {
		Usage *struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.Usage == nil {
		return UsageData{}, false
	}
	u := resp.Usage
	if u.InputTokens == 0 && u.OutputTokens == 0 {
		return UsageData{}, false
	}
	return UsageData{
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		CacheReadTokens:  u.CacheReadInputTokens,
		CacheWriteTokens: u.CacheCreationInputTokens,
		FromAPI:          true,
	}, true
}

// jsonFloat safely converts a JSON-unmarshalled interface{} value to float64.
func jsonFloat(v interface{}) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

// RequestBody represents Claude API request structure
type RequestBody struct {
	Model    string           `json:"model"`
	Messages []RequestMessage `json:"messages"`
	Stream   bool             `json:"stream,omitempty"`
}

// ContentBlock represents a single content block in multi-modal messages
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// RequestMessage represents a message in Claude API request format
// Content can be either a string (simple text) or an array of ContentBlock (multi-modal)
type RequestMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // Can be string or array
}

// CountRequestTokens extracts messages from request body and counts tokens
// Supports both simple string content and multi-modal array content
func CountRequestTokens(body []byte, counter TokenCounter) (int, error) {
	if len(body) == 0 {
		return 0, nil
	}

	var req RequestBody
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("Warning: failed to parse request body for token counting: %v", err)
		return 0, nil // Graceful degradation
	}

	totalTokens := 0
	for _, msg := range req.Messages {
		// Try to parse content as string first (simple text message)
		var contentStr string
		if err := json.Unmarshal(msg.Content, &contentStr); err == nil {
			tokens, err := counter.CountTokens(contentStr)
			if err != nil {
				log.Printf("Warning: failed to count tokens for message: %v", err)
				continue
			}
			totalTokens += tokens
			continue
		}

		// If not a string, try array of ContentBlock (multi-modal message)
		var contentBlocks []ContentBlock
		if err := json.Unmarshal(msg.Content, &contentBlocks); err == nil {
			for _, block := range contentBlocks {
				if block.Type == "text" && block.Text != "" {
					tokens, err := counter.CountTokens(block.Text)
					if err != nil {
						log.Printf("Warning: failed to count tokens for content block: %v", err)
						continue
					}
					totalTokens += tokens
				}
				// Other block types (image, etc.) are skipped for token counting
			}
		} else {
			log.Printf("Warning: failed to parse message content (neither string nor array): %v", err)
		}
	}

	return totalTokens, nil
}

// ResponseBodyCapture captures streaming response body for token counting
type ResponseBodyCapture struct {
	originalBody io.ReadCloser
	buffer       *bytes.Buffer
	teeReader    io.Reader
	counter      TokenCounter
}

// NewResponseBodyCapture creates a new response body capture that uses io.TeeReader
func NewResponseBodyCapture(body io.ReadCloser, counter TokenCounter) *ResponseBodyCapture {
	buffer := &bytes.Buffer{}
	teeReader := io.TeeReader(body, buffer)

	return &ResponseBodyCapture{
		originalBody: body,
		buffer:       buffer,
		teeReader:    teeReader,
		counter:      counter,
	}
}

// WrapResponseWithUsage wraps a non-streaming Z.AI JSON response with Claude-compatible usage field
// This enables ccdash to track GLM token consumption from session logs
func WrapResponseWithUsage(originalResp []byte, inputTokens, outputTokens int) ([]byte, error) {
	// Parse the original Z.AI response
	var zaiResp map[string]interface{}
	if err := json.Unmarshal(originalResp, &zaiResp); err != nil {
		log.Printf("Warning: failed to parse Z.AI response: %v", err)
		return originalResp, err // Return original on parse error
	}

	// Extract the actual result from Z.AI response structure
	var result interface{}
	if res, ok := zaiResp["result"]; ok {
		result = res
	} else {
		result = zaiResp
	}

	// Wrap in Claude-compatible format with usage field
	wrapped := map[string]interface{}{
		"result": result,
		"usage": map[string]interface{}{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
			"cache_read_input_tokens": 0,
			"cache_creation_input_tokens": 0,
		},
	}

	wrappedJSON, err := json.Marshal(wrapped)
	if err != nil {
		log.Printf("Warning: failed to marshal wrapped response: %v", err)
		return originalResp, err
	}

	log.Printf("Injected usage into Z.AI response: input=%d, output=%d", inputTokens, outputTokens)
	return wrappedJSON, nil
}

// Read implements io.Reader, forwarding reads while capturing content
func (rbc *ResponseBodyCapture) Read(p []byte) (n int, err error) {
	return rbc.teeReader.Read(p)
}

// Close implements io.Closer
func (rbc *ResponseBodyCapture) Close() error {
	return rbc.originalBody.Close()
}

// GetCapturedContent returns the captured response body
func (rbc *ResponseBodyCapture) GetCapturedContent() []byte {
	return rbc.buffer.Bytes()
}

// CountOutputTokens counts tokens in the captured response
func (rbc *ResponseBodyCapture) CountOutputTokens() (int, error) {
	content := rbc.buffer.Bytes()
	if len(content) == 0 {
		return 0, nil
	}

	// Check if this is a streaming response (SSE format)
	if bytes.Contains(content, []byte("data: ")) {
		return rbc.countSSETokens(content)
	}

	// Non-streaming response
	return rbc.countJSONTokens(content)
}

// countSSETokens counts tokens in SSE (Server-Sent Events) streaming response
func (rbc *ResponseBodyCapture) countSSETokens(content []byte) (int, error) {
	lines := bytes.Split(content, []byte("\n"))
	totalTokens := 0

	for _, line := range lines {
		// Parse SSE data lines
		if !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}

		jsonData := bytes.TrimPrefix(line, []byte("data: "))
		if len(jsonData) == 0 {
			continue
		}

		var event map[string]interface{}
		if err := json.Unmarshal(jsonData, &event); err != nil {
			continue
		}

		// Extract text from content_block_delta events
		if eventType, ok := event["type"].(string); ok && eventType == "content_block_delta" {
			if delta, ok := event["delta"].(map[string]interface{}); ok {
				if text, ok := delta["text"].(string); ok {
					tokens, err := rbc.counter.CountTokens(text)
					if err == nil {
						totalTokens += tokens
					}
				}
			}
		}
	}

	return totalTokens, nil
}

// countJSONTokens counts tokens in non-streaming JSON response
func (rbc *ResponseBodyCapture) countJSONTokens(content []byte) (int, error) {
	var resp map[string]interface{}
	if err := json.Unmarshal(content, &resp); err != nil {
		log.Printf("Warning: failed to parse response body for token counting: %v", err)
		return 0, nil
	}

	totalTokens := 0

	// Extract text from content blocks
	if contentBlocks, ok := resp["content"].([]interface{}); ok {
		for _, block := range contentBlocks {
			if blockMap, ok := block.(map[string]interface{}); ok {
				if text, ok := blockMap["text"].(string); ok {
					tokens, err := rbc.counter.CountTokens(text)
					if err == nil {
						totalTokens += tokens
					}
				}
			}
		}
	}

	return totalTokens, nil
}

// InjectTokenUsage injects token usage into response body
// Note: SSE streaming responses are handled by StreamingResponseBodyCapture in main.go
// This function only handles non-streaming JSON responses
func InjectTokenUsage(body []byte, inputTokens, outputTokens int) ([]byte, error) {
	// For SSE format, return as-is - streaming is handled elsewhere
	if bytes.Contains(body, []byte("data: ")) {
		return body, nil
	}

	// Non-streaming JSON response
	return injectJSONUsage(body, inputTokens, outputTokens)
}

// injectSSEUsage injects token usage into the message_delta event in an SSE response.
func injectSSEUsage(body []byte, inputTokens, outputTokens int) ([]byte, error) {
	lines := strings.Split(string(body), "\n")
	var out []string
	for _, line := range lines {
		if !strings.HasPrefix(line, "data: ") {
			out = append(out, line)
			continue
		}
		jsonData := strings.TrimPrefix(line, "data: ")
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
			out = append(out, line)
			continue
		}
		if eventType, ok := event["type"].(string); ok && eventType == "message_delta" {
			event["usage"] = map[string]int{
				"input_tokens":  inputTokens,
				"output_tokens": outputTokens,
			}
			modified, err := json.Marshal(event)
			if err != nil {
				out = append(out, line)
				continue
			}
			out = append(out, "data: "+string(modified))
			continue
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n")), nil
}

// injectJSONUsage adds usage field to JSON response
func injectJSONUsage(body []byte, inputTokens, outputTokens int) ([]byte, error) {
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		log.Printf("Warning: failed to parse response for usage injection: %v", err)
		return body, nil // Return original on error
	}

	resp["usage"] = map[string]int{
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
	}

	return json.Marshal(resp)
}

// StreamingResponseBodyCapture captures streaming response body for token counting
// and injects usage information into the message_delta event
type StreamingResponseBodyCapture struct {
	originalBody io.ReadCloser
	buffer       *bytes.Buffer
	teeReader    io.Reader
	counter      TokenCounter
	inputTokens  int
	outputTokens int
	state        string    // "reading", "injecting", "done"
	injectBuffer []byte
	deltaSeen    bool
	usage        UsageData // API-reported token counts accumulated from SSE events
}

// NewStreamingResponseBodyCapture creates a new streaming response body capture
// that injects token usage into the message_delta SSE event
func NewStreamingResponseBodyCapture(body io.ReadCloser, counter TokenCounter, inputTokens int) *StreamingResponseBodyCapture {
	buffer := &bytes.Buffer{}
	teeReader := io.TeeReader(body, buffer)

	return &StreamingResponseBodyCapture{
		originalBody: body,
		buffer:       buffer,
		teeReader:    teeReader,
		counter:      counter,
		inputTokens:  inputTokens,
		state:        "reading",
		deltaSeen:    false,
	}
}

// Read implements io.Reader with on-the-fly SSE usage injection
func (srbc *StreamingResponseBodyCapture) Read(p []byte) (n int, err error) {
	// If we have data in the inject buffer, return that first
	if len(srbc.injectBuffer) > 0 {
		n = copy(p, srbc.injectBuffer)
		srbc.injectBuffer = srbc.injectBuffer[n:]
		if len(srbc.injectBuffer) == 0 {
			srbc.state = "done"
		}
		return n, nil
	}

	// Read from the underlying reader
	n, err = srbc.teeReader.Read(p)
	if n > 0 {
		// Process the newly read data to find and inject usage
		srbc.processChunk(p[:n], &n)
	}
	return n, err
}

// processChunk processes a chunk of data to inject usage into message_delta
func (srbc *StreamingResponseBodyCapture) processChunk(chunk []byte, n *int) {
	// IMPORTANT: Count tokens FIRST, before checking for message_delta.
	// This ensures tokens from content_block_delta events in the same chunk
	// as message_delta are counted before we inject the usage.
	srbc.countTokensInChunk(chunk)

	// Look for message_delta events in the chunk
	data := string(chunk)

	// Check if this chunk contains "message_delta"
	if !srbc.deltaSeen && strings.Contains(data, "message_delta") {
		srbc.deltaSeen = true

		// Parse and inject usage
		lines := strings.Split(data, "\n")
		modifiedLines := make([]string, 0, len(lines))

		for _, line := range lines {
			if !strings.HasPrefix(line, "data: ") {
				modifiedLines = append(modifiedLines, line)
				continue
			}

			jsonData := strings.TrimPrefix(line, "data: ")
			if jsonData == "" {
				modifiedLines = append(modifiedLines, line)
				continue
			}

			var event map[string]interface{}
			if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
				modifiedLines = append(modifiedLines, line)
				continue
			}

			// Inject usage into message_delta event
			if eventType, ok := event["type"].(string); ok && eventType == "message_delta" {
				// Check if upstream API already provided usage - pass through if so
				if existingUsage, ok := event["usage"].(map[string]interface{}); ok && len(existingUsage) > 0 {
					log.Printf("Using upstream usage from message_delta: %+v", existingUsage)
					modifiedLines = append(modifiedLines, line)
					continue
				}

				// No upstream usage provided, inject proxy-counted values
				event["usage"] = map[string]int{
					"input_tokens":                srbc.inputTokens,
					"output_tokens":               srbc.outputTokens,
					"cache_creation_input_tokens": 0,
					"cache_read_input_tokens":     0,
				}

				modifiedJSON, err := json.Marshal(event)
				if err == nil {
					modifiedLines = append(modifiedLines, "data: "+string(modifiedJSON))
					log.Printf("Injected token usage into message_delta: input=%d, output=%d", srbc.inputTokens, srbc.outputTokens)
					continue
				}
			}

			modifiedLines = append(modifiedLines, line)
		}

		// Reconstruct the chunk with modifications
		modifiedData := strings.Join(modifiedLines, "\n")
		*n = copy(chunk, modifiedData)
		if len(modifiedData) > len(chunk) {
			// If modified data is larger, store the overflow in injectBuffer
			srbc.injectBuffer = []byte(modifiedData[len(chunk):])
		}
	}
}

// countTokensInChunk extracts token usage from SSE events.
// message_start provides input + cache counts; message_delta provides output count.
// content_block_delta text is counted via tiktoken as a fallback for output.
func (srbc *StreamingResponseBodyCapture) countTokensInChunk(chunk []byte) {
	lines := strings.Split(string(chunk), "\n")
	for _, line := range lines {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		jsonData := strings.TrimPrefix(line, "data: ")
		if len(jsonData) == 0 {
			continue
		}
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
			continue
		}
		eventType, _ := event["type"].(string)
		switch eventType {
		case "message_start":
			if msg, ok := event["message"].(map[string]interface{}); ok {
				if u, ok := msg["usage"].(map[string]interface{}); ok {
					srbc.usage.InputTokens = int(jsonFloat(u["input_tokens"]))
					srbc.usage.CacheReadTokens = int(jsonFloat(u["cache_read_input_tokens"]))
					srbc.usage.CacheWriteTokens = int(jsonFloat(u["cache_creation_input_tokens"]))
					srbc.usage.FromAPI = true
				}
			}
		case "message_delta":
			if u, ok := event["usage"].(map[string]interface{}); ok {
				if out := int(jsonFloat(u["output_tokens"])); out > 0 {
					srbc.usage.OutputTokens = out
					srbc.usage.FromAPI = true
				}
			}
		case "content_block_delta":
			if srbc.counter != nil {
				if delta, ok := event["delta"].(map[string]interface{}); ok {
					if text, ok := delta["text"].(string); ok {
						if tokens, err := srbc.counter.CountTokens(text); err == nil {
							srbc.outputTokens += tokens
						}
					}
				}
			}
		}
	}
}

// GetUsage returns API-reported token counts, falling back to tiktoken estimates
// for any values the API did not provide.
func (srbc *StreamingResponseBodyCapture) GetUsage() UsageData {
	if srbc.usage.FromAPI {
		result := srbc.usage
		if result.OutputTokens == 0 && srbc.outputTokens > 0 {
			result.OutputTokens = srbc.outputTokens
		}
		return result
	}
	return UsageData{
		InputTokens:  srbc.inputTokens,
		OutputTokens: srbc.outputTokens,
	}
}

// Close implements io.Closer
func (srbc *StreamingResponseBodyCapture) Close() error {
	return srbc.originalBody.Close()
}

// GetOutputTokenCount returns the counted output tokens
func (srbc *StreamingResponseBodyCapture) GetOutputTokenCount() int {
	return srbc.outputTokens
}
