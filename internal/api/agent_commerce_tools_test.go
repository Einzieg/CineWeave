package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/httpx"
)

func TestInvokeAgentCommerceHandlerPreservesIdentityAndIdempotency(t *testing.T) {
	server := &Server{}
	task := AgentTask{ID: "9d7b9ea6-4b4c-4f73-ae50-31f41551644f"}
	step := AgentStep{ID: "d0e13468-dfab-4ee1-96df-505e97406c78", ToolName: "commerce.script_unit.storyboard.generate"}
	projectID := "3b412793-6bf8-43e1-b94b-462128de1ced"
	scriptUnitID := "62dc5404-b475-4204-918b-66c6b3c8738c"
	workflowRunID := "c62fe103-1dd4-4e60-9017-735885a61a90"
	args := map[string]any{"scriptUnitId": scriptUnitID, "expectedUnitGenerationId": "frozen-generation"}
	principal := auth.Principal{UserID: "user", OrganizationID: "organization"}
	parent := requestWithContext(t.Context())

	result := server.invokeAgentCommerceHandler(parent, principal, task, step, args, http.MethodPost,
		"/api/projects/"+projectID+"/commerce/script-units/"+scriptUnitID+"/storyboard-plans",
		map[string]string{"projectId": projectID, "scriptUnitId": scriptUnitID}, nil,
		map[string]any{"expectedUnitGenerationId": "frozen-generation"},
		func(w http.ResponseWriter, r *http.Request, got auth.Principal) {
			if got != principal {
				t.Fatalf("principal = %+v", got)
			}
			if r.PathValue("projectId") != projectID || r.PathValue("scriptUnitId") != scriptUnitID {
				t.Fatalf("path values = %q / %q", r.PathValue("projectId"), r.PathValue("scriptUnitId"))
			}
			if got := r.Header.Get("Idempotency-Key"); got != agentStepIdempotencyKey(task, step) {
				t.Fatalf("idempotency key = %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["expectedUnitGenerationId"] != "frozen-generation" {
				t.Fatalf("body = %+v", body)
			}
			httpx.WriteJSON(w, r, http.StatusAccepted, map[string]any{
				"id":           workflowRunID,
				"workflowType": "commerce_storyboard_planning",
			}, nil)
		})
	if result.Status != "succeeded" || result.Data["workflowRunId"] != workflowRunID {
		t.Fatalf("result = %+v", result)
	}
	ids, err := agentWorkflowRunIDsFromValue(result.Data)
	if err != nil || len(ids) != 1 || ids[0] != workflowRunID {
		t.Fatalf("workflow ids = %v, err = %v", ids, err)
	}
}

func TestInvokeAgentCommerceHandlerPreservesStructuredError(t *testing.T) {
	server := &Server{}
	task := AgentTask{ID: "9d7b9ea6-4b4c-4f73-ae50-31f41551644f"}
	step := AgentStep{ID: "d0e13468-dfab-4ee1-96df-505e97406c78", ToolName: "commerce.script_unit.version.activate"}
	result := server.invokeAgentCommerceHandler(requestWithContext(t.Context()), auth.Principal{}, task, step, map[string]any{},
		http.MethodPost, "/api/projects/p/commerce/script-units/u/versions/v/activate", nil, nil, nil,
		func(w http.ResponseWriter, r *http.Request, _ auth.Principal) {
			httpx.WriteError(w, r, http.StatusConflict, "COMMERCE_SCRIPT_UNIT_REVISION_CONFLICT", "脚本已被其他操作修改", map[string]any{"expectedRevision": 3}, false)
		})
	if result.Status != "failed" || result.ErrorCode != "COMMERCE_SCRIPT_UNIT_REVISION_CONFLICT" || result.ErrorMessage != "脚本已被其他操作修改" {
		t.Fatalf("result = %+v", result)
	}
	if result.Data["httpStatus"] != float64(http.StatusConflict) && result.Data["httpStatus"] != http.StatusConflict {
		t.Fatalf("http status = %#v", result.Data["httpStatus"])
	}
}

func TestCommerceAgentSafetyClassification(t *testing.T) {
	if !agentToolReadOnly("commerce.script_unit.storyboard.list") {
		t.Fatal("Commerce storyboard list must be read-only")
	}
	if agentToolReadOnly("commerce.product.rebuild_impact") {
		t.Fatal("rebuild impact persists a confirmation snapshot and must not bypass the kill switch")
	}
	if !agentToolMayGenerateVideo("commerce.script_unit.shot_videos.generate", nil) {
		t.Fatal("shot video generation must respect allowVideoGeneration")
	}
	if !agentToolMaySpendProvider("commerce.script_unit.reference_images.generate", map[string]any{"operation": "generate_images"}) {
		t.Fatal("reference image generation must respect provider cost constraints")
	}
	if !agentToolMayGenerateVideo("commerce.script_unit.batch.retry_failed", nil) {
		t.Fatal("batch retry can resume video generation and must respect allowVideoGeneration")
	}
	args := map[string]any{"operation": "generate_images", "shotIds": []any{
		"2bb3a91a-ab29-4071-8430-75c78282389a", "eb8280db-30e9-41d5-81d3-b37836f53482",
	}}
	if got := agentEstimatedProviderCostCents("commerce.script_unit.reference_images.generate", args, 0); got != 20 {
		t.Fatalf("estimated cost = %v, want 20", got)
	}
	batchArgs := map[string]any{
		"targetStage": "shot_videos",
		"items": []any{
			map[string]any{"shotIds": []any{"shot-1", "shot-2"}},
			map[string]any{"shotIds": []any{"shot-3", "shot-4", "shot-5"}},
		},
	}
	if got := agentEstimatedProviderCostCents("commerce.script_unit.batch.advance", batchArgs, 0); got != 250 {
		t.Fatalf("batch estimated cost = %v, want 250", got)
	}
}
