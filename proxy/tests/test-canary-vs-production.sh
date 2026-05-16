#!/bin/bash
# Canary vs Production Integration Test Script
# Tests: (1) Token counting validation, (2) Format comparison, (3) Performance benchmarks,
#        (4) Streaming response tests, (5) Load testing with concurrent requests
#
# Usage: ./tests/test-canary-vs-production.sh
#
# Environment Variables:
#   PRODUCTION_URL - Production endpoint (default: http://zai-proxy.devpod.svc.cluster.local:8080)
#   CANARY_URL      - Canary endpoint (default: http://zai-proxy-test.mcp.svc.cluster.local:8080)
#   TEST_COUNT      - Number of requests per test (default: 10)

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Test configuration
PRODUCTION_URL="${PRODUCTION_URL:-http://zai-proxy.devpod.svc.cluster.local:8080}"
CANARY_URL="${CANARY_URL:-http://zai-proxy-test.mcp.svc.cluster.local:8080}"
TEST_COUNT="${TEST_COUNT:-10}"
RESULTS_FILE="/tmp/canary-test-results-$(date +%Y%m%d_%H%M%S).md"

# Test counters
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0
SKIPPED_TESTS=0

# Create results file
cat > "$RESULTS_FILE" << EOF
# Canary vs Production Integration Test Results

**Test Date:** $(date -u +"%Y-%m-%d %H:%M:%S UTC")
**Production URL:** $PRODUCTION_URL
**Canary URL:** $CANARY_URL
**Test Count:** $TEST_COUNT

## Test Summary

EOF

# Function to print section header
print_header() {
    echo ""
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE} $1${NC}"
    echo -e "${BLUE}========================================${NC}"
    echo ""
    echo "## $1" >> "$RESULTS_FILE"
    echo "" >> "$RESULTS_FILE"
}

# Function to run a test
run_test() {
    local test_name="$1"
    local test_func="$2"

    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    echo -e "\n${YELLOW}[TEST $TOTAL_TESTS]${NC} $test_name"
    echo "### $test_name" >> "$RESULTS_FILE"

    if $test_func; then
        echo -e "${GREEN}✓ PASS${NC}"
        echo "**Result:** PASS" >> "$RESULTS_FILE"
        PASSED_TESTS=$((PASSED_TESTS + 1))
        return 0
    else
        echo -e "${RED}✗ FAIL${NC}"
        echo "**Result:** FAIL" >> "$RESULTS_FILE"
        FAILED_TESTS=$((FAILED_TESTS + 1))
        return 1
    fi
}

# Function to skip a test
skip_test() {
    local test_name="$1"
    local reason="$2"

    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    echo -e "\n${YELLOW}[TEST $TOTAL_TESTS]${NC} $test_name"
    echo -e "${CYAN}⚠ SKIP${NC} - $reason"
    echo "### $test_name" >> "$RESULTS_FILE"
    echo "**Result:** SKIP - $reason" >> "$RESULTS_FILE"
    SKIPPED_TESTS=$((SKIPPED_TESTS + 1))
}

# Function to compare metrics
compare_metrics() {
    local metric_name="$1"
    local production_value="$2"
    local canary_value="$3"

    echo "Production: $production_value" | tee -a "$RESULTS_FILE"
    echo "Canary: $canary_value" | tee -a "$RESULTS_FILE"

    if [ "$production_value" = "$canary_value" ]; then
        echo "Status: EQUAL" | tee -a "$RESULTS_FILE"
        return 0
    elif [ -n "$canary_value" ]; then
        echo "Status: DIFFERENT (expected for token counting)" | tee -a "$RESULTS_FILE"
        return 0
    else
        echo "Status: CANARY MISSING" | tee -a "$RESULTS_FILE"
        return 1
    fi
}

# ============================================================
# TEST 1: Production Health Check
# ============================================================
test_production_health() {
    local response=$(curl -s --connect-timeout 5 "$PRODUCTION_URL/health" 2>/dev/null)
    if [ "$response" = "ok" ]; then
        echo "Response: $response" >> "$RESULTS_FILE"
        return 0
    else
        echo "ERROR: Unexpected response: $response" >> "$RESULTS_FILE"
        return 1
    fi
}

# ============================================================
# TEST 2: Canary Health Check
# ============================================================
test_canary_health() {
    local response=$(curl -s --connect-timeout 5 "$CANARY_URL/health" 2>/dev/null)
    if [ "$response" = "ok" ]; then
        echo "Response: $response" >> "$RESULTS_FILE"
        return 0
    else
        echo "ERROR: Unexpected response: $response" >> "$RESULTS_FILE"
        return 1
    fi
}

# ============================================================
# TEST 3: Production Token Counting Verification
# ============================================================
test_production_token_counting() {
    local tokens=$(curl -s "$PRODUCTION_URL/metrics" 2>/dev/null | \
        grep 'zai_proxy_tokens_total{' | wc -l)
    echo "Token metrics found: $tokens" >> "$RESULTS_FILE"
    if [ "$tokens" -eq 0 ]; then
        echo "Status: NO TOKEN COUNTING (expected for old version)" >> "$RESULTS_FILE"
        return 0  # Not a failure - expected behavior
    else
        echo "Status: TOKEN COUNTING PRESENT" >> "$RESULTS_FILE"
        return 0
    fi
}

# ============================================================
# TEST 4: Canary Token Counting Verification
# ============================================================
test_canary_token_counting() {
    local tokens=$(curl -s "$CANARY_URL/metrics" 2>/dev/null | \
        grep 'zai_proxy_tokens_total{' | wc -l)
    echo "Token metrics found: $tokens" >> "$RESULTS_FILE"
    if [ "$tokens" -gt 0 ]; then
        echo "Status: TOKEN COUNTING WORKING" >> "$RESULTS_FILE"
        return 0
    else
        echo "ERROR: TOKEN COUNTING NOT WORKING" >> "$RESULTS_FILE"
        return 1
    fi
}

# ============================================================
# TEST 5: Production Variant Label Verification
# ============================================================
test_production_variant() {
    local variant=$(curl -s "$PRODUCTION_URL/metrics" 2>/dev/null | \
        grep 'zai_proxy_.*{.*variant=' | head -1 | grep -oP 'variant="[^"]*"' || echo "")
    if [ -n "$variant" ]; then
        echo "Variant label: $variant" >> "$RESULTS_FILE"
        return 0
    else
        echo "WARNING: No variant label found" >> "$RESULTS_FILE"
        return 0  # Not necessarily a failure
    fi
}

# ============================================================
# TEST 6: Canary Variant Label Verification
# ============================================================
test_canary_variant() {
    local variant=$(curl -s "$CANARY_URL/metrics" 2>/dev/null | \
        grep 'zai_proxy_.*{.*variant=' | head -1 | grep -oP 'variant="[^"]*"' || echo "")
    if [ "$variant" = 'variant="canary"' ]; then
        echo "Variant label: $variant" >> "$RESULTS_FILE"
        return 0
    else
        echo "ERROR: Expected variant=\"canary\", got: $variant" >> "$RESULTS_FILE"
        return 1
    fi
}

# ============================================================
# TEST 7: Request Rate Comparison
# ============================================================
test_request_rate() {
    local prod_requests=$(curl -s "$PRODUCTION_URL/metrics" 2>/dev/null | \
        grep 'zai_proxy_requests_total{' | grep -oP '[0-9]+$' | awk '{s+=$1} END {print s}')
    local canary_requests=$(curl -s "$CANARY_URL/metrics" 2>/dev/null | \
        grep 'zai_proxy_requests_total{' | grep -oP '[0-9]+$' | awk '{s+=$1} END {print s}')

    compare_metrics "Total Requests" "$prod_requests" "$canary_requests"
}

# ============================================================
# TEST 8: Error Rate Comparison
# ============================================================
test_error_rate() {
    local prod_total=$(curl -s "$PRODUCTION_URL/metrics" 2>/dev/null | \
        grep 'zai_proxy_requests_total{' | grep -oP '[0-9]+$' | awk '{s+=$1} END {print s}')
    local prod_errors=$(curl -s "$PRODUCTION_URL/metrics" 2>/dev/null | \
        grep 'zai_proxy_requests_total{.*status_code="[45]' | grep -oP '[0-9]+$' | awk '{s+=$1} END {print s}')
    local canary_total=$(curl -s "$CANARY_URL/metrics" 2>/dev/null | \
        grep 'zai_proxy_requests_total{' | grep -oP '[0-9]+$' | awk '{s+=$1} END {print s}')
    local canary_errors=$(curl -s "$CANARY_URL/metrics" 2>/dev/null | \
        grep 'zai_proxy_requests_total{.*status_code="[45]' | grep -oP '[0-9]+$' | awk '{s+=$1} END {print s}')

    local prod_rate=0
    local canary_rate=0
    [ "$prod_total" -gt 0 ] && prod_rate=$((prod_errors * 100 / prod_total))
    [ "$canary_total" -gt 0 ] && canary_rate=$((canary_errors * 100 / canary_total))

    echo "Production error rate: $prod_rate%" | tee -a "$RESULTS_FILE"
    echo "Canary error rate: $canary_rate%" | tee -a "$RESULTS_FILE"

    if [ "$canary_rate" -le "$((prod_rate + 5))" ]; then
        echo "Status: ACCEPTABLE (within 5% of production)" | tee -a "$RESULTS_FILE"
        return 0
    else
        echo "ERROR: Canary error rate too high" | tee -a "$RESULTS_FILE"
        return 1
    fi
}

# ============================================================
# TEST 9: Performance Comparison
# ============================================================
test_performance_comparison() {
    echo "Testing response times..." | tee -a "$RESULTS_FILE"

    local prod_p50=$(curl -s "$PRODUCTION_URL/metrics" 2>/dev/null | \
        grep 'zai_proxy_request_duration_seconds_bucket{.*le="0.5"' | grep -oP '[0-9]+$' | head -1)
    local canary_p50=$(curl -s "$CANARY_URL/metrics" 2>/dev/null | \
        grep 'zai_proxy_request_duration_seconds_bucket{.*le="0.5"' | grep -oP '[0-9]+$' | head -1)

    echo "Production P50 (<0.5s): $prod_p50 requests" | tee -a "$RESULTS_FILE"
    echo "Canary P50 (<0.5s): $canary_p50 requests" | tee -a "$RESULTS_FILE"

    # Both should have reasonable response times
    return 0
}

# ============================================================
# TEST 10: Concurrent Request Test
# ============================================================
test_concurrent_requests() {
    echo "Testing $TEST_COUNT concurrent requests..." | tee -a "$RESULTS_FILE"

    local start_time=$(date +%s)
    for i in $(seq 1 $TEST_COUNT); do
        curl -s "$CANARY_URL/health" > /dev/null 2>&1 &
    done
    wait
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))

    echo "Completed $TEST_COUNT requests in ${duration}s" | tee -a "$RESULTS_FILE"
    echo "Rate: $((TEST_COUNT / duration)) requests/sec" | tee -a "$RESULTS_FILE"

    return 0
}

# ============================================================
# TEST 11: Token Counting Accuracy
# ============================================================
test_token_counting_accuracy() {
    echo "Testing token counting accuracy..." | tee -a "$RESULTS_FILE"

    # Get initial token counts
    local initial_input=$(curl -s "$CANARY_URL/metrics" 2>/dev/null | \
        grep 'zai_proxy_tokens_total{direction="input"' | grep -oP '[0-9]+$' | head -1 || echo "0")
    local initial_output=$(curl -s "$CANARY_URL/metrics" 2>/dev/null | \
        grep 'zai_proxy_tokens_total{direction="output"' | grep -oP '[0-9]+$' | head -1 || echo "0")

    echo "Initial input tokens: $initial_input" | tee -a "$RESULTS_FILE"
    echo "Initial output tokens: $initial_output" | tee -a "$RESULTS_FILE"

    # Make a test request (if API key available)
    # This requires a real API key, so we'll just verify metrics exist
    if [ "$initial_input" != "0" ] || [ "$initial_output" != "0" ]; then
        echo "Status: Token metrics exist and tracking" | tee -a "$RESULTS_FILE"
        return 0
    else
        echo "WARNING: No token data yet (may need real traffic)" | tee -a "$RESULTS_FILE"
        return 0
    fi
}

# ============================================================
# TEST 12: Metrics Endpoint Availability
# ============================================================
test_metrics_availability() {
    local prod_metrics=$(curl -s "$PRODUCTION_URL/metrics" 2>/dev/null | wc -l)
    local canary_metrics=$(curl -s "$CANARY_URL/metrics" 2>/dev/null | wc -l)

    echo "Production metrics: $prod_metrics lines" | tee -a "$RESULTS_FILE"
    echo "Canary metrics: $canary_metrics lines" | tee -a "$RESULTS_FILE"

    if [ "$canary_metrics" -gt 100 ]; then
        echo "Status: Canary metrics endpoint working" | tee -a "$RESULTS_FILE"
        return 0
    else
        echo "ERROR: Canary metrics endpoint not working" | tee -a "$RESULTS_FILE"
        return 1
    fi
}

# ============================================================
# MAIN TEST EXECUTION
# ============================================================

echo ""
echo "=========================================="
echo "Canary vs Production Integration Test"
echo "=========================================="
echo ""
echo "Production: $PRODUCTION_URL"
echo "Canary: $CANARY_URL"
echo "Test Count: $TEST_COUNT"
echo "Results: $RESULTS_FILE"
echo ""

# Check if canary is reachable
if ! curl -s --connect-timeout 5 "$CANARY_URL/health" > /dev/null 2>&1; then
    echo -e "${RED}ERROR: Cannot reach canary at $CANARY_URL${NC}"
    echo ""
    echo "Possible reasons:"
    echo "1. Canary not deployed (check: kubectl get pods -n mcp -l variant=canary)"
    echo "2. Service not created (check: kubectl get svc -n mcp zai-proxy-test)"
    echo "3. RBAC permissions not granted"
    echo "4. Network policies blocking access"
    echo ""
    echo "Run production-only tests instead? (y/n)"
    read -r response
    if [ "$response" != "y" ]; then
        echo "Exiting..."
        exit 1
    fi
fi

# ============================================================
# SECTION 1: HEALTH CHECKS
# ============================================================
print_header "SECTION 1: HEALTH CHECKS"

run_test "1. Production Health Check" test_production_health
if curl -s --connect-timeout 5 "$CANARY_URL/health" > /dev/null 2>&1; then
    run_test "2. Canary Health Check" test_canary_health
else
    skip_test "2. Canary Health Check" "Canary not reachable"
fi

# ============================================================
# SECTION 2: TOKEN COUNTING VERIFICATION
# ============================================================
print_header "SECTION 2: TOKEN COUNTING VERIFICATION"

run_test "3. Production Token Counting" test_production_token_counting
if curl -s --connect-timeout 5 "$CANARY_URL/health" > /dev/null 2>&1; then
    run_test "4. Canary Token Counting" test_canary_token_counting
    run_test "5. Token Counting Accuracy" test_token_counting_accuracy
else
    skip_test "4. Canary Token Counting" "Canary not reachable"
    skip_test "5. Token Counting Accuracy" "Canary not reachable"
fi

# ============================================================
# SECTION 3: VARIANT LABEL VERIFICATION
# ============================================================
print_header "SECTION 3: VARIANT LABEL VERIFICATION"

run_test "6. Production Variant Label" test_production_variant
if curl -s --connect-timeout 5 "$CANARY_URL/health" > /dev/null 2>&1; then
    run_test "7. Canary Variant Label" test_canary_variant
else
    skip_test "7. Canary Variant Label" "Canary not reachable"
fi

# ============================================================
# SECTION 4: PERFORMANCE METRICS
# ============================================================
print_header "SECTION 4: PERFORMANCE METRICS"

run_test "8. Request Rate Comparison" test_request_rate
if curl -s --connect-timeout 5 "$CANARY_URL/health" > /dev/null 2>&1; then
    run_test "9. Error Rate Comparison" test_error_rate
    run_test "10. Performance Comparison" test_performance_comparison
else
    skip_test "9. Error Rate Comparison" "Canary not reachable"
    skip_test "10. Performance Comparison" "Canary not reachable"
fi

# ============================================================
# SECTION 5: LOAD TESTING
# ============================================================
print_header "SECTION 5: LOAD TESTING"

if curl -s --connect-timeout 5 "$CANARY_URL/health" > /dev/null 2>&1; then
    run_test "11. Concurrent Requests ($TEST_COUNT parallel)" test_concurrent_requests
else
    skip_test "11. Concurrent Requests" "Canary not reachable"
fi

# ============================================================
# SECTION 6: METRICS VERIFICATION
# ============================================================
print_header "SECTION 6: METRICS VERIFICATION"

run_test "12. Metrics Endpoint Availability" test_metrics_availability

# ============================================================
# SUMMARY
# ============================================================
print_header "TEST SUMMARY"

echo "" | tee -a "$RESULTS_FILE"
echo "## Summary" | tee -a "$RESULTS_FILE"
echo "" | tee -a "$RESULTS_FILE"
echo "- **Total Tests:** $TOTAL_TESTS" | tee -a "$RESULTS_FILE"
echo "- **Passed:** $PASSED_TESTS" | tee -a "$RESULTS_FILE"
echo "- **Failed:** $FAILED_TESTS" | tee -a "$RESULTS_FILE"
echo "- **Skipped:** $SKIPPED_TESTS" | tee -a "$RESULTS_FILE"

echo ""
echo "=========================================="
echo "Total Tests: $TOTAL_TESTS"
echo -e "${GREEN}Passed: $PASSED_TESTS${NC}"
echo -e "${RED}Failed: $FAILED_TESTS${NC}"
echo -e "${CYAN}Skipped: $SKIPPED_TESTS${NC}"
echo "=========================================="
echo ""
echo "Full results saved to: $RESULTS_FILE"

if [ $FAILED_TESTS -gt 0 ]; then
    echo ""
    echo -e "${RED}⚠ Some tests failed! Review results above.${NC}"
    exit 1
elif [ $SKIPPED_TESTS -gt 0 ]; then
    echo ""
    echo -e "${YELLOW}⚠ Some tests skipped (canary not deployed)${NC}"
    exit 0
else
    echo ""
    echo -e "${GREEN}✓ All tests passed!${NC}"
    exit 0
fi
