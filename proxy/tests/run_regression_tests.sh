#!/bin/bash
# Regression Test Runner for ZAI Proxy Token Counting
# Runs the complete regression test suite and generates coverage report

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_ROOT"

echo "======================================"
echo "ZAI Proxy Regression Test Suite"
echo "======================================"
echo ""

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if Go is available
if ! command -v go &> /dev/null; then
    echo -e "${RED}Error: Go compiler not found${NC}"
    echo "Running tests in Docker instead..."

    # Build Docker image
    echo "Building Docker image..."
    docker build -t zai-proxy:test .

    # Run tests in Docker
    echo "Running regression tests in Docker..."
    docker run --rm zai-proxy:test go test -v -run "^TestRegression_" -timeout 30m

    echo ""
    echo "Running all tests for coverage report..."
    docker run --rm zai-proxy:test go test -v -cover -coverprofile=coverage.out

    echo ""
    echo "Extracting coverage data..."
    docker run --rm zai-proxy:test go tool cover -func=coverage.out

    exit 0
fi

echo "Running Regression Test Suite..."
echo "=================================="
echo ""

# Run only regression tests first
echo "1. Running regression tests..."
go test -v -run "^TestRegression_" -timeout 30m 2>&1 | tee /tmp/regression_test_output.txt

# Check if tests passed
if [ ${PIPESTATUS[0]} -eq 0 ]; then
    echo -e "${GREEN}✅ All regression tests passed!${NC}"
else
    echo -e "${RED}❌ Some regression tests failed${NC}"
    exit 1
fi

echo ""
echo "2. Running full test suite with coverage..."
go test -v -cover -coverprofile=coverage.out -timeout 30m

echo ""
echo "3. Generating coverage report..."
echo "=================================="

# Generate coverage summary
go tool cover -func=coverage.out

echo ""
echo "4. Coverage by package..."
echo "=================================="

# Extract coverage percentage
COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}')
echo -e "Total coverage: ${GREEN}${COVERAGE}${NC}"

# Check if coverage meets target (90%)
COVERAGE_NUM=$(echo $COVERAGE | sed 's/%//')
TARGET=90

if (( $(echo "$COVERAGE_NUM >= $TARGET" | bc -l) )); then
    echo -e "${GREEN}✅ Coverage target met: ${COVERAGE_NUM}% >= ${TARGET}%${NC}"
else
    echo -e "${YELLOW}⚠️  Coverage below target: ${COVERAGE_NUM}% < ${TARGET}%${NC}"
    echo "Consider adding more test cases to improve coverage"
fi

echo ""
echo "5. Detailed coverage report..."
echo "=================================="

# Generate HTML coverage report
if [ -f coverage.out ]; then
    go tool cover -html=coverage.out -o coverage.html
    echo -e "${GREEN}HTML coverage report generated: coverage.html${NC}"
fi

echo ""
echo "======================================"
echo "Test Summary"
echo "======================================"

# Count test results
PASSED=$(grep -c "PASS:" /tmp/regression_test_output.txt || echo "0")
FAILED=$(grep -c "FAIL:" /tmp/regression_test_output.txt || echo "0")

echo "Regression tests passed: $PASSED"
echo "Regression tests failed: $FAILED"
echo "Code coverage: $COVERAGE"

if [ "$FAILED" -eq 0 ]; then
    echo -e "${GREEN}✅ All tests passed successfully!${NC}"
    exit 0
else
    echo -e "${RED}❌ $FAILED test(s) failed${NC}"
    exit 1
fi
