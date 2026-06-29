package provider

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Einzieg/cineweave/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGatewayRoutingIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run provider gateway routing integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for provider gateway routing integration tests")
	}
	t.Setenv("CINEWEAVE_ALLOW_PRIVATE_PROVIDER_MEDIA_URLS", "true")

	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	vault, err := NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}

	t.Run("text fallback", func(t *testing.T) {
		upstream := httptest.NewServer(textRoutingMock(t))
		defer upstream.Close()
		orgID, _, firstModelID := seedGatewayIntegrationData(t, ctx, pool, vault, upstream.URL)
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
		})
		accountID := providerAccountIDForModel(t, ctx, pool, firstModelID)
		secondModelID := insertProviderModelForRouting(t, ctx, pool, accountID, "gpt-routing-fallback", "text", `["text.generate"]`, `{"inputTokenPer1K":"0.0100","outputTokenPer1K":"0.0200"}`)
		profileKey := insertRoutingProfile(t, ctx, pool, orgID, "text_routing_default", []string{firstModelID, secondModelID})

		service := NewService(pool, vault)
		service.EnableGatewayRuntime()
		resp, err := service.GenerateText(ctx, GatewayTextRequest{
			OrganizationID:  orgID,
			ModelProfileKey: profileKey,
			Input:           mustJSON(map[string]any{"prompt": "route text"}),
		})
		if err != nil {
			t.Fatalf("GenerateText: %v", err)
		}
		if resp.Status != "succeeded" || resp.ModelID != secondModelID || resp.Output.Text != "fallback text ok" {
			t.Fatalf("text response = %+v", resp)
		}
		assertRoutingAttempts(t, resp.Attempts, []string{"failed", "succeeded"}, []string{firstModelID, secondModelID})
		assertProviderCallStatus(t, ctx, pool, resp.Attempts[0].ProviderCallID, "failed", CodeUpstreamInternalError)
		assertProviderCallStatus(t, ctx, pool, resp.Attempts[1].ProviderCallID, "succeeded", "")
	})

	t.Run("image guard fallback", func(t *testing.T) {
		upstream := httptest.NewServer(imageRoutingMock(t))
		defer upstream.Close()
		orgID, _, projectID, firstModelID := seedGatewayImageIntegrationData(t, ctx, pool, vault, upstream.URL)
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
		})
		accountID := providerAccountIDForModel(t, ctx, pool, firstModelID)
		secondModelID := insertProviderModelForRouting(t, ctx, pool, accountID, "gpt-image-routing-fallback", "image", `["image.generate"]`, `{"imageCost":"0.0050"}`)
		profileKey := insertRoutingProfile(t, ctx, pool, orgID, "image_routing_default", []string{firstModelID, secondModelID})
		insertLimitPolicy(t, ctx, pool, orgID, accountID, firstModelID, TaskTypeImageGenerate, map[string]any{"max_concurrency": 0})

		service := NewService(pool, vault)
		service.EnableGatewayRuntime()
		service.SetStorage(newMemoryObjectStorage())
		resp, err := service.GenerateImage(ctx, GatewayImageRequest{
			OrganizationID:  orgID,
			ProjectID:       projectID,
			ModelProfileKey: profileKey,
			Input:           mustJSON(map[string]any{"prompt": "route image", "size": "1024x1024"}),
		})
		if err != nil {
			t.Fatalf("GenerateImage: %v", err)
		}
		if resp.Status != "succeeded" || resp.ModelID != secondModelID || resp.Output.ArtifactID == "" {
			t.Fatalf("image response = %+v", resp)
		}
		assertRoutingAttempts(t, resp.Attempts, []string{"blocked", "succeeded"}, []string{firstModelID, secondModelID})
		assertProviderCallStatus(t, ctx, pool, resp.Attempts[0].ProviderCallID, "blocked", CodeProviderConcurrencyLimited)
		assertNoCostRecord(t, ctx, pool, resp.Attempts[0].ProviderCallID)
	})

	t.Run("video create fallback", func(t *testing.T) {
		upstream := httptest.NewServer(videoCreateRoutingMock(t))
		defer upstream.Close()
		orgID, _, projectID, firstModelID := seedGatewayVideoIntegrationData(t, ctx, pool, vault, upstream.URL)
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
		})
		accountID := providerAccountIDForModel(t, ctx, pool, firstModelID)
		secondModelID := insertProviderModelForRouting(t, ctx, pool, accountID, "video-routing-fallback", "video", `["video.generate"]`, `{"videoCostPerSecond":"0.0500"}`)
		profileKey := insertRoutingProfile(t, ctx, pool, orgID, "video_routing_default", []string{firstModelID, secondModelID})

		service := NewService(pool, vault)
		service.EnableGatewayRuntime()
		resp, err := service.CreateVideoTask(ctx, GatewayVideoCreateTaskRequest{
			OrganizationID:  orgID,
			ProjectID:       projectID,
			ModelProfileKey: profileKey,
			Input: mustJSON(map[string]any{
				"prompt":      "route video",
				"duration":    5,
				"aspectRatio": "16:9",
				"resolution":  "720p",
			}),
		})
		if err != nil {
			t.Fatalf("CreateVideoTask: %v", err)
		}
		if resp.Status != "running" || resp.ModelID != secondModelID || resp.ProviderAsyncTaskID == "" {
			t.Fatalf("video response = %+v", resp)
		}
		assertRoutingAttempts(t, resp.Attempts, []string{"failed", "running"}, []string{firstModelID, secondModelID})
		assertAsyncTaskModel(t, ctx, pool, resp.ProviderAsyncTaskID, secondModelID)
		assertProviderCallStatus(t, ctx, pool, resp.Attempts[0].ProviderCallID, "failed", CodeUpstreamInternalError)
	})
}

func textRoutingMock(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode text request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if request["model"] == "gpt-integration" {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"code":"server_error"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"fallback text ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`))
	})
}

func imageRoutingMock(t *testing.T) http.Handler {
	t.Helper()
	pngBody := testPNGBytes(t)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || (r.URL.Path != "/images/generations" && r.URL.Path != "/v1/images/generations") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&map[string]any{})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{
				"b64_json":  base64.StdEncoding.EncodeToString(pngBody),
				"mime_type": "image/png",
			}},
		})
	})
}

func videoCreateRoutingMock(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/video/create" {
			http.NotFound(w, r)
			return
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode video request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if request["model"] == "video-integration-model" {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"code":"server_error"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"taskId": "routing-task", "status": "processing"})
	})
}

func insertRoutingProfile(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, key string, modelIDs []string) string {
	t.Helper()
	var profileID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO model_profiles(organization_id, profile_key, name, purpose, routing_strategy, fallback_strategy)
		VALUES ($1, $2, $3, $4, 'priority_with_fallback', '{"enabled":true,"maxAttempts":3}')
		RETURNING id
	`, orgID, key, key, key).Scan(&profileID); err != nil {
		t.Fatalf("insert routing profile: %v", err)
	}
	for i, modelID := range modelIDs {
		if _, err := pool.Exec(ctx, `
			INSERT INTO model_profile_bindings(model_profile_id, provider_model_id, priority, weight, enabled)
			VALUES ($1, $2, $3, 100, true)
		`, profileID, modelID, i+1); err != nil {
			t.Fatalf("insert routing binding: %v", err)
		}
	}
	return key
}

func insertProviderModelForRouting(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accountID, modelKey, modality, taskTypes, pricing string) string {
	t.Helper()
	var modelID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status)
		VALUES ($1, $2, $3, $4, 'active')
		RETURNING id
	`, accountID, modelKey, "Routing "+modelKey, modality).Scan(&modelID); err != nil {
		t.Fatalf("insert routing model: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_model_capabilities(
			provider_model_id, task_types, input_limits, output_limits, quality_tiers, provider_options_schema, pricing_policy
		)
		VALUES ($1, $2, '{}', '{}', '[]', '{}', $3)
	`, modelID, taskTypes, pricing); err != nil {
		t.Fatalf("insert routing capability: %v", err)
	}
	return modelID
}

func assertRoutingAttempts(t *testing.T, attempts []GatewayAttempt, statuses, modelIDs []string) {
	t.Helper()
	if len(attempts) != len(statuses) {
		t.Fatalf("attempt count = %d, want %d: %+v", len(attempts), len(statuses), attempts)
	}
	for i := range statuses {
		if attempts[i].Status != statuses[i] || attempts[i].ProviderModelID != modelIDs[i] {
			t.Fatalf("attempt %d = status=%s model=%s, want %s/%s", i, attempts[i].Status, attempts[i].ProviderModelID, statuses[i], modelIDs[i])
		}
	}
}

func assertProviderCallStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, providerCallID, wantStatus, wantCode string) {
	t.Helper()
	var status string
	var errorCode sql.NullString
	if err := pool.QueryRow(ctx, `
		SELECT status, error_code
		FROM provider_call_logs
		WHERE id = $1
	`, providerCallID).Scan(&status, &errorCode); err != nil {
		t.Fatalf("select provider call status: %v", err)
	}
	if status != wantStatus || errorCode.String != wantCode {
		t.Fatalf("provider call = status=%s code=%s, want %s/%s", status, errorCode.String, wantStatus, wantCode)
	}
}

func assertAsyncTaskModel(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID, modelID string) {
	t.Helper()
	var got string
	if err := pool.QueryRow(ctx, `SELECT provider_model_id::text FROM provider_async_tasks WHERE id = $1`, taskID).Scan(&got); err != nil {
		t.Fatalf("select async task model: %v", err)
	}
	if got != modelID {
		t.Fatalf("async task model = %s, want %s", got, modelID)
	}
}
