package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	mediapkg "github.com/Einzieg/cineweave/internal/media"
	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/sdk/temporal"
)

type ComposeFinalVideoInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId"`
	CreatedBy      string `json:"createdBy"`

	AspectRatio string `json:"aspectRatio"`
	Resolution  string `json:"resolution"`
}

type ComposeFinalVideoOutput struct {
	NodeRunID          string  `json:"nodeRunId"`
	ArtifactID         string  `json:"artifactId"`
	MediaFileID        string  `json:"mediaFileId"`
	StorageKey         string  `json:"storageKey"`
	MimeType           string  `json:"mimeType"`
	DurationSeconds    float64 `json:"durationSeconds,omitempty"`
	Width              int     `json:"width,omitempty"`
	Height             int     `json:"height,omitempty"`
	TimelineArtifactID string  `json:"timelineArtifactId,omitempty"`
}

type composeClipRecord struct {
	ShotID           string
	ShotIndex        int
	ShotNo           int
	VideoArtifactID  string
	VideoMediaFileID string
	StorageKey       string
	MimeType         string
	DurationSeconds  float64
}

type timelineManifest struct {
	WorkflowRunID string                 `json:"workflowRunId"`
	ProjectID     string                 `json:"projectId"`
	Clips         []timelineManifestClip `json:"clips"`
	Compose       map[string]string      `json:"compose"`
}

type timelineManifestClip struct {
	ShotID           string  `json:"shotId"`
	ShotNo           int     `json:"shotNo"`
	ShotIndex        int     `json:"shotIndex"`
	VideoArtifactID  string  `json:"videoArtifactId"`
	VideoMediaFileID string  `json:"videoMediaFileId"`
	StorageKey       string  `json:"storageKey"`
	DurationSeconds  float64 `json:"durationSeconds,omitempty"`
}

func (a Activities) ComposeFinalVideo(ctx context.Context, input ComposeFinalVideoInput) (ComposeFinalVideoOutput, error) {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" {
		return ComposeFinalVideoOutput{}, fmt.Errorf("organizationId, projectId, and workflowRunId are required")
	}
	if existing, ok, err := a.existingComposeFinalVideo(ctx, input.WorkflowRunID); err != nil {
		return ComposeFinalVideoOutput{}, err
	} else if ok {
		return existing, nil
	}

	nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeComposeFinalVideoKey,
		NodeType:       "media.compose",
		Input: mustJSON(map[string]any{
			"aspectRatio": input.AspectRatio,
			"resolution":  input.Resolution,
		}),
	})
	if err != nil {
		return ComposeFinalVideoOutput{}, err
	}

	clips, err := a.composeVideoClips(ctx, input.WorkflowRunID)
	if err != nil {
		return ComposeFinalVideoOutput{}, a.failComposeFinalVideo(ctx, input, nodeRunID, codeActivityFailed, err.Error())
	}
	if len(clips) == 0 {
		return ComposeFinalVideoOutput{}, a.failComposeFinalVideo(ctx, input, nodeRunID, codeNoVideoClipsToCompose, "no succeeded shot videos are available to compose")
	}
	objectStore, ok := a.storage.(mediapkg.ObjectStore)
	if !ok {
		return ComposeFinalVideoOutput{}, a.failComposeFinalVideo(ctx, input, nodeRunID, codeActivityFailed, "object storage does not support media compose")
	}

	manifest := buildTimelineManifest(input, clips)
	timelinePut, err := a.storage.PutJSON(ctx, timelineStorageKey(input), manifest)
	if err != nil {
		return ComposeFinalVideoOutput{}, a.failComposeFinalVideo(ctx, input, nodeRunID, codeActivityFailed, err.Error())
	}

	composeReq := mediapkg.ComposeRequest{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		Clips:          make([]mediapkg.Clip, 0, len(clips)),
		AspectRatio:    defaultString(input.AspectRatio, "16:9"),
		Resolution:     defaultString(input.Resolution, "720p"),
		OutputMimeType: "video/mp4",
	}
	for _, clip := range clips {
		composeReq.Clips = append(composeReq.Clips, mediapkg.Clip{
			ShotID:          clip.ShotID,
			ShotIndex:       clip.ShotIndex,
			StorageKey:      clip.StorageKey,
			MimeType:        clip.MimeType,
			DurationSeconds: clip.DurationSeconds,
		})
	}
	result, err := mediapkg.ComposeClipsWithStore(ctx, composeReq, objectStore)
	if err != nil {
		code := codeActivityFailed
		if errors.Is(err, mediapkg.ErrNoVideoClips) {
			code = codeNoVideoClipsToCompose
		}
		return ComposeFinalVideoOutput{}, a.failComposeFinalVideo(ctx, input, nodeRunID, code, err.Error())
	}
	output, err := a.completeComposeFinalVideo(ctx, input, nodeRunID, clips, timelinePut, result)
	if err != nil {
		return ComposeFinalVideoOutput{}, a.failComposeFinalVideo(ctx, input, nodeRunID, codeActivityFailed, err.Error())
	}
	return output, nil
}

func (a Activities) existingComposeFinalVideo(ctx context.Context, workflowRunID string) (ComposeFinalVideoOutput, bool, error) {
	var output ComposeFinalVideoOutput
	var raw json.RawMessage
	err := a.db.QueryRow(ctx, `
		SELECT COALESCE(output, '{}'::jsonb)
		FROM workflow_node_runs
		WHERE workflow_run_id = $1
		  AND node_key = $2
		  AND status = 'succeeded'
	`, workflowRunID, nodeComposeFinalVideoKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return ComposeFinalVideoOutput{}, false, nil
	}
	if err != nil {
		return ComposeFinalVideoOutput{}, false, err
	}
	if err := json.Unmarshal(raw, &output); err != nil {
		return ComposeFinalVideoOutput{}, false, err
	}
	return output, output.ArtifactID != "" && output.MediaFileID != "" && output.StorageKey != "", nil
}

func (a Activities) composeVideoClips(ctx context.Context, workflowRunID string) ([]composeClipRecord, error) {
	rows, err := a.db.Query(ctx, `
		SELECT
			s.id::text,
			s.shot_index,
			COALESCE(s.shot_no, s.shot_index + 1),
			COALESCE(s.video_artifact_id::text, ''),
			COALESCE(s.video_media_file_id::text, ''),
			COALESCE(s.video_storage_key, mf.storage_key, ''),
			COALESCE(mf.mime_type, 'video/mp4'),
			COALESCE(mf.duration_seconds, s.duration_seconds, 0)::float8
		FROM storyboard_shots s
		LEFT JOIN media_files mf ON mf.id = s.video_media_file_id
		WHERE s.workflow_run_id = $1
		  AND s.status = 'video_succeeded'
		  AND COALESCE(s.video_storage_key, mf.storage_key, '') <> ''
		ORDER BY s.shot_index ASC
	`, workflowRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	clips := make([]composeClipRecord, 0)
	for rows.Next() {
		var clip composeClipRecord
		if err := rows.Scan(
			&clip.ShotID,
			&clip.ShotIndex,
			&clip.ShotNo,
			&clip.VideoArtifactID,
			&clip.VideoMediaFileID,
			&clip.StorageKey,
			&clip.MimeType,
			&clip.DurationSeconds,
		); err != nil {
			return nil, err
		}
		clips = append(clips, clip)
	}
	return clips, rows.Err()
}

func (a Activities) completeComposeFinalVideo(ctx context.Context, input ComposeFinalVideoInput, nodeRunID string, clips []composeClipRecord, timelinePut storage.PutResult, result mediapkg.ComposeResult) (ComposeFinalVideoOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ComposeFinalVideoOutput{}, err
	}
	defer tx.Rollback(ctx)

	var nodeStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM workflow_node_runs WHERE id = $1 FOR UPDATE`, nodeRunID).Scan(&nodeStatus); err != nil {
		return ComposeFinalVideoOutput{}, err
	}
	if nodeStatus == "cancelled" || nodeStatus == "failed" {
		return ComposeFinalVideoOutput{}, fmt.Errorf("compose node is already %s", nodeStatus)
	}

	shotIDs := make([]string, 0, len(clips))
	clipStorageKeys := make([]string, 0, len(clips))
	for _, clip := range clips {
		shotIDs = append(shotIDs, clip.ShotID)
		clipStorageKeys = append(clipStorageKeys, clip.StorageKey)
	}

	var timelineArtifactID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, metadata, created_by)
		VALUES ($1, $2, $3, $4, 'timeline_json', $5, 'application/json', $6, $7, $8)
		RETURNING id
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, nodeRunID, timelinePut.StorageKey, timelinePut.ContentHash, mustJSON(map[string]any{
		"source":   "media_worker",
		"type":     "timeline_manifest",
		"byteSize": timelinePut.ByteSize,
	}), nullIfEmpty(input.CreatedBy)).Scan(&timelineArtifactID); err != nil {
		return ComposeFinalVideoOutput{}, err
	}

	finalMetadata := map[string]any{
		"source":          "media_worker",
		"nodeKey":         nodeComposeFinalVideoKey,
		"nodeRunId":       nodeRunID,
		"workflowRunId":   input.WorkflowRunID,
		"shotIds":         shotIDs,
		"clipStorageKeys": clipStorageKeys,
		"clipCount":       len(clips),
		"composeSettings": map[string]any{
			"aspectRatio": defaultString(input.AspectRatio, "16:9"),
			"resolution":  defaultString(input.Resolution, "720p"),
			"format":      "mp4",
		},
		"timelineArtifactId": timelineArtifactID,
	}
	var finalArtifactID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, metadata, created_by)
		VALUES ($1, $2, $3, $4, 'final_video', $5, $6, $7, $8, $9)
		RETURNING id
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, nodeRunID, result.StorageKey, result.MimeType, result.ContentHash, mustJSON(finalMetadata), nullIfEmpty(input.CreatedBy)).Scan(&finalArtifactID); err != nil {
		return ComposeFinalVideoOutput{}, err
	}
	var mediaFileID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO media_files(organization_id, project_id, artifact_id, storage_key, mime_type, byte_size, width, height, duration_seconds, checksum, metadata, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id
	`, input.OrganizationID, input.ProjectID, finalArtifactID, result.StorageKey, result.MimeType, nullInt64(result.ByteSize), nullInt(result.Width), nullInt(result.Height), nullFloat(result.DurationSeconds), nullIfEmpty(result.ContentHash), mustJSON(map[string]any{
		"source":        "media_worker",
		"workflowRunId": input.WorkflowRunID,
		"shotIds":       shotIDs,
		"clipCount":     len(clips),
	}), nullIfEmpty(input.CreatedBy)).Scan(&mediaFileID); err != nil {
		return ComposeFinalVideoOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE artifacts
		SET metadata = metadata || $2::jsonb
		WHERE id = $1
	`, finalArtifactID, mustJSON(map[string]any{"mediaFileId": mediaFileID})); err != nil {
		return ComposeFinalVideoOutput{}, err
	}

	output := ComposeFinalVideoOutput{
		NodeRunID:          nodeRunID,
		ArtifactID:         finalArtifactID,
		MediaFileID:        mediaFileID,
		StorageKey:         result.StorageKey,
		MimeType:           result.MimeType,
		DurationSeconds:    result.DurationSeconds,
		Width:              result.Width,
		Height:             result.Height,
		TimelineArtifactID: timelineArtifactID,
	}
	outputJSON := mustJSON(output)
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_node_runs
		SET status = 'succeeded', output = $2, completed_at = now()
		WHERE id = $1
		  AND status NOT IN ('cancelled', 'failed')
	`, nodeRunID, outputJSON); err != nil {
		return ComposeFinalVideoOutput{}, err
	}
	events := []struct {
		eventType     string
		aggregateType string
		aggregateID   string
		payload       json.RawMessage
	}{
		{"artifact.created", "artifact", timelineArtifactID, mustJSON(map[string]any{
			"artifactId":    timelineArtifactID,
			"workflowRunId": input.WorkflowRunID,
			"nodeRunId":     nodeRunID,
			"storageKey":    timelinePut.StorageKey,
			"type":          "timeline_json",
		})},
		{"artifact.created", "artifact", finalArtifactID, mustJSON(map[string]any{
			"artifactId":    finalArtifactID,
			"workflowRunId": input.WorkflowRunID,
			"nodeRunId":     nodeRunID,
			"storageKey":    result.StorageKey,
			"type":          "final_video",
			"mediaFileId":   mediaFileID,
		})},
		{"workflow.node.completed", "workflow_node_run", nodeRunID, mustJSON(map[string]any{
			"workflowRunId": input.WorkflowRunID,
			"nodeKey":       nodeComposeFinalVideoKey,
			"output":        json.RawMessage(outputJSON),
		})},
		{"media.compose.completed", "workflow_node_run", nodeRunID, mustJSON(map[string]any{
			"workflowRunId":      input.WorkflowRunID,
			"artifactId":         finalArtifactID,
			"mediaFileId":        mediaFileID,
			"storageKey":         result.StorageKey,
			"timelineArtifactId": timelineArtifactID,
			"clipCount":          len(clips),
		})},
	}
	for _, event := range events {
		if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, event.eventType, event.aggregateType, event.aggregateID, event.payload); err != nil {
			return ComposeFinalVideoOutput{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ComposeFinalVideoOutput{}, err
	}
	return output, nil
}

func (a Activities) failComposeFinalVideo(ctx context.Context, input ComposeFinalVideoInput, nodeRunID, code, message string) error {
	if strings.TrimSpace(nodeRunID) != "" {
		_ = FailNodeRun(ctx, a.db, nodeRunID, code, message)
	}
	_ = a.markWorkflowFailed(ctx, TextToStoryboardInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		Prompt:         "compose final video",
		CreatedBy:      input.CreatedBy,
	}, code, message)
	tx, err := a.db.Begin(ctx)
	if err == nil {
		defer tx.Rollback(ctx)
		_ = insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "media.compose.failed", "workflow_node_run", nodeRunID, mustJSON(map[string]any{
			"workflowRunId": input.WorkflowRunID,
			"nodeRunId":     nodeRunID,
			"nodeKey":       nodeComposeFinalVideoKey,
			"code":          code,
			"message":       message,
		}))
		_ = tx.Commit(ctx)
	}
	return temporal.NewApplicationError(message, code)
}

func buildTimelineManifest(input ComposeFinalVideoInput, clips []composeClipRecord) timelineManifest {
	manifest := timelineManifest{
		WorkflowRunID: input.WorkflowRunID,
		ProjectID:     input.ProjectID,
		Clips:         make([]timelineManifestClip, 0, len(clips)),
		Compose: map[string]string{
			"aspectRatio": defaultString(input.AspectRatio, "16:9"),
			"resolution":  defaultString(input.Resolution, "720p"),
			"format":      "mp4",
		},
	}
	for _, clip := range clips {
		manifest.Clips = append(manifest.Clips, timelineManifestClip{
			ShotID:           clip.ShotID,
			ShotNo:           clip.ShotNo,
			ShotIndex:        clip.ShotIndex,
			VideoArtifactID:  clip.VideoArtifactID,
			VideoMediaFileID: clip.VideoMediaFileID,
			StorageKey:       clip.StorageKey,
			DurationSeconds:  clip.DurationSeconds,
		})
	}
	return manifest
}

func timelineStorageKey(input ComposeFinalVideoInput) string {
	return fmt.Sprintf("org/%s/project/%s/workflow/%s/timeline/timeline.json", input.OrganizationID, input.ProjectID, input.WorkflowRunID)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func nullInt(value int) any {
	if value <= 0 {
		return nil
	}
	return value
}

func nullInt64(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func nullFloat(value float64) any {
	if value <= 0 {
		return nil
	}
	return value
}
