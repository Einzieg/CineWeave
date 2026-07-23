package workflows

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/commerce"
	mediapkg "github.com/Einzieg/cineweave/internal/media"
	"github.com/Einzieg/cineweave/internal/storage"
	storyboardtiming "github.com/Einzieg/cineweave/internal/storyboard"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/sdk/temporal"
)

type ComposeFinalVideoInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId"`
	CreatedBy      string `json:"createdBy"`

	SourceWorkflowRunID      string                           `json:"sourceWorkflowRunId,omitempty"`
	TimelineID               string                           `json:"timelineId,omitempty"`
	Title                    string                           `json:"title,omitempty"`
	AspectRatio              string                           `json:"aspectRatio"`
	Resolution               string                           `json:"resolution"`
	ProductionPartial        bool                             `json:"productionPartial,omitempty"`
	CommerceIdentity         *commerce.UnitGenerationIdentity `json:"commerceIdentity,omitempty"`
	ExpectedTimelineRevision int64                            `json:"expectedTimelineRevision,omitempty"`
}

type ComposeFinalVideoOutput struct {
	NodeRunID           string  `json:"nodeRunId"`
	ArtifactID          string  `json:"artifactId"`
	MediaFileID         string  `json:"mediaFileId"`
	StorageKey          string  `json:"storageKey"`
	MimeType            string  `json:"mimeType"`
	DurationSeconds     float64 `json:"durationSeconds,omitempty"`
	DurationTicks       int64   `json:"durationTicks,omitempty"`
	TimelineTimebase    int64   `json:"timelineTimebase,omitempty"`
	FPSNumerator        int     `json:"fpsNumerator,omitempty"`
	FPSDenominator      int     `json:"fpsDenominator,omitempty"`
	Width               int     `json:"width,omitempty"`
	Height              int     `json:"height,omitempty"`
	TimelineArtifactID  string  `json:"timelineArtifactId,omitempty"`
	FinalVideoVersionID string  `json:"finalVideoVersionId,omitempty"`
	NativeAudioStatus   string  `json:"nativeAudioStatus"`
	ProductionReadiness string  `json:"productionReadiness"`
}

type composeClipRecord struct {
	TimelineClipID        string
	ShotID                string
	ShotIndex             int
	ShotNo                int
	ClipIndex             int
	Title                 string
	Enabled               bool
	VideoArtifactID       string
	VideoMediaFileID      string
	StorageKey            string
	MimeType              string
	StartTick             int64
	EndTick               int64
	DurationTicks         int64
	SourceDurationTicks   int64
	TrimStartTick         int64
	TrimEndTick           *int64
	TimelineTimebase      int64
	FPSNumerator          int
	FPSDenominator        int
	DurationSeconds       float64
	TrimStartSeconds      float64
	TrimEndSeconds        *float64
	TargetDurationSeconds *float64
	NativeAudioStatus     string
	ProductionReadiness   string
	TextOverlays          []mediapkg.TextOverlay
}

type timelineManifest struct {
	WorkflowRunID    string                 `json:"workflowRunId"`
	ProjectID        string                 `json:"projectId"`
	TimelineID       string                 `json:"timelineId,omitempty"`
	TimelineTimebase int64                  `json:"timelineTimebase"`
	FPSNumerator     int                    `json:"fpsNumerator"`
	FPSDenominator   int                    `json:"fpsDenominator"`
	Clips            []timelineManifestClip `json:"clips"`
	Compose          map[string]string      `json:"compose"`
	EndCard          *mediapkg.EndCard      `json:"endCard,omitempty"`
}

type timelineManifestClip struct {
	TimelineClipID        string                 `json:"timelineClipId,omitempty"`
	ShotID                string                 `json:"shotId"`
	ShotNo                int                    `json:"shotNo"`
	ShotIndex             int                    `json:"shotIndex"`
	ClipIndex             int                    `json:"clipIndex"`
	Title                 string                 `json:"title,omitempty"`
	Enabled               bool                   `json:"enabled"`
	VideoArtifactID       string                 `json:"videoArtifactId"`
	VideoMediaFileID      string                 `json:"videoMediaFileId"`
	StorageKey            string                 `json:"storageKey"`
	StartTick             int64                  `json:"startTick"`
	EndTick               int64                  `json:"endTick"`
	DurationTicks         int64                  `json:"durationTicks"`
	SourceDurationTicks   int64                  `json:"sourceDurationTicks,omitempty"`
	TrimStartTick         int64                  `json:"trimStartTick,omitempty"`
	TrimEndTick           *int64                 `json:"trimEndTick,omitempty"`
	DurationSeconds       float64                `json:"durationSeconds,omitempty"`
	TrimStartSeconds      float64                `json:"trimStartSeconds,omitempty"`
	TrimEndSeconds        *float64               `json:"trimEndSeconds,omitempty"`
	TargetDurationSeconds *float64               `json:"targetDurationSeconds,omitempty"`
	TextOverlays          []mediapkg.TextOverlay `json:"textOverlays,omitempty"`
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
	if err := a.validateComposeTimelineIdentity(ctx, input); err != nil {
		return ComposeFinalVideoOutput{}, err
	}

	nodeExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeComposeFinalVideoKey,
		NodeType:       "media.compose",
		Input: mustJSON(map[string]any{
			"aspectRatio": input.AspectRatio,
			"resolution":  input.Resolution,
			"timelineId":  input.TimelineID,
			"title":       input.Title,
		}),
	})
	if err != nil {
		return ComposeFinalVideoOutput{}, err
	}

	sourceWorkflowRunID := firstNonEmptyString(input.SourceWorkflowRunID, input.WorkflowRunID)
	var clips []composeClipRecord
	if strings.TrimSpace(input.TimelineID) != "" {
		clips, err = a.composeTimelineClips(ctx, input)
	} else {
		project, settingsErr := a.projectProductionSettings(ctx, input.ProjectID, sourceWorkflowRunID)
		if settingsErr != nil {
			err = settingsErr
		} else {
			clips, err = a.composeVideoClips(ctx, sourceWorkflowRunID, project)
		}
	}
	if err != nil {
		return ComposeFinalVideoOutput{}, a.failComposeFinalVideo(ctx, input, nodeExecution, codeActivityFailed, err.Error())
	}
	if len(clips) == 0 {
		return ComposeFinalVideoOutput{}, a.failComposeFinalVideo(ctx, input, nodeExecution, codeNoVideoClipsToCompose, "no succeeded shot videos are available to compose")
	}
	objectStore, ok := a.storage.(mediapkg.ObjectStore)
	if !ok {
		return ComposeFinalVideoOutput{}, a.failComposeFinalVideo(ctx, input, nodeExecution, codeActivityFailed, "object storage does not support media compose")
	}

	var endCard *mediapkg.EndCard
	if strings.TrimSpace(input.TimelineID) != "" {
		endCard, err = a.composeTimelineEndCard(ctx, input)
		if err != nil {
			return ComposeFinalVideoOutput{}, a.failComposeFinalVideo(ctx, input, nodeExecution, codeActivityFailed, err.Error())
		}
	}
	manifest := buildTimelineManifest(input, clips)
	manifest.EndCard = endCard
	timelinePut, err := a.storage.PutJSON(ctx, timelineStorageKey(input), manifest)
	if err != nil {
		return ComposeFinalVideoOutput{}, a.failComposeFinalVideo(ctx, input, nodeExecution, codeActivityFailed, err.Error())
	}

	composeReq := mediapkg.ComposeRequest{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		Clips:          make([]mediapkg.Clip, 0, len(clips)),
		AspectRatio:    defaultString(input.AspectRatio, "16:9"),
		Resolution:     defaultString(input.Resolution, "720p"),
		FPSNumerator:   clips[0].FPSNumerator,
		FPSDenominator: clips[0].FPSDenominator,
		OutputMimeType: "video/mp4",
	}
	for _, clip := range clips {
		composeReq.Clips = append(composeReq.Clips, mediapkg.Clip{
			ShotID:                clip.ShotID,
			ShotIndex:             clip.ShotIndex,
			StorageKey:            clip.StorageKey,
			MimeType:              clip.MimeType,
			DurationSeconds:       clip.DurationSeconds,
			TrimStartSeconds:      clip.TrimStartSeconds,
			TrimEndSeconds:        clip.TrimEndSeconds,
			TargetDurationSeconds: clip.TargetDurationSeconds,
			TextOverlays:          clip.TextOverlays,
		})
	}
	composeReq.EndCard = endCard
	result, err := mediapkg.ComposeClipsWithStore(ctx, composeReq, objectStore)
	if err != nil {
		code := codeActivityFailed
		if errors.Is(err, mediapkg.ErrNoVideoClips) {
			code = codeNoVideoClipsToCompose
		}
		return ComposeFinalVideoOutput{}, a.failComposeFinalVideo(ctx, input, nodeExecution, code, err.Error())
	}
	output, err := a.completeComposeFinalVideo(ctx, input, nodeExecution, clips, timelinePut, result)
	if err != nil {
		return ComposeFinalVideoOutput{}, a.failComposeFinalVideo(ctx, input, nodeExecution, codeActivityFailed, err.Error())
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

func (a Activities) composeVideoClips(ctx context.Context, workflowRunID string, project ProjectProductionSettings) ([]composeClipRecord, error) {
	rows, err := a.db.Query(ctx, `
		SELECT
			s.id::text,
			s.shot_index,
			COALESCE(s.shot_no, s.shot_index + 1),
			COALESCE(s.video_artifact_id::text, ''),
			COALESCE(s.video_media_file_id::text, ''),
			COALESCE(s.video_storage_key, mf.storage_key, ''),
			COALESCE(mf.mime_type, 'video/mp4'),
			mf.duration_seconds::float8,
			s.start_tick,
			s.end_tick,
			s.planned_duration_ticks,
			$3::bigint,
			$4::integer,
			$5::integer,
			COALESCE(s.native_audio_status, 'not_requested'),
			COALESCE(s.production_readiness, 'ready')
		FROM storyboard_shots s
		LEFT JOIN media_files mf ON mf.id = s.video_media_file_id
		WHERE s.workflow_run_id = $1
		  AND s.production_generation_id = $2
		  AND s.status = 'video_succeeded'
		  AND COALESCE(s.video_storage_key, mf.storage_key, '') <> ''
		  AND s.deleted_at IS NULL
		ORDER BY s.shot_index ASC
	`, workflowRunID, project.ProductionGenerationID, project.TimelineTimebase, project.FPSNumerator, project.FPSDenominator)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	clips := make([]composeClipRecord, 0)
	for rows.Next() {
		var clip composeClipRecord
		var mediaDuration sql.NullFloat64
		if err := rows.Scan(
			&clip.ShotID,
			&clip.ShotIndex,
			&clip.ShotNo,
			&clip.VideoArtifactID,
			&clip.VideoMediaFileID,
			&clip.StorageKey,
			&clip.MimeType,
			&mediaDuration,
			&clip.StartTick,
			&clip.EndTick,
			&clip.DurationTicks,
			&clip.TimelineTimebase,
			&clip.FPSNumerator,
			&clip.FPSDenominator,
			&clip.NativeAudioStatus,
			&clip.ProductionReadiness,
		); err != nil {
			return nil, err
		}
		timebase, err := clip.timebase()
		if err != nil {
			return nil, err
		}
		clip.SourceDurationTicks = clip.DurationTicks
		if mediaDuration.Valid && mediaDuration.Float64 > 0 {
			clip.SourceDurationTicks = timebase.QuantizeTickNearest(timebase.SecondsToTicks(mediaDuration.Float64))
		}
		clip.TrimEndTick = int64Ptr(clip.SourceDurationTicks)
		clip.deriveSeconds()
		clip.ClipIndex = clip.ShotIndex
		clip.Enabled = true
		clips = append(clips, clip)
	}
	return clips, rows.Err()
}

func (a Activities) validateComposeTimelineIdentity(ctx context.Context, input ComposeFinalVideoInput) error {
	if strings.TrimSpace(input.TimelineID) == "" {
		if input.CommerceIdentity != nil {
			return fmt.Errorf("commerce final compose requires a timeline")
		}
		return nil
	}
	var organizationID, projectID, productionGenerationID string
	var timelineOrganizationID, timelineProjectID, timelineGenerationID string
	var scriptUnitID, unitGenerationID sql.NullString
	var revision int64
	err := a.db.QueryRow(ctx, `
		SELECT run.organization_id::text, run.project_id::text, run.production_generation_id::text,
		       timeline.organization_id::text, timeline.project_id::text,
		       timeline.production_generation_id::text,
		       timeline.commerce_script_unit_id::text,
		       timeline.commerce_script_unit_generation_id::text,
		       timeline.revision
		FROM workflow_runs run
		JOIN project_timelines timeline ON timeline.id = $2
		WHERE run.id = $1
	`, input.WorkflowRunID, input.TimelineID).Scan(
		&organizationID, &projectID, &productionGenerationID,
		&timelineOrganizationID, &timelineProjectID, &timelineGenerationID,
		&scriptUnitID, &unitGenerationID, &revision,
	)
	if err != nil {
		return err
	}
	if organizationID != input.OrganizationID || projectID != input.ProjectID ||
		timelineOrganizationID != input.OrganizationID || timelineProjectID != input.ProjectID ||
		timelineGenerationID != productionGenerationID {
		return fmt.Errorf("timeline and workflow production identity mismatch")
	}
	if input.CommerceIdentity == nil {
		if scriptUnitID.Valid || unitGenerationID.Valid {
			return fmt.Errorf("commerce timeline requires commerce compose identity")
		}
		return nil
	}
	identity := input.CommerceIdentity
	if err := ValidateCommerceUnitGenerationIdentity(*identity); err != nil {
		return err
	}
	if identity.OrganizationID != organizationID || identity.ProjectID != projectID ||
		identity.ProjectGenerationID != productionGenerationID ||
		!scriptUnitID.Valid || scriptUnitID.String != identity.ScriptUnitID ||
		!unitGenerationID.Valid || unitGenerationID.String != identity.UnitGenerationID ||
		input.ExpectedTimelineRevision < 1 || revision != input.ExpectedTimelineRevision {
		return commerce.Error{Code: CommerceCodeGenerationMismatch, Message: "成片时间线与当前脚本单元生产代不一致"}
	}
	return nil
}

func (a Activities) composeTimelineClips(ctx context.Context, input ComposeFinalVideoInput) ([]composeClipRecord, error) {
	rows, err := a.db.Query(ctx, `
		SELECT
			c.id::text,
			COALESCE(c.storyboard_shot_id::text, ''),
			COALESCE(s.shot_index, c.clip_index),
			COALESCE(s.shot_no, s.shot_index + 1, c.clip_index + 1),
			c.clip_index,
			COALESCE(c.title, s.title, s.visual, ''),
			COALESCE(c.enabled, true),
			COALESCE(c.video_artifact_id::text, s.video_artifact_id::text, ''),
			COALESCE(c.video_media_file_id::text, s.video_media_file_id::text, ''),
			COALESCE(c.source_storage_key, s.video_storage_key, mf.storage_key, va.storage_key, ''),
			COALESCE(mf.mime_type, va.mime_type, 'video/mp4'),
			c.start_tick,
			c.end_tick,
			(c.end_tick - c.start_tick),
			COALESCE(c.source_duration_ticks, s.planned_duration_ticks),
			c.trim_start_tick,
			c.trim_end_tick,
			t.timeline_timebase,
			t.fps_numerator,
			t.fps_denominator,
			COALESCE(s.native_audio_status, 'not_requested'),
			COALESCE(s.production_readiness, 'ready'),
			COALESCE((
				SELECT jsonb_agg(jsonb_build_object(
					'text', overlay.text_content,
					'startTick', overlay.start_tick,
					'endTick', overlay.end_tick,
					'position', COALESCE(NULLIF(overlay.style->>'position', ''), 'bottom')
				) ORDER BY overlay.ordinal)
				FROM commerce_timeline_overlays overlay
				WHERE overlay.timeline_clip_id = c.id AND overlay.role = 'onscreen_text'
			), '[]'::jsonb)
		FROM timeline_clips c
		JOIN project_timelines t ON t.id = c.timeline_id
		LEFT JOIN storyboard_shots s ON s.id = c.storyboard_shot_id
		LEFT JOIN media_files mf ON mf.id = COALESCE(c.video_media_file_id, s.video_media_file_id)
		LEFT JOIN artifacts va ON va.id = COALESCE(c.video_artifact_id, s.video_artifact_id)
		WHERE c.timeline_id = $1
		  AND t.organization_id = $2
		  AND t.project_id = $3
		  AND t.production_generation_id = (
		      SELECT production_generation_id FROM workflow_runs WHERE id = $4
		  )
		  AND c.enabled = true
		  AND COALESCE(c.source_storage_key, s.video_storage_key, mf.storage_key, va.storage_key, '') <> ''
		ORDER BY c.clip_index ASC
	`, input.TimelineID, input.OrganizationID, input.ProjectID, input.WorkflowRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	clips := make([]composeClipRecord, 0)
	for rows.Next() {
		var clip composeClipRecord
		var sourceDuration, trimEnd sql.NullInt64
		var overlaysRaw json.RawMessage
		if err := rows.Scan(
			&clip.TimelineClipID,
			&clip.ShotID,
			&clip.ShotIndex,
			&clip.ShotNo,
			&clip.ClipIndex,
			&clip.Title,
			&clip.Enabled,
			&clip.VideoArtifactID,
			&clip.VideoMediaFileID,
			&clip.StorageKey,
			&clip.MimeType,
			&clip.StartTick,
			&clip.EndTick,
			&clip.DurationTicks,
			&sourceDuration,
			&clip.TrimStartTick,
			&trimEnd,
			&clip.TimelineTimebase,
			&clip.FPSNumerator,
			&clip.FPSDenominator,
			&clip.NativeAudioStatus,
			&clip.ProductionReadiness,
			&overlaysRaw,
		); err != nil {
			return nil, err
		}
		if sourceDuration.Valid {
			clip.SourceDurationTicks = sourceDuration.Int64
		}
		if trimEnd.Valid {
			clip.TrimEndTick = int64Ptr(trimEnd.Int64)
		}
		if _, err := clip.timebase(); err != nil {
			return nil, err
		}
		clip.deriveSeconds()
		var overlayRows []struct {
			Text      string `json:"text"`
			StartTick int64  `json:"startTick"`
			EndTick   int64  `json:"endTick"`
			Position  string `json:"position"`
		}
		if err := json.Unmarshal(overlaysRaw, &overlayRows); err != nil {
			return nil, err
		}
		for _, overlay := range overlayRows {
			startTick := overlay.StartTick - clip.StartTick
			endTick := overlay.EndTick - clip.StartTick
			if startTick < 0 {
				startTick = 0
			}
			if endTick > clip.DurationTicks {
				endTick = clip.DurationTicks
			}
			if endTick <= startTick {
				continue
			}
			clip.TextOverlays = append(clip.TextOverlays, mediapkg.TextOverlay{
				Text: overlay.Text, StartSeconds: float64(startTick) / float64(clip.TimelineTimebase),
				EndSeconds: float64(endTick) / float64(clip.TimelineTimebase), Position: overlay.Position,
			})
		}
		clips = append(clips, clip)
	}
	return clips, rows.Err()
}

func (a Activities) composeTimelineEndCard(ctx context.Context, input ComposeFinalVideoInput) (*mediapkg.EndCard, error) {
	var text string
	var duration float64
	err := a.db.QueryRow(ctx, `
		SELECT overlay.text_content,
		       (overlay.end_tick - overlay.start_tick)::float8 / timeline.timeline_timebase::float8
		FROM commerce_timeline_overlays overlay
		JOIN project_timelines timeline ON timeline.id = overlay.timeline_id
		WHERE overlay.timeline_id = $1
		  AND overlay.role = 'cta_end_card'
		  AND overlay.organization_id = $2
		  AND overlay.project_id = $3
		  AND overlay.production_generation_id = (
		      SELECT production_generation_id FROM workflow_runs WHERE id = $4
		  )
		ORDER BY overlay.ordinal
		LIMIT 1
	`, input.TimelineID, input.OrganizationID, input.ProjectID, input.WorkflowRunID).Scan(&text, &duration)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &mediapkg.EndCard{Text: text, DurationSeconds: duration}, nil
}

func (a Activities) completeComposeFinalVideo(ctx context.Context, input ComposeFinalVideoInput, execution NodeExecution, clips []composeClipRecord, timelinePut storage.PutResult, result mediapkg.ComposeResult) (ComposeFinalVideoOutput, error) {
	if len(clips) == 0 {
		return ComposeFinalVideoOutput{}, fmt.Errorf("compose clips are required")
	}
	timebase, err := clips[0].timebase()
	if err != nil {
		return ComposeFinalVideoOutput{}, err
	}
	for index := 1; index < len(clips); index++ {
		if clips[index].TimelineTimebase != clips[0].TimelineTimebase ||
			clips[index].FPSNumerator != clips[0].FPSNumerator ||
			clips[index].FPSDenominator != clips[0].FPSDenominator {
			return ComposeFinalVideoOutput{}, fmt.Errorf("compose clips use inconsistent timebase snapshots")
		}
	}
	nativeAudioStatus, productionReadiness := composeClipProductionState(clips)
	if input.ProductionPartial {
		productionReadiness = "partial"
	}
	resultDurationTicks := timebase.QuantizeTickNearest(timebase.SecondsToTicks(result.DurationSeconds))
	if resultDurationTicks <= 0 {
		for _, clip := range clips {
			resultDurationTicks += clip.DurationTicks
		}
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ComposeFinalVideoOutput{}, err
	}
	defer tx.Rollback(ctx)

	runCtx, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution)
	if err != nil {
		return ComposeFinalVideoOutput{}, err
	}
	var timelineScriptUnitID, timelineUnitGenerationID sql.NullString
	var timelineRevision int64
	if strings.TrimSpace(input.TimelineID) != "" {
		if err := tx.QueryRow(ctx, `
			SELECT commerce_script_unit_id::text,
			       commerce_script_unit_generation_id::text,
			       revision
			FROM project_timelines
			WHERE id = $1 AND organization_id = $2 AND project_id = $3
			  AND production_generation_id = $4
			FOR UPDATE
		`, input.TimelineID, input.OrganizationID, input.ProjectID, runCtx.ProductionGenerationID).Scan(
			&timelineScriptUnitID, &timelineUnitGenerationID, &timelineRevision,
		); err != nil {
			return ComposeFinalVideoOutput{}, err
		}
		if input.CommerceIdentity == nil {
			if timelineScriptUnitID.Valid || timelineUnitGenerationID.Valid {
				return ComposeFinalVideoOutput{}, fmt.Errorf("commerce timeline requires commerce compose identity")
			}
		} else if input.CommerceIdentity.ProjectGenerationID != runCtx.ProductionGenerationID ||
			!timelineScriptUnitID.Valid || timelineScriptUnitID.String != input.CommerceIdentity.ScriptUnitID ||
			!timelineUnitGenerationID.Valid || timelineUnitGenerationID.String != input.CommerceIdentity.UnitGenerationID ||
			input.ExpectedTimelineRevision < 1 || timelineRevision != input.ExpectedTimelineRevision {
			return ComposeFinalVideoOutput{}, commerce.Error{Code: CommerceCodeGenerationMismatch, Message: "成片时间线已变化，请重新提交合成任务"}
		}
	}

	shotIDs := make([]string, 0, len(clips))
	clipStorageKeys := make([]string, 0, len(clips))
	for _, clip := range clips {
		shotIDs = append(shotIDs, clip.ShotID)
		clipStorageKeys = append(clipStorageKeys, clip.StorageKey)
	}

	var timelineArtifactID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, metadata, created_by, production_generation_id)
		VALUES ($1, $2, $3, $4, 'timeline_json', $5, 'application/json', $6, $7, $8, $9)
		RETURNING id
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, execution.NodeRunID, timelinePut.StorageKey, timelinePut.ContentHash, mustJSON(map[string]any{
		"source":   "media_worker",
		"type":     "timeline_manifest",
		"byteSize": timelinePut.ByteSize,
	}), nullIfEmpty(input.CreatedBy), runCtx.ProductionGenerationID).Scan(&timelineArtifactID); err != nil {
		return ComposeFinalVideoOutput{}, err
	}

	finalMetadata := map[string]any{
		"source":              "media_worker",
		"nodeKey":             nodeComposeFinalVideoKey,
		"nodeRunId":           execution.NodeRunID,
		"workflowRunId":       input.WorkflowRunID,
		"sourceWorkflowRunId": firstNonEmptyString(input.SourceWorkflowRunID, input.WorkflowRunID),
		"timelineId":          input.TimelineID,
		"staleState":          "fresh",
		"shotIds":             shotIDs,
		"clipStorageKeys":     clipStorageKeys,
		"clipCount":           len(clips),
		"durationTicks":       resultDurationTicks,
		"timelineTimebase":    timebase.TicksPerSecond,
		"fpsNumerator":        timebase.FPSNumerator,
		"fpsDenominator":      timebase.FPSDenominator,
		"composeSettings": map[string]any{
			"aspectRatio": defaultString(input.AspectRatio, "16:9"),
			"resolution":  defaultString(input.Resolution, "720p"),
			"format":      "mp4",
		},
		"timelineArtifactId":  timelineArtifactID,
		"nativeAudioStatus":   nativeAudioStatus,
		"productionReadiness": productionReadiness,
		"mediaProbe":          result.Probe,
	}
	if input.CommerceIdentity != nil {
		finalMetadata["commerceScriptUnitId"] = input.CommerceIdentity.ScriptUnitID
		finalMetadata["commerceScriptUnitGenerationId"] = input.CommerceIdentity.UnitGenerationID
		finalMetadata["timelineRevision"] = input.ExpectedTimelineRevision
	}
	var finalArtifactID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, metadata, created_by, production_generation_id)
		VALUES ($1, $2, $3, $4, 'final_video', $5, $6, $7, $8, $9, $10)
		RETURNING id
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, execution.NodeRunID, result.StorageKey, result.MimeType, result.ContentHash, mustJSON(finalMetadata), nullIfEmpty(input.CreatedBy), runCtx.ProductionGenerationID).Scan(&finalArtifactID); err != nil {
		return ComposeFinalVideoOutput{}, err
	}
	var mediaFileID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO media_files(organization_id, project_id, artifact_id, storage_key, mime_type, byte_size, width, height, duration_seconds, checksum, metadata, created_by, production_generation_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id
	`, input.OrganizationID, input.ProjectID, finalArtifactID, result.StorageKey, result.MimeType, nullInt64(result.ByteSize), nullInt(result.Width), nullInt(result.Height), nullFloat(result.DurationSeconds), nullIfEmpty(result.ContentHash), mustJSON(map[string]any{
		"source":              "media_worker",
		"workflowRunId":       input.WorkflowRunID,
		"sourceWorkflowRunId": firstNonEmptyString(input.SourceWorkflowRunID, input.WorkflowRunID),
		"timelineId":          input.TimelineID,
		"shotIds":             shotIDs,
		"clipCount":           len(clips),
	}), nullIfEmpty(input.CreatedBy), runCtx.ProductionGenerationID).Scan(&mediaFileID); err != nil {
		return ComposeFinalVideoOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE artifacts
		SET metadata = metadata || $2::jsonb
		WHERE id = $1
	`, finalArtifactID, mustJSON(map[string]any{"mediaFileId": mediaFileID})); err != nil {
		return ComposeFinalVideoOutput{}, err
	}

	var finalVideoVersionID string
	if strings.TrimSpace(input.TimelineID) != "" {
		status := "ready"
		var activeFinalVideoVersionID sql.NullString
		if input.CommerceIdentity == nil {
			if err := tx.QueryRow(ctx, `
				SELECT active_final_video_version_id::text
				FROM projects
				WHERE id = $1
				FOR UPDATE
			`, input.ProjectID).Scan(&activeFinalVideoVersionID); err != nil {
				return ComposeFinalVideoOutput{}, err
			}
		} else {
			if err := tx.QueryRow(ctx, `
				SELECT id::text
				FROM final_video_versions
				WHERE project_id = $1 AND commerce_script_unit_id = $2 AND status = 'active'
				ORDER BY version DESC
				LIMIT 1
				FOR UPDATE
			`, input.ProjectID, input.CommerceIdentity.ScriptUnitID).Scan(&activeFinalVideoVersionID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return ComposeFinalVideoOutput{}, err
			}
		}
		if productionReadiness == "ready" && (!activeFinalVideoVersionID.Valid || strings.TrimSpace(activeFinalVideoVersionID.String) == "") {
			status = "active"
		}
		var version int
		versionQuery := `
			SELECT COALESCE(MAX(version), 0) + 1
			FROM final_video_versions
			WHERE project_id = $1 AND production_generation_id = $2
		`
		versionArgs := []any{input.ProjectID, runCtx.ProductionGenerationID}
		if input.CommerceIdentity != nil {
			versionQuery = `
				SELECT COALESCE(MAX(version), 0) + 1
				FROM final_video_versions
				WHERE project_id = $1 AND commerce_script_unit_id = $2
			`
			versionArgs = []any{input.ProjectID, input.CommerceIdentity.ScriptUnitID}
		}
		if err := tx.QueryRow(ctx, versionQuery, versionArgs...).Scan(&version); err != nil {
			return ComposeFinalVideoOutput{}, err
		}
		title := strings.TrimSpace(input.Title)
		if title == "" {
			title = fmt.Sprintf("成片 v%d", version)
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO final_video_versions(
				organization_id, project_id, timeline_id, workflow_run_id, version, title, status,
				artifact_id, media_file_id, storage_key, duration_ticks, resolution, aspect_ratio,
				compose_settings, metadata, created_by, native_audio_status, production_readiness,
				production_generation_id, commerce_script_unit_id,
				commerce_script_unit_generation_id
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19,
			        NULLIF($20, '')::uuid, NULLIF($21, '')::uuid)
			RETURNING id::text
		`, input.OrganizationID, input.ProjectID, input.TimelineID, input.WorkflowRunID, version, title, status,
			finalArtifactID, mediaFileID, result.StorageKey, nullInt64(resultDurationTicks),
			defaultString(input.Resolution, "720p"), defaultString(input.AspectRatio, "16:9"),
			mustJSON(map[string]any{
				"aspectRatio": defaultString(input.AspectRatio, "16:9"),
				"resolution":  defaultString(input.Resolution, "720p"),
				"format":      "mp4",
			}),
			mustJSON(map[string]any{
				"source":             "compose_timeline",
				"nodeRunId":          execution.NodeRunID,
				"timelineArtifactId": timelineArtifactID,
				"clipCount":          len(clips),
			}),
			nullIfEmpty(input.CreatedBy),
			nativeAudioStatus,
			productionReadiness,
			runCtx.ProductionGenerationID,
			optionalCommerceIdentityValue(input.CommerceIdentity, func(identity commerce.UnitGenerationIdentity) string { return identity.ScriptUnitID }),
			optionalCommerceIdentityValue(input.CommerceIdentity, func(identity commerce.UnitGenerationIdentity) string { return identity.UnitGenerationID }),
		).Scan(&finalVideoVersionID); err != nil {
			return ComposeFinalVideoOutput{}, err
		}
		if status == "active" && input.CommerceIdentity == nil {
			if _, err := tx.Exec(ctx, `
				UPDATE projects
				SET active_final_video_version_id = $2
				WHERE id = $1
			`, input.ProjectID, finalVideoVersionID); err != nil {
				return ComposeFinalVideoOutput{}, err
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE artifacts
			SET metadata = metadata || $2::jsonb
			WHERE id = $1
		`, finalArtifactID, mustJSON(map[string]any{"finalVideoVersionId": finalVideoVersionID})); err != nil {
			return ComposeFinalVideoOutput{}, err
		}
	}

	output := ComposeFinalVideoOutput{
		NodeRunID:           execution.NodeRunID,
		ArtifactID:          finalArtifactID,
		MediaFileID:         mediaFileID,
		StorageKey:          result.StorageKey,
		MimeType:            result.MimeType,
		DurationSeconds:     result.DurationSeconds,
		DurationTicks:       resultDurationTicks,
		TimelineTimebase:    timebase.TicksPerSecond,
		FPSNumerator:        int(timebase.FPSNumerator),
		FPSDenominator:      int(timebase.FPSDenominator),
		Width:               result.Width,
		Height:              result.Height,
		TimelineArtifactID:  timelineArtifactID,
		FinalVideoVersionID: finalVideoVersionID,
		NativeAudioStatus:   nativeAudioStatus,
		ProductionReadiness: productionReadiness,
	}
	outputJSON := mustJSON(output)
	if _, err := completeNodeRunTx(ctx, tx, execution, outputJSON); err != nil {
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
			"nodeRunId":     execution.NodeRunID,
			"storageKey":    timelinePut.StorageKey,
			"type":          "timeline_json",
		})},
		{"artifact.created", "artifact", finalArtifactID, mustJSON(map[string]any{
			"artifactId":    finalArtifactID,
			"workflowRunId": input.WorkflowRunID,
			"nodeRunId":     execution.NodeRunID,
			"storageKey":    result.StorageKey,
			"type":          "final_video",
			"mediaFileId":   mediaFileID,
		})},
		{"media.compose.completed", "workflow_node_run", execution.NodeRunID, mustJSON(map[string]any{
			"workflowRunId":       input.WorkflowRunID,
			"artifactId":          finalArtifactID,
			"mediaFileId":         mediaFileID,
			"storageKey":          result.StorageKey,
			"timelineId":          input.TimelineID,
			"timelineArtifactId":  timelineArtifactID,
			"finalVideoVersionId": finalVideoVersionID,
			"clipCount":           len(clips),
		})},
	}
	if input.CommerceIdentity != nil && finalVideoVersionID != "" {
		events = append(events, struct {
			eventType     string
			aggregateType string
			aggregateID   string
			payload       json.RawMessage
		}{
			eventType: "commerce.final_video.completed", aggregateType: "final_video_version", aggregateID: finalVideoVersionID,
			payload: mustJSON(map[string]any{
				"workflowRunId":                   input.WorkflowRunID,
				"finalVideoVersionId":             finalVideoVersionID,
				"timelineId":                      input.TimelineID,
				"commerceScriptUnitId":            input.CommerceIdentity.ScriptUnitID,
				"scriptUnitGenerationId":          input.CommerceIdentity.UnitGenerationID,
				"commerceWorkflowBindingId":       input.CommerceIdentity.CommerceWorkflowBindingID,
				"commerceWorkflowBindingRevision": input.CommerceIdentity.CommerceWorkflowBindingRevision,
				"status":                          statusForCommerceFinalEvent(productionReadiness),
			}),
		})
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

func optionalCommerceIdentityValue(identity *commerce.UnitGenerationIdentity, selector func(commerce.UnitGenerationIdentity) string) string {
	if identity == nil || selector == nil {
		return ""
	}
	return strings.TrimSpace(selector(*identity))
}

func statusForCommerceFinalEvent(readiness string) string {
	if readiness == "ready" {
		return "ready"
	}
	return "preview_only"
}

func composeClipProductionState(clips []composeClipRecord) (string, string) {
	audioStatus := "not_requested"
	readiness := "ready"
	for _, clip := range clips {
		switch clip.NativeAudioStatus {
		case "needs_audio_retry":
			audioStatus = "needs_audio_retry"
		case "native_audio_unavailable":
			if audioStatus != "needs_audio_retry" {
				audioStatus = "native_audio_unavailable"
			}
		case "audio_unverified":
			if audioStatus != "needs_audio_retry" && audioStatus != "native_audio_unavailable" {
				audioStatus = "audio_unverified"
			}
		case "audio_verified":
			if audioStatus == "not_requested" {
				audioStatus = "audio_verified"
			}
		}
		switch clip.ProductionReadiness {
		case "blocked":
			readiness = "blocked"
		case "partial":
			if readiness != "blocked" {
				readiness = "partial"
			}
		case "preview_only":
			if readiness == "ready" {
				readiness = "preview_only"
			}
		}
	}
	return audioStatus, readiness
}

func (a Activities) failComposeFinalVideo(ctx context.Context, input ComposeFinalVideoInput, execution NodeExecution, code, message string) error {
	output := mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID,
		"nodeRunId":     execution.NodeRunID,
		"nodeKey":       nodeComposeFinalVideoKey,
		"code":          code,
		"message":       message,
	})
	if !execution.valid() {
		_ = TransitionWorkflowRun(ctx, a.db, input.WorkflowRunID, "failed", code, message, output)
		return temporal.NewApplicationError(message, code)
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return temporal.NewApplicationError(message, code)
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return temporal.NewApplicationError(message, code)
	}
	if _, err := failNodeRunTx(ctx, tx, execution, code, message, output); err != nil {
		return temporal.NewApplicationError(message, code)
	}
	if _, _, err := transitionWorkflowRunTx(ctx, tx, input.WorkflowRunID, "failed", code, message, output); err != nil {
		return temporal.NewApplicationError(message, code)
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "media.compose.failed", "workflow_node_run", execution.NodeRunID, output); err != nil {
		return temporal.NewApplicationError(message, code)
	}
	_ = tx.Commit(ctx)
	return temporal.NewApplicationError(message, code)
}

func buildTimelineManifest(input ComposeFinalVideoInput, clips []composeClipRecord) timelineManifest {
	manifest := timelineManifest{
		WorkflowRunID: input.WorkflowRunID,
		ProjectID:     input.ProjectID,
		TimelineID:    input.TimelineID,
		Clips:         make([]timelineManifestClip, 0, len(clips)),
		Compose: map[string]string{
			"aspectRatio":         defaultString(input.AspectRatio, "16:9"),
			"resolution":          defaultString(input.Resolution, "720p"),
			"format":              "mp4",
			"sourceWorkflowRunId": firstNonEmptyString(input.SourceWorkflowRunID, input.WorkflowRunID),
			"title":               strings.TrimSpace(input.Title),
		},
	}
	// The end card is loaded and rendered from the same frozen timeline overlay
	// contract in ComposeFinalVideo. It is attached to the artifact metadata by
	// the caller rather than inferred from prompt text.
	if len(clips) > 0 {
		manifest.TimelineTimebase = clips[0].TimelineTimebase
		manifest.FPSNumerator = clips[0].FPSNumerator
		manifest.FPSDenominator = clips[0].FPSDenominator
	}
	for _, clip := range clips {
		manifest.Clips = append(manifest.Clips, timelineManifestClip{
			TimelineClipID:        clip.TimelineClipID,
			ShotID:                clip.ShotID,
			ShotNo:                clip.ShotNo,
			ShotIndex:             clip.ShotIndex,
			ClipIndex:             clip.ClipIndex,
			Title:                 clip.Title,
			Enabled:               clip.Enabled,
			VideoArtifactID:       clip.VideoArtifactID,
			VideoMediaFileID:      clip.VideoMediaFileID,
			StorageKey:            clip.StorageKey,
			StartTick:             clip.StartTick,
			EndTick:               clip.EndTick,
			DurationTicks:         clip.DurationTicks,
			SourceDurationTicks:   clip.SourceDurationTicks,
			TrimStartTick:         clip.TrimStartTick,
			TrimEndTick:           clip.TrimEndTick,
			DurationSeconds:       clip.DurationSeconds,
			TrimStartSeconds:      clip.TrimStartSeconds,
			TrimEndSeconds:        clip.TrimEndSeconds,
			TargetDurationSeconds: clip.TargetDurationSeconds,
			TextOverlays:          append([]mediapkg.TextOverlay(nil), clip.TextOverlays...),
		})
	}
	return manifest
}

func timelineStorageKey(input ComposeFinalVideoInput) string {
	return fmt.Sprintf("org/%s/project/%s/workflow/%s/timeline/timeline.json", input.OrganizationID, input.ProjectID, input.WorkflowRunID)
}

func (clip composeClipRecord) timebase() (storyboardtiming.Timebase, error) {
	timebase := storyboardtiming.Timebase{
		TicksPerSecond: clip.TimelineTimebase,
		FPSNumerator:   int64(clip.FPSNumerator),
		FPSDenominator: int64(clip.FPSDenominator),
	}
	return timebase, timebase.Validate()
}

func (clip *composeClipRecord) deriveSeconds() {
	if clip == nil || clip.TimelineTimebase <= 0 {
		return
	}
	timebase := float64(clip.TimelineTimebase)
	clip.DurationSeconds = float64(clip.SourceDurationTicks) / timebase
	clip.TrimStartSeconds = float64(clip.TrimStartTick) / timebase
	if clip.TrimEndTick != nil {
		value := float64(*clip.TrimEndTick) / timebase
		clip.TrimEndSeconds = &value
	}
	if clip.DurationTicks > 0 {
		value := float64(clip.DurationTicks) / timebase
		clip.TargetDurationSeconds = &value
	}
}

func int64Ptr(value int64) *int64 {
	return &value
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
