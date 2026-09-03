package main

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/time/rate"

	"git.ardenone.com/jedarden/zai-proxy/proxy/config"
)

const rateLimitClientBucketCount = 64

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
	rateChanged        chan struct{}
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
	lastCeilingUpdate  time.Time // When estimatedCeiling was last learned (zero until then)
	recent429Count     int64
	recentSuccessCount int64

	// Ceiling persistence: statePath is the file the learned ceiling is
	// written to on every ceiling update and resumed from on startup. Empty
	// disables persistence (tests mostly).
	statePath         string
	restoredFromState bool

	// Fair scheduling happens before the global token bucket. The queues are
	// keyed by a fixed set of source buckets, so neither queue state nor metric
	// labels can grow with the number of callers.
	fairMu          sync.Mutex
	fairCond        *sync.Cond
	fairQueues      map[string][]*fairRequest
	fairOrder       []string
	fairNext        int
	fairDispatching bool
}

// A non-zero-size marker guarantees distinct request pointers, which lets a
// source queue distinguish consecutive requests without allocating an ID.
type fairRequest struct{ marker byte }

func NewAdaptiveRateLimiter(initialRate, minRate, maxRate float64) *AdaptiveRateLimiter {
	return NewAdaptiveRateLimiterWithWindow(initialRate, minRate, maxRate, 30*time.Second)
}

// NewAdaptiveRateLimiterWithWindow creates an AdaptiveRateLimiter with a configurable adjustment window.
// Use this for tests to inject shorter durations (e.g., 1ms or 100ms) for fast execution without sleeping.
func NewAdaptiveRateLimiterWithWindow(initialRate, minRate, maxRate float64, windowDuration time.Duration) *AdaptiveRateLimiter {
	arl := &AdaptiveRateLimiter{
		limiter:            rate.NewLimiter(rate.Limit(initialRate), limiterBurst(initialRate)),
		rateChanged:        make(chan struct{}),
		currentRate:        initialRate,
		minRate:            minRate,
		maxRate:            maxRate,
		estimatedCeiling:   maxRate, // Assume max until we learn otherwise
		ceilingSmoothAlpha: 0.3,     // 30% new observation, 70% history
		holdMargin:         0.02,    // Hold 2% below estimated ceiling
		probeInterval:      10,      // Probe every 10 clean windows (5 min at 30s windows)
		cleanWindows:       0,
		lastAdjustment:     time.Now(),
		adjustmentWindow:   windowDuration,
		fairQueues:         make(map[string][]*fairRequest),
	}
	arl.fairCond = sync.NewCond(&arl.fairMu)
	return arl
}

// Wait waits for a global token using the default source bucket. It is kept
// for callers that do not have an HTTP request from which to derive a source.
func (arl *AdaptiveRateLimiter) Wait(variant string) time.Duration {
	return arl.waitForClient(variant, rateLimitClientBucket(""))
}

// waitForClient schedules a request fairly among active source buckets before
// taking a token from the shared adaptive limiter. It does not add capacity:
// every request still obtains exactly one token from the existing global
// bucket. A source with queued work receives at most one turn before the next
// active source is considered.
func (arl *AdaptiveRateLimiter) waitForClient(variant, client string) time.Duration {
	start := time.Now()
	if client == "" {
		client = rateLimitClientBucket("")
	}

	request := &fairRequest{}
	arl.fairMu.Lock()
	if len(arl.fairQueues[client]) == 0 {
		arl.fairOrder = append(arl.fairOrder, client)
	}
	arl.fairQueues[client] = append(arl.fairQueues[client], request)
	for !arl.canDispatchLocked(client, request) {
		arl.fairCond.Wait()
	}
	arl.fairDispatching = true
	arl.fairMu.Unlock()

	arl.waitForGlobalToken()

	arl.fairMu.Lock()
	arl.completeDispatchLocked(client, request)
	arl.fairDispatching = false
	arl.fairCond.Broadcast()
	arl.fairMu.Unlock()

	waitTime := time.Since(start)
	rateLimitWaitTime.WithLabelValues(variant, client).Observe(waitTime.Seconds())
	return waitTime
}

// canDispatchLocked returns whether request owns the next round-robin turn.
// fairMu must be held by the caller.
func (arl *AdaptiveRateLimiter) canDispatchLocked(client string, request *fairRequest) bool {
	if arl.fairDispatching || len(arl.fairOrder) == 0 {
		return false
	}
	if arl.fairNext >= len(arl.fairOrder) {
		arl.fairNext = 0
	}
	queue := arl.fairQueues[client]
	return arl.fairOrder[arl.fairNext] == client && len(queue) > 0 && queue[0] == request
}

// completeDispatchLocked removes the served request and moves the round-robin
// cursor. Keeping the request at the head until its global token arrives means
// requests that arrive while that source is waiting cannot take another turn
// ahead of another already-queued source.
// fairMu must be held by the caller.
func (arl *AdaptiveRateLimiter) completeDispatchLocked(client string, request *fairRequest) {
	queue := arl.fairQueues[client]
	if len(queue) == 0 || queue[0] != request {
		panic("rate limiter fair queue lost its active request")
	}

	queue = queue[1:]
	if len(queue) > 0 {
		arl.fairQueues[client] = queue
		arl.fairNext = (arl.fairNext + 1) % len(arl.fairOrder)
		return
	}

	delete(arl.fairQueues, client)
	arl.fairOrder = append(arl.fairOrder[:arl.fairNext], arl.fairOrder[arl.fairNext+1:]...)
	if len(arl.fairOrder) == 0 || arl.fairNext >= len(arl.fairOrder) {
		arl.fairNext = 0
	}
}

// waitForGlobalToken preserves the adaptive limiter as the outer authority.
// A zero configured rate is intentionally an indefinite wait, matching the
// previous Wait behavior, but a Reset can wake it by replacing rateChanged.
func (arl *AdaptiveRateLimiter) waitForGlobalToken() {
	for {
		arl.mu.RLock()
		limiter := arl.limiter
		currentRate := arl.currentRate
		rateChanged := arl.rateChanged
		arl.mu.RUnlock()

		if currentRate <= 0 {
			<-rateChanged
			continue
		}

		if err := limiter.Wait(context.Background()); err == nil {
			return
		}

		// A positive rate always has a burst of at least one, so this branch
		// is defensive. Wait until a rate change instead of admitting a
		// request without a global token.
		<-rateChanged
	}
}

func limiterBurst(rateValue float64) int {
	if rateValue <= 0 {
		return 0
	}
	burst := int(rateValue * 2)
	if burst < 1 {
		return 1
	}
	return burst
}

// rateLimitClientBucket derives a stable, bounded source identity from the
// direct network peer. Do not use X-Forwarded-For or a user-controlled header
// here: those can be spoofed by callers and would let one caller manufacture
// arbitrary scheduler identities. Pod-to-service connections carry the pod IP
// in RemoteAddr in the supported cluster-internal deployment.
func rateLimitClientBucket(remoteAddr string) string {
	source := remoteAddr
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		source = host
	}
	if source == "" {
		source = "unknown"
	}

	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(source))
	return fmt.Sprintf("source-%02d", hasher.Sum32()%rateLimitClientBucketCount)
}

func (arl *AdaptiveRateLimiter) notifyRateChangeLocked() {
	close(arl.rateChanged)
	arl.rateChanged = make(chan struct{})
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

	if time.Since(arl.lastAdjustment) < arl.adjustmentWindow {
		arl.mu.Unlock()
		return
	}

	count429 := atomic.SwapInt64(&arl.recent429Count, 0)
	countSuccess := atomic.SwapInt64(&arl.recentSuccessCount, 0)
	total := count429 + countSuccess

	if total == 0 {
		arl.mu.Unlock()
		return
	}

	error429Rate := float64(count429) / float64(total)
	newRate := arl.currentRate
	ceilingUpdated := false

	if error429Rate > 0.05 {
		// 429s detected — update ceiling estimate via EWMA
		oldCeiling := arl.estimatedCeiling
		arl.estimatedCeiling = arl.ceilingSmoothAlpha*arl.currentRate + (1-arl.ceilingSmoothAlpha)*arl.estimatedCeiling
		arl.cleanWindows = 0
		arl.lastCeilingUpdate = time.Now()
		ceilingUpdated = true

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
		arl.limiter.SetBurst(limiterBurst(newRate))
		arl.notifyRateChangeLocked()
		rateLimitCurrentRate.WithLabelValues(deploymentVariant).Set(newRate)
	}

	arl.lastAdjustment = time.Now()

	// Snapshot the learned ceiling while the state is locked, then write it
	// after the lock is released — the write is disk I/O and must not widen
	// the critical section every waiter rounds through.
	var snapshot *RateLimitState
	if ceilingUpdated && arl.statePath != "" {
		snapshot = arl.stateSnapshotLocked()
	}
	arl.mu.Unlock()

	if snapshot != nil {
		arl.persistState(snapshot)
	}
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
	arl.limiter = rate.NewLimiter(rate.Limit(initialRate), limiterBurst(initialRate))
	arl.notifyRateChangeLocked()
	arl.lastAdjustment = time.Now()
	arl.lastCeilingUpdate = time.Time{}
	arl.restoredFromState = false
	atomic.StoreInt64(&arl.recent429Count, 0)
	atomic.StoreInt64(&arl.recentSuccessCount, 0)
	log.Printf("Rate limiter reset: rate=%.1f, ceiling=%.1f", arl.currentRate, arl.estimatedCeiling)

	// The snapshot no longer describes this limiter: drop it so a restart
	// starts over exactly as the reset asked, instead of resurrecting the
	// estimate that was just discarded.
	if arl.statePath != "" {
		if err := os.Remove(arl.statePath); err != nil && !os.IsNotExist(err) {
			log.Printf("Rate limit: failed to clear persisted ceiling state at %s: %v", arl.statePath, err)
		}
	}
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

	// Resume the inferred ceiling across restarts instead of re-learning it
	// from RATE_LIMIT_MAX (each restart otherwise re-runs a 40-60% 429 burst;
	// see docs/plan/plan.md, "Ceiling persistence across restarts"). The probe
	// loop is unchanged, so a genuinely higher upstream limit is still learned.
	statePath := config.GetRateLimitStateFile()
	stateMaxAge := config.GetRateLimitStateMaxAge()
	rateLimiter.statePath = statePath
	restored := rateLimiter.RestoreFromStateFile(statePath, stateMaxAge)

	rateLimitCurrentRate.WithLabelValues(deploymentVariant).Set(rateLimiter.GetCurrentRate())
	log.Printf("Adaptive rate limiting: initial=%.1f, min=%.1f, max=%.1f req/s (ceiling alpha=%.2f, margin=%.1f%%, probe every %d windows, state file=%s, max age=%s, restored=%t)",
		initialRate, minRate, maxRate, rateLimiter.ceilingSmoothAlpha, rateLimiter.holdMargin*100, rateLimiter.probeInterval, statePath, stateMaxAge, restored)

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
	// Reuse the configured limiter rather than constructing a second one in the
	// handler, so the configured EWMA parameters remain the outer fairness bound.
	proxyHandler.rateLimiter = rateLimiter

	// Metrics endpoint
	http.Handle("/metrics", promhttp.Handler())

	// Health endpoint: status code is what the probes read; the body carries
	// the live rate-limit state, mirroring the persisted ceiling snapshot.
	http.Handle("/health", newHealthHandler(rateLimiter))

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
