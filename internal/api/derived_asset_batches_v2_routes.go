package api

import (
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
