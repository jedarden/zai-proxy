package quota

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"git.ardenone.com/jedarden/zai-proxy/internal/configenv"
)

// Defaults for the quota polling client. The poll is out of band and must
// never compete with model traffic, so both the timeout and the response
// budget are tight: real payloads are well under a kilobyte.
const (
	// DefaultBaseURL is the Z.AI origin that hosts the quota endpoint.
	DefaultBaseURL = "https://api.z.ai"

	// QuotaLimitPath is the account quota endpoint path.
	QuotaLimitPath = "/api/monitor/usage/quota/limit"

	// DefaultTimeout bounds one quota poll.
	DefaultTimeout = 5 * time.Second

	// DefaultMaxResponseBytes bounds how much of a response body is read.
	DefaultMaxResponseBytes = 64 << 10
)

// ErrResponseTooLarge is returned when the endpoint responds with more
// bytes than the client is willing to read.
var ErrResponseTooLarge = errors.New("quota response exceeds the read budget")

// Environment variables honored by NewClientFromEnv.
const (
	// EnvBaseURL overrides the quota endpoint origin.
	EnvBaseURL = "ZAI_QUOTA_BASE_URL"
	// EnvTimeout bounds one quota poll (Go duration string).
	EnvTimeout = "QUOTA_POLL_TIMEOUT"
	// EnvMaxResponseBytes bounds the response read budget in bytes.
	EnvMaxResponseBytes = "QUOTA_MAX_RESPONSE_BYTES"
)

// Logger is the minimal logging surface the client uses. Callers wiring
// slog, zap, or the standard library can adapt to it in one line.
type Logger interface {
	Printf(format string, args ...any)
}

// Option configures optional Client behavior.
type Option func(*Client)

// WithHTTPClient replaces the underlying HTTP client. Its Timeout, if any,
// is left untouched; the quota timeout is applied per request.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.http = hc }
}

// WithTimeout bounds one quota poll.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.timeout = d
		}
	}
}

// WithMaxResponseBytes bounds how much of a response body is read before
// the poll fails with ErrResponseTooLarge.
func WithMaxResponseBytes(n int64) Option {
	return func(c *Client) {
		if n > 0 {
			c.maxBytes = n
		}
	}
}

// WithLogger attaches a logger. The client logs only non-secret facts:
// status codes, error classes, and window counts. It never logs the API
// key, request headers, response bodies, or account identifiers.
func WithLogger(l Logger) Option {
	return func(c *Client) { c.logger = l }
}

// WithNow overrides the clock used to stamp snapshots, for tests.
func WithNow(now func() time.Time) Option {
	return func(c *Client) {
		if now != nil {
			c.now = now
		}
	}
}

// Client polls Z.AI's account quota endpoint with the proxy-held
// credential. It is safe for concurrent use. A Client failure must never
// block the model data path: callers should poll on a slow background
// cadence and keep their last-known-good snapshot.
type Client struct {
	apiKey   string
	endpoint string
	timeout  time.Duration
	maxBytes int64
	logger   Logger
	now      func() time.Time
	http     *http.Client
}

// NewClient returns a Client that polls baseURL + QuotaLimitPath using the
// given API key. The key is sent as the raw Authorization value, without a
// Bearer prefix, matching Z.AI's official usage-query tooling.
func NewClient(apiKey, baseURL string, opts ...Option) (*Client, error) {
	if apiKey == "" {
		return nil, errors.New("quota client requires an API key")
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	c := &Client{
		apiKey:   apiKey,
		endpoint: strings.TrimSuffix(baseURL, "/") + QuotaLimitPath,
		timeout:  DefaultTimeout,
		maxBytes: DefaultMaxResponseBytes,
		now:      time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.http == nil {
		c.http = &http.Client{
			// A monitor endpoint has no legitimate reason to redirect, and
			// Go re-attaches the Authorization header on same-host and
			// subdomain redirects. Refusing to follow keeps the credential
			// on the configured origin; handing back the 3xx response lets
			// Fetch report it as an unexpected status instead.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return c, nil
}

// NewClientFromEnv builds a Client from the proxy-held ZAI_API_KEY plus
// optional tuning variables (EnvBaseURL, EnvTimeout, EnvMaxResponseBytes).
func NewClientFromEnv(opts ...Option) (*Client, error) {
	apiKey := configenv.GetString("ZAI_API_KEY", "")
	if apiKey == "" {
		return nil, errors.New("ZAI_API_KEY environment variable required")
	}
	opts = append([]Option{
		WithTimeout(configenv.ParseDurationOrDefault(EnvTimeout, DefaultTimeout)),
		WithMaxResponseBytes(configenv.GetInt64(EnvMaxResponseBytes, DefaultMaxResponseBytes)),
	}, opts...)
	return NewClient(apiKey, configenv.GetString(EnvBaseURL, DefaultBaseURL), opts...)
}

// Fetch performs one quota poll and returns the normalized snapshot.
// Errors carry status codes or error classes only; they never embed the
// credential, the request, or the response body.
func (c *Client) Fetch(ctx context.Context) (Snapshot, error) {
	pollCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(pollCtx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return Snapshot{}, fmt.Errorf("quota poll request could not be built: %w", err)
	}
	// Z.AI expects the raw key in Authorization; a Bearer prefix is not
	// part of its usage-query contract.
	req.Header.Set("Authorization", c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := c.http.Do(req)
	if err != nil {
		c.logf("quota poll failed: %v", err)
		return Snapshot{}, fmt.Errorf("quota poll failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Drain within the read budget so the connection can be reused,
		// then discard: the body of a failed monitor call is not part of
		// any error we emit.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, c.maxBytes))
		c.logf("quota poll failed: unexpected status %d", resp.StatusCode)
		return Snapshot{}, fmt.Errorf("quota poll returned unexpected status %d", resp.StatusCode)
	}

	body, err := readBounded(resp.Body, c.maxBytes)
	if err != nil {
		c.logf("quota poll failed: %v", err)
		return Snapshot{}, err
	}

	snapshot, err := Normalize(body, c.now())
	if err != nil {
		var providerErr *ProviderError
		if errors.As(err, &providerErr) {
			c.logf("quota poll rejected: provider code %d", providerErr.Code)
		} else {
			c.logf("quota poll failed: %v", err)
		}
		return Snapshot{}, err
	}

	c.logf("quota poll ok: windows=%d", len(snapshot.Windows))
	return snapshot, nil
}

func (c *Client) logf(format string, args ...any) {
	if c.logger != nil {
		c.logger.Printf(format, args...)
	}
}

// readBounded reads at most maxBytes from r, failing with
// ErrResponseTooLarge when the source is larger.
func readBounded(r io.Reader, maxBytes int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("quota response could not be read: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("%w (limit %d bytes)", ErrResponseTooLarge, maxBytes)
	}
	return body, nil
}
