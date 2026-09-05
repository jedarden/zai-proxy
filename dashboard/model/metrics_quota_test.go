package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMetricSnapshotJSON_WithoutQuotaOmitsKey(t *testing.T) {
	data, err := (&MetricSnapshot{Timestamp: 1, Variant: "production"}).ToJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "quota") {
		t.Fatalf("snapshot without quota telemetry must not serialize a quota key: %s", data)
	}
}

func TestQuotaStateJSON_SerializesWindowsAndOmitsMissing(t *testing.T) {
	usage := 0.42
	remaining := 0.58
	reset := int64(1900000000)
	open := true
	snapshot := &MetricSnapshot{
		Timestamp: 1,
		Variant:   "production",
		Quota: &QuotaState{
			FiveHour: &QuotaWindowState{
				LimitType:      "CREDIT_LIMIT",
				UsageRatio:     &usage,
				RemainingRatio: &remaining,
				ResetTimeUnix:  &reset,
			},
			Weekly:      &QuotaWindowState{LimitType: "CREDIT_LIMIT"},
			GateOpen:    &open,
			Enforcement: true,
		},
	}

	data, err := snapshot.ToJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded MetricSnapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	quota := decoded.Quota
	if quota == nil {
		t.Fatal("quota block lost in serialization")
	}
	if quota.FiveHour == nil || quota.FiveHour.UsageRatio == nil || *quota.FiveHour.UsageRatio != usage {
		t.Errorf("five-hour usage round-trip = %+v", quota.FiveHour)
	}
	if quota.FiveHour.ResetTimeUnix == nil || *quota.FiveHour.ResetTimeUnix != reset {
		t.Errorf("five-hour reset round-trip = %+v", quota.FiveHour)
	}
	// The weekly window advertised only the schema: its missing observations
	// must stay absent, not become zero.
	if quota.Weekly == nil || quota.Weekly.UsageRatio != nil || quota.Weekly.RemainingRatio != nil {
		t.Errorf("weekly missing fields must stay nil, got %+v", quota.Weekly)
	}
	if quota.GateOpen == nil || !*quota.GateOpen || !quota.Enforcement {
		t.Errorf("enforcement signals round-trip = open %+v enforcing %v", quota.GateOpen, quota.Enforcement)
	}
	// Sample age was not advertised, so the key must be absent entirely.
	if strings.Contains(string(data), "sample_age_seconds") {
		t.Errorf("absent sample age must be omitted, got %s", data)
	}
}

func TestQuotaStateJSON_ObserveOnlyOmitsEnforcement(t *testing.T) {
	usage := 0.12
	data, err := (&MetricSnapshot{
		Quota: &QuotaState{
			Weekly: &QuotaWindowState{UsageRatio: &usage},
		},
	}).ToJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "enforcement") {
		t.Errorf("observe-only snapshot must not claim enforcement, got %s", data)
	}

	var decoded MetricSnapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Quota == nil || decoded.Quota.Enforcement {
		t.Error("absent enforcement must decode as observe-only")
	}
}

func TestQuotaStateCloneIsDeep(t *testing.T) {
	usage := 0.42
	original := &QuotaState{
		FiveHour: &QuotaWindowState{UsageRatio: &usage},
		Weekly:   &QuotaWindowState{LimitType: "CREDIT_LIMIT"},
	}

	clone := original.Clone()
	if clone == original {
		t.Fatal("Clone must return a new value")
	}

	clone.FiveHour.UsageRatio = nil
	clone.Weekly.LimitType = "TOKENS_LIMIT"
	clone.Weekly.UsageRatio = &usage

	if original.FiveHour.UsageRatio == nil {
		t.Error("mutating the clone must not nil the original's five-hour usage")
	}
	if original.Weekly.LimitType != "CREDIT_LIMIT" || original.Weekly.UsageRatio != nil {
		t.Error("mutating the clone must not leak into the original's weekly window")
	}

	var nilQuota *QuotaState
	if nilQuota.Clone() != nil {
		t.Error("cloning a nil quota state must stay nil")
	}
}
