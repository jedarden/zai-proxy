package evaluation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"text/template"
	"time"
)

// ReportGenerator creates evaluation reports
type ReportGenerator struct {
	results []ComparisonResult
	metrics *EvaluationMetrics
}

// NewReportGenerator creates a new report generator
func NewReportGenerator(results []ComparisonResult, metrics *EvaluationMetrics) *ReportGenerator {
	return &ReportGenerator{
		results: results,
		metrics: metrics,
	}
}

// GenerateTextReport creates a text-based evaluation report
func (rg *ReportGenerator) GenerateTextReport() string {
	var buf bytes.Buffer

	buf.WriteString("╔══════════════════════════════════════════════════════════════════════════════╗\n")
	buf.WriteString("║                    Z.AI PROXY EVALUATION REPORT                                ║\n")
	buf.WriteString(fmt.Sprintf("║                    Generated: %s                                        ║\n", time.Now().Format("2006-01-02 15:04:05")))
	buf.WriteString("╚══════════════════════════════════════════════════════════════════════════════╝\n\n")

	// Executive Summary
	buf.WriteString("## EXECUTIVE SUMMARY\n\n")
	buf.WriteString(fmt.Sprintf("Total Tests Run:        %d\n", rg.metrics.TotalTests))
	buf.WriteString(fmt.Sprintf("Successful Tests:       %d (%.1f%%)\n", rg.metrics.SuccessfulTests, float64(rg.metrics.SuccessfulTests)/float64(rg.metrics.TotalTests)*100))
	buf.WriteString(fmt.Sprintf("Structure Match Rate:   %d (%.1f%%)\n", rg.metrics.StructureMatchCount, float64(rg.metrics.StructureMatchCount)/float64(rg.metrics.TotalTests)*100))
	buf.WriteString("\n")

	// Token Accuracy Metrics
	buf.WriteString("## TOKEN ACCURACY METRICS\n\n")
	buf.WriteString("┌────────────────────────┬──────────────┬──────────────┬──────────────────┐\n")
	buf.WriteString("│ Metric                 │ Input Tokens │ Output Tokens │ Difference       │\n")
	buf.WriteString("├────────────────────────┼──────────────┼──────────────┼──────────────────┤\n")

	if rg.metrics.InputTokenMAE > 0 {
		buf.WriteString(fmt.Sprintf("│ Mean Absolute Error   │ %12.2f │ %12.2f │                  │\n", rg.metrics.InputTokenMAE, rg.metrics.OutputTokenMAE))
	}
	if rg.metrics.InputTokenAvgPercentDiff > 0 {
		buf.WriteString(fmt.Sprintf("│ Avg Percent Diff      │ %11.2f%% │ %11.2f%% │                  │\n", rg.metrics.InputTokenAvgPercentDiff, rg.metrics.OutputTokenAvgPercentDiff))
	}

	buf.WriteString("└────────────────────────┴──────────────┴──────────────┴──────────────────┘\n\n")

	// Detailed Test Results
	buf.WriteString("## DETAILED TEST RESULTS\n\n")

	for i, result := range rg.results {
		buf.WriteString(fmt.Sprintf("### Test %d: %s\n\n", i+1, result.TestName))

		// Status
		zaiStatus := "✓ OK"
		if result.ZaiResponse.Error != nil {
			zaiStatus = fmt.Sprintf("✗ Error: %s", result.ZaiResponse.Error)
		}
		anthropicStatus := "✓ OK"
		if result.AnthropicResponse.Error != nil {
			anthropicStatus = fmt.Sprintf("✗ Error: %s", result.AnthropicResponse.Error)
		}

		buf.WriteString(fmt.Sprintf("Z.AI Status:       %s\n", zaiStatus))
		buf.WriteString(fmt.Sprintf("Anthropic Status: %s\n", anthropicStatus))

		// Response times
		buf.WriteString(fmt.Sprintf("Z.AI Response:     %v\n", result.ZaiResponse.Duration))
		buf.WriteString(fmt.Sprintf("Anthropic Response: %v\n", result.AnthropicResponse.Duration))

		// Token comparison
		if result.ZaiResponse.TokenUsage != nil && result.AnthropicResponse.TokenUsage != nil {
			buf.WriteString("\nToken Comparison:\n")
			buf.WriteString("┌─────────────────────┬──────────┬──────────┬──────────┬────────────┐\n")
			buf.WriteString("│ Direction            │ Z.AI     │ Anthropic│ Diff     │ %% Diff     │\n")
			buf.WriteString("├─────────────────────┼──────────┼──────────┼──────────┼────────────┤\n")
			buf.WriteString(fmt.Sprintf("│ Input Tokens         │ %8d │ %8d │ %8d │ %9.2f%% │\n",
				result.ZaiResponse.TokenUsage.InputTokens,
				result.AnthropicResponse.TokenUsage.InputTokens,
				result.InputTokenDiff,
				result.InputTokenPercentDiff))
			buf.WriteString(fmt.Sprintf("│ Output Tokens        │ %8d │ %8d │ %8d │ %9.2f%% │\n",
				result.ZaiResponse.TokenUsage.OutputTokens,
				result.AnthropicResponse.TokenUsage.OutputTokens,
				result.OutputTokenDiff,
				result.OutputTokenPercentDiff))
			buf.WriteString("└─────────────────────┴──────────┴──────────┴──────────┴────────────┘\n")

			// Match indicators
			inputMatch := "✓"
			if !result.InputTokenMatch {
				inputMatch = "✗"
			}
			outputMatch := "✓"
			if !result.OutputTokenMatch {
				outputMatch = "✗"
			}
			buf.WriteString(fmt.Sprintf("\nInput Tokens Match:  %s  Output Tokens Match: %s\n", inputMatch, outputMatch))
		} else {
			buf.WriteString("\n⚠ Token usage data not available for comparison\n")
			if result.ZaiResponse.TokenUsage == nil {
				buf.WriteString("  - Z.AI token usage: Not available\n")
			}
			if result.AnthropicResponse.TokenUsage == nil {
				buf.WriteString("  - Anthropic token usage: Not available\n")
			}
		}

		// Structure match
		structureMatch := "✓"
		if !result.ResponseStructureMatch {
			structureMatch = "✗"
		}
		buf.WriteString(fmt.Sprintf("Structure Match:     %s\n\n", structureMatch))

		// Response snippets (truncated)
		if len(result.ZaiResponse.Body) > 0 && len(result.AnthropicResponse.Body) > 0 {
			buf.WriteString("Response Preview:\n")
			buf.WriteString("Z.AI Response:\n")
			buf.WriteString(formatJSONPreview(result.ZaiResponse.Body, 200))
			buf.WriteString("\nAnthropic Response:\n")
			buf.WriteString(formatJSONPreview(result.AnthropicResponse.Body, 200))
			buf.WriteString("\n")
		}

		buf.WriteString("---\n\n")
	}

	// Analysis and Recommendations
	buf.WriteString("## ANALYSIS AND RECOMMENDATIONS\n\n")
	buf.WriteString(rg.generateAnalysis())

	return buf.String()
}

// generateAnalysis creates analysis based on metrics
func (rg *ReportGenerator) generateAnalysis() string {
	var buf bytes.Buffer

	// Token accuracy analysis
	if rg.metrics.InputTokenMAE > 10 || rg.metrics.OutputTokenMAE > 10 {
		buf.WriteString("### ⚠ Token Counting Accuracy Concerns\n\n")
		if rg.metrics.InputTokenMAE > 10 {
			buf.WriteString(fmt.Sprintf("- Input token MAE (%.2f) exceeds threshold of 10 tokens\n", rg.metrics.InputTokenMAE))
		}
		if rg.metrics.OutputTokenMAE > 10 {
			buf.WriteString(fmt.Sprintf("- Output token MAE (%.2f) exceeds threshold of 10 tokens\n", rg.metrics.OutputTokenMAE))
		}
		buf.WriteString("- Recommendation: Review tokenizer configuration and consider model-specific encoding\n\n")
	} else if rg.metrics.InputTokenMAE > 0 {
		buf.WriteString("### ✓ Token Counting Accuracy\n\n")
		buf.WriteString("Token counts are within acceptable tolerance levels.\n")
		buf.WriteString(fmt.Sprintf("- Input MAE: %.2f tokens\n", rg.metrics.InputTokenMAE))
		buf.WriteString(fmt.Sprintf("- Output MAE: %.2f tokens\n\n", rg.metrics.OutputTokenMAE))
	}

	// Percentage difference analysis
	if rg.metrics.InputTokenAvgPercentDiff > 5 || rg.metrics.OutputTokenAvgPercentDiff > 5 {
		buf.WriteString("### ⚠ Percentage Difference Analysis\n\n")
		if rg.metrics.InputTokenAvgPercentDiff > 5 {
			buf.WriteString(fmt.Sprintf("- Average input token difference (%.2f%%) exceeds 5%% threshold\n", rg.metrics.InputTokenAvgPercentDiff))
		}
		if rg.metrics.OutputTokenAvgPercentDiff > 5 {
			buf.WriteString(fmt.Sprintf("- Average output token difference (%.2f%%) exceeds 5%% threshold\n", rg.metrics.OutputTokenAvgPercentDiff))
		}
		buf.WriteString("- Recommendation: Investigate systematic biases in token counting\n\n")
	}

	// Success rate analysis
	successRate := float64(rg.metrics.SuccessfulTests) / float64(rg.metrics.TotalTests) * 100
	if successRate < 100 {
		buf.WriteString(fmt.Sprintf("### ⚠ Success Rate: %.1f%%\n\n", successRate))
		buf.WriteString("Some tests failed. Review error logs above for details.\n\n")
	}

	// Structure match analysis
	structureRate := float64(rg.metrics.StructureMatchCount) / float64(rg.metrics.TotalTests) * 100
	if structureRate < 100 {
		buf.WriteString(fmt.Sprintf("### ⚠ Structure Match Rate: %.1f%%\n\n", structureRate))
		buf.WriteString("Some responses have different structures. This may indicate:\n")
		buf.WriteString("- Different response formats between endpoints\n")
		buf.WriteString("- Missing or extra fields in responses\n\n")
	}

	// Pattern analysis
	buf.WriteString("### Pattern Analysis\n\n")
	buf.WriteString(rg.identifyPatterns())

	return buf.String()
}

// identifyPatterns identifies systematic patterns in discrepancies
func (rg *ReportGenerator) identifyPatterns() string {
	var buf bytes.Buffer

	inputConsistent := 0
	inputZaiHigher := 0
	inputZaiLower := 0
	outputConsistent := 0
	outputZaiHigher := 0
	outputZaiLower := 0
	streamingTests := 0
	nonStreamingTests := 0

	for _, result := range rg.results {
		if result.ZaiResponse.TokenUsage != nil && result.AnthropicResponse.TokenUsage != nil {
			if result.InputTokenDiff == 0 {
				inputConsistent++
			} else if result.InputTokenDiff > 0 {
				inputZaiHigher++
			} else {
				inputZaiLower++
			}

			if result.OutputTokenDiff == 0 {
				outputConsistent++
			} else if result.OutputTokenDiff > 0 {
				outputZaiHigher++
			} else {
				outputZaiLower++
			}
		}

		if result.ZaiResponse.TokenUsage != nil {
			// Check if streaming by looking at the test
			for _, test := range GetTestCases() {
				if test.Name == result.TestName {
					if test.Stream {
						streamingTests++
					} else {
						nonStreamingTests++
					}
					break
				}
			}
		}
	}

	buf.WriteString("#### Input Token Patterns\n")
	buf.WriteString(fmt.Sprintf("- Exact matches: %d\n", inputConsistent))
	buf.WriteString(fmt.Sprintf("- Z.AI higher: %d\n", inputZaiHigher))
	buf.WriteString(fmt.Sprintf("- Z.AI lower: %d\n", inputZaiLower))

	if inputZaiHigher > inputZaiLower*2 {
		buf.WriteString("→ Pattern: Z.AI consistently reports higher input tokens\n")
		buf.WriteString("  Possible cause: Different tokenization algorithm or encoding\n")
	} else if inputZaiLower > inputZaiHigher*2 {
		buf.WriteString("→ Pattern: Z.AI consistently reports lower input tokens\n")
		buf.WriteString("  Possible cause: Undercounting or missing tokens in calculation\n")
	} else if inputConsistent == 0 {
		buf.WriteString("→ Pattern: No exact matches found\n")
		buf.WriteString("  Possible cause: Systematic difference in counting methodology\n")
	}

	buf.WriteString("\n#### Output Token Patterns\n")
	buf.WriteString(fmt.Sprintf("- Exact matches: %d\n", outputConsistent))
	buf.WriteString(fmt.Sprintf("- Z.AI higher: %d\n", outputZaiHigher))
	buf.WriteString(fmt.Sprintf("- Z.AI lower: %d\n", outputZaiLower))

	if outputZaiHigher > outputZaiLower*2 {
		buf.WriteString("→ Pattern: Z.AI consistently reports higher output tokens\n")
		buf.WriteString("  Possible cause: Counting control tokens or metadata\n")
	} else if outputZaiLower > outputZaiHigher*2 {
		buf.WriteString("→ Pattern: Z.AI consistently reports lower output tokens\n")
		buf.WriteString("  Possible cause: Truncation or incomplete capture\n")
	}

	buf.WriteString(fmt.Sprintf("\n#### Test Type Distribution\n"))
	buf.WriteString(fmt.Sprintf("- Streaming tests: %d\n", streamingTests))
	buf.WriteString(fmt.Sprintf("- Non-streaming tests: %d\n", nonStreamingTests))

	return buf.String()
}

// formatJSONPreview creates a formatted preview of JSON response
func formatJSONPreview(data []byte, maxLen int) string {
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, data, "", "  "); err != nil {
		return string(data)
	}

	preview := prettyJSON.String()
	if len(preview) > maxLen {
		return preview[:maxLen] + "..."
	}
	return preview
}

// SaveToFile saves the report to a file
func (rg *ReportGenerator) SaveToFile(filename string) error {
	report := rg.GenerateTextReport()
	return os.WriteFile(filename, []byte(report), 0644)
}

// GenerateJSONReport creates a JSON report for programmatic consumption
func (rg *ReportGenerator) GenerateJSONReport() ([]byte, error) {
	report := struct {
		GeneratedAt  string                `json:"generated_at"`
		Metrics      *EvaluationMetrics    `json:"metrics"`
		TestResults  []ComparisonResult    `json:"test_results"`
		Interpretation map[string]interface{} `json:"interpretation"`
	}{
		GeneratedAt:  time.Now().Format(time.RFC3339),
		Metrics:      rg.metrics,
		TestResults:  rg.results,
		Interpretation: map[string]interface{}{
			"overall_accuracy": rg.calculateOverallAccuracy(),
			"recommendations":  rg.getRecommendations(),
			"patterns":         rg.identifyPatterns(),
		},
	}

	return json.MarshalIndent(report, "", "  ")
}

// calculateOverallAccuracy calculates an overall accuracy score
func (rg *ReportGenerator) calculateOverallAccuracy() map[string]float64 {
	accuracy := make(map[string]float64)

	if rg.metrics.TotalTests > 0 {
		accuracy["success_rate"] = float64(rg.metrics.SuccessfulTests) / float64(rg.metrics.TotalTests) * 100
		accuracy["structure_match_rate"] = float64(rg.metrics.StructureMatchCount) / float64(rg.metrics.TotalTests) * 100
	}

	// Token accuracy (inverse of MAE, scaled)
	if rg.metrics.InputTokenMAE > 0 {
		accuracy["input_token_accuracy"] = 100 - min(rg.metrics.InputTokenAvgPercentDiff, 100)
	}
	if rg.metrics.OutputTokenMAE > 0 {
		accuracy["output_token_accuracy"] = 100 - min(rg.metrics.OutputTokenAvgPercentDiff, 100)
	}

	return accuracy
}

// getRecommendations returns actionable recommendations
func (rg *ReportGenerator) getRecommendations() []string {
	var recommendations []string

	if rg.metrics.InputTokenMAE > 10 {
		recommendations = append(recommendations, "Input token counting has high variance - verify tokenizer model matches Anthropic's")
	}

	if rg.metrics.OutputTokenMAE > 10 {
		recommendations = append(recommendations, "Output token counting has high variance - check SSE parsing logic")
	}

	successRate := float64(rg.metrics.SuccessfulTests) / float64(rg.metrics.TotalTests)
	if successRate < 1.0 {
		recommendations = append(recommendations, "Some requests failed - review error handling and retry logic")
	}

	structureRate := float64(rg.metrics.StructureMatchCount) / float64(rg.metrics.TotalTests)
	if structureRate < 1.0 {
		recommendations = append(recommendations, "Response structure mismatches detected - verify response forwarding")
	}

	if len(recommendations) == 0 {
		recommendations = append(recommendations, "All metrics within acceptable ranges - no immediate action required")
	}

	return recommendations
}

// GenerateHTMLReport creates an HTML formatted report
func (rg *ReportGenerator) GenerateHTMLReport() (string, error) {
	const htmlTemplate = `
<!DOCTYPE html>
<html>
<head>
    <title>Z.AI Proxy Evaluation Report</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; background: #f5f5f5; }
        .container { max-width: 1200px; margin: 0 auto; background: white; padding: 30px; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        h1 { color: #333; border-bottom: 3px solid #4CAF50; padding-bottom: 10px; }
        h2 { color: #555; margin-top: 30px; border-bottom: 1px solid #ddd; padding-bottom: 5px; }
        .summary { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 20px; margin: 20px 0; }
        .metric-card { background: #f9f9f9; padding: 20px; border-radius: 6px; border-left: 4px solid #4CAF50; }
        .metric-label { font-size: 12px; color: #666; text-transform: uppercase; }
        .metric-value { font-size: 32px; font-weight: bold; color: #333; }
        .success { color: #4CAF50; }
        .warning { color: #ff9800; }
        .error { color: #f44336; }
        table { width: 100%; border-collapse: collapse; margin: 20px 0; }
        th, td { padding: 12px; text-align: left; border-bottom: 1px solid #ddd; }
        th { background: #f5f5f5; font-weight: 600; }
        .test-result { margin: 20px 0; padding: 15px; background: #f9f9f9; border-radius: 6px; }
        .match { color: #4CAF50; font-weight: bold; }
        .mismatch { color: #ff9800; font-weight: bold; }
        .timestamp { color: #999; font-size: 12px; }
    </style>
</head>
<body>
    <div class="container">
        <h1>🔍 Z.AI Proxy Evaluation Report</h1>
        <p class="timestamp">Generated: {{.Timestamp}}</p>

        <h2>Executive Summary</h2>
        <div class="summary">
            <div class="metric-card">
                <div class="metric-label">Total Tests</div>
                <div class="metric-value">{{.TotalTests}}</div>
            </div>
            <div class="metric-card">
                <div class="metric-label">Success Rate</div>
                <div class="metric-value {{if .HighSuccessRate}}success{{else}}warning{{end}}">{{.SuccessRate}}%</div>
            </div>
            <div class="metric-card">
                <div class="metric-label">Input Token MAE</div>
                <div class="metric-value {{if .LowInputMAE}}success{{else}}warning{{end}}">{{.InputMAE}}</div>
            </div>
            <div class="metric-card">
                <div class="metric-label">Output Token MAE</div>
                <div class="metric-value {{if .LowOutputMAE}}success{{else}}warning{{end}}">{{.OutputMAE}}</div>
            </div>
        </div>

        <h2>Detailed Results</h2>
        {{range .Results}}
        <div class="test-result">
            <h3>{{.TestName}}</h3>
            <table>
                <tr>
                    <th>Metric</th>
                    <th>Z.AI</th>
                    <th>Anthropic</th>
                    <th>Difference</th>
                </tr>
                {{if .ZaiResponse.TokenUsage}}
                <tr>
                    <td>Input Tokens</td>
                    <td>{{.ZaiResponse.TokenUsage.InputTokens}}</td>
                    <td>{{.AnthropicResponse.TokenUsage.InputTokens}}</td>
                    <td class="{{if .InputTokenMatch}}match{{else}}mismatch{{end}}">{{.InputTokenDiff}} ({{.InputTokenPercentDiff}}%)</td>
                </tr>
                <tr>
                    <td>Output Tokens</td>
                    <td>{{.ZaiResponse.TokenUsage.OutputTokens}}</td>
                    <td>{{.AnthropicResponse.TokenUsage.OutputTokens}}</td>
                    <td class="{{if .OutputTokenMatch}}match{{else}}mismatch{{end}}">{{.OutputTokenDiff}} ({{.OutputTokenPercentDiff}}%)</td>
                </tr>
                {{end}}
            </table>
        </div>
        {{end}}
    </div>
</body>
</html>
`

	data := struct {
		Timestamp      string
		TotalTests     int
		SuccessRate    float64
		HighSuccessRate bool
		InputMAE       float64
		OutputMAE      float64
		LowInputMAE    bool
		LowOutputMAE   bool
		Results        []ComparisonResult
	}{
		Timestamp:      time.Now().Format("2006-01-02 15:04:05"),
		TotalTests:     rg.metrics.TotalTests,
		SuccessRate:    float64(rg.metrics.SuccessfulTests) / float64(rg.metrics.TotalTests) * 100,
		HighSuccessRate: rg.metrics.SuccessfulTests == rg.metrics.TotalTests,
		InputMAE:       rg.metrics.InputTokenMAE,
		OutputMAE:      rg.metrics.OutputTokenMAE,
		LowInputMAE:    rg.metrics.InputTokenMAE < 10,
		LowOutputMAE:   rg.metrics.OutputTokenMAE < 10,
		Results:        rg.results,
	}

	tmpl, err := template.New("report").Parse(htmlTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
