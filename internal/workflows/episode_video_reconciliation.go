package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	episodeVideoMissingItemCode       = "VIDEO_PRODUCTION_ITEM_MISSING"
	episodeVideoInvalidResultCode     = "VIDEO_PRODUCTION_DURABLE_RESULT_INVALID"
	episodeVideoIncompleteResultCode  = "VIDEO_PRODUCTION_STATE_INCOMPLETE"
	episodeVideoLegacyIdentityCode    = "VIDEO_EXECUTION_IDENTITY_MISMATCH"
	episodeVideoReconciliationVersion = 2
)

type episodeVideoCheckpointSnapshot struct {
	Status        string
	Revision      int64
	UpdatedAt     time.Time
	TargetShotIDs []string
	Metadata      map[string]any
}

type episodeVideoDurableOutcome struct {
	ShotID                     string
	ItemID                     string
	ItemStatus                 string
	ItemAttempt                int
	ItemRevision               int64
	IdentityVersion            int
	ItemErrorCode              string
	ItemErrorMessage           string
	BatchID                    string
	PlanID                     string
	PlanStatus                 string
	PlanOutputArtifactID       string
	PlanOutputMediaFileID      string
	PlanIdentityMatches        bool
	SegmentCount               int
	SucceededSegmentCount      int
	FailedSegmentCount         int
	CancelledSegmentCount      int
	ActiveSegmentCount         int
	ProviderTaskCount          int
	SucceededProviderTaskCount int
	FailedProviderTaskCount    int
	CancelledProviderTaskCount int
	ActiveProviderTaskCount    int
	ExecutionErrorCode         string
	ExecutionErrorMessage      string
	ProviderTaskIDs            []string
}

type episodeVideoNormalizedOutcome struct {
	ShotID       string `json:"shotId"`
	ItemID       string `json:"itemId,omitempty"`
	ItemAttempt  int    `json:"itemAttempt,omitempty"`
	PlanID       string `json:"planId,omitempty"`
	Status       string `json:"status"`
	ErrorCode    string `json:"errorCode,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
	Diagnostic   string `json:"diagnostic,omitempty"`
}

type episodeVideoReconciliationSummary struct {
	Status          string                          `json:"status"`
	SucceededCount  int                             `json:"succeededCount"`
	FailedCount     int                             `json:"failedCount"`
	CancelledCount  int                             `json:"cancelledCount"`
	DiagnosticCount int                             `json:"diagnosticCount"`
	Outcomes        []episodeVideoNormalizedOutcome `json:"outcomes"`
}

func (a Activities) reconcileEpisodeVideoProductionCheckpointV2(ctx context.Context, plan EpisodeVideoProductionPlan) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	snapshot, err := loadEpisodeVideoCheckpointSnapshotTx(ctx, tx, plan)
	if err != nil {
		return err
	}
	durable, err := loadEpisodeVideoDurableOutcomes(ctx, tx, plan.CheckpointID, snapshot.TargetShotIDs)
	if err != nil {
		return err
	}
	normalized := normalizeEpisodeVideoOutcomes(snapshot, durable)
	changed, err := repairEpisodeVideoItemsTx(ctx, tx, normalized, durable)
	if err != nil {
		return err
	}
	batchChanged, err := repairEpisodeVideoBatchesTx(ctx, tx, plan.CheckpointID)
	if err != nil {
		return err
	}
	changed = changed || batchChanged

	summary := summarizeEpisodeVideoReconciliation(snapshot.Status, normalized)
	reconciliationHash := hashEpisodeVideoValue(map[string]any{
		"version":  episodeVideoReconciliationVersion,
		"status":   summary.Status,
		"outcomes": summary.Outcomes,
	})
	existingHash := stringMetadataValue(snapshot.Metadata, "reconciliationHash")
	if !changed && existingHash == reconciliationHash && snapshot.Status == summary.Status {
		return tx.Commit(ctx)
	}
	reconciliation := map[string]any{
		"version":         episodeVideoReconciliationVersion,
		"status":          summary.Status,
		"succeededCount":  summary.SucceededCount,
		"failedCount":     summary.FailedCount,
		"cancelledCount":  summary.CancelledCount,
		"diagnosticCount": summary.DiagnosticCount,
		"hash":            reconciliationHash,
		"reconciledAt":    time.Now().UTC(),
	}
	command, err := tx.Exec(ctx, `
		UPDATE episode_video_production_checkpoints
		SET status = $3,
		    completed_at = COALESCE(completed_at, now()),
		    updated_at = now(),
		    revision = revision + 1,
		    metadata = metadata || jsonb_build_object(
		      'runtimeVersion', 2,
		      'reconciliationHash', $4::text,
		      'reconciliation', $5::jsonb
		    )
		WHERE id = $1 AND revision = $2
	`, plan.CheckpointID, snapshot.Revision, summary.Status, reconciliationHash, mustJSON(reconciliation))
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("episode video checkpoint %s changed while it was being reconciled", plan.CheckpointID)
	}
	payload := episodeVideoEventPayload(plan, "", "", "", summary.Status)
	payload["reconciliationHash"] = reconciliationHash
	payload["succeededCount"] = summary.SucceededCount
	payload["failedCount"] = summary.FailedCount
	payload["cancelledCount"] = summary.CancelledCount
	payload["diagnosticCount"] = summary.DiagnosticCount
	if err := insertEvent(ctx, tx, plan.OrganizationID, plan.ProjectID,
		"video.production.checkpoint.reconciled", "episode_video_checkpoint", plan.CheckpointID,
		mustJSON(payload),
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func loadEpisodeVideoCheckpointSnapshotTx(ctx context.Context, tx pgx.Tx, plan EpisodeVideoProductionPlan) (episodeVideoCheckpointSnapshot, error) {
	var snapshot episodeVideoCheckpointSnapshot
	var targetRaw, metadataRaw json.RawMessage
	if err := tx.QueryRow(ctx, `
		SELECT status, revision, updated_at, metadata->'targetShotIds', metadata
		FROM episode_video_production_checkpoints
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND production_generation_id = $4
		  AND video_production_binding_id = $5
		  AND video_production_binding_revision = $6
		  AND workflow_run_id = $7
		FOR UPDATE
	`, plan.CheckpointID, plan.OrganizationID, plan.ProjectID, plan.ProductionGenerationID,
		plan.VideoProductionBindingID, plan.VideoProductionBindingRevision, plan.WorkflowRunID).Scan(
		&snapshot.Status, &snapshot.Revision, &snapshot.UpdatedAt, &targetRaw, &metadataRaw,
	); err != nil {
		return episodeVideoCheckpointSnapshot{}, err
	}
	if err := json.Unmarshal(targetRaw, &snapshot.TargetShotIDs); err != nil || len(snapshot.TargetShotIDs) == 0 {
		return episodeVideoCheckpointSnapshot{}, fmt.Errorf("episode video checkpoint target list is invalid")
	}
	if err := json.Unmarshal(metadataRaw, &snapshot.Metadata); err != nil {
		return episodeVideoCheckpointSnapshot{}, err
	}
	return snapshot, nil
}

func loadEpisodeVideoDurableOutcomes(
	ctx context.Context,
	query episodeVideoOutputQuerier,
	checkpointID string,
	targetShotIDs []string,
) ([]episodeVideoDurableOutcome, error) {
	rows, err := query.Query(ctx, `
		WITH target AS (
		  SELECT value::uuid AS shot_id, ordinal
		  FROM jsonb_array_elements_text($2::jsonb) WITH ORDINALITY AS source(value, ordinal)
		)
		SELECT target.shot_id::text,
		       COALESCE(item.id::text, ''), COALESCE(item.status, ''), COALESCE(item.attempt, 0),
		       COALESCE(item.revision, 0), COALESCE(item.execution_identity_version, 0),
		       COALESCE(item.error_code, ''), COALESCE(item.error_detail->>'message', ''),
		       COALESCE(item.batch_id::text, ''),
		       COALESCE(plan.id::text, ''), COALESCE(plan.status, ''),
		       COALESCE(plan.output_artifact_id::text, ''), COALESCE(plan.output_media_file_id::text, ''),
		       COALESCE(plan.operation_item_id = item.id AND plan.operation_item_attempt = item.attempt, false),
		       COALESCE(execution.segment_count, 0), COALESCE(execution.succeeded_segments, 0),
		       COALESCE(execution.failed_segments, 0), COALESCE(execution.cancelled_segments, 0),
		       COALESCE(execution.active_segments, 0), COALESCE(execution.task_count, 0),
		       COALESCE(execution.succeeded_tasks, 0), COALESCE(execution.failed_tasks, 0),
		       COALESCE(execution.cancelled_tasks, 0), COALESCE(execution.active_tasks, 0),
		       COALESCE(execution.error_code, ''), COALESCE(execution.error_message, ''),
		       COALESCE(execution.provider_task_ids, '[]'::jsonb)
		FROM target
		LEFT JOIN LATERAL (
		  SELECT item.*
		  FROM episode_video_production_batches batch
		  JOIN episode_video_production_items item ON item.batch_id = batch.id
		  WHERE batch.checkpoint_id = $1 AND item.storyboard_shot_id = target.shot_id
		  ORDER BY item.attempt DESC, item.created_at DESC, item.id DESC
		  LIMIT 1
		) item ON true
		LEFT JOIN video_render_plans plan ON plan.id = item.video_render_plan_id
		LEFT JOIN LATERAL (
		  SELECT count(segment.id)::integer AS segment_count,
		         count(segment.id) FILTER (WHERE segment.status = 'succeeded')::integer AS succeeded_segments,
		         count(segment.id) FILTER (WHERE segment.status IN ('failed', 'stale'))::integer AS failed_segments,
		         count(segment.id) FILTER (WHERE segment.status = 'cancelled')::integer AS cancelled_segments,
		         count(segment.id) FILTER (WHERE segment.status IN ('planned', 'queued', 'running'))::integer AS active_segments,
		         count(task.id)::integer AS task_count,
		         count(task.id) FILTER (WHERE task.status = 'succeeded')::integer AS succeeded_tasks,
		         count(task.id) FILTER (WHERE task.status IN ('failed', 'blocked', 'unknown_outcome'))::integer AS failed_tasks,
		         count(task.id) FILTER (WHERE task.status = 'cancelled')::integer AS cancelled_tasks,
		         count(task.id) FILTER (WHERE task.status IN ('queued', 'running', 'cancelling'))::integer AS active_tasks,
		         min(COALESCE(NULLIF(task.error_code, ''), NULLIF(segment.error_code, ''))) AS error_code,
		         min(COALESCE(NULLIF(task.error_message, ''), NULLIF(segment.error_message, ''))) AS error_message,
		         jsonb_agg(task.id::text ORDER BY segment.segment_index)
		           FILTER (WHERE task.id IS NOT NULL) AS provider_task_ids
		  FROM video_render_segments segment
		  LEFT JOIN provider_async_tasks task
		    ON task.id = segment.provider_async_task_id
		   AND task.video_render_plan_id = plan.id
		   AND task.video_render_segment_id = segment.id
		   AND task.operation_item_id = item.id
		   AND task.operation_item_attempt = item.attempt
		  WHERE segment.video_render_plan_id = plan.id
		) execution ON true
		ORDER BY target.ordinal
	`, checkpointID, mustJSON(targetShotIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	outcomes := make([]episodeVideoDurableOutcome, 0, len(targetShotIDs))
	for rows.Next() {
		var outcome episodeVideoDurableOutcome
		var providerTaskIDsRaw json.RawMessage
		if err := rows.Scan(
			&outcome.ShotID,
			&outcome.ItemID, &outcome.ItemStatus, &outcome.ItemAttempt,
			&outcome.ItemRevision, &outcome.IdentityVersion,
			&outcome.ItemErrorCode, &outcome.ItemErrorMessage,
			&outcome.BatchID,
			&outcome.PlanID, &outcome.PlanStatus,
			&outcome.PlanOutputArtifactID, &outcome.PlanOutputMediaFileID,
			&outcome.PlanIdentityMatches,
			&outcome.SegmentCount, &outcome.SucceededSegmentCount,
			&outcome.FailedSegmentCount, &outcome.CancelledSegmentCount,
			&outcome.ActiveSegmentCount, &outcome.ProviderTaskCount,
			&outcome.SucceededProviderTaskCount, &outcome.FailedProviderTaskCount,
			&outcome.CancelledProviderTaskCount, &outcome.ActiveProviderTaskCount,
			&outcome.ExecutionErrorCode, &outcome.ExecutionErrorMessage,
			&providerTaskIDsRaw,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(providerTaskIDsRaw, &outcome.ProviderTaskIDs); err != nil {
			return nil, err
		}
		outcomes = append(outcomes, outcome)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(outcomes) != len(targetShotIDs) {
		return nil, fmt.Errorf("episode video checkpoint target projection is incomplete")
	}
	return outcomes, nil
}

func normalizeEpisodeVideoOutcomes(
	snapshot episodeVideoCheckpointSnapshot,
	durable []episodeVideoDurableOutcome,
) []episodeVideoNormalizedOutcome {
	normalized := make([]episodeVideoNormalizedOutcome, 0, len(durable))
	for _, outcome := range durable {
		result := episodeVideoNormalizedOutcome{
			ShotID: outcome.ShotID, ItemID: outcome.ItemID, ItemAttempt: outcome.ItemAttempt, PlanID: outcome.PlanID,
		}
		switch {
		case outcome.ItemID == "":
			result.Status = "failed"
			result.ErrorCode = episodeVideoMissingItemCode
			result.ErrorMessage = "镜头没有持久化的视频执行项"
			result.Diagnostic = "missing_item"
			if snapshot.Status == "cancelled" || snapshot.Status == "cancelling" {
				result.Status = "cancelled"
				result.ErrorCode = "VIDEO_PRODUCTION_ITEM_CANCELLED"
				result.ErrorMessage = "分集视频生产已取消"
			}
		case outcome.IdentityVersion != 2:
			result.Status = "failed"
			result.ErrorCode = episodeVideoLegacyIdentityCode
			result.ErrorMessage = "镜头执行项不是 v2 精确身份契约"
			result.Diagnostic = "legacy_item"
		case outcome.ItemStatus == "cancelled":
			result.Status = "cancelled"
			result.ErrorCode = firstNonEmptyString(outcome.ItemErrorCode, "VIDEO_PRODUCTION_ITEM_CANCELLED")
			result.ErrorMessage = firstNonEmptyString(outcome.ItemErrorMessage, "镜头视频生产已取消")
		case outcome.ItemStatus == "failed" || outcome.ItemStatus == "discarded":
			result.Status = "failed"
			result.ErrorCode = firstNonEmptyString(outcome.ItemErrorCode, outcome.ExecutionErrorCode, "VIDEO_PRODUCTION_ITEM_FAILED")
			result.ErrorMessage = firstNonEmptyString(outcome.ItemErrorMessage, outcome.ExecutionErrorMessage, "镜头视频生产失败")
		case episodeVideoDurableOutcomeSucceeded(outcome):
			result.Status = "succeeded"
		case snapshot.Status == "cancelled" || snapshot.Status == "cancelling":
			result.Status = "cancelled"
			result.ErrorCode = "VIDEO_PRODUCTION_ITEM_CANCELLED"
			result.ErrorMessage = "分集视频生产已取消"
		case outcome.PlanID != "" && episodeVideoDurableOutcomeFailed(outcome):
			result.Status = "failed"
			result.ErrorCode = firstNonEmptyString(outcome.ExecutionErrorCode, episodeVideoInvalidResultCode)
			result.ErrorMessage = firstNonEmptyString(outcome.ExecutionErrorMessage, "镜头视频执行计划未产生完整媒体结果")
			result.Diagnostic = "execution_failed"
		default:
			result.Status = "failed"
			result.ErrorCode = episodeVideoIncompleteResultCode
			result.ErrorMessage = "镜头视频执行没有可恢复的完整终态"
			result.Diagnostic = "incomplete_execution"
		}
		normalized = append(normalized, result)
	}
	return normalized
}

func episodeVideoDurableOutcomeSucceeded(outcome episodeVideoDurableOutcome) bool {
	return outcome.IdentityVersion == 2 && outcome.PlanID != "" && outcome.PlanIdentityMatches &&
		outcome.PlanStatus == "succeeded" && outcome.PlanOutputArtifactID != "" && outcome.PlanOutputMediaFileID != "" &&
		outcome.SegmentCount > 0 && outcome.SucceededSegmentCount == outcome.SegmentCount &&
		outcome.ProviderTaskCount == outcome.SegmentCount && outcome.SucceededProviderTaskCount == outcome.SegmentCount &&
		outcome.FailedSegmentCount == 0 && outcome.CancelledSegmentCount == 0 && outcome.ActiveSegmentCount == 0 &&
		outcome.FailedProviderTaskCount == 0 && outcome.CancelledProviderTaskCount == 0 && outcome.ActiveProviderTaskCount == 0
}

func episodeVideoDurableOutcomeFailed(outcome episodeVideoDurableOutcome) bool {
	switch outcome.PlanStatus {
	case "failed", "cancelled", "stale", "archived", "replan_required":
		return true
	}
	return outcome.FailedSegmentCount > 0 || outcome.CancelledSegmentCount > 0 ||
		outcome.FailedProviderTaskCount > 0 || outcome.CancelledProviderTaskCount > 0
}

func repairEpisodeVideoItemsTx(
	ctx context.Context,
	tx pgx.Tx,
	normalized []episodeVideoNormalizedOutcome,
	durable []episodeVideoDurableOutcome,
) (bool, error) {
	changed := false
	for index, result := range normalized {
		outcome := durable[index]
		if outcome.ItemID == "" {
			continue
		}
		errorCode := result.ErrorCode
		errorMessage := result.ErrorMessage
		needsUpdate := outcome.ItemStatus != result.Status
		if result.Status != "succeeded" && (outcome.ItemErrorCode != errorCode || outcome.ItemErrorMessage != errorMessage) {
			needsUpdate = true
		}
		if !needsUpdate {
			continue
		}
		command, err := tx.Exec(ctx, `
			UPDATE episode_video_production_items
			SET status = $3,
			    error_code = NULLIF($4, ''),
			    error_detail = CASE WHEN $5 = '' THEN '{}'::jsonb ELSE jsonb_build_object('message', $5::text) END,
			    completed_at = COALESCE(completed_at, now()),
			    updated_at = now(), revision = revision + 1,
			    metadata = metadata || jsonb_build_object('reconciledAt', now(), 'reconciliationVersion', 2)
			WHERE id = $1 AND revision = $2
		`, outcome.ItemID, outcome.ItemRevision, result.Status, errorCode, errorMessage)
		if err != nil {
			return false, err
		}
		if command.RowsAffected() != 1 {
			return false, fmt.Errorf("episode video item %s changed while it was being reconciled", outcome.ItemID)
		}
		changed = true
	}
	return changed, nil
}

func repairEpisodeVideoBatchesTx(ctx context.Context, tx pgx.Tx, checkpointID string) (bool, error) {
	rows, err := tx.Query(ctx, `
		SELECT batch.id::text, batch.status, batch.revision,
		       batch.total_items, batch.succeeded_items, batch.failed_items, batch.cancelled_items,
		       COALESCE(batch.metadata->>'reconciliationHash', ''),
		       COALESCE(counts.total_items, 0), COALESCE(counts.succeeded_items, 0),
		       COALESCE(counts.failed_items, 0), COALESCE(counts.cancelled_items, 0)
		FROM episode_video_production_batches batch
		LEFT JOIN LATERAL (
		  SELECT count(item.id)::integer AS total_items,
		         count(item.id) FILTER (WHERE item.status = 'succeeded')::integer AS succeeded_items,
		         count(item.id) FILTER (WHERE item.status IN ('failed', 'discarded'))::integer AS failed_items,
		         count(item.id) FILTER (WHERE item.status = 'cancelled')::integer AS cancelled_items
		  FROM episode_video_production_items item
		  WHERE item.batch_id = batch.id
		) counts ON true
		WHERE batch.checkpoint_id = $1
		ORDER BY batch.ordinal, batch.attempt
		FOR UPDATE OF batch
	`, checkpointID)
	if err != nil {
		return false, err
	}
	type batchProjection struct {
		ID, Status, ReconciliationHash string
		Revision                       int64
		StoredTotal, StoredSucceeded   int
		StoredFailed, StoredCancelled  int
		Total, Succeeded, Failed       int
		Cancelled                      int
	}
	projections := make([]batchProjection, 0)
	for rows.Next() {
		var projection batchProjection
		if err := rows.Scan(
			&projection.ID, &projection.Status, &projection.Revision,
			&projection.StoredTotal, &projection.StoredSucceeded, &projection.StoredFailed, &projection.StoredCancelled,
			&projection.ReconciliationHash,
			&projection.Total, &projection.Succeeded, &projection.Failed, &projection.Cancelled,
		); err != nil {
			rows.Close()
			return false, err
		}
		projections = append(projections, projection)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	rows.Close()
	changed := false
	for _, projection := range projections {
		status := episodeVideoAggregateStatus(projection.Total, projection.Succeeded, projection.Failed, projection.Cancelled)
		hash := hashEpisodeVideoValue(map[string]any{
			"version":   episodeVideoReconciliationVersion,
			"status":    status,
			"total":     projection.Total,
			"succeeded": projection.Succeeded,
			"failed":    projection.Failed,
			"cancelled": projection.Cancelled,
		})
		if projection.Status == status && projection.StoredTotal == projection.Total &&
			projection.StoredSucceeded == projection.Succeeded && projection.StoredFailed == projection.Failed &&
			projection.StoredCancelled == projection.Cancelled && projection.ReconciliationHash == hash {
			continue
		}
		command, err := tx.Exec(ctx, `
			UPDATE episode_video_production_batches
			SET status = $3, total_items = $4, succeeded_items = $5,
			    failed_items = $6, cancelled_items = $7,
			    completed_at = COALESCE(completed_at, now()),
			    updated_at = now(), revision = revision + 1,
			    metadata = metadata || jsonb_build_object(
			      'reconciliationHash', $8::text,
			      'reconciliationVersion', 2,
			      'reconciledAt', now()
			    )
			WHERE id = $1 AND revision = $2
		`, projection.ID, projection.Revision, status, projection.Total,
			projection.Succeeded, projection.Failed, projection.Cancelled, hash)
		if err != nil {
			return false, err
		}
		if command.RowsAffected() != 1 {
			return false, fmt.Errorf("episode video batch %s changed while it was being reconciled", projection.ID)
		}
		changed = true
	}
	return changed, nil
}

func summarizeEpisodeVideoReconciliation(
	checkpointStatus string,
	outcomes []episodeVideoNormalizedOutcome,
) episodeVideoReconciliationSummary {
	summary := episodeVideoReconciliationSummary{Outcomes: outcomes}
	for _, outcome := range outcomes {
		switch outcome.Status {
		case "succeeded":
			summary.SucceededCount++
		case "cancelled":
			summary.CancelledCount++
		default:
			summary.FailedCount++
		}
		if outcome.Diagnostic != "" {
			summary.DiagnosticCount++
		}
	}
	if checkpointStatus == "cancelled" || checkpointStatus == "cancelling" {
		summary.Status = "cancelled"
		return summary
	}
	if checkpointStatus == "failed" {
		if summary.SucceededCount > 0 {
			summary.Status = "partial_succeeded"
		} else {
			summary.Status = "failed"
		}
		return summary
	}
	if checkpointStatus == "partial_succeeded" {
		if summary.SucceededCount > 0 {
			summary.Status = "partial_succeeded"
		} else if summary.CancelledCount == len(outcomes) {
			summary.Status = "cancelled"
		} else {
			summary.Status = "failed"
		}
		return summary
	}
	summary.Status = episodeVideoAggregateStatus(len(outcomes), summary.SucceededCount, summary.FailedCount, summary.CancelledCount)
	return summary
}

func episodeVideoAggregateStatus(total, succeeded, failed, cancelled int) string {
	if total <= 0 {
		return "failed"
	}
	if succeeded == total {
		return "succeeded"
	}
	if succeeded > 0 {
		return "partial_succeeded"
	}
	if cancelled == total {
		return "cancelled"
	}
	if failed > 0 || cancelled > 0 {
		return "failed"
	}
	return "failed"
}

func loadEpisodeVideoCheckpointOutputV2(
	ctx context.Context,
	query episodeVideoOutputQuerier,
	plan EpisodeVideoProductionPlan,
) (BatchShotProductionOutput, error) {
	var status string
	var targetRaw, metadataRaw json.RawMessage
	if err := query.QueryRow(ctx, `
		SELECT status, metadata->'targetShotIds', metadata
		FROM episode_video_production_checkpoints
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND production_generation_id = $4
		  AND video_production_binding_id = $5
		  AND video_production_binding_revision = $6
		  AND workflow_run_id = $7
	`, plan.CheckpointID, plan.OrganizationID, plan.ProjectID, plan.ProductionGenerationID,
		plan.VideoProductionBindingID, plan.VideoProductionBindingRevision, plan.WorkflowRunID).Scan(
		&status, &targetRaw, &metadataRaw,
	); err != nil {
		return BatchShotProductionOutput{}, err
	}
	if !isTerminalEpisodeVideoCheckpoint(status) {
		return BatchShotProductionOutput{}, fmt.Errorf("episode video checkpoint %s is not terminal", plan.CheckpointID)
	}
	var targetShotIDs []string
	if err := json.Unmarshal(targetRaw, &targetShotIDs); err != nil || len(targetShotIDs) == 0 {
		return BatchShotProductionOutput{}, fmt.Errorf("episode video checkpoint target list is invalid")
	}
	var metadata map[string]any
	if err := json.Unmarshal(metadataRaw, &metadata); err != nil {
		return BatchShotProductionOutput{}, err
	}
	output := newBatchShotVideoOutput(TextToStoryboardInput{WorkflowRunID: plan.WorkflowRunID}, targetShotIDs)
	if err := loadEpisodeVideoBatchOutputDetails(ctx, query, plan.CheckpointID, &output); err != nil {
		return BatchShotProductionOutput{}, err
	}
	output.SucceededShotIDs = nil
	output.FailedShotIDs = nil
	output.CancelledShotIDs = nil
	output.ProviderAsyncTaskIDs = map[string]string{}
	output.Errors = map[string]string{}
	output.ErrorCodes = map[string]string{}

	durable, err := loadEpisodeVideoDurableOutcomes(ctx, query, plan.CheckpointID, targetShotIDs)
	if err != nil {
		return BatchShotProductionOutput{}, err
	}
	snapshot := episodeVideoCheckpointSnapshot{Status: status, TargetShotIDs: targetShotIDs, Metadata: metadata}
	normalized := normalizeEpisodeVideoOutcomes(snapshot, durable)
	summary := summarizeEpisodeVideoReconciliation(status, normalized)
	for index, result := range normalized {
		switch result.Status {
		case "succeeded":
			output.SucceededShotIDs = append(output.SucceededShotIDs, result.ShotID)
		case "cancelled":
			output.CancelledShotIDs = append(output.CancelledShotIDs, result.ShotID)
		default:
			output.FailedShotIDs = append(output.FailedShotIDs, result.ShotID)
		}
		if result.ErrorCode != "" {
			output.ErrorCodes[result.ShotID] = result.ErrorCode
		}
		if result.ErrorMessage != "" {
			output.Errors[result.ShotID] = result.ErrorMessage
		}
		for taskIndex, taskID := range durable[index].ProviderTaskIDs {
			if taskIndex == 0 {
				output.ProviderAsyncTaskIDs[result.ShotID] = taskID
			}
			output.ProviderAsyncTaskIDs[fmt.Sprintf("%s:%d", result.ShotID, taskIndex)] = taskID
		}
	}
	output.Status = summary.Status
	return output, nil
}

func loadEpisodeVideoBatchOutputDetails(
	ctx context.Context,
	query episodeVideoOutputQuerier,
	checkpointID string,
	output *BatchShotProductionOutput,
) error {
	rows, err := query.Query(ctx, `
		SELECT metadata->'batchOutput'
		FROM episode_video_production_batches
		WHERE checkpoint_id = $1 AND metadata ? 'batchOutput'
		ORDER BY ordinal, attempt
	`, checkpointID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw json.RawMessage
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		var batch BatchShotProductionOutput
		if err := json.Unmarshal(raw, &batch); err != nil {
			return err
		}
		mergeBatchShotVideoOutput(output, batch)
	}
	return rows.Err()
}

// ReconcileStuckEpisodeVideoProductionCheckpoints only selects checkpoints whose
// workflow is terminal, or whose durable item/provider work is already terminal.
// It never takes over a live checkpoint with an active provider task.
func ReconcileStuckEpisodeVideoProductionCheckpoints(
	ctx context.Context,
	pool *pgxpool.Pool,
	staleAfter time.Duration,
	limit int,
) (int, error) {
	if pool == nil {
		return 0, nil
	}
	if staleAfter <= 0 {
		staleAfter = 10 * time.Minute
	}
	if limit <= 0 {
		limit = 32
	}
	rows, err := pool.Query(ctx, `
		SELECT checkpoint.id::text, checkpoint.organization_id::text, checkpoint.project_id::text,
		       COALESCE(checkpoint.workflow_run_id::text, ''), checkpoint.script_episode_id::text,
		       checkpoint.production_generation_id::text, checkpoint.video_production_binding_id::text,
		       checkpoint.video_production_binding_revision, checkpoint.profile_version_id::text,
		       checkpoint.profile_snapshot_hash, checkpoint.temporal_workflow_id
		FROM episode_video_production_checkpoints checkpoint
		LEFT JOIN workflow_runs run ON run.id = checkpoint.workflow_run_id
		WHERE checkpoint.status IN ('queued', 'running', 'cancelling')
		  AND checkpoint.workflow_run_id IS NOT NULL
		  AND checkpoint.updated_at <= now() - $1::interval
		  AND (
		    run.status IN ('succeeded', 'failed', 'cancelled')
		    OR (
		      EXISTS (
		        SELECT 1 FROM episode_video_production_batches batch
		        JOIN episode_video_production_items item ON item.batch_id = batch.id
		        WHERE batch.checkpoint_id = checkpoint.id
		      )
		      AND NOT EXISTS (
		        SELECT 1 FROM episode_video_production_batches batch
		        JOIN episode_video_production_items item ON item.batch_id = batch.id
		        WHERE batch.checkpoint_id = checkpoint.id AND item.status IN ('queued', 'running', 'cancelling')
		          AND (
		            item.video_render_plan_id IS NULL
		            OR NOT EXISTS (
		              SELECT 1 FROM video_render_segments terminal_segment
		              WHERE terminal_segment.video_render_plan_id = item.video_render_plan_id
		            )
		            OR EXISTS (
		              SELECT 1 FROM video_render_segments active_segment
		              WHERE active_segment.video_render_plan_id = item.video_render_plan_id
		                AND active_segment.status IN ('planned', 'queued', 'running')
		            )
		          )
		      )
		      AND NOT EXISTS (
		        SELECT 1 FROM episode_video_production_batches batch
		        JOIN episode_video_production_items item ON item.batch_id = batch.id
		        JOIN video_render_segments segment ON segment.video_render_plan_id = item.video_render_plan_id
		        JOIN provider_async_tasks task ON task.id = segment.provider_async_task_id
		        WHERE batch.checkpoint_id = checkpoint.id
		          AND task.status IN ('queued', 'running', 'cancelling')
		      )
		    )
		  )
		ORDER BY checkpoint.updated_at, checkpoint.id
		LIMIT $2
	`, postgresInterval(staleAfter), limit)
	if err != nil {
		return 0, err
	}
	plans := make([]EpisodeVideoProductionPlan, 0, limit)
	for rows.Next() {
		var plan EpisodeVideoProductionPlan
		if err := rows.Scan(
			&plan.CheckpointID, &plan.OrganizationID, &plan.ProjectID, &plan.WorkflowRunID,
			&plan.ScriptEpisodeID, &plan.ProductionGenerationID, &plan.VideoProductionBindingID,
			&plan.VideoProductionBindingRevision, &plan.ProductionProfileVersionID,
			&plan.ProductionProfileSnapshotHash, &plan.TemporalWorkflowID,
		); err != nil {
			rows.Close()
			return 0, err
		}
		plans = append(plans, plan)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	activities := Activities{db: pool}
	reconciled := 0
	for _, plan := range plans {
		if strings.TrimSpace(plan.WorkflowRunID) == "" {
			continue
		}
		if err := activities.reconcileEpisodeVideoProductionCheckpointV2(ctx, plan); err != nil {
			return reconciled, err
		}
		reconciled++
	}
	return reconciled, nil
}

func postgresInterval(duration time.Duration) string {
	seconds := duration.Seconds()
	if seconds < 1 {
		seconds = 1
	}
	return fmt.Sprintf("%.0f seconds", seconds)
}
