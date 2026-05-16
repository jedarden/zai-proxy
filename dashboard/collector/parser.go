// Package collector implements Prometheus metrics scraping and parsing.
package collector

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"

	"github.com/ardenone/ardenone-cluster/containers/zai-proxy-dashboard/model"
)

// Parser parses Prometheus exposition format text into metric families.
type Parser struct{}

// NewParser creates a new Parser.
func NewParser() *Parser {
	return &Parser{}
}

// Parse parses Prometheus exposition format text and returns a map of metric names
// to their values and labels.
func (p *Parser) Parse(text string) (map[string][]MetricValue, error) {
	var parser expfmt.TextParser
	families, err := parser.TextToMetricFamilies(bytes.NewReader([]byte(text)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse prometheus text: %w", err)
	}

	result := make(map[string][]MetricValue)
	for name, family := range families {
		values := p.parseFamily(family)
		result[name] = values
	}
	return result, nil
}

// MetricValue represents a single metric value with its labels.
type MetricValue struct {
	Value  float64
	Labels map[string]string
}

// parseFamily extracts all values from a metric family.
func (p *Parser) parseFamily(family *dto.MetricFamily) []MetricValue {
	var values []MetricValue

	switch t := family.GetType(); t {
	case dto.MetricType_COUNTER:
		for _, m := range family.GetMetric() {
			values = append(values, MetricValue{
				Value:  m.GetCounter().GetValue(),
				Labels: labelsToMap(m.GetLabel()),
			})
		}
	case dto.MetricType_GAUGE:
		for _, m := range family.GetMetric() {
			values = append(values, MetricValue{
				Value:  m.GetGauge().GetValue(),
				Labels: labelsToMap(m.GetLabel()),
			})
		}
	case dto.MetricType_HISTOGRAM:
		for _, m := range family.GetMetric() {
			h := m.GetHistogram()
			values = append(values, MetricValue{
				Value:  float64(h.GetSampleCount()),
				Labels: mergeLabels(labelsToMap(m.GetLabel()), map[string]string{"_type": "count"}),
			})
			values = append(values, MetricValue{
				Value:  h.GetSampleSum(),
				Labels: mergeLabels(labelsToMap(m.GetLabel()), map[string]string{"_type": "sum"}),
			})
			// Also store bucket values for percentile computation
			for _, bucket := range h.GetBucket() {
				bound := bucket.GetUpperBound()
				values = append(values, MetricValue{
					Value: float64(bucket.GetCumulativeCount()),
					Labels: mergeLabels(labelsToMap(m.GetLabel()), map[string]string{
						"_type":  "bucket",
						"le":     formatBound(bound),
					}),
				})
			}
		}
	case dto.MetricType_SUMMARY, dto.MetricType_UNTYPED:
		// Handle as gauge for simplicity
		for _, m := range family.GetMetric() {
			var val float64
			if t == dto.MetricType_SUMMARY {
				val = m.GetSummary().GetSampleSum()
			} else {
				val = m.GetUntyped().GetValue()
			}
			values = append(values, MetricValue{
				Value:  val,
				Labels: labelsToMap(m.GetLabel()),
			})
		}
	}

	return values
}

// ParseHistogram extracts a histogram from parsed metrics.
func (p *Parser) ParseHistogram(metrics map[string][]MetricValue, name string, filterLabels map[string]string) (*model.Histogram, error) {
	values, ok := metrics[name]
	if !ok {
		return nil, fmt.Errorf("histogram %s not found", name)
	}

	hist := &model.Histogram{}
	bucketMap := make(map[float64]float64)

	for _, v := range values {
		if !matchesLabels(v.Labels, filterLabels) {
			continue
		}
		switch v.Labels["_type"] {
		case "count":
			hist.Count = v.Value
		case "sum":
			hist.Sum = v.Value
		case "bucket":
			le := v.Labels["le"]
			if le == "+Inf" {
				bucketMap[math.Inf(1)] = v.Value
			} else {
				var bound float64
				fmt.Sscanf(le, "%f", &bound)
				bucketMap[bound] = v.Value
			}
		}
	}

	// Convert bucket map to sorted slice
	var bounds []float64
	for b := range bucketMap {
		bounds = append(bounds, b)
	}
	sort.Float64s(bounds)

	for _, b := range bounds {
		hist.Buckets = append(hist.Buckets, model.HistogramBucket{
			UpperBound: b,
			Count:      bucketMap[b],
		})
	}

	return hist, nil
}

// HistogramQuantile computes the quantile from histogram buckets using linear interpolation.
// This implements the same algorithm as Prometheus's histogram_quantile() function.
func HistogramQuantile(q float64, buckets []model.HistogramBucket) float64 {
	if len(buckets) == 0 {
		return math.NaN()
	}

	// Find the total count (should be in the +Inf bucket)
	var totalCount float64
	for _, b := range buckets {
		if math.IsInf(b.UpperBound, 1) {
			totalCount = b.Count
			break
		}
	}
	if totalCount == 0 {
		return math.NaN()
	}

	// Handle single bucket case
	if len(buckets) == 1 {
		if math.IsInf(buckets[0].UpperBound, 1) {
			return math.NaN() // Only +Inf bucket, can't compute quantile
		}
		return buckets[0].UpperBound
	}

	target := q * totalCount
	var prevCount, prevBound float64

	for _, b := range buckets {
		if b.Count >= target {
			// Linear interpolation between this bucket and the previous
			if b.Count == prevCount {
				return prevBound
			}
			// Scale linearly between bounds based on count ratio
			scale := (target - prevCount) / (b.Count - prevCount)
			return prevBound + scale*(b.UpperBound-prevBound)
		}
		prevCount = b.Count
		prevBound = b.UpperBound
	}

	return prevBound
}

// labelsToMap converts Prometheus labels to a Go map.
func labelsToMap(labels []*dto.LabelPair) map[string]string {
	result := make(map[string]string)
	for _, l := range labels {
		result[l.GetName()] = l.GetValue()
	}
	return result
}

// mergeLabels merges two label maps.
func mergeLabels(a, b map[string]string) map[string]string {
	result := make(map[string]string)
	for k, v := range a {
		result[k] = v
	}
	for k, v := range b {
		result[k] = v
	}
	return result
}

// matchesLabels checks if a metric's labels match the filter.
func matchesLabels(labels, filter map[string]string) bool {
	for k, v := range filter {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// formatBound formats a bucket bound for the "le" label.
func formatBound(b float64) string {
	if math.IsInf(b, 1) {
		return "+Inf"
	}
	// Use simple format to avoid scientific notation for small numbers
	s := fmt.Sprintf("%.6f", b)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}

// ParseFromReader parses Prometheus exposition format from an io.Reader.
func (p *Parser) ParseFromReader(r io.Reader) (map[string][]MetricValue, error) {
	text, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read metrics: %w", err)
	}
	return p.Parse(string(text))
}
