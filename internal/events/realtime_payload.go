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

// ProjectRealtimePayload removes canonical task snapshots from Agent activity
// notifications. Agent tasks and steps remain the source of truth; realtime
// consumers use these identifiers to refresh the authoritative records.
func ProjectRealtimePayload(eventType string, payload json.RawMessage) (json.RawMessage, bool) {
	if !isAgentActivityEvent(eventType) {
		return payload, false
	}

	values := map[string]any{}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &values)
	}
	projected := make(map[string]any, len(agentActivityStringFields)+len(agentActivityScalarFields)+1)
	for field, limit := range agentActivityStringFields {
		value, ok := values[field].(string)
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		projected[field] = truncateRealtimeText(value, limit)
	}
	for _, field := range agentActivityScalarFields {
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
