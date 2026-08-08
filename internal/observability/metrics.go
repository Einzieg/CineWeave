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

	mcpAuthentication = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cineweave",
		Subsystem: "mcp",
		Name:      "authentication_total",
		Help:      "CineWeave MCP authentication outcomes.",
	}, []string{"result"})
	mcpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cineweave",
		Subsystem: "mcp",
		Name:      "requests_total",
		Help:      "CineWeave MCP requests by protocol operation and result.",
	}, []string{"operation", "result"})
	mcpRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cineweave",
		Subsystem: "mcp",
		Name:      "request_duration_seconds",
		Help:      "CineWeave MCP request latency by protocol operation and result.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"operation", "result"})
	mcpToolCalls = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cineweave",
		Subsystem: "mcp",
		Name:      "tool_calls_total",
		Help:      "CineWeave MCP tool calls by action, controller, and result.",
	}, []string{"action", "controller", "result"})
	mcpToolDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cineweave",
		Subsystem: "mcp",
		Name:      "tool_duration_seconds",
		Help:      "CineWeave MCP tool execution latency.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"action", "controller", "result"})
	projectControlCommands = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cineweave",
		Subsystem: "project_control",
		Name:      "commands",
		Help:      "Current Project Control command count by status and controller.",
	}, []string{"status", "controller"})
	projectControlExpiredLeases = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cineweave",
		Subsystem: "project_control",
		Name:      "expired_leases",
		Help:      "Active Project Control commands with an expired lease.",
	})
	projectControlOverdueReconciliations = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cineweave",
		Subsystem: "project_control",
		Name:      "overdue_reconciliations",
		Help:      "Project Control commands overdue for workflow reconciliation.",
	})
	projectControlOldestReconcileLag = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cineweave",
		Subsystem: "project_control",
		Name:      "oldest_reconcile_lag_seconds",
		Help:      "Age of the oldest overdue Project Control reconciliation.",
	})
	projectControlUnlinkedWorkflows = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cineweave",
		Subsystem: "project_control",
		Name:      "unlinked_deterministic_workflows",
		Help:      "Deterministic child workflows missing a Project Control workflow link.",
	})
	projectControlDispatchClaims = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cineweave",
		Subsystem: "project_control",
		Name:      "dispatch_claims_total",
		Help:      "Project Control dispatcher claims and lease takeovers.",
	}, []string{"action", "result", "reclaimed"})
	projectControlDispatchAttempts = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cineweave",
		Subsystem: "project_control",
		Name:      "dispatch_attempt_number",
		Help:      "Project Control dispatch attempt number observed per action.",
		Buckets:   []float64{1, 2, 3, 4, 5, 8, 13},
	}, []string{"action"})
	projectControlDispatchDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cineweave",
		Subsystem: "project_control",
		Name:      "dispatch_duration_seconds",
		Help:      "Project Control dispatcher execution latency.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"action", "result"})
	projectControlReconciliations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cineweave",
		Subsystem: "project_control",
		Name:      "reconciliations_total",
		Help:      "Project Control reconciliation outcomes.",
	}, []string{"result"})
	projectControlReconcileDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cineweave",
		Subsystem: "project_control",
		Name:      "reconcile_duration_seconds",
		Help:      "Project Control reconciliation latency.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"result"})
	projectControlCorrections = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cineweave",
		Subsystem: "project_control",
		Name:      "reconcile_corrections_total",
		Help:      "Project Control reconciliation state corrections.",
	}, []string{"kind"})
	projectControlWaits = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cineweave",
		Subsystem: "project_control",
		Name:      "command_wait_total",
		Help:      "Project Control command wait outcomes.",
	}, []string{"result"})
	projectControlWaitDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cineweave",
		Subsystem: "project_control",
		Name:      "command_wait_duration_seconds",
		Help:      "Project Control command wait latency.",
		Buckets:   []float64{0.01, 0.1, 0.5, 1, 2, 5, 10, 20, 30, 45, 60},
	}, []string{"result"})
	projectControlTerminalDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cineweave",
		Subsystem: "project_control",
		Name:      "terminal_duration_seconds",
		Help:      "Elapsed time from Project Control command creation to terminal state.",
		Buckets:   []float64{0.1, 1, 5, 15, 30, 60, 300, 900, 1800, 4200, 7200},
	}, []string{"action", "controller", "status"})
	projectControlConflicts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cineweave",
		Subsystem: "project_control",
		Name:      "conflicts_total",
		Help:      "Project Control idempotency replay, idempotency conflict, and revision conflict outcomes.",
	}, []string{"kind"})
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
		mcpAuthentication,
		mcpRequests,
		mcpRequestDuration,
		mcpToolCalls,
		mcpToolDuration,
		projectControlCommands,
		projectControlExpiredLeases,
		projectControlOverdueReconciliations,
		projectControlOldestReconcileLag,
		projectControlUnlinkedWorkflows,
		projectControlDispatchClaims,
		projectControlDispatchAttempts,
		projectControlDispatchDuration,
		projectControlReconciliations,
		projectControlReconcileDuration,
		projectControlCorrections,
		projectControlWaits,
		projectControlWaitDuration,
		projectControlTerminalDuration,
		projectControlConflicts,
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

func RecordMCPAuthentication(result string) {
	mcpAuthentication.WithLabelValues(metricLabel(result)).Inc()
}

func RecordMCPRequest(operation, result string, duration time.Duration) {
	operation = metricLabel(operation)
	result = metricLabel(result)
	mcpRequests.WithLabelValues(operation, result).Inc()
	mcpRequestDuration.WithLabelValues(operation, result).Observe(nonNegative(duration.Seconds()))
}

func RecordMCPTool(action, controller, result string, duration time.Duration) {
	action = metricLabel(action)
	controller = metricLabel(controller)
	result = metricLabel(result)
	mcpToolCalls.WithLabelValues(action, controller, result).Inc()
	mcpToolDuration.WithLabelValues(action, controller, result).Observe(nonNegative(duration.Seconds()))
}

type ProjectControlCommandCount struct {
	Status     string
	Controller string
	Count      int64
}

type ProjectControlRuntimeSnapshot struct {
	CommandCounts                  []ProjectControlCommandCount
	ExpiredLeases                  int64
	OverdueReconciliations         int64
	OldestReconcileLagSeconds      float64
	UnlinkedDeterministicWorkflows int64
}

func SetProjectControlRuntime(snapshot ProjectControlRuntimeSnapshot) {
	projectControlCommands.Reset()
	for _, count := range snapshot.CommandCounts {
		projectControlCommands.WithLabelValues(
			metricLabel(count.Status), metricLabel(count.Controller),
		).Set(float64(count.Count))
	}
	projectControlExpiredLeases.Set(float64(snapshot.ExpiredLeases))
	projectControlOverdueReconciliations.Set(float64(snapshot.OverdueReconciliations))
	projectControlOldestReconcileLag.Set(nonNegative(snapshot.OldestReconcileLagSeconds))
	projectControlUnlinkedWorkflows.Set(float64(snapshot.UnlinkedDeterministicWorkflows))
}

func RecordProjectControlDispatch(action, result string, reclaimed bool, attempt int, duration time.Duration) {
	action = metricLabel(action)
	result = metricLabel(result)
	projectControlDispatchClaims.WithLabelValues(action, result, strconv.FormatBool(reclaimed)).Inc()
	if attempt > 0 {
		projectControlDispatchAttempts.WithLabelValues(action).Observe(float64(attempt))
	}
	projectControlDispatchDuration.WithLabelValues(action, result).Observe(nonNegative(duration.Seconds()))
}

func RecordProjectControlReconcile(result string, duration time.Duration) {
	result = metricLabel(result)
	projectControlReconciliations.WithLabelValues(result).Inc()
	projectControlReconcileDuration.WithLabelValues(result).Observe(nonNegative(duration.Seconds()))
}

func RecordProjectControlCorrection(kind string) {
	projectControlCorrections.WithLabelValues(metricLabel(kind)).Inc()
}

func RecordProjectControlWait(result string, duration time.Duration) {
	result = metricLabel(result)
	projectControlWaits.WithLabelValues(result).Inc()
	projectControlWaitDuration.WithLabelValues(result).Observe(nonNegative(duration.Seconds()))
}

func RecordProjectControlTerminal(action, controller, status string, duration time.Duration) {
	projectControlTerminalDuration.WithLabelValues(
		metricLabel(action), metricLabel(controller), metricLabel(status),
	).Observe(nonNegative(duration.Seconds()))
}

func RecordProjectControlConflict(kind string) {
	projectControlConflicts.WithLabelValues(metricLabel(kind)).Inc()
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
