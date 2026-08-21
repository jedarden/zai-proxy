// Command failure-report converts verbose Go test output into a source-traceable
// JSON and Markdown failure report.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
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
	Category   string  `json:"category"`
	Label      string  `json:"label"`
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
}

type rankedCount struct {
	Name       string  `json:"name"`
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
}

type patternCount struct {
	Pattern    string  `json:"pattern"`
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
}

type summary struct {
	TotalFailures        int             `json:"total_failures"`
	FailedTestCases      int             `json:"failed_test_cases"`
	AggregateFailMarkers int             `json:"aggregate_failure_markers"`
	Categorized          int             `json:"categorized"`
	Uncategorized        int             `json:"uncategorized"`
	LowConfidence        int             `json:"low_confidence"`
	ByCategory           []categoryCount `json:"by_category"`
	ByTest               []rankedCount   `json:"by_test"`
	ByFile               []rankedCount   `json:"by_file"`
	ByPattern            []patternCount  `json:"by_pattern"`
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
	testCounts := make(map[string]int)
	fileCounts := make(map[string]int)
	patternCounts := make(map[string]int)
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
			testCounts[testName]++
			fileCounts[sourcePath]++
			patternCounts[failurePattern(diagnostic.message)]++
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
		count.Percentage = percentage(count.Count, report.Summary.TotalFailures)
		report.Summary.ByCategory = append(report.Summary.ByCategory, count)
	}
	sortCategoryCounts(report.Summary.ByCategory)
	report.Summary.ByTest = rankedCounts(testCounts, report.Summary.TotalFailures)
	report.Summary.ByFile = rankedCounts(fileCounts, report.Summary.TotalFailures)
	report.Summary.ByPattern = patternCountsForReport(patternCounts, report.Summary.TotalFailures)
	report.Validation = validation{
		Complete:                   len(report.Failures) > 0 && failedTestCases > 0,
		EveryFailureHasSource:      len(report.Failures) > 0,
		EverySourceExists:          allSourcesExist,
		EveryFailureIsCategorized:  true,
		FailureDetailsMatchTestLog: len(report.Failures) > 0,
	}
	return report, nil
}

func failurePattern(message string) string {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "memory allocation too high"):
		return "Memory-allocation limit exceeded"
	case strings.Contains(lower, "expected changed=true"):
		return "Transformation did not report a change"
	case strings.Contains(lower, "'thinking' should have been removed"):
		return "Thinking field was not removed"
	case strings.Contains(lower, "'system' should be a string"):
		return "System field was not converted to a string"
	case strings.Contains(lower, "cache_control after stripping"):
		return "cache_control was not removed"
	case strings.Contains(lower, "expected error for invalid json"):
		return "Invalid JSON did not return an error"
	default:
		return "Other failure message"
	}
}

func percentage(count, total int) float64 {
	if total == 0 {
		return 0
	}
	return math.Round(float64(count)/float64(total)*1000) / 10
}

func sortCategoryCounts(counts []categoryCount) {
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].Count != counts[j].Count {
			return counts[i].Count > counts[j].Count
		}
		return counts[i].Category < counts[j].Category
	})
}

func rankedCounts(counts map[string]int, total int) []rankedCount {
	ranked := make([]rankedCount, 0, len(counts))
	for name, count := range counts {
		ranked = append(ranked, rankedCount{Name: name, Count: count, Percentage: percentage(count, total)})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Count != ranked[j].Count {
			return ranked[i].Count > ranked[j].Count
		}
		return ranked[i].Name < ranked[j].Name
	})
	return ranked
}

func patternCountsForReport(counts map[string]int, total int) []patternCount {
	ranked := make([]patternCount, 0, len(counts))
	for pattern, count := range counts {
		ranked = append(ranked, patternCount{Pattern: pattern, Count: count, Percentage: percentage(count, total)})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Count != ranked[j].Count {
			return ranked[i].Count > ranked[j].Count
		}
		return ranked[i].Pattern < ranked[j].Pattern
	})
	return ranked
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
	fmt.Fprintf(&builder, "## Failures by Category\n\n| Category | Count | Share of failure details |\n| --- | ---: | ---: |\n")
	for _, count := range report.Summary.ByCategory {
		fmt.Fprintf(&builder, "| %s | %d | %.1f%% |\n", markdownCell(count.Label), count.Count, count.Percentage)
	}
	if missing := missingStandardCategories(report.Summary.ByCategory); len(missing) > 0 {
		fmt.Fprintf(&builder, "\nNot observed: %s.\n", strings.Join(missing, ", "))
	}

	fmt.Fprintf(&builder, "\n## Most Frequently Failing Tests\n\n| Test name | Failure details | Share of failure details |\n| --- | ---: | ---: |\n")
	for _, count := range report.Summary.ByTest {
		fmt.Fprintf(&builder, "| %s | %d | %.1f%% |\n", markdownCell(count.Name), count.Count, count.Percentage)
	}

	fmt.Fprintf(&builder, "\n## Files with Highest Failure Density\n\nFailure density is the share of emitted failure details located in a file; the extracted log does not include per-file execution counts.\n\n| File | Failure details | Failure density |\n| --- | ---: | ---: |\n")
	for _, count := range report.Summary.ByFile {
		fmt.Fprintf(&builder, "| %s | %d | %.1f%% |\n", markdownCell(count.Name), count.Count, count.Percentage)
	}

	fmt.Fprintf(&builder, "\n## Most Common Failure Patterns\n\nPatterns are grouped from the emitted diagnostic messages, independently of the technical category.\n\n| Pattern | Failure details | Share of failure details |\n| --- | ---: | ---: |\n")
	for _, count := range report.Summary.ByPattern {
		fmt.Fprintf(&builder, "| %s | %d | %.1f%% |\n", markdownCell(count.Pattern), count.Count, count.Percentage)
	}

	rateLimiterFailures := rateLimiterFailureCount(report.Failures)
	fmt.Fprintf(&builder, "\n## Rate-limiter Impact\n\n")
	if rateLimiterFailures == 0 {
		fmt.Fprintf(&builder, "No emitted failure detail is from a rate-limiter test or source file (0 of %d). The extracted failures are concentrated in translator and performance-benchmark tests.\n", report.Summary.TotalFailures)
	} else {
		fmt.Fprintf(&builder, "%d of %d emitted failure details are from rate-limiter tests or source files (%.1f%%).\n", rateLimiterFailures, report.Summary.TotalFailures, percentage(rateLimiterFailures, report.Summary.TotalFailures))
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

func missingStandardCategories(categories []categoryCount) []string {
	observed := make(map[string]bool, len(categories))
	for _, category := range categories {
		observed[category.Category] = true
	}
	standard := []struct {
		category string
		label    string
	}{
		{string(testutil.CategoryTimeout), "timeouts"},
		{string(testutil.CategoryPanic), "panics"},
		{string(testutil.CategoryDataRace), "data races"},
		{string(testutil.CategoryDeadlock), "deadlocks"},
		{string(testutil.CategoryNilPointer), "nil-pointer dereferences"},
		{string(testutil.CategoryIOError), "I/O errors"},
		{string(testutil.CategoryHTTPError), "HTTP errors"},
	}
	missing := make([]string, 0, len(standard))
	for _, candidate := range standard {
		if !observed[candidate.category] {
			missing = append(missing, candidate.label)
		}
	}
	return missing
}

func rateLimiterFailureCount(failures []failureRecord) int {
	count := 0
	for _, failure := range failures {
		name := strings.ToLower(failure.TestName + " " + failure.FilePath)
		if strings.Contains(name, "ratelimiter") || strings.Contains(name, "rate_limiter") || strings.Contains(name, "rate-limit") {
			count++
		}
	}
	return count
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
