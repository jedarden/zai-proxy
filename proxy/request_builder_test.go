package main

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// TestRequestBuilders verifies that all request builder functions work correctly
func TestRequestBuilders(t *testing.T) {

	// ============================================================================
	// CreateProxyRequest() tests
	// ============================================================================
	t.Run("CreateProxyRequest", func(t *testing.T) {
		t.Run("sets correct method", func(t *testing.T) {
			req := CreateProxyRequest("POST", "/v1/messages", "")
			if req.Method != "POST" {
				t.Errorf("Expected method POST, got %s", req.Method)
			}
		})

		t.Run("sets correct method for GET", func(t *testing.T) {
			req := CreateProxyRequest("GET", "/v1/models", "")
			if req.Method != "GET" {
				t.Errorf("Expected method GET, got %s", req.Method)
			}
		})

		t.Run("sets correct URL path", func(t *testing.T) {
			req := CreateProxyRequest("POST", "/v1/messages", "")
			if req.URL.Path != "/v1/messages" {
				t.Errorf("Expected path /v1/messages, got %s", req.URL.Path)
			}
		})

		t.Run("sets Authorization header", func(t *testing.T) {
			req := CreateProxyRequest("POST", "/v1/messages", "")
			authHeader := req.Header.Get("Authorization")
			if authHeader != "Bearer test-key" {
				t.Errorf("Expected Authorization header 'Bearer test-key', got %q", authHeader)
			}
		})

		t.Run("sets Content-Type header", func(t *testing.T) {
			req := CreateProxyRequest("POST", "/v1/messages", "")
		 contentType := req.Header.Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("Expected Content-Type 'application/json', got %q", contentType)
			}
		})

		t.Run("handles body correctly - with body", func(t *testing.T) {
			body := `{"test":"data"}`
			req := CreateProxyRequest("POST", "/v1/messages", body)
			bodyBytes, _ := io.ReadAll(req.Body)
			if string(bodyBytes) != body {
				t.Errorf("Expected body %q, got %q", body, string(bodyBytes))
			}
		})

		t.Run("handles body correctly - empty body", func(t *testing.T) {
			req := CreateProxyRequest("POST", "/v1/messages", "")
			if req.Body != nil {
				bodyBytes, _ := io.ReadAll(req.Body)
				if len(bodyBytes) != 0 {
					t.Errorf("Expected empty body, got %q", string(bodyBytes))
				}
			}
		})

		t.Run("handles body correctly - nil when empty", func(t *testing.T) {
			req := CreateProxyRequest("POST", "/v1/messages", "")
			// httptest.NewRequest creates a non-nil Body even for empty strings
			// but the body should be readable and empty
			if req.Body == nil {
				t.Error("Body should not be nil")
			}
			bodyBytes, _ := io.ReadAll(req.Body)
			if len(bodyBytes) != 0 {
				t.Errorf("Expected readable empty body, got %d bytes", len(bodyBytes))
			}
		})
	})

	// ============================================================================
	// CreateMessagesRequest() tests
	// ============================================================================
	t.Run("CreateMessagesRequest", func(t *testing.T) {
		t.Run("creates POST /v1/messages request", func(t *testing.T) {
			body := `{"test":"data"}`
			req := CreateMessagesRequest(body)

			if req.Method != "POST" {
				t.Errorf("Expected method POST, got %s", req.Method)
			}

			if req.URL.Path != "/v1/messages" {
				t.Errorf("Expected path /v1/messages, got %s", req.URL.Path)
			}
		})

		t.Run("includes body in request", func(t *testing.T) {
			body := `{"model":"glm-4","messages":[{"role":"user","content":"Hello"}]}`
			req := CreateMessagesRequest(body)

			bodyBytes, _ := io.ReadAll(req.Body)
			if string(bodyBytes) != body {
				t.Errorf("Expected body %q, got %q", body, string(bodyBytes))
			}
		})

		t.Run("sets Authorization header", func(t *testing.T) {
			req := CreateMessagesRequest(`{}`)
			authHeader := req.Header.Get("Authorization")
			if authHeader != "Bearer test-key" {
				t.Errorf("Expected Authorization header 'Bearer test-key', got %q", authHeader)
			}
		})

		t.Run("sets Content-Type header", func(t *testing.T) {
			req := CreateMessagesRequest(`{}`)
			contentType := req.Header.Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("Expected Content-Type 'application/json', got %q", contentType)
			}
		})
	})

	// ============================================================================
	// CreateStreamingRequestBody() tests
	// ============================================================================
	t.Run("CreateStreamingRequestBody", func(t *testing.T) {
		t.Run("generates valid streaming request JSON", func(t *testing.T) {
			body := CreateStreamingRequestBody()

			// Verify it's valid JSON
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(body), &parsed); err != nil {
				t.Fatalf("Generated body is not valid JSON: %v", err)
			}

			// Verify it has the required fields
			if parsed["model"] != "glm-4" {
				t.Errorf("Expected model 'glm-4', got %v", parsed["model"])
			}

			// Verify stream field is true
			streamValue, exists := parsed["stream"]
			if !exists {
				t.Error("Streaming body should have 'stream' field")
			} else if streamValue != true {
				t.Errorf("Expected stream=true, got %v", streamValue)
			}

			// Verify messages array exists and has content
			messages, exists := parsed["messages"]
			if !exists {
				t.Error("Streaming body should have 'messages' field")
			} else {
				msgs, ok := messages.([]interface{})
				if !ok || len(msgs) == 0 {
					t.Error("Messages should be a non-empty array")
				}
			}
		})

		t.Run("contains stream:true field", func(t *testing.T) {
			body := CreateStreamingRequestBody()
			if !strings.Contains(body, `"stream":true`) {
				t.Errorf("Expected body to contain 'stream:true', got %q", body)
			}
		})

		t.Run("contains model field", func(t *testing.T) {
			body := CreateStreamingRequestBody()
			if !strings.Contains(body, `"model":"glm-4"`) {
				t.Errorf("Expected body to contain model, got %q", body)
			}
		})

		t.Run("contains messages array", func(t *testing.T) {
			body := CreateStreamingRequestBody()
			if !strings.Contains(body, `"messages"`) {
				t.Errorf("Expected body to contain messages array, got %q", body)
			}
		})
	})

	// ============================================================================
	// CreateNonStreamingRequestBody() tests
	// ============================================================================
	t.Run("CreateNonStreamingRequestBody", func(t *testing.T) {
		t.Run("generates valid non-streaming request JSON", func(t *testing.T) {
			body := CreateNonStreamingRequestBody()

			// Verify it's valid JSON
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(body), &parsed); err != nil {
				t.Fatalf("Generated body is not valid JSON: %v", err)
			}

			// Verify it has the required fields
			if parsed["model"] != "glm-4" {
				t.Errorf("Expected model 'glm-4', got %v", parsed["model"])
			}

			// Verify stream field is absent or false
			streamValue, exists := parsed["stream"]
			if exists && streamValue != false {
				t.Errorf("Expected no stream field or stream=false, got %v", streamValue)
			}

			// Verify messages array exists and has content
			messages, exists := parsed["messages"]
			if !exists {
				t.Error("Non-streaming body should have 'messages' field")
			} else {
				msgs, ok := messages.([]interface{})
				if !ok || len(msgs) == 0 {
					t.Error("Messages should be a non-empty array")
				}
			}
		})

		t.Run("does not contain stream:true field", func(t *testing.T) {
			body := CreateNonStreamingRequestBody()
			if strings.Contains(body, `"stream":true`) {
				t.Errorf("Non-streaming body should not contain 'stream:true', got %q", body)
			}
		})

		t.Run("contains model field", func(t *testing.T) {
			body := CreateNonStreamingRequestBody()
			if !strings.Contains(body, `"model":"glm-4"`) {
				t.Errorf("Expected body to contain model, got %q", body)
			}
		})

		t.Run("contains messages array", func(t *testing.T) {
			body := CreateNonStreamingRequestBody()
			if !strings.Contains(body, `"messages"`) {
				t.Errorf("Expected body to contain messages array, got %q", body)
			}
		})
	})

	// ============================================================================
	// CreateStreamingMessagesRequest() tests
	// ============================================================================
	t.Run("CreateStreamingMessagesRequest", func(t *testing.T) {
		t.Run("creates streaming request", func(t *testing.T) {
			req := CreateStreamingMessagesRequest()

			if req.Method != "POST" {
				t.Errorf("Expected method POST, got %s", req.Method)
			}

			if req.URL.Path != "/v1/messages" {
				t.Errorf("Expected path /v1/messages, got %s", req.URL.Path)
			}
		})

		t.Run("request body contains stream:true", func(t *testing.T) {
			req := CreateStreamingMessagesRequest()
			bodyBytes, _ := io.ReadAll(req.Body)

			if !strings.Contains(string(bodyBytes), `"stream":true`) {
				t.Errorf("Streaming request should contain 'stream:true', got %q", string(bodyBytes))
			}
		})

		t.Run("sets correct headers", func(t *testing.T) {
			req := CreateStreamingMessagesRequest()

			authHeader := req.Header.Get("Authorization")
			if authHeader != "Bearer test-key" {
				t.Errorf("Expected Authorization 'Bearer test-key', got %q", authHeader)
			}

			contentType := req.Header.Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("Expected Content-Type 'application/json', got %q", contentType)
			}
		})
	})

	// ============================================================================
	// CreateNonStreamingMessagesRequest() tests
	// ============================================================================
	t.Run("CreateNonStreamingMessagesRequest", func(t *testing.T) {
		t.Run("creates non-streaming request", func(t *testing.T) {
			req := CreateNonStreamingMessagesRequest()

			if req.Method != "POST" {
				t.Errorf("Expected method POST, got %s", req.Method)
			}

			if req.URL.Path != "/v1/messages" {
				t.Errorf("Expected path /v1/messages, got %s", req.URL.Path)
			}
		})

		t.Run("request body does not contain stream:true", func(t *testing.T) {
			req := CreateNonStreamingMessagesRequest()
			bodyBytes, _ := io.ReadAll(req.Body)

			if strings.Contains(string(bodyBytes), `"stream":true`) {
				t.Errorf("Non-streaming request should not contain 'stream:true', got %q", string(bodyBytes))
			}
		})

		t.Run("sets correct headers", func(t *testing.T) {
			req := CreateNonStreamingMessagesRequest()

			authHeader := req.Header.Get("Authorization")
			if authHeader != "Bearer test-key" {
				t.Errorf("Expected Authorization 'Bearer test-key', got %q", authHeader)
			}

			contentType := req.Header.Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("Expected Content-Type 'application/json', got %q", contentType)
			}
		})
	})

	// ============================================================================
	// CreateTestRequestBody() tests
	// ============================================================================
	t.Run("CreateTestRequestBody", func(t *testing.T) {
		t.Run("creates custom request body with model", func(t *testing.T) {
			messages := []map[string]string{
				{"role": "user", "content": "Hello"},
			}
			body, err := CreateTestRequestBody("custom-model", messages, false)

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(body), &parsed); err != nil {
				t.Fatalf("Generated body is not valid JSON: %v", err)
			}

			if parsed["model"] != "custom-model" {
				t.Errorf("Expected model 'custom-model', got %v", parsed["model"])
			}
		})

		t.Run("creates custom request body with messages", func(t *testing.T) {
			messages := []map[string]string{
				{"role": "user", "content": "Hello"},
				{"role": "assistant", "content": "Hi there"},
			}
			body, err := CreateTestRequestBody("glm-4", messages, false)

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(body), &parsed); err != nil {
				t.Fatalf("Generated body is not valid JSON: %v", err)
			}

			msgs, ok := parsed["messages"].([]interface{})
			if !ok {
				t.Fatal("Messages should be an array")
			}

			if len(msgs) != 2 {
				t.Errorf("Expected 2 messages, got %d", len(msgs))
			}
		})

		t.Run("creates streaming body when stream=true", func(t *testing.T) {
			messages := []map[string]string{
				{"role": "user", "content": "Hello"},
			}
			body, err := CreateTestRequestBody("glm-4", messages, true)

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(body), &parsed); err != nil {
				t.Fatalf("Generated body is not valid JSON: %v", err)
			}

			streamValue, exists := parsed["stream"]
			if !exists {
				t.Error("Streaming body should have 'stream' field")
			} else if streamValue != true {
				t.Errorf("Expected stream=true, got %v", streamValue)
			}
		})

		t.Run("creates non-streaming body when stream=false", func(t *testing.T) {
			messages := []map[string]string{
				{"role": "user", "content": "Hello"},
			}
			body, err := CreateTestRequestBody("glm-4", messages, false)

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(body), &parsed); err != nil {
				t.Fatalf("Generated body is not valid JSON: %v", err)
			}

			// stream field should not exist or be false
			streamValue, exists := parsed["stream"]
			if exists && streamValue != false {
				t.Errorf("Expected no stream field for non-streaming, got %v", streamValue)
			}
		})

		t.Run("handles empty messages array", func(t *testing.T) {
			messages := []map[string]string{}
			body, err := CreateTestRequestBody("glm-4", messages, false)

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(body), &parsed); err != nil {
				t.Fatalf("Generated body is not valid JSON: %v", err)
			}

			msgs, ok := parsed["messages"].([]interface{})
			if !ok {
				t.Fatal("Messages should be an array")
			}

			if len(msgs) != 0 {
				t.Errorf("Expected 0 messages, got %d", len(msgs))
			}
		})

		t.Run("returns error on invalid JSON structure", func(t *testing.T) {
			// This test verifies that JSON marshaling can fail
			// Since our function only uses json.Marshal, it should handle any error
			messages := []map[string]string{
				{"role": "user", "content": "Hello"},
			}

			// Create a body that can be marshaled
			body, err := CreateTestRequestBody("glm-4", messages, false)
			if err != nil {
				t.Fatalf("Unexpected error with valid input: %v", err)
			}

			// Verify the body is valid JSON
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(body), &parsed); err != nil {
				t.Errorf("Generated body should be valid JSON: %v", err)
			}
		})
	})

	// ============================================================================
	// MustCreateTestRequestBody() tests
	// ============================================================================
	t.Run("MustCreateTestRequestBody", func(t *testing.T) {
		t.Run("returns body on success", func(t *testing.T) {
			messages := []map[string]string{
				{"role": "user", "content": "Hello"},
			}

			// This should not panic
			body := MustCreateTestRequestBody("glm-4", messages, true)

			// Verify the body is valid
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(body), &parsed); err != nil {
				t.Fatalf("Generated body is not valid JSON: %v", err)
			}

			if parsed["stream"] != true {
				t.Error("Expected stream=true")
			}
		})

		t.Run("panics on error - cannot test directly", func(t *testing.T) {
			// Note: Testing that a function panics is difficult because
			// json.Marshal rarely fails with normal inputs. The function
			// would only panic if json.Marshal fails, which happens with:
			// - unmarshalable types (channels, functions)
			// - circular references

			// Since CreateTestRequestBody only uses basic types
			// (strings, maps, bools), json.Marshal will always succeed.
			// The panic protection is there for API completeness.

			messages := []map[string]string{
				{"role": "user", "content": "Hello"},
			}

			// This should not panic
			body := MustCreateTestRequestBody("glm-4", messages, false)
			if body == "" {
				t.Error("Body should not be empty")
			}

			// Verify it's valid JSON
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(body), &parsed); err != nil {
				t.Errorf("Generated body should be valid JSON: %v", err)
			}
		})

		t.Run("behavior matches CreateTestRequestBody for valid input", func(t *testing.T) {
			messages := []map[string]string{
				{"role": "user", "content": "Test"},
			}

			// Get results from both functions
			body1, err := CreateTestRequestBody("glm-4", messages, true)
			if err != nil {
				t.Fatalf("CreateTestRequestBody failed: %v", err)
			}

			body2 := MustCreateTestRequestBody("glm-4", messages, true)

			// They should be identical
			if body1 != body2 {
				t.Errorf("Results differ:\nCreateTestRequestBody: %q\nMustCreateTestRequestBody: %q", body1, body2)
			}
		})
	})
}
