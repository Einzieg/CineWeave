package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	commerceReferenceImageBatchHandler = "commerce_reference_image_batch"
	commerceVideoPromptBatchHandler    = "commerce_video_prompt_batch"
	commerceShotVideoBatchHandler      = "commerce_shot_video_batch"
)

type commerceReferenceImageBatchRequest struct {
	Operation                string   `json:"operation"`
	PlanID                   string   `json:"planId"`
	ExpectedPlanRevision     int64    `json:"expectedPlanRevision"`
	ExpectedUnitGenerationID string   `json:"expectedUnitGenerationId"`
	ShotIDs                  []string `json:"shotIds"`
	Force                    bool     `json:"force"`
	Concurrency              int      `json:"concurrency"`
}

type commerceReferenceImageRunSnapshot struct {
	Operation    string   `json:"operation"`
	PlanID       string   `json:"planId"`
	PlanRevision int64    `json:"planRevision"`
	ShotIDs      []string `json:"shotIds"`
	Force        bool     `json:"force"`
	Concurrency  int      `json:"concurrency"`
	RetryOfRunID string   `json:"retryOfRunId,omitempty"`
}

type commerceVideoBatchRequest struct {
	PlanID                   string   `json:"planId"`
	ExpectedPlanRevision     int64    `json:"expectedPlanRevision"`
	ExpectedUnitGenerationID string   `json:"expectedUnitGenerationId"`
	ShotIDs                  []string `json:"shotIds"`
	Force                    bool     `json:"force"`
	Concurrency              int      `json:"concurrency"`
	Resolution               string   `json:"resolution"`
}

type commerceVideoRunSnapshot struct {
	Operation    string   `json:"operation"`
	PlanID       string   `json:"planId"`
	PlanRevision int64    `json:"planRevision"`
	ShotIDs      []string `json:"shotIds"`
	Force        bool     `json:"force"`
	Concurrency  int      `json:"concurrency"`
	Resolution   string   `json:"resolution"`
	RetryOfRunID string   `json:"retryOfRunId,omitempty"`
}

type commerceRetryProductionRunRequest struct {
	ItemIDs     []string `json:"itemIds"`
	Concurrency int      `json:"concurrency"`
}

type commerceCancelProductionRunRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) generateCommerceReferenceImageBatch(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionStoryboardGenerate)
	if !ok {
		return
	}
	if s.temporal == nil {
		s.writeError(w, r, apiError{Status: http.StatusServiceUnavailable, Code: "TEMPORAL_UNAVAILABLE", Message: "工作流服务暂不可用", Retryable: true})
		return
	}
	var req commerceReferenceImageBatchRequest
	if !decode(w, r, &req) {
		return
	}
	if err := normalizeCommerceReferenceImageBatchRequest(&req); err != nil {
		s.writeError(w, r, err)
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED", "批量生产参考图需要请求标识", nil, false)
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	scope := "commerce_reference_images:" + r.PathValue("scriptUnitId") + ":" + req.Operation
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId": project.ID, "scriptUnitId": r.PathValue("scriptUnitId"),
		"operation": req.Operation, "planId": req.PlanID,
		"expectedPlanRevision":     req.ExpectedPlanRevision,
		"expectedUnitGenerationId": req.ExpectedUnitGenerationID,
		"shotIds":                  req.ShotIDs, "force": req.Force, "concurrency": req.Concurrency,
	})
	claim, err := claimIdempotencyTx(r.Context(), tx, project.OrganizationID, scope, idempotencyKey, requestHash)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if len(claim.replaySnapshot) > 0 {
		var response commercepkg.ProductionRun
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

	run, created, err := s.createCommerceReferenceImageRunTx(
		r.Context(), tx, project, principal.UserID, r.PathValue("scriptUnitId"),
		req, scope, idempotencyKey, "",
	)
	if err != nil {
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
	httpx.WriteJSON(w, r, http.StatusAccepted, run, map[string]any{"created": created})
}

func (s *Server) generateCommerceVideoPromptBatch(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	s.generateCommerceVideoBatch(w, r, principal, "generate_prompts")
}

func (s *Server) generateCommerceShotVideoBatch(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	s.generateCommerceVideoBatch(w, r, principal, "generate_videos")
}

func (s *Server) generateCommerceVideoBatch(
	w http.ResponseWriter,
	r *http.Request,
	principal auth.Principal,
	operation string,
) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionStoryboardGenerate)
	if !ok {
		return
	}
	if s.temporal == nil {
		s.writeError(w, r, apiError{Status: http.StatusServiceUnavailable, Code: "TEMPORAL_UNAVAILABLE", Message: "工作流服务暂不可用", Retryable: true})
		return
	}
	var req commerceVideoBatchRequest
	if !decode(w, r, &req) {
		return
	}
	if err := normalizeCommerceVideoBatchRequest(&req); err != nil {
		s.writeError(w, r, err)
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED", "批量视频生产需要请求标识", nil, false)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	scope := "commerce_video:" + r.PathValue("scriptUnitId") + ":" + operation
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId":                project.ID,
		"scriptUnitId":             r.PathValue("scriptUnitId"),
		"operation":                operation,
		"planId":                   req.PlanID,
		"expectedPlanRevision":     req.ExpectedPlanRevision,
		"expectedUnitGenerationId": req.ExpectedUnitGenerationID,
		"shotIds":                  req.ShotIDs,
		"force":                    req.Force,
		"concurrency":              req.Concurrency,
		"resolution":               req.Resolution,
	})
	claim, err := claimIdempotencyTx(r.Context(), tx, project.OrganizationID, scope, idempotencyKey, requestHash)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if len(claim.replaySnapshot) > 0 {
		var response commercepkg.ProductionRun
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
	run, created, err := s.createCommerceVideoRunTx(
		r.Context(), tx, project, principal.UserID, r.PathValue("scriptUnitId"),
		operation, req, scope, idempotencyKey, "",
	)
	if err != nil {
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
	httpx.WriteJSON(w, r, http.StatusAccepted, run, map[string]any{"created": created})
}

func (s *Server) createCommerceVideoRunTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	createdBy string,
	scriptUnitID string,
	operation string,
	req commerceVideoBatchRequest,
	idempotencyScope string,
	idempotencyKey string,
	retryOfRunID string,
) (commercepkg.ProductionRun, bool, error) {
	identity, err := s.commerceCatalog.LockActiveStoryboardGeneration(
		ctx, tx, project.OrganizationID, project.ID, scriptUnitID,
	)
	if err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	if identity.UnitGenerationID != req.ExpectedUnitGenerationID {
		return commercepkg.ProductionRun{}, false, commercepkg.Error{
			Code: commercepkg.CodeGenerationMismatch, Message: "脚本单元生产代已变化，请刷新后重试",
		}
	}
	detail, err := s.commerceCatalog.GetStoryboardPlan(
		ctx, tx, project.OrganizationID, project.ID, identity.ScriptUnitID, req.PlanID,
	)
	if err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	shotIDs, err := validateCommerceVideoSelection(ctx, tx, detail, identity, operation, req)
	if err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	workflowInput := workflows.CommerceVideoBatchInput{
		Identity:          identity,
		Operation:         operation,
		ShotIDs:           shotIDs,
		StoryboardPlanID:  req.PlanID,
		PlanEditRevision:  int(req.ExpectedPlanRevision),
		Force:             req.Force,
		Concurrency:       req.Concurrency,
		Resolution:        req.Resolution,
		CreatedBy:         createdBy,
		AttemptGeneration: 1,
	}
	subjects := make([]commercepkg.ProductionSubject, 0, len(shotIDs))
	for _, shotID := range shotIDs {
		inputHash, err := workflows.CommerceVideoSubjectHash(workflowInput, shotID)
		if err != nil {
			return commercepkg.ProductionRun{}, false, err
		}
		subjects = append(subjects, commercepkg.ProductionSubject{
			Type:             commercepkg.SubjectStoryboardShot,
			Key:              shotID,
			StoryboardShotID: shotID,
			InputHash:        inputHash,
		})
	}
	runType := commercepkg.RunTypeVideoPrompts
	workflowType := "commerce_video_prompts"
	handler := commerceVideoPromptBatchHandler
	if operation == "generate_videos" {
		runType = commercepkg.RunTypeShotVideos
		workflowType = "commerce_shot_videos"
		handler = commerceShotVideoBatchHandler
	}
	inputSnapshot := mustRawJSON(commerceVideoRunSnapshot{
		Operation:    operation,
		PlanID:       req.PlanID,
		PlanRevision: req.ExpectedPlanRevision,
		ShotIDs:      shotIDs,
		Force:        req.Force,
		Concurrency:  req.Concurrency,
		Resolution:   req.Resolution,
		RetryOfRunID: retryOfRunID,
	})
	run, created, err := s.commerceCatalog.CreateProductionRun(ctx, tx, commercepkg.CreateProductionRunParams{
		Identity:         identity,
		RunType:          runType,
		IdempotencyScope: idempotencyScope,
		IdempotencyKey:   idempotencyKey,
		InputSnapshot:    inputSnapshot,
		Subjects:         subjects,
		CreatedBy:        createdBy,
	})
	if err != nil || !created {
		return run, created, err
	}
	workflowRunID := uuid.NewString()
	workflowInput.WorkflowRunID = workflowRunID
	workflowInput.ProductionRunID = run.ID
	temporalWorkflowID := workflowType + "-" + run.ID
	raw, _, err := marshalWorkflowStartInput(workflowInput)
	if err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO workflow_runs(
			id, organization_id, project_id, temporal_workflow_id, workflow_type,
			status, input, output, created_by, production_generation_id,
			video_production_binding_id, video_production_binding_revision,
			attempt_generation
		)
		VALUES ($1, $2, $3, $4, $5, 'queued', $6, '{}', $7, $8, $9, $10, 1)
	`, workflowRunID, identity.OrganizationID, identity.ProjectID, temporalWorkflowID,
		workflowType, raw, createdBy, identity.ProjectGenerationID,
		identity.VideoProductionBindingID, identity.VideoProductionBindingRevision); err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	if err := s.enqueueWorkflowStartTx(
		ctx, tx, workflowRunID, "", identity.OrganizationID, identity.ProjectID,
		identity.ProjectGenerationID, workflowType, handler,
		temporalWorkflowID, workflows.ScriptTaskQueue, workflowInput,
	); err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	if err := s.commerceCatalog.AttachProductionRunWorkflow(
		ctx, tx, identity.OrganizationID, identity.ProjectID, run.ID, workflowRunID,
	); err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	workflowRun, err := scanWorkflowRun(tx.QueryRow(ctx, workflowRunSelectSQL(`WHERE id = $1`), workflowRunID))
	if err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	if err := insertWorkflowQueuedEventTx(ctx, tx, workflowRun, workflowType); err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	run.WorkflowRunID = workflowRunID
	return run, true, nil
}

func (s *Server) createCommerceReferenceImageRunTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	createdBy string,
	scriptUnitID string,
	req commerceReferenceImageBatchRequest,
	idempotencyScope string,
	idempotencyKey string,
	retryOfRunID string,
) (commercepkg.ProductionRun, bool, error) {
	identity, err := s.commerceCatalog.LockActiveStoryboardGeneration(
		ctx, tx, project.OrganizationID, project.ID, scriptUnitID,
	)
	if err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	if identity.UnitGenerationID != req.ExpectedUnitGenerationID {
		return commercepkg.ProductionRun{}, false, commercepkg.Error{
			Code: commercepkg.CodeGenerationMismatch, Message: "脚本单元生产代已变化，请刷新后重试",
		}
	}
	detail, err := s.commerceCatalog.GetStoryboardPlan(
		ctx, tx, project.OrganizationID, project.ID, identity.ScriptUnitID, req.PlanID,
	)
	if err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	shotIDs, err := validateCommerceReferenceImageSelection(detail, identity, req)
	if err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	workflowInput := workflows.CommerceReferenceImageBatchInput{
		Identity: identity, Operation: req.Operation, ShotIDs: shotIDs,
		StoryboardPlanID: req.PlanID, PlanEditRevision: int(req.ExpectedPlanRevision),
		Force: req.Force, Concurrency: req.Concurrency,
		CreatedBy: createdBy, AttemptGeneration: 1,
	}
	subjects := make([]commercepkg.ProductionSubject, 0, len(shotIDs))
	for _, shotID := range shotIDs {
		inputHash, err := workflows.CommerceReferenceImageSubjectHash(workflowInput, shotID)
		if err != nil {
			return commercepkg.ProductionRun{}, false, err
		}
		subjects = append(subjects, commercepkg.ProductionSubject{
			Type: commercepkg.SubjectStoryboardShot, Key: shotID,
			StoryboardShotID: shotID, InputHash: inputHash,
		})
	}
	inputSnapshot := mustRawJSON(commerceReferenceImageRunSnapshot{
		Operation: req.Operation, PlanID: req.PlanID, PlanRevision: req.ExpectedPlanRevision,
		ShotIDs: shotIDs, Force: req.Force, Concurrency: req.Concurrency, RetryOfRunID: retryOfRunID,
	})
	run, created, err := s.commerceCatalog.CreateProductionRun(ctx, tx, commercepkg.CreateProductionRunParams{
		Identity: identity, RunType: commercepkg.RunTypeReferenceImages,
		IdempotencyScope: idempotencyScope, IdempotencyKey: idempotencyKey,
		InputSnapshot: inputSnapshot, Subjects: subjects, CreatedBy: createdBy,
	})
	if err != nil || !created {
		return run, created, err
	}
	workflowRunID := uuid.NewString()
	workflowInput.WorkflowRunID = workflowRunID
	workflowInput.ProductionRunID = run.ID
	workflowType := commerceReferenceImageWorkflowType(req.Operation)
	temporalWorkflowID := workflowType + "-" + run.ID
	raw, _, err := marshalWorkflowStartInput(workflowInput)
	if err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO workflow_runs(
			id, organization_id, project_id, temporal_workflow_id, workflow_type,
			status, input, output, created_by, production_generation_id,
			video_production_binding_id, video_production_binding_revision,
			attempt_generation
		)
		VALUES ($1, $2, $3, $4, $5, 'queued', $6, '{}', $7, $8, $9, $10, 1)
	`, workflowRunID, identity.OrganizationID, identity.ProjectID, temporalWorkflowID,
		workflowType, raw, createdBy, identity.ProjectGenerationID,
		identity.VideoProductionBindingID, identity.VideoProductionBindingRevision); err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	if err := s.enqueueWorkflowStartTx(
		ctx, tx, workflowRunID, "", identity.OrganizationID, identity.ProjectID,
		identity.ProjectGenerationID, workflowType, commerceReferenceImageBatchHandler,
		temporalWorkflowID, workflows.ScriptTaskQueue, workflowInput,
	); err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	if err := s.commerceCatalog.AttachProductionRunWorkflow(
		ctx, tx, identity.OrganizationID, identity.ProjectID, run.ID, workflowRunID,
	); err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	workflowRun, err := scanWorkflowRun(tx.QueryRow(ctx, workflowRunSelectSQL(`WHERE id = $1`), workflowRunID))
	if err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	if err := insertWorkflowQueuedEventTx(ctx, tx, workflowRun, workflowType); err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	run.WorkflowRunID = workflowRunID
	return run, true, nil
}

func (s *Server) listCommerceProductionRuns(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.commerceCatalog.ListProductionRuns(
		r.Context(), s.db, project.OrganizationID, project.ID,
		strings.TrimSpace(r.URL.Query().Get("filter[scriptUnitId]")),
		strings.TrimSpace(r.URL.Query().Get("filter[runType]")), limit,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) getCommerceProductionRun(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead)
	if !ok {
		return
	}
	item, err := s.commerceCatalog.GetProductionRun(
		r.Context(), s.db, project.OrganizationID, project.ID, r.PathValue("runId"),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) retryFailedCommerceProductionRun(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionStoryboardGenerate)
	if !ok {
		return
	}
	if s.temporal == nil {
		s.writeError(w, r, apiError{Status: http.StatusServiceUnavailable, Code: "TEMPORAL_UNAVAILABLE", Message: "工作流服务暂不可用", Retryable: true})
		return
	}
	var req commerceRetryProductionRunRequest
	if !decode(w, r, &req) {
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED", "重试失败项需要请求标识", nil, false)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	original, err := s.commerceCatalog.GetProductionRun(
		r.Context(), tx, project.OrganizationID, project.ID, r.PathValue("runId"),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if original.Run.Status != commercepkg.RunFailed && original.Run.Status != commercepkg.RunPartiallySucceeded {
		s.writeError(w, r, commercepkg.Error{Code: commercepkg.CodeRunStateConflict, Message: "当前批次没有可重试的失败项"})
		return
	}
	selectedItems, err := selectRetryableCommerceRunItems(original.Items, req.ItemIDs)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	shotIDs := make([]string, 0, len(selectedItems))
	itemIDs := make([]string, 0, len(selectedItems))
	for _, item := range selectedItems {
		shotIDs = append(shotIDs, item.Subject.StoryboardShotID)
		itemIDs = append(itemIDs, item.ID)
	}
	var referenceReq commerceReferenceImageBatchRequest
	var videoReq commerceVideoBatchRequest
	var finalReq commerceFinalComposeRequest
	videoOperation := ""
	resolvedConcurrency := req.Concurrency
	switch original.Run.RunType {
	case commercepkg.RunTypeReferenceImages:
		var snapshot commerceReferenceImageRunSnapshot
		if err := json.Unmarshal(original.Run.InputSnapshot, &snapshot); err != nil {
			s.writeError(w, r, commercepkg.Error{Code: commercepkg.CodeRunStateConflict, Message: "原批次输入快照无法解析", Cause: err})
			return
		}
		referenceReq = commerceReferenceImageBatchRequest{
			Operation: snapshot.Operation, PlanID: snapshot.PlanID,
			ExpectedPlanRevision:     snapshot.PlanRevision,
			ExpectedUnitGenerationID: original.Run.Identity.UnitGenerationID,
			ShotIDs:                  shotIDs, Force: true, Concurrency: resolvedConcurrency,
		}
		if referenceReq.Concurrency == 0 {
			referenceReq.Concurrency = snapshot.Concurrency
		}
		if err := normalizeCommerceReferenceImageBatchRequest(&referenceReq); err != nil {
			s.writeError(w, r, err)
			return
		}
		resolvedConcurrency = referenceReq.Concurrency
	case commercepkg.RunTypeVideoPrompts, commercepkg.RunTypeShotVideos:
		var snapshot commerceVideoRunSnapshot
		if err := json.Unmarshal(original.Run.InputSnapshot, &snapshot); err != nil {
			s.writeError(w, r, commercepkg.Error{Code: commercepkg.CodeRunStateConflict, Message: "原视频批次输入快照无法解析", Cause: err})
			return
		}
		videoOperation = snapshot.Operation
		expectedOperation := "generate_prompts"
		if original.Run.RunType == commercepkg.RunTypeShotVideos {
			expectedOperation = "generate_videos"
		}
		if videoOperation != expectedOperation {
			s.writeError(w, r, commercepkg.Error{Code: commercepkg.CodeRunStateConflict, Message: "原视频批次类型与输入快照不一致"})
			return
		}
		videoReq = commerceVideoBatchRequest{
			PlanID:                   snapshot.PlanID,
			ExpectedPlanRevision:     snapshot.PlanRevision,
			ExpectedUnitGenerationID: original.Run.Identity.UnitGenerationID,
			ShotIDs:                  shotIDs, Force: true, Concurrency: resolvedConcurrency,
			Resolution: snapshot.Resolution,
		}
		if videoReq.Concurrency == 0 {
			videoReq.Concurrency = snapshot.Concurrency
		}
		if err := normalizeCommerceVideoBatchRequest(&videoReq); err != nil {
			s.writeError(w, r, err)
			return
		}
		resolvedConcurrency = videoReq.Concurrency
	case commercepkg.RunTypeFinalCompose:
		var snapshot commerceFinalComposeSnapshot
		if err := json.Unmarshal(original.Run.InputSnapshot, &snapshot); err != nil {
			s.writeError(w, r, commercepkg.Error{Code: commercepkg.CodeRunStateConflict, Message: "原成片批次输入快照无法解析", Cause: err})
			return
		}
		finalReq = commerceFinalComposeRequest{
			TimelineID:               snapshot.TimelineID,
			ExpectedTimelineRevision: snapshot.TimelineRevision,
			ExpectedUnitGenerationID: original.Run.Identity.UnitGenerationID,
			Title:                    snapshot.Title,
			Resolution:               snapshot.Resolution,
		}
		if err := normalizeCommerceFinalComposeRequest(&finalReq); err != nil {
			s.writeError(w, r, err)
			return
		}
		resolvedConcurrency = 0
	default:
		s.writeError(w, r, commercepkg.Error{Code: commercepkg.CodeRunStateConflict, Message: "当前生产批次不支持失败项重试"})
		return
	}
	scope := "commerce_production_run_retry:" + original.Run.ID
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId": project.ID, "originalRunId": original.Run.ID,
		"runType": original.Run.RunType, "itemIds": itemIDs, "concurrency": resolvedConcurrency,
	})
	claim, err := claimIdempotencyTx(r.Context(), tx, project.OrganizationID, scope, idempotencyKey, requestHash)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if len(claim.replaySnapshot) > 0 {
		var response commercepkg.ProductionRun
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
	var run commercepkg.ProductionRun
	var created bool
	if original.Run.RunType == commercepkg.RunTypeReferenceImages {
		run, created, err = s.createCommerceReferenceImageRunTx(
			r.Context(), tx, project, principal.UserID, original.Run.Identity.ScriptUnitID,
			referenceReq, scope, idempotencyKey, original.Run.ID,
		)
	} else if original.Run.RunType == commercepkg.RunTypeFinalCompose {
		run, created, err = s.createCommerceFinalComposeRunTx(
			r.Context(), tx, project, principal.UserID, original.Run.Identity.ScriptUnitID,
			finalReq, scope, idempotencyKey, original.Run.ID,
		)
	} else {
		run, created, err = s.createCommerceVideoRunTx(
			r.Context(), tx, project, principal.UserID, original.Run.Identity.ScriptUnitID,
			videoOperation, videoReq, scope, idempotencyKey, original.Run.ID,
		)
	}
	if err != nil {
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
	httpx.WriteJSON(w, r, http.StatusAccepted, run, map[string]any{"created": created, "retryOfRunId": original.Run.ID})
}

func (s *Server) cancelCommerceProductionRun(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowCancel)
	if !ok {
		return
	}
	var req commerceCancelProductionRunRequest
	if !decode(w, r, &req) {
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "用户取消带货视频生产批次"
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	detail, err := s.commerceCatalog.GetProductionRun(r.Context(), tx, project.OrganizationID, project.ID, r.PathValue("runId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	run, err := s.commerceCatalog.CancelProductionRun(r.Context(), tx, project.OrganizationID, project.ID, detail.Run.ID, reason)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	if detail.Run.WorkflowRunID != "" {
		workflowRun, loadErr := scanWorkflowRun(s.db.QueryRow(r.Context(), workflowRunSelectSQL(`WHERE id = $1`), detail.Run.WorkflowRunID))
		if loadErr != nil {
			s.writeError(w, r, loadErr)
			return
		}
		if _, cancelErr := s.cancelWorkflowRunItem(r.Context(), workflowRun, reason); cancelErr != nil {
			s.writeError(w, r, cancelErr)
			return
		}
	}
	updated, err := s.commerceCatalog.GetProductionRun(r.Context(), s.db, project.OrganizationID, project.ID, run.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, updated, nil)
}

func selectRetryableCommerceRunItems(items []commercepkg.ProductionRunItem, requestedIDs []string) ([]commercepkg.ProductionRunItem, error) {
	requested := make(map[string]struct{}, len(requestedIDs))
	for _, itemID := range requestedIDs {
		itemID = strings.TrimSpace(itemID)
		if _, err := uuid.Parse(itemID); err != nil {
			return nil, commercepkg.Error{Code: commercepkg.CodeRunStateConflict, Message: "重试项标识无效"}
		}
		requested[itemID] = struct{}{}
	}
	selected := make([]commercepkg.ProductionRunItem, 0)
	for _, item := range items {
		if len(requested) > 0 {
			if _, ok := requested[item.ID]; !ok {
				continue
			}
			delete(requested, item.ID)
		}
		if item.Status != commercepkg.ItemFailedRetryable && item.Status != commercepkg.ItemFailedTerminal && item.Status != commercepkg.ItemDiscarded {
			if len(requestedIDs) > 0 {
				return nil, commercepkg.Error{Code: commercepkg.CodeRunStateConflict, Message: "所选生产项不是失败状态"}
			}
			continue
		}
		selected = append(selected, item)
	}
	if len(requested) > 0 {
		return nil, commercepkg.Error{Code: commercepkg.CodeRunStateConflict, Message: "部分重试项不属于原批次"}
	}
	if len(selected) == 0 {
		return nil, commercepkg.Error{Code: commercepkg.CodeRunStateConflict, Message: "原批次没有可重试的失败项"}
	}
	return selected, nil
}

func normalizeCommerceReferenceImageBatchRequest(req *commerceReferenceImageBatchRequest) error {
	req.Operation = strings.TrimSpace(req.Operation)
	req.PlanID = strings.TrimSpace(req.PlanID)
	req.ExpectedUnitGenerationID = strings.TrimSpace(req.ExpectedUnitGenerationID)
	if req.Operation != "generate_prompts" && req.Operation != "generate_images" {
		return commercepkg.Error{Code: commercepkg.CodeStoryboardInvalid, Message: "参考图批次操作无效"}
	}
	if _, err := uuid.Parse(req.PlanID); err != nil {
		return commercepkg.Error{Code: commercepkg.CodeStoryboardInvalid, Message: "分镜方案标识无效"}
	}
	if _, err := uuid.Parse(req.ExpectedUnitGenerationID); err != nil {
		return commercepkg.Error{Code: commercepkg.CodeGenerationMismatch, Message: "脚本单元生产代标识无效"}
	}
	if req.ExpectedPlanRevision < 1 {
		return commercepkg.Error{Code: commercepkg.CodeStoryboardRevision, Message: "分镜方案版本无效"}
	}
	if len(req.ShotIDs) == 0 {
		return commercepkg.Error{Code: commercepkg.CodeStoryboardInvalid, Message: "必须明确选择至少一个镜头"}
	}
	seen := make(map[string]struct{}, len(req.ShotIDs))
	for index := range req.ShotIDs {
		req.ShotIDs[index] = strings.TrimSpace(req.ShotIDs[index])
		if _, err := uuid.Parse(req.ShotIDs[index]); err != nil {
			return commercepkg.Error{Code: commercepkg.CodeStoryboardInvalid, Message: "镜头标识无效"}
		}
		if _, exists := seen[req.ShotIDs[index]]; exists {
			return commercepkg.Error{Code: commercepkg.CodeStoryboardInvalid, Message: "镜头列表包含重复项"}
		}
		seen[req.ShotIDs[index]] = struct{}{}
	}
	sort.Strings(req.ShotIDs)
	if req.Concurrency == 0 {
		req.Concurrency = 5
	}
	if req.Concurrency < 1 || req.Concurrency > 16 {
		return commercepkg.Error{Code: commercepkg.CodeStoryboardInvalid, Message: "并发数必须在 1 到 16 之间"}
	}
	return nil
}

func normalizeCommerceVideoBatchRequest(req *commerceVideoBatchRequest) error {
	req.PlanID = strings.TrimSpace(req.PlanID)
	req.ExpectedUnitGenerationID = strings.TrimSpace(req.ExpectedUnitGenerationID)
	req.Resolution = strings.ToLower(strings.TrimSpace(req.Resolution))
	if _, err := uuid.Parse(req.PlanID); err != nil {
		return commercepkg.Error{Code: commercepkg.CodeStoryboardInvalid, Message: "分镜方案标识无效"}
	}
	if _, err := uuid.Parse(req.ExpectedUnitGenerationID); err != nil {
		return commercepkg.Error{Code: commercepkg.CodeGenerationMismatch, Message: "脚本单元生产代标识无效"}
	}
	if req.ExpectedPlanRevision < 1 {
		return commercepkg.Error{Code: commercepkg.CodeStoryboardRevision, Message: "分镜方案版本无效"}
	}
	if len(req.ShotIDs) == 0 {
		return commercepkg.Error{Code: commercepkg.CodeStoryboardInvalid, Message: "必须明确选择至少一个镜头"}
	}
	seen := make(map[string]struct{}, len(req.ShotIDs))
	for index := range req.ShotIDs {
		req.ShotIDs[index] = strings.TrimSpace(req.ShotIDs[index])
		if _, err := uuid.Parse(req.ShotIDs[index]); err != nil {
			return commercepkg.Error{Code: commercepkg.CodeStoryboardInvalid, Message: "镜头标识无效"}
		}
		if _, exists := seen[req.ShotIDs[index]]; exists {
			return commercepkg.Error{Code: commercepkg.CodeStoryboardInvalid, Message: "镜头列表包含重复项"}
		}
		seen[req.ShotIDs[index]] = struct{}{}
	}
	sort.Strings(req.ShotIDs)
	if req.Concurrency == 0 {
		req.Concurrency = 5
	}
	if req.Concurrency < 1 || req.Concurrency > 16 {
		return commercepkg.Error{Code: commercepkg.CodeStoryboardInvalid, Message: "并发数必须在 1 到 16 之间"}
	}
	if req.Resolution == "" {
		req.Resolution = "1080p"
	}
	return nil
}

func validateCommerceVideoSelection(
	ctx context.Context,
	tx pgx.Tx,
	detail commercepkg.StoryboardPlanDetail,
	identity commercepkg.UnitGenerationIdentity,
	operation string,
	req commerceVideoBatchRequest,
) ([]string, error) {
	if operation != "generate_prompts" && operation != "generate_videos" {
		return nil, commercepkg.Error{Code: commercepkg.CodeStoryboardInvalid, Message: "视频生产操作无效"}
	}
	if !detail.Plan.Active || detail.Plan.Status != "ready" || detail.Plan.StaleState != "fresh" {
		return nil, commercepkg.Error{Code: commercepkg.CodeStoryboardPlanStale, Message: "当前分镜方案未启用或已过期"}
	}
	if detail.Plan.UnitGenerationID != identity.UnitGenerationID ||
		detail.Plan.ProjectGenerationID != identity.ProjectGenerationID ||
		detail.Plan.CommerceWorkflowBindingID != identity.CommerceWorkflowBindingID ||
		detail.Plan.CommerceBindingRevision != identity.CommerceWorkflowBindingRevision {
		return nil, commercepkg.Error{Code: commercepkg.CodeGenerationMismatch, Message: "分镜方案与当前脚本单元生产代不一致"}
	}
	if detail.Plan.EditRevision != req.ExpectedPlanRevision {
		return nil, commercepkg.Error{
			Code:    commercepkg.CodeStoryboardRevision,
			Message: "分镜方案已被修改，请刷新后重试",
			Details: map[string]any{"currentRevision": detail.Plan.EditRevision},
		}
	}
	requested := make(map[string]struct{}, len(req.ShotIDs))
	for _, shotID := range req.ShotIDs {
		requested[shotID] = struct{}{}
	}
	for _, shot := range detail.Shots {
		delete(requested, shot.ID)
	}
	if len(requested) > 0 {
		missing := make([]string, 0, len(requested))
		for shotID := range requested {
			missing = append(missing, shotID)
		}
		sort.Strings(missing)
		return nil, commercepkg.Error{
			Code:    commercepkg.CodeStoryboardShotRequired,
			Message: "部分镜头不属于当前分镜方案",
			Details: map[string]any{"shotIds": missing},
		}
	}
	rows, err := tx.Query(ctx, `
		SELECT shot.id::text,
		       EXISTS (
		         SELECT 1 FROM commerce_shot_image_versions image
		         WHERE image.id = shot.active_commerce_image_version_id
		           AND image.storyboard_shot_id = shot.id
		           AND image.active AND image.status = 'succeeded'
		           AND image.fidelity_status = 'approved'
		       ) AS image_ready,
		       EXISTS (
		         SELECT 1
		         FROM video_prompt_plans prompt
		         JOIN storyboard_shot_state_versions state
		           ON state.storyboard_shot_id = shot.id
		          AND state.state_role = 'planned_entry' AND state.status = 'approved'
		         JOIN shot_reference_packs reference_pack
		           ON reference_pack.storyboard_shot_id = shot.id
		          AND reference_pack.status = 'active' AND reference_pack.purpose = 'video'
		         JOIN prompt_context_plans context_plan
		           ON context_plan.id = prompt.prompt_context_plan_id AND context_plan.status = 'active'
		         JOIN video_native_audio_contracts audio
		           ON audio.video_prompt_plan_id = prompt.id AND audio.status = 'active'
		         WHERE prompt.storyboard_shot_id = shot.id
		           AND prompt.status = 'approved'
		           AND prompt.production_generation_id = $4
		           AND prompt.commerce_script_unit_id = $2
		           AND prompt.commerce_script_unit_generation_id = $3
		           AND prompt.shot_state_hash = state.state_hash
		           AND prompt.reference_pack_hash = reference_pack.manifest_hash
		           AND prompt.prompt_context_plan_hash = context_plan.plan_hash
		       ) AS prompt_ready
		FROM storyboard_shots shot
		WHERE shot.organization_id = $1 AND shot.project_id = $5
		  AND shot.commerce_storyboard_plan_id = $6
		  AND shot.deleted_at IS NULL
		  AND shot.id = ANY($7::text[]::uuid[])
		ORDER BY shot.shot_index, shot.id
	`, identity.OrganizationID, identity.ScriptUnitID, identity.UnitGenerationID,
		identity.ProjectGenerationID, identity.ProjectID, req.PlanID, req.ShotIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	selected := make([]string, 0, len(req.ShotIDs))
	missingImages := make([]string, 0)
	missingPrompts := make([]string, 0)
	for rows.Next() {
		var shotID string
		var imageReady, promptReady bool
		if err := rows.Scan(&shotID, &imageReady, &promptReady); err != nil {
			return nil, err
		}
		selected = append(selected, shotID)
		if !imageReady {
			missingImages = append(missingImages, shotID)
		}
		if operation == "generate_videos" && !promptReady {
			missingPrompts = append(missingPrompts, shotID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(selected) != len(req.ShotIDs) {
		return nil, commercepkg.Error{Code: commercepkg.CodeStoryboardShotRequired, Message: "部分镜头已不属于当前分镜方案"}
	}
	if len(missingImages) > 0 {
		return nil, commercepkg.Error{
			Code:    commercepkg.CodeImagePromptRequired,
			Message: "部分镜头缺少已通过商品保真审核的首帧参考图",
			Details: map[string]any{"shotIds": missingImages},
		}
	}
	if len(missingPrompts) > 0 {
		return nil, commercepkg.Error{
			Code:    workflows.CommerceCodeVideoPromptContractInvalid,
			Message: "部分镜头缺少匹配当前分镜与首帧的已审核视频提示词",
			Details: map[string]any{"shotIds": missingPrompts},
		}
	}
	return selected, nil
}

func validateCommerceReferenceImageSelection(
	detail commercepkg.StoryboardPlanDetail,
	identity commercepkg.UnitGenerationIdentity,
	req commerceReferenceImageBatchRequest,
) ([]string, error) {
	if !detail.Plan.Active || detail.Plan.Status != "ready" || detail.Plan.StaleState != "fresh" {
		return nil, commercepkg.Error{Code: commercepkg.CodeStoryboardPlanStale, Message: "当前分镜方案未启用或已过期"}
	}
	if detail.Plan.UnitGenerationID != identity.UnitGenerationID || detail.Plan.ProjectGenerationID != identity.ProjectGenerationID ||
		detail.Plan.CommerceWorkflowBindingID != identity.CommerceWorkflowBindingID ||
		detail.Plan.CommerceBindingRevision != identity.CommerceWorkflowBindingRevision {
		return nil, commercepkg.Error{Code: commercepkg.CodeGenerationMismatch, Message: "分镜方案与当前脚本单元生产代不一致"}
	}
	if detail.Plan.EditRevision != req.ExpectedPlanRevision {
		return nil, commercepkg.Error{
			Code: commercepkg.CodeStoryboardRevision, Message: "分镜方案已被修改，请刷新后重试",
			Details: map[string]any{"currentRevision": detail.Plan.EditRevision},
		}
	}
	requested := make(map[string]struct{}, len(req.ShotIDs))
	for _, shotID := range req.ShotIDs {
		requested[shotID] = struct{}{}
	}
	selected := make([]string, 0, len(requested))
	missingPrompts := make([]string, 0)
	for _, shot := range detail.Shots {
		if _, exists := requested[shot.ID]; !exists {
			continue
		}
		if len(shot.ProductReferences) == 0 {
			return nil, commercepkg.Error{
				Code: commercepkg.CodeStoryboardInvalid, Message: "所选镜头缺少商品参考图",
				Details: map[string]any{"shotId": shot.ID},
			}
		}
		if req.Operation == "generate_images" && shot.ImagePromptStatus != "succeeded" {
			missingPrompts = append(missingPrompts, shot.ID)
		}
		selected = append(selected, shot.ID)
		delete(requested, shot.ID)
	}
	if len(requested) > 0 {
		missing := make([]string, 0, len(requested))
		for shotID := range requested {
			missing = append(missing, shotID)
		}
		sort.Strings(missing)
		return nil, commercepkg.Error{
			Code: commercepkg.CodeStoryboardShotRequired, Message: "部分镜头不属于当前分镜方案",
			Details: map[string]any{"shotIds": missing},
		}
	}
	if len(missingPrompts) > 0 {
		return nil, commercepkg.Error{
			Code: commercepkg.CodeImagePromptRequired, Message: "部分镜头缺少当前版本的已审核图片提示词",
			Details: map[string]any{"shotIds": missingPrompts},
		}
	}
	return selected, nil
}

func commerceReferenceImageWorkflowType(operation string) string {
	if operation == "generate_prompts" {
		return "commerce_reference_image_prompts"
	}
	return "commerce_reference_images"
}
