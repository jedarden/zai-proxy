package main

import (
	"math"
	"testing"
	"time"
)

const (
	clean429s          = 9
	cleanThreshold429s = 10
	nonClean429s       = 11
	windowTotal        = 1000
)

// recordWindow records one complete accounting window, then evaluates it without
// adding a synthetic request. Keeping the numerator and denominator exact makes
// the tests deterministic on either side of the 1% clean-window threshold.
func recordWindow(t *testing.T, arl *AdaptiveRateLimiter, count429, total int) {
	t.Helper()
	if count429 < 0 || count429 > total {
		t.Fatalf("invalid window: %d 429s in %d requests", count429, total)
	}

	// Prevent the request loop from crossing a short test window under the race
	// detector. The complete window must be evaluated as one accounting unit.
	arl.mu.Lock()
	arl.lastAdjustment = time.Now().Add(arl.adjustmentWindow + time.Hour)
	arl.mu.Unlock()

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

// recordWindowAtPercent is the shared fixture for clean-window tests that use
// human-readable percentages. The 0.001% resolution keeps every threshold
// case exact while avoiding an extra request merely to trigger evaluation.
func recordWindowAtPercent(t *testing.T, arl *AdaptiveRateLimiter, percent429 float64) {
	t.Helper()
	if percent429 < 0 || percent429 > 100 {
		t.Fatalf("invalid 429 percentage: %f", percent429)
	}

	const (
		thousandthsPerPercent = 1000
		windowRequests        = 100 * thousandthsPerPercent
	)
	recordWindow(t, arl, int(math.Round(percent429*thousandthsPerPercent)), windowRequests)
}

func newStateTransitionLimiter() *AdaptiveRateLimiter {
	arl := NewAdaptiveRateLimiterWithWindow(30, 1, 50, time.Hour)
	// Probes reset cleanWindows, so defer them past every sequence in this file.
	arl.probeInterval = 100
	return arl
}

func TestAdaptiveRateLimiterCleanWindowDetection(t *testing.T) {
	tests := []struct {
		name            string
		count429        int
		wantCleanWindow bool
	}{
		{name: "zero percent is clean", count429: 0, wantCleanWindow: true},
		{name: "below one percent is clean", count429: clean429s, wantCleanWindow: true},
		{name: "exactly one percent is non-clean", count429: cleanThreshold429s, wantCleanWindow: false},
		{name: "above one percent is non-clean", count429: nonClean429s, wantCleanWindow: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arl := newStateTransitionLimiter()

			recordWindow(t, arl, tt.count429, windowTotal)

			gotCleanWindow := arl.cleanWindows == 1
			if gotCleanWindow != tt.wantCleanWindow {
				t.Errorf("%d/%d 429s: clean window = %t, want %t (clean requires a rate below 1%%)",
					tt.count429, windowTotal, gotCleanWindow, tt.wantCleanWindow)
			}
		})
	}
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
