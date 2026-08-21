package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
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
		retrySleep: time.Sleep,
	}
}

// sleepForRetry waits before a retry. retrySleep is injectable so tests can
// assert the chosen delay without waiting for production-sized backoff.
func (h *ProxyHandler) sleepForRetry(delay time.Duration) {
	if h.retrySleep != nil {
		h.retrySleep(delay)
		return
	}
	time.Sleep(delay)
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
		requestsTotal.WithLabelValues(r.Method, r.URL.Path, "503", h.deploymentVariant).Inc()
		http.Error(w, "Service at capacity", http.StatusServiceUnavailable)
		return
	}

	// Apply rate limiting
	h.rateLimiter.Wait(h.deploymentVariant)

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

	for attempt := 0; attempt <= h.maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s, etc.
			backoffDuration := time.Duration(1<<uint(attempt-1)) * time.Second
			log.Printf("Retry attempt %d/%d after %v", attempt, h.maxRetries, backoffDuration)
			h.sleepForRetry(backoffDuration)
			retryAttempts.WithLabelValues("retry", h.deploymentVariant).Inc()
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
		upstreamReq.Header.Set("Authorization", "Bearer "+h.apiKey)
		// Disable gzip so the proxy can parse/modify the response body
		upstreamReq.Header.Set("Accept-Encoding", "identity")

		resp, err = h.client.Do(upstreamReq)
		if err != nil {
			lastErr = err
			log.Printf("Error forwarding request (attempt %d/%d): %v", attempt+1, h.maxRetries+1, err)
			upstreamErrors.WithLabelValues("upstream_connection", h.deploymentVariant).Inc()

			// Retry on network errors
			if attempt < h.maxRetries {
				retryAttempts.WithLabelValues("network_error", h.deploymentVariant).Inc()
				continue
			}

			requestsTotal.WithLabelValues(r.Method, r.URL.Path, "502", h.deploymentVariant).Inc()
			requestDuration.WithLabelValues(r.Method, r.URL.Path, "502", h.deploymentVariant).Observe(time.Since(start).Seconds())
			http.Error(w, "Upstream error", http.StatusBadGateway)
			return
		}

		// Handle 429 Rate Limit
		if resp.StatusCode == 429 {
			resp.Body.Close()
			upstreamErrors.WithLabelValues("429", h.deploymentVariant).Inc()
			h.rateLimiter.Record429()

			// Check Retry-After header
			retryAfter := resp.Header.Get("Retry-After")
			if retryAfter != "" {
				if seconds, err := strconv.Atoi(retryAfter); err == nil {
					log.Printf("429 Rate Limited, retry after %d seconds", seconds)
					h.sleepForRetry(time.Duration(seconds) * time.Second)
				}
			}

			if attempt < h.maxRetries {
				log.Printf("429 Rate Limited, retrying (attempt %d/%d)", attempt+1, h.maxRetries+1)
				retryAttempts.WithLabelValues("429", h.deploymentVariant).Inc()
				continue
			}

			// Exceeded max retries, return 429 to client
			log.Printf("429 Rate Limited, max retries exceeded")
			requestsTotal.WithLabelValues(r.Method, r.URL.Path, "429", h.deploymentVariant).Inc()
			requestDuration.WithLabelValues(r.Method, r.URL.Path, "429", h.deploymentVariant).Observe(time.Since(start).Seconds())
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
