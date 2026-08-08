package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPrepareTransportEventProjectsOversizedAgentActivity(t *testing.T) {
	event := outboxEvent{
		ID:        "event-1",
		EventType: "agent.step.completed",
		Payload: json.RawMessage(`{
			"agentTaskId":"task-1",
			"agentStepId":"step-1",
			"status":"succeeded",
			"data":{"content":"` + strings.Repeat("x", 1_500_000) + `"},
			"verifier":{"content":"large"}
		}`),
	}

	projected, changed := prepareTransportEvent(event)
	if !changed {
		t.Fatal("agent activity event was not projected")
	}
	if len(projected.Payload) >= 8_192 {
		t.Fatalf("projected payload is too large: %d bytes", len(projected.Payload))
	}
	if strings.Contains(string(projected.Payload), `"data"`) || strings.Contains(string(projected.Payload), `"verifier"`) {
		t.Fatalf("canonical task details leaked into transport payload: %s", projected.Payload)
	}
	if !strings.Contains(string(projected.Payload), `"agentTaskId":"task-1"`) {
		t.Fatalf("task identity was not preserved: %s", projected.Payload)
	}
}

func TestPrepareTransportEventProjectsOversizedWorkflowActivity(t *testing.T) {
	event := outboxEvent{
		EventType: "workflow.node.completed",
		Payload: json.RawMessage(`{
			"workflowRunId":"run-1",
			"nodeKey":"generate_episode",
			"output":{"content":"` + strings.Repeat("x", 1_500_000) + `"}
		}`),
	}
	projected, changed := prepareTransportEvent(event)
	if !changed {
		t.Fatal("workflow activity event was not projected")
	}
	if len(projected.Payload) >= 8_192 {
		t.Fatalf("projected payload is too large: %d bytes", len(projected.Payload))
	}
	if strings.Contains(string(projected.Payload), `"output"`) {
		t.Fatalf("canonical workflow output leaked into transport payload: %s", projected.Payload)
	}
	if !strings.Contains(string(projected.Payload), `"workflowRunId":"run-1"`) {
		t.Fatalf("workflow identity was not preserved: %s", projected.Payload)
	}
}

func TestPrepareTransportEventPreservesNonActivityPayload(t *testing.T) {
	event := outboxEvent{EventType: "asset.card.updated", Payload: json.RawMessage(`{"assetId":"asset-1"}`)}
	projected, changed := prepareTransportEvent(event)
	if changed {
		t.Fatal("non-activity event was unexpectedly projected")
	}
	if string(projected.Payload) != string(event.Payload) {
		t.Fatalf("payload changed: %s", projected.Payload)
	}
}
