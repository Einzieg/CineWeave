package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/production"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	storyboardtiming "github.com/Einzieg/cineweave/internal/storyboard"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/jackc/pgx/v5"
)

type storyboardCreateShotActionInput struct {
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

type storyboardCreateShotControlInput struct {
	Shot storyboardCreateShotActionInput `json:"shot"`
}

type storyboardDeleteShotActionInput struct {
	ShotID string `json:"shotId"`
}

type storyboardReviewShotActionInput struct {
	ShotID       string `json:"shotId"`
	ReviewStatus string `json:"reviewStatus"`
	Note         string `json:"note,omitempty"`
}

type storyboardUnlinkMediaActionInput struct {
	ShotID string `json:"shotId"`
	Kind   string `json:"kind"`
}

type storyboardReviewAnchorActionInput struct {
	ShotID           string `json:"shotId"`
	AnchorID         string `json:"anchorId"`
	ExpectedRevision int    `json:"expectedRevision"`
	Reason           string `json:"reason,omitempty"`
}

type storyboardReorderItem struct {
	ShotID           string `json:"shotId"`
	ShotIndex        int    `json:"shotIndex"`
	ShotNo           int    `json:"shotNo"`
	EpisodeShotIndex *int   `json:"episodeShotIndex,omitempty"`
}

type storyboardReorderActionInput struct {
	Items   []storyboardReorderItem `json:"items,omitempty"`
	ShotIDs []string                `json:"shotIds,omitempty"`
}

func (s *Server) createStoryboardShotActionTx(ctx context.Context, tx pgx.Tx, project Project, actorID string, req storyboardCreateShotActionInput) (StoryboardShot, error) {
	if req.ShotIndex != nil && *req.ShotIndex < 0 {
		return StoryboardShot{}, controlValidationError("shotIndex 不能小于 0")
	}
	if req.ShotNo != nil && *req.ShotNo <= 0 {
		return StoryboardShot{}, controlValidationError("shotNo 必须大于 0")
	}
	productionContext, err := lockActiveVideoProductionContext(ctx, tx, project.ID)
	if err != nil {
		return StoryboardShot{}, err
	}
	productionConfiguration, err := videoproduction.DecodeProductionConfiguration(productionContext.Binding.ProfileSnapshot)
	if err != nil {
		return StoryboardShot{}, err
	}
	workflowRunID, err := storyboardWorkflowRunForCreateTx(
		ctx, tx, project.OrganizationID, project.ID, productionContext,
		strings.TrimSpace(req.WorkflowRunID), actorID,
	)
	if err != nil {
		return StoryboardShot{}, err
	}
	if strings.TrimSpace(req.ScriptSceneID) != "" {
		if _, err := workflows.ScanScriptSceneRecord(tx.QueryRow(ctx, workflows.ScriptSceneSelectSQL(`
			WHERE project_id = $1 AND id = $2 AND deleted_at IS NULL
		`), project.ID, strings.TrimSpace(req.ScriptSceneID))); err != nil {
			return StoryboardShot{}, err
		}
	}
	shotIndex, shotNo, err := nextStoryboardShotPositionTx(
		ctx, tx, project.ID, productionContext.Generation.ID, workflowRunID, req.ShotIndex, req.ShotNo,
	)
	if err != nil {
		return StoryboardShot{}, err
	}
	timebase := storyboardtiming.Timebase{
		TicksPerSecond: productionConfiguration.TimelineTimebase,
		FPSNumerator:   int64(productionConfiguration.FPSNumerator), FPSDenominator: int64(productionConfiguration.FPSDenominator),
	}
	if err := timebase.Validate(); err != nil {
		return StoryboardShot{}, err
	}
	startTick := int64(0)
	if req.StartTick != nil {
		startTick = *req.StartTick
	} else if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(end_tick), 0)
		FROM storyboard_shots
		WHERE project_id = $1 AND production_generation_id = $2
		  AND workflow_run_id = $3 AND deleted_at IS NULL
	`, project.ID, productionContext.Generation.ID, workflowRunID).Scan(&startTick); err != nil {
		return StoryboardShot{}, err
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
		return StoryboardShot{}, controlValidationError("镜头时间必须为正数且对齐项目帧率")
	}
	var shotID string
	if err := tx.QueryRow(ctx, `
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
		strings.TrimSpace(req.ImagePrompt), strings.TrimSpace(req.VideoPrompt), actorID,
		productionConfiguration.TimelineTimebase, productionConfiguration.FPSNumerator,
		productionConfiguration.FPSDenominator, productionContext.Generation.ID).Scan(&shotID); err != nil {
		return StoryboardShot{}, err
	}
	item, err := scanStoryboardShot(tx.QueryRow(ctx, storyboardShotSelectSQL(`
		WHERE s.project_id = $1 AND s.id = $2 AND s.deleted_at IS NULL
	`), project.ID, shotID))
	if err != nil {
		return StoryboardShot{}, err
	}
	if err := production.MarkFinalVideoStale(ctx, tx, project.ID, workflowRunID); err != nil {
		return StoryboardShot{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "storyboard.shot.created", "storyboard_shot", item.ID, mustRawJSON(map[string]any{
		"shotId": item.ID, "workflowRunId": workflowRunID, "scriptSceneId": item.ScriptSceneID,
		"shotNo": item.ShotNo, "productionGenerationId": productionContext.Generation.ID,
		"bindingId": productionContext.Binding.ID, "bindingRevision": productionContext.Binding.Revision,
	})); err != nil {
		return StoryboardShot{}, err
	}
	return item, nil
}

func (s *Server) deleteStoryboardShotActionTx(ctx context.Context, tx pgx.Tx, project Project, shotID string) (StoryboardShot, error) {
	shotID = strings.TrimSpace(shotID)
	if shotID == "" {
		return StoryboardShot{}, controlValidationError("shotId 不能为空")
	}
	current, err := scanStoryboardShot(tx.QueryRow(ctx, storyboardShotSelectSQL(`
		WHERE s.project_id = $1 AND s.id = $2 AND s.deleted_at IS NULL
		FOR UPDATE OF s
	`), project.ID, shotID))
	if err != nil {
		return StoryboardShot{}, err
	}
	if current.StoryboardPlanID != nil {
		apiErr := newAPIError(http.StatusConflict, "STORYBOARD_PLAN_REVISION_REQUIRED", "storyboard plan shots cannot be deleted in place; create a plan revision instead")
		apiErr.Details = map[string]any{"storyboardPlanId": *current.StoryboardPlanID, "shotId": current.ID}
		return StoryboardShot{}, apiErr
	}
	tag, err := tx.Exec(ctx, `
		UPDATE storyboard_shots SET deleted_at = now(), updated_at = now()
		WHERE project_id = $1 AND id = $2 AND deleted_at IS NULL
	`, project.ID, current.ID)
	if err != nil {
		return StoryboardShot{}, err
	}
	if tag.RowsAffected() == 0 {
		return StoryboardShot{}, pgx.ErrNoRows
	}
	if err := reflowStoryboardShotTicksTx(ctx, tx, project.ID); err != nil {
		return StoryboardShot{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_plans
		SET active = false,
		    status = CASE WHEN status = 'ready' THEN 'archived' ELSE status END,
		    stale_state = 'upstream_changed',
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('shotDeletedAt', now())
		WHERE project_id = $1 AND active = true
	`, project.ID); err != nil {
		return StoryboardShot{}, err
	}
	if err := production.MarkFinalVideoStale(ctx, tx, project.ID, current.WorkflowRunID); err != nil {
		return StoryboardShot{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "storyboard.shot.deleted", "storyboard_shot", current.ID, mustRawJSON(map[string]any{
		"shotId": current.ID, "workflowRunId": current.WorkflowRunID,
	})); err != nil {
		return StoryboardShot{}, err
	}
	return current, nil
}

func normalizeStoryboardReorderInput(input storyboardReorderActionInput) (storyboardReorderActionInput, error) {
	if len(input.Items) == 0 {
		input.ShotIDs = uniqueNonEmptyStrings(input.ShotIDs)
		input.Items = make([]storyboardReorderItem, 0, len(input.ShotIDs))
		for index, shotID := range input.ShotIDs {
			input.Items = append(input.Items, storyboardReorderItem{ShotID: shotID, ShotIndex: index, ShotNo: index + 1})
		}
	}
	if len(input.Items) == 0 {
		return storyboardReorderActionInput{}, controlValidationError("items 或 shotIds 不能为空")
	}
	seen := map[string]struct{}{}
	for index := range input.Items {
		item := &input.Items[index]
		item.ShotID = strings.TrimSpace(item.ShotID)
		if item.ShotID == "" || item.ShotIndex < 0 || item.ShotNo <= 0 || (item.EpisodeShotIndex != nil && *item.EpisodeShotIndex < 0) {
			return storyboardReorderActionInput{}, controlValidationError("shotId、非负 shotIndex 和正数 shotNo 为必填项")
		}
		if _, exists := seen[item.ShotID]; exists {
			return storyboardReorderActionInput{}, controlValidationError("shotIds 不能重复")
		}
		seen[item.ShotID] = struct{}{}
	}
	return input, nil
}

func (s *Server) reorderStoryboardShotsActionTx(ctx context.Context, tx pgx.Tx, project Project, input storyboardReorderActionInput) ([]storyboardReorderItem, error) {
	input, err := normalizeStoryboardReorderInput(input)
	if err != nil {
		return nil, err
	}
	workflowRunIDs := map[string]bool{}
	for _, item := range input.Items {
		var storyboardPlanID *string
		if err := tx.QueryRow(ctx, `
			SELECT storyboard_plan_id::text FROM storyboard_shots
			WHERE project_id = $1 AND id = $2 AND deleted_at IS NULL
			FOR UPDATE
		`, project.ID, item.ShotID).Scan(&storyboardPlanID); err != nil {
			return nil, err
		}
		if storyboardPlanID != nil {
			apiErr := newAPIError(http.StatusConflict, "STORYBOARD_PLAN_REVISION_REQUIRED", "storyboard plan shots cannot be reordered in place; use split, merge, or timing revision endpoints")
			apiErr.Details = map[string]any{"storyboardPlanId": *storyboardPlanID, "shotId": item.ShotID}
			return nil, apiErr
		}
	}
	for index, item := range input.Items {
		var workflowRunID string
		if err := tx.QueryRow(ctx, `
			UPDATE storyboard_shots
			SET shot_index = $3, manual_override = true, stale_state = 'needs_regeneration', updated_at = now()
			WHERE project_id = $1 AND id = $2 AND deleted_at IS NULL
			RETURNING COALESCE(workflow_run_id::text, '')
		`, project.ID, item.ShotID, -(index + 1)).Scan(&workflowRunID); err != nil {
			return nil, err
		}
		if strings.TrimSpace(workflowRunID) != "" {
			workflowRunIDs[workflowRunID] = true
		}
	}
	for _, item := range input.Items {
		if _, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET shot_index = $3, shot_no = $4,
			    episode_shot_index = COALESCE($5, episode_shot_index), updated_at = now()
			WHERE project_id = $1 AND id = $2 AND deleted_at IS NULL
		`, project.ID, item.ShotID, item.ShotIndex, item.ShotNo, item.EpisodeShotIndex); err != nil {
			return nil, err
		}
	}
	if err := reflowStoryboardShotTicksTx(ctx, tx, project.ID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_plans
		SET active = false,
		    status = CASE WHEN status = 'ready' THEN 'archived' ELSE status END,
		    stale_state = 'upstream_changed',
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('shotsReorderedAt', now())
		WHERE project_id = $1 AND active = true
	`, project.ID); err != nil {
		return nil, err
	}
	for workflowRunID := range workflowRunIDs {
		if err := production.MarkFinalVideoStale(ctx, tx, project.ID, workflowRunID); err != nil {
			return nil, err
		}
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "storyboard.shots.reordered", "project", project.ID, mustRawJSON(map[string]any{"items": input.Items})); err != nil {
		return nil, err
	}
	return input.Items, nil
}

func (s *Server) reviewStoryboardShotActionTx(ctx context.Context, tx pgx.Tx, project Project, actorID string, input storyboardReviewShotActionInput) (ReviewResponse, error) {
	input.ShotID = strings.TrimSpace(input.ShotID)
	input.ReviewStatus = strings.TrimSpace(input.ReviewStatus)
	input.Note = strings.TrimSpace(input.Note)
	if input.ShotID == "" || !validReviewStatus(input.ReviewStatus) {
		return ReviewResponse{}, controlValidationError("shotId 和有效的 reviewStatus 为必填项")
	}
	var response ReviewResponse
	if err := tx.QueryRow(ctx, `
		UPDATE storyboard_shots
		SET review_status = $3,
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'reviewStatus', $3,
		      'reviewNote', $4,
		      'reviewedBy', $5,
		      'reviewedAt', now()
		    )
		WHERE project_id = $1 AND id = $2 AND deleted_at IS NULL
		RETURNING id::text, review_status, updated_at
	`, project.ID, input.ShotID, input.ReviewStatus, input.Note, actorID).Scan(
		&response.ID, &response.ReviewStatus, &response.UpdatedAt,
	); err != nil {
		return ReviewResponse{}, err
	}
	response.Note = stringPtrFromValue(input.Note)
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "storyboard.shot.reviewed", "storyboard_shot", response.ID, mustRawJSON(map[string]any{
		"shotId": response.ID, "reviewStatus": response.ReviewStatus, "note": input.Note, "reviewedBy": actorID,
	})); err != nil {
		return ReviewResponse{}, err
	}
	return response, nil
}

func (s *Server) unlinkStoryboardShotMediaActionTx(ctx context.Context, tx pgx.Tx, project Project, input storyboardUnlinkMediaActionInput) (StoryboardShot, error) {
	input.ShotID = strings.TrimSpace(input.ShotID)
	input.Kind = strings.TrimSpace(input.Kind)
	if input.ShotID == "" || (input.Kind != "image" && input.Kind != "video") {
		return StoryboardShot{}, controlValidationError("shotId 为必填项，kind 必须是 image 或 video")
	}
	current, err := scanStoryboardShot(tx.QueryRow(ctx, storyboardShotSelectSQL(`
		WHERE s.project_id = $1 AND s.id = $2 AND s.deleted_at IS NULL
		FOR UPDATE OF s
	`), project.ID, input.ShotID))
	if err != nil {
		return StoryboardShot{}, err
	}
	if input.Kind == "image" {
		_, err = tx.Exec(ctx, `
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
			WHERE project_id = $1 AND id = $2 AND deleted_at IS NULL
		`, project.ID, current.ID)
	} else {
		_, err = tx.Exec(ctx, `
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
			WHERE project_id = $1 AND id = $2 AND deleted_at IS NULL
		`, project.ID, current.ID)
	}
	if err != nil {
		return StoryboardShot{}, err
	}
	item, err := scanStoryboardShot(tx.QueryRow(ctx, storyboardShotSelectSQL(`
		WHERE s.project_id = $1 AND s.id = $2 AND s.deleted_at IS NULL
	`), project.ID, current.ID))
	if err != nil {
		return StoryboardShot{}, err
	}
	if err := production.MarkFinalVideoStale(ctx, tx, project.ID, current.WorkflowRunID); err != nil {
		return StoryboardShot{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "storyboard.shot.media.unlinked", "storyboard_shot", item.ID, mustRawJSON(map[string]any{
		"shotId": item.ID, "kind": input.Kind,
	})); err != nil {
		return StoryboardShot{}, err
	}
	return item, nil
}

func (s *Server) executeStoryboardCreateShotSyncAction(ctx context.Context, tx pgx.Tx, principal auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	var input storyboardCreateShotControlInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	item, err := s.createStoryboardShotActionTx(ctx, tx, project, principal.UserID, input.Shot)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK(command.ActionName, workflowActionArguments(raw), "已新增分镜镜头。", map[string]any{"shot": item}), nil
}

func (s *Server) executeStoryboardDeleteShotSyncAction(ctx context.Context, tx pgx.Tx, _ auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	var input storyboardDeleteShotActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	item, err := s.deleteStoryboardShotActionTx(ctx, tx, project, input.ShotID)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK(command.ActionName, workflowActionArguments(raw), "已删除分镜镜头并标记下游需重新生成。", map[string]any{"deleted": true, "shotId": item.ID}), nil
}

func (s *Server) executeStoryboardReorderSyncAction(ctx context.Context, tx pgx.Tx, _ auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	var input storyboardReorderActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	items, err := s.reorderStoryboardShotsActionTx(ctx, tx, project, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK(command.ActionName, workflowActionArguments(raw), fmt.Sprintf("已重排 %d 个分镜镜头。", len(items)), map[string]any{"items": items}), nil
}

func (s *Server) executeStoryboardReviewShotSyncAction(ctx context.Context, tx pgx.Tx, principal auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	var input storyboardReviewShotActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	item, err := s.reviewStoryboardShotActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK(command.ActionName, workflowActionArguments(raw), "已更新分镜镜头审核状态。", map[string]any{"item": item}), nil
}

func (s *Server) executeStoryboardUnlinkMediaSyncAction(ctx context.Context, tx pgx.Tx, _ auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	var input storyboardUnlinkMediaActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	item, err := s.unlinkStoryboardShotMediaActionTx(ctx, tx, project, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK(command.ActionName, workflowActionArguments(raw), "已解除镜头媒体绑定并标记下游需重新生成。", map[string]any{"shot": item, "kind": input.Kind}), nil
}

func (s *Server) executeStoryboardReviewAnchorSyncAction(decision string) projectControlSyncAction {
	return func(ctx context.Context, tx pgx.Tx, principal auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
		var input storyboardReviewAnchorActionInput
		if err := decodeControlInput(raw, &input); err != nil {
			return agentToolResult{}, err
		}
		item, err := s.reviewStoryboardShotAnchorActionTx(ctx, tx, project, principal.UserID, input.ShotID, input.AnchorID, decision, reviewShotVisualAnchorRequest{
			ExpectedRevision: input.ExpectedRevision, Reason: input.Reason,
		})
		if err != nil {
			return agentToolResult{}, err
		}
		return agentToolOK(command.ActionName, workflowActionArguments(raw), "已更新镜头锚点审核状态。", map[string]any{"anchor": item, "decision": decision}), nil
	}
}
