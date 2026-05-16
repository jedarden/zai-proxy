// Package api implements the SSE hub for real-time metric broadcasting.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ardenone/ardenone-cluster/containers/zai-proxy-dashboard/logger"
	"github.com/ardenone/ardenone-cluster/containers/zai-proxy-dashboard/model"
)

var sseLog = logger.Component("sse")

// SSEClient represents a connected SSE client.
type SSEClient struct {
	send chan []byte
	done chan struct{}
}

// SSEHub manages SSE connections and broadcasts metrics.
type SSEHub struct {
	clients map[*SSEClient]bool
	mu      sync.RWMutex

	broadcast chan *model.MetricSnapshot
	register  chan *SSEClient
	unregister chan *SSEClient

	config *Config
}

// NewSSEHub creates a new SSE hub.
func NewSSEHub(config *Config) *SSEHub {
	return &SSEHub{
		clients:    make(map[*SSEClient]bool),
		broadcast:  make(chan *model.MetricSnapshot, 100),
		register:   make(chan *SSEClient),
		unregister: make(chan *SSEClient),
		config:     config,
	}
}

// Run starts the hub's main loop.
func (h *SSEHub) Run() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			count := len(h.clients)
			h.mu.Unlock()
			sseLog.Info("client connected", "total_clients", count)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				count := len(h.clients)
				sseLog.Info("client disconnected", "total_clients", count)
			}
			h.mu.Unlock()

		case snapshot := <-h.broadcast:
			msg := &model.SSEMessage{
				Type: "metrics",
				Data: snapshot,
			}
			data, err := json.Marshal(msg)
			if err != nil {
				sseLog.Error("failed to marshal broadcast message", "error", err)
				continue
			}

			h.mu.RLock()
			var slow []*SSEClient
			for client := range h.clients {
				select {
				case client.send <- data:
				default:
					slow = append(slow, client)
				}
			}
			h.mu.RUnlock()

			// Unregister slow consumers from a separate goroutine to avoid
			// deadlocking the Run() loop (h.unregister is consumed by Run()).
			for _, c := range slow {
				go func(cl *SSEClient) { h.unregister <- cl }(c)
			}

		case <-ticker.C:
			h.mu.RLock()
			count := len(h.clients)
			h.mu.RUnlock()
			if count > 0 {
				sseLog.Debug("connection stats", "active_clients", count)
			}
		}
	}
}

// Broadcast sends a metric snapshot to all connected clients.
func (h *SSEHub) Broadcast(snapshot *model.MetricSnapshot) {
	select {
	case h.broadcast <- snapshot:
	default:
		sseLog.Warn("broadcast channel full, dropping snapshot", "variant", snapshot.Variant)
	}
}

// HandleSSE handles SSE connection requests.
func (h *SSEHub) HandleSSE(w http.ResponseWriter, r *http.Request) {
	// SSE requires these headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	client := &SSEClient{
		send: make(chan []byte, 16),
		done: make(chan struct{}),
	}

	h.register <- client
	defer func() { h.unregister <- client }()

	// Send connected event
	connMsg := &model.SSEMessage{
		Type:           "connected",
		ScrapeInterval: int(h.config.ScrapeInterval.Seconds()),
		Variants:       []string{"production", "canary"},
	}
	connData, _ := json.Marshal(connMsg)
	fmt.Fprintf(w, "data: %s\n\n", connData)
	flusher.Flush()

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case data, ok := <-client.send:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

		case <-heartbeat.C:
			// Send SSE comment as keepalive
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()

		case <-r.Context().Done():
			return
		}
	}
}

// ClientCount returns the number of connected clients.
func (h *SSEHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
