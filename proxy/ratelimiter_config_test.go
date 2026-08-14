package main

import (
	"os"
	"testing"

	"git.ardenone.com/jedarden/zai-proxy/proxy/config"
)

// TestEnvCeilingAlphaDefault verifies that GetRateLimitCeilingAlpha returns
// the default value (0.3) when RATE_LIMIT_CEILING_ALPHA env var is unset.
func TestEnvCeilingAlphaDefault(t *testing.T) {
	orig := os.Getenv("RATE_LIMIT_CEILING_ALPHA")
	defer func() {
		if orig != "" {
			os.Setenv("RATE_LIMIT_CEILING_ALPHA", orig)
		} else {
			os.Unsetenv("RATE_LIMIT_CEILING_ALPHA")
		}
	}()

	os.Unsetenv("RATE_LIMIT_CEILING_ALPHA")

	got := config.GetRateLimitCeilingAlpha()
	want := config.DefaultRateLimitCeilingAlpha

	if got != want {
		t.Errorf("GetRateLimitCeilingAlpha() = %v, want default %v", got, want)
	}
}

// TestEnvHoldMarginDefault verifies that GetRateLimitHoldMargin returns
// the default value (0.02) when RATE_LIMIT_HOLD_MARGIN env var is unset.
func TestEnvHoldMarginDefault(t *testing.T) {
	orig := os.Getenv("RATE_LIMIT_HOLD_MARGIN")
	defer func() {
		if orig != "" {
			os.Setenv("RATE_LIMIT_HOLD_MARGIN", orig)
		} else {
			os.Unsetenv("RATE_LIMIT_HOLD_MARGIN")
		}
	}()

	os.Unsetenv("RATE_LIMIT_HOLD_MARGIN")

	got := config.GetRateLimitHoldMargin()
	want := config.DefaultRateLimitHoldMargin

	if got != want {
		t.Errorf("GetRateLimitHoldMargin() = %v, want default %v", got, want)
	}
}

// TestEnvProbeIntervalDefault verifies that GetRateLimitProbeInterval returns
// the default value (10) when RATE_LIMIT_PROBE_INTERVAL env var is unset.
func TestEnvProbeIntervalDefault(t *testing.T) {
	orig := os.Getenv("RATE_LIMIT_PROBE_INTERVAL")
	defer func() {
		if orig != "" {
			os.Setenv("RATE_LIMIT_PROBE_INTERVAL", orig)
		} else {
			os.Unsetenv("RATE_LIMIT_PROBE_INTERVAL")
		}
	}()

	os.Unsetenv("RATE_LIMIT_PROBE_INTERVAL")

	got := config.GetRateLimitProbeInterval()
	want := config.DefaultRateLimitProbeInterval

	if got != want {
		t.Errorf("GetRateLimitProbeInterval() = %v, want default %v", got, want)
	}
}

// TestEnvVarParsing validates that valid numeric values are correctly parsed
// from environment variables and override the config defaults.
func TestEnvVarParsing(t *testing.T) {
	tests := []struct {
		name              string
		envVar            string
		envValue          string
		getter            func() float64
		getterInt         func() int
		wantFloat         float64
		wantInt           int
		parseAsFloat      bool
	}{
		{
			name:         "ceiling alpha valid 0.5",
			envVar:       "RATE_LIMIT_CEILING_ALPHA",
			envValue:     "0.5",
			getter:       config.GetRateLimitCeilingAlpha,
			wantFloat:    0.5,
			parseAsFloat: true,
		},
		{
			name:         "ceiling alpha valid 0.99",
			envVar:       "RATE_LIMIT_CEILING_ALPHA",
			envValue:     "0.99",
			getter:       config.GetRateLimitCeilingAlpha,
			wantFloat:    0.99,
			parseAsFloat: true,
		},
		{
			name:         "ceiling alpha valid 0.001",
			envVar:       "RATE_LIMIT_CEILING_ALPHA",
			envValue:     "0.001",
			getter:       config.GetRateLimitCeilingAlpha,
			wantFloat:    0.001,
			parseAsFloat: true,
		},
		{
			name:         "ceiling alpha at boundary 1",
			envVar:       "RATE_LIMIT_CEILING_ALPHA",
			envValue:     "1.0",
			getter:       config.GetRateLimitCeilingAlpha,
			wantFloat:    1.0,
			parseAsFloat: true,
		},
		{
			name:         "hold margin valid 0.5",
			envVar:       "RATE_LIMIT_HOLD_MARGIN",
			envValue:     "0.5",
			getter:       config.GetRateLimitHoldMargin,
			wantFloat:    0.5,
			parseAsFloat: true,
		},
		{
			name:         "hold margin valid 0.9",
			envVar:       "RATE_LIMIT_HOLD_MARGIN",
			envValue:     "0.9",
			getter:       config.GetRateLimitHoldMargin,
			wantFloat:    0.9,
			parseAsFloat: true,
		},
		{
			name:         "hold margin valid 0.001",
			envVar:       "RATE_LIMIT_HOLD_MARGIN",
			envValue:     "0.001",
			getter:       config.GetRateLimitHoldMargin,
			wantFloat:    0.001,
			parseAsFloat: true,
		},
		{
			name:         "probe interval valid 5",
			envVar:       "RATE_LIMIT_PROBE_INTERVAL",
			envValue:     "5",
			getterInt:    config.GetRateLimitProbeInterval,
			wantInt:      5,
			parseAsFloat: false,
		},
		{
			name:         "probe interval valid 20",
			envVar:       "RATE_LIMIT_PROBE_INTERVAL",
			envValue:     "20",
			getterInt:    config.GetRateLimitProbeInterval,
			wantInt:      20,
			parseAsFloat: false,
		},
		{
			name:         "probe interval valid 100",
			envVar:       "RATE_LIMIT_PROBE_INTERVAL",
			envValue:     "100",
			getterInt:    config.GetRateLimitProbeInterval,
			wantInt:      100,
			parseAsFloat: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := os.Getenv(tt.envVar)
			defer func() {
				if orig != "" {
					os.Setenv(tt.envVar, orig)
				} else {
					os.Unsetenv(tt.envVar)
				}
			}()

			os.Setenv(tt.envVar, tt.envValue)

			if tt.parseAsFloat {
				got := tt.getter()
				if got != tt.wantFloat {
					t.Errorf("got %v, want %v", got, tt.wantFloat)
				}
			} else {
				got := tt.getterInt()
				if got != tt.wantInt {
					t.Errorf("got %v, want %v", got, tt.wantInt)
				}
			}
		})
	}
}

// TestEnvVarInvalid confirms that invalid environment variable values are
// rejected or handled gracefully by falling back to default values.
func TestEnvVarInvalid(t *testing.T) {
	tests := []struct {
		name              string
		envVar            string
		envValue          string
		getter            func() float64
		getterInt         func() int
		wantDefaultFloat  float64
		wantDefaultInt    int
		parseAsFloat      bool
	}{
		// Ceiling alpha invalid cases
		{
			name:             "ceiling alpha exactly 0 (exclusive min)",
			envVar:           "RATE_LIMIT_CEILING_ALPHA",
			envValue:         "0",
			getter:           config.GetRateLimitCeilingAlpha,
			wantDefaultFloat: config.DefaultRateLimitCeilingAlpha,
			parseAsFloat:     true,
		},
		{
			name:             "ceiling alpha negative",
			envVar:           "RATE_LIMIT_CEILING_ALPHA",
			envValue:         "-0.5",
			getter:           config.GetRateLimitCeilingAlpha,
			wantDefaultFloat: config.DefaultRateLimitCeilingAlpha,
			parseAsFloat:     true,
		},
		{
			name:             "ceiling alpha above 1",
			envVar:           "RATE_LIMIT_CEILING_ALPHA",
			envValue:         "1.5",
			getter:           config.GetRateLimitCeilingAlpha,
			wantDefaultFloat: config.DefaultRateLimitCeilingAlpha,
			parseAsFloat:     true,
		},
		{
			name:             "ceiling alpha non-numeric",
			envVar:           "RATE_LIMIT_CEILING_ALPHA",
			envValue:         "invalid",
			getter:           config.GetRateLimitCeilingAlpha,
			wantDefaultFloat: config.DefaultRateLimitCeilingAlpha,
			parseAsFloat:     true,
		},
		{
			name:             "ceiling alpha with whitespace",
			envVar:           "RATE_LIMIT_CEILING_ALPHA",
			envValue:         " 0.5 ",
			getter:           config.GetRateLimitCeilingAlpha,
			wantDefaultFloat: config.DefaultRateLimitCeilingAlpha,
			parseAsFloat:     true,
		},
		// Hold margin invalid cases
		{
			name:             "hold margin exactly 0 (exclusive min)",
			envVar:           "RATE_LIMIT_HOLD_MARGIN",
			envValue:         "0",
			getter:           config.GetRateLimitHoldMargin,
			wantDefaultFloat: config.DefaultRateLimitHoldMargin,
			parseAsFloat:     true,
		},
		{
			name:             "hold margin exactly 1 (exclusive max)",
			envVar:           "RATE_LIMIT_HOLD_MARGIN",
			envValue:         "1.0",
			getter:           config.GetRateLimitHoldMargin,
			wantDefaultFloat: config.DefaultRateLimitHoldMargin,
			parseAsFloat:     true,
		},
		{
			name:             "hold margin negative",
			envVar:           "RATE_LIMIT_HOLD_MARGIN",
			envValue:         "-0.5",
			getter:           config.GetRateLimitHoldMargin,
			wantDefaultFloat: config.DefaultRateLimitHoldMargin,
			parseAsFloat:     true,
		},
		{
			name:             "hold margin above 1",
			envVar:           "RATE_LIMIT_HOLD_MARGIN",
			envValue:         "1.5",
			getter:           config.GetRateLimitHoldMargin,
			wantDefaultFloat: config.DefaultRateLimitHoldMargin,
			parseAsFloat:     true,
		},
		{
			name:             "hold margin non-numeric",
			envVar:           "RATE_LIMIT_HOLD_MARGIN",
			envValue:         "invalid",
			getter:           config.GetRateLimitHoldMargin,
			wantDefaultFloat: config.DefaultRateLimitHoldMargin,
			parseAsFloat:     true,
		},
		// Probe interval invalid cases
		{
			name:            "probe interval zero",
			envVar:          "RATE_LIMIT_PROBE_INTERVAL",
			envValue:        "0",
			getterInt:       config.GetRateLimitProbeInterval,
			wantDefaultInt:  config.DefaultRateLimitProbeInterval,
			parseAsFloat:    false,
		},
		{
			name:            "probe interval negative",
			envVar:          "RATE_LIMIT_PROBE_INTERVAL",
			envValue:        "-5",
			getterInt:       config.GetRateLimitProbeInterval,
			wantDefaultInt:  config.DefaultRateLimitProbeInterval,
			parseAsFloat:    false,
		},
		{
			name:            "probe interval non-numeric",
			envVar:          "RATE_LIMIT_PROBE_INTERVAL",
			envValue:        "invalid",
			getterInt:       config.GetRateLimitProbeInterval,
			wantDefaultInt:  config.DefaultRateLimitProbeInterval,
			parseAsFloat:    false,
		},
		{
			name:            "probe interval float",
			envVar:          "RATE_LIMIT_PROBE_INTERVAL",
			envValue:        "5.5",
			getterInt:       config.GetRateLimitProbeInterval,
			wantDefaultInt:  config.DefaultRateLimitProbeInterval,
			parseAsFloat:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := os.Getenv(tt.envVar)
			defer func() {
				if orig != "" {
					os.Setenv(tt.envVar, orig)
				} else {
					os.Unsetenv(tt.envVar)
				}
			}()

			os.Setenv(tt.envVar, tt.envValue)

			if tt.parseAsFloat {
				got := tt.getter()
				if got != tt.wantDefaultFloat {
					t.Errorf("got %v, want default %v", got, tt.wantDefaultFloat)
				}
			} else {
				got := tt.getterInt()
				if got != tt.wantDefaultInt {
					t.Errorf("got %v, want default %v", got, tt.wantDefaultInt)
				}
			}
		})
	}
}

// TestEnvVarOverride confirms that environment variables correctly override
// the built-in config defaults.
func TestEnvVarOverride(t *testing.T) {
	tests := []struct {
		name              string
		envVar            string
		envValue          string
		getter            func() float64
		getterInt         func() int
		defaultFloat      float64
		defaultInt        int
		parseAsFloat      bool
	}{
		{
			name:         "ceiling alpha overrides default",
			envVar:       "RATE_LIMIT_CEILING_ALPHA",
			envValue:     "0.7",
			getter:       config.GetRateLimitCeilingAlpha,
			defaultFloat: config.DefaultRateLimitCeilingAlpha,
			parseAsFloat: true,
		},
		{
			name:         "hold margin overrides default",
			envVar:       "RATE_LIMIT_HOLD_MARGIN",
			envValue:     "0.15",
			getter:       config.GetRateLimitHoldMargin,
			defaultFloat: config.DefaultRateLimitHoldMargin,
			parseAsFloat: true,
		},
		{
			name:        "probe interval overrides default",
			envVar:      "RATE_LIMIT_PROBE_INTERVAL",
			envValue:    "7",
			getterInt:   config.GetRateLimitProbeInterval,
			defaultInt:  config.DefaultRateLimitProbeInterval,
			parseAsFloat: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := os.Getenv(tt.envVar)
			defer func() {
				if orig != "" {
					os.Setenv(tt.envVar, orig)
				} else {
					os.Unsetenv(tt.envVar)
				}
			}()

			// First verify the default when env var is unset
			os.Unsetenv(tt.envVar)

			if tt.parseAsFloat {
				defaultValue := tt.getter()
				if defaultValue != tt.defaultFloat {
					t.Errorf("default value = %v, want %v", defaultValue, tt.defaultFloat)
				}
			} else {
				defaultValue := tt.getterInt()
				if defaultValue != tt.defaultInt {
					t.Errorf("default value = %v, want %v", defaultValue, tt.defaultInt)
				}
			}

			// Now set the env var and verify it overrides
			os.Setenv(tt.envVar, tt.envValue)

			if tt.parseAsFloat {
				overrideValue := tt.getter()
				// For this test we're just checking that it's different from default
				if overrideValue == tt.defaultFloat {
					t.Errorf("override value %v should differ from default %v", overrideValue, tt.defaultFloat)
				}
			} else {
				overrideValue := tt.getterInt()
				// For this test we're just checking that it's different from default
				if overrideValue == tt.defaultInt {
					t.Errorf("override value %v should differ from default %v", overrideValue, tt.defaultInt)
				}
			}
		})
	}
}
