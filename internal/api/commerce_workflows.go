package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Server) organizeCommerceScriptUnit(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowRun)
	if !ok {
		return
	}
	if s.temporal == nil {
		s.writeError(w, r, apiError{Status: http.StatusServiceUnavailable, Code: "TEMPORAL_UNAVAILABLE", Message: "Temporal 服务不可用", Retryable: true})
		return
	}
	var req struct {
		ExpectedUnitGenerationID string `json:"expectedUnitGenerationId"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.ExpectedUnitGenerationID) == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "请选择当前脚本生产代", nil, false)
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED", "整理销售脚本需要请求标识", nil, false)
		return
	}
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId": project.ID, "scriptUnitId": r.PathValue("scriptUnitId"),
		"expectedUnitGenerationId": strings.TrimSpace(req.ExpectedUnitGenerationID),
	})
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	claim, err := claimIdempotencyTx(
		r.Context(), tx, project.OrganizationID,
		"commerce_script_organization:create:"+r.PathValue("scriptUnitId"),
		idempotencyKey, requestHash,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if len(claim.replaySnapshot) > 0 {
		var response WorkflowRun
		if err := json.Unmarshal(claim.replaySnapshot, &response); err != nil {
			s.writeError(w, r, err)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			s.writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, r, http.StatusAccepted, response, map[string]any{"idempotentReplay": true})
		return
	}
	identity, err := s.commerceCatalog.LockActiveStoryboardGeneration(
		r.Context(), tx, project.OrganizationID, project.ID, r.PathValue("scriptUnitId"),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if expected := strings.TrimSpace(req.ExpectedUnitGenerationID); expected != identity.UnitGenerationID {
		s.writeError(w, r, commerce.Error{Code: commerce.CodeGenerationMismatch, Message: "脚本单元生产代已变化，请刷新后重试"})
		return
	}
	runID, err := workflows.EnqueueCommerceScriptOrganizationTx(r.Context(), tx, identity, principal.UserID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	run, err := scanWorkflowRun(tx.QueryRow(r.Context(), workflowRunSelectSQL(`WHERE id = $1`), runID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertWorkflowQueuedEventTx(r.Context(), tx, run, run.WorkflowType); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := completeIdempotencyTxWithStatus(r.Context(), tx, claim.state, http.StatusAccepted, run); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, run, nil)
}

func (s *Server) getCommerceScriptUnitRebuildImpact(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	var req commerce.ScriptUnitRebuildTarget
	if !decode(w, r, &req) {
		return
	}
	raw, err := json.Marshal(map[string]any{
		"scriptUnitId":                strings.TrimSpace(r.PathValue("scriptUnitId")),
		"expectedRevision":            req.ExpectedRevision,
		"targetSourceScriptVersionId": req.TargetSourceScriptVersionID,
		"targetLanguageMode":          req.TargetLanguageMode,
		"targetLanguage":              req.TargetLanguage,
		"targetDurationSeconds":       req.TargetDurationSeconds,
		"targetPlatform":              req.TargetPlatform,
		"targetStoryboardStrategy":    req.TargetStoryboardStrategy,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "commerce.script.rebuild_impact", raw,
		strings.TrimSpace(r.Header.Get("Idempotency-Key")),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	impact, err := decodeAgentToolData[commerce.ScriptUnitRebuildImpact](result.Data)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, impact, nil)
}

func (s *Server) createCommerceScriptUnitRebuild(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowRun)
	if !ok {
		return
	}
	var req struct {
		ImpactToken      string `json:"impactToken"`
		ExpectedRevision int64  `json:"expectedRevision"`
	}
	if !decode(w, r, &req) {
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	result, err := s.executeManualAsyncAction(
		r.Context(), principal, project, "commerce.script.rebuild",
		map[string]any{
			"scriptUnitId":     strings.TrimSpace(r.PathValue("scriptUnitId")),
			"impactToken":      strings.TrimSpace(req.ImpactToken),
			"expectedRevision": req.ExpectedRevision,
		},
		idempotencyKey,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeManualAsyncActionResult(w, r, result)
}

func (s *Server) prepareCommerceScriptUnit(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowRun)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	if !decode(w, r, &req) {
		return
	}
	if s.temporal == nil {
		s.writeError(w, r, apiError{Status: http.StatusServiceUnavailable, Code: "TEMPORAL_UNAVAILABLE", Message: "Temporal 服务不可用", Retryable: true})
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	identity, err := s.commerceCatalog.FreezeScriptUnitPreparation(
		r.Context(), tx, project.OrganizationID, project.ID, r.PathValue("scriptUnitId"),
		req.ExpectedRevision, principal.UserID,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if existing, found, err := loadActiveCommercePreparationRun(r.Context(), tx, identity); err != nil {
		s.writeError(w, r, err)
		return
	} else if found {
		if err := tx.Commit(r.Context()); err != nil {
			s.writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, r, http.StatusAccepted, existing, nil)
		return
	}
	run, err := s.enqueueCommercePreparationRunTx(r.Context(), tx, principal, project, identity, "")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, run, nil)
}

func loadActiveCommercePreparationRun(
	ctx context.Context,
	tx pgx.Tx,
	identity commerce.ScriptUnitPreparationIdentity,
) (WorkflowRun, bool, error) {
	identityRaw, err := json.Marshal(identity)
	if err != nil {
		return WorkflowRun{}, false, err
	}
	run, err := scanWorkflowRun(tx.QueryRow(ctx, workflowRunSelectSQL(`
		WHERE organization_id = $1 AND project_id = $2
		  AND workflow_type = 'commerce_script_unit_preparation'
		  AND status IN ('queued', 'running', 'waiting_review', 'cancelling')
		  AND input->'identity' = $3::jsonb
		ORDER BY created_at DESC
		LIMIT 1
		FOR UPDATE
	`), identity.OrganizationID, identity.ProjectID, identityRaw))
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkflowRun{}, false, nil
	}
	return run, err == nil, err
}

func loadActiveCommercePreparationLanguageRun(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	unitID string,
	sourceScriptVersionID string,
) (WorkflowRun, workflows.CommerceScriptUnitPreparationInput, bool, error) {
	run, err := scanWorkflowRun(tx.QueryRow(ctx, workflowRunSelectSQL(`
		WHERE organization_id = $1 AND project_id = $2
		  AND workflow_type = 'commerce_script_unit_preparation'
		  AND status IN ('queued', 'running', 'waiting_review')
		  AND input->'identity'->>'scriptUnitId' = $3
		  AND input->'identity'->>'sourceScriptVersionId' = $4
		ORDER BY created_at DESC
		LIMIT 1
		FOR UPDATE
	`), organizationID, projectID, unitID, sourceScriptVersionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkflowRun{}, workflows.CommerceScriptUnitPreparationInput{}, false, nil
	}
	if err != nil {
		return WorkflowRun{}, workflows.CommerceScriptUnitPreparationInput{}, false, err
	}
	var input workflows.CommerceScriptUnitPreparationInput
	if err := json.Unmarshal(run.Input, &input); err != nil {
		return WorkflowRun{}, workflows.CommerceScriptUnitPreparationInput{}, false, err
	}
	if input.WorkflowRunID != run.ID || input.Identity.OrganizationID != organizationID ||
		input.Identity.ProjectID != projectID || input.Identity.ScriptUnitID != unitID ||
		input.Identity.SourceScriptVersionID != sourceScriptVersionID {
		return WorkflowRun{}, workflows.CommerceScriptUnitPreparationInput{}, false,
			commerce.Error{Code: commerce.CodeGenerationMismatch, Message: "语言确认工作流身份不一致"}
	}
	return run, input, true, nil
}

func (s *Server) enqueueCommercePreparationRunTx(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	identity commerce.ScriptUnitPreparationIdentity,
	projectControlCommandID string,
) (WorkflowRun, error) {
	runID := uuid.NewString()
	input := workflows.CommerceScriptUnitPreparationInput{
		Identity: identity, WorkflowRunID: runID, CreatedBy: principal.UserID, AttemptGeneration: 1,
		ProjectControlCommandID: strings.TrimSpace(projectControlCommandID),
	}
	raw, _, err := marshalWorkflowStartInput(input)
	if err != nil {
		return WorkflowRun{}, err
	}
	temporalWorkflowID := "commerce-preparation-" + runID
	if _, err := tx.Exec(ctx, `
		INSERT INTO workflow_runs(
			id, organization_id, project_id, temporal_workflow_id, workflow_type,
			status, input, output, created_by, production_generation_id,
			video_production_binding_id, video_production_binding_revision
		)
		VALUES ($1, $2, $3, $4, 'commerce_script_unit_preparation',
		        'queued', $5, '{}', $6, $7, $8, $9)
	`, runID, project.OrganizationID, project.ID, temporalWorkflowID, raw, principal.UserID,
		identity.ProjectGenerationID, identity.VideoProductionBindingID,
		identity.VideoProductionBindingRevision); err != nil {
		return WorkflowRun{}, err
	}
	if err := s.enqueueWorkflowStartTx(
		ctx, tx, runID, "", project.OrganizationID, project.ID,
		identity.ProjectGenerationID, "commerce_script_unit_preparation",
		"commerce_script_unit_preparation", temporalWorkflowID,
		workflows.ScriptTaskQueue, input,
	); err != nil {
		return WorkflowRun{}, err
	}
	run, err := scanWorkflowRun(tx.QueryRow(ctx, workflowRunSelectSQL(`WHERE id = $1`), runID))
	if err != nil {
		return WorkflowRun{}, err
	}
	if err := insertWorkflowQueuedEventTx(ctx, tx, run, run.WorkflowType); err != nil {
		return WorkflowRun{}, err
	}
	return run, nil
}
