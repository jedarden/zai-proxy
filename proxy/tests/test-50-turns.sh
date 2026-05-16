#!/bin/bash
# Long-running session test: 50+ turns with Claude Code via z.ai proxy

set -e

PROXY_URL="http://zai-proxy.devpod.svc.cluster.local:8080/api/anthropic"
TEST_DIR="/tmp/claude-code-50turns-$$"
RESULTS_FILE="/tmp/claude-code-50turns-results-$$"

mkdir -p "$TEST_DIR"
cd "$TEST_DIR"

# Create settings file
cat > "$TEST_DIR/settings-test.json" << EOF
{
  "env": {
    "ANTHROPIC_AUTH_TOKEN": "proxy-handles-auth",
    "ANTHROPIC_BASE_URL": "$PROXY_URL",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "glm-4.7",
    "API_TIMEOUT_MS": "300000",
    "DISABLE_AUTOUPDATER": "1",
    "DISABLE_TELEMETRY": "1"
  },
  "permissions": {
    "mode": "unrestricted"
  }
}
EOF

echo "=========================================="
echo "50+ Turn Long-Running Session Test"
echo "=========================================="
echo "Test started: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo ""

# Track metrics
total_turns=50
success_count=0
fail_count=0
retry_count=0
total_tokens=0

# Get initial metrics
initial_requests=$(curl -s 'http://zai-proxy.devpod.svc.cluster.local:8080/metrics' 2>/dev/null | grep 'zai_proxy_requests_total{.*path="/api/anthropic/v1/messages"' | grep -oP '[0-9]+$' | head -1 || echo "0")

start_time=$(date +%s)

echo "Running $total_turns sequential requests..."
echo ""

for i in $(seq 1 $total_turns); do
    turn_start=$(date +%s)

    # Vary prompts to test different scenarios
    case $((i % 10)) in
        0) prompt="What is $i + $i? Answer with just the number." ;;
        1) prompt="Turn $i: Say 'hello world'" ;;
        2) prompt="Count to 3" ;;
        3) prompt="What is 2 * $i? Answer with just the number." ;;
        4) prompt="Say 'ok' in all lowercase" ;;
        5) prompt="What is $i modulo 3? Answer with just the number." ;;
        6) prompt="Repeat the word 'test' 3 times" ;;
        7) prompt="What's the opposite of hot? One word answer." ;;
        8) prompt="Say 'yes'" ;;
        9) prompt="What comes after letter $((i % 26 + 1)) in alphabet? One word." ;;
    esac

    # Execute request with retry on failure
    max_retries=2
    retry=0
    success=false

    while [ $retry -le $max_retries ] && [ "$success" = false ]; do
        if CLAUDE_CONFIG_DIR="$TEST_DIR" timeout 30 claude --settings "$TEST_DIR/settings-test.json" --print --model haiku "$prompt" > "$TEST_DIR/turn-$i.log" 2>&1; then
            # Check if response is not empty
            if [ -s "$TEST_DIR/turn-$i.log" ] && grep -qiE "[a-z0-9]" "$TEST_DIR/turn-$i.log" 2>/dev/null; then
                success=true
                success_count=$((success_count + 1))
                echo -n "✓"
            else
                retry=$((retry + 1))
                retry_count=$((retry_count + 1))
                echo -n "r"
                sleep 1
            fi
        else
            retry=$((retry + 1))
            retry_count=$((retry_count + 1))
            echo -n "x"
            sleep 1
        fi
    done

    if [ "$success" = false ]; then
        fail_count=$((fail_count + 1))
        echo -n "✗"
    fi

    turn_end=$(date +%s)
    turn_duration=$((turn_end - turn_start))

    # Progress indicator every 10 turns
    if [ $((i % 10)) -eq 0 ]; then
        echo " [$i/$total_turns in ${turn_duration}s]"
    fi

    # Small delay to avoid overwhelming the proxy
    sleep 0.2
done

end_time=$(date +%s)
total_duration=$((end_time - start_time))

# Get final metrics
final_requests=$(curl -s 'http://zai-proxy.devpod.svc.cluster.local:8080/metrics' 2>/dev/null | grep 'zai_proxy_requests_total{.*path="/api/anthropic/v1/messages"' | grep -oP '[0-9]+$' | head -1 || echo "0")
requests_processed=$((final_requests - initial_requests))

echo ""
echo ""
echo "=========================================="
echo "RESULTS"
echo "=========================================="
echo "Total turns attempted: $total_turns"
echo "Successful turns: $success_count"
echo "Failed turns: $fail_count"
echo "Retries: $retry_count"
echo "Success rate: $(echo "scale=2; $success_count * 100 / $total_turns" | bc)%"
echo ""
echo "Total duration: ${total_duration}s"
echo "Average per turn: $(echo "scale=2; $total_duration / $total_turns" | bc)s"
echo "Requests processed by proxy: $requests_processed"
echo ""

# Calculate success rate percentage
success_rate=$(echo "scale=2; $success_count * 100 / $total_turns" | bc)

# Write results to file
cat > "$RESULTS_FILE" << EOF
Claude Code + Z.AI Proxy 50+ Turn Session Test Results
=======================================================

Test started: $(date -u -d @$start_time +%Y-%m-%dT%H:%M:%SZ)
Test completed: $(date -u +%Y-%m-%dT%H:%M:%SZ)

Configuration:
- Proxy URL: $PROXY_URL
- Model: glm-4.7 (haiku alias)
- Total turns: $total_turns

Results:
- Successful turns: $success_count
- Failed turns: $fail_count
- Retries: $retry_count
- Success rate: ${success_rate}%
- Total duration: ${total_duration}s
- Average per turn: $(echo "scale=2; $total_duration / $total_turns" | bc)s
- Proxy requests processed: $requests_processed

Test output and logs preserved in: $TEST_DIR
EOF

cat "$RESULTS_FILE"

# Overall result
if [ $success_count -ge 45 ]; then  # 90% success rate
    echo ""
    echo "✓ TEST PASSED - 90%+ success rate"
    exit 0
elif [ $success_count -ge 35 ]; then  # 70% success rate
    echo ""
    echo "⚠ TEST PARTIAL - 70-90% success rate"
    exit 0
else
    echo ""
    echo "✗ TEST FAILED - Below 70% success rate"
    exit 1
fi
