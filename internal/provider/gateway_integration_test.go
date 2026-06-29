package provider

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

const gatewayIntegrationAPIKey = "sk-integration-secret"

func TestGatewayTextRuntimeIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run provider gateway integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for provider gateway integration tests")
	}

	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	upstream := httptest.NewServer(openAICompatibleMock(t))
	defer upstream.Close()

	vault, err := NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	orgID, userID, modelID := seedGatewayIntegrationData(t, ctx, pool, vault, upstream.URL)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})

	gatewayService := NewService(pool, vault)
	gatewayService.EnableGatewayRuntime()
	gatewayToken := "integration-service-token"
	gateway := httptest.NewServer(testProviderGatewayHTTP(t, gatewayService, gatewayToken))
	defer gateway.Close()

	apiService := NewService(pool, vault)
	apiService.SetGateway(gateway.URL, gatewayToken)

	textResult, err := apiService.RecordProviderModelTest(ctx, orgID, userID, modelID, TestProviderModelRequest{
		TestType: "text_generation_test",
		Input:    json.RawMessage(`{"prompt":"write a line","maxOutputTokens":32}`),
	})
	if err != nil {
		t.Fatalf("text_generation_test: %v", err)
	}
	assertGatewayProviderTestResult(t, textResult, "gateway text ok")
	assertProviderCallPersisted(t, ctx, pool, textResult.ProviderCallID, "text.generate", modelID)
	assertCostRecordPersisted(t, ctx, pool, textResult.ProviderCallID)
	assertSnapshotsDoNotLeakAPIKey(t, ctx, pool, textResult.ProviderCallID, textResult.TestRunID)

	streamResult, err := apiService.RecordProviderModelTest(ctx, orgID, userID, modelID, TestProviderModelRequest{
		TestType: "streaming_test",
		Input:    json.RawMessage(`{"messages":[{"role":"user","content":"stream"}]}`),
	})
	if err != nil {
		t.Fatalf("streaming_test: %v", err)
	}
	assertGatewayProviderTestResult(t, streamResult, "hello stream")
	assertProviderCallPersisted(t, ctx, pool, streamResult.ProviderCallID, "text.stream", modelID)
	assertCostRecordPersisted(t, ctx, pool, streamResult.ProviderCallID)
	assertSnapshotsDoNotLeakAPIKey(t, ctx, pool, streamResult.ProviderCallID, streamResult.TestRunID)
}

func openAICompatibleMock(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+gatewayIntegrationAPIKey {
			t.Errorf("Authorization header = %q, want bearer test key", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-integration"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions":
			var request struct {
				Stream bool `json:"stream"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode upstream request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if request.Stream {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello \"}}]}\n\n"))
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"stream\"}}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"gateway text ok"}}],"usage":{"prompt_tokens":1000,"completion_tokens":500,"total_tokens":1500}}`))
		default:
			http.NotFound(w, r)
		}
	})
}

func testProviderGatewayHTTP(t *testing.T, service *Service, token string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": StandardError{Code: CodeAuthFailed, Message: "service token is invalid"}})
			return
		}
		switch r.URL.Path {
		case "/internal/provider/text/generate":
			var req GatewayTextRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			resp, err := service.GenerateText(r.Context(), req)
			writeGatewayIntegrationEnvelope(t, w, resp, err)
		case "/internal/provider/text/stream":
			var req GatewayTextRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			resp, err := service.StreamText(r.Context(), req, func(delta GatewayTextDelta) error {
				return writeGatewayIntegrationSSE(w, "provider.delta", delta)
			})
			if err != nil {
				_ = writeGatewayIntegrationSSE(w, "provider.error", StandardError{Code: CodeUnknownError, Message: err.Error()})
				return
			}
			_ = writeGatewayIntegrationSSE(w, "provider.completed", resp)
		case "/internal/provider/image/generate":
			var req GatewayImageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			resp, err := service.GenerateImage(r.Context(), req)
			writeGatewayIntegrationEnvelope(t, w, resp, err)
		case "/internal/provider/video/create-task":
			var req GatewayVideoCreateTaskRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			resp, err := service.CreateVideoTask(r.Context(), req)
			writeGatewayIntegrationEnvelope(t, w, resp, err)
		case "/internal/provider/video/poll-task":
			var req GatewayVideoPollTaskRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			resp, err := service.PollVideoTask(r.Context(), req)
			writeGatewayIntegrationEnvelope(t, w, resp, err)
		case "/internal/provider/video/cancel-task":
			var req GatewayVideoCancelTaskRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			resp, err := service.CancelVideoTask(r.Context(), req)
			writeGatewayIntegrationEnvelope(t, w, resp, err)
		default:
			http.NotFound(w, r)
		}
	})
}

func writeGatewayIntegrationEnvelope(t *testing.T, w http.ResponseWriter, data any, err error) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": StandardError{Code: CodeUnknownError, Message: err.Error()}})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func writeGatewayIntegrationSSE(w http.ResponseWriter, event string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return err
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func seedGatewayIntegrationData(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vault *Vault, upstreamURL string) (string, string, string) {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var orgID, userID, connectorID, accountID, modelID string
	if err := pool.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ($1, $2) RETURNING id`, "Gateway Integration", "gateway-integration-"+suffix).Scan(&orgID); err != nil {
		t.Fatalf("insert organization: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email, display_name) VALUES ($1, $2) RETURNING id`, "gateway-"+suffix+"@example.test", "Gateway Test").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organization_members(organization_id, user_id) VALUES ($1, $2)`, orgID, userID); err != nil {
		t.Fatalf("insert organization member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_connectors(connector_key, name, type, is_official, manifest, version)
		VALUES ($1, 'OpenAI Compatible Integration', 'openai_compatible', true, '{}', 'v1')
		RETURNING id
	`, "openai-compatible-integration-"+suffix).Scan(&connectorID); err != nil {
		t.Fatalf("insert provider connector: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM provider_connectors WHERE id = $1`, connectorID)
	})
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_accounts(organization_id, connector_id, name, base_url, auth_type, status, config, created_by)
		VALUES ($1, $2, 'Integration Account', $3, 'bearer', 'active', '{}', $4)
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
		VALUES ($1, 'gpt-integration', 'GPT Integration', 'text', 'active')
		RETURNING id
	`, accountID).Scan(&modelID); err != nil {
		t.Fatalf("insert provider model: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_model_capabilities(
			provider_model_id, task_types, input_limits, output_limits, quality_tiers, provider_options_schema, pricing_policy
		)
		VALUES ($1, '["text.generate","text.stream"]', '{}', '{}', '[]', '{}', '{"currency":"USD","inputTokenPer1K":"0.0100","outputTokenPer1K":"0.0200"}')
	`, modelID); err != nil {
		t.Fatalf("insert provider model capability: %v", err)
	}
	return orgID, userID, modelID
}

func assertGatewayProviderTestResult(t *testing.T, result ProviderTestResult, wantText string) {
	t.Helper()
	if result.Status != "succeeded" {
		t.Fatalf("status = %q, want succeeded; error=%v", result.Status, result.ErrorMessage)
	}
	if strings.TrimSpace(result.ProviderCallID) == "" {
		t.Fatal("providerCallId is empty")
	}
	var output struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(result.NormalizedOutput, &output); err != nil {
		t.Fatalf("decode normalized output: %v", err)
	}
	if output.Text != wantText {
		t.Fatalf("output text = %q, want %q", output.Text, wantText)
	}
}

func assertProviderCallPersisted(t *testing.T, ctx context.Context, pool *pgxpool.Pool, providerCallID, wantTaskType, wantModelID string) {
	t.Helper()
	var credentialID, modelID, taskType string
	var latency sql.NullInt64
	if err := pool.QueryRow(ctx, `
		SELECT credential_id::text, provider_model_id::text, task_type, latency_ms
		FROM provider_call_logs
		WHERE id = $1
	`, providerCallID).Scan(&credentialID, &modelID, &taskType, &latency); err != nil {
		t.Fatalf("select provider_call_logs: %v", err)
	}
	if strings.TrimSpace(credentialID) == "" {
		t.Fatal("credential_id was not recorded")
	}
	if modelID != wantModelID {
		t.Fatalf("provider_model_id = %q, want %q", modelID, wantModelID)
	}
	if taskType != wantTaskType {
		t.Fatalf("task_type = %q, want %q", taskType, wantTaskType)
	}
	if !latency.Valid {
		t.Fatal("latency_ms was not recorded")
	}
}

func assertCostRecordPersisted(t *testing.T, ctx context.Context, pool *pgxpool.Pool, providerCallID string) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM cost_records WHERE provider_call_id = $1`, providerCallID).Scan(&count); err != nil {
		t.Fatalf("select cost_records: %v", err)
	}
	if count == 0 {
		t.Fatal("cost_records row was not written")
	}
}

func assertSnapshotsDoNotLeakAPIKey(t *testing.T, ctx context.Context, pool *pgxpool.Pool, providerCallID, testRunID string) {
	t.Helper()
	var callRequest, callResponse, callOutput string
	if err := pool.QueryRow(ctx, `
		SELECT request_snapshot::text, COALESCE(response_snapshot::text, ''), COALESCE(normalized_output::text, '')
		FROM provider_call_logs
		WHERE id = $1
	`, providerCallID).Scan(&callRequest, &callResponse, &callOutput); err != nil {
		t.Fatalf("select call snapshots: %v", err)
	}
	var testRequest string
	if err := pool.QueryRow(ctx, `SELECT request_snapshot::text FROM provider_test_runs WHERE id = $1`, testRunID).Scan(&testRequest); err != nil {
		t.Fatalf("select test run snapshot: %v", err)
	}
	for name, snapshot := range map[string]string{
		"provider_call_logs.request_snapshot":  callRequest,
		"provider_call_logs.response_snapshot": callResponse,
		"provider_call_logs.normalized_output": callOutput,
		"provider_test_runs.request_snapshot":  testRequest,
	} {
		if strings.Contains(snapshot, gatewayIntegrationAPIKey) {
			t.Fatalf("%s leaked API key: %s", name, snapshot)
		}
	}
}
