#!/usr/bin/env python3
"""Z.AI Proxy Evaluation Framework - CLI Entry Point

Compares token counts from z.ai proxy with Anthropic API responses.
"""

import argparse
import json
import os
import sys
from datetime import datetime
from pathlib import Path

# Add evaluation package to path
sys.path.insert(0, str(Path(__file__).parent))

from zai_eval.client import DualClient
from zai_eval.test_cases import get_test_cases
from zai_eval.models import EvaluationResult, EvaluationReport


def parse_args():
    parser = argparse.ArgumentParser(
        description="Evaluate z.ai proxy token counting against Anthropic API"
    )
    parser.add_argument(
        "--proxy-url",
        default=os.getenv("ZAI_PROXY_URL", "http://localhost:8080"),
        help="Z.AI proxy URL (default: from ZAI_PROXY_URL or http://localhost:8080)"
    )
    parser.add_argument(
        "--proxy-key",
        default=os.getenv("ZAI_API_KEY"),
        help="Z.AI API key (default: from ZAI_API_KEY)"
    )
    parser.add_argument(
        "--anthropic-key",
        default=os.getenv("ANTHROPIC_API_KEY"),
        help="Anthropic API key (default: from ANTHROPIC_API_KEY)"
    )
    parser.add_argument(
        "--output-dir",
        default="evaluation/results",
        help="Output directory for reports (default: evaluation/results)"
    )
    parser.add_argument(
        "--test-name",
        help="Run only a specific test case by name"
    )
    parser.add_argument(
        "--verbose", "-v",
        action="store_true",
        help="Enable verbose output"
    )
    return parser.parse_args()


def run_evaluation(args):
    """Run the evaluation suite."""
    # Validate required parameters
    if not args.proxy_key:
        print("Error: Z.AI API key required. Set ZAI_API_KEY or use --proxy-key")
        sys.exit(1)
    if not args.anthropic_key:
        print("Error: Anthropic API key required. Set ANTHROPIC_API_KEY or use --anthropic-key")
        sys.exit(1)

    print("=" * 70)
    print("Z.AI Proxy Evaluation Framework")
    print("=" * 70)
    print(f"Proxy URL:        {args.proxy_url}")
    print(f"Anthropic API:    https://api.anthropic.com")
    print(f"Output Directory: {args.output_dir}")
    print()

    # Create output directory
    output_dir = Path(args.output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)

    # Get test cases
    all_tests = get_test_cases()
    if args.test_name:
        tests = [t for t in all_tests if t.name == args.test_name]
        if not tests:
            print(f"Error: Test case '{args.test_name}' not found")
            print(f"Available tests: {', '.join(t.name for t in all_tests)}")
            sys.exit(1)
    else:
        tests = all_tests

    print(f"Running {len(tests)} test case(s)...")
    print()

    # Create dual client
    client = DualClient(args.proxy_url, args.proxy_key, args.anthropic_key)

    # Run evaluation
    results = []
    for i, test in enumerate(tests, 1):
        print(f"[{i}/{len(tests)}] {test.name}: {test.description}")
        if args.verbose:
            print(f"  Model: {test.model}")
            print(f"  Max tokens: {test.max_tokens}")
            print(f"  Stream: {test.stream}")

        # Execute parallel requests
        proxy_resp, anthropic_resp = client.evaluate_request(
            model=test.model,
            messages=test.messages,
            max_tokens=test.max_tokens,
            stream=test.stream,
            temperature=test.temperature,
        )

        # Create result
        result = EvaluationResult(
            request_name=test.name,
            proxy_response=proxy_resp,
            anthropic_response=anthropic_resp,
            input_match=False,
            output_match=False,
            total_match=False,
        )
        result.calculate_metrics()

        results.append(result)

        # Show result
        status = "✓" if (proxy_resp.status_code == 200 and anthropic_resp.status_code == 200) else "✗"
        print(f"  Status: {status}")
        print(f"  Proxy:      {proxy_resp.status_code} | "
              f"In: {proxy_resp.input_tokens or 'N/A':>4} | "
              f"Out: {proxy_resp.output_tokens or 'N/A':>4} | "
              f"Latency: {proxy_resp.latency_ms:.0f}ms")
        print(f"  Anthropic:  {anthropic_resp.status_code} | "
              f"In: {anthropic_resp.input_tokens or 'N/A':>4} | "
              f"Out: {anthropic_resp.output_tokens or 'N/A':>4} | "
              f"Latency: {anthropic_resp.latency_ms:.0f}ms")

        if proxy_resp.status_code == 200 and anthropic_resp.status_code == 200:
            match_indicator = "✓" if result.input_match else "✗"
            print(f"  Input match:  {match_indicator} (diff: {result.input_diff}, {result.input_pct_diff:.1f}%)")
            match_indicator = "✓" if result.output_match else "✗"
            print(f"  Output match: {match_indicator} (diff: {result.output_diff}, {result.output_pct_diff:.1f}%)")
        elif proxy_resp.error:
            print(f"  Proxy error: {proxy_resp.error}")
        elif anthropic_resp.error:
            print(f"  Anthropic error: {anthropic_resp.error}")
        print()

    # Generate report
    print("Generating report...")
    report = EvaluationReport(
        total_requests=len(tests),
        successful_requests=0,
        failed_requests=0,
        results=results,
    )
    report.calculate_summary_metrics()

    # Print summary
    print()
    print("=" * 70)
    print("EVALUATION SUMMARY")
    print("=" * 70)
    print(f"Total tests:       {report.total_requests}")
    print(f"Successful:        {report.successful_requests}")
    print(f"Failed:            {report.failed_requests}")
    print()
    print("Token Accuracy:")
    print(f"  Input tokens:     {report.input_token_accuracy:.1f}%")
    print(f"  Output tokens:    {report.output_token_accuracy:.1f}%")
    print(f"  Overall:          {report.overall_accuracy:.1f}%")
    print()
    print("Mean Absolute Error:")
    print(f"  Input tokens:     {report.input_mae:.2f}")
    print(f"  Output tokens:    {report.output_mae:.2f}")
    print(f"  Total tokens:     {report.total_mae:.2f}")
    print()
    print("Systematic Bias:")
    print(f"  Input bias:       {report.input_bias_mean:+.2f} (positive = proxy overcounts)")
    print(f"  Output bias:      {report.output_bias_mean:+.2f} (positive = proxy overcounts)")
    print()
    print("Latency:")
    print(f"  Avg proxy:        {report.avg_proxy_latency_ms:.0f}ms")
    print(f"  Avg Anthropic:    {report.avg_anthropic_latency_ms:.0f}ms")
    print()

    # Save reports
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")

    # JSON report
    json_file = output_dir / f"evaluation_report_{timestamp}.json"
    with open(json_file, "w") as f:
        json.dump(report.dict(), f, indent=2, default=str)
    print(f"✓ JSON report saved: {json_file}")

    # Text report
    text_file = output_dir / f"evaluation_report_{timestamp}.txt"
    with open(text_file, "w") as f:
        f.write(generate_text_report(report))
    print(f"✓ Text report saved: {text_file}")

    # Analysis
    print()
    print("=" * 70)
    print("ANALYSIS")
    print("=" * 70)
    print(generate_analysis(report))

    return report


def generate_text_report(report: EvaluationReport) -> str:
    """Generate a detailed text report."""
    lines = [
        "Z.AI Proxy Evaluation Report",
        "=" * 70,
        f"Generated: {report.timestamp.isoformat()}",
        "",
        "EXECUTIVE SUMMARY",
        "-" * 70,
        f"Total Tests:       {report.total_requests}",
        f"Successful:        {report.successful_requests}",
        f"Failed:            {report.failed_requests}",
        "",
        "TOKEN ACCURACY METRICS",
        "-" * 70,
        f"Input Token Accuracy:  {report.input_token_accuracy:.1f}%",
        f"Output Token Accuracy: {report.output_token_accuracy:.1f}%",
        f"Overall Accuracy:      {report.overall_accuracy:.1f}%",
        "",
        "MEAN ABSOLUTE ERROR",
        "-" * 70,
        f"Input MAE:       {report.input_mae:.2f} tokens",
        f"Output MAE:      {report.output_mae:.2f} tokens",
        f"Total MAE:       {report.total_mae:.2f} tokens",
        "",
        "SYSTEMATIC BIAS",
        "-" * 70,
        f"Input Bias:      {report.input_bias_mean:+.2f} (positive = proxy overcounts)",
        f"Output Bias:     {report.output_bias_mean:+.2f} (positive = proxy overcounts)",
        "",
        "LATENCY",
        "-" * 70,
        f"Avg Proxy Latency:     {report.avg_proxy_latency_ms:.0f}ms",
        f"Avg Anthropic Latency: {report.avg_anthropic_latency_ms:.0f}ms",
        "",
        "DETAILED RESULTS",
        "-" * 70,
    ]

    for result in report.results:
        lines.extend([
            "",
            f"Test: {result.request_name}",
            f"  Proxy:      Status={result.proxy_response.status_code} | "
            f"In={result.proxy_response.input_tokens or 'N/A':>4} | "
            f"Out={result.proxy_response.output_tokens or 'N/A':>4}",
            f"  Anthropic:  Status={result.anthropic_response.status_code} | "
            f"In={result.anthropic_response.input_tokens or 'N/A':>4} | "
            f"Out={result.anthropic_response.output_tokens or 'N/A':>4}",
            f"  Match:      Input={result.input_match} | Output={result.output_match}",
            f"  Diff:       Input={result.input_diff} ({result.input_pct_diff:.1f}%) | "
            f"Output={result.output_diff} ({result.output_pct_diff:.1f}%)",
        ])

    lines.extend(["", "", "ANALYSIS", "-" * 70])
    lines.append(generate_analysis(report))

    return "\n".join(lines)


def generate_analysis(report: EvaluationReport) -> str:
    """Generate analysis and recommendations."""
    lines = []

    # Token accuracy assessment
    if report.input_token_accuracy >= 95:
        lines.append("✓ Input token counting is excellent (≥95% accuracy)")
    elif report.input_token_accuracy >= 80:
        lines.append("⚠ Input token counting needs attention (80-95% accuracy)")
    else:
        lines.append("✗ Input token counting has significant issues (<80% accuracy)")

    if report.output_token_accuracy >= 95:
        lines.append("✓ Output token counting is excellent (≥95% accuracy)")
    elif report.output_token_accuracy >= 80:
        lines.append("⚠ Output token counting needs attention (80-95% accuracy)")
    else:
        lines.append("✗ Output token counting has significant issues (<80% accuracy)")

    # MAE assessment
    if report.input_mae > 10:
        lines.append(f"⚠ High input token MAE ({report.input_mae:.2f}) - review tokenizer configuration")
    if report.output_mae > 10:
        lines.append(f"⚠ High output token MAE ({report.output_mae:.2f}) - check SSE parsing logic")

    # Bias analysis
    if abs(report.input_bias_mean) > 5:
        direction = "overcounts" if report.input_bias_mean > 0 else "undercounts"
        lines.append(f"⚠ Proxy consistently {direction} input tokens by {abs(report.input_bias_mean):.2f} on average")
    if abs(report.output_bias_mean) > 5:
        direction = "overcounts" if report.output_bias_mean > 0 else "undercounts"
        lines.append(f"⚠ Proxy consistently {direction} output tokens by {abs(report.output_bias_mean):.2f} on average")

    # Pattern analysis
    input_matches = sum(1 for r in report.results if r.input_match)
    output_matches = sum(1 for r in report.results if r.output_match)

    if input_matches == 0:
        lines.append("⚠ No exact input token matches found - systematic difference detected")
    if output_matches == 0:
        lines.append("⚠ No exact output token matches found - systematic difference detected")

    return "\n".join(lines)


if __name__ == "__main__":
    args = parse_args()
    report = run_evaluation(args)

    # Exit with error if any tests failed
    if report.failed_requests > 0:
        sys.exit(1)
