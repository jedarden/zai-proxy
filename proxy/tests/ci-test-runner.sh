#!/bin/bash
# CI/CD Test Runner for ZAI Proxy
# Designed for GitHub Actions, GitLab CI, or ArgoCD pre-sync hooks

set -e

echo "======================================"
echo "ZAI Proxy CI/CD Test Runner"
echo "======================================"

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
COVERAGE_TARGET=90
TIMEOUT=30m

# Change to project root
cd "$(dirname "$0")/.."

echo ""
echo -e "${BLUE}Step 1: Environment Check${NC}"
echo "======================================"

# Check Go installation
if ! command -v go &> /dev/null; then
    echo -e "${RED}❌ Go not found. Please install Go 1.21+${NC}"
    exit 1
fi

GO_VERSION=$(go version)
echo -e "${GREEN}✅ Go found: $GO_VERSION${NC}"

# Check dependencies
echo ""
echo "Checking Go dependencies..."
if [ ! -f go.mod ]; then
    echo -e "${RED}❌ go.mod not found${NC}"
    exit 1
fi

go mod download
go mod verify
echo -e "${GREEN}✅ Dependencies verified${NC}"

echo ""
echo -e "${BLUE}Step 2: Run Regression Tests${NC}"
echo "======================================"

# Run regression tests with verbose output
if go test -v -run "^TestRegression_" -timeout $TIMEOUT 2>&1 | tee /tmp/regression_output.txt; then
    echo -e "${GREEN}✅ All regression tests passed${NC}"
    REGRESSION_PASS=1
else
    echo -e "${RED}❌ Regression tests failed${NC}"
    REGRESSION_PASS=0
fi

echo ""
echo -e "${BLUE}Step 3: Run Full Test Suite${NC}"
echo "======================================"

# Run all tests with coverage
if go test -v -cover -coverprofile=coverage.out -timeout $TIMEOUT 2>&1 | tee /tmp/full_test_output.txt; then
    echo -e "${GREEN}✅ All tests passed${NC}"
    FULL_TEST_PASS=1
else
    echo -e "${RED}❌ Some tests failed${NC}"
    FULL_TEST_PASS=0
fi

echo ""
echo -e "${BLUE}Step 4: Generate Coverage Report${NC}"
echo "======================================"

if [ -f coverage.out ]; then
    # Generate function-level coverage
    go tool cover -func=coverage.out > coverage_summary.txt

    # Extract total coverage percentage
    COVERAGE=$(grep "total:" coverage_summary.txt | awk '{print $3}' | sed 's/%//')

    echo ""
    echo "Coverage Summary:"
    echo "----------------"
    cat coverage_summary.txt | tail -10
    echo ""

    echo -e "Total Coverage: ${BLUE}${COVERAGE}%${NC}"

    # Check if coverage meets target
    if (( $(echo "$COVERAGE >= $COVERAGE_TARGET" | bc -l) )); then
        echo -e "${GREEN}✅ Coverage target met: ${COVERAGE}% >= ${COVERAGE_TARGET}%${NC}"
        COVERAGE_PASS=1
    else
        echo -e "${YELLOW}⚠️  Coverage below target: ${COVERAGE}% < ${COVERAGE_TARGET}%${NC}"
        COVERAGE_PASS=0
    fi

    # Generate HTML report
    go tool cover -html=coverage.out -o coverage.html
    echo -e "${GREEN}✅ HTML coverage report: coverage.html${NC}"
else
    echo -e "${RED}❌ Coverage file not generated${NC}"
    COVERAGE_PASS=0
fi

echo ""
echo -e "${BLUE}Step 5: Run Benchmark Tests${NC}"
echo "======================================"

# Run performance benchmarks
if go test -bench=. -benchmem -benchtime=100x 2>&1 | tee /tmp/benchmark_output.txt; then
    echo -e "${GREEN}✅ Benchmarks completed${NC}"

    # Extract performance metrics
    echo ""
    echo "Performance Summary:"
    echo "-------------------"
    grep "Benchmark" /tmp/benchmark_output.txt || echo "No benchmark results"
else
    echo -e "${YELLOW}⚠️  Benchmark tests had issues${NC}"
fi

echo ""
echo -e "${BLUE}Step 6: Test Result Summary${NC}"
echo "======================================"

# Count test results
REGRESSION_COUNT=$(grep -c "^=== RUN.*Regression" /tmp/regression_output.txt 2>/dev/null || echo "0")
TOTAL_COUNT=$(grep -c "^=== RUN" /tmp/full_test_output.txt 2>/dev/null || echo "0")
PASSED=$(grep -c "^--- PASS" /tmp/full_test_output.txt 2>/dev/null || echo "0")
FAILED=$(grep -c "^--- FAIL" /tmp/full_test_output.txt 2>/dev/null || echo "0")
SKIPPED=$(grep -c "^--- SKIP" /tmp/full_test_output.txt 2>/dev/null || echo "0")

echo ""
echo "Test Statistics:"
echo "---------------"
echo "Regression tests run: $REGRESSION_COUNT"
echo "Total tests run: $TOTAL_COUNT"
echo -e "Passed: ${GREEN}$PASSED${NC}"
echo -e "Failed: ${RED}$FAILED${NC}"
echo -e "Skipped: ${YELLOW}$SKIPPED${NC}"

if [ -f coverage.out ]; then
    echo -e "Code coverage: ${BLUE}${COVERAGE}%${NC}"
fi

echo ""
echo "======================================"
echo "Final Result"
echo "======================================"

# Determine overall success
if [ $REGRESSION_PASS -eq 1 ] && [ $FULL_TEST_PASS -eq 1 ] && [ $COVERAGE_PASS -eq 1 ]; then
    echo -e "${GREEN}✅ ALL CHECKS PASSED${NC}"
    echo ""
    echo "✓ Regression tests passed"
    echo "✓ Full test suite passed"
    echo "✓ Coverage target met (${COVERAGE}% >= ${COVERAGE_TARGET}%)"
    exit 0
else
    echo -e "${RED}❌ SOME CHECKS FAILED${NC}"
    echo ""

    if [ $REGRESSION_PASS -eq 0 ]; then
        echo "✗ Regression tests failed"
    fi

    if [ $FULL_TEST_PASS -eq 0 ]; then
        echo "✗ Full test suite failed"
    fi

    if [ $COVERAGE_PASS -eq 0 ]; then
        echo "✗ Coverage below target (${COVERAGE}% < ${COVERAGE_TARGET}%)"
    fi

    exit 1
fi
