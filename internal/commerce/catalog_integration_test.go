package commerce

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCommerceCatalogLifecycle(t *testing.T) {
	if os.Getenv("CINEWEAVE_COMMERCE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_COMMERCE_INTEGRATION_TEST=1 to run commerce catalog integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for commerce catalog integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(context.Background())

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	var organizationID, userID, workspaceID, projectID string
	if err := tx.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ('Catalog', $1) RETURNING id::text`, "catalog-"+suffix).Scan(&organizationID); err != nil {
		t.Fatalf("organization: %v", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO users(email, display_name) VALUES ($1, 'Catalog') RETURNING id::text`, "catalog-"+suffix+"@example.test").Scan(&userID); err != nil {
		t.Fatalf("user: %v", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO workspaces(organization_id, name) VALUES ($1, 'Catalog') RETURNING id::text`, organizationID).Scan(&workspaceID); err != nil {
		t.Fatalf("workspace: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO projects(
			organization_id, workspace_id, name, project_kind, project_type, content_type,
			video_ratio, settings, created_by
		) VALUES ($1, $2, 'Catalog', 'commerce_video', 'commerce_video', NULL, '9:16', '{}', $3)
		RETURNING id::text
	`, organizationID, workspaceID, userID).Scan(&projectID); err != nil {
		t.Fatalf("project: %v", err)
	}

	catalog := NewCatalogService(NewRepository())
	zero := int64(0)
	productResult, err := catalog.CreateProductVersion(ctx, tx, organizationID, projectID, userID, &zero, ProductVersionInput{
		Name: "防晒喷雾", Brand: "CineWeave", SellingPoints: json.RawMessage(`["清爽"]`),
		ImmutableFeatures: json.RawMessage(`{"package":"white"}`), ProhibitedClaims: json.RawMessage(`[]`),
	})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	if !productResult.Activated || productResult.Product.CurrentVersionID == nil {
		t.Fatal("initial product version was not activated")
	}
	firstReference, err := catalog.CreateProductReference(ctx, tx, CreateProductReferenceParams{
		OrganizationID: organizationID, ProjectID: projectID, ProductID: productResult.Product.ID,
		StorageKey: "catalog/first.png", MimeType: "image/png", ContentHash: strings.Repeat("a", 64),
		ByteSize: 128, Width: 100, Height: 100, ReferenceRole: "front", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("create first reference: %v", err)
	}
	secondReference, err := catalog.CreateProductReference(ctx, tx, CreateProductReferenceParams{
		OrganizationID: organizationID, ProjectID: projectID, ProductID: productResult.Product.ID,
		StorageKey: "catalog/second.png", MimeType: "image/png", ContentHash: strings.Repeat("b", 64),
		ByteSize: 128, Width: 100, Height: 100, ReferenceRole: "detail", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("create second reference: %v", err)
	}
	targetOrdinal := firstReference.Ordinal
	setPrimary := true
	secondReference, err = catalog.UpdateProductReference(ctx, tx, organizationID, projectID, secondReference.ID,
		secondReference.Revision, "detail", &targetOrdinal, &setPrimary)
	if err != nil {
		t.Fatalf("swap references: %v", err)
	}
	if !secondReference.IsPrimary || secondReference.Ordinal != firstReference.Ordinal {
		t.Fatalf("updated reference = %+v", secondReference)
	}

	product, err := catalog.GetProduct(ctx, tx, organizationID, projectID)
	if err != nil {
		t.Fatalf("reload product: %v", err)
	}
	firstUnit, err := catalog.CreateScriptUnit(ctx, tx, organizationID, projectID, userID, product.ScriptUnitsRevision, CreateScriptUnitInput{
		Title: "痛点版", Content: "出门怕晒黑？\n这款防晒喷雾清爽易用。", LanguageMode: "explicit",
		ExplicitTargetLanguage: stringPointer("zh-CN"), TargetDurationSeconds: 30, TargetPlatform: "douyin",
	})
	if err != nil {
		t.Fatalf("create first unit: %v", err)
	}
	product, _ = catalog.GetProduct(ctx, tx, organizationID, projectID)
	secondUnit, err := catalog.CreateScriptUnit(ctx, tx, organizationID, projectID, userID, product.ScriptUnitsRevision, CreateScriptUnitInput{
		Title: "演示版", Content: "轻轻一喷。\n快速出门。", LanguageMode: "auto", TargetDurationSeconds: 15, TargetPlatform: "douyin",
	})
	if err != nil {
		t.Fatalf("create second unit: %v", err)
	}
	if firstUnit.ScriptUnit.UnitNo != 1 || secondUnit.ScriptUnit.UnitNo != 2 {
		t.Fatalf("unit numbers = %d/%d", firstUnit.ScriptUnit.UnitNo, secondUnit.ScriptUnit.UnitNo)
	}
	product, _ = catalog.GetProduct(ctx, tx, organizationID, projectID)
	if _, err := catalog.ReorderScriptUnits(ctx, tx, organizationID, projectID, product.ScriptUnitsRevision, []ReorderScriptUnitItem{
		{ScriptUnitID: secondUnit.ScriptUnit.ID, SortOrder: 10},
		{ScriptUnitID: firstUnit.ScriptUnit.ID, SortOrder: 20},
	}); err != nil {
		t.Fatalf("reorder units: %v", err)
	}
	updated, err := catalog.UpdateScriptUnit(ctx, tx, organizationID, projectID, firstUnit.ScriptUnit.ID, firstUnit.ScriptUnit.Revision+1,
		UpdateScriptUnitInput{DraftContent: stringPointer("出门怕晒黑？\n清爽防晒，轻轻一喷。")})
	if err != nil {
		t.Fatalf("update draft: %v", err)
	}
	versionMutation, err := catalog.CreateScriptVersion(ctx, tx, organizationID, projectID, updated.ID, userID, updated.Revision,
		updated.DraftContent, stringPointer("zh-CN"), true)
	if err != nil {
		t.Fatalf("create script version: %v", err)
	}
	resolution, err := catalog.ResolveLanguage(ctx, tx, organizationID, projectID, updated.ID, userID)
	if err != nil {
		t.Fatalf("resolve language: %v", err)
	}
	if resolution.Status != "confirmed" {
		t.Fatalf("explicit resolution status = %s", resolution.Status)
	}
	localization, timing, err := catalog.CreateLocalization(ctx, tx, organizationID, projectID, updated.ID, userID, LocalizationInput{
		SourceScriptVersionID: versionMutation.Version.ID, LanguageResolutionID: resolution.ID,
		SourceLanguage: "zh-CN", TargetLanguage: "zh-CN", LocalizedContent: updated.DraftContent,
		StructuredContract: json.RawMessage(`{}`), ReviewerOutput: json.RawMessage(`{"manual":true}`), Approve: true,
	})
	if err != nil {
		t.Fatalf("create localization: %v", err)
	}
	if localization.Status != "approved" || timing.Exceeded {
		t.Fatalf("localization=%s timing=%+v", localization.Status, timing)
	}
}
