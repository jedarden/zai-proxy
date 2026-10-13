package main

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestSubgovConcurrencyCounterPublishedAtStartup(t *testing.T) {
	const variant = "subgov-startup"
	NewProxyHandler("test-key", "http://127.0.0.1", 0, 0, variant, nil, "glm-4", 100, 1, 100)
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != "zai_proxy_upstream_concurrency_rejections_total" {
			continue
		}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if label.GetName() == "variant" && label.GetValue() == variant {
					if metric.GetCounter().GetValue() != 0 {
						t.Fatal("startup counter is not zero")
					}
					return
				}
			}
		}
	}
	t.Fatal("initialized concurrency series is absent")
}

func TestSubgovConcurrencyCounterCountsOnly1302(t *testing.T) {
	for _, code := range []string{"1302", "1303", "1308", "1310", "1312", "9999"} {
		t.Run(code, func(t *testing.T) {
			name := "subgov-metric-" + code
			fixture := newClassRetryFixture(t, name, 2, func(w http.ResponseWriter, _ int) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprintf(w, `{"error":{"code":"%s","message":"fixture"}}`, code)
			})
			fixture.do(t)
			want := float64(0)
			if code == "1302" {
				want = float64(fixture.upstreamCalls.Load())
				if want < 2 {
					t.Fatal("fixture did not exercise a retry")
				}
			}
			if got := testutil.ToFloat64(upstreamConcurrencyRejections.WithLabelValues("class-" + name)); got != want {
				t.Fatalf("counter = %v, want %v", got, want)
			}
		})
	}
}
