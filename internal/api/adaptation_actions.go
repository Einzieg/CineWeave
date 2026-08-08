package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type novelEventPatch struct {
	Title          *string   `json:"title"`
	Summary        *string   `json:"summary"`
	EventType      *string   `json:"eventType"`
	Importance     *int      `json:"importance"`
	TimelineHint   *string   `json:"timelineHint"`
	LocationHint   *string   `json:"locationHint"`
	EmotionalTone  *string   `json:"emotionalTone"`
	Conflict       *string   `json:"conflict"`
	Outcome        *string   `json:"outcome"`
	AdaptationHint *string   `json:"adaptationHint"`
	Characters     *[]string `json:"characters"`
	Scenes         *[]string `json:"scenes"`
	Props          *[]string `json:"props"`
	Keywords       *[]string `json:"keywords"`
	RawExcerpt     *string   `json:"rawExcerpt"`
	ReviewStatus   *string   `json:"reviewStatus"`
}

type novelEventUpdateActionInput struct {
	EventID          string          `json:"eventId"`
	ExpectedRevision int64           `json:"expectedRevision"`
	Patch            novelEventPatch `json:"patch"`
}

type revisionedReviewActionInput struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	ReviewStatus     string `json:"reviewStatus"`
	Note             string `json:"note"`
}

type novelEventReviewActionInput struct {
	EventID string `json:"eventId"`
	revisionedReviewActionInput
}

type adaptationPlanCreateActionInput struct {
	SourceID              string          `json:"sourceId"`
	Title                 string          `json:"title"`
	Status                string          `json:"status"`
	TargetFormat          string          `json:"targetFormat"`
	TargetDurationSeconds int             `json:"targetDurationSeconds"`
	MaxShots              int             `json:"maxShots"`
	SelectedEventIDs      []string        `json:"selectedEventIds"`
	Structure             json.RawMessage `json:"structure"`
	Content               string          `json:"content"`
}

type adaptationPlanPatch struct {
	Title                 *string          `json:"title"`
	Status                *string          `json:"status"`
	TargetFormat          *string          `json:"targetFormat"`
	TargetDurationSeconds *int             `json:"targetDurationSeconds"`
	MaxShots              *int             `json:"maxShots"`
	SelectedEventIDs      *[]string        `json:"selectedEventIds"`
	Structure             *json.RawMessage `json:"structure"`
	Content               *string          `json:"content"`
	ReviewStatus          *string          `json:"reviewStatus"`
}

type adaptationPlanUpdateActionInput struct {
	PlanID           string              `json:"planId"`
	ExpectedRevision int64               `json:"expectedRevision"`
	Patch            adaptationPlanPatch `json:"patch"`
}

type adaptationPlanReviewActionInput struct {
	PlanID string `json:"planId"`
	revisionedReviewActionInput
}

type adaptationPlanActivateActionInput struct {
	PlanID           string `json:"planId"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

func decodeNovelEventUpdateActionInput(raw json.RawMessage) (novelEventUpdateActionInput, error) {
	var input novelEventUpdateActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return input, err
	}
	if strings.TrimSpace(input.EventID) == "" || input.ExpectedRevision <= 0 {
		return input, controlValidationError("eventId 和 expectedRevision 为必填项")
	}
	if !novelEventPatchHasChanges(input.Patch) {
		return input, controlValidationError("小说事件补丁不能为空")
	}
	return input, nil
}

func decodeNovelEventReviewActionInput(raw json.RawMessage) (novelEventReviewActionInput, error) {
	var input novelEventReviewActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return input, err
	}
	if strings.TrimSpace(input.EventID) == "" || input.ExpectedRevision <= 0 || !validReviewStatus(strings.TrimSpace(input.ReviewStatus)) {
		return input, controlValidationError("eventId、expectedRevision 和有效 reviewStatus 为必填项")
	}
	return input, nil
}

func decodeAdaptationPlanCreateActionInput(raw json.RawMessage) (adaptationPlanCreateActionInput, error) {
	var input adaptationPlanCreateActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return input, err
	}
	input.Title = strings.TrimSpace(input.Title)
	input.SourceID = strings.TrimSpace(input.SourceID)
	input.Status = firstNonEmpty(strings.TrimSpace(input.Status), "draft")
	input.TargetFormat = firstNonEmpty(strings.TrimSpace(input.TargetFormat), "short_video")
	input.SelectedEventIDs = normalizeStringSlice(input.SelectedEventIDs)
	if input.Title == "" || !validAdaptationPlanStatus(input.Status) {
		return input, controlValidationError("计划名称为空或状态无效")
	}
	if input.TargetDurationSeconds < 0 || input.MaxShots < 0 {
		return input, controlValidationError("目标时长和最大镜头数不能为负数")
	}
	structure, err := normalizedJSONObject(input.Structure)
	if err != nil {
		return input, err
	}
	input.Structure = structure
	return input, nil
}

func decodeAdaptationPlanUpdateActionInput(raw json.RawMessage) (adaptationPlanUpdateActionInput, error) {
	var input adaptationPlanUpdateActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return input, err
	}
	if strings.TrimSpace(input.PlanID) == "" || input.ExpectedRevision <= 0 {
		return input, controlValidationError("planId 和 expectedRevision 为必填项")
	}
	if !adaptationPlanPatchHasChanges(input.Patch) {
		return input, controlValidationError("改编计划补丁不能为空")
	}
	return input, nil
}

func decodeAdaptationPlanReviewActionInput(raw json.RawMessage) (adaptationPlanReviewActionInput, error) {
	var input adaptationPlanReviewActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return input, err
	}
	if strings.TrimSpace(input.PlanID) == "" || input.ExpectedRevision <= 0 || !validReviewStatus(strings.TrimSpace(input.ReviewStatus)) {
		return input, controlValidationError("planId、expectedRevision 和有效 reviewStatus 为必填项")
	}
	return input, nil
}

func decodeAdaptationPlanActivateActionInput(raw json.RawMessage) (adaptationPlanActivateActionInput, error) {
	var input adaptationPlanActivateActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return input, err
	}
	if strings.TrimSpace(input.PlanID) == "" || input.ExpectedRevision <= 0 {
		return input, controlValidationError("planId 和 expectedRevision 为必填项")
	}
	return input, nil
}

func novelEventPatchHasChanges(patch novelEventPatch) bool {
	return patch.Title != nil || patch.Summary != nil || patch.EventType != nil || patch.Importance != nil ||
		patch.TimelineHint != nil || patch.LocationHint != nil || patch.EmotionalTone != nil || patch.Conflict != nil ||
		patch.Outcome != nil || patch.AdaptationHint != nil || patch.Characters != nil || patch.Scenes != nil ||
		patch.Props != nil || patch.Keywords != nil || patch.RawExcerpt != nil || patch.ReviewStatus != nil
}

func adaptationPlanPatchHasChanges(patch adaptationPlanPatch) bool {
	return patch.Title != nil || patch.Status != nil || patch.TargetFormat != nil || patch.TargetDurationSeconds != nil ||
		patch.MaxShots != nil || patch.SelectedEventIDs != nil || patch.Structure != nil || patch.Content != nil || patch.ReviewStatus != nil
}

func normalizedJSONObject(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return nil, controlValidationError("structure 必须为 JSON 对象")
	}
	return raw, nil
}

func (s *Server) updateNovelEventActionTx(ctx context.Context, tx pgx.Tx, project Project, userID string, input novelEventUpdateActionInput) (NovelEvent, error) {
	item, err := scanNovelEvent(tx.QueryRow(ctx, novelEventSelectSQL(`WHERE e.project_id = $1 AND e.id = $2 FOR UPDATE OF e`), project.ID, input.EventID))
	if err != nil {
		return NovelEvent{}, err
	}
	if item.Revision != input.ExpectedRevision {
		return NovelEvent{}, revisionConflictError("NOVEL_EVENT_REVISION_CONFLICT", "小说事件已被其他操作修改", input.ExpectedRevision, item.Revision)
	}
	patch := input.Patch
	if patch.Title != nil {
		item.Title = strings.TrimSpace(*patch.Title)
		if item.Title == "" {
			return NovelEvent{}, controlValidationError("事件标题不能为空")
		}
	}
	if patch.Summary != nil {
		item.Summary = strings.TrimSpace(*patch.Summary)
		if item.Summary == "" {
			return NovelEvent{}, controlValidationError("事件摘要不能为空")
		}
	}
	if patch.EventType != nil {
		item.EventType = stringPtrFromValue(*patch.EventType)
	}
	if patch.Importance != nil {
		if *patch.Importance < 1 || *patch.Importance > 5 {
			return NovelEvent{}, controlValidationError("事件重要性必须在 1 到 5 之间")
		}
		item.Importance = *patch.Importance
	}
	if patch.TimelineHint != nil {
		item.TimelineHint = stringPtrFromValue(*patch.TimelineHint)
	}
	if patch.LocationHint != nil {
		item.LocationHint = stringPtrFromValue(*patch.LocationHint)
	}
	if patch.EmotionalTone != nil {
		item.EmotionalTone = stringPtrFromValue(*patch.EmotionalTone)
	}
	if patch.Conflict != nil {
		item.Conflict = stringPtrFromValue(*patch.Conflict)
	}
	if patch.Outcome != nil {
		item.Outcome = stringPtrFromValue(*patch.Outcome)
	}
	if patch.AdaptationHint != nil {
		item.AdaptationHint = stringPtrFromValue(*patch.AdaptationHint)
	}
	if patch.RawExcerpt != nil {
		item.RawExcerpt = stringPtrFromValue(*patch.RawExcerpt)
	}
	if patch.Characters != nil {
		item.Characters = mustRawJSON(normalizeStringSlice(*patch.Characters))
	}
	if patch.Scenes != nil {
		item.Scenes = mustRawJSON(normalizeStringSlice(*patch.Scenes))
	}
	if patch.Props != nil {
		item.Props = mustRawJSON(normalizeStringSlice(*patch.Props))
	}
	if patch.Keywords != nil {
		item.Keywords = mustRawJSON(normalizeStringSlice(*patch.Keywords))
	}
	reviewStatus := "pending"
	if patch.ReviewStatus != nil {
		reviewStatus = strings.TrimSpace(*patch.ReviewStatus)
		if !validReviewStatus(reviewStatus) {
			return NovelEvent{}, controlValidationError("审核状态无效")
		}
	}
	command, err := tx.Exec(ctx, `
		UPDATE novel_events
		SET title = $3, summary = $4, event_type = $5, importance = $6,
		    timeline_hint = $7, location_hint = $8, emotional_tone = $9,
		    conflict = $10, outcome = $11, adaptation_hint = $12,
		    characters = $13, scenes = $14, props = $15, keywords = $16,
		    raw_excerpt = $17, review_status = $18, manual_override = true,
		    edited_by = $19, edited_at = now(), updated_at = now()
		WHERE project_id = $1 AND id = $2 AND revision = $20
	`, project.ID, item.ID, item.Title, item.Summary, optionalStringPtrValue(item.EventType), item.Importance,
		optionalStringPtrValue(item.TimelineHint), optionalStringPtrValue(item.LocationHint), optionalStringPtrValue(item.EmotionalTone),
		optionalStringPtrValue(item.Conflict), optionalStringPtrValue(item.Outcome), optionalStringPtrValue(item.AdaptationHint),
		rawOrDefault(item.Characters, `[]`), rawOrDefault(item.Scenes, `[]`), rawOrDefault(item.Props, `[]`), rawOrDefault(item.Keywords, `[]`),
		optionalStringPtrValue(item.RawExcerpt), reviewStatus, userID, input.ExpectedRevision)
	if err != nil {
		return NovelEvent{}, err
	}
	if command.RowsAffected() != 1 {
		return NovelEvent{}, revisionConflictError("NOVEL_EVENT_REVISION_CONFLICT", "小说事件已被其他操作修改", input.ExpectedRevision, item.Revision)
	}
	return scanNovelEvent(tx.QueryRow(ctx, novelEventSelectSQL(`WHERE e.project_id = $1 AND e.id = $2`), project.ID, item.ID))
}

func (s *Server) reviewNovelEventActionTx(ctx context.Context, tx pgx.Tx, project Project, userID string, input novelEventReviewActionInput) (NovelEvent, ReviewResponse, error) {
	item, err := scanNovelEvent(tx.QueryRow(ctx, novelEventSelectSQL(`WHERE e.project_id = $1 AND e.id = $2 FOR UPDATE OF e`), project.ID, input.EventID))
	if err != nil {
		return NovelEvent{}, ReviewResponse{}, err
	}
	if item.Revision != input.ExpectedRevision {
		return NovelEvent{}, ReviewResponse{}, revisionConflictError("NOVEL_EVENT_REVISION_CONFLICT", "小说事件已被其他操作修改", input.ExpectedRevision, item.Revision)
	}
	command, err := tx.Exec(ctx, `
		UPDATE novel_events
		SET review_status = $3::text,
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'reviewStatus', $3::text, 'reviewNote', $4::text, 'reviewedBy', $5::text, 'reviewedAt', now()
		    )
		WHERE project_id = $1 AND id = $2 AND revision = $6
	`, project.ID, item.ID, strings.TrimSpace(input.ReviewStatus), strings.TrimSpace(input.Note), userID, input.ExpectedRevision)
	if err != nil {
		return NovelEvent{}, ReviewResponse{}, err
	}
	if command.RowsAffected() != 1 {
		return NovelEvent{}, ReviewResponse{}, revisionConflictError("NOVEL_EVENT_REVISION_CONFLICT", "小说事件已被其他操作修改", input.ExpectedRevision, item.Revision)
	}
	updated, err := scanNovelEvent(tx.QueryRow(ctx, novelEventSelectSQL(`WHERE e.project_id = $1 AND e.id = $2`), project.ID, item.ID))
	if err != nil {
		return NovelEvent{}, ReviewResponse{}, err
	}
	return updated, reviewResponseFromRevisionedEntity(updated.ID, updated.ReviewStatus, input.Note, updated.UpdatedAt, updated.Revision), nil
}

func (s *Server) createAdaptationPlanActionTx(ctx context.Context, tx pgx.Tx, project Project, userID string, input adaptationPlanCreateActionInput) (AdaptationPlan, error) {
	if err := validateAdaptationPlanReferencesTx(ctx, tx, project.ID, input.SourceID, input.SelectedEventIDs); err != nil {
		return AdaptationPlan{}, err
	}
	var planID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO adaptation_plans(
			organization_id, project_id, source_id, title, status, target_format,
			target_duration_seconds, max_shots, selected_event_ids, structure, content,
			manual_override, created_by
		)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6, NULLIF($7, 0), NULLIF($8, 0), $9, $10, $11, true, $12)
		RETURNING id::text
	`, project.OrganizationID, project.ID, input.SourceID, input.Title, input.Status, input.TargetFormat,
		input.TargetDurationSeconds, input.MaxShots, mustRawJSON(input.SelectedEventIDs), input.Structure, input.Content, userID).Scan(&planID); err != nil {
		return AdaptationPlan{}, err
	}
	if input.Status == "active" {
		if err := demoteOtherActiveAdaptationPlansTx(ctx, tx, project.ID, input.SourceID, planID); err != nil {
			return AdaptationPlan{}, err
		}
	}
	return scanAdaptationPlan(tx.QueryRow(ctx, adaptationPlanSelectSQL(`WHERE p.project_id = $1 AND p.id = $2`), project.ID, planID))
}

func (s *Server) updateAdaptationPlanActionTx(ctx context.Context, tx pgx.Tx, project Project, userID string, input adaptationPlanUpdateActionInput) (AdaptationPlan, error) {
	item, err := scanAdaptationPlan(tx.QueryRow(ctx, adaptationPlanSelectSQL(`WHERE p.project_id = $1 AND p.id = $2 FOR UPDATE OF p`), project.ID, input.PlanID))
	if err != nil {
		return AdaptationPlan{}, err
	}
	if item.Revision != input.ExpectedRevision {
		return AdaptationPlan{}, revisionConflictError("ADAPTATION_PLAN_REVISION_CONFLICT", "改编计划已被其他操作修改", input.ExpectedRevision, item.Revision)
	}
	patch := input.Patch
	if patch.Title != nil {
		item.Title = strings.TrimSpace(*patch.Title)
		if item.Title == "" {
			return AdaptationPlan{}, controlValidationError("计划名称不能为空")
		}
	}
	if patch.Status != nil {
		item.Status = strings.TrimSpace(*patch.Status)
		if !validAdaptationPlanStatus(item.Status) {
			return AdaptationPlan{}, controlValidationError("计划状态无效")
		}
	}
	if patch.TargetFormat != nil {
		item.TargetFormat = firstNonEmpty(strings.TrimSpace(*patch.TargetFormat), "short_video")
	}
	if patch.TargetDurationSeconds != nil {
		if *patch.TargetDurationSeconds < 0 {
			return AdaptationPlan{}, controlValidationError("目标时长不能为负数")
		}
		item.TargetDurationSeconds = intPtrIfPositive(*patch.TargetDurationSeconds)
	}
	if patch.MaxShots != nil {
		if *patch.MaxShots < 0 {
			return AdaptationPlan{}, controlValidationError("最大镜头数不能为负数")
		}
		item.MaxShots = intPtrIfPositive(*patch.MaxShots)
	}
	if patch.SelectedEventIDs != nil {
		ids := normalizeStringSlice(*patch.SelectedEventIDs)
		if err := validateAdaptationPlanReferencesTx(ctx, tx, project.ID, optionalStringPtrValue(item.SourceID), ids); err != nil {
			return AdaptationPlan{}, err
		}
		item.SelectedEventIDs = mustRawJSON(ids)
	}
	if patch.Structure != nil {
		structure, err := normalizedJSONObject(*patch.Structure)
		if err != nil {
			return AdaptationPlan{}, err
		}
		item.Structure = structure
	}
	if patch.Content != nil {
		item.Content = strings.TrimSpace(*patch.Content)
	}
	reviewStatus := "pending"
	if patch.ReviewStatus != nil {
		reviewStatus = strings.TrimSpace(*patch.ReviewStatus)
		if !validReviewStatus(reviewStatus) {
			return AdaptationPlan{}, controlValidationError("审核状态无效")
		}
	}
	command, err := tx.Exec(ctx, `
		UPDATE adaptation_plans
		SET title = $3, status = $4, target_format = $5,
		    target_duration_seconds = $6, max_shots = $7, selected_event_ids = $8,
		    structure = $9, content = $10, review_status = $11,
		    manual_override = true, edited_by = $12, edited_at = now(), updated_at = now()
		WHERE project_id = $1 AND id = $2 AND revision = $13
	`, project.ID, item.ID, item.Title, item.Status, item.TargetFormat, optionalIntPtrValue(item.TargetDurationSeconds),
		optionalIntPtrValue(item.MaxShots), rawOrDefault(item.SelectedEventIDs, `[]`), rawOrDefault(item.Structure, `{}`),
		item.Content, reviewStatus, userID, input.ExpectedRevision)
	if err != nil {
		return AdaptationPlan{}, err
	}
	if command.RowsAffected() != 1 {
		return AdaptationPlan{}, revisionConflictError("ADAPTATION_PLAN_REVISION_CONFLICT", "改编计划已被其他操作修改", input.ExpectedRevision, item.Revision)
	}
	if item.Status == "active" {
		if err := demoteOtherActiveAdaptationPlansTx(ctx, tx, project.ID, optionalStringPtrValue(item.SourceID), item.ID); err != nil {
			return AdaptationPlan{}, err
		}
	}
	return scanAdaptationPlan(tx.QueryRow(ctx, adaptationPlanSelectSQL(`WHERE p.project_id = $1 AND p.id = $2`), project.ID, item.ID))
}

func (s *Server) reviewAdaptationPlanActionTx(ctx context.Context, tx pgx.Tx, project Project, userID string, input adaptationPlanReviewActionInput) (AdaptationPlan, ReviewResponse, error) {
	item, err := scanAdaptationPlan(tx.QueryRow(ctx, adaptationPlanSelectSQL(`WHERE p.project_id = $1 AND p.id = $2 FOR UPDATE OF p`), project.ID, input.PlanID))
	if err != nil {
		return AdaptationPlan{}, ReviewResponse{}, err
	}
	if item.Revision != input.ExpectedRevision {
		return AdaptationPlan{}, ReviewResponse{}, revisionConflictError("ADAPTATION_PLAN_REVISION_CONFLICT", "改编计划已被其他操作修改", input.ExpectedRevision, item.Revision)
	}
	command, err := tx.Exec(ctx, `
		UPDATE adaptation_plans
		SET review_status = $3::text,
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'reviewStatus', $3::text, 'reviewNote', $4::text, 'reviewedBy', $5::text, 'reviewedAt', now()
		    )
		WHERE project_id = $1 AND id = $2 AND revision = $6
	`, project.ID, item.ID, strings.TrimSpace(input.ReviewStatus), strings.TrimSpace(input.Note), userID, input.ExpectedRevision)
	if err != nil {
		return AdaptationPlan{}, ReviewResponse{}, err
	}
	if command.RowsAffected() != 1 {
		return AdaptationPlan{}, ReviewResponse{}, revisionConflictError("ADAPTATION_PLAN_REVISION_CONFLICT", "改编计划已被其他操作修改", input.ExpectedRevision, item.Revision)
	}
	updated, err := scanAdaptationPlan(tx.QueryRow(ctx, adaptationPlanSelectSQL(`WHERE p.project_id = $1 AND p.id = $2`), project.ID, item.ID))
	if err != nil {
		return AdaptationPlan{}, ReviewResponse{}, err
	}
	return updated, reviewResponseFromRevisionedEntity(updated.ID, updated.ReviewStatus, input.Note, updated.UpdatedAt, updated.Revision), nil
}

func (s *Server) activateAdaptationPlanActionTx(ctx context.Context, tx pgx.Tx, project Project, input adaptationPlanActivateActionInput) (AdaptationPlan, error) {
	item, err := scanAdaptationPlan(tx.QueryRow(ctx, adaptationPlanSelectSQL(`WHERE p.project_id = $1 AND p.id = $2 FOR UPDATE OF p`), project.ID, input.PlanID))
	if err != nil {
		return AdaptationPlan{}, err
	}
	if item.Revision != input.ExpectedRevision {
		return AdaptationPlan{}, revisionConflictError("ADAPTATION_PLAN_REVISION_CONFLICT", "改编计划已被其他操作修改", input.ExpectedRevision, item.Revision)
	}
	if err := demoteOtherActiveAdaptationPlansTx(ctx, tx, project.ID, optionalStringPtrValue(item.SourceID), item.ID); err != nil {
		return AdaptationPlan{}, err
	}
	command, err := tx.Exec(ctx, `
		UPDATE adaptation_plans SET status = 'active', updated_at = now()
		WHERE project_id = $1 AND id = $2 AND revision = $3
	`, project.ID, item.ID, input.ExpectedRevision)
	if err != nil {
		return AdaptationPlan{}, err
	}
	if command.RowsAffected() != 1 {
		return AdaptationPlan{}, revisionConflictError("ADAPTATION_PLAN_REVISION_CONFLICT", "改编计划已被其他操作修改", input.ExpectedRevision, item.Revision)
	}
	return scanAdaptationPlan(tx.QueryRow(ctx, adaptationPlanSelectSQL(`WHERE p.project_id = $1 AND p.id = $2`), project.ID, item.ID))
}

func validateAdaptationPlanReferencesTx(ctx context.Context, tx pgx.Tx, projectID, sourceID string, eventIDs []string) error {
	if sourceID != "" {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM project_sources WHERE project_id = $1 AND id = $2`, projectID, sourceID).Scan(&status); err != nil {
			return err
		}
		if status == "archived" {
			return controlValidationError("已归档原文不能用于改编计划")
		}
	}
	for _, eventID := range eventIDs {
		var eventSourceID string
		if err := tx.QueryRow(ctx, `SELECT source_id::text FROM novel_events WHERE project_id = $1 AND id = $2`, projectID, eventID).Scan(&eventSourceID); err != nil {
			return err
		}
		if sourceID != "" && eventSourceID != sourceID {
			return controlValidationError("selectedEventIds 包含其它原文的事件")
		}
	}
	return nil
}

func demoteOtherActiveAdaptationPlansTx(ctx context.Context, tx pgx.Tx, projectID, sourceID, selectedID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE adaptation_plans
		SET status = 'draft', updated_at = now()
		WHERE project_id = $1
		  AND (($2 = '' AND source_id IS NULL) OR source_id = NULLIF($2, '')::uuid)
		  AND id <> $3
		  AND status = 'active'
	`, projectID, sourceID, selectedID)
	return err
}

func decodeAgentToolResultValue[T any](result agentToolResult, key string) (T, error) {
	var value T
	raw, err := json.Marshal(result.Data[key])
	if err != nil {
		return value, fmt.Errorf("encode project control result %s: %w", key, err)
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, fmt.Errorf("decode project control result %s: %w", key, err)
	}
	return value, nil
}

func revisionConflictError(code, message string, expected, current int64) apiError {
	err := newAPIError(http.StatusConflict, code, message)
	err.Details = map[string]any{"expectedRevision": expected, "currentRevision": current}
	return err
}

func reviewResponseFromRevisionedEntity(id, status, note string, updatedAt time.Time, revision int64) ReviewResponse {
	return ReviewResponse{ID: id, ReviewStatus: status, Note: stringPtrFromValue(strings.TrimSpace(note)), UpdatedAt: updatedAt, Revision: revision}
}

func novelEventUpdateAgentResult(args map[string]any, item NovelEvent) agentToolResult {
	return agentToolOK("novel_event.update", args, fmt.Sprintf("小说事件已更新，当前 revision 为 %d。", item.Revision), map[string]any{"event": item})
}

func novelEventReviewAgentResult(args map[string]any, item NovelEvent, review ReviewResponse) agentToolResult {
	return agentToolOK("novel_event.review", args, "小说事件审核状态已更新。", map[string]any{"event": item, "review": review})
}

func adaptationPlanCreateAgentResult(args map[string]any, item AdaptationPlan) agentToolResult {
	return agentToolOK("adaptation.create", args, "改编计划已创建。", map[string]any{"plan": item})
}

func adaptationPlanUpdateAgentResult(args map[string]any, item AdaptationPlan) agentToolResult {
	return agentToolOK("adaptation.update", args, fmt.Sprintf("改编计划已更新，当前 revision 为 %d。", item.Revision), map[string]any{"plan": item})
}

func adaptationPlanReviewAgentResult(args map[string]any, item AdaptationPlan, review ReviewResponse) agentToolResult {
	return agentToolOK("adaptation.review", args, "改编计划审核状态已更新。", map[string]any{"plan": item, "review": review})
}

func adaptationPlanActivateAgentResult(args map[string]any, item AdaptationPlan) agentToolResult {
	return agentToolOK("adaptation.activate", args, "改编计划已激活。", map[string]any{"plan": item})
}

func validateAdaptationActionCommand(projectID, actorUserID, action string) error {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(actorUserID) == "" {
		return newAPIError(http.StatusUnprocessableEntity, "PROJECT_CONTROL_CONTEXT_INVALID", action+" 缺少项目或执行用户")
	}
	return nil
}
