"""Metrics calculation for evaluation framework."""

import numpy as np
from scipy import stats
from typing import List
from zai_eval.models import EvaluationResult, EvaluationReport


def calculate_metrics(results: List[EvaluationResult]) -> EvaluationReport:
    """Calculate comprehensive metrics from evaluation results.

    Args:
        results: List of evaluation results

    Returns:
        EvaluationReport with calculated metrics
    """
    report = EvaluationReport(
        total_requests=len(results),
        results=results,
    )
    report.calculate_summary_metrics()
    return report


def calculate_advanced_metrics(results: List[EvaluationResult]) -> dict:
    """Calculate advanced statistical metrics.

    Args:
        results: List of evaluation results

    Returns:
        Dictionary with advanced metrics
    """
    successful = [r for r in results if not r.proxy_response.error and not r.anthropic_response.error]

    if not successful:
        return {}

    input_diffs = [r.input_diff for r in successful]
    output_diffs = [r.output_diff for r in successful]
    total_diffs = [r.total_diff for r in successful]

    input_pct_diffs = [r.input_pct_diff for r in successful]
    output_pct_diffs = [r.output_pct_diff for r in successful]

    return {
        # Standard deviation
        "input_diff_std": np.std(input_diffs) if input_diffs else 0,
        "output_diff_std": np.std(output_diffs) if output_diffs else 0,
        "total_diff_std": np.std(total_diffs) if total_diffs else 0,
        # Median
        "input_diff_median": np.median(input_diffs) if input_diffs else 0,
        "output_diff_median": np.median(output_diffs) if output_diffs else 0,
        "total_diff_median": np.median(total_diffs) if total_diffs else 0,
        # Percentiles
        "input_diff_75th": np.percentile(input_diffs, 75) if input_diffs else 0,
        "input_diff_95th": np.percentile(input_diffs, 95) if input_diffs else 0,
        "input_diff_99th": np.percentile(input_diffs, 99) if input_diffs else 0,
        # Max errors
        "input_diff_max": max(input_diffs) if input_diffs else 0,
        "output_diff_max": max(output_diffs) if output_diffs else 0,
        # Correlation
        "input_output_correlation": (
            np.corrcoef(
                [r.proxy_response.input_tokens or 0 for r in successful],
                [r.anthropic_response.input_tokens or 0 for r in successful],
            )[0, 1]
            if len(successful) > 1
            else 0
        ),
        # Statistical significance tests
        "input_ttest": (
            stats.ttest_1samp(input_diffs, 0).pvalue if input_diffs else None
        ),
        "output_ttest": (
            stats.ttest_1samp(output_diffs, 0).pvalue if output_diffs else None
        ),
    }


def detect_systematic_bias(results: List[EvaluationResult]) -> dict:
    """Detect systematic biases in token counting.

    Args:
        results: List of evaluation results

    Returns:
        Dictionary with bias analysis
    """
    successful = [r for r in results if not r.proxy_response.error and not r.anthropic_response.error]

    if not successful:
        return {}

    input_diffs = [
        (r.proxy_response.input_tokens or 0) - (r.anthropic_response.input_tokens or 0)
        for r in successful
    ]
    output_diffs = [
        (r.proxy_response.output_tokens or 0) - (r.anthropic_response.output_tokens or 0)
        for r in successful
    ]

    return {
        # Input bias
        "input_bias_mean": np.mean(input_diffs) if input_diffs else 0,
        "input_bias_std": np.std(input_diffs) if input_diffs else 0,
        "input_consistently_high": sum(1 for d in input_diffs if d > 0),
        "input_consistently_low": sum(1 for d in input_diffs if d < 0),
        # Output bias
        "output_bias_mean": np.mean(output_diffs) if output_diffs else 0,
        "output_bias_std": np.std(output_diffs) if output_diffs else 0,
        "output_consistently_high": sum(1 for d in output_diffs if d > 0),
        "output_consistently_low": sum(1 for d in output_diffs if d < 0),
        # Bias patterns
        "both_high": sum(
            1 for i, o in zip(input_diffs, output_diffs) if i > 0 and o > 0
        ),
        "both_low": sum(
            1 for i, o in zip(input_diffs, output_diffs) if i < 0 and o < 0
        ),
        "mixed_bias": sum(
            1 for i, o in zip(input_diffs, output_diffs) if (i > 0) != (o > 0)
        ),
    }


def calculate_accuracy_by_token_range(results: List[EvaluationResult]) -> dict:
    """Calculate accuracy metrics grouped by token count ranges.

    Args:
        results: List of evaluation results

    Returns:
        Dictionary with accuracy by token range
    """
    successful = [r for r in results if not r.proxy_response.error and not r.anthropic_response.error]

    if not successful:
        return {}

    ranges = {
        "small (0-100)": [],
        "medium (101-500)": [],
        "large (501-1000)": [],
        "xlarge (1000+)": [],
    }

    for r in successful:
        total = (r.anthropic_response.input_tokens or 0) + (r.anthropic_response.output_tokens or 0)
        if total <= 100:
            ranges["small (0-100)"].append(r.total_diff)
        elif total <= 500:
            ranges["medium (101-500)"].append(r.total_diff)
        elif total <= 1000:
            ranges["large (501-1000)"].append(r.total_diff)
        else:
            ranges["xlarge (1000+)"].append(r.total_diff)

    return {
        range_name: {
            "count": len(diffs),
            "mae": np.mean(diffs) if diffs else 0,
            "max_error": max(diffs) if diffs else 0,
        }
        for range_name, diffs in ranges.items()
    }
