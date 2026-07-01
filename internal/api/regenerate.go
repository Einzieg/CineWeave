package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/jackc/pgx/v5"
)

type RegenerateRequest struct {
	TargetType string         `json:"targetType"`
	TargetID   string         `json:"targetId"`
	Options    map[string]any `json:"options"`
}

type RegenerateResponse struct {
	TargetType    string `json:"targetType"`
	TargetID      string `json:"targetId"`
	WorkflowRunID string `json:"workflowRunId"`
	Status        string `json:"status"`
	WorkflowType  string `json:"workflowType"`
}

func (s *Server) regenerateCreativeObject(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req RegenerateRequest
	if !decode(w, r, &req) {
		return
	}
	targetType := strings.TrimSpace(req.TargetType)
	targetID := strings.TrimSpace(req.TargetID)
	workflowType, workflowFunc, permissions, ok := regenerationWorkflow(targetType)
	if !ok {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "regeneration targetType is not supported", nil, false)
		return
	}
	project, err := s.project(r, r.PathValue("projectId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorizeAny(w, r, principal, permissions, authz.Resource{ProjectID: project.ID}) {
		return
	}
	resolvedTargetID, ok := s.requireRegenerationTarget(w, r, project.ID, targetType, targetID)
	if !ok {
		return
	}
	options := map[string]any{
		"targetId":    resolvedTargetID,
		"force":       true,
		"aspectRatio": firstNonEmpty(project.VideoRatio, stringValue(project.AspectRatio), "16:9"),
		"resolution":  "720p",
	}
	for key, value := range req.Options {
		options[key] = value
	}
	options["targetId"] = resolvedTargetID
	run, ok := s.startProjectWorkflow(w, r, principal, project, workflowType, options, workflowFunc)
	if !ok {
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, RegenerateResponse{
		TargetType:    targetType,
		TargetID:      resolvedTargetID,
		WorkflowRunID: run.ID,
		Status:        run.Status,
		WorkflowType:  workflowType,
	}, nil)
}

func (s *Server) requireRegenerationTarget(w http.ResponseWriter, r *http.Request, projectID, targetType, targetID string) (string, bool) {
	switch targetType {
	case "canonical_asset_image":
		if targetID == "" {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "targetId is required", nil, false)
			return "", false
		}
		if _, err := s.canonicalAsset(r, projectID, targetID); err != nil {
			s.writeError(w, r, err)
			return "", false
		}
		return targetID, true
	case "derived_asset_image":
		if targetID == "" {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "targetId is required", nil, false)
			return "", false
		}
		if _, err := s.shotAssetRequirement(r, projectID, targetID); err != nil {
			s.writeError(w, r, err)
			return "", false
		}
		return targetID, true
	case "shot_image", "shot_video":
		if targetID == "" {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "targetId is required", nil, false)
			return "", false
		}
		if _, err := s.storyboardShotByID(r, projectID, targetID); err != nil {
			s.writeError(w, r, err)
			return "", false
		}
		return targetID, true
	case "script_scene", "scene_storyboard":
		if targetID == "" {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "targetId is required", nil, false)
			return "", false
		}
		if _, err := s.scriptScene(r, projectID, targetID); err != nil {
			s.writeError(w, r, err)
			return "", false
		}
		return targetID, true
	case "final_video":
		if targetID != "" {
			var exists bool
			if err := s.db.QueryRow(r.Context(), `
				SELECT EXISTS(SELECT 1 FROM workflow_runs WHERE project_id = $1 AND id = $2)
			`, projectID, targetID).Scan(&exists); err != nil {
				s.writeError(w, r, err)
				return "", false
			}
			if !exists {
				httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "source workflow run was not found", nil, false)
				return "", false
			}
			return targetID, true
		}
		latest, err := s.latestVideoSourceWorkflowRun(r, projectID)
		if err != nil {
			s.writeError(w, r, err)
			return "", false
		}
		if latest == "" {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "no succeeded shot videos are available to compose", nil, false)
			return "", false
		}
		return latest, true
	default:
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "regeneration targetType is not supported", nil, false)
		return "", false
	}
}

func (s *Server) latestVideoSourceWorkflowRun(r *http.Request, projectID string) (string, error) {
	var workflowRunID sql.NullString
	err := s.db.QueryRow(r.Context(), `
		SELECT workflow_run_id::text
		FROM storyboard_shots
		WHERE project_id = $1
		  AND workflow_run_id IS NOT NULL
		  AND status = 'video_succeeded'
		ORDER BY updated_at DESC
		LIMIT 1
	`, projectID).Scan(&workflowRunID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !workflowRunID.Valid {
		return "", nil
	}
	return workflowRunID.String, nil
}

func regenerationWorkflow(targetType string) (string, any, []string, bool) {
	switch targetType {
	case "canonical_asset_image":
		return "regenerate_canonical_asset_image", workflows.RegenerateCanonicalAssetImageWorkflow, []string{authz.PermissionAssetGenerate}, true
	case "derived_asset_image":
		return "regenerate_derived_asset_image", workflows.RegenerateDerivedAssetImageWorkflow, []string{authz.PermissionAssetGenerate}, true
	case "shot_image":
		return "regenerate_shot_image", workflows.RegenerateShotImageWorkflow, []string{authz.PermissionStoryboardGenerate, authz.PermissionWorkflowRun}, true
	case "shot_video":
		return "regenerate_shot_video", workflows.RegenerateShotVideoWorkflow, []string{authz.PermissionWorkflowRun}, true
	case "final_video":
		return "regenerate_final_video", workflows.RegenerateFinalVideoWorkflow, []string{authz.PermissionWorkflowRun}, true
	case "script_scene":
		return "regenerate_script_scene", workflows.RegenerateScriptSceneWorkflow, []string{authz.PermissionScriptWrite}, true
	case "scene_storyboard":
		return "regenerate_scene_storyboard", workflows.RegenerateSceneStoryboardWorkflow, []string{authz.PermissionStoryboardGenerate, authz.PermissionScriptWrite}, true
	default:
		return "", nil, nil, false
	}
}

func (s *Server) authorizeAny(w http.ResponseWriter, r *http.Request, principal auth.Principal, permissions []string, resource authz.Resource) bool {
	var lastErr error
	for _, permission := range permissions {
		if err := s.authorizer.Authorize(r.Context(), principal, permission, resource); err == nil {
			return true
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		s.writeError(w, r, lastErr)
		return false
	}
	httpx.WriteError(w, r, http.StatusForbidden, "ACCESS_DENIED", "access denied", nil, false)
	return false
}
