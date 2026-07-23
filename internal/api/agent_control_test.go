package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Einzieg/cineweave/internal/agent"
	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/workflows"
)

func TestNormalizeStoryboardAgentPlanCollapsesEpisodeWorkflows(t *testing.T) {
	plan := agent.Plan{Steps: []agent.PlanStep{
		{Tool: "project.read_summary", Args: json.RawMessage(`{}`)},
		{Tool: "workflow.start", Args: json.RawMessage(`{"workflowType":"parse_script_scenes","input":{"scriptId":"script-1"}}`)},
		{Tool: "workflow.start", Args: json.RawMessage(`{"workflowType":"script_to_storyboard","input":{"scriptId":"script-1","scriptEpisodeIds":["episode-1"]}}`)},
		{Tool: "workflow.start", Args: json.RawMessage(`{"workflowType":"script_to_storyboard","input":{"scriptId":"script-1","scriptEpisodeIds":["episode-2"]}}`)},
		{Tool: "storyboard.list", Args: json.RawMessage(`{}`)},
	}}

	got := normalizeStoryboardAgentPlan(plan, "生成第 1-2 集分镜")
	if len(got.Steps) != 3 {
		t.Fatalf("steps = %+v, want read + one workflow + list", got.Steps)
	}
	if agentPlanWorkflowType(got.Steps[1]) != "script_to_storyboard" {
		t.Fatalf("workflow step = %+v", got.Steps[1])
	}
	args := rawObject(got.Steps[1].Args)
	input, _ := args["input"].(map[string]any)
	ids := stringSliceFromAny(input["scriptEpisodeIds"])
	if strings.Join(ids, ",") != "episode-1,episode-2" {
		t.Fatalf("scriptEpisodeIds = %v", ids)
	}
}

func TestAgentToolsAndTasksAPI(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/text/generate" {
			t.Fatalf("unexpected gateway path %s", r.URL.Path)
		}
		var req struct {
			Input map[string]any `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode gateway request: %v", err)
		}
		if req.Input["responseFormat"] != "json" {
			t.Fatalf("gateway responseFormat = %v, want json", req.Input["responseFormat"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"providerCallId":"00000000-0000-0000-0000-000000000001","modelId":"planner-model","status":"succeeded","output":{"text":"{\"summary\":\"先读取项目，再等待用户确认是否启动生产\",\"steps\":[{\"tool\":\"project.read_summary\",\"args\":{},\"expectedResult\":\"得到项目缺口\"},{\"tool\":\"workflow.start\",\"args\":{\"workflowType\":\"script_to_storyboard\"},\"expectedResult\":\"启动分镜生产\"}]}"}}}`))
	}))
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "test-service-token")

	assertAPIErrorCode(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/agent/tools", seed.otherToken, seed.organizationID, nil, http.StatusForbidden, "FORBIDDEN")

	var tools struct {
		Items []struct {
			Name             string         `json:"name"`
			Risk             string         `json:"risk"`
			RequiresApproval bool           `json:"requiresApproval"`
			InputSchema      map[string]any `json:"inputSchema"`
		} `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/agent/tools", seed.ownerToken, seed.organizationID, nil, &tools)
	if len(tools.Items) == 0 {
		t.Fatal("agent tools list is empty")
	}
	assertAgentToolListed(t, tools.Items, "project.read_summary", "read", false)
	assertAgentToolListed(t, tools.Items, "project.clear_production_content", "destructive", true)
	assertAgentToolListed(t, tools.Items, "agent.ask_user", "draft", true)
	assertAgentToolListed(t, tools.Items, "source.update", "write", true)
	assertAgentToolListed(t, tools.Items, "source.delete", "destructive", true)
	assertAgentToolListed(t, tools.Items, "source.delete_chapter", "destructive", true)
	assertAgentToolListed(t, tools.Items, "script.update_episode", "write", true)
	assertAgentToolListed(t, tools.Items, "script.delete", "destructive", true)
	assertAgentToolListed(t, tools.Items, "asset.delete", "destructive", true)
	assertAgentToolListed(t, tools.Items, "asset.batch_generate_prompts", "workflow", true)
	assertAgentToolListed(t, tools.Items, "asset.batch_generate_images", "workflow", true)
	assertAgentToolListed(t, tools.Items, "workflow.start", "workflow", true)
	assertAgentToolListed(t, tools.Items, "provider.update_account", "admin", true)
	assertAgentToolListed(t, tools.Items, "provider.update_model", "admin", true)
	assertAgentToolListed(t, tools.Items, "provider.install_catalog_preset", "admin", true)
	assertAgentToolListed(t, tools.Items, "prompt.create_version", "admin", true)
	assertAgentToolListed(t, tools.Items, "prompt.activate_version", "admin", true)
	assertAgentToolListed(t, tools.Items, "commerce.product.get", "read", false)
	assertAgentToolListed(t, tools.Items, "commerce.product.rebuild", "destructive", true)
	assertAgentToolListed(t, tools.Items, "commerce.script_unit.storyboard.generate", "workflow", true)
	assertAgentToolListed(t, tools.Items, "commerce.script_unit.shot_videos.generate", "workflow", true)
	assertAgentToolListed(t, tools.Items, "commerce.script_unit.batch.advance", "workflow", true)
	registry, err := seed.apiServer.projectAgentRegistry()
	if err != nil {
		t.Fatalf("project agent registry: %v", err)
	}
	for _, name := range []string{"project.read_summary", "workflow.start", "script.create_version", "asset.batch_generate_prompts", "asset.batch_generate_images", "commerce.product.get", "commerce.script_unit.storyboard.generate"} {
		tool, ok := registry.Get(name)
		if !ok || tool.Execute == nil {
			t.Fatalf("registry tool %s execute = %v, exists=%v", name, tool.Execute, ok)
		}
	}

	var created AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal":        "总结项目离成片还差什么",
		"mode":        "plan_only",
		"constraints": map[string]any{"allowVideoGeneration": false},
	}, &created)
	if created.ID == "" || created.Status != "succeeded" || created.Mode != "plan_only" {
		t.Fatalf("created task = %+v", created)
	}
	if created.Constraints == nil || string(created.Constraints) == "" {
		t.Fatalf("created task constraints missing: %+v", created)
	}
	if len(created.Steps) != 2 || created.Steps[0].ToolName != "project.read_summary" || created.Steps[1].ToolName != "workflow.start" {
		t.Fatalf("created steps = %+v", created.Steps)
	}
	if len(created.Approvals) != 0 {
		t.Fatalf("plan_only approvals = %+v, want none", created.Approvals)
	}

	var supervised AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "检查项目并准备生成分镜",
		"mode": "supervised",
	}, &supervised)
	if supervised.Status != "waiting_approval" {
		t.Fatalf("supervised status = %s, want waiting_approval", supervised.Status)
	}
	if len(supervised.Steps) != 2 || supervised.Steps[0].Status != "succeeded" || supervised.Steps[1].Status != "waiting_approval" {
		t.Fatalf("supervised steps = %+v", supervised.Steps)
	}
	var stepOutput struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(supervised.Steps[0].Output, &stepOutput); err != nil {
		t.Fatalf("decode step output: %v", err)
	}
	if stepOutput.Status != "succeeded" || len(supervised.Approvals) != 1 {
		t.Fatalf("supervised output/approvals = %+v / %+v", stepOutput, supervised.Approvals)
	}

	var list struct {
		Items []AgentTask `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, nil, &list)
	if len(list.Items) == 0 || list.Items[0].ID != created.ID {
		t.Fatalf("task list = %+v, want first %s", list.Items, created.ID)
	}

	cancelTaskID := seed.insertAgentTask(t, "running")
	linkedWorkflowID := seed.insertWorkflowRun(t, "running")
	seed.insertAgentStepWithOutput(t, cancelTaskID, 1, "workflow.start", "workflow", "running", `{"workflowRunId":"`+linkedWorkflowID+`"}`)
	var detail AgentTask
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/agent/tasks/"+created.ID, seed.ownerToken, seed.organizationID, nil, &detail)
	if detail.ID != created.ID {
		t.Fatalf("task detail id = %s, want %s", detail.ID, created.ID)
	}

	var cancelled AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks/"+cancelTaskID+"/cancel", seed.ownerToken, seed.organizationID, nil, &cancelled)
	if cancelled.Status != "cancelled" || cancelled.ErrorCode == nil || *cancelled.ErrorCode != "AGENT_TASK_CANCELLED" {
		t.Fatalf("cancelled task = %+v", cancelled)
	}
	assertWorkflowStatus(t, seed, linkedWorkflowID, "cancelling")

	explicitCancelTaskID := seed.insertAgentTask(t, "running")
	explicitLinkedWorkflowID := seed.insertWorkflowRun(t, "running")
	seed.apiServer.temporal = &fakeTemporalClient{signalErr: errors.New("workflow execution already completed")}
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE agent_tasks SET temporal_workflow_id = 'completed-project-agent-workflow' WHERE id = $1
	`, explicitCancelTaskID); err != nil {
		t.Fatalf("set completed agent temporal workflow: %v", err)
	}
	seed.insertAgentStepWithOutput(
		t,
		explicitCancelTaskID,
		1,
		"shot.generate_missing_videos",
		"workflow",
		"succeeded",
		`{"status":"succeeded","childWorkflowRunIds":["`+explicitLinkedWorkflowID+`"],"data":{"workflowRunId":"`+explicitLinkedWorkflowID+`"}}`,
	)
	var explicitCancelled AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks/"+explicitCancelTaskID+"/cancel", seed.ownerToken, seed.organizationID, nil, &explicitCancelled)
	if explicitCancelled.Status != "cancelled" {
		t.Fatalf("explicit child workflow cancellation task = %+v", explicitCancelled)
	}
	assertWorkflowStatus(t, seed, explicitLinkedWorkflowID, "cancelling")
	var cancelledGenerationID, cancelledBindingID string
	var cancelledBindingRevision int64
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT production_generation_id::text, video_production_binding_id::text, video_production_binding_revision
		FROM workflow_runs
		WHERE id = $1
	`, explicitLinkedWorkflowID).Scan(&cancelledGenerationID, &cancelledBindingID, &cancelledBindingRevision); err != nil {
		t.Fatalf("load cancelled workflow production identity: %v", err)
	}
	if cancelledGenerationID == "" || cancelledBindingID == "" || cancelledBindingRevision < 1 {
		t.Fatalf("cancelled workflow production identity = %q / %q / %d", cancelledGenerationID, cancelledBindingID, cancelledBindingRevision)
	}

	resumableID := seed.insertAgentTask(t, "failed")
	approveStepID := seed.insertAgentStep(t, resumableID, 1, "workflow.start", "workflow", "waiting_approval")
	seed.insertAgentApproval(t, resumableID, approveStepID, "workflow")
	var approved AgentApproval
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks/"+resumableID+"/steps/"+approveStepID+"/approve", seed.ownerToken, seed.organizationID, nil, &approved)
	if approved.Status != "approved" {
		t.Fatalf("approved response = %+v", approved)
	}
	assertAgentStepStatus(t, seed, approveStepID, "approved")

	rejectStepID := seed.insertAgentStep(t, resumableID, 2, "review.apply_fix", "write", "waiting_approval")
	seed.insertAgentApproval(t, resumableID, rejectStepID, "write")
	var rejected AgentApproval
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks/"+resumableID+"/steps/"+rejectStepID+"/reject", seed.ownerToken, seed.organizationID, map[string]any{"note": "not now"}, &rejected)
	if rejected.Status != "rejected" {
		t.Fatalf("rejected response = %+v", rejected)
	}
	assertAgentStepStatus(t, seed, rejectStepID, "skipped")

	var resumed AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks/"+resumableID+"/resume", seed.ownerToken, seed.organizationID, nil, &resumed)
	if resumed.Status != "queued" || resumed.ErrorCode != nil || resumed.CompletedAt != nil {
		t.Fatalf("resumed task = %+v", resumed)
	}
}

func TestAgentGoalEffectClassification(t *testing.T) {
	tests := []struct {
		name       string
		goal       string
		write      bool
		clearScope bool
	}{
		{name: "clear production", goal: "清空当前项目除小说原文外的内容", write: true, clearScope: true},
		{name: "confirmed clear", goal: "彻底删除全部生产内容，保留小说原文", write: true, clearScope: true},
		{name: "diagnose deletion", goal: "检查为什么删除剧本失败", write: false},
		{name: "read status", goal: "查看当前项目状态", write: false},
		{name: "explicit update", goal: "请修改第一集剧本", write: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := agentGoalRequiresWriteEffect(test.goal); got != test.write {
				t.Fatalf("agentGoalRequiresWriteEffect(%q) = %v, want %v", test.goal, got, test.write)
			}
			if got := agentGoalRequestsProductionContentClear(test.goal); got != test.clearScope {
				t.Fatalf("agentGoalRequestsProductionContentClear(%q) = %v, want %v", test.goal, got, test.clearScope)
			}
		})
	}
}

func TestAgentTaskDetailIncludesLiveWorkflowEpisodeProgress(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	taskID := seed.insertAgentTask(t, "running")
	workflowRunID := seed.insertWorkflowRunWithType(t, "script_to_storyboard", "running")
	seed.insertAgentStepWithOutput(t, taskID, 1, "workflow.start", "workflow", "succeeded", `{"status":"succeeded","data":{"workflowRunId":"`+workflowRunID+`"}}`)
	if _, err := seed.pool.Exec(seed.ctx, `
		INSERT INTO workflow_node_runs(
			organization_id, project_id, workflow_run_id, node_key, node_type, status,
			input, output, started_at
		)
		VALUES (
			$1, $2, $3, 'generate_storyboard_from_script_episode-2', 'agent.storyboard_generate', 'running',
			jsonb_build_object('episodeIndex', 2, 'episodeTotal', 10, 'episodeTitle', '第二集'),
			jsonb_build_object('status', 'streaming', 'partialText', '正在生成第二集分镜', 'receivedChars', 10),
			now()
		)
	`, seed.organizationID, seed.projectID, workflowRunID); err != nil {
		t.Fatalf("insert live storyboard node: %v", err)
	}

	var detail AgentTask
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/agent/tasks/"+taskID, seed.ownerToken, seed.organizationID, nil, &detail)
	if len(detail.Steps) != 1 {
		t.Fatalf("steps = %+v", detail.Steps)
	}
	output := rawObject(detail.Steps[0].Output)
	data, _ := output["data"].(map[string]any)
	progress, _ := data["workflowProgress"].(map[string]any)
	activeNode, _ := progress["activeNode"].(map[string]any)
	nodeInput, _ := activeNode["input"].(map[string]any)
	nodeOutput, _ := activeNode["output"].(map[string]any)
	if progress["status"] != "running" || nodeInput["episodeIndex"] != float64(2) || nodeInput["episodeTotal"] != float64(10) {
		t.Fatalf("workflow progress = %+v", progress)
	}
	if nodeOutput["partialText"] != "正在生成第二集分镜" {
		t.Fatalf("workflow node output = %+v", nodeOutput)
	}
}

func TestProjectAgentStateGateBlocksRunningWorkflow(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	runningID := seed.insertWorkflowRunWithType(t, "script_to_storyboard", "running")
	gateway := newAgentPlannerGateway(t, map[string]any{
		"summary": "启动分镜生产",
		"steps": []map[string]any{
			{
				"tool":           "workflow.start",
				"args":           map[string]any{"workflowType": "script_to_storyboard"},
				"expectedResult": "启动分镜生产",
			},
		},
	})
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "test-service-token")

	var task AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "从当前剧本生成分镜",
		"mode": "supervised",
	}, &task)
	if task.Status != "blocked" || len(task.Steps) != 1 || task.Steps[0].Status != "blocked" {
		t.Fatalf("task = %+v, want blocked step", task)
	}
	assertAgentStepStateGateReason(t, task.Steps[0], "workflow_already_running")
	var latest string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT status FROM workflow_runs WHERE id = $1`, runningID).Scan(&latest); err != nil {
		t.Fatalf("select workflow: %v", err)
	}
	if latest != "running" {
		t.Fatalf("running workflow status = %s, want running", latest)
	}
}

func TestAgentTaskListFiltersBySession(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	firstSessionID := seed.insertAgentSession(t, "First Session")
	secondSessionID := seed.insertAgentSession(t, "Second Session")
	firstTaskID := seed.insertAgentTaskWithSession(t, "running", "first session task", firstSessionID)
	secondTaskID := seed.insertAgentTaskWithSession(t, "running", "second session task", secondSessionID)

	var firstList struct {
		Items []AgentTask `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/agent/tasks?filter%5BsessionId%5D="+firstSessionID, seed.ownerToken, seed.organizationID, nil, &firstList)
	if len(firstList.Items) != 1 || firstList.Items[0].ID != firstTaskID {
		t.Fatalf("first session task list = %+v, want %s only", firstList.Items, firstTaskID)
	}

	var secondList struct {
		Items []AgentTask `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/agent/tasks?filter%5BsessionId%5D="+secondSessionID, seed.ownerToken, seed.organizationID, nil, &secondList)
	if len(secondList.Items) != 1 || secondList.Items[0].ID != secondTaskID {
		t.Fatalf("second session task list = %+v, want %s only", secondList.Items, secondTaskID)
	}
}

func TestProjectAgentAskUserQuestion(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	gateway := newAgentPlannerGatewaySequence(t, map[string]any{
		"summary": "需要用户选择下一步",
		"steps": []map[string]any{
			{
				"tool": "agent.ask_user",
				"args": map[string]any{
					"question":    "要先处理哪一部分？",
					"allowCustom": true,
					"options": []map[string]any{
						{
							"id":          "content",
							"label":       "先整理内容",
							"description": "读取原文并确认分集。",
							"nextGoal":    "整理内容分集",
						},
						{
							"id":          "script",
							"label":       "先看剧本",
							"description": "读取剧本版本并检查分集。",
							"nextGoal":    "检查剧本分集",
						},
					},
					"defaultOptionId": "content",
				},
				"expectedResult": "得到用户选择",
			},
		},
	}, map[string]any{
		"summary": "按用户选择继续整理内容分集",
		"steps": []map[string]any{
			{
				"tool":           "source.list",
				"args":           map[string]any{},
				"expectedResult": "读取原文列表并继续整理内容分集",
			},
		},
	})
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "test-service-token")

	var task AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "帮我继续推进项目",
		"mode": "supervised",
		"constraints": map[string]any{
			"permissionMode": "full_access",
		},
	}, &task)
	if task.Status != "waiting_approval" || len(task.Steps) != 1 || task.Steps[0].Status != "waiting_approval" {
		t.Fatalf("task = %+v, want waiting question", task)
	}
	if len(task.Approvals) != 1 || task.Approvals[0].ApprovalType != "question" {
		t.Fatalf("approvals = %+v, want one question approval", task.Approvals)
	}
	requested := rawObject(task.Approvals[0].RequestedPayload)
	if got := stringValueFromAny(requested["question"]); got != "要先处理哪一部分？" {
		t.Fatalf("question = %q", got)
	}
	if options := requested["options"].([]any); len(options) != 2 {
		t.Fatalf("options = %+v, want 2", requested["options"])
	}

	var approval AgentApproval
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks/"+task.ID+"/steps/"+task.Steps[0].ID+"/approve", seed.ownerToken, seed.organizationID, map[string]any{
		"note": "先整理内容",
		"decision": map[string]any{
			"kind":                "option",
			"selectedOptionId":    "content",
			"selectedOptionLabel": "先整理内容",
			"nextGoal":            "整理内容分集",
		},
	}, &approval)
	if approval.Status != "approved" || approval.ApprovalType != "question" {
		t.Fatalf("approval = %+v, want approved question", approval)
	}

	var detail AgentTask
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/agent/tasks/"+task.ID, seed.ownerToken, seed.organizationID, nil, &detail)
	if detail.Status != "succeeded" || len(detail.Steps) != 2 || detail.Steps[0].Status != "succeeded" || detail.Steps[1].Status != "succeeded" {
		t.Fatalf("detail = %+v, want succeeded question and continuation steps", detail)
	}
	output := rawObject(detail.Steps[0].Output)
	data := agentObjectFromAny(output["data"])
	if got := stringValueFromAny(data["selectedOptionId"]); got != "content" {
		t.Fatalf("selectedOptionId = %q, output=%s", got, string(detail.Steps[0].Output))
	}
	if got := stringValueFromAny(data["nextGoal"]); got != "整理内容分集" {
		t.Fatalf("nextGoal = %q, output=%s", got, string(detail.Steps[0].Output))
	}
	if detail.Steps[1].ToolName != "source.list" {
		t.Fatalf("continuation tool = %q, want source.list", detail.Steps[1].ToolName)
	}
}

func TestProjectAgentStateGateBlocksVideoWithoutImages(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	workflowRunID := seed.insertWorkflowRunWithType(t, "script_to_video", "succeeded")
	_ = seed.insertProductionShotAt(t, workflowRunID, 0, "shot without image")
	gateway := newAgentPlannerGateway(t, map[string]any{
		"summary": "生成缺失视频",
		"steps": []map[string]any{
			{
				"tool":           "shot.generate_missing_videos",
				"args":           map[string]any{},
				"expectedResult": "启动缺失镜头视频生成",
			},
		},
	})
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "test-service-token")

	var task AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "生成缺失镜头视频",
		"mode": "supervised",
	}, &task)
	if task.Status != "blocked" || len(task.Steps) != 1 || task.Steps[0].Status != "blocked" {
		t.Fatalf("task = %+v, want blocked step", task)
	}
	assertAgentStepStateGateReason(t, task.Steps[0], "no_target_shots")
}

func TestProjectAgentCostGateBlocksProviderSpend(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	gateway := newAgentPlannerGateway(t, map[string]any{
		"summary": "测试提示词",
		"steps": []map[string]any{
			{
				"tool":           "prompt.render_test",
				"args":           map[string]any{"templateKey": "project_agent_plan"},
				"expectedResult": "渲染并测试提示词",
			},
		},
	})
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "test-service-token")

	var task AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal":        "测试提示词但不要产生供应商成本",
		"mode":        "supervised",
		"constraints": map[string]any{"allowProviderCost": false},
	}, &task)
	if task.Status != "blocked" || len(task.Steps) != 1 || task.Steps[0].Status != "blocked" {
		t.Fatalf("task = %+v, want provider cost blocked", task)
	}
	assertAgentStepStateGateReason(t, task.Steps[0], "provider_cost_disabled")
}

func TestProjectAgentReviewGateBlocksProduction(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "character", "Blocking Asset", "approved", "")
	_ = seed.insertReviewItemForTarget(t, "open", "asset", "high", "canonical_asset", assetID)
	gateway := newAgentPlannerGateway(t, map[string]any{
		"summary": "启动分镜生产",
		"steps": []map[string]any{
			{
				"tool":           "workflow.start",
				"args":           map[string]any{"workflowType": "script_to_storyboard"},
				"expectedResult": "启动分镜生产",
			},
		},
	})
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "test-service-token")

	var task AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "生成分镜",
		"mode": "supervised",
	}, &task)
	if task.Status != "blocked" || len(task.Steps) != 1 || task.Steps[0].Status != "blocked" {
		t.Fatalf("task = %+v, want review blocked", task)
	}
	assertAgentStepStateGateReason(t, task.Steps[0], "open_blocking_review_items")
}

func TestProjectAgentWaitsForRunningChildWorkflow(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	server := New(seed.pool, seed.authService, nil, nil, nil)
	project, err := seed.apiServer.project(requestWithContext(seed.ctx), seed.projectID)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	taskID := seed.insertAgentTaskWithGoal(t, "running", "生成成片")
	workflowRunID := seed.insertWorkflowRunWithType(t, "script_to_storyboard", "running")
	seed.insertAgentStepWithOutput(t, taskID, 1, "workflow.start", "workflow", "succeeded", string(mustMarshal(agentToolResult{
		Name:   "workflow.start",
		Status: "succeeded",
		Data: map[string]any{
			"workflowRunId": workflowRunID,
			"workflowType":  "script_to_storyboard",
			"status":        "running",
		},
	})))

	updated, err := server.executeAgentTaskReadySteps(requestWithContext(seed.ctx), auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID}, project, taskID)
	if err != nil {
		t.Fatalf("execute ready steps: %v", err)
	}
	if updated.Status != "running" {
		t.Fatalf("task status = %s, want running", updated.Status)
	}
	var summary struct {
		WaitingForWorkflowRuns []agentPendingWorkflowRun `json:"waitingForWorkflowRuns"`
	}
	if err := json.Unmarshal(updated.Summary, &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if len(summary.WaitingForWorkflowRuns) != 1 || summary.WaitingForWorkflowRuns[0].ID != workflowRunID {
		t.Fatalf("waiting runs = %#v, want %s", summary.WaitingForWorkflowRuns, workflowRunID)
	}
}

func TestProjectAgentStopsAfterStartingChildWorkflow(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	server := New(seed.pool, seed.authService, nil, nil, nil)
	temporal := &fakeTemporalClient{}
	server.temporal = temporal
	scriptID := seed.insertActiveScript(t)
	project, err := seed.apiServer.project(requestWithContext(seed.ctx), seed.projectID)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	taskID := seed.insertAgentTaskWithGoal(t, "queued", "启动分镜后检查资产")
	workflowStepID := seed.insertAgentStepWithInputOutput(t, taskID, 1, "workflow.start", "workflow", "planned", string(mustMarshal(map[string]any{
		"workflowType": "script_to_storyboard",
		"input":        map[string]any{"scriptId": scriptID},
	})), `{}`)
	assetStepID := seed.insertAgentStepWithInputOutput(t, taskID, 2, "asset.list", "read", "planned", `{}`, `{}`)

	updated, err := server.executeAgentTaskReadySteps(requestWithContext(seed.ctx), auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID}, project, taskID)
	if err != nil {
		t.Fatalf("execute ready steps: %v", err)
	}
	if updated.Status != "running" {
		t.Fatalf("task status = %s, want running", updated.Status)
	}
	assertAgentStepStatus(t, seed, workflowStepID, "succeeded")
	assertAgentStepStatus(t, seed, assetStepID, "planned")
	var summary struct {
		WaitingForWorkflowRuns []agentPendingWorkflowRun `json:"waitingForWorkflowRuns"`
	}
	if err := json.Unmarshal(updated.Summary, &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if len(summary.WaitingForWorkflowRuns) != 1 || summary.WaitingForWorkflowRuns[0].Status != "queued" {
		t.Fatalf("waiting runs = %#v, want one queued child workflow", summary.WaitingForWorkflowRuns)
	}
}

func TestProjectAgentDoesNotResumeNextStepWhileChildWorkflowRuns(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	server := New(seed.pool, seed.authService, nil, nil, nil)
	project, err := seed.apiServer.project(requestWithContext(seed.ctx), seed.projectID)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	taskID := seed.insertAgentTaskWithGoal(t, "running", "等待分镜后再列出资产")
	workflowRunID := seed.insertWorkflowRunWithType(t, "script_to_storyboard", "running")
	seed.insertAgentStepWithOutput(t, taskID, 1, "workflow.start", "workflow", "succeeded", string(mustMarshal(agentToolResult{
		Name:   "workflow.start",
		Status: "succeeded",
		Data: map[string]any{
			"workflowRunId": workflowRunID,
			"workflowType":  "script_to_storyboard",
			"status":        "running",
		},
	})))
	assetStepID := seed.insertAgentStepWithInputOutput(t, taskID, 2, "asset.list", "read", "planned", `{}`, `{}`)

	updated, err := server.executeAgentTaskReadySteps(requestWithContext(seed.ctx), auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID}, project, taskID)
	if err != nil {
		t.Fatalf("execute ready steps: %v", err)
	}
	if updated.Status != "running" {
		t.Fatalf("task status = %s, want running", updated.Status)
	}
	assertAgentStepStatus(t, seed, assetStepID, "planned")
	var summary struct {
		WaitingForWorkflowRuns []agentPendingWorkflowRun `json:"waitingForWorkflowRuns"`
	}
	if err := json.Unmarshal(updated.Summary, &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if len(summary.WaitingForWorkflowRuns) != 1 || summary.WaitingForWorkflowRuns[0].ID != workflowRunID {
		t.Fatalf("waiting runs = %#v, want %s", summary.WaitingForWorkflowRuns, workflowRunID)
	}
}

func TestProjectAgentWaitsForImagePromptChildWorkflow(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	server := New(seed.pool, seed.authService, nil, nil, nil)
	project, err := seed.apiServer.project(requestWithContext(seed.ctx), seed.projectID)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	taskID := seed.insertAgentTaskWithGoal(t, "running", "生成第二集镜头图片提示词")
	workflowRunID := seed.insertWorkflowRunWithType(t, "batch_generate_shot_image_prompts", "running")
	seed.insertAgentStepWithOutput(t, taskID, 1, "shot.generate_image_prompts", "workflow", "succeeded", string(mustMarshal(agentToolResult{
		Name:                "shot.generate_image_prompts",
		Status:              "succeeded",
		ChildWorkflowRunIDs: []string{workflowRunID},
		Data: map[string]any{
			"workflowRunId": workflowRunID,
			"workflowType":  "batch_generate_shot_image_prompts",
			"status":        "running",
		},
	})))

	updated, err := server.executeAgentTaskReadySteps(requestWithContext(seed.ctx), auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID}, project, taskID)
	if err != nil {
		t.Fatalf("execute ready steps: %v", err)
	}
	if updated.Status != "running" {
		t.Fatalf("task status = %s, want running", updated.Status)
	}
	var summary struct {
		WaitingForWorkflowRuns []agentPendingWorkflowRun `json:"waitingForWorkflowRuns"`
	}
	if err := json.Unmarshal(updated.Summary, &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if len(summary.WaitingForWorkflowRuns) != 1 || summary.WaitingForWorkflowRuns[0].ID != workflowRunID {
		t.Fatalf("waiting runs = %#v, want %s", summary.WaitingForWorkflowRuns, workflowRunID)
	}
}

func TestProjectAgentWaitsForActiveNodesWhenWorkflowRunLooksComplete(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	server := New(seed.pool, seed.authService, nil, nil, nil)
	project, err := seed.apiServer.project(requestWithContext(seed.ctx), seed.projectID)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	taskID := seed.insertAgentTaskWithGoal(t, "running", "等待内部节点完成后再列出资产")
	workflowRunID := seed.insertWorkflowRunWithType(t, "script_to_storyboard", "succeeded")
	seed.insertWorkflowNodeRun(t, workflowRunID, "generate_storyboard_from_script", "running")
	seed.insertAgentStepWithOutput(t, taskID, 1, "workflow.start", "workflow", "succeeded", string(mustMarshal(agentToolResult{
		Name:   "workflow.start",
		Status: "succeeded",
		Data: map[string]any{
			"workflowRunId": workflowRunID,
			"workflowType":  "script_to_storyboard",
			"status":        "succeeded",
		},
	})))
	assetStepID := seed.insertAgentStepWithInputOutput(t, taskID, 2, "asset.list", "read", "planned", `{}`, `{}`)

	updated, err := server.executeAgentTaskReadySteps(requestWithContext(seed.ctx), auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID}, project, taskID)
	if err != nil {
		t.Fatalf("execute ready steps: %v", err)
	}
	if updated.Status != "running" {
		t.Fatalf("task status = %s, want running", updated.Status)
	}
	assertAgentStepStatus(t, seed, assetStepID, "planned")
	var summary struct {
		WaitingForWorkflowRuns []agentPendingWorkflowRun `json:"waitingForWorkflowRuns"`
	}
	if err := json.Unmarshal(updated.Summary, &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if len(summary.WaitingForWorkflowRuns) != 1 || summary.WaitingForWorkflowRuns[0].ID != workflowRunID {
		t.Fatalf("waiting runs = %#v, want %s", summary.WaitingForWorkflowRuns, workflowRunID)
	}
	if summary.WaitingForWorkflowRuns[0].Status != "succeeded" || summary.WaitingForWorkflowRuns[0].ActiveNodeRuns != 1 {
		t.Fatalf("waiting run = %#v, want succeeded with one active node", summary.WaitingForWorkflowRuns[0])
	}
}

func TestProjectAgentWaitsForActiveProviderTaskWhenWorkflowRunLooksComplete(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	server := New(seed.pool, seed.authService, nil, nil, nil)
	project, err := seed.apiServer.project(requestWithContext(seed.ctx), seed.projectID)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	taskID := seed.insertAgentTaskWithGoal(t, "running", "等待视频任务完成后再列出资产")
	workflowRunID := seed.insertWorkflowRunWithType(t, "script_to_video", "succeeded")
	seed.insertProviderAsyncTask(t, workflowRunID, "running")
	seed.insertAgentStepWithOutput(t, taskID, 1, "workflow.start", "workflow", "succeeded", string(mustMarshal(agentToolResult{
		Name:   "workflow.start",
		Status: "succeeded",
		Data: map[string]any{
			"workflowRunId": workflowRunID,
			"workflowType":  "script_to_video",
			"status":        "succeeded",
		},
	})))
	assetStepID := seed.insertAgentStepWithInputOutput(t, taskID, 2, "asset.list", "read", "planned", `{}`, `{}`)

	updated, err := server.executeAgentTaskReadySteps(requestWithContext(seed.ctx), auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID}, project, taskID)
	if err != nil {
		t.Fatalf("execute ready steps: %v", err)
	}
	if updated.Status != "running" {
		t.Fatalf("task status = %s, want running", updated.Status)
	}
	assertAgentStepStatus(t, seed, assetStepID, "planned")
	var summary struct {
		WaitingForWorkflowRuns []agentPendingWorkflowRun `json:"waitingForWorkflowRuns"`
	}
	if err := json.Unmarshal(updated.Summary, &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if len(summary.WaitingForWorkflowRuns) != 1 || summary.WaitingForWorkflowRuns[0].ID != workflowRunID {
		t.Fatalf("waiting runs = %#v, want %s", summary.WaitingForWorkflowRuns, workflowRunID)
	}
	if summary.WaitingForWorkflowRuns[0].Status != "succeeded" || summary.WaitingForWorkflowRuns[0].ActiveProviderTasks != 1 {
		t.Fatalf("waiting run = %#v, want succeeded with one active provider task", summary.WaitingForWorkflowRuns[0])
	}
}

func TestProjectAgentStopsWhenChildWorkflowFails(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	server := New(seed.pool, seed.authService, nil, nil, nil)
	project, err := seed.apiServer.project(requestWithContext(seed.ctx), seed.projectID)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	taskID := seed.insertAgentTaskWithGoal(t, "running", "生成1-10集剧本后再列出资产")
	workflowRunID := seed.insertWorkflowRunWithType(t, "source_to_script", "failed")
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE workflow_runs
		SET error_code = 'UPSTREAM_TIMEOUT', error_message = '视频任务在上游排队超时'
		WHERE id = $1
	`, workflowRunID); err != nil {
		t.Fatalf("set workflow failure: %v", err)
	}
	seed.insertAgentStepWithOutput(t, taskID, 1, "script.generate_from_source", "write", "succeeded", string(mustMarshal(agentToolResult{
		Name:   "script.generate_from_source",
		Status: "succeeded",
		Data: map[string]any{
			"workflowRunId": workflowRunID,
			"workflowType":  "source_to_script",
			"status":        "queued",
		},
	})))
	assetStepID := seed.insertAgentStepWithInputOutput(t, taskID, 2, "asset.list", "read", "planned", `{}`, `{}`)

	updated, err := server.executeAgentTaskReadySteps(requestWithContext(seed.ctx), auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID}, project, taskID)
	if err != nil {
		t.Fatalf("execute ready steps: %v", err)
	}
	if updated.Status != "failed" || updated.ErrorCode == nil || *updated.ErrorCode != "UPSTREAM_TIMEOUT" || updated.ErrorMessage == nil || *updated.ErrorMessage != "视频任务在上游排队超时" {
		t.Fatalf("task = %+v, want child workflow failure", updated)
	}
	assertAgentStepStatus(t, seed, assetStepID, "planned")
	var summary struct {
		FailedWorkflowRuns []agentPendingWorkflowRun `json:"failedWorkflowRuns"`
	}
	if err := json.Unmarshal(updated.Summary, &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if len(summary.FailedWorkflowRuns) != 1 || summary.FailedWorkflowRuns[0].ID != workflowRunID {
		t.Fatalf("failed runs = %#v, want %s", summary.FailedWorkflowRuns, workflowRunID)
	}
}

func TestProjectAgentAutoContinuationAppendsNextProductionPlan(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	server := New(seed.pool, seed.authService, nil, nil, nil)
	seed.insertReadyProviderProfiles(t)
	_ = seed.insertActiveScript(t)
	project, err := seed.apiServer.project(requestWithContext(seed.ctx), seed.projectID)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	taskID := seed.insertAgentTaskWithGoal(t, "running", "生成成片")
	workflowRunID := seed.insertWorkflowRunWithType(t, "source_to_script", "succeeded")
	seed.insertAgentStepWithOutput(t, taskID, 1, "workflow.start", "workflow", "succeeded", string(mustMarshal(agentToolResult{
		Name:   "workflow.start",
		Status: "succeeded",
		Data: map[string]any{
			"workflowRunId": workflowRunID,
			"workflowType":  "source_to_script",
			"status":        "succeeded",
		},
	})))

	appended, stopped, err := server.appendAgentAutoContinuationPlan(requestWithContext(seed.ctx), auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID}, project, taskID)
	if err != nil {
		t.Fatalf("append continuation plan: %v", err)
	}
	if stopped != nil {
		t.Fatalf("continuation stopped unexpectedly: %+v", *stopped)
	}
	if !appended {
		t.Fatal("expected continuation plan to be appended")
	}
	detail, err := server.agentTaskWithDetails(requestWithContext(seed.ctx), seed.projectID, taskID)
	if err != nil {
		t.Fatalf("load task detail: %v", err)
	}
	if len(detail.Steps) < 4 {
		t.Fatalf("steps after continuation = %+v", detail.Steps)
	}
	last := detail.Steps[len(detail.Steps)-1]
	if last.ToolName != "workflow.start" || last.StepIndex <= 1 {
		t.Fatalf("last step = %+v, want appended workflow.start", last)
	}
	args := rawObject(last.Input)
	if got := stringValueFromAny(args["workflowType"]); got != "parse_script_scenes" {
		t.Fatalf("appended workflowType = %q, want parse_script_scenes", got)
	}
}

func TestProjectAgentShotImageGenerationVerifier(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	server := New(seed.pool, seed.authService, nil, nil, nil)
	temporal := &fakeTemporalClient{}
	workflowRunID := seed.insertWorkflowRunWithType(t, "script_to_storyboard", "succeeded")
	shotID := insertShotProductionShot(t, seed, workflowRunID, 0, "", "", "not_started", "not_started")
	gateway := newAgentPlannerGateway(t, map[string]any{
		"summary": "生成缺失镜头图片",
		"steps": []map[string]any{
			{
				"tool":           "shot.generate_missing_images",
				"args":           map[string]any{},
				"expectedResult": "启动缺失镜头图片生成",
			},
		},
	})
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "test-service-token")

	var task AgentTask
	doAPISuccess(t, server.Handler(), http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "只生成缺失镜头图片，不要生成视频",
		"mode": "supervised",
	}, &task)
	if task.Status != "waiting_approval" || len(task.Steps) != 1 || task.Steps[0].Status != "waiting_approval" {
		t.Fatalf("task before approval = %+v", task)
	}
	server.temporal = temporal
	var imageStatusBefore string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT image_status FROM storyboard_shots WHERE id = $1`, shotID).Scan(&imageStatusBefore); err != nil {
		t.Fatalf("read image status before: %v", err)
	}
	if imageStatusBefore != "not_started" {
		t.Fatalf("image status before approval = %s", imageStatusBefore)
	}

	var approval AgentApproval
	doAPISuccess(t, server.Handler(), http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks/"+task.ID+"/steps/"+task.Steps[0].ID+"/approve", seed.ownerToken, seed.organizationID, nil, &approval)
	if approval.Status != "approved" {
		t.Fatalf("approval = %+v", approval)
	}
	dispatchWorkflowStartsForTest(t, server)
	var imageStatusAfter string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT image_status FROM storyboard_shots WHERE id = $1`, shotID).Scan(&imageStatusAfter); err != nil {
		t.Fatalf("read image status after: %v", err)
	}
	if imageStatusAfter != "queued" {
		t.Fatalf("image status after approval = %s, want queued", imageStatusAfter)
	}
	var videoStatusAfter string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT video_status FROM storyboard_shots WHERE id = $1`, shotID).Scan(&videoStatusAfter); err != nil {
		t.Fatalf("read video status after image generation: %v", err)
	}
	if videoStatusAfter != "not_started" {
		t.Fatalf("video status after image-only approval = %s, want not_started", videoStatusAfter)
	}
	if len(temporal.args) != 1 {
		t.Fatalf("temporal args len = %d, want 1", len(temporal.args))
	}
	workflowInput := temporal.args[0].(workflows.TextToStoryboardInput)
	var workflowOptions struct {
		MaxConcurrency int `json:"maxConcurrency"`
	}
	if err := json.Unmarshal(workflowInput.Input, &workflowOptions); err != nil {
		t.Fatalf("decode image workflow input: %v", err)
	}
	if workflowOptions.MaxConcurrency != workflows.DefaultShotImageConcurrency {
		t.Fatalf("maxConcurrency = %d, want %d", workflowOptions.MaxConcurrency, workflows.DefaultShotImageConcurrency)
	}
	var detail AgentTask
	doAPISuccess(t, server.Handler(), http.MethodGet, "/api/projects/"+seed.projectID+"/agent/tasks/"+task.ID, seed.ownerToken, seed.organizationID, nil, &detail)
	if detail.Status != "running" || len(detail.Steps) != 1 || detail.Steps[0].Status != "succeeded" {
		t.Fatalf("task after approval = %+v", detail)
	}
	assertAgentStepVerifierStatus(t, detail.Steps[0], "skipped")
}

func TestProjectAgentShotVideoGenerationVerifier(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	server := New(seed.pool, seed.authService, nil, nil, nil)
	server.temporal = &fakeTemporalClient{}
	workflowRunID := seed.insertWorkflowRunWithType(t, "script_to_video", "succeeded")
	imageArtifactID := seed.insertArtifact(t, "generated_image", "org/project/shot-for-video.png", "image/png")
	shotID := insertShotProductionShot(t, seed, workflowRunID, 0, imageArtifactID, "", "succeeded", "not_started")
	gateway := newAgentPlannerGateway(t, map[string]any{
		"summary": "生成缺失镜头视频",
		"steps": []map[string]any{
			{
				"tool":           "shot.generate_missing_videos",
				"args":           map[string]any{"workflowRunId": workflowRunID},
				"expectedResult": "启动缺失镜头视频生成",
			},
		},
	})
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "test-service-token")

	var task AgentTask
	doAPISuccess(t, server.Handler(), http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "生成缺失镜头视频",
		"mode": "supervised",
	}, &task)
	if task.Status != "waiting_approval" || len(task.Steps) != 1 || task.Steps[0].Status != "waiting_approval" {
		t.Fatalf("task before approval = %+v", task)
	}

	var approval AgentApproval
	doAPISuccess(t, server.Handler(), http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks/"+task.ID+"/steps/"+task.Steps[0].ID+"/approve", seed.ownerToken, seed.organizationID, nil, &approval)
	if approval.Status != "approved" {
		t.Fatalf("approval = %+v", approval)
	}
	var videoStatus string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT video_status FROM storyboard_shots WHERE id = $1`, shotID).Scan(&videoStatus); err != nil {
		t.Fatalf("read video status: %v", err)
	}
	if videoStatus != "queued" {
		t.Fatalf("video status after approval = %s, want queued", videoStatus)
	}
	var detail AgentTask
	doAPISuccess(t, server.Handler(), http.MethodGet, "/api/projects/"+seed.projectID+"/agent/tasks/"+task.ID, seed.ownerToken, seed.organizationID, nil, &detail)
	if detail.Status != "succeeded" || len(detail.Steps) != 1 || detail.Steps[0].Status != "succeeded" {
		t.Fatalf("task after approval = %+v", detail)
	}
	assertAgentStepVerifierStatus(t, detail.Steps[0], "succeeded")
}

func TestProjectAgentReviewToolsExecute(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", "")
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE canonical_assets SET description = '' WHERE id = $1`, assetID); err != nil {
		t.Fatalf("clear asset description: %v", err)
	}
	itemID := seed.insertReviewItemForTarget(t, "open", "asset", "medium", "canonical_asset", assetID)

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/text/generate" {
			t.Fatalf("unexpected gateway path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		plan := map[string]any{
			"summary": "运行项目审阅并生成修复草稿",
			"steps": []map[string]any{
				{
					"tool":           "review.run",
					"args":           map[string]any{"reviewType": "project", "includeDeterministicChecks": true, "includeAgent": false},
					"expectedResult": "写入项目审阅结果",
				},
				{
					"tool":           "review.generate_fix",
					"args":           map[string]any{"itemId": itemID, "mode": "deterministic"},
					"expectedResult": "生成修复草稿",
				},
			},
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"providerCallId": "00000000-0000-0000-0000-000000000002",
				"modelId":        "planner-model",
				"status":         "succeeded",
				"output":         map[string]any{"text": string(mustMarshal(plan))},
			},
		}); err != nil {
			t.Fatalf("write gateway response: %v", err)
		}
	}))
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "test-service-token")

	var task AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "检查项目并给修复建议，不要直接修改",
		"mode": "supervised",
	}, &task)
	if task.Status != "succeeded" {
		t.Fatalf("task status = %s, want succeeded: %+v", task.Status, task)
	}
	if len(task.Steps) != 2 || task.Steps[0].Status != "succeeded" || task.Steps[1].Status != "succeeded" {
		t.Fatalf("task steps = %+v", task.Steps)
	}
	var reviewRunCount int
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM review_runs WHERE project_id = $1 AND status = 'succeeded'`, seed.projectID).Scan(&reviewRunCount); err != nil {
		t.Fatalf("count review runs: %v", err)
	}
	if reviewRunCount == 0 {
		t.Fatal("agent review.run did not create a succeeded review run")
	}
	var fixStatus, patchText string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT status, patch::text FROM review_fixes WHERE project_id = $1 AND review_item_id = $2`, seed.projectID, itemID).Scan(&fixStatus, &patchText); err != nil {
		t.Fatalf("read review fix: %v", err)
	}
	if fixStatus != "draft" || patchText == "{}" {
		t.Fatalf("review fix status=%s patch=%s", fixStatus, patchText)
	}
	var assetDescription string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT description FROM canonical_assets WHERE id = $1`, assetID).Scan(&assetDescription); err != nil {
		t.Fatalf("read asset: %v", err)
	}
	if assetDescription != "" {
		t.Fatalf("review.generate_fix changed target description = %q", assetDescription)
	}
}

func TestProjectAgentSuggestionOnlyGoalStopsWritesAtApproval(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "character", "Suggestion Only Asset", "approved", "")
	gateway := newAgentPlannerGateway(t, map[string]any{
		"summary": "检查项目并给修复建议，不要直接修改",
		"steps": []map[string]any{
			{
				"tool":           "asset.update",
				"args":           map[string]any{"assetId": assetID, "patch": map[string]any{"description": "should not be written"}},
				"expectedResult": "错误的直接修改步骤必须等待审批",
			},
		},
	})
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "test-service-token")

	var task AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "检查项目并给修复建议，不要直接修改",
		"mode": "supervised",
	}, &task)
	if task.Status != "waiting_approval" || len(task.Steps) != 1 || task.Steps[0].Status != "waiting_approval" || len(task.Approvals) != 1 {
		t.Fatalf("task = %+v, want write step waiting approval", task)
	}
	var description string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT description FROM canonical_assets WHERE id = $1`, assetID).Scan(&description); err != nil {
		t.Fatalf("read asset: %v", err)
	}
	if description == "should not be written" {
		t.Fatal("suggestion-only task modified asset before approval")
	}
}

func TestProjectAgentPromptAndRewritePreviewToolsExecute(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	scriptID := seed.insertActiveScript(t)
	versionID := seed.currentScriptVersionID(t, scriptID)

	callCount := 0
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/text/generate" {
			t.Fatalf("unexpected gateway path %s", r.URL.Path)
		}
		callCount++
		w.Header().Set("Content-Type", "application/json")
		text := "改写后的预览内容"
		if callCount == 1 {
			plan := map[string]any{
				"summary": "测试提示词并生成剧本改写预览",
				"steps": []map[string]any{
					{
						"tool":           "prompt.render_test",
						"args":           map[string]any{"templateKey": "script_agent_rewrite", "variables": map[string]any{"input": map[string]any{"instruction": "压缩对白"}, "script": map[string]any{"content": "原始剧本"}}},
						"expectedResult": "渲染提示词",
					},
					{
						"tool":           "script.rewrite_preview",
						"args":           map[string]any{"scriptId": scriptID, "versionId": versionID, "instruction": "压缩对白"},
						"expectedResult": "生成预览内容",
					},
				},
			}
			text = string(mustMarshal(plan))
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"providerCallId": "00000000-0000-0000-0000-000000000003",
				"modelId":        "planner-model",
				"status":         "succeeded",
				"output":         map[string]any{"text": text},
			},
		}); err != nil {
			t.Fatalf("write gateway response: %v", err)
		}
	}))
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "test-service-token")

	var beforeCount int
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM script_versions WHERE script_id = $1`, scriptID).Scan(&beforeCount); err != nil {
		t.Fatalf("count script versions before: %v", err)
	}
	var task AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "测试提示词并预览改写，不要写入剧本版本",
		"mode": "supervised",
	}, &task)
	if task.Status != "succeeded" {
		t.Fatalf("task status = %s, want succeeded: %+v", task.Status, task)
	}
	if len(task.Steps) != 2 || task.Steps[0].Status != "succeeded" || task.Steps[1].Status != "succeeded" {
		t.Fatalf("task steps = %+v", task.Steps)
	}
	var afterCount int
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM script_versions WHERE script_id = $1`, scriptID).Scan(&afterCount); err != nil {
		t.Fatalf("count script versions after: %v", err)
	}
	if beforeCount != afterCount {
		t.Fatalf("script.rewrite_preview created versions: before=%d after=%d", beforeCount, afterCount)
	}
	if callCount != 2 {
		t.Fatalf("gateway callCount = %d, want planner + rewrite preview", callCount)
	}
}

func TestProjectAgentApplyReviewFixRequiresApproval(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", "")
	itemID := seed.insertReviewItemForTarget(t, "open", "asset", "medium", "canonical_asset", assetID)
	fixID := seed.insertReviewFixDraft(t, itemID, "canonical_asset", assetID, map[string]any{"description": "Updated by agent"}, nil)

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/text/generate" {
			t.Fatalf("unexpected gateway path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		plan := map[string]any{
			"summary": "应用已确认的修复",
			"steps": []map[string]any{
				{
					"tool":           "review.apply_fix",
					"args":           map[string]any{"fixId": fixID, "resolveReviewItem": true},
					"expectedResult": "更新目标对象并解决审阅问题",
				},
			},
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"providerCallId": "00000000-0000-0000-0000-000000000004",
				"modelId":        "planner-model",
				"status":         "succeeded",
				"output":         map[string]any{"text": string(mustMarshal(plan))},
			},
		}); err != nil {
			t.Fatalf("write gateway response: %v", err)
		}
	}))
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "test-service-token")

	var task AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "应用这个修复",
		"mode": "supervised",
	}, &task)
	if task.Status != "waiting_approval" || len(task.Steps) != 1 || task.Steps[0].Status != "waiting_approval" || len(task.Approvals) != 1 {
		t.Fatalf("task before approval = %+v", task)
	}
	var dryRun struct {
		Summary      string         `json:"summary"`
		Patch        map[string]any `json:"patch"`
		AfterPreview map[string]any `json:"afterPreview"`
	}
	if err := json.Unmarshal(task.Steps[0].DryRunOutput, &dryRun); err != nil {
		t.Fatalf("decode dry run output: %v", err)
	}
	if dryRun.Summary == "" || dryRun.Patch["description"] != "Updated by agent" || dryRun.AfterPreview["description"] != "Updated by agent" {
		t.Fatalf("dry run output = %+v", dryRun)
	}
	var descriptionBefore string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT description FROM canonical_assets WHERE id = $1`, assetID).Scan(&descriptionBefore); err != nil {
		t.Fatalf("read asset before: %v", err)
	}
	if descriptionBefore == "Updated by agent" {
		t.Fatal("review.apply_fix ran before approval")
	}

	var approval AgentApproval
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks/"+task.ID+"/steps/"+task.Steps[0].ID+"/approve", seed.ownerToken, seed.organizationID, nil, &approval)
	if approval.Status != "approved" {
		t.Fatalf("approval = %+v", approval)
	}
	var descriptionAfter, fixStatus, itemStatus string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT description FROM canonical_assets WHERE id = $1`, assetID).Scan(&descriptionAfter); err != nil {
		t.Fatalf("read asset after: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `SELECT status FROM review_fixes WHERE id = $1`, fixID).Scan(&fixStatus); err != nil {
		t.Fatalf("read fix status: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `SELECT status FROM review_items WHERE id = $1`, itemID).Scan(&itemStatus); err != nil {
		t.Fatalf("read item status: %v", err)
	}
	if descriptionAfter != "Updated by agent" || fixStatus != "applied" || itemStatus != "resolved" {
		t.Fatalf("description=%q fixStatus=%s itemStatus=%s", descriptionAfter, fixStatus, itemStatus)
	}
	var detail AgentTask
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/agent/tasks/"+task.ID, seed.ownerToken, seed.organizationID, nil, &detail)
	if len(detail.Steps) != 1 || detail.Steps[0].Status != "succeeded" {
		t.Fatalf("task detail after apply = %+v", detail)
	}
	assertAgentStepVerifierStatus(t, detail.Steps[0], "succeeded")
}

func TestProjectAgentScriptVersionToolsRequireApproval(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	scriptID := seed.insertActiveScript(t)
	initialVersionID := seed.currentScriptVersionID(t, scriptID)

	callCount := 0
	var createdVersionID string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/text/generate" {
			t.Fatalf("unexpected gateway path %s", r.URL.Path)
		}
		callCount++
		w.Header().Set("Content-Type", "application/json")
		step := map[string]any{
			"tool":           "script.create_version",
			"args":           map[string]any{"scriptId": scriptID, "content": "agent version content", "contentFormat": "markdown", "activate": false},
			"expectedResult": "创建剧本版本",
		}
		summary := "创建剧本版本"
		if callCount == 2 {
			step = map[string]any{
				"tool":           "script.activate_version",
				"args":           map[string]any{"scriptId": scriptID, "versionId": createdVersionID},
				"expectedResult": "激活剧本版本",
			}
			summary = "激活剧本版本"
		}
		plan := map[string]any{"summary": summary, "steps": []map[string]any{step}}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"providerCallId": "00000000-0000-0000-0000-000000000005",
				"modelId":        "planner-model",
				"status":         "succeeded",
				"output":         map[string]any{"text": string(mustMarshal(plan))},
			},
		}); err != nil {
			t.Fatalf("write gateway response: %v", err)
		}
	}))
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "test-service-token")

	var createTask AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "创建剧本版本",
		"mode": "supervised",
	}, &createTask)
	if createTask.Status != "waiting_approval" || len(createTask.Steps) != 1 {
		t.Fatalf("create task before approval = %+v", createTask)
	}
	var versionCountBefore int
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM script_versions WHERE script_id = $1`, scriptID).Scan(&versionCountBefore); err != nil {
		t.Fatalf("count versions before approve: %v", err)
	}
	var approval AgentApproval
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks/"+createTask.ID+"/steps/"+createTask.Steps[0].ID+"/approve", seed.ownerToken, seed.organizationID, nil, &approval)
	if approval.Status != "approved" {
		t.Fatalf("create approval = %+v", approval)
	}
	var versionCountAfter int
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM script_versions WHERE script_id = $1`, scriptID).Scan(&versionCountAfter); err != nil {
		t.Fatalf("count versions after approve: %v", err)
	}
	if versionCountAfter != versionCountBefore+1 {
		t.Fatalf("version count before=%d after=%d", versionCountBefore, versionCountAfter)
	}
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT id::text
		FROM script_versions
		WHERE script_id = $1
		ORDER BY version DESC
		LIMIT 1
	`, scriptID).Scan(&createdVersionID); err != nil {
		t.Fatalf("select created version: %v", err)
	}
	if createdVersionID == initialVersionID || createdVersionID == "" {
		t.Fatalf("createdVersionID=%s initial=%s", createdVersionID, initialVersionID)
	}
	project, err := seed.apiServer.project(requestWithContext(seed.ctx), seed.projectID)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	taskForRetry, err := seed.apiServer.agentTask(requestWithContext(seed.ctx), seed.projectID, createTask.ID)
	if err != nil {
		t.Fatalf("load agent task: %v", err)
	}
	stepForRetry, err := seed.apiServer.agentStep(requestWithContext(seed.ctx), createTask.ID, createTask.Steps[0].ID)
	if err != nil {
		t.Fatalf("load agent step: %v", err)
	}
	registry, err := seed.apiServer.projectAgentRegistry()
	if err != nil {
		t.Fatalf("project agent registry: %v", err)
	}
	tool, ok := registry.Get("script.create_version")
	if !ok || tool.Execute == nil {
		t.Fatalf("script.create_version registry tool missing execute")
	}
	retryResult := seed.apiServer.executeProjectAgentTool(requestWithContext(seed.ctx), auth.Principal{
		UserID:         seed.ownerUserID,
		OrganizationID: seed.organizationID,
	}, project, taskForRetry, stepForRetry, tool)
	if retryResult.Status != "succeeded" || !boolValue(retryResult.Data["idempotent"]) {
		t.Fatalf("retry result = %+v, want idempotent success", retryResult)
	}
	var versionCountAfterRetry int
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM script_versions WHERE script_id = $1`, scriptID).Scan(&versionCountAfterRetry); err != nil {
		t.Fatalf("count versions after retry: %v", err)
	}
	if versionCountAfterRetry != versionCountAfter {
		t.Fatalf("version count after retry=%d want %d", versionCountAfterRetry, versionCountAfter)
	}

	var activateTask AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "激活刚创建的剧本版本",
		"mode": "supervised",
	}, &activateTask)
	if activateTask.Status != "waiting_approval" || len(activateTask.Steps) != 1 {
		t.Fatalf("activate task before approval = %+v", activateTask)
	}
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks/"+activateTask.ID+"/steps/"+activateTask.Steps[0].ID+"/approve", seed.ownerToken, seed.organizationID, nil, &approval)
	if approval.Status != "approved" {
		t.Fatalf("activate approval = %+v", approval)
	}
	var currentVersionID string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT current_version_id::text FROM scripts WHERE id = $1`, scriptID).Scan(&currentVersionID); err != nil {
		t.Fatalf("read current version: %v", err)
	}
	if currentVersionID != createdVersionID {
		t.Fatalf("currentVersionID=%s want %s", currentVersionID, createdVersionID)
	}
}

func TestAgentTaskCompletionSummaryCollectsTrace(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	taskID := seed.insertAgentTask(t, "running")
	seed.insertAgentStepWithOutput(t, taskID, 1, "provider.test_model", "admin", "succeeded", string(mustMarshal(agentToolResult{
		Name:   "provider.test_model",
		Label:  "测试模型",
		Status: "succeeded",
		Data: map[string]any{
			"providerCallId":  "00000000-0000-0000-0000-000000000011",
			"promptHash":      "prompt-hash-a",
			"promptVersionId": "00000000-0000-0000-0000-000000000012",
			"testRunId":       "00000000-0000-0000-0000-000000000013",
			"result": map[string]any{
				"providerCallId": "00000000-0000-0000-0000-000000000014",
				"modelId":        "model-a",
			},
		},
	})))
	seed.insertAgentStepWithOutput(t, taskID, 2, "artifact.list", "read", "succeeded", string(mustMarshal(agentToolResult{
		Name:   "artifact.list",
		Label:  "成果列表",
		Status: "succeeded",
		Data: map[string]any{
			"workflowRun": map[string]any{"id": "00000000-0000-0000-0000-000000000015"},
			"artifacts": []map[string]any{{
				"id":         "00000000-0000-0000-0000-000000000016",
				"promptHash": "prompt-hash-b",
				"modelId":    "model-b",
			}},
		},
	})))
	if err := seed.apiServer.mergeAgentTaskCompletionSummary(seed.ctx, seed.projectID, taskID); err != nil {
		t.Fatalf("merge completion summary: %v", err)
	}
	var summaryRaw json.RawMessage
	if err := seed.pool.QueryRow(seed.ctx, `SELECT summary FROM agent_tasks WHERE id = $1`, taskID).Scan(&summaryRaw); err != nil {
		t.Fatalf("read task summary: %v", err)
	}
	var summary map[string]any
	if err := json.Unmarshal(summaryRaw, &summary); err != nil {
		t.Fatalf("decode task summary: %v", err)
	}
	trace, ok := mapFromAny(summary["trace"])
	if !ok {
		t.Fatalf("summary trace missing: %s", string(summaryRaw))
	}
	assertTraceContains(t, trace, "providerCallIds", "00000000-0000-0000-0000-000000000011")
	assertTraceContains(t, trace, "providerCallIds", "00000000-0000-0000-0000-000000000014")
	assertTraceContains(t, trace, "promptHashes", "prompt-hash-a")
	assertTraceContains(t, trace, "promptHashes", "prompt-hash-b")
	assertTraceContains(t, trace, "promptVersionIds", "00000000-0000-0000-0000-000000000012")
	assertTraceContains(t, trace, "testRunIds", "00000000-0000-0000-0000-000000000013")
	assertTraceContains(t, trace, "workflowRunIds", "00000000-0000-0000-0000-000000000015")
	assertTraceContains(t, trace, "artifactIds", "00000000-0000-0000-0000-000000000016")
	assertTraceContains(t, trace, "modelIds", "model-a")
	assertTraceContains(t, trace, "modelIds", "model-b")
}

func TestAgentTaskCompletionSummaryReplacesWaitingWorkflowState(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	taskID := seed.insertAgentTask(t, "succeeded")
	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE workflow_runs
		SET total_items = 1, completed_items = 1, failed_items = 0,
		    input = jsonb_build_object('workflowType', 'batch_generate_shot_videos')
		WHERE id = $1
	`, workflowRunID); err != nil {
		t.Fatalf("prepare completed workflow: %v", err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE agent_tasks
		SET summary = jsonb_build_object(
		  'summary', '已启动生产任务，正在等待 1 个工作流完成。',
		  'waitingForWorkflowRuns', jsonb_build_array(jsonb_build_object('id', $2::text, 'status', 'running'))
		)
		WHERE id = $1
	`, taskID, workflowRunID); err != nil {
		t.Fatalf("prepare waiting summary: %v", err)
	}
	seed.insertAgentStepWithOutput(t, taskID, 1, "shot.generate_missing_videos", "costed", "succeeded", string(mustMarshal(agentToolResult{
		Name:                "generate_missing_videos",
		Label:               "生成缺失视频",
		Status:              "succeeded",
		Summary:             "已启动视频生成。",
		ChildWorkflowRunIDs: []string{workflowRunID},
	})))

	item, err := seed.apiServer.agentTaskWithDetails(requestWithContext(seed.ctx), seed.projectID, taskID)
	if err != nil {
		t.Fatalf("read task details: %v", err)
	}
	if !json.Valid(item.Summary) {
		t.Fatalf("task summary is invalid JSON: %s", string(item.Summary))
	}
	var summaryRaw json.RawMessage
	if err := seed.pool.QueryRow(seed.ctx, `SELECT summary FROM agent_tasks WHERE id = $1`, taskID).Scan(&summaryRaw); err != nil {
		t.Fatalf("read task summary: %v", err)
	}
	var summary map[string]any
	if err := json.Unmarshal(summaryRaw, &summary); err != nil {
		t.Fatalf("decode task summary: %v", err)
	}
	if got := stringValueFromAny(summary["summary"]); got != "生产任务已完成：1 个工作流，共完成 1 项。" {
		t.Fatalf("summary = %q", got)
	}
	waiting, ok := summary["waitingForWorkflowRuns"].([]any)
	if !ok || len(waiting) != 0 {
		t.Fatalf("waiting workflows = %#v", summary["waitingForWorkflowRuns"])
	}
	completed, ok := summary["completedWorkflowRuns"].([]any)
	if !ok || len(completed) != 1 {
		t.Fatalf("completed workflows = %#v", summary["completedWorkflowRuns"])
	}
}

func TestAgentCompletedWorkflowSummaryReportsPartialCompletion(t *testing.T) {
	got := agentCompletedWorkflowSummary([]agentCompletedWorkflowRun{
		{Status: "partial_succeeded", CompletedItems: 30, FailedItems: 1},
	})
	if got != "生产任务部分完成：1 个工作流，成功 30 项，失败 1 项。" {
		t.Fatalf("summary = %q", got)
	}
}

func TestProjectAgentScriptGenerateAndRewriteRequireApproval(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	sourceID := seed.insertProjectSource(t, "novel", "Agent Novel")
	seed.insertNovelChapterWithOrdinals(t, sourceID, 1, 1, 1, "第一节", "第一节原文。")
	var generatedScriptID string

	callCount := 0
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/text/generate" && r.URL.Path != "/internal/provider/text/stream" {
			t.Fatalf("unexpected gateway path %s", r.URL.Path)
		}
		callCount++
		text := "生成后的剧本内容"
		switch callCount {
		case 1:
			plan := map[string]any{
				"summary": "从原文生成剧本",
				"steps": []map[string]any{{
					"tool":           "script.generate_from_source",
					"args":           map[string]any{"sourceId": sourceID, "title": "Agent Script", "instruction": "生成短剧本", "chapterRange": "1集"},
					"expectedResult": "创建剧本",
				}},
			}
			text = string(mustMarshal(plan))
		case 2:
			text = "生成后的剧本内容"
		case 3:
			plan := map[string]any{
				"summary": "改写剧本",
				"steps": []map[string]any{{
					"tool":           "script.rewrite",
					"args":           map[string]any{"scriptId": generatedScriptID, "instruction": "压缩对白", "activate": true},
					"expectedResult": "创建并激活改写版本",
				}},
			}
			text = string(mustMarshal(plan))
		default:
			text = "改写后的剧本内容"
		}
		if r.URL.Path == "/internal/provider/text/stream" {
			writeAgentTestSSE(t, w, text, callCount)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"providerCallId": "00000000-0000-0000-0000-000000000010",
				"modelId":        "planner-model",
				"status":         "succeeded",
				"output":         map[string]any{"text": text},
			},
		}); err != nil {
			t.Fatalf("write gateway response: %v", err)
		}
	}))
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "test-service-token")

	var generateTask AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "从原文生成剧本",
		"mode": "supervised",
	}, &generateTask)
	if generateTask.Status != "waiting_approval" || len(generateTask.Steps) != 1 {
		t.Fatalf("generate task before approval = %+v", generateTask)
	}
	var scriptCountBefore int
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM scripts WHERE project_id = $1`, seed.projectID).Scan(&scriptCountBefore); err != nil {
		t.Fatalf("count scripts before: %v", err)
	}
	var approval AgentApproval
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks/"+generateTask.ID+"/steps/"+generateTask.Steps[0].ID+"/approve", seed.ownerToken, seed.organizationID, nil, &approval)
	if approval.Status != "approved" {
		t.Fatalf("generate approval = %+v", approval)
	}
	var scriptCountAfter int
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM scripts WHERE project_id = $1`, seed.projectID).Scan(&scriptCountAfter); err != nil {
		t.Fatalf("count scripts after: %v", err)
	}
	if scriptCountAfter != scriptCountBefore+1 {
		t.Fatalf("script count before=%d after=%d", scriptCountBefore, scriptCountAfter)
	}
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT id::text
		FROM scripts
		WHERE project_id = $1 AND title = 'Agent Script'
		ORDER BY created_at DESC
		LIMIT 1
	`, seed.projectID).Scan(&generatedScriptID); err != nil {
		t.Fatalf("select generated script: %v", err)
	}

	var rewriteTask AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "改写剧本",
		"mode": "supervised",
	}, &rewriteTask)
	if rewriteTask.Status != "waiting_approval" || len(rewriteTask.Steps) != 1 {
		t.Fatalf("rewrite task before approval = %+v", rewriteTask)
	}
	var versionCountBefore int
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM script_versions WHERE script_id = $1`, generatedScriptID).Scan(&versionCountBefore); err != nil {
		t.Fatalf("count versions before rewrite: %v", err)
	}
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks/"+rewriteTask.ID+"/steps/"+rewriteTask.Steps[0].ID+"/approve", seed.ownerToken, seed.organizationID, nil, &approval)
	if approval.Status != "approved" {
		t.Fatalf("rewrite approval = %+v", approval)
	}
	var versionCountAfter int
	var currentContent string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM script_versions WHERE script_id = $1`, generatedScriptID).Scan(&versionCountAfter); err != nil {
		t.Fatalf("count versions after rewrite: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT sv.content
		FROM scripts s
		JOIN script_versions sv ON sv.id = s.current_version_id
		WHERE s.id = $1
	`, generatedScriptID).Scan(&currentContent); err != nil {
		t.Fatalf("read current script content: %v", err)
	}
	if versionCountAfter != versionCountBefore+1 || currentContent != "改写后的剧本内容" {
		t.Fatalf("version count before=%d after=%d currentContent=%q", versionCountBefore, versionCountAfter, currentContent)
	}
}

func TestProjectAgentGenerateScriptFromSourceCreatesOneEpisodePerNovelChapter(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	temporal := &fakeTemporalClient{}
	seed.apiServer.temporal = temporal

	sourceID := seed.insertProjectSource(t, "novel", "十集小说")
	for i := 1; i <= 10; i++ {
		seed.insertNovelChapterWithOrdinals(t, sourceID, i, 1, i, fmt.Sprintf("第%d节", i), fmt.Sprintf("第%d节原文。", i))
	}

	project, err := seed.apiServer.project(requestWithContext(seed.ctx), seed.projectID)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	taskID := seed.insertAgentTaskWithGoal(t, "running", "生成1-10集剧本")
	stepID := seed.insertAgentStepWithInputOutput(t, taskID, 1, "script.generate_from_source", "write", "running", `{}`, `{}`)
	result := seed.apiServer.agentToolGenerateScriptFromSource(
		requestWithContext(seed.ctx),
		auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID},
		project,
		AgentTask{ID: taskID, ProjectID: seed.projectID, UserGoal: "生成1-10集剧本", Mode: "supervised", Constraints: json.RawMessage(`{}`)},
		AgentStep{ID: stepID, TaskID: taskID, ToolName: "script.generate_from_source"},
		map[string]any{"sourceId": sourceID, "title": "十集剧本", "chapterRange": "1-10集"},
	)
	if result.Status != "succeeded" {
		t.Fatalf("tool result = %+v", result)
	}
	workflowRunID, _ := result.Data["workflowRunId"].(string)
	if workflowRunID == "" || result.Data["workflowType"] != "source_to_script" {
		t.Fatalf("result data missing workflow: %+v", result.Data)
	}
	dispatchWorkflowStartsForTest(t, seed.apiServer)
	if temporal.executeCount != 1 {
		t.Fatalf("temporal execute count = %d, want 1", temporal.executeCount)
	}
	if len(temporal.args) != 1 {
		t.Fatalf("temporal args len = %d", len(temporal.args))
	}
	workflowInput, ok := temporal.args[0].(workflows.TextToStoryboardInput)
	if !ok {
		t.Fatalf("workflow input type = %T", temporal.args[0])
	}
	var options struct {
		SourceID       string   `json:"sourceId"`
		ChapterIDs     []string `json:"chapterIds"`
		AgentTaskID    string   `json:"agentTaskId"`
		AgentStepID    string   `json:"agentStepId"`
		IdempotencyKey string   `json:"idempotencyKey"`
		MaxConcurrency int      `json:"maxConcurrency"`
	}
	if err := json.Unmarshal(workflowInput.Input, &options); err != nil {
		t.Fatalf("decode workflow input: %v", err)
	}
	if options.SourceID != sourceID || len(options.ChapterIDs) != 10 || options.AgentTaskID != taskID || options.AgentStepID != stepID || options.IdempotencyKey == "" || options.MaxConcurrency != 2 {
		t.Fatalf("workflow options = %+v", options)
	}
}

func TestProjectAgentVerifierReadsScriptVersionFromSourceWorkflowOutput(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	server := seed.apiServer
	project, err := server.project(requestWithContext(seed.ctx), seed.projectID)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	scriptID := seed.insertActiveScript(t)
	versionID := seed.currentScriptVersionID(t, scriptID)
	sourceID := seed.insertProjectSource(t, "novel", "十集小说")
	workflowRunID := seed.insertWorkflowRunWithType(t, "source_to_script", "succeeded")
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE workflow_runs
		SET output = $2
		WHERE id = $1
	`, workflowRunID, mustMarshal(map[string]any{
		"scriptId":         scriptID,
		"scriptVersionId":  versionID,
		"episodeCount":     10,
		"providerCallIds":  []string{},
		"providerCallId":   "",
		"sourceId":         sourceID,
		"workflowRunId":    workflowRunID,
		"generationStatus": "succeeded",
	})); err != nil {
		t.Fatalf("update workflow output: %v", err)
	}
	verifier := server.verifyAgentToolResult(seed.ctx, project, agentToolResult{
		Name:   "script.generate_from_source",
		Status: "succeeded",
		Data: map[string]any{
			"workflowRunId": workflowRunID,
			"workflowType":  "source_to_script",
			"status":        "succeeded",
		},
	})
	if got := stringValueFromAny(verifier["status"]); got != "succeeded" {
		t.Fatalf("verifier status = %q, output=%+v", got, verifier)
	}
}

func TestProjectAgentCreativeUpdateToolsRequireApproval(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", "")
	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	shotID := seed.insertProductionShot(t, workflowRunID, "", "", "approved", "image_succeeded")

	callCount := 0
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/text/generate" {
			t.Fatalf("unexpected gateway path %s", r.URL.Path)
		}
		callCount++
		w.Header().Set("Content-Type", "application/json")
		step := map[string]any{
			"tool":           "asset.update",
			"args":           map[string]any{"assetId": assetID, "patch": map[string]any{"description": "Agent edited asset"}},
			"expectedResult": "更新资产描述",
		}
		summary := "更新资产"
		if callCount == 2 {
			step = map[string]any{
				"tool":           "storyboard.update_shot",
				"args":           map[string]any{"shotId": shotID, "patch": map[string]any{"visual": "Agent edited shot visual"}},
				"expectedResult": "更新分镜画面",
			}
			summary = "更新分镜"
		}
		plan := map[string]any{"summary": summary, "steps": []map[string]any{step}}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"providerCallId": "00000000-0000-0000-0000-000000000006",
				"modelId":        "planner-model",
				"status":         "succeeded",
				"output":         map[string]any{"text": string(mustMarshal(plan))},
			},
		}); err != nil {
			t.Fatalf("write gateway response: %v", err)
		}
	}))
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "test-service-token")

	var assetTask AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "更新资产描述",
		"mode": "supervised",
	}, &assetTask)
	if assetTask.Status != "waiting_approval" || len(assetTask.Steps) != 1 {
		t.Fatalf("asset task before approval = %+v", assetTask)
	}
	var approval AgentApproval
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks/"+assetTask.ID+"/steps/"+assetTask.Steps[0].ID+"/approve", seed.ownerToken, seed.organizationID, nil, &approval)
	if approval.Status != "approved" {
		t.Fatalf("asset approval = %+v", approval)
	}
	var assetDescription string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT description FROM canonical_assets WHERE id = $1`, assetID).Scan(&assetDescription); err != nil {
		t.Fatalf("read asset description: %v", err)
	}
	if assetDescription != "Agent edited asset" {
		t.Fatalf("asset description = %q", assetDescription)
	}

	var shotTask AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "更新分镜画面",
		"mode": "supervised",
	}, &shotTask)
	if shotTask.Status != "waiting_approval" || len(shotTask.Steps) != 1 {
		t.Fatalf("shot task before approval = %+v", shotTask)
	}
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks/"+shotTask.ID+"/steps/"+shotTask.Steps[0].ID+"/approve", seed.ownerToken, seed.organizationID, nil, &approval)
	if approval.Status != "approved" {
		t.Fatalf("shot approval = %+v", approval)
	}
	var shotVisual string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT visual FROM storyboard_shots WHERE id = $1`, shotID).Scan(&shotVisual); err != nil {
		t.Fatalf("read shot visual: %v", err)
	}
	if shotVisual != "Agent edited shot visual" {
		t.Fatalf("shot visual = %q", shotVisual)
	}
	var eventAggregateType, eventAggregateID, eventEntityType, eventEntityID string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT aggregate_type, aggregate_id::text, payload->>'entityType', payload->>'entityId'
		FROM events
		WHERE event_type = 'agent.target.updated'
		  AND payload->>'agentTaskId' = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, shotTask.ID).Scan(&eventAggregateType, &eventAggregateID, &eventEntityType, &eventEntityID); err != nil {
		t.Fatalf("read agent target event: %v", err)
	}
	if eventAggregateType != "agent_task" || eventAggregateID != shotTask.ID {
		t.Fatalf("event aggregate = %s/%s, want agent_task/%s", eventAggregateType, eventAggregateID, shotTask.ID)
	}
	if eventEntityType != "storyboard_shot" || eventEntityID != shotID {
		t.Fatalf("event target = %s/%s, want storyboard_shot/%s", eventEntityType, eventEntityID, shotID)
	}
}

func TestProjectAgentRevisesAssetPromptByNameWithApproval(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "prop", "Oil Lamp", "approved", "")
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE canonical_assets
		SET base_prompt = 'old base prompt',
		    consistency_prompt = 'old consistency prompt',
		    negative_prompt = 'old negative prompt',
		    status = 'prompt_ready'
		WHERE id = $1
	`, assetID); err != nil {
		t.Fatalf("seed asset prompts: %v", err)
	}

	callCount := 0
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/text/generate" {
			t.Fatalf("unexpected gateway path %s", r.URL.Path)
		}
		callCount++
		var req struct {
			PromptTemplateKey string         `json:"promptTemplateKey"`
			Input             map[string]any `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode gateway request: %v", err)
		}
		responseText := ""
		modelID := "planner-model"
		providerCallID := "00000000-0000-0000-0000-000000000010"
		if callCount == 1 {
			plan := map[string]any{
				"summary": "强化油灯资产提示词",
				"steps": []map[string]any{{
					"tool": "asset.revise_prompt",
					"args": map[string]any{
						"assetName":   "Oil Lamp",
						"instruction": "增强四视图和纯道具约束",
						"fields":      []string{"basePrompt", "negativePrompt"},
					},
					"expectedResult": "保留一致性提示词并强化指定字段",
				}},
			}
			responseText = string(mustMarshal(plan))
		} else {
			if req.PromptTemplateKey != "asset_prompt_revision" {
				t.Fatalf("prompt template key = %q", req.PromptTemplateKey)
			}
			prompt := stringValueFromAny(req.Input["prompt"])
			if !strings.Contains(prompt, "old base prompt") || !strings.Contains(prompt, "增强四视图和纯道具约束") {
				t.Fatalf("revision prompt is missing current state or instruction: %q", prompt)
			}
			responseText = `{"basePrompt":"new four-view base prompt","consistencyPrompt":"model attempted replacement","negativePrompt":"new people-free negative prompt"}`
			modelID = "asset-revision-model"
			providerCallID = "00000000-0000-0000-0000-000000000011"
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"providerCallId": providerCallID,
				"modelId":        modelID,
				"status":         "succeeded",
				"output":         map[string]any{"text": responseText},
			},
		}); err != nil {
			t.Fatalf("write gateway response: %v", err)
		}
	}))
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "test-service-token")

	var task AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "把 Oil Lamp 的提示词增强四视图和纯道具约束，保持其他设定不变",
		"mode": "supervised",
	}, &task)
	if task.Status != "waiting_approval" || len(task.Steps) != 1 || task.Steps[0].ToolName != "asset.revise_prompt" {
		t.Fatalf("task before approval = %+v", task)
	}
	var approval AgentApproval
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks/"+task.ID+"/steps/"+task.Steps[0].ID+"/approve", seed.ownerToken, seed.organizationID, nil, &approval)
	if approval.Status != "approved" {
		t.Fatalf("approval = %+v", approval)
	}
	var basePrompt, consistencyPrompt, negativePrompt, status, staleState string
	var manualOverride bool
	var metadata map[string]any
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT COALESCE(base_prompt, ''), COALESCE(consistency_prompt, ''), COALESCE(negative_prompt, ''),
		       status, stale_state, manual_override, metadata
		FROM canonical_assets
		WHERE id = $1
	`, assetID).Scan(&basePrompt, &consistencyPrompt, &negativePrompt, &status, &staleState, &manualOverride, &metadata); err != nil {
		t.Fatalf("read revised asset: %v", err)
	}
	if basePrompt != "new four-view base prompt" || consistencyPrompt != "old consistency prompt" || negativePrompt != "new people-free negative prompt" {
		t.Fatalf("revised prompts base=%q consistency=%q negative=%q", basePrompt, consistencyPrompt, negativePrompt)
	}
	if status != "prompt_ready" || staleState != "needs_regeneration" || !manualOverride {
		t.Fatalf("asset state status=%q stale=%q manual=%v", status, staleState, manualOverride)
	}
	if metadata["agentTool"] != "asset.revise_prompt" || metadata["providerCallId"] != "00000000-0000-0000-0000-000000000011" {
		t.Fatalf("asset metadata = %+v", metadata)
	}
	if callCount != 2 {
		t.Fatalf("gateway call count = %d", callCount)
	}
}

func TestProjectAgentStoryboardReorderRequiresApproval(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	shot1 := seed.insertProductionShotAt(t, workflowRunID, 0, "Shot one")
	shot2 := seed.insertProductionShotAt(t, workflowRunID, 1, "Shot two")
	shot3 := seed.insertProductionShotAt(t, workflowRunID, 2, "Shot three")
	targetOrder := []string{shot3, shot1, shot2}

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/text/generate" {
			t.Fatalf("unexpected gateway path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		plan := map[string]any{
			"summary": "重排分镜",
			"steps": []map[string]any{
				{
					"tool":           "storyboard.reorder",
					"args":           map[string]any{"shotIds": targetOrder},
					"expectedResult": "按目标顺序更新分镜",
				},
			},
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"providerCallId": "00000000-0000-0000-0000-000000000007",
				"modelId":        "planner-model",
				"status":         "succeeded",
				"output":         map[string]any{"text": string(mustMarshal(plan))},
			},
		}); err != nil {
			t.Fatalf("write gateway response: %v", err)
		}
	}))
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "test-service-token")

	var task AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "重排分镜",
		"mode": "supervised",
	}, &task)
	if task.Status != "waiting_approval" || len(task.Steps) != 1 {
		t.Fatalf("reorder task before approval = %+v", task)
	}
	var approval AgentApproval
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks/"+task.ID+"/steps/"+task.Steps[0].ID+"/approve", seed.ownerToken, seed.organizationID, nil, &approval)
	if approval.Status != "approved" {
		t.Fatalf("reorder approval = %+v", approval)
	}
	rows, err := seed.pool.Query(seed.ctx, `
		SELECT id::text
		FROM storyboard_shots
		WHERE workflow_run_id = $1
		ORDER BY shot_index ASC
	`, workflowRunID)
	if err != nil {
		t.Fatalf("query reordered shots: %v", err)
	}
	defer rows.Close()
	got := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan shot id: %v", err)
		}
		got = append(got, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("shot rows: %v", err)
	}
	if string(mustMarshal(got)) != string(mustMarshal(targetOrder)) {
		t.Fatalf("shot order = %+v, want %+v", got, targetOrder)
	}
}

func TestProjectAgentFinalVideoActivateRequiresApproval(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	timelineID := insertProjectTimeline(t, seed)
	first := insertFinalVideoVersion(t, seed, timelineID, 1, "active")
	second := insertFinalVideoVersion(t, seed, timelineID, 2, "ready")

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/text/generate" {
			t.Fatalf("unexpected gateway path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		plan := map[string]any{
			"summary": "激活成片",
			"steps": []map[string]any{
				{
					"tool":           "final_video.activate",
					"args":           map[string]any{"finalVideoId": second},
					"expectedResult": "切换当前成片版本",
				},
			},
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"providerCallId": "00000000-0000-0000-0000-000000000008",
				"modelId":        "planner-model",
				"status":         "succeeded",
				"output":         map[string]any{"text": string(mustMarshal(plan))},
			},
		}); err != nil {
			t.Fatalf("write gateway response: %v", err)
		}
	}))
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "test-service-token")

	var task AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "激活第二版成片",
		"mode": "supervised",
	}, &task)
	if task.Status != "waiting_approval" || len(task.Steps) != 1 {
		t.Fatalf("final video task before approval = %+v", task)
	}
	var approval AgentApproval
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks/"+task.ID+"/steps/"+task.Steps[0].ID+"/approve", seed.ownerToken, seed.organizationID, nil, &approval)
	if approval.Status != "approved" {
		t.Fatalf("final video approval = %+v", approval)
	}
	var firstStatus, secondStatus, activeID string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT status FROM final_video_versions WHERE id = $1`, first).Scan(&firstStatus); err != nil {
		t.Fatalf("read first status: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `SELECT status FROM final_video_versions WHERE id = $1`, second).Scan(&secondStatus); err != nil {
		t.Fatalf("read second status: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `SELECT active_final_video_version_id::text FROM projects WHERE id = $1`, seed.projectID).Scan(&activeID); err != nil {
		t.Fatalf("read active final video: %v", err)
	}
	if firstStatus != "ready" || secondStatus != "active" || activeID != second {
		t.Fatalf("first=%s second=%s active=%s", firstStatus, secondStatus, activeID)
	}
}

func TestProjectAgentTimelineClipUpdateRequiresApproval(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	timelineID := insertProjectTimeline(t, seed)
	clipID := seed.insertTimelineClipForAgent(t, timelineID, 0, "Clip before")

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/text/generate" {
			t.Fatalf("unexpected gateway path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		plan := map[string]any{
			"summary": "更新时间线片段",
			"steps": []map[string]any{{
				"tool":           "timeline.update_clip",
				"args":           map[string]any{"clipId": clipID, "patch": map[string]any{"title": "Clip after", "notes": "Agent note"}},
				"expectedResult": "更新时间线片段",
			}},
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"providerCallId": "00000000-0000-0000-0000-000000000011",
				"modelId":        "planner-model",
				"status":         "succeeded",
				"output":         map[string]any{"text": string(mustMarshal(plan))},
			},
		}); err != nil {
			t.Fatalf("write gateway response: %v", err)
		}
	}))
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "test-service-token")

	var task AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "更新时间线片段",
		"mode": "supervised",
	}, &task)
	if task.Status != "waiting_approval" || len(task.Steps) != 1 {
		t.Fatalf("timeline task before approval = %+v", task)
	}
	var approval AgentApproval
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks/"+task.ID+"/steps/"+task.Steps[0].ID+"/approve", seed.ownerToken, seed.organizationID, nil, &approval)
	if approval.Status != "approved" {
		t.Fatalf("timeline approval = %+v", approval)
	}
	var title, notes string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT title, COALESCE(notes, '') FROM timeline_clips WHERE id = $1`, clipID).Scan(&title, &notes); err != nil {
		t.Fatalf("read timeline clip: %v", err)
	}
	if title != "Clip after" || notes != "Agent note" {
		t.Fatalf("title=%q notes=%q", title, notes)
	}
}

func TestProjectAgentAutoApproveExecutesWriteStep(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	timelineID := insertProjectTimeline(t, seed)
	clipID := seed.insertTimelineClipForAgent(t, timelineID, 0, "Auto before")
	gateway := newAgentPlannerGateway(t, map[string]any{
		"summary": "自动更新时间线片段",
		"steps": []map[string]any{{
			"tool":           "timeline.update_clip",
			"args":           map[string]any{"clipId": clipID, "patch": map[string]any{"title": "Auto after", "notes": "Auto note"}},
			"expectedResult": "更新时间线片段",
		}},
	})
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "test-service-token")

	var task AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal":        "自动更新时间线片段",
		"mode":        "supervised",
		"constraints": map[string]any{"permissionMode": "auto_approve"},
	}, &task)
	if task.Status != "succeeded" || len(task.Steps) != 1 || task.Steps[0].Status != "succeeded" || len(task.Approvals) != 0 {
		t.Fatalf("auto approve task = %+v", task)
	}
	var title, notes string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT title, COALESCE(notes, '') FROM timeline_clips WHERE id = $1`, clipID).Scan(&title, &notes); err != nil {
		t.Fatalf("read timeline clip: %v", err)
	}
	if title != "Auto after" || notes != "Auto note" {
		t.Fatalf("title=%q notes=%q", title, notes)
	}
}

func TestProjectAgentPromptVersionToolsRequireApproval(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	templateID := seed.insertPromptTemplateForAgent(t, "agent_prompt_test_"+randomStorageSegment())

	callCount := 0
	var createdVersionID string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/text/generate" {
			t.Fatalf("unexpected gateway path %s", r.URL.Path)
		}
		callCount++
		w.Header().Set("Content-Type", "application/json")
		step := map[string]any{
			"tool":           "prompt.create_version",
			"args":           map[string]any{"templateId": templateID, "title": "Agent draft", "content": "Hello {{ input.prompt }}", "contentFormat": "text", "activate": false},
			"expectedResult": "创建提示词版本",
		}
		summary := "创建提示词版本"
		if callCount == 2 {
			step = map[string]any{
				"tool":           "prompt.activate_version",
				"args":           map[string]any{"versionId": createdVersionID},
				"expectedResult": "激活提示词版本",
			}
			summary = "激活提示词版本"
		}
		plan := map[string]any{"summary": summary, "steps": []map[string]any{step}}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"providerCallId": "00000000-0000-0000-0000-000000000009",
				"modelId":        "planner-model",
				"status":         "succeeded",
				"output":         map[string]any{"text": string(mustMarshal(plan))},
			},
		}); err != nil {
			t.Fatalf("write gateway response: %v", err)
		}
	}))
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "test-service-token")

	var createTask AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "创建提示词版本",
		"mode": "supervised",
	}, &createTask)
	if createTask.Status != "waiting_approval" || len(createTask.Steps) != 1 {
		t.Fatalf("prompt create task before approval = %+v", createTask)
	}
	var approval AgentApproval
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks/"+createTask.ID+"/steps/"+createTask.Steps[0].ID+"/approve", seed.ownerToken, seed.organizationID, nil, &approval)
	if approval.Status != "approved" {
		t.Fatalf("prompt create approval = %+v", approval)
	}
	var createdStatus string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT id::text, status
		FROM prompt_versions
		WHERE template_id = $1
		ORDER BY COALESCE(version, version_no) DESC
		LIMIT 1
	`, templateID).Scan(&createdVersionID, &createdStatus); err != nil {
		t.Fatalf("read created prompt version: %v", err)
	}
	if createdVersionID == "" || createdStatus != "draft" {
		t.Fatalf("createdVersionID=%s status=%s", createdVersionID, createdStatus)
	}

	var activateTask AgentTask
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, map[string]any{
		"goal": "激活提示词版本",
		"mode": "supervised",
	}, &activateTask)
	if activateTask.Status != "waiting_approval" || len(activateTask.Steps) != 1 {
		t.Fatalf("prompt activate task before approval = %+v", activateTask)
	}
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/agent/tasks/"+activateTask.ID+"/steps/"+activateTask.Steps[0].ID+"/approve", seed.ownerToken, seed.organizationID, nil, &approval)
	if approval.Status != "approved" {
		t.Fatalf("prompt activate approval = %+v", approval)
	}
	var activeStatus string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT status FROM prompt_versions WHERE id = $1`, createdVersionID).Scan(&activeStatus); err != nil {
		t.Fatalf("read activated prompt version: %v", err)
	}
	if activeStatus != "active" {
		t.Fatalf("activeStatus = %s", activeStatus)
	}
}

func assertAgentToolListed(t *testing.T, items []struct {
	Name             string         `json:"name"`
	Risk             string         `json:"risk"`
	RequiresApproval bool           `json:"requiresApproval"`
	InputSchema      map[string]any `json:"inputSchema"`
}, name, risk string, requiresApproval bool) {
	t.Helper()
	for _, item := range items {
		if item.Name != name {
			continue
		}
		if item.Risk != risk || item.RequiresApproval != requiresApproval || len(item.InputSchema) == 0 {
			t.Fatalf("tool %s = %+v, want risk=%s approval=%v schema", name, item, risk, requiresApproval)
		}
		return
	}
	t.Fatalf("tool %s not found in %+v", name, items)
}

func newAgentPlannerGateway(t *testing.T, plan map[string]any) *httptest.Server {
	return newAgentPlannerGatewaySequence(t, plan)
}

func newAgentPlannerGatewaySequence(t *testing.T, plans ...map[string]any) *httptest.Server {
	t.Helper()
	var callIndex int
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/text/generate" {
			t.Fatalf("unexpected gateway path %s", r.URL.Path)
		}
		index := callIndex
		callIndex++
		if index >= len(plans) {
			index = len(plans) - 1
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"providerCallId": "",
				"modelId":        "planner-model",
				"status":         "succeeded",
				"output":         map[string]any{"text": string(mustMarshal(plans[index]))},
			},
		}); err != nil {
			t.Fatalf("write gateway response: %v", err)
		}
	}))
}

func writeAgentTestSSE(t *testing.T, w http.ResponseWriter, text string, callIndex int) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	delta, err := json.Marshal(map[string]any{"text": text})
	if err != nil {
		t.Fatalf("marshal delta: %v", err)
	}
	completed, err := json.Marshal(map[string]any{
		"providerCallId": fmt.Sprintf("00000000-0000-0000-0000-%012d", callIndex),
		"modelId":        "script-model",
		"status":         "succeeded",
		"output":         map[string]any{"text": text},
		"usage":          map[string]any{"estimatedCost": "0.00000000", "currency": "USD"},
	})
	if err != nil {
		t.Fatalf("marshal completed: %v", err)
	}
	if _, err := fmt.Fprintf(w, "event: provider.delta\ndata: %s\n\n", delta); err != nil {
		t.Fatalf("write delta: %v", err)
	}
	if _, err := fmt.Fprintf(w, "event: provider.completed\ndata: %s\n\n", completed); err != nil {
		t.Fatalf("write completed: %v", err)
	}
}

func assertAgentStepStateGateReason(t *testing.T, step AgentStep, want string) {
	t.Helper()
	var payload struct {
		StateGate struct {
			Reason string `json:"reason"`
		} `json:"stateGate"`
	}
	if err := json.Unmarshal(step.SupervisorDecision, &payload); err != nil {
		t.Fatalf("decode supervisor decision: %v", err)
	}
	if payload.StateGate.Reason != want {
		t.Fatalf("state gate reason = %q, want %q, decision=%s", payload.StateGate.Reason, want, string(step.SupervisorDecision))
	}
}

func assertAgentStepVerifierStatus(t *testing.T, step AgentStep, want string) {
	t.Helper()
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(step.VerifierOutput, &payload); err != nil {
		t.Fatalf("decode verifier output: %v", err)
	}
	if payload.Status != want {
		t.Fatalf("verifier status = %q, want %q, output=%s", payload.Status, want, string(step.VerifierOutput))
	}
}

func (s *artifactPreviewSeed) insertAgentTask(t *testing.T, status string) string {
	return s.insertAgentTaskWithGoal(t, status, "resume me")
}

func (s *artifactPreviewSeed) insertAgentTaskWithGoal(t *testing.T, status, goal string) string {
	t.Helper()
	var id string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO agent_tasks(organization_id, project_id, agent_type, user_goal, mode, status, constraints, plan, summary, error_code, error_message, created_by, completed_at)
		VALUES (
		  $1, $2, 'project_agent', $3, 'supervised', $4, '{}', '{}', '{}',
		  CASE WHEN $4 IN ('failed', 'blocked') THEN 'TEST_ERROR' ELSE NULL END,
		  CASE WHEN $4 IN ('failed', 'blocked') THEN 'failed for test' ELSE NULL END,
		  $5,
		  CASE WHEN $4 IN ('succeeded', 'failed', 'cancelled') THEN now() ELSE NULL END
		)
		RETURNING id
	`, s.organizationID, s.projectID, goal, status, s.ownerUserID).Scan(&id); err != nil {
		t.Fatalf("insert agent task: %v", err)
	}
	return id
}

func (s *artifactPreviewSeed) insertAgentSession(t *testing.T, title string) string {
	t.Helper()
	var id string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO agent_sessions(organization_id, project_id, agent_type, title, status, created_by)
		VALUES ($1, $2, 'project_agent', $3, 'active', $4)
		RETURNING id
	`, s.organizationID, s.projectID, title, s.ownerUserID).Scan(&id); err != nil {
		t.Fatalf("insert agent session: %v", err)
	}
	return id
}

func (s *artifactPreviewSeed) insertAgentTaskWithSession(t *testing.T, status, goal, sessionID string) string {
	t.Helper()
	var id string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO agent_tasks(organization_id, project_id, session_id, agent_type, user_goal, mode, status, constraints, plan, summary, created_by)
		VALUES ($1, $2, $3, 'project_agent', $4, 'supervised', $5, '{}', '{}', '{}', $6)
		RETURNING id
	`, s.organizationID, s.projectID, sessionID, goal, status, s.ownerUserID).Scan(&id); err != nil {
		t.Fatalf("insert agent task with session: %v", err)
	}
	return id
}

func (s *artifactPreviewSeed) insertNovelChapterWithOrdinals(t *testing.T, sourceID string, chapterIndex, volumeIndex, sectionIndex int, chapterTitle, content string) string {
	t.Helper()
	var id string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO novel_chapters(
			organization_id, project_id, source_id, chapter_index,
			volume_index, section_index, volume_title, chapter_title, content, event_state
		)
		VALUES ($1, $2, $3, $4, NULLIF($5, 0), NULLIF($6, 0), $7, $8, $9, 'pending')
		RETURNING id::text
	`, s.organizationID, s.projectID, sourceID, chapterIndex, volumeIndex, sectionIndex, "第一卷", chapterTitle, content).Scan(&id); err != nil {
		t.Fatalf("insert novel chapter: %v", err)
	}
	return id
}

func (s *artifactPreviewSeed) insertWorkflowRunWithType(t *testing.T, workflowType, status string) string {
	t.Helper()
	var workflowRunID string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO workflow_runs(organization_id, project_id, temporal_workflow_id, status, input, output, created_by)
		VALUES ($1, $2, $3, $4, $5, '{}', $6)
		RETURNING id
	`, s.organizationID, s.projectID, "agent-state-"+workflowType+"-"+randomStorageSegment(), status, mustMarshal(map[string]any{
		"workflowType": workflowType,
		"input":        map[string]any{},
	}), s.ownerUserID).Scan(&workflowRunID); err != nil {
		t.Fatalf("insert workflow run with type: %v", err)
	}
	return workflowRunID
}

func (s *artifactPreviewSeed) insertWorkflowNodeRun(t *testing.T, workflowRunID, nodeKey, status string) string {
	t.Helper()
	var nodeRunID string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO workflow_node_runs(organization_id, project_id, workflow_run_id, node_key, node_type, status, input, output, started_at, completed_at)
		VALUES (
			$1, $2, $3, $4, 'agent.test', $5, '{}', '{}',
			CASE WHEN $5 IN ('running', 'succeeded', 'failed') THEN now() ELSE NULL END,
			CASE WHEN $5 IN ('succeeded', 'failed', 'cancelled') THEN now() ELSE NULL END
		)
		RETURNING id::text
	`, s.organizationID, s.projectID, workflowRunID, nodeKey, status).Scan(&nodeRunID); err != nil {
		t.Fatalf("insert workflow node run: %v", err)
	}
	return nodeRunID
}

func (s *artifactPreviewSeed) insertProviderAsyncTask(t *testing.T, workflowRunID, status string) string {
	t.Helper()
	connectorKey := "agent-async-test-" + randomStorageSegment()
	var connectorID string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO provider_connectors(connector_key, name, type, is_official, manifest)
		VALUES ($1, 'Agent Async Test Connector', 'openai_compatible', false, '{}')
		RETURNING id
	`, connectorKey).Scan(&connectorID); err != nil {
		t.Fatalf("insert provider connector: %v", err)
	}
	var accountID string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO provider_accounts(organization_id, connector_id, name, base_url, auth_type, status, config, created_by)
		VALUES ($1, $2, 'Agent Async Test Account', 'http://provider.example.test', 'bearer', 'active', '{}', $3)
		RETURNING id
	`, s.organizationID, connectorID, s.ownerUserID).Scan(&accountID); err != nil {
		t.Fatalf("insert provider account: %v", err)
	}
	var callID string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO provider_call_logs(
			organization_id, project_id, workflow_run_id, provider_account_id, task_type, execution_mode, status,
			request_snapshot, response_snapshot, normalized_output
		)
		VALUES ($1, $2, $3, $4, 'video.create_task', 'async', 'running', '{}', '{}', '{}')
		RETURNING id
	`, s.organizationID, s.projectID, workflowRunID, accountID).Scan(&callID); err != nil {
		t.Fatalf("insert provider call log: %v", err)
	}
	var taskID string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO provider_async_tasks(
			provider_call_id, organization_id, provider_account_id, workflow_run_id, external_task_id, status, raw_status
		)
		VALUES ($1, $2, $3, $4, $5, $6, '{}')
		RETURNING id
	`, callID, s.organizationID, accountID, workflowRunID, "agent-async-"+randomStorageSegment(), status).Scan(&taskID); err != nil {
		t.Fatalf("insert provider async task: %v", err)
	}
	return taskID
}

func (s *artifactPreviewSeed) insertReadyProviderProfiles(t *testing.T) {
	t.Helper()
	var connectorID string
	connectorKey := "agent-test-" + randomStorageSegment()
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO provider_connectors(connector_key, name, type, is_official, manifest)
		VALUES ($1, 'Agent Test Connector', 'openai_compatible', false, '{}')
		ON CONFLICT (connector_key) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, connectorKey).Scan(&connectorID); err != nil {
		t.Fatalf("insert provider connector: %v", err)
	}
	var accountID string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO provider_accounts(organization_id, connector_id, name, base_url, auth_type, status, config, created_by)
		VALUES ($1, $2, 'Agent Test Account', 'http://provider-gateway.test', 'bearer', 'active', '{}', $3)
		RETURNING id
	`, s.organizationID, connectorID, s.ownerUserID).Scan(&accountID); err != nil {
		t.Fatalf("insert provider account: %v", err)
	}
	defs := []struct {
		key      string
		name     string
		purpose  string
		modality string
	}{
		{key: "script_agent_default", name: "Agent Test Text", purpose: "agent_test_text_" + randomStorageSegment(), modality: "text"},
		{key: "image_generation_default", name: "Agent Test Image", purpose: "agent_test_image_" + randomStorageSegment(), modality: "image"},
		{key: "video_generation_default", name: "Agent Test Video", purpose: "agent_test_video_" + randomStorageSegment(), modality: "video"},
	}
	for _, def := range defs {
		var modelID string
		if err := s.pool.QueryRow(s.ctx, `
			INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status)
			VALUES ($1, $2, $3, $4, 'active')
			RETURNING id
		`, accountID, def.key+"-"+randomStorageSegment(), def.name, def.modality).Scan(&modelID); err != nil {
			t.Fatalf("insert provider model %s: %v", def.key, err)
		}
		var profileID string
		if err := s.pool.QueryRow(s.ctx, `
			INSERT INTO model_profiles(organization_id, profile_key, name, purpose, routing_strategy, fallback_strategy)
			VALUES ($1, $2, $3, $4, 'priority', '{}')
			ON CONFLICT (organization_id, profile_key)
			DO UPDATE SET name = EXCLUDED.name, purpose = EXCLUDED.purpose
			RETURNING id
		`, s.organizationID, def.key, def.name, def.purpose).Scan(&profileID); err != nil {
			t.Fatalf("insert model profile %s: %v", def.key, err)
		}
		if _, err := s.pool.Exec(s.ctx, `
			INSERT INTO model_profile_bindings(model_profile_id, provider_model_id, priority, weight, enabled)
			VALUES ($1, $2, 100, 100, true)
			ON CONFLICT (model_profile_id, provider_model_id)
			DO UPDATE SET enabled = true
		`, profileID, modelID); err != nil {
			t.Fatalf("insert model profile binding %s: %v", def.key, err)
		}
	}
}

func (s *artifactPreviewSeed) insertAgentStep(t *testing.T, taskID string, index int, toolName, risk, status string) string {
	return s.insertAgentStepWithOutput(t, taskID, index, toolName, risk, status, `{}`)
}

func (s *artifactPreviewSeed) insertAgentStepWithOutput(t *testing.T, taskID string, index int, toolName, risk, status, output string) string {
	return s.insertAgentStepWithInputOutput(t, taskID, index, toolName, risk, status, `{}`, output)
}

func (s *artifactPreviewSeed) insertAgentStepWithInputOutput(t *testing.T, taskID string, index int, toolName, risk, status, input, output string) string {
	t.Helper()
	var id string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO agent_steps(task_id, step_index, tool_name, risk, status, requires_approval, input, dry_run_output, supervisor_decision, output, verifier_output)
		VALUES ($1, $2, $3, $4, $5, true, $6::jsonb, '{}', '{}', $7::jsonb, '{}')
		RETURNING id
	`, taskID, index, toolName, risk, status, input, output).Scan(&id); err != nil {
		t.Fatalf("insert agent step: %v", err)
	}
	return id
}

func (s *artifactPreviewSeed) insertAgentApproval(t *testing.T, taskID, stepID, approvalType string) string {
	t.Helper()
	var id string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO agent_approvals(task_id, step_id, approval_type, status, requested_payload, decision_payload)
		VALUES ($1, $2, $3, 'pending', '{}', '{}')
		RETURNING id
	`, taskID, stepID, approvalType).Scan(&id); err != nil {
		t.Fatalf("insert agent approval: %v", err)
	}
	return id
}

func assertAgentStepStatus(t *testing.T, seed *artifactPreviewSeed, stepID, want string) {
	t.Helper()
	var got string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT status FROM agent_steps WHERE id = $1`, stepID).Scan(&got); err != nil {
		t.Fatalf("select agent step status: %v", err)
	}
	if got != want {
		t.Fatalf("agent step %s status = %s, want %s", stepID, got, want)
	}
}

func assertTraceContains(t *testing.T, trace map[string]any, key, want string) {
	t.Helper()
	for _, got := range traceStringsFromAny(trace[key]) {
		if got == want {
			return
		}
	}
	t.Fatalf("trace[%s]=%v, want %s", key, trace[key], want)
}

func assertWorkflowStatus(t *testing.T, seed *artifactPreviewSeed, workflowRunID, want string) {
	t.Helper()
	var got string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT status FROM workflow_runs WHERE id = $1`, workflowRunID).Scan(&got); err != nil {
		t.Fatalf("select workflow status: %v", err)
	}
	if got != want {
		t.Fatalf("workflow %s status = %s, want %s", workflowRunID, got, want)
	}
}

func (s *artifactPreviewSeed) insertProductionShotAt(t *testing.T, workflowRunID string, index int, visual string) string {
	t.Helper()
	var id string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, workflow_run_id, shot_index, shot_no,
			start_tick, end_tick, duration_min_ticks, duration_max_ticks,
			visual, camera, motion, mood, image_prompt, video_prompt,
			status, review_status, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $7, $8, 450000, 450000, $6, 'camera', 'motion', 'mood', 'image prompt', 'video prompt',
		        'image_succeeded', 'approved', '{}')
		RETURNING id
	`, s.organizationID, s.projectID, workflowRunID, index, index+1, visual,
		int64(index)*450000, int64(index+1)*450000).Scan(&id); err != nil {
		t.Fatalf("insert production shot at %d: %v", index, err)
	}
	return id
}

func (s *artifactPreviewSeed) insertPromptTemplateForAgent(t *testing.T, templateKey string) string {
	t.Helper()
	var id string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO prompt_templates(
			organization_id, template_key, name, purpose, modality, task_type, scope, status, is_system, created_by
		)
		VALUES ($1, $2, 'Agent Prompt Test', 'agent_test', 'text', 'text.generate', 'organization', 'active', false, $3)
		RETURNING id::text
	`, s.organizationID, templateKey, s.ownerUserID).Scan(&id); err != nil {
		t.Fatalf("insert prompt template: %v", err)
	}
	return id
}

func (s *artifactPreviewSeed) insertTimelineClipForAgent(t *testing.T, timelineID string, index int, title string) string {
	t.Helper()
	var id string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO timeline_clips(
			organization_id, project_id, timeline_id, clip_index, title, enabled,
			start_tick, end_tick, source_duration_ticks, trim_start_tick, trim_end_tick, notes, metadata
		)
		VALUES ($1, $2, $3, $4, $5, true, $6, $7, 270000, 0, 270000, '', '{}')
		RETURNING id::text
	`, s.organizationID, s.projectID, timelineID, index, title, int64(index)*270000, int64(index+1)*270000).Scan(&id); err != nil {
		t.Fatalf("insert timeline clip: %v", err)
	}
	return id
}
