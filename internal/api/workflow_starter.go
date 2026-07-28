package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/config"
	"github.com/Einzieg/cineweave/internal/observability"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/jackc/pgx/v5"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

const (
	workflowStartDefaultMaxAttempts = 12
	workflowStartErrorCode          = "TEMPORAL_START_FAILED"
)

type workflowStartDefinition struct {
	workflow    any
	decodeInput func(json.RawMessage) (any, error)
}

type workflowStartOutboxItem struct {
	ID                       string
	WorkflowRunID            *string
	AgentTaskID              *string
	CommerceSetupRunID       *string
	ProjectDeletionRequestID *string
	OrganizationID           string
	ProjectID                string
	ProductionGenerationID   string
	ProfileVersionID         string
	WorkflowType             string
	WorkflowHandler          string
	TemporalWorkflowID       string
	TaskQueue                string
	Input                    json.RawMessage
	InputHash                string
	AttemptCount             int
	MaxAttempts              int
}

type workflowStartFailure struct {
	code      string
	err       error
	permanent bool
}

type workflowStartExecutionResult string

const (
	workflowStartResultStarted         workflowStartExecutionResult = "started"
	workflowStartResultAlreadyStarted  workflowStartExecutionResult = "already_started"
	workflowStartResultCancelledFenced workflowStartExecutionResult = "cancelled_fenced"
)

func (e workflowStartFailure) Error() string {
	return e.err.Error()
}

func decodeWorkflowStartInput[T any](raw json.RawMessage) (any, error) {
	var input T
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	return input, nil
}

var workflowStartDefinitions = map[string]workflowStartDefinition{
	"commerce_project_setup":                 {workflows.CommerceProjectSetupWorkflow, decodeWorkflowStartInput[workflows.CommerceProjectSetupInput]},
	"commerce_script_unit_preparation":       {workflows.CommerceScriptUnitPreparationWorkflow, decodeWorkflowStartInput[workflows.CommerceScriptUnitPreparationInput]},
	"commerce_script_organization":           {workflows.CommerceScriptOrganizationWorkflow, decodeWorkflowStartInput[workflows.CommerceScriptOrganizationInput]},
	"commerce_storyboard_planning":           {workflows.CommerceStoryboardPlanningWorkflow, decodeWorkflowStartInput[workflows.CommerceStoryboardPlanningInput]},
	"commerce_reference_image_batch":         {workflows.CommerceReferenceImageBatchWorkflow, decodeWorkflowStartInput[workflows.CommerceReferenceImageBatchInput]},
	"commerce_video_prompt_batch":            {workflows.CommerceVideoPromptBatchWorkflow, decodeWorkflowStartInput[workflows.CommerceVideoBatchInput]},
	"commerce_shot_video_batch":              {workflows.CommerceShotVideoBatchWorkflow, decodeWorkflowStartInput[workflows.CommerceVideoBatchInput]},
	"commerce_direct_video":                  {workflows.CommerceDirectVideoWorkflow, decodeWorkflowStartInput[workflows.CommerceDirectVideoInput]},
	"commerce_script_derivation":             {workflows.CommerceScriptDerivationBatchWorkflow, decodeWorkflowStartInput[workflows.CommerceScriptDerivationBatchInput]},
	"commerce_final_compose":                 {workflows.CommerceFinalComposeWorkflow, decodeWorkflowStartInput[workflows.CommerceFinalComposeInput]},
	"commerce_script_unit_batch_coordinator": {workflows.CommerceScriptUnitBatchCoordinatorWorkflow, decodeWorkflowStartInput[workflows.CommerceScriptUnitBatchCoordinatorInput]},
	"text_to_storyboard":                     {workflows.TextToStoryboardWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"extract_novel_events":                   {workflows.ExtractNovelEventsWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"generate_adaptation_plan":               {workflows.GenerateAdaptationPlanWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"adaptation_plan_to_script":              {workflows.AdaptationPlanToScriptWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"source_to_script":                       {workflows.SourceToScriptWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"parse_script_scenes":                    {workflows.ParseScriptScenesWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"script_to_assets":                       {workflows.ScriptToAssetsWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"script_to_storyboard":                   {workflows.ScriptToStoryboardWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"script_episode_timing":                  {workflows.AnalyzeScriptEpisodeTimingWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"video_production":                       {workflows.VideoProductionWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"compose_timeline":                       {workflows.ComposeTimelineWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"regenerate_canonical_asset_image":       {workflows.RegenerateCanonicalAssetImageWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"regenerate_derived_asset_image":         {workflows.RegenerateDerivedAssetImageWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"regenerate_shot_image":                  {workflows.RegenerateShotImageWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"regenerate_shot_video":                  {workflows.RegenerateShotVideoWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"regenerate_final_video":                 {workflows.RegenerateFinalVideoWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"regenerate_script_scene":                {workflows.RegenerateScriptSceneWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"regenerate_scene_storyboard":            {workflows.RegenerateSceneStoryboardWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"batch_generate_shot_image_prompts":      {workflows.BatchGenerateShotImagePromptsWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"batch_generate_shot_images":             {workflows.BatchGenerateShotImagesWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"batch_generate_shot_video_prompts":      {workflows.BatchGenerateShotVideoPromptsWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"batch_generate_shot_videos":             {workflows.EpisodeBatchGenerateShotVideosWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"batch_cancel_shot_videos":               {workflows.BatchCancelShotVideosWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"batch_generate_asset_cards":             {workflows.BatchGenerateAssetCardsWorkflow, decodeWorkflowStartInput[workflows.AssetBatchWorkflowInput]},
	"batch_generate_asset_images":            {workflows.BatchGenerateCanonicalAssetImagesWorkflow, decodeWorkflowStartInput[workflows.AssetBatchWorkflowInput]},
	"batch_generate_derived_asset_images":    {workflows.BatchGenerateDerivedAssetImagesWorkflow, decodeWorkflowStartInput[workflows.TextToStoryboardInput]},
	"episode_audio_production":               {workflows.EpisodeAudioProductionWorkflow, decodeWorkflowStartInput[workflows.EpisodeAudioProductionInput]},
	"native_audio_review":                    {workflows.NativeAudioReviewWorkflow, decodeWorkflowStartInput[workflows.NativeAudioReviewWorkflowInput]},
	"export_project":                         {workflows.ExportProjectWorkflow, decodeWorkflowStartInput[workflows.ExportProjectInput]},
	"project_agent":                          {workflows.ProjectAgentWorkflow, decodeWorkflowStartInput[workflows.ProjectAgentWorkflowInput]},
	"project_video_production_rebuild":       {workflows.ProjectVideoProductionRebuildWorkflow, decodeWorkflowStartInput[workflows.ProjectVideoProductionRebuildInput]},
	"project_deletion":                       {workflows.ProjectDeletionWorkflow, decodeWorkflowStartInput[workflows.ProjectDeletionInput]},
}

func workflowHandlerForFunction(workflowFunc any) (string, error) {
	if workflowFunc == nil {
		return "", fmt.Errorf("workflow function is required")
	}
	value := reflect.ValueOf(workflowFunc)
	if value.Kind() != reflect.Func {
		return "", fmt.Errorf("workflow handler must be a function")
	}
	pointer := value.Pointer()
	for key, definition := range workflowStartDefinitions {
		if reflect.ValueOf(definition.workflow).Pointer() == pointer {
			return key, nil
		}
	}
	return "", fmt.Errorf("workflow function is not registered")
}

func marshalWorkflowStartInput(input any) (json.RawMessage, string, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, "", err
	}
	canonical, err := canonicalWorkflowStartInput(raw)
	if err != nil {
		return nil, "", err
	}
	return canonical, hashWorkflowStartInput(canonical), nil
}

func canonicalWorkflowStartInput(raw json.RawMessage) (json.RawMessage, error) {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("workflow input must be a JSON object: %w", err)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return canonical, nil
}

func hashWorkflowStartInput(raw json.RawMessage) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func workflowStartVisibility(item workflowStartOutboxItem) (map[string]interface{}, map[string]interface{}) {
	values := map[string]string{
		"ProjectId":              strings.TrimSpace(item.ProjectID),
		"ProductionGenerationId": strings.TrimSpace(item.ProductionGenerationID),
		"EpisodeId":              workflowStartInputString(item.Input, "scriptEpisodeId", "episodeId"),
		"ProfileVersionId":       strings.TrimSpace(item.ProfileVersionID),
		"RebuildId":              workflowStartInputString(item.Input, "rebuildId"),
	}
	searchAttributes := make(map[string]interface{}, len(values))
	memo := make(map[string]interface{}, len(values)+2)
	for key, value := range values {
		if value == "" {
			continue
		}
		searchAttributes[key] = value
		memo[key] = value
	}
	memo["WorkflowType"] = item.WorkflowType
	if item.WorkflowRunID != nil && strings.TrimSpace(*item.WorkflowRunID) != "" {
		memo["WorkflowRunId"] = strings.TrimSpace(*item.WorkflowRunID)
	}
	if item.CommerceSetupRunID != nil && strings.TrimSpace(*item.CommerceSetupRunID) != "" {
		memo["CommerceSetupRunId"] = strings.TrimSpace(*item.CommerceSetupRunID)
	}
	if item.ProjectDeletionRequestID != nil && strings.TrimSpace(*item.ProjectDeletionRequestID) != "" {
		memo["ProjectDeletionRequestId"] = strings.TrimSpace(*item.ProjectDeletionRequestID)
	}
	return searchAttributes, memo
}

func workflowStartInputString(raw json.RawMessage, keys ...string) string {
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return ""
	}
	containers := []map[string]any{root}
	for _, containerKey := range []string{"input", "plan", "options"} {
		if nested, ok := root[containerKey].(map[string]any); ok {
			containers = append(containers, nested)
		}
	}
	for _, container := range containers {
		for _, key := range keys {
			if value, ok := container[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func (s *Server) insertWorkflowRunTx(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	workflowType string,
	runInput json.RawMessage,
) (WorkflowRun, error) {
	productionContext, err := videoproduction.LoadWritableContextTx(
		ctx,
		tx,
		project.ID,
		workflowStartAllowsLockedProject(workflowType),
	)
	if err != nil {
		return WorkflowRun{}, err
	}
	var run WorkflowRun
	err = tx.QueryRow(ctx, `
		WITH new_run AS (SELECT gen_random_uuid() AS id)
		INSERT INTO workflow_runs(
			id, organization_id, project_id, temporal_workflow_id, workflow_type,
			status, input, output, created_by, production_generation_id,
			video_production_binding_id, video_production_binding_revision
		)
		SELECT id, $1, $2, 'workflow-' || id::text, $3, 'queued', $4, '{}', $5, $6, $7, $8
		FROM new_run
		RETURNING id, organization_id, project_id, production_generation_id,
		          video_production_binding_id, video_production_binding_revision,
		          template_id, temporal_workflow_id, status,
		          input, output, error_code, error_message, created_by, created_at,
		          started_at, completed_at, cancelled_at, workflow_type, total_items,
		          completed_items, failed_items, revision, attempt_generation, root_workflow_run_id,
		          retry_of_workflow_run_id, updated_at
	`, project.OrganizationID, project.ID, workflowType, runInput, principal.UserID,
		productionContext.Generation.ID, productionContext.Binding.ID, productionContext.Binding.Revision).Scan(
		&run.ID,
		&run.OrganizationID,
		&run.ProjectID,
		&run.ProductionGenerationID,
		&run.VideoProductionBindingID,
		&run.VideoProductionBindingRevision,
		&run.TemplateID,
		&run.TemporalWorkflowID,
		&run.Status,
		&run.Input,
		&run.Output,
		&run.ErrorCode,
		&run.ErrorMessage,
		&run.CreatedBy,
		&run.CreatedAt,
		&run.StartedAt,
		&run.CompletedAt,
		&run.CancelledAt,
		&run.WorkflowType,
		&run.TotalItems,
		&run.CompletedItems,
		&run.FailedItems,
		&run.Revision,
		&run.AttemptGeneration,
		&run.RootWorkflowRunID,
		&run.RetryOfWorkflowRunID,
		&run.UpdatedAt,
	)
	return run, err
}

func (s *Server) enqueueWorkflowStartTx(
	ctx context.Context,
	tx pgx.Tx,
	workflowRunID string,
	agentTaskID string,
	organizationID string,
	projectID string,
	productionGenerationID string,
	workflowType string,
	workflowHandler string,
	temporalWorkflowID string,
	taskQueue string,
	input any,
) error {
	raw, inputHash, err := marshalWorkflowStartInput(input)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO workflow_start_outbox(
			workflow_run_id, agent_task_id, organization_id, project_id, workflow_type,
			workflow_handler, temporal_workflow_id, task_queue, input, input_hash, max_attempts,
			production_generation_id
		)
		VALUES (
			NULLIF($1, '')::uuid, NULLIF($2, '')::uuid, $3, $4, $6,
			$7, $8, $9, $10, $11, $12, $5
		)
	`, workflowRunID, agentTaskID, organizationID, projectID, productionGenerationID, workflowType, workflowHandler,
		temporalWorkflowID, taskQueue, raw, inputHash, workflowStartDefaultMaxAttempts)
	return err
}

func (s *Server) enqueueCommerceSetupRunTx(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	setupSessionID string,
	input any,
) (string, error) {
	if s.temporal == nil {
		return "", apiError{Status: 503, Code: "TEMPORAL_UNAVAILABLE", Message: "Temporal client is not configured", Retryable: true}
	}
	const handler = "commerce_project_setup"
	if _, ok := workflowStartDefinitions[handler]; !ok {
		return "", errors.New("commerce project setup workflow is not registered")
	}
	raw, inputHash, err := marshalWorkflowStartInput(input)
	if err != nil {
		return "", err
	}
	var runID, temporalWorkflowID string
	if err := tx.QueryRow(ctx, `
		WITH new_run AS (SELECT gen_random_uuid() AS id)
		INSERT INTO commerce_setup_runs(
			id, organization_id, project_id, setup_session_id, temporal_workflow_id,
			attempt_no, status, input, input_hash, output, created_by
		)
		SELECT id, $1, $2, $3, 'commerce-setup-' || id::text,
		       COALESCE((SELECT MAX(attempt_no) + 1 FROM commerce_setup_runs WHERE setup_session_id = $3), 1),
		       'queued', $4, $5, '{}', $6
		FROM new_run
		RETURNING id::text, temporal_workflow_id
	`, project.OrganizationID, project.ID, setupSessionID, raw, inputHash, principal.UserID).Scan(&runID, &temporalWorkflowID); err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO workflow_start_outbox(
			workflow_run_id, agent_task_id, commerce_setup_run_id,
			organization_id, project_id, production_generation_id,
			workflow_type, workflow_handler, temporal_workflow_id, task_queue,
			input, input_hash, max_attempts
		)
		VALUES (NULL, NULL, $1, $2, $3, NULL, $4, $4, $5, $6, $7, $8, $9)
	`, runID, project.OrganizationID, project.ID, handler, temporalWorkflowID,
		workflows.ScriptTaskQueue, raw, inputHash, workflowStartDefaultMaxAttempts)
	if err != nil {
		return "", err
	}
	return runID, nil
}

func (s *Server) enqueueProjectWorkflow(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	workflowType string,
	runInput json.RawMessage,
	taskQueue string,
	workflowFunc any,
	buildInput func(WorkflowRun) any,
) (WorkflowRun, error) {
	return s.enqueueProjectWorkflowWithHook(ctx, principal, project, workflowType, runInput, taskQueue, workflowFunc, buildInput, nil)
}

func (s *Server) enqueueProjectWorkflowWithHook(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	workflowType string,
	runInput json.RawMessage,
	taskQueue string,
	workflowFunc any,
	buildInput func(WorkflowRun) any,
	afterEnqueue func(context.Context, pgx.Tx, WorkflowRun) error,
) (WorkflowRun, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return WorkflowRun{}, err
	}
	defer tx.Rollback(ctx)
	run, err := s.enqueueProjectWorkflowTx(ctx, tx, principal, project, workflowType, runInput, taskQueue, workflowFunc, buildInput, afterEnqueue)
	if err != nil {
		return WorkflowRun{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return WorkflowRun{}, err
	}
	return run, nil
}

// enqueueProjectWorkflowTx persists the operation and Temporal start outbox in
// a caller-owned transaction. It performs no network calls and never commits.
func (s *Server) enqueueProjectWorkflowTx(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	workflowType string,
	runInput json.RawMessage,
	taskQueue string,
	workflowFunc any,
	buildInput func(WorkflowRun) any,
	afterEnqueue func(context.Context, pgx.Tx, WorkflowRun) error,
) (WorkflowRun, error) {
	if s.temporal == nil {
		return WorkflowRun{}, apiError{Status: 503, Code: "TEMPORAL_UNAVAILABLE", Message: "Temporal client is not configured", Retryable: true}
	}
	handler, err := workflowHandlerForFunction(workflowFunc)
	if err != nil {
		return WorkflowRun{}, err
	}
	run, err := s.insertWorkflowRunTx(ctx, tx, principal, project, workflowType, runInput)
	if err != nil {
		return WorkflowRun{}, err
	}
	if err := s.enqueueWorkflowStartTx(
		ctx, tx, run.ID, "", project.OrganizationID, project.ID, run.ProductionGenerationID, workflowType,
		handler, run.TemporalWorkflowID, taskQueue, buildInput(run),
	); err != nil {
		return WorkflowRun{}, err
	}
	if err := insertWorkflowQueuedEventTx(ctx, tx, run, workflowType); err != nil {
		return WorkflowRun{}, err
	}
	if afterEnqueue != nil {
		if err := afterEnqueue(ctx, tx, run); err != nil {
			return WorkflowRun{}, err
		}
	}
	return run, nil
}

func (s *Server) RunWorkflowCancellationReconciler(ctx context.Context, logger *slog.Logger) {
	pollInterval := config.Duration("CINEWEAVE_WORKFLOW_CANCELLATION_RECONCILE_INTERVAL", time.Second)
	batchSize := config.Int("CINEWEAVE_WORKFLOW_CANCELLATION_RECONCILE_BATCH_SIZE", 32)
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	if batchSize <= 0 {
		batchSize = 32
	}
	ctx = observability.WithLogger(ctx, logger)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		reconciled, err := workflows.ReconcileExpiredWorkflowCancellations(ctx, s.db, batchSize)
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("workflow cancellation reconciliation failed", "error", err)
		} else if reconciled > 0 {
			logger.Info("workflow cancellations reconciled", "count", reconciled)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) RunWorkflowStartDispatcher(ctx context.Context, logger *slog.Logger) {
	if s.temporal == nil {
		logger.Warn("workflow start dispatcher disabled", "reason", "Temporal client is not configured")
		return
	}
	pollInterval := config.Duration("CINEWEAVE_WORKFLOW_START_POLL_INTERVAL", time.Second)
	leaseTimeout := config.Duration("CINEWEAVE_WORKFLOW_START_LEASE_TIMEOUT", 30*time.Second)
	startTimeout := config.Duration("CINEWEAVE_WORKFLOW_START_TIMEOUT", 15*time.Second)
	batchSize := config.Int("CINEWEAVE_WORKFLOW_START_BATCH_SIZE", 16)
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	if leaseTimeout <= 0 {
		leaseTimeout = 30 * time.Second
	}
	if startTimeout <= 0 {
		startTimeout = 15 * time.Second
	}
	if batchSize <= 0 {
		batchSize = 16
	}
	hostname, _ := os.Hostname()
	workerID := fmt.Sprintf("%s:%d", strings.TrimSpace(hostname), os.Getpid())
	ctx = observability.WithLogger(ctx, logger)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		if err := s.observeWorkflowRuntime(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Warn("workflow runtime metrics collection failed", "error", err)
		}
		processed, err := s.dispatchWorkflowStarts(ctx, workerID, leaseTimeout, startTimeout, batchSize)
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("workflow start dispatch failed", "error", err)
		}
		if ctx.Err() != nil {
			return
		}
		if processed >= batchSize {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) dispatchWorkflowStarts(
	ctx context.Context,
	workerID string,
	leaseTimeout time.Duration,
	startTimeout time.Duration,
	batchSize int,
) (int, error) {
	processed := 0
	for processed < batchSize {
		item, ok, err := s.claimWorkflowStart(ctx, workerID, leaseTimeout)
		if err != nil {
			return processed, err
		}
		if !ok {
			return processed, nil
		}
		processed++
		startCtx, cancel := context.WithTimeout(ctx, startTimeout)
		started := time.Now()
		result, executeErr := s.executeWorkflowStart(startCtx, workerID, item)
		err = executeErr
		cancel()
		if err == nil {
			observability.RecordWorkflowStart(string(result), time.Since(started))
			logWorkflowStartAttempt(ctx, item, string(result), time.Since(started), nil)
			continue
		}
		failure := workflowStartFailure{code: workflowStartErrorCode, err: err}
		if errors.As(err, &failure) {
			// The typed failure already carries the stable error code and retry policy.
		}
		resultLabel := "retry_scheduled"
		if failure.permanent || item.AttemptCount >= item.MaxAttempts {
			resultLabel = "failed"
		}
		if err := s.markWorkflowStartFailed(ctx, workerID, item, failure); err != nil {
			observability.RecordWorkflowStart("state_update_failed", time.Since(started))
			logWorkflowStartAttempt(ctx, item, "state_update_failed", time.Since(started), err)
			return processed, err
		}
		observability.RecordWorkflowStart(resultLabel, time.Since(started))
		logWorkflowStartAttempt(ctx, item, resultLabel, time.Since(started), failure.err)
	}
	return processed, nil
}

func (s *Server) claimWorkflowStart(ctx context.Context, workerID string, leaseTimeout time.Duration) (workflowStartOutboxItem, bool, error) {
	var item workflowStartOutboxItem
	err := s.db.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id
			FROM workflow_start_outbox
			WHERE (
				status = 'pending'
				AND next_attempt_at <= now()
				AND attempt_count < max_attempts
			) OR (
				status = 'processing'
				AND locked_at <= now() - $2::interval
			)
			ORDER BY next_attempt_at, created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE workflow_start_outbox AS outbox
		SET status = 'processing',
		    attempt_count = CASE
		        WHEN outbox.status = 'pending' THEN outbox.attempt_count + 1
		        ELSE outbox.attempt_count
		    END,
		    locked_at = now(),
		    locked_by = $1,
		    updated_at = now()
		FROM candidate
		WHERE outbox.id = candidate.id
		RETURNING outbox.id::text, outbox.workflow_run_id::text, outbox.agent_task_id::text,
		          outbox.commerce_setup_run_id::text,
		          outbox.project_deletion_request_id::text,
		          outbox.organization_id::text, outbox.project_id::text,
		          COALESCE(outbox.production_generation_id::text, ''),
		          COALESCE((
		            SELECT binding.profile_version_id::text
		            FROM project_video_production_generations generation
		            JOIN project_video_production_bindings binding ON binding.id = generation.binding_id
		            WHERE generation.id = outbox.production_generation_id
		          ), ''),
		          outbox.workflow_type,
		          outbox.workflow_handler, outbox.temporal_workflow_id, outbox.task_queue,
		          outbox.input, outbox.input_hash, outbox.attempt_count, outbox.max_attempts
	`, workerID, leaseTimeout.String()).Scan(
		&item.ID,
		&item.WorkflowRunID,
		&item.AgentTaskID,
		&item.CommerceSetupRunID,
		&item.ProjectDeletionRequestID,
		&item.OrganizationID,
		&item.ProjectID,
		&item.ProductionGenerationID,
		&item.ProfileVersionID,
		&item.WorkflowType,
		&item.WorkflowHandler,
		&item.TemporalWorkflowID,
		&item.TaskQueue,
		&item.Input,
		&item.InputHash,
		&item.AttemptCount,
		&item.MaxAttempts,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return workflowStartOutboxItem{}, false, nil
	}
	return item, err == nil, err
}

func (s *Server) executeWorkflowStart(ctx context.Context, workerID string, item workflowStartOutboxItem) (workflowStartExecutionResult, error) {
	canonical, err := canonicalWorkflowStartInput(item.Input)
	if err != nil {
		return "", workflowStartFailure{code: "WORKFLOW_START_PAYLOAD_INVALID", err: err, permanent: true}
	}
	if hashWorkflowStartInput(canonical) != item.InputHash {
		return "", workflowStartFailure{
			code:      "WORKFLOW_START_INPUT_HASH_MISMATCH",
			err:       fmt.Errorf("workflow start input hash does not match the persisted payload"),
			permanent: true,
		}
	}
	definition, ok := workflowStartDefinitions[item.WorkflowHandler]
	if !ok {
		return "", workflowStartFailure{
			code:      "WORKFLOW_START_HANDLER_UNKNOWN",
			err:       fmt.Errorf("workflow handler %q is not registered", item.WorkflowHandler),
			permanent: true,
		}
	}
	input, err := definition.decodeInput(canonical)
	if err != nil {
		return "", workflowStartFailure{code: "WORKFLOW_START_PAYLOAD_INVALID", err: err, permanent: true}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var status, lockedBy string
	err = tx.QueryRow(ctx, `
		SELECT status, COALESCE(locked_by, '')
		FROM workflow_start_outbox
		WHERE id = $1
		FOR UPDATE
	`, item.ID).Scan(&status, &lockedBy)
	if err != nil {
		return "", err
	}
	if status != "processing" || lockedBy != workerID {
		if err := tx.Commit(ctx); err != nil {
			return "", err
		}
		return workflowStartResultCancelledFenced, nil
	}
	if item.ProjectDeletionRequestID != nil {
		if err := assertProjectDeletionStartWritableTx(ctx, tx, item); err != nil {
			if err := s.cancelFencedWorkflowStartTx(ctx, tx, workerID, item, "PROJECT_DELETION_STALE", err.Error()); err != nil {
				return "", err
			}
			if err := tx.Commit(ctx); err != nil {
				return "", err
			}
			return workflowStartResultCancelledFenced, nil
		}
	} else if item.CommerceSetupRunID != nil {
		if err := assertCommerceSetupStartWritableTx(ctx, tx, item); err != nil {
			if err := s.cancelFencedWorkflowStartTx(ctx, tx, workerID, item, "COMMERCE_SETUP_STALE", err.Error()); err != nil {
				return "", err
			}
			if err := tx.Commit(ctx); err != nil {
				return "", err
			}
			return workflowStartResultCancelledFenced, nil
		}
	} else {
		if _, err := videoproduction.AssertGenerationWritableTx(
			ctx,
			tx,
			item.ProjectID,
			item.ProductionGenerationID,
			workflowStartAllowsLockedProject(item.WorkflowType),
		); err != nil {
			if domainErr, ok := videoproduction.AsError(err); ok &&
				(domainErr.Code == videoproduction.CodeProjectLocked || domainErr.Code == videoproduction.CodeGenerationMismatch) {
				if err := s.cancelFencedWorkflowStartTx(ctx, tx, workerID, item, domainErr.Code, domainErr.Error()); err != nil {
					return "", err
				}
				if err := tx.Commit(ctx); err != nil {
					return "", err
				}
				return workflowStartResultCancelledFenced, nil
			}
			return "", err
		}
	}

	alreadyStartedExecution := false
	searchAttributes, memo := workflowStartVisibility(item)
	startOptions := client.StartWorkflowOptions{
		ID:               item.TemporalWorkflowID,
		TaskQueue:        item.TaskQueue,
		SearchAttributes: searchAttributes,
		Memo:             memo,
	}
	if item.ProjectDeletionRequestID != nil {
		startOptions.WorkflowIDReusePolicy = enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY
	}
	_, err = s.temporal.ExecuteWorkflow(ctx, startOptions, definition.workflow, input)
	if err != nil {
		var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
		if !errors.As(err, &alreadyStarted) {
			return "", err
		}
		alreadyStartedExecution = true
	}
	tag, err := tx.Exec(ctx, `
		UPDATE workflow_start_outbox
		SET status = 'started', started_at = COALESCE(started_at, now()), completed_at = now(),
		    locked_at = NULL, locked_by = NULL, last_error_code = NULL,
		    last_error_message = NULL, updated_at = now()
		WHERE id = $1 AND status = 'processing' AND locked_by = $2
	`, item.ID, workerID)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		if err := tx.Commit(ctx); err != nil {
			return "", err
		}
		return workflowStartResultCancelledFenced, nil
	}
	if item.WorkflowRunID != nil {
		runTag, err := tx.Exec(ctx, `
			UPDATE workflow_runs
			SET status = 'running',
			    started_at = COALESCE(started_at, now()),
			    updated_at = now(), revision = revision + 1
			WHERE id = $1 AND status = 'queued'
		`, *item.WorkflowRunID)
		if err != nil {
			return "", err
		}
		if runTag.RowsAffected() > 0 {
			if err := insertAPIEvent(ctx, tx, item.OrganizationID, item.ProjectID, "workflow.run.started", "workflow_run", *item.WorkflowRunID, mustMarshal(map[string]any{
				"workflowRunId": *item.WorkflowRunID,
				"workflowType":  item.WorkflowType,
				"status":        "running",
			})); err != nil {
				return "", err
			}
		}
	}
	if item.CommerceSetupRunID != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE commerce_setup_runs
			SET status = 'running', started_at = COALESCE(started_at, now()),
			    updated_at = now(), revision = revision + 1
			WHERE id = $1 AND status = 'queued'
		`, *item.CommerceSetupRunID); err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE commerce_setup_sessions
			SET state = 'started', step = 'workflow_started', updated_at = now(), revision = revision + 1
			WHERE setup_workflow_run_id = $1 AND state = 'starting'
		`, *item.CommerceSetupRunID); err != nil {
			return "", err
		}
	}
	if item.ProjectDeletionRequestID != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE project_deletion_requests
			SET updated_at = now()
			WHERE id = $1 AND status = 'requested'
		`, *item.ProjectDeletionRequestID); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	if alreadyStartedExecution {
		return workflowStartResultAlreadyStarted, nil
	}
	return workflowStartResultStarted, nil
}

func workflowStartAllowsLockedProject(workflowType string) bool {
	return strings.TrimSpace(workflowType) == "project_video_production_rebuild"
}

func (s *Server) cancelFencedWorkflowStartTx(
	ctx context.Context,
	tx pgx.Tx,
	workerID string,
	item workflowStartOutboxItem,
	code string,
	message string,
) error {
	tag, err := tx.Exec(ctx, `
		UPDATE workflow_start_outbox
		SET status = 'cancelled', completed_at = now(), locked_at = NULL, locked_by = NULL,
		    last_error_code = $3, last_error_message = $4, updated_at = now()
		WHERE id = $1 AND status = 'processing' AND locked_by = $2
	`, item.ID, workerID, code, message)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	if item.WorkflowRunID != nil {
		runTag, err := tx.Exec(ctx, `
			UPDATE workflow_runs
			SET status = 'cancelled', error_code = $2, error_message = $3,
			    completed_at = now(), cancelled_at = now(), terminalized_at = now(), settled_at = now(),
			    updated_at = now(), revision = revision + 1
			WHERE id = $1 AND status IN ('pending', 'queued')
		`, *item.WorkflowRunID, code, message)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE workflow_node_runs
			SET status = 'cancelled', error_code = $2, error_message = $3,
			    completed_at = now(), updated_at = now(), revision = revision + 1
			WHERE workflow_run_id = $1 AND status IN ('pending', 'queued')
		`, *item.WorkflowRunID, code, message); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE project_exports
			SET status = 'cancelled', error_code = $2, error_message = $3, completed_at = now()
			WHERE workflow_run_id = $1 AND status = 'queued'
		`, *item.WorkflowRunID, code, message); err != nil {
			return err
		}
		if runTag.RowsAffected() > 0 {
			if err := insertAPIEvent(ctx, tx, item.OrganizationID, item.ProjectID, "workflow.run.cancelled", "workflow_run", *item.WorkflowRunID, mustMarshal(map[string]any{
				"workflowRunId": *item.WorkflowRunID,
				"workflowType":  item.WorkflowType,
				"status":        "cancelled",
				"errorCode":     code,
				"errorMessage":  message,
			})); err != nil {
				return err
			}
		}
	}
	if item.AgentTaskID != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE agent_tasks
			SET status = 'cancelled', error_code = $3, error_message = $4,
			    completed_at = now(), updated_at = now()
			WHERE id = $1 AND temporal_workflow_id = $2
			  AND status IN ('queued', 'planning', 'running', 'waiting_approval')
		`, *item.AgentTaskID, item.TemporalWorkflowID, code, message); err != nil {
			return err
		}
	}
	if item.CommerceSetupRunID != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE commerce_setup_runs
			SET status = 'cancelled', error_code = $2, error_message = $3,
			    completed_at = now(), updated_at = now(), revision = revision + 1
			WHERE id = $1 AND status IN ('queued', 'running')
		`, *item.CommerceSetupRunID, code, message); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE commerce_setup_sessions
			SET state = 'failed', step = 'workflow_start_cancelled',
			    last_error_code = $2, last_error_message = $3,
			    updated_at = now(), revision = revision + 1
			WHERE setup_workflow_run_id = $1 AND state NOT IN ('completed', 'abandoned')
		`, *item.CommerceSetupRunID, code, message); err != nil {
			return err
		}
	}
	if item.ProjectDeletionRequestID != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE project_deletion_requests
			SET status = 'failed_retryable',
			    error_code = $2,
			    error_message = $3,
			    updated_at = now()
			WHERE id = $1
			  AND status NOT IN ('completed', 'failed_terminal')
		`, *item.ProjectDeletionRequestID, code, message); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) observeWorkflowRuntime(ctx context.Context) error {
	var snapshot observability.WorkflowRuntimeSnapshot
	if err := s.db.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE status = 'pending'),
			count(*) FILTER (WHERE status = 'processing'),
			COALESCE(EXTRACT(EPOCH FROM now() - min(created_at) FILTER (
				WHERE status IN ('pending', 'processing')
			)), 0)
		FROM workflow_start_outbox
	`).Scan(
		&snapshot.PendingOutboxItems,
		&snapshot.ProcessingOutboxItems,
		&snapshot.OldestQueuedAgeSeconds,
	); err != nil {
		return err
	}
	if err := s.db.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE status = 'partial_succeeded'),
			count(*) FILTER (WHERE status = 'cancelling'),
			COALESCE(EXTRACT(EPOCH FROM now() - min(cancelled_at) FILTER (
				WHERE status = 'cancelling'
			)), 0)
		FROM workflow_runs
	`).Scan(
		&snapshot.PartialSucceededRuns,
		&snapshot.CancellingRuns,
		&snapshot.OldestCancellationSeconds,
	); err != nil {
		return err
	}
	if err := s.db.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE node.status = 'queued'),
			count(*) FILTER (WHERE node.status = 'running')
		FROM workflow_node_runs AS node
		JOIN workflow_runs AS run ON run.id = node.workflow_run_id
		WHERE run.status IN ('pending', 'queued', 'running', 'cancelling')
	`).Scan(&snapshot.QueuedNodes, &snapshot.RunningNodes); err != nil {
		return err
	}
	observability.SetWorkflowRuntime(snapshot)
	return nil
}

func logWorkflowStartAttempt(ctx context.Context, item workflowStartOutboxItem, result string, duration time.Duration, runErr error) {
	args := []any{
		"outboxId", item.ID,
		"workflowType", item.WorkflowType,
		"workflowHandler", item.WorkflowHandler,
		"temporalWorkflowId", item.TemporalWorkflowID,
		"taskQueue", item.TaskQueue,
		"attempt", item.AttemptCount,
		"result", result,
		"durationMs", duration.Milliseconds(),
	}
	if item.WorkflowRunID != nil {
		args = append(args, "workflowRunId", *item.WorkflowRunID)
	}
	if item.AgentTaskID != nil {
		args = append(args, "agentTaskId", *item.AgentTaskID)
	}
	if item.CommerceSetupRunID != nil {
		args = append(args, "commerceSetupRunId", *item.CommerceSetupRunID)
	}
	if item.ProjectDeletionRequestID != nil {
		args = append(args, "projectDeletionRequestId", *item.ProjectDeletionRequestID)
	}
	if runErr != nil {
		args = append(args, "error", runErr)
	}
	level := slog.LevelInfo
	if runErr != nil {
		level = slog.LevelWarn
	}
	observability.Log(ctx, level, "workflow start attempt completed", args...)
}

func (s *Server) markWorkflowStartFailed(ctx context.Context, workerID string, item workflowStartOutboxItem, failure workflowStartFailure) error {
	terminal := failure.permanent || item.AttemptCount >= item.MaxAttempts
	if !terminal {
		delay := workflowStartRetryDelay(item.AttemptCount)
		_, err := s.db.Exec(ctx, `
			UPDATE workflow_start_outbox
			SET status = 'pending', next_attempt_at = now() + $3::interval,
			    locked_at = NULL, locked_by = NULL, last_error_code = $4,
			    last_error_message = $5, updated_at = now()
			WHERE id = $1 AND status = 'processing' AND locked_by = $2
		`, item.ID, workerID, delay.String(), failure.code, failure.err.Error())
		return err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE workflow_start_outbox
		SET status = 'failed', completed_at = now(), locked_at = NULL, locked_by = NULL,
		    last_error_code = $3, last_error_message = $4, updated_at = now()
		WHERE id = $1 AND status = 'processing' AND locked_by = $2
	`, item.ID, workerID, failure.code, failure.err.Error())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	if item.WorkflowRunID != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE workflow_runs
			SET status = 'failed', error_code = $2, error_message = $3,
			    completed_at = now(), updated_at = now(), revision = revision + 1
			WHERE id = $1 AND status = 'queued'
		`, *item.WorkflowRunID, failure.code, failure.err.Error()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE project_exports
			SET status = 'failed', error_code = $2, error_message = $3, completed_at = now()
			WHERE workflow_run_id = $1 AND status = 'queued'
		`, *item.WorkflowRunID, failure.code, failure.err.Error()); err != nil {
			return err
		}
		if err := insertAPIEvent(ctx, tx, item.OrganizationID, item.ProjectID, "workflow.run.failed", "workflow_run", *item.WorkflowRunID, mustMarshal(map[string]any{
			"workflowRunId": *item.WorkflowRunID,
			"workflowType":  item.WorkflowType,
			"status":        "failed",
			"errorCode":     failure.code,
			"errorMessage":  failure.err.Error(),
		})); err != nil {
			return err
		}
	}
	if item.AgentTaskID != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE agent_tasks
			SET status = 'failed', error_code = $3, error_message = $4,
			    completed_at = now(), updated_at = now()
			WHERE id = $1 AND temporal_workflow_id = $2
			  AND status IN ('queued', 'planning', 'running')
		`, *item.AgentTaskID, item.TemporalWorkflowID, failure.code, failure.err.Error()); err != nil {
			return err
		}
	}
	if item.CommerceSetupRunID != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE commerce_setup_runs
			SET status = 'failed', error_code = $2, error_message = $3,
			    completed_at = now(), updated_at = now(), revision = revision + 1
			WHERE id = $1 AND status = 'queued'
		`, *item.CommerceSetupRunID, failure.code, failure.err.Error()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE commerce_setup_sessions
			SET state = 'failed', step = 'workflow_start_failed',
			    last_error_code = $2, last_error_message = $3,
			    updated_at = now(), revision = revision + 1
			WHERE setup_workflow_run_id = $1 AND state NOT IN ('completed', 'abandoned')
		`, *item.CommerceSetupRunID, failure.code, failure.err.Error()); err != nil {
			return err
		}
	}
	if item.ProjectDeletionRequestID != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE project_deletion_requests
			SET status = 'failed_retryable',
			    error_code = $2,
			    error_message = $3,
			    updated_at = now()
			WHERE id = $1
			  AND status NOT IN ('completed', 'failed_terminal')
		`, *item.ProjectDeletionRequestID, failure.code, failure.err.Error()); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func assertCommerceSetupStartWritableTx(ctx context.Context, tx pgx.Tx, item workflowStartOutboxItem) error {
	if item.CommerceSetupRunID == nil || strings.TrimSpace(*item.CommerceSetupRunID) == "" {
		return errors.New("commerce setup run identity is missing")
	}
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM commerce_setup_runs run
		JOIN commerce_setup_sessions session ON session.id = run.setup_session_id
		JOIN projects project ON project.id = run.project_id AND project.organization_id = run.organization_id
		WHERE run.id = $1
		  AND run.organization_id = $2
		  AND run.project_id = $3
		  AND run.status = 'queued'
		  AND session.setup_workflow_run_id = run.id
		  AND session.state = 'starting'
		  AND session.expires_at > now()
		  AND project.project_kind = 'commerce_video'
		  AND project.active_video_production_generation_id IS NULL
		  AND project.video_production_state = 'unconfigured'
	`, *item.CommerceSetupRunID, item.OrganizationID, item.ProjectID).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return errors.New("commerce setup run is no longer writable")
	}
	return nil
}

func assertProjectDeletionStartWritableTx(ctx context.Context, tx pgx.Tx, item workflowStartOutboxItem) error {
	if item.ProjectDeletionRequestID == nil || strings.TrimSpace(*item.ProjectDeletionRequestID) == "" {
		return errors.New("project deletion request identity is missing")
	}
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM project_deletion_requests request
		JOIN projects project
		  ON project.id = request.project_id
		 AND project.organization_id = request.organization_id
		WHERE request.id = $1
		  AND request.organization_id = $2
		  AND request.project_id = $3
		  AND request.status = 'requested'
		  AND project.lifecycle_status = 'deleting'
		  AND project.deletion_revision = request.deletion_revision
	`, *item.ProjectDeletionRequestID, item.OrganizationID, item.ProjectID).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return errors.New("project deletion request is no longer writable")
	}
	return nil
}

func workflowStartRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 9 {
		attempt = 9
	}
	delay := time.Second * time.Duration(1<<(attempt-1))
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}
