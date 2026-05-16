"""HTTP client for making requests to proxy and Anthropic APIs."""

import time
import json
from typing import Optional
import httpx


class ProxyClient:
    """Client for z.ai proxy."""

    def __init__(self, base_url: str, api_key: str):
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.client = httpx.Client(timeout=300.0)

    def make_request(
        self,
        model: str,
        messages: list[dict],
        max_tokens: int = 100,
        stream: bool = False,
        temperature: Optional[float] = None,
    ) -> dict:
        """Make a request to the proxy and extract token usage."""
        from zai_eval.models import ProxyResponse

        start_time = time.time()

        headers = {
            "Content-Type": "application/json",
            "x-api-key": self.api_key,
            "anthropic-version": "2023-06-01",
        }

        payload = {
            "model": model,
            "messages": messages,
            "max_tokens": max_tokens,
            "stream": stream,
        }

        if temperature is not None:
            payload["temperature"] = temperature

        try:
            response = self.client.post(
                f"{self.base_url}/v1/messages",
                headers=headers,
                json=payload,
            )

            latency_ms = (time.time() - start_time) * 1000

            # Extract token counts from headers or response body
            input_tokens = None
            output_tokens = None

            # Check trailer headers first
            input_tokens = response.headers.get("X-Token-Input")
            output_tokens = response.headers.get("X-Token-Output")
            total_tokens = response.headers.get("X-Token-Total")

            # If not in headers, try response body
            if input_tokens is None:
                try:
                    data = response.json()
                    if "usage" in data:
                        input_tokens = data["usage"].get("input_tokens")
                        output_tokens = data["usage"].get("output_tokens")
                except (json.JSONDecodeError, KeyError):
                    pass

            # Convert to int
            input_tokens = int(input_tokens) if input_tokens else None
            output_tokens = int(output_tokens) if output_tokens else None
            total_tokens = int(total_tokens) if total_tokens else None

            return ProxyResponse(
                status_code=response.status_code,
                input_tokens=input_tokens,
                output_tokens=output_tokens,
                total_tokens=total_tokens,
                usage_header=response.headers.get("X-Token-Usage"),
                latency_ms=latency_ms,
            )

        except httpx.HTTPError as e:
            return ProxyResponse(
                status_code=0,
                error=str(e),
                latency_ms=(time.time() - start_time) * 1000,
            )


class AnthropicClient:
    """Client for Anthropic API."""

    def __init__(self, api_key: str):
        self.api_key = api_key
        self.client = httpx.Client(
            base_url="https://api.anthropic.com",
            timeout=300.0,
        )

    def make_request(
        self,
        model: str,
        messages: list[dict],
        max_tokens: int = 100,
        stream: bool = False,
        temperature: Optional[float] = None,
    ) -> dict:
        """Make a request to Anthropic API and extract token usage."""
        from zai_eval.models import AnthropicResponse

        start_time = time.time()

        headers = {
            "Content-Type": "application/json",
            "x-api-key": self.api_key,
            "anthropic-version": "2023-06-01",
        }

        payload = {
            "model": model,
            "messages": messages,
            "max_tokens": max_tokens,
            "stream": stream,
        }

        if temperature is not None:
            payload["temperature"] = temperature

        try:
            response = self.client.post(
                "/v1/messages",
                headers=headers,
                json=payload,
            )

            latency_ms = (time.time() - start_time) * 1000

            input_tokens = None
            output_tokens = None

            if response.status_code == 200:
                try:
                    data = response.json()
                    if "usage" in data:
                        input_tokens = data["usage"].get("input_tokens")
                        output_tokens = data["usage"].get("output_tokens")
                except (json.JSONDecodeError, KeyError):
                    pass

            return AnthropicResponse(
                status_code=response.status_code,
                input_tokens=input_tokens,
                output_tokens=output_tokens,
                total_tokens=(input_tokens or 0) + (output_tokens or 0),
                latency_ms=latency_ms,
            )

        except httpx.HTTPError as e:
            return AnthropicResponse(
                status_code=0,
                error=str(e),
                latency_ms=(time.time() - start_time) * 1000,
            )


class DualClient:
    """Client that makes parallel requests to both proxy and Anthropic."""

    def __init__(self, proxy_url: str, proxy_api_key: str, anthropic_api_key: str):
        self.proxy = ProxyClient(proxy_url, proxy_api_key)
        self.anthropic = AnthropicClient(anthropic_api_key)

    def evaluate_request(
        self,
        model: str,
        messages: list[dict],
        max_tokens: int = 100,
        stream: bool = False,
        temperature: Optional[float] = None,
    ) -> tuple:
        """Make parallel requests to both endpoints."""
        import concurrent.futures

        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as executor:
            proxy_future = executor.submit(
                self.proxy.make_request,
                model,
                messages,
                max_tokens,
                stream,
                temperature,
            )
            anthropic_future = executor.submit(
                self.anthropic.make_request,
                model,
                messages,
                max_tokens,
                stream,
                temperature,
            )

            proxy_response = proxy_future.result()
            anthropic_response = anthropic_future.result()

        return proxy_response, anthropic_response
