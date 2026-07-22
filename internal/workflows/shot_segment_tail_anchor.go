package workflows

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	mediapkg "github.com/Einzieg/cineweave/internal/media"
	"github.com/jackc/pgx/v5"
)

type ExtractRenderSegmentTailAnchorInput struct {
	OrganizationID         string `json:"organizationId"`
	ProjectID              string `json:"projectId"`
	WorkflowRunID          string `json:"workflowRunId"`
	CreatedBy              string `json:"createdBy,omitempty"`
	ShotID                 string `json:"shotId"`
	SourceRenderSegmentID  string `json:"sourceRenderSegmentId"`
	SourceVideoArtifactID  string `json:"sourceVideoArtifactId"`
	SourceVideoMediaFileID string `json:"sourceVideoMediaFileId,omitempty"`
	SourceVideoStorageKey  string `json:"sourceVideoStorageKey"`
}

type ExtractRenderSegmentTailAnchorOutput struct {
	AnchorID              string               `json:"anchorId"`
	SourceShotID          string               `json:"sourceShotId"`
	SourceRenderSegmentID string               `json:"sourceRenderSegmentId"`
	SourceVideoArtifactID string               `json:"sourceVideoArtifactId"`
	ArtifactID            string               `json:"artifactId"`
	MediaFileID           string               `json:"mediaFileId"`
	StorageKey            string               `json:"storageKey"`
	MimeType              string               `json:"mimeType"`
	Width                 int                  `json:"width"`
	Height                int                  `json:"height"`
	FrameTimeSeconds      float64              `json:"frameTimeSeconds"`
	ContentHash           string               `json:"contentHash"`
	GeneratedAt           time.Time            `json:"generatedAt"`
	SourceProbe           mediapkg.ProbeResult `json:"sourceProbe"`
}

type ShotSegmentTailAnchorReference struct {
	AnchorID              string    `json:"anchorId"`
	SourceShotID          string    `json:"sourceShotId"`
	SourceRenderSegmentID string    `json:"sourceRenderSegmentId"`
	SourceVideoArtifactID string    `json:"sourceVideoArtifactId"`
	ArtifactID            string    `json:"artifactId"`
	MediaFileID           string    `json:"mediaFileId"`
	StorageKey            string    `json:"storageKey"`
	ContentHash           string    `json:"contentHash"`
	GeneratedAt           time.Time `json:"generatedAt"`
}

func (a Activities) ExtractRenderSegmentTailAnchor(ctx context.Context, input ExtractRenderSegmentTailAnchorInput) (_ ExtractRenderSegmentTailAnchorOutput, err error) {
	var execution NodeExecution
	defer func() {
		err = finalizeWorkflowActivityError(ctx, a.db, execution, err)
	}()
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" ||
		strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.ShotID) == "" ||
		strings.TrimSpace(input.SourceRenderSegmentID) == "" || strings.TrimSpace(input.SourceVideoArtifactID) == "" ||
		strings.TrimSpace(input.SourceVideoStorageKey) == "" {
		return ExtractRenderSegmentTailAnchorOutput{}, fmt.Errorf("organizationId, projectId, workflowRunId, shotId, sourceRenderSegmentId, sourceVideoArtifactId, and sourceVideoStorageKey are required")
	}
	if err := validateRenderSegmentTailSource(ctx, a.db, input, false); err != nil {
		return ExtractRenderSegmentTailAnchorOutput{}, err
	}
	if existing, ok, err := a.findRenderSegmentTailAnchor(ctx, input); err != nil {
		return ExtractRenderSegmentTailAnchorOutput{}, err
	} else if ok {
		return existing, nil
	}
	objectStore, ok := a.storage.(mediapkg.ObjectStore)
	if !ok {
		return ExtractRenderSegmentTailAnchorOutput{}, fmt.Errorf("object storage does not support render segment tail extraction")
	}
	execution, err = StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKeyForID("render_segment_tail_anchor", input.SourceRenderSegmentID),
		NodeType:       "media.video.segment_tail_anchor",
		Input:          mustJSON(input),
	})
	if err != nil {
		return ExtractRenderSegmentTailAnchorOutput{}, err
	}
	outputKey := fmt.Sprintf(
		"organizations/%s/projects/%s/storyboard-shots/%s/render-segments/%s/tail-anchor.png",
		input.OrganizationID, input.ProjectID, input.ShotID, input.SourceRenderSegmentID,
	)
	result, err := mediapkg.ExtractVideoTailFrame(ctx, mediapkg.VideoTailFrameRequest{
		SourceStorageKey: input.SourceVideoStorageKey,
		SourceMimeType:   "video/mp4",
		OutputStorageKey: outputKey,
	}, objectStore)
	if err != nil {
		_ = FailNodeRun(ctx, a.db, execution, codeActivityFailed, err.Error())
		return ExtractRenderSegmentTailAnchorOutput{}, err
	}

	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ExtractRenderSegmentTailAnchorOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return ExtractRenderSegmentTailAnchorOutput{}, err
	}
	if err := validateRenderSegmentTailSource(ctx, tx, input, true); err != nil {
		return ExtractRenderSegmentTailAnchorOutput{}, err
	}
	if err := lockRenderSegmentTailAnchorSlot(ctx, tx, input); err != nil {
		return ExtractRenderSegmentTailAnchorOutput{}, err
	}
	if existing, ok, err := findRenderSegmentTailAnchorTx(ctx, tx, input); err != nil {
		return ExtractRenderSegmentTailAnchorOutput{}, err
	} else if ok {
		if applied, err := completeNodeRunTx(ctx, tx, execution, mustJSON(existing)); err != nil {
			return ExtractRenderSegmentTailAnchorOutput{}, err
		} else if !applied {
			return ExtractRenderSegmentTailAnchorOutput{}, ErrWorkflowWriteFenced
		}
		if err := tx.Commit(ctx); err != nil {
			return ExtractRenderSegmentTailAnchorOutput{}, err
		}
		return existing, nil
	}

	generatedAt := time.Now().UTC()
	metadata := mustJSON(map[string]any{
		"source":                 "same_shot_segment_continuation",
		"anchorRole":             "observed_tail_frame",
		"sourceRole":             "previous_segment_tail",
		"storyboardShotId":       input.ShotID,
		"sourceRenderSegmentId":  input.SourceRenderSegmentID,
		"sourceVideoArtifactId":  input.SourceVideoArtifactID,
		"sourceVideoMediaFileId": input.SourceVideoMediaFileID,
		"sourceVideoStorageKey":  input.SourceVideoStorageKey,
		"frameTimeSeconds":       result.FrameTimeSeconds,
		"contentHash":            result.ContentHash,
		"generatedAt":            generatedAt,
		"sourceProbe":            result.SourceProbe,
		"frameProbe":             result.FrameProbe,
	})
	var artifactID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO artifacts(
			organization_id, project_id, workflow_run_id, node_run_id, type,
			storage_key, mime_type, content_hash, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, 'video_render_segment_tail_anchor', $5,
		        'image/png', $6, $7, NULLIF($8, '')::uuid)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, execution.NodeRunID,
		result.StorageKey, result.ContentHash, metadata, input.CreatedBy).Scan(&artifactID); err != nil {
		return ExtractRenderSegmentTailAnchorOutput{}, err
	}
	var mediaFileID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO media_files(
			organization_id, project_id, artifact_id, storage_key, mime_type,
			byte_size, width, height, checksum, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, 'image/png', $5, NULLIF($6, 0), NULLIF($7, 0),
		        $8, $9, NULLIF($10, '')::uuid)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, artifactID, result.StorageKey, result.ByteSize,
		result.Width, result.Height, result.ContentHash, metadata, input.CreatedBy).Scan(&mediaFileID); err != nil {
		return ExtractRenderSegmentTailAnchorOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE shot_visual_anchors
		SET status = 'stale', review_status = 'needs_edit', updated_at = now(),
		    metadata = metadata || jsonb_build_object(
		      'supersededAt', now(),
		      'supersededBySourceRenderSegmentId', $2::text
		    )
		WHERE storyboard_shot_id = $1
		  AND anchor_role = 'observed_tail_frame'
		  AND status = 'ready' AND review_status = 'approved'
	`, input.ShotID, input.SourceRenderSegmentID); err != nil {
		return ExtractRenderSegmentTailAnchorOutput{}, err
	}
	output := ExtractRenderSegmentTailAnchorOutput{
		SourceShotID: input.ShotID, SourceRenderSegmentID: input.SourceRenderSegmentID,
		SourceVideoArtifactID: input.SourceVideoArtifactID, ArtifactID: artifactID,
		MediaFileID: mediaFileID, StorageKey: result.StorageKey, MimeType: result.MimeType,
		Width: result.Width, Height: result.Height, FrameTimeSeconds: result.FrameTimeSeconds,
		ContentHash: result.ContentHash, GeneratedAt: generatedAt,
		SourceProbe: result.SourceProbe,
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO shot_visual_anchors(
			organization_id, project_id, production_generation_id, storyboard_shot_id,
			shot_state_version_id, anchor_role, revision, status, review_status,
			artifact_id, media_file_id, storage_key, source_render_segment_id,
			source_video_artifact_id, source_role, metadata, created_at, updated_at
		)
		SELECT shot.organization_id, shot.project_id, shot.production_generation_id, shot.id,
		       state.id, 'observed_tail_frame',
		       COALESCE((SELECT max(anchor.revision) FROM shot_visual_anchors anchor
		                 WHERE anchor.storyboard_shot_id = shot.id AND anchor.anchor_role = 'observed_tail_frame'), 0) + 1,
		       'ready', 'approved', $4, $5, $6, $7, $8, 'previous_segment_tail', $9, $10, $10
		FROM storyboard_shots shot
		LEFT JOIN LATERAL (
			SELECT candidate.id FROM storyboard_shot_state_versions candidate
			WHERE candidate.storyboard_shot_id = shot.id
			  AND candidate.production_generation_id = shot.production_generation_id
			  AND candidate.state_role = 'planned_exit' AND candidate.status = 'approved'
			ORDER BY candidate.revision DESC LIMIT 1
		) state ON true
		WHERE shot.organization_id = $1 AND shot.project_id = $2 AND shot.id = $3
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, input.ShotID, artifactID, mediaFileID,
		result.StorageKey, input.SourceRenderSegmentID, input.SourceVideoArtifactID, metadata, generatedAt).Scan(&output.AnchorID); err != nil {
		return ExtractRenderSegmentTailAnchorOutput{}, err
	}
	eventPayload := mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "storyboardShotId": input.ShotID,
		"sourceRenderSegmentId": input.SourceRenderSegmentID,
		"sourceVideoArtifactId": input.SourceVideoArtifactID,
		"anchorId":              output.AnchorID, "artifactId": artifactID, "mediaFileId": mediaFileID,
	})
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"storyboard.shot.segment_tail_anchor.extracted", "shot_visual_anchor", output.AnchorID, eventPayload); err != nil {
		return ExtractRenderSegmentTailAnchorOutput{}, err
	}
	if applied, err := completeNodeRunTx(ctx, tx, execution, mustJSON(output)); err != nil {
		return ExtractRenderSegmentTailAnchorOutput{}, err
	} else if !applied {
		return ExtractRenderSegmentTailAnchorOutput{}, ErrWorkflowWriteFenced
	}
	if err := tx.Commit(ctx); err != nil {
		return ExtractRenderSegmentTailAnchorOutput{}, err
	}
	return output, nil
}

func lockRenderSegmentTailAnchorSlot(ctx context.Context, tx pgx.Tx, input ExtractRenderSegmentTailAnchorInput) error {
	var shotID string
	return tx.QueryRow(ctx, `
		SELECT shot.id::text
		FROM storyboard_shots shot
		WHERE shot.organization_id = $1 AND shot.project_id = $2 AND shot.id = $3
		FOR UPDATE
	`, input.OrganizationID, input.ProjectID, input.ShotID).Scan(&shotID)
}

type renderSegmentTailSourceQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func validateRenderSegmentTailSource(ctx context.Context, db renderSegmentTailSourceQuerier, input ExtractRenderSegmentTailAnchorInput, lock bool) error {
	query := `
		SELECT COALESCE(segment.artifact_id::text, ''), COALESCE(segment.media_file_id::text, ''),
		       COALESCE(segment.storage_key, ''), segment.status
		FROM video_render_segments segment
		JOIN video_render_plans plan ON plan.id = segment.video_render_plan_id
		WHERE segment.id = $1 AND segment.storyboard_shot_id = $2
		  AND segment.project_id = $3 AND segment.organization_id = $4
		  AND plan.active = true AND plan.production_generation_id = segment.production_generation_id`
	if lock {
		query += " FOR UPDATE OF segment"
	}
	var artifactID, mediaFileID, storageKey, status string
	if err := db.QueryRow(ctx, query, input.SourceRenderSegmentID, input.ShotID, input.ProjectID, input.OrganizationID).Scan(
		&artifactID, &mediaFileID, &storageKey, &status,
	); err != nil {
		return err
	}
	if status != "succeeded" {
		return fmt.Errorf("source render segment is not a succeeded output")
	}
	if artifactID != input.SourceVideoArtifactID || storageKey != input.SourceVideoStorageKey {
		return fmt.Errorf("source render segment changed before tail anchor extraction")
	}
	if input.SourceVideoMediaFileID != "" && mediaFileID != input.SourceVideoMediaFileID {
		return fmt.Errorf("source render segment media changed before tail anchor extraction")
	}
	return nil
}

func (a Activities) findRenderSegmentTailAnchor(ctx context.Context, input ExtractRenderSegmentTailAnchorInput) (ExtractRenderSegmentTailAnchorOutput, bool, error) {
	return scanRenderSegmentTailAnchor(a.db.QueryRow(ctx, renderSegmentTailAnchorSelectSQL,
		input.OrganizationID, input.ProjectID, input.ShotID, input.SourceRenderSegmentID, input.SourceVideoArtifactID))
}

func findRenderSegmentTailAnchorTx(ctx context.Context, tx pgx.Tx, input ExtractRenderSegmentTailAnchorInput) (ExtractRenderSegmentTailAnchorOutput, bool, error) {
	return scanRenderSegmentTailAnchor(tx.QueryRow(ctx, renderSegmentTailAnchorSelectSQL+" FOR UPDATE OF anchor",
		input.OrganizationID, input.ProjectID, input.ShotID, input.SourceRenderSegmentID, input.SourceVideoArtifactID))
}

func scanRenderSegmentTailAnchor(row pgx.Row) (ExtractRenderSegmentTailAnchorOutput, bool, error) {
	var output ExtractRenderSegmentTailAnchorOutput
	err := row.Scan(
		&output.AnchorID, &output.SourceShotID, &output.SourceRenderSegmentID,
		&output.SourceVideoArtifactID, &output.ArtifactID, &output.MediaFileID,
		&output.StorageKey, &output.MimeType, &output.Width, &output.Height,
		&output.FrameTimeSeconds, &output.ContentHash, &output.GeneratedAt, &output.SourceProbe,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ExtractRenderSegmentTailAnchorOutput{}, false, nil
	}
	return output, err == nil, err
}

const renderSegmentTailAnchorSelectSQL = `
	SELECT anchor.id::text, anchor.storyboard_shot_id::text, anchor.source_render_segment_id::text,
	       anchor.source_video_artifact_id::text, anchor.artifact_id::text,
	       anchor.media_file_id::text, anchor.storage_key,
	       COALESCE(media.mime_type, 'image/png'), COALESCE(media.width, 0), COALESCE(media.height, 0),
	       COALESCE((anchor.metadata->>'frameTimeSeconds')::numeric, 0),
	       COALESCE(artifact.content_hash, media.checksum, ''), anchor.created_at,
	       COALESCE(anchor.metadata->'sourceProbe', '{}'::jsonb)
	FROM shot_visual_anchors anchor
	JOIN artifacts artifact ON artifact.id = anchor.artifact_id
	JOIN media_files media ON media.id = anchor.media_file_id
	WHERE anchor.organization_id = $1 AND anchor.project_id = $2 AND anchor.storyboard_shot_id = $3
	  AND anchor.source_render_segment_id = $4 AND anchor.source_video_artifact_id = $5
	  AND anchor.anchor_role = 'observed_tail_frame' AND anchor.source_role = 'previous_segment_tail'
	  AND anchor.status = 'ready' AND anchor.review_status = 'approved'`
