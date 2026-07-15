package api

import (
	"encoding/json"
	"testing"

	"github.com/Einzieg/cineweave/internal/agent"
)

func TestParseScriptAgentPlan(t *testing.T) {
	plan, err := parseScriptAgentPlan("```json\n{\"message\":\"准备查询\",\"toolCalls\":[{\"name\":\"list_sources\",\"arguments\":{\"limit\":5}}]}\n```")
	if err != nil {
		t.Fatalf("parse plan: %v", err)
	}
	if plan.Message != "准备查询" {
		t.Fatalf("message = %q", plan.Message)
	}
	if len(plan.ToolCalls) != 1 {
		t.Fatalf("tool call count = %d", len(plan.ToolCalls))
	}
	if plan.ToolCalls[0].Name != "list_sources" {
		t.Fatalf("tool name = %q", plan.ToolCalls[0].Name)
	}
	if got := agentIntArg(plan.ToolCalls[0].Arguments, "limit", 0, 0, 100); got != 5 {
		t.Fatalf("limit = %d", got)
	}
}

func TestParseScriptAgentPlanSupportsArray(t *testing.T) {
	plan, err := parseScriptAgentPlan(`[{"name":"get_project_status"},{"name":"start_production_action","arguments":{"action":"extract_events"}}]`)
	if err != nil {
		t.Fatalf("parse plan: %v", err)
	}
	if len(plan.ToolCalls) != 2 {
		t.Fatalf("tool call count = %d", len(plan.ToolCalls))
	}
	if plan.ToolCalls[0].Arguments == nil {
		t.Fatal("arguments were not normalized")
	}
}

func TestScriptAgentAllowsMutation(t *testing.T) {
	if !scriptAgentAllowsMutation("请开始提取第一卷第一节的事件") {
		t.Fatal("expected Chinese mutation intent")
	}
	if !scriptAgentAllowsMutation("run full production") {
		t.Fatal("expected English mutation intent")
	}
	if scriptAgentAllowsMutation("现在项目到哪一步了？") {
		t.Fatal("unexpected mutation intent")
	}
}

func TestAgentToolErrorIncludesRetryableAndNextActions(t *testing.T) {
	result := agentToolError("provider.test_model", map[string]any{}, apiError{
		Status:    503,
		Code:      "PROVIDER_SERVICE_UNAVAILABLE",
		Message:   "provider service is not configured",
		Retryable: true,
	})
	if !result.Retryable {
		t.Fatal("expected retryable tool error")
	}
	if len(result.NextActions) == 0 {
		t.Fatalf("expected next actions: %+v", result)
	}
	if result.NextActions[0].Tool != "provider.list_status" {
		t.Fatalf("next action = %+v", result.NextActions[0])
	}
}

func TestAgentReferenceArgsIgnorePlannerPlaceholders(t *testing.T) {
	validSourceID := "3d81d715-0d3a-42b1-95dd-f32a55576f93"
	args := map[string]any{
		"sourceId": "{{上一步确认的小说sourceId}}",
		"content":  "Hello {{ input.prompt }}",
		"options": map[string]any{
			"sourceId":   "{{sourceId}}",
			"chapterIds": []any{"{{chapterId}}", "3d81d715-0d3a-42b1-95dd-f32a55576f93"},
			"prompt":     "保留 {{ input.prompt }} 模板变量",
		},
	}

	if got := agentReferenceStringArg(args, "sourceId"); got != "" {
		t.Fatalf("sourceId = %q, want empty placeholder", got)
	}
	if got := agentStringArg(args, "content"); got != "Hello {{ input.prompt }}" {
		t.Fatalf("content = %q, want template text unchanged", got)
	}
	options := cleanAgentReferenceOptions(agentMapArg(args, "options"))
	if _, ok := options["sourceId"]; ok {
		t.Fatalf("placeholder sourceId was not removed: %+v", options)
	}
	chapterIDs := stringSliceFromAny(options["chapterIds"])
	if len(chapterIDs) != 1 || chapterIDs[0] != "3d81d715-0d3a-42b1-95dd-f32a55576f93" {
		t.Fatalf("chapterIds = %+v", chapterIDs)
	}
	if got := options["prompt"]; got != "保留 {{ input.prompt }} 模板变量" {
		t.Fatalf("prompt = %v, want unchanged", got)
	}
	for _, placeholder := range []string{
		"<由source.list返回的小说原文sourceId>",
		"not-a-uuid",
	} {
		if got := agentReferenceStringArg(map[string]any{"sourceId": placeholder}, "sourceId"); got != "" {
			t.Fatalf("sourceId = %q for placeholder %q, want empty", got, placeholder)
		}
	}
	if got := agentReferenceStringArg(map[string]any{"sourceId": validSourceID}, "sourceId"); got != validSourceID {
		t.Fatalf("sourceId = %q, want %q", got, validSourceID)
	}
}

func TestValidateAgentRuntimeArgumentsRejectsUnresolvedValues(t *testing.T) {
	for _, args := range []map[string]any{
		{"sourceId": "<由source.list返回的小说原文sourceId>"},
		{"sourceId": "not-a-uuid"},
		{"patch": map[string]any{"content": "<从读取到的原文全文中移除第1节后的完整正文>"}},
		{"chapterIds": []any{"3d81d715-0d3a-42b1-95dd-f32a55576f93", "not-a-uuid"}},
	} {
		if err := validateAgentRuntimeArguments(args); err == nil {
			t.Fatalf("args = %+v, want validation error", args)
		}
	}
	if err := validateAgentRuntimeArguments(map[string]any{
		"sourceId": "3d81d715-0d3a-42b1-95dd-f32a55576f93",
		"patch":    map[string]any{"content": "Hello {{ input.prompt }}"},
	}); err != nil {
		t.Fatalf("valid runtime args: %v", err)
	}
}

func TestAgentRuntimeSafetySwitches(t *testing.T) {
	t.Setenv("CINEWEAVE_AGENT_KILL_SWITCH", "true")
	if !agentGlobalKillSwitchEnabled() {
		t.Fatal("expected kill switch enabled")
	}
	if !agentToolReadOnly("project.read_summary") {
		t.Fatal("project.read_summary should be read-only")
	}
	if agentToolReadOnly("workflow.start") {
		t.Fatal("workflow.start should not be read-only")
	}

	t.Setenv("CINEWEAVE_AGENT_MAX_ACTIVE_TASKS_PER_PROJECT", "7")
	if got := agentProjectTaskConcurrencyLimit(); got != 7 {
		t.Fatalf("concurrency limit = %d", got)
	}
	t.Setenv("CINEWEAVE_AGENT_MAX_ACTIVE_TASKS_PER_PROJECT", "0")
	if got := agentProjectTaskConcurrencyLimit(); got != 0 {
		t.Fatalf("disabled concurrency limit = %d", got)
	}
}

func TestAgentPermissionModeForTask(t *testing.T) {
	task := AgentTask{
		Mode:        string(agent.TaskModeSupervised),
		Constraints: json.RawMessage(`{"permissionMode":"full_access"}`),
	}
	if got := agentPermissionModeForTask(task); got != agent.PermissionModeFullAccess {
		t.Fatalf("permission mode = %s, want full_access", got)
	}

	task = AgentTask{
		Mode:        string(agent.TaskModeAutoLowRisk),
		Constraints: json.RawMessage(`{}`),
	}
	if got := agentPermissionModeForTask(task); got != agent.PermissionModeAutoApprove {
		t.Fatalf("permission mode = %s, want auto_approve", got)
	}

	task = AgentTask{
		Mode:        string(agent.TaskModeSupervised),
		Constraints: json.RawMessage(`{"permissionMode":"unknown"}`),
	}
	if got := agentPermissionModeForTask(task); got != agent.PermissionModeRequireApproval {
		t.Fatalf("permission mode = %s, want require_approval", got)
	}
}
