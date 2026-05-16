package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"git.ardenone.com/jedarden/zai-proxy/evaluation"
)

func main() {
	// Command-line flags
	zaiEndpoint := flag.String("zai-endpoint", "http://localhost:8080/v1/messages", "Z.AI proxy endpoint")
	anthropicEndpoint := flag.String("anthropic-endpoint", "https://api.anthropic.com/v1/messages", "Anthropic API endpoint")
	zaiKey := flag.String("zai-key", os.Getenv("ZAI_API_KEY"), "Z.AI API key (or set ZAI_API_KEY env var)")
	anthropicKey := flag.String("anthropic-key", os.Getenv("ANTHROPIC_API_KEY"), "Anthropic API key (or set ANTHROPIC_API_KEY env var)")
	outputFile := flag.String("output", "evaluation-report.txt", "Output file for report")
	jsonOutput := flag.String("json-output", "", "JSON output file (optional)")
	htmlOutput := flag.String("html-output", "", "HTML output file (optional)")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")

	flag.Parse()

	// Validate required parameters
	if *zaiKey == "" {
		log.Fatal("Z.AI API key is required. Set ZAI_API_KEY environment variable or use -zai-key flag")
	}
	if *anthropicKey == "" {
		log.Fatal("Anthropic API key is required. Set ANTHROPIC_API_KEY environment variable or use -anthropic-key flag")
	}

	if !*verbose {
		log.SetOutput(os.Stdout)
	}

	log.Println("🔍 Z.AI Proxy Evaluation Framework")
	log.Println("==================================")
	log.Printf("Z.AI Endpoint:       %s", *zaiEndpoint)
	log.Printf("Anthropic Endpoint: %s", *anthropicEndpoint)
	log.Println()

	// Create evaluator
	eval := evaluation.NewEvaluator(*zaiEndpoint, *anthropicEndpoint, *zaiKey, *anthropicKey)

	// Get test cases
	tests := evaluation.GetTestCases()
	log.Printf("Running %d test cases...\n", len(tests))

	// Run evaluation
	results, metrics := eval.RunTests(tests)

	// Generate report
	log.Println("\nGenerating reports...")
	reporter := evaluation.NewReportGenerator(results, metrics)

	// Save text report
	if err := reporter.SaveToFile(*outputFile); err != nil {
		log.Fatalf("Failed to save text report: %v", err)
	}
	log.Printf("✓ Text report saved to: %s", *outputFile)

	// Save JSON report if requested
	if *jsonOutput != "" {
		jsonData, err := reporter.GenerateJSONReport()
		if err != nil {
			log.Fatalf("Failed to generate JSON report: %v", err)
		}
		if err := os.WriteFile(*jsonOutput, jsonData, 0644); err != nil {
			log.Fatalf("Failed to save JSON report: %v", err)
		}
		log.Printf("✓ JSON report saved to: %s", *jsonOutput)
	}

	// Save HTML report if requested
	if *htmlOutput != "" {
		htmlReport, err := reporter.GenerateHTMLReport()
		if err != nil {
			log.Fatalf("Failed to generate HTML report: %v", err)
		}
		if err := os.WriteFile(*htmlOutput, []byte(htmlReport), 0644); err != nil {
			log.Fatalf("Failed to save HTML report: %v", err)
		}
		log.Printf("✓ HTML report saved to: %s", *htmlOutput)
	}

	// Print summary
	fmt.Println("\n" + reporter.GenerateTextReport())

	// Exit with appropriate code
	if metrics.SuccessfulTests < metrics.TotalTests {
		os.Exit(1)
	}
}
