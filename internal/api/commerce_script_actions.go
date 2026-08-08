package api

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/jackc/pgx/v5"
)

type commerceScriptSelectionActionInput struct {
	ScriptUnitID                string `json:"scriptUnitId"`
	StableOrdinal               int    `json:"stableOrdinal"`
	ExpectedScriptUnitsRevision int64  `json:"expectedScriptUnitsRevision"`
}

type commerceScriptCreateActionInput struct {
	ExpectedScriptUnitsRevision int64   `json:"expectedScriptUnitsRevision"`
	Title                       string  `json:"title"`
	Content                     string  `json:"content"`
	LanguageMode                string  `json:"languageMode"`
	ExplicitTargetLanguage      *string `json:"explicitTargetLanguage"`
	TargetDurationSeconds       int     `json:"targetDurationSeconds"`
	TargetPlatform              string  `json:"targetPlatform"`
	SourceLanguageHint          *string `json:"sourceLanguageHint"`
}

type commerceScriptUpdateActionInput struct {
	commerceScriptSelectionActionInput
	ExpectedRevision       int64   `json:"expectedRevision"`
	Title                  *string `json:"title"`
	DraftContent           *string `json:"draftContent"`
	LanguageMode           *string `json:"languageMode"`
	ExplicitTargetLanguage *string `json:"explicitTargetLanguage"`
	TargetDurationSeconds  *int    `json:"targetDurationSeconds"`
	TargetPlatform         *string `json:"targetPlatform"`
}

type commerceScriptArchiveActionInput struct {
	commerceScriptSelectionActionInput
	ExpectedRevision int64  `json:"expectedRevision"`
	Reason           string `json:"reason"`
}

type commerceScriptDuplicateActionInput struct {
	commerceScriptSelectionActionInput
	TargetLanguage *string `json:"targetLanguage"`
}

type commerceScriptDefaultsActionInput struct {
	ExpectedRevision      int64   `json:"expectedRevision"`
	TargetDurationSeconds int     `json:"targetDurationSeconds"`
	TargetPlatform        string  `json:"targetPlatform"`
	LanguageMode          string  `json:"languageMode"`
	TargetLanguage        *string `json:"targetLanguage"`
}

type commerceScriptReorderActionInput struct {
	ExpectedScriptUnitsRevision int64                               `json:"expectedScriptUnitsRevision"`
	Items                       []commercepkg.ReorderScriptUnitItem `json:"items"`
}

type commerceScriptReferenceArchiveActionInput struct {
	commerceScriptSelectionActionInput
	ReferenceID      string `json:"referenceId"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

type commerceScriptRebuildImpactActionInput struct {
	commerceScriptSelectionActionInput
	commercepkg.ScriptUnitRebuildTarget
}

func (s *Server) createCommerceScriptUnitActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input commerceScriptCreateActionInput,
) (commercepkg.ScriptVersionMutation, error) {
	if err := s.validateCommerceScriptContentForCurrentVideoModel(ctx, project, input.Content); err != nil {
		return commercepkg.ScriptVersionMutation{}, err
	}
	item, err := s.commerceCatalog.CreateScriptUnit(
		ctx, tx, project.OrganizationID, project.ID, actorUserID, input.ExpectedScriptUnitsRevision,
		commercepkg.CreateScriptUnitInput{
			Title: input.Title, Content: input.Content, LanguageMode: input.LanguageMode,
			ExplicitTargetLanguage: input.ExplicitTargetLanguage,
			TargetDurationSeconds:  input.TargetDurationSeconds,
			TargetPlatform:         input.TargetPlatform,
			SourceLanguageHint:     input.SourceLanguageHint,
		},
	)
	if err != nil {
		return commercepkg.ScriptVersionMutation{}, err
	}
	if err := appendCommerceScriptMutationEvents(
		ctx, tx, project.OrganizationID, project.ID, item, "commerce.script_unit.created",
	); err != nil {
		return commercepkg.ScriptVersionMutation{}, err
	}
	return item, nil
}

func (s *Server) updateCommerceScriptUnitActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input commerceScriptUpdateActionInput,
) (commercepkg.ScriptUnit, error) {
	if input.DraftContent != nil {
		if err := s.validateCommerceScriptContentForCurrentVideoModel(ctx, project, *input.DraftContent); err != nil {
			return commercepkg.ScriptUnit{}, err
		}
	}
	item, err := s.commerceCatalog.UpdateScriptUnit(
		ctx, tx, project.OrganizationID, project.ID, input.ScriptUnitID, actorUserID,
		input.ExpectedRevision, commercepkg.UpdateScriptUnitInput{
			Title: input.Title, DraftContent: input.DraftContent, LanguageMode: input.LanguageMode,
			ExplicitTargetLanguage: input.ExplicitTargetLanguage,
			TargetDurationSeconds:  input.TargetDurationSeconds,
			TargetPlatform:         input.TargetPlatform,
		},
	)
	if err != nil {
		return commercepkg.ScriptUnit{}, err
	}
	if err := appendCommerceScriptUnitEvent(
		ctx, tx, project.OrganizationID, project.ID, "commerce.script_unit.updated", item,
	); err != nil {
		return commercepkg.ScriptUnit{}, err
	}
	return item, nil
}

func (s *Server) archiveCommerceScriptUnitActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	input commerceScriptArchiveActionInput,
) (commercepkg.ScriptUnit, error) {
	item, err := s.commerceCatalog.ArchiveScriptUnit(
		ctx, tx, project.OrganizationID, project.ID, input.ScriptUnitID, input.ExpectedRevision,
	)
	if err != nil {
		return commercepkg.ScriptUnit{}, err
	}
	if err := appendCommerceScriptUnitEvent(
		ctx, tx, project.OrganizationID, project.ID, "commerce.script_unit.archived", item,
	); err != nil {
		return commercepkg.ScriptUnit{}, err
	}
	return item, nil
}

func (s *Server) reorderCommerceScriptUnitsActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	input commerceScriptReorderActionInput,
) (int64, error) {
	revision, err := s.commerceCatalog.ReorderScriptUnits(
		ctx, tx, project.OrganizationID, project.ID, input.ExpectedScriptUnitsRevision, input.Items,
	)
	if err != nil {
		return 0, err
	}
	scriptUnitIDs := make([]string, 0, len(input.Items))
	for _, item := range input.Items {
		scriptUnitIDs = append(scriptUnitIDs, item.ScriptUnitID)
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID,
		"commerce.script_unit.reordered", "project", project.ID, mustRawJSON(map[string]any{
			"commerceScriptUnitIds": scriptUnitIDs,
			"scriptUnitsRevision":   revision,
		})); err != nil {
		return 0, err
	}
	return revision, nil
}

func (s *Server) duplicateCommerceScriptUnitActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input commerceScriptDuplicateActionInput,
) (commercepkg.ScriptVersionMutation, error) {
	item, err := s.commerceCatalog.DuplicateScriptUnit(
		ctx, tx, project.OrganizationID, project.ID, input.ScriptUnitID, actorUserID,
		input.ExpectedScriptUnitsRevision, input.TargetLanguage,
	)
	if err != nil {
		return commercepkg.ScriptVersionMutation{}, err
	}
	if err := appendCommerceScriptMutationEvents(
		ctx, tx, project.OrganizationID, project.ID, item, "commerce.script_unit.created",
	); err != nil {
		return commercepkg.ScriptVersionMutation{}, err
	}
	return item, nil
}

func (s *Server) updateCommerceScriptDefaultsActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	input commerceScriptDefaultsActionInput,
) (Project, error) {
	if input.ExpectedRevision <= 0 {
		return Project{}, controlValidationError("expectedRevision 必须为正整数")
	}
	defaults, err := normalizedCommerceScriptUnitDefaults(commercepkg.ScriptUnitDefaults{
		TargetDurationSeconds: input.TargetDurationSeconds,
		TargetPlatform:        input.TargetPlatform,
		LanguageMode:          input.LanguageMode,
		TargetLanguage:        input.TargetLanguage,
	})
	if err != nil {
		return Project{}, err
	}
	options, err := s.loadCommerceProjectOptions(ctx, project.OrganizationID, true)
	if err != nil {
		return Project{}, err
	}
	if err := validateCommerceScriptUnitDefaults(options, project, defaults); err != nil {
		return Project{}, err
	}
	locked, err := scanProject(tx.QueryRow(ctx, projectSelectSQL(`WHERE p.id = $1 FOR UPDATE OF p`), project.ID))
	if err != nil {
		return Project{}, err
	}
	if locked.Revision != input.ExpectedRevision {
		return Project{}, projectRevisionConflict(locked, input.ExpectedRevision)
	}
	settings, err := mergeCommerceScriptUnitDefaults(locked.Settings, defaults)
	if err != nil {
		return Project{}, err
	}
	command, err := tx.Exec(ctx, `
		UPDATE projects
		SET settings = $2, revision = revision + 1, updated_at = now()
		WHERE id = $1 AND revision = $3
	`, locked.ID, settings, locked.Revision)
	if err != nil {
		return Project{}, err
	}
	if command.RowsAffected() != 1 {
		return Project{}, projectRevisionConflict(locked, locked.Revision)
	}
	updated, err := scanProject(tx.QueryRow(ctx, projectSelectSQL(`WHERE p.id = $1`), locked.ID))
	if err != nil {
		return Project{}, err
	}
	if err := insertAPIEvent(ctx, tx, locked.OrganizationID, locked.ID,
		"commerce.project.defaults.updated", "project", locked.ID, mustRawJSON(map[string]any{
			"revision": updated.Revision, "targetDurationSeconds": defaults.TargetDurationSeconds,
			"targetPlatform": defaults.TargetPlatform, "languageMode": defaults.LanguageMode,
			"targetLanguage": defaults.TargetLanguage,
		})); err != nil {
		return Project{}, err
	}
	if err := s.attachVideoProductionContext(ctx, tx, &updated); err != nil {
		return Project{}, err
	}
	return updated, nil
}

func (s *Server) archiveCommerceScriptReferenceActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	input commerceScriptReferenceArchiveActionInput,
) (commercepkg.ScriptReferenceImage, error) {
	item, err := s.commerceDirect.ArchiveScriptReference(
		ctx, tx, project.OrganizationID, project.ID, input.ScriptUnitID,
		input.ReferenceID, input.ExpectedRevision,
	)
	if err != nil {
		return commercepkg.ScriptReferenceImage{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID,
		"commerce.script_reference.archived", "commerce_script_reference_image", item.ID,
		mustRawJSON(map[string]any{
			"commerceScriptUnitId": item.ScriptUnitID,
			"scriptReferenceId":    item.ID,
			"revision":             item.Revision,
		})); err != nil {
		return commercepkg.ScriptReferenceImage{}, err
	}
	return item, nil
}

func (s *Server) planCommerceScriptRebuildActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input commerceScriptRebuildImpactActionInput,
) (commercepkg.ScriptUnitRebuildImpact, error) {
	return s.commerceCatalog.PlanScriptUnitRebuild(
		ctx, tx, project.OrganizationID, project.ID, input.ScriptUnitID,
		input.ScriptUnitRebuildTarget, actorUserID,
	)
}

func decodeCommerceScriptActionInput[T any](raw json.RawMessage, message string) (T, error) {
	var input T
	if err := json.Unmarshal(raw, &input); err != nil {
		return input, controlValidationError(message)
	}
	return input, nil
}

func (s *Server) resolveCommerceScriptSelectionForAction(
	ctx context.Context,
	project Project,
	raw json.RawMessage,
	argumentName string,
) (map[string]any, string, error) {
	arguments, err := decodeCommerceActionMap(raw)
	if err != nil {
		return nil, "", err
	}
	scriptUnitID, err := s.resolveCommerceActionScriptUnitID(ctx, project, arguments, argumentName, true)
	if err != nil {
		return nil, "", err
	}
	return arguments, scriptUnitID, nil
}

func (s *Server) executeCommerceScriptCreateSyncAction(
	ctx context.Context, tx pgx.Tx, principal auth.Principal, project Project,
	_ projectcontrol.Command, raw json.RawMessage,
) (agentToolResult, error) {
	input, err := decodeCommerceScriptActionInput[commerceScriptCreateActionInput](raw, "广告脚本创建参数无效")
	if err != nil {
		return agentToolResult{}, err
	}
	item, err := s.createCommerceScriptUnitActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	arguments, _ := decodeCommerceActionMap(raw)
	return agentToolOK("commerce.script.create", arguments, "广告脚本已创建", projectOperationalReadData(item)), nil
}

func (s *Server) executeCommerceScriptUpdateSyncAction(
	ctx context.Context, tx pgx.Tx, principal auth.Principal, project Project,
	_ projectcontrol.Command, raw json.RawMessage,
) (agentToolResult, error) {
	arguments, scriptUnitID, err := s.resolveCommerceScriptSelectionForAction(ctx, project, raw, "scriptUnitId")
	if err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeCommerceScriptActionInput[commerceScriptUpdateActionInput](raw, "广告脚本修改参数无效")
	if err != nil {
		return agentToolResult{}, err
	}
	input.ScriptUnitID = scriptUnitID
	item, err := s.updateCommerceScriptUnitActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.script.update", arguments, "广告脚本已更新", projectOperationalReadData(item)), nil
}

func (s *Server) executeCommerceScriptArchiveSyncAction(
	ctx context.Context, tx pgx.Tx, _ auth.Principal, project Project,
	_ projectcontrol.Command, raw json.RawMessage,
) (agentToolResult, error) {
	arguments, scriptUnitID, err := s.resolveCommerceScriptSelectionForAction(ctx, project, raw, "scriptUnitId")
	if err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeCommerceScriptActionInput[commerceScriptArchiveActionInput](raw, "广告脚本归档参数无效")
	if err != nil {
		return agentToolResult{}, err
	}
	input.ScriptUnitID = scriptUnitID
	item, err := s.archiveCommerceScriptUnitActionTx(ctx, tx, project, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.script.archive", arguments, "广告脚本已归档", projectOperationalReadData(item)), nil
}

func (s *Server) executeCommerceScriptDuplicateSyncAction(
	ctx context.Context, tx pgx.Tx, principal auth.Principal, project Project,
	_ projectcontrol.Command, raw json.RawMessage,
) (agentToolResult, error) {
	arguments, scriptUnitID, err := s.resolveCommerceScriptSelectionForAction(ctx, project, raw, "scriptUnitId")
	if err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeCommerceScriptActionInput[commerceScriptDuplicateActionInput](raw, "广告脚本复制参数无效")
	if err != nil {
		return agentToolResult{}, err
	}
	input.ScriptUnitID = scriptUnitID
	item, err := s.duplicateCommerceScriptUnitActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.script.duplicate", arguments, "广告脚本已复制", projectOperationalReadData(item)), nil
}

func (s *Server) executeCommerceScriptLanguageVariantSyncAction(
	ctx context.Context, tx pgx.Tx, principal auth.Principal, project Project,
	_ projectcontrol.Command, raw json.RawMessage,
) (agentToolResult, error) {
	arguments, scriptUnitID, err := s.resolveCommerceScriptSelectionForAction(ctx, project, raw, "scriptUnitId")
	if err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeCommerceScriptActionInput[commerceScriptDuplicateActionInput](raw, "多语言脚本参数无效")
	if err != nil {
		return agentToolResult{}, err
	}
	input.ScriptUnitID = scriptUnitID
	language := strings.TrimSpace(agentStringArg(arguments, "targetLanguage"))
	if language == "" {
		return agentToolResult{}, controlValidationError("targetLanguage 不能为空")
	}
	input.TargetLanguage = &language
	item, err := s.duplicateCommerceScriptUnitActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.script.create_language_variant", arguments, "多语言广告脚本已创建", projectOperationalReadData(item)), nil
}

func (s *Server) executeCommerceScriptReorderSyncAction(
	ctx context.Context, tx pgx.Tx, _ auth.Principal, project Project,
	_ projectcontrol.Command, raw json.RawMessage,
) (agentToolResult, error) {
	input, err := decodeCommerceScriptActionInput[commerceScriptReorderActionInput](raw, "广告脚本排序参数无效")
	if err != nil {
		return agentToolResult{}, err
	}
	revision, err := s.reorderCommerceScriptUnitsActionTx(ctx, tx, project, input)
	if err != nil {
		return agentToolResult{}, err
	}
	arguments, _ := decodeCommerceActionMap(raw)
	return agentToolOK("commerce.script.reorder", arguments, "广告脚本顺序已更新", map[string]any{"scriptUnitsRevision": revision}), nil
}

func (s *Server) executeCommerceScriptDefaultsUpdateSyncAction(
	ctx context.Context, tx pgx.Tx, _ auth.Principal, project Project,
	_ projectcontrol.Command, raw json.RawMessage,
) (agentToolResult, error) {
	input, err := decodeCommerceScriptActionInput[commerceScriptDefaultsActionInput](raw, "广告脚本默认值参数无效")
	if err != nil {
		return agentToolResult{}, err
	}
	updated, err := s.updateCommerceScriptDefaultsActionTx(ctx, tx, project, input)
	if err != nil {
		return agentToolResult{}, err
	}
	arguments, _ := decodeCommerceActionMap(raw)
	return agentToolOK("commerce.script.defaults.update", arguments, "广告脚本默认值已更新", projectOperationalReadData(updated)), nil
}

func (s *Server) executeCommerceScriptReferenceArchiveSyncAction(
	ctx context.Context, tx pgx.Tx, _ auth.Principal, project Project,
	_ projectcontrol.Command, raw json.RawMessage,
) (agentToolResult, error) {
	arguments, scriptUnitID, err := s.resolveCommerceScriptSelectionForAction(ctx, project, raw, "scriptUnitId")
	if err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeCommerceScriptActionInput[commerceScriptReferenceArchiveActionInput](raw, "脚本参考图归档参数无效")
	if err != nil {
		return agentToolResult{}, err
	}
	input.ScriptUnitID = scriptUnitID
	input.ReferenceID = strings.TrimSpace(input.ReferenceID)
	if input.ReferenceID == "" || input.ExpectedRevision <= 0 {
		return agentToolResult{}, controlValidationError("referenceId 和 expectedRevision 不能为空")
	}
	item, err := s.archiveCommerceScriptReferenceActionTx(ctx, tx, project, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.script.reference.archive", arguments, "脚本参考图已归档", projectOperationalReadData(item)), nil
}

func (s *Server) executeCommerceScriptRebuildImpactSyncAction(
	ctx context.Context, tx pgx.Tx, principal auth.Principal, project Project,
	_ projectcontrol.Command, raw json.RawMessage,
) (agentToolResult, error) {
	arguments, scriptUnitID, err := s.resolveCommerceScriptSelectionForAction(ctx, project, raw, "scriptUnitId")
	if err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeCommerceScriptActionInput[commerceScriptRebuildImpactActionInput](raw, "广告脚本换代影响参数无效")
	if err != nil {
		return agentToolResult{}, err
	}
	input.ScriptUnitID = scriptUnitID
	impact, err := s.planCommerceScriptRebuildActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.script.rebuild_impact", arguments, "广告脚本换代影响已计算", projectOperationalReadData(impact)), nil
}
