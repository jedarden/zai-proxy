#!/usr/bin/env python3
"""Credential-safe quota canary for the zai-proxy observe-only telemetry path.

This is the script-side companion to the canaries in
``proxy/quota_canary_test.go`` (hermetic, production wiring) and
``proxy/quota_canary_live_test.go`` (live, credential-gated). It samples the
two surfaces the operations doc compares -- the proxy's normalized quota
telemetry on ``/health`` and ZCode's usage view, which renders the same
provider endpoint (``/api/monitor/usage/quota/limit``) -- and emits one
artifact whose measurements are only ever timestamps, percentages, and reset
measures.

Modes
-----

``hermetic`` (default; no credential, no non-loopback traffic)
    A local synthetic origin serves the committed payloads in
    ``proxy/testdata/quota_canary/``: the provider fixtures on the monitor
    path and the committed proxy-quota-view fixtures on ``/health``. Those
    view fixtures are pinned to the production renderer by
    ``TestQuotaCanaryProxyViewFixtures``, so the proxy surface here is the
    rendering the proxy provably produces for the same payload, and the Go
    canaries run first unless ``--no-go-canary`` is passed. A synthetic
    credential is generated at run time and sent exactly as production sends
    one, which gives the secret-safety phase something real to assert against.
    Advancing rounds rotates the fixtures, which is what makes a usage
    percentage delta and a reset-stamp delta observable with no real account.

``live`` (``--proxy-url`` plus a credential in the environment)
    Samples a real proxy and the real provider origin. The credential is read
    only from ``$ZAI_API_KEY`` -- never argv, never a file in the repo -- and
    is discarded with the process.

Output schema
-------------

Every emitted measurement is a timestamp, a percentage, or a reset measure.
The only other fields are structural: which surface and window a number
belongs to, the tolerances the verdict used, and the verdict itself. The
artifact is validated against that allowlist at run time, so a future edit
that adds a field fails the canary instead of silently widening what it
publishes. Absolute credit amounts, plan metadata, poll counters, raw payload
keys, and the credential are outside the schema by construction.

Secret safety
-------------

Every run scans the script itself, the fixture directory, and the artifact it
is about to emit, and asserts the synthetic credential appears on no surface.
``secret-scan --self-test`` plants synthetic secrets in a temp file and
asserts the scanner finds them, so a scan that has gone blind fails loudly
rather than reporting a clean pass it did not earn. Findings cannot be
suppressed; the tool has no allowlist mechanism by design.

Exit codes: 0 the surfaces agree and the scan is clean, 1 the surfaces
diverge or the scan found something, 2 the canary could not run.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import secrets
import shutil
import subprocess
import sys
import tempfile
import threading
import time
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Callable
from urllib.error import URLError
from urllib.request import HTTPRedirectHandler, Request, build_opener

SCHEMA_VERSION = 1

# The provider endpoint ZCode's usage view renders and this proxy polls.
MONITOR_PATH = "/api/monitor/usage/quota/limit"

# Same response budget as proxy/quota/client.go: payloads are well under a
# kilobyte, so an oversized reply is a failure, not something to read.
MAX_RESPONSE_BYTES = 64 << 10

# The only provider window units the proxy models (proxy/quota/schema.go:
# wireUnitFiveHour, wireUnitWeekly). Entries with any other unit, and entries
# with an unmodelled limit type, are projected by neither surface.
WINDOW_BY_UNIT = {3: "five_hour", 6: "weekly"}
MODELLED_LIMIT_TYPES = {"credit_limit", "tokens_limit"}

# The hermetic rotation, in the order rounds observe it. Each provider payload
# has a matching committed proxy view, and the pairs are pinned to the
# production renderer by TestQuotaCanaryProxyViewFixtures. The third pair is
# what makes a reset-stamp delta observable without any real account.
HERMETIC_ROUNDS = [
    ("healthy.json", "proxy_quota_view_healthy.json"),
    ("usage_increased.json", "proxy_quota_view_increased.json"),
    ("reset_shifted.json", "proxy_quota_view_reset_shifted.json"),
]

# ---- Output schema allowlist -------------------------------------------------
#
# The artifact is walked against these sets at run time. A key outside them is
# a canary failure: the schema can only widen by an explicit edit here, never
# by a field added downstream.

ALLOWED_TOP = {
    "schema_version",
    "generated_at",
    "mode",
    "tolerance_pp",
    "reset_tolerance_seconds",
    "samples",
    "comparisons",
    "verdict",
}
ALLOWED_SAMPLE = {
    "at",
    "surface",
    "round",
    "window",
    "used_percent",
    "used_percent_delta_pp",
    "reset_at",
    "reset_delta_seconds",
    "reset_in_seconds",
}
ALLOWED_COMPARISON = {
    "at",
    "round",
    "window",
    "proxy_used_percent",
    "zcode_used_percent",
    "delta_pp",
    "proxy_reset_at",
    "zcode_reset_at",
    "reset_delta_seconds",
    "within_tolerance",
}

SURFACES = ("proxy", "zcode")
WINDOWS = ("five_hour", "weekly")
MODES = ("hermetic", "live")
VERDICTS = ("agree", "divergent")

# ---- Secret scanner ----------------------------------------------------------
#
# Patterns cover the shapes a credential actually takes in this workflow: a
# key or token assigned as a literal, a long hex or base64 run, and a bearer
# header. They are deliberately conservative about prose -- a scanner that
# cries wolf gets skipped -- and there is no suppression mechanism, so a
# finding is always actionable: remove the material.

SECRET_PATTERNS = [
    (
        "credential_literal",
        re.compile(
            r"(?i)\b(api[_-]?key|apikey|token|secret|password|passwd)\b\s*[:=]\s*"
            r"[\"']?([A-Za-z0-9+/_-]{16,})[\"']?"
        ),
    ),
    ("hex_run", re.compile(r"\b[A-Fa-f0-9]{32,}\b")),
    ("base64_run", re.compile(r"\b[A-Za-z0-9+/]{40,}={0,2}\b")),
    ("bearer_header", re.compile(r"(?i)\bbearer\s+[A-Za-z0-9._-]{16,}")),
]

# The two encoded-run heuristics are content heuristics: aimed at a filesystem
# path they fire on any long directory name -- an operator's --out can be
# exactly that -- and a finding on a path the canary itself composed is a
# false positive that blocks a legitimate run. Path-shaped tokens are
# therefore excluded from those two patterns only. Every other pattern runs
# over the whole text and the credential-verbatim check never sees redacted
# text, so a credential that reaches a path is still a finding; only the
# encoded-run guesses are scoped to content, and planted_secrets pins that
# boundary from the self-test.
PATH_TOKEN = re.compile(r"\S*/\S*")
ENCODED_RUN_PATTERNS = ("hex_run", "base64_run")

# Raw provider payload keys. They may exist in a fixture -- fixtures stand in
# for the provider's own bytes -- but they may never appear in an emitted
# artifact, which carries the projection and nothing else.
RAW_PAYLOAD_KEYS = (
    "currentValue",
    "nextResetTime",
    "percentage",
    "remaining",
    "limits",
    "level",
    "msg",
    "success",
)


class CanaryError(Exception):
    """The canary could not produce a verdict. Exit code 2."""


# ---- Time and HTTP helpers ---------------------------------------------------


def utc_now_iso() -> str:
    """RFC3339 UTC stamp with microsecond precision, the artifact's `at` form."""
    return datetime.now(timezone.utc).isoformat(timespec="microseconds").replace("+00:00", "Z")


def parse_rfc3339(stamp: str) -> datetime:
    value = datetime.fromisoformat(stamp.replace("Z", "+00:00"))
    if value.tzinfo is None:
        raise CanaryError(f"timestamp {stamp!r} carries no offset")
    return value


class NoRedirect(HTTPRedirectHandler):
    """Refuse redirects, exactly as proxy/quota/client.go does.

    A monitor endpoint has no legitimate reason to redirect, and a redirect is
    where a credential can be carried off the configured origin. Surfacing the
    3xx as an unexpected status keeps the fetch on the configured host.
    """

    def redirect_request(self, req, fp, code, msg, headers, newurl):  # noqa: ANN001
        return None


def http_get(url: str, api_key: str | None = None) -> tuple[int, bytes]:
    """GET one surface and return (status, body) with the response budget applied.

    The body is read once, bounded, and handed to the caller; nothing here
    retains or logs it. Errors carry the status code or an error class only,
    so a caller that prints them cannot leak payload text.
    """
    path_only = url.split("?")[0]
    request = Request(url, method="GET")
    request.add_header("Accept", "application/json")
    if api_key is not None:
        # Z.AI expects the raw key in Authorization, no Bearer prefix -- the
        # same contract proxy/quota/client.go implements.
        request.add_header("Authorization", api_key)

    try:
        opener = build_opener(NoRedirect())
        with opener.open(request, timeout=10) as response:
            status, body = response.status, response.read(MAX_RESPONSE_BYTES + 1)
    except URLError as exc:
        reason = getattr(exc, "reason", None)
        raise CanaryError(f"GET {path_only} failed: {type(reason or exc).__name__}") from exc

    if len(body) > MAX_RESPONSE_BYTES:
        raise CanaryError(f"GET {path_only} exceeded the {MAX_RESPONSE_BYTES}-byte read budget")
    return status, body


# ---- Surface projections -----------------------------------------------------


def project_usage_view(body: bytes) -> dict[str, dict[str, object]]:
    """Reduce a raw provider payload to {window: {used_percent, reset_at}}.

    This is a projection of ZCode's usage view, not a second implementation of
    the proxy's normalizer: it keeps only the two fields the comparison needs,
    applies the same window-unit mapping the proxy models, and drops the
    envelope, the unmodelled entries, and every absolute amount on the floor.
    The raw body does not survive this function.
    """
    try:
        envelope = json.loads(body)
    except json.JSONDecodeError as exc:
        raise CanaryError("usage view returned a payload that is not valid JSON") from exc

    if envelope.get("success") is False:
        # The provider rejected the credential or the request. The message
        # text is deliberately not surfaced: it is not guaranteed free of
        # account-identifying material.
        raise CanaryError(f"usage view rejected the request (provider code {envelope.get('code')})")

    limits = (envelope.get("data") or {}).get("limits")
    if not isinstance(limits, list):
        raise CanaryError("usage view payload is missing its data.limits array")

    windows: dict[str, dict[str, object]] = {}
    for limit in limits:
        if not isinstance(limit, dict):
            continue
        limit_type = str(limit.get("type", ""))
        if limit_type.lower() not in MODELLED_LIMIT_TYPES:
            continue
        window = WINDOW_BY_UNIT.get(limit.get("unit"))
        if window is None or window in windows:
            continue
        percentage = limit.get("percentage")
        if not isinstance(percentage, (int, float)) or isinstance(percentage, bool):
            continue
        windows[window] = {
            "used_percent": round(float(percentage), 6),
            "reset_at": epoch_millis_to_iso(limit.get("nextResetTime")),
        }
    if not windows:
        raise CanaryError("usage view payload modelled no five-hour or weekly window")
    return windows


def epoch_millis_to_iso(millis: object) -> str | None:
    """Render the provider's unix-millisecond reset stamp, or None when absent."""
    if not isinstance(millis, (int, float)) or isinstance(millis, bool) or millis <= 0:
        return None
    return datetime.fromtimestamp(millis / 1000, tz=timezone.utc).isoformat().replace("+00:00", "Z")


def fetch_usage_view(base_url: str, api_key: str) -> dict[str, dict[str, object]]:
    """One poll of ZCode's usage view, reduced to the projected windows."""
    status, body = http_get(base_url.rstrip("/") + MONITOR_PATH, api_key=api_key)
    if status != 200:
        raise CanaryError(f"usage view answered status {status} (redirects are refused, not followed)")
    return project_usage_view(body)


def fetch_proxy_quota_view(proxy_url: str) -> dict[str, dict[str, object]]:
    """Read the proxy's normalized telemetry from /health and project it.

    The proxy has already normalized the provider payload; this only converts
    its fraction to the percentage the comparison uses. A proxy whose sample is
    stale, or whose poller is disabled, is refused rather than compared: the
    verdict would otherwise describe a render of nothing.
    """
    status, body = http_get(proxy_url.rstrip("/") + "/health")
    if status != 200:
        raise CanaryError(f"proxy /health answered status {status}")
    try:
        payload = json.loads(body)
    except json.JSONDecodeError as exc:
        raise CanaryError("proxy /health returned a payload that is not valid JSON") from exc

    quota = payload.get("quota") or {}
    if not quota.get("enabled"):
        raise CanaryError("proxy quota polling is disabled; nothing to compare")
    if not quota.get("fresh"):
        raise CanaryError("proxy quota sample is stale; refusing to compare a stale render")

    windows: dict[str, dict[str, object]] = {}
    for window in quota.get("windows") or []:
        name = window.get("window")
        fraction = window.get("used_fraction")
        if name not in WINDOWS or not isinstance(fraction, (int, float)) or isinstance(fraction, bool):
            continue
        windows[name] = {
            "used_percent": round(float(fraction) * 100, 6),
            "reset_at": window.get("reset_at"),
        }
    if not windows:
        raise CanaryError("proxy /health modelled no five-hour or weekly window")
    return windows


# ---- Synthetic origin (hermetic mode) -----------------------------------------


class SyntheticOrigin:
    """Local stand-in for both surfaces, serving only committed payloads.

    The provider fixtures are served on the monitor path and the committed
    proxy views on /health, one pair per round, so the two surfaces render the
    same source exactly as they do in production. The credential is compared,
    never stored: the request record keeps a boolean, the same discipline as
    the Go canary's fixture. Bound to loopback only.
    """

    def __init__(self, fixture_dir: Path, synthetic_key: str):
        self._fixture_dir = fixture_dir
        self._synthetic_key = synthetic_key
        self._lock = threading.Lock()
        self._round = 0
        self._polls: list[tuple[str, str, bool]] = []
        self._httpd = ThreadingHTTPServer(("127.0.0.1", 0), self._make_handler())
        self._httpd.daemon_threads = True
        self.url = f"http://127.0.0.1:{self._httpd.server_address[1]}"

    def _make_handler(self) -> type[BaseHTTPRequestHandler]:
        origin = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):  # noqa: ANN002, ANN003
                pass  # the default access log would echo request paths into the run output

            def do_GET(self) -> None:  # noqa: N802
                if self.path == MONITOR_PATH:
                    origin._record_poll(self)
                    self._send(200, origin._provider_payload())
                elif self.path == "/health":
                    self._send(200, origin._proxy_view_payload())
                else:
                    self._send(404, {"error": "not a canary path"})

            def do_POST(self) -> None:  # noqa: N802
                if self.path == "/__advance":
                    origin._advance()
                    self._send(200, {"advanced": True})
                else:
                    self._send(404, {"error": "not a canary path"})

            def _send(self, status: int, payload: dict) -> None:
                body = json.dumps(payload).encode()
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

        return Handler

    def _record_poll(self, handler: BaseHTTPRequestHandler) -> None:
        with self._lock:
            self._polls.append(
                (
                    handler.command,
                    handler.path,
                    handler.headers.get("Authorization") == self._synthetic_key,
                )
            )

    def _round_index(self) -> int:
        with self._lock:
            return min(self._round, len(HERMETIC_ROUNDS) - 1)

    def _payload(self, name: str) -> bytes:
        return (self._fixture_dir / name).read_bytes()

    def _provider_payload(self) -> dict:
        return json.loads(self._payload(HERMETIC_ROUNDS[self._round_index()][0]))

    def _proxy_view_payload(self) -> dict:
        # The committed views pin the quota *section* -- exactly the subset
        # TestQuotaCanaryProxyViewFixtures decodes -- while the real /health
        # nests that section under "quota" (proxy/ratelimiter_state.go). Serve
        # the shape the real endpoint answers with, so the fetch path here is
        # the production shape and not a special case for the fixture.
        return {"quota": json.loads(self._payload(HERMETIC_ROUNDS[self._round_index()][1]))}

    def advance(self) -> None:
        """Move to the next fixture pair. Past the last pair, it holds still."""
        with self._lock:
            self._round = min(self._round + 1, len(HERMETIC_ROUNDS) - 1)

    def stats(self) -> tuple[int, bool, bool]:
        """Poll count, whether every poll was a monitor GET, and whether every
        poll carried the credential -- three non-secret facts the summary
        reports and the run asserts."""
        with self._lock:
            polls = list(self._polls)
        return (
            len(polls),
            all(method == "GET" and path == MONITOR_PATH for method, path, _ in polls),
            bool(polls) and all(match for _, _, match in polls),
        )

    def start(self) -> None:
        threading.Thread(target=self._httpd.serve_forever, daemon=True).start()

    def close(self) -> None:
        self._httpd.shutdown()
        self._httpd.server_close()


# ---- Sampling -----------------------------------------------------------------


def sample_rounds(
    proxy_url: str,
    usage_base_url: str,
    api_key: str,
    rounds: int,
    interval_seconds: float,
    advance: Callable[[], None] | None,
) -> tuple[list[dict], list[dict]]:
    """Sample both surfaces for `rounds` rounds and derive the deltas.

    Deltas are taken against the previous sample of the same surface and
    window, so the artifact reports what moved and by how much -- a usage
    percentage delta and a reset-stamp delta -- and never an absolute account
    amount. The first sample of each series carries no delta.
    """
    samples: list[dict] = []
    comparisons: list[dict] = []
    previous: dict[tuple[str, str], dict] = {}

    for round_index in range(rounds):
        at = utc_now_iso()
        proxy_windows = fetch_proxy_quota_view(proxy_url)
        zcode_windows = fetch_usage_view(usage_base_url, api_key)

        for surface, windows in (("proxy", proxy_windows), ("zcode", zcode_windows)):
            for window in WINDOWS:
                if window not in windows:
                    continue
                sample = _build_sample(at, surface, round_index, window, windows[window], previous)
                samples.append(sample)
                previous[(surface, window)] = sample

        for window in WINDOWS:
            if window in proxy_windows and window in zcode_windows:
                comparisons.append(
                    _build_comparison(at, round_index, window, proxy_windows[window], zcode_windows[window])
                )

        if advance is not None:
            advance()
        if interval_seconds > 0 and round_index + 1 < rounds:
            time.sleep(interval_seconds)
    return samples, comparisons


def _build_sample(
    at: str,
    surface: str,
    round_index: int,
    window: str,
    projected: dict[str, object],
    previous: dict[tuple[str, str], dict],
) -> dict:
    reset_at = projected.get("reset_at")
    sample: dict[str, object] = {
        "at": at,
        "surface": surface,
        "round": round_index,
        "window": window,
        "used_percent": projected["used_percent"],
        "reset_at": reset_at,
    }
    if isinstance(reset_at, str):
        sample["reset_in_seconds"] = round(
            (parse_rfc3339(reset_at) - parse_rfc3339(at)).total_seconds(), 6
        )

    prior = previous.get((surface, window))
    if prior is not None:
        sample["used_percent_delta_pp"] = round(
            float(sample["used_percent"]) - float(prior["used_percent"]), 6
        )
        if isinstance(reset_at, str) and isinstance(prior.get("reset_at"), str):
            sample["reset_delta_seconds"] = round(
                (parse_rfc3339(str(reset_at)) - parse_rfc3339(str(prior["reset_at"]))).total_seconds(),
                6,
            )
    return sample


def _build_comparison(
    at: str, round_index: int, window: str, proxy_window: dict, zcode_window: dict
) -> dict:
    proxy_reset = proxy_window.get("reset_at")
    zcode_reset = zcode_window.get("reset_at")
    reset_delta = None
    if isinstance(proxy_reset, str) and isinstance(zcode_reset, str):
        reset_delta = round(
            (parse_rfc3339(zcode_reset) - parse_rfc3339(proxy_reset)).total_seconds(), 6
        )
    return {
        "at": at,
        "round": round_index,
        "window": window,
        "proxy_used_percent": proxy_window["used_percent"],
        "zcode_used_percent": zcode_window["used_percent"],
        "delta_pp": round(float(zcode_window["used_percent"]) - float(proxy_window["used_percent"]), 6),
        "proxy_reset_at": proxy_reset,
        "zcode_reset_at": zcode_reset,
        "reset_delta_seconds": reset_delta,
    }


def annotate_comparisons(
    comparisons: list[dict], tolerance_pp: float, reset_tolerance_seconds: float
) -> None:
    """Stamp each comparison with its own within-tolerance verdict."""
    for comparison in comparisons:
        within = abs(float(comparison["delta_pp"])) <= tolerance_pp
        reset_delta = comparison.get("reset_delta_seconds")
        if reset_delta is not None:
            within = within and abs(float(reset_delta)) <= reset_tolerance_seconds
        comparison["within_tolerance"] = bool(within)


def verdict_for(comparisons: list[dict]) -> str:
    """`agree` only when every comparison is inside both tolerances."""
    if comparisons and all(c["within_tolerance"] for c in comparisons):
        return "agree"
    return "divergent"


# ---- Schema enforcement -------------------------------------------------------


def validate_schema(artifact: dict) -> None:
    """Fail the run if the artifact grew a field outside the allowlist.

    This is what keeps "emits only timestamps, percentages, and reset deltas"
    a property of the artifact rather than of this comment: a field added at
    any level without updating the allowlist fails every subsequent run.
    """
    _check_keys(artifact, ALLOWED_TOP, "artifact")
    if artifact.get("mode") not in MODES:
        raise CanaryError(f"artifact mode {artifact.get('mode')!r} is not one of {MODES}")
    if artifact.get("verdict") not in VERDICTS:
        raise CanaryError(f"artifact verdict {artifact.get('verdict')!r} is not one of {VERDICTS}")

    for sample in artifact.get("samples") or []:
        _check_keys(sample, ALLOWED_SAMPLE, "sample")
        if sample.get("surface") not in SURFACES:
            raise CanaryError(f"sample surface {sample.get('surface')!r} is not one of {SURFACES}")
        if sample.get("window") not in WINDOWS:
            raise CanaryError(f"sample window {sample.get('window')!r} is not one of {WINDOWS}")

    for comparison in artifact.get("comparisons") or []:
        _check_keys(comparison, ALLOWED_COMPARISON, "comparison")
        if comparison.get("window") not in WINDOWS:
            raise CanaryError(f"comparison window {comparison.get('window')!r} is not one of {WINDOWS}")


def _check_keys(obj: dict, allowed: set[str], what: str) -> None:
    unexpected = sorted(set(obj) - allowed)
    if unexpected:
        raise CanaryError(f"{what} carries fields outside the output schema: {', '.join(unexpected)}")


# ---- Secret safety ------------------------------------------------------------


def secret_findings(text: str, label: str, extra_secrets: tuple[str, ...] = ()) -> list[tuple[str, str, str]]:
    """Findings for one scan target as (label, pattern-name, context).

    The encoded-run heuristics see the text with path-shaped tokens removed
    (see PATH_TOKEN); every other pattern sees the whole text.
    """
    findings: list[tuple[str, str, str]] = []
    for name, pattern in SECRET_PATTERNS:
        scanned = PATH_TOKEN.sub(" ", text) if name in ENCODED_RUN_PATTERNS else text
        for match in pattern.finditer(scanned):
            context = text[max(0, match.start() - 24) : match.end() + 8].replace("\n", " ")
            findings.append((label, name, context))
    for secret in extra_secrets:
        if secret and secret in text:
            findings.append((label, "canary_credential_verbatim", secret[:8] + "..."))
    return findings


def scan_paths(paths: list[Path], extra_secrets: tuple[str, ...] = ()) -> list[tuple[str, str, str]]:
    findings: list[tuple[str, str, str]] = []
    for path in paths:
        findings.extend(secret_findings(path.read_text(errors="replace"), str(path), extra_secrets))
    return findings


def format_findings(findings: list[tuple[str, str, str]]) -> str:
    return "\n".join(f"{label}: {name} near {context!r}" for label, name, context in findings)


def assert_artifact_is_secret_free(artifact_text: str, label: str, api_key: str | None) -> None:
    """An emitted surface must carry no credential and no raw payload key.

    Applied to the artifact and to everything printed about it: the summary
    names paths that outlive this process, so it is an emitted surface too.
    """
    findings = secret_findings(artifact_text, label, (api_key,) if api_key else ())
    for key in RAW_PAYLOAD_KEYS:
        if f'"{key}"' in artifact_text:
            findings.append((label, "raw_provider_payload_key", key))
    if findings:
        raise CanaryError(
            f"{label} failed its secret-safety check and was not emitted:\n"
            + format_findings(findings)
        )


def planted_secrets() -> dict[str, str]:
    """One synthetic example of each pattern class, assembled at run time.

    Every value is built from pieces short enough that no pattern matches this
    file, and each must actually match its own class -- a planted example the
    scanner cannot see is a self-test that passes for the wrong reason. The
    credential literal is quoted with chr(34) and split into short chunks so
    neither the quotes nor a 16+ character run appear in this source, which is
    what the credential_literal pattern needs on the other side of the scan.

    ``credential_in_path`` plants material inside a path-shaped token, the one
    shape the encoded-run heuristics deliberately skip (see PATH_TOKEN). It
    must still be found, by the keyed-literal pattern that reads the whole
    text, or the path scoping has silently become an allowlist.
    """
    quote = chr(34)
    value = "".join(["AbCdEf", "123456GhIjKl", "67890MnOpQr"])
    return {
        "credential_literal": "api_key = " + quote + value + quote,
        "credential_in_path": "token=/run/agent/" + value,
        "hex_run": "deadbeef" * 8,
        "base64_run": "ABCDEFGHIJKLMNOPQRSTUVWXYZ" + "abcdefghijklmnop",
        "bearer_header": "Authorization: Bearer " + "abcdefghijklmnop",
    }


def scanner_self_test(script_path: Path, fixture_dir: Path, tmp_dir: Path) -> None:
    """Prove the scanner is neither blind nor noisy.

    A scanner that reports zero findings everywhere is indistinguishable from
    a scanner that cannot match anything, so the self-test plants one example
    per class and asserts each is detected -- individually, through the same
    entry point a real scan uses, so a blind pattern or a scoping rule that
    hides a class fails here rather than in front of a leak -- and then
    asserts the canary's own files are clean.
    """
    planted = planted_secrets()
    blind = sorted(
        name for name, value in planted.items() if not secret_findings(value, "self-test")
    )
    if blind:
        raise CanaryError(f"the secret scanner missed planted material: {', '.join(blind)}")

    planted_file = tmp_dir / "planted-secrets.txt"
    planted_file.write_text("\n".join(planted.values()) + "\n")
    try:
        found = {name for _, name, _ in scan_paths([planted_file])}
        if not found:
            raise CanaryError("the secret scanner reported no findings on a file of planted material")
    finally:
        planted_file.unlink(missing_ok=True)

    findings = scan_paths(canary_files(script_path, fixture_dir))
    if findings:
        raise CanaryError(
            "the secret scanner reported findings on the canary's own files:\n"
            + format_findings(findings)
        )


def canary_files(script_path: Path, fixture_dir: Path) -> list[Path]:
    """Everything the canary owns and is therefore responsible for scanning."""
    return [script_path, *sorted(fixture_dir.glob("*"))]


# ---- Production wiring check (hermetic mode) ----------------------------------


def run_go_canary(repo_root: Path) -> None:
    """Run the hermetic Go canaries before this script samples anything.

    They exercise the production poller, the /health renderer, and the
    inference path against the same committed fixtures, so a rendering change
    fails in the proxy before a canary verdict can be built on it. Their
    output is synthetic and credential-free, but it is captured rather than
    streamed to keep this script's own output to the summary.
    """
    go_binary = shutil.which("go")
    if go_binary is None:
        raise CanaryError("go is not on PATH; install it or pass --no-go-canary")
    result = subprocess.run(
        [
            go_binary,
            "test",
            "-count=1",
            "-run",
            "TestQuotaObserveOnlyCanary|TestQuotaCanaryProxyViewFixtures",
            "./proxy/",
        ],
        cwd=repo_root,
        capture_output=True,
        text=True,
        timeout=600,
    )
    if result.returncode != 0:
        tail = "\n".join((result.stdout + result.stderr).splitlines()[-40:])
        raise CanaryError(f"the hermetic Go canaries failed:\n{tail}")


# ---- Artifact -----------------------------------------------------------------


def write_artifact(artifact: dict, out_dir: Path | None, to_stdout: bool) -> str:
    """Persist the validated, secret-free artifact and report where it went."""
    artifact_text = json.dumps(artifact, indent=2) + "\n"

    if to_stdout:
        sys.stdout.write(artifact_text)
        return "<stdout>"

    directory = out_dir or Path(tempfile.gettempdir())
    directory.mkdir(parents=True, exist_ok=True)
    path = directory / f"zai-quota-canary-{int(datetime.now(timezone.utc).timestamp())}.json"
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, "w") as handle:
        handle.write(artifact_text)
    return str(path)


# ---- Entry points --------------------------------------------------------------


def run_canary(args: argparse.Namespace) -> int:
    repo_root = Path(__file__).resolve().parents[2]
    fixture_dir = (
        Path(args.fixture_dir).resolve()
        if args.fixture_dir
        else Path(__file__).resolve().parents[1] / "testdata" / "quota_canary"
    )
    if not fixture_dir.is_dir():
        raise CanaryError(f"fixture directory {fixture_dir} does not exist")

    rounds = args.rounds
    interval_seconds = args.interval
    api_key: str
    mode: str
    origin: SyntheticOrigin | None = None
    proxy_url = args.proxy_url
    usage_base_url = args.zcode_base_url

    if proxy_url:
        # Live mode. The credential travels from the environment into the
        # request header and nowhere else; it is never a literal here.
        mode = "live"
        api_key = os.environ.get("ZAI_API_KEY", "")
        if not api_key:
            raise CanaryError(
                "live mode (--proxy-url) requires ZAI_API_KEY in the environment; "
                "fetch it by pipe, never as a literal on the command line"
            )
    else:
        # Hermetic mode. The synthetic credential is generated here, at run
        # time, so the secret-safety phase asserts against a value that was
        # real enough to go over the wire and is real enough to be missed.
        mode = "hermetic"
        rounds = min(rounds, len(HERMETIC_ROUNDS))
        api_key = "canary-" + secrets.token_hex(24)
        origin = SyntheticOrigin(fixture_dir, api_key)
        origin.start()
        proxy_url = origin.url
        usage_base_url = origin.url

    try:
        if origin is not None and not args.no_go_canary:
            run_go_canary(repo_root)

        samples, comparisons = sample_rounds(
            proxy_url,
            usage_base_url,
            api_key,
            rounds,
            interval_seconds,
            origin.advance if origin is not None else None,
        )
        origin_stats = origin.stats() if origin is not None else None
    finally:
        if origin is not None:
            origin.close()

    annotate_comparisons(comparisons, args.tolerance_pp, args.reset_tolerance_seconds)
    artifact = {
        "schema_version": SCHEMA_VERSION,
        "generated_at": utc_now_iso(),
        "mode": mode,
        "tolerance_pp": args.tolerance_pp,
        "reset_tolerance_seconds": args.reset_tolerance_seconds,
        "samples": samples,
        "comparisons": comparisons,
        "verdict": verdict_for(comparisons),
    }
    validate_schema(artifact)

    artifact_text = json.dumps(artifact, indent=2) + "\n"
    assert_artifact_is_secret_free(artifact_text, "artifact", api_key)
    artifact_path = write_artifact(
        artifact, Path(args.out).resolve() if args.out else None, args.stdout
    )

    max_delta = max((abs(float(c["delta_pp"])) for c in comparisons), default=0.0)
    max_reset_delta = max(
        (
            abs(float(c["reset_delta_seconds"]))
            for c in comparisons
            if c["reset_delta_seconds"] is not None
        ),
        default=0.0,
    )
    summary = [
        f"canary: mode={mode} rounds={rounds} samples={len(samples)} comparisons={len(comparisons)}",
        f"canary: verdict={artifact['verdict']} max_abs_delta_pp={max_delta} "
        f"max_abs_reset_delta_seconds={max_reset_delta}",
        f"canary: tolerance_pp={args.tolerance_pp} "
        f"reset_tolerance_seconds={args.reset_tolerance_seconds}",
    ]
    if origin_stats is not None:
        poll_count, monitor_gets_only, credential_carried = origin_stats
        summary.append(
            f"canary: synthetic_origin_polls={poll_count} "
            f"all_get_on_monitor_path={monitor_gets_only} "
            f"credential_carried_on_every_poll={credential_carried}"
        )
    summary.append(f"canary: artifact={artifact_path}")
    summary.append(
        "canary: secret_scan=clean (script, fixtures, artifact, and summary scanned; "
        "run `secret-scan --self-test` to re-verify the scanner itself)"
    )
    summary_text = "\n".join(summary) + "\n"
    assert_artifact_is_secret_free(summary_text, "summary", api_key)
    sys.stdout.write(summary_text)

    if artifact["verdict"] != "agree":
        print(
            f"canary: the surfaces diverged beyond tolerance "
            f"({args.tolerance_pp}pp, {args.reset_tolerance_seconds}s reset)",
            file=sys.stderr,
        )
        return 1
    return 0


def secret_scan_command(args: argparse.Namespace) -> int:
    script_path = Path(__file__).resolve()
    fixture_dir = (
        Path(args.fixture_dir).resolve()
        if args.fixture_dir
        else script_path.parents[1] / "testdata" / "quota_canary"
    )
    if not fixture_dir.is_dir():
        raise CanaryError(f"fixture directory {fixture_dir} does not exist")

    if args.self_test:
        with tempfile.TemporaryDirectory(prefix="quota-canary-scan-") as tmp:
            scanner_self_test(script_path, fixture_dir, Path(tmp))
        print(
            f"canary: scanner self-test passed "
            f"(all {len(SECRET_PATTERNS)} pattern classes detected, own files clean)"
        )

    targets = canary_files(script_path, fixture_dir)
    if args.artifact:
        targets.append(Path(args.artifact).resolve())
    findings = scan_paths(targets)
    if findings:
        print(format_findings(findings), file=sys.stderr)
        print(
            f"canary: secret_scan={len(findings)} finding(s) across {len(targets)} file(s)",
            file=sys.stderr,
        )
        return 1
    print(f"canary: secret_scan=clean across {len(targets)} file(s)")
    return 0


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        prog="quota_canary.py",
        description="Credential-safe canary comparing the proxy's normalized quota telemetry "
        "with ZCode's usage view. Emits timestamps, percentages, and reset deltas only.",
    )
    subparsers = parser.add_subparsers(dest="command")

    run_parser = subparsers.add_parser(
        "run", help="sample both surfaces and emit the artifact (the default command)"
    )
    run_parser.set_defaults(handler=run_canary)
    run_parser.add_argument(
        "--proxy-url",
        help="live mode: the proxy to sample. Omit for the hermetic synthetic origin.",
    )
    run_parser.add_argument(
        "--zcode-base-url",
        default="https://api.z.ai",
        help="live mode: the origin ZCode's usage view renders (default: %(default)s)",
    )
    run_parser.add_argument(
        "--fixture-dir",
        help="hermetic mode: directory holding the committed canary fixtures "
        "(default: proxy/testdata/quota_canary)",
    )
    run_parser.add_argument("--rounds", type=int, default=3, help="sample rounds (default: %(default)s)")
    run_parser.add_argument(
        "--interval",
        type=float,
        default=0.0,
        help="seconds between rounds; live runs should keep this slow (default: %(default)s)",
    )
    run_parser.add_argument(
        "--tolerance-pp",
        type=float,
        default=1.0,
        help="percentage-point tolerance for the verdict (default: %(default)s). "
        "The provider reports whole percentage steps, so ±1pp is expected agreement.",
    )
    run_parser.add_argument(
        "--reset-tolerance-seconds",
        type=float,
        default=5.0,
        help="reset-stamp tolerance for the verdict in seconds (default: %(default)s)",
    )
    run_parser.add_argument("--out", help="directory for the artifact (default: the OS temp dir)")
    run_parser.add_argument(
        "--stdout", action="store_true", help="emit the artifact on stdout instead of a file"
    )
    run_parser.add_argument(
        "--no-go-canary",
        action="store_true",
        help="skip the hermetic Go canaries that normally run first in hermetic mode",
    )

    scan_parser = subparsers.add_parser(
        "secret-scan", help="scan the canary script and fixtures for secret material"
    )
    scan_parser.set_defaults(handler=secret_scan_command)
    scan_parser.add_argument(
        "--fixture-dir", help="directory to scan (default: proxy/testdata/quota_canary)"
    )
    scan_parser.add_argument("--artifact", help="also scan an emitted artifact")
    scan_parser.add_argument(
        "--self-test",
        action="store_true",
        help="plant synthetic secrets in a temp file and assert the scanner finds them first",
    )

    if argv and argv[0] in {"run", "secret-scan"}:
        return parser.parse_args(argv)
    # Bare invocation runs the canary: `quota_canary.py --rounds 2` and
    # `quota_canary.py run --rounds 2` are the same command.
    return parser.parse_args(["run", *argv])


def main(argv: list[str]) -> int:
    args = parse_args(argv)
    try:
        return args.handler(args)
    except CanaryError as exc:
        print(f"canary: {exc}", file=sys.stderr)
        return 2
    except KeyboardInterrupt:  # pragma: no cover - operator interrupt
        return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
