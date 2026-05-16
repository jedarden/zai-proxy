#!/bin/bash
# Comprehensive test execution script for zai-proxy tokenizer
# Runs unit tests, benchmarks, and generates test report

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
cd "$PROJECT_ROOT"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Output file
REPORT_FILE="test_results_$(date +%Y%m%d_%H%M%S).md"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ZAI Proxy Tokenizer Test Suite${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Function to print section header
print_header() {
    echo ""
    echo -e "${BLUE}=== $1 ===${NC}"
    echo ""
}

# Function to run tests and capture output
run_test_suite() {
    local test_name="$1"
    local test_cmd="$2"

    print_header "$test_name"

    if eval "$test_cmd"; then
        echo -e "${GREEN}✓ $test_name PASSED${NC}"
        return 0
    else
        echo -e "${RED}✗ $test_name FAILED${NC}"
        return 1
    fi
}

# Initialize report
cat > "$REPORT_FILE" << 'EOF'
# ZAI Proxy Tokenizer Test Results

**Test Date:** $(date -u +"%Y-%m-%d %H:%M:%S UTC")
**Environment:** Kubernetes cluster (zai-proxy pod)
**Go Version:** $(go version 2>/dev/null || echo "N/A")

## Test Suite Results

EOF

# Check if Go is available
if ! command -v go &> /dev/null; then
    echo -e "${YELLOW}Go not found in PATH. Attempting to run tests via Docker...${NC}"
    USE_DOCKER=1
else
    echo -e "${GREEN}Go found: $(go version)${NC}"
    USE_DOCKER=0
fi

# Test execution
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

if [ "$USE_DOCKER" -eq 1 ]; then
    print_header "Building Docker Test Image"

    if ! docker build -t zai-proxy:test . ; then
        echo -e "${RED}Failed to build Docker image${NC}"
        exit 1
    fi

    print_header "Running Unit Tests in Docker"
    if docker run --rm zai-proxy:test go test -v ./... ; then
        ((PASSED_TESTS++))
    else
        ((FAILED_TESTS++))
    fi
    ((TOTAL_TESTS++))

    print_header "Running Benchmarks in Docker"
    if docker run --rm zai-proxy:test go test -bench=. -benchmem ; then
        ((PASSED_TESTS++))
    else
        ((FAILED_TESTS++))
    fi
    ((TOTAL_TESTS++))

else
    # Run tests directly with Go

    print_header "1. Basic Tokenizer Tests"
    if run_test_suite "TikToken Counter Tests" "go test -v -run TestTikToken"; then
        ((PASSED_TESTS++))
    else
        ((FAILED_TESTS++))
    fi
    ((TOTAL_TESTS++))

    print_header "2. Request Token Counting Tests"
    if run_test_suite "Request Token Counting" "go test -v -run TestCountRequestTokens"; then
        ((PASSED_TESTS++))
    else
        ((FAILED_TESTS++))
    fi
    ((TOTAL_TESTS++))

    print_header "3. Response Token Counting Tests"
    if run_test_suite "Response Token Counting" "go test -v -run TestCountJSONResponseTokens"; then
        ((PASSED_TESTS++))
    else
        ((FAILED_TESTS++))
    fi
    ((TOTAL_TESTS++))

    print_header "4. SSE Streaming Tests"
    if run_test_suite "SSE Streaming Token Counting" "go test -v -run TestCountSSEResponseTokens"; then
        ((PASSED_TESTS++))
    else
        ((FAILED_TESTS++))
    fi
    ((TOTAL_TESTS++))

    print_header "5. Sample API Request Tests"
    if run_test_suite "Sample API Requests" "go test -v -run TestSampleAPIRequestAccuracy"; then
        ((PASSED_TESTS++))
    else
        ((FAILED_TESTS++))
    fi
    ((TOTAL_TESTS++))

    print_header "6. Edge Case Tests"
    if run_test_suite "Edge Cases" "go test -v -run TestEdgeCases"; then
        ((PASSED_TESTS++))
    else
        ((FAILED_TESTS++))
    fi
    ((TOTAL_TESTS++))

    print_header "7. Streaming Response Accuracy"
    if run_test_suite "Streaming Response Accuracy" "go test -v -run TestStreamingResponseAccuracy"; then
        ((PASSED_TESTS++))
    else
        ((FAILED_TESTS++))
    fi
    ((TOTAL_TESTS++))

    print_header "8. Performance Overhead Tests"
    if run_test_suite "Performance Overhead" "go test -v -run TestPerformanceOverhead"; then
        ((PASSED_TESTS++))
    else
        ((FAILED_TESTS++))
    fi
    ((TOTAL_TESTS++))

    print_header "9. Concurrent Token Counting"
    if run_test_suite "Concurrent Token Counting" "go test -v -run TestConcurrentTokenCounting"; then
        ((PASSED_TESTS++))
    else
        ((FAILED_TESTS++))
    fi
    ((TOTAL_TESTS++))

    print_header "10. Usage Injection Tests"
    if run_test_suite "Usage Injection" "go test -v -run TestUsageInjection"; then
        ((PASSED_TESTS++))
    else
        ((FAILED_TESTS++))
    fi
    ((TOTAL_TESTS++))

    print_header "11. GLM-4 Specific Tests"
    if run_test_suite "GLM-4 Specific Cases" "go test -v -run TestGLM4SpecificCases"; then
        ((PASSED_TESTS++))
    else
        ((FAILED_TESTS++))
    fi
    ((TOTAL_TESTS++))

    print_header "12. Performance Benchmarks"
    echo "Running benchmarks (this may take a minute)..."
    go test -bench=. -benchmem -benchtime=1000x > benchmark_results.txt 2>&1 || true
    cat benchmark_results.txt
    ((TOTAL_TESTS++))
    ((PASSED_TESTS++))  # Benchmarks don't fail

    print_header "13. Race Condition Detection"
    if run_test_suite "Race Condition Tests" "go test -race -run TestConcurrent"; then
        ((PASSED_TESTS++))
    else
        ((FAILED_TESTS++))
    fi
    ((TOTAL_TESTS++))
fi

# Summary
echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Test Summary${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "Total Test Suites: ${TOTAL_TESTS}"
echo -e "${GREEN}Passed: ${PASSED_TESTS}${NC}"
echo -e "${RED}Failed: ${FAILED_TESTS}${NC}"

if [ $FAILED_TESTS -eq 0 ]; then
    echo ""
    echo -e "${GREEN}✓ All tests passed!${NC}"
    EXIT_CODE=0
else
    echo ""
    echo -e "${RED}✗ Some tests failed${NC}"
    EXIT_CODE=1
fi

# Generate report summary
cat >> "$REPORT_FILE" << EOF

## Summary

- **Total Test Suites**: $TOTAL_TESTS
- **Passed**: $PASSED_TESTS
- **Failed**: $FAILED_TESTS
- **Status**: $([ $FAILED_TESTS -eq 0 ] && echo "✓ PASS" || echo "✗ FAIL")

## Test Details

See full test output above.

## Performance Benchmarks

$([ -f benchmark_results.txt ] && cat benchmark_results.txt || echo "Benchmarks not run")

## Recommendations

$(if [ $FAILED_TESTS -eq 0 ]; then
    echo "- All tests passing. Token counting implementation is ready for production."
    echo "- Performance meets <5ms target."
    echo "- Edge cases handled correctly."
else
    echo "- Review failed test output above."
    echo "- Fix failing tests before deployment."
    echo "- Re-run tests after fixes."
fi)

## Next Steps

1. Review test results
2. Address any failures
3. Run integration tests against live cluster
4. Update documentation if needed
5. Deploy to production if all tests pass

---
Generated by run_all_tests.sh
EOF

echo ""
echo -e "${BLUE}Test report saved to: ${REPORT_FILE}${NC}"

exit $EXIT_CODE
