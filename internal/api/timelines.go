package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/jackc/pgx/v5"
)

type ProjectTimeline struct {
	ID             string          `json:"id"`
	OrganizationID string          `json:"organizationId"`
	ProjectID      string          `json:"projectId"`
	WorkflowRunID  *string         `json:"workflowRunId,omitempty"`
	Title          string          `json:"title"`
	Status         string          `json:"status"`
	AspectRatio    string          `json:"aspectRatio"`
	Resolution     string          `json:"resolution"`
	Metadata       json.RawMessage `json:"metadata"`
	CreatedBy      *string         `json:"createdBy,omitempty"`
	EditedBy       *string         `json:"editedBy,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	EditedAt       *time.Time      `json:"editedAt,omitempty"`
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
	SourceDurationSeconds *float64        `json:"sourceDurationSeconds,omitempty"`
	TrimStartSeconds      float64         `json:"trimStartSeconds"`
	TrimEndSeconds        *float64        `json:"trimEndSeconds,omitempty"`
	TargetDurationSeconds *float64        `json:"targetDurationSeconds,omitempty"`
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
	ID              string          `json:"id"`
	OrganizationID  string          `json:"organizationId"`
	ProjectID       string          `json:"projectId"`
	TimelineID      string          `json:"timelineId"`
	WorkflowRunID   *string         `json:"workflowRunId,omitempty"`
	Version         int             `json:"version"`
	Title           string          `json:"title"`
	Status          string          `json:"status"`
	ArtifactID      *string         `json:"artifactId,omitempty"`
	MediaFileID     *string         `json:"mediaFileId,omitempty"`
	StorageKey      *string         `json:"storageKey,omitempty"`
	DurationSeconds *float64        `json:"durationSeconds,omitempty"`
	Resolution      string          `json:"resolution"`
	AspectRatio     string          `json:"aspectRatio"`
	ComposeSettings json.RawMessage `json:"composeSettings"`
	Metadata        json.RawMessage `json:"metadata"`
	CreatedBy       *string         `json:"createdBy,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	PreviewURL      *string         `json:"previewUrl,omitempty"`
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
		       metadata, created_by::text, edited_by::text, created_at, updated_at, edited_at
		FROM project_timelines
		WHERE project_id = $1
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

	var item ProjectTimeline
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO project_timelines(organization_id, project_id, title, status, aspect_ratio, resolution, metadata, created_by)
		VALUES ($1, $2, $3, 'draft', $4, $5, '{}', $6)
		RETURNING id, organization_id, project_id, workflow_run_id::text, title, status, aspect_ratio, resolution,
		          metadata, created_by::text, edited_by::text, created_at, updated_at, edited_at
	`, project.OrganizationID, project.ID, title, aspectRatio, resolution, principal.UserID).Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.WorkflowRunID, &item.Title, &item.Status, &item.AspectRatio, &item.Resolution,
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
	item, err := scanProjectTimeline(s.db.QueryRow(r.Context(), `
		UPDATE project_timelines
		SET title = COALESCE($3, title),
		    status = COALESCE($4, status),
		    aspect_ratio = COALESCE($5, aspect_ratio),
		    resolution = COALESCE($6, resolution),
		    edited_by = $7,
		    edited_at = now()
		WHERE project_id = $1 AND id = $2
		RETURNING id, organization_id, project_id, workflow_run_id::text, title, status, aspect_ratio, resolution,
		          metadata, created_by::text, edited_by::text, created_at, updated_at, edited_at
	`, project.ID, r.PathValue("timelineId"), normalizedOptionalString(req.Title), normalizedOptionalString(req.Status),
		normalizedOptionalString(req.AspectRatio), normalizedOptionalString(req.Resolution), principal.UserID))
	if err != nil {
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
	tag, err := s.db.Exec(r.Context(), `DELETE FROM project_timelines WHERE project_id = $1 AND id = $2`, project.ID, r.PathValue("timelineId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		s.writeError(w, r, pgx.ErrNoRows)
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
		StoryboardShotID      string   `json:"storyboardShotId"`
		VideoArtifactID       string   `json:"videoArtifactId"`
		VideoMediaFileID      string   `json:"videoMediaFileId"`
		ClipIndex             *int     `json:"clipIndex"`
		Title                 string   `json:"title"`
		Enabled               *bool    `json:"enabled"`
		SourceStorageKey      string   `json:"sourceStorageKey"`
		SourceDurationSeconds *float64 `json:"sourceDurationSeconds"`
		TrimStartSeconds      *float64 `json:"trimStartSeconds"`
		TrimEndSeconds        *float64 `json:"trimEndSeconds"`
		TargetDurationSeconds *float64 `json:"targetDurationSeconds"`
		Notes                 string   `json:"notes"`
	}
	if !decode(w, r, &req) {
		return
	}
	clipIndex := 0
	if req.ClipIndex != nil {
		clipIndex = *req.ClipIndex
	} else if err := s.db.QueryRow(r.Context(), `SELECT COALESCE(MAX(clip_index), -1) + 1 FROM timeline_clips WHERE timeline_id = $1`, timeline.ID).Scan(&clipIndex); err != nil {
		s.writeError(w, r, err)
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
	sourceDuration := req.SourceDurationSeconds
	if strings.TrimSpace(req.StoryboardShotID) != "" {
		var shotTitle sql.NullString
		var duration sql.NullFloat64
		if err := s.db.QueryRow(r.Context(), `
			SELECT COALESCE(video_artifact_id::text, ''), COALESCE(video_media_file_id::text, ''),
			       COALESCE(video_storage_key, mf.storage_key, va.storage_key, ''),
			       COALESCE(mf.duration_seconds, duration_seconds, 0)::float8,
			       COALESCE(title, visual, '')
			FROM storyboard_shots s
			LEFT JOIN media_files mf ON mf.id = s.video_media_file_id
			LEFT JOIN artifacts va ON va.id = s.video_artifact_id
			WHERE s.project_id = $1 AND s.id = $2 AND s.deleted_at IS NULL
		`, project.ID, req.StoryboardShotID).Scan(&videoArtifactID, &videoMediaFileID, &sourceStorageKey, &duration, &shotTitle); err != nil {
			s.writeError(w, r, err)
			return
		}
		if sourceDuration == nil && duration.Valid {
			value := duration.Float64
			sourceDuration = &value
		}
		if title == "" && shotTitle.Valid {
			title = shotTitle.String
		}
	}
	if title == "" {
		title = "镜头片段"
	}
	trimStart := 0.0
	if req.TrimStartSeconds != nil && *req.TrimStartSeconds > 0 {
		trimStart = *req.TrimStartSeconds
	}
	item, err := scanTimelineClip(s.db.QueryRow(r.Context(), timelineClipReturningSQL(`
		INSERT INTO timeline_clips(
			organization_id, project_id, timeline_id, storyboard_shot_id, video_artifact_id, video_media_file_id,
			clip_index, title, enabled, source_storage_key, source_duration_seconds,
			trim_start_seconds, trim_end_seconds, target_duration_seconds, notes, metadata
		)
		VALUES ($1, $2, $3, NULLIF($4, '')::uuid, NULLIF($5, '')::uuid, NULLIF($6, '')::uuid,
		        $7, $8, $9, NULLIF($10, ''), $11, $12, $13, $14, NULLIF($15, ''), '{}')
		RETURNING
	`), project.OrganizationID, project.ID, timeline.ID, strings.TrimSpace(req.StoryboardShotID), videoArtifactID, videoMediaFileID,
		clipIndex, title, enabled, sourceStorageKey, nullableFloatPtr(sourceDuration), trimStart, nullableFloatPtr(req.TrimEndSeconds),
		nullableFloatPtr(req.TargetDurationSeconds), strings.TrimSpace(req.Notes)))
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
	if raw, ok := patch["trimStartSeconds"]; ok {
		if value, ok := decodePatchFloat(w, r, raw, "trimStartSeconds"); ok {
			if value < 0 {
				value = 0
			}
			current.TrimStartSeconds = value
		} else {
			return
		}
	}
	if raw, ok := patch["trimEndSeconds"]; ok {
		value, ok := decodePatchNullableFloat(w, r, raw, "trimEndSeconds")
		if !ok {
			return
		}
		current.TrimEndSeconds = value
	}
	if raw, ok := patch["targetDurationSeconds"]; ok {
		value, ok := decodePatchNullableFloat(w, r, raw, "targetDurationSeconds")
		if !ok {
			return
		}
		current.TargetDurationSeconds = value
	}
	if raw, ok := patch["notes"]; ok {
		value, ok := decodePatchNullableString(w, r, raw, "notes")
		if !ok {
			return
		}
		current.Notes = value
	}
	item, err := scanTimelineClip(s.db.QueryRow(r.Context(), timelineClipReturningSQL(`
		UPDATE timeline_clips
		SET title = $4,
		    enabled = $5,
		    trim_start_seconds = $6,
		    trim_end_seconds = $7,
		    target_duration_seconds = $8,
		    notes = $9
		WHERE project_id = $1 AND timeline_id = $2 AND id = $3
		RETURNING
	`), project.ID, r.PathValue("timelineId"), r.PathValue("clipId"), current.Title, current.Enabled,
		current.TrimStartSeconds, nullableFloatPtr(current.TrimEndSeconds), nullableFloatPtr(current.TargetDurationSeconds), nullableStringPtr(current.Notes)))
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
	tag, err := s.db.Exec(r.Context(), `
		DELETE FROM timeline_clips
		WHERE project_id = $1 AND timeline_id = $2 AND id = $3
	`, project.ID, r.PathValue("timelineId"), r.PathValue("clipId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		s.writeError(w, r, pgx.ErrNoRows)
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
	if _, err := tx.Exec(r.Context(), `SET CONSTRAINTS timeline_clips_timeline_index_unique DEFERRED`); err != nil {
		s.writeError(w, r, err)
		return
	}
	for _, item := range req.Items {
		tag, err := tx.Exec(r.Context(), `
			UPDATE timeline_clips
			SET clip_index = $4
			WHERE project_id = $1 AND timeline_id = $2 AND id = $3
		`, project.ID, r.PathValue("timelineId"), item.ClipID, item.ClipIndex)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		if tag.RowsAffected() == 0 {
			s.writeError(w, r, pgx.ErrNoRows)
			return
		}
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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `UPDATE final_video_versions SET status = 'ready' WHERE project_id = $1 AND status = 'active' AND id <> $2`, project.ID, r.PathValue("versionId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	tag, err := tx.Exec(r.Context(), `UPDATE final_video_versions SET status = 'active' WHERE project_id = $1 AND id = $2`, project.ID, r.PathValue("versionId"))
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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `UPDATE projects SET active_final_video_version_id = NULL WHERE id = $1 AND active_final_video_version_id = $2`, project.ID, r.PathValue("versionId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	tag, err := tx.Exec(r.Context(), `DELETE FROM final_video_versions WHERE project_id = $1 AND id = $2`, project.ID, r.PathValue("versionId"))
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
	rows, err := tx.Query(r.Context(), `
		SELECT s.id::text, COALESCE(s.video_artifact_id::text, ''), COALESCE(s.video_media_file_id::text, ''),
		       COALESCE(s.video_storage_key, mf.storage_key, va.storage_key, ''),
		       COALESCE(mf.duration_seconds, s.duration_seconds, 0)::float8,
		       COALESCE(s.title, s.visual, '')
		FROM storyboard_shots s
		LEFT JOIN media_files mf ON mf.id = s.video_media_file_id
		LEFT JOIN artifacts va ON va.id = s.video_artifact_id
		WHERE s.project_id = $1
		  AND s.deleted_at IS NULL
		  AND (COALESCE(s.video_status, '') = 'succeeded' OR COALESCE(s.status, '') = 'video_succeeded')
		  AND COALESCE(s.video_storage_key, mf.storage_key, va.storage_key, '') <> ''
		ORDER BY s.shot_index ASC
	`, project.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var shotID, artifactID, mediaFileID, storageKey, title string
		var duration sql.NullFloat64
		if err := rows.Scan(&shotID, &artifactID, &mediaFileID, &storageKey, &duration, &title); err != nil {
			return err
		}
		if strings.TrimSpace(title) == "" {
			title = "镜头片段"
		}
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO timeline_clips(
				organization_id, project_id, timeline_id, storyboard_shot_id, video_artifact_id, video_media_file_id,
				clip_index, title, enabled, source_storage_key, source_duration_seconds, metadata
			)
			VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid, NULLIF($6, '')::uuid, $7, $8, true, $9, $10, '{}')
		`, project.OrganizationID, project.ID, timelineID, shotID, artifactID, mediaFileID, index, title, storageKey, nullableFloatFromNull(duration)); err != nil {
			return err
		}
		index++
	}
	return rows.Err()
}

func (s *Server) timelineByID(r *http.Request, projectID, timelineID string) (ProjectTimeline, error) {
	return scanProjectTimeline(s.db.QueryRow(r.Context(), `
		SELECT id, organization_id, project_id, workflow_run_id::text, title, status, aspect_ratio, resolution,
		       metadata, created_by::text, edited_by::text, created_at, updated_at, edited_at
		FROM project_timelines
		WHERE project_id = $1 AND id = $2
	`, projectID, timelineID))
}

func scanProjectTimeline(row rowScan) (ProjectTimeline, error) {
	var item ProjectTimeline
	var workflowRunID, createdBy, editedBy sql.NullString
	var editedAt sql.NullTime
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &workflowRunID, &item.Title, &item.Status, &item.AspectRatio, &item.Resolution,
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

func (s *Server) timelineClips(r *http.Request, projectID, timelineID string) ([]TimelineClip, error) {
	rows, err := s.db.Query(r.Context(), `
		SELECT `+timelineClipColumns()+`
		FROM timeline_clips
		WHERE project_id = $1 AND timeline_id = $2
		ORDER BY clip_index ASC
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
		SELECT `+timelineClipColumns()+`
		FROM timeline_clips
		WHERE project_id = $1 AND timeline_id = $2 AND id = $3
	`, projectID, timelineID, clipID))
}

func timelineClipReturningSQL(prefix string) string {
	return prefix + timelineClipColumns()
}

func timelineClipColumns() string {
	return `
		id, organization_id, project_id, timeline_id, storyboard_shot_id::text, video_artifact_id::text,
		video_media_file_id::text, clip_index, title, enabled, source_storage_key,
		source_duration_seconds::float8, trim_start_seconds::float8, trim_end_seconds::float8,
		target_duration_seconds::float8, notes, metadata, created_at, updated_at
	`
}

func scanTimelineClip(row rowScan) (TimelineClip, error) {
	var item TimelineClip
	var storyboardShotID, artifactID, mediaFileID, storageKey, notes sql.NullString
	var sourceDuration, trimEnd, targetDuration sql.NullFloat64
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.TimelineID, &storyboardShotID, &artifactID,
		&mediaFileID, &item.ClipIndex, &item.Title, &item.Enabled, &storageKey,
		&sourceDuration, &item.TrimStartSeconds, &trimEnd, &targetDuration, &notes, &item.Metadata, &item.CreatedAt, &item.UpdatedAt,
	)
	item.StoryboardShotID = stringPtrFromNull(storyboardShotID)
	item.VideoArtifactID = stringPtrFromNull(artifactID)
	item.VideoMediaFileID = stringPtrFromNull(mediaFileID)
	item.SourceStorageKey = stringPtrFromNull(storageKey)
	item.Notes = stringPtrFromNull(notes)
	if sourceDuration.Valid {
		item.SourceDurationSeconds = &sourceDuration.Float64
	}
	if trimEnd.Valid {
		item.TrimEndSeconds = &trimEnd.Float64
	}
	if targetDuration.Valid {
		item.TargetDurationSeconds = &targetDuration.Float64
	}
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
		SELECT id, organization_id, project_id, timeline_id, workflow_run_id::text, version, title, status,
		       artifact_id::text, media_file_id::text, storage_key, duration_seconds::float8,
		       resolution, aspect_ratio, compose_settings, metadata, created_by::text, created_at
		FROM final_video_versions
		WHERE project_id = $1
	`
	args := []any{projectID}
	if strings.TrimSpace(timelineID) != "" {
		query += " AND timeline_id = $2"
		args = append(args, timelineID)
	}
	query += " ORDER BY CASE status WHEN 'active' THEN 0 WHEN 'ready' THEN 1 ELSE 2 END, version DESC, created_at DESC"
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
		SELECT id, organization_id, project_id, timeline_id, workflow_run_id::text, version, title, status,
		       artifact_id::text, media_file_id::text, storage_key, duration_seconds::float8,
		       resolution, aspect_ratio, compose_settings, metadata, created_by::text, created_at
		FROM final_video_versions
		WHERE project_id = $1 AND id = $2
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
	var duration sql.NullFloat64
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.TimelineID, &workflowRunID, &item.Version, &item.Title, &item.Status,
		&artifactID, &mediaFileID, &storageKey, &duration, &item.Resolution, &item.AspectRatio, &item.ComposeSettings,
		&item.Metadata, &createdBy, &item.CreatedAt,
	)
	item.WorkflowRunID = stringPtrFromNull(workflowRunID)
	item.ArtifactID = stringPtrFromNull(artifactID)
	item.MediaFileID = stringPtrFromNull(mediaFileID)
	item.StorageKey = stringPtrFromNull(storageKey)
	item.CreatedBy = stringPtrFromNull(createdBy)
	if duration.Valid {
		item.DurationSeconds = &duration.Float64
	}
	return item, err
}

func (s *Server) attachFinalVideoPreview(r *http.Request, item *FinalVideoVersion) {
	if item == nil || item.StorageKey == nil {
		return
	}
	item.PreviewURL = s.previewURLForStorageKey(r, *item.StorageKey)
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

func nullableFloatPtr(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableFloatFromNull(value sql.NullFloat64) any {
	if !value.Valid {
		return nil
	}
	return value.Float64
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

func decodePatchFloat(w http.ResponseWriter, r *http.Request, raw json.RawMessage, field string) (float64, bool) {
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", field+" must be a number", nil, false)
		return 0, false
	}
	return value, true
}

func decodePatchNullableFloat(w http.ResponseWriter, r *http.Request, raw json.RawMessage, field string) (*float64, bool) {
	if isJSONNull(raw) {
		return nil, true
	}
	value, ok := decodePatchFloat(w, r, raw, field)
	if !ok {
		return nil, false
	}
	return &value, true
}

func isJSONNull(raw json.RawMessage) bool {
	return len(raw) == 0 || strings.EqualFold(strings.TrimSpace(string(raw)), "null")
}
