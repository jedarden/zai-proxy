package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"git.ardenone.com/jedarden/zai-proxy/proxy/config"
	"git.ardenone.com/jedarden/zai-proxy/proxy/quota"
)

// Quota poll outcomes, mirroring the documented
// zai_proxy_quota_poll_total result enum in metrics.go.
const (
	quotaPollOutcomeSuccess   = "success"
	quotaPollOutcomeError     = "error"
	quotaPollOutcomeMalformed = "malformed"
	quotaPollOutcomeStale     = "stale"
)

// quotaMissingLimitsPrefix is the message the quota package uses for a
// well-formed JSON body with no data.limits array. Parse failures carry no
// sentinel or exported type, so the outcome classifier matches this one
// documented prefix; the JSON decoder failures are matched structurally.
const quotaMissingLimitsPrefix = "quota response is missing its data.limits array"

// newQuotaPollerFromConfig builds the observe-only poller from the
// deployment's quota configuration: the proxy-held credential polls the
// configured origin on the configured cadence. The credential reaches only
// the client — never the returned value, an error, or a log line.
func newQuotaPollerFromConfig(apiKey, variant string) (*QuotaPoller, error) {
	client, err := quota.NewClient(
		apiKey,
		config.GetQuotaBaseURL(),
		quota.WithTimeout(config.GetQuotaPollTimeout()),
		// The standard logger satisfies quota.Logger directly, and the client
		// logs non-secret facts only.
		quota.WithLogger(log.Default()),
	)
	if err != nil {
		return nil, err
	}
	return NewQuotaPoller(client, config.GetQuotaPollInterval(), config.GetQuotaStaleAfter(), variant)
}

// pollFetcher is the quota source the poller refreshes from. *quota.Client
// implements it, and the indirection keeps the poller testable without an
// endpoint. Fetch errors carry status codes and error classes only; they
// never embed the credential, the request, or the response body.
type pollFetcher interface {
	Fetch(ctx context.Context) (quota.Snapshot, error)
}

// QuotaPoller polls the account quota endpoint out of band and retains the
// last-known-good normalized snapshot for /health and /metrics.
//
// It is deliberately observe-only (docs/plan/plan.md, "Quota-aware throttling
// plan"): nothing here feeds request admission, so a failed, malformed,
// timed-out, or stale poll changes what operators see and nothing else. The
// poller runs on its own goroutine and never blocks the request path.
type QuotaPoller struct {
	fetcher    pollFetcher
	interval   time.Duration
	staleAfter time.Duration
	variant    string
	now        func() time.Time

	mu          sync.Mutex
	last        *quota.Snapshot // last-known-good; nil until the first success
	lastSuccess time.Time       // zero until the first success
	lastOutcome string
	staleSeen   bool // the current staleness has already been counted
}

// NewQuotaPoller returns a poller that refreshes from fetcher every interval
// and reports its sample stale after staleAfter. Both durations must be
// positive; they bound the poll loop and the sample's trusted lifetime.
func NewQuotaPoller(fetcher pollFetcher, interval, staleAfter time.Duration, variant string) (*QuotaPoller, error) {
	if fetcher == nil {
		return nil, errors.New("quota poller requires a fetch source")
	}
	if interval <= 0 {
		return nil, errors.New("quota poll interval must be positive")
	}
	if staleAfter <= 0 {
		return nil, errors.New("quota stale-after must be positive")
	}
	return &QuotaPoller{
		fetcher:    fetcher,
		interval:   interval,
		staleAfter: staleAfter,
		variant:    variant,
		now:        time.Now,
	}, nil
}

// Run polls immediately — so /health and /metrics populate without waiting
// an interval — and then once per tick until ctx is cancelled. It returns
// ctx.Err(), and is the poller's whole lifecycle: stopping it is a cancel,
// never a request-path concern.
func (p *QuotaPoller) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		// Cancelled before the first poll: report it rather than counting a
		// poll the context had already refused.
		return err
	}

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	p.pollOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			log.Printf("Quota polling stopped: %v", ctx.Err())
			return ctx.Err()
		case <-ticker.C:
			p.pollOnce(ctx)
		}
	}
}

// pollOnce performs one refresh. A failure keeps the last-known-good sample
// and counts the outcome; only a success replaces the retained state.
func (p *QuotaPoller) pollOnce(ctx context.Context) {
	snapshot, err := p.fetcher.Fetch(ctx)

	p.mu.Lock()
	now := p.now()
	outcome := quotaPollOutcomeSuccess
	if err != nil {
		outcome = classifyQuotaPollError(err)
	} else {
		p.last = &snapshot
		p.lastSuccess = now
		p.staleSeen = false
	}
	p.lastOutcome = outcome
	fresh := p.freshLocked(now)
	sampleAge := p.ageLocked(now)
	hasSample := p.last != nil
	staleTransition := !fresh && !p.staleSeen
	if staleTransition {
		p.staleSeen = true
	}
	p.mu.Unlock()

	RecordQuotaPollOutcome(outcome, p.variant)
	if staleTransition {
		// Counted on the transition into staleness only, so a long outage
		// accumulates one stale sample rather than one per retry. A poller
		// that has never succeeded is stale from its first poll: "stale"
		// means no trusted sample, not merely an old one.
		RecordQuotaPollOutcome(quotaPollOutcomeStale, p.variant)
	}
	if err == nil {
		recordQuotaSnapshot(p.variant, snapshot)
	}
	// The age gauge describes the last valid sample, so it stays absent until
	// there has been one; exporting 0 before then would read as a fresh
	// sample that does not exist.
	if hasSample {
		RecordQuotaSampleAge(p.variant, sampleAge)
	}
}

// recordQuotaSnapshot publishes one normalized snapshot to the observe-only
// quota gauges. No admission-rate or gate metric is touched: those describe
// enforcement, which is a separately gated change.
func recordQuotaSnapshot(variant string, snapshot quota.Snapshot) {
	for _, window := range snapshot.Windows {
		name := window.Window.String()
		limitType := string(window.LimitType)
		RecordQuotaUsageRatio(name, limitType, variant, window.UsedFraction)
		RecordQuotaRemainingRatio(name, limitType, variant, 1-window.UsedFraction)
		RecordQuotaResetTime(name, limitType, variant, window.ResetTime)
	}
}

// classifyQuotaPollError maps a failed poll onto the bounded outcome enum.
// A structured provider rejection, a refused read budget, or a transport
// failure counts as "error"; a payload the endpoint did send but that this
// package cannot parse counts as "malformed".
func classifyQuotaPollError(err error) string {
	if err == nil {
		return quotaPollOutcomeSuccess
	}

	var providerErr *quota.ProviderError
	if errors.As(err, &providerErr) {
		return quotaPollOutcomeError
	}
	if errors.Is(err, quota.ErrResponseTooLarge) {
		return quotaPollOutcomeError
	}

	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &syntaxErr) || errors.As(err, &typeErr) ||
		strings.HasPrefix(err.Error(), quotaMissingLimitsPrefix) {
		return quotaPollOutcomeMalformed
	}
	return quotaPollOutcomeError
}

// QuotaWindowHealth is one normalized quota window as /health reports it.
// The values are the provider's own observation, not a local decision.
type QuotaWindowHealth struct {
	Window       string  `json:"window"`
	LimitType    string  `json:"limit_type"`
	UsedFraction float64 `json:"used_fraction"`
	ResetAt      string  `json:"reset_at,omitempty"`
}

// QuotaHealth is the quota portion of the /health payload. It reports the
// poller's own configuration alongside the retained sample, so an operator
// can tell a disabled poller from one that has never succeeded from one
// whose sample has gone stale.
type QuotaHealth struct {
	Enabled          bool                `json:"enabled"`
	Fresh            bool                `json:"fresh"`
	Interval         string              `json:"interval,omitempty"`
	StaleAfter       string              `json:"stale_after,omitempty"`
	LastSuccessAt    string              `json:"last_success_at,omitempty"`
	SampleAgeSeconds float64             `json:"sample_age_seconds,omitempty"`
	LastOutcome      string              `json:"last_outcome,omitempty"`
	PlanTier         string              `json:"plan_tier,omitempty"`
	Windows          []QuotaWindowHealth `json:"windows,omitempty"`
}

// HealthState returns the quota state exposed on /health. It always reports
// Enabled true; the disabled case is rendered by quotaHealthSection.
func (p *QuotaPoller) HealthState() QuotaHealth {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := p.now()
	health := QuotaHealth{
		Enabled:     true,
		Fresh:       p.freshLocked(now),
		Interval:    p.interval.String(),
		StaleAfter:  p.staleAfter.String(),
		LastOutcome: p.lastOutcome,
	}
	if p.last == nil {
		return health
	}

	health.SampleAgeSeconds = p.ageLocked(now).Seconds()
	if !p.lastSuccess.IsZero() {
		health.LastSuccessAt = p.lastSuccess.UTC().Format(time.RFC3339)
	}
	health.PlanTier = p.last.PlanTier
	for _, window := range p.last.Windows {
		state := QuotaWindowHealth{
			Window:       window.Window.String(),
			LimitType:    string(window.LimitType),
			UsedFraction: window.UsedFraction,
		}
		if !window.ResetTime.IsZero() {
			state.ResetAt = window.ResetTime.UTC().Format(time.RFC3339)
		}
		health.Windows = append(health.Windows, state)
	}
	return health
}

// quotaHealthSection renders the /health quota section. A poller that was
// never constructed — quota polling disabled — still reports itself, so the
// flag taking effect is visible in the payload rather than only in the log.
func quotaHealthSection(poller *QuotaPoller) QuotaHealth {
	if poller == nil {
		return QuotaHealth{}
	}
	return poller.HealthState()
}

// freshLocked reports whether the retained sample is still trusted. A nil
// sample — no successful poll yet — is never fresh.
// The caller must hold p.mu.
func (p *QuotaPoller) freshLocked(now time.Time) bool {
	if p.last == nil {
		return false
	}
	return now.Sub(p.lastSuccess) <= p.staleAfter
}

// ageLocked returns the age of the retained sample, clamped to zero so clock
// skew between the poll that stamped it and this read cannot export a
// negative age. The caller must hold p.mu.
func (p *QuotaPoller) ageLocked(now time.Time) time.Duration {
	if p.last == nil {
		return 0
	}
	if age := now.Sub(p.lastSuccess); age > 0 {
		return age
	}
	return 0
}
