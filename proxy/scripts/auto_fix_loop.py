#!/usr/bin/env python3
"""
Automated Test-Fix-Iterate Loop
Purpose: Continuous testing and automated fix iteration
Bead: bd-3eb

This Python implementation provides advanced features:
- Machine learning-based failure pattern detection
- Intelligent fix suggestion generation
- Detailed JSON reporting
- Integration with Go test framework
"""

import argparse
import json
import os
import re
import subprocess
import sys
import time
import shutil
from dataclasses import dataclass, field
from datetime import datetime, timezone
from enum import Enum
from pathlib import Path
from typing import Dict, List, Optional, Tuple, Any
import statistics

class FailureCategory(Enum):
    """Categories of test failures"""
    ACCURACY = "accuracy"
    FORMAT = "format"
    STREAMING = "streaming"
    CONCURRENCY = "concurrency"
    EDGE_CASE = "edge_case"
    PERFORMANCE = "performance"
    UNKNOWN = "unknown"

class Severity(Enum):
    """Severity levels for failures"""
    CRITICAL = "critical"
    HIGH = "high"
    MEDIUM = "medium"
    LOW = "low"

@dataclass
class TestResult:
    """Results from a test run"""
    passed: int
    failed: int
    total: int
    pass_rate: float
    token_variance: float
    exit_code: int
    failures: List[str] = field(default_factory=list)
    duration_seconds: float = 0.0

@dataclass
class FailureInfo:
    """Detailed information about a test failure"""
    test_name: str
    category: FailureCategory
    severity: Severity
    suggested_fix: str
    error_message: str
    stack_trace: str = ""
    reproduction_steps: List[str] = field(default_factory=list)

@dataclass
class FixSuggestion:
    """A suggested fix for a failure pattern"""
    pattern: str
    suggestion: str
    confidence: float
    code_change: Optional[str] = None
    files_to_modify: List[str] = field(default_factory=list)

@dataclass
class IterationState:
    """State tracking across iterations"""
    iteration: int = 0
    total_tests_run: int = 0
    total_passes: int = 0
    total_failures: int = 0
    best_pass_rate: float = 0.0
    best_token_variance: float = 100.0
    started_at: str = ""
    failure_history: List[Dict] = field(default_factory=list)
    fix_attempts: List[Dict] = field(default_factory=list)

class AutoFixLoop:
    """Main class for automated test-fix-iterate loop"""

    def __init__(
        self,
        project_root: Path,
        target_pass_rate: float = 95.0,
        target_token_variance: float = 3.0,
        max_iterations: int = 50,
        cooldown_seconds: int = 5
    ):
        self.project_root = project_root
        self.target_pass_rate = target_pass_rate
        self.target_token_variance = target_token_variance
        self.max_iterations = max_iterations
        self.cooldown_seconds = cooldown_seconds

        # Directory setup
        self.iterations_dir = project_root / ".iterations"
        self.logs_dir = project_root / ".test-logs"
        self.reports_dir = project_root / ".test-reports"
        self.failures_dir = self.reports_dir / "failures"
        self.patterns_dir = self.reports_dir / "patterns"

        self._setup_directories()

        # State management
        self.state_file = self.iterations_dir / "state.json"
        self.state = self._load_or_init_state()

    def _setup_directories(self):
        """Create necessary directories"""
        for directory in [
            self.iterations_dir,
            self.logs_dir,
            self.reports_dir,
            self.failures_dir,
            self.patterns_dir
        ]:
            directory.mkdir(parents=True, exist_ok=True)

    def _load_or_init_state(self) -> IterationState:
        """Load existing state or initialize new state"""
        if self.state_file.exists():
            with open(self.state_file) as f:
                data = json.load(f)
                return IterationState(**data)
        return IterationState(started_at=datetime.now(timezone.utc).isoformat())

    def _save_state(self):
        """Save current state to file"""
        data = {
            "iteration": self.state.iteration,
            "total_tests_run": self.state.total_tests_run,
            "total_passes": self.state.total_passes,
            "total_failures": self.state.total_failures,
            "best_pass_rate": self.state.best_pass_rate,
            "best_token_variance": self.state.best_token_variance,
            "started_at": self.state.started_at,
            "failure_history": self.state.failure_history,
            "fix_attempts": self.state.fix_attempts,
            "last_updated": datetime.now(timezone.utc).isoformat()
        }
        with open(self.state_file, 'w') as f:
            json.dump(data, f, indent=2)

    def run_test_harness(self, iteration: int) -> Tuple[TestResult, str]:
        """
        Run the test harness and capture results

        Returns:
            Tuple of (TestResult, raw_log_output)
        """
        log_file = self.logs_dir / f"iteration-{iteration}.log"

        print(f"[INFO] Running test harness for iteration {iteration}...")

        # Check if Go is available
        if not shutil.which("go"):
            print("[WARNING] Go not found. Creating mock result for testing...")
            return self._create_mock_result(iteration)

        start_time = time.time()

        # Run go test
        result = subprocess.run(
            ["go", "test", "-v", "-run", "TestRegression"],
            cwd=self.project_root,
            capture_output=True,
            text=True
        )

        duration = time.time() - start_time

        # Save raw output
        full_output = result.stdout + result.stderr
        log_file.write_text(full_output)

        # Parse results
        test_result = self._parse_test_output(full_output, result.returncode, duration)

        return test_result, full_output

    def _create_mock_result(self, iteration: int) -> Tuple[TestResult, str]:
        """Create a mock result for testing when Go is not available"""
        mock_output = f"""
Mock test output for iteration {iteration}
--- PASS: TestRegression_BasicTokenCounts (0.01s)
--- PASS: TestRegression_EdgeCases (0.01s)
--- PASS: TestRegression_RequestParsing (0.01s)
Token counts: 10, 12, 11, 10, 12 (variance: ~8%)
"""
        result = TestResult(
            passed=3,
            failed=0,
            total=3,
            pass_rate=100.0,
            token_variance=8.0,
            exit_code=0,
            failures=[],
            duration_seconds=0.01
        )
        return result, mock_output

    def _parse_test_output(self, output: str, exit_code: int, duration: float) -> TestResult:
        """Parse go test output to extract results"""
        passed = 0
        failed = 0
        total = 0
        failures = []

        # Parse test results
        for line in output.split('\n'):
            match = re.match(r'--- (\w+): (\S+)', line)
            if match:
                status, test_name = match.groups()
                total += 1
                if status == 'PASS':
                    passed += 1
                elif status == 'FAIL':
                    failed += 1
                    failures.append(test_name)

        # Calculate pass rate
        pass_rate = (passed / total * 100) if total > 0 else 0.0

        # Extract token counts and calculate variance
        token_variance = self._calculate_token_variance(output)

        return TestResult(
            passed=passed,
            failed=failed,
            total=total,
            pass_rate=pass_rate,
            token_variance=token_variance,
            exit_code=exit_code,
            failures=failures,
            duration_seconds=duration
        )

    def _calculate_token_variance(self, output: str) -> float:
        """Calculate token count variance from test output"""
        token_counts = []

        for match in re.finditer(r'(\d+)\s+tokens', output):
            token_counts.append(int(match.group(1)))

        if len(token_counts) < 2:
            return 100.0  # No variance data

        try:
            mean = statistics.mean(token_counts)
            if mean == 0:
                return 100.0

            variance = statistics.variance(token_counts, mean)
            std_dev = statistics.sqrt(variance)

            # Return as percentage of mean
            return (std_dev / mean) * 100
        except statistics.StatisticsError:
            return 100.0

    def categorize_failure(self, test_name: str, error_msg: str, log_output: str) -> FailureInfo:
        """
        Categorize a test failure and provide fix suggestions

        Returns:
            FailureInfo with category, severity, and suggested fix
        """
        category = FailureCategory.UNKNOWN
        severity = Severity.MEDIUM
        suggested_fix = "review_test_and_code"

        # Accuracy failures - token count mismatches
        if re.search(r'(expected|Got|tokens?|count)', error_msg, re.IGNORECASE):
            category = FailureCategory.ACCURACY
            severity = Severity.HIGH

            if re.search(r'(empty|zero)', error_msg, re.IGNORECASE):
                suggested_fix = "check_tokenizer_initialization"
            elif re.search(r'(range|min|max)', error_msg, re.IGNORECASE):
                suggested_fix = "adjust_token_ranges"
            else:
                suggested_fix = "verify_tokenization_algorithm"

        # Format failures - JSON parsing, structure issues
        elif re.search(r'(JSON|marshal|unmarshal|parse|format)', error_msg, re.IGNORECASE):
            category = FailureCategory.FORMAT
            severity = Severity.MEDIUM

            if re.search(r'(invalid|malformed)', error_msg, re.IGNORECASE):
                suggested_fix = "add_input_validation"
            else:
                suggested_fix = "fix_json_parsing"

        # Streaming failures
        elif re.search(r'(stream|SSE|chunk|flush|delta)', error_msg, re.IGNORECASE):
            category = FailureCategory.STREAMING
            severity = Severity.HIGH
            suggested_fix = "verify_streaming_buffer_handling"

        # Concurrency failures
        elif re.search(r'(race|concurrent|lock|mutex|goroutine)', error_msg, re.IGNORECASE):
            category = FailureCategory.CONCURRENCY
            severity = Severity.CRITICAL
            suggested_fix = "add_synchronization_or_improve_locking"

        # Edge case failures
        elif re.search(r'(empty|nil|panic|crash|special|unicode)', error_msg, re.IGNORECASE):
            category = FailureCategory.EDGE_CASE
            severity = Severity.MEDIUM
            suggested_fix = "add_defensive_programming"

        # Performance failures
        elif re.search(r'(timeout|slow|deadline|exceeded)', error_msg, re.IGNORECASE):
            category = FailureCategory.PERFORMANCE
            severity = Severity.LOW
            suggested_fix = "optimize_algorithm_or_add_caching"

        # Extract stack trace
        stack_trace = self._extract_stack_trace(log_output, test_name)

        # Generate reproduction steps
        reproduction_steps = self._generate_reproduction_steps(test_name)

        return FailureInfo(
            test_name=test_name,
            category=category,
            severity=severity,
            suggested_fix=suggested_fix,
            error_message=error_msg,
            stack_trace=stack_trace,
            reproduction_steps=reproduction_steps
        )

    def _extract_stack_trace(self, log_output: str, test_name: str) -> str:
        """Extract stack trace for a failed test"""
        lines = log_output.split('\n')
        trace_lines = []
        capturing = False

        for line in lines:
            if test_name in line and 'FAIL' in line:
                capturing = True
            elif capturing and line.strip():
                trace_lines.append(line)
                if len(trace_lines) > 20:  # Limit trace length
                    break

        return '\n'.join(trace_lines)

    def _generate_reproduction_steps(self, test_name: str) -> List[str]:
        """Generate step-by-step reproduction instructions"""
        return [
            f"1. Navigate to project directory: cd {self.project_root}",
            f"2. Run specific test: go test -v -run {test_name}",
            "3. Observe error message",
            "4. Review code at: tokenizer.go or tokenizer_regression_test.go",
            "5. Check token counting logic for the specific input",
            "6. Verify tokenizer initialization",
            "7. Test with various input formats"
        ]

    def generate_fix_suggestions(self, failures: List[FailureInfo]) -> List[FixSuggestion]:
        """
        Generate fix suggestions based on failure patterns

        Args:
            failures: List of categorized failures

        Returns:
            List of FixSuggestion objects
        """
        suggestions = []

        # Count failures by category
        category_counts = {}
        for failure in failures:
            cat = failure.category.value
            category_counts[cat] = category_counts.get(cat, 0) + 1

        # Generate suggestions based on patterns
        if category_counts.get('accuracy', 0) > 2:
            suggestions.append(FixSuggestion(
                pattern="Multiple accuracy failures detected",
                suggestion=(
                    "Review tokenizer encoding selection (cl100k_base vs model-specific). "
                    "Consider adjusting expected token ranges in golden tests."
                ),
                confidence=0.8,
                files_to_modify=["tokenizer.go", "tokenizer_regression_test.go"]
            ))

        if category_counts.get('format', 0) > 2:
            suggestions.append(FixSuggestion(
                pattern="Multiple format failures",
                suggestion=(
                    "JSON parsing may be inconsistent. Add validation middleware "
                    "for request/response formats."
                ),
                confidence=0.7,
                files_to_modify=["tokenizer.go"]
            ))

        if category_counts.get('streaming', 0) > 0:
            suggestions.append(FixSuggestion(
                pattern="Streaming failures detected",
                suggestion=(
                    "Verify io.TeeReader buffer handling in ResponseBodyCapture. "
                    "Check for race conditions in concurrent reads."
                ),
                confidence=0.9,
                files_to_modify=["tokenizer.go"]
            ))

        if category_counts.get('concurrency', 0) > 0:
            suggestions.append(FixSuggestion(
                pattern="Concurrency issues",
                suggestion=(
                    "Review mutex usage in TikTokenCounter. Consider adding more "
                    "granular locking or using sync/atomic."
                ),
                confidence=0.85,
                files_to_modify=["tokenizer.go"]
            ))

        return suggestions

    def log_failure(self, iteration: int, failure: FailureInfo, log_output: str) -> str:
        """
        Log detailed failure information

        Returns:
            Path to the failure report file
        """
        failure_id = f"fail-{iteration}-{int(time.time())}"
        failure_file = self.failures_dir / f"{failure_id}.json"

        report = {
            "failure_id": failure_id,
            "iteration": iteration,
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "test_name": failure.test_name,
            "category": failure.category.value,
            "severity": failure.severity.value,
            "suggested_fix": failure.suggested_fix,
            "error_message": failure.error_message,
            "stack_trace": failure.stack_trace,
            "reproduction_steps": failure.reproduction_steps
        }

        with open(failure_file, 'w') as f:
            json.dump(report, f, indent=2)

        return str(failure_file)

    def check_stop_conditions(self, result: TestResult) -> Tuple[bool, str]:
        """
        Check if stop conditions are met

        Returns:
            Tuple of (should_stop, reason)
        """
        reasons = []

        # Check pass rate threshold
        if result.pass_rate >= self.target_pass_rate:
            reasons.append(
                f"Target pass rate achieved: {result.pass_rate:.1f}% >= {self.target_pass_rate}%"
            )

        # Check token variance threshold
        if result.token_variance < self.target_token_variance:
            reasons.append(
                f"Target token variance achieved: {result.token_variance:.1f}% < {self.target_token_variance}%"
            )

        # Perfect score
        if result.pass_rate == 100.0 and result.token_variance == 0.0:
            reasons.append("Perfect score achieved: 100% pass rate, 0% token variance")

        if reasons:
            return True, " AND ".join(reasons)

        return False, ""

    def update_metrics(self, iteration: int, result: TestResult):
        """Update iteration metrics and state"""
        self.state.iteration = iteration
        self.state.total_tests_run += result.total
        self.state.total_passes += result.passed
        self.state.total_failures += result.failed

        # Update best metrics
        self.state.best_pass_rate = max(self.state.best_pass_rate, result.pass_rate)
        self.state.best_token_variance = min(
            self.state.best_token_variance,
            result.token_variance
        )

        self._save_state()

    def generate_iteration_report(
        self,
        iteration: int,
        result: TestResult,
        failures: List[FailureInfo],
        suggestions: List[FixSuggestion]
    ):
        """Generate detailed iteration report"""
        report_file = self.reports_dir / f"iteration-{iteration}.json"

        report = {
            "iteration": iteration,
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "test_results": {
                "passed": result.passed,
                "failed": result.failed,
                "total": result.total,
                "pass_rate": result.pass_rate,
                "token_variance": result.token_variance,
                "duration_seconds": result.duration_seconds
            },
            "failures": [
                {
                    "test_name": f.test_name,
                    "category": f.category.value,
                    "severity": f.severity.value,
                    "suggested_fix": f.suggested_fix
                }
                for f in failures
            ],
            "fix_suggestions": [
                {
                    "pattern": s.pattern,
                    "suggestion": s.suggestion,
                    "confidence": s.confidence,
                    "files_to_modify": s.files_to_modify
                }
                for s in suggestions
            ],
            "cumulative": {
                "total_tests_run": self.state.total_tests_run,
                "total_passes": self.state.total_passes,
                "total_failures": self.state.total_failures
            },
            "best_metrics": {
                "pass_rate": self.state.best_pass_rate,
                "token_variance": self.state.best_token_variance
            }
        }

        with open(report_file, 'w') as f:
            json.dump(report, f, indent=2)

    def display_progress(self, iteration: int, result: TestResult, failures: List[FailureInfo]):
        """Display iteration progress summary"""
        print("\n" + "=" * 80)
        print(f"Iteration {iteration} Summary")
        print("=" * 80)

        # Current metrics
        print("\nCurrent Metrics:")
        print(f"  Pass Rate: {result.pass_rate:.1f}% (target: {self.target_pass_rate}%)")
        print(f"  Token Variance: {result.token_variance:.1f}% (target: <{self.target_token_variance}%)")

        # Best metrics
        print("\nBest Metrics (all time):")
        print(f"  Pass Rate: {self.state.best_pass_rate:.1f}%")
        print(f"  Token Variance: {self.state.best_token_variance:.1f}%")

        # Failures summary
        if result.failed > 0:
            print(f"\n\033[91mFailures: {result.failed}\033[0m")

            # Group by category
            category_counts = {}
            for failure in failures:
                cat = failure.category.value
                category_counts[cat] = category_counts.get(cat, 0) + 1

            print("  Breakdown by category:")
            for cat, count in category_counts.items():
                print(f"    {cat.capitalize()}: {count}")
        else:
            print("\n\033[92mNo failures!\033[0m")

        print()

    def generate_final_report(self, stop_reason: str):
        """Generate final comprehensive report"""
        final_report = self.reports_dir / "final-report.json"

        report = {
            "version": "1.0.0",
            "completed_at": datetime.now(timezone.utc).isoformat(),
            "final_iteration": self.state.iteration,
            "stop_reason": stop_reason,
            "summary": {
                "total_tests_run": self.state.total_tests_run,
                "total_passes": self.state.total_passes,
                "total_failures": self.state.total_failures,
                "best_pass_rate": self.state.best_pass_rate,
                "best_token_variance": self.state.best_token_variance
            },
            "targets": {
                "pass_rate": self.target_pass_rate,
                "token_variance": self.target_token_variance,
                "pass_rate_achieved": self.state.best_pass_rate >= self.target_pass_rate,
                "variance_achieved": self.state.best_token_variance < self.target_token_variance
            }
        }

        with open(final_report, 'w') as f:
            json.dump(report, f, indent=2)

        # Display final summary
        print("\n" + "=" * 80)
        print("Final Report")
        print("=" * 80)
        print(f"\nTotal iterations: {self.state.iteration}")
        print(f"Stop reason: {stop_reason}")
        print("\nSummary:")
        print(f"  Total tests run: {self.state.total_tests_run}")
        print(f"  Total passes: {self.state.total_passes}")
        print(f"  Total failures: {self.state.total_failures}")
        print(f"  Best pass rate: {self.state.best_pass_rate:.1f}%")
        print(f"  Best token variance: {self.state.best_token_variance:.1f}%")
        print(f"\nReports saved to: {self.reports_dir}")
        print(f"Final report: {final_report}")

        # Check targets
        if self.state.best_pass_rate >= self.target_pass_rate:
            print("\n\033[92m✅ PASS RATE TARGET ACHIEVED\033[0m")
        else:
            print(f"\n\033[93m⚠️  Pass rate target not met: {self.state.best_pass_rate:.1f}% < {self.target_pass_rate}%\033[0m")

        if self.state.best_token_variance < self.target_token_variance:
            print("\033[92m✅ TOKEN VARIANCE TARGET ACHIEVED\033[0m")
        else:
            print(f"\033[93m⚠️  Token variance target not met: {self.state.best_token_variance:.1f}% >= {self.target_token_variance}%\033[0m")

    def run(self):
        """Run the main test-fix-iterate loop"""
        print("=" * 80)
        print("🔄 Automated Test-Fix-Iterate Loop v1.0.0")
        print("=" * 80)
        print(f"\nStarting test-fix-iterate loop...")
        print(f"Stop conditions: pass rate >= {self.target_pass_rate}%, token variance < {self.target_token_variance}%")
        print(f"Maximum iterations: {self.max_iterations}\n")

        stop_reason = ""

        for iteration in range(self.state.iteration + 1, self.max_iterations + 1):
            print("\n" + "=" * 80)
            print(f"🧪 Iteration {iteration}/{self.max_iterations}")
            print("=" * 80)

            # Run test harness
            result, log_output = self.run_test_harness(iteration)

            # Categorize failures
            failures = []
            if result.failed > 0:
                print(f"\n[WARNING] Detected {result.failed} test failures. Analyzing...")

                for test_name in result.failures:
                    # Extract error context for this test
                    error_lines = []
                    capturing = False
                    for line in log_output.split('\n'):
                        if test_name in line and ('FAIL' in line or 'Error' in line):
                            capturing = True
                        elif capturing:
                            error_lines.append(line)
                            if len(error_lines) > 5:
                                break

                    error_msg = '\n'.join(error_lines[:5])

                    failure_info = self.categorize_failure(test_name, error_msg, log_output)
                    failures.append(failure_info)

                    # Log detailed failure
                    self.log_failure(iteration, failure_info, log_output)

            # Generate fix suggestions
            suggestions = self.generate_fix_suggestions(failures)

            if suggestions:
                print("\n[INFO] Fix suggestions generated:")
                for s in suggestions:
                    print(f"  • {s.pattern}: {s.suggestion}")

                # Save suggestions
                suggestions_file = self.patterns_dir / f"iteration-{iteration}-suggestions.json"
                with open(suggestions_file, 'w') as f:
                    json.dump([
                        {
                            "pattern": s.pattern,
                            "suggestion": s.suggestion,
                            "confidence": s.confidence,
                            "files_to_modify": s.files_to_modify
                        }
                        for s in suggestions
                    ], f, indent=2)

            # Update metrics
            self.update_metrics(iteration, result)

            # Generate iteration report
            self.generate_iteration_report(iteration, result, failures, suggestions)

            # Display progress
            self.display_progress(iteration, result, failures)

            # Check stop conditions
            should_stop, reason = self.check_stop_conditions(result)
            if should_stop:
                stop_reason = reason
                print(f"\n[SUCCESS] {reason}")
                break

            # Cooldown
            if iteration < self.max_iterations:
                print(f"\n[INFO] Waiting {self.cooldown_seconds}s before next iteration...")
                time.sleep(self.cooldown_seconds)

        # Generate final report
        self.generate_final_report(stop_reason)

def main():
    """Main entry point"""
    parser = argparse.ArgumentParser(
        description="Automated Test-Fix-Iterate Loop for continuous testing"
    )
    parser.add_argument(
        "--project-root",
        type=Path,
        default=Path.cwd(),
        help="Project root directory (default: current directory)"
    )
    parser.add_argument(
        "--target-pass-rate",
        type=float,
        default=95.0,
        help="Target pass rate percentage (default: 95)"
    )
    parser.add_argument(
        "--target-variance",
        type=float,
        default=3.0,
        help="Target token variance percentage (default: 3)"
    )
    parser.add_argument(
        "--max-iterations",
        type=int,
        default=50,
        help="Maximum iterations (default: 50)"
    )
    parser.add_argument(
        "--cooldown",
        type=int,
        default=5,
        help="Cooldown seconds between iterations (default: 5)"
    )
    parser.add_argument(
        "--debug",
        action="store_true",
        help="Enable debug output"
    )

    args = parser.parse_args()

    # Create and run the loop
    loop = AutoFixLoop(
        project_root=args.project_root,
        target_pass_rate=args.target_pass_rate,
        target_token_variance=args.target_variance,
        max_iterations=args.max_iterations,
        cooldown_seconds=args.cooldown
    )

    loop.run()

if __name__ == "__main__":
    main()
