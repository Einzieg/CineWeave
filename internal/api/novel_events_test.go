package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/google/uuid"
)

func TestPatchNovelEventMarksManualOverride(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	sourceID := seed.insertProjectSource(t, "novel", "Novel Source")
	chapterID := seed.insertNovelChapter(t, sourceID)
	eventID := seed.insertNovelEvent(t, sourceID, chapterID, 1, "Original Event", "Original summary", "pending")

	var updated NovelEvent
	doAPISuccess(t, server, http.MethodPatch, "/api/projects/"+seed.projectID+"/novel-events/"+eventID, seed.ownerToken, seed.organizationID, map[string]any{
		"expectedRevision": 1,
		"title":            "Manual Event",
		"summary":          "Manual summary",
		"importance":       4,
		"adaptationHint":   "Keep the station reveal.",
	}, &updated, map[string]string{"Idempotency-Key": "novel-event-manual-update-test"})
	if !updated.ManualOverride || updated.ReviewStatus != "pending" || updated.Title != "Manual Event" || updated.Importance != 4 || updated.EditedBy == nil || *updated.EditedBy != seed.ownerUserID || updated.EditedAt == nil {
		t.Fatalf("updated event = %+v", updated)
	}
	var manualOverride bool
	var editedBy string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT manual_override, edited_by::text
		FROM novel_events
		WHERE id = $1 AND project_id = $2
	`, eventID, seed.projectID).Scan(&manualOverride, &editedBy); err != nil {
		t.Fatalf("select updated novel event: %v", err)
	}
	if !manualOverride || editedBy != seed.ownerUserID {
		t.Fatalf("manual override = %v editedBy=%s", manualOverride, editedBy)
	}
}

func TestGenerateAdaptationPlanWritesPlan(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/text/generate" {
			http.NotFound(w, r)
			return
		}
		var req provider.GatewayTextRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode gateway request: %v", err)
		}
		if req.PromptTemplateKey != "adaptation_plan_generation" || len(req.Input) == 0 {
			t.Fatalf("gateway request = %+v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"data": provider.GatewayTextResponse{
			ProviderCallID: uuid.NewString(),
			ModelID:        "model-test",
			Status:         "succeeded",
			Output: provider.GatewayTextOutput{Text: `{
				"title": "Station Adaptation",
				"logline": "A stranger finds a clue at dawn.",
				"theme": "choice",
				"structure": {"opening": "Dawn station", "development": "Clue appears", "climax": "Decision", "ending": "Departure"},
				"selectedEvents": ["1"],
				"omittedEvents": [{"event": "Side thread", "reason": "Too long"}],
				"visualStrategy": "Use clean silhouettes.",
				"characterStrategy": "Keep only the protagonist.",
				"shotStrategy": "Three concise shots.",
				"estimatedShots": 3,
				"notes": "Silent-video friendly."
			}`},
			Usage:     provider.GatewayUsage{EstimatedCost: "0.00000000", Currency: "USD"},
			LatencyMS: 10,
		}}); err != nil {
			t.Fatalf("encode gateway response: %v", err)
		}
	}))
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "novel-event-test-token")

	sourceID := seed.insertProjectSource(t, "novel", "Novel Source")
	chapterID := seed.insertNovelChapter(t, sourceID)
	eventID := seed.insertNovelEvent(t, sourceID, chapterID, 1, "Station clue", "A clue appears at the station.", "pending")

	var plan AdaptationPlan
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/sources/"+sourceID+"/generate-adaptation-plan", seed.ownerToken, seed.organizationID, map[string]any{
		"targetFormat":          "short_video",
		"targetDurationSeconds": 30,
		"maxShots":              3,
		"instruction":           "Keep it visual.",
	}, &plan)
	if plan.ID == "" || plan.SourceID == nil || *plan.SourceID != sourceID || plan.Title != "Station Adaptation" {
		t.Fatalf("plan = %+v", plan)
	}
	var selectedEventIDs []string
	if err := json.Unmarshal(plan.SelectedEventIDs, &selectedEventIDs); err != nil {
		t.Fatalf("decode selected events: %v", err)
	}
	if len(selectedEventIDs) != 1 || selectedEventIDs[0] != eventID {
		t.Fatalf("selected events = %+v, want %s", selectedEventIDs, eventID)
	}
	var metadata struct {
		Warning        string `json:"warning"`
		Logline        string `json:"logline"`
		VisualStrategy string `json:"visualStrategy"`
	}
	if err := json.Unmarshal(plan.Metadata, &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if metadata.Warning == "" || metadata.Logline == "" || metadata.VisualStrategy == "" {
		t.Fatalf("metadata = %+v", metadata)
	}
	var count int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT count(*)
		FROM adaptation_plans
		WHERE id = $1 AND project_id = $2 AND source_id = $3
	`, plan.ID, seed.projectID, sourceID).Scan(&count); err != nil {
		t.Fatalf("count adaptation plans: %v", err)
	}
	if count != 1 {
		t.Fatalf("adaptation plan count = %d, want 1", count)
	}
}

func TestGenerateScriptFromAdaptationPlanUsesNovelChapterText(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/text/generate" {
			http.NotFound(w, r)
			return
		}
		var req provider.GatewayTextRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode gateway request: %v", err)
		}
		if req.PromptTemplateKey != "script_from_adaptation_plan" {
			t.Fatalf("prompt template = %q", req.PromptTemplateKey)
		}
		var input struct {
			Prompt string `json:"prompt"`
		}
		if err := json.Unmarshal(req.Input, &input); err != nil {
			t.Fatalf("decode gateway input: %v", err)
		}
		if !strings.Contains(input.Prompt, "小说正文：") || !strings.Contains(input.Prompt, "chapter content") {
			t.Fatalf("rendered prompt missing chapter text: %s", input.Prompt)
		}
		if !strings.Contains(input.Prompt, "台词零捏造") {
			t.Fatalf("rendered prompt missing faithful dialogue rule: %s", input.Prompt)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"data": provider.GatewayTextResponse{
			ProviderCallID: uuid.NewString(),
			ModelID:        "model-test",
			Status:         "succeeded",
			Output:         provider.GatewayTextOutput{Text: "分镜1.1\n时间：夜\n场景：山寨\n人物：少年时期的方源\n道具：无\n预估时长：6秒\n\n△雨声压住窗棂。"},
			Usage:          provider.GatewayUsage{EstimatedCost: "0.00000000", Currency: "USD"},
			LatencyMS:      10,
		}}); err != nil {
			t.Fatalf("encode gateway response: %v", err)
		}
	}))
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "novel-script-test-token")

	sourceID := seed.insertProjectSource(t, "novel", "Novel Source")
	chapterID := seed.insertNovelChapter(t, sourceID)
	eventID := seed.insertNovelEvent(t, sourceID, chapterID, 1, "Station clue", "A clue appears at the station.", "pending")
	var planID string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO adaptation_plans(organization_id, project_id, source_id, title, selected_event_ids, structure, content, max_shots, created_by)
		VALUES ($1, $2, $3, 'Faithful Plan', $4, '{}', '{"title":"Faithful Plan"}', 6, $5)
		RETURNING id
	`, seed.organizationID, seed.projectID, sourceID, json.RawMessage(mustMarshal([]string{eventID})), seed.ownerUserID).Scan(&planID); err != nil {
		t.Fatalf("insert adaptation plan: %v", err)
	}

	var response struct {
		ScriptID  string `json:"scriptId"`
		VersionID string `json:"versionId"`
		Content   string `json:"content"`
	}
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/adaptation-plans/"+planID+"/generate-script", seed.ownerToken, seed.organizationID, map[string]any{}, &response)
	if response.ScriptID == "" || response.VersionID == "" || !strings.Contains(response.Content, "分镜1.1") {
		t.Fatalf("response = %+v", response)
	}
}

func TestGenerateScriptFromAdaptationPlanCreatesEpisodePerNovelChapter(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	gatewayCalls := 0
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/text/generate" {
			t.Fatalf("unexpected gateway path %s", r.URL.Path)
		}
		gatewayCalls++
		var req provider.GatewayTextRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode gateway request: %v", err)
		}
		var input struct {
			Prompt string `json:"prompt"`
		}
		if err := json.Unmarshal(req.Input, &input); err != nil {
			t.Fatalf("decode gateway input: %v", err)
		}
		expected := fmt.Sprintf("第%d节原文", gatewayCalls)
		if !strings.Contains(input.Prompt, expected) {
			t.Fatalf("prompt for call %d missing %q: %s", gatewayCalls, expected, input.Prompt)
		}
		unexpected := fmt.Sprintf("第%d节原文", gatewayCalls+1)
		if gatewayCalls < 3 && strings.Contains(input.Prompt, unexpected) {
			t.Fatalf("prompt for call %d leaked next chapter %q: %s", gatewayCalls, unexpected, input.Prompt)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"data": provider.GatewayTextResponse{
			ProviderCallID: uuid.NewString(),
			ModelID:        "model-test",
			Status:         "succeeded",
			Output:         provider.GatewayTextOutput{Text: fmt.Sprintf("第 %d 集剧本正文", gatewayCalls)},
		}}); err != nil {
			t.Fatalf("encode gateway response: %v", err)
		}
	}))
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "novel-script-test-token")

	sourceID := seed.insertProjectSource(t, "novel", "三集小说")
	eventIDs := make([]string, 0, 3)
	for i := 1; i <= 3; i++ {
		chapterID := seed.insertNovelChapterWithOrdinals(t, sourceID, i, 1, i, fmt.Sprintf("第%d节", i), fmt.Sprintf("第%d节原文。", i))
		eventIDs = append(eventIDs, seed.insertNovelEvent(t, sourceID, chapterID, i, fmt.Sprintf("事件%d", i), fmt.Sprintf("事件%d摘要", i), "pending"))
	}
	var planID string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO adaptation_plans(organization_id, project_id, source_id, title, selected_event_ids, structure, content, max_shots, created_by)
		VALUES ($1, $2, $3, 'Three Episodes Plan', $4, '{}', '{"title":"Three Episodes Plan"}', 6, $5)
		RETURNING id
	`, seed.organizationID, seed.projectID, sourceID, json.RawMessage(mustMarshal(eventIDs)), seed.ownerUserID).Scan(&planID); err != nil {
		t.Fatalf("insert adaptation plan: %v", err)
	}

	var response struct {
		ScriptID     string `json:"scriptId"`
		VersionID    string `json:"versionId"`
		EpisodeCount int    `json:"episodeCount"`
		Content      string `json:"content"`
	}
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/adaptation-plans/"+planID+"/generate-script", seed.ownerToken, seed.organizationID, map[string]any{}, &response)
	if gatewayCalls != 3 {
		t.Fatalf("gateway calls = %d, want 3", gatewayCalls)
	}
	if response.ScriptID == "" || response.VersionID == "" || response.EpisodeCount != 3 {
		t.Fatalf("response = %+v, want script/version and 3 episodes", response)
	}
	var episodeCount, distinctSourceChapters int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT count(*), count(DISTINCT source_chapter_id)
		FROM script_episodes
		WHERE project_id = $1 AND script_id = $2 AND script_version_id = $3
	`, seed.projectID, response.ScriptID, response.VersionID).Scan(&episodeCount, &distinctSourceChapters); err != nil {
		t.Fatalf("count script episodes: %v", err)
	}
	if episodeCount != 3 || distinctSourceChapters != 3 {
		t.Fatalf("episodes=%d distinctSourceChapters=%d, want 3/3", episodeCount, distinctSourceChapters)
	}
}

func (s *artifactPreviewSeed) insertNovelChapter(t *testing.T, sourceID string) string {
	t.Helper()
	var id string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO novel_chapters(organization_id, project_id, source_id, chapter_index, chapter_title, content, event_state)
		VALUES ($1, $2, $3, 1, 'Chapter One', 'chapter content', 'pending')
		RETURNING id
	`, s.organizationID, s.projectID, sourceID).Scan(&id); err != nil {
		t.Fatalf("insert novel chapter: %v", err)
	}
	return id
}

func (s *artifactPreviewSeed) insertNovelEvent(t *testing.T, sourceID, chapterID string, eventIndex int, title, summary, reviewStatus string) string {
	t.Helper()
	var id string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO novel_events(
			organization_id, project_id, source_id, chapter_id, event_index, sequence_no,
			title, summary, importance, review_status, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 3, $9, $10)
		RETURNING id
	`, s.organizationID, s.projectID, sourceID, chapterID, eventIndex, 1000+eventIndex, title, summary, reviewStatus, s.ownerUserID).Scan(&id); err != nil {
		t.Fatalf("insert novel event: %v", err)
	}
	return id
}
