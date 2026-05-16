#!/bin/bash

# Direct API Test - Tests z.ai proxy API compatibility
# Simulates Claude Code client behavior without requiring full Claude Code installation

set -euo pipefail

PROXY_URL="${PROXY_URL:-http://localhost:8080}"
API_KEY="${ZAI_API_KEY:-test-key}"
RESULTS_FILE="/tmp/direct-api-test-results-$(date +%s).md"

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

TESTS_RUN=0
TESTS_PASSED=0

log() {
    echo -e "$@" | tee -a "$RESULTS_FILE"
}

test_api() {
    local test_name="$1"
    local method="${2:-POST}"
    local endpoint="${3:-/v1/messages}"
    local data="$4"
    local expected="$5"

    ((TESTS_RUN++))
    log "\n### Test $TESTS_RUN: $test_name"

    local response=$(curl -s -w "\n%{http_code}" -X "$method" "${PROXY_URL}${endpoint}" \
        -H "Content-Type: application/json" \
        -H "x-api-key: $API_KEY" \
        -H "anthropic-version: 2023-06-01" \
        -d "$data")

    local http_code=$(echo "$response" | tail -1)
    local body=$(echo "$response" | head -n -1)

    log "**HTTP Status:** $http_code"
    log "**Response:** \`\`\`json"
    echo "$body" | jq '.' 2>/dev/null || echo "$body" | tee -a "$RESULTS_FILE" >/dev/null
    log "\`\`\`"

    if echo "$body" | grep -q "$expected"; then
        log "${GREEN}✓ PASS${NC}"
        ((TESTS_PASSED++))
        return 0
    else
        log "${RED}✗ FAIL${NC} - Expected to find: $expected"
        return 1
    fi
}

# Initialize results
cat > "$RESULTS_FILE" <<EOF
# Direct API Test Results
**Date:** $(date -u +"%Y-%m-%d %H:%M:%S UTC")
**Proxy URL:** $PROXY_URL

---

EOF

log "# Running Direct API Tests\n"

# Test 1: Basic non-streaming request
test_api "Basic non-streaming request" "POST" "/v1/messages" '{
    "model": "claude-3-sonnet",
    "messages": [{"role": "user", "content": "Say hello in 3 words"}],
    "max_tokens": 20
}' "content"

# Test 2: Streaming request
log "\n### Test $((TESTS_RUN + 1)): Streaming request"
((TESTS_RUN++))

STREAM_OUTPUT="/tmp/stream-test-$$.txt"
curl -s -X POST "${PROXY_URL}/v1/messages" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    -d '{
        "model": "claude-3-sonnet",
        "messages": [{"role": "user", "content": "Count: 1, 2, 3"}],
        "max_tokens": 50,
        "stream": true
    }' > "$STREAM_OUTPUT"

log "**Streaming response length:** $(wc -c < "$STREAM_OUTPUT") bytes"

if grep -q "event:" "$STREAM_OUTPUT" && grep -q "data:" "$STREAM_OUTPUT"; then
    log "${GREEN}✓ PASS${NC} - Received SSE events"
    ((TESTS_PASSED++))
else
    log "${RED}✗ FAIL${NC} - No SSE events in response"
    log "\`\`\`"
    head -20 "$STREAM_OUTPUT" | tee -a "$RESULTS_FILE" >/dev/null
    log "\`\`\`"
fi

# Test 3: Multi-turn simulation (5 quick requests)
log "\n### Test $((TESTS_RUN + 1)): Multi-turn conversation (5 requests)"
((TESTS_RUN++))

MULTI_SUCCESS=0
for i in {1..5}; do
    response=$(curl -s -X POST "${PROXY_URL}/v1/messages" \
        -H "Content-Type: application/json" \
        -H "x-api-key: $API_KEY" \
        -H "anthropic-version: 2023-06-01" \
        -d "{
            \"model\": \"claude-3-sonnet\",
            \"messages\": [{\"role\": \"user\", \"content\": \"Turn $i\"}],
            \"max_tokens\": 15
        }")

    if echo "$response" | grep -q "content"; then
        ((MULTI_SUCCESS++))
    fi
    sleep 0.3
done

log "**Success rate:** $MULTI_SUCCESS/5"
if [[ $MULTI_SUCCESS -eq 5 ]]; then
    log "${GREEN}✓ PASS${NC}"
    ((TESTS_PASSED++))
else
    log "${YELLOW}⚠ PARTIAL${NC} - $MULTI_SUCCESS/5 succeeded"
fi

# Test 4: Token counting verification
log "\n### Test $((TESTS_RUN + 1)): Token counting metrics"
((TESTS_RUN++))

METRICS=$(curl -s "${PROXY_URL}/metrics")
INPUT_TOKENS=$(echo "$METRICS" | grep 'zai_proxy_tokens_total{direction="input"' | grep -oP '\d+$' | head -1 || echo "0")
OUTPUT_TOKENS=$(echo "$METRICS" | grep 'zai_proxy_tokens_total{direction="output"' | grep -oP '\d+$' | head -1 || echo "0")

log "**Input tokens:** $INPUT_TOKENS"
log "**Output tokens:** $OUTPUT_TOKENS"

if [[ $INPUT_TOKENS -gt 0 ]] && [[ $OUTPUT_TOKENS -gt 0 ]]; then
    log "${GREEN}✓ PASS${NC} - Token counting active"
    ((TESTS_PASSED++))
else
    log "${RED}✗ FAIL${NC} - Token metrics not increasing"
fi

# Test 5: Error handling
test_api "Error handling (invalid request)" "POST" "/v1/messages" '{
    "model": "claude-3-sonnet",
    "messages": [{"role": "invalid_role", "content": "test"}],
    "max_tokens": 10
}' "error"

# Test 6: Rate limiting metrics
log "\n### Test $((TESTS_RUN + 1)): Rate limiting metrics"
((TESTS_RUN++))

RATE_LIMIT=$(echo "$METRICS" | grep 'zai_proxy_rate_limit_requests_per_second' | grep -oP '[\d.]+$' | head -1 || echo "0")
log "**Current rate limit:** $RATE_LIMIT req/s"

if [[ -n "$RATE_LIMIT" ]] && [[ "$RATE_LIMIT" != "0" ]]; then
    log "${GREEN}✓ PASS${NC}"
    ((TESTS_PASSED++))
else
    log "${YELLOW}⚠ WARN${NC} - Rate limit metrics not available"
fi

# Summary
log "\n---\n"
log "## Test Summary\n"
log "**Tests run:** $TESTS_RUN"
log "**Tests passed:** $TESTS_PASSED"
log "**Pass rate:** $(echo "scale=1; $TESTS_PASSED * 100 / $TESTS_RUN" | bc)%\n"

# Metrics snapshot
log "## Prometheus Metrics Snapshot\n"
log "\`\`\`"
curl -s "${PROXY_URL}/metrics" | grep -E "zai_proxy_(requests|tokens|rate)" | grep -v "^#" | tee -a "$RESULTS_FILE" >/dev/null
log "\`\`\`"

# Final output
echo ""
echo "=========================================="
if [[ $TESTS_PASSED -eq $TESTS_RUN ]]; then
    echo -e "${GREEN}All tests passed!${NC} ($TESTS_PASSED/$TESTS_RUN)"
else
    echo -e "${YELLOW}Some tests failed.${NC} ($TESTS_PASSED/$TESTS_RUN passed)"
fi
echo "=========================================="
echo ""
echo "Results saved to: $RESULTS_FILE"
echo "View: cat $RESULTS_FILE"
