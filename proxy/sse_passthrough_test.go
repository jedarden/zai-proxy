package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// The root README promises "SSE streaming — passes through chunked streaming
// responses without buffering" (README.md, Features). The tests in this file
// verify that promise end to end against a real proxy server (not a recording
// ResponseWriter) for both relay paths -- plain copy and token-counting usage
// injection -- plus the two lifecycle guarantees a streaming caller depends on:
// a client disconnect tears down the upstream request, and nothing is retried
// or re-streamed once the first chunk has been flushed.

// sseAckTimeout bounds how long the gated upstream waits for the client to
// observe a chunk before declaring the proxy held it back.
const sseAckTimeout = 10 * time.Second

// sseTestChunks is a realistic Anthropic-style SSE event sequence. Events are
// deliberately far below any transport buffer size, so none of them would
// reach the client without an explicit flush. There is no message_delta event:
// the token-counting path rewrites only message_delta, so both relay paths
// must forward these bytes verbatim.
var sseTestChunks = []string{
	"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_sse_001\",\"model\":\"glm-4\"}}\n\n",
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hel\"}}\n\n",
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"lo\"}}\n\n",
	"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
}

// newSSETestHandler builds a ProxyHandler the way CreateTestProxyHandler does,
// but lets the caller enable the token counter so the streaming relay can be
// exercised on the usage-injection path as well as the plain-copy path.
func newSSETestHandler(t *testing.T, upstreamURL string, maxRetries int, counter TokenCounter) *ProxyHandler {
	t.Helper()
	ConfigureTestEnv(t)
	t.Setenv(TestMaxRetriesEnv, strconv.Itoa(maxRetries))
	t.Setenv("ZAI_API_KEY", "test-key")
	handler := NewProxyHandler(
		"test-key",
		upstreamURL,
		maxRetries,
		100,     // maxWorkers
		"test",  // deploymentVariant
		counter, // tokenCounter (nil disables counting)
		"glm-4", // tokenizerModel
		1000.0,  // flat rate limits keep admission instant
		1000.0,
		1000.0,
	)
	// Retry waits vanish so a bug that retries would fail fast instead of
	// sleeping real backoff inside these tests.
	handler.retrySleep = func(time.Duration) {}
	return handler
}

// gatedSSEUpstream serves one streaming response whose chunk production is
// gated by client observation: after writing chunk i (and flushing it), the
// handler blocks until acks[i] is closed before producing chunk i+1. This
// turns "the client saw each chunk as it was produced" into a causal
// guarantee a buffering proxy cannot fake -- if the proxy held bytes back,
// no ack would ever arrive, the upstream would stall, and sseAckTimeout
// fails the test with a named side.
type gatedSSEUpstream struct {
	server *httptest.Server
	calls  atomic.Int32
	mu     sync.Mutex
	writes []time.Time // one entry per chunk, recorded after its flush returns
}

func newGatedSSEUpstream(t *testing.T, chunks []string, acks []chan struct{}) *gatedSSEUpstream {
	t.Helper()
	g := &gatedSSEUpstream{}
	g.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for i, chunk := range chunks {
			if _, err := io.WriteString(w, chunk); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			g.mu.Lock()
			g.writes = append(g.writes, time.Now())
			g.mu.Unlock()
			if i < len(acks) && acks[i] != nil {
				select {
				case <-acks[i]:
				case <-time.After(sseAckTimeout):
					t.Errorf("upstream: client never acknowledged chunk %d within %v -- proxy buffered it", i, sseAckTimeout)
					return
				}
			}
		}
	}))
	t.Cleanup(g.server.Close)
	return g
}

func (g *gatedSSEUpstream) writeTimes() []time.Time {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]time.Time(nil), g.writes...)
}

// collectSSEEvents reads body until want complete events (each terminated by
// a blank line) have been observed, closing acks[i] the moment event i
// completes, and returns the bytes read plus one arrival timestamp per event.
// The arrival timestamp is recorded before the ack closes, so the caller can
// assert arrival[i] happened strictly before the upstream was allowed to
// write chunk i+1.
func collectSSEEvents(body io.Reader, want int, acks []chan struct{}) ([]byte, []time.Time, error) {
	var data []byte
	var arrivals []time.Time
	buf := make([]byte, 256) // small reads force several relay rounds per event
	for len(arrivals) < want {
		n, err := body.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
			for bytes.Count(data, []byte("\n\n")) > len(arrivals) && len(arrivals) < want {
				arrivals = append(arrivals, time.Now())
				if acks != nil && len(arrivals)-1 < len(acks) && acks[len(arrivals)-1] != nil {
					close(acks[len(arrivals)-1])
				}
			}
		}
		if err != nil {
			return data, arrivals, fmt.Errorf("stream ended after %d of %d events: %w", len(arrivals), want, err)
		}
	}
	return data, arrivals, nil
}

// TestSSEPassthroughRelaysChunksIncrementally proves the no-buffering promise:
// every chunk is in the client's hands before the upstream has even produced
// the next one. Per-chunk arrival timing is asserted causally (ack-gated
// production plus write/arrival timestamp cross-checks) rather than by wall
// clock spacing, which would be flaky; the only wall clock bound is on the
// first chunk, which must not wait for anything later in the stream.
func TestSSEPassthroughRelaysChunksIncrementally(t *testing.T) {
	for _, tc := range []struct {
		name    string
		counter TokenCounter
	}{
		{name: "token counting disabled", counter: nil},
		{name: "token counting enabled", counter: NewSimpleTokenCounter()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			acks := make([]chan struct{}, len(sseTestChunks))
			for i := range acks {
				acks[i] = make(chan struct{})
			}
			upstream := newGatedSSEUpstream(t, sseTestChunks, acks)
			handler := newSSETestHandler(t, upstream.server.URL, 3, tc.counter)
			proxy := httptest.NewServer(handler)
			defer proxy.Close()

			start := time.Now()
			resp, err := http.Post(proxy.URL+"/v1/messages", "application/json", strings.NewReader(CreateStreamingRequestBody()))
			if err != nil {
				t.Fatalf("POST through proxy: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
			}
			if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
				t.Errorf("Content-Type = %q, want upstream's text/event-stream preserved", ct)
			}

			type readResult struct {
				data     []byte
				arrivals []time.Time
				err      error
			}
			resCh := make(chan readResult, 1)
			go func() {
				data, arrivals, err := collectSSEEvents(resp.Body, len(sseTestChunks), acks)
				resCh <- readResult{data: data, arrivals: arrivals, err: err}
			}()
			select {
			case res := <-resCh:
				if res.err != nil {
					t.Fatalf("client did not observe every chunk as it was produced: %v", res.err)
				}
				writes := upstream.writeTimes()
				if len(writes) != len(sseTestChunks) {
					t.Fatalf("upstream write timestamps = %d, want %d", len(writes), len(sseTestChunks))
				}
				for i := range res.arrivals {
					if !res.arrivals[i].After(writes[i]) {
						t.Errorf("event %d arrival %v not after its upstream write %v", i, res.arrivals[i], writes[i])
					}
					if i+1 < len(writes) && !res.arrivals[i].Before(writes[i+1]) {
						t.Errorf("event %d arrived at %v, but upstream wrote event %d at %v -- bytes were held back instead of relayed incrementally",
							i, res.arrivals[i], i+1, writes[i+1])
					}
				}
				// The first chunk must reach the client without waiting for
				// the rest of the stream; 2s is orders of magnitude above the
				// proxy's relay overhead.
				if first := res.arrivals[0]; first.Sub(start) > 2*time.Second {
					t.Errorf("first chunk arrived after %v, want well under 2s", first.Sub(start))
				}
				rest, drainErr := io.ReadAll(resp.Body)
				if drainErr != nil {
					t.Fatalf("draining stream after final event: %v", drainErr)
				}
				if got, want := string(append(res.data, rest...)), strings.Join(sseTestChunks, ""); got != want {
					t.Errorf("relayed payload = %q, want upstream bytes verbatim %q", got, want)
				}
			case <-time.After(4 * sseAckTimeout):
				t.Fatal("timed out waiting for SSE chunks -- relay did not deliver them as they were produced (buffering?)")
			}
		})
	}
}

// flushRecordingWriter is an http.ResponseWriter that records every Write and
// Flush the handler performs, for asserting that each relayed chunk is
// flushed, not just eventually written.
type flushRecordingWriter struct {
	header  http.Header
	onWrite func() // invoked after each Write; used to gate upstream production
	body    bytes.Buffer
	writes  int
	flushes int
	status  int
}

func (w *flushRecordingWriter) Header() http.Header { return w.header }
func (w *flushRecordingWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *flushRecordingWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.body.Write(p)
	w.writes++
	if w.onWrite != nil {
		w.onWrite()
	}
	return len(p), nil
}
func (w *flushRecordingWriter) Flush() { w.flushes++ }

// TestSSEPassthroughFlushesEveryChunk pins the flush half of the promise at
// the handler level: the upstream produces the next event only after the
// proxy has Written the previous one, so exactly one relayed chunk exists per
// Write, and every Write must be matched by a Flush -- an unflushed write
// would leave the chunk sitting in the server's buffer.
func TestSSEPassthroughFlushesEveryChunk(t *testing.T) {
	// Buffered so the recorder's onWrite never blocks even if the upstream
	// has already given up; capacity covers one signal per chunk plus slack
	// for write splitting.
	written := make(chan struct{}, len(sseTestChunks)+4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for i, chunk := range sseTestChunks {
			if _, err := io.WriteString(w, chunk); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			if i < len(sseTestChunks)-1 {
				select {
				case <-written:
				case <-time.After(sseAckTimeout):
					t.Errorf("upstream: proxy never wrote chunk %d back within %v", i, sseAckTimeout)
					return
				}
			}
		}
	}))
	defer upstream.Close()

	rec := &flushRecordingWriter{
		header:  make(http.Header),
		onWrite: func() { written <- struct{}{} },
	}
	handler := newSSETestHandler(t, upstream.URL, 1, nil)
	handler.ServeHTTP(rec, CreateStreamingMessagesRequest())

	if rec.status != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.status, http.StatusOK)
	}
	if ct := rec.header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if got, want := rec.body.String(), strings.Join(sseTestChunks, ""); got != want {
		t.Errorf("relayed payload = %q, want %q", got, want)
	}
	if rec.writes < len(sseTestChunks) {
		t.Errorf("chunk writes = %d, want at least one Write per chunk (%d)", rec.writes, len(sseTestChunks))
	}
	if rec.flushes != rec.writes {
		t.Errorf("flushes = %d, writes = %d -- every chunk write must be flushed before the relay continues", rec.flushes, rec.writes)
	}
}

// TestSSEPassthroughClientDisconnectCancelsUpstream proves disconnect
// propagation end to end. The proxy builds its upstream request with the
// inbound request's context (handler.go, http.NewRequestWithContext), so a
// client that walks away mid-stream must cancel the upstream request instead
// of leaving it consuming an upstream slot. A raw TCP connection is used so
// the disconnect is an actual connection teardown rather than whatever the
// http.Client's body-close policy decides to do.
func TestSSEPassthroughClientDisconnectCancelsUpstream(t *testing.T) {
	canceled := make(chan struct{})
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, sseTestChunks[0])
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		// Hold the stream open until the client goes away.
		select {
		case <-r.Context().Done():
			close(canceled)
		case <-time.After(15 * time.Second):
			t.Errorf("upstream: request context not canceled within 15s of client disconnect")
		}
	}))
	defer upstream.Close()

	handler := newSSETestHandler(t, upstream.URL, 3, nil)
	proxy := httptest.NewServer(handler)
	defer proxy.Close()

	conn, err := net.Dial("tcp", proxy.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dialing proxy: %v", err)
	}
	body := CreateStreamingRequestBody()
	request := fmt.Sprintf("POST /v1/messages HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s",
		proxy.Listener.Addr(), len(body), body)
	if _, err := conn.Write([]byte(request)); err != nil {
		t.Fatalf("writing request: %v", err)
	}

	// Read until the first SSE event has crossed the wire. The chunked
	// framing surrounds but never splits the event text itself.
	acc := make([]byte, 0, 4096)
	buf := make([]byte, 512)
	for !bytes.Contains(acc, []byte("message_start")) {
		if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			t.Fatalf("setting read deadline: %v", err)
		}
		n, err := conn.Read(buf)
		if n > 0 {
			acc = append(acc, buf[:n]...)
		}
		if err != nil {
			t.Fatalf("reading first SSE event from proxy: %v (received so far: %q)", err, acc)
		}
	}

	// The client goes away mid-stream.
	if err := conn.Close(); err != nil {
		t.Fatalf("closing client connection: %v", err)
	}

	select {
	case <-canceled:
	case <-time.After(15 * time.Second):
		t.Fatal("client disconnect did not propagate: upstream request context was never canceled")
	}
	if calls.Load() != 1 {
		t.Errorf("upstream calls = %d, want 1 -- a client disconnect must not trigger a retry", calls.Load())
	}
}

// TestSSEPassthroughNoRetryAfterFirstFlushedChunk proves the retry
// interaction: retries exist in the handler only before the first bytes are
// committed to the client, so once a chunk has been flushed, a mid-stream
// upstream failure must terminate the response rather than retry or re-stream
// it. The upstream aborts (connection torn down without a terminal chunk)
// only after the client has demonstrably received the first event, with
// MAX_RETRIES=3 available to make any wrongful retry observable.
func TestSSEPassthroughNoRetryAfterFirstFlushedChunk(t *testing.T) {
	var calls atomic.Int32
	acked := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, sseTestChunks[0])
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-acked // wait until the client has the first event in hand
		// Upstream dies mid-stream: connection torn down, no further events,
		// no terminal chunk.
		panic(http.ErrAbortHandler)
	}))
	defer upstream.Close()

	handler := newSSETestHandler(t, upstream.URL, 3, nil)
	proxy := httptest.NewServer(handler)
	defer proxy.Close()

	retryBefore := testutil.ToFloat64(retryAttempts.WithLabelValues("retry", "test"))
	readErrBefore := testutil.ToFloat64(upstreamErrors.WithLabelValues("read_error", "test"))

	resp, err := http.Post(proxy.URL+"/v1/messages", "application/json", strings.NewReader(CreateStreamingRequestBody()))
	if err != nil {
		t.Fatalf("POST through proxy: %v", err)
	}
	defer resp.Body.Close()

	data, _, err := collectSSEEvents(resp.Body, 1, []chan struct{}{acked})
	if err != nil {
		t.Fatalf("first chunk never reached the client: %v", err)
	}
	// The stream must terminate (bound the wait; a hang would mean the abort
	// was never relayed). A mid-stream abort surfaces to the client as a
	// stream that simply ends early: net/http terminates the proxy's chunked
	// response cleanly when the handler returns, so the client cannot rely on
	// a transport error and must detect truncation at the SSE protocol level
	// (a missing message_stop). What must never happen is a second copy of
	// the first event or a new upstream attempt -- both asserted below.
	restCh := make(chan string, 1)
	go func() {
		rest, _ := io.ReadAll(resp.Body)
		restCh <- string(rest)
	}()
	var rest string
	select {
	case rest = <-restCh:
	case <-time.After(15 * time.Second):
		t.Fatal("stream never terminated after upstream abort")
	}
	if got, want := string(data)+rest, sseTestChunks[0]; got != want {
		t.Errorf("client received %q, want exactly the first event once -- no re-streaming: %q", got, want)
	}
	if calls.Load() != 1 {
		t.Errorf("upstream calls = %d, want 1 -- a mid-stream abort must not be retried even with MAX_RETRIES=3", calls.Load())
	}
	if got := testutil.ToFloat64(retryAttempts.WithLabelValues("retry", "test")); got != retryBefore {
		t.Errorf("retry attempts metric = %v, want unchanged %v", got, retryBefore)
	}
	if got := testutil.ToFloat64(upstreamErrors.WithLabelValues("read_error", "test")); got != readErrBefore+1 {
		t.Errorf("upstream read_error metric = %v, want %v -- the abort should be classified as a read error, not a retry", got, readErrBefore+1)
	}
}
