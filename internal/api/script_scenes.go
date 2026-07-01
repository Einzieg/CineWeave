package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/production"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/jackc/pgx/v5/pgconn"
)

type ParseScriptScenesRequest struct {
	Force bool `json:"force"`
}

type ParseScriptScenesResponse struct {
	ScriptID       string                        `json:"scriptId"`
	VersionID      string                        `json:"versionId"`
	SceneCount     int                           `json:"sceneCount"`
	Scenes         []workflows.ScriptSceneRecord `json:"scenes"`
	ProviderCallID string                        `json:"providerCallId,omitempty"`
	ModelID        string                        `json:"modelId,omitempty"`
}

func (s *Server) parseScriptScenes(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req ParseScriptScenesRequest
	if !decode(w, r, &req) {
		return
	}
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	script, err := s.script(r, project.ID, r.PathValue("scriptId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	version, err := s.scriptVersion(r, project.ID, script.ID, r.PathValue("versionId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	rendered, gatewayResp, err := s.runTextGatewayPrompt(r, project, "script_scene_parser", map[string]any{
		"project": projectPromptVariables(project),
		"script":  map[string]any{"id": script.ID, "versionId": version.ID, "title": script.Title, "content": version.Content},
	}, true)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	candidates, err := workflows.NormalizeScriptSceneParser(gatewayResp.Output.Text)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "INVALID_SCRIPT_SCENE_JSON", err.Error(), nil, false)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	scenes, err := workflows.StoreScriptScenes(r.Context(), tx, workflows.ScriptSceneStoreInput{
		OrganizationID:    project.OrganizationID,
		ProjectID:         project.ID,
		ScriptID:          script.ID,
		ScriptVersionID:   version.ID,
		CreatedBy:         principal.UserID,
		Force:             req.Force,
		ProviderCallID:    gatewayResp.ProviderCallID,
		ModelID:           gatewayResp.ModelID,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		Source:            "script_scene_parser",
	}, candidates)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "script.scenes.parsed", "script_version", version.ID, mustRawJSON(map[string]any{
		"scriptId":        script.ID,
		"scriptVersionId": version.ID,
		"sceneCount":      len(scenes),
		"force":           req.Force,
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, ParseScriptScenesResponse{
		ScriptID:       script.ID,
		VersionID:      version.ID,
		SceneCount:     len(scenes),
		Scenes:         scenes,
		ProviderCallID: gatewayResp.ProviderCallID,
		ModelID:        gatewayResp.ModelID,
	}, nil)
}

func (s *Server) listScriptScenes(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead)
	if !ok {
		return
	}
	script, err := s.script(r, project.ID, r.PathValue("scriptId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	versionID := firstNonEmpty(strings.TrimSpace(r.URL.Query().Get("scriptVersionId")), strings.TrimSpace(r.URL.Query().Get("filter[scriptVersionId]")))
	reviewStatus := firstNonEmpty(strings.TrimSpace(r.URL.Query().Get("reviewStatus")), strings.TrimSpace(r.URL.Query().Get("filter[reviewStatus]")))
	rows, err := s.db.Query(r.Context(), workflows.ScriptSceneSelectSQL(`
		WHERE project_id = $1
		  AND script_id = $2
		  AND ($3 = '' OR script_version_id = $3::uuid)
		  AND ($4 = '' OR review_status = $4)
		ORDER BY scene_index ASC
	`), project.ID, script.ID, versionID, reviewStatus)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]workflows.ScriptSceneRecord, 0)
	for rows.Next() {
		item, err := workflows.ScanScriptSceneRecord(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) getScriptScene(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead)
	if !ok {
		return
	}
	item, err := s.scriptScene(r, project.ID, r.PathValue("sceneId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) updateScriptScene(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req struct {
		Title         *string   `json:"title"`
		Summary       *string   `json:"summary"`
		Location      *string   `json:"location"`
		TimeOfDay     *string   `json:"timeOfDay"`
		Atmosphere    *string   `json:"atmosphere"`
		Characters    *[]string `json:"characters"`
		Scenes        *[]string `json:"scenes"`
		Props         *[]string `json:"props"`
		Action        *string   `json:"action"`
		Dialogue      *string   `json:"dialogue"`
		VisualGoal    *string   `json:"visualGoal"`
		EmotionalTone *string   `json:"emotionalTone"`
		Conflict      *string   `json:"conflict"`
		Outcome       *string   `json:"outcome"`
		Content       *string   `json:"content"`
	}
	if !decode(w, r, &req) {
		return
	}
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	current, err := s.scriptScene(r, project.ID, r.PathValue("sceneId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if req.Title != nil {
		current.Title = strings.TrimSpace(*req.Title)
	}
	if current.Title == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "title is required", nil, false)
		return
	}
	if req.Summary != nil {
		current.Summary = strings.TrimSpace(*req.Summary)
	}
	if req.Location != nil {
		current.Location = strings.TrimSpace(*req.Location)
	}
	if req.TimeOfDay != nil {
		current.TimeOfDay = strings.TrimSpace(*req.TimeOfDay)
	}
	if req.Atmosphere != nil {
		current.Atmosphere = strings.TrimSpace(*req.Atmosphere)
	}
	if req.Action != nil {
		current.Action = strings.TrimSpace(*req.Action)
	}
	if req.Dialogue != nil {
		current.Dialogue = strings.TrimSpace(*req.Dialogue)
	}
	if req.VisualGoal != nil {
		current.VisualGoal = strings.TrimSpace(*req.VisualGoal)
	}
	if req.EmotionalTone != nil {
		current.EmotionalTone = strings.TrimSpace(*req.EmotionalTone)
	}
	if req.Conflict != nil {
		current.Conflict = strings.TrimSpace(*req.Conflict)
	}
	if req.Outcome != nil {
		current.Outcome = strings.TrimSpace(*req.Outcome)
	}
	if req.Content != nil {
		current.Content = strings.TrimSpace(*req.Content)
	}
	if req.Characters != nil {
		current.Characters = json.RawMessage(mustMarshal(normalizeStringSlice(*req.Characters)))
	}
	if req.Scenes != nil {
		current.Scenes = json.RawMessage(mustMarshal(normalizeStringSlice(*req.Scenes)))
	}
	if req.Props != nil {
		current.Props = json.RawMessage(mustMarshal(normalizeStringSlice(*req.Props)))
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	item, err := workflows.ScanScriptSceneRecord(tx.QueryRow(r.Context(), `
		UPDATE script_scenes
		SET title = $3,
		    summary = NULLIF($4, ''),
		    location = NULLIF($5, ''),
		    time_of_day = NULLIF($6, ''),
		    atmosphere = NULLIF($7, ''),
		    characters = $8,
		    scenes = $9,
		    props = $10,
		    action = NULLIF($11, ''),
		    dialogue = NULLIF($12, ''),
		    visual_goal = NULLIF($13, ''),
		    emotional_tone = NULLIF($14, ''),
		    conflict = NULLIF($15, ''),
		    outcome = NULLIF($16, ''),
		    content = $17,
		    review_status = 'pending',
		    manual_override = true,
		    stale_state = 'needs_regeneration',
		    edited_by = $18,
		    edited_at = now(),
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
		RETURNING `+workflows.ScriptSceneColumns()+`
	`, project.ID, current.ID, current.Title, current.Summary, current.Location, current.TimeOfDay, current.Atmosphere,
		current.Characters, current.Scenes, current.Props, current.Action, current.Dialogue, current.VisualGoal,
		current.EmotionalTone, current.Conflict, current.Outcome, current.Content, principal.UserID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := markScriptSceneDownstreamStale(r, tx, project.ID, item.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := production.MarkFinalVideoStale(r.Context(), tx, project.ID, ""); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "script.scene.updated", "script_scene", item.ID, mustRawJSON(map[string]any{
		"scriptSceneId":  item.ID,
		"scriptId":       item.ScriptID,
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

func (s *Server) reviewScriptScene(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req ReviewRequest
	if !decode(w, r, &req) {
		return
	}
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	status := strings.TrimSpace(req.ReviewStatus)
	if !validReviewStatus(status) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "reviewStatus is invalid", nil, false)
		return
	}
	current, err := s.scriptScene(r, project.ID, r.PathValue("sceneId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	note := strings.TrimSpace(req.Note)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var resp ReviewResponse
	if err := tx.QueryRow(r.Context(), `
		UPDATE script_scenes
		SET review_status = $3,
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'reviewStatus', $3,
		      'reviewNote', $4,
		      'reviewedBy', $5,
		      'reviewedAt', now()
		    ),
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
		RETURNING id, review_status, updated_at
	`, project.ID, current.ID, status, note, principal.UserID).Scan(&resp.ID, &resp.ReviewStatus, &resp.UpdatedAt); err != nil {
		s.writeError(w, r, err)
		return
	}
	resp.Note = stringPtrFromValue(note)
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "script.scene.reviewed", "script_scene", current.ID, mustRawJSON(map[string]any{
		"scriptSceneId": current.ID,
		"scriptId":      current.ScriptID,
		"reviewStatus":  status,
		"note":          note,
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, resp, nil)
}

func (s *Server) scriptScene(r *http.Request, projectID, sceneID string) (workflows.ScriptSceneRecord, error) {
	return workflows.ScanScriptSceneRecord(s.db.QueryRow(r.Context(), workflows.ScriptSceneSelectSQL(`
		WHERE project_id = $1 AND id = $2
	`), projectID, sceneID))
}

type scriptSceneExecer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func markScriptSceneDownstreamStale(r *http.Request, db scriptSceneExecer, projectID, sceneID string) error {
	if _, err := db.Exec(r.Context(), `
		UPDATE scene_asset_links
		SET metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		  'staleState', 'upstream_changed',
		  'staleReason', 'script_scene_updated'
		)
		WHERE project_id = $1 AND script_scene_id = $2
	`, projectID, sceneID); err != nil {
		return err
	}
	if _, err := db.Exec(r.Context(), `
		UPDATE canonical_assets
		SET stale_state = 'upstream_changed', updated_at = now()
		WHERE project_id = $1
		  AND id IN (
		    SELECT asset_id
		    FROM scene_asset_links
		    WHERE project_id = $1 AND script_scene_id = $2
		  )
	`, projectID, sceneID); err != nil {
		return err
	}
	if _, err := db.Exec(r.Context(), `
		UPDATE shot_asset_requirements r
		SET stale_state = 'upstream_changed', updated_at = now()
		FROM storyboard_shots s
		WHERE r.storyboard_shot_id = s.id
		  AND r.project_id = $1
		  AND s.script_scene_id = $2
		  AND s.deleted_at IS NULL
	`, projectID, sceneID); err != nil {
		return err
	}
	_, err := db.Exec(r.Context(), `
		UPDATE storyboard_shots
		SET stale_state = 'needs_regeneration',
		    image_status = CASE
		      WHEN image_artifact_id IS NOT NULL OR image_media_file_id IS NOT NULL OR COALESCE(image_storage_key, '') <> '' THEN 'stale'
		      ELSE image_status
		    END,
		    video_status = CASE
		      WHEN video_artifact_id IS NOT NULL OR video_media_file_id IS NOT NULL OR COALESCE(video_storage_key, '') <> '' THEN 'stale'
		      ELSE video_status
		    END,
		    updated_at = now()
		WHERE project_id = $1 AND script_scene_id = $2 AND deleted_at IS NULL
	`, projectID, sceneID)
	return err
}
