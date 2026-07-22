package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
)

type WorkflowVideoProductionActivity struct {
	WorkflowRunID  string                             `json:"workflowRunId"`
	Checkpoints    []EpisodeVideoProductionCheckpoint `json:"checkpoints"`
	TotalItems     int                                `json:"totalItems"`
	SucceededItems int                                `json:"succeededItems"`
	FailedItems    int                                `json:"failedItems"`
	ActiveItems    int                                `json:"activeItems"`
}

type EpisodeVideoProductionCheckpoint struct {
	ID                             string                        `json:"id"`
	ProductionGenerationID         string                        `json:"productionGenerationId"`
	VideoProductionBindingID       string                        `json:"videoProductionBindingId"`
	VideoProductionBindingRevision int64                         `json:"videoProductionBindingRevision"`
	ScriptEpisodeID                string                        `json:"scriptEpisodeId"`
	EpisodeIndex                   int                           `json:"episodeIndex"`
	EpisodeTitle                   string                        `json:"episodeTitle"`
	ProfileVersionID               string                        `json:"profileVersionId"`
	ProfileSnapshotHash            string                        `json:"profileSnapshotHash"`
	TemporalWorkflowID             string                        `json:"temporalWorkflowId"`
	TemporalRunID                  *string                       `json:"temporalRunId,omitempty"`
	Status                         string                        `json:"status"`
	NextBatchOrdinal               int                           `json:"nextBatchOrdinal"`
	Revision                       int64                         `json:"revision"`
	Metadata                       json.RawMessage               `json:"metadata"`
	CreatedAt                      time.Time                     `json:"createdAt"`
	UpdatedAt                      time.Time                     `json:"updatedAt"`
	CompletedAt                    *time.Time                    `json:"completedAt,omitempty"`
	Batches                        []EpisodeVideoProductionBatch `json:"batches"`
}

type EpisodeVideoProductionBatch struct {
	ID                     string                       `json:"id"`
	CheckpointID           string                       `json:"checkpointId"`
	Ordinal                int                          `json:"ordinal"`
	DependencySnapshotHash string                       `json:"dependencySnapshotHash"`
	WorkflowRunID          *string                      `json:"workflowRunId,omitempty"`
	TemporalWorkflowID     *string                      `json:"temporalWorkflowId,omitempty"`
	TemporalRunID          *string                      `json:"temporalRunId,omitempty"`
	Status                 string                       `json:"status"`
	Attempt                int                          `json:"attempt"`
	TotalItems             int                          `json:"totalItems"`
	SucceededItems         int                          `json:"succeededItems"`
	FailedItems            int                          `json:"failedItems"`
	CancelledItems         int                          `json:"cancelledItems"`
	Revision               int64                        `json:"revision"`
	Metadata               json.RawMessage              `json:"metadata"`
	CreatedAt              time.Time                    `json:"createdAt"`
	UpdatedAt              time.Time                    `json:"updatedAt"`
	StartedAt              *time.Time                   `json:"startedAt,omitempty"`
	CompletedAt            *time.Time                   `json:"completedAt,omitempty"`
	Items                  []EpisodeVideoProductionItem `json:"items"`
}

type EpisodeVideoProductionItem struct {
	ID                           string                          `json:"id"`
	BatchID                      string                          `json:"batchId"`
	StoryboardShotID             string                          `json:"storyboardShotId"`
	ShotNo                       int                             `json:"shotNo"`
	ShotTitle                    string                          `json:"shotTitle"`
	ShotStateHash                string                          `json:"shotStateHash"`
	ExecutionIdentityVersion     int                             `json:"executionIdentityVersion"`
	PredecessorVideoRenderPlanID *string                         `json:"predecessorVideoRenderPlanId,omitempty"`
	ReferencePackID              *string                         `json:"referencePackId,omitempty"`
	ReferencePackStatus          *string                         `json:"referencePackStatus,omitempty"`
	VideoPromptPlanID            *string                         `json:"videoPromptPlanId,omitempty"`
	VideoPromptPlanStatus        *string                         `json:"videoPromptPlanStatus,omitempty"`
	VideoPromptPlanRevision      *int                            `json:"videoPromptPlanRevision,omitempty"`
	VideoRenderPlanID            *string                         `json:"videoRenderPlanId,omitempty"`
	VideoRenderPlanStatus        *string                         `json:"videoRenderPlanStatus,omitempty"`
	ProviderAsyncTaskID          *string                         `json:"providerAsyncTaskId,omitempty"`
	ProviderAsyncTaskStatus      *string                         `json:"providerAsyncTaskStatus,omitempty"`
	ExternalTaskID               *string                         `json:"externalTaskId,omitempty"`
	ProviderPollCount            *int                            `json:"providerPollCount,omitempty"`
	ProviderErrorCode            *string                         `json:"providerErrorCode,omitempty"`
	ProviderErrorMessage         *string                         `json:"providerErrorMessage,omitempty"`
	AnchorID                     *string                         `json:"anchorId,omitempty"`
	AnchorStatus                 *string                         `json:"anchorStatus,omitempty"`
	AnchorReviewStatus           *string                         `json:"anchorReviewStatus,omitempty"`
	MediaStatus                  string                          `json:"mediaStatus"`
	Status                       string                          `json:"status"`
	Attempt                      int                             `json:"attempt"`
	Revision                     int64                           `json:"revision"`
	ErrorCode                    *string                         `json:"errorCode,omitempty"`
	ErrorDetail                  json.RawMessage                 `json:"errorDetail"`
	Metadata                     json.RawMessage                 `json:"metadata"`
	CreatedAt                    time.Time                       `json:"createdAt"`
	UpdatedAt                    time.Time                       `json:"updatedAt"`
	StartedAt                    *time.Time                      `json:"startedAt,omitempty"`
	CompletedAt                  *time.Time                      `json:"completedAt,omitempty"`
	Segments                     []EpisodeVideoProductionSegment `json:"segments"`
}

type EpisodeVideoProductionSegment struct {
	ID                       string                               `json:"id"`
	SegmentIndex             int                                  `json:"segmentIndex"`
	Status                   string                               `json:"status"`
	InputContractKey         *string                              `json:"inputContractKey,omitempty"`
	RequestedDurationSeconds float64                              `json:"requestedDurationSeconds"`
	ProviderTasks            []EpisodeVideoProductionProviderTask `json:"providerTasks"`
}

type EpisodeVideoProductionProviderTask struct {
	ID             string     `json:"id"`
	Status         string     `json:"status"`
	ExternalTaskID *string    `json:"externalTaskId,omitempty"`
	PollCount      int        `json:"pollCount"`
	ErrorCode      *string    `json:"errorCode,omitempty"`
	ErrorMessage   *string    `json:"errorMessage,omitempty"`
	RequestHash    *string    `json:"requestHash,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"`
}

func (s *Server) getWorkflowVideoProductionActivity(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	run, err := scanWorkflowRun(s.db.QueryRow(r.Context(), workflowRunSelectSQL(`WHERE id = $1`), r.PathValue("workflowRunId")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionWorkflowRead, authz.Resource{ProjectID: run.ProjectID}) {
		return
	}
	result := WorkflowVideoProductionActivity{WorkflowRunID: run.ID, Checkpoints: make([]EpisodeVideoProductionCheckpoint, 0)}
	checkpointRows, err := s.db.Query(r.Context(), `
		SELECT checkpoint.id::text, checkpoint.production_generation_id::text,
		       checkpoint.video_production_binding_id::text,
		       checkpoint.video_production_binding_revision,
		       checkpoint.script_episode_id::text, episode.episode_index, episode.episode_title,
		       checkpoint.profile_version_id::text,
		       checkpoint.profile_snapshot_hash, checkpoint.temporal_workflow_id,
		       checkpoint.temporal_run_id, checkpoint.status, checkpoint.next_batch_ordinal,
		       checkpoint.revision, checkpoint.metadata, checkpoint.created_at,
		       checkpoint.updated_at, checkpoint.completed_at
		FROM episode_video_production_checkpoints checkpoint
		JOIN script_episodes episode ON episode.id = checkpoint.script_episode_id
		WHERE checkpoint.project_id = $1
		  AND (
		    checkpoint.workflow_run_id = $2
		    OR EXISTS (
		      SELECT 1 FROM episode_video_production_batches batch
		      WHERE batch.checkpoint_id = checkpoint.id AND batch.workflow_run_id = $2
		    )
		  )
		ORDER BY checkpoint.created_at DESC
	`, run.ProjectID, run.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer checkpointRows.Close()
	checkpointIndex := make(map[string]int)
	for checkpointRows.Next() {
		var item EpisodeVideoProductionCheckpoint
		if err := checkpointRows.Scan(
			&item.ID, &item.ProductionGenerationID, &item.VideoProductionBindingID,
			&item.VideoProductionBindingRevision, &item.ScriptEpisodeID, &item.EpisodeIndex,
			&item.EpisodeTitle, &item.ProfileVersionID,
			&item.ProfileSnapshotHash, &item.TemporalWorkflowID, &item.TemporalRunID,
			&item.Status, &item.NextBatchOrdinal, &item.Revision, &item.Metadata,
			&item.CreatedAt, &item.UpdatedAt, &item.CompletedAt,
		); err != nil {
			s.writeError(w, r, err)
			return
		}
		item.Batches = make([]EpisodeVideoProductionBatch, 0)
		checkpointIndex[item.ID] = len(result.Checkpoints)
		result.Checkpoints = append(result.Checkpoints, item)
	}
	if err := checkpointRows.Err(); err != nil {
		s.writeError(w, r, err)
		return
	}
	if len(result.Checkpoints) == 0 {
		httpx.WriteJSON(w, r, http.StatusOK, result, nil)
		return
	}

	batchRows, err := s.db.Query(r.Context(), `
		SELECT batch.id::text, batch.checkpoint_id::text, batch.ordinal,
		       batch.dependency_snapshot_hash, batch.workflow_run_id::text,
		       batch.temporal_workflow_id, batch.temporal_run_id, batch.status,
		       batch.attempt, batch.total_items, batch.succeeded_items,
		       batch.failed_items, batch.cancelled_items, batch.revision,
		       batch.metadata, batch.created_at, batch.updated_at,
		       batch.started_at, batch.completed_at
		FROM episode_video_production_batches batch
		JOIN episode_video_production_checkpoints checkpoint ON checkpoint.id = batch.checkpoint_id
		WHERE checkpoint.project_id = $1
		  AND (checkpoint.workflow_run_id = $2 OR batch.workflow_run_id = $2)
		ORDER BY checkpoint.created_at DESC, batch.ordinal, batch.attempt
	`, run.ProjectID, run.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer batchRows.Close()
	type batchLocation struct{ checkpointIndex, batchIndex int }
	batchIndex := make(map[string]batchLocation)
	for batchRows.Next() {
		var item EpisodeVideoProductionBatch
		if err := batchRows.Scan(
			&item.ID, &item.CheckpointID, &item.Ordinal, &item.DependencySnapshotHash,
			&item.WorkflowRunID, &item.TemporalWorkflowID, &item.TemporalRunID,
			&item.Status, &item.Attempt, &item.TotalItems, &item.SucceededItems,
			&item.FailedItems, &item.CancelledItems, &item.Revision, &item.Metadata,
			&item.CreatedAt, &item.UpdatedAt, &item.StartedAt, &item.CompletedAt,
		); err != nil {
			s.writeError(w, r, err)
			return
		}
		item.Items = make([]EpisodeVideoProductionItem, 0)
		checkpointPosition, exists := checkpointIndex[item.CheckpointID]
		if !exists {
			continue
		}
		batchPosition := len(result.Checkpoints[checkpointPosition].Batches)
		result.Checkpoints[checkpointPosition].Batches = append(result.Checkpoints[checkpointPosition].Batches, item)
		batchIndex[item.ID] = batchLocation{checkpointIndex: checkpointPosition, batchIndex: batchPosition}
	}
	if err := batchRows.Err(); err != nil {
		s.writeError(w, r, err)
		return
	}

	itemRows, err := s.db.Query(r.Context(), `
		SELECT item.id::text, item.batch_id::text, item.storyboard_shot_id::text,
		       COALESCE(shot.shot_no, shot.shot_index + 1), COALESCE(shot.title, ''),
		       item.shot_state_hash, item.execution_identity_version,
		       item.predecessor_video_render_plan_id::text,
		       item.reference_pack_id::text, reference.status,
		       item.video_prompt_plan_id::text, prompt.status, prompt.revision,
		       render.id::text, render.status,
		       provider_task.id::text, provider_task.status,
		       provider_task.external_task_id, provider_task.poll_count,
		       provider_task.error_code, provider_task.error_message,
		       anchor.id::text, anchor.status, anchor.review_status,
		       CASE
		         WHEN item.status = 'succeeded'
		          AND render.status = 'succeeded'
		          AND render.output_artifact_id IS NOT NULL
		          AND render.output_media_file_id IS NOT NULL THEN 'stored'
		         WHEN item.status IN ('failed', 'cancelled', 'discarded')
		           OR render.status IN ('failed', 'cancelled', 'replan_required')
		           OR provider_task.status IN ('failed', 'cancelled') THEN 'failed'
		         WHEN provider_task.status = 'succeeded' THEN 'transferring'
		         ELSE 'pending'
		       END,
		       item.status, item.attempt, item.revision, item.error_code,
		       item.error_detail, item.metadata, item.created_at, item.updated_at,
		       item.started_at, item.completed_at
		FROM episode_video_production_items item
		JOIN episode_video_production_batches batch ON batch.id = item.batch_id
		JOIN episode_video_production_checkpoints checkpoint ON checkpoint.id = batch.checkpoint_id
		JOIN storyboard_shots shot ON shot.id = item.storyboard_shot_id
		LEFT JOIN shot_reference_packs reference ON reference.id = item.reference_pack_id
		LEFT JOIN video_prompt_plans prompt ON prompt.id = item.video_prompt_plan_id
		LEFT JOIN LATERAL (
		  SELECT candidate.id, candidate.status,
		         candidate.output_artifact_id, candidate.output_media_file_id
		  FROM video_render_plans candidate
		  WHERE candidate.organization_id = checkpoint.organization_id
		    AND candidate.project_id = checkpoint.project_id
		    AND candidate.storyboard_shot_id = item.storyboard_shot_id
		    AND (
		      (item.execution_identity_version = 2
		       AND candidate.id = item.video_render_plan_id
		       AND candidate.operation_item_id = item.id
		       AND candidate.operation_item_attempt = item.attempt)
		      OR
		      (item.execution_identity_version = 1
		       AND candidate.workflow_run_id = COALESCE(batch.workflow_run_id, checkpoint.workflow_run_id)
		       AND candidate.production_generation_id = checkpoint.production_generation_id
		       AND candidate.video_production_binding_id = checkpoint.video_production_binding_id
		       AND candidate.video_production_binding_revision = checkpoint.video_production_binding_revision)
		    )
		  ORDER BY candidate.created_at DESC, candidate.id DESC
		  LIMIT 1
		) render ON true
		LEFT JOIN LATERAL (
		  SELECT candidate.id, candidate.status, candidate.external_task_id,
		         candidate.poll_count, candidate.error_code, candidate.error_message
		  FROM (
		    SELECT segment.provider_async_task_id AS id, true AS plan_linked
		    FROM video_render_segments segment
		    WHERE segment.video_render_plan_id = render.id
		      AND segment.storyboard_shot_id = item.storyboard_shot_id
		      AND segment.provider_async_task_id IS NOT NULL
		    UNION ALL
		    SELECT item.provider_async_task_id, false
		    WHERE item.execution_identity_version = 1 AND item.provider_async_task_id IS NOT NULL
		  ) task_ref
		  JOIN provider_async_tasks candidate ON candidate.id = task_ref.id
		  WHERE task_ref.plan_linked OR (
		      candidate.workflow_run_id = COALESCE(batch.workflow_run_id, checkpoint.workflow_run_id)
		      AND candidate.production_generation_id = checkpoint.production_generation_id
		      AND candidate.video_production_binding_id = checkpoint.video_production_binding_id
		      AND candidate.video_production_binding_revision = checkpoint.video_production_binding_revision
		  )
		  ORDER BY CASE candidate.status
		             WHEN 'running' THEN 0
		             WHEN 'queued' THEN 1
		             WHEN 'cancelling' THEN 2
		             WHEN 'failed' THEN 3
		             WHEN 'cancelled' THEN 4
		             WHEN 'succeeded' THEN 5
		             ELSE 6
		           END,
		           candidate.created_at DESC, candidate.id DESC
		  LIMIT 1
		) provider_task ON true
		LEFT JOIN LATERAL (
		  SELECT candidate.id, candidate.status, candidate.review_status
		  FROM shot_visual_anchors candidate
		  WHERE candidate.storyboard_shot_id = item.storyboard_shot_id
		    AND candidate.anchor_role = 'planned_first_frame'
		  ORDER BY candidate.revision DESC LIMIT 1
		) anchor ON true
		WHERE checkpoint.project_id = $1
		  AND (checkpoint.workflow_run_id = $2 OR batch.workflow_run_id = $2)
		ORDER BY checkpoint.created_at DESC, batch.ordinal, shot.start_tick, item.attempt
	`, run.ProjectID, run.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer itemRows.Close()
	type itemLocation struct{ checkpointIndex, batchIndex, itemIndex int }
	itemIndex := make(map[string]itemLocation)
	for itemRows.Next() {
		var item EpisodeVideoProductionItem
		if err := itemRows.Scan(
			&item.ID, &item.BatchID, &item.StoryboardShotID, &item.ShotNo, &item.ShotTitle,
			&item.ShotStateHash, &item.ExecutionIdentityVersion, &item.PredecessorVideoRenderPlanID,
			&item.ReferencePackID, &item.ReferencePackStatus,
			&item.VideoPromptPlanID, &item.VideoPromptPlanStatus, &item.VideoPromptPlanRevision,
			&item.VideoRenderPlanID, &item.VideoRenderPlanStatus,
			&item.ProviderAsyncTaskID, &item.ProviderAsyncTaskStatus, &item.ExternalTaskID,
			&item.ProviderPollCount, &item.ProviderErrorCode, &item.ProviderErrorMessage,
			&item.AnchorID, &item.AnchorStatus, &item.AnchorReviewStatus, &item.MediaStatus,
			&item.Status, &item.Attempt, &item.Revision, &item.ErrorCode, &item.ErrorDetail,
			&item.Metadata, &item.CreatedAt, &item.UpdatedAt, &item.StartedAt, &item.CompletedAt,
		); err != nil {
			s.writeError(w, r, err)
			return
		}
		location, exists := batchIndex[item.BatchID]
		if !exists {
			continue
		}
		batch := &result.Checkpoints[location.checkpointIndex].Batches[location.batchIndex]
		item.Segments = make([]EpisodeVideoProductionSegment, 0)
		itemIndex[item.ID] = itemLocation{checkpointIndex: location.checkpointIndex, batchIndex: location.batchIndex, itemIndex: len(batch.Items)}
		batch.Items = append(batch.Items, item)
		result.TotalItems++
		switch item.Status {
		case "succeeded":
			result.SucceededItems++
		case "failed", "discarded":
			result.FailedItems++
		case "queued", "running", "cancelling":
			result.ActiveItems++
		}
	}
	if err := itemRows.Err(); err != nil {
		s.writeError(w, r, err)
		return
	}

	segmentRows, err := s.db.Query(r.Context(), `
		SELECT item.id::text, segment.id::text, segment.segment_index, segment.status,
		       segment.input_contract_key, segment.requested_duration_seconds::float8
		FROM episode_video_production_items item
		JOIN episode_video_production_batches batch ON batch.id = item.batch_id
		JOIN episode_video_production_checkpoints checkpoint ON checkpoint.id = batch.checkpoint_id
		JOIN LATERAL (
		  SELECT candidate.*
		  FROM video_render_plans candidate
		  WHERE candidate.organization_id = checkpoint.organization_id
		    AND candidate.project_id = checkpoint.project_id
		    AND candidate.storyboard_shot_id = item.storyboard_shot_id
		    AND (
		      (item.execution_identity_version = 2
		       AND candidate.id = item.video_render_plan_id
		       AND candidate.operation_item_id = item.id
		       AND candidate.operation_item_attempt = item.attempt)
		      OR
		      (item.execution_identity_version = 1
		       AND candidate.workflow_run_id = COALESCE(batch.workflow_run_id, checkpoint.workflow_run_id)
		       AND candidate.production_generation_id = checkpoint.production_generation_id
		       AND candidate.video_production_binding_id = checkpoint.video_production_binding_id
		       AND candidate.video_production_binding_revision = checkpoint.video_production_binding_revision)
		    )
		  ORDER BY candidate.created_at DESC, candidate.id DESC
		  LIMIT 1
		) plan ON true
		JOIN video_render_segments segment
		  ON segment.video_render_plan_id = plan.id
		 AND segment.storyboard_shot_id = item.storyboard_shot_id
		WHERE checkpoint.project_id = $1
		  AND (checkpoint.workflow_run_id = $2 OR batch.workflow_run_id = $2)
		ORDER BY batch.ordinal, item.created_at, segment.segment_index, segment.id
	`, run.ProjectID, run.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	type segmentLocation struct {
		item         itemLocation
		segmentIndex int
	}
	segmentIndex := make(map[string]segmentLocation)
	for segmentRows.Next() {
		var itemID string
		var segment EpisodeVideoProductionSegment
		if err := segmentRows.Scan(
			&itemID, &segment.ID, &segment.SegmentIndex, &segment.Status,
			&segment.InputContractKey, &segment.RequestedDurationSeconds,
		); err != nil {
			segmentRows.Close()
			s.writeError(w, r, err)
			return
		}
		location, exists := itemIndex[itemID]
		if !exists {
			continue
		}
		segment.ProviderTasks = make([]EpisodeVideoProductionProviderTask, 0)
		item := &result.Checkpoints[location.checkpointIndex].Batches[location.batchIndex].Items[location.itemIndex]
		segmentIndex[segment.ID] = segmentLocation{item: location, segmentIndex: len(item.Segments)}
		item.Segments = append(item.Segments, segment)
	}
	if err := segmentRows.Err(); err != nil {
		segmentRows.Close()
		s.writeError(w, r, err)
		return
	}
	segmentRows.Close()

	taskRows, err := s.db.Query(r.Context(), `
		SELECT segment.id::text, task.id::text, task.status, task.external_task_id,
		       task.poll_count, task.error_code, task.error_message, task.request_hash,
		       task.created_at, task.completed_at
		FROM episode_video_production_items item
		JOIN episode_video_production_batches batch ON batch.id = item.batch_id
		JOIN episode_video_production_checkpoints checkpoint ON checkpoint.id = batch.checkpoint_id
		JOIN LATERAL (
		  SELECT candidate.*
		  FROM video_render_plans candidate
		  WHERE candidate.organization_id = checkpoint.organization_id
		    AND candidate.project_id = checkpoint.project_id
		    AND candidate.storyboard_shot_id = item.storyboard_shot_id
		    AND (
		      (item.execution_identity_version = 2
		       AND candidate.id = item.video_render_plan_id
		       AND candidate.operation_item_id = item.id
		       AND candidate.operation_item_attempt = item.attempt)
		      OR
		      (item.execution_identity_version = 1
		       AND candidate.workflow_run_id = COALESCE(batch.workflow_run_id, checkpoint.workflow_run_id)
		       AND candidate.production_generation_id = checkpoint.production_generation_id
		       AND candidate.video_production_binding_id = checkpoint.video_production_binding_id
		       AND candidate.video_production_binding_revision = checkpoint.video_production_binding_revision)
		    )
		  ORDER BY candidate.created_at DESC, candidate.id DESC
		  LIMIT 1
		) plan ON true
		JOIN video_render_segments segment ON segment.video_render_plan_id = plan.id
		JOIN provider_async_tasks task
		  ON task.video_render_plan_id = plan.id
		 AND task.video_render_segment_id = segment.id
		 AND (
		   (item.execution_identity_version = 2
		    AND task.operation_item_id = item.id
		    AND task.operation_item_attempt = item.attempt)
		   OR
		   (item.execution_identity_version = 1
		    AND task.workflow_run_id = COALESCE(batch.workflow_run_id, checkpoint.workflow_run_id))
		 )
		WHERE checkpoint.project_id = $1
		  AND (checkpoint.workflow_run_id = $2 OR batch.workflow_run_id = $2)
		ORDER BY segment.segment_index, task.created_at, task.id
	`, run.ProjectID, run.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	for taskRows.Next() {
		var segmentID string
		var task EpisodeVideoProductionProviderTask
		if err := taskRows.Scan(
			&segmentID, &task.ID, &task.Status, &task.ExternalTaskID,
			&task.PollCount, &task.ErrorCode, &task.ErrorMessage, &task.RequestHash,
			&task.CreatedAt, &task.CompletedAt,
		); err != nil {
			taskRows.Close()
			s.writeError(w, r, err)
			return
		}
		location, exists := segmentIndex[segmentID]
		if !exists {
			continue
		}
		item := &result.Checkpoints[location.item.checkpointIndex].Batches[location.item.batchIndex].Items[location.item.itemIndex]
		item.Segments[location.segmentIndex].ProviderTasks = append(item.Segments[location.segmentIndex].ProviderTasks, task)
	}
	if err := taskRows.Err(); err != nil {
		taskRows.Close()
		s.writeError(w, r, err)
		return
	}
	taskRows.Close()
	httpx.WriteJSON(w, r, http.StatusOK, result, nil)
}
