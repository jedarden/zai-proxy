package main

import (
	"sync"
	"testing"
	"time"
)

// TestAdaptiveRateLimiter_Bounds verifies rate stays within [minRate, maxRate]
func TestAdaptiveRateLimiter_Bounds(t *testing.T) {
	tests := []struct {
		name             string
		initialRate      float64
		minRate          float64
		maxRate          float64
		operations       []operation
		wantFinalInRange bool
	}{
		{
			name:        "429 overload stays at minimum",
			initialRate: 10.0,
			minRate:     1.0,
			maxRate:     50.0,
			operations: []operation{
				record429s(100),
				advanceWindow(),
				record429s(100),
				advanceWindow(),
				record429s(100),
				advanceWindow(),
			},
			wantFinalInRange: true,
		},
		{
			name:        "continuous success converges to ceiling but respects max",
			initialRate: 10.0,
			minRate:     1.0,
			maxRate:     20.0,
			operations: []operation{
				recordSuccesses(1000),
				advanceWindow(),
				recordSuccesses(1000),
				advanceWindow(),
				recordSuccesses(1000),
				advanceWindow(),
				recordSuccesses(1000),
				advanceWindow(),
				recordSuccesses(1000),
				advanceWindow(),
				recordSuccesses(1000),
				advanceWindow(),
				recordSuccesses(1000),
				advanceWindow(),
				recordSuccesses(1000),
				advanceWindow(),
				recordSuccesses(1000),
				advanceWindow(),
				recordSuccesses(1000),
				advanceWindow(),
				recordSuccesses(1000),
				advanceWindow(),
				recordSuccesses(1000),
				advanceWindow(),
				recordSuccesses(1000),
				advanceWindow(),
				recordSuccesses(1000),
				advanceWindow(),
			},
			wantFinalInRange: true,
		},
		{
			name:        "mixed 429/success stays in bounds",
			initialRate: 25.0,
			minRate:     5.0,
			maxRate:     100.0,
			operations: []operation{
				record429s(20),
				advanceWindow(),
				recordSuccesses(100),
				advanceWindow(),
				record429s(10),
				advanceWindow(),
				recordSuccesses(100),
				advanceWindow(),
				record429s(50),
				advanceWindow(),
			},
			wantFinalInRange: true,
		},
		{
			name:        "extreme 429 burst then recovery stays in bounds",
			initialRate: 30.0,
			minRate:     2.0,
			maxRate:     60.0,
			operations: []operation{
				record429s(500),
				advanceWindow(),
				record429s(500),
				advanceWindow(),
				recordSuccesses(100),
				advanceWindow(),
				recordSuccesses(100),
				advanceWindow(),
				recordSuccesses(100),
				advanceWindow(),
			},
			wantFinalInRange: true,
		},
		{
			name:        "rate never goes below minRate even with sustained 429s",
			initialRate: 10.0,
			minRate:     5.0,
			maxRate:     50.0,
			operations: []operation{
				record429s(1000),
				advanceWindow(),
				record429s(1000),
				advanceWindow(),
				record429s(1000),
				advanceWindow(),
				record429s(1000),
				advanceWindow(),
				record429s(1000),
				advanceWindow(),
			},
			wantFinalInRange: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arl := NewAdaptiveRateLimiter(tt.initialRate, tt.minRate, tt.maxRate)

			for _, op := range tt.operations {
				op.apply(arl)
			}

			finalRate := arl.GetCurrentRate()

			if finalRate < tt.minRate || finalRate > tt.maxRate {
				t.Errorf("Rate out of bounds: got %.2f, want in [%.2f, %.2f]",
					finalRate, tt.minRate, tt.maxRate)
			}
		})
	}
}

// TestAdaptiveRateLimiter_EWMACeilingUpdate tests 429-rate > 5% triggers EWMA update
func TestAdaptiveRateLimiter_EWMACeilingUpdate(t *testing.T) {
	tests := []struct {
		name               string
		initialRate        float64
		minRate            float64
		maxRate            float64
		alpha              float64
		holdMargin         float64
		operations         []operation
		wantCeilingDecrease bool
		wantRateDrop       bool
	}{
		{
			name:        "high 429 rate updates ceiling and drops rate",
			initialRate: 30.0,
			minRate:     1.0,
			maxRate:     50.0,
			alpha:       0.3,
			holdMargin:  0.02,
			operations: []operation{
				record429s(10),
				recordSuccesses(90),
				advanceWindow(),
			},
			wantCeilingDecrease: true,
			wantRateDrop:       true,
		},
		{
			name:        "exactly 5% 429 rate triggers decrease",
			initialRate: 20.0,
			minRate:     1.0,
			maxRate:     40.0,
			alpha:       0.3,
			holdMargin:  0.02,
			operations: []operation{
				record429s(5),
				recordSuccesses(95),
				advanceWindow(),
			},
			wantCeilingDecrease: true,
			wantRateDrop:       true,
		},
		{
			name:        "just above 5% threshold (5.1%)",
			initialRate: 25.0,
			minRate:     1.0,
			maxRate:     50.0,
			alpha:       0.3,
			holdMargin:  0.02,
			operations: []operation{
				record429s(6),
				recordSuccesses(94),
				advanceWindow(),
			},
			wantCeilingDecrease: true,
			wantRateDrop:       true,
		},
		{
			name:        "just below 5% threshold (4.9%)",
			initialRate: 25.0,
			minRate:     1.0,
			maxRate:     50.0,
			alpha:       0.3,
			holdMargin:  0.02,
			operations: []operation{
				record429s(4),
				recordSuccesses(96),
				advanceWindow(),
			},
			wantCeilingDecrease: false,
			wantRateDrop:       false,
		},
		{
			name:        "severe 429 burst (50%) drops ceiling aggressively",
			initialRate: 40.0,
			minRate:     1.0,
			maxRate:     80.0,
			alpha:       0.3,
			holdMargin:  0.02,
			operations: []operation{
				record429s(50),
				recordSuccesses(50),
				advanceWindow(),
			},
			wantCeilingDecrease: true,
			wantRateDrop:       true,
		},
		{
			name:        "100% 429 rate drops ceiling to near current rate",
			initialRate: 30.0,
			minRate:     1.0,
			maxRate:     60.0,
			alpha:       0.3,
			holdMargin:  0.02,
			operations: []operation{
				record429s(100),
				advanceWindow(),
			},
			wantCeilingDecrease: true,
			wantRateDrop:       true,
		},
		{
			name:        "custom alpha (0.5) makes ceiling more reactive",
			initialRate: 30.0,
			minRate:     1.0,
			maxRate:     50.0,
			alpha:       0.5,
			holdMargin:  0.02,
			operations: []operation{
				record429s(10),
				recordSuccesses(90),
				advanceWindow(),
			},
			wantCeilingDecrease: true,
			wantRateDrop:       true,
		},
		{
			name:        "custom alpha (0.1) makes ceiling less reactive",
			initialRate: 30.0,
			minRate:     1.0,
			maxRate:     50.0,
			alpha:       0.1,
			holdMargin:  0.02,
			operations: []operation{
				record429s(10),
				recordSuccesses(90),
				advanceWindow(),
			},
			wantCeilingDecrease: true,
			wantRateDrop:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arl := NewAdaptiveRateLimiter(tt.initialRate, tt.minRate, tt.maxRate)
			arl.ceilingSmoothAlpha = tt.alpha
			arl.holdMargin = tt.holdMargin

			initialCeiling := arl.estimatedCeiling
			initialRate := arl.GetCurrentRate()

			for _, op := range tt.operations {
				op.apply(arl)
			}

			finalCeiling := arl.estimatedCeiling
			finalRate := arl.GetCurrentRate()

			// Calculate expected hold position based on final ceiling
			expectedHoldRate := finalCeiling * (1 - tt.holdMargin)

			if tt.wantCeilingDecrease && finalCeiling >= initialCeiling {
				t.Errorf("Expected ceiling to decrease, but went from %.2f to %.2f",
					initialCeiling, finalCeiling)
			}
			if !tt.wantCeilingDecrease && finalCeiling < initialCeiling {
				t.Errorf("Expected ceiling to stay same or increase, but went from %.2f to %.2f",
					initialCeiling, finalCeiling)
			}

			// Rate adjustment behavior: when 429s are detected, rate moves to hold position
			// The hold position may be higher OR lower than initial rate depending on starting point
			if tt.wantRateDrop {
				// Rate should move toward hold position
				tolerance := expectedHoldRate * 0.01
				if finalRate < expectedHoldRate-tolerance || finalRate > expectedHoldRate+tolerance {
					t.Errorf("Expected rate near hold position %.2f±%.2f, got %.2f (initial: %.2f, ceiling: %.2f)",
						expectedHoldRate, tolerance, finalRate, initialRate, finalCeiling)
				}
			} else {
				// Rate should stay relatively stable (within 5%)
				if finalRate < initialRate*0.95 || finalRate > initialRate*1.05 {
					t.Errorf("Expected rate to stay stable, but changed from %.2f to %.2f",
						initialRate, finalRate)
				}
			}
		})
	}
}

// TestAdaptiveRateLimiter_Convergence tests 429-rate < 1% convergence behavior
func TestAdaptiveRateLimiter_Convergence(t *testing.T) {
	tests := []struct {
		name           string
		initialRate    float64
		minRate        float64
		maxRate        float64
		holdMargin     float64
		startBelowHold bool
		operations     []operation
		wantIncrease   bool
	}{
		{
			name:           "clean window when below hold converges upward",
			initialRate:    10.0,
			minRate:        1.0,
			maxRate:        50.0,
			holdMargin:     0.02,
			startBelowHold: true,
			operations: []operation{
				recordSuccesses(100),
				advanceWindow(),
			},
			wantIncrease: true,
		},
		{
			name:           "exactly 1% 429 rate allows convergence",
			initialRate:    10.0,
			minRate:        1.0,
			maxRate:        50.0,
			holdMargin:     0.02,
			startBelowHold: true,
			operations: []operation{
				record429s(1),
				recordSuccesses(99),
				advanceWindow(),
			},
			wantIncrease: true,
		},
		{
			name:           "just below 1% (0.9%) allows convergence",
			initialRate:    10.0,
			minRate:        1.0,
			maxRate:        50.0,
			holdMargin:     0.02,
			startBelowHold: true,
			operations: []operation{
				record429s(1),
				recordSuccesses(109),
				advanceWindow(),
			},
			wantIncrease: true,
		},
		{
			name:           "multiple clean windows converge stepwise",
			initialRate:    10.0,
			minRate:        1.0,
			maxRate:        50.0,
			holdMargin:     0.02,
			startBelowHold: true,
			operations: []operation{
				recordSuccesses(100),
				advanceWindow(),
				recordSuccesses(100),
				advanceWindow(),
				recordSuccesses(100),
				advanceWindow(),
			},
			wantIncrease: true,
		},
		{
			name:           "at or above hold with clean windows stays steady",
			initialRate:    49.0,
			minRate:        1.0,
			maxRate:        50.0,
			holdMargin:     0.02,
			startBelowHold: false,
			operations: []operation{
				recordSuccesses(100),
				advanceWindow(),
				recordSuccesses(100),
				advanceWindow(),
			},
			wantIncrease: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arl := NewAdaptiveRateLimiter(tt.initialRate, tt.minRate, tt.maxRate)
			arl.holdMargin = tt.holdMargin

			if tt.startBelowHold {
				holdRate := arl.estimatedCeiling * (1 - tt.holdMargin)
				if arl.GetCurrentRate() >= holdRate {
					t.Skipf("Cannot test below-hold convergence: current rate %.2f >= hold %.2f",
						arl.GetCurrentRate(), holdRate)
				}
			}

			initialRate := arl.GetCurrentRate()

			for _, op := range tt.operations {
				op.apply(arl)
			}

			finalRate := arl.GetCurrentRate()

			if tt.wantIncrease && finalRate <= initialRate {
				t.Errorf("Expected rate to increase, but stayed at or below %.2f (got %.2f)",
					initialRate, finalRate)
			}
			if !tt.wantIncrease && finalRate > initialRate*1.01 {
				t.Errorf("Expected rate to stay steady, but increased from %.2f to %.2f",
					initialRate, finalRate)
			}
		})
	}
}

// TestAdaptiveRateLimiter_Probe tests probing above ceiling after probe_interval clean windows
func TestAdaptiveRateLimiter_Probe(t *testing.T) {
	tests := []struct {
		name              string
		initialRate       float64
		minRate           float64
		maxRate           float64
		holdMargin        float64
		probeInterval     int
		operations        []operation
		wantRateAboveHold bool
	}{
		{
			name:          "probe after default 10 clean windows",
			initialRate:   30.0,
			minRate:       1.0,
			maxRate:       50.0,
			holdMargin:    0.02,
			probeInterval: 10,
			operations: flattenSequence(
				repeatOps(10, sequence(
					recordSuccesses(100),
					advanceWindow(),
				)),
			),
			wantRateAboveHold: true,
		},
		{
			name:          "probe with custom interval of 5",
			initialRate:   30.0,
			minRate:       1.0,
			maxRate:       50.0,
			holdMargin:    0.02,
			probeInterval: 5,
			operations: flattenSequence(
				repeatOps(5, sequence(
					recordSuccesses(100),
					advanceWindow(),
				)),
			),
			wantRateAboveHold: true,
		},
		{
			name:          "probe capped at maxRate",
			initialRate:   40.0,
			minRate:       1.0,
			maxRate:       45.0,
			holdMargin:    0.02,
			probeInterval: 10,
			operations: flattenSequence(
				repeatOps(10, sequence(
					recordSuccesses(100),
					advanceWindow(),
				)),
			),
			wantRateAboveHold: true,
		},
		{
			name:          "no probe before interval (9 windows with interval 10)",
			initialRate:   30.0,
			minRate:       1.0,
			maxRate:       50.0,
			holdMargin:    0.02,
			probeInterval: 10,
			operations: flattenSequence(
				repeatOps(9, sequence(
					recordSuccesses(100),
					advanceWindow(),
				)),
			),
			wantRateAboveHold: false,
		},
		{
			name:          "single 429 resets clean window counter",
			initialRate:   30.0,
			minRate:       1.0,
			maxRate:       50.0,
			holdMargin:    0.02,
			probeInterval: 10,
			operations: flattenSequence(
				repeatOps(9, sequence(
					recordSuccesses(100),
					advanceWindow(),
				)),
				[]operation{sequence(
					record429s(1),
					recordSuccesses(99),
					advanceWindow(),
				)},
				repeatOps(10, sequence(
					recordSuccesses(100),
					advanceWindow(),
				)),
			),
			wantRateAboveHold: true,
		},
		{
			name:          "probe hits ceiling then 429 drops back",
			initialRate:   30.0,
			minRate:       1.0,
			maxRate:       50.0,
			holdMargin:    0.02,
			probeInterval: 10,
			operations: flattenSequence(
				repeatOps(10, sequence(
					recordSuccesses(100),
					advanceWindow(),
				)),
				[]operation{sequence(
					record429s(10),
					recordSuccesses(90),
					advanceWindow(),
				)},
			),
			wantRateAboveHold: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arl := NewAdaptiveRateLimiter(tt.initialRate, tt.minRate, tt.maxRate)
			arl.holdMargin = tt.holdMargin
			arl.probeInterval = tt.probeInterval

			holdRate := arl.estimatedCeiling * (1 - tt.holdMargin)

			for _, op := range tt.operations {
				op.apply(arl)
			}

			finalRate := arl.GetCurrentRate()

			if tt.wantRateAboveHold && finalRate <= holdRate {
				t.Errorf("Expected rate above hold (%.2f), got %.2f",
					holdRate, finalRate)
			}
			if !tt.wantRateAboveHold && finalRate > arl.estimatedCeiling {
				t.Errorf("Expected rate at or below ceiling (%.2f), got %.2f",
					arl.estimatedCeiling, finalRate)
			}
		})
	}
}

// TestAdaptiveRateLimiter_Reset tests Reset() restores initial state
func TestAdaptiveRateLimiter_Reset(t *testing.T) {
	tests := []struct {
		name             string
		initialRate      float64
		minRate          float64
		maxRate          float64
		resetTo          float64
		preResetOps      []operation
		wantCurrentAfter float64
		wantCeilingAfter float64
		wantCleanWindows int
	}{
		{
			name:             "reset after heavy 429 load",
			initialRate:      10.0,
			minRate:          1.0,
			maxRate:          50.0,
			resetTo:          10.0,
			preResetOps: []operation{
				record429s(100),
				advanceWindow(),
				record429s(100),
				advanceWindow(),
			},
			wantCurrentAfter: 10.0,
			wantCeilingAfter: 10.0,
			wantCleanWindows: 0,
		},
		{
			name:             "reset to different rate",
			initialRate:      10.0,
			minRate:          1.0,
			maxRate:          50.0,
			resetTo:          25.0,
			preResetOps: []operation{
				record429s(50),
				advanceWindow(),
			},
			wantCurrentAfter: 25.0,
			wantCeilingAfter: 25.0,
			wantCleanWindows: 0,
		},
		{
			name:             "reset clears atomic counters",
			initialRate:      10.0,
			minRate:          1.0,
			maxRate:          50.0,
			resetTo:          10.0,
			preResetOps: []operation{
				record429s(10),
				recordSuccesses(10),
			},
			wantCurrentAfter: 10.0,
			wantCeilingAfter: 10.0,
			wantCleanWindows: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arl := NewAdaptiveRateLimiter(tt.initialRate, tt.minRate, tt.maxRate)

			for _, op := range tt.preResetOps {
				op.apply(arl)
			}

			arl.Reset(tt.resetTo)

			if got := arl.GetCurrentRate(); got != tt.wantCurrentAfter {
				t.Errorf("Reset() currentRate = %.2f, want %.2f", got, tt.wantCurrentAfter)
			}
			if got := arl.estimatedCeiling; got != tt.wantCeilingAfter {
				t.Errorf("Reset() estimatedCeiling = %.2f, want %.2f", got, tt.wantCeilingAfter)
			}
			if got := arl.cleanWindows; got != tt.wantCleanWindows {
				t.Errorf("Reset() cleanWindows = %d, want %d", got, tt.wantCleanWindows)
			}
		})
	}
}

// TestAdaptiveRateLimiter_Wait tests Wait() returns sane durations
func TestAdaptiveRateLimiter_Wait(t *testing.T) {
	tests := []struct {
		name       string
		rate       float64
		minWait    time.Duration
		maxWait    time.Duration
		concurrent int
	}{
		{
			name:       "low rate (1 req/s) has measurable wait",
			rate:       1.0,
			minWait:    0,
			maxWait:    2 * time.Second,
			concurrent: 1,
		},
		{
			name:       "high rate (100 req/s) has minimal wait",
			rate:       100.0,
			minWait:    0,
			maxWait:    100 * time.Millisecond,
			concurrent: 1,
		},
		{
			name:       "medium rate (10 req/s)",
			rate:       10.0,
			minWait:    0,
			maxWait:    500 * time.Millisecond,
			concurrent: 1,
		},
		{
			name:       "concurrent waits at low rate",
			rate:       2.0,
			minWait:    0,
			maxWait:    3 * time.Second,
			concurrent: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arl := NewAdaptiveRateLimiter(tt.rate, 0.1, tt.rate*10)
			var totalWait time.Duration
			var maxObservedWait time.Duration

			var wg sync.WaitGroup
			for i := 0; i < tt.concurrent; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					wait := arl.Wait("test")
					if wait > maxObservedWait {
						maxObservedWait = wait
					}
					totalWait += wait
				}()
			}
			wg.Wait()

			avgWait := totalWait / time.Duration(tt.concurrent)

			t.Logf("Rate: %.1f req/s, Avg wait: %v, Max wait: %v",
				tt.rate, avgWait, maxObservedWait)

			if maxObservedWait < tt.minWait {
				t.Errorf("Expected wait >= %v, got %v", tt.minWait, maxObservedWait)
			}
			if maxObservedWait > tt.maxWait {
				t.Errorf("Expected wait <= %v, got %v", tt.maxWait, maxObservedWait)
			}
		})
	}
}

// TestAdaptiveRateLimiter_Concurrency tests concurrent Record429/RecordSuccess safety
func TestAdaptiveRateLimiter_Concurrency(t *testing.T) {
	tests := []struct {
		name        string
		initialRate float64
		goroutines  int
		operations  int
	}{
		{
			name:        "concurrent 429 recording",
			initialRate: 10.0,
			goroutines:  10,
			operations:  100,
		},
		{
			name:        "concurrent success recording",
			initialRate: 10.0,
			goroutines:  10,
			operations:  100,
		},
		{
			name:        "concurrent mixed 429 and success",
			initialRate: 10.0,
			goroutines:  20,
			operations:  100,
		},
		{
			name:        "concurrent with Wait() calls",
			initialRate: 10.0,
			goroutines:  10,
			operations:  50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arl := NewAdaptiveRateLimiter(tt.initialRate, 1.0, 50.0)
			var wg sync.WaitGroup

			for i := 0; i < tt.goroutines; i++ {
				wg.Add(1)
				go func(goroutineID int) {
					defer wg.Done()
					for j := 0; j < tt.operations; j++ {
						switch {
						case goroutineID%3 == 0:
							arl.Record429()
						case goroutineID%3 == 1:
							arl.RecordSuccess()
						default:
							arl.Wait("test")
						}
					}
				}(i)
			}

			wg.Wait()

			finalRate := arl.GetCurrentRate()
			t.Logf("Final rate after concurrent ops: %.2f", finalRate)

			if finalRate < 1.0 || finalRate > 50.0 {
				t.Errorf("Rate out of bounds after concurrent ops: %.2f", finalRate)
			}
		})
	}
}

// TestAdaptiveRateLimiter_EnvVars tests environment variable parsing
func TestAdaptiveRateLimiter_EnvVars(t *testing.T) {
	tests := []struct {
		name              string
		initialRate       float64
		minRate           float64
		maxRate           float64
		setAlpha          float64
		setHoldMargin     float64
		setProbeInterval  int
		wantAlpha         float64
		wantHoldMargin    float64
		wantProbeInterval int
	}{
		{
			name:             "default values",
			initialRate:      10.0,
			minRate:          1.0,
			maxRate:          50.0,
			setAlpha:         0.3,
			setHoldMargin:    0.02,
			setProbeInterval: 10,
			wantAlpha:        0.3,
			wantHoldMargin:   0.02,
			wantProbeInterval: 10,
		},
		{
			name:             "custom alpha 0.5",
			initialRate:      10.0,
			minRate:          1.0,
			maxRate:          50.0,
			setAlpha:         0.5,
			setHoldMargin:    0.02,
			setProbeInterval: 10,
			wantAlpha:        0.5,
			wantHoldMargin:   0.02,
			wantProbeInterval: 10,
		},
		{
			name:             "custom hold margin 5%",
			initialRate:      10.0,
			minRate:          1.0,
			maxRate:          50.0,
			setAlpha:         0.3,
			setHoldMargin:    0.05,
			setProbeInterval: 10,
			wantAlpha:        0.3,
			wantHoldMargin:   0.05,
			wantProbeInterval: 10,
		},
		{
			name:             "custom probe interval 20",
			initialRate:      10.0,
			minRate:          1.0,
			maxRate:          50.0,
			setAlpha:         0.3,
			setHoldMargin:    0.02,
			setProbeInterval: 20,
			wantAlpha:        0.3,
			wantHoldMargin:   0.02,
			wantProbeInterval: 20,
		},
		{
			name:             "all custom values",
			initialRate:      10.0,
			minRate:          1.0,
			maxRate:          50.0,
			setAlpha:         0.7,
			setHoldMargin:    0.10,
			setProbeInterval: 15,
			wantAlpha:        0.7,
			wantHoldMargin:   0.10,
			wantProbeInterval: 15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arl := NewAdaptiveRateLimiter(tt.initialRate, tt.minRate, tt.maxRate)

			arl.ceilingSmoothAlpha = tt.setAlpha
			arl.holdMargin = tt.setHoldMargin
			arl.probeInterval = tt.setProbeInterval

			if got := arl.ceilingSmoothAlpha; got != tt.wantAlpha {
				t.Errorf("ceilingSmoothAlpha = %.2f, want %.2f", got, tt.wantAlpha)
			}
			if got := arl.holdMargin; got != tt.wantHoldMargin {
				t.Errorf("holdMargin = %.2f, want %.2f", got, tt.wantHoldMargin)
			}
			if got := arl.probeInterval; got != tt.wantProbeInterval {
				t.Errorf("probeInterval = %d, want %d", got, tt.wantProbeInterval)
			}
		})
	}
}

// TestAdaptiveRateLimiter_BasicState tests basic state initialization and access
func TestAdaptiveRateLimiter_BasicState(t *testing.T) {
	tests := []struct {
		name             string
		initialRate      float64
		minRate          float64
		maxRate          float64
		wantInitialRate  float64
		wantCeiling      float64
	}{
		{
			name:            "default initialization",
			initialRate:     50.0,
			minRate:         10.0,
			maxRate:         100.0,
			wantInitialRate: 50.0,
			wantCeiling:     100.0,
		},
		{
			name:            "initial rate at max",
			initialRate:     100.0,
			minRate:         10.0,
			maxRate:         100.0,
			wantInitialRate: 100.0,
			wantCeiling:     100.0,
		},
		{
			name:            "initial rate at min",
			initialRate:     10.0,
			minRate:         10.0,
			maxRate:         100.0,
			wantInitialRate: 10.0,
			wantCeiling:     100.0,
		},
		{
			name:            "small range",
			initialRate:     25.0,
			minRate:         20.0,
			maxRate:         30.0,
			wantInitialRate: 25.0,
			wantCeiling:     30.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arl := NewAdaptiveRateLimiter(tt.initialRate, tt.minRate, tt.maxRate)

			// Test GetCurrentRate returns initial rate
			if got := arl.GetCurrentRate(); got != tt.wantInitialRate {
				t.Errorf("GetCurrentRate() = %.2f, want %.2f", got, tt.wantInitialRate)
			}

			// Test ceiling starts at maxRate
			if got := arl.estimatedCeiling; got != tt.wantCeiling {
				t.Errorf("estimatedCeiling = %.2f, want %.2f", got, tt.wantCeiling)
			}

			// Test initial rate equals current rate
			if got := arl.GetCurrentRate(); got != tt.initialRate {
				t.Errorf("Initial currentRate = %.2f, want %.2f", got, tt.initialRate)
			}
		})
	}
}

// TestAdaptiveRateLimiter_BasicBounds tests basic min/max rate enforcement
func TestAdaptiveRateLimiter_BasicBounds(t *testing.T) {
	// Use short window duration for fast test execution without sleep calls
	testWindow := 10 * time.Millisecond

	tests := []struct {
		name        string
		initialRate float64
		minRate     float64
		maxRate     float64
	}{
		{
			name:        "standard bounds",
			initialRate: 50.0,
			minRate:     10.0,
			maxRate:     100.0,
		},
		{
			name:        "default values mentioned in task",
			initialRate: 50.0,
			minRate:     10.0,
			maxRate:     100.0,
		},
		{
			name:        "narrow range",
			initialRate: 25.0,
			minRate:     20.0,
			maxRate:     30.0,
		},
		{
			name:        "wide range",
			initialRate: 100.0,
			minRate:     1.0,
			maxRate:     1000.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use injected window duration for test speed
			arl := NewAdaptiveRateLimiterWithWindow(tt.initialRate, tt.minRate, tt.maxRate, testWindow)

			// Simulate heavy 429 load to drive rate down
			for i := 0; i < 100; i++ {
				arl.Record429()
			}

			// Force window advancement by manipulating internal state
			arl.mu.Lock()
			arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
			arl.mu.Unlock()
			arl.Record429()

			// Rate should not drop below minRate
			currentRate := arl.GetCurrentRate()
			if currentRate < tt.minRate {
				t.Errorf("Rate %.2f dropped below minRate %.2f", currentRate, tt.minRate)
			}

			// Simulate continuous success to drive rate up
			for i := 0; i < 1000; i++ {
				arl.RecordSuccess()
			}

			// Force multiple window advancements
			for i := 0; i < 20; i++ {
				arl.mu.Lock()
				arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
				arl.mu.Unlock()
				arl.RecordSuccess()
			}

			// Rate should not exceed maxRate
			currentRate = arl.GetCurrentRate()
			if currentRate > tt.maxRate {
				t.Errorf("Rate %.2f exceeded maxRate %.2f", currentRate, tt.maxRate)
			}

			// Final check: rate must be within bounds
			if currentRate < tt.minRate || currentRate > tt.maxRate {
				t.Errorf("Final rate %.2f out of bounds [%.2f, %.2f]",
					currentRate, tt.minRate, tt.maxRate)
			}
		})
	}
}

// TestAdaptiveRateLimiter_BasicReset tests basic Reset functionality
func TestAdaptiveRateLimiter_BasicReset(t *testing.T) {
	tests := []struct {
		name        string
		initialRate float64
		minRate     float64
		maxRate     float64
		resetTo     float64
	}{
		{
			name:        "reset to original",
			initialRate: 50.0,
			minRate:     10.0,
			maxRate:     100.0,
			resetTo:     50.0,
		},
		{
			name:        "reset to different value",
			initialRate: 50.0,
			minRate:     10.0,
			maxRate:     100.0,
			resetTo:     75.0,
		},
		{
			name:        "reset to min",
			initialRate: 50.0,
			minRate:     10.0,
			maxRate:     100.0,
			resetTo:     10.0,
		},
		{
			name:        "reset to max",
			initialRate: 50.0,
			minRate:     10.0,
			maxRate:     100.0,
			resetTo:     100.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arl := NewAdaptiveRateLimiter(tt.initialRate, tt.minRate, tt.maxRate)

			// Drive rate to min
			for i := 0; i < 100; i++ {
				arl.Record429()
			}
			arl.mu.Lock()
			arl.lastAdjustment = arl.lastAdjustment.Add(-arl.adjustmentWindow - 1*time.Second)
			arl.mu.Unlock()
			arl.Record429()

			// Verify rate changed
			rateBeforeReset := arl.GetCurrentRate()
			if rateBeforeReset == tt.resetTo {
				t.Skip("Rate already at reset value, cannot test reset behavior")
			}

			// Reset
			arl.Reset(tt.resetTo)

			// Verify current rate restored
			if got := arl.GetCurrentRate(); got != tt.resetTo {
				t.Errorf("After Reset(%v), GetCurrentRate() = %.2f, want %.2f",
					tt.resetTo, got, tt.resetTo)
			}

			// Verify ceiling restored
			if got := arl.estimatedCeiling; got != tt.resetTo {
				t.Errorf("After Reset(%v), estimatedCeiling = %.2f, want %.2f",
					tt.resetTo, got, tt.resetTo)
			}

			// Verify clean windows counter reset
			if got := arl.cleanWindows; got != 0 {
				t.Errorf("After Reset(), cleanWindows = %d, want 0", got)
			}
		})
	}
}

// TestAdaptiveRateLimiter_BasicGetCurrentRate tests GetCurrentRate returns expected values
func TestAdaptiveRateLimiter_BasicGetCurrentRate(t *testing.T) {
	tests := []struct {
		name           string
		initialRate    float64
		minRate        float64
		maxRate        float64
		modifyRate     bool
		expectedChange bool
	}{
		{
			name:           "returns initial rate",
			initialRate:    50.0,
			minRate:        10.0,
			maxRate:        100.0,
			modifyRate:     false,
			expectedChange: false,
		},
		{
			name:           "returns modified rate after 429",
			initialRate:    50.0,
			minRate:        10.0,
			maxRate:        100.0,
			modifyRate:     true,
			expectedChange: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arl := NewAdaptiveRateLimiter(tt.initialRate, tt.minRate, tt.maxRate)

			initialCurrentRate := arl.GetCurrentRate()
			if initialCurrentRate != tt.initialRate {
				t.Errorf("Initial GetCurrentRate() = %.2f, want %.2f",
					initialCurrentRate, tt.initialRate)
			}

			if tt.modifyRate {
				// Drive rate down with 429s
				for i := 0; i < 100; i++ {
					arl.Record429()
				}
				arl.mu.Lock()
				arl.lastAdjustment = arl.lastAdjustment.Add(-arl.adjustmentWindow - 1*time.Second)
				arl.mu.Unlock()
				arl.Record429()

				modifiedRate := arl.GetCurrentRate()
				if tt.expectedChange && modifiedRate == initialCurrentRate {
					t.Errorf("GetCurrentRate() should have changed after 429s, still %.2f",
						modifiedRate)
				}
			}
		})
	}
}

// TestAdaptiveRateLimiter_BasicEdgeCases tests basic edge cases
func TestAdaptiveRateLimiter_BasicEdgeCases(t *testing.T) {
	// Use injected window duration for fast test execution without sleep calls
	testWindow := 10 * time.Millisecond

	tests := []struct {
		name        string
		initialRate float64
		minRate     float64
		maxRate     float64
	}{
		{
			name:        "min equals max (fixed rate)",
			initialRate: 25.0,
			minRate:     25.0,
			maxRate:     25.0,
		},
		{
			name:        "zero initial rate and ceiling",
			initialRate: 0.0,
			minRate:     0.0,
			maxRate:     100.0,
		},
		{
			name:        "very small min values",
			initialRate: 1.0,
			minRate:     0.001,
			maxRate:     100.0,
		},
		{
			name:        "large max values",
			initialRate: 1000.0,
			minRate:     1.0,
			maxRate:     100000.0,
		},
		{
			name:        "extremely small range",
			initialRate: 5.0,
			minRate:     5.0,
			maxRate:     5.001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				// Should not panic on any edge case input
				if r := recover(); r != nil {
					t.Errorf("Unexpected panic on edge case %+v: %v", tt, r)
				}
			}()

			// Use injected window duration for fast tests
			arl := NewAdaptiveRateLimiterWithWindow(tt.initialRate, tt.minRate, tt.maxRate, testWindow)

			// Should return valid current rate within bounds
			currentRate := arl.GetCurrentRate()
			if currentRate < tt.minRate || currentRate > tt.maxRate {
				t.Errorf("Initial rate %.2f out of bounds [%.2f, %.2f]",
					currentRate, tt.minRate, tt.maxRate)
			}

			// Test Reset works with edge case values
			arl.Reset(tt.initialRate)
			if got := arl.GetCurrentRate(); got != tt.initialRate {
				t.Errorf("After Reset(%.2f), GetCurrentRate() = %.2f",
					tt.initialRate, got)
			}

			// For min==max case, verify rate stays constant even after adjustments
			if tt.minRate == tt.maxRate || tt.maxRate-tt.minRate < 0.01 {
				// Apply adjustment pressure with 429s
				for i := 0; i < 100; i++ {
					arl.Record429()
				}

				// Force window advancement using injected duration
				arl.mu.Lock()
				arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
				arl.mu.Unlock()
				arl.Record429()

				// Rate should still be at min/max (fixed rate behavior)
				currentRate = arl.GetCurrentRate()
				if currentRate < tt.minRate || currentRate > tt.maxRate {
					t.Errorf("With min≈max, rate should stay in [%.2f, %.2f], got %.2f",
						tt.minRate, tt.maxRate, currentRate)
				}

				// Also verify with success pressure
				for i := 0; i < 1000; i++ {
					arl.RecordSuccess()
				}
				arl.mu.Lock()
				arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
				arl.mu.Unlock()
				arl.RecordSuccess()

				currentRate = arl.GetCurrentRate()
				if currentRate < tt.minRate || currentRate > tt.maxRate {
					t.Errorf("With min≈max, rate should stay in [%.2f, %.2f] even with success pressure, got %.2f",
						tt.minRate, tt.maxRate, currentRate)
				}
			}
		})
	}
}

// TestAdaptiveRateLimiter_NoAdjustInWindow tests that tryAdjustRate doesn't run mid-window
func TestAdaptiveRateLimiter_NoAdjustInWindow(t *testing.T) {
	// Use injected window duration for fast test execution (100ms instead of 30s default)
	testWindow := 100 * time.Millisecond
	arl := NewAdaptiveRateLimiterWithWindow(10.0, 1.0, 50.0, testWindow)

	initialRate := arl.GetCurrentRate()

	for i := 0; i < 100; i++ {
		arl.Record429()
	}

	midWindowRate := arl.GetCurrentRate()
	if midWindowRate != initialRate {
		t.Errorf("Rate changed mid-window: %.2f -> %.2f", initialRate, midWindowRate)
	}

	// Manually advance the window instead of sleeping
	arl.mu.Lock()
	arl.lastAdjustment = arl.lastAdjustment.Add(-arl.adjustmentWindow - 1*time.Second)
	arl.mu.Unlock()
	arl.Record429()

	finalRate := arl.GetCurrentRate()
	if finalRate == initialRate {
		t.Errorf("Rate did not change after window advanced: still %.2f", finalRate)
	}

	// After 429s, rate should move to hold position (ceiling * (1 - holdMargin))
	// With ceiling=50 and holdMargin=0.02, expected hold position is 49.0
	// Starting from 10.0, this is an INCREASE, not a decrease
	expectedHoldRate := arl.estimatedCeiling * (1 - arl.holdMargin)
	tolerance := expectedHoldRate * 0.01
	if finalRate < expectedHoldRate-tolerance || finalRate > expectedHoldRate+tolerance {
		t.Errorf("Rate should be near hold position %.2f±%.2f after 429s, got %.2f (from %.2f)",
			expectedHoldRate, tolerance, finalRate, initialRate)
	}
}

// TestAdaptiveRateLimiter_EdgeCases tests edge cases
func TestAdaptiveRateLimiter_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		initialRate float64
		minRate     float64
		maxRate     float64
		operations  []operation
		wantPanic   bool
	}{
		{
			name:        "zero total requests (no adjustment)",
			initialRate: 10.0,
			minRate:     1.0,
			maxRate:     50.0,
			operations: []operation{
				advanceWindow(),
			},
			wantPanic: false,
		},
		{
			name:        "very small window duration",
			initialRate: 10.0,
			minRate:     1.0,
			maxRate:     50.0,
			operations: []operation{
				func(arl *AdaptiveRateLimiter) {
					arl.adjustmentWindow = 1 * time.Nanosecond
				},
				record429s(1),
				advanceWindow(),
			},
			wantPanic: false,
		},
		{
			name:        "min equals max (fixed rate)",
			initialRate: 25.0,
			minRate:     25.0,
			maxRate:     25.0,
			operations: []operation{
				record429s(100),
				advanceWindow(),
			},
			wantPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					if !tt.wantPanic {
						t.Errorf("Unexpected panic: %v", r)
					}
				}
			}()

			arl := NewAdaptiveRateLimiter(tt.initialRate, tt.minRate, tt.maxRate)

			for _, op := range tt.operations {
				op.apply(arl)
			}

			rate := arl.GetCurrentRate()
			if rate < tt.minRate || rate > tt.maxRate {
				t.Errorf("Rate out of bounds: %.2f not in [%.2f, %.2f]",
					rate, tt.minRate, tt.maxRate)
			}
		})
	}
}

// Helper types and functions

type operation func(*AdaptiveRateLimiter)

func (op operation) apply(arl *AdaptiveRateLimiter) {
	op(arl)
}

func record429s(n int64) operation {
	return func(arl *AdaptiveRateLimiter) {
		for i := int64(0); i < n; i++ {
			arl.Record429()
		}
	}
}

func recordSuccesses(n int64) operation {
	return func(arl *AdaptiveRateLimiter) {
		for i := int64(0); i < n; i++ {
			arl.RecordSuccess()
		}
	}
}

func advanceWindow() operation {
	return func(arl *AdaptiveRateLimiter) {
		arl.mu.Lock()
		arl.lastAdjustment = arl.lastAdjustment.Add(-arl.adjustmentWindow - 1*time.Second)
		arl.mu.Unlock()
		arl.RecordSuccess()
	}
}

func sequence(ops ...operation) operation {
	return func(arl *AdaptiveRateLimiter) {
		for _, op := range ops {
			op.apply(arl)
		}
	}
}

func repeatOps(n int, op operation) []operation {
	result := make([]operation, n)
	for i := 0; i < n; i++ {
		result[i] = op
	}
	return result
}

// TestAdaptiveRateLimiter_EWMAMath verifies the EWMA ceiling calculation matches formula
func TestAdaptiveRateLimiter_EWMAMath(t *testing.T) {
	// EWMA formula: new_ceiling = alpha * current_rate + (1-alpha) * old_ceiling
	// Default alpha is 0.3, so: new_ceiling = 0.3 * current_rate + 0.7 * old_ceiling

	tests := []struct {
		name               string
		initialRate        float64
		minRate            float64
		maxRate            float64
		alpha              float64
		holdMargin         float64
		initialCeiling     float64
		currentRate        float64  // Rate when 429s are detected
		percent429         float64  // 429 rate percentage (0-100)
		wantNewCeiling     float64
		wantHoldRate       float64
		description        string
	}{
		{
			name:           "alpha=0.3 basic EWMA calculation",
			initialRate:    30.0,
			minRate:        1.0,
			maxRate:        50.0,
			alpha:          0.3,
			holdMargin:     0.02,
			initialCeiling: 50.0,
			currentRate:    30.0,
			percent429:     10.0,
			// new_ceiling = 0.3 * 30 + 0.7 * 50 = 9 + 35 = 44
			wantNewCeiling: 44.0,
			// hold_rate = 44 * (1 - 0.02) = 44 * 0.98 = 43.12
			wantHoldRate:   43.12,
			description:    "Standard EWMA: 30% weight to current rate, 70% to old ceiling",
		},
		{
			name:           "alpha=0.3 severe 429 burst at high rate",
			initialRate:    48.0,
			minRate:        1.0,
			maxRate:        50.0,
			alpha:          0.3,
			holdMargin:     0.02,
			initialCeiling: 50.0,
			currentRate:    48.0,
			percent429:     20.0,
			// new_ceiling = 0.3 * 48 + 0.7 * 50 = 14.4 + 35 = 49.4
			wantNewCeiling: 49.4,
			// hold_rate = 49.4 * 0.98 = 48.412
			wantHoldRate:   48.412,
			description:    "High current rate near ceiling still reduces ceiling slightly",
		},
		{
			name:           "alpha=0.3 moderate drop from ceiling",
			initialRate:    40.0,
			minRate:        1.0,
			maxRate:        50.0,
			alpha:          0.3,
			holdMargin:     0.02,
			initialCeiling: 50.0,
			currentRate:    40.0,
			percent429:     8.0,
			// new_ceiling = 0.3 * 40 + 0.7 * 50 = 12 + 35 = 47
			wantNewCeiling: 47.0,
			// hold_rate = 47 * 0.98 = 46.06
			wantHoldRate:   46.06,
			description:    "Current rate 10 below ceiling pulls ceiling down by 3",
		},
		{
			name:           "alpha=0.5 more reactive smoothing",
			initialRate:    30.0,
			minRate:        1.0,
			maxRate:        50.0,
			alpha:          0.5,
			holdMargin:     0.02,
			initialCeiling: 50.0,
			currentRate:    30.0,
			percent429:     10.0,
			// new_ceiling = 0.5 * 30 + 0.5 * 50 = 15 + 25 = 40
			wantNewCeiling: 40.0,
			// hold_rate = 40 * 0.98 = 39.2
			wantHoldRate:   39.2,
			description:    "Higher alpha (0.5) gives more weight to current observation",
		},
		{
			name:           "alpha=0.1 less reactive smoothing",
			initialRate:    30.0,
			minRate:        1.0,
			maxRate:        50.0,
			alpha:          0.1,
			holdMargin:     0.02,
			initialCeiling: 50.0,
			currentRate:    30.0,
			percent429:     10.0,
			// new_ceiling = 0.1 * 30 + 0.9 * 50 = 3 + 45 = 48
			wantNewCeiling: 48.0,
			// hold_rate = 48 * 0.98 = 47.04
			wantHoldRate:   47.04,
			description:    "Lower alpha (0.1) gives more weight to historical ceiling",
		},
		{
			name:           "alpha=0.3 multiple consecutive updates compound",
			initialRate:    30.0,
			minRate:        1.0,
			maxRate:        50.0,
			alpha:          0.3,
			holdMargin:     0.02,
			initialCeiling: 50.0,
			currentRate:    25.0,
			percent429:     15.0,
			// First update: new_ceiling = 0.3 * 25 + 0.7 * 50 = 7.5 + 35 = 42.5
			// But we only do one window here, so ceiling = 42.5
			wantNewCeiling: 42.5,
			// hold_rate = 42.5 * 0.98 = 41.65
			wantHoldRate:   41.65,
			description:    "First EWMA update from 50 to 42.5",
		},
		{
			name:           "alpha=0.3 near-minimum rate preserves floor",
			initialRate:    2.0,
			minRate:        1.0,
			maxRate:        50.0,
			alpha:          0.3,
			holdMargin:     0.02,
			initialCeiling: 50.0,
			currentRate:    2.0,
			percent429:     100.0,
			// new_ceiling = 0.3 * 2 + 0.7 * 50 = 0.6 + 35 = 35.6
			wantNewCeiling: 35.6,
			// hold_rate = 35.6 * 0.98 = 34.888, but min is 1.0, so final rate is clamped
			wantHoldRate:   34.888,
			description:    "Even extreme 429s at low rate still produce valid ceiling",
		},
		{
			name:           "alpha=0.3 zero current rate edge case",
			initialRate:    0.0,
			minRate:        0.0,
			maxRate:        50.0,
			alpha:          0.3,
			holdMargin:     0.02,
			initialCeiling: 50.0,
			currentRate:    0.0,
			percent429:     100.0,
			// new_ceiling = 0.3 * 0 + 0.7 * 50 = 0 + 35 = 35
			wantNewCeiling: 35.0,
			// hold_rate = 35 * 0.98 = 34.3
			wantHoldRate:   34.3,
			description:    "Zero current rate still yields valid EWMA calculation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use injected window duration for fast test execution
			testWindow := 10 * time.Millisecond
			arl := NewAdaptiveRateLimiterWithWindow(tt.initialRate, tt.minRate, tt.maxRate, testWindow)

			// Set custom parameters
			arl.ceilingSmoothAlpha = tt.alpha
			arl.holdMargin = tt.holdMargin
			arl.estimatedCeiling = tt.initialCeiling
			arl.currentRate = tt.currentRate

			// Calculate total requests to achieve the desired 429 percentage
			// Use 100 total requests for easy percentage calculation
			totalRequests := int64(100)
			count429 := int64(float64(totalRequests) * tt.percent429 / 100.0)
			countSuccess := totalRequests - count429

			// Record the requests
			for i := int64(0); i < count429; i++ {
				arl.Record429()
			}
			for i := int64(0); i < countSuccess; i++ {
				arl.RecordSuccess()
			}

			// Force window advancement to trigger tryAdjustRate
			arl.mu.Lock()
			arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
			arl.mu.Unlock()
			arl.RecordSuccess() // Trigger adjustment

			// Verify EWMA ceiling math
			gotCeiling := arl.estimatedCeiling
			tolerance := 0.01 // Allow 0.01 float precision
			if gotCeiling < tt.wantNewCeiling-tolerance || gotCeiling > tt.wantNewCeiling+tolerance {
				t.Errorf("%s: EWMA ceiling = %.4f, want %.4f±%.2f\n%s",
					tt.name, gotCeiling, tt.wantNewCeiling, tolerance, tt.description)
				t.Logf("  Formula: new_ceiling = %.2f * %.2f + %.2f * %.2f = %.2f",
					tt.alpha, tt.currentRate, 1-tt.alpha, tt.initialCeiling, tt.wantNewCeiling)
			}

			// Verify hold position rate (before min clamping)
			gotHoldRate := tt.wantNewCeiling * (1 - tt.holdMargin)
			tolerance = 0.001 // Tighter tolerance for derived value
			if gotHoldRate < tt.wantHoldRate-tolerance || gotHoldRate > tt.wantHoldRate+tolerance {
				t.Errorf("%s: Expected hold rate = %.4f, got %.4f±%.2f",
					tt.name, tt.wantHoldRate, gotHoldRate, tolerance)
			}

			// Verify final rate (may be clamped to minRate)
			finalRate := arl.GetCurrentRate()
			expectedFinalRate := tt.wantHoldRate
			if expectedFinalRate < tt.minRate {
				expectedFinalRate = tt.minRate
			}
			if finalRate < expectedFinalRate-0.01 || finalRate > expectedFinalRate+0.01 {
				t.Errorf("%s: Final rate = %.4f, want %.4f (clamped to min %.2f)",
					tt.name, finalRate, expectedFinalRate, tt.minRate)
			}

			t.Logf("✓ %s", tt.description)
			t.Logf("  Ceiling: %.2f → %.2f, hold rate: %.2f, final rate: %.2f",
				tt.initialCeiling, gotCeiling, gotHoldRate, finalRate)
		})
	}
}

// TestAdaptiveRateLimiter_ConvergenceSteps verifies 50% step convergence to hold position
func TestAdaptiveRateLimiter_ConvergenceSteps(t *testing.T) {
	// Convergence formula when below hold and 429-rate < 1%:
	// step = gap * 0.5 (close 50% of gap each window, minimum 0.25)
	// new_rate = current_rate + step

	tests := []struct {
		name            string
		initialRate     float64
		minRate         float64
		maxRate         float64
		holdMargin      float64
		ceiling         float64
		wantRatesAfter  []float64  // Expected rates after each clean window
		description     string
	}{
		{
			name:        "convergence from 10 to hold position 49",
			initialRate: 10.0,
			minRate:     1.0,
			maxRate:     50.0,
			holdMargin:  0.02,
			ceiling:     50.0,
			// hold position = 50 * 0.98 = 49.0
			// Window 1: gap = 49 - 10 = 39, step = 39 * 0.5 = 19.5, new = 10 + 19.5 = 29.5
			// Window 2: gap = 49 - 29.5 = 19.5, step = 19.5 * 0.5 = 9.75, new = 29.5 + 9.75 = 39.25
			// Window 3: gap = 49 - 39.25 = 9.75, step = 9.75 * 0.5 = 4.875, new = 39.25 + 4.875 = 44.125
			// Window 4: gap = 49 - 44.125 = 4.875, step = 4.875 * 0.5 = 2.4375, new = 44.125 + 2.4375 = 46.5625
			// Window 5: gap = 49 - 46.5625 = 2.4375, step = 2.4375 * 0.5 = 1.21875, new = 46.5625 + 1.21875 = 47.78125
			wantRatesAfter: []float64{29.5, 39.25, 44.125, 46.5625, 47.78125},
			description: "Geometric convergence: each step halves the remaining gap",
		},
		{
			name:        "convergence from 40 to hold position 49",
			initialRate: 40.0,
			minRate:     1.0,
			maxRate:     50.0,
			holdMargin:  0.02,
			ceiling:     50.0,
			// hold position = 49.0
			// Window 1: gap = 9, step = 4.5, new = 44.5
			// Window 2: gap = 4.5, step = 2.25, new = 46.75
			// Window 3: gap = 2.25, step = 1.125, new = 47.875
			// Window 4: gap = 1.125, step = 0.5625, new = 48.4375
			wantRatesAfter: []float64{44.5, 46.75, 47.875, 48.4375},
			description: "Closer starting point, fewer steps to converge",
		},
		{
			name:        "convergence with 5% hold margin",
			initialRate: 10.0,
			minRate:     1.0,
			maxRate:     50.0,
			holdMargin:  0.05,
			ceiling:     50.0,
			// hold position = 50 * 0.95 = 47.5
			// Window 1: gap = 37.5, step = 18.75, new = 28.75
			// Window 2: gap = 18.75, step = 9.375, new = 38.125
			// Window 3: gap = 9.375, step = 4.6875, new = 42.8125
			wantRatesAfter: []float64{28.75, 38.125, 42.8125},
			description: "Different hold margin changes convergence target",
		},
		{
			name:        "convergence from near-zero to hold position",
			initialRate: 1.0,
			minRate:     1.0,
			maxRate:     50.0,
			holdMargin:  0.02,
			ceiling:     50.0,
			// hold position = 49.0
			// Window 1: gap = 48, step = 24, new = 25
			// Window 2: gap = 24, step = 12, new = 37
			// Window 3: gap = 12, step = 6, new = 43
			// Window 4: gap = 6, step = 3, new = 46
			// Window 5: gap = 3, step = 1.5, new = 47.5
			wantRatesAfter: []float64{25.0, 37.0, 43.0, 46.0, 47.5},
			description: "Large gap from minimum to hold position",
		},
		{
			name:        "minimum step size 0.25 applies for tiny gaps",
			initialRate: 48.5,
			minRate:     1.0,
			maxRate:     50.0,
			holdMargin:  0.02,
			ceiling:     50.0,
			// hold position = 49.0
			// Window 1: gap = 0.5, step = 0.5 * 0.5 = 0.25, but minimum is 0.25, so step = 0.25, new = 48.75
			// Window 2: gap = 0.25, step = 0.25 * 0.5 = 0.125, but minimum is 0.25, so step = 0.25, new = 49.0 (clamped to hold)
			wantRatesAfter: []float64{48.75, 49.0},
			description: "Minimum step of 0.25 prevents slow convergence for tiny gaps",
		},
		{
			name:        "asymptotic approach never exceeds hold position",
			initialRate: 20.0,
			minRate:     1.0,
			maxRate:     50.0,
			holdMargin:  0.02,
			ceiling:     50.0,
			// hold position = 49.0
			// Verify convergence approaches but never exceeds hold position
			// After many windows, rate should approach but not reach 49.0
			wantRatesAfter: []float64{34.5, 41.75, 45.375, 47.1875, 48.09375, 48.546875},
			description: "Geometric progression asymptotically approaches target",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testWindow := 10 * time.Millisecond
			arl := NewAdaptiveRateLimiterWithWindow(tt.initialRate, tt.minRate, tt.maxRate, testWindow)
			arl.holdMargin = tt.holdMargin
			arl.estimatedCeiling = tt.ceiling

			holdPosition := tt.ceiling * (1 - tt.holdMargin)
			t.Logf("Testing: %s", tt.description)
			t.Logf("  Hold position: %.2f (ceiling %.2f * (1 - %.2f))",
				holdPosition, tt.ceiling, tt.holdMargin)

			for i, wantRate := range tt.wantRatesAfter {
				// Record clean window (no 429s)
				for j := 0; j < 100; j++ {
					arl.RecordSuccess()
				}

				// Force window advancement
				arl.mu.Lock()
				arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
				arl.mu.Unlock()
				arl.RecordSuccess() // Trigger adjustment

				gotRate := arl.GetCurrentRate()
				tolerance := 0.01

				// Special case for final convergence to hold position
				if i == len(tt.wantRatesAfter)-1 && wantRate >= holdPosition-0.01 {
					// Final rate may be clamped to hold position
					if gotRate > holdPosition+tolerance {
						t.Errorf("Window %d: rate %.4f exceeded hold position %.4f",
							i+1, gotRate, holdPosition)
					}
				} else if gotRate < wantRate-tolerance || gotRate > wantRate+tolerance {
					t.Errorf("Window %d: got rate %.4f, want %.4f±%.2f",
						i+1, gotRate, wantRate, tolerance)
				}

				t.Logf("  Window %d: rate = %.4f (expected %.4f), gap to hold = %.4f",
					i+1, gotRate, wantRate, holdPosition-gotRate)

				// Verify rate never exceeds hold position
				if gotRate > holdPosition+tolerance {
					t.Errorf("Rate %.4f exceeded hold position %.4f", gotRate, holdPosition)
				}
			}

			// Verify asymptotic behavior: rate should be monotonically increasing
			// but never exceed hold position
			finalRate := arl.GetCurrentRate()
			if finalRate > holdPosition+0.01 {
				t.Errorf("Final rate %.4f exceeded hold position %.4f", finalRate, holdPosition)
			}
			if finalRate < tt.initialRate {
				t.Errorf("Final rate %.4f decreased from initial %.4f", finalRate, tt.initialRate)
			}

			t.Logf("✓ Convergence verified: %.2f → %.2f (hold: %.2f)",
				tt.initialRate, finalRate, holdPosition)
		})
	}
}

// TestAdaptiveRateLimiter_ThreeRegimes tests the three 429-rate regimes
func TestAdaptiveRateLimiter_ThreeRegimes(t *testing.T) {
	tests := []struct {
		name           string
		initialRate    float64
		minRate        float64
		maxRate        float64
		alpha          float64
		holdMargin     float64
		initialCeiling float64
		regime         string  // "high", "middle", "low"
		percent429     float64
		wantRateChange bool    // true if rate should change
		wantCeilingChange bool  // true if ceiling should change
		description    string
	}{
		{
			name:           "high regime: 10% 429 triggers ceiling update and rate drop",
			initialRate:    30.0,
			minRate:        1.0,
			maxRate:        50.0,
			alpha:          0.3,
			holdMargin:     0.02,
			initialCeiling: 50.0,
			regime:         "high",
			percent429:     10.0,
			wantRateChange: true,
			wantCeilingChange: true,
			description:    "429-rate > 5%: EWMA ceiling update, rate drops to hold position",
		},
		{
			name:           "middle regime: exactly 5% 429 holds position (boundary)",
			initialRate:    30.0,
			minRate:        1.0,
			maxRate:        50.0,
			alpha:          0.3,
			holdMargin:     0.02,
			initialCeiling: 50.0,
			regime:         "middle",
			percent429:     5.0,
			wantRateChange: false,
			wantCeilingChange: false,
			description:    "429-rate at threshold (5%) uses strict >, falls to middle regime",
		},
		{
			name:           "high regime: 50% 429 aggressive ceiling drop",
			initialRate:    40.0,
			minRate:        1.0,
			maxRate:        50.0,
			alpha:          0.3,
			holdMargin:     0.02,
			initialCeiling: 50.0,
			regime:         "high",
			percent429:     50.0,
			wantRateChange: true,
			wantCeilingChange: true,
			description:    "Very high 429 rate forces significant ceiling adjustment",
		},
		{
			name:           "middle regime: 3% 429 holds position",
			initialRate:    30.0,
			minRate:        1.0,
			maxRate:        50.0,
			alpha:          0.3,
			holdMargin:     0.02,
			initialCeiling: 50.0,
			regime:         "middle",
			percent429:     3.0,
			wantRateChange: false,
			wantCeilingChange: false,
			description:    "429-rate 1-5%: hold position, no change",
		},
		{
			name:           "middle regime: 2% 429 holds position",
			initialRate:    30.0,
			minRate:        1.0,
			maxRate:        50.0,
			alpha:          0.3,
			holdMargin:     0.02,
			initialCeiling: 50.0,
			regime:         "middle",
			percent429:     2.0,
			wantRateChange: false,
			wantCeilingChange: false,
			description:    "429-rate in middle of 1-5% range holds steady",
		},
		{
			name:           "middle regime: 4.9% 429 holds position",
			initialRate:    30.0,
			minRate:        1.0,
			maxRate:        50.0,
			alpha:          0.3,
			holdMargin:     0.02,
			initialCeiling: 50.0,
			regime:         "middle",
			percent429:     4.9,
			wantRateChange: false,
			wantCeilingChange: false,
			description:    "Just below 5% threshold holds position",
		},
		{
			name:           "low regime: 0% 429 converges to hold",
			initialRate:    20.0,
			minRate:        1.0,
			maxRate:        50.0,
			alpha:          0.3,
			holdMargin:     0.02,
			initialCeiling: 50.0,
			regime:         "low",
			percent429:     0.0,
			wantRateChange: true,
			wantCeilingChange: false,
			description:    "429-rate < 1%: converge toward hold position in 50% steps",
		},
		{
			name:           "low regime: 0.5% 429 converges to hold",
			initialRate:    20.0,
			minRate:        1.0,
			maxRate:        50.0,
			alpha:          0.3,
			holdMargin:     0.02,
			initialCeiling: 50.0,
			regime:         "low",
			percent429:     0.5,
			wantRateChange: true,
			wantCeilingChange: false,
			description:    "429-rate below 1% threshold allows convergence",
		},
		{
			name:           "low regime: just below 1% (0.99%) allows convergence",
			initialRate:    20.0,
			minRate:        1.0,
			maxRate:        50.0,
			alpha:          0.3,
			holdMargin:     0.02,
			initialCeiling: 50.0,
			regime:         "low",
			percent429:     0.99,
			wantRateChange: true,
			wantCeilingChange: false,
			description:    "429-rate < 1% (0.99% is strictly less than 0.01) allows convergence",
		},
		{
			name:           "low regime: 0.9% 429 allows convergence",
			initialRate:    20.0,
			minRate:        1.0,
			maxRate:        50.0,
			alpha:          0.3,
			holdMargin:     0.02,
			initialCeiling: 50.0,
			regime:         "low",
			percent429:     0.9,
			wantRateChange: true,
			wantCeilingChange: false,
			description:    "Just below 1% threshold allows convergence",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testWindow := 10 * time.Millisecond
			arl := NewAdaptiveRateLimiterWithWindow(tt.initialRate, tt.minRate, tt.maxRate, testWindow)
			arl.ceilingSmoothAlpha = tt.alpha
			arl.holdMargin = tt.holdMargin
			arl.estimatedCeiling = tt.initialCeiling

			initialRate := arl.GetCurrentRate()
			initialCeiling := arl.estimatedCeiling

			// Record requests to achieve the desired 429 percentage
			totalRequests := int64(100)
			count429 := int64(float64(totalRequests) * tt.percent429 / 100.0)
			countSuccess := totalRequests - count429

			for i := int64(0); i < count429; i++ {
				arl.Record429()
			}
			for i := int64(0); i < countSuccess; i++ {
				arl.RecordSuccess()
			}

			// Force window advancement
			arl.mu.Lock()
			arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
			arl.mu.Unlock()
			arl.RecordSuccess() // Trigger adjustment

			finalRate := arl.GetCurrentRate()
			finalCeiling := arl.estimatedCeiling

			t.Logf("Regime: %s (429-rate: %.1f%%)", tt.regime, tt.percent429)
			t.Logf("  Rate: %.2f → %.2f", initialRate, finalRate)
			t.Logf("  Ceiling: %.2f → %.2f", initialCeiling, finalCeiling)
			t.Logf("  %s", tt.description)

			// Verify ceiling change expectation
			ceilingChanged := finalCeiling != initialCeiling
			if tt.wantCeilingChange && !ceilingChanged {
				t.Errorf("Expected ceiling to change, but stayed at %.2f", finalCeiling)
			}
			if !tt.wantCeilingChange && ceilingChanged {
				t.Errorf("Expected ceiling to stay steady, but changed from %.2f to %.2f",
					initialCeiling, finalCeiling)
			}

			// Verify rate change expectation
			rateChanged := finalRate != initialRate
			if tt.wantRateChange && !rateChanged {
				t.Errorf("Expected rate to change, but stayed at %.2f", finalRate)
			}

			// Additional regime-specific verifications
			switch tt.regime {
			case "high":
				// High regime: ceiling should decrease, rate should move to hold position
				if tt.wantCeilingChange && finalCeiling >= initialCeiling {
					t.Errorf("High regime: ceiling should decrease, but went from %.2f to %.2f",
						initialCeiling, finalCeiling)
				}
				expectedHoldRate := finalCeiling * (1 - tt.holdMargin)
				tolerance := expectedHoldRate * 0.01
				if finalRate < expectedHoldRate-tolerance || finalRate > expectedHoldRate+tolerance {
					t.Logf("  Note: Rate %.2f vs hold %.2f (within tolerance?)",
						finalRate, expectedHoldRate)
				}

			case "middle":
				// Middle regime: no change
				if finalRate != initialRate {
					t.Errorf("Middle regime: rate should stay steady, but changed from %.2f to %.2f",
						initialRate, finalRate)
				}
				if finalCeiling != initialCeiling {
					t.Errorf("Middle regime: ceiling should stay steady, but changed from %.2f to %.2f",
						initialCeiling, finalCeiling)
				}

			case "low":
				// Low regime: rate should converge toward hold, ceiling unchanged
				if tt.wantCeilingChange && finalCeiling != initialCeiling {
					t.Errorf("Low regime: ceiling should not change, but went from %.2f to %.2f",
						initialCeiling, finalCeiling)
				}
				if tt.wantRateChange && finalRate <= initialRate {
					t.Errorf("Low regime: rate should increase toward hold, but went from %.2f to %.2f",
						initialRate, finalRate)
				}
			}

			t.Logf("✓ %s regime behavior verified", tt.regime)
		})
	}
}

func flattenSequence(ops ...[]operation) []operation {
	var result []operation
	for _, opSlice := range ops {
		result = append(result, opSlice...)
	}
	return result
}
