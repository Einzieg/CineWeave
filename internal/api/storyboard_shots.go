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
	"github.com/Einzieg/cineweave/internal/production"
	"github.com/jackc/pgx/v5"
)

type StoryboardShot struct {
	ID                       string           `json:"id"`
	WorkflowRunID            string           `json:"workflowRunId"`
	ScriptSceneID            *string          `json:"scriptSceneId,omitempty"`
	SourceScene              *ShotSourceScene `json:"sourceScene,omitempty"`
	ShotIndex                int              `json:"shotIndex"`
	ShotNo                   int              `json:"shotNo"`
	DurationSeconds          *float64         `json:"durationSeconds,omitempty"`
	Visual                   string           `json:"visual,omitempty"`
	Camera                   string           `json:"camera,omitempty"`
	Motion                   string           `json:"motion,omitempty"`
	Mood                     string           `json:"mood,omitempty"`
	ImagePrompt              string           `json:"imagePrompt,omitempty"`
	VideoPrompt              string           `json:"videoPrompt,omitempty"`
	ImageArtifactID          *string          `json:"imageArtifactId,omitempty"`
	ImageMediaFileID         *string          `json:"imageMediaFileId,omitempty"`
	ImageStorageKey          *string          `json:"imageStorageKey,omitempty"`
	ImagePreviewURL          *string          `json:"imagePreviewUrl,omitempty"`
	VideoArtifactID          *string          `json:"videoArtifactId,omitempty"`
	VideoMediaFileID         *string          `json:"videoMediaFileId,omitempty"`
	VideoStorageKey          *string          `json:"videoStorageKey,omitempty"`
	VideoPreviewURL          *string          `json:"videoPreviewUrl,omitempty"`
	VideoProviderAsyncTaskID *string          `json:"providerAsyncTaskId,omitempty"`
	VideoExternalTaskID      *string          `json:"externalTaskId,omitempty"`
	Status                   string           `json:"status"`
	ReviewStatus             string           `json:"reviewStatus"`
	ManualOverride           bool             `json:"manualOverride"`
	StaleState               string           `json:"staleState"`
	EditedBy                 *string          `json:"editedBy,omitempty"`
	EditedAt                 *time.Time       `json:"editedAt,omitempty"`

	imageArtifactStorageKey *string
	imageArtifactMimeType   *string
	videoArtifactStorageKey *string
	videoArtifactMimeType   *string
}

type ShotSourceScene struct {
	ID         string          `json:"id"`
	SceneNo    int             `json:"sceneNo"`
	Title      string          `json:"title"`
	Location   string          `json:"location,omitempty"`
	Characters json.RawMessage `json:"characters"`
}

type StoryboardShotRequirementDetail struct {
	ShotAssetRequirement
	DerivedPreviewURL *string `json:"derivedPreviewUrl,omitempty"`
}

type StoryboardShotDetail struct {
	Shot            StoryboardShot                    `json:"shot"`
	ScriptScene     *ShotSourceScene                  `json:"scriptScene,omitempty"`
	Requirements    []StoryboardShotRequirementDetail `json:"requirements"`
	ImageArtifact   *Artifact                         `json:"imageArtifact,omitempty"`
	ImagePreviewURL *string                           `json:"imagePreviewUrl,omitempty"`
	VideoArtifact   *Artifact                         `json:"videoArtifact,omitempty"`
	VideoPreviewURL *string                           `json:"videoPreviewUrl,omitempty"`
}

func (s *Server) listWorkflowRunShots(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	run, err := scanWorkflowRun(s.db.QueryRow(r.Context(), `
		SELECT id, organization_id, project_id, template_id, temporal_workflow_id, status, input, output, error_code, error_message, created_by, created_at, started_at, completed_at, cancelled_at
		FROM workflow_runs
		WHERE id = $1
	`, r.PathValue("workflowRunId")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionWorkflowRead, authz.Resource{ProjectID: run.ProjectID}) {
		return
	}
	includePreviewURL := strings.EqualFold(r.URL.Query().Get("includePreviewUrl"), "true")
	previewExpires := previewURLExpiryFromRequest(r)
	if includePreviewURL && s.storage == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "object storage is not configured", nil, true)
		return
	}
	rows, err := s.db.Query(r.Context(), `
		`+storyboardShotSelectSQL(`
		WHERE s.workflow_run_id = $1
		  AND s.deleted_at IS NULL
		ORDER BY s.shot_index ASC
	`), run.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]StoryboardShot, 0)
	for rows.Next() {
		item, err := scanStoryboardShot(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		if includePreviewURL {
			if err := s.attachShotPreviewURLs(r, &item, previewExpires); err != nil {
				s.writeError(w, r, err)
				return
			}
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createStoryboardShot(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{authz.PermissionStoryboardGenerate, authz.PermissionProjectWrite})
	if !ok {
		return
	}
	var req struct {
		WorkflowRunID   string   `json:"workflowRunId"`
		ScriptSceneID   string   `json:"scriptSceneId"`
		ShotNo          *int     `json:"shotNo"`
		ShotIndex       *int     `json:"shotIndex"`
		DurationSeconds *float64 `json:"durationSeconds"`
		Visual          string   `json:"visual"`
		Camera          string   `json:"camera"`
		Motion          string   `json:"motion"`
		Mood            string   `json:"mood"`
		ImagePrompt     string   `json:"imagePrompt"`
		VideoPrompt     string   `json:"videoPrompt"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.ShotIndex != nil && *req.ShotIndex < 0 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "shotIndex must be greater than or equal to zero", nil, false)
		return
	}
	if req.ShotNo != nil && *req.ShotNo <= 0 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "shotNo must be greater than zero", nil, false)
		return
	}
	if req.DurationSeconds != nil && *req.DurationSeconds <= 0 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "durationSeconds must be greater than zero", nil, false)
		return
	}
	workflowRunID, err := s.storyboardWorkflowRunForCreate(r, project, strings.TrimSpace(req.WorkflowRunID), principal.UserID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if strings.TrimSpace(req.ScriptSceneID) != "" {
		if _, err := s.scriptScene(r, project.ID, strings.TrimSpace(req.ScriptSceneID)); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	shotIndex, shotNo, err := s.nextStoryboardShotPosition(r, project.ID, workflowRunID, req.ShotIndex, req.ShotNo)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var duration any
	if req.DurationSeconds != nil {
		duration = *req.DurationSeconds
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var shotID string
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO storyboard_shots(
			organization_id, project_id, workflow_run_id, script_scene_id, shot_index, shot_no,
			duration_seconds, visual, camera, motion, mood, image_prompt, video_prompt,
			status, review_status, manual_override, stale_state, edited_by, edited_at, metadata
		)
		VALUES ($1, $2, $3, NULLIF($4, '')::uuid, $5, $6, $7, NULLIF($8, ''), NULLIF($9, ''), NULLIF($10, ''), NULLIF($11, ''),
		        NULLIF($12, ''), NULLIF($13, ''), 'pending', 'pending', true, 'needs_regeneration', $14, now(), '{}')
		RETURNING id::text
	`, project.OrganizationID, project.ID, workflowRunID, strings.TrimSpace(req.ScriptSceneID), shotIndex, shotNo, duration,
		strings.TrimSpace(req.Visual), strings.TrimSpace(req.Camera), strings.TrimSpace(req.Motion), strings.TrimSpace(req.Mood),
		strings.TrimSpace(req.ImagePrompt), strings.TrimSpace(req.VideoPrompt), principal.UserID).Scan(&shotID); err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := scanStoryboardShot(tx.QueryRow(r.Context(), storyboardShotSelectSQL(`
		WHERE s.project_id = $1 AND s.id = $2 AND s.deleted_at IS NULL
	`), project.ID, shotID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := production.MarkFinalVideoStale(r.Context(), tx, project.ID, workflowRunID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "storyboard.shot.created", "storyboard_shot", item.ID, mustRawJSON(map[string]any{
		"shotId":        item.ID,
		"workflowRunId": workflowRunID,
		"scriptSceneId": item.ScriptSceneID,
		"shotNo":        item.ShotNo,
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) deleteStoryboardShot(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{authz.PermissionStoryboardGenerate, authz.PermissionProjectWrite})
	if !ok {
		return
	}
	current, err := s.storyboardShotByID(r, project.ID, r.PathValue("shotId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `
		UPDATE storyboard_shots
		SET deleted_at = now(), updated_at = now()
		WHERE project_id = $1 AND id = $2 AND deleted_at IS NULL
	`, project.ID, current.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		s.writeError(w, r, pgx.ErrNoRows)
		return
	}
	if err := production.MarkFinalVideoStale(r.Context(), tx, project.ID, current.WorkflowRunID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "storyboard.shot.deleted", "storyboard_shot", current.ID, mustRawJSON(map[string]any{
		"shotId":        current.ID,
		"workflowRunId": current.WorkflowRunID,
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "shotId": current.ID}, nil)
}

func (s *Server) reorderStoryboardShots(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{authz.PermissionStoryboardGenerate, authz.PermissionProjectWrite})
	if !ok {
		return
	}
	var req struct {
		Items []struct {
			ShotID    string `json:"shotId"`
			ShotIndex int    `json:"shotIndex"`
			ShotNo    int    `json:"shotNo"`
		} `json:"items"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Items) == 0 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "items are required", nil, false)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	workflowRunIDs := map[string]bool{}
	for index, item := range req.Items {
		shotID := strings.TrimSpace(item.ShotID)
		if shotID == "" || item.ShotIndex < 0 || item.ShotNo <= 0 {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "shotId, non-negative shotIndex, and positive shotNo are required", nil, false)
			return
		}
		var workflowRunID string
		if err := tx.QueryRow(r.Context(), `
			UPDATE storyboard_shots
			SET shot_index = $3,
			    manual_override = true,
			    stale_state = 'needs_regeneration',
			    updated_at = now()
			WHERE project_id = $1 AND id = $2 AND deleted_at IS NULL
			RETURNING COALESCE(workflow_run_id::text, '')
		`, project.ID, shotID, -(index + 1)).Scan(&workflowRunID); err != nil {
			s.writeError(w, r, err)
			return
		}
		if strings.TrimSpace(workflowRunID) != "" {
			workflowRunIDs[workflowRunID] = true
		}
	}
	for _, item := range req.Items {
		if _, err := tx.Exec(r.Context(), `
			UPDATE storyboard_shots
			SET shot_index = $3,
			    shot_no = $4,
			    updated_at = now()
			WHERE project_id = $1 AND id = $2 AND deleted_at IS NULL
		`, project.ID, strings.TrimSpace(item.ShotID), item.ShotIndex, item.ShotNo); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	for workflowRunID := range workflowRunIDs {
		if err := production.MarkFinalVideoStale(r.Context(), tx, project.ID, workflowRunID); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "storyboard.shots.reordered", "project", project.ID, mustRawJSON(map[string]any{
		"items": req.Items,
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": req.Items}, nil)
}

func (s *Server) getStoryboardShotDetail(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	shot, err := s.storyboardShotByID(r, project.ID, r.PathValue("shotId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if s.storage != nil {
		if err := s.attachShotPreviewURLs(r, &shot, previewURLExpiryFromRequest(r)); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	requirements, err := s.storyboardShotRequirementDetails(r, project.ID, shot.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	imageArtifact, imagePreview := s.optionalArtifactWithPreview(r, stringValue(shot.ImageArtifactID))
	videoArtifact, videoPreview := s.optionalArtifactWithPreview(r, stringValue(shot.VideoArtifactID))
	detail := StoryboardShotDetail{
		Shot:            shot,
		ScriptScene:     shot.SourceScene,
		Requirements:    requirements,
		ImageArtifact:   imageArtifact,
		ImagePreviewURL: firstStringPtr(shot.ImagePreviewURL, imagePreview),
		VideoArtifact:   videoArtifact,
		VideoPreviewURL: firstStringPtr(shot.VideoPreviewURL, videoPreview),
	}
	httpx.WriteJSON(w, r, http.StatusOK, detail, nil)
}

func storyboardShotSelectSQL(where string) string {
	return `
		SELECT
			s.id,
			COALESCE(s.workflow_run_id::text, ''),
			s.shot_index,
			COALESCE(s.shot_no, s.shot_index + 1),
			s.duration_seconds,
			COALESCE(s.visual, ''),
			COALESCE(s.camera, ''),
			COALESCE(s.motion, ''),
			COALESCE(s.mood, ''),
			COALESCE(s.image_prompt, ''),
			COALESCE(s.video_prompt, ''),
			s.image_artifact_id,
			s.image_media_file_id,
			COALESCE(s.image_storage_key, ia.storage_key),
			ia.storage_key,
			ia.mime_type,
			s.video_artifact_id,
			s.video_media_file_id,
			COALESCE(s.video_storage_key, va.storage_key),
			va.storage_key,
			va.mime_type,
			s.video_provider_async_task_id,
			s.video_external_task_id,
			COALESCE(s.status, 'pending'),
			COALESCE(s.review_status, 'pending'),
			COALESCE(s.manual_override, false),
			COALESCE(s.stale_state, 'fresh'),
			s.edited_by,
			s.edited_at,
			s.script_scene_id::text,
			sc.id::text,
			COALESCE(sc.scene_no, 0),
			COALESCE(sc.title, ''),
			COALESCE(sc.location, ''),
			COALESCE(sc.characters, '[]'::jsonb)
		FROM storyboard_shots s
		LEFT JOIN artifacts ia ON ia.id = s.image_artifact_id
		LEFT JOIN artifacts va ON va.id = s.video_artifact_id
		LEFT JOIN script_scenes sc ON sc.id = s.script_scene_id
	` + where
}

func scanStoryboardShot(row pgx.Row) (StoryboardShot, error) {
	var item StoryboardShot
	var duration sql.NullFloat64
	var imageArtifactID, imageMediaFileID, imageStorageKey, imageArtifactStorageKey, imageArtifactMimeType sql.NullString
	var videoArtifactID, videoMediaFileID, videoStorageKey, videoArtifactStorageKey, videoArtifactMimeType sql.NullString
	var providerAsyncTaskID, externalTaskID, editedBy sql.NullString
	var scriptSceneID, sourceSceneID, sourceSceneTitle, sourceSceneLocation sql.NullString
	var sourceSceneNo sql.NullInt64
	var sourceSceneCharacters []byte
	var editedAt sql.NullTime
	err := row.Scan(
		&item.ID,
		&item.WorkflowRunID,
		&item.ShotIndex,
		&item.ShotNo,
		&duration,
		&item.Visual,
		&item.Camera,
		&item.Motion,
		&item.Mood,
		&item.ImagePrompt,
		&item.VideoPrompt,
		&imageArtifactID,
		&imageMediaFileID,
		&imageStorageKey,
		&imageArtifactStorageKey,
		&imageArtifactMimeType,
		&videoArtifactID,
		&videoMediaFileID,
		&videoStorageKey,
		&videoArtifactStorageKey,
		&videoArtifactMimeType,
		&providerAsyncTaskID,
		&externalTaskID,
		&item.Status,
		&item.ReviewStatus,
		&item.ManualOverride,
		&item.StaleState,
		&editedBy,
		&editedAt,
		&scriptSceneID,
		&sourceSceneID,
		&sourceSceneNo,
		&sourceSceneTitle,
		&sourceSceneLocation,
		&sourceSceneCharacters,
	)
	if duration.Valid {
		item.DurationSeconds = &duration.Float64
	}
	item.ImageArtifactID = stringPtrFromNull(imageArtifactID)
	item.ImageMediaFileID = stringPtrFromNull(imageMediaFileID)
	item.ImageStorageKey = stringPtrFromNull(imageStorageKey)
	item.imageArtifactStorageKey = stringPtrFromNull(imageArtifactStorageKey)
	item.imageArtifactMimeType = stringPtrFromNull(imageArtifactMimeType)
	item.VideoArtifactID = stringPtrFromNull(videoArtifactID)
	item.VideoMediaFileID = stringPtrFromNull(videoMediaFileID)
	item.VideoStorageKey = stringPtrFromNull(videoStorageKey)
	item.videoArtifactStorageKey = stringPtrFromNull(videoArtifactStorageKey)
	item.videoArtifactMimeType = stringPtrFromNull(videoArtifactMimeType)
	item.VideoProviderAsyncTaskID = stringPtrFromNull(providerAsyncTaskID)
	item.VideoExternalTaskID = stringPtrFromNull(externalTaskID)
	item.EditedBy = stringPtrFromNull(editedBy)
	item.ScriptSceneID = stringPtrFromNull(scriptSceneID)
	if sourceSceneID.Valid && strings.TrimSpace(sourceSceneID.String) != "" {
		item.SourceScene = &ShotSourceScene{
			ID:         sourceSceneID.String,
			SceneNo:    int(sourceSceneNo.Int64),
			Title:      sourceSceneTitle.String,
			Location:   sourceSceneLocation.String,
			Characters: rawOrDefaultBytes(sourceSceneCharacters, "[]"),
		}
	}
	if editedAt.Valid {
		item.EditedAt = &editedAt.Time
	}
	return item, err
}

func (s *Server) attachShotPreviewURLs(r *http.Request, item *StoryboardShot, expires time.Duration) error {
	if item.imageArtifactStorageKey != nil && item.imageArtifactMimeType != nil && canPreviewMimeType(*item.imageArtifactMimeType) && strings.TrimSpace(*item.imageArtifactStorageKey) != "" {
		presigned, err := s.storage.PresignGetObject(r.Context(), *item.imageArtifactStorageKey, expires)
		if err != nil {
			return err
		}
		item.ImagePreviewURL = &presigned.URL
	}
	if item.videoArtifactStorageKey != nil && item.videoArtifactMimeType != nil && canPreviewMimeType(*item.videoArtifactMimeType) && strings.TrimSpace(*item.videoArtifactStorageKey) != "" {
		presigned, err := s.storage.PresignGetObject(r.Context(), *item.videoArtifactStorageKey, expires)
		if err != nil {
			return err
		}
		item.VideoPreviewURL = &presigned.URL
	}
	return nil
}

func (s *Server) storyboardWorkflowRunForCreate(r *http.Request, project Project, requestedWorkflowRunID, userID string) (string, error) {
	if requestedWorkflowRunID != "" {
		var id string
		if err := s.db.QueryRow(r.Context(), `
			SELECT id::text
			FROM workflow_runs
			WHERE id = $1 AND project_id = $2
		`, requestedWorkflowRunID, project.ID).Scan(&id); err != nil {
			return "", err
		}
		return id, nil
	}
	var id string
	err := s.db.QueryRow(r.Context(), `
		SELECT id::text
		FROM workflow_runs
		WHERE project_id = $1
		  AND input->>'workflowType' IN ('script_to_storyboard', 'script_to_video', 'full_production')
		ORDER BY created_at DESC
		LIMIT 1
	`, project.ID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != pgx.ErrNoRows {
		return "", err
	}
	if err := s.db.QueryRow(r.Context(), `
		WITH new_run AS (SELECT gen_random_uuid() AS id)
		INSERT INTO workflow_runs(id, organization_id, project_id, temporal_workflow_id, status, input, output, created_by)
		SELECT id, $1, $2, 'manual-storyboard-' || id::text, 'succeeded',
		       '{"workflowType":"script_to_storyboard","input":{"manual":true}}'::jsonb, '{}', $3
		FROM new_run
		RETURNING id::text
	`, project.OrganizationID, project.ID, userID).Scan(&id); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Server) nextStoryboardShotPosition(r *http.Request, projectID, workflowRunID string, requestedShotIndex, requestedShotNo *int) (int, int, error) {
	if requestedShotIndex != nil && requestedShotNo != nil {
		return *requestedShotIndex, *requestedShotNo, nil
	}
	var maxIndex, maxNo sql.NullInt64
	if err := s.db.QueryRow(r.Context(), `
		SELECT max(shot_index), max(COALESCE(shot_no, shot_index + 1))
		FROM storyboard_shots
		WHERE project_id = $1 AND workflow_run_id = $2 AND deleted_at IS NULL
	`, projectID, workflowRunID).Scan(&maxIndex, &maxNo); err != nil {
		return 0, 0, err
	}
	shotIndex := 0
	if maxIndex.Valid {
		shotIndex = int(maxIndex.Int64) + 1
	}
	if requestedShotIndex != nil {
		shotIndex = *requestedShotIndex
	}
	shotNo := shotIndex + 1
	if maxNo.Valid {
		shotNo = int(maxNo.Int64) + 1
	}
	if requestedShotNo != nil {
		shotNo = *requestedShotNo
	}
	return shotIndex, shotNo, nil
}

func (s *Server) storyboardShotRequirementDetails(r *http.Request, projectID, shotID string) ([]StoryboardShotRequirementDetail, error) {
	rows, err := s.db.Query(r.Context(), shotAssetRequirementSelectSQL(`
		WHERE r.project_id = $1 AND r.storyboard_shot_id = $2
		ORDER BY r.created_at ASC
	`), projectID, shotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]StoryboardShotRequirementDetail, 0)
	for rows.Next() {
		requirement, err := scanShotAssetRequirement(rows)
		if err != nil {
			return nil, err
		}
		detail := StoryboardShotRequirementDetail{ShotAssetRequirement: requirement}
		if asset, err := s.canonicalAsset(r, projectID, requirement.AssetID); err == nil {
			assets := []CanonicalAsset{asset}
			if err := s.attachCanonicalAssetReferences(r, projectID, assets, s.storage != nil); err != nil {
				return nil, err
			}
			detail.Asset = &assets[0]
		} else if err != pgx.ErrNoRows {
			return nil, err
		}
		if s.storage != nil {
			if preview := s.previewURLForStorageKey(r, stringValue(requirement.DerivedStorageKey)); preview != nil {
				detail.DerivedPreviewURL = preview
			} else if artifact, preview := s.optionalArtifactWithPreview(r, stringValue(requirement.DerivedArtifactID)); artifact != nil && preview != nil {
				detail.DerivedPreviewURL = preview
			}
		}
		items = append(items, detail)
	}
	return items, rows.Err()
}

func (s *Server) optionalArtifactWithPreview(r *http.Request, artifactID string) (*Artifact, *string) {
	if strings.TrimSpace(artifactID) == "" {
		return nil, nil
	}
	artifact, err := s.artifact(r, artifactID)
	if err != nil {
		return nil, nil
	}
	var preview *string
	if s.storage != nil && artifactCanPreview(artifact) && artifact.StorageKey != nil {
		preview = s.previewURLForStorageKey(r, *artifact.StorageKey)
	}
	if preview != nil {
		artifact.PreviewURL = preview
	}
	return &artifact, preview
}

func (s *Server) previewURLForStorageKey(r *http.Request, storageKey string) *string {
	if s.storage == nil || strings.TrimSpace(storageKey) == "" {
		return nil
	}
	presigned, err := s.storage.PresignGetObject(r.Context(), storageKey, previewURLExpiryFromRequest(r))
	if err != nil {
		return nil
	}
	return &presigned.URL
}

func firstStringPtr(values ...*string) *string {
	for _, value := range values {
		if value != nil && strings.TrimSpace(*value) != "" {
			return value
		}
	}
	return nil
}
