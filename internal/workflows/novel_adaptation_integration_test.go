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
	defer pool.Close()

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
	defer pool.Close()

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
	output, err := activities.createGeneratedScriptFromPlan(ctx, GenerateScriptFromPlanInput{
		OrganizationID: seed.orgID,
		ProjectID:      seed.projectID,
		WorkflowRunID:  seed.workflowRunID,
		CreatedBy:      seed.userID,
		PlanID:         planID,
	}, adaptationPlanRecord{ID: planID, SourceID: seed.source.ID, Title: "Plan A", Content: `{}`, Structure: []byte(`{}`)}, promptsvc.RenderedPrompt{
		TemplateKey:     promptKeyScriptFromAdaptationPlan,
		RenderedHash:    "sha256:test",
		RenderedText:    "prompt",
		PromptVersionID: "",
	}, provider.GatewayTextResponse{ProviderCallID: uuid.NewString(), ModelID: "model-1"}, "script content")
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
	defer pool.Close()

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
