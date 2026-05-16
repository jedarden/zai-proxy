#!/bin/bash
# Canary Integration Tests for zai-proxy
# Tests production and canary deployments to verify token counting behavior
#
# Usage:
#   ./scripts/canary_integration_tests.sh [--production-url URL] [--canary-url URL] [--api-key KEY]
#
# Environment Variables:
#   PRODUCTION_URL - Production proxy URL (default: http://zai-proxy.devpod.svc.cluster.local:8080)
#   CANARY_URL - Canary proxy URL (default: http://zai-proxy-canary.devpod.svc.cluster.local:8080)
#   ZAI_API_KEY - API key for authentication (required)
#
# Exit Codes:
#   0 - All tests passed
#   1 - Test failures
#   2 - Configuration errors

set -euo pipefail

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test counters
TESTS_TOTAL=0
TESTS_PASSED=0
TESTS_FAILED=0
TESTS_SKIPPED=0

# Test results directory
RESULTS_DIR="${RESULTS_DIR:-/tmp/zai-proxy-canary-tests}"
mkdir -p "$RESULTS_DIR"

# Timestamp for this test run
TIMESTAMP=$(date -u +"%Y%m%d_%H%M%S")
RESULTS_FILE="$RESULTS_DIR/canary_test_results_$TIMESTAMP.txt"
SUMMARY_FILE="$RESULTS_DIR/canary_test_summary_$TIMESTAMP.json"

# Default URLs
PRODUCTION_URL="${PRODUCTION_URL:-http://zai-proxy.devpod.svc.cluster.local:8080}"
CANARY_URL="${CANARY_URL:-http://zai-proxy-canary.devpod.svc.cluster.local:8080}"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --production-url)
            PRODUCTION_URL="$2"
            shift 2
            ;;
        --canary-url)
            CANARY_URL="$2"
            shift 2
            ;;
        --api-key)
            ZAI_API_KEY="$2"
            shift 2
            ;;
        -h|--help)
            echo "Usage: $0 [--production-url URL] [--canary-url URL] [--api-key KEY]"
            echo ""
            echo "Environment Variables:"
            echo "  PRODUCTION_URL - Production proxy URL"
            echo "  CANARY_URL - Canary proxy URL"
            echo "  ZAI_API_KEY - API key for authentication"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 2
            ;;
    esac
done

# Validate required parameters
if [[ -z "${ZAI_API_KEY:-}" ]]; then
    echo -e "${RED}Error: ZAI_API_KEY is required${NC}"
    echo "Set via environment variable or --api-key argument"
    exit 2
fi

# Helper functions
log_header() {
    echo -e "${BLUE}=== $1 ===${NC}"
    echo "=== $1 ===" >> "$RESULTS_FILE"
}

log_test() {
    echo -e "${YELLOW}[TEST]${NC} $1"
    echo "[TEST] $1" >> "$RESULTS_FILE"
}

log_pass() {
    echo -e "${GREEN}[PASS]${NC} $1"
    echo "[PASS] $1" >> "$RESULTS_FILE"
    ((TESTS_PASSED++))
}

log_fail() {
    echo -e "${RED}[FAIL]${NC} $1"
    echo "[FAIL] $1" >> "$RESULTS_FILE"
    ((TESTS_FAILED++))
}

log_skip() {
    echo -e "${YELLOW}[SKIP]${NC} $1"
    echo "[SKIP] $1" >> "$RESULTS_FILE"
    ((TESTS_SKIPPED++))
}

log_info() {
    echo "$1"
    echo "$1" >> "$RESULTS_FILE"
}

# Record test start
((TESTS_TOTAL++))

# Initialize results file
{
    echo "Z.AI Proxy Canary Integration Tests"
    echo "Started: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "Production URL: $PRODUCTION_URL"
    echo "Canary URL: $CANARY_URL"
    echo ""
} > "$RESULTS_FILE"

log_header "Z.AI Proxy Canary Integration Tests"
log_info "Production URL: $PRODUCTION_URL"
log_info "Canary URL: $CANARY_URL"
log_info "Results: $RESULTS_FILE"
echo ""

# Test 1: Health Check
log_test "Health Check - Production"
if curl -sf "$PRODUCTION_URL/health" > /dev/null 2>&1; then
    log_pass "Production health endpoint is responding"
else
    log_fail "Production health endpoint is not responding"
fi
((TESTS_TOTAL++))

log_test "Health Check - Canary"
if curl -sf "$CANARY_URL/health" > /dev/null 2>&1; then
    log_pass "Canary health endpoint is responding"
    CANARY_AVAILABLE=true
else
    log_skip "Canary deployment not available (expected for initial testing)"
    CANARY_AVAILABLE=false
fi
echo ""

# Test 2: Metrics Endpoint
log_test "Metrics Endpoint - Production"
if curl -sf "$PRODUCTION_URL/metrics" > /dev/null 2>&1; then
    log_pass "Production metrics endpoint is responding"

    # Check for token counting metrics
    if curl -s "$PRODUCTION_URL/metrics" | grep -q "zai_proxy_tokens_total"; then
        log_pass "Production has token counting metrics (token counting ENABLED)"
        PRODUCTION_TOKEN_COUNTING=true
    else
        log_info "Production missing token counting metrics (token counting DISABLED)"
        PRODUCTION_TOKEN_COUNTING=false
    fi
else
    log_fail "Production metrics endpoint is not responding"
fi
((TESTS_TOTAL++))

if [[ "$CANARY_AVAILABLE" == "true" ]]; then
    log_test "Metrics Endpoint - Canary"
    if curl -sf "$CANARY_URL/metrics" > /dev/null 2>&1; then
        log_pass "Canary metrics endpoint is responding"

        # Check for token counting metrics
        if curl -s "$CANARY_URL/metrics" | grep -q "zai_proxy_tokens_total"; then
            log_pass "Canary has token counting metrics (token counting ENABLED)"
            CANARY_TOKEN_COUNTING=true
        else
            log_fail "Canary missing token counting metrics (token counting DISABLED)"
            CANARY_TOKEN_COUNTING=false
        fi
    else
        log_fail "Canary metrics endpoint is not responding"
    fi
    ((TESTS_TOTAL++))
fi
echo ""

# Test 3: Token Counting Validation
log_header "Token Counting Validation Tests"

# Test 3.1: Basic Request Token Counting
log_test "Basic Request - Token Counting"
RESPONSE=$(curl -s -X POST "$PRODUCTION_URL/v1/messages" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $ZAI_API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    -d '{
        "model": "glm-4",
        "max_tokens": 50,
        "messages": [{"role": "user", "content": "Hello, how are you?"}]
    }')

if echo "$RESPONSE" | jq -e '.usage' > /dev/null 2>&1; then
    INPUT_TOKENS=$(echo "$RESPONSE" | jq -r '.usage.input_tokens // "null"')
    OUTPUT_TOKENS=$(echo "$RESPONSE" | jq -r '.usage.output_tokens // "null"')

    if [[ "$INPUT_TOKENS" != "null" && "$OUTPUT_TOKENS" != "null" ]]; then
        log_pass "Production returns token usage: input=$INPUT_TOKENS, output=$OUTPUT_TOKENS"
    else
        log_fail "Production usage field missing token counts: $RESPONSE"
    fi
else
    log_info "Production does not return usage field (token counting may be disabled)"
fi
((TESTS_TOTAL++))

# Test 3.2: Streaming Request Token Counting
log_test "Streaming Request - Token Counting"
STREAM_RESPONSE=$(curl -s -X POST "$PRODUCTION_URL/v1/messages" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $ZAI_API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    -d '{
        "model": "glm-4",
        "max_tokens": 50,
        "stream": true,
        "messages": [{"role": "user", "content": "Say hello"}]
    }')

# Check for usage in streaming response
if echo "$STREAM_RESPONSE" | grep -q '"usage"'; then
    # Extract usage from message_delta event
    USAGE_LINE=$(echo "$STREAM_RESPONSE" | grep '"usage"' | head -1)
    log_pass "Streaming response includes token usage: $USAGE_LINE"
else
    log_info "Streaming response does not include token usage"
fi
((TESTS_TOTAL++))
echo ""

# Test 4: Format Comparison
log_header "Format Comparison Tests"

# Test 4.1: Response Format Validation
log_test "Response Format Validation"
FORMAT_RESPONSE=$(curl -s -X POST "$PRODUCTION_URL/v1/messages" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $ZAI_API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    -d '{
        "model": "glm-4",
        "max_tokens": 50,
        "messages": [{"role": "user", "content": "Hi"}]
    }')

# Validate response structure
HAS_ID=$(echo "$FORMAT_RESPONSE" | jq -e '.id' > /dev/null 2>&1 && echo "true" || echo "false")
HAS_TYPE=$(echo "$FORMAT_RESPONSE" | jq -e '.type' > /dev/null 2>&1 && echo "true" || echo "false")
HAS_ROLE=$(echo "$FORMAT_RESPONSE" | jq -e '.role' > /dev/null 2>&1 && echo "true" || echo "false")
HAS_CONTENT=$(echo "$FORMAT_RESPONSE" | jq -e '.content' > /dev/null 2>&1 && echo "true" || echo "false")

if [[ "$HAS_ID" == "true" && "$HAS_TYPE" == "true" && "$HAS_ROLE" == "true" && "$HAS_CONTENT" == "true" ]]; then
    log_pass "Response has valid structure: id, type, role, content"
else
    log_fail "Response structure incomplete: id=$HAS_ID, type=$HAS_TYPE, role=$HAS_ROLE, content=$HAS_CONTENT"
fi
((TESTS_TOTAL++))
echo ""

# Test 5: Performance Benchmarks
log_header "Performance Benchmark Tests"

log_test "Latency Benchmark (10 requests)"
TOTAL_TIME=0
NUM_REQUESTS=10

for i in $(seq 1 $NUM_REQUESTS); do
    START=$(date +%s%3N)
    curl -s -X POST "$PRODUCTION_URL/v1/messages" \
        -H "Content-Type: application/json" \
        -H "x-api-key: $ZAI_API_KEY" \
        -H "anthropic-version: 2023-06-01" \
        -d '{
            "model": "glm-4",
            "max_tokens": 20,
            "messages": [{"role": "user", "content": "Hi"}]
        }' > /dev/null
    END=$(date +%s%3N)
    ELAPSED=$((END - START))
    TOTAL_TIME=$((TOTAL_TIME + ELAPSED))
done

AVG_LATENCY=$((TOTAL_TIME / NUM_REQUESTS))
log_info "Average latency: ${AVG_LATENCY}ms over $NUM_REQUESTS requests"

if [[ $AVG_LATENCY -lt 100 ]]; then
    log_pass "Latency excellent (<100ms average)"
elif [[ $AVG_LATENCY -lt 500 ]]; then
    log_pass "Latency acceptable (<500ms average)"
else
    log_fail "Latency high (>=500ms average): ${AVG_LATENCY}ms"
fi
((TESTS_TOTAL++))
echo ""

# Test 6: Streaming Response Tests
log_header "Streaming Response Tests"

log_test "Streaming Response Format"
STREAM_TEST=$(curl -s -X POST "$PRODUCTION_URL/v1/messages" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $ZAI_API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    -d '{
        "model": "glm-4",
        "max_tokens": 50,
        "stream": true,
        "messages": [{"role": "user", "content": "Count to 5"}]
    }')

# Check for required SSE events
HAS_MESSAGE_START=$(echo "$STREAM_TEST" | grep -q "message_start" && echo "true" || echo "false")
HAS_CONTENT_DELTA=$(echo "$STREAM_TEST" | grep -q "content_block_delta" && echo "true" || echo "false")
HAS_MESSAGE_STOP=$(echo "$STREAM_TEST" | grep -q "message_stop" && echo "true" || echo "false")

if [[ "$HAS_MESSAGE_START" == "true" && "$HAS_CONTENT_DELTA" == "true" && "$HAS_MESSAGE_STOP" == "true" ]]; then
    log_pass "Streaming response has valid SSE format"
else
    log_fail "Streaming response missing SSE events: start=$HAS_MESSAGE_START, delta=$HAS_CONTENT_DELTA, stop=$HAS_MESSAGE_STOP"
fi
((TESTS_TOTAL++))
echo ""

# Test 7: Load Testing
log_header "Load Testing - Concurrent Requests"

log_test "Concurrent Request Test (5 parallel)"
TEMP_DIR=$(mktemp -d)
PIDS=()

for i in {1..5}; do
    (
        START=$(date +%s%3N)
        curl -s -X POST "$PRODUCTION_URL/v1/messages" \
            -H "Content-Type: application/json" \
            -H "x-api-key: $ZAI_API_KEY" \
            -H "anthropic-version: 2023-06-01" \
            -d '{
                "model": "glm-4",
                "max_tokens": 20,
                "messages": [{"role": "user", "content": "Test"}]
            }' > "$TEMP_DIR/response_$i.json" 2>&1
        END=$(date +%s%3N)
        echo $((END - START)) > "$TEMP_DIR/latency_$i.txt"
    ) &
    PIDS+=($!)
done

# Wait for all requests to complete
for pid in "${PIDS[@]}"; do
    wait $pid || true
done

# Check results
SUCCESS_COUNT=0
TOTAL_LATENCY=0
for i in {1..5}; do
    if [[ -f "$TEMP_DIR/response_$i.json" ]]; then
        if jq -e '.content' "$TEMP_DIR/response_$i.json" > /dev/null 2>&1; then
            ((SUCCESS_COUNT++))
        fi
    fi
    if [[ -f "$TEMP_DIR/latency_$i.txt" ]]; then
        LATENCY=$(cat "$TEMP_DIR/latency_$i.txt")
        TOTAL_LATENCY=$((TOTAL_LATENCY + LATENCY))
    fi
done

rm -rf "$TEMP_DIR"

log_info "Concurrent test: $SUCCESS_COUNT/5 requests succeeded"
if [[ $SUCCESS_COUNT -eq 5 ]]; then
    log_pass "All concurrent requests succeeded"
else
    log_fail "Some concurrent requests failed: $SUCCESS_COUNT/5"
fi
((TESTS_TOTAL++))
echo ""

# Test 8: Production Metrics Monitoring
log_header "Production Metrics Monitoring"

log_test "Production Token Counting Metrics"
PROD_METRICS=$(curl -s "$PRODUCTION_URL/metrics" 2>/dev/null || echo "")

if [[ -n "$PROD_METRICS" ]]; then
    # Extract token counts from metrics
    INPUT_TOTAL=$(echo "$PROD_METRICS" | grep 'zai_proxy_tokens_total{direction="input"' | grep -oP '[0-9]+$' | head -1 || echo "0")
    OUTPUT_TOTAL=$(echo "$PROD_METRICS" | grep 'zai_proxy_tokens_total{direction="output"' | grep -oP '[0-9]+$' | head -1 || echo "0")

    log_info "Production metrics - Input tokens: $INPUT_TOTAL, Output tokens: $OUTPUT_TOTAL"

    if [[ "$INPUT_TOTAL" -gt 0 || "$OUTPUT_TOTAL" -gt 0 ]]; then
        log_pass "Production has recorded token usage"
    else
        log_info "Production has not recorded token usage yet (may be new deployment)"
    fi

    # Check request counts
    REQUEST_TOTAL=$(echo "$PROD_METRICS" | grep 'zai_proxy_requests_total' | grep -oP '[0-9]+$' | awk '{s+=$1} END {print s}' || echo "0")
    log_info "Total requests processed: $REQUEST_TOTAL"
else
    log_fail "Could not fetch production metrics"
fi
((TESTS_TOTAL++))
echo ""

# Canary vs Production Comparison (if canary is available)
if [[ "$CANARY_AVAILABLE" == "true" ]]; then
    log_header "Canary vs Production Comparison"

    log_test "Token Counting Feature Comparison"
    if [[ "$PRODUCTION_TOKEN_COUNTING" == "true" && "$CANARY_TOKEN_COUNTING" == "true" ]]; then
        log_pass "Both production and canary have token counting enabled"
    elif [[ "$PRODUCTION_TOKEN_COUNTING" == "false" && "$CANARY_TOKEN_COUNTING" == "true" ]]; then
        log_pass "Canary has token counting, production does not (EXPECTED for canary testing)"
    elif [[ "$PRODUCTION_TOKEN_COUNTING" == "true" && "$CANARY_TOKEN_COUNTING" == "false" ]]; then
        log_fail "Production has token counting but canary does not (UNEXPECTED)"
    else
        log_info "Both production and canary have token counting disabled"
    fi
    ((TESTS_TOTAL++))

    # Test identical request on both
    log_test "Identical Request Comparison"

    # Production request
    PROD_RESP=$(curl -s -X POST "$PRODUCTION_URL/v1/messages" \
        -H "Content-Type: application/json" \
        -H "x-api-key: $ZAI_API_KEY" \
        -H "anthropic-version: 2023-06-01" \
        -d '{
            "model": "glm-4",
            "max_tokens": 50,
            "messages": [{"role": "user", "content": "What is 2+2?"}]
        }')

    # Canary request
    CANARY_RESP=$(curl -s -X POST "$CANARY_URL/v1/messages" \
        -H "Content-Type: application/json" \
        -H "x-api-key: $ZAI_API_KEY" \
        -H "anthropic-version: 2023-06-01" \
        -d '{
            "model": "glm-4",
            "max_tokens": 50,
            "messages": [{"role": "user", "content": "What is 2+2?"}]
        }')

    PROD_INPUT=$(echo "$PROD_RESP" | jq -r '.usage.input_tokens // "null"')
    CANARY_INPUT=$(echo "$CANARY_RESP" | jq -r '.usage.input_tokens // "null"')

    if [[ "$PROD_INPUT" != "null" && "$CANARY_INPUT" != "null" ]]; then
        log_info "Production input tokens: $PROD_INPUT, Canary input tokens: $CANARY_INPUT"
        if [[ "$PROD_INPUT" -eq "$CANARY_INPUT" ]]; then
            log_pass "Input token counts match between production and canary"
        else
            log_info "Input token counts differ (expected if implementations differ)"
        fi
    else
        log_info "Cannot compare token counts (one or both missing usage field)"
    fi
    ((TESTS_TOTAL++))
    echo ""
fi

# Generate Summary
log_header "Test Summary"

log_info "Total Tests: $TESTS_TOTAL"
log_info "Passed: $TESTS_PASSED"
log_info "Failed: $TESTS_FAILED"
log_info "Skipped: $TESTS_SKIPPED"

# Calculate pass rate
if [[ $TESTS_TOTAL -gt 0 ]]; then
    PASS_RATE=$((TESTS_PASSED * 100 / TESTS_TOTAL))
    log_info "Pass Rate: ${PASS_RATE}%"
fi

# Write JSON summary
cat > "$SUMMARY_FILE" << EOF
{
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "production_url": "$PRODUCTION_URL",
  "canary_url": "$CANARY_URL",
  "tests": {
    "total": $TESTS_TOTAL,
    "passed": $TESTS_PASSED,
    "failed": $TESTS_FAILED,
    "skipped": $TESTS_SKIPPED,
    "pass_rate": $PASS_RATE
  },
  "production": {
    "token_counting_enabled": $PRODUCTION_TOKEN_COUNTING
  },
  "canary": {
    "available": $CANARY_AVAILABLE,
    "token_counting_enabled": ${CANARY_TOKEN_COUNTING:-false}
  }
}
EOF

log_info "Results saved to: $RESULTS_FILE"
log_info "Summary saved to: $SUMMARY_FILE"

# Final verdict
echo ""
if [[ $TESTS_FAILED -eq 0 ]]; then
    log_pass "All tests passed!"
    exit 0
elif [[ $TESTS_FAILED -lt $((TESTS_TOTAL / 2)) ]]; then
    log_info "Some tests failed but majority passed"
    exit 1
else
    log_fail "Majority of tests failed"
    exit 1
fi
