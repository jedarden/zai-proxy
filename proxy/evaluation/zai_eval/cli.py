"""CLI interface for evaluation framework."""

import os
import sys
from pathlib import Path
from typing import Optional

import typer
from rich.console import Console
from dotenv import load_dotenv

from zai_eval.client import DualClient
from zai_eval.test_cases import get_test_cases, get_test_case_by_name, TEST_CASES
from zai_eval.models import EvaluationResult
from zai_eval.metrics import calculate_metrics
from zai_eval.report import print_report, save_report_json, save_report_markdown

app = typer.Typer(help="Z.AI Proxy Evaluation Framework")
console = Console()


def get_api_keys() -> tuple[str, str, str]:
    """Get API keys from environment variables.

    Returns:
        Tuple of (proxy_url, proxy_api_key, anthropic_api_key)
    """
    load_dotenv()

    proxy_url = os.getenv("ZAI_PROXY_URL", "http://localhost:8080")
    proxy_api_key = os.getenv("ZAI_API_KEY")
    anthropic_api_key = os.getenv("ANTHROPIC_API_KEY")

    if not proxy_api_key:
        console.print("[red]Error: ZAI_API_KEY environment variable not set[/red]")
        console.print("Set it with: export ZAI_API_KEY=your-key")
        raise typer.Exit(1)

    if not anthropic_api_key:
        console.print("[red]Error: ANTHROPIC_API_KEY environment variable not set[/red]")
        console.print("Set it with: export ANTHROPIC_API_KEY=your-key")
        raise typer.Exit(1)

    return proxy_url, proxy_api_key, anthropic_api_key


@app.command()
def list_tests():
    """List all available test cases."""
    console.print("\n[bold cyan]Available Test Cases[/bold cyan]\n")

    for i, test in enumerate(TEST_CASES, 1):
        console.print(f"{i}. [yellow]{test.name}[/yellow]")
        console.print(f"   {test.description}")
        console.print(f"   Model: {test.model} | Max tokens: {test.max_tokens}")
        console.print()


@app.command()
def run(
    test_name: Optional[str] = typer.Argument(None, help="Name of specific test to run"),
    output_dir: Optional[Path] = typer.Option(None, "--output", "-o", help="Output directory for reports"),
    json_output: bool = typer.Option(False, "--json", help="Save JSON report"),
    markdown_output: bool = typer.Option(False, "--markdown", help="Save Markdown report"),
    verbose: bool = typer.Option(False, "--verbose", "-v", help="Verbose output"),
):
    """Run evaluation tests.

    If TEST_NAME is provided, run only that test. Otherwise run all tests.
    """
    proxy_url, proxy_api_key, anthropic_api_key = get_api_keys()

    # Get test cases to run
    if test_name:
        test_case = get_test_case_by_name(test_name)
        if not test_case:
            console.print(f"[red]Error: Test '{test_name}' not found[/red]")
            console.print("Use 'zai-eval list-tests' to see available tests")
            raise typer.Exit(1)
        tests_to_run = [test_case]
        console.print(f"[cyan]Running test: {test_name}[/cyan]\n")
    else:
        tests_to_run = get_test_cases()
        console.print(f"[cyan]Running {len(tests_to_run)} tests[/cyan]\n")

    # Initialize client
    client = DualClient(proxy_url, proxy_api_key, anthropic_api_key)

    # Run tests
    results = []

    with console.status("[bold green]Running evaluation...") as status:
        for i, test in enumerate(tests_to_run, 1):
            status.update(f"[bold green]Running test {i}/{len(tests_to_run)}: {test.name}[/bold green]")

            if verbose:
                console.print(f"\n[yellow]Test: {test.name}[/yellow]")
                console.print(f"  Description: {test.description}")

            proxy_response, anthropic_response = client.evaluate_request(
                model=test.model,
                messages=test.messages,
                max_tokens=test.max_tokens,
                stream=test.stream,
                temperature=test.temperature,
            )

            result = EvaluationResult(
                request_name=test.name,
                proxy_response=proxy_response,
                anthropic_response=anthropic_response,
            )
            result.calculate_metrics()
            results.append(result)

            if verbose:
                if proxy_response.error:
                    console.print(f"  [red]Proxy error: {proxy_response.error}[/red]")
                if anthropic_response.error:
                    console.print(f"  [red]Anthropic error: {anthropic_response.error}[/red]")
                console.print(f"  Proxy: {proxy_response.input_tokens}/{proxy_response.output_tokens}")
                console.print(f"  Anthropic: {anthropic_response.input_tokens}/{anthropic_response.output_tokens}")
                console.print(f"  Diff: {result.total_diff:+d} ({result.total_pct_diff:.1f}%)")

    # Calculate metrics
    report = calculate_metrics(results)

    # Print report
    print_report(console, report)

    # Save reports if requested
    if output_dir:
        output_dir.mkdir(parents=True, exist_ok=True)

        if json_output:
            json_path = output_dir / "evaluation_report.json"
            save_report_json(report, str(json_path))
            console.print(f"\n[green]JSON report saved to: {json_path}[/green]")

        if markdown_output:
            md_path = output_dir / "evaluation_report.md"
            save_report_markdown(report, str(md_path))
            console.print(f"[green]Markdown report saved to: {md_path}[/green]")

    # Exit with error code if any tests failed
    failed_count = sum(1 for r in results if r.proxy_response.error or r.anthropic_response.error)
    if failed_count > 0:
        raise typer.Exit(1)


@app.command()
def quick(
    prompt: str = typer.Argument(..., help="Prompt text to test"),
    model: str = typer.Option("claude-3-sonnet-20240229", "--model", "-m", help="Model to use"),
    max_tokens: int = typer.Option(100, "--max-tokens", help="Max tokens"),
):
    """Run a quick single-test evaluation with custom prompt."""
    proxy_url, proxy_api_key, anthropic_api_key = get_api_keys()

    console.print(f"[cyan]Quick test with model: {model}[/cyan]\n")
    console.print(f"Prompt: {prompt[:100]}{'...' if len(prompt) > 100 else ''}\n")

    client = DualClient(proxy_url, proxy_api_key, anthropic_api_key)

    messages = [{"role": "user", "content": prompt}]

    proxy_response, anthropic_response = client.evaluate_request(
        model=model,
        messages=messages,
        max_tokens=max_tokens,
    )

    console.print("\n[bold]Results:[/bold]")
    console.print(f"Proxy:        In={proxy_response.input_tokens or 0}, Out={proxy_response.output_tokens or 0}")
    console.print(f"Anthropic:    In={anthropic_response.input_tokens or 0}, Out={anthropic_response.output_tokens or 0}")

    if proxy_response.error:
        console.print(f"[red]Proxy error: {proxy_response.error}[/red]")
    if anthropic_response.error:
        console.print(f"[red]Anthropic error: {anthropic_response.error}[/red]")


@app.command()
def validate():
    """Validate that both endpoints are accessible."""
    proxy_url, proxy_api_key, anthropic_api_key = get_api_keys()

    console.print("[cyan]Validating endpoints...[/cyan]\n")

    client = DualClient(proxy_url, proxy_api_key, anthropic_api_key)

    # Test proxy
    console.print("Testing Z.AI proxy...")
    proxy_resp, _ = client.evaluate_request(
        model="claude-3-sonnet-20240229",
        messages=[{"role": "user", "content": "test"}],
        max_tokens=10,
    )

    if proxy_resp.error:
        console.print(f"  [red]✗ Failed: {proxy_resp.error}[/red]")
    else:
        console.print(f"  [green]✓ OK[/green] (status: {proxy_resp.status_code})")

    # Test Anthropic
    console.print("Testing Anthropic API...")
    _, anthropic_resp = client.evaluate_request(
        model="claude-3-sonnet-20240229",
        messages=[{"role": "user", "content": "test"}],
        max_tokens=10,
    )

    if anthropic_resp.error:
        console.print(f"  [red]✗ Failed: {anthropic_resp.error}[/red]")
    else:
        console.print(f"  [green]✓ OK[/green] (status: {anthropic_resp.status_code})")

    console.print()


if __name__ == "__main__":
    app()
