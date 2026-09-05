// Package quota polls Z.AI's account-level Coding Plan quota endpoint
// (/api/monitor/usage/quota/limit) with the proxy-held credential and
// normalizes its five-hour and weekly usage windows.
//
// The package is deliberately isolated from the request data path: it never
// sees model traffic, it is meant to be polled out of band on a slow cadence,
// and it retains only normalized, non-secret state. API keys, raw account
// identifiers, and response bodies are never written to logs or error strings.
package quota

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Window identifies which provider quota window an observation describes.
type Window string

const (
	// WindowFiveHour is the rolling five-hour model-usage window.
	WindowFiveHour Window = "five_hour"
	// WindowWeekly is the weekly model-usage window.
	WindowWeekly Window = "weekly"
)

// String returns the canonical name of the window, for logs and metrics.
func (w Window) String() string { return string(w) }

// LimitType records which provider schema variant a window was parsed from,
// so downstream consumers can tell percentage-only legacy observations apart
// from current credit-count observations.
type LimitType string

const (
	// LimitTypeTokens marks the legacy TOKENS_LIMIT schema, which carries
	// only a used percentage and the reset time.
	LimitTypeTokens LimitType = "TOKENS_LIMIT"
	// LimitTypeCredit marks the current CREDIT_LIMIT schema, which carries
	// absolute credit amounts (allowance, used, remaining).
	LimitTypeCredit LimitType = "CREDIT_LIMIT"
)

// wireWindowUnit values observed in the provider payload. The unit field is
// the only reliable window discriminator: the weekly entry has historically
// also been sent with number set to 7, and sorting by reset time flips the
// windows near a weekly reset.
const (
	wireUnitFiveHour = 3
	wireUnitWeekly   = 6
)

// WindowState is the normalized, non-secret observation of one quota window.
// Absolute amounts are credits for current CREDIT_LIMIT payloads and zero
// with HasUsage false for legacy TOKENS_LIMIT payloads, which are
// percentage-only.
type WindowState struct {
	// Window is the normalized window kind (five-hour or weekly).
	Window Window
	// LimitType is the provider schema variant the state was parsed from.
	LimitType LimitType
	// ResetTime is when the provider will reset the window, in UTC. It is
	// the zero time when the provider did not advertise a reset.
	ResetTime time.Time

	// Used, Limit, and Remaining are absolute credit amounts. They are
	// meaningful only when HasUsage is true.
	Used      float64
	Limit     float64
	Remaining float64
	HasUsage  bool

	// UsedFraction is the consumed share of the allowance, clamped to
	// [0, 1]. It is derived from the absolute amounts when available and
	// from the provider-reported percentage otherwise.
	UsedFraction float64
}

// Snapshot is the normalized result of one successful quota poll.
type Snapshot struct {
	// FetchedAt is the time the payload was observed.
	FetchedAt time.Time
	// PlanTier is the provider-reported plan level (e.g. "lite", "pro").
	// It is plan metadata, not an account identifier, and may be empty.
	PlanTier string
	// Windows holds the normalized windows in a fixed order: five-hour
	// first, then weekly. Windows the provider sent but this package does
	// not model are omitted.
	Windows []WindowState
}

// Window returns the normalized state for w and whether the snapshot
// contains it.
func (s Snapshot) Window(w Window) (WindowState, bool) {
	for _, state := range s.Windows {
		if state.Window == w {
			return state, true
		}
	}
	return WindowState{}, false
}

// wireEnvelope mirrors the provider response wrapper. Pointer fields
// distinguish absent values from zero values.
type wireEnvelope struct {
	Code    int     `json:"code"`
	Msg     string  `json:"msg"`
	Success *bool   `json:"success"`
	Data    wireTop `json:"data"`
}

type wireTop struct {
	Level  string      `json:"level"`
	Limits []wireLimit `json:"limits"`
}

type wireLimit struct {
	Type          string   `json:"type"`
	Unit          int      `json:"unit"`
	Number        int      `json:"number"`
	Usage         *float64 `json:"usage"`        // total allowance, despite the name
	CurrentValue  *float64 `json:"currentValue"` // used amount
	Remaining     *float64 `json:"remaining"`
	Percentage    *float64 `json:"percentage"`    // used percent
	NextResetTime *int64   `json:"nextResetTime"` // unix epoch milliseconds
}

// ProviderError reports a structured rejection returned by the quota
// endpoint itself inside an HTTP 200 body, such as an invalid or expired
// credential or a non-Coding-Plan key. Message is the provider-supplied
// text; it is deliberately kept out of Error() because provider messages
// are not guaranteed to be free of account-identifying material.
type ProviderError struct {
	Code    int
	Message string
}

// Error implements the error interface without embedding the provider
// message, so logging the error can never leak account-identifying text.
func (e *ProviderError) Error() string {
	return fmt.Sprintf("quota endpoint rejected the request (provider code %d)", e.Code)
}

// Normalize parses a raw /api/monitor/usage/quota/limit response body and
// returns its five-hour and weekly windows. Entries with unknown limit
// types or unknown window units (monthly tool quotas, future schema
// additions) are ignored rather than rejected. now stamps FetchedAt.
//
// Normalize returns an error when the body is not a well-formed response,
// and a *ProviderError when the endpoint answered with a structured
// business error. Error text never quotes payload content.
func Normalize(body []byte, now time.Time) (Snapshot, error) {
	var envelope wireEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Snapshot{}, fmt.Errorf("quota response is not valid JSON: %w", err)
	}

	if envelope.Success != nil && !*envelope.Success {
		return Snapshot{}, &ProviderError{Code: envelope.Code, Message: envelope.Msg}
	}
	// Some provider eras omit the success flag and signal failure purely
	// through the business code.
	if envelope.Success == nil && envelope.Code != 0 && envelope.Code != 200 {
		return Snapshot{}, &ProviderError{Code: envelope.Code, Message: envelope.Msg}
	}

	if envelope.Data.Limits == nil {
		return Snapshot{}, errors.New("quota response is missing its data.limits array")
	}

	snapshot := Snapshot{
		FetchedAt: now,
		PlanTier:  envelope.Data.Level,
		Windows:   make([]WindowState, 0, 2),
	}
	seen := make(map[Window]bool, 2)
	for _, limit := range envelope.Data.Limits {
		state, ok := normalizeLimit(limit)
		if !ok || seen[state.Window] {
			continue
		}
		seen[state.Window] = true
		snapshot.Windows = append(snapshot.Windows, state)
	}
	return snapshot, nil
}

// normalizeLimit converts one limits[] entry into normalized state. The
// second return is false when the entry is not a five-hour or weekly
// model-usage window this package models, or when it carries no usable
// quota signal; such entries are skipped by the caller.
func normalizeLimit(limit wireLimit) (WindowState, bool) {
	var limitType LimitType
	switch {
	case strings.EqualFold(limit.Type, string(LimitTypeTokens)):
		limitType = LimitTypeTokens
	case strings.EqualFold(limit.Type, string(LimitTypeCredit)):
		limitType = LimitTypeCredit
	default:
		return WindowState{}, false
	}

	var window Window
	switch limit.Unit {
	case wireUnitFiveHour:
		window = WindowFiveHour
	case wireUnitWeekly:
		window = WindowWeekly
	default:
		return WindowState{}, false
	}

	state := WindowState{
		Window:    window,
		LimitType: limitType,
		ResetTime: normalizeResetTime(limit.NextResetTime),
	}

	switch limitType {
	case LimitTypeCredit:
		state.Used = nonNegative(limit.CurrentValue)
		state.Limit = nonNegative(limit.Usage)
		state.Remaining = creditRemaining(limit)
		state.HasUsage = limit.Usage != nil && limit.CurrentValue != nil
		state.UsedFraction = creditUsedFraction(limit)
	default:
		if limit.Percentage == nil {
			// A legacy entry with no percentage carries no quota
			// signal worth retaining.
			return WindowState{}, false
		}
		state.UsedFraction = clampUnit(*limit.Percentage / 100)
	}

	if state.UsedFraction < 0 || isNaN(state.UsedFraction) {
		return WindowState{}, false
	}
	return state, true
}

// normalizeResetTime converts the provider's unix-millisecond reset stamp
// to UTC, treating absent and non-positive stamps as unknown.
func normalizeResetTime(millis *int64) time.Time {
	if millis == nil || *millis <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(*millis).UTC()
}

// creditUsedFraction derives the consumed share from absolute amounts when
// they are present and falls back to the provider-reported percentage,
// which some payloads omit and which server-side rounding can distort.
func creditUsedFraction(limit wireLimit) float64 {
	if limit.Usage != nil && *limit.Usage > 0 && limit.CurrentValue != nil {
		return clampUnit(*limit.CurrentValue / *limit.Usage)
	}
	if limit.Percentage != nil {
		return clampUnit(*limit.Percentage / 100)
	}
	return -1
}

// creditRemaining reports the remaining allowance, deriving it from
// usage minus currentValue when the provider omitted the field.
func creditRemaining(limit wireLimit) float64 {
	if limit.Remaining != nil {
		return nonNegative(limit.Remaining)
	}
	if limit.Usage != nil && limit.CurrentValue != nil {
		return nonNegative(float64Ptr(*limit.Usage - *limit.CurrentValue))
	}
	return 0
}

func float64Ptr(v float64) *float64 { return &v }

func nonNegative(v *float64) float64 {
	if v == nil || *v < 0 || isNaN(*v) {
		return 0
	}
	return *v
}

func clampUnit(v float64) float64 {
	if isNaN(v) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func isNaN(v float64) bool { return v != v }
