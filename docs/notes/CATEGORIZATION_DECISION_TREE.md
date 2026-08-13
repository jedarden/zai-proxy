# Test Failure Categorization Decision Tree

This document explains the decision tree used for categorizing test failures in the zai-proxy project.

## Overview

The categorization system uses a priority-based pattern matching approach to classify test failures into specific categories. Each category has defined patterns, priority levels, and confidence scores to handle ambiguous cases consistently.

## Decision Tree Logic

### Step 1: Data Race Detection (Priority: 100)
**First check:** Does the error contain race detector output?

- **Pattern:** `WARNING: DATA RACE`, `Write at`, `Previous`, `data race`
- **Category:** `CategoryDataRace`
- **Confidence:** 1.0 (100%)
- **Reasoning:** Data races have unique, unambiguous output from the race detector

### Step 2: Deadlock Detection (Priority: 90)
**Second check:** Does the error indicate potential deadlock?

- **Pattern:** `potential deadlock`, `deadlock detected`
- **Category:** `CategoryDeadlock`
- **Confidence:** 1.0 (100%)
- **Reasoning:** Deadlock detection is specific and unambiguous

### Step 3: Timeout Detection (Priority: 70)
**Fourth check:** Did the operation exceed a time limit?

- **Pattern:** `context deadline exceeded`, `timeout.*exceeded`, `timed out`, `timeout waiting for`, `test timed out`, `exceeded.*timeout`
- **Category:** `CategoryTimeout`
- **Confidence:** 0.95 (95%)
- **Reasoning:** Timeouts have clear terminology but can sometimes appear in error messages

### Step 4: Nil Pointer Detection (Priority: 65)
**Fourth check:** Is this a nil/null pointer dereference?

- **Pattern:** `null pointer`, `nil pointer dereference`, `panic on nil pointer`
- **Category:** `CategoryNilPointer`
- **Confidence:** 1.0 (100%)
- **Reasoning:** Nil pointer errors have explicit, unambiguous messages

### Step 5: Index Out of Range (Priority: 60)
**Fifth check:** Is this an array/slice bounds error?

- **Pattern:** `index out of range`, `slice bounds out of range`
- **Category:** `CategoryIndexOutOfRange`
- **Confidence:** 1.0 (100%)
- **Reasoning:** Index errors have explicit messages

### Step 6: Map Key Errors (Priority: 55)
**Sixth check:** Is this a map key access issue?

- **Pattern:** `map key.*not found`, `zero map key`, `key not found`
- **Category:** `CategoryMapKey`
- **Confidence:** 0.95 (95%)
- **Reasoning:** Map errors are specific but key-related phrasing can vary

### Step 7: Channel Errors (Priority: 50)
**Seventh check:** Is this a channel operation error?

- **Pattern:** `send on closed channel`, `close of closed channel`, `channel.*closed`, `receive on closed channel`
- **Category:** `CategoryChannel`
- **Confidence:** 1.0 (100%)
- **Reasoning:** Channel errors have explicit, unambiguous messages

### Step 8: Goroutine Panic Detection (Priority: 55)
**Eighth check:** Does the error involve goroutine issues?

- **Pattern:** `goroutine [running]:`, `leaked goroutine`, `goroutines? created`
- **Category:** `CategoryGoroutinePanic`
- **Confidence:** 0.9 (90%)
- **Reasoning:** Goroutine-specific failures are distinct from general panics

### Step 9: Panic Detection (Priority: 50)
**Ninth check:** Is this a general runtime panic?

- **Pattern:** `panic:`, `runtime panic`, `panic()`
- **Category:** `CategoryPanic`
- **Subcategory:** `runtime_panic`
- **Confidence:** 1.0 (100%)
- **Reasoning:** Panics are explicit and checked before type errors to prevent misclassification

### Step 10: Type Mismatch (Priority: 45)
**Tenth check:** Is this a type conversion/assertion failure?

- **Pattern:** `type.*interface`, `interface conversion`, `cannot convert.*type`, `type mismatch`, `type assertion`
- **Category:** `CategoryTypeMismatch`
- **Confidence:** 0.9 (90%)
- **Reasoning:** Type errors have clear patterns but terminology varies

### Step 11: HTTP/Network Errors (Priority: 40)
**Eleventh check:** Is this an HTTP or network communication error?

- **Pattern:** `http.*status`, `status code`, `connection refused`, `connection reset`, `http.*error`, `dial.*tcp`, `connection timeout`
- **Category:** `CategoryHTTPError`
- **Subcategory:** `network`
- **Confidence:** 0.85 (85%)
- **Reasoning:** Network errors have clear patterns but overlap with other I/O errors

### Step 12: I/O Errors (Priority: 35)
**Twelfth check:** Is this a general I/O operation error?

- **Pattern:** `no such file`, `directory.*not found`, `file not found`, `permission denied`, `i/o error`, `read.*failed`, `write.*failed`
- **Category:** `CategoryIOError`
- **Confidence:** 0.9 (90%)
- **Reasoning:** I/O errors are specific but can vary by operation type

### Step 13: Assertion Errors (Priority: 10)
**Thirteenth check:** Does this appear to be an assertion/expectation failure?

- **Pattern:** `assertion.*failed`, `expected.*but.*got`, `not equal`, `should.*be`, `want.*got`, `expected.*got`, `assert`
- **Category:** `CategoryAssertionError`
- **Confidence:** 0.7 (70%)
- **Reasoning:** Assertion-like patterns can appear in various contexts, so this is checked last as a fallback

### Step 14: Unknown (Priority: 0)
**Default:** If no patterns match, categorize as unknown.

- **Category:** `CategoryUnknown`
- **Confidence:** 0.0 (0%)
- **Reasoning:** No recognizable pattern - requires manual review

## Handling Ambiguous Cases

The priority system resolves ambiguity by checking specific patterns before general ones:

### Example 1: Panic vs. Type Mismatch
If an error contains "panic: interface conversion":
- **Result:** `CategoryPanic` (priority 50) wins over `CategoryTypeMismatch` (priority 45)
- **Rationale:** Panic is more fundamental than type conversion errors

### Example 2: Panic vs. Assertion
If an error contains both "panic:" and "expected...got":
- **Result:** `CategoryPanic` (priority 50) wins over `CategoryAssertionError` (priority 10)
- **Rationale:** Runtime panic is more specific and takes precedence

### Example 3: Data Race vs. Assertion
If an error contains both "WARNING: DATA RACE" and "assertion failed":
- **Result:** `CategoryDataRace` (priority 100) wins over `CategoryAssertionError` (priority 10)
- **Rationale:** Data race is a critical concurrency issue that supersedes assertion failures

### Example 4: Timeout vs. HTTP Error
If an error contains both "connection timeout" and "context deadline exceeded":
- **Result:** `CategoryTimeout` (priority 70) wins over `CategoryHTTPError` (priority 40)
- **Rationale:** Timeout is more fundamental than the specific HTTP error

### Example 5: HTTP Error vs. I/O Error
If an error contains both "dial tcp" and "connection refused":
- **Result:** `CategoryHTTPError` (priority 40) wins over `CategoryIOError` (priority 35)
- **Rationale:** Network/HTTP errors are more specific than general I/O errors

## Category Definitions and Examples

### 1. AssertionError (assertion_error)
**Definition:** Test expectations that don't match actual values or conditions.

**Examples:**
- `assertion failed: expected 200, got 500`
- `expected success, got error`
- `values not equal: 42 != 43`
- `response should be OK`
- `want true, got false`

**Edge cases:** 
- General assertion patterns checked last to avoid misclassifying specific errors
- Confidence 0.7 due to potential false positives

### 2. Timeout (timeout)
**Definition:** Operations that exceed their time limits.

**Examples:**
- `context deadline exceeded`
- `test timed out after 5s`
- `timeout waiting for response`
- `operation exceeded 30s timeout`

**Edge cases:**
- Connection timeouts categorized as HTTP errors (priority 40) not general timeouts
- High confidence (0.95) due to explicit timeout terminology

### 3. Panic (panic)
**Definition:** Runtime panic conditions that crash the program.

**Examples:**
- `panic: division by zero`
- `runtime panic: invalid memory address`
- `panic("something went wrong")`

**Edge cases:**
- Specific panic types (nil pointer, index out of range) checked before general panic
- Goroutine panics categorized separately as `CategoryGoroutinePanic`
- Confidence 1.0 due to explicit panic markers

### 4. DataRace (data_race)
**Definition:** Concurrent memory access detected by the race detector.

**Examples:**
```
WARNING: DATA RACE
Write at 0x... by goroutine 7:
  previous test
  /path/to/test.go:45
Read at 0x... by main goroutine:
  current test
  /path/to/test.go:50
```

**Edge cases:**
- Highest priority (100) due to critical nature of concurrency bugs
- Confidence 1.0 due to unambiguous race detector output

### 5. NilPointer (nil_pointer_dereference)
**Definition:** Attempts to dereference a nil/null pointer.

**Examples:**
- `nil pointer dereference`
- `null pointer access`
- `panic on nil pointer`

**Edge cases:**
- Higher priority (65) than general panic (25) to ensure specificity
- Confidence 1.0 due to explicit nil pointer messages

### 6. TypeMismatch (type_mismatch)
**Definition:** Type conversion or type assertion failures.

**Examples:**
- `interface conversion: interface {} is string, not int`
- `type mismatch: cannot convert string to int`
- `type assertion failed`

**Edge cases:**
- Confidence 0.9 due to varied type error terminology
- Medium priority (45) as type errors are specific but common

### 7. IndexOutOfRange (index_out_of_range)
**Definition:** Array or slice index beyond bounds.

**Examples:**
- `index out of range [5] with length 3`
- `slice bounds out of range [0:10] with length 5`

**Edge cases:**
- High confidence (1.0) due to explicit bounds messages
- Priority 60, checked before general panics

### 8. MapKey (map_key_error)
**Definition:** Map key access issues.

**Examples:**
- `map key not found`
- `zero map key in map access`
- `key not found in map`

**Edge cases:**
- Confidence 0.95 due to specific but varied key-related phrasing
- Priority 55

### 9. Channel (channel_error)
**Definition:** Channel operation errors.

**Examples:**
- `send on closed channel`
- `close of closed channel`
- `receive on closed channel`

**Edge cases:**
- High confidence (1.0) due to explicit channel operation messages
- Priority 50

### 10. GoroutinePanic (goroutine_panic)
**Definition:** Goroutine-specific failures or leaks.

**Examples:**
- `goroutine 1 [running]:`
- `leaked goroutine detected`
- `goroutines created without cleanup`

**Edge cases:**
- Confidence 0.9 due to goroutine-specific stack trace patterns
- Priority 24, checked before general panic

### 11. Deadlock (deadlock)
**Definition:** Potential deadlock conditions.

**Examples:**
- `potential deadlock detected`
- `deadlock detected in goroutine`

**Edge cases:**
- High confidence (1.0) due to explicit deadlock detection
- Priority 90,仅次于 data races

### 12. IOError (io_error)
**Definition:** General I/O operation failures.

**Examples:**
- `no such file or directory: /tmp/test.txt`
- `permission denied reading file`
- `i/o error: read failed`

**Edge cases:**
- Confidence 0.9 due to specific I/O error patterns
- Priority 35, after network-specific errors

### 13. HTTPError (http_error)
**Definition:** HTTP/network communication errors.

**Examples:**
- `HTTP status code 500`
- `dial tcp 127.0.0.1:8080: connection refused`
- `connection timeout`

**Edge cases:**
- Subcategory: `network` for additional specificity
- Confidence 0.85 due to network error variability
- Priority 40, above general I/O errors

### 14. Unknown (unknown)
**Definition:** Failures that don't match any known pattern.

**Examples:**
- `something completely unexpected happened`
- Unusual custom error messages

**Edge cases:**
- Zero confidence - requires manual review
- Used as default category when no patterns match

## Usage Guidelines

### For Manual Categorization
1. Start at the top of the decision tree (data races)
2. Check each pattern in priority order
3. Use the first matching category
4. Document reasoning if confidence is below 0.8

### For Adding New Categories
1. Define clear, specific patterns
2. Set appropriate priority (higher = more specific)
3. Assign confidence based on pattern ambiguity
4. Add comprehensive test cases
5. Update this decision tree documentation

### For Debugging Categorization
1. Check the reasoning field in `CategorizedFailure`
2. Verify priority order if multiple patterns match
3. Use `PrintCategorizationReport` to see all categorizations
4. Review ambiguous cases manually when confidence < 0.8

## Implementation Notes

The decision tree is implemented in `proxy/testutil/categorization.go` using:
- Priority-sorted rules array
- Regex pattern matching
- Confidence scoring
- Automatic reasoning generation

See the source code for detailed implementation patterns and additional edge cases.
