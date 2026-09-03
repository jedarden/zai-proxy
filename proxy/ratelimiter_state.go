package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/time/rate"
)

// RateLimitState is the persisted snapshot of the inferred upstream ceiling.
// The ceiling is deliberately unknown and learned from the observed 429 rate
// (see docs/plan/plan.md, "Ceiling persistence across restarts"), so this
// snapshot is the proxy's most valuable state: without it every container
// restart re-learns the ceiling from RATE_LIMIT_MAX and burns a burst of 429s.
type RateLimitState struct {
	Ceiling float64   `json:"ceiling"` // estimated upstream ceiling in req/s
	Hold    float64   `json:"hold"`    // rate the limiter was holding at, in req/s
	Ts      time.Time `json:"ts"`      // when the ceiling was last learned
}

// clampRate confines a rate to [lo, hi].
func clampRate(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// saveRateLimitState atomically writes s to path: a temp file in the same
// directory followed by a rename, so a reader never sees a half-written
// snapshot even if the process dies mid-write.
func saveRateLimitState(path string, s *RateLimitState) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// No-op once the rename below succeeds.
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// loadRateLimitState reads a persisted ceiling snapshot from path, returning:
//
//	(state, nil) — a usable snapshot no older than maxAge
//	(nil, nil)   — no state file exists yet, which is normal before the first
//	               ceiling update has ever been written
//	(nil, err)   — a file is present but unusable: unreadable, corrupt,
//	               nonsensical, or older than maxAge
//
// The age is judged from the recorded ts, falling back to the file's
// modification time when ts is missing.
func loadRateLimitState(path string, maxAge time.Duration) (*RateLimitState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var s RateLimitState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("corrupt state: %w", err)
	}
	if s.Ceiling <= 0 || s.Hold <= 0 {
		return nil, fmt.Errorf("nonsensical state: ceiling=%v hold=%v", s.Ceiling, s.Hold)
	}

	written := s.Ts
	if written.IsZero() {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		written = info.ModTime()
	}

	if age := time.Since(written); age > maxAge {
		return nil, fmt.Errorf("state is %s old, beyond max age %s", age.Round(time.Second), maxAge)
	}
	return &s, nil
}

// stateSnapshotLocked captures the ceiling state for persistence. The caller
// must hold arl.mu.
func (arl *AdaptiveRateLimiter) stateSnapshotLocked() *RateLimitState {
	return &RateLimitState{
		Ceiling: arl.estimatedCeiling,
		Hold:    arl.currentRate,
		Ts:      arl.lastCeilingUpdate,
	}
}

// holdTargetLocked returns the rate the limiter should hold at given the
// current ceiling estimate. The caller must hold arl.mu (or arl.mu.RLock()).
func (arl *AdaptiveRateLimiter) holdTargetLocked() float64 {
	return clampRate(arl.estimatedCeiling*(1-arl.holdMargin), arl.minRate, arl.maxRate)
}

// persistState writes the ceiling snapshot to the state file. A failure is
// logged and otherwise ignored: the only cost of a missing or stale snapshot
// is re-learning the ceiling after the next restart.
func (arl *AdaptiveRateLimiter) persistState(s *RateLimitState) {
	if err := saveRateLimitState(arl.statePath, s); err != nil {
		log.Printf("Rate limit: failed to persist ceiling state to %s: %v", arl.statePath, err)
		return
	}
	log.Printf("Rate limit: persisted ceiling %.2f req/s (hold %.2f) to %s",
		s.Ceiling, s.Hold, arl.statePath)
}

// RestoreCeiling seeds the limiter from a persisted snapshot so a restart
// resumes at the learned ceiling instead of re-learning it from RATE_LIMIT_MAX.
// Values are clamped into the configured range. The probe loop is deliberately
// left unchanged, so if upstream has loosened since the snapshot was written
// the estimate still drifts upward.
func (arl *AdaptiveRateLimiter) RestoreCeiling(s *RateLimitState) {
	arl.mu.Lock()
	defer arl.mu.Unlock()

	ceiling := clampRate(s.Ceiling, arl.minRate, arl.maxRate)
	hold := clampRate(s.Hold, arl.minRate, ceiling)

	arl.estimatedCeiling = ceiling
	arl.currentRate = hold
	arl.limiter.SetLimit(rate.Limit(hold))
	arl.limiter.SetBurst(limiterBurst(hold))
	arl.cleanWindows = 0
	arl.lastAdjustment = time.Now()
	arl.lastCeilingUpdate = s.Ts
	arl.restoredFromState = true
	arl.notifyRateChangeLocked()

	learnedAt := "an unknown time"
	if !s.Ts.IsZero() {
		learnedAt = s.Ts.UTC().Format(time.RFC3339)
	}
	log.Printf("Rate limit: resumed at persisted ceiling %.2f req/s, holding at %.2f req/s (learned %s)",
		ceiling, hold, learnedAt)
	rateLimitCurrentRate.WithLabelValues(deploymentVariant).Set(hold)
}

// RestoreFromStateFile applies the ceiling snapshot at path when one exists
// and is fresh enough, leaving the limiter at its RATE_LIMIT_MAX assumption
// otherwise. It reports whether a snapshot was applied.
func (arl *AdaptiveRateLimiter) RestoreFromStateFile(path string, maxAge time.Duration) bool {
	state, err := loadRateLimitState(path, maxAge)
	if err != nil {
		log.Printf("Rate limit: ignoring persisted ceiling at %s: %v", path, err)
		return false
	}
	if state == nil {
		log.Printf("Rate limit: no persisted ceiling at %s; starting from RATE_LIMIT_MAX", path)
		return false
	}
	arl.RestoreCeiling(state)
	return true
}

// RateLimitHealth is the rate-limit portion of the /health payload. Ceiling,
// hold and ceiling_updated_at mirror the persisted snapshot; hold is computed
// live from the current estimate, so it diverges from current_rate only while
// the limiter is probing or converging.
type RateLimitHealth struct {
	CurrentRate       float64 `json:"current_rate"`
	Ceiling           float64 `json:"ceiling"`
	Hold              float64 `json:"hold"`
	CeilingUpdatedAt  string  `json:"ceiling_updated_at,omitempty"`
	StateFile         string  `json:"state_file,omitempty"`
	RestoredFromState bool    `json:"restored_from_state"`
}

// HealthState returns the rate-limit state exposed on /health.
func (arl *AdaptiveRateLimiter) HealthState() RateLimitHealth {
	arl.mu.RLock()
	defer arl.mu.RUnlock()

	health := RateLimitHealth{
		CurrentRate:       arl.currentRate,
		Ceiling:           arl.estimatedCeiling,
		Hold:              arl.holdTargetLocked(),
		StateFile:         arl.statePath,
		RestoredFromState: arl.restoredFromState,
	}
	if !arl.lastCeilingUpdate.IsZero() {
		health.CeilingUpdatedAt = arl.lastCeilingUpdate.UTC().Format(time.RFC3339)
	}
	return health
}

// newHealthHandler returns the /health handler. Kubernetes probes only read
// the status code; the JSON body carries the live rate-limit state for humans
// and the dashboard.
func newHealthHandler(arl *AdaptiveRateLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "ok",
			"rate_limit": arl.HealthState(),
		})
	}
}
