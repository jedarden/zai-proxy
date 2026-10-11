package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocumentedProxyHTTPContracts(t *testing.T) {
	rateLimiter := NewAdaptiveRateLimiter(8, 0.5, 40)
	mux := http.NewServeMux()
	registerHTTPHandlers(mux, nil, rateLimiter, nil, 8)
	server := httptest.NewServer(mux)
	defer server.Close()

	tests := []struct {
		name  string
		path  string
		check func(t *testing.T, response *http.Response, body []byte)
	}{
		{
			name: "health",
			path: "/health",
			check: func(t *testing.T, response *http.Response, body []byte) {
				if got := response.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
					t.Errorf("Content-Type = %q, want application/json", got)
				}
				var payload map[string]json.RawMessage
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("/health response is not JSON: %v", err)
				}
				var status string
				if err := json.Unmarshal(payload["status"], &status); err != nil {
					t.Fatalf("/health status is not a string: %v", err)
				}
				if status != "ok" {
					t.Errorf("/health status = %q, want ok", status)
				}
			},
		},
		{
			name: "metrics",
			path: "/metrics",
			check: func(t *testing.T, response *http.Response, body []byte) {
				if got := response.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
					t.Errorf("Content-Type = %q, want text/plain", got)
				}
				if len(body) == 0 {
					t.Fatal("/metrics response is empty")
				}
				if !strings.Contains(string(body), "# HELP") || !strings.Contains(string(body), "# TYPE") {
					t.Fatal("/metrics response is not parseable Prometheus exposition text")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := server.Client().Get(server.URL + tt.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("GET %s status = %d, want 200", tt.path, response.StatusCode)
			}
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatalf("read GET %s response: %v", tt.path, err)
			}
			tt.check(t, response, body)
		})
	}
}
