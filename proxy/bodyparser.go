package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
)

// ParseRequestBody parses the request body and extracts all text content
// Supports both simple string content and multi-modal array content
// Returns all text content concatenated, suitable for token counting
func ParseRequestBody(body []byte) ([]string, error) {
	if len(body) == 0 {
		return nil, nil
	}

	var req RequestBody
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("Warning: failed to parse request body: %v", err)
		return nil, nil // Graceful degradation
	}

	var textParts []string
	for _, msg := range req.Messages {
		// Try to parse content as string first (simple text message)
		var contentStr string
		if err := json.Unmarshal(msg.Content, &contentStr); err == nil {
			if contentStr != "" {
				textParts = append(textParts, contentStr)
			}
			continue
		}

		// If not a string, try array of ContentBlock (multi-modal message)
		var contentBlocks []ContentBlock
		if err := json.Unmarshal(msg.Content, &contentBlocks); err == nil {
			for _, block := range contentBlocks {
				if block.Type == "text" && block.Text != "" {
					textParts = append(textParts, block.Text)
				}
				// Other block types (image, etc.) are skipped
			}
		} else {
			log.Printf("Warning: failed to parse message content (neither string nor array): %v", err)
		}
	}

	return textParts, nil
}

// IsStreamingRequest checks if the request is for streaming response
func IsStreamingRequest(body []byte) bool {
	if len(body) == 0 {
		return false
	}

	var req RequestBody
	if err := json.Unmarshal(body, &req); err != nil {
		return false
	}

	return req.Stream
}

// BodyTeeReader captures request body while allowing it to be forwarded
// Uses io.TeeReader to duplicate the stream - one copy for reading, one for capture
type BodyTeeReader struct {
	originalBody io.ReadCloser
	buffer       *bytes.Buffer
	teeReader    io.Reader
}

// NewBodyTeeReader creates a new body tee reader that captures while forwarding
func NewBodyTeeReader(body io.ReadCloser) *BodyTeeReader {
	buffer := &bytes.Buffer{}
	teeReader := io.TeeReader(body, buffer)

	return &BodyTeeReader{
		originalBody: body,
		buffer:       buffer,
		teeReader:    teeReader,
	}
}

// Read implements io.Reader, forwarding reads while capturing content
func (btr *BodyTeeReader) Read(p []byte) (n int, err error) {
	return btr.teeReader.Read(p)
}

// Close implements io.Closer
func (btr *BodyTeeReader) Close() error {
	return btr.originalBody.Close()
}

// GetCapturedBody returns the captured request body
func (btr *BodyTeeReader) GetCapturedBody() []byte {
	return btr.buffer.Bytes()
}

// ParseResponseBody parses a non-streaming JSON response and extracts text content
// Returns all text content from content blocks
func ParseResponseBody(body []byte) ([]string, error) {
	if len(body) == 0 {
		return nil, nil
	}

	// Parse as generic map to handle various response formats
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		log.Printf("Warning: failed to parse response body: %v", err)
		return nil, nil // Graceful degradation
	}

	var textParts []string

	// Extract text from content blocks
	if contentBlocks, ok := resp["content"].([]interface{}); ok {
		for _, block := range contentBlocks {
			if blockMap, ok := block.(map[string]interface{}); ok {
				if blockType, ok := blockMap["type"].(string); ok && blockType == "text" {
					if text, ok := blockMap["text"].(string); ok && text != "" {
						textParts = append(textParts, text)
					}
				}
			}
		}
	}

	return textParts, nil
}

// ParseSSEResponse parses Server-Sent Events streaming response
// Extracts text deltas from content_block_delta events
// Returns all text deltas
func ParseSSEResponse(body []byte) ([]string, error) {
	if len(body) == 0 {
		return nil, nil
	}

	lines := bytes.Split(body, []byte("\n"))
	var textParts []string

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
				if text, ok := delta["text"].(string); ok && text != "" {
					textParts = append(textParts, text)
				}
			}
		}
	}

	return textParts, nil
}

// IsSSEFormat checks if the response body is in SSE (Server-Sent Events) format
func IsSSEFormat(body []byte) bool {
	return bytes.Contains(body, []byte("data: "))
}
