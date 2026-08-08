package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/jackc/pgx/v5"
)

const maxAssetBatchItems = 500

// snapshotQuerier is the only database surface snapshot builders may use.
// Passing a pgx.Tx here prevents a resolver from silently escaping the
// caller's repeatable-read snapshot through the server connection pool.
type snapshotQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type createAssetBatchRequest struct {
	Operation               string   `json:"operation"`
	AssetIDs                []string `json:"assetIds"`
	MaxConcurrency          int      `json:"maxConcurrency,omitempty"`
	Force                   bool     `json:"force,omitempty"`
	ExpectedProjectRevision int64    `json:"expectedProjectRevision"`
	IdempotencyKey          string   `json:"idempotencyKey,omitempty"`
}

type retryFailedWorkflowRequest struct {
	MaxConcurrency          int    `json:"maxConcurrency,omitempty"`
	Force                   *bool  `json:"force,omitempty"`
	ExpectedProjectRevision int64  `json:"expectedProjectRevision"`
	IdempotencyKey          string `json:"idempotencyKey,omitempty"`
}

func (s *Server) createAssetBatch(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetGenerate)
	if !ok {
		return
	}
	var req createAssetBatchRequest
	if !decode(w, r, &req) {
		return
	}
	run, started, err := s.createAssetBatchRun(w, r, principal, project, req, "", "", 1, "asset-batches:create")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !started {
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, run, nil)
}

func (s *Server) retryFailedWorkflowRun(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	original, err := scanWorkflowRun(s.db.QueryRow(r.Context(), workflowRunSelectSQL(`WHERE id = $1`), r.PathValue("workflowRunId")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !isTerminalWorkflowStatus(original.Status) {
		httpx.WriteError(w, r, http.StatusConflict, "WORKFLOW_NOT_TERMINAL", "workflow must reach a terminal state before retry", nil, true)
		return
	}
	var req retryFailedWorkflowRequest
	if !decode(w, r, &req) {
		return
	}
	var run WorkflowRun
	var started bool
	switch original.WorkflowType {
	case "batch_generate_asset_cards", "batch_generate_asset_images":
		if !s.authorize(w, r, principal, authz.PermissionAssetGenerate, authz.Resource{ProjectID: original.ProjectID}) {
			return
		}
		run, started, err = s.retryFailedAssetBatch(r, w, principal, original, req)
	case "source_to_script":
		if !s.authorize(w, r, principal, authz.PermissionWorkflowRun, authz.Resource{ProjectID: original.ProjectID}) {
			return
		}
		run, started, err = s.retryFailedSourceToScript(r, w, principal, original, req)
	case derivedAssetBatchWorkflowType:
		if !s.authorize(w, r, principal, authz.PermissionAssetGenerate, authz.Resource{ProjectID: original.ProjectID}) {
			return
		}
		run, started, err = s.retryFailedDerivedAssetBatch(r, w, principal, original, req)
	default:
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "WORKFLOW_RETRY_UNSUPPORTED", "this workflow type does not support failed-item retry", nil, false)
		return
	}
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !started {
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, run, nil)
}

func (s *Server) retryFailedAssetBatch(r *http.Request, w http.ResponseWriter, principal auth.Principal, original WorkflowRun, req retryFailedWorkflowRequest) (WorkflowRun, bool, error) {
	project, err := s.project(r, original.ProjectID)
	if err != nil {
		return WorkflowRun{}, false, err
	}
	var originalInput workflows.AssetBatchWorkflowInput
	if err := json.Unmarshal(original.Input, &originalInput); err != nil {
		return WorkflowRun{}, false, newAPIError(http.StatusConflict, "WORKFLOW_INPUT_INVALID", "asset batch workflow input cannot be replayed")
	}
	failedAssetIDs, err := s.failedAssetBatchIDs(r.Context(), original.ID)
	if err != nil {
		return WorkflowRun{}, false, err
	}
	if len(failedAssetIDs) == 0 {
		return WorkflowRun{}, false, newAPIError(http.StatusConflict, "NO_FAILED_ITEMS", "workflow has no failed asset items to retry")
	}
	force := originalInput.Force
	if req.Force != nil {
		force = *req.Force
	}
	maxConcurrency := req.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = originalInput.MaxConcurrency
	}
	rootID := original.ID
	if original.RootWorkflowRunID != nil && strings.TrimSpace(*original.RootWorkflowRunID) != "" {
		rootID = *original.RootWorkflowRunID
	}
	attemptGeneration := max(original.AttemptGeneration, originalInput.AttemptGeneration) + 1
	if attemptGeneration <= 1 {
		attemptGeneration = 2
	}
	operation := workflows.AssetBatchOperationGeneratePrompts
	if original.WorkflowType == "batch_generate_asset_images" {
		operation = workflows.AssetBatchOperationGenerateImages
	}
	return s.createAssetBatchRun(w, r, principal, project, createAssetBatchRequest{
		Operation: operation, AssetIDs: failedAssetIDs, MaxConcurrency: maxConcurrency, Force: force,
		ExpectedProjectRevision: req.ExpectedProjectRevision, IdempotencyKey: req.IdempotencyKey,
	}, rootID, original.ID, attemptGeneration, "asset-batches:retry:"+original.ID)
}

func (s *Server) retryFailedSourceToScript(r *http.Request, w http.ResponseWriter, principal auth.Principal, original WorkflowRun, req retryFailedWorkflowRequest) (WorkflowRun, bool, error) {
	if req.ExpectedProjectRevision <= 0 {
		return WorkflowRun{}, false, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "expectedProjectRevision is required")
	}
	project, err := s.project(r, original.ProjectID)
	if err != nil {
		return WorkflowRun{}, false, err
	}
	rootID := original.ID
	if original.RootWorkflowRunID != nil && strings.TrimSpace(*original.RootWorkflowRunID) != "" {
		rootID = strings.TrimSpace(*original.RootWorkflowRunID)
	}
	plan, err := s.loadSourceToScriptRetryPlan(r.Context(), rootID, original.ID)
	if err != nil {
		return WorkflowRun{}, false, err
	}
	failedChapterIDs, failedCount, err := s.failedSourceToScriptChapterIDs(r.Context(), original.ID)
	if err != nil {
		return WorkflowRun{}, false, err
	}
	if failedCount == 0 {
		return WorkflowRun{}, false, newAPIError(http.StatusConflict, "NO_FAILED_ITEMS", "workflow has no failed script episodes to retry")
	}
	prompt, optionsRaw, err := sourceToScriptRetryOptions(original.Input)
	if err != nil {
		return WorkflowRun{}, false, err
	}
	var options workflows.SourceToScriptOptions
	if err := json.Unmarshal(optionsRaw, &options); err != nil {
		return WorkflowRun{}, false, newAPIError(http.StatusConflict, "WORKFLOW_INPUT_INVALID", "source-to-script workflow options cannot be replayed")
	}
	if strings.TrimSpace(options.SourceID) == "" {
		options.SourceID = plan.SourceID
	}
	if options.SourceID != plan.SourceID {
		return WorkflowRun{}, false, newAPIError(http.StatusConflict, "WORKFLOW_INPUT_INVALID", "source-to-script plan does not match the workflow input")
	}
	if plan.SourceType == "novel" {
		if len(failedChapterIDs) != failedCount {
			return WorkflowRun{}, false, newAPIError(http.StatusConflict, "WORKFLOW_INPUT_INVALID", "failed novel script results are missing source chapter identity")
		}
		options.ChapterIDs = failedChapterIDs
	} else {
		options.ChapterIDs = nil
	}
	options.TargetScriptID = plan.ScriptID
	options.CreateNewScript = false
	if req.MaxConcurrency > 0 {
		options.MaxConcurrency = req.MaxConcurrency
	}
	options.MaxConcurrency = workflows.NormalizeSourceToScriptConcurrency(options.MaxConcurrency)
	optionsRaw = mustRawJSON(options)
	attemptGeneration := original.AttemptGeneration + 1
	if attemptGeneration <= 1 {
		attemptGeneration = 2
	}
	state := workflows.SourceToScriptWorkflowState{AttemptGeneration: attemptGeneration}
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId": project.ID, "workflowType": original.WorkflowType, "retryOfWorkflowRunId": original.ID,
		"rootWorkflowRunId": rootID, "attemptGeneration": attemptGeneration,
		"sourceChapterIds": failedChapterIDs, "options": json.RawMessage(optionsRaw),
		"expectedProjectRevision": req.ExpectedProjectRevision,
	})
	idempotencyScope := "workflow-runs:retry-failed:" + original.ID
	tx, err := s.db.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return WorkflowRun{}, false, err
	}
	defer tx.Rollback(r.Context())
	lockedProject, err := scanProject(tx.QueryRow(r.Context(), projectSelectSQL(`WHERE p.id = $1 FOR UPDATE OF p`), project.ID))
	if err != nil {
		return WorkflowRun{}, false, err
	}
	if lockedProject.OrganizationID != project.OrganizationID {
		return WorkflowRun{}, false, newAPIError(http.StatusNotFound, "NOT_FOUND", "project was not found")
	}
	if req.ExpectedProjectRevision != lockedProject.Revision {
		conflict := newAPIError(http.StatusConflict, "PROJECT_REVISION_CONFLICT", "project settings changed before the retry was created")
		conflict.Details = map[string]any{"expectedRevision": req.ExpectedProjectRevision, "currentRevision": lockedProject.Revision}
		return WorkflowRun{}, false, conflict
	}
	claim, err := claimIdempotencyTx(
		r.Context(), tx, lockedProject.OrganizationID, idempotencyScope,
		idempotencyKey(r, req.IdempotencyKey), requestHash,
	)
	if err != nil {
		return WorkflowRun{}, false, err
	}
	if len(claim.replaySnapshot) > 0 {
		var replay WorkflowRun
		if err := json.Unmarshal(claim.replaySnapshot, &replay); err != nil {
			return WorkflowRun{}, false, err
		}
		_ = tx.Rollback(r.Context())
		status := claim.replayStatus
		if status < 200 || status > 299 {
			status = http.StatusOK
		}
		if w != nil {
			httpx.WriteJSON(w, r, status, replay, map[string]any{"idempotentReplay": true, "operationId": claim.state.operationID})
		}
		return replay, false, nil
	}
	operationID, err := ensureRuntimeOperationTx(
		r.Context(), tx, &claim, lockedProject.OrganizationID, lockedProject.ID, idempotencyScope, requestHash,
	)
	if err != nil {
		return WorkflowRun{}, false, err
	}
	runInput := mustRawJSON(map[string]any{
		"prompt": prompt, "workflowType": "source_to_script", "input": json.RawMessage(optionsRaw),
		"retry": map[string]any{"rootWorkflowRunId": rootID, "retryOfWorkflowRunId": original.ID, "attemptGeneration": attemptGeneration, "sourceChapterIds": failedChapterIDs},
	})
	run, err := s.enqueueProjectWorkflowTx(
		r.Context(), tx, principal, lockedProject, "source_to_script", runInput, workflows.ScriptTaskQueue, workflows.SourceToScriptWorkflow,
		func(run WorkflowRun) any {
			startState := state
			return workflows.TextToStoryboardInput{
				OrganizationID: run.OrganizationID, ProjectID: run.ProjectID, WorkflowRunID: run.ID,
				Prompt: prompt, CreatedBy: principal.UserID, Input: optionsRaw, SourceToScriptState: &startState,
			}
		},
		func(ctx context.Context, tx pgx.Tx, run WorkflowRun) error {
			if _, err := tx.Exec(ctx, `
				UPDATE workflow_runs
				SET total_items = $2, root_workflow_run_id = $3, retry_of_workflow_run_id = $4,
				    attempt_generation = $5, revision = revision + 1, updated_at = now()
				WHERE id = $1
			`, run.ID, failedCount, rootID, original.ID, attemptGeneration); err != nil {
				return err
			}
			startState := state
			startInput := workflows.TextToStoryboardInput{
				OrganizationID: run.OrganizationID, ProjectID: run.ProjectID, WorkflowRunID: run.ID,
				Prompt: prompt, CreatedBy: principal.UserID, Input: optionsRaw, SourceToScriptState: &startState,
			}
			snapshotRaw, snapshotHash, err := marshalWorkflowStartInput(startInput)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO workflow_input_snapshots(
					workflow_run_id, organization_id, project_id, project_revision, snapshot, snapshot_hash,
					production_generation_id
				)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
			`, run.ID, run.OrganizationID, run.ProjectID, lockedProject.Revision, snapshotRaw, snapshotHash,
				run.ProductionGenerationID); err != nil {
				return err
			}
			updated, err := scanWorkflowRun(tx.QueryRow(ctx, workflowRunSelectSQL(`WHERE id = $1`), run.ID))
			if err != nil {
				return err
			}
			if _, err := completeRuntimeOperationTx(ctx, tx, operationID, run.ID, updated); err != nil {
				return err
			}
			return completeIdempotencyTxWithStatus(ctx, tx, claim.state, http.StatusAccepted, updated)
		},
	)
	if err != nil {
		return WorkflowRun{}, false, err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return WorkflowRun{}, false, err
	}
	updated, err := scanWorkflowRun(s.db.QueryRow(r.Context(), workflowRunSelectSQL(`WHERE id = $1`), run.ID))
	return updated, err == nil, err
}

func (s *Server) loadSourceToScriptRetryPlan(ctx context.Context, workflowRunIDs ...string) (workflows.SourceToScriptPlan, error) {
	seen := map[string]bool{}
	for _, workflowRunID := range workflowRunIDs {
		workflowRunID = strings.TrimSpace(workflowRunID)
		if workflowRunID == "" || seen[workflowRunID] {
			continue
		}
		seen[workflowRunID] = true
		var raw []byte
		err := s.db.QueryRow(ctx, `
			SELECT output
			FROM workflow_node_runs
			WHERE workflow_run_id = $1 AND node_key = $2 AND status = 'succeeded'
			ORDER BY completed_at DESC NULLS LAST
			LIMIT 1
		`, workflowRunID, workflows.SourceToScriptPrepareNodeKey).Scan(&raw)
		if err == pgx.ErrNoRows {
			continue
		}
		if err != nil {
			return workflows.SourceToScriptPlan{}, err
		}
		var plan workflows.SourceToScriptPlan
		if err := json.Unmarshal(raw, &plan); err != nil {
			return workflows.SourceToScriptPlan{}, newAPIError(http.StatusConflict, "WORKFLOW_INPUT_INVALID", "source-to-script plan cannot be replayed")
		}
		if strings.TrimSpace(plan.GenerationID) == "" || strings.TrimSpace(plan.SourceID) == "" || strings.TrimSpace(plan.ScriptID) == "" || plan.EpisodeTotal <= 0 {
			return workflows.SourceToScriptPlan{}, newAPIError(http.StatusConflict, "WORKFLOW_INPUT_INVALID", "source-to-script plan is incomplete")
		}
		if len(plan.Chapters) > 0 && len(plan.Chapters) != plan.EpisodeTotal {
			return workflows.SourceToScriptPlan{}, newAPIError(http.StatusConflict, "WORKFLOW_INPUT_INVALID", "source-to-script plan chapter count is inconsistent")
		}
		return plan, nil
	}
	return workflows.SourceToScriptPlan{}, newAPIError(http.StatusConflict, "WORKFLOW_INPUT_INVALID", "source-to-script prepare plan was not found")
}

func (s *Server) failedSourceToScriptChapterIDs(ctx context.Context, workflowRunID string) ([]string, int, error) {
	rows, err := s.db.Query(ctx, `
		SELECT COALESCE(source_chapter_id::text, '')
		FROM script_episode_generation_results
		WHERE workflow_run_id = $1 AND status = 'failed'
		ORDER BY completed_at, item_key
	`, workflowRunID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	chapterIDs := make([]string, 0)
	failedCount := 0
	for rows.Next() {
		var chapterID string
		if err := rows.Scan(&chapterID); err != nil {
			return nil, 0, err
		}
		failedCount++
		if chapterID != "" && !seen[chapterID] {
			seen[chapterID] = true
			chapterIDs = append(chapterIDs, chapterID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return chapterIDs, failedCount, nil
}

func sourceToScriptRetryOptions(raw json.RawMessage) (string, json.RawMessage, error) {
	var envelope struct {
		Prompt string          `json:"prompt"`
		Input  json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && len(envelope.Input) > 0 && string(envelope.Input) != "null" {
		return strings.TrimSpace(envelope.Prompt), envelope.Input, nil
	}
	var direct workflows.TextToStoryboardInput
	if err := json.Unmarshal(raw, &direct); err == nil && len(direct.Input) > 0 && string(direct.Input) != "null" {
		return strings.TrimSpace(direct.Prompt), direct.Input, nil
	}
	return "", nil, newAPIError(http.StatusConflict, "WORKFLOW_INPUT_INVALID", "source-to-script workflow input cannot be replayed")
}

func (s *Server) createAssetBatchRun(
	w http.ResponseWriter,
	r *http.Request,
	principal auth.Principal,
	project Project,
	req createAssetBatchRequest,
	rootWorkflowRunID string,
	retryOfWorkflowRunID string,
	attemptGeneration int,
	idempotencyScope string,
) (WorkflowRun, bool, error) {
	req.Operation = strings.TrimSpace(req.Operation)
	if req.Operation != workflows.AssetBatchOperationGeneratePrompts && req.Operation != workflows.AssetBatchOperationGenerateImages {
		return WorkflowRun{}, false, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "asset batch operation is invalid")
	}
	req.AssetIDs = uniqueNonEmptyStrings(req.AssetIDs)
	if len(req.AssetIDs) == 0 || len(req.AssetIDs) > maxAssetBatchItems {
		return WorkflowRun{}, false, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "assetIds must contain between 1 and 500 unique values")
	}
	if req.MaxConcurrency <= 0 {
		req.MaxConcurrency = workflows.DefaultAssetBatchConcurrency
	}
	if req.MaxConcurrency > workflows.MaxAssetBatchConcurrency {
		req.MaxConcurrency = workflows.MaxAssetBatchConcurrency
	}
	if req.ExpectedProjectRevision <= 0 {
		return WorkflowRun{}, false, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "expectedProjectRevision is required")
	}
	workflowType := "batch_generate_asset_cards"
	workflowFunc := any(workflows.BatchGenerateAssetCardsWorkflow)
	if req.Operation == workflows.AssetBatchOperationGenerateImages {
		workflowType = "batch_generate_asset_images"
		workflowFunc = workflows.BatchGenerateCanonicalAssetImagesWorkflow
	}
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId": project.ID, "operation": req.Operation, "assetIds": req.AssetIDs,
		"maxConcurrency": req.MaxConcurrency, "force": req.Force,
		"expectedProjectRevision": req.ExpectedProjectRevision,
		"rootWorkflowRunId":       rootWorkflowRunID, "retryOfWorkflowRunId": retryOfWorkflowRunID,
	})
	tx, err := s.db.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return WorkflowRun{}, false, err
	}
	defer tx.Rollback(r.Context())

	lockedProject, err := scanProject(tx.QueryRow(r.Context(), projectSelectSQL(`WHERE p.id = $1 FOR UPDATE OF p`), project.ID))
	if err != nil {
		return WorkflowRun{}, false, err
	}
	if lockedProject.OrganizationID != project.OrganizationID {
		return WorkflowRun{}, false, newAPIError(http.StatusNotFound, "NOT_FOUND", "project was not found")
	}
	productionContext, err := videoproduction.LoadWritableContextTx(r.Context(), tx, lockedProject.ID, false)
	if err != nil {
		return WorkflowRun{}, false, err
	}
	lockedProject.VideoProductionBinding = &productionContext.Binding
	lockedProject.ProductionGeneration = &productionContext.Generation
	lockedProject.VideoProductionState = productionContext.State
	lockedProject.VideoProductionLocked = productionContext.Locked
	if req.ExpectedProjectRevision != lockedProject.Revision {
		conflict := newAPIError(http.StatusConflict, "PROJECT_REVISION_CONFLICT", "project settings changed before the batch was created")
		conflict.Details = map[string]any{"expectedRevision": req.ExpectedProjectRevision, "currentRevision": lockedProject.Revision}
		return WorkflowRun{}, false, conflict
	}
	if s.assetBatchSnapshotLockedHook != nil {
		s.assetBatchSnapshotLockedHook()
	}

	claim, err := claimIdempotencyTx(
		r.Context(), tx, lockedProject.OrganizationID, idempotencyScope,
		idempotencyKey(r, req.IdempotencyKey), requestHash,
	)
	if err != nil {
		return WorkflowRun{}, false, err
	}
	if len(claim.replaySnapshot) > 0 {
		var replay WorkflowRun
		if err := json.Unmarshal(claim.replaySnapshot, &replay); err != nil {
			return WorkflowRun{}, false, err
		}
		_ = tx.Rollback(r.Context())
		if w != nil {
			status := claim.replayStatus
			if status < 200 || status > 299 {
				status = http.StatusOK
			}
			httpx.WriteJSON(w, r, status, replay, map[string]any{"idempotentReplay": true, "operationId": claim.state.operationID})
		}
		return replay, false, nil
	}

	operationID, err := ensureRuntimeOperationTx(
		r.Context(), tx, &claim, lockedProject.OrganizationID, lockedProject.ID, idempotencyScope, requestHash,
	)
	if err != nil {
		return WorkflowRun{}, false, err
	}

	snapshot, err := s.buildAssetBatchSnapshot(r.Context(), tx, lockedProject, req, attemptGeneration, retryOfWorkflowRunID)
	if err != nil {
		return WorkflowRun{}, false, err
	}
	snapshot.CreatedBy = principal.UserID
	runInput := json.RawMessage(mustMarshal(snapshot))
	run, err := s.enqueueProjectWorkflowTx(
		r.Context(), tx, principal, lockedProject, workflowType, runInput, workflows.ScriptTaskQueue, workflowFunc,
		func(run WorkflowRun) any {
			startInput := snapshot
			startInput.WorkflowRunID = run.ID
			return startInput
		},
		func(ctx context.Context, tx pgx.Tx, run WorkflowRun) error {
			startInput := snapshot
			startInput.WorkflowRunID = run.ID
			rootID := rootWorkflowRunID
			if rootID == "" {
				rootID = run.ID
			}
			if _, err := tx.Exec(ctx, `
				UPDATE workflow_runs
				SET input = $2,
				    total_items = $3,
				    root_workflow_run_id = $4,
				    retry_of_workflow_run_id = NULLIF($5, '')::uuid,
				    attempt_generation = $6,
				    revision = revision + 1,
				    updated_at = now()
				WHERE id = $1
			`, run.ID, mustRawJSON(startInput), len(startInput.Items), rootID, retryOfWorkflowRunID, startInput.AttemptGeneration); err != nil {
				return err
			}
			for _, item := range startInput.Items {
				nodeType := "asset.prompt.generate"
				if req.Operation == workflows.AssetBatchOperationGenerateImages {
					nodeType = "asset.image.generate"
				}
				if _, err := tx.Exec(ctx, `
					INSERT INTO workflow_node_runs(
						organization_id, project_id, workflow_run_id, node_key, node_type,
						status, input, output, attempt_generation, production_generation_id
					)
					VALUES ($1, $2, $3, $4, $5, 'queued', $6, '{}', $7, $8)
					ON CONFLICT (workflow_run_id, node_key) DO NOTHING
				`, lockedProject.OrganizationID, lockedProject.ID, run.ID, workflows.AssetBatchNodeKey(req.Operation, item.AssetID), nodeType,
					mustRawJSON(item), startInput.AttemptGeneration, run.ProductionGenerationID); err != nil {
					return err
				}
			}
			snapshotRaw, snapshotHash, err := marshalWorkflowStartInput(startInput)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO workflow_input_snapshots(
					workflow_run_id, organization_id, project_id, project_revision, snapshot, snapshot_hash,
					production_generation_id
				)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
			`, run.ID, lockedProject.OrganizationID, lockedProject.ID, lockedProject.Revision, snapshotRaw, snapshotHash,
				run.ProductionGenerationID); err != nil {
				return err
			}
			updated, err := scanWorkflowRun(tx.QueryRow(ctx, workflowRunSelectSQL(`WHERE id = $1`), run.ID))
			if err != nil {
				return err
			}
			if _, err := completeRuntimeOperationTx(ctx, tx, operationID, run.ID, updated); err != nil {
				return err
			}
			return completeIdempotencyTxWithStatus(ctx, tx, claim.state, http.StatusAccepted, updated)
		},
	)
	if err != nil {
		return WorkflowRun{}, false, err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return WorkflowRun{}, false, err
	}
	updated, err := scanWorkflowRun(s.db.QueryRow(r.Context(), workflowRunSelectSQL(`WHERE id = $1`), run.ID))
	return updated, err == nil, err
}

func (s *Server) buildAssetBatchSnapshot(
	ctx context.Context,
	db snapshotQuerier,
	project Project,
	req createAssetBatchRequest,
	attemptGeneration int,
	retryOfWorkflowRunID string,
) (workflows.AssetBatchWorkflowInput, error) {
	if project.VideoProductionBinding == nil || project.ProductionGeneration == nil || strings.TrimSpace(project.VideoProductionBinding.ProfileKey) == "" {
		return workflows.AssetBatchWorkflowInput{}, videoproduction.NewError(
			videoproduction.CodeGenerationMismatch,
			"项目没有可用的视频生产代，请先完成视频生产配置",
			false,
		)
	}
	promptKey := "asset_card_generation"
	if req.Operation == workflows.AssetBatchOperationGenerateImages {
		promptKey = "canonical_asset_image_prompt"
	}
	resolved, err := promptsvc.NewService(db).Resolve(ctx, promptsvc.ResolveRequest{
		OrganizationID: project.OrganizationID, ProjectID: project.ID, TemplateKey: promptKey,
	})
	if err != nil {
		return workflows.AssetBatchWorkflowInput{}, err
	}
	aspectRatio := strings.TrimSpace(project.VideoRatio)
	if aspectRatio == "" {
		aspectRatio = stringValue(project.AspectRatio)
	}
	if aspectRatio == "" {
		aspectRatio = "16:9"
	}
	snapshot := workflows.AssetBatchWorkflowInput{
		OrganizationID: project.OrganizationID, ProjectID: project.ID, CreatedBy: "",
		Operation: req.Operation, MaxConcurrency: req.MaxConcurrency, Force: req.Force,
		AttemptGeneration: attemptGeneration,
		Project: workflows.AssetBatchProjectSnapshot{
			Revision: project.Revision, AspectRatio: aspectRatio, VideoRatio: aspectRatio,
			ArtStyle: project.ArtStyle, ImageQuality: project.ImageQuality, VideoProductionProfileKey: strings.TrimSpace(project.VideoProductionBinding.ProfileKey),
			ScriptModelProfileKey: project.ScriptModelProfileKey, ImageModelProfileKey: project.ImageModelProfileKey,
			PromptTemplateKey: promptKey, PromptVersionID: resolved.VersionID,
		},
		Items: make([]workflows.AssetBatchItemSnapshot, 0, len(req.AssetIDs)),
	}
	manualBindings, err := assetBatchManualBindings(ctx, db, project.ID)
	if err != nil {
		return workflows.AssetBatchWorkflowInput{}, err
	}
	modelBindings, err := assetBatchModelBindings(ctx, db, project.OrganizationID, []string{
		project.ScriptModelProfileKey,
		project.ImageModelProfileKey,
	})
	if err != nil {
		return workflows.AssetBatchWorkflowInput{}, err
	}
	snapshot.Project.ManualBindings = manualBindings
	snapshot.Project.ModelBindings = modelBindings
	requiredProfile := project.ScriptModelProfileKey
	if req.Operation == workflows.AssetBatchOperationGenerateImages {
		requiredProfile = project.ImageModelProfileKey
	}
	if snapshot.Project.ProviderModelID(requiredProfile) == "" {
		return workflows.AssetBatchWorkflowInput{}, newAPIError(http.StatusUnprocessableEntity, "MODEL_PROFILE_NOT_CONFIGURED", "the selected model profile has no active model binding")
	}
	visualCache := map[string]workflows.AssetBatchVisualSnapshot{}
	for _, assetID := range req.AssetIDs {
		asset, err := s.canonicalAssetWithDB(ctx, db, project.ID, assetID)
		if err != nil {
			return workflows.AssetBatchWorkflowInput{}, err
		}
		if asset.Status == "archived" {
			return workflows.AssetBatchWorkflowInput{}, newAPIError(http.StatusUnprocessableEntity, "ASSET_ARCHIVED", "archived assets cannot be added to a batch")
		}
		visual, ok := visualCache[asset.AssetType]
		if !ok {
			resolvedVisual, err := s.resolveAssetCardVisualContextWithDB(ctx, db, project, "", asset.AssetType)
			if err != nil {
				return workflows.AssetBatchWorkflowInput{}, err
			}
			visual = workflows.AssetBatchVisualSnapshot{
				ManualTemplateKey: resolvedVisual.ManualTemplateKey, ManualPromptVersionID: resolvedVisual.ManualPromptVersionID,
				ManualContentHash: resolvedVisual.ManualContentHash, StyleSlug: resolvedVisual.StyleSlug, AssetType: resolvedVisual.AssetType,
				PrefixTemplateKey: resolvedVisual.PrefixTemplateKey, PrefixPromptVersionID: resolvedVisual.PrefixPromptVersionID,
				AssetTypeTemplateKey: resolvedVisual.AssetTypeTemplateKey, AssetTypePromptVersionID: resolvedVisual.AssetTypePromptVersionID,
				StylePrefix: resolvedVisual.StylePrefix, AssetTypeRules: resolvedVisual.AssetTypeRules, ManualFallback: resolvedVisual.ManualFallback,
			}
			visualCache[asset.AssetType] = visual
		}
		sceneContext, err := s.assetScenePromptContextWithDB(ctx, db, project.ID, asset.ID)
		if err != nil {
			return workflows.AssetBatchWorkflowInput{}, err
		}
		itemSnapshot := workflows.AssetBatchItemSnapshot{
			AssetID: asset.ID, AssetType: asset.AssetType, Name: asset.Name, Description: asset.Description,
			Profile: asset.Profile, BasePrompt: stringValue(asset.BasePrompt),
			ConsistencyPrompt: stringValue(asset.ConsistencyPrompt), NegativePrompt: stringValue(asset.NegativePrompt),
			VisualTraits: asset.VisualTraits, ManualOverride: asset.ManualOverride,
			Revision: asset.Revision, PromptRevision: asset.PromptRevision, SceneContext: sceneContext,
			Visual: visual, References: lockedCanonicalAssetImageReferences(asset),
		}
		if req.Operation == workflows.AssetBatchOperationGenerateImages && strings.TrimSpace(retryOfWorkflowRunID) != "" {
			recovered, err := assetBatchRecoveredImage(ctx, db, project, retryOfWorkflowRunID, asset.ID)
			if err != nil {
				return workflows.AssetBatchWorkflowInput{}, err
			}
			itemSnapshot.RecoveredImage = recovered
		}
		snapshot.Items = append(snapshot.Items, itemSnapshot)
	}
	return snapshot, nil
}

func assetBatchRecoveredImage(
	ctx context.Context,
	db snapshotQuerier,
	project Project,
	sourceWorkflowRunID, assetID string,
) (*workflows.AssetBatchRecoveredImageSnapshot, error) {
	var recovered workflows.AssetBatchRecoveredImageSnapshot
	err := db.QueryRow(ctx, `
		WITH RECURSIVE retry_chain AS (
			SELECT run.id, run.retry_of_workflow_run_id, 0 AS depth, ARRAY[run.id] AS path
			FROM workflow_runs run
			WHERE run.id = $1
			  AND run.project_id = $2
			UNION ALL
			SELECT parent.id, parent.retry_of_workflow_run_id, chain.depth + 1, chain.path || parent.id
			FROM retry_chain chain
			JOIN workflow_runs parent ON parent.id = chain.retry_of_workflow_run_id
			WHERE parent.project_id = $2
			  AND chain.depth < 32
			  AND NOT parent.id = ANY(chain.path)
		)
		SELECT node.workflow_run_id::text, node.id::text, call.id::text,
		       call.provider_model_id::text, call.prompt_hash,
		       artifact.id::text, media.id::text, artifact.storage_key
		FROM retry_chain chain
		JOIN workflow_node_runs node ON node.workflow_run_id = chain.id
		JOIN provider_call_logs call
		  ON call.node_run_id = node.id
		 AND call.status = 'succeeded'
		 AND call.task_type = 'image.generate'
		JOIN artifacts artifact
		  ON artifact.id = NULLIF(call.artifact_ids->>0, '')::uuid
		 AND artifact.organization_id = node.organization_id
		 AND artifact.project_id = node.project_id
		JOIN media_files media
		  ON media.id = NULLIF(call.media_file_ids->>0, '')::uuid
		 AND media.artifact_id = artifact.id
		 AND media.organization_id = node.organization_id
		 AND media.project_id = node.project_id
		WHERE node.project_id = $2
		  AND node.input->>'assetId' = $3
		  AND call.provider_model_id IS NOT NULL
		  AND COALESCE(call.prompt_hash, '') <> ''
		  AND artifact.production_generation_id = $4
		  AND media.production_generation_id = $4
		ORDER BY chain.depth, call.completed_at DESC, call.created_at DESC
		LIMIT 1
	`, sourceWorkflowRunID, project.ID, assetID, project.ProductionGeneration.ID).Scan(
		&recovered.SourceWorkflowRunID,
		&recovered.SourceNodeRunID,
		&recovered.ProviderCallID,
		&recovered.ProviderModelID,
		&recovered.PromptHash,
		&recovered.ArtifactID,
		&recovered.MediaFileID,
		&recovered.StorageKey,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &recovered, nil
}

func assetBatchManualBindings(ctx context.Context, db snapshotQuerier, projectID string) ([]workflows.AssetBatchManualBindingSnapshot, error) {
	rows, err := db.Query(ctx, `
		SELECT b.id::text, b.manual_kind, pt.template_key, pv.id::text, pv.content_hash
		FROM project_manual_bindings b
		JOIN prompt_versions pv ON pv.id = b.prompt_version_id
		JOIN prompt_templates pt ON pt.id = COALESCE(pv.template_id, pv.prompt_template_id)
		WHERE b.project_id = $1
		  AND b.status = 'active'
		  AND pv.status = 'active'
		ORDER BY b.manual_kind, b.updated_at DESC, b.id
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]workflows.AssetBatchManualBindingSnapshot, 0, 2)
	seen := map[string]bool{}
	for rows.Next() {
		var item workflows.AssetBatchManualBindingSnapshot
		if err := rows.Scan(&item.BindingID, &item.ManualKind, &item.TemplateKey, &item.PromptVersionID, &item.ContentHash); err != nil {
			return nil, err
		}
		if seen[item.ManualKind] {
			continue
		}
		seen[item.ManualKind] = true
		items = append(items, item)
	}
	return items, rows.Err()
}

func assetBatchModelBindings(ctx context.Context, db snapshotQuerier, organizationID string, profileKeys []string) ([]workflows.AssetBatchModelBindingSnapshot, error) {
	profileKeys = uniqueNonEmptyStrings(profileKeys)
	if len(profileKeys) == 0 {
		return nil, nil
	}
	rows, err := db.Query(ctx, `
		SELECT b.id::text, p.id::text, p.profile_key, m.id::text, m.model_key,
		       m.modality, b.priority, b.weight, m.updated_at
		FROM model_profiles p
		JOIN model_profile_bindings b ON b.model_profile_id = p.id
		JOIN provider_models m ON m.id = b.provider_model_id
		JOIN provider_accounts a ON a.id = m.provider_account_id
		WHERE p.organization_id = $1
		  AND p.profile_key = ANY($2::text[])
		  AND b.enabled = true
		  AND m.status = 'active'
		  AND a.status = 'active'
		ORDER BY p.profile_key, b.priority ASC, b.weight DESC, b.created_at, b.id
	`, organizationID, profileKeys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]workflows.AssetBatchModelBindingSnapshot, 0)
	for rows.Next() {
		var item workflows.AssetBatchModelBindingSnapshot
		var updatedAt time.Time
		if err := rows.Scan(
			&item.BindingID, &item.ProfileID, &item.ProfileKey, &item.ProviderModelID,
			&item.ModelKey, &item.Modality, &item.Priority, &item.Weight, &updatedAt,
		); err != nil {
			return nil, err
		}
		item.ModelUpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) failedAssetBatchIDs(ctx context.Context, workflowRunID string) ([]string, error) {
	rows, err := s.db.Query(ctx, `
		SELECT input->>'assetId'
		FROM workflow_node_runs
		WHERE workflow_run_id = $1
		  AND status IN ('failed', 'pending', 'queued', 'running')
		  AND COALESCE(input->>'assetId', '') <> ''
		ORDER BY created_at, node_key
	`, workflowRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]string, 0)
	for rows.Next() {
		var assetID string
		if err := rows.Scan(&assetID); err != nil {
			return nil, err
		}
		items = append(items, assetID)
	}
	return items, rows.Err()
}
