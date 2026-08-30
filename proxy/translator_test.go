package main

import (
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
	// TranslateRequest is a no-op and does not validate JSON
	// It passes through the body unchanged even for invalid JSON
	out, changed, err := TranslateRequest([]byte(`{invalid`))
	if err != nil {
		t.Errorf("unexpected error for invalid JSON: %v", err)
	}
	if changed {
		t.Error("expected changed=false for no-op translation")
	}
	if string(out) != `{invalid` {
		t.Errorf("body should be passed through unchanged: got %s", string(out))
	}
}

func TestTranslateRequest_StripsThinking(t *testing.T) {
	// TranslateRequest is a no-op - it does not strip thinking field
	body := `{"model":"claude-3-5-sonnet","max_tokens":16000,"thinking":{"type":"enabled","budget_tokens":10000},"messages":[{"role":"user","content":"hello"}]}`
	out, changed, err := TranslateRequest([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Error("expected changed=false for no-op translation")
	}

	if string(out) != body {
		t.Errorf("body should be passed through unchanged: got %s", string(out))
	}
}

func TestTranslateRequest_SystemArrayToString(t *testing.T) {
	// TranslateRequest is a no-op - it does not convert system arrays to strings
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
	if changed {
		t.Error("expected changed=false for no-op translation")
	}

	if string(out) != body {
		t.Errorf("body should be passed through unchanged: got %s", string(out))
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
	// TranslateRequest is a no-op - it does not strip cache_control from messages
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
	if changed {
		t.Error("expected changed=false for no-op translation")
	}

	if string(out) != body {
		t.Errorf("body should be passed through unchanged: got %s", string(out))
	}
}

func TestTranslateRequest_CombinedTransformations(t *testing.T) {
	// TranslateRequest is a no-op - it does not perform any transformations
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
	if changed {
		t.Error("expected changed=false for no-op translation")
	}

	if string(out) != body {
		t.Errorf("body should be passed through unchanged: got %s", string(out))
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
