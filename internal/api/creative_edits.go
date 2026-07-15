package api

import (
	"net/http"
	"slices"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/production"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	storyboardtiming "github.com/Einzieg/cineweave/internal/storyboard"
)

func (s *Server) updateStoryboardShot(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	current, err := s.storyboardShotByID(r, project.ID, r.PathValue("shotId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req struct {
		Visual               *string   `json:"visual"`
		Camera               *string   `json:"camera"`
		Motion               *string   `json:"motion"`
		Mood                 *string   `json:"mood"`
		PlannedDurationTicks *int64    `json:"plannedDurationTicks"`
		ImagePrompt          *string   `json:"imagePrompt"`
		VideoPrompt          *string   `json:"videoPrompt"`
		ImageReferenceMode   *string   `json:"imageReferenceMode"`
		ImageReferenceKeys   *[]string `json:"imageReferenceKeys"`
		VideoReferenceMode   *string   `json:"videoReferenceMode"`
		VideoReferenceKeys   *[]string `json:"videoReferenceKeys"`
	}
	if !decode(w, r, &req) {
		return
	}
	if current.StoryboardPlanID != nil && req.PlannedDurationTicks != nil {
		httpx.WriteError(w, r, http.StatusConflict, "STORYBOARD_PLAN_REVISION_REQUIRED", "planned timing for a storyboard plan shot must be changed through the storyboard plan timing revision endpoint", map[string]any{
			"storyboardPlanId": *current.StoryboardPlanID,
			"shotId":           current.ID,
		}, false)
		return
	}
	timebase := storyboardtiming.Timebase{
		TicksPerSecond: project.TimelineTimebase,
		FPSNumerator:   int64(project.FPSNumerator),
		FPSDenominator: int64(project.FPSDenominator),
	}
	if req.PlannedDurationTicks != nil && (*req.PlannedDurationTicks <= 0 || !timebase.IsFrameAligned(*req.PlannedDurationTicks)) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "plannedDurationTicks must be positive and aligned to the project frame rate", nil, false)
		return
	}
	imageReferenceMode := current.ImageReferenceMode
	if imageReferenceMode == "" {
		imageReferenceMode = "auto"
	}
	imageReferenceKeys := append([]string(nil), current.ImageReferenceKeys...)
	if req.ImageReferenceMode != nil {
		imageReferenceMode = strings.TrimSpace(*req.ImageReferenceMode)
	}
	if req.ImageReferenceKeys != nil {
		imageReferenceKeys = cleanStoryboardShotReferenceKeys(*req.ImageReferenceKeys)
	}
	videoReferenceMode := current.VideoReferenceMode
	if videoReferenceMode == "" {
		videoReferenceMode = "auto"
	}
	videoReferenceKeys := append([]string(nil), current.VideoReferenceKeys...)
	if req.VideoReferenceMode != nil {
		videoReferenceMode = strings.TrimSpace(*req.VideoReferenceMode)
	}
	if req.VideoReferenceKeys != nil {
		videoReferenceKeys = cleanStoryboardShotReferenceKeys(*req.VideoReferenceKeys)
	}
	var requirements []StoryboardShotRequirementDetail
	var imageOptions []StoryboardShotImageReferenceOption
	loadReferenceOptions := func() error {
		if requirements != nil {
			return nil
		}
		var err error
		requirements, err = s.storyboardShotRequirementDetails(r, project.ID, current.ID)
		if err == nil {
			var projectOptions []StoryboardShotImageReferenceOption
			projectOptions, err = s.projectCurrentAssetReferenceOptions(r, project.ID)
			if err == nil {
				imageOptions = s.storyboardShotImageReferenceOptions(r, current, requirements, projectOptions...)
			}
		}
		return err
	}
	if req.ImageReferenceMode != nil || req.ImageReferenceKeys != nil {
		if imageReferenceMode != "auto" && imageReferenceMode != "custom" && imageReferenceMode != "none" {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "imageReferenceMode must be auto, custom, or none", nil, false)
			return
		}
		if imageReferenceMode != "custom" {
			imageReferenceKeys = []string{}
		} else {
			if err := loadReferenceOptions(); err != nil {
				s.writeError(w, r, err)
				return
			}
			available := map[string]bool{}
			for _, option := range imageOptions {
				available[option.Key] = true
			}
			if len(imageReferenceKeys) == 0 {
				httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "custom image references require at least one selected reference", nil, false)
				return
			}
			for _, key := range imageReferenceKeys {
				if !available[key] {
					httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "selected image reference is not available for this shot", map[string]any{"referenceKey": key}, false)
					return
				}
			}
		}
	}
	if req.VideoReferenceMode != nil || req.VideoReferenceKeys != nil {
		if videoReferenceMode != "auto" && videoReferenceMode != "custom" && videoReferenceMode != "none" {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "videoReferenceMode must be auto, custom, or none", nil, false)
			return
		}
		if videoReferenceMode != "custom" {
			videoReferenceKeys = []string{}
		} else {
			if err := loadReferenceOptions(); err != nil {
				s.writeError(w, r, err)
				return
			}
			available := map[string]bool{}
			for _, option := range s.storyboardShotVideoReferenceOptions(r, current, imageOptions) {
				available[option.Key] = true
			}
			if len(videoReferenceKeys) == 0 {
				httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "custom video references require at least one selected reference", nil, false)
				return
			}
			for _, key := range videoReferenceKeys {
				if !available[key] {
					httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "selected video reference is not available for this shot", map[string]any{"referenceKey": key}, false)
					return
				}
			}
		}
	}
	hasFields := req.Visual != nil || req.Camera != nil || req.Motion != nil || req.Mood != nil || req.PlannedDurationTicks != nil ||
		req.ImagePrompt != nil || req.VideoPrompt != nil || req.ImageReferenceMode != nil || req.ImageReferenceKeys != nil ||
		req.VideoReferenceMode != nil || req.VideoReferenceKeys != nil
	if !hasFields {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "at least one storyboard shot field is required", nil, false)
		return
	}
	visualChanged := stringFieldChanged(req.Visual, current.Visual)
	cameraChanged := stringFieldChanged(req.Camera, current.Camera)
	motionChanged := stringFieldChanged(req.Motion, current.Motion)
	moodChanged := stringFieldChanged(req.Mood, current.Mood)
	durationChanged := req.PlannedDurationTicks != nil && *req.PlannedDurationTicks != current.PlannedDurationTicks
	imageReferenceChanged := (req.ImageReferenceMode != nil && imageReferenceMode != current.ImageReferenceMode) ||
		((req.ImageReferenceKeys != nil || req.ImageReferenceMode != nil) && !slices.Equal(imageReferenceKeys, current.ImageReferenceKeys))
	videoReferenceChanged := (req.VideoReferenceMode != nil && videoReferenceMode != current.VideoReferenceMode) ||
		((req.VideoReferenceKeys != nil || req.VideoReferenceMode != nil) && !slices.Equal(videoReferenceKeys, current.VideoReferenceKeys))
	manualImagePromptStatusChanged := req.ImagePrompt != nil && current.ImagePromptStatus != "succeeded"
	imageChanged := visualChanged || cameraChanged || motionChanged || moodChanged || stringFieldChanged(req.ImagePrompt, current.ImagePrompt) || manualImagePromptStatusChanged || imageReferenceChanged
	videoChanged := imageChanged || durationChanged || stringFieldChanged(req.VideoPrompt, current.VideoPrompt) || videoReferenceChanged
	if !videoChanged {
		httpx.WriteJSON(w, r, http.StatusOK, current, nil)
		return
	}
	if imageChanged && (current.ImageStatus == "queued" || current.ImageStatus == "running") {
		httpx.WriteError(w, r, http.StatusConflict, "SHOT_IMAGE_RUNNING", "shot image settings cannot be changed while generation is running", nil, true)
		return
	}
	if imageChanged && (current.ImagePromptStatus == "queued" || current.ImagePromptStatus == "running") {
		httpx.WriteError(w, r, http.StatusConflict, "SHOT_IMAGE_PROMPT_RUNNING", "shot image prompt settings cannot be changed while prompt generation is running", nil, true)
		return
	}
	if videoChanged && (current.VideoStatus == "queued" || current.VideoStatus == "running") {
		httpx.WriteError(w, r, http.StatusConflict, "SHOT_VIDEO_RUNNING", "shot video settings cannot be changed while generation is running", nil, true)
		return
	}
	if videoChanged && (current.VideoPromptStatus == "queued" || current.VideoPromptStatus == "running") {
		httpx.WriteError(w, r, http.StatusConflict, "SHOT_VIDEO_PROMPT_RUNNING", "shot video prompt settings cannot be changed while prompt generation is running", nil, true)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	manualImagePromptMetadata := mustRawJSON(map[string]any{
		"status":     "manual",
		"promptHash": promptsvc.HashText(trimPtr(req.ImagePrompt)),
		"editedBy":   principal.UserID,
	})
	if _, err := tx.Exec(r.Context(), `
		UPDATE storyboard_shots
		SET visual = CASE WHEN $3 THEN NULLIF($4, '') ELSE visual END,
		    camera = CASE WHEN $5 THEN NULLIF($6, '') ELSE camera END,
		    motion = CASE WHEN $7 THEN NULLIF($8, '') ELSE motion END,
		    mood = CASE WHEN $9 THEN NULLIF($10, '') ELSE mood END,
		    end_tick = CASE WHEN $11 THEN start_tick + $12::bigint ELSE end_tick END,
		    duration_min_ticks = CASE WHEN $11 THEN $12::bigint ELSE duration_min_ticks END,
		    duration_max_ticks = CASE WHEN $11 THEN $12::bigint ELSE duration_max_ticks END,
		    duration_source = CASE WHEN $11 THEN 'manual_locked' ELSE duration_source END,
		    duration_locked = CASE WHEN $11 THEN true ELSE duration_locked END,
		    image_prompt = CASE WHEN $13 THEN NULLIF($14, '') ELSE image_prompt END,
		    image_prompt_status = CASE
		      WHEN $13 AND NULLIF(BTRIM($14), '') IS NOT NULL THEN 'succeeded'
		      WHEN $25 THEN 'not_started'
		      ELSE image_prompt_status
		    END,
		    image_prompt_error_code = CASE WHEN $13 OR $25 THEN NULL ELSE image_prompt_error_code END,
		    image_prompt_error_message = CASE WHEN $13 OR $25 THEN NULL ELSE image_prompt_error_message END,
		    image_prompt_workflow_run_id = CASE WHEN $13 OR $25 THEN NULL ELSE image_prompt_workflow_run_id END,
		    image_prompt_updated_at = CASE WHEN $13 OR $25 THEN now() ELSE image_prompt_updated_at END,
		    video_prompt = CASE WHEN $15 THEN NULLIF($16, '') ELSE video_prompt END,
		    video_prompt_status = CASE
		      WHEN $15 AND NULLIF(BTRIM($16), '') IS NOT NULL THEN 'succeeded'
		      WHEN $15 OR $26 THEN 'not_started'
		      ELSE video_prompt_status
		    END,
		    video_prompt_error_code = CASE WHEN $15 OR $26 THEN NULL ELSE video_prompt_error_code END,
		    video_prompt_error_message = CASE WHEN $15 OR $26 THEN NULL ELSE video_prompt_error_message END,
		    video_prompt_updated_at = CASE WHEN $15 OR $26 THEN now() ELSE video_prompt_updated_at END,
		    image_reference_mode = CASE WHEN $17 THEN $18 ELSE image_reference_mode END,
		    image_reference_keys = CASE WHEN $19 THEN $20::text[] ELSE image_reference_keys END,
		    video_reference_mode = CASE WHEN $21 THEN $22 ELSE video_reference_mode END,
		    video_reference_keys = CASE WHEN $23 THEN $24::text[] ELSE video_reference_keys END,
		    review_status = 'pending',
		    manual_override = true,
		    stale_state = CASE WHEN $25 THEN 'needs_regeneration' ELSE stale_state END,
		    image_status = CASE
		      WHEN NOT $25 THEN image_status
		      WHEN image_artifact_id IS NOT NULL OR image_media_file_id IS NOT NULL OR COALESCE(image_storage_key, '') <> '' THEN 'stale'
		      ELSE 'not_started'
		    END,
		    image_error_code = CASE WHEN $25 THEN NULL ELSE image_error_code END,
		    image_error_message = CASE WHEN $25 THEN NULL ELSE image_error_message END,
		    video_status = CASE
		      WHEN NOT $26 THEN video_status
		      WHEN video_artifact_id IS NOT NULL OR video_media_file_id IS NOT NULL OR COALESCE(video_storage_key, '') <> '' THEN 'stale'
		      ELSE 'not_started'
		    END,
		    video_error_code = CASE WHEN $26 THEN NULL ELSE video_error_code END,
		    video_error_message = CASE WHEN $26 THEN NULL ELSE video_error_message END,
		    metadata = CASE
		      WHEN $13 AND NULLIF(BTRIM($14), '') IS NOT NULL THEN COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('imagePromptAgent', $28::jsonb)
		      ELSE metadata
		    END,
		    edited_by = $27,
		    edited_at = now(),
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
	`, project.ID, current.ID,
		req.Visual != nil, trimPtr(req.Visual),
		req.Camera != nil, trimPtr(req.Camera),
		req.Motion != nil, trimPtr(req.Motion),
		req.Mood != nil, trimPtr(req.Mood),
		req.PlannedDurationTicks != nil, int64PtrValue(req.PlannedDurationTicks),
		req.ImagePrompt != nil, trimPtr(req.ImagePrompt),
		req.VideoPrompt != nil, trimPtr(req.VideoPrompt),
		req.ImageReferenceMode != nil, imageReferenceMode,
		req.ImageReferenceKeys != nil || req.ImageReferenceMode != nil, imageReferenceKeys,
		req.VideoReferenceMode != nil, videoReferenceMode,
		req.VideoReferenceKeys != nil || req.VideoReferenceMode != nil, videoReferenceKeys,
		imageChanged,
		videoChanged,
		principal.UserID,
		manualImagePromptMetadata); err != nil {
		s.writeError(w, r, err)
		return
	}
	videoPlanInputsChanged := imageChanged || durationChanged || videoReferenceChanged
	if videoPlanInputsChanged && current.ActiveVideoRenderPlanID != nil {
		if _, err := tx.Exec(r.Context(), `
			UPDATE video_render_plans
			SET active = false,
			    status = CASE WHEN status IN ('archived', 'cancelled') THEN status ELSE 'stale' END,
			    metadata = metadata || jsonb_build_object('shotInputsChangedAt', now()),
			    updated_at = now()
			WHERE id = $1 AND project_id = $2
		`, *current.ActiveVideoRenderPlanID, project.ID); err != nil {
			s.writeError(w, r, err)
			return
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE storyboard_shots
			SET active_video_render_plan_id = NULL,
			    video_prompt_status = 'not_started',
			    video_prompt_error_code = NULL,
			    video_prompt_error_message = NULL,
			    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
			      'videoPromptPlan', jsonb_build_object('status', 'stale', 'invalidatedAt', now())
			    ),
			    updated_at = now()
			WHERE id = $1 AND project_id = $2
		`, current.ID, project.ID); err != nil {
			s.writeError(w, r, err)
			return
		}
	} else if req.VideoPrompt != nil {
		manualPrompt := strings.TrimSpace(*req.VideoPrompt)
		if manualPrompt == "" {
			if _, err := tx.Exec(r.Context(), `
				UPDATE video_render_segments segment
				SET prompt = NULL,
				    metadata = metadata || jsonb_build_object('promptStatus', 'not_started') ||
				      jsonb_build_object('videoPromptAgent', jsonb_build_object('status', 'not_started')),
				    updated_at = now()
				FROM storyboard_shots shot
				WHERE shot.id = $1 AND shot.project_id = $2
				  AND segment.video_render_plan_id = shot.active_video_render_plan_id
			`, current.ID, project.ID); err != nil {
				s.writeError(w, r, err)
				return
			}
		} else {
			manualPromptHash := promptsvc.HashText(manualPrompt)
			result, err := tx.Exec(r.Context(), `
				UPDATE video_render_segments segment
				SET prompt = $3,
				    metadata = metadata || jsonb_build_object('promptStatus', 'succeeded', 'promptCompletedAt', now()) ||
				      jsonb_build_object('videoPromptAgent', jsonb_build_object(
				        'status', 'manual_approved', 'promptHash', $4::text,
				        'editedBy', $5::uuid::text, 'editedAt', now()
				      )),
				    error_code = NULL,
				    error_message = NULL,
				    updated_at = now()
				FROM storyboard_shots shot
				JOIN video_render_plans plan ON plan.id = shot.active_video_render_plan_id
				WHERE shot.id = $1 AND shot.project_id = $2
				  AND plan.active = true
				  AND plan.status NOT IN ('stale', 'archived', 'cancelled', 'replan_required')
				  AND segment.video_render_plan_id = plan.id
			`, current.ID, project.ID, manualPrompt, manualPromptHash, principal.UserID)
			if err != nil {
				s.writeError(w, r, err)
				return
			}
			promptPlanStatus := "ready"
			promptStatus := "succeeded"
			if result.RowsAffected() == 0 {
				promptPlanStatus = "missing"
				promptStatus = "not_started"
			}
			if _, err := tx.Exec(r.Context(), `
				UPDATE storyboard_shots
				SET video_prompt_status = $3,
				    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
				      'videoPromptPlan', jsonb_build_object(
				        'status', $4::text, 'promptSource', 'manual',
				        'promptHash', $5::text, 'updatedAt', now()
				      )
				    ),
				    updated_at = now()
				WHERE id = $1 AND project_id = $2
			`, current.ID, project.ID, promptStatus, promptPlanStatus, manualPromptHash); err != nil {
				s.writeError(w, r, err)
				return
			}
		}
	}
	if durationChanged {
		if err := reflowStoryboardShotTicksTx(r.Context(), tx, project.ID); err != nil {
			s.writeError(w, r, err)
			return
		}
		if current.StoryboardPlanID != nil {
			if _, err := tx.Exec(r.Context(), `
				UPDATE storyboard_plans
				SET active = false,
				    status = CASE WHEN status = 'ready' THEN 'archived' ELSE status END,
				    stale_state = 'upstream_changed',
				    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('shotTimingChangedAt', now())
				WHERE id = $1
			`, *current.StoryboardPlanID); err != nil {
				s.writeError(w, r, err)
				return
			}
		}
	}
	item, err := scanStoryboardShot(tx.QueryRow(r.Context(), storyboardShotSelectSQL(`
		WHERE s.project_id = $1 AND s.id = $2
	`), project.ID, current.ID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if videoChanged {
		if err := production.MarkFinalVideoStale(r.Context(), tx, project.ID, current.WorkflowRunID); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "storyboard.shot.updated", "storyboard_shot", item.ID, mustRawJSON(map[string]any{
		"shotId":             item.ID,
		"manualOverride":     item.ManualOverride,
		"staleState":         item.StaleState,
		"imageReferenceMode": item.ImageReferenceMode,
		"imageReferenceKeys": item.ImageReferenceKeys,
		"videoReferenceMode": item.VideoReferenceMode,
		"videoReferenceKeys": item.VideoReferenceKeys,
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func cleanStoryboardShotReferenceKeys(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
		if len(out) == 16 {
			break
		}
	}
	return out
}

func stringFieldChanged(value *string, current string) bool {
	return value != nil && strings.TrimSpace(*value) != strings.TrimSpace(current)
}

func (s *Server) unlinkStoryboardShotMedia(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	current, err := s.storyboardShotByID(r, project.ID, r.PathValue("shotId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req struct {
		Kind string `json:"kind"`
	}
	if !decode(w, r, &req) {
		return
	}
	kind := strings.TrimSpace(req.Kind)
	if kind != "image" && kind != "video" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "kind must be image or video", nil, false)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())

	if kind == "image" {
		_, err = tx.Exec(r.Context(), `
			UPDATE storyboard_shots
			SET image_artifact_id = NULL,
			    image_media_file_id = NULL,
			    image_storage_key = NULL,
			    image_status = 'not_started',
			    image_error_code = NULL,
			    image_error_message = NULL,
			    video_status = CASE
			      WHEN video_artifact_id IS NOT NULL OR video_media_file_id IS NOT NULL OR COALESCE(video_storage_key, '') <> '' THEN 'stale'
			      ELSE video_status
			    END,
			    stale_state = 'needs_regeneration',
			    updated_at = now()
			WHERE project_id = $1 AND id = $2
		`, project.ID, current.ID)
	} else {
		_, err = tx.Exec(r.Context(), `
			UPDATE storyboard_shots
			SET video_artifact_id = NULL,
			    video_media_file_id = NULL,
			    video_storage_key = NULL,
			    video_provider_async_task_id = NULL,
			    video_external_task_id = NULL,
			    video_status = 'not_started',
			    video_error_code = NULL,
			    video_error_message = NULL,
			    stale_state = 'needs_regeneration',
			    updated_at = now()
			WHERE project_id = $1 AND id = $2
		`, project.ID, current.ID)
	}
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := scanStoryboardShot(tx.QueryRow(r.Context(), storyboardShotSelectSQL(`
		WHERE s.project_id = $1 AND s.id = $2
	`), project.ID, current.ID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := production.MarkFinalVideoStale(r.Context(), tx, project.ID, current.WorkflowRunID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "storyboard.shot.media.unlinked", "storyboard_shot", item.ID, mustRawJSON(map[string]any{
		"shotId": item.ID,
		"kind":   kind,
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) updateShotAssetRequirement(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	current, err := s.shotAssetRequirement(r, project.ID, r.PathValue("requirementId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req struct {
		Costume        *string `json:"costume"`
		Pose           *string `json:"pose"`
		Expression     *string `json:"expression"`
		Action         *string `json:"action"`
		CameraRelation *string `json:"cameraRelation"`
		SceneState     *string `json:"sceneState"`
		PropState      *string `json:"propState"`
		Prompt         *string `json:"prompt"`
	}
	if !decode(w, r, &req) {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var updatedID string
	if err := tx.QueryRow(r.Context(), `
		UPDATE shot_asset_requirements
		SET costume = CASE WHEN $3 THEN NULLIF($4, '') ELSE costume END,
		    pose = CASE WHEN $5 THEN NULLIF($6, '') ELSE pose END,
		    expression = CASE WHEN $7 THEN NULLIF($8, '') ELSE expression END,
		    action = CASE WHEN $9 THEN NULLIF($10, '') ELSE action END,
		    camera_relation = CASE WHEN $11 THEN NULLIF($12, '') ELSE camera_relation END,
		    scene_state = CASE WHEN $13 THEN NULLIF($14, '') ELSE scene_state END,
		    prop_state = CASE WHEN $15 THEN NULLIF($16, '') ELSE prop_state END,
		    prompt = CASE WHEN $17 THEN NULLIF($18, '') ELSE prompt END,
		    review_status = 'pending',
		    manual_override = true,
		    stale_state = 'needs_regeneration',
		    edited_by = $19,
		    edited_at = now(),
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
		RETURNING id
	`, project.ID, current.ID,
		req.Costume != nil, trimPtr(req.Costume),
		req.Pose != nil, trimPtr(req.Pose),
		req.Expression != nil, trimPtr(req.Expression),
		req.Action != nil, trimPtr(req.Action),
		req.CameraRelation != nil, trimPtr(req.CameraRelation),
		req.SceneState != nil, trimPtr(req.SceneState),
		req.PropState != nil, trimPtr(req.PropState),
		req.Prompt != nil, trimPtr(req.Prompt),
		principal.UserID).Scan(&updatedID); err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := scanShotAssetRequirement(tx.QueryRow(r.Context(), shotAssetRequirementSelectSQL(`
		WHERE r.project_id = $1 AND r.id = $2
	`), project.ID, updatedID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := production.MarkRequirementDownstreamStale(r.Context(), tx, project.ID, current.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := production.MarkFinalVideoStale(r.Context(), tx, project.ID, stringValue(current.WorkflowRunID)); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "shot_asset_requirement.updated", "shot_asset_requirement", item.ID, mustRawJSON(map[string]any{
		"requirementId":  item.ID,
		"shotId":         item.StoryboardShotID,
		"manualOverride": item.ManualOverride,
		"staleState":     item.StaleState,
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) skipShotAssetRequirement(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	current, err := s.shotAssetRequirement(r, project.ID, r.PathValue("requirementId"))
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
	if _, err := tx.Exec(r.Context(), `
		UPDATE shot_asset_requirements
		SET status = 'skipped',
		    review_status = 'approved',
		    stale_state = 'fresh',
		    manual_override = true,
		    edited_by = $3,
		    edited_at = now(),
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
	`, project.ID, current.ID, principal.UserID); err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := scanShotAssetRequirement(tx.QueryRow(r.Context(), shotAssetRequirementSelectSQL(`
		WHERE r.project_id = $1 AND r.id = $2
	`), project.ID, current.ID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := production.MarkFinalVideoStale(r.Context(), tx, project.ID, stringValue(current.WorkflowRunID)); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "shot_asset_requirement.skipped", "shot_asset_requirement", item.ID, mustRawJSON(map[string]any{
		"requirementId": item.ID,
		"shotId":        item.StoryboardShotID,
		"status":        item.Status,
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func trimPtr(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func int64PtrValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
