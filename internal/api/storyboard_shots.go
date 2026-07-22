package api

import (
	"context"
	"database/sql"
	"encoding/json"
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

const defaultStoryboardShotDurationSeconds = 5.0

type StoryboardShot struct {
	ID                       string                             `json:"id"`
	WorkflowRunID            string                             `json:"workflowRunId"`
	ScriptSceneID            *string                            `json:"scriptSceneId,omitempty"`
	ScriptEpisodeID          *string                            `json:"scriptEpisodeId,omitempty"`
	EpisodeIndex             *int                               `json:"episodeIndex,omitempty"`
	EpisodeShotIndex         *int                               `json:"episodeShotIndex,omitempty"`
	EpisodeTitle             string                             `json:"episodeTitle,omitempty"`
	SourceScene              *ShotSourceScene                   `json:"sourceScene,omitempty"`
	ShotIndex                int                                `json:"shotIndex"`
	ShotNo                   int                                `json:"shotNo"`
	Title                    string                             `json:"title,omitempty"`
	DurationSeconds          *float64                           `json:"durationSeconds,omitempty"`
	StoryboardPlanID         *string                            `json:"storyboardPlanId,omitempty"`
	StartTick                int64                              `json:"startTick"`
	EndTick                  int64                              `json:"endTick"`
	PlannedDurationTicks     int64                              `json:"plannedDurationTicks"`
	DurationMinTicks         *int64                             `json:"durationMinTicks,omitempty"`
	DurationMaxTicks         *int64                             `json:"durationMaxTicks,omitempty"`
	DurationSource           string                             `json:"durationSource"`
	TimingConfidence         *float64                           `json:"timingConfidence,omitempty"`
	DurationLocked           bool                               `json:"durationLocked"`
	ShotGroupID              *string                            `json:"shotGroupId,omitempty"`
	OneTake                  bool                               `json:"oneTake"`
	TimingRevision           int                                `json:"timingRevision"`
	TimelineTimebase         int64                              `json:"timelineTimebase"`
	FPSNumerator             int                                `json:"fpsNumerator"`
	FPSDenominator           int                                `json:"fpsDenominator"`
	Visual                   string                             `json:"visual,omitempty"`
	Camera                   string                             `json:"camera,omitempty"`
	Motion                   string                             `json:"motion,omitempty"`
	Mood                     string                             `json:"mood,omitempty"`
	ImagePrompt              string                             `json:"imagePrompt,omitempty"`
	ImagePromptStatus        string                             `json:"imagePromptStatus"`
	ImagePromptErrorCode     *string                            `json:"imagePromptErrorCode,omitempty"`
	ImagePromptErrorMessage  *string                            `json:"imagePromptErrorMessage,omitempty"`
	ImagePromptWorkflowRunID *string                            `json:"imagePromptWorkflowRunId,omitempty"`
	ImagePromptUpdatedAt     *time.Time                         `json:"imagePromptUpdatedAt,omitempty"`
	VideoPrompt              string                             `json:"videoPrompt,omitempty"`
	ScriptDialogue           []workflows.StoryboardDialogueLine `json:"scriptDialogue"`
	VideoPromptStatus        string                             `json:"videoPromptStatus"`
	VideoPromptErrorCode     *string                            `json:"videoPromptErrorCode,omitempty"`
	VideoPromptErrorMessage  *string                            `json:"videoPromptErrorMessage,omitempty"`
	VideoPromptWorkflowRunID *string                            `json:"videoPromptWorkflowRunId,omitempty"`
	VideoPromptUpdatedAt     *time.Time                         `json:"videoPromptUpdatedAt,omitempty"`
	ImageReferenceMode       string                             `json:"imageReferenceMode"`
	ImageReferenceKeys       []string                           `json:"imageReferenceKeys"`
	VideoReferenceMode       string                             `json:"videoReferenceMode"`
	VideoReferenceKeys       []string                           `json:"videoReferenceKeys"`
	ImageArtifactID          *string                            `json:"imageArtifactId,omitempty"`
	ImageMediaFileID         *string                            `json:"imageMediaFileId,omitempty"`
	ImageStorageKey          *string                            `json:"imageStorageKey,omitempty"`
	ImagePreviewURL          *string                            `json:"imagePreviewUrl,omitempty"`
	VideoArtifactID          *string                            `json:"videoArtifactId,omitempty"`
	VideoMediaFileID         *string                            `json:"videoMediaFileId,omitempty"`
	VideoStorageKey          *string                            `json:"videoStorageKey,omitempty"`
	VideoPreviewURL          *string                            `json:"videoPreviewUrl,omitempty"`
	VideoProviderAsyncTaskID *string                            `json:"providerAsyncTaskId,omitempty"`
	VideoExternalTaskID      *string                            `json:"externalTaskId,omitempty"`
	ImageStatus              string                             `json:"imageStatus"`
	VideoStatus              string                             `json:"videoStatus"`
	ActiveVideoRenderPlanID  *string                            `json:"activeVideoRenderPlanId,omitempty"`
	NativeAudioStatus        string                             `json:"nativeAudioStatus"`
	ProductionReadiness      string                             `json:"productionReadiness"`
	ImageErrorCode           *string                            `json:"imageErrorCode,omitempty"`
	ImageErrorMessage        *string                            `json:"imageErrorMessage,omitempty"`
	VideoErrorCode           *string                            `json:"videoErrorCode,omitempty"`
	VideoErrorMessage        *string                            `json:"videoErrorMessage,omitempty"`
	ImageStartedAt           *time.Time                         `json:"imageStartedAt,omitempty"`
	ImageCompletedAt         *time.Time                         `json:"imageCompletedAt,omitempty"`
	VideoStartedAt           *time.Time                         `json:"videoStartedAt,omitempty"`
	VideoCompletedAt         *time.Time                         `json:"videoCompletedAt,omitempty"`
	ImageWorkflowRunID       *string                            `json:"imageWorkflowRunId,omitempty"`
	VideoWorkflowRunID       *string                            `json:"videoWorkflowRunId,omitempty"`
	Status                   string                             `json:"status"`
	ReviewStatus             string                             `json:"reviewStatus"`
	ManualOverride           bool                               `json:"manualOverride"`
	StaleState               string                             `json:"staleState"`
	EditedBy                 *string                            `json:"editedBy,omitempty"`
	EditedAt                 *time.Time                         `json:"editedAt,omitempty"`

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
	AspectRatio           string                               `json:"aspectRatio"`
	Shot                  StoryboardShot                       `json:"shot"`
	ScriptScene           *ShotSourceScene                     `json:"scriptScene,omitempty"`
	Requirements          []StoryboardShotRequirementDetail    `json:"requirements"`
	ImageReferenceOptions []StoryboardShotImageReferenceOption `json:"imageReferenceOptions"`
	ImageGenerationRuns   []StoryboardShotImageGenerationRun   `json:"imageGenerationRuns"`
	VideoReferenceOptions []StoryboardShotVideoReferenceOption `json:"videoReferenceOptions"`
	VideoGenerationRuns   []StoryboardShotVideoGenerationRun   `json:"videoGenerationRuns"`
	ImageArtifact         *Artifact                            `json:"imageArtifact,omitempty"`
	ImagePreviewURL       *string                              `json:"imagePreviewUrl,omitempty"`
	VideoArtifact         *Artifact                            `json:"videoArtifact,omitempty"`
	VideoPreviewURL       *string                              `json:"videoPreviewUrl,omitempty"`
}

type StoryboardShotImageReferenceOption struct {
	Key          string  `json:"key"`
	SourceType   string  `json:"sourceType"`
	SourceID     string  `json:"sourceId"`
	AssetID      string  `json:"assetId"`
	AssetType    string  `json:"assetType"`
	AssetName    string  `json:"assetName"`
	Title        string  `json:"title"`
	ArtifactID   *string `json:"artifactId,omitempty"`
	MediaFileID  *string `json:"mediaFileId,omitempty"`
	StorageKey   *string `json:"storageKey,omitempty"`
	PreviewURL   *string `json:"previewUrl,omitempty"`
	IsShotAsset  bool    `json:"isShotAsset"`
	Selected     bool    `json:"selected"`
	AutoSelected bool    `json:"autoSelected"`
}

type StoryboardShotImageGenerationRun struct {
	ProviderCallID  string          `json:"providerCallId"`
	ModelID         *string         `json:"modelId,omitempty"`
	ModelName       *string         `json:"modelName,omitempty"`
	Status          string          `json:"status"`
	Prompt          *string         `json:"prompt,omitempty"`
	PromptTruncated bool            `json:"promptTruncated"`
	PromptVersionID *string         `json:"promptVersionId,omitempty"`
	PromptHash      *string         `json:"promptHash,omitempty"`
	ArtifactID      *string         `json:"artifactId,omitempty"`
	PreviewURL      *string         `json:"previewUrl,omitempty"`
	ErrorCode       *string         `json:"errorCode,omitempty"`
	ErrorMessage    *string         `json:"errorMessage,omitempty"`
	StartedAt       *time.Time      `json:"startedAt,omitempty"`
	CompletedAt     *time.Time      `json:"completedAt,omitempty"`
	References      json.RawMessage `json:"references"`
}

type StoryboardShotVideoReferenceOption struct {
	Key           string  `json:"key"`
	ReferenceType string  `json:"referenceType"`
	SourceType    string  `json:"sourceType"`
	SourceID      string  `json:"sourceId"`
	AssetID       string  `json:"assetId,omitempty"`
	AssetName     string  `json:"assetName,omitempty"`
	Title         string  `json:"title"`
	ArtifactID    *string `json:"artifactId,omitempty"`
	MediaFileID   *string `json:"mediaFileId,omitempty"`
	StorageKey    *string `json:"storageKey,omitempty"`
	PreviewURL    *string `json:"previewUrl,omitempty"`
	Selected      bool    `json:"selected"`
	AutoSelected  bool    `json:"autoSelected"`
}

type StoryboardShotVideoGenerationRun struct {
	ProviderCallID      string          `json:"providerCallId"`
	ProviderAsyncTaskID *string         `json:"providerAsyncTaskId,omitempty"`
	ExternalTaskID      *string         `json:"externalTaskId,omitempty"`
	ModelID             *string         `json:"modelId,omitempty"`
	ModelName           *string         `json:"modelName,omitempty"`
	Status              string          `json:"status"`
	Prompt              *string         `json:"prompt,omitempty"`
	PromptTruncated     bool            `json:"promptTruncated"`
	PromptVersionID     *string         `json:"promptVersionId,omitempty"`
	PromptHash          *string         `json:"promptHash,omitempty"`
	ArtifactID          *string         `json:"artifactId,omitempty"`
	PreviewURL          *string         `json:"previewUrl,omitempty"`
	ErrorCode           *string         `json:"errorCode,omitempty"`
	ErrorMessage        *string         `json:"errorMessage,omitempty"`
	StartedAt           *time.Time      `json:"startedAt,omitempty"`
	CompletedAt         *time.Time      `json:"completedAt,omitempty"`
	References          json.RawMessage `json:"references"`
}

func (s *Server) listWorkflowRunShots(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	run, err := scanWorkflowRun(s.db.QueryRow(r.Context(), workflowRunSelectSQL(`
		WHERE id = $1
	`), r.PathValue("workflowRunId")))
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
		ORDER BY s.episode_index ASC NULLS LAST, s.episode_shot_index ASC NULLS LAST, s.shot_index ASC
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
		WorkflowRunID        string `json:"workflowRunId"`
		ScriptSceneID        string `json:"scriptSceneId"`
		ShotNo               *int   `json:"shotNo"`
		ShotIndex            *int   `json:"shotIndex"`
		StartTick            *int64 `json:"startTick"`
		EndTick              *int64 `json:"endTick"`
		PlannedDurationTicks *int64 `json:"plannedDurationTicks"`
		Visual               string `json:"visual"`
		Camera               string `json:"camera"`
		Motion               string `json:"motion"`
		Mood                 string `json:"mood"`
		ImagePrompt          string `json:"imagePrompt"`
		VideoPrompt          string `json:"videoPrompt"`
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
	productionConfiguration, err := videoproduction.DecodeProductionConfiguration(productionContext.Binding.ProfileSnapshot)
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	workflowRunID, err := storyboardWorkflowRunForCreateTx(
		r.Context(), tx, project.OrganizationID, project.ID, productionContext,
		strings.TrimSpace(req.WorkflowRunID), principal.UserID,
	)
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	if strings.TrimSpace(req.ScriptSceneID) != "" {
		if _, err := workflows.ScanScriptSceneRecord(tx.QueryRow(r.Context(), workflows.ScriptSceneSelectSQL(`
			WHERE project_id = $1 AND id = $2 AND deleted_at IS NULL
		`), project.ID, strings.TrimSpace(req.ScriptSceneID))); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	shotIndex, shotNo, err := nextStoryboardShotPositionTx(
		r.Context(), tx, project.ID, productionContext.Generation.ID, workflowRunID,
		req.ShotIndex, req.ShotNo,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	timebase := storyboardtiming.Timebase{
		TicksPerSecond: productionConfiguration.TimelineTimebase,
		FPSNumerator:   int64(productionConfiguration.FPSNumerator),
		FPSDenominator: int64(productionConfiguration.FPSDenominator),
	}
	if err := timebase.Validate(); err != nil {
		s.writeError(w, r, err)
		return
	}
	startTick := int64(0)
	if req.StartTick != nil {
		startTick = *req.StartTick
	} else if err := tx.QueryRow(r.Context(), `
		SELECT COALESCE(MAX(end_tick), 0)
		FROM storyboard_shots
		WHERE project_id = $1 AND production_generation_id = $2
		  AND workflow_run_id = $3 AND deleted_at IS NULL
	`, project.ID, productionContext.Generation.ID, workflowRunID).Scan(&startTick); err != nil {
		s.writeError(w, r, err)
		return
	}
	durationTicks := timebase.SecondsToFrameTicksCeil(defaultStoryboardShotDurationSeconds)
	if req.PlannedDurationTicks != nil {
		durationTicks = *req.PlannedDurationTicks
	}
	endTick := startTick + durationTicks
	if req.EndTick != nil {
		endTick = *req.EndTick
		durationTicks = endTick - startTick
	}
	if startTick < 0 || durationTicks <= 0 || !timebase.IsFrameAligned(startTick) || !timebase.IsFrameAligned(endTick) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "shot timing must be positive and aligned to the project frame rate", nil, false)
		return
	}
	var shotID string
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO storyboard_shots(
			organization_id, project_id, workflow_run_id, script_scene_id, shot_index, shot_no,
			start_tick, end_tick, duration_min_ticks, duration_max_ticks, duration_source, duration_locked,
			visual, camera, motion, mood, image_prompt, video_prompt,
			status, review_status, manual_override, stale_state, edited_by, edited_at, metadata,
			production_generation_id
		)
		VALUES ($1, $2, $3, NULLIF($4, '')::uuid, $5, $6,
		        $7, $8, $9, $9, 'manual_locked', true,
		        NULLIF($10, ''), NULLIF($11, ''), NULLIF($12, ''), NULLIF($13, ''),
		        NULLIF($14, ''), NULLIF($15, ''), 'pending', 'pending', true, 'needs_regeneration', $16, now(),
		        jsonb_build_object('timingEditedAt', now(), 'timelineTimebase', $17::bigint, 'fpsNumerator', $18::integer, 'fpsDenominator', $19::integer),
		        $20)
		RETURNING id::text
	`, project.OrganizationID, project.ID, workflowRunID, strings.TrimSpace(req.ScriptSceneID), shotIndex, shotNo, startTick, endTick, durationTicks,
		strings.TrimSpace(req.Visual), strings.TrimSpace(req.Camera), strings.TrimSpace(req.Motion), strings.TrimSpace(req.Mood),
		strings.TrimSpace(req.ImagePrompt), strings.TrimSpace(req.VideoPrompt), principal.UserID,
		productionConfiguration.TimelineTimebase, productionConfiguration.FPSNumerator,
		productionConfiguration.FPSDenominator, productionContext.Generation.ID).Scan(&shotID); err != nil {
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
		"shotId":                 item.ID,
		"workflowRunId":          workflowRunID,
		"scriptSceneId":          item.ScriptSceneID,
		"shotNo":                 item.ShotNo,
		"productionGenerationId": productionContext.Generation.ID,
		"bindingId":              productionContext.Binding.ID,
		"bindingRevision":        productionContext.Binding.Revision,
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
	if current.StoryboardPlanID != nil {
		httpx.WriteError(w, r, http.StatusConflict, "STORYBOARD_PLAN_REVISION_REQUIRED", "storyboard plan shots cannot be deleted in place; create a plan revision instead", map[string]any{
			"storyboardPlanId": *current.StoryboardPlanID,
			"shotId":           current.ID,
		}, false)
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
	if err := reflowStoryboardShotTicksTx(r.Context(), tx, project.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE storyboard_plans
		SET active = false,
		    status = CASE WHEN status = 'ready' THEN 'archived' ELSE status END,
		    stale_state = 'upstream_changed',
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('shotDeletedAt', now())
		WHERE project_id = $1 AND active = true
	`, project.ID); err != nil {
		s.writeError(w, r, err)
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
			ShotID           string `json:"shotId"`
			ShotIndex        int    `json:"shotIndex"`
			ShotNo           int    `json:"shotNo"`
			EpisodeShotIndex *int   `json:"episodeShotIndex,omitempty"`
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
	for _, item := range req.Items {
		shotID := strings.TrimSpace(item.ShotID)
		if shotID == "" || item.ShotIndex < 0 || item.ShotNo <= 0 || (item.EpisodeShotIndex != nil && *item.EpisodeShotIndex < 0) {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "shotId, non-negative shotIndex, and positive shotNo are required", nil, false)
			return
		}
		var storyboardPlanID *string
		if err := tx.QueryRow(r.Context(), `
			SELECT storyboard_plan_id::text
			FROM storyboard_shots
			WHERE project_id = $1 AND id = $2 AND deleted_at IS NULL
		`, project.ID, shotID).Scan(&storyboardPlanID); err != nil {
			s.writeError(w, r, err)
			return
		}
		if storyboardPlanID != nil {
			httpx.WriteError(w, r, http.StatusConflict, "STORYBOARD_PLAN_REVISION_REQUIRED", "storyboard plan shots cannot be reordered in place; use split, merge, or timing revision endpoints", map[string]any{
				"storyboardPlanId": *storyboardPlanID,
				"shotId":           shotID,
			}, false)
			return
		}
	}
	for index, item := range req.Items {
		shotID := strings.TrimSpace(item.ShotID)
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
			    episode_shot_index = COALESCE($5, episode_shot_index),
			    updated_at = now()
			WHERE project_id = $1 AND id = $2 AND deleted_at IS NULL
		`, project.ID, strings.TrimSpace(item.ShotID), item.ShotIndex, item.ShotNo, item.EpisodeShotIndex); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if err := reflowStoryboardShotTicksTx(r.Context(), tx, project.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE storyboard_plans
		SET active = false,
		    status = CASE WHEN status = 'ready' THEN 'archived' ELSE status END,
		    stale_state = 'upstream_changed',
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('shotsReorderedAt', now())
		WHERE project_id = $1 AND active = true
	`, project.ID); err != nil {
		s.writeError(w, r, err)
		return
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

func reflowStoryboardShotTicksTx(ctx context.Context, tx pgx.Tx, projectID string) error {
	_, err := tx.Exec(ctx, `
		WITH durations AS (
			SELECT
				id,
				project_id,
				COALESCE(script_episode_id, workflow_run_id, storyboard_id, project_id) AS partition_id,
				COALESCE(episode_shot_index, shot_index) AS order_index,
				created_at,
				planned_duration_ticks AS duration_ticks
			FROM storyboard_shots
			WHERE project_id = $1 AND deleted_at IS NULL
		), positioned AS (
			SELECT
				id,
				COALESCE(SUM(duration_ticks) OVER (
					PARTITION BY project_id, partition_id
					ORDER BY order_index, created_at, id
					ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING
				), 0)::bigint AS start_tick,
				duration_ticks
			FROM durations
		)
		UPDATE storyboard_shots shot
		SET start_tick = positioned.start_tick,
		    end_tick = positioned.start_tick + positioned.duration_ticks,
		    updated_at = now()
		FROM positioned
		WHERE shot.id = positioned.id
	`, projectID)
	return err
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
	projectReferenceOptions, err := s.projectCurrentAssetReferenceOptions(r, project.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	referenceOptions := s.storyboardShotImageReferenceOptions(r, shot, requirements, projectReferenceOptions...)
	generationRuns, err := s.storyboardShotImageGenerationRuns(r, project.ID, shot.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	videoReferenceOptions := s.storyboardShotVideoReferenceOptions(r, shot, referenceOptions)
	videoGenerationRuns, err := s.storyboardShotVideoGenerationRuns(r, project.ID, shot.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	detail := StoryboardShotDetail{
		AspectRatio:           firstNonEmptyString(project.VideoRatio, stringValue(project.AspectRatio), "16:9"),
		Shot:                  shot,
		ScriptScene:           shot.SourceScene,
		Requirements:          requirements,
		ImageReferenceOptions: referenceOptions,
		ImageGenerationRuns:   generationRuns,
		VideoReferenceOptions: videoReferenceOptions,
		VideoGenerationRuns:   videoGenerationRuns,
		ImageArtifact:         imageArtifact,
		ImagePreviewURL:       firstStringPtr(shot.ImagePreviewURL, imagePreview),
		VideoArtifact:         videoArtifact,
		VideoPreviewURL:       firstStringPtr(shot.VideoPreviewURL, videoPreview),
	}
	httpx.WriteJSON(w, r, http.StatusOK, detail, nil)
}

func (s *Server) storyboardShotImageReferenceOptions(r *http.Request, shot StoryboardShot, requirements []StoryboardShotRequirementDetail, projectOptions ...StoryboardShotImageReferenceOption) []StoryboardShotImageReferenceOption {
	options := make([]StoryboardShotImageReferenceOption, 0)
	indexByKey := map[string]int{}
	autoKeys := map[string]bool{}
	appendOption := func(option StoryboardShotImageReferenceOption) {
		if option.Key == "" || (option.ArtifactID == nil && option.StorageKey == nil) {
			return
		}
		if index, exists := indexByKey[option.Key]; exists {
			if options[index].ArtifactID == nil {
				options[index].ArtifactID = option.ArtifactID
			}
			if options[index].MediaFileID == nil {
				options[index].MediaFileID = option.MediaFileID
			}
			if options[index].StorageKey == nil {
				options[index].StorageKey = option.StorageKey
			}
			if options[index].PreviewURL == nil {
				options[index].PreviewURL = option.PreviewURL
				if options[index].PreviewURL == nil && options[index].StorageKey != nil {
					options[index].PreviewURL = s.previewURLForStorageKey(r, *options[index].StorageKey)
				}
			}
			if option.AutoSelected {
				options[index].AutoSelected = true
				autoKeys[option.Key] = true
			}
			if option.IsShotAsset {
				options[index].IsShotAsset = true
			}
			return
		}
		if option.PreviewURL == nil && option.StorageKey != nil {
			option.PreviewURL = s.previewURLForStorageKey(r, *option.StorageKey)
		}
		indexByKey[option.Key] = len(options)
		if option.AutoSelected {
			autoKeys[option.Key] = true
		}
		options = append(options, option)
	}

	for _, requirement := range requirements {
		asset := requirement.Asset
		assetID := requirement.AssetID
		assetName := ""
		assetType := ""
		if asset != nil {
			assetID = asset.ID
			assetName = asset.Name
			assetType = asset.AssetType
		}
		autoKey := ""
		if requirement.DerivedArtifactID != nil || requirement.DerivedStorageKey != nil {
			autoKey = "derived:" + requirement.ID
			appendOption(StoryboardShotImageReferenceOption{
				Key: autoKey, SourceType: "derived_asset", SourceID: requirement.ID,
				AssetID: assetID, AssetType: assetType, AssetName: assetName, Title: assetName + " · 镜头衍生图",
				ArtifactID: requirement.DerivedArtifactID, MediaFileID: requirement.DerivedMediaFileID,
				StorageKey: requirement.DerivedStorageKey, PreviewURL: requirement.DerivedPreviewURL,
				IsShotAsset: true, AutoSelected: true,
			})
		}
		if asset == nil {
			continue
		}
		if asset.PrimaryReferenceArtifactID != nil || asset.PrimaryReferenceStorageKey != nil || asset.ReferenceArtifactID != nil || asset.ReferenceStorageKey != nil {
			primaryKey := "asset_primary:" + asset.ID
			appendOption(StoryboardShotImageReferenceOption{
				Key: primaryKey, SourceType: "asset_primary", SourceID: asset.ID,
				AssetID: asset.ID, AssetType: asset.AssetType, AssetName: asset.Name, Title: asset.Name + " · 当前主图",
				ArtifactID:  firstStringPtr(asset.PrimaryReferenceArtifactID, asset.ReferenceArtifactID),
				MediaFileID: firstStringPtr(asset.PrimaryReferenceMediaFileID, asset.ReferenceMediaFileID),
				StorageKey:  firstStringPtr(asset.PrimaryReferenceStorageKey, asset.ReferenceStorageKey),
				IsShotAsset: true,
			})
			if autoKey == "" {
				autoKey = primaryKey
			}
		}
		if autoKey != "" {
			autoKeys[autoKey] = true
			if index, ok := indexByKey[autoKey]; ok {
				options[index].AutoSelected = true
			}
		}
	}
	for _, option := range projectOptions {
		appendOption(option)
	}

	selectedKeys := map[string]bool{}
	for _, key := range shot.ImageReferenceKeys {
		selectedKeys[key] = true
	}
	for index := range options {
		switch shot.ImageReferenceMode {
		case "custom":
			options[index].Selected = selectedKeys[options[index].Key]
		case "none":
			options[index].Selected = false
		default:
			options[index].Selected = autoKeys[options[index].Key]
		}
	}
	return options
}

func (s *Server) projectCurrentAssetReferenceOptions(r *http.Request, projectID string) ([]StoryboardShotImageReferenceOption, error) {
	rows, err := s.db.Query(r.Context(), `
		SELECT
			a.id::text,
			a.asset_type,
			a.name,
			COALESCE(a.primary_reference_artifact_id, a.reference_artifact_id)::text,
			COALESCE(a.primary_reference_media_file_id, a.reference_media_file_id)::text,
			COALESCE(
				NULLIF(a.primary_reference_storage_key, ''),
				NULLIF(a.reference_storage_key, ''),
				NULLIF(artifact.storage_key, '')
			)
		FROM canonical_assets a
		LEFT JOIN artifacts artifact
		  ON artifact.id = COALESCE(a.primary_reference_artifact_id, a.reference_artifact_id)
		WHERE a.project_id = $1
		  AND COALESCE(a.status, 'draft') <> 'archived'
		  AND (
			a.primary_reference_artifact_id IS NOT NULL
			OR COALESCE(a.primary_reference_storage_key, '') <> ''
			OR a.reference_artifact_id IS NOT NULL
			OR COALESCE(a.reference_storage_key, '') <> ''
		  )
		ORDER BY CASE a.asset_type WHEN 'character' THEN 1 WHEN 'scene' THEN 2 WHEN 'prop' THEN 3 ELSE 4 END, a.name, a.id
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	options := make([]StoryboardShotImageReferenceOption, 0)
	for rows.Next() {
		var assetID, assetType, assetName string
		var artifactID, mediaFileID, storageKey sql.NullString
		if err := rows.Scan(&assetID, &assetType, &assetName, &artifactID, &mediaFileID, &storageKey); err != nil {
			return nil, err
		}
		options = append(options, StoryboardShotImageReferenceOption{
			Key:         "asset_primary:" + assetID,
			SourceType:  "asset_primary",
			SourceID:    assetID,
			AssetID:     assetID,
			AssetType:   assetType,
			AssetName:   assetName,
			Title:       assetName + " · 当前主图",
			ArtifactID:  stringPtrFromNull(artifactID),
			MediaFileID: stringPtrFromNull(mediaFileID),
			StorageKey:  stringPtrFromNull(storageKey),
		})
	}
	return options, rows.Err()
}

func (s *Server) storyboardShotVideoReferenceOptions(r *http.Request, shot StoryboardShot, imageOptions []StoryboardShotImageReferenceOption) []StoryboardShotVideoReferenceOption {
	options := make([]StoryboardShotVideoReferenceOption, 0, len(imageOptions)+1)
	hasShotImage := shot.ImageArtifactID != nil || shot.ImageStorageKey != nil
	if hasShotImage {
		previewURL := shot.ImagePreviewURL
		if previewURL == nil && shot.ImageStorageKey != nil {
			previewURL = s.previewURLForStorageKey(r, *shot.ImageStorageKey)
		}
		options = append(options, StoryboardShotVideoReferenceOption{
			Key:           "shot_image:" + shot.ID,
			ReferenceType: "first_frame",
			SourceType:    "shot_image",
			SourceID:      shot.ID,
			Title:         "当前镜头图",
			ArtifactID:    shot.ImageArtifactID,
			MediaFileID:   shot.ImageMediaFileID,
			StorageKey:    shot.ImageStorageKey,
			PreviewURL:    previewURL,
			AutoSelected:  true,
		})
	}
	for _, option := range imageOptions {
		options = append(options, StoryboardShotVideoReferenceOption{
			Key:           option.Key,
			ReferenceType: "image",
			SourceType:    option.SourceType,
			SourceID:      option.SourceID,
			AssetID:       option.AssetID,
			AssetName:     option.AssetName,
			Title:         option.Title,
			ArtifactID:    option.ArtifactID,
			MediaFileID:   option.MediaFileID,
			StorageKey:    option.StorageKey,
			PreviewURL:    option.PreviewURL,
			AutoSelected:  !hasShotImage && option.AutoSelected,
		})
	}
	selectedKeys := make(map[string]bool, len(shot.VideoReferenceKeys))
	for _, key := range shot.VideoReferenceKeys {
		selectedKeys[key] = true
	}
	for index := range options {
		switch shot.VideoReferenceMode {
		case "custom":
			options[index].Selected = selectedKeys[options[index].Key]
		case "none":
			options[index].Selected = false
		default:
			options[index].Selected = options[index].AutoSelected
		}
	}
	return options
}

func (s *Server) storyboardShotImageGenerationRuns(r *http.Request, projectID, shotID string) ([]StoryboardShotImageGenerationRun, error) {
	rows, err := s.db.Query(r.Context(), `
		SELECT
			p.id::text,
			p.provider_model_id::text,
			COALESCE(NULLIF(pm.display_name, ''), NULLIF(pm.model_key, '')),
			p.status,
			NULLIF(left(p.request_snapshot->>'prompt', 16000), ''),
			length(COALESCE(p.request_snapshot->>'prompt', '')) > 16000,
			p.prompt_version_id::text,
			NULLIF(p.prompt_hash, ''),
			NULLIF(p.artifact_ids->>0, ''),
			p.error_code,
			p.error_message,
			p.started_at,
			p.completed_at,
			COALESCE(p.request_snapshot->'referenceKeys', n.input->'imageReferenceKeys', '[]'::jsonb)
		FROM provider_call_logs p
		JOIN workflow_node_runs n ON n.id = p.node_run_id
		LEFT JOIN provider_models pm ON pm.id = p.provider_model_id
		WHERE p.project_id = $1
		  AND p.task_type = 'image.generate'
		  AND n.input->>'shotId' = $2
		ORDER BY p.started_at DESC NULLS LAST, p.created_at DESC
		LIMIT 20
	`, projectID, shotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]StoryboardShotImageGenerationRun, 0)
	for rows.Next() {
		var item StoryboardShotImageGenerationRun
		var modelID, modelName, prompt, promptVersionID, promptHash, artifactID, errorCode, errorMessage sql.NullString
		var startedAt, completedAt sql.NullTime
		if err := rows.Scan(&item.ProviderCallID, &modelID, &modelName, &item.Status, &prompt, &item.PromptTruncated, &promptVersionID, &promptHash, &artifactID, &errorCode, &errorMessage, &startedAt, &completedAt, &item.References); err != nil {
			return nil, err
		}
		item.ModelID = stringPtrFromNull(modelID)
		item.ModelName = stringPtrFromNull(modelName)
		item.Prompt = stringPtrFromNull(prompt)
		item.PromptVersionID = stringPtrFromNull(promptVersionID)
		item.PromptHash = stringPtrFromNull(promptHash)
		item.ArtifactID = stringPtrFromNull(artifactID)
		item.ErrorCode = stringPtrFromNull(errorCode)
		item.ErrorMessage = stringPtrFromNull(errorMessage)
		if startedAt.Valid {
			item.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			item.CompletedAt = &completedAt.Time
		}
		if item.ArtifactID != nil {
			_, item.PreviewURL = s.optionalArtifactWithPreview(r, *item.ArtifactID)
		}
		runs = append(runs, item)
	}
	return runs, rows.Err()
}

func (s *Server) storyboardShotVideoGenerationRuns(r *http.Request, projectID, shotID string) ([]StoryboardShotVideoGenerationRun, error) {
	rows, err := s.db.Query(r.Context(), `
		SELECT
			p.id::text,
			t.id::text,
			COALESCE(NULLIF(t.external_task_id, ''), NULLIF(p.external_task_id, '')),
			p.provider_model_id::text,
			COALESCE(NULLIF(pm.display_name, ''), NULLIF(pm.model_key, '')),
			COALESCE(NULLIF(t.status, ''), p.status),
			NULLIF(left(p.request_snapshot->>'prompt', 16000), ''),
			length(COALESCE(p.request_snapshot->>'prompt', '')) > 16000,
			p.prompt_version_id::text,
			NULLIF(p.prompt_hash, ''),
			COALESCE(output_call.artifact_id, NULLIF(p.artifact_ids->>0, '')),
			COALESCE(NULLIF(t.error_code, ''), p.error_code),
			COALESCE(NULLIF(t.error_message, ''), p.error_message),
			COALESCE(t.started_at, p.started_at),
			COALESCE(t.completed_at, output_call.completed_at, p.completed_at),
			COALESCE(n.input->'videoReferenceKeys', '[]'::jsonb)
		FROM provider_call_logs p
		JOIN workflow_node_runs n ON n.id = p.node_run_id
		LEFT JOIN provider_async_tasks t ON t.provider_call_id = p.id
		LEFT JOIN provider_models pm ON pm.id = p.provider_model_id
		LEFT JOIN LATERAL (
			SELECT NULLIF(pc.artifact_ids->>0, '') AS artifact_id, pc.completed_at
			FROM provider_call_logs pc
			WHERE pc.project_id = p.project_id
			  AND pc.task_type = 'video.poll_task'
			  AND NULLIF(pc.external_task_id, '') = COALESCE(NULLIF(t.external_task_id, ''), NULLIF(p.external_task_id, ''))
			ORDER BY pc.completed_at DESC NULLS LAST, pc.created_at DESC
			LIMIT 1
		) output_call ON true
		WHERE p.project_id = $1
		  AND p.task_type = 'video.create_task'
		  AND n.input->>'shotId' = $2
		ORDER BY p.started_at DESC NULLS LAST, p.created_at DESC
		LIMIT 20
	`, projectID, shotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]StoryboardShotVideoGenerationRun, 0)
	for rows.Next() {
		var item StoryboardShotVideoGenerationRun
		var providerAsyncTaskID, externalTaskID, modelID, modelName, prompt, promptVersionID, promptHash, artifactID, errorCode, errorMessage sql.NullString
		var startedAt, completedAt sql.NullTime
		if err := rows.Scan(
			&item.ProviderCallID,
			&providerAsyncTaskID,
			&externalTaskID,
			&modelID,
			&modelName,
			&item.Status,
			&prompt,
			&item.PromptTruncated,
			&promptVersionID,
			&promptHash,
			&artifactID,
			&errorCode,
			&errorMessage,
			&startedAt,
			&completedAt,
			&item.References,
		); err != nil {
			return nil, err
		}
		item.ProviderAsyncTaskID = stringPtrFromNull(providerAsyncTaskID)
		item.ExternalTaskID = stringPtrFromNull(externalTaskID)
		item.ModelID = stringPtrFromNull(modelID)
		item.ModelName = stringPtrFromNull(modelName)
		item.Prompt = stringPtrFromNull(prompt)
		item.PromptVersionID = stringPtrFromNull(promptVersionID)
		item.PromptHash = stringPtrFromNull(promptHash)
		item.ArtifactID = stringPtrFromNull(artifactID)
		item.ErrorCode = stringPtrFromNull(errorCode)
		item.ErrorMessage = stringPtrFromNull(errorMessage)
		if startedAt.Valid {
			item.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			item.CompletedAt = &completedAt.Time
		}
		if item.ArtifactID != nil {
			_, item.PreviewURL = s.optionalArtifactWithPreview(r, *item.ArtifactID)
		}
		runs = append(runs, item)
	}
	return runs, rows.Err()
}

func storyboardShotSelectSQL(where string) string {
	return `
		SELECT
			s.id,
			COALESCE(s.workflow_run_id::text, ''),
			s.script_episode_id::text,
			s.episode_index,
			s.episode_shot_index,
			COALESCE(se.episode_title, ''),
			s.shot_index,
			COALESCE(s.shot_no, s.shot_index + 1),
			COALESCE(s.title, ''),
			s.storyboard_plan_id::text,
			s.start_tick,
			s.end_tick,
			s.planned_duration_ticks,
			s.planned_duration_ticks::float8 / p.timeline_timebase,
			s.duration_min_ticks,
			s.duration_max_ticks,
			s.duration_source,
			s.timing_confidence::float8,
			s.duration_locked,
			s.shot_group_id::text,
			s.one_take,
			s.timing_revision,
			p.timeline_timebase,
			p.fps_numerator,
			p.fps_denominator,
			COALESCE(s.visual, ''),
			COALESCE(s.camera, ''),
			COALESCE(s.motion, ''),
			COALESCE(s.mood, ''),
			COALESCE(s.image_prompt, ''),
			COALESCE(s.image_prompt_status, 'not_started'),
			s.image_prompt_error_code,
			s.image_prompt_error_message,
			s.image_prompt_workflow_run_id::text,
			s.image_prompt_updated_at,
			COALESCE(s.video_prompt, ''),
			COALESCE(s.script_dialogue, '[]'::jsonb),
			COALESCE(s.video_prompt_status, 'not_started'),
			s.video_prompt_error_code,
			s.video_prompt_error_message,
			s.video_prompt_workflow_run_id::text,
			s.video_prompt_updated_at,
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
			COALESCE(s.image_status, 'not_started'),
			COALESCE(s.video_status, 'not_started'),
			s.active_video_render_plan_id::text,
			COALESCE(s.native_audio_status, 'not_requested'),
			COALESCE(s.production_readiness, 'blocked'),
			s.image_error_code,
			s.image_error_message,
			s.video_error_code,
			s.video_error_message,
			s.image_started_at,
			s.image_completed_at,
			s.video_started_at,
			s.video_completed_at,
			s.image_workflow_run_id::text,
			s.video_workflow_run_id::text,
			COALESCE(s.status, 'pending'),
			COALESCE(s.review_status, 'pending'),
			COALESCE(s.manual_override, false),
			COALESCE(s.stale_state, 'fresh'),
			s.edited_by,
			s.edited_at,
			COALESCE(s.image_reference_mode, 'auto'),
			COALESCE(s.image_reference_keys, ARRAY[]::text[]),
			COALESCE(s.video_reference_mode, 'auto'),
			COALESCE(s.video_reference_keys, ARRAY[]::text[]),
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
		LEFT JOIN script_episodes se ON se.id = s.script_episode_id
		JOIN projects p ON p.id = s.project_id
	` + where
}

func scanStoryboardShot(row pgx.Row) (StoryboardShot, error) {
	var item StoryboardShot
	var duration sql.NullFloat64
	var storyboardPlanID, shotGroupID sql.NullString
	var durationMinTicks, durationMaxTicks sql.NullInt64
	var timingConfidence sql.NullFloat64
	var imageArtifactID, imageMediaFileID, imageStorageKey, imageArtifactStorageKey, imageArtifactMimeType sql.NullString
	var videoArtifactID, videoMediaFileID, videoStorageKey, videoArtifactStorageKey, videoArtifactMimeType sql.NullString
	var providerAsyncTaskID, externalTaskID, activeVideoRenderPlanID, imageErrorCode, imageErrorMessage, videoErrorCode, videoErrorMessage, imageWorkflowRunID, videoWorkflowRunID, imagePromptErrorCode, imagePromptErrorMessage, imagePromptWorkflowRunID, videoPromptErrorCode, videoPromptErrorMessage, videoPromptWorkflowRunID, editedBy sql.NullString
	var scriptSceneID, scriptEpisodeID, sourceSceneID, sourceSceneTitle, sourceSceneLocation sql.NullString
	var episodeIndex, episodeShotIndex sql.NullInt32
	var sourceSceneNo sql.NullInt64
	var sourceSceneCharacters, scriptDialogue []byte
	var imageStartedAt, imageCompletedAt, videoStartedAt, videoCompletedAt, imagePromptUpdatedAt, videoPromptUpdatedAt, editedAt sql.NullTime
	err := row.Scan(
		&item.ID,
		&item.WorkflowRunID,
		&scriptEpisodeID,
		&episodeIndex,
		&episodeShotIndex,
		&item.EpisodeTitle,
		&item.ShotIndex,
		&item.ShotNo,
		&item.Title,
		&storyboardPlanID,
		&item.StartTick,
		&item.EndTick,
		&item.PlannedDurationTicks,
		&duration,
		&durationMinTicks,
		&durationMaxTicks,
		&item.DurationSource,
		&timingConfidence,
		&item.DurationLocked,
		&shotGroupID,
		&item.OneTake,
		&item.TimingRevision,
		&item.TimelineTimebase,
		&item.FPSNumerator,
		&item.FPSDenominator,
		&item.Visual,
		&item.Camera,
		&item.Motion,
		&item.Mood,
		&item.ImagePrompt,
		&item.ImagePromptStatus,
		&imagePromptErrorCode,
		&imagePromptErrorMessage,
		&imagePromptWorkflowRunID,
		&imagePromptUpdatedAt,
		&item.VideoPrompt,
		&scriptDialogue,
		&item.VideoPromptStatus,
		&videoPromptErrorCode,
		&videoPromptErrorMessage,
		&videoPromptWorkflowRunID,
		&videoPromptUpdatedAt,
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
		&item.ImageStatus,
		&item.VideoStatus,
		&activeVideoRenderPlanID,
		&item.NativeAudioStatus,
		&item.ProductionReadiness,
		&imageErrorCode,
		&imageErrorMessage,
		&videoErrorCode,
		&videoErrorMessage,
		&imageStartedAt,
		&imageCompletedAt,
		&videoStartedAt,
		&videoCompletedAt,
		&imageWorkflowRunID,
		&videoWorkflowRunID,
		&item.Status,
		&item.ReviewStatus,
		&item.ManualOverride,
		&item.StaleState,
		&editedBy,
		&editedAt,
		&item.ImageReferenceMode,
		&item.ImageReferenceKeys,
		&item.VideoReferenceMode,
		&item.VideoReferenceKeys,
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
	item.StoryboardPlanID = stringPtrFromNull(storyboardPlanID)
	item.ActiveVideoRenderPlanID = stringPtrFromNull(activeVideoRenderPlanID)
	if durationMinTicks.Valid {
		item.DurationMinTicks = &durationMinTicks.Int64
	}
	if durationMaxTicks.Valid {
		item.DurationMaxTicks = &durationMaxTicks.Int64
	}
	if timingConfidence.Valid {
		item.TimingConfidence = &timingConfidence.Float64
	}
	item.ShotGroupID = stringPtrFromNull(shotGroupID)
	item.ImageArtifactID = stringPtrFromNull(imageArtifactID)
	item.ScriptEpisodeID = stringPtrFromNull(scriptEpisodeID)
	if episodeIndex.Valid {
		value := int(episodeIndex.Int32)
		item.EpisodeIndex = &value
	}
	if episodeShotIndex.Valid {
		value := int(episodeShotIndex.Int32)
		item.EpisodeShotIndex = &value
	}
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
	item.ImageErrorCode = stringPtrFromNull(imageErrorCode)
	item.ImageErrorMessage = stringPtrFromNull(imageErrorMessage)
	item.VideoErrorCode = stringPtrFromNull(videoErrorCode)
	item.VideoErrorMessage = stringPtrFromNull(videoErrorMessage)
	item.ImagePromptErrorCode = stringPtrFromNull(imagePromptErrorCode)
	item.ImagePromptErrorMessage = stringPtrFromNull(imagePromptErrorMessage)
	item.ImagePromptWorkflowRunID = stringPtrFromNull(imagePromptWorkflowRunID)
	if imagePromptUpdatedAt.Valid {
		item.ImagePromptUpdatedAt = &imagePromptUpdatedAt.Time
	}
	item.VideoPromptErrorCode = stringPtrFromNull(videoPromptErrorCode)
	item.VideoPromptErrorMessage = stringPtrFromNull(videoPromptErrorMessage)
	item.VideoPromptWorkflowRunID = stringPtrFromNull(videoPromptWorkflowRunID)
	if videoPromptUpdatedAt.Valid {
		item.VideoPromptUpdatedAt = &videoPromptUpdatedAt.Time
	}
	if err == nil {
		if decodeErr := json.Unmarshal(scriptDialogue, &item.ScriptDialogue); decodeErr != nil {
			return StoryboardShot{}, decodeErr
		}
		item.ScriptDialogue = workflows.NormalizeStoryboardDialogue(item.ScriptDialogue)
	}
	item.ImageWorkflowRunID = stringPtrFromNull(imageWorkflowRunID)
	item.VideoWorkflowRunID = stringPtrFromNull(videoWorkflowRunID)
	if imageStartedAt.Valid {
		item.ImageStartedAt = &imageStartedAt.Time
	}
	if imageCompletedAt.Valid {
		item.ImageCompletedAt = &imageCompletedAt.Time
	}
	if videoStartedAt.Valid {
		item.VideoStartedAt = &videoStartedAt.Time
	}
	if videoCompletedAt.Valid {
		item.VideoCompletedAt = &videoCompletedAt.Time
	}
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
	normalizeStoryboardShotProductionStatus(&item)
	return item, err
}

func normalizeStoryboardShotProductionStatus(item *StoryboardShot) {
	hasImage := item.ImageArtifactID != nil || item.ImageMediaFileID != nil || stringValue(item.ImageStorageKey) != ""
	hasVideo := item.VideoArtifactID != nil || item.VideoMediaFileID != nil || stringValue(item.VideoStorageKey) != ""
	item.ImageStatus = normalizeShotProductionStatus(item.ImageStatus, item.StaleState, hasImage)
	item.VideoStatus = normalizeShotProductionStatus(item.VideoStatus, item.StaleState, hasVideo)
}

func normalizeShotProductionStatus(current, staleState string, hasArtifact bool) string {
	current = strings.TrimSpace(current)
	stale := strings.TrimSpace(staleState) != "" && staleState != "fresh"
	switch current {
	case "queued", "running", "succeeded", "failed", "cancelled":
		return current
	case "stale":
		if hasArtifact {
			return "stale"
		}
	}
	if stale && hasArtifact {
		return "stale"
	}
	if hasArtifact {
		return "succeeded"
	}
	return "not_started"
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

func storyboardWorkflowRunForCreateTx(
	ctx context.Context,
	tx pgx.Tx,
	organizationID, projectID string,
	productionContext videoproduction.Context,
	requestedWorkflowRunID, userID string,
) (string, error) {
	if requestedWorkflowRunID != "" {
		var id, runOrganizationID, runProjectID, generationID, bindingID, workflowType string
		var bindingRevision int64
		if err := tx.QueryRow(ctx, `
			SELECT id::text, organization_id::text, project_id::text,
			       production_generation_id::text, video_production_binding_id::text,
			       video_production_binding_revision, workflow_type
			FROM workflow_runs
			WHERE id::text = $1
		`, requestedWorkflowRunID).Scan(
			&id, &runOrganizationID, &runProjectID, &generationID, &bindingID,
			&bindingRevision, &workflowType,
		); err != nil {
			return "", err
		}
		if runOrganizationID != organizationID || runProjectID != projectID ||
			generationID != productionContext.Generation.ID || bindingID != productionContext.Binding.ID ||
			bindingRevision != productionContext.Binding.Revision || !manualStoryboardWorkflowTypeAllowed(workflowType) {
			return "", videoproduction.NewError(
				videoproduction.CodeGenerationMismatch,
				"所选工作流不属于当前视频生产代，无法创建分镜",
				false,
			)
		}
		return id, nil
	}
	var id string
	err := tx.QueryRow(ctx, `
		SELECT id::text
		FROM workflow_runs
		WHERE project_id = $1
		  AND organization_id = $2
		  AND production_generation_id = $3
		  AND video_production_binding_id = $4
		  AND video_production_binding_revision = $5
		  AND workflow_type IN ('script_to_storyboard', 'script_to_video', 'full_production')
		ORDER BY created_at DESC
		LIMIT 1
	`, projectID, organizationID, productionContext.Generation.ID,
		productionContext.Binding.ID, productionContext.Binding.Revision).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != pgx.ErrNoRows {
		return "", err
	}
	if err := tx.QueryRow(ctx, `
		WITH new_run AS (SELECT gen_random_uuid() AS id)
		INSERT INTO workflow_runs(
			id, organization_id, project_id, temporal_workflow_id, workflow_type, status,
			input, output, created_by, production_generation_id,
			video_production_binding_id, video_production_binding_revision
		)
		SELECT id, $1, $2, 'manual-storyboard-' || id::text, 'script_to_storyboard',
		       'succeeded', '{"workflowType":"script_to_storyboard","input":{"manual":true}}'::jsonb,
		       '{}', $3, $4, $5, $6
		FROM new_run
		RETURNING id::text
	`, organizationID, projectID, userID, productionContext.Generation.ID,
		productionContext.Binding.ID, productionContext.Binding.Revision).Scan(&id); err != nil {
		return "", err
	}
	return id, nil
}

func manualStoryboardWorkflowTypeAllowed(workflowType string) bool {
	switch workflowType {
	case "script_to_storyboard", "script_to_video", "full_production":
		return true
	default:
		return false
	}
}

func nextStoryboardShotPositionTx(
	ctx context.Context,
	tx pgx.Tx,
	projectID, generationID, workflowRunID string,
	requestedShotIndex, requestedShotNo *int,
) (int, int, error) {
	if requestedShotIndex != nil && requestedShotNo != nil {
		return *requestedShotIndex, *requestedShotNo, nil
	}
	var maxIndex, maxNo sql.NullInt64
	if err := tx.QueryRow(ctx, `
		SELECT max(shot_index), max(COALESCE(shot_no, shot_index + 1))
		FROM storyboard_shots
		WHERE project_id = $1 AND production_generation_id = $2
		  AND workflow_run_id = $3 AND deleted_at IS NULL
	`, projectID, generationID, workflowRunID).Scan(&maxIndex, &maxNo); err != nil {
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
