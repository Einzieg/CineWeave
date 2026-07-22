package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/jackc/pgx/v5"
)

type ShotProductionStatus struct {
	ProjectID   string                `json:"projectId"`
	AspectRatio string                `json:"aspectRatio"`
	Summary     ShotProductionSummary `json:"summary"`
	Shots       []ShotProductionShot  `json:"shots"`
}

type ShotProductionSummary struct {
	Total                int `json:"total"`
	ImageSucceeded       int `json:"imageSucceeded"`
	ImageMissing         int `json:"imageMissing"`
	ImageFailed          int `json:"imageFailed"`
	ImageStale           int `json:"imageStale"`
	ImagePromptSucceeded int `json:"imagePromptSucceeded"`
	ImagePromptMissing   int `json:"imagePromptMissing"`
	ImagePromptFailed    int `json:"imagePromptFailed"`
	ImagePromptRunning   int `json:"imagePromptRunning"`
	VideoSucceeded       int `json:"videoSucceeded"`
	VideoMissing         int `json:"videoMissing"`
	VideoFailed          int `json:"videoFailed"`
	VideoStale           int `json:"videoStale"`
	VideoPromptSucceeded int `json:"videoPromptSucceeded"`
	VideoPromptMissing   int `json:"videoPromptMissing"`
	VideoPromptFailed    int `json:"videoPromptFailed"`
	VideoPromptRunning   int `json:"videoPromptRunning"`
	Running              int `json:"running"`
}

type ShotProductionShot struct {
	ID                       string                             `json:"id"`
	WorkflowRunID            string                             `json:"workflowRunId"`
	StoryboardPlanID         *string                            `json:"storyboardPlanId,omitempty"`
	ScriptSceneID            *string                            `json:"scriptSceneId,omitempty"`
	ScriptEpisodeID          *string                            `json:"scriptEpisodeId,omitempty"`
	EpisodeIndex             *int                               `json:"episodeIndex,omitempty"`
	EpisodeShotIndex         *int                               `json:"episodeShotIndex,omitempty"`
	EpisodeTitle             string                             `json:"episodeTitle,omitempty"`
	ShotIndex                int                                `json:"shotIndex"`
	ShotNo                   int                                `json:"shotNo"`
	Title                    string                             `json:"title,omitempty"`
	DurationSeconds          *float64                           `json:"durationSeconds,omitempty"`
	Visual                   string                             `json:"visual,omitempty"`
	ImagePrompt              string                             `json:"imagePrompt,omitempty"`
	ImagePromptStatus        string                             `json:"imagePromptStatus"`
	ImagePromptErrorCode     *string                            `json:"imagePromptErrorCode,omitempty"`
	ImagePromptErrorMessage  *string                            `json:"imagePromptErrorMessage,omitempty"`
	ImagePromptWorkflowRunID *string                            `json:"imagePromptWorkflowRunId,omitempty"`
	VideoPrompt              string                             `json:"videoPrompt,omitempty"`
	ScriptDialogue           []workflows.StoryboardDialogueLine `json:"scriptDialogue"`
	VideoPromptStatus        string                             `json:"videoPromptStatus"`
	VideoPromptErrorCode     *string                            `json:"videoPromptErrorCode,omitempty"`
	VideoPromptErrorMessage  *string                            `json:"videoPromptErrorMessage,omitempty"`
	VideoPromptWorkflowRunID *string                            `json:"videoPromptWorkflowRunId,omitempty"`
	ImageStatus              string                             `json:"imageStatus"`
	VideoStatus              string                             `json:"videoStatus"`
	StaleState               string                             `json:"staleState"`
	ImageArtifactID          *string                            `json:"imageArtifactId,omitempty"`
	ImageMediaFileID         *string                            `json:"imageMediaFileId,omitempty"`
	ImageStorageKey          *string                            `json:"imageStorageKey,omitempty"`
	ImagePreviewURL          *string                            `json:"imagePreviewUrl,omitempty"`
	VideoArtifactID          *string                            `json:"videoArtifactId,omitempty"`
	VideoMediaFileID         *string                            `json:"videoMediaFileId,omitempty"`
	VideoStorageKey          *string                            `json:"videoStorageKey,omitempty"`
	VideoPreviewURL          *string                            `json:"videoPreviewUrl,omitempty"`
	VideoReferenceMode       string                             `json:"videoReferenceMode"`
	VideoReferenceKeys       []string                           `json:"videoReferenceKeys"`
	ImageErrorCode           *string                            `json:"imageErrorCode,omitempty"`
	ImageErrorMessage        *string                            `json:"imageErrorMessage,omitempty"`
	VideoErrorCode           *string                            `json:"videoErrorCode,omitempty"`
	VideoErrorMessage        *string                            `json:"videoErrorMessage,omitempty"`
	ImageWorkflowRunID       *string                            `json:"imageWorkflowRunId,omitempty"`
	VideoWorkflowRunID       *string                            `json:"videoWorkflowRunId,omitempty"`
	ProviderAsyncTaskID      *string                            `json:"providerAsyncTaskId,omitempty"`
	ExternalTaskID           *string                            `json:"externalTaskId,omitempty"`
	CanGenerateImage         bool                               `json:"canGenerateImage"`
	CanGenerateImagePrompt   bool                               `json:"canGenerateImagePrompt"`
	CanGenerateVideo         bool                               `json:"canGenerateVideo"`
	CanGenerateVideoPrompt   bool                               `json:"canGenerateVideoPrompt"`
	CanRetryImage            bool                               `json:"canRetryImage"`
	CanRetryVideo            bool                               `json:"canRetryVideo"`
}

type ShotProductionActionRequest struct {
	Action          string         `json:"action"`
	ScriptSceneID   string         `json:"scriptSceneId"`
	ScriptEpisodeID string         `json:"scriptEpisodeId"`
	WorkflowRunID   string         `json:"workflowRunId"`
	ShotIDs         []string       `json:"shotIds"`
	Options         map[string]any `json:"options"`
}

type ShotProductionActionResponse struct {
	Action        string   `json:"action"`
	WorkflowRunID string   `json:"workflowRunId"`
	Status        string   `json:"status"`
	WorkflowType  string   `json:"workflowType"`
	TargetShotIDs []string `json:"targetShotIds"`
}

type ShotProductionBatchRequest struct {
	ScriptEpisodeID     string   `json:"scriptEpisodeId"`
	WorkflowRunID       string   `json:"workflowRunId"`
	ShotIDs             []string `json:"shotIds"`
	Force               *bool    `json:"force,omitempty"`
	MaxConcurrency      int      `json:"maxConcurrency,omitempty"`
	Resolution          string   `json:"resolution,omitempty"`
	PollIntervalSeconds int      `json:"pollIntervalSeconds,omitempty"`
	MaxPolls            int      `json:"maxPolls,omitempty"`
}

func (s *Server) getShotProductionStatus(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	status, err := s.loadShotProductionStatusForEpisode(
		r,
		project.ID,
		r.URL.Query().Get("scriptSceneId"),
		r.URL.Query().Get("workflowRunId"),
		r.URL.Query().Get("scriptEpisodeId"),
		r.URL.Query().Get("storyboardPlanId"),
		strings.EqualFold(r.URL.Query().Get("includePreviewUrl"), "true"),
	)
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
	s.runShotProductionActionRequest(w, r, principal, req)
}

func (s *Server) generateVideoPromptsBatch(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	s.runShotProductionBatchRequest(w, r, principal, true)
}

func (s *Server) generateShotVideosBatch(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	s.runShotProductionBatchRequest(w, r, principal, false)
}

func (s *Server) runShotProductionBatchRequest(
	w http.ResponseWriter,
	r *http.Request,
	principal auth.Principal,
	promptBatch bool,
) {
	var batch ShotProductionBatchRequest
	if !decode(w, r, &batch) {
		return
	}
	action := "generate_missing_videos"
	if promptBatch {
		action = "generate_video_prompts"
	}
	if len(batch.ShotIDs) > 0 {
		if promptBatch {
			action = "generate_selected_video_prompts"
		} else {
			action = "generate_selected_videos"
		}
	}
	options := map[string]any{}
	if batch.Force != nil {
		options["force"] = *batch.Force
	}
	if batch.MaxConcurrency > 0 {
		options["maxConcurrency"] = batch.MaxConcurrency
	}
	if value := strings.TrimSpace(batch.Resolution); value != "" {
		options["resolution"] = value
	}
	if batch.PollIntervalSeconds > 0 {
		options["pollIntervalSeconds"] = batch.PollIntervalSeconds
	}
	if batch.MaxPolls > 0 {
		options["maxPolls"] = batch.MaxPolls
	}
	s.runShotProductionActionRequest(w, r, principal, ShotProductionActionRequest{
		Action: action, ScriptEpisodeID: batch.ScriptEpisodeID, WorkflowRunID: batch.WorkflowRunID,
		ShotIDs: batch.ShotIDs, Options: options,
	})
}

func (s *Server) runShotProductionActionRequest(
	w http.ResponseWriter,
	r *http.Request,
	principal auth.Principal,
	req ShotProductionActionRequest,
) {
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
	scriptSceneID, workflowRunID, scriptEpisodeID := shotProductionScopeFilters(req)
	status, err := s.loadShotProductionStatusForEpisode(
		r,
		project.ID,
		scriptSceneID,
		workflowRunID,
		scriptEpisodeID,
		"",
		false,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	targets, errorCode := selectShotProductionTargets(req, status.Shots)
	if errorCode != "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, errorCode, shotProductionActionErrorMessage(errorCode), nil, false)
		return
	}
	scriptEpisodeID = shotProductionTargetEpisodeID(req, status.Shots, targets)
	workflowType, workflowFunc, _ := shotProductionWorkflowForAction(req.Action)
	input := map[string]any{
		"action":         req.Action,
		"shotIds":        targets,
		"force":          shotProductionOptionBool(req.Options, "force", true),
		"aspectRatio":    firstNonEmptyString(project.VideoRatio, stringValue(project.AspectRatio), "16:9"),
		"resolution":     firstNonEmptyString(shotProductionOptionString(req.Options, "resolution"), "720p"),
		"maxConcurrency": shotProductionMaxConcurrency(req.Action, req.Options),
	}
	if scriptEpisodeID != "" {
		input["scriptEpisodeId"] = scriptEpisodeID
	}
	if value := shotProductionOptionFloat(req.Options, "duration", 0); value > 0 {
		input["duration"] = value
	}
	if value := shotProductionOptionInt(req.Options, "pollIntervalSeconds", 0); value > 0 {
		input["pollIntervalSeconds"] = value
	}
	if value := shotProductionOptionInt(req.Options, "maxPolls", 0); value > 0 {
		input["maxPolls"] = value
	}
	run, err := s.startProjectWorkflowCoreWithHook(
		r.Context(), principal, project, workflowType, input, workflowFunc,
		func(ctx context.Context, tx pgx.Tx, run WorkflowRun) error {
			return markShotProductionQueuedTx(ctx, tx, req.Action, run.ID, targets)
		},
	)
	if err != nil {
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
	return s.loadShotProductionStatusForEpisode(r, projectID, scriptSceneID, workflowRunID, "", "", includePreviewURL)
}

func (s *Server) loadShotProductionStatusForEpisode(r *http.Request, projectID, scriptSceneID, workflowRunID, scriptEpisodeID, storyboardPlanID string, includePreviewURL bool) (ShotProductionStatus, error) {
	return s.loadShotProductionStatusForEpisodeGeneration(r, projectID, scriptSceneID, workflowRunID, scriptEpisodeID, storyboardPlanID, "", includePreviewURL)
}

func (s *Server) loadShotProductionStatusForEpisodeGeneration(r *http.Request, projectID, scriptSceneID, workflowRunID, scriptEpisodeID, storyboardPlanID, generationID string, includePreviewURL bool) (ShotProductionStatus, error) {
	var aspectRatio, activeGenerationID string
	if err := s.db.QueryRow(r.Context(), `
		SELECT COALESCE(NULLIF(BTRIM(video_ratio), ''), NULLIF(BTRIM(aspect_ratio), ''), '16:9'),
		       active_video_production_generation_id::text
		FROM projects
		WHERE id = $1
	`, projectID).Scan(&aspectRatio, &activeGenerationID); err != nil {
		return ShotProductionStatus{}, err
	}
	if strings.TrimSpace(generationID) == "" {
		generationID = activeGenerationID
	} else if generationID != activeGenerationID {
		return ShotProductionStatus{}, videoproduction.NewError(videoproduction.CodeGenerationMismatch, "项目视频生产代已切换，请刷新后重试", false)
	}
	if err := s.reconcileTerminalShotImageStates(r, projectID, generationID); err != nil {
		return ShotProductionStatus{}, err
	}
	if err := s.reconcileTerminalShotVideoStates(r, projectID, generationID); err != nil {
		return ShotProductionStatus{}, err
	}
	rows, err := s.db.Query(r.Context(), `
		`+storyboardShotSelectSQL(`
		WHERE s.project_id = $1
		  AND s.production_generation_id = $6
		  AND s.deleted_at IS NULL
		  AND ($2 = '' OR s.script_scene_id = $2::uuid)
		  AND ($3 = '' OR s.workflow_run_id = $3::uuid)
		  AND ($4 = '' OR s.script_episode_id = $4::uuid)
		  AND ($5 = '' OR s.storyboard_plan_id = $5::uuid)
		  AND (
		    $5 <> ''
		    OR $3 <> ''
		    OR EXISTS (
		      SELECT 1
		      FROM storyboard_plans active_plan
		      WHERE active_plan.id = s.storyboard_plan_id
		        AND active_plan.project_id = s.project_id
		        AND active_plan.production_generation_id = $6
		        AND active_plan.active = true
		        AND active_plan.status = 'ready'
		    )
		    OR (
		      s.storyboard_plan_id IS NULL
		      AND NOT EXISTS (
		        SELECT 1
		        FROM storyboard_plans project_active_plan
		        WHERE project_active_plan.project_id = s.project_id
		          AND project_active_plan.production_generation_id = $6
		          AND project_active_plan.active = true
		          AND project_active_plan.status = 'ready'
		      )
		    )
		  )
		ORDER BY s.episode_index ASC NULLS LAST, s.episode_shot_index ASC NULLS LAST, COALESCE(sc.scene_no, 0), s.shot_index ASC
	`), projectID, strings.TrimSpace(scriptSceneID), strings.TrimSpace(workflowRunID), strings.TrimSpace(scriptEpisodeID), strings.TrimSpace(storyboardPlanID), generationID)
	if err != nil {
		return ShotProductionStatus{}, err
	}
	defer rows.Close()
	status := ShotProductionStatus{ProjectID: projectID, AspectRatio: aspectRatio, Shots: make([]ShotProductionShot, 0)}
	publicIndexByScope := map[string]int{}
	for rows.Next() {
		shot, err := scanStoryboardShot(rows)
		if err != nil {
			return ShotProductionStatus{}, err
		}
		normalizeShotProductionPublicNumber(&shot, publicIndexByScope)
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

func normalizeShotProductionPublicNumber(shot *StoryboardShot, publicIndexByScope map[string]int) {
	scope := firstNonEmptyString(stringValue(shot.StoryboardPlanID), stringValue(shot.ScriptEpisodeID), shot.WorkflowRunID, "project")
	publicIndex := publicIndexByScope[scope]
	shot.ShotIndex = publicIndex
	shot.ShotNo = publicIndex + 1
	shot.EpisodeShotIndex = &publicIndex
	publicIndexByScope[scope] = publicIndex + 1
}

func (s *Server) reconcileTerminalShotImageStates(r *http.Request, projectID, generationID string) error {
	_, err := s.db.Exec(r.Context(), `
		UPDATE storyboard_shots s
		SET image_status = 'failed',
		    image_error_code = CASE
		      WHEN image_run.status = 'cancelled' THEN 'USER_CANCELLED'
		      ELSE 'WORKFLOW_TERMINAL'
		    END,
		    image_error_message = CASE
		      WHEN image_run.status = 'cancelled' THEN '关联工作流已取消，图片生成未完成'
		      ELSE '关联工作流已结束，图片生成未完成'
		    END,
		    image_completed_at = now(),
		    status = 'image_failed',
		    updated_at = now()
		FROM workflow_runs image_run
		WHERE s.project_id = $1
		  AND s.production_generation_id = $2
		  AND s.deleted_at IS NULL
		  AND image_run.id = s.image_workflow_run_id
		  AND s.image_status IN ('queued', 'running')
		  AND image_run.status IN ('succeeded', 'failed', 'cancelled')
	`, projectID, generationID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(r.Context(), `
		UPDATE storyboard_shots s
		SET image_prompt_status = 'failed',
		    image_prompt_error_code = CASE WHEN w.status = 'cancelled' THEN 'USER_CANCELLED' ELSE 'WORKFLOW_TERMINAL' END,
		    image_prompt_error_message = CASE WHEN w.status = 'cancelled' THEN '关联工作流已取消，图片提示词未完成' ELSE '关联工作流已结束，图片提示词未完成' END,
		    image_prompt_updated_at = now(),
		    updated_at = now()
		FROM workflow_runs w
		WHERE s.project_id = $1
		  AND s.production_generation_id = $2
		  AND s.deleted_at IS NULL
		  AND s.image_prompt_workflow_run_id = w.id
		  AND s.image_prompt_status IN ('queued', 'running')
		  AND w.status IN ('succeeded', 'failed', 'cancelled')
	`, projectID, generationID)
	return err
}

func shotProductionShotFromStoryboard(shot StoryboardShot) ShotProductionShot {
	canUseVideoInput := storyboardShotHasVideoInput(shot)
	return ShotProductionShot{
		ID:                       shot.ID,
		WorkflowRunID:            shot.WorkflowRunID,
		StoryboardPlanID:         shot.StoryboardPlanID,
		ScriptSceneID:            shot.ScriptSceneID,
		ScriptEpisodeID:          shot.ScriptEpisodeID,
		EpisodeIndex:             shot.EpisodeIndex,
		EpisodeShotIndex:         shot.EpisodeShotIndex,
		EpisodeTitle:             shot.EpisodeTitle,
		ShotIndex:                shot.ShotIndex,
		ShotNo:                   shot.ShotNo,
		Title:                    shot.Title,
		DurationSeconds:          shot.DurationSeconds,
		Visual:                   shot.Visual,
		ImagePrompt:              shot.ImagePrompt,
		ImagePromptStatus:        shot.ImagePromptStatus,
		ImagePromptErrorCode:     shot.ImagePromptErrorCode,
		ImagePromptErrorMessage:  shot.ImagePromptErrorMessage,
		ImagePromptWorkflowRunID: shot.ImagePromptWorkflowRunID,
		VideoPrompt:              shot.VideoPrompt,
		ScriptDialogue:           append([]workflows.StoryboardDialogueLine(nil), shot.ScriptDialogue...),
		VideoPromptStatus:        shot.VideoPromptStatus,
		VideoPromptErrorCode:     shot.VideoPromptErrorCode,
		VideoPromptErrorMessage:  shot.VideoPromptErrorMessage,
		VideoPromptWorkflowRunID: shot.VideoPromptWorkflowRunID,
		ImageStatus:              shot.ImageStatus,
		VideoStatus:              shot.VideoStatus,
		StaleState:               shot.StaleState,
		ImageArtifactID:          shot.ImageArtifactID,
		ImageMediaFileID:         shot.ImageMediaFileID,
		ImageStorageKey:          shot.ImageStorageKey,
		ImagePreviewURL:          shot.ImagePreviewURL,
		VideoArtifactID:          shot.VideoArtifactID,
		VideoMediaFileID:         shot.VideoMediaFileID,
		VideoStorageKey:          shot.VideoStorageKey,
		VideoPreviewURL:          shot.VideoPreviewURL,
		VideoReferenceMode:       shot.VideoReferenceMode,
		VideoReferenceKeys:       append([]string(nil), shot.VideoReferenceKeys...),
		ImageErrorCode:           shot.ImageErrorCode,
		ImageErrorMessage:        shot.ImageErrorMessage,
		VideoErrorCode:           shot.VideoErrorCode,
		VideoErrorMessage:        shot.VideoErrorMessage,
		ImageWorkflowRunID:       shot.ImageWorkflowRunID,
		VideoWorkflowRunID:       shot.VideoWorkflowRunID,
		ProviderAsyncTaskID:      shot.VideoProviderAsyncTaskID,
		ExternalTaskID:           shot.VideoExternalTaskID,
		CanGenerateImage:         shot.ImagePromptStatus == "succeeded" && (shot.ImageStatus == "not_started" || shot.ImageStatus == "stale"),
		CanGenerateImagePrompt:   shot.ImagePromptStatus != "queued" && shot.ImagePromptStatus != "running",
		CanGenerateVideo:         canUseVideoInput && shot.VideoPromptStatus == "succeeded" && (shot.VideoStatus == "not_started" || shot.VideoStatus == "stale"),
		CanGenerateVideoPrompt:   shot.VideoPromptStatus != "queued" && shot.VideoPromptStatus != "running",
		CanRetryImage:            shot.ImagePromptStatus == "succeeded" && shot.ImageStatus == "failed",
		CanRetryVideo:            canUseVideoInput && shot.VideoPromptStatus == "succeeded" && shot.VideoStatus == "failed",
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
	switch shot.ImagePromptStatus {
	case "succeeded":
		summary.ImagePromptSucceeded++
	case "failed":
		summary.ImagePromptFailed++
	case "queued", "running":
		summary.ImagePromptRunning++
		running = true
	default:
		summary.ImagePromptMissing++
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
	switch shot.VideoPromptStatus {
	case "succeeded":
		summary.VideoPromptSucceeded++
	case "failed":
		summary.VideoPromptFailed++
	case "queued", "running":
		summary.VideoPromptRunning++
		running = true
	default:
		summary.VideoPromptMissing++
	}
	if running {
		summary.Running++
	}
}

func selectShotProductionTargets(req ShotProductionActionRequest, shots []ShotProductionShot) ([]string, string) {
	requested := cleanShotProductionIDs(req.ShotIDs)
	selectedActions := map[string]bool{
		"generate_selected_images":        true,
		"generate_selected_image_prompts": true,
		"generate_selected_videos":        true,
		"generate_selected_video_prompts": true,
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
		if req.Action == "generate_selected_images" {
			for _, shotID := range requested {
				if shotByID[shotID].ImagePromptStatus != "succeeded" {
					return nil, "SHOT_IMAGE_PROMPT_REQUIRED"
				}
			}
		} else if req.Action == "generate_selected_image_prompts" {
			for _, shotID := range requested {
				if !shotByID[shotID].CanGenerateImagePrompt {
					return nil, "SHOT_IMAGE_PROMPT_RUNNING"
				}
			}
		} else if req.Action == "generate_selected_videos" {
			for _, shotID := range requested {
				if shotByID[shotID].VideoPromptStatus != "succeeded" {
					return nil, "SHOT_VIDEO_PROMPT_REQUIRED"
				}
				if !shotProductionHasVideoInput(shotByID[shotID]) {
					return nil, "SHOT_IMAGE_REQUIRED"
				}
			}
		} else if req.Action == "generate_selected_video_prompts" {
			for _, shotID := range requested {
				if !shotByID[shotID].CanGenerateVideoPrompt {
					return nil, "SHOT_VIDEO_PROMPT_RUNNING"
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
	forceVideoPrompts := req.Action == "generate_video_prompts" && shotProductionOptionBool(req.Options, "force", false)
	for _, shot := range shots {
		if len(requested) > 0 && !containsString(requested, shot.ID) {
			continue
		}
		if shotProductionActionMatches(req.Action, shot) || (forceVideoPrompts && shot.CanGenerateVideoPrompt) {
			out = append(out, shot.ID)
		}
	}
	if len(out) == 0 {
		return nil, "NO_TARGET_SHOTS"
	}
	return out, ""
}

func shotProductionScopeFilters(req ShotProductionActionRequest) (scriptSceneID, workflowRunID, scriptEpisodeID string) {
	if len(cleanShotProductionIDs(req.ShotIDs)) > 0 {
		// Explicit shot IDs are the authoritative scope. Secondary identifiers are
		// redundant hints and may be stale or model-generated.
		return "", "", ""
	}
	return strings.TrimSpace(req.ScriptSceneID), strings.TrimSpace(req.WorkflowRunID), strings.TrimSpace(req.ScriptEpisodeID)
}

func shotProductionTargetEpisodeID(req ShotProductionActionRequest, shots []ShotProductionShot, targets []string) string {
	if len(cleanShotProductionIDs(req.ShotIDs)) == 0 {
		return strings.TrimSpace(req.ScriptEpisodeID)
	}
	targetSet := make(map[string]struct{}, len(targets))
	for _, id := range targets {
		targetSet[id] = struct{}{}
	}
	episodeID := ""
	for _, shot := range shots {
		if _, ok := targetSet[shot.ID]; !ok || shot.ScriptEpisodeID == nil {
			continue
		}
		current := strings.TrimSpace(*shot.ScriptEpisodeID)
		if current == "" {
			continue
		}
		if episodeID != "" && episodeID != current {
			return ""
		}
		episodeID = current
	}
	return episodeID
}

func shotProductionActionMatches(action string, shot ShotProductionShot) bool {
	switch action {
	case "generate_image_prompts":
		return shot.CanGenerateImagePrompt && (shot.ImagePromptStatus == "not_started" || shot.ImagePromptStatus == "failed")
	case "generate_missing_images":
		return shot.ImagePromptStatus == "succeeded" && (shot.ImageStatus == "not_started" || shot.ImageStatus == "stale")
	case "regenerate_stale_images":
		return shot.ImagePromptStatus == "succeeded" && shot.ImageStatus == "stale"
	case "regenerate_failed_images":
		return shot.ImagePromptStatus == "succeeded" && shot.ImageStatus == "failed"
	case "generate_missing_videos":
		return shot.VideoPromptStatus == "succeeded" && shotProductionHasVideoInput(shot) && (shot.VideoStatus == "not_started" || shot.VideoStatus == "stale")
	case "regenerate_stale_videos":
		return shot.VideoPromptStatus == "succeeded" && shotProductionHasVideoInput(shot) && shot.VideoStatus == "stale"
	case "regenerate_failed_videos":
		return shot.VideoPromptStatus == "succeeded" && shotProductionHasVideoInput(shot) && shot.VideoStatus == "failed"
	case "generate_video_prompts":
		return shot.CanGenerateVideoPrompt && (shot.VideoPromptStatus == "not_started" || shot.VideoPromptStatus == "failed")
	case "cancel_running_videos":
		return (shot.VideoStatus == "queued" || shot.VideoStatus == "running") && stringValue(shot.ProviderAsyncTaskID) != ""
	default:
		return false
	}
}

func shotProductionWorkflowForAction(action string) (string, any, bool) {
	switch action {
	case "generate_image_prompts", "generate_selected_image_prompts":
		return "batch_generate_shot_image_prompts", workflows.BatchGenerateShotImagePromptsWorkflow, true
	case "generate_missing_images", "regenerate_stale_images", "regenerate_failed_images", "generate_selected_images":
		return "batch_generate_shot_images", workflows.BatchGenerateShotImagesWorkflow, true
	case "generate_missing_videos", "regenerate_stale_videos", "regenerate_failed_videos", "generate_selected_videos":
		return "batch_generate_shot_videos", workflows.EpisodeBatchGenerateShotVideosWorkflow, true
	case "generate_video_prompts", "generate_selected_video_prompts":
		return "batch_generate_shot_video_prompts", workflows.BatchGenerateShotVideoPromptsWorkflow, true
	case "cancel_running_videos":
		return "batch_cancel_shot_videos", workflows.BatchCancelShotVideosWorkflow, true
	default:
		return "", nil, false
	}
}

func shotProductionMaxConcurrency(action string, options map[string]any) int {
	fallback := workflows.DefaultShotVideoConcurrency
	maximum := workflows.MaxShotVideoConcurrency
	if shotProductionImagePromptAction(action) {
		fallback = workflows.DefaultShotImagePromptConcurrency
		maximum = workflows.MaxShotImagePromptConcurrency
	} else if shotProductionVideoPromptAction(action) {
		fallback = workflows.DefaultShotVideoPromptConcurrency
		maximum = workflows.MaxShotVideoPromptConcurrency
	} else if shotProductionImageAction(action) {
		fallback = workflows.DefaultShotImageConcurrency
		maximum = workflows.MaxShotImageConcurrency
	}
	value := shotProductionOptionInt(options, "maxConcurrency", fallback)
	if value < 1 {
		value = fallback
	}
	if value > maximum {
		value = maximum
	}
	return value
}

func shotProductionVideoPromptAction(action string) bool {
	return action == "generate_video_prompts" || action == "generate_selected_video_prompts"
}

func shotProductionImagePromptAction(action string) bool {
	return action == "generate_image_prompts" || action == "generate_selected_image_prompts"
}

func shotProductionImageAction(action string) bool {
	switch action {
	case "generate_missing_images", "regenerate_stale_images", "regenerate_failed_images", "generate_selected_images":
		return true
	default:
		return false
	}
}

func markShotProductionQueuedTx(ctx context.Context, tx pgx.Tx, action, workflowRunID string, shotIDs []string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET total_items = $2,
		    completed_items = 0,
		    failed_items = 0,
		    revision = revision + 1,
		    updated_at = now()
		WHERE id = $1
	`, workflowRunID, len(shotIDs)); err != nil {
		return err
	}
	if action == "cancel_running_videos" {
		return nil
	}
	switch {
	case shotProductionImagePromptAction(action):
		_, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET image_prompt_status = 'queued',
			    image_prompt_error_code = NULL,
			    image_prompt_error_message = NULL,
			    image_prompt_workflow_run_id = $1,
			    image_prompt_updated_at = now(),
			    updated_at = now()
			WHERE id = ANY($2::uuid[])
		`, workflowRunID, shotIDs)
		return err
	case shotProductionVideoPromptAction(action):
		_, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET video_prompt_status = 'queued',
			    video_prompt_error_code = NULL,
			    video_prompt_error_message = NULL,
			    video_prompt_workflow_run_id = $1,
			    video_prompt_updated_at = now(),
			    updated_at = now()
			WHERE id = ANY($2::uuid[])
		`, workflowRunID, shotIDs)
		return err
	case strings.Contains(action, "_images"):
		_, err := tx.Exec(ctx, `
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
		_, err := tx.Exec(ctx, `
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
	case "SHOT_IMAGE_PROMPT_REQUIRED":
		return "shot image prompt must be ready before image generation"
	case "SHOT_IMAGE_PROMPT_RUNNING":
		return "shot image prompt generation is already running"
	case "SHOT_VIDEO_PROMPT_RUNNING":
		return "shot video prompt generation is already running"
	case "SHOT_VIDEO_PROMPT_REQUIRED":
		return "shot video prompt must be generated and reviewed before video generation"
	default:
		return "shot production action is invalid"
	}
}

func shotHasImage(shot StoryboardShot) bool {
	return shot.ImageArtifactID != nil || shot.ImageMediaFileID != nil || stringValue(shot.ImageStorageKey) != ""
}

func storyboardShotHasVideoInput(shot StoryboardShot) bool {
	switch shot.VideoReferenceMode {
	case "none":
		return true
	case "custom":
		return len(shot.VideoReferenceKeys) > 0
	default:
		return shotHasImage(shot)
	}
}

func shotProductionHasImage(shot ShotProductionShot) bool {
	return shot.ImageArtifactID != nil || shot.ImageMediaFileID != nil || stringValue(shot.ImageStorageKey) != ""
}

func shotProductionHasVideoInput(shot ShotProductionShot) bool {
	switch shot.VideoReferenceMode {
	case "none":
		return true
	case "custom":
		return len(shot.VideoReferenceKeys) > 0
	default:
		return shotProductionHasImage(shot)
	}
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
