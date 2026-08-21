package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestProxyRetryResponseValidationContract(t *testing.T) {
	const (
		validResponse         = `{"id":"msg_test"}`
		unprocessableResponse = `{"error":"unprocessable"}`
		passThroughResponse   = `{"error":"upstream"}`
	)

	tests := []struct {
		name                  string
		maxRetries            int
		requestBody           string
		respond               func(t *testing.T, w http.ResponseWriter, r *http.Request, call int)
		wantStatus            int
		wantCalls             int32
		retryReason           string
		wantRetryIncrements   float64
		errorType             string
		wantErrorIncrements   float64
		wantBody              string
		wantDelays            []time.Duration
		wantCallsWhenSleeping []int32
	}{
		{
			name:        "429_retry_after_then_success",
			maxRetries:  1,
			requestBody: createNonStreamingRequestBody(),
			respond: func(_ *testing.T, w http.ResponseWriter, _ *http.Request, call int) {
				if call == 1 {
					w.Header().Set("Retry-After", "3")
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(validResponse))
			},
			wantStatus:            http.StatusOK,
			wantCalls:             2,
			retryReason:           "429",
			wantRetryIncrements:   1,
			errorType:             "429",
			wantErrorIncrements:   1,
			wantBody:              validResponse,
			wantDelays:            []time.Duration{3 * time.Second, time.Second},
			wantCallsWhenSleeping: []int32{1, 1},
		},
		{
			name:        "429_without_retry_after_uses_exponential_backoff",
			maxRetries:  2,
			requestBody: createNonStreamingRequestBody(),
			respond: func(_ *testing.T, w http.ResponseWriter, _ *http.Request, _ int) {
				w.WriteHeader(http.StatusTooManyRequests)
			},
			wantStatus:            http.StatusTooManyRequests,
			wantCalls:             3,
			retryReason:           "429",
			wantRetryIncrements:   2,
			errorType:             "429",
			wantErrorIncrements:   3,
			wantDelays:            []time.Duration{time.Second, 2 * time.Second},
			wantCallsWhenSleeping: []int32{1, 2},
		},
		{
			name:        "422_is_passed_through_without_retry",
			maxRetries:  2,
			requestBody: createNonStreamingRequestBody(),
			respond: func(_ *testing.T, w http.ResponseWriter, _ *http.Request, _ int) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(unprocessableResponse))
			},
			wantStatus:          http.StatusUnprocessableEntity,
			wantCalls:           1,
			retryReason:         "retry",
			errorType:           "422",
			wantErrorIncrements: 1,
			wantBody:            unprocessableResponse,
		},
		{
			name:        "empty_non_streaming_json_is_retried_then_502",
			maxRetries:  2,
			requestBody: createNonStreamingRequestBody(),
			respond: func(_ *testing.T, w http.ResponseWriter, _ *http.Request, _ int) {
				w.WriteHeader(http.StatusOK)
			},
			wantStatus:            http.StatusBadGateway,
			wantCalls:             3,
			retryReason:           "truncated_response",
			wantRetryIncrements:   2,
			errorType:             "truncated_response",
			wantErrorIncrements:   3,
			wantDelays:            []time.Duration{time.Second, 2 * time.Second},
			wantCallsWhenSleeping: []int32{1, 2},
		},
		{
			name:        "invalid_non_streaming_json_is_retried_then_502",
			maxRetries:  2,
			requestBody: createNonStreamingRequestBody(),
			respond: func(_ *testing.T, w http.ResponseWriter, _ *http.Request, _ int) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id":`))
			},
			wantStatus:            http.StatusBadGateway,
			wantCalls:             3,
			retryReason:           "truncated_response",
			wantRetryIncrements:   2,
			errorType:             "truncated_response",
			wantErrorIncrements:   3,
			wantDelays:            []time.Duration{time.Second, 2 * time.Second},
			wantCallsWhenSleeping: []int32{1, 2},
		},
		{
			name:        "empty_stream_is_retried_then_502",
			maxRetries:  2,
			requestBody: createStreamingRequestBody(),
			respond: func(_ *testing.T, w http.ResponseWriter, _ *http.Request, _ int) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
			},
			wantStatus:            http.StatusBadGateway,
			wantCalls:             3,
			retryReason:           "empty_streaming",
			wantRetryIncrements:   2,
			errorType:             "empty_streaming",
			wantErrorIncrements:   3,
			wantDelays:            []time.Duration{time.Second, 2 * time.Second},
			wantCallsWhenSleeping: []int32{1, 2},
		},
		{
			name:        "network_error_is_retried_then_502",
			maxRetries:  2,
			requestBody: createNonStreamingRequestBody(),
			respond: func(t *testing.T, w http.ResponseWriter, _ *http.Request, _ int) {
				conn, _, err := http.NewResponseController(w).Hijack()
				if err != nil {
					t.Errorf("Hijack upstream connection: %v", err)
					return
				}
				_ = conn.Close()
			},
			wantStatus:            http.StatusBadGateway,
			wantCalls:             3,
			retryReason:           "network_error",
			wantRetryIncrements:   2,
			errorType:             "upstream_connection",
			wantErrorIncrements:   3,
			wantDelays:            []time.Duration{time.Second, 2 * time.Second},
			wantCallsWhenSleeping: []int32{1, 2},
		},
		{
			name:        "other_4xx_is_passed_through_without_retry",
			maxRetries:  2,
			requestBody: createNonStreamingRequestBody(),
			respond: func(_ *testing.T, w http.ResponseWriter, _ *http.Request, _ int) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(passThroughResponse))
			},
			wantStatus:          http.StatusBadRequest,
			wantCalls:           1,
			retryReason:         "retry",
			errorType:           "400",
			wantErrorIncrements: 1,
			wantBody:            passThroughResponse,
		},
		{
			name:        "other_5xx_is_passed_through_without_retry",
			maxRetries:  2,
			requestBody: createNonStreamingRequestBody(),
			respond: func(_ *testing.T, w http.ResponseWriter, _ *http.Request, _ int) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(passThroughResponse))
			},
			wantStatus:          http.StatusServiceUnavailable,
			wantCalls:           1,
			retryReason:         "retry",
			errorType:           "503",
			wantErrorIncrements: 1,
			wantBody:            passThroughResponse,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var upstreamCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tc.respond(t, w, r, int(upstreamCalls.Add(1)))
			}))
			defer upstream.Close()

			variant := "retry-contract-" + tc.name
			retryBefore := testutil.ToFloat64(retryAttempts.WithLabelValues(tc.retryReason, variant))
			errorBefore := testutil.ToFloat64(upstreamErrors.WithLabelValues(tc.errorType, variant))

			var observedDelays []time.Duration
			var callsWhenSleeping []int32
			handler := NewProxyHandler(
				"test-key", upstream.URL, tc.maxRetries, 10, variant, nil, "glm-4", 1000, 1000, 1000,
			)
			handler.retrySleep = func(delay time.Duration) {
				observedDelays = append(observedDelays, delay)
				callsWhenSleeping = append(callsWhenSleeping, upstreamCalls.Load())
			}

			req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(tc.requestBody))
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)

			if got := response.Code; got != tc.wantStatus {
				t.Errorf("status = %d, want %d; body=%q", got, tc.wantStatus, response.Body.String())
			}
			if got := upstreamCalls.Load(); got != tc.wantCalls {
				t.Errorf("upstream calls = %d, want %d", got, tc.wantCalls)
			}
			if tc.wantBody != "" && response.Body.String() != tc.wantBody {
				t.Errorf("response body = %q, want %q", response.Body.String(), tc.wantBody)
			}

			if got := testutil.ToFloat64(retryAttempts.WithLabelValues(tc.retryReason, variant)) - retryBefore; got != tc.wantRetryIncrements {
				t.Errorf("retry metric %q increment = %v, want %v", tc.retryReason, got, tc.wantRetryIncrements)
			}
			if got := testutil.ToFloat64(upstreamErrors.WithLabelValues(tc.errorType, variant)) - errorBefore; got != tc.wantErrorIncrements {
				t.Errorf("upstream error metric %q increment = %v, want %v", tc.errorType, got, tc.wantErrorIncrements)
			}

			assertRetryContractDelays(t, observedDelays, tc.wantDelays)
			if len(callsWhenSleeping) != len(tc.wantCallsWhenSleeping) {
				t.Fatalf("sleep call-count observations = %v, want %v", callsWhenSleeping, tc.wantCallsWhenSleeping)
			}
			for i, want := range tc.wantCallsWhenSleeping {
				if got := callsWhenSleeping[i]; got != want {
					t.Errorf("sleep %d followed upstream call %d, want %d", i, got, want)
				}
			}
		})
	}
}

func assertRetryContractDelays(t *testing.T, got, want []time.Duration) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("retry delays = %v, want %v", got, want)
	}
	for i, wantDelay := range want {
		if gotDelay := got[i]; gotDelay != wantDelay {
			t.Errorf("retry delay %d = %v, want %v", i, gotDelay, wantDelay)
		}
	}
}
