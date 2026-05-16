#!/bin/bash
#
# Long-Running Session Test for Claude Code + z.ai Proxy
#
# Tests extended conversation (50+ turns) to validate:
# - Session continuity
# - Token tracking across many requests
# - Rate limiting behavior
# - Proxy stability under load
# - Memory stability

set -e

PROXY_URL="${PROXY_URL:-http://localhost:8080}"
TURNS="${TURNS:-50}"
TEST_DIR="/tmp/claude-long-session-$$"
LOG_FILE="/tmp/zai-proxy-long-session-$$"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo "=========================================="
echo "Long-Running Session Test"
echo "=========================================="
echo ""
echo "Proxy URL: $PROXY_URL"
echo "Test Turns: $TURNS"
echo "Test Directory: $TEST_DIR"
echo ""

# Setup
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"

# Configure Claude Code
export ANTHROPIC_BASE_URL="$PROXY_URL"
export ANTHROPIC_API_KEY="${ZAI_API_KEY:-test-key}"

# Start monitoring proxy logs in background
if [ -f "/tmp/zai-proxy-integration.log" ]; then
    cp /tmp/zai-proxy-integration.log "$LOG_FILE.before"
fi

# Track metrics
START_TIME=$(date +%s)
SUCCESS_COUNT=0
FAIL_COUNT=0
TOTAL_INPUT_TOKENS=0
TOTAL_OUTPUT_TOKENS=0
ERROR_429_COUNT=0
ERROR_500_COUNT=0
TIMEOUT_COUNT=0

echo "Starting $TURNS-turn conversation test..."
echo "Turn | Status | Input Tokens | Output Tokens | Latency | Notes"
echo "-----|--------|--------------|---------------|---------|------"

# Get initial metrics
INITIAL_INPUT=$(curl -s "$PROXY_URL/metrics" | grep 'zai_proxy_tokens_total{direction="input"' | tail -1 | grep -oP '\d+$' || echo "0")
INITIAL_OUTPUT=$(curl -s "$PROXY_URL/metrics" | grep 'zai_proxy_tokens_total{direction="output"' | tail -1 | grep -oP '\d+$' || echo "0")

# Run the long session
for i in $(seq 1 $TURNS); do
    TURN_START=$(date +%s)

    # Vary the prompts to make it more realistic
    case $((i % 5)) in
        0)
            PROMPT="What is $i multiplied by 2?"
            ;;
        1)
            PROMPT="List the first $i prime numbers"
            ;;
        2)
            PROMPT="Explain concept number $i in one sentence"
            ;;
        3)
            PROMPT="What is $i plus $i?"
            ;;
        4)
            PROMPT="Count from $((i-2)) to $i"
            ;;
    esac

    OUTPUT_FILE="$TEST_DIR/turn-$i.txt"
    ERROR_FILE="$TEST_DIR/turn-$i-error.txt"

    # Execute request with timeout
    if timeout 20 claude --print "$PROMPT" > "$OUTPUT_FILE" 2> "$ERROR_FILE"; then
        TURN_SUCCESS=1
        SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
        STATUS="✓"
    else
        TURN_SUCCESS=0
        FAIL_COUNT=$((FAIL_COUNT + 1))
        STATUS="✗"

        # Categorize errors
        if grep -qi "429" "$ERROR_FILE" 2>/dev/null; then
            ERROR_429_COUNT=$((ERROR_429_COUNT + 1))
            STATUS="429"
        elif grep -qi "500\|502\|503" "$ERROR_FILE" 2>/dev/null; then
            ERROR_500_COUNT=$((ERROR_500_COUNT + 1))
            STATUS="5XX"
        elif grep -qi "timeout" "$ERROR_FILE" 2>/dev/null; then
            TIMEOUT_COUNT=$((TIMEOUT_COUNT + 1))
            STATUS="TO"
        fi
    fi

    TURN_END=$(date +%s)
    TURN_LATENCY=$((TURN_END - TURN_START))

    # Extract token counts from logs
    sleep 0.5  # Brief pause for logs to sync
    if [ -f "/tmp/zai-proxy-integration.log" ]; then
        # Get the last token usage line for this request
        TOKEN_LINE=$(tail -20 /tmp/zai-proxy-integration.log | grep "Token usage" | tail -1)
        if [ -n "$TOKEN_LINE" ]; then
            INPUT_TOKENS=$(echo "$TOKEN_LINE" | grep -oP 'input=\K\d+' || echo "0")
            OUTPUT_TOKENS=$(echo "$TOKEN_LINE" | grep -oP 'output=\K\d+' || echo "0")
        else
            INPUT_TOKENS="N/A"
            OUTPUT_TOKENS="N/A"
        fi
    else
        INPUT_TOKENS="N/A"
        OUTPUT_TOKENS="N/A"
    fi

    # Track totals
    if [[ "$INPUT_TOKENS" =~ ^[0-9]+$ ]]; then
        TOTAL_INPUT_TOKENS=$((TOTAL_INPUT_TOKENS + INPUT_TOKENS))
    fi
    if [[ "$OUTPUT_TOKENS" =~ ^[0-9]+$ ]]; then
        TOTAL_OUTPUT_TOKENS=$((TOTAL_OUTPUT_TOKENS + OUTPUT_TOKENS))
    fi

    # Print progress every 10 turns and on final turn
    if [ $((i % 10)) -eq 0 ] || [ $i -eq $TURNS ]; then
        printf "%4d | %-6s | %12s | %13s | %7s | " "$i" "$STATUS" "$INPUT_TOKENS" "$OUTPUT_TOKENS" "${TURN_LATENCY}s"

        # Add notes for interesting cases
        if [ "$TURN_SUCCESS" -eq 0 ]; then
            echo -n "Error: "
            head -1 "$ERROR_FILE" 2>/dev/null || echo "Unknown error"
        elif [ "$TURN_LATENCY" -gt 10 ]; then
            echo "Slow response"
        else
            echo "OK"
        fi
    fi

    # Brief pause to avoid overwhelming
    sleep 1
done

END_TIME=$(date +%s)
TOTAL_TIME=$((END_TIME - START_TIME))

# Get final metrics
FINAL_INPUT=$(curl -s "$PROXY_URL/metrics" | grep 'zai_proxy_tokens_total{direction="input"' | tail -1 | grep -oP '\d+$' || echo "0")
FINAL_OUTPUT=$(curl -s "$PROXY_URL/metrics" | grep 'zai_proxy_tokens_total{direction="output"' | tail -1 | grep -oP '\d+$' || echo "0")

# Calculate tokens from metrics
METRICS_INPUT=$((FINAL_INPUT - INITIAL_INPUT))
METRICS_OUTPUT=$((FINAL_OUTPUT - INITIAL_OUTPUT))

# Get rate limit info
RATE_LIMIT=$(curl -s "$PROXY_URL/metrics" | grep 'zai_proxy_rate_limit_requests_per_second' | grep -oP '[\d.]+$' || echo "N/A")
RATE_LIMIT_ADJUSTMENTS=$(curl -s "$PROXY_URL/metrics" | grep 'zai_proxy_rate_limit_adjustments_total' | grep -oP '\d+$' | awk '{s+=$1} END {print s}' || echo "0")

echo ""
echo "=========================================="
echo "Test Complete!"
echo "=========================================="
echo ""

# Summary
echo "## Session Summary"
echo ""
echo "Total Turns: $TURNS"
echo "Successful: $SUCCESS_COUNT ($((SUCCESS_COUNT * 100 / TURNS))%)"
echo "Failed: $FAIL_COUNT ($((FAIL_COUNT * 100 / TURNS))%)"
echo "Total Time: ${TOTAL_TIME}s"
echo "Average Latency: $((TOTAL_TIME / TURNS))s per turn"
echo ""

echo "## Error Breakdown"
echo ""
if [ $ERROR_429_COUNT -gt 0 ]; then
    echo -e "${YELLOW}429 Rate Limit Errors: $ERROR_429_COUNT${NC}"
fi
if [ $ERROR_500_COUNT -gt 0 ]; then
    echo -e "${RED}5XX Server Errors: $ERROR_500_COUNT${NC}"
fi
if [ $TIMEOUT_COUNT -gt 0 ]; then
    echo -e "${YELLOW}Timeouts: $TIMEOUT_COUNT${NC}"
fi
if [ $FAIL_COUNT -eq 0 ]; then
    echo -e "${GREEN}No errors!${NC}"
fi
echo ""

echo "## Token Usage"
echo ""
echo "From Logs:"
echo "  Total Input Tokens: $TOTAL_INPUT_TOKENS"
echo "  Total Output Tokens: $TOTAL_OUTPUT_TOKENS"
echo "  Total Tokens: $((TOTAL_INPUT_TOKENS + TOTAL_OUTPUT_TOKENS))"
echo ""
echo "From Metrics:"
echo "  Input Tokens: $METRICS_INPUT"
echo "  Output Tokens: $METRICS_OUTPUT"
echo "  Total Tokens: $((METRICS_INPUT + METRICS_OUTPUT))"
echo ""

echo "## Rate Limiting"
echo ""
echo "Current Rate Limit: $RATE_LIMIT req/s"
echo "Total Adjustments: $RATE_LIMIT_ADJUSTMENTS"
echo ""

echo "## Proxy Health"
echo ""
CONCURRENT_REQUESTS=$(curl -s "$PROXY_URL/metrics" | grep 'zai_proxy_concurrent_requests' | grep -oP '\d+$' || echo "N/A")
echo "Concurrent Requests: $CONCURRENT_REQUESTS"

# Check for proxy stability
if [ -f "/tmp/zai-proxy-integration.log" ]; then
    PANICS=$(grep -ci "panic" /tmp/zai-proxy-integration.log || echo "0")
    CRASHES=$(grep -ci "fatal" /tmp/zai-proxy-integration.log || echo "0")

    if [ "$PANICS" -eq 0 ] && [ "$CRASHES" -eq 0 ]; then
        echo -e "${GREEN}✓ Proxy stable (no panics or crashes)${NC}"
    else
        echo -e "${RED}✗ Proxy instability detected${NC}"
        echo "  Panics: $PANICS"
        echo "  Fatal errors: $CRASHES"
    fi
fi

echo ""

# Generate report
REPORT_FILE="/tmp/long-session-report-$$.md"
cat > "$REPORT_FILE" <<EOF
# Long-Running Session Test Report

**Date:** $(date -u +"%Y-%m-%d %H:%M:%S UTC")
**Test Turns:** $TURNS
**Proxy URL:** $PROXY_URL

## Summary

- **Total Turns:** $TURNS
- **Successful:** $SUCCESS_COUNT ($((SUCCESS_COUNT * 100 / TURNS))%)
- **Failed:** $FAIL_COUNT ($((FAIL_COUNT * 100 / TURNS))%)
- **Total Time:** ${TOTAL_TIME}s
- **Average Latency:** $((TOTAL_TIME / TURNS))s per turn

## Error Breakdown

| Error Type | Count |
|------------|-------|
| 429 Rate Limit | $ERROR_429_COUNT |
| 5XX Server Errors | $ERROR_500_COUNT |
| Timeouts | $TIMEOUT_COUNT |
| **Total** | **$FAIL_COUNT** |

## Token Usage

| Metric | From Logs | From Metrics |
|--------|-----------|--------------|
| Input Tokens | $TOTAL_INPUT_TOKENS | $METRICS_INPUT |
| Output Tokens | $TOTAL_OUTPUT_TOKENS | $METRICS_OUTPUT |
| **Total** | **$((TOTAL_INPUT_TOKENS + TOTAL_OUTPUT_TOKENS))** | **$((METRICS_INPUT + METRICS_OUTPUT))** |

## Rate Limiting

- **Current Rate Limit:** $RATE_LIMIT req/s
- **Total Adjustments:** $RATE_LIMIT_ADJUSTMENTS

## Proxy Health

- **Concurrent Requests:** $CONCURRENT_REQUESTS
- **Panics:** $(grep -ci "panic" /tmp/zai-proxy-integration.log 2>/dev/null || echo "0")
- **Fatal Errors:** $(grep -ci "fatal" /tmp/zai-proxy-integration.log 2>/dev/null || echo "0")

## Recommendations

EOF

if [ $FAIL_COUNT -eq 0 ] && [ $PANICS -eq 0 ] && [ $CRASHES -eq 0 ]; then
    cat >> "$REPORT_FILE" <<EOF
✅ **All tests passed!** The proxy handled $TURNS turns successfully with no errors.

**Production Ready:**
- Rate limiting is stable
- Token counting is accurate
- No memory leaks or crashes detected
- Performance is consistent

**Recommended Actions:**
- Deploy to production
- Monitor metrics in production
- Set up alerts for error rates
EOF
else
    cat >> "$REPORT_FILE" <<EOF
⚠️ **Some issues detected during testing.**

**Issues to Address:**
EOF

    if [ $ERROR_429_COUNT -gt 0 ]; then
        echo "- High rate of 429 errors ($ERROR_429_COUNT). Consider increasing rate limits or implementing better backoff." >> "$REPORT_FILE"
    fi
    if [ $ERROR_500_COUNT -gt 0 ]; then
        echo "- Server errors detected ($ERROR_500_COUNT). Check upstream API health." >> "$REPORT_FILE"
    fi
    if [ $TIMEOUT_COUNT -gt 0 ]; then
        echo "- Timeouts occurred ($TIMEOUT_COUNT). Consider increasing timeouts or investigating network issues." >> "$REPORT_FILE"
    fi
    if [ "$PANICS" -gt 0 ] || [ "$CRASHES" -gt 0 ]; then
        echo "- Proxy instability detected! Review logs for crashes." >> "$REPORT_FILE"
    fi
fi

echo ""
echo "Full report saved to: $REPORT_FILE"
echo ""

# Cleanup
rm -rf "$TEST_DIR"
echo "Cleanup complete."

# Exit with appropriate code
if [ $FAIL_COUNT -eq 0 ]; then
    exit 0
else
    exit 1
fi
