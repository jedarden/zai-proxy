package testutil

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

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

func TestParseTestFailures_SingleFailure(t *testing.T) {
	// Standard Go test failure output
	output := []byte(`=== RUN   TestRateLimit
    ratelimiter_test.go:45: assertion failed: expected 200, got 500
--- FAIL: TestRateLimit (0.05s)
FAIL`)

	failures, err := ParseTestFailures(output)
	if err != nil {
		t.Fatalf("ParseTestFailures returned error: %v", err)
	}

	if len(failures) != 1 {
		t.Fatalf("Expected 1 failure, got %d", len(failures))
	}

	failure := failures[0]
	if failure.TestName != "TestRateLimit" {
		t.Errorf("TestName: got %q, want %q", failure.TestName, "TestRateLimit")
	}
	if failure.FilePath != "ratelimiter_test.go" {
		t.Errorf("FilePath: got %q, want %q", failure.FilePath, "ratelimiter_test.go")
	}
	if failure.LineNumber != 45 {
		t.Errorf("LineNumber: got %d, want %d", failure.LineNumber, 45)
	}
	if failure.ErrorMessage != "assertion failed: expected 200, got 500" {
		t.Errorf("ErrorMessage: got %q, want %q", failure.ErrorMessage, "assertion failed: expected 200, got 500")
	}
}

func TestParseTestFailures_MultipleFailures(t *testing.T) {
	// Multiple test failures in one output
	output := []byte(`=== RUN   TestRateLimit
    ratelimiter_test.go:45: assertion failed: expected 200, got 500
--- FAIL: TestRateLimit (0.05s)
=== RUN   TestTokenBucket
    ratelimiter_test.go:78: bucket not empty after reset
--- FAIL: TestTokenBucket (0.02s)
FAIL`)

	failures, err := ParseTestFailures(output)
	if err != nil {
		t.Fatalf("ParseTestFailures returned error: %v", err)
	}

	if len(failures) != 2 {
		t.Fatalf("Expected 2 failures, got %d", len(failures))
	}

	// Check first failure
	if failures[0].TestName != "TestRateLimit" {
		t.Errorf("First test name: got %q, want %q", failures[0].TestName, "TestRateLimit")
	}
	if failures[0].FilePath != "ratelimiter_test.go" {
		t.Errorf("First file path: got %q, want %q", failures[0].FilePath, "ratelimiter_test.go")
	}

	// Check second failure
	if failures[1].TestName != "TestTokenBucket" {
		t.Errorf("Second test name: got %q, want %q", failures[1].TestName, "TestTokenBucket")
	}
	if failures[1].LineNumber != 78 {
		t.Errorf("Second line number: got %d, want %d", failures[1].LineNumber, 78)
	}
	if failures[1].ErrorMessage != "bucket not empty after reset" {
		t.Errorf("Second error message: got %q, want %q", failures[1].ErrorMessage, "bucket not empty after reset")
	}
}

func TestParseTestFailures_WithStackTrace(t *testing.T) {
	// Test failure with stack trace
	output := []byte(`=== RUN   TestPanic
    ratelimiter_test.go:123: runtime panic
goroutine 1 [running]:
testing.(*common).Fail()
	/usr/local/go/src/testing/testing.go:522
github.com/user/pkg.TestPanic()
	/home/user/pkg/ratelimiter_test.go:123
--- FAIL: TestPanic (0.01s)
FAIL`)

	failures, err := ParseTestFailures(output)
	if err != nil {
		t.Fatalf("ParseTestFailures returned error: %v", err)
	}

	if len(failures) != 1 {
		t.Fatalf("Expected 1 failure, got %d", len(failures))
	}

	failure := failures[0]
	if failure.TestName != "TestPanic" {
		t.Errorf("TestName: got %q, want %q", failure.TestName, "TestPanic")
	}
	if failure.StackTrace == "" {
		t.Error("StackTrace should not be empty")
	}
	// Verify stack trace contains expected elements
	if !strings.Contains(failure.StackTrace, "goroutine") {
		t.Error("StackTrace should contain 'goroutine'")
	}
}

func TestParseTestFailures_NoFailures(t *testing.T) {
	// Successful test output with no failures
	output := []byte(`=== RUN   TestSuccess
--- PASS: TestSuccess (0.01s)
PASS`)

	failures, err := ParseTestFailures(output)
	if err != nil {
		t.Fatalf("ParseTestFailures returned error: %v", err)
	}

	if len(failures) != 0 {
		t.Fatalf("Expected 0 failures, got %d", len(failures))
	}

	// Verify it's an empty slice, not nil
	if failures == nil {
		t.Error("Expected empty slice, got nil")
	}
}

func TestParseTestFailures_EmptyOutput(t *testing.T) {
	// Empty test output
	output := []byte(``)

	failures, err := ParseTestFailures(output)
	if err != nil {
		t.Fatalf("ParseTestFailures returned error: %v", err)
	}

	if len(failures) != 0 {
		t.Fatalf("Expected 0 failures, got %d", len(failures))
	}
}

func TestParseTestFailures_WhitespaceOnly(t *testing.T) {
	// Output with only whitespace
	output := []byte(`


`)

	failures, err := ParseTestFailures(output)
	if err != nil {
		t.Fatalf("ParseTestFailures returned error: %v", err)
	}

	if len(failures) != 0 {
		t.Fatalf("Expected 0 failures, got %d", len(failures))
	}
}

func TestParseTestFailures_ParallelTests(t *testing.T) {
	// Parallel test output with intermixed RUN/PASS/FAIL lines
	output := []byte(`=== RUN   TestParallelOne
=== RUN   TestParallelTwo
=== PAUSE TestParallelOne
=== PAUSE TestParallelTwo
=== CONT  TestParallelTwo
    handlers_test.go:23: assertion failed in parallel test two
--- FAIL: TestParallelTwo (0.05s)
=== CONT  TestParallelOne
    handlers_test.go:45: assertion failed in parallel test one
--- FAIL: TestParallelOne (0.03s)
FAIL`)

	failures, err := ParseTestFailures(output)
	if err != nil {
		t.Fatalf("ParseTestFailures returned error: %v", err)
	}

	if len(failures) != 2 {
		t.Fatalf("Expected 2 failures, got %d", len(failures))
	}

	// Verify both failures were captured correctly
	if failures[0].TestName != "TestParallelTwo" {
		t.Errorf("First test name: got %q, want %q", failures[0].TestName, "TestParallelTwo")
	}
	if failures[1].TestName != "TestParallelOne" {
		t.Errorf("Second test name: got %q, want %q", failures[1].TestName, "TestParallelOne")
	}
}

func TestParseTestFailures_HelperFrames(t *testing.T) {
	// Test with t.Helper() in stack trace
	output := []byte(`=== RUN   TestWithHelper
    helpers_test.go:78: TestHelper failed
	goroutine 1 [running]:
	testing.(*common).Fail()
		/usr/local/go/src/testing/testing.go:522
	github.com/user/pkg.TestHelper.func1()
		/home/user/pkg/helpers_test.go:45
	testing/testenv.SetEnv()
		/usr/local/go/src/testing/testenv.go:123
	created by testing.tRunner
		/usr/local/go/src/testing/testing.go:689
--- FAIL: TestWithHelper (0.01s)
FAIL`)

	failures, err := ParseTestFailures(output)
	if err != nil {
		t.Fatalf("ParseTestFailures returned error: %v", err)
	}

	if len(failures) != 1 {
		t.Fatalf("Expected 1 failure, got %d", len(failures))
	}

	if failures[0].TestName != "TestWithHelper" {
		t.Errorf("TestName: got %q, want %q", failures[0].TestName, "TestWithHelper")
	}

	// Debug: print what we actually got
	t.Logf("DEBUG: StackTrace length = %d", len(failures[0].StackTrace))
	t.Logf("DEBUG: StackTrace contains 'goroutine' = %v", strings.Contains(failures[0].StackTrace, "goroutine"))
	t.Logf("DEBUG: StackTrace contains 'testing.tRunner' = %v", strings.Contains(failures[0].StackTrace, "testing.tRunner"))
	if len(failures[0].StackTrace) > 0 {
		t.Logf("DEBUG: First 100 chars of StackTrace: %q", failures[0].StackTrace[:min(100, len(failures[0].StackTrace))])
	}

	// Verify stack trace was captured even with helper frames
	if failures[0].StackTrace == "" {
		t.Error("StackTrace should not be empty for helper failure")
	}
	if !strings.Contains(failures[0].StackTrace, "goroutine") {
		t.Error("StackTrace should contain 'goroutine'")
	}
	if !strings.Contains(failures[0].StackTrace, "testing.tRunner") {
		t.Error("StackTrace should contain helper frame marker 'testing.tRunner'")
	}
}

func TestParseTestFailures_MalformedLineNumber(t *testing.T) {
	// Test with malformed line number (non-numeric)
	output := []byte(`=== RUN   TestBadLine
    handlers_test.go:abc: assertion failed
--- FAIL: TestBadLine (0.02s)
FAIL`)

	// This should not fail - it should log a warning and continue
	failures, err := ParseTestFailures(output)
	if err != nil {
		t.Fatalf("ParseTestFailures should not return error for malformed line number: %v", err)
	}

	if len(failures) != 1 {
		t.Fatalf("Expected 1 failure, got %d", len(failures))
	}

	// Line number should be 0 when parsing fails
	if failures[0].LineNumber != 0 {
		t.Errorf("LineNumber should be 0 for malformed input, got %d", failures[0].LineNumber)
	}

	// Other fields should still be populated
	if failures[0].TestName != "TestBadLine" {
		t.Errorf("TestName: got %q, want %q", failures[0].TestName, "TestBadLine")
	}
	if failures[0].FilePath != "handlers_test.go" {
		t.Errorf("FilePath: got %q, want %q", failures[0].FilePath, "handlers_test.go")
	}
}

func TestParseTestFailures_MissingFileLine(t *testing.T) {
	// Test failure with no file:line information (should log warning but not fail)
	output := []byte(`=== RUN   TestNoFileInfo
--- FAIL: TestNoFileInfo (0.01s)
FAIL`)

	failures, err := ParseTestFailures(output)
	if err != nil {
		t.Fatalf("ParseTestFailures returned error: %v", err)
	}

	if len(failures) != 1 {
		t.Fatalf("Expected 1 failure, got %d", len(failures))
	}

	if failures[0].TestName != "TestNoFileInfo" {
		t.Errorf("TestName: got %q, want %q", failures[0].TestName, "TestNoFileInfo")
	}

	// Optional fields should be empty/zero
	if failures[0].FilePath != "" {
		t.Errorf("FilePath should be empty, got %q", failures[0].FilePath)
	}
	if failures[0].LineNumber != 0 {
		t.Errorf("LineNumber should be 0, got %d", failures[0].LineNumber)
	}
	if failures[0].ErrorMessage != "" {
		t.Errorf("ErrorMessage should be empty, got %q", failures[0].ErrorMessage)
	}
}

func TestParseTestFailures_IntermixedPassFail(t *testing.T) {
	// Test with PASS and FAIL lines intermixed
	output := []byte(`=== RUN   TestOne
--- PASS: TestOne (0.01s)
=== RUN   TestTwo
    handlers_test.go:56: TestTwo failed
--- FAIL: TestTwo (0.02s)
=== RUN   TestThree
--- PASS: TestThree (0.01s)
=== RUN   TestFour
    handlers_test.go:99: TestFour failed
--- FAIL: TestFour (0.03s)
FAIL`)

	failures, err := ParseTestFailures(output)
	if err != nil {
		t.Fatalf("ParseTestFailures returned error: %v", err)
	}

	if len(failures) != 2 {
		t.Fatalf("Expected 2 failures, got %d", len(failures))
	}

	// Verify only FAIL tests were captured
	if failures[0].TestName != "TestTwo" {
		t.Errorf("First failure: got %q, want %q", failures[0].TestName, "TestTwo")
	}
	if failures[1].TestName != "TestFour" {
		t.Errorf("Second failure: got %q, want %q", failures[1].TestName, "TestFour")
	}
}

func TestParseTestFailures_IncompleteStackTrace(t *testing.T) {
	// Test with incomplete/truncated stack trace
	output := []byte(`=== RUN   TestIncompleteTrace
    handlers_test.go:111: incomplete trace
	goroutine 1 [running]:
	testing.(*common).Fail()
--- FAIL: TestIncompleteTrace (0.01s)
FAIL`)

	failures, err := ParseTestFailures(output)
	if err != nil {
		t.Fatalf("ParseTestFailures returned error: %v", err)
	}

	if len(failures) != 1 {
		t.Fatalf("Expected 1 failure, got %d", len(failures))
	}

	// Should capture what trace exists
	if failures[0].StackTrace == "" {
		t.Error("StackTrace should capture partial trace")
	}
	if !strings.Contains(failures[0].StackTrace, "goroutine") {
		t.Error("StackTrace should contain 'goroutine' even if incomplete")
	}
}

func TestParseTestFailures_ThreeFailuresInSequence(t *testing.T) {
	// Test handling three failures in sequence
	output := []byte(`=== RUN   TestFirstFailure
    handlers_test.go:10: first error
--- FAIL: TestFirstFailure (0.01s)
=== RUN   TestSecondFailure
    handlers_test.go:20: second error
--- FAIL: TestSecondFailure (0.02s)
=== RUN   TestThirdFailure
    handlers_test.go:30: third error
--- FAIL: TestThirdFailure (0.01s)
FAIL`)

	failures, err := ParseTestFailures(output)
	if err != nil {
		t.Fatalf("ParseTestFailures returned error: %v", err)
	}

	if len(failures) != 3 {
		t.Fatalf("Expected 3 failures, got %d", len(failures))
	}

	// Verify all three failures were captured in order
	tests := []struct {
		name      string
		line      int
		message   string
	}{
		{"TestFirstFailure", 10, "first error"},
		{"TestSecondFailure", 20, "second error"},
		{"TestThirdFailure", 30, "third error"},
	}

	for i, tt := range tests {
		if failures[i].TestName != tt.name {
			t.Errorf("Failure %d name: got %q, want %q", i, failures[i].TestName, tt.name)
		}
		if failures[i].LineNumber != tt.line {
			t.Errorf("Failure %d line: got %d, want %d", i, failures[i].LineNumber, tt.line)
		}
		if failures[i].ErrorMessage != tt.message {
			t.Errorf("Failure %d message: got %q, want %q", i, failures[i].ErrorMessage, tt.message)
		}
	}
}

func TestParseTestFailures_VerifyWarningLogsForMalformed(t *testing.T) {
	// This test verifies that warnings are logged for malformed input
	// Since we can't capture log output directly, we just ensure parsing doesn't fail
	testCases := []struct {
		name  string
		input []byte
	}{
		{
			name: "non_numeric_line_number",
			input: []byte(`=== RUN   TestBad
    file.go:xyz: error
--- FAIL: TestBad (0.01s)
FAIL`),
		},
		{
			name: "missing_file_line",
			input: []byte(`=== RUN   TestMissing
--- FAIL: TestMissing (0.01s)
FAIL`),
		},
		{
			name: "empty_test_name_with_fail",
			input: []byte(`--- FAIL:  (0.01s)
FAIL`),
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			// Should not return error even with malformed input
			failures, err := ParseTestFailures(tt.input)
			if err != nil {
				t.Errorf("ParseTestFailures should not error for malformed input: %v", err)
			}
			// Should always return at least empty slice
			if failures == nil {
				t.Error("ParseTestFailures should return empty slice, not nil")
			}
		})
	}
}

func TestParseTestFailures_NilInput(t *testing.T) {
	// Test with nil input
	failures, err := ParseTestFailures(nil)
	if err != nil {
		t.Fatalf("ParseTestFailures returned error for nil input: %v", err)
	}

	if len(failures) != 0 {
		t.Fatalf("Expected 0 failures for nil input, got %d", len(failures))
	}
}

func TestParseTestFailures_VeryLongLine(t *testing.T) {
	// Test with very long error message (tests buffer expansion)
	longMessage := strings.Repeat("error text ", 1000) // ~12KB message
	output := []byte(fmt.Sprintf(`=== RUN   TestLongMessage
    handlers_test.go:42: %s
--- FAIL: TestLongMessage (0.05s)
FAIL`, longMessage))

	failures, err := ParseTestFailures(output)
	if err != nil {
		t.Fatalf("ParseTestFailures returned error: %v", err)
	}

	if len(failures) != 1 {
		t.Fatalf("Expected 1 failure, got %d", len(failures))
	}

	// Verify the long message was captured
	if !strings.Contains(failures[0].ErrorMessage, "error text") {
		t.Error("ErrorMessage should contain part of long message")
	}
}

func TestParseTestFailures_SubtestNames(t *testing.T) {
	// Test with subtest names (contain /)
	output := []byte(`=== RUN   TestParent/subtest_a
    handlers_test.go:88: subtest failed
--- FAIL: TestParent/subtest_a (0.01s)
FAIL`)

	failures, err := ParseTestFailures(output)
	if err != nil {
		t.Fatalf("ParseTestFailures returned error: %v", err)
	}

	if len(failures) != 1 {
		t.Fatalf("Expected 1 failure, got %d", len(failures))
	}

	if failures[0].TestName != "TestParent/subtest_a" {
		t.Errorf("TestName: got %q, want %q", failures[0].TestName, "TestParent/subtest_a")
	}
}

func TestParseTestFailures_ComprehensiveEdgeCases(t *testing.T) {
	// Test multiple edge cases in a single output:
	// - Parallel tests with PAUSE/CONT
	// - Multiple failures in sequence
	// - t.Helper stack traces
	// - Malformed line numbers
	// - Missing file:line info
	// - Intermixed PASS/FAIL
	output := []byte(`=== RUN   TestParallelOne
=== RUN   TestParallelTwo
=== PAUSE TestParallelOne
=== PAUSE TestParallelTwo
=== CONT  TestParallelTwo
	    handlers_test.go:23: assertion failed in parallel test two
	goroutine 1 [running]:
	testing.(*common).Fail()
		/usr/local/go/src/testing/testing.go:522
--- FAIL: TestParallelTwo (0.05s)
=== CONT  TestParallelOne
--- PASS: TestParallelOne (0.03s)
=== RUN   TestMalformed
	    handlers_test.go:abc: malformed line number
--- FAIL: TestMalformed (0.02s)
=== RUN   TestNoFileInfo
--- FAIL: TestNoFileInfo (0.01s)
=== RUN   TestHelper
	    helpers_test.go:78: TestHelper failed
	goroutine 1 [running]:
	testing.(*common).Fail()
		/usr/local/go/src/testing/testing.go:522
	github.com/user/pkg.TestHelper.func1()
		/home/user/pkg/helpers_test.go:45
	created by testing.tRunner
		/usr/local/go/src/testing/testing.go:689
--- FAIL: TestHelper (0.01s)
FAIL`)

	failures, err := ParseTestFailures(output)
	if err != nil {
		t.Fatalf("ParseTestFailures returned error: %v", err)
	}

	if len(failures) != 4 {
		t.Fatalf("Expected 4 failures, got %d", len(failures))
	}

	// Verify TestParallelTwo (with stack trace)
	if failures[0].TestName != "TestParallelTwo" {
		t.Errorf("First failure TestName: got %q, want %q", failures[0].TestName, "TestParallelTwo")
	}
	if failures[0].StackTrace == "" {
		t.Error("TestParallelTwo should have stack trace")
	}

	// Verify TestMalformed (malformed line number)
	if failures[1].TestName != "TestMalformed" {
		t.Errorf("Second failure TestName: got %q, want %q", failures[1].TestName, "TestMalformed")
	}
	if failures[1].LineNumber != 0 {
		t.Errorf("TestMalformed LineNumber should be 0 for malformed input, got %d", failures[1].LineNumber)
	}

	// Verify TestNoFileInfo (no file:line info)
	if failures[2].TestName != "TestNoFileInfo" {
		t.Errorf("Third failure TestName: got %q, want %q", failures[2].TestName, "TestNoFileInfo")
	}
	if failures[2].FilePath != "" {
		t.Errorf("TestNoFileInfo FilePath should be empty, got %q", failures[2].FilePath)
	}

		// Verify TestHelper (with t.Helper stack trace)
		if failures[3].TestName != "TestHelper" {
			t.Errorf("Fourth failure TestName: got %q, want %q", failures[3].TestName, "TestHelper")
		}
		if failures[3].StackTrace == "" {
			t.Error("TestHelper should have stack trace")
		}
		if !strings.Contains(failures[3].StackTrace, "testing.tRunner") {
			t.Error("TestHelper stack trace should contain 'testing.tRunner'")
		}
}

func TestParseTestFailures_AllEdgeCasesCombined(t *testing.T) {
	// Comprehensive test of all edge cases working together
	output := []byte(`=== RUN   TestOne
--- PASS: TestOne (0.01s)
=== RUN   TestTwo
	    file.go:10: error two
--- FAIL: TestTwo (0.02s)
=== RUN   TestThree
	    file.go:abc: malformed line
	goroutine 1 [running]:
	testing.(*common).Fail()
		/usr/local/go/src/testing/testing.go:522
--- FAIL: TestThree (0.01s)
=== RUN   TestFour
--- FAIL: TestFour (0.01s)
FAIL`)

	failures, err := ParseTestFailures(output)
	if err != nil {
		t.Fatalf("ParseTestFailures returned error: %v", err)
	}

	// Should have 3 failures (TestTwo, TestThree, TestFour)
	if len(failures) != 3 {
		t.Fatalf("Expected 3 failures, got %d", len(failures))
	}

	// TestTwo: normal failure with file:line
	if failures[0].TestName != "TestTwo" {
		t.Errorf("TestTwo name: got %q, want TestTwo", failures[0].TestName)
	}
	if failures[0].LineNumber != 10 {
		t.Errorf("TestTwo line: got %d, want 10", failures[0].LineNumber)
	}

	// TestThree: malformed line number with stack trace
	if failures[1].TestName != "TestThree" {
		t.Errorf("TestThree name: got %q, want TestThree", failures[1].TestName)
	}
	if failures[1].LineNumber != 0 {
		t.Errorf("TestThree line should be 0 for malformed, got %d", failures[1].LineNumber)
	}
	if failures[1].StackTrace == "" {
		t.Error("TestThree should have stack trace")
	}

	// TestFour: no file:line info
	if failures[2].TestName != "TestFour" {
		t.Errorf("TestFour name: got %q, want TestFour", failures[2].TestName)
	}
	if failures[2].FilePath != "" {
		t.Errorf("TestFour file should be empty, got %q", failures[2].FilePath)
	}
}

func TestExportFailuresJSON_Success(t *testing.T) {
	// Create valid test failures
	failures := []TestFailure{
		{
			TestName:    "TestExampleFunction",
			FilePath:    "proxy/handlers_test.go",
			LineNumber:  42,
			ErrorMessage: "expected 200, got 500",
			StackTrace: "goroutine 1 [running]:\ntestExampleFunction()\n\t/proxy/handlers_test.go:42",
		},
		{
			TestName:    "TestAnotherFunction",
			FilePath:    "proxy/ratelimiter_test.go",
			LineNumber:  15,
			ErrorMessage: "rate limit exceeded",
		},
	}

	// Create temp file for output
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "failures.json")

	// Export to JSON
	err := ExportFailuresJSON(failures, outputPath)
	if err != nil {
		t.Fatalf("ExportFailuresJSON returned error: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Fatal("Output file was not created")
	}

	// Read and verify JSON content
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	// Verify it's valid JSON
	var imported []TestFailure
	if err := json.Unmarshal(data, &imported); err != nil {
		t.Fatalf("Output is not valid JSON: %v", err)
	}

	// Verify all failures were exported
	if len(imported) != len(failures) {
		t.Errorf("Expected %d failures, got %d", len(failures), len(imported))
	}

	// Verify first failure matches
	if imported[0].TestName != failures[0].TestName {
		t.Errorf("TestName mismatch: got %q, want %q", imported[0].TestName, failures[0].TestName)
	}
	if imported[0].FilePath != failures[0].FilePath {
		t.Errorf("FilePath mismatch: got %q, want %q", imported[0].FilePath, failures[0].FilePath)
	}
	if imported[0].ErrorMessage != failures[0].ErrorMessage {
		t.Errorf("ErrorMessage mismatch: got %q, want %q", imported[0].ErrorMessage, failures[0].ErrorMessage)
	}

	// Verify JSON is formatted (indented) for readability
	jsonStr := string(data)
	if !strings.Contains(jsonStr, "  ") {
		t.Error("JSON should be indented for readability")
	}
	if !strings.Contains(jsonStr, "\n") {
		t.Error("JSON should contain newlines for readability")
	}
}

func TestExportFailuresJSON_MissingTestName(t *testing.T) {
	// Create failure with missing TestName
	failures := []TestFailure{
		{
			TestName:    "", // Missing required field
			FilePath:    "proxy/handlers_test.go",
			LineNumber:  42,
			ErrorMessage: "expected 200, got 500",
		},
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "failures.json")

	err := ExportFailuresJSON(failures, outputPath)
	if err == nil {
		t.Fatal("Expected validation error for missing TestName, got nil")
	}

	if !strings.Contains(err.Error(), "missing required field TestName") {
		t.Errorf("Error message should mention missing TestName, got: %v", err)
	}

	// Verify no file was created
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Error("Output file should not be created when validation fails")
	}
}

func TestExportFailuresJSON_MissingFilePath(t *testing.T) {
	// Create failure with missing FilePath
	failures := []TestFailure{
		{
			TestName:    "TestExampleFunction",
			FilePath:    "", // Missing required field
			LineNumber:  42,
			ErrorMessage: "expected 200, got 500",
		},
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "failures.json")

	err := ExportFailuresJSON(failures, outputPath)
	if err == nil {
		t.Fatal("Expected validation error for missing FilePath, got nil")
	}

	if !strings.Contains(err.Error(), "missing required field FilePath") {
		t.Errorf("Error message should mention missing FilePath, got: %v", err)
	}

	// Verify no file was created
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Error("Output file should not be created when validation fails")
	}
}

func TestExportFailuresJSON_MissingErrorMessage(t *testing.T) {
	// Create failure with missing ErrorMessage
	failures := []TestFailure{
		{
			TestName:    "TestExampleFunction",
			FilePath:    "proxy/handlers_test.go",
			LineNumber:  42,
			ErrorMessage: "", // Missing required field
		},
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "failures.json")

	err := ExportFailuresJSON(failures, outputPath)
	if err == nil {
		t.Fatal("Expected validation error for missing ErrorMessage, got nil")
	}

	if !strings.Contains(err.Error(), "missing required field ErrorMessage") {
		t.Errorf("Error message should mention missing ErrorMessage, got: %v", err)
	}

	// Verify no file was created
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Error("Output file should not be created when validation fails")
	}
}

func TestExportFailuresJSON_EmptyFailures(t *testing.T) {
	// Export empty failures slice (should succeed)
	failures := []TestFailure{}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "empty_failures.json")

	err := ExportFailuresJSON(failures, outputPath)
	if err != nil {
		t.Fatalf("ExportFailuresJSON with empty slice should succeed, got error: %v", err)
	}

	// Verify file was created
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	// Verify it's valid empty JSON array
	var imported []TestFailure
	if err := json.Unmarshal(data, &imported); err != nil {
		t.Fatalf("Output is not valid JSON: %v", err)
	}

	if len(imported) != 0 {
		t.Errorf("Expected 0 failures, got %d", len(imported))
	}

	// Verify JSON contains empty array
	jsonStr := string(data)
	if jsonStr != "[]" {
		t.Errorf("Expected empty JSON array '[]', got: %s", jsonStr)
	}
}

func TestExportFailuresJSON_MultipleValidationErrors(t *testing.T) {
	// Create multiple failures with different validation errors
	failures := []TestFailure{
		{
			TestName:    "TestValidOne",
			FilePath:    "proxy/test_one.go",
			LineNumber:  10,
			ErrorMessage: "error one",
		},
		{
			TestName:    "", // Missing TestName - should fail here
			FilePath:    "proxy/test_two.go",
			LineNumber:  20,
			ErrorMessage: "error two",
		},
		{
			TestName:    "TestValidThree",
			FilePath:    "",
			LineNumber:  30,
			ErrorMessage: "error three",
		},
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "failures.json")

	err := ExportFailuresJSON(failures, outputPath)
	if err == nil {
		t.Fatal("Expected validation error, got nil")
	}

	// Should fail on the first failure (index 1) with missing TestName
	if !strings.Contains(err.Error(), "index 1") {
		t.Errorf("Error should mention index 1, got: %v", err)
	}
	if !strings.Contains(err.Error(), "TestName") {
		t.Errorf("Error should mention TestName, got: %v", err)
	}

	// Verify no file was created
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Error("Output file should not be created when validation fails")
	}
}

func TestExportFailuresJSON_WithoutStackTrace(t *testing.T) {
	// Test that optional StackTrace field is not required
	failures := []TestFailure{
		{
			TestName:    "TestNoStackTrace",
			FilePath:    "proxy/simple_test.go",
			LineNumber:  10,
			ErrorMessage: "assertion failed",
			// StackTrace intentionally omitted
		},
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "no_trace.json")

	err := ExportFailuresJSON(failures, outputPath)
	if err != nil {
		t.Fatalf("ExportFailuresJSON should succeed without StackTrace, got error: %v", err)
	}

	// Read and verify
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	var imported []TestFailure
	if err := json.Unmarshal(data, &imported); err != nil {
		t.Fatalf("Output is not valid JSON: %v", err)
	}

	if len(imported) != 1 {
		t.Fatalf("Expected 1 failure, got %d", len(imported))
	}

	if imported[0].StackTrace != "" {
		t.Errorf("StackTrace should be empty when not provided, got: %q", imported[0].StackTrace)
	}
}

func TestValidateFailures_SeparateValidation(t *testing.T) {
	// Test the separate ValidateFailures function
	validFailures := []TestFailure{
		{
			TestName:    "TestValid",
			FilePath:    "proxy/valid_test.go",
			LineNumber:  10,
			ErrorMessage: "valid error",
		},
	}

	err := ValidateFailures(validFailures)
	if err != nil {
		t.Errorf("ValidateFailures should succeed for valid failures, got: %v", err)
	}

	// Test invalid failures
	invalidFailures := []TestFailure{
		{
			TestName:    "", // Missing
			FilePath:    "proxy/invalid_test.go",
			LineNumber:  10,
			ErrorMessage: "invalid error",
		},
	}

	err = ValidateFailures(invalidFailures)
	if err == nil {
		t.Fatal("ValidateFailures should return error for invalid failures")
	}

	if !strings.Contains(err.Error(), "TestName") {
		t.Errorf("Error should mention TestName, got: %v", err)
	}
}

func TestExportFailuresJSON_RoundTrip(t *testing.T) {
	// Test complete round-trip: parse → export → import → verify
	// Using same format as TestParseTestFailures_MultipleFailures
	originalOutput := []byte(`=== RUN   TestRateLimit
	    ratelimiter_test.go:45: assertion failed: expected 200, got 500
--- FAIL: TestRateLimit (0.05s)
=== RUN   TestTokenBucket
	    ratelimiter_test.go:78: bucket not empty after reset
--- FAIL: TestTokenBucket (0.02s)
FAIL`)

	// Parse failures
	failures, err := ParseTestFailures(originalOutput)
	if err != nil {
		t.Fatalf("ParseTestFailures failed: %v", err)
	}

	if len(failures) != 2 {
		t.Fatalf("Expected 2 failures, got %d", len(failures))
	}

	// Export to JSON
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "roundtrip.json")
	err = ExportFailuresJSON(failures, outputPath)
	if err != nil {
		t.Fatalf("ExportFailuresJSON failed: %v", err)
	}

	// Import from JSON
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read exported file: %v", err)
	}

	var imported []TestFailure
	if err := json.Unmarshal(data, &imported); err != nil {
		t.Fatalf("Failed to import JSON: %v", err)
	}

	// Verify round-trip integrity
	if len(imported) != len(failures) {
		t.Fatalf("Round-trip failed: expected %d failures, got %d", len(failures), len(imported))
	}

	for i := range failures {
		if imported[i].TestName != failures[i].TestName {
			t.Errorf("Round-trip TestName mismatch at index %d: got %q, want %q",
				i, imported[i].TestName, failures[i].TestName)
		}
		if imported[i].FilePath != failures[i].FilePath {
			t.Errorf("Round-trip FilePath mismatch at index %d: got %q, want %q",
				i, imported[i].FilePath, failures[i].FilePath)
		}
		if imported[i].LineNumber != failures[i].LineNumber {
			t.Errorf("Round-trip LineNumber mismatch at index %d: got %d, want %d",
				i, imported[i].LineNumber, failures[i].LineNumber)
		}
		if imported[i].ErrorMessage != failures[i].ErrorMessage {
			t.Errorf("Round-trip ErrorMessage mismatch at index %d: got %q, want %q",
				i, imported[i].ErrorMessage, failures[i].ErrorMessage)
		}
		if imported[i].StackTrace != failures[i].StackTrace {
			t.Errorf("Round-trip StackTrace mismatch at index %d: got %q, want %q",
				i, imported[i].StackTrace, failures[i].StackTrace)
		}
	}
}

func TestExportFailuresJSON_FileWriteError(t *testing.T) {
	// Test error handling when file write fails
	failures := []TestFailure{
		{
			TestName:    "TestFunction",
			FilePath:    "proxy/test.go",
			LineNumber:  10,
			ErrorMessage: "test error",
		},
	}

	// Try to write to an invalid path (directory that doesn't exist)
	invalidPath := "/nonexistent/directory/subdir/failures.json"

	err := ExportFailuresJSON(failures, invalidPath)
	if err == nil {
		t.Fatal("Expected error when writing to nonexistent directory, got nil")
	}

	if !strings.Contains(err.Error(), "failed to write") {
		t.Errorf("Error message should mention write failure, got: %v", err)
	}
}
