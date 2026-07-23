package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Einzieg/cineweave/internal/commerce"
	"github.com/jackc/pgx/v5"
)

func (r *CommerceGenerationRuntime) BeginCommerceFinalCompose(
	ctx context.Context,
	input CommerceFinalComposeInput,
) (CommerceReferenceImageItemAttempt, error) {
	if err := validateCommerceFinalComposeInput(input); err != nil {
		return CommerceReferenceImageItemAttempt{}, err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceReferenceImageItemAttempt{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := r.lockGenerationState(ctx, tx, input.Identity); err != nil {
		return CommerceReferenceImageItemAttempt{}, err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowRunID, CommercePhaseFinalCompose, input); err != nil {
		return CommerceReferenceImageItemAttempt{}, err
	}
	if err := assertCommerceTimelineReadyForCompose(ctx, tx, input); err != nil {
		return CommerceReferenceImageItemAttempt{}, err
	}
	expectedHash, err := CommerceFinalComposeSubjectHash(input)
	if err != nil {
		return CommerceReferenceImageItemAttempt{}, err
	}
	var itemID string
	if err := tx.QueryRow(ctx, `
		SELECT id::text
		FROM commerce_production_run_items
		WHERE organization_id = $1 AND project_id = $2
		  AND run_id = $3 AND script_unit_id = $4
		  AND script_unit_generation_id = $5
		  AND subject_type = 'final_compose' AND subject_key = $6
		  AND input_hash = $7
		FOR UPDATE
	`, input.Identity.OrganizationID, input.Identity.ProjectID, input.ProductionRunID,
		input.Identity.ScriptUnitID, input.Identity.UnitGenerationID,
		input.TimelineID, expectedHash).Scan(&itemID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CommerceReferenceImageItemAttempt{}, generationMismatch("成片生产项与冻结时间线不一致", err)
		}
		return CommerceReferenceImageItemAttempt{}, err
	}
	attempt, err := r.runs.StartAttempt(ctx, tx, commerce.StartProductionAttemptParams{
		OrganizationID: input.Identity.OrganizationID,
		ProjectID:      input.Identity.ProjectID,
		RunID:          input.ProductionRunID,
		ItemID:         itemID,
		InputHash:      expectedHash,
		WorkflowRunID:  input.WorkflowRunID,
	})
	if err != nil {
		return CommerceReferenceImageItemAttempt{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommerceReferenceImageItemAttempt{}, err
	}
	return CommerceReferenceImageItemAttempt{
		ItemID: attempt.ItemID, AttemptID: attempt.ID,
		AttemptNumber: attempt.AttemptNumber, InputHash: attempt.InputHash,
	}, nil
}

func (r *CommerceGenerationRuntime) CommitCommerceFinalCompose(
	ctx context.Context,
	input CommitCommerceFinalComposeInput,
) (CommerceFinalComposeOutput, error) {
	if err := validateCommerceFinalComposeInput(input.WorkflowInput); err != nil {
		return CommerceFinalComposeOutput{}, err
	}
	if strings.TrimSpace(input.Result.FinalVideoVersionID) == "" || strings.TrimSpace(input.Result.ArtifactID) == "" ||
		strings.TrimSpace(input.Result.MediaFileID) == "" || strings.TrimSpace(input.Result.StorageKey) == "" {
		return CommerceFinalComposeOutput{}, commerce.Error{Code: CommerceCodeGenerationMismatch, Message: "媒体合成结果不完整"}
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceFinalComposeOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := r.lockGenerationState(ctx, tx, input.WorkflowInput.Identity); err != nil {
		return CommerceFinalComposeOutput{}, err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowInput.WorkflowRunID, CommercePhaseFinalCompose, input.WorkflowInput); err != nil {
		return CommerceFinalComposeOutput{}, err
	}
	if err := assertCommerceTimelineReadyForCompose(ctx, tx, input.WorkflowInput); err != nil {
		return CommerceFinalComposeOutput{}, err
	}
	var finalArtifactID, finalMediaFileID, finalStorageKey string
	if err := tx.QueryRow(ctx, `
		SELECT artifact_id::text, media_file_id::text, storage_key
		FROM final_video_versions
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND timeline_id = $4 AND production_generation_id = $5
		  AND commerce_script_unit_id = $6
		  AND commerce_script_unit_generation_id = $7
		  AND workflow_run_id = $8
		FOR UPDATE
	`, input.Result.FinalVideoVersionID, input.WorkflowInput.Identity.OrganizationID,
		input.WorkflowInput.Identity.ProjectID, input.WorkflowInput.TimelineID,
		input.WorkflowInput.Identity.ProjectGenerationID, input.WorkflowInput.Identity.ScriptUnitID,
		input.WorkflowInput.Identity.UnitGenerationID, input.WorkflowInput.WorkflowRunID).Scan(
		&finalArtifactID, &finalMediaFileID, &finalStorageKey,
	); err != nil {
		return CommerceFinalComposeOutput{}, generationMismatch("成片版本与当前脚本单元生产身份不一致", err)
	}
	if finalArtifactID != input.Result.ArtifactID || finalMediaFileID != input.Result.MediaFileID || finalStorageKey != input.Result.StorageKey {
		return CommerceFinalComposeOutput{}, generationMismatch("媒体合成输出与成片版本记录不一致", nil)
	}
	output := CommerceFinalComposeOutput{
		Identity: input.WorkflowInput.Identity, ProductionRunID: input.WorkflowInput.ProductionRunID,
		TimelineID: input.WorkflowInput.TimelineID, FinalVideoVersionID: input.Result.FinalVideoVersionID,
		ArtifactID: input.Result.ArtifactID, MediaFileID: input.Result.MediaFileID,
		StorageKey: input.Result.StorageKey, Status: commerce.RunSucceeded,
	}
	run, err := r.runs.CompleteAttempt(ctx, tx, commerce.CompleteProductionAttemptParams{
		OrganizationID:            input.WorkflowInput.Identity.OrganizationID,
		ProjectID:                 input.WorkflowInput.Identity.ProjectID,
		RunID:                     input.WorkflowInput.ProductionRunID,
		ItemID:                    input.Attempt.ItemID,
		AttemptID:                 input.Attempt.AttemptID,
		Status:                    commerce.ItemSucceeded,
		OutputSnapshot:            mustJSON(output),
		OutputArtifactID:          input.Result.ArtifactID,
		OutputMediaFileID:         input.Result.MediaFileID,
		OutputFinalVideoVersionID: input.Result.FinalVideoVersionID,
	})
	if err != nil {
		return CommerceFinalComposeOutput{}, err
	}
	if run.Status != commerce.RunSucceeded {
		return CommerceFinalComposeOutput{}, generationMismatch("成片批次未进入成功终态", nil)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_timelines
		SET status = 'archived', updated_at = now()
		WHERE project_id = $1 AND commerce_script_unit_id = $2
		  AND id <> $3 AND status = 'active'
	`, input.WorkflowInput.Identity.ProjectID, input.WorkflowInput.Identity.ScriptUnitID,
		input.WorkflowInput.TimelineID); err != nil {
		return CommerceFinalComposeOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_timelines
		SET status = 'active', workflow_run_id = $2, updated_at = now()
		WHERE id = $1 AND revision = $3
	`, input.WorkflowInput.TimelineID, input.WorkflowInput.WorkflowRunID,
		input.WorkflowInput.ExpectedTimelineRevision); err != nil {
		return CommerceFinalComposeOutput{}, err
	}
	if _, _, err := transitionWorkflowRunTx(ctx, tx, input.WorkflowInput.WorkflowRunID, "succeeded", "", "", mustJSON(output)); err != nil {
		return CommerceFinalComposeOutput{}, err
	}
	if err := insertEvent(ctx, tx, input.WorkflowInput.Identity.OrganizationID, input.WorkflowInput.Identity.ProjectID,
		"commerce.production.final_compose.completed", "commerce_production_run", input.WorkflowInput.ProductionRunID,
		mustJSON(map[string]any{
			"workflowRunId":           input.WorkflowInput.WorkflowRunID,
			"commerceProductionRunId": input.WorkflowInput.ProductionRunID,
			"commerceScriptUnitId":    input.WorkflowInput.Identity.ScriptUnitID,
			"scriptUnitGenerationId":  input.WorkflowInput.Identity.UnitGenerationID,
			"timelineId":              input.WorkflowInput.TimelineID,
			"finalVideoVersionId":     input.Result.FinalVideoVersionID,
			"status":                  "succeeded",
		})); err != nil {
		return CommerceFinalComposeOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommerceFinalComposeOutput{}, err
	}
	return output, nil
}

func (r *CommerceGenerationRuntime) FailCommerceFinalCompose(ctx context.Context, input FailCommerceFinalComposeInput) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	record, err := loadCommerceWorkflowRunRecord(ctx, tx, input.WorkflowInput.WorkflowRunID)
	if err != nil {
		return err
	}
	if err := validateCommerceWorkflowRunRecord(record, input.WorkflowInput.WorkflowRunID, CommercePhaseFinalCompose, input.WorkflowInput); err != nil {
		return err
	}
	code := strings.TrimSpace(input.ErrorCode)
	if code == "" {
		code = codeActivityFailed
	}
	message := strings.TrimSpace(input.ErrorMessage)
	if message == "" {
		message = "成片合成失败"
	}
	var run commerce.ProductionRun
	if input.Cancelled {
		run, err = r.repository.CancelProductionRun(ctx, tx,
			input.WorkflowInput.Identity.OrganizationID, input.WorkflowInput.Identity.ProjectID,
			input.WorkflowInput.ProductionRunID, message)
	} else {
		status := commerce.ItemFailedTerminal
		if input.Retryable {
			status = commerce.ItemFailedRetryable
		}
		run, err = r.runs.CompleteAttempt(ctx, tx, commerce.CompleteProductionAttemptParams{
			OrganizationID: input.WorkflowInput.Identity.OrganizationID,
			ProjectID:      input.WorkflowInput.Identity.ProjectID,
			RunID:          input.WorkflowInput.ProductionRunID,
			ItemID:         input.Attempt.ItemID,
			AttemptID:      input.Attempt.AttemptID,
			Status:         status,
			OutputSnapshot: json.RawMessage(`{}`),
			ErrorCode:      code, ErrorMessage: message, Retryable: input.Retryable,
		})
		if err != nil && record.Status == "failed" {
			run, err = r.repository.FailActiveProductionRunItems(ctx, tx,
				input.WorkflowInput.Identity.OrganizationID, input.WorkflowInput.Identity.ProjectID,
				input.WorkflowInput.ProductionRunID, code, message, input.Retryable)
		}
	}
	if err != nil {
		return err
	}
	if record.Status == "queued" || record.Status == "running" || record.Status == "waiting_review" || record.Status == "cancelling" {
		target := "failed"
		if input.Cancelled {
			target = "cancelled"
		}
		if _, _, err := transitionWorkflowRunTx(ctx, tx, input.WorkflowInput.WorkflowRunID, target, code, message, mustJSON(map[string]any{
			"productionRunId": run.ID, "status": run.Status,
		})); err != nil {
			return err
		}
	}
	if err := insertEvent(ctx, tx, input.WorkflowInput.Identity.OrganizationID, input.WorkflowInput.Identity.ProjectID,
		"commerce.production.run.failed", "commerce_production_run", input.WorkflowInput.ProductionRunID,
		mustJSON(map[string]any{
			"workflowRunId":           input.WorkflowInput.WorkflowRunID,
			"commerceProductionRunId": input.WorkflowInput.ProductionRunID,
			"commerceScriptUnitId":    input.WorkflowInput.Identity.ScriptUnitID,
			"scriptUnitGenerationId":  input.WorkflowInput.Identity.UnitGenerationID,
			"timelineId":              input.WorkflowInput.TimelineID,
			"status":                  run.Status, "errorCode": code, "errorMessage": message,
		})); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func assertCommerceTimelineReadyForCompose(ctx context.Context, tx pgx.Tx, input CommerceFinalComposeInput) error {
	var revision int64
	var status string
	if err := tx.QueryRow(ctx, `
		SELECT revision, status
		FROM project_timelines
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND production_generation_id = $4
		  AND commerce_script_unit_id = $5
		  AND commerce_script_unit_generation_id = $6
		  AND status IN ('draft', 'active')
		FOR UPDATE
	`, input.TimelineID, input.Identity.OrganizationID, input.Identity.ProjectID,
		input.Identity.ProjectGenerationID, input.Identity.ScriptUnitID,
		input.Identity.UnitGenerationID).Scan(&revision, &status); err != nil {
		return generationMismatch("当前脚本单元时间线不存在或已归档", err)
	}
	if revision != input.ExpectedTimelineRevision {
		return commerce.Error{Code: CommerceCodeGenerationMismatch, Message: "时间线已被修改，请刷新后重试"}
	}
	var total, invalid int
	if err := tx.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE
			shot.id IS NULL
			OR contract.storyboard_shot_id IS NULL
			OR contract.script_unit_id IS DISTINCT FROM $3::uuid
			OR contract.script_unit_generation_id IS DISTINCT FROM $4::uuid
			OR COALESCE(shot.video_status, '') <> 'succeeded'
			OR (clip.video_artifact_id IS NULL AND shot.video_artifact_id IS NULL)
			OR (clip.video_media_file_id IS NULL AND shot.video_media_file_id IS NULL)
			OR COALESCE(clip.source_storage_key, shot.video_storage_key, '') = ''
		)
		FROM timeline_clips clip
		LEFT JOIN storyboard_shots shot ON shot.id = clip.storyboard_shot_id AND shot.deleted_at IS NULL
		LEFT JOIN commerce_shot_contracts contract
		  ON contract.storyboard_shot_id = shot.id
		 AND contract.commerce_storyboard_plan_id = shot.commerce_storyboard_plan_id
		 AND contract.organization_id = shot.organization_id
		 AND contract.project_id = shot.project_id
		WHERE clip.timeline_id = $1 AND clip.project_id = $2 AND clip.enabled
	`, input.TimelineID, input.Identity.ProjectID, input.Identity.ScriptUnitID,
		input.Identity.UnitGenerationID).Scan(&total, &invalid); err != nil {
		return err
	}
	if total == 0 || invalid != 0 {
		return commerce.Error{Code: CommerceCodeGenerationMismatch, Message: "时间线仍有未完成或身份不一致的镜头视频"}
	}
	return nil
}
