package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/provider"
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
	episodeRefs, err := workflows.LoadScriptSceneEpisodeRefs(r.Context(), s.db, project.ID, script.ID, version.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	rendered, gatewayResp, err := s.runTextGatewayPrompt(r, project, "script_scene_parser", map[string]any{
		"project": projectPromptVariables(project),
		"script":  map[string]any{"id": script.ID, "versionId": version.ID, "title": script.Title, "content": version.Content, "episodes": string(mustRawJSON(episodeRefs))},
	}, true, authz.PermissionScriptWrite, provider.BillingContextReasonManualProvider)
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
		ScriptEpisodeIDs:  workflows.ScriptSceneEpisodeIDMap(episodeRefs),
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
		  AND deleted_at IS NULL
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
		ExpectedRevision int64  `json:"expectedRevision"`
		IdempotencyKey   string `json:"idempotencyKey"`
		scriptScenePatch
	}
	if !decode(w, r, &req) {
		return
	}
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	actionInput := mustRawJSON(map[string]any{
		"sceneId": r.PathValue("sceneId"), "expectedRevision": req.ExpectedRevision, "patch": req.scriptScenePatch,
	})
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "script_scene.update", actionInput, idempotencyKey(r, req.IdempotencyKey),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	encoded, err := json.Marshal(result.Data["scene"])
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var item workflows.ScriptSceneRecord
	if err := json.Unmarshal(encoded, &item); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) reviewScriptScene(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req struct {
		ReviewRequest
		ExpectedRevision int64  `json:"expectedRevision"`
		IdempotencyKey   string `json:"idempotencyKey"`
	}
	if !decode(w, r, &req) {
		return
	}
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	actionInput := mustRawJSON(map[string]any{
		"sceneId": r.PathValue("sceneId"), "expectedRevision": req.ExpectedRevision,
		"reviewStatus": req.ReviewStatus, "note": req.Note,
	})
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "script_scene.review", actionInput, idempotencyKey(r, req.IdempotencyKey),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	encoded, err := json.Marshal(result.Data)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var outcome scriptSceneReviewActionOutcome
	if err := json.Unmarshal(encoded, &outcome); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, outcome, nil)
}

func (s *Server) deleteScriptScene(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req struct {
		ExpectedRevision int64  `json:"expectedRevision"`
		IdempotencyKey   string `json:"idempotencyKey"`
		Reason           string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	actionInput := mustRawJSON(map[string]any{
		"sceneId": r.PathValue("sceneId"), "expectedRevision": req.ExpectedRevision, "reason": req.Reason,
	})
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "script_scene.delete", actionInput, idempotencyKey(r, req.IdempotencyKey),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, result.Data, nil)
}

func (s *Server) scriptScene(r *http.Request, projectID, sceneID string) (workflows.ScriptSceneRecord, error) {
	return workflows.ScanScriptSceneRecord(s.db.QueryRow(r.Context(), workflows.ScriptSceneSelectSQL(`
		WHERE project_id = $1 AND id = $2 AND deleted_at IS NULL
	`), projectID, sceneID))
}

type scriptSceneExecer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func markScriptSceneDownstreamStale(ctx context.Context, db scriptSceneExecer, projectID, sceneID string) error {
	if _, err := db.Exec(ctx, `
		UPDATE scene_asset_links
		SET metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		  'staleState', 'upstream_changed',
		  'staleReason', 'script_scene_updated'
		)
		WHERE project_id = $1 AND script_scene_id = $2
	`, projectID, sceneID); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `
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
	if _, err := db.Exec(ctx, `
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
	_, err := db.Exec(ctx, `
		UPDATE storyboard_shots
		SET stale_state = 'needs_regeneration',
		    image_prompt_status = 'not_started',
		    image_prompt_error_code = NULL,
		    image_prompt_error_message = NULL,
		    image_prompt_workflow_run_id = NULL,
		    image_prompt_updated_at = now(),
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
