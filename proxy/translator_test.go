package main

import (
	"encoding/json"
	"testing"
)

func TestTranslateRequest_NoChangesNeeded(t *testing.T) {
	body := `{"model":"glm-4","max_tokens":1024,"messages":[{"role":"user","content":"hello"}]}`
	out, changed, err := TranslateRequest([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Error("expected no changes, got changed=true")
	}
	if string(out) != body {
		t.Errorf("body should be unchanged: got %s", string(out))
	}
}

func TestTranslateRequest_EmptyBody(t *testing.T) {
	out, changed, err := TranslateRequest(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Error("expected no changes for empty body")
	}
	if len(out) != 0 {
		t.Errorf("expected empty output, got: %s", string(out))
	}
}

func TestTranslateRequest_InvalidJSON(t *testing.T) {
	_, _, err := TranslateRequest([]byte(`{invalid`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestTranslateRequest_StripsThinking(t *testing.T) {
	body := `{"model":"claude-3-5-sonnet","max_tokens":16000,"thinking":{"type":"enabled","budget_tokens":10000},"messages":[{"role":"user","content":"hello"}]}`
	out, changed, err := TranslateRequest([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Error("expected changed=true when thinking is stripped")
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if _, ok := result["thinking"]; ok {
		t.Error("'thinking' should have been removed")
	}
	if _, ok := result["messages"]; !ok {
		t.Error("'messages' should still be present")
	}
}

func TestTranslateRequest_SystemArrayToString(t *testing.T) {
	body := `{
		"model":"glm-4",
		"max_tokens":1024,
		"system":[
			{"type":"text","text":"You are a helpful assistant.","cache_control":{"type":"ephemeral"}},
			{"type":"text","text":"Be concise."}
		],
		"messages":[{"role":"user","content":"hello"}]
	}`
	out, changed, err := TranslateRequest([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Error("expected changed=true when system is converted")
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	system, ok := result["system"].(string)
	if !ok {
		t.Fatalf("'system' should be a string, got %T: %v", result["system"], result["system"])
	}
	if system != "You are a helpful assistant.\nBe concise." {
		t.Errorf("unexpected system string: %q", system)
	}
}

func TestTranslateRequest_SystemStringUnchanged(t *testing.T) {
	body := `{"model":"glm-4","max_tokens":1024,"system":"You are helpful.","messages":[{"role":"user","content":"hi"}]}`
	out, changed, err := TranslateRequest([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Error("expected no changes when system is already a string")
	}
	if string(out) != body {
		t.Errorf("body should be unchanged: got %s", string(out))
	}
}

func TestTranslateRequest_StripsCacheControlFromMessages(t *testing.T) {
	body := `{
		"model":"glm-4",
		"max_tokens":1024,
		"messages":[{
			"role":"user",
			"content":[
				{"type":"text","text":"Hello","cache_control":{"type":"ephemeral"}},
				{"type":"text","text":"World"}
			]
		}]
	}`
	out, changed, err := TranslateRequest([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Error("expected changed=true when cache_control is stripped")
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	messages := result["messages"].([]interface{})
	msg := messages[0].(map[string]interface{})
	content := msg["content"].([]interface{})

	for i, block := range content {
		blockMap := block.(map[string]interface{})
		if _, hasCacheControl := blockMap["cache_control"]; hasCacheControl {
			t.Errorf("content block %d still has cache_control after stripping", i)
		}
	}

	// Text should be preserved
	firstBlock := content[0].(map[string]interface{})
	if firstBlock["text"] != "Hello" {
		t.Errorf("text should be preserved, got: %v", firstBlock["text"])
	}
}

func TestTranslateRequest_CombinedTransformations(t *testing.T) {
	// Request with thinking + system array + cache_control in messages
	body := `{
		"model":"claude-3-5-sonnet",
		"max_tokens":16000,
		"thinking":{"type":"enabled","budget_tokens":10000},
		"system":[{"type":"text","text":"You are a helpful assistant.","cache_control":{"type":"ephemeral"}}],
		"messages":[{
			"role":"user",
			"content":[
				{"type":"text","text":"Explain recursion.","cache_control":{"type":"ephemeral"}}
			]
		}]
	}`
	out, changed, err := TranslateRequest([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Error("expected changed=true for combined transformations")
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if _, ok := result["thinking"]; ok {
		t.Error("'thinking' should have been removed")
	}

	system, ok := result["system"].(string)
	if !ok {
		t.Fatalf("'system' should be a string after translation")
	}
	if system != "You are a helpful assistant." {
		t.Errorf("unexpected system string: %q", system)
	}

	messages := result["messages"].([]interface{})
	msg := messages[0].(map[string]interface{})
	content := msg["content"].([]interface{})
	block := content[0].(map[string]interface{})
	if _, hasCacheControl := block["cache_control"]; hasCacheControl {
		t.Error("cache_control should have been stripped from content block")
	}
	if block["text"] != "Explain recursion." {
		t.Errorf("text should be preserved, got: %v", block["text"])
	}
}

func TestTranslateRequest_StringContentUnchanged(t *testing.T) {
	// Messages with string content (not array) should not be affected
	body := `{"model":"glm-4","max_tokens":1024,"messages":[{"role":"user","content":"hello world"}]}`
	out, changed, err := TranslateRequest([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Error("string content messages should not trigger changes")
	}
	if string(out) != body {
		t.Errorf("body should be unchanged: got %s", string(out))
	}
}
