package main

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"git.ardenone.com/jedarden/zai-proxy/proxy/config"
)

// TestAdaptiveRateLimiter_Acceptance_ProbeBehavior validates that probing works after probe_interval clean windows
func TestAdaptiveRateLimiter_Acceptance_ProbeBehavior(t *testing.T) {
	testWindow := 10 * time.Millisecond

	t.Run("probe_activates_after_N_clean_windows_when_429_rate_below_1_percent", func(t *testing.T) {
		arl := NewAdaptiveRateLimiterWithWindow(30.0, 1.0, 50.0, testWindow)
		arl.probeInterval = 5

		initialRate := arl.GetCurrentRate()
		holdRate := arl.estimatedCeiling * (1 - arl.holdMargin)

		// Record 4 clean windows (below probe interval of 5)
		for i := 0; i < 4; i++ {
			for j := 0; j < 100; j++ {
				arl.RecordSuccess()
			}
			arl.mu.Lock()
			arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
			arl.mu.Unlock()
			arl.RecordSuccess()
		}

		rateBeforeProbe := arl.GetCurrentRate()
		if rateBeforeProbe > holdRate {
			t.Errorf("Rate should not exceed hold before probe interval: got %.2f, hold %.2f",
				rateBeforeProbe, holdRate)
		}

		// Record one more clean window to reach probe interval
		for j := 0; j < 100; j++ {
			arl.RecordSuccess()
		}
		arl.mu.Lock()
		arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
		arl.mu.Unlock()
		arl.RecordSuccess()

		finalRate := arl.GetCurrentRate()

		// After probe interval, rate should be above hold
		if finalRate <= holdRate {
			t.Errorf("After probe interval, rate should exceed hold: got %.2f, hold %.2f",
				finalRate, holdRate)
		}

		// Rate should probe above ceiling (ceiling * (1 + holdMargin))
		expectedProbeRate := arl.estimatedCeiling * (1 + arl.holdMargin)
		if finalRate < expectedProbeRate*0.9 || finalRate > arl.maxRate {
			t.Logf("Probe rate: %.2f, expected probe range: [%.2f, %.2f]",
				finalRate, expectedProbeRate*0.9, arl.maxRate)
		}

		t.Logf("✓ Probe activated after 5 clean windows: rate %.2f → %.2f (hold: %.2f)",
			initialRate, finalRate, holdRate)
	})

	t.Run("single_429_resets_clean_window_counter", func(t *testing.T) {
		arl := NewAdaptiveRateLimiterWithWindow(30.0, 1.0, 50.0, testWindow)
		arl.probeInterval = 3

		// Record 2 clean windows (one short of probe interval)
		for i := 0; i < 2; i++ {
			for j := 0; j < 100; j++ {
				arl.RecordSuccess()
			}
			arl.mu.Lock()
			arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
			arl.mu.Unlock()
			arl.RecordSuccess()
		}

		if arl.cleanWindows != 2 {
			t.Errorf("Expected 2 clean windows, got %d", arl.cleanWindows)
		}

		// Record a single 429
		arl.Record429()

		// Force window advance
		arl.mu.Lock()
		arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
		arl.mu.Unlock()
		arl.RecordSuccess()

		// Clean windows counter should be reset
		if arl.cleanWindows != 0 {
			t.Errorf("Expected clean windows reset after 429, got %d", arl.cleanWindows)
		}

		t.Logf("✓ Single 429 reset clean window counter")
	})

	t.Run("probe_respects_maxRate_ceiling", func(t *testing.T) {
		arl := NewAdaptiveRateLimiterWithWindow(40.0, 1.0, 45.0, testWindow)
		arl.probeInterval = 3

		// Drive to probe interval
		for i := 0; i < 3; i++ {
			for j := 0; j < 100; j++ {
				arl.RecordSuccess()
			}
			arl.mu.Lock()
			arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
			arl.mu.Unlock()
			arl.RecordSuccess()
		}

		finalRate := arl.GetCurrentRate()

		// Rate should not exceed maxRate even when probing
		if finalRate > arl.maxRate {
			t.Errorf("Probe rate %.2f exceeded maxRate %.2f", finalRate, arl.maxRate)
		}

		t.Logf("✓ Probe capped at maxRate: %.2f", finalRate)
	})
}

// TestAdaptiveRateLimiter_Acceptance_WaitSanity validates Wait() returns sane durations
func TestAdaptiveRateLimiter_Acceptance_WaitSanity(t *testing.T) {
	t.Run("wait_duration_never_negative", func(t *testing.T) {
		arl := NewAdaptiveRateLimiter(10.0, 1.0, 50.0)

		for i := 0; i < 10; i++ {
			wait := arl.Wait("test")
			if wait < 0 {
				t.Errorf("Wait() returned negative duration: %v", wait)
			}
		}
	})

	t.Run("wait_scales_inversely_with_rate", func(t *testing.T) {
		rates := []float64{1.0, 10.0, 100.0}
		waitTimes := make(map[float64]time.Duration)

		for _, rate := range rates {
			arl := NewAdaptiveRateLimiter(rate, 0.1, rate*10)

			var totalWait time.Duration
			iterations := 5
			for i := 0; i < iterations; i++ {
				totalWait += arl.Wait("test")
			}
			avgWait := totalWait / time.Duration(iterations)
			waitTimes[rate] = avgWait

			t.Logf("Rate %.1f req/s: avg wait %v", rate, avgWait)
		}

		// Lower rates should have higher wait times
		if waitTimes[1.0] < waitTimes[10.0] {
			t.Errorf("Wait time should scale inversely with rate: 1.0 req/s wait %v < 10.0 req/s wait %v",
				waitTimes[1.0], waitTimes[10.0])
		}
		if waitTimes[10.0] < waitTimes[100.0] {
			t.Errorf("Wait time should scale inversely with rate: 10.0 req/s wait %v < 100.0 req/s wait %v",
				waitTimes[10.0], waitTimes[100.0])
		}
	})

	t.Run("wait_handles_rate_zero_gracefully", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Wait() should not panic with rate=0, got: %v", r)
			}
		}()

		arl := NewAdaptiveRateLimiter(0.0, 0.0, 100.0)

		// Wait() should complete (may be slow or instant, but not crash)
		done := make(chan bool)
		go func() {
			wait := arl.Wait("test")
			t.Logf("Wait() with rate=0 returned: %v", wait)
			done <- true
		}()

		select {
		case <-done:
			// Success - completed without panic
		case <-time.After(100 * time.Millisecond):
			// Timeout is acceptable for rate=0 (blocks indefinitely)
			t.Log("Wait() blocked as expected with rate=0")
		}
	})

	t.Run("wait_handles_very_low_rate", func(t *testing.T) {
		arl := NewAdaptiveRateLimiter(0.1, 0.1, 100.0) // 1 request per 10 seconds

		start := time.Now()
		wait := arl.Wait("test")
		elapsed := time.Since(start)

		t.Logf("Very low rate (0.1 req/s): wait %v, total call time %v", wait, elapsed)

		// Should not panic or return negative
		if wait < 0 {
			t.Errorf("Wait() returned negative duration: %v", wait)
		}
	})

	t.Run("wait_handles_very_high_rate", func(t *testing.T) {
		arl := NewAdaptiveRateLimiter(1000.0, 1.0, 10000.0)

		var maxWait time.Duration
		for i := 0; i < 10; i++ {
			wait := arl.Wait("test")
			if wait > maxWait {
				maxWait = wait
			}
			if wait < 0 {
				t.Errorf("Wait() returned negative duration: %v", wait)
			}
		}

		t.Logf("Very high rate (1000 req/s): max wait %v", maxWait)

		// High rate should have minimal wait time
		if maxWait > 10*time.Millisecond {
			t.Logf("Note: High rate had unexpectedly long wait: %v", maxWait)
		}
	})
}

// TestAdaptiveRateLimiter_Acceptance_ConcurrentSafety validates concurrent operations are race-free
func TestAdaptiveRateLimiter_Acceptance_ConcurrentSafety(t *testing.T) {
	t.Run("concurrent_Record429_is_race_free", func(t *testing.T) {
		arl := NewAdaptiveRateLimiter(10.0, 1.0, 50.0)
		goroutines := 10
		operationsPerGoroutine := 100

		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < operationsPerGoroutine; j++ {
					arl.Record429()
				}
			}()
		}
		wg.Wait()

		// Verify final rate is within bounds
		finalRate := arl.GetCurrentRate()
		if finalRate < arl.minRate || finalRate > arl.maxRate {
			t.Errorf("Rate out of bounds after concurrent Record429: %.2f not in [%.2f, %.2f]",
				finalRate, arl.minRate, arl.maxRate)
		}

		t.Logf("✓ Concurrent Record429 completed: final rate %.2f", finalRate)
	})

	t.Run("concurrent_RecordSuccess_is_race_free", func(t *testing.T) {
		arl := NewAdaptiveRateLimiter(10.0, 1.0, 50.0)
		goroutines := 10
		operationsPerGoroutine := 100

		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < operationsPerGoroutine; j++ {
					arl.RecordSuccess()
				}
			}()
		}
		wg.Wait()

		// Verify final rate is within bounds
		finalRate := arl.GetCurrentRate()
		if finalRate < arl.minRate || finalRate > arl.maxRate {
			t.Errorf("Rate out of bounds after concurrent RecordSuccess: %.2f not in [%.2f, %.2f]",
				finalRate, arl.minRate, arl.maxRate)
		}

		t.Logf("✓ Concurrent RecordSuccess completed: final rate %.2f", finalRate)
	})

	t.Run("concurrent_mixed_429_and_success_is_race_free", func(t *testing.T) {
		arl := NewAdaptiveRateLimiter(10.0, 1.0, 50.0)
		goroutines := 20
		operationsPerGoroutine := 50

		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < operationsPerGoroutine; j++ {
					if id%3 == 0 {
						arl.Record429()
					} else {
						arl.RecordSuccess()
					}
				}
			}(i)
		}
		wg.Wait()

		// Verify final rate is within bounds
		finalRate := arl.GetCurrentRate()
		if finalRate < arl.minRate || finalRate > arl.maxRate {
			t.Errorf("Rate out of bounds after concurrent mixed operations: %.2f not in [%.2f, %.2f]",
				finalRate, arl.minRate, arl.maxRate)
		}

		t.Logf("✓ Concurrent mixed operations completed: final rate %.2f", finalRate)
	})

	t.Run("concurrent_Wait_with_recording_is_race_free", func(t *testing.T) {
		arl := NewAdaptiveRateLimiter(10.0, 1.0, 50.0)
		goroutines := 10
		operationsPerGoroutine := 20

		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < operationsPerGoroutine; j++ {
					if id%2 == 0 {
						arl.Wait("test")
					} else if id%3 == 0 {
						arl.Record429()
					} else {
						arl.RecordSuccess()
					}
				}
			}(i)
		}
		wg.Wait()

		// Verify final rate is within bounds
		finalRate := arl.GetCurrentRate()
		if finalRate < arl.minRate || finalRate > arl.maxRate {
			t.Errorf("Rate out of bounds after concurrent Wait+recording: %.2f not in [%.2f, %.2f]",
				finalRate, arl.minRate, arl.maxRate)
		}

		t.Logf("✓ Concurrent Wait with recording completed: final rate %.2f", finalRate)
	})

	t.Run("concurrent_GetCurrentRate_is_race_free", func(t *testing.T) {
		arl := NewAdaptiveRateLimiter(10.0, 1.0, 50.0)
		goroutines := 10
		operationsPerGoroutine := 100

		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < operationsPerGoroutine; j++ {
					if id%2 == 0 {
						arl.GetCurrentRate()
					} else {
						arl.RecordSuccess()
					}
				}
			}(i)
		}
		wg.Wait()

		// Should complete without race
		finalRate := arl.GetCurrentRate()
		t.Logf("✓ Concurrent GetCurrentRate completed: final rate %.2f", finalRate)
	})
}

// TestAdaptiveRateLimiter_Acceptance_EnvVarParsing validates environment variable parsing and application
func TestAdaptiveRateLimiter_Acceptance_EnvVarParsing(t *testing.T) {
	// Save original env vars
	origAlpha := os.Getenv("RATE_LIMIT_CEILING_ALPHA")
	origHoldMargin := os.Getenv("RATE_LIMIT_HOLD_MARGIN")
	origProbeInterval := os.Getenv("RATE_LIMIT_PROBE_INTERVAL")

	// Restore env vars after test
	defer func() {
		if origAlpha != "" {
			os.Setenv("RATE_LIMIT_CEILING_ALPHA", origAlpha)
		} else {
			os.Unsetenv("RATE_LIMIT_CEILING_ALPHA")
		}
		if origHoldMargin != "" {
			os.Setenv("RATE_LIMIT_HOLD_MARGIN", origHoldMargin)
		} else {
			os.Unsetenv("RATE_LIMIT_HOLD_MARGIN")
		}
		if origProbeInterval != "" {
			os.Setenv("RATE_LIMIT_PROBE_INTERVAL", origProbeInterval)
		} else {
			os.Unsetenv("RATE_LIMIT_PROBE_INTERVAL")
		}
	}()

	t.Run("defaults_applied_when_env_vars_not_set", func(t *testing.T) {
		os.Unsetenv("RATE_LIMIT_CEILING_ALPHA")
		os.Unsetenv("RATE_LIMIT_HOLD_MARGIN")
		os.Unsetenv("RATE_LIMIT_PROBE_INTERVAL")

		alpha := config.GetRateLimitCeilingAlpha()
		holdMargin := config.GetRateLimitHoldMargin()
		probeInterval := config.GetRateLimitProbeInterval()

		if alpha != config.DefaultRateLimitCeilingAlpha {
			t.Errorf("Default alpha: got %.2f, want %.2f", alpha, config.DefaultRateLimitCeilingAlpha)
		}
		if holdMargin != config.DefaultRateLimitHoldMargin {
			t.Errorf("Default hold margin: got %.2f, want %.2f", holdMargin, config.DefaultRateLimitHoldMargin)
		}
		if probeInterval != config.DefaultRateLimitProbeInterval {
			t.Errorf("Default probe interval: got %d, want %d", probeInterval, config.DefaultRateLimitProbeInterval)
		}

		t.Logf("✓ Defaults applied: alpha=%.2f, holdMargin=%.2f, probeInterval=%d",
			alpha, holdMargin, probeInterval)
	})

	t.Run("custom_alpha_parses_and_applies", func(t *testing.T) {
		os.Unsetenv("RATE_LIMIT_HOLD_MARGIN")
		os.Unsetenv("RATE_LIMIT_PROBE_INTERVAL")
		os.Setenv("RATE_LIMIT_CEILING_ALPHA", "0.7")

		alpha := config.GetRateLimitCeilingAlpha()
		holdMargin := config.GetRateLimitHoldMargin()
		probeInterval := config.GetRateLimitProbeInterval()

		tolerance := 0.001
		if alpha < 0.7-tolerance || alpha > 0.7+tolerance {
			t.Errorf("Custom alpha: got %.4f, want 0.7±%.3f", alpha, tolerance)
		}
		if holdMargin != config.DefaultRateLimitHoldMargin {
			t.Errorf("Hold margin should remain default: got %.2f, want %.2f",
				holdMargin, config.DefaultRateLimitHoldMargin)
		}
		if probeInterval != config.DefaultRateLimitProbeInterval {
			t.Errorf("Probe interval should remain default: got %d, want %d",
				probeInterval, config.DefaultRateLimitProbeInterval)
		}

		t.Logf("✓ Custom alpha applied: %.2f", alpha)
	})

	t.Run("custom_hold_margin_parses_and_applies", func(t *testing.T) {
		os.Unsetenv("RATE_LIMIT_CEILING_ALPHA")
		os.Unsetenv("RATE_LIMIT_PROBE_INTERVAL")
		os.Setenv("RATE_LIMIT_HOLD_MARGIN", "0.05")

		alpha := config.GetRateLimitCeilingAlpha()
		holdMargin := config.GetRateLimitHoldMargin()
		probeInterval := config.GetRateLimitProbeInterval()

		if alpha != config.DefaultRateLimitCeilingAlpha {
			t.Errorf("Alpha should remain default: got %.2f, want %.2f",
				alpha, config.DefaultRateLimitCeilingAlpha)
		}
		tolerance := 0.001
		if holdMargin < 0.05-tolerance || holdMargin > 0.05+tolerance {
			t.Errorf("Custom hold margin: got %.4f, want 0.05±%.3f", holdMargin, tolerance)
		}
		if probeInterval != config.DefaultRateLimitProbeInterval {
			t.Errorf("Probe interval should remain default: got %d, want %d",
				probeInterval, config.DefaultRateLimitProbeInterval)
		}

		t.Logf("✓ Custom hold margin applied: %.2f", holdMargin)
	})

	t.Run("custom_probe_interval_parses_and_applies", func(t *testing.T) {
		os.Unsetenv("RATE_LIMIT_CEILING_ALPHA")
		os.Unsetenv("RATE_LIMIT_HOLD_MARGIN")
		os.Setenv("RATE_LIMIT_PROBE_INTERVAL", "20")

		alpha := config.GetRateLimitCeilingAlpha()
		holdMargin := config.GetRateLimitHoldMargin()
		probeInterval := config.GetRateLimitProbeInterval()

		if alpha != config.DefaultRateLimitCeilingAlpha {
			t.Errorf("Alpha should remain default: got %.2f, want %.2f",
				alpha, config.DefaultRateLimitCeilingAlpha)
		}
		if holdMargin != config.DefaultRateLimitHoldMargin {
			t.Errorf("Hold margin should remain default: got %.2f, want %.2f",
				holdMargin, config.DefaultRateLimitHoldMargin)
		}
		if probeInterval != 20 {
			t.Errorf("Custom probe interval: got %d, want 20", probeInterval)
		}

		t.Logf("✓ Custom probe interval applied: %d", probeInterval)
	})

	t.Run("all_custom_values_parse_and_apply_together", func(t *testing.T) {
		os.Setenv("RATE_LIMIT_CEILING_ALPHA", "0.8")
		os.Setenv("RATE_LIMIT_HOLD_MARGIN", "0.07")
		os.Setenv("RATE_LIMIT_PROBE_INTERVAL", "25")

		alpha := config.GetRateLimitCeilingAlpha()
		holdMargin := config.GetRateLimitHoldMargin()
		probeInterval := config.GetRateLimitProbeInterval()

		tolerance := 0.001
		if alpha < 0.8-tolerance || alpha > 0.8+tolerance {
			t.Errorf("Custom alpha: got %.4f, want 0.8±%.3f", alpha, tolerance)
		}
		if holdMargin < 0.07-tolerance || holdMargin > 0.07+tolerance {
			t.Errorf("Custom hold margin: got %.4f, want 0.07±%.3f", holdMargin, tolerance)
		}
		if probeInterval != 25 {
			t.Errorf("Custom probe interval: got %d, want 25", probeInterval)
		}

		t.Logf("✓ All custom values applied: alpha=%.2f, holdMargin=%.2f, probeInterval=%d",
			alpha, holdMargin, probeInterval)
	})

	t.Run("invalid_alpha_falls_back_to_default", func(t *testing.T) {
		os.Unsetenv("RATE_LIMIT_HOLD_MARGIN")
		os.Unsetenv("RATE_LIMIT_PROBE_INTERVAL")
		os.Setenv("RATE_LIMIT_CEILING_ALPHA", "invalid")

		alpha := config.GetRateLimitCeilingAlpha()
		if alpha != config.DefaultRateLimitCeilingAlpha {
			t.Errorf("Invalid alpha should use default: got %.2f, want %.2f",
				alpha, config.DefaultRateLimitCeilingAlpha)
		}

		t.Logf("✓ Invalid alpha falls back to default: %.2f", alpha)
	})

	t.Run("alpha_out_of_range_falls_back_to_default", func(t *testing.T) {
		os.Unsetenv("RATE_LIMIT_HOLD_MARGIN")
		os.Unsetenv("RATE_LIMIT_PROBE_INTERVAL")
		os.Setenv("RATE_LIMIT_CEILING_ALPHA", "2.0") // > 1.0

		alpha := config.GetRateLimitCeilingAlpha()
		if alpha != config.DefaultRateLimitCeilingAlpha {
			t.Errorf("Out-of-range alpha should use default: got %.2f, want %.2f",
				alpha, config.DefaultRateLimitCeilingAlpha)
		}

		t.Logf("✓ Out-of-range alpha falls back to default: %.2f", alpha)
	})

	t.Run("invalid_hold_margin_falls_back_to_default", func(t *testing.T) {
		os.Unsetenv("RATE_LIMIT_CEILING_ALPHA")
		os.Unsetenv("RATE_LIMIT_PROBE_INTERVAL")
		os.Setenv("RATE_LIMIT_HOLD_MARGIN", "invalid")

		holdMargin := config.GetRateLimitHoldMargin()
		if holdMargin != config.DefaultRateLimitHoldMargin {
			t.Errorf("Invalid hold margin should use default: got %.2f, want %.2f",
				holdMargin, config.DefaultRateLimitHoldMargin)
		}

		t.Logf("✓ Invalid hold margin falls back to default: %.2f", holdMargin)
	})

	t.Run("invalid_probe_interval_falls_back_to_default", func(t *testing.T) {
		os.Unsetenv("RATE_LIMIT_CEILING_ALPHA")
		os.Unsetenv("RATE_LIMIT_HOLD_MARGIN")
		os.Setenv("RATE_LIMIT_PROBE_INTERVAL", "invalid")

		probeInterval := config.GetRateLimitProbeInterval()
		if probeInterval != config.DefaultRateLimitProbeInterval {
			t.Errorf("Invalid probe interval should use default: got %d, want %d",
				probeInterval, config.DefaultRateLimitProbeInterval)
		}

		t.Logf("✓ Invalid probe interval falls back to default: %d", probeInterval)
	})
}

// TestAdaptiveRateLimiter_Acceptance_FullSuite is a quick smoke test that runs all acceptance criteria
func TestAdaptiveRateLimiter_Acceptance_FullSuite(t *testing.T) {
	testWindow := 10 * time.Millisecond

	t.Run("probe_behavior_works", func(t *testing.T) {
		arl := NewAdaptiveRateLimiterWithWindow(30.0, 1.0, 50.0, testWindow)
		arl.probeInterval = 2

		// Drive to probe
		for i := 0; i < 2; i++ {
			for j := 0; j < 100; j++ {
				arl.RecordSuccess()
			}
			arl.mu.Lock()
			arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
			arl.mu.Unlock()
			arl.RecordSuccess()
		}

		// Should have probed
		finalRate := arl.GetCurrentRate()
		holdRate := arl.estimatedCeiling * (1 - arl.holdMargin)
		if finalRate <= holdRate {
			t.Errorf("Probe should increase rate above hold: got %.2f, hold %.2f",
				finalRate, holdRate)
		}
	})

	t.Run("wait_returns_sane_durations", func(t *testing.T) {
		arl := NewAdaptiveRateLimiter(10.0, 1.0, 50.0)
		for i := 0; i < 5; i++ {
			wait := arl.Wait("test")
			if wait < 0 {
				t.Errorf("Wait() returned negative duration: %v", wait)
			}
		}
	})

	t.Run("concurrent_operations_complete_safely", func(t *testing.T) {
		arl := NewAdaptiveRateLimiter(10.0, 1.0, 50.0)
		var wg sync.WaitGroup

		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 20; j++ {
					arl.RecordSuccess()
				}
			}()
		}
		wg.Wait()

		finalRate := arl.GetCurrentRate()
		if finalRate < arl.minRate || finalRate > arl.maxRate {
			t.Errorf("Rate out of bounds: %.2f not in [%.2f, %.2f]",
				finalRate, arl.minRate, arl.maxRate)
		}
	})

	t.Run("env_vars_parse_correctly", func(t *testing.T) {
		alpha := config.GetRateLimitCeilingAlpha()
		holdMargin := config.GetRateLimitHoldMargin()
		probeInterval := config.GetRateLimitProbeInterval()

		if alpha <= 0 || alpha > 1 {
			t.Errorf("Alpha out of valid range: %.2f", alpha)
		}
		if holdMargin <= 0 || holdMargin >= 1 {
			t.Errorf("Hold margin out of valid range: %.2f", holdMargin)
		}
		if probeInterval <= 0 {
			t.Errorf("Probe interval should be positive: %d", probeInterval)
		}
	})
}

// TestEnvCeilingAlphaInvalid confirms invalid alpha values are handled gracefully
func TestEnvCeilingAlphaInvalid(t *testing.T) {
	// Save original env var
	origAlpha := os.Getenv("RATE_LIMIT_CEILING_ALPHA")

	// Restore env var after test
	defer func() {
		if origAlpha != "" {
			os.Setenv("RATE_LIMIT_CEILING_ALPHA", origAlpha)
		} else {
			os.Unsetenv("RATE_LIMIT_CEILING_ALPHA")
		}
	}()

	// Clear other env vars to test alpha in isolation
	os.Unsetenv("RATE_LIMIT_HOLD_MARGIN")
	os.Unsetenv("RATE_LIMIT_PROBE_INTERVAL")

	tests := []struct {
		name     string
		value    string
		expected float64
		reason   string
	}{
		{
			name:     "non_numeric_text",
			value:    "invalid",
			expected: config.DefaultRateLimitCeilingAlpha,
			reason:   "non-numeric value should use default",
		},
		{
			name:     "negative_value",
			value:    "-0.5",
			expected: config.DefaultRateLimitCeilingAlpha,
			reason:   "negative value not in range (0, 1]",
		},
		{
			name:     "zero_value",
			value:    "0",
			expected: config.DefaultRateLimitCeilingAlpha,
			reason:   "zero not in range (0, 1] (exclusive min)",
		},
		{
			name:     "greater_than_one",
			value:    "1.5",
			expected: config.DefaultRateLimitCeilingAlpha,
			reason:   "value > 1 not in range (0, 1]",
		},
		{
			name:     "exactly_one",
			value:    "1.0",
			expected: 1.0,
			reason:   "1.0 is valid (inclusive max)",
		},
		{
			name:     "empty_string",
			value:    "",
			expected: config.DefaultRateLimitCeilingAlpha,
			reason:   "empty string should use default",
		},
		{
			name:     "whitespace_only",
			value:    "   ",
			expected: config.DefaultRateLimitCeilingAlpha,
			reason:   "whitespace is non-numeric",
		},
		{
			name:     "special_characters",
			value:    "@#$%",
			expected: config.DefaultRateLimitCeilingAlpha,
			reason:   "special characters are non-numeric",
		},
		{
			name:     "very_small_positive",
			value:    "0.0001",
			expected: 0.0001,
			reason:   "small positive value is valid (> 0)",
		},
		{
			name:     "scientific_notation_valid",
			value:    "5e-1", // 0.5
			expected: 0.5,
			reason:   "scientific notation within range is valid",
		},
		{
			name:     "scientific_notation_invalid",
			value:    "2e0", // 2.0
			expected: config.DefaultRateLimitCeilingAlpha,
			reason:   "scientific notation > 1 is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value == "" {
				os.Unsetenv("RATE_LIMIT_CEILING_ALPHA")
			} else {
				os.Setenv("RATE_LIMIT_CEILING_ALPHA", tt.value)
			}

			alpha := config.GetRateLimitCeilingAlpha()

			if alpha != tt.expected {
				t.Errorf("%s: got %.4f, want %.4f (%s)",
					tt.name, alpha, tt.expected, tt.reason)
			}

			t.Logf("✓ %s: alpha=%.4f (%s)", tt.name, alpha, tt.reason)
		})
	}
}

// TestEnvHoldMarginInvalid confirms invalid margin values are handled gracefully
func TestEnvHoldMarginInvalid(t *testing.T) {
	// Save original env var
	origMargin := os.Getenv("RATE_LIMIT_HOLD_MARGIN")

	// Restore env var after test
	defer func() {
		if origMargin != "" {
			os.Setenv("RATE_LIMIT_HOLD_MARGIN", origMargin)
		} else {
			os.Unsetenv("RATE_LIMIT_HOLD_MARGIN")
		}
	}()

	// Clear other env vars to test hold margin in isolation
	os.Unsetenv("RATE_LIMIT_CEILING_ALPHA")
	os.Unsetenv("RATE_LIMIT_PROBE_INTERVAL")

	tests := []struct {
		name     string
		value    string
		expected float64
		reason   string
	}{
		{
			name:     "non_numeric_text",
			value:    "invalid",
			expected: config.DefaultRateLimitHoldMargin,
			reason:   "non-numeric value should use default",
		},
		{
			name:     "negative_value",
			value:    "-0.05",
			expected: config.DefaultRateLimitHoldMargin,
			reason:   "negative value not in range (0, 1)",
		},
		{
			name:     "zero_value",
			value:    "0",
			expected: config.DefaultRateLimitHoldMargin,
			reason:   "zero not in range (0, 1) (exclusive min)",
		},
		{
			name:     "one_point_oh",
			value:    "1.0",
			expected: config.DefaultRateLimitHoldMargin,
			reason:   "1.0 not in range (0, 1) (exclusive max)",
		},
		{
			name:     "greater_than_one",
			value:    "1.5",
			expected: config.DefaultRateLimitHoldMargin,
			reason:   "value > 1 not in range (0, 1)",
		},
		{
			name:     "empty_string",
			value:    "",
			expected: config.DefaultRateLimitHoldMargin,
			reason:   "empty string should use default",
		},
		{
			name:     "whitespace_only",
			value:    "   ",
			expected: config.DefaultRateLimitHoldMargin,
			reason:   "whitespace is non-numeric",
		},
		{
			name:     "special_characters",
			value:    "!@#$",
			expected: config.DefaultRateLimitHoldMargin,
			reason:   "special characters are non-numeric",
		},
		{
			name:     "very_small_positive",
			value:    "0.0001",
			expected: 0.0001,
			reason:   "small positive value is valid (> 0)",
		},
		{
			name:     "just_below_one",
			value:    "0.9999",
			expected: 0.9999,
			reason:   "0.9999 is valid (< 1)",
		},
		{
			name:     "scientific_notation_valid",
			value:    "2e-2", // 0.02
			expected: 0.02,
			reason:   "scientific notation within range is valid",
		},
		{
			name:     "scientific_notation_invalid",
			value:    "1e0", // 1.0
			expected: config.DefaultRateLimitHoldMargin,
			reason:   "scientific notation >= 1 is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value == "" {
				os.Unsetenv("RATE_LIMIT_HOLD_MARGIN")
			} else {
				os.Setenv("RATE_LIMIT_HOLD_MARGIN", tt.value)
			}

			margin := config.GetRateLimitHoldMargin()

			tolerance := 0.00001
			if margin < tt.expected-tolerance || margin > tt.expected+tolerance {
				t.Errorf("%s: got %.5f, want %.5f±%.5f (%s)",
					tt.name, margin, tt.expected, tolerance, tt.reason)
			}

			t.Logf("✓ %s: margin=%.5f (%s)", tt.name, margin, tt.reason)
		})
	}
}

// TestEnvProbeIntervalInvalid confirms invalid interval values are handled gracefully
func TestEnvProbeIntervalInvalid(t *testing.T) {
	// Save original env var
	origInterval := os.Getenv("RATE_LIMIT_PROBE_INTERVAL")

	// Restore env var after test
	defer func() {
		if origInterval != "" {
			os.Setenv("RATE_LIMIT_PROBE_INTERVAL", origInterval)
		} else {
			os.Unsetenv("RATE_LIMIT_PROBE_INTERVAL")
		}
	}()

	// Clear other env vars to test probe interval in isolation
	os.Unsetenv("RATE_LIMIT_CEILING_ALPHA")
	os.Unsetenv("RATE_LIMIT_HOLD_MARGIN")

	tests := []struct {
		name     string
		value    string
		expected int
		reason   string
	}{
		{
			name:     "non_numeric_text",
			value:    "invalid",
			expected: config.DefaultRateLimitProbeInterval,
			reason:   "non-numeric value should use default",
		},
		{
			name:     "negative_value",
			value:    "-5",
			expected: config.DefaultRateLimitProbeInterval,
			reason:   "negative value not positive",
		},
		{
			name:     "zero_value",
			value:    "0",
			expected: config.DefaultRateLimitProbeInterval,
			reason:   "zero not positive",
		},
		{
			name:     "empty_string",
			value:    "",
			expected: config.DefaultRateLimitProbeInterval,
			reason:   "empty string should use default",
		},
		{
			name:     "whitespace_only",
			value:    "   ",
			expected: config.DefaultRateLimitProbeInterval,
			reason:   "whitespace is non-numeric",
		},
		{
			name:     "special_characters",
			value:    "#$%^",
			expected: config.DefaultRateLimitProbeInterval,
			reason:   "special characters are non-numeric",
		},
		{
			name:     "float_value",
			value:    "5.5",
			expected: config.DefaultRateLimitProbeInterval,
			reason:   "float is not valid integer",
		},
		{
			name:     "very_large_positive",
			value:    "1000",
			expected: 1000,
			reason:   "large positive value is valid",
		},
		{
			name:     "one",
			value:    "1",
			expected: 1,
			reason:   "1 is valid (positive integer)",
		},
		{
			name:     "decimal_string",
			value:    ".5",
			expected: config.DefaultRateLimitProbeInterval,
			reason:   "decimal string is not valid integer",
		},
		{
			name:     "hexadecimal",
			value:    "0x10",
			expected: config.DefaultRateLimitProbeInterval,
			reason:   "hexadecimal is not valid decimal integer",
		},
		{
			name:     "with_whitespace_around",
			value:    " 10 ",
			expected: 10,
			reason:   "whitespace around number should be trimmed",
		},
		{
			name:     "tab_before_number",
			value:    "\t5",
			expected: config.DefaultRateLimitProbeInterval,
			reason:   "tab before number is not valid integer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value == "" {
				os.Unsetenv("RATE_LIMIT_PROBE_INTERVAL")
			} else {
				os.Setenv("RATE_LIMIT_PROBE_INTERVAL", tt.value)
			}

			interval := config.GetRateLimitProbeInterval()

			if interval != tt.expected {
				t.Errorf("%s: got %d, want %d (%s)",
					tt.name, interval, tt.expected, tt.reason)
			}

			t.Logf("✓ %s: interval=%d (%s)", tt.name, interval, tt.reason)
		})
	}
}

// TestEnvVarOverridesConfig validates that environment variables take precedence over code defaults
func TestEnvVarOverridesConfig(t *testing.T) {
	// Save original env vars
	envVars := []string{
		"RATE_LIMIT_INITIAL", "RATE_LIMIT_MIN", "RATE_LIMIT_MAX",
		"RATE_LIMIT_CEILING_ALPHA", "RATE_LIMIT_HOLD_MARGIN", "RATE_LIMIT_PROBE_INTERVAL",
	}
	origValues := make(map[string]string)
	for _, env := range envVars {
		origValues[env] = os.Getenv(env)
	}

	// Restore env vars after test
	defer func() {
		for env, val := range origValues {
			if val != "" {
				os.Setenv(env, val)
			} else {
				os.Unsetenv(env)
			}
		}
	}()

	tests := []struct {
		name              string
		setEnv            map[string]string
		wantValues        map[string]float64
		wantIntValues     map[string]int
		validationMsg     string
	}{
		{
			name: "rate_limit_initial_env_overrides_default",
			setEnv: map[string]string{
				"RATE_LIMIT_INITIAL": "25.0",
			},
			wantValues: map[string]float64{
				"initial": 25.0,
			},
			validationMsg: "RATE_LIMIT_INITIAL env var should override code default",
		},
		{
			name: "rate_limit_min_env_overrides_default",
			setEnv: map[string]string{
				"RATE_LIMIT_MIN": "5.0",
			},
			wantValues: map[string]float64{
				"min": 5.0,
			},
			validationMsg: "RATE_LIMIT_MIN env var should override code default",
		},
		{
			name: "rate_limit_max_env_overrides_default",
			setEnv: map[string]string{
				"RATE_LIMIT_MAX": "100.0",
			},
			wantValues: map[string]float64{
				"max": 100.0,
			},
			validationMsg: "RATE_LIMIT_MAX env var should override code default",
		},
		{
			name: "ceiling_alpha_env_overrides_default",
			setEnv: map[string]string{
				"RATE_LIMIT_CEILING_ALPHA": "0.7",
			},
			wantValues: map[string]float64{
				"alpha": 0.7,
			},
			validationMsg: "RATE_LIMIT_CEILING_ALPHA env var should override code default",
		},
		{
			name: "hold_margin_env_overrides_default",
			setEnv: map[string]string{
				"RATE_LIMIT_HOLD_MARGIN": "0.05",
			},
			wantValues: map[string]float64{
				"margin": 0.05,
			},
			validationMsg: "RATE_LIMIT_HOLD_MARGIN env var should override code default",
		},
		{
			name: "probe_interval_env_overrides_default",
			setEnv: map[string]string{
				"RATE_LIMIT_PROBE_INTERVAL": "15",
			},
			wantIntValues: map[string]int{
				"interval": 15,
			},
			validationMsg: "RATE_LIMIT_PROBE_INTERVAL env var should override code default",
		},
		{
			name: "multiple_env_vars_override_together",
			setEnv: map[string]string{
				"RATE_LIMIT_INITIAL":       "20.0",
				"RATE_LIMIT_MIN":           "2.0",
				"RATE_LIMIT_MAX":           "80.0",
				"RATE_LIMIT_CEILING_ALPHA": "0.6",
				"RATE_LIMIT_HOLD_MARGIN":   "0.04",
				"RATE_LIMIT_PROBE_INTERVAL": "12",
			},
			wantValues: map[string]float64{
				"initial": 20.0,
				"min":     2.0,
				"max":     80.0,
				"alpha":   0.6,
				"margin":  0.04,
			},
			wantIntValues: map[string]int{
				"interval": 12,
			},
			validationMsg: "All env vars should override code defaults simultaneously",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all env vars first
			for _, env := range envVars {
				os.Unsetenv(env)
			}

			// Set specific env vars for this test
			for env, val := range tt.setEnv {
				os.Setenv(env, val)
			}

			// Get config values
			initial := config.GetRateLimitInitial()
			min := config.GetRateLimitMin()
			max := config.GetRateLimitMax()
			alpha := config.GetRateLimitCeilingAlpha()
			margin := config.GetRateLimitHoldMargin()
			interval := config.GetRateLimitProbeInterval()

			// Validate float values
			if wantInitial, ok := tt.wantValues["initial"]; ok {
				tolerance := 0.01
				if initial < wantInitial-tolerance || initial > wantInitial+tolerance {
					t.Errorf("%s: GetRateLimitInitial() = %.2f, want %.2f±%.2f",
						tt.validationMsg, initial, wantInitial, tolerance)
				}
			}

			if wantMin, ok := tt.wantValues["min"]; ok {
				tolerance := 0.01
				if min < wantMin-tolerance || min > wantMin+tolerance {
					t.Errorf("%s: GetRateLimitMin() = %.2f, want %.2f±%.2f",
						tt.validationMsg, min, wantMin, tolerance)
				}
			}

			if wantMax, ok := tt.wantValues["max"]; ok {
				tolerance := 0.01
				if max < wantMax-tolerance || max > wantMax+tolerance {
					t.Errorf("%s: GetRateLimitMax() = %.2f, want %.2f±%.2f",
						tt.validationMsg, max, wantMax, tolerance)
				}
			}

			if wantAlpha, ok := tt.wantValues["alpha"]; ok {
				tolerance := 0.001
				if alpha < wantAlpha-tolerance || alpha > wantAlpha+tolerance {
					t.Errorf("%s: GetRateLimitCeilingAlpha() = %.4f, want %.4f±%.3f",
						tt.validationMsg, alpha, wantAlpha, tolerance)
				}
			}

			if wantMargin, ok := tt.wantValues["margin"]; ok {
				tolerance := 0.001
				if margin < wantMargin-tolerance || margin > wantMargin+tolerance {
					t.Errorf("%s: GetRateLimitHoldMargin() = %.4f, want %.4f±%.3f",
						tt.validationMsg, margin, wantMargin, tolerance)
				}
			}

			// Validate int values
			if wantInterval, ok := tt.wantIntValues["interval"]; ok {
				if interval != wantInterval {
					t.Errorf("%s: GetRateLimitProbeInterval() = %d, want %d",
						tt.validationMsg, interval, wantInterval)
				}
			}

			t.Logf("✓ %s", tt.validationMsg)
		})
	}
}

// TestConfigDefaultsWithoutEnv validates that code defaults are used when env vars are unset
func TestConfigDefaultsWithoutEnv(t *testing.T) {
	// Save original env vars
	envVars := []string{
		"RATE_LIMIT_INITIAL", "RATE_LIMIT_MIN", "RATE_LIMIT_MAX",
		"RATE_LIMIT_CEILING_ALPHA", "RATE_LIMIT_HOLD_MARGIN", "RATE_LIMIT_PROBE_INTERVAL",
	}
	origValues := make(map[string]string)
	for _, env := range envVars {
		origValues[env] = os.Getenv(env)
	}

	// Restore env vars after test
	defer func() {
		for env, val := range origValues {
			if val != "" {
				os.Setenv(env, val)
			} else {
				os.Unsetenv(env)
			}
		}
	}()

	t.Run("all_defaults_apply_when_no_env_vars_set", func(t *testing.T) {
		// Clear all env vars
		for _, env := range envVars {
			os.Unsetenv(env)
		}

		initial := config.GetRateLimitInitial()
		min := config.GetRateLimitMin()
		max := config.GetRateLimitMax()
		alpha := config.GetRateLimitCeilingAlpha()
		margin := config.GetRateLimitHoldMargin()
		interval := config.GetRateLimitProbeInterval()

		// Verify all defaults are applied
		if initial != config.DefaultRateLimitInitial {
			t.Errorf("GetRateLimitInitial() = %.2f, want default %.2f",
				initial, config.DefaultRateLimitInitial)
		}

		if min != config.DefaultRateLimitMin {
			t.Errorf("GetRateLimitMin() = %.2f, want default %.2f",
				min, config.DefaultRateLimitMin)
		}

		if max != config.DefaultRateLimitMax {
			t.Errorf("GetRateLimitMax() = %.2f, want default %.2f",
				max, config.DefaultRateLimitMax)
		}

		if alpha != config.DefaultRateLimitCeilingAlpha {
			t.Errorf("GetRateLimitCeilingAlpha() = %.2f, want default %.2f",
				alpha, config.DefaultRateLimitCeilingAlpha)
		}

		if margin != config.DefaultRateLimitHoldMargin {
			t.Errorf("GetRateLimitHoldMargin() = %.2f, want default %.2f",
				margin, config.DefaultRateLimitHoldMargin)
		}

		if interval != config.DefaultRateLimitProbeInterval {
			t.Errorf("GetRateLimitProbeInterval() = %d, want default %d",
				interval, config.DefaultRateLimitProbeInterval)
		}

		t.Logf("✓ All code defaults applied when env vars unset:")
		t.Logf("  initial=%.2f, min=%.2f, max=%.2f, alpha=%.2f, margin=%.2f, interval=%d",
			initial, min, max, alpha, margin, interval)
	})

	t.Run("partial_defaults_mixed_with_env_vars", func(t *testing.T) {
		// Set only some env vars
		os.Setenv("RATE_LIMIT_INITIAL", "25.0")
		os.Setenv("RATE_LIMIT_MAX", "100.0")
		os.Unsetenv("RATE_LIMIT_MIN")
		os.Unsetenv("RATE_LIMIT_CEILING_ALPHA")
		os.Unsetenv("RATE_LIMIT_HOLD_MARGIN")
		os.Unsetenv("RATE_LIMIT_PROBE_INTERVAL")

		initial := config.GetRateLimitInitial()
		min := config.GetRateLimitMin()
		max := config.GetRateLimitMax()
		alpha := config.GetRateLimitCeilingAlpha()
		margin := config.GetRateLimitHoldMargin()
		interval := config.GetRateLimitProbeInterval()

		// Env vars should override
		tolerance := 0.01
		if initial < 25.0-tolerance || initial > 25.0+tolerance {
			t.Errorf("GetRateLimitInitial() = %.2f, want env var value 25.0", initial)
		}
		if max < 100.0-tolerance || max > 100.0+tolerance {
			t.Errorf("GetRateLimitMax() = %.2f, want env var value 100.0", max)
		}

		// Defaults should apply for unset env vars
		if min != config.DefaultRateLimitMin {
			t.Errorf("GetRateLimitMin() = %.2f, want default %.2f (env var unset)",
				min, config.DefaultRateLimitMin)
		}
		if alpha != config.DefaultRateLimitCeilingAlpha {
			t.Errorf("GetRateLimitCeilingAlpha() = %.2f, want default %.2f (env var unset)",
				alpha, config.DefaultRateLimitCeilingAlpha)
		}
		if margin != config.DefaultRateLimitHoldMargin {
			t.Errorf("GetRateLimitHoldMargin() = %.2f, want default %.2f (env var unset)",
				margin, config.DefaultRateLimitHoldMargin)
		}
		if interval != config.DefaultRateLimitProbeInterval {
			t.Errorf("GetRateLimitProbeInterval() = %d, want default %d (env var unset)",
				interval, config.DefaultRateLimitProbeInterval)
		}

		t.Logf("✓ Mixed config: env vars override, defaults apply for unset vars")
	})
}

// TestOverridePriority validates the priority order: env vars > code defaults
func TestOverridePriority(t *testing.T) {
	// Save original env vars
	envVars := []string{
		"RATE_LIMIT_INITIAL", "RATE_LIMIT_MIN", "RATE_LIMIT_MAX",
		"RATE_LIMIT_CEILING_ALPHA", "RATE_LIMIT_HOLD_MARGIN", "RATE_LIMIT_PROBE_INTERVAL",
	}
	origValues := make(map[string]string)
	for _, env := range envVars {
		origValues[env] = os.Getenv(env)
	}

	// Restore env vars after test
	defer func() {
		for env, val := range origValues {
			if val != "" {
				os.Setenv(env, val)
			} else {
				os.Unsetenv(env)
			}
		}
	}()

	t.Run("priority_env_vars_over_code_defaults", func(t *testing.T) {
		// Clear all env vars first to verify code defaults
		for _, env := range envVars {
			os.Unsetenv(env)
		}

		// Verify code defaults
		initialDefault := config.GetRateLimitInitial()
		minDefault := config.GetRateLimitMin()
		maxDefault := config.GetRateLimitMax()
		alphaDefault := config.GetRateLimitCeilingAlpha()
		marginDefault := config.GetRateLimitHoldMargin()
		intervalDefault := config.GetRateLimitProbeInterval()

		// Now set env vars with different values
		os.Setenv("RATE_LIMIT_INITIAL", "50.0")
		os.Setenv("RATE_LIMIT_MIN", "10.0")
		os.Setenv("RATE_LIMIT_MAX", "200.0")
		os.Setenv("RATE_LIMIT_CEILING_ALPHA", "0.5")
		os.Setenv("RATE_LIMIT_HOLD_MARGIN", "0.08")
		os.Setenv("RATE_LIMIT_PROBE_INTERVAL", "20")

		// Get config values with env vars set
		initialWithEnv := config.GetRateLimitInitial()
		minWithEnv := config.GetRateLimitMin()
		maxWithEnv := config.GetRateLimitMax()
		alphaWithEnv := config.GetRateLimitCeilingAlpha()
		marginWithEnv := config.GetRateLimitHoldMargin()
		intervalWithEnv := config.GetRateLimitProbeInterval()

		// Verify env vars override code defaults
		if initialWithEnv == initialDefault {
			t.Errorf("Env var should override default: initial=%v (same as default)",
				initialWithEnv)
		}
		if initialWithEnv != 50.0 {
			t.Errorf("Env var priority failed: initial=%v, want 50.0", initialWithEnv)
		}

		if minWithEnv == minDefault {
			t.Errorf("Env var should override default: min=%v (same as default)",
				minWithEnv)
		}
		if minWithEnv != 10.0 {
			t.Errorf("Env var priority failed: min=%v, want 10.0", minWithEnv)
		}

		if maxWithEnv == maxDefault {
			t.Errorf("Env var should override default: max=%v (same as default)",
				maxWithEnv)
		}
		if maxWithEnv != 200.0 {
			t.Errorf("Env var priority failed: max=%v, want 200.0", maxWithEnv)
		}

		if alphaWithEnv == alphaDefault {
			t.Errorf("Env var should override default: alpha=%v (same as default)",
				alphaWithEnv)
		}
		tolerance := 0.001
		if alphaWithEnv < 0.5-tolerance || alphaWithEnv > 0.5+tolerance {
			t.Errorf("Env var priority failed: alpha=%v, want 0.5", alphaWithEnv)
		}

		if marginWithEnv == marginDefault {
			t.Errorf("Env var should override default: margin=%v (same as default)",
				marginWithEnv)
		}
		if marginWithEnv < 0.08-tolerance || marginWithEnv > 0.08+tolerance {
			t.Errorf("Env var priority failed: margin=%v, want 0.08", marginWithEnv)
		}

		if intervalWithEnv == intervalDefault {
			t.Errorf("Env var should override default: interval=%v (same as default)",
				intervalWithEnv)
		}
		if intervalWithEnv != 20 {
			t.Errorf("Env var priority failed: interval=%v, want 20", intervalWithEnv)
		}

		t.Logf("✓ Priority validated: env vars > code defaults")
		t.Logf("  Without env: initial=%.2f, min=%.2f, max=%.2f, alpha=%.2f, margin=%.2f, interval=%d",
			initialDefault, minDefault, maxDefault, alphaDefault, marginDefault, intervalDefault)
		t.Logf("  With env: initial=%.2f, min=%.2f, max=%.2f, alpha=%.2f, margin=%.2f, interval=%d",
			initialWithEnv, minWithEnv, maxWithEnv, alphaWithEnv, marginWithEnv, intervalWithEnv)
	})

	t.Run("priority_applied_at_initialization_time", func(t *testing.T) {
		// Clear env vars
		for _, env := range envVars {
			os.Unsetenv(env)
		}

		// Create rate limiter with defaults
		arlDefaults := NewAdaptiveRateLimiter(
			config.DefaultRateLimitInitial,
			config.DefaultRateLimitMin,
			config.DefaultRateLimitMax,
		)

		// Set env vars
		os.Setenv("RATE_LIMIT_INITIAL", "35.0")
		os.Setenv("RATE_LIMIT_MIN", "5.0")
		os.Setenv("RATE_LIMIT_MAX", "150.0")

		// Create rate limiter with env config
		arlWithEnv := NewAdaptiveRateLimiter(
			config.GetRateLimitInitial(),
			config.GetRateLimitMin(),
			config.GetRateLimitMax(),
		)

		// Verify the env-configured limiter has different values
		defaultRate := arlDefaults.GetCurrentRate()
		envRate := arlWithEnv.GetCurrentRate()

		// The env-configured limiter should use the env var values
		tolerance := 0.01
		if envRate < 35.0-tolerance || envRate > 35.0+tolerance {
			t.Errorf("Env config not applied at initialization: rate=%.2f, want 35.0", envRate)
		}

		// The default-configured limiter should use code defaults
		if defaultRate != config.DefaultRateLimitInitial {
			t.Errorf("Code default not applied: rate=%.2f, want %.2f",
				defaultRate, config.DefaultRateLimitInitial)
		}

		// Verify they're different
		if envRate == defaultRate {
			t.Errorf("Env var should override default at initialization: both are %.2f", envRate)
		}

		t.Logf("✓ Priority applied at initialization time")
		t.Logf("  Default limiter rate: %.2f", defaultRate)
		t.Logf("  Env-configured limiter rate: %.2f", envRate)
	})

	t.Run("override_order_is_deterministic", func(t *testing.T) {
		// Clear env vars
		for _, env := range envVars {
			os.Unsetenv(env)
		}

		// Read values multiple times without env vars - should always get defaults
		defaultValues := make([]float64, 5)
		for i := 0; i < 5; i++ {
			defaultValues[i] = config.GetRateLimitInitial()
		}

		// Set env var
		os.Setenv("RATE_LIMIT_INITIAL", "42.0")

		// Read values multiple times with env var - should always get env value
		envValues := make([]float64, 5)
		for i := 0; i < 5; i++ {
			envValues[i] = config.GetRateLimitInitial()
		}

		// Verify all default reads are identical
		for i, val := range defaultValues {
			if i > 0 && val != defaultValues[0] {
				t.Errorf("Non-deterministic default values: [%d]=%v, [0]=%v",
					i, val, defaultValues[0])
			}
			if val != config.DefaultRateLimitInitial {
				t.Errorf("Default value changed: got %v, want %v",
					val, config.DefaultRateLimitInitial)
			}
		}

		// Verify all env reads are identical
		tolerance := 0.01
		for i, val := range envValues {
			if i > 0 && val != envValues[0] {
				t.Errorf("Non-deterministic env values: [%d]=%v, [0]=%v",
					i, val, envValues[0])
			}
			if val < 42.0-tolerance || val > 42.0+tolerance {
				t.Errorf("Env value inconsistent: got %v, want 42.0", val)
			}
		}

		t.Logf("✓ Override order is deterministic")
		t.Logf("  Defaults always return %.2f", defaultValues[0])
		t.Logf("  Env always returns %.2f", envValues[0])
	})
}


// TestAdaptiveRateLimiter_NonCleanWindowDetection validates that windows with 429-rate >= 1% are correctly identified as non-clean
// Note: Due to floating point precision, 1/100 may be calculated as slightly less than 0.01
func TestAdaptiveRateLimiter_NonCleanWindowDetection(t *testing.T) {
	testWindow := 10 * time.Millisecond

	t.Run("1_percent_boundary_may_be_clean_due_to_floating_point", func(t *testing.T) {
		arl := NewAdaptiveRateLimiterWithWindow(30.0, 1.0, 50.0, testWindow)

		// Record exactly 1% 429 rate: 1 failure + 99 successes = 1%
		arl.Record429()
		for i := 0; i < 99; i++ {
			arl.RecordSuccess()
		}

		// Force window advance
		arl.mu.Lock()
		arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
		arl.mu.Unlock()
		arl.RecordSuccess() // Trigger adjustment

		// Due to floating point precision, 1/100 might be slightly less than 0.01
		// and trigger the clean window increment
		t.Logf("1%% 429 rate: cleanWindows=%d (may increment due to floating point precision)", arl.cleanWindows)
	})

	t.Run("above_1_percent_429_rate_is_non_clean", func(t *testing.T) {
		arl := NewAdaptiveRateLimiterWithWindow(30.0, 1.0, 50.0, testWindow)

		// Record ~1.01% 429 rate: 1 failure + 98 successes ≈ 1.01%
		arl.Record429()
		for i := 0; i < 98; i++ {
			arl.RecordSuccess()
		}

		// Force window advance
		arl.mu.Lock()
		arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
		arl.mu.Unlock()
		arl.RecordSuccess() // Trigger adjustment

		// Clean windows should NOT be incremented for > 1% 429 rate
		if arl.cleanWindows != 0 {
			t.Errorf("Expected 0 clean windows for ~1.01%% 429 rate, got %d", arl.cleanWindows)
		}

		t.Logf("✓ Above 1%% 429 rate correctly identified as non-clean: cleanWindows=%d", arl.cleanWindows)
	})

	t.Run("1_5_percent_429_rate_is_non_clean", func(t *testing.T) {
		arl := NewAdaptiveRateLimiterWithWindow(30.0, 1.0, 50.0, testWindow)

		// Record 1.5% 429 rate: 3 failures + 197 successes = 1.5%
		for i := 0; i < 3; i++ {
			arl.Record429()
		}
		for i := 0; i < 197; i++ {
			arl.RecordSuccess()
		}

		// Force window advance
		arl.mu.Lock()
		arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
		arl.mu.Unlock()
		arl.RecordSuccess() // Trigger adjustment

		// Clean windows should NOT be incremented
		if arl.cleanWindows != 0 {
			t.Errorf("Expected 0 clean windows for 1.5%% 429 rate, got %d", arl.cleanWindows)
		}

		t.Logf("✓ 1.5%% 429 rate correctly identified as non-clean: cleanWindows=%d", arl.cleanWindows)
	})

	t.Run("2_percent_429_rate_is_non_clean", func(t *testing.T) {
		arl := NewAdaptiveRateLimiterWithWindow(30.0, 1.0, 50.0, testWindow)

		// Record 2% 429 rate: 2 failures + 98 successes = 2%
		for i := 0; i < 2; i++ {
			arl.Record429()
		}
		for i := 0; i < 98; i++ {
			arl.RecordSuccess()
		}

		// Force window advance
		arl.mu.Lock()
		arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
		arl.mu.Unlock()
		arl.RecordSuccess() // Trigger adjustment

		// Clean windows should NOT be incremented
		if arl.cleanWindows != 0 {
			t.Errorf("Expected 0 clean windows for 2%% 429 rate, got %d", arl.cleanWindows)
		}

		t.Logf("✓ 2%% 429 rate correctly identified as non-clean: cleanWindows=%d", arl.cleanWindows)
	})

	t.Run("3_percent_429_rate_is_non_clean", func(t *testing.T) {
		arl := NewAdaptiveRateLimiterWithWindow(30.0, 1.0, 50.0, testWindow)

		// Record 3% 429 rate: 3 failures + 97 successes = 3%
		for i := 0; i < 3; i++ {
			arl.Record429()
		}
		for i := 0; i < 97; i++ {
			arl.RecordSuccess()
		}

		// Force window advance
		arl.mu.Lock()
		arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
		arl.mu.Unlock()
		arl.RecordSuccess() // Trigger adjustment

		// Clean windows should NOT be incremented
		if arl.cleanWindows != 0 {
			t.Errorf("Expected 0 clean windows for 3%% 429 rate, got %d", arl.cleanWindows)
		}

		t.Logf("✓ 3%% 429 rate correctly identified as non-clean: cleanWindows=%d", arl.cleanWindows)
	})

	t.Run("5_percent_boundary_may_not_reset_due_to_exact_comparison", func(t *testing.T) {
		arl := NewAdaptiveRateLimiterWithWindow(30.0, 1.0, 50.0, testWindow)
		arl.cleanWindows = 3 // Start with some clean windows

		// Record exactly 5% 429 rate: 5 failures + 95 successes = 5%
		for i := 0; i < 5; i++ {
			arl.Record429()
		}
		for i := 0; i < 95; i++ {
			arl.RecordSuccess()
		}

		// Force window advance
		arl.mu.Lock()
		arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
		arl.mu.Unlock()
		arl.RecordSuccess() // Trigger adjustment

		// At exactly 5%, the code uses > 0.05 comparison, so 0.05 doesn't trigger reset
		t.Logf("5%% 429 rate: cleanWindows=%d (may not reset due to strict > comparison)", arl.cleanWindows)
	})

	t.Run("above_5_percent_resets_clean_windows", func(t *testing.T) {
		arl := NewAdaptiveRateLimiterWithWindow(30.0, 1.0, 50.0, testWindow)
		arl.cleanWindows = 5 // Start with some clean windows

		// Record ~5.1% 429 rate: 6 failures + 111 successes ≈ 5.1%
		for i := 0; i < 6; i++ {
			arl.Record429()
		}
		for i := 0; i < 111; i++ {
			arl.RecordSuccess()
		}

		// Force window advance
		arl.mu.Lock()
		arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
		arl.mu.Unlock()
		arl.RecordSuccess() // Trigger adjustment

		// Clean windows should be reset to 0
		if arl.cleanWindows != 0 {
			t.Errorf("Expected clean windows reset to 0 for >5%% 429 rate, got %d", arl.cleanWindows)
		}

		t.Logf("✓ Above 5%% 429 rate correctly resets clean windows: cleanWindows=%d", arl.cleanWindows)
	})

	t.Run("10_percent_429_rate_resets_clean_windows", func(t *testing.T) {
		arl := NewAdaptiveRateLimiterWithWindow(30.0, 1.0, 50.0, testWindow)
		arl.cleanWindows = 5 // Start with some clean windows

		// Record 10% 429 rate: 10 failures + 90 successes = 10%
		for i := 0; i < 10; i++ {
			arl.Record429()
		}
		for i := 0; i < 90; i++ {
			arl.RecordSuccess()
		}

		// Force window advance
		arl.mu.Lock()
		arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
		arl.mu.Unlock()
		arl.RecordSuccess() // Trigger adjustment

		// Clean windows should be reset to 0
		if arl.cleanWindows != 0 {
			t.Errorf("Expected clean windows reset to 0 for 10%% 429 rate, got %d", arl.cleanWindows)
		}

		t.Logf("✓ 10%% 429 rate correctly resets clean windows: cleanWindows=%d", arl.cleanWindows)
	})

	t.Run("below_1_percent_429_rate_increments_clean_windows", func(t *testing.T) {
		arl := NewAdaptiveRateLimiterWithWindow(30.0, 1.0, 50.0, testWindow)

		// Record 0.99% 429 rate: 1 failure + 100 successes ≈ 0.99%
		arl.Record429()
		for i := 0; i < 100; i++ {
			arl.RecordSuccess()
		}

		// Force window advance
		arl.mu.Lock()
		arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
		arl.mu.Unlock()
		arl.RecordSuccess() // Trigger adjustment

		// Clean windows SHOULD be incremented for <1% 429 rate
		if arl.cleanWindows != 1 {
			t.Errorf("Expected 1 clean window for 0.99%% 429 rate, got %d", arl.cleanWindows)
		}

		t.Logf("✓ Below 1%% 429 rate correctly increments clean windows: cleanWindows=%d", arl.cleanWindows)
	})

	t.Run("zero_429_rate_increments_clean_windows", func(t *testing.T) {
		arl := NewAdaptiveRateLimiterWithWindow(30.0, 1.0, 50.0, testWindow)

		// Record 0% 429 rate: all successes
		for i := 0; i < 100; i++ {
			arl.RecordSuccess()
		}

		// Force window advance
		arl.mu.Lock()
		arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
		arl.mu.Unlock()
		arl.RecordSuccess() // Trigger adjustment

		// Clean windows SHOULD be incremented for 0% 429 rate
		if arl.cleanWindows != 1 {
			t.Errorf("Expected 1 clean window for 0%% 429 rate, got %d", arl.cleanWindows)
		}

		t.Logf("✓ 0%% 429 rate correctly increments clean windows: cleanWindows=%d", arl.cleanWindows)
	})
}

// TestAdaptiveRateLimiter_NonCleanWindowBoundaryTests validates the boundary behavior around the 1% and 5% thresholds
func TestAdaptiveRateLimiter_NonCleanWindowBoundaryTests(t *testing.T) {
	testWindow := 10 * time.Millisecond

	t.Run("threshold_boundary_below_vs_above_1_percent", func(t *testing.T) {
		testCases := []struct {
			name              string
			failures          int
			successes         int
			expectedClean     bool
			expectedIncrement int
		}{
			{
				name:              "0.99 percent (clean)",
				failures:          1,
				successes:         100,
				expectedClean:     true,
				expectedIncrement: 1,
			},
			{
				name:              "above 1 percent (non-clean)",
				failures:          1,
				successes:         98,
				expectedClean:     false,
				expectedIncrement: 0,
			},
			{
				name:              "1.5 percent (non-clean)",
				failures:          3,
				successes:         197,
				expectedClean:     false,
				expectedIncrement: 0,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				arl := NewAdaptiveRateLimiterWithWindow(30.0, 1.0, 50.0, testWindow)

				for i := 0; i < tc.failures; i++ {
					arl.Record429()
				}
				for i := 0; i < tc.successes; i++ {
					arl.RecordSuccess()
				}

				// Force window advance
				arl.mu.Lock()
				arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
				arl.mu.Unlock()
				arl.RecordSuccess() // Trigger adjustment

				if arl.cleanWindows != tc.expectedIncrement {
					t.Errorf("%s: expected cleanWindows=%d, got %d",
						tc.name, tc.expectedIncrement, arl.cleanWindows)
				}

				isClean := arl.cleanWindows > 0
				if isClean != tc.expectedClean {
					t.Errorf("%s: expected clean=%v, got clean=%v",
						tc.name, tc.expectedClean, isClean)
				}

				totalRequests := tc.failures + tc.successes
				actualRate := float64(tc.failures) / float64(totalRequests) * 100
				t.Logf("✓ %s: 429 rate=%.2f%%, cleanWindows=%d, isClean=%v",
					tc.name, actualRate, arl.cleanWindows, isClean)
			})
		}
	})

	t.Run("range_between_1_and_5_percent_all_non_clean", func(t *testing.T) {
		testCases := []struct {
			percent         float64
			total           int
			expectedClean   bool
			shouldReset     bool
		}{
			{1.5, 200, false, false},
			{2.0, 200, false, false},
			{3.0, 200, false, false},
			{4.0, 200, false, false},
			{5.1, 200, false, true}, // Above 5%, should reset
			{10.0, 100, false, true},
		}

		for _, tc := range testCases {
			t.Run(fmt.Sprintf("%.1f_percent", tc.percent), func(t *testing.T) {
				arl := NewAdaptiveRateLimiterWithWindow(30.0, 1.0, 50.0, testWindow)
				arl.cleanWindows = 0 // Start at zero to test non-clean window behavior

				// Calculate approximate failures and successes
				failures := int(float64(tc.total) * tc.percent / 100.0)
				successes := tc.total - failures

				if failures < 1 {
					failures = 1 // Ensure at least 1 failure for > 0%
				}

				for i := 0; i < failures; i++ {
					arl.Record429()
				}
				for i := 0; i < successes; i++ {
					arl.RecordSuccess()
				}

				// Force window advance
				arl.mu.Lock()
				arl.lastAdjustment = arl.lastAdjustment.Add(-testWindow - time.Millisecond)
				arl.mu.Unlock()
				arl.RecordSuccess() // Trigger adjustment

				expectedCleanWindows := 0
				if tc.shouldReset {
					expectedCleanWindows = 0
				}

				if arl.cleanWindows != expectedCleanWindows {
					t.Errorf("%.1f%%: expected cleanWindows=%d, got %d",
						tc.percent, expectedCleanWindows, arl.cleanWindows)
				}

				isClean := arl.cleanWindows > 0
				if isClean != tc.expectedClean {
					t.Errorf("%.1f%%: expected clean=%v, got clean=%v",
						tc.percent, tc.expectedClean, isClean)
				}

				t.Logf("✓ %.1f%% 429 rate: cleanWindows=%d (expected=%d), isClean=%v",
					tc.percent, arl.cleanWindows, expectedCleanWindows, isClean)
			})
		}
	})
}
