package main

import (
	"strings"
	"testing"
	"time"
)

// fixedReset is a plausible near-future reset instant used across the
// metadata cases: the parser must pass it through unchanged, in UTC.
var fixedReset = time.Unix(1788624000, 0).UTC()

// assertZaiBusinessError compares every field, using time.Equal for the
// reset instant so wall-clock representation differences cannot flake.
func assertZaiBusinessError(t *testing.T, got, want ZaiBusinessError) {
	t.Helper()
	if got.Class != want.Class {
		t.Errorf("Class = %q, want %q", got.Class, want.Class)
	}
	if got.Code != want.Code {
		t.Errorf("Code = %q, want %q", got.Code, want.Code)
	}
	if got.Oversized != want.Oversized {
		t.Errorf("Oversized = %v, want %v", got.Oversized, want.Oversized)
	}
	if got.RetryAfter != want.RetryAfter {
		t.Errorf("RetryAfter = %v, want %v", got.RetryAfter, want.RetryAfter)
	}
	if !got.ResetAt.Equal(want.ResetAt) {
		t.Errorf("ResetAt = %v, want %v", got.ResetAt, want.ResetAt)
	}
}

func TestParseZaiError_ClassificationAcrossEnvelopes(t *testing.T) {
	tests := []struct {
		name string
		body string
		want ZaiBusinessError
	}{
		// Concurrency (1302) in every tolerated shape.
		{"1302 nested string code", `{"error":{"code":"1302","message":"并发线路已满"}}`,
			ZaiBusinessError{Class: ZaiErrorClassConcurrency, Code: "1302"}},
		{"1302 nested numeric code", `{"error":{"code":1302,"message":"concurrency"}}`,
			ZaiBusinessError{Class: ZaiErrorClassConcurrency, Code: "1302"}},
		{"1302 flat string code", `{"code":"1302","msg":"concurrency"}`,
			ZaiBusinessError{Class: ZaiErrorClassConcurrency, Code: "1302"}},
		{"1302 flat numeric code", `{"code":1302,"message":"concurrency"}`,
			ZaiBusinessError{Class: ZaiErrorClassConcurrency, Code: "1302"}},
		{"1302 padded code string", `{"code":" 1302 ","msg":"concurrency"}`,
			ZaiBusinessError{Class: ZaiErrorClassConcurrency, Code: "1302"}},
		{"1302 top level with nested error lacking code", `{"code":1302,"error":{"message":"detail"}}`,
			ZaiBusinessError{Class: ZaiErrorClassConcurrency, Code: "1302"}},
		{"1302 anthropic style with null error at top", `{"type":"error","error":null,"code":1302}`,
			ZaiBusinessError{Class: ZaiErrorClassConcurrency, Code: "1302"}},

		// Frequency (1303, 1305).
		{"1303 flat numeric", `{"code":1303,"msg":"rate"}`,
			ZaiBusinessError{Class: ZaiErrorClassFrequency, Code: "1303"}},
		{"1303 anthropic style", `{"type":"error","error":{"type":"rate_limit_error","code":"1303","message":"rate"}}`,
			ZaiBusinessError{Class: ZaiErrorClassFrequency, Code: "1303"}},
		{"1305 flat string", `{"code":"1305","msg":"rate"}`,
			ZaiBusinessError{Class: ZaiErrorClassFrequency, Code: "1305"}},
		{"1305 nested numeric", `{"error":{"code":1305,"message":"rate"}}`,
			ZaiBusinessError{Class: ZaiErrorClassFrequency, Code: "1305"}},

		// Quota exhaustion (1308, 1310).
		{"1308 flat string", `{"code":"1308","msg":"quota"}`,
			ZaiBusinessError{Class: ZaiErrorClassQuota, Code: "1308"}},
		{"1308 nested numeric", `{"error":{"code":1308,"message":"quota"}}`,
			ZaiBusinessError{Class: ZaiErrorClassQuota, Code: "1308"}},
		{"1310 flat numeric", `{"code":1310,"msg":"quota"}`,
			ZaiBusinessError{Class: ZaiErrorClassQuota, Code: "1310"}},
		{"1310 nested string", `{"error":{"code":"1310","message":"quota"}}`,
			ZaiBusinessError{Class: ZaiErrorClassQuota, Code: "1310"}},

		// Model congestion (1312).
		{"1312 flat numeric", `{"code":1312,"msg":"congested"}`,
			ZaiBusinessError{Class: ZaiErrorClassModelCongestion, Code: "1312"}},
		{"1312 nested string", `{"error":{"code":"1312","message":"congested"}}`,
			ZaiBusinessError{Class: ZaiErrorClassModelCongestion, Code: "1312"}},

		// Whitespace around a valid body must not break parsing.
		{"1312 whitespace padded body", "\n\t {\"code\":1312,\"msg\":\"congested\"} \n",
			ZaiBusinessError{Class: ZaiErrorClassModelCongestion, Code: "1312"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertZaiBusinessError(t, ParseZaiError([]byte(tt.body), 0), tt.want)
		})
	}
}

func TestParseZaiError_MessageEmbeddedCode(t *testing.T) {
	tests := []struct {
		name string
		body string
		want ZaiBusinessError
	}{
		{"1302 prefixed to message", `{"error":{"message":"1302: Concurrency limit reached"}}`,
			ZaiBusinessError{Class: ZaiErrorClassConcurrency, Code: "1302"}},
		{"1312 prefixed after whitespace", `{"msg":" 1312 model busy"}`,
			ZaiBusinessError{Class: ZaiErrorClassModelCongestion, Code: "1312"}},
		{"digit run ending at non-digit", `{"message":"1302x"}`,
			ZaiBusinessError{Class: ZaiErrorClassConcurrency, Code: "1302"}},
		{"unknown leading number ignored", `{"msg":"1234: something"}`,
			ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"number inside prose ignored", `{"message":"retries: 1302 attempted"}`,
			ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"overlong digit run ignored", `{"message":"1302123456: too long"}`,
			ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"explicit code wins over message prefix", `{"code":"1312","message":"1302: misleading prefix"}`,
			ZaiBusinessError{Class: ZaiErrorClassModelCongestion, Code: "1312"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertZaiBusinessError(t, ParseZaiError([]byte(tt.body), 0), tt.want)
		})
	}
}

func TestParseZaiError_ResetMetadata(t *testing.T) {
	week := 7 * 24 * time.Hour
	tests := []struct {
		name string
		body string
		want ZaiBusinessError
	}{
		{"reset_time seconds", `{"code":1303,"msg":"rate","reset_time":1788624000}`,
			ZaiBusinessError{Class: ZaiErrorClassFrequency, Code: "1303", ResetAt: fixedReset}},
		{"nextResetTime millis", `{"error":{"code":"1308","message":"quota","nextResetTime":1788624000000}}`,
			ZaiBusinessError{Class: ZaiErrorClassQuota, Code: "1308", ResetAt: fixedReset}},
		{"reset_at numeric string", `{"code":"1305","reset_at":"1788624000"}`,
			ZaiBusinessError{Class: ZaiErrorClassFrequency, Code: "1305", ResetAt: fixedReset}},
		{"resetTime scientific notation", `{"code":1303,"resetTime":1.788624e9}`,
			ZaiBusinessError{Class: ZaiErrorClassFrequency, Code: "1303", ResetAt: fixedReset}},
		{"retry_after seconds", `{"code":1303,"retry_after":42}`,
			ZaiBusinessError{Class: ZaiErrorClassFrequency, Code: "1303", RetryAfter: 42 * time.Second}},
		{"retryAfter numeric string", `{"code":"1305","retryAfter":"7"}`,
			ZaiBusinessError{Class: ZaiErrorClassFrequency, Code: "1305", RetryAfter: 7 * time.Second}},
		{"nested reset beats outer", `{"error":{"code":"1308","reset_time":1788624000},"reset_time":1000000000}`,
			ZaiBusinessError{Class: ZaiErrorClassQuota, Code: "1308", ResetAt: fixedReset}},
		{"outer reset used when nested lacks it", `{"error":{"code":"1308"},"reset_time":1788624000}`,
			ZaiBusinessError{Class: ZaiErrorClassQuota, Code: "1308", ResetAt: fixedReset}},
		{"metadata with unknown code retained", `{"code":9999,"msg":"?","reset_time":1788624000,"retry_after":5}`,
			ZaiBusinessError{Class: ZaiErrorClassUnknown, Code: "9999", ResetAt: fixedReset, RetryAfter: 5 * time.Second}},
		{"reset at exactly week is accepted", `{"code":1303,"retry_after":604800}`,
			ZaiBusinessError{Class: ZaiErrorClassFrequency, Code: "1303", RetryAfter: week}},

		// Implausible reset metadata is treated as absent, never clamped.
		{"epoch below one second resolution ignored", `{"code":1303,"reset_time":42}`,
			ZaiBusinessError{Class: ZaiErrorClassFrequency, Code: "1303"}},
		{"negative epoch ignored", `{"code":1303,"reset_time":-1788624000}`,
			ZaiBusinessError{Class: ZaiErrorClassFrequency, Code: "1303"}},
		{"ambiguous sub-millisecond-magnitude epoch ignored", `{"code":1303,"reset_time":999999999999}`,
			ZaiBusinessError{Class: ZaiErrorClassFrequency, Code: "1303"}},
		{"far-future epoch ignored", `{"code":1303,"reset_time":90000000000000}`,
			ZaiBusinessError{Class: ZaiErrorClassFrequency, Code: "1303"}},
		{"non-numeric reset ignored", `{"code":1303,"reset_time":"soon"}`,
			ZaiBusinessError{Class: ZaiErrorClassFrequency, Code: "1303"}},
		{"null reset ignored", `{"code":1303,"reset_time":null}`,
			ZaiBusinessError{Class: ZaiErrorClassFrequency, Code: "1303"}},
		{"zero retry_after ignored", `{"code":1303,"retry_after":0}`,
			ZaiBusinessError{Class: ZaiErrorClassFrequency, Code: "1303"}},
		{"negative retry_after ignored", `{"code":1303,"retry_after":-5}`,
			ZaiBusinessError{Class: ZaiErrorClassFrequency, Code: "1303"}},
		{"retry_after beyond a week ignored", `{"code":1303,"retry_after":604900}`,
			ZaiBusinessError{Class: ZaiErrorClassFrequency, Code: "1303"}},
		{"NaN retry_after ignored", `{"code":1303,"retry_after":"NaN"}`,
			ZaiBusinessError{Class: ZaiErrorClassFrequency, Code: "1303"}},
		{"infinite retry_after ignored", `{"code":1303,"retry_after":"Inf"}`,
			ZaiBusinessError{Class: ZaiErrorClassFrequency, Code: "1303"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertZaiBusinessError(t, ParseZaiError([]byte(tt.body), 0), tt.want)
		})
	}
}

func TestParseZaiError_MalformedAndUnknown(t *testing.T) {
	tests := []struct {
		name string
		body string
		want ZaiBusinessError
	}{
		{"empty body", "", ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"whitespace only body", "   \n\t", ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"truncated JSON", `{"error":{"code":"1302"`, ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"truncated flat JSON", `{"code":1302`, ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"JSON array", `[{"code":1302}]`, ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"bare JSON string", `"1302"`, ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"bare JSON number", `1302`, ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"JSON null", `null`, ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"empty object", `{}`, ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"unrelated fields only", `{"foo":"bar","n":42}`, ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"binary garbage", "\x00\x01\x02\xff", ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"SSE data prefix", "data: {\"code\":1302}", ZaiBusinessError{Class: ZaiErrorClassUnknown}},

		// Unknown business codes still retain their bounded code.
		{"unknown flat numeric code", `{"code":9999,"msg":"mystery"}`,
			ZaiBusinessError{Class: ZaiErrorClassUnknown, Code: "9999"}},
		{"unknown nested string code", `{"error":{"code":"4290","message":"?"}}`,
			ZaiBusinessError{Class: ZaiErrorClassUnknown, Code: "4290"}},
		{"near-miss known code", `{"code":1301,"msg":"filtered"}`,
			ZaiBusinessError{Class: ZaiErrorClassUnknown, Code: "1301"}},

		// Codes that cannot be a safe label are dropped entirely.
		{"non-numeric code", `{"code":"rate_limited","msg":"x"}`,
			ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"negative code", `{"code":-1302,"msg":"x"}`,
			ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"decimal code", `{"code":1302.5,"msg":"x"}`,
			ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"nine digit code", `{"code":130200000,"msg":"x"}`,
			ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"object code", `{"code":{"value":1302},"msg":"x"}`,
			ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"boolean code", `{"code":true,"msg":"x"}`,
			ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"null code", `{"code":null,"msg":"x"}`,
			ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"string error object ignored", `{"error":"1302"}`,
			ZaiBusinessError{Class: ZaiErrorClassUnknown}},
		{"no code and no message", `{"error":{"type":"rate_limit_error"}}`,
			ZaiBusinessError{Class: ZaiErrorClassUnknown}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertZaiBusinessError(t, ParseZaiError([]byte(tt.body), 0), tt.want)
		})
	}
}

func TestParseZaiError_BodyBudget(t *testing.T) {
	valid := `{"code":1302,"msg":"concurrency"}`

	t.Run("body at the limit parses", func(t *testing.T) {
		assertZaiBusinessError(t,
			ParseZaiError([]byte(valid), len(valid)),
			ZaiBusinessError{Class: ZaiErrorClassConcurrency, Code: "1302"})
	})

	t.Run("body over the limit is oversized and unparsed", func(t *testing.T) {
		got := ParseZaiError([]byte(valid), len(valid)-1)
		assertZaiBusinessError(t, got, ZaiBusinessError{
			Class:     ZaiErrorClassUnknown,
			Oversized: true,
		})
	})

	t.Run("oversized valid error is not classified", func(t *testing.T) {
		padded := valid + strings.Repeat(" ", DefaultMaxZaiErrorBodyBytes)
		got := ParseZaiError([]byte(padded), 0)
		if !got.Oversized || got.Class != ZaiErrorClassUnknown || got.Code != "" {
			t.Errorf("got %+v, want oversized unknown with no code", got)
		}
	})

	t.Run("negative limit means the default budget", func(t *testing.T) {
		padded := valid + strings.Repeat(" ", DefaultMaxZaiErrorBodyBytes)
		if got := ParseZaiError([]byte(padded), -1); !got.Oversized {
			t.Errorf("body over the default budget reported %+v, want oversized", got)
		}
		within := strings.Repeat(" ", DefaultMaxZaiErrorBodyBytes-len(valid)) + valid
		if got := ParseZaiError([]byte(within), -1); got.Class != ZaiErrorClassConcurrency {
			t.Errorf("body within the default budget reported %+v, want concurrency", got)
		}
	})

	t.Run("default budget classification still works", func(t *testing.T) {
		assertZaiBusinessError(t,
			ParseZaiError([]byte(valid), DefaultMaxZaiErrorBodyBytes),
			ZaiBusinessError{Class: ZaiErrorClassConcurrency, Code: "1302"})
	})
}

func TestClassifyZaiErrorCode(t *testing.T) {
	tests := []struct {
		code string
		want ZaiErrorClass
	}{
		{"1302", ZaiErrorClassConcurrency},
		{"1303", ZaiErrorClassFrequency},
		{"1305", ZaiErrorClassFrequency},
		{"1308", ZaiErrorClassQuota},
		{"1310", ZaiErrorClassQuota},
		{"1312", ZaiErrorClassModelCongestion},
		{"", ZaiErrorClassUnknown},
		{"1300", ZaiErrorClassUnknown},
		{"1301", ZaiErrorClassUnknown},
		{"1304", ZaiErrorClassUnknown},
		{"1309", ZaiErrorClassUnknown},
		{"1311", ZaiErrorClassUnknown},
		{"1313", ZaiErrorClassUnknown},
		{"9999", ZaiErrorClassUnknown},
		{"13021", ZaiErrorClassUnknown},
		{" 1302", ZaiErrorClassUnknown},
		{"abc", ZaiErrorClassUnknown},
	}
	for _, tt := range tests {
		t.Run("code "+tt.code, func(t *testing.T) {
			if got := ClassifyZaiErrorCode(tt.code); got != tt.want {
				t.Errorf("ClassifyZaiErrorCode(%q) = %q, want %q", tt.code, got, tt.want)
			}
		})
	}
}
