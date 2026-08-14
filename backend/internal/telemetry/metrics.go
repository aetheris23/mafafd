package telemetry

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics bundles the Prometheus collectors used across services.
type Metrics struct {
	Reg *prometheus.Registry

	HTTPRequests *prometheus.CounterVec
	HTTPLatency  *prometheus.HistogramVec

	TxIngestedTotal  prometheus.Counter
	TxRejectedTotal  *prometheus.CounterVec
	TxPublishedTotal prometheus.Counter

	TxConsumedTotal prometheus.Counter
	TxDecisionTotal *prometheus.CounterVec
	RuleLatency     *prometheus.HistogramVec

	AICallsTotal  *prometheus.CounterVec
	AILatency     *prometheus.HistogramVec
	AIFallbackTotal prometheus.Counter

	WSClients *prometheus.GaugeVec

	QueueDepth *prometheus.GaugeVec
}

// NewMetrics creates the registry with process + Go collectors.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	m := &Metrics{Reg: reg}
	m.HTTPRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests by method, path, status.",
	}, []string{"method", "path", "status"})
	m.HTTPLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency.",
		Buckets: prometheus.ExponentialBuckets(0.0005, 2, 12), // 0.5ms .. ~1s
	}, []string{"method", "path"})

	m.TxIngestedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "transactions_ingested_total", Help: "Transactions accepted by the ingestion API.",
	})
	m.TxRejectedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "transactions_rejected_total", Help: "Transactions rejected by the ingestion API.",
	}, []string{"reason"})
	m.TxPublishedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "transactions_published_total", Help: "Transactions published to the message broker.",
	})

	m.TxConsumedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "transactions_consumed_total", Help: "Transactions consumed from the message broker.",
	})
	m.TxDecisionTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "transactions_decision_total", Help: "Transactions classified by final risk level.",
	}, []string{"level"})
	m.RuleLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "rule_evaluation_seconds",
		Help:    "Deterministic rule evaluation latency.",
		Buckets: prometheus.ExponentialBuckets(0.0001, 2, 10),
	}, []string{"rule"})

	m.AICallsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ai_calls_total", Help: "AI provider calls.",
	}, []string{"provider", "outcome"})
	m.AILatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ai_call_duration_seconds",
		Help:    "AI provider call latency.",
		Buckets: prometheus.ExponentialBuckets(0.01, 2, 10),
	}, []string{"provider"})
	m.AIFallbackTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ai_fallback_total", Help: "Times the local heuristic fallback was used.",
	})

	m.WSClients = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ws_clients", Help: "Connected WebSocket clients by tenant.",
	}, []string{"tenant"})

	m.QueueDepth = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "queue_depth", Help: "Approximate message queue depth.",
	}, []string{"queue"})

	reg.MustRegister(m.HTTPRequests, m.HTTPLatency, m.TxIngestedTotal, m.TxRejectedTotal,
		m.TxPublishedTotal, m.TxConsumedTotal, m.TxDecisionTotal, m.RuleLatency,
		m.AICallsTotal, m.AILatency, m.AIFallbackTotal, m.WSClients, m.QueueDepth)
	return m
}

// HTTPHandler returns an http.Handler serving Prometheus metrics.
func (m *Metrics) HTTPHandler() http.Handler {
	return promhttp.HandlerFor(m.Reg, promhttp.HandlerOpts{})
}
