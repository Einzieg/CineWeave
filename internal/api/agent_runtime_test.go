package api

import (
	"encoding/json"
	"testing"
)

func TestAgentRuntimeExtractsStructuredEntityReferences(t *testing.T) {
	refs := extractAgentEntityReferences(map[string]any{
		"scriptUnitId": "script-2",
		"data": map[string]any{
			"workflowRunIds": []any{"workflow-1", "workflow-2", "workflow-1"},
			"items": []any{
				map[string]any{"outputScriptUnitId": "script-3"},
				map[string]any{"label": "not-an-id"},
			},
		},
	})
	if got := refs["scriptUnitId"]; len(got) != 1 || got[0] != "script-2" {
		t.Fatalf("script refs = %#v", got)
	}
	if got := refs["workflowRunIds"]; len(got) != 2 {
		t.Fatalf("workflow refs = %#v", got)
	}
	if got := refs["outputScriptUnitId"]; len(got) != 1 || got[0] != "script-3" {
		t.Fatalf("output refs = %#v", got)
	}
	if _, exists := refs["label"]; exists {
		t.Fatalf("non-ID field leaked into refs: %#v", refs)
	}
}

func TestAgentRuntimeCompactsLargeObservations(t *testing.T) {
	long := make([]rune, 1200)
	for index := range long {
		long[index] = '字'
	}
	compacted, ok := compactAgentObservationValue(map[string]any{
		"content": string(long),
		"items":   make([]any, 25),
	}, 0).(map[string]any)
	if !ok {
		t.Fatalf("compacted value = %#v", compacted)
	}
	content, _ := compacted["content"].(string)
	if len([]rune(content)) > 1003 {
		t.Fatalf("content was not compacted: %d", len([]rune(content)))
	}
	items, _ := compacted["items"].([]any)
	if len(items) != 20 {
		t.Fatalf("item count = %d, want 20", len(items))
	}
}

func TestAgentRuntimeUsesSameLimitsForAllProjectKinds(t *testing.T) {
	for _, projectKind := range []string{"narrative", "commerce_video"} {
		task := AgentTask{Mode: "supervised", Summary: json.RawMessage(`{}`)}
		if got := agentRuntimeMaxPlanSteps(task); got != 1 {
			t.Fatalf("%s step limit = %d, want 1", projectKind, got)
		}
	}
	if got := agentRuntimeMaxPlanSteps(AgentTask{Mode: "plan_only"}); got != agentRuntimeMaxActions {
		t.Fatalf("plan-only step limit = %d", got)
	}
}

func TestAgentStepScriptDerivationBatchID(t *testing.T) {
	batchID := "6ce3f192-a872-49a0-b0e5-793333402bf2"
	if got := agentStepScriptDerivationBatchID(
		"commerce.script.derive.batch",
		map[string]any{"id": batchID},
	); got != batchID {
		t.Fatalf("batch id = %q", got)
	}
	if got := agentStepScriptDerivationBatchID(
		"commerce.script.derive.preview",
		map[string]any{"id": batchID},
	); got != "" {
		t.Fatalf("preview must not be treated as a persisted batch: %q", got)
	}
	if got := agentStepScriptDerivationBatchID(
		"commerce.script.derivation.get",
		map[string]any{"batchId": "not-a-uuid"},
	); got != "" {
		t.Fatalf("invalid batch id = %q", got)
	}
}

func TestAgentStepCommerceDirectVideoJobID(t *testing.T) {
	jobID := "6ce3f192-a872-49a0-b0e5-793333402bf2"
	if got := agentStepCommerceDirectVideoJobID(
		"commerce.video.generate",
		map[string]any{"id": jobID},
	); got != jobID {
		t.Fatalf("direct video job id = %q", got)
	}
	if got := agentStepCommerceDirectVideoJobID(
		"commerce.video.list",
		map[string]any{"id": jobID},
	); got != "" {
		t.Fatalf("video list must not be treated as one job: %q", got)
	}
	if got := agentStepCommerceDirectVideoJobID(
		"commerce.video.get",
		map[string]any{"jobId": "not-a-uuid"},
	); got != "" {
		t.Fatalf("invalid job id accepted: %q", got)
	}
}
