package api

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Einzieg/cineweave/internal/agent"
)

func TestAgentStepChildWorkflowRunIDsUsesExplicitContract(t *testing.T) {
	ids := []string{
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
	}
	raw := mustMarshal(agentToolResult{
		Name:                "future.async.tool",
		Status:              "succeeded",
		ChildWorkflowRunIDs: ids,
		Data: map[string]any{
			"workflowRunId": ids[0],
		},
	})
	if got := agentStepChildWorkflowRunIDs("future.async.tool", raw); !reflect.DeepEqual(got, ids) {
		t.Fatalf("child workflow ids = %#v, want %#v", got, ids)
	}
}

func TestAgentStepChildWorkflowRunIDsSupportsLegacyImagePromptStep(t *testing.T) {
	want := []string{"11111111-1111-4111-8111-111111111111"}
	raw := mustMarshal(agentToolResult{
		Name:   "shot.generate_image_prompts",
		Status: "succeeded",
		Data:   map[string]any{"workflowRunId": want[0]},
	})
	if got := agentStepChildWorkflowRunIDs("shot.generate_image_prompts", raw); !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy child workflow ids = %#v, want %#v", got, want)
	}
}

func TestAgentStepChildWorkflowRunIDsIgnoresReadResults(t *testing.T) {
	raw := mustMarshal(agentToolResult{
		Name:   "shot.status",
		Status: "succeeded",
		Data:   map[string]any{"status": map[string]any{"workflowRunId": "11111111-1111-4111-8111-111111111111"}},
	})
	if got := agentStepChildWorkflowRunIDs("shot.status", raw); len(got) != 0 {
		t.Fatalf("read-only child workflow ids = %#v, want none", got)
	}
}

func TestAgentWorkflowRecoveryPlanRebuildsPromptContractBeforeVideoRetry(t *testing.T) {
	server := &Server{}
	plan, handled, ok, err := server.agentWorkflowRecoveryPlan(context.Background(), Project{}, "生成失败镜头视频并自动修复依赖", []agentPendingWorkflowRun{{
		ID: "workflow-1", WorkflowType: "batch_generate_shot_videos", ErrorCode: "BATCH_ALL_FAILED",
		ErrorMessage: "activity error: 没有可执行的已审核视频提示词契约 (type: RENDER_PLAN_REPLAN_REQUIRED, retryable: false)",
		Input:        json.RawMessage(`{"workflowType":"batch_generate_shot_videos","input":{"shotIds":["11111111-1111-4111-8111-111111111111","22222222-2222-4222-8222-222222222222"]}}`),
	}})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(handled) != 1 || handled[0] != "workflow-1" {
		t.Fatalf("handled = %+v", handled)
	}
	if len(plan.Steps) != 2 || plan.Steps[0].Tool != "shot.generate_video_prompts" || plan.Steps[1].Tool != "shot.generate_missing_videos" {
		t.Fatalf("plan = %+v", plan)
	}
	registry, err := agent.DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.ValidatePlan(plan, registry, 20); err != nil {
		t.Fatalf("recovery plan is invalid: %v", err)
	}
}

func TestAgentWorkflowRecoveryPlanHonorsExplicitVideoPromptProhibition(t *testing.T) {
	server := &Server{}
	plan, handled, ok, err := server.agentWorkflowRecoveryPlan(
		context.Background(),
		Project{},
		"仅调用 shot.generate_missing_videos，复用已审核的视频提示词，不得重新生成或修改提示词，只生成指定镜头视频。",
		[]agentPendingWorkflowRun{{
			ID: "workflow-1", WorkflowType: "batch_generate_shot_videos", ErrorCode: "RENDER_PLAN_REPLAN_REQUIRED",
			ErrorMessage: "video model capabilities changed after the execution plan was created",
			Input:        json.RawMessage(`{"workflowType":"batch_generate_shot_videos","input":{"shotIds":["11111111-1111-4111-8111-111111111111"]}}`),
		}},
	)
	if err != nil {
		t.Fatalf("recovery plan error: %v", err)
	}
	if ok || len(plan.Steps) != 0 || len(handled) != 0 {
		t.Fatalf("explicitly forbidden recovery must not be planned: ok=%v plan=%+v handled=%+v", ok, plan, handled)
	}
}

func TestAgentWorkflowRecoveryPolicyRestrictsRecoveryToExplicitTools(t *testing.T) {
	policy := agentWorkflowRecoveryPolicyForGoal(
		"仅调用 shot.generate_missing_videos，复用现有图片和已审核提示词，不得重新生成或修改提示词。",
	)
	if !policy.restrictedTools || !policy.allows("shot.generate_missing_videos") {
		t.Fatalf("explicit video-only policy = %+v", policy)
	}
	for _, tool := range []string{"shot.generate_video_prompts", "provider.list_status", "provider.attest_video_capability", "agent.ask_user"} {
		if policy.allows(tool) {
			t.Fatalf("explicit video-only policy unexpectedly allows %s", tool)
		}
	}
}

func TestAgentShotProductionActionUsesSelectedActionsForExplicitTargets(t *testing.T) {
	args := map[string]any{"shotIds": []any{"11111111-1111-4111-8111-111111111111"}}
	if got := agentShotProductionAction("generate_image_prompts", args); got != "generate_selected_image_prompts" {
		t.Fatalf("image prompt action = %s", got)
	}
	if got := agentShotProductionAction("generate_video_prompts", args); got != "generate_selected_video_prompts" {
		t.Fatalf("video prompt action = %s", got)
	}
	if got := agentShotProductionAction("generate_missing_videos", args); got != "generate_selected_videos" {
		t.Fatalf("video action = %s", got)
	}
}
