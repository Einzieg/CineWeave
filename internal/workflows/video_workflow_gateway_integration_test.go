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
	shots, err := activities.ListStoryboardShots(ctx, ListStoryboardShotsInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
	})
	if err != nil {
		t.Fatalf("ListStoryboardShots: %v", err)
	}
	if len(shots) != 2 {
		t.Fatalf("shots len = %d, want 2: %+v", len(shots), shots)
	}
	providerCalls := VideoProductionProviderCalls{Storyboard: storyboard.ProviderCallID}
	shotOutputs := make([]VideoProductionShotOutput, 0, len(shots))
	for _, shot := range shots {
		imageOutput, err := activities.GenerateShotImage(ctx, GenerateShotImageInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			CreatedBy:      input.CreatedBy,
			ShotID:         shot.ID,
			ShotIndex:      shot.ShotIndex,
			ShotNo:         shot.ShotNo,
			WorkflowPrompt: input.Prompt,
			AspectRatio:    "16:9",
		})
		if err != nil {
			t.Fatalf("GenerateShotImage shot %d: %v", shot.ShotIndex, err)
		}
		providerCalls.Images = append(providerCalls.Images, imageOutput.ProviderCallID)
		createOutput, err := activities.CreateShotVideoTask(ctx, CreateShotVideoTaskInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			CreatedBy:      input.CreatedBy,
			ShotID:         shot.ID,
			ShotIndex:      shot.ShotIndex,
			ShotNo:         shot.ShotNo,
			WorkflowPrompt: input.Prompt,
			Duration:       shot.Duration,
			AspectRatio:    "16:9",
			Resolution:     "720p",
		})
		if err != nil {
			t.Fatalf("CreateShotVideoTask shot %d: %v", shot.ShotIndex, err)
		}
		providerCalls.VideoCreates = append(providerCalls.VideoCreates, createOutput.ProviderCallID)
		firstPoll, err := activities.PollShotVideoTask(ctx, PollShotVideoTaskInput{
			OrganizationID:      input.OrganizationID,
			ProjectID:           input.ProjectID,
			WorkflowRunID:       input.WorkflowRunID,
			ShotID:              shot.ID,
			ShotIndex:           shot.ShotIndex,
			ShotNo:              shot.ShotNo,
			NodeRunID:           createOutput.NodeRunID,
			ProviderAsyncTaskID: createOutput.ProviderAsyncTaskID,
			ExternalTaskID:      createOutput.ExternalTaskID,
			PollCount:           1,
		})
		if err != nil {
			t.Fatalf("PollShotVideoTask first shot %d: %v", shot.ShotIndex, err)
		}
		providerCalls.VideoPolls = append(providerCalls.VideoPolls, firstPoll.ProviderCallID)
		if firstPoll.Status != "running" {
			t.Fatalf("first poll status = %q, want running", firstPoll.Status)
		}
		secondPoll, err := activities.PollShotVideoTask(ctx, PollShotVideoTaskInput{
			OrganizationID:      input.OrganizationID,
			ProjectID:           input.ProjectID,
			WorkflowRunID:       input.WorkflowRunID,
			ShotID:              shot.ID,
			ShotIndex:           shot.ShotIndex,
			ShotNo:              shot.ShotNo,
			NodeRunID:           createOutput.NodeRunID,
			ProviderAsyncTaskID: createOutput.ProviderAsyncTaskID,
			ExternalTaskID:      createOutput.ExternalTaskID,
			PollCount:           2,
		})
		if err != nil {
			t.Fatalf("PollShotVideoTask second shot %d: %v", shot.ShotIndex, err)
		}
		providerCalls.VideoPolls = append(providerCalls.VideoPolls, secondPoll.ProviderCallID)
		shotOutputs = append(shotOutputs, VideoProductionShotOutput{
			ShotID:              shot.ID,
			ShotIndex:           shot.ShotIndex,
			ShotNo:              shot.ShotNo,
			Duration:            shot.Duration,
			ImageArtifactID:     imageOutput.ImageArtifactID,
			ImageMediaFileID:    imageOutput.ImageMediaFileID,
			ImageStorageKey:     imageOutput.ImageStorageKey,
			VideoArtifactID:     secondPoll.ArtifactID,
			VideoMediaFileID:    secondPoll.MediaFileID,
			VideoStorageKey:     secondPoll.StorageKey,
			ProviderAsyncTaskID: createOutput.ProviderAsyncTaskID,
			ExternalTaskID:      secondPoll.ExternalTaskID,
		})
	}
	output := BuildMultiShotVideoProductionOutput(storyboard, shotOutputs, providerCalls)
	if err := activities.CompleteVideoProductionWorkflow(ctx, input, output); err != nil {
		t.Fatalf("CompleteVideoProductionWorkflow: %v", err)
	}

	assertVideoWorkflowRunSucceeded(t, ctx, pool, workflowRunID)
	assertVideoWorkflowNodeRuns(t, ctx, pool, workflowRunID)
	assertVideoWorkflowStoryboardShots(t, ctx, pool, workflowRunID, 2)
	assertVideoWorkflowArtifacts(t, ctx, pool, workflowRunID)
	assertVideoWorkflowEvents(t, ctx, pool, orgID, workflowRunID)
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
	promptTraceByTask := map[string]map[string]string{}
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
					"shots": [
						{
							"shotNo": 1,
							"duration": 5,
							"visual": "Wide sunrise platform",
							"camera": "slow push-in",
							"motion": "mist drifting",
							"mood": "hopeful",
							"imagePrompt": "Cinematic sunrise train station, high detail",
							"videoPrompt": "A quiet train platform at sunrise with mist and soft light"
						},
						{
							"shotNo": 2,
							"duration": 4,
							"visual": "Close view of a train door opening",
							"camera": "gentle handheld",
							"motion": "warm light moving across the platform",
							"mood": "anticipatory",
							"imagePrompt": "Close cinematic train doorway, warm sunrise detail",
							"videoPrompt": "The train door opens as sunrise light rolls over the platform"
						}
					]
				}`},
				Usage:     provider.GatewayUsage{EstimatedCost: "0.00000000", Currency: "USD"},
				LatencyMS: 12,
			})
		case "/internal/provider/image/generate":
			var req provider.GatewayImageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode image request: %v", err)
			}
			if req.PromptTemplateKey != promptKeyStoryboardImage || req.PromptVersionID == "" || !strings.HasPrefix(req.PromptHash, "sha256:") || req.PromptSource == "" {
				t.Fatalf("image prompt trace = %+v", req)
			}
			artifactID, mediaFileID, storageKey := insertMockGatewayMediaArtifact(t, r.Context(), pool, req.OrganizationID, req.ProjectID, req.WorkflowRunID, req.NodeRunID, imageModelID, "generated_image", "image/png", req.PromptTemplateKey, req.PromptVersionID, req.PromptHash, req.PromptSource)
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
			if req.PromptTemplateKey != promptKeyStoryboardVideo || req.PromptVersionID == "" || !strings.HasPrefix(req.PromptHash, "sha256:") || req.PromptSource == "" {
				t.Fatalf("video prompt trace = %+v", req)
			}
			externalTaskID := "external-video-task-" + uuid.NewString()
			providerCallID, taskID := insertMockProviderAsyncTask(t, r.Context(), pool, req.OrganizationID, req.ProjectID, req.WorkflowRunID, req.NodeRunID, videoModelID, externalTaskID)
			mu.Lock()
			promptTraceByTask[taskID] = map[string]string{
				"promptTemplateKey": req.PromptTemplateKey,
				"promptVersionId":   req.PromptVersionID,
				"promptHash":        req.PromptHash,
				"promptSource":      req.PromptSource,
			}
			mu.Unlock()
			writeWorkflowGatewayEnvelope(t, w, provider.GatewayVideoCreateTaskResponse{
				ProviderCallID:      providerCallID,
				ProviderAsyncTaskID: taskID,
				ExternalTaskID:      externalTaskID,
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
			trace := promptTraceByTask[req.ProviderAsyncTaskID]
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
				artifactID, mediaFileID, storageKey := insertMockGatewayMediaArtifact(t, r.Context(), pool, req.OrganizationID, req.ProjectID, req.WorkflowRunID, req.NodeRunID, videoModelID, "generated_video", "video/mp4", trace["promptTemplateKey"], trace["promptVersionId"], trace["promptHash"], trace["promptSource"])
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

func insertMockGatewayMediaArtifact(t *testing.T, ctx context.Context, pool *pgxpool.Pool, organizationID, projectID, workflowRunID, nodeRunID, modelID, artifactType, mimeType, promptTemplateKey, promptVersionID, promptHash, promptSource string) (string, string, string) {
	t.Helper()
	storageKey := fmt.Sprintf("org/%s/project/%s/workflow/%s/mock/%s-%d", organizationID, projectID, workflowRunID, artifactType, time.Now().UnixNano())
	var artifactID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, prompt_hash, model_id, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'sha256:mock', $8, $9, $10)
		RETURNING id
	`, organizationID, projectID, workflowRunID, nodeRunID, artifactType, storageKey, mimeType, promptHash, modelID, mustJSON(map[string]any{
		"source":            "mock_provider_gateway",
		"promptTemplateKey": promptTemplateKey,
		"promptVersionId":   promptVersionID,
		"promptHash":        promptHash,
		"promptSource":      promptSource,
	})).Scan(&artifactID); err != nil {
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

func insertMockProviderAsyncTask(t *testing.T, ctx context.Context, pool *pgxpool.Pool, organizationID, projectID, workflowRunID, nodeRunID, modelID, externalTaskID string) (string, string) {
	t.Helper()
	var accountID string
	if err := pool.QueryRow(ctx, `SELECT provider_account_id FROM provider_models WHERE id = $1`, modelID).Scan(&accountID); err != nil {
		t.Fatalf("select provider account for async task: %v", err)
	}
	var callID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_call_logs(
			organization_id, project_id, workflow_run_id, node_run_id,
			provider_account_id, provider_model_id, task_type, execution_mode,
			status, external_task_id, request_hash, request_snapshot, started_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, 'video.create_task', 'async_create', 'running', $7, 'sha256:mock', '{}', now())
		RETURNING id
	`, organizationID, projectID, workflowRunID, nodeRunID, accountID, modelID, externalTaskID).Scan(&callID); err != nil {
		t.Fatalf("insert mock provider call: %v", err)
	}
	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_async_tasks(
			provider_call_id, organization_id, project_id, workflow_run_id, node_run_id,
			provider_account_id, provider_model_id, external_task_id, status,
			task_type, execution_mode, input, raw_status, started_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'running', 'video.generate', 'async_polling', '{}', '{}', now())
		RETURNING id
	`, callID, organizationID, projectID, workflowRunID, nodeRunID, accountID, modelID, externalTaskID).Scan(&taskID); err != nil {
		t.Fatalf("insert mock provider async task: %v", err)
	}
	return callID, taskID
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
	if status != "succeeded" || len(output.Shots) != 2 || output.VideoArtifactID == "" || output.VideoMediaFileID == "" || output.VideoStorageKey == "" || len(output.ProviderCalls.VideoCreates) != 2 || len(output.ProviderCalls.VideoPolls) != 4 {
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
	for _, nodeKey := range []string{nodeGenerateStoryboardTextKey, "generate_shot_image_0", "create_shot_video_0", "generate_shot_image_1", "create_shot_video_1"} {
		if statuses[nodeKey] != "succeeded" {
			t.Fatalf("node statuses = %#v", statuses)
		}
	}
}

func assertVideoWorkflowStoryboardShots(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workflowRunID string, want int) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT status, image_artifact_id IS NOT NULL, video_artifact_id IS NOT NULL
		FROM storyboard_shots
		WHERE workflow_run_id = $1
		ORDER BY shot_index
	`, workflowRunID)
	if err != nil {
		t.Fatalf("select storyboard shots: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		var status string
		var hasImage, hasVideo bool
		if err := rows.Scan(&status, &hasImage, &hasVideo); err != nil {
			t.Fatalf("scan storyboard shot: %v", err)
		}
		if status != "video_succeeded" || !hasImage || !hasVideo {
			t.Fatalf("shot status/image/video = %s/%v/%v", status, hasImage, hasVideo)
		}
	}
	if count != want {
		t.Fatalf("storyboard shot count = %d, want %d", count, want)
	}
}

func assertVideoWorkflowArtifacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workflowRunID string) {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT type, metadata FROM artifacts WHERE workflow_run_id = $1`, workflowRunID)
	if err != nil {
		t.Fatalf("select artifacts: %v", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var artifactType string
		var metadata json.RawMessage
		if err := rows.Scan(&artifactType, &metadata); err != nil {
			t.Fatalf("scan artifact: %v", err)
		}
		seen[artifactType] = true
		switch artifactType {
		case "storyboard_json":
			assertPromptTraceMetadata(t, metadata, promptKeyStoryboardPlanner)
		case "generated_image":
			assertPromptTraceMetadata(t, metadata, promptKeyStoryboardImage)
		case "generated_video":
			assertPromptTraceMetadata(t, metadata, promptKeyStoryboardVideo)
		}
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
	for _, eventType := range []string{"workflow.node.started", "workflow.node.progress", "workflow.node.completed", "artifact.created", "storyboard.shot.video.completed", "workflow.run.completed"} {
		if !seen[eventType] {
			t.Fatalf("missing event %s in %#v", eventType, seen)
		}
	}
}
