package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type failingReadCloser struct{}

func (failingReadCloser) Read([]byte) (int, error) {
	return 0, errors.New("forced upstream read failure")
}

func (failingReadCloser) Close() error {
	return nil
}

type failingResponseWriter struct {
	header     http.Header
	statusCode int
}

func (w *failingResponseWriter) Header() http.Header {
	return w.header
}

func (w *failingResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

func (w *failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("forced client write failure")
}

// TestPassThroughErrors verifies the non-retryable branches of the error
// classification table in docs/plan/plan.md. Each response is forwarded as-is
// and records its upstream status as the error_type metric label.
func TestPassThroughErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{name: "unprocessable entity", statusCode: http.StatusUnprocessableEntity, body: `{"error":"unprocessable entity"}`},
		{name: "bad request", statusCode: http.StatusBadRequest, body: `{"error":"invalid request"}`},
		{name: "internal server error", statusCode: http.StatusInternalServerError, body: `{"error":"upstream failure"}`},
		{name: "service unavailable", statusCode: http.StatusServiceUnavailable, body: `{"error":"temporarily unavailable"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var upstreamCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamCalls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Upstream-Error", "preserved")
				w.WriteHeader(tt.statusCode)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer upstream.Close()

			handler := CreateTestProxyHandler(t, upstream.URL, 5)
			errorMetric := upstreamErrors.WithLabelValues(strconv.Itoa(tt.statusCode), "test")
			errorBefore := testutil.ToFloat64(errorMetric)
			retryBefore := testutil.ToFloat64(retryAttempts.WithLabelValues("retry", "test"))

			response := ExecuteMessagesRequest(t, handler, createNonStreamingRequestBody())
			defer response.Body.Close()

			if response.StatusCode != tt.statusCode {
				t.Errorf("status = %d, want %d", response.StatusCode, tt.statusCode)
			}
			if calls := upstreamCalls.Load(); calls != 1 {
				t.Errorf("upstream calls = %d, want exactly 1", calls)
			}
			if got := response.Header.Get("X-Upstream-Error"); got != "preserved" {
				t.Errorf("X-Upstream-Error = %q, want preserved", got)
			}
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatalf("read response body: %v", err)
			}
			if got := string(body); got != tt.body {
				t.Errorf("response body = %q, want %q", got, tt.body)
			}
			if got := testutil.ToFloat64(errorMetric); got != errorBefore+1 {
				t.Errorf("upstream error metric = %v, want %v", got, errorBefore+1)
			}
			if got := testutil.ToFloat64(retryAttempts.WithLabelValues("retry", "test")); got != retryBefore {
				t.Errorf("retry metric = %v, want %v", got, retryBefore)
			}
		})
	}
}

// TestErrorClassificationTableCoverage exercises every row in the documented
// error-classification table and verifies the corresponding upstream metric.
func TestErrorClassificationTableCoverage(t *testing.T) {
	tests := []struct {
		name             string
		maxRetries       int
		requestBody      string
		expectedStatus   int
		expectedCalls    int32
		errorType        string
		expectedErrors   float64
		useFailingClient bool
		respond          func(http.ResponseWriter, int32)
	}{
		{
			name:           "429 with Retry-After retries then succeeds",
			maxRetries:     1,
			requestBody:    createNonStreamingRequestBody(),
			expectedStatus: http.StatusOK,
			expectedCalls:  2,
			errorType:      "429",
			expectedErrors: 1,
			respond: func(w http.ResponseWriter, call int32) {
				if call == 1 {
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{}`)
			},
		},
		{
			name:           "429 without Retry-After retries then returns 429",
			maxRetries:     1,
			requestBody:    createNonStreamingRequestBody(),
			expectedStatus: http.StatusTooManyRequests,
			expectedCalls:  2,
			errorType:      "429",
			expectedErrors: 2,
			respond: func(w http.ResponseWriter, _ int32) {
				w.WriteHeader(http.StatusTooManyRequests)
			},
		},
		{
			name:           "422 passes through without retry",
			maxRetries:     5,
			requestBody:    createNonStreamingRequestBody(),
			expectedStatus: http.StatusUnprocessableEntity,
			expectedCalls:  1,
			errorType:      "422",
			expectedErrors: 1,
			respond: func(w http.ResponseWriter, _ int32) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = io.WriteString(w, `{"error":"unprocessable"}`)
			},
		},
		{
			name:           "empty JSON retries then returns 502",
			maxRetries:     1,
			requestBody:    createNonStreamingRequestBody(),
			expectedStatus: http.StatusBadGateway,
			expectedCalls:  2,
			errorType:      "truncated_response",
			expectedErrors: 2,
			respond: func(w http.ResponseWriter, _ int32) {
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name:           "invalid JSON retries then returns 502",
			maxRetries:     1,
			requestBody:    createNonStreamingRequestBody(),
			expectedStatus: http.StatusBadGateway,
			expectedCalls:  2,
			errorType:      "truncated_response",
			expectedErrors: 2,
			respond: func(w http.ResponseWriter, _ int32) {
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, `{invalid json`)
			},
		},
		{
			name:           "empty streaming response retries then returns 502",
			maxRetries:     1,
			requestBody:    createStreamingRequestBody(),
			expectedStatus: http.StatusBadGateway,
			expectedCalls:  2,
			errorType:      "empty_streaming",
			expectedErrors: 2,
			respond: func(w http.ResponseWriter, _ int32) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name:             "network error retries then returns 502",
			maxRetries:       1,
			requestBody:      createNonStreamingRequestBody(),
			expectedStatus:   http.StatusBadGateway,
			expectedCalls:    2,
			errorType:        "upstream_connection",
			expectedErrors:   2,
			useFailingClient: true,
		},
		{
			name:           "other upstream 4xx passes through",
			maxRetries:     5,
			requestBody:    createNonStreamingRequestBody(),
			expectedStatus: http.StatusBadRequest,
			expectedCalls:  1,
			errorType:      "400",
			expectedErrors: 1,
			respond: func(w http.ResponseWriter, _ int32) {
				w.WriteHeader(http.StatusBadRequest)
			},
		},
		{
			name:           "other upstream 5xx passes through",
			maxRetries:     5,
			requestBody:    createNonStreamingRequestBody(),
			expectedStatus: http.StatusServiceUnavailable,
			expectedCalls:  1,
			errorType:      "503",
			expectedErrors: 1,
			respond: func(w http.ResponseWriter, _ int32) {
				w.WriteHeader(http.StatusServiceUnavailable)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var upstreamCalls atomic.Int32
			var upstream *httptest.Server
			targetURL := "http://upstream.invalid"
			if !tt.useFailingClient {
				upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					tt.respond(w, upstreamCalls.Add(1))
				}))
				defer upstream.Close()
				targetURL = upstream.URL
			}

			handler := CreateTestProxyHandler(t, targetURL, tt.maxRetries)
			if tt.useFailingClient {
				handler.client.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
					upstreamCalls.Add(1)
					return nil, errors.New("upstream unavailable")
				})
			}

			errorMetric := upstreamErrors.WithLabelValues(tt.errorType, "test")
			errorBefore := testutil.ToFloat64(errorMetric)
			response := ExecuteMessagesRequest(t, handler, tt.requestBody)
			defer response.Body.Close()

			if response.StatusCode != tt.expectedStatus {
				t.Errorf("status = %d, want %d", response.StatusCode, tt.expectedStatus)
			}
			if calls := upstreamCalls.Load(); calls != tt.expectedCalls {
				t.Errorf("upstream calls = %d, want %d", calls, tt.expectedCalls)
			}
			if got := testutil.ToFloat64(errorMetric); got != errorBefore+tt.expectedErrors {
				t.Errorf("upstream error metric = %v, want %v", got, errorBefore+tt.expectedErrors)
			}
		})
	}
}

// TestUpstreamErrorMetricsForNonHTTPFailures covers the remaining error types
// in zai_proxy_upstream_errors_total that are outside the HTTP response
// classification table.
func TestUpstreamErrorMetricsForNonHTTPFailures(t *testing.T) {
	t.Run("request creation", func(t *testing.T) {
		handler := CreateTestProxyHandler(t, "://invalid-target", 0)
		errorMetric := upstreamErrors.WithLabelValues("request_creation", "test")
		errorBefore := testutil.ToFloat64(errorMetric)

		response := ExecuteMessagesRequest(t, handler, createNonStreamingRequestBody())
		defer response.Body.Close()

		if response.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", response.StatusCode, http.StatusBadRequest)
		}
		if got := testutil.ToFloat64(errorMetric); got != errorBefore+1 {
			t.Errorf("upstream error metric = %v, want %v", got, errorBefore+1)
		}
	})

	t.Run("upstream read", func(t *testing.T) {
		handler := CreateTestProxyHandler(t, "http://upstream.invalid", 0)
		handler.client.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Header:     make(http.Header),
				Body:       failingReadCloser{},
			}, nil
		})
		errorMetric := upstreamErrors.WithLabelValues("read_error", "test")
		errorBefore := testutil.ToFloat64(errorMetric)

		response := ExecuteMessagesRequest(t, handler, createNonStreamingRequestBody())
		defer response.Body.Close()

		if response.StatusCode != http.StatusInternalServerError {
			t.Errorf("status = %d, want %d", response.StatusCode, http.StatusInternalServerError)
		}
		if got := testutil.ToFloat64(errorMetric); got != errorBefore+1 {
			t.Errorf("upstream error metric = %v, want %v", got, errorBefore+1)
		}
	})

	t.Run("client write", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":"upstream failure"}`)
		}))
		defer upstream.Close()

		handler := CreateTestProxyHandler(t, upstream.URL, 0)
		errorMetric := upstreamErrors.WithLabelValues("write_error", "test")
		errorBefore := testutil.ToFloat64(errorMetric)
		writer := &failingResponseWriter{header: make(http.Header)}

		handler.ServeHTTP(writer, CreateProxyRequest("POST", "/v1/messages", createNonStreamingRequestBody()))

		if writer.statusCode != http.StatusInternalServerError {
			t.Errorf("status = %d, want %d", writer.statusCode, http.StatusInternalServerError)
		}
		if got := testutil.ToFloat64(errorMetric); got != errorBefore+1 {
			t.Errorf("upstream error metric = %v, want %v", got, errorBefore+1)
		}
	})
}
