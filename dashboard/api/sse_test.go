package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ardenone/ardenone-cluster/containers/zai-proxy-dashboard/model"
)

func newTestSSEHub() *SSEHub {
	return NewSSEHub(&Config{
		ListenAddr:     ":8080",
		ScrapeInterval: 5 * time.Second,
		ScrapeTargets:  []string{"http://localhost:8080/metrics"},
	})
}

// readSSEEvent reads one SSE data line from a response body.
func readSSEEvent(t *testing.T, scanner *bufio.Scanner) *model.SSEMessage {
	t.Helper()
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			payload := strings.TrimPrefix(line, "data: ")
			var msg model.SSEMessage
			if err := json.Unmarshal([]byte(payload), &msg); err != nil {
				t.Fatalf("failed to unmarshal SSE event: %v", err)
			}
			return &msg
		}
	}
	t.Fatal("SSE stream ended without data event")
	return nil
}

func TestSSEHub_Connect(t *testing.T) {
	hub := newTestSSEHub()
	go hub.Run()

	server := httptest.NewServer(http.HandlerFunc(hub.HandleSSE))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %q", ct)
	}

	scanner := bufio.NewScanner(resp.Body)
	msg := readSSEEvent(t, scanner)
	if msg.Type != "connected" {
		t.Errorf("expected 'connected' message, got %q", msg.Type)
	}
	if msg.ScrapeInterval != 5 {
		t.Errorf("expected scrape_interval 5, got %d", msg.ScrapeInterval)
	}
}

func TestSSEHub_Broadcast(t *testing.T) {
	hub := newTestSSEHub()
	go hub.Run()

	server := httptest.NewServer(http.HandlerFunc(hub.HandleSSE))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)

	// Skip connected message
	readSSEEvent(t, scanner)

	// Wait for client to register
	time.Sleep(50 * time.Millisecond)

	snapshot := &model.MetricSnapshot{
		Timestamp:         time.Now().UnixMilli(),
		Variant:           "production",
		Requests2xx:       100,
		ReqRate:           2.5,
		LatencyP50:        150.0,
		WorkerUtilization: 0.75,
	}
	hub.Broadcast(snapshot)

	msg := readSSEEvent(t, scanner)
	if msg.Type != "metrics" {
		t.Errorf("expected 'metrics' message, got %q", msg.Type)
	}
	if msg.Data == nil {
		t.Fatal("expected non-nil data")
	}
	if msg.Data.ReqRate != 2.5 {
		t.Errorf("expected req_rate 2.5, got %f", msg.Data.ReqRate)
	}
}

func TestSSEHub_EmptyHub(t *testing.T) {
	hub := newTestSSEHub()
	go hub.Run()

	snapshot := &model.MetricSnapshot{
		Timestamp: time.Now().UnixMilli(),
		Variant:   "production",
	}
	for i := 0; i < 10; i++ {
		hub.Broadcast(snapshot)
	}

	if hub.ClientCount() != 0 {
		t.Errorf("expected 0 clients, got %d", hub.ClientCount())
	}
}

// TestSSE_NoWebSocketUpgrade verifies that the SSE endpoint does NOT require
// a WebSocket upgrade (HTTP 101). This is the regression test for bd-5ark:
// cloudflared tunnels with http2Origin=true use HTTP/2 CONNECT for WebSocket
// upgrades, which breaks gorilla/websocket backends. SSE works natively over
// both HTTP/1.1 and HTTP/2 without any protocol upgrade.
func TestSSE_NoWebSocketUpgrade(t *testing.T) {
	hub := newTestSSEHub()
	go hub.Run()

	server := httptest.NewServer(http.HandlerFunc(hub.HandleSSE))
	defer server.Close()

	// Simulate a standard HTTP GET (no Upgrade header)
	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Must be 200 OK, NOT 101 Switching Protocols (WebSocket)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 OK, got %d", resp.StatusCode)
	}

	// Must be text/event-stream, NOT any WebSocket-related content type
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %q", ct)
	}

	// Must NOT have Connection: Upgrade header
	if conn := resp.Header.Get("Connection"); strings.EqualFold(conn, "Upgrade") {
		t.Error("SSE response must not include Connection: Upgrade header")
	}

	// Must NOT have Upgrade header
	if upgrade := resp.Header.Get("Upgrade"); upgrade != "" {
		t.Errorf("SSE response must not include Upgrade header, got %q", upgrade)
	}

	// Verify the initial "connected" SSE message arrives
	scanner := bufio.NewScanner(resp.Body)
	msg := readSSEEvent(t, scanner)
	if msg.Type != "connected" {
		t.Errorf("expected 'connected' message, got %q", msg.Type)
	}
}

// TestSSE_RouterEndpoint verifies that /api/events is properly registered
// and accessible through the full router with middleware chain.
func TestSSE_RouterEndpoint(t *testing.T) {
	hub := newTestSSEHub()
	go hub.Run()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/events", hub.HandleSSE)

	handler := Chain(mux, RecoveryMiddleware, CORSMiddleware)

	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/events")
	if err != nil {
		t.Fatalf("failed to connect to /api/events: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %q", ct)
	}

	// Verify CORS headers are present (middleware chain applied)
	if cors := resp.Header.Get("Access-Control-Allow-Origin"); cors != "*" {
		t.Errorf("expected CORS header *, got %q", cors)
	}

	// Verify initial connected message
	scanner := bufio.NewScanner(resp.Body)
	msg := readSSEEvent(t, scanner)
	if msg.Type != "connected" {
		t.Errorf("expected 'connected' message, got %q", msg.Type)
	}
}

// TestSSE_MultipleClients verifies multiple concurrent SSE clients
// all receive broadcast messages (regression for connection isolation).
func TestSSE_MultipleClients(t *testing.T) {
	hub := newTestSSEHub()
	go hub.Run()

	server := httptest.NewServer(http.HandlerFunc(hub.HandleSSE))
	defer server.Close()

	// Connect two clients
	resp1, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("client 1 failed: %v", err)
	}
	defer resp1.Body.Close()

	resp2, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("client 2 failed: %v", err)
	}
	defer resp2.Body.Close()

	// Both should be connected
	if hub.ClientCount() != 2 {
		t.Errorf("expected 2 clients, got %d", hub.ClientCount())
	}

	// Skip connected messages
	scan1 := bufio.NewScanner(resp1.Body)
	scan2 := bufio.NewScanner(resp2.Body)
	readSSEEvent(t, scan1)
	readSSEEvent(t, scan2)

	// Wait for registration
	time.Sleep(50 * time.Millisecond)

	// Broadcast
	snapshot := &model.MetricSnapshot{
		Timestamp: time.Now().UnixMilli(),
		Variant:   "canary",
		ReqRate:   5.0,
	}
	hub.Broadcast(snapshot)

	// Both clients should receive the broadcast
	msg1 := readSSEEvent(t, scan1)
	if msg1.Type != "metrics" || msg1.Data == nil || msg1.Data.ReqRate != 5.0 {
		t.Errorf("client 1: expected metrics with req_rate=5.0, got %+v", msg1)
	}

	msg2 := readSSEEvent(t, scan2)
	if msg2.Type != "metrics" || msg2.Data == nil || msg2.Data.ReqRate != 5.0 {
		t.Errorf("client 2: expected metrics with req_rate=5.0, got %+v", msg2)
	}
}
