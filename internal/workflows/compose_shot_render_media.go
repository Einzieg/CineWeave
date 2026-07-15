package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mediapkg "github.com/Einzieg/cineweave/internal/media"
)

type ComposeShotRenderPlanMediaInput struct {
	OrganizationID  string `json:"organizationId"`
	ProjectID       string `json:"projectId"`
	WorkflowRunID   string `json:"workflowRunId"`
	CreatedBy       string `json:"createdBy,omitempty"`
	ExecutionPlanID string `json:"executionPlanId"`
	ShotID          string `json:"shotId"`
}

type ComposeShotRenderPlanMediaOutput struct {
	WorkflowRunID       string               `json:"workflowRunId,omitempty"`
	ExecutionPlanID     string               `json:"executionPlanId"`
	ShotID              string               `json:"shotId"`
	ArtifactID          string               `json:"artifactId"`
	MediaFileID         string               `json:"mediaFileId"`
	StorageKey          string               `json:"storageKey"`
	MimeType            string               `json:"mimeType"`
	DurationSeconds     float64              `json:"durationSeconds"`
	NativeAudioStatus   string               `json:"nativeAudioStatus"`
	ProductionReadiness string               `json:"productionReadiness"`
	Probe               mediapkg.ProbeResult `json:"probe"`
}

type renderPlanComposeRecord struct {
	AspectRatio       string
	Resolution        string
	TimelineTimebase  int64
	FPSNumerator      int
	FPSDenominator    int
	NativeAudioStatus string
	Readiness         string
	OutputArtifactID  string
	OutputMediaFileID string
	OutputStorageKey  string
	OutputMimeType    string
	OutputDuration    float64
	OutputProbe       []byte
}

func (a Activities) ComposeShotRenderPlanMedia(ctx context.Context, input ComposeShotRenderPlanMediaInput) (_ ComposeShotRenderPlanMediaOutput, err error) {
	var execution NodeExecution
	defer func() {
		err = finalizeWorkflowActivityError(ctx, a.db, execution, err)
	}()
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.ExecutionPlanID) == "" || strings.TrimSpace(input.ShotID) == "" {
		return ComposeShotRenderPlanMediaOutput{}, fmt.Errorf("organizationId, projectId, workflowRunId, executionPlanId, and shotId are required")
	}
	plan, err := a.loadRenderPlanComposeRecord(ctx, input)
	if err != nil {
		return ComposeShotRenderPlanMediaOutput{}, err
	}
	if plan.OutputArtifactID != "" && plan.OutputMediaFileID != "" && plan.OutputStorageKey != "" {
		return composeShotRenderOutput(input, plan), nil
	}
	objectStore, ok := a.storage.(mediapkg.ObjectStore)
	if !ok {
		return ComposeShotRenderPlanMediaOutput{}, fmt.Errorf("object storage does not support shot render composition")
	}
	clips, err := a.loadRenderPlanComposeClips(ctx, input, plan.TimelineTimebase)
	if err != nil {
		return ComposeShotRenderPlanMediaOutput{}, err
	}
	execution, err = StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKeyForID("video_render_plan_compose", input.ExecutionPlanID),
		NodeType:       "media.video.compose",
		Input:          mustJSON(input),
	})
	if err != nil {
		return ComposeShotRenderPlanMediaOutput{}, err
	}
	result, err := mediapkg.ComposeClipsWithStore(ctx, mediapkg.ComposeRequest{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		Clips: clips, AspectRatio: plan.AspectRatio, Resolution: plan.Resolution,
		FPSNumerator: plan.FPSNumerator, FPSDenominator: plan.FPSDenominator, OutputMimeType: "video/mp4",
		OutputStorageKey: fmt.Sprintf("organizations/%s/projects/%s/video-render-plans/%s/shot.mp4", input.OrganizationID, input.ProjectID, input.ExecutionPlanID),
	}, objectStore)
	if err != nil {
		_ = FailNodeRun(ctx, a.db, execution, codeActivityFailed, err.Error())
		return ComposeShotRenderPlanMediaOutput{}, err
	}
	return a.persistShotRenderPlanMedia(ctx, input, execution, plan, result)
}

func (a Activities) loadRenderPlanComposeRecord(ctx context.Context, input ComposeShotRenderPlanMediaInput) (renderPlanComposeRecord, error) {
	var record renderPlanComposeRecord
	err := a.db.QueryRow(ctx, `
		SELECT plan.aspect_ratio, plan.resolution, plan.timeline_timebase, plan.fps_numerator, plan.fps_denominator,
		       plan.native_audio_status, plan.production_readiness,
		       COALESCE(plan.output_artifact_id::text, ''), COALESCE(plan.output_media_file_id::text, ''), COALESCE(plan.output_storage_key, ''),
		       COALESCE(media.mime_type, artifact.mime_type, 'video/mp4'), COALESCE(media.duration_seconds, 0),
		       COALESCE(artifact.metadata->'mediaProbe', 'null'::jsonb)
		FROM video_render_plans plan
		LEFT JOIN artifacts artifact ON artifact.id = plan.output_artifact_id
		LEFT JOIN media_files media ON media.id = plan.output_media_file_id
		WHERE plan.id = $1 AND plan.storyboard_shot_id = $2 AND plan.project_id = $3 AND plan.organization_id = $4
	`, input.ExecutionPlanID, input.ShotID, input.ProjectID, input.OrganizationID).Scan(
		&record.AspectRatio, &record.Resolution, &record.TimelineTimebase, &record.FPSNumerator, &record.FPSDenominator,
		&record.NativeAudioStatus, &record.Readiness, &record.OutputArtifactID, &record.OutputMediaFileID, &record.OutputStorageKey,
		&record.OutputMimeType, &record.OutputDuration, &record.OutputProbe,
	)
	return record, err
}

func (a Activities) loadRenderPlanComposeClips(ctx context.Context, input ComposeShotRenderPlanMediaInput, timebase int64) ([]mediapkg.Clip, error) {
	rows, err := a.db.Query(ctx, `
		SELECT segment.segment_index, COALESCE(segment.storage_key, artifact.storage_key, ''), segment.planned_duration_ticks, segment.status
		FROM video_render_segments segment
		LEFT JOIN artifacts artifact ON artifact.id = segment.raw_av_artifact_id
		WHERE segment.video_render_plan_id = $1 AND segment.storyboard_shot_id = $2
		ORDER BY segment.segment_index
	`, input.ExecutionPlanID, input.ShotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	clips := make([]mediapkg.Clip, 0)
	for rows.Next() {
		var index int
		var storageKey, status string
		var durationTicks int64
		if err := rows.Scan(&index, &storageKey, &durationTicks, &status); err != nil {
			return nil, err
		}
		if status != "succeeded" || strings.TrimSpace(storageKey) == "" || durationTicks <= 0 || timebase <= 0 {
			return nil, fmt.Errorf("render segment %d is not ready for composition", index)
		}
		duration := float64(durationTicks) / float64(timebase)
		clips = append(clips, mediapkg.Clip{ShotID: input.ShotID, ShotIndex: index, StorageKey: storageKey, MimeType: "video/mp4", DurationSeconds: duration, TrimEndSeconds: &duration, TargetDurationSeconds: &duration})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(clips) == 0 {
		return nil, fmt.Errorf("render plan has no succeeded segments")
	}
	return clips, nil
}

func (a Activities) persistShotRenderPlanMedia(ctx context.Context, input ComposeShotRenderPlanMediaInput, execution NodeExecution, plan renderPlanComposeRecord, result mediapkg.ComposeResult) (ComposeShotRenderPlanMediaOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ComposeShotRenderPlanMediaOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return ComposeShotRenderPlanMediaOutput{}, err
	}
	metadata := mustJSON(map[string]any{
		"source": "render_plan_compose", "executionPlanId": input.ExecutionPlanID, "shotId": input.ShotID,
		"mediaProbe": result.Probe, "nativeAudioStatus": plan.NativeAudioStatus, "productionReadiness": plan.Readiness,
	})
	var artifactID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, metadata, created_by)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, 'storyboard_shot_video', $5, $6, $7, $8, NULLIF($9, '')::uuid)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, execution.NodeRunID, result.StorageKey, result.MimeType, result.ContentHash, metadata, input.CreatedBy).Scan(&artifactID); err != nil {
		return ComposeShotRenderPlanMediaOutput{}, err
	}
	var mediaFileID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO media_files(organization_id, project_id, artifact_id, storage_key, mime_type, byte_size, width, height, duration_seconds, checksum, metadata, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, 0), NULLIF($8, 0), NULLIF($9, 0), $10, $11, NULLIF($12, '')::uuid)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, artifactID, result.StorageKey, result.MimeType, result.ByteSize,
		result.Width, result.Height, result.DurationSeconds, result.ContentHash, metadata, input.CreatedBy).Scan(&mediaFileID); err != nil {
		return ComposeShotRenderPlanMediaOutput{}, err
	}
	planTag, err := tx.Exec(ctx, `
		UPDATE video_render_plans
		SET output_artifact_id = $2, output_media_file_id = $3, output_storage_key = $4,
		    metadata = metadata || $5::jsonb, updated_at = now()
		WHERE id = $1 AND active = true
	`, input.ExecutionPlanID, artifactID, mediaFileID, result.StorageKey, metadata)
	if err != nil {
		return ComposeShotRenderPlanMediaOutput{}, err
	}
	if planTag.RowsAffected() != 1 {
		return ComposeShotRenderPlanMediaOutput{}, ErrWorkflowWriteFenced
	}
	shotTag, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET video_artifact_id = $2, video_media_file_id = $3, video_storage_key = $4,
		    status = 'video_succeeded', video_status = 'succeeded', video_error_code = NULL, video_error_message = NULL,
		    video_workflow_run_id = NULLIF($5, '')::uuid, video_completed_at = now(), stale_state = 'fresh',
		    native_audio_status = $6, production_readiness = $7, updated_at = now()
		WHERE id = $1 AND active_video_render_plan_id = $8
	`, input.ShotID, artifactID, mediaFileID, result.StorageKey, input.WorkflowRunID, plan.NativeAudioStatus, plan.Readiness, input.ExecutionPlanID)
	if err != nil {
		return ComposeShotRenderPlanMediaOutput{}, err
	}
	if shotTag.RowsAffected() != 1 {
		return ComposeShotRenderPlanMediaOutput{}, ErrWorkflowWriteFenced
	}
	output := ComposeShotRenderPlanMediaOutput{
		WorkflowRunID:   input.WorkflowRunID,
		ExecutionPlanID: input.ExecutionPlanID, ShotID: input.ShotID, ArtifactID: artifactID, MediaFileID: mediaFileID,
		StorageKey: result.StorageKey, MimeType: result.MimeType, DurationSeconds: result.DurationSeconds,
		NativeAudioStatus: plan.NativeAudioStatus, ProductionReadiness: plan.Readiness, Probe: result.Probe,
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.video.composed", "storyboard_shot", input.ShotID, mustJSON(output)); err != nil {
		return ComposeShotRenderPlanMediaOutput{}, err
	}
	if applied, err := completeNodeRunTx(ctx, tx, execution, mustJSON(output)); err != nil {
		return ComposeShotRenderPlanMediaOutput{}, err
	} else if !applied {
		return ComposeShotRenderPlanMediaOutput{}, ErrWorkflowWriteFenced
	}
	if err := tx.Commit(ctx); err != nil {
		return ComposeShotRenderPlanMediaOutput{}, err
	}
	return output, nil
}

func composeShotRenderOutput(input ComposeShotRenderPlanMediaInput, plan renderPlanComposeRecord) ComposeShotRenderPlanMediaOutput {
	output := ComposeShotRenderPlanMediaOutput{
		WorkflowRunID:   input.WorkflowRunID,
		ExecutionPlanID: input.ExecutionPlanID, ShotID: input.ShotID, ArtifactID: plan.OutputArtifactID,
		MediaFileID: plan.OutputMediaFileID, StorageKey: plan.OutputStorageKey, MimeType: plan.OutputMimeType,
		DurationSeconds: plan.OutputDuration, NativeAudioStatus: plan.NativeAudioStatus, ProductionReadiness: plan.Readiness,
	}
	_ = json.Unmarshal(plan.OutputProbe, &output.Probe)
	return output
}
