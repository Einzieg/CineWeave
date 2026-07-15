package workflows

import (
	"context"
	"errors"
	"fmt"
	"strings"

	mediapkg "github.com/Einzieg/cineweave/internal/media"
	"github.com/jackc/pgx/v5"
)

type ExtractShotContinuityFrameInput struct {
	OrganizationID         string `json:"organizationId"`
	ProjectID              string `json:"projectId"`
	WorkflowRunID          string `json:"workflowRunId,omitempty"`
	CreatedBy              string `json:"createdBy,omitempty"`
	ShotID                 string `json:"shotId"`
	SourceVideoArtifactID  string `json:"sourceVideoArtifactId"`
	SourceVideoMediaFileID string `json:"sourceVideoMediaFileId,omitempty"`
	SourceVideoStorageKey  string `json:"sourceVideoStorageKey"`
}

type ExtractShotContinuityFrameOutput struct {
	ContinuityFrameID     string               `json:"continuityFrameId"`
	SourceShotID          string               `json:"sourceShotId"`
	SourceVideoArtifactID string               `json:"sourceVideoArtifactId"`
	ArtifactID            string               `json:"artifactId"`
	MediaFileID           string               `json:"mediaFileId"`
	StorageKey            string               `json:"storageKey"`
	MimeType              string               `json:"mimeType"`
	Width                 int                  `json:"width"`
	Height                int                  `json:"height"`
	FrameTimeSeconds      float64              `json:"frameTimeSeconds"`
	SourceProbe           mediapkg.ProbeResult `json:"sourceProbe"`
}

func (a Activities) ExtractShotContinuityFrame(ctx context.Context, input ExtractShotContinuityFrameInput) (_ ExtractShotContinuityFrameOutput, err error) {
	var execution NodeExecution
	defer func() {
		err = finalizeWorkflowActivityError(ctx, a.db, execution, err)
	}()
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.ShotID) == "" || strings.TrimSpace(input.SourceVideoArtifactID) == "" || strings.TrimSpace(input.SourceVideoStorageKey) == "" {
		return ExtractShotContinuityFrameOutput{}, fmt.Errorf("organizationId, projectId, workflowRunId, shotId, sourceVideoArtifactId, and sourceVideoStorageKey are required")
	}
	if err := a.validateCurrentShotVideoSource(ctx, input); err != nil {
		return ExtractShotContinuityFrameOutput{}, err
	}
	if existing, ok, err := a.findShotContinuityFrame(ctx, input); err != nil {
		return ExtractShotContinuityFrameOutput{}, err
	} else if ok {
		return existing, nil
	}
	objectStore, ok := a.storage.(mediapkg.ObjectStore)
	if !ok {
		return ExtractShotContinuityFrameOutput{}, fmt.Errorf("object storage does not support continuity frame extraction")
	}
	execution, err = StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKeyForID("shot_continuity_frame", input.SourceVideoArtifactID),
		NodeType:       "media.video.continuity_frame",
		Input:          mustJSON(input),
	})
	if err != nil {
		return ExtractShotContinuityFrameOutput{}, err
	}
	outputKey := fmt.Sprintf(
		"organizations/%s/projects/%s/storyboard-shots/%s/continuity/%s/tail-frame.png",
		input.OrganizationID, input.ProjectID, input.ShotID, input.SourceVideoArtifactID,
	)
	result, err := mediapkg.ExtractContinuityTailFrame(ctx, mediapkg.ContinuityFrameRequest{
		SourceStorageKey: input.SourceVideoStorageKey,
		SourceMimeType:   "video/mp4",
		OutputStorageKey: outputKey,
	}, objectStore)
	if err != nil {
		_ = FailNodeRun(ctx, a.db, execution, codeActivityFailed, err.Error())
		return ExtractShotContinuityFrameOutput{}, err
	}

	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ExtractShotContinuityFrameOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return ExtractShotContinuityFrameOutput{}, err
	}
	if err := validateCurrentShotVideoSourceTx(ctx, tx, input); err != nil {
		return ExtractShotContinuityFrameOutput{}, err
	}
	if existing, ok, err := findShotContinuityFrameTx(ctx, tx, input); err != nil {
		return ExtractShotContinuityFrameOutput{}, err
	} else if ok {
		if applied, err := completeNodeRunTx(ctx, tx, execution, mustJSON(existing)); err != nil {
			return ExtractShotContinuityFrameOutput{}, err
		} else if !applied {
			return ExtractShotContinuityFrameOutput{}, ErrWorkflowWriteFenced
		}
		if err := tx.Commit(ctx); err != nil {
			return ExtractShotContinuityFrameOutput{}, err
		}
		return existing, nil
	}

	metadata := mustJSON(map[string]any{
		"source":                 "cross_shot_continuity",
		"frameRole":              "tail",
		"storyboardShotId":       input.ShotID,
		"sourceVideoArtifactId":  input.SourceVideoArtifactID,
		"sourceVideoMediaFileId": input.SourceVideoMediaFileID,
		"sourceVideoStorageKey":  input.SourceVideoStorageKey,
		"frameTimeSeconds":       result.FrameTimeSeconds,
		"sourceProbe":            result.SourceProbe,
		"frameProbe":             result.FrameProbe,
	})
	var artifactID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, metadata, created_by)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, 'storyboard_shot_continuity_frame', $5, 'image/png', $6, $7, NULLIF($8, '')::uuid)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, execution.NodeRunID, result.StorageKey, result.ContentHash, metadata, input.CreatedBy).Scan(&artifactID); err != nil {
		return ExtractShotContinuityFrameOutput{}, err
	}
	var mediaFileID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO media_files(organization_id, project_id, artifact_id, storage_key, mime_type, byte_size, width, height, checksum, metadata, created_by)
		VALUES ($1, $2, $3, $4, 'image/png', $5, NULLIF($6, 0), NULLIF($7, 0), $8, $9, NULLIF($10, '')::uuid)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, artifactID, result.StorageKey, result.ByteSize, result.Width, result.Height, result.ContentHash, metadata, input.CreatedBy).Scan(&mediaFileID); err != nil {
		return ExtractShotContinuityFrameOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shot_continuity_frames
		SET status = 'superseded', updated_at = now()
		WHERE storyboard_shot_id = $1 AND frame_role = 'tail' AND status = 'active'
	`, input.ShotID); err != nil {
		return ExtractShotContinuityFrameOutput{}, err
	}
	output := ExtractShotContinuityFrameOutput{
		SourceShotID: input.ShotID, SourceVideoArtifactID: input.SourceVideoArtifactID,
		ArtifactID: artifactID, MediaFileID: mediaFileID, StorageKey: result.StorageKey, MimeType: result.MimeType,
		Width: result.Width, Height: result.Height, FrameTimeSeconds: result.FrameTimeSeconds, SourceProbe: result.SourceProbe,
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO storyboard_shot_continuity_frames(
			organization_id, project_id, storyboard_shot_id, source_video_artifact_id, source_video_media_file_id,
			frame_artifact_id, frame_media_file_id, storage_key, frame_role, status, frame_time_seconds,
			workflow_run_id, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid, $6, $7, $8, 'tail', 'active', $9,
		        NULLIF($10, '')::uuid, $11, NULLIF($12, '')::uuid)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, input.ShotID, input.SourceVideoArtifactID, input.SourceVideoMediaFileID,
		artifactID, mediaFileID, result.StorageKey, result.FrameTimeSeconds, input.WorkflowRunID, metadata, input.CreatedBy,
	).Scan(&output.ContinuityFrameID); err != nil {
		return ExtractShotContinuityFrameOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
			'continuityTailFrame', jsonb_build_object(
				'id', $2::text, 'artifactId', $3::text, 'mediaFileId', $4::text, 'storageKey', $5::text,
				'sourceVideoArtifactId', $6::text, 'frameTimeSeconds', $7::numeric
			)
		), updated_at = now()
		WHERE id = $1
	`, input.ShotID, output.ContinuityFrameID, artifactID, mediaFileID, result.StorageKey, input.SourceVideoArtifactID, result.FrameTimeSeconds); err != nil {
		return ExtractShotContinuityFrameOutput{}, err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.continuity_frame.extracted", "storyboard_shot", input.ShotID, mustJSON(output)); err != nil {
		return ExtractShotContinuityFrameOutput{}, err
	}
	if applied, err := completeNodeRunTx(ctx, tx, execution, mustJSON(output)); err != nil {
		return ExtractShotContinuityFrameOutput{}, err
	} else if !applied {
		return ExtractShotContinuityFrameOutput{}, ErrWorkflowWriteFenced
	}
	if err := tx.Commit(ctx); err != nil {
		return ExtractShotContinuityFrameOutput{}, err
	}
	return output, nil
}

func (a Activities) validateCurrentShotVideoSource(ctx context.Context, input ExtractShotContinuityFrameInput) error {
	var artifactID, mediaFileID, storageKey, status, staleState string
	err := a.db.QueryRow(ctx, `
		SELECT COALESCE(video_artifact_id::text, ''), COALESCE(video_media_file_id::text, ''), COALESCE(video_storage_key, ''),
		       COALESCE(video_status, ''), COALESCE(stale_state, 'fresh')
		FROM storyboard_shots
		WHERE id = $1 AND project_id = $2 AND organization_id = $3 AND deleted_at IS NULL
	`, input.ShotID, input.ProjectID, input.OrganizationID).Scan(&artifactID, &mediaFileID, &storageKey, &status, &staleState)
	if err != nil {
		return err
	}
	return validateShotVideoSourceValues(input, artifactID, mediaFileID, storageKey, status, staleState)
}

func validateCurrentShotVideoSourceTx(ctx context.Context, tx pgx.Tx, input ExtractShotContinuityFrameInput) error {
	var artifactID, mediaFileID, storageKey, status, staleState string
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(video_artifact_id::text, ''), COALESCE(video_media_file_id::text, ''), COALESCE(video_storage_key, ''),
		       COALESCE(video_status, ''), COALESCE(stale_state, 'fresh')
		FROM storyboard_shots
		WHERE id = $1 AND project_id = $2 AND organization_id = $3 AND deleted_at IS NULL
		FOR UPDATE
	`, input.ShotID, input.ProjectID, input.OrganizationID).Scan(&artifactID, &mediaFileID, &storageKey, &status, &staleState)
	if err != nil {
		return err
	}
	return validateShotVideoSourceValues(input, artifactID, mediaFileID, storageKey, status, staleState)
}

func validateShotVideoSourceValues(input ExtractShotContinuityFrameInput, artifactID, mediaFileID, storageKey, status, staleState string) error {
	if status != "succeeded" || staleState != "fresh" {
		return fmt.Errorf("source shot video is not a fresh succeeded output")
	}
	if artifactID != input.SourceVideoArtifactID || storageKey != input.SourceVideoStorageKey {
		return fmt.Errorf("source shot video changed before continuity frame extraction")
	}
	if input.SourceVideoMediaFileID != "" && mediaFileID != input.SourceVideoMediaFileID {
		return fmt.Errorf("source shot video media changed before continuity frame extraction")
	}
	return nil
}

func (a Activities) findShotContinuityFrame(ctx context.Context, input ExtractShotContinuityFrameInput) (ExtractShotContinuityFrameOutput, bool, error) {
	var output ExtractShotContinuityFrameOutput
	err := a.db.QueryRow(ctx, continuityFrameSelectSQL, input.OrganizationID, input.ProjectID, input.ShotID, input.SourceVideoArtifactID).Scan(
		&output.ContinuityFrameID, &output.SourceShotID, &output.SourceVideoArtifactID, &output.ArtifactID, &output.MediaFileID,
		&output.StorageKey, &output.MimeType, &output.Width, &output.Height, &output.FrameTimeSeconds, &output.SourceProbe,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ExtractShotContinuityFrameOutput{}, false, nil
	}
	return output, err == nil, err
}

func findShotContinuityFrameTx(ctx context.Context, tx pgx.Tx, input ExtractShotContinuityFrameInput) (ExtractShotContinuityFrameOutput, bool, error) {
	var output ExtractShotContinuityFrameOutput
	err := tx.QueryRow(ctx, continuityFrameSelectSQL+" FOR UPDATE", input.OrganizationID, input.ProjectID, input.ShotID, input.SourceVideoArtifactID).Scan(
		&output.ContinuityFrameID, &output.SourceShotID, &output.SourceVideoArtifactID, &output.ArtifactID, &output.MediaFileID,
		&output.StorageKey, &output.MimeType, &output.Width, &output.Height, &output.FrameTimeSeconds, &output.SourceProbe,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ExtractShotContinuityFrameOutput{}, false, nil
	}
	return output, err == nil, err
}

const continuityFrameSelectSQL = `
	SELECT frame.id::text, frame.storyboard_shot_id::text, frame.source_video_artifact_id::text,
	       frame.frame_artifact_id::text, frame.frame_media_file_id::text, frame.storage_key,
	       COALESCE(media.mime_type, 'image/png'), COALESCE(media.width, 0), COALESCE(media.height, 0),
	       frame.frame_time_seconds, COALESCE(frame.metadata->'sourceProbe', '{}'::jsonb)
	FROM storyboard_shot_continuity_frames frame
	JOIN media_files media ON media.id = frame.frame_media_file_id
	WHERE frame.organization_id = $1 AND frame.project_id = $2 AND frame.storyboard_shot_id = $3
	  AND frame.source_video_artifact_id = $4 AND frame.frame_role = 'tail'`
