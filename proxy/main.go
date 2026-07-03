package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
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

		if arl.cleanWindows >= arl.probeInterval && arl.currentRate < arl.maxRate {
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

	client := &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

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
		rateLimiter.Reset(initialRate)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "reset",
			"current_rate": rateLimiter.GetCurrentRate(),
		})
	})

	// Proxy handler with adaptive rate limiting
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Increment concurrent requests
		current := atomic.AddInt64(&currentRequests, 1)
		concurrentRequests.WithLabelValues(deploymentVariant).Set(float64(current))
		updateUtilization()

		defer func() {
			current := atomic.AddInt64(&currentRequests, -1)
			concurrentRequests.WithLabelValues(deploymentVariant).Set(float64(current))
			updateUtilization()
		}()

		// Check if we're at max capacity
		max := atomic.LoadInt64(&maxWorkersValue)
		if current > max {
			log.Printf("Max workers exceeded: %d/%d", current, max)
			requestsTotal.WithLabelValues(r.Method, r.URL.Path, "503", deploymentVariant).Inc()
			http.Error(w, "Service at capacity", http.StatusServiceUnavailable)
			return
		}

		// Apply rate limiting
		rateLimiter.Wait(deploymentVariant)

		// Track request size
		if r.ContentLength > 0 {
			requestSize.WithLabelValues(r.Method, r.URL.Path, deploymentVariant).Observe(float64(r.ContentLength))
		}

		// Capture request body (always, for translation + optional token counting).
		var requestBody []byte
		if r.Body != nil {
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, r.Body); err != nil {
				log.Printf("Error reading request body: %v", err)
			} else {
				requestBody = buf.Bytes()
			}
			r.Body.Close()
		}

		// Extract model name from request body for metrics labels.
		reqModel := tokenizerModel
		if len(requestBody) > 0 {
			var rb RequestBody
			if err := json.Unmarshal(requestBody, &rb); err == nil && rb.Model != "" {
				reqModel = rb.Model
			}
		}

		// Count input tokens if enabled.
		var inputTokens int
		if tokenCounter != nil && len(requestBody) > 0 {
			countStart := time.Now()
			inputTokens, _ = CountRequestTokens(requestBody, tokenCounter)
			countDuration := time.Since(countStart)
			tokenCountDuration.WithLabelValues(deploymentVariant).Observe(countDuration.Seconds())
			// Input tokens recorded after response completes via RecordUsage.
		}

		// Translate request body: strip Anthropic API fields ZhipuAI doesn't support.
		translatedBody := requestBody
		if len(requestBody) > 0 {
			if translated, changed, err := TranslateRequest(requestBody); err != nil {
				log.Printf("Warning: failed to translate request body: %v", err)
			} else if changed {
				translatedBody = translated
			}
		}

		// Retry logic for transient errors
		var lastErr error
		var resp *http.Response
		var validatedBody []byte // Pre-validated non-streaming body (read inside retry loop)
			var streamingPeek []byte   // First bytes peeked from streaming response (read inside retry loop)

		for attempt := 0; attempt <= maxRetries; attempt++ {
			if attempt > 0 {
				// Exponential backoff: 1s, 2s, 4s, etc.
				backoffDuration := time.Duration(1<<uint(attempt-1)) * time.Second
				log.Printf("Retry attempt %d/%d after %v", attempt, maxRetries, backoffDuration)
				time.Sleep(backoffDuration)
				retryAttempts.WithLabelValues("retry", deploymentVariant).Inc()
			}

			upstreamURL := target + r.URL.Path
			if r.URL.RawQuery != "" {
				upstreamURL += "?" + r.URL.RawQuery
			}

			var reqBodyReader io.Reader
			if len(translatedBody) > 0 {
				reqBodyReader = bytes.NewReader(translatedBody)
			}
			upstreamReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, reqBodyReader)
			if err != nil {
				log.Printf("Error creating request: %v", err)
				upstreamErrors.WithLabelValues("request_creation", deploymentVariant).Inc()
				requestsTotal.WithLabelValues(r.Method, r.URL.Path, "400", deploymentVariant).Inc()
				requestDuration.WithLabelValues(r.Method, r.URL.Path, "400", deploymentVariant).Observe(time.Since(start).Seconds())
				http.Error(w, "Bad request", http.StatusBadRequest)
				return
			}

			for key, values := range r.Header {
				for _, value := range values {
					upstreamReq.Header.Add(key, value)
				}
			}

			upstreamReq.Header.Set("Host", "api.z.ai")
			upstreamReq.Header.Set("Authorization", "Bearer "+apiKey)
			// Disable gzip so the proxy can parse/modify the response body
			upstreamReq.Header.Set("Accept-Encoding", "identity")

			resp, err = client.Do(upstreamReq)
			if err != nil {
				lastErr = err
				log.Printf("Error forwarding request (attempt %d/%d): %v", attempt+1, maxRetries+1, err)
				upstreamErrors.WithLabelValues("upstream_connection", deploymentVariant).Inc()

				// Retry on network errors
				if attempt < maxRetries {
					retryAttempts.WithLabelValues("network_error", deploymentVariant).Inc()
					continue
				}

				requestsTotal.WithLabelValues(r.Method, r.URL.Path, "502", deploymentVariant).Inc()
				requestDuration.WithLabelValues(r.Method, r.URL.Path, "502", deploymentVariant).Observe(time.Since(start).Seconds())
				http.Error(w, "Upstream error", http.StatusBadGateway)
				return
			}

			// Handle 429 Rate Limit
			if resp.StatusCode == 429 {
				resp.Body.Close()
				rateLimiter.Record429()

				// Check Retry-After header
				retryAfter := resp.Header.Get("Retry-After")
				if retryAfter != "" {
					if seconds, err := strconv.Atoi(retryAfter); err == nil {
						log.Printf("429 Rate Limited, retry after %d seconds", seconds)
						time.Sleep(time.Duration(seconds) * time.Second)
					}
				}

				if attempt < maxRetries {
					log.Printf("429 Rate Limited, retrying (attempt %d/%d)", attempt+1, maxRetries+1)
					retryAttempts.WithLabelValues("429", deploymentVariant).Inc()
					continue
				}

				// Exceeded max retries, return 429 to client
				log.Printf("429 Rate Limited, max retries exceeded")
				requestsTotal.WithLabelValues(r.Method, r.URL.Path, "429", deploymentVariant).Inc()
				requestDuration.WithLabelValues(r.Method, r.URL.Path, "429", deploymentVariant).Observe(time.Since(start).Seconds())
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			// Handle 422 Unprocessable Entity — log full bodies for diagnosis,
			// then return a clear error to the client so callers can fail fast.
			// 422s are not retried: they indicate a structural problem with the
			// request body that retrying won't fix.
			if resp.StatusCode == 422 {
				respBody, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				log.Printf("422 from upstream — request body: %s", string(requestBody))
				log.Printf("422 from upstream — response body: %s", string(respBody))
				upstreamErrors.WithLabelValues("422", deploymentVariant).Inc()
				requestsTotal.WithLabelValues(r.Method, r.URL.Path, "422", deploymentVariant).Inc()
				requestDuration.WithLabelValues(r.Method, r.URL.Path, "422", deploymentVariant).Observe(time.Since(start).Seconds())
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnprocessableEntity)
				w.Write(respBody)
				return
			}

			// Validate response body before committing to the client.
			// Z.AI occasionally returns HTTP 200 with empty or truncated JSON.
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				if !IsStreamingRequest(requestBody) {
					// Non-streaming: validate entire body
					bodyBytes, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					if len(bodyBytes) == 0 || !json.Valid(bodyBytes) {
						log.Printf("Malformed response from upstream (empty=%v, size=%d), retrying (attempt %d/%d)", len(bodyBytes) == 0, len(bodyBytes), attempt+1, maxRetries+1)
						upstreamErrors.WithLabelValues("truncated_response", deploymentVariant).Inc()
						if attempt < maxRetries {
							retryAttempts.WithLabelValues("truncated_response", deploymentVariant).Inc()
							continue
						}
						log.Printf("Malformed response from upstream, max retries exceeded - returning 502")
						requestsTotal.WithLabelValues(r.Method, r.URL.Path, "502", deploymentVariant).Inc()
						requestDuration.WithLabelValues(r.Method, r.URL.Path, "502", deploymentVariant).Observe(time.Since(start).Seconds())
						http.Error(w, "Upstream returned empty or malformed response after retries", http.StatusBadGateway)
						return
					}
					validatedBody = bodyBytes
				} else {
					// Streaming: peek at first chunk to confirm the response has data
					peekBuf := make([]byte, 4096)
					n, _ := resp.Body.Read(peekBuf)
					if n == 0 {
						resp.Body.Close()
						log.Printf("Empty streaming response from upstream, retrying (attempt %d/%d)", attempt+1, maxRetries+1)
						upstreamErrors.WithLabelValues("empty_streaming", deploymentVariant).Inc()
						if attempt < maxRetries {
							retryAttempts.WithLabelValues("empty_streaming", deploymentVariant).Inc()
							continue
						}
						log.Printf("Empty streaming response, max retries exceeded - returning 502")
						requestsTotal.WithLabelValues(r.Method, r.URL.Path, "502", deploymentVariant).Inc()
						requestDuration.WithLabelValues(r.Method, r.URL.Path, "502", deploymentVariant).Observe(time.Since(start).Seconds())
						http.Error(w, "Upstream returned empty streaming response after retries", http.StatusBadGateway)
						return
					}
					streamingPeek = peekBuf[:n]
				}
			}

			// Success or non-retryable error
			break
		}

		if resp == nil {
			log.Printf("All retry attempts failed: %v", lastErr)
			requestsTotal.WithLabelValues(r.Method, r.URL.Path, "502", deploymentVariant).Inc()
			requestDuration.WithLabelValues(r.Method, r.URL.Path, "502", deploymentVariant).Observe(time.Since(start).Seconds())
			http.Error(w, "Upstream error after retries", http.StatusBadGateway)
			return
		}

		statusCode := strconv.Itoa(resp.StatusCode)

		// Record success for rate limiter adaptation
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			rateLimiter.RecordSuccess()
		}

		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		// Remove Content-Length: proxy may modify body (usage injection), causing overrun
		w.Header().Del("Content-Length")

		// Declare trailer headers for token usage (will be sent after response body)
		if tokenCounter != nil && inputTokens > 0 {
			w.Header().Add("Trailer", "X-Token-Output")
			w.Header().Add("Trailer", "X-Token-Total")
			// Set input token count in initial headers (we know this upfront)
			w.Header().Set("X-Token-Input", strconv.Itoa(inputTokens))
		}

		w.WriteHeader(resp.StatusCode)

		var bytesWritten int64

		// Use token counting and injection if enabled, otherwise direct copy
		if tokenCounter != nil {
			// For streaming responses, we need to inject usage into the message_delta event
			// Check if this is a streaming request by checking the request body
			isStreaming := false
			if len(requestBody) > 0 {
				var req RequestBody
				if err := json.Unmarshal(requestBody, &req); err == nil {
					isStreaming = req.Stream
				}
			}

			if isStreaming {
				// Streaming response: capture, count, and inject usage into message_delta
				// If we peeked at the first chunk during validation, prepend it
				var bodyReader io.Reader = resp.Body
				if len(streamingPeek) > 0 {
					bodyReader = io.MultiReader(bytes.NewReader(streamingPeek), resp.Body)
				}
				bodyCapture := NewStreamingResponseBodyCapture(io.NopCloser(bodyReader), tokenCounter, inputTokens)
				defer bodyCapture.Close()

				buf := make([]byte, 1024)
				flusher, canFlush := w.(http.Flusher)

				for {
					n, readErr := bodyCapture.Read(buf)
					if n > 0 {
						written, writeErr := w.Write(buf[:n])
						bytesWritten += int64(written)
						if writeErr != nil {
							log.Printf("Error writing response: %v", writeErr)
							upstreamErrors.WithLabelValues("write_error", deploymentVariant).Inc()
							return
						}
						if canFlush {
							flusher.Flush()
						}
					}
					if readErr == io.EOF {
						break
					}
					if readErr != nil {
						log.Printf("Error reading response: %v", readErr)
						upstreamErrors.WithLabelValues("read_error", deploymentVariant).Inc()
						return
					}
				}

				// Record token counts from API response (or tiktoken fallback).
				usage := bodyCapture.GetUsage()
				RecordUsage(reqModel, deploymentVariant, usage)
				log.Printf("Token usage (stream, fromAPI=%v): input=%d output=%d cache_read=%d cache_write=%d",
					usage.FromAPI, usage.InputTokens, usage.OutputTokens, usage.CacheReadTokens, usage.CacheWriteTokens)
			} else {
				// Non-streaming: capture body, count tokens, wrap with usage, then send.
				// If body was pre-read during truncation validation, reuse it.
				var bodySource io.ReadCloser
				if len(validatedBody) > 0 {
					bodySource = io.NopCloser(bytes.NewReader(validatedBody))
				} else {
					bodySource = resp.Body
				}

				bodyCapture := NewResponseBodyCapture(bodySource, tokenCounter)
				defer bodyCapture.Close()

				bodyBytes, readErr := io.ReadAll(bodyCapture)
				if readErr != nil {
					log.Printf("Error reading response: %v", readErr)
					upstreamErrors.WithLabelValues("read_error", deploymentVariant).Inc()
					return
				}

				// Prefer API-reported usage from the response body.
				if usage, ok := ExtractUsageFromJSON(bodyBytes); ok {
					RecordUsage(reqModel, deploymentVariant, usage)
					log.Printf("Token usage (API): input=%d output=%d cache_read=%d cache_write=%d",
						usage.InputTokens, usage.OutputTokens, usage.CacheReadTokens, usage.CacheWriteTokens)
					written, writeErr := w.Write(bodyBytes)
					bytesWritten += int64(written)
					if writeErr != nil {
						log.Printf("Error writing response: %v", writeErr)
						upstreamErrors.WithLabelValues("write_error", deploymentVariant).Inc()
						return
					}
					if flusher, canFlush := w.(http.Flusher); canFlush {
						flusher.Flush()
					}
				} else {
					// Tiktoken fallback: estimate and wrap.
					countStart := time.Now()
					outputTokens, err := bodyCapture.CountOutputTokens()
					countDuration := time.Since(countStart)
					tokenCountDuration.WithLabelValues(deploymentVariant).Observe(countDuration.Seconds())
					if err != nil {
						log.Printf("Warning: failed to count output tokens: %v", err)
					}
					RecordUsage(reqModel, deploymentVariant, UsageData{InputTokens: inputTokens, OutputTokens: outputTokens})
					RecordOutputTokenRate(tokenizerModel, deploymentVariant, countDuration, outputTokens)
					log.Printf("Token usage (estimated): input=%d output=%d", inputTokens, outputTokens)

					wrappedResp, wrapErr := WrapResponseWithUsage(bodyBytes, inputTokens, outputTokens)
					if wrapErr != nil {
						log.Printf("Warning: failed to wrap response with usage, sending original: %v", wrapErr)
						wrappedResp = bodyBytes
					}
					written, writeErr := w.Write(wrappedResp)
					bytesWritten += int64(written)
					if writeErr != nil {
						log.Printf("Error writing response: %v", writeErr)
						upstreamErrors.WithLabelValues("write_error", deploymentVariant).Inc()
						return
					}
					if flusher, canFlush := w.(http.Flusher); canFlush {
						flusher.Flush()
					}
				}
			}
		} else {
			// Token counting disabled, direct streaming
			defer resp.Body.Close()

			buf := make([]byte, 1024)
			flusher, canFlush := w.(http.Flusher)

			for {
				n, err := resp.Body.Read(buf)
				if n > 0 {
					written, writeErr := w.Write(buf[:n])
					bytesWritten += int64(written)
					if writeErr != nil {
						log.Printf("Error writing response: %v", writeErr)
						upstreamErrors.WithLabelValues("write_error", deploymentVariant).Inc()
						return
					}
					if canFlush {
						flusher.Flush()
					}
				}
				if err == io.EOF {
					break
				}
				if err != nil {
					log.Printf("Error reading response: %v", err)
					upstreamErrors.WithLabelValues("read_error", deploymentVariant).Inc()
					return
				}
			}
		}

		// Record metrics
		duration := time.Since(start).Seconds()
		requestsTotal.WithLabelValues(r.Method, r.URL.Path, statusCode, deploymentVariant).Inc()
		requestDuration.WithLabelValues(r.Method, r.URL.Path, statusCode, deploymentVariant).Observe(duration)
		responseSize.WithLabelValues(r.Method, r.URL.Path, statusCode, deploymentVariant).Observe(float64(bytesWritten))
	})

	log.Println("Z.AI proxy listening on :8080")
	log.Println("Metrics available at :8080/metrics")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
