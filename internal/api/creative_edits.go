package api

import (
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/production"
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
		Visual          *string  `json:"visual"`
		Camera          *string  `json:"camera"`
		Motion          *string  `json:"motion"`
		Mood            *string  `json:"mood"`
		DurationSeconds *float64 `json:"durationSeconds"`
		ImagePrompt     *string  `json:"imagePrompt"`
		VideoPrompt     *string  `json:"videoPrompt"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.DurationSeconds != nil && *req.DurationSeconds <= 0 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "durationSeconds must be greater than zero", nil, false)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	item, err := scanStoryboardShot(tx.QueryRow(r.Context(), `
		UPDATE storyboard_shots
		SET visual = CASE WHEN $3 THEN NULLIF($4, '') ELSE visual END,
		    camera = CASE WHEN $5 THEN NULLIF($6, '') ELSE camera END,
		    motion = CASE WHEN $7 THEN NULLIF($8, '') ELSE motion END,
		    mood = CASE WHEN $9 THEN NULLIF($10, '') ELSE mood END,
		    duration_seconds = CASE WHEN $11 THEN $12::numeric ELSE duration_seconds END,
		    image_prompt = CASE WHEN $13 THEN NULLIF($14, '') ELSE image_prompt END,
		    video_prompt = CASE WHEN $15 THEN NULLIF($16, '') ELSE video_prompt END,
		    review_status = 'pending',
		    manual_override = true,
		    stale_state = 'needs_regeneration',
		    edited_by = $17,
		    edited_at = now(),
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
		RETURNING
			id,
			COALESCE(workflow_run_id::text, ''),
			shot_index,
			COALESCE(shot_no, shot_index + 1),
			duration_seconds,
			COALESCE(visual, ''),
			COALESCE(camera, ''),
			COALESCE(motion, ''),
			COALESCE(mood, ''),
			COALESCE(image_prompt, ''),
			COALESCE(video_prompt, ''),
			image_artifact_id,
			image_media_file_id,
			image_storage_key,
			NULL,
			NULL,
			video_artifact_id,
			video_media_file_id,
			video_storage_key,
			NULL,
			NULL,
			video_provider_async_task_id,
			video_external_task_id,
			COALESCE(status, 'pending'),
			COALESCE(review_status, 'pending'),
			COALESCE(manual_override, false),
			COALESCE(stale_state, 'fresh'),
			edited_by,
			edited_at
	`, project.ID, current.ID,
		req.Visual != nil, trimPtr(req.Visual),
		req.Camera != nil, trimPtr(req.Camera),
		req.Motion != nil, trimPtr(req.Motion),
		req.Mood != nil, trimPtr(req.Mood),
		req.DurationSeconds != nil, floatPtrValue(req.DurationSeconds),
		req.ImagePrompt != nil, trimPtr(req.ImagePrompt),
		req.VideoPrompt != nil, trimPtr(req.VideoPrompt),
		principal.UserID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := production.MarkShotDownstreamStale(r.Context(), tx, project.ID, current.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := production.MarkFinalVideoStale(r.Context(), tx, project.ID, current.WorkflowRunID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "storyboard.shot.updated", "storyboard_shot", item.ID, mustRawJSON(map[string]any{
		"shotId":         item.ID,
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

func trimPtr(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func floatPtrValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}
