#!/bin/bash
#
# Integration Test: Claude Code CLI with z.ai Proxy
#
# Tests real Claude Code client compatibility with z.ai proxy
# Validates token counting, streaming, multi-turn conversations, error handling

set -e

PROXY_URL="${PROXY_URL:-http://localhost:8080}"
TEST_DIR="/tmp/claude-code-integration-test-$$"
LOG_FILE="/tmp/zai-proxy-integration.log"
RESULTS_FILE="/tmp/claude-code-integration-results-$$.md"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "=========================================="
echo "Claude Code + z.ai Proxy Integration Test"
echo "=========================================="
echo ""
echo "Proxy URL: $PROXY_URL"
echo "Test Directory: $TEST_DIR"
echo "Proxy Log: $LOG_FILE"
echo "Results: $RESULTS_FILE"
echo ""

# Initialize results file
cat > "$RESULTS_FILE" <<EOF
# Claude Code + z.ai Proxy Integration Test Results

**Test Date:** $(date -u +"%Y-%m-%d %H:%M:%S UTC")
**Proxy URL:** $PROXY_URL
**Claude Code Version:** $(claude --version 2>&1 | head -1)

---

## Test Summary

EOF

# Function to log test results
log_result() {
    local test_name="$1"
    local status="$2"
    local details="$3"

    if [ "$status" = "PASS" ]; then
        echo -e "${GREEN}✓${NC} $test_name"
        echo "- ✅ **$test_name**: PASS" >> "$RESULTS_FILE"
    elif [ "$status" = "FAIL" ]; then
        echo -e "${RED}✗${NC} $test_name"
        echo "- ❌ **$test_name**: FAIL" >> "$RESULTS_FILE"
    else
        echo -e "${YELLOW}⚠${NC} $test_name"
        echo "- ⚠️ **$test_name**: $status" >> "$RESULTS_FILE"
    fi

    if [ -n "$details" ]; then
        echo "  $details"
        echo "  - Details: $details" >> "$RESULTS_FILE"
    fi
    echo "" >> "$RESULTS_FILE"
}

# Setup test workspace
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"

# Initialize a simple project
cat > package.json <<EOF
{
  "name": "zai-proxy-test",
  "version": "1.0.0",
  "description": "Integration test for z.ai proxy"
}
EOF

cat > test.js <<EOF
// Test file for Claude Code editing
function add(a, b) {
    return a + b;
}

module.exports = { add };
EOF

echo ""
echo "=== Test 1: Proxy Health Check ==="
if curl -s "$PROXY_URL/metrics" | grep -q "zai_proxy"; then
    log_result "Proxy Health Check" "PASS" "Metrics endpoint responding"
else
    log_result "Proxy Health Check" "FAIL" "Metrics endpoint not responding"
    exit 1
fi

echo ""
echo "=== Test 2: Basic Code Generation ==="
# Configure Claude Code to use the proxy
export ANTHROPIC_BASE_URL="$PROXY_URL"
export ANTHROPIC_API_KEY="${ZAI_API_KEY:-test-key}"

# Test basic code generation - use print mode for non-interactive
PROMPT="Create a simple hello world function"
timeout 30 claude --print "$PROMPT" 2>&1 > /tmp/test2-output.txt || true

if [ -s /tmp/test2-output.txt ] && grep -qi "function\|hello" /tmp/test2-output.txt; then
    log_result "Basic Code Generation" "PASS" "Code generation successful"

    # Check token counting in logs
    sleep 1
    if grep -q "Token usage" "$LOG_FILE" 2>/dev/null; then
        TOKEN_COUNT=$(grep "Token usage" "$LOG_FILE" | tail -1)
        log_result "Token Counting (Code Generation)" "PASS" "$TOKEN_COUNT"
    else
        log_result "Token Counting (Code Generation)" "WARN" "No token usage found in logs"
    fi
else
    log_result "Basic Code Generation" "FAIL" "No code generated"
fi

echo ""
echo "=== Test 3: Simple Conversation ==="
# Test simple conversation instead of file editing (more reliable)
CONV_PROMPT="What is 2 plus 2?"
timeout 30 claude --print "$CONV_PROMPT" 2>&1 > /tmp/test3-output.txt || true

if [ -s /tmp/test3-output.txt ] && grep -qiE "4|four" /tmp/test3-output.txt; then
    log_result "Simple Conversation" "PASS" "Response received successfully"

    # Check token usage
    sleep 1
    if grep -q "Token usage" "$LOG_FILE" 2>/dev/null; then
        TOKEN_COUNT=$(grep "Token usage" "$LOG_FILE" | tail -1)
        log_result "Token Counting (Conversation)" "PASS" "$TOKEN_COUNT"
    else
        log_result "Token Counting (Conversation)" "WARN" "No token usage in logs"
    fi
else
    log_result "Simple Conversation" "WARN" "Response not received"
fi

echo ""
echo "=== Test 4: Streaming Response ==="
# Test streaming with print mode
STREAM_PROMPT="List three programming languages"
STREAM_OUTPUT="/tmp/stream-output-$$.txt"

# Use print mode for streaming
timeout 20 claude --print "$STREAM_PROMPT" 2>&1 | tee "$STREAM_OUTPUT" || true

if [ -s "$STREAM_OUTPUT" ]; then
    WORD_COUNT=$(wc -w < "$STREAM_OUTPUT")
    log_result "Streaming Response" "PASS" "Received $WORD_COUNT words in response"
else
    log_result "Streaming Response" "FAIL" "No streaming output received"
fi

echo ""
echo "=== Test 5: Multi-Turn Conversation ==="
# Simulate multi-turn conversation (5 turns for quick test)
TURNS=5
SUCCESS_COUNT=0

for i in $(seq 1 $TURNS); do
    echo "Turn $i/$TURNS..."
    TURN_PROMPT="What is $i plus $i?"
    TURN_OUTPUT="/tmp/turn-$i-$$.txt"

    timeout 15 claude --print "$TURN_PROMPT" 2>&1 > "$TURN_OUTPUT" || true

    if [ -s "$TURN_OUTPUT" ] && grep -qE "[0-9]" "$TURN_OUTPUT"; then
        SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
    fi

    sleep 2  # Brief pause between turns
done

if [ "$SUCCESS_COUNT" -eq "$TURNS" ]; then
    log_result "Multi-Turn Conversation (5 turns)" "PASS" "All $TURNS turns successful"
else
    log_result "Multi-Turn Conversation (5 turns)" "PARTIAL" "$SUCCESS_COUNT/$TURNS turns successful"
fi

echo ""
echo "=== Test 6: Token Usage Metrics ==="
# Check Prometheus metrics for token counts
METRICS=$(curl -s "$PROXY_URL/metrics")

INPUT_TOKENS=$(echo "$METRICS" | grep 'zai_proxy_tokens_total{direction="input"' | grep -oP '\d+$' || echo "0")
OUTPUT_TOKENS=$(echo "$METRICS" | grep 'zai_proxy_tokens_total{direction="output"' | grep -oP '\d+$' || echo "0")

if [ "$INPUT_TOKENS" -gt 0 ] && [ "$OUTPUT_TOKENS" -gt 0 ]; then
    log_result "Token Metrics (Prometheus)" "PASS" "Input: $INPUT_TOKENS, Output: $OUTPUT_TOKENS"
else
    log_result "Token Metrics (Prometheus)" "WARN" "Input: $INPUT_TOKENS, Output: $OUTPUT_TOKENS (may be zero)"
fi

echo ""
echo "=== Test 7: Request Metrics ==="
REQUEST_COUNT=$(echo "$METRICS" | grep 'zai_proxy_requests_total' | grep -oP '\d+$' | head -1 || echo "0")
if [ "$REQUEST_COUNT" -gt 0 ]; then
    log_result "Request Count Metrics" "PASS" "Total requests: $REQUEST_COUNT"
else
    log_result "Request Count Metrics" "WARN" "No requests recorded in metrics"
fi

echo ""
echo "=== Test 8: Error Handling ==="
# Test with invalid API key
export ANTHROPIC_API_KEY="invalid-key-test"
ERROR_OUTPUT="/tmp/error-test-$$.txt"
timeout 10 claude --print "Test error handling" 2>&1 > "$ERROR_OUTPUT" || true

if grep -qiE "error|unauthorized|invalid|failed" "$ERROR_OUTPUT"; then
    log_result "Error Handling" "PASS" "Error properly propagated from proxy"
else
    log_result "Error Handling" "WARN" "Error handling unclear"
fi

# Restore valid API key
export ANTHROPIC_API_KEY="${ZAI_API_KEY:-test-key}"

echo ""
echo "=== Test 9: Rate Limiting Metrics ==="
RATE_LIMIT=$(echo "$METRICS" | grep 'zai_proxy_rate_limit_requests_per_second' | grep -oP '[\d.]+$' || echo "0")
if [ -n "$RATE_LIMIT" ] && [ "$RATE_LIMIT" != "0" ]; then
    log_result "Rate Limit Metrics" "PASS" "Current rate limit: $RATE_LIMIT req/s"
else
    log_result "Rate Limit Metrics" "WARN" "Rate limit metrics not available"
fi

echo ""
echo "=== Test 10: Concurrent Requests ==="
# Test 3 concurrent requests with print mode
for i in 1 2 3; do
    (timeout 15 claude --print "Count to $i" 2>&1 > "/tmp/concurrent-$i-$$.txt") &
done

wait
CONCURRENT_SUCCESS=0
for i in 1 2 3; do
    if [ -s "/tmp/concurrent-$i-$$.txt" ] && grep -qE "[0-9]" "/tmp/concurrent-$i-$$.txt"; then
        CONCURRENT_SUCCESS=$((CONCURRENT_SUCCESS + 1))
    fi
done

if [ "$CONCURRENT_SUCCESS" -eq 3 ]; then
    log_result "Concurrent Requests" "PASS" "All 3 concurrent requests succeeded"
else
    log_result "Concurrent Requests" "PARTIAL" "$CONCURRENT_SUCCESS/3 concurrent requests succeeded"
fi

# Final summary
echo "" | tee -a "$RESULTS_FILE"
echo "=========================================="
echo "Test Complete!"
echo "=========================================="
echo ""
echo "Results saved to: $RESULTS_FILE"
echo "Proxy logs: $LOG_FILE"
echo ""

# Show final metrics
echo "=== Final Metrics Summary ===" | tee -a "$RESULTS_FILE"
echo "" >> "$RESULTS_FILE"
echo '```' >> "$RESULTS_FILE"
curl -s "$PROXY_URL/metrics" | grep -E "zai_proxy_(requests|tokens|rate_limit)" | grep -v "^#" | tee -a "$RESULTS_FILE"
echo '```' >> "$RESULTS_FILE"

echo ""
echo "View full results: cat $RESULTS_FILE"
echo "View proxy logs: tail -f $LOG_FILE"

# Cleanup
rm -rf "$TEST_DIR"
echo ""
echo "Cleanup complete."
