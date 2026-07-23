package api

import (
	"context"

	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/events"
)

func appendCommerceProductMutationEvents(
	ctx context.Context,
	exec events.Execer,
	organizationID string,
	projectID string,
	result commercepkg.ProductMutationResult,
) error {
	versionPayload := mustRawJSON(map[string]any{
		"productId":        result.Product.ID,
		"productVersionId": result.Version.ID,
		"version":          result.Version.Version,
		"activated":        result.Activated,
	})
	if err := insertAPIEvent(ctx, exec, organizationID, projectID,
		"commerce.product.version.created", "commerce_product_version", result.Version.ID, versionPayload); err != nil {
		return err
	}
	if err := insertAPIEvent(ctx, exec, organizationID, projectID,
		"commerce.product.updated", "commerce_product", result.Product.ID, mustRawJSON(map[string]any{
			"productId":        result.Product.ID,
			"productVersionId": result.Version.ID,
			"revision":         result.Product.Revision,
			"activated":        result.Activated,
			"requiresRebuild":  result.RequiresRebuild,
		})); err != nil {
		return err
	}
	if !result.Activated {
		return nil
	}
	return insertAPIEvent(ctx, exec, organizationID, projectID,
		"commerce.product.version.activated", "commerce_product_version", result.Version.ID, versionPayload)
}

func appendCommerceProductReferenceEvent(
	ctx context.Context,
	exec events.Execer,
	organizationID string,
	projectID string,
	eventName string,
	item commercepkg.ProductReference,
) error {
	return insertAPIEvent(ctx, exec, organizationID, projectID, eventName,
		"commerce_product_reference", item.ID, mustRawJSON(map[string]any{
			"productId":          item.ProductID,
			"productReferenceId": item.ID,
			"referenceRole":      item.ReferenceRole,
			"isPrimary":          item.IsPrimary,
			"revision":           item.Revision,
			"status":             item.Status,
		}))
}

func appendCommerceScriptUnitEvent(
	ctx context.Context,
	exec events.Execer,
	organizationID string,
	projectID string,
	eventName string,
	item commercepkg.ScriptUnit,
) error {
	payload := map[string]any{
		"commerceScriptUnitId": item.ID,
		"revision":             item.Revision,
		"status":               item.Status,
		"unitNo":               item.UnitNo,
		"sortOrder":            item.SortOrder,
	}
	if item.ActiveUnitGenerationID != nil {
		payload["scriptUnitGenerationId"] = *item.ActiveUnitGenerationID
	}
	return insertAPIEvent(ctx, exec, organizationID, projectID, eventName,
		"commerce_script_unit", item.ID, mustRawJSON(payload))
}

func appendCommerceScriptVersionEvent(
	ctx context.Context,
	exec events.Execer,
	organizationID string,
	projectID string,
	eventName string,
	version commercepkg.ScriptVersion,
) error {
	return insertAPIEvent(ctx, exec, organizationID, projectID, eventName,
		"commerce_script_version", version.ID, mustRawJSON(map[string]any{
			"commerceScriptUnitId": version.ScriptUnitID,
			"scriptVersionId":      version.ID,
			"version":              version.Version,
		}))
}

func appendCommerceScriptMutationEvents(
	ctx context.Context,
	exec events.Execer,
	organizationID string,
	projectID string,
	result commercepkg.ScriptVersionMutation,
	unitEventName string,
) error {
	if unitEventName != "" {
		if err := appendCommerceScriptUnitEvent(ctx, exec, organizationID, projectID, unitEventName, result.ScriptUnit); err != nil {
			return err
		}
	}
	if result.Version.ID == "" {
		return nil
	}
	if err := appendCommerceScriptVersionEvent(ctx, exec, organizationID, projectID,
		"commerce.script.version.created", result.Version); err != nil {
		return err
	}
	if !result.Activated {
		return nil
	}
	return appendCommerceScriptVersionEvent(ctx, exec, organizationID, projectID,
		"commerce.script.version.activated", result.Version)
}

func appendCommerceLanguageEvent(
	ctx context.Context,
	exec events.Execer,
	organizationID string,
	projectID string,
	item commercepkg.LanguageResolution,
) error {
	eventName := "commerce.language.resolved"
	if item.NeedsUserConfirmation || item.Status == "needs_confirmation" {
		eventName = "commerce.language.confirmation_required"
	}
	return insertAPIEvent(ctx, exec, organizationID, projectID, eventName,
		"commerce_language_resolution", item.ID, mustRawJSON(map[string]any{
			"commerceScriptUnitId":  item.ScriptUnitID,
			"languageResolutionId":  item.ID,
			"sourceScriptVersionId": item.SourceScriptVersionID,
			"sourceLanguage":        item.SourceLanguage,
			"targetLanguage":        item.TargetLanguage,
			"confidence":            item.Confidence,
			"status":                item.Status,
		}))
}

func appendCommerceLocalizationEvent(
	ctx context.Context,
	exec events.Execer,
	organizationID string,
	projectID string,
	eventName string,
	item commercepkg.ScriptLocalization,
) error {
	return insertAPIEvent(ctx, exec, organizationID, projectID, eventName,
		"commerce_script_localization", item.ID, mustRawJSON(map[string]any{
			"commerceScriptUnitId":  item.ScriptUnitID,
			"localizationId":        item.ID,
			"sourceScriptVersionId": item.SourceScriptVersionID,
			"sourceLanguage":        item.SourceLanguage,
			"targetLanguage":        item.TargetLanguage,
			"reviewStatus":          item.ReviewStatus,
			"status":                item.Status,
			"revision":              item.Revision,
		}))
}

func appendCommerceStoryboardPlanEvent(
	ctx context.Context,
	exec events.Execer,
	organizationID string,
	projectID string,
	eventName string,
	plan commercepkg.StoryboardPlan,
) error {
	payload := map[string]any{
		"commerceScriptUnitId":     plan.ScriptUnitID,
		"scriptUnitGenerationId":   plan.UnitGenerationID,
		"commerceStoryboardPlanId": plan.ID,
		"revision":                 plan.EditRevision,
		"status":                   plan.Status,
	}
	if plan.WorkflowRunID != nil {
		payload["workflowRunId"] = *plan.WorkflowRunID
	}
	return insertAPIEvent(ctx, exec, organizationID, projectID, eventName,
		"commerce_storyboard_plan", plan.ID, mustRawJSON(payload))
}

func appendCommerceShotEvent(
	ctx context.Context,
	exec events.Execer,
	organizationID string,
	projectID string,
	plan commercepkg.StoryboardPlan,
	shotID string,
	operation string,
) error {
	return insertAPIEvent(ctx, exec, organizationID, projectID,
		"commerce.shot.updated", "storyboard_shot", shotID, mustRawJSON(map[string]any{
			"commerceScriptUnitId":     plan.ScriptUnitID,
			"scriptUnitGenerationId":   plan.UnitGenerationID,
			"commerceStoryboardPlanId": plan.ID,
			"storyboardShotId":         shotID,
			"revision":                 plan.EditRevision,
			"operation":                operation,
		}))
}
