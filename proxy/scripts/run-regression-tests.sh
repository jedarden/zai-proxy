#!/bin/bash
# Automated Regression Test Runner
# Purpose: Run comprehensive regression test suite for token counting
# Usage: ./scripts/run-regression-tests.sh [--docker] [--coverage] [--race]
# Bead: bd-10d

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Parse command line arguments
USE_DOCKER=false
GENERATE_COVERAGE=false
USE_RACE_DETECTOR=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --docker)
            USE_DOCKER=true
            shift
            ;;
        --coverage)
            GENERATE_COVERAGE=true
            shift
            ;;
        --race)
            USE_RACE_DETECTOR=true
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --docker      Run tests in Docker container"
            echo "  --coverage    Generate coverage report"
            echo "  --race        Enable race detector"
            echo "  -h, --help    Show this help message"
            echo ""
            echo "Examples:"
            echo "  $0                    # Run tests locally"
            echo "  $0 --docker           # Run tests in Docker"
            echo "  $0 --coverage --race  # Run with coverage and race detection"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use -h or --help for usage information"
            exit 1
            ;;
    esac
done

# Function to print section headers
print_section() {
    echo ""
    echo -e "${CYAN}=================================${NC}"
    echo -e "${CYAN}$1${NC}"
    echo -e "${CYAN}=================================${NC}"
    echo ""
}

# Function to print test status
print_status() {
    if [ $1 -eq 0 ]; then
        echo -e "${GREEN}✅ $2${NC}"
    else
        echo -e "${RED}❌ $2${NC}"
        return 1
    fi
}

# Function to run go test with optional flags
run_test() {
    local test_name=$1
    local test_pattern=$2
    local extra_flags=$3

    echo -e "${YELLOW}Running: $test_name${NC}"

    if [ "$USE_RACE_DETECTOR" = true ] && [[ "$test_name" == *"Concurrent"* ]]; then
        extra_flags="$extra_flags -race"
    fi

    if go test -v -run "$test_pattern" $extra_flags; then
        print_status 0 "$test_name passed"
        return 0
    else
        print_status 1 "$test_name FAILED"
        return 1
    fi
}

# Start test suite
print_section "🧪 Regression Test Suite for Token Counting"

echo "Configuration:"
echo "  Docker mode: $USE_DOCKER"
echo "  Coverage: $GENERATE_COVERAGE"
echo "  Race detector: $USE_RACE_DETECTOR"
echo ""

# Check if running in Docker mode
if [ "$USE_DOCKER" = true ]; then
    print_section "🐳 Running Tests in Docker"

    # Build Docker image
    echo "Building Docker image..."
    if ! docker build -t zai-proxy:regression-test .; then
        print_status 1 "Docker build failed"
        exit 1
    fi
    print_status 0 "Docker image built"

    # Run tests in container
    echo ""
    echo "Running regression tests in container..."
    if docker run --rm zai-proxy:regression-test go test -v -run TestRegression; then
        print_status 0 "All Docker regression tests passed"
        exit 0
    else
        print_status 1 "Docker regression tests failed"
        exit 1
    fi
fi

# Check Go installation
if ! command -v go &> /dev/null; then
    echo -e "${RED}❌ Go not found. Install Go or use --docker flag.${NC}"
    exit 1
fi

echo "Go version: $(go version)"
echo ""

# Track test results
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

# Test 1: Basic Token Counts
print_section "📊 Test 1: Basic Token Counts (Golden Test Cases)"
if run_test "Basic Token Counts" "TestRegression_BasicTokenCounts"; then
    ((PASSED_TESTS++))
else
    ((FAILED_TESTS++))
fi
((TOTAL_TESTS++))

# Test 2: Edge Cases
print_section "🔍 Test 2: Edge Cases"
if run_test "Edge Cases" "TestRegression_EdgeCases"; then
    ((PASSED_TESTS++))
else
    ((FAILED_TESTS++))
fi
((TOTAL_TESTS++))

# Test 3: Request Parsing
print_section "📥 Test 3: Request Parsing"
if run_test "Request Parsing" "TestRegression_RequestParsing"; then
    ((PASSED_TESTS++))
else
    ((FAILED_TESTS++))
fi
((TOTAL_TESTS++))

# Test 4: Streaming Responses
print_section "📡 Test 4: Streaming Responses (SSE)"
if run_test "Streaming Responses" "TestRegression_StreamingResponses"; then
    ((PASSED_TESTS++))
else
    ((FAILED_TESTS++))
fi
((TOTAL_TESTS++))

# Test 5: JSON Responses
print_section "📄 Test 5: JSON Responses"
if run_test "JSON Responses" "TestRegression_JSONResponses"; then
    ((PASSED_TESTS++))
else
    ((FAILED_TESTS++))
fi
((TOTAL_TESTS++))

# Test 6: Usage Injection
print_section "💉 Test 6: Usage Injection"
if run_test "Usage Injection" "TestRegression_UsageInjection"; then
    ((PASSED_TESTS++))
else
    ((FAILED_TESTS++))
fi
((TOTAL_TESTS++))

# Test 7: Concurrent Access (with race detector)
print_section "🔀 Test 7: Concurrent Access (Thread Safety)"
if [ "$USE_RACE_DETECTOR" = true ]; then
    echo -e "${YELLOW}Running with race detector enabled${NC}"
    if run_test "Concurrent Access" "TestRegression_ConcurrentAccess" "-race"; then
        ((PASSED_TESTS++))
    else
        ((FAILED_TESTS++))
    fi
else
    if run_test "Concurrent Access" "TestRegression_ConcurrentAccess"; then
        ((PASSED_TESTS++))
    else
        ((FAILED_TESTS++))
    fi
fi
((TOTAL_TESTS++))

# Test 8: Fallback Counter
print_section "🔄 Test 8: Fallback Counter (SimpleTokenCounter)"
if run_test "Fallback Counter" "TestRegression_FallbackCounter"; then
    ((PASSED_TESTS++))
else
    ((FAILED_TESTS++))
fi
((TOTAL_TESTS++))

# Test 9: Streaming Preservation
print_section "📺 Test 9: Streaming Preservation"
if run_test "Streaming Preservation" "TestRegression_StreamingPreservation"; then
    ((PASSED_TESTS++))
else
    ((FAILED_TESTS++))
fi
((TOTAL_TESTS++))

# Generate coverage report if requested
if [ "$GENERATE_COVERAGE" = true ]; then
    print_section "📈 Generating Coverage Report"

    echo "Generating coverage profile..."
    if go test -coverprofile=regression_coverage.out -run TestRegression; then
        print_status 0 "Coverage profile generated"

        echo ""
        echo "Coverage summary:"
        go tool cover -func=regression_coverage.out | tail -10

        echo ""
        echo "Generating HTML coverage report..."
        go tool cover -html=regression_coverage.out -o regression_coverage.html
        print_status 0 "HTML coverage report generated: regression_coverage.html"

        # Calculate overall coverage percentage
        COVERAGE=$(go tool cover -func=regression_coverage.out | grep total | awk '{print $3}')
        echo ""
        echo -e "${BLUE}Overall Coverage: $COVERAGE${NC}"

        # Check if coverage meets target
        COVERAGE_NUM=$(echo $COVERAGE | sed 's/%//')
        if (( $(echo "$COVERAGE_NUM >= 90" | bc -l) )); then
            echo -e "${GREEN}✅ Coverage target met (≥90%)${NC}"
        else
            echo -e "${YELLOW}⚠️  Coverage below target: $COVERAGE_NUM% < 90%${NC}"
        fi
    else
        print_status 1 "Coverage generation failed"
    fi
fi

# Print summary
print_section "📋 Test Summary"

echo "Total test suites: $TOTAL_TESTS"
echo -e "Passed: ${GREEN}$PASSED_TESTS${NC}"
echo -e "Failed: ${RED}$FAILED_TESTS${NC}"
echo ""

# Calculate percentage
if [ $TOTAL_TESTS -gt 0 ]; then
    PASS_PERCENTAGE=$((PASSED_TESTS * 100 / TOTAL_TESTS))
    echo "Success rate: $PASS_PERCENTAGE%"
fi

echo ""

# Final status
if [ $FAILED_TESTS -eq 0 ]; then
    print_section "✅ All Regression Tests Passed!"
    echo -e "${GREEN}Token counting code is stable and regression-free.${NC}"
    echo ""
    echo "Next steps:"
    echo "  • Commit changes with: git commit -m 'test: regression suite validated'"
    echo "  • Run integration tests: ./test_api_requests.sh"
    echo "  • Deploy with confidence!"
    exit 0
else
    print_section "❌ Some Tests Failed"
    echo -e "${RED}$FAILED_TESTS out of $TOTAL_TESTS test suite(s) failed.${NC}"
    echo ""
    echo "Troubleshooting:"
    echo "  • Check test output above for error details"
    echo "  • Review docs/REGRESSION_TESTING.md for guidance"
    echo "  • Run failed tests individually: go test -v -run TestRegression_<Name>"
    echo "  • Check if tiktoken-go is installed: go mod download"
    exit 1
fi
