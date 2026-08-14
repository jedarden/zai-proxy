package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// TestFailure represents a parsed test failure
type TestFailure struct {
	TestName    string   `json:"test_name"`
	FilePath    string   `json:"file_path,omitempty"`
	LineNumber  string   `json:"line_number,omitempty"`
	ErrorLines  []string `json:"error_lines"`
	SubTest     string   `json:"sub_test,omitempty"`
	FullContext string   `json:"full_context,omitempty"`
}

// TestOutput represents the complete parsed test output
type TestOutput struct {
	TotalTests   int          `json:"total_tests"`
	PassedTests  int          `json:"passed_tests"`
	FailedTests  int          `json:"failed_tests"`
	Failures     []TestFailure `json:"failures"`
	Summary      string       `json:"summary"`
}

var (
	// Regex patterns for parsing
	testRunPattern    = regexp.MustCompile(`=== RUN   (\S+)(?:/(\S+))?`)
	testPassPattern   = regexp.MustCompile(`--- PASS: (\S+)(?:/(\S+))?`)
	testFailPattern   = regexp.MustCompile(`--- FAIL: (\S+)(?:/(\S+))?`)
	fileLinePattern   = regexp.MustCompile(`^(\S+\.go):(\d+):`)
	summaryPattern    = regexp.MustCompile(`(FAIL|PASS)\s+([^\s]+)\s+(\d+\.\d+)s`)
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: test-output-parser <test-output-file>")
		os.Exit(1)
	}

	inputFile := os.Args[1]
	data, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	output := parseTestOutput(string(data))

	// Output JSON
	jsonData, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(jsonData))
}

func parseTestOutput(output string) TestOutput {
	scanner := bufio.NewScanner(strings.NewReader(output))
	var result TestOutput

	var currentTest *TestFailure
	var currentTestName string
	var currentSubTest string
	var errorLines []string
	var inFailureContext bool
	var passedTests, failedTests int

	for scanner.Scan() {
		line := scanner.Text()

		// Check for test start
		if matches := testRunPattern.FindStringSubmatch(line); matches != nil {
			currentTestName = matches[1]
			if len(matches) > 2 && matches[2] != "" {
				currentSubTest = matches[2]
			}
			continue
		}

		// Check for test pass
		if matches := testPassPattern.FindStringSubmatch(line); matches != nil {
			passedTests++
			currentTestName = ""
			currentSubTest = ""
			errorLines = nil
			continue
		}

		// Check for test failure
		if matches := testFailPattern.FindStringSubmatch(line); matches != nil {
			failedTests++

			// Save the previous failure if exists
			if currentTest != nil && len(errorLines) > 0 {
				currentTest.ErrorLines = errorLines
				result.Failures = append(result.Failures, *currentTest)
			}

			// Start new failure
			testName := matches[1]
			subTest := ""
			if len(matches) > 2 && matches[2] != "" {
				subTest = matches[2]
			}

			currentTest = &TestFailure{
				TestName:   testName,
				SubTest:    subTest,
				ErrorLines: errorLines,
			}
			errorLines = nil
			inFailureContext = true
			continue
		}

		// Check for file:line pattern within failure context
		if inFailureContext && currentTest != nil {
			if matches := fileLinePattern.FindStringSubmatch(line); matches != nil {
				currentTest.FilePath = matches[1]
				currentTest.LineNumber = matches[2]
				errorLines = append(errorLines, line)
			} else if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "---") && !strings.HasPrefix(line, "===") {
				// Collect error context lines
				errorLines = append(errorLines, line)
			}
		}

		// Check for summary line (end of test run)
		if matches := summaryPattern.FindStringSubmatch(line); matches != nil {
			result.Summary = strings.TrimSpace(line)
		}
	}

	// Add the last failure
	if currentTest != nil && len(errorLines) > 0 {
		currentTest.ErrorLines = errorLines
		result.Failures = append(result.Failures, *currentTest)
	}

	result.TotalTests = passedTests + failedTests
	result.PassedTests = passedTests
	result.FailedTests = failedTests

	return result
}
