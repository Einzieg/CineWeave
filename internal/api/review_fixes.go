package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/production"
	"github.com/Einzieg/cineweave/internal/provider"
	reviewpkg "github.com/Einzieg/cineweave/internal/review"
	"github.com/jackc/pgx/v5"
)

const reviewFixPromptKey = "review_fix_agent"

type ReviewFix struct {
	ID                string          `json:"id"`
	OrganizationID    string          `json:"organizationId"`
	ProjectID         string          `json:"projectId"`
	ReviewItemID      string          `json:"reviewItemId"`
	TargetEntityType  string          `json:"targetEntityType"`
	TargetEntityID    *string         `json:"targetEntityId,omitempty"`
	Status            string          `json:"status"`
	FixType           string          `json:"fixType"`
	Title             string          `json:"title"`
	Explanation       string          `json:"explanation"`
	BeforeSnapshot    json.RawMessage `json:"beforeSnapshot"`
	Patch             json.RawMessage `json:"patch"`
	AfterPreview      json.RawMessage `json:"afterPreview"`
	RegenerateRequest json.RawMessage `json:"regenerateRequest,omitempty"`
	PromptVersionID   *string         `json:"promptVersionId,omitempty"`
	PromptHash        *string         `json:"promptHash,omitempty"`
	ProviderCallID    *string         `json:"providerCallId,omitempty"`
	ErrorCode         *string         `json:"errorCode,omitempty"`
	ErrorMessage      *string         `json:"errorMessage,omitempty"`
	CreatedBy         *string         `json:"createdBy,omitempty"`
	AppliedBy         *string         `json:"appliedBy,omitempty"`
	CreatedAt         time.Time       `json:"createdAt"`
	AppliedAt         *time.Time      `json:"appliedAt,omitempty"`
	UpdatedAt         time.Time       `json:"updatedAt"`
}

type ApplyReviewFixResponse struct {
	FixID            string  `json:"fixId"`
	Status           string  `json:"status"`
	ReviewItemStatus *string `json:"reviewItemStatus,omitempty"`
	WorkflowRunID    *string `json:"workflowRunId,omitempty"`
}

type DismissReviewFixResponse struct {
	FixID  string `json:"fixId"`
	Status string `json:"status"`
}

type reviewFixDraft struct {
	FixType           string
	Title             string
	Explanation       string
	Patch             map[string]any
	RegenerateRequest map[string]any
	PromptVersionID   string
	PromptHash        string
	ProviderCallID    string
}

func (s *Server) generateReviewFix(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	item, ok := s.reviewItemForFix(w, r, project.ID, r.PathValue("itemId"))
	if !ok {
		return
	}
	if item.EntityID == nil || strings.TrimSpace(*item.EntityID) == "" || !reviewpkg.SupportedFixTarget(item.EntityType) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "REVIEW_FIX_UNSUPPORTED", "current review item does not support automatic fixes", nil, false)
		return
	}
	var req struct {
		Mode        string `json:"mode"`
		Instruction string `json:"instruction"`
	}
	if !decode(w, r, &req) {
		return
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = "deterministic"
	}
	target, err := reviewpkg.LoadReviewFixTarget(r.Context(), s.db, project.ID, item.EntityType, *item.EntityID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var draft reviewFixDraft
	switch mode {
	case "agent":
		draft, err = s.generateAgentReviewFix(r.Context(), project, item, target, strings.TrimSpace(req.Instruction))
	case "deterministic":
		draft, err = deterministicReviewFix(item, target)
	default:
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "mode is invalid", nil, false)
		return
	}
	if err != nil {
		if errors.Is(err, reviewpkg.ErrUnsupportedFixTarget) {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "REVIEW_FIX_UNSUPPORTED", "current review item does not support automatic fixes", nil, false)
			return
		}
		s.writeError(w, r, err)
		return
	}
	if draft.FixType == "" {
		draft.FixType = "patch"
	}
	if draft.Patch == nil {
		draft.Patch = map[string]any{}
	}
	if draft.FixType != "note" {
		if err := reviewpkg.ValidateReviewPatch(target.EntityType, draft.Patch); err != nil {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error(), nil, false)
			return
		}
	}
	afterPreview := reviewpkg.ApplyReviewPatchPreview(target.Snapshot, draft.Patch)
	fix, err := scanReviewFix(s.db.QueryRow(r.Context(), `
		INSERT INTO review_fixes(
			organization_id, project_id, review_item_id, target_entity_type, target_entity_id, status, fix_type,
			title, explanation, before_snapshot, patch, after_preview, regenerate_request,
			prompt_version_id, prompt_hash, provider_call_id, created_by
		)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid, 'draft', $6, $7, $8, $9, $10, $11, $12,
		        NULLIF($13, '')::uuid, NULLIF($14, ''), NULLIF($15, '')::uuid, $16)
		RETURNING `+reviewFixReturningSQL(), project.OrganizationID, project.ID, item.ID, target.EntityType, target.EntityID, normalizeReviewFixType(draft.FixType),
		defaultAPIString(draft.Title, "修复建议"), defaultAPIString(draft.Explanation, "请确认后应用该修复建议。"),
		mustRawJSON(target.Snapshot), mustRawJSON(draft.Patch), mustRawJSON(afterPreview), rawNullableObject(draft.RegenerateRequest),
		draft.PromptVersionID, draft.PromptHash, draft.ProviderCallID, principal.UserID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, fix, nil)
}

func (s *Server) listReviewFixes(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	if _, ok := s.reviewItemForFix(w, r, project.ID, r.PathValue("itemId")); !ok {
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT `+reviewFixColumns()+`
		FROM review_fixes
		WHERE project_id = $1 AND review_item_id = $2
		ORDER BY created_at DESC
	`, project.ID, r.PathValue("itemId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]ReviewFix, 0)
	for rows.Next() {
		item, err := scanReviewFix(rows)
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

func (s *Server) getReviewFix(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	fix, err := scanReviewFix(s.db.QueryRow(r.Context(), `
		SELECT `+reviewFixColumns()+`
		FROM review_fixes
		WHERE project_id = $1 AND id = $2
	`, project.ID, r.PathValue("fixId")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, fix, nil)
}

func (s *Server) applyReviewFix(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	var req struct {
		ResolveReviewItem   bool `json:"resolveReviewItem"`
		TriggerRegeneration bool `json:"triggerRegeneration"`
	}
	if !decode(w, r, &req) {
		return
	}
	fix, err := scanReviewFix(s.db.QueryRow(r.Context(), `
		SELECT `+reviewFixColumns()+`
		FROM review_fixes
		WHERE project_id = $1 AND id = $2
	`, project.ID, r.PathValue("fixId")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if fix.Status != "draft" {
		httpx.WriteError(w, r, http.StatusConflict, "REVIEW_FIX_NOT_DRAFT", "review fix is not a draft", nil, false)
		return
	}
	if fix.TargetEntityID == nil || !reviewpkg.SupportedFixTarget(fix.TargetEntityType) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "REVIEW_FIX_UNSUPPORTED", "current review fix cannot be applied automatically", nil, false)
		return
	}
	current, err := reviewpkg.LoadReviewFixTarget(r.Context(), s.db, project.ID, fix.TargetEntityType, *fix.TargetEntityID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	before := rawObject(fix.BeforeSnapshot)
	if !reviewpkg.SnapshotsEqual(current.Snapshot, before) {
		httpx.WriteError(w, r, http.StatusConflict, "TARGET_CHANGED", "target entity changed after the fix was generated", nil, false)
		return
	}
	patch := rawObject(fix.Patch)
	if fix.FixType != "note" {
		if err := reviewpkg.ValidateReviewPatch(fix.TargetEntityType, patch); err != nil {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error(), nil, false)
			return
		}
	}
	after := reviewpkg.ApplyReviewPatchPreview(current.Snapshot, patch)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if fix.FixType != "note" {
		if err := s.applyReviewPatchToTarget(r.Context(), tx, project, fix.TargetEntityType, *fix.TargetEntityID, after, principal.UserID); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	reviewItemStatus := (*string)(nil)
	if req.ResolveReviewItem {
		status := "resolved"
		reviewItemStatus = &status
		if _, err := tx.Exec(r.Context(), `
			UPDATE review_items
			SET status = 'resolved', resolved_by = $3, resolved_at = now(), resolution_note = 'review fix applied'
			WHERE project_id = $1 AND id = $2
		`, project.ID, fix.ReviewItemID, principal.UserID); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE review_fixes
		SET status = 'applied', applied_by = $3, applied_at = now(), after_preview = $4
		WHERE project_id = $1 AND id = $2
	`, project.ID, fix.ID, principal.UserID, mustRawJSON(after)); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "review.fix.applied", "review_fix", fix.ID, mustRawJSON(map[string]any{
		"reviewFixId":      fix.ID,
		"reviewItemId":     fix.ReviewItemID,
		"targetEntityType": fix.TargetEntityType,
		"targetEntityId":   stringPtrValue(fix.TargetEntityID),
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}

	var workflowRunID *string
	if req.TriggerRegeneration && len(fix.RegenerateRequest) > 0 && string(fix.RegenerateRequest) != "null" {
		runID, ok := s.startReviewFixRegeneration(w, r, principal, project, fix.RegenerateRequest)
		if !ok {
			return
		}
		workflowRunID = &runID
	}
	httpx.WriteJSON(w, r, http.StatusOK, ApplyReviewFixResponse{
		FixID:            fix.ID,
		Status:           "applied",
		ReviewItemStatus: reviewItemStatus,
		WorkflowRunID:    workflowRunID,
	}, nil)
}

func (s *Server) dismissReviewFix(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	tag, err := s.db.Exec(r.Context(), `
		UPDATE review_fixes
		SET status = 'dismissed'
		WHERE project_id = $1 AND id = $2 AND status = 'draft'
	`, project.ID, r.PathValue("fixId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.WriteError(w, r, http.StatusConflict, "REVIEW_FIX_NOT_DRAFT", "review fix is not a draft", nil, false)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, DismissReviewFixResponse{FixID: r.PathValue("fixId"), Status: "dismissed"}, nil)
}

func (s *Server) reviewItemForFix(w http.ResponseWriter, r *http.Request, projectID, itemID string) (ReviewItem, bool) {
	item, err := scanReviewItem(s.db.QueryRow(r.Context(), reviewItemSelectSQL(`WHERE project_id = $1 AND id = $2`), projectID, itemID))
	if err != nil {
		s.writeError(w, r, err)
		return ReviewItem{}, false
	}
	return item, true
}

func deterministicReviewFix(item ReviewItem, target reviewpkg.ReviewFixTarget) (reviewFixDraft, error) {
	patch := map[string]any{}
	title := "生成简单修复建议"
	explanation := "根据当前问题和目标对象状态生成一个保守的字段补丁。"
	switch target.EntityType {
	case "canonical_asset":
		if blankString(target.Snapshot["description"]) || strings.Contains(item.Title+item.Description, "描述") {
			patch["description"] = "请补充该资产的视觉描述。"
			title = "补充资产描述"
			explanation = "资产描述为空时，后续提示词缺少稳定设定。"
		}
		if blankString(target.Snapshot["consistencyPrompt"]) || strings.Contains(strings.ToLower(item.Title+item.Description), "consistencyprompt") || strings.Contains(item.Title+item.Description, "一致性") {
			patch["consistencyPrompt"] = "保持该资产的名称、外观、关键特征和项目画风一致。"
			title = "补充一致性提示"
			explanation = "一致性提示可减少后续镜头生成中的外观漂移。"
		}
	case "storyboard_shot":
		if blankString(target.Snapshot["visual"]) || strings.Contains(item.Title+item.Description, "画面") || strings.Contains(strings.ToLower(item.Title+item.Description), "visual") {
			patch["visual"] = "请补充该镜头的画面描述。"
			title = "补充镜头画面"
			explanation = "镜头画面描述为空时，图片和视频生成缺少核心目标。"
		}
		if nonPositiveNumber(target.Snapshot["durationSeconds"]) || strings.Contains(item.Title+item.Description, "时长") || strings.Contains(strings.ToLower(item.Title+item.Description), "duration") {
			patch["durationSeconds"] = 3
			title = "补充镜头时长"
			explanation = "为缺少时长的镜头设置一个保守的 3 秒默认值。"
		}
	case "shot_asset_requirement":
		if blankString(target.Snapshot["prompt"]) || strings.Contains(strings.ToLower(item.Title+item.Description), "prompt") {
			parts := []string{}
			for _, key := range []string{"costume", "pose", "expression", "action"} {
				if value := strings.TrimSpace(stringValueFromAny(target.Snapshot[key])); value != "" {
					parts = append(parts, value)
				}
			}
			if len(parts) == 0 {
				parts = append(parts, "请补充该派生资产在镜头中的外观、姿态和动作要求。")
			}
			patch["prompt"] = strings.Join(parts, "，")
			title = "补充派生资产提示"
			explanation = "根据已有服装、姿态、表情和动作字段拼接生成提示。"
		}
	case "timeline_clip":
		text := strings.ToLower(item.Title + item.Description)
		if strings.Contains(text, "disable") || strings.Contains(item.Title+item.Description, "禁用") {
			patch["enabled"] = true
			title = "启用时间线片段"
			explanation = "该问题指向禁用片段，建议先启用片段后再检查成片。"
		}
		if nonPositiveNumber(target.Snapshot["targetDurationSeconds"]) && (strings.Contains(text, "targetduration") || strings.Contains(item.Title+item.Description, "目标时长")) {
			patch["targetDurationSeconds"] = 3
			title = "补充片段目标时长"
			explanation = "为缺少目标时长的片段设置一个保守默认值。"
		}
	default:
		return noteReviewFix("需要人工处理", "该问题暂不支持自动生成字段补丁。"), nil
	}
	if len(patch) == 0 {
		return noteReviewFix("需要人工处理", "该问题需要人工判断，暂不支持自动生成补丁。"), nil
	}
	return reviewFixDraft{
		FixType:           "patch",
		Title:             title,
		Explanation:       explanation,
		Patch:             patch,
		RegenerateRequest: regenerateRequestForReviewItem(item, target.EntityID),
	}, nil
}

func noteReviewFix(title, explanation string) reviewFixDraft {
	return reviewFixDraft{FixType: "note", Title: title, Explanation: explanation, Patch: map[string]any{}}
}

func (s *Server) generateAgentReviewFix(ctx context.Context, project Project, item ReviewItem, target reviewpkg.ReviewFixTarget, instruction string) (reviewFixDraft, error) {
	reviewContext, err := reviewpkg.BuildProjectReviewContext(ctx, s.db, project.ID)
	if err != nil {
		return reviewFixDraft{}, err
	}
	rendered, err := s.renderProjectPrompt(ctx, project, reviewFixPromptKey, map[string]any{
		"reviewItem": map[string]any{"json": string(mustRawJSON(item))},
		"target": map[string]any{
			"entityType":     target.EntityType,
			"snapshot":       string(mustRawJSON(target.Snapshot)),
			"editableFields": string(mustRawJSON(target.EditableFields)),
		},
		"project": map[string]any{"json": string(mustRawJSON(reviewContext))},
		"input":   map[string]any{"instruction": instruction},
	})
	if err != nil {
		return reviewFixDraft{}, err
	}
	agentCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	resp, err := provider.NewGatewayClientFromEnv().GenerateText(agentCtx, provider.GatewayTextRequest{
		OrganizationID:    project.OrganizationID,
		ProjectID:         project.ID,
		ModelProfileKey:   project.ScriptModelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		Input: mustMarshal(map[string]any{
			"prompt":         rendered.RenderedText,
			"responseFormat": "json",
		}),
		Options: provider.GatewayTextOptions{TimeoutMS: 15000},
	})
	if err != nil {
		return reviewFixDraft{}, err
	}
	draft, err := parseAgentReviewFix(resp.Output.Text)
	if err != nil {
		return reviewFixDraft{}, err
	}
	draft.ProviderCallID = resp.ProviderCallID
	draft.PromptVersionID = rendered.PromptVersionID
	draft.PromptHash = rendered.RenderedHash
	return draft, nil
}

func parseAgentReviewFix(text string) (reviewFixDraft, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	var raw struct {
		FixType     string         `json:"fixType"`
		Title       string         `json:"title"`
		Explanation string         `json:"explanation"`
		Patch       map[string]any `json:"patch"`
		Regenerate  struct {
			Recommended bool   `json:"recommended"`
			TargetType  string `json:"targetType"`
			TargetID    string `json:"targetId"`
			Reason      string `json:"reason"`
		} `json:"regenerate"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &raw); err != nil {
		return reviewFixDraft{}, err
	}
	regenerate := map[string]any(nil)
	if raw.Regenerate.Recommended && strings.TrimSpace(raw.Regenerate.TargetType) != "" {
		regenerate = map[string]any{
			"targetType": strings.TrimSpace(raw.Regenerate.TargetType),
			"targetId":   strings.TrimSpace(raw.Regenerate.TargetID),
			"reason":     strings.TrimSpace(raw.Regenerate.Reason),
		}
	}
	return reviewFixDraft{
		FixType:           normalizeReviewFixType(raw.FixType),
		Title:             strings.TrimSpace(raw.Title),
		Explanation:       strings.TrimSpace(raw.Explanation),
		Patch:             raw.Patch,
		RegenerateRequest: regenerate,
	}, nil
}

func (s *Server) applyReviewPatchToTarget(ctx context.Context, tx pgx.Tx, project Project, entityType, entityID string, after map[string]any, userID string) error {
	switch entityType {
	case "script_scene":
		_, err := tx.Exec(ctx, `
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
		`, project.ID, entityID, stringValueFromAny(after["title"]), stringValueFromAny(after["summary"]), stringValueFromAny(after["location"]),
			stringValueFromAny(after["timeOfDay"]), stringValueFromAny(after["atmosphere"]), jsonValueOrDefault(after["characters"], []any{}),
			jsonValueOrDefault(after["scenes"], []any{}), jsonValueOrDefault(after["props"], []any{}), stringValueFromAny(after["action"]),
			stringValueFromAny(after["dialogue"]), stringValueFromAny(after["visualGoal"]), stringValueFromAny(after["emotionalTone"]),
			stringValueFromAny(after["conflict"]), stringValueFromAny(after["outcome"]), stringValueFromAny(after["content"]), userID)
		if err != nil {
			return err
		}
		if err := markScriptSceneDownstreamStaleFromContext(ctx, tx, project.ID, entityID); err != nil {
			return err
		}
		return production.MarkFinalVideoStale(ctx, tx, project.ID, "")
	case "canonical_asset":
		_, err := tx.Exec(ctx, `
			UPDATE canonical_assets
			SET name = $3,
			    description = $4,
			    profile = $5,
			    base_prompt = NULLIF($6, ''),
			    consistency_prompt = NULLIF($7, ''),
			    negative_prompt = NULLIF($8, ''),
			    lock_reference = $9,
			    review_status = 'pending',
			    manual_override = true,
			    stale_state = 'fresh',
			    edited_by = $10,
			    edited_at = now(),
			    updated_at = now()
			WHERE project_id = $1 AND id = $2
		`, project.ID, entityID, stringValueFromAny(after["name"]), stringValueFromAny(after["description"]), jsonValueOrDefault(after["profile"], map[string]any{}),
			stringValueFromAny(after["basePrompt"]), stringValueFromAny(after["consistencyPrompt"]), stringValueFromAny(after["negativePrompt"]), boolValueFromAny(after["lockReference"]), userID)
		if err != nil {
			return err
		}
		if err := production.MarkAssetDownstreamStale(ctx, tx, project.ID, entityID); err != nil {
			return err
		}
		return production.MarkFinalVideoStale(ctx, tx, project.ID, "")
	case "storyboard_shot":
		_, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET visual = NULLIF($3, ''),
			    camera = NULLIF($4, ''),
			    motion = NULLIF($5, ''),
			    mood = NULLIF($6, ''),
			    duration_seconds = $7,
			    image_prompt = NULLIF($8, ''),
			    video_prompt = NULLIF($9, ''),
			    review_status = 'pending',
			    manual_override = true,
			    stale_state = 'needs_regeneration',
			    image_status = CASE
			      WHEN image_artifact_id IS NOT NULL OR image_media_file_id IS NOT NULL OR COALESCE(image_storage_key, '') <> '' THEN 'stale'
			      ELSE image_status
			    END,
			    video_status = CASE
			      WHEN video_artifact_id IS NOT NULL OR video_media_file_id IS NOT NULL OR COALESCE(video_storage_key, '') <> '' THEN 'stale'
			      ELSE video_status
			    END,
			    edited_by = $10,
			    edited_at = now(),
			    updated_at = now()
			WHERE project_id = $1 AND id = $2
		`, project.ID, entityID, stringValueFromAny(after["visual"]), stringValueFromAny(after["camera"]), stringValueFromAny(after["motion"]),
			stringValueFromAny(after["mood"]), nullableFloat64Value(after["durationSeconds"]), stringValueFromAny(after["imagePrompt"]),
			stringValueFromAny(after["videoPrompt"]), userID)
		if err != nil {
			return err
		}
		if err := production.MarkShotDownstreamStale(ctx, tx, project.ID, entityID); err != nil {
			return err
		}
		return production.MarkFinalVideoStale(ctx, tx, project.ID, "")
	case "shot_asset_requirement":
		_, err := tx.Exec(ctx, `
			UPDATE shot_asset_requirements
			SET costume = NULLIF($3, ''),
			    pose = NULLIF($4, ''),
			    expression = NULLIF($5, ''),
			    action = NULLIF($6, ''),
			    camera_relation = NULLIF($7, ''),
			    scene_state = NULLIF($8, ''),
			    prop_state = NULLIF($9, ''),
			    prompt = NULLIF($10, ''),
			    review_status = 'pending',
			    manual_override = true,
			    stale_state = 'needs_regeneration',
			    edited_by = $11,
			    edited_at = now(),
			    updated_at = now()
			WHERE project_id = $1 AND id = $2
		`, project.ID, entityID, stringValueFromAny(after["costume"]), stringValueFromAny(after["pose"]), stringValueFromAny(after["expression"]),
			stringValueFromAny(after["action"]), stringValueFromAny(after["cameraRelation"]), stringValueFromAny(after["sceneState"]),
			stringValueFromAny(after["propState"]), stringValueFromAny(after["prompt"]), userID)
		if err != nil {
			return err
		}
		if err := production.MarkRequirementDownstreamStale(ctx, tx, project.ID, entityID); err != nil {
			return err
		}
		return production.MarkFinalVideoStale(ctx, tx, project.ID, "")
	case "timeline_clip":
		_, err := tx.Exec(ctx, `
			UPDATE timeline_clips
			SET title = $3,
			    enabled = $4,
			    trim_start_seconds = $5,
			    trim_end_seconds = $6,
			    target_duration_seconds = $7,
			    notes = NULLIF($8, ''),
			    manual_override = true,
			    stale_state = 'needs_regeneration',
			    edited_by = $9,
			    edited_at = now(),
			    updated_at = now()
			WHERE project_id = $1 AND id = $2
		`, project.ID, entityID, stringValueFromAny(after["title"]), boolValueFromAny(after["enabled"]), float64Value(after["trimStartSeconds"]),
			nullableFloat64Value(after["trimEndSeconds"]), nullableFloat64Value(after["targetDurationSeconds"]), stringValueFromAny(after["notes"]), userID)
		if err != nil {
			return err
		}
		return production.MarkFinalVideoStale(ctx, tx, project.ID, "")
	case "project_timeline":
		_, err := tx.Exec(ctx, `
			UPDATE project_timelines
			SET title = $3,
			    aspect_ratio = $4,
			    resolution = $5,
			    metadata = $6,
			    manual_override = true,
			    stale_state = 'needs_regeneration',
			    edited_by = $7,
			    edited_at = now(),
			    updated_at = now()
			WHERE project_id = $1 AND id = $2
		`, project.ID, entityID, stringValueFromAny(after["title"]), stringValueFromAny(after["aspectRatio"]), stringValueFromAny(after["resolution"]),
			jsonValueOrDefault(after["metadata"], map[string]any{}), userID)
		if err != nil {
			return err
		}
		return production.MarkFinalVideoStale(ctx, tx, project.ID, "")
	default:
		return reviewpkg.ErrUnsupportedFixTarget
	}
}

func markScriptSceneDownstreamStaleFromContext(ctx context.Context, db scriptSceneExecer, projectID, sceneID string) error {
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

func (s *Server) startReviewFixRegeneration(w http.ResponseWriter, r *http.Request, principal auth.Principal, project Project, raw json.RawMessage) (string, bool) {
	var req struct {
		TargetType string         `json:"targetType"`
		TargetID   string         `json:"targetId"`
		Options    map[string]any `json:"options"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "regenerateRequest is invalid", nil, false)
		return "", false
	}
	workflowType, workflowFunc, permissions, ok := regenerationWorkflow(strings.TrimSpace(req.TargetType))
	if !ok {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "regeneration targetType is not supported", nil, false)
		return "", false
	}
	if !s.authorizeAny(w, r, principal, permissions, authz.Resource{ProjectID: project.ID}) {
		return "", false
	}
	resolvedTargetID, ok := s.requireRegenerationTarget(w, r, project.ID, strings.TrimSpace(req.TargetType), strings.TrimSpace(req.TargetID))
	if !ok {
		return "", false
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
		return "", false
	}
	return run.ID, true
}

func scanReviewFix(row rowScan) (ReviewFix, error) {
	var item ReviewFix
	var targetEntityID, promptVersionID, promptHash, providerCallID, errorCode, errorMessage, createdBy, appliedBy sql.NullString
	var regenerateRequest []byte
	err := row.Scan(&item.ID, &item.OrganizationID, &item.ProjectID, &item.ReviewItemID, &item.TargetEntityType, &targetEntityID, &item.Status, &item.FixType, &item.Title, &item.Explanation, &item.BeforeSnapshot, &item.Patch, &item.AfterPreview, &regenerateRequest, &promptVersionID, &promptHash, &providerCallID, &errorCode, &errorMessage, &createdBy, &appliedBy, &item.CreatedAt, &item.AppliedAt, &item.UpdatedAt)
	item.TargetEntityID = stringPtrFromNull(targetEntityID)
	item.PromptVersionID = stringPtrFromNull(promptVersionID)
	item.PromptHash = stringPtrFromNull(promptHash)
	item.ProviderCallID = stringPtrFromNull(providerCallID)
	item.ErrorCode = stringPtrFromNull(errorCode)
	item.ErrorMessage = stringPtrFromNull(errorMessage)
	item.CreatedBy = stringPtrFromNull(createdBy)
	item.AppliedBy = stringPtrFromNull(appliedBy)
	if len(regenerateRequest) > 0 && string(regenerateRequest) != "null" {
		item.RegenerateRequest = json.RawMessage(regenerateRequest)
	}
	return item, err
}

func reviewFixColumns() string {
	return `id, organization_id, project_id, review_item_id, target_entity_type, target_entity_id::text, status, fix_type,
	        title, explanation, before_snapshot, patch, after_preview, regenerate_request,
	        prompt_version_id::text, prompt_hash, provider_call_id::text, error_code, error_message,
	        created_by::text, applied_by::text, created_at, applied_at, updated_at`
}

func reviewFixReturningSQL() string {
	return reviewFixColumns()
}

func normalizeReviewFixType(value string) string {
	switch strings.TrimSpace(value) {
	case "patch", "regenerate", "navigate", "note":
		return strings.TrimSpace(value)
	default:
		return "patch"
	}
}

func rawObject(raw json.RawMessage) map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func rawNullableObject(value map[string]any) any {
	if len(value) == 0 {
		return nil
	}
	return mustRawJSON(value)
}

func regenerateRequestForReviewItem(item ReviewItem, targetID string) map[string]any {
	switch item.EntityType {
	case "canonical_asset":
		return map[string]any{"targetType": "canonical_asset_image", "targetId": targetID}
	case "storyboard_shot":
		targetType := "shot_image"
		if item.Category == "shot_video" {
			targetType = "shot_video"
		}
		return map[string]any{"targetType": targetType, "targetId": targetID}
	case "shot_asset_requirement":
		return map[string]any{"targetType": "derived_asset_image", "targetId": targetID}
	case "script_scene":
		return map[string]any{"targetType": "scene_storyboard", "targetId": targetID, "options": map[string]any{"maxShots": 3}}
	default:
		return nil
	}
}

func blankString(value any) bool {
	return strings.TrimSpace(stringValueFromAny(value)) == ""
}

func stringValueFromAny(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func boolValueFromAny(value any) bool {
	valueBool, ok := value.(bool)
	return ok && valueBool
}

func float64Value(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		parsed, _ := typed.Float64()
		return parsed
	default:
		return 0
	}
}

func nullableFloat64Value(value any) *float64 {
	if value == nil {
		return nil
	}
	parsed := float64Value(value)
	if parsed <= 0 {
		return nil
	}
	return &parsed
}

func nonPositiveNumber(value any) bool {
	return value == nil || float64Value(value) <= 0
}

func jsonValueOrDefault(value any, fallback any) json.RawMessage {
	if value == nil {
		return mustRawJSON(fallback)
	}
	return mustRawJSON(value)
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
