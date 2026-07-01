package api

import (
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/workflows"
)

type ShotProductionStatus struct {
	ProjectID string                `json:"projectId"`
	Summary   ShotProductionSummary `json:"summary"`
	Shots     []ShotProductionShot  `json:"shots"`
}

type ShotProductionSummary struct {
	Total          int `json:"total"`
	ImageSucceeded int `json:"imageSucceeded"`
	ImageMissing   int `json:"imageMissing"`
	ImageFailed    int `json:"imageFailed"`
	ImageStale     int `json:"imageStale"`
	VideoSucceeded int `json:"videoSucceeded"`
	VideoMissing   int `json:"videoMissing"`
	VideoFailed    int `json:"videoFailed"`
	VideoStale     int `json:"videoStale"`
	Running        int `json:"running"`
}

type ShotProductionShot struct {
	ID                  string  `json:"id"`
	WorkflowRunID       string  `json:"workflowRunId"`
	ScriptSceneID       *string `json:"scriptSceneId,omitempty"`
	ShotIndex           int     `json:"shotIndex"`
	ShotNo              int     `json:"shotNo"`
	Visual              string  `json:"visual,omitempty"`
	ImageStatus         string  `json:"imageStatus"`
	VideoStatus         string  `json:"videoStatus"`
	StaleState          string  `json:"staleState"`
	ImageArtifactID     *string `json:"imageArtifactId,omitempty"`
	ImageMediaFileID    *string `json:"imageMediaFileId,omitempty"`
	ImageStorageKey     *string `json:"imageStorageKey,omitempty"`
	ImagePreviewURL     *string `json:"imagePreviewUrl,omitempty"`
	VideoArtifactID     *string `json:"videoArtifactId,omitempty"`
	VideoMediaFileID    *string `json:"videoMediaFileId,omitempty"`
	VideoStorageKey     *string `json:"videoStorageKey,omitempty"`
	VideoPreviewURL     *string `json:"videoPreviewUrl,omitempty"`
	ImageErrorCode      *string `json:"imageErrorCode,omitempty"`
	ImageErrorMessage   *string `json:"imageErrorMessage,omitempty"`
	VideoErrorCode      *string `json:"videoErrorCode,omitempty"`
	VideoErrorMessage   *string `json:"videoErrorMessage,omitempty"`
	ImageWorkflowRunID  *string `json:"imageWorkflowRunId,omitempty"`
	VideoWorkflowRunID  *string `json:"videoWorkflowRunId,omitempty"`
	ProviderAsyncTaskID *string `json:"providerAsyncTaskId,omitempty"`
	ExternalTaskID      *string `json:"externalTaskId,omitempty"`
	CanGenerateImage    bool    `json:"canGenerateImage"`
	CanGenerateVideo    bool    `json:"canGenerateVideo"`
	CanRetryImage       bool    `json:"canRetryImage"`
	CanRetryVideo       bool    `json:"canRetryVideo"`
}

type ShotProductionActionRequest struct {
	Action        string         `json:"action"`
	ScriptSceneID string         `json:"scriptSceneId"`
	WorkflowRunID string         `json:"workflowRunId"`
	ShotIDs       []string       `json:"shotIds"`
	Options       map[string]any `json:"options"`
}

type ShotProductionActionResponse struct {
	Action        string   `json:"action"`
	WorkflowRunID string   `json:"workflowRunId"`
	Status        string   `json:"status"`
	WorkflowType  string   `json:"workflowType"`
	TargetShotIDs []string `json:"targetShotIds"`
}

func (s *Server) getShotProductionStatus(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	status, err := s.loadShotProductionStatus(r, project.ID, r.URL.Query().Get("scriptSceneId"), r.URL.Query().Get("workflowRunId"), strings.EqualFold(r.URL.Query().Get("includePreviewUrl"), "true"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, status, nil)
}

func (s *Server) runShotProductionAction(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req ShotProductionActionRequest
	if !decode(w, r, &req) {
		return
	}
	req.Action = strings.TrimSpace(req.Action)
	if req.Options == nil {
		req.Options = map[string]any{}
	}
	if _, _, ok := shotProductionWorkflowForAction(req.Action); !ok {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "shot production action is not supported", nil, false)
		return
	}
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowRun)
	if !ok {
		return
	}
	status, err := s.loadShotProductionStatus(r, project.ID, req.ScriptSceneID, req.WorkflowRunID, false)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	targets, errorCode := selectShotProductionTargets(req, status.Shots)
	if errorCode != "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, errorCode, shotProductionActionErrorMessage(errorCode), nil, false)
		return
	}
	workflowType, workflowFunc, _ := shotProductionWorkflowForAction(req.Action)
	input := map[string]any{
		"action":      req.Action,
		"shotIds":     targets,
		"force":       shotProductionOptionBool(req.Options, "force", true),
		"aspectRatio": firstNonEmptyString(shotProductionOptionString(req.Options, "aspectRatio"), project.VideoRatio, stringValue(project.AspectRatio), "16:9"),
		"resolution":  firstNonEmptyString(shotProductionOptionString(req.Options, "resolution"), "720p"),
	}
	if value := shotProductionOptionFloat(req.Options, "duration", 0); value > 0 {
		input["duration"] = value
	}
	if value := shotProductionOptionInt(req.Options, "maxConcurrency", 1); value > 0 {
		input["maxConcurrency"] = value
	}
	if value := shotProductionOptionInt(req.Options, "pollIntervalSeconds", 0); value > 0 {
		input["pollIntervalSeconds"] = value
	}
	if value := shotProductionOptionInt(req.Options, "maxPolls", 0); value > 0 {
		input["maxPolls"] = value
	}
	run, ok := s.startProjectWorkflow(w, r, principal, project, workflowType, input, workflowFunc)
	if !ok {
		return
	}
	if err := s.markShotProductionQueued(r, req.Action, run.ID, targets); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, ShotProductionActionResponse{
		Action:        req.Action,
		WorkflowRunID: run.ID,
		Status:        run.Status,
		WorkflowType:  workflowType,
		TargetShotIDs: targets,
	}, nil)
}

func (s *Server) loadShotProductionStatus(r *http.Request, projectID, scriptSceneID, workflowRunID string, includePreviewURL bool) (ShotProductionStatus, error) {
	rows, err := s.db.Query(r.Context(), `
		`+storyboardShotSelectSQL(`
		WHERE s.project_id = $1
		  AND s.deleted_at IS NULL
		  AND ($2 = '' OR s.script_scene_id = $2::uuid)
		  AND ($3 = '' OR s.workflow_run_id = $3::uuid)
		ORDER BY COALESCE(sc.scene_no, 0), s.shot_index ASC
	`), projectID, strings.TrimSpace(scriptSceneID), strings.TrimSpace(workflowRunID))
	if err != nil {
		return ShotProductionStatus{}, err
	}
	defer rows.Close()
	status := ShotProductionStatus{ProjectID: projectID, Shots: make([]ShotProductionShot, 0)}
	for rows.Next() {
		shot, err := scanStoryboardShot(rows)
		if err != nil {
			return ShotProductionStatus{}, err
		}
		if includePreviewURL && s.storage != nil {
			if err := s.attachShotPreviewURLs(r, &shot, previewURLExpiryFromRequest(r)); err != nil {
				return ShotProductionStatus{}, err
			}
		}
		item := shotProductionShotFromStoryboard(shot)
		status.Summary.add(item)
		status.Shots = append(status.Shots, item)
	}
	return status, rows.Err()
}

func shotProductionShotFromStoryboard(shot StoryboardShot) ShotProductionShot {
	hasImage := shotHasImage(shot)
	return ShotProductionShot{
		ID:                  shot.ID,
		WorkflowRunID:       shot.WorkflowRunID,
		ScriptSceneID:       shot.ScriptSceneID,
		ShotIndex:           shot.ShotIndex,
		ShotNo:              shot.ShotNo,
		Visual:              shot.Visual,
		ImageStatus:         shot.ImageStatus,
		VideoStatus:         shot.VideoStatus,
		StaleState:          shot.StaleState,
		ImageArtifactID:     shot.ImageArtifactID,
		ImageMediaFileID:    shot.ImageMediaFileID,
		ImageStorageKey:     shot.ImageStorageKey,
		ImagePreviewURL:     shot.ImagePreviewURL,
		VideoArtifactID:     shot.VideoArtifactID,
		VideoMediaFileID:    shot.VideoMediaFileID,
		VideoStorageKey:     shot.VideoStorageKey,
		VideoPreviewURL:     shot.VideoPreviewURL,
		ImageErrorCode:      shot.ImageErrorCode,
		ImageErrorMessage:   shot.ImageErrorMessage,
		VideoErrorCode:      shot.VideoErrorCode,
		VideoErrorMessage:   shot.VideoErrorMessage,
		ImageWorkflowRunID:  shot.ImageWorkflowRunID,
		VideoWorkflowRunID:  shot.VideoWorkflowRunID,
		ProviderAsyncTaskID: shot.VideoProviderAsyncTaskID,
		ExternalTaskID:      shot.VideoExternalTaskID,
		CanGenerateImage:    shot.ImageStatus == "not_started" || shot.ImageStatus == "stale",
		CanGenerateVideo:    hasImage && (shot.VideoStatus == "not_started" || shot.VideoStatus == "stale"),
		CanRetryImage:       shot.ImageStatus == "failed",
		CanRetryVideo:       hasImage && shot.VideoStatus == "failed",
	}
}

func (summary *ShotProductionSummary) add(shot ShotProductionShot) {
	summary.Total++
	running := false
	switch shot.ImageStatus {
	case "succeeded":
		summary.ImageSucceeded++
	case "failed":
		summary.ImageFailed++
	case "stale":
		summary.ImageStale++
	case "queued", "running":
		running = true
	default:
		summary.ImageMissing++
	}
	switch shot.VideoStatus {
	case "succeeded":
		summary.VideoSucceeded++
	case "failed":
		summary.VideoFailed++
	case "stale":
		summary.VideoStale++
	case "queued", "running":
		running = true
	default:
		summary.VideoMissing++
	}
	if running {
		summary.Running++
	}
}

func selectShotProductionTargets(req ShotProductionActionRequest, shots []ShotProductionShot) ([]string, string) {
	requested := cleanShotProductionIDs(req.ShotIDs)
	selectedActions := map[string]bool{
		"generate_selected_images": true,
		"generate_selected_videos": true,
	}
	shotByID := make(map[string]ShotProductionShot, len(shots))
	for _, shot := range shots {
		shotByID[shot.ID] = shot
	}
	if selectedActions[req.Action] {
		if len(requested) == 0 {
			return nil, "INVALID_SHOT_SELECTION"
		}
		for _, shotID := range requested {
			if _, ok := shotByID[shotID]; !ok {
				return nil, "INVALID_SHOT_SELECTION"
			}
		}
		if req.Action == "generate_selected_videos" {
			for _, shotID := range requested {
				if !shotProductionHasImage(shotByID[shotID]) {
					return nil, "SHOT_IMAGE_REQUIRED"
				}
			}
		}
		return requested, ""
	}
	if len(requested) > 0 {
		for _, shotID := range requested {
			if _, ok := shotByID[shotID]; !ok {
				return nil, "INVALID_SHOT_SELECTION"
			}
		}
	}
	out := make([]string, 0)
	for _, shot := range shots {
		if len(requested) > 0 && !containsString(requested, shot.ID) {
			continue
		}
		if shotProductionActionMatches(req.Action, shot) {
			out = append(out, shot.ID)
		}
	}
	if len(out) == 0 {
		return nil, "NO_TARGET_SHOTS"
	}
	return out, ""
}

func shotProductionActionMatches(action string, shot ShotProductionShot) bool {
	switch action {
	case "generate_missing_images":
		return shot.ImageStatus == "not_started" || shot.ImageStatus == "stale"
	case "regenerate_stale_images":
		return shot.ImageStatus == "stale"
	case "regenerate_failed_images":
		return shot.ImageStatus == "failed"
	case "generate_missing_videos":
		return shotProductionHasImage(shot) && (shot.VideoStatus == "not_started" || shot.VideoStatus == "stale")
	case "regenerate_stale_videos":
		return shotProductionHasImage(shot) && shot.VideoStatus == "stale"
	case "regenerate_failed_videos":
		return shotProductionHasImage(shot) && shot.VideoStatus == "failed"
	case "cancel_running_videos":
		return (shot.VideoStatus == "queued" || shot.VideoStatus == "running") && stringValue(shot.ProviderAsyncTaskID) != ""
	default:
		return false
	}
}

func shotProductionWorkflowForAction(action string) (string, any, bool) {
	switch action {
	case "generate_missing_images", "regenerate_stale_images", "regenerate_failed_images", "generate_selected_images":
		return "batch_generate_shot_images", workflows.BatchGenerateShotImagesWorkflow, true
	case "generate_missing_videos", "regenerate_stale_videos", "regenerate_failed_videos", "generate_selected_videos":
		return "batch_generate_shot_videos", workflows.BatchGenerateShotVideosWorkflow, true
	case "cancel_running_videos":
		return "batch_cancel_shot_videos", workflows.BatchCancelShotVideosWorkflow, true
	default:
		return "", nil, false
	}
}

func (s *Server) markShotProductionQueued(r *http.Request, action, workflowRunID string, shotIDs []string) error {
	if action == "cancel_running_videos" {
		return nil
	}
	switch {
	case strings.Contains(action, "_images"):
		_, err := s.db.Exec(r.Context(), `
			UPDATE storyboard_shots
			SET image_status = 'queued',
			    image_error_code = NULL,
			    image_error_message = NULL,
			    image_workflow_run_id = $1,
			    updated_at = now()
			WHERE id = ANY($2::uuid[])
		`, workflowRunID, shotIDs)
		return err
	case strings.Contains(action, "_videos"):
		_, err := s.db.Exec(r.Context(), `
			UPDATE storyboard_shots
			SET video_status = 'queued',
			    video_error_code = NULL,
			    video_error_message = NULL,
			    video_workflow_run_id = $1,
			    updated_at = now()
			WHERE id = ANY($2::uuid[])
		`, workflowRunID, shotIDs)
		return err
	default:
		return nil
	}
}

func shotProductionActionErrorMessage(code string) string {
	switch code {
	case "NO_TARGET_SHOTS":
		return "no target shots match the action"
	case "INVALID_SHOT_SELECTION":
		return "shot selection is invalid"
	case "SHOT_IMAGE_REQUIRED":
		return "shot image is required before video generation"
	default:
		return "shot production action is invalid"
	}
}

func shotHasImage(shot StoryboardShot) bool {
	return shot.ImageArtifactID != nil || shot.ImageMediaFileID != nil || stringValue(shot.ImageStorageKey) != ""
}

func shotProductionHasImage(shot ShotProductionShot) bool {
	return shot.ImageArtifactID != nil || shot.ImageMediaFileID != nil || stringValue(shot.ImageStorageKey) != ""
}

func cleanShotProductionIDs(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func shotProductionOptionString(options map[string]any, key string) string {
	if value, ok := options[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func shotProductionOptionBool(options map[string]any, key string, fallback bool) bool {
	if value, ok := options[key].(bool); ok {
		return value
	}
	return fallback
}

func shotProductionOptionInt(options map[string]any, key string, fallback int) int {
	switch value := options[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return fallback
	}
}

func shotProductionOptionFloat(options map[string]any, key string, fallback float64) float64 {
	switch value := options[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	default:
		return fallback
	}
}
