package workflows

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCommerceScriptUnitRebuildCommitIsAtomicIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_COMMERCE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_COMMERCE_INTEGRATION_TEST=1 to run commerce workflow integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for commerce workflow integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	t.Cleanup(pool.Close)

	fixture := seedCommerceScriptUnitRebuildAtomicFixture(t, ctx, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, fixture.organizationID)
	})

	failingTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin failing rebuild transaction: %v", err)
	}
	failingState := fixture.state
	failingState.Unit.Revision++
	if _, err := activateRebuiltUnitGeneration(
		ctx, failingTx, failingState, fixture.commit,
		fixture.targetLocalizationID, fixture.targetGenerationID,
		fixture.targetGenerationNo, fixture.targetConfigurationHash,
	); err == nil {
		_ = failingTx.Rollback(ctx)
		t.Fatal("script rebuild activation unexpectedly succeeded with a stale script unit revision")
	}
	if err := failingTx.Rollback(ctx); err != nil && err != pgx.ErrTxClosed {
		t.Fatalf("rollback failing rebuild transaction: %v", err)
	}
	assertCommerceScriptUnitRebuildAtomicState(t, ctx, pool, fixture, false)

	successTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin successful rebuild transaction: %v", err)
	}
	result, err := activateRebuiltUnitGeneration(
		ctx, successTx, fixture.state, fixture.commit,
		fixture.targetLocalizationID, fixture.targetGenerationID,
		fixture.targetGenerationNo, fixture.targetConfigurationHash,
	)
	if err != nil {
		_ = successTx.Rollback(ctx)
		t.Fatalf("activate rebuilt unit generation: %v", err)
	}
	if result.UnitGenerationID != fixture.targetGenerationID ||
		result.UnitGenerationNo != fixture.targetGenerationNo ||
		result.ScriptUnitRevision != fixture.state.Unit.Revision+1 {
		_ = successTx.Rollback(ctx)
		t.Fatalf("rebuilt unit identity = %+v", result)
	}
	if err := successTx.Commit(ctx); err != nil {
		t.Fatalf("commit successful rebuild transaction: %v", err)
	}
	assertCommerceScriptUnitRebuildAtomicState(t, ctx, pool, fixture, true)
}

type commerceScriptUnitRebuildAtomicFixture struct {
	organizationID          string
	sourceGenerationID      string
	targetGenerationID      string
	targetLocalizationID    string
	targetSourceVersionID   string
	targetConfigurationHash string
	targetGenerationNo      int64
	state                   commercePreparationFrozenState
	commit                  CommerceScriptUnitPreparationCommit
}

func seedCommerceScriptUnitRebuildAtomicFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) commerceScriptUnitRebuildAtomicFixture {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin fixture transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	hash := func(value byte) string { return strings.Repeat(string(value), 64) }
	var organizationID, userID, workspaceID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO organizations(name, slug)
		VALUES ('Commerce Rebuild Atomic', $1)
		RETURNING id::text
	`, "commerce-rebuild-atomic-"+suffix).Scan(&organizationID); err != nil {
		t.Fatalf("insert organization: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO users(email, display_name)
		VALUES ($1, 'Commerce Rebuild Atomic')
		RETURNING id::text
	`, "commerce-rebuild-atomic-"+suffix+"@example.test").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO workspaces(organization_id, name)
		VALUES ($1, 'Commerce Rebuild Atomic')
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
		VALUES ($1, $2, 'Commerce Rebuild Atomic', 'active', $3)
		RETURNING id::text
	`, organizationID, commerce.DefaultWorkflowTemplateKey, userID).Scan(&templateID); err != nil {
		t.Fatalf("insert workflow template: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_workflow_template_versions(
			template_id, version, configuration_snapshot, prompt_bindings,
			agent_model_contracts, language_contract, image_capability_contract,
			video_capability_contract, video_production_profile_version_id,
			content_hash, status, created_by, published_at
		)
		VALUES ($1, 1, '{}', '{}', '{}', '{}', '{}', '{}', $2, $3, 'published', $4, now())
		RETURNING id::text
	`, templateID, profileVersionID, hash('a'), userID).Scan(&templateVersionID); err != nil {
		t.Fatalf("insert workflow template version: %v", err)
	}

	repository := commerce.NewRepository()
	service := commerce.NewService(repository)
	draft, err := service.CreateDraftProject(ctx, tx, commerce.DraftProjectParams{
		OrganizationID: organizationID, WorkspaceID: workspaceID,
		Name: "Commerce Rebuild Atomic", VideoRatio: "9:16",
		AspectRatio: commerceRebuildStringPointer("9:16"), AudioStrategy: "native_av",
		AudioRequirement: "preferred", ImageQuality: "standard",
		TimelineTimebase: 90000, FPSNumerator: 24, FPSDenominator: 1,
		Settings:  json.RawMessage(`{"defaultLanguageMode":"explicit"}`),
		CreatedBy: userID, IdempotencyScope: "commerce_project_create",
		ClientRequestID: "commerce-rebuild-atomic-" + suffix,
		RequestHash:     hash('b'), InputSnapshot: json.RawMessage(`{"projectKind":"commerce_video"}`),
		SetupExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create commerce draft: %v", err)
	}
	bindings, err := service.PrepareInitialBindings(ctx, tx, commerce.InitialBindingParams{
		OrganizationID: organizationID, ProjectID: draft.ProjectID,
		WorkflowTemplateVersion: templateVersionID, CreatedBy: userID,
		CompatibilityPolicy: videoproduction.CompatibilityStrict,
		VideoOverrides:      json.RawMessage(`{}`),
		ProductionConfiguration: videoproduction.ProductionConfigurationSnapshot{
			SchemaVersion: videoproduction.ProductionConfigurationSnapshotVersion,
			ProjectType:   commerce.ProjectTypeCommerce, AspectRatio: "9:16", VideoRatio: "9:16",
			ImageModelProfileKey: "image_generation_default", VideoModelProfileKey: "video_generation_default",
			ScriptModelProfileKey: "script_agent_default", TTSModelProfileKey: "tts_generation_default",
			ASRModelProfileKey: "audio_transcription_default", AudioStrategy: "native_av",
			AudioRequirement: "preferred", ImageQuality: "standard", TimelineTimebase: 90000,
			FPSNumerator: 24, FPSDenominator: 1, Settings: json.RawMessage(`{}`),
			ManualBindings: map[string]videoproduction.ManualBindingSnapshot{},
		},
		ConfigurationSnapshot: json.RawMessage(`{"schemaVersion":2}`),
		ModelRoutingSnapshot:  json.RawMessage(`{}`),
		CapabilitySnapshot:    json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("prepare initial commerce bindings: %v", err)
	}
	if err := service.ActivateInitialBindings(ctx, tx, draft.ProjectID, bindings); err != nil {
		t.Fatalf("activate initial commerce bindings: %v", err)
	}
	production, err := repository.LoadActiveProductionContext(ctx, tx, organizationID, draft.ProjectID)
	if err != nil {
		t.Fatalf("load active production context: %v", err)
	}

	var productID, productVersionID, referencePackID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_products(organization_id, project_id, status, created_by)
		VALUES ($1, $2, 'draft', $3)
		RETURNING id::text
	`, organizationID, draft.ProjectID, userID).Scan(&productID); err != nil {
		t.Fatalf("insert commerce product: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_product_versions(
			organization_id, project_id, product_id, version, name,
			facts_snapshot, facts_hash, created_by
		)
		VALUES ($1, $2, $3, 1, 'Atomic Product', '{"name":"Atomic Product"}', $4, $5)
		RETURNING id::text
	`, organizationID, draft.ProjectID, productID, hash('c'), userID).Scan(&productVersionID); err != nil {
		t.Fatalf("insert commerce product version: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_products SET current_version_id = $2, status = 'ready' WHERE id = $1
	`, productID, productVersionID); err != nil {
		t.Fatalf("activate commerce product: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_product_reference_packs(
			organization_id, project_id, product_id, product_version_id,
			product_facts_hash, reference_set_hash, pack_hash, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id::text
	`, organizationID, draft.ProjectID, productID, productVersionID,
		hash('c'), hash('d'), hash('e'), userID).Scan(&referencePackID); err != nil {
		t.Fatalf("insert commerce reference pack: %v", err)
	}

	var scriptUnitID, sourceVersionID, targetVersionID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_script_units(
			organization_id, project_id, product_id, unit_no, title, sort_order,
			language_mode, explicit_target_language, target_duration_seconds,
			target_platform, draft_content, draft_content_hash, created_by
		)
		VALUES ($1, $2, $3, 1, 'Atomic Unit', 1, 'explicit', 'zh-CN', 30,
		        'douyin', '旧广告脚本', $4, $5)
		RETURNING id::text
	`, organizationID, draft.ProjectID, productID, hash('f'), userID).Scan(&scriptUnitID); err != nil {
		t.Fatalf("insert commerce script unit: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_ad_script_versions(
			organization_id, project_id, product_id, script_unit_id,
			version, content, content_hash, detected_source_language, created_by
		)
		VALUES ($1, $2, $3, $4, 1, '旧广告脚本', $5, 'zh-CN', $6)
		RETURNING id::text
	`, organizationID, draft.ProjectID, productID, scriptUnitID, hash('f'), userID).Scan(&sourceVersionID); err != nil {
		t.Fatalf("insert source script version: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_ad_script_versions(
			organization_id, project_id, product_id, script_unit_id,
			version, content, content_hash, detected_source_language,
			source_version_id, created_by
		)
		VALUES ($1, $2, $3, $4, 2, 'New commerce script', $5, 'en-US', $6, $7)
		RETURNING id::text
	`, organizationID, draft.ProjectID, productID, scriptUnitID, hash('1'), sourceVersionID, userID).Scan(&targetVersionID); err != nil {
		t.Fatalf("insert target script version: %v", err)
	}

	var sourceResolutionID, targetResolutionID, sourceLocalizationID, targetLocalizationID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_language_resolutions(
			organization_id, project_id, product_id, script_unit_id,
			source_script_version_id, language_mode, source_language,
			target_language, confidence, status, confirmed_by, confirmed_at, input_hash
		)
		VALUES ($1, $2, $3, $4, $5, 'explicit', 'zh-CN', 'zh-CN', 1,
		        'confirmed', $6, now(), $7)
		RETURNING id::text
	`, organizationID, draft.ProjectID, productID, scriptUnitID,
		sourceVersionID, userID, hash('2')).Scan(&sourceResolutionID); err != nil {
		t.Fatalf("insert source language resolution: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_language_resolutions(
			organization_id, project_id, product_id, script_unit_id,
			source_script_version_id, language_mode, source_language,
			target_language, confidence, status, confirmed_by, confirmed_at, input_hash
		)
		VALUES ($1, $2, $3, $4, $5, 'explicit', 'en-US', 'en-US', 1,
		        'confirmed', $6, now(), $7)
		RETURNING id::text
	`, organizationID, draft.ProjectID, productID, scriptUnitID,
		targetVersionID, userID, hash('3')).Scan(&targetResolutionID); err != nil {
		t.Fatalf("insert target language resolution: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_ad_script_localizations(
			organization_id, project_id, product_id, script_unit_id,
			source_script_version_id, language_resolution_id, version,
			source_language, target_language, localized_content,
			localized_content_hash, structured_contract, estimated_voiceover_seconds,
			timing_analysis, timing_policy_version, review_status, status, created_by, approved_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, 1, 'zh-CN', 'zh-CN', '旧广告脚本',
		        $7, '{}', 4, '{}', 'zh-v1', 'approved', 'approved', $8, now())
		RETURNING id::text
	`, organizationID, draft.ProjectID, productID, scriptUnitID, sourceVersionID,
		sourceResolutionID, hash('4'), userID).Scan(&sourceLocalizationID); err != nil {
		t.Fatalf("insert source localization: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_ad_script_localizations(
			organization_id, project_id, product_id, script_unit_id,
			source_script_version_id, language_resolution_id, version,
			source_language, target_language, localized_content,
			localized_content_hash, structured_contract, estimated_voiceover_seconds,
			timing_analysis, timing_policy_version, review_status, status, created_by, approved_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, 2, 'en-US', 'en-US', 'New commerce script',
		        $7, '{}', 4, '{}', 'en-v1', 'approved', 'approved', $8, now())
		RETURNING id::text
	`, organizationID, draft.ProjectID, productID, scriptUnitID, targetVersionID,
		targetResolutionID, hash('5'), userID).Scan(&targetLocalizationID); err != nil {
		t.Fatalf("insert target localization: %v", err)
	}

	var sourceGenerationID, targetGenerationID string
	sourceConfigurationHash := hash('6')
	targetConfigurationHash := hash('7')
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_script_unit_generations(
			organization_id, project_id, product_id, script_unit_id,
			project_production_generation_id, unit_generation_no, status,
			commerce_workflow_binding_id, commerce_workflow_binding_revision,
			product_version_id, source_script_version_id, localization_id,
			reference_pack_id, unit_configuration_snapshot, unit_configuration_hash,
			created_by, activated_at
		)
		VALUES ($1, $2, $3, $4, $5, 1, 'active', $6, $7, $8, $9, $10, $11,
		        '{}', $12, $13, now())
		RETURNING id::text
	`, organizationID, draft.ProjectID, productID, scriptUnitID,
		production.Generation.ID, production.CommerceBinding.ID, production.CommerceBinding.Revision,
		productVersionID, sourceVersionID, sourceLocalizationID, referencePackID,
		sourceConfigurationHash, userID).Scan(&sourceGenerationID); err != nil {
		t.Fatalf("insert source unit generation: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_script_unit_generations(
			organization_id, project_id, product_id, script_unit_id,
			project_production_generation_id, unit_generation_no, status,
			commerce_workflow_binding_id, commerce_workflow_binding_revision,
			product_version_id, source_script_version_id, localization_id,
			reference_pack_id, unit_configuration_snapshot, unit_configuration_hash,
			source_unit_generation_id, created_by
		)
		VALUES ($1, $2, $3, $4, $5, 2, 'preparing', $6, $7, $8, $9, $10, $11,
		        '{}', $12, $13, $14)
		RETURNING id::text
	`, organizationID, draft.ProjectID, productID, scriptUnitID,
		production.Generation.ID, production.CommerceBinding.ID, production.CommerceBinding.Revision,
		productVersionID, targetVersionID, targetLocalizationID, referencePackID,
		targetConfigurationHash, sourceGenerationID, userID).Scan(&targetGenerationID); err != nil {
		t.Fatalf("insert target unit generation: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_units
		SET current_source_version_id = $2, current_localization_id = $3,
		    active_unit_generation_id = $4, unit_generation_no = 1, status = 'ready'
		WHERE id = $1
	`, scriptUnitID, sourceVersionID, sourceLocalizationID, sourceGenerationID); err != nil {
		t.Fatalf("activate source unit generation: %v", err)
	}

	var workflowRunID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO workflow_runs(
			organization_id, project_id, temporal_workflow_id, workflow_type, status,
			input, created_by, production_generation_id,
			video_production_binding_id, video_production_binding_revision, started_at
		)
		VALUES ($1, $2, $3, 'commerce_script_unit_preparation', 'running', '{}',
		        $4, $5, $6, $7, now())
		RETURNING id::text
	`, organizationID, draft.ProjectID, "commerce-rebuild-atomic-"+suffix, userID,
		production.Generation.ID, production.VideoBinding.ID, production.VideoBinding.Revision).Scan(&workflowRunID); err != nil {
		t.Fatalf("insert rebuild workflow run: %v", err)
	}
	var rebuildID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_script_unit_rebuilds(
			organization_id, project_id, product_id, script_unit_id,
			project_production_generation_id, source_unit_generation_id,
			source_unit_configuration_hash, source_script_version_id,
			source_localization_id, target_source_script_version_id,
			target_language_mode, target_explicit_language,
			target_duration_seconds, target_platform,
			target_configuration_snapshot, target_configuration_hash,
			impact_snapshot, impact_token, impact_expires_at,
			expected_script_unit_revision, status, idempotency_key,
			workflow_run_id, requested_by, started_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
		        'explicit', 'en-US', 60, 'tiktok', '{}', $11,
		        '{}', $12, now() + interval '15 minutes', 1, 'running', $13,
		        $14, $15, now())
		RETURNING id::text
	`, organizationID, draft.ProjectID, productID, scriptUnitID,
		production.Generation.ID, sourceGenerationID, sourceConfigurationHash,
		sourceVersionID, sourceLocalizationID, targetVersionID,
		targetConfigurationHash, hash('8'), "commerce-rebuild-atomic-"+suffix,
		workflowRunID, userID).Scan(&rebuildID); err != nil {
		t.Fatalf("insert script unit rebuild: %v", err)
	}

	targetLanguage := "en-US"
	commit := CommerceScriptUnitPreparationCommit{
		WorkflowInput: CommerceScriptUnitPreparationInput{
			Identity: commerce.ScriptUnitPreparationIdentity{
				ExecutionIdentity: production.ExecutionIdentity(),
				ProductID:         productID, ProductVersionID: productVersionID,
				ProductFactsHash: hash('c'), ScriptUnitID: scriptUnitID,
				ScriptUnitRevision: 1, SourceScriptVersionID: targetVersionID,
				SourceScriptContentHash: hash('1'), ReferencePackID: referencePackID,
				ReferencePackHash: hash('e'), RebuildID: rebuildID,
				SourceUnitGenerationID:  sourceGenerationID,
				TargetConfigurationHash: targetConfigurationHash,
			},
			WorkflowRunID: workflowRunID, AttemptGeneration: 1, CreatedBy: userID,
		},
	}
	state := commercePreparationFrozenState{
		Production: production,
		Product:    commerce.Product{ID: productID, OrganizationID: organizationID, ProjectID: draft.ProjectID},
		Unit: commerce.ScriptUnit{
			ID: scriptUnitID, OrganizationID: organizationID, ProjectID: draft.ProjectID,
			ProductID: productID, Revision: 1, LanguageMode: "explicit",
			ExplicitTargetLanguage: &targetLanguage, TargetDurationSeconds: 60,
			TargetPlatform: "tiktok",
		},
		SourceVersion: commerce.ScriptVersion{
			ID: targetVersionID, OrganizationID: organizationID, ProjectID: draft.ProjectID,
			ProductID: productID, ScriptUnitID: scriptUnitID,
			Content: "New commerce script", ContentHash: hash('1'),
		},
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit rebuild fixture: %v", err)
	}
	return commerceScriptUnitRebuildAtomicFixture{
		organizationID: organizationID, sourceGenerationID: sourceGenerationID,
		targetGenerationID: targetGenerationID, targetLocalizationID: targetLocalizationID,
		targetSourceVersionID: targetVersionID, targetConfigurationHash: targetConfigurationHash,
		targetGenerationNo: 2, state: state, commit: commit,
	}
}

func assertCommerceScriptUnitRebuildAtomicState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture commerceScriptUnitRebuildAtomicFixture,
	committed bool,
) {
	t.Helper()
	var sourceStatus, targetStatus, rebuildStatus string
	var activeGenerationID, currentSourceVersionID, currentLocalizationID string
	var languageMode, explicitLanguage, targetPlatform, draftContent string
	var targetDuration int
	var unitGenerationNo, unitRevision int64
	var rebuildTargetLocalizationID, rebuildTargetGenerationID *string
	if err := pool.QueryRow(ctx, `
		SELECT source.status, target.status, rebuild.status,
		       unit.active_unit_generation_id::text, unit.current_source_version_id::text,
		       unit.current_localization_id::text, unit.language_mode,
		       COALESCE(unit.explicit_target_language, ''), unit.target_duration_seconds,
		       unit.target_platform, unit.draft_content, unit.unit_generation_no, unit.revision,
		       rebuild.target_localization_id::text, rebuild.target_unit_generation_id::text
		FROM commerce_script_unit_rebuilds rebuild
		JOIN commerce_script_units unit ON unit.id = rebuild.script_unit_id
		JOIN commerce_script_unit_generations source ON source.id = rebuild.source_unit_generation_id
		JOIN commerce_script_unit_generations target ON target.id = $2
		WHERE rebuild.id = $1
	`, fixture.commit.WorkflowInput.Identity.RebuildID, fixture.targetGenerationID).Scan(
		&sourceStatus, &targetStatus, &rebuildStatus,
		&activeGenerationID, &currentSourceVersionID, &currentLocalizationID,
		&languageMode, &explicitLanguage, &targetDuration, &targetPlatform,
		&draftContent, &unitGenerationNo, &unitRevision,
		&rebuildTargetLocalizationID, &rebuildTargetGenerationID,
	); err != nil {
		t.Fatalf("load script unit rebuild atomic state: %v", err)
	}
	if !committed {
		if sourceStatus != "active" || targetStatus != "preparing" || rebuildStatus != "running" ||
			activeGenerationID != fixture.sourceGenerationID || unitGenerationNo != 1 || unitRevision != 1 ||
			rebuildTargetLocalizationID != nil || rebuildTargetGenerationID != nil {
			t.Fatalf("failed rebuild leaked state source=%s target=%s rebuild=%s active=%s generation=%d revision=%d targetIds=%v/%v",
				sourceStatus, targetStatus, rebuildStatus, activeGenerationID,
				unitGenerationNo, unitRevision, rebuildTargetLocalizationID, rebuildTargetGenerationID)
		}
		return
	}
	if sourceStatus != "archived" || targetStatus != "active" || rebuildStatus != "succeeded" ||
		activeGenerationID != fixture.targetGenerationID || currentSourceVersionID != fixture.targetSourceVersionID ||
		currentLocalizationID != fixture.targetLocalizationID || languageMode != "explicit" || explicitLanguage != "en-US" ||
		targetDuration != 60 || targetPlatform != "tiktok" || draftContent != "New commerce script" ||
		unitGenerationNo != fixture.targetGenerationNo || unitRevision != 2 ||
		rebuildTargetLocalizationID == nil || *rebuildTargetLocalizationID != fixture.targetLocalizationID ||
		rebuildTargetGenerationID == nil || *rebuildTargetGenerationID != fixture.targetGenerationID {
		t.Fatalf("committed rebuild state source=%s target=%s rebuild=%s active=%s sourceVersion=%s localization=%s language=%s/%s duration=%d platform=%s draft=%q generation=%d revision=%d targetIds=%v/%v",
			sourceStatus, targetStatus, rebuildStatus, activeGenerationID, currentSourceVersionID,
			currentLocalizationID, languageMode, explicitLanguage, targetDuration, targetPlatform,
			draftContent, unitGenerationNo, unitRevision, rebuildTargetLocalizationID, rebuildTargetGenerationID)
	}
}

func commerceRebuildStringPointer(value string) *string {
	return &value
}
