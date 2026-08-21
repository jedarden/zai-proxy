package main

import (
	"testing"
	"time"
)

// newProbeEdgeLimiter starts at the hold rate for a known ceiling. This keeps
// clean windows from exercising convergence, so a rate above the ceiling can
// only be caused by the probe branch.
func newProbeEdgeLimiter(probeInterval int) *AdaptiveRateLimiter {
	arl := NewAdaptiveRateLimiterWithWindow(49, 1, 60, time.Hour)
	arl.estimatedCeiling = 50
	arl.holdMargin = 0.02
	arl.probeInterval = probeInterval
	return arl
}

func TestAdaptiveRateLimiterProbeActivatesOnFirstCleanWindow(t *testing.T) {
	arl := newProbeEdgeLimiter(1)

	recordWindow(t, arl, 0, 1)

	if got, want := arl.cleanWindows, 0; got != want {
		t.Errorf("cleanWindows after immediate probe = %d, want %d", got, want)
	}

	probeRate := arl.estimatedCeiling * (1 + arl.holdMargin)
	if got := arl.GetCurrentRate(); got != probeRate {
		t.Errorf("rate after first clean window = %.2f, want immediate probe rate %.2f", got, probeRate)
	}
}

func TestAdaptiveRateLimiterProbeCanBeDisabled(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	tests := []struct {
		name          string
		probeInterval int
	}{
		{name: "zero interval", probeInterval: 0},
		{name: "effectively infinite interval", probeInterval: maxInt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arl := newProbeEdgeLimiter(tt.probeInterval)

			for window := 0; window < 3; window++ {
				recordWindow(t, arl, 0, 1)
			}

			if got, want := arl.cleanWindows, 3; got != want {
				t.Errorf("cleanWindows = %d, want %d without a probe", got, want)
			}
			if got, want := arl.GetCurrentRate(), 49.0; got != want {
				t.Errorf("rate = %.2f, want hold rate %.2f when probes are disabled", got, want)
			}
		})
	}
}

func TestAdaptiveRateLimiterProbeRepeatsAfterCooldown(t *testing.T) {
	arl := newProbeEdgeLimiter(2)
	probeRate := arl.estimatedCeiling * (1 + arl.holdMargin)

	for cycle := 1; cycle <= 3; cycle++ {
		// This is the cooldown window after the previous probe (or before the
		// first one). It must not activate another probe by itself.
		recordWindow(t, arl, 0, 1)
		if got, want := arl.cleanWindows, 1; got != want {
			t.Fatalf("cycle %d cooldown: cleanWindows = %d, want %d", cycle, got, want)
		}

		// The next clean window completes the interval and activates a new probe.
		recordWindow(t, arl, 0, 1)
		if got, want := arl.cleanWindows, 0; got != want {
			t.Errorf("cycle %d activation: cleanWindows = %d, want %d", cycle, got, want)
		}
		if got := arl.GetCurrentRate(); got != probeRate {
			t.Errorf("cycle %d activation: rate = %.2f, want probe rate %.2f", cycle, got, probeRate)
		}
	}
}
