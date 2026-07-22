package api

import (
	"fmt"
	"net/http"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/workflows"
)

func (s *Server) getDerivedAssetBatchActivity(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	run, err := scanWorkflowRun(s.db.QueryRow(
		r.Context(),
		workflowRunSelectSQL(`WHERE id = $1 AND organization_id = $2`),
		r.PathValue("workflowRunId"),
		principal.OrganizationID,
	))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionProjectRead, authz.Resource{ProjectID: run.ProjectID}) {
		return
	}
	projection, err := s.derivedAssetBatchProjectionByWorkflowRun(
		r.Context(), principal.OrganizationID, run.ProjectID, run.ID,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, projection, nil)
}

func (s *Server) retryFailedDerivedAssetBatch(
	r *http.Request,
	w http.ResponseWriter,
	principal auth.Principal,
	original WorkflowRun,
	req retryFailedWorkflowRequest,
) (WorkflowRun, bool, error) {
	if req.ExpectedProjectRevision <= 0 {
		return WorkflowRun{}, false, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "expectedProjectRevision is required")
	}
	project, err := s.project(r, original.ProjectID)
	if err != nil {
		return WorkflowRun{}, false, err
	}
	source, err := s.derivedAssetBatchProjectionByWorkflowRun(
		r.Context(), principal.OrganizationID, project.ID, original.ID,
	)
	if err != nil {
		return WorkflowRun{}, false, err
	}
	result, err := s.createDerivedAssetBatchRun(r.Context(), principal, project, DerivedAssetBatchCreateOptions{
		Mode:                    derivedAssetBatchModeRetry,
		RetryOfBatchID:          source.ID,
		MaxConcurrency:          req.MaxConcurrency,
		ExpectedProjectRevision: req.ExpectedProjectRevision,
		IdempotencyKey:          idempotencyKey(r, req.IdempotencyKey),
	})
	if err != nil {
		return WorkflowRun{}, false, err
	}
	return result.WorkflowRun, true, nil
}

func (s *Server) agentToolStartDerivedAssetBatch(
	r *http.Request,
	principal auth.Principal,
	project Project,
	task AgentTask,
	step AgentStep,
	args map[string]any,
	input map[string]any,
) agentToolResult {
	if existing, ok, err := s.agentWorkflowRunForStep(r.Context(), project.ID, task.ID, step.ID); err != nil {
		return agentToolError("workflow.start", args, err)
	} else if ok {
		return agentToolOK("workflow.start", args, fmt.Sprintf("已存在 %s 工作流 %s，未重复启动。", derivedAssetBatchWorkflowType, existing.ID), map[string]any{
			"workflowRunId": existing.ID,
			"workflowType":  derivedAssetBatchWorkflowType,
			"status":        existing.Status,
			"input":         rawObject(existing.Input),
			"agentTaskId":   task.ID,
			"agentStepId":   step.ID,
			"idempotent":    true,
		})
	}

	requirementIDs := agentReferenceStringSliceArg(input, "requirementIds")
	mode := derivedAssetBatchModeSelectAll
	if len(requirementIDs) > 0 {
		mode = derivedAssetBatchModeExplicit
	}
	result, err := s.createDerivedAssetBatchRun(r.Context(), principal, project, DerivedAssetBatchCreateOptions{
		Mode:           mode,
		RequirementIDs: requirementIDs,
		Filters: DerivedAssetBatchFilters{
			ScriptEpisodeID: agentReferenceStringArg(input, "scriptEpisodeId"),
			ShotIDs:         agentReferenceStringSliceArg(input, "shotIds"),
		},
		MaxConcurrency:          agentIntArg(input, "maxConcurrency", workflows.DefaultDerivedAssetImageConcurrency, 1, workflows.MaxDerivedAssetImageConcurrency),
		Force:                   boolValue(input["force"]),
		ExpectedProjectRevision: project.Revision,
		IdempotencyKey:          agentStepIdempotencyKey(task, step),
		AgentTaskID:             task.ID,
		AgentStepID:             step.ID,
	})
	if err != nil {
		return agentToolError("workflow.start", args, err)
	}
	return agentToolOK("workflow.start", args, fmt.Sprintf("已启动 %s，工作流 %s 当前状态 %s。", derivedAssetBatchWorkflowType, result.WorkflowRun.ID, result.WorkflowRun.Status), map[string]any{
		"workflowRunId": result.WorkflowRun.ID,
		"workflowType":  derivedAssetBatchWorkflowType,
		"status":        result.WorkflowRun.Status,
		"input":         input,
		"agentTaskId":   task.ID,
		"agentStepId":   step.ID,
		"operationId":   result.OperationID,
		"derivedAssets": result.Batch,
	})
}

func (s *Server) createDerivedAssetBatchForAgentAction(
	r *http.Request,
	principal auth.Principal,
	project Project,
	message AgentMessage,
	req ProductionActionRequest,
) (DerivedAssetBatchCommandResult, error) {
	requirementIDs := productionOptionStringSlice(req.Options, "requirementIds")
	mode := derivedAssetBatchModeSelectAll
	if len(requirementIDs) > 0 {
		mode = derivedAssetBatchModeExplicit
	}
	return s.createDerivedAssetBatchRun(r.Context(), principal, project, DerivedAssetBatchCreateOptions{
		Mode:           mode,
		RequirementIDs: requirementIDs,
		Filters: DerivedAssetBatchFilters{
			ScriptEpisodeID: productionOptionString(req.Options, "scriptEpisodeId"),
			ShotIDs:         productionOptionStringSlice(req.Options, "shotIds"),
		},
		MaxConcurrency:          productionOptionInt(req.Options, "maxConcurrency", workflows.DefaultDerivedAssetImageConcurrency),
		Force:                   productionOptionBool(req.Options, "force", false),
		ExpectedProjectRevision: project.Revision,
		IdempotencyKey:          "agent-production-action:" + message.ID + ":" + derivedAssetBatchWorkflowType,
	})
}
