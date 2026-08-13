package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/time/rate"

	"git.ardenone.com/jedarden/zai-proxy/proxy/config"
)

var (
	currentRequests   int64
	maxWorkersValue   int64
	tokenCounter      TokenCounter
	tokenizerModel    string
	deploymentVariant string

	// Build info — set via -ldflags at build time
	buildVersion string
	buildCommit  string
	buildTimeStr string
)

// AdaptiveRateLimiter manages rate limiting by tracking the upstream 429 ceiling
// and holding just below it. Periodically probes above to detect ceiling shifts.
type AdaptiveRateLimiter struct {
	limiter            *rate.Limiter
	mu                 sync.RWMutex
	currentRate        float64
	minRate            float64
	maxRate            float64
	estimatedCeiling   float64 // EWMA of the rate at which 429s occur
	ceilingSmoothAlpha float64 // EWMA smoothing factor (0-1, higher = more reactive)
	holdMargin         float64 // Hold this fraction below ceiling (e.g., 0.02 = 2%)
	probeInterval      int     // Probe above ceiling every N clean windows
	cleanWindows       int     // Consecutive clean windows since last 429
	lastAdjustment     time.Time
	adjustmentWindow   time.Duration
	recent429Count     int64
	recentSuccessCount int64
}

func NewAdaptiveRateLimiter(initialRate, minRate, maxRate float64) *AdaptiveRateLimiter {
	return NewAdaptiveRateLimiterWithWindow(initialRate, minRate, maxRate, 30*time.Second)
}

// NewAdaptiveRateLimiterWithWindow creates an AdaptiveRateLimiter with a configurable adjustment window.
// Use this for tests to inject shorter durations (e.g., 1ms or 100ms) for fast execution without sleeping.
func NewAdaptiveRateLimiterWithWindow(initialRate, minRate, maxRate float64, windowDuration time.Duration) *AdaptiveRateLimiter {
	return &AdaptiveRateLimiter{
		limiter:            rate.NewLimiter(rate.Limit(initialRate), int(initialRate*2)),
		currentRate:        initialRate,
		minRate:            minRate,
		maxRate:            maxRate,
		estimatedCeiling:   maxRate,              // Assume max until we learn otherwise
		ceilingSmoothAlpha: 0.3,                  // 30% new observation, 70% history
		holdMargin:         0.02,                 // Hold 2% below estimated ceiling
		probeInterval:      10,                   // Probe every 10 clean windows (5 min at 30s windows)
		cleanWindows:       0,
		lastAdjustment:     time.Now(),
		adjustmentWindow:   windowDuration,
	}
}

func (arl *AdaptiveRateLimiter) Wait(variant string) time.Duration {
	start := time.Now()
	// Protect access to limiter with read lock to prevent race with tryAdjustRate()
	arl.mu.RLock()
	limiter := arl.limiter
	arl.mu.RUnlock()
	limiter.Wait(context.Background())
	waitTime := time.Since(start)
	rateLimitWaitTime.WithLabelValues(variant).Observe(waitTime.Seconds())
	return waitTime
}

func (arl *AdaptiveRateLimiter) Record429() {
	atomic.AddInt64(&arl.recent429Count, 1)
	arl.tryAdjustRate()
}

func (arl *AdaptiveRateLimiter) RecordSuccess() {
	atomic.AddInt64(&arl.recentSuccessCount, 1)
	arl.tryAdjustRate()
}

func (arl *AdaptiveRateLimiter) tryAdjustRate() {
	arl.mu.Lock()
	defer arl.mu.Unlock()

	if time.Since(arl.lastAdjustment) < arl.adjustmentWindow {
		return
	}

	count429 := atomic.SwapInt64(&arl.recent429Count, 0)
	countSuccess := atomic.SwapInt64(&arl.recentSuccessCount, 0)
	total := count429 + countSuccess

	if total == 0 {
		return
	}

	error429Rate := float64(count429) / float64(total)
	newRate := arl.currentRate

	if error429Rate > 0.05 {
		// 429s detected — update ceiling estimate via EWMA
		oldCeiling := arl.estimatedCeiling
		arl.estimatedCeiling = arl.ceilingSmoothAlpha*arl.currentRate + (1-arl.ceilingSmoothAlpha)*arl.estimatedCeiling
		arl.cleanWindows = 0

		// Drop to hold position: just below the updated ceiling
		newRate = arl.estimatedCeiling * (1 - arl.holdMargin)
		if newRate < arl.minRate {
			newRate = arl.minRate
		}
		log.Printf("Rate limit: Ceiling updated %.2f → %.2f req/s, holding at %.2f req/s (429 rate: %.2f%%)",
			oldCeiling, arl.estimatedCeiling, newRate, error429Rate*100)
		rateLimitAdjustments.WithLabelValues("decrease", deploymentVariant).Inc()

	} else if error429Rate < 0.01 {
		arl.cleanWindows++
		targetRate := arl.estimatedCeiling * (1 - arl.holdMargin)

		if arl.probeInterval > 0 && arl.cleanWindows >= arl.probeInterval && arl.currentRate < arl.maxRate {
			// Probe: the ceiling may have shifted up. Step above our hold point
			// to test for higher throughput.
			probeRate := arl.estimatedCeiling * (1 + arl.holdMargin)
			if probeRate > arl.maxRate {
				probeRate = arl.maxRate
			}
			newRate = probeRate
			arl.cleanWindows = 0
			log.Printf("Rate limit: Probing ceiling at %.2f req/s (estimated ceiling: %.2f, clean windows: %d)",
				newRate, arl.estimatedCeiling, arl.probeInterval)
			rateLimitAdjustments.WithLabelValues("probe", deploymentVariant).Inc()

		} else if arl.currentRate < targetRate {
			// Below hold point — move toward it quickly
			gap := targetRate - arl.currentRate
			step := gap * 0.5 // Close half the gap each window
			if step < 0.25 {
				step = 0.25
			}
			newRate = arl.currentRate + step
			if newRate > targetRate {
				newRate = targetRate
			}
			log.Printf("Rate limit: Converging to %.2f req/s (target: %.2f, ceiling: %.2f)",
				newRate, targetRate, arl.estimatedCeiling)
			rateLimitAdjustments.WithLabelValues("increase", deploymentVariant).Inc()
		}
		// At or above target with no 429s — hold steady, no log spam
	}

	if newRate != arl.currentRate {
		arl.currentRate = newRate
		arl.limiter.SetLimit(rate.Limit(newRate))
		arl.limiter.SetBurst(int(newRate * 2))
		rateLimitCurrentRate.WithLabelValues(deploymentVariant).Set(newRate)
	}

	arl.lastAdjustment = time.Now()
}

func (arl *AdaptiveRateLimiter) GetCurrentRate() float64 {
	arl.mu.RLock()
	defer arl.mu.RUnlock()
	return arl.currentRate
}

func (arl *AdaptiveRateLimiter) Reset(initialRate float64) {
	arl.mu.Lock()
	defer arl.mu.Unlock()
	arl.currentRate = initialRate
	arl.estimatedCeiling = initialRate
	arl.cleanWindows = 0
	arl.limiter = rate.NewLimiter(rate.Limit(initialRate), int(initialRate*2))
	arl.lastAdjustment = time.Now()
	atomic.StoreInt64(&arl.recent429Count, 0)
	atomic.StoreInt64(&arl.recentSuccessCount, 0)
	log.Printf("Rate limiter reset: rate=%.1f, ceiling=%.1f", arl.currentRate, arl.estimatedCeiling)
}

func updateUtilization() {
	current := atomic.LoadInt64(&currentRequests)
	max := atomic.LoadInt64(&maxWorkersValue)
	if max > 0 {
		utilization := float64(current) / float64(max)
		workerUtilization.WithLabelValues(deploymentVariant).Set(utilization)
	}
}

func main() {
	apiKey := os.Getenv("ZAI_API_KEY")
	if apiKey == "" {
		log.Fatal("ZAI_API_KEY environment variable required")
	}

	// Read deployment variant from environment
	deploymentVariant = config.GetDeploymentVariant()
	if deploymentVariant != config.DefaultDeploymentVariant {
		log.Printf("Deployment variant: %s", deploymentVariant)
	}

	// Read build info — prefer ldflags, fall back to env vars, then "unknown"
	version := buildVersion
	if version == "" {
		version = os.Getenv("ZAI_PROXY_VERSION")
	}
	if version == "" {
		version = "unknown"
	}
	commit := buildCommit
	if commit == "" {
		commit = os.Getenv("ZAI_PROXY_COMMIT")
	}
	if commit == "" {
		commit = "unknown"
	}
	buildTime := buildTimeStr
	if buildTime == "" {
		buildTime = os.Getenv("ZAI_PROXY_BUILD_TIME")
	}
	if buildTime == "" {
		buildTime = "unknown"
	}

	// Set build info metric
	buildInfo.WithLabelValues(version, deploymentVariant, commit, buildTime).Set(1)
	log.Printf("Build info: version=%s, variant=%s, commit=%s, build_time=%s", version, deploymentVariant, commit, buildTime)

	// Read tokenizer configuration from environment
	//
	// TOKEN_COUNTING_ENABLED: Enable/disable token counting (default: true)
	//   Set to "false" or "0" to disable token counting entirely.
	//   When disabled, no token metrics are collected and tokenizer is not initialized.
	//
	// TOKENIZER_MODEL: Model name for Prometheus metrics labels (default: glm-4)
	//   Used to tag token count metrics in Prometheus (e.g., glm-4, claude-3, etc.)
	//   This is purely for metrics labeling and does not affect tokenization algorithm.
	tokenCountingEnabled := config.GetTokenCountingEnabled()
	tokenizerModel = config.GetTokenizerModel()

	// Initialize tokenizer with tiktoken cl100k_base encoding
	if tokenCountingEnabled {
		tikTokenCounter, err := NewTikTokenCounter()
		if err != nil {
			log.Printf("Warning: Failed to initialize TikToken counter: %v", err)
			log.Println("Falling back to SimpleTokenCounter")
			tokenCounter = NewSimpleTokenCounter()
			log.Printf("Token counting enabled (fallback mode, model: %s)", tokenizerModel)
		} else {
			tokenCounter = tikTokenCounter
			log.Printf("Token counting enabled (tiktoken cl100k_base encoding, model: %s)", tokenizerModel)
		}
	} else {
		log.Println("Token counting disabled (TOKEN_COUNTING_ENABLED=false)")
		tokenCounter = nil
	}

	// Read max workers from environment
	maxWorkersValue = config.GetMaxWorkers()
	maxWorkers.WithLabelValues(deploymentVariant).Set(float64(maxWorkersValue))
	log.Printf("Max workers: %d", maxWorkersValue)

	// Initialize adaptive rate limiter
	initialRate := config.GetRateLimitInitial()
	minRate := config.GetRateLimitMin()
	maxRate := config.GetRateLimitMax()

	rateLimiter := NewAdaptiveRateLimiter(initialRate, minRate, maxRate)
	rateLimiter.ceilingSmoothAlpha = config.GetRateLimitCeilingAlpha()
	rateLimiter.holdMargin = config.GetRateLimitHoldMargin()
	rateLimiter.probeInterval = config.GetRateLimitProbeInterval()
	rateLimitCurrentRate.WithLabelValues(deploymentVariant).Set(initialRate)
	log.Printf("Adaptive rate limiting: initial=%.1f, min=%.1f, max=%.1f req/s (ceiling alpha=%.2f, margin=%.1f%%, probe every %d windows)",
		initialRate, minRate, maxRate, rateLimiter.ceilingSmoothAlpha, rateLimiter.holdMargin*100, rateLimiter.probeInterval)

	// Retry configuration
	maxRetries := config.GetMaxRetries()

	target := config.GetTargetURL()

	// Create the proxy handler with all configuration
	proxyHandler := NewProxyHandler(
		apiKey,
		target,
		maxRetries,
		maxWorkersValue,
		deploymentVariant,
		tokenCounter,
		tokenizerModel,
		initialRate,
		minRate,
		maxRate,
	)

	// Metrics endpoint
	http.Handle("/metrics", promhttp.Handler())

	// Health endpoint
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Admin: reset rate limiter
	http.HandleFunc("/admin/reset-rate-limit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		proxyHandler.ResetRateLimit(initialRate)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "reset",
			"current_rate": proxyHandler.GetCurrentRate(),
		})
	})

	// Proxy handler with adaptive rate limiting
	http.Handle("/", proxyHandler)
	log.Println("Z.AI proxy listening on :8080")
	log.Println("Metrics available at :8080/metrics")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
