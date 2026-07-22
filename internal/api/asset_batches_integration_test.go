package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAssetBatchCreateRetryAndRevisionConflict(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run asset batch API integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for asset batch API integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)
	authService := auth.NewService(pool, "asset-batch-api-test-secret", time.Hour, 24*time.Hour)
	vault, err := provider.NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	server := New(pool, authService, provider.NewService(pool, vault), nil, nil)
	server.temporal = &fakeTemporalClient{}
	handler := server.Handler()
	suffix := uuid.NewString()
	owner, err := authService.Register(ctx, auth.RegisterRequest{
		Email: "asset-batch-" + suffix + "@example.test", Username: randomStorageSegment(), Password: "Password123!", DisplayName: "Asset Batch",
		OrganizationName: "Asset Batch Org " + suffix,
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, owner.OrganizationID)
	})
	workspaceID := firstWorkspaceID(t, ctx, pool, owner.OrganizationID)
	var project Project
	doAPISuccess(t, handler, http.MethodPost, "/api/projects", owner.AccessToken, owner.OrganizationID, map[string]any{
		"workspaceId": workspaceID, "name": "Asset Batch Project", "settings": map[string]any{},
	}, &project)
	if project.Revision < 1 {
		t.Fatalf("project revision = %d", project.Revision)
	}
	initialRevision := project.Revision
	var updatedProject Project
	doAPISuccess(t, handler, http.MethodPatch, "/api/projects/"+project.ID, owner.AccessToken, owner.OrganizationID, map[string]any{
		"name": "Asset Batch Project Updated", "expectedRevision": initialRevision,
	}, &updatedProject)
	if updatedProject.Revision <= initialRevision {
		t.Fatalf("project revision did not advance: initial=%d updated=%d", initialRevision, updatedProject.Revision)
	}
	assertAPIErrorCode(t, handler, http.MethodPatch, "/api/projects/"+project.ID, owner.AccessToken, owner.OrganizationID, map[string]any{
		"description": "stale update", "expectedRevision": initialRevision,
	}, http.StatusConflict, "PROJECT_REVISION_CONFLICT")
	project = updatedProject

	assetIDs := make([]string, 0, 2)
	for index, name := range []string{"主角", "山门"} {
		assetType := "character"
		if index == 1 {
			assetType = "scene"
		}
		var assetID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO canonical_assets(
				organization_id, project_id, asset_type, name, description, status,
				profile, visual_traits, source_script_ids, metadata, created_by
			)
			VALUES ($1, $2, $3, $4, $5, 'draft', '{}', '{}', '[]', '{}', $6)
			RETURNING id::text
		`, owner.OrganizationID, project.ID, assetType, name, name+"描述", owner.User.ID).Scan(&assetID); err != nil {
			t.Fatalf("insert canonical asset: %v", err)
		}
		assetIDs = append(assetIDs, assetID)
	}
	seedAssetBatchModelBinding(t, ctx, pool, owner.OrganizationID, owner.User.ID, project.ScriptModelProfileKey)

	createBody := map[string]any{
		"operation": "generate_prompts", "assetIds": assetIDs, "maxConcurrency": 2,
		"force": true, "expectedProjectRevision": project.Revision,
	}
	var original WorkflowRun
	doAPISuccess(t, handler, http.MethodPost, "/api/projects/"+project.ID+"/asset-batches", owner.AccessToken, owner.OrganizationID, createBody, &original)
	if original.Status != "queued" || original.WorkflowType != "batch_generate_asset_cards" || original.TotalItems != 2 {
		t.Fatalf("created workflow = %+v", original)
	}
	var nodeCount, outboxCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_node_runs WHERE workflow_run_id = $1 AND status = 'queued'`, original.ID).Scan(&nodeCount); err != nil {
		t.Fatalf("count workflow nodes: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_start_outbox WHERE workflow_run_id = $1 AND status = 'pending'`, original.ID).Scan(&outboxCount); err != nil {
		t.Fatalf("count workflow start outbox: %v", err)
	}
	if nodeCount != 2 || outboxCount != 1 {
		t.Fatalf("nodes=%d outbox=%d, want 2 and 1", nodeCount, outboxCount)
	}
	var snapshotRevision int64
	var snapshotJSON []byte
	var snapshotHash string
	if err := pool.QueryRow(ctx, `
		SELECT project_revision, snapshot, snapshot_hash
		FROM workflow_input_snapshots
		WHERE workflow_run_id = $1
	`, original.ID).Scan(&snapshotRevision, &snapshotJSON, &snapshotHash); err != nil {
		t.Fatalf("load durable workflow snapshot: %v", err)
	}
	var durableSnapshot workflows.AssetBatchWorkflowInput
	if err := json.Unmarshal(snapshotJSON, &durableSnapshot); err != nil {
		t.Fatalf("decode durable workflow snapshot: %v", err)
	}
	if snapshotRevision != project.Revision || durableSnapshot.Project.Revision != project.Revision || len(snapshotHash) != 64 {
		t.Fatalf("snapshot revision=%d projectRevision=%d hash=%q", snapshotRevision, durableSnapshot.Project.Revision, snapshotHash)
	}

	conflictBody := map[string]any{
		"operation": "generate_prompts", "assetIds": assetIDs, "expectedProjectRevision": project.Revision + 1,
	}
	assertAPIErrorCode(t, handler, http.MethodPost, "/api/projects/"+project.ID+"/asset-batches", owner.AccessToken, owner.OrganizationID, conflictBody, http.StatusConflict, "PROJECT_REVISION_CONFLICT")

	if _, err := pool.Exec(ctx, `
		UPDATE workflow_node_runs
		SET status = CASE WHEN input->>'assetId' = $2 THEN 'failed' ELSE 'succeeded' END,
		    error_code = CASE WHEN input->>'assetId' = $2 THEN 'PROVIDER_REJECTED' ELSE NULL END,
		    error_message = CASE WHEN input->>'assetId' = $2 THEN 'image rejected' ELSE NULL END,
		    completed_at = now(), updated_at = now()
		WHERE workflow_run_id = $1
	`, original.ID, assetIDs[1]); err != nil {
		t.Fatalf("complete original nodes: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'partial_succeeded', completed_items = 1, failed_items = 1, completed_at = now(), updated_at = now()
		WHERE id = $1
	`, original.ID); err != nil {
		t.Fatalf("complete original workflow: %v", err)
	}

	var retry WorkflowRun
	doAPISuccess(t, handler, http.MethodPost, "/api/workflow-runs/"+original.ID+"/retry-failed", owner.AccessToken, owner.OrganizationID, map[string]any{
		"expectedProjectRevision": project.Revision, "maxConcurrency": 2,
	}, &retry)
	if retry.Status != "queued" || retry.TotalItems != 1 || retry.RetryOfWorkflowRunID == nil || *retry.RetryOfWorkflowRunID != original.ID {
		t.Fatalf("retry workflow = %+v", retry)
	}
	if retry.RootWorkflowRunID == nil || *retry.RootWorkflowRunID != original.ID {
		t.Fatalf("retry root = %#v, want %s", retry.RootWorkflowRunID, original.ID)
	}
	var retryAssetID string
	if err := pool.QueryRow(ctx, `SELECT input->>'assetId' FROM workflow_node_runs WHERE workflow_run_id = $1`, retry.ID).Scan(&retryAssetID); err != nil {
		t.Fatalf("load retry node: %v", err)
	}
	if retryAssetID != assetIDs[1] {
		t.Fatalf("retry asset = %s, want %s", retryAssetID, assetIDs[1])
	}
}

func TestSourceToScriptRetryCreatesNewGenerationForFailedEpisodes(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run source-to-script retry integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for source-to-script retry integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)
	authService := auth.NewService(pool, "source-script-retry-test-secret", time.Hour, 24*time.Hour)
	vault, err := provider.NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	server := New(pool, authService, provider.NewService(pool, vault), nil, nil)
	server.temporal = &fakeTemporalClient{}
	handler := server.Handler()
	suffix := uuid.NewString()
	owner, err := authService.Register(ctx, auth.RegisterRequest{
		Email: "source-script-retry-" + suffix + "@example.test", Username: randomStorageSegment(), Password: "Password123!", DisplayName: "Source Script Retry",
		OrganizationName: "Source Script Retry Org " + suffix,
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, owner.OrganizationID)
	})
	workspaceID := firstWorkspaceID(t, ctx, pool, owner.OrganizationID)
	var project Project
	doAPISuccess(t, handler, http.MethodPost, "/api/projects", owner.AccessToken, owner.OrganizationID, map[string]any{
		"workspaceId": workspaceID, "name": "Source Script Retry Project", "settings": map[string]any{},
	}, &project)
	sourceID := uuid.NewString()
	scriptID := uuid.NewString()
	versionID := uuid.NewString()
	generationID := uuid.NewString()
	chapterIDs := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	var sourceRevision, scriptRevision int64
	var sourceHash string
	if err := pool.QueryRow(ctx, `
		INSERT INTO project_sources(
			id, organization_id, project_id, source_type, title, content, content_format, status, created_by
		)
		VALUES ($1, $2, $3, 'novel', 'Retry Novel', 'chapter one\nchapter seventy-four\nchapter one hundred twenty', 'plain_text', 'processed', $4)
		RETURNING content_revision, content_hash
	`, sourceID, owner.OrganizationID, project.ID, owner.User.ID).Scan(&sourceRevision, &sourceHash); err != nil {
		t.Fatalf("insert retry source: %v", err)
	}
	manifestOrdinals := []int{1, 74, 120}
	for index, chapterID := range chapterIDs {
		if _, err := pool.Exec(ctx, `
			INSERT INTO novel_chapters(
				id, organization_id, project_id, source_id, chapter_index, volume_index, section_index,
				volume_title, chapter_title, content, event_state
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'completed')
		`, chapterID, owner.OrganizationID, project.ID, sourceID, manifestOrdinals[index], index+1, manifestOrdinals[index],
			fmt.Sprintf("第%d卷", index+1), fmt.Sprintf("第%d节", manifestOrdinals[index]), fmt.Sprintf("source chapter %d", manifestOrdinals[index])); err != nil {
			t.Fatalf("insert retry chapter %d: %v", index, err)
		}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO scripts(id, organization_id, project_id, source_id, title, status, created_by)
		VALUES ($1, $2, $3, $4, 'Retry Script', 'active', $5)
	`, scriptID, owner.OrganizationID, project.ID, sourceID, owner.User.ID); err != nil {
		t.Fatalf("insert retry script: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO script_versions(
			id, organization_id, project_id, script_id, version_no, version, content,
			content_format, status, source_type, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, 1, 1, 'base script', 'markdown', 'active', 'agent_generated', '{}', $5)
	`, versionID, owner.OrganizationID, project.ID, scriptID, owner.User.ID); err != nil {
		t.Fatalf("insert retry script version: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		UPDATE scripts SET current_version_id = $2 WHERE id = $1 RETURNING revision
	`, scriptID, versionID).Scan(&scriptRevision); err != nil {
		t.Fatalf("activate retry script version: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE projects SET active_script_id = $2 WHERE id = $1`, project.ID, scriptID); err != nil {
		t.Fatalf("set retry active script: %v", err)
	}
	plan := workflows.SourceToScriptPlan{
		GenerationID: generationID, RootGenerationID: generationID, AttemptGeneration: 1,
		SourceID: sourceID, SourceType: "novel", SourceTitle: "Retry Novel", ScriptID: scriptID,
		BaseScriptVersionID: versionID, PreviousScriptVersionID: versionID,
		ExpectedScriptRevision: scriptRevision, Title: "Retry Script", EpisodeTotal: 3, SeriesEpisodeTotal: 120,
		Chapters: []workflows.SourceToScriptChapterRef{
			{ID: chapterIDs[0], ItemKey: chapterIDs[0], ManifestOrdinal: 1, ChapterIndex: 1, Title: "第一节"},
			{ID: chapterIDs[1], ItemKey: chapterIDs[1], ManifestOrdinal: 74, ChapterIndex: 74, Title: "第七十四节"},
			{ID: chapterIDs[2], ItemKey: chapterIDs[2], ManifestOrdinal: 120, ChapterIndex: 120, Title: "第一百二十节"},
		},
	}
	options := workflows.SourceToScriptOptions{SourceID: sourceID, ChapterIDs: chapterIDs, Instruction: "faithful", MaxConcurrency: 2}
	originalInput := mustRawJSON(map[string]any{
		"prompt": "source_to_script", "workflowType": "source_to_script", "input": mustRawJSON(options),
	})
	var originalID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workflow_runs(
			organization_id, project_id, temporal_workflow_id, workflow_type, status,
			input, output, created_by, total_items, completed_items, failed_items, attempt_generation,
			completed_at, terminalized_at, settled_at, production_generation_id,
			video_production_binding_id, video_production_binding_revision
		)
		VALUES ($1, $2, 'source-script-retry-original-' || gen_random_uuid()::text, 'source_to_script', 'partial_succeeded',
		        $3, '{}', $4, 3, 2, 1, 1, now(), now(), now(), $5, $6, $7)
		RETURNING id::text
	`, owner.OrganizationID, project.ID, originalInput, owner.User.ID,
		project.ProductionGeneration.ID, project.VideoProductionBinding.ID, project.VideoProductionBinding.Revision).Scan(&originalID); err != nil {
		t.Fatalf("insert original workflow: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_runs SET root_workflow_run_id = id WHERE id = $1`, originalID); err != nil {
		t.Fatalf("set original root: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO source_to_script_generations(
			id, organization_id, project_id, workflow_run_id, attempt_generation,
			source_id, source_type, source_revision, source_content_hash, source_snapshot_hash,
			script_id, expected_active_script_id, expected_current_version_id, expected_script_revision,
			base_script_version_id, prompt_template_key, prompt_content_hash, model_profile_key,
			project_snapshot, manual_bindings, model_bindings, manifest, manifest_hash,
			status, idempotency_key, created_by
		)
		VALUES (
			$1, $2, $3, $4, 1,
			$5, 'novel', $6, $7, $8,
			$9, $9, $10, $11,
			$10, 'script_agent_generate', $12, 'script_default',
			'{}', '[]', '[]', $13, $14,
			'partial_succeeded', 'retry-fixture', $15
		)
	`, generationID, owner.OrganizationID, project.ID, originalID, sourceID, sourceRevision, sourceHash,
		strings.Repeat("a", 64), scriptID, versionID, scriptRevision, "sha256:"+strings.Repeat("b", 64),
		mustRawJSON(map[string]any{"schemaVersion": 1, "sourceId": sourceID, "scriptId": scriptID}),
		strings.Repeat("c", 64), owner.User.ID); err != nil {
		t.Fatalf("insert source-to-script generation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO workflow_node_runs(
			organization_id, project_id, workflow_run_id, node_key, node_type, status,
			input, output, attempt_generation, started_at, completed_at, production_generation_id
		)
		VALUES ($1, $2, $3, $4, 'workflow.script_prepare', 'succeeded', '{}', $5, 1, now(), now(), $6)
	`, owner.OrganizationID, project.ID, originalID, workflows.SourceToScriptPrepareNodeKey, mustRawJSON(plan), project.ProductionGeneration.ID); err != nil {
		t.Fatalf("insert prepare node: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO script_episode_generation_results(
			organization_id, project_id, workflow_run_id, generation_id, attempt_generation,
			source_id, source_chapter_id, item_key, status, error_code, error_message, provenance
		)
		VALUES ($1, $2, $3, $4, 1, $5, $6, $7, 'failed', 'UPSTREAM_TIMEOUT', 'episode timed out', '{}')
	`, owner.OrganizationID, project.ID, originalID, generationID, sourceID, chapterIDs[1], chapterIDs[1]); err != nil {
		t.Fatalf("insert failed staging result: %v", err)
	}
	var retry WorkflowRun
	doAPISuccess(t, handler, http.MethodPost, "/api/workflow-runs/"+originalID+"/retry-failed", owner.AccessToken, owner.OrganizationID, map[string]any{
		"expectedProjectRevision": project.Revision, "maxConcurrency": 3, "idempotencyKey": "retry-episode-2-" + suffix,
	}, &retry)
	if retry.WorkflowType != "source_to_script" || retry.Status != "queued" || retry.TotalItems != 1 || retry.AttemptGeneration != 2 {
		t.Fatalf("retry workflow = %+v", retry)
	}
	if retry.RootWorkflowRunID == nil || *retry.RootWorkflowRunID != originalID || retry.RetryOfWorkflowRunID == nil || *retry.RetryOfWorkflowRunID != originalID {
		t.Fatalf("retry chain root=%v retryOf=%v", retry.RootWorkflowRunID, retry.RetryOfWorkflowRunID)
	}
	var retryEpisodeNodeCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM workflow_node_runs
		WHERE workflow_run_id = $1 AND node_type = $2
	`, retry.ID, workflows.SourceToScriptEpisodeNodeType).Scan(&retryEpisodeNodeCount); err != nil {
		t.Fatalf("count retry episode nodes: %v", err)
	}
	if retryEpisodeNodeCount != 0 {
		t.Fatalf("retry episode node count = %d, want 0 before the new prepare snapshot", retryEpisodeNodeCount)
	}
	var snapshot []byte
	if err := pool.QueryRow(ctx, `SELECT snapshot FROM workflow_input_snapshots WHERE workflow_run_id = $1`, retry.ID).Scan(&snapshot); err != nil {
		t.Fatalf("load retry snapshot: %v", err)
	}
	var startInput workflows.TextToStoryboardInput
	if err := json.Unmarshal(snapshot, &startInput); err != nil {
		t.Fatalf("decode retry snapshot: %v", err)
	}
	if startInput.SourceToScriptState == nil || startInput.SourceToScriptState.Initialized || startInput.SourceToScriptState.AttemptGeneration != 2 {
		t.Fatalf("retry state = %+v", startInput.SourceToScriptState)
	}
	var retryOptions workflows.SourceToScriptOptions
	if err := json.Unmarshal(startInput.Input, &retryOptions); err != nil {
		t.Fatalf("decode retry options: %v", err)
	}
	if len(retryOptions.ChapterIDs) != 1 || retryOptions.ChapterIDs[0] != chapterIDs[1] || retryOptions.TargetScriptID != scriptID {
		t.Fatalf("retry options = %+v, want exact failed chapter %s", retryOptions, chapterIDs[1])
	}
	var operationStatus, idempotencyStatus string
	if err := pool.QueryRow(ctx, `
		SELECT operation.status, key.status
		FROM runtime_operations operation
		JOIN idempotency_keys key ON key.operation_id = operation.id
		WHERE operation.workflow_run_id = $1
	`, retry.ID).Scan(&operationStatus, &idempotencyStatus); err != nil {
		t.Fatalf("load retry operation: %v", err)
	}
	if operationStatus != "succeeded" || idempotencyStatus != "succeeded" {
		t.Fatalf("operation=%s idempotency=%s", operationStatus, idempotencyStatus)
	}
	var replay WorkflowRun
	doAPISuccess(t, handler, http.MethodPost, "/api/workflow-runs/"+originalID+"/retry-failed", owner.AccessToken, owner.OrganizationID, map[string]any{
		"expectedProjectRevision": project.Revision, "maxConcurrency": 3, "idempotencyKey": "retry-episode-2-" + suffix,
	}, &replay)
	if replay.ID != retry.ID || replay.AttemptGeneration != retry.AttemptGeneration {
		t.Fatalf("idempotent replay = %+v, want workflow %s generation %d", replay, retry.ID, retry.AttemptGeneration)
	}
	var retryRunCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_runs WHERE retry_of_workflow_run_id = $1`, originalID).Scan(&retryRunCount); err != nil {
		t.Fatalf("count retry runs: %v", err)
	}
	if retryRunCount != 1 {
		t.Fatalf("retry run count = %d, want 1", retryRunCount)
	}
}

func TestAssetBatchSnapshotSerializesConcurrentPromptManualModelAndRatioMutation(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run asset batch API integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for asset batch API integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)
	authService := auth.NewService(pool, "asset-batch-snapshot-test-secret", time.Hour, 24*time.Hour)
	vault, err := provider.NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	server := New(pool, authService, provider.NewService(pool, vault), nil, nil)
	server.temporal = &fakeTemporalClient{}
	handler := server.Handler()
	suffix := uuid.NewString()
	owner, err := authService.Register(ctx, auth.RegisterRequest{
		Email: "asset-snapshot-" + suffix + "@example.test", Username: randomStorageSegment(), Password: "Password123!", DisplayName: "Asset Snapshot",
		OrganizationName: "Asset Snapshot Org " + suffix,
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, owner.OrganizationID)
	})
	workspaceID := firstWorkspaceID(t, ctx, pool, owner.OrganizationID)
	var project Project
	doAPISuccess(t, handler, http.MethodPost, "/api/projects", owner.AccessToken, owner.OrganizationID, map[string]any{
		"workspaceId": workspaceID, "name": "Snapshot Serialization", "settings": map[string]any{},
	}, &project)
	seedAssetBatchModelBinding(t, ctx, pool, owner.OrganizationID, owner.User.ID, project.ScriptModelProfileKey)

	var assetID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO canonical_assets(
			organization_id, project_id, asset_type, name, description, status,
			profile, visual_traits, source_script_ids, metadata, created_by
		)
		VALUES ($1, $2, 'character', '并发快照角色', '并发快照角色描述', 'draft', '{}', '{}', '[]', '{}', $3)
		RETURNING id::text
	`, owner.OrganizationID, project.ID, owner.User.ID).Scan(&assetID); err != nil {
		t.Fatalf("insert canonical asset: %v", err)
	}
	oldPromptVersionID, newPromptVersionID := seedSnapshotPromptTemplate(t, ctx, pool, owner.OrganizationID, owner.User.ID, "asset_card_generation", "old card prompt", "new card prompt")
	oldManualVersionID, newManualVersionID := seedSnapshotPromptTemplate(t, ctx, pool, owner.OrganizationID, owner.User.ID, "snapshot_visual_manual_"+suffix, "old visual manual", "new visual manual")
	if _, err := pool.Exec(ctx, `UPDATE prompt_bindings SET status = 'disabled' WHERE project_id = $1 AND template_key = 'asset_card_generation' AND status = 'active'`, project.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO prompt_bindings(organization_id, project_id, template_key, prompt_version_id, status, created_by)
		VALUES ($1, $2, 'asset_card_generation', $3, 'active', $4)
	`, owner.OrganizationID, project.ID, oldPromptVersionID, owner.User.ID); err != nil {
		t.Fatalf("bind old prompt version: %v", err)
	}
	manualTag, err := pool.Exec(ctx, `
		UPDATE project_manual_bindings
		SET prompt_version_id = $2, updated_at = now()
		WHERE project_id = $1 AND manual_kind = 'visual' AND status = 'active'
	`, project.ID, oldManualVersionID)
	if err != nil {
		t.Fatalf("bind old visual manual: %v", err)
	}
	if manualTag.RowsAffected() == 0 {
		if _, err := pool.Exec(ctx, `
			INSERT INTO project_manual_bindings(organization_id, project_id, manual_kind, prompt_version_id, status, created_by)
			VALUES ($1, $2, 'visual', $3, 'active', $4)
		`, owner.OrganizationID, project.ID, oldManualVersionID, owner.User.ID); err != nil {
			t.Fatalf("insert old visual manual binding: %v", err)
		}
	}
	var modelBindingID string
	var oldPriority int
	if err := pool.QueryRow(ctx, `
		SELECT b.id::text, b.priority
		FROM model_profile_bindings b
		JOIN model_profiles p ON p.id = b.model_profile_id
		WHERE p.organization_id = $1 AND p.profile_key = $2 AND b.enabled = true
		ORDER BY b.priority, b.id LIMIT 1
	`, owner.OrganizationID, project.ScriptModelProfileKey).Scan(&modelBindingID, &oldPriority); err != nil {
		t.Fatalf("load old model binding: %v", err)
	}
	var oldRevision int64
	if err := pool.QueryRow(ctx, `
		UPDATE projects SET revision = revision + 1, updated_at = now() WHERE id = $1 RETURNING revision
	`, project.ID).Scan(&oldRevision); err != nil {
		t.Fatalf("bump initial project revision: %v", err)
	}

	snapshotLocked := make(chan struct{})
	releaseSnapshot := make(chan struct{})
	server.assetBatchSnapshotLockedHook = func() {
		close(snapshotLocked)
		<-releaseSnapshot
	}
	requestBody := map[string]any{
		"operation": "generate_prompts", "assetIds": []string{assetID}, "maxConcurrency": 1,
		"expectedProjectRevision": oldRevision, "idempotencyKey": "snapshot-old-" + suffix,
	}
	responseCh := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		raw, _ := json.Marshal(requestBody)
		req := httptest.NewRequest(http.MethodPost, "/api/projects/"+project.ID+"/asset-batches", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+owner.AccessToken)
		req.Header.Set("X-Organization-Id", owner.OrganizationID)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		responseCh <- recorder
	}()
	select {
	case <-snapshotLocked:
	case <-time.After(5 * time.Second):
		t.Fatal("asset batch did not reach the locked snapshot boundary")
	}

	mutationDone := make(chan error, 1)
	go func() {
		tx, err := pool.Begin(ctx)
		if err != nil {
			mutationDone <- err
			return
		}
		defer tx.Rollback(ctx)
		var revision int64
		if err := tx.QueryRow(ctx, `SELECT revision FROM projects WHERE id = $1 FOR UPDATE`, project.ID).Scan(&revision); err != nil {
			mutationDone <- err
			return
		}
		statements := []struct {
			query string
			args  []any
		}{
			{`UPDATE prompt_versions SET status = 'archived', activated_at = NULL WHERE id = $1`, []any{oldPromptVersionID}},
			{`UPDATE prompt_versions SET status = 'active', activated_at = now() WHERE id = $1`, []any{newPromptVersionID}},
			{`UPDATE prompt_bindings SET prompt_version_id = $2, updated_at = now() WHERE project_id = $1 AND template_key = 'asset_card_generation' AND status = 'active'`, []any{project.ID, newPromptVersionID}},
			{`UPDATE prompt_versions SET status = 'archived', activated_at = NULL WHERE id = $1`, []any{oldManualVersionID}},
			{`UPDATE prompt_versions SET status = 'active', activated_at = now() WHERE id = $1`, []any{newManualVersionID}},
			{`UPDATE project_manual_bindings SET prompt_version_id = $2, updated_at = now() WHERE project_id = $1 AND manual_kind = 'visual' AND status = 'active'`, []any{project.ID, newManualVersionID}},
			{`UPDATE model_profile_bindings SET priority = 5, weight = 77 WHERE id = $1`, []any{modelBindingID}},
			{`UPDATE projects SET video_ratio = '9:16', revision = revision + 1, updated_at = now() WHERE id = $1`, []any{project.ID}},
		}
		for _, statement := range statements {
			if _, err := tx.Exec(ctx, statement.query, statement.args...); err != nil {
				mutationDone <- err
				return
			}
		}
		mutationDone <- tx.Commit(ctx)
	}()
	close(releaseSnapshot)
	server.assetBatchSnapshotLockedHook = nil

	var firstResponse *httptest.ResponseRecorder
	select {
	case firstResponse = <-responseCh:
	case <-time.After(10 * time.Second):
		t.Fatal("old snapshot request did not finish")
	}
	if firstResponse.Code != http.StatusAccepted {
		t.Fatalf("old snapshot status=%d body=%s", firstResponse.Code, firstResponse.Body.String())
	}
	if err := <-mutationDone; err != nil {
		t.Fatalf("concurrent configuration mutation: %v", err)
	}
	var firstEnvelope struct {
		Data WorkflowRun `json:"data"`
	}
	if err := json.Unmarshal(firstResponse.Body.Bytes(), &firstEnvelope); err != nil {
		t.Fatalf("decode old snapshot response: %v", err)
	}
	oldSnapshot := loadAssetBatchSnapshot(t, ctx, pool, firstEnvelope.Data.ID)
	assertAssetBatchConfigurationSnapshot(t, oldSnapshot, oldRevision, oldPromptVersionID, oldManualVersionID, oldPriority, "16:9")

	var newRevision int64
	if err := pool.QueryRow(ctx, `SELECT revision FROM projects WHERE id = $1`, project.ID).Scan(&newRevision); err != nil {
		t.Fatal(err)
	}
	if newRevision != oldRevision+1 {
		t.Fatalf("new revision=%d, want %d", newRevision, oldRevision+1)
	}
	var secondRun WorkflowRun
	doAPISuccess(t, handler, http.MethodPost, "/api/projects/"+project.ID+"/asset-batches", owner.AccessToken, owner.OrganizationID, map[string]any{
		"operation": "generate_prompts", "assetIds": []string{assetID}, "maxConcurrency": 1,
		"expectedProjectRevision": newRevision, "idempotencyKey": "snapshot-new-" + suffix,
	}, &secondRun)
	newSnapshot := loadAssetBatchSnapshot(t, ctx, pool, secondRun.ID)
	assertAssetBatchConfigurationSnapshot(t, newSnapshot, newRevision, newPromptVersionID, newManualVersionID, 5, "9:16")
}

func seedSnapshotPromptTemplate(t *testing.T, ctx context.Context, pool *pgxpool.Pool, organizationID, userID, templateKey, oldContent, newContent string) (string, string) {
	t.Helper()
	var templateID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO prompt_templates(
			organization_id, template_key, name, purpose, description, modality, task_type, scope, status, is_system, created_by
		)
		VALUES ($1, $2, $2, 'snapshot integration test', 'snapshot integration test', 'text', 'text.generate', 'organization', 'active', false, $3)
		RETURNING id::text
	`, organizationID, templateKey, userID).Scan(&templateID); err != nil {
		t.Fatalf("insert prompt template %s: %v", templateKey, err)
	}
	var oldID, newID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO prompt_versions(
			prompt_template_id, template_id, version_no, version, content, variables_schema, content_hash,
			status, title, content_format, metadata, activated_at, created_by
		)
		VALUES ($1, $1, 1, 1, $2, '{}', $3, 'active', 'old', 'text', '{}', now(), $4)
		RETURNING id::text
	`, templateID, oldContent, strings.Repeat("a", 64), userID).Scan(&oldID); err != nil {
		t.Fatalf("insert old prompt version %s: %v", templateKey, err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO prompt_versions(
			prompt_template_id, template_id, version_no, version, content, variables_schema, content_hash,
			status, title, content_format, metadata, created_by
		)
		VALUES ($1, $1, 2, 2, $2, '{}', $3, 'draft', 'new', 'text', '{}', $4)
		RETURNING id::text
	`, templateID, newContent, strings.Repeat("b", 64), userID).Scan(&newID); err != nil {
		t.Fatalf("insert new prompt version %s: %v", templateKey, err)
	}
	return oldID, newID
}

func loadAssetBatchSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workflowRunID string) workflows.AssetBatchWorkflowInput {
	t.Helper()
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT snapshot FROM workflow_input_snapshots WHERE workflow_run_id = $1`, workflowRunID).Scan(&raw); err != nil {
		t.Fatalf("load asset batch snapshot: %v", err)
	}
	var snapshot workflows.AssetBatchWorkflowInput
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("decode asset batch snapshot: %v", err)
	}
	return snapshot
}

func assertAssetBatchConfigurationSnapshot(t *testing.T, snapshot workflows.AssetBatchWorkflowInput, revision int64, promptVersionID, manualVersionID string, priority int, ratio string) {
	t.Helper()
	if snapshot.Project.Revision != revision || snapshot.Project.PromptVersionID != promptVersionID || snapshot.Project.VideoRatio != ratio || snapshot.Project.AspectRatio != ratio {
		t.Fatalf("project snapshot = %+v", snapshot.Project)
	}
	visualManualID := ""
	for _, binding := range snapshot.Project.ManualBindings {
		if binding.ManualKind == "visual" {
			visualManualID = binding.PromptVersionID
			break
		}
	}
	if visualManualID != manualVersionID {
		t.Fatalf("visual manual version=%s, want %s", visualManualID, manualVersionID)
	}
	modelPriority := -1
	for _, binding := range snapshot.Project.ModelBindings {
		if binding.ProfileKey == snapshot.Project.ScriptModelProfileKey {
			modelPriority = binding.Priority
			break
		}
	}
	if modelPriority != priority {
		t.Fatalf("model priority=%d, want %d", modelPriority, priority)
	}
}

func seedAssetBatchModelBinding(t *testing.T, ctx context.Context, pool interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}, organizationID, userID, profileKey string) {
	t.Helper()
	var connectorID string
	if err := pool.QueryRow(ctx, `
		SELECT id::text
		FROM provider_connectors
		ORDER BY is_official DESC, created_at
		LIMIT 1
	`).Scan(&connectorID); err != nil {
		t.Fatalf("load provider connector: %v", err)
	}
	var accountID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_accounts(organization_id, connector_id, name, base_url, auth_type, status, created_by)
		VALUES ($1, $2, 'Asset Batch Test', 'https://example.invalid/v1', 'none', 'active', $3)
		RETURNING id::text
	`, organizationID, connectorID, userID).Scan(&accountID); err != nil {
		t.Fatalf("insert provider account: %v", err)
	}
	var modelID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status)
		VALUES ($1, 'asset-batch-test-text', 'Asset Batch Test Text', 'text', 'active')
		RETURNING id::text
	`, accountID).Scan(&modelID); err != nil {
		t.Fatalf("insert provider model: %v", err)
	}
	var profileID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO model_profiles(organization_id, profile_key, name, purpose)
		VALUES ($1, $2, 'Asset Batch Test Profile', 'asset batch integration test')
		ON CONFLICT (organization_id, profile_key) DO UPDATE SET updated_at = now()
		RETURNING id::text
	`, organizationID, profileKey).Scan(&profileID); err != nil {
		t.Fatalf("upsert model profile: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO model_profile_bindings(model_profile_id, provider_model_id, priority, weight, enabled)
		VALUES ($1, $2, 100, 100, true)
	`, profileID, modelID); err != nil {
		t.Fatalf("insert model profile binding: %v", err)
	}
}
