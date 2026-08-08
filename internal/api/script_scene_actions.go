package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/production"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type scriptScenePatch struct {
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

type scriptSceneUpdateActionInput struct {
	SceneID          string           `json:"sceneId"`
	ExpectedRevision int64            `json:"expectedRevision"`
	Patch            scriptScenePatch `json:"patch"`
}

type scriptSceneReviewActionInput struct {
	SceneID          string `json:"sceneId"`
	ExpectedRevision int64  `json:"expectedRevision"`
	ReviewStatus     string `json:"reviewStatus"`
	Note             string `json:"note"`
}

type scriptSceneDeleteActionInput struct {
	SceneID          string `json:"sceneId"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Reason           string `json:"reason"`
}

type scriptSceneReviewActionOutcome struct {
	ID           string    `json:"id"`
	Revision     int64     `json:"revision"`
	ReviewStatus string    `json:"reviewStatus"`
	Note         *string   `json:"note,omitempty"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type scriptSceneDeleteActionOutcome struct {
	Deleted  bool   `json:"deleted"`
	Mode     string `json:"mode"`
	SceneID  string `json:"sceneId"`
	Revision int64  `json:"revision"`
}

func decodeScriptSceneUpdateActionInput(raw json.RawMessage) (scriptSceneUpdateActionInput, error) {
	var input scriptSceneUpdateActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return scriptSceneUpdateActionInput{}, controlValidationError("script_scene.update 输入格式无效")
	}
	input.SceneID = strings.TrimSpace(input.SceneID)
	if uuid.Validate(input.SceneID) != nil {
		return scriptSceneUpdateActionInput{}, controlValidationError("sceneId 无效")
	}
	if input.ExpectedRevision < 1 {
		return scriptSceneUpdateActionInput{}, controlValidationError("expectedRevision 必须大于 0")
	}
	if scriptScenePatchEmpty(input.Patch) {
		return scriptSceneUpdateActionInput{}, controlValidationError("patch 至少需要一个可修改字段")
	}
	return input, nil
}

func decodeScriptSceneReviewActionInput(raw json.RawMessage) (scriptSceneReviewActionInput, error) {
	var input scriptSceneReviewActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return scriptSceneReviewActionInput{}, controlValidationError("script_scene.review 输入格式无效")
	}
	input.SceneID = strings.TrimSpace(input.SceneID)
	input.ReviewStatus = strings.TrimSpace(input.ReviewStatus)
	input.Note = strings.TrimSpace(input.Note)
	if uuid.Validate(input.SceneID) != nil {
		return scriptSceneReviewActionInput{}, controlValidationError("sceneId 无效")
	}
	if input.ExpectedRevision < 1 {
		return scriptSceneReviewActionInput{}, controlValidationError("expectedRevision 必须大于 0")
	}
	if !validReviewStatus(input.ReviewStatus) {
		return scriptSceneReviewActionInput{}, controlValidationError("reviewStatus 无效")
	}
	return input, nil
}

func decodeScriptSceneDeleteActionInput(raw json.RawMessage) (scriptSceneDeleteActionInput, error) {
	var input scriptSceneDeleteActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return scriptSceneDeleteActionInput{}, controlValidationError("script_scene.delete 输入格式无效")
	}
	input.SceneID = strings.TrimSpace(input.SceneID)
	input.Reason = strings.TrimSpace(input.Reason)
	if uuid.Validate(input.SceneID) != nil {
		return scriptSceneDeleteActionInput{}, controlValidationError("sceneId 无效")
	}
	if input.ExpectedRevision < 1 {
		return scriptSceneDeleteActionInput{}, controlValidationError("expectedRevision 必须大于 0")
	}
	return input, nil
}

func scriptScenePatchEmpty(patch scriptScenePatch) bool {
	return patch.Title == nil && patch.Summary == nil && patch.Location == nil && patch.TimeOfDay == nil &&
		patch.Atmosphere == nil && patch.Characters == nil && patch.Scenes == nil && patch.Props == nil &&
		patch.Action == nil && patch.Dialogue == nil && patch.VisualGoal == nil && patch.EmotionalTone == nil &&
		patch.Conflict == nil && patch.Outcome == nil && patch.Content == nil
}

func (s *Server) updateScriptSceneActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input scriptSceneUpdateActionInput,
) (workflows.ScriptSceneRecord, error) {
	current, err := lockScriptSceneForAction(ctx, tx, project.ID, input.SceneID)
	if err != nil {
		return workflows.ScriptSceneRecord{}, err
	}
	if err := validateScriptSceneRevision(current, input.ExpectedRevision); err != nil {
		return workflows.ScriptSceneRecord{}, err
	}
	applyScriptScenePatch(&current, input.Patch)
	if current.Title == "" {
		return workflows.ScriptSceneRecord{}, controlValidationError("title 不能为空")
	}
	item, err := workflows.ScanScriptSceneRecord(tx.QueryRow(ctx, `
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
		WHERE project_id = $1 AND id = $2 AND revision = $19 AND deleted_at IS NULL
		RETURNING `+workflows.ScriptSceneColumns()+`
	`, project.ID, current.ID, current.Title, current.Summary, current.Location, current.TimeOfDay, current.Atmosphere,
		current.Characters, current.Scenes, current.Props, current.Action, current.Dialogue, current.VisualGoal,
		current.EmotionalTone, current.Conflict, current.Outcome, current.Content, actorUserID, input.ExpectedRevision))
	if errors.Is(err, pgx.ErrNoRows) {
		return workflows.ScriptSceneRecord{}, scriptSceneRevisionConflict(input.ExpectedRevision, current)
	}
	if err != nil {
		return workflows.ScriptSceneRecord{}, err
	}
	if err := markScriptSceneDownstreamStale(ctx, tx, project.ID, item.ID); err != nil {
		return workflows.ScriptSceneRecord{}, err
	}
	if err := production.MarkFinalVideoStale(ctx, tx, project.ID, ""); err != nil {
		return workflows.ScriptSceneRecord{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "script.scene.updated", "script_scene", item.ID, mustRawJSON(map[string]any{
		"scriptSceneId": item.ID, "scriptId": item.ScriptID, "revision": item.Revision,
		"manualOverride": item.ManualOverride, "staleState": item.StaleState,
	})); err != nil {
		return workflows.ScriptSceneRecord{}, err
	}
	return item, nil
}

func (s *Server) reviewScriptSceneActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input scriptSceneReviewActionInput,
) (scriptSceneReviewActionOutcome, error) {
	current, err := lockScriptSceneForAction(ctx, tx, project.ID, input.SceneID)
	if err != nil {
		return scriptSceneReviewActionOutcome{}, err
	}
	if err := validateScriptSceneRevision(current, input.ExpectedRevision); err != nil {
		return scriptSceneReviewActionOutcome{}, err
	}
	var outcome scriptSceneReviewActionOutcome
	err = tx.QueryRow(ctx, `
		UPDATE script_scenes
		SET review_status = $3,
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'reviewStatus', $3,
		      'reviewNote', $4,
		      'reviewedBy', $5,
		      'reviewedAt', now()
		    ),
		    updated_at = now()
		WHERE project_id = $1 AND id = $2 AND revision = $6 AND deleted_at IS NULL
		RETURNING id::text, revision, review_status, updated_at
	`, project.ID, current.ID, input.ReviewStatus, input.Note, actorUserID, input.ExpectedRevision).Scan(
		&outcome.ID, &outcome.Revision, &outcome.ReviewStatus, &outcome.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return scriptSceneReviewActionOutcome{}, scriptSceneRevisionConflict(input.ExpectedRevision, current)
	}
	if err != nil {
		return scriptSceneReviewActionOutcome{}, err
	}
	outcome.Note = stringPtrFromValue(input.Note)
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "script.scene.reviewed", "script_scene", current.ID, mustRawJSON(map[string]any{
		"scriptSceneId": current.ID, "scriptId": current.ScriptID, "reviewStatus": input.ReviewStatus,
		"note": input.Note, "revision": outcome.Revision,
	})); err != nil {
		return scriptSceneReviewActionOutcome{}, err
	}
	return outcome, nil
}

func (s *Server) deleteScriptSceneActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input scriptSceneDeleteActionInput,
) (scriptSceneDeleteActionOutcome, error) {
	current, err := lockScriptSceneForAction(ctx, tx, project.ID, input.SceneID)
	if err != nil {
		return scriptSceneDeleteActionOutcome{}, err
	}
	if err := validateScriptSceneRevision(current, input.ExpectedRevision); err != nil {
		return scriptSceneDeleteActionOutcome{}, err
	}
	if err := markScriptSceneDownstreamStale(ctx, tx, project.ID, current.ID); err != nil {
		return scriptSceneDeleteActionOutcome{}, err
	}
	var revision int64
	err = tx.QueryRow(ctx, `
		UPDATE script_scenes
		SET deleted_at = now(),
		    stale_state = 'needs_regeneration',
		    manual_override = true,
		    edited_by = $3,
		    edited_at = now(),
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'archiveReason', $4::text, 'archivedBy', $3::text, 'archivedAt', now()
		    ),
		    updated_at = now()
		WHERE project_id = $1 AND id = $2 AND revision = $5 AND deleted_at IS NULL
		RETURNING revision
	`, project.ID, current.ID, actorUserID, input.Reason, input.ExpectedRevision).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return scriptSceneDeleteActionOutcome{}, scriptSceneRevisionConflict(input.ExpectedRevision, current)
	}
	if err != nil {
		return scriptSceneDeleteActionOutcome{}, err
	}
	if err := production.MarkFinalVideoStale(ctx, tx, project.ID, ""); err != nil {
		return scriptSceneDeleteActionOutcome{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "script.scene.archived", "script_scene", current.ID, mustRawJSON(map[string]any{
		"scriptSceneId": current.ID, "scriptId": current.ScriptID, "versionId": current.ScriptVersionID,
		"reason": input.Reason, "revision": revision,
	})); err != nil {
		return scriptSceneDeleteActionOutcome{}, err
	}
	return scriptSceneDeleteActionOutcome{Deleted: true, Mode: "archive", SceneID: current.ID, Revision: revision}, nil
}

func lockScriptSceneForAction(ctx context.Context, tx pgx.Tx, projectID, sceneID string) (workflows.ScriptSceneRecord, error) {
	item, err := workflows.ScanScriptSceneRecord(tx.QueryRow(ctx, workflows.ScriptSceneSelectSQL(`
		WHERE project_id = $1 AND id = $2 AND deleted_at IS NULL
		FOR UPDATE
	`), projectID, sceneID))
	if errors.Is(err, pgx.ErrNoRows) {
		return workflows.ScriptSceneRecord{}, newAPIError(http.StatusNotFound, "SCRIPT_SCENE_NOT_FOUND", "未找到剧本场景")
	}
	return item, err
}

func validateScriptSceneRevision(current workflows.ScriptSceneRecord, expectedRevision int64) error {
	if current.Revision != expectedRevision {
		return scriptSceneRevisionConflict(expectedRevision, current)
	}
	return nil
}

func scriptSceneRevisionConflict(expectedRevision int64, current workflows.ScriptSceneRecord) error {
	return apiError{
		Status: http.StatusConflict, Code: "SCRIPT_SCENE_REVISION_CONFLICT",
		Message: "剧本场景已被其他操作更新，请重新读取后重试", Retryable: true,
		Details: map[string]any{
			"expectedRevision": expectedRevision, "currentRevision": current.Revision,
			"sceneId": current.ID, "updatedAt": current.UpdatedAt,
		},
	}
}

func applyScriptScenePatch(current *workflows.ScriptSceneRecord, patch scriptScenePatch) {
	setTrimmed := func(target *string, value *string) {
		if value != nil {
			*target = strings.TrimSpace(*value)
		}
	}
	setTrimmed(&current.Title, patch.Title)
	setTrimmed(&current.Summary, patch.Summary)
	setTrimmed(&current.Location, patch.Location)
	setTrimmed(&current.TimeOfDay, patch.TimeOfDay)
	setTrimmed(&current.Atmosphere, patch.Atmosphere)
	setTrimmed(&current.Action, patch.Action)
	setTrimmed(&current.Dialogue, patch.Dialogue)
	setTrimmed(&current.VisualGoal, patch.VisualGoal)
	setTrimmed(&current.EmotionalTone, patch.EmotionalTone)
	setTrimmed(&current.Conflict, patch.Conflict)
	setTrimmed(&current.Outcome, patch.Outcome)
	setTrimmed(&current.Content, patch.Content)
	if patch.Characters != nil {
		current.Characters = json.RawMessage(mustMarshal(normalizeStringSlice(*patch.Characters)))
	}
	if patch.Scenes != nil {
		current.Scenes = json.RawMessage(mustMarshal(normalizeStringSlice(*patch.Scenes)))
	}
	if patch.Props != nil {
		current.Props = json.RawMessage(mustMarshal(normalizeStringSlice(*patch.Props)))
	}
}

func scriptSceneUpdateAgentResult(arguments map[string]any, item workflows.ScriptSceneRecord) agentToolResult {
	return agentToolOK("script_scene.update", arguments, "已更新剧本场景并标记下游需要重生成。", map[string]any{
		"scene": item, "sceneId": item.ID, "revision": item.Revision,
	})
}

func scriptSceneReviewAgentResult(arguments map[string]any, outcome scriptSceneReviewActionOutcome) agentToolResult {
	return agentToolOK("script_scene.review", arguments, "已更新剧本场景审核状态。", map[string]any{
		"id": outcome.ID, "revision": outcome.Revision, "reviewStatus": outcome.ReviewStatus,
		"note": outcome.Note, "updatedAt": outcome.UpdatedAt,
	})
}

func scriptSceneDeleteAgentResult(arguments map[string]any, outcome scriptSceneDeleteActionOutcome) agentToolResult {
	return agentToolOK("script_scene.delete", arguments, "已归档剧本场景并标记下游需要重生成。", map[string]any{
		"deleted": outcome.Deleted, "mode": outcome.Mode, "sceneId": outcome.SceneID, "revision": outcome.Revision,
	})
}
