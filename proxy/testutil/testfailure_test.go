package testutil

import (
	"encoding/json"
	"testing"
)

func TestTestFailureJSONSerialization(t *testing.T) {
	// Create a TestFailure instance with sample data
	failure := TestFailure{
		TestName:    "TestExampleFunction",
		FilePath:    "proxy/handlers_test.go",
		LineNumber:  42,
		ErrorMessage: "expected 200, got 500",
		StackTrace: "goroutine 1 [running]:\ntestExampleFunction()\n\t/proxy/handlers_test.go:42",
	}

	// Serialize to JSON
	data, err := json.Marshal(failure)
	if err != nil {
		t.Fatalf("Failed to marshal TestFailure to JSON: %v", err)
	}

	// Deserialize back to struct
	var decoded TestFailure
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal JSON to TestFailure: %v", err)
	}

	// Verify all fields match
	if decoded.TestName != failure.TestName {
		t.Errorf("TestName mismatch: got %q, want %q", decoded.TestName, failure.TestName)
	}
	if decoded.FilePath != failure.FilePath {
		t.Errorf("FilePath mismatch: got %q, want %q", decoded.FilePath, failure.FilePath)
	}
	if decoded.LineNumber != failure.LineNumber {
		t.Errorf("LineNumber mismatch: got %d, want %d", decoded.LineNumber, failure.LineNumber)
	}
	if decoded.ErrorMessage != failure.ErrorMessage {
		t.Errorf("ErrorMessage mismatch: got %q, want %q", decoded.ErrorMessage, failure.ErrorMessage)
	}
	if decoded.StackTrace != failure.StackTrace {
		t.Errorf("StackTrace mismatch: got %q, want %q", decoded.StackTrace, failure.StackTrace)
	}

	// Verify JSON field names match expectations
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Failed to unmarshal to raw map: %v", err)
	}

	expectedFields := []string{"test_name", "file_path", "line_number", "error_message", "stack_trace"}
	for _, field := range expectedFields {
		if _, ok := raw[field]; !ok {
			t.Errorf("Missing expected JSON field: %s", field)
		}
	}
}

func TestTestFailureWithEmptyStackTrace(t *testing.T) {
	// Test that omitempty works for empty StackTrace
	failure := TestFailure{
		TestName:    "TestSimple",
		FilePath:    "proxy/simple_test.go",
		LineNumber:  10,
		ErrorMessage: "assertion failed",
		StackTrace: "",
	}

	data, err := json.Marshal(failure)
	if err != nil {
		t.Fatalf("Failed to marshal TestFailure with empty StackTrace: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Failed to unmarshal to raw map: %v", err)
	}

	// stack_trace should be omitted when empty
	if _, ok := raw["stack_trace"]; ok {
		t.Error("stack_trace field should be omitted when empty (omitempty not working)")
	}
}
