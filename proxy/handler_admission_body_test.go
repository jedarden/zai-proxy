package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests pin the ordering of body capture against the admission wait.
// waitForClient can hold a request for seconds, and the fronting proxy in
// front of the real deployment enforces a read deadline over the whole
// client request, body included (Traefik's vpn entrypoint defaults to 60s).
// A body first read only after the wait was severed mid-transfer whenever it
// outgrew the socket buffers, surfacing to callers as 499 Client Closed
// (2026-10-29). ServeHTTP must therefore buffer the body before waiting.

// newReadDeadlineProxy serves handler from a real http.Server with the given
// ReadTimeout, standing in for the fronting proxy's read deadline. It returns
// the proxy's base URL.
func newReadDeadlineProxy(t *testing.T, handler http.Handler, readTimeout time.Duration) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: handler, ReadTimeout: readTimeout}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return "http://" + ln.Addr().String()
}

// admissionBodyUpstream records every request body it receives and answers
// with a minimal valid JSON response.
func admissionBodyUpstream(t *testing.T) (*httptest.Server, func() [][]byte) {
	t.Helper()
	var mu sync.Mutex
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, got)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)
	return server, func() [][]byte {
		mu.Lock()
		defer mu.Unlock()
		return bodies
	}
}

// largeJSONBody builds a valid JSON Anthropic-style request of exactly total
// bytes by padding the single user message.
func largeJSONBody(total int) []byte {
	prefix := []byte(`{"model":"glm-4","messages":[{"role":"user","content":"`)
	suffix := []byte(`"}]}`)
	body := make([]byte, 0, total)
	body = append(body, prefix...)
	body = append(body, bytes.Repeat([]byte("A"), total-len(prefix)-len(suffix))...)
	body = append(body, suffix...)
	return body
}

// newAdmissionWaitHandler builds a handler whose limiter admits the first
// request immediately and then holds the next one for ~2s: at 0.5 req/s the
// default burst of one token is drained by the first request, and the second
// waits a full token interval. A 1s server read deadline then expires in the
// middle of that wait, which is the production failure shape compressed
// 60:1.
func newAdmissionWaitHandler(upstreamURL string) *ProxyHandler {
	return NewProxyHandler(
		"test-key", upstreamURL,
		0, // maxRetries: one attempt, no retry waits
		0, // maxWorkers: capacity check disabled
		"admission-body", nil, "glm-4",
		0.5, // initialRate
		0.1, // minRate
		10,  // maxRate
	)
}

func postJSONClient() *http.Client {
	// Fresh connection per request: keep-alive reuse would entangle the
	// priming request's timing with the measured one.
	return &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
}

// TestLargeBodySurvivesAdmissionWait posts a body far larger than the socket
// buffers through a 1s read deadline while admission holds the request for
// ~2s. The upstream must receive the complete body and the client a 200:
// anything less means the body was still unread when the deadline fired.
func TestLargeBodySurvivesAdmissionWait(t *testing.T) {
	upstream, receivedBodies := admissionBodyUpstream(t)
	proxyURL := newReadDeadlineProxy(t, newAdmissionWaitHandler(upstream.URL), time.Second)
	client := postJSONClient()

	// Prime the token bucket so the measured request waits ~2s for admission.
	prime, err := http.NewRequest(http.MethodPost, proxyURL+"/v1/messages",
		strings.NewReader(`{"model":"glm-4","messages":[]}`))
	if err != nil {
		t.Fatalf("building prime request: %v", err)
	}
	resp, err := client.Do(prime)
	if err != nil {
		t.Fatalf("prime request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("prime request status = %d, want 200", resp.StatusCode)
	}

	body := largeJSONBody(8 << 20) // 8 MiB, well past any socket buffer
	req, err := http.NewRequest(http.MethodPost, proxyURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("building measured request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("measured request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("measured request status = %d, want 200", resp.StatusCode)
	}

	bodies := receivedBodies()
	if len(bodies) != 2 {
		t.Fatalf("upstream received %d requests, want 2", len(bodies))
	}
	if !bytes.Equal(bodies[1], body) {
		t.Fatalf("upstream received %d of %d body bytes", len(bodies[1]), len(body))
	}
}

// TestClientAbandonDuringAdmissionWait cancels the client mid-admission-wait.
// The buffered body must not be forwarded: upstream quota would be spent on a
// caller that is already gone. Only the priming request may reach upstream.
func TestClientAbandonDuringAdmissionWait(t *testing.T) {
	upstream, receivedBodies := admissionBodyUpstream(t)
	proxyURL := newReadDeadlineProxy(t, newAdmissionWaitHandler(upstream.URL), 30*time.Second)
	client := postJSONClient()

	prime, err := http.NewRequest(http.MethodPost, proxyURL+"/v1/messages",
		strings.NewReader(`{"model":"glm-4","messages":[]}`))
	if err != nil {
		t.Fatalf("building prime request: %v", err)
	}
	resp, err := client.Do(prime)
	if err != nil {
		t.Fatalf("prime request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("prime request status = %d, want 200", resp.StatusCode)
	}

	// Cancel 1.2s in, inside the ~2s admission wait.
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, proxyURL+"/v1/messages",
		strings.NewReader(`{"model":"glm-4","messages":[]}`))
	if err != nil {
		t.Fatalf("building measured request: %v", err)
	}
	if _, err := client.Do(req); err == nil {
		t.Fatal("measured request: expected cancellation error, got nil")
	}

	// The handler returns after its uncancellable admission wait ends; give
	// it the full wait plus slack before asserting on upstream traffic.
	time.Sleep(1200 * time.Millisecond)
	if bodies := receivedBodies(); len(bodies) != 1 {
		t.Fatalf("upstream received %d requests, want 1 (the prime only)", len(bodies))
	}
}
