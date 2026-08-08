package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/agent"
	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

type projectControlReadAction func(
	context.Context,
	auth.Principal,
	Project,
	json.RawMessage,
) (agentToolResult, error)

func projectControlReadActionSet(server *Server) map[string]projectControlReadAction {
	return map[string]projectControlReadAction{
		"commerce.project.read_summary":    server.executeCommerceProjectSummaryReadAction,
		"commerce.product.get":             server.executeCommerceProductGetReadAction,
		"commerce.product.references.list": server.executeCommerceProductReferencesReadAction,
		"commerce.product.versions.list":   server.executeCommerceProductVersionsReadAction,
		"commerce.script.derivation.get":   server.executeCommerceScriptDerivationGetReadAction,
		"commerce.script.get":              server.executeCommerceScriptGetReadAction,
		"commerce.script.list":             server.executeCommerceScriptListReadAction,
		"commerce.video.get":               server.executeCommerceVideoGetReadAction,
		"commerce.video.list":              server.executeCommerceVideoListReadAction,
		"commerce.video.options":           server.executeCommerceVideoOptionsReadAction,
	}
}

func (e *projectControlExecutor) executeSharedReadAction(
	ctx context.Context,
	identity controlmcp.Identity,
	descriptor projectcontrol.Descriptor,
	action projectControlReadAction,
	raw json.RawMessage,
) (projectcontrol.Result, error) {
	projectID, _, actionInput, err := decodeProjectControlAgentInput(raw, false)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	if tool, exists := e.agentTools[descriptor.Name]; exists {
		if err := agent.ValidateToolInput(tool, actionInput); err != nil {
			return projectcontrol.Result{}, controlValidationError(err.Error())
		}
	}
	project, principal, err := e.authorizedProjectID(
		ctx, identity.Principal, projectID, descriptorPermissions(descriptor)...,
	)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	if !projectControlDescriptorAllowsProjectKind(descriptor, string(project.ProjectKind)) {
		return projectControlFailure(
			"PROJECT_KIND_MISMATCH", "当前项目类型不支持此操作", false,
			map[string]any{"projectKind": project.ProjectKind, "action": descriptor.Name},
		), nil
	}
	result, err := action(ctx, principal, project, actionInput)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	return projectControlResultFromAgentTool(result), nil
}

func decodeCommerceActionArguments(raw json.RawMessage) (map[string]any, error) {
	arguments := map[string]any{}
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return arguments, nil
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, controlValidationError("带货项目动作参数必须是 JSON 对象")
	}
	return arguments, nil
}

func (s *Server) resolveCommerceActionScriptUnitID(
	ctx context.Context,
	project Project,
	arguments map[string]any,
	argumentName string,
	required bool,
) (string, error) {
	explicitID := strings.TrimSpace(stringValueFromAny(arguments[argumentName]))
	stableOrdinal := agentIntArg(arguments, "stableOrdinal", 0, 0, 1000000)
	if explicitID == "" && stableOrdinal == 0 {
		if required {
			return "", controlValidationError("缺少广告脚本 ID 或 stableOrdinal")
		}
		return "", nil
	}
	scripts, err := s.listAgentCommerceScriptsForSelection(ctx, project)
	if err != nil {
		return "", err
	}
	selection, err := resolveCommerceAgentScriptSelection(arguments, argumentName, 0, scripts)
	if err != nil {
		return "", newAPIError(
			http.StatusConflict,
			"COMMERCE_SCRIPT_SELECTION_STALE",
			"广告脚本列表已变化或选择无效，请重新读取脚本列表后重试",
		)
	}
	return selection.ScriptUnitID, nil
}

func (s *Server) executeCommerceProjectSummaryReadAction(
	ctx context.Context,
	_ auth.Principal,
	project Project,
	raw json.RawMessage,
) (agentToolResult, error) {
	arguments, err := decodeCommerceActionArguments(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	summary, err := s.commerceAgentPlannerContext(ctx, project)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.project.read_summary", arguments, "已读取带货项目摘要", projectOperationalReadData(summary)), nil
}

func (s *Server) executeCommerceProductGetReadAction(
	ctx context.Context,
	_ auth.Principal,
	project Project,
	raw json.RawMessage,
) (agentToolResult, error) {
	arguments, err := decodeCommerceActionArguments(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	item, err := s.commerceCatalog.GetProduct(ctx, s.db, project.OrganizationID, project.ID)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.product.get", arguments, "已读取商品配置", projectOperationalReadData(item)), nil
}

func (s *Server) executeCommerceProductVersionsReadAction(
	ctx context.Context,
	_ auth.Principal,
	project Project,
	raw json.RawMessage,
) (agentToolResult, error) {
	arguments, err := decodeCommerceActionArguments(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	items, err := s.commerceCatalog.ListProductVersions(ctx, s.db, project.OrganizationID, project.ID)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.product.versions.list", arguments, fmt.Sprintf("已读取 %d 个商品版本", len(items)), projectOperationalReadData(map[string]any{"items": items})), nil
}

func (s *Server) executeCommerceProductReferencesReadAction(
	ctx context.Context,
	_ auth.Principal,
	project Project,
	raw json.RawMessage,
) (agentToolResult, error) {
	arguments, err := decodeCommerceActionArguments(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	items, err := s.listCommerceProductReferencesCore(ctx, project, agentStringArg(arguments, "status"))
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.product.references.list", arguments, fmt.Sprintf("已读取 %d 张商品参考图", len(items)), projectOperationalReadData(map[string]any{"items": items})), nil
}

func (s *Server) executeCommerceScriptListReadAction(
	ctx context.Context,
	_ auth.Principal,
	project Project,
	raw json.RawMessage,
) (agentToolResult, error) {
	arguments, err := decodeCommerceActionArguments(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	items, err := s.listCommerceScriptUnitsCore(
		ctx, project, agentStringArg(arguments, "status"), agentStringArg(arguments, "cursor"),
		agentIntArg(arguments, "limit", 50, 1, 200), true,
	)
	if err != nil {
		return agentToolResult{}, err
	}
	data := projectOperationalReadData(items)
	annotateAgentCommerceScriptList(data, items)
	return agentToolOK("commerce.script.list", arguments, fmt.Sprintf("已读取 %d 条广告脚本", len(items.Items)), data), nil
}

func (s *Server) executeCommerceScriptGetReadAction(
	ctx context.Context,
	_ auth.Principal,
	project Project,
	raw json.RawMessage,
) (agentToolResult, error) {
	arguments, err := decodeCommerceActionArguments(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	scriptUnitID, err := s.resolveCommerceActionScriptUnitID(ctx, project, arguments, "scriptUnitId", true)
	if err != nil {
		return agentToolResult{}, err
	}
	item, err := s.commerceCatalog.GetScriptUnit(ctx, s.db, project.OrganizationID, project.ID, scriptUnitID)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.script.get", arguments, "已读取广告脚本", projectOperationalReadData(item)), nil
}

func (s *Server) executeCommerceScriptDerivationGetReadAction(
	ctx context.Context,
	_ auth.Principal,
	project Project,
	raw json.RawMessage,
) (agentToolResult, error) {
	arguments, err := decodeCommerceActionArguments(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	batchID := strings.TrimSpace(agentStringArg(arguments, "batchId"))
	if batchID == "" {
		return agentToolResult{}, controlValidationError("batchId 不能为空")
	}
	item, err := s.commerceDerivations.GetBatch(
		ctx, s.db, project.OrganizationID, project.ID, batchID,
		strings.EqualFold(agentStringArg(arguments, "include"), "lineage"),
	)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.script.derivation.get", arguments, "已读取脚本裂变批次", projectOperationalReadData(item)), nil
}

func (s *Server) executeCommerceVideoOptionsReadAction(
	ctx context.Context,
	_ auth.Principal,
	project Project,
	raw json.RawMessage,
) (agentToolResult, error) {
	arguments, err := decodeCommerceActionArguments(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	if _, err := s.resolveCommerceActionScriptUnitID(ctx, project, arguments, "scriptUnitId", false); err != nil {
		return agentToolResult{}, err
	}
	options, err := s.commerceDirect.Options(ctx, s.db, project.OrganizationID, project.ID)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.video.options", arguments, "已读取视频生成选项", projectOperationalReadData(options)), nil
}

func (s *Server) executeCommerceVideoListReadAction(
	ctx context.Context,
	_ auth.Principal,
	project Project,
	raw json.RawMessage,
) (agentToolResult, error) {
	arguments, err := decodeCommerceActionArguments(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	scriptUnitID, err := s.resolveCommerceActionScriptUnitID(ctx, project, arguments, "scriptUnitId", false)
	if err != nil {
		return agentToolResult{}, err
	}
	items, err := s.listCommerceDirectVideosCore(ctx, project, scriptUnitID)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.video.list", arguments, fmt.Sprintf("已读取 %d 个视频任务", len(items)), projectOperationalReadData(map[string]any{"items": items})), nil
}

func (s *Server) executeCommerceVideoGetReadAction(
	ctx context.Context,
	_ auth.Principal,
	project Project,
	raw json.RawMessage,
) (agentToolResult, error) {
	arguments, err := decodeCommerceActionArguments(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	jobID := strings.TrimSpace(agentStringArg(arguments, "jobId"))
	if jobID == "" {
		return agentToolResult{}, controlValidationError("jobId 不能为空")
	}
	item, err := s.getCommerceDirectVideoCore(ctx, project, jobID)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.video.get", arguments, "已读取视频任务", projectOperationalReadData(item)), nil
}
