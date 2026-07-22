package workflows

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type sourceToScriptGenerationFixture struct {
	organizationID string
	userID         string
	projectID      string
	workflowRunID  string
	sourceID       string
	scriptID       string
	baseVersionID  string
	chapterIDs     []string
	activities     Activities
}

type sourceToScriptFixtureChapter struct {
	volumeIndex  int
	sectionIndex int
	title        string
	content      string
	baseContent  string
}

func TestSourceToScriptMixedFailureKeepsFallbackEpisodeAndActivatesCompleteVersionIntegration(t *testing.T) {
	requireSourceToScriptIntegration(t)
	ctx := context.Background()
	pool := openNovelAdaptationTestDB(t, ctx)
	t.Cleanup(pool.Close)

	fixture := seedSourceToScriptGenerationFixture(t, ctx, pool, []sourceToScriptFixtureChapter{
		{volumeIndex: 1, sectionIndex: 1, title: "第一节", content: "source one", baseContent: "old episode one"},
		{volumeIndex: 1, sectionIndex: 2, title: "第二节", content: "source two", baseContent: "old episode two"},
	})
	plan := prepareSourceToScriptGenerationFixture(t, ctx, fixture, fixture.chapterIDs)
	first := loadQueuedSourceToScriptEpisode(t, ctx, pool, fixture.workflowRunID, plan.Chapters[0])
	stageSourceToScriptEpisodeSuccess(t, ctx, pool, fixture.activities, first, plan, fixture.userID, "new episode one")
	second := loadQueuedSourceToScriptEpisode(t, ctx, pool, fixture.workflowRunID, plan.Chapters[1])
	if err := fixture.activities.FailSourceScriptEpisode(ctx, FailSourceScriptEpisodeInput{
		Episode: second, ErrorCode: "UPSTREAM_TIMEOUT", ErrorMessage: "provider timed out",
	}); err != nil {
		t.Fatalf("fail second episode: %v", err)
	}

	output, err := fixture.activities.FinalizeScriptFromSource(ctx, sourceToScriptFixtureInput(fixture), plan, SourceToScriptFinalization{
		RequestedEpisodeCount: 2, CompletedEpisodeCount: 1, FailedEpisodeCount: 1,
	})
	if err != nil {
		t.Fatalf("FinalizeScriptFromSource: %v", err)
	}
	if output.Status != "partial_succeeded" || output.ScriptVersionID == fixture.baseVersionID || output.EpisodeCount != 2 {
		t.Fatalf("output = %+v", output)
	}
	var currentVersionID string
	if err := pool.QueryRow(ctx, `SELECT current_version_id::text FROM scripts WHERE id = $1`, fixture.scriptID).Scan(&currentVersionID); err != nil {
		t.Fatalf("load current version: %v", err)
	}
	if currentVersionID != output.ScriptVersionID {
		t.Fatalf("current version = %s, want %s", currentVersionID, output.ScriptVersionID)
	}
	rows, err := pool.Query(ctx, `
		SELECT episode_index, content, stale_state, metadata->>'generationFallback'
		FROM script_episodes
		WHERE script_version_id = $1
		ORDER BY episode_index
	`, output.ScriptVersionID)
	if err != nil {
		t.Fatalf("list finalized episodes: %v", err)
	}
	defer rows.Close()
	type episodeState struct {
		index    int
		content  string
		stale    string
		fallback *string
	}
	states := make([]episodeState, 0, 2)
	for rows.Next() {
		var state episodeState
		if err := rows.Scan(&state.index, &state.content, &state.stale, &state.fallback); err != nil {
			t.Fatalf("scan finalized episode: %v", err)
		}
		states = append(states, state)
	}
	if len(states) != 2 || states[0].content != "new episode one" || states[0].stale != "fresh" ||
		states[1].content != "old episode two" || states[1].stale != "needs_regeneration" || states[1].fallback == nil || *states[1].fallback != "true" {
		t.Fatalf("finalized episode states = %+v", states)
	}
}

func TestSourceToScriptMissingFailedEpisodeCreatesUnactivatedPartialVersionIntegration(t *testing.T) {
	requireSourceToScriptIntegration(t)
	ctx := context.Background()
	pool := openNovelAdaptationTestDB(t, ctx)
	t.Cleanup(pool.Close)

	fixture := seedSourceToScriptGenerationFixture(t, ctx, pool, []sourceToScriptFixtureChapter{
		{volumeIndex: 1, sectionIndex: 1, title: "第一节", content: "source one", baseContent: "old episode one"},
		{volumeIndex: 1, sectionIndex: 2, title: "第二节", content: "source two"},
	})
	plan := prepareSourceToScriptGenerationFixture(t, ctx, fixture, fixture.chapterIDs)
	first := loadQueuedSourceToScriptEpisode(t, ctx, pool, fixture.workflowRunID, plan.Chapters[0])
	stageSourceToScriptEpisodeSuccess(t, ctx, pool, fixture.activities, first, plan, fixture.userID, "new episode one")
	second := loadQueuedSourceToScriptEpisode(t, ctx, pool, fixture.workflowRunID, plan.Chapters[1])
	if err := fixture.activities.FailSourceScriptEpisode(ctx, FailSourceScriptEpisodeInput{
		Episode: second, ErrorCode: "CONTENT_REJECTED", ErrorMessage: "episode rejected",
	}); err != nil {
		t.Fatalf("fail missing second episode: %v", err)
	}

	output, err := fixture.activities.FinalizeScriptFromSource(ctx, sourceToScriptFixtureInput(fixture), plan, SourceToScriptFinalization{
		RequestedEpisodeCount: 2, CompletedEpisodeCount: 1, FailedEpisodeCount: 1,
	})
	if err != nil {
		t.Fatalf("FinalizeScriptFromSource: %v", err)
	}
	if output.Status != "partial_succeeded" || output.ScriptVersionID == "" || output.EpisodeCount != 1 {
		t.Fatalf("output = %+v", output)
	}
	var currentVersionID, partialStatus string
	if err := pool.QueryRow(ctx, `SELECT current_version_id::text FROM scripts WHERE id = $1`, fixture.scriptID).Scan(&currentVersionID); err != nil {
		t.Fatalf("load current version: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM script_versions WHERE id = $1`, output.ScriptVersionID).Scan(&partialStatus); err != nil {
		t.Fatalf("load partial version: %v", err)
	}
	if currentVersionID != fixture.baseVersionID || partialStatus != "partial" {
		t.Fatalf("partial publication changed current version: current=%s base=%s status=%s", currentVersionID, fixture.baseVersionID, partialStatus)
	}
}

func TestSourceToScriptRejectsSourceAndScriptChangesBeforePublicationIntegration(t *testing.T) {
	requireSourceToScriptIntegration(t)
	t.Run("source revision", func(t *testing.T) {
		ctx := context.Background()
		pool := openNovelAdaptationTestDB(t, ctx)
		t.Cleanup(pool.Close)
		fixture := seedSourceToScriptGenerationFixture(t, ctx, pool, []sourceToScriptFixtureChapter{
			{volumeIndex: 1, sectionIndex: 1, title: "第一节", content: "source one", baseContent: "old episode"},
		})
		plan := prepareSourceToScriptGenerationFixture(t, ctx, fixture, fixture.chapterIDs)
		queued := loadQueuedSourceToScriptEpisode(t, ctx, pool, fixture.workflowRunID, plan.Chapters[0])
		stageSourceToScriptEpisodeSuccess(t, ctx, pool, fixture.activities, queued, plan, fixture.userID, "new episode")
		if _, err := pool.Exec(ctx, `UPDATE novel_chapters SET content = 'changed source' WHERE id = $1`, fixture.chapterIDs[0]); err != nil {
			t.Fatalf("change source chapter: %v", err)
		}
		_, err := fixture.activities.FinalizeScriptFromSource(ctx, sourceToScriptFixtureInput(fixture), plan, SourceToScriptFinalization{RequestedEpisodeCount: 1, CompletedEpisodeCount: 1})
		if err == nil || !strings.Contains(err.Error(), codeSourceToScriptReplanRequired) {
			t.Fatalf("finalize error = %v, want %s", err, codeSourceToScriptReplanRequired)
		}
		assertSourceToScriptPublicationRejected(t, ctx, pool, fixture, plan, codeSourceToScriptReplanRequired)
	})

	t.Run("script compare and swap", func(t *testing.T) {
		ctx := context.Background()
		pool := openNovelAdaptationTestDB(t, ctx)
		t.Cleanup(pool.Close)
		fixture := seedSourceToScriptGenerationFixture(t, ctx, pool, []sourceToScriptFixtureChapter{
			{volumeIndex: 1, sectionIndex: 1, title: "第一节", content: "source one", baseContent: "old episode"},
		})
		plan := prepareSourceToScriptGenerationFixture(t, ctx, fixture, fixture.chapterIDs)
		queued := loadQueuedSourceToScriptEpisode(t, ctx, pool, fixture.workflowRunID, plan.Chapters[0])
		stageSourceToScriptEpisodeSuccess(t, ctx, pool, fixture.activities, queued, plan, fixture.userID, "new episode")
		if _, err := pool.Exec(ctx, `UPDATE scripts SET title = title || ' user edit' WHERE id = $1`, fixture.scriptID); err != nil {
			t.Fatalf("change script: %v", err)
		}
		_, err := fixture.activities.FinalizeScriptFromSource(ctx, sourceToScriptFixtureInput(fixture), plan, SourceToScriptFinalization{RequestedEpisodeCount: 1, CompletedEpisodeCount: 1})
		if err == nil || !strings.Contains(err.Error(), "SCRIPT_VERSION_CONFLICT") {
			t.Fatalf("finalize error = %v, want SCRIPT_VERSION_CONFLICT", err)
		}
		assertSourceToScriptPublicationRejected(t, ctx, pool, fixture, plan, "SCRIPT_VERSION_CONFLICT")
	})
}

func TestSourceToScriptManifestReindexesAfterChapterDeletionWithoutDroppingRetainedEpisodeIntegration(t *testing.T) {
	requireSourceToScriptIntegration(t)
	ctx := context.Background()
	pool := openNovelAdaptationTestDB(t, ctx)
	t.Cleanup(pool.Close)

	fixture := seedSourceToScriptGenerationFixture(t, ctx, pool, []sourceToScriptFixtureChapter{
		{volumeIndex: 1, sectionIndex: 1, title: "第一节", content: "source A", baseContent: "old A"},
		{volumeIndex: 2, sectionIndex: 1, title: "第一节", content: "source B", baseContent: "old B"},
		{volumeIndex: 3, sectionIndex: 1, title: "第一节", content: "source C", baseContent: "old C"},
	})
	chapterBID, chapterCID := fixture.chapterIDs[1], fixture.chapterIDs[2]
	if _, err := pool.Exec(ctx, `DELETE FROM novel_chapters WHERE id = $1`, fixture.chapterIDs[0]); err != nil {
		t.Fatalf("delete first chapter: %v", err)
	}
	plan := prepareSourceToScriptGenerationFixture(t, ctx, fixture, []string{chapterCID})
	if plan.SeriesEpisodeTotal != 2 || len(plan.Chapters) != 1 || plan.Chapters[0].ID != chapterCID || plan.Chapters[0].ManifestOrdinal != 2 {
		t.Fatalf("plan = %+v", plan)
	}
	queued := loadQueuedSourceToScriptEpisode(t, ctx, pool, fixture.workflowRunID, plan.Chapters[0])
	stageSourceToScriptEpisodeSuccess(t, ctx, pool, fixture.activities, queued, plan, fixture.userID, "new C")
	output, err := fixture.activities.FinalizeScriptFromSource(ctx, sourceToScriptFixtureInput(fixture), plan, SourceToScriptFinalization{RequestedEpisodeCount: 1, CompletedEpisodeCount: 1})
	if err != nil {
		t.Fatalf("FinalizeScriptFromSource: %v", err)
	}
	rows, err := pool.Query(ctx, `
		SELECT episode_index, source_chapter_id::text, content
		FROM script_episodes
		WHERE script_version_id = $1
		ORDER BY episode_index
	`, output.ScriptVersionID)
	if err != nil {
		t.Fatalf("list reindexed episodes: %v", err)
	}
	defer rows.Close()
	type episodeIdentity struct {
		index     int
		chapterID string
		content   string
	}
	items := make([]episodeIdentity, 0, 2)
	for rows.Next() {
		var item episodeIdentity
		if err := rows.Scan(&item.index, &item.chapterID, &item.content); err != nil {
			t.Fatalf("scan reindexed episode: %v", err)
		}
		items = append(items, item)
	}
	if len(items) != 2 || items[0].index != 1 || items[0].chapterID != chapterBID || items[0].content != "old B" ||
		items[1].index != 2 || items[1].chapterID != chapterCID || items[1].content != "new C" {
		t.Fatalf("reindexed episodes = %+v", items)
	}
}

func TestSourceToScriptManifestHandlesMiddleDeletionAndInsertionInOnePublicationIntegration(t *testing.T) {
	requireSourceToScriptIntegration(t)
	ctx := context.Background()
	pool := openNovelAdaptationTestDB(t, ctx)
	t.Cleanup(pool.Close)

	fixture := seedSourceToScriptGenerationFixture(t, ctx, pool, []sourceToScriptFixtureChapter{
		{volumeIndex: 1, sectionIndex: 1, title: "同名章节", content: "source A", baseContent: "old A"},
		{volumeIndex: 2, sectionIndex: 1, title: "同名章节", content: "source B", baseContent: "old B"},
		{volumeIndex: 3, sectionIndex: 1, title: "同名章节", content: "source C", baseContent: "old C"},
	})
	if _, err := pool.Exec(ctx, `DELETE FROM novel_chapters WHERE id = $1`, fixture.chapterIDs[1]); err != nil {
		t.Fatalf("delete middle chapter: %v", err)
	}
	var insertedChapterID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO novel_chapters(
			organization_id, project_id, source_id, chapter_index, volume_index, section_index,
			volume_title, chapter_title, content, event_state
		)
		VALUES ($1, $2, $3, 4, 2, 1, '第二卷', '同名章节', 'source D', 'pending')
		RETURNING id::text
	`, fixture.organizationID, fixture.projectID, fixture.sourceID).Scan(&insertedChapterID); err != nil {
		t.Fatalf("insert replacement chapter: %v", err)
	}

	plan := prepareSourceToScriptGenerationFixture(t, ctx, fixture, []string{fixture.chapterIDs[2], insertedChapterID})
	if plan.SeriesEpisodeTotal != 3 || len(plan.Chapters) != 2 ||
		plan.Chapters[0].ID != insertedChapterID || plan.Chapters[0].ManifestOrdinal != 2 ||
		plan.Chapters[1].ID != fixture.chapterIDs[2] || plan.Chapters[1].ManifestOrdinal != 3 {
		t.Fatalf("plan after delete/insert = %+v", plan)
	}
	for _, chapter := range plan.Chapters {
		queued := loadQueuedSourceToScriptEpisode(t, ctx, pool, fixture.workflowRunID, chapter)
		stageSourceToScriptEpisodeSuccess(t, ctx, pool, fixture.activities, queued, plan, fixture.userID, "new "+chapter.ID)
	}
	output, err := fixture.activities.FinalizeScriptFromSource(ctx, sourceToScriptFixtureInput(fixture), plan, SourceToScriptFinalization{
		RequestedEpisodeCount: 2, CompletedEpisodeCount: 2,
	})
	if err != nil {
		t.Fatalf("FinalizeScriptFromSource: %v", err)
	}
	rows, err := pool.Query(ctx, `
		SELECT episode_index, source_chapter_id::text, content
		FROM script_episodes
		WHERE script_version_id = $1
		ORDER BY episode_index
	`, output.ScriptVersionID)
	if err != nil {
		t.Fatalf("list final episodes: %v", err)
	}
	defer rows.Close()
	type episodeIdentity struct {
		index     int
		chapterID string
		content   string
	}
	items := make([]episodeIdentity, 0, 3)
	for rows.Next() {
		var item episodeIdentity
		if err := rows.Scan(&item.index, &item.chapterID, &item.content); err != nil {
			t.Fatalf("scan final episode: %v", err)
		}
		items = append(items, item)
	}
	if len(items) != 3 ||
		items[0] != (episodeIdentity{index: 1, chapterID: fixture.chapterIDs[0], content: "old A"}) ||
		items[1] != (episodeIdentity{index: 2, chapterID: insertedChapterID, content: "new " + insertedChapterID}) ||
		items[2] != (episodeIdentity{index: 3, chapterID: fixture.chapterIDs[2], content: "new " + fixture.chapterIDs[2]}) {
		t.Fatalf("final episodes after delete/insert = %+v", items)
	}
}

func TestPurgeExpiredSourceToScriptPayloadsKeepsFormalContentAndProvenanceIntegration(t *testing.T) {
	requireSourceToScriptIntegration(t)
	ctx := context.Background()
	pool := openNovelAdaptationTestDB(t, ctx)
	t.Cleanup(pool.Close)

	fixture := seedSourceToScriptGenerationFixture(t, ctx, pool, []sourceToScriptFixtureChapter{
		{volumeIndex: 1, sectionIndex: 1, title: "第一节", content: "large frozen source payload", baseContent: "old episode"},
	})
	plan := prepareSourceToScriptGenerationFixture(t, ctx, fixture, fixture.chapterIDs)
	queued := loadQueuedSourceToScriptEpisode(t, ctx, pool, fixture.workflowRunID, plan.Chapters[0])
	staged := stageSourceToScriptEpisodeSuccess(t, ctx, pool, fixture.activities, queued, plan, fixture.userID, "durable formal episode")
	output, err := fixture.activities.FinalizeScriptFromSource(ctx, sourceToScriptFixtureInput(fixture), plan, SourceToScriptFinalization{
		RequestedEpisodeCount: 1, CompletedEpisodeCount: 1,
	})
	if err != nil {
		t.Fatalf("FinalizeScriptFromSource: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE source_to_script_generations
		SET retention_expires_at = now() - interval '1 second'
		WHERE id = $1
	`, plan.GenerationID); err != nil {
		t.Fatalf("expire generation payload: %v", err)
	}

	purged, err := PurgeExpiredSourceToScriptPayloads(ctx, pool, 10)
	if err != nil {
		t.Fatalf("PurgeExpiredSourceToScriptPayloads: %v", err)
	}
	if purged.Generations != 1 || purged.Items != 1 || purged.Results != 1 {
		t.Fatalf("purge result = %+v", purged)
	}
	var itemContent, resultContent *string
	var itemHash, resultHash, formalContent, formalResultID string
	var generationPurgedAt, itemPurgedAt, resultPurgedAt *time.Time
	if err := pool.QueryRow(ctx, `
		SELECT source_content, source_content_hash, payload_purged_at
		FROM source_to_script_generation_items
		WHERE generation_id = $1
	`, plan.GenerationID).Scan(&itemContent, &itemHash, &itemPurgedAt); err != nil {
		t.Fatalf("load purged source item: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT content, content_hash, payload_purged_at
		FROM script_episode_generation_results
		WHERE id = $1
	`, staged.GenerationResultID).Scan(&resultContent, &resultHash, &resultPurgedAt); err != nil {
		t.Fatalf("load purged staging result: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT content, generation_result_id::text
		FROM script_episodes
		WHERE script_version_id = $1
	`, output.ScriptVersionID).Scan(&formalContent, &formalResultID); err != nil {
		t.Fatalf("load formal episode after purge: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT payload_purged_at FROM source_to_script_generations WHERE id = $1`, plan.GenerationID).Scan(&generationPurgedAt); err != nil {
		t.Fatalf("load generation purge marker: %v", err)
	}
	if itemContent != nil || resultContent != nil || itemHash == "" || resultHash == "" ||
		itemPurgedAt == nil || resultPurgedAt == nil || generationPurgedAt == nil ||
		formalContent != "durable formal episode" || formalResultID != staged.GenerationResultID {
		t.Fatalf("purged provenance mismatch: itemContent=%v resultContent=%v itemHash=%q resultHash=%q formal=%q result=%q", itemContent, resultContent, itemHash, resultHash, formalContent, formalResultID)
	}
	replayed, err := PurgeExpiredSourceToScriptPayloads(ctx, pool, 10)
	if err != nil {
		t.Fatalf("replay PurgeExpiredSourceToScriptPayloads: %v", err)
	}
	if replayed != (SourceToScriptPayloadPurgeResult{}) {
		t.Fatalf("replayed purge result = %+v, want zero", replayed)
	}
}

func requireSourceToScriptIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run source-to-script generation integration tests")
	}
}

func seedSourceToScriptGenerationFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	chapters []sourceToScriptFixtureChapter,
) sourceToScriptGenerationFixture {
	t.Helper()
	orgID, userID, projectID, workflowRunID, _, _ := seedWorkflowGatewayIntegrationData(t, ctx, pool)
	fixture := sourceToScriptGenerationFixture{
		organizationID: orgID, userID: userID, projectID: projectID, workflowRunID: workflowRunID,
		activities: NewActivities(pool, nil, nil),
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})
	sourceContent := make([]string, 0, len(chapters))
	for _, chapter := range chapters {
		sourceContent = append(sourceContent, chapter.content)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO project_sources(organization_id, project_id, source_type, title, content, content_format, status, created_by)
		VALUES ($1, $2, 'novel', 'Generation Source', $3, 'plain_text', 'ready', $4)
		RETURNING id::text
	`, orgID, projectID, strings.Join(sourceContent, "\n"), userID).Scan(&fixture.sourceID); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	for index, chapter := range chapters {
		var chapterID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO novel_chapters(
				organization_id, project_id, source_id, chapter_index, volume_index, section_index,
				volume_title, chapter_title, content, event_state
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending')
			RETURNING id::text
		`, orgID, projectID, fixture.sourceID, index+1, chapter.volumeIndex, chapter.sectionIndex,
			fmt.Sprintf("第%d卷", chapter.volumeIndex), chapter.title, chapter.content).Scan(&chapterID); err != nil {
			t.Fatalf("insert chapter %d: %v", index+1, err)
		}
		fixture.chapterIDs = append(fixture.chapterIDs, chapterID)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO scripts(organization_id, project_id, source_id, title, status, created_by)
		VALUES ($1, $2, $3, 'Generation Script', 'active', $4)
		RETURNING id::text
	`, orgID, projectID, fixture.sourceID, userID).Scan(&fixture.scriptID); err != nil {
		t.Fatalf("insert script: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO script_versions(
			organization_id, project_id, script_id, version_no, version, content,
			content_format, status, source_type, metadata, created_by
		)
		VALUES ($1, $2, $3, 1, 1, 'base script', 'markdown', 'active', 'manual', '{}', $4)
		RETURNING id::text
	`, orgID, projectID, fixture.scriptID, userID).Scan(&fixture.baseVersionID); err != nil {
		t.Fatalf("insert base script version: %v", err)
	}
	for index, chapter := range chapters {
		if chapter.baseContent == "" {
			continue
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO script_episodes(
				organization_id, project_id, script_id, script_version_id, source_id, source_chapter_id,
				episode_index, volume_index, section_index, volume_title, episode_title, content, created_by
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		`, orgID, projectID, fixture.scriptID, fixture.baseVersionID, fixture.sourceID, fixture.chapterIDs[index],
			index+1, chapter.volumeIndex, chapter.sectionIndex, fmt.Sprintf("第%d卷", chapter.volumeIndex), chapter.title,
			chapter.baseContent, userID); err != nil {
			t.Fatalf("insert base episode %d: %v", index+1, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE scripts SET current_version_id = $2 WHERE id = $1`, fixture.scriptID, fixture.baseVersionID); err != nil {
		t.Fatalf("activate base script version: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE projects SET active_script_id = $2 WHERE id = $1`, projectID, fixture.scriptID); err != nil {
		t.Fatalf("set active project script: %v", err)
	}
	return fixture
}

func prepareSourceToScriptGenerationFixture(
	t *testing.T,
	ctx context.Context,
	fixture sourceToScriptGenerationFixture,
	chapterIDs []string,
) SourceToScriptPlan {
	t.Helper()
	plan, err := fixture.activities.PrepareScriptFromSource(ctx, PrepareScriptFromSourceInput{GenerateScriptFromSourceInput: GenerateScriptFromSourceInput{
		OrganizationID: fixture.organizationID, ProjectID: fixture.projectID, WorkflowRunID: fixture.workflowRunID,
		CreatedBy: fixture.userID, SourceID: fixture.sourceID, TargetScriptID: fixture.scriptID,
		ChapterIDs: chapterIDs, IdempotencyKey: "source-to-script-" + fmt.Sprint(time.Now().UnixNano()),
	}})
	if err != nil {
		t.Fatalf("PrepareScriptFromSource: %v", err)
	}
	return plan
}

func sourceToScriptFixtureInput(fixture sourceToScriptGenerationFixture) GenerateScriptFromSourceInput {
	return GenerateScriptFromSourceInput{
		OrganizationID: fixture.organizationID, ProjectID: fixture.projectID,
		WorkflowRunID: fixture.workflowRunID, CreatedBy: fixture.userID, SourceID: fixture.sourceID,
		TargetScriptID: fixture.scriptID,
	}
}

func assertSourceToScriptPublicationRejected(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture sourceToScriptGenerationFixture,
	plan SourceToScriptPlan,
	wantCode string,
) {
	t.Helper()
	var versionCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM script_versions WHERE script_id = $1`, fixture.scriptID).Scan(&versionCount); err != nil {
		t.Fatalf("count script versions: %v", err)
	}
	if versionCount != 1 {
		t.Fatalf("script version count = %d, want 1 after rejected publication", versionCount)
	}
	var generationStatus, errorCode string
	if err := pool.QueryRow(ctx, `SELECT status, error_code FROM source_to_script_generations WHERE id = $1`, plan.GenerationID).Scan(&generationStatus, &errorCode); err != nil {
		t.Fatalf("load generation failure state: %v", err)
	}
	if generationStatus != "replan_required" || errorCode != wantCode {
		t.Fatalf("generation failure state = %s/%s, want replan_required/%s", generationStatus, errorCode, wantCode)
	}
}
