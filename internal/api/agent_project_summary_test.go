package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildAgentProjectGapSummaryReportsBlockingGaps(t *testing.T) {
	status := ProductionStatus{
		Overall: ProductionOverall{Stage: "source", Status: "pending", Progress: 0},
		Stages: ProductionStages{
			Source: ProductionSourceStage{Status: "not_started"},
		},
	}
	summary := buildAgentProjectGapSummary(status, agentProjectWorkflowGap{Failed: 1}, agentProjectReviewGap{OpenCritical: 1, OpenHigh: 2}, []agentProviderProfileStatus{
		{Purpose: "文本/剧本业务模型", ProfileKey: "script_agent_default", RequiredModality: "text", ProfileExists: true, Ready: true},
		{Purpose: "图片业务模型", ProfileKey: "image_generation_default", RequiredModality: "image", ProfileExists: true, EnabledBindingCount: 0, Reason: "binding_missing"},
		{Purpose: "视频业务模型", ProfileKey: "video_generation_default", RequiredModality: "video", Reason: "profile_not_found"},
	})
	if !strings.Contains(summary.Summary, "缺少原文或剧本来源") {
		t.Fatalf("summary did not mention missing source: %s", summary.Summary)
	}
	if !strings.Contains(summary.Summary, "业务模型绑定不可用") {
		t.Fatalf("summary did not mention model profile gaps: %s", summary.Summary)
	}
	if summary.Reviews.OpenCritical != 1 || summary.Reviews.OpenHigh != 2 {
		t.Fatalf("review counts = %#v", summary.Reviews)
	}
	if len(summary.NextActions) == 0 || summary.NextActions[0].Key != "import_source" {
		t.Fatalf("next actions = %#v", summary.NextActions)
	}
}

func TestBuildAgentProjectGapSummarySuggestsProductionActions(t *testing.T) {
	scriptID := "11111111-1111-1111-1111-111111111111"
	status := ProductionStatus{
		Overall: ProductionOverall{Stage: "shot_images", Status: "pending", Progress: 70},
		Stages: ProductionStages{
			Source: ProductionSourceStage{
				Status:              "scenes_ready",
				NovelSourceCount:    1,
				ChapterCount:        12,
				EventCount:          24,
				ActiveScriptID:      &scriptID,
				ScriptSceneCount:    10,
				AdaptationPlanCount: 1,
			},
			Assets:     ProductionAssetsStage{Status: "completed", CharacterCount: 2},
			Storyboard: ProductionStoryboardStage{Status: "ready", ShotCount: 5, ConfirmedShotCount: 5},
			ShotImages: ProductionShotMediaStage{Status: "partial", Total: 5, Succeeded: 3, Pending: 2},
			ShotVideos: ProductionShotMediaStage{Status: "not_started", Total: 5, Pending: 5},
		},
	}
	summary := buildAgentProjectGapSummary(status, agentProjectWorkflowGap{}, agentProjectReviewGap{}, readyProviderProfiles())
	foundImageAction := false
	for _, action := range summary.NextActions {
		if action.Tool == "shot.generate_missing_images" {
			foundImageAction = true
		}
		if action.Tool == "shot.generate_missing_videos" {
			t.Fatalf("video action should wait for images: %#v", action)
		}
	}
	if !foundImageAction {
		t.Fatalf("missing image generation action: %#v", summary.NextActions)
	}
}

func TestAgentTaskSummaryPatchFromProjectReadSummary(t *testing.T) {
	raw := mustRawJSON(agentToolResult{
		Name:    "project.read_summary",
		Status:  "succeeded",
		Summary: "项目离成片还差：缺少分镜镜头。",
		Data: map[string]any{
			"projectGapSummary": map[string]any{
				"summary": "项目离成片还差：缺少分镜镜头。",
				"gaps":    []string{"缺少分镜镜头"},
			},
		},
	})
	patch, ok := agentTaskSummaryPatchFromStepOutput(json.RawMessage(raw))
	if !ok {
		t.Fatal("expected project read summary patch")
	}
	if got := stringValueFromAny(patch["summary"]); got != "项目离成片还差：缺少分镜镜头。" {
		t.Fatalf("summary = %q", got)
	}
	gapSummary, ok := patch["projectGapSummary"].(map[string]any)
	if !ok {
		t.Fatalf("projectGapSummary = %#v", patch["projectGapSummary"])
	}
	if stringValueFromAny(gapSummary["summary"]) == "" {
		t.Fatal("project gap summary missing summary text")
	}
}

func TestAgentAutoProductionPlanPausesForProviderProfiles(t *testing.T) {
	summary := agentProjectGapSummary{
		Summary: "项目离成片还差：业务模型绑定不可用。",
		ProviderProfiles: []agentProviderProfileStatus{
			{Purpose: "文本/剧本业务模型", ProfileKey: "script_agent_default", RequiredModality: "text", Ready: true},
			{Purpose: "图片业务模型", ProfileKey: "image_generation_default", RequiredModality: "image", Ready: false, Reason: "binding_missing"},
		},
	}
	plan, ok := agentAutoProductionPlanFromSummary("请自动推进到成片", summary)
	if !ok {
		t.Fatal("expected auto production plan")
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("steps = %#v", plan.Steps)
	}
	if plan.Steps[0].Tool != "project.read_summary" || plan.Steps[1].Tool != "provider.list_status" {
		t.Fatalf("steps = %#v", plan.Steps)
	}
}

func TestAgentAutoProductionPlanPausesForFailedWorkflows(t *testing.T) {
	summary := agentProjectGapSummary{
		Summary:          "项目存在失败工作流。",
		Workflows:        agentProjectWorkflowGap{Failed: 1},
		ProviderProfiles: readyProviderProfiles(),
		NextActions: []agentProjectNextAction{
			workflowNextAction("script_to_storyboard", "从剧本生成分镜", "活动剧本还没有分镜镜头", "script_to_storyboard", map[string]any{}),
		},
	}
	plan, ok := agentAutoProductionPlanFromSummary("生成成片", summary)
	if !ok {
		t.Fatal("expected auto production plan")
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("steps = %#v", plan.Steps)
	}
	if plan.Steps[0].Tool != "project.read_summary" || plan.Steps[1].Tool != "workflow.read_runs" {
		t.Fatalf("steps = %#v", plan.Steps)
	}
}

func TestAgentAutoProductionPlanRunsReviewBeforeProduction(t *testing.T) {
	summary := agentProjectGapSummary{
		Summary:          "项目离成片还差：缺少分镜镜头。",
		ProviderProfiles: readyProviderProfiles(),
		NextActions: []agentProjectNextAction{
			workflowNextAction("script_to_storyboard", "从剧本生成分镜", "活动剧本还没有分镜镜头", "script_to_storyboard", map[string]any{}),
		},
	}
	plan, ok := agentAutoProductionPlanFromSummary("生成成片", summary)
	if !ok {
		t.Fatal("expected auto production plan")
	}
	if len(plan.Steps) != 3 {
		t.Fatalf("steps = %#v", plan.Steps)
	}
	if plan.Steps[0].Tool != "project.read_summary" || plan.Steps[1].Tool != "review.run" || plan.Steps[2].Tool != "workflow.start" {
		t.Fatalf("steps = %#v", plan.Steps)
	}
	args := map[string]any{}
	if err := json.Unmarshal(plan.Steps[2].Args, &args); err != nil {
		t.Fatalf("decode workflow args: %v", err)
	}
	if got := stringValueFromAny(args["workflowType"]); got != "script_to_storyboard" {
		t.Fatalf("workflowType = %q", got)
	}
}

func readyProviderProfiles() []agentProviderProfileStatus {
	return []agentProviderProfileStatus{
		{Purpose: "文本/剧本业务模型", ProfileKey: "script_agent_default", RequiredModality: "text", ProfileExists: true, EnabledBindingCount: 1, ActiveBindingCount: 1, ActiveCompatibleBindingCount: 1, Ready: true},
		{Purpose: "图片业务模型", ProfileKey: "image_generation_default", RequiredModality: "image", ProfileExists: true, EnabledBindingCount: 1, ActiveBindingCount: 1, ActiveCompatibleBindingCount: 1, Ready: true},
		{Purpose: "视频业务模型", ProfileKey: "video_generation_default", RequiredModality: "video", ProfileExists: true, EnabledBindingCount: 1, ActiveBindingCount: 1, ActiveCompatibleBindingCount: 1, Ready: true},
	}
}
