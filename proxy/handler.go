package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// ProxyHandler handles HTTP requests to the Z.AI upstream with retry logic and rate limiting.
type ProxyHandler struct {
	apiKey            string
	targetURL         string
	maxRetries        int
	maxWorkers        int64
	deploymentVariant string
	tokenCounter      TokenCounter
	tokenizerModel    string
	rateLimiter       *AdaptiveRateLimiter
	client            *http.Client
	retrySleep        func(time.Duration)
	retryJitter       func() float64
	currentRequests   atomic.Int64
	mu                sync.RWMutex
}

// NewProxyHandler creates a new proxy handler with the given configuration.
func NewProxyHandler(
	apiKey string,
	targetURL string,
	maxRetries int,
	maxWorkers int64,
	deploymentVariant string,
	tokenCounter TokenCounter,
	tokenizerModel string,
	initialRate float64,
	minRate float64,
	maxRate float64,
) *ProxyHandler {
	return &ProxyHandler{
		apiKey:            apiKey,
		targetURL:         targetURL,
		maxRetries:        maxRetries,
		maxWorkers:        maxWorkers,
		deploymentVariant: deploymentVariant,
		tokenCounter:      tokenCounter,
		tokenizerModel:    tokenizerModel,
		rateLimiter:       NewAdaptiveRateLimiter(initialRate, minRate, maxRate),
		client: &http.Client{
			Timeout: 5 * time.Minute,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		// retrySleep stays nil in production so retry waits are cancellable;
		// see waitForRetry. Tests set it to assert the chosen delay.
	}
}

// waitForRetry performs one pre-retry wait, abandoning the request when the
// caller has gone away: waiting out a retry for a closed connection spends
// admission and upstream quota on nobody. retrySleep, when set, replaces the
// wait entirely so tests can assert the chosen delay without waiting it out;
// production leaves it nil and takes the cancellable path below.
func (h *ProxyHandler) waitForRetry(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return ctx.Err() == nil
	}
	if h.retrySleep != nil {
		h.retrySleep(delay)
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// updateUtilization updates the worker utilization metric.
func (h *ProxyHandler) updateUtilization() {
	current := h.currentRequests.Load()
	max := atomic.LoadInt64(&h.maxWorkers)
	if max > 0 {
		utilization := float64(current) / float64(max)
		workerUtilization.WithLabelValues(h.deploymentVariant).Set(utilization)
	}
}

// ServeHTTP handles an incoming HTTP request.
func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	clientBucket := rateLimitClientBucket(r.RemoteAddr)

	// Increment concurrent requests
	current := h.currentRequests.Add(1)
	concurrentRequests.WithLabelValues(h.deploymentVariant).Set(float64(current))
	h.updateUtilization()

	defer func() {
		current := h.currentRequests.Add(-1)
		concurrentRequests.WithLabelValues(h.deploymentVariant).Set(float64(current))
		h.updateUtilization()
	}()

	// Check if we're at max capacity
	max := atomic.LoadInt64(&h.maxWorkers)
	if max > 0 && current > max {
		log.Printf("Max workers exceeded: %d/%d", current, max)
		rateLimitRejections.WithLabelValues(h.deploymentVariant, clientBucket).Inc()
		requestsTotal.WithLabelValues(r.Method, r.URL.Path, "503", h.deploymentVariant).Inc()
		http.Error(w, "Service at capacity", http.StatusServiceUnavailable)
		return
	}

	// Apply rate limiting
	h.rateLimiter.waitForClient(h.deploymentVariant, clientBucket)

	// Track request size
	if r.ContentLength > 0 {
		requestSize.WithLabelValues(r.Method, r.URL.Path, h.deploymentVariant).Observe(float64(r.ContentLength))
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
	reqModel := h.tokenizerModel
	if len(requestBody) > 0 {
		var rb RequestBody
		if err := json.Unmarshal(requestBody, &rb); err == nil && rb.Model != "" {
			reqModel = rb.Model
		}
	}

	// Count input tokens if enabled.
	var inputTokens int
	if h.tokenCounter != nil && len(requestBody) > 0 {
		countStart := time.Now()
		inputTokens, _ = CountRequestTokens(requestBody, h.tokenCounter)
		countDuration := time.Since(countStart)
		tokenCountDuration.WithLabelValues(h.deploymentVariant).Observe(countDuration.Seconds())
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
	var validatedBody []byte
	var streamingPeek []byte

	// retryDelay is the wait to perform before the next upstream attempt. The
	// path that decides to retry sets it; the top of the loop performs the
	// wait so every wait is cancellation-safe and every retry is preceded by
	// exactly one admission.
	var retryDelay time.Duration

	for attempt := 0; attempt <= h.maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("Retry attempt %d/%d after %v", attempt, h.maxRetries+1, retryDelay)
			if !h.waitForRetry(r.Context(), retryDelay) {
				log.Printf("Client abandoned request during retry wait")
				return
			}
			// A retry is an upstream attempt like any other, so it reacquires
			// admission: a retry must never bypass the learned ceiling or the
			// pacing the first attempt was admitted under.
			h.rateLimiter.waitForClient(h.deploymentVariant, clientBucket)
			retryAttempts.WithLabelValues("retry", h.deploymentVariant).Inc()
			if r.Context().Err() != nil {
				return
			}
		}

		upstreamURL := h.targetURL + r.URL.Path
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
			upstreamErrors.WithLabelValues("request_creation", h.deploymentVariant).Inc()
			requestsTotal.WithLabelValues(r.Method, r.URL.Path, "400", h.deploymentVariant).Inc()
			requestDuration.WithLabelValues(r.Method, r.URL.Path, "400", h.deploymentVariant).Observe(time.Since(start).Seconds())
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		for key, values := range r.Header {
			for _, value := range values {
				upstreamReq.Header.Add(key, value)
			}
		}

		upstreamReq.Header.Set("Host", "api.z.ai")
		// The caller's own Authorization/x-api-key is meaningless here -- this
		// proxy's whole purpose is to hold the real credential so callers don't
		// need it (NEEDLE's adapters literally send ANTHROPIC_AUTH_TOKEN=
		// 'proxy-handles-auth'). Z.AI's Anthropic-compatible endpoint
		// authenticates via x-api-key, not Authorization: Bearer -- setting
		// Bearer here left the real key in a header upstream ignores while the
		// caller's placeholder sat untouched in the header it actually reads,
		// which its WAF/CDN front silently rejected with a bare nginx 404
		// instead of Z.AI's own structured 401 (verified 2026-09-04: a direct
		// x-api-key request to https://api.z.ai/api/anthropic/v1/messages gets
		// Z.AI's real JSON error; the old Authorization: Bearer path did not).
		upstreamReq.Header.Del("Authorization")
		upstreamReq.Header.Set("x-api-key", h.apiKey)
		// Disable gzip so the proxy can parse/modify the response body
		upstreamReq.Header.Set("Accept-Encoding", "identity")

		resp, err = h.client.Do(upstreamReq)
		if err != nil {
			lastErr = err
			log.Printf("Error forwarding request (attempt %d/%d): %v", attempt+1, h.maxRetries+1, err)
			upstreamErrors.WithLabelValues("upstream_connection", h.deploymentVariant).Inc()

			// Retry on network errors
			if attempt < h.maxRetries {
				retryDelay = zaiBackoffDelay(attempt + 1)
				retryAttempts.WithLabelValues("network_error", h.deploymentVariant).Inc()
				continue
			}

			requestsTotal.WithLabelValues(r.Method, r.URL.Path, "502", h.deploymentVariant).Inc()
			requestDuration.WithLabelValues(r.Method, r.URL.Path, "502", h.deploymentVariant).Observe(time.Since(start).Seconds())
			http.Error(w, "Upstream error", http.StatusBadGateway)
			return
		}

		// Handle 429 Rate Limit. The status alone does not say what Z.AI ran
		// out of, so the bounded error body is classified before anything
		// adapts: the class decides which controller hears about the failure,
		// whether another attempt is worth making, and how long to wait.
		if resp.StatusCode == 429 {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, DefaultMaxZaiErrorBodyBytes+1))
			resp.Body.Close()
			// The extra byte above the budget only tells ParseZaiError the body
			// was oversize; it was never classified. Trim it so what the caller
			// receives is exactly what was classified.
			if len(errBody) > DefaultMaxZaiErrorBodyBytes {
				errBody = errBody[:DefaultMaxZaiErrorBodyBytes]
			}
			parsed := ParseZaiError(errBody, DefaultMaxZaiErrorBodyBytes)
			upstreamErrors.WithLabelValues("429", h.deploymentVariant).Inc()

			// Retry-After is the provider's own instruction and is honored for
			// every class that retries.
			retryAfter := zaiRetryAfterHeader(resp.Header.Get("Retry-After"))

			switch parsed.Class {
			case ZaiErrorClassQuota:
				// 1308/1310: the plan window is exhausted. No wait inside the
				// retry budget outlives a five-hour or weekly reset, so retry
				// only burns quota; hand the reset to the caller instead. The
				// requests-per-second ceiling is not walked down for it.
				log.Printf("429 quota exhausted (code %s): not retrying, returning reset metadata", parsed.Code)
				h.writeZaiRateLimitedResponse(w, r, resp, errBody, parsed, start)
				return

			case ZaiErrorClassFrequency:
				// 1303/1305: short-horizon rate pressure is exactly what the
				// adaptive requests-per-second controller learns from.
				h.rateLimiter.Record429()

			case ZaiErrorClassConcurrency:
				// 1302: the account's concurrent slots are full. Holding
				// concurrency is the fix, and counting this as a 429 would
				// walk the learned requests-per-second ceiling down for a
				// problem that limiter can neither see nor fix.

			case ZaiErrorClassModelCongestion:
				// 1312: the model is busy, not this account's quota. A
				// jittered retry spreads callers out without ever waiting
				// longer than the plain curve.

			default:
				// Nothing in the body was recognized: a 429 can only be
				// assumed to be rate pressure, so feed the controller and keep
				// the pre-classification retry shape, which waits out both the
				// provider's hint and the exponential curve.
				h.rateLimiter.Record429()
			}

			if attempt >= h.maxRetries {
				// Max retries exceeded: pass the upstream 429 through with its
				// classification and reset metadata attached.
				log.Printf("429 %s (code %s), max retries exceeded", parsed.Class, parsed.Code)
				h.writeZaiRateLimitedResponse(w, r, resp, errBody, parsed, start)
				return
			}

			if parsed.Class == ZaiErrorClassUnknown {
				// Honor Retry-After now, then the exponential curve before the
				// next attempt -- the two waits the unclassified path has
				// always taken.
				if retryAfter > 0 {
					if !h.waitForRetry(r.Context(), retryAfter) {
						return
					}
				}
				retryDelay = zaiBackoffDelay(attempt + 1)
			} else {
				retryDelay = zaiClassRetryDelay(parsed.Class, attempt+1, retryAfter, parsed.RetryAfter, h.retryJitter)
			}
			log.Printf("429 %s (code %s), retrying (attempt %d/%d) after %v",
				parsed.Class, parsed.Code, attempt+1, h.maxRetries+1, retryDelay)
			retryAttempts.WithLabelValues("429", h.deploymentVariant).Inc()
			continue
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
			upstreamErrors.WithLabelValues("422", h.deploymentVariant).Inc()
			requestsTotal.WithLabelValues(r.Method, r.URL.Path, "422", h.deploymentVariant).Inc()
			requestDuration.WithLabelValues(r.Method, r.URL.Path, "422", h.deploymentVariant).Observe(time.Since(start).Seconds())
			for key, values := range resp.Header {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}
			w.Header().Del("Content-Length")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write(respBody)
			return
		}

		// Other upstream 4xx/5xx responses are passed through without retrying.
		// Keep their status code as the error type so callers can distinguish
		// upstream validation failures from server failures in Prometheus.
		if resp.StatusCode >= http.StatusBadRequest {
			upstreamErrors.WithLabelValues(strconv.Itoa(resp.StatusCode), h.deploymentVariant).Inc()
		}

		// Validate response body before committing to the client.
		// Z.AI occasionally returns HTTP 200 with empty or truncated JSON.
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if !IsStreamingRequest(requestBody) {
				// Non-streaming: validate entire body
				bodyBytes, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if len(bodyBytes) == 0 || !json.Valid(bodyBytes) {
					log.Printf("Malformed response from upstream (empty=%v, size=%d), retrying (attempt %d/%d)", len(bodyBytes) == 0, len(bodyBytes), attempt+1, h.maxRetries+1)
					upstreamErrors.WithLabelValues("truncated_response", h.deploymentVariant).Inc()
					if attempt < h.maxRetries {
						retryDelay = zaiBackoffDelay(attempt + 1)
						retryAttempts.WithLabelValues("truncated_response", h.deploymentVariant).Inc()
						continue
					}
					log.Printf("Malformed response from upstream, max retries exceeded - returning 502")
					requestsTotal.WithLabelValues(r.Method, r.URL.Path, "502", h.deploymentVariant).Inc()
					requestDuration.WithLabelValues(r.Method, r.URL.Path, "502", h.deploymentVariant).Observe(time.Since(start).Seconds())
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
					log.Printf("Empty streaming response from upstream, retrying (attempt %d/%d)", attempt+1, h.maxRetries+1)
					upstreamErrors.WithLabelValues("empty_streaming", h.deploymentVariant).Inc()
					if attempt < h.maxRetries {
						retryDelay = zaiBackoffDelay(attempt + 1)
						retryAttempts.WithLabelValues("empty_streaming", h.deploymentVariant).Inc()
						continue
					}
					log.Printf("Empty streaming response, max retries exceeded - returning 502")
					requestsTotal.WithLabelValues(r.Method, r.URL.Path, "502", h.deploymentVariant).Inc()
					requestDuration.WithLabelValues(r.Method, r.URL.Path, "502", h.deploymentVariant).Observe(time.Since(start).Seconds())
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
		requestsTotal.WithLabelValues(r.Method, r.URL.Path, "502", h.deploymentVariant).Inc()
		requestDuration.WithLabelValues(r.Method, r.URL.Path, "502", h.deploymentVariant).Observe(time.Since(start).Seconds())
		http.Error(w, "Upstream error after retries", http.StatusBadGateway)
		return
	}

	statusCode := strconv.Itoa(resp.StatusCode)

	// Record success for rate limiter adaptation
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		h.rateLimiter.RecordSuccess()
	}

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	// Remove Content-Length: proxy may modify body (usage injection), causing overrun
	w.Header().Del("Content-Length")

	// Declare trailer headers for token usage (will be sent after response body)
	if h.tokenCounter != nil && inputTokens > 0 {
		w.Header().Add("Trailer", "X-Token-Output")
		w.Header().Add("Trailer", "X-Token-Total")
		// Set input token count in initial headers (we know this upfront)
		w.Header().Set("X-Token-Input", strconv.Itoa(inputTokens))
	}

	w.WriteHeader(resp.StatusCode)

	var bytesWritten int64

	// Use token counting and injection if enabled, otherwise direct copy
	if h.tokenCounter != nil {
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
			bodyCapture := NewStreamingResponseBodyCapture(io.NopCloser(bodyReader), h.tokenCounter, inputTokens)
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
						upstreamErrors.WithLabelValues("write_error", h.deploymentVariant).Inc()
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
					upstreamErrors.WithLabelValues("read_error", h.deploymentVariant).Inc()
					return
				}
			}

			// Record token counts from API response (or tiktoken fallback).
			usage := bodyCapture.GetUsage()
			RecordUsage(reqModel, h.deploymentVariant, usage)
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

			bodyCapture := NewResponseBodyCapture(bodySource, h.tokenCounter)
			defer bodyCapture.Close()

			bodyBytes, readErr := io.ReadAll(bodyCapture)
			if readErr != nil {
				log.Printf("Error reading response: %v", readErr)
				upstreamErrors.WithLabelValues("read_error", h.deploymentVariant).Inc()
				return
			}

			// Prefer API-reported usage from the response body.
			if usage, ok := ExtractUsageFromJSON(bodyBytes); ok {
				RecordUsage(reqModel, h.deploymentVariant, usage)
				log.Printf("Token usage (API): input=%d output=%d cache_read=%d cache_write=%d",
					usage.InputTokens, usage.OutputTokens, usage.CacheReadTokens, usage.CacheWriteTokens)
				written, writeErr := w.Write(bodyBytes)
				bytesWritten += int64(written)
				if writeErr != nil {
					log.Printf("Error writing response: %v", writeErr)
					upstreamErrors.WithLabelValues("write_error", h.deploymentVariant).Inc()
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
				tokenCountDuration.WithLabelValues(h.deploymentVariant).Observe(countDuration.Seconds())
				if err != nil {
					log.Printf("Warning: failed to count output tokens: %v", err)
				}
				RecordUsage(reqModel, h.deploymentVariant, UsageData{InputTokens: inputTokens, OutputTokens: outputTokens})
				RecordOutputTokenRate(h.tokenizerModel, h.deploymentVariant, countDuration, outputTokens)
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
					upstreamErrors.WithLabelValues("write_error", h.deploymentVariant).Inc()
					return
				}
				if flusher, canFlush := w.(http.Flusher); canFlush {
					flusher.Flush()
				}
			}
		}
	} else {
		// Token counting disabled, copy the response directly. Validation may
		// already have consumed a non-streaming body or read the first streaming
		// chunk, so replay those bytes before continuing with the upstream body.
		defer resp.Body.Close()

		var bodyReader io.Reader = resp.Body
		if len(validatedBody) > 0 {
			bodyReader = bytes.NewReader(validatedBody)
		} else if len(streamingPeek) > 0 {
			bodyReader = io.MultiReader(bytes.NewReader(streamingPeek), resp.Body)
		}

		buf := make([]byte, 1024)
		flusher, canFlush := w.(http.Flusher)

		for {
			n, err := bodyReader.Read(buf)
			if n > 0 {
				written, writeErr := w.Write(buf[:n])
				bytesWritten += int64(written)
				if writeErr != nil {
					log.Printf("Error writing response: %v", writeErr)
					upstreamErrors.WithLabelValues("write_error", h.deploymentVariant).Inc()
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
				upstreamErrors.WithLabelValues("read_error", h.deploymentVariant).Inc()
				return
			}
		}
	}

	// Record metrics
	duration := time.Since(start).Seconds()
	requestsTotal.WithLabelValues(r.Method, r.URL.Path, statusCode, h.deploymentVariant).Inc()
	requestDuration.WithLabelValues(r.Method, r.URL.Path, statusCode, h.deploymentVariant).Observe(duration)
	responseSize.WithLabelValues(r.Method, r.URL.Path, statusCode, h.deploymentVariant).Observe(float64(bytesWritten))
}

// zaiBackoffDelay is the plain exponential retry curve -- 1s, 2s, 4s, ... --
// used wherever a class has not asked for a shaped wait. retry counts the
// upcoming retry, so the first retry waits one second.
func zaiBackoffDelay(retry int) time.Duration {
	const maxShift = 32 // far beyond any configured retry budget
	if retry <= 0 {
		return 0
	}
	if retry > maxShift {
		retry = maxShift
	}
	return time.Duration(1<<uint(retry-1)) * time.Second
}

// zaiJitteredBackoff halves-and-jitters a wait: the result lands in
// [base/2, base), so a population of callers that all hit the same 429 at the
// same moment spreads out without any of them waiting longer than the plain
// curve. jitter supplies a uniform sample in [0,1); production uses the
// package default and tests inject a deterministic source.
func zaiJitteredBackoff(base time.Duration, jitter func() float64) time.Duration {
	if base <= 0 {
		return 0
	}
	if jitter == nil {
		jitter = rand.Float64
	}
	u := jitter()
	if !(u >= 0) || u >= 1 { // out of range or NaN: fall back to no jitter
		u = 0
	}
	return base/2 + time.Duration(float64(base/2)*u)
}

// zaiClassRetryDelay resolves the wait before one classified 429 retry: the
// largest of the class's own backoff, the provider's Retry-After header, and
// a retry_after the body advertised. Frequency and model-congestion retries
// are jittered; a quota window is never retried at all.
func zaiClassRetryDelay(class ZaiErrorClass, retry int, retryAfterHeader, retryAfterBody time.Duration, jitter func() float64) time.Duration {
	base := zaiBackoffDelay(retry)
	switch class {
	case ZaiErrorClassFrequency, ZaiErrorClassModelCongestion:
		base = zaiJitteredBackoff(base, jitter)
	}
	return max(base, retryAfterHeader, retryAfterBody)
}

// zaiRetryAfterHeader parses a Retry-After header expressed in seconds. The
// HTTP-date form is left unparsed, as the unclassified path always did.
func zaiRetryAfterHeader(header string) time.Duration {
	if header == "" {
		return 0
	}
	seconds, err := strconv.Atoi(header)
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

// zaiAdvertisedRetryAfter returns the retry hint a classified 429 carries for
// the caller: an explicit retry_after when the body advertised one, otherwise
// how long until the advertised reset. Nothing advertised means zero and no
// header is invented.
func zaiAdvertisedRetryAfter(parsed ZaiBusinessError) time.Duration {
	if parsed.RetryAfter > 0 {
		return parsed.RetryAfter
	}
	if parsed.ResetAt.IsZero() {
		return 0
	}
	if until := time.Until(parsed.ResetAt); until > 0 {
		return until
	}
	return 0
}

// writeZaiRateLimitedResponse hands a final Z.AI 429 to the caller. The
// upstream status, headers, and error body are preserved: the provider's
// error envelope is the caller's best diagnosis of its own request, and no
// part of it is logged, because provider message text is not guaranteed to be
// free of account-identifying material. The bounded classification and reset
// metadata are added as headers so a client can react without parsing the
// provider envelope.
func (h *ProxyHandler) writeZaiRateLimitedResponse(w http.ResponseWriter, r *http.Request, resp *http.Response, body []byte, parsed ZaiBusinessError, start time.Time) {
	headers := w.Header()
	for key, values := range resp.Header {
		for _, value := range values {
			headers.Add(key, value)
		}
	}
	// The body is replayed verbatim but no longer through the upstream
	// connection, so the claimed length may not hold.
	headers.Del("Content-Length")

	headers.Set("X-Zai-Error-Class", string(parsed.Class))
	if parsed.Code != "" {
		headers.Set("X-Zai-Error-Code", parsed.Code)
	}
	if !parsed.ResetAt.IsZero() {
		headers.Set("X-Zai-Rate-Limit-Reset", parsed.ResetAt.UTC().Format(time.RFC3339))
	}
	if headers.Get("Retry-After") == "" {
		if advertised := zaiAdvertisedRetryAfter(parsed); advertised > 0 {
			headers.Set("Retry-After", strconv.Itoa(int(math.Ceil(advertised.Seconds()))))
		}
	}

	w.WriteHeader(resp.StatusCode)
	if len(body) > 0 {
		if _, err := w.Write(body); err != nil {
			log.Printf("Error writing upstream 429 response: %v", err)
		}
	}

	requestsTotal.WithLabelValues(r.Method, r.URL.Path, strconv.Itoa(resp.StatusCode), h.deploymentVariant).Inc()
	requestDuration.WithLabelValues(r.Method, r.URL.Path, strconv.Itoa(resp.StatusCode), h.deploymentVariant).Observe(time.Since(start).Seconds())
}

// GetCurrentRate returns the current rate limit for the handler.
func (h *ProxyHandler) GetCurrentRate() float64 {
	return h.rateLimiter.GetCurrentRate()
}

// ResetRateLimit resets the rate limiter to the initial rate.
func (h *ProxyHandler) ResetRateLimit(initialRate float64) {
	h.rateLimiter.Reset(initialRate)
}

// SetMaxWorkers sets the maximum number of concurrent workers.
func (h *ProxyHandler) SetMaxWorkers(max int64) {
	atomic.StoreInt64(&h.maxWorkers, max)
	maxWorkers.WithLabelValues(h.deploymentVariant).Set(float64(max))
}
