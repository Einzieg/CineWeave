package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	CommerceDirectVideoWorkflowName = "CommerceDirectVideoWorkflow"

	commerceDirectVideoWorkflowType     = "commerce_direct_video"
	CreateCommerceDirectVideoActivity   = "CreateCommerceDirectVideoTask"
	PollCommerceDirectVideoActivity     = "PollCommerceDirectVideoTask"
	CompleteCommerceDirectVideoActivity = "CompleteCommerceDirectVideo"
	FailCommerceDirectVideoActivity     = "FailCommerceDirectVideo"
	CancelCommerceDirectVideoActivity   = "CancelCommerceDirectVideo"
)

type CommerceDirectVideoInput struct {
	OrganizationID          string `json:"organizationId"`
	ProjectID               string `json:"projectId"`
	ScriptUnitID            string `json:"scriptUnitId"`
	JobID                   string `json:"jobId"`
	WorkflowRunID           string `json:"workflowRunId"`
	CreatedBy               string `json:"createdBy"`
	AttemptGeneration       int    `json:"attemptGeneration"`
	ProjectControlCommandID string `json:"projectControlCommandId,omitempty"`
}

type CommerceDirectVideoTaskState struct {
	JobID               string                      `json:"jobId"`
	Status              string                      `json:"status"`
	NodeExecution       NodeExecution               `json:"nodeExecution"`
	ProviderRequestID   string                      `json:"providerRequestId,omitempty"`
	ProviderCallID      string                      `json:"providerCallId,omitempty"`
	ProviderAsyncTaskID string                      `json:"providerAsyncTaskId,omitempty"`
	ExternalTaskID      string                      `json:"externalTaskId,omitempty"`
	Output              provider.GatewayVideoOutput `json:"output,omitempty"`
}

type CommerceDirectVideoOutput struct {
	JobID             string `json:"jobId"`
	Status            string `json:"status"`
	OutputArtifactID  string `json:"outputArtifactId"`
	OutputMediaFileID string `json:"outputMediaFileId"`
	OutputStorageKey  string `json:"outputStorageKey"`
}

type CommerceDirectVideoFailureInput struct {
	WorkflowInput CommerceDirectVideoInput `json:"workflowInput"`
	ErrorCode     string                   `json:"errorCode"`
	ErrorMessage  string                   `json:"errorMessage"`
}

func EnqueueCommerceDirectVideoTx(
	ctx context.Context,
	tx pgx.Tx,
	input CommerceDirectVideoInput,
	production commerce.ProductionContext,
) error {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" ||
		strings.TrimSpace(input.ScriptUnitID) == "" || strings.TrimSpace(input.JobID) == "" ||
		strings.TrimSpace(input.WorkflowRunID) == "" ||
		strings.TrimSpace(input.CreatedBy) == "" {
		return errors.New("commerce direct video workflow identity is incomplete")
	}
	input.AttemptGeneration = 1
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	inputHash, err := commerceContractHash(input)
	if err != nil {
		return err
	}
	temporalWorkflowID := fmt.Sprintf("commerce-direct-video-%s", input.JobID)
	if _, err := tx.Exec(ctx, `
		INSERT INTO workflow_runs(
			id, organization_id, project_id, temporal_workflow_id, workflow_type,
			status, input, output, created_by, production_generation_id,
			video_production_binding_id, video_production_binding_revision
		)
		VALUES ($1, $2, $3, $4, $5, 'queued', $6, '{}', $7, $8, $9, $10)
	`, input.WorkflowRunID, input.OrganizationID, input.ProjectID, temporalWorkflowID,
		commerceDirectVideoWorkflowType, raw, input.CreatedBy,
		production.Generation.ID, production.VideoBinding.ID, production.VideoBinding.Revision); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO workflow_start_outbox(
			workflow_run_id, organization_id, project_id, production_generation_id,
			workflow_type, workflow_handler, temporal_workflow_id, task_queue,
			input, input_hash, max_attempts
		)
		VALUES ($1, $2, $3, $4, $5, $5, $6, $7, $8, $9, 12)
	`, input.WorkflowRunID, input.OrganizationID, input.ProjectID, production.Generation.ID,
		commerceDirectVideoWorkflowType, temporalWorkflowID, ScriptTaskQueue, raw, inputHash)
	return err
}

func CommerceDirectVideoWorkflow(ctx workflow.Context, input CommerceDirectVideoInput) (output CommerceDirectVideoOutput, workflowErr error) {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" ||
		strings.TrimSpace(input.ScriptUnitID) == "" || strings.TrimSpace(input.JobID) == "" ||
		strings.TrimSpace(input.WorkflowRunID) == "" {
		return output, temporal.NewNonRetryableApplicationError(
			"带货视频直生成任务身份不完整", "COMMERCE_DIRECT_VIDEO_INVALID", nil,
		)
	}
	defer func() {
		if workflowErr == nil {
			return
		}
		disconnected, _ := workflow.NewDisconnectedContext(ctx)
		if temporal.IsCanceledError(workflowErr) {
			_ = workflow.ExecuteActivity(
				workflow.WithActivityOptions(disconnected, commerceDirectVideoProviderActivityOptions()),
				CancelCommerceDirectVideoActivity, input,
			).Get(disconnected, nil)
			return
		}
		code, message := commerceDirectVideoErrorFields(workflowErr)
		_ = workflow.ExecuteActivity(
			workflow.WithActivityOptions(disconnected, defaultActivityOptions()),
			FailCommerceDirectVideoActivity,
			CommerceDirectVideoFailureInput{WorkflowInput: input, ErrorCode: code, ErrorMessage: message},
		).Get(disconnected, nil)
	}()

	providerCtx := workflow.WithActivityOptions(ctx, commerceDirectVideoProviderActivityOptions())
	var state CommerceDirectVideoTaskState
	if workflowErr = workflow.ExecuteActivity(providerCtx, CreateCommerceDirectVideoActivity, input).Get(providerCtx, &state); workflowErr != nil {
		return output, workflowErr
	}
	for state.Status != "succeeded" {
		if state.Status == "failed" || state.Status == "cancelled" {
			return output, temporal.NewNonRetryableApplicationError(
				"视频供应商任务未成功完成", "COMMERCE_DIRECT_VIDEO_FAILED", nil,
			)
		}
		if workflowErr = workflow.Sleep(ctx, 8*time.Second); workflowErr != nil {
			return output, workflowErr
		}
		if workflowErr = workflow.ExecuteActivity(providerCtx, PollCommerceDirectVideoActivity, input).Get(providerCtx, &state); workflowErr != nil {
			return output, workflowErr
		}
	}
	output = CommerceDirectVideoOutput{
		JobID: input.JobID, Status: state.Status,
		OutputArtifactID: state.Output.ArtifactID, OutputMediaFileID: state.Output.MediaFileID,
		OutputStorageKey: state.Output.StorageKey,
	}
	completeCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	if workflowErr = workflow.ExecuteActivity(completeCtx, CompleteCommerceDirectVideoActivity, input, output).Get(completeCtx, nil); workflowErr != nil {
		return CommerceDirectVideoOutput{}, workflowErr
	}
	return output, nil
}

func commerceDirectVideoProviderActivityOptions() workflow.ActivityOptions {
	options := defaultActivityOptions()
	options.StartToCloseTimeout = 30 * time.Minute
	options.HeartbeatTimeout = providerTextHeartbeatTimeout
	if options.RetryPolicy != nil {
		options.RetryPolicy.MaximumAttempts = 3
	}
	return options
}

func (a Activities) CreateCommerceDirectVideoTask(
	ctx context.Context,
	input CommerceDirectVideoInput,
) (CommerceDirectVideoTaskState, error) {
	service := commerce.NewDirectVideoService(commerce.NewRepository())
	job, err := service.GetJob(ctx, a.db, input.OrganizationID, input.ProjectID, input.JobID)
	if err != nil {
		return CommerceDirectVideoTaskState{}, err
	}
	if err := validateCommerceDirectVideoWorkflowJob(input, job); err != nil {
		return CommerceDirectVideoTaskState{}, err
	}
	if job.Status == "succeeded" {
		return commerceDirectVideoTaskState(job, NodeExecution{}), nil
	}
	if job.Status == "failed" || job.Status == "cancelled" {
		return CommerceDirectVideoTaskState{}, temporal.NewNonRetryableApplicationError(
			directVideoErrorMessage(job), directVideoErrorCode(job), nil,
		)
	}
	execution, err := a.loadOrStartCommerceDirectVideoNode(ctx, input, job)
	if err != nil {
		return CommerceDirectVideoTaskState{}, err
	}
	if job.ProviderAsyncTaskID != nil && strings.TrimSpace(*job.ProviderAsyncTaskID) != "" {
		return commerceDirectVideoTaskState(job, execution), nil
	}
	var contract struct {
		InputContractHash string `json:"inputContractHash"`
		Route             struct {
			InputContract struct {
				ContractKey string `json:"contractKey"`
			} `json:"inputContract"`
		} `json:"route"`
	}
	if err := json.Unmarshal(job.ExecutionContract, &contract); err != nil {
		return CommerceDirectVideoTaskState{}, err
	}
	references := make([]provider.GatewayVideoReference, 0, len(job.References))
	for _, reference := range job.References {
		references = append(references, provider.GatewayVideoReference{
			Role: reference.ReferenceRole, Type: "image", Semantics: "product_visual_reference",
			SourceType: reference.SourceType, SourceID: reference.SourceID,
			SourceVersion: strconv.FormatInt(reference.SourceRevision, 10),
			ArtifactID:    reference.ArtifactID, MediaFileID: reference.MediaFileID,
			ContentHash: reference.ContentHash, StorageKey: reference.StorageKey, MimeType: reference.MimeType,
		})
	}
	activity.RecordHeartbeat(ctx, map[string]any{"jobId": job.ID, "stage": "provider_create"})
	response, err := a.gateway.CreateVideoTaskResult(ctx, provider.GatewayVideoCreateTaskRequest{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID,
		CommerceDirectVideoJobID:       job.ID,
		ProductionGenerationID:         job.ProjectProductionGenerationID,
		VideoProductionBindingID:       job.VideoProductionBindingID,
		VideoProductionBindingRevision: job.VideoProductionBindingRevision,
		ProductionProfileVersionID:     job.VideoProfileVersionID,
		ProductionProfileSnapshotHash:  job.VideoProfileSnapshotHash,
		InputContractKey:               contract.Route.InputContract.ContractKey,
		InputContractHash:              contract.InputContractHash,
		InputContractVersion:           commerce.CommerceDirectVideoContractV1,
		NativeAudioRequired:            job.GenerateAudio,
		WorkflowRunID:                  input.WorkflowRunID, NodeRunID: execution.NodeRunID,
		NodeExecutionToken: execution.ExecutionToken, NodeAttemptGeneration: execution.AttemptGeneration,
		ModelProfileKey: job.ModelProfileKey, ProviderModelID: pointerString(job.ProviderModelID),
		ProviderModelKey: job.ProviderModelKey,
		PromptHash:       job.PromptHash, PromptSource: "user_script",
		IdempotencyKey:         "commerce-direct-video:" + job.ID + ":create",
		CapabilitySnapshotHash: job.CapabilitySnapshotHash,
		Input: mustJSON(map[string]any{
			"prompt": job.ScriptSnapshot, "duration": job.RequestedDurationSeconds,
			"aspectRatio": job.AspectRatio, "resolution": job.Resolution,
			"mode": "image_to_video", "generateAudio": job.GenerateAudio,
			"commerceDirectVideoJobId": job.ID,
		}),
		References: references,
		Options:    provider.GatewayVideoOptions{TimeoutMS: 20 * 60 * 1000},
	})
	if err != nil {
		return CommerceDirectVideoTaskState{}, err
	}
	if response.Error != nil || response.Status == "failed" {
		code, message := "COMMERCE_DIRECT_VIDEO_FAILED", "视频供应商拒绝了生成请求"
		if response.Error != nil {
			code, message = response.Error.Code, response.Error.Message
		}
		_ = a.failCommerceDirectVideoJob(ctx, input, execution, code, message)
		return CommerceDirectVideoTaskState{}, temporal.NewNonRetryableApplicationError(message, code, nil)
	}
	if strings.TrimSpace(response.ProviderAsyncTaskID) == "" {
		return CommerceDirectVideoTaskState{}, errors.New("provider video create response has no async task identity")
	}
	if err := a.persistCommerceDirectVideoCreated(ctx, input, execution, response); err != nil {
		return CommerceDirectVideoTaskState{}, err
	}
	job, err = service.GetJob(ctx, a.db, input.OrganizationID, input.ProjectID, input.JobID)
	if err != nil {
		return CommerceDirectVideoTaskState{}, err
	}
	return commerceDirectVideoTaskState(job, execution), nil
}

func (a Activities) PollCommerceDirectVideoTask(
	ctx context.Context,
	input CommerceDirectVideoInput,
) (CommerceDirectVideoTaskState, error) {
	service := commerce.NewDirectVideoService(commerce.NewRepository())
	job, err := service.GetJob(ctx, a.db, input.OrganizationID, input.ProjectID, input.JobID)
	if err != nil {
		return CommerceDirectVideoTaskState{}, err
	}
	if err := validateCommerceDirectVideoWorkflowJob(input, job); err != nil {
		return CommerceDirectVideoTaskState{}, err
	}
	execution, err := a.loadCommerceDirectVideoNode(ctx, input, job)
	if err != nil {
		return CommerceDirectVideoTaskState{}, err
	}
	if job.Status == "succeeded" || job.Status == "failed" || job.Status == "cancelled" {
		return commerceDirectVideoTaskState(job, execution), nil
	}
	if job.ProviderAsyncTaskID == nil || strings.TrimSpace(*job.ProviderAsyncTaskID) == "" {
		return CommerceDirectVideoTaskState{}, errors.New("commerce direct video job has no provider async task")
	}
	activity.RecordHeartbeat(ctx, map[string]any{"jobId": job.ID, "stage": "provider_poll"})
	response, err := a.gateway.PollVideoTaskResult(ctx, provider.GatewayVideoPollTaskRequest{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID,
		ProviderAsyncTaskID:            *job.ProviderAsyncTaskID,
		ProductionGenerationID:         job.ProjectProductionGenerationID,
		VideoProductionBindingID:       job.VideoProductionBindingID,
		VideoProductionBindingRevision: job.VideoProductionBindingRevision,
		WorkflowRunID:                  input.WorkflowRunID, NodeRunID: execution.NodeRunID,
		NodeExecutionToken: execution.ExecutionToken, NodeAttemptGeneration: execution.AttemptGeneration,
		Options: provider.GatewayVideoOptions{TimeoutMS: 2 * 60 * 1000},
	})
	if err != nil {
		return CommerceDirectVideoTaskState{}, err
	}
	switch response.Status {
	case "succeeded":
		if strings.TrimSpace(response.Output.ArtifactID) == "" || strings.TrimSpace(response.Output.MediaFileID) == "" {
			return CommerceDirectVideoTaskState{}, errors.New("provider video succeeded without stored media")
		}
		if err := a.completeCommerceDirectVideoJob(ctx, input, execution, response); err != nil {
			return CommerceDirectVideoTaskState{}, err
		}
	case "failed", "cancelled":
		code, message := "COMMERCE_DIRECT_VIDEO_FAILED", "视频供应商任务执行失败"
		if response.Error != nil {
			code, message = response.Error.Code, response.Error.Message
		}
		if err := a.failCommerceDirectVideoJob(ctx, input, execution, code, message); err != nil {
			return CommerceDirectVideoTaskState{}, err
		}
		return CommerceDirectVideoTaskState{}, temporal.NewNonRetryableApplicationError(message, code, nil)
	default:
		if err := a.progressCommerceDirectVideoJob(ctx, input, execution, response.Status); err != nil {
			return CommerceDirectVideoTaskState{}, err
		}
	}
	job, err = service.GetJob(ctx, a.db, input.OrganizationID, input.ProjectID, input.JobID)
	if err != nil {
		return CommerceDirectVideoTaskState{}, err
	}
	state := commerceDirectVideoTaskState(job, execution)
	state.Output = response.Output
	return state, nil
}

func (a Activities) CompleteCommerceDirectVideo(
	ctx context.Context,
	input CommerceDirectVideoInput,
	output CommerceDirectVideoOutput,
) error {
	job, err := commerce.NewDirectVideoService(commerce.NewRepository()).GetJob(
		ctx, a.db, input.OrganizationID, input.ProjectID, input.JobID,
	)
	if err != nil {
		return err
	}
	if err := validateCommerceDirectVideoWorkflowJob(input, job); err != nil {
		return err
	}
	if job.Status != "succeeded" ||
		pointerString(job.OutputArtifactID) != strings.TrimSpace(output.OutputArtifactID) ||
		pointerString(job.OutputMediaFileID) != strings.TrimSpace(output.OutputMediaFileID) ||
		pointerString(job.OutputStorageKey) != strings.TrimSpace(output.OutputStorageKey) {
		return ErrWorkflowWriteFenced
	}
	return TransitionWorkflowRun(ctx, a.db, input.WorkflowRunID, "succeeded", "", "", mustJSON(output))
}

func (a Activities) FailCommerceDirectVideo(ctx context.Context, input CommerceDirectVideoFailureInput) error {
	job, err := commerce.NewDirectVideoService(commerce.NewRepository()).GetJob(
		ctx, a.db, input.WorkflowInput.OrganizationID, input.WorkflowInput.ProjectID, input.WorkflowInput.JobID,
	)
	if err != nil {
		return err
	}
	if err := validateCommerceDirectVideoWorkflowJob(input.WorkflowInput, job); err != nil {
		return err
	}
	if job.Status == "succeeded" {
		output := CommerceDirectVideoOutput{
			JobID:             job.ID,
			Status:            job.Status,
			OutputArtifactID:  pointerString(job.OutputArtifactID),
			OutputMediaFileID: pointerString(job.OutputMediaFileID),
			OutputStorageKey:  pointerString(job.OutputStorageKey),
		}
		return TransitionWorkflowRun(
			ctx, a.db, input.WorkflowInput.WorkflowRunID, "succeeded", "", "", mustJSON(output),
		)
	}
	if job.Status == "cancelled" {
		return CancelWorkflowRun(
			ctx, a.db, input.WorkflowInput.WorkflowRunID,
			mustJSON(map[string]any{"jobId": input.WorkflowInput.JobID, "status": "cancelled"}),
			directVideoErrorMessage(job),
		)
	}
	code, message := strings.TrimSpace(input.ErrorCode), strings.TrimSpace(input.ErrorMessage)
	if job.Status == "failed" {
		code, message = directVideoErrorCode(job), directVideoErrorMessage(job)
	}
	execution := NodeExecution{}
	if job.NodeRunID != nil {
		execution, err = a.loadCommerceDirectVideoNode(ctx, input.WorkflowInput, job)
		if err != nil {
			return err
		}
	}
	if err := a.failCommerceDirectVideoJob(ctx, input.WorkflowInput, execution, code, message); err != nil {
		return err
	}
	return TransitionWorkflowRun(
		ctx, a.db, input.WorkflowInput.WorkflowRunID, "failed",
		code, message, mustJSON(map[string]any{"jobId": input.WorkflowInput.JobID}),
	)
}

func (a Activities) CancelCommerceDirectVideo(ctx context.Context, input CommerceDirectVideoInput) error {
	service := commerce.NewDirectVideoService(commerce.NewRepository())
	job, err := service.GetJob(ctx, a.db, input.OrganizationID, input.ProjectID, input.JobID)
	if err != nil {
		return err
	}
	if err := validateCommerceDirectVideoWorkflowJob(input, job); err != nil {
		return err
	}
	if job.ProviderAsyncTaskID != nil && strings.TrimSpace(*job.ProviderAsyncTaskID) != "" {
		_, _ = a.gateway.CancelVideoTask(ctx, provider.GatewayVideoCancelTaskRequest{
			OrganizationID: input.OrganizationID, ProviderAsyncTaskID: *job.ProviderAsyncTaskID,
			IdempotencyKey: "commerce-direct-video:" + job.ID + ":cancel",
		})
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE commerce_direct_video_jobs
		SET status = 'cancelled', completed_at = now(), cancelled_at = now(),
		    error_code = 'CANCELLED', error_message = '任务已取消', updated_at = now()
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND script_unit_id = $4 AND workflow_run_id = $5
		  AND status IN ('queued', 'running', 'cancelling')
	`, input.JobID, input.OrganizationID, input.ProjectID, input.ScriptUnitID, input.WorkflowRunID)
	if err != nil {
		return err
	}
	if _, _, err := cancelWorkflowRunTx(
		ctx, tx, input.WorkflowRunID,
		mustJSON(map[string]any{"jobId": input.JobID, "status": "cancelled"}),
		"任务已取消", "CANCELLED",
	); err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID,
			"commerce.direct_video.cancelled", "commerce_direct_video_job", input.JobID,
			mustJSON(map[string]any{
				"workflowRunId": input.WorkflowRunID, "commerceScriptUnitId": input.ScriptUnitID,
			})); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (a Activities) loadOrStartCommerceDirectVideoNode(
	ctx context.Context,
	input CommerceDirectVideoInput,
	job commerce.DirectVideoJob,
) (NodeExecution, error) {
	if job.NodeRunID != nil && strings.TrimSpace(*job.NodeRunID) != "" {
		return a.loadCommerceDirectVideoNode(ctx, input, job)
	}
	execution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID,
		WorkflowRunID: input.WorkflowRunID, NodeKey: "direct_video:" + input.JobID,
		NodeType: "provider_video", AttemptGeneration: input.AttemptGeneration,
		Input: mustJSON(map[string]any{"jobId": input.JobID}),
	})
	if err != nil {
		return NodeExecution{}, err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return NodeExecution{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE commerce_direct_video_jobs
		SET node_run_id = $2, status = 'running',
		    started_at = COALESCE(started_at, now()), updated_at = now()
		WHERE id = $1 AND organization_id = $3 AND project_id = $4
		  AND workflow_run_id = $5 AND status IN ('queued', 'running')
		  AND (node_run_id IS NULL OR node_run_id = $2)
	`, input.JobID, execution.NodeRunID, input.OrganizationID, input.ProjectID, input.WorkflowRunID)
	if err != nil {
		return NodeExecution{}, err
	}
	if tag.RowsAffected() != 1 {
		return NodeExecution{}, ErrWorkflowWriteFenced
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"commerce.direct_video.started", "commerce_direct_video_job", input.JobID,
		mustJSON(map[string]any{
			"workflowRunId": input.WorkflowRunID, "nodeRunId": execution.NodeRunID,
			"commerceScriptUnitId": input.ScriptUnitID,
		})); err != nil {
		return NodeExecution{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return NodeExecution{}, err
	}
	return execution, nil
}

func (a Activities) loadCommerceDirectVideoNode(
	ctx context.Context,
	input CommerceDirectVideoInput,
	job commerce.DirectVideoJob,
) (NodeExecution, error) {
	if job.NodeRunID == nil {
		return NodeExecution{}, errors.New("commerce direct video node is not initialized")
	}
	var execution NodeExecution
	err := a.db.QueryRow(ctx, `
		SELECT node.id::text, node.execution_token::text, node.attempt_generation,
		       run.production_generation_id::text, run.video_production_binding_id::text,
		       run.video_production_binding_revision
		FROM workflow_node_runs node
		JOIN workflow_runs run ON run.id = node.workflow_run_id
		WHERE node.id = $1 AND node.workflow_run_id = $2
		  AND node.organization_id = $3 AND node.project_id = $4
		  AND node.node_key = $5
	`, *job.NodeRunID, input.WorkflowRunID, input.OrganizationID, input.ProjectID,
		"direct_video:"+input.JobID).Scan(
		&execution.NodeRunID, &execution.ExecutionToken, &execution.AttemptGeneration,
		&execution.ProductionGenerationID, &execution.VideoProductionBindingID,
		&execution.VideoProductionBindingRevision,
	)
	if err == nil &&
		(execution.ProductionGenerationID != job.ProjectProductionGenerationID ||
			execution.VideoProductionBindingID != job.VideoProductionBindingID ||
			execution.VideoProductionBindingRevision != job.VideoProductionBindingRevision) {
		return NodeExecution{}, ErrWorkflowWriteFenced
	}
	return execution, err
}

func (a Activities) persistCommerceDirectVideoCreated(
	ctx context.Context,
	input CommerceDirectVideoInput,
	execution NodeExecution,
	response provider.GatewayVideoCreateTaskResponse,
) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE commerce_direct_video_jobs
		SET provider_request_id = COALESCE(provider_request_id, NULLIF($2, '')::uuid),
		    provider_call_id = COALESCE(provider_call_id, NULLIF($3, '')::uuid),
		    provider_async_task_id = COALESCE(provider_async_task_id, NULLIF($4, '')::uuid),
		    external_task_id = COALESCE(external_task_id, NULLIF($5, '')),
		    status = 'running', updated_at = now()
		WHERE id = $1 AND organization_id = $6 AND project_id = $7
		  AND workflow_run_id = $8 AND node_run_id = $9
		  AND status = 'running'
	`, input.JobID, response.ProviderRequestID, response.ProviderCallID,
		response.ProviderAsyncTaskID, response.ExternalTaskID,
		input.OrganizationID, input.ProjectID, input.WorkflowRunID, execution.NodeRunID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrWorkflowWriteFenced
	}
	return tx.Commit(ctx)
}

func (a Activities) progressCommerceDirectVideoJob(
	ctx context.Context,
	input CommerceDirectVideoInput,
	execution NodeExecution,
	status string,
) error {
	if err := ProgressNodeRun(ctx, a.db, execution, mustJSON(map[string]any{
		"jobId": input.JobID, "providerStatus": status,
	})); err != nil {
		return err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"commerce.direct_video.progressed", "commerce_direct_video_job", input.JobID,
		mustJSON(map[string]any{
			"workflowRunId": input.WorkflowRunID, "status": status,
			"commerceScriptUnitId": input.ScriptUnitID,
		})); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) completeCommerceDirectVideoJob(
	ctx context.Context,
	input CommerceDirectVideoInput,
	execution NodeExecution,
	response provider.GatewayVideoPollTaskResponse,
) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE commerce_direct_video_jobs
		SET status = 'succeeded', output_artifact_id = $2, output_media_file_id = $3,
		    output_storage_key = NULLIF($4, ''), output_mime_type = NULLIF($5, ''),
		    completed_at = now(), updated_at = now(), error_code = NULL, error_message = NULL
		WHERE id = $1 AND organization_id = $6 AND project_id = $7
		  AND workflow_run_id = $8 AND node_run_id = $9 AND status = 'running'
	`, input.JobID, response.Output.ArtifactID, response.Output.MediaFileID,
		response.Output.StorageKey, response.Output.MimeType,
		input.OrganizationID, input.ProjectID, input.WorkflowRunID, execution.NodeRunID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrWorkflowWriteFenced
	}
	applied, err := completeNodeRunTx(ctx, tx, execution, mustJSON(response.Output))
	if err != nil {
		return err
	}
	if !applied {
		return ErrWorkflowWriteFenced
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"commerce.direct_video.succeeded", "commerce_direct_video_job", input.JobID,
		mustJSON(map[string]any{
			"workflowRunId": input.WorkflowRunID, "nodeRunId": execution.NodeRunID,
			"artifactId": response.Output.ArtifactID, "mediaFileId": response.Output.MediaFileID,
			"commerceScriptUnitId": input.ScriptUnitID,
		})); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) failCommerceDirectVideoJob(
	ctx context.Context,
	input CommerceDirectVideoInput,
	execution NodeExecution,
	code string,
	message string,
) error {
	code = strings.TrimSpace(code)
	if code == "" {
		code = "COMMERCE_DIRECT_VIDEO_FAILED"
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "视频生成失败"
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE commerce_direct_video_jobs
		SET status = 'failed', error_code = $2, error_message = $3,
		    completed_at = now(), updated_at = now()
		WHERE id = $1 AND organization_id = $4 AND project_id = $5
		  AND script_unit_id = $6 AND workflow_run_id = $7
		  AND status IN ('queued', 'running', 'cancelling')
	`, input.JobID, code, message, input.OrganizationID, input.ProjectID,
		input.ScriptUnitID, input.WorkflowRunID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}
	if execution.valid() {
		if _, err := failNodeRunTx(ctx, tx, execution, code, message, mustJSON(map[string]any{"jobId": input.JobID})); err != nil {
			return err
		}
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"commerce.direct_video.failed", "commerce_direct_video_job", input.JobID,
		mustJSON(map[string]any{
			"workflowRunId": input.WorkflowRunID, "errorCode": code, "errorMessage": message,
			"commerceScriptUnitId": input.ScriptUnitID,
		})); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func commerceDirectVideoTaskState(job commerce.DirectVideoJob, execution NodeExecution) CommerceDirectVideoTaskState {
	state := CommerceDirectVideoTaskState{JobID: job.ID, Status: job.Status, NodeExecution: execution}
	state.ProviderRequestID = pointerString(job.ProviderRequestID)
	state.ProviderCallID = pointerString(job.ProviderCallID)
	state.ProviderAsyncTaskID = pointerString(job.ProviderAsyncTaskID)
	state.ExternalTaskID = pointerString(job.ExternalTaskID)
	if job.OutputArtifactID != nil {
		state.Output.ArtifactID = *job.OutputArtifactID
	}
	if job.OutputMediaFileID != nil {
		state.Output.MediaFileID = *job.OutputMediaFileID
	}
	if job.OutputStorageKey != nil {
		state.Output.StorageKey = *job.OutputStorageKey
	}
	if job.OutputMimeType != nil {
		state.Output.MimeType = *job.OutputMimeType
	}
	return state
}

func directVideoErrorCode(job commerce.DirectVideoJob) string {
	if job.ErrorCode != nil && strings.TrimSpace(*job.ErrorCode) != "" {
		return *job.ErrorCode
	}
	return "COMMERCE_DIRECT_VIDEO_FAILED"
}

func directVideoErrorMessage(job commerce.DirectVideoJob) string {
	if job.ErrorMessage != nil && strings.TrimSpace(*job.ErrorMessage) != "" {
		return *job.ErrorMessage
	}
	return "视频生成任务已失败"
}

func commerceDirectVideoErrorFields(err error) (string, string) {
	var applicationErr *temporal.ApplicationError
	if errors.As(err, &applicationErr) {
		code := strings.TrimSpace(applicationErr.Type())
		if code == "" {
			code = "COMMERCE_DIRECT_VIDEO_FAILED"
		}
		return code, strings.TrimSpace(applicationErr.Error())
	}
	return "COMMERCE_DIRECT_VIDEO_FAILED", strings.TrimSpace(err.Error())
}

func pointerString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func validateCommerceDirectVideoWorkflowJob(input CommerceDirectVideoInput, job commerce.DirectVideoJob) error {
	if strings.TrimSpace(job.ID) != strings.TrimSpace(input.JobID) ||
		job.OrganizationID != strings.TrimSpace(input.OrganizationID) ||
		job.ProjectID != strings.TrimSpace(input.ProjectID) ||
		job.ScriptUnitID != strings.TrimSpace(input.ScriptUnitID) ||
		pointerString(job.WorkflowRunID) != strings.TrimSpace(input.WorkflowRunID) ||
		job.AttemptGeneration != input.AttemptGeneration {
		return ErrWorkflowWriteFenced
	}
	return nil
}
