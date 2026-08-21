package main

import (
	"testing"
	"time"
)

func TestAssertRetryTiming(t *testing.T) {
	t.Run("passes when elapsed time includes the retry delay", func(t *testing.T) {
		AssertRetryTiming(t, 1250*time.Millisecond, time.Second)
	})

	t.Run("allows a zero retry delay", func(t *testing.T) {
		AssertRetryTiming(t, 0, 0)
	})
}

func TestValidateRetryTiming(t *testing.T) {
	testCases := []struct {
		name          string
		elapsed       time.Duration
		expectedDelay time.Duration
		wantError     bool
	}{
		{name: "exact delay", elapsed: time.Second, expectedDelay: time.Second},
		{name: "includes request overhead", elapsed: 1100 * time.Millisecond, expectedDelay: time.Second},
		{name: "delay was skipped", elapsed: 999 * time.Millisecond, expectedDelay: time.Second, wantError: true},
		{name: "negative elapsed", elapsed: -time.Millisecond, expectedDelay: time.Second, wantError: true},
		{name: "negative expected delay", elapsed: time.Second, expectedDelay: -time.Millisecond, wantError: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRetryTiming(tc.elapsed, tc.expectedDelay)
			if (err != nil) != tc.wantError {
				t.Fatalf("validateRetryTiming(%v, %v) error = %v, wantError %t", tc.elapsed, tc.expectedDelay, err, tc.wantError)
			}
		})
	}
}

func TestAssertTotalTimeout(t *testing.T) {
	t.Run("passes when elapsed time is within the overall timeout", func(t *testing.T) {
		AssertTotalTimeout(t, 900*time.Millisecond, time.Second)
	})

	t.Run("allows an exact timeout boundary", func(t *testing.T) {
		AssertTotalTimeout(t, time.Second, time.Second)
	})
}

func TestValidateTotalTimeout(t *testing.T) {
	testCases := []struct {
		name      string
		elapsed   time.Duration
		timeout   time.Duration
		wantError bool
	}{
		{name: "within timeout", elapsed: 900 * time.Millisecond, timeout: time.Second},
		{name: "at timeout", elapsed: time.Second, timeout: time.Second},
		{name: "exceeds timeout", elapsed: 1001 * time.Millisecond, timeout: time.Second, wantError: true},
		{name: "negative elapsed", elapsed: -time.Millisecond, timeout: time.Second, wantError: true},
		{name: "negative timeout", elapsed: time.Second, timeout: -time.Millisecond, wantError: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTotalTimeout(tc.elapsed, tc.timeout)
			if (err != nil) != tc.wantError {
				t.Fatalf("validateTotalTimeout(%v, %v) error = %v, wantError %t", tc.elapsed, tc.timeout, err, tc.wantError)
			}
		})
	}
}

func TestAssertExponentialBackoff(t *testing.T) {
	delays := []time.Duration{
		CalculateBackoffDelay(1),
		CalculateBackoffDelay(2),
		CalculateBackoffDelay(3),
		CalculateBackoffDelay(4),
	}

	AssertExponentialBackoff(t, delays)
}

func TestValidateExponentialBackoff(t *testing.T) {
	testCases := []struct {
		name      string
		delays    []time.Duration
		wantError bool
	}{
		{
			name: "canonical production curve",
			delays: []time.Duration{
				time.Second,
				2 * time.Second,
				4 * time.Second,
				8 * time.Second,
			},
		},
		{name: "no delays", delays: nil, wantError: true},
		{name: "only one delay", delays: []time.Duration{time.Second}, wantError: true},
		{
			name: "does not double",
			delays: []time.Duration{
				time.Second,
				3 * time.Second,
			},
			wantError: true,
		},
		{
			name: "wrong initial delay",
			delays: []time.Duration{
				500 * time.Millisecond,
				time.Second,
			},
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateExponentialBackoff(tc.delays)
			if (err != nil) != tc.wantError {
				t.Fatalf("validateExponentialBackoff(%v) error = %v, wantError %t", tc.delays, err, tc.wantError)
			}
		})
	}
}
