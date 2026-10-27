package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics owns every Prometheus collector the service exposes.
type Metrics struct {
	Registry *prometheus.Registry

	RequestsTotal   *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
	InFlight        prometheus.Gauge
	UploadsTotal    *prometheus.CounterVec
	StorageOpsTotal *prometheus.CounterVec
}

func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()

	m := &Metrics{
		Registry: registry,
		RequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests by method, route and status.",
		}, []string{"method", "route", "status"}),
		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency by method and route.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
		InFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "http_in_flight_requests",
			Help: "Number of HTTP requests currently being served.",
		}),
		UploadsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "file_uploads_total",
			Help: "File upload outcomes by status.",
		}, []string{"status"}),
		StorageOpsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "storage_operations_total",
			Help: "Object storage operations by operation and outcome.",
		}, []string{"operation", "status"}),
	}

	registry.MustRegister(
		m.RequestsTotal,
		m.RequestDuration,
		m.InFlight,
		m.UploadsTotal,
		m.StorageOpsTotal,
	)

	// Go runtime and process collectors are useful in every deployment.
	registry.MustRegister(prometheus.NewGoCollector())
	registry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))

	return m
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}

func (m *Metrics) ObserveRequest(method, route string, status int, elapsed time.Duration) {
	m.RequestsTotal.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
	m.RequestDuration.WithLabelValues(method, route).Observe(elapsed.Seconds())
}

func (m *Metrics) ObserveUpload(status string) {
	if m == nil {
		return
	}
	m.UploadsTotal.WithLabelValues(status).Inc()
}

func (m *Metrics) ObserveStorage(operation string, err error) {
	if m == nil {
		return
	}
	status := "success"
	if err != nil {
		status = "error"
	}
	m.StorageOpsTotal.WithLabelValues(operation, status).Inc()
}
