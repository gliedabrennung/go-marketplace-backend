package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func NewRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return reg
}

func MetricsHandler(reg *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg})
}

type HTTPMetrics struct {
	duration *prometheus.HistogramVec
	total    *prometheus.CounterVec
}

func NewHTTPMetrics(reg prometheus.Registerer) *HTTPMetrics {
	m := &HTTPMetrics{
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency.",
			Buckets: []float64{.005, .01, .025, .05, .1, .15, .2, .3, .5, .75, 1, 1.5, 2.5, 5},
		}, []string{"method", "route", "status"}),
		total: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "HTTP requests processed.",
		}, []string{"method", "route", "status"}),
	}
	reg.MustRegister(m.duration, m.total)
	return m
}

func (m *HTTPMetrics) Observe(method, route string, status int, d time.Duration) {
	s := strconv.Itoa(status)
	m.duration.WithLabelValues(method, route, s).Observe(d.Seconds())
	m.total.WithLabelValues(method, route, s).Inc()
}

type CommandMetrics struct {
	duration *prometheus.HistogramVec
}

func NewCommandMetrics(reg prometheus.Registerer) *CommandMetrics {
	m := &CommandMetrics{
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "command_duration_seconds",
			Help:    "Application command and query handling latency.",
			Buckets: prometheus.DefBuckets,
		}, []string{"handler", "outcome"}),
	}
	reg.MustRegister(m.duration)
	return m
}

func (m *CommandMetrics) ObserveCommand(name string, d time.Duration, ok bool) {
	outcome := "success"
	if !ok {
		outcome = "error"
	}
	m.duration.WithLabelValues(name, outcome).Observe(d.Seconds())
}

type ExternalMetrics struct {
	duration *prometheus.HistogramVec
	breaker  *prometheus.GaugeVec
}

func NewExternalMetrics(reg prometheus.Registerer) *ExternalMetrics {
	m := &ExternalMetrics{
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "external_call_duration_seconds",
			Help:    "Latency of calls to external providers.",
			Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2, 5},
		}, []string{"provider", "operation", "outcome"}),
		breaker: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "circuit_breaker_state",
			Help: "Circuit breaker state per provider: 0 closed, 1 half-open, 2 open.",
		}, []string{"provider"}),
	}
	reg.MustRegister(m.duration, m.breaker)
	return m
}

func (m *ExternalMetrics) ObserveCall(provider, operation string, d time.Duration, err error) {
	outcome := "success"
	if err != nil {
		outcome = "error"
	}
	m.duration.WithLabelValues(provider, operation, outcome).Observe(d.Seconds())
}

func (m *ExternalMetrics) SetBreakerState(provider string, state float64) {
	m.breaker.WithLabelValues(provider).Set(state)
}

type Counter struct {
	vec *prometheus.CounterVec
}

func NewCounter(reg prometheus.Registerer, name, help string, labels ...string) *Counter {
	c := &Counter{vec: prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help}, labels)}
	reg.MustRegister(c.vec)
	return c
}

func (c *Counter) Inc(labels ...string) {
	c.vec.WithLabelValues(labels...).Inc()
}

func (c *Counter) Add(value float64, labels ...string) {
	c.vec.WithLabelValues(labels...).Add(value)
}
