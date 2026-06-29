package workflows

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWorkflowGatewayIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run workflow gateway integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for workflow gateway integration tests")
	}

	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	orgID, userID, projectID, workflowRunID, textModelID, imageModelID := seedWorkflowGatewayIntegrationData(t, ctx, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})

	gateway := httptest.NewServer(mockWorkflowProviderGateway(t, textModelID, imageModelID))
	defer gateway.Close()

	activities := NewActivities(pool, newWorkflowMemoryStorage(), &provider.GatewayClient{
		BaseURL: gateway.URL,
		Token:   "workflow-service-token",
		Client:  gateway.Client(),
	})
	input := TextToStoryboardInput{
		OrganizationID: orgID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		Prompt:         "A quiet train station at sunrise with cinematic lighting.",
		CreatedBy:      userID,
	}
	storyboard, err := activities.GenerateStoryboardText(ctx, generateStoryboardTextInput(input))
	if err != nil {
		t.Fatalf("GenerateStoryboardText: %v", err)
	}
	finalOutput := BuildTextToStoryboardOutput(storyboard)
	if err := activities.CompleteTextToStoryboardWorkflow(ctx, input, finalOutput); err != nil {
		t.Fatalf("CompleteTextToStoryboardWorkflow: %v", err)
	}

	assertWorkflowGatewayNodeRuns(t, ctx, pool, workflowRunID)
	assertWorkflowGatewayStoryboardArtifact(t, ctx, pool, workflowRunID, storyboard.StoryboardArtifactID)
	assertWorkflowGatewayStoryboardShots(t, ctx, pool, workflowRunID, 1)
	assertWorkflowGatewayRunOutput(t, ctx, pool, workflowRunID)
	assertWorkflowGatewayEvents(t, ctx, pool, orgID, workflowRunID)
	assertWorkflowDidNotWriteProviderAccounting(t, ctx, pool, orgID)
}

func mockWorkflowProviderGateway(t *testing.T, textModelID, imageModelID string) http.Handler {
	t.Helper()
	_ = imageModelID
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer workflow-service-token" {
			t.Fatalf("Authorization header = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/internal/provider/text/generate":
			var req provider.GatewayTextRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode text request: %v", err)
			}
			if req.ModelProfileKey != scriptModelProfileKey || req.NodeRunID == "" {
				t.Fatalf("text gateway request = %+v", req)
			}
			if req.PromptTemplateKey != promptKeyStoryboardPlanner || req.PromptVersionID == "" || !strings.HasPrefix(req.PromptHash, "sha256:") || req.PromptSource == "" {
				t.Fatalf("text prompt trace = %+v", req)
			}
			writeWorkflowGatewayEnvelope(t, w, provider.GatewayTextResponse{
				ProviderCallID: uuid.NewString(),
				ModelID:        textModelID,
				Status:         "succeeded",
				Output: provider.GatewayTextOutput{Text: `{
					"title": "Sunrise Station",
					"summary": "A quiet cinematic opening.",
					"shots": [{
						"shotNo": 1,
						"duration": 3,
						"visual": "Wide sunrise platform",
						"camera": "slow dolly",
						"motion": "mist drifting",
						"mood": "hopeful",
						"imagePrompt": "Cinematic sunrise train station, high detail"
					}]
				}`},
				Usage:     provider.GatewayUsage{EstimatedCost: "0.00000000", Currency: "USD"},
				LatencyMS: 12,
			})
		case "/internal/provider/image/generate":
			t.Fatal("text_to_storyboard should not call image gateway")
		default:
			http.NotFound(w, r)
		}
	})
}

func writeWorkflowGatewayEnvelope(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(map[string]any{"data": data}); err != nil {
		t.Fatalf("encode gateway response: %v", err)
	}
}

func seedWorkflowGatewayIntegrationData(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, string, string, string, string, string) {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var orgID, userID, workspaceID, projectID, workflowRunID, connectorID, accountID, textModelID, imageModelID, scriptProfileID, imageProfileID string
	if err := pool.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ($1, $2) RETURNING id`, "Workflow Gateway", "workflow-gateway-"+suffix).Scan(&orgID); err != nil {
		t.Fatalf("insert organization: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email, display_name) VALUES ($1, $2) RETURNING id`, "workflow-gateway-"+suffix+"@example.test", "Workflow Gateway").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organization_members(organization_id, user_id) VALUES ($1, $2)`, orgID, userID); err != nil {
		t.Fatalf("insert organization member: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(organization_id, name) VALUES ($1, 'Workflow Workspace') RETURNING id`, orgID).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects(organization_id, workspace_id, name, created_by)
		VALUES ($1, $2, 'Workflow Project', $3)
		RETURNING id
	`, orgID, workspaceID, userID).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO project_members(project_id, user_id) VALUES ($1, $2)`, projectID, userID); err != nil {
		t.Fatalf("insert project member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workflow_runs(organization_id, project_id, temporal_workflow_id, status, input, output, created_by)
		VALUES ($1, $2, $3, 'queued', $4, '{}', $5)
		RETURNING id
	`, orgID, projectID, "workflow-gateway-"+suffix, mustJSON(map[string]any{"prompt": "train"}), userID).Scan(&workflowRunID); err != nil {
		t.Fatalf("insert workflow run: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_connectors(connector_key, name, type, is_official, manifest, version)
		VALUES ($1, 'Workflow Gateway Provider', 'http', true, '{}', 'v1')
		RETURNING id
	`, "workflow-gateway-provider-"+suffix).Scan(&connectorID); err != nil {
		t.Fatalf("insert provider connector: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM provider_connectors WHERE id = $1`, connectorID)
	})
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_accounts(organization_id, connector_id, name, base_url, auth_type, status, config, created_by)
		VALUES ($1, $2, 'Workflow Gateway Account', 'http://gateway.test', 'bearer', 'active', '{}', $3)
		RETURNING id
	`, orgID, connectorID, userID).Scan(&accountID); err != nil {
		t.Fatalf("insert provider account: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status)
		VALUES ($1, 'text-model', 'Text Model', 'text', 'active')
		RETURNING id
	`, accountID).Scan(&textModelID); err != nil {
		t.Fatalf("insert text model: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status)
		VALUES ($1, 'image-model', 'Image Model', 'image', 'active')
		RETURNING id
	`, accountID).Scan(&imageModelID); err != nil {
		t.Fatalf("insert image model: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO model_profiles(organization_id, profile_key, name, purpose, routing_strategy, fallback_strategy)
		VALUES ($1, $2, 'Script Default', 'script', 'priority', '{}')
		RETURNING id
	`, orgID, scriptModelProfileKey).Scan(&scriptProfileID); err != nil {
		t.Fatalf("insert script profile: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO model_profiles(organization_id, profile_key, name, purpose, routing_strategy, fallback_strategy)
		VALUES ($1, $2, 'Image Default', 'image', 'priority', '{}')
		RETURNING id
	`, orgID, imageGenerationModelProfileKey).Scan(&imageProfileID); err != nil {
		t.Fatalf("insert image profile: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO model_profile_bindings(model_profile_id, provider_model_id, priority, weight, enabled)
		VALUES ($1, $2, 100, 100, true), ($3, $4, 100, 100, true)
	`, scriptProfileID, textModelID, imageProfileID, imageModelID); err != nil {
		t.Fatalf("insert profile bindings: %v", err)
	}
	return orgID, userID, projectID, workflowRunID, textModelID, imageModelID
}

func assertWorkflowGatewayNodeRuns(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workflowRunID string) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT node_key, status
		FROM workflow_node_runs
		WHERE workflow_run_id = $1
	`, workflowRunID)
	if err != nil {
		t.Fatalf("select node runs: %v", err)
	}
	defer rows.Close()
	statuses := map[string]string{}
	for rows.Next() {
		var nodeKey, status string
		if err := rows.Scan(&nodeKey, &status); err != nil {
			t.Fatalf("scan node run: %v", err)
		}
		statuses[nodeKey] = status
	}
	if statuses[nodeGenerateStoryboardTextKey] != "succeeded" {
		t.Fatalf("node statuses = %#v", statuses)
	}
	if _, ok := statuses[nodeGenerateStoryboardImageKey]; ok {
		t.Fatalf("node statuses = %#v", statuses)
	}
}

func assertWorkflowGatewayStoryboardArtifact(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workflowRunID, artifactID string) {
	t.Helper()
	var artifactType, storageKey, promptHash string
	var metadata json.RawMessage
	if err := pool.QueryRow(ctx, `
		SELECT type, storage_key, prompt_hash, metadata
		FROM artifacts
		WHERE id = $1 AND workflow_run_id = $2
	`, artifactID, workflowRunID).Scan(&artifactType, &storageKey, &promptHash, &metadata); err != nil {
		t.Fatalf("select storyboard artifact: %v", err)
	}
	if artifactType != "storyboard_json" || storageKey == "" {
		t.Fatalf("artifact type/storageKey = %q/%q", artifactType, storageKey)
	}
	assertPromptTraceMetadata(t, metadata, promptKeyStoryboardPlanner)
	if !strings.HasPrefix(promptHash, "sha256:") {
		t.Fatalf("storyboard prompt hash = %q", promptHash)
	}
}

func assertPromptTraceMetadata(t *testing.T, metadata json.RawMessage, templateKey string) {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(metadata, &decoded); err != nil {
		t.Fatalf("decode artifact metadata: %v", err)
	}
	if decoded["promptTemplateKey"] != templateKey {
		t.Fatalf("promptTemplateKey = %v, want %s metadata=%s", decoded["promptTemplateKey"], templateKey, string(metadata))
	}
	if value, _ := decoded["promptVersionId"].(string); value == "" {
		t.Fatalf("promptVersionId missing metadata=%s", string(metadata))
	}
	if value, _ := decoded["promptHash"].(string); !strings.HasPrefix(value, "sha256:") {
		t.Fatalf("promptHash = %q metadata=%s", value, string(metadata))
	}
	if value, _ := decoded["promptSource"].(string); value == "" {
		t.Fatalf("promptSource missing metadata=%s", string(metadata))
	}
}

func assertWorkflowGatewayRunOutput(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workflowRunID string) {
	t.Helper()
	var status string
	var rawOutput json.RawMessage
	var output struct {
		Shots         []StoryboardShotRecord `json:"shots"`
		ProviderCalls struct {
			Storyboard string `json:"storyboard"`
		} `json:"providerCalls"`
	}
	if err := pool.QueryRow(ctx, `
		SELECT status, output
		FROM workflow_runs
		WHERE id = $1
	`, workflowRunID).Scan(&status, &rawOutput); err != nil {
		t.Fatalf("select workflow run: %v", err)
	}
	if err := json.Unmarshal(rawOutput, &output); err != nil {
		t.Fatalf("decode workflow output: %v", err)
	}
	if status != "succeeded" || len(output.Shots) != 1 || output.ProviderCalls.Storyboard == "" {
		t.Fatalf("workflow status/output = %s/%+v", status, output)
	}
}

func assertWorkflowGatewayStoryboardShots(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workflowRunID string, want int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM storyboard_shots WHERE workflow_run_id = $1`, workflowRunID).Scan(&count); err != nil {
		t.Fatalf("select storyboard_shots: %v", err)
	}
	if count != want {
		t.Fatalf("storyboard_shots count = %d, want %d", count, want)
	}
}

func assertWorkflowGatewayEvents(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, workflowRunID string) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT event_type
		FROM event_outbox
		WHERE organization_id = $1
		  AND (payload->>'workflowRunId' = $2 OR aggregate_id = $2::uuid)
	`, orgID, workflowRunID)
	if err != nil {
		t.Fatalf("select events: %v", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var eventType string
		if err := rows.Scan(&eventType); err != nil {
			t.Fatalf("scan event: %v", err)
		}
		seen[eventType] = true
	}
	for _, eventType := range []string{"workflow.node.completed", "artifact.created", "storyboard.shots.created", "workflow.run.completed"} {
		if !seen[eventType] {
			t.Fatalf("missing event %s in %#v", eventType, seen)
		}
	}
}

func assertWorkflowDidNotWriteProviderAccounting(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID string) {
	t.Helper()
	var callCount, costCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM provider_call_logs WHERE organization_id = $1`, orgID).Scan(&callCount); err != nil {
		t.Fatalf("select provider_call_logs: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM cost_records WHERE organization_id = $1`, orgID).Scan(&costCount); err != nil {
		t.Fatalf("select cost_records: %v", err)
	}
	if callCount != 0 || costCount != 0 {
		t.Fatalf("worker wrote provider accounting rows: calls=%d costs=%d", callCount, costCount)
	}
}

type workflowMemoryStorage struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newWorkflowMemoryStorage() *workflowMemoryStorage {
	return &workflowMemoryStorage{objects: map[string][]byte{}}
}

func (s *workflowMemoryStorage) PutJSON(ctx context.Context, key string, value any) (storage.PutResult, error) {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return storage.PutResult{}, err
	}
	s.mu.Lock()
	s.objects[key] = bytes.Clone(body)
	s.mu.Unlock()
	sum := sha256.Sum256(body)
	return storage.PutResult{
		StorageKey:  key,
		ContentHash: "sha256:" + hex.EncodeToString(sum[:]),
		ByteSize:    int64(len(body)),
	}, nil
}
