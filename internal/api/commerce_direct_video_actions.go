package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/google/uuid"
)

func (s *Server) executeCommerceDirectVideoGenerateAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	arguments, err := decodeCommerceActionMap(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	scriptUnitID, err := s.resolveCommerceActionScriptUnitID(
		ctx, project, arguments, "scriptUnitId", true,
	)
	if err != nil {
		return agentToolResult{}, err
	}
	var input commercepkg.CreateDirectVideoJobInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return agentToolResult{}, controlValidationError("视频生成参数无效")
	}
	job, replayed, err := s.createCommerceDirectVideoCore(
		ctx, principal, project, scriptUnitID, input, command.ID,
	)
	if err != nil {
		return agentToolResult{}, err
	}
	data, err := agentCommerceValueData(job)
	if err != nil {
		return agentToolResult{}, err
	}
	data["idempotentReplay"] = replayed
	if job.WorkflowRunID != nil && strings.TrimSpace(*job.WorkflowRunID) != "" {
		data["workflowRunIds"] = []string{strings.TrimSpace(*job.WorkflowRunID)}
	}
	summary := "带货视频生成任务已创建"
	if replayed {
		summary = "带货视频生成任务已存在，未重复创建"
	}
	return agentToolOK("commerce.video.generate", arguments, summary, data), nil
}

func (s *Server) createCommerceDirectVideoCore(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	scriptUnitID string,
	input commercepkg.CreateDirectVideoJobInput,
	idempotencyKey string,
) (commercepkg.DirectVideoJob, bool, error) {
	scriptUnitID = strings.TrimSpace(scriptUnitID)
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId": project.ID, "scriptUnitId": scriptUnitID, "input": input,
	})
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return commercepkg.DirectVideoJob{}, false, err
	}
	defer tx.Rollback(ctx)
	claim, err := claimIdempotencyTx(
		ctx, tx, project.OrganizationID,
		"commerce_direct_video:create:"+scriptUnitID, idempotencyKey, requestHash,
	)
	if err != nil {
		return commercepkg.DirectVideoJob{}, false, err
	}
	if len(claim.replaySnapshot) > 0 {
		var replay commercepkg.DirectVideoJob
		if err := json.Unmarshal(claim.replaySnapshot, &replay); err != nil {
			return commercepkg.DirectVideoJob{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return commercepkg.DirectVideoJob{}, false, err
		}
		return replay, true, nil
	}
	jobID := uuid.NewString()
	workflowRunID := uuid.NewString()
	prepared, err := s.commerceDirect.PrepareJob(
		ctx, tx, commercepkg.PrepareDirectVideoJobParams{
			JobID: jobID, WorkflowRunID: workflowRunID,
			OrganizationID: project.OrganizationID, ProjectID: project.ID,
			ScriptUnitID: scriptUnitID, CreatedBy: principal.UserID,
			IdempotencyKey: idempotencyKey, Input: input,
		},
	)
	if err != nil {
		return commercepkg.DirectVideoJob{}, false, err
	}
	workflowInput := workflows.CommerceDirectVideoInput{
		OrganizationID: project.OrganizationID, ProjectID: project.ID,
		ScriptUnitID: scriptUnitID, JobID: jobID,
		WorkflowRunID: workflowRunID, CreatedBy: principal.UserID,
		ProjectControlCommandID: idempotencyKey,
	}
	if err := workflows.EnqueueCommerceDirectVideoTx(ctx, tx, workflowInput, prepared.Production); err != nil {
		return commercepkg.DirectVideoJob{}, false, err
	}
	item, err := s.commerceDirect.InsertPreparedJob(ctx, tx, prepared, idempotencyKey)
	if err != nil {
		return commercepkg.DirectVideoJob{}, false, err
	}
	run, err := scanWorkflowRun(tx.QueryRow(ctx, workflowRunSelectSQL(`WHERE id = $1`), workflowRunID))
	if err != nil {
		return commercepkg.DirectVideoJob{}, false, err
	}
	if err := insertWorkflowQueuedEventTx(ctx, tx, run, run.WorkflowType); err != nil {
		return commercepkg.DirectVideoJob{}, false, err
	}
	if err := completeIdempotencyTxWithStatus(ctx, tx, claim.state, http.StatusAccepted, item); err != nil {
		return commercepkg.DirectVideoJob{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return commercepkg.DirectVideoJob{}, false, err
	}
	return item, false, nil
}

func (s *Server) executeCommerceDirectVideoCancelAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	arguments, err := decodeCommerceActionMap(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	job, replayed, err := s.cancelCommerceDirectVideoCore(
		ctx, principal, project,
		agentReferenceStringArg(arguments, "jobId"),
		agentStringArg(arguments, "reason"), command.ID,
	)
	if err != nil {
		return agentToolResult{}, err
	}
	data, err := agentCommerceValueData(job)
	if err != nil {
		return agentToolResult{}, err
	}
	data["idempotentReplay"] = replayed
	return agentToolOK("commerce.video.cancel", arguments, "带货视频取消请求已提交", data), nil
}

func (s *Server) cancelCommerceDirectVideoCore(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	jobID string,
	reason string,
	idempotencyKey string,
) (commercepkg.DirectVideoJob, bool, error) {
	jobID = strings.TrimSpace(jobID)
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "用户取消带货视频任务"
	}
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId": project.ID, "jobId": jobID,
		"userId": principal.UserID, "reason": reason,
	})
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return commercepkg.DirectVideoJob{}, false, err
	}
	defer tx.Rollback(ctx)
	claim, err := claimIdempotencyTx(
		ctx, tx, project.OrganizationID,
		"commerce_direct_video:cancel:"+jobID,
		idempotencyKey, requestHash,
	)
	if err != nil {
		return commercepkg.DirectVideoJob{}, false, err
	}
	replayed := len(claim.replaySnapshot) > 0
	job, err := s.commerceDirect.GetJob(
		ctx, tx, project.OrganizationID, project.ID, jobID,
	)
	if err != nil {
		return commercepkg.DirectVideoJob{}, false, err
	}
	if !replayed {
		status := http.StatusAccepted
		if commerceDirectVideoTerminal(job.Status) {
			status = http.StatusOK
		} else {
			if job.WorkflowRunID == nil || strings.TrimSpace(*job.WorkflowRunID) == "" {
				return commercepkg.DirectVideoJob{}, false, newAPIError(
					http.StatusConflict, commercepkg.CodeDirectVideoStateConflict,
					"视频任务缺少可取消的工作流",
				)
			}
			tag, err := tx.Exec(ctx, `
				UPDATE commerce_direct_video_jobs
				SET status = 'cancelling',
				    error_code = 'USER_CANCELLED',
				    error_message = $4,
				    updated_at = now()
				WHERE id = $1 AND organization_id = $2 AND project_id = $3
				  AND status IN ('queued', 'running', 'cancelling')
			`, job.ID, project.OrganizationID, project.ID, reason)
			if err != nil {
				return commercepkg.DirectVideoJob{}, false, err
			}
			if tag.RowsAffected() != 1 {
				return commercepkg.DirectVideoJob{}, false, newAPIError(
					http.StatusConflict, commercepkg.CodeDirectVideoStateConflict,
					"视频任务状态已变化，请刷新后重试",
				)
			}
			job.Status = "cancelling"
			errorCode := "USER_CANCELLED"
			job.ErrorCode = &errorCode
			job.ErrorMessage = &reason
		}
		if err := completeIdempotencyTxWithStatus(ctx, tx, claim.state, status, job); err != nil {
			return commercepkg.DirectVideoJob{}, false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return commercepkg.DirectVideoJob{}, false, err
	}
	current, err := s.commerceDirect.GetJob(
		ctx, s.db, project.OrganizationID, project.ID, jobID,
	)
	if err != nil {
		return commercepkg.DirectVideoJob{}, false, err
	}
	if !commerceDirectVideoTerminal(current.Status) {
		if err := s.requestCommerceDirectVideoCancellationContext(ctx, project, current, reason); err != nil {
			return commercepkg.DirectVideoJob{}, false, err
		}
		current, err = s.commerceDirect.GetJob(
			ctx, s.db, project.OrganizationID, project.ID, jobID,
		)
		if err != nil {
			return commercepkg.DirectVideoJob{}, false, err
		}
	}
	return current, replayed, nil
}

func (s *Server) requestCommerceDirectVideoCancellationContext(
	ctx context.Context,
	project Project,
	job commercepkg.DirectVideoJob,
	reason string,
) error {
	if job.WorkflowRunID == nil || strings.TrimSpace(*job.WorkflowRunID) == "" {
		return newAPIError(
			http.StatusConflict, commercepkg.CodeDirectVideoStateConflict,
			"视频任务缺少可取消的工作流",
		)
	}
	run, err := scanWorkflowRun(s.db.QueryRow(ctx, workflowRunSelectSQL(`
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
	`), *job.WorkflowRunID, project.OrganizationID, project.ID))
	if err != nil {
		return err
	}
	updatedRun, err := s.cancelWorkflowRunItem(ctx, run, reason)
	if err != nil {
		return err
	}
	if updatedRun.Status == "cancelled" {
		return s.finalizePreStartCommerceDirectVideoCancellationContext(ctx, project, job, reason)
	}
	return nil
}

func (s *Server) finalizePreStartCommerceDirectVideoCancellationContext(
	ctx context.Context,
	project Project,
	job commercepkg.DirectVideoJob,
	reason string,
) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE commerce_direct_video_jobs
		SET status = 'cancelled',
		    completed_at = now(),
		    cancelled_at = now(),
		    error_code = 'USER_CANCELLED',
		    error_message = $4,
		    updated_at = now()
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND status = 'cancelling'
	`, job.ID, project.OrganizationID, project.ID, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		if err := insertAPIEvent(
			ctx, tx, project.OrganizationID, project.ID,
			"commerce.direct_video.cancelled", "commerce_direct_video_job", job.ID,
			mustRawJSON(map[string]any{
				"workflowRunId":        job.WorkflowRunID,
				"commerceScriptUnitId": job.ScriptUnitID,
				"reason":               reason,
			}),
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
