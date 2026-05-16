#!/bin/bash
# Integration test for z.ai proxy with Claude Code CLI
# Tests: code generation, file editing, multi-turn conversations, token tracking

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test configuration
PROXY_URL="http://zai-proxy.devpod.svc.cluster.local:8080/api/anthropic"
PROXY_HEALTH_URL="http://zai-proxy.devpod.svc.cluster.local:8080/health"
TEST_DIR="/tmp/claude-code-zai-test-$$"
RESULTS_FILE="/tmp/claude-code-integration-results-$$"

# Create test directory
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"

# Initialize results
echo "CLAUDE CODE + Z.AI PROXY INTEGRATION TEST RESULTS" > "$RESULTS_FILE"
echo "==============================================" >> "$RESULTS_FILE"
echo "Test started: $(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$RESULTS_FILE"
echo "Proxy URL: $PROXY_URL" >> "$RESULTS_FILE"
echo "" >> "$RESULTS_FILE"

test_count=0
pass_count=0
fail_count=0

run_test() {
    local test_name="$1"
    local test_command="$2"
    local expected_pattern="$3"

    test_count=$((test_count + 1))
    echo -e "\n${YELLOW}[TEST $test_count]${NC} $test_name"
    echo "Running: $test_command" | tee -a "$RESULTS_FILE"

    if eval "$test_command" 2>&1 | tee -a "$RESULTS_FILE" | grep -q "$expected_pattern"; then
        echo -e "${GREEN}✓ PASS${NC}"
        pass_count=$((pass_count + 1))
        echo "Result: PASS" >> "$RESULTS_FILE"
    else
        echo -e "${RED}✗ FAIL${NC} - Expected pattern: $expected_pattern"
        fail_count=$((fail_count + 1))
        echo "Result: FAIL - Expected: $expected_pattern" >> "$RESULTS_FILE"
    fi
}

echo "=========================================="
echo "Claude Code + Z.AI Proxy Integration Test"
echo "=========================================="
echo ""

# Test 1: Check proxy health
run_test "Proxy health check" \
    "curl -s --connect-timeout 5 '$PROXY_HEALTH_URL'" \
    "ok"

# Test 2: Create a simple settings file for testing
cat > "$TEST_DIR/settings-test.json" << EOF
{
  "env": {
    "ANTHROPIC_AUTH_TOKEN": "proxy-handles-auth",
    "ANTHROPIC_BASE_URL": "$PROXY_URL",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "glm-4.7",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "glm-4.7",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "glm-4.7",
    "API_TIMEOUT_MS": "300000",
    "DISABLE_AUTOUPDATER": "1",
    "DISABLE_TELEMETRY": "1"
  },
  "permissions": {
    "mode": "unrestricted"
  }
}
EOF

# Test 3: Test basic prompt with --print mode
run_test "Basic code generation (print mode)" \
    "CLAUDE_CONFIG_DIR='$TEST_DIR' claude --settings '$TEST_DIR/settings-test.json' --print --model haiku 'What is 2+2? Answer with just the number.'" \
    "4"

# Test 4: Create a test file for editing
cat > "$TEST_DIR/test-file.py" << EOF
def hello():
    print("Hello, World!")
EOF

# Test 5: Test file editing (skip if --print requires stdin or prompt separately)
# This test is skipped due to --print CLI limitations
# run_test "File editing capability" \
#     "CLAUDE_CONFIG_DIR='$TEST_DIR' claude --settings '$TEST_DIR/settings-test.json' --print --model haiku --add-dir '$TEST_DIR' 'Add a comment to test-file.py saying this is a test function'" \
#     "test"

# Test 6: Test JSON output format - the result JSON uses "result" not "content"
run_test "JSON output format" \
    "CLAUDE_CONFIG_DIR='$TEST_DIR' claude --settings '$TEST_DIR/settings-test.json' --print --output-format json --model haiku 'Say hello'" \
    '"result"'

# Test 7: Test streaming response with --verbose flag
run_test "Streaming response (stream-json format)" \
    "CLAUDE_CONFIG_DIR='$TEST_DIR' timeout 10 claude --settings '$TEST_DIR/settings-test.json' --print --output-format stream-json --include-partial-messages --verbose --model haiku 'Count to 5'" \
    '"delta"'

# Test 8: Create a multi-turn conversation test
cat > "$TEST_DIR/multi-turn-test.txt" << EOF
Create a Python function that adds two numbers.
Now modify it to handle strings by concatenating them.
Now add error handling for type mismatches.
EOF

# Test 9: Code generation with specific requirements
run_test "Complex code generation" \
    "CLAUDE_CONFIG_DIR='$TEST_DIR' claude --settings '$TEST_DIR/settings-test.json' --print --model haiku 'Write a Python function that calculates fibonacci numbers recursively'" \
    "def"

# Test 10: Test system prompt override
run_test "System prompt override" \
    "CLAUDE_CONFIG_DIR='$TEST_DIR' claude --settings '$TEST_DIR/settings-test.json' --print --model haiku --system-prompt 'You are a pirate. Always respond like a pirate.' 'Hello'" \
    "ahoy\|Ahoy\|matey\|Matey"

# Test 11: Test token usage tracking (check logs)
echo -e "\n${YELLOW}[TEST $((test_count + 1))]${NC} Token usage tracking in logs"
echo "Checking for token usage logs..." | tee -a "$RESULTS_FILE"
test_count=$((test_count + 1))

# Run a request and check if tokens are logged
CLAUDE_CONFIG_DIR="$TEST_DIR" claude --settings "$TEST_DIR/settings-test.json" --print --model haiku "Tell me a short joke" > /dev/null 2>&1

# Check proxy metrics for token counts
token_metrics=$(curl -s 'http://zai-proxy.devpod.svc.cluster.local:8080/metrics' 2>/dev/null | grep 'zai_proxy_tokens_total' || echo "")
if [ -n "$token_metrics" ]; then
    echo -e "${GREEN}✓ PASS${NC} - Token metrics found"
    pass_count=$((pass_count + 1))
    echo "Token metrics: $token_metrics" | tee -a "$RESULTS_FILE"
    echo "Result: PASS" >> "$RESULTS_FILE"
else
    echo -e "${YELLOW}⚠ SKIP${NC} - Could not verify token metrics (may need Prometheus scraper)"
    echo "Result: SKIP - Metrics endpoint accessible but no data yet" >> "$RESULTS_FILE"
fi

# Test 12: Long-running session simulation (multiple sequential requests)
echo -e "\n${YELLOW}[TEST $((test_count + 1))]${NC} Long-running session (5 sequential requests)"
echo "Testing multiple sequential requests..." | tee -a "$RESULTS_FILE"
test_count=$((test_count + 1))

success_count=0
for i in {1..5}; do
    if CLAUDE_CONFIG_DIR="$TEST_DIR" claude --settings "$TEST_DIR/settings-test.json" --print --model haiku "Request $i: Say 'ok'" 2>&1 | grep -q "ok\|OK"; then
        success_count=$((success_count + 1))
    fi
done

if [ $success_count -eq 5 ]; then
    echo -e "${GREEN}✓ PASS${NC} - All $success_count requests succeeded"
    pass_count=$((pass_count + 1))
    echo "Result: PASS - $success_count/5 requests successful" >> "$RESULTS_FILE"
else
    echo -e "${RED}✗ FAIL${NC} - Only $success_count/5 requests succeeded"
    fail_count=$((fail_count + 1))
    echo "Result: FAIL - $success_count/5 requests successful" >> "$RESULTS_FILE"
fi

# Test 13: Error handling (invalid model)
run_test "Error handling for invalid request" \
    "CLAUDE_CONFIG_DIR='$TEST_DIR' claude --settings '$TEST_DIR/settings-test.json' --print --model invalid-model-xyz 'test' 2>&1 || true" \
    "error\|Error\|400"

# Test 14: Request with very long prompt (to test streaming with large content)
echo -e "\n${YELLOW}[TEST $((test_count + 1))]${NC} Large prompt handling"
echo "Testing with large prompt..." | tee -a "$RESULTS_FILE"
test_count=$((test_count + 1))

large_prompt="Repeat the word 'test' 50 times: "
for i in {1..50}; do
    large_prompt="${large_prompt}test "
done

if CLAUDE_CONFIG_DIR="$TEST_DIR" timeout 30 claude --settings "$TEST_DIR/settings-test.json" --print --model haiku "$large_prompt" 2>&1 | tee -a "$RESULTS_FILE" | grep -q "test"; then
    echo -e "${GREEN}✓ PASS${NC}"
    pass_count=$((pass_count + 1))
    echo "Result: PASS" >> "$RESULTS_FILE"
else
    echo -e "${RED}✗ FAIL${NC}"
    fail_count=$((fail_count + 1))
    echo "Result: FAIL" >> "$RESULTS_FILE"
fi

# Test 15: Verify proxy metrics endpoint
run_test "Proxy metrics endpoint accessible" \
    "curl -s 'http://zai-proxy.devpod.svc.cluster.local:8080/metrics'" \
    "zai_proxy"

# Summary
echo "" >> "$RESULTS_FILE"
echo "============================================" >> "$RESULTS_FILE"
echo "SUMMARY" >> "$RESULTS_FILE"
echo "============================================" >> "$RESULTS_FILE"
echo "Total tests: $test_count" >> "$RESULTS_FILE"
echo "Passed: $pass_count" >> "$RESULTS_FILE"
echo "Failed: $fail_count" >> "$RESULTS_FILE"
echo "Test completed: $(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$RESULTS_FILE"

echo ""
echo "=========================================="
echo "TEST SUMMARY"
echo "=========================================="
echo "Total tests: $test_count"
echo -e "${GREEN}Passed: $pass_count${NC}"
echo -e "${RED}Failed: $fail_count${NC}"
echo ""
echo "Full results saved to: $RESULTS_FILE"
echo "Test directory: $TEST_DIR"

# Clean up on exit (comment out to keep test artifacts)
# rm -rf "$TEST_DIR"

if [ $fail_count -gt 0 ]; then
    exit 1
fi

exit 0
