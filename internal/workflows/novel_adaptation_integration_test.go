package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/db"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNovelEventManualOverrideIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run novel adaptation integration tests")
	}
	ctx := context.Background()
	pool := openNovelAdaptationTestDB(t, ctx)
	t.Cleanup(pool.Close)

	seed := seedNovelAdaptationBase(t, ctx, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, seed.orgID)
	})

	input := ExtractNovelEventsInput{
		OrganizationID: seed.orgID,
		ProjectID:      seed.projectID,
		WorkflowRunID:  seed.workflowRunID,
		CreatedBy:      seed.userID,
		SourceID:       seed.source.ID,
	}
	rendered := promptsvc.RenderedPrompt{TemplateKey: promptKeyNovelEventExtraction, PromptVersionID: "", RenderedHash: "sha256:test", Source: "test"}
	gatewayResp := provider.GatewayTextResponse{ProviderCallID: uuid.NewString(), ModelID: "model-1"}
	event := NovelEventCandidate{EventIndex: 1, Title: "Agent title", Summary: "Agent summary", Importance: 3}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	eventID, err := upsertNovelEventTx(ctx, tx, input, seed.source, seed.chapter, event, rendered, gatewayResp)
	if err != nil {
		t.Fatalf("upsert first event: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit first event: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE novel_events SET title = 'Manual title', manual_override = true WHERE id = $1`, eventID); err != nil {
		t.Fatalf("mark manual override: %v", err)
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin second: %v", err)
	}
	event.Title = "Agent overwrite"
	if _, err := upsertNovelEventTx(ctx, tx, input, seed.source, seed.chapter, event, rendered, gatewayResp); err != nil {
		t.Fatalf("upsert force false: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit force false: %v", err)
	}
	var title string
	if err := pool.QueryRow(ctx, `SELECT title FROM novel_events WHERE id = $1`, eventID).Scan(&title); err != nil {
		t.Fatalf("select title: %v", err)
	}
	if title != "Manual title" {
		t.Fatalf("title = %q, want manual title", title)
	}

	input.Force = true
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin force true: %v", err)
	}
	if _, err := upsertNovelEventTx(ctx, tx, input, seed.source, seed.chapter, event, rendered, gatewayResp); err != nil {
		t.Fatalf("upsert force true: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit force true: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT title FROM novel_events WHERE id = $1`, eventID).Scan(&title); err != nil {
		t.Fatalf("select forced title: %v", err)
	}
	if title != "Agent overwrite" {
		t.Fatalf("forced title = %q", title)
	}
}

func TestAdaptationPlanScriptMetadataIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run novel adaptation integration tests")
	}
	ctx := context.Background()
	pool := openNovelAdaptationTestDB(t, ctx)
	t.Cleanup(pool.Close)

	seed := seedNovelAdaptationBase(t, ctx, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, seed.orgID)
	})
	var planID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO adaptation_plans(organization_id, project_id, source_id, title, selected_event_ids, structure, content, created_by)
		VALUES ($1, $2, $3, 'Plan A', '[]', '{}', '{}', $4)
		RETURNING id::text
	`, seed.orgID, seed.projectID, seed.source.ID, seed.userID).Scan(&planID); err != nil {
		t.Fatalf("insert adaptation plan: %v", err)
	}
	activities := NewActivities(pool, nil, nil)
	providerCallID := uuid.NewString()
	nodeExecution, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: seed.orgID,
		ProjectID:      seed.projectID,
		WorkflowRunID:  seed.workflowRunID,
		NodeKey:        "test-create-script-from-plan",
		NodeType:       "test",
	})
	if err != nil {
		t.Fatalf("start node: %v", err)
	}
	output, err := activities.createGeneratedScriptFromPlan(ctx, GenerateScriptFromPlanInput{
		OrganizationID: seed.orgID,
		ProjectID:      seed.projectID,
		WorkflowRunID:  seed.workflowRunID,
		CreatedBy:      seed.userID,
		PlanID:         planID,
	}, adaptationPlanRecord{ID: planID, SourceID: seed.source.ID, Title: "Plan A", Content: `{}`, Structure: []byte(`{}`)}, nodeExecution, "script content", []workflowScriptEpisodeDraft{
		workflowDefaultScriptEpisodeDraft(seed.source.ID, "第 1 集", "script content", "", "sha256:test", providerCallID, mustJSON(map[string]any{
			"source":           "adaptation_plan_to_script",
			"adaptationPlanId": planID,
			"providerCallId":   providerCallID,
			"modelId":          "model-1",
		})),
	}, "", "sha256:test", []string{providerCallID}, []string{"model-1"})
	if err != nil {
		t.Fatalf("createGeneratedScriptFromPlan: %v", err)
	}
	var adaptationPlanID string
	if err := pool.QueryRow(ctx, `
		SELECT metadata->>'adaptationPlanId'
		FROM script_versions
		WHERE id = $1
	`, output.ScriptVersionID).Scan(&adaptationPlanID); err != nil {
		t.Fatalf("select script metadata: %v", err)
	}
	if adaptationPlanID != planID {
		t.Fatalf("adaptationPlanId = %q, want %q", adaptationPlanID, planID)
	}
}

func TestSourceToScriptNovelPathUsesEventsAndPlanIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run novel adaptation integration tests")
	}
	ctx := context.Background()
	pool := openNovelAdaptationTestDB(t, ctx)
	t.Cleanup(pool.Close)

	orgID, userID, projectID, workflowRunID, _, _ := seedWorkflowGatewayIntegrationData(t, ctx, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})
	var source ProjectSourceRecord
	if err := pool.QueryRow(ctx, `
		INSERT INTO project_sources(organization_id, project_id, source_type, title, content, content_format, status, created_by)
		VALUES ($1, $2, 'novel', 'Novel Source', 'chapter text', 'plain_text', 'ready', $3)
		RETURNING id::text, source_type, title, content, content_format
	`, orgID, projectID, userID).Scan(&source.ID, &source.SourceType, &source.Title, &source.Content, &source.ContentFormat); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO novel_chapters(organization_id, project_id, source_id, chapter_index, chapter_title, content, event_state)
		VALUES ($1, $2, $3, 1, 'Chapter One', 'chapter text', 'pending')
	`, orgID, projectID, source.ID); err != nil {
		t.Fatalf("insert chapter: %v", err)
	}

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/text/generate" {
			http.NotFound(w, r)
			return
		}
		var req provider.GatewayTextRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode gateway request: %v", err)
		}
		if req.NodeRunID == "" || req.ModelProfileKey != scriptModelProfileKey {
			t.Fatalf("gateway request = %+v", req)
		}
		switch req.PromptTemplateKey {
		case promptKeyNovelEventExtraction:
			writeWorkflowGatewayEnvelope(t, w, provider.GatewayTextResponse{
				ProviderCallID: uuid.NewString(),
				ModelID:        "model-text",
				Status:         "succeeded",
				Output: provider.GatewayTextOutput{Text: `{
					"events": [{
						"title": "Station clue",
						"summary": "The protagonist finds a clue at the station.",
						"eventType": "reveal",
						"importance": 4,
						"characters": ["Lin"],
						"scenes": ["Station"],
						"props": ["Camera"],
						"keywords": ["clue"]
					}],
					"links": []
				}`},
			})
		case promptKeyAdaptationPlanGeneration:
			writeWorkflowGatewayEnvelope(t, w, provider.GatewayTextResponse{
				ProviderCallID: uuid.NewString(),
				ModelID:        "model-text",
				Status:         "succeeded",
				Output: provider.GatewayTextOutput{Text: `{
					"title": "Station Plan",
					"logline": "A clue pushes Lin into motion.",
					"structure": {"opening": "Station clue", "ending": "Departure"},
					"selectedEvents": ["1"],
					"omittedEvents": [],
					"visualStrategy": "Quiet dawn frames",
					"characterStrategy": "Focus on Lin",
					"shotStrategy": "Three shots",
					"estimatedShots": 3
				}`},
			})
		case promptKeyScriptFromAdaptationPlan:
			writeWorkflowGatewayEnvelope(t, w, provider.GatewayTextResponse{
				ProviderCallID: uuid.NewString(),
				ModelID:        "model-text",
				Status:         "succeeded",
				Output:         provider.GatewayTextOutput{Text: "# Station Plan\n\n## Scene 1\nLin finds the clue."},
			})
		default:
			t.Fatalf("unexpected prompt template %q", req.PromptTemplateKey)
		}
	}))
	defer gateway.Close()

	activities := NewActivities(pool, nil, &provider.GatewayClient{BaseURL: gateway.URL, Token: "source-to-script-test", Client: gateway.Client()})
	output, err := activities.GenerateScriptFromSource(ctx, GenerateScriptFromSourceInput{
		OrganizationID: orgID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		CreatedBy:      userID,
		SourceID:       source.ID,
		Instruction:    "Keep it visual.",
		Title:          "Generated Script",
	})
	if err != nil {
		t.Fatalf("GenerateScriptFromSource: %v", err)
	}
	if output.AdaptationPlanID == "" || output.ScriptID == "" || output.ScriptVersionID == "" || !strings.Contains(output.Content, "Scene 1") {
		t.Fatalf("output = %+v", output)
	}
	var eventCount, planCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM novel_events WHERE project_id = $1 AND source_id = $2`, projectID, source.ID).Scan(&eventCount); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM adaptation_plans WHERE project_id = $1 AND source_id = $2`, projectID, source.ID).Scan(&planCount); err != nil {
		t.Fatalf("count plans: %v", err)
	}
	if eventCount != 1 || planCount != 1 {
		t.Fatalf("eventCount=%d planCount=%d", eventCount, planCount)
	}
	var adaptationPlanID string
	if err := pool.QueryRow(ctx, `SELECT metadata->>'adaptationPlanId' FROM script_versions WHERE id = $1`, output.ScriptVersionID).Scan(&adaptationPlanID); err != nil {
		t.Fatalf("select script metadata: %v", err)
	}
	if adaptationPlanID != output.AdaptationPlanID {
		t.Fatalf("adaptationPlanId = %q, want %q", adaptationPlanID, output.AdaptationPlanID)
	}
}

func TestPrepareSourceToScriptAppendsSecondChapterToCurrentScriptIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run novel adaptation integration tests")
	}
	ctx := context.Background()
	pool := openNovelAdaptationTestDB(t, ctx)
	t.Cleanup(pool.Close)

	orgID, userID, projectID, workflowRunID, _, _ := seedWorkflowGatewayIntegrationData(t, ctx, pool)
	var sourceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO project_sources(organization_id, project_id, source_type, title, content, content_format, status, created_by)
		VALUES ($1, $2, 'novel', 'Sequential Novel', 'chapter one\nchapter two', 'plain_text', 'ready', $3)
		RETURNING id::text
	`, orgID, projectID, userID).Scan(&sourceID); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	var chapterOneID, chapterTwoID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO novel_chapters(
			organization_id, project_id, source_id, chapter_index, volume_index, section_index,
			volume_title, chapter_title, content, event_state
		)
		VALUES ($1, $2, $3, 1, 1, 1, '第一卷', '第一节', 'first chapter text', 'pending')
		RETURNING id::text
	`, orgID, projectID, sourceID).Scan(&chapterOneID); err != nil {
		t.Fatalf("insert first chapter: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO novel_chapters(
			organization_id, project_id, source_id, chapter_index, volume_index, section_index,
			volume_title, chapter_title, content, event_state
		)
		VALUES ($1, $2, $3, 2, 1, 2, '第一卷', '第二节', 'second chapter text', 'pending')
		RETURNING id::text
	`, orgID, projectID, sourceID).Scan(&chapterTwoID); err != nil {
		t.Fatalf("insert second chapter: %v", err)
	}

	var scriptID, versionID, firstEpisodeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO scripts(organization_id, project_id, source_id, title, status, created_by)
		VALUES ($1, $2, $3, 'Current Script', 'active', $4)
		RETURNING id::text
	`, orgID, projectID, sourceID, userID).Scan(&scriptID); err != nil {
		t.Fatalf("insert script: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO script_versions(
			organization_id, project_id, script_id, version_no, version, content,
			content_format, status, source_type, metadata, created_by
		)
		VALUES ($1, $2, $3, 1, 1, 'episode one', 'markdown', 'active', 'agent_generated', '{}', $4)
		RETURNING id::text
	`, orgID, projectID, scriptID, userID).Scan(&versionID); err != nil {
		t.Fatalf("insert script version: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO script_episodes(
			organization_id, project_id, script_id, script_version_id, source_id, source_chapter_id,
			episode_index, volume_index, section_index, volume_title, episode_title, content, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, 1, 1, 1, '第一卷', '第一节', 'episode one', $7)
		RETURNING id::text
	`, orgID, projectID, scriptID, versionID, sourceID, chapterOneID, userID).Scan(&firstEpisodeID); err != nil {
		t.Fatalf("insert first episode: %v", err)
	}
	var firstSceneID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO script_scenes(
			organization_id, project_id, script_id, script_version_id, script_episode_id,
			scene_index, scene_no, title, content, created_by
		)
		VALUES ($1, $2, $3, $4, $5, 1, 1, 'Episode One Scene', 'scene content', $6)
		RETURNING id::text
	`, orgID, projectID, scriptID, versionID, firstEpisodeID, userID).Scan(&firstSceneID); err != nil {
		t.Fatalf("insert first scene: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE scripts SET current_version_id = $2 WHERE id = $1`, scriptID, versionID); err != nil {
		t.Fatalf("activate script version: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE projects SET active_script_id = $2 WHERE id = $1`, projectID, scriptID); err != nil {
		t.Fatalf("set current project script: %v", err)
	}

	activities := NewActivities(pool, nil, nil)
	plan, err := activities.PrepareScriptFromSource(ctx, PrepareScriptFromSourceInput{GenerateScriptFromSourceInput: GenerateScriptFromSourceInput{
		OrganizationID: orgID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		CreatedBy:      userID,
		SourceID:       sourceID,
		ChapterIDs:     []string{chapterTwoID},
		IdempotencyKey: "append-second-chapter",
	}})
	if err != nil {
		t.Fatalf("PrepareScriptFromSource: %v", err)
	}
	if plan.ScriptID != scriptID || plan.GenerationID == "" || plan.BaseScriptVersionID != versionID || plan.ScriptVersionID != "" {
		t.Fatalf("plan did not freeze the current script without publishing a version: %+v", plan)
	}
	if plan.PreviousScriptVersionID != versionID || plan.PreviousActiveScriptID != scriptID {
		t.Fatalf("plan previous identities = %+v", plan)
	}
	if plan.EpisodeTotal != 1 || plan.SeriesEpisodeTotal != 2 || SourceToScriptEpisodeNumber(plan, 0) != 2 {
		t.Fatalf("plan episode identity = %+v", plan)
	}
	var scriptCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM scripts WHERE project_id = $1`, projectID).Scan(&scriptCount); err != nil {
		t.Fatalf("count scripts: %v", err)
	}
	if scriptCount != 1 {
		t.Fatalf("script count = %d, want 1", scriptCount)
	}
	var preparedCurrentVersionID, preparedActiveScriptID string
	if err := pool.QueryRow(ctx, `SELECT current_version_id::text FROM scripts WHERE id = $1`, scriptID).Scan(&preparedCurrentVersionID); err != nil {
		t.Fatalf("load current version after prepare: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT active_script_id::text FROM projects WHERE id = $1`, projectID).Scan(&preparedActiveScriptID); err != nil {
		t.Fatalf("load active script after prepare: %v", err)
	}
	if preparedCurrentVersionID != versionID || preparedActiveScriptID != scriptID {
		t.Fatalf("prepare changed active identities: version=%s script=%s", preparedCurrentVersionID, preparedActiveScriptID)
	}
	var preparedVersionCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM script_versions WHERE script_id = $1`, scriptID).Scan(&preparedVersionCount); err != nil {
		t.Fatalf("count versions after prepare: %v", err)
	}
	if preparedVersionCount != 1 {
		t.Fatalf("version count after prepare = %d, want 1", preparedVersionCount)
	}

	var queuedRaw json.RawMessage
	if err := pool.QueryRow(ctx, `
		SELECT input
		FROM workflow_node_runs
		WHERE workflow_run_id = $1 AND node_type = $2
	`, workflowRunID, SourceToScriptEpisodeNodeType).Scan(&queuedRaw); err != nil {
		t.Fatalf("load queued episode: %v", err)
	}
	var queued GenerateSourceScriptEpisodeInput
	if err := json.Unmarshal(queuedRaw, &queued); err != nil {
		t.Fatalf("decode queued episode: %v", err)
	}
	if queued.EpisodeIndex != 2 || queued.EpisodeTotal != 2 || queued.Chapter.ID != chapterTwoID {
		t.Fatalf("queued episode = %+v", queued)
	}

	output := stageSourceToScriptEpisodeSuccess(t, ctx, pool, activities, queued, plan, userID, "episode two")
	if output.EpisodeIndex != 2 || output.ScriptID != scriptID || output.GenerationResultID == "" {
		t.Fatalf("staged output = %+v", output)
	}
	var formalEpisodeCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM script_episodes WHERE script_id = $1`, scriptID).Scan(&formalEpisodeCount); err != nil {
		t.Fatalf("count episodes before finalize: %v", err)
	}
	if formalEpisodeCount != 1 {
		t.Fatalf("formal episode count before finalize = %d, want 1", formalEpisodeCount)
	}
	var originalEpisodeCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM script_episodes WHERE script_version_id = $1 AND id = $2`, versionID, firstEpisodeID).Scan(&originalEpisodeCount); err != nil {
		t.Fatalf("count original episode: %v", err)
	}
	if originalEpisodeCount != 1 {
		t.Fatalf("original episode count = %d, want 1", originalEpisodeCount)
	}

	finalized, err := activities.FinalizeScriptFromSource(ctx, GenerateScriptFromSourceInput{
		OrganizationID: orgID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		CreatedBy:      userID,
		SourceID:       sourceID,
	}, plan, SourceToScriptFinalization{RequestedEpisodeCount: 1, CompletedEpisodeCount: 1})
	if err != nil {
		t.Fatalf("FinalizeScriptFromSource: %v", err)
	}
	if finalized.Status != "succeeded" || finalized.ScriptVersionID == "" || finalized.ScriptVersionID == versionID || finalized.EpisodeCount != 2 {
		t.Fatalf("finalized output = %+v", finalized)
	}
	type episodeIdentity struct {
		id        string
		index     int
		chapterID string
	}
	rows, err := pool.Query(ctx, `
		SELECT id::text, episode_index, source_chapter_id::text
		FROM script_episodes
		WHERE script_version_id = $1
		ORDER BY episode_index
	`, finalized.ScriptVersionID)
	if err != nil {
		t.Fatalf("list finalized episodes: %v", err)
	}
	identities := make([]episodeIdentity, 0, 2)
	for rows.Next() {
		var item episodeIdentity
		if err := rows.Scan(&item.id, &item.index, &item.chapterID); err != nil {
			rows.Close()
			t.Fatalf("scan finalized episode: %v", err)
		}
		identities = append(identities, item)
	}
	rows.Close()
	if len(identities) != 2 || identities[0].id == firstEpisodeID || identities[0].index != 1 || identities[0].chapterID != chapterOneID || identities[1].index != 2 || identities[1].chapterID != chapterTwoID {
		t.Fatalf("episode identities = %+v", identities)
	}
	var currentVersionID, sceneStaleState string
	if err := pool.QueryRow(ctx, `SELECT current_version_id::text FROM scripts WHERE id = $1`, scriptID).Scan(&currentVersionID); err != nil {
		t.Fatalf("load activated version: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT stale_state FROM script_scenes WHERE id = $1`, firstSceneID).Scan(&sceneStaleState); err != nil {
		t.Fatalf("load previous scene stale state: %v", err)
	}
	if currentVersionID != finalized.ScriptVersionID || sceneStaleState != "needs_regeneration" {
		t.Fatalf("activation result: version=%s sceneStaleState=%s", currentVersionID, sceneStaleState)
	}
	replayed, err := activities.FinalizeScriptFromSource(ctx, GenerateScriptFromSourceInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, SourceID: sourceID,
	}, plan, SourceToScriptFinalization{RequestedEpisodeCount: 1, CompletedEpisodeCount: 1})
	if err != nil {
		t.Fatalf("replay FinalizeScriptFromSource: %v", err)
	}
	if replayed.ScriptVersionID != finalized.ScriptVersionID {
		t.Fatalf("replayed version = %s, want %s", replayed.ScriptVersionID, finalized.ScriptVersionID)
	}
	var finalizedVersionCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM script_versions WHERE script_id = $1`, scriptID).Scan(&finalizedVersionCount); err != nil {
		t.Fatalf("count versions after replay: %v", err)
	}
	if finalizedVersionCount != 2 {
		t.Fatalf("version count after replay = %d, want 2", finalizedVersionCount)
	}
}

func TestFinalizeSourceToScriptKeepsPreviousProjectScriptWhenAllEpisodesFailIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run novel adaptation integration tests")
	}
	ctx := context.Background()
	pool := openNovelAdaptationTestDB(t, ctx)
	t.Cleanup(pool.Close)

	orgID, userID, projectID, workflowRunID, _, _ := seedWorkflowGatewayIntegrationData(t, ctx, pool)
	var sourceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO project_sources(organization_id, project_id, source_type, title, content, content_format, status, created_by)
		VALUES ($1, $2, 'brief', 'Failed Draft Source', 'source content', 'plain_text', 'ready', $3)
		RETURNING id::text
	`, orgID, projectID, userID).Scan(&sourceID); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	var previousScriptID, previousVersionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO scripts(organization_id, project_id, source_id, title, status, created_by)
		VALUES ($1, $2, $3, 'Previous Script', 'active', $4)
		RETURNING id::text
	`, orgID, projectID, sourceID, userID).Scan(&previousScriptID); err != nil {
		t.Fatalf("insert previous script: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO script_versions(
			organization_id, project_id, script_id, version_no, version, content,
			content_format, status, source_type, metadata, created_by
		)
		VALUES ($1, $2, $3, 1, 1, 'previous content', 'markdown', 'active', 'manual', '{}', $4)
		RETURNING id::text
	`, orgID, projectID, previousScriptID, userID).Scan(&previousVersionID); err != nil {
		t.Fatalf("insert previous version: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE scripts SET current_version_id = $2 WHERE id = $1`, previousScriptID, previousVersionID); err != nil {
		t.Fatalf("set previous current version: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE projects SET active_script_id = $2 WHERE id = $1`, projectID, previousScriptID); err != nil {
		t.Fatalf("set previous project script: %v", err)
	}

	activities := NewActivities(pool, nil, nil)
	input := GenerateScriptFromSourceInput{
		OrganizationID:  orgID,
		ProjectID:       projectID,
		WorkflowRunID:   workflowRunID,
		CreatedBy:       userID,
		SourceID:        sourceID,
		CreateNewScript: true,
		IdempotencyKey:  "all-episodes-fail",
	}
	plan, err := activities.PrepareScriptFromSource(ctx, PrepareScriptFromSourceInput{GenerateScriptFromSourceInput: input})
	if err != nil {
		t.Fatalf("PrepareScriptFromSource: %v", err)
	}
	if plan.ScriptID == previousScriptID || plan.PreviousActiveScriptID != previousScriptID {
		t.Fatalf("prepared plan = %+v", plan)
	}
	queued := loadQueuedSourceToScriptEpisode(t, ctx, pool, workflowRunID, plan.Chapters[0])
	if err := activities.FailSourceScriptEpisode(ctx, FailSourceScriptEpisodeInput{
		Episode: queued, ErrorCode: "UPSTREAM_TIMEOUT", ErrorMessage: "provider timed out",
	}); err != nil {
		t.Fatalf("persist failed episode staging result: %v", err)
	}

	output, err := activities.FinalizeScriptFromSource(ctx, input, plan, SourceToScriptFinalization{
		RequestedEpisodeCount: 1,
		FailedEpisodeCount:    1,
	})
	if err != nil {
		t.Fatalf("FinalizeScriptFromSource: %v", err)
	}
	if output.Status != "failed" || output.CompletedItems != 0 || output.FailedItems != 1 {
		t.Fatalf("failed output = %+v", output)
	}
	var activeScriptID, draftStatus, previousCurrentVersionID string
	if err := pool.QueryRow(ctx, `SELECT active_script_id::text FROM projects WHERE id = $1`, projectID).Scan(&activeScriptID); err != nil {
		t.Fatalf("load active project script: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM scripts WHERE id = $1`, plan.ScriptID).Scan(&draftStatus); err != nil {
		t.Fatalf("load failed draft status: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT current_version_id::text FROM scripts WHERE id = $1`, previousScriptID).Scan(&previousCurrentVersionID); err != nil {
		t.Fatalf("load previous current version: %v", err)
	}
	if activeScriptID != previousScriptID || previousCurrentVersionID != previousVersionID || draftStatus != "draft" {
		t.Fatalf("failed generation changed current script: active=%s previousVersion=%s draftStatus=%s", activeScriptID, previousCurrentVersionID, draftStatus)
	}
	var failedDraftVersionCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM script_versions WHERE script_id = $1`, plan.ScriptID).Scan(&failedDraftVersionCount); err != nil {
		t.Fatalf("count failed draft versions: %v", err)
	}
	if failedDraftVersionCount != 0 {
		t.Fatalf("failed draft version count = %d, want 0", failedDraftVersionCount)
	}
}

func loadQueuedSourceToScriptEpisode(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	workflowRunID string,
	chapter SourceToScriptChapterRef,
) GenerateSourceScriptEpisodeInput {
	t.Helper()
	var raw json.RawMessage
	if err := pool.QueryRow(ctx, `
		SELECT input
		FROM workflow_node_runs
		WHERE workflow_run_id = $1 AND node_key = $2
	`, workflowRunID, SourceToScriptEpisodeNodeKey(chapter.ID, chapter.ManifestOrdinal)).Scan(&raw); err != nil {
		t.Fatalf("load queued source-to-script episode: %v", err)
	}
	var queued GenerateSourceScriptEpisodeInput
	if err := json.Unmarshal(raw, &queued); err != nil {
		t.Fatalf("decode queued source-to-script episode: %v", err)
	}
	return queued
}

func stageSourceToScriptEpisodeSuccess(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	activities Activities,
	queued GenerateSourceScriptEpisodeInput,
	plan SourceToScriptPlan,
	createdBy string,
	content string,
) SourceScriptEpisodeOutput {
	t.Helper()
	execution, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: queued.OrganizationID, ProjectID: queued.ProjectID, WorkflowRunID: queued.WorkflowRunID,
		NodeKey:  SourceToScriptEpisodeNodeKey(queued.Chapter.ID, queued.EpisodeIndex),
		NodeType: SourceToScriptEpisodeNodeType, Input: mustJSON(queued), AttemptGeneration: queued.AttemptGeneration,
	})
	if err != nil {
		t.Fatalf("start source-to-script episode node: %v", err)
	}
	generation, item, err := activities.loadSourceToScriptGenerationItem(ctx, queued)
	if err != nil {
		t.Fatalf("load frozen source-to-script generation item: %v", err)
	}
	var agentRunID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runs(organization_id, project_id, agent_type, task_type, status, input, created_by, started_at)
		VALUES ($1, $2, 'script_agent', 'generate_script', 'running', '{}', NULLIF($3, '')::uuid, now())
		RETURNING id::text
	`, queued.OrganizationID, queued.ProjectID, createdBy).Scan(&agentRunID); err != nil {
		t.Fatalf("insert source-to-script agent run: %v", err)
	}
	renderedHash := "sha256:" + sourceToScriptTextHash(queued.ItemKey+":"+content)
	output, err := activities.storeSourceScriptGenerationResult(
		ctx, queued, generation, item, execution,
		promptsvc.RenderedPrompt{
			TemplateKey: plan.PromptTemplateKey, PromptVersionID: plan.PromptVersionID,
			RenderedHash: renderedHash, Source: "integration_test",
		},
		provider.GatewayTextResponse{ModelID: plan.ProviderModelID},
		agentRunID,
		content,
	)
	if err != nil {
		t.Fatalf("store staged source-to-script result: %v", err)
	}
	return output
}

type novelAdaptationSeed struct {
	orgID         string
	userID        string
	projectID     string
	workflowRunID string
	source        ProjectSourceRecord
	chapter       novelChapterRecord
}

func openNovelAdaptationTestDB(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for novel adaptation integration tests")
	}
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	return pool
}

func seedNovelAdaptationBase(t *testing.T, ctx context.Context, pool *pgxpool.Pool) novelAdaptationSeed {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var seed novelAdaptationSeed
	var workspaceID string
	if err := pool.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ($1, $2) RETURNING id::text`, "Novel Adaptation", "novel-adaptation-"+suffix).Scan(&seed.orgID); err != nil {
		t.Fatalf("insert organization: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email, display_name) VALUES ($1, $2) RETURNING id::text`, "novel-adaptation-"+suffix+"@example.test", "Novel Adaptation").Scan(&seed.userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organization_members(organization_id, user_id) VALUES ($1, $2)`, seed.orgID, seed.userID); err != nil {
		t.Fatalf("insert organization member: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(organization_id, name) VALUES ($1, 'Novel Workspace') RETURNING id::text`, seed.orgID).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects(organization_id, workspace_id, name, created_by)
		VALUES ($1, $2, 'Novel Project', $3)
		RETURNING id::text
	`, seed.orgID, workspaceID, seed.userID).Scan(&seed.projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO project_members(project_id, user_id) VALUES ($1, $2)`, seed.projectID, seed.userID); err != nil {
		t.Fatalf("insert project member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workflow_runs(organization_id, project_id, temporal_workflow_id, status, input, output, created_by)
		VALUES ($1, $2, $3, 'queued', '{}', '{}', $4)
		RETURNING id::text
	`, seed.orgID, seed.projectID, "novel-workflow-"+suffix, seed.userID).Scan(&seed.workflowRunID); err != nil {
		t.Fatalf("insert workflow run: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO project_sources(organization_id, project_id, source_type, title, content, content_format, status, created_by)
		VALUES ($1, $2, 'novel', 'Novel Source', 'chapter text', 'plain_text', 'ready', $3)
		RETURNING id::text, source_type, title, content, content_format
	`, seed.orgID, seed.projectID, seed.userID).Scan(&seed.source.ID, &seed.source.SourceType, &seed.source.Title, &seed.source.Content, &seed.source.ContentFormat); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO novel_chapters(organization_id, project_id, source_id, chapter_index, chapter_title, content, event_state)
		VALUES ($1, $2, $3, 1, 'Chapter One', 'chapter text', 'pending')
		RETURNING id::text, chapter_index, COALESCE(volume_title, ''), COALESCE(chapter_title, ''), content
	`, seed.orgID, seed.projectID, seed.source.ID).Scan(&seed.chapter.ID, &seed.chapter.ChapterIndex, &seed.chapter.VolumeTitle, &seed.chapter.ChapterTitle, &seed.chapter.Content); err != nil {
		t.Fatalf("insert chapter: %v", err)
	}
	return seed
}
