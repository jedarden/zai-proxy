package quota

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// testNow is the fixed clock used for snapshot stamping in normalizer tests.
var testNow = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return body
}

// assertWindow compares every normalized field of one window.
func assertWindow(t *testing.T, name string, got, want WindowState) {
	t.Helper()
	if got.Window != want.Window {
		t.Errorf("%s: window = %q, want %q", name, got.Window, want.Window)
	}
	if got.LimitType != want.LimitType {
		t.Errorf("%s: limit type = %q, want %q", name, got.LimitType, want.LimitType)
	}
	if !got.ResetTime.Equal(want.ResetTime) {
		t.Errorf("%s: reset time = %v, want %v", name, got.ResetTime, want.ResetTime)
	}
	if got.Used != want.Used {
		t.Errorf("%s: used = %v, want %v", name, got.Used, want.Used)
	}
	if got.Limit != want.Limit {
		t.Errorf("%s: limit = %v, want %v", name, got.Limit, want.Limit)
	}
	if got.Remaining != want.Remaining {
		t.Errorf("%s: remaining = %v, want %v", name, got.Remaining, want.Remaining)
	}
	if got.HasUsage != want.HasUsage {
		t.Errorf("%s: has usage = %v, want %v", name, got.HasUsage, want.HasUsage)
	}
	if got.UsedFraction != want.UsedFraction {
		t.Errorf("%s: used fraction = %v, want %v", name, got.UsedFraction, want.UsedFraction)
	}
}

func TestNormalizeCurrentCreditLimitFixture(t *testing.T) {
	snapshot, err := Normalize(loadFixture(t, "current_credit_limit.json"), testNow)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	if snapshot.FetchedAt != testNow {
		t.Errorf("fetched at = %v, want %v", snapshot.FetchedAt, testNow)
	}
	if snapshot.PlanTier != "lite" {
		t.Errorf("plan tier = %q, want %q", snapshot.PlanTier, "lite")
	}
	if len(snapshot.Windows) != 2 {
		t.Fatalf("windows = %d, want 2 (%v)", len(snapshot.Windows), snapshot.Windows)
	}

	// The monthly TIME_LIMIT and the unknown future type must be dropped,
	// leaving the five-hour window first and the weekly window second.
	fiveHour, ok := snapshot.Window(WindowFiveHour)
	if !ok {
		t.Fatal("five-hour window missing")
	}
	assertWindow(t, "five-hour", fiveHour, WindowState{
		Window:       WindowFiveHour,
		LimitType:    LimitTypeCredit,
		ResetTime:    time.UnixMilli(1788541838272).UTC(),
		Used:         740,
		Limit:        2000,
		Remaining:    1259,
		HasUsage:     true,
		UsedFraction: 0.37,
	})

	weekly, ok := snapshot.Window(WindowWeekly)
	if !ok {
		t.Fatal("weekly window missing")
	}
	assertWindow(t, "weekly", weekly, WindowState{
		Window:       WindowWeekly,
		LimitType:    LimitTypeCredit,
		ResetTime:    time.UnixMilli(1789128084993).UTC(),
		Used:         5207,
		Limit:        10000,
		Remaining:    4792,
		HasUsage:     true,
		UsedFraction: 0.5207,
	})
}

func TestNormalizeLegacyTokensLimitFixture(t *testing.T) {
	snapshot, err := Normalize(loadFixture(t, "legacy_tokens_limit.json"), testNow)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if len(snapshot.Windows) != 2 {
		t.Fatalf("windows = %d, want 2 (%v)", len(snapshot.Windows), snapshot.Windows)
	}

	// Legacy payloads carry only the used percentage and the reset time;
	// the ratio must survive and the absolute amounts must be marked
	// absent rather than reported as zeros that look like data.
	fiveHour, ok := snapshot.Window(WindowFiveHour)
	if !ok {
		t.Fatal("five-hour window missing")
	}
	assertWindow(t, "five-hour", fiveHour, WindowState{
		Window:       WindowFiveHour,
		LimitType:    LimitTypeTokens,
		ResetTime:    time.UnixMilli(1788541838272).UTC(),
		HasUsage:     false,
		UsedFraction: 0.37,
	})

	weekly, ok := snapshot.Window(WindowWeekly)
	if !ok {
		t.Fatal("weekly window missing")
	}
	assertWindow(t, "weekly", weekly, WindowState{
		Window:       WindowWeekly,
		LimitType:    LimitTypeTokens,
		ResetTime:    time.UnixMilli(1789128084993).UTC(),
		HasUsage:     false,
		UsedFraction: 0.07,
	})
}

func TestNormalizeUnknownTypesOnlyFixture(t *testing.T) {
	snapshot, err := Normalize(loadFixture(t, "unknown_types_only.json"), testNow)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if len(snapshot.Windows) != 0 {
		t.Fatalf("windows = %v, want none", snapshot.Windows)
	}
}

func TestNormalizeProviderErrorFixture(t *testing.T) {
	_, err := Normalize(loadFixture(t, "provider_error.json"), testNow)
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("Normalize error = %v, want *ProviderError", err)
	}
	if providerErr.Code != 401 {
		t.Errorf("provider code = %d, want 401", providerErr.Code)
	}

	// The provider message quotes an account marker; neither the error
	// text nor anything else returned may carry it.
	if strings.Contains(err.Error(), "acct:marker-7f3a9") {
		t.Errorf("error text leaks provider message: %q", err.Error())
	}
	if providerErr.Message == "" {
		t.Error("ProviderError.Message should retain the provider text for callers that need it")
	}
}

func TestNormalizeMalformedFixture(t *testing.T) {
	_, err := Normalize(loadFixture(t, "malformed_truncated.json"), testNow)
	if err == nil {
		t.Fatal("Normalize of truncated JSON should fail")
	}
	if strings.Contains(err.Error(), "limits") && strings.Contains(err.Error(), "Operation") {
		t.Errorf("error text quotes payload content: %q", err.Error())
	}
}

func TestNormalizeStructuralErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "JSON null", body: "null"},
		{name: "limits missing", body: `{"code":200,"success":true,"data":{"level":"lite"}}`},
		{name: "data missing", body: `{"code":200,"success":true}`},
		{name: "limits not an array", body: `{"code":200,"success":true,"data":{"limits":{}}}`},
		{name: "wrong type field", body: `{"code":200,"success":true,"data":{"limits":[{"type":3}]}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Normalize([]byte(tc.body), testNow)
			if err == nil {
				t.Fatalf("Normalize(%s) should fail", tc.body)
			}
		})
	}
}

func TestNormalizeSuccessVariants(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantCode  int // provider error expectation; 0 means success expected
		wantCount int
	}{
		{
			name:      "empty limits array is a valid empty snapshot",
			body:      `{"code":200,"success":true,"data":{"limits":[]}}`,
			wantCount: 0,
		},
		{
			name:      "success flag absent with code 200",
			body:      `{"code":200,"data":{"limits":[{"type":"CREDIT_LIMIT","unit":3,"usage":100,"currentValue":25}]}}`,
			wantCount: 1,
		},
		{
			name:      "success flag absent with nonzero error code",
			body:      `{"code":401,"msg":"token expired or incorrect","data":null}`,
			wantCode:  401,
			wantCount: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, err := Normalize([]byte(tc.body), testNow)
			if tc.wantCode != 0 {
				var providerErr *ProviderError
				if !errors.As(err, &providerErr) {
					t.Fatalf("Normalize error = %v, want *ProviderError", err)
				}
				if providerErr.Code != tc.wantCode {
					t.Errorf("provider code = %d, want %d", providerErr.Code, tc.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("Normalize: %v", err)
			}
			if len(snapshot.Windows) != tc.wantCount {
				t.Errorf("windows = %d, want %d", len(snapshot.Windows), tc.wantCount)
			}
		})
	}
}

func TestNormalizeLimitEntries(t *testing.T) {
	credit := func(fields string) string {
		return `{"code":200,"success":true,"data":{"limits":[` + fields + `]}}`
	}
	tests := []struct {
		name        string
		body        string
		wantCount   int
		wantWindows []WindowState
	}{
		{
			name:      "unknown window unit is ignored",
			body:      credit(`{"type":"CREDIT_LIMIT","unit":5,"usage":100,"currentValue":10}`),
			wantCount: 0,
		},
		{
			name:      "unknown type with known unit is ignored",
			body:      credit(`{"type":"TOKENS_LIMIT_V3","unit":3,"percentage":40}`),
			wantCount: 0,
		},
		{
			name:      "type matching is case-insensitive",
			body:      credit(`{"type":"credit_limit","unit":3,"usage":100,"currentValue":10},{"type":"tokens_limit","unit":6,"percentage":8}`),
			wantCount: 2,
			wantWindows: []WindowState{
				{Window: WindowFiveHour, LimitType: LimitTypeCredit, HasUsage: true, Used: 10, Limit: 100, Remaining: 90, UsedFraction: 0.1},
				{Window: WindowWeekly, LimitType: LimitTypeTokens, UsedFraction: 0.08},
			},
		},
		{
			name:      "weekly window anchors on unit not number",
			body:      credit(`{"type":"CREDIT_LIMIT","unit":6,"number":7,"usage":100,"currentValue":10}`),
			wantCount: 1,
			wantWindows: []WindowState{
				{Window: WindowWeekly, LimitType: LimitTypeCredit, HasUsage: true, Used: 10, Limit: 100, Remaining: 90, UsedFraction: 0.1},
			},
		},
		{
			name:      "duplicate window keeps the first observation",
			body:      credit(`{"type":"CREDIT_LIMIT","unit":3,"usage":100,"currentValue":10},{"type":"CREDIT_LIMIT","unit":3,"usage":100,"currentValue":99}`),
			wantCount: 1,
			wantWindows: []WindowState{
				{Window: WindowFiveHour, LimitType: LimitTypeCredit, HasUsage: true, Used: 10, Limit: 100, Remaining: 90, UsedFraction: 0.1},
			},
		},
		{
			name:      "percentage above 100 clamps to full",
			body:      credit(`{"type":"CREDIT_LIMIT","unit":3,"usage":100,"currentValue":150}`),
			wantCount: 1,
			wantWindows: []WindowState{
				{Window: WindowFiveHour, LimitType: LimitTypeCredit, HasUsage: true, Used: 150, Limit: 100, Remaining: 0, UsedFraction: 1},
			},
		},
		{
			name:      "negative used clamps to zero",
			body:      credit(`{"type":"CREDIT_LIMIT","unit":3,"usage":100,"currentValue":-5,"remaining":-2}`),
			wantCount: 1,
			wantWindows: []WindowState{
				{Window: WindowFiveHour, LimitType: LimitTypeCredit, HasUsage: true, Used: 0, Limit: 100, Remaining: 0, UsedFraction: 0},
			},
		},
		{
			name:      "zero allowance falls back to percentage",
			body:      credit(`{"type":"CREDIT_LIMIT","unit":3,"usage":0,"currentValue":0,"percentage":25}`),
			wantCount: 1,
			wantWindows: []WindowState{
				{Window: WindowFiveHour, LimitType: LimitTypeCredit, HasUsage: true, UsedFraction: 0.25},
			},
		},
		{
			name:      "missing remaining derives from usage minus used",
			body:      credit(`{"type":"CREDIT_LIMIT","unit":3,"usage":200,"currentValue":50}`),
			wantCount: 1,
			wantWindows: []WindowState{
				{Window: WindowFiveHour, LimitType: LimitTypeCredit, HasUsage: true, Used: 50, Limit: 200, Remaining: 150, UsedFraction: 0.25},
			},
		},
		{
			name:      "credit entry with no usable signal is ignored",
			body:      credit(`{"type":"CREDIT_LIMIT","unit":3}`),
			wantCount: 0,
		},
		{
			name:      "legacy entry with no percentage is ignored",
			body:      credit(`{"type":"TOKENS_LIMIT","unit":3,"nextResetTime":1788541838272}`),
			wantCount: 0,
		},
		{
			name:      "legacy percentage outside 0-100 clamps",
			body:      credit(`{"type":"TOKENS_LIMIT","unit":3,"percentage":120}`),
			wantCount: 1,
			wantWindows: []WindowState{
				{Window: WindowFiveHour, LimitType: LimitTypeTokens, UsedFraction: 1},
			},
		},
		{
			name:      "missing reset time yields zero time",
			body:      credit(`{"type":"CREDIT_LIMIT","unit":6,"usage":100,"currentValue":10,"percentage":10}`),
			wantCount: 1,
			wantWindows: []WindowState{
				{Window: WindowWeekly, LimitType: LimitTypeCredit, HasUsage: true, Used: 10, Limit: 100, Remaining: 90, UsedFraction: 0.1},
			},
		},
		{
			name:      "non-positive reset time yields zero time",
			body:      credit(`{"type":"CREDIT_LIMIT","unit":6,"usage":100,"currentValue":10,"nextResetTime":0}`),
			wantCount: 1,
			wantWindows: []WindowState{
				{Window: WindowWeekly, LimitType: LimitTypeCredit, HasUsage: true, Used: 10, Limit: 100, Remaining: 90, UsedFraction: 0.1},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, err := Normalize([]byte(tc.body), testNow)
			if err != nil {
				t.Fatalf("Normalize: %v", err)
			}
			if len(snapshot.Windows) != tc.wantCount {
				t.Fatalf("windows = %d (%v), want %d", len(snapshot.Windows), snapshot.Windows, tc.wantCount)
			}
			for i, want := range tc.wantWindows {
				assertWindow(t, want.Window.String(), snapshot.Windows[i], want)
			}
		})
	}
}

func TestSnapshotWindowLookup(t *testing.T) {
	snapshot, err := Normalize(loadFixture(t, "current_credit_limit.json"), testNow)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if _, ok := snapshot.Window(Window("monthly")); ok {
		t.Error("lookup of an unmodeled window should miss")
	}
	if _, ok := (Snapshot{}).Window(WindowFiveHour); ok {
		t.Error("lookup on an empty snapshot should miss")
	}
}
