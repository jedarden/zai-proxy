#!/bin/bash
set -euo pipefail

# Benchmark script for zai-proxy token counting overhead
# Measures performance with and without token counting enabled

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_DIR"

echo "======================================"
echo "zai-proxy Token Counting Benchmarks"
echo "======================================"
echo "Project dir: $PROJECT_DIR"
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo -e "${RED}Error: Go is not installed${NC}"
    exit 1
fi

echo "Go version: $(go version)"
echo ""

# Function to run benchmarks and parse results
run_benchmarks() {
    local bench_pattern=$1
    local description=$2

    echo "--------------------------------------"
    echo "$description"
    echo "--------------------------------------"

    go test -bench="$bench_pattern" -benchmem -benchtime=1s ./... 2>&1 | grep -E "^(Benchmark|PASS|ok|FAIL)"
    echo ""
}

# Function to check if benchmark meets latency target
check_latency_target() {
    local output=$1
    local target_ms=$2
    local test_name=$3

    # Extract ns/op from benchmark output
    ns_per_op=$(echo "$output" | grep "$test_name" | awk '{print $3}' | sed 's/ns\/op//')

    if [[ -n "$ns_per_op" ]]; then
        ms_per_op=$(echo "scale=3; $ns_per_op / 1000000" | bc)
        echo "  Latency: $ms_per_op ms (target: ${target_ms}ms)"

        if (( $(echo "$ms_per_op > $target_ms" | bc -l) )); then
            echo -e "  ${RED}FAIL: Exceeds target${NC}"
            return 1
        else
            echo -e "  ${GREEN}PASS: Meets target${NC}"
            return 0
        fi
    fi
}

# Main benchmark execution
main() {
    # Run all benchmarks
    echo "Running Go benchmarks..."
    echo ""

    # 1. Basic token counting benchmarks
    run_benchmarks "BenchmarkTikTokenCounter" "TikToken Performance by Input Size"
    run_benchmarks "BenchmarkSimpleTokenCounter" "SimpleTokenCounter Performance (Baseline)"
    run_benchmarks "BenchmarkCountRequestTokens" "Request Token Counting"

    # 2. Concurrent benchmarks
    run_benchmarks "BenchmarkConcurrentTokenCounting" "Concurrent Token Counting (10/50/100)"
    run_benchmarks "BenchmarkTikTokenCounterParallel" "Parallel Token Counting"

    # 3. Streaming and SSE benchmarks
    run_benchmarks "BenchmarkCountSSE" "SSE Token Counting"

    # 4. Memory benchmarks
    run_benchmarks "BenchmarkTikTokenCounterMemory" "Memory Allocation"

    # 5. Full request benchmarks
    run_benchmarks "BenchmarkFullRequest" "Full Request with Token Counting"

    echo "======================================"
    echo "Benchmark Summary"
    echo "======================================"
    echo ""
    echo "Running detailed benchmarks with timing..."

    # Run benchmarks with more detailed output
    echo ""
    echo "1. Input Size Benchmarks"
    echo "------------------------"
    for size in Small Medium Large XLarge; do
        echo -n "  $size: "
        go test -bench="^BenchmarkTikTokenCounter${size}$" -benchtime=100x ./... 2>&1 | grep "$size" | tail -1 | awk '{print $3 " " $4}'
    done

    echo ""
    echo "2. Latency Targets (must be <5ms)"
    echo "----------------------------------"
    for size in Small Medium Large; do
        echo -n "  $size: "
        result=$(go test -bench="^BenchmarkTikTokenCounter${size}$" -benchtime=100x ./... 2>&1 | grep "$size" | tail -1)
        ns_per_op=$(echo "$result" | awk '{print $3}')
        if [[ -n "$ns_per_op" ]]; then
            ms_per_op=$(echo "scale=2; ${ns_per_op/ns\/op/} / 1000000" | bc)
            echo "$ms_per_op ms"
            if (( $(echo "$ms_per_op >= 5" | bc -l) )); then
                echo -e "    ${RED}WARNING: Exceeds 5ms target${NC}"
            fi
        fi
    done

    echo ""
    echo "3. Concurrent Load Performance"
    echo "------------------------------"
    for concurrency in 10 50 100; do
        echo -n "  $concurrency concurrent: "
        result=$(go test -bench="^BenchmarkConcurrentTokenCounting${concurrency}$" -benchtime=10x ./... 2>&1 | grep "$concurrency" | tail -1)
        if [[ -n "$result" ]]; then
            echo "$result" | awk '{print $3 " " $4}'
        fi
    done

    echo ""
    echo "4. Memory per Operation"
    echo "-----------------------"
    for size in Small Medium Large; do
        echo -n "  $size: "
        result=$(go test -bench="^BenchmarkTikTokenCounter${size}$" -benchmem -benchtime=100x ./... 2>&1 | grep "$size" | tail -1)
        if [[ -n "$result" ]]; then
            echo "$result" | awk '{print $5 " " $6}'
        fi
    done

    echo ""
    echo "======================================"
    echo "Benchmark Complete"
    echo "======================================"
    echo ""
    echo "For detailed CPU profiling, run:"
    echo "  go test -cpuprofile=cpu.prof -bench=. ./..."
    echo "  go tool pprof cpu.prof"
    echo ""
    echo "For memory profiling, run:"
    echo "  go test -memprofile=mem.prof -bench=. ./..."
    echo "  go tool pprof mem.prof"
}

# Run main function
main "$@"
