package collector

import (
	"math"
	"testing"

	"git.ardenone.com/jedarden/zai-proxy/dashboard/model"
)

func TestParser_ParseCounter(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		metric   string
		wantVal  float64
		wantLen  int
	}{
		{
			name: "simple counter",
			input: `# HELP test_counter A test counter
# TYPE test_counter counter
test_counter 42`,
			metric:  "test_counter",
			wantVal: 42,
			wantLen: 1,
		},
		{
			name: "counter with labels",
			input: `# TYPE http_requests_total counter
http_requests_total{method="GET",status="200"} 100
http_requests_total{method="POST",status="201"} 50`,
			metric:  "http_requests_total",
			wantVal: 100, // First value
			wantLen: 2,
		},
		{
			name: "counter with special characters in labels",
			input: `# TYPE api_requests counter
api_requests{endpoint="/api/v1/users",method="GET"} 1000`,
			metric:  "api_requests",
			wantVal: 1000,
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			result, err := p.Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			values, ok := result[tt.metric]
			if !ok {
				t.Fatalf("metric %s not found in result", tt.metric)
			}
			if len(values) != tt.wantLen {
				t.Errorf("got %d values, want %d", len(values), tt.wantLen)
			}
			if values[0].Value != tt.wantVal {
				t.Errorf("got value %f, want %f", values[0].Value, tt.wantVal)
			}
		})
	}
}

func TestParser_ParseGauge(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		metric  string
		wantVal float64
	}{
		{
			name: "simple gauge",
			input: `# TYPE temperature gauge
temperature 23.5`,
			metric:  "temperature",
			wantVal: 23.5,
		},
		{
			name: "gauge with negative value",
			input: `# TYPE temperature gauge
temperature -10.5`,
			metric:  "temperature",
			wantVal: -10.5,
		},
		{
			name: "gauge with labels",
			input: `# TYPE memory_bytes gauge
memory_bytes{type="used"} 1048576
memory_bytes{type="free"} 2097152`,
			metric:  "memory_bytes",
			wantVal: 1048576,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			result, err := p.Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			values, ok := result[tt.metric]
			if !ok {
				t.Fatalf("metric %s not found in result", tt.metric)
			}
			if values[0].Value != tt.wantVal {
				t.Errorf("got value %f, want %f", values[0].Value, tt.wantVal)
			}
		})
	}
}

func TestParser_ParseHistogram(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		metric    string
		wantCount float64
		wantSum   float64
		wantBuckets int
	}{
		{
			name: "standard histogram",
			input: `# TYPE http_request_duration_seconds histogram
http_request_duration_seconds_bucket{le="0.1"} 10
http_request_duration_seconds_bucket{le="0.5"} 25
http_request_duration_seconds_bucket{le="1"} 45
http_request_duration_seconds_bucket{le="+Inf"} 50
http_request_duration_seconds_sum 25.5
http_request_duration_seconds_count 50`,
			metric:      "http_request_duration_seconds",
			wantCount:   50,
			wantSum:     25.5,
			wantBuckets: 4,
		},
		{
			name: "histogram with labels",
			input: `# TYPE request_size_bytes histogram
request_size_bytes_bucket{le="100"} 5
request_size_bytes_bucket{le="1000"} 15
request_size_bytes_bucket{le="+Inf"} 20
request_size_bytes_sum 5000
request_size_bytes_count 20`,
			metric:      "request_size_bytes",
			wantCount:   20,
			wantSum:     5000,
			wantBuckets: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			result, err := p.Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			hist, err := p.ParseHistogram(result, tt.metric, nil)
			if err != nil {
				t.Fatalf("ParseHistogram() error = %v", err)
			}

			if hist.Count != tt.wantCount {
				t.Errorf("got count %f, want %f", hist.Count, tt.wantCount)
			}
			if hist.Sum != tt.wantSum {
				t.Errorf("got sum %f, want %f", hist.Sum, tt.wantSum)
			}
			if len(hist.Buckets) != tt.wantBuckets {
				t.Errorf("got %d buckets, want %d", len(hist.Buckets), tt.wantBuckets)
			}
		})
	}
}

func TestParser_ParseEmpty(t *testing.T) {
	p := NewParser()
	result, err := p.Parse("")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d metrics", len(result))
	}
}

func TestParser_ParseComments(t *testing.T) {
	input := `# This is a comment
# HELP some_metric Help text
# TYPE some_metric gauge
# Another comment`

	p := NewParser()
	result, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	// Should have the metric even with comments
	if len(result) != 1 {
		t.Errorf("expected 1 metric, got %d", len(result))
	}
}

func TestHistogramQuantile(t *testing.T) {
	tests := []struct {
		name     string
		q        float64
		buckets  []model.HistogramBucket
		wantNaN  bool
		minValue float64 // If not NaN, value should be >= this
		maxValue float64 // If not NaN, value should be <= this
	}{
		{
			name:    "empty buckets",
			q:       0.5,
			buckets: nil,
			wantNaN: true,
		},
		{
			name: "single bucket +Inf only",
			q:    0.5,
			buckets: []model.HistogramBucket{
				{UpperBound: math.Inf(1), Count: 100},
			},
			wantNaN: true, // Can't compute quantile with only +Inf bucket
		},
		{
			name: "single finite bucket",
			q:    0.5,
			buckets: []model.HistogramBucket{
				{UpperBound: 1.0, Count: 100},
				{UpperBound: math.Inf(1), Count: 100},
			},
			minValue: 0,
			maxValue: 1.0,
		},
		{
			name: "standard histogram p50",
			q:    0.5,
			buckets: []model.HistogramBucket{
				{UpperBound: 0.1, Count: 10},
				{UpperBound: 0.5, Count: 25},
				{UpperBound: 1.0, Count: 45},
				{UpperBound: math.Inf(1), Count: 50},
			},
			minValue: 0.5,
			maxValue: 1.0,
		},
		{
			name: "standard histogram p95",
			q:    0.95,
			buckets: []model.HistogramBucket{
				{UpperBound: 0.1, Count: 10},
				{UpperBound: 0.5, Count: 25},
				{UpperBound: 1.0, Count: 45},
				{UpperBound: 2.0, Count: 48},
				{UpperBound: math.Inf(1), Count: 50},
			},
			minValue: 1.0,
			maxValue: 2.0,
		},
		{
			name: "standard histogram p99",
			q:    0.99,
			buckets: []model.HistogramBucket{
				{UpperBound: 0.1, Count: 10},
				{UpperBound: 0.5, Count: 25},
				{UpperBound: 1.0, Count: 45},
				{UpperBound: 2.0, Count: 49},
				{UpperBound: math.Inf(1), Count: 50},
			},
			minValue: 1.0,
			maxValue: 2.0,
		},
		{
			name: "all values in first bucket",
			q:    0.5,
			buckets: []model.HistogramBucket{
				{UpperBound: 0.1, Count: 100},
				{UpperBound: math.Inf(1), Count: 100},
			},
			minValue: 0,
			maxValue: 0.1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HistogramQuantile(tt.q, tt.buckets)

			if tt.wantNaN {
				if !math.IsNaN(result) {
					t.Errorf("expected NaN, got %f", result)
				}
				return
			}

			if math.IsNaN(result) {
				t.Errorf("unexpected NaN")
				return
			}

			if result < tt.minValue || result > tt.maxValue {
				t.Errorf("result %f not in expected range [%f, %f]", result, tt.minValue, tt.maxValue)
			}
		})
	}
}

func TestHistogramQuantile_EdgeCases(t *testing.T) {
	t.Run("zero quantile", func(t *testing.T) {
		buckets := []model.HistogramBucket{
			{UpperBound: 0.1, Count: 10},
			{UpperBound: math.Inf(1), Count: 50},
		}
		result := HistogramQuantile(0, buckets)
		if math.IsNaN(result) || result < 0 {
			t.Errorf("unexpected result for q=0: %f", result)
		}
	})

	t.Run("quantile 1.0", func(t *testing.T) {
		buckets := []model.HistogramBucket{
			{UpperBound: 0.1, Count: 10},
			{UpperBound: 1.0, Count: 45},
			{UpperBound: math.Inf(1), Count: 50},
		}
		result := HistogramQuantile(1.0, buckets)
		// At q=1.0, we should get close to the last finite bucket
		if math.IsNaN(result) {
			t.Errorf("unexpected NaN for q=1.0")
		}
	})

	t.Run("buckets with zero count", func(t *testing.T) {
		buckets := []model.HistogramBucket{
			{UpperBound: 0.1, Count: 0},
			{UpperBound: 0.5, Count: 0},
			{UpperBound: 1.0, Count: 10},
			{UpperBound: math.Inf(1), Count: 10},
		}
		result := HistogramQuantile(0.5, buckets)
		// Should still compute something reasonable
		if math.IsNaN(result) {
			t.Errorf("unexpected NaN for buckets with zero counts")
		}
	})

	t.Run("all values in +Inf bucket", func(t *testing.T) {
		buckets := []model.HistogramBucket{
			{UpperBound: 0.1, Count: 0},
			{UpperBound: math.Inf(1), Count: 100},
		}
		result := HistogramQuantile(0.5, buckets)
		// All values are > 0.1 but we don't know the upper bound
		// Should return something >= 0.1
		if result < 0.1 {
			t.Errorf("expected result >= 0.1, got %f", result)
		}
	})
}
