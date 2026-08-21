// Command failure-report converts verbose Go test output into a source-traceable
// JSON and Markdown failure report.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"git.ardenone.com/jedarden/zai-proxy/proxy/testutil"
)

const schemaVersion = 1

var (
	runPattern        = regexp.MustCompile(`^=== RUN\s+(\S+)\s*$`)
	failPattern       = regexp.MustCompile(`^\s*--- FAIL:\s+(\S+)`)
	diagnosticPattern = regexp.MustCompile(`^\s+(\S+\.go):(\d+):\s*(.+)$`)
	testFailureCall   = regexp.MustCompile(`\bt\.(?:Error|Errorf|Fatal|Fatalf|Fail|FailNow)\s*\(`)
)

type diagnostic struct {
	file    string
	line    int
	message string
}

type sourceOutput struct {
	TestCommand string `json:"test_command"`
	ExitCode    int    `json:"exit_code"`
	RawLog      string `json:"raw_log"`
}

type categoryCount struct {
	Category string `json:"category"`
	Label    string `json:"label"`
	Count    int    `json:"count"`
}

type summary struct {
	TotalFailures        int             `json:"total_failures"`
	FailedTestCases      int             `json:"failed_test_cases"`
	AggregateFailMarkers int             `json:"aggregate_failure_markers"`
	Categorized          int             `json:"categorized"`
	Uncategorized        int             `json:"uncategorized"`
	LowConfidence        int             `json:"low_confidence"`
	ByCategory           []categoryCount `json:"by_category"`
}

type failureRecord struct {
	ID              string  `json:"id"`
	TestName        string  `json:"test_name"`
	FilePath        string  `json:"file_path"`
	LineNumber      int     `json:"line_number"`
	SourceReference string  `json:"source_reference"`
	SourceLink      string  `json:"source_link"`
	ErrorMessage    string  `json:"error_message"`
	Category        string  `json:"category"`
	CategoryLabel   string  `json:"category_label"`
	Subcategory     string  `json:"subcategory,omitempty"`
	Confidence      float64 `json:"confidence"`
	Uncertain       bool    `json:"uncertain"`
	Reasoning       string  `json:"reasoning"`
}

type validation struct {
	Complete                   bool `json:"complete"`
	EveryFailureHasSource      bool `json:"every_failure_has_source_location"`
	EverySourceExists          bool `json:"every_source_location_exists"`
	EveryFailureIsCategorized  bool `json:"every_failure_is_categorized"`
	FailureDetailsMatchTestLog bool `json:"failure_details_match_test_log"`
}

type report struct {
	SchemaVersion      int             `json:"schema_version"`
	GeneratedAt        string          `json:"generated_at"`
	RepositoryRevision string          `json:"repository_revision"`
	SourceOutput       sourceOutput    `json:"source_output"`
	Summary            summary         `json:"summary"`
	Failures           []failureRecord `json:"failures"`
	Validation         validation      `json:"validation"`
}

func main() {
	inputPath := flag.String("input", "", "verbose go test output to parse")
	jsonPath := flag.String("json", "failures.json", "JSON report destination")
	markdownPath := flag.String("markdown", "failures.md", "Markdown report destination")
	sourceRoot := flag.String("source-root", ".", "repository-relative directory containing diagnostics in the input")
	revision := flag.String("revision", "", "source revision tested")
	testCommand := flag.String("test-command", "go test -v ./...", "command that produced the input")
	exitCode := flag.Int("exit-code", 0, "exit code from the test command")
	generatedAt := flag.String("generated-at", "", "RFC3339 report timestamp (defaults to now)")
	flag.Parse()

	if *inputPath == "" {
		fatal(errors.New("-input is required"))
	}
	if *revision == "" {
		fatal(errors.New("-revision is required"))
	}

	diagnostics, failedTests, failureMarkers, err := parseOutput(*inputPath)
	if err != nil {
		fatal(err)
	}

	generated := time.Now().UTC().Format(time.RFC3339)
	if *generatedAt != "" {
		if _, err := time.Parse(time.RFC3339, *generatedAt); err != nil {
			fatal(fmt.Errorf("parse -generated-at: %w", err))
		}
		generated = *generatedAt
	}

	report, err := buildReport(diagnostics, failedTests, failureMarkers, *sourceRoot, *markdownPath, *revision, generated, sourceOutput{
		TestCommand: *testCommand,
		ExitCode:    *exitCode,
		RawLog:      *inputPath,
	})
	if err != nil {
		fatal(err)
	}

	jsonBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fatal(fmt.Errorf("marshal report: %w", err))
	}
	if err := os.WriteFile(*jsonPath, append(jsonBytes, '\n'), 0o644); err != nil {
		fatal(fmt.Errorf("write JSON report: %w", err))
	}

	markdown := renderMarkdown(report)
	if err := os.WriteFile(*markdownPath, []byte(markdown), 0o644); err != nil {
		fatal(fmt.Errorf("write Markdown report: %w", err))
	}

	fmt.Printf("Wrote %d failure details to %s and %s\n", len(report.Failures), *jsonPath, *markdownPath)
}

func parseOutput(inputPath string) (map[string][]diagnostic, []string, int, error) {
	file, err := os.Open(inputPath)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("open test output %q: %w", inputPath, err)
	}
	defer file.Close()

	diagnosticsByTest := make(map[string][]diagnostic)
	failed := make(map[string]bool)
	failedOrder := make([]string, 0)
	currentTest := ""
	failureMarkers := 0

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if matches := runPattern.FindStringSubmatch(line); matches != nil {
			currentTest = matches[1]
			continue
		}
		if matches := diagnosticPattern.FindStringSubmatch(line); matches != nil && currentTest != "" {
			var lineNumber int
			if _, err := fmt.Sscanf(matches[2], "%d", &lineNumber); err != nil {
				return nil, nil, 0, fmt.Errorf("parse diagnostic line number %q: %w", matches[2], err)
			}
			diagnosticsByTest[currentTest] = append(diagnosticsByTest[currentTest], diagnostic{
				file: matches[1], line: lineNumber, message: matches[3],
			})
			continue
		}
		if matches := failPattern.FindStringSubmatch(line); matches != nil {
			failureMarkers++
			if !failed[matches[1]] {
				failed[matches[1]] = true
				failedOrder = append(failedOrder, matches[1])
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, 0, fmt.Errorf("read test output: %w", err)
	}

	for _, testName := range failedOrder {
		if len(diagnosticsByTest[testName]) > 0 || hasFailedDescendant(testName, failed) {
			continue
		}
		return nil, nil, 0, fmt.Errorf("failed test %q has no source diagnostic", testName)
	}
	return diagnosticsByTest, failedOrder, failureMarkers, nil
}

func hasFailedDescendant(testName string, failed map[string]bool) bool {
	prefix := testName + "/"
	for candidate := range failed {
		if strings.HasPrefix(candidate, prefix) {
			return true
		}
	}
	return false
}

func buildReport(diagnosticsByTest map[string][]diagnostic, failedTests []string, failureMarkers int, sourceRoot, markdownPath, sourceRevision, generatedAt string, output sourceOutput) (report, error) {
	report := report{
		SchemaVersion:      schemaVersion,
		GeneratedAt:        generatedAt,
		RepositoryRevision: sourceRevision,
		SourceOutput:       output,
		Failures:           make([]failureRecord, 0),
	}
	categoryCounts := make(map[string]categoryCount)
	failedTestCases := 0
	allSourcesExist := true

	for _, testName := range failedTests {
		diagnostics := diagnosticsByTest[testName]
		failureDiagnostics := make([]struct {
			diagnostic
			sourcePath string
		}, 0, len(diagnostics))
		for _, candidate := range diagnostics {
			sourcePath := candidate.file
			if !filepath.IsAbs(sourcePath) {
				sourcePath = filepath.Join(sourceRoot, sourcePath)
			}
			sourcePath = filepath.ToSlash(filepath.Clean(sourcePath))
			if _, err := os.Stat(sourcePath); err != nil {
				allSourcesExist = false
				return report, fmt.Errorf("failure source %s:%d: %w", sourcePath, candidate.line, err)
			}
			isFailure, err := sourceLineIsFailureCall(sourcePath, candidate.line)
			if err != nil {
				return report, err
			}
			if isFailure {
				failureDiagnostics = append(failureDiagnostics, struct {
					diagnostic
					sourcePath string
				}{diagnostic: candidate, sourcePath: sourcePath})
			}
		}
		if len(failureDiagnostics) == 0 {
			if hasFailedDescendant(testName, failedTestSet(failedTests)) {
				continue // Aggregate parent failure; its failed subtests carry the diagnostics.
			}
			return report, fmt.Errorf("failed test %q has no t.Error or t.Fatal source diagnostic", testName)
		}
		failedTestCases++
		for _, failureDiagnostic := range failureDiagnostics {
			diagnostic := failureDiagnostic.diagnostic
			sourcePath := failureDiagnostic.sourcePath
			categorized := testutil.CategorizeFailure(testutil.TestFailure{
				TestName: testName, FilePath: sourcePath, LineNumber: diagnostic.line, ErrorMessage: diagnostic.message,
			})
			category := string(categorized.Category)
			label := testutil.GetCategoryLabel(categorized)
			record := failureRecord{
				ID:              fmt.Sprintf("F%03d", len(report.Failures)+1),
				TestName:        testName,
				FilePath:        sourcePath,
				LineNumber:      diagnostic.line,
				SourceReference: fmt.Sprintf("%s:%d", sourcePath, diagnostic.line),
				SourceLink:      sourceLink(markdownPath, sourcePath, diagnostic.line),
				ErrorMessage:    diagnostic.message,
				Category:        category,
				CategoryLabel:   label,
				Subcategory:     categorized.Subcategory,
				Confidence:      categorized.Confidence.Float64(),
				Uncertain:       categorized.Uncertain,
				Reasoning:       categorized.Reasoning,
			}
			report.Failures = append(report.Failures, record)
			count := categoryCounts[category]
			count.Category, count.Label, count.Count = category, label, count.Count+1
			categoryCounts[category] = count
			if category == string(testutil.CategoryUnknown) {
				report.Summary.Uncategorized++
			} else {
				report.Summary.Categorized++
			}
			if categorized.Confidence <= 0.5 {
				report.Summary.LowConfidence++
			}
		}
	}

	report.Summary.TotalFailures = len(report.Failures)
	report.Summary.FailedTestCases = failedTestCases
	report.Summary.AggregateFailMarkers = failureMarkers - failedTestCases
	for _, count := range categoryCounts {
		report.Summary.ByCategory = append(report.Summary.ByCategory, count)
	}
	sort.Slice(report.Summary.ByCategory, func(i, j int) bool {
		if report.Summary.ByCategory[i].Count != report.Summary.ByCategory[j].Count {
			return report.Summary.ByCategory[i].Count > report.Summary.ByCategory[j].Count
		}
		return report.Summary.ByCategory[i].Category < report.Summary.ByCategory[j].Category
	})
	report.Validation = validation{
		Complete:                   len(report.Failures) > 0 && failedTestCases > 0,
		EveryFailureHasSource:      len(report.Failures) > 0,
		EverySourceExists:          allSourcesExist,
		EveryFailureIsCategorized:  true,
		FailureDetailsMatchTestLog: len(report.Failures) > 0,
	}
	return report, nil
}

func failedTestSet(failedTests []string) map[string]bool {
	failed := make(map[string]bool, len(failedTests))
	for _, testName := range failedTests {
		failed[testName] = true
	}
	return failed
}

func sourceLineIsFailureCall(sourcePath string, lineNumber int) (bool, error) {
	contents, err := os.ReadFile(sourcePath)
	if err != nil {
		return false, fmt.Errorf("read failure source %q: %w", sourcePath, err)
	}
	lines := strings.Split(string(contents), "\n")
	if lineNumber < 1 || lineNumber > len(lines) {
		return false, fmt.Errorf("failure source %q has no line %d", sourcePath, lineNumber)
	}
	return testFailureCall.MatchString(lines[lineNumber-1]), nil
}

func sourceLink(markdownPath, sourcePath string, line int) string {
	base := filepath.Dir(markdownPath)
	relative, err := filepath.Rel(base, sourcePath)
	if err != nil {
		relative = sourcePath
	}
	return filepath.ToSlash(relative) + fmt.Sprintf("#L%d", line)
}

func renderMarkdown(report report) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# Structured Failure Report\n\n")
	fmt.Fprintf(&builder, "Generated from `%s` at `%s` (revision `%s`; exit code `%d`).\n\n", report.SourceOutput.TestCommand, report.GeneratedAt, report.RepositoryRevision, report.SourceOutput.ExitCode)
	fmt.Fprintf(&builder, "The raw verbose test log is retained outside the repository at `%s`. `total_failures` counts emitted source diagnostics; a parent Go subtest aggregate is not counted twice.\n\n", report.SourceOutput.RawLog)
	fmt.Fprintf(&builder, "## Summary\n\n| Metric | Count |\n| --- | ---: |\n| Total failure details | %d |\n| Failed test cases | %d |\n| Aggregate failure markers | %d |\n| Categorized | %d |\n| Other / uncategorized | %d |\n| Low confidence | %d |\n\n", report.Summary.TotalFailures, report.Summary.FailedTestCases, report.Summary.AggregateFailMarkers, report.Summary.Categorized, report.Summary.Uncategorized, report.Summary.LowConfidence)
	fmt.Fprintf(&builder, "## Failures by Category\n\n| Category | Count |\n| --- | ---: |\n")
	for _, count := range report.Summary.ByCategory {
		fmt.Fprintf(&builder, "| %s | %d |\n", markdownCell(count.Label), count.Count)
	}
	fmt.Fprintf(&builder, "\n## Failure Details\n\n| Test name | File:line | Error message | Category |\n| --- | --- | --- | --- |\n")
	for _, failure := range report.Failures {
		location := fmt.Sprintf("[%s](%s)", failure.SourceReference, failure.SourceLink)
		category := failure.CategoryLabel
		if failure.Subcategory != "" && !strings.HasPrefix(category, "Other:") {
			category += ": " + strings.ReplaceAll(failure.Subcategory, "_", " ")
		}
		fmt.Fprintf(&builder, "| %s | %s | %s | %s |\n", markdownCell(failure.TestName), location, markdownCell(truncate(failure.ErrorMessage, 160)), markdownCell(category))
	}
	fmt.Fprintf(&builder, "\n## Validation\n\nAll %d emitted failure details have a source location that exists at the tested revision, and all were classified by `proxy/testutil`. `%d` details are explicitly `Other: unclassified failure`; these need a taxonomy rule or manual review rather than being silently omitted.\n", report.Summary.TotalFailures, report.Summary.Uncategorized)
	return builder.String()
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "|", "\\|")
	return strings.ReplaceAll(value, "\n", " ")
}

func truncate(value string, maxRunes int) string {
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes-1]) + "…"
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "failure-report:", err)
	os.Exit(1)
}
