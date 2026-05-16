// Package collector implements the metrics collector that scrapes zai-proxy endpoints.
package collector

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"git.ardenone.com/jedarden/zai-proxy/dashboard/model"
)

// Collector scrapes Prometheus metrics from zai-proxy endpoints.
type Collector struct {
	targets       []string
	client        *http.Client
	interval      time.Duration
	timeout       time.Duration
	parser        *Parser
	snapshots     chan *model.MetricSnapshot
	prevMetrics   map[string]map[string][]MetricValue // target -> metrics
	prevTime      map[string]time.Time
	mu            sync.RWMutex
	variantNames  map[string]string // target URL -> variant name
}

// Config holds configuration for the collector.
type Config struct {
	Targets  []string      // URLs to scrape
	Interval time.Duration // Scrape interval
	Timeout  time.Duration // HTTP timeout per scrape
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	targets := os.Getenv("SCRAPE_TARGETS")
	if targets == "" {
		targets = "http://zai-proxy.mcp.svc.cluster.local:8080/metrics"
	}

	interval := 5 * time.Second
	if v := os.Getenv("SCRAPE_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			interval = d
		}
	}

	timeout := 3 * time.Second
	if v := os.Getenv("SCRAPE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			timeout = d
		}
	}

	return Config{
		Targets:  strings.Split(targets, ","),
		Interval: interval,
		Timeout:  timeout,
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

	return s
}

// parseFloatEnv parses a float from an environment variable.
func parseFloatEnv(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}
