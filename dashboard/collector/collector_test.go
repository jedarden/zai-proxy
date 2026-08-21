package collector

import (
	"testing"
	"time"
)

func TestBuildSnapshotAggregatesEstimatedCostCounters(t *testing.T) {
	collector := NewCollector(Config{})
	snapshot := collector.buildSnapshot(map[string][]MetricValue{
		"zai_proxy_estimated_cost_usd_total": {
			{Value: 0.50, Labels: map[string]string{"direction": "input", "pricing_tier": "off_peak"}},
			{Value: 0.25, Labels: map[string]string{"direction": "input", "pricing_tier": "peak"}},
			{Value: 1.10, Labels: map[string]string{"direction": "output", "pricing_tier": "off_peak"}},
			{Value: 0.03, Labels: map[string]string{"direction": "cache_read", "pricing_tier": "peak"}},
			{Value: 0.00, Labels: map[string]string{"direction": "cache_write", "pricing_tier": "off_peak"}},
		},
	}, nil, time.Now(), time.Time{}, "production")

	if snapshot.EstimatedCostUSDInput != 0.75 {
		t.Errorf("input cost = %v, want 0.75", snapshot.EstimatedCostUSDInput)
	}
	if snapshot.EstimatedCostUSDOutput != 1.10 {
		t.Errorf("output cost = %v, want 1.10", snapshot.EstimatedCostUSDOutput)
	}
	if snapshot.EstimatedCostUSDCacheRead != 0.03 {
		t.Errorf("cache-read cost = %v, want 0.03", snapshot.EstimatedCostUSDCacheRead)
	}
	if snapshot.EstimatedCostUSDCacheWrite != 0 {
		t.Errorf("cache-write cost = %v, want 0", snapshot.EstimatedCostUSDCacheWrite)
	}
}
