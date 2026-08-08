package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Einzieg/cineweave/internal/agent"
	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	editionpkg "github.com/Einzieg/cineweave/internal/edition"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

var editionPathParameterPattern = regexp.MustCompile(`\{([A-Za-z][A-Za-z0-9_]*)\}`)

type projectControlEditionAction struct {
	registration editionpkg.ProjectControlActionRegistration
	api          editionpkg.APIModuleRegistration
}

type projectControlEditionRuntimeHandler struct {
	executor *projectControlExecutor
	action   projectControlEditionAction
}

func projectControlEditionActionSet(
	ctx context.Context,
	runtime *editionpkg.Runtime,
) (map[string]projectControlEditionAction, error) {
	if runtime == nil {
		return map[string]projectControlEditionAction{}, nil
	}
	apiModules, err := runtime.ValidatedAPIModules(ctx)
	if err != nil {
		return nil, err
	}
	registrations, err := runtime.ValidatedProjectControlActions(ctx)
	if err != nil {
		return nil, err
	}
	apiByOperation := make(map[string]editionpkg.APIModuleRegistration, len(apiModules))
	for _, registration := range apiModules {
		apiByOperation[registration.OperationID] = registration
	}
	actions := make(map[string]projectControlEditionAction, len(registrations))
	for _, registration := range registrations {
		apiRegistration, exists := apiByOperation[registration.APIOperationID]
		if !exists {
			return nil, fmt.Errorf(
				"commercial project-control action %s references unavailable API operation %s",
				registration.Descriptor.Name,
				registration.APIOperationID,
			)
		}
		name := strings.TrimSpace(registration.Descriptor.Name)
		if _, exists := actions[name]; exists {
			return nil, fmt.Errorf("commercial project-control action %s is duplicated", name)
		}
		actions[name] = projectControlEditionAction{
			registration: registration,
			api:          apiRegistration,
		}
	}
	return actions, nil
}

func (e *projectControlExecutor) executeEditionAction(
	ctx context.Context,
	identity controlmcp.Identity,
	descriptor projectcontrol.Descriptor,
	action projectControlEditionAction,
	raw json.RawMessage,
) (projectcontrol.Result, error) {
	projectID, _, actionInput, err := decodeProjectControlAgentInput(raw, !descriptor.ReadOnly)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, principal, err := e.authorizedProjectID(
		ctx,
		identity.Principal,
		projectID,
		descriptorPermissions(descriptor)...,
	)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	if !projectControlDescriptorAllowsProjectKind(descriptor, string(project.ProjectKind)) {
		return projectControlFailure(
			"PROJECT_KIND_MISMATCH",
			"当前项目类型不支持此操作",
			false,
			map[string]any{"projectKind": project.ProjectKind, "action": descriptor.Name},
		), nil
	}
	return e.invokeEditionAction(ctx, principal, project, action, actionInput)
}

func (e *projectControlExecutor) invokeEditionAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	action projectControlEditionAction,
	raw json.RawMessage,
) (projectcontrol.Result, error) {
	request, err := buildEditionActionRequest(ctx, project, action, raw)
	if err != nil {
		return projectcontrol.Result{}, controlValidationError(err.Error())
	}
	resource, err := editionModuleResource(request, principal, action.api)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	if err := authorizeEditionAPIModule(
		ctx,
		e.server.currentEditionRuntime().Entitlements,
		e.server.authorizer,
		principal,
		resource,
		strings.TrimSpace(request.PathValue("billingAccountId")),
		action.api,
	); err != nil {
		if business, ok := projectControlBusinessError(err); ok {
			return business, nil
		}
		return projectcontrol.Result{}, err
	}
	return action.registration.Handler(ctx, editionpkg.ProjectControlActionRequest{
		Principal: editionpkg.APIPrincipal{
			UserID:           principal.UserID,
			OrganizationID:   principal.OrganizationID,
			BillingAccountID: strings.TrimSpace(request.PathValue("billingAccountId")),
		},
		ProjectID: project.ID,
		Input:     append(json.RawMessage(nil), raw...),
	})
}

func buildEditionActionRequest(
	ctx context.Context,
	project Project,
	action projectControlEditionAction,
	raw json.RawMessage,
) (*http.Request, error) {
	input := map[string]any{}
	if len(raw) > 0 && strings.TrimSpace(string(raw)) != "" && strings.TrimSpace(string(raw)) != "null" {
		if err := json.Unmarshal(raw, &input); err != nil {
			return nil, fmt.Errorf("动作参数必须是 JSON 对象")
		}
	}
	input["projectId"] = project.ID
	path := action.api.Pattern
	pathValues := make(map[string]string)
	for _, match := range editionPathParameterPattern.FindAllStringSubmatch(action.api.Pattern, -1) {
		parameter := match[1]
		value := strings.TrimSpace(fmt.Sprint(input[parameter]))
		if parameter == "projectId" {
			value = project.ID
		}
		if value == "" || value == "<nil>" {
			return nil, fmt.Errorf("%s 不能为空", parameter)
		}
		pathValues[parameter] = value
		path = strings.ReplaceAll(path, match[0], url.PathEscape(value))
		delete(input, parameter)
	}
	query := url.Values{}
	for _, parameter := range action.registration.QueryParameters {
		value, exists := input[parameter]
		if !exists || value == nil {
			continue
		}
		query.Set(parameter, fmt.Sprint(value))
		delete(input, parameter)
	}
	delete(input, "idempotencyKey")

	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	request, err := http.NewRequestWithContext(ctx, action.api.Method, path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range pathValues {
		request.SetPathValue(key, value)
	}
	return request, nil
}

func (h *projectControlEditionRuntimeHandler) Execute(
	ctx context.Context,
	request projectcontrol.DispatchRequest,
) (projectcontrol.DispatchOutcome, error) {
	if h == nil || h.executor == nil || h.executor.server == nil {
		return projectcontrol.DispatchOutcome{}, projectcontrol.NewRuntimeFailure(
			"COMMERCIAL_ACTION_UNAVAILABLE",
			"商业项目动作执行器不可用",
			true,
			nil,
		)
	}
	project, principal, err := h.executor.authorizedProjectID(
		ctx,
		auth.Principal{UserID: request.Command.ActorUserID},
		request.Command.ProjectID,
		descriptorPermissions(h.action.registration.Descriptor)...,
	)
	if err != nil {
		return projectcontrol.DispatchOutcome{}, projectcontrol.NewRuntimeFailure(
			"PROJECT_NOT_FOUND", "项目不存在", false, err,
		)
	}
	descriptor := h.action.registration.Descriptor
	if !projectControlDescriptorAllowsProjectKind(descriptor, string(project.ProjectKind)) {
		return projectcontrol.DispatchOutcome{}, projectcontrol.NewRuntimeFailure(
			"PROJECT_KIND_MISMATCH", "当前项目类型不支持此操作", false, nil,
		)
	}
	result, err := h.executor.invokeEditionAction(ctx, principal, project, h.action, request.Command.Input)
	if err != nil {
		return projectcontrol.DispatchOutcome{}, err
	}
	if result.Error != nil || result.Status != "succeeded" {
		code := "COMMERCIAL_ACTION_FAILED"
		message := firstNonEmpty(result.Summary, "商业功能操作失败")
		retryable := result.Retryable
		if result.Error != nil {
			code = firstNonEmpty(result.Error.Code, code)
			message = firstNonEmpty(result.Error.UserMessage, message)
			retryable = result.Error.Retryable
		}
		return projectcontrol.DispatchOutcome{}, projectcontrol.NewRuntimeFailure(code, message, retryable, nil)
	}
	agentResult := agentToolResult{
		Name: descriptor.Name, Label: descriptor.Label, Status: "succeeded",
		Summary: result.Summary, Data: rawObject(result.Data),
	}
	output, err := json.Marshal(agentResult)
	if err != nil {
		return projectcontrol.DispatchOutcome{}, err
	}
	return projectcontrol.DispatchOutcome{Output: output}, nil
}

func projectControlDescriptorAllowsProjectKind(
	descriptor projectcontrol.Descriptor,
	projectKind string,
) bool {
	if len(descriptor.ProjectKinds) == 0 {
		return true
	}
	for _, allowed := range descriptor.ProjectKinds {
		if strings.EqualFold(strings.TrimSpace(allowed), strings.TrimSpace(projectKind)) {
			return true
		}
	}
	return false
}

func projectControlAgentToolFromDescriptor(descriptor projectcontrol.Descriptor) agent.AgentTool {
	exportToMCP := descriptor.ExportToMCP
	return agent.AgentTool{
		Name: descriptor.Name, Version: descriptor.Version, Label: descriptor.Label,
		Description: descriptor.Description, Risk: descriptor.Risk,
		Permission: descriptor.Permission, Permissions: append([]string(nil), descriptor.Permissions...),
		ProjectKinds:     append([]string(nil), descriptor.ProjectKinds...),
		InputSchema:      append(json.RawMessage(nil), descriptor.InputSchema...),
		OutputSchema:     append(json.RawMessage(nil), descriptor.OutputSchema...),
		RequiresApproval: descriptor.RequiresApproval, Effects: descriptor.Effects,
		ActivityVisibility: descriptor.ActivityVisibility, ExportToMCP: &exportToMCP,
	}
}
