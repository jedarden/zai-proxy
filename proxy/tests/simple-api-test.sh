#!/bin/bash

# Simple API Test - Quick validation of z.ai proxy functionality
# Tests basic API calls, streaming, token counting

set -e

PROXY_URL="http://localhost:8080"
API_KEY="${ZAI_API_KEY:-test-key}"

echo "=== z.ai Proxy Simple API Test ==="
echo "Proxy: $PROXY_URL"
echo ""

# Test 1: Health check
echo "Test 1: Health Check"
if curl -sf "$PROXY_URL/metrics" >/dev/null; then
    echo "✓ Proxy is accessible"
else
    echo "✗ Proxy is not accessible"
    exit 1
fi
echo ""

# Test 2: Basic API call
echo "Test 2: Basic API Call (non-streaming)"
echo "Making request..."
RESPONSE=$(curl -s -X POST "$PROXY_URL/v1/messages" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    --max-time 30 \
    -d '{
        "model": "glm-4-flash",
        "messages": [{"role": "user", "content": "Say hi in 2 words"}],
        "max_tokens": 10
    }')

if echo "$RESPONSE" | grep -q "content"; then
    echo "✓ API call successful"
    echo "Response preview:"
    echo "$RESPONSE" | jq -r '.content[0].text' 2>/dev/null || echo "$RESPONSE" | grep -o '"text":"[^"]*"' | head -1
else
    echo "✗ API call failed"
    echo "Response: $RESPONSE"
fi
echo ""

# Test 3: Streaming
echo "Test 3: Streaming API Call"
echo "Making streaming request..."
STREAM_FILE="/tmp/stream-test-$$.txt"
curl -s -X POST "$PROXY_URL/v1/messages" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    --max-time 30 \
    -d '{
        "model": "glm-4-flash",
        "messages": [{"role": "user", "content": "Count 1, 2, 3"}],
        "max_tokens": 30,
        "stream": true
    }' > "$STREAM_FILE"

if grep -q "event:" "$STREAM_FILE"; then
    echo "✓ Streaming successful (received SSE events)"
    EVENT_COUNT=$(grep -c "event:" "$STREAM_FILE")
    echo "  Events received: $EVENT_COUNT"
else
    echo "✗ Streaming failed"
    echo "Response preview:"
    head -5 "$STREAM_FILE"
fi
rm -f "$STREAM_FILE"
echo ""

# Test 4: Token counting metrics
echo "Test 4: Token Counting Metrics"
sleep 1  # Let metrics update
METRICS=$(curl -s "$PROXY_URL/metrics")

INPUT_TOKENS=$(echo "$METRICS" | grep 'zai_proxy_tokens_total{direction="input"' | tail -1 | grep -oP '\d+$' || echo "0")
OUTPUT_TOKENS=$(echo "$METRICS" | grep 'zai_proxy_tokens_total{direction="output"' | tail -1 | grep -oP '\d+$' || echo "0")

echo "Input tokens: $INPUT_TOKENS"
echo "Output tokens: $OUTPUT_TOKENS"

if [[ "$INPUT_TOKENS" -gt 0 ]] && [[ "$OUTPUT_TOKENS" -gt 0 ]]; then
    echo "✓ Token counting is working"
else
    echo "⚠ Token counting may not be active"
fi
echo ""

# Test 5: Multi-request test
echo "Test 5: Multiple Requests (5 quick calls)"
SUCCESS=0
for i in {1..5}; do
    RESP=$(curl -s -X POST "$PROXY_URL/v1/messages" \
        -H "Content-Type: application/json" \
        -H "x-api-key: $API_KEY" \
        -H "anthropic-version: 2023-06-01" \
        --max-time 30 \
        -d "{
            \"model\": \"glm-4-flash\",
            \"messages\": [{\"role\": \"user\", \"content\": \"$i\"}],
            \"max_tokens\": 5
        }")

    if echo "$RESP" | grep -q "content"; then
        ((SUCCESS++))
    fi
done

echo "Success rate: $SUCCESS/5"
if [[ $SUCCESS -ge 4 ]]; then
    echo "✓ Multiple requests working"
else
    echo "⚠ Some requests failed ($SUCCESS/5)"
fi
echo ""

# Test 6: Rate limit metrics
echo "Test 6: Rate Limiting Metrics"
RATE_LIMIT=$(echo "$METRICS" | grep 'zai_proxy_rate_limit_requests_per_second' | tail -1 | grep -oP '[\d.]+$' || echo "N/A")
echo "Current rate limit: $RATE_LIMIT req/s"

if [[ "$RATE_LIMIT" != "N/A" ]]; then
    echo "✓ Rate limiting metrics available"
else
    echo "⚠ Rate limiting metrics not found"
fi
echo ""

# Test 7: Proxy logs check
echo "Test 7: Proxy Logs"
LOG_FILE="/tmp/zai-proxy-integration.log"
if [[ -f "$LOG_FILE" ]]; then
    echo "Last 5 log lines:"
    tail -5 "$LOG_FILE"
    echo ""
    if grep -q "Token usage" "$LOG_FILE"; then
        echo "✓ Token usage logging active"
        echo "Example: $(grep "Token usage" "$LOG_FILE" | tail -1)"
    else
        echo "⚠ No token usage in logs"
    fi
else
    echo "⚠ Log file not found at $LOG_FILE"
fi
echo ""

echo "=== Test Summary ==="
echo "All basic tests completed!"
echo ""
echo "Metrics snapshot:"
curl -s "$PROXY_URL/metrics" | grep -E "zai_proxy_(requests_total|tokens_total|rate_limit_requests)" | head -10
echo ""
echo "Test complete."
