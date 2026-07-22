package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/assetprompts"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/jackc/pgx/v5"
)

type assetCardVisualContext struct {
	ManualTemplateKey        string
	ManualPromptVersionID    string
	ManualContentHash        string
	StyleSlug                string
	AssetType                string
	PrefixTemplateKey        string
	PrefixPromptVersionID    string
	AssetTypeTemplateKey     string
	AssetTypePromptVersionID string
	StylePrefix              string
	AssetTypeRules           string
	ManualFallback           string
}

func (s *Server) resolveAssetCardVisualContext(ctx context.Context, project Project, requestedVersionID, assetType string) (assetCardVisualContext, error) {
	return s.resolveAssetCardVisualContextWithDB(ctx, s.db, project, requestedVersionID, assetType)
}

func (s *Server) resolveAssetCardVisualContextWithDB(ctx context.Context, db snapshotQuerier, project Project, requestedVersionID, assetType string) (assetCardVisualContext, error) {
	requestedVersionID = strings.TrimSpace(requestedVersionID)
	visual := assetCardVisualContext{AssetType: strings.ToLower(strings.TrimSpace(assetType))}
	var content string
	var metadata []byte
	query := `
		SELECT pt.template_key, pv.id::text, pv.content_hash, pv.content, pv.metadata
		FROM project_manual_bindings b
		JOIN prompt_versions pv ON pv.id = b.prompt_version_id
		JOIN prompt_templates pt ON pt.id = COALESCE(pv.template_id, pv.prompt_template_id)
		WHERE b.project_id = $1
		  AND b.manual_kind = 'visual'
		  AND b.status = 'active'
		  AND pv.status = 'active'
		ORDER BY b.updated_at DESC
		LIMIT 1
	`
	args := []any{project.ID}
	if requestedVersionID != "" {
		query = `
			SELECT pt.template_key, pv.id::text, pv.content_hash, pv.content, pv.metadata
			FROM project_manual_bindings b
			JOIN prompt_versions pv ON pv.id = b.prompt_version_id
			JOIN prompt_templates pt ON pt.id = COALESCE(pv.template_id, pv.prompt_template_id)
			WHERE b.project_id = $1
			  AND b.manual_kind = 'visual'
			  AND b.prompt_version_id = $2
			ORDER BY b.updated_at DESC
			LIMIT 1
		`
		args = append(args, requestedVersionID)
	}
	err := db.QueryRow(ctx, query, args...).Scan(
		&visual.ManualTemplateKey,
		&visual.ManualPromptVersionID,
		&visual.ManualContentHash,
		&content,
		&metadata,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if requestedVersionID != "" {
			return assetCardVisualContext{}, newAPIError(http.StatusUnprocessableEntity, "VISUAL_MANUAL_SNAPSHOT_INVALID", "visual manual snapshot is not bound to this project")
		}
		visual.ManualTemplateKey = "project_visual_manual_text"
		visual.ManualContentHash = promptsvc.HashText(project.VisualManual)
		visual.ManualFallback = assetprompts.RuntimeManualSummary(project.VisualManual, assetprompts.RuntimeAssetCardManualFallbackMaxRunes)
		return visual, nil
	}
	if err != nil {
		return assetCardVisualContext{}, err
	}

	var metadataValues map[string]any
	_ = json.Unmarshal(metadata, &metadataValues)
	visual.StyleSlug = strings.TrimSpace(stringMapValue(metadataValues, "styleSlug"))
	if visual.StyleSlug == "" {
		visual.StyleSlug = strings.TrimPrefix(visual.ManualTemplateKey, "toonflow_visual_manual_")
		if visual.StyleSlug == visual.ManualTemplateKey {
			visual.StyleSlug = ""
		}
	}
	visual.StyleSlug = assetprompts.ToonflowStyleSlug(visual.StyleSlug)

	suffix := assetprompts.ToonflowVisualTemplateSuffix(visual.AssetType, false)
	if visual.StyleSlug == "" || suffix == "" {
		visual.ManualFallback = assetprompts.RuntimeManualSummary(content, assetprompts.RuntimeAssetCardManualFallbackMaxRunes)
		return visual, nil
	}

	visual.PrefixTemplateKey = "toonflow_visual_" + visual.StyleSlug + "_prefix"
	visual.AssetTypeTemplateKey = "toonflow_visual_" + visual.StyleSlug + "_" + suffix
	prefix, err := s.resolveAssetCardVisualRuleWithDB(ctx, db, project, visual.PrefixTemplateKey)
	if err != nil {
		return assetCardVisualContext{}, err
	}
	target, err := s.resolveAssetCardVisualRuleWithDB(ctx, db, project, visual.AssetTypeTemplateKey)
	if err != nil {
		return assetCardVisualContext{}, err
	}
	visual.PrefixPromptVersionID = prefix.PromptVersionID
	visual.AssetTypePromptVersionID = target.PromptVersionID
	visual.StylePrefix = assetprompts.RuntimeManualSummary(prefix.RenderedText, assetprompts.RuntimeAssetCardVisualPrefixMaxRunes)
	visual.AssetTypeRules = assetprompts.RuntimeManualSummary(target.RenderedText, assetprompts.RuntimeAssetCardVisualTemplateMaxRunes)
	if visual.StylePrefix == "" || visual.AssetTypeRules == "" {
		return assetCardVisualContext{}, newAPIError(http.StatusUnprocessableEntity, "VISUAL_MANUAL_RULES_MISSING", "visual manual does not contain usable rules for this asset type")
	}
	return visual, nil
}

func (s *Server) resolveAssetCardVisualRuleWithDB(ctx context.Context, db promptsvc.QueryRower, project Project, templateKey string) (promptsvc.RenderedPrompt, error) {
	resolved, err := promptsvc.NewService(db).Resolve(ctx, promptsvc.ResolveRequest{
		OrganizationID: project.OrganizationID,
		ProjectID:      project.ID,
		TemplateKey:    templateKey,
	})
	if err != nil {
		return promptsvc.RenderedPrompt{}, err
	}
	return promptsvc.Render(resolved, map[string]any{})
}

func (visual assetCardVisualContext) promptVariables() map[string]any {
	return map[string]any{
		"manualTemplateKey":        visual.ManualTemplateKey,
		"manualPromptVersionId":    visual.ManualPromptVersionID,
		"manualContentHash":        visual.ManualContentHash,
		"styleSlug":                visual.StyleSlug,
		"styleFamily":              assetprompts.VisualStyleFamily(visual.StyleSlug),
		"assetType":                visual.AssetType,
		"prefixTemplateKey":        visual.PrefixTemplateKey,
		"prefixPromptVersionId":    visual.PrefixPromptVersionID,
		"assetTypeTemplateKey":     visual.AssetTypeTemplateKey,
		"assetTypePromptVersionId": visual.AssetTypePromptVersionID,
		"stylePrefix":              visual.StylePrefix,
		"assetTypeRules":           visual.AssetTypeRules,
		"manualFallback":           visual.ManualFallback,
	}
}

func stringMapValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return value
}
