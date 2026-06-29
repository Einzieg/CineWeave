package api

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/jackc/pgx/v5"
)

type StoryboardShot struct {
	ID                       string   `json:"id"`
	WorkflowRunID            string   `json:"workflowRunId"`
	ShotIndex                int      `json:"shotIndex"`
	ShotNo                   int      `json:"shotNo"`
	DurationSeconds          *float64 `json:"durationSeconds,omitempty"`
	Visual                   string   `json:"visual,omitempty"`
	Camera                   string   `json:"camera,omitempty"`
	Motion                   string   `json:"motion,omitempty"`
	Mood                     string   `json:"mood,omitempty"`
	ImagePrompt              string   `json:"imagePrompt,omitempty"`
	VideoPrompt              string   `json:"videoPrompt,omitempty"`
	ImageArtifactID          *string  `json:"imageArtifactId,omitempty"`
	ImageMediaFileID         *string  `json:"imageMediaFileId,omitempty"`
	ImageStorageKey          *string  `json:"imageStorageKey,omitempty"`
	ImagePreviewURL          *string  `json:"imagePreviewUrl,omitempty"`
	VideoArtifactID          *string  `json:"videoArtifactId,omitempty"`
	VideoMediaFileID         *string  `json:"videoMediaFileId,omitempty"`
	VideoStorageKey          *string  `json:"videoStorageKey,omitempty"`
	VideoPreviewURL          *string  `json:"videoPreviewUrl,omitempty"`
	VideoProviderAsyncTaskID *string  `json:"providerAsyncTaskId,omitempty"`
	VideoExternalTaskID      *string  `json:"externalTaskId,omitempty"`
	Status                   string   `json:"status"`

	imageArtifactStorageKey *string
	imageArtifactMimeType   *string
	videoArtifactStorageKey *string
	videoArtifactMimeType   *string
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
		SELECT
			s.id,
			s.workflow_run_id,
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
			COALESCE(s.status, 'pending')
		FROM storyboard_shots s
		LEFT JOIN artifacts ia ON ia.id = s.image_artifact_id
		LEFT JOIN artifacts va ON va.id = s.video_artifact_id
		WHERE s.workflow_run_id = $1
		ORDER BY s.shot_index ASC
	`, run.ID)
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

func scanStoryboardShot(row pgx.Row) (StoryboardShot, error) {
	var item StoryboardShot
	var duration sql.NullFloat64
	var imageArtifactID, imageMediaFileID, imageStorageKey, imageArtifactStorageKey, imageArtifactMimeType sql.NullString
	var videoArtifactID, videoMediaFileID, videoStorageKey, videoArtifactStorageKey, videoArtifactMimeType sql.NullString
	var providerAsyncTaskID, externalTaskID sql.NullString
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
