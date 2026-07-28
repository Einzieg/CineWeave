package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Einzieg/cineweave/internal/auth"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/httpx"
)

func TestInvokeAgentCommerceHandlerPreservesIdentityAndIdempotency(t *testing.T) {
	server := &Server{}
	task := AgentTask{ID: "9d7b9ea6-4b4c-4f73-ae50-31f41551644f"}
	step := AgentStep{ID: "d0e13468-dfab-4ee1-96df-505e97406c78", ToolName: "commerce.video.generate"}
	projectID := "3b412793-6bf8-43e1-b94b-462128de1ced"
	scriptUnitID := "62dc5404-b475-4204-918b-66c6b3c8738c"
	workflowRunID := "c62fe103-1dd4-4e60-9017-735885a61a90"
	args := map[string]any{"scriptUnitId": scriptUnitID, "expectedUnitGenerationId": "frozen-generation"}
	principal := auth.Principal{UserID: "user", OrganizationID: "organization"}
	parent := requestWithContext(t.Context())

	result := server.invokeAgentCommerceHandler(parent, principal, task, step, args, http.MethodPost,
		"/api/projects/"+projectID+"/commerce/script-units/"+scriptUnitID+"/direct-videos",
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
				"workflowType": "commerce_direct_video",
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
	step := AgentStep{ID: "d0e13468-dfab-4ee1-96df-505e97406c78", ToolName: "commerce.script.update"}
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

func TestAnnotateAgentCommerceScriptListAddsStableIdentity(t *testing.T) {
	firstID := "11111111-1111-4111-8111-111111111111"
	secondID := "22222222-2222-4222-8222-222222222222"
	data := map[string]any{
		"items": []any{
			map[string]any{"id": secondID, "title": "第二条"},
			map[string]any{"id": "archived", "title": "不在活动列表"},
		},
	}
	annotateAgentCommerceScriptList(data, commercepkg.ScriptUnitList{
		ScriptUnitsRevision: 7,
		Items: []commercepkg.ScriptUnit{
			{ID: firstID},
			{ID: secondID},
		},
	})
	items := data["items"].([]any)
	if got := agentIntArg(items[0].(map[string]any), "stableOrdinal", 0, 0, 100); got != 2 {
		t.Fatalf("stableOrdinal = %d", got)
	}
	if _, exists := items[1].(map[string]any)["stableOrdinal"]; exists {
		t.Fatal("non-active script must not receive an active stable ordinal")
	}
	if got := agentInt64Value(data["scriptUnitsRevision"]); got != 7 {
		t.Fatalf("scriptUnitsRevision = %d", got)
	}
}

func TestCommerceAgentSafetyClassification(t *testing.T) {
	if !agentToolReadOnly("commerce.video.get") {
		t.Fatal("Commerce direct-video read must be read-only")
	}
	if agentToolReadOnly("commerce.attachment.assign") {
		t.Fatal("attachment assignment writes project state")
	}
	if !agentToolMayGenerateVideo("commerce.video.generate", nil) {
		t.Fatal("direct video generation must respect allowVideoGeneration")
	}
	if !agentToolMaySpendProvider("commerce.script.derive.preview", nil) {
		t.Fatal("derivation preview must respect provider cost constraints")
	}
	if agentToolMayGenerateVideo("commerce.script.derive.retry_failed", nil) {
		t.Fatal("derivation retry does not generate video")
	}
	batchArgs := map[string]any{"variations": []any{
		map[string]any{"key": "night", "label": "夜景"},
		map[string]any{"key": "office", "label": "办公室"},
	}}
	if got := agentEstimatedProviderCostCents("commerce.script.derive.batch", batchArgs, 0); got != 12 {
		t.Fatalf("derivation estimated cost = %v, want 12", got)
	}
	if got := agentEstimatedProviderCostCents("commerce.video.generate", nil, 0); got != 50 {
		t.Fatalf("video estimated cost = %v, want 50", got)
	}
	for _, legacy := range []string{
		"commerce.script_unit.storyboard.list",
		"commerce.script_unit.reference_images.generate",
		"commerce.script_unit.batch.advance",
	} {
		if agentToolReadOnly(legacy) || agentToolMaySpendProvider(legacy, nil) || agentToolMayGenerateVideo(legacy, nil) {
			t.Fatalf("legacy tool %s must not participate in active safety classification", legacy)
		}
	}
}
