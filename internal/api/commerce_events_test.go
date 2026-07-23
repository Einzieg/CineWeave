package api

import (
	"context"
	"testing"

	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/jackc/pgx/v5/pgconn"
)

type commerceEventExecer struct {
	eventNames []string
}

func (exec *commerceEventExecer) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	if len(args) > 2 {
		if eventName, ok := args[2].(string); ok {
			exec.eventNames = append(exec.eventNames, eventName)
		}
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func TestCommerceEventHelpersMatchCatalogContracts(t *testing.T) {
	ctx := context.Background()
	exec := &commerceEventExecer{}
	const organizationID = "organization-1"
	const projectID = "project-1"
	const unitID = "script-unit-1"
	const generationID = "unit-generation-1"

	if err := appendCommerceProductMutationEvents(ctx, exec, organizationID, projectID, commercepkg.ProductMutationResult{
		Product:   commercepkg.Product{ID: "product-1", Revision: 2},
		Version:   commercepkg.ProductVersion{ID: "product-version-1", ProductID: "product-1", Version: 2},
		Activated: true,
	}); err != nil {
		t.Fatalf("append product events: %v", err)
	}
	if err := appendCommerceProductReferenceEvent(ctx, exec, organizationID, projectID,
		"commerce.product.reference.added", commercepkg.ProductReference{
			ID: "reference-1", ProductID: "product-1", Status: "active", Revision: 1,
		}); err != nil {
		t.Fatalf("append product reference event: %v", err)
	}
	if err := appendCommerceScriptMutationEvents(ctx, exec, organizationID, projectID, commercepkg.ScriptVersionMutation{
		ScriptUnit: commercepkg.ScriptUnit{
			ID: unitID, Status: "ready", Revision: 2, ActiveUnitGenerationID: stringPointer(generationID),
		},
		Version:   commercepkg.ScriptVersion{ID: "script-version-1", ScriptUnitID: unitID, Version: 1},
		Activated: true,
	}, "commerce.script_unit.created"); err != nil {
		t.Fatalf("append script events: %v", err)
	}
	if err := appendCommerceLanguageEvent(ctx, exec, organizationID, projectID, commercepkg.LanguageResolution{
		ID: "resolution-1", ScriptUnitID: unitID, SourceScriptVersionID: "script-version-1",
		Status: "needs_confirmation", NeedsUserConfirmation: true,
	}); err != nil {
		t.Fatalf("append language event: %v", err)
	}
	if err := appendCommerceLocalizationEvent(ctx, exec, organizationID, projectID,
		"commerce.script.localization.approved", commercepkg.ScriptLocalization{
			ID: "localization-1", ScriptUnitID: unitID, SourceScriptVersionID: "script-version-1",
			TargetLanguage: "en-US", Status: "approved",
		}); err != nil {
		t.Fatalf("append localization event: %v", err)
	}
	plan := commercepkg.StoryboardPlan{
		ID: "plan-1", ScriptUnitID: unitID, UnitGenerationID: generationID,
		EditRevision: 3, Status: "ready",
	}
	if err := appendCommerceStoryboardPlanEvent(ctx, exec, organizationID, projectID,
		"commerce.storyboard.plan.activated", plan); err != nil {
		t.Fatalf("append storyboard plan event: %v", err)
	}
	if err := appendCommerceShotEvent(ctx, exec, organizationID, projectID, plan, "shot-1", "updated"); err != nil {
		t.Fatalf("append shot event: %v", err)
	}

	if len(exec.eventNames) != 11 {
		t.Fatalf("event count = %d, want 11: %#v", len(exec.eventNames), exec.eventNames)
	}
}

func stringPointer(value string) *string {
	return &value
}
