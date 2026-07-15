package observability

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cineweave",
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "HTTP requests completed by CineWeave services.",
	}, []string{"service", "method", "status"})
	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cineweave",
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "HTTP request latency by service and method.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"service", "method"})

	providerRequestTransitions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cineweave",
		Subsystem: "provider",
		Name:      "request_transitions_total",
		Help:      "Durable Provider Request state transitions.",
	}, []string{"task_type", "status"})
	providerRequestAttempts = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cineweave",
		Subsystem: "provider",
		Name:      "request_attempt_generation",
		Help:      "Provider Request attempt generation observed at execution time.",
		Buckets:   []float64{1, 2, 3, 4, 5, 8, 13, 21},
	}, []string{"task_type"})
	providerIdempotency = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cineweave",
		Subsystem: "provider",
		Name:      "idempotency_outcomes_total",
		Help:      "Provider logical idempotency outcomes.",
	}, []string{"outcome"})
	providerStreamTerminal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cineweave",
		Subsystem: "provider",
		Name:      "stream_terminal_total",
		Help:      "Provider streaming terminal-mode outcomes.",
	}, []string{"terminal_mode", "result"})
	providerMediaPolicyRejections = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cineweave",
		Subsystem: "provider",
		Name:      "media_policy_rejections_total",
		Help:      "Provider media downloads rejected by the outbound policy.",
	}, []string{"kind", "reason"})
	providerMediaDownloadBytes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cineweave",
		Subsystem: "provider",
		Name:      "media_download_bytes_total",
		Help:      "Bytes accepted from provider media responses.",
	}, []string{"kind"})
	providerMediaRedirects = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cineweave",
		Subsystem: "provider",
		Name:      "media_redirects_total",
		Help:      "Redirect hops followed by provider media downloads.",
	}, []string{"kind"})
	providerRunningRequests = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cineweave",
		Subsystem: "provider",
		Name:      "running_requests",
		Help:      "Provider Requests currently in the running state.",
	})
	providerOldestRunningAge = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cineweave",
		Subsystem: "provider",
		Name:      "oldest_running_request_age_seconds",
		Help:      "Age in seconds of the oldest running Provider Request.",
	})

	workflowStartAttempts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cineweave",
		Subsystem: "workflow",
		Name:      "start_attempts_total",
		Help:      "Workflow Start Outbox execution attempts.",
	}, []string{"result"})
	workflowStartDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cineweave",
		Subsystem: "workflow",
		Name:      "start_duration_seconds",
		Help:      "Workflow Start Outbox execution latency.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"result"})
	workflowOutboxItems = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cineweave",
		Subsystem: "workflow",
		Name:      "start_outbox_items",
		Help:      "Workflow Start Outbox items by active state.",
	}, []string{"status"})
	workflowQueuedAge = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cineweave",
		Subsystem: "workflow",
		Name:      "oldest_queued_age_seconds",
		Help:      "Age in seconds of the oldest queued Workflow Start Outbox item.",
	})
	workflowActiveNodes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cineweave",
		Subsystem: "workflow",
		Name:      "active_nodes",
		Help:      "Node counts belonging to non-terminal Workflow Runs.",
	}, []string{"status"})
	workflowPartialRuns = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cineweave",
		Subsystem: "workflow",
		Name:      "partial_succeeded_runs",
		Help:      "Persisted Workflow Runs in partial_succeeded state.",
	})
	workflowCancellingRuns = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cineweave",
		Subsystem: "workflow",
		Name:      "cancelling_runs",
		Help:      "Workflow Runs currently waiting for cancellation reconciliation.",
	})
	workflowCancellationAge = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cineweave",
		Subsystem: "workflow",
		Name:      "oldest_cancellation_age_seconds",
		Help:      "Age in seconds of the oldest Workflow Run still cancelling.",
	})

	temporalWorkerDeployment = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cineweave",
		Subsystem: "temporal",
		Name:      "worker_deployment_info",
		Help:      "Registered Temporal Worker Deployment release information.",
	}, []string{"deployment", "build_id", "versioning_mode"})
)

func init() {
	prometheus.MustRegister(
		httpRequests,
		httpDuration,
		providerRequestTransitions,
		providerRequestAttempts,
		providerIdempotency,
		providerStreamTerminal,
		providerMediaPolicyRejections,
		providerMediaDownloadBytes,
		providerMediaRedirects,
		providerRunningRequests,
		providerOldestRunningAge,
		workflowStartAttempts,
		workflowStartDuration,
		workflowOutboxItems,
		workflowQueuedAge,
		workflowActiveNodes,
		workflowPartialRuns,
		workflowCancellingRuns,
		workflowCancellationAge,
		temporalWorkerDeployment,
	)
}

func MetricsHandler() http.Handler {
	return promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

func RecordHTTPRequest(service, method string, status int, duration time.Duration) {
	service = metricLabel(service)
	method = strings.ToUpper(metricLabel(method))
	httpRequests.WithLabelValues(service, method, strconv.Itoa(status)).Inc()
	httpDuration.WithLabelValues(service, method).Observe(duration.Seconds())
}

func RecordProviderRequest(taskType, status string, attempt int) {
	providerRequestTransitions.WithLabelValues(metricLabel(taskType), metricLabel(status)).Inc()
	if attempt > 0 {
		providerRequestAttempts.WithLabelValues(metricLabel(taskType)).Observe(float64(attempt))
	}
}

func RecordProviderIdempotency(outcome string) {
	providerIdempotency.WithLabelValues(metricLabel(outcome)).Inc()
}

func RecordProviderStreamTerminal(mode, result string) {
	providerStreamTerminal.WithLabelValues(metricLabel(mode), metricLabel(result)).Inc()
}

func RecordProviderMediaPolicyRejection(kind, reason string) {
	providerMediaPolicyRejections.WithLabelValues(metricLabel(kind), metricLabel(reason)).Inc()
}

func RecordProviderMediaDownload(kind string, bytes int64, redirects int) {
	kind = metricLabel(kind)
	if bytes > 0 {
		providerMediaDownloadBytes.WithLabelValues(kind).Add(float64(bytes))
	}
	if redirects > 0 {
		providerMediaRedirects.WithLabelValues(kind).Add(float64(redirects))
	}
}

func SetProviderRequestRuntime(running int64, oldestAgeSeconds float64) {
	providerRunningRequests.Set(float64(running))
	providerOldestRunningAge.Set(nonNegative(oldestAgeSeconds))
}

func RecordWorkflowStart(result string, duration time.Duration) {
	result = metricLabel(result)
	workflowStartAttempts.WithLabelValues(result).Inc()
	workflowStartDuration.WithLabelValues(result).Observe(duration.Seconds())
}

type WorkflowRuntimeSnapshot struct {
	PendingOutboxItems        int64
	ProcessingOutboxItems     int64
	OldestQueuedAgeSeconds    float64
	QueuedNodes               int64
	RunningNodes              int64
	PartialSucceededRuns      int64
	CancellingRuns            int64
	OldestCancellationSeconds float64
}

func SetWorkflowRuntime(snapshot WorkflowRuntimeSnapshot) {
	workflowOutboxItems.WithLabelValues("pending").Set(float64(snapshot.PendingOutboxItems))
	workflowOutboxItems.WithLabelValues("processing").Set(float64(snapshot.ProcessingOutboxItems))
	workflowQueuedAge.Set(nonNegative(snapshot.OldestQueuedAgeSeconds))
	workflowActiveNodes.WithLabelValues("queued").Set(float64(snapshot.QueuedNodes))
	workflowActiveNodes.WithLabelValues("running").Set(float64(snapshot.RunningNodes))
	workflowPartialRuns.Set(float64(snapshot.PartialSucceededRuns))
	workflowCancellingRuns.Set(float64(snapshot.CancellingRuns))
	workflowCancellationAge.Set(nonNegative(snapshot.OldestCancellationSeconds))
}

func RecordTemporalWorkerDeployment(deployment, buildID, versioningMode string) {
	temporalWorkerDeployment.WithLabelValues(metricLabel(deployment), metricLabel(buildID), metricLabel(versioningMode)).Set(1)
}

func metricLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	if len(value) > 96 {
		return value[:96]
	}
	return value
}

func nonNegative(value float64) float64 {
	if value < 0 {
		return 0
	}
	return value
}
