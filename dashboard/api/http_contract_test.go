package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.ardenone.com/jedarden/zai-proxy/dashboard/model"
	"git.ardenone.com/jedarden/zai-proxy/dashboard/storage"
)

type documentedJSONType string

const (
	documentedBoolean documentedJSONType = "boolean"
	documentedNumber  documentedJSONType = "number"
	documentedObject  documentedJSONType = "object"
	documentedString  documentedJSONType = "string"
	documentedArray   documentedJSONType = "array"
)

func TestDocumentedHTTPContracts(t *testing.T) {
	apiConfig := &Config{
		ListenAddr:     ":8080",
		ScrapeInterval: 5 * time.Second,
		ScrapeTargets:  []string{"http://proxy.example/metrics"},
	}
	store := storage.NewStorage(storage.Config{
		Retention5s: time.Hour,
		Retention1m: 24 * time.Hour,
		MaxVariants: 2,
	})
	defer store.Close()

	now := time.Now().UnixMilli()
	store.Write(&model.MetricSnapshot{
		Timestamp:            now,
		Variant:              "production",
		Requests2xx:          100,
		Requests4xx:          2,
		Requests5xx:          1,
		TokensInput:          1000,
		TokensOutput:         2000,
		TokensCacheRead:      300,
		TokensCacheWrite:     40,
		ConcurrentRequests:   8,
		MaxWorkers:           25,
		RateLimitRps:         50,
		RateLimitRejections:  3,
		RateLimitAdjIncrease: 1,
		RateLimitAdjDecrease: 2,
		UpstreamErrors:       1,
		RetryAttempts:        2,
		LatencyP50:           145,
		LatencyP95:           280,
		LatencyP99:           450,
		RequestSizeAvg:       1024,
		ResponseSizeAvg:      8192,
		TokenRateIn:          1250.5,
		TokenRateOut:         3800.2,
		TokenRateCacheRead:   250.3,
		TokenRateCacheWrite:  100.1,
		ReqRate:              42.5,
		ErrorRatePct:         0.12,
		WorkerUtilization:    0.32,
		StatusCodeRates:      map[string]float64{"200": 40.2},
	})
	store.Write(&model.MetricSnapshot{Timestamp: now, Variant: "canary", ReqRate: 2.1})

	hub := NewSSEHub(apiConfig)
	go hub.Run()
	router := NewRouter(hub, store, apiConfig)
	mux := http.NewServeMux()
	router.SetupRoutes(mux)
	handler := Chain(mux, RecoveryMiddleware, LoggingMiddleware, CORSMiddleware)
	server := httptest.NewServer(handler)
	defer server.Close()

	tests := []struct {
		name  string
		path  string
		check func(t *testing.T, body []byte)
	}{
		{
			name: "health",
			path: "/healthz",
			check: func(t *testing.T, body []byte) {
				fields := decodeJSONObject(t, body)
				assertDocumentedFields(t, fields, map[string]documentedJSONType{
					"status": documentedString,
				})
				if got := decodeString(t, fields["status"]); got != "ok" {
					t.Errorf("status = %q, want %q", got, "ok")
				}
			},
		},
		{
			name: "config",
			path: "/api/config",
			check: func(t *testing.T, body []byte) {
				fields := decodeJSONObject(t, body)
				assertDocumentedFields(t, fields, map[string]documentedJSONType{
					"scrape_interval": documentedNumber,
					"targets":         documentedArray,
				})
				if got := decodeNumber(t, fields["scrape_interval"]); got != 5 {
					t.Errorf("scrape_interval = %v, want 5", got)
				}
				var targets []string
				if err := json.Unmarshal(fields["targets"], &targets); err != nil {
					t.Fatalf("targets is not string[]: %v", err)
				}
				if len(targets) != 1 || targets[0] != apiConfig.ScrapeTargets[0] {
					t.Errorf("targets = %#v, want %#v", targets, apiConfig.ScrapeTargets)
				}
			},
		},
		{
			name: "status",
			path: "/api/status",
			check: func(t *testing.T, body []byte) {
				fields := decodeJSONObject(t, body)
				for _, variant := range []string{"production", "canary"} {
					variantFields := decodeJSONObject(t, fields[variant])
					assertDocumentedFields(t, variantFields, map[string]documentedJSONType{
						"healthy":            documentedBoolean,
						"last_scrape":        documentedString,
						"req_rate":           documentedNumber,
						"error_rate_pct":     documentedNumber,
						"latency_p50_ms":     documentedNumber,
						"concurrent":         documentedNumber,
						"worker_utilization": documentedNumber,
						"rate_limit_rps":     documentedNumber,
						"token_rate_in":      documentedNumber,
						"token_rate_out":     documentedNumber,
					})
					if _, err := time.Parse(time.RFC3339Nano, decodeString(t, variantFields["last_scrape"])); err != nil {
						t.Errorf("%s.last_scrape is not an ISO 8601 timestamp: %v", variant, err)
					}
				}
			},
		},
		{
			name: "metrics",
			path: "/api/metrics?range=1h&variant=production",
			check: func(t *testing.T, body []byte) {
				var snapshots []map[string]json.RawMessage
				if err := json.Unmarshal(body, &snapshots); err != nil {
					t.Fatalf("metrics response is not a JSON array: %v", err)
				}
				if len(snapshots) != 1 {
					t.Fatalf("metrics response has %d snapshots, want 1", len(snapshots))
				}
				assertDocumentedFields(t, snapshots[0], map[string]documentedJSONType{
					"timestamp":               documentedNumber,
					"variant":                 documentedString,
					"requests_2xx":            documentedNumber,
					"requests_4xx":            documentedNumber,
					"requests_5xx":            documentedNumber,
					"tokens_input":            documentedNumber,
					"tokens_output":           documentedNumber,
					"tokens_cache_read":       documentedNumber,
					"tokens_cache_write":      documentedNumber,
					"concurrent_requests":     documentedNumber,
					"max_workers":             documentedNumber,
					"rate_limit_rps":          documentedNumber,
					"rate_limit_rejections":   documentedNumber,
					"rate_limit_adj_increase": documentedNumber,
					"rate_limit_adj_decrease": documentedNumber,
					"upstream_errors":         documentedNumber,
					"retry_attempts":          documentedNumber,
					"latency_p50":             documentedNumber,
					"latency_p95":             documentedNumber,
					"latency_p99":             documentedNumber,
					"request_size_avg":        documentedNumber,
					"response_size_avg":       documentedNumber,
					"token_rate_in":           documentedNumber,
					"token_rate_out":          documentedNumber,
					"token_rate_cache_read":   documentedNumber,
					"token_rate_cache_write":  documentedNumber,
					"req_rate":                documentedNumber,
					"error_rate_pct":          documentedNumber,
					"worker_utilization":      documentedNumber,
					"status_code_rates":       documentedObject,
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := server.Client().Get(server.URL + tt.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s status = %d, want 200", tt.path, resp.StatusCode)
			}
			if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
				t.Errorf("GET %s CORS origin = %q, want *", tt.path, got)
			}
			tt.check(t, mustReadAll(t, resp))
		})
	}

	t.Run("events", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/events", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("GET /api/events: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/events status = %d, want 200", resp.StatusCode)
		}
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("GET /api/events CORS origin = %q, want *", got)
		}
		if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
			t.Fatalf("GET /api/events Content-Type = %q, want text/event-stream", got)
		}

		scanner := bufio.NewScanner(resp.Body)
		var event map[string]json.RawMessage
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
					t.Fatalf("SSE data payload is not parseable JSON: %v", err)
				}
				break
			}
		}
		if err := scanner.Err(); err != nil {
			t.Fatalf("read SSE stream: %v", err)
		}
		if event == nil {
			t.Fatal("SSE stream emitted no data event")
		}
		assertDocumentedFields(t, event, map[string]documentedJSONType{
			"type":            documentedString,
			"scrape_interval": documentedNumber,
			"variants":        documentedArray,
		})
		if got := decodeString(t, event["type"]); got != "connected" {
			t.Errorf("first SSE event type = %q, want connected", got)
		}
		var variants []string
		if err := json.Unmarshal(event["variants"], &variants); err != nil {
			t.Fatalf("SSE variants is not string[]: %v", err)
		}
		if len(variants) != 2 || variants[0] != "production" || variants[1] != "canary" {
			t.Errorf("SSE variants = %#v, want production and canary", variants)
		}
	})
}

func decodeJSONObject(t *testing.T, body []byte) map[string]json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatalf("response is not a JSON object: %v", err)
	}
	return fields
}

func assertDocumentedFields(t *testing.T, fields map[string]json.RawMessage, expected map[string]documentedJSONType) {
	t.Helper()
	for name, want := range expected {
		raw, ok := fields[name]
		if !ok {
			t.Errorf("missing documented field %q", name)
			continue
		}
		if got := jsonType(raw); got != want {
			t.Errorf("field %q has JSON type %q, want %q", name, got, want)
		}
	}
}

func jsonType(raw json.RawMessage) documentedJSONType {
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return "invalid"
	}
	switch value.(type) {
	case bool:
		return documentedBoolean
	case json.Number:
		return documentedNumber
	case string:
		return documentedString
	case []any:
		return documentedArray
	case map[string]any:
		return documentedObject
	default:
		return "null"
	}
}

func decodeString(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("field is not a string: %v", err)
	}
	return value
}

func decodeNumber(t *testing.T, raw json.RawMessage) float64 {
	t.Helper()
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("field is not a number: %v", err)
	}
	return value
}

func mustReadAll(t *testing.T, response *http.Response) []byte {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return body
}
