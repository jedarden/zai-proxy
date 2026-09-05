// Package collector implements the metrics collector that scrapes zai-proxy endpoints.
package collector

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"git.ardenone.com/jedarden/zai-proxy/dashboard/config"
	"git.ardenone.com/jedarden/zai-proxy/dashboard/model"
)

// Collector scrapes Prometheus metrics from zai-proxy endpoints.
type Collector struct {
	targets      []string
	client       *http.Client
	interval     time.Duration
	timeout      time.Duration
	parser       *Parser
	snapshots    chan *model.MetricSnapshot
	prevMetrics  map[string]map[string][]MetricValue // target -> metrics
	prevTime     map[string]time.Time
	mu           sync.RWMutex
	variantNames map[string]string // target URL -> variant name
}

// Config holds configuration for the collector.
type Config struct {
	Targets  []string      // URLs to scrape
	Interval time.Duration // Scrape interval
	Timeout  time.Duration // HTTP timeout per scrape
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		Targets:  config.GetScrapeTargets(),
		Interval: config.GetScrapeInterval(),
		Timeout:  config.GetScrapeTimeout(),
	}
}

// NewCollector creates a new Collector.
func NewCollector(cfg Config) *Collector {
	c := &Collector{
		targets:      cfg.Targets,
		interval:     cfg.Interval,
		timeout:      cfg.Timeout,
		parser:       NewParser(),
		snapshots:    make(chan *model.MetricSnapshot, 100),
		prevMetrics:  make(map[string]map[string][]MetricValue),
		prevTime:     make(map[string]time.Time),
		variantNames: make(map[string]string),
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}

	// Determine variant names from URLs
	for _, target := range cfg.Targets {
		if strings.Contains(target, "test") || strings.Contains(target, "canary") {
			c.variantNames[target] = "canary"
		} else {
			c.variantNames[target] = "production"
		}
	}

	return c
}

// Snapshots returns the channel for receiving metric snapshots.
func (c *Collector) Snapshots() <-chan *model.MetricSnapshot {
	return c.snapshots
}

// Start begins the collection loop.
func (c *Collector) Start(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collect(ctx)
		}
	}
}

// collect scrapes all targets and emits snapshots.
func (c *Collector) collect(ctx context.Context) {
	var wg sync.WaitGroup

	for _, target := range c.targets {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			snapshot, err := c.scrapeTarget(ctx, url)
			if err != nil {
				log.Printf("error scraping %s: %v", url, err)
				return
			}
			if snapshot != nil {
				select {
				case c.snapshots <- snapshot:
				default:
					log.Printf("snapshot channel full, dropping snapshot for %s", url)
				}
			}
		}(target)
	}

	wg.Wait()
}

// scrapeTarget scrapes a single target and returns a snapshot.
func (c *Collector) scrapeTarget(ctx context.Context, url string) (*model.MetricSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metrics: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	metrics, err := c.parser.ParseFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse metrics: %w", err)
	}

	now := time.Now()

	c.mu.RLock()
	prevMetrics := c.prevMetrics[url]
	prevTime := c.prevTime[url]
	c.mu.RUnlock()

	snapshot := c.buildSnapshot(metrics, prevMetrics, now, prevTime, c.variantNames[url])

	c.mu.Lock()
	c.prevMetrics[url] = metrics
	c.prevTime[url] = now
	c.mu.Unlock()

	return snapshot, nil
}

// buildSnapshot constructs a MetricSnapshot from parsed metrics.
func (c *Collector) buildSnapshot(cur, prev map[string][]MetricValue, now, prevTime time.Time, variant string) *model.MetricSnapshot {
	elapsed := now.Sub(prevTime).Seconds()
	if elapsed <= 0 || prevTime.IsZero() {
		elapsed = 0
	}

	s := &model.MetricSnapshot{
		Timestamp: now.UnixMilli(),
		Variant:   variant,
	}

	// Helper functions for metric extraction
	sumMetric := func(name string, filter map[string]string) float64 {
		var total float64
		for _, v := range cur[name] {
			if matchesLabels(v.Labels, filter) {
				total += v.Value
			}
		}
		return total
	}

	prefixSum := func(name, key, prefix string) float64 {
		var total float64
		for _, v := range cur[name] {
			if strings.HasPrefix(v.Labels[key], prefix) {
				total += v.Value
			}
		}
		return total
	}

	rate := func(name string, filter map[string]string) float64 {
		if elapsed <= 0 || prev == nil {
			return 0
		}
		curVal := sumMetric(name, filter)
		prevVal := 0.0
		for _, v := range prev[name] {
			if matchesLabels(v.Labels, filter) {
				prevVal += v.Value
			}
		}
		delta := curVal - prevVal
		// Handle counter reset
		if delta < 0 {
			delta = curVal
		}
		return delta / elapsed
	}

	prefixRate := func(name, key, prefix string) float64 {
		if elapsed <= 0 || prev == nil {
			return 0
		}
		curVal := prefixSum(name, key, prefix)
		prevVal := 0.0
		for _, v := range prev[name] {
			if strings.HasPrefix(v.Labels[key], prefix) {
				prevVal += v.Value
			}
		}
		delta := curVal - prevVal
		if delta < 0 {
			delta = curVal
		}
		return delta / elapsed
	}

	// Counter metrics
	s.Requests2xx = sumMetric("zai_proxy_requests_total", map[string]string{})
	s.Requests4xx = sumMetric("zai_proxy_requests_total", map[string]string{})
	s.Requests5xx = sumMetric("zai_proxy_requests_total", map[string]string{})

	// Actually, we need to filter by status_code prefix
	s.Requests2xx = prefixSum("zai_proxy_requests_total", "status_code", "2")
	s.Requests4xx = prefixSum("zai_proxy_requests_total", "status_code", "4")
	s.Requests5xx = prefixSum("zai_proxy_requests_total", "status_code", "5")

	s.TokensInput = sumMetric("zai_proxy_tokens_total", map[string]string{"direction": "input"})
	s.TokensOutput = sumMetric("zai_proxy_tokens_total", map[string]string{"direction": "output"})
	s.TokensCacheRead = sumMetric("zai_proxy_tokens_total", map[string]string{"direction": "cache_read"})
	s.TokensCacheWrite = sumMetric("zai_proxy_tokens_total", map[string]string{"direction": "cache_write"})
	s.EstimatedCostUSDInput = sumMetric("zai_proxy_estimated_cost_usd_total", map[string]string{"direction": "input"})
	s.EstimatedCostUSDOutput = sumMetric("zai_proxy_estimated_cost_usd_total", map[string]string{"direction": "output"})
	s.EstimatedCostUSDCacheRead = sumMetric("zai_proxy_estimated_cost_usd_total", map[string]string{"direction": "cache_read"})
	s.EstimatedCostUSDCacheWrite = sumMetric("zai_proxy_estimated_cost_usd_total", map[string]string{"direction": "cache_write"})
	s.ConcurrentRequests = sumMetric("zai_proxy_concurrent_requests", nil)
	s.MaxWorkers = sumMetric("zai_proxy_max_workers", nil)
	s.RateLimitRps = sumMetric("zai_proxy_rate_limit_requests_per_second", nil)
	s.RateLimitRejections = sumMetric("zai_proxy_rate_limit_rejections_total", nil)
	s.RateLimitAdjIncrease = sumMetric("zai_proxy_rate_limit_adjustments_total", map[string]string{"direction": "increase"})
	s.RateLimitAdjDecrease = sumMetric("zai_proxy_rate_limit_adjustments_total", map[string]string{"direction": "decrease"})
	s.UpstreamErrors = sumMetric("zai_proxy_upstream_errors_total", nil)
	s.RetryAttempts = sumMetric("zai_proxy_retry_attempts_total", nil)
	s.WorkerUtilization = sumMetric("zai_proxy_worker_utilization_ratio", nil)

	// Rate computations
	s.ReqRate = prefixRate("zai_proxy_requests_total", "status_code", "")
	s.TokenRateIn = rate("zai_proxy_tokens_total", map[string]string{"direction": "input"})
	s.TokenRateOut = rate("zai_proxy_tokens_total", map[string]string{"direction": "output"})
	s.TokenRateCacheRead = rate("zai_proxy_tokens_total", map[string]string{"direction": "cache_read"})
	s.TokenRateCacheWrite = rate("zai_proxy_tokens_total", map[string]string{"direction": "cache_write"})

	// Per-status-code rates
	s.StatusCodeRates = make(map[string]float64)
	if elapsed > 0 && prev != nil {
		curByCode := make(map[string]float64)
		for _, v := range cur["zai_proxy_requests_total"] {
			code := v.Labels["status_code"]
			if code == "" {
				continue
			}
			curByCode[code] += v.Value
		}
		prevByCode := make(map[string]float64)
		for _, v := range prev["zai_proxy_requests_total"] {
			code := v.Labels["status_code"]
			if code == "" {
				continue
			}
			prevByCode[code] += v.Value
		}
		for code, curVal := range curByCode {
			delta := curVal - prevByCode[code]
			if delta < 0 {
				delta = curVal // counter reset
			}
			s.StatusCodeRates[code] = delta / elapsed
		}
	}

	// Error rate percentage
	totalReqs := s.Requests2xx + s.Requests4xx + s.Requests5xx
	if totalReqs > 0 {
		s.ErrorRatePct = (s.Requests5xx / totalReqs) * 100
	}

	// Histogram percentiles
	durationHist, err := c.parser.ParseHistogram(cur, "zai_proxy_request_duration_seconds", nil)
	if err == nil {
		s.LatencyP50 = HistogramQuantile(0.50, durationHist.Buckets) * 1000 // Convert to ms
		s.LatencyP95 = HistogramQuantile(0.95, durationHist.Buckets) * 1000
		s.LatencyP99 = HistogramQuantile(0.99, durationHist.Buckets) * 1000
	}

	// Average request/response sizes from histograms
	reqSizeHist, err := c.parser.ParseHistogram(cur, "zai_proxy_request_size_bytes", nil)
	if err == nil && reqSizeHist.Count > 0 {
		s.RequestSizeAvg = reqSizeHist.Sum / reqSizeHist.Count
	}
	respSizeHist, err := c.parser.ParseHistogram(cur, "zai_proxy_response_size_bytes", nil)
	if err == nil && respSizeHist.Count > 0 {
		s.ResponseSizeAvg = respSizeHist.Sum / respSizeHist.Count
	}

	// Provider quota telemetry (observe-only gauges on the proxy). Absent
	// series leave s.Quota nil so missing data stays explicit end to end.
	s.Quota = buildQuotaState(cur)

	return s
}

// quotaWindows is the bounded set of provider windows the dashboard displays,
// mirroring the proxy's documented window enum.
var quotaWindows = []string{"five_hour", "weekly"}

// quotaMetricNames are the per-window quota gauges, in the order fields are
// filled from the selected provider schema.
const (
	quotaUsageMetric   = "zai_proxy_quota_usage_ratio"
	quotaRemainMetric  = "zai_proxy_quota_remaining_ratio"
	quotaResetMetric   = "zai_proxy_quota_reset_time_seconds"
	quotaAgeMetric     = "zai_proxy_quota_sample_age_seconds"
	quotaGateMetric    = "zai_proxy_quota_gate_open"
	quotaRateCapMetric = "zai_proxy_quota_rate_cap"
)

// quotaLimitTypePreference is the deterministic order in which one provider
// schema is picked per window when a scrape carries more than one. A provider
// schema switch leaves the previous gauge series exported until the proxy
// restarts; CREDIT_LIMIT is the provider's current schema. Anything else
// falls back to the lexicographically smallest label so the choice stays
// stable across scrapes.
var quotaLimitTypePreference = []string{"CREDIT_LIMIT", "TOKENS_LIMIT"}

// buildQuotaState extracts the quota block from one scrape. It returns nil
// when the proxy exported no quota series, so "no telemetry" is distinguishable
// from "telemetry present but individual observations missing". Each window's
// fields come from a single provider schema; a field that schema did not
// advertise stays nil rather than being merged across schemas. The proxy's
// own variant label is not filtered on: one scrape target is one proxy
// instance, matching how every other metric here is aggregated.
func buildQuotaState(cur map[string][]MetricValue) *model.QuotaState {
	state := &model.QuotaState{}
	for _, window := range quotaWindows {
		windowState := buildQuotaWindowState(cur, window)
		switch window {
		case "five_hour":
			state.FiveHour = windowState
		case "weekly":
			state.Weekly = windowState
		}
	}

	// The age gauge describes the last valid sample; the maximum across
	// exported series is the conservative freshness reading.
	if ages := cur[quotaAgeMetric]; len(ages) > 0 {
		oldest := ages[0].Value
		for _, v := range ages[1:] {
			if v.Value > oldest {
				oldest = v.Value
			}
		}
		state.SampleAgeSeconds = &oldest
	}

	// Gate and rate-cap series exist only when enforcement is wired; their
	// presence is the observe-only/enforcement signal.
	if gates, ok := cur[quotaGateMetric]; ok && len(gates) > 0 {
		open := false
		for _, v := range gates {
			if v.Value >= 1 {
				open = true
			}
		}
		state.GateOpen = &open
		state.Enforcement = true
	}
	if caps, ok := cur[quotaRateCapMetric]; ok && len(caps) > 0 {
		state.Enforcement = true
	}

	if state.FiveHour == nil && state.Weekly == nil &&
		state.SampleAgeSeconds == nil && !state.Enforcement {
		return nil
	}
	return state
}

// buildQuotaWindowState builds one window's state, or nil when the scrape
// carries no series for that window.
func buildQuotaWindowState(cur map[string][]MetricValue, window string) *model.QuotaWindowState {
	usage := quotaSeriesForWindow(cur[quotaUsageMetric], window)
	remaining := quotaSeriesForWindow(cur[quotaRemainMetric], window)
	resets := quotaSeriesForWindow(cur[quotaResetMetric], window)
	if len(usage) == 0 && len(remaining) == 0 && len(resets) == 0 {
		return nil
	}

	limitType := selectQuotaLimitType(usage, remaining, resets)
	state := &model.QuotaWindowState{LimitType: limitType}

	for _, v := range usage {
		if v.Labels["limit_type"] == limitType {
			ratio := v.Value
			state.UsageRatio = &ratio
			break
		}
	}
	for _, v := range remaining {
		if v.Labels["limit_type"] == limitType {
			ratio := v.Value
			state.RemainingRatio = &ratio
			break
		}
	}
	for _, v := range resets {
		if v.Labels["limit_type"] != limitType {
			continue
		}
		// A non-positive stamp is not a reset time; the proxy never records
		// one, so treat it as absent rather than as an epoch-reset countdown.
		if seconds := int64(v.Value); seconds > 0 {
			state.ResetTimeUnix = &seconds
		}
		break
	}
	return state
}

// quotaSeriesForWindow keeps only series whose window label matches. The
// proxy collapses unrecognized windows into "unknown", which never matches a
// documented window and so never reaches the dashboard.
func quotaSeriesForWindow(values []MetricValue, window string) []MetricValue {
	var matched []MetricValue
	for _, v := range values {
		if v.Labels["window"] == window {
			matched = append(matched, v)
		}
	}
	return matched
}

// selectQuotaLimitType picks the one provider schema a window's fields are
// read from, preferring the documented current schema and falling back to
// the lexicographically smallest label for stability.
func selectQuotaLimitType(seriesSets ...[]MetricValue) string {
	present := make(map[string]struct{})
	for _, series := range seriesSets {
		for _, v := range series {
			if lt := v.Labels["limit_type"]; lt != "" {
				present[lt] = struct{}{}
			}
		}
	}
	if len(present) == 0 {
		return ""
	}
	for _, preferred := range quotaLimitTypePreference {
		if _, ok := present[preferred]; ok {
			return preferred
		}
	}
	smallest := ""
	for lt := range present {
		if smallest == "" || lt < smallest {
			smallest = lt
		}
	}
	return smallest
}
