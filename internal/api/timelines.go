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
	"github.com/jackc/pgx/v5"
)

type ProjectTimeline struct {
	ID               string          `json:"id"`
	OrganizationID   string          `json:"organizationId"`
	ProjectID        string          `json:"projectId"`
	Revision         int64           `json:"revision"`
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
	Revision              int64           `json:"revision"`
	TimelineRevision      int64           `json:"timelineRevision"`
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
	Revision            int64           `json:"revision"`
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
		SELECT id, organization_id, project_id, revision, workflow_run_id::text, title, status, aspect_ratio, resolution,
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
	var input timelineCreateActionInput
	if !decode(w, r, &input) {
		return
	}
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "timeline.create", mustRawJSON(input), idempotencyKey(r, ""),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := decodeAgentToolResultValue[ProjectTimeline](result, "timeline")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
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
		ExpectedRevision int64   `json:"expectedRevision"`
		Title            *string `json:"title"`
		Status           *string `json:"status"`
		AspectRatio      *string `json:"aspectRatio"`
		Resolution       *string `json:"resolution"`
	}
	if !decode(w, r, &req) {
		return
	}
	input := timelineUpdateActionInput{
		TimelineID: r.PathValue("timelineId"), ExpectedRevision: req.ExpectedRevision,
		Patch: timelineUpdatePatch{Title: req.Title, Status: req.Status, AspectRatio: req.AspectRatio, Resolution: req.Resolution},
	}
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "timeline.update", mustRawJSON(input), idempotencyKey(r, ""),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := decodeAgentToolResultValue[ProjectTimeline](result, "timeline")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) deleteProjectTimeline(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	if !decode(w, r, &req) {
		return
	}
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "timeline.delete", mustRawJSON(timelineDeleteActionInput{
			TimelineID: r.PathValue("timelineId"), ExpectedRevision: req.ExpectedRevision,
		}), idempotencyKey(r, ""),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	deleted, err := decodeAgentToolResultValue[bool](result, "deleted")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"deleted": deleted, "timelineId": r.PathValue("timelineId")}, nil)
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
	var req timelineComposeActionInput
	if !decode(w, r, &req) {
		return
	}
	req.TimelineID = r.PathValue("timelineId")
	run, timeline, _, err := s.composeTimelineAction(r.Context(), principal, project, req, "")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, ComposeTimelineResponse{WorkflowRunID: run.ID, TimelineID: timeline.ID, Status: run.Status}, nil)
}

func (s *Server) projectShotVideosReady(r *http.Request, projectID string) (bool, error) {
	return s.projectShotVideosReadyContext(r.Context(), projectID)
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
	var req struct {
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	if !decode(w, r, &req) {
		return
	}
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "final_video.activate", mustRawJSON(finalVideoActivateActionInput{
			VersionID: r.PathValue("versionId"), ExpectedRevision: req.ExpectedRevision,
		}), idempotencyKey(r, ""),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := decodeAgentToolResultValue[FinalVideoVersion](result, "finalVideo")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.attachFinalVideoPreview(r, &item)
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) deleteFinalVideo(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64 `json:"expectedRevision"`
		ConfirmActive    bool  `json:"confirmActive"`
	}
	if !decode(w, r, &req) {
		return
	}
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "final_video.delete", mustRawJSON(finalVideoDeleteActionInput{
			VersionID: r.PathValue("versionId"), ExpectedRevision: req.ExpectedRevision, ConfirmActive: req.ConfirmActive,
		}), idempotencyKey(r, ""),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	deleted, err := decodeAgentToolResultValue[bool](result, "deleted")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"deleted": deleted, "versionId": r.PathValue("versionId")}, nil)
}

func (s *Server) createTimelineClipsFromStoryboard(ctx context.Context, tx pgx.Tx, project Project, timelineID string) error {
	timebase := storyboardtiming.Timebase{
		TicksPerSecond: project.TimelineTimebase,
		FPSNumerator:   int64(project.FPSNumerator),
		FPSDenominator: int64(project.FPSDenominator),
	}
	if err := timebase.Validate(); err != nil {
		return err
	}
	var productionGenerationID string
	if err := tx.QueryRow(ctx, `
		SELECT production_generation_id::text
		FROM project_timelines
		WHERE id = $1 AND project_id = $2
	`, timelineID, project.ID).Scan(&productionGenerationID); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `
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
		if _, err := tx.Exec(ctx, `
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
		SELECT id, organization_id, project_id, revision, workflow_run_id::text, title, status, aspect_ratio, resolution,
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
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.Revision, &workflowRunID, &item.Title, &item.Status, &item.AspectRatio, &item.Resolution,
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
		%[1]s.revision, %[2]s.revision,
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
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.TimelineID, &item.Revision, &item.TimelineRevision, &storyboardShotID, &artifactID,
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
		       version_row.workflow_run_id::text, version_row.version, version_row.revision, version_row.title, version_row.status,
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
		       version_row.workflow_run_id::text, version_row.version, version_row.revision, version_row.title, version_row.status,
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
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.TimelineID, &workflowRunID, &item.Version, &item.Revision, &item.Title, &item.Status,
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

func isJSONNull(raw json.RawMessage) bool {
	return len(raw) == 0 || strings.EqualFold(strings.TrimSpace(string(raw)), "null")
}
