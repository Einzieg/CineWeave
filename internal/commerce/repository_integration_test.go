package commerce

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCreateDraftAndActivateInitialBindings(t *testing.T) {
	if os.Getenv("CINEWEAVE_COMMERCE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_COMMERCE_INTEGRATION_TEST=1 to run commerce repository integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for commerce repository integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer tx.Rollback(context.Background())

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	var organizationID, userID, workspaceID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO organizations(name, slug)
		VALUES ('Commerce Integration', $1)
		RETURNING id::text
	`, "commerce-integration-"+suffix).Scan(&organizationID); err != nil {
		t.Fatalf("insert organization: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO users(email, display_name)
		VALUES ($1, 'Commerce Integration')
		RETURNING id::text
	`, "commerce-integration-"+suffix+"@example.test").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO workspaces(organization_id, name)
		VALUES ($1, 'Commerce Integration')
		RETURNING id::text
	`, organizationID).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO roles(role_key, name, scope, is_system)
		VALUES ('project_owner', 'Project Owner', 'project', true)
		ON CONFLICT DO NOTHING
	`); err != nil {
		t.Fatalf("insert project owner role: %v", err)
	}

	var profileVersionID string
	if err := tx.QueryRow(ctx, `
		SELECT version.id::text
		FROM video_production_profile_versions version
		JOIN video_production_profiles profile ON profile.id = version.profile_id
		WHERE profile.profile_key = $1
		  AND version.lifecycle_state = 'published'
		  AND version.implementation_state = 'available'
		ORDER BY version.version DESC
		LIMIT 1
	`, videoproduction.ProfileSingleFrameI2V).Scan(&profileVersionID); err != nil {
		t.Fatalf("resolve video profile fixture: %v", err)
	}

	var templateID, templateVersionID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_workflow_templates(
			organization_id, template_key, name, status, created_by
		)
		VALUES ($1, $2, 'Commerce Integration', 'active', $3)
		RETURNING id::text
	`, organizationID, DefaultWorkflowTemplateKey, userID).Scan(&templateID); err != nil {
		t.Fatalf("insert workflow template: %v", err)
	}
	contentHash := strings.Repeat("a", 64)
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_workflow_template_versions(
			template_id, version, configuration_snapshot, prompt_bindings,
			agent_model_contracts, language_contract, image_capability_contract,
			video_capability_contract, video_production_profile_version_id,
			content_hash, status, created_by, published_at
		)
		VALUES ($1, 1, '{}', '{}', '{}', '{}', '{}', '{}', $2, $3, 'published', $4, now())
		RETURNING id::text
	`, templateID, profileVersionID, contentHash, userID).Scan(&templateVersionID); err != nil {
		t.Fatalf("insert workflow template version: %v", err)
	}

	repository := NewRepository()
	service := NewService(repository)
	draft, err := service.CreateDraftProject(ctx, tx, DraftProjectParams{
		OrganizationID:   organizationID,
		WorkspaceID:      workspaceID,
		Name:             "Integration Product",
		VideoRatio:       "9:16",
		AspectRatio:      stringPointer("9:16"),
		AudioStrategy:    "native_av",
		AudioRequirement: "preferred",
		ImageQuality:     "standard",
		TimelineTimebase: 90000,
		FPSNumerator:     24,
		FPSDenominator:   1,
		Settings:         json.RawMessage(`{"defaultLanguageMode":"auto"}`),
		CreatedBy:        userID,
		IdempotencyScope: "commerce_project_create",
		ClientRequestID:  "integration-" + suffix,
		RequestHash:      strings.Repeat("b", 64),
		InputSnapshot:    json.RawMessage(`{"projectKind":"commerce_video"}`),
		SetupExpiresAt:   time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create draft project: %v", err)
	}
	if draft.WorkflowTemplateVersionID != templateVersionID || draft.SetupConfigurationHash != contentHash {
		t.Fatalf("draft = %+v", draft)
	}

	uploadCatalog := NewCatalogService(repository)
	zeroRevision := int64(0)
	productMutation, err := uploadCatalog.CreateProductVersion(ctx, tx, organizationID, draft.ProjectID, userID, &zeroRevision, ProductVersionInput{
		Name:              "Rejected upload fixture",
		SellingPoints:     json.RawMessage(`[]`),
		ImmutableFeatures: json.RawMessage(`{}`),
		ProhibitedClaims:  json.RawMessage(`[]`),
	})
	if err != nil {
		t.Fatalf("create upload fixture product: %v", err)
	}
	storageKey := "commerce/rejected-" + suffix + ".png"
	upload, uploadReplay, err := uploadCatalog.ClaimProductReferenceUpload(
		ctx, tx, organizationID, draft.ProjectID, productMutation.Product.ID,
		stringPointer(draft.SetupSessionID), storageKey, "image/png", "rejected.png",
		"rejected-upload-"+suffix, userID, time.Now().Add(time.Hour),
	)
	if err != nil || uploadReplay {
		t.Fatalf("claim upload = %+v replay=%v error=%v", upload, uploadReplay, err)
	}
	if _, err := uploadCatalog.TrackSetupUpload(ctx, tx, organizationID, draft.ProjectID, draft.SetupSessionID, storageKey); err != nil {
		t.Fatalf("track rejected upload: %v", err)
	}
	upload, err = uploadCatalog.AbandonProductReferenceUpload(ctx, tx, upload)
	if err != nil {
		t.Fatalf("abandon rejected upload: %v", err)
	}
	if upload.Status != "abandoned" || upload.AbandonedAt == nil {
		t.Fatalf("abandoned upload = %+v", upload)
	}
	setupAfterRelease, err := uploadCatalog.CompleteSetupUpload(ctx, tx, organizationID, draft.ProjectID, draft.SetupSessionID, storageKey)
	if err != nil {
		t.Fatalf("release rejected setup upload: %v", err)
	}
	if setupContainsUpload(setupAfterRelease.InputSnapshot, storageKey) {
		t.Fatalf("rejected upload remains in setup snapshot: %s", setupAfterRelease.InputSnapshot)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM commerce_products WHERE id = $1`, productMutation.Product.ID); err != nil {
		t.Fatalf("remove rejected upload fixture product: %v", err)
	}

	var memberCount, ownerBindingCount, setupCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_members WHERE project_id = $1 AND user_id = $2 AND status = 'active'`, draft.ProjectID, userID).Scan(&memberCount); err != nil {
		t.Fatalf("count project member: %v", err)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM role_bindings WHERE resource_project_id = $1 AND subject_user_id = $2`, draft.ProjectID, userID).Scan(&ownerBindingCount); err != nil {
		t.Fatalf("count owner binding: %v", err)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM commerce_setup_sessions WHERE id = $1 AND project_id = $2 AND state = 'uploading'`, draft.SetupSessionID, draft.ProjectID).Scan(&setupCount); err != nil {
		t.Fatalf("count setup session: %v", err)
	}
	if memberCount != 1 || ownerBindingCount != 1 || setupCount != 1 {
		t.Fatalf("draft side effects member=%d owner=%d uploading-setup=%d", memberCount, ownerBindingCount, setupCount)
	}

	bindings, err := service.PrepareInitialBindings(ctx, tx, InitialBindingParams{
		OrganizationID:          organizationID,
		ProjectID:               draft.ProjectID,
		WorkflowTemplateVersion: templateVersionID,
		CreatedBy:               userID,
		CompatibilityPolicy:     videoproduction.CompatibilityStrict,
		VideoOverrides:          json.RawMessage(`{}`),
		ProductionConfiguration: videoproduction.ProductionConfigurationSnapshot{
			SchemaVersion:         videoproduction.ProductionConfigurationSnapshotVersion,
			ProjectType:           ProjectTypeCommerce,
			AspectRatio:           "9:16",
			VideoRatio:            "9:16",
			ImageModelProfileKey:  "image_generation_default",
			VideoModelProfileKey:  "video_generation_default",
			ScriptModelProfileKey: "script_agent_default",
			TTSModelProfileKey:    "tts_generation_default",
			ASRModelProfileKey:    "audio_transcription_default",
			AudioStrategy:         "native_av",
			AudioRequirement:      "preferred",
			ImageQuality:          "standard",
			TimelineTimebase:      90000,
			FPSNumerator:          24,
			FPSDenominator:        1,
			Settings:              json.RawMessage(`{}`),
			ManualBindings:        map[string]videoproduction.ManualBindingSnapshot{},
		},
		ConfigurationSnapshot: json.RawMessage(`{"defaultLanguageMode":"auto","videoRatio":"9:16"}`),
		ModelRoutingSnapshot:  json.RawMessage(`{"video":"video_generation_default"}`),
		CapabilitySnapshot:    json.RawMessage(`{"approved":true}`),
	})
	if err != nil {
		t.Fatalf("prepare initial bindings: %v", err)
	}
	if err := service.ActivateInitialBindings(ctx, tx, draft.ProjectID, bindings); err != nil {
		t.Fatalf("activate initial bindings: %v", err)
	}

	var projectGenerationID, videoBindingID, commerceBindingID string
	var generationNo, videoRevision, commerceRevision int64
	if err := tx.QueryRow(ctx, `
		SELECT project.active_video_production_generation_id::text,
		       generation.binding_id::text, generation.commerce_workflow_binding_id::text,
		       generation.generation_no, video.revision, commerce.binding_revision
		FROM projects project
		JOIN project_video_production_generations generation
		  ON generation.id = project.active_video_production_generation_id
		JOIN project_video_production_bindings video ON video.id = generation.binding_id
		JOIN project_commerce_workflow_bindings commerce
		  ON commerce.id = generation.commerce_workflow_binding_id
		WHERE project.id = $1
		  AND generation.status = 'active'
		  AND video.status = 'active'
		  AND commerce.status = 'active'
	`, draft.ProjectID).Scan(
		&projectGenerationID,
		&videoBindingID,
		&commerceBindingID,
		&generationNo,
		&videoRevision,
		&commerceRevision,
	); err != nil {
		t.Fatalf("load activated identities: %v", err)
	}
	if projectGenerationID != bindings.ProjectGenerationID || videoBindingID != bindings.VideoBindingID || commerceBindingID != bindings.CommerceBindingID {
		t.Fatalf("activated identities do not match result: %+v", bindings)
	}
	if generationNo != bindings.ProjectGenerationNo || videoRevision != bindings.VideoBindingRevision || commerceRevision != bindings.CommerceBindingRevision || videoRevision != commerceRevision {
		t.Fatalf("activated revisions generation=%d video=%d commerce=%d result=%+v", generationNo, videoRevision, commerceRevision, bindings)
	}

	production, err := repository.LoadActiveProductionContext(ctx, tx, organizationID, draft.ProjectID)
	if err != nil {
		t.Fatalf("load active production context: %v", err)
	}
	executionIdentity := production.ExecutionIdentity()
	if executionIdentity.ProjectGenerationID != bindings.ProjectGenerationID ||
		executionIdentity.VideoProductionBindingID != bindings.VideoBindingID ||
		executionIdentity.CommerceWorkflowBindingID != bindings.CommerceBindingID {
		t.Fatalf("production context = %+v, bindings = %+v", production, bindings)
	}
	if _, err := service.AssertWritableExecution(ctx, tx, executionIdentity); err != nil {
		t.Fatalf("assert writable execution: %v", err)
	}

	unitIdentity := insertCommerceUnitGenerationFixture(t, ctx, tx, production, userID)
	unit, err := service.AssertWritableUnitGeneration(ctx, tx, unitIdentity)
	if err != nil {
		t.Fatalf("assert writable unit generation: %v", err)
	}
	if unit.Identity != unitIdentity || unit.Status != "active" {
		t.Fatalf("unit generation = %+v, want identity %+v", unit, unitIdentity)
	}
	staleIdentity := unitIdentity
	staleIdentity.UnitConfigurationHash = strings.Repeat("f", 64)
	if _, err := service.AssertWritableUnitGeneration(ctx, tx, staleIdentity); err == nil {
		t.Fatal("stale unit configuration hash was accepted")
	} else if typed, ok := AsError(err); !ok || typed.Code != CodeGenerationMismatch {
		t.Fatalf("stale unit error = %v, want %s", err, CodeGenerationMismatch)
	}
	staleRevisionIdentity := unitIdentity
	staleRevisionIdentity.ScriptUnitRevision++
	if _, err := service.AssertWritableUnitGeneration(ctx, tx, staleRevisionIdentity); err == nil {
		t.Fatal("stale script unit revision was accepted")
	} else if typed, ok := AsError(err); !ok || typed.Code != CodeRevisionConflict {
		t.Fatalf("stale revision error = %v, want %s", err, CodeRevisionConflict)
	}

	catalog := NewCatalogService(repository)
	currentScriptUnit, err := catalog.GetScriptUnit(ctx, tx, organizationID, draft.ProjectID, unitIdentity.ScriptUnitID)
	if err != nil {
		t.Fatalf("load commerce script unit for rebuild: %v", err)
	}
	targetScript, err := catalog.CreateScriptVersion(
		ctx, tx, organizationID, draft.ProjectID, currentScriptUnit.ID, userID,
		currentScriptUnit.Revision, "更新后的真实商品脚本", stringPointer("zh-CN"), true,
	)
	if err != nil {
		t.Fatalf("create target script version for rebuild: %v", err)
	}
	if targetScript.Activated || !targetScript.RequiresRebuild {
		t.Fatalf("target script mutation = %+v, want rebuild without activation", targetScript)
	}
	updatedTitle := currentScriptUnit.Title + "（标题已编辑）"
	currentScriptUnit, err = catalog.UpdateScriptUnit(
		ctx, tx, organizationID, draft.ProjectID, currentScriptUnit.ID,
		currentScriptUnit.Revision, UpdateScriptUnitInput{Title: &updatedTitle},
	)
	if err != nil {
		t.Fatalf("update commerce script unit title: %v", err)
	}
	if currentScriptUnit.Revision <= unitIdentity.ScriptUnitRevision {
		t.Fatalf("script unit revision = %d, want newer than frozen generation revision %d",
			currentScriptUnit.Revision, unitIdentity.ScriptUnitRevision)
	}
	frozenGeneration, err := service.AssertWritableUnitGeneration(ctx, tx, unitIdentity)
	if err != nil {
		t.Fatalf("draft script version changed active generation identity: %v", err)
	}
	if frozenGeneration.Identity.ScriptUnitRevision != unitIdentity.ScriptUnitRevision {
		t.Fatalf("frozen generation revision = %d, want %d",
			frozenGeneration.Identity.ScriptUnitRevision, unitIdentity.ScriptUnitRevision)
	}
	storyboardIdentity, err := catalog.LockActiveStoryboardGeneration(
		ctx, tx, organizationID, draft.ProjectID, unitIdentity.ScriptUnitID,
	)
	if err != nil {
		t.Fatalf("lock storyboard generation after draft version: %v", err)
	}
	if storyboardIdentity != unitIdentity {
		t.Fatalf("storyboard generation identity = %+v, want frozen identity %+v",
			storyboardIdentity, unitIdentity)
	}
	scriptImpact, err := catalog.PlanScriptUnitRebuild(ctx, tx, organizationID, draft.ProjectID,
		currentScriptUnit.ID, ScriptUnitRebuildTarget{
			ExpectedRevision:            currentScriptUnit.Revision,
			TargetSourceScriptVersionID: targetScript.Version.ID,
			TargetLanguageMode:          "auto",
			TargetDurationSeconds:       30,
			TargetPlatform:              "douyin",
		}, userID)
	if err != nil {
		t.Fatalf("plan script unit rebuild: %v", err)
	}
	if scriptImpact.SourceUnitGenerationID != unitIdentity.UnitGenerationID ||
		scriptImpact.TargetSourceScriptVersionID != targetScript.Version.ID ||
		scriptImpact.TargetConfigurationHash == "" || scriptImpact.ImpactToken == "" ||
		len(scriptImpact.Blockers) != 0 {
		t.Fatalf("script unit rebuild impact = %+v", scriptImpact)
	}
	scriptRebuildKey := "script-rebuild-" + suffix
	scriptExecution, err := catalog.ApproveScriptUnitRebuild(
		ctx, tx, organizationID, draft.ProjectID, currentScriptUnit.ID,
		scriptImpact.ImpactToken, currentScriptUnit.Revision, scriptRebuildKey,
	)
	if err != nil {
		t.Fatalf("approve script unit rebuild: %v", err)
	}
	if scriptExecution.IdempotentReplay || scriptExecution.Status != "running" ||
		scriptExecution.PreparationIdentity.SourceScriptVersionID != targetScript.Version.ID ||
		scriptExecution.PreparationIdentity.SourceUnitGenerationID != unitIdentity.UnitGenerationID {
		t.Fatalf("script unit rebuild execution = %+v", scriptExecution)
	}
	if _, err := service.AssertWritableUnitGeneration(ctx, tx, unitIdentity); err == nil {
		t.Fatal("active script rebuild did not block new writes to the source unit generation")
	} else if typed, ok := AsError(err); !ok || typed.Code != CodeScriptRebuildBlocked {
		t.Fatalf("active script rebuild write error = %v, want %s", err, CodeScriptRebuildBlocked)
	}
	var scriptRebuildWorkflowRunID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO workflow_runs(
			organization_id, project_id, temporal_workflow_id, workflow_type, status,
			input, created_by, production_generation_id,
			video_production_binding_id, video_production_binding_revision,
			started_at
		)
		VALUES ($1, $2, $3, 'commerce_script_unit_preparation', 'running', $4, $5, $6, $7, $8, now())
		RETURNING id::text
	`, organizationID, draft.ProjectID, "commerce-script-rebuild-"+suffix,
		json.RawMessage(`{"identity":{"scriptUnitGenerationId":"`+unitIdentity.UnitGenerationID+`"}}`),
		userID, production.Generation.ID, production.VideoBinding.ID, production.VideoBinding.Revision,
	).Scan(&scriptRebuildWorkflowRunID); err != nil {
		t.Fatalf("insert script rebuild workflow run: %v", err)
	}
	if err := catalog.AttachScriptUnitRebuildWorkflow(ctx, tx, scriptExecution.RebuildID, scriptRebuildWorkflowRunID); err != nil {
		t.Fatalf("attach script rebuild workflow: %v", err)
	}
	scriptReplay, err := catalog.ApproveScriptUnitRebuild(
		ctx, tx, organizationID, draft.ProjectID, currentScriptUnit.ID,
		scriptImpact.ImpactToken, currentScriptUnit.Revision, scriptRebuildKey,
	)
	if err != nil {
		t.Fatalf("replay script unit rebuild: %v", err)
	}
	if !scriptReplay.IdempotentReplay || scriptReplay.RebuildID != scriptExecution.RebuildID ||
		scriptReplay.WorkflowRunID != scriptRebuildWorkflowRunID {
		t.Fatalf("script unit rebuild replay = %+v", scriptReplay)
	}
	var activeScriptVersionID, activeUnitGenerationID, activeUnitGenerationStatus, scriptRebuildStatus string
	if err := tx.QueryRow(ctx, `
		SELECT unit.current_source_version_id::text, unit.active_unit_generation_id::text,
		       generation.status, rebuild.status
		FROM commerce_script_units unit
		JOIN commerce_script_unit_generations generation ON generation.id = unit.active_unit_generation_id
		JOIN commerce_script_unit_rebuilds rebuild ON rebuild.id = $2
		WHERE unit.id = $1
	`, currentScriptUnit.ID, scriptExecution.RebuildID).Scan(
		&activeScriptVersionID, &activeUnitGenerationID, &activeUnitGenerationStatus, &scriptRebuildStatus,
	); err != nil {
		t.Fatalf("load pre-switch script rebuild state: %v", err)
	}
	if activeScriptVersionID != unit.SourceScriptVersionID || activeUnitGenerationID != unitIdentity.UnitGenerationID ||
		activeUnitGenerationStatus != "active" || scriptRebuildStatus != "running" {
		t.Fatalf("pre-switch script state source=%s generation=%s/%s rebuild=%s",
			activeScriptVersionID, activeUnitGenerationID, activeUnitGenerationStatus, scriptRebuildStatus)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_unit_rebuilds
		SET status = 'cancelled', completed_at = now(),
		    error_code = 'TEST_CLEANUP', error_message = 'integration test cleanup'
		WHERE id = $1 AND status = 'running'
	`, scriptExecution.RebuildID); err != nil {
		t.Fatalf("cancel script rebuild fixture: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'cancelled', completed_at = now(), updated_at = now(), revision = revision + 1
		WHERE id = $1 AND status = 'running'
	`, scriptRebuildWorkflowRunID); err != nil {
		t.Fatalf("cancel script rebuild workflow fixture: %v", err)
	}
	if _, err := service.AssertWritableUnitGeneration(ctx, tx, unitIdentity); err != nil {
		t.Fatalf("source unit generation did not become writable after rebuild cancellation: %v", err)
	}

	product, err := catalog.GetProduct(ctx, tx, organizationID, draft.ProjectID)
	if err != nil {
		t.Fatalf("load commerce product for product rebuild: %v", err)
	}
	primaryReference, err := catalog.CreateProductReference(ctx, tx, CreateProductReferenceParams{
		OrganizationID: organizationID, ProjectID: draft.ProjectID, ProductID: product.ID,
		StorageKey: "commerce-integration/primary.png", MimeType: "image/png",
		ContentHash: strings.Repeat("e", 64), ByteSize: 256, Width: 1080, Height: 1920,
		ReferenceRole: "primary", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("create product rebuild primary reference: %v", err)
	}
	product, err = catalog.GetProduct(ctx, tx, organizationID, draft.ProjectID)
	if err != nil {
		t.Fatalf("reload commerce product after reference upload: %v", err)
	}
	targetProduct, err := catalog.CreateProductVersion(ctx, tx, organizationID, draft.ProjectID, userID, &product.Revision, ProductVersionInput{
		Name: "Integration Product v2", Brand: "CineWeave",
		SellingPoints: json.RawMessage(`["new selling point"]`), ImmutableFeatures: json.RawMessage(`{"package":"white"}`),
		ProhibitedClaims: json.RawMessage(`[]`),
	})
	if err != nil {
		t.Fatalf("create target product version: %v", err)
	}
	if targetProduct.Activated || !targetProduct.RequiresRebuild {
		t.Fatalf("target product version mutation = %+v", targetProduct)
	}
	impact, err := catalog.PlanProductRebuild(ctx, tx, organizationID, draft.ProjectID,
		targetProduct.Version.ID, []string{primaryReference.ID}, product.Revision, userID)
	if err != nil {
		t.Fatalf("plan product rebuild: %v", err)
	}
	if len(impact.AffectedUnits) != 1 || impact.AffectedUnits[0].SourceUnitGenerationID != unitIdentity.UnitGenerationID {
		t.Fatalf("product rebuild impact = %+v", impact)
	}
	productRebuild, err := catalog.ExecuteProductRebuild(ctx, tx, organizationID, draft.ProjectID,
		impact.ImpactToken, product.Revision, "product-rebuild-"+suffix, userID)
	if err != nil {
		t.Fatalf("execute product rebuild: %v", err)
	}
	if productRebuild.Status != "succeeded" || productRebuild.AffectedUnitCount != 1 || productRebuild.ReferencePackID == "" {
		t.Fatalf("product rebuild result = %+v", productRebuild)
	}
	replay, err := catalog.ExecuteProductRebuild(ctx, tx, organizationID, draft.ProjectID,
		impact.ImpactToken, product.Revision, "product-rebuild-"+suffix, userID)
	if err != nil {
		t.Fatalf("replay product rebuild: %v", err)
	}
	if !replay.IdempotentReplay || replay.RebuildID != productRebuild.RebuildID {
		t.Fatalf("product rebuild replay = %+v", replay)
	}
	var rebuiltGenerationID, rebuiltGenerationStatus, sourceProductGenerationStatus, activeProductVersionID string
	var rebuiltGenerationNo, rebuiltScriptUnitRevision, stableUnitNo int64
	if err := tx.QueryRow(ctx, `
		SELECT unit.active_unit_generation_id::text, unit.unit_generation_no, unit.unit_no,
		       target.script_unit_revision, target.status, source.status,
		       product.current_version_id::text
		FROM commerce_script_units unit
		JOIN commerce_script_unit_generations target ON target.id = unit.active_unit_generation_id
		JOIN commerce_script_unit_generations source ON source.id = $2
		JOIN commerce_products product ON product.id = unit.product_id
		WHERE unit.id = $1
	`, unitIdentity.ScriptUnitID, unitIdentity.UnitGenerationID).Scan(
		&rebuiltGenerationID, &rebuiltGenerationNo, &stableUnitNo,
		&rebuiltScriptUnitRevision,
		&rebuiltGenerationStatus, &sourceProductGenerationStatus, &activeProductVersionID,
	); err != nil {
		t.Fatalf("load product rebuild state: %v", err)
	}
	if rebuiltGenerationID == unitIdentity.UnitGenerationID || rebuiltGenerationNo != 2 || stableUnitNo != 1 ||
		rebuiltGenerationStatus != "active" || sourceProductGenerationStatus != "archived" ||
		activeProductVersionID != targetProduct.Version.ID {
		t.Fatalf("product rebuild state target=%s no=%d unitNo=%d targetStatus=%s sourceStatus=%s productVersion=%s",
			rebuiltGenerationID, rebuiltGenerationNo, stableUnitNo, rebuiltGenerationStatus,
			sourceProductGenerationStatus, activeProductVersionID)
	}
	unitIdentity.UnitGenerationID = rebuiltGenerationID
	unitIdentity.UnitGenerationNo = rebuiltGenerationNo
	unitIdentity.ScriptUnitRevision = rebuiltScriptUnitRevision
	if err := tx.QueryRow(ctx, `SELECT unit_configuration_hash FROM commerce_script_unit_generations WHERE id = $1`, rebuiltGenerationID).Scan(&unitIdentity.UnitConfigurationHash); err != nil {
		t.Fatalf("load rebuilt unit configuration hash: %v", err)
	}
	unit, err = service.AssertWritableUnitGeneration(ctx, tx, unitIdentity)
	if err != nil {
		t.Fatalf("assert rebuilt unit generation: %v", err)
	}

	hashValue := func(value byte) string { return strings.Repeat(string(value), 64) }
	salesScriptContractID, salesScriptContractHash := insertCommerceSalesScriptContractFixture(
		t, ctx, tx, production, unit, userID, suffix,
	)
	var sourceStoryboardPlanID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_storyboard_plans(
			organization_id, project_id, product_id, product_version_id,
			script_unit_id, source_script_version_id, localization_id, reference_pack_id,
			project_production_generation_id, script_unit_generation_id,
			commerce_workflow_binding_id, commerce_workflow_binding_revision,
			sales_script_contract_id, sales_script_contract_hash,
			revision, status, active, stale_state, target_language,
			localized_content_hash, localized_contract_hash, timing_policy_version,
			target_duration_seconds, aspect_ratio, timeline_timebase,
			fps_numerator, fps_denominator, allowed_shot_durations,
			estimated_shot_count, actual_shot_count,
			review_status, plan_hash, projection_hash, created_by, activated_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
			1, 'ready', true, 'fresh', 'zh-CN', $15, $16, 'zh-default-v1',
			30, '9:16', 90000, 24, 1, ARRAY[5, 10], 1, 1, 'approved', $17, $18, $19, now()
		)
		RETURNING id::text
	`, production.OrganizationID, production.ProjectID, unit.Identity.ProductID,
		unit.ProductVersionID, unit.Identity.ScriptUnitID, unit.SourceScriptVersionID,
		unit.LocalizationID, unit.ReferencePackID, production.Generation.ID,
		unit.Identity.UnitGenerationID, production.CommerceBinding.ID,
		production.CommerceBinding.Revision, salesScriptContractID, salesScriptContractHash,
		hashValue('8'), hashValue('9'), hashValue('a'), hashValue('b'), userID).Scan(&sourceStoryboardPlanID); err != nil {
		t.Fatalf("insert source commerce storyboard plan: %v", err)
	}

	targetConfiguration := videoproduction.ProductionConfigurationSnapshot{
		SchemaVersion:         videoproduction.ProductionConfigurationSnapshotVersion,
		ProjectType:           ProjectTypeCommerce,
		AspectRatio:           "9:16",
		VideoRatio:            "9:16",
		ImageModelProfileKey:  "image_generation_default",
		VideoModelProfileKey:  "video_generation_default",
		ScriptModelProfileKey: "script_agent_default",
		TTSModelProfileKey:    "tts_generation_default",
		ASRModelProfileKey:    "audio_transcription_default",
		AudioStrategy:         "native_av",
		AudioRequirement:      "preferred",
		ImageQuality:          "high",
		TimelineTimebase:      90000,
		FPSNumerator:          24,
		FPSDenominator:        1,
		Settings:              json.RawMessage(`{"defaultLanguageMode":"auto"}`),
		ManualBindings:        map[string]videoproduction.ManualBindingSnapshot{},
	}
	targetConfigurationJSON, err := json.Marshal(targetConfiguration)
	if err != nil {
		t.Fatalf("marshal target configuration: %v", err)
	}
	targetConfigurationHash, err := hashJSON(targetConfigurationJSON)
	if err != nil {
		t.Fatalf("hash target configuration: %v", err)
	}
	rebuildID := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		INSERT INTO project_video_production_rebuilds(
			id, organization_id, project_id, source_binding_id, source_generation_id,
			source_video_production_state, source_commerce_workflow_binding_id,
			source_commerce_configuration_hash, target_profile_version_id,
			status, reason, target_configuration, target_configuration_hash,
			impact_snapshot, impact_token, expected_project_revision,
			idempotency_key, requested_by, approved_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9,
			'approved', 'configuration_change', $10, $11, '{}', $12, $13, $14, $15, now()
		)
	`, rebuildID, production.OrganizationID, production.ProjectID,
		production.VideoBinding.ID, production.Generation.ID, production.ProjectState,
		production.CommerceBinding.ID, production.CommerceBinding.ConfigurationHash,
		production.VideoBinding.ProfileVersionID, targetConfigurationJSON,
		targetConfigurationHash, hashValue('b'), production.ProjectRevision,
		"commerce-rebuild-"+suffix, userID); err != nil {
		t.Fatalf("insert commerce project rebuild: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE projects
		SET video_production_locked = true,
		    video_production_state = 'rebuilding',
		    active_video_production_rebuild_id = $2
		WHERE id = $1 AND revision = $3
	`, production.ProjectID, rebuildID, production.ProjectRevision); err != nil {
		t.Fatalf("lock commerce project rebuild: %v", err)
	}

	prepared, err := service.PrepareProjectRebuild(ctx, tx, production.OrganizationID, production.ProjectID, rebuildID, InitialBindingParams{
		WorkflowTemplateVersion: templateVersionID,
		CreatedBy:               userID,
		CompatibilityPolicy:     videoproduction.CompatibilityStrict,
		VideoOverrides:          json.RawMessage(`{}`),
		ProductionConfiguration: targetConfiguration,
		ConfigurationSnapshot:   json.RawMessage(`{"defaultLanguageMode":"auto","videoRatio":"9:16","imageQuality":"high"}`),
		ModelRoutingSnapshot:    json.RawMessage(`{"video":"video_generation_default"}`),
		CapabilitySnapshot:      json.RawMessage(`{"approved":true}`),
	})
	if err != nil {
		t.Fatalf("prepare commerce project rebuild: %v", err)
	}
	if prepared.PreparedUnitCount != 1 || prepared.Target.ProjectGenerationID == production.Generation.ID {
		t.Fatalf("prepared rebuild = %+v", prepared)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE commerce_project_rebuild_items
		SET status = 'blocked', blockers = '[{"code":"TEST_BLOCKER"}]', completed_at = now()
		WHERE rebuild_id = $1
	`, rebuildID); err != nil {
		t.Fatalf("set rebuild blocker: %v", err)
	}
	if _, err := service.ActivatePreparedProjectRebuild(ctx, tx, production.OrganizationID, production.ProjectID, rebuildID); err == nil {
		t.Fatal("blocked project rebuild was activated")
	} else if typed, ok := AsError(err); !ok || typed.Code != CodeProjectRebuildBlocked {
		t.Fatalf("blocked activation error = %v, want %s", err, CodeProjectRebuildBlocked)
	}
	var sourceGenerationStatus, sourceUnitStatus, targetGenerationStatus string
	if err := tx.QueryRow(ctx, `
		SELECT source_generation.status, source_unit.status, target_generation.status
		FROM project_video_production_generations source_generation
		JOIN commerce_script_unit_generations source_unit ON source_unit.id = $2
		JOIN project_video_production_generations target_generation ON target_generation.id = $3
		WHERE source_generation.id = $1
	`, production.Generation.ID, unit.Identity.UnitGenerationID, prepared.Target.ProjectGenerationID).Scan(
		&sourceGenerationStatus, &sourceUnitStatus, &targetGenerationStatus,
	); err != nil {
		t.Fatalf("load blocked activation state: %v", err)
	}
	if sourceGenerationStatus != "active" || sourceUnitStatus != "active" || targetGenerationStatus != "preparing" {
		t.Fatalf("blocked activation changed state source=%s unit=%s target=%s", sourceGenerationStatus, sourceUnitStatus, targetGenerationStatus)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE commerce_project_rebuild_items
		SET status = 'ready', blockers = '[]', completed_at = NULL
		WHERE rebuild_id = $1
	`, rebuildID); err != nil {
		t.Fatalf("clear rebuild blocker: %v", err)
	}
	activated, err := service.ActivatePreparedProjectRebuild(ctx, tx, production.OrganizationID, production.ProjectID, rebuildID)
	if err != nil {
		t.Fatalf("activate commerce project rebuild: %v", err)
	}
	if activated.ProjectGeneration.ID != prepared.Target.ProjectGenerationID ||
		activated.VideoBinding.ID != prepared.Target.VideoBindingID ||
		activated.CommerceBinding.ID != prepared.Target.CommerceBindingID ||
		activated.SwitchedUnitCount != 1 || activated.ProjectRevision != production.ProjectRevision+1 {
		t.Fatalf("activated rebuild = %+v, prepared = %+v", activated, prepared)
	}
	var sourceVideoStatus, sourceCommerceStatus, targetUnitStatus, storyboardStatus, storyboardStaleState string
	var currentUnitGenerationID string
	var currentUnitRevision int64
	if err := tx.QueryRow(ctx, `
		SELECT source_video.status, source_commerce.status, target_unit.status,
		       unit.active_unit_generation_id::text, unit.revision,
		       storyboard.status, storyboard.stale_state
		FROM project_video_production_bindings source_video
		JOIN project_commerce_workflow_bindings source_commerce ON source_commerce.id = $2
		JOIN commerce_script_units unit ON unit.id = $3
		JOIN commerce_script_unit_generations target_unit ON target_unit.id = unit.active_unit_generation_id
		JOIN commerce_storyboard_plans storyboard ON storyboard.id = $4
		WHERE source_video.id = $1
	`, production.VideoBinding.ID, production.CommerceBinding.ID, unit.Identity.ScriptUnitID,
		sourceStoryboardPlanID).Scan(&sourceVideoStatus, &sourceCommerceStatus, &targetUnitStatus,
		&currentUnitGenerationID, &currentUnitRevision, &storyboardStatus, &storyboardStaleState); err != nil {
		t.Fatalf("load activated commerce rebuild state: %v", err)
	}
	if sourceVideoStatus != "superseded" || sourceCommerceStatus != "superseded" ||
		targetUnitStatus != "active" || currentUnitGenerationID == unit.Identity.UnitGenerationID ||
		currentUnitRevision != unit.Identity.ScriptUnitRevision+1 || storyboardStatus != "stale" ||
		storyboardStaleState != "upstream_changed" {
		t.Fatalf("activated states video=%s commerce=%s unit=%s current=%s revision=%d storyboard=%s/%s",
			sourceVideoStatus, sourceCommerceStatus, targetUnitStatus, currentUnitGenerationID,
			currentUnitRevision, storyboardStatus, storyboardStaleState)
	}

	var currentUnitGenerationNo int64
	var currentUnitConfigurationHash string
	if err := tx.QueryRow(ctx, `
		SELECT unit_generation_no, unit_configuration_hash
		FROM commerce_script_unit_generations
		WHERE id = $1
	`, currentUnitGenerationID).Scan(&currentUnitGenerationNo, &currentUnitConfigurationHash); err != nil {
		t.Fatalf("load current unit generation identity: %v", err)
	}
	runIdentity := UnitGenerationIdentity{
		ExecutionIdentity: ExecutionIdentity{
			OrganizationID:                  production.OrganizationID,
			ProjectID:                       production.ProjectID,
			ProjectGenerationID:             activated.ProjectGeneration.ID,
			VideoProductionBindingID:        activated.VideoBinding.ID,
			VideoProductionBindingRevision:  activated.VideoBinding.Revision,
			VideoProfileSnapshotHash:        activated.VideoBinding.ProfileSnapshotHash,
			CommerceWorkflowBindingID:       activated.CommerceBinding.ID,
			CommerceWorkflowBindingRevision: activated.CommerceBinding.Revision,
			CommerceConfigurationHash:       activated.CommerceBinding.ConfigurationHash,
		},
		ProductID:             unit.Identity.ProductID,
		ScriptUnitID:          unit.Identity.ScriptUnitID,
		ScriptUnitRevision:    currentUnitRevision,
		UnitGenerationID:      currentUnitGenerationID,
		UnitGenerationNo:      currentUnitGenerationNo,
		UnitConfigurationHash: currentUnitConfigurationHash,
	}
	runService := NewProductionRunService(repository)
	runParams := CreateProductionRunParams{
		Identity:         runIdentity,
		RunType:          RunTypeStoryboardPlan,
		IdempotencyScope: "commerce_storyboard_plan",
		IdempotencyKey:   "commerce-run-" + suffix,
		InputSnapshot:    json.RawMessage(`{"phase":"plan"}`),
		Subjects: []ProductionSubject{
			{Type: SubjectPlanPhase, Key: "analyze", InputHash: hashValue('c')},
			{Type: SubjectCandidateShot, Key: "candidate-001", InputHash: hashValue('d')},
		},
		CreatedBy: userID,
	}
	run, created, err := runService.CreateRun(ctx, tx, runParams)
	if err != nil {
		t.Fatalf("create commerce production run: %v", err)
	}
	if !created || run.TotalItems != 2 || run.Status != RunQueued {
		t.Fatalf("created run = %+v, created=%v", run, created)
	}
	idempotentRun, created, err := runService.CreateRun(ctx, tx, runParams)
	if err != nil {
		t.Fatalf("load idempotent commerce production run: %v", err)
	}
	if created || idempotentRun.ID != run.ID {
		t.Fatalf("idempotent run = %+v, created=%v, original=%s", idempotentRun, created, run.ID)
	}
	conflictingParams := runParams
	conflictingParams.InputSnapshot = json.RawMessage(`{"phase":"changed"}`)
	if _, _, err := runService.CreateRun(ctx, tx, conflictingParams); err == nil {
		t.Fatal("reused idempotency key with a different payload was accepted")
	} else if typed, ok := AsError(err); !ok || typed.Code != CodeIdempotencyKeyReused {
		t.Fatalf("idempotency conflict = %v, want %s", err, CodeIdempotencyKeyReused)
	}

	rows, err := tx.Query(ctx, `
		SELECT id::text, subject_key, input_hash
		FROM commerce_production_run_items
		WHERE run_id = $1
		ORDER BY subject_key
	`, run.ID)
	if err != nil {
		t.Fatalf("list commerce production items: %v", err)
	}
	type runItemFixture struct{ id, key, inputHash string }
	items := make([]runItemFixture, 0, 2)
	for rows.Next() {
		var item runItemFixture
		if err := rows.Scan(&item.id, &item.key, &item.inputHash); err != nil {
			rows.Close()
			t.Fatalf("scan commerce production item: %v", err)
		}
		items = append(items, item)
	}
	rows.Close()
	if len(items) != 2 {
		t.Fatalf("production item count = %d, want 2", len(items))
	}
	productionProviderCallID := insertCommerceProductionProviderCallFixture(
		t, ctx, tx, production.OrganizationID, production.ProjectID, activated.ProjectGeneration.ID,
	)
	firstAttempt, err := runService.StartAttempt(ctx, tx, StartProductionAttemptParams{
		OrganizationID: production.OrganizationID,
		ProjectID:      production.ProjectID,
		RunID:          run.ID,
		ItemID:         items[0].id,
		InputHash:      items[0].inputHash,
	})
	if err != nil {
		t.Fatalf("start first commerce production attempt: %v", err)
	}
	run, err = runService.CompleteAttempt(ctx, tx, CompleteProductionAttemptParams{
		OrganizationID: production.OrganizationID,
		ProjectID:      production.ProjectID,
		RunID:          run.ID,
		ItemID:         items[0].id,
		AttemptID:      firstAttempt.ID,
		Status:         ItemSucceeded,
		OutputSnapshot: json.RawMessage(`{"committed":true}`),
		ProviderCallID: productionProviderCallID,
	})
	if err != nil {
		t.Fatalf("complete first commerce production attempt: %v", err)
	}
	if run.Status != RunRunning || run.CompletedItems != 1 || run.TotalItems != 2 {
		t.Fatalf("run after first commit = %+v", run)
	}
	secondAttempt, err := runService.StartAttempt(ctx, tx, StartProductionAttemptParams{
		OrganizationID: production.OrganizationID,
		ProjectID:      production.ProjectID,
		RunID:          run.ID,
		ItemID:         items[1].id,
		InputHash:      items[1].inputHash,
	})
	if err != nil {
		t.Fatalf("start second commerce production attempt: %v", err)
	}
	run, err = runService.CompleteAttempt(ctx, tx, CompleteProductionAttemptParams{
		OrganizationID: production.OrganizationID,
		ProjectID:      production.ProjectID,
		RunID:          run.ID,
		ItemID:         items[1].id,
		AttemptID:      secondAttempt.ID,
		Status:         ItemFailedRetryable,
		OutputSnapshot: json.RawMessage(`{}`),
		ErrorCode:      "UPSTREAM_TIMEOUT",
		ErrorMessage:   "provider request timed out",
		Retryable:      true,
	})
	if err != nil {
		t.Fatalf("fail second commerce production attempt: %v", err)
	}
	if run.Status != RunPartiallySucceeded || run.CompletedItems != 1 || run.FailedItems != 1 {
		t.Fatalf("partially succeeded run = %+v", run)
	}
	if _, err := runService.StartAttempt(ctx, tx, StartProductionAttemptParams{
		OrganizationID: production.OrganizationID,
		ProjectID:      production.ProjectID,
		RunID:          run.ID,
		ItemID:         items[1].id,
		InputHash:      hashValue('e'),
	}); err == nil {
		t.Fatal("start retry with changed input hash succeeded, want generation mismatch")
	} else if typed, ok := AsError(err); !ok || typed.Code != CodeGenerationMismatch {
		t.Fatalf("changed retry input error = %v, want %s", err, CodeGenerationMismatch)
	}
	retryAttempt, err := runService.StartAttempt(ctx, tx, StartProductionAttemptParams{
		OrganizationID: production.OrganizationID,
		ProjectID:      production.ProjectID,
		RunID:          run.ID,
		ItemID:         items[1].id,
		InputHash:      items[1].inputHash,
	})
	if err != nil {
		t.Fatalf("start retry commerce production attempt: %v", err)
	}
	if retryAttempt.AttemptNumber != 2 {
		t.Fatalf("retry attempt number = %d, want 2", retryAttempt.AttemptNumber)
	}
	run, err = runService.CompleteAttempt(ctx, tx, CompleteProductionAttemptParams{
		OrganizationID: production.OrganizationID,
		ProjectID:      production.ProjectID,
		RunID:          run.ID,
		ItemID:         items[1].id,
		AttemptID:      retryAttempt.ID,
		Status:         ItemSucceeded,
		OutputSnapshot: json.RawMessage(`{"committed":true,"retry":true}`),
	})
	if err != nil {
		t.Fatalf("complete retry commerce production attempt: %v", err)
	}
	if run.Status != RunSucceeded || run.CompletedItems != 2 || run.FailedItems != 0 || run.CompletedAt == nil {
		t.Fatalf("succeeded run after retry = %+v", run)
	}
	var attemptCount, failedAttemptCount int
	if err := tx.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE status = 'failed_retryable')
		FROM commerce_production_run_item_attempts
		WHERE run_id = $1
	`, run.ID).Scan(&attemptCount, &failedAttemptCount); err != nil {
		t.Fatalf("count immutable commerce attempts: %v", err)
	}
	if attemptCount != 3 || failedAttemptCount != 1 {
		t.Fatalf("attempt history count=%d failed=%d, want 3/1", attemptCount, failedAttemptCount)
	}
	listedItems, err := repository.ListProductionRunItems(ctx, tx, production.OrganizationID, production.ProjectID, run.ID)
	if err != nil {
		t.Fatalf("list commerce production run provenance: %v", err)
	}
	if len(listedItems) != 2 || listedItems[0].ProviderCallID != productionProviderCallID {
		t.Fatalf("production run item provenance = %+v, want provider call %s", listedItems, productionProviderCallID)
	}
}

func insertCommerceProductionProviderCallFixture(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	productionGenerationID string,
) string {
	t.Helper()
	var providerCallID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO provider_call_logs(
			organization_id, project_id, provider_account_id,
			prompt_version_id, prompt_hash, input_hash, output_hash,
			task_type, execution_mode, status, completed_at,
			production_generation_id
		)
		SELECT $1, $2, account.id, version.id, $3, $4, $5,
		       'text.generate', 'sync', 'succeeded', now(), $6
		FROM provider_accounts account
		CROSS JOIN LATERAL (
			SELECT prompt_version.id
			FROM prompt_versions prompt_version
			WHERE prompt_version.status = 'active'
			ORDER BY prompt_version.created_at
			LIMIT 1
		) version
		WHERE account.organization_id = $1
		ORDER BY account.created_at
		LIMIT 1
		RETURNING id::text
	`, organizationID, projectID, strings.Repeat("1", 64), strings.Repeat("2", 64),
		strings.Repeat("3", 64), productionGenerationID).Scan(&providerCallID); err != nil {
		t.Fatalf("insert commerce production provider call fixture: %v", err)
	}
	return providerCallID
}

func insertCommerceSalesScriptContractFixture(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	production ProductionContext,
	unit UnitGenerationContext,
	userID string,
	suffix string,
) (string, string) {
	t.Helper()
	hash := func(value byte) string { return strings.Repeat(string(value), 64) }
	var workflowRunID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO workflow_runs(
			organization_id, project_id, temporal_workflow_id, workflow_type, status,
			input, output, created_by, production_generation_id,
			video_production_binding_id, video_production_binding_revision,
			started_at, completed_at
		)
		VALUES ($1, $2, $3, 'commerce_script_organization', 'succeeded', '{}', '{}', $4, $5, $6, $7, now(), now())
		RETURNING id::text
	`, production.OrganizationID, production.ProjectID, "commerce-script-contract-"+suffix, userID,
		production.Generation.ID, production.VideoBinding.ID, production.VideoBinding.Revision).Scan(&workflowRunID); err != nil {
		t.Fatalf("insert commerce sales script workflow run: %v", err)
	}
	var promptVersionID string
	if err := tx.QueryRow(ctx, `
		SELECT version.id::text
		FROM prompt_versions version
		JOIN prompt_templates template ON template.id = version.prompt_template_id
		WHERE template.template_key = 'commerce_script_organizer'
		  AND version.status = 'active'
		ORDER BY version.version DESC
		LIMIT 1
	`).Scan(&promptVersionID); err != nil {
		t.Fatalf("resolve commerce script organizer prompt: %v", err)
	}
	var connectorID, accountID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO provider_connectors(connector_key, name, type, manifest)
		VALUES ($1, 'Commerce Contract Fixture', 'openai_compatible', '{}')
		RETURNING id::text
	`, "commerce-contract-"+suffix).Scan(&connectorID); err != nil {
		t.Fatalf("insert provider connector fixture: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO provider_accounts(organization_id, connector_id, name, created_by)
		VALUES ($1, $2, 'Commerce Contract Fixture', $3)
		RETURNING id::text
	`, production.OrganizationID, connectorID, userID).Scan(&accountID); err != nil {
		t.Fatalf("insert provider account fixture: %v", err)
	}
	var providerCallID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO provider_call_logs(
			organization_id, project_id, workflow_run_id, provider_account_id,
			prompt_version_id, prompt_hash, input_hash, output_hash,
			task_type, execution_mode, status, completed_at,
			production_generation_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'text.generate', 'sync', 'succeeded', now(), $9)
		RETURNING id::text
	`, production.OrganizationID, production.ProjectID, workflowRunID, accountID,
		promptVersionID, hash('b'), hash('c'), hash('d'), production.Generation.ID).Scan(&providerCallID); err != nil {
		t.Fatalf("insert provider call fixture: %v", err)
	}
	contractHash := hash('e')
	var contractID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_sales_script_contracts(
			organization_id, project_id, product_id, script_unit_id,
			script_unit_generation_id, project_production_generation_id,
			commerce_workflow_binding_id, commerce_workflow_binding_revision,
			product_version_id, source_script_version_id, localization_id, reference_pack_id,
			status, attempt_generation, current_workflow_run_id, input_hash,
			contract_version, contract, contract_hash, prompt_version_id,
			provider_call_id, accepted_round, created_by, completed_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
			'ready', 1, $13, $14, 'commerce-sales-script/v1',
			'{"segments":[]}', $15, $16, $17, 1, $18, now()
		)
		RETURNING id::text
	`, production.OrganizationID, production.ProjectID, unit.Identity.ProductID, unit.Identity.ScriptUnitID,
		unit.Identity.UnitGenerationID, production.Generation.ID, production.CommerceBinding.ID,
		production.CommerceBinding.Revision, unit.ProductVersionID, unit.SourceScriptVersionID,
		unit.LocalizationID, unit.ReferencePackID, workflowRunID, hash('f'), contractHash,
		promptVersionID, providerCallID, userID).Scan(&contractID); err != nil {
		t.Fatalf("insert commerce sales script contract fixture: %v", err)
	}
	return contractID, contractHash
}

func stringPointer(value string) *string {
	return &value
}

func insertCommerceUnitGenerationFixture(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	production ProductionContext,
	userID string,
) UnitGenerationIdentity {
	t.Helper()
	hash := func(value byte) string { return strings.Repeat(string(value), 64) }
	var productID, productVersionID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_products(organization_id, project_id, status, created_by)
		VALUES ($1, $2, 'draft', $3)
		RETURNING id::text
	`, production.OrganizationID, production.ProjectID, userID).Scan(&productID); err != nil {
		t.Fatalf("insert commerce product: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_product_versions(
			organization_id, project_id, product_id, version, name,
			facts_snapshot, facts_hash, created_by
		)
		VALUES ($1, $2, $3, 1, 'Integration Product', '{"name":"Integration Product"}', $4, $5)
		RETURNING id::text
	`, production.OrganizationID, production.ProjectID, productID, hash('1'), userID).Scan(&productVersionID); err != nil {
		t.Fatalf("insert commerce product version: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_products
		SET current_version_id = $2, status = 'ready'
		WHERE id = $1
	`, productID, productVersionID); err != nil {
		t.Fatalf("activate commerce product version: %v", err)
	}

	var scriptUnitID, sourceVersionID, resolutionID, localizationID, referencePackID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_script_units(
			organization_id, project_id, product_id, unit_no, title, sort_order,
			language_mode, target_duration_seconds, created_by
		)
		VALUES ($1, $2, $3, 1, 'Unit 1', 1, 'auto', 30, $4)
		RETURNING id::text
	`, production.OrganizationID, production.ProjectID, productID, userID).Scan(&scriptUnitID); err != nil {
		t.Fatalf("insert commerce script unit: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_ad_script_versions(
			organization_id, project_id, product_id, script_unit_id,
			version, content, content_hash, detected_source_language, created_by
		)
		VALUES ($1, $2, $3, $4, 1, '真实商品脚本', $5, 'zh-CN', $6)
		RETURNING id::text
	`, production.OrganizationID, production.ProjectID, productID, scriptUnitID, hash('2'), userID).Scan(&sourceVersionID); err != nil {
		t.Fatalf("insert commerce source version: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_language_resolutions(
			organization_id, project_id, product_id, script_unit_id, source_script_version_id,
			language_mode, source_language, target_language, confidence, status,
			confirmed_by, confirmed_at, input_hash
		)
		VALUES ($1, $2, $3, $4, $5, 'auto', 'zh-CN', 'zh-CN', 1, 'confirmed', $6, now(), $7)
		RETURNING id::text
	`, production.OrganizationID, production.ProjectID, productID, scriptUnitID, sourceVersionID, userID, hash('3')).Scan(&resolutionID); err != nil {
		t.Fatalf("insert commerce language resolution: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_ad_script_localizations(
			organization_id, project_id, product_id, script_unit_id, source_script_version_id,
			language_resolution_id, version, source_language, target_language,
			localized_content, localized_content_hash, structured_contract,
			estimated_voiceover_seconds, timing_analysis, timing_policy_version,
			review_status, status, created_by, approved_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, 1, 'zh-CN', 'zh-CN',
		        '真实商品脚本', $7, '{}', 4, '{}', 'zh-default-v1',
		        'approved', 'approved', $8, now())
		RETURNING id::text
	`, production.OrganizationID, production.ProjectID, productID, scriptUnitID, sourceVersionID, resolutionID, hash('4'), userID).Scan(&localizationID); err != nil {
		t.Fatalf("insert commerce localization: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_product_reference_packs(
			organization_id, project_id, product_id, product_version_id,
			product_facts_hash, reference_set_hash, pack_hash, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id::text
	`, production.OrganizationID, production.ProjectID, productID, productVersionID, hash('1'), hash('5'), hash('6'), userID).Scan(&referencePackID); err != nil {
		t.Fatalf("insert commerce reference pack: %v", err)
	}

	configurationHash := hash('7')
	var unitGenerationID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_script_unit_generations(
			organization_id, project_id, product_id, script_unit_id,
			script_unit_revision, project_production_generation_id,
			unit_generation_no, status,
			commerce_workflow_binding_id, commerce_workflow_binding_revision,
			product_version_id, source_script_version_id, localization_id,
			reference_pack_id, unit_configuration_snapshot, unit_configuration_hash,
			created_by, activated_at
		)
		VALUES ($1, $2, $3, $4, 1, $5, 1, 'active', $6, $7, $8, $9, $10, $11, '{}', $12, $13, now())
		RETURNING id::text
	`, production.OrganizationID, production.ProjectID, productID, scriptUnitID,
		production.Generation.ID, production.CommerceBinding.ID, production.CommerceBinding.Revision,
		productVersionID, sourceVersionID, localizationID, referencePackID, configurationHash, userID).Scan(&unitGenerationID); err != nil {
		t.Fatalf("insert commerce unit generation: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_units
		SET current_source_version_id = $2,
		    current_localization_id = $3,
		    active_unit_generation_id = $4,
		    unit_generation_no = 1,
		    status = 'ready'
		WHERE id = $1
	`, scriptUnitID, sourceVersionID, localizationID, unitGenerationID); err != nil {
		t.Fatalf("activate commerce unit generation: %v", err)
	}

	identity := production.ExecutionIdentity()
	return UnitGenerationIdentity{
		ExecutionIdentity:     identity,
		ProductID:             productID,
		ScriptUnitID:          scriptUnitID,
		ScriptUnitRevision:    1,
		UnitGenerationID:      unitGenerationID,
		UnitGenerationNo:      1,
		UnitConfigurationHash: configurationHash,
	}
}
