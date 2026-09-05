package main

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Z.AI rejects over-quota and over-rate model requests with a small JSON
// body whose business code, not the HTTP status, distinguishes what actually
// happened. The codes this proxy acts on, per docs/plan/plan.md:
//
//	1302        concurrency — the account's concurrent slots are full
//	1303, 1305  frequency — short-horizon request-rate pressure
//	1308        five-hour quota window exhausted
//	1310        weekly/monthly quota window exhausted
//	1312        temporary model congestion, not account-quota exhaustion
//
// Z.AI has used more than one envelope for these bodies across its
// OpenAI-compatible and Anthropic-compatible endpoints, so the parser
// tolerates the observed shapes instead of pinning one:
//
//	{"error":{"code":"1302","message":"..."}}
//	{"code":1302,"msg":"..."}
//	{"type":"error","error":{"type":"rate_limit_error","code":"1303",...}}
//
// The parser is pure and bounded: it takes an already-read body plus a byte
// budget, classifies without touching admission behavior, and retains only
// the class, the code, and reset metadata. Reset metadata comes from a
// structured reset field when the body carries one, and otherwise from the
// naive wall-clock stamp Z.AI embeds in quota message text ("Your limit
// will reset at 2026-09-06 00:00:00"); only the parsed instant is kept. The
// provider message text is inspected transiently at most and never
// retained — like the quota package, provider messages are not guaranteed
// to be free of account-identifying material, so they must not reach logs
// or metrics through this type.

// ZaiErrorClass is the bounded business-error classification. The values
// are the metric label vocabulary reserved for
// zai_proxy_zai_errors_total{class,...} in docs/plan/plan.md.
type ZaiErrorClass string

const (
	// ZaiErrorClassConcurrency means the account's concurrent-request
	// slots are full (code 1302). Admission should hold concurrency; the
	// requests-per-second ceiling is not poisoned.
	ZaiErrorClassConcurrency ZaiErrorClass = "concurrency"
	// ZaiErrorClassFrequency means short-horizon rate pressure
	// (codes 1303, 1305).
	ZaiErrorClassFrequency ZaiErrorClass = "frequency"
	// ZaiErrorClassQuota means a plan quota window is exhausted
	// (codes 1308, 1310). The code distinguishes the window.
	ZaiErrorClassQuota ZaiErrorClass = "quota"
	// ZaiErrorClassModelCongestion means the model itself is temporarily
	// congested (code 1312); it is not account-quota exhaustion.
	ZaiErrorClassModelCongestion ZaiErrorClass = "model_congestion"
	// ZaiErrorClassUnknown covers every other outcome: unrecognized codes,
	// unrecognized envelopes, and bodies too malformed or too large to
	// classify.
	ZaiErrorClassUnknown ZaiErrorClass = "unknown"
)

// DefaultMaxZaiErrorBodyBytes is the body budget ParseZaiError applies when
// the caller passes no limit. Real Z.AI business errors are well under a
// kilobyte; the budget exists so a misbehaving upstream cannot turn error
// handling into an unbounded parse.
const DefaultMaxZaiErrorBodyBytes = 32 << 10

// maxZaiErrorCodeDigits bounds the code retained for classification (and,
// eventually, a metric label). Z.AI business codes are four digits; the
// margin absorbs future codes without letting arbitrary payload content
// become a high-cardinality label.
const maxZaiErrorCodeDigits = 8

// ZaiBusinessError is the safe, bounded summary of one Z.AI business-error
// response. Class is always populated (ZaiErrorClassUnknown when nothing
// more specific was recovered); Code, ResetAt, and RetryAfter are zero when
// the body did not carry them.
type ZaiBusinessError struct {
	// Class is the bounded classification of the response.
	Class ZaiErrorClass
	// Code is the provider business code as a digit string, retained only
	// when it is short enough to be a safe label. It may be present with
	// Class ZaiErrorClassUnknown, which is how novel provider codes are
	// surfaced without inventing a class for them.
	Code string
	// ResetAt is the provider-advertised reset instant in UTC, from a
	// structured reset-time field when the body carries one, and otherwise
	// from the reset stamp embedded in quota message text. It is the zero
	// time when the body advertised no plausible reset.
	ResetAt time.Time
	// RetryAfter is a retry_after value from the body, as a duration. It
	// is zero when absent or implausible; Retry-After headers are the
	// caller's concern and are not parsed here.
	RetryAfter time.Duration
	// Oversized reports that the body exceeded the byte budget and was not
	// parsed at all.
	Oversized bool
}

// ParseZaiError classifies one bounded Z.AI error body. maxBytes caps how
// much of the body is considered; bodies larger than the cap are reported
// as oversized and unknown, never parsed. A non-positive maxBytes means
// DefaultMaxZaiErrorBodyBytes. The result is always safe to log: it never
// contains provider message text.
func ParseZaiError(body []byte, maxBytes int) ZaiBusinessError {
	result := ZaiBusinessError{Class: ZaiErrorClassUnknown}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxZaiErrorBodyBytes
	}
	if len(body) > maxBytes {
		result.Oversized = true
		return result
	}
	if len(body) == 0 {
		return result
	}

	inner, outer := decodeZaiErrorEnvelope(body)
	if outer == nil {
		// Not a JSON object at all: array, scalar, truncated, or null.
		return result
	}

	// A missing code yields a nil raw message, which normalizes to "".
	raw, _ := zaiErrorLookup(inner, outer, "code")
	code := zaiNormalizeCode(raw)
	if code == "" {
		// Last resort: some endpoints put the business code at the front
		// of the message text instead of a code field. Only a leading
		// digit run naming a known class is accepted, so incidental
		// numbers elsewhere in prose can never become a code.
		if msg, ok := zaiErrorLookup(inner, outer, "msg", "message"); ok {
			code = zaiCodeFromMessage(unquoteJSONString(msg))
		}
	}
	if code != "" {
		result.Code = code
		result.Class = ClassifyZaiErrorCode(code)
	}

	// Reset metadata is extracted independently of classification: an
	// unrecognized code that still advertises a reset is exactly the case
	// a supervisory circuit needs metadata for.
	if raw, ok := zaiErrorLookup(inner, outer, zaiResetTimeKeys...); ok {
		result.ResetAt = zaiResetTimeFromRaw(raw)
	}
	if result.ResetAt.IsZero() {
		// Quota bodies often advertise the reset only inside message text
		// ("Your limit will reset at 2026-09-06 00:00:00"). Only the parsed
		// instant survives; the message itself is never retained.
		if msg, ok := zaiErrorLookup(inner, outer, "msg", "message"); ok {
			result.ResetAt = zaiResetTimeFromMessage(unquoteJSONString(msg))
		}
	}
	if raw, ok := zaiErrorLookup(inner, outer, zaiRetryAfterKeys...); ok {
		result.RetryAfter = zaiRetryAfterFromRaw(raw)
	}
	return result
}

// ClassifyZaiErrorCode maps a provider business code to its bounded class.
// Unknown or empty codes classify as ZaiErrorClassUnknown.
func ClassifyZaiErrorCode(code string) ZaiErrorClass {
	switch code {
	case "1302":
		return ZaiErrorClassConcurrency
	case "1303", "1305":
		return ZaiErrorClassFrequency
	case "1308", "1310":
		return ZaiErrorClassQuota
	case "1312":
		return ZaiErrorClassModelCongestion
	default:
		return ZaiErrorClassUnknown
	}
}

// zaiResetTimeKeys are the reset-time field names tolerated across envelope
// eras, checked innermost first.
var zaiResetTimeKeys = []string{
	"reset_time", "resetTime", "reset_at", "resetAt",
	"next_reset_time", "nextResetTime",
}

// zaiRetryAfterKeys are the retry-delay field names tolerated in the body.
var zaiRetryAfterKeys = []string{"retry_after", "retryAfter"}

// decodeZaiErrorEnvelope parses the body as a JSON object and splits it
// into the nested error object, if any, and the top-level object. Envelopes
// without a usable object return a nil outer map. A null or non-object
// "error" field is ignored so the top level is still searched.
func decodeZaiErrorEnvelope(body []byte) (inner, outer map[string]json.RawMessage) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil || top == nil {
		return nil, nil
	}
	if raw, ok := top["error"]; ok {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(raw, &nested); err == nil && nested != nil {
			return nested, top
		}
	}
	return nil, top
}

// zaiErrorLookup returns the first present key, searching the nested error
// object before the top-level envelope so the most specific envelope wins.
func zaiErrorLookup(inner, outer map[string]json.RawMessage, keys ...string) (json.RawMessage, bool) {
	for _, level := range []map[string]json.RawMessage{inner, outer} {
		if level == nil {
			continue
		}
		for _, key := range keys {
			if raw, ok := level[key]; ok {
				return raw, true
			}
		}
	}
	return nil, false
}

// zaiNormalizeCode converts a raw JSON value into a bounded digit-string
// code, accepting JSON strings and numbers. Anything else — objects,
// booleans, signed or oversized digit runs — yields "".
func zaiNormalizeCode(raw json.RawMessage) string {
	s := strings.TrimSpace(unquoteJSONString(raw))
	if len(s) == 0 || len(s) > maxZaiErrorCodeDigits {
		return ""
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return ""
		}
	}
	return s
}

// zaiCodeFromMessage recovers a business code prefixed to message text
// ("1302: ..."). Only digit runs that name a known class are returned;
// unrecognized leading numbers are ignored rather than surfaced, because
// prose regularly begins with counts and statuses that are not codes.
func zaiCodeFromMessage(msg string) string {
	msg = strings.TrimLeft(msg, " \t\r\n")
	end := 0
	for end < len(msg) && msg[end] >= '0' && msg[end] <= '9' {
		end++
	}
	if end == 0 || end > maxZaiErrorCodeDigits {
		return ""
	}
	code := msg[:end]
	if ClassifyZaiErrorCode(code) == ZaiErrorClassUnknown {
		return ""
	}
	return code
}

// zaiProviderLocation is the fixed UTC+8 offset Z.AI uses for the naive
// wall-clock reset stamps it embeds in quota messages. A fixed zone is used
// deliberately: the stamp is Beijing time year-round (China has no DST), so
// parsing must not depend on a time-zone database being installed.
var zaiProviderLocation = time.FixedZone("UTC+8", 8*3600)

// zaiMessageResetPattern matches the naive wall-clock stamp Z.AI embeds in
// quota message text, e.g. "Your limit will reset at 2026-09-06 00:00:00".
var zaiMessageResetPattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}`)

// zaiPlausibleResetBounds is the inclusive wall-clock window a parsed
// timestamp must land in to be believed, mirroring the epoch range
// zaiEpochFromSeconds accepts (~2001 through ~year 2600). Timestamps
// outside it are treated as absent rather than clamped, on the same
// reasoning: inventing a reset instant would hold a circuit open until the
// wrong time.
var zaiPlausibleResetBounds = [2]time.Time{time.Unix(1e9, 0).UTC(), time.Unix(2e10, 0).UTC()}

// zaiResetTimeFromRaw converts a raw reset-time value into a UTC instant,
// accepting unix seconds or unix milliseconds (as a JSON number or numeric
// string), an RFC3339 timestamp, or a naive "2006-01-02 15:04:05" stamp
// read in the provider's Beijing wall clock. Anything else yields the zero
// time.
func zaiResetTimeFromRaw(raw json.RawMessage) time.Time {
	if v, ok := zaiFloatFromRaw(raw); ok {
		return zaiEpochFromSeconds(v)
	}
	if t, ok := zaiParseTimestamp(unquoteJSONString(raw)); ok {
		return t
	}
	return time.Time{}
}

// zaiEpochFromSeconds converts a numeric epoch, in seconds or milliseconds
// distinguished by magnitude (every post-2001 epoch-millisecond stamp
// exceeds 1e12), into a UTC instant. Values outside the plausible epoch
// range are treated as absent rather than clamped: inventing a reset
// instant would open a circuit until the wrong time.
func zaiEpochFromSeconds(v float64) time.Time {
	var sec float64
	switch {
	case v >= 1e12:
		sec = v / 1e3
	case v >= 1e9:
		sec = v
	default:
		return time.Time{}
	}
	if sec > 2e10 { // past year 2600: not a real provider stamp
		return time.Time{}
	}
	return time.Unix(int64(sec), 0).UTC()
}

// zaiParseTimestamp parses an RFC3339 timestamp, or a naive
// "2006-01-02 15:04:05" stamp interpreted in the provider's Beijing wall
// clock (UTC+8), which is how Z.AI's documented "limit will reset at
// {next_flush_time}" message template advertises quota resets. Stamps
// outside zaiPlausibleResetBounds are rejected so an implausible field or
// an incidental date in prose can never become a reset instant.
func zaiParseTimestamp(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	var t time.Time
	var err error
	if t, err = time.Parse(time.RFC3339, s); err != nil {
		for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
			if t, err = time.ParseInLocation(layout, s, zaiProviderLocation); err == nil {
				break
			}
		}
	}
	if err != nil {
		return time.Time{}, false
	}
	t = t.UTC()
	lo, hi := zaiPlausibleResetBounds[0], zaiPlausibleResetBounds[1]
	if t.Before(lo) || t.After(hi) {
		return time.Time{}, false
	}
	return t, true
}

// zaiResetTimeFromMessage extracts the reset instant from the first
// wall-clock stamp embedded in message text. The message carries a single
// stamp in practice, so an implausible first match is treated as absent
// rather than mined for later matches. Only the parsed instant survives;
// the message itself is discarded.
func zaiResetTimeFromMessage(msg string) time.Time {
	match := zaiMessageResetPattern.FindString(msg)
	if match == "" {
		return time.Time{}
	}
	t, ok := zaiParseTimestamp(match)
	if !ok {
		return time.Time{}
	}
	return t
}

// zaiRetryAfterFromRaw converts a raw retry_after value into a duration of
// seconds. Non-positive values and magnitudes beyond a week — no Z.AI
// window is longer — are treated as absent.
func zaiRetryAfterFromRaw(raw json.RawMessage) time.Duration {
	v, ok := zaiFloatFromRaw(raw)
	if !ok || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v <= 0 || v > 7*24*3600 {
		return 0
	}
	return time.Duration(v * float64(time.Second))
}

// zaiFloatFromRaw reads a JSON number (or numeric string) as a float.
func zaiFloatFromRaw(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(unquoteJSONString(raw)), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// unquoteJSONString returns the string content of a raw JSON string, or the
// raw token text itself for numbers and other literals.
func unquoteJSONString(raw json.RawMessage) string {
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return ""
		}
		return s
	}
	return string(raw)
}
