package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/production"
)

type assetPromptRevisionDraft struct {
	BasePrompt        string `json:"basePrompt"`
	ConsistencyPrompt string `json:"consistencyPrompt"`
	NegativePrompt    string `json:"negativePrompt"`
}

func (s *Server) agentToolGetCanonicalAsset(r *http.Request, project Project, args map[string]any) agentToolResult {
	asset, err := s.agentCanonicalAssetByReference(r, project.ID, args)
	if err != nil {
		return agentToolError("asset.get", args, err)
	}
	return agentToolOK("asset.get", args, "已读取完整资产卡。", map[string]any{
		"asset": agentCanonicalAssetSnapshot(asset),
	})
}

func (s *Server) agentToolReviseCanonicalAssetPrompt(
	r *http.Request,
	principal auth.Principal,
	project Project,
	task AgentTask,
	step AgentStep,
	args map[string]any,
) agentToolResult {
	asset, err := s.agentCanonicalAssetByReference(r, project.ID, args)
	if err != nil {
		return agentToolError("asset.revise_prompt", args, err)
	}
	if asset.Status == "archived" {
		return agentToolError("asset.revise_prompt", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "canonical asset is archived"))
	}
	instruction := strings.TrimSpace(agentStringArg(args, "instruction"))
	if instruction == "" {
		return agentToolError("asset.revise_prompt", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "instruction is required"))
	}
	fields, selectedFields, err := normalizeAssetPromptRevisionFields(agentStringSliceArg(args, "fields"))
	if err != nil {
		return agentToolError("asset.revise_prompt", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error()))
	}
	before := assetPromptSnapshot(asset)
	if already, err := s.agentPatchTargetHasStep(r.Context(), project.ID, "canonical_asset", asset.ID, step.ID); err != nil {
		return agentToolError("asset.revise_prompt", args, err)
	} else if already {
		return agentToolOK("asset.revise_prompt", args, "当前 Agent 步骤已完成资产提示词修订，未重复调用模型。", map[string]any{
			"assetId":        asset.ID,
			"assetName":      asset.Name,
			"before":         before,
			"after":          before,
			"changedFields":  []string{},
			"idempotent":     true,
			"agentTaskId":    task.ID,
			"agentStepId":    step.ID,
			"idempotencyKey": agentStepIdempotencyKey(task, step),
		})
	}
	scenes, err := s.assetScenePromptContext(r, project.ID, asset.ID)
	if err != nil {
		return agentToolError("asset.revise_prompt", args, err)
	}
	rendered, gatewayResp, err := s.runTextGatewayPrompt(r, project, "asset_prompt_revision", map[string]any{
		"project": projectPromptVariables(project),
		"asset": map[string]any{
			"id":                asset.ID,
			"assetType":         asset.AssetType,
			"name":              asset.Name,
			"description":       asset.Description,
			"profile":           rawObject(asset.Profile),
			"basePrompt":        stringValue(asset.BasePrompt),
			"consistencyPrompt": stringValue(asset.ConsistencyPrompt),
			"negativePrompt":    stringValue(asset.NegativePrompt),
		},
		"input": map[string]any{
			"instruction": instruction,
			"fields":      fields,
		},
		"scenes": scenes,
	}, true)
	if err != nil {
		return agentToolError("asset.revise_prompt", args, err)
	}
	revision, err := normalizeAssetPromptRevision(gatewayResp.Output.Text)
	if err != nil {
		return agentToolError("asset.revise_prompt", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error()))
	}
	after := map[string]any{
		"basePrompt":        stringValue(asset.BasePrompt),
		"consistencyPrompt": stringValue(asset.ConsistencyPrompt),
		"negativePrompt":    stringValue(asset.NegativePrompt),
	}
	if selectedFields["basePrompt"] {
		after["basePrompt"] = revision.BasePrompt
	}
	if selectedFields["consistencyPrompt"] {
		after["consistencyPrompt"] = revision.ConsistencyPrompt
	}
	if selectedFields["negativePrompt"] {
		after["negativePrompt"] = revision.NegativePrompt
	}
	changedFields := changedAssetPromptFields(before, after)
	status := "draft"
	if canonicalAssetPromptFieldsReady(stringValueFromAny(after["basePrompt"]), stringValueFromAny(after["consistencyPrompt"])) {
		status = "prompt_ready"
	}
	metadata := mergeAgentStepMetadata(map[string]any{
		"providerCallId":      gatewayResp.ProviderCallID,
		"modelId":             gatewayResp.ModelID,
		"promptTemplateKey":   rendered.TemplateKey,
		"promptVersionId":     rendered.PromptVersionID,
		"promptHash":          rendered.RenderedHash,
		"promptSource":        rendered.Source,
		"revisionInstruction": instruction,
		"revisionFields":      fields,
	}, task, step, "asset.revise_prompt")
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return agentToolError("asset.revise_prompt", args, err)
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `
		UPDATE canonical_assets
		SET base_prompt = NULLIF($3, ''),
		    consistency_prompt = NULLIF($4, ''),
		    negative_prompt = NULLIF($5, ''),
		    status = $6,
		    review_status = 'pending',
		    manual_override = true,
		    stale_state = CASE WHEN $7 THEN 'needs_regeneration' ELSE stale_state END,
		    metadata = COALESCE(metadata, '{}'::jsonb) || $8::jsonb,
		    edited_by = $9,
		    edited_at = now(),
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
	`, project.ID, asset.ID, stringValueFromAny(after["basePrompt"]), stringValueFromAny(after["consistencyPrompt"]),
		stringValueFromAny(after["negativePrompt"]), status, len(changedFields) > 0, mustMarshal(metadata), principal.UserID); err != nil {
		return agentToolError("asset.revise_prompt", args, err)
	}
	if len(changedFields) > 0 {
		if err := production.MarkAssetDownstreamStale(r.Context(), tx, project.ID, asset.ID); err != nil {
			return agentToolError("asset.revise_prompt", args, err)
		}
		if err := production.MarkFinalVideoStale(r.Context(), tx, project.ID, ""); err != nil {
			return agentToolError("asset.revise_prompt", args, err)
		}
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "agent.asset.prompt_revised", "canonical_asset", asset.ID, mustRawJSON(map[string]any{
		"assetId":        asset.ID,
		"assetName":      asset.Name,
		"changedFields":  changedFields,
		"providerCallId": gatewayResp.ProviderCallID,
		"agentTaskId":    task.ID,
		"agentStepId":    step.ID,
		"idempotencyKey": agentStepIdempotencyKey(task, step),
	})); err != nil {
		return agentToolError("asset.revise_prompt", args, err)
	}
	if err := tx.Commit(r.Context()); err != nil {
		return agentToolError("asset.revise_prompt", args, err)
	}
	return agentToolOK("asset.revise_prompt", args, "已按用户要求修订资产提示词，并标记相关下游内容需要重新生成。", map[string]any{
		"assetId":        asset.ID,
		"assetName":      asset.Name,
		"before":         before,
		"after":          after,
		"changedFields":  changedFields,
		"providerCallId": gatewayResp.ProviderCallID,
		"modelId":        gatewayResp.ModelID,
		"agentTaskId":    task.ID,
		"agentStepId":    step.ID,
		"idempotencyKey": agentStepIdempotencyKey(task, step),
	})
}

func (s *Server) agentCanonicalAssetByReference(r *http.Request, projectID string, args map[string]any) (CanonicalAsset, error) {
	if assetID := agentReferenceStringArg(args, "assetId"); assetID != "" {
		return s.canonicalAsset(r, projectID, assetID)
	}
	assetName := strings.TrimSpace(agentStringArg(args, "assetName"))
	if assetName == "" {
		return CanonicalAsset{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "assetId or assetName is required")
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id::text
		FROM canonical_assets
		WHERE project_id = $1
		  AND lower(name) = lower($2)
		  AND COALESCE(status, 'draft') <> 'archived'
		ORDER BY updated_at DESC, created_at DESC
		LIMIT 2
	`, projectID, assetName)
	if err != nil {
		return CanonicalAsset{}, err
	}
	defer rows.Close()
	ids := make([]string, 0, 2)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return CanonicalAsset{}, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return CanonicalAsset{}, err
	}
	if len(ids) == 0 {
		return CanonicalAsset{}, newAPIError(http.StatusNotFound, "ASSET_NOT_FOUND", "asset was not found by name")
	}
	if len(ids) > 1 {
		return CanonicalAsset{}, newAPIError(http.StatusUnprocessableEntity, "AMBIGUOUS_ASSET_NAME", "multiple assets have this name; use assetId")
	}
	return s.canonicalAsset(r, projectID, ids[0])
}

func agentCanonicalAssetSnapshot(asset CanonicalAsset) map[string]any {
	return map[string]any{
		"id":                   asset.ID,
		"assetType":            asset.AssetType,
		"name":                 asset.Name,
		"description":          asset.Description,
		"profile":              rawObject(asset.Profile),
		"basePrompt":           stringValue(asset.BasePrompt),
		"consistencyPrompt":    stringValue(asset.ConsistencyPrompt),
		"negativePrompt":       stringValue(asset.NegativePrompt),
		"visualTraits":         rawObject(asset.VisualTraits),
		"lockReference":        asset.LockReference,
		"status":               asset.Status,
		"reviewStatus":         asset.ReviewStatus,
		"staleState":           asset.StaleState,
		"manualOverride":       asset.ManualOverride,
		"promptReady":          canonicalAssetPromptReady(asset),
		"sceneCount":           asset.SceneCount,
		"referenceCount":       asset.ReferenceCount,
		"shotRequirementCount": asset.ShotRequirementCount,
		"updatedAt":            asset.UpdatedAt,
	}
}

func assetPromptSnapshot(asset CanonicalAsset) map[string]any {
	return map[string]any{
		"basePrompt":        stringValue(asset.BasePrompt),
		"consistencyPrompt": stringValue(asset.ConsistencyPrompt),
		"negativePrompt":    stringValue(asset.NegativePrompt),
	}
}

func normalizeAssetPromptRevisionFields(values []string) ([]string, map[string]bool, error) {
	allowed := map[string]bool{
		"basePrompt":        true,
		"consistencyPrompt": true,
		"negativePrompt":    true,
	}
	if len(values) == 0 {
		values = []string{"basePrompt", "consistencyPrompt", "negativePrompt"}
	}
	fields := make([]string, 0, len(values))
	selected := make(map[string]bool, len(values))
	for _, field := range values {
		field = strings.TrimSpace(field)
		if !allowed[field] {
			return nil, nil, fmt.Errorf("unsupported asset prompt field %q", field)
		}
		if selected[field] {
			continue
		}
		selected[field] = true
		fields = append(fields, field)
	}
	return fields, selected, nil
}

func normalizeAssetPromptRevision(text string) (assetPromptRevisionDraft, error) {
	candidate := strings.TrimSpace(text)
	if strings.HasPrefix(candidate, "```") {
		if newline := strings.IndexByte(candidate, '\n'); newline >= 0 {
			candidate = candidate[newline+1:]
		}
		candidate = strings.TrimSpace(strings.TrimSuffix(candidate, "```"))
	}
	var draft assetPromptRevisionDraft
	if err := json.Unmarshal([]byte(candidate), &draft); err != nil {
		return assetPromptRevisionDraft{}, err
	}
	draft.BasePrompt = strings.TrimSpace(draft.BasePrompt)
	draft.ConsistencyPrompt = strings.TrimSpace(draft.ConsistencyPrompt)
	draft.NegativePrompt = strings.TrimSpace(draft.NegativePrompt)
	if !canonicalAssetPromptFieldsReady(draft.BasePrompt, draft.ConsistencyPrompt) {
		return assetPromptRevisionDraft{}, fmt.Errorf("basePrompt and consistencyPrompt are required")
	}
	return draft, nil
}

func changedAssetPromptFields(before, after map[string]any) []string {
	fields := []string{"basePrompt", "consistencyPrompt", "negativePrompt"}
	changed := make([]string, 0, len(fields))
	for _, field := range fields {
		if strings.TrimSpace(stringValueFromAny(before[field])) != strings.TrimSpace(stringValueFromAny(after[field])) {
			changed = append(changed, field)
		}
	}
	return changed
}
