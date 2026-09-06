# Z.AI Proxy Quota Observability (observe-only)

## Status and scope

Out-of-band quota polling is **observe-only**. It publishes Z.AI's account-level
Coding Plan quota windows to `/health` and `/metrics` and does nothing else:
no request is admitted, rejected, delayed, or re-prioritized because of it.
Enforcement — the `quota_rate_cap` / `quota_gate_open` path in
`docs/plan/plan.md` ("Quota-aware throttling plan") — is a separately gated
change and is not wired anywhere in this build.

This document is the operations record for that telemetry: how to check it
hermetically, how to run the credential-safe live canary, what the canary
measured, and how to diff the proxy's numbers against ZCode's usage view.

## Reproducing every claim in this document

Run from the repo root. Steps 1–4 are hermetic: no credential, no
non-loopback traffic. Step 5 is the only one that touches the real origin,
and it takes the credential by pipe. The sections that follow explain what
each step pins down; this is the order to run them in.

```bash
# 1. Module green, quota surface green under race.
go test -count=1 -skip TestConcurrentLoad ./...
go test -race -count=2 ./proxy/quota/
go test -race -count=1 -run Quota ./proxy/
```

Expected: every package `ok`. `TestConcurrentLoad` is skipped because it is
the load-bound flake under "Known non-quota failures", not a quota test;
`TestLiveQuotaObservationCanary` reports `SKIP` here because `QUOTA_CANARY`
is unset, which is the correct result until step 5.

```bash
# 2. The failure-injection set behind "failures do not affect inference",
#    named individually so a renamed or deleted test fails the step.
go test -count=1 -race -v -run \
  'TestQuotaPollerKeepsLastKnownGoodAcrossFailures|TestQuotaPollerTimeoutDoesNotBlockAdmission|TestQuotaPollerClassifiesProviderRejectionAndTransportFailure|TestQuotaPollerClassifiesMalformedPayload|TestQuotaPollerNeverLogsCredential|TestQuotaDecisionMetrics|TestQuotaExhaustionIsRecordedButEnforcesNothing|TestQuotaSignalPhasesNeverTriggerEnforcement|TestQuotaEnforcementRecordersHaveNoLiveCallSite' \
  ./proxy/
```

Expected: nine `PASS` lines and
`ok git.ardenone.com/jedarden/zai-proxy/proxy`.

```bash
# 3. Enforcement still off — checks 1 and 2 of "Verifying enforcement is
#    off", whose expected outputs are spelled out there. Check 3 needs a
#    running instance and greps its /metrics.
grep -rn "RecordQuotaRateCap\|RecordQuotaGateOpen" --include='*.go' . | grep -v '_test.go'
grep -rn "quotaPoller\|QuotaPoller" proxy/*.go | grep -v '_test.go'
```

```bash
# 4. The canary script end to end, hermetically.
python3 proxy/scripts/quota_canary.py run
python3 proxy/scripts/quota_canary.py secret-scan --self-test
```

Expected from the first: `mode=hermetic`, `verdict=agree`, both deltas
`0.0`, `credential_carried_on_every_poll=True`, and `secret_scan=clean`
over the script, the fixtures, and the emitted artifact. Expected from the
second: all four pattern classes detected on planted material, the canary's
own files clean.

```bash
# 5. The live canary — the only step that needs a credential, fetched by
#    pipe into the test process's environment. Never a literal anywhere.
mkdir -p /tmp/quota-canary && chmod 700 /tmp/quota-canary
ZAI_API_KEY="$(bao-as rs-manager bao kv get -field=api-key secret/rs-manager/apexalgo-iad/mcp/zai/api-key)" \
QUOTA_CANARY=true QUOTA_CANARY_OUT=/tmp/quota-canary \
  go test -count=1 -run TestLiveQuotaObservationCanary -v -timeout 9m ./proxy/
```

Expected: three phases, every poll `success`, the sample `fresh`
throughout, `zai_proxy_requests_total` still 0 when it finishes — the test
fails if any model request was sent — and one normalized report under
`/tmp/quota-canary/`.

Record what comes out using only the fields listed under "Fields safe to
record" below.

Re-verified green on 2026-09-06 at commit `84226e5`, the module tests run
under `go test -overlay` so that unrelated in-flight sibling edits in this
shared checkout were held out of the compile: steps 1 and 2 exactly as
printed (all packages `ok`; nine `PASS` lines), step 3's greps 1 and 2
producing exactly the expected output — its check 3 needs a running instance
and was not run here — and step 4 with `verdict=agree`, both deltas `0.0`,
and `secret_scan=clean`. Step 5 was not re-run for this document; its most
recent run is the recorded evidence in the next two sections.

## The two surfaces

Both are views of one retained snapshot — the last successful poll. There is
no third source, and no interpolation.

`GET /health` → `"quota"` object:

| Field | Meaning |
|---|---|
| `enabled` | Always true; a disabled poller renders as `{}` (absent fields), so the flag taking effect is visible in the payload |
| `fresh` | False when there has never been a successful poll or the sample is older than `QUOTA_STALE_AFTER` |
| `interval`, `stale_after` | The poller's own configuration |
| `last_outcome` | `success`, `error`, `malformed`, or `stale` |
| `last_success_at`, `sample_age_seconds` | Present only once a sample exists |
| `plan_tier` | Provider-reported plan level (e.g. `max`, `pro`) — plan metadata, not an account identifier |
| `windows[]` | `window`, `limit_type`, `used_fraction`, `reset_at` |

`GET /metrics` → the `zai_proxy_quota_*` families:

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `zai_proxy_quota_usage_ratio` | gauge | `window`, `limit_type`, `variant` | Provider-reported usage, clamped to [0, 1] by `clampUnit` (`proxy/quota/schema.go`), so an overdrawn window reads exactly 1 — the raw >1 reading the provider sent is not recoverable from the metric |
| `zai_proxy_quota_remaining_ratio` | gauge | `window`, `limit_type`, `variant` | `1 - usage_ratio`, no local policy adjustment |
| `zai_proxy_quota_reset_time_seconds` | gauge | `window`, `limit_type`, `variant` | Provider reset time as a Unix timestamp |
| `zai_proxy_quota_sample_age_seconds` | gauge | `variant` | Age of the last valid sample |
| `zai_proxy_quota_poll_total` | counter | `result`, `variant` | Poll outcomes: `success`, `error`, `malformed`, `stale` |
| `zai_proxy_quota_rate_cap` | gauge | `variant` | **Not produced.** Enforcement metric, no live writer |
| `zai_proxy_quota_gate_open` | gauge | `window`, `variant` | **Not produced.** Enforcement metric, no live writer |

Label values are bounded to a fixed vocabulary (`quotaWindowLabel`,
`quotaPollResultLabel`, `sanitizeQuotaLabel` in `proxy/metrics.go`), so an
unexpected provider string cannot widen the series.

## Verifying enforcement is off

Three independent checks. All three should stay true; any one of them
flipping is the signal that a throttling change landed.

```bash
# 1. The enforcement recorders have no live call site.
#    Expected output: only the definitions in metrics.go, plus _test.go hits.
grep -rn "RecordQuotaRateCap\|RecordQuotaGateOpen" --include='*.go' . | grep -v '_test.go'

# 2. The poller's only consumer is the /health renderer. Expected: main.go's
#    newHealthHandler call and quota_poller.go / ratelimiter_state.go's own
#    definitions — nothing in handler.go, retry.go, or the rate limiter.
grep -rn "quotaPoller\|QuotaPoller" proxy/*.go | grep -v '_test.go'

# 3. No enforcement series is exported on a running instance.
#    Expected: both greps find nothing.
curl -s http://127.0.0.1:8080/metrics | grep -E '^zai_proxy_quota_(rate_cap|gate_open)'
```

Production additionally does not run the poller at all: the `zai-proxy`
Deployment in `declarative-config` sets no `QUOTA_*` variable, and
`QUOTA_POLL_ENABLED` defaults to false (`proxy/config/config.go`).

## Hermetic checks

No test touches the network by default; the quota client is exercised against
`httptest` fixtures in `proxy/quota/testdata/`.

```bash
# Whole module. Expected: all packages ok, except TestConcurrentLoad, which is
# a load-bound latency benchmark (5ms target) that fails on a busy shared
# checkout for reasons unrelated to quota. See "Known non-quota failures".
go test -count=1 ./...

# The quota surface under race.
go test -race -count=2 ./proxy/quota/
go test -race -count=1 -run Quota ./proxy/

# Failure isolation, staleness, and credential hygiene.
go test -count=1 -race -v -run \
  'TestQuotaPollerKeepsLastKnownGoodAcrossFailures|TestQuotaPollerTimeoutDoesNotBlockAdmission|TestQuotaPollerClassifiesProviderRejectionAndTransportFailure|TestQuotaPollerClassifiesMalformedPayload|TestQuotaPollerNeverLogsCredential|TestQuotaDecisionMetrics' \
  ./proxy/
```

What those tests pin down:

- A failed, malformed, timed-out, or oversized poll keeps the last-known-good
  sample and counts exactly one outcome — it never clears the retained state
  (`TestQuotaPollerKeepsLastKnownGoodAcrossFailures`).
- A poll that takes its whole timeout does not block or delay admission
  (`TestQuotaPollerTimeoutDoesNotBlockAdmission`).
- The poller never logs the credential, and a provider rejection is logged by
  numeric code only, never by message text
  (`TestQuotaPollerNeverLogsCredential`).
- Enforcement gauges stay unmoved across healthy, failing, stale, and
  recovered phases (`TestQuotaDecisionMetrics`).
- The normalization itself is pinned from committed provider payloads
  (`proxy/testdata/quota_telemetry/`) through the production client, poller,
  and both surfaces (`TestQuotaTelemetryRendersFixturesOnBothSurfaces` in
  `proxy/quota_telemetry_observeonly_test.go`): each window's limit type, used
  fraction, and reset stamp are exact — RFC3339 on `/health`, Unix seconds on
  `/metrics` — a window that advertises no reset renders no reset telemetry
  rather than an epoch, and a window the proxy does not model (`TIME_LIMIT`)
  reaches neither surface.
- Exhaustion is an observation, not an action:
  `TestQuotaExhaustionIsRecordedButEnforcesNothing` records an overdrawn
  window as exactly 1 while the limiter, its health state, admission timing,
  and both enforcement gauges stay at baseline;
  `TestQuotaSignalPhasesNeverTriggerEnforcement` walks healthy → exhausted →
  stale outage → recovery with the same null result in every phase; and
  `TestQuotaEnforcementRecordersHaveNoLiveCallSite` fails the run the moment
  either recorder gains a call site outside its own definition in
  `metrics.go`.

## Live canary

`TestLiveQuotaObservationCanary` (`proxy/quota_canary_live_test.go`) drives the
production poller against the real quota origin in three phases — a slow
cadence, a rapid burst of back-to-back polls, and a second slow cadence — and
writes a normalized, secret-free report.

**Skipped unless both variables are set**, so plain `go test ./...` never runs
it.

```bash
mkdir -p /tmp/quota-canary && chmod 700 /tmp/quota-canary

# The credential is fetched by pipe into the test process's environment. It is
# never a literal here, never written to disk, and never printed; the report
# is checked for it before being written.
ZAI_API_KEY="$(bao-as rs-manager bao kv get -field=api-key secret/rs-manager/apexalgo-iad/mcp/zai/api-key)" \
QUOTA_CANARY=true QUOTA_CANARY_OUT=/tmp/quota-canary \
  go test -count=1 -run TestLiveQuotaObservationCanary -v -timeout 9m ./proxy/
```

The run makes about 55 monitor GETs over roughly two minutes and sends **zero**
model requests — the canary constructs the poller only, never a proxy handler,
and asserts `zai_proxy_requests_total` is still zero when it finishes.

What the report (`/tmp/quota-canary/zai-quota-canary-<epoch>.json`, mode 600)
holds, and what it deliberately does not:

- Per-sample timestamps, `last_outcome`, sample age, plan tier, per-window
  `used_fraction` and `reset_at`, and the `/metrics` gauge values for the same
  snapshot.
- Phase-boundary absolute credit amounts (`used`, `limit`, `remaining`) read
  from the normalized snapshot — the numbers ZCode's usage view shows.
- Derived analysis: burn rate per cadence phase, the burst window's delta
  against the drift the cadence rate predicts, reset-time deltas, and the
  `/health` vs `/metrics` maximum divergence.
- **Not** the credential, **not** any raw response payload, and **not** any
  account identifier. The test fails rather than writing the report if the
  credential appears in it.

### Zero-burn: what is being tested and what it can resolve

The monitor endpoint is only proven unmetered if polling is shown not to move
the counters while something else demonstrably is moving them. The burst phase
is the instrument: 40 polls back-to-back, compared against the drift the
adjacent cadence windows show.

The verdict is one of three, and the middle one is the honest outcome when the
numbers cannot decide:

- `confirmed` — the burst window's usage delta did not exceed the cadence
  windows' drift;
- `unresolved: burst excess is below provider reporting granularity` — the
  excess was positive but smaller than 0.001 of the window (0.1%), which the
  provider's coarse percentage steps cannot resolve;
- `falsified` — the burst outburned the predicted drift by more than a step.

## Recorded canary evidence

Run 2026-09-05 22:29–22:31 UTC against `https://api.z.ai`, plan tier `max`,
poller configured interval `1m0s`, stale-after `15m0s`. 55 endpoint polls, 0
model requests, every poll `success`, sample `fresh` throughout.

Only the five-hour window was present in this account's payload, and it used
the legacy `TOKENS_LIMIT` schema — percentage-only, so absolute amounts are
zero with `has_usage` false by design.

| Time (UTC) | Phase | five-hour used | reset_at | reset_in |
|---|---|---|---|---|
| 22:29:42.52 | baseline first | 12.0000% | 2026-09-05T22:51:32Z | 0.34 h |
| 22:30:53.92 | baseline last | **13.0000%** | 2026-09-05T22:51:32Z | 0.34 h |
| 22:30:54.11 | credit reading | 13.0000% | 2026-09-05T22:51:32Z | 0.34 h |
| 22:31:01.98 | burst (40 polls) | 13.0000% | 2026-09-05T22:51:32Z | 0.34 h |
| 22:31:32.78 | post-burst last | 13.0000% | 2026-09-05T22:51:32Z | 0.33 h |

Analysis:

| Quantity | Value |
|---|---|
| `/health` vs `/metrics` maximum divergence | 0 (exactly equal, every sample) |
| Baseline burn rate | 1.4 × 10⁻⁴ usage-fraction/second |
| Post-burst burn rate | 0 |
| Burst window | 7.68 s, 40 polls |
| Burst usage delta | 0 |
| Drift the cadence rate predicts for that window | 1.08 × 10⁻³ |
| Burst excess | −1.08 × 10⁻³ |
| Reset stamp drift across the whole run | 0 (only `reset_in` decayed, by wall clock) |

**Zero-burn: confirmed.** The account was demonstrably burning during the run
— one full percentage point of the five-hour window between 22:29:42 and
22:30:53, from the fleet's parallel ZCode campaign activity — and 40
back-to-back monitor polls in 7.7 s still moved the counter by exactly zero.
The reset stamp held constant across every reading while its countdown decayed
by the wall clock, so polling does not disturb the window either.

**Failures do not affect inference: confirmed** on two independent grounds.
Structurally, the poller's only consumer is the `/health` renderer and the
enforcement recorders have no live call site (see "Verifying enforcement is
off"). Behaviourally, the hermetic suite pins that every failure mode retains
the last-known-good sample, counts one bounded outcome, and leaves admission
untouched. Production runs neither the poller nor any `QUOTA_*` setting.

## Recorded canary evidence — 2026-09-06

Three live runs and one paired usage-view probe, all against `https://api.z.ai`,
plan tier `max`, poller interval `1m0s`, stale-after `15m0s`. Every run: all
polls `success`, sample `fresh` throughout, `/health` vs `/metrics` maximum
divergence **0**, and **0** model requests. Endpoint calls total 183 across the
three runs (55 each) plus 18 probe polls — about 5 calls/minute over the 35
minutes, an order of magnitude below what a 1-minute production poller would
spend.

Only the five-hour window appeared, legacy `TOKENS_LIMIT` (percentage-only), so
absolute amounts are zero with `has_usage` false throughout.

### Run 03:30:29–03:32:20 UTC — burst against a visibly burning account

The instrument that decides the claim: the account was burning during the run,
so the burst has a real control to be compared against.

| Time (UTC) | Phase | five-hour used | reset_at |
|---|---|---|---|
| 03:30:29.37 | baseline first | 18% | 2026-09-06T03:51:33Z |
| 03:31:40.79 | baseline last | 18% | 2026-09-06T03:51:33Z |
| 03:31:49.11 | burst (40 polls) | 18% | 2026-09-06T03:51:33Z |
| 03:32:19.93 | post-burst last | **19%** | 2026-09-06T03:51:33Z |

| Quantity | Value |
|---|---|
| Baseline burn rate | 0 usage-fraction/second |
| Post-burst burn rate | 3.26 × 10⁻⁴ usage-fraction/second (18% → 19% in 30.6 s) |
| Burst window | 7.934 s, 40 polls |
| Burst usage delta | 0 |
| Drift the cadence rate predicts for that window | 2.59 × 10⁻³ |
| Burst excess | −2.59 × 10⁻³ |
| Reset stamp drift across the whole run | 0 (all 12 samples and all 3 credit readings carry `2026-09-06T03:51:33Z`) |

### Runs 03:57:02 and 04:03:50 UTC — after the 03:51:33Z window reset

Both confirmed, but **degenerate**: the freshly-reset window sat still at 1% for
the whole of each run, so the cadence phases show 0 drift and the burst
comparison is 0 against 0. These runs establish that polling does not disturb a
quiet window and that the reset boundary is crossed cleanly (reset stamp moved
`03:51:33Z` → `08:51:39Z`, a 5 h + 6 s step, with no intermediate or malformed
reading); they do not by themselves decide zero-burn, because a burst compared
against a zero control cannot distinguish "unmetered" from "nothing to measure".

### Paired proxy-vs-ZCode comparison — 04:03:50–04:04:20 UTC overlap

The proxy side is run 04:03:50's normalized telemetry; the ZCode side is the
raw provider endpoint projected by `quota_canary.py`'s own
`project_usage_view`, sampled on a 10 s cadence (18 polls, 04:01:30–04:04:20).
The two timelines overlap for 30 s.

| Time (UTC) | Surface | five-hour used | reset_at |
|---|---|---|---|
| 04:03:50.43 | proxy `/health` | 1% | 2026-09-06T08:51:39Z |
| 04:04:00.06 | ZCode usage view | 1.0% | 2026-09-06T08:51:39.513Z |
| 04:04:10.06 | ZCode usage view | 1.0% | 2026-09-06T08:51:39.513Z |
| 04:04:20.06 | ZCode usage view | 1.0% | 2026-09-06T08:51:39.513Z |

| Delta | Value | Note |
|---|---|---|
| Percentage | **0.0 pp** | both surfaces read 1% at every overlapping sample |
| Reset stamp | **0.513 s** | the proxy truncates the provider's millisecond stamp to whole-second RFC3339; the script's own tolerance is 5.0 s |

Both deltas are inside tolerance, and both are the *expected* shape of
agreement: the provider reports whole percentage steps, and the reset delta is
exactly the sub-second precision the proxy's RFC3339 rendering discards.

**Zero-burn: confirmed** (run 03:30:29, the non-degenerate instrument). 40
back-to-back monitor polls in 7.9 s moved the counter by exactly zero while the
same account burned one full percentage point of the five-hour window during
the same 111-second run. Runs 03:57:02 and 04:03:50 are consistent but carry no
control. The reset stamp held at a single value across every sample of all
three runs while its countdown decayed by the wall clock.

**Failures do not affect inference: re-confirmed** on 2026-09-06 at the same
commit, by the three structural checks above plus the hermetic failure-injection
suite run green under `-race`:
`TestQuotaPollerKeepsLastKnownGoodAcrossFailures`,
`TestQuotaPollerTimeoutDoesNotBlockAdmission`,
`TestQuotaPollerClassifiesProviderRejectionAndTransportFailure`,
`TestQuotaPollerClassifiesMalformedPayload`,
`TestQuotaPollerNeverLogsCredential`, `TestQuotaDecisionMetrics`,
`TestQuotaExhaustionIsRecordedButEnforcesNothing`,
`TestQuotaSignalPhasesNeverTriggerEnforcement`,
`TestQuotaEnforcementRecordersHaveNoLiveCallSite`, and
`go test -race -count=2 ./proxy/quota/`. The whole module passes with
`TestConcurrentLoad` skipped (known load-bound flake, unrelated to quota).

Live mode of `quota_canary.py` was **not** used for the comparison above: it
needs a reachable proxy serving `/health` with the poller enabled, and neither
production `zai-proxy` nor `zai-proxy-canary` sets any `QUOTA_*` variable, so
neither has quota telemetry to compare. That refusal is itself the enforcement
guardrail holding — but it means the paired measurement has to come from the Go
live canary plus a direct projection of the provider endpoint.

One reporting defect found, not fixed here: the canary report's
`analysis.reset_time_deltas_seconds` is named as a delta but holds the *last
observed countdown* (`ResetInSecond`), overwritten per window — it reads
1153.27 s on run 03:30:29 and 0 on the degenerate runs, neither of which is a
drift measure. Reset-stamp drift must be read from `reset_at` constancy across
`samples[]` and `credit_readings[]`, as done above.

## Comparing against ZCode's usage view

ZCode's usage view renders the same account endpoint this proxy polls
(`/api/monitor/usage/quota/limit`), so the comparison is a diff of two renders
of one source, not of two measurements. The proxy's `used_fraction` × 100 and
`reset_at` should match what ZCode displays at the same instant.

To reproduce:

1. Run the live canary and note its start and end timestamps.
2. Open ZCode's usage view during that window and read the five-hour
   percentage and reset time.
3. Diff against the report's `credit_readings[].windows[]` — `used_percent`
   and `reset_at` are the fields to compare.

`proxy/scripts/quota_canary.py` automates this comparison. Its default
`hermetic` mode needs no credential and no non-loopback traffic: a local
synthetic origin serves the committed fixtures in
`proxy/testdata/quota_canary/` to both surfaces, rotating them across rounds so
a usage-percentage delta and a reset-stamp delta are observable without a real
account, and it runs the hermetic Go canaries first so a fixture that has
drifted from what the proxy actually renders fails before any verdict is built
on it.

```bash
python3 proxy/scripts/quota_canary.py run                  # hermetic; no credential
python3 proxy/scripts/quota_canary.py secret-scan --self-test
```

In `live` mode (`--proxy-url` plus `ZAI_API_KEY` in the environment) it samples
a real proxy and the real origin instead; the credential is read only from the
environment, never argv and never a file in the repo. Both modes emit one
artifact whose schema is an allowlist enforced at run time — timestamps,
percentages, and reset measures, plus the structural fields that say what a
number belongs to — and both the artifact and the summary are scanned for the
credential and the provider's own payload keys before either is emitted; a run
that would leak exits without writing. `TestQuotaCanaryScriptHermeticRun` pins
all of this from Go, restating the allowlist from outside the script.

Agreement within ±1 percentage point is expected: the provider reports the
five-hour window in whole percentage steps under the legacy `TOKENS_LIMIT`
schema, so a reading taken across a step boundary differs by exactly one step.
Larger divergence means the two surfaces are reading different windows or the
sample has gone stale (`fresh: false`, `result="stale"` in
`zai_proxy_quota_poll_total`).

The 2026-09-05 run's comparison window is 22:29:42–22:31:32 UTC, five-hour
window 12% → 13%, reset `2026-09-05T22:51:32Z`, plan tier `max`. The
2026-09-06 comparison is the paired measurement recorded above: 04:03:50–
04:04:20 UTC, five-hour window 1% on both surfaces, reset
`2026-09-06T08:51:39Z` (proxy) against `2026-09-06T08:51:39.513Z` (raw) —
0.0 pp and 0.513 s, both inside tolerance.

## Secret-safety rules for this surface

- The credential travels by pipe into the test process's environment. Never a
  literal in a command, file, bead, doc, or log.
- The client sends the raw key as the `Authorization` value with no `Bearer`
  prefix, matching Z.AI's usage-query tooling, and never follows a redirect,
  so the credential cannot leave the configured origin.
- Errors carry status codes and error classes only. Provider message text is
  deliberately excluded from `ProviderError.Error()` because it is not
  guaranteed free of account-identifying material.
- The report holds derived numbers only, is written mode 600 outside the repo,
  and is checked for the credential before being written. Delete it when the
  evidence has been recorded.

### Fields safe to record

Evidence copied out of a report into a doc, a bead, or an incident note may
contain exactly this:

- **Timestamps** — sample times, `last_success_at`, `sample_age_seconds`,
  `reset_at`, `reset_in` countdowns, phase durations.
- **Labels that say what a number belongs to** — `plan_tier`, `window`
  (`five_hour`/`weekly`), `limit_type`, `variant`, `last_outcome`, the poll
  `result` vocabulary, and the phase names.
- **Numbers** — `used_fraction` and its percentage rendering, remaining
  ratio, absolute `used`/`limit`/`remaining` credit amounts where the
  provider's schema reports them (zero with `has_usage` false under the
  legacy `TOKENS_LIMIT` schema), per-phase burn rates, burst windows and
  deltas, reset-stamp drift, and `/health`-vs-`/metrics` divergence.
- **Process facts** — poll and model-request counts, test names with their
  results, commit IDs, origin host, and the verdict words (`confirmed`,
  `unresolved`, `falsified`).

Never record: the credential in any form; a raw provider payload or any of
its keys (`currentValue`, `nextResetTime`, `percentage`, `remaining`,
`limits`, `level`, `msg`, `success`); an account identifier; or provider
error message text.

This restates what the emitters already enforce. The script's artifact is a
run-time allowlist (`ALLOWED_TOP` / `ALLOWED_SAMPLE` / `ALLOWED_COMPARISON`
in `proxy/scripts/quota_canary.py` — a field outside it aborts the run
before anything is written), and the Go canary's report is a fixed field set
in `proxy/quota_canary_live_test.go` scanned for the credential before it is
written. Copying fields from either report can only widen the record if a
hand-rolled format is invented; don't invent one.

### Scanning the documentation itself

```bash
# The canary scanner over its own scope, and the scanner self-checked.
python3 proxy/scripts/quota_canary.py secret-scan --self-test

# This file under gitleaks. Expected: no leaks found.
mkdir -p /tmp/doc-scan && cp docs/notes/zai-proxy-quota-observability.md /tmp/doc-scan/
gitleaks detect --no-git --source /tmp/doc-scan
rm -rf /tmp/doc-scan
```

Recorded 2026-09-06: gitleaks 8.25.1 reports **0 findings** on this file.
The canary scanner's credential-directed classes (`credential_literal`,
`bearer_header`) are also clean here, but a raw run of that scanner over
this file does not exit clean: its two
encoded-run heuristics (`hex_run`, `base64_run` — any 32/40+ character
alphanumeric run) fire 25 times, every one on the long Go test identifiers
this document quotes (`TestQuotaPollerKeepsLastKnownGoodAcrossFailures` and
the rest — 8 distinct tokens, all named `Test…`). Triage rule for any such
hit: print the matched token; a CamelCase identifier naming a symbol in this
repo is benign, anything opaque is not. The scanner is scoped to the
canary's own files — script and fixtures — where it exits clean without
triage.

(Out of scope of this file, noted for whoever scans `docs/notes/` as a
whole: gitleaks reports one pre-existing false positive in
`TOKENIZER_CONFIGURATION.md`, where the `export TOKENIZER_MODEL=<name>`
example line trips the `generic-api-key` rule on the "TOKEN" inside the
variable name. The value is a model name, not a credential — quoting the
line verbatim here reproduces the same finding, which is why it is
paraphrased.)

## Known non-quota failures and limitations

- `TestConcurrentLoad` (`proxy/performance_benchmark_test.go`) asserts a 5 ms
  average token-counting latency and fails on a busy shared checkout. Verified
  pre-existing at HEAD via a `go test -overlay` run of the committed files, so
  it is not caused by the quota work. Exclude it with
  `go test -count=1 -skip TestConcurrentLoad ./...`.
- Only the five-hour window appeared in this account's payload. The weekly
  window is modelled and appears as a second entry whenever the provider sends
  one; its absence here is the provider's payload, not a parsing gap.
- The legacy `TOKENS_LIMIT` schema is percentage-only, so absolute credit
  amounts are zero with `has_usage` false for those windows. The current
  `CREDIT_LIMIT` schema populates them.
- Zero-burn resolution is bounded by the provider's whole-percentage steps; the
  verdict reports `unresolved` rather than claiming precision it does not have.
