package main

import (
	"io"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestAssertResponseStructure(t *testing.T) {
	responseBody := `{
		"id":"msg_123",
		"usage":{"input_tokens":10,"output_tokens":5},
		"content":[{"type":"text","text":"hello"}],
		"extra":"permitted"
	}`
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(responseBody))}

	AssertResponseStructure(t, resp, map[string]interface{}{
		"id": "",
		"usage": map[string]interface{}{
			"input_tokens":  0,
			"output_tokens": float64(0),
		},
		"content": []interface{}{map[string]interface{}{
			"type": "",
			"text": "",
		}},
	})

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read restored response body: %v", err)
	}
	if string(got) != responseBody {
		t.Errorf("response body changed after structure assertion: got %q", got)
	}
}

func TestValidateResponseStructure(t *testing.T) {
	actual := map[string]interface{}{
		"id":       "msg_123",
		"count":    float64(3),
		"metadata": map[string]interface{}{"active": true},
		"items":    []interface{}{map[string]interface{}{"name": "first"}},
	}

	testCases := []struct {
		name        string
		expected    map[string]interface{}
		wantError   bool
		wantInError string
	}{
		{
			name: "nested structure and numeric exemplar",
			expected: map[string]interface{}{
				"id":    "",
				"count": 0,
				"metadata": map[string]interface{}{
					"active": false,
				},
				"items": []interface{}{map[string]interface{}{"name": ""}},
			},
		},
		{
			name:        "missing required field",
			expected:    map[string]interface{}{"missing": nil},
			wantError:   true,
			wantInError: "$.missing",
		},
		{
			name: "wrong nested type",
			expected: map[string]interface{}{
				"metadata": []interface{}{},
			},
			wantError:   true,
			wantInError: "expected array",
		},
		{
			name: "array elements must match schema",
			expected: map[string]interface{}{
				"items": []interface{}{map[string]interface{}{"missing": nil}},
			},
			wantError:   true,
			wantInError: "$.items[0].missing",
		},
		{
			name: "multiple array schemas are rejected",
			expected: map[string]interface{}{
				"items": []interface{}{map[string]interface{}{}, map[string]interface{}{}},
			},
			wantError:   true,
			wantInError: "array element definitions",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateResponseStructure(actual, tc.expected)
			if (err != nil) != tc.wantError {
				t.Fatalf("validateResponseStructure() error = %v, wantError %t", err, tc.wantError)
			}
			if tc.wantInError != "" && !strings.Contains(err.Error(), tc.wantInError) {
				t.Errorf("validateResponseStructure() error = %q, want substring %q", err, tc.wantInError)
			}
		})
	}
}

func TestAssertErrorResponse(t *testing.T) {
	testCases := []struct {
		name           string
		status         int
		body           string
		expectedStatus int
		expectedText   string
	}{
		{
			name:           "JSON string error",
			status:         http.StatusUnprocessableEntity,
			body:           `{"error":"unprocessable entity"}`,
			expectedStatus: http.StatusUnprocessableEntity,
			expectedText:   "unprocessable",
		},
		{
			name:           "nested JSON error message",
			status:         http.StatusBadRequest,
			body:           `{"error":{"message":"invalid request body"}}`,
			expectedStatus: http.StatusBadRequest,
			expectedText:   "request body",
		},
		{
			name:           "plain text http error",
			status:         http.StatusTooManyRequests,
			body:           "Rate limit exceeded\n",
			expectedStatus: http.StatusTooManyRequests,
			expectedText:   "Rate limit exceeded",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tc.status,
				Body:       io.NopCloser(strings.NewReader(tc.body)),
			}
			AssertErrorResponse(t, resp, tc.expectedStatus, tc.expectedText)

			got, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read restored error response: %v", err)
			}
			if string(got) != tc.body {
				t.Errorf("error response body changed after assertion: got %q, want %q", got, tc.body)
			}
		})
	}
}

func TestErrorResponseValidation(t *testing.T) {
	testCases := []struct {
		name     string
		status   int
		expected int
		actual   string
		want     string
	}{
		{name: "matching error", status: 429, expected: 429, actual: "rate limit exceeded"},
		{name: "success is not valid expectation", status: 200, expected: 200, actual: "error", want: "4xx or 5xx"},
		{name: "status mismatch", status: 500, expected: 502, actual: "upstream error", want: "expected 502"},
		{name: "empty message", status: 500, expected: 500, actual: " ", want: "message is empty"},
		{name: "message mismatch", status: 400, expected: 400, actual: "invalid request", want: "expected to contain"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateErrorResponse(tc.status, tc.expected, tc.actual, "limit")
			if tc.want == "" {
				if err != nil {
					t.Fatalf("validateErrorResponse() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("validateErrorResponse() error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestAssertMetricsIncremented(t *testing.T) {
	metric := prometheus.NewCounter(prometheus.CounterOpts{Name: "advanced_assertions_test_total", Help: "test counter"})
	metric.Add(2.5)
	AssertMetricsIncremented(t, metric, 0, 2.5)
}

func TestValidateMetricsIncremented(t *testing.T) {
	testCases := []struct {
		name      string
		before    float64
		actual    float64
		expected  float64
		wantError bool
	}{
		{name: "exact increment", before: 2, actual: 5, expected: 3},
		{name: "rounding tolerance", before: 0, actual: 0.30000000000000004, expected: 0.3},
		{name: "wrong increment", before: 2, actual: 4, expected: 3, wantError: true},
		{name: "negative expected increment", before: 2, actual: 1, expected: -1, wantError: true},
		{name: "NaN baseline", before: math.NaN(), actual: 1, expected: 1, wantError: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateMetricsIncremented(tc.before, tc.actual, tc.expected)
			if (err != nil) != tc.wantError {
				t.Errorf("validateMetricsIncremented() error = %v, wantError %t", err, tc.wantError)
			}
		})
	}
}

func TestAssertRateLimitBehavior(t *testing.T) {
	responses := []*http.Response{
		{StatusCode: http.StatusOK},
		{StatusCode: http.StatusCreated},
		{StatusCode: http.StatusTooManyRequests},
	}
	AssertRateLimitBehavior(t, responses, 2, 1)
}

func TestValidateRateLimitBehavior(t *testing.T) {
	testCases := []struct {
		name        string
		statusCodes []int
		allowed     int
		rateLimited int
		wantError   bool
	}{
		{name: "2xx responses followed by 429", statusCodes: []int{200, 204, 429}, allowed: 2, rateLimited: 1},
		{name: "no rate limited requests", statusCodes: []int{200}, allowed: 1},
		{name: "response count mismatch", statusCodes: []int{200}, allowed: 1, rateLimited: 1, wantError: true},
		{name: "non-success before limit", statusCodes: []int{500, 429}, allowed: 1, rateLimited: 1, wantError: true},
		{name: "429 arrives too early", statusCodes: []int{429, 200}, allowed: 1, rateLimited: 1, wantError: true},
		{name: "wrong status after limit", statusCodes: []int{200, 503}, allowed: 1, rateLimited: 1, wantError: true},
		{name: "negative expected count", statusCodes: nil, allowed: -1, wantError: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRateLimitBehavior(tc.statusCodes, tc.allowed, tc.rateLimited)
			if (err != nil) != tc.wantError {
				t.Errorf("validateRateLimitBehavior() error = %v, wantError %t", err, tc.wantError)
			}
		})
	}
}
