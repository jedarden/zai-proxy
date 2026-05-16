#!/bin/bash
set -euo pipefail

# HTTP Load Test for zai-proxy
# Tests token counting overhead under concurrent load

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_DIR"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
PROXY_URL="${PROXY_URL:-http://localhost:8080}"
API_KEY="${ZAI_API_KEY:-test-key}"

# Test data sizes
SMALL_PROMPT='{"model":"glm-4","messages":[{"role":"user","content":"What is the capital of France?"}],"stream":false}'
MEDIUM_PROMPT='{"model":"glm-4","messages":[{"role":"user","content":"Explain the history of the Roman Empire in detail, including its founding, major expansion periods, key emperors, political structure, military campaigns, economic system, social hierarchy, cultural achievements, architectural innovations, legal developments, religious evolution, and eventual decline."}],"stream":false}'
LARGE_PROMPT='{"model":"glm-4","messages":[{"role":"user","content":"Provide a comprehensive analysis of artificial intelligence covering: 1) Historical development from Turing test to modern deep learning, 2) Machine learning fundamentals including supervised, unsupervised, and reinforcement learning, 3) Neural network architectures from perceptrons to transformers, 4) Natural language processing breakthroughs, 5) Computer vision applications, 6) Ethical considerations and bias mitigation, 7) Future research directions including AGI, 8) Industry applications across healthcare, finance, transportation, and creative fields, 9) Technical challenges in scaling, interpretability, and safety, 10) Societal impacts on employment, privacy, and human-computer interaction."}],"stream":false}'

# Function to print colored output
print_color() {
    local color=$1
    local text=$2
    echo -e "${color}${text}${NC}"
}

# Function to check if proxy is running
check_proxy() {
    if ! curl -s -f "$PROXY_URL/health" > /dev/null 2>&1; then
        print_color "$RED" "Error: Proxy is not running at $PROXY_URL"
        print_color "$YELLOW" "Start the proxy with: go run ."
        exit 1
    fi
}

# Function to make a single request
make_request() {
    local prompt=$1
    local request_id=$2

    local start_time=$(date +%s.%N)
    local response=$(curl -s -w "\n%{http_code}\n%{time_total}" \
        -X POST \
        "$PROXY_URL/v1/messages" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $API_KEY" \
        -d "$prompt" \
        2>&1) || echo "500\n0"
    local end_time=$(date +%s.%N)

    # Parse response
    local body=$(echo "$response" | sed '$d' | sed '$d')
    local http_code=$(echo "$response" | tail -n 2 | head -n 1)
    local total_time=$(echo "$response" | tail -n 1)

    echo "$request_id|$http_code|$total_time|$start_time|$end_time"
}

# Function to run concurrent load test
run_load_test() {
    local concurrency=$1
    local total_requests=$2
    local prompt=$3
    local test_name=$4

    print_color "$BLUE" "Running: $test_name"
    echo "  Concurrency: $concurrency"
    echo "  Total requests: $total_requests"
    echo ""

    local requests_per_batch=$((total_requests / concurrency))
    local remaining=$((total_requests % concurrency))

    local pids=()
    local temp_files=()
    local start_time=$(date +%s.%N)

    # Launch concurrent workers
    for ((i=0; i<concurrency; i++)); do
        local batch_size=$requests_per_batch
        if ((i < remaining)); then
            batch_size=$((batch_size + 1))
        fi

        local temp_file=$(mktemp)
        temp_files+=("$temp_file")

        (
            for ((j=0; j<batch_size; j++)); do
                make_request "$prompt" "$i-$j"
                # Small delay to avoid overwhelming
                sleep 0.01
            done
        ) > "$temp_file" &

        pids+=($!)
    done

    # Wait for all workers
    for pid in "${pids[@]}"; do
        wait $pid 2>/dev/null || true
    done

    local end_time=$(date +%s.%N)

    # Collect results
    local total_requests_completed=0
    local successful_requests=0
    local failed_requests=0
    local total_time=0
    local min_time=999999
    local max_time=0

    for temp_file in "${temp_files[@]}"; do
        while IFS='|' read -r request_id http_code total_time_req start end; do
            ((total_requests_completed++))

            if [[ "$http_code" == "200" ]]; then
                ((successful_requests++))
                total_time=$(echo "$total_time + $total_time_req" | bc)
                if (( $(echo "$total_time_req < $min_time" | bc -l) )); then
                    min_time=$total_time_req
                fi
                if (( $(echo "$total_time_req > $max_time" | bc -l) )); then
                    max_time=$total_time_req
                fi
            else
                ((failed_requests++))
            fi
        done < "$temp_file"
        rm -f "$temp_file"
    done

    # Calculate statistics
    local total_test_time=$(echo "$end_time - $start_time" | bc)
    local avg_time=0
    if ((successful_requests > 0)); then
        avg_time=$(echo "scale=3; $total_time / $successful_requests" | bc)
    fi

    # Print results
    print_color "$GREEN" "Results: $test_name"
    echo "  Total requests: $total_requests_completed"
    echo "  Successful: $successful_requests"
    echo "  Failed: $failed_requests"
    echo "  Total time: $(echo "scale=2; $total_test_time" | bc) seconds"
    echo "  Requests/sec: $(echo "scale=2; $total_requests_completed / $total_test_time" | bc)"
    echo "  Avg response time: ${avg_time}s"
    echo "  Min response time: ${min_time}s"
    echo "  Max response time: ${max_time}s"
    echo ""

    # Check latency target
    local avg_ms=$(echo "$avg_time * 1000" | bc)
    if (( $(echo "$avg_time > 5" | bc -l) )); then
        print_color "$YELLOW" "  WARNING: Avg response time exceeds 5s (consider increasing timeout)"
    fi
}

# Function to compare with/without token counting
compare_token_counting() {
    print_color "$BLUE" "======================================"
    print_color "$BLUE" "Token Counting Overhead Comparison"
    print_color "$BLUE" "======================================"
    echo ""

    print_color "$YELLOW" "This test requires running the proxy with and without token counting."
    print_color "$YELLOW" "Run the following in separate terminals:"
    echo ""
    echo "  Terminal 1 (with counting):"
    echo "    TOKEN_COUNTING_ENABLED=true go run ."
    echo ""
    echo "  Terminal 2 (without counting):"
    echo "    TOKEN_COUNTING_ENABLED=false go run ."
    echo ""
    print_color "$YELLOW" "Then run this script for each configuration:"
    echo "  PROXY_URL=http://localhost:8080 ./scripts/load-test-proxy.sh"
    echo ""
}

# Main function
main() {
    print_color "$BLUE" "======================================"
    print_color "$BLUE" "zai-proxy HTTP Load Test"
    print_color "$BLUE" "======================================"
    echo ""
    echo "Proxy URL: $PROXY_URL"
    echo ""

    # Check if proxy is running
    check_proxy

    # Get proxy info
    print_color "$GREEN" "Proxy is running"
    echo ""

    # Check token counting status from metrics
    local metrics=$(curl -s "$PROXY_URL/metrics" 2>/dev/null || echo "")
    if echo "$metrics" | grep -q "zai_proxy_tokens_total"; then
        print_color "$GREEN" "Token counting: ENABLED"
    else
        print_color "$YELLOW" "Token counting: DISABLED"
    fi
    echo ""

    # Run load tests with different concurrency levels
    print_color "$BLUE" "======================================"
    print_color "$BLUE" "Load Test Scenarios"
    print_color "$BLUE" "======================================"
    echo ""

    # Small prompt, low concurrency
    run_load_test 10 50 "$SMALL_PROMPT" "Small prompt, 10 concurrent"

    # Small prompt, medium concurrency
    run_load_test 50 100 "$SMALL_PROMPT" "Small prompt, 50 concurrent"

    # Small prompt, high concurrency
    run_load_test 100 200 "$SMALL_PROMPT" "Small prompt, 100 concurrent"

    echo ""

    # Medium prompt, medium concurrency
    run_load_test 50 100 "$MEDIUM_PROMPT" "Medium prompt, 50 concurrent"

    echo ""

    # Large prompt, low concurrency
    run_load_test 10 20 "$LARGE_PROMPT" "Large prompt, 10 concurrent"

    echo ""
    print_color "$GREEN" "======================================"
    print_color "$GREEN" "Load Test Complete"
    print_color "$GREEN" "======================================"
    echo ""
    echo "To compare with/without token counting, restart proxy with:"
    echo "  TOKEN_COUNTING_ENABLED=false"
    echo ""
}

# Check if we should show comparison instructions
if [[ "${1:-}" == "--compare" ]]; then
    compare_token_counting
    exit 0
fi

# Run main tests
main "$@"
