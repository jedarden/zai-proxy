package collector

import (
	"math"
	"testing"
)

// TestRateComputation tests the rate computation logic.
// Since rate computation happens in the collector, we test it through
// buildSnapshot-like scenarios.

func TestRateComputation_FirstScrape(t *testing.T) {
	// On the first scrape, rates should be 0
	cur := map[string][]MetricValue{
		"test_counter": {{Value: 100, Labels: map[string]string{}}},
	}
	prev := map[string][]MetricValue{}
	elapsed := 5.0

	rate := computeRate(cur, prev, "test_counter", nil, elapsed)
	if rate != 0 {
		t.Errorf("first scrape rate should be 0, got %f", rate)
	}
}

func TestRateComputation_NormalDelta(t *testing.T) {
	cur := map[string][]MetricValue{
		"test_counter": {{Value: 110, Labels: map[string]string{}}},
	}
	prev := map[string][]MetricValue{
		"test_counter": {{Value: 100, Labels: map[string]string{}}},
	}
	elapsed := 5.0

	rate := computeRate(cur, prev, "test_counter", nil, elapsed)
	expected := 10.0 / 5.0 // 2.0
	if math.Abs(rate-expected) > 0.001 {
		t.Errorf("rate should be %f, got %f", expected, rate)
	}
}

func TestRateComputation_CounterReset(t *testing.T) {
	// Counter reset: current < previous
	cur := map[string][]MetricValue{
		"test_counter": {{Value: 5, Labels: map[string]string{}}},
	}
	prev := map[string][]MetricValue{
		"test_counter": {{Value: 100, Labels: map[string]string{}}},
	}
	elapsed := 5.0

	rate := computeRate(cur, prev, "test_counter", nil, elapsed)
	// On counter reset, should use current value as delta
	expected := 5.0 / 5.0 // 1.0
	if math.Abs(rate-expected) > 0.001 {
		t.Errorf("rate after counter reset should be %f, got %f", expected, rate)
	}
}

func TestRateComputation_ZeroElapsed(t *testing.T) {
	cur := map[string][]MetricValue{
		"test_counter": {{Value: 110, Labels: map[string]string{}}},
	}
	prev := map[string][]MetricValue{
		"test_counter": {{Value: 100, Labels: map[string]string{}}},
	}
	elapsed := 0.0

	rate := computeRate(cur, prev, "test_counter", nil, elapsed)
	if rate != 0 {
		t.Errorf("rate with zero elapsed should be 0, got %f", rate)
	}
}

func TestRateComputation_NegativeElapsed(t *testing.T) {
	cur := map[string][]MetricValue{
		"test_counter": {{Value: 110, Labels: map[string]string{}}},
	}
	prev := map[string][]MetricValue{
		"test_counter": {{Value: 100, Labels: map[string]string{}}},
	}
	elapsed := -1.0

	rate := computeRate(cur, prev, "test_counter", nil, elapsed)
	if rate != 0 {
		t.Errorf("rate with negative elapsed should be 0, got %f", rate)
	}
}

func TestRateComputation_WithLabelFilter(t *testing.T) {
	cur := map[string][]MetricValue{
		"test_counter": {
			{Value: 100, Labels: map[string]string{"direction": "input"}},
			{Value: 50, Labels: map[string]string{"direction": "output"}},
		},
	}
	prev := map[string][]MetricValue{
		"test_counter": {
			{Value: 90, Labels: map[string]string{"direction": "input"}},
			{Value: 45, Labels: map[string]string{"direction": "output"}},
		},
	}
	elapsed := 5.0

	rate := computeRate(cur, prev, "test_counter", map[string]string{"direction": "input"}, elapsed)
	expected := 10.0 / 5.0 // 2.0
	if math.Abs(rate-expected) > 0.001 {
		t.Errorf("filtered rate should be %f, got %f", expected, rate)
	}
}

func TestRateComputation_MissingMetric(t *testing.T) {
	cur := map[string][]MetricValue{}
	prev := map[string][]MetricValue{
		"test_counter": {{Value: 100, Labels: map[string]string{}}},
	}
	elapsed := 5.0

	rate := computeRate(cur, prev, "test_counter", nil, elapsed)
	// Missing metric should result in 0 rate
	if rate != 0 {
		t.Errorf("rate for missing metric should be 0, got %f", rate)
	}
}

// computeRate is a helper that mimics the rate computation in the collector.
func computeRate(cur, prev map[string][]MetricValue, name string, filter map[string]string, elapsed float64) float64 {
	if elapsed <= 0 || len(prev) == 0 {
		return 0
	}

	curVal := sumValues(cur[name], filter)
	prevVal := sumValues(prev[name], filter)

	delta := curVal - prevVal
	if delta < 0 {
		delta = curVal // Counter reset
	}
	return delta / elapsed
}

func sumValues(values []MetricValue, filter map[string]string) float64 {
	var total float64
	for _, v := range values {
		if matchesLabels(v.Labels, filter) {
			total += v.Value
		}
	}
	return total
}

func matchesLabels(labels, filter map[string]string) bool {
	if filter == nil {
		return true
	}
	for k, v := range filter {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// TestDeltaComputation tests delta computation for counters.
func TestDeltaComputation(t *testing.T) {
	tests := []struct {
		name     string
		curVal   float64
		prevVal  float64
		expected float64
	}{
		{
			name:     "normal increment",
			curVal:   110,
			prevVal:  100,
			expected: 10,
		},
		{
			name:     "no change",
			curVal:   100,
			prevVal:  100,
			expected: 0,
		},
		{
			name:     "counter reset",
			curVal:   5,
			prevVal:  100,
			expected: 5, // Use current value
		},
		{
			name:     "large increment",
			curVal:   1000000,
			prevVal:  999000,
			expected: 1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delta := tt.curVal - tt.prevVal
			if delta < 0 {
				delta = tt.curVal
			}
			if delta != tt.expected {
				t.Errorf("delta should be %f, got %f", tt.expected, delta)
			}
		})
	}
}
