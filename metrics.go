package uilib

import "github.com/prometheus/client_golang/prometheus"

var Metrics = []prometheus.Collector{
	SessionCreationTime,
}

var (
	SessionCreationTime = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ui_app_lib_session_creation_time",
		Help:    "Total time to create session in ms",
		Buckets: []float64{100, 500, 1000, 5000, 10000},
	}, []string{"session"})
)
