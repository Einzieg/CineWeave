package api

import (
	"bytes"
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
		Email: "asset-batch-" + suffix + "@example.test", Password: "Password123!", DisplayName: "Asset Batch",
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
		Email: "source-script-retry-" + suffix + "@example.test", Password: "Password123!", DisplayName: "Source Script Retry",
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
	chapterIDs := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	plan := workflows.SourceToScriptPlan{
		SourceID: sourceID, SourceType: "novel", SourceTitle: "Retry Novel", ScriptID: scriptID,
		ScriptVersionID: versionID, Title: "Retry Script", EpisodeTotal: 3,
		Chapters: []workflows.SourceToScriptChapterRef{
			{ID: chapterIDs[0], ChapterIndex: 1, Title: "第一节"},
			{ID: chapterIDs[1], ChapterIndex: 2, Title: "第二节"},
			{ID: chapterIDs[2], ChapterIndex: 3, Title: "第三节"},
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
			completed_at, terminalized_at, settled_at
		)
		VALUES ($1, $2, 'source-script-retry-original-' || gen_random_uuid()::text, 'source_to_script', 'partial_succeeded',
		        $3, '{}', $4, 3, 2, 1, 1, now(), now(), now())
		RETURNING id::text
	`, owner.OrganizationID, project.ID, originalInput, owner.User.ID).Scan(&originalID); err != nil {
		t.Fatalf("insert original workflow: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_runs SET root_workflow_run_id = id WHERE id = $1`, originalID); err != nil {
		t.Fatalf("set original root: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO workflow_node_runs(
			organization_id, project_id, workflow_run_id, node_key, node_type, status,
			input, output, attempt_generation, started_at, completed_at
		)
		VALUES ($1, $2, $3, $4, 'workflow.script_prepare', 'succeeded', '{}', $5, 1, now(), now())
	`, owner.OrganizationID, project.ID, originalID, workflows.SourceToScriptPrepareNodeKey, mustRawJSON(plan)); err != nil {
		t.Fatalf("insert prepare node: %v", err)
	}
	for index, chapter := range plan.Chapters {
		status := "succeeded"
		if index == 1 {
			status = "failed"
		}
		episode := workflows.GenerateSourceScriptEpisodeInput{
			OrganizationID: owner.OrganizationID, ProjectID: project.ID, WorkflowRunID: originalID,
			CreatedBy: owner.User.ID, SourceID: sourceID, ScriptID: scriptID, ScriptVersionID: versionID,
			Instruction: options.Instruction, EpisodeIndex: index + 1, EpisodeTotal: 3, Chapter: chapter, AttemptGeneration: 1,
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO workflow_node_runs(
				organization_id, project_id, workflow_run_id, node_key, node_type, status,
				input, output, attempt_generation, started_at, completed_at, error_code, error_message
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, '{}', 1, now(), now(),
			        CASE WHEN $6 = 'failed' THEN 'UPSTREAM_TIMEOUT' ELSE NULL END,
			        CASE WHEN $6 = 'failed' THEN 'episode timed out' ELSE NULL END)
		`, owner.OrganizationID, project.ID, originalID,
			workflows.SourceToScriptEpisodeNodeKey(chapter.ID, index+1), workflows.SourceToScriptEpisodeNodeType,
			status, mustRawJSON(episode)); err != nil {
			t.Fatalf("insert episode node %d: %v", index+1, err)
		}
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
	var nodeInput []byte
	if err := pool.QueryRow(ctx, `
		SELECT input FROM workflow_node_runs
		WHERE workflow_run_id = $1 AND node_type = $2
	`, retry.ID, workflows.SourceToScriptEpisodeNodeType).Scan(&nodeInput); err != nil {
		t.Fatalf("load retry episode node: %v", err)
	}
	var retriedEpisode workflows.GenerateSourceScriptEpisodeInput
	if err := json.Unmarshal(nodeInput, &retriedEpisode); err != nil {
		t.Fatalf("decode retry episode node: %v", err)
	}
	if retriedEpisode.EpisodeIndex != 2 || retriedEpisode.Chapter.ID != chapterIDs[1] || retriedEpisode.AttemptGeneration != 2 {
		t.Fatalf("retried episode = %+v", retriedEpisode)
	}
	var snapshot []byte
	if err := pool.QueryRow(ctx, `SELECT snapshot FROM workflow_input_snapshots WHERE workflow_run_id = $1`, retry.ID).Scan(&snapshot); err != nil {
		t.Fatalf("load retry snapshot: %v", err)
	}
	var startInput workflows.TextToStoryboardInput
	if err := json.Unmarshal(snapshot, &startInput); err != nil {
		t.Fatalf("decode retry snapshot: %v", err)
	}
	if startInput.SourceToScriptState == nil || len(startInput.SourceToScriptState.EpisodeIndexes) != 1 || startInput.SourceToScriptState.EpisodeIndexes[0] != 1 || startInput.SourceToScriptState.AttemptGeneration != 2 {
		t.Fatalf("retry state = %+v", startInput.SourceToScriptState)
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
		Email: "asset-snapshot-" + suffix + "@example.test", Password: "Password123!", DisplayName: "Asset Snapshot",
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
