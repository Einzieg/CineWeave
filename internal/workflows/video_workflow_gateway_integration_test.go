package workflows

import (
	"context"
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
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestVideoProductionWorkflowGatewayIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run video workflow gateway integration test")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for video workflow gateway integration test")
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
	videoModelID := seedWorkflowVideoProfile(t, ctx, pool, orgID, userID, textModelID)

	gateway := httptest.NewServer(mockVideoWorkflowProviderGateway(t, pool, textModelID, imageModelID, videoModelID))
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
		Input:          json.RawMessage(`{"duration":5,"aspectRatio":"16:9","resolution":"720p","pollIntervalSeconds":1,"maxPolls":2}`),
	}

	storyboard, err := activities.GenerateStoryboardText(ctx, generateStoryboardTextInput(input))
	if err != nil {
		t.Fatalf("GenerateStoryboardText: %v", err)
	}
	imageOutput, err := activities.GenerateStoryboardImage(ctx, GenerateStoryboardImageInput{
		OrganizationID:         input.OrganizationID,
		ProjectID:              input.ProjectID,
		WorkflowRunID:          input.WorkflowRunID,
		Prompt:                 input.Prompt,
		CreatedBy:              input.CreatedBy,
		StoryboardArtifactID:   storyboard.StoryboardArtifactID,
		Storyboard:             storyboard.Storyboard,
		StoryboardProviderCall: storyboard.ProviderCallID,
	})
	if err != nil {
		t.Fatalf("GenerateStoryboardImage: %v", err)
	}
	createOutput, err := activities.CreateStoryboardVideoTask(ctx, CreateStoryboardVideoTaskInput{
		OrganizationID:       input.OrganizationID,
		ProjectID:            input.ProjectID,
		WorkflowRunID:        input.WorkflowRunID,
		CreatedBy:            input.CreatedBy,
		StoryboardArtifactID: storyboard.StoryboardArtifactID,
		ImageArtifactID:      imageOutput.ImageArtifactID,
		ImageMediaFileID:     imageOutput.ImageMediaFileID,
		ImageStorageKey:      imageOutput.ImageStorageKey,
		Prompt:               input.Prompt,
		VideoPrompt:          selectVideoPrompt(storyboard.Storyboard, input.Prompt, 5),
		Duration:             5,
		AspectRatio:          "16:9",
		Resolution:           "720p",
		Storyboard:           storyboard.Storyboard,
	})
	if err != nil {
		t.Fatalf("CreateStoryboardVideoTask: %v", err)
	}
	firstPoll, err := activities.PollStoryboardVideoTask(ctx, PollStoryboardVideoTaskInput{
		OrganizationID:      input.OrganizationID,
		ProjectID:           input.ProjectID,
		WorkflowRunID:       input.WorkflowRunID,
		NodeRunID:           createOutput.NodeRunID,
		ProviderAsyncTaskID: createOutput.ProviderAsyncTaskID,
		ExternalTaskID:      createOutput.ExternalTaskID,
		PollCount:           1,
	})
	if err != nil {
		t.Fatalf("PollStoryboardVideoTask first: %v", err)
	}
	if firstPoll.Status != "running" {
		t.Fatalf("first poll status = %q, want running", firstPoll.Status)
	}
	secondPoll, err := activities.PollStoryboardVideoTask(ctx, PollStoryboardVideoTaskInput{
		OrganizationID:      input.OrganizationID,
		ProjectID:           input.ProjectID,
		WorkflowRunID:       input.WorkflowRunID,
		NodeRunID:           createOutput.NodeRunID,
		ProviderAsyncTaskID: createOutput.ProviderAsyncTaskID,
		ExternalTaskID:      createOutput.ExternalTaskID,
		PollCount:           2,
	})
	if err != nil {
		t.Fatalf("PollStoryboardVideoTask second: %v", err)
	}
	output := BuildVideoProductionOutput(storyboard, imageOutput, createOutput, secondPoll)
	if err := activities.CompleteVideoProductionWorkflow(ctx, input, output); err != nil {
		t.Fatalf("CompleteVideoProductionWorkflow: %v", err)
	}

	assertVideoWorkflowRunSucceeded(t, ctx, pool, workflowRunID)
	assertVideoWorkflowNodeRuns(t, ctx, pool, workflowRunID)
	assertVideoWorkflowArtifacts(t, ctx, pool, workflowRunID)
	assertVideoWorkflowEvents(t, ctx, pool, orgID, workflowRunID)
	assertWorkflowDidNotWriteProviderAccounting(t, ctx, pool, orgID)
}

func seedWorkflowVideoProfile(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, userID, textModelID string) string {
	t.Helper()
	var accountID string
	if err := pool.QueryRow(ctx, `SELECT provider_account_id FROM provider_models WHERE id = $1`, textModelID).Scan(&accountID); err != nil {
		t.Fatalf("select provider account: %v", err)
	}
	var videoModelID, videoProfileID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status)
		VALUES ($1, 'video-model', 'Video Model', 'video', 'active')
		RETURNING id
	`, accountID).Scan(&videoModelID); err != nil {
		t.Fatalf("insert video model: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO model_profiles(organization_id, profile_key, name, purpose, routing_strategy, fallback_strategy)
		VALUES ($1, $2, 'Video Default', 'video', 'priority', '{}')
		RETURNING id
	`, orgID, videoGenerationModelProfileKey).Scan(&videoProfileID); err != nil {
		t.Fatalf("insert video profile: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO model_profile_bindings(model_profile_id, provider_model_id, priority, weight, enabled)
		VALUES ($1, $2, 100, 100, true)
	`, videoProfileID, videoModelID); err != nil {
		t.Fatalf("insert video binding: %v", err)
	}
	_ = userID
	return videoModelID
}

func mockVideoWorkflowProviderGateway(t *testing.T, pool *pgxpool.Pool, textModelID, imageModelID, videoModelID string) http.Handler {
	t.Helper()
	var mu sync.Mutex
	pollCountByTask := map[string]int{}
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
			writeWorkflowGatewayEnvelope(t, w, provider.GatewayTextResponse{
				ProviderCallID: uuid.NewString(),
				ModelID:        textModelID,
				Status:         "succeeded",
				Output: provider.GatewayTextOutput{Text: `{
					"title": "Sunrise Station",
					"summary": "A quiet cinematic opening.",
					"shots": [{
						"shotNo": 1,
						"duration": 5,
						"visual": "Wide sunrise platform",
						"camera": "slow push-in",
						"motion": "mist drifting",
						"mood": "hopeful",
						"imagePrompt": "Cinematic sunrise train station, high detail",
						"videoPrompt": "A quiet train platform at sunrise with mist and soft light"
					}]
				}`},
				Usage:     provider.GatewayUsage{EstimatedCost: "0.00000000", Currency: "USD"},
				LatencyMS: 12,
			})
		case "/internal/provider/image/generate":
			var req provider.GatewayImageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode image request: %v", err)
			}
			artifactID, mediaFileID, storageKey := insertMockGatewayMediaArtifact(t, r.Context(), pool, req.OrganizationID, req.ProjectID, req.WorkflowRunID, req.NodeRunID, imageModelID, "generated_image", "image/png")
			writeWorkflowGatewayEnvelope(t, w, provider.GatewayImageResponse{
				ProviderCallID: uuid.NewString(),
				ModelID:        imageModelID,
				Status:         "succeeded",
				Output: provider.GatewayImageOutput{
					ArtifactID:  artifactID,
					MediaFileID: mediaFileID,
					StorageKey:  storageKey,
					MimeType:    "image/png",
				},
				Usage:     provider.GatewayUsage{EstimatedCost: "0.00000000", Currency: "USD"},
				LatencyMS: 20,
			})
		case "/internal/provider/video/create-task":
			var req provider.GatewayVideoCreateTaskRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode video create request: %v", err)
			}
			if req.ModelProfileKey != videoGenerationModelProfileKey || req.NodeRunID == "" || len(req.References) != 1 {
				t.Fatalf("video create request = %+v", req)
			}
			writeWorkflowGatewayEnvelope(t, w, provider.GatewayVideoCreateTaskResponse{
				ProviderCallID:      uuid.NewString(),
				ProviderAsyncTaskID: uuid.NewString(),
				ExternalTaskID:      "external-video-task",
				ModelID:             videoModelID,
				Status:              "running",
				LatencyMS:           25,
			})
		case "/internal/provider/video/poll-task":
			var req provider.GatewayVideoPollTaskRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode video poll request: %v", err)
			}
			mu.Lock()
			pollCountByTask[req.ProviderAsyncTaskID]++
			pollCount := pollCountByTask[req.ProviderAsyncTaskID]
			mu.Unlock()
			response := provider.GatewayVideoPollTaskResponse{
				ProviderCallID:      uuid.NewString(),
				ProviderAsyncTaskID: req.ProviderAsyncTaskID,
				ExternalTaskID:      req.ExternalTaskID,
				ModelID:             videoModelID,
				Status:              "running",
				Usage:               provider.GatewayUsage{EstimatedCost: "0.00000000", Currency: "USD"},
				LatencyMS:           18,
			}
			if pollCount >= 2 {
				artifactID, mediaFileID, storageKey := insertMockGatewayMediaArtifact(t, r.Context(), pool, req.OrganizationID, req.ProjectID, req.WorkflowRunID, req.NodeRunID, videoModelID, "generated_video", "video/mp4")
				duration := 5.0
				response.Status = "succeeded"
				response.Output = provider.GatewayVideoOutput{
					ArtifactID:      artifactID,
					MediaFileID:     mediaFileID,
					StorageKey:      storageKey,
					MimeType:        "video/mp4",
					DurationSeconds: &duration,
				}
			}
			writeWorkflowGatewayEnvelope(t, w, response)
		default:
			http.NotFound(w, r)
		}
	})
}

func insertMockGatewayMediaArtifact(t *testing.T, ctx context.Context, pool *pgxpool.Pool, organizationID, projectID, workflowRunID, nodeRunID, modelID, artifactType, mimeType string) (string, string, string) {
	t.Helper()
	storageKey := fmt.Sprintf("org/%s/project/%s/workflow/%s/mock/%s-%d", organizationID, projectID, workflowRunID, artifactType, time.Now().UnixNano())
	var artifactID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, model_id, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'sha256:mock', $8, $9)
		RETURNING id
	`, organizationID, projectID, workflowRunID, nodeRunID, artifactType, storageKey, mimeType, modelID, mustJSON(map[string]any{"source": "mock_provider_gateway"})).Scan(&artifactID); err != nil {
		t.Fatalf("insert mock artifact: %v", err)
	}
	var mediaFileID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_files(organization_id, project_id, artifact_id, storage_key, mime_type, byte_size, checksum, metadata)
		VALUES ($1, $2, $3, $4, $5, 128, 'sha256:mock', $6)
		RETURNING id
	`, organizationID, projectID, artifactID, storageKey, mimeType, mustJSON(map[string]any{"source": "mock_provider_gateway"})).Scan(&mediaFileID); err != nil {
		t.Fatalf("insert mock media file: %v", err)
	}
	return artifactID, mediaFileID, storageKey
}

func assertVideoWorkflowRunSucceeded(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workflowRunID string) {
	t.Helper()
	var status string
	var rawOutput json.RawMessage
	var output VideoProductionOutput
	if err := pool.QueryRow(ctx, `SELECT status, output FROM workflow_runs WHERE id = $1`, workflowRunID).Scan(&status, &rawOutput); err != nil {
		t.Fatalf("select workflow run: %v", err)
	}
	if err := json.Unmarshal(rawOutput, &output); err != nil {
		t.Fatalf("decode workflow output: %v", err)
	}
	if status != "succeeded" || output.VideoArtifactID == "" || output.VideoMediaFileID == "" || output.VideoStorageKey == "" || output.ProviderCalls["videoCreate"] == "" || output.ProviderCalls["videoPoll"] == "" {
		t.Fatalf("workflow status/output = %s/%+v", status, output)
	}
}

func assertVideoWorkflowNodeRuns(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workflowRunID string) {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT node_key, status FROM workflow_node_runs WHERE workflow_run_id = $1`, workflowRunID)
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
	for _, nodeKey := range []string{nodeGenerateStoryboardTextKey, nodeGenerateStoryboardImageKey, nodeGenerateStoryboardVideoKey} {
		if statuses[nodeKey] != "succeeded" {
			t.Fatalf("node statuses = %#v", statuses)
		}
	}
}

func assertVideoWorkflowArtifacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workflowRunID string) {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT type FROM artifacts WHERE workflow_run_id = $1`, workflowRunID)
	if err != nil {
		t.Fatalf("select artifacts: %v", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var artifactType string
		if err := rows.Scan(&artifactType); err != nil {
			t.Fatalf("scan artifact: %v", err)
		}
		seen[artifactType] = true
	}
	for _, artifactType := range []string{"storyboard_json", "generated_image", "generated_video"} {
		if !seen[artifactType] {
			t.Fatalf("missing artifact type %s in %#v", artifactType, seen)
		}
	}
}

func assertVideoWorkflowEvents(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, workflowRunID string) {
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
	for _, eventType := range []string{"workflow.node.started", "workflow.node.progress", "workflow.node.completed", "artifact.created", "workflow.run.completed"} {
		if !seen[eventType] {
			t.Fatalf("missing event %s in %#v", eventType, seen)
		}
	}
}
