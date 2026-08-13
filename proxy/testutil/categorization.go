package testutil

import (
	"fmt"
	"regexp"
	"strings"
)

// Test Failure Categorization System
//
// This package implements a comprehensive categorization system for test failures
// using a priority-based decision tree approach. The system categorizes failures
// into specific types (assertion errors, timeouts, panics, data races, etc.) with
// confidence scores and detailed reasoning.
//
// DECISION TREE APPROACH:
// Categories are determined by checking patterns in priority order (highest first).
// This ensures that specific, unambiguous patterns (data races, deadlocks) are
// checked before general patterns (assertion errors), providing consistent
// categorization even for ambiguous cases.
//
// See docs/notes/CATEGORIZATION_DECISION_TREE.md for:
// - Complete decision tree logic with step-by-step explanations
// - Category definitions with extensive examples
// - Ambiguous case handling with specific scenarios
// - Usage guidelines and implementation notes
//
// Key features:
// - Priority-based pattern matching (handles ambiguous cases)
// - Confidence scoring (0.0 to 1.0) indicates categorization certainty
// - Automatic reasoning generation explains why each category was chosen
// - Subcategories provide additional specificity (e.g., HTTP/network errors)
// - Comprehensive statistics and reporting for analysis

// FailureCategory represents the type of test failure
type FailureCategory string

const (
	// CategoryAssertionError indicates an assertion/expectation failure (e.g., "expected X, got Y")
	// Priority: 10 (lowest - checked last as fallback)
	// Confidence: 0.7 (general patterns can appear in various contexts)
	CategoryAssertionError FailureCategory = "assertion_error"

	// CategoryTimeout indicates a test timeout (context deadline, timeout waiting for, etc.)
	// Priority: 70 (checked early to catch timeout-related issues)
	// Confidence: 0.95 (clear timeout terminology)
	CategoryTimeout FailureCategory = "timeout"

	// CategoryPanic indicates a runtime panic (panic:.*, runtime panic, etc.)
	// Priority: 25 (checked after specific panic types like nil pointer, index errors)
	// Confidence: 1.0 (explicit panic markers)
	CategoryPanic FailureCategory = "panic"

	// CategoryDataRace indicates a data race detected by race detector
	// Priority: 100 (highest - most specific and critical)
	// Confidence: 1.0 (unambiguous race detector output)
	CategoryDataRace FailureCategory = "data_race"

	// CategoryNilPointer indicates a nil pointer dereference
	// Priority: 65 (checked before general panics for specificity)
	// Confidence: 1.0 (explicit nil pointer messages)
	CategoryNilPointer FailureCategory = "nil_pointer_dereference"

	// CategoryTypeMismatch indicates type conversion or type assertion failures
	// Priority: 45 (medium - specific but varied terminology)
	// Confidence: 0.9 (type errors have clear patterns)
	CategoryTypeMismatch FailureCategory = "type_mismatch"

	// CategoryIndexOutOfRange indicates array/slice index out of range
	// Priority: 60 (checked before general panics for specificity)
	// Confidence: 1.0 (explicit bounds checking messages)
	CategoryIndexOutOfRange FailureCategory = "index_out_of_range"

	// CategoryMapKey indicates map key access errors (key not found, zero key, etc.)
	// Priority: 55 (specific map operation errors)
	// Confidence: 0.95 (clear but varied key-related phrasing)
	CategoryMapKey FailureCategory = "map_key_error"

	// CategoryChannel indicates channel operation errors (send on closed channel, etc.)
	// Priority: 50 (concurrency-specific channel errors)
	// Confidence: 1.0 (explicit channel operation messages)
	CategoryChannel FailureCategory = "channel_error"

	// CategoryGoroutinePanic indicates goroutine panic (leaked goroutines, etc.)
	// Priority: 24 (checked before general panics for goroutine-specific issues)
	// Confidence: 0.9 (goroutine stack trace patterns)
	CategoryGoroutinePanic FailureCategory = "goroutine_panic"

	// CategoryDeadlock indicates potential deadlock detected
	// Priority: 90 (critical concurrency issue,仅次于 data races)
	// Confidence: 1.0 (explicit deadlock detection)
	CategoryDeadlock FailureCategory = "deadlock"

	// CategoryIOError indicates I/O related errors (file not found, connection refused, etc.)
	// Priority: 35 (general I/O operation failures)
	// Confidence: 0.9 (specific I/O error patterns)
	CategoryIOError FailureCategory = "io_error"

	// CategoryHTTPError indicates HTTP related errors (status codes, connection errors, etc.)
	// Priority: 40 (network-specific errors, checked before general I/O)
	// Confidence: 0.85 (network error variability)
	CategoryHTTPError FailureCategory = "http_error"

	// CategoryUnknown indicates failures that don't fit any known category
	// Priority: 0 (default when no patterns match)
	// Confidence: 0.0 (requires manual review)
	CategoryUnknown FailureCategory = "unknown"
)

// CategorizedFailure represents a test failure with its category
type CategorizedFailure struct {
	TestFailure
	Category      FailureCategory `json:"category"`
	Subcategory   string           `json:"subcategory,omitempty"`
	Confidence    float64           `json:"confidence"` // 0.0 to 1.0
	Reasoning     string            `json:"reasoning,omitempty"`
}

// CategorizationRule defines how to categorize failures
type CategorizationRule struct {
	Category    FailureCategory
	Pattern     *regexp.Regexp
	Subcategory string
	Priority    int // Higher priority rules checked first
	Confidence  float64
}

// categorizationRules defines the order and patterns for categorization
// This implements a priority-based decision tree for categorizing test failures.
// Rules are checked in priority order (highest first) to handle ambiguous cases.
// See docs/notes/CATEGORIZATION_DECISION_TREE.md for detailed decision tree documentation.
var categorizationRules = []CategorizationRule{
	// DECISION TREE STEP 1: Data race detection (Priority: 100)
	// Rationale: Data races are critical concurrency bugs with unique, unambiguous output
	// Checked first because race detector output is highly specific and takes precedence over all other errors
	{
		Category:   CategoryDataRace,
		Pattern:    regexp.MustCompile(`(?i)WARNING: DATA RACE|Write at|Previous|data race`),
		Priority:   100,
		Confidence: 1.0,
	},

	// DECISION TREE STEP 2: Deadlock detection (Priority: 90)
	// Rationale: Deadlocks are critical concurrency issues with explicit detection messages
	// Checked second because deadlock detection is specific and unambiguous
	{
		Category:   CategoryDeadlock,
		Pattern:    regexp.MustCompile(`(?i)potential deadlock|deadlock detected`),
		Priority:   90,
		Confidence: 1.0,
	},

	// DECISION TREE STEP 3: Timeout detection (Priority: 70)
	// Rationale: Timeouts have clear terminology but can appear in various contexts
	// Checked before specific error types to catch timeout-related issues early
	{
		Category:   CategoryTimeout,
		Pattern:    regexp.MustCompile(`(?i)context deadline exceeded|timeout.*exceeded|timed out|timeout waiting for|test timed out|exceeded.*timeout`),
		Priority:   70,
		Confidence: 0.95,
	},

	// DECISION TREE STEP 5: Nil pointer dereference (Priority: 65)
	// Rationale: Nil pointer errors have explicit, unambiguous messages
	// Checked before general panics to provide specific categorization
	{
		Category:   CategoryNilPointer,
		Pattern:    regexp.MustCompile(`(?i)null pointer|nil pointer dereference|nil pointer dereference|panic on nil pointer`),
		Priority:   65,
		Confidence: 1.0,
	},

	// DECISION TREE STEP 6: Index out of range (Priority: 60)
	// Rationale: Index errors have explicit messages that are easy to identify
	// Checked before general panics to distinguish specific panic types
	{
		Category:   CategoryIndexOutOfRange,
		Pattern:    regexp.MustCompile(`(?i)index out of range|slice bounds out of range`),
		Priority:   60,
		Confidence: 1.0,
	},

	// DECISION TREE STEP 7: Map key errors (Priority: 55)
	// Rationale: Map errors are specific but key-related phrasing can vary
	// Checked as a separate category for better debugging of map-related issues
	{
		Category:   CategoryMapKey,
		Pattern:    regexp.MustCompile(`(?i)map key.*not found|zero map key|key not found`),
		Priority:   55,
		Confidence: 0.95,
	},

	// DECISION TREE STEP 8: Channel errors (Priority: 50)
	// Rationale: Channel errors have explicit, unambiguous messages
	// Checked before type errors to catch concurrency-specific issues
	{
		Category:   CategoryChannel,
		Pattern:    regexp.MustCompile(`(?i)send on closed channel|close of closed channel|channel.*closed|receive on closed channel`),
		Priority:   50,
		Confidence: 1.0,
	},

	// DECISION TREE STEP 9: Goroutine panic detection (Priority: 55)
	// Rationale: Goroutine panics have distinct patterns ("goroutine [running]")
	// Checked before general panics to provide specific categorization
	{
		Category:    CategoryGoroutinePanic,
		Pattern:     regexp.MustCompile(`(?i)goroutine \d+ \[running\]|leaked goroutine|goroutines? created`),
		Priority:    55,
		Confidence:  0.9,
	},

	// DECISION TREE STEP 10: General panic detection (Priority: 50)
	// Rationale: Panics are explicit and should be checked before type errors
	// Moved up to prevent "panic: interface conversion" from matching type patterns
	{
		Category:    CategoryPanic,
		Pattern:     regexp.MustCompile(`(?i)\bpanic:|runtime panic|panic\(\)`),
		Subcategory: "runtime_panic",
		Priority:    50,
		Confidence:  1.0,
	},

	// DECISION TREE STEP 10: Type mismatch (Priority: 45)
	// Rationale: Type errors have clear patterns but terminology varies
	// Checked after panics to avoid misclassifying panic messages as type errors
	{
		Category:   CategoryTypeMismatch,
		Pattern:    regexp.MustCompile(`(?i)type.*interface|interface conversion|cannot convert.*type|type mismatch|type assertion`),
		Priority:   45,
		Confidence: 0.9,
	},

	// DECISION TREE STEP 11: HTTP/Network errors (Priority: 40)
	// Rationale: Network errors have clear patterns but overlap with other I/O errors
	// Checked before general I/O errors to distinguish network-specific issues
	{
		Category:   CategoryHTTPError,
		Pattern:    regexp.MustCompile(`(?i)http.*status|status code|connection refused|connection reset|http.*error|dial.*tcp|connection timeout`),
		Subcategory: "network",
		Priority:    40,
		Confidence:  0.85,
	},

	// DECISION TREE STEP 12: I/O errors (Priority: 35)
	// Rationale: I/O errors are specific but can vary by operation type
	// Checked as a general category for file system and device I/O issues
	{
		Category:   CategoryIOError,
		Pattern:    regexp.MustCompile(`(?i)no such file|directory.*not found|file not found|permission denied|i/o error|read.*failed|write.*failed`),
		Priority:   35,
		Confidence: 0.9,
	},

	// DECISION TREE STEP 12: General panic detection (Priority: 25)
	// Rationale: Panics are explicit but checked after specific panic types
	// Only matches if no specific error pattern matched (nil pointer, index, channel, etc.)
	{
		Category:    CategoryPanic,
		Pattern:     regexp.MustCompile(`(?i)\bpanic:|runtime panic|panic\(\)`),
		Subcategory: "runtime_panic",
		Priority:    25,
		Confidence:  1.0,
	},

	// DECISION TREE STEP 14: Assertion errors (Priority: 10)
	// Rationale: Assertion patterns are the most common but also the most general
	// Checked last as a fallback to avoid misclassifying specific errors as assertions
	// Lower confidence because assertion-like patterns can appear in various contexts
	{
		Category: CategoryAssertionError,
		Pattern:  regexp.MustCompile(`(?i)assertion.*failed|expected.*but.*got|not equal|should.*be|want.*got|expected.*got|assert`),
		Priority: 10,
		// Lower confidence because assertion-like patterns can appear in other contexts
		Confidence: 0.7,
	},
}

// CategorizeFailure categorizes a single test failure using the decision tree.
// This function implements the priority-based decision tree logic documented in
// docs/notes/CATEGORIZATION_DECISION_TREE.md.
//
// Algorithm:
// 1. Combine error message and stack trace for comprehensive analysis
// 2. Sort rules by priority (highest first) to implement decision tree order
// 3. Check each rule in priority order - first match wins (handles ambiguous cases)
// 4. Return categorized failure with reasoning and confidence score
// 5. If no patterns match, categorize as unknown for manual review
//
// Returns the categorized failure with reasoning explaining which pattern matched.
func CategorizeFailure(failure TestFailure) CategorizedFailure {
	// Combine error message and stack trace for analysis
	fullText := failure.ErrorMessage
	if failure.StackTrace != "" {
		fullText = fullText + "\n" + failure.StackTrace
	}

	// Sort rules by priority (highest first)
	sortedRules := make([]CategorizationRule, len(categorizationRules))
	copy(sortedRules, categorizationRules)

	// Check each rule in priority order
	for _, rule := range sortedRules {
		if rule.Pattern.MatchString(fullText) {
			return CategorizedFailure{
				TestFailure: failure,
				Category:    rule.Category,
				Subcategory: rule.Subcategory,
				Confidence:  rule.Confidence,
				Reasoning: fmt.Sprintf("Matched pattern for category '%s': pattern '%s' found in error message or stack trace",
					rule.Category, rule.Pattern.String()),
			}
		}
	}

	// If no pattern matched, categorize as unknown
	return CategorizedFailure{
		TestFailure: failure,
		Category:    CategoryUnknown,
		Confidence:  0.0,
		Reasoning:   "No categorization pattern matched; needs manual review",
	}
}

// CategorizeFailures categorizes a list of test failures using the decision tree.
// This function applies the priority-based decision tree to each failure and
// generates comprehensive statistics about the categorization results.
//
// Process:
// 1. Apply CategorizeFailure (decision tree) to each failure
// 2. Track statistics: total count, count by category, low confidence count
// 3. Return categorized failures and statistics for analysis
//
// The decision tree ensures consistent categorization across multiple failures
// by always checking rules in the same priority order.
//
// Returns a list of categorized failures with detailed statistics for analysis.
func CategorizeFailures(failures []TestFailure) ([]CategorizedFailure, CategorizationStats) {
	categorized := make([]CategorizedFailure, len(failures))
	stats := CategorizationStats{
		ByCategory: make(map[FailureCategory]int),
	}

	for i, failure := range failures {
		cat := CategorizeFailure(failure)
		categorized[i] = cat
		stats.Total++
		stats.ByCategory[cat.Category]++

		if cat.Confidence < 0.5 {
			stats.LowConfidence++
		}
	}

	return categorized, stats
}

// CategorizationStats provides statistics about categorized failures
type CategorizationStats struct {
	Total          int                         `json:"total"`
	ByCategory     map[FailureCategory]int    `json:"by_category"`
	LowConfidence  int                         `json:"low_confidence"`
}

// GetCategoryDescription returns a human-readable description for a category
func GetCategoryDescription(cat FailureCategory) string {
	descriptions := map[FailureCategory]string{
		CategoryAssertionError:      "Assertion or expectation failure (expected vs actual mismatch)",
		CategoryTimeout:             "Test exceeded timeout limit or context deadline",
		CategoryPanic:               "Runtime panic occurred",
		CategoryDataRace:            "Data race detected by race detector",
		CategoryNilPointer:          "Nil pointer dereference",
		CategoryTypeMismatch:        "Type conversion or assertion failure",
		CategoryIndexOutOfRange:     "Array or slice index out of bounds",
		CategoryMapKey:              "Map key access error",
		CategoryChannel:             "Channel operation error (closed channel, etc.)",
		CategoryGoroutinePanic:      "Goroutine panic or leak",
		CategoryDeadlock:            "Potential deadlock detected",
		CategoryIOError:             "I/O operation failed (file, network, etc.)",
		CategoryHTTPError:           "HTTP/network communication error",
		CategoryUnknown:             "Unknown failure type - requires manual analysis",
	}

	if desc, ok := descriptions[cat]; ok {
		return desc
	}
	return "No description available"
}

// PrintCategorizationReport prints a human-readable categorization report
func PrintCategorizationReport(categorized []CategorizedFailure, stats CategorizationStats) string {
	var sb strings.Builder

	sb.WriteString("=== Test Failure Categorization Report ===\n\n")
	sb.WriteString(fmt.Sprintf("Total failures: %d\n\n", stats.Total))

	// Print by category
	sb.WriteString("Failures by category:\n")
	for _, cat := range []FailureCategory{
		CategoryAssertionError,
		CategoryTimeout,
		CategoryPanic,
		CategoryDataRace,
		CategoryNilPointer,
		CategoryTypeMismatch,
		CategoryIndexOutOfRange,
		CategoryMapKey,
		CategoryChannel,
		CategoryGoroutinePanic,
		CategoryDeadlock,
		CategoryIOError,
		CategoryHTTPError,
		CategoryUnknown,
	} {
		count := stats.ByCategory[cat]
		if count > 0 {
			sb.WriteString(fmt.Sprintf("  %s: %d (%s)\n", cat, count, GetCategoryDescription(cat)))
		}
	}

	sb.WriteString(fmt.Sprintf("\nLow confidence categorizations: %d\n\n", stats.LowConfidence))

	// Print individual failures
	sb.WriteString("Individual failures:\n")
	for i, cat := range categorized {
		sb.WriteString(fmt.Sprintf("%d. [%s] %s (%.0f%% confidence)\n",
			i+1, cat.Category, cat.TestName, cat.Confidence*100))
		sb.WriteString(fmt.Sprintf("   File: %s:%d\n", cat.FilePath, cat.LineNumber))
		sb.WriteString(fmt.Sprintf("   Error: %s\n", cat.ErrorMessage))
		if cat.Reasoning != "" {
			sb.WriteString(fmt.Sprintf("   Reasoning: %s\n", cat.Reasoning))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
