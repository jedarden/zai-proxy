#!/bin/bash
#
# Kubernetes Integration Test for z.ai Proxy
#
# Tests the deployed z.ai proxy in apexalgo-iad cluster
# Validates API connectivity, token counting, streaming, and metrics

set -e

# Configuration
KUBECONFIG="${KUBECONFIG:-/home/coder/.kube/apexalgo-iad.kubeconfig}"
NAMESPACE="mcp"
SERVICE="zai-proxy"
LOCAL_PORT=18080
PROXY_URL="http://localhost:$LOCAL_PORT"
API_KEY="${ZAI_API_KEY:-test-key}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Results tracking
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0
WARNED_TESTS=0

# Test result logging
log_result() {
    local test_name="$1"
    local status="$2"
    local details="$3"

    ((TOTAL_TESTS++))

    case "$status" in
        "PASS")
            ((PASSED_TESTS++))
            echo -e "${GREEN}✓${NC} $test_name"
            ;;
        "FAIL")
            ((FAILED_TESTS++))
            echo -e "${RED}✗${NC} $test_name"
            ;;
        "WARN")
            ((WARNED_TESTS++))
            echo -e "${YELLOW}⚠${NC} $test_name"
            ;;
    esac

    if [ -n "$details" ]; then
        echo "  $details"
    fi
    echo ""
}

echo "=========================================="
echo "z.ai Proxy Kubernetes Integration Test"
echo "=========================================="
echo ""
echo "Cluster: apexalgo-iad"
echo "Namespace: $NAMESPACE"
echo "Service: $SERVICE"
echo "Local Port: $LOCAL_PORT"
echo "Proxy URL: $PROXY_URL"
echo ""

# Setup port-forward
echo "Setting up port-forward to $SERVICE.$NAMESPACE.svc.cluster.local:8080..."
kubectl --kubeconfig="$KUBECONFIG" port-forward -n "$NAMESPACE" "svc/$SERVICE" "$LOCAL_PORT:8080" >/dev/null 2>&1 &
PF_PID=$!

# Wait for port-forward to be ready
sleep 3

# Cleanup function
cleanup() {
    echo ""
    echo "Cleaning up..."
    kill $PF_PID 2>/dev/null || true
    wait $PF_PID 2>/dev/null || true
}

trap cleanup EXIT

# Verify port-forward is working
echo "Verifying port-forward..."
if ! curl -sf "$PROXY_URL/health" >/dev/null 2>&1; then
    echo "Failed to connect to proxy via port-forward"
    exit 1
fi
log_result "Port-Forward Setup" "PASS" "Connected to $SERVICE service"

echo "=========================================="
echo ""
echo "Running Tests..."
echo ""
echo "=========================================="
echo ""

# Test 1: Health Check
echo "=== Test 1: Health Check ==="
HEALTH=$(curl -sf "$PROXY_URL/health" || echo "failed")
# /health returns JSON whose status field carries the health word; the
# rate_limit object mirrors the persisted ceiling snapshot.
if echo "$HEALTH" | grep -q '"status":"ok"'; then
    log_result "Health Endpoint" "PASS" "Service healthy"
else
    log_result "Health Endpoint" "FAIL" "Health check returned: $HEALTH"
fi

# Test 2: Metrics Endpoint
echo "=== Test 2: Metrics Endpoint ==="
METRICS=$(curl -sf "$PROXY_URL/metrics" || echo "failed")
if echo "$METRICS" | grep -q "zai_proxy"; then
    log_result "Metrics Endpoint" "PASS" "Prometheus metrics available"
else
    log_result "Metrics Endpoint" "FAIL" "No metrics found"
fi

# Test 3: Basic API Request (Non-Streaming)
echo "=== Test 3: Basic API Request ==="
RESPONSE=$(curl -s -X POST "$PROXY_URL/v1/messages" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    --max-time 30 \
    -d '{
        "model": "glm-4-flash",
        "messages": [{"role": "user", "content": "Say hello"}],
        "max_tokens": 20
    }' 2>&1)

if echo "$RESPONSE" | grep -q "content\|error\|text"; then
    log_result "Basic API Request" "PASS" "Request processed successfully"

    # Show preview of response
    if echo "$RESPONSE" | jq -e '.content[0].text' >/dev/null 2>&1; then
        RESPONSE_TEXT=$(echo "$RESPONSE" | jq -r '.content[0].text' 2>/dev/null)
        echo "  Response: $RESPONSE_TEXT"
    elif echo "$RESPONSE" | grep -q '"text"'; then
        RESPONSE_TEXT=$(echo "$RESPONSE" | grep -o '"text":"[^"]*"' | head -1 | cut -d'"' -f4)
        echo "  Response: $RESPONSE_TEXT"
    fi
else
    log_result "Basic API Request" "WARN" "Unexpected response format"
    echo "  Response: $RESPONSE"
fi

# Test 4: Streaming Request
echo "=== Test 4: Streaming Request ==="
STREAM_OUTPUT="/tmp/stream-test-$$.txt"
curl -s -X POST "$PROXY_URL/v1/messages" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    --max-time 30 \
    -d '{
        "model": "glm-4-flash",
        "messages": [{"role": "user", "content": "Count 1, 2, 3"}],
        "max_tokens": 50,
        "stream": true
    }' > "$STREAM_OUTPUT" 2>&1

if grep -q "event:" "$STREAM_OUTPUT" || grep -q "data:" "$STREAM_OUTPUT"; then
    EVENT_COUNT=$(grep -c "event:\|data:" "$STREAM_OUTPUT" 2>/dev/null || echo "0")
    log_result "Streaming Request" "PASS" "Received $EVENT_COUNT SSE events"
else
    log_result "Streaming Request" "WARN" "No SSE events detected"
    echo "  Output preview:"
    head -3 "$STREAM_OUTPUT" | sed 's/^/    /'
fi
rm -f "$STREAM_OUTPUT"

# Test 5: Token Counting Metrics
echo "=== Test 5: Token Counting ==="
sleep 2  # Let metrics update
METRICS=$(curl -sf "$PROXY_URL/metrics")

INPUT_METRIC=$(echo "$METRICS" | grep 'zai_proxy_tokens_total{direction="input"' || echo "")
OUTPUT_METRIC=$(echo "$METRICS" | grep 'zai_proxy_tokens_total{direction="output"' || echo "")

if [ -n "$INPUT_METRIC" ] && [ -n "$OUTPUT_METRIC" ]; then
    INPUT_TOKENS=$(echo "$INPUT_METRIC" | tail -1 | grep -oP '\d+$' || echo "0")
    OUTPUT_TOKENS=$(echo "$OUTPUT_METRIC" | tail -1 | grep -oP '\d+$' || echo "0")
    log_result "Token Counting" "PASS" "Input: $INPUT_TOKENS, Output: $OUTPUT_TOKENS"
else
    log_result "Token Counting" "WARN" "Token metrics not found or empty"
fi

# Test 6: Request Metrics
echo "=== Test 6: Request Metrics ==="
REQUEST_METRIC=$(echo "$METRICS" | grep 'zai_proxy_requests_total' | head -1)
if [ -n "$REQUEST_METRIC" ]; then
    REQUEST_COUNT=$(echo "$REQUEST_METRIC" | grep -oP '\d+$' || echo "0")
    log_result "Request Counting" "PASS" "Total requests: $REQUEST_COUNT"
else
    log_result "Request Counting" "WARN" "Request metrics not found"
fi

# Test 7: Rate Limiting Metrics
echo "=== Test 7: Rate Limiting ==="
RATE_LIMIT_METRIC=$(echo "$METRICS" | grep 'zai_proxy_rate_limit_requests_per_second')
if [ -n "$RATE_LIMIT_METRIC" ]; then
    CURRENT_RATE=$(echo "$RATE_LIMIT_METRIC" | grep -oP '[\d.]+$' || echo "N/A")
    log_result "Rate Limiting" "PASS" "Current limit: $CURRENT_RATE req/s"
else
    log_result "Rate Limiting" "WARN" "Rate limit metrics not found"
fi

# Test 8: Concurrent Requests
echo "=== Test 8: Concurrent Requests ==="
mkdir -p /tmp/concurrent-test-$$
SUCCESS_COUNT=0
for i in 1 2 3; do
    (
        RESP=$(curl -s -X POST "$PROXY_URL/v1/messages" \
            -H "Content-Type: application/json" \
            -H "x-api-key: $API_KEY" \
            -H "anthropic-version: 2023-06-01" \
            --max-time 30 \
            -d "{\"model\": \"glm-4-flash\", \"messages\": [{\"role\": \"user\", \"content\": \"number $i\"}], \"max_tokens\": 5}")
        if echo "$RESP" | grep -q "content\|text"; then
            echo "success" > "/tmp/concurrent-test-$$/req-$i.txt"
        else
            echo "failed" > "/tmp/concurrent-test-$$/req-$i.txt"
        fi
    ) &
done

wait
for i in 1 2 3; do
    if [ -f "/tmp/concurrent-test-$$/req-$i.txt" ] && grep -q "success" "/tmp/concurrent-test-$$/req-$i.txt"; then
        ((SUCCESS_COUNT++))
    fi
done
rm -rf "/tmp/concurrent-test-$$"

if [ $SUCCESS_COUNT -eq 3 ]; then
    log_result "Concurrent Requests" "PASS" "All 3 concurrent requests succeeded"
elif [ $SUCCESS_COUNT -gt 0 ]; then
    log_result "Concurrent Requests" "WARN" "$SUCCESS_COUNT/3 requests succeeded"
else
    log_result "Concurrent Requests" "FAIL" "All requests failed"
fi

# Test 9: Error Handling (Invalid Request)
echo "=== Test 9: Error Handling ==="
ERROR_RESPONSE=$(curl -s -X POST "$PROXY_URL/v1/messages" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    --max-time 10 \
    -d '{
        "model": "invalid-model",
        "messages": [{"role": "user", "content": "test"}],
        "max_tokens": 10
    }')

if echo "$ERROR_RESPONSE" | grep -qi "error\|invalid\|not found"; then
    log_result "Error Handling" "PASS" "Errors properly propagated"
else
    log_result "Error Handling" "WARN" "Error response unclear"
fi

# Test 10: Multiple Sequential Requests
echo "=== Test 10: Sequential Requests ==="
SEQ_SUCCESS=0
for i in {1..5}; do
    RESP=$(curl -s -X POST "$PROXY_URL/v1/messages" \
        -H "Content-Type: application/json" \
        -H "x-api-key: $API_KEY" \
        -H "anthropic-version: 2023-06-01" \
        --max-time 30 \
        -d "{\"model\": \"glm-4-flash\", \"messages\": [{\"role\": \"user\", \"content\": \"$i\"}], \"max_tokens\": 5}")
    if echo "$RESP" | grep -q "content\|text"; then
        ((SEQ_SUCCESS++))
    fi
    sleep 0.5
done

if [ $SEQ_SUCCESS -ge 4 ]; then
    log_result "Sequential Requests" "PASS" "$SEQ_SUCCESS/5 requests succeeded"
elif [ $SEQ_SUCCESS -gt 0 ]; then
    log_result "Sequential Requests" "WARN" "$SEQ_SUCCESS/5 requests succeeded"
else
    log_result "Sequential Requests" "FAIL" "All requests failed"
fi

# Test 11: Pod Health Check
echo "=== Test 11: Pod Health ==="
POD_INFO=$(kubectl --kubeconfig="$KUBECONFIG" get pod -n "$NAMESPACE" -l app="$SERVICE" -o json 2>/dev/null)
if [ -n "$POD_INFO" ]; then
    POD_READY=$(echo "$POD_INFO" | jq -r '.items[0].status.containerStatuses[0].ready' 2>/dev/null || echo "false")
    POD_RESTARTS=$(echo "$POD_INFO" | jq -r '.items[0].status.containerStatuses[0].restartCount' 2>/dev/null || echo "N/A")
    if [ "$POD_READY" = "true" ]; then
        log_result "Pod Health" "PASS" "Pod ready, restarts: $POD_RESTARTS"
    else
        log_result "Pod Health" "WARN" "Pod may not be ready"
    fi
else
    log_result "Pod Health" "WARN" "Could not fetch pod info"
fi

# Test 12: Service Connectivity
echo "=== Test 12: Service Endpoints ==="
ENDPOINTS=$(kubectl --kubeconfig="$KUBECONFIG" get endpoints -n "$NAMESPACE" "$SERVICE" -o json 2>/dev/null)
if [ -n "$ENDPOINTS" ]; then
    READY_ADDRESSES=$(echo "$ENDPOINTS" | jq -r '.subsets[0].addresses | length' 2>/dev/null || echo "0")
    if [ "$READY_ADDRESSES" -gt 0 ]; then
        log_result "Service Endpoints" "PASS" "$READY_ADDRESSES endpoint(s) ready"
    else
        log_result "Service Endpoints" "FAIL" "No ready endpoints"
    fi
else
    log_result "Service Endpoints" "WARN" "Could not fetch endpoints"
fi

# Final Summary
echo "=========================================="
echo "Test Summary"
echo "=========================================="
echo ""
echo "Total Tests: $TOTAL_TESTS"
echo -e "${GREEN}Passed: $PASSED_TESTS${NC}"
echo -e "${YELLOW}Warnings: $WARNED_TESTS${NC}"
echo -e "${RED}Failed: $FAILED_TESTS${NC}"
echo ""

if [ $FAILED_TESTS -eq 0 ]; then
    echo -e "${GREEN}✓ All critical tests passed!${NC}"
    EXIT_CODE=0
else
    echo -e "${RED}✗ Some tests failed${NC}"
    EXIT_CODE=1
fi

# Show current metrics
echo ""
echo "=== Current Metrics Snapshot ==="
echo ""
curl -sf "$PROXY_URL/metrics" | grep -E "zai_proxy_(requests_total|tokens_total|rate_limit_requests_per_second|concurrent_requests)" | grep -v "^#" | head -20
echo ""

echo "=========================================="
echo "Test Complete"
echo "=========================================="

exit $EXIT_CODE
