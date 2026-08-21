package testutil

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// ErrUncategorizedFailures is returned when one or more failures did not
// match a categorization rule. Callers can use errors.Is to distinguish this
// from input, parsing, and report-writing errors.
var ErrUncategorizedFailures = errors.New("one or more failures are uncategorized")

// CategoryDistribution describes one category's share of a report.
type CategoryDistribution struct {
	Category   FailureCategory `json:"category"`
	Count      int             `json:"count"`
	Percentage float64         `json:"percentage"`
}

// CategorizationStatistics is the aggregate view of a categorization report.
// Distribution is ordered by count (then category name) so JSON reports are
// deterministic. MostCommonFailureTypes contains every category tied for the
// highest count, rather than selecting an arbitrary winner.
type CategorizationStatistics struct {
	Total                  int                     `json:"total"`
	Categorized            int                     `json:"categorized"`
	Uncategorized          int                     `json:"uncategorized"`
	ByCategory             map[FailureCategory]int `json:"by_category"`
	Distribution           []CategoryDistribution  `json:"distribution"`
	MostCommonFailureTypes []CategoryDistribution  `json:"most_common_failure_types"`
	LowConfidence          int                     `json:"low_confidence"`
	AmbiguousCases         int                     `json:"ambiguous_cases"`
}

// CategorizationReport contains every categorized test failure and the
// summary used to verify the result. ParseError is populated when the parser
// recovered failures from malformed or truncated input but could not consume
// the complete file.
type CategorizationReport struct {
	Source     string                   `json:"source,omitempty"`
	Failures   []CategorizedFailure     `json:"failures"`
	Statistics CategorizationStatistics `json:"statistics"`
	ParseError string                   `json:"parse_error,omitempty"`
}

// CategorizedFailureOutputFormat controls how a categorized failure report is
// rendered. The zero value is JSON so callers get a structured format without
// needing to set an option explicitly.
type CategorizedFailureOutputFormat string

const (
	CategorizedFailureOutputJSON CategorizedFailureOutputFormat = "json"
	CategorizedFailureOutputText CategorizedFailureOutputFormat = "text"
)

// CategorizedFailureOutputOptions controls a rendered report. Categories is
// optional: an empty slice includes every category, while a non-empty slice
// includes only failures whose selected category appears in the slice.
// Statistics in filtered output describe only the displayed failures.
type CategorizedFailureOutputOptions struct {
	Format     CategorizedFailureOutputFormat
	Categories []FailureCategory
}

// LabeledCategorizedFailure is the machine-readable categorization result
// augmented with the human-readable category label used by text reports.
// Category remains the stable value for programmatic filtering and grouping.
type LabeledCategorizedFailure struct {
	CategorizedFailure
	Label string `json:"label"`
}

// CategorizedFailureOutput is the structured value rendered as JSON or text.
// It contains labels on every failure and statistics for exactly the failures
// selected by CategorizedFailureOutputOptions.
type CategorizedFailureOutput struct {
	Source     string                      `json:"source,omitempty"`
	Failures   []LabeledCategorizedFailure `json:"failures"`
	Statistics CategorizationStatistics    `json:"statistics"`
	ParseError string                      `json:"parse_error,omitempty"`
}

// CategorizeParsedFailures applies CategorizeFailure to every parsed failure
// and calculates the complete aggregate report. It is useful when a caller
// already owns the raw test output and has parsed it independently.
func CategorizeParsedFailures(failures []TestFailure) CategorizationReport {
	categorized, _ := CategorizeFailures(failures)

	return CategorizationReport{
		Failures:   categorized,
		Statistics: categorizationStatistics(categorized),
	}
}

func categorizationStatistics(failures []CategorizedFailure) CategorizationStatistics {
	statistics := CategorizationStatistics{
		Total:        len(failures),
		ByCategory:   make(map[FailureCategory]int),
		Distribution: make([]CategoryDistribution, 0),
	}

	for _, failure := range failures {
		category := categorizedFailureCategory(failure)
		statistics.ByCategory[category]++
		if category == CategoryUnknown {
			statistics.Uncategorized++
		} else {
			statistics.Categorized++
		}
		if failure.Confidence <= 0.5 {
			statistics.LowConfidence++
		}
		if IsAmbiguous(failure) {
			statistics.AmbiguousCases++
		}
	}

	for category, count := range statistics.ByCategory {
		percentage := 0.0
		if statistics.Total > 0 {
			percentage = float64(count) * 100 / float64(statistics.Total)
		}
		statistics.Distribution = append(statistics.Distribution, CategoryDistribution{
			Category:   category,
			Count:      count,
			Percentage: percentage,
		})
	}

	sort.Slice(statistics.Distribution, func(i, j int) bool {
		if statistics.Distribution[i].Count != statistics.Distribution[j].Count {
			return statistics.Distribution[i].Count > statistics.Distribution[j].Count
		}
		return statistics.Distribution[i].Category < statistics.Distribution[j].Category
	})

	if len(statistics.Distribution) == 0 {
		statistics.MostCommonFailureTypes = []CategoryDistribution{}
		return statistics
	}

	mostCommonCount := statistics.Distribution[0].Count
	for _, distribution := range statistics.Distribution {
		if distribution.Count != mostCommonCount {
			break
		}
		statistics.MostCommonFailureTypes = append(statistics.MostCommonFailureTypes, distribution)
	}

	return statistics
}

func categorizedFailureCategory(failure CategorizedFailure) FailureCategory {
	if failure.Category != "" {
		return failure.Category
	}
	if failure.Type != "" {
		return failure.Type
	}
	return CategoryUnknown
}

// CategorizeTestOutput reads and parses a Go test-output file, then applies
// the categorization engine to every parsed failure. If the parser can recover
// failures from truncated output, they are returned with ParseError set and the
// parsing error is returned alongside the report.
func CategorizeTestOutput(inputPath string) (CategorizationReport, error) {
	output, err := ReadTestOutput(inputPath)
	if err != nil {
		return CategorizationReport{}, fmt.Errorf("read test output %q: %w", inputPath, err)
	}

	failures, parseErr := ParseTestFailures(output)
	report := CategorizeParsedFailures(failures)
	report.Source = inputPath
	if parseErr != nil {
		report.ParseError = parseErr.Error()
		return report, fmt.Errorf("parse test output %q: %w", inputPath, parseErr)
	}

	return report, nil
}

// VerifyCategorizedFailures ensures that the categorization engine did not
// leave any failure in the unknown category. It accepts a report directly so
// callers do not need to reconstruct the verification input from JSON.
func VerifyCategorizedFailures(report CategorizationReport) error {
	unknownTests := make([]string, 0, report.Statistics.Uncategorized)
	for _, failure := range report.Failures {
		if categorizedFailureCategory(failure) == CategoryUnknown {
			unknownTests = append(unknownTests, failure.TestName)
		}
	}

	if len(unknownTests) == 0 {
		return nil
	}

	return fmt.Errorf("%w: %d (%s)", ErrUncategorizedFailures, len(unknownTests), strings.Join(unknownTests, ", "))
}

// BuildCategorizedFailureOutput filters a report and enriches every displayed
// failure with its human-readable category label. It does not mutate report.
func BuildCategorizedFailureOutput(report CategorizationReport, options CategorizedFailureOutputOptions) CategorizedFailureOutput {
	categoryFilter := make(map[FailureCategory]struct{}, len(options.Categories))
	for _, category := range options.Categories {
		categoryFilter[category] = struct{}{}
	}

	failures := make([]LabeledCategorizedFailure, 0, len(report.Failures))
	for _, failure := range report.Failures {
		category := categorizedFailureCategory(failure)
		if len(categoryFilter) > 0 {
			if _, selected := categoryFilter[category]; !selected {
				continue
			}
		}

		failure.Category = category
		if failure.Type == "" {
			failure.Type = category
		}
		failures = append(failures, LabeledCategorizedFailure{
			CategorizedFailure: failure,
			Label:              GetCategoryLabel(failure),
		})
	}

	categorized := make([]CategorizedFailure, len(failures))
	for i, failure := range failures {
		categorized[i] = failure.CategorizedFailure
	}

	return CategorizedFailureOutput{
		Source:     report.Source,
		Failures:   failures,
		Statistics: categorizationStatistics(categorized),
		ParseError: report.ParseError,
	}
}

// FormatCategorizedFailureOutput renders a labeled categorization report.
// JSON is the default format. Text is deterministic and intended for terminal
// logs, while JSON is intended for CI artifacts and other tools.
func FormatCategorizedFailureOutput(report CategorizationReport, options CategorizedFailureOutputOptions) (string, error) {
	output := BuildCategorizedFailureOutput(report, options)

	switch options.Format {
	case "", CategorizedFailureOutputJSON:
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return "", fmt.Errorf("marshal categorized failure output: %w", err)
		}
		return string(data), nil
	case CategorizedFailureOutputText:
		return formatCategorizedFailureOutputText(output, options.Categories), nil
	default:
		return "", fmt.Errorf("unsupported categorized failure output format %q", options.Format)
	}
}

// WriteCategorizedFailureOutput writes a labeled, optionally filtered report
// to writer and returns the same structured result that was rendered.
func WriteCategorizedFailureOutput(writer io.Writer, report CategorizationReport, options CategorizedFailureOutputOptions) (CategorizedFailureOutput, error) {
	output := BuildCategorizedFailureOutput(report, options)
	if writer == nil {
		return output, errors.New("categorized failure output writer is nil")
	}

	rendered, err := FormatCategorizedFailureOutput(report, options)
	if err != nil {
		return output, err
	}
	if _, err := io.WriteString(writer, rendered); err != nil {
		return output, fmt.Errorf("write categorized failure output: %w", err)
	}

	return output, nil
}

// OutputCategorizedFailures is the parsed-failure-to-output workflow. It
// categorizes every supplied parsed failure, applies any requested category
// filter, renders the selected format, and returns the structured result.
func OutputCategorizedFailures(writer io.Writer, failures []TestFailure, options CategorizedFailureOutputOptions) (CategorizedFailureOutput, error) {
	return WriteCategorizedFailureOutput(writer, CategorizeParsedFailures(failures), options)
}

func formatCategorizedFailureOutputText(output CategorizedFailureOutput, categories []FailureCategory) string {
	var builder strings.Builder

	builder.WriteString("=== Test Failure Categorization Report ===\n")
	if output.Source != "" {
		fmt.Fprintf(&builder, "Source: %s\n", output.Source)
	}
	if len(categories) > 0 {
		labels := make([]string, len(categories))
		for i, category := range categories {
			labels[i] = categoryGroupLabel(category)
		}
		fmt.Fprintf(&builder, "Category filter: %s\n", strings.Join(labels, ", "))
	}
	if output.ParseError != "" {
		fmt.Fprintf(&builder, "Parse warning: %s\n", output.ParseError)
	}

	statistics := output.Statistics
	fmt.Fprintf(&builder, "\nTotal failures: %d\n", statistics.Total)
	fmt.Fprintf(&builder, "Categorized: %d\n", statistics.Categorized)
	fmt.Fprintf(&builder, "Uncategorized: %d\n", statistics.Uncategorized)
	fmt.Fprintf(&builder, "Low confidence: %d\n", statistics.LowConfidence)
	fmt.Fprintf(&builder, "Ambiguous cases: %d\n", statistics.AmbiguousCases)

	builder.WriteString("\nFailures by category:\n")
	if len(statistics.Distribution) == 0 {
		builder.WriteString("  none\n")
	} else {
		for _, distribution := range statistics.Distribution {
			fmt.Fprintf(&builder, "  %s: %d (%.2f%%)\n",
				categoryGroupLabel(distribution.Category), distribution.Count, distribution.Percentage)
		}
	}

	builder.WriteString("\nIndividual failures:\n")
	if len(output.Failures) == 0 {
		builder.WriteString("  none\n")
		return builder.String()
	}

	for i, failure := range output.Failures {
		fmt.Fprintf(&builder, "%d. [%s] %s (%.0f%% confidence)\n",
			i+1, failure.Label, failure.TestName, failure.Confidence.Float64()*100)
		if failure.FilePath != "" || failure.LineNumber != 0 {
			fmt.Fprintf(&builder, "   File: %s:%d\n", failure.FilePath, failure.LineNumber)
		}
		if failure.ErrorMessage != "" {
			fmt.Fprintf(&builder, "   Error: %s\n", failure.ErrorMessage)
		}
		if failure.Reasoning != "" {
			fmt.Fprintf(&builder, "   Reasoning: %s\n", failure.Reasoning)
		}
	}

	return builder.String()
}

// ExportCategorizationReportJSON writes the default complete JSON report for
// later verification. JSON failures include both the stable category value and
// a human-readable label.
func ExportCategorizationReportJSON(report CategorizationReport, outputPath string) error {
	rendered, err := FormatCategorizedFailureOutput(report, CategorizedFailureOutputOptions{
		Format: CategorizedFailureOutputJSON,
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(outputPath, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write categorization report %q: %w", outputPath, err)
	}
	return nil
}

// ProcessTestOutput is the file-to-report workflow. It reads the named output
// file with ReadTestOutput, parses and categorizes each failure, saves the
// report as JSON, and verifies that no result is unknown. Reports are written
// before verification errors are returned so an unexpected failure remains
// inspectable by a CI job or developer.
func ProcessTestOutput(inputPath, outputPath string) (CategorizationReport, error) {
	report, parseErr := CategorizeTestOutput(inputPath)
	if parseErr != nil && report.Source == "" {
		return report, parseErr
	}

	writeErr := ExportCategorizationReportJSON(report, outputPath)
	verifyErr := VerifyCategorizedFailures(report)
	return report, errors.Join(parseErr, writeErr, verifyErr)
}
