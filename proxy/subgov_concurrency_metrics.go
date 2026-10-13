package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// A dedicated counter lets consumers distinguish an observed zero from an
// older proxy that cannot classify provider concurrency rejections. Neither
// provider messages nor arbitrary error codes become metric labels.
var upstreamConcurrencyRejections = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "zai_proxy_upstream_concurrency_rejections_total",
	Help: "Upstream Z.AI code 1302 concurrency rejections, including retries",
}, []string{"variant"})
