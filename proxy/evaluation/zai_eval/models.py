"""Data models for evaluation framework."""

from typing import Optional
from pydantic import BaseModel, Field
from datetime import datetime


class TokenUsage(BaseModel):
    """Token usage from API response."""

    input_tokens: int
    output_tokens: int
    total_tokens: int = Field(default_factory=lambda: 0)

    def __post_init__(self):
        """Calculate total tokens."""
        if self.total_tokens == 0:
            self.total_tokens = self.input_tokens + self.output_tokens


class EvaluationRequest(BaseModel):
    """A single evaluation request configuration."""

    name: str
    description: str
    model: str
    max_tokens: int
    messages: list[dict]
    stream: bool = False
    temperature: Optional[float] = None
    metadata: dict = Field(default_factory=dict)


class ProxyResponse(BaseModel):
    """Response from proxy endpoint."""

    status_code: int
    input_tokens: Optional[int] = None
    output_tokens: Optional[int] = None
    total_tokens: Optional[int] = None
    usage_header: Optional[str] = None
    error: Optional[str] = None
    latency_ms: float = 0


class AnthropicResponse(BaseModel):
    """Response from Anthropic API."""

    status_code: int
    input_tokens: Optional[int] = None
    output_tokens: Optional[int] = None
    total_tokens: Optional[int] = None
    error: Optional[str] = None
    latency_ms: float = 0


class EvaluationResult(BaseModel):
    """Result of comparing proxy vs Anthropic."""

    request_name: str
    proxy_response: ProxyResponse
    anthropic_response: AnthropicResponse

    # Token count comparisons
    input_match: bool
    output_match: bool
    total_match: bool

    # Differences
    input_diff: int = 0
    output_diff: int = 0
    total_diff: int = 0

    # Percentage differences
    input_pct_diff: float = 0.0
    output_pct_diff: float = 0.0
    total_pct_diff: float = 0.0

    # Accuracy metrics
    input_error_rate: float = 0.0
    output_error_rate: float = 0.0

    timestamp: datetime = Field(default_factory=datetime.utcnow)

    def calculate_metrics(self) -> None:
        """Calculate comparison metrics."""
        p_in = self.proxy_response.input_tokens or 0
        p_out = self.proxy_response.output_tokens or 0
        a_in = self.anthropic_response.input_tokens or 0
        a_out = self.anthropic_response.output_tokens or 0

        # Calculate differences
        self.input_diff = abs(p_in - a_in)
        self.output_diff = abs(p_out - a_out)
        self.total_diff = abs((p_in + p_out) - (a_in + a_out))

        # Calculate percentage differences
        if a_in > 0:
            self.input_pct_diff = (self.input_diff / a_in) * 100
        if a_out > 0:
            self.output_pct_diff = (self.output_diff / a_out) * 100

        total_a = a_in + a_out
        if total_a > 0:
            self.total_pct_diff = (self.total_diff / total_a) * 100

        # Calculate error rates
        self.input_error_rate = abs(p_in - a_in) / max(a_in, 1)
        self.output_error_rate = abs(p_out - a_out) / max(a_out, 1)

        # Determine matches
        self.input_match = p_in == a_in
        self.output_match = p_out == a_out
        self.total_match = (p_in + p_out) == (a_in + a_out)


class EvaluationReport(BaseModel):
    """Summary report of all evaluation results."""

    total_requests: int
    successful_requests: int
    failed_requests: int

    # Accuracy metrics
    input_token_accuracy: float = 0.0
    output_token_accuracy: float = 0.0
    overall_accuracy: float = 0.0

    # Mean Absolute Error
    input_mae: float = 0.0
    output_mae: float = 0.0
    total_mae: float = 0.0

    # Mean Percentage Error
    input_mpe: float = 0.0
    output_mpe: float = 0.0
    total_mpe: float = 0.0

    # Statistics
    results: list[EvaluationResult] = Field(default_factory=list)

    # Systematic biases
    input_bias_mean: float = 0.0
    output_bias_mean: float = 0.0

    # Latency comparison
    avg_proxy_latency_ms: float = 0.0
    avg_anthropic_latency_ms: float = 0.0

    timestamp: datetime = Field(default_factory=datetime.utcnow)

    def calculate_summary_metrics(self) -> None:
        """Calculate summary statistics from all results."""
        if not self.results:
            return

        successful = [r for r in self.results if not r.proxy_response.error and not r.anthropic_response.error]
        self.successful_requests = len(successful)
        self.failed_requests = len(self.results) - len(successful)

        if not successful:
            return

        # Accuracy
        input_matches = sum(1 for r in successful if r.input_match)
        output_matches = sum(1 for r in successful if r.output_match)
        total_matches = sum(1 for r in successful if r.total_match)

        self.input_token_accuracy = (input_matches / len(successful)) * 100
        self.output_token_accuracy = (output_matches / len(successful)) * 100
        self.overall_accuracy = (total_matches / len(successful)) * 100

        # Mean Absolute Error
        self.input_mae = sum(r.input_diff for r in successful) / len(successful)
        self.output_mae = sum(r.output_diff for r in successful) / len(successful)
        self.total_mae = sum(r.total_diff for r in successful) / len(successful)

        # Mean Percentage Error
        self.input_mpe = sum(r.input_pct_diff for r in successful) / len(successful)
        self.output_mpe = sum(r.output_pct_diff for r in successful) / len(successful)
        total_pct_diffs = [r.total_pct_diff for r in successful]
        self.total_mpe = sum(total_pct_diffs) / len(total_pct_diffs) if total_pct_diffs else 0

        # Systematic bias (positive = proxy overcounts, negative = proxy undercounts)
        input_diffs = [
            (r.proxy_response.input_tokens or 0) - (r.anthropic_response.input_tokens or 0)
            for r in successful
        ]
        output_diffs = [
            (r.proxy_response.output_tokens or 0) - (r.anthropic_response.output_tokens or 0)
            for r in successful
        ]

        self.input_bias_mean = sum(input_diffs) / len(input_diffs) if input_diffs else 0
        self.output_bias_mean = sum(output_diffs) / len(output_diffs) if output_diffs else 0

        # Latency
        self.avg_proxy_latency_ms = sum(r.proxy_response.latency_ms for r in successful) / len(successful)
        self.avg_anthropic_latency_ms = sum(r.anthropic_response.latency_ms for r in successful) / len(successful)
