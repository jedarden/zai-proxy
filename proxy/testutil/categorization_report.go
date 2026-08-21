package testutil

import (
	"encoding/json"
	"errors"
	"fmt"
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

// CategorizeParsedFailures applies CategorizeFailure to every parsed failure
// and calculates the complete aggregate report. It is useful when a caller
// already owns the raw test output and has parsed it independently.
func CategorizeParsedFailures(failures []TestFailure) CategorizationReport {
	categorized, baseStats := CategorizeFailures(failures)

	statistics := CategorizationStatistics{
		Total:          baseStats.Total,
		ByCategory:     make(map[FailureCategory]int, len(baseStats.ByCategory)),
		Distribution:   make([]CategoryDistribution, 0, len(baseStats.ByCategory)),
		LowConfidence:  baseStats.LowConfidence,
		AmbiguousCases: baseStats.AmbiguousCases,
	}

	for category, count := range baseStats.ByCategory {
		statistics.ByCategory[category] = count
		if category == CategoryUnknown {
			statistics.Uncategorized += count
		} else {
			statistics.Categorized += count
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

	if len(statistics.Distribution) > 0 {
		mostCommonCount := statistics.Distribution[0].Count
		for _, distribution := range statistics.Distribution {
			if distribution.Count != mostCommonCount {
				break
			}
			statistics.MostCommonFailureTypes = append(statistics.MostCommonFailureTypes, distribution)
		}
	} else {
		statistics.MostCommonFailureTypes = []CategoryDistribution{}
	}

	return CategorizationReport{
		Failures:   categorized,
		Statistics: statistics,
	}
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
		if failure.Category == CategoryUnknown || failure.Type == CategoryUnknown {
			unknownTests = append(unknownTests, failure.TestName)
		}
	}

	if len(unknownTests) == 0 {
		return nil
	}

	return fmt.Errorf("%w: %d (%s)", ErrUncategorizedFailures, len(unknownTests), strings.Join(unknownTests, ", "))
}

// ExportCategorizationReportJSON writes a complete, human-readable report for
// later verification. It preserves both the categorized failures and their
// aggregate statistics in a single document.
func ExportCategorizationReportJSON(report CategorizationReport, outputPath string) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal categorization report: %w", err)
	}
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
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
