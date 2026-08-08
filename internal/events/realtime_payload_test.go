package events

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProjectRealtimePayloadKeepsAgentActivitySignalOnly(t *testing.T) {
	payload := json.RawMessage(`{
		"agentTaskId":"task-1",
		"agentStepId":"step-1",
		"sessionId":"session-1",
		"tool":"script.read",
		"status":"succeeded",
		"summary":"读取完成",
		"retryable":false,
		"data":{"content":"` + strings.Repeat("x", 1_500_000) + `"},
		"verifier":{"content":"large"},
		"trace":{"content":"large"},
		"nextActions":[{"label":"large"}]
	}`)

	projected, changed := ProjectRealtimePayload("agent.step.completed", payload)
	if !changed {
		t.Fatal("agent activity payload was not projected")
	}
	if len(projected) >= 8_192 {
		t.Fatalf("projected payload is too large: %d bytes", len(projected))
	}
	var values map[string]any
	if err := json.Unmarshal(projected, &values); err != nil {
		t.Fatalf("decode projected payload: %v", err)
	}
	for _, field := range []string{"agentTaskId", "agentStepId", "sessionId", "tool", "status", "summary", "retryable", "detailsAvailable"} {
		if _, ok := values[field]; !ok {
			t.Fatalf("projected payload is missing %s: %#v", field, values)
		}
	}
	for _, field := range []string{"data", "verifier", "trace", "nextActions"} {
		if _, ok := values[field]; ok {
			t.Fatalf("projected payload retained %s", field)
		}
	}
}

func TestProjectRealtimePayloadKeepsWorkflowActivitySignalOnly(t *testing.T) {
	payload := json.RawMessage(`{
		"workflowRunId":"run-1",
		"workflowType":"source_to_script",
		"nodeKey":"generate_episode",
		"status":"partial_succeeded",
		"totalItems":199,
		"completedItems":198,
		"content":"` + strings.Repeat("x", 1_500_000) + `",
		"output":{"content":"large"},
		"providerCallIds":["call-1","call-2"]
	}`)

	projected, changed := ProjectRealtimePayload("workflow.run.partial_succeeded", payload)
	if !changed {
		t.Fatal("workflow activity payload was not projected")
	}
	if len(projected) >= 8_192 {
		t.Fatalf("projected payload is too large: %d bytes", len(projected))
	}
	var values map[string]any
	if err := json.Unmarshal(projected, &values); err != nil {
		t.Fatalf("decode projected payload: %v", err)
	}
	for _, field := range []string{"workflowRunId", "workflowType", "nodeKey", "status", "totalItems", "completedItems", "detailsAvailable"} {
		if _, ok := values[field]; !ok {
			t.Fatalf("projected payload is missing %s: %#v", field, values)
		}
	}
	for _, field := range []string{"content", "output", "providerCallIds"} {
		if _, ok := values[field]; ok {
			t.Fatalf("projected payload retained %s", field)
		}
	}
}

func TestProjectRealtimePayloadLeavesNonActivityEventUnchanged(t *testing.T) {
	payload := json.RawMessage(`{"workflowRunId":"run-1","output":{"count":2}}`)
	projected, changed := ProjectRealtimePayload("asset.card.updated", payload)
	if changed {
		t.Fatal("non-activity event was unexpectedly projected")
	}
	if string(projected) != string(payload) {
		t.Fatalf("payload changed: %s", projected)
	}
}

func TestProjectRealtimePayloadTruncatesAgentMessages(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"agentTaskId": "task-1",
		"summary":     strings.Repeat("中", realtimeSummaryLimit+20),
	})
	if err != nil {
		t.Fatal(err)
	}
	projected, _ := ProjectRealtimePayload("agent.task.blocked", payload)
	var values map[string]any
	if err := json.Unmarshal(projected, &values); err != nil {
		t.Fatal(err)
	}
	if got := []rune(values["summary"].(string)); len(got) != realtimeSummaryLimit+3 {
		t.Fatalf("summary rune count=%d", len(got))
	}
}
