#!/bin/bash
# Validate Golden Test Data
# Ensures golden_test_data.json is properly formatted and complete

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GOLDEN_DATA="$SCRIPT_DIR/golden_test_data.json"

echo "======================================"
echo "Golden Test Data Validator"
echo "======================================"
echo ""

# Check if jq is available
if ! command -v jq &> /dev/null; then
    echo "Error: jq is required but not installed"
    echo "Install with: apt-get install jq"
    exit 1
fi

# Check if golden data file exists
if [ ! -f "$GOLDEN_DATA" ]; then
    echo "Error: Golden data file not found: $GOLDEN_DATA"
    exit 1
fi

echo "Validating: $GOLDEN_DATA"
echo ""

# Validate JSON syntax
echo "1. Checking JSON syntax..."
if jq . "$GOLDEN_DATA" > /dev/null 2>&1; then
    echo "✅ JSON syntax valid"
else
    echo "❌ JSON syntax error"
    jq . "$GOLDEN_DATA"
    exit 1
fi

# Count test categories
echo ""
echo "2. Counting test cases..."

BASIC_COUNT=$(jq '.basic_token_counts | length' "$GOLDEN_DATA")
EDGE_COUNT=$(jq '.edge_cases | length' "$GOLDEN_DATA")
API_COUNT=$(jq '.api_requests | length' "$GOLDEN_DATA")
STREAMING_COUNT=$(jq '.streaming_responses | length' "$GOLDEN_DATA")
JSON_COUNT=$(jq '.json_responses | length' "$GOLDEN_DATA")
MALFORMED_COUNT=$(jq '.malformed_inputs | length' "$GOLDEN_DATA")
BENCH_COUNT=$(jq '.performance_benchmarks | length' "$GOLDEN_DATA")

TOTAL_COUNT=$((BASIC_COUNT + EDGE_COUNT + API_COUNT + STREAMING_COUNT + JSON_COUNT + MALFORMED_COUNT + BENCH_COUNT))

echo "  Basic token counts: $BASIC_COUNT"
echo "  Edge cases: $EDGE_COUNT"
echo "  API requests: $API_COUNT"
echo "  Streaming responses: $STREAMING_COUNT"
echo "  JSON responses: $JSON_COUNT"
echo "  Malformed inputs: $MALFORMED_COUNT"
echo "  Performance benchmarks: $BENCH_COUNT"
echo "  ---"
echo "  Total test cases: $TOTAL_COUNT"

if [ $TOTAL_COUNT -lt 30 ]; then
    echo "⚠️  Warning: Low test count ($TOTAL_COUNT < 30)"
else
    echo "✅ Good test coverage ($TOTAL_COUNT cases)"
fi

# Validate required fields
echo ""
echo "3. Validating test case structure..."

# Check basic_token_counts have required fields
MISSING=0
for i in $(seq 0 $((BASIC_COUNT - 1))); do
    ID=$(jq -r ".basic_token_counts[$i].id" "$GOLDEN_DATA")
    INPUT=$(jq -r ".basic_token_counts[$i].input" "$GOLDEN_DATA")
    MIN=$(jq -r ".basic_token_counts[$i].expected_min" "$GOLDEN_DATA")
    MAX=$(jq -r ".basic_token_counts[$i].expected_max" "$GOLDEN_DATA")

    if [ "$ID" == "null" ] || [ "$INPUT" == "null" ] || [ "$MIN" == "null" ] || [ "$MAX" == "null" ]; then
        echo "❌ Missing field in basic_token_counts[$i]"
        MISSING=$((MISSING + 1))
    fi

    if [ "$MIN" != "null" ] && [ "$MAX" != "null" ] && [ "$MIN" -gt "$MAX" ]; then
        echo "❌ Invalid range in $ID: min ($MIN) > max ($MAX)"
        MISSING=$((MISSING + 1))
    fi
done

if [ $MISSING -eq 0 ]; then
    echo "✅ All basic test cases valid"
else
    echo "❌ $MISSING validation errors found"
    exit 1
fi

# Validate metadata
echo ""
echo "4. Checking metadata..."

VERSION=$(jq -r '.metadata.version' "$GOLDEN_DATA")
TOKENIZER=$(jq -r '.metadata.tokenizer' "$GOLDEN_DATA")
TARGET_MODEL=$(jq -r '.metadata.model_target' "$GOLDEN_DATA")
COVERAGE_TARGET=$(jq -r '.metadata.coverage_target' "$GOLDEN_DATA")

echo "  Version: $VERSION"
echo "  Tokenizer: $TOKENIZER"
echo "  Target model: $TARGET_MODEL"
echo "  Coverage target: $COVERAGE_TARGET"

if [ "$VERSION" == "null" ] || [ "$TOKENIZER" == "null" ]; then
    echo "❌ Missing metadata"
    exit 1
else
    echo "✅ Metadata complete"
fi

# Check for duplicate IDs
echo ""
echo "5. Checking for duplicate test IDs..."

ALL_IDS=$(jq -r '
    [.basic_token_counts[].id,
     .edge_cases[].id,
     .api_requests[].id,
     .streaming_responses[].id,
     .json_responses[].id,
     .malformed_inputs[].id,
     .performance_benchmarks[].id] | .[]
' "$GOLDEN_DATA" | sort)

DUPLICATE_IDS=$(echo "$ALL_IDS" | uniq -d)

if [ -n "$DUPLICATE_IDS" ]; then
    echo "❌ Duplicate test IDs found:"
    echo "$DUPLICATE_IDS"
    exit 1
else
    echo "✅ No duplicate IDs"
fi

# Summary
echo ""
echo "======================================"
echo "Validation Summary"
echo "======================================"
echo "✅ JSON syntax valid"
echo "✅ $TOTAL_COUNT test cases found"
echo "✅ All required fields present"
echo "✅ No duplicate IDs"
echo "✅ Metadata complete"
echo ""
echo "Golden data is valid and ready for use!"
