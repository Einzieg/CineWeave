package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mediapkg "github.com/Einzieg/cineweave/internal/media"
)

type ProcessRenderSegmentMediaInput struct {
	OrganizationID         string  `json:"organizationId"`
	ProjectID              string  `json:"projectId"`
	WorkflowRunID          string  `json:"workflowRunId"`
	CreatedBy              string  `json:"createdBy,omitempty"`
	ExecutionPlanID        string  `json:"executionPlanId"`
	RenderSegmentID        string  `json:"renderSegmentId"`
	SegmentIndex           int     `json:"segmentIndex"`
	RawArtifactID          string  `json:"rawArtifactId"`
	RawMediaFileID         string  `json:"rawMediaFileId"`
	RawStorageKey          string  `json:"rawStorageKey"`
	RawMimeType            string  `json:"rawMimeType"`
	AspectRatio            string  `json:"aspectRatio"`
	Resolution             string  `json:"resolution"`
	FPSNumerator           int     `json:"fpsNumerator"`
	FPSDenominator         int     `json:"fpsDenominator"`
	PlannedDurationSeconds float64 `json:"plannedDurationSeconds"`
}

type ProcessRenderSegmentMediaOutput struct {
	WorkflowRunID             string                `json:"workflowRunId,omitempty"`
	ExecutionPlanID           string                `json:"executionPlanId"`
	RenderSegmentID           string                `json:"renderSegmentId"`
	RawArtifactID             string                `json:"rawArtifactId"`
	RawMediaFileID            string                `json:"rawMediaFileId"`
	RawStorageKey             string                `json:"rawStorageKey"`
	MezzanineArtifactID       string                `json:"mezzanineArtifactId"`
	MezzanineMediaFileID      string                `json:"mezzanineMediaFileId"`
	MezzanineStorageKey       string                `json:"mezzanineStorageKey"`
	ExtractedAudioArtifactID  string                `json:"extractedAudioArtifactId,omitempty"`
	ExtractedAudioMediaFileID string                `json:"extractedAudioMediaFileId,omitempty"`
	ExtractedAudioStorageKey  string                `json:"extractedAudioStorageKey,omitempty"`
	SourceProbe               mediapkg.ProbeResult  `json:"sourceProbe"`
	MezzanineProbe            mediapkg.ProbeResult  `json:"mezzanineProbe"`
	AudioProbe                *mediapkg.ProbeResult `json:"audioProbe,omitempty"`
}

func (a Activities) ProcessRenderSegmentMedia(ctx context.Context, input ProcessRenderSegmentMediaInput) (_ ProcessRenderSegmentMediaOutput, err error) {
	var execution NodeExecution
	defer func() {
		err = finalizeWorkflowActivityError(ctx, a.db, execution, err)
	}()
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.ExecutionPlanID) == "" || strings.TrimSpace(input.RenderSegmentID) == "" || strings.TrimSpace(input.RawArtifactID) == "" || strings.TrimSpace(input.RawStorageKey) == "" {
		return ProcessRenderSegmentMediaOutput{}, fmt.Errorf("organizationId, projectId, workflowRunId, executionPlanId, renderSegmentId, rawArtifactId, and rawStorageKey are required")
	}
	if existing, ok, err := a.existingRenderSegmentMedia(ctx, input); err != nil {
		return ProcessRenderSegmentMediaOutput{}, err
	} else if ok {
		return existing, nil
	}
	objectStore, ok := a.storage.(mediapkg.ObjectStore)
	if !ok {
		return ProcessRenderSegmentMediaOutput{}, fmt.Errorf("object storage does not support render segment processing")
	}
	execution, err = StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKeyForID("video_render_segment_media", input.RenderSegmentID),
		NodeType:       "media.video.segment",
		Input:          mustJSON(input),
	})
	if err != nil {
		return ProcessRenderSegmentMediaOutput{}, err
	}
	baseKey := fmt.Sprintf("organizations/%s/projects/%s/video-render-plans/%s/segments/%04d", input.OrganizationID, input.ProjectID, input.ExecutionPlanID, input.SegmentIndex)
	processed, err := mediapkg.ProcessRenderSegmentMedia(ctx, mediapkg.RenderSegmentMediaRequest{
		SourceStorageKey: input.RawStorageKey, SourceMimeType: input.RawMimeType,
		MezzanineStorageKey: baseKey + "/mezzanine.mp4", AudioStorageKey: baseKey + "/native-audio.m4a",
		AspectRatio: input.AspectRatio, Resolution: input.Resolution,
		FPSNumerator: input.FPSNumerator, FPSDenominator: input.FPSDenominator,
		PlannedDurationSeconds: input.PlannedDurationSeconds,
	}, objectStore)
	if err != nil {
		_ = FailNodeRun(ctx, a.db, execution, codeActivityFailed, err.Error())
		return ProcessRenderSegmentMediaOutput{}, err
	}
	return a.persistRenderSegmentMedia(ctx, input, execution, processed)
}

func (a Activities) existingRenderSegmentMedia(ctx context.Context, input ProcessRenderSegmentMediaInput) (ProcessRenderSegmentMediaOutput, bool, error) {
	var output ProcessRenderSegmentMediaOutput
	var sourceProbe, mezzanineProbe []byte
	var audioProbe []byte
	err := a.db.QueryRow(ctx, `
		SELECT COALESCE(segment.raw_av_artifact_id::text, ''), COALESCE(raw_media.id::text, ''), COALESCE(raw.storage_key, ''),
		       COALESCE(segment.mezzanine_artifact_id::text, ''), COALESCE(mezz_media.id::text, ''), COALESCE(mezz.storage_key, ''),
		       COALESCE(segment.extracted_audio_artifact_id::text, ''), COALESCE(audio_media.id::text, ''), COALESCE(audio.storage_key, ''),
		       COALESCE(segment.metadata->'sourceProbe', 'null'::jsonb), COALESCE(segment.metadata->'mezzanineProbe', 'null'::jsonb), COALESCE(segment.metadata->'audioProbe', 'null'::jsonb)
		FROM video_render_segments segment
		LEFT JOIN artifacts raw ON raw.id = segment.raw_av_artifact_id
		LEFT JOIN media_files raw_media ON raw_media.artifact_id = raw.id
		LEFT JOIN artifacts mezz ON mezz.id = segment.mezzanine_artifact_id
		LEFT JOIN media_files mezz_media ON mezz_media.artifact_id = mezz.id
		LEFT JOIN artifacts audio ON audio.id = segment.extracted_audio_artifact_id
		LEFT JOIN media_files audio_media ON audio_media.artifact_id = audio.id
		WHERE segment.id = $1 AND segment.video_render_plan_id = $2 AND segment.project_id = $3
	`, input.RenderSegmentID, input.ExecutionPlanID, input.ProjectID).Scan(
		&output.RawArtifactID, &output.RawMediaFileID, &output.RawStorageKey,
		&output.MezzanineArtifactID, &output.MezzanineMediaFileID, &output.MezzanineStorageKey,
		&output.ExtractedAudioArtifactID, &output.ExtractedAudioMediaFileID, &output.ExtractedAudioStorageKey,
		&sourceProbe, &mezzanineProbe, &audioProbe,
	)
	if err != nil || output.MezzanineArtifactID == "" {
		return ProcessRenderSegmentMediaOutput{}, false, nil
	}
	output.ExecutionPlanID = input.ExecutionPlanID
	output.RenderSegmentID = input.RenderSegmentID
	output.WorkflowRunID = input.WorkflowRunID
	_ = json.Unmarshal(sourceProbe, &output.SourceProbe)
	_ = json.Unmarshal(mezzanineProbe, &output.MezzanineProbe)
	if len(audioProbe) > 0 && string(audioProbe) != "null" {
		var probe mediapkg.ProbeResult
		if json.Unmarshal(audioProbe, &probe) == nil {
			output.AudioProbe = &probe
		}
	}
	return output, true, nil
}

func (a Activities) persistRenderSegmentMedia(ctx context.Context, input ProcessRenderSegmentMediaInput, execution NodeExecution, processed mediapkg.RenderSegmentMediaResult) (ProcessRenderSegmentMediaOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ProcessRenderSegmentMediaOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return ProcessRenderSegmentMediaOutput{}, err
	}
	var mezzanineArtifactID, mezzanineMediaFileID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, metadata, created_by)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, 'video_render_segment_mezzanine', $5, 'video/mp4', $6,
		        jsonb_build_object('executionPlanId', $7::text, 'renderSegmentId', $8::text, 'sourceArtifactId', $9::text), NULLIF($10, '')::uuid)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, execution.NodeRunID, processed.Mezzanine.StorageKey, processed.Mezzanine.ContentHash,
		input.ExecutionPlanID, input.RenderSegmentID, input.RawArtifactID, input.CreatedBy).Scan(&mezzanineArtifactID); err != nil {
		return ProcessRenderSegmentMediaOutput{}, err
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO media_files(organization_id, project_id, artifact_id, storage_key, mime_type, byte_size, width, height, duration_seconds, checksum)
		VALUES ($1, $2, $3, $4, 'video/mp4', $5, NULLIF($6, 0), NULLIF($7, 0), NULLIF($8, 0), $9)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, mezzanineArtifactID, processed.Mezzanine.StorageKey, processed.Mezzanine.ByteSize,
		processed.MezzanineProbe.Width, processed.MezzanineProbe.Height, processed.MezzanineProbe.DurationSeconds, processed.Mezzanine.ContentHash).Scan(&mezzanineMediaFileID); err != nil {
		return ProcessRenderSegmentMediaOutput{}, err
	}
	output := ProcessRenderSegmentMediaOutput{
		WorkflowRunID:   input.WorkflowRunID,
		ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID,
		RawArtifactID: input.RawArtifactID, RawMediaFileID: input.RawMediaFileID, RawStorageKey: input.RawStorageKey,
		MezzanineArtifactID: mezzanineArtifactID, MezzanineMediaFileID: mezzanineMediaFileID, MezzanineStorageKey: processed.Mezzanine.StorageKey,
		SourceProbe: processed.SourceProbe, MezzanineProbe: processed.MezzanineProbe, AudioProbe: processed.AudioProbe,
	}
	if processed.Audio != nil {
		if err := tx.QueryRow(ctx, `
			INSERT INTO artifacts(organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, metadata, created_by)
			VALUES ($1, $2, NULLIF($3, '')::uuid, $4, 'video_render_segment_audio', $5, 'audio/mp4', $6,
			        jsonb_build_object('executionPlanId', $7::text, 'renderSegmentId', $8::text, 'sourceArtifactId', $9::text), NULLIF($10, '')::uuid)
			RETURNING id::text
		`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, execution.NodeRunID, processed.Audio.StorageKey, processed.Audio.ContentHash,
			input.ExecutionPlanID, input.RenderSegmentID, input.RawArtifactID, input.CreatedBy).Scan(&output.ExtractedAudioArtifactID); err != nil {
			return ProcessRenderSegmentMediaOutput{}, err
		}
		var duration float64
		if processed.AudioProbe != nil {
			duration = processed.AudioProbe.DurationSeconds
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO media_files(organization_id, project_id, artifact_id, storage_key, mime_type, byte_size, duration_seconds, checksum)
			VALUES ($1, $2, $3, $4, 'audio/mp4', $5, NULLIF($6, 0), $7) RETURNING id::text
		`, input.OrganizationID, input.ProjectID, output.ExtractedAudioArtifactID, processed.Audio.StorageKey, processed.Audio.ByteSize, duration, processed.Audio.ContentHash).Scan(&output.ExtractedAudioMediaFileID); err != nil {
			return ProcessRenderSegmentMediaOutput{}, err
		}
		output.ExtractedAudioStorageKey = processed.Audio.StorageKey
	}
	metadata := mustJSON(map[string]any{"sourceProbe": processed.SourceProbe, "mezzanineProbe": processed.MezzanineProbe, "audioProbe": processed.AudioProbe, "mediaProcessed": true})
	if _, err := tx.Exec(ctx, `
		UPDATE video_render_segments
		SET raw_av_artifact_id = $3, mezzanine_artifact_id = $4, extracted_audio_artifact_id = NULLIF($5, '')::uuid,
		    metadata = metadata || $6::jsonb, updated_at = now()
		WHERE id = $1 AND video_render_plan_id = $2
	`, input.RenderSegmentID, input.ExecutionPlanID, input.RawArtifactID, output.MezzanineArtifactID, output.ExtractedAudioArtifactID, metadata); err != nil {
		return ProcessRenderSegmentMediaOutput{}, err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.segment.media.processed", "video_render_segment", input.RenderSegmentID, mustJSON(output)); err != nil {
		return ProcessRenderSegmentMediaOutput{}, err
	}
	if applied, err := completeNodeRunTx(ctx, tx, execution, mustJSON(output)); err != nil {
		return ProcessRenderSegmentMediaOutput{}, err
	} else if !applied {
		return ProcessRenderSegmentMediaOutput{}, ErrWorkflowWriteFenced
	}
	if err := tx.Commit(ctx); err != nil {
		return ProcessRenderSegmentMediaOutput{}, err
	}
	return output, nil
}
