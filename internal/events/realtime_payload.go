package events

import (
	"encoding/json"
	"strings"
)

const (
	realtimeIdentifierLimit = 256
	realtimeSummaryLimit    = 2048
)

var agentActivityStringFields = map[string]int{
	"agentTaskId":             realtimeIdentifierLimit,
	"agentStepId":             realtimeIdentifierLimit,
	"sessionId":               realtimeIdentifierLimit,
	"workflowRunId":           realtimeIdentifierLimit,
	"projectControlCommandId": realtimeIdentifierLimit,
	"tool":                    realtimeIdentifierLimit,
	"status":                  realtimeIdentifierLimit,
	"risk":                    realtimeIdentifierLimit,
	"reason":                  realtimeSummaryLimit,
	"summary":                 realtimeSummaryLimit,
	"errorCode":               realtimeIdentifierLimit,
	"errorMessage":            realtimeSummaryLimit,
}

var agentActivityScalarFields = []string{
	"stepIndex",
	"attempt",
	"maxAttempts",
	"progress",
	"retryable",
	"idempotentReplay",
}

var workflowActivityStringFields = map[string]int{
	"workflowRunId":            realtimeIdentifierLimit,
	"workflowType":             realtimeIdentifierLimit,
	"nodeKey":                  realtimeIdentifierLimit,
	"status":                   realtimeIdentifierLimit,
	"errorCode":                realtimeIdentifierLimit,
	"errorMessage":             realtimeSummaryLimit,
	"sourceId":                 realtimeIdentifierLimit,
	"scriptId":                 realtimeIdentifierLimit,
	"scriptVersionId":          realtimeIdentifierLimit,
	"scriptEpisodeId":          realtimeIdentifierLimit,
	"episodeId":                realtimeIdentifierLimit,
	"storyboardShotId":         realtimeIdentifierLimit,
	"shotId":                   realtimeIdentifierLimit,
	"assetId":                  realtimeIdentifierLimit,
	"canonicalAssetId":         realtimeIdentifierLimit,
	"providerModelId":          realtimeIdentifierLimit,
	"providerCallId":           realtimeIdentifierLimit,
	"modelId":                  realtimeIdentifierLimit,
	"rebuildId":                realtimeIdentifierLimit,
	"commerceScriptUnitId":     realtimeIdentifierLimit,
	"commerceProductionRunId":  realtimeIdentifierLimit,
	"commerceStoryboardPlanId": realtimeIdentifierLimit,
	"projectDeletionRequestId": realtimeIdentifierLimit,
	"projectControlCommandId":  realtimeIdentifierLimit,
}

var workflowActivityScalarFields = []string{
	"revision",
	"workflowRevision",
	"progress",
	"retryable",
	"activated",
	"episodeCount",
	"totalItems",
	"completedItems",
	"failedItems",
	"missingItems",
}

// ProjectRealtimePayload removes canonical task and workflow outputs from
// activity notifications. Realtime consumers use the retained identifiers to
// refresh the authoritative task, step, workflow, and node records.
func ProjectRealtimePayload(eventType string, payload json.RawMessage) (json.RawMessage, bool) {
	var stringFields map[string]int
	var scalarFields []string
	switch {
	case isAgentActivityEvent(eventType):
		stringFields = agentActivityStringFields
		scalarFields = agentActivityScalarFields
	case isWorkflowActivityEvent(eventType):
		stringFields = workflowActivityStringFields
		scalarFields = workflowActivityScalarFields
	default:
		return payload, false
	}

	values := map[string]any{}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &values)
	}
	projected := make(map[string]any, len(stringFields)+len(scalarFields)+1)
	for field, limit := range stringFields {
		value, ok := values[field].(string)
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		projected[field] = truncateRealtimeText(value, limit)
	}
	for _, field := range scalarFields {
		value, ok := values[field]
		if !ok || !isRealtimeScalar(value) {
			continue
		}
		projected[field] = value
	}
	projected["detailsAvailable"] = true

	raw, err := json.Marshal(projected)
	if err != nil {
		return json.RawMessage(`{"detailsAvailable":true}`), true
	}
	return raw, true
}

func isAgentActivityEvent(eventType string) bool {
	eventType = strings.TrimSpace(eventType)
	return strings.HasPrefix(eventType, "agent.step.") || strings.HasPrefix(eventType, "agent.task.")
}

func isWorkflowActivityEvent(eventType string) bool {
	eventType = strings.TrimSpace(eventType)
	return strings.HasPrefix(eventType, "workflow.node.") ||
		strings.HasPrefix(eventType, "workflow.run.") ||
		strings.HasPrefix(eventType, "workflow.result.")
}

func isRealtimeScalar(value any) bool {
	switch value.(type) {
	case bool, float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	default:
		return false
	}
}

func truncateRealtimeText(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}
