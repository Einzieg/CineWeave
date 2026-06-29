package provider

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
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGatewayVideoRuntimeIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run provider gateway video integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for provider gateway video integration tests")
	}
	t.Setenv("CINEWEAVE_ALLOW_PRIVATE_PROVIDER_MEDIA_URLS", "true")

	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	upstream := newVideoRuntimeMock(t)
	defer upstream.Close()

	vault, err := NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	orgID, userID, projectID, modelID := seedGatewayVideoIntegrationData(t, ctx, pool, vault, upstream.URL)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})

	objectStorage := newMemoryObjectStorage()
	gatewayService := NewService(pool, vault)
	gatewayService.EnableGatewayRuntime()
	gatewayService.SetStorage(objectStorage)

	createResp, err := gatewayService.CreateVideoTask(ctx, GatewayVideoCreateTaskRequest{
		OrganizationID:  orgID,
		ProjectID:       projectID,
		ProviderModelID: modelID,
		Input: mustJSON(map[string]any{
			"prompt":      "A cinematic sunrise train station with slow camera movement",
			"duration":    5,
			"aspectRatio": "16:9",
			"resolution":  "720p",
		}),
	})
	if err != nil {
		t.Fatalf("CreateVideoTask: %v", err)
	}
	if createResp.Status != "running" || createResp.ProviderAsyncTaskID == "" || createResp.ExternalTaskID == "" {
		t.Fatalf("create response = %+v", createResp)
	}
	assertGatewayVideoAsyncTask(t, ctx, pool, createResp.ProviderAsyncTaskID, "running", 0)
	assertGatewayVideoCallLog(t, ctx, pool, createResp.ProviderCallID, "video.create_task", "", "")

	firstPoll, err := gatewayService.PollVideoTask(ctx, GatewayVideoPollTaskRequest{
		OrganizationID:      orgID,
		ProviderAsyncTaskID: createResp.ProviderAsyncTaskID,
		ProjectID:           projectID,
	})
	if err != nil {
		t.Fatalf("first PollVideoTask: %v", err)
	}
	if firstPoll.Status != "running" || firstPoll.Output.ArtifactID != "" {
		t.Fatalf("first poll = %+v", firstPoll)
	}
	assertGatewayVideoArtifactCount(t, ctx, pool, createResp.ProviderAsyncTaskID, 0)

	secondPoll, err := gatewayService.PollVideoTask(ctx, GatewayVideoPollTaskRequest{
		OrganizationID:      orgID,
		ProviderAsyncTaskID: createResp.ProviderAsyncTaskID,
		ProjectID:           projectID,
	})
	if err != nil {
		t.Fatalf("second PollVideoTask: %v", err)
	}
	if secondPoll.Status != "succeeded" || secondPoll.Output.ArtifactID == "" || secondPoll.Output.MediaFileID == "" || secondPoll.Output.StorageKey == "" {
		t.Fatalf("second poll = %+v", secondPoll)
	}
	assertGatewayVideoObjectStored(t, objectStorage, secondPoll.Output.StorageKey)
	assertGatewayVideoRowsPersisted(t, ctx, pool, secondPoll.ProviderCallID, createResp.ProviderAsyncTaskID, createResp.ExternalTaskID, secondPoll.Output, projectID, modelID)
	assertGatewayVideoAsyncTask(t, ctx, pool, createResp.ProviderAsyncTaskID, "succeeded", 2)
	assertGatewayVideoCostRecord(t, ctx, pool, secondPoll.ProviderCallID)
	assertGatewayVideoCallLog(t, ctx, pool, secondPoll.ProviderCallID, "video.poll_task", secondPoll.Output.ArtifactID, secondPoll.Output.MediaFileID)

	gatewayToken := "video-integration-token"
	gateway := httptest.NewServer(testProviderGatewayHTTP(t, gatewayService, gatewayToken))
	defer gateway.Close()
	apiService := NewService(pool, vault)
	apiService.SetGateway(gateway.URL, gatewayToken)
	result, err := apiService.RecordProviderModelTest(ctx, orgID, userID, modelID, TestProviderModelRequest{
		TestType: "video_generation_test",
		Input: mustJSON(map[string]any{
			"prompt":      "A cinematic sunrise train station with slow camera movement",
			"duration":    5,
			"aspectRatio": "16:9",
			"resolution":  "720p",
			"projectId":   projectID,
		}),
	})
	if err != nil {
		t.Fatalf("video_generation_test: %v", err)
	}
	assertGatewayVideoProviderTestResult(t, result)
	assertSnapshotsDoNotLeakAPIKey(t, ctx, pool, result.ProviderCallID, result.TestRunID)
}

func newVideoRuntimeMock(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	polls := map[string]int{}
	nextTask := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/files/video.mp4" {
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("fake mp4 bytes"))
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+gatewayIntegrationAPIKey {
			t.Errorf("Authorization header = %q, want bearer test key", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/video/create":
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode create request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if request["model"] != "video-integration-model" || request["prompt"] == "" || request["duration"] != float64(5) {
				t.Errorf("create request = %#v", request)
			}
			mu.Lock()
			nextTask++
			taskID := fmt.Sprintf("task-%d", nextTask)
			polls[taskID] = 0
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"taskId": taskID, "status": "processing"})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/video/poll/"):
			taskID := strings.TrimPrefix(r.URL.Path, "/video/poll/")
			mu.Lock()
			polls[taskID]++
			count := polls[taskID]
			mu.Unlock()
			if count == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{"status": "processing"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":          "completed",
				"videoUrl":        server.URL + "/files/video.mp4",
				"mimeType":        "video/mp4",
				"durationSeconds": 5,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	return server
}

func seedGatewayVideoIntegrationData(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vault *Vault, upstreamURL string) (string, string, string, string) {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var orgID, userID, workspaceID, projectID, connectorID, accountID, modelID string
	if err := pool.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ($1, $2) RETURNING id`, "Gateway Video Integration", "gateway-video-integration-"+suffix).Scan(&orgID); err != nil {
		t.Fatalf("insert organization: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email, display_name) VALUES ($1, $2) RETURNING id`, "gateway-video-"+suffix+"@example.test", "Gateway Video Test").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organization_members(organization_id, user_id) VALUES ($1, $2)`, orgID, userID); err != nil {
		t.Fatalf("insert organization member: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(organization_id, name) VALUES ($1, 'Video Workspace') RETURNING id`, orgID).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects(organization_id, workspace_id, name, created_by)
		VALUES ($1, $2, 'Video Project', $3)
		RETURNING id
	`, orgID, workspaceID, userID).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO project_members(project_id, user_id) VALUES ($1, $2)`, projectID, userID); err != nil {
		t.Fatalf("insert project member: %v", err)
	}
	manifest := videoIntegrationManifest(upstreamURL)
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_connectors(connector_key, name, type, is_official, manifest, version)
		VALUES ($1, 'Video Manifest Integration', 'http', true, $2, 'v1')
		RETURNING id
	`, "video-manifest-integration-"+suffix, manifest).Scan(&connectorID); err != nil {
		t.Fatalf("insert provider connector: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM provider_connectors WHERE id = $1`, connectorID)
	})
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_accounts(organization_id, connector_id, name, base_url, auth_type, status, config, created_by)
		VALUES ($1, $2, 'Video Integration Account', $3, 'bearer', 'active', '{}', $4)
		RETURNING id
	`, orgID, connectorID, upstreamURL, userID).Scan(&accountID); err != nil {
		t.Fatalf("insert provider account: %v", err)
	}
	encrypted, err := vault.EncryptJSON(map[string]any{"apiKey": gatewayIntegrationAPIKey})
	if err != nil {
		t.Fatalf("encrypt credential: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_credentials(
			organization_id, provider_account_id, credential_key, credential_type,
			secret_ref, encrypted_payload, masked_preview, status, is_active, created_by
		)
		VALUES ($1, $2, 'default', 'api_key', 'local:aes-gcm:v1', $3, $4, 'active', true, $5)
	`, orgID, accountID, encrypted, MaskSecret(gatewayIntegrationAPIKey), userID); err != nil {
		t.Fatalf("insert provider credential: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status)
		VALUES ($1, 'video-integration-model', 'Video Integration Model', 'video', 'active')
		RETURNING id
	`, accountID).Scan(&modelID); err != nil {
		t.Fatalf("insert provider model: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_model_capabilities(
			provider_model_id, task_types, input_limits, output_limits, quality_tiers, provider_options_schema, pricing_policy
		)
		VALUES ($1, '["video.generate"]', '{}', '{}', '["standard"]', '{}', '{"currency":"USD","videoCostPerSecond":"0.0500","videoCostByResolution":{"720p":"0.0500"},"videoCostFlat":"0.2000"}')
	`, modelID); err != nil {
		t.Fatalf("insert provider model capability: %v", err)
	}
	return orgID, userID, projectID, modelID
}

func videoIntegrationManifest(baseURL string) json.RawMessage {
	return mustJSON(map[string]any{
		"kind":      "ProviderConnector",
		"version":   "v1",
		"id":        "video-integration",
		"name":      "Video Integration",
		"transport": "http",
		"baseUrl":   baseURL,
		"auth": map[string]any{
			"type":          "bearer",
			"header":        "Authorization",
			"valueTemplate": "Bearer {{ credential.apiKey }}",
		},
		"models": []map[string]any{{
			"id":          "video-integration-model",
			"displayName": "Video Integration Model",
			"modality":    "video",
			"capabilities": map[string]any{
				"taskTypes": []string{"video.generate"},
			},
		}},
		"endpoints": map[string]any{
			"video_generate": map[string]any{
				"endpointType":    "async_create",
				"method":          "POST",
				"pathTemplate":    "/video/create",
				"pollEndpointKey": "video_poll",
				"requestTemplate": map[string]any{
					"model":        "{{ model.id }}",
					"prompt":       "{{ input.prompt }}",
					"duration":     "{{ input.duration }}",
					"aspect_ratio": "{{ input.aspectRatio }}",
					"resolution":   "{{ input.resolution }}",
				},
				"responseMapping": map[string]string{
					"externalTaskId": "$.taskId",
					"status":         "$.status",
				},
			},
			"video_poll": map[string]any{
				"endpointType": "async_poll",
				"method":       "GET",
				"pathTemplate": "/video/poll/{{ task.externalTaskId }}",
				"responseMapping": map[string]string{
					"status":          "$.status",
					"videoUrl":        "$.videoUrl",
					"mimeType":        "$.mimeType",
					"durationSeconds": "$.durationSeconds",
				},
			},
		},
	})
}

func assertGatewayVideoObjectStored(t *testing.T, objectStorage *memoryObjectStorage, storageKey string) {
	t.Helper()
	object, ok := objectStorage.get(storageKey)
	if !ok {
		t.Fatalf("storage key %q was not written", storageKey)
	}
	if object.contentType != "video/mp4" || string(object.body) != "fake mp4 bytes" {
		t.Fatalf("stored object = %+v", object)
	}
}

func assertGatewayVideoAsyncTask(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID, wantStatus string, wantPollCount int) {
	t.Helper()
	var status string
	var pollCount int
	if err := pool.QueryRow(ctx, `SELECT status, poll_count FROM provider_async_tasks WHERE id = $1`, taskID).Scan(&status, &pollCount); err != nil {
		t.Fatalf("select provider_async_tasks: %v", err)
	}
	if status != wantStatus || pollCount != wantPollCount {
		t.Fatalf("async task status/poll_count = %s/%d, want %s/%d", status, pollCount, wantStatus, wantPollCount)
	}
}

func assertGatewayVideoArtifactCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID string, want int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM artifacts WHERE metadata->>'providerAsyncTaskId' = $1`, taskID).Scan(&count); err != nil {
		t.Fatalf("count video artifacts: %v", err)
	}
	if count != want {
		t.Fatalf("video artifact count = %d, want %d", count, want)
	}
}

func assertGatewayVideoRowsPersisted(t *testing.T, ctx context.Context, pool *pgxpool.Pool, providerCallID, providerAsyncTaskID, externalTaskID string, output GatewayVideoOutput, projectID, modelID string) {
	t.Helper()
	var mediaArtifactID, mediaStorageKey, mediaMimeType, mediaChecksum, mediaSource, mediaCallID, mediaTaskID string
	var mediaByteSize int64
	if err := pool.QueryRow(ctx, `
		SELECT artifact_id::text, storage_key, mime_type, byte_size, checksum, metadata->>'source', metadata->>'providerCallId', metadata->>'providerAsyncTaskId'
		FROM media_files
		WHERE id = $1
	`, output.MediaFileID).Scan(&mediaArtifactID, &mediaStorageKey, &mediaMimeType, &mediaByteSize, &mediaChecksum, &mediaSource, &mediaCallID, &mediaTaskID); err != nil {
		t.Fatalf("select media_files: %v", err)
	}
	if mediaArtifactID != output.ArtifactID || mediaStorageKey != output.StorageKey || mediaMimeType != "video/mp4" || mediaByteSize == 0 || mediaSource != "provider_gateway" || mediaCallID != providerCallID || mediaTaskID != providerAsyncTaskID || !strings.HasPrefix(mediaChecksum, "sha256:") {
		t.Fatalf("media row mismatch: artifact=%s key=%s mime=%s bytes=%d source=%s call=%s task=%s checksum=%s", mediaArtifactID, mediaStorageKey, mediaMimeType, mediaByteSize, mediaSource, mediaCallID, mediaTaskID, mediaChecksum)
	}

	var artifactProjectID, artifactType, artifactStorageKey, artifactModelID, artifactMediaID, artifactCallID, artifactTaskID, artifactExternalTaskID string
	if err := pool.QueryRow(ctx, `
		SELECT project_id::text, type, storage_key, model_id::text, metadata->>'mediaFileId', metadata->>'providerCallId', metadata->>'providerAsyncTaskId', metadata->>'externalTaskId'
		FROM artifacts
		WHERE id = $1
	`, output.ArtifactID).Scan(&artifactProjectID, &artifactType, &artifactStorageKey, &artifactModelID, &artifactMediaID, &artifactCallID, &artifactTaskID, &artifactExternalTaskID); err != nil {
		t.Fatalf("select artifacts: %v", err)
	}
	if artifactProjectID != projectID || artifactType != "generated_video" || artifactStorageKey != output.StorageKey || artifactModelID != modelID || artifactMediaID != output.MediaFileID || artifactCallID != providerCallID || artifactTaskID != providerAsyncTaskID || artifactExternalTaskID != externalTaskID {
		t.Fatalf("artifact row mismatch: project=%s type=%s key=%s model=%s media=%s call=%s task=%s external=%s", artifactProjectID, artifactType, artifactStorageKey, artifactModelID, artifactMediaID, artifactCallID, artifactTaskID, artifactExternalTaskID)
	}
}

func assertGatewayVideoCostRecord(t *testing.T, ctx context.Context, pool *pgxpool.Pool, providerCallID string) {
	t.Helper()
	var costType, unit, currency, resolution string
	var amount, quantity float64
	if err := pool.QueryRow(ctx, `
		SELECT cost_type, unit, currency, amount::float8, quantity::float8, metadata->>'resolution'
		FROM cost_records
		WHERE provider_call_id = $1
	`, providerCallID).Scan(&costType, &unit, &currency, &amount, &quantity, &resolution); err != nil {
		t.Fatalf("select cost_records: %v", err)
	}
	if costType != "video.generate" || unit != "second" || currency != "USD" || amount != 0.25 || quantity != 5 || resolution != "720p" {
		t.Fatalf("cost row mismatch: type=%s unit=%s currency=%s amount=%f quantity=%f resolution=%s", costType, unit, currency, amount, quantity, resolution)
	}
}

func assertGatewayVideoCallLog(t *testing.T, ctx context.Context, pool *pgxpool.Pool, providerCallID, wantTaskType, artifactID, mediaFileID string) {
	t.Helper()
	var taskType, artifactIDsRaw, mediaFileIDsRaw, requestSnapshot string
	if err := pool.QueryRow(ctx, `
		SELECT task_type, artifact_ids::text, media_file_ids::text, request_snapshot::text
		FROM provider_call_logs
		WHERE id = $1
	`, providerCallID).Scan(&taskType, &artifactIDsRaw, &mediaFileIDsRaw, &requestSnapshot); err != nil {
		t.Fatalf("select provider_call_logs: %v", err)
	}
	if taskType != wantTaskType {
		t.Fatalf("taskType = %s, want %s", taskType, wantTaskType)
	}
	if strings.Contains(requestSnapshot, gatewayIntegrationAPIKey) {
		t.Fatal("request_snapshot leaked API key")
	}
	if artifactID != "" && (!jsonArrayContains(artifactIDsRaw, artifactID) || !jsonArrayContains(mediaFileIDsRaw, mediaFileID)) {
		t.Fatalf("call log ids mismatch: artifact_ids=%s media_file_ids=%s", artifactIDsRaw, mediaFileIDsRaw)
	}
}

func assertGatewayVideoProviderTestResult(t *testing.T, result ProviderTestResult) {
	t.Helper()
	if result.Status != "succeeded" {
		t.Fatalf("status = %q, want succeeded; error=%v", result.Status, result.ErrorMessage)
	}
	var output map[string]any
	if err := json.Unmarshal(result.NormalizedOutput, &output); err != nil {
		t.Fatalf("decode normalized output: %v", err)
	}
	if output["providerAsyncTaskId"] == "" || output["artifactId"] == "" || output["mediaFileId"] == "" || output["storageKey"] == "" {
		t.Fatalf("provider test normalized output = %#v", output)
	}
}
