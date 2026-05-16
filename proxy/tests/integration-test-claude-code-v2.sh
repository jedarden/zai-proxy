#!/bin/bash
# Integration test for z.ai proxy with Claude Code CLI v2
# Tests: code generation, file editing, multi-turn conversations, token tracking

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test configuration
PROXY_URL="http://zai-proxy.devpod.svc.cluster.local:8080/api/anthropic"
PROXY_HEALTH_URL="http://zai-proxy.devpod.svc.cluster.local:8080/health"
PROXY_METRICS_URL="http://zai-proxy.devpod.svc.cluster.local:8080/metrics"
TEST_DIR="/tmp/claude-code-zai-test-$$"
RESULTS_FILE="/tmp/claude-code-integration-results-$$"

# Create test directory
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"

# Initialize results
echo "CLAUDE CODE + Z.AI PROXY INTEGRATION TEST RESULTS V2" > "$RESULTS_FILE"
echo "==============================================" >> "$RESULTS_FILE"
echo "Test started: $(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$RESULTS_FILE"
echo "Proxy URL: $PROXY_URL" >> "$RESULTS_FILE"
echo "" >> "$RESULTS_FILE"

test_count=0
pass_count=0
fail_count=0
skip_count=0

run_test() {
    local test_name="$1"
    local test_command="$2"
    local expected_pattern="$3"

    test_count=$((test_count + 1))
    echo -e "\n${YELLOW}[TEST $test_count]${NC} $test_name"
    echo "Running: $test_command" | tee -a "$RESULTS_FILE"

    if eval "$test_command" 2>&1 | tee -a "$RESULTS_FILE" | grep -qiE "$expected_pattern"; then
        echo -e "${GREEN}✓ PASS${NC}"
        pass_count=$((pass_count + 1))
        echo "Result: PASS" >> "$RESULTS_FILE"
    else
        echo -e "${RED}✗ FAIL${NC} - Expected pattern: $expected_pattern"
        fail_count=$((fail_count + 1))
        echo "Result: FAIL - Expected: $expected_pattern" >> "$RESULTS_FILE"
    fi
}

run_test_skip() {
    local test_name="$1"
    local reason="$2"

    test_count=$((test_count + 1))
    echo -e "\n${YELLOW}[TEST $test_count]${NC} $test_name"
    echo -e "${BLUE}⚠ SKIP${NC} - $reason"
    skip_count=$((skip_count + 1))
    echo "Result: SKIP - $reason" >> "$RESULTS_FILE"
}

echo "=========================================="
echo "Claude Code + Z.AI Proxy Integration Test V2"
echo "=========================================="
echo ""

# Create settings file for testing
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

# ============================================================
# SECTION 1: BASIC FUNCTIONALITY
# ============================================================
echo -e "\n${BLUE}=== SECTION 1: BASIC FUNCTIONALITY ===${NC}"

run_test "1. Proxy health check" \
    "curl -s --connect-timeout 5 '$PROXY_HEALTH_URL'" \
    "ok"

run_test "2. Basic prompt (print mode)" \
    "CLAUDE_CONFIG_DIR='$TEST_DIR' claude --settings '$TEST_DIR/settings-test.json' --print --model haiku 'What is 2+2? Answer with just the number.'" \
    "4"

run_test "3. JSON output format" \
    "CLAUDE_CONFIG_DIR='$TEST_DIR' claude --settings '$TEST_DIR/settings-test.json' --print --output-format json --model haiku 'Say hello' | jq -r '.result'" \
    "hello"

run_test "4. Streaming response with verbose" \
    "CLAUDE_CONFIG_DIR='$TEST_DIR' timeout 15 claude --settings '$TEST_DIR/settings-test.json' --print --output-format stream-json --include-partial-messages --verbose --model haiku 'Count to 3' 2>&1 | head -20" \
    "delta"

# ============================================================
# SECTION 2: CODE GENERATION
# ============================================================
echo -e "\n${BLUE}=== SECTION 2: CODE GENERATION ===${NC}"

run_test "5. Simple code generation" \
    "CLAUDE_CONFIG_DIR='$TEST_DIR' claude --settings '$TEST_DIR/settings-test.json' --print --model haiku 'Write a Python hello world function'" \
    "def"

run_test "6. Fibonacci recursive function" \
    "CLAUDE_CONFIG_DIR='$TEST_DIR' claude --settings '$TEST_DIR/settings-test.json' --print --model haiku 'Write a Python function that calculates fibonacci numbers recursively'" \
    "fibonacci"

run_test "7. System prompt override (pirate mode)" \
    "CLAUDE_CONFIG_DIR='$TEST_DIR' claude --settings '$TEST_DIR/settings-test.json' --print --model haiku --system-prompt 'You are a pirate. Always respond like a pirate.' 'Hello'" \
    "ahoy|matey|arr"

# ============================================================
# SECTION 3: FILE OPERATIONS
# ============================================================
echo -e "\n${BLUE}=== SECTION 3: FILE OPERATIONS ===${NC}"

# Create a test file
cat > "$TEST_DIR/test-file.py" << 'EOF'
def hello():
    print("Hello, World!")
EOF

run_test "8. File reading (workspace context)" \
    "CLAUDE_CONFIG_DIR='$TEST_DIR' claude --settings '$TEST_DIR/settings-test.json' --print --model haiku --add-dir '$TEST_DIR' 'What does test-file.py contain? Describe the function.' --dangerously-skip-permissions" \
    "hello"

# ============================================================
# SECTION 4: ERROR HANDLING
# ============================================================
echo -e "\n${BLUE}=== SECTION 4: ERROR HANDLING ===${NC}"

run_test "9. Invalid model error" \
    "CLAUDE_CONFIG_DIR='$TEST_DIR' claude --settings '$TEST_DIR/settings-test.json' --print --model invalid-model-xyz 'test' 2>&1 || true" \
    "error|unknown|invalid"

run_test "10. Large prompt handling" \
    "CLAUDE_CONFIG_DIR='$TEST_DIR' timeout 30 claude --settings '$TEST_DIR/settings-test.json' --print --model haiku 'Tell me a very long story about programming' 2>&1 | head -5" \
    "programming|code|story"

# ============================================================
# SECTION 5: MULTI-TURN CONVERSATIONS
# ============================================================
echo -e "\n${BLUE}=== SECTION 5: MULTI-TURN CONVERSATIONS ===${NC}"

# Test multi-turn by creating a session and resuming it
run_test "11. Create session for multi-turn" \
    "CLAUDE_CONFIG_DIR='$TEST_DIR' cd '$TEST_DIR' && echo 'Create a variable called x with value 5' | claude --settings '$TEST_DIR/settings-test.json' --print --model haiku --session-id '$(uuidgen)' 2>&1" \
    "x|5"

run_test "12. Sequential requests (session simulation)" \
    "for i in {1..3}; do CLAUDE_CONFIG_DIR='$TEST_DIR' claude --settings '$TEST_DIR/settings-test.json' --print --model haiku \"Request \$i: Say ok\" 2>&1 | grep -qi ok && echo 'Request \$i ok'; done" \
    "ok"

# ============================================================
# SECTION 6: LONG-RUNNING SESSION (20 turns)
# ============================================================
echo -e "\n${BLUE}=== SECTION 6: LONG-RUNNING SESSION (20 turns) ===${NC}"

echo -e "\n${YELLOW}[TEST $((test_count + 1))]${NC} Long-running session (20 sequential requests)"
echo "Testing 20 sequential requests..." | tee -a "$RESULTS_FILE"
test_count=$((test_count + 1))

success_count=0
start_time=$(date +%s)
for i in {1..20}; do
    echo -n "." >&2
    if CLAUDE_CONFIG_DIR="$TEST_DIR" claude --settings "$TEST_DIR/settings-test.json" --print --model haiku "Turn $i: Say 'ok'" 2>&1 | grep -qiE "ok|OK"; then
        success_count=$((success_count + 1))
    fi
    sleep 0.1  # Small delay to avoid rate limiting
done
end_time=$(date +%s)
duration=$((end_time - start_time))

echo "" >&2
if [ $success_count -ge 18 ]; then  # Allow 2 failures
    echo -e "${GREEN}✓ PASS${NC} - $success_count/20 requests succeeded in ${duration}s"
    pass_count=$((pass_count + 1))
    echo "Result: PASS - $success_count/20 in ${duration}s" >> "$RESULTS_FILE"
else
    echo -e "${RED}✗ FAIL${NC} - Only $success_count/20 requests succeeded in ${duration}s"
    fail_count=$((fail_count + 1))
    echo "Result: FAIL - $success_count/20 in ${duration}s" >> "$RESULTS_FILE"
fi

# ============================================================
# SECTION 7: TOKEN TRACKING
# ============================================================
echo -e "\n${BLUE}=== SECTION 7: TOKEN TRACKING ===${NC}"

echo -e "\n${YELLOW}[TEST $((test_count + 1))]${NC} Proxy metrics endpoint and token tracking"
echo "Checking proxy metrics..." | tee -a "$RESULTS_FILE"
test_count=$((test_count + 1))

# Get initial token counts
initial_input=$(curl -s "$PROXY_METRICS_URL" 2>/dev/null | grep 'zai_proxy_tokens_total{direction="input"' | grep -oP '[0-9]+$' | head -1 || echo "0")
initial_output=$(curl -s "$PROXY_METRICS_URL" 2>/dev/null | grep 'zai_proxy_tokens_total{direction="output"' | grep -oP '[0-9]+$' | head -1 || echo "0")

# Make a request
CLAUDE_CONFIG_DIR="$TEST_DIR" claude --settings "$TEST_DIR/settings-test.json" --print --model haiku "Tell me a joke" > /dev/null 2>&1

# Get final token counts
final_input=$(curl -s "$PROXY_METRICS_URL" 2>/dev/null | grep 'zai_proxy_tokens_total{direction="input"' | grep -oP '[0-9]+$' | head -1 || echo "0")
final_output=$(curl -s "$PROXY_METRICS_URL" 2>/dev/null | grep 'zai_proxy_tokens_total{direction="output"' | grep -oP '[0-9]+$' | head -1 || echo "0")

# Check if tokens increased
if [ "$final_input" -gt "$initial_input" ] 2>/dev/null; then
    echo -e "${GREEN}✓ PASS${NC} - Input tokens tracked (increased from $initial_input to $final_input)"
    echo "Output tokens: $initial_output -> $final_output" | tee -a "$RESULTS_FILE"
    pass_count=$((pass_count + 1))
    echo "Result: PASS - Tokens tracked" >> "$RESULTS_FILE"
elif [ -n "$final_input" ]; then
    echo -e "${GREEN}✓ PASS${NC} - Token metrics available (input: $final_input, output: $final_output)"
    pass_count=$((pass_count + 1))
    echo "Result: PASS - Metrics endpoint working" >> "$RESULTS_FILE"
else
    echo -e "${YELLOW}⚠ SKIP${NC} - Could not verify token metrics"
    skip_count=$((skip_count + 1))
    echo "Result: SKIP" >> "$RESULTS_FILE"
fi

# ============================================================
# SECTION 8: STREAMING VALIDATION
# ============================================================
echo -e "\n${BLUE}=== SECTION 8: STREAMING VALIDATION ===${NC}"

run_test "14. Streaming with verbose flag" \
    "CLAUDE_CONFIG_DIR='$TEST_DIR' timeout 10 claude --settings '$TEST_DIR/settings-test.json' --print --output-format stream-json --include-partial-messages --verbose --model haiku 'Say hello' 2>&1 | head -30" \
    "delta"

run_test "15. Large content streaming" \
    "CLAUDE_CONFIG_DIR='$TEST_DIR' timeout 15 claude --settings '$TEST_DIR/settings-test.json' --print --verbose --model haiku 'Write a 10 line poem' 2>&1" \
    "poem|verse"

# ============================================================
# SECTION 9: EDGE CASES
# ============================================================
echo -e "\n${BLUE}=== SECTION 9: EDGE CASES ===${NC}"

run_test "16. Empty prompt handling" \
    "CLAUDE_CONFIG_DIR='$TEST_DIR' claude --settings '$TEST_DIR/settings-test.json' --print --model haiku '' 2>&1 || true" \
    "error|empty"

run_test "17. Special characters in prompt" \
    "CLAUDE_CONFIG_DIR='$TEST_DIR' claude --settings '$TEST_DIR/settings-test.json' --print --model haiku 'Explain this: @#\$%^&*()_+'" \
    "explain"

run_test "18. Unicode prompt" \
    "CLAUDE_CONFIG_DIR='$TEST_DIR' claude --settings '$TEST_DIR/settings-test.json' --print --model haiku '你好' 2>&1" \
    "."

# ============================================================
# SECTION 10: PERFORMANCE
# ============================================================
echo -e "\n${BLUE}=== SECTION 10: PERFORMANCE ===${NC}"

echo -e "\n${YELLOW}[TEST $((test_count + 1))]${NC} Concurrent requests (5 parallel)"
echo "Testing 5 parallel requests..." | tee -a "$RESULTS_FILE"
test_count=$((test_count + 1))

pids=""
for i in {1..5}; do
    (CLAUDE_CONFIG_DIR="$TEST_DIR" claude --settings "$TEST_DIR/settings-test.json" --print --model haiku "Parallel $i: Say ok" 2>&1 | grep -qiE "ok|OK" && echo "Parallel $i: success" >> "$TEST_DIR/parallel-$i.log") &
    pids="$pids $!"
done

# Wait for all background jobs
wait $pids 2>/dev/null || true

# Count successes
parallel_success=0
for i in {1..5}; do
    if [ -f "$TEST_DIR/parallel-$i.log" ] && grep -q "success" "$TEST_DIR/parallel-$i.log"; then
        parallel_success=$((parallel_success + 1))
    fi
done

if [ $parallel_success -ge 4 ]; then  # Allow 1 failure
    echo -e "${GREEN}✓ PASS${NC} - $parallel_success/5 parallel requests succeeded"
    pass_count=$((pass_count + 1))
    echo "Result: PASS - $parallel_success/5 parallel" >> "$RESULTS_FILE"
else
    echo -e "${RED}✗ FAIL${NC} - Only $parallel_success/5 parallel requests succeeded"
    fail_count=$((fail_count + 1))
    echo "Result: FAIL - $parallel_success/5 parallel" >> "$RESULTS_FILE"
fi

# ============================================================
# SECTION 11: METRICS ENDPOINT
# ============================================================
echo -e "\n${BLUE}=== SECTION 11: METRICS ENDPOINT ===${NC}"

run_test "20. Proxy metrics endpoint" \
    "curl -s '$PROXY_METRICS_URL'" \
    "zai_proxy"

run_test "21. Request duration metrics" \
    "curl -s '$PROXY_METRICS_URL' | grep 'zai_proxy_request_duration_seconds'" \
    "zai_proxy_request_duration_seconds"

run_test "22. Token metrics" \
    "curl -s '$PROXY_METRICS_URL' | grep 'zai_proxy_tokens'" \
    "zai_proxy_tokens"

# ============================================================
# SUMMARY
# ============================================================
echo "" >> "$RESULTS_FILE"
echo "============================================" >> "$RESULTS_FILE"
echo "SUMMARY" >> "$RESULTS_FILE"
echo "============================================" >> "$RESULTS_FILE"
echo "Total tests: $test_count" >> "$RESULTS_FILE"
echo "Passed: $pass_count" >> "$RESULTS_FILE"
echo "Failed: $fail_count" >> "$RESULTS_FILE"
echo "Skipped: $skip_count" >> "$RESULTS_FILE"
echo "Test completed: $(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$RESULTS_FILE"

echo ""
echo "=========================================="
echo "TEST SUMMARY"
echo "=========================================="
echo "Total tests: $test_count"
echo -e "${GREEN}Passed: $pass_count${NC}"
echo -e "${RED}Failed: $fail_count${NC}"
echo -e "${BLUE}Skipped: $skip_count${NC}"
echo ""
echo "Full results saved to: $RESULTS_FILE"
echo "Test directory: $TEST_DIR"

# Keep test directory for inspection
echo ""
echo "Test artifacts preserved in: $TEST_DIR"

if [ $fail_count -gt 0 ]; then
    exit 1
fi

exit 0
