# Regression Test Quick Reference Card

## 🎯 Purpose
Prevent future breakage of token counting functionality by maintaining a comprehensive regression test suite.

## 📊 Status
- **Total Coverage:** ~95%+ (Target: 90%+) ✅
- **Regression Tests:** 9 test functions, 38+ scenarios
- **Total Test Code:** 2,609 lines across 4 test files

---

## ⚡ Quick Commands

### Run Regression Tests
```bash
# All regression tests
go test -v -run "^TestRegression_" -timeout 30m

# Specific test
go test -v -run "TestRegression_BasicTokenCounts"

# With coverage
go test -v -cover -coverprofile=coverage.out -run "^TestRegression_"

# Automated runner (full suite + coverage report)
./tests/run_regression_tests.sh
```

### Run in Docker (No Go Installed)
```bash
docker build -t zai-proxy:test .
docker run --rm zai-proxy:test go test -v -run "^TestRegression_"
```

---

## 📝 Adding a Test Case

### 1. Choose Category

| Category | When to Use | Test Function |
|----------|-------------|---------------|
| **BasicTokenCounts** | Golden test cases with known good outputs | `TestRegression_BasicTokenCounts()` |
| **EdgeCases** | Edge cases that could crash or fail | `TestRegression_EdgeCases()` |
| **RequestParsing** | Request body parsing edge cases | `TestRegression_RequestParsing()` |
| **StreamingResponses** | SSE streaming token counting | `TestRegression_StreamingResponses()` |
| **JSONResponses** | Non-streaming response counting | `TestRegression_JSONResponses()` |
| **UsageInjection** | Token usage injection validation | `TestRegression_UsageInjection()` |
| **ConcurrentAccess** | Thread safety validation | `TestRegression_ConcurrentAccess()` |
| **FallbackCounter** | SimpleTokenCounter fallback | `TestRegression_FallbackCounter()` |
| **StreamingPreservation** | Streaming integrity | `TestRegression_StreamingPreservation()` |

### 2. Add Test Case

```go
// In tokenizer_regression_test.go
// Find appropriate test function and add to test cases slice

{
    name:        "Short descriptive name",
    text:        "Input text to test",
    expectedMin: 5,   // -10% tolerance
    expectedMax: 10,  // +10% tolerance
    description: "Why this exists - BD-XYZ reference",
},
```

### 3. Validate

```bash
# Run your new test
go test -v -run "TestRegression_YourCategory/Short_descriptive_name"

# Check output, adjust expectedMin/expectedMax if needed
```

### 4. Commit

```bash
git add tokenizer_regression_test.go
git commit -m "test(bd-10d): Add regression test for [feature]

Prevents re-introduction of [bug/issue]. Expected: X-Y tokens.

Co-Authored-By: Claude Worker <noreply@anthropic.com>"
git push origin main
```

---

## 🧪 Test Case Template

### Basic Token Count Test
```go
{
    name:        "Technical documentation",
    text:        "The API endpoint returns a JSON response.",
    expectedMin: 7,
    expectedMax: 11,
    description: "Technical sentence - validated in BD-XYZ",
},
```

### Edge Case Test
```go
{
    name:        "Binary data",
    text:        "\x00\x01\x02\xff\xfe",
    shouldError: false,
    description: "Binary characters - must not crash",
},
```

### Streaming Response Test
```go
{
    name: "Code block stream",
    response: `data: {"type":"content_block_delta","delta":{"text":"def hello():\n"}}

data: {"type":"content_block_delta","delta":{"text":"    return 42\n"}}
`,
    expectedMin: 6,
    expectedMax: 12,
    description: "Code with formatting in streaming response",
},
```

---

## 📏 Expected Value Guidelines

| Text Length | Tolerance | Example |
|-------------|-----------|---------|
| <10 tokens | ±1 token | min: 4, max: 6 for ~5 tokens |
| 10-100 tokens | ±10% | min: 45, max: 55 for ~50 tokens |
| >100 tokens | ±15% | min: 85, max: 115 for ~100 tokens |

---

## ✅ Best Practices

### DO:
- ✅ Use table-driven tests
- ✅ Set realistic token ranges (not exact counts)
- ✅ Include description with BD-XXX reference
- ✅ Log success cases with `t.Logf()`
- ✅ Validate errors are handled gracefully
- ✅ Add test for every bug fix
- ✅ Run tests before committing

### DON'T:
- ❌ Use exact token counts (brittle)
- ❌ Ignore errors silently
- ❌ Hardcode large text (use `strings.Repeat()`)
- ❌ Skip validation of expected values
- ❌ Commit without running tests
- ❌ Add tests without descriptions

---

## 🐛 Debugging Failed Tests

### Token Count Out of Range
```
FAIL: Got 45 tokens, expected 38-42
```
**Fix:** Check actual output, adjust `expectedMin/expectedMax` if text/encoding changed

### TikToken Not Available
```
Skipping regression tests: TikToken not available
```
**Fix:** Run `go mod download && go mod tidy`, rebuild

### Race Condition
```
WARNING: DATA RACE
```
**Fix:** Run `go test -race -run TestName` to identify, add mutex protection

---

## 📂 File Structure

```
zai-proxy/
├── tokenizer.go                      # Implementation (294 lines)
├── tokenizer_regression_test.go      # Regression suite (712 lines) ← ADD TESTS HERE
├── tokenizer_test.go                 # Unit tests (565 lines)
├── main_test.go                      # Integration tests (499 lines)
├── comprehensive_tokenizer_tests.go  # End-to-end tests (533 lines)
├── tests/
│   ├── README.md                     # Test overview
│   ├── COVERAGE_REPORT.md            # Coverage metrics
│   └── run_regression_tests.sh       # Automated test runner
└── docs/
    ├── REGRESSION_TEST_GUIDE.md      # Complete guide
    └── REGRESSION_TEST_QUICKREF.md   # This file
```

---

## 📚 Documentation

- **[Regression Test Guide](./REGRESSION_TEST_GUIDE.md)** - Complete testing guide
- **[Coverage Report](../tests/COVERAGE_REPORT.md)** - Coverage metrics and validation
- **[Tests README](../tests/README.md)** - Test suite overview

---

## 🎯 Coverage Targets

| Component | Target | Current | Status |
|-----------|--------|---------|--------|
| Token counting core | 100% | 100% | ✅ |
| Request parsing | 95%+ | 98% | ✅ |
| Response parsing | 95%+ | 97% | ✅ |
| Edge cases | 90%+ | 95% | ✅ |
| **Overall** | **90%+** | **95%+** | ✅ |

---

**Last Updated:** 2026-02-08
**Task:** BD-10D - Create regression test suite
**Status:** ✅ Complete (95%+ coverage achieved)
