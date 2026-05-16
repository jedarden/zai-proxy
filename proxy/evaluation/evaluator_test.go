package evaluation

import (
	"encoding/json"
	"testing"
)

// TestGetTestCases verifies test cases are properly defined
func TestGetTestCases(t *testing.T) {
	tests := GetTestCases()

	if len(tests) < 10 {
		t.Errorf("Expected at least 10 test cases, got %d", len(tests))
	}

	for i, tc := range tests {
		if tc.Name == "" {
			t.Errorf("Test case %d: missing name", i)
		}
		if tc.Request.Model == "" {
			t.Errorf("Test case %d (%s): missing model", i, tc.Name)
		}
		if len(tc.Request.Messages) == 0 {
			t.Errorf("Test case %d (%s): no messages", i, tc.Name)
		}
		if tc.Request.MaxTokens <= 0 {
			t.Errorf("Test case %d (%s): invalid max_tokens", i, tc.Name)
		}
	}
}

// TestEvaluatorCreation tests evaluator initialization
func TestEvaluatorCreation(t *testing.T) {
	e := NewEvaluator("http://localhost:8080", "https://api.anthropic.com", "test-key-1", "test-key-2")

	if e == nil {
		t.Fatal("NewEvaluator returned nil")
	}

	if e.ZaiEndpoint != "http://localhost:8080" {
		t.Errorf("Expected ZaiEndpoint 'http://localhost:8080', got '%s'", e.ZaiEndpoint)
	}

	if e.ZaiAPIKey != "test-key-1" {
		t.Errorf("Expected ZaiAPIKey 'test-key-1', got '%s'", e.ZaiAPIKey)
	}

	if e.Client == nil {
		t.Error("Client is nil")
	}
}

// TestExtractJSONTokenUsage tests token extraction from JSON responses
func TestExtractJSONTokenUsage(t *testing.T) {
	e := NewEvaluator("", "", "", "")

	tests := []struct {
		name         string
		body         string
		expectInput  int
		expectOutput int
		expectNil    bool
	}{
		{
			name: "Valid response with usage",
			body: `{"id":"msg_123","type":"message","usage":{"input_tokens":100,"output_tokens":50}}`,
			expectInput:  100,
			expectOutput: 50,
			expectNil:    false,
		},
		{
			name: "Response with zero tokens",
			body: `{"id":"msg_123","usage":{"input_tokens":0,"output_tokens":0}}`,
			expectInput:  0,
			expectOutput: 0,
			expectNil:    false,
		},
		{
			name:      "Response without usage",
			body:      `{"id":"msg_123","type":"message"}`,
			expectNil: true,
		},
		{
			name:      "Invalid JSON",
			body:      `{invalid json}`,
			expectNil: true,
		},
		{
			name:      "Empty body",
			body:      ``,
			expectNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := e.extractJSONTokenUsage([]byte(tc.body))

			if tc.expectNil {
				if result != nil {
					t.Errorf("Expected nil result, got %+v", result)
				}
			} else {
				if result == nil {
					t.Fatal("Expected non-nil result, got nil")
				}
				if result.InputTokens != tc.expectInput {
					t.Errorf("InputTokens: got %d, want %d", result.InputTokens, tc.expectInput)
				}
				if result.OutputTokens != tc.expectOutput {
					t.Errorf("OutputTokens: got %d, want %d", result.OutputTokens, tc.expectOutput)
				}
			}
		})
	}
}

// TestExtractSSETokenUsage tests token extraction from SSE responses
func TestExtractSSETokenUsage(t *testing.T) {
	e := NewEvaluator("", "", "", "")

	tests := []struct {
		name         string
		body         string
		expectInput  int
		expectOutput int
		expectNil    bool
	}{
		{
			name: "Valid SSE with usage in message_delta",
			body: `data: {"type":"message_start"}
data: {"type":"content_block_delta","delta":{"text":"Hello"}}
data: {"type":"message_delta","usage":{"input_tokens":10,"output_tokens":20}}
data: {"type":"message_stop"}`,
			expectInput:  10,
			expectOutput: 20,
			expectNil:    false,
		},
		{
			name: "SSE without usage",
			body: `data: {"type":"message_start"}
data: {"type":"message_stop"}`,
			expectNil: true,
		},
		{
			name:      "Empty SSE",
			body:      ``,
			expectNil: true,
		},
		{
			name: "SSE with [DONE]",
			body: `data: {"type":"content_block_delta","delta":{"text":"Hi"}}
data: [DONE]
data: {"type":"message_stop"}`,
			expectNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := e.extractSSETokenUsage([]byte(tc.body))

			if tc.expectNil {
				if result != nil {
					t.Errorf("Expected nil result, got %+v", result)
				}
			} else {
				if result == nil {
					t.Fatal("Expected non-nil result, got nil")
				}
				if result.InputTokens != tc.expectInput {
					t.Errorf("InputTokens: got %d, want %d", result.InputTokens, tc.expectInput)
				}
				if result.OutputTokens != tc.expectOutput {
					t.Errorf("OutputTokens: got %d, want %d", result.OutputTokens, tc.expectOutput)
				}
			}
		})
	}
}

// TestCompareResponseStructure tests structural comparison
func TestCompareResponseStructure(t *testing.T) {
	e := NewEvaluator("", "", "", "")

	tests := []struct {
		name     string
		zaiBody  string
		anthropicBody string
		expectMatch bool
	}{
		{
			name: "Identical structure",
			zaiBody:  `{"id":"msg_123","type":"message","content":[],"role":"assistant"}`,
			anthropicBody: `{"id":"msg_456","type":"message","content":[],"role":"assistant"}`,
			expectMatch: true,
		},
		{
			name:          "Different number of keys",
			zaiBody:       `{"id":"msg_123","type":"message"}`,
			anthropicBody: `{"id":"msg_456","type":"message","extra":"field"}`,
			expectMatch:   false,
		},
		{
			name:          "Different key names",
			zaiBody:       `{"id":"msg_123","type":"message"}`,
			anthropicBody: `{"id":"msg_456","content":"message"}`,
			expectMatch:   false,
		},
		{
			name:          "Invalid JSON in zai",
			zaiBody:       `{invalid}`,
			anthropicBody: `{"id":"msg_456"}`,
			expectMatch:   false,
		},
		{
			name:          "Invalid JSON in anthropic",
			zaiBody:       `{"id":"msg_123"}`,
			anthropicBody: `{invalid}`,
			expectMatch:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := e.compareResponseStructure([]byte(tc.zaiBody), []byte(tc.anthropicBody))
			if result != tc.expectMatch {
				t.Errorf("compareResponseStructure() = %v, want %v", result, tc.expectMatch)
			}
		})
	}
}

// TestCalculateMetrics tests metrics calculation
func TestCalculateMetrics(t *testing.T) {
	e := NewEvaluator("", "", "", "")

	results := []ComparisonResult{
		{
			TestName: "Test 1",
			ZaiResponse: ResponseData{
				TokenUsage: &TokenUsage{InputTokens: 100, OutputTokens: 50},
			},
			AnthropicResponse: ResponseData{
				TokenUsage: &TokenUsage{InputTokens: 105, OutputTokens: 52},
			},
			InputTokenDiff:       -5,
			OutputTokenDiff:      -2,
			InputTokenPercentDiff: -5.0,
			OutputTokenPercentDiff: -4.0,
			ResponseStructureMatch: true,
		},
		{
			TestName: "Test 2",
			ZaiResponse: ResponseData{
				TokenUsage: &TokenUsage{InputTokens: 200, OutputTokens: 100},
			},
			AnthropicResponse: ResponseData{
				TokenUsage: &TokenUsage{InputTokens: 190, OutputTokens: 95},
			},
			InputTokenDiff:       10,
			OutputTokenDiff:      5,
			InputTokenPercentDiff: 5.0,
			OutputTokenPercentDiff: 5.0,
			ResponseStructureMatch: true,
		},
		{
			TestName: "Test 3 (no token data)",
			ZaiResponse: ResponseData{},
			AnthropicResponse: ResponseData{},
			ResponseStructureMatch: false,
		},
	}

	metrics := e.calculateMetrics(results)

	if metrics.TotalTests != 3 {
		t.Errorf("TotalTests: got %d, want 3", metrics.TotalTests)
	}

	// First two tests have token data
	if metrics.InputTokenMAE != 7.5 { // (5 + 10) / 2
		t.Errorf("InputTokenMAE: got %.2f, want 7.5", metrics.InputTokenMAE)
	}

	if metrics.OutputTokenMAE != 3.5 { // (2 + 5) / 2
		t.Errorf("OutputTokenMAE: got %.2f, want 3.5", metrics.OutputTokenMAE)
	}

	if metrics.InputTokenAvgPercentDiff != 5.0 {
		t.Errorf("InputTokenAvgPercentDiff: got %.2f, want 5.0", metrics.InputTokenAvgPercentDiff)
	}

	if metrics.OutputTokenAvgPercentDiff != 4.5 {
		t.Errorf("OutputTokenAvgPercentDiff: got %.2f, want 4.5", metrics.OutputTokenAvgPercentDiff)
	}

	if metrics.StructureMatchCount != 2 {
		t.Errorf("StructureMatchCount: got %d, want 2", metrics.StructureMatchCount)
	}
}

// TestReportGeneration tests report generation
func TestReportGeneration(t *testing.T) {
	results := []ComparisonResult{
		{
			TestName: "Sample Test",
			ZaiResponse: ResponseData{
				TokenUsage: &TokenUsage{InputTokens: 100, OutputTokens: 50},
				Duration:   100000000, // 100ms
			},
			AnthropicResponse: ResponseData{
				TokenUsage: &TokenUsage{InputTokens: 100, OutputTokens: 50},
				Duration:   90000000, // 90ms
			},
			InputTokenMatch:       true,
			OutputTokenMatch:      true,
			InputTokenDiff:        0,
			OutputTokenDiff:       0,
			InputTokenPercentDiff: 0.0,
			OutputTokenPercentDiff: 0.0,
			ResponseStructureMatch: true,
		},
	}

	metrics := &EvaluationMetrics{
		TotalTests:             1,
		SuccessfulTests:        1,
		InputTokenMAE:          0.0,
		OutputTokenMAE:         0.0,
		InputTokenAvgPercentDiff: 0.0,
		OutputTokenAvgPercentDiff: 0.0,
		StructureMatchCount:    1,
	}

	reporter := NewReportGenerator(results, metrics)

	// Test text report generation
	textReport := reporter.GenerateTextReport()
	if textReport == "" {
		t.Error("GenerateTextReport() returned empty string")
	}

	if len(textReport) < 100 {
		t.Errorf("Text report too short: %d characters", len(textReport))
	}

	// Check for expected sections
	expectedSections := []string{
		"EXECUTIVE SUMMARY",
		"TOKEN ACCURACY METRICS",
		"DETAILED TEST RESULTS",
		"ANALYSIS AND RECOMMENDATIONS",
	}

	for _, section := range expectedSections {
		if !contains(textReport, section) {
			t.Errorf("Text report missing section: %s", section)
		}
	}

	// Test JSON report generation
	jsonReport, err := reporter.GenerateJSONReport()
	if err != nil {
		t.Errorf("GenerateJSONReport() error: %v", err)
	}

	if len(jsonReport) == 0 {
		t.Error("JSON report is empty")
	}

	var jsonData map[string]interface{}
	if err := json.Unmarshal(jsonReport, &jsonData); err != nil {
		t.Errorf("JSON report is invalid: %v", err)
	}

	// Verify required fields
	requiredFields := []string{"generated_at", "metrics", "test_results", "interpretation"}
	for _, field := range requiredFields {
		if _, ok := jsonData[field]; !ok {
			t.Errorf("JSON report missing field: %s", field)
		}
	}
}

// TestPatternAnalysis tests pattern identification
func TestPatternAnalysis(t *testing.T) {
	results := []ComparisonResult{
		{
			TestName: "Test 1",
			ZaiResponse: ResponseData{
				TokenUsage: &TokenUsage{InputTokens: 105, OutputTokens: 52},
			},
			AnthropicResponse: ResponseData{
				TokenUsage: &TokenUsage{InputTokens: 100, OutputTokens: 50},
			},
			InputTokenDiff:  5,
			OutputTokenDiff: 2,
		},
		{
			TestName: "Test 2",
			ZaiResponse: ResponseData{
				TokenUsage: &TokenUsage{InputTokens: 110, OutputTokens: 55},
			},
			AnthropicResponse: ResponseData{
				TokenUsage: &TokenUsage{InputTokens: 100, OutputTokens: 50},
			},
			InputTokenDiff:  10,
			OutputTokenDiff: 5,
		},
	}

	metrics := &EvaluationMetrics{
		TotalTests:          2,
		SuccessfulTests:     2,
		InputTokenMAE:       7.5,
		OutputTokenMAE:      3.5,
		StructureMatchCount: 2,
	}

	reporter := NewReportGenerator(results, metrics)
	patterns := reporter.identifyPatterns()

	// Should detect Z.AI consistently higher
	if !contains(patterns, "Z.AI consistently reports higher input tokens") {
		t.Error("Pattern analysis should detect Z.AI consistently higher for input tokens")
	}

	if !contains(patterns, "Z.AI consistently reports higher output tokens") {
		t.Error("Pattern analysis should detect Z.AI consistently higher for output tokens")
	}
}

// TestRecommendations tests recommendation generation
func TestRecommendations(t *testing.T) {
	tests := []struct {
		name             string
		metrics          *EvaluationMetrics
		expectRecommendation bool
		expectedKeyword  string
	}{
		{
			name: "High MAE should recommend tokenizer review",
			metrics: &EvaluationMetrics{
				TotalTests:       10,
				SuccessfulTests:  10,
				InputTokenMAE:    15.0,
				OutputTokenMAE:   2.0,
				StructureMatchCount: 10,
			},
			expectRecommendation: true,
			expectedKeyword:  "tokenizer",
		},
		{
			name: "Low success rate should recommend error handling review",
			metrics: &EvaluationMetrics{
				TotalTests:       10,
				SuccessfulTests:  8,
				InputTokenMAE:    2.0,
				OutputTokenMAE:   2.0,
				StructureMatchCount: 10,
			},
			expectRecommendation: true,
			expectedKeyword:  "failed",
		},
		{
			name: "Perfect metrics should recommend no action",
			metrics: &EvaluationMetrics{
				TotalTests:       10,
				SuccessfulTests:  10,
				InputTokenMAE:    0.0,
				OutputTokenMAE:   0.0,
				StructureMatchCount: 10,
			},
			expectRecommendation: true,
			expectedKeyword:  "no immediate action",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reporter := NewReportGenerator([]ComparisonResult{}, tc.metrics)
			recommendations := reporter.getRecommendations()

			if len(recommendations) == 0 {
				t.Error("Expected at least one recommendation")
			}

			found := false
			for _, rec := range recommendations {
				if contains(rec, tc.expectedKeyword) {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("Expected recommendation to contain '%s', got: %v", tc.expectedKeyword, recommendations)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && (s[0:len(substr)] == substr || contains(s[1:], substr))))
}
