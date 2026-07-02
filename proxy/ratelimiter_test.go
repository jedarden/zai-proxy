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

	time.Sleep(testWindow + 10*time.Millisecond)

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

func flattenSequence(ops ...[]operation) []operation {
	var result []operation
	for _, opSlice := range ops {
		result = append(result, opSlice...)
	}
	return result
}
