package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/sdk/temporal"
)

type PlanShotVideoInput struct {
	OrganizationID          string   `json:"organizationId"`
	ProjectID               string   `json:"projectId"`
	WorkflowRunID           string   `json:"workflowRunId"`
	CreatedBy               string   `json:"createdBy,omitempty"`
	WorkflowPrompt          string   `json:"workflowPrompt,omitempty"`
	FailureScope            string   `json:"failureScope,omitempty"`
	ShotID                  string   `json:"shotId"`
	ShotIndex               int      `json:"shotIndex"`
	AspectRatio             string   `json:"aspectRatio"`
	Resolution              string   `json:"resolution"`
	AudioStrategy           string   `json:"audioStrategy"`
	AudioRequirement        string   `json:"audioRequirement"`
	Force                   bool     `json:"force,omitempty"`
	PromptOnly              bool     `json:"promptOnly,omitempty"`
	ExcludeProviderModelIDs []string `json:"excludeProviderModelIds,omitempty"`
	PreviousExecutionPlanID string   `json:"previousExecutionPlanId,omitempty"`
}

type PlanShotVideoOutput struct {
	NodeRunID         string `json:"nodeRunId"`
	ExecutionToken    string `json:"executionToken"`
	AttemptGeneration int    `json:"attemptGeneration"`
	provider.GatewayVideoPlanResponse
}

func (a Activities) PlanShotVideo(ctx context.Context, input PlanShotVideoInput) (PlanShotVideoOutput, error) {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.ShotID) == "" {
		return PlanShotVideoOutput{}, fmt.Errorf("organizationId, projectId, workflowRunId, and shotId are required")
	}
	shot, err := a.storyboardShot(ctx, input.ProjectID, input.WorkflowRunID, input.ShotID, input.ShotIndex)
	if err != nil {
		return PlanShotVideoOutput{}, err
	}
	baseInput := TextToStoryboardInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		Prompt:         firstNonEmptyString(input.WorkflowPrompt, "plan_shot_video"),
		CreatedBy:      input.CreatedBy,
		FailureScope:   input.FailureScope,
	}
	fail := func(nodeExecution NodeExecution, cause error) (PlanShotVideoOutput, error) {
		if input.PromptOnly {
			promptInput := PrepareShotVideoPromptInput{
				OrganizationID: input.OrganizationID,
				ProjectID:      input.ProjectID,
				WorkflowRunID:  input.WorkflowRunID,
				CreatedBy:      input.CreatedBy,
				ShotID:         shot.ID,
				ShotIndex:      shot.ShotIndex,
				ShotNo:         shot.ShotNo,
				WorkflowPrompt: input.WorkflowPrompt,
				PromptOnly:     true,
			}
			return PlanShotVideoOutput{}, a.failShotVideoPromptActivity(ctx, promptInput, baseInput, shot, nodeExecution, cause)
		}
		return PlanShotVideoOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeExecution, "video_failed", "storyboard.shot.video.failed", cause)
	}
	settings, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return fail(NodeExecution{}, err)
	}
	assetContext, err := a.shotAssetContext(ctx, input.ProjectID, shot.ID)
	if err != nil {
		return fail(NodeExecution{}, err)
	}
	references, err := a.shotVideoReferenceContext(ctx, input.ProjectID, shot, assetContext)
	if err != nil {
		return fail(NodeExecution{}, err)
	}
	referenceMode := "first_frame"
	taskType := "video.image_to_video"
	if references.ReferenceMode == "none" || len(references.References) == 0 {
		referenceMode = "none"
		taskType = "video.text_to_video"
	}
	aspectRatio := firstNonEmptyString(settings.VideoRatio, settings.AspectRatio, input.AspectRatio, "16:9")
	resolution := firstNonEmptyString(input.Resolution, "720p")
	audioStrategy := firstNonEmptyString(input.AudioStrategy, "native_av")
	audioRequirement := firstNonEmptyString(input.AudioRequirement, "preferred")
	dialogueSpans, err := gatewayVideoDialogueSpans(shot)
	if err != nil {
		return fail(NodeExecution{}, err)
	}
	nodeExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		NodeKey: fmt.Sprintf("plan_shot_video_%d", shot.ShotIndex), NodeType: "video.plan",
		Input: mustJSON(map[string]any{
			"shotId": shot.ID, "storyboardPlanId": shot.StoryboardPlanID,
			"targetDurationTicks": shot.PlannedDurationTicks, "timelineTimebase": shot.TimelineTimebase,
			"fpsNumerator": shot.FPSNumerator, "fpsDenominator": shot.FPSDenominator,
			"taskType": taskType, "referenceMode": referenceMode, "aspectRatio": aspectRatio,
			"resolution": resolution, "audioStrategy": audioStrategy, "audioRequirement": audioRequirement,
		}),
	})
	if err != nil {
		return PlanShotVideoOutput{}, err
	}
	if input.PromptOnly {
		if err := a.markShotVideoPromptRunning(ctx, PrepareShotVideoPromptInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			CreatedBy:      input.CreatedBy,
			ShotID:         shot.ID,
			ShotIndex:      shot.ShotIndex,
			ShotNo:         shot.ShotNo,
			WorkflowPrompt: input.WorkflowPrompt,
			PromptOnly:     true,
		}, shot, nodeExecution); err != nil {
			return fail(nodeExecution, err)
		}
	}
	if shotVideoReferencesStoryboardImage(references, shot.ID) {
		if err := a.validateShotImageAspectRatio(ctx, input.ProjectID, shot.ImageMediaFileID, aspectRatio); err != nil {
			return fail(nodeExecution, err)
		}
	}
	if a.gateway == nil {
		cause := workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured", Retryable: false, RetryabilityKnown: true}
		return fail(nodeExecution, cause)
	}
	plan, err := a.gateway.PlanVideo(ctx, provider.GatewayVideoPlanRequest{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, NodeRunID: nodeExecution.NodeRunID,
		NodeExecutionToken: nodeExecution.ExecutionToken, NodeAttemptGeneration: nodeExecution.AttemptGeneration,
		StoryboardPlanID: shot.StoryboardPlanID, StoryboardShotID: shot.ID, ModelProfileKey: settings.VideoModelProfileKey,
		TaskType: taskType, TargetDurationTicks: shot.PlannedDurationTicks, TimelineTimebase: shot.TimelineTimebase,
		FPSNumerator: int64(shot.FPSNumerator), FPSDenominator: int64(shot.FPSDenominator),
		AudioStrategy: audioStrategy, AudioRequirement: audioRequirement, DialogueLanguage: "zh-CN", HasDialogue: len(dialogueSpans) > 0,
		ReferenceMode: referenceMode, AspectRatio: aspectRatio, Resolution: resolution, PromptLanguage: "zh-CN",
		ExpiresInSeconds:        24 * 60 * 60,
		Force:                   input.Force,
		ExcludeProviderModelIDs: input.ExcludeProviderModelIDs, PreviousExecutionPlanID: input.PreviousExecutionPlanID,
		DialogueSpans: dialogueSpans,
	})
	if err != nil {
		workflowErr := workflowErrorFromProvider(err, provider.CodeModelCapabilityUnavailable)
		return fail(nodeExecution, workflowErr)
	}
	output := PlanShotVideoOutput{
		NodeRunID: nodeExecution.NodeRunID, ExecutionToken: nodeExecution.ExecutionToken,
		AttemptGeneration: nodeExecution.AttemptGeneration, GatewayVideoPlanResponse: plan,
	}
	if err := CompleteNodeRun(ctx, a.db, nodeExecution, mustJSON(output)); err != nil {
		return PlanShotVideoOutput{}, err
	}
	return output, nil
}

type FinalizeShotVideoPromptPlanInput struct {
	OrganizationID      string  `json:"organizationId"`
	ProjectID           string  `json:"projectId"`
	WorkflowRunID       string  `json:"workflowRunId"`
	ShotID              string  `json:"shotId"`
	ExecutionPlanID     string  `json:"executionPlanId"`
	PromptWorkflowRunID *string `json:"promptWorkflowRunId,omitempty"`
	PromptSource        string  `json:"promptSource,omitempty"`
}

type PreparedShotVideoSegment struct {
	provider.GatewayVideoPlanSegment
	Prompt                   string `json:"prompt"`
	NegativePrompt           string `json:"negativePrompt,omitempty"`
	PromptHash               string `json:"promptHash"`
	GenerationProviderCallID string `json:"generationProviderCallId,omitempty"`
	ReviewProviderCallID     string `json:"reviewProviderCallId,omitempty"`
	ReviewTemplateKey        string `json:"reviewTemplateKey,omitempty"`
	ReviewPromptVersionID    string `json:"reviewPromptVersionId,omitempty"`
}

type LoadPreparedShotVideoPlanInput struct {
	OrganizationID   string `json:"organizationId"`
	ProjectID        string `json:"projectId"`
	WorkflowRunID    string `json:"workflowRunId"`
	ShotID           string `json:"shotId"`
	ShotIndex        int    `json:"shotIndex"`
	AspectRatio      string `json:"aspectRatio,omitempty"`
	Resolution       string `json:"resolution,omitempty"`
	AudioStrategy    string `json:"audioStrategy,omitempty"`
	AudioRequirement string `json:"audioRequirement,omitempty"`
}

type LoadPreparedShotVideoPlanOutput struct {
	Plan     PlanShotVideoOutput        `json:"plan"`
	Segments []PreparedShotVideoSegment `json:"segments"`
}

type EnsurePreparedShotVideoPlanInput struct {
	OrganizationID   string `json:"organizationId"`
	ProjectID        string `json:"projectId"`
	WorkflowRunID    string `json:"workflowRunId"`
	CreatedBy        string `json:"createdBy,omitempty"`
	WorkflowPrompt   string `json:"workflowPrompt,omitempty"`
	FailureScope     string `json:"failureScope,omitempty"`
	ShotID           string `json:"shotId"`
	ShotIndex        int    `json:"shotIndex"`
	AspectRatio      string `json:"aspectRatio,omitempty"`
	Resolution       string `json:"resolution,omitempty"`
	AudioStrategy    string `json:"audioStrategy,omitempty"`
	AudioRequirement string `json:"audioRequirement,omitempty"`
	Force            bool   `json:"force,omitempty"`
}

type reviewedShotVideoPromptSource struct {
	Prompt              string
	PromptWorkflowRunID string
}

type videoPromptPlanSegmentRef struct {
	ID    string
	Index int
}

var videoPromptSegmentHeaderPattern = regexp.MustCompile(`(?m)^\[片段\s+(\d+)\s*/\s*(\d+)\]\s*$`)

func (a Activities) FinalizeShotVideoPromptPlan(ctx context.Context, input FinalizeShotVideoPromptPlanInput) error {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.ShotID) == "" || strings.TrimSpace(input.ExecutionPlanID) == "" {
		return fmt.Errorf("organizationId, projectId, workflowRunId, shotId, and executionPlanId are required")
	}
	ready, err := a.shotVideoPromptPlanReady(ctx, input.ProjectID, input.ShotID, input.ExecutionPlanID)
	if err != nil {
		return err
	}
	if ready {
		return nil
	}
	execution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKeyForID("video_prompt_plan_finalize", input.ExecutionPlanID),
		NodeType:       "video.prompt.plan_finalize",
		Input:          mustJSON(input),
	})
	if err != nil {
		return err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return err
	}
	if err := a.finalizeShotVideoPromptPlanTx(ctx, tx, input); err != nil {
		return err
	}
	if applied, err := completeNodeRunTx(ctx, tx, execution, mustJSON(map[string]any{"shotId": input.ShotID, "executionPlanId": input.ExecutionPlanID, "status": "ready"})); err != nil {
		return err
	} else if !applied {
		return ErrWorkflowWriteFenced
	}
	return tx.Commit(ctx)
}

func (a Activities) shotVideoPromptPlanReady(ctx context.Context, projectID, shotID, executionPlanID string) (bool, error) {
	var ready bool
	err := a.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM storyboard_shots shot
			JOIN video_render_plans plan ON plan.id = shot.active_video_render_plan_id
			WHERE shot.id = $1 AND shot.project_id = $2 AND plan.id = $3
			  AND shot.video_prompt_status = 'succeeded'
			  AND COALESCE(shot.metadata #>> '{videoPromptPlan,status}', '') = 'ready'
			  AND plan.active = true
		)
	`, shotID, projectID, executionPlanID).Scan(&ready)
	return ready, err
}

func (a Activities) finalizeShotVideoPromptPlanTx(ctx context.Context, tx pgx.Tx, input FinalizeShotVideoPromptPlanInput) error {
	rows, err := tx.Query(ctx, `
		SELECT segment_index, COALESCE(prompt, ''),
		       COALESCE(metadata #>> '{videoPromptAgent,status}', ''),
		       COALESCE(metadata #>> '{videoPromptAgent,promptHash}', '')
		FROM video_render_segments
		WHERE video_render_plan_id = $1 AND storyboard_shot_id = $2 AND project_id = $3
		ORDER BY segment_index
	`, input.ExecutionPlanID, input.ShotID, input.ProjectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	prompts := make([]string, 0)
	promptHashes := make([]string, 0)
	for rows.Next() {
		var segmentIndex int
		var prompt, status, promptHash string
		if err := rows.Scan(&segmentIndex, &prompt, &status, &promptHash); err != nil {
			return err
		}
		if strings.TrimSpace(prompt) == "" || (status != "approved" && status != "manual_approved") || strings.TrimSpace(promptHash) == "" {
			return preparedVideoPromptError(fmt.Sprintf("视频片段 %d 的提示词尚未完成审核，请重新生成视频提示词", segmentIndex+1))
		}
		prompts = append(prompts, strings.TrimSpace(prompt))
		promptHashes = append(promptHashes, strings.TrimSpace(promptHash))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(prompts) == 0 {
		return preparedVideoPromptError("视频执行计划没有可用的已审核片段提示词，请重新生成视频提示词")
	}
	aggregatedPrompt := prompts[0]
	if len(prompts) > 1 {
		parts := make([]string, 0, len(prompts))
		for index, prompt := range prompts {
			parts = append(parts, fmt.Sprintf("[片段 %d/%d]\n%s", index+1, len(prompts), prompt))
		}
		aggregatedPrompt = strings.Join(parts, "\n\n")
	}
	promptWorkflowRunID := input.WorkflowRunID
	if input.PromptWorkflowRunID != nil {
		promptWorkflowRunID = strings.TrimSpace(*input.PromptWorkflowRunID)
	}
	promptSource := firstNonEmptyString(input.PromptSource, "video_prompt_agents")
	result, err := tx.Exec(ctx, `
		UPDATE storyboard_shots shot
		SET video_prompt = $4,
		    video_prompt_status = 'succeeded',
		    video_prompt_error_code = NULL,
		    video_prompt_error_message = NULL,
		    video_prompt_workflow_run_id = NULLIF($5, '')::uuid,
		    video_prompt_updated_at = now(),
		    metadata = COALESCE(shot.metadata, '{}'::jsonb) || jsonb_build_object(
		      'videoPromptPlan', jsonb_build_object(
		        'status', 'ready', 'executionPlanId', $2::uuid::text,
		        'segmentCount', $6::integer, 'segmentPromptHashes', $7::jsonb,
		        'promptSource', $8::text, 'preparedAt', now()
		      )
		    ),
		    updated_at = now()
		FROM video_render_plans plan
		WHERE shot.id = $1 AND shot.active_video_render_plan_id = $2
		  AND shot.project_id = $3 AND shot.deleted_at IS NULL
		  AND plan.id = $2 AND plan.active = true
		  AND plan.status NOT IN ('stale', 'archived', 'cancelled', 'replan_required')
	`, input.ShotID, input.ExecutionPlanID, input.ProjectID, aggregatedPrompt, promptWorkflowRunID, len(prompts), mustJSON(promptHashes), promptSource)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return preparedVideoPromptError("视频执行计划已失效，请重新生成视频提示词")
	}
	if _, err := tx.Exec(ctx, `
		UPDATE video_render_plans
		SET metadata = metadata || jsonb_build_object(
		      'promptStatus', 'ready', 'promptSource', $3::text, 'promptPreparedAt', now()
		    ), updated_at = now()
		WHERE id = $1 AND project_id = $2
	`, input.ExecutionPlanID, input.ProjectID, promptSource); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.video_prompt.plan_ready", "storyboard_shot", input.ShotID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "shotId": input.ShotID, "executionPlanId": input.ExecutionPlanID,
		"promptWorkflowRunId": promptWorkflowRunID, "promptSource": promptSource,
		"segmentCount": len(prompts), "segmentPromptHashes": promptHashes,
	})); err != nil {
		return err
	}
	return nil
}

func (a Activities) EnsurePreparedShotVideoPlan(ctx context.Context, input EnsurePreparedShotVideoPlanInput) (LoadPreparedShotVideoPlanOutput, error) {
	loadInput := LoadPreparedShotVideoPlanInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		ShotID: input.ShotID, ShotIndex: input.ShotIndex, AspectRatio: input.AspectRatio, Resolution: input.Resolution,
		AudioStrategy: input.AudioStrategy, AudioRequirement: input.AudioRequirement,
	}
	if shouldReusePreparedShotVideoPlan(input.Force) {
		prepared, loadErr := a.loadPreparedShotVideoPlan(ctx, loadInput)
		if loadErr == nil {
			return prepared, nil
		}
		if !isPreparedVideoPromptPlanError(loadErr) {
			return LoadPreparedShotVideoPlanOutput{}, loadErr
		}
	}
	source, err := a.reviewedShotVideoPromptSource(ctx, input.OrganizationID, input.ProjectID, input.ShotID)
	if err != nil {
		return LoadPreparedShotVideoPlanOutput{}, err
	}
	if err := a.validateReviewedShotVideoPromptDialogue(ctx, input, source); err != nil {
		return LoadPreparedShotVideoPlanOutput{}, err
	}
	plan, err := a.PlanShotVideo(ctx, PlanShotVideoInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		CreatedBy: input.CreatedBy, WorkflowPrompt: input.WorkflowPrompt, FailureScope: input.FailureScope,
		ShotID: input.ShotID, ShotIndex: input.ShotIndex,
		AspectRatio: input.AspectRatio, Resolution: input.Resolution, AudioStrategy: input.AudioStrategy,
		AudioRequirement: input.AudioRequirement, Force: input.Force,
	})
	if err != nil {
		return LoadPreparedShotVideoPlanOutput{}, err
	}
	if err := a.materializeReviewedShotVideoPromptPlan(ctx, input, plan.ExecutionPlanID); err != nil {
		return LoadPreparedShotVideoPlanOutput{}, err
	}
	return a.loadPreparedShotVideoPlan(ctx, loadInput)
}

func shouldReusePreparedShotVideoPlan(force bool) bool {
	return !force
}

func (a Activities) reviewedShotVideoPromptSource(ctx context.Context, organizationID, projectID, shotID string) (reviewedShotVideoPromptSource, error) {
	var source reviewedShotVideoPromptSource
	var promptStatus string
	err := a.db.QueryRow(ctx, `
		SELECT COALESCE(video_prompt, ''), COALESCE(video_prompt_status, 'not_started'),
		       COALESCE(video_prompt_workflow_run_id::text, '')
		FROM storyboard_shots
		WHERE id = $1 AND project_id = $2 AND organization_id = $3 AND deleted_at IS NULL
	`, shotID, projectID, organizationID).Scan(&source.Prompt, &promptStatus, &source.PromptWorkflowRunID)
	if err != nil {
		return reviewedShotVideoPromptSource{}, err
	}
	if promptStatus != "succeeded" || strings.TrimSpace(source.Prompt) == "" {
		return reviewedShotVideoPromptSource{}, preparedVideoPromptError("镜头没有可复用的已审核视频提示词，请先批量生成视频提示词")
	}
	source.Prompt = strings.TrimSpace(source.Prompt)
	return source, nil
}

func (a Activities) validateReviewedShotVideoPromptDialogue(ctx context.Context, input EnsurePreparedShotVideoPlanInput, source reviewedShotVideoPromptSource) error {
	shot, err := a.storyboardShot(ctx, input.ProjectID, input.WorkflowRunID, input.ShotID, input.ShotIndex)
	if err != nil {
		return err
	}
	var rawDialogue []byte
	var promptSource string
	if err := a.db.QueryRow(ctx, `
		SELECT metadata #> '{videoPromptAgent,dialogueLines}',
		       COALESCE(metadata #>> '{videoPromptPlan,promptSource}', '')
		FROM storyboard_shots
		WHERE id = $1 AND project_id = $2 AND organization_id = $3 AND deleted_at IS NULL
	`, input.ShotID, input.ProjectID, input.OrganizationID).Scan(&rawDialogue, &promptSource); err != nil {
		return err
	}
	currentDialogue := NormalizeStoryboardDialogue(shot.Dialogue)
	if strings.EqualFold(promptSource, "manual") {
		for _, line := range currentDialogue {
			if !strings.Contains(source.Prompt, line.Text) {
				return a.markReviewedShotVideoPromptContextChanged(ctx, input, "手动视频提示词没有保留当前镜头对白，请修正提示词后再生成视频")
			}
		}
		return nil
	}
	if len(rawDialogue) == 0 {
		for _, line := range currentDialogue {
			if !strings.Contains(source.Prompt, line.Speaker) || !strings.Contains(source.Prompt, line.Text) {
				return a.markReviewedShotVideoPromptContextChanged(ctx, input, "现有视频提示词没有保留当前镜头对白，请重新批量生成视频提示词")
			}
		}
		return nil
	}
	var reviewedDialogue []StoryboardDialogueLine
	if err := json.Unmarshal(rawDialogue, &reviewedDialogue); err != nil {
		return err
	}
	if sameStoryboardDialogueContent(reviewedDialogue, currentDialogue) {
		return nil
	}
	return a.markReviewedShotVideoPromptContextChanged(ctx, input, "镜头对白归属已按时间轴校正，现有视频提示词与当前镜头对白不一致，请重新批量生成视频提示词")
}

func sameStoryboardDialogueContent(left, right []StoryboardDialogueLine) bool {
	left = NormalizeStoryboardDialogue(left)
	right = NormalizeStoryboardDialogue(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if dialogueLineKey(left[index]) != dialogueLineKey(right[index]) || left[index].Kind != right[index].Kind {
			return false
		}
	}
	return true
}

func (a Activities) markReviewedShotVideoPromptContextChanged(ctx context.Context, input EnsurePreparedShotVideoPlanInput, message string) error {
	execution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKeyForID("video_prompt_context_changed", input.ShotID),
		NodeType:       "video.prompt.context_validate",
		Input:          mustJSON(input),
	})
	if err != nil {
		return err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET video_prompt_status = 'failed',
		    video_prompt_error_code = $4,
		    video_prompt_error_message = $5,
		    video_prompt_updated_at = now(), updated_at = now()
		WHERE id = $1 AND project_id = $2 AND organization_id = $3 AND deleted_at IS NULL
	`, input.ShotID, input.ProjectID, input.OrganizationID, provider.CodeRenderPlanReplanRequired, message); err != nil {
		return err
	}
	if applied, err := failNodeRunTx(ctx, tx, execution, provider.CodeRenderPlanReplanRequired, message, mustJSON(map[string]any{"shotId": input.ShotID})); err != nil {
		return err
	} else if !applied {
		return ErrWorkflowWriteFenced
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.video_prompt.context_changed", "storyboard_shot", input.ShotID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "shotId": input.ShotID,
		"errorCode": provider.CodeRenderPlanReplanRequired, "errorMessage": message,
	})); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return preparedVideoPromptError(message)
}

func (a Activities) materializeReviewedShotVideoPromptPlan(ctx context.Context, input EnsurePreparedShotVideoPlanInput, executionPlanID string) error {
	execution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKeyForID("video_prompt_plan_materialize", executionPlanID),
		NodeType:       "video.prompt.plan_materialize",
		Input:          mustJSON(map[string]any{"shotId": input.ShotID, "executionPlanId": executionPlanID}),
	})
	if err != nil {
		return err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return err
	}
	var source reviewedShotVideoPromptSource
	var promptStatus, sourceReviewStatus, promptSource string
	var sourceAgentMetadata []byte
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(video_prompt, ''), COALESCE(video_prompt_status, 'not_started'),
		       COALESCE(video_prompt_workflow_run_id::text, ''),
		       COALESCE(metadata #>> '{videoPromptAgent,status}', ''),
		       COALESCE(metadata #>> '{videoPromptPlan,promptSource}', ''),
		       COALESCE(metadata->'videoPromptAgent', '{}'::jsonb)
		FROM storyboard_shots
		WHERE id = $1 AND project_id = $2 AND organization_id = $3 AND deleted_at IS NULL
		  AND active_video_render_plan_id = $4
		FOR UPDATE
	`, input.ShotID, input.ProjectID, input.OrganizationID, executionPlanID).Scan(
		&source.Prompt, &promptStatus, &source.PromptWorkflowRunID, &sourceReviewStatus, &promptSource, &sourceAgentMetadata,
	); err != nil {
		return err
	}
	if promptStatus != "succeeded" || strings.TrimSpace(source.Prompt) == "" {
		return preparedVideoPromptError("镜头没有可复用的已审核视频提示词，请先批量生成视频提示词")
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text, segment_index
		FROM video_render_segments
		WHERE video_render_plan_id = $1 AND storyboard_shot_id = $2 AND project_id = $3
		ORDER BY segment_index
		FOR UPDATE
	`, executionPlanID, input.ShotID, input.ProjectID)
	if err != nil {
		return err
	}
	segments := make([]videoPromptPlanSegmentRef, 0)
	for rows.Next() {
		var segment videoPromptPlanSegmentRef
		if err := rows.Scan(&segment.ID, &segment.Index); err != nil {
			rows.Close()
			return err
		}
		segments = append(segments, segment)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	segmentPrompts, splitErr := splitReviewedShotVideoPrompt(source.Prompt, len(segments))
	if splitErr != nil {
		message := fmt.Sprintf("现有视频提示词无法映射到 %d 个模型片段，请重新批量生成视频提示词", len(segments))
		if err := markVideoPromptPlanRequiresRegenerationTx(ctx, tx, input, executionPlanID, message); err != nil {
			return err
		}
		if applied, err := failNodeRunTx(ctx, tx, execution, provider.CodeRenderPlanReplanRequired, message, mustJSON(map[string]any{"shotId": input.ShotID, "executionPlanId": executionPlanID})); err != nil {
			return err
		} else if !applied {
			return ErrWorkflowWriteFenced
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return preparedVideoPromptError(message)
	}
	var agentMetadata map[string]any
	if len(sourceAgentMetadata) > 0 {
		_ = json.Unmarshal(sourceAgentMetadata, &agentMetadata)
	}
	if agentMetadata == nil {
		agentMetadata = map[string]any{}
	}
	reviewStatus := "approved"
	if strings.EqualFold(sourceReviewStatus, "manual") || strings.EqualFold(sourceReviewStatus, "manual_approved") || strings.EqualFold(promptSource, "manual") {
		reviewStatus = "manual_approved"
	}
	for index, segment := range segments {
		prompt := segmentPrompts[index]
		promptHash := promptsvc.HashText(prompt)
		segmentMetadata := make(map[string]any, len(agentMetadata)+5)
		for key, value := range agentMetadata {
			segmentMetadata[key] = value
		}
		segmentMetadata["status"] = reviewStatus
		segmentMetadata["promptHash"] = promptHash
		segmentMetadata["promptSource"] = "existing_reviewed_shot_prompt"
		segmentMetadata["sourcePromptWorkflowRunId"] = source.PromptWorkflowRunID
		segmentMetadata["materializedByWorkflowRunId"] = input.WorkflowRunID
		if _, err := tx.Exec(ctx, `
			UPDATE video_render_segments
			SET prompt = $2,
			    metadata = COALESCE(metadata, '{}'::jsonb)
			      || jsonb_build_object('promptStatus', 'succeeded', 'promptCompletedAt', now())
			      || jsonb_build_object('videoPromptAgent', $3::jsonb),
			    error_code = NULL, error_message = NULL, updated_at = now()
			WHERE id = $1 AND video_render_plan_id = $4 AND project_id = $5
		`, segment.ID, prompt, mustJSON(segmentMetadata), executionPlanID, input.ProjectID); err != nil {
			return err
		}
	}
	promptWorkflowRunID := source.PromptWorkflowRunID
	if err := a.finalizeShotVideoPromptPlanTx(ctx, tx, FinalizeShotVideoPromptPlanInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		ShotID: input.ShotID, ExecutionPlanID: executionPlanID,
		PromptWorkflowRunID: &promptWorkflowRunID, PromptSource: "existing_reviewed_shot_prompt",
	}); err != nil {
		return err
	}
	if applied, err := completeNodeRunTx(ctx, tx, execution, mustJSON(map[string]any{"shotId": input.ShotID, "executionPlanId": executionPlanID, "status": "ready"})); err != nil {
		return err
	} else if !applied {
		return ErrWorkflowWriteFenced
	}
	return tx.Commit(ctx)
}

func markVideoPromptPlanRequiresRegenerationTx(ctx context.Context, tx pgx.Tx, input EnsurePreparedShotVideoPlanInput, executionPlanID, message string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE video_render_plans
		SET active = false, status = 'replan_required',
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'promptStatus', 'segmentation_required', 'promptSegmentationError', $3::text, 'updatedAt', now()
		    ), updated_at = now()
		WHERE id = $1 AND project_id = $2
	`, executionPlanID, input.ProjectID, message); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET active_video_render_plan_id = NULL,
		    video_prompt_status = 'failed',
		    video_prompt_error_code = $4,
		    video_prompt_error_message = $5,
		    video_prompt_updated_at = now(), updated_at = now()
		WHERE id = $1 AND project_id = $2 AND organization_id = $3
		  AND active_video_render_plan_id = $6
	`, input.ShotID, input.ProjectID, input.OrganizationID, provider.CodeRenderPlanReplanRequired, message, executionPlanID); err != nil {
		return err
	}
	return insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.video_prompt.segmentation_required", "storyboard_shot", input.ShotID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "shotId": input.ShotID, "executionPlanId": executionPlanID,
		"code": provider.CodeRenderPlanReplanRequired, "message": message,
	}))
}

func splitReviewedShotVideoPrompt(prompt string, segmentCount int) ([]string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || segmentCount <= 0 {
		return nil, fmt.Errorf("prompt and segment count are required")
	}
	if segmentCount == 1 {
		return []string{prompt}, nil
	}
	matches := videoPromptSegmentHeaderPattern.FindAllStringSubmatchIndex(prompt, -1)
	if len(matches) != segmentCount {
		return nil, fmt.Errorf("prompt contains %d segment headers, want %d", len(matches), segmentCount)
	}
	result := make([]string, segmentCount)
	for index, match := range matches {
		ordinal, err := strconv.Atoi(prompt[match[2]:match[3]])
		if err != nil || ordinal != index+1 {
			return nil, fmt.Errorf("prompt segment ordinal is not contiguous")
		}
		total, err := strconv.Atoi(prompt[match[4]:match[5]])
		if err != nil || total != segmentCount {
			return nil, fmt.Errorf("prompt segment total does not match render plan")
		}
		contentEnd := len(prompt)
		if index+1 < len(matches) {
			contentEnd = matches[index+1][0]
		}
		content := strings.TrimSpace(prompt[match[1]:contentEnd])
		if content == "" {
			return nil, fmt.Errorf("prompt segment %d is empty", ordinal)
		}
		result[index] = content
	}
	return result, nil
}

func (a Activities) LoadPreparedShotVideoPlan(ctx context.Context, input LoadPreparedShotVideoPlanInput) (LoadPreparedShotVideoPlanOutput, error) {
	return a.loadPreparedShotVideoPlan(ctx, input)
}

func (a Activities) loadPreparedShotVideoPlan(ctx context.Context, input LoadPreparedShotVideoPlanInput) (LoadPreparedShotVideoPlanOutput, error) {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.ShotID) == "" {
		return LoadPreparedShotVideoPlanOutput{}, fmt.Errorf("organizationId, projectId, workflowRunId, and shotId are required")
	}
	shot, err := a.storyboardShot(ctx, input.ProjectID, input.WorkflowRunID, input.ShotID, input.ShotIndex)
	if err != nil {
		return LoadPreparedShotVideoPlanOutput{}, err
	}
	var plan PlanShotVideoOutput
	var snapshot []byte
	var expiresAt time.Time
	var promptStatus, planStatus, planAspectRatio, planResolution string
	var targetDurationTicks int64
	err = a.db.QueryRow(ctx, `
		SELECT plan.id::text, plan.provider_model_id::text, plan.provider_account_id::text,
		       plan.model_family, plan.variant_key, plan.capability_snapshot,
		       plan.capability_snapshot_hash, plan.timeline_timebase, plan.fps_numerator,
		       plan.fps_denominator, plan.expires_at, plan.audio_strategy,
		       plan.audio_requirement, plan.native_audio_status, plan.production_readiness,
		       plan.status, plan.aspect_ratio, plan.resolution, plan.target_duration_ticks,
		       COALESCE(shot.video_prompt_status, 'not_started')
		FROM storyboard_shots shot
		JOIN video_render_plans plan ON plan.id = shot.active_video_render_plan_id
		JOIN provider_models model ON model.id = plan.provider_model_id AND model.status = 'active'
		JOIN provider_accounts account ON account.id = plan.provider_account_id AND account.status = 'active'
		WHERE shot.id = $1 AND shot.project_id = $2 AND shot.organization_id = $3
		  AND shot.deleted_at IS NULL AND plan.active = true
		  AND plan.status NOT IN ('stale', 'archived', 'cancelled', 'replan_required')
	`, shot.ID, input.ProjectID, input.OrganizationID).Scan(
		&plan.ExecutionPlanID, &plan.ProviderModelID, &plan.ProviderAccountID,
		&plan.ModelFamily, &plan.VariantKey, &snapshot, &plan.CapabilitySnapshotHash,
		&plan.TimelineTimebase, &plan.FPSNumerator, &plan.FPSDenominator, &expiresAt,
		&plan.AudioStrategy, &plan.AudioRequirement, &plan.NativeAudioStatus, &plan.ProductionReadiness,
		&planStatus, &planAspectRatio, &planResolution, &targetDurationTicks, &promptStatus,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LoadPreparedShotVideoPlanOutput{}, preparedVideoPromptError("没有可执行的已审核视频提示词计划，请先批量生成视频提示词")
		}
		return LoadPreparedShotVideoPlanOutput{}, err
	}
	_ = planStatus
	if promptStatus != "succeeded" {
		return LoadPreparedShotVideoPlanOutput{}, preparedVideoPromptError("视频提示词尚未完成生成和审核，请先批量生成视频提示词")
	}
	if time.Now().UTC().After(expiresAt) {
		return LoadPreparedShotVideoPlanOutput{}, preparedVideoPromptError("视频提示词执行计划已过期，请重新生成视频提示词")
	}
	if targetDurationTicks != shot.PlannedDurationTicks || plan.TimelineTimebase != shot.TimelineTimebase || int(plan.FPSNumerator) != shot.FPSNumerator || int(plan.FPSDenominator) != shot.FPSDenominator {
		return LoadPreparedShotVideoPlanOutput{}, preparedVideoPromptError("分镜时长或帧率已变化，请重新生成视频提示词")
	}
	if value := strings.TrimSpace(input.AspectRatio); value != "" && !strings.EqualFold(value, planAspectRatio) {
		return LoadPreparedShotVideoPlanOutput{}, preparedVideoPromptError("项目视频比例已变化，请重新生成视频提示词")
	}
	if value := strings.TrimSpace(input.Resolution); value != "" && !strings.EqualFold(value, planResolution) {
		return LoadPreparedShotVideoPlanOutput{}, preparedVideoPromptError("视频清晰度已变化，请重新生成视频提示词")
	}
	if value := strings.TrimSpace(input.AudioStrategy); value != "" && !strings.EqualFold(value, plan.AudioStrategy) {
		return LoadPreparedShotVideoPlanOutput{}, preparedVideoPromptError("视频音频策略已变化，请重新生成视频提示词")
	}
	if value := strings.TrimSpace(input.AudioRequirement); value != "" && !strings.EqualFold(value, plan.AudioRequirement) {
		return LoadPreparedShotVideoPlanOutput{}, preparedVideoPromptError("视频音频要求已变化，请重新生成视频提示词")
	}
	if err := json.Unmarshal(snapshot, &plan.CapabilitySnapshot); err != nil {
		return LoadPreparedShotVideoPlanOutput{}, err
	}
	plan.ExpiresAt = expiresAt.UTC().Format(time.RFC3339Nano)
	rows, err := a.db.Query(ctx, `
		SELECT id::text, segment_index, planned_start_tick, planned_end_tick,
		       planned_duration_ticks, requested_duration_seconds::float8,
		       continuity_mode, COALESCE(trim_end_tick, 0), dialogue,
		       COALESCE(prompt, ''),
		       COALESCE(metadata #>> '{videoPromptAgent,status}', ''),
		       COALESCE(metadata #>> '{videoPromptAgent,negativePrompt}', ''),
		       COALESCE(metadata #>> '{videoPromptAgent,promptHash}', ''),
		       COALESCE(metadata #>> '{videoPromptAgent,generationProviderCallId}', ''),
		       COALESCE(metadata #>> '{videoPromptAgent,reviewProviderCallId}', ''),
		       COALESCE(metadata #>> '{videoPromptAgent,reviewTemplateKey}', ''),
		       COALESCE(metadata #>> '{videoPromptAgent,reviewPromptVersionId}', '')
		FROM video_render_segments
		WHERE video_render_plan_id = $1 AND storyboard_shot_id = $2 AND project_id = $3
		ORDER BY segment_index
	`, plan.ExecutionPlanID, shot.ID, input.ProjectID)
	if err != nil {
		return LoadPreparedShotVideoPlanOutput{}, err
	}
	defer rows.Close()
	segments := make([]PreparedShotVideoSegment, 0)
	for rows.Next() {
		var segment PreparedShotVideoSegment
		var dialogue []byte
		var promptReviewStatus string
		if err := rows.Scan(
			&segment.SegmentID, &segment.SegmentIndex, &segment.PlannedStartTick, &segment.PlannedEndTick,
			&segment.PlannedDurationTicks, &segment.RequestedDurationSeconds, &segment.ContinuityMode,
			&segment.TrimEndTick, &dialogue, &segment.Prompt, &promptReviewStatus, &segment.NegativePrompt,
			&segment.PromptHash, &segment.GenerationProviderCallID, &segment.ReviewProviderCallID,
			&segment.ReviewTemplateKey, &segment.ReviewPromptVersionID,
		); err != nil {
			return LoadPreparedShotVideoPlanOutput{}, err
		}
		segment.PlannedDurationSeconds = float64(segment.PlannedDurationTicks) / float64(plan.TimelineTimebase)
		segment.DialogueSpans = decodeStoredVideoDialogue(dialogue)
		if strings.TrimSpace(segment.Prompt) == "" || (promptReviewStatus != "approved" && promptReviewStatus != "manual_approved") || strings.TrimSpace(segment.PromptHash) == "" {
			return LoadPreparedShotVideoPlanOutput{}, preparedVideoPromptError(fmt.Sprintf("视频片段 %d 的提示词尚未完成审核，请重新生成视频提示词", segment.SegmentIndex+1))
		}
		segments = append(segments, segment)
		plan.Segments = append(plan.Segments, segment.GatewayVideoPlanSegment)
	}
	if err := rows.Err(); err != nil {
		return LoadPreparedShotVideoPlanOutput{}, err
	}
	if len(segments) == 0 {
		return LoadPreparedShotVideoPlanOutput{}, preparedVideoPromptError("视频执行计划没有渲染片段，请重新生成视频提示词")
	}
	return LoadPreparedShotVideoPlanOutput{Plan: plan, Segments: segments}, nil
}

func decodeStoredVideoDialogue(raw []byte) []provider.GatewayVideoDialogueSpan {
	var spans []provider.GatewayVideoDialogueSpan
	if len(raw) == 0 || json.Unmarshal(raw, &spans) != nil {
		return nil
	}
	if len(spans) > 0 && (spans[0].StartTick != 0 || spans[0].EndTick != 0) {
		return spans
	}
	var lines []StoryboardDialogueLine
	if json.Unmarshal(raw, &lines) != nil {
		return spans
	}
	result := make([]provider.GatewayVideoDialogueSpan, 0, len(lines))
	for _, line := range lines {
		result = append(result, provider.GatewayVideoDialogueSpan{
			TimingUnitID: line.TimingUnitID, Speaker: line.Speaker, Text: line.Text, Delivery: line.Delivery, Kind: line.Kind,
			StartTick: line.SpanStartTick, EndTick: line.SpanEndTick,
			ContinuesFromPrevious: line.ContinuesFromPrevious, ContinuesToNext: line.ContinuesToNext,
		})
	}
	return result
}

func preparedVideoPromptError(message string) error {
	cause := workflowError{Code: provider.CodeRenderPlanReplanRequired, Message: message, Retryable: false, RetryabilityKnown: true}
	return newWorkflowApplicationError(cause, provider.CodeRenderPlanReplanRequired, message)
}

func isPreparedVideoPromptPlanError(err error) bool {
	var applicationErr *temporal.ApplicationError
	return errors.As(err, &applicationErr) && applicationErr.Type() == provider.CodeRenderPlanReplanRequired
}

func gatewayVideoDialogueSpans(shot StoryboardShotRecord) ([]provider.GatewayVideoDialogueSpan, error) {
	lines := NormalizeStoryboardDialogue(shot.Dialogue)
	result := make([]provider.GatewayVideoDialogueSpan, 0, len(lines))
	for _, line := range lines {
		if line.SpanEndTick <= line.SpanStartTick || line.SpanStartTick < shot.StartTick || line.SpanEndTick > shot.EndTick {
			return nil, workflowError{Code: provider.CodeStoryboardReplanRequired, Message: "storyboard dialogue is missing an exact frame-aligned timing span"}
		}
		result = append(result, provider.GatewayVideoDialogueSpan{
			TimingUnitID: line.TimingUnitID, Speaker: line.Speaker, Text: line.Text, Delivery: line.Delivery, Kind: line.Kind,
			StartTick: line.SpanStartTick - shot.StartTick, EndTick: line.SpanEndTick - shot.StartTick,
			ContinuesFromPrevious: line.ContinuesFromPrevious, ContinuesToNext: line.ContinuesToNext,
		})
	}
	return result, nil
}

type RetryShotVideoRenderSegmentInput struct {
	OrganizationID         string `json:"organizationId"`
	ProjectID              string `json:"projectId"`
	WorkflowRunID          string `json:"workflowRunId"`
	ExecutionPlanID        string `json:"executionPlanId"`
	RenderSegmentID        string `json:"renderSegmentId"`
	CurrentRetryGeneration int    `json:"currentRetryGeneration"`
	FailureCode            string `json:"failureCode,omitempty"`
	FailureMessage         string `json:"failureMessage,omitempty"`
}

type RetryShotVideoRenderSegmentOutput struct {
	NodeExecution
	provider.GatewayVideoRetrySegmentResponse
}

func (a Activities) RetryShotVideoRenderSegment(ctx context.Context, input RetryShotVideoRenderSegmentInput) (RetryShotVideoRenderSegmentOutput, error) {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.ExecutionPlanID) == "" || strings.TrimSpace(input.RenderSegmentID) == "" {
		return RetryShotVideoRenderSegmentOutput{}, newWorkflowApplicationError(
			workflowError{Code: provider.CodeInvalidRequest, Message: "organizationId, projectId, workflowRunId, executionPlanId, and renderSegmentId are required", Retryable: false, RetryabilityKnown: true},
			provider.CodeInvalidRequest,
			"video render segment retry input is incomplete",
		)
	}
	execution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey: fmt.Sprintf(
			"retry_shot_video_segment_%s_g%d",
			input.RenderSegmentID,
			input.CurrentRetryGeneration+1,
		),
		NodeType: "video.retry",
		Input: mustJSON(map[string]any{
			"executionPlanId":        input.ExecutionPlanID,
			"renderSegmentId":        input.RenderSegmentID,
			"currentRetryGeneration": input.CurrentRetryGeneration,
			"failureCode":            input.FailureCode,
			"failureMessage":         input.FailureMessage,
		}),
	})
	if err != nil {
		return RetryShotVideoRenderSegmentOutput{}, err
	}
	fail := func(cause error) (RetryShotVideoRenderSegmentOutput, error) {
		workflowErr := workflowErrorFromProvider(cause, provider.CodeRenderPlanReplanRequired)
		code, message := workflowErrorFields(workflowErr, provider.CodeRenderPlanReplanRequired)
		if failErr := FailNodeRun(ctx, a.db, execution, code, message); failErr != nil && !errors.Is(failErr, ErrWorkflowWriteFenced) {
			return RetryShotVideoRenderSegmentOutput{}, failErr
		}
		return RetryShotVideoRenderSegmentOutput{}, newWorkflowApplicationError(workflowErr, code, message)
	}
	if a.gateway == nil {
		return fail(newWorkflowApplicationError(
			workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"},
			provider.CodeProviderGatewayRequired,
			"provider gateway client is not configured",
		))
	}
	response, err := a.gateway.RetryVideoRenderSegment(ctx, provider.GatewayVideoRetrySegmentRequest{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		NodeRunID: execution.NodeRunID, NodeExecutionToken: execution.ExecutionToken, NodeAttemptGeneration: execution.AttemptGeneration,
		ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID,
		FailureCode: input.FailureCode, FailureMessage: input.FailureMessage,
	})
	if err != nil {
		return fail(err)
	}
	output := RetryShotVideoRenderSegmentOutput{NodeExecution: execution, GatewayVideoRetrySegmentResponse: response}
	if err := CompleteNodeRun(ctx, a.db, execution, mustJSON(output)); err != nil {
		return RetryShotVideoRenderSegmentOutput{}, err
	}
	return output, nil
}
