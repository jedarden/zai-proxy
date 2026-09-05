package main

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.ardenone.com/jedarden/zai-proxy/proxy/config"
)

// forceWindowAdvance ages lastAdjustment past the adjustment window so the
// next Record* call runs an adjustment. This is the same idiom the acceptance
// tests use to drive window transitions without sleeping.
func forceWindowAdvance(arl *AdaptiveRateLimiter, window time.Duration) {
	arl.mu.Lock()
	arl.lastAdjustment = arl.lastAdjustment.Add(-window - time.Millisecond)
	arl.mu.Unlock()
}

// writeStateFile marshals s and writes it to path, for tests that seed a
// snapshot as a previous container start would have left it.
func writeStateFile(t *testing.T, path string, s RateLimitState) {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Failed to marshal state: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("Failed to write state file: %v", err)
	}
}

// readStateFile parses the state file at path.
func readStateFile(t *testing.T, path string) RateLimitState {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read state file %s: %v", path, err)
	}
	var s RateLimitState
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("Failed to parse state file %s: %v", path, err)
	}
	return s
}

// TestSaveRateLimitStateWritesDocumentedShape verifies the on-disk format is
// the documented {ceiling, hold, ts} triple.
func TestSaveRateLimitStateWritesDocumentedShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ceiling.json")
	ts := time.Now().Add(-2 * time.Minute).Truncate(time.Second)

	if err := saveRateLimitState(path, &RateLimitState{Ceiling: 30.4, Hold: 29.79, Ts: ts}); err != nil {
		t.Fatalf("saveRateLimitState() failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read saved state: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Saved state is not valid JSON: %v", err)
	}
	for _, key := range []string{"ceiling", "hold", "ts"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("Saved state missing key %q; got %s", key, data)
		}
	}

	got := readStateFile(t, path)
	if got.Ceiling != 30.4 || got.Hold != 29.79 || !got.Ts.Equal(ts) {
		t.Errorf("Round-trip mismatch: got %+v, want ceiling=30.4 hold=29.79 ts=%v", got, ts)
	}
}

// TestSaveRateLimitStateIsAtomicShape verifies the temp-then-rename write
// leaves no temp files behind in the state directory.
func TestSaveRateLimitStateLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ceiling.json")

	if err := saveRateLimitState(path, &RateLimitState{Ceiling: 10, Hold: 9.8, Ts: time.Now()}); err != nil {
		t.Fatalf("saveRateLimitState() failed: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("Failed to read state dir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("Temp file %q left behind after save", e.Name())
		}
	}
}

// TestLoadRateLimitStateRoundTrip verifies a saved snapshot reads back
// unchanged.
func TestLoadRateLimitStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ceiling.json")
	want := RateLimitState{Ceiling: 30.4, Hold: 29.79, Ts: time.Now().Add(-time.Minute)}

	if err := saveRateLimitState(path, &want); err != nil {
		t.Fatalf("saveRateLimitState() failed: %v", err)
	}

	got, err := loadRateLimitState(path, config.DefaultRateLimitStateMaxAge)
	if err != nil {
		t.Fatalf("loadRateLimitState() failed: %v", err)
	}
	if got == nil {
		t.Fatal("loadRateLimitState() returned nil state for a fresh file")
	}
	if got.Ceiling != want.Ceiling || got.Hold != want.Hold || !got.Ts.Equal(want.Ts) {
		t.Errorf("loadRateLimitState() = %+v, want %+v", got, &want)
	}
}

// TestLoadRateLimitStateMissingFileIsNotAnError verifies the first-ever start
// (no state file yet) is a normal condition, not a failure.
func TestLoadRateLimitStateMissingFileIsNotAnError(t *testing.T) {
	state, err := loadRateLimitState(filepath.Join(t.TempDir(), "ceiling.json"), config.DefaultRateLimitStateMaxAge)
	if err != nil {
		t.Errorf("loadRateLimitState() for a missing file returned error %v, want nil", err)
	}
	if state != nil {
		t.Errorf("loadRateLimitState() for a missing file = %+v, want nil state", state)
	}
}

// TestLoadRateLimitStateRejectsStaleState verifies a snapshot older than the
// max age is not returned — a restart must fall back to RATE_LIMIT_MAX rather
// than resume from a ceiling learned yesterday.
func TestLoadRateLimitStateRejectsStaleState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ceiling.json")
	writeStateFile(t, path, RateLimitState{
		Ceiling: 30.4,
		Hold:    29.79,
		Ts:      time.Now().Add(-config.DefaultRateLimitStateMaxAge - time.Minute),
	})

	state, err := loadRateLimitState(path, config.DefaultRateLimitStateMaxAge)
	if err == nil {
		t.Error("loadRateLimitState() for a stale snapshot returned nil error, want stale error")
	}
	if state != nil {
		t.Errorf("loadRateLimitState() for a stale snapshot = %+v, want nil", state)
	}
}

// TestLoadRateLimitStateUsesMtimeWhenTsMissing verifies the mtime fallback for
// a file without a usable ts, so a stale snapshot cannot pass as fresh just
// because its writer omitted the timestamp.
func TestLoadRateLimitStateUsesMtimeWhenTsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ceiling.json")
	writeStateFile(t, path, RateLimitState{Ceiling: 30.4, Hold: 29.79})

	old := time.Now().Add(-config.DefaultRateLimitStateMaxAge - time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("Failed to set file times: %v", err)
	}

	state, err := loadRateLimitState(path, config.DefaultRateLimitStateMaxAge)
	if err == nil {
		t.Error("loadRateLimitState() ignored an old mtime, want stale error")
	}
	if state != nil {
		t.Errorf("loadRateLimitState() for a stale mtime = %+v, want nil", state)
	}
}

// TestLoadRateLimitStateRejectsCorruptFile verifies a truncated or corrupt
// snapshot is reported rather than applied.
func TestLoadRateLimitStateRejectsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ceiling.json")
	if err := os.WriteFile(path, []byte(`{"ceiling":30.4,"hol`), 0o644); err != nil {
		t.Fatalf("Failed to write corrupt state: %v", err)
	}

	state, err := loadRateLimitState(path, config.DefaultRateLimitStateMaxAge)
	if err == nil {
		t.Error("loadRateLimitState() for a corrupt file returned nil error, want error")
	}
	if state != nil {
		t.Errorf("loadRateLimitState() for a corrupt file = %+v, want nil", state)
	}
}

// TestLoadRateLimitStateRejectsNonsensicalValues verifies a parsed but
// nonsensical snapshot (non-positive rates) is reported rather than applied.
func TestLoadRateLimitStateRejectsNonsensicalValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ceiling.json")
	writeStateFile(t, path, RateLimitState{Ceiling: 0, Hold: 29.79, Ts: time.Now()})

	if _, err := loadRateLimitState(path, config.DefaultRateLimitStateMaxAge); err == nil {
		t.Error("loadRateLimitState() for ceiling=0 returned nil error, want error")
	}
}

// TestRestoreFromStateFileFreshStateResumesAtStoredCeiling is the core
// acceptance case: a restart with a fresh state file must resume at the
// stored ceiling and hold rate instead of re-learning from RATE_LIMIT_MAX.
func TestRestoreFromStateFileFreshStateResumesAtStoredCeiling(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ceiling.json")
	writeStateFile(t, path, RateLimitState{Ceiling: 30.4, Hold: 29.79, Ts: time.Now()})

	// initial 8 req/s, RATE_LIMIT_MAX 40 — the production configuration that
	// logged "Ceiling updated 40.00 → 30.40" on every restart.
	arl := NewAdaptiveRateLimiterWithWindow(8.0, 0.5, 40.0, 30*time.Second)
	if !arl.RestoreFromStateFile(path, config.DefaultRateLimitStateMaxAge) {
		t.Fatal("RestoreFromStateFile() = false for a fresh state, want true")
	}

	arl.mu.RLock()
	gotCeiling, gotHold := arl.estimatedCeiling, arl.currentRate
	arl.mu.RUnlock()

	if gotCeiling != 30.4 {
		t.Errorf("Resumed ceiling = %.2f, want 30.40 (RATE_LIMIT_MAX would be 40.00)", gotCeiling)
	}
	if gotHold != 29.79 {
		t.Errorf("Resumed hold rate = %.2f, want 29.79", gotHold)
	}
	if got := arl.GetCurrentRate(); got != 29.79 {
		t.Errorf("GetCurrentRate() = %.2f, want the resumed hold 29.79", got)
	}
}

// TestRestoreFromStateFileStaleStateIgnored verifies a stale snapshot is
// ignored and the limiter keeps the pre-restart assumption of RATE_LIMIT_MAX.
func TestRestoreFromStateFileStaleStateIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ceiling.json")
	writeStateFile(t, path, RateLimitState{
		Ceiling: 30.4,
		Hold:    29.79,
		Ts:      time.Now().Add(-config.DefaultRateLimitStateMaxAge - time.Minute),
	})

	arl := NewAdaptiveRateLimiterWithWindow(8.0, 0.5, 40.0, 30*time.Second)
	if arl.RestoreFromStateFile(path, config.DefaultRateLimitStateMaxAge) {
		t.Error("RestoreFromStateFile() = true for a stale state, want false")
	}

	arl.mu.RLock()
	gotCeiling, restored := arl.estimatedCeiling, arl.restoredFromState
	arl.mu.RUnlock()

	if gotCeiling != 40.0 {
		t.Errorf("Ceiling after ignoring stale state = %.2f, want the RATE_LIMIT_MAX 40.00", gotCeiling)
	}
	if restored {
		t.Error("restoredFromState = true after ignoring stale state, want false")
	}
}

// TestRestoreFromStateFileMissingStateStartsFromMax verifies the no-file case
// behaves exactly like the pre-persistence behaviour.
func TestRestoreFromStateFileMissingStateStartsFromMax(t *testing.T) {
	arl := NewAdaptiveRateLimiterWithWindow(8.0, 0.5, 40.0, 30*time.Second)
	if arl.RestoreFromStateFile(filepath.Join(t.TempDir(), "ceiling.json"), config.DefaultRateLimitStateMaxAge) {
		t.Error("RestoreFromStateFile() = true with no state file, want false")
	}

	arl.mu.RLock()
	gotCeiling := arl.estimatedCeiling
	arl.mu.RUnlock()

	if gotCeiling != 40.0 {
		t.Errorf("Ceiling with no state file = %.2f, want RATE_LIMIT_MAX 40.00", gotCeiling)
	}
}

// TestRestoreCeilingClampsToConfiguredRange verifies a snapshot taken under a
// larger RATE_LIMIT_MAX cannot push this instance past its own configured
// bounds.
func TestRestoreCeilingClampsToConfiguredRange(t *testing.T) {
	arl := NewAdaptiveRateLimiterWithWindow(8.0, 0.5, 40.0, 30*time.Second)

	arl.RestoreCeiling(&RateLimitState{Ceiling: 500, Hold: 490, Ts: time.Now()})
	arl.mu.RLock()
	ceiling, hold := arl.estimatedCeiling, arl.currentRate
	arl.mu.RUnlock()
	if ceiling != 40.0 {
		t.Errorf("Ceiling above maxRate not clamped: got %.2f, want 40.00", ceiling)
	}
	if hold != 40.0 {
		t.Errorf("Hold above ceiling not clamped: got %.2f, want 40.00", hold)
	}

	arl.RestoreCeiling(&RateLimitState{Ceiling: 20, Hold: 0.001, Ts: time.Now()})
	arl.mu.RLock()
	ceiling, hold = arl.estimatedCeiling, arl.currentRate
	arl.mu.RUnlock()
	if hold != 0.5 {
		t.Errorf("Hold below minRate not clamped: got %.2f, want 0.50", hold)
	}
}

// TestCeilingUpdatePersistsState verifies every ceiling update writes the
// snapshot to the state file.
func TestCeilingUpdatePersistsState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ceiling.json")
	testWindow := 10 * time.Millisecond

	arl := NewAdaptiveRateLimiterWithWindow(30.0, 1.0, 50.0, testWindow)
	arl.statePath = path

	// A 429-dominated window is a ceiling update and must be persisted.
	for i := 0; i < 5; i++ {
		arl.Record429()
	}
	forceWindowAdvance(arl, testWindow)
	arl.RecordSuccess()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Ceiling update did not persist state to %s: %v", path, err)
	}

	got := readStateFile(t, path)
	arl.mu.RLock()
	ceiling, rate := arl.estimatedCeiling, arl.currentRate
	arl.mu.RUnlock()

	if got.Ceiling != ceiling {
		t.Errorf("Persisted ceiling = %.4f, want the live estimate %.4f", got.Ceiling, ceiling)
	}
	if got.Hold != rate {
		t.Errorf("Persisted hold = %.4f, want the live rate %.4f", got.Hold, rate)
	}
	if got.Ts.IsZero() {
		t.Error("Persisted ts is zero, want the time the ceiling was learned")
	}
	if ceiling >= 50.0 {
		t.Errorf("Ceiling did not move off RATE_LIMIT_MAX after a 429 window: %.2f", ceiling)
	}
}

// TestCleanWindowsDoNotPersistState verifies the state file is written on
// ceiling updates only, not on every converge/probe adjustment.
func TestCleanWindowsDoNotPersistState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ceiling.json")
	testWindow := 10 * time.Millisecond

	arl := NewAdaptiveRateLimiterWithWindow(30.0, 1.0, 50.0, testWindow)
	arl.statePath = path

	// Clean windows adjust the rate (converge, then probe) without learning a
	// new ceiling, so nothing should be written.
	for i := 0; i < 3; i++ {
		for j := 0; j < 100; j++ {
			arl.RecordSuccess()
		}
		forceWindowAdvance(arl, testWindow)
		arl.RecordSuccess()
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("State file written without a ceiling update: %v", err)
	}
}

// TestPersistFailureIsTolerated verifies an unwritable state path does not
// disturb the adjustment itself — persistence is best-effort.
func TestPersistFailureIsTolerated(t *testing.T) {
	testWindow := 10 * time.Millisecond

	arl := NewAdaptiveRateLimiterWithWindow(30.0, 1.0, 50.0, testWindow)
	// Parent directory does not exist, so the write cannot succeed.
	arl.statePath = filepath.Join(t.TempDir(), "missing-dir", "ceiling.json")

	arl.Record429()
	forceWindowAdvance(arl, testWindow)
	arl.RecordSuccess()

	// The adjustment still took effect even though the write failed: the rate
	// moved off its initial 30 req/s to the hold point of the updated ceiling.
	if got := arl.GetCurrentRate(); got == 30.0 {
		t.Error("Rate unchanged after a 429 window; the failed persist must not swallow the adjustment")
	}
}

// TestResetClearsPersistedState verifies an explicit reset also drops the
// snapshot, so a restart does not resurrect the estimate that was discarded.
func TestResetClearsPersistedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ceiling.json")
	writeStateFile(t, path, RateLimitState{Ceiling: 30.4, Hold: 29.79, Ts: time.Now()})

	arl := NewAdaptiveRateLimiterWithWindow(30.0, 1.0, 50.0, 30*time.Second)
	arl.statePath = path
	arl.Reset(8.0)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("State file survived a reset: %v", err)
	}
	arl.mu.RLock()
	updated := arl.lastCeilingUpdate
	arl.mu.RUnlock()
	if !updated.IsZero() {
		t.Errorf("lastCeilingUpdate = %v after reset, want zero", updated)
	}
}

// TestResetWithoutStatePath verifies the reset path tolerates persistence
// being disabled (statePath empty).
func TestResetWithoutStatePath(t *testing.T) {
	arl := NewAdaptiveRateLimiterWithWindow(30.0, 1.0, 50.0, 30*time.Second)
	arl.Reset(8.0) // must not panic
}

// TestProbeStillDriftsUpwardAfterRestore verifies restoring a persisted
// ceiling leaves the probe-for-shift logic intact: when upstream has loosened,
// the estimate still climbs above the restored value.
func TestProbeStillDriftsUpwardAfterRestore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ceiling.json")
	writeStateFile(t, path, RateLimitState{Ceiling: 20.0, Hold: 19.6, Ts: time.Now()})
	testWindow := 10 * time.Millisecond

	arl := NewAdaptiveRateLimiterWithWindow(30.0, 1.0, 50.0, testWindow)
	arl.probeInterval = 3
	if !arl.RestoreFromStateFile(path, config.DefaultRateLimitStateMaxAge) {
		t.Fatal("RestoreFromStateFile() = false for a fresh state, want true")
	}

	// Drive clean windows until the probe fires above the restored ceiling.
	for i := 0; i < 10; i++ {
		for j := 0; j < 100; j++ {
			arl.RecordSuccess()
		}
		forceWindowAdvance(arl, testWindow)
		arl.RecordSuccess()
	}

	if got := arl.GetCurrentRate(); got <= 20.0 {
		t.Errorf("Rate after probing past a restored ceiling of 20 = %.2f, want above 20", got)
	}
}

// TestHealthStateMirrorsPersistedFields verifies the health payload carries
// the ceiling, hold point and learning timestamp that the state file holds.
func TestHealthStateMirrorsPersistedFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ceiling.json")
	ts := time.Now().Add(-time.Minute).Truncate(time.Second)
	writeStateFile(t, path, RateLimitState{Ceiling: 30.4, Hold: 29.79, Ts: ts})

	arl := NewAdaptiveRateLimiterWithWindow(8.0, 0.5, 40.0, 30*time.Second)
	arl.statePath = path
	arl.RestoreFromStateFile(path, config.DefaultRateLimitStateMaxAge)

	health := arl.HealthState()
	if health.Ceiling != 30.4 {
		t.Errorf("HealthState() ceiling = %.2f, want 30.40", health.Ceiling)
	}
	// hold is the live hold point derived from the restored ceiling.
	wantHold := 30.4 * (1 - arl.holdMargin)
	if math.Abs(health.Hold-wantHold) > 1e-9 {
		t.Errorf("HealthState() hold = %.6f, want the hold point %.6f", health.Hold, wantHold)
	}
	if health.CurrentRate != 29.79 {
		t.Errorf("HealthState() current_rate = %.2f, want the resumed hold 29.79", health.CurrentRate)
	}
	if health.CeilingUpdatedAt != ts.UTC().Format(time.RFC3339) {
		t.Errorf("HealthState() ceiling_updated_at = %q, want %q", health.CeilingUpdatedAt, ts.UTC().Format(time.RFC3339))
	}
	if health.StateFile != path {
		t.Errorf("HealthState() state_file = %q, want %q", health.StateFile, path)
	}
	if !health.RestoredFromState {
		t.Error("HealthState() restored_from_state = false, want true after a restore")
	}
}

// TestHealthStateWithoutCeilingUpdate verifies the health payload before any
// ceiling is learned: no timestamp, and the RATE_LIMIT_MAX assumption.
func TestHealthStateWithoutCeilingUpdate(t *testing.T) {
	arl := NewAdaptiveRateLimiterWithWindow(8.0, 0.5, 40.0, 30*time.Second)

	health := arl.HealthState()
	if health.Ceiling != 40.0 {
		t.Errorf("HealthState() ceiling = %.2f, want the RATE_LIMIT_MAX 40.00", health.Ceiling)
	}
	if health.CeilingUpdatedAt != "" {
		t.Errorf("HealthState() ceiling_updated_at = %q before any learning, want empty", health.CeilingUpdatedAt)
	}
	if health.RestoredFromState {
		t.Error("HealthState() restored_from_state = true without a restore, want false")
	}
}

// TestHealthHandler exercises the real /health handler: probes read the
// status code, callers read the JSON body.
func TestHealthHandler(t *testing.T) {
	arl := NewAdaptiveRateLimiterWithWindow(8.0, 0.5, 40.0, 30*time.Second)
	arl.statePath = config.DefaultRateLimitStateFile

	rec := httptest.NewRecorder()
	newHealthHandler(arl, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("/health status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("/health Content-Type = %q, want application/json", ct)
	}

	var payload struct {
		Status    string          `json:"status"`
		RateLimit RateLimitHealth `json:"rate_limit"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Failed to decode /health body %q: %v", rec.Body.String(), err)
	}
	if payload.Status != "ok" {
		t.Errorf("/health status field = %q, want \"ok\"", payload.Status)
	}
	if payload.RateLimit.Ceiling != 40.0 {
		t.Errorf("/health rate_limit.ceiling = %.2f, want 40.00", payload.RateLimit.Ceiling)
	}
	if payload.RateLimit.StateFile != config.DefaultRateLimitStateFile {
		t.Errorf("/health rate_limit.state_file = %q, want %q", payload.RateLimit.StateFile, config.DefaultRateLimitStateFile)
	}
}
