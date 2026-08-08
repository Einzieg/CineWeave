package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/agent"
	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	editionpkg "github.com/Einzieg/cineweave/internal/edition"
	"github.com/Einzieg/cineweave/internal/observability"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/jackc/pgx/v5"
)

const (
	projectControlDefaultPageSize = 20
	projectControlMaximumPageSize = 50
	projectControlMaximumWait     = 45 * time.Second
)

var (
	emptyControlInputSchema = json.RawMessage(`{"type":"object","additionalProperties":false}`)
	controlOutputSchema     = json.RawMessage(`{"type":"object","additionalProperties":true}`)
)

type projectControlExecutor struct {
	server         *Server
	repository     *projectcontrol.Repository
	registry       *projectcontrol.Registry
	runtime        *projectcontrol.RuntimeRegistry
	agentTools     map[string]agent.AgentTool
	readActions    map[string]projectControlReadAction
	syncActions    map[string]projectControlSyncAction
	asyncActions   map[string]projectControlAsyncAction
	editionActions map[string]projectControlEditionAction
}

func newProjectControlExecutor(server *Server) (*projectControlExecutor, error) {
	if server == nil || server.db == nil || server.auth == nil || server.authorizer == nil {
		return nil, fmt.Errorf("project control API dependencies are incomplete")
	}
	descriptors, agentTools, err := projectControlDescriptorSet()
	if err != nil {
		return nil, err
	}
	editionActions, err := projectControlEditionActionSet(context.Background(), server.currentEditionRuntime())
	if err != nil {
		return nil, err
	}
	for _, action := range editionActions {
		descriptors = append(descriptors, action.registration.Descriptor.Clone())
	}
	sort.Slice(descriptors, func(i, j int) bool {
		if descriptors[i].Name == descriptors[j].Name {
			return descriptors[i].Version < descriptors[j].Version
		}
		return descriptors[i].Name < descriptors[j].Name
	})
	registry, err := projectcontrol.NewRegistry(descriptors...)
	if err != nil {
		return nil, err
	}
	runtimeRegistry, err := projectcontrol.NewRuntimeRegistry()
	if err != nil {
		return nil, err
	}
	executor := &projectControlExecutor{
		server:         server,
		repository:     projectcontrol.NewRepository(server.db),
		registry:       registry,
		runtime:        runtimeRegistry,
		agentTools:     agentTools,
		readActions:    projectControlReadActionSet(server),
		syncActions:    projectControlSyncActionSet(server),
		asyncActions:   projectControlAsyncActionSet(server),
		editionActions: editionActions,
	}
	for _, descriptor := range descriptors {
		if descriptor.ReadOnly {
			continue
		}
		if _, isSyncAction := executor.syncActions[descriptor.Name]; isSyncAction {
			continue
		}
		if action, isAsyncAction := executor.asyncActions[descriptor.Name]; isAsyncAction {
			if err := runtimeRegistry.Register(descriptor, &projectControlSharedRuntimeHandler{
				executor: executor, descriptor: descriptor, action: action,
			}); err != nil {
				return nil, err
			}
			continue
		}
		if editionAction, isEditionAction := editionActions[descriptor.Name]; isEditionAction {
			if err := runtimeRegistry.Register(descriptor, &projectControlEditionRuntimeHandler{
				executor: executor, action: editionAction,
			}); err != nil {
				return nil, err
			}
			continue
		}
		if _, isAgentAction := agentTools[descriptor.Name]; isAgentAction {
			return nil, fmt.Errorf("project control action %s has no shared runtime implementation", descriptor.Name)
		}
	}
	return executor, nil
}

// ProjectControlDescriptors returns the deterministic descriptor surface used
// by MCP, the embedded assistant adapter, and generated contract artifacts.
// It has no runtime or database dependency so release tooling can invoke it
// from a clean source checkout.
func ProjectControlDescriptors() ([]projectcontrol.Descriptor, error) {
	descriptors, _, err := projectControlDescriptorSet()
	if err != nil {
		return nil, err
	}
	result := make([]projectcontrol.Descriptor, len(descriptors))
	for index, descriptor := range descriptors {
		result[index] = descriptor.Clone()
	}
	return result, nil
}

func projectControlDescriptorSet() ([]projectcontrol.Descriptor, map[string]agent.AgentTool, error) {
	agentDescriptors, agentTools, err := projectControlAgentDescriptors()
	if err != nil {
		return nil, nil, err
	}
	descriptors := append(projectControlReadDescriptors(), agentDescriptors...)
	sort.Slice(descriptors, func(i, j int) bool {
		if descriptors[i].Name == descriptors[j].Name {
			return descriptors[i].Version < descriptors[j].Version
		}
		return descriptors[i].Name < descriptors[j].Name
	})
	return descriptors, agentTools, nil
}

func (s *Server) ProjectControlRuntimeRegistry() *projectcontrol.RuntimeRegistry {
	if s == nil || s.projectControl == nil {
		return nil
	}
	return s.projectControl.runtime
}

func (e *projectControlExecutor) Descriptors() []projectcontrol.Descriptor {
	if e == nil || e.registry == nil {
		return []projectcontrol.Descriptor{}
	}
	return e.registry.List()
}

func (e *projectControlExecutor) ActiveCommandCount(ctx context.Context, identity controlmcp.Identity) (int, error) {
	return e.repository.ActiveCount(ctx, identity.Principal.UserID)
}

func (e *projectControlExecutor) Execute(ctx context.Context, identity controlmcp.Identity, name string, raw json.RawMessage) (projectcontrol.Result, error) {
	descriptor, ok := e.registry.Get(name)
	if !ok {
		return projectControlFailure("ACTION_NOT_FOUND", "项目控制动作不存在", false, nil), nil
	}
	var result projectcontrol.Result
	var err error
	switch name {
	case "identity.me":
		result, err = e.identityMe(ctx, identity, raw)
	case "organization.list":
		result, err = e.organizationList(ctx, identity, raw)
	case "workspace.list":
		result, err = e.workspaceList(ctx, identity, raw)
	case "project.list":
		result, err = e.projectList(ctx, identity, raw)
	case "project.get":
		result, err = e.projectGet(ctx, identity, raw)
	case "project.context":
		result, err = e.projectContext(ctx, identity, raw)
	case "project.capabilities":
		result, err = e.projectCapabilities(ctx, identity, raw)
	case "project.production_status":
		result, err = e.projectProductionStatus(ctx, identity, raw)
	case "project.read_summary":
		result, err = e.projectReadSummary(ctx, identity, raw)
	case "project.deletion_impact":
		result, err = e.projectDeletionImpact(ctx, identity, raw)
	case "project.task_activity":
		result, err = e.projectTaskActivity(ctx, identity, raw)
	case "source.list":
		result, err = e.sourceList(ctx, identity, raw)
	case "source.list_chapters":
		result, err = e.sourceListChapters(ctx, identity, raw)
	case "script.list":
		result, err = e.scriptList(ctx, identity, raw)
	case "script.get":
		result, err = e.scriptGet(ctx, identity, raw)
	case "asset.list":
		result, err = e.assetList(ctx, identity, raw)
	case "asset.get":
		result, err = e.assetGet(ctx, identity, raw)
	case "asset.impact":
		result, err = e.assetImpact(ctx, identity, raw)
	case "asset.reference.list":
		result, err = e.assetReferenceList(ctx, identity, raw)
	case "artifact.list":
		result, err = e.artifactList(ctx, identity, raw)
	case "artifact.get":
		result, err = e.artifactGet(ctx, identity, raw)
	case "artifact.preview_url":
		result, err = e.artifactPreviewURL(ctx, identity, raw)
	case "export.download_url":
		result, err = e.exportDownloadURL(ctx, identity, raw)
	case "final_video.download_url":
		result, err = e.finalVideoDownloadURL(ctx, identity, raw)
	case "storyboard.list":
		result, err = e.storyboardList(ctx, identity, raw)
	case "workflow.read_runs":
		result, err = e.workflowReadRuns(ctx, identity, raw)
	case "workflow.read_nodes":
		result, err = e.workflowReadNodes(ctx, identity, raw)
	case "workflow.read_shots":
		result, err = e.workflowReadShots(ctx, identity, raw)
	case "review.list_items":
		result, err = e.reviewListItems(ctx, identity, raw)
	case "provider.list_status":
		result, err = e.providerListStatus(ctx, identity, raw)
	case "shot.status":
		result, err = e.shotStatus(ctx, identity, raw)
	case "shot_asset.list_requirements":
		result, err = e.shotAssetListRequirements(ctx, identity, raw)
	case "prompt.render_test":
		result, err = e.promptRenderTest(ctx, identity, raw)
	case "content.describe":
		result, err = e.contentDescribe(ctx, identity, raw)
	case "content.read":
		result, err = e.contentRead(ctx, identity, raw)
	case "content.write.begin":
		result, err = e.contentWriteBegin(ctx, identity, raw)
	case "content.write.chunk":
		result, err = e.contentWriteChunk(ctx, identity, raw)
	case "content.write.commit":
		result, err = e.contentWriteCommit(ctx, identity, raw)
	case "content.write.abort":
		result, err = e.contentWriteAbort(ctx, identity, raw)
	case "control.command.list":
		result, err = e.commandList(ctx, identity, raw)
	case "control.command.get":
		result, err = e.commandGet(ctx, identity, raw)
	case "control.command.events":
		result, err = e.commandEvents(ctx, identity, raw)
	case "control.command.wait":
		result, err = e.commandWait(ctx, identity, raw)
	case "control.command.cancel":
		result, err = e.commandCancel(ctx, identity, raw)
	case "control.command.retry":
		result, err = e.commandRetry(ctx, identity, raw)
	case "control.command.resolve":
		result, err = e.commandResolve(ctx, identity, raw)
	default:
		if readAction, exists := e.readActions[name]; exists {
			result, err = e.executeSharedReadAction(ctx, identity, descriptor, readAction, raw)
		} else if editionAction, exists := e.editionActions[name]; exists {
			result, err = e.executeEditionAction(ctx, identity, descriptor, editionAction, raw)
		} else if tool, exists := e.agentTools[name]; exists {
			result, err = e.executeAgentAction(ctx, identity, descriptor, tool, raw)
		} else {
			return projectControlFailure("ACTION_NOT_IMPLEMENTED", "项目控制动作尚未接入统一执行器", false, map[string]any{"action": name}), nil
		}
	}
	if err == nil {
		return result, nil
	}
	if business, ok := projectControlBusinessError(err); ok {
		return business, nil
	}
	return projectcontrol.Result{}, err
}

// ProjectControlDescriptorsForRuntime returns the assembled Core plus optional
// Commercial descriptor surface. Release assembly tooling uses this to produce
// a catalog that matches the exact edition runtime.
func ProjectControlDescriptorsForRuntime(ctx context.Context, runtime *editionpkg.Runtime) ([]projectcontrol.Descriptor, error) {
	descriptors, _, err := projectControlDescriptorSet()
	if err != nil {
		return nil, err
	}
	actions, err := projectControlEditionActionSet(ctx, runtime)
	if err != nil {
		return nil, err
	}
	for _, action := range actions {
		descriptors = append(descriptors, action.registration.Descriptor.Clone())
	}
	sort.Slice(descriptors, func(i, j int) bool {
		if descriptors[i].Name == descriptors[j].Name {
			return descriptors[i].Version < descriptors[j].Version
		}
		return descriptors[i].Name < descriptors[j].Name
	})
	return descriptors, nil
}

type controlPageInput struct {
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor"`
}

type controlOrganizationInput struct {
	OrganizationID string `json:"organizationId"`
	Limit          int    `json:"limit"`
	Cursor         string `json:"cursor"`
}

type controlProjectListInput struct {
	OrganizationID string `json:"organizationId"`
	WorkspaceID    string `json:"workspaceId"`
	Limit          int    `json:"limit"`
	Cursor         string `json:"cursor"`
}

type controlProjectInput struct {
	ProjectID string `json:"projectId"`
}

type controlCommandListInput struct {
	ProjectID      string   `json:"projectId"`
	Statuses       []string `json:"statuses"`
	ControllerType string   `json:"controllerType"`
	CreatedAfter   string   `json:"createdAfter"`
	View           string   `json:"view"`
	Limit          int      `json:"limit"`
	Cursor         string   `json:"cursor"`
}

type controlCommandInput struct {
	CommandID string `json:"commandId"`
}

type controlCommandEventsInput struct {
	CommandID   string `json:"commandId"`
	AfterCursor string `json:"afterCursor"`
	Limit       int    `json:"limit"`
}

type controlCommandWaitInput struct {
	CommandID      string `json:"commandId"`
	AfterCursor    string `json:"afterCursor"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

type controlCommandCancelInput struct {
	CommandID        string `json:"commandId"`
	ExpectedRevision int64  `json:"expectedCommandRevision"`
	IdempotencyKey   string `json:"idempotencyKey"`
	Reason           string `json:"reason"`
}

type controlCommandRetryInput struct {
	CommandID        string `json:"commandId"`
	ExpectedRevision int64  `json:"expectedCommandRevision"`
	IdempotencyKey   string `json:"idempotencyKey"`
}

type controlCommandResolveInput struct {
	CommandID        string          `json:"commandId"`
	PromptID         string          `json:"promptId"`
	ExpectedRevision int64           `json:"expectedCommandRevision"`
	IdempotencyKey   string          `json:"idempotencyKey"`
	Answer           json.RawMessage `json:"answer"`
}

func (e *projectControlExecutor) identityMe(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	if err := decodeControlInput(raw, &struct{}{}); err != nil {
		return projectcontrol.Result{}, err
	}
	user, err := e.server.auth.Me(ctx, identity.Principal)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	organizations, err := e.server.listAccessibleOrganizations(ctx, identity.Principal, 100, "")
	if err != nil {
		return projectcontrol.Result{}, err
	}
	return projectControlSuccess("已读取当前用户身份", map[string]any{
		"user":          user,
		"organizations": organizations.Items,
		"controlKey":    identity.Key,
	}), nil
}

func (e *projectControlExecutor) organizationList(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	var input controlPageInput
	if err := decodeControlInput(raw, &input); err != nil {
		return projectcontrol.Result{}, err
	}
	page, err := e.server.listAccessibleOrganizations(ctx, identity.Principal, boundedControlLimit(input.Limit), input.Cursor)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlSuccess(fmt.Sprintf("找到 %d 个可访问组织", len(page.Items)), map[string]any{"items": page.Items})
	result.NextCursor = page.NextCursor
	return result, nil
}

func (e *projectControlExecutor) workspaceList(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	var input controlOrganizationInput
	if err := decodeControlInput(raw, &input); err != nil {
		return projectcontrol.Result{}, err
	}
	if strings.TrimSpace(input.OrganizationID) == "" {
		return projectcontrol.Result{}, controlValidationError("organizationId 不能为空")
	}
	page, err := e.server.listAccessibleWorkspaces(ctx, identity.Principal, input.OrganizationID, boundedControlLimit(input.Limit), input.Cursor)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlSuccess(fmt.Sprintf("找到 %d 个可访问工作区", len(page.Items)), map[string]any{"items": page.Items})
	result.NextCursor = page.NextCursor
	return result, nil
}

func (e *projectControlExecutor) projectList(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	var input controlProjectListInput
	if err := decodeControlInput(raw, &input); err != nil {
		return projectcontrol.Result{}, err
	}
	if strings.TrimSpace(input.OrganizationID) == "" {
		return projectcontrol.Result{}, controlValidationError("organizationId 不能为空")
	}
	page, err := e.server.listAccessibleProjects(ctx, identity.Principal, input.OrganizationID, input.WorkspaceID, boundedControlLimit(input.Limit), input.Cursor)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlSuccess(fmt.Sprintf("找到 %d 个可访问项目", len(page.Items)), map[string]any{"items": page.Items})
	result.NextCursor = page.NextCursor
	return result, nil
}

func (e *projectControlExecutor) projectGet(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	project, _, err := e.authorizedProject(ctx, identity.Principal, raw, authz.PermissionProjectRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	return projectControlSuccess("已读取项目", map[string]any{"project": project}), nil
}

func (e *projectControlExecutor) sourceList(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, actionInput, arguments, err := e.decodeNativeAgentReadInput("source.list", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "source.list", projectID, authz.PermissionSourceRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	var input sourceListActionInput
	if err := json.Unmarshal(actionInput, &input); err != nil {
		return projectcontrol.Result{}, controlValidationError("source.list 输入格式无效")
	}
	page, err := e.server.listProjectSourcesAction(ctx, project, input)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlResultFromAgentTool(sourceListAgentResult(arguments, page))
	result.NextCursor = page.NextCursor
	return result, nil
}

func (e *projectControlExecutor) sourceListChapters(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, actionInput, arguments, err := e.decodeNativeAgentReadInput("source.list_chapters", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "source.list_chapters", projectID, authz.PermissionSourceRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	var input sourceListChaptersActionInput
	if err := json.Unmarshal(actionInput, &input); err != nil {
		return projectcontrol.Result{}, controlValidationError("source.list_chapters 输入格式无效")
	}
	page, err := e.server.listSourceChaptersAction(ctx, project, input)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlResultFromAgentTool(sourceListChaptersAgentResult(arguments, page))
	result.NextCursor = page.NextCursor
	return result, nil
}

func (e *projectControlExecutor) scriptList(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, actionInput, arguments, err := e.decodeNativeAgentReadInput("script.list", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "script.list", projectID, authz.PermissionScriptRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	input, err := decodeScriptListActionInput(actionInput)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	page, err := e.server.listScriptsAction(ctx, project, input)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlResultFromAgentTool(scriptListAgentResult(arguments, page))
	result.NextCursor = page.NextCursor
	return result, nil
}

func (e *projectControlExecutor) scriptGet(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, actionInput, arguments, err := e.decodeNativeAgentReadInput("script.get", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "script.get", projectID, authz.PermissionScriptRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	input, err := decodeScriptGetActionInput(actionInput)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	page, err := e.server.getScriptAction(ctx, project, input)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlResultFromAgentTool(scriptGetAgentResult(arguments, page))
	result.NextCursor = page.NextEpisodeCursor
	return result, nil
}

func (e *projectControlExecutor) assetList(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, actionInput, arguments, err := e.decodeNativeAgentReadInput("asset.list", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "asset.list", projectID, authz.PermissionAssetRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	input, err := decodeAssetListActionInput(actionInput)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	page, err := e.server.listCanonicalAssetsAction(ctx, project, input)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlResultFromAgentTool(assetListAgentResult(arguments, page))
	result.NextCursor = page.NextCursor
	return result, nil
}

func (e *projectControlExecutor) assetGet(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, actionInput, arguments, err := e.decodeNativeAgentReadInput("asset.get", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "asset.get", projectID, authz.PermissionAssetRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	input, err := decodeAssetGetActionInput(actionInput)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	asset, err := e.server.getCanonicalAssetAction(ctx, project, input)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	return projectControlResultFromAgentTool(assetGetAgentResult(arguments, asset)), nil
}

func (e *projectControlExecutor) assetImpact(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, actionInput, arguments, err := e.decodeNativeAgentReadInput("asset.impact", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "asset.impact", projectID, authz.PermissionAssetRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	input, err := decodeAssetImpactActionInput(actionInput)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	impact, err := e.server.getCanonicalAssetImpactAction(ctx, project, input)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	return projectControlResultFromAgentTool(assetImpactAgentResult(arguments, impact)), nil
}

func (e *projectControlExecutor) assetReferenceList(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, actionInput, arguments, err := e.decodeNativeAgentReadInput("asset.reference.list", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "asset.reference.list", projectID, authz.PermissionAssetRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	input, err := decodeAssetReferenceListActionInput(actionInput)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	page, err := e.server.listAssetReferencesAction(ctx, project, input)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlResultFromAgentTool(assetReferenceListAgentResult(arguments, page))
	result.NextCursor = page.NextCursor
	return result, nil
}

func (e *projectControlExecutor) artifactList(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, actionInput, arguments, err := e.decodeNativeAgentReadInput("artifact.list", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "artifact.list", projectID, authz.PermissionArtifactRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	input, err := decodeArtifactListActionInput(actionInput)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	page, err := e.server.listProjectArtifactsAction(ctx, project, input)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlResultFromAgentTool(artifactListAgentResult(arguments, page))
	result.NextCursor = page.NextCursor
	return result, nil
}

func (e *projectControlExecutor) artifactGet(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, actionInput, arguments, err := e.decodeNativeAgentReadInput("artifact.get", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "artifact.get", projectID, authz.PermissionArtifactRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	input, err := decodeArtifactGetActionInput(actionInput)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	item, err := e.server.getProjectArtifactAction(ctx, project, input)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	return projectControlResultFromAgentTool(artifactGetAgentResult(arguments, item)), nil
}

func (e *projectControlExecutor) artifactPreviewURL(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, actionInput, arguments, err := e.decodeNativeAgentReadInput("artifact.preview_url", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "artifact.preview_url", projectID, authz.PermissionArtifactRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	input, err := decodeArtifactPreviewActionInput(actionInput)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	preview, err := e.server.createProjectArtifactPreviewAction(ctx, project, input)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	return projectControlResultFromAgentTool(artifactPreviewAgentResult(arguments, preview)), nil
}

func (e *projectControlExecutor) projectContext(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	project, principal, err := e.authorizedProject(ctx, identity.Principal, raw, authz.PermissionProjectRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	request := (&http.Request{}).WithContext(ctx)
	contextData := map[string]any{"project": project}
	status, statusErr := e.server.productionStatus(request, project)
	if statusErr == nil {
		contextData["productionStatus"] = status
	} else {
		contextData["productionStatusUnavailable"] = true
	}
	if e.server.authorizer.Authorize(ctx, principal, authz.PermissionWorkflowRead, authz.Resource{ProjectID: project.ID}) == nil {
		workflows, queryErr := e.recentWorkflowRuns(ctx, project.ID, 5)
		if queryErr != nil {
			return projectcontrol.Result{}, queryErr
		}
		contextData["recentWorkflowRuns"] = workflows
	}
	return projectControlSuccess("已读取项目上下文", contextData), nil
}

func (e *projectControlExecutor) projectCapabilities(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	project, principal, err := e.authorizedProject(ctx, identity.Principal, raw, authz.PermissionProjectRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	type capability struct {
		Action  string   `json:"action"`
		Label   string   `json:"label"`
		Allowed bool     `json:"allowed"`
		Reasons []string `json:"reasons"`
	}
	items := make([]capability, 0)
	for _, descriptor := range e.registry.List() {
		if descriptor.Scope != projectcontrol.ScopeProject {
			continue
		}
		entry := capability{Action: descriptor.Name, Label: descriptor.Label, Allowed: true, Reasons: []string{}}
		if !descriptorAllowsProjectKind(descriptor, string(project.ProjectKind)) {
			entry.Allowed = false
			entry.Reasons = append(entry.Reasons, "当前项目类型不支持该动作")
		}
		for _, permission := range descriptorPermissions(descriptor) {
			if err := e.server.authorizer.Authorize(ctx, principal, permission, authz.Resource{ProjectID: project.ID}); err != nil {
				entry.Allowed = false
				entry.Reasons = append(entry.Reasons, "缺少权限 "+permission)
			}
		}
		items = append(items, entry)
	}
	return projectControlSuccess("已计算当前项目可执行能力", map[string]any{
		"projectId":   project.ID,
		"projectKind": project.ProjectKind,
		"actions":     items,
	}), nil
}

func (e *projectControlExecutor) projectProductionStatus(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	project, _, err := e.authorizedProject(ctx, identity.Principal, raw, authz.PermissionProjectRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	status, err := e.server.productionStatus((&http.Request{}).WithContext(ctx), project)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	return projectControlSuccess("已读取项目生产状态", map[string]any{"productionStatus": status}), nil
}

func (e *projectControlExecutor) projectReadSummary(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, _, arguments, err := e.decodeNativeAgentReadInput("project.read_summary", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "project.read_summary", projectID, authz.PermissionProjectRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result, err := e.server.readProjectSummaryAction(ctx, project)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	return projectControlResultFromAgentTool(projectSummaryAgentResult(arguments, result)), nil
}

func (e *projectControlExecutor) projectDeletionImpact(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, _, _, err := e.decodeNativeAgentReadInput("project.deletion_impact", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "project.deletion_impact", projectID, authz.PermissionProjectDelete)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	impact, err := e.server.projectDeletionImpact(ctx, e.server.db, project)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	return projectControlSuccess("已读取项目删除影响", map[string]any{"impact": impact}), nil
}

func (e *projectControlExecutor) storyboardList(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, actionInput, arguments, err := e.decodeNativeAgentReadInput("storyboard.list", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "storyboard.list", projectID, authz.PermissionProjectRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	var input storyboardListActionInput
	if err := json.Unmarshal(actionInput, &input); err != nil {
		return projectcontrol.Result{}, controlValidationError("storyboard.list 输入格式无效")
	}
	page, err := e.server.listStoryboardShotsAction(ctx, project, input)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlResultFromAgentTool(storyboardListAgentResult(arguments, page))
	result.NextCursor = page.NextCursor
	return result, nil
}

func (e *projectControlExecutor) workflowReadRuns(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, actionInput, arguments, err := e.decodeNativeAgentReadInput("workflow.read_runs", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "workflow.read_runs", projectID, authz.PermissionWorkflowRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	var input workflowRunListActionInput
	if err := json.Unmarshal(actionInput, &input); err != nil {
		return projectcontrol.Result{}, controlValidationError("workflow.read_runs 输入格式无效")
	}
	page, err := e.server.listProjectWorkflowRunsAction(ctx, project, input)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlResultFromAgentTool(workflowRunListAgentResult(arguments, page))
	result.NextCursor = page.NextCursor
	return result, nil
}

func (e *projectControlExecutor) workflowReadNodes(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, actionInput, arguments, err := e.decodeNativeAgentReadInput("workflow.read_nodes", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "workflow.read_nodes", projectID, authz.PermissionWorkflowRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	var input workflowRunChildrenActionInput
	if err := json.Unmarshal(actionInput, &input); err != nil {
		return projectcontrol.Result{}, controlValidationError("workflow.read_nodes 输入格式无效")
	}
	page, err := e.server.listWorkflowNodesAction(ctx, project, input)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlResultFromAgentTool(workflowNodeListAgentResult(arguments, page))
	result.NextCursor = page.NextCursor
	return result, nil
}

func (e *projectControlExecutor) workflowReadShots(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, actionInput, arguments, err := e.decodeNativeAgentReadInput("workflow.read_shots", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "workflow.read_shots", projectID, authz.PermissionWorkflowRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	var input workflowRunChildrenActionInput
	if err := json.Unmarshal(actionInput, &input); err != nil {
		return projectcontrol.Result{}, controlValidationError("workflow.read_shots 输入格式无效")
	}
	page, err := e.server.listWorkflowShotsAction(ctx, project, input)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlResultFromAgentTool(workflowShotListAgentResult(arguments, page))
	result.NextCursor = page.NextCursor
	return result, nil
}

func (e *projectControlExecutor) reviewListItems(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, actionInput, arguments, err := e.decodeNativeAgentReadInput("review.list_items", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "review.list_items", projectID, authz.PermissionProjectRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	var input reviewItemListActionInput
	if err := json.Unmarshal(actionInput, &input); err != nil {
		return projectcontrol.Result{}, controlValidationError("review.list_items 输入格式无效")
	}
	page, err := e.server.listReviewItemsAction(ctx, project, input)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	status := firstNonEmpty(strings.TrimSpace(input.Status), "open")
	result := projectControlResultFromAgentTool(reviewItemListAgentResult(arguments, page, status))
	result.NextCursor = page.NextCursor
	return result, nil
}

func (e *projectControlExecutor) providerListStatus(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, _, arguments, err := e.decodeNativeAgentReadInput("provider.list_status", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "provider.list_status", projectID, authz.PermissionProviderRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	status, err := e.server.readProviderStatusAction(ctx, project)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	return projectControlResultFromAgentTool(providerStatusAgentResult(arguments, status)), nil
}

func (e *projectControlExecutor) shotStatus(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, actionInput, arguments, err := e.decodeNativeAgentReadInput("shot.status", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "shot.status", projectID, authz.PermissionWorkflowRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	var input shotStatusActionInput
	if err := json.Unmarshal(actionInput, &input); err != nil {
		return projectcontrol.Result{}, controlValidationError("shot.status 输入格式无效")
	}
	status, err := e.server.readShotStatusAction(ctx, project, input)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	return projectControlResultFromAgentTool(shotStatusAgentResult(arguments, status)), nil
}

func (e *projectControlExecutor) shotAssetListRequirements(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, actionInput, arguments, err := e.decodeNativeAgentReadInput("shot_asset.list_requirements", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "shot_asset.list_requirements", projectID, authz.PermissionAssetRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	var input shotAssetRequirementListActionInput
	if err := json.Unmarshal(actionInput, &input); err != nil {
		return projectcontrol.Result{}, controlValidationError("shot_asset.list_requirements 输入格式无效")
	}
	page, err := e.server.listShotAssetRequirementsAction(ctx, project, input)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlResultFromAgentTool(shotAssetRequirementListAgentResult(arguments, page))
	result.NextCursor = page.NextCursor
	return result, nil
}

func (e *projectControlExecutor) projectTaskActivity(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	var input struct {
		ProjectID string `json:"projectId"`
		Limit     int    `json:"limit"`
		Cursor    string `json:"cursor"`
	}
	if err := decodeControlInput(raw, &input); err != nil {
		return projectcontrol.Result{}, err
	}
	project, principal, err := e.authorizedProjectID(ctx, identity.Principal, input.ProjectID, authz.PermissionWorkflowRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	cursor, err := projectcontrol.DecodeCommandCursor(input.Cursor)
	if err != nil {
		return projectcontrol.Result{}, controlValidationError("cursor 无效")
	}
	filter := projectcontrol.ListCommandsFilter{
		ProjectID: project.ID,
		Limit:     boundedControlLimit(input.Limit),
	}
	if cursor != nil {
		filter.BeforeCreatedAt = &cursor.CreatedAt
		filter.BeforeID = cursor.ID
	}
	commands, err := e.repository.List(ctx, filter)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	workflows, err := e.recentWorkflowRuns(ctx, project.ID, boundedControlLimit(input.Limit))
	if err != nil {
		return projectcontrol.Result{}, err
	}
	_ = principal
	result := projectControlSuccess("已读取项目任务活动", map[string]any{
		"commands":     commands.Commands,
		"workflowRuns": workflows,
	})
	if commands.NextCursor != nil {
		result.NextCursor, err = projectcontrol.EncodeCommandCursor(*commands.NextCursor)
		if err != nil {
			return projectcontrol.Result{}, err
		}
	}
	return result, nil
}

func (e *projectControlExecutor) commandList(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	var input controlCommandListInput
	if err := decodeControlInput(raw, &input); err != nil {
		return projectcontrol.Result{}, err
	}
	filter := projectcontrol.ListCommandsFilter{
		ActorUserID: identity.Principal.UserID,
		ProjectID:   strings.TrimSpace(input.ProjectID),
		Limit:       boundedControlLimit(input.Limit),
	}
	input.View = strings.TrimSpace(input.View)
	if input.View != "" && input.View != "activity" {
		return projectcontrol.Result{}, controlValidationError("view 无效")
	}
	if input.View == "activity" {
		if filter.ProjectID == "" {
			return projectcontrol.Result{}, controlValidationError("任务活动视图必须指定 projectId")
		}
		filter.ActivityView = true
	}
	if input.ControllerType != "" {
		filter.ControllerType = projectcontrol.ControllerType(input.ControllerType)
		switch filter.ControllerType {
		case projectcontrol.ControllerCodexMCP, projectcontrol.ControllerEmbeddedAgent, projectcontrol.ControllerManual:
		default:
			return projectcontrol.Result{}, controlValidationError("controllerType 无效")
		}
	}
	for _, value := range input.Statuses {
		status := projectcontrol.CommandStatus(strings.TrimSpace(value))
		if !status.Valid() {
			return projectcontrol.Result{}, controlValidationError("statuses 包含无效状态")
		}
		filter.Statuses = append(filter.Statuses, status)
	}
	if input.CreatedAfter != "" {
		createdAfter, err := time.Parse(time.RFC3339, input.CreatedAfter)
		if err != nil {
			return projectcontrol.Result{}, controlValidationError("createdAfter 必须是 RFC3339 时间")
		}
		filter.CreatedAfter = &createdAfter
	}
	cursor, err := projectcontrol.DecodeCommandCursor(input.Cursor)
	if err != nil {
		return projectcontrol.Result{}, controlValidationError("cursor 无效")
	}
	if cursor != nil {
		filter.BeforeCreatedAt = &cursor.CreatedAt
		filter.BeforeID = cursor.ID
	}
	if filter.ProjectID != "" {
		if _, _, err := e.authorizedProjectID(ctx, identity.Principal, filter.ProjectID, authz.PermissionProjectRead); err != nil {
			return projectcontrol.Result{}, err
		}
	}
	page, err := e.repository.List(ctx, filter)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	visible := make([]projectcontrol.Command, 0, len(page.Commands))
	for _, command := range page.Commands {
		if e.authorizeCommand(ctx, identity.Principal, command) == nil {
			visible = append(visible, command)
		}
	}
	visible, err = e.decorateCommands(ctx, visible)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlSuccess(fmt.Sprintf("找到 %d 条项目控制命令", len(visible)), map[string]any{"items": visible})
	if page.NextCursor != nil {
		result.NextCursor, err = projectcontrol.EncodeCommandCursor(*page.NextCursor)
		if err != nil {
			return projectcontrol.Result{}, err
		}
	}
	return result, nil
}

func (e *projectControlExecutor) commandGet(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	var input controlCommandInput
	if err := decodeControlInput(raw, &input); err != nil {
		return projectcontrol.Result{}, err
	}
	command, err := e.authorizedCommand(ctx, identity.Principal, input.CommandID)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	data, err := e.commandSnapshot(ctx, command)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlSuccess("已读取项目控制命令", data)
	result.CommandID = command.ID
	result.Status = string(command.Status)
	return result, nil
}

func (e *projectControlExecutor) commandEvents(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	var input controlCommandEventsInput
	if err := decodeControlInput(raw, &input); err != nil {
		return projectcontrol.Result{}, err
	}
	command, err := e.authorizedCommand(ctx, identity.Principal, input.CommandID)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	after, err := parseEventCursor(input.AfterCursor)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	limit := input.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	events, err := e.repository.Events(ctx, command.ID, after, limit)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlSuccess(fmt.Sprintf("读取到 %d 条命令事件", len(events)), map[string]any{
		"command": command,
		"events":  events,
	})
	result.CommandID = command.ID
	result.Status = string(command.Status)
	result.NextCursor = eventCursor(events, after)
	return result, nil
}

func (e *projectControlExecutor) commandWait(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	startedAt := time.Now()
	waitResult := "failed"
	defer func() {
		observability.RecordProjectControlWait(waitResult, time.Since(startedAt))
	}()
	var input controlCommandWaitInput
	if err := decodeControlInput(raw, &input); err != nil {
		return projectcontrol.Result{}, err
	}
	after, err := parseEventCursor(input.AfterCursor)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	timeout := time.Duration(input.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	if timeout > projectControlMaximumWait {
		timeout = projectControlMaximumWait
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		command, err := e.authorizedCommand(ctx, identity.Principal, input.CommandID)
		if err != nil {
			return projectcontrol.Result{}, err
		}
		events, err := e.repository.Events(ctx, command.ID, after, 200)
		if err != nil {
			return projectcontrol.Result{}, err
		}
		if len(events) > 0 || command.Terminal() {
			data, err := e.commandSnapshot(ctx, command)
			if err != nil {
				return projectcontrol.Result{}, err
			}
			data["events"] = events
			data["continueWaiting"] = !command.Terminal()
			result := projectControlSuccess("项目控制命令状态已更新", data)
			result.CommandID = command.ID
			result.Status = string(command.Status)
			result.NextCursor = eventCursor(events, after)
			waitResult = "updated"
			return result, nil
		}
		select {
		case <-ctx.Done():
			waitResult = "cancelled"
			return projectcontrol.Result{}, ctx.Err()
		case <-deadline.C:
			data, err := e.commandSnapshot(ctx, command)
			if err != nil {
				return projectcontrol.Result{}, err
			}
			data["events"] = []projectcontrol.CommandEvent{}
			data["continueWaiting"] = !command.Terminal()
			result := projectControlSuccess("等待时间已到，命令仍在执行", data)
			result.CommandID = command.ID
			result.Status = string(command.Status)
			result.NextCursor = strconv.FormatInt(after, 10)
			waitResult = "timeout"
			return result, nil
		case <-ticker.C:
		}
	}
}

func (e *projectControlExecutor) commandCancel(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	var input controlCommandCancelInput
	if err := decodeControlInput(raw, &input); err != nil {
		return projectcontrol.Result{}, err
	}
	command, err := e.authorizedCommand(ctx, identity.Principal, input.CommandID)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	if err := e.authorizeCommandPermission(ctx, identity.Principal, command, authz.PermissionWorkflowCancel); err != nil {
		return projectcontrol.Result{}, err
	}
	updated, replay, err := e.repository.RequestCancellation(ctx, projectcontrol.RequestCancellation{
		CommandID: command.ID, ExpectedRevision: input.ExpectedRevision,
		ActorUserID: identity.Principal.UserID, IdempotencyKey: input.IdempotencyKey,
		Reason: input.Reason,
	})
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlSuccess("已提交命令取消请求", map[string]any{
		"command": updated, "idempotentReplay": replay,
	})
	result.CommandID = updated.ID
	result.Status = string(updated.Status)
	return result, nil
}

func (e *projectControlExecutor) commandRetry(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	var input controlCommandRetryInput
	if err := decodeControlInput(raw, &input); err != nil {
		return projectcontrol.Result{}, err
	}
	original, err := e.authorizedCommand(ctx, identity.Principal, input.CommandID)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	descriptor, ok := e.registry.Get(original.ActionName)
	if !ok || descriptor.Version != original.ActionVersion {
		return projectcontrol.Result{}, newAPIError(http.StatusConflict, "ACTION_CONTRACT_UNAVAILABLE", "原命令动作契约在当前版本不可用")
	}
	for _, permission := range descriptorPermissions(descriptor) {
		if err := e.authorizeCommandPermission(ctx, identity.Principal, original, permission); err != nil {
			return projectcontrol.Result{}, err
		}
	}
	controllerType := identity.ControllerType
	if controllerType == "" {
		controllerType = projectcontrol.ControllerManual
	}
	controlKeyID := ""
	if controllerType == projectcontrol.ControllerCodexMCP {
		controlKeyID = identity.Key.ID
	}
	retry, replay, err := e.repository.Retry(ctx, projectcontrol.RetryCommand{
		CommandID: original.ID, ExpectedRevision: input.ExpectedRevision,
		ActorUserID: identity.Principal.UserID, ControllerType: controllerType,
		ControlKeyID: controlKeyID, Descriptor: descriptor, IdempotencyKey: input.IdempotencyKey,
	})
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlSuccess("已创建失败项重试命令", map[string]any{
		"command": retry, "retryOfCommandId": original.ID, "idempotentReplay": replay,
	})
	result.CommandID = retry.ID
	result.Status = string(retry.Status)
	return result, nil
}

func (e *projectControlExecutor) commandResolve(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	var input controlCommandResolveInput
	if err := decodeControlInput(raw, &input); err != nil {
		return projectcontrol.Result{}, err
	}
	command, err := e.authorizedCommand(ctx, identity.Principal, input.CommandID)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	prompt, updated, replay, err := e.repository.ResolvePrompt(ctx, projectcontrol.ResolveCommandPrompt{
		CommandID: command.ID, PromptID: input.PromptID, ActorUserID: identity.Principal.UserID,
		ExpectedCommandRevision: input.ExpectedRevision, IdempotencyKey: input.IdempotencyKey,
		Answer: input.Answer, ResumeStatus: projectcontrol.CommandQueued,
	})
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlSuccess("已提交用户选择并恢复命令", map[string]any{
		"command": updated, "prompt": prompt, "idempotentReplay": replay,
	})
	result.CommandID = updated.ID
	result.Status = string(updated.Status)
	return result, nil
}

func (e *projectControlExecutor) authorizedProject(ctx context.Context, actor auth.Principal, raw json.RawMessage, permissions ...string) (Project, auth.Principal, error) {
	var input controlProjectInput
	if err := decodeControlInput(raw, &input); err != nil {
		return Project{}, auth.Principal{}, err
	}
	return e.authorizedProjectID(ctx, actor, input.ProjectID, permissions...)
}

func (e *projectControlExecutor) decodeNativeAgentReadInput(name string, raw json.RawMessage) (string, json.RawMessage, map[string]any, error) {
	projectID, _, actionInput, err := decodeProjectControlAgentInput(raw, false)
	if err != nil {
		return "", nil, nil, err
	}
	tool, exists := e.agentTools[name]
	if !exists {
		return "", nil, nil, fmt.Errorf("native project control read %s has no shared tool contract", name)
	}
	if err := agent.ValidateToolInput(tool, actionInput); err != nil {
		return "", nil, nil, controlValidationError(err.Error())
	}
	var arguments map[string]any
	if err := json.Unmarshal(actionInput, &arguments); err != nil {
		return "", nil, nil, controlValidationError("项目控制读取输入格式无效")
	}
	return projectID, actionInput, arguments, nil
}

func (e *projectControlExecutor) authorizedProjectID(ctx context.Context, actor auth.Principal, projectID string, permissions ...string) (Project, auth.Principal, error) {
	if strings.TrimSpace(projectID) == "" {
		return Project{}, auth.Principal{}, controlValidationError("projectId 不能为空")
	}
	project, err := projectByIDForControl(ctx, e.server, projectID)
	if err != nil {
		return Project{}, auth.Principal{}, err
	}
	principal := actor
	principal.OrganizationID = project.OrganizationID
	for _, permission := range permissions {
		if err := e.server.authorizer.Authorize(ctx, principal, permission, authz.Resource{ProjectID: project.ID}); err != nil {
			return Project{}, auth.Principal{}, err
		}
	}
	return project, principal, nil
}

func (e *projectControlExecutor) authorizedNativeProjectAction(
	ctx context.Context,
	actor auth.Principal,
	actionName, projectID string,
	permissions ...string,
) (Project, auth.Principal, error) {
	project, principal, err := e.authorizedProjectID(ctx, actor, projectID, permissions...)
	if err != nil {
		return Project{}, auth.Principal{}, err
	}
	descriptor, exists := e.registry.Get(actionName)
	if !exists {
		return Project{}, auth.Principal{}, newAPIError(http.StatusNotFound, "ACTION_NOT_FOUND", "项目控制动作不存在")
	}
	if len(descriptor.ProjectKinds) > 0 {
		allowed := false
		for _, projectKind := range descriptor.ProjectKinds {
			if projectKind == string(project.ProjectKind) {
				allowed = true
				break
			}
		}
		if !allowed {
			return Project{}, auth.Principal{}, newAPIError(http.StatusUnprocessableEntity, "PROJECT_KIND_MISMATCH", "当前项目类型不支持此操作")
		}
	}
	return project, principal, nil
}

func (e *projectControlExecutor) authorizedCommand(ctx context.Context, actor auth.Principal, commandID string) (projectcontrol.Command, error) {
	if strings.TrimSpace(commandID) == "" {
		return projectcontrol.Command{}, controlValidationError("commandId 不能为空")
	}
	command, err := e.repository.Get(ctx, commandID)
	if err != nil {
		return projectcontrol.Command{}, err
	}
	if err := e.authorizeCommand(ctx, actor, command); err != nil {
		return projectcontrol.Command{}, err
	}
	return command, nil
}

func (e *projectControlExecutor) authorizeCommand(ctx context.Context, actor auth.Principal, command projectcontrol.Command) error {
	principal := actor
	principal.OrganizationID = command.OrganizationID
	resource := authz.Resource{OrganizationID: command.OrganizationID}
	permission := authz.PermissionOrganizationRead
	if command.ProjectID != "" {
		resource = authz.Resource{ProjectID: command.ProjectID}
		permission = authz.PermissionProjectRead
	} else if command.WorkspaceID != "" {
		resource = authz.Resource{WorkspaceID: command.WorkspaceID}
		permission = authz.PermissionWorkspaceRead
	}
	return e.server.authorizer.Authorize(ctx, principal, permission, resource)
}

func (e *projectControlExecutor) authorizeCommandPermission(ctx context.Context, actor auth.Principal, command projectcontrol.Command, permission string) error {
	principal := actor
	principal.OrganizationID = command.OrganizationID
	resource := authz.Resource{OrganizationID: command.OrganizationID}
	if command.ProjectID != "" {
		resource = authz.Resource{ProjectID: command.ProjectID}
	} else if command.WorkspaceID != "" {
		resource = authz.Resource{WorkspaceID: command.WorkspaceID}
	}
	return e.server.authorizer.Authorize(ctx, principal, permission, resource)
}

func (e *projectControlExecutor) commandSnapshot(ctx context.Context, command projectcontrol.Command) (map[string]any, error) {
	command = e.decorateCommand(command)
	items, err := e.repository.Items(ctx, command.ID)
	if err != nil {
		return nil, err
	}
	workflows, err := e.repository.WorkflowLinks(ctx, command.ID)
	if err != nil {
		return nil, err
	}
	command.WorkflowRunIDs = workflowRunIDsFromLinks(workflows)
	children, err := e.repository.Children(ctx, command.ID)
	if err != nil {
		return nil, err
	}
	for index := range children {
		children[index] = e.decorateCommand(children[index])
	}
	workflowRuns, err := e.workflowRunsForControlLinks(ctx, workflows)
	if err != nil {
		return nil, err
	}
	data := map[string]any{
		"command":       command,
		"items":         items,
		"childCommands": children,
		"workflows":     workflows,
		"workflowRuns":  workflowRuns,
	}
	prompt, err := e.repository.PendingPrompt(ctx, command.ID)
	if err == nil {
		data["pendingPrompt"] = prompt
	} else if !errors.Is(err, projectcontrol.ErrPromptNotFound) {
		return nil, err
	}
	return data, nil
}

func (e *projectControlExecutor) decorateCommand(command projectcontrol.Command) projectcontrol.Command {
	if descriptor, ok := e.registry.Get(command.ActionName); ok {
		command.ActionLabel = descriptor.Label
	}
	return command
}

func (e *projectControlExecutor) decorateCommands(ctx context.Context, commands []projectcontrol.Command) ([]projectcontrol.Command, error) {
	ids := make([]string, 0, len(commands))
	for _, command := range commands {
		ids = append(ids, command.ID)
	}
	workflowIDs, err := e.repository.WorkflowRunIDsByCommand(ctx, ids)
	if err != nil {
		return nil, err
	}
	for index := range commands {
		commands[index] = e.decorateCommand(commands[index])
		commands[index].WorkflowRunIDs = workflowIDs[commands[index].ID]
	}
	return commands, nil
}

func workflowRunIDsFromLinks(links []projectcontrol.WorkflowLink) []string {
	ids := make([]string, 0, len(links))
	seen := make(map[string]struct{}, len(links))
	for _, link := range links {
		id := strings.TrimSpace(link.WorkflowRunID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func (e *projectControlExecutor) workflowRunsForControlLinks(ctx context.Context, links []projectcontrol.WorkflowLink) ([]WorkflowRun, error) {
	ids := workflowRunIDsFromLinks(links)
	if len(ids) == 0 {
		return []WorkflowRun{}, nil
	}
	rows, err := e.server.db.Query(ctx, workflowRunSelectSQL(`
		WHERE id = ANY($1::uuid[])
		ORDER BY created_at, id
	`), ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]WorkflowRun, 0, len(ids))
	for rows.Next() {
		item, err := scanWorkflowRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (e *projectControlExecutor) recentWorkflowRuns(ctx context.Context, projectID string, limit int) ([]WorkflowRun, error) {
	if limit <= 0 || limit > projectControlMaximumPageSize {
		limit = projectControlDefaultPageSize
	}
	rows, err := e.server.db.Query(ctx, workflowRunSelectSQL(`
		WHERE project_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`), projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]WorkflowRun, 0, limit)
	for rows.Next() {
		item, err := scanWorkflowRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func decodeControlInput(raw json.RawMessage, target any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return controlValidationError("请求参数无效：" + err.Error())
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return controlValidationError("请求只能包含一个 JSON 对象")
	}
	return nil
}

func controlValidationError(message string) apiError {
	return newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", message)
}

func boundedControlLimit(value int) int {
	if value <= 0 {
		return projectControlDefaultPageSize
	}
	if value > projectControlMaximumPageSize {
		return projectControlMaximumPageSize
	}
	return value
}

func projectControlSuccess(summary string, data any) projectcontrol.Result {
	result := projectcontrol.NewResult("succeeded", summary)
	if data != nil {
		result.Data = mustProjectControlJSON(data)
	}
	return result
}

func projectControlFailure(code, message string, retryable bool, details any) projectcontrol.Result {
	result := projectcontrol.NewResult("failed", message)
	result.Retryable = retryable
	result.Error = &projectcontrol.Error{Code: code, UserMessage: message, Retryable: retryable}
	if details != nil {
		result.Error.Details = mustProjectControlJSON(details)
	}
	return result
}

func projectControlBusinessError(err error) (projectcontrol.Result, bool) {
	var appErr apiError
	var accessErr authz.AccessError
	switch {
	case errors.As(err, &appErr):
		return projectControlFailure(appErr.Code, appErr.Message, appErr.Retryable, appErr.Details), true
	case errors.As(err, &accessErr), errors.Is(err, authz.ErrAccessDenied), errors.Is(err, auth.ErrForbidden):
		return projectControlFailure("PERMISSION_DENIED", "当前用户没有执行该操作的权限", false, nil), true
	case errors.Is(err, projectcontrol.ErrCommandNotFound), errors.Is(err, pgx.ErrNoRows):
		return projectControlFailure("NOT_FOUND", "请求的项目控制对象不存在", false, nil), true
	case errors.Is(err, projectcontrol.ErrRevisionConflict):
		return projectControlFailure("REVISION_CONFLICT", "对象已被其他操作修改，请重新读取后重试", false, nil), true
	case errors.Is(err, projectcontrol.ErrIdempotencyConflict):
		return projectControlFailure("IDEMPOTENCY_CONFLICT", "幂等键已用于不同请求", false, nil), true
	case errors.Is(err, projectcontrol.ErrRetryAlreadyActive):
		return projectControlFailure("RETRY_ALREADY_ACTIVE", "该命令已有活动重试，请等待其完成", false, nil), true
	case errors.Is(err, projectcontrol.ErrRetryUnavailable):
		return projectControlFailure("RETRY_UNAVAILABLE", "命令没有可重试的失败执行单元", false, nil), true
	case errors.Is(err, projectcontrol.ErrPromptAlreadyResolved):
		return projectControlFailure("PROMPT_ALREADY_RESOLVED", "该问题已经回答，不能再次提交", false, nil), true
	case errors.Is(err, projectcontrol.ErrPromptExpired):
		return projectControlFailure("PROMPT_EXPIRED", "该问题已过期，请重新执行相关动作", false, nil), true
	default:
		return projectcontrol.Result{}, false
	}
}

func mustProjectControlJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

func parseEventCursor(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	cursor, err := strconv.ParseInt(value, 10, 64)
	if err != nil || cursor < 0 {
		return 0, controlValidationError("afterCursor 无效")
	}
	return cursor, nil
}

func eventCursor(events []projectcontrol.CommandEvent, fallback int64) string {
	if len(events) == 0 {
		return strconv.FormatInt(fallback, 10)
	}
	return strconv.FormatInt(events[len(events)-1].Sequence, 10)
}

func descriptorPermissions(descriptor projectcontrol.Descriptor) []string {
	seen := make(map[string]struct{})
	items := make([]string, 0, len(descriptor.Permissions)+1)
	for _, value := range append([]string{descriptor.Permission}, descriptor.Permissions...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		items = append(items, value)
	}
	return items
}

func descriptorAllowsProjectKind(descriptor projectcontrol.Descriptor, projectKind string) bool {
	if len(descriptor.ProjectKinds) == 0 {
		return true
	}
	for _, value := range descriptor.ProjectKinds {
		if value == projectKind {
			return true
		}
	}
	return false
}

func projectControlReadDescriptors() []projectcontrol.Descriptor {
	return []projectcontrol.Descriptor{
		newReadDescriptor("identity.me", "当前用户", "读取当前用户、控制密钥元数据和可访问组织", projectcontrol.ScopeSystem, "", emptyControlInputSchema),
		newReadDescriptor("organization.list", "组织列表", "分页列出当前用户可访问的组织", projectcontrol.ScopeSystem, "", controlPageSchema()),
		newReadDescriptor("workspace.list", "工作区列表", "分页列出组织内可访问的工作区", projectcontrol.ScopeOrganization, authz.PermissionWorkspaceRead, organizationPageSchema()),
		newReadDescriptor("project.list", "项目列表", "分页列出组织或工作区内可访问的项目", projectcontrol.ScopeOrganization, authz.PermissionProjectRead, projectListSchema()),
		newReadDescriptor("project.get", "项目详情", "读取项目基本信息和当前生产绑定", projectcontrol.ScopeProject, authz.PermissionProjectRead, projectIDSchema()),
		newReadDescriptor("project.context", "项目上下文", "读取项目、生产状态和最近工作流上下文", projectcontrol.ScopeProject, authz.PermissionProjectRead, projectIDSchema()),
		newReadDescriptor("project.capabilities", "项目能力", "按当前用户权限和项目类型列出可执行动作", projectcontrol.ScopeProject, authz.PermissionProjectRead, projectIDSchema()),
		newReadDescriptor("project.production_status", "生产状态", "读取项目当前生产阶段和完成度", projectcontrol.ScopeProject, authz.PermissionProjectRead, projectIDSchema()),
		newReadDescriptorWithPermissions("project.task_activity", "任务活动", "分页读取项目控制命令和最近工作流", projectcontrol.ScopeProject, []string{authz.PermissionProjectRead, authz.PermissionWorkflowRead}, projectTaskActivitySchema()),
		newReadDescriptor("content.describe", "内容信息", "读取长文本内容的长度、版本、格式和 SHA-256", projectcontrol.ScopeProject, authz.PermissionProjectRead, contentDescribeSchema()),
		newReadDescriptor("content.read", "分块读取内容", "按 UTF-8 边界分页读取原文、章节、剧本分集或带货脚本", projectcontrol.ScopeProject, authz.PermissionProjectRead, contentReadSchema()),
		newContentWriteDescriptor("content.write.begin", "开始暂存内容", "校验目标 revision 并创建私有长文本暂存上传", projectcontrol.RiskWrite, contentWriteBeginSchema()),
		newContentWriteDescriptor("content.write.chunk", "写入内容分块", "按 chunk index 和 SHA-256 幂等写入私有暂存区", projectcontrol.RiskWrite, contentWriteChunkSchema()),
		newContentWriteDescriptor("content.write.commit", "提交暂存内容", "校验完整内容 hash 和 revision 后原子更新 canonical 内容", projectcontrol.RiskWrite, contentWriteCommitSchema()),
		newContentWriteDescriptor("content.write.abort", "放弃暂存内容", "幂等终止未提交的内容暂存上传", projectcontrol.RiskDestructive, contentWriteAbortSchema()),
		newReadDescriptor("control.command.list", "命令列表", "分页查找活动命令和最近命令", projectcontrol.ScopeSystem, "", commandListSchema()),
		newReadDescriptor("control.command.get", "命令详情", "读取命令、item、工作流和待确认问题", projectcontrol.ScopeSystem, "", commandIDSchema()),
		newReadDescriptor("control.command.events", "命令事件", "按事件游标读取命令增量动态", projectcontrol.ScopeSystem, "", commandEventsSchema()),
		newReadDescriptor("control.command.wait", "等待命令", "短等待命令状态或新增事件，最长 45 秒", projectcontrol.ScopeSystem, "", commandWaitSchema()),
		newControlWriteDescriptor("control.command.cancel", "取消命令", "请求取消命令及其仍在活动的子工作流", projectcontrol.RiskDestructive, commandCancelSchema()),
		newControlWriteDescriptor("control.command.retry", "重试失败项", "按冻结失败项创建独立重试命令", projectcontrol.RiskWorkflow, commandRetrySchema()),
		newControlWriteDescriptor("control.command.resolve", "回答命令问题", "回答待确认问题并恢复命令执行", projectcontrol.RiskWrite, commandResolveSchema()),
	}
}

func newReadDescriptor(name, label, summary string, scope projectcontrol.ScopeKind, permission string, input json.RawMessage) projectcontrol.Descriptor {
	permissions := []string{}
	if permission != "" {
		permissions = append(permissions, permission)
	}
	return newReadDescriptorWithPermissions(name, label, summary, scope, permissions, input)
}

func newReadDescriptorWithPermissions(name, label, summary string, scope projectcontrol.ScopeKind, permissions []string, input json.RawMessage) projectcontrol.Descriptor {
	permission := ""
	if len(permissions) > 0 {
		permission = permissions[0]
	}
	return projectcontrol.Descriptor{
		Name: name, Version: 1, Label: label, Summary: summary, Description: summary,
		Risk: projectcontrol.RiskRead, Scope: scope, Permission: permission,
		Permissions: append([]string(nil), permissions...), ProjectKinds: []string{},
		InputSchema: input, OutputSchema: controlOutputSchema,
		Effects: projectcontrol.Effects{}, ReadOnly: true, Destructive: false,
		Idempotent: true, Costed: false, StartsWorkflow: false, SupportsDryRun: false,
		ExecutionMode:      projectcontrol.ExecutionModeSync,
		ActivityVisibility: projectcontrol.ActivityVisibilityAuditOnly,
		ExportToMCP:        true,
	}
}

func newControlWriteDescriptor(name, label, summary string, risk projectcontrol.Risk, input json.RawMessage) projectcontrol.Descriptor {
	destructive := risk == projectcontrol.RiskDestructive
	return projectcontrol.Descriptor{
		Name: name, Version: 1, Label: label, Summary: summary, Description: summary,
		Risk: risk, Scope: projectcontrol.ScopeSystem, Permissions: []string{}, ProjectKinds: []string{},
		InputSchema: input, OutputSchema: controlOutputSchema,
		Effects: projectcontrol.Effects{WritesProject: true, Destructive: destructive}, ReadOnly: false,
		Destructive: destructive, Idempotent: true,
		Costed: false, StartsWorkflow: false, SupportsDryRun: false,
		ExecutionMode:      projectcontrol.ExecutionModeSync,
		ActivityVisibility: projectcontrol.ActivityVisibilityAuditOnly,
		ExportToMCP:        true,
	}
}

func newContentWriteDescriptor(name, label, summary string, risk projectcontrol.Risk, input json.RawMessage) projectcontrol.Descriptor {
	destructive := risk == projectcontrol.RiskDestructive
	return projectcontrol.Descriptor{
		Name: name, Version: 1, Label: label, Summary: summary, Description: summary,
		Risk: risk, Scope: projectcontrol.ScopeProject, Permission: authz.PermissionProjectWrite,
		Permissions: []string{authz.PermissionProjectWrite}, ProjectKinds: []string{},
		InputSchema: input, OutputSchema: controlOutputSchema,
		Effects:  projectcontrol.Effects{WritesProject: true, Destructive: destructive},
		ReadOnly: false, Destructive: destructive, Idempotent: true,
		ExecutionMode: projectcontrol.ExecutionModeSync, ActivityVisibility: projectcontrol.ActivityVisibilityAuditOnly,
		ExportToMCP: true,
	}
}

func controlPageSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"limit":{"type":"integer","minimum":1,"maximum":50},"cursor":{"type":"string"}}}`)
}

func organizationPageSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["organizationId"],"properties":{"organizationId":{"type":"string","format":"uuid"},"limit":{"type":"integer","minimum":1,"maximum":50},"cursor":{"type":"string"}}}`)
}

func projectListSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["organizationId"],"properties":{"organizationId":{"type":"string","format":"uuid"},"workspaceId":{"type":"string","format":"uuid"},"limit":{"type":"integer","minimum":1,"maximum":50},"cursor":{"type":"string"}}}`)
}

func projectIDSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["projectId"],"properties":{"projectId":{"type":"string","format":"uuid"}}}`)
}

func projectTaskActivitySchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["projectId"],"properties":{"projectId":{"type":"string","format":"uuid"},"limit":{"type":"integer","minimum":1,"maximum":50},"cursor":{"type":"string"}}}`)
}

func contentDescribeSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["projectId","targetType","targetId"],"properties":{"projectId":{"type":"string","format":"uuid"},"targetType":{"type":"string","enum":["project_source","novel_chapter","script_version","script_episode","commerce_script_unit"]},"targetId":{"type":"string","format":"uuid"}}}`)
}

func contentReadSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["projectId","targetType","targetId"],"properties":{"projectId":{"type":"string","format":"uuid"},"targetType":{"type":"string","enum":["project_source","novel_chapter","script_version","script_episode","commerce_script_unit"]},"targetId":{"type":"string","format":"uuid"},"contentHash":{"type":"string","pattern":"^[0-9a-f]{64}$"},"cursor":{"type":"string"},"maxBytes":{"type":"integer","minimum":1,"maximum":262144}}}`)
}

func contentWriteBeginSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["projectId","targetType","targetId","expectedRevision","contentHash","contentFormat","expectedSizeBytes","expectedChunkCount","idempotencyKey"],"properties":{"projectId":{"type":"string","format":"uuid"},"targetType":{"type":"string","enum":["project_source","novel_chapter","script_episode","commerce_script_unit"]},"targetId":{"type":"string","format":"uuid"},"expectedRevision":{"type":"integer","minimum":1},"contentHash":{"type":"string","pattern":"^[0-9a-f]{64}$"},"contentFormat":{"type":"string","minLength":1,"maxLength":100},"expectedSizeBytes":{"type":"integer","minimum":1,"maximum":67108864},"expectedChunkCount":{"type":"integer","minimum":1,"maximum":10000},"idempotencyKey":{"type":"string","minLength":1,"maxLength":200}}}`)
}

func contentWriteChunkSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["projectId","uploadId","chunkIndex","chunkHash","chunkText"],"properties":{"projectId":{"type":"string","format":"uuid"},"uploadId":{"type":"string","format":"uuid"},"chunkIndex":{"type":"integer","minimum":0,"maximum":9999},"chunkHash":{"type":"string","pattern":"^[0-9a-f]{64}$"},"chunkText":{"type":"string","minLength":1}}}`)
}

func contentWriteCommitSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["projectId","uploadId","idempotencyKey"],"properties":{"projectId":{"type":"string","format":"uuid"},"uploadId":{"type":"string","format":"uuid"},"idempotencyKey":{"type":"string","minLength":1,"maxLength":200}}}`)
}

func contentWriteAbortSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["projectId","uploadId","idempotencyKey"],"properties":{"projectId":{"type":"string","format":"uuid"},"uploadId":{"type":"string","format":"uuid"},"idempotencyKey":{"type":"string","minLength":1,"maxLength":200}}}`)
}

func commandListSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"projectId":{"type":"string","format":"uuid"},"statuses":{"type":"array","items":{"type":"string","enum":["queued","running","waiting_workflow","waiting_input","succeeded","partial_succeeded","failed","cancelled"]}},"controllerType":{"type":"string","enum":["embedded_agent","codex_mcp","manual"]},"createdAfter":{"type":"string","format":"date-time"},"view":{"type":"string","enum":["activity"]},"limit":{"type":"integer","minimum":1,"maximum":50},"cursor":{"type":"string"}}}`)
}

func commandIDSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["commandId"],"properties":{"commandId":{"type":"string","format":"uuid"}}}`)
}

func commandEventsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["commandId"],"properties":{"commandId":{"type":"string","format":"uuid"},"afterCursor":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":200}}}`)
}

func commandWaitSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["commandId"],"properties":{"commandId":{"type":"string","format":"uuid"},"afterCursor":{"type":"string"},"timeoutSeconds":{"type":"integer","minimum":1,"maximum":45}}}`)
}

func commandCancelSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["commandId","expectedCommandRevision","idempotencyKey"],"properties":{"commandId":{"type":"string","format":"uuid"},"expectedCommandRevision":{"type":"integer","minimum":1},"idempotencyKey":{"type":"string","minLength":1,"maxLength":200},"reason":{"type":"string","maxLength":1000}}}`)
}

func commandRetrySchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["commandId","expectedCommandRevision","idempotencyKey"],"properties":{"commandId":{"type":"string","format":"uuid"},"expectedCommandRevision":{"type":"integer","minimum":1},"idempotencyKey":{"type":"string","minLength":1,"maxLength":200}}}`)
}

func commandResolveSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["commandId","promptId","expectedCommandRevision","idempotencyKey","answer"],"properties":{"commandId":{"type":"string","format":"uuid"},"promptId":{"type":"string","format":"uuid"},"expectedCommandRevision":{"type":"integer","minimum":1},"idempotencyKey":{"type":"string","minLength":1,"maxLength":200},"answer":{"type":"object"}}}`)
}
