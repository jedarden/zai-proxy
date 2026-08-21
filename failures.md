# Structured Failure Report

Generated from `go test -v ./...` at `2026-08-21T06:27:44Z` (revision `f983ca7`; exit code `1`).

The raw verbose test log is retained outside the repository at `/home/coding/scratch/zai-proxy-failure-report/go-test-v-20260821.txt`. `total_failures` counts emitted source diagnostics; a parent Go subtest aggregate is not counted twice.

## Summary

| Metric | Count |
| --- | ---: |
| Total failure details | 12 |
| Failed test cases | 7 |
| Aggregate failure markers | 1 |
| Categorized | 4 |
| Other / uncategorized | 8 |
| Low confidence | 8 |

## Failures by Category

| Category | Count |
| --- | ---: |
| Other: unclassified failure | 8 |
| assertion_error | 4 |

## Failure Details

| Test name | File:line | Error message | Category |
| --- | --- | --- | --- |
| TestMemoryProfile/Medium | [proxy/performance_benchmark_test.go:627](proxy/performance_benchmark_test.go#L627) | Memory allocation too high: 43448 bytes (max: 10240) | Other: unclassified failure |
| TestMemoryProfile/Long | [proxy/performance_benchmark_test.go:627](proxy/performance_benchmark_test.go#L627) | Memory allocation too high: 419853 bytes (max: 10240) | Other: unclassified failure |
| TestTranslateRequest_InvalidJSON | [proxy/translator_test.go:38](proxy/translator_test.go#L38) | expected error for invalid JSON | Other: unclassified failure |
| TestTranslateRequest_StripsThinking | [proxy/translator_test.go:49](proxy/translator_test.go#L49) | expected changed=true when thinking is stripped | Other: unclassified failure |
| TestTranslateRequest_StripsThinking | [proxy/translator_test.go:57](proxy/translator_test.go#L57) | 'thinking' should have been removed | assertion_error |
| TestTranslateRequest_SystemArrayToString | [proxy/translator_test.go:79](proxy/translator_test.go#L79) | expected changed=true when system is converted | Other: unclassified failure |
| TestTranslateRequest_SystemArrayToString | [proxy/translator_test.go:89](proxy/translator_test.go#L89) | 'system' should be a string, got []interface {}: [map[cache_control:map[type:ephemeral] text:You are a helpful assistant. type:text] map[text:Be concise. type:… | assertion_error |
| TestTranslateRequest_StripsCacheControlFromMessages | [proxy/translator_test.go:127](proxy/translator_test.go#L127) | expected changed=true when cache_control is stripped | Other: unclassified failure |
| TestTranslateRequest_StripsCacheControlFromMessages | [proxy/translator_test.go:142](proxy/translator_test.go#L142) | content block 0 still has cache_control after stripping | Other: unclassified failure |
| TestTranslateRequest_CombinedTransformations | [proxy/translator_test.go:172](proxy/translator_test.go#L172) | expected changed=true for combined transformations | Other: unclassified failure |
| TestTranslateRequest_CombinedTransformations | [proxy/translator_test.go:181](proxy/translator_test.go#L181) | 'thinking' should have been removed | assertion_error |
| TestTranslateRequest_CombinedTransformations | [proxy/translator_test.go:186](proxy/translator_test.go#L186) | 'system' should be a string after translation | assertion_error |

## Validation

All 12 emitted failure details have a source location that exists at the tested revision, and all were classified by `proxy/testutil`. `8` details are explicitly `Other: unclassified failure`; these need a taxonomy rule or manual review rather than being silently omitted.
