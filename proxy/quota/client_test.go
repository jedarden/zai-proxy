package quota

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testAPIKey is deliberately distinctive so leak assertions can grep for it.
const testAPIKey = "sk-quota-secret-9f2b7c" // gitleaks:allow - fake fixture key, never a real credential

func newTestClient(t *testing.T, serverURL string, opts ...Option) *Client {
	t.Helper()
	opts = append([]Option{WithNow(func() time.Time { return testNow })}, opts...)
	client, err := NewClient(testAPIKey, serverURL, opts...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func TestNewClientRequiresAPIKey(t *testing.T) {
	if _, err := NewClient("", DefaultBaseURL); err == nil {
		t.Fatal("NewClient with an empty API key should fail")
	}
}

func TestNewClientBuildsEndpoint(t *testing.T) {
	tests := []struct {
		name         string
		baseURL      string
		wantEndpoint string
	}{
		{
			name:         "plain origin",
			baseURL:      "https://api.z.ai",
			wantEndpoint: "https://api.z.ai" + QuotaLimitPath,
		},
		{
			name:         "trailing slash is trimmed",
			baseURL:      "https://api.z.ai/",
			wantEndpoint: "https://api.z.ai" + QuotaLimitPath,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewClient("key", tc.baseURL)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			if client.endpoint != tc.wantEndpoint {
				t.Errorf("endpoint = %q, want %q", client.endpoint, tc.wantEndpoint)
			}
		})
	}
}

func TestFetchSuccess(t *testing.T) {
	var gotAuth, gotAccept, gotPath, gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotPath = r.URL.Path
		gotMethod = r.Method
		_, _ = w.Write(loadFixture(t, "current_credit_limit.json"))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	snapshot, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// Z.AI's usage-query contract uses the raw key without a Bearer prefix.
	if gotAuth != testAPIKey {
		t.Errorf("Authorization = %q, want the raw key %q", gotAuth, testAPIKey)
	}
	if strings.HasPrefix(gotAuth, "Bearer ") {
		t.Error("Authorization must not carry a Bearer prefix")
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
	if gotPath != QuotaLimitPath {
		t.Errorf("request path = %q, want %q", gotPath, QuotaLimitPath)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("request method = %q, want GET", gotMethod)
	}

	if snapshot.FetchedAt != testNow {
		t.Errorf("fetched at = %v, want %v", snapshot.FetchedAt, testNow)
	}
	if len(snapshot.Windows) != 2 {
		t.Fatalf("windows = %d, want 2", len(snapshot.Windows))
	}
	if _, ok := snapshot.Window(WindowWeekly); !ok {
		t.Error("weekly window missing from snapshot")
	}
}

func TestFetchUnexpectedStatus(t *testing.T) {
	const bodyMarker = "upstream-body acct:must-not-leak"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, bodyMarker, http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Fetch(context.Background())
	if err == nil {
		t.Fatal("Fetch against a 503 should fail")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error %q should carry the status code", err.Error())
	}
	if strings.Contains(err.Error(), bodyMarker) {
		t.Errorf("error %q leaks the upstream body", err.Error())
	}
}

func TestFetchOversizeResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), 4096))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, WithMaxResponseBytes(1024))
	_, err := client.Fetch(context.Background())
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("Fetch error = %v, want ErrResponseTooLarge", err)
	}
}

// TestFetchDoesNotFollowRedirects pins the credential-egress guard: Go
// re-attaches Authorization on a same-host redirect, so an unguarded client
// would replay the key to the redirect target. The quota endpoint must never
// be followed, and the error may only carry the redirect status.
func TestFetchDoesNotFollowRedirects(t *testing.T) {
	var targetRequests int32
	var targetAuth string
	mux := http.NewServeMux()
	mux.HandleFunc(QuotaLimitPath, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, QuotaLimitPath+"/elsewhere", http.StatusFound)
	})
	mux.HandleFunc(QuotaLimitPath+"/elsewhere", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&targetRequests, 1)
		targetAuth = r.Header.Get("Authorization")
		_, _ = w.Write(loadFixture(t, "current_credit_limit.json"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Fetch(context.Background())
	if err == nil {
		t.Fatal("Fetch against a redirect should fail")
	}
	if !strings.Contains(err.Error(), "302") {
		t.Errorf("error %q should carry the redirect status", err.Error())
	}
	if atomic.LoadInt32(&targetRequests) != 0 {
		t.Error("Fetch followed the redirect target")
	}
	if targetAuth != "" {
		t.Errorf("the credential reached the redirect target: %q", targetAuth)
	}
}

func TestFetchRejectsProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(loadFixture(t, "provider_error.json"))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Fetch(context.Background())
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("Fetch error = %v, want *ProviderError", err)
	}
	if providerErr.Code != 401 {
		t.Errorf("provider code = %d, want 401", providerErr.Code)
	}
}

func TestFetchTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, WithTimeout(50*time.Millisecond))

	fetchStart := time.Now()
	_, err := client.Fetch(context.Background())
	elapsed := time.Since(fetchStart)
	if err == nil {
		t.Fatal("Fetch against a stalled server should fail")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want a context deadline failure", err)
	}
	if elapsed > 250*time.Millisecond {
		t.Errorf("Fetch returned after %v; the short timeout was not honored", elapsed)
	}
}

func TestFetchContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	if _, err := client.Fetch(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want a context cancellation failure", err)
	}
}

// TestFetchNeverLogsSecrets drives every failure mode with a distinctive
// API key and an account marker in the provider payload, and asserts none
// of them reach the log or the error text.
func TestFetchNeverLogsSecrets(t *testing.T) {
	scenarios := []struct {
		name   string
		server func(t *testing.T) *httptest.Server
	}{
		{
			name: "unexpected status",
			server: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Error(w, "boom acct:must-not-leak", http.StatusInternalServerError)
				}))
			},
		},
		{
			name: "provider business error",
			server: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write(loadFixture(t, "provider_error.json"))
				}))
			},
		},
		{
			name: "malformed body",
			server: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write(loadFixture(t, "malformed_truncated.json"))
				}))
			},
		},
		{
			name: "oversize body",
			server: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write(bytes.Repeat([]byte("x"), 4096))
				}))
			},
		},
		{
			name: "stalled server",
			server: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					time.Sleep(500 * time.Millisecond)
				}))
			},
		},
	}

	for _, tc := range scenarios {
		t.Run(tc.name, func(t *testing.T) {
			var logBuf bytes.Buffer
			server := tc.server(t)
			defer server.Close()

			client := newTestClient(t, server.URL,
				WithTimeout(50*time.Millisecond),
				WithMaxResponseBytes(1024),
				WithLogger(log.New(&logBuf, "", 0)),
			)
			_, err := client.Fetch(context.Background())
			if err == nil {
				t.Fatal("Fetch should fail in every scenario")
			}

			for _, leaked := range []string{testAPIKey, "acct:must-not-leak", "acct:marker-7f3a9"} {
				if strings.Contains(logBuf.String(), leaked) {
					t.Errorf("log leaks %q:\n%s", leaked, logBuf.String())
				}
				if strings.Contains(err.Error(), leaked) {
					t.Errorf("error text leaks %q: %q", leaked, err.Error())
				}
			}
		})
	}
}

func TestFetchLogsOnlySanitizedFacts(t *testing.T) {
	var logBuf bytes.Buffer
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(loadFixture(t, "current_credit_limit.json"))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, WithLogger(log.New(&logBuf, "", 0)))
	if _, err := client.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	logged := logBuf.String()
	if !strings.Contains(logged, "windows=2") {
		t.Errorf("success log should carry the window count, got %q", logged)
	}
	if strings.Contains(logged, testAPIKey) || strings.Contains(logged, "Authorization") {
		t.Errorf("success log carries credential material: %q", logged)
	}
}

func TestNewClientFromEnv(t *testing.T) {
	t.Setenv("ZAI_API_KEY", testAPIKey)
	t.Setenv("ZAI_QUOTA_BASE_URL", "https://quota.example.com/")
	t.Setenv("QUOTA_POLL_TIMEOUT", "750ms")
	t.Setenv("QUOTA_MAX_RESPONSE_BYTES", "2048")

	client, err := NewClientFromEnv()
	if err != nil {
		t.Fatalf("NewClientFromEnv: %v", err)
	}
	if client.endpoint != "https://quota.example.com"+QuotaLimitPath {
		t.Errorf("endpoint = %q, want the env-configured origin + path", client.endpoint)
	}
	if client.timeout != 750*time.Millisecond {
		t.Errorf("timeout = %v, want 750ms", client.timeout)
	}
	if client.maxBytes != 2048 {
		t.Errorf("max bytes = %d, want 2048", client.maxBytes)
	}
}

func TestNewClientFromEnvRequiresAPIKey(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "")
	if _, err := NewClientFromEnv(); err == nil {
		t.Fatal("NewClientFromEnv without ZAI_API_KEY should fail")
	}
}

func TestReadBounded(t *testing.T) {
	tests := []struct {
		name    string
		body    []byte
		max     int64
		wantErr bool
	}{
		{name: "within budget", body: []byte("{}"), max: 10},
		{name: "exactly at budget", body: bytes.Repeat([]byte("a"), 10), max: 10},
		{name: "one byte over budget", body: bytes.Repeat([]byte("a"), 11), max: 10, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readBounded(bytes.NewReader(tc.body), tc.max)
			if tc.wantErr {
				if !errors.Is(err, ErrResponseTooLarge) {
					t.Fatalf("error = %v, want ErrResponseTooLarge", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("readBounded: %v", err)
			}
			if !bytes.Equal(got, tc.body) {
				t.Errorf("read %d bytes, want %d", len(got), len(tc.body))
			}
		})
	}
}
