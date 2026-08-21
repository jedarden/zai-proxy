package main

import (
	"testing"
	"time"
)

const (
	clean429s    = 9
	nonClean429s = 11
	windowTotal  = 1000
)

// recordWindow records one complete accounting window, then evaluates it without
// adding a synthetic request. Keeping the numerator and denominator exact makes
// the tests deterministic on either side of the 1% clean-window threshold.
func recordWindow(t *testing.T, arl *AdaptiveRateLimiter, count429, total int) {
	t.Helper()
	if count429 < 0 || count429 > total {
		t.Fatalf("invalid window: %d 429s in %d requests", count429, total)
	}

	for i := 0; i < count429; i++ {
		arl.Record429()
	}
	for i := count429; i < total; i++ {
		arl.RecordSuccess()
	}

	arl.mu.Lock()
	arl.lastAdjustment = time.Now().Add(-arl.adjustmentWindow - time.Millisecond)
	arl.mu.Unlock()
	arl.tryAdjustRate()
}

func newStateTransitionLimiter() *AdaptiveRateLimiter {
	arl := NewAdaptiveRateLimiterWithWindow(30, 1, 50, time.Hour)
	// Probes reset cleanWindows, so defer them past every sequence in this file.
	arl.probeInterval = 100
	return arl
}

func TestAdaptiveRateLimiterStateTransitionCleanToNonClean(t *testing.T) {
	arl := newStateTransitionLimiter()

	recordWindow(t, arl, clean429s, windowTotal) // 0.9%, clean
	if got, want := arl.cleanWindows, 1; got != want {
		t.Fatalf("after a clean window, cleanWindows = %d, want %d", got, want)
	}

	recordWindow(t, arl, nonClean429s, windowTotal) // 1.1%, non-clean
	if got, want := arl.cleanWindows, 1; got != want {
		t.Errorf("after crossing from 0.9%% to 1.1%% 429s, cleanWindows = %d, want %d", got, want)
	}
}

func TestAdaptiveRateLimiterStateTransitionNonCleanToClean(t *testing.T) {
	arl := newStateTransitionLimiter()

	recordWindow(t, arl, clean429s, windowTotal)
	recordWindow(t, arl, clean429s, windowTotal)
	if got, want := arl.cleanWindows, 2; got != want {
		t.Fatalf("after initial clean windows, cleanWindows = %d, want %d", got, want)
	}

	recordWindow(t, arl, nonClean429s, windowTotal)
	if got, want := arl.cleanWindows, 2; got != want {
		t.Fatalf("after a non-clean window, cleanWindows = %d, want %d", got, want)
	}

	recordWindow(t, arl, clean429s, windowTotal)
	if got, want := arl.cleanWindows, 3; got != want {
		t.Errorf("after dropping from 1.1%% to 0.9%% 429s, cleanWindows = %d, want %d", got, want)
	}
}

func TestAdaptiveRateLimiterStatePersistenceAcrossWindows(t *testing.T) {
	arl := newStateTransitionLimiter()

	for window := 1; window <= 3; window++ {
		recordWindow(t, arl, clean429s, windowTotal)
		if got, want := arl.cleanWindows, window; got != want {
			t.Fatalf("clean window %d: cleanWindows = %d, want %d", window, got, want)
		}
	}

	for window := 1; window <= 3; window++ {
		recordWindow(t, arl, nonClean429s, windowTotal)
		if got, want := arl.cleanWindows, 3; got != want {
			t.Errorf("non-clean window %d: cleanWindows = %d, want %d", window, got, want)
		}
	}
}

func TestAdaptiveRateLimiterStateRapidThresholdCrossings(t *testing.T) {
	arl := newStateTransitionLimiter()

	tests := []struct {
		name        string
		count429    int
		wantWindows int
	}{
		{name: "clean below threshold", count429: clean429s, wantWindows: 1},
		{name: "non-clean above threshold", count429: nonClean429s, wantWindows: 1},
		{name: "clean below threshold again", count429: clean429s, wantWindows: 2},
		{name: "non-clean above threshold again", count429: nonClean429s, wantWindows: 2},
		{name: "clean below threshold once more", count429: clean429s, wantWindows: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recordWindow(t, arl, tt.count429, windowTotal)
			if got := arl.cleanWindows; got != tt.wantWindows {
				t.Errorf("after %d/1000 429s, cleanWindows = %d, want %d", tt.count429, got, tt.wantWindows)
			}
		})
	}
}
