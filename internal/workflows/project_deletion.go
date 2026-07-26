package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/events"
	"github.com/Einzieg/cineweave/internal/projectdeletion"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ProjectDeletionWorkflowName               = "ProjectDeletionWorkflow"
	PrepareProjectDeletionActivityName        = "PrepareProjectDeletion"
	CancelProjectProviderTasksActivityName    = "CancelProjectProviderTasks"
	CheckProjectDeletionDrainActivityName     = "CheckProjectDeletionDrain"
	BuildProjectDeletionManifestActivityName  = "BuildProjectDeletionManifest"
	DeleteProjectStorageBatchActivityName     = "DeleteProjectStorageBatch"
	CommitProjectDeletionActivityName         = "CommitProjectDeletion"
	FailProjectDeletionActivityName           = "FailProjectDeletion"
	projectDeletionStorageBatchSize           = 32
	projectDeletionRequestRetention           = 7 * 24 * time.Hour
	projectDeletionPollInterval               = 5 * time.Second
	projectDeletionStoragePollInterval        = 2 * time.Second
	projectDeletionStorageClaimLease          = 5 * time.Minute
	CodeProjectDeletionDrainTimeout           = "PROJECT_DELETION_DRAIN_TIMEOUT"
	CodeProjectDeletionStorageFailed          = "PROJECT_DELETION_STORAGE_FAILED"
	CodeProjectDeletionFailed                 = "PROJECT_DELETION_FAILED"
	projectDeletionStatusRequested            = "requested"
	projectDeletionStatusCancellingTasks      = "cancelling_tasks"
	projectDeletionStatusWaitingForTerminal   = "waiting_for_terminal"
	projectDeletionStatusDeletingStorage      = "deleting_storage"
	projectDeletionStatusDeletingBusinessData = "deleting_business_data"
	projectDeletionStatusCompleted            = "completed"
	projectDeletionStatusFailedRetryable      = "failed_retryable"
	projectDeletionStatusFailedTerminal       = "failed_terminal"
)

type ProjectDeletionInput struct {
	OrganizationID   string `json:"organizationId"`
	WorkspaceID      string `json:"workspaceId"`
	ProjectID        string `json:"projectId"`
	RequestID        string `json:"requestId"`
	DeletionRevision int64  `json:"deletionRevision"`
	RequestedBy      string `json:"requestedBy"`
}

type ProjectDeletionExternalWorkflow struct {
	TemporalWorkflowID string `json:"temporalWorkflowId"`
}

type PrepareProjectDeletionOutput struct {
	ExternalWorkflows []ProjectDeletionExternalWorkflow `json:"externalWorkflows"`
	DrainDeadlineAt   time.Time                         `json:"drainDeadlineAt"`
}

type ProjectDeletionDrainOutput struct {
	Drained                 bool `json:"drained"`
	ActiveWorkflowCount     int  `json:"activeWorkflowCount"`
	ActiveAgentTaskCount    int  `json:"activeAgentTaskCount"`
	ActiveSetupRunCount     int  `json:"activeSetupRunCount"`
	ActiveProviderTaskCount int  `json:"activeProviderTaskCount"`
	ActiveOutboxCount       int  `json:"activeOutboxCount"`
}

type ProjectDeletionManifestOutput struct {
	ObjectCount int   `json:"objectCount"`
	ByteSize    int64 `json:"byteSize"`
}

type ProjectDeletionStorageBatchOutput struct {
	SelectedCount      int `json:"selectedCount"`
	DeletedCount       int `json:"deletedCount"`
	SkippedSharedCount int `json:"skippedSharedCount"`
	FailedCount        int `json:"failedCount"`
	PendingCount       int `json:"pendingCount"`
	InFlightCount      int `json:"inFlightCount"`
	TotalFailedCount   int `json:"totalFailedCount"`
}

type ProjectDeletionOutput struct {
	RequestID          string `json:"requestId"`
	ProjectID          string `json:"projectId"`
	Status             string `json:"status"`
	DeletedObjectCount int    `json:"deletedObjectCount"`
	SkippedObjectCount int    `json:"skippedObjectCount"`
}

type projectDeletionStorage interface {
	DeleteObject(context.Context, string) error
}

func ProjectDeletionWorkflow(ctx workflow.Context, input ProjectDeletionInput) (output ProjectDeletionOutput, resultErr error) {
	activityCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	defer func() {
		if resultErr == nil {
			return
		}
		failureCtx, _ := workflow.NewDisconnectedContext(ctx)
		failureCtx = workflow.WithActivityOptions(failureCtx, defaultActivityOptions())
		_ = workflow.ExecuteActivity(
			failureCtx,
			FailProjectDeletionActivityName,
			input,
			CodeProjectDeletionFailed,
			resultErr.Error(),
			true,
		).Get(failureCtx, nil)
	}()

	var preparation PrepareProjectDeletionOutput
	if err := workflow.ExecuteActivity(activityCtx, PrepareProjectDeletionActivityName, input).Get(activityCtx, &preparation); err != nil {
		return output, err
	}
	for _, external := range preparation.ExternalWorkflows {
		if strings.TrimSpace(external.TemporalWorkflowID) == "" {
			continue
		}
		_ = workflow.RequestCancelExternalWorkflow(ctx, external.TemporalWorkflowID, "").Get(ctx, nil)
	}
	if err := workflow.ExecuteActivity(activityCtx, CancelProjectProviderTasksActivityName, input).Get(activityCtx, nil); err != nil {
		return output, err
	}

	for {
		var drain ProjectDeletionDrainOutput
		if err := workflow.ExecuteActivity(activityCtx, CheckProjectDeletionDrainActivityName, input).Get(activityCtx, &drain); err != nil {
			return output, err
		}
		if drain.Drained {
			break
		}
		if !workflow.Now(ctx).Before(preparation.DrainDeadlineAt) {
			message := fmt.Sprintf(
				"项目仍有活动任务：工作流 %d、助手任务 %d、初始化任务 %d、供应商任务 %d、待启动任务 %d",
				drain.ActiveWorkflowCount,
				drain.ActiveAgentTaskCount,
				drain.ActiveSetupRunCount,
				drain.ActiveProviderTaskCount,
				drain.ActiveOutboxCount,
			)
			_ = workflow.ExecuteActivity(
				activityCtx,
				FailProjectDeletionActivityName,
				input,
				CodeProjectDeletionDrainTimeout,
				message,
				true,
			).Get(activityCtx, nil)
			return output, temporal.NewNonRetryableApplicationError(message, CodeProjectDeletionDrainTimeout, nil)
		}
		if err := workflow.Sleep(ctx, projectDeletionPollInterval); err != nil {
			return output, err
		}
	}

	if err := workflow.ExecuteActivity(activityCtx, BuildProjectDeletionManifestActivityName, input).Get(activityCtx, nil); err != nil {
		return output, err
	}
	storageOptions := defaultActivityOptions()
	storageOptions.StartToCloseTimeout = 10 * time.Minute
	storageOptions.HeartbeatTimeout = 2 * time.Minute
	if storageOptions.RetryPolicy != nil {
		storageOptions.RetryPolicy.MaximumAttempts = 5
	}
	storageCtx := workflow.WithActivityOptions(ctx, storageOptions)
	for {
		var batch ProjectDeletionStorageBatchOutput
		if err := workflow.ExecuteActivity(storageCtx, DeleteProjectStorageBatchActivityName, input, projectDeletionStorageBatchSize).Get(storageCtx, &batch); err != nil {
			return output, err
		}
		if batch.PendingCount > 0 || batch.InFlightCount > 0 {
			if err := workflow.Sleep(ctx, projectDeletionStoragePollInterval); err != nil {
				return output, err
			}
			continue
		}
		if batch.TotalFailedCount > 0 {
			message := fmt.Sprintf("对象存储仍有 %d 个文件删除失败，可重试当前删除请求", batch.TotalFailedCount)
			_ = workflow.ExecuteActivity(
				activityCtx,
				FailProjectDeletionActivityName,
				input,
				CodeProjectDeletionStorageFailed,
				message,
				true,
			).Get(activityCtx, nil)
			return output, temporal.NewNonRetryableApplicationError(message, CodeProjectDeletionStorageFailed, nil)
		}
		break
	}
	if err := workflow.ExecuteActivity(activityCtx, CommitProjectDeletionActivityName, input).Get(activityCtx, &output); err != nil {
		return output, err
	}
	return output, nil
}

func (a Activities) PrepareProjectDeletion(ctx context.Context, input ProjectDeletionInput) (PrepareProjectDeletionOutput, error) {
	if err := validateProjectDeletionInput(input); err != nil {
		return PrepareProjectDeletionOutput{}, err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return PrepareProjectDeletionOutput{}, err
	}
	defer tx.Rollback(ctx)

	var status string
	var retryCount int
	var drainDeadline time.Time
	if err := tx.QueryRow(ctx, `
		SELECT request.status, request.retry_count, request.drain_deadline_at
		FROM project_deletion_requests request
		JOIN projects project
		  ON project.id = request.project_id
		 AND project.organization_id = request.organization_id
		 AND project.workspace_id = request.workspace_id
		WHERE request.id = $1
		  AND request.organization_id = $2
		  AND request.project_id = $3
		  AND request.deletion_revision = $4
		  AND project.lifecycle_status = 'deleting'
		  AND project.deletion_revision = request.deletion_revision
		FOR UPDATE OF request, project
	`, input.RequestID, input.OrganizationID, input.ProjectID, input.DeletionRevision).Scan(
		&status,
		&retryCount,
		&drainDeadline,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PrepareProjectDeletionOutput{}, ErrWorkflowWriteFenced
		}
		return PrepareProjectDeletionOutput{}, err
	}
	if status == projectDeletionStatusCompleted || status == projectDeletionStatusFailedTerminal {
		return PrepareProjectDeletionOutput{}, ErrWorkflowWriteFenced
	}
	if status == projectDeletionStatusFailedRetryable ||
		(status == projectDeletionStatusRequested && retryCount > 0) {
		if _, err := tx.Exec(ctx, `
			UPDATE project_deletion_objects
			SET status = 'pending',
			    claim_token = NULL,
			    claim_expires_at = NULL,
			    error_code = NULL,
			    error_message = NULL,
			    updated_at = now()
			WHERE request_id = $1 AND status IN ('failed', 'deleting')
		`, input.RequestID); err != nil {
			return PrepareProjectDeletionOutput{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_deletion_requests
		SET status = 'cancelling_tasks',
		    started_at = COALESCE(started_at, now()),
		    error_code = NULL,
		    error_message = NULL,
		    updated_at = now()
		WHERE id = $1
	`, input.RequestID); err != nil {
		return PrepareProjectDeletionOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_start_outbox
		SET status = 'cancelled',
		    completed_at = now(),
		    last_error_code = 'PROJECT_DELETION_IN_PROGRESS',
		    last_error_message = '项目正在删除',
		    locked_at = NULL,
		    locked_by = NULL,
		    updated_at = now()
		WHERE project_id = $1
		  AND project_deletion_request_id IS DISTINCT FROM $2
		  AND status = 'pending'
	`, input.ProjectID, input.RequestID); err != nil {
		return PrepareProjectDeletionOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'cancelled',
		    error_code = 'PROJECT_DELETION_IN_PROGRESS',
		    error_message = '项目正在删除',
		    completed_at = now(),
		    cancelled_at = now(),
		    terminalized_at = now(),
		    settled_at = now(),
		    revision = revision + 1,
		    updated_at = now()
		WHERE project_id = $1 AND status IN ('pending', 'queued')
	`, input.ProjectID); err != nil {
		return PrepareProjectDeletionOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE agent_tasks
		SET status = 'cancelled',
		    error_code = 'PROJECT_DELETION_IN_PROGRESS',
		    error_message = '项目正在删除',
		    completed_at = now(),
		    updated_at = now()
		WHERE project_id = $1 AND status IN ('queued', 'planning', 'waiting_approval')
	`, input.ProjectID); err != nil {
		return PrepareProjectDeletionOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_setup_runs
		SET status = 'cancelled',
		    error_code = 'PROJECT_DELETION_IN_PROGRESS',
		    error_message = '项目正在删除',
		    completed_at = now(),
		    revision = revision + 1,
		    updated_at = now()
		WHERE project_id = $1 AND status = 'queued'
	`, input.ProjectID); err != nil {
		return PrepareProjectDeletionOutput{}, err
	}

	rows, err := tx.Query(ctx, `
		SELECT temporal_workflow_id
		FROM (
			SELECT temporal_workflow_id
			FROM workflow_runs
			WHERE project_id = $1
			  AND status IN ('running', 'cancelling', 'waiting_review')
			UNION
			SELECT temporal_workflow_id
			FROM agent_tasks
			WHERE project_id = $1
			  AND status = 'running'
			  AND temporal_workflow_id IS NOT NULL
			UNION
			SELECT temporal_workflow_id
			FROM commerce_setup_runs
			WHERE project_id = $1
			  AND status IN ('running', 'waiting_user_confirmation', 'needs_user_review')
		) active
		WHERE btrim(temporal_workflow_id) <> ''
		ORDER BY temporal_workflow_id
	`, input.ProjectID)
	if err != nil {
		return PrepareProjectDeletionOutput{}, err
	}
	external := make([]ProjectDeletionExternalWorkflow, 0)
	for rows.Next() {
		var temporalWorkflowID string
		if err := rows.Scan(&temporalWorkflowID); err != nil {
			rows.Close()
			return PrepareProjectDeletionOutput{}, err
		}
		external = append(external, ProjectDeletionExternalWorkflow{TemporalWorkflowID: temporalWorkflowID})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return PrepareProjectDeletionOutput{}, err
	}
	if err := appendProjectDeletionEvent(
		ctx,
		tx,
		input,
		"project.deletion.tasks_cancelling",
		projectDeletionStatusCancellingTasks,
		map[string]any{"externalWorkflowCount": len(external)},
	); err != nil {
		return PrepareProjectDeletionOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PrepareProjectDeletionOutput{}, err
	}
	return PrepareProjectDeletionOutput{
		ExternalWorkflows: external,
		DrainDeadlineAt:   drainDeadline,
	}, nil
}

func (a Activities) CancelProjectProviderTasks(ctx context.Context, input ProjectDeletionInput) error {
	if err := validateProjectDeletionInput(input); err != nil {
		return err
	}
	rows, err := a.db.Query(ctx, `
		SELECT id::text, COALESCE(external_task_id, '')
		FROM provider_async_tasks
		WHERE organization_id = $1 AND project_id = $2
		  AND status IN ('queued', 'running', 'cancelling')
		ORDER BY created_at, id
	`, input.OrganizationID, input.ProjectID)
	if err != nil {
		return err
	}
	type task struct {
		id         string
		externalID string
	}
	tasks := make([]task, 0)
	for rows.Next() {
		var item task
		if err := rows.Scan(&item.id, &item.externalID); err != nil {
			rows.Close()
			return err
		}
		tasks = append(tasks, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(tasks) > 0 && a.gateway == nil {
		return errors.New("provider gateway client is not configured")
	}
	for index, item := range tasks {
		activity.RecordHeartbeat(ctx, map[string]any{
			"projectId": input.ProjectID,
			"current":   index + 1,
			"total":     len(tasks),
		})
		if _, err := a.gateway.CancelVideoTask(ctx, provider.GatewayVideoCancelTaskRequest{
			OrganizationID:      input.OrganizationID,
			ProviderAsyncTaskID: item.id,
			ExternalTaskID:      item.externalID,
			IdempotencyKey:      "project-deletion-" + input.RequestID + "-" + item.id,
		}); err != nil {
			return fmt.Errorf("cancel provider task %s: %w", item.id, err)
		}
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	command, err := tx.Exec(ctx, `
		UPDATE project_deletion_requests
		SET status = 'waiting_for_terminal', updated_at = now()
		WHERE id = $1
		  AND project_id = $2
		  AND deletion_revision = $3
		  AND status IN ('cancelling_tasks', 'waiting_for_terminal')
	`, input.RequestID, input.ProjectID, input.DeletionRevision)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrWorkflowWriteFenced
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func (a Activities) CheckProjectDeletionDrain(ctx context.Context, input ProjectDeletionInput) (ProjectDeletionDrainOutput, error) {
	var output ProjectDeletionDrainOutput
	if err := a.db.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM workflow_runs
			 WHERE project_id = request.project_id
			   AND status IN ('pending', 'queued', 'running', 'cancelling', 'waiting_review')),
			(SELECT count(*) FROM agent_tasks
			 WHERE project_id = request.project_id
			   AND status IN ('queued', 'planning', 'waiting_approval', 'running')),
			(SELECT count(*) FROM commerce_setup_runs
			 WHERE project_id = request.project_id
			   AND status IN ('queued', 'running', 'waiting_user_confirmation', 'needs_user_review')),
			(SELECT count(*) FROM provider_async_tasks
			 WHERE project_id = request.project_id
			   AND status IN ('queued', 'running', 'cancelling')),
			(SELECT count(*) FROM workflow_start_outbox
			 WHERE project_id = request.project_id
			   AND project_deletion_request_id IS DISTINCT FROM request.id
			   AND status IN ('pending', 'processing'))
		FROM project_deletion_requests request
		JOIN projects project
		  ON project.id = request.project_id
		 AND project.lifecycle_status = 'deleting'
		 AND project.deletion_revision = request.deletion_revision
		WHERE request.id = $1
		  AND request.project_id = $2
		  AND request.deletion_revision = $3
		  AND request.status IN ('cancelling_tasks', 'waiting_for_terminal')
	`, input.RequestID, input.ProjectID, input.DeletionRevision).Scan(
		&output.ActiveWorkflowCount,
		&output.ActiveAgentTaskCount,
		&output.ActiveSetupRunCount,
		&output.ActiveProviderTaskCount,
		&output.ActiveOutboxCount,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProjectDeletionDrainOutput{}, ErrWorkflowWriteFenced
		}
		return ProjectDeletionDrainOutput{}, err
	}
	output.Drained = output.ActiveWorkflowCount == 0 &&
		output.ActiveAgentTaskCount == 0 &&
		output.ActiveSetupRunCount == 0 &&
		output.ActiveProviderTaskCount == 0 &&
		output.ActiveOutboxCount == 0
	return output, nil
}

func (a Activities) BuildProjectDeletionManifest(ctx context.Context, input ProjectDeletionInput) (ProjectDeletionManifestOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ProjectDeletionManifestOutput{}, err
	}
	defer tx.Rollback(ctx)
	if err := assertProjectDeletionWritableTx(ctx, tx, input, []string{
		projectDeletionStatusWaitingForTerminal,
		projectDeletionStatusDeletingStorage,
	}); err != nil {
		return ProjectDeletionManifestOutput{}, err
	}
	query := `
		INSERT INTO project_deletion_objects(
			request_id, project_id, source_kind, storage_key, byte_size
		)
		SELECT $1, $2, min(candidate.source_kind), candidate.storage_key, max(candidate.byte_size)
		FROM (` + projectdeletion.StorageCandidateUnion("$2") + `) candidate
		GROUP BY candidate.storage_key
		ON CONFLICT (request_id, storage_key) DO UPDATE SET
			byte_size = COALESCE(project_deletion_objects.byte_size, EXCLUDED.byte_size),
			updated_at = now()
	`
	if _, err := tx.Exec(ctx, query, input.RequestID, input.ProjectID); err != nil {
		return ProjectDeletionManifestOutput{}, err
	}
	var output ProjectDeletionManifestOutput
	if err := tx.QueryRow(ctx, `
		SELECT count(*), COALESCE(sum(byte_size), 0)
		FROM project_deletion_objects
		WHERE request_id = $1
	`, input.RequestID).Scan(&output.ObjectCount, &output.ByteSize); err != nil {
		return ProjectDeletionManifestOutput{}, err
	}
	command, err := tx.Exec(ctx, `
		UPDATE project_deletion_requests
		SET status = 'deleting_storage',
		    storage_object_count = $2,
		    storage_deleted_count = (
		        SELECT count(*) FROM project_deletion_objects
		        WHERE request_id = $1 AND status = 'deleted'
		    ),
		    storage_failed_count = (
		        SELECT count(*) FROM project_deletion_objects
		        WHERE request_id = $1 AND status = 'failed'
		    ),
		    storage_skipped_shared_count = (
		        SELECT count(*) FROM project_deletion_objects
		        WHERE request_id = $1 AND status = 'skipped_shared'
		    ),
		    updated_at = now()
		WHERE id = $1
	`, input.RequestID, output.ObjectCount)
	if err != nil {
		return ProjectDeletionManifestOutput{}, err
	}
	if command.RowsAffected() != 1 {
		return ProjectDeletionManifestOutput{}, ErrWorkflowWriteFenced
	}
	if err := appendProjectDeletionEvent(
		ctx,
		tx,
		input,
		"project.deletion.storage_started",
		projectDeletionStatusDeletingStorage,
		map[string]any{"storageObjectCount": output.ObjectCount, "storageByteSize": output.ByteSize},
	); err != nil {
		return ProjectDeletionManifestOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectDeletionManifestOutput{}, err
	}
	return output, nil
}

func (a Activities) DeleteProjectStorageBatch(ctx context.Context, input ProjectDeletionInput, batchSize int) (ProjectDeletionStorageBatchOutput, error) {
	if batchSize <= 0 || batchSize > 128 {
		batchSize = projectDeletionStorageBatchSize
	}
	deleter, ok := a.storage.(projectDeletionStorage)
	if !ok || deleter == nil {
		return ProjectDeletionStorageBatchOutput{}, errors.New("storage client does not support object deletion")
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ProjectDeletionStorageBatchOutput{}, err
	}
	defer tx.Rollback(ctx)
	if err := assertProjectDeletionWritableTx(ctx, tx, input, []string{projectDeletionStatusDeletingStorage}); err != nil {
		return ProjectDeletionStorageBatchOutput{}, err
	}
	claimToken := uuid.NewString()
	rows, err := tx.Query(ctx, `
		WITH candidates AS (
			SELECT id
			FROM project_deletion_objects
			WHERE request_id = $1
			  AND (
			      status = 'pending'
			      OR (
			          status = 'deleting'
			          AND claim_expires_at <= now()
			      )
			  )
			ORDER BY id
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		)
		UPDATE project_deletion_objects object
		SET status = 'deleting',
		    claim_token = $3,
		    claim_expires_at = now() + $4::interval,
		    attempt_count = object.attempt_count + 1,
		    error_code = NULL,
		    error_message = NULL,
		    updated_at = now()
		FROM candidates
		WHERE object.id = candidates.id
		RETURNING object.id, object.storage_key
	`, input.RequestID, batchSize, claimToken, projectDeletionStorageClaimLease.String())
	if err != nil {
		return ProjectDeletionStorageBatchOutput{}, err
	}
	type deletionObject struct {
		id  int64
		key string
	}
	objects := make([]deletionObject, 0, batchSize)
	for rows.Next() {
		var item deletionObject
		if err := rows.Scan(&item.id, &item.key); err != nil {
			rows.Close()
			return ProjectDeletionStorageBatchOutput{}, err
		}
		objects = append(objects, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return ProjectDeletionStorageBatchOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectDeletionStorageBatchOutput{}, err
	}

	output := ProjectDeletionStorageBatchOutput{SelectedCount: len(objects)}
	for index, item := range objects {
		activity.RecordHeartbeat(ctx, map[string]any{
			"requestId":  input.RequestID,
			"current":    index + 1,
			"total":      len(objects),
			"storageKey": item.key,
		})
		shared, err := a.projectDeletionStorageKeyShared(ctx, input.ProjectID, item.key)
		switch {
		case err != nil:
			updated, updateErr := a.finishProjectDeletionObject(
				ctx,
				item.id,
				claimToken,
				"failed",
				"PROJECT_DELETION_OWNERSHIP_CHECK_FAILED",
				err.Error(),
			)
			if updateErr != nil {
				return ProjectDeletionStorageBatchOutput{}, updateErr
			}
			if updated {
				output.FailedCount++
			}
		case shared:
			updated, err := a.finishProjectDeletionObject(ctx, item.id, claimToken, "skipped_shared", "", "")
			if err != nil {
				return ProjectDeletionStorageBatchOutput{}, err
			}
			if updated {
				output.SkippedSharedCount++
			}
		case !shared:
			if err := deleter.DeleteObject(ctx, item.key); err != nil {
				updated, updateErr := a.finishProjectDeletionObject(
					ctx,
					item.id,
					claimToken,
					"failed",
					CodeProjectDeletionStorageFailed,
					err.Error(),
				)
				if updateErr != nil {
					return ProjectDeletionStorageBatchOutput{}, updateErr
				}
				if updated {
					output.FailedCount++
				}
				continue
			}
			updated, err := a.finishProjectDeletionObject(ctx, item.id, claimToken, "deleted", "", "")
			if err != nil {
				return ProjectDeletionStorageBatchOutput{}, err
			}
			if updated {
				output.DeletedCount++
			}
		}
	}
	tx, err = a.db.Begin(ctx)
	if err != nil {
		return ProjectDeletionStorageBatchOutput{}, err
	}
	defer tx.Rollback(ctx)
	if err := tx.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE status = 'pending'),
			count(*) FILTER (WHERE status = 'deleting'),
			count(*) FILTER (WHERE status = 'failed')
		FROM project_deletion_objects
		WHERE request_id = $1
	`, input.RequestID).Scan(&output.PendingCount, &output.InFlightCount, &output.TotalFailedCount); err != nil {
		return ProjectDeletionStorageBatchOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_deletion_requests
		SET manifest_cursor = COALESCE((
		        SELECT max(id) FROM project_deletion_objects
		        WHERE request_id = $1 AND status IN ('deleted', 'skipped_shared', 'failed')
		    ), manifest_cursor),
		    storage_deleted_count = (
		        SELECT count(*) FROM project_deletion_objects
		        WHERE request_id = $1 AND status = 'deleted'
		    ),
		    storage_failed_count = (
		        SELECT count(*) FROM project_deletion_objects
		        WHERE request_id = $1 AND status = 'failed'
		    ),
		    storage_skipped_shared_count = (
		        SELECT count(*) FROM project_deletion_objects
		        WHERE request_id = $1 AND status = 'skipped_shared'
		    ),
		    updated_at = now()
		WHERE id = $1
	`, input.RequestID); err != nil {
		return ProjectDeletionStorageBatchOutput{}, err
	}
	if len(objects) > 0 {
		if err := appendProjectDeletionEvent(
			ctx,
			tx,
			input,
			"project.deletion.storage_progress",
			projectDeletionStatusDeletingStorage,
			map[string]any{
				"selectedCount":      output.SelectedCount,
				"deletedCount":       output.DeletedCount,
				"skippedSharedCount": output.SkippedSharedCount,
				"failedCount":        output.FailedCount,
				"pendingCount":       output.PendingCount,
				"inFlightCount":      output.InFlightCount,
				"totalFailedCount":   output.TotalFailedCount,
			},
		); err != nil {
			return ProjectDeletionStorageBatchOutput{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectDeletionStorageBatchOutput{}, err
	}
	return output, nil
}

func (a Activities) CommitProjectDeletion(ctx context.Context, input ProjectDeletionInput) (ProjectDeletionOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ProjectDeletionOutput{}, err
	}
	defer tx.Rollback(ctx)
	if err := assertProjectDeletionWritableTx(ctx, tx, input, []string{projectDeletionStatusDeletingStorage}); err != nil {
		return ProjectDeletionOutput{}, err
	}
	var pendingOrFailed int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM project_deletion_objects
		WHERE request_id = $1 AND status IN ('pending', 'deleting', 'failed')
	`, input.RequestID).Scan(&pendingOrFailed); err != nil {
		return ProjectDeletionOutput{}, err
	}
	if pendingOrFailed > 0 {
		return ProjectDeletionOutput{}, fmt.Errorf("project deletion manifest still has %d unfinished objects", pendingOrFailed)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_deletion_requests
		SET status = 'deleting_business_data', updated_at = now()
		WHERE id = $1
	`, input.RequestID); err != nil {
		return ProjectDeletionOutput{}, err
	}
	if err := appendProjectDeletionEvent(
		ctx,
		tx,
		input,
		"project.deletion.business_data_started",
		projectDeletionStatusDeletingBusinessData,
		nil,
	); err != nil {
		return ProjectDeletionOutput{}, err
	}
	if _, err := tx.Exec(ctx, `SET CONSTRAINTS ALL DEFERRED`); err != nil {
		return ProjectDeletionOutput{}, err
	}
	command, err := tx.Exec(ctx, `
		DELETE FROM projects
		WHERE id = $1
		  AND organization_id = $2
		  AND lifecycle_status = 'deleting'
		  AND deletion_revision = $3
	`, input.ProjectID, input.OrganizationID, input.DeletionRevision)
	if err != nil {
		return ProjectDeletionOutput{}, err
	}
	if command.RowsAffected() != 1 {
		return ProjectDeletionOutput{}, ErrWorkflowWriteFenced
	}
	var output ProjectDeletionOutput
	output.RequestID = input.RequestID
	output.ProjectID = input.ProjectID
	output.Status = projectDeletionStatusCompleted
	if err := tx.QueryRow(ctx, `
		UPDATE project_deletion_requests
		SET status = 'completed',
		    completed_at = now(),
		    expires_at = now() + $2::interval,
		    error_code = NULL,
		    error_message = NULL,
		    updated_at = now()
		WHERE id = $1
		RETURNING storage_deleted_count, storage_skipped_shared_count
	`, input.RequestID, projectDeletionRequestRetention.String()).Scan(
		&output.DeletedObjectCount,
		&output.SkippedObjectCount,
	); err != nil {
		return ProjectDeletionOutput{}, err
	}
	if err := appendProjectDeletionEvent(
		ctx,
		tx,
		input,
		"project.deletion.completed",
		projectDeletionStatusCompleted,
		map[string]any{
			"deletedObjectCount": output.DeletedObjectCount,
			"skippedObjectCount": output.SkippedObjectCount,
		},
	); err != nil {
		return ProjectDeletionOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectDeletionOutput{}, err
	}
	return output, nil
}

func (a Activities) FailProjectDeletion(ctx context.Context, input ProjectDeletionInput, code, message string, retryable bool) error {
	status := projectDeletionStatusFailedTerminal
	if retryable {
		status = projectDeletionStatusFailedRetryable
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var currentStatus string
	if err := tx.QueryRow(ctx, `
		SELECT status
		FROM project_deletion_requests
		WHERE id = $1 AND project_id = $2 AND deletion_revision = $3
		FOR UPDATE
	`, input.RequestID, input.ProjectID, input.DeletionRevision).Scan(&currentStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if currentStatus == projectDeletionStatusCompleted ||
		currentStatus == projectDeletionStatusFailedTerminal ||
		currentStatus == projectDeletionStatusFailedRetryable {
		return tx.Commit(ctx)
	}
	expiresAt := any(nil)
	completedAt := any(nil)
	if status == projectDeletionStatusFailedTerminal {
		completedAt = time.Now().UTC()
		expiresAt = time.Now().UTC().Add(projectDeletionRequestRetention)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_deletion_requests
		SET status = $2,
		    error_code = $3,
		    error_message = $4,
		    completed_at = $5,
		    expires_at = $6,
		    updated_at = now()
		WHERE id = $1
	`, input.RequestID, status, strings.TrimSpace(code), strings.TrimSpace(message), completedAt, expiresAt); err != nil {
		return err
	}
	eventName := "project.deletion.failed"
	if code == CodeProjectDeletionDrainTimeout {
		eventName = "project.deletion.drain_timeout"
	}
	if err := appendProjectDeletionEvent(
		ctx,
		tx,
		input,
		eventName,
		status,
		map[string]any{"errorCode": code, "errorMessage": message, "retryable": retryable},
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) finishProjectDeletionObject(
	ctx context.Context,
	id int64,
	claimToken string,
	status string,
	code string,
	message string,
) (bool, error) {
	terminalAt := any(nil)
	if status == "deleted" || status == "skipped_shared" {
		terminalAt = time.Now().UTC()
	}
	command, err := a.db.Exec(ctx, `
		UPDATE project_deletion_objects
		SET status = $2,
		    claim_token = NULL,
		    claim_expires_at = NULL,
		    error_code = NULLIF($4, ''),
		    error_message = NULLIF($5, ''),
		    deleted_at = $6,
		    updated_at = now()
		WHERE id = $1
		  AND status = 'deleting'
		  AND claim_token = $3
	`, id, status, claimToken, code, message, terminalAt)
	if err != nil {
		return false, err
	}
	return command.RowsAffected() == 1, nil
}

func (a Activities) projectDeletionStorageKeyShared(ctx context.Context, projectID, storageKey string) (bool, error) {
	var shared bool
	err := a.db.QueryRow(
		ctx,
		projectdeletion.SharedStorageReferenceQuery("$1", "$2"),
		storageKey,
		projectID,
	).Scan(&shared)
	return shared, err
}

func validateProjectDeletionInput(input ProjectDeletionInput) error {
	if strings.TrimSpace(input.OrganizationID) == "" ||
		strings.TrimSpace(input.WorkspaceID) == "" ||
		strings.TrimSpace(input.ProjectID) == "" ||
		strings.TrimSpace(input.RequestID) == "" ||
		input.DeletionRevision <= 0 {
		return errors.New("organizationId, workspaceId, projectId, requestId, and deletionRevision are required")
	}
	for _, value := range []string{input.OrganizationID, input.WorkspaceID, input.ProjectID, input.RequestID} {
		if _, err := uuid.Parse(value); err != nil {
			return fmt.Errorf("invalid project deletion identity %q: %w", value, err)
		}
	}
	return nil
}

func assertProjectDeletionWritableTx(ctx context.Context, tx pgx.Tx, input ProjectDeletionInput, statuses []string) error {
	var requestID string
	if err := tx.QueryRow(ctx, `
		SELECT request.id::text
		FROM project_deletion_requests request
		JOIN projects project
		  ON project.id = request.project_id
		 AND project.organization_id = request.organization_id
		 AND project.lifecycle_status = 'deleting'
		 AND project.deletion_revision = request.deletion_revision
		WHERE request.id = $1
		  AND request.organization_id = $2
		  AND request.project_id = $3
		  AND request.deletion_revision = $4
		  AND request.status = ANY($5::text[])
		FOR UPDATE OF request, project
	`, input.RequestID, input.OrganizationID, input.ProjectID, input.DeletionRevision, statuses).Scan(&requestID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrWorkflowWriteFenced
		}
		return err
	}
	if requestID != input.RequestID {
		return ErrWorkflowWriteFenced
	}
	return nil
}

func appendProjectDeletionEvent(
	ctx context.Context,
	tx pgx.Tx,
	input ProjectDeletionInput,
	eventName string,
	status string,
	extra map[string]any,
) error {
	payload := map[string]any{
		"projectDeletionRequestId": input.RequestID,
		"projectId":                input.ProjectID,
		"deletionRevision":         input.DeletionRevision,
		"status":                   status,
	}
	for key, value := range extra {
		payload[key] = value
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return events.AppendTx(
		ctx,
		tx,
		input.OrganizationID,
		"",
		eventName,
		"project_deletion_request",
		input.RequestID,
		raw,
	)
}

func PurgeExpiredProjectDeletionRequests(ctx context.Context, db interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, batchSize int) (int64, error) {
	if batchSize <= 0 || batchSize > 1000 {
		batchSize = 100
	}
	command, err := db.Exec(ctx, `
		DELETE FROM project_deletion_requests
		WHERE id IN (
			SELECT id
			FROM project_deletion_requests
			WHERE expires_at IS NOT NULL AND expires_at <= now()
			ORDER BY expires_at
			LIMIT $1
		)
	`, batchSize)
	if err != nil {
		return 0, err
	}
	return command.RowsAffected(), nil
}
