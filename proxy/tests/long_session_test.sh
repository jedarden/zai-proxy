#!/bin/bash
# Test long-running session with multiple turns
# This simulates a 50+ turn conversation with Claude Code

set -e

PROXY_URL="${PROXY_URL:-http://zai-proxy.devpod.svc.cluster.local:8080}"
SETTINGS="${HOME}/.claude/settings.json"
OUTPUT_DIR="/tmp/claude-long-session-test"
OUTPUT_FILE="${OUTPUT_DIR}/results.jsonl"

mkdir -p "${OUTPUT_DIR}"
echo "Starting long session test at $(date)" | tee "${OUTPUT_DIR}/test.log"

# Test metrics
total_requests=0
successful_requests=0
failed_requests=0
total_duration_ms=0

# Simple prompts for testing
prompts=(
    "What is 1+1?"
    "What is 2+2?"
    "What is 3+3?"
    "What is 4+4?"
    "What is 5+5?"
    "What is 6+6?"
    "What is 7+7?"
    "What is 8+8?"
    "What is 9+9?"
    "What is 10+10?"
)

# Run 50 turns
echo "" > "${OUTPUT_FILE}"

for i in {1..50}; do
    prompt_index=$((($i - 1) % ${#prompts[@]}))
    prompt="${prompts[$prompt_index]}"

    echo "Turn $i: $prompt" | tee -a "${OUTPUT_DIR}/test.log"

    start_time=$(date +%s%3N)

    response=$(timeout 120 claude --settings "${SETTINGS}" -p "$prompt" --output-format json 2>&1)

    end_time=$(date +%s%3N)
    duration=$((end_time - start_time))
    total_duration_ms=$((total_duration_ms + duration))

    # Check if successful
    if echo "$response" | jq -e '.type == "result"' > /dev/null 2>&1; then
        successful_requests=$((successful_requests + 1))
        result=$(echo "$response" | jq -r '.result')
        echo "  ✓ Success (${duration}ms): $result" | tee -a "${OUTPUT_DIR}/test.log"
    else
        failed_requests=$((failed_requests + 1))
        echo "  ✗ Failed (${duration}ms): $response" | tee -a "${OUTPUT_DIR}/test.log"
    fi

    total_requests=$((total_requests + 1))

    # Add small delay between requests
    sleep 0.5
done

# Print summary
echo "" | tee -a "${OUTPUT_DIR}/test.log"
echo "=== Test Summary ===" | tee -a "${OUTPUT_DIR}/test.log"
echo "Total requests: $total_requests" | tee -a "${OUTPUT_DIR}/test.log"
echo "Successful: $successful_requests" | tee -a "${OUTPUT_DIR}/test.log"
echo "Failed: $failed_requests" | tee -a "${OUTPUT_DIR}/test.log"
echo "Total duration: ${total_duration_ms}ms" | tee -a "${OUTPUT_DIR}/test.log"
echo "Average duration per request: $((total_duration_ms / total_requests))ms" | tee -a "${OUTPUT_DIR}/test.log"
echo "Success rate: $(echo "scale=2; $successful_requests * 100 / $total_requests" | bc)%" | tee -a "${OUTPUT_DIR}/test.log"

# Check proxy metrics after test
echo "" | tee -a "${OUTPUT_DIR}/test.log"
echo "=== Proxy Metrics After Test ===" | tee -a "${OUTPUT_DIR}/test.log"
curl -s "${PROXY_URL}/metrics" | grep -E "zai_proxy_requests_total{method=\"POST\",path=\"/api/anthropic/v1/messages\"" | tee -a "${OUTPUT_DIR}/test.log"

echo "Test completed at $(date)" | tee -a "${OUTPUT_DIR}/test.log"

exit 0
