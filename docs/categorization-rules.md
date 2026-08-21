# Test Failure Categorization Rules

This document defines the stable taxonomy and decision rules for classifying
`go test` failures. It is the contract for `testutil.CategorizeFailure` and
for any importer that produces the same failure records.

The classifier is diagnostic, not a replacement for the original failure
text. It chooses one primary category for grouping and triage, while retaining
the original error message, stack trace, every matching rule in the reasoning,
and a confidence value.

## Input and result contract

The input is a failure's `ErrorMessage` plus `StackTrace`, separated by a
newline when both exist. Detection is case-insensitive and searches the
combined text, so a marker may be in either the test's error line or a stack
trace. Patterns below are Go/RE2-style regular expressions unless quoted as
literal keywords.

The result has these fields:

| Field | Meaning |
| --- | --- |
| `type` / `category` | The primary category ID. Both fields have the same value; `type` is the concise API name. |
| `subcategory` | Optional additional detail. It is a lowercase snake-case value and must not change the meaning of the primary category. |
| `confidence` | A value from 0.0 through 1.0. An exact runtime marker is normally high confidence; broad assertion text is deliberately lower confidence. |
| `uncertain` | `true` when confidence is at or below 0.70. `unknown` is always uncertain. |
| `reasoning` | The matched pattern and any competing matches or resolution applied. |

`unknown` is serialized by the implementation and displayed to users as
**Other**. Every Other result must have a subcategory before it is stored in a
triage report. Use one of `unclassified`, `parser`, `test_framework`,
`environment`, or a short, stable, snake-case label supplied by the reviewer.
For example, an unrecognized failure initially becomes
`{ "type": "unknown", "subcategory": "unclassified" }`. The raw
classifier may omit an empty subcategory; the report writer or reviewer is
responsible for this required Other enrichment.

## Categories and detection rules

The following table lists all category IDs. The first seven are the minimum
cross-language categories expected by consumers; the additional Go-oriented
categories make common runtime failures actionable without overloading
`panic` or Other.

| Priority | Category ID (display name) | Detection rules: patterns, keywords, and stack-frame evidence | Default subcategory |
| ---: | --- | --- | --- |
| 100 | `data_race` (Data race) | Race-detector output: `WARNING: DATA RACE`, `data race`, `Write at`, or `Previous`. A normal report also contains `Read at`/`Write at` sections and goroutine frames such as `goroutine 7`. | — |
| 90 | `deadlock` (Deadlock) | `potential deadlock` or `deadlock detected`. Goroutine dumps with blocked frames support the diagnosis but do not alone match this category. | — |
| 70 | `timeout` (Timeout) | `context deadline exceeded`, `context canceled`, `timeout.*exceeded`, `timed out`, `timeout waiting for`, `timeout occurred`, `test timed out`, or `exceeded.*timeout`. The Go timeout dump (`panic: test timed out after ...` followed by goroutine frames) is also covered because the timeout text wins over the general panic marker. | — |
| 65 | `nil_pointer_dereference` (Nil pointer dereference) | `null pointer`, `nil pointer dereference`, `panic on nil pointer`, or `assignment to entry in nil map`. The latter is treated as a nil-initialization error rather than a map-key error. | `test_setup` when the text mentions setup, a mock, or a fake; otherwise — |
| 60 | `index_out_of_range` (Index or slice bounds) | `index out of range` or `slice bounds out of range`, including Go runtime stack traces that show the failing indexing frame. | — |
| 56 | `channel_error` (Channel operation) | `send on closed channel`, `close of closed channel`, `channel.*closed`, or `receive on closed channel`. A goroutine frame is supporting evidence only. | — |
| 55 | `map_key_error` (Map key) | `map key.*not found`, `zero map key`, or `key not found`. | — |
| 55 | `goroutine_panic` (Goroutine failure) | `goroutine <number> [running]`, `leaked goroutine`, or `goroutine(s) created`. This category captures goroutine-specific output even when the test runner has no standalone `panic:` line. | — |
| 50 | `panic` (Runtime panic) | `panic:`, `runtime panic`, `panic()`, `panic in`, or `runtime error`. Inspect the following Go stack frames to preserve the crashing function in the original failure record. | `runtime_panic` |
| 45 | `type_mismatch` (Type mismatch) | `type.*interface`, `interface conversion`, `cannot convert.*type`, `type mismatch`, or `type assertion`. A safe assertion written with `, ok` is not also an assertion-error match. | — |
| 40 | `http_error` (HTTP or network) | `http.*status`, `status code`, `connection refused`, `connection reset`, `http.*error`, `dial.*tcp`, or `connection timeout`. The `dial tcp` frame/error chain is the decisive network evidence. | `network` |
| 35 | `io_error` (I/O) | `no such file`, `directory.*not found`, `file not found`, `permission denied`, `i/o error`, `read.*failed`, `write.*failed`, or `broken pipe`. File-operation frames and `os`/filesystem paths are supporting evidence. | — |
| 10 | `assertion_error` (Assertion or expectation) | `assertion.*failed`, `expected ... (but ... got\|got\|non-nil\|to exist\|value at\|element at\|type)`, `not equal`, `should.*be`, `want.*got`, or `assert`. A test source frame such as `*_test.go:<line>` supports the match but does not override a more specific runtime failure. | — |
| 0 | `unknown` (Other) | No category rule matched. Preserve the most useful non-sensitive token, error code, or test-framework name as its reviewer-selected subcategory. | `unclassified` (required on report output) |

The literal patterns are intentionally conservative. A category is selected
because its rule matched the combined failure text; a source file name, a test
name, or a generic goroutine dump cannot create a match by itself unless the
rule above explicitly lists it.

## Priority and ambiguous failures

The classifier scans **all** rules before choosing the primary category. It
then uses the following descending order. This ordering favors root causes and
unambiguous runtime diagnostics over a test's final assertion message:

1. `data_race`
2. `deadlock`
3. `timeout`
4. `nil_pointer_dereference`
5. `index_out_of_range`
6. `channel_error`
7. `map_key_error`
8. `goroutine_panic`
9. `panic`
10. `type_mismatch`
11. `http_error`
12. `io_error`
13. `assertion_error`
14. `unknown` / Other (only when nothing matched)

`map_key_error` and `goroutine_panic` have the same numeric priority. Their
tie-break is stable declaration order: map-key error first, then goroutine
failure. Do not rely on map iteration or arrival order of log lines.

There is one intentional exception to the numeric order: if the candidate is
`nil_pointer_dereference` and any general `panic` marker also matched, choose
`panic`. The panic event is then the primary failure, while the nil-pointer
text remains in the reasoning and stack trace. `panic on nil pointer` without
a general panic marker remains a nil-pointer dereference.

Other important resolutions are:

| Competing signals | Primary result | Why |
| --- | --- | --- |
| `WARNING: DATA RACE` and assertion text | `data_race` | Race-detector output is a direct concurrency finding; assertion text is often the symptom. |
| `test timed out` and `panic:` | `timeout` | The test-runner panic describes the timeout termination mechanism, not a separate runtime defect. |
| `nil pointer dereference` and assertion text | `nil_pointer_dereference` | The runtime fault is more specific than the failed expectation. |
| `panic: interface conversion` and a type pattern | `panic` | An explicit runtime panic takes precedence; record the type evidence in reasoning. |
| Type pattern and assertion text, without panic | `type_mismatch` | Conversion/assertion semantics are more specific than a generic test assertion. |
| `dial tcp`/connection timeout and timeout text | `timeout` for the base classifier; `http_error` with subcategory `timeout` after optional ambiguity resolution | The base rule preserves the time-limit failure. `ResolveAmbiguity` may reclassify a TCP dial as a network timeout. |
| HTTP/network text and general I/O text | `http_error` | Network diagnostics are more specific than general I/O. |
| Channel error and goroutine frame | `channel_error` | A concrete illegal channel operation wins over generic goroutine context. |

When two or more rules match, retain all matches in `reasoning` and lower
confidence according to the primary rule's ambiguity adjustment. A result at
or below 0.70 is marked `uncertain` for review; it is not silently changed to
Other.

## Categorization pseudocode

```text
function categorize(failure):
    text = failure.error_message
    if failure.stack_trace is not empty:
        text = text + "\n" + failure.stack_trace

    matches = every rule whose case-insensitive pattern matches text

    # ", ok" is a safe Go type assertion; it is not a test-framework assert.
    if text contains both "type assertion" and ", ok":
        remove assertion_error from matches

    if matches is empty:
        return category(type="unknown", subcategory="unclassified",
                        confidence=0.0, uncertain=true,
                        reasoning="no categorization pattern matched")

    sort matches by priority descending, using declared order to break ties
    primary = matches[0]

    # Exception to the normal priority ordering.
    if primary.type == "nil_pointer_dereference" and
       matches contains "panic":
        primary = the matching panic rule

    confidence = primary.base_confidence
    for each non-primary match:
        apply the pair-specific ambiguity reduction, or the default reduction

    apply documented edge-case adjustments
    subcategory = primary.default_subcategory
    if primary.type == "nil_pointer_dereference" and
       text mentions setup, mock, or fake:
        subcategory = "test_setup"

    return category(primary.type, subcategory, confidence,
                    uncertain=(confidence <= 0.70), reasoning=all matches)
```

The pseudocode expresses the intended report contract. The current Go API
uses an empty `subcategory` for a raw `unknown` result because its JSON field
is `omitempty`; callers that persist or display Other must normalize it to
`unclassified` as described above.

## Review guidance

Review a result when it is Other, `uncertain`, or has multiple matching rules.
Use the stack trace to confirm the primary event, but do not recategorize a
failure solely because a test name or file path sounds related. If the same
Other subcategory occurs repeatedly, add a precise category rule and tests
before making it a new primary category.
