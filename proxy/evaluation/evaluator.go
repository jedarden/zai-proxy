package evaluation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// TestRequest represents a test case for evaluation
type TestRequest struct {
	Name    string          `json:"name"`
	Request ClaudeRequest   `json:"request"`
	Stream  bool            `json:"stream"`
}

// ClaudeRequest represents an Anthropic API request
type ClaudeRequest struct {
	Model    string        `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	Messages []Message     `json:"messages"`
	Stream   bool          `json:"stream,omitempty"`
}

// Message represents a message in the conversation
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// TokenUsage represents token counts from responses
type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ResponseData captures response data from an endpoint
type ResponseData struct {
	StatusCode int
	Body       []byte
	Headers    http.Header
	Duration   time.Duration
	TokenUsage *TokenUsage
	Error      error
}

// ComparisonResult represents a comparison between two responses
type ComparisonResult struct {
	TestName             string
	ZaiResponse          ResponseData
	AnthropicResponse    ResponseData
	InputTokenMatch      bool
	OutputTokenMatch     bool
	InputTokenDiff       int
	OutputTokenDiff      int
	InputTokenPercentDiff float64
	OutputTokenPercentDiff float64
	ResponseStructureMatch bool
}

// EvaluationMetrics contains aggregated metrics
type EvaluationMetrics struct {
	TotalTests             int
	SuccessfulTests        int
	InputTokenMAE          float64
	OutputTokenMAE         float64
	InputTokenAvgPercentDiff float64
	OutputTokenAvgPercentDiff float64
	StructureMatchCount    int
}

// Evaluator manages the evaluation process
type Evaluator struct {
	ZaiEndpoint       string
	AnthropicEndpoint string
	ZaiAPIKey         string
	AnthropicAPIKey   string
	Client            *http.Client
}

// NewEvaluator creates a new evaluator instance
func NewEvaluator(zaiEndpoint, anthropicEndpoint, zaiKey, anthropicKey string) *Evaluator {
	return &Evaluator{
		ZaiEndpoint:       zaiEndpoint,
		AnthropicEndpoint: anthropicEndpoint,
		ZaiAPIKey:         zaiKey,
		AnthropicAPIKey:   anthropicKey,
		Client: &http.Client{
			Timeout: 2 * time.Minute,
		},
	}
}

// sendRequest sends a request to the specified endpoint
func (e *Evaluator) sendRequest(endpoint, apiKey string, req ClaudeRequest) ResponseData {
	start := time.Now()

	reqBody, err := json.Marshal(req)
	if err != nil {
		return ResponseData{Error: fmt.Errorf("failed to marshal request: %w", err)}
	}

	httpReq, err := http.NewRequest("POST", endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return ResponseData{Error: fmt.Errorf("failed to create request: %w", err)}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := e.Client.Do(httpReq)
	if err != nil {
		return ResponseData{Error: fmt.Errorf("request failed: %w", err)}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ResponseData{
			StatusCode: resp.StatusCode,
			Error:      fmt.Errorf("failed to read response body: %w", err),
		}
	}

	// Try to extract token usage from response
	tokenUsage := e.extractTokenUsage(body, req.Stream)

	return ResponseData{
		StatusCode: resp.StatusCode,
		Body:       body,
		Headers:    resp.Header,
		Duration:   time.Since(start),
		TokenUsage: tokenUsage,
	}
}

// extractTokenUsage attempts to extract token usage from response body
func (e *Evaluator) extractTokenUsage(body []byte, isStreaming bool) *TokenUsage {
	if isStreaming {
		return e.extractSSETokenUsage(body)
	}
	return e.extractJSONTokenUsage(body)
}

// extractJSONTokenUsage extracts usage from non-streaming JSON response
func (e *Evaluator) extractJSONTokenUsage(body []byte) *TokenUsage {
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		log.Printf("Warning: failed to parse JSON response: %v", err)
		return nil
	}

	usage, ok := resp["usage"].(map[string]interface{})
	if !ok {
		return nil
	}

	inputTokens, _ := usage["input_tokens"].(float64)
	outputTokens, _ := usage["output_tokens"].(float64)

	return &TokenUsage{
		InputTokens:  int(inputTokens),
		OutputTokens: int(outputTokens),
	}
}

// extractSSETokenUsage extracts usage from streaming SSE response
func (e *Evaluator) extractSSETokenUsage(body []byte) *TokenUsage {
	lines := bytes.Split(body, []byte("\n"))
	for _, line := range lines {
		if !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}

		jsonData := bytes.TrimPrefix(line, []byte("data: "))
		if len(jsonData) == 0 || bytes.Equal(jsonData, []byte("[DONE]")) {
			continue
		}

		var event map[string]interface{}
		if err := json.Unmarshal(jsonData, &event); err != nil {
			continue
		}

		if eventType, ok := event["type"].(string); ok && eventType == "message_delta" {
			if usage, ok := event["usage"].(map[string]interface{}); ok {
				inputTokens, _ := usage["input_tokens"].(float64)
				outputTokens, _ := usage["output_tokens"].(float64)
				return &TokenUsage{
					InputTokens:  int(inputTokens),
					OutputTokens: int(outputTokens),
				}
			}
		}
	}

	return nil
}

// RunTest executes a single test case, comparing responses from both endpoints
func (e *Evaluator) RunTest(test TestRequest) ComparisonResult {
	log.Printf("Running test: %s", test.Name)

	// Send requests concurrently
	var wg sync.WaitGroup
	var zaiResp, anthropicResp ResponseData

	wg.Add(2)
	go func() {
		defer wg.Done()
		zaiResp = e.sendRequest(e.ZaiEndpoint, e.ZaiAPIKey, test.Request)
	}()
	go func() {
		defer wg.Done()
		anthropicResp = e.sendRequest(e.AnthropicEndpoint, e.AnthropicAPIKey, test.Request)
	}()
	wg.Wait()

	result := ComparisonResult{
		TestName:          test.Name,
		ZaiResponse:       zaiResp,
		AnthropicResponse: anthropicResp,
	}

	// Compare token counts if both responses have usage data
	if zaiResp.TokenUsage != nil && anthropicResp.TokenUsage != nil {
		result.InputTokenMatch = zaiResp.TokenUsage.InputTokens == anthropicResp.TokenUsage.InputTokens
		result.OutputTokenMatch = zaiResp.TokenUsage.OutputTokens == anthropicResp.TokenUsage.OutputTokens
		result.InputTokenDiff = zaiResp.TokenUsage.InputTokens - anthropicResp.TokenUsage.InputTokens
		result.OutputTokenDiff = zaiResp.TokenUsage.OutputTokens - anthropicResp.TokenUsage.OutputTokens

		if anthropicResp.TokenUsage.InputTokens > 0 {
			result.InputTokenPercentDiff = float64(result.InputTokenDiff) / float64(anthropicResp.TokenUsage.InputTokens) * 100
		}
		if anthropicResp.TokenUsage.OutputTokens > 0 {
			result.OutputTokenPercentDiff = float64(result.OutputTokenDiff) / float64(anthropicResp.TokenUsage.OutputTokens) * 100
		}
	}

	// Compare response structure (basic check)
	result.ResponseStructureMatch = e.compareResponseStructure(zaiResp.Body, anthropicResp.Body)

	return result
}

// compareResponseStructure performs basic structural comparison
func (e *Evaluator) compareResponseStructure(zaiBody, anthropicBody []byte) bool {
	var zaiMap, anthropicMap map[string]interface{}

	if err := json.Unmarshal(zaiBody, &zaiMap); err != nil {
		return false
	}
	if err := json.Unmarshal(anthropicBody, &anthropicMap); err != nil {
		return false
	}

	// Compare top-level keys
	if len(zaiMap) != len(anthropicMap) {
		return false
	}

	for key := range zaiMap {
		if _, ok := anthropicMap[key]; !ok {
			return false
		}
	}

	return true
}

// RunTests executes multiple test cases and returns aggregated metrics
func (e *Evaluator) RunTests(tests []TestRequest) ([]ComparisonResult, *EvaluationMetrics) {
	results := make([]ComparisonResult, len(tests))

	for i, test := range tests {
		results[i] = e.RunTest(test)
	}

	metrics := e.calculateMetrics(results)

	return results, metrics
}

// calculateMetrics computes aggregated metrics from comparison results
func (e *Evaluator) calculateMetrics(results []ComparisonResult) *EvaluationMetrics {
	return e.CalculateMetricsFromResults(results)
}

// CalculateMetricsFromResults computes aggregated metrics from comparison results (public method)
func (e *Evaluator) CalculateMetricsFromResults(results []ComparisonResult) *EvaluationMetrics {
	metrics := &EvaluationMetrics{
		TotalTests:       len(results),
		StructureMatchCount: 0,
	}

	var inputTokenSum, outputTokenSum float64
	var inputPercentSum, outputPercentSum float64
	var validInputCount, validOutputCount int

	for _, result := range results {
		if result.ZaiResponse.Error == nil && result.AnthropicResponse.Error == nil {
			metrics.SuccessfulTests++
		}

		if result.ResponseStructureMatch {
			metrics.StructureMatchCount++
		}

		// Calculate MAE and average percentage differences
		if result.AnthropicResponse.TokenUsage != nil && result.ZaiResponse.TokenUsage != nil {
			inputTokenSum += absFloat(float64(result.InputTokenDiff))
			outputTokenSum += absFloat(float64(result.OutputTokenDiff))

			if result.AnthropicResponse.TokenUsage.InputTokens > 0 {
				inputPercentSum += absFloat(result.InputTokenPercentDiff)
				validInputCount++
			}
			if result.AnthropicResponse.TokenUsage.OutputTokens > 0 {
				outputPercentSum += absFloat(result.OutputTokenPercentDiff)
				validOutputCount++
			}
		}
	}

	if validInputCount > 0 {
		metrics.InputTokenMAE = inputTokenSum / float64(validInputCount)
		metrics.InputTokenAvgPercentDiff = inputPercentSum / float64(validInputCount)
	}
	if validOutputCount > 0 {
		metrics.OutputTokenMAE = outputTokenSum / float64(validOutputCount)
		metrics.OutputTokenAvgPercentDiff = outputPercentSum / float64(validOutputCount)
	}

	return metrics
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
