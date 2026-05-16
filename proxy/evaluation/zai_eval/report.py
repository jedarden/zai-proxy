"""Report generation for evaluation framework."""

from rich.console import Console
from rich.table import Table
from rich.panel import Panel
from rich.progress import Progress, SpinnerColumn, TextColumn
from typing import List
from zai_eval.models import EvaluationResult, EvaluationReport
from zai_eval.metrics import calculate_advanced_metrics, detect_systematic_bias, calculate_accuracy_by_token_range


def print_report(console: Console, report: EvaluationReport) -> None:
    """Print comprehensive evaluation report to console.

    Args:
        console: Rich console instance
        report: Evaluation report to print
    """
    console.print("\n")
    console.print(Panel.fit("Z.AI PROXY EVALUATION REPORT", style="bold cyan"))

    # Summary section
    _print_summary(console, report)

    # Accuracy metrics
    _print_accuracy(console, report)

    # Error metrics
    _print_error_metrics(console, report)

    # Latency comparison
    _print_latency(console, report)

    # Systematic bias
    _print_bias_analysis(console, report)

    # Advanced metrics
    _print_advanced_metrics(console, report)

    # Detailed results table
    _print_detailed_results(console, report)


def _print_summary(console: Console, report: EvaluationReport) -> None:
    """Print summary section."""
    console.print("\n[bold cyan]Summary[/bold cyan]")
    console.print(f"Total Requests: {report.total_requests}")
    console.print(f"Successful: [green]{report.successful_requests}[/green]")
    console.print(f"Failed: [red]{report.failed_requests}[/red]")


def _print_accuracy(console: Console, report: EvaluationReport) -> None:
    """Print accuracy metrics."""
    table = Table(title="Token Count Accuracy", show_header=True, header_style="bold magenta")
    table.add_column("Metric", style="cyan")
    table.add_column("Accuracy (%)")

    table.add_row("Input Token Accuracy", f"{report.input_token_accuracy:.2f}%")
    table.add_row("Output Token Accuracy", f"{report.output_token_accuracy:.2f}%")
    table.add_row("Overall Accuracy", f"{report.overall_accuracy:.2f}%")

    console.print("\n")
    console.print(table)


def _print_error_metrics(console: Console, report: EvaluationReport) -> None:
    """Print error metrics."""
    table = Table(title="Mean Absolute Error (MAE)", show_header=True, header_style="bold magenta")
    table.add_column("Metric", style="cyan")
    table.add_column("MAE (tokens)")
    table.add_column("MPE (%)")

    table.add_row("Input Tokens", f"{report.input_mae:.2f}", f"{report.input_mpe:.2f}%")
    table.add_row("Output Tokens", f"{report.output_mae:.2f}", f"{report.output_mpe:.2f}%")
    table.add_row("Total Tokens", f"{report.total_mae:.2f}", f"{report.total_mpe:.2f}%")

    console.print("\n")
    console.print(table)


def _print_latency(console: Console, report: EvaluationReport) -> None:
    """Print latency comparison."""
    table = Table(title="Latency Comparison", show_header=True, header_style="bold magenta")
    table.add_column("Endpoint", style="cyan")
    table.add_column("Avg Latency (ms)")

    table.add_row("Z.AI Proxy", f"{report.avg_proxy_latency_ms:.2f}")
    table.add_row("Anthropic API", f"{report.avg_anthropic_latency_ms:.2f}")

    overhead = report.avg_proxy_latency_ms - report.avg_anthropic_latency_ms
    overhead_pct = (overhead / report.avg_anthropic_latency_ms * 100) if report.avg_anthropic_latency_ms > 0 else 0

    table.add_row("Overhead", f"{overhead:.2f} ({overhead_pct:+.1f}%)", style="yellow" if overhead > 0 else "green")

    console.print("\n")
    console.print(table)


def _print_bias_analysis(console: Console, report: EvaluationReport) -> None:
    """Print systematic bias analysis."""
    bias = detect_systematic_bias(report.results)

    if not bias:
        return

    table = Table(title="Systematic Bias Analysis", show_header=True, header_style="bold magenta")
    table.add_column("Metric", style="cyan")
    table.add_column("Value")

    input_status = "Overcounts" if bias["input_bias_mean"] > 0 else "Undercounts" if bias["input_bias_mean"] < 0 else "Accurate"
    output_status = "Overcounts" if bias["output_bias_mean"] > 0 else "Undercounts" if bias["output_bias_mean"] < 0 else "Accurate"

    table.add_row("Input Bias", f"{bias['input_bias_mean']:+.2f} tokens ({input_status})")
    table.add_row("Output Bias", f"{bias['output_bias_mean']:+.2f} tokens ({output_status})")

    console.print("\n")
    console.print(table)

    # Bias patterns
    if bias.get("mixed_bias", 0) > len(report.results) / 2:
        console.print("\n[yellow]⚠ Mixed bias detected - token counting may be inconsistent[/yellow]")
    elif bias.get("both_high", 0) > len(report.results) * 0.7:
        console.print("\n[red]⚠ Consistent overcounting detected[/red]")
    elif bias.get("both_low", 0) > len(report.results) * 0.7:
        console.print("\n[red]⚠ Consistent undercounting detected[/red]")


def _print_advanced_metrics(console: Console, report: EvaluationReport) -> None:
    """Print advanced statistical metrics."""
    advanced = calculate_advanced_metrics(report.results)

    if not advanced:
        return

    table = Table(title="Advanced Statistics", show_header=True, header_style="bold magenta")
    table.add_column("Metric", style="cyan")
    table.add_column("Input")
    table.add_column("Output")

    table.add_row("Std Dev", f"{advanced['input_diff_std']:.2f}", f"{advanced['output_diff_std']:.2f}")
    table.add_row("Median", f"{advanced['input_diff_median']:.2f}", f"{advanced['output_diff_median']:.2f}")
    table.add_row("95th Percentile", f"{advanced['input_diff_95th']:.2f}", f"{advanced['output_diff_95th']:.2f}")
    table.add_row("Max Error", f"{advanced['input_diff_max']:.2f}", f"{advanced['output_diff_max']:.2f}")

    console.print("\n")
    console.print(table)


def _print_detailed_results(console: Console, report: EvaluationReport) -> None:
    """Print detailed results table."""
    table = Table(title="Detailed Results", show_header=True, header_style="bold magenta")
    table.add_column("Test", style="cyan")
    table.add_column("Proxy In/Out")
    table.add_column("Anthropic In/Out")
    table.add_column("Diff")
    table.add_column("Status")

    for r in report.results:
        proxy_tokens = f"{r.proxy_response.input_tokens or 0}/{r.proxy_response.output_tokens or 0}"
        anthropic_tokens = f"{r.anthropic_response.input_tokens or 0}/{r.anthropic_response.output_tokens or 0}"
        diff = f"{r.total_diff:+d}"

        if r.proxy_response.error or r.anthropic_response.error:
            status = "[red]ERROR[/red]"
        elif r.total_diff == 0:
            status = "[green]✓[/green]"
        elif r.total_pct_diff < 5:
            status = "[yellow]~[/yellow]"
        else:
            status = "[red]✗[/red]"

        table.add_row(r.request_name, proxy_tokens, anthropic_tokens, diff, status)

    console.print("\n")
    console.print(table)


def save_report_json(report: EvaluationReport, filepath: str) -> None:
    """Save report as JSON file.

    Args:
        report: Evaluation report to save
        filepath: Path to output JSON file
    """
    import json
    from zai_eval.models import EvaluationResult

    def convert_result(result: EvaluationResult) -> dict:
        return {
            "request_name": result.request_name,
            "proxy": {
                "status_code": result.proxy_response.status_code,
                "input_tokens": result.proxy_response.input_tokens,
                "output_tokens": result.proxy_response.output_tokens,
                "error": result.proxy_response.error,
                "latency_ms": result.proxy_response.latency_ms,
            },
            "anthropic": {
                "status_code": result.anthropic_response.status_code,
                "input_tokens": result.anthropic_response.input_tokens,
                "output_tokens": result.anthropic_response.output_tokens,
                "error": result.anthropic_response.error,
                "latency_ms": result.anthropic_response.latency_ms,
            },
            "metrics": {
                "input_match": result.input_match,
                "output_match": result.output_match,
                "input_diff": result.input_diff,
                "output_diff": result.output_diff,
                "input_pct_diff": result.input_pct_diff,
                "output_pct_diff": result.output_pct_diff,
            },
            "timestamp": result.timestamp.isoformat(),
        }

    data = {
        "summary": {
            "total_requests": report.total_requests,
            "successful_requests": report.successful_requests,
            "failed_requests": report.failed_requests,
            "input_token_accuracy": report.input_token_accuracy,
            "output_token_accuracy": report.output_token_accuracy,
            "overall_accuracy": report.overall_accuracy,
            "input_mae": report.input_mae,
            "output_mae": report.output_mae,
            "total_mae": report.total_mae,
            "input_mpe": report.input_mpe,
            "output_mpe": report.output_mpe,
            "total_mpe": report.total_mpe,
            "avg_proxy_latency_ms": report.avg_proxy_latency_ms,
            "avg_anthropic_latency_ms": report.avg_anthropic_latency_ms,
        },
        "advanced_metrics": calculate_advanced_metrics(report.results),
        "bias_analysis": detect_systematic_bias(report.results),
        "accuracy_by_range": calculate_accuracy_by_token_range(report.results),
        "results": [convert_result(r) for r in report.results],
        "timestamp": report.timestamp.isoformat(),
    }

    with open(filepath, "w") as f:
        json.dump(data, f, indent=2)


def save_report_markdown(report: EvaluationReport, filepath: str) -> None:
    """Save report as Markdown file.

    Args:
        report: Evaluation report to save
        filepath: Path to output Markdown file
    """
    lines = [
        "# Z.AI Proxy Evaluation Report",
        "",
        f"**Generated:** {report.timestamp.isoformat()}",
        "",
        "## Summary",
        "",
        f"- **Total Requests:** {report.total_requests}",
        f"- **Successful:** {report.successful_requests}",
        f"- **Failed:** {report.failed_requests}",
        "",
        "## Accuracy Metrics",
        "",
        "| Metric | Accuracy |",
        "|--------|----------|",
        f"| Input Token Accuracy | {report.input_token_accuracy:.2f}% |",
        f"| Output Token Accuracy | {report.output_token_accuracy:.2f}% |",
        f"| Overall Accuracy | {report.overall_accuracy:.2f}% |",
        "",
        "## Error Metrics",
        "",
        "| Metric | MAE (tokens) | MPE (%) |",
        "|--------|---------------|---------|",
        f"| Input Tokens | {report.input_mae:.2f} | {report.input_mpe:.2f}% |",
        f"| Output Tokens | {report.output_mae:.2f} | {report.output_mpe:.2f}% |",
        f"| Total Tokens | {report.total_mae:.2f} | {report.total_mpe:.2f}% |",
        "",
        "## Latency Comparison",
        "",
        f"| Endpoint | Avg Latency (ms) |",
        f"|----------|------------------|",
        f"| Z.AI Proxy | {report.avg_proxy_latency_ms:.2f} |",
        f"| Anthropic API | {report.avg_anthropic_latency_ms:.2f} |",
        "",
        "## Systematic Bias",
        "",
        f"- **Input Bias:** {report.input_bias_mean:+.2f} tokens",
        f"- **Output Bias:** {report.output_bias_mean:+.2f} tokens",
        "",
        "## Detailed Results",
        "",
        "| Test | Proxy (In/Out) | Anthropic (In/Out) | Diff | Status |",
        "|------|-----------------|-------------------|------|--------|",
    ]

    for r in report.results:
        proxy_tokens = f"{r.proxy_response.input_tokens or 0}/{r.proxy_response.output_tokens or 0}"
        anthropic_tokens = f"{r.anthropic_response.input_tokens or 0}/{r.anthropic_response.output_tokens or 0}"

        if r.proxy_response.error or r.anthropic_response.error:
            status = "❌ ERROR"
        elif r.total_diff == 0:
            status = "✅ MATCH"
        elif r.total_pct_diff < 5:
            status = "⚠️ CLOSE"
        else:
            status = "❌ MISMATCH"

        lines.append(f"| {r.request_name} | {proxy_tokens} | {anthropic_tokens} | {r.total_diff:+d} | {status} |")

    with open(filepath, "w") as f:
        f.write("\n".join(lines))
