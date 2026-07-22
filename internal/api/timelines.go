package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/production"
	storyboardtiming "github.com/Einzieg/cineweave/internal/storyboard"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/jackc/pgx/v5"
)

type ProjectTimeline struct {
	ID               string          `json:"id"`
	OrganizationID   string          `json:"organizationId"`
	ProjectID        string          `json:"projectId"`
	WorkflowRunID    *string         `json:"workflowRunId,omitempty"`
	Title            string          `json:"title"`
	Status           string          `json:"status"`
	AspectRatio      string          `json:"aspectRatio"`
	Resolution       string          `json:"resolution"`
	TimelineTimebase int64           `json:"timelineTimebase"`
	FPSNumerator     int             `json:"fpsNumerator"`
	FPSDenominator   int             `json:"fpsDenominator"`
	Metadata         json.RawMessage `json:"metadata"`
	CreatedBy        *string         `json:"createdBy,omitempty"`
	EditedBy         *string         `json:"editedBy,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
	EditedAt         *time.Time      `json:"editedAt,omitempty"`
}

type TimelineClip struct {
	ID                    string          `json:"id"`
	OrganizationID        string          `json:"organizationId"`
	ProjectID             string          `json:"projectId"`
	TimelineID            string          `json:"timelineId"`
	StoryboardShotID      *string         `json:"storyboardShotId,omitempty"`
	VideoArtifactID       *string         `json:"videoArtifactId,omitempty"`
	VideoMediaFileID      *string         `json:"videoMediaFileId,omitempty"`
	ClipIndex             int             `json:"clipIndex"`
	Title                 string          `json:"title"`
	Enabled               bool            `json:"enabled"`
	SourceStorageKey      *string         `json:"sourceStorageKey,omitempty"`
	StartTick             int64           `json:"startTick"`
	EndTick               int64           `json:"endTick"`
	DurationTicks         int64           `json:"durationTicks"`
	SourceDurationTicks   *int64          `json:"sourceDurationTicks,omitempty"`
	TrimStartTick         int64           `json:"trimStartTick"`
	TrimEndTick           *int64          `json:"trimEndTick,omitempty"`
	TimelineTimebase      int64           `json:"timelineTimebase"`
	FPSNumerator          int             `json:"fpsNumerator"`
	FPSDenominator        int             `json:"fpsDenominator"`
	DurationSeconds       float64         `json:"durationSeconds"`
	SourceDurationSeconds *float64        `json:"sourceDurationSeconds,omitempty"`
	TrimStartSeconds      float64         `json:"trimStartSeconds"`
	TrimEndSeconds        *float64        `json:"trimEndSeconds,omitempty"`
	Notes                 *string         `json:"notes,omitempty"`
	Metadata              json.RawMessage `json:"metadata"`
	CreatedAt             time.Time       `json:"createdAt"`
	UpdatedAt             time.Time       `json:"updatedAt"`
}

type TimelineClipDetail struct {
	TimelineClip
	Shot          *StoryboardShot `json:"shot,omitempty"`
	VideoArtifact *Artifact       `json:"videoArtifact,omitempty"`
	PreviewURL    *string         `json:"previewUrl,omitempty"`
}

type TimelineDetail struct {
	Timeline           ProjectTimeline      `json:"timeline"`
	Clips              []TimelineClipDetail `json:"clips"`
	FinalVideoVersions []FinalVideoVersion  `json:"finalVideoVersions"`
}

type FinalVideoVersion struct {
	ID                  string          `json:"id"`
	OrganizationID      string          `json:"organizationId"`
	ProjectID           string          `json:"projectId"`
	TimelineID          string          `json:"timelineId"`
	WorkflowRunID       *string         `json:"workflowRunId,omitempty"`
	Version             int             `json:"version"`
	Title               string          `json:"title"`
	Status              string          `json:"status"`
	NativeAudioStatus   string          `json:"nativeAudioStatus"`
	ProductionReadiness string          `json:"productionReadiness"`
	ArtifactID          *string         `json:"artifactId,omitempty"`
	MediaFileID         *string         `json:"mediaFileId,omitempty"`
	StorageKey          *string         `json:"storageKey,omitempty"`
	DurationTicks       *int64          `json:"durationTicks,omitempty"`
	DurationSeconds     *float64        `json:"durationSeconds,omitempty"`
	TimelineTimebase    int64           `json:"timelineTimebase"`
	FPSNumerator        int             `json:"fpsNumerator"`
	FPSDenominator      int             `json:"fpsDenominator"`
	Resolution          string          `json:"resolution"`
	AspectRatio         string          `json:"aspectRatio"`
	ComposeSettings     json.RawMessage `json:"composeSettings"`
	Metadata            json.RawMessage `json:"metadata"`
	CreatedBy           *string         `json:"createdBy,omitempty"`
	CreatedAt           time.Time       `json:"createdAt"`
	PreviewURL          *string         `json:"previewUrl,omitempty"`
}

type ComposeTimelineResponse struct {
	WorkflowRunID string `json:"workflowRunId"`
	TimelineID    string `json:"timelineId"`
	Status        string `json:"status"`
}

func (s *Server) listProjectTimelines(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, workflow_run_id::text, title, status, aspect_ratio, resolution,
		       timeline_timebase, fps_numerator, fps_denominator,
		       metadata, created_by::text, edited_by::text, created_at, updated_at, edited_at
		FROM project_timelines
		WHERE project_id = $1
		  AND production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
		ORDER BY CASE status WHEN 'active' THEN 0 WHEN 'draft' THEN 1 ELSE 2 END, created_at DESC
		LIMIT 100
	`, project.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]ProjectTimeline, 0)
	for rows.Next() {
		item, err := scanProjectTimeline(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createProjectTimeline(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	var req struct {
		Title               string `json:"title"`
		AspectRatio         string `json:"aspectRatio"`
		Resolution          string `json:"resolution"`
		FromStoryboardShots bool   `json:"fromStoryboardShots"`
	}
	if !decode(w, r, &req) {
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "默认时间线"
	}
	aspectRatio := defaultAPIString(req.AspectRatio, project.VideoRatio, stringValue(project.AspectRatio), "16:9")
	resolution := defaultAPIString(req.Resolution, "720p")

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	productionContext, err := lockActiveVideoProductionContext(r.Context(), tx, project.ID)
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}

	var item ProjectTimeline
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO project_timelines(
			organization_id, project_id, title, status, aspect_ratio, resolution,
			timeline_timebase, fps_numerator, fps_denominator, metadata, created_by,
			production_generation_id
		)
		VALUES ($1, $2, $3, 'draft', $4, $5, $6, $7, $8, '{}', $9, $10)
		RETURNING id, organization_id, project_id, workflow_run_id::text, title, status, aspect_ratio, resolution,
		          timeline_timebase, fps_numerator, fps_denominator,
		          metadata, created_by::text, edited_by::text, created_at, updated_at, edited_at
	`, project.OrganizationID, project.ID, title, aspectRatio, resolution,
		project.TimelineTimebase, project.FPSNumerator, project.FPSDenominator, principal.UserID, productionContext.Generation.ID).Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.WorkflowRunID, &item.Title, &item.Status, &item.AspectRatio, &item.Resolution,
		&item.TimelineTimebase, &item.FPSNumerator, &item.FPSDenominator,
		&item.Metadata, &item.CreatedBy, &item.EditedBy, &item.CreatedAt, &item.UpdatedAt, &item.EditedAt,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if req.FromStoryboardShots {
		if err := s.createTimelineClipsFromStoryboard(r, tx, project, item.ID); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) getProjectTimeline(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	item, err := s.timelineByID(r, project.ID, r.PathValue("timelineId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) updateProjectTimeline(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	var req struct {
		Title       *string `json:"title"`
		Status      *string `json:"status"`
		AspectRatio *string `json:"aspectRatio"`
		Resolution  *string `json:"resolution"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Status != nil && !validTimelineStatus(*req.Status) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "timeline status is invalid", nil, false)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	productionContext, err := lockActiveVideoProductionContext(r.Context(), tx, project.ID)
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	item, err := scanProjectTimeline(tx.QueryRow(r.Context(), `
		UPDATE project_timelines
		SET title = COALESCE($3, title),
		    status = COALESCE($4, status),
		    aspect_ratio = COALESCE($5, aspect_ratio),
		    resolution = COALESCE($6, resolution),
		    edited_by = $7,
		    edited_at = now()
		WHERE project_id = $1 AND id = $2 AND production_generation_id = $8
		RETURNING id, organization_id, project_id, workflow_run_id::text, title, status, aspect_ratio, resolution,
		          timeline_timebase, fps_numerator, fps_denominator,
		          metadata, created_by::text, edited_by::text, created_at, updated_at, edited_at
	`, project.ID, r.PathValue("timelineId"), normalizedOptionalString(req.Title), normalizedOptionalString(req.Status),
		normalizedOptionalString(req.AspectRatio), normalizedOptionalString(req.Resolution), principal.UserID, productionContext.Generation.ID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) deleteProjectTimeline(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	productionContext, err := lockActiveVideoProductionContext(r.Context(), tx, project.ID)
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	tag, err := tx.Exec(r.Context(), `DELETE FROM project_timelines WHERE project_id = $1 AND id = $2 AND production_generation_id = $3`, project.ID, r.PathValue("timelineId"), productionContext.Generation.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		s.writeError(w, r, pgx.ErrNoRows)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"deleted": true}, nil)
}

func (s *Server) listTimelineClips(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	if _, err := s.timelineByID(r, project.ID, r.PathValue("timelineId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	items, err := s.timelineClips(r, project.ID, r.PathValue("timelineId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createTimelineClip(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	timeline, err := s.timelineByID(r, project.ID, r.PathValue("timelineId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req struct {
		StoryboardShotID    string `json:"storyboardShotId"`
		VideoArtifactID     string `json:"videoArtifactId"`
		VideoMediaFileID    string `json:"videoMediaFileId"`
		ClipIndex           *int   `json:"clipIndex"`
		Title               string `json:"title"`
		Enabled             *bool  `json:"enabled"`
		SourceStorageKey    string `json:"sourceStorageKey"`
		SourceDurationTicks *int64 `json:"sourceDurationTicks"`
		TrimStartTick       *int64 `json:"trimStartTick"`
		TrimEndTick         *int64 `json:"trimEndTick"`
		DurationTicks       *int64 `json:"durationTicks"`
		Notes               string `json:"notes"`
	}
	if !decode(w, r, &req) {
		return
	}
	timebase, err := projectTimelineTimebase(timeline)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "INVALID_TIMELINE_TIMEBASE", err.Error(), nil, false)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	productionContext, err := lockActiveVideoProductionContext(r.Context(), tx, project.ID)
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `SET CONSTRAINTS timeline_clips_timeline_index_unique DEFERRED`); err != nil {
		s.writeError(w, r, err)
		return
	}
	var clipCount int
	if err := tx.QueryRow(r.Context(), `SELECT COUNT(*) FROM timeline_clips WHERE timeline_id = $1`, timeline.ID).Scan(&clipCount); err != nil {
		s.writeError(w, r, err)
		return
	}
	clipIndex := clipCount
	if req.ClipIndex != nil {
		clipIndex = *req.ClipIndex
	}
	if clipIndex < 0 || clipIndex > clipCount {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "clipIndex is outside the timeline range", nil, false)
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	title := strings.TrimSpace(req.Title)
	videoArtifactID := strings.TrimSpace(req.VideoArtifactID)
	videoMediaFileID := strings.TrimSpace(req.VideoMediaFileID)
	sourceStorageKey := strings.TrimSpace(req.SourceStorageKey)
	sourceDurationTicks := req.SourceDurationTicks
	var shotDurationTicks *int64
	if strings.TrimSpace(req.StoryboardShotID) != "" {
		var shotTitle sql.NullString
		var mediaDuration sql.NullFloat64
		var plannedDuration int64
		if err := tx.QueryRow(r.Context(), `
			SELECT COALESCE(video_artifact_id::text, ''), COALESCE(video_media_file_id::text, ''),
			       COALESCE(video_storage_key, mf.storage_key, va.storage_key, ''),
			       mf.duration_seconds::float8,
			       planned_duration_ticks,
			       COALESCE(title, visual, '')
			FROM storyboard_shots s
			LEFT JOIN media_files mf ON mf.id = s.video_media_file_id
			LEFT JOIN artifacts va ON va.id = s.video_artifact_id
			WHERE s.project_id = $1 AND s.id = $2 AND s.deleted_at IS NULL
		`, project.ID, req.StoryboardShotID).Scan(&videoArtifactID, &videoMediaFileID, &sourceStorageKey, &mediaDuration, &plannedDuration, &shotTitle); err != nil {
			s.writeError(w, r, err)
			return
		}
		shotDurationTicks = &plannedDuration
		if sourceDurationTicks == nil {
			value := plannedDuration
			if mediaDuration.Valid && mediaDuration.Float64 > 0 {
				value = timebase.QuantizeTickNearest(timebase.SecondsToTicks(mediaDuration.Float64))
			}
			sourceDurationTicks = &value
		}
		if title == "" && shotTitle.Valid {
			title = shotTitle.String
		}
	}
	if title == "" {
		title = "镜头片段"
	}
	trimStartTick := int64(0)
	if req.TrimStartTick != nil {
		trimStartTick = *req.TrimStartTick
	}
	durationTicks := req.DurationTicks
	if durationTicks == nil && shotDurationTicks != nil {
		durationTicks = shotDurationTicks
	}
	sourceDurationTicks, trimStartTick, trimEndTick, resolvedDurationTicks, err := resolveTimelineClipTiming(
		timebase, sourceDurationTicks, trimStartTick, req.TrimEndTick, durationTicks,
	)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error(), nil, false)
		return
	}
	if clipIndex < clipCount {
		if _, err := tx.Exec(r.Context(), `
			UPDATE timeline_clips
			SET clip_index = clip_index + 1
			WHERE timeline_id = $1 AND clip_index >= $2
		`, timeline.ID, clipIndex); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	var clipID string
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO timeline_clips(
			organization_id, project_id, timeline_id, storyboard_shot_id, video_artifact_id, video_media_file_id,
			clip_index, title, enabled, source_storage_key, source_duration_ticks,
			trim_start_tick, trim_end_tick, start_tick, end_tick, notes, metadata, manual_override, stale_state, edited_by, edited_at,
			production_generation_id
		)
		VALUES ($1, $2, $3, NULLIF($4, '')::uuid, NULLIF($5, '')::uuid, NULLIF($6, '')::uuid,
		        $7, $8, $9, NULLIF($10, ''), $11, $12, $13, 0, $14, NULLIF($15, ''), '{}', true, 'fresh', $16, now(), $17)
		RETURNING id::text
	`, project.OrganizationID, project.ID, timeline.ID, strings.TrimSpace(req.StoryboardShotID), videoArtifactID, videoMediaFileID,
		clipIndex, title, enabled, sourceStorageKey, nullableInt64Ptr(sourceDurationTicks), trimStartTick,
		nullableInt64Ptr(trimEndTick), resolvedDurationTicks, strings.TrimSpace(req.Notes), principal.UserID, productionContext.Generation.ID).Scan(&clipID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := reflowTimelineClipTicks(r.Context(), tx, timeline.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := markTimelineEdited(r.Context(), tx, project.ID, timeline.ID, principal.UserID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := s.timelineClipByID(r, project.ID, timeline.ID, clipID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) updateTimelineClip(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	current, err := s.timelineClipByID(r, project.ID, r.PathValue("timelineId"), r.PathValue("clipId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var patch map[string]json.RawMessage
	if !decode(w, r, &patch) {
		return
	}
	timeline, err := s.timelineByID(r, project.ID, r.PathValue("timelineId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	timebase, err := projectTimelineTimebase(timeline)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "INVALID_TIMELINE_TIMEBASE", err.Error(), nil, false)
		return
	}
	if raw, ok := patch["title"]; ok {
		if value, ok := decodePatchString(w, r, raw, "title"); ok {
			current.Title = value
		} else {
			return
		}
	}
	if raw, ok := patch["enabled"]; ok {
		if value, ok := decodePatchBool(w, r, raw, "enabled"); ok {
			current.Enabled = value
		} else {
			return
		}
	}
	if raw, ok := patch["trimStartTick"]; ok {
		if value, ok := decodePatchInt64(w, r, raw, "trimStartTick"); ok {
			current.TrimStartTick = value
		} else {
			return
		}
	}
	if raw, ok := patch["trimEndTick"]; ok {
		value, ok := decodePatchNullableInt64(w, r, raw, "trimEndTick")
		if !ok {
			return
		}
		current.TrimEndTick = value
	}
	durationTicks := current.DurationTicks
	if raw, ok := patch["durationTicks"]; ok {
		value, ok := decodePatchInt64(w, r, raw, "durationTicks")
		if !ok {
			return
		}
		durationTicks = value
	}
	if raw, ok := patch["notes"]; ok {
		value, ok := decodePatchNullableString(w, r, raw, "notes")
		if !ok {
			return
		}
		current.Notes = value
	}
	_, trimStartTick, trimEndTick, durationTicks, err := resolveTimelineClipTiming(
		timebase, current.SourceDurationTicks, current.TrimStartTick, current.TrimEndTick, &durationTicks,
	)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error(), nil, false)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	productionContext, err := lockActiveVideoProductionContext(r.Context(), tx, project.ID)
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE timeline_clips
		SET title = $4,
		    enabled = $5,
		    trim_start_tick = $6,
		    trim_end_tick = $7,
		    end_tick = start_tick + $8,
		    notes = $9,
		    manual_override = true,
		    stale_state = 'fresh',
		    edited_by = $10,
		    edited_at = now()
		WHERE project_id = $1 AND timeline_id = $2 AND id = $3 AND production_generation_id = $11
	`, project.ID, r.PathValue("timelineId"), r.PathValue("clipId"), current.Title, current.Enabled,
		trimStartTick, nullableInt64Ptr(trimEndTick), durationTicks, nullableStringPtr(current.Notes), principal.UserID, productionContext.Generation.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := reflowTimelineClipTicks(r.Context(), tx, timeline.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := markTimelineEdited(r.Context(), tx, project.ID, timeline.ID, principal.UserID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := s.timelineClipByID(r, project.ID, timeline.ID, r.PathValue("clipId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) deleteTimelineClip(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	productionContext, err := lockActiveVideoProductionContext(r.Context(), tx, project.ID)
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	tag, err := tx.Exec(r.Context(), `
		DELETE FROM timeline_clips
		WHERE project_id = $1 AND timeline_id = $2 AND id = $3 AND production_generation_id = $4
	`, project.ID, r.PathValue("timelineId"), r.PathValue("clipId"), productionContext.Generation.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		s.writeError(w, r, pgx.ErrNoRows)
		return
	}
	if err := reindexTimelineClips(r.Context(), tx, r.PathValue("timelineId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := reflowTimelineClipTicks(r.Context(), tx, r.PathValue("timelineId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := markTimelineEdited(r.Context(), tx, project.ID, r.PathValue("timelineId"), principal.UserID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "clipId": r.PathValue("clipId")}, nil)
}

func (s *Server) reorderTimelineClips(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	var req struct {
		Items []struct {
			ClipID    string `json:"clipId"`
			ClipIndex int    `json:"clipIndex"`
		} `json:"items"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Items) == 0 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "items is required", nil, false)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	productionContext, err := lockActiveVideoProductionContext(r.Context(), tx, project.ID)
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `SET CONSTRAINTS timeline_clips_timeline_index_unique DEFERRED`); err != nil {
		s.writeError(w, r, err)
		return
	}
	for _, item := range req.Items {
		tag, err := tx.Exec(r.Context(), `
			UPDATE timeline_clips
			SET clip_index = $4
			WHERE project_id = $1 AND timeline_id = $2 AND id = $3 AND production_generation_id = $5
		`, project.ID, r.PathValue("timelineId"), item.ClipID, item.ClipIndex, productionContext.Generation.ID)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		if tag.RowsAffected() == 0 {
			s.writeError(w, r, pgx.ErrNoRows)
			return
		}
	}
	if err := reindexTimelineClips(r.Context(), tx, r.PathValue("timelineId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := reflowTimelineClipTicks(r.Context(), tx, r.PathValue("timelineId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := markTimelineEdited(r.Context(), tx, project.ID, r.PathValue("timelineId"), principal.UserID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": req.Items}, nil)
}

func (s *Server) getTimelineDetail(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	timeline, err := s.timelineByID(r, project.ID, r.PathValue("timelineId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	clips, err := s.timelineClipDetails(r, project.ID, timeline.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	versions, err := s.finalVideoVersions(r, project.ID, timeline.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, TimelineDetail{Timeline: timeline, Clips: clips, FinalVideoVersions: versions}, nil)
}

func (s *Server) composeTimeline(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowRun)
	if !ok {
		return
	}
	timeline, err := s.timelineByID(r, project.ID, r.PathValue("timelineId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if ok, err := s.projectShotVideosReady(r, project.ID); err != nil {
		s.writeError(w, r, err)
		return
	} else if !ok {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "SHOT_VIDEOS_REQUIRED", "all storyboard shots must have completed video before composing final video", nil, false)
		return
	}
	var req struct {
		Title       string `json:"title"`
		Resolution  string `json:"resolution"`
		AspectRatio string `json:"aspectRatio"`
	}
	if !decode(w, r, &req) {
		return
	}
	workflowType := "compose_timeline"
	input := map[string]any{
		"timelineId":  timeline.ID,
		"title":       defaultAPIString(req.Title, timeline.Title),
		"resolution":  defaultAPIString(req.Resolution, timeline.Resolution, "720p"),
		"aspectRatio": defaultAPIString(req.AspectRatio, timeline.AspectRatio, "16:9"),
	}
	run, ok := s.startProjectWorkflow(w, r, principal, project, workflowType, input, workflows.ComposeTimelineWorkflow)
	if !ok {
		return
	}
	if _, err := s.db.Exec(r.Context(), `
		UPDATE project_timelines
		SET workflow_run_id = $2, status = 'active', edited_by = $3, edited_at = now()
		WHERE id = $1
	`, timeline.ID, run.ID, principal.UserID); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, ComposeTimelineResponse{WorkflowRunID: run.ID, TimelineID: timeline.ID, Status: run.Status}, nil)
}

func (s *Server) projectShotVideosReady(r *http.Request, projectID string) (bool, error) {
	var missing int
	err := s.db.QueryRow(r.Context(), `
		SELECT COUNT(*)
		FROM storyboard_shots
		WHERE project_id = $1
		  AND deleted_at IS NULL
		  AND NOT (
		    (COALESCE(video_status, '') = 'succeeded' OR COALESCE(status, '') = 'video_succeeded')
		    AND (video_artifact_id IS NOT NULL OR video_media_file_id IS NOT NULL OR COALESCE(video_storage_key, '') <> '')
		  )
	`, projectID).Scan(&missing)
	return missing == 0, err
}

func (s *Server) listFinalVideos(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	items, err := s.finalVideoVersions(r, project.ID, "")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) getFinalVideo(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	item, err := s.finalVideoVersionByID(r, project.ID, r.PathValue("versionId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) activateFinalVideo(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	if _, err := s.requireFinalVideoProductionReady(r.Context(), project.ID, r.PathValue("versionId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	productionContext, err := lockActiveVideoProductionContext(r.Context(), tx, project.ID)
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE final_video_versions SET status = 'ready' WHERE project_id = $1 AND status = 'active' AND id <> $2 AND production_generation_id = $3`, project.ID, r.PathValue("versionId"), productionContext.Generation.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	tag, err := tx.Exec(r.Context(), `UPDATE final_video_versions SET status = 'active' WHERE project_id = $1 AND id = $2 AND production_generation_id = $3`, project.ID, r.PathValue("versionId"), productionContext.Generation.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		s.writeError(w, r, pgx.ErrNoRows)
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE projects SET active_final_video_version_id = $2 WHERE id = $1`, project.ID, r.PathValue("versionId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := s.finalVideoVersionByID(r, project.ID, r.PathValue("versionId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) deleteFinalVideo(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	current, err := s.finalVideoVersionByID(r, project.ID, r.PathValue("versionId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if current.Status == "active" && !strings.EqualFold(r.URL.Query().Get("confirmActive"), "true") {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "ACTIVE_FINAL_VIDEO_REQUIRES_CONFIRMATION", "active final video deletion requires confirmActive=true", nil, false)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	productionContext, err := lockActiveVideoProductionContext(r.Context(), tx, project.ID)
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE projects SET active_final_video_version_id = NULL WHERE id = $1 AND active_final_video_version_id = $2`, project.ID, r.PathValue("versionId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	tag, err := tx.Exec(r.Context(), `DELETE FROM final_video_versions WHERE project_id = $1 AND id = $2 AND production_generation_id = $3`, project.ID, r.PathValue("versionId"), productionContext.Generation.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		s.writeError(w, r, pgx.ErrNoRows)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "versionId": r.PathValue("versionId")}, nil)
}

func (s *Server) createTimelineClipsFromStoryboard(r *http.Request, tx pgx.Tx, project Project, timelineID string) error {
	timebase := storyboardtiming.Timebase{
		TicksPerSecond: project.TimelineTimebase,
		FPSNumerator:   int64(project.FPSNumerator),
		FPSDenominator: int64(project.FPSDenominator),
	}
	if err := timebase.Validate(); err != nil {
		return err
	}
	var productionGenerationID string
	if err := tx.QueryRow(r.Context(), `
		SELECT production_generation_id::text
		FROM project_timelines
		WHERE id = $1 AND project_id = $2
	`, timelineID, project.ID).Scan(&productionGenerationID); err != nil {
		return err
	}
	rows, err := tx.Query(r.Context(), `
		SELECT s.id::text, COALESCE(s.video_artifact_id::text, ''), COALESCE(s.video_media_file_id::text, ''),
		       COALESCE(s.video_storage_key, mf.storage_key, va.storage_key, ''),
		       mf.duration_seconds::float8,
		       s.planned_duration_ticks,
		       COALESCE(s.title, s.visual, '')
		FROM storyboard_shots s
		LEFT JOIN media_files mf ON mf.id = s.video_media_file_id
		LEFT JOIN artifacts va ON va.id = s.video_artifact_id
		WHERE s.project_id = $1
		  AND s.production_generation_id = $2
		  AND s.deleted_at IS NULL
		  AND (COALESCE(s.video_status, '') = 'succeeded' OR COALESCE(s.status, '') = 'video_succeeded')
		  AND COALESCE(s.video_storage_key, mf.storage_key, va.storage_key, '') <> ''
		ORDER BY s.shot_index ASC
	`, project.ID, productionGenerationID)
	if err != nil {
		return err
	}
	type sourceShot struct {
		shotID               string
		artifactID           string
		mediaFileID          string
		storageKey           string
		title                string
		mediaDuration        sql.NullFloat64
		plannedDurationTicks int64
	}
	shots := make([]sourceShot, 0)
	for rows.Next() {
		var shot sourceShot
		if err := rows.Scan(
			&shot.shotID,
			&shot.artifactID,
			&shot.mediaFileID,
			&shot.storageKey,
			&shot.mediaDuration,
			&shot.plannedDurationTicks,
			&shot.title,
		); err != nil {
			rows.Close()
			return err
		}
		shots = append(shots, shot)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	startTick := int64(0)
	for index, shot := range shots {
		if strings.TrimSpace(shot.title) == "" {
			shot.title = "镜头片段"
		}
		sourceDurationTicks := shot.plannedDurationTicks
		if shot.mediaDuration.Valid && shot.mediaDuration.Float64 > 0 {
			sourceDurationTicks = timebase.QuantizeTickNearest(timebase.SecondsToTicks(shot.mediaDuration.Float64))
		}
		if sourceDurationTicks <= 0 {
			sourceDurationTicks = shot.plannedDurationTicks
		}
		endTick := startTick + shot.plannedDurationTicks
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO timeline_clips(
				organization_id, project_id, timeline_id, storyboard_shot_id, video_artifact_id, video_media_file_id,
				clip_index, title, enabled, source_storage_key, source_duration_ticks,
				trim_start_tick, trim_end_tick, start_tick, end_tick, metadata, stale_state,
				production_generation_id
			)
			VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid, NULLIF($6, '')::uuid, $7, $8, true, $9, $10, 0, $10, $11, $12, '{}', 'fresh', $13)
		`, project.OrganizationID, project.ID, timelineID, shot.shotID, shot.artifactID, shot.mediaFileID, index, shot.title, shot.storageKey,
			sourceDurationTicks, startTick, endTick, productionGenerationID); err != nil {
			return err
		}
		startTick = endTick
	}
	return nil
}

func (s *Server) timelineByID(r *http.Request, projectID, timelineID string) (ProjectTimeline, error) {
	return scanProjectTimeline(s.db.QueryRow(r.Context(), `
		SELECT id, organization_id, project_id, workflow_run_id::text, title, status, aspect_ratio, resolution,
		       timeline_timebase, fps_numerator, fps_denominator,
		       metadata, created_by::text, edited_by::text, created_at, updated_at, edited_at
		FROM project_timelines
		WHERE project_id = $1 AND id = $2
		  AND production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
	`, projectID, timelineID))
}

func scanProjectTimeline(row rowScan) (ProjectTimeline, error) {
	var item ProjectTimeline
	var workflowRunID, createdBy, editedBy sql.NullString
	var editedAt sql.NullTime
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &workflowRunID, &item.Title, &item.Status, &item.AspectRatio, &item.Resolution,
		&item.TimelineTimebase, &item.FPSNumerator, &item.FPSDenominator,
		&item.Metadata, &createdBy, &editedBy, &item.CreatedAt, &item.UpdatedAt, &editedAt,
	)
	item.WorkflowRunID = stringPtrFromNull(workflowRunID)
	item.CreatedBy = stringPtrFromNull(createdBy)
	item.EditedBy = stringPtrFromNull(editedBy)
	if editedAt.Valid {
		item.EditedAt = &editedAt.Time
	}
	return item, err
}

func lockActiveVideoProductionContext(ctx context.Context, tx pgx.Tx, projectID string) (videoproduction.Context, error) {
	active, err := videoproduction.LoadActiveContext(ctx, tx, projectID)
	if err != nil {
		return videoproduction.Context{}, err
	}
	return videoproduction.AssertWritableTx(
		ctx, tx, projectID, active.Generation.ID, active.Binding.ID, active.Binding.Revision,
	)
}

func (s *Server) timelineClips(r *http.Request, projectID, timelineID string) ([]TimelineClip, error) {
	rows, err := s.db.Query(r.Context(), `
		SELECT `+timelineClipColumns("c", "t")+`
		FROM timeline_clips c
		JOIN project_timelines t ON t.id = c.timeline_id
		WHERE c.project_id = $1 AND c.timeline_id = $2
		  AND c.production_generation_id = t.production_generation_id
		  AND t.production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
		ORDER BY c.clip_index ASC
	`, projectID, timelineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]TimelineClip, 0)
	for rows.Next() {
		item, err := scanTimelineClip(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) timelineClipByID(r *http.Request, projectID, timelineID, clipID string) (TimelineClip, error) {
	return scanTimelineClip(s.db.QueryRow(r.Context(), `
		SELECT `+timelineClipColumns("c", "t")+`
		FROM timeline_clips c
		JOIN project_timelines t ON t.id = c.timeline_id
		WHERE c.project_id = $1 AND c.timeline_id = $2 AND c.id = $3
		  AND c.production_generation_id = t.production_generation_id
		  AND t.production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
	`, projectID, timelineID, clipID))
}

func timelineClipColumns(clipAlias, timelineAlias string) string {
	return fmt.Sprintf(`
		%[1]s.id, %[1]s.organization_id, %[1]s.project_id, %[1]s.timeline_id,
		%[1]s.storyboard_shot_id::text, %[1]s.video_artifact_id::text,
		%[1]s.video_media_file_id::text, %[1]s.clip_index, %[1]s.title, %[1]s.enabled, %[1]s.source_storage_key,
		%[1]s.start_tick, %[1]s.end_tick, (%[1]s.end_tick - %[1]s.start_tick),
		%[1]s.source_duration_ticks, %[1]s.trim_start_tick, %[1]s.trim_end_tick,
		%[2]s.timeline_timebase, %[2]s.fps_numerator, %[2]s.fps_denominator,
		%[1]s.notes, %[1]s.metadata, %[1]s.created_at, %[1]s.updated_at
	`, clipAlias, timelineAlias)
}

func scanTimelineClip(row rowScan) (TimelineClip, error) {
	var item TimelineClip
	var storyboardShotID, artifactID, mediaFileID, storageKey, notes sql.NullString
	var sourceDuration, trimEnd sql.NullInt64
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.TimelineID, &storyboardShotID, &artifactID,
		&mediaFileID, &item.ClipIndex, &item.Title, &item.Enabled, &storageKey,
		&item.StartTick, &item.EndTick, &item.DurationTicks,
		&sourceDuration, &item.TrimStartTick, &trimEnd,
		&item.TimelineTimebase, &item.FPSNumerator, &item.FPSDenominator,
		&notes, &item.Metadata, &item.CreatedAt, &item.UpdatedAt,
	)
	item.StoryboardShotID = stringPtrFromNull(storyboardShotID)
	item.VideoArtifactID = stringPtrFromNull(artifactID)
	item.VideoMediaFileID = stringPtrFromNull(mediaFileID)
	item.SourceStorageKey = stringPtrFromNull(storageKey)
	item.Notes = stringPtrFromNull(notes)
	if sourceDuration.Valid {
		item.SourceDurationTicks = &sourceDuration.Int64
	}
	if trimEnd.Valid {
		item.TrimEndTick = &trimEnd.Int64
	}
	item.attachDerivedSeconds()
	return item, err
}

func (s *Server) timelineClipDetails(r *http.Request, projectID, timelineID string) ([]TimelineClipDetail, error) {
	clips, err := s.timelineClips(r, projectID, timelineID)
	if err != nil {
		return nil, err
	}
	items := make([]TimelineClipDetail, 0, len(clips))
	for _, clip := range clips {
		detail := TimelineClipDetail{TimelineClip: clip}
		if clip.StoryboardShotID != nil {
			if shot, err := s.storyboardShotByID(r, projectID, *clip.StoryboardShotID); err == nil {
				if s.storage != nil {
					_ = s.attachShotPreviewURLs(r, &shot, previewURLExpiryFromRequest(r))
				}
				detail.Shot = &shot
				detail.PreviewURL = shot.VideoPreviewURL
				if detail.VideoArtifact == nil && shot.VideoArtifactID != nil {
					artifact, preview := s.optionalArtifactWithPreview(r, *shot.VideoArtifactID)
					detail.VideoArtifact = artifact
					if detail.PreviewURL == nil {
						detail.PreviewURL = preview
					}
				}
			} else if err != pgx.ErrNoRows {
				return nil, err
			}
		}
		if clip.VideoArtifactID != nil {
			artifact, preview := s.optionalArtifactWithPreview(r, *clip.VideoArtifactID)
			detail.VideoArtifact = artifact
			if detail.PreviewURL == nil {
				detail.PreviewURL = preview
			}
		}
		if detail.PreviewURL == nil && clip.SourceStorageKey != nil {
			detail.PreviewURL = s.previewURLForStorageKey(r, *clip.SourceStorageKey)
		}
		items = append(items, detail)
	}
	return items, nil
}

func (s *Server) finalVideoVersions(r *http.Request, projectID, timelineID string) ([]FinalVideoVersion, error) {
	query := `
		SELECT version_row.id, version_row.organization_id, version_row.project_id, version_row.timeline_id,
		       version_row.workflow_run_id::text, version_row.version, version_row.title, version_row.status,
		       version_row.artifact_id::text, version_row.media_file_id::text, version_row.storage_key, version_row.duration_ticks,
		       timeline.timeline_timebase, timeline.fps_numerator, timeline.fps_denominator,
		       version_row.resolution, version_row.aspect_ratio, version_row.native_audio_status, version_row.production_readiness,
		       version_row.compose_settings, version_row.metadata,
		       version_row.created_by::text, version_row.created_at
		FROM final_video_versions version_row
		JOIN project_timelines timeline ON timeline.id = version_row.timeline_id
		WHERE version_row.project_id = $1
		  AND version_row.production_generation_id = timeline.production_generation_id
		  AND timeline.production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
	`
	args := []any{projectID}
	if strings.TrimSpace(timelineID) != "" {
		query += " AND version_row.timeline_id = $2"
		args = append(args, timelineID)
	}
	query += " ORDER BY CASE version_row.status WHEN 'active' THEN 0 WHEN 'ready' THEN 1 ELSE 2 END, version_row.version DESC, version_row.created_at DESC"
	rows, err := s.db.Query(r.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]FinalVideoVersion, 0)
	for rows.Next() {
		item, err := scanFinalVideoVersion(rows)
		if err != nil {
			return nil, err
		}
		s.attachFinalVideoPreview(r, &item)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) finalVideoVersionByID(r *http.Request, projectID, versionID string) (FinalVideoVersion, error) {
	item, err := scanFinalVideoVersion(s.db.QueryRow(r.Context(), `
		SELECT version_row.id, version_row.organization_id, version_row.project_id, version_row.timeline_id,
		       version_row.workflow_run_id::text, version_row.version, version_row.title, version_row.status,
		       version_row.artifact_id::text, version_row.media_file_id::text, version_row.storage_key, version_row.duration_ticks,
		       timeline.timeline_timebase, timeline.fps_numerator, timeline.fps_denominator,
		       version_row.resolution, version_row.aspect_ratio, version_row.native_audio_status, version_row.production_readiness,
		       version_row.compose_settings, version_row.metadata,
		       version_row.created_by::text, version_row.created_at
		FROM final_video_versions version_row
		JOIN project_timelines timeline ON timeline.id = version_row.timeline_id
		WHERE version_row.project_id = $1 AND version_row.id = $2
		  AND version_row.production_generation_id = timeline.production_generation_id
		  AND timeline.production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
	`, projectID, versionID))
	if err != nil {
		return FinalVideoVersion{}, err
	}
	s.attachFinalVideoPreview(r, &item)
	return item, nil
}

func scanFinalVideoVersion(row rowScan) (FinalVideoVersion, error) {
	var item FinalVideoVersion
	var workflowRunID, artifactID, mediaFileID, storageKey, createdBy sql.NullString
	var duration sql.NullInt64
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.TimelineID, &workflowRunID, &item.Version, &item.Title, &item.Status,
		&artifactID, &mediaFileID, &storageKey, &duration,
		&item.TimelineTimebase, &item.FPSNumerator, &item.FPSDenominator,
		&item.Resolution, &item.AspectRatio, &item.NativeAudioStatus, &item.ProductionReadiness, &item.ComposeSettings,
		&item.Metadata, &createdBy, &item.CreatedAt,
	)
	item.WorkflowRunID = stringPtrFromNull(workflowRunID)
	item.ArtifactID = stringPtrFromNull(artifactID)
	item.MediaFileID = stringPtrFromNull(mediaFileID)
	item.StorageKey = stringPtrFromNull(storageKey)
	item.CreatedBy = stringPtrFromNull(createdBy)
	if duration.Valid {
		item.DurationTicks = &duration.Int64
		seconds := float64(duration.Int64) / float64(item.TimelineTimebase)
		item.DurationSeconds = &seconds
	}
	return item, err
}

func (s *Server) attachFinalVideoPreview(r *http.Request, item *FinalVideoVersion) {
	if item == nil || item.StorageKey == nil {
		return
	}
	item.PreviewURL = s.previewURLForStorageKey(r, *item.StorageKey)
}

func projectTimelineTimebase(timeline ProjectTimeline) (storyboardtiming.Timebase, error) {
	timebase := storyboardtiming.Timebase{
		TicksPerSecond: timeline.TimelineTimebase,
		FPSNumerator:   int64(timeline.FPSNumerator),
		FPSDenominator: int64(timeline.FPSDenominator),
	}
	return timebase, timebase.Validate()
}

func resolveTimelineClipTiming(
	timebase storyboardtiming.Timebase,
	sourceDurationTicks *int64,
	trimStartTick int64,
	trimEndTick *int64,
	durationTicks *int64,
) (*int64, int64, *int64, int64, error) {
	if err := timebase.Validate(); err != nil {
		return nil, 0, nil, 0, err
	}
	if trimStartTick < 0 || !timebase.IsFrameAligned(trimStartTick) {
		return nil, 0, nil, 0, fmt.Errorf("trimStartTick must be non-negative and frame-aligned")
	}
	if sourceDurationTicks != nil {
		if *sourceDurationTicks <= 0 || !timebase.IsFrameAligned(*sourceDurationTicks) {
			return nil, 0, nil, 0, fmt.Errorf("sourceDurationTicks must be positive and frame-aligned")
		}
		value := *sourceDurationTicks
		sourceDurationTicks = &value
	}
	if trimEndTick == nil && sourceDurationTicks != nil {
		value := *sourceDurationTicks
		trimEndTick = &value
	}
	if trimEndTick != nil {
		if *trimEndTick <= trimStartTick || !timebase.IsFrameAligned(*trimEndTick) {
			return nil, 0, nil, 0, fmt.Errorf("trimEndTick must be after trimStartTick and frame-aligned")
		}
		if sourceDurationTicks != nil && *trimEndTick > *sourceDurationTicks {
			return nil, 0, nil, 0, fmt.Errorf("trimEndTick cannot exceed sourceDurationTicks")
		}
		value := *trimEndTick
		trimEndTick = &value
	}
	resolvedDuration := int64(0)
	if durationTicks != nil {
		resolvedDuration = *durationTicks
	} else if trimEndTick != nil {
		resolvedDuration = *trimEndTick - trimStartTick
	} else if sourceDurationTicks != nil {
		resolvedDuration = *sourceDurationTicks - trimStartTick
	}
	if resolvedDuration <= 0 || !timebase.IsFrameAligned(resolvedDuration) {
		return nil, 0, nil, 0, fmt.Errorf("durationTicks must be positive and frame-aligned")
	}
	return sourceDurationTicks, trimStartTick, trimEndTick, resolvedDuration, nil
}

func reindexTimelineClips(ctx context.Context, tx pgx.Tx, timelineID string) error {
	if _, err := tx.Exec(ctx, `SET CONSTRAINTS timeline_clips_timeline_index_unique DEFERRED`); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		WITH ordered AS (
			SELECT id, ROW_NUMBER() OVER (ORDER BY clip_index, id) - 1 AS next_index
			FROM timeline_clips
			WHERE timeline_id = $1
		)
		UPDATE timeline_clips clip
		SET clip_index = ordered.next_index
		FROM ordered
		WHERE clip.id = ordered.id
	`, timelineID)
	return err
}

func reflowTimelineClipTicks(ctx context.Context, tx pgx.Tx, timelineID string) error {
	_, err := tx.Exec(ctx, `
		WITH durations AS (
			SELECT id, clip_index, GREATEST(end_tick - start_tick, 1) AS duration_ticks
			FROM timeline_clips
			WHERE timeline_id = $1
		), positioned AS (
			SELECT id,
			       COALESCE(SUM(duration_ticks) OVER (
			         ORDER BY clip_index, id
			         ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING
			       ), 0)::BIGINT AS next_start_tick,
			       duration_ticks
			FROM durations
		)
		UPDATE timeline_clips clip
		SET start_tick = positioned.next_start_tick,
		    end_tick = positioned.next_start_tick + positioned.duration_ticks
		FROM positioned
		WHERE clip.id = positioned.id
	`, timelineID)
	return err
}

func markTimelineEdited(ctx context.Context, tx pgx.Tx, projectID, timelineID, userID string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE project_timelines
		SET manual_override = true,
		    stale_state = 'fresh',
		    edited_by = NULLIF($3, '')::uuid,
		    edited_at = now(),
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('timingEditedAt', now())
		WHERE project_id = $1 AND id = $2
	`, projectID, timelineID, userID); err != nil {
		return err
	}
	return production.MarkFinalVideoStale(ctx, tx, projectID, "")
}

func (item *TimelineClip) attachDerivedSeconds() {
	if item == nil || item.TimelineTimebase <= 0 {
		return
	}
	denominator := float64(item.TimelineTimebase)
	item.DurationSeconds = float64(item.DurationTicks) / denominator
	item.TrimStartSeconds = float64(item.TrimStartTick) / denominator
	if item.SourceDurationTicks != nil {
		value := float64(*item.SourceDurationTicks) / denominator
		item.SourceDurationSeconds = &value
	}
	if item.TrimEndTick != nil {
		value := float64(*item.TrimEndTick) / denominator
		item.TrimEndSeconds = &value
	}
}

func validTimelineStatus(value string) bool {
	switch strings.TrimSpace(value) {
	case "draft", "active", "archived":
		return true
	default:
		return false
	}
}

func defaultAPIString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nullableInt64Ptr(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableStringPtr(value *string) any {
	if value == nil {
		return nil
	}
	return nullableString(*value)
}

func decodePatchString(w http.ResponseWriter, r *http.Request, raw json.RawMessage, field string) (string, bool) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", field+" must be a string", nil, false)
		return "", false
	}
	return strings.TrimSpace(value), true
}

func decodePatchNullableString(w http.ResponseWriter, r *http.Request, raw json.RawMessage, field string) (*string, bool) {
	if isJSONNull(raw) {
		return nil, true
	}
	value, ok := decodePatchString(w, r, raw, field)
	if !ok {
		return nil, false
	}
	return stringPtrFromValue(value), true
}

func decodePatchBool(w http.ResponseWriter, r *http.Request, raw json.RawMessage, field string) (bool, bool) {
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", field+" must be a boolean", nil, false)
		return false, false
	}
	return value, true
}

func decodePatchInt64(w http.ResponseWriter, r *http.Request, raw json.RawMessage, field string) (int64, bool) {
	var value int64
	if err := json.Unmarshal(raw, &value); err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", field+" must be an integer", nil, false)
		return 0, false
	}
	return value, true
}

func decodePatchNullableInt64(w http.ResponseWriter, r *http.Request, raw json.RawMessage, field string) (*int64, bool) {
	if isJSONNull(raw) {
		return nil, true
	}
	value, ok := decodePatchInt64(w, r, raw, field)
	if !ok {
		return nil, false
	}
	return &value, true
}

func isJSONNull(raw json.RawMessage) bool {
	return len(raw) == 0 || strings.EqualFold(strings.TrimSpace(string(raw)), "null")
}
