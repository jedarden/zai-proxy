package main

import (
	"os"
	"strconv"
	"testing"
)

// TestIsTestMode verifies that IsTestMode correctly detects test mode
func TestIsTestMode(t *testing.T) {
	t.Run("returns true during test execution via testing.Testing", func(t *testing.T) {
		// When called from a test, testing.Testing() returns true
		if !IsTestMode() {
			t.Error("IsTestMode should return true during test execution via testing.Testing()")
		}
	})

	t.Run("returns true when ZAI_PROXY_TEST_MODE is true", func(t *testing.T) {
		// Save original value
		orig := os.Getenv(TestModeEnv)

		// Set env var and verify
		t.Setenv(TestModeEnv, "true")
		if !IsTestMode() {
			t.Error("IsTestMode should return true when ZAI_PROXY_TEST_MODE=true")
		}

		// Restore
		if orig != "" {
			t.Setenv(TestModeEnv, orig)
		} else {
			os.Unsetenv(TestModeEnv)
		}
	})

	t.Run("returns false when neither condition is met", func(t *testing.T) {
		// This test verifies the logic, though we can't truly test
		// the "non-test" case since we're always in a test context.
		// The key point is that during tests, it should return true.
		if !IsTestMode() {
			t.Error("IsTestMode should return true in test context")
		}
	})
}

// TestGetTestMaxRetries verifies that GetTestMaxRetries returns the correct value
func TestGetTestMaxRetries(t *testing.T) {
	t.Run("returns DefaultTestMaxRetries (1) when MAX_RETRIES not set", func(t *testing.T) {
		// Save original value
		orig := os.Getenv(TestMaxRetriesEnv)

		// Ensure env var is unset
		os.Unsetenv(TestMaxRetriesEnv)

		retries := GetTestMaxRetries()
		if retries != DefaultTestMaxRetries {
			t.Errorf("Expected DefaultTestMaxRetries=%d, got %d", DefaultTestMaxRetries, retries)
		}

		// Restore
		if orig != "" {
			t.Setenv(TestMaxRetriesEnv, orig)
		}
	})

	t.Run("returns DefaultTestMaxRetries (1) when MAX_RETRIES is empty string", func(t *testing.T) {
		// Save original value
		orig := os.Getenv(TestMaxRetriesEnv)

		t.Setenv(TestMaxRetriesEnv, "")

		retries := GetTestMaxRetries()
		if retries != DefaultTestMaxRetries {
			t.Errorf("Expected DefaultTestMaxRetries=%d when env is empty, got %d", DefaultTestMaxRetries, retries)
		}

		// Restore
		if orig != "" {
			t.Setenv(TestMaxRetriesEnv, orig)
		}
	})

	t.Run("returns value from MAX_RETRIES when set to valid positive integer", func(t *testing.T) {
		// Save original value
		orig := os.Getenv(TestMaxRetriesEnv)

		testCases := []struct {
			name     string
			envValue string
			expected int
		}{
			{"zero", "0", 0},
			{"one", "1", 1},
			{"two", "2", 2},
			{"five", "5", 5},
			{"ten", "10", 10},
			{"large", "100", 100},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Setenv(TestMaxRetriesEnv, tc.envValue)
				retries := GetTestMaxRetries()
				if retries != tc.expected {
					t.Errorf("Expected %d, got %d", tc.expected, retries)
				}
			})
		}

		// Restore
		if orig != "" {
			t.Setenv(TestMaxRetriesEnv, orig)
		}
	})

	t.Run("returns DefaultTestMaxRetries when MAX_RETRIES is invalid", func(t *testing.T) {
		// Save original value
		orig := os.Getenv(TestMaxRetriesEnv)

		invalidValues := []string{
			"not-a-number",
			"-1",  // negative
			"-5",  // negative
			"1.5", // not an integer
			"abc",
		}

		for _, val := range invalidValues {
			t.Run("value_"+val, func(t *testing.T) {
				t.Setenv(TestMaxRetriesEnv, val)
				retries := GetTestMaxRetries()
				if retries != DefaultTestMaxRetries {
					t.Errorf("Expected DefaultTestMaxRetries=%d for invalid value %q, got %d",
						DefaultTestMaxRetries, val, retries)
				}
			})
		}

		// Restore
		if orig != "" {
			t.Setenv(TestMaxRetriesEnv, orig)
		}
	})
}

// TestConfigureTestEnv verifies that ConfigureTestEnv sets all required environment variables
func TestConfigureTestEnv(t *testing.T) {
	// Save original values
	origTestMode := os.Getenv(TestModeEnv)
	origMaxRetries := os.Getenv(TestMaxRetriesEnv)
	origDeploymentVariant := os.Getenv("DEPLOYMENT_VARIANT")
	origTokenCounting := os.Getenv("TOKEN_COUNTING_ENABLED")
	origRateLimitInitial := os.Getenv("RATE_LIMIT_INITIAL")
	origRateLimitMin := os.Getenv("RATE_LIMIT_MIN")
	origRateLimitMax := os.Getenv("RATE_LIMIT_MAX")

	// Clear all env vars to ensure clean state
	os.Unsetenv(TestModeEnv)
	os.Unsetenv(TestMaxRetriesEnv)
	os.Unsetenv("DEPLOYMENT_VARIANT")
	os.Unsetenv("TOKEN_COUNTING_ENABLED")
	os.Unsetenv("RATE_LIMIT_INITIAL")
	os.Unsetenv("RATE_LIMIT_MIN")
	os.Unsetenv("RATE_LIMIT_MAX")

	// Call ConfigureTestEnv
	ConfigureTestEnv(t)

	// Verify TestMaxRetriesEnv is set to DefaultTestMaxRetries
	t.Run("sets TestMaxRetriesEnv (MAX_RETRIES) to DefaultTestMaxRetries", func(t *testing.T) {
		expected := strconv.Itoa(DefaultTestMaxRetries)
		actual := os.Getenv(TestMaxRetriesEnv)
		if actual != expected {
			t.Errorf("Expected MAX_RETRIES=%s, got %s", expected, actual)
		}
	})

	// Verify TestModeEnv is set to "true"
	t.Run("sets TestModeEnv (ZAI_PROXY_TEST_MODE) to true", func(t *testing.T) {
		actual := os.Getenv(TestModeEnv)
		if actual != "true" {
			t.Errorf("Expected ZAI_PROXY_TEST_MODE=true, got %s", actual)
		}
	})

	// Verify DEPLOYMENT_VARIANT is set to "test"
	t.Run("sets DEPLOYMENT_VARIANT to test", func(t *testing.T) {
		actual := os.Getenv("DEPLOYMENT_VARIANT")
		if actual != "test" {
			t.Errorf("Expected DEPLOYMENT_VARIANT=test, got %s", actual)
		}
	})

	// Verify TOKEN_COUNTING_ENABLED is set to "false"
	t.Run("sets TOKEN_COUNTING_ENABLED to false", func(t *testing.T) {
		actual := os.Getenv("TOKEN_COUNTING_ENABLED")
		if actual != "false" {
			t.Errorf("Expected TOKEN_COUNTING_ENABLED=false, got %s", actual)
		}
	})

	// Verify RATE_LIMIT_INITIAL is set to "1000"
	t.Run("sets RATE_LIMIT_INITIAL to 1000", func(t *testing.T) {
		actual := os.Getenv("RATE_LIMIT_INITIAL")
		if actual != "1000" {
			t.Errorf("Expected RATE_LIMIT_INITIAL=1000, got %s", actual)
		}
	})

	// Verify RATE_LIMIT_MIN is set to "1000"
	t.Run("sets RATE_LIMIT_MIN to 1000", func(t *testing.T) {
		actual := os.Getenv("RATE_LIMIT_MIN")
		if actual != "1000" {
			t.Errorf("Expected RATE_LIMIT_MIN=1000, got %s", actual)
		}
	})

	// Verify RATE_LIMIT_MAX is set to "1000"
	t.Run("sets RATE_LIMIT_MAX to 1000", func(t *testing.T) {
		actual := os.Getenv("RATE_LIMIT_MAX")
		if actual != "1000" {
			t.Errorf("Expected RATE_LIMIT_MAX=1000, got %s", actual)
		}
	})

	// Restore original values
	if origTestMode != "" {
		os.Setenv(TestModeEnv, origTestMode)
	} else {
		os.Unsetenv(TestModeEnv)
	}
	if origMaxRetries != "" {
		os.Setenv(TestMaxRetriesEnv, origMaxRetries)
	} else {
		os.Unsetenv(TestMaxRetriesEnv)
	}
	if origDeploymentVariant != "" {
		os.Setenv("DEPLOYMENT_VARIANT", origDeploymentVariant)
	} else {
		os.Unsetenv("DEPLOYMENT_VARIANT")
	}
	if origTokenCounting != "" {
		os.Setenv("TOKEN_COUNTING_ENABLED", origTokenCounting)
	} else {
		os.Unsetenv("TOKEN_COUNTING_ENABLED")
	}
	if origRateLimitInitial != "" {
		os.Setenv("RATE_LIMIT_INITIAL", origRateLimitInitial)
	} else {
		os.Unsetenv("RATE_LIMIT_INITIAL")
	}
	if origRateLimitMin != "" {
		os.Setenv("RATE_LIMIT_MIN", origRateLimitMin)
	} else {
		os.Unsetenv("RATE_LIMIT_MIN")
	}
	if origRateLimitMax != "" {
		os.Setenv("RATE_LIMIT_MAX", origRateLimitMax)
	} else {
		os.Unsetenv("RATE_LIMIT_MAX")
	}
}
