# Z.AI Proxy Evaluation Framework - Example Usage

This document provides examples and usage patterns for the evaluation framework.

## Quick Start

### 1. Set up environment

```bash
cd /home/coding/zai-proxy/proxy/evaluation

# Create and activate virtual environment
python3 -m venv .venv
source .venv/bin/activate

# Install dependencies
pip install -r requirements.txt
pip install -e .

# Set up environment variables
export ZAI_API_KEY="your-zai-api-key"
export ANTHROPIC_API_KEY="your-anthropic-api-key"
export ZAI_PROXY_URL="http://zai-proxy.devpod.svc.cluster.local:8080"
```

### 2. Run all tests

```bash
zai-eval run
```

### 3. Run specific test

```bash
zai-eval run short_simple
```

### 4. Run with output reports

```bash
zai-eval run --output ./results --json --markdown
```

## Test Results Interpretation

### Console Output

```
╭──────────────────────────────────────────╮
│     Z.AI PROXY EVALUATION REPORT        │
╜──────────────────────────────────────────╯

Summary
────────────────────────────────────
Total Requests: 14
Successful: 14
Failed: 0

Token Count Accuracy
┏━━━━━━━━━━━━━━━━━━━━┳━━━━━━━━━━━━━━━┓
┃ Metric             ┃ Accuracy (%) ┃
┡━━━━━━━━━━━━━━━━━━━━╇━━━━━━━━━━━━━━━┩
│ Input Token Accuracy│ 85.71%       │
│ Output Token Accuracy│ 92.86%      │
│ Overall Accuracy   │ 78.57%       │
└────────────────────┴───────────────┘

Systematic Bias Analysis
┏━━━━━━━━━━━━━━━━━━━━┳━━━━━━━━━━━━━━━┓
┃ Metric             ┃ Value        ┃
┡━━━━━━━━━━━━━━━━━━━━╇━━━━━━━━━━━━━━━┩
│ Input Bias         │ +2.3 tokens  │
│ Output Bias        │ +1.1 tokens  │
└────────────────────┴───────────────┘
```

### Interpreting Bias

- **Positive bias (+)**: Proxy overcounts (more tokens than Anthropic)
- **Negative bias (-)**: Proxy undercounts (fewer tokens than Anthropic)
- **Near zero**: Accurate counting

## Test Cases Reference

| Test Name | Description | Expected Behavior |
|-----------|-------------|-------------------|
| short_simple | Short simple text | Should match exactly |
| medium_conversation | Medium conversation | Should match exactly |
| long_context | Long detailed text | May have small variance |
| code_snippet | Code content | Special characters may affect count |
| multilingual_text | Multiple languages | Different tokenization per language |
| special_characters | Many symbols | May differ due to encoding |

## Common Issues

### Issue: Proxy returns no token counts

**Symptom**: `input_tokens=None` in results

**Solution**: Check proxy is running with token counting enabled:
```bash
kubectl logs deployment/zai-proxy -n devpod | grep "Token counting"
```

Expected output:
```
Token counting enabled (tiktoken cl100k_base encoding, model: glm-4)
```

### Issue: Anthropic API returns 401

**Symptom**: `anthropic_response.error` contains "401"

**Solution**: Verify `ANTHROPIC_API_KEY` is set correctly:
```bash
echo $ANTHROPIC_API_KEY | cut -c1-10
```

### Issue: Connection refused

**Symptom**: `Connection refused` for proxy

**Solution**: Verify proxy URL:
```bash
# From within cluster
export ZAI_PROXY_URL="http://zai-proxy.devpod.svc.cluster.local:8080"

# From local machine
export ZAI_PROXY_URL="http://localhost:8080"
```

## Advanced Usage

### Custom test case

Create a Python script:

```python
from zai_eval.client import DualClient
from zai_eval.models import EvaluationResult
from zai_eval.metrics import calculate_metrics
import os

client = DualClient(
    proxy_url=os.getenv("ZAI_PROXY_URL"),
    proxy_api_key=os.getenv("ZAI_API_KEY"),
    anthropic_api_key=os.getenv("ANTHROPIC_API_KEY"),
)

proxy_resp, anthropic_resp = client.evaluate_request(
    model="claude-3-sonnet-20240229",
    messages=[{"role": "user", "content": "Your custom prompt"}],
    max_tokens=100,
)

result = EvaluationResult(
    request_name="custom_test",
    proxy_response=proxy_resp,
    anthropic_response=anthropic_resp,
)
result.calculate_metrics()

print(f"Input tokens: Proxy={proxy_resp.input_tokens}, Anthropic={anthropic_resp.input_tokens}")
print(f"Difference: {result.input_diff} ({result.input_pct_diff:.1f}%)")
```

### Batch testing

```bash
# Run specific tests only
zai-eval run short_simple medium_conversation long_context

# With verbose output
zai-eval run --verbose

# Save to custom location
zai-eval run --output ~/evaluation-results --json --markdown
```

## Metrics Reference

### Accuracy Metrics

- **Input Token Accuracy**: Percentage of exact input token matches
- **Output Token Accuracy**: Percentage of exact output token matches
- **Overall Accuracy**: Percentage where both input AND output match

### Error Metrics

- **MAE (Mean Absolute Error)**: Average token difference
- **MPE (Mean Percentage Error)**: Average percentage difference

### Latency Metrics

- **Proxy Latency**: Time for proxy request (ms)
- **Anthropic Latency**: Time for Anthropic request (ms)
- **Overhead**: Additional latency from proxy

### Bias Analysis

- **Input Bias Mean**: Average over/under-count for input tokens
- **Output Bias Mean**: Average over/under-count for output tokens
- **Consistently High/Low**: Number of tests with consistent bias direction

## Integration with CI/CD

```yaml
# .github/workflows/evaluation.yml
name: Token Accuracy Evaluation

on:
  schedule:
    - cron: '0 0 * * *'  # Daily
  workflow_dispatch:

jobs:
  evaluate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Set up Python
        uses: actions/setup-python@v4
        with:
          python-version: '3.11'
      - name: Install dependencies
        run: |
          cd evaluation
          pip install -r requirements.txt
          pip install -e .
      - name: Run evaluation
        env:
          ZAI_API_KEY: ${{ secrets.ZAI_API_KEY }}
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
          ZAI_PROXY_URL: ${{ secrets.ZAI_PROXY_URL }}
        run: |
          cd evaluation
          zai-eval run --output results --json --markdown
      - name: Upload results
        uses: actions/upload-artifact@v3
        with:
          name: evaluation-results
          path: evaluation/results/
```
