package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"git.ardenone.com/jedarden/zai-proxy/proxy/evaluation"
)

func main() {
	log.Println("🔍 Z.AI Proxy Evaluation Framework - Demo Mode")
	log.Println("==============================================")
	log.Println("Running simulated evaluation without real API calls...")
	log.Println()

	// Create simulated results to demonstrate the framework
	results := generateSimulatedResults()

	// Calculate metrics
	eval := evaluation.NewEvaluator("", "", "", "")
	metrics := eval.CalculateMetricsFromResults(results)

	// Generate report
	log.Println("Generating reports...")
	reporter := evaluation.NewReportGenerator(results, metrics)

	// Save text report
	textReport := reporter.GenerateTextReport()
	if err := os.WriteFile("evaluation-report.txt", []byte(textReport), 0644); err != nil {
		log.Fatalf("Failed to save text report: %v", err)
	}
	log.Println("✓ Text report saved to: evaluation-report.txt")

	// Save JSON report
	jsonReport, err := reporter.GenerateJSONReport()
	if err != nil {
		log.Fatalf("Failed to generate JSON report: %v", err)
	}
	if err := os.WriteFile("evaluation-report.json", jsonReport, 0644); err != nil {
		log.Fatalf("Failed to save JSON report: %v", err)
	}
	log.Println("✓ JSON report saved to: evaluation-report.json")

	// Print summary
	fmt.Println("\n" + textReport)

	log.Println("\n✓ Evaluation complete!")
	log.Println("To run with real endpoints:")
	log.Println("  export ZAI_API_KEY=your-zai-key")
	log.Println("  export ANTHROPIC_API_KEY=your-anthropic-key")
	log.Println("  go run cmd/evaluate/main.go -zai-endpoint http://localhost:8080/v1/messages")
}

// generateSimulatedResults creates realistic test results for demonstration
func generateSimulatedResults() []evaluation.ComparisonResult {
	tests := evaluation.GetTestCases()
	results := make([]evaluation.ComparisonResult, len(tests))

	// Simulate various scenarios:
	// - Some tests with perfect matches
	// - Some tests with small discrepancies (undercounting)
	// - Some tests with larger discrepancies
	scenarios := []struct {
		inputDiff   int
		outputDiff  int
		structMatch bool
		error       bool
	}{
		{0, 0, true, false},   // Perfect match
		{2, 1, true, false},   // Small discrepancy
		{-1, 0, true, false},  // Z.AI undercounts
		{5, 3, true, false},   // Medium discrepancy
		{0, 0, true, false},   // Perfect match
		{1, 2, true, false},   // Small discrepancy (streaming)
		{3, 1, true, false},   // Medium discrepancy
		{0, 1, true, false},   // Nearly perfect
		{0, 0, true, false},   // Perfect match
		{4, 2, true, false},   // Medium discrepancy
		{0, 0, true, false},   // Perfect match
		{2, 1, true, false},   // Small discrepancy
		{1, 3, true, false},   // Small discrepancy (streaming)
	}

	for i, test := range tests {
		if i >= len(scenarios) {
			break
		}
		scenario := scenarios[i]

		// Generate base token counts
		baseInput := (i+1)*10 + 20
		baseOutput := (i+1)*5 + 15

		results[i] = evaluation.ComparisonResult{
			TestName: test.Name,
			ZaiResponse: evaluation.ResponseData{
				StatusCode: 200,
				Duration:   time.Second + time.Duration(i*100)*time.Millisecond,
				TokenUsage: &evaluation.TokenUsage{
					InputTokens:  baseInput + scenario.inputDiff,
					OutputTokens: baseOutput + scenario.outputDiff,
				},
			},
			AnthropicResponse: evaluation.ResponseData{
				StatusCode: 200,
				Duration:   time.Duration(900+i*100)*time.Millisecond,
				TokenUsage: &evaluation.TokenUsage{
					InputTokens:  baseInput,
					OutputTokens: baseOutput,
				},
			},
			InputTokenMatch:        scenario.inputDiff == 0,
			OutputTokenMatch:       scenario.outputDiff == 0,
			InputTokenDiff:         scenario.inputDiff,
			OutputTokenDiff:        scenario.outputDiff,
			InputTokenPercentDiff:  float64(scenario.inputDiff) / float64(baseInput) * 100,
			OutputTokenPercentDiff: float64(scenario.outputDiff) / float64(baseOutput) * 100,
			ResponseStructureMatch: scenario.structMatch,
		}
	}

	return results
}
