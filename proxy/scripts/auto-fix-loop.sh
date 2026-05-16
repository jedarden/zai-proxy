#!/bin/bash
# Automated Test-Fix-Iterate Loop
# Purpose: Continuous testing and automated fix iteration
# Bead: bd-3eb
#
# This script implements a closed-loop system that:
# 1. Runs test harness
# 2. Detects failures and captures error details
# 3. Categorizes failure types
# 4. Logs failures with reproduction steps
# 5. Generates fix suggestions
# 6. Tracks iteration progress
# 7. Stops when conditions are met (95% pass rate, <3% token variance)

set -e

# Script version
VERSION="1.0.0"

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
WORKSPACE_DIR="$PROJECT_ROOT"
ITERATIONS_DIR="$PROJECT_ROOT/.iterations"
LOGS_DIR="$PROJECT_ROOT/.test-logs"
REPORTS_DIR="$PROJECT_ROOT/.test-reports"

# Thresholds (from bead requirements)
TARGET_PASS_RATE=95          # 95% test pass rate
TARGET_TOKEN_VARIANCE=3      # <3% token count variance
MAX_ITERATIONS=50            # Maximum iterations to prevent infinite loops
COOLDOWN_SECONDS=5           # Wait between iterations

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
NC='\033[0m'

# State tracking (persisted)
STATE_FILE="$ITERATIONS_DIR/state.json"

# ============================================
# UTILITY FUNCTIONS
# ============================================

log_info() {
    echo -e "${BLUE}[INFO]${NC} $(date '+%Y-%m-%d %H:%M:%S') $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $(date '+%Y-%m-%d %H:%M:%S') $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $(date '+%Y-%m-%d %H:%M:%S') $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $(date '+%Y-%m-%d %H:%M:%S') $1"
}

log_debug() {
    if [[ "${DEBUG:-false}" == "true" ]]; then
        echo -e "${CYAN}[DEBUG]${NC} $(date '+%Y-%m-%d %H:%M:%S') $1"
    fi
}

print_banner() {
    local text="$1"
    echo ""
    echo -e "${CYAN}$(printf '=%.0s' {1..80})${NC}"
    echo -e "${CYAN}$text${NC}"
    echo -e "${CYAN}$(printf '=%.0s' {1..80})${NC}"
    echo ""
}

# ============================================
# INITIALIZATION
# ============================================

init_directories() {
    log_info "Initializing workspace directories..."
    mkdir -p "$ITERATIONS_DIR"
    mkdir -p "$LOGS_DIR"
    mkdir -p "$REPORTS_DIR"
    mkdir -p "$REPORTS_DIR/failures"
    mkdir -p "$REPORTS_DIR/patterns"
}

init_state() {
    if [[ ! -f "$STATE_FILE" ]]; then
        log_info "Creating new state file..."
        cat > "$STATE_FILE" << EOF
{
  "version": "$VERSION",
  "started_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "iteration": 0,
  "total_tests_run": 0,
  "total_passes": 0,
  "total_failures": 0,
  "best_pass_rate": 0.0,
  "best_token_variance": 100.0,
  "failure_history": [],
  "fix_attempts": [],
  "stop_reason": null
}
EOF
    fi
}

load_state() {
    if [[ -f "$STATE_FILE" ]]; then
        # Source the state as bash variables
        eval "$(jq -r '
            "ITERATION=\(.iteration // 0)",
            "TOTAL_TESTS_RUN=\(.total_tests_run // 0)",
            "TOTAL_PASSES=\(.total_passes // 0)",
            "TOTAL_FAILURES=\(.total_failures // 0)",
            "BEST_PASS_RATE=\(.best_pass_rate // 0.0)",
            "BEST_TOKEN_VARIANCE=\(.best_token_variance // 100.0)"
        ' "$STATE_FILE")"
    else
        ITERATION=0
        TOTAL_TESTS_RUN=0
        TOTAL_PASSES=0
        TOTAL_FAILURES=0
        BEST_PASS_RATE=0.0
        BEST_TOKEN_VARIANCE=100.0
    fi
}

save_state() {
    local iteration="$1"
    local tests_run="$2"
    local passes="$3"
    local failures="$4"
    local pass_rate="$5"
    local token_variance="$6"

    jq --arg iteration "$iteration" \
       --arg tests_run "$tests_run" \
       --arg passes "$passes" \
       --arg failures "$failures" \
       --arg pass_rate "$pass_rate" \
       --arg token_variance "$token_variance" \
       --arg started_at "$(jq -r '.started_at // now' "$STATE_FILE")" \
       --argjson failure_history "$(jq '.failure_history // []' "$STATE_FILE")" \
       --argjson fix_attempts "$(jq '.fix_attempts // []' "$STATE_FILE")" \
       --arg best_pass_rate "$(max_float "$BEST_PASS_RATE" "$pass_rate")" \
       --arg best_token_variance "$(min_float "$BEST_TOKEN_VARIANCE" "$token_variance")" \
       '{
         version: $VERSION,
         started_at: $started_at,
         iteration: ($iteration | tonumber),
         total_tests_run: ($tests_run | tonumber),
         total_passes: ($passes | tonumber),
         total_failures: ($failures | tonumber),
         best_pass_rate: ($best_pass_rate | tonumber),
         best_token_variance: ($best_token_variance | tonumber),
         failure_history: $failure_history,
         fix_attempts: $fix_attempts,
         last_updated: now
       }' <<< "{\"VERSION\":\"$VERSION\"}" > "$STATE_FILE.tmp" && mv "$STATE_FILE.tmp" "$STATE_FILE"
}

max_float() {
    echo "$1 $2" | awk '{if ($1 > $2) print $1; else print $2}'
}

min_float() {
    echo "$1 $2" | awk '{if ($1 < $2) print $1; else print $2}'
}

# ============================================
# TEST HARNESS
# ============================================

run_test_harness() {
    local iteration_num="$1"
    local log_file="$LOGS_DIR/iteration-$iteration_num.log"

    log_info "Running test harness for iteration $iteration_num..."

    cd "$PROJECT_ROOT"

    # Run all regression tests and capture output
    local test_output
    local exit_code

    test_output=$(go test -v -run TestRegression 2>&1) || exit_code=$?

    # Save raw output
    echo "$test_output" > "$log_file"

    # Parse results
    parse_test_results "$test_output" "$exit_code"
}

parse_test_results() {
    local output="$1"
    local exit_code="${2:-0}"

    local passed=0
    local failed=0
    local total=0
    local failures=()

    # Parse test output for failures
    while IFS= read -r line; do
        if [[ $line =~ ---\ (PASS|FAIL):\ ([^\ ]+) ]]; then
            ((total++))
            if [[ "${BASH_REMATCH[1]}" == "PASS" ]]; then
                ((passed++))
            else
                ((failed++))
                failures+=("${BASH_REMATCH[2]}")
            fi
        fi
    done <<< "$output"

    # Extract token counts if available
    local token_variance=100.0
    if grep -q "token" <<< "$output"; then
        # Extract token counts and calculate variance
        local token_counts
        token_counts=$(grep -oE '[0-9]+ tokens' <<< "$output" | grep -oE '[0-9]+' || true)

        if [[ -n "$token_counts" ]]; then
            local count_array=($token_counts)
            if [[ ${#count_array[@]} -gt 1 ]]; then
                # Calculate variance
                local sum=0
                local sum_sq=0
                local count=${#count_array[@]}

                for val in "${count_array[@]}"; do
                    sum=$((sum + val))
                done

                local mean=$((sum / count))
                local variance_sum=0

                for val in "${count_array[@]}"; do
                    local diff=$((val - mean))
                    variance_sum=$((variance_sum + diff * diff))
                done

                local variance=$((variance_sum / count))
                local std_dev=$((variance ** 0.5))

                # Calculate percentage variance relative to mean
                if [[ $mean -gt 0 ]]; then
                    token_variance=$((std_dev * 100 / mean))
                fi
            fi
        fi
    fi

    # Calculate pass rate
    local pass_rate=0.0
    if [[ $total -gt 0 ]]; then
        pass_rate=$(awk "BEGIN {printf \"%.2f\", ($passed / $total) * 100}")
    fi

    # Return results as JSON
    jq -n \
        --arg passed "$passed" \
        --arg failed "$failed" \
        --arg total "$total" \
        --arg pass_rate "$pass_rate" \
        --arg token_variance "$token_variance" \
        --arg exit_code "$exit_code" \
        '{
            passed: ($passed | tonumber),
            failed: ($failed | tonumber),
            total: ($total | tonumber),
            pass_rate: ($pass_rate | tonumber),
            token_variance: ($token_variance | tonumber),
            exit_code: ($exit_code | tonumber)
        }'
}

# ============================================
# FAILURE CATEGORIZATION
# ============================================

categorize_failure() {
    local test_name="$1"
    local error_message="$2"
    local test_log="$3"

    local category="unknown"
    local severity="medium"
    local suggested_fix="generic"

    # Accuracy failures - token count mismatches
    if [[ $error_message =~ (expected|Got|tokens|count) ]]; then
        category="accuracy"
        severity="high"

        # Determine specific accuracy issue
        if [[ $error_message =~ (empty|zero) ]]; then
            suggested_fix="check_tokenizer_initialization"
        elif [[ $error_message =~ (range|min|max) ]]; then
            suggested_fix="adjust_token_ranges"
        else
            suggested_fix="verify_tokenization_algorithm"
        fi
    fi

    # Format failures - JSON parsing, structure issues
    if [[ $error_message =~ (JSON|marshal|unmarshal|parse|format) ]]; then
        category="format"
        severity="medium"

        if [[ $error_message =~ (invalid|malformed) ]]; then
            suggested_fix="add_input_validation"
        else
            suggested_fix="fix_json_parsing"
        fi
    fi

    # Streaming failures - SSE, chunking issues
    if [[ $error_message =~ (stream|SSE|chunk|flush|delta) ]]; then
        category="streaming"
        severity="high"
        suggested_fix="verify_streaming_buffer_handling"
    fi

    # Concurrency failures - race conditions, locks
    if [[ $error_message =~ (race|concurrent|lock|mutex|goroutine) ]]; then
        category="concurrency"
        severity="critical"
        suggested_fix="add_synchronization_or_improve_locking"
    fi

    # Edge case failures - empty input, special characters
    if [[ $error_message =~ (empty|nil|panic|crash|special|unicode) ]]; then
        category="edge_case"
        severity="medium"
        suggested_fix="add_defensive_programming"
    fi

    # Performance failures - timeout, slow operations
    if [[ $error_message =~ (timeout|slow|deadline|exceeded) ]]; then
        category="performance"
        severity="low"
        suggested_fix="optimize_algorithm_or_add_caching"
    fi

    jq -n \
        --arg test_name "$test_name" \
        --arg category "$category" \
        --arg severity "$severity" \
        --arg suggested_fix "$suggested_fix" \
        --arg error_message "$error_message" \
        '{
            test_name: $test_name,
            category: $category,
            severity: $severity,
            suggested_fix: $suggested_fix,
            error_message: $error_message
        }'
}

# ============================================
# FAILURE LOGGING
# ============================================

log_failure_with_reproduction() {
    local iteration="$1"
    local test_name="$2"
    local category_info="$3"
    local test_log="$4"

    local failure_id="fail-${iteration}-$(date +%s)"
    local failure_file="$REPORTS_DIR/failures/${failure_id}.json"

    # Extract relevant test context
    local reproduction_steps
    reproduction_steps=$(extract_reproduction_steps "$test_name" "$test_log")

    # Create detailed failure report
    jq -n \
        --arg failure_id "$failure_id" \
        --arg iteration "$iteration" \
        --arg timestamp "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        --arg test_name "$test_name" \
        --argjson category_info "$category_info" \
        --argjson reproduction_steps "$reproduction_steps" \
        '{
            failure_id: $failure_id,
            iteration: ($iteration | tonumber),
            timestamp: $timestamp,
            test_name: $test_name,
            category: $category_info.category,
            severity: $category_info.severity,
            suggested_fix: $category_info.suggested_fix,
            error_message: $category_info.error_message,
            reproduction_steps: $reproduction_steps
        }' > "$failure_file"

    echo "$failure_file"
}

extract_reproduction_steps() {
    local test_name="$1"
    local test_log="$2"

    # Create step-by-step reproduction guide
    cat <<'EOF' | jq -R -s -c 'split("\n") | map(select(length > 0))'
1. Navigate to project directory: cd /home/coder/ardenone-cluster/containers/zai-proxy
2. Run specific test: go test -v -run TEST_NAME
3. Observe error message
4. Review code at: tokenizer.go or tokenizer_regression_test.go
5. Check token counting logic for the specific input
6. Verify tokenizer initialization
7. Test with various input formats
EOF
}

# ============================================
# FIX SUGGESTION GENERATION
# ============================================

generate_fix_suggestions() {
    local failures_json="$1"

    local suggestions=()

    # Analyze failure patterns
    local accuracy_count=0
    local format_count=0
    local streaming_count=0
    local concurrency_count=0

    while read -r failure; do
        local category
        category=$(jq -r '.category' <<< "$failure")

        case $category in
            accuracy) ((accuracy_count++)) ;;
            format) ((format_count++)) ;;
            streaming) ((streaming_count++)) ;;
            concurrency) ((concurrency_count++)) ;;
        esac
    done <<< "$(jq -c '.[]' <<< "$failures_json")"

    # Generate suggestions based on patterns
    if [[ $accuracy_count -gt 2 ]]; then
        suggestions+=("PATTERN: Multiple accuracy failures detected. SUGGESTION: Review tokenizer encoding selection (cl100k_base vs model-specific). Consider adjusting expected token ranges in golden tests.")
    fi

    if [[ $format_count -gt 2 ]]; then
        suggestions+=("PATTERN: Multiple format failures. SUGGESTION: JSON parsing may be inconsistent. Add validation middleware for request/response formats.")
    fi

    if [[ $streaming_count -gt 0 ]]; then
        suggestions+=("PATTERN: Streaming failures detected. SUGGESTION: Verify io.TeeReader buffer handling in ResponseBodyCapture. Check for race conditions in concurrent reads.")
    fi

    if [[ $concurrency_count -gt 0 ]]; then
        suggestions+=("PATTERN: Concurrency issues. SUGGESTION: Review mutex usage in TikTokenCounter. Consider adding more granular locking or using sync/atomic.")
    fi

    # Output as JSON array
    printf '%s\n' "${suggestions[@]}" | jq -R . | jq -s .
}

# ============================================
# ITERATION TRACKING
# ============================================

update_iteration_metrics() {
    local iteration="$1"
    local test_results="$2"
    local failures="$3"

    local passed
    local failed
    local total
    local pass_rate
    local token_variance

    passed=$(jq -r '.passed' <<< "$test_results")
    failed=$(jq -r '.failed' <<< "$test_results")
    total=$(jq -r '.total' <<< "$test_results")
    pass_rate=$(jq -r '.pass_rate' <<< "$test_results")
    token_variance=$(jq -r '.token_variance' <<< "$test_results")

    # Update running totals
    TOTAL_TESTS_RUN=$((TOTAL_TESTS_RUN + total))
    TOTAL_PASSES=$((TOTAL_PASSES + passed))
    TOTAL_FAILURES=$((TOTAL_FAILURES + failed))

    # Update bests
    BEST_PASS_RATE=$(max_float "$BEST_PASS_RATE" "$pass_rate")
    BEST_TOKEN_VARIANCE=$(min_float "$BEST_TOKEN_VARIANCE" "$token_variance")

    # Save state
    save_state "$iteration" "$TOTAL_TESTS_RUN" "$TOTAL_PASSES" "$TOTAL_FAILURES" "$pass_rate" "$token_variance"

    # Generate iteration report
    generate_iteration_report "$iteration" "$test_results" "$failures"
}

generate_iteration_report() {
    local iteration="$1"
    local test_results="$2"
    local failures="$3"

    local report_file="$REPORTS_DIR/iteration-$iteration.json"

    jq -n \
        --arg iteration "$iteration" \
        --arg timestamp "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        --argjson test_results "$test_results" \
        --argjson failures "$failures" \
        --arg total_tests_run "$TOTAL_TESTS_RUN" \
        --arg total_passes "$TOTAL_PASSES" \
        --arg total_failures "$TOTAL_FAILURES" \
        --arg best_pass_rate "$BEST_PASS_RATE" \
        --arg best_token_variance "$BEST_TOKEN_VARIANCE" \
        '{
            iteration: ($iteration | tonumber),
            timestamp: $timestamp,
            test_results: $test_results,
            failures: ($failures | length),
            failure_details: $failures,
            cumulative: {
                total_tests_run: ($total_tests_run | tonumber),
                total_passes: ($total_passes | tonumber),
                total_failures: ($total_failures | tonumber)
            },
            best_metrics: {
                pass_rate: ($best_pass_rate | tonumber),
                token_variance: ($best_token_variance | tonumber)
            }
        }' > "$report_file"

    log_info "Iteration report saved to $report_file"
}

# ============================================
# STOP CONDITION CHECKER
# ============================================

check_stop_conditions() {
    local test_results="$1"

    local pass_rate
    local token_variance
    local should_stop=false
    local stop_reason=""

    pass_rate=$(jq -r '.pass_rate' <<< "$test_results")
    token_variance=$(jq -r '.token_variance' <<< "$test_results")

    # Check pass rate threshold
    if (( $(echo "$pass_rate >= $TARGET_PASS_RATE" | bc -l) )); then
        should_stop=true
        stop_reason="Target pass rate achieved: ${pass_rate}% >= ${TARGET_PASS_RATE}%"
    fi

    # Check token variance threshold
    if (( $(echo "$token_variance < $TARGET_TOKEN_VARIANCE" | bc -l) )); then
        if [[ "$should_stop" == "true" ]]; then
            stop_reason="$stop_reason AND target token variance achieved: ${token_variance}% < ${TARGET_TOKEN_VARIANCE}%"
        else
            should_stop=true
            stop_reason="Target token variance achieved: ${token_variance}% < ${TARGET_TOKEN_VARIANCE}%"
        fi
    fi

    # Check for perfect score
    if (( $(echo "$pass_rate == 100.0" | bc -l) )) && \
       (( $(echo "$token_variance == 0.0" | bc -l) )); then
        should_stop=true
        stop_reason="Perfect score achieved: 100% pass rate, 0% token variance"
    fi

    jq -n \
        --arg should_stop "$should_stop" \
        --arg stop_reason "$stop_reason" \
        --arg pass_rate "$pass_rate" \
        --arg token_variance "$token_variance" \
        '{
            should_stop: ($should_stop == "true"),
            reason: $stop_reason,
            current_metrics: {
                pass_rate: ($pass_rate | tonumber),
                token_variance: ($token_variance | tonumber)
            },
            targets: {
                pass_rate: 95.0,
                token_variance: 3.0
            }
        }'
}

# ============================================
# PROGRESS DISPLAY
# ============================================

display_progress() {
    local iteration="$1"
    local test_results="$2"
    local failures="$3"

    local pass_rate
    local token_variance
    local failed

    pass_rate=$(jq -r '.pass_rate' <<< "$test_results")
    token_variance=$(jq -r '.token_variance' <<< "$test_results")
    failed=$(jq -r '.failed' <<< "$test_results")

    print_banner "Iteration $iteration Summary"

    # Metrics display
    echo -e "${CYAN}Current Metrics:${NC}"
    echo "  Pass Rate: ${pass_rate}% (target: ${TARGET_PASS_RATE}%)"
    echo "  Token Variance: ${token_variance}% (target: <${TARGET_TOKEN_VARIANCE}%)"
    echo ""

    # Best metrics
    echo -e "${CYAN}Best Metrics (all time):${NC}"
    echo "  Pass Rate: ${BEST_PASS_RATE}%"
    echo "  Token Variance: ${BEST_TOKEN_VARIANCE}%"
    echo ""

    # Failures summary
    if [[ $failed -gt 0 ]]; then
        echo -e "${RED}Failures: $failed${NC}"

        # Group by category
        local accuracy=0 format=0 streaming=0 concurrency=0 edge_case=0
        while read -r failure; do
            local category
            category=$(jq -r '.category' <<< "$failure")
            case $category in
                accuracy) ((accuracy++)) ;;
                format) ((format++)) ;;
                streaming) ((streaming++)) ;;
                concurrency) ((concurrency++)) ;;
                edge_case) ((edge_case++)) ;;
            esac
        done <<< "$(jq -c '.[]' <<< "$failures")"

        echo -e "  ${YELLOW}Breakdown by category:${NC}"
        [[ $accuracy -gt 0 ]] && echo "    Accuracy: $accuracy"
        [[ $format -gt 0 ]] && echo "    Format: $format"
        [[ $streaming -gt 0 ]] && echo "    Streaming: $streaming"
        [[ $concurrency -gt 0 ]] && echo "    Concurrency: $concurrency"
        [[ $edge_case -gt 0 ]] && echo "    Edge Case: $edge_case"
    else
        echo -e "${GREEN}No failures!${NC}"
    fi

    echo ""
}

# ============================================
# MAIN LOOP
# ============================================

main() {
    print_banner "🔄 Automated Test-Fix-Iterate Loop v$VERSION"

    # Initialize
    init_directories
    init_state
    load_state

    log_info "Starting test-fix-iterate loop..."
    log_info "Stop conditions: pass rate >= ${TARGET_PASS_RATE}%, token variance < ${TARGET_TOKEN_VARIANCE}%"
    log_info "Maximum iterations: $MAX_ITERATIONS"

    local iteration=$ITERATION
    local final_reason=""

    # Main iteration loop
    while [[ $iteration -lt $MAX_ITERATIONS ]]; do
        ((iteration++))

        print_banner "🧪 Iteration $iteration/$MAX_ITERATIONS"

        # Run test harness
        local test_results
        test_results=$(run_test_harness "$iteration")

        # Parse and categorize failures
        local failures_array=()

        local passed
        local failed
        passed=$(jq -r '.passed' <<< "$test_results")
        failed=$(jq -r '.failed' <<< "$test_results")

        if [[ $failed -gt 0 ]]; then
            log_warning "Detected $failed test failures. Analyzing..."

            # Read test log for error details
            local test_log="$LOGS_DIR/iteration-$iteration.log"

            # Categorize each failure
            while IFS= read -r line; do
                if [[ $line =~ FAIL:\ ([^\ ]+) ]]; then
                    local test_name="${BASH_REMATCH[1]}"
                    local error_msg
                    error_msg=$(grep -A 5 "$test_name" "$test_log" | head -6)

                    local category_info
                    category_info=$(categorize_failure "$test_name" "$error_msg" "$test_log")

                    # Log with reproduction steps
                    local failure_file
                    failure_file=$(log_failure_with_reproduction "$iteration" "$test_name" "$category_info" "$test_log")

                    failures_array+=("$(cat "$failure_file")")
                fi
            done < "$test_log"
        fi

        # Convert failures array to JSON
        local failures_json
        failures_json=$(printf '%s\n' "${failures_array[@]}" | jq -s .)

        # Generate fix suggestions if there are failures
        if [[ $failed -gt 0 ]]; then
            local suggestions
            suggestions=$(generate_fix_suggestions "$failures_json")

            log_info "Fix suggestions generated:"
            jq -r '.[]' <<< "$suggestions" | while IFS= read -r suggestion; do
                echo -e "  ${YELLOW}•${NC} $suggestion"
            done

            # Save suggestions
            echo "$suggestions" > "$REPORTS_DIR/patterns/iteration-$iteration-suggestions.json"
        fi

        # Update iteration metrics
        update_iteration_metrics "$iteration" "$test_results" "$failures_json"

        # Display progress
        display_progress "$iteration" "$test_results" "$failures_json"

        # Check stop conditions
        local stop_check
        stop_check=$(check_stop_conditions "$test_results")

        local should_stop
        should_stop=$(jq -r '.should_stop' <<< "$stop_check")

        if [[ "$should_stop" == "true" ]]; then
            final_reason=$(jq -r '.reason' <<< "$stop_check")
            log_success "$final_reason"
            break
        fi

        # Cooldown before next iteration
        if [[ $iteration -lt $MAX_ITERATIONS ]]; then
            log_info "Waiting ${COOLDOWN_SECONDS}s before next iteration..."
            sleep $COOLDOWN_SECONDS
        fi
    done

    # Final report
    print_final_report "$iteration" "$final_reason"
}

print_final_report() {
    local final_iteration="$1"
    local stop_reason="$2"

    local final_report="$REPORTS_DIR/final-report.json"

    jq -n \
        --arg version "$VERSION" \
        --arg final_iteration "$final_iteration" \
        --arg stop_reason "$stop_reason" \
        --arg total_tests_run "$TOTAL_TESTS_RUN" \
        --arg total_passes "$TOTAL_PASSES" \
        --arg total_failures "$TOTAL_FAILURES" \
        --arg best_pass_rate "$BEST_PASS_RATE" \
        --arg best_token_variance "$BEST_TOKEN_VARIANCE" \
        --arg completed_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        '{
            version: $version,
            final_iteration: ($final_iteration | tonumber),
            stop_reason: $stop_reason,
            completed_at: $completed_at,
            summary: {
                total_tests_run: ($total_tests_run | tonumber),
                total_passes: ($total_passes | tonumber),
                total_failures: ($total_failures | tonumber),
                best_pass_rate: ($best_pass_rate | tonumber),
                best_token_variance: ($best_token_variance | tonumber)
            }
        }' > "$final_report"

    print_banner "📊 Final Report"

    echo -e "${GREEN}Test-Fix-Iterate Loop Completed${NC}"
    echo ""
    echo "Total iterations: $final_iteration"
    echo "Stop reason: $stop_reason"
    echo ""
    echo "Summary:"
    echo "  Total tests run: $TOTAL_TESTS_RUN"
    echo "  Total passes: $TOTAL_PASSES"
    echo "  Total failures: $TOTAL_FAILURES"
    echo "  Best pass rate: ${BEST_PASS_RATE}%"
    echo "  Best token variance: ${BEST_TOKEN_VARIANCE}%"
    echo ""
    echo "Reports saved to: $REPORTS_DIR"
    echo "Final report: $final_report"
    echo ""

    # Check if targets were met
    if (( $(echo "$BEST_PASS_RATE >= $TARGET_PASS_RATE" | bc -l) )); then
        echo -e "${GREEN}✅ PASS RATE TARGET ACHIEVED${NC}"
    else
        echo -e "${YELLOW}⚠️  Pass rate target not met: ${BEST_PASS_RATE}% < ${TARGET_PASS_RATE}%${NC}"
    fi

    if (( $(echo "$BEST_TOKEN_VARIANCE < $TARGET_TOKEN_VARIANCE" | bc -l) )); then
        echo -e "${GREEN}✅ TOKEN VARIANCE TARGET ACHIEVED${NC}"
    else
        echo -e "${YELLOW}⚠️  Token variance target not met: ${BEST_TOKEN_VARIANCE}% >= ${TARGET_TOKEN_VARIANCE}%${NC}"
    fi

    echo ""
}

# ============================================
# SCRIPT ENTRY POINT
# ============================================

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --debug)
            DEBUG=true
            shift
            ;;
        --max-iterations)
            MAX_ITERATIONS="$2"
            shift 2
            ;;
        --target-pass-rate)
            TARGET_PASS_RATE="$2"
            shift 2
            ;;
        --target-variance)
            TARGET_TOKEN_VARIANCE="$2"
            shift 2
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Automated Test-Fix-Iterate Loop for continuous testing"
            echo ""
            echo "Options:"
            echo "  --debug                Enable debug output"
            echo "  --max-iterations N      Maximum iterations (default: 50)"
            echo "  --target-pass-rate N    Target pass rate % (default: 95)"
            echo "  --target-variance N     Target token variance % (default: 3)"
            echo "  -h, --help             Show this help"
            echo ""
            echo "Stop Conditions:"
            echo "  - Pass rate >= 95%"
            echo "  - Token variance < 3%"
            echo "  - Maximum iterations reached"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use -h or --help for usage information"
            exit 1
            ;;
    esac
done

# Run main loop
main
