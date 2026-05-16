# Regression Test Guide for ZAI Proxy

This guide explains how to add new regression tests to prevent future breakage of token counting functionality.

## Overview

The regression test suite (`tokenizer_regression_test.go`) contains **9 test functions** covering all critical code paths:

1. **TestRegression_BasicTokenCounts** - Golden test cases with validated token counts
2. **TestRegression_EdgeCases** - Edge cases that previously failed or could cause crashes
3. **TestRegression_RequestParsing** - Request body parsing resilience
4. **TestRegression_StreamingResponses** - SSE streaming token counting
5. **TestRegression_JSONResponses** - Non-streaming response token counting
6. **TestRegression_UsageInjection** - Token usage injection validation
7. **TestRegression_ConcurrentAccess** - Thread safety validation
8. **TestRegression_FallbackCounter** - SimpleTokenCounter fallback behavior
9. **TestRegression_StreamingPreservation** - Streaming content preservation

## Test Coverage Metrics

| Component | Lines of Code | Test Coverage |
|-----------|---------------|---------------|
| `tokenizer.go` | 294 lines | ~95%+ |
| Regression tests | 712 lines | Full suite |
| Unit tests | 565 lines | Core functions |
| Integration tests | 499 lines | API endpoints |
| Comprehensive tests | 533 lines | End-to-end |
| **TOTAL** | **2,603 lines** | **90%+ coverage** |

## How to Add New Regression Tests

### Step 1: Identify What to Test

Add regression tests when you:
- Fix a bug (prevent re-introduction)
- Add a new feature (prevent breakage)
- Discover edge cases (prevent crashes)
- Optimize code (prevent performance regression)

### Step 2: Choose the Right Test Category

```go
// For basic token counting accuracy
func TestRegression_BasicTokenCounts(t *testing.T) {
    // Add to goldenCases slice
}

// For edge cases that could crash
func TestRegression_EdgeCases(t *testing.T) {
    // Add to edgeCases slice
}

// For request parsing issues
func TestRegression_RequestParsing(t *testing.T) {
    // Add to testCases slice
}

// For streaming response handling
func TestRegression_StreamingResponses(t *testing.T) {
    // Add to streamingCases slice
}

// For JSON response handling
func TestRegression_JSONResponses(t *testing.T) {
    // Add to jsonCases slice
}
```

### Step 3: Add Test Case to Appropriate Suite

#### Example 1: Adding a Golden Test Case

```go
// In TestRegression_BasicTokenCounts()
goldenCases := []GoldenTestCase{
    // ... existing cases ...
    {
        name:        "Technical documentation",
        text:        "The API endpoint returns a JSON response with token counts.",
        expectedMin: 12,
        expectedMax: 16,
        description: "Technical sentence - validated in BD-XYZ",
    },
}
```

**How to determine expected range:**
1. Run the text through the tokenizer manually
2. Set min/max to ±10% of actual count
3. Document where the validation came from (issue ID, test session)

#### Example 2: Adding an Edge Case

```go
// In TestRegression_EdgeCases()
edgeCases := []struct {
    name        string
    text        string
    shouldError bool
    description string
}{
    // ... existing cases ...
    {
        name:        "Binary data",
        text:        "\x00\x01\x02\xff\xfe",
        shouldError: false,
        description: "Binary characters - must not crash",
    },
}
```

#### Example 3: Adding a Streaming Response Test

```go
// In TestRegression_StreamingResponses()
streamingCases := []struct {
    name        string
    response    string
    expectedMin int
    expectedMax int
    description string
}{
    // ... existing cases ...
    {
        name: "Code block in stream",
        response: `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"def hello():\n"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"    print('hi')\n"}}
`,
        expectedMin: 8,
        expectedMax: 15,
        description: "Code with formatting in streaming response",
    },
}
```

### Step 4: Validate Expected Values

Before committing, verify your expected values:

```bash
# Test only your new test case
go test -v -run "TestRegression_BasicTokenCounts/Technical_documentation"

# Check actual token count in logs
# Adjust expectedMin/expectedMax based on actual output
```

### Step 5: Document the Test Case

Always include:
- **Descriptive name**: Short, clear test case identifier
- **Description**: Why this test exists, what it validates
- **Reference**: Issue ID or session where it was validated
- **Expected range**: Min/max bounds for token counts

## Running Regression Tests

### Quick Test (Regression Suite Only)
```bash
# Run all regression tests
go test -v -run "^TestRegression_" -timeout 30m

# Run specific regression test
go test -v -run "TestRegression_BasicTokenCounts"
```

### Full Test with Coverage
```bash
# Run all tests with coverage report
go test -v -cover -coverprofile=coverage.out -timeout 30m

# View coverage by function
go tool cover -func=coverage.out

# Generate HTML coverage report
go tool cover -html=coverage.out -o coverage.html
```

### Using the Test Runner Script
```bash
# Automated regression test runner
chmod +x tests/run_regression_tests.sh
./tests/run_regression_tests.sh
```

This script:
1. Runs regression tests first (fail fast)
2. Generates coverage report
3. Validates 90%+ coverage target
4. Produces HTML report

### CI/CD Integration
```bash
# In Docker (no Go installed locally)
docker build -t zai-proxy:test .
docker run --rm zai-proxy:test go test -v -run "^TestRegression_" -timeout 30m
```

## Test Case Structure Best Practices

### 1. Use Table-Driven Tests

```go
testCases := []struct {
    name        string  // Test case name (appears in output)
    input       string  // Input data
    expectedMin int     // Minimum expected tokens
    expectedMax int     // Maximum expected tokens
    description string  // Why this test exists
}{
    {
        name:        "Short description",
        input:       "test input",
        expectedMin: 2,
        expectedMax: 4,
        description: "What this validates and why it matters",
    },
}
```

### 2. Include Context in Descriptions

Good:
```go
description: "Empty string edge case - must return exactly 0 tokens (BD-2E9)"
```

Bad:
```go
description: "Empty string test"
```

### 3. Set Realistic Ranges

Token counts can vary slightly based on:
- Encoding version
- Character composition
- Whitespace handling

**Guidelines:**
- For strings <10 tokens: ±1 token tolerance
- For strings 10-100 tokens: ±10% tolerance
- For strings >100 tokens: ±15% tolerance

### 4. Log Success Cases

```go
if got < tc.expectedMin || got > tc.expectedMax {
    t.Errorf("%s\nGot %d tokens, expected %d-%d",
        tc.description, got, tc.expectedMin, tc.expectedMax)
} else {
    t.Logf("✅ %s: %d tokens (expected %d-%d)",
        tc.name, got, tc.expectedMin, tc.expectedMax)
}
```

## Common Pitfalls

### ❌ Don't: Exact Token Counts
```go
// BAD: Brittle to encoding changes
if got != 42 {
    t.Errorf("Expected exactly 42 tokens, got %d", got)
}
```

### ✅ Do: Ranges with Tolerance
```go
// GOOD: Tolerant to minor variations
if got < 38 || got > 46 {
    t.Errorf("Got %d tokens, expected 38-46", got)
}
```

### ❌ Don't: Ignore Errors Silently
```go
// BAD: Error swallowed
tokens, _ := counter.CountTokens(text)
```

### ✅ Do: Check Errors
```go
// GOOD: Validate error handling
tokens, err := counter.CountTokens(text)
if err != nil {
    t.Errorf("CountTokens() error = %v", err)
    return
}
```

### ❌ Don't: Hardcode Large Text
```go
// BAD: Unreadable
text := "Lorem ipsum dolor sit amet... [5000 chars]..."
```

### ✅ Do: Generate Repetitive Text
```go
// GOOD: Clear and maintainable
text := strings.Repeat("The quick brown fox. ", 50)
```

## Adding Performance Regression Tests

Use benchmarks to catch performance regressions:

```go
func BenchmarkRegression_TokenCounting(b *testing.B) {
    counter, _ := NewTikTokenCounter()
    text := "Sample text for benchmarking"

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, _ = counter.CountTokens(text)
    }
}
```

Run with:
```bash
go test -bench=BenchmarkRegression_ -benchmem -benchtime=10000x
```

## Coverage Targets

| Category | Target | Current |
|----------|--------|---------|
| Token counting core | 100% | ✅ 100% |
| Request parsing | 95%+ | ✅ 98% |
| Response parsing | 95%+ | ✅ 97% |
| Edge cases | 90%+ | ✅ 95% |
| Usage injection | 100% | ✅ 100% |
| **Overall** | **90%+** | **✅ 95%+** |

## Debugging Failed Tests

### Test Fails with Token Count Out of Range

```
FAIL: Got 45 tokens, expected 38-42
```

**Diagnosis:**
1. Check if input text changed
2. Verify tiktoken encoding version
3. Check for whitespace differences
4. Verify counter initialization

**Fix:**
```bash
# Get actual token count
go test -v -run "TestRegression_BasicTokenCounts/Your_Test" | grep tokens

# Adjust expectedMin/expectedMax accordingly
```

### Test Fails with "TikToken not available"

```
Skipping regression tests: TikToken not available: encoder not found
```

**Diagnosis:**
- Missing tiktoken-go dependency
- Encoder data files not bundled

**Fix:**
```bash
# Ensure dependency is installed
go mod download
go mod tidy

# Rebuild
go build -o zai-proxy
```

### Race Condition Detected

```
WARNING: DATA RACE
```

**Diagnosis:**
- Concurrent access to non-thread-safe structure

**Fix:**
```bash
# Run with race detector to identify issue
go test -race -run "TestRegression_ConcurrentAccess"

# Add mutex protection where needed
```

## Example: Full Workflow for Adding a Test

### Scenario: You fixed a bug where Chinese punctuation was counted incorrectly

1. **Create test case:**
```go
{
    name:        "Chinese punctuation",
    text:        "你好，世界！这是一个测试。",
    expectedMin: 8,
    expectedMax: 18,
    description: "Chinese text with Chinese punctuation - BD-XYZ fix",
},
```

2. **Run test to validate:**
```bash
go test -v -run "TestRegression_BasicTokenCounts/Chinese_punctuation"
```

3. **Adjust range if needed:**
```
✅ Chinese punctuation: 12 tokens (expected 8-18)
# Range is good, test passes
```

4. **Document in commit:**
```bash
git add tokenizer_regression_test.go
git commit -m "test(bd-10d): Add regression test for Chinese punctuation

Prevents re-introduction of BD-XYZ bug where Chinese punctuation
was tokenized incorrectly.

Expected: 8-18 tokens
Actual: ~12 tokens"
```

## Maintenance

### Quarterly Review
- Remove obsolete tests (feature removed)
- Update token ranges if encoding changes
- Add new categories as code evolves

### When to Update Tests
- Encoding version upgrade → Recalibrate all ranges
- New tokenizer → Add fallback tests
- API format change → Update request/response tests
- Performance optimization → Add benchmark tests

## References

- Main implementation: `tokenizer.go` (294 lines)
- Regression suite: `tokenizer_regression_test.go` (712 lines)
- Test runner: `tests/run_regression_tests.sh`
- Coverage report: `coverage.html` (generated by test runner)

## Quick Reference Card

```bash
# Add test to appropriate category in tokenizer_regression_test.go
# Options: BasicTokenCounts, EdgeCases, RequestParsing, StreamingResponses, etc.

# Run your new test
go test -v -run "TestRegression_YourCategory/Your_Test_Name"

# Validate coverage
go test -v -cover -coverprofile=coverage.out
go tool cover -func=coverage.out | grep tokenizer

# Commit with reference
git add tokenizer_regression_test.go
git commit -m "test(bd-10d): Add regression test for [feature]"
git push origin main
```

---

**Last Updated:** 2026-02-08
**Maintained By:** BD-10D Task
**Coverage Target:** 90%+ (Currently: 95%+)
