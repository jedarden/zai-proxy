# Z.AI Proxy Evaluation Framework

Tool to compare token counts from z.ai proxy with real Anthropic API responses.

## Purpose

The z.ai proxy counts tokens using tiktoken's `cl100k_base` encoding. This framework validates that the proxy's token counts match the official Anthropic API usage metadata.

## Installation

```bash
cd /home/coding/zai-proxy/proxy/evaluation

# Create virtual environment
python3 -m venv .venv
source .venv/bin/activate

# Install dependencies
pip install -r requirements.txt

# Or install as package
pip install -e .
```

## Configuration

Set up environment variables:

```bash
cp .env.example .env
# Edit .env with your API keys
```

Required variables:
- `ZAI_API_KEY` - Your z.ai API key
- `ZAI_PROXY_URL` - Proxy URL (default: http://localhost:8080)
- `ANTHROPIC_API_KEY` - Your Anthropic API key

## Usage

### List available test cases

```bash
zai-eval list-tests
```

### Run all tests

```bash
zai-eval run
```

### Run a specific test

```bash
zai-eval run short_simple
```

### Run with output reports

```bash
zai-eval run --output ./results --json --markdown
```

### Quick test with custom prompt

```bash
zai-eval quick "What is the capital of France?"
```

### Validate endpoints

```bash
zai-eval validate
```

## Test Cases

The framework includes 14 diverse test cases:

1. **short_simple** - Short simple text
2. **medium_conversation** - Medium length conversation
3. **long_context** - Long context with detailed information
4. **code_snippet** - Request involving code
5. **multi_turn_conversation** - Multiple turns of conversation
6. **structured_data** - Request with structured data format
7. **mathematical_content** - Content with mathematical expressions
8. **multilingual_text** - Text with multiple languages
9. **list_heavy_content** - Content with many list items
10. **json_only_response** - Request expecting JSON response
11. **creative_writing** - Creative writing prompt
12. **technical_explanation** - Technical concept explanation
13. **empty_system_message** - Request with system message
14. **special_characters** - Text with many special characters

## Metrics

The framework calculates:

- **Accuracy metrics**: Percentage of exact matches for input/output/total tokens
- **Mean Absolute Error (MAE)**: Average token count difference
- **Mean Percentage Error (MPE)**: Average percentage difference
- **Systematic bias**: Consistent over/under-counting patterns
- **Latency comparison**: Proxy vs Anthropic API response times

## Output

### Console Output

Rich-formatted console output with color-coded results:
- ✓ Green: Exact match
- ~ Yellow: Close (<5% difference)
- ✗ Red: Mismatch

### JSON Report

```json
{
  "summary": {
    "total_requests": 14,
    "input_token_accuracy": 85.71,
    "output_token_accuracy": 92.86,
    "overall_accuracy": 78.57
  },
  "advanced_metrics": {...},
  "bias_analysis": {...},
  "results": [...]
}
```

### Markdown Report

Human-readable report with tables and summaries.

## Architecture

```
┌─────────────┐
│   CLI       │
└──────┬──────┘
       │
       ↓
┌─────────────────────────────────────┐
│      DualClient                    │
│  ┌────────────┐  ┌──────────────┐ │
│  │ Proxy      │  │ Anthropic    │ │
│  │ Client     │  │ Client       │ │
│  └────────────┘  └──────────────┘ │
└─────────────────────────────────────┘
       │
       ↓
┌─────────────────────────────────────┐
│     EvaluationResult               │
│  • Compare token counts            │
│  • Calculate metrics               │
│  • Detect biases                   │
└─────────────────────────────────────┘
       │
       ↓
┌─────────────────────────────────────┐
│   EvaluationReport                 │
│  • Summary statistics              │
│  • Accuracy metrics                │
│  • Bias analysis                   │
└─────────────────────────────────────┘
```

## Development

### Project structure

```
evaluation/
├── zai_eval/
│   ├── __init__.py
│   ├── cli.py              # CLI interface
│   ├── client.py           # HTTP clients
│   ├── models.py           # Data models
│   ├── test_cases.py       # Test case definitions
│   ├── metrics.py          # Metrics calculation
│   └── report.py           # Report generation
├── requirements.txt
├── pyproject.toml
├── .env.example
└── README.md
```

### Adding new test cases

Edit `zai_eval/test_cases.py`:

```python
TEST_CASES.append(
    EvaluationRequest(
        name="my_test",
        description="My test description",
        model="claude-3-sonnet-20240229",
        max_tokens=100,
        messages=[...],
    )
)
```

## License

Same as parent project.
