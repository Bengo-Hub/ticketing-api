// Package metrics exposes Prometheus metrics: the Go and process collectors from the default
// registry plus HTTP request counts and latencies by method, route pattern and status.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	requests = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "ticketing",
		Name:      "http_requests_total",
		Help:      "HTTP requests by method, route pattern and status code.",
	}, []string{"method", "route", "status"})

	duration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "ticketing",
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request latency by method and route pattern.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "route"})
)

// Handler serves the metrics in the Prometheus text format.
func Handler() http.Handler { return promhttp.Handler() }

// statusRecorder captures the response status.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Flush keeps Server-Sent Events streaming through the recorder.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Middleware records every request. The route label is chi's matched pattern
// (/api/v1/{tenant}/tickets/{ticketId}), never the raw path, so ids do not explode the series.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		route := "unmatched"
		if rc := chi.RouteContext(r.Context()); rc != nil {
			if p := rc.RoutePattern(); p != "" {
				route = p
			}
		}
		requests.WithLabelValues(r.Method, route, strconv.Itoa(rec.status)).Inc()
		duration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
	})
}
