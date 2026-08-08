package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/production"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/Einzieg/cineweave/internal/provider"
)

type assetPromptRevisionDraft struct {
	BasePrompt        string `json:"basePrompt"`
	ConsistencyPrompt string `json:"consistencyPrompt"`
	NegativePrompt    string `json:"negativePrompt"`
}

type assetPromptRevisionActionInput struct {
	AssetID     string   `json:"assetId"`
	AssetName   string   `json:"assetName"`
	Instruction string   `json:"instruction"`
	Fields      []string `json:"fields"`
}

type assetPromptRevisionActionResult struct {
	AssetID          string         `json:"assetId"`
	AssetName        string         `json:"assetName"`
	Before           map[string]any `json:"before"`
	After            map[string]any `json:"after"`
	ChangedFields    []string       `json:"changedFields"`
	ProviderCallID   string         `json:"providerCallId,omitempty"`
	ModelID          string         `json:"modelId,omitempty"`
	CommandID        string         `json:"projectControlCommandId,omitempty"`
	AgentTaskID      string         `json:"agentTaskId,omitempty"`
	AgentStepID      string         `json:"agentStepId,omitempty"`
	IdempotencyKey   string         `json:"idempotencyKey,omitempty"`
	IdempotentReplay bool           `json:"idempotentReplay"`
}

func (s *Server) agentToolGetCanonicalAsset(r *http.Request, project Project, args map[string]any) agentToolResult {
	includePreviewURL, _ := agentBoolArg(args, "includePreviewUrl")
	input := assetGetActionInput{
		AssetID:           agentReferenceStringArg(args, "assetId"),
		AssetName:         agentStringArg(args, "assetName"),
		IncludePreviewURL: includePreviewURL,
	}
	asset, err := s.getCanonicalAssetAction(r.Context(), project, input)
	if err != nil {
		return agentToolError("asset.get", args, err)
	}
	return assetGetAgentResult(args, asset)
}

func (s *Server) executeAssetRevisePromptAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	var input assetPromptRevisionActionInput
	if err := decodeWorkflowActionInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	result, err := s.reviseCanonicalAssetPromptCore(ctx, principal, project, command, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("asset.revise_prompt", workflowActionArguments(raw), "已按要求修订资产提示词，并标记相关下游内容需要重新生成。", map[string]any{
		"assetId": result.AssetID, "assetName": result.AssetName,
		"before": result.Before, "after": result.After, "changedFields": result.ChangedFields,
		"providerCallId": result.ProviderCallID, "modelId": result.ModelID,
		"projectControlCommandId": result.CommandID,
		"agentTaskId":             result.AgentTaskID, "agentStepId": result.AgentStepID,
		"idempotencyKey": result.IdempotencyKey, "idempotentReplay": result.IdempotentReplay,
	}), nil
}

func (s *Server) reviseCanonicalAssetPromptCore(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	input assetPromptRevisionActionInput,
) (assetPromptRevisionActionResult, error) {
	asset, err := s.resolveCanonicalAssetReferenceContext(
		ctx, project.ID, strings.TrimSpace(input.AssetID), strings.TrimSpace(input.AssetName),
	)
	if err != nil {
		return assetPromptRevisionActionResult{}, err
	}
	if asset.Status == "archived" {
		return assetPromptRevisionActionResult{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "已归档资产不能修订提示词")
	}
	if replay, ok := assetPromptRevisionReplay(asset, command.ID); ok {
		return replay, nil
	}
	instruction := strings.TrimSpace(input.Instruction)
	if instruction == "" {
		return assetPromptRevisionActionResult{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "instruction 不能为空")
	}
	fields, selectedFields, err := normalizeAssetPromptRevisionFields(input.Fields)
	if err != nil {
		return assetPromptRevisionActionResult{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error())
	}
	before := assetPromptSnapshot(asset)
	r := requestWithContext(ctx)
	scenes, err := s.assetScenePromptContext(r, project.ID, asset.ID)
	if err != nil {
		return assetPromptRevisionActionResult{}, err
	}
	rendered, gatewayResp, err := s.runTextGatewayPrompt(r, project, "asset_prompt_revision", map[string]any{
		"project": projectPromptVariables(project),
		"asset": map[string]any{
			"id": asset.ID, "assetType": asset.AssetType, "name": asset.Name,
			"description": asset.Description, "profile": rawObject(asset.Profile),
			"basePrompt":        stringValue(asset.BasePrompt),
			"consistencyPrompt": stringValue(asset.ConsistencyPrompt),
			"negativePrompt":    stringValue(asset.NegativePrompt),
		},
		"input":  map[string]any{"instruction": instruction, "fields": fields},
		"scenes": scenes,
	}, true, authz.PermissionAssetWrite, provider.BillingContextReasonAgentAction)
	if err != nil {
		return assetPromptRevisionActionResult{}, err
	}
	revision, err := normalizeAssetPromptRevision(gatewayResp.Output.Text)
	if err != nil {
		return assetPromptRevisionActionResult{}, newAPIError(http.StatusBadGateway, "PROVIDER_OUTPUT_INVALID", err.Error())
	}
	after := map[string]any{
		"basePrompt": stringValue(asset.BasePrompt), "consistencyPrompt": stringValue(asset.ConsistencyPrompt),
		"negativePrompt": stringValue(asset.NegativePrompt),
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
	result := assetPromptRevisionActionResult{
		AssetID: asset.ID, AssetName: asset.Name, Before: before, After: after,
		ChangedFields: changedFields, ProviderCallID: gatewayResp.ProviderCallID, ModelID: gatewayResp.ModelID,
		CommandID: command.ID, AgentTaskID: command.AgentTaskID, AgentStepID: command.AgentStepID,
		IdempotencyKey: command.IdempotencyKey,
	}
	effect := map[string]any{
		"commandId": command.ID, "actionName": "asset.revise_prompt",
		"assetId": result.AssetID, "assetName": result.AssetName,
		"before": result.Before, "after": result.After, "changedFields": result.ChangedFields,
		"providerCallId": result.ProviderCallID, "modelId": result.ModelID,
		"agentTaskId": result.AgentTaskID, "agentStepId": result.AgentStepID,
		"idempotencyKey": result.IdempotencyKey,
	}
	metadata := map[string]any{
		"providerCallId": gatewayResp.ProviderCallID, "modelId": gatewayResp.ModelID,
		"promptTemplateKey": rendered.TemplateKey, "promptVersionId": rendered.PromptVersionID,
		"promptHash": rendered.RenderedHash, "promptSource": rendered.Source,
		"revisionInstruction": instruction, "revisionFields": fields,
		"projectControlCommandId": command.ID, "controllerType": command.ControllerType,
		"projectControlEffect": effect,
	}
	if command.AgentTaskID != "" {
		metadata["agentTaskId"] = command.AgentTaskID
	}
	if command.AgentStepID != "" {
		metadata["agentStepId"] = command.AgentStepID
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return assetPromptRevisionActionResult{}, err
	}
	defer tx.Rollback(ctx)
	update, err := tx.Exec(ctx, `
		UPDATE canonical_assets
		SET base_prompt = NULLIF($3, ''),
		    consistency_prompt = NULLIF($4, ''),
		    negative_prompt = NULLIF($5, ''),
		    status = $6,
		    review_status = 'pending',
		    manual_override = true,
		    stale_state = CASE WHEN $7 THEN 'needs_regeneration' ELSE stale_state END,
		    prompt_revision = prompt_revision + CASE WHEN $7 THEN 1 ELSE 0 END,
		    metadata = COALESCE(metadata, '{}'::jsonb) || $8::jsonb,
		    edited_by = $9,
		    edited_at = now(),
		    updated_at = now()
		WHERE project_id = $1 AND id = $2 AND revision = $10
	`, project.ID, asset.ID, stringValueFromAny(after["basePrompt"]), stringValueFromAny(after["consistencyPrompt"]),
		stringValueFromAny(after["negativePrompt"]), status, len(changedFields) > 0, mustMarshal(metadata), principal.UserID, asset.Revision)
	if err != nil {
		return assetPromptRevisionActionResult{}, err
	}
	if update.RowsAffected() != 1 {
		var currentRevision int64
		if err := tx.QueryRow(ctx, `SELECT revision FROM canonical_assets WHERE project_id = $1 AND id = $2`, project.ID, asset.ID).Scan(&currentRevision); err != nil {
			return assetPromptRevisionActionResult{}, err
		}
		return assetPromptRevisionActionResult{}, revisionConflictError("ASSET_REVISION_CONFLICT", "资产已被其他操作修改", asset.Revision, currentRevision)
	}
	if len(changedFields) > 0 {
		if err := production.MarkAssetDownstreamStale(ctx, tx, project.ID, asset.ID); err != nil {
			return assetPromptRevisionActionResult{}, err
		}
		if err := production.MarkFinalVideoStale(ctx, tx, project.ID, ""); err != nil {
			return assetPromptRevisionActionResult{}, err
		}
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "agent.asset.prompt_revised", "canonical_asset", asset.ID, mustRawJSON(map[string]any{
		"assetId": asset.ID, "assetName": asset.Name, "changedFields": changedFields,
		"providerCallId": gatewayResp.ProviderCallID, "projectControlCommandId": command.ID,
		"controllerType": command.ControllerType, "agentTaskId": command.AgentTaskID, "agentStepId": command.AgentStepID,
		"idempotencyKey": command.IdempotencyKey,
	})); err != nil {
		return assetPromptRevisionActionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return assetPromptRevisionActionResult{}, err
	}
	return result, nil
}

func assetPromptRevisionReplay(asset CanonicalAsset, commandID string) (assetPromptRevisionActionResult, bool) {
	if strings.TrimSpace(commandID) == "" {
		return assetPromptRevisionActionResult{}, false
	}
	effect := agentMapArg(rawObject(asset.Metadata), "projectControlEffect")
	if stringValueFromAny(effect["commandId"]) != commandID || stringValueFromAny(effect["actionName"]) != "asset.revise_prompt" {
		return assetPromptRevisionActionResult{}, false
	}
	return assetPromptRevisionActionResult{
		AssetID: asset.ID, AssetName: asset.Name,
		Before: agentMapArg(effect, "before"), After: agentMapArg(effect, "after"),
		ChangedFields:  agentStringSliceArg(effect, "changedFields"),
		ProviderCallID: stringValueFromAny(effect["providerCallId"]), ModelID: stringValueFromAny(effect["modelId"]),
		CommandID: commandID, AgentTaskID: stringValueFromAny(effect["agentTaskId"]),
		AgentStepID: stringValueFromAny(effect["agentStepId"]), IdempotencyKey: stringValueFromAny(effect["idempotencyKey"]),
		IdempotentReplay: true,
	}, true
}

func assetPromptSnapshot(asset CanonicalAsset) map[string]any {
	return map[string]any{
		"basePrompt": stringValue(asset.BasePrompt), "consistencyPrompt": stringValue(asset.ConsistencyPrompt),
		"negativePrompt": stringValue(asset.NegativePrompt),
	}
}

func normalizeAssetPromptRevisionFields(values []string) ([]string, map[string]bool, error) {
	allowed := map[string]bool{"basePrompt": true, "consistencyPrompt": true, "negativePrompt": true}
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
