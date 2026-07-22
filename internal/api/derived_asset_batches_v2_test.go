package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/google/uuid"
)

func TestNormalizeDerivedAssetBatchOptionsPreservesExplicitOrderAndDuplicates(t *testing.T) {
	first := uuid.NewString()
	second := uuid.NewString()
	options, err := normalizeDerivedAssetBatchOptions(DerivedAssetBatchCreateOptions{
		Mode:           " EXPLICIT ",
		RequirementIDs: []string{first, second, first},
		Filters: DerivedAssetBatchFilters{
			AssetTypes:     []string{"Scene", "character", "scene"},
			ReviewStatuses: []string{"APPROVED", "pending", "approved"},
			ShotIDs:        []string{" shot-b ", "", "shot-a", "shot-b"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(options.RequirementIDs, ","); got != strings.Join([]string{first, second, first}, ",") {
		t.Fatalf("requirement IDs changed: %s", got)
	}
	if got := strings.Join(options.Filters.AssetTypes, ","); got != "character,scene" {
		t.Fatalf("asset filters = %s", got)
	}
	if got := strings.Join(options.Filters.ReviewStatuses, ","); got != "approved,pending" {
		t.Fatalf("review filters = %s", got)
	}
	if got := strings.Join(options.Filters.ShotIDs, ","); got != "shot-a,shot-b" {
		t.Fatalf("shot filters = %s", got)
	}
	if options.MaxConcurrency != workflows.DefaultDerivedAssetImageConcurrency {
		t.Fatalf("max concurrency = %d", options.MaxConcurrency)
	}
}

func TestDerivedAssetRequestedItemPreservesMalformedOriginalID(t *testing.T) {
	first := newDerivedAssetRequestedItem("not-a-uuid")
	second := newDerivedAssetRequestedItem("not-a-uuid")
	if first.originalID != second.originalID || first.inputHash != second.inputHash {
		t.Fatalf("original identity is not stable: %#v %#v", first, second)
	}
	if first.originalID != "not-a-uuid" || first.lookupID != "" {
		t.Fatalf("malformed original ID was rewritten: %#v", first)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(first.inputSnapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot["requestedId"] != "not-a-uuid" || snapshot["originalId"] != first.originalID {
		t.Fatalf("input snapshot = %#v", snapshot)
	}
}

func TestDerivedAssetNodeKeyIsUniquePerRequestItem(t *testing.T) {
	batchID := uuid.NewString()
	first := derivedAssetNodeKey(batchID, uuid.NewString(), 1)
	second := derivedAssetNodeKey(batchID, uuid.NewString(), 2)
	if first == second || !strings.Contains(first, "/request/000001/") || !strings.Contains(second, "/request/000002/") {
		t.Fatalf("node keys are not ordinal/request unique: %q %q", first, second)
	}
}

func TestDerivedAssetSnapshotUsesWorkflowHashContract(t *testing.T) {
	snapshot := workflows.DerivedAssetRequirementSnapshot{
		ID: uuid.NewString(), ProjectID: uuid.NewString(), ProductionGenerationID: uuid.NewString(),
		StoryboardShotID: uuid.NewString(), CanonicalAssetID: uuid.NewString(),
		ReviewStatus: "approved", Status: "pending", Prompt: "frozen prompt",
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	raw, hash := derivedAssetSnapshot(snapshot)
	if hash != workflows.HashDerivedAssetSnapshot(snapshot) {
		t.Fatalf("hash = %s, want workflow hash", hash)
	}
	var decoded workflows.DerivedAssetRequirementSnapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != snapshot {
		t.Fatalf("decoded snapshot = %+v, want %+v", decoded, snapshot)
	}
}

func TestDerivedAssetLogicalProviderRequestHashIgnoresExecutionRouting(t *testing.T) {
	request := provider.GatewayImageRequest{
		OrganizationID: uuid.NewString(),
		ProjectID:      uuid.NewString(),
		WorkflowRunID:  uuid.NewString(),
		NodeRunID:      uuid.NewString(),
		Input:          json.RawMessage(`{"prompt":"frozen prompt"}`),
		IdempotencyKey: uuid.NewString(),
		Options:        provider.GatewayImageOptions{IdempotencyKey: uuid.NewString(), Retry: true},
	}
	want := derivedAssetLogicalProviderRequestHash(request)
	request.WorkflowRunID = uuid.NewString()
	request.NodeRunID = uuid.NewString()
	if got := derivedAssetLogicalProviderRequestHash(request); got != want {
		t.Fatalf("execution routing changed logical request hash: got %s, want %s", got, want)
	}
	request.Input = json.RawMessage(`{"prompt":"changed prompt"}`)
	if got := derivedAssetLogicalProviderRequestHash(request); got == want {
		t.Fatalf("business input did not change logical request hash: %s", got)
	}
}

func TestDerivedAssetBatchCommandPersistsFullWorksetAndRetry(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run derived asset batch API integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for derived asset batch API integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)
	var migrationReady bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.derived_asset_batches') IS NOT NULL`).Scan(&migrationReady); err != nil {
		t.Fatal(err)
	}
	if !migrationReady {
		t.Skip("migration 000043 is not applied in the integration database")
	}

	authService := auth.NewService(pool, "derived-asset-batch-api-test-secret", time.Hour, 24*time.Hour)
	vault, err := provider.NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	server := New(pool, authService, provider.NewService(pool, vault), nil, nil)
	server.temporal = &fakeTemporalClient{}
	handler := server.Handler()
	suffix := uuid.NewString()
	owner, err := authService.Register(ctx, auth.RegisterRequest{
		Email: "derived-assets-" + suffix + "@example.test", Username: "derived" + strings.ReplaceAll(suffix[:12], "-", ""),
		Password: "Password123!", DisplayName: "Derived Assets", OrganizationName: "Derived Assets " + suffix,
	}, httptest.NewRequest(http.MethodPost, "/api/auth/setup", nil))
	if err != nil {
		t.Fatalf("register isolated owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, owner.OrganizationID)
	})
	workspaceID := firstWorkspaceID(t, ctx, pool, owner.OrganizationID)
	var project Project
	doAPISuccess(t, handler, http.MethodPost, "/api/projects", owner.AccessToken, owner.OrganizationID, map[string]any{
		"workspaceId": workspaceID, "name": "Derived Asset Batch Project", "settings": map[string]any{},
	}, &project)
	seedDerivedAssetImageModel(t, ctx, pool, owner.OrganizationID, owner.User.ID, project.ImageModelProfileKey)
	ensureDerivedAssetPrompt(t, ctx, pool, owner.OrganizationID, owner.User.ID)

	assetID := seedDerivedAssetCanonicalAsset(t, ctx, pool, owner.OrganizationID, project.ID, owner.User.ID)
	workflowRunID, shotID := seedDerivedAssetShot(t, ctx, pool, project, owner.User.ID)
	executableID := seedDerivedAssetRequirement(t, ctx, pool, project, workflowRunID, shotID, assetID, "approved")
	directID := seedDerivedAssetRequirement(t, ctx, pool, project, workflowRunID, shotID, assetID, "approved")
	reviewRequiredID := seedDerivedAssetRequirement(t, ctx, pool, project, workflowRunID, shotID, assetID, "pending")
	missingID := uuid.NewString()
	principal := auth.Principal{UserID: owner.User.ID, OrganizationID: owner.OrganizationID}
	var direct DerivedAssetBatchCommandResult
	doAPISuccess(t, handler, http.MethodPost,
		"/api/projects/"+project.ID+"/shot-asset-requirements/"+directID+"/generate-image",
		owner.AccessToken, owner.OrganizationID, map[string]any{}, &direct)
	if direct.Batch.TotalItems != 1 || direct.Batch.ExecutableItems != 1 || len(direct.Batch.Items) != 1 || direct.Batch.Items[0].Execution == nil {
		t.Fatalf("single-item route did not create the durable v2 workset: %+v", direct.Batch)
	}
	if direct.WorkflowRun.WorkflowType != derivedAssetBatchWorkflowType {
		t.Fatalf("single-item route workflow type = %q, want %q", direct.WorkflowRun.WorkflowType, derivedAssetBatchWorkflowType)
	}
	var providerCallCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM provider_call_logs WHERE project_id = $1`, project.ID).Scan(&providerCallCount); err != nil {
		t.Fatal(err)
	}
	if providerCallCount != 0 {
		t.Fatalf("API command performed %d provider calls before Worker claim", providerCallCount)
	}

	result, err := server.createDerivedAssetBatchRun(ctx, principal, project, DerivedAssetBatchCreateOptions{
		Mode:           derivedAssetBatchModeExplicit,
		RequirementIDs: []string{executableID, reviewRequiredID, executableID, missingID},
		MaxConcurrency: 3, Force: true, ExpectedProjectRevision: project.Revision,
		IdempotencyKey: "derived-full-workset-" + suffix,
	})
	if err != nil {
		t.Fatalf("create derived asset batch: %v", err)
	}
	batch := result.Batch
	if batch.TotalItems != 4 || batch.ExecutableItems != 1 || batch.ReviewRequiredItems != 1 || batch.DuplicateItems != 1 || batch.NotFoundItems != 1 {
		t.Fatalf("batch counts = %+v", batch)
	}
	if len(batch.Items) != 4 || batch.Items[0].OriginalID != executableID || batch.Items[1].OriginalID != reviewRequiredID ||
		batch.Items[2].OriginalID != executableID || batch.Items[3].OriginalID != missingID {
		t.Fatalf("request order was not preserved: %#v", batch.Items)
	}
	if batch.Items[2].DuplicateOfRequestItemID == nil || *batch.Items[2].DuplicateOfRequestItemID != batch.Items[0].ID {
		t.Fatalf("duplicate lineage = %#v", batch.Items[2])
	}
	if batch.Items[0].Execution == nil || batch.Items[0].Execution.NodeRunID == "" || batch.Items[0].Execution.Status != "queued" {
		t.Fatalf("executable item = %#v", batch.Items[0])
	}
	var nodeCount, terminalNodeCount, executionCount, outboxCount int
	if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE status IN ('failed','skipped')) FROM workflow_node_runs WHERE workflow_run_id = $1`, result.WorkflowRun.ID).Scan(&nodeCount, &terminalNodeCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM derived_asset_execution_items WHERE batch_id = $1`, batch.ID).Scan(&executionCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_start_outbox WHERE workflow_run_id = $1`, result.WorkflowRun.ID).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if nodeCount != 4 || terminalNodeCount != 3 || executionCount != 1 || outboxCount != 1 {
		t.Fatalf("nodes=%d terminal=%d executions=%d outbox=%d", nodeCount, terminalNodeCount, executionCount, outboxCount)
	}
	assertDerivedAssetFrozenSnapshots(t, ctx, pool, batch.Items[0].Execution.ID, result.WorkflowRun.ID, batch.Items[0].Execution.NodeRunID)

	replay, err := server.createDerivedAssetBatchRun(ctx, principal, project, DerivedAssetBatchCreateOptions{
		Mode:           derivedAssetBatchModeExplicit,
		RequirementIDs: []string{executableID, reviewRequiredID, executableID, missingID},
		MaxConcurrency: 3, Force: true, ExpectedProjectRevision: project.Revision,
		IdempotencyKey: "derived-full-workset-" + suffix,
	})
	if err != nil {
		t.Fatalf("replay derived asset batch: %v", err)
	}
	if !replay.IdempotentReplay || replay.Batch.ID != batch.ID {
		t.Fatalf("replay = %+v", replay)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE shot_asset_requirements
		SET review_status = 'approved', updated_at = now()
		WHERE id = $1
	`, reviewRequiredID); err != nil {
		t.Fatalf("approve blocked requirement: %v", err)
	}
	retry, err := server.retryDerivedAssetBatchRun(ctx, principal, project, batch.ID, 2, "derived-retry-"+suffix)
	if err != nil {
		t.Fatalf("retry derived asset batch: %v", err)
	}
	if retry.Batch.TotalItems != 1 || retry.Batch.ExecutableItems != 1 || retry.Batch.RetryOfBatchID == nil || *retry.Batch.RetryOfBatchID != batch.ID {
		t.Fatalf("retry batch = %+v", retry.Batch)
	}
	if retry.Batch.Items[0].RetryOfRequestItemID == nil || retry.Batch.Items[0].Execution == nil || retry.Batch.Items[0].Execution.AttemptNo != 1 {
		t.Fatalf("retry item = %+v", retry.Batch.Items[0])
	}
	var retryOfAttemptID *string
	if err := pool.QueryRow(ctx, `SELECT retry_of_attempt_id::text FROM derived_asset_execution_items WHERE id = $1`, retry.Batch.Items[0].Execution.ID).Scan(&retryOfAttemptID); err != nil {
		t.Fatal(err)
	}
	if retryOfAttemptID != nil {
		t.Fatalf("blocked item without a prior attempt gained retry attempt lineage: %v", *retryOfAttemptID)
	}

	alreadyRunning, err := server.createDerivedAssetBatchRun(ctx, principal, project, DerivedAssetBatchCreateOptions{
		Mode: derivedAssetBatchModeExplicit, RequirementIDs: []string{directID},
		IdempotencyKey: "derived-already-running-" + suffix,
	})
	if err != nil {
		t.Fatalf("classify already-running requirement: %v", err)
	}
	if alreadyRunning.Batch.TotalItems != 1 || alreadyRunning.Batch.AlreadyRunningItems != 1 ||
		alreadyRunning.Batch.Items[0].Disposition != "already_running" ||
		stringValue(alreadyRunning.Batch.Items[0].ErrorCode) != "DERIVED_ASSET_ALREADY_RUNNING" {
		t.Fatalf("already-running classification = %+v", alreadyRunning.Batch)
	}

	selectAll, err := server.createDerivedAssetBatchRun(ctx, principal, project, DerivedAssetBatchCreateOptions{
		Mode: derivedAssetBatchModeSelectAll,
		Filters: DerivedAssetBatchFilters{
			ReviewStatuses: []string{"approved"},
		},
		IdempotencyKey: "derived-select-all-" + suffix,
	})
	if err != nil {
		t.Fatalf("create select-all audit batch: %v", err)
	}
	if selectAll.Batch.RequestMode != derivedAssetBatchModeSelectAll || selectAll.Batch.SelectorCandidateCount != 3 ||
		selectAll.Batch.SelectorSkippedCount != 3 || selectAll.Batch.TotalItems != 3 || selectAll.Batch.AlreadyRunningItems != 3 ||
		len(selectAll.Batch.Filters) == 0 || selectAll.Batch.FiltersHash == "" {
		t.Fatalf("select-all audit projection = %+v", selectAll.Batch)
	}

	var otherProject Project
	doAPISuccess(t, handler, http.MethodPost, "/api/projects", owner.AccessToken, owner.OrganizationID, map[string]any{
		"workspaceId": workspaceID, "name": "Other Derived Asset Project", "settings": map[string]any{},
	}, &otherProject)
	otherAssetID := seedDerivedAssetCanonicalAsset(t, ctx, pool, owner.OrganizationID, otherProject.ID, owner.User.ID)
	otherWorkflowRunID, otherShotID := seedDerivedAssetShot(t, ctx, pool, otherProject, owner.User.ID)
	otherRequirementID := seedDerivedAssetRequirement(t, ctx, pool, otherProject, otherWorkflowRunID, otherShotID, otherAssetID, "approved")

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin production generation reset: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE derived_asset_execution_items
		SET status = 'cancelled', revision = revision + 1,
		    error_code = 'TEST_GENERATION_SWITCH', error_message = 'test fixture generation switch',
		    lease_owner = NULL, lease_token = NULL, lease_expires_at = NULL, heartbeat_at = NULL,
		    completed_at = now()
		WHERE project_id = $1
		  AND status IN ('prepared', 'queued', 'leased', 'provider_running', 'transferring', 'committing', 'unknown_outcome')
	`, project.ID); err != nil {
		tx.Rollback(ctx)
		t.Fatalf("terminalize API test executions before generation reset: %v", err)
	}
	if _, _, err := videoproduction.ResetActiveGeneration(ctx, tx, project.ID); err != nil {
		tx.Rollback(ctx)
		t.Fatalf("reset active generation: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit production generation reset: %v", err)
	}

	classified, err := server.createDerivedAssetBatchRun(ctx, principal, project, DerivedAssetBatchCreateOptions{
		Mode: derivedAssetBatchModeExplicit,
		RequirementIDs: []string{
			executableID,
			missingID,
			otherRequirementID,
		},
		IdempotencyKey: "derived-identity-dispositions-" + suffix,
	})
	if err != nil {
		t.Fatalf("classify generation and project boundaries: %v", err)
	}
	if classified.Batch.TotalItems != 3 || classified.Batch.GenerationMismatchItems != 1 || classified.Batch.NotFoundItems != 2 {
		t.Fatalf("identity disposition counts = %+v", classified.Batch)
	}
	if classified.Batch.Items[0].Disposition != "generation_mismatch" ||
		stringValue(classified.Batch.Items[0].ErrorCode) != "PRODUCTION_GENERATION_MISMATCH" {
		t.Fatalf("old-generation item = %+v", classified.Batch.Items[0])
	}
	for _, item := range classified.Batch.Items[1:] {
		if item.Disposition != "not_found" || stringValue(item.ErrorCode) != "DERIVED_ASSET_REQUIREMENT_NOT_FOUND" || item.RequirementID != nil {
			t.Fatalf("not-found boundary item = %+v", item)
		}
	}
	var finalProviderCallCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM provider_call_logs WHERE project_id = $1`, project.ID).Scan(&finalProviderCallCount); err != nil {
		t.Fatal(err)
	}
	if finalProviderCallCount != 0 {
		t.Fatalf("command classification performed %d provider calls", finalProviderCallCount)
	}
}

func seedDerivedAssetImageModel(t *testing.T, ctx context.Context, pool dbQueryer, organizationID, userID, profileKey string) {
	t.Helper()
	var connectorID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM provider_connectors ORDER BY is_official DESC, created_at LIMIT 1`).Scan(&connectorID); err != nil {
		t.Fatal(err)
	}
	var accountID, modelID, profileID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_accounts(organization_id, connector_id, name, base_url, auth_type, status, created_by)
		VALUES ($1, $2, 'Derived Asset Test', 'https://example.invalid/v1', 'none', 'active', $3)
		RETURNING id::text
	`, organizationID, connectorID, userID).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status)
		VALUES ($1, 'derived-image-test', 'Derived Image Test', 'image', 'active') RETURNING id::text
	`, accountID).Scan(&modelID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_model_capabilities(
			provider_model_id, task_types, input_limits, output_limits, quality_tiers,
			provider_options_schema, pricing_policy
		) VALUES ($1, '["image.generate"]', '{"referenceImageCount":1}', '{"formats":["png"]}', '["high"]', '{}', '{}')
	`, modelID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO model_profiles(organization_id, profile_key, name, purpose)
		VALUES ($1, $2, 'Derived Image Test', 'derived asset image integration test')
		ON CONFLICT (organization_id, profile_key) DO UPDATE SET updated_at = now()
		RETURNING id::text
	`, organizationID, profileKey).Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO model_profile_bindings(model_profile_id, provider_model_id, priority, weight, enabled)
		VALUES ($1, $2, 1, 100, true)
	`, profileID, modelID); err != nil {
		t.Fatal(err)
	}
}

func ensureDerivedAssetPrompt(t *testing.T, ctx context.Context, pool dbQueryer, organizationID, userID string) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM prompt_templates template
		  JOIN prompt_versions version ON template.id = COALESCE(version.template_id, version.prompt_template_id)
		  WHERE (template.organization_id IS NULL OR template.organization_id = $1)
		    AND template.template_key = 'derived_asset_image_prompt'
		    AND template.status = 'active' AND version.status = 'active'
		)
	`, organizationID).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		var templateID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO prompt_templates(
				organization_id, template_key, name, purpose, description, modality,
				task_type, scope, status, is_system, created_by
			)
			VALUES ($1, 'derived_asset_image_prompt', 'Derived Asset Image',
			        'integration test', 'integration test', 'image', 'image.generate',
			        'organization', 'active', false, $2)
			RETURNING id::text
		`, organizationID, userID).Scan(&templateID); err != nil {
			t.Fatal(err)
		}
		content := "{{baseAsset.name}}\n{{shot.summary}}\n{{requirement.summary}}"
		if _, err := pool.Exec(ctx, `
			INSERT INTO prompt_versions(
				prompt_template_id, template_id, version_no, version, content,
				variables_schema, content_hash, status, title, content_format,
				metadata, activated_at, created_by
			)
			VALUES ($1, $1, 1, 1, $2, '{}', $3, 'active', 'active', 'text', '{}', now(), $4)
		`, templateID, content, workflows.HashDerivedAssetSnapshot(content), userID); err != nil {
			t.Fatal(err)
		}
	}
}

func seedDerivedAssetCanonicalAsset(t *testing.T, ctx context.Context, pool dbQueryer, organizationID, projectID, userID string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `
		INSERT INTO canonical_assets(
			organization_id, project_id, asset_type, name, description, base_prompt,
			visual_traits, primary_reference_storage_key, lock_reference, status,
			review_status, source_script_ids, metadata, created_by
		)
		VALUES ($1, $2, 'character', '主角', '主角设定', '完整角色提示词', '{}',
		        'assets/reference.png', true, 'prompt_ready', 'approved', '[]', '{}', $3)
		RETURNING id::text
	`, organizationID, projectID, userID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedDerivedAssetShot(t *testing.T, ctx context.Context, pool dbQueryer, project Project, userID string) (string, string) {
	t.Helper()
	var workflowRunID, shotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workflow_runs(
			organization_id, project_id, temporal_workflow_id, workflow_type, status,
			input, output, created_by, production_generation_id,
			video_production_binding_id, video_production_binding_revision
		)
		VALUES ($1, $2, $3, 'script_to_storyboard', 'succeeded', '{}', '{}', $4, $5, $6, $7)
		RETURNING id::text
	`, project.OrganizationID, project.ID, "derived-source-"+uuid.NewString(), userID,
		project.ProductionGeneration.ID, project.VideoProductionBinding.ID, project.VideoProductionBinding.Revision).Scan(&workflowRunID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, workflow_run_id, shot_index, shot_no,
			start_tick, end_tick, duration_min_ticks, duration_max_ticks,
			visual, camera, motion, mood, image_prompt, video_prompt,
			status, review_status, metadata, production_generation_id
		)
		VALUES ($1, $2, $3, 0, 1, 0, 450000, 450000, 450000,
		        '角色站在庭院', '中景', '衣摆轻动', '克制', 'image prompt', 'video prompt',
		        'storyboard_ready', 'approved', '{}', $4)
		RETURNING id::text
	`, project.OrganizationID, project.ID, workflowRunID, project.ProductionGeneration.ID).Scan(&shotID); err != nil {
		t.Fatal(err)
	}
	return workflowRunID, shotID
}

func seedDerivedAssetRequirement(t *testing.T, ctx context.Context, pool dbQueryer, project Project, workflowRunID, shotID, assetID, reviewStatus string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `
		INSERT INTO shot_asset_requirements(
			organization_id, project_id, workflow_run_id, storyboard_shot_id, asset_id,
			requirement_type, role_in_shot, prompt, status, review_status, metadata,
			production_generation_id
		)
		VALUES ($1, $2, $3, $4, $5, 'character_appearance', 'foreground',
		        'frozen requirement prompt', 'pending', $6, '{}', $7)
		RETURNING id::text
	`, project.OrganizationID, project.ID, workflowRunID, shotID, assetID, reviewStatus, project.ProductionGeneration.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func assertDerivedAssetFrozenSnapshots(t *testing.T, ctx context.Context, pool dbQueryer, executionID, workflowRunID, nodeRunID string) {
	t.Helper()
	var requirementRaw, shotRaw, assetRaw, promptRaw, referenceRaw, modelRaw, capabilityRaw, requestRaw []byte
	if err := pool.QueryRow(ctx, `
		SELECT requirement_snapshot, storyboard_shot_snapshot, canonical_asset_snapshot,
		       prompt_snapshot, reference_snapshot, model_snapshot, capability_snapshot, request_snapshot
		FROM derived_asset_execution_items WHERE id = $1
	`, executionID).Scan(&requirementRaw, &shotRaw, &assetRaw, &promptRaw, &referenceRaw, &modelRaw, &capabilityRaw, &requestRaw); err != nil {
		t.Fatal(err)
	}
	var requirement workflows.DerivedAssetRequirementSnapshot
	var shot workflows.DerivedAssetStoryboardShotSnapshot
	var asset workflows.DerivedAssetCanonicalAssetSnapshot
	var prompt workflows.DerivedAssetPromptSnapshot
	var references workflows.DerivedAssetReferenceSnapshot
	var model workflows.DerivedAssetModelSnapshot
	var capability workflows.DerivedAssetCapabilitySnapshot
	var request provider.GatewayImageRequest
	for _, target := range []struct {
		raw    []byte
		target any
	}{
		{requirementRaw, &requirement}, {shotRaw, &shot}, {assetRaw, &asset}, {promptRaw, &prompt},
		{referenceRaw, &references}, {modelRaw, &model}, {capabilityRaw, &capability}, {requestRaw, &request},
	} {
		if err := json.Unmarshal(target.raw, target.target); err != nil {
			t.Fatalf("decode frozen snapshot: %v", err)
		}
	}
	if requirement.ID == "" || shot.ID == "" || asset.ID == "" || prompt.Text == "" || len(references.Items) != 1 {
		t.Fatalf("incomplete snapshots: requirement=%+v shot=%+v asset=%+v prompt=%+v references=%+v", requirement, shot, asset, prompt, references)
	}
	if model.ProviderModelID == "" || len(capability.TaskTypes) == 0 || request.ProviderModelID != model.ProviderModelID || request.WorkflowRunID != workflowRunID {
		t.Fatalf("model/capability/request snapshots: model=%+v capability=%s request=%+v", model, capability.TaskTypes, request)
	}
	if request.NodeRunID != "" {
		t.Fatalf("frozen request nodeRunId = %q, want empty for workflow CAS fill", request.NodeRunID)
	}
	var frozenNodeRunID string
	if err := pool.QueryRow(ctx, `SELECT node_run_id::text FROM derived_asset_execution_items WHERE id = $1`, executionID).Scan(&frozenNodeRunID); err != nil {
		t.Fatal(err)
	}
	if frozenNodeRunID != nodeRunID {
		t.Fatalf("execution nodeRunId = %s, want %s", frozenNodeRunID, nodeRunID)
	}
}
