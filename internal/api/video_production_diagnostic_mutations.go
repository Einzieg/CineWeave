package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/production"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
)

type updateStoryboardShotTransitionRequest struct {
	ExpectedRevision int      `json:"expectedRevision"`
	TransitionType   string   `json:"transitionType"`
	Confidence       *float64 `json:"confidence,omitempty"`
	Reason           string   `json:"reason"`
}

type reviewShotVisualAnchorRequest struct {
	ExpectedRevision int    `json:"expectedRevision"`
	Reason           string `json:"reason"`
}

type generateShotVisualAnchorRequest struct {
	AnchorRole string `json:"anchorRole,omitempty"`
}

type reviewVideoPromptPlanRequest struct {
	PromptPlanID     string `json:"promptPlanId,omitempty"`
	ExpectedRevision int    `json:"expectedRevision"`
	Reason           string `json:"reason"`
}

type videoPromptPlanReviewResult struct {
	ID               string `json:"id"`
	StoryboardShotID string `json:"storyboardShotId"`
	Revision         int    `json:"revision"`
	Status           string `json:"status"`
}

type createManualVideoPromptPlanRevisionRequest struct {
	ShotID           string `json:"shotId,omitempty"`
	ExpectedRevision int    `json:"expectedRevision"`
	RenderedPrompt   string `json:"renderedPrompt"`
	Reason           string `json:"reason"`
}

type shotProductionEventIdentity struct {
	ProductionGenerationID         string
	VideoProductionBindingID       string
	VideoProductionBindingRevision int64
	ScriptEpisodeID                string
	WorkflowRunID                  string
}

func (s *Server) updateStoryboardShotTransition(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{authz.PermissionStoryboardGenerate, authz.PermissionProjectWrite})
	if !ok {
		return
	}
	shot, err := s.storyboardShotByID(r, project.ID, r.PathValue("shotId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req updateStoryboardShotTransitionRequest
	if !decode(w, r, &req) {
		return
	}
	if req.ExpectedRevision <= 0 || strings.TrimSpace(req.TransitionType) == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "expectedRevision and transitionType are required", nil, false)
		return
	}
	if req.Confidence != nil && (*req.Confidence < 0 || *req.Confidence > 1) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "confidence must be between 0 and 1", nil, false)
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var current StoryboardShotTransitionDetail
	if err := tx.QueryRow(r.Context(), `
		SELECT id::text, production_generation_id::text, storyboard_plan_id::text,
		       source_shot_id::text, target_shot_id::text, transition_type,
		       tail_policy, anchor_policy, carry_constraints, reset_constraints,
		       confidence::float8, revision, status, review_status, metadata,
		       created_at, updated_at
		FROM storyboard_shot_transitions
		WHERE project_id = $1 AND target_shot_id = $2 AND status = 'active'
		FOR UPDATE
	`, project.ID, shot.ID).Scan(
		&current.ID, &current.ProductionGenerationID, &current.StoryboardPlanID,
		&current.SourceShotID, &current.TargetShotID, &current.TransitionType,
		&current.TailPolicy, &current.AnchorPolicy, &current.CarryConstraints,
		&current.ResetConstraints, &current.Confidence, &current.Revision, &current.Status,
		&current.ReviewStatus, &current.Metadata, &current.CreatedAt, &current.UpdatedAt,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if current.Revision != req.ExpectedRevision {
		httpx.WriteError(w, r, http.StatusConflict, "REVISION_CONFLICT", "转场已被其他操作修改，请刷新后重试", map[string]any{"currentRevision": current.Revision}, false)
		return
	}

	entry, err := loadApprovedShotStateTx(r.Context(), tx, shot.ID, videoproduction.StateRolePlannedEntry)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var previous *videoproduction.ShotState
	if current.SourceShotID != nil {
		previous, err = loadApprovedShotStateTx(r.Context(), tx, *current.SourceShotID, videoproduction.StateRolePlannedExit)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	confidence := current.Confidence
	if req.Confidence != nil {
		confidence = *req.Confidence
	}
	classified, err := videoproduction.ClassifyTransition(previous, *entry, videoproduction.TransitionSuggestion{
		TransitionType: strings.TrimSpace(req.TransitionType),
		Confidence:     confidence,
	})
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error(), nil, false)
		return
	}
	transitionHash, err := videoproduction.HashTransition(classified)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error(), nil, false)
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE storyboard_shot_transitions SET status = 'superseded' WHERE id = $1`, current.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	var updated StoryboardShotTransitionDetail
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO storyboard_shot_transitions(
			organization_id, project_id, production_generation_id, storyboard_plan_id,
			source_shot_id, target_shot_id, transition_type, tail_policy, anchor_policy,
			carry_constraints, reset_constraints, confidence, revision, status,
			review_status, metadata
		)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid, $6, $7, $8, $9,
		        $10, $11, $12, $13, 'active', 'approved',
		        COALESCE($14::jsonb, '{}'::jsonb) || $15::jsonb)
		RETURNING id::text, production_generation_id::text, storyboard_plan_id::text,
		          source_shot_id::text, target_shot_id::text, transition_type,
		          tail_policy, anchor_policy, carry_constraints, reset_constraints,
		          confidence::float8, revision, status, review_status, metadata,
		          created_at, updated_at
	`, project.OrganizationID, project.ID, current.ProductionGenerationID, current.StoryboardPlanID,
		stringValue(current.SourceShotID), shot.ID, classified.TransitionType, classified.TailPolicy,
		classified.AnchorPolicy, mustMarshal(classified.Carry), mustMarshal(classified.Reset),
		classified.Confidence, current.Revision+1, current.Metadata, mustMarshal(map[string]any{
			"transitionHash":     transitionHash,
			"source":             "manual_request_deterministic_classifier_v1",
			"requestedType":      strings.TrimSpace(req.TransitionType),
			"requestedReason":    strings.TrimSpace(req.Reason),
			"previousRevisionId": current.ID,
			"updatedBy":          principal.UserID,
		})).Scan(
		&updated.ID, &updated.ProductionGenerationID, &updated.StoryboardPlanID,
		&updated.SourceShotID, &updated.TargetShotID, &updated.TransitionType,
		&updated.TailPolicy, &updated.AnchorPolicy, &updated.CarryConstraints,
		&updated.ResetConstraints, &updated.Confidence, &updated.Revision,
		&updated.Status, &updated.ReviewStatus, &updated.Metadata,
		&updated.CreatedAt, &updated.UpdatedAt,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := invalidateShotProductionContractsTx(r.Context(), tx, project.ID, shot.ID, true, "transition_changed"); err != nil {
		s.writeError(w, r, err)
		return
	}
	identity, err := loadShotProductionEventIdentityTx(r.Context(), tx, project.ID, shot.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "storyboard.shot.transition.updated", "storyboard_shot_transition", updated.ID, mustMarshal(map[string]any{
		"bindingId":              identity.VideoProductionBindingID,
		"bindingRevision":        identity.VideoProductionBindingRevision,
		"productionGenerationId": identity.ProductionGenerationID,
		"episodeId":              identity.ScriptEpisodeID,
		"storyboardShotId":       shot.ID,
		"workflowRunId":          identity.WorkflowRunID,
		"previousRevision":       current.Revision,
		"revision":               updated.Revision,
		"transitionHash":         transitionHash,
		"transitionType":         updated.TransitionType,
		"updatedBy":              principal.UserID,
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, updated, nil)
}

func (s *Server) replanStoryboardShotState(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionStoryboardGenerate)
	if !ok {
		return
	}
	result, err := s.replanStoryboardShotStateCore(r.Context(), principal, project, r.PathValue("shotId"), "")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, RegenerateResponse{
		TargetType: "shot_state", TargetID: r.PathValue("shotId"), WorkflowRunID: result.Run.ID,
		Status: result.Run.Status, WorkflowType: "script_to_storyboard",
	}, nil)
}

func (s *Server) generateStoryboardShotAnchor(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{authz.PermissionStoryboardGenerate, authz.PermissionWorkflowRun})
	if !ok {
		return
	}
	var req generateShotVisualAnchorRequest
	if !decode(w, r, &req) {
		return
	}
	result, err := s.generateStoryboardShotAnchorCore(r.Context(), principal, project, storyboardGenerateAnchorActionInput{
		ShotID: r.PathValue("shotId"), AnchorRole: req.AnchorRole,
	}, "")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, map[string]any{
		"anchorId": result.AnchorID, "workflowRunId": result.Run.ID, "status": result.Run.Status,
		"workflowType": "regenerate_shot_image", "anchorRole": firstNonEmpty(req.AnchorRole, videoproduction.AnchorRolePlannedFirstFrame),
	}, nil)
}

func (s *Server) approveStoryboardShotAnchor(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	s.reviewStoryboardShotAnchor(w, r, principal, "approved")
}

func (s *Server) rejectStoryboardShotAnchor(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	s.reviewStoryboardShotAnchor(w, r, principal, "rejected")
}

func (s *Server) reviewStoryboardShotAnchor(w http.ResponseWriter, r *http.Request, principal auth.Principal, decision string) {
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{authz.PermissionStoryboardGenerate, authz.PermissionProjectWrite})
	if !ok {
		return
	}
	var req reviewShotVisualAnchorRequest
	if !decode(w, r, &req) {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	item, err := s.reviewStoryboardShotAnchorActionTx(
		r.Context(), tx, project, principal.UserID, r.PathValue("shotId"), r.PathValue("anchorId"), decision, req,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	if item.StorageKey != nil {
		item.PreviewURL = s.previewURLForStorageKeyRequest(r.Context(), *item.StorageKey)
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) reviewStoryboardShotAnchorActionTx(ctx context.Context, tx pgx.Tx, project Project, actorID, shotID, anchorID, decision string, req reviewShotVisualAnchorRequest) (ShotVisualAnchorDetail, error) {
	shotID = strings.TrimSpace(shotID)
	anchorID = strings.TrimSpace(anchorID)
	decision = strings.TrimSpace(decision)
	if shotID == "" || anchorID == "" || req.ExpectedRevision <= 0 || (decision == "rejected" && strings.TrimSpace(req.Reason) == "") || (decision != "approved" && decision != "rejected") {
		return ShotVisualAnchorDetail{}, controlValidationError("shotId、anchorId、expectedRevision 为必填项，拒绝时必须填写原因")
	}
	shot, err := scanStoryboardShot(tx.QueryRow(ctx, storyboardShotSelectSQL(`
		WHERE s.project_id = $1 AND s.id = $2 AND s.deleted_at IS NULL
		FOR UPDATE OF s
	`), project.ID, shotID))
	if err != nil {
		return ShotVisualAnchorDetail{}, err
	}
	var item ShotVisualAnchorDetail
	if err := tx.QueryRow(ctx, `
		SELECT id::text, production_generation_id::text, storyboard_shot_id::text,
		       shot_state_version_id::text, anchor_role, revision, status, review_status,
		       artifact_id::text, media_file_id::text, storage_key, prompt,
		       prompt_version_id::text, prompt_hash, provider_call_id::text,
		       model_id::text, reference_pack_id::text, metadata, created_at, updated_at
		FROM shot_visual_anchors
		WHERE project_id = $1 AND storyboard_shot_id = $2 AND id = $3
		FOR UPDATE
	`, project.ID, shot.ID, anchorID).Scan(
		&item.ID, &item.ProductionGenerationID, &item.StoryboardShotID,
		&item.ShotStateVersionID, &item.AnchorRole, &item.Revision, &item.Status,
		&item.ReviewStatus, &item.ArtifactID, &item.MediaFileID, &item.StorageKey,
		&item.Prompt, &item.PromptVersionID, &item.PromptHash, &item.ProviderCallID,
		&item.ModelID, &item.ReferencePackID, &item.Metadata, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return ShotVisualAnchorDetail{}, err
	}
	if item.Revision != req.ExpectedRevision {
		apiErr := newAPIError(http.StatusConflict, "REVISION_CONFLICT", "锚点已被其他操作修改，请刷新后重试")
		apiErr.Details = map[string]any{"currentRevision": item.Revision}
		return ShotVisualAnchorDetail{}, apiErr
	}
	if item.Status != "ready" {
		apiErr := newAPIError(http.StatusConflict, "ANCHOR_NOT_READY", "锚点尚未生成完成，不能审核")
		apiErr.Details = map[string]any{"status": item.Status}
		return ShotVisualAnchorDetail{}, apiErr
	}
	if decision == "approved" {
		if _, err := tx.Exec(ctx, `
			UPDATE shot_visual_anchors
			SET status = 'stale', review_status = 'needs_edit', reference_pack_id = NULL,
			    metadata = metadata || jsonb_build_object('supersededAt', now(), 'supersededByAnchorId', $3::text)
			WHERE project_id = $1 AND storyboard_shot_id = $2 AND anchor_role = $4
			  AND id <> $3 AND status = 'ready' AND review_status = 'approved'
		`, project.ID, shot.ID, item.ID, item.AnchorRole); err != nil {
			return ShotVisualAnchorDetail{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE shot_visual_anchors
		SET review_status = $2,
		    metadata = metadata || jsonb_build_object(
		      'reviewedAt', now(), 'reviewedBy', $3::text,
		      'reviewDecision', $2::text, 'reviewReason', $4::text
		    )
		WHERE id = $1
	`, item.ID, decision, actorID, strings.TrimSpace(req.Reason)); err != nil {
		return ShotVisualAnchorDetail{}, err
	}
	if err := invalidateShotProductionContractsTx(ctx, tx, project.ID, shot.ID, false, "visual_anchor_review_changed"); err != nil {
		return ShotVisualAnchorDetail{}, err
	}
	identity, err := loadShotProductionEventIdentityTx(ctx, tx, project.ID, shot.ID)
	if err != nil {
		return ShotVisualAnchorDetail{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "storyboard.shot.anchor.reviewed", "shot_visual_anchor", item.ID, mustMarshal(map[string]any{
		"bindingId": identity.VideoProductionBindingID, "bindingRevision": identity.VideoProductionBindingRevision,
		"productionGenerationId": identity.ProductionGenerationID, "episodeId": identity.ScriptEpisodeID,
		"storyboardShotId": shot.ID, "workflowRunId": identity.WorkflowRunID,
		"anchorId": item.ID, "revision": item.Revision, "decision": decision,
	})); err != nil {
		return ShotVisualAnchorDetail{}, err
	}
	item.ReviewStatus = decision
	return item, nil
}

func (s *Server) approveVideoPromptPlan(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	s.reviewVideoPromptPlan(w, r, principal, "approved")
}

func (s *Server) createManualVideoPromptPlanRevision(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{authz.PermissionStoryboardGenerate, authz.PermissionWorkflowRun})
	if !ok {
		return
	}
	shot, err := s.storyboardShotByID(r, project.ID, r.PathValue("shotId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req createManualVideoPromptPlanRevisionRequest
	if !decode(w, r, &req) {
		return
	}
	req.ShotID = shot.ID
	prompt := strings.TrimSpace(req.RenderedPrompt)
	if req.ExpectedRevision <= 0 || prompt == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "expectedRevision and renderedPrompt are required", nil, false)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	result, err := s.createManualVideoPromptPlanRevisionActionTx(r.Context(), tx, project, principal.UserID, req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	items, err := s.videoPromptPlans(r, project.ID, shot.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	for index := range items {
		if items[index].ID == result.ID {
			httpx.WriteJSON(w, r, http.StatusCreated, items[index], nil)
			return
		}
	}
	s.writeError(w, r, pgx.ErrNoRows)
}

func (s *Server) createManualVideoPromptPlanRevisionActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorID string,
	req createManualVideoPromptPlanRevisionRequest,
) (videoPromptPlanReviewResult, error) {
	req.ShotID = strings.TrimSpace(req.ShotID)
	prompt := strings.TrimSpace(req.RenderedPrompt)
	if req.ShotID == "" || req.ExpectedRevision <= 0 || prompt == "" {
		return videoPromptPlanReviewResult{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "shotId, expectedRevision and renderedPrompt are required")
	}
	promptDigest := sha256.Sum256([]byte(prompt))
	promptHash := hex.EncodeToString(promptDigest[:])
	var sourceID, sourceStatus, contextStatus, referenceStatus string
	var sourceRevision int
	if err := tx.QueryRow(ctx, `
		SELECT plan.id::text, plan.revision, plan.status,
		       context.status,
		       COALESCE(reference.status, '')
		FROM video_prompt_plans plan
		JOIN prompt_context_plans context ON context.id = plan.prompt_context_plan_id
		LEFT JOIN shot_reference_packs reference
		  ON reference.storyboard_shot_id = plan.storyboard_shot_id
		 AND reference.manifest_hash = plan.reference_pack_hash
		 AND reference.status = 'active'
		WHERE plan.project_id = $1 AND plan.storyboard_shot_id = $2
		ORDER BY CASE WHEN plan.status = 'approved' THEN 0 ELSE 1 END, plan.revision DESC
		LIMIT 1
		FOR UPDATE OF plan
	`, project.ID, req.ShotID).Scan(&sourceID, &sourceRevision, &sourceStatus, &contextStatus, &referenceStatus); err != nil {
		return videoPromptPlanReviewResult{}, err
	}
	if sourceRevision != req.ExpectedRevision {
		return videoPromptPlanReviewResult{}, apiError{Status: http.StatusConflict, Code: "REVISION_CONFLICT", Message: "视频提示词计划已被其他操作修改，请刷新后重试", Details: map[string]any{"currentRevision": sourceRevision}}
	}
	if sourceStatus != "approved" || contextStatus != "active" || referenceStatus != "active" {
		return videoPromptPlanReviewResult{}, apiError{Status: http.StatusConflict, Code: "PROMPT_PLAN_STALE", Message: "当前提示词上下文或引用已过期，请先重新生成视频提示词", Details: map[string]any{
			"promptPlanStatus": sourceStatus, "contextStatus": contextStatus, "referencePackStatus": referenceStatus,
		}}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE video_prompt_plans
		SET status = 'stale', stale_at = now(),
		    metadata = metadata || jsonb_build_object('staleReason', 'manual_revision', 'staleAt', now())
		WHERE id = $1
	`, sourceID); err != nil {
		return videoPromptPlanReviewResult{}, err
	}
	var newPlanID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO video_prompt_plans(
			organization_id, project_id, production_generation_id,
			video_production_binding_id, video_production_binding_revision,
			profile_version_id, storyboard_shot_id, prompt_context_plan_id,
			prompt_version_id, reviewer_prompt_version_id, workflow_run_id, node_run_id,
			provider_call_id, reviewer_provider_call_id, provider_model_id,
			revision, status, rendered_prompt, prompt_hash, prompt_context_plan_hash,
			profile_snapshot_hash, shot_state_hash, transition_hash, reference_pack_hash,
			capability_snapshot_hash, input_contract_version, dialogue_cues,
			native_audio_required, audio_strategy, audio_requirement,
			reviewer_output, metadata, created_by, reviewed_at, approved_at
		)
		SELECT organization_id, project_id, production_generation_id,
		       video_production_binding_id, video_production_binding_revision,
		       profile_version_id, storyboard_shot_id, prompt_context_plan_id,
		       prompt_version_id, reviewer_prompt_version_id, NULL, NULL,
		       NULL, NULL, provider_model_id,
		       revision + 1, 'approved', $2, $3, prompt_context_plan_hash,
		       profile_snapshot_hash, shot_state_hash, transition_hash, reference_pack_hash,
		       capability_snapshot_hash, input_contract_version, dialogue_cues,
		       native_audio_required, audio_strategy, audio_requirement,
		       reviewer_output || jsonb_build_object(
		         'manualDecision', 'approved', 'manualReviewerId', $4::text,
		         'manualReason', $5::text, 'manualReviewedAt', now()
		       ),
		       metadata || jsonb_build_object(
		         'source', 'manual_revision', 'sourcePromptPlanId', id::text,
		         'sourceRevision', revision
		       ),
		       NULLIF($4::text, '')::uuid, now(), now()
		FROM video_prompt_plans WHERE id = $1
		RETURNING id::text
	`, sourceID, prompt, promptHash, actorID, strings.TrimSpace(req.Reason)).Scan(&newPlanID); err != nil {
		return videoPromptPlanReviewResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET video_prompt = $2, video_prompt_status = 'succeeded',
		    video_prompt_error_code = NULL, video_prompt_error_message = NULL,
		    video_prompt_updated_at = now(),
		    metadata = metadata || jsonb_build_object(
		      'activeVideoPromptPlanId', $3::text,
		      'videoPromptManualRevision', $4::int,
		      'videoPromptManualEditedBy', $5::text
		    ),
		    updated_at = now()
		WHERE project_id = $1 AND id = $6
	`, project.ID, prompt, newPlanID, sourceRevision+1, actorID, req.ShotID); err != nil {
		return videoPromptPlanReviewResult{}, err
	}
	if err := invalidateShotRenderPlansTx(ctx, tx, project.ID, req.ShotID, "video_prompt_manual_revision"); err != nil {
		return videoPromptPlanReviewResult{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "storyboard.shot.video_prompt.reviewed", "storyboard_shot", req.ShotID, mustMarshal(map[string]any{
		"storyboardShotId": req.ShotID, "videoPromptPlanId": newPlanID,
		"revision": sourceRevision + 1, "decision": "approved", "reviewedBy": actorID,
		"source": "manual_revision",
	})); err != nil {
		return videoPromptPlanReviewResult{}, err
	}
	return videoPromptPlanReviewResult{ID: newPlanID, StoryboardShotID: req.ShotID, Revision: sourceRevision + 1, Status: "approved"}, nil
}

func (s *Server) executeShotVideoPromptCreateRevisionSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	_ projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	var input createManualVideoPromptPlanRevisionRequest
	if err := decodeWorkflowActionInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	result, err := s.createManualVideoPromptPlanRevisionActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("shot.video_prompt.create_revision", workflowActionArguments(raw), "已创建并批准人工视频提示词版本。", map[string]any{
		"videoPromptPlan": result,
	}), nil
}

func (s *Server) rejectVideoPromptPlan(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	s.reviewVideoPromptPlan(w, r, principal, "rejected")
}

func (s *Server) reviewVideoPromptPlan(w http.ResponseWriter, r *http.Request, principal auth.Principal, decision string) {
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{authz.PermissionStoryboardGenerate, authz.PermissionWorkflowRun})
	if !ok {
		return
	}
	var req reviewVideoPromptPlanRequest
	if !decode(w, r, &req) {
		return
	}
	req.PromptPlanID = r.PathValue("promptPlanId")
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	result, err := s.reviewVideoPromptPlanActionTx(r.Context(), tx, project, principal.UserID, decision, req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, result, nil)
}

func (s *Server) reviewVideoPromptPlanActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorID string,
	decision string,
	req reviewVideoPromptPlanRequest,
) (videoPromptPlanReviewResult, error) {
	req.PromptPlanID = strings.TrimSpace(req.PromptPlanID)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.PromptPlanID == "" || req.ExpectedRevision <= 0 || (decision == "rejected" && req.Reason == "") {
		return videoPromptPlanReviewResult{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "promptPlanId and expectedRevision are required and rejection requires a reason")
	}
	if decision != "approved" && decision != "rejected" {
		return videoPromptPlanReviewResult{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "decision is invalid")
	}
	var planID, shotID, status string
	var revision int
	if err := tx.QueryRow(ctx, `
		SELECT id::text, storyboard_shot_id::text, revision, status
		FROM video_prompt_plans
		WHERE project_id = $1 AND id = $2
		FOR UPDATE
	`, project.ID, req.PromptPlanID).Scan(&planID, &shotID, &revision, &status); err != nil {
		return videoPromptPlanReviewResult{}, err
	}
	if revision != req.ExpectedRevision {
		return videoPromptPlanReviewResult{}, apiError{Status: http.StatusConflict, Code: "REVISION_CONFLICT", Message: "视频提示词计划已被其他操作修改，请刷新后重试", Details: map[string]any{"currentRevision": revision}}
	}
	if status == "stale" || status == "archived" {
		return videoPromptPlanReviewResult{}, apiError{Status: http.StatusConflict, Code: "PROMPT_PLAN_STALE", Message: "过期或归档的视频提示词计划不能审核", Details: map[string]any{"status": status}}
	}
	if decision == "approved" && status != "approved" {
		if _, err := tx.Exec(ctx, `
			UPDATE video_prompt_plans
			SET status = 'stale', stale_at = now(),
			    metadata = metadata || jsonb_build_object('staleReason', 'replaced_by_manual_approval', 'replacementPromptPlanId', $3::text)
			WHERE project_id = $1 AND storyboard_shot_id = $2 AND id <> $3 AND status = 'approved'
		`, project.ID, shotID, planID); err != nil {
			return videoPromptPlanReviewResult{}, err
		}
	}
	if status != decision {
		if _, err := tx.Exec(ctx, `
			UPDATE video_prompt_plans
			SET status = $2,
			    reviewed_at = now(),
			    approved_at = CASE WHEN $2 = 'approved' THEN now() ELSE NULL END,
			    reviewer_output = reviewer_output || jsonb_build_object(
			      'manualDecision', $2::text, 'manualReason', $3::text,
			      'manualReviewerId', $4::text, 'manualReviewedAt', now()
			    )
			WHERE id = $1
		`, planID, decision, req.Reason, actorID); err != nil {
			return videoPromptPlanReviewResult{}, err
		}
	}
	if err := invalidateShotRenderPlansTx(ctx, tx, project.ID, shotID, "video_prompt_review_changed"); err != nil {
		return videoPromptPlanReviewResult{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "storyboard.shot.video_prompt.reviewed", "storyboard_shot", shotID, mustMarshal(map[string]any{
		"storyboardShotId": shotID, "videoPromptPlanId": planID, "revision": revision,
		"decision": decision, "reviewedBy": actorID,
	})); err != nil {
		return videoPromptPlanReviewResult{}, err
	}
	return videoPromptPlanReviewResult{ID: planID, StoryboardShotID: shotID, Revision: revision, Status: decision}, nil
}

func (s *Server) executeShotVideoPromptReviewSyncAction(decision string) projectControlSyncAction {
	return func(ctx context.Context, tx pgx.Tx, principal auth.Principal, project Project, _ projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
		var input reviewVideoPromptPlanRequest
		if err := decodeWorkflowActionInput(raw, &input); err != nil {
			return agentToolResult{}, err
		}
		result, err := s.reviewVideoPromptPlanActionTx(ctx, tx, project, principal.UserID, decision, input)
		if err != nil {
			return agentToolResult{}, err
		}
		name := "shot.video_prompt.approve"
		if decision == "rejected" {
			name = "shot.video_prompt.reject"
		}
		return agentToolOK(name, workflowActionArguments(raw), "已完成视频提示词审核。", map[string]any{"videoPromptPlan": result}), nil
	}
}

func loadApprovedShotStateTx(ctx context.Context, tx pgx.Tx, shotID, role string) (*videoproduction.ShotState, error) {
	var raw json.RawMessage
	if err := tx.QueryRow(ctx, `
		SELECT state
		FROM storyboard_shot_state_versions
		WHERE storyboard_shot_id = $1 AND state_role = $2 AND status = 'approved'
	`, shotID, role).Scan(&raw); err != nil {
		return nil, err
	}
	var state videoproduction.ShotState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func loadShotProductionEventIdentityTx(ctx context.Context, tx pgx.Tx, projectID, shotID string) (shotProductionEventIdentity, error) {
	var item shotProductionEventIdentity
	err := tx.QueryRow(ctx, `
		SELECT shot.production_generation_id::text, generation.binding_id::text,
		       binding.revision, plan.script_episode_id::text,
		       COALESCE(shot.workflow_run_id::text,
		         (SELECT run.id::text FROM workflow_runs run WHERE run.project_id = shot.project_id ORDER BY run.created_at DESC LIMIT 1),
		         generation.id::text)
		FROM storyboard_shots shot
		JOIN storyboard_plans plan ON plan.id = shot.storyboard_plan_id
		JOIN project_video_production_generations generation ON generation.id = shot.production_generation_id
		JOIN project_video_production_bindings binding ON binding.id = generation.binding_id
		WHERE shot.project_id = $1 AND shot.id = $2
	`, projectID, shotID).Scan(
		&item.ProductionGenerationID, &item.VideoProductionBindingID,
		&item.VideoProductionBindingRevision, &item.ScriptEpisodeID, &item.WorkflowRunID,
	)
	return item, err
}

func invalidateShotProductionContractsTx(ctx context.Context, tx pgx.Tx, projectID, shotID string, invalidateAnchor bool, reason string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE shot_reference_packs SET status = 'stale'
		WHERE project_id = $1 AND storyboard_shot_id = $2 AND status = 'active'
	`, projectID, shotID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE prompt_context_plans SET status = 'stale', stale_at = now()
		WHERE project_id = $1 AND storyboard_shot_id = $2 AND status = 'active'
	`, projectID, shotID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE video_prompt_plans
		SET status = 'stale', stale_at = now(),
		    metadata = metadata || jsonb_build_object('staleReason', $3::text, 'staleAt', now())
		WHERE project_id = $1 AND storyboard_shot_id = $2 AND status NOT IN ('stale', 'archived')
	`, projectID, shotID, reason); err != nil {
		return err
	}
	if invalidateAnchor {
		if _, err := tx.Exec(ctx, `
			UPDATE shot_visual_anchors
			SET status = 'stale', review_status = 'needs_edit', reference_pack_id = NULL,
			    metadata = metadata || jsonb_build_object('staleReason', $3::text, 'staleAt', now())
			WHERE project_id = $1 AND storyboard_shot_id = $2 AND status NOT IN ('stale', 'archived')
		`, projectID, shotID, reason); err != nil {
			return err
		}
	}
	return invalidateShotRenderPlansTx(ctx, tx, projectID, shotID, reason)
}

func invalidateShotRenderPlansTx(ctx context.Context, tx pgx.Tx, projectID, shotID, reason string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE video_render_plans
		SET active = false,
		    status = CASE WHEN status IN ('planned', 'running', 'succeeded') THEN 'stale' ELSE status END,
		    metadata = metadata || jsonb_build_object('staleReason', $3::text, 'staleAt', now()),
		    updated_at = now()
		WHERE project_id = $1 AND storyboard_shot_id = $2 AND active = true
	`, projectID, shotID, reason); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET active_video_render_plan_id = NULL,
		    video_status = CASE WHEN video_status IN ('queued', 'running', 'succeeded') THEN 'stale' ELSE video_status END,
		    production_readiness = 'preview_only', stale_state = 'needs_regeneration', updated_at = now()
		WHERE project_id = $1 AND id = $2
	`, projectID, shotID); err != nil {
		return err
	}
	return production.MarkFinalVideoStale(ctx, tx, projectID, "")
}

func (s *Server) markShotAnchorGenerating(ctx context.Context, project Project, shotID, anchorRole, profileKey, workflowRunID, userID string) (string, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	anchorID, err := s.markShotAnchorGeneratingTx(ctx, tx, project, shotID, anchorRole, profileKey, workflowRunID, userID)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return anchorID, nil
}

func (s *Server) markShotAnchorGeneratingTx(ctx context.Context, tx pgx.Tx, project Project, shotID, anchorRole, profileKey, workflowRunID, userID string) (string, error) {
	identity, err := loadShotProductionEventIdentityTx(ctx, tx, project.ID, shotID)
	if err != nil {
		return "", err
	}
	stateRole := ""
	strategy, err := videoproduction.ProfileStrategyFor(profileKey)
	if err != nil {
		return "", err
	}
	for _, requirement := range strategy.Anchors().Requirements() {
		if requirement.Role == anchorRole {
			stateRole = requirement.StateRole
			break
		}
	}
	if strings.TrimSpace(stateRole) == "" {
		return "", videoproduction.NewError(videoproduction.CodeProfileIncompatible, "当前视频生产方案的锚点没有绑定镜头状态", false)
	}
	var anchorID, status, reviewStatus string
	var revision int
	err = tx.QueryRow(ctx, `
		SELECT id::text, revision, status, review_status
		FROM shot_visual_anchors
		WHERE project_id = $1 AND storyboard_shot_id = $2 AND anchor_role = $3
		ORDER BY revision DESC LIMIT 1
		FOR UPDATE
	`, project.ID, shotID, anchorRole).Scan(&anchorID, &revision, &status, &reviewStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		var stateVersionID string
		if err := tx.QueryRow(ctx, `
			SELECT id::text FROM storyboard_shot_state_versions
			WHERE storyboard_shot_id = $1 AND state_role = $2 AND status = 'approved'
		`, shotID, stateRole).Scan(&stateVersionID); err != nil {
			return "", err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO shot_visual_anchors(
				organization_id, project_id, production_generation_id, storyboard_shot_id,
				shot_state_version_id, anchor_role, revision, status, review_status, metadata
			)
			VALUES ($1, $2, $3, $4, $5, $6, 1, 'generating', 'pending', $7)
			RETURNING id::text
		`, project.OrganizationID, project.ID, identity.ProductionGenerationID, shotID, stateVersionID, anchorRole,
			mustMarshal(map[string]any{"workflowRunId": workflowRunID, "requestedBy": userID, "anchorRole": anchorRole})).Scan(&anchorID); err != nil {
			return "", err
		}
		revision = 1
	} else if err != nil {
		return "", err
	} else if status == "ready" || status == "stale" || status == "archived" || reviewStatus == "approved" {
		if _, err := tx.Exec(ctx, `
			UPDATE shot_visual_anchors
			SET status = 'stale', review_status = 'needs_edit', reference_pack_id = NULL,
			    metadata = metadata || jsonb_build_object('supersededAt', now())
			WHERE id = $1
		`, anchorID); err != nil {
			return "", err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO shot_visual_anchors(
				organization_id, project_id, production_generation_id, storyboard_shot_id,
				shot_state_version_id, anchor_role, revision, status, review_status, metadata
			)
			SELECT organization_id, project_id, production_generation_id, storyboard_shot_id,
			       shot_state_version_id, anchor_role, revision + 1, 'generating', 'pending',
			       metadata || jsonb_build_object('workflowRunId', $2::text, 'requestedBy', $3::text, 'previousAnchorId', id::text)
			FROM shot_visual_anchors WHERE id = $1
			RETURNING id::text, revision
		`, anchorID, workflowRunID, userID).Scan(&anchorID, &revision); err != nil {
			return "", err
		}
	} else {
		if _, err := tx.Exec(ctx, `
			UPDATE shot_visual_anchors
			SET status = 'generating', review_status = 'pending',
			    artifact_id = NULL, media_file_id = NULL, storage_key = NULL,
			    provider_call_id = NULL, model_id = NULL, reference_pack_id = NULL,
			    metadata = metadata || jsonb_build_object('workflowRunId', $2::text, 'requestedBy', $3::text)
			WHERE id = $1
		`, anchorID, workflowRunID, userID); err != nil {
			return "", err
		}
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "storyboard.shot.anchor.started", "shot_visual_anchor", anchorID, mustMarshal(map[string]any{
		"bindingId": identity.VideoProductionBindingID, "bindingRevision": identity.VideoProductionBindingRevision,
		"productionGenerationId": identity.ProductionGenerationID, "episodeId": identity.ScriptEpisodeID,
		"storyboardShotId": shotID, "workflowRunId": workflowRunID,
		"anchorId": anchorID, "anchorRole": anchorRole, "revision": revision,
	})); err != nil {
		return "", err
	}
	return anchorID, nil
}
