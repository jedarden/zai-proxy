package testutil

import (
	"fmt"
	"log"
	"math"
	"regexp"
	"sort"
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
// UNCERTAINTY DETECTION:
// The system includes an uncertainty threshold (0.7) that identifies categorizations
// that may need manual review. Categorizations with confidence below this
// threshold are flagged as uncertain.
//
// Example usage - Basic categorization with uncertainty check:
//
//	failures := []TestFailure{ /* ... */ }
//	categorized, stats := CategorizeFailures(failures)
//
//	for _, cat := range categorized {
//	    if cat.Uncertain {
//	        log.Printf("Uncertain categorization: %s (confidence: %.0f%%)",
//	            cat.Category, cat.Confidence.Float64()*100)
//	        // Flag for manual review or apply additional analysis
//	    }
//	}
//
// Example usage - Filter uncertain failures:
//
//	uncertainFailures := GetUncertainFailures(categorized)
//	if len(uncertainFailures) > 0 {
//	    log.Printf("Found %d uncertain categorizations requiring review", len(uncertainFailures))
//	}
//
// Example usage - Check uncertainty using Confidence method:
//
//	confidence := NewConfidence(0.65)
//	if confidence.IsUncertain() {
//	    // Confidence is 65%, below uncertainty threshold of 70%
//	    log.Printf("Low confidence: %.0f%%", confidence.Float64()*100)
//	}
//
// Example usage - Filter by custom uncertainty threshold:
//
//	// Use 0.8 as threshold instead of default 0.7
//	uncertainAt80 := GetUncertainFailures(categorized, 0.8)
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
// - Uncertainty detection (threshold 0.7) flags low-confidence categorizations
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
	// Priority: 50 (except an explicit panic marker takes precedence over a nil pointer match)
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

const (
	fallbackSubcategoryUnclassified        = "unclassified"
	fallbackSubcategoryEmptyFailure        = "empty_failure"
	fallbackSubcategoryMalformedOutput     = "malformed_output"
	fallbackSubcategoryUnknownPanicMessage = "unknown_panic_message"
	fallbackSubcategoryUnknownFatalMessage = "unknown_fatal_message"
)

// MatchSignalType represents the type of pattern match signal.
// Different signal types have different weights in confidence calculation.
type MatchSignalType string

const (
	// SignalExactMatch indicates an exact, unambiguous pattern match.
	// Examples: "DATA RACE", "panic:", "nil pointer dereference"
	// Weight: 1.0 (strongest signal)
	SignalExactMatch MatchSignalType = "exact_match"

	// SignalKeywordMatch indicates a clear keyword presence match.
	// Examples: "timeout", "refused", "permission denied"
	// Weight: 0.9 (strong signal)
	SignalKeywordMatch MatchSignalType = "keyword_match"

	// SignalContextualMatch indicates a pattern match with additional supporting context.
	// Examples: Multiple related keywords, stack trace context
	// Weight: 0.8 (moderate-strong signal)
	SignalContextualMatch MatchSignalType = "contextual_match"

	// SignalPartialMatch indicates a partial or ambiguous pattern match.
	// Examples: Generic patterns that could match multiple categories
	// Weight: 0.6 (weak signal)
	SignalPartialMatch MatchSignalType = "partial_match"

	// SignalInferred indicates an inference from surrounding context.
	// Examples: Category inferred from file path, test name
	// Weight: 0.4 (very weak signal)
	SignalInferred MatchSignalType = "inferred"
)

// MatchSignal represents a single pattern match signal with its type and context.
type MatchSignal struct {
	Type     MatchSignalType `json:"type"`
	Pattern  string          `json:"pattern"`
	Context  string          `json:"context,omitempty"`  // Additional context (e.g., stack trace, test name)
	Strength float64         `json:"strength,omitempty"` // Optional strength modifier (0.5 to 1.5)
}

// ConfidenceCalculationParams represents parameters for confidence calculation.
type ConfidenceCalculationParams struct {
	BaseConfidence   float64       `json:"base_confidence"`   // Starting confidence (0.0 to 1.0)
	Signals          []MatchSignal `json:"signals"`           // Match signals to weight
	AmbiguityPenalty float64       `json:"ambiguity_penalty"` // Penalty for ambiguous matches (0.0 to 1.0)
	ContextBoost     float64       `json:"context_boost"`     // Boost for strong supporting context (0.0 to 1.0)
}

// CalculateConfidence calculates a confidence score based on match signals.
//
// Algorithm:
// 1. Start with base confidence
// 2. Calculate signal contribution: for each signal, add its weighted strength
// 3. Normalize signal contribution to [0.0, 1.0]
// 4. Apply ambiguity penalty (reduces confidence for ambiguous cases)
// 5. Apply context boost (increases confidence for strong supporting context)
// 6. Clamp final result to [0.0, 1.0]
//
// Signal Weights:
// - ExactMatch: 1.0 (strongest - unambiguous pattern match)
// - KeywordMatch: 0.9 (strong - clear keyword presence)
// - ContextualMatch: 0.8 (moderate-strong - pattern with supporting context)
// - PartialMatch: 0.6 (weak - ambiguous or generic pattern)
// - Inferred: 0.4 (very weak - inference from context)
//
// Normalization:
// Signal contributions are normalized using a sigmoid-like function that
// prevents any single signal from dominating and ensures smooth scaling.
// The formula: normalized = contribution / (1.0 + contribution)
//
// Parameters:
// - params: ConfidenceCalculationParams containing base confidence and signals
//
// Returns a Confidence value in [0.0, 1.0] with type safety.
func CalculateConfidence(params ConfidenceCalculationParams) Confidence {
	if len(params.Signals) == 0 {
		return NewConfidence(params.BaseConfidence)
	}

	// Start with base confidence
	confidence := params.BaseConfidence

	// Calculate signal contribution
	signalContribution := 0.0
	signalWeights := map[MatchSignalType]float64{
		SignalExactMatch:      1.0,
		SignalKeywordMatch:    0.9,
		SignalContextualMatch: 0.8,
		SignalPartialMatch:    0.6,
		SignalInferred:        0.4,
	}

	for _, signal := range params.Signals {
		weight := signalWeights[signal.Type]
		strength := signal.Strength
		if strength == 0 {
			strength = 1.0 // Default strength
		}
		signalContribution += weight * strength
	}

	// Normalize signal contribution to [0.0, 1.0]
	// Using sigmoid-like normalization: contribution / (1.0 + contribution)
	normalizedContribution := signalContribution / (1.0 + signalContribution)

	// Add normalized contribution to base confidence
	confidence += normalizedContribution * 0.5 // Scale to prevent overconfidence

	// Apply ambiguity penalty (reduces confidence)
	if params.AmbiguityPenalty > 0 {
		confidence *= (1.0 - params.AmbiguityPenalty)
	}

	// Apply context boost (increases confidence)
	if params.ContextBoost > 0 {
		confidence += params.ContextBoost * (1.0 - confidence) // Boost approaches but doesn't exceed 1.0
	}

	// Clamp to [0.0, 1.0]
	return NewConfidence(confidence)
}

// Confidence represents a confidence score with bounded validation (0.0 to 1.0).
//
// CONFIDENCE SCORING APPROACH:
//
// The confidence scoring system quantifies categorization certainty using a weighted
// signal algorithm that considers multiple factors:
//
//  1. Base Confidence: Each category starts with a base confidence (0.0 to 1.0)
//     representing the inherent specificity of its patterns. Examples:
//     - Data race: 1.0 (unambiguous "WARNING: DATA RACE" marker)
//     - Timeout: 0.95 (clear terminology but various contexts)
//     - Assertion error: 0.7 (general patterns, high ambiguity)
//
// 2. Signal Weighting: Match signals contribute to confidence based on type:
//
//   - ExactMatch: 1.0 (strongest - "panic:", "DATA RACE")
//
//   - KeywordMatch: 0.9 (strong - "timeout", "refused")
//
//   - ContextualMatch: 0.8 (moderate-strong - pattern with supporting context)
//
//   - PartialMatch: 0.6 (weak - generic patterns)
//
//   - Inferred: 0.4 (very weak - inference from context only)
//
//     3. Ambiguity Penalty: When multiple patterns match, confidence is reduced based
//     on predefined ambiguity handlers. This prevents overconfidence in ambiguous cases.
//
//     4. Context Boost: Strong supporting context can increase confidence, but never
//     above 1.0 (the formula: boost * (1.0 - confidence) ensures this).
//
//     5. Normalization: Signal contributions are normalized using a sigmoid-like function
//     (contribution / (1.0 + contribution)) to prevent any single signal from dominating.
//
// INTERPRETING CONFIDENCE VALUES:
//
// - 0.95 - 1.0 (Very High): Unambiguous categorization, manual review not needed
// - 0.80 - 0.94 (High): High confidence, reliable for automation
// - 0.70 - 0.79 (Moderate): Acceptable confidence, flag for review if critical
// - 0.50 - 0.69 (Low): Uncertain, manual review recommended
// - 0.00 - 0.49 (Very Low): Very uncertain, manual review required
//
// The uncertainty threshold (0.7) was chosen because:
// - It captures cases where ambiguity penalties have significantly reduced confidence
// - It excludes high-confidence matches (0.8+) that are reliable for automation
// - It aligns with the "Moderate" level, providing clear semantic meaning
//
// Example usage:
//
//	conf := NewConfidence(0.65)
//	if conf.IsUncertain() {
//	    // Log warning: categorization confidence is 65%, below uncertainty threshold
//	    // Flag for manual review or apply additional analysis
//	}
//
// This type ensures that confidence values are always within valid range and
// provides type safety for confidence-based operations.
type Confidence float64

// Confidence constants for common threshold values
const (
	ConfidenceMin      Confidence = 0.0 // Minimum possible confidence (unknown/no match)
	ConfidenceMax      Confidence = 1.0 // Maximum possible confidence (unambiguous)
	ConfidenceLow      Confidence = 0.5 // Low confidence threshold
	ConfidenceModerate Confidence = 0.7 // Moderate confidence threshold (uncertain below this)
	// UncertainThreshold is the confidence level below which categorizations
	// are considered uncertain and may need manual review. A value of 0.7
	// means categorizations with less than 70% confidence are flagged.
	// This threshold balances false positives (low threshold) vs missed issues (high threshold).
	UncertainThreshold Confidence = 0.7
	ConfidenceHigh     Confidence = 0.8  // High confidence threshold
	ConfidenceVeryHigh Confidence = 0.95 // Very high confidence threshold
)

// NewConfidence creates a new Confidence value with bounds validation.
// Values outside [0.0, 1.0], including NaN, are clamped to the nearest bound.
func NewConfidence(value float64) Confidence {
	if math.IsNaN(value) {
		return ConfidenceMin
	}
	if value < 0.0 {
		return ConfidenceMin
	}
	if value > 1.0 {
		return ConfidenceMax
	}
	return Confidence(value)
}

// Float64 returns the confidence value as a float64 for backward compatibility.
func (c Confidence) Float64() float64 {
	return float64(c)
}

// IsCertain returns true if confidence is at or above the uncertainty threshold (0.7).
//
// Use this method to identify categorizations that are reliable for automation.
// Categorizations with confidence at or above 0.7 are considered certain enough to use
// without manual review in most scenarios.
//
// Example usage:
//
//	confidence := NewConfidence(0.85)
//	if confidence.IsCertain() {
//	    // Confidence is 85%, at or above uncertainty threshold of 70%
//	    // Safe to use for automation without manual review
//	}
//
// This method uses >= (greater than or equal), so a confidence of exactly 0.7 is
// considered certain. This provides an inclusive boundary where the threshold value
// itself is acceptable for automation.
func (c Confidence) IsCertain() bool {
	return c >= UncertainThreshold
}

// IsUncertain returns true if confidence is below the uncertainty threshold (0.7).
//
// Use this method to identify categorizations that may need manual review or additional
// analysis. The uncertainty threshold balances between catching potential issues and
// avoiding excessive false positives.
//
// Example usage:
//
//	confidence := NewConfidence(0.65)
//	if confidence.IsUncertain() {
//	    // Confidence is 65%, below uncertainty threshold of 70%
//	    // Flag for manual review or apply additional analysis
//	    log.Printf("Low confidence categorization: %.0f%%", confidence.Float64()*100)
//	}
//
// The method uses < (less than), so a confidence of exactly 0.7 is not uncertain.
// This makes the uncertainty and certainty predicates complementary at the boundary.
func (c Confidence) IsUncertain() bool {
	return c < UncertainThreshold
}

// NeedsManualReview returns true if confidence suggests manual review is needed (<= 0.5).
func (c Confidence) NeedsManualReview() bool {
	return c <= ConfidenceLow
}

// Level returns a human-readable confidence level.
func (c Confidence) Level() string {
	switch {
	case c >= ConfidenceVeryHigh:
		return "Very High"
	case c >= ConfidenceHigh:
		return "High"
	case c >= ConfidenceModerate:
		return "Moderate"
	case c >= ConfidenceLow:
		return "Low"
	default:
		return "Very Low"
	}
}

// CategorizedFailure represents a test failure with its category, confidence,
// and uncertainty flag. This is the primary result type returned by the
// categorization system.
//
// Example usage:
//
//	failure := CategorizeFailure(testFailure)
//	if failure.Uncertain {
//	    log.Printf("Uncertain categorization: %s (%.0f%% confidence)",
//	        failure.Category, failure.Confidence.Float64()*100)
//	    // Consider manual review or additional analysis
//	}
//
//	if failure.Confidence.IsUncertain() {
//	    // Alternative: Check uncertainty using the Confidence method
//	    // This is equivalent to checking the Uncertain field
//	}
type CategorizedFailure struct {
	TestFailure
	// Type is the category selected by CategorizeFailure. It is the concise
	// result field used by the Failure/Category API.
	Type FailureCategory `json:"type"`
	// Category is retained for compatibility with callers that used the
	// original, more detailed CategorizedFailure API. It always equals Type
	// for values produced by this package.
	Category    FailureCategory `json:"category"`
	Subcategory string          `json:"subcategory,omitempty"` // Optional subcategory for specificity
	// Confidence is the categorization confidence score (0.0 to 1.0).
	// Use the IsUncertain() method to check if this score is below the
	// uncertainty threshold (0.7), or access the Uncertain field directly.
	Confidence Confidence `json:"confidence"`
	// Uncertain is true if confidence is below the uncertainty threshold (0.7).
	// This field is automatically computed during categorization.
	// Categorizations with Uncertain=true may need manual review.
	// Use Confidence.IsUncertain() for the same check in a fluent style.
	Uncertain bool   `json:"uncertain"`           // true if confidence < 0.7
	Reasoning string `json:"reasoning,omitempty"` // Human-readable explanation of categorization
}

// Failure is the input accepted by CategorizeFailure.
//
// It aliases TestFailure so existing parser callers and the concise
// categorization API use the same representation.
type Failure = TestFailure

// Category is the result returned by CategorizeFailure.
//
// Category.Type and Category.Subcategory provide the compact categorization
// result, while the embedded failure and confidence fields remain available to
// existing consumers.
type Category = CategorizedFailure

// CategorizationRule defines how to categorize failures.
// The BaseConfidence represents the initial confidence score (0.0 to 1.0)
// for this categorization rule, which may be adjusted based on ambiguity detection
// and edge case handling.
type CategorizationRule struct {
	Category       FailureCategory
	Pattern        *regexp.Regexp
	Subcategory    string
	Priority       int     // Higher priority rules checked first
	BaseConfidence float64 // Base confidence for this rule (0.0 to 1.0)
	// AmbiguityHandlers defines rules to adjust confidence when ambiguous with other categories
	AmbiguityHandlers map[FailureCategory]ConfidenceAdjustment
}

// ConfidenceAdjustment defines how to adjust confidence when this rule
// could be confused with another category
type ConfidenceAdjustment struct {
	// ReduceBaseBy reduces confidence when both patterns match (0.0 to 1.0)
	ReduceBaseBy float64
	// Reason explains why confidence was reduced
	Reason string
}

var (
	// explicitPanicLinePattern recognizes a runtime panic marker rather than a
	// diagnostic string merely quoted by an assertion failure. A panic marker
	// emitted by Go starts a line in the failure output.
	explicitPanicLinePattern = regexp.MustCompile(`(?mi)^\s*(?:panic:|runtime panic|runtime error:)`)

	// assertionExpectationPattern identifies a test assertion that compares an
	// expected value with an actual value. It is deliberately stricter than the
	// assertion categorization rule because it is only used to prevent a quoted
	// diagnostic (for example, "panic:") from winning by regex priority.
	assertionExpectationPattern = regexp.MustCompile(`(?is)\b(?:assertion\s+failed|expected|want)\b.*\b(?:got|actual)\b`)

	// assertionDiagnosticValuePattern requires the diagnostic to occur in an
	// expected or actual value. This keeps a real nil-pointer failure followed
	// by an assertion message from being mistaken for a quoted diagnostic.
	assertionDiagnosticValuePattern = regexp.MustCompile(`(?is)\b(?:expected|want|got|actual)\b[^\n]{0,80}\b(?:panic:|nil pointer|context deadline exceeded|context canceled)`)
)

// categorizationRules defines the order and patterns for categorization
// This implements a priority-based decision tree for categorizing test failures.
// Rules are checked in priority order (highest first) to handle ambiguous cases.
// See docs/notes/CATEGORIZATION_DECISION_TREE.md for detailed decision tree documentation.
var categorizationRules = []CategorizationRule{
	// DECISION TREE STEP 1: Data race detection (Priority: 100)
	// Rationale: Data races are critical concurrency bugs with unique, unambiguous output
	// Checked first because race detector output is highly specific and takes precedence over all other errors
	{
		Category:       CategoryDataRace,
		Pattern:        regexp.MustCompile(`(?i)WARNING: DATA RACE|Write at|Previous|data race`),
		Priority:       100,
		BaseConfidence: 1.0,
		AmbiguityHandlers: map[FailureCategory]ConfidenceAdjustment{
			CategoryAssertionError: {ReduceBaseBy: 0.0, Reason: "Data race detector output is unambiguous, even if assertion text appears"},
		},
	},

	// DECISION TREE STEP 2: Deadlock detection (Priority: 90)
	// Rationale: Deadlocks are critical concurrency issues with explicit detection messages
	// Checked second because deadlock detection is specific and unambiguous
	{
		Category:       CategoryDeadlock,
		Pattern:        regexp.MustCompile(`(?i)potential deadlock|deadlock detected`),
		Priority:       90,
		BaseConfidence: 1.0,
		AmbiguityHandlers: map[FailureCategory]ConfidenceAdjustment{
			CategoryAssertionError: {ReduceBaseBy: 0.0, Reason: "Deadlock detection is unambiguous"},
		},
	},

	// DECISION TREE STEP 3: Timeout detection (Priority: 70)
	// Rationale: Timeouts have clear terminology but can appear in various contexts
	// Checked before specific error types to catch timeout-related issues early
	{
		Category:       CategoryTimeout,
		Pattern:        regexp.MustCompile(`(?i)context deadline exceeded|context canceled|timeout.*exceeded|timed out|timeout waiting for|timeout occurred|test timed out|exceeded.*timeout`),
		Priority:       70,
		BaseConfidence: 0.95,
		AmbiguityHandlers: map[FailureCategory]ConfidenceAdjustment{
			CategoryHTTPError: {ReduceBaseBy: 0.15, Reason: "Connection timeout could be HTTP error or general timeout"},
			CategoryIOError:   {ReduceBaseBy: 0.2, Reason: "Timeout during I/O operation could be I/O error"},
		},
	},

	// DECISION TREE STEP 4: Nil pointer dereference (Priority: 65)
	// Rationale: Nil pointer errors have explicit, unambiguous messages
	// Checked before general panics to provide specific categorization
	{
		Category:       CategoryNilPointer,
		Pattern:        regexp.MustCompile(`(?i)null pointer|nil pointer dereference|panic on nil pointer|assignment to entry in nil map`),
		Priority:       65,
		BaseConfidence: 1.0,
		AmbiguityHandlers: map[FailureCategory]ConfidenceAdjustment{
			CategoryPanic:          {ReduceBaseBy: 0.1, Reason: "Nil pointer dereference causes panic, but is more specific"},
			CategoryAssertionError: {ReduceBaseBy: 0.3, Reason: "Nil pointer could appear in assertion message text"},
		},
	},

	// DECISION TREE STEP 5: Index out of range (Priority: 60)
	// Rationale: Index errors have explicit messages that are easy to identify
	// Checked before general panics to distinguish specific panic types
	{
		Category:       CategoryIndexOutOfRange,
		Pattern:        regexp.MustCompile(`(?i)index out of range|slice bounds out of range`),
		Priority:       60,
		BaseConfidence: 1.0,
		AmbiguityHandlers: map[FailureCategory]ConfidenceAdjustment{
			CategoryPanic:          {ReduceBaseBy: 0.1, Reason: "Index out of range causes panic, but is more specific"},
			CategoryAssertionError: {ReduceBaseBy: 0.3, Reason: "Index error could appear in assertion message text"},
		},
	},

	// DECISION TREE STEP 6: Map key errors (Priority: 55)
	// Rationale: Map errors are specific but key-related phrasing can vary
	// Checked as a separate category for better debugging of map-related issues
	{
		Category:       CategoryMapKey,
		Pattern:        regexp.MustCompile(`(?i)map key.*not found|zero map key|key not found`),
		Priority:       55,
		BaseConfidence: 0.95,
		AmbiguityHandlers: map[FailureCategory]ConfidenceAdjustment{
			CategoryPanic:          {ReduceBaseBy: 0.15, Reason: "Map key error causes panic, but is more specific"},
			CategoryAssertionError: {ReduceBaseBy: 0.3, Reason: "Key not found could appear in assertion message text"},
		},
	},

	// DECISION TREE STEP 8: Goroutine panic detection (Priority: 55)
	// Rationale: Goroutine panics have distinct patterns ("goroutine [running]")
	// Checked before general panics to provide specific categorization
	{
		Category:       CategoryGoroutinePanic,
		Pattern:        regexp.MustCompile(`(?i)goroutine \d+ \[running\]|leaked goroutine|goroutines? created`),
		Priority:       55,
		BaseConfidence: 0.9,
		AmbiguityHandlers: map[FailureCategory]ConfidenceAdjustment{
			CategoryPanic:    {ReduceBaseBy: 0.2, Reason: "Goroutine panic is a type of panic, checked first for specificity"},
			CategoryDeadlock: {ReduceBaseBy: 0.3, Reason: "Goroutine issues could be related to deadlock"},
		},
	},

	// DECISION TREE STEP 7: Channel errors (Priority: 56)
	// Rationale: Channel errors have explicit, unambiguous messages
	// Checked before type errors to catch concurrency-specific issues
	{
		Category:       CategoryChannel,
		Pattern:        regexp.MustCompile(`(?i)send on closed channel|close of closed channel|channel.*closed|receive on closed channel`),
		Priority:       56,
		BaseConfidence: 1.0,
		AmbiguityHandlers: map[FailureCategory]ConfidenceAdjustment{
			CategoryPanic:          {ReduceBaseBy: 0.05, Reason: "Channel operation causes panic, but is more specific"},
			CategoryGoroutinePanic: {ReduceBaseBy: 0.1, Reason: "Channel errors often involve goroutines"},
		},
	},

	// DECISION TREE STEP 9: General panic detection (Priority: 50)
	// Rationale: Panics are explicit and should be checked before type errors
	// Moved up to prevent "panic: interface conversion" from matching type patterns
	{
		Category:       CategoryPanic,
		Pattern:        regexp.MustCompile(`(?i)\bpanic:|runtime panic|panic\(\)|\bpanic in\b|runtime error`),
		Subcategory:    "runtime_panic",
		Priority:       50,
		BaseConfidence: 1.0,
		AmbiguityHandlers: map[FailureCategory]ConfidenceAdjustment{
			CategoryTypeMismatch:   {ReduceBaseBy: 0.2, Reason: "Panic about type conversion vs. type mismatch"},
			CategoryAssertionError: {ReduceBaseBy: 0.3, Reason: "Panic could appear in assertion message text"},
		},
	},

	// DECISION TREE STEP 10: Type mismatch (Priority: 45)
	// Rationale: Type errors have clear patterns but terminology varies
	// Checked after panics to avoid misclassifying panic messages as type errors
	{
		Category:       CategoryTypeMismatch,
		Pattern:        regexp.MustCompile(`(?i)type.*interface|interface conversion|cannot convert.*type|type mismatch|type assertion`),
		Priority:       45,
		BaseConfidence: 0.9,
		AmbiguityHandlers: map[FailureCategory]ConfidenceAdjustment{
			CategoryPanic:          {ReduceBaseBy: 0.25, Reason: "Type conversion panic vs. type mismatch"},
			CategoryAssertionError: {ReduceBaseBy: 0.35, Reason: "Type errors often appear in assertion messages"},
		},
	},

	// DECISION TREE STEP 11: HTTP/Network errors (Priority: 40)
	// Rationale: Network errors have clear patterns but overlap with other I/O errors
	// Checked before general I/O errors to distinguish network-specific issues
	{
		Category:       CategoryHTTPError,
		Pattern:        regexp.MustCompile(`(?i)http.*status|status code|connection refused|connection reset|http.*error|dial.*tcp|connection timeout`),
		Subcategory:    "network",
		Priority:       40,
		BaseConfidence: 0.85,
		AmbiguityHandlers: map[FailureCategory]ConfidenceAdjustment{
			CategoryTimeout: {ReduceBaseBy: 0.3, Reason: "Connection timeout could be HTTP error or timeout"},
			CategoryIOError: {ReduceBaseBy: 0.2, Reason: "Network errors are a type of I/O error"},
		},
	},

	// DECISION TREE STEP 12: I/O errors (Priority: 35)
	// Rationale: I/O errors are specific but can vary by operation type
	// Checked as a general category for file system and device I/O issues
	{
		Category:       CategoryIOError,
		Pattern:        regexp.MustCompile(`(?i)no such file|directory.*not found|file not found|permission denied|i/o error|read.*failed|write.*failed|broken pipe`),
		Priority:       35,
		BaseConfidence: 0.9,
		AmbiguityHandlers: map[FailureCategory]ConfidenceAdjustment{
			CategoryHTTPError: {ReduceBaseBy: 0.2, Reason: "File I/O errors could be network-related"},
			CategoryTimeout:   {ReduceBaseBy: 0.25, Reason: "I/O operations can timeout"},
		},
	},

	// DECISION TREE STEP 13: Assertion errors (Priority: 10)
	// Rationale: Assertion patterns are the most common but also the most general
	// Checked last as a fallback to avoid misclassifying specific errors as assertions
	// Lower confidence because assertion-like patterns can appear in various contexts
	{
		Category:       CategoryAssertionError,
		Pattern:        regexp.MustCompile(`(?i)assertion.*failed|expected.*(?:but.*got|got|non-nil|to exist|value at|element at|type)|not equal|should.*be|want.*got|assert`),
		Priority:       10,
		BaseConfidence: 0.7,
		// No ambiguity handlers needed - this is the fallback category
	},
}

// CategorizeFailure categorizes a single test failure using the decision tree.
// This function implements the priority-based decision tree logic documented in
// docs/notes/CATEGORIZATION_DECISION_TREE.md with enhanced ambiguity detection.
//
// Algorithm:
// 1. Combine error message and stack trace for comprehensive analysis
// 2. Sort rules by priority (highest first) to implement decision tree order
// 3. Find ALL matching patterns (not just first) to detect ambiguity
// 4. If multiple patterns match, adjust confidence based on ambiguity handlers
// 5. Apply confidence penalties for ambiguous cases
// 6. Return categorized failure with detailed reasoning and confidence score
// 7. If no patterns match, categorize as unknown for manual review
//
// Returns the categorized failure with reasoning explaining which pattern matched
// and whether ambiguity was detected.
func CategorizeFailure(failure Failure) Category {
	// Combine error message and stack trace for analysis
	fullText := failure.ErrorMessage
	if failure.StackTrace != "" {
		fullText = fullText + "\n" + failure.StackTrace
	}

	// Sort rules by priority (highest first). Keeping this explicit makes the
	// result independent of the declaration order of categorizationRules.
	sortedRules := sortedCategorizationRules()

	matchingRules := matchingCategorizationRules(fullText, sortedRules)

	// If every category rule (and therefore every ambiguity resolver) has no
	// applicable signal, preserve the failure as an explicit Other result.
	if len(matchingRules) == 0 {
		return categorizeFallback(failure, fullText)
	}

	// Primary category is the highest priority match. An explicit panic is the
	// exception: when its message also reports a nil pointer, the panic is the
	// primary failure event and takes precedence over the derived nil-pointer
	// category.
	primaryRule := primaryCategorizationRule(matchingRules)
	confidence := primaryRule.BaseConfidence // Use float64 for calculations

	// Build reasoning
	reasoning := fmt.Sprintf("Matched pattern for category '%s': pattern '%s' found in error message or stack trace",
		primaryRule.Category, primaryRule.Pattern.String())

	// Check for ambiguity and adjust confidence
	if len(matchingRules) > 1 {
		for _, matched := range matchingRules {
			if matched.Category == primaryRule.Category {
				continue
			}

			// Check if primary rule has ambiguity handler for this category
			if adjustment, exists := primaryRule.AmbiguityHandlers[matched.Category]; exists {
				confidence -= adjustment.ReduceBaseBy
				if confidence < 0.1 {
					confidence = 0.1 // Minimum confidence floor
				}
				reasoning += fmt.Sprintf("\n  Ambiguity detected: also matches '%s' (priority %d) - confidence reduced by %.2f: %s",
					matched.Category, matched.Priority, adjustment.ReduceBaseBy, adjustment.Reason)
			} else {
				// Default confidence penalty for unhandled ambiguity
				confidence -= 0.15
				if confidence < 0.2 {
					confidence = 0.2
				}
				reasoning += fmt.Sprintf("\n  Ambiguity detected: also matches '%s' (priority %d) - default confidence penalty applied",
					matched.Category, matched.Priority)
			}
		}

		// Add ambiguity summary
		reasoning += fmt.Sprintf("\n  Total matching patterns: %d (ambiguous case)", len(matchingRules))
	}

	// Special edge case handling
	confidence = applyEdgeCaseAdjustments(fullText, primaryRule.Category, confidence)

	// Convert to Confidence type with bounds validation
	finalConfidence := NewConfidence(confidence)
	subcategory := primaryRule.Subcategory
	if primaryRule.Category == CategoryTimeout {
		subcategory = timeoutSubcategory(fullText)
	}
	if primaryRule.Category == CategoryNilPointer &&
		(strings.Contains(strings.ToLower(fullText), "setup") ||
			strings.Contains(strings.ToLower(fullText), "mock") ||
			strings.Contains(strings.ToLower(fullText), "fake")) {
		subcategory = "test_setup"
	}

	return CategorizedFailure{
		TestFailure: failure,
		Type:        primaryRule.Category,
		Category:    primaryRule.Category,
		Subcategory: subcategory,
		Confidence:  finalConfidence,
		Uncertain:   finalConfidence.IsUncertain(),
		Reasoning:   reasoning,
	}
}

// categorizeFallback creates the terminal Other result for a failure whose
// combined error message and stack trace match no known category. The log
// deliberately records only stable metadata, never the raw failure text.
func categorizeFallback(failure Failure, fullText string) CategorizedFailure {
	subcategory := fallbackSubcategory(fullText)
	confidence := 0.0
	if len(fullText) < 20 {
		confidence = 0.05
	}

	log.Printf("test failure categorization fallback: category=Other subcategory=%q test=%q file=%q line=%d",
		subcategory, failure.TestName, failure.FilePath, failure.LineNumber)

	return CategorizedFailure{
		TestFailure: failure,
		Type:        CategoryUnknown,
		Category:    CategoryUnknown,
		Subcategory: subcategory,
		Confidence:  NewConfidence(confidence),
		Uncertain:   true,
		Reasoning:   fmt.Sprintf("No categorization pattern matched; fallback category %q requires manual review", subcategory),
	}
}

func fallbackSubcategory(fullText string) string {
	lowerText := strings.ToLower(strings.TrimSpace(fullText))

	switch {
	case lowerText == "":
		return fallbackSubcategoryEmptyFailure
	case strings.Contains(lowerText, "malformed") ||
		strings.Contains(lowerText, "truncated") ||
		strings.Contains(lowerText, "parse error"):
		return fallbackSubcategoryMalformedOutput
	case strings.Contains(lowerText, "panic"):
		return fallbackSubcategoryUnknownPanicMessage
	case strings.Contains(lowerText, "fatal"):
		return fallbackSubcategoryUnknownFatalMessage
	default:
		return fallbackSubcategoryUnclassified
	}
}

func matchingCategorizationRules(fullText string, rules []CategorizationRule) []CategorizationRule {
	matchingRules := make([]CategorizationRule, 0, len(rules))
	for _, rule := range rules {
		if rule.Pattern.MatchString(fullText) {
			matchingRules = append(matchingRules, rule)
		}
	}

	// A safe type assertion is deliberately checking its boolean result. Its
	// wording should not also be treated as a test-framework assertion failure.
	lowerText := strings.ToLower(fullText)
	if strings.Contains(lowerText, "type assertion") && strings.Contains(lowerText, ", ok") {
		matchingRules = removeCategory(matchingRules, CategoryAssertionError)
	}

	return matchingRules
}

func removeCategory(rules []CategorizationRule, category FailureCategory) []CategorizationRule {
	filtered := rules[:0]
	for _, rule := range rules {
		if rule.Category != category {
			filtered = append(filtered, rule)
		}
	}
	return filtered
}

func primaryCategorizationRule(matchingRules []CategorizationRule) CategorizationRule {
	primary := matchingRules[0]
	if primary.Category != CategoryNilPointer {
		return primary
	}

	for _, rule := range matchingRules {
		if rule.Category == CategoryPanic {
			return rule
		}
	}

	return primary
}

func sortedCategorizationRules() []CategorizationRule {
	rules := make([]CategorizationRule, len(categorizationRules))
	copy(rules, categorizationRules)
	sort.SliceStable(rules, func(i, j int) bool {
		return rules[i].Priority > rules[j].Priority
	})
	return rules
}

// applyEdgeCaseAdjustments applies special confidence adjustments for known edge cases
// that don't fit the standard ambiguity handler pattern
func applyEdgeCaseAdjustments(fullText string, category FailureCategory, baseConfidence float64) float64 {
	confidence := baseConfidence
	lowerText := strings.ToLower(fullText)

	// Edge case: Panic with interface conversion text could be type mismatch
	if category == CategoryPanic && strings.Contains(lowerText, "interface conversion") {
		confidence -= 0.15
		if confidence < 0.5 {
			confidence = 0.5
		}
	}

	// Edge case: Assignment to entry in nil map is definitely nil pointer, not map key error
	if category == CategoryMapKey && strings.Contains(lowerText, "assignment to entry in nil map") {
		// This should have been caught by nil pointer pattern, but if it wasn't,
		// reduce confidence as it's actually a nil pointer issue
		confidence -= 0.4
		if confidence < 0.3 {
			confidence = 0.3
		}
	}

	// Edge case: "panic on nil pointer" vs general nil pointer
	if category == CategoryNilPointer && strings.Contains(fullText, "panic on nil pointer") {
		// This is explicit - high confidence
		confidence = 1.0
	}

	// Edge case: Multiple panic types in same error
	panicCount := strings.Count(lowerText, "panic")
	if panicCount > 1 && category == CategoryPanic {
		// Multiple panics suggest complex failure - slightly reduce confidence
		confidence -= 0.05
		if confidence < 0.7 {
			confidence = 0.7
		}
	}

	// Edge case: Connection timeout with "dial tcp" - likely HTTP error
	if (strings.Contains(lowerText, "connection timeout") || strings.Contains(lowerText, "connection timed out")) &&
		strings.Contains(lowerText, "dial") && strings.Contains(lowerText, "tcp") &&
		category == CategoryTimeout {
		// This is probably an HTTP error, not a general timeout
		confidence -= 0.2
		if confidence < 0.5 {
			confidence = 0.5
		}
	}

	// NEW: Edge case: "close of closed channel" in concurrent context - could be race condition
	if category == CategoryChannel && strings.Contains(lowerText, "close of closed channel") {
		// This often indicates a race condition - slightly reduce confidence
		confidence -= 0.1
		if confidence < 0.8 {
			confidence = 0.8
		}
	}

	// NEW: Edge case: Context cancellation vs timeout
	if category == CategoryTimeout && strings.Contains(lowerText, "context canceled") {
		// Context cancellation is more specific than general timeout - boost confidence
		confidence = 1.0
	}

	// NEW: Edge case: "nil pointer dereference" in testing context
	if category == CategoryNilPointer && strings.Contains(lowerText, "test") &&
		(strings.Contains(lowerText, "mock") || strings.Contains(lowerText, "fake")) {
		// Nil pointer in test with mock/fake context suggests test setup issue - moderate confidence
		confidence -= 0.1
		if confidence < 0.8 {
			confidence = 0.8
		}
	}

	// NEW: Edge case: Type assertion with "ok" pattern (safe type assertion)
	if category == CategoryTypeMismatch && strings.Contains(lowerText, ", ok") {
		// Safe type assertion that failed - high confidence this is type mismatch
		confidence = 1.0
	}

	// NEW: Edge case: Index out of range with specific array access pattern
	if category == CategoryIndexOutOfRange && strings.Contains(lowerText, "slice bounds") {
		// Slice bounds error is very specific - maximum confidence
		confidence = 1.0
	}

	// NEW: Edge case: Multiple goroutine panics suggesting broader concurrency issue
	if category == CategoryGoroutinePanic && strings.Count(lowerText, "goroutine") > 3 {
		// Many goroutines suggest systemic issue - slightly reduce confidence
		confidence -= 0.1
		if confidence < 0.7 {
			confidence = 0.7
		}
	}

	// NEW: Edge case: Deadlock detection with channel operations
	if category == CategoryDeadlock && strings.Contains(lowerText, "channel") {
		// Channel deadlock is very specific - boost confidence
		confidence = 1.0
	}

	// NEW: Edge case: I/O error with "broken pipe" - likely connection issue
	if category == CategoryIOError && strings.Contains(lowerText, "broken pipe") {
		// Broken pipe suggests connection issue - could be HTTP error
		confidence -= 0.15
		if confidence < 0.7 {
			confidence = 0.7
		}
	}

	// NEW: Edge case: HTTP error with specific status code in assertion
	if category == CategoryHTTPError && strings.Contains(lowerText, "status code") &&
		(strings.Contains(lowerText, "expected") || strings.Contains(lowerText, "want")) {
		// HTTP status code in assertion context - could be assertion error
		confidence -= 0.2
		if confidence < 0.6 {
			confidence = 0.6
		}
	}

	// NEW: Edge case: Map key error with "zero map key" - very specific pattern
	if category == CategoryMapKey && strings.Contains(lowerText, "zero map key") {
		// Zero map key is unambiguous - maximum confidence
		confidence = 1.0
	}

	// NEW: Edge case: Unknown error with very short message (< 20 chars)
	if category == CategoryUnknown && len(fullText) < 20 {
		// Very short unknown messages suggest test framework issues
		confidence = 0.05
	}

	// Ensure minimum confidence
	if confidence < 0.05 {
		confidence = 0.05
	}
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// CategorizeFailures categorizes a list of test failures using the decision tree.
// This function applies the priority-based decision tree to each failure and
// generates comprehensive statistics about the categorization results.
//
// Process:
// 1. Apply CategorizeFailure (decision tree) to each failure
// 2. Track statistics: total count, count by category, low confidence count, ambiguous cases
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

		if cat.Confidence <= 0.5 {
			stats.LowConfidence++
		}

		// Track ambiguous cases (reasoning contains "Ambiguity detected")
		if IsAmbiguous(cat) {
			stats.AmbiguousCases++
		}
	}

	return categorized, stats
}

// IsAmbiguous checks if a categorized failure was ambiguous (multiple patterns matched)
func IsAmbiguous(cat CategorizedFailure) bool {
	return strings.Contains(cat.Reasoning, "Ambiguity detected")
}

// GetAmbiguousCount returns the number of matching patterns for a categorized failure
func GetAmbiguousCount(cat CategorizedFailure) int {
	// Parse reasoning to extract "Total matching patterns: N"
	if strings.Contains(cat.Reasoning, "Total matching patterns:") {
		parts := strings.Split(cat.Reasoning, "Total matching patterns:")
		if len(parts) > 1 {
			var count int
			_, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &count)
			if err == nil {
				return count
			}
		}
	}
	return 1 // Default to 1 if not ambiguous
}

// GetHighConfidenceFailures returns only failures with confidence >= threshold
func GetHighConfidenceFailures(categorized []CategorizedFailure, threshold float64) []CategorizedFailure {
	var highConf []CategorizedFailure
	for _, cat := range categorized {
		if cat.Confidence.Float64() >= threshold {
			highConf = append(highConf, cat)
		}
	}
	return highConf
}

// GetAmbiguousFailures returns only failures that had multiple matching patterns
func GetAmbiguousFailures(categorized []CategorizedFailure) []CategorizedFailure {
	var ambiguous []CategorizedFailure
	for _, cat := range categorized {
		if IsAmbiguous(cat) {
			ambiguous = append(ambiguous, cat)
		}
	}
	return ambiguous
}

// GetUncertainFailures returns failures with confidence below the threshold (default 0.7).
// Values at the threshold are considered sufficiently certain and are excluded.
func GetUncertainFailures(categorized []CategorizedFailure, threshold ...float64) []CategorizedFailure {
	minConf := 0.7
	if len(threshold) > 0 {
		minConf = threshold[0]
	}

	var uncertain []CategorizedFailure
	for _, cat := range categorized {
		if cat.Confidence.Float64() < minConf {
			uncertain = append(uncertain, cat)
		}
	}
	return uncertain
}

// GetMatchingCategoriesForFailure returns all categories that matched for a given failure
// This is useful for analyzing why ambiguity occurred
func GetMatchingCategoriesForFailure(failure Failure) []FailureCategory {
	fullText := failure.ErrorMessage
	if failure.StackTrace != "" {
		fullText = fullText + "\n" + failure.StackTrace
	}

	matchingRules := matchingCategorizationRules(fullText, sortedCategorizationRules())
	matched := make([]FailureCategory, 0, len(matchingRules))
	for _, rule := range matchingRules {
		matched = append(matched, rule.Category)
	}

	return matched
}

// GetConfidenceLevel returns a human-readable confidence level
func GetConfidenceLevel(confidence float64) string {
	switch {
	case confidence >= 0.95:
		return "Very High"
	case confidence >= 0.8:
		return "High"
	case confidence >= 0.6:
		return "Moderate"
	case confidence >= 0.4:
		return "Low"
	case confidence >= 0.2:
		return "Very Low"
	default:
		return "Uncertain"
	}
}

// NeedsManualReview checks if a categorization needs manual review based on confidence
// Returns true if confidence is at or below threshold (default 0.5) or category is unknown.
// Borderline cases (exactly at threshold) are flagged for manual review.
func NeedsManualReview(cat CategorizedFailure, threshold ...float64) bool {
	minConf := 0.5
	if len(threshold) > 0 {
		minConf = threshold[0]
	}
	return cat.Confidence.Float64() <= minConf || cat.Category == CategoryUnknown
}

// GetSuggestedSubcategory returns a suggested subcategory for ambiguous cases
// based on pattern analysis
func GetSuggestedSubcategory(cat CategorizedFailure) string {
	if cat.Subcategory != "" {
		return cat.Subcategory
	}

	// Analyze error message for subcategory hints
	fullText := cat.ErrorMessage + " " + cat.StackTrace
	lowerText := strings.ToLower(fullText)

	// Suggest subcategories based on content
	switch cat.Category {
	case CategoryHTTPError:
		if strings.Contains(lowerText, "timeout") || strings.Contains(lowerText, "deadline") {
			return "timeout"
		}
		if strings.Contains(lowerText, "refused") || strings.Contains(lowerText, "reset") {
			return "connection"
		}
		if strings.Contains(lowerText, "status code") || strings.Contains(lowerText, "500") || strings.Contains(lowerText, "404") {
			return "status"
		}
		return "network"

	case CategoryIOError:
		if strings.Contains(lowerText, "permission") {
			return "permission"
		}
		if strings.Contains(lowerText, "no such file") || strings.Contains(lowerText, "not found") {
			return "not_found"
		}
		if strings.Contains(lowerText, "broken pipe") || strings.Contains(lowerText, "connection") {
			return "connection"
		}
		return "filesystem"

	case CategoryPanic:
		if strings.Contains(lowerText, "nil pointer") {
			return "nil_pointer"
		}
		if strings.Contains(lowerText, "index") || strings.Contains(lowerText, "slice") || strings.Contains(lowerText, "bounds") {
			return "bounds"
		}
		if strings.Contains(lowerText, "interface") || strings.Contains(lowerText, "conversion") {
			return "type"
		}
		return "runtime"

	case CategoryTimeout:
		if strings.Contains(lowerText, "context") {
			return "context"
		}
		if strings.Contains(lowerText, "test") {
			return "test"
		}
		return "operation"

	default:
		return ""
	}
}

// ResolveAmbiguity applies the additional decision criteria documented in
// docs/categorization-rules.md. CategorizeFailure intentionally retains the
// base priority result; callers that need the more specific interpretation can
// opt into this resolver and retain its explanation and confidence adjustment.
func ResolveAmbiguity(cat CategorizedFailure) CategorizedFailure {
	fullText := cat.ErrorMessage + "\n" + cat.StackTrace
	lowerText := strings.ToLower(fullText)

	// Most ambiguity is represented by multiple matching category rules. A
	// context can expose both cancellation and deadline markers under the same
	// timeout rule, so it needs an explicit intra-category check as well.
	if !IsAmbiguous(cat) && !hasIntrinsicAmbiguity(cat.Category, lowerText) {
		return cat
	}

	// A framework assertion that quotes a diagnostic is more precise than a
	// broad diagnostic regex, unless the output also contains an actual Go
	// runtime marker at the start of a line.
	if resolveAssertionPatternOverlap(&cat, fullText) {
		return finalizeAmbiguityResolution(cat)
	}

	if resolvePanicAssertionAmbiguity(&cat, fullText) {
		return finalizeAmbiguityResolution(cat)
	}

	if resolveTimeoutContextAmbiguity(&cat, fullText) {
		return finalizeAmbiguityResolution(cat)
	}

	// Resolution rule: If error mentions both HTTP and timeout, check for specific indicators.
	if cat.Category == CategoryTimeout && strings.Contains(lowerText, "dial") && strings.Contains(lowerText, "tcp") {
		cat.Category = CategoryHTTPError
		cat.Subcategory = "timeout"
		cat.Reasoning += "\n  Ambiguity resolution: 'dial tcp' identifies a network timeout over a general timeout"
		cat.Confidence = NewConfidence(0.75)
	}

	// Resolution rule: If panic mentions interface conversion, it can be a type mismatch.
	if cat.Category == CategoryPanic && strings.Contains(lowerText, "interface conversion") &&
		!strings.Contains(lowerText, "panic:") && !strings.Contains(lowerText, "runtime panic") {
		cat.Category = CategoryTypeMismatch
		cat.Reasoning += "\n  Ambiguity resolution: interface conversion without explicit panic marker identifies a type mismatch"
		cat.Confidence = NewConfidence(0.8)
	}

	// Resolution rule: If nil pointer appears in test setup context.
	if cat.Category == CategoryNilPointer && (strings.Contains(lowerText, "setup") ||
		strings.Contains(lowerText, "initialize") || strings.Contains(lowerText, "before all")) {
		cat.Subcategory = "test_setup"
		cat.Reasoning += "\n  Ambiguity resolution: nil pointer in test setup context"
	}

	return finalizeAmbiguityResolution(cat)
}

func hasIntrinsicAmbiguity(category FailureCategory, lowerText string) bool {
	return category == CategoryTimeout &&
		strings.Contains(lowerText, "context deadline exceeded") &&
		strings.Contains(lowerText, "context canceled")
}

func resolveAssertionPatternOverlap(cat *CategorizedFailure, fullText string) bool {
	if !assertionExpectationPattern.MatchString(fullText) || explicitPanicLinePattern.MatchString(fullText) {
		return false
	}

	// This is only an overlap when a broad diagnostic could otherwise win over
	// the assertion rule. The assertion itself remains the observable failure.
	if !assertionDiagnosticValuePattern.MatchString(fullText) {
		return false
	}

	cat.Category = CategoryAssertionError
	cat.Subcategory = ""
	cat.Confidence = CalculateConfidence(ConfidenceCalculationParams{
		BaseConfidence: 0.7,
		Signals: []MatchSignal{
			{Type: SignalExactMatch, Pattern: "assertion expectation"},
			{Type: SignalPartialMatch, Pattern: "quoted diagnostic"},
		},
		AmbiguityPenalty: 0.5,
	})
	cat.Reasoning += "\n  Ambiguity resolution: assertion expectation quotes a diagnostic; assertion_error wins over the quoted regex match"
	return true
}

func resolvePanicAssertionAmbiguity(cat *CategorizedFailure, fullText string) bool {
	lowerText := strings.ToLower(fullText)
	hasNilPointer := strings.Contains(lowerText, "nil pointer") || strings.Contains(lowerText, "null pointer")
	hasAssertion := strings.Contains(lowerText, "assertion") || assertionExpectationPattern.MatchString(fullText)

	// A Go panic marker is the primary event. The nil-pointer diagnostic gives
	// the panic a more useful subtype, while assertion text is retained as a
	// competing signal rather than replacing the runtime failure.
	if cat.Category == CategoryPanic && hasNilPointer && explicitPanicLinePattern.MatchString(fullText) {
		cat.Subcategory = "nil_pointer_dereference"
		params := ConfidenceCalculationParams{
			BaseConfidence: 0.9,
			Signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "runtime panic marker"},
				{Type: SignalExactMatch, Pattern: "nil pointer diagnostic"},
			},
			ContextBoost: 0.05,
		}
		if hasAssertion {
			params.Signals = append(params.Signals, MatchSignal{Type: SignalPartialMatch, Pattern: "assertion text"})
			params.AmbiguityPenalty = 0.5
		}
		cat.Confidence = CalculateConfidence(params)
		cat.Reasoning += "\n  Ambiguity resolution: explicit panic marker is the primary event; nil-pointer evidence is recorded as its subtype"
		return true
	}

	// Without an explicit panic marker, a direct nil-pointer diagnostic is more
	// specific than generic assertion text. Lower confidence keeps this overlap
	// visible for review rather than silently treating it as unambiguous.
	if cat.Category == CategoryNilPointer && hasAssertion {
		cat.Subcategory = "assertion_context"
		cat.Confidence = CalculateConfidence(ConfidenceCalculationParams{
			BaseConfidence: 0.85,
			Signals: []MatchSignal{
				{Type: SignalExactMatch, Pattern: "nil pointer diagnostic"},
				{Type: SignalPartialMatch, Pattern: "assertion text"},
			},
			AmbiguityPenalty: 0.5,
		})
		cat.Reasoning += "\n  Ambiguity resolution: explicit nil-pointer diagnostic outranks generic assertion text"
		return true
	}

	return false
}

func resolveTimeoutContextAmbiguity(cat *CategorizedFailure, fullText string) bool {
	lowerText := strings.ToLower(fullText)
	if cat.Category != CategoryTimeout ||
		!strings.Contains(lowerText, "context deadline exceeded") ||
		!strings.Contains(lowerText, "context canceled") {
		return false
	}

	category := timeoutSubcategory(fullText)
	cat.Subcategory = category
	cat.Confidence = CalculateConfidence(ConfidenceCalculationParams{
		BaseConfidence: 0.85,
		Signals: []MatchSignal{
			{Type: SignalExactMatch, Pattern: "context deadline exceeded"},
			{Type: SignalExactMatch, Pattern: "context canceled"},
		},
		AmbiguityPenalty: 0.5,
	})
	cat.Reasoning += fmt.Sprintf("\n  Ambiguity detected: both context deadline and cancellation markers matched\n  Ambiguity resolution: the final context marker selects timeout subcategory %q", category)
	return true
}

func timeoutSubcategory(fullText string) string {
	lowerText := strings.ToLower(fullText)
	deadlineIndex := strings.LastIndex(lowerText, "context deadline exceeded")
	canceledIndex := strings.LastIndex(lowerText, "context canceled")

	switch {
	case deadlineIndex < 0 && canceledIndex < 0:
		return ""
	case canceledIndex > deadlineIndex:
		return "context_canceled"
	default:
		return "deadline_exceeded"
	}
}

func finalizeAmbiguityResolution(cat CategorizedFailure) CategorizedFailure {
	cat.Type = cat.Category
	cat.Uncertain = cat.Confidence.IsUncertain()
	return cat
}

// CategorizationStats provides statistics about categorized failures
type CategorizationStats struct {
	Total          int                     `json:"total"`
	ByCategory     map[FailureCategory]int `json:"by_category"`
	LowConfidence  int                     `json:"low_confidence"`
	AmbiguousCases int                     `json:"ambiguous_cases"` // Count of failures with multiple matching patterns
}

// GetCategoryDescription returns a human-readable description for a category
func GetCategoryDescription(cat FailureCategory) string {
	descriptions := map[FailureCategory]string{
		CategoryAssertionError:  "Assertion or expectation failure (expected vs actual mismatch)",
		CategoryTimeout:         "Test exceeded timeout limit or context deadline",
		CategoryPanic:           "Runtime panic occurred",
		CategoryDataRace:        "Data race detected by race detector",
		CategoryNilPointer:      "Nil pointer dereference",
		CategoryTypeMismatch:    "Type conversion or assertion failure",
		CategoryIndexOutOfRange: "Array or slice index out of bounds",
		CategoryMapKey:          "Map key access error",
		CategoryChannel:         "Channel operation error (closed channel, etc.)",
		CategoryGoroutinePanic:  "Goroutine panic or leak",
		CategoryDeadlock:        "Potential deadlock detected",
		CategoryIOError:         "I/O operation failed (file, network, etc.)",
		CategoryHTTPError:       "HTTP/network communication error",
		CategoryUnknown:         "Other failure - requires manual analysis",
	}

	if desc, ok := descriptions[cat]; ok {
		return desc
	}
	return "No description available"
}

// GetCategoryLabel returns the human-readable category label used in reports.
// The stable JSON/API value for a fallback remains "unknown", while people
// see the more actionable "Other: <description>" form.
func GetCategoryLabel(cat CategorizedFailure) string {
	if cat.Category != CategoryUnknown {
		return string(cat.Category)
	}

	return "Other: " + fallbackDescription(cat.Subcategory)
}

func fallbackDescription(subcategory string) string {
	switch subcategory {
	case fallbackSubcategoryEmptyFailure:
		return "empty failure"
	case fallbackSubcategoryMalformedOutput:
		return "malformed output"
	case fallbackSubcategoryUnknownPanicMessage:
		return "unknown panic message"
	case fallbackSubcategoryUnknownFatalMessage:
		return "unknown fatal message"
	case "", fallbackSubcategoryUnclassified:
		return "unclassified failure"
	default:
		return strings.ReplaceAll(subcategory, "_", " ")
	}
}

func categoryGroupLabel(category FailureCategory) string {
	if category == CategoryUnknown {
		return "Other"
	}
	return string(category)
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
			sb.WriteString(fmt.Sprintf("  %s: %d (%s)\n", categoryGroupLabel(cat), count, GetCategoryDescription(cat)))
		}
	}

	sb.WriteString(fmt.Sprintf("\nLow confidence categorizations: %d\n\n", stats.LowConfidence))

	// Print individual failures
	sb.WriteString("Individual failures:\n")
	for i, cat := range categorized {
		sb.WriteString(fmt.Sprintf("%d. [%s] %s (%.0f%% confidence)\n",
			i+1, GetCategoryLabel(cat), cat.TestName, cat.Confidence*100))
		sb.WriteString(fmt.Sprintf("   File: %s:%d\n", cat.FilePath, cat.LineNumber))
		sb.WriteString(fmt.Sprintf("   Error: %s\n", cat.ErrorMessage))
		if cat.Reasoning != "" {
			sb.WriteString(fmt.Sprintf("   Reasoning: %s\n", cat.Reasoning))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
