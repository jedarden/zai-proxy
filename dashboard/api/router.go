// Package api implements the HTTP REST API and SSE hub.
package api

import (
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/ardenone/ardenone-cluster/containers/zai-proxy-dashboard/model"
	"github.com/ardenone/ardenone-cluster/containers/zai-proxy-dashboard/storage"
)

// Router sets up the HTTP routes.
type Router struct {
	hub      *SSEHub
	storage  *storage.Storage
	config   *Config
}

// Config holds API configuration.
type Config struct {
	ListenAddr     string
	ScrapeInterval time.Duration
	ScrapeTargets  []string
}

// DefaultConfig returns the default API configuration.
func DefaultConfig() *Config {
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	interval := 5 * time.Second
	if v := os.Getenv("SCRAPE_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			interval = d
		}
	}

	targets := []string{"http://zai-proxy.mcp.svc.cluster.local:8080/metrics"}
	if v := os.Getenv("SCRAPE_TARGETS"); v != "" {
		// Simple split by comma
		for i, t := range splitTargets(v) {
			if i == 0 {
				targets = nil
			}
			targets = append(targets, t)
		}
	}

	return &Config{
		ListenAddr:     addr,
		ScrapeInterval: interval,
		ScrapeTargets:  targets,
	}
}

func splitTargets(s string) []string {
	var result []string
	var current string
	for _, c := range s {
		if c == ',' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

// NewRouter creates a new Router.
func NewRouter(hub *SSEHub, store *storage.Storage, config *Config) *Router {
	return &Router{
		hub:     hub,
		storage: store,
		config:  config,
	}
}

// SetupRoutes configures the HTTP routes.
func (r *Router) SetupRoutes(mux *http.ServeMux) {
	// REST API endpoints
	mux.HandleFunc("/api/metrics", r.handleMetrics)
	mux.HandleFunc("/api/status", r.handleStatus)
	mux.HandleFunc("/api/config", r.handleConfig)
	mux.HandleFunc("/healthz", r.handleHealth)

	// SSE endpoint for real-time metric streaming
	mux.HandleFunc("/api/events", r.hub.HandleSSE)

	// Note: Frontend "/" route is handled by main.go with embedded files
}

func (r *Router) handleHealth(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// Hub returns the SSE hub.
func (r *Router) Hub() *SSEHub {
	return r.hub
}

// handleMetrics returns historical metrics.
// Query params: range (5m, 15m, 1h, 6h, 24h, 7d), variant (production, canary, all)
func (r *Router) handleMetrics(w http.ResponseWriter, req *http.Request) {
	rangeStr := req.URL.Query().Get("range")
	if rangeStr == "" {
		rangeStr = "1h"
	}

	variant := req.URL.Query().Get("variant")
	if variant == "" {
		variant = "all"
	}

	d, err := parseDuration(rangeStr)
	if err != nil {
		http.Error(w, "invalid range parameter", http.StatusBadRequest)
		return
	}

	snapshots, err := r.storage.QueryRange(d, variant)
	if err != nil {
		http.Error(w, "failed to query metrics", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Write JSON array
	w.Write([]byte("["))
	for i, s := range snapshots {
		if i > 0 {
			w.Write([]byte(","))
		}
		data, err := s.ToJSON()
		if err != nil {
			continue
		}
		w.Write(data)
	}
	w.Write([]byte("]"))
}

// handleStatus returns current proxy health summary.
func (r *Router) handleStatus(w http.ResponseWriter, req *http.Request) {
	latest, err := r.storage.GetLatest()
	if err != nil {
		http.Error(w, "failed to get status", http.StatusInternalServerError)
		return
	}

	resp := &model.StatusResponse{}
	for variant, snapshot := range latest {
		status := &model.VariantStatus{
			Healthy:           true, // Assume healthy if we have data
			LastScrape:        time.UnixMilli(snapshot.Timestamp),
			ReqRate:           snapshot.ReqRate,
			ErrorRatePct:      snapshot.ErrorRatePct,
			LatencyP50Ms:      snapshot.LatencyP50,
			Concurrent:        snapshot.ConcurrentRequests,
			WorkerUtilization: snapshot.WorkerUtilization,
			RateLimitRps:      snapshot.RateLimitRps,
			TokenRateIn:       snapshot.TokenRateIn,
			TokenRateOut:      snapshot.TokenRateOut,
		}

		if variant == "production" {
			resp.Production = status
		} else if variant == "canary" {
			resp.Canary = status
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	data, err := resp.MarshalJSON()
	if err != nil {
		http.Error(w, "failed to encode status", http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

// handleConfig returns dashboard configuration.
func (r *Router) handleConfig(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Simple JSON encoding
	w.Write([]byte("{"))
	w.Write([]byte(`"scrape_interval":`))
	w.Write([]byte(floatToString(r.config.ScrapeInterval.Seconds())))
	w.Write([]byte(`,"targets":[`))
	for i, t := range r.config.ScrapeTargets {
		if i > 0 {
			w.Write([]byte(","))
		}
		w.Write([]byte(`"` + t + `"`))
	}
	w.Write([]byte("]}"))
}

func floatToString(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// parseDuration parses duration strings like "5m", "1h", "7d".
func parseDuration(s string) (time.Duration, error) {
	switch s {
	case "5m":
		return 5 * time.Minute, nil
	case "15m":
		return 15 * time.Minute, nil
	case "1h":
		return time.Hour, nil
	case "6h":
		return 6 * time.Hour, nil
	case "24h":
		return 24 * time.Hour, nil
	case "7d":
		return 7 * 24 * time.Hour, nil
	default:
		return time.ParseDuration(s)
	}
}
