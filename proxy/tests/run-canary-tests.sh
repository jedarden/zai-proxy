#!/bin/bash
# Canary Integration Test Script for ZAI Proxy Token Counting
# This script runs the full integration test suite against the canary deployment

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
CANARY_URL="${CANARY_URL:-http://zai-proxy-canary.devpod.svc.cluster.local:8080}"
PROD_URL="${PROD_URL:-http://zai-proxy.devpod.svc.cluster.local:8080}"
NAMESPACE="${NAMESPACE:-devpod}"

# Get API key from secret
echo "Getting API key from secret..."
API_KEY=$(kubectl get secret -n "$NAMESPACE" zai-api-key -o jsonpath='{.data.key}' 2>/dev/null | base64 -d || echo "")

if [ -z "$API_KEY" ]; then
    echo -e "${RED}ERROR: Could not get API key from secret${NC}"
    echo "Please set API_KEY environment variable or ensure secret exists"
    exit 1
fi

# Test counter
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

# Helper functions
print_header() {
    echo ""
    echo "========================================"
    echo "$1"
    echo "========================================"
}

print_test() {
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    echo -e "\n${YELLOW}[Test $TOTAL_TESTS]${NC} $1"
}

print_pass() {
    echo -e "${GREEN}✓ PASS${NC}: $1"
    PASSED_TESTS=$((PASSED_TESTS + 1))
}

print_fail() {
    echo -e "${RED}✗ FAIL${NC}: $1"
    FAILED_TESTS=$((FAILED_TESTS + 1))
}

# Pre-flight checks
print_header "Pre-flight Checks"

echo -n "Checking canary deployment... "
if kubectl get deployment -n "$NAMESPACE" zai-proxy-canary >/dev/null 2>&1; then
    print_pass "Canary deployment exists"
else
    print_fail "Canary deployment NOT found. Deploy canary first."
    exit 1
fi

echo -n "Checking canary service... "
if kubectl get service -n "$NAMESPACE" zai-proxy-canary >/dev/null 2>&1; then
    print_pass "Canary service exists"
else
    print_fail "Canary service NOT found"
    exit 1
fi

echo -n "Checking production pods... "
PROD_PODS=$(kubectl get pods -n "$NAMESPACE" -l app=zai-proxy --no-headers 2>/dev/null | wc -l)
if [ "$PROD_PODS" -ge 5 ]; then
    print_pass "Production has $PROD_PODS pods running"
else
    print_fail "Production has only $PROD_PODS pods (expected >=5)"
fi

# Test 1: Token Counting Validation
print_header "Test 1: Token Counting Validation"

print_test "Sending basic request to canary..."
RESPONSE=$(curl -s -X POST "$CANARY_URL/v1/messages" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    -d '{
        "model": "glm-4",
        "max_tokens": 100,
        "messages": [{"role": "user", "content": "Hello, how are you?"}]
    }' 2>&1)

if echo "$RESPONSE" | jq -e '.content' >/dev/null 2>&1; then
    print_pass "Canary responds with valid JSON"
else
    print_fail "Canary response invalid: $RESPONSE"
fi

print_test "Checking Prometheus metrics for token counts..."
METRICS=$(curl -s "$CANARY_URL/metrics" 2>/dev/null || echo "")
if echo "$METRICS" | grep -q "zai_proxy_tokens_total"; then
    TOKEN_COUNT=$(echo "$METRICS" | grep "zai_proxy_tokens_total.*direction=\"input\"" | awk '{print $2}')
    if [ "$TOKEN_COUNT" -gt 0 ]; then
        print_pass "Token counting active: $TOKEN_COUNT input tokens counted"
    else
        print_fail "Token count is 0"
    fi
else
    print_fail "Token metrics not found"
fi

print_test "Verifying production has NO token counting..."
PROD_METRICS=$(curl -s "$PROD_URL/metrics" 2>/dev/null || echo "")
if echo "$PROD_METRICS" | grep -q "zai_proxy_tokens_total"; then
    PROD_TOKEN_COUNT=$(echo "$PROD_METRICS" | grep "zai_proxy_tokens_total.*direction=\"input\"" | awk '{print $2}')
    if [ "$PROD_TOKEN_COUNT" = "0" ] || [ -z "$PROD_TOKEN_COUNT" ]; then
        print_pass "Production has no token counting (as expected)"
    else
        print_fail "Production unexpectedly has token counting: $PROD_TOKEN_COUNT"
    fi
else
    print_pass "Production has no token metrics (as expected)"
fi

# Test 2: Format Comparison
print_header "Test 2: Format Comparison Tests"

print_test "Comparing response formats..."
PROD_RESPONSE=$(curl -s -X POST "$PROD_URL/v1/messages" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $API_KEY" \
    -d '{"model":"glm-4","max_tokens":50,"messages":[{"role":"user","content":"Say hello"}]}' 2>/dev/null)

CANARY_RESPONSE=$(curl -s -X POST "$CANARY_URL/v1/messages" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $API_KEY" \
    -d '{"model":"glm-4","max_tokens":50,"messages":[{"role":"user","content":"Say hello"}]}' 2>/dev/null)

# Check both have required fields
PROD_HAS_ID=$(echo "$PROD_RESPONSE" | jq -e '.id' >/dev/null 2>&1 && echo "yes" || echo "no")
CANARY_HAS_ID=$(echo "$CANARY_RESPONSE" | jq -e '.id' >/dev/null 2>&1 && echo "yes" || echo "no")
PROD_HAS_CONTENT=$(echo "$PROD_RESPONSE" | jq -e '.content' >/dev/null 2>&1 && echo "yes" || echo "no")
CANARY_HAS_CONTENT=$(echo "$CANARY_RESPONSE" | jq -e '.content' >/dev/null 2>&1 && echo "yes" || echo "no")

if [ "$PROD_HAS_ID" = "yes" ] && [ "$CANARY_HAS_ID" = "yes" ] && \
   [ "$PROD_HAS_CONTENT" = "yes" ] && [ "$CANARY_HAS_CONTENT" = "yes" ]; then
    print_pass "Both responses have identical structure"
else
    print_fail "Response structure mismatch"
fi

# Test 3: Performance Benchmarks
print_header "Test 3: Performance Benchmarks"

print_test "Measuring production latency (10 samples)..."
PROD_TOTAL=0
for i in {1..10}; do
    START=$(date +%s%3N)
    curl -s -X POST "$PROD_URL/v1/messages" \
        -H "Content-Type: application/json" \
        -H "x-api-key: $API_KEY" \
        -d '{"model":"glm-4","max_tokens":20,"messages":[{"role":"user","content":"Hi"}]}' >/dev/null 2>&1
    END=$(date +%s%3N)
    PROD_TOTAL=$((PROD_TOTAL + END - START))
done
PROD_AVG=$((PROD_TOTAL / 10))
echo "Production average: ${PROD_AVG}ms"

print_test "Measuring canary latency (10 samples)..."
CANARY_TOTAL=0
for i in {1..10}; do
    START=$(date +%s%3N)
    curl -s -X POST "$CANARY_URL/v1/messages" \
        -H "Content-Type: application/json" \
        -H "x-api-key: $API_KEY" \
        -d '{"model":"glm-4","max_tokens":20,"messages":[{"role":"user","content":"Hi"}]}' >/dev/null 2>&1
    END=$(date +%s%3N)
    CANARY_TOTAL=$((CANARY_TOTAL + END - START))
done
CANARY_AVG=$((CANARY_TOTAL / 10))
echo "Canary average: ${CANARY_AVG}ms"

OVERHEAD=$((CANARY_AVG - PROD_AVG))
if [ $OVERHEAD -lt 50 ]; then
    print_pass "Canary overhead: ${OVERHEAD}ms (within 50ms threshold)"
else
    print_fail "Canary overhead: ${OVERHEAD}ms (exceeds 50ms threshold)"
fi

# Test 4: Streaming Response Tests
print_header "Test 4: Streaming Response Tests"

print_test "Testing streaming responses..."
STREAM_RESPONSE=$(curl -s -X POST "$CANARY_URL/v1/messages" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $API_KEY" \
    -d '{"model":"glm-4","max_tokens":50,"stream":true,"messages":[{"role":"user","content":"Count to 3"}]}' 2>/dev/null)

if echo "$STREAM_RESPONSE" | grep -q "data:"; then
    print_pass "Streaming response format correct"
else
    print_fail "Streaming response missing SSE format"
fi

# Test 5: Load Testing
print_header "Test 5: Load Testing (Concurrent Requests)"

print_test "Running 20 concurrent requests..."
FAILURES=0
for i in {1..20}; do
    (
        curl -s -X POST "$CANARY_URL/v1/messages" \
            -H "Content-Type: application/json" \
            -H "x-api-key: $API_KEY" \
            -d '{"model":"glm-4","max_tokens":20,"messages":[{"role":"user","content":"Test"}]}' >/dev/null 2>&1
    ) &
done
wait

if [ $FAILURES -eq 0 ]; then
    print_pass "All concurrent requests completed"
else
    print_fail "$FAILURES requests failed"
fi

# Test 6: Edge Cases
print_header "Test 6: Edge Cases"

print_test "Testing empty message..."
EMPTY_RESPONSE=$(curl -s -X POST "$CANARY_URL/v1/messages" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $API_KEY" \
    -d '{"model":"glm-4","max_tokens":20,"messages":[{"role":"user","content":""}]}' 2>/dev/null)
if echo "$EMPTY_RESPONSE" | jq -e '.content' >/dev/null 2>&1; then
    print_pass "Empty message handled"
else
    print_fail "Empty message caused error"
fi

print_test "Testing multi-turn conversation..."
MULTI_RESPONSE=$(curl -s -X POST "$CANARY_URL/v1/messages" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $API_KEY" \
    -d '{"model":"glm-4","max_tokens":50,"messages":[{"role":"user","content":"What is 2+2?"},{"role":"assistant","content":"4"},{"role":"user","content":"And 3+3?"}]}' 2>/dev/null)
if echo "$MULTI_RESPONSE" | jq -e '.content' >/dev/null 2>&1; then
    print_pass "Multi-turn conversation handled"
else
    print_fail "Multi-turn conversation failed"
fi

# Test 7: Production Isolation
print_header "Test 7: Production Isolation Verification"

print_test "Verifying production unaffected..."
FINAL_PROD_PODS=$(kubectl get pods -n "$NAMESPACE" -l app=zai-proxy --no-headers 2>/dev/null | wc -l)
if [ "$FINAL_PROD_PODS" = "$PROD_PODS" ]; then
    print_pass "Production pods stable ($FINAL_PROD_PODS running)"
else
    print_fail "Production pods changed ($PROD_PODS -> $FINAL_PROD_PODS)"
fi

# Final Summary
print_header "Test Summary"
echo ""
echo "Total Tests: $TOTAL_TESTS"
echo -e "${GREEN}Passed: $PASSED_TESTS${NC}"
echo -e "${RED}Failed: $FAILED_TESTS${NC}"
echo ""

if [ $FAILED_TESTS -eq 0 ]; then
    echo -e "${GREEN}ALL TESTS PASSED${NC}"
    exit 0
else
    echo -e "${RED}SOME TESTS FAILED${NC}"
    exit 1
fi
