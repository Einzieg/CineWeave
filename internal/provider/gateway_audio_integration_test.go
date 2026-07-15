package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGatewayAudioRuntimeIntegration(t *testing.T) {
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
	t.Cleanup(pool.Close)

	upstream := httptest.NewServer(openAICompatibleAudioMock(t))
	defer upstream.Close()
	vault, err := NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	objectStorage := newMemoryObjectStorage()
	orgID, userID, projectID, modelID, sourceMediaID := seedGatewayAudioIntegrationData(t, ctx, pool, vault, objectStorage, upstream.URL)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID) })

	gatewayService := NewService(pool, vault)
	gatewayService.EnableGatewayRuntime()
	gatewayService.SetStorage(objectStorage)
	gatewayToken := "audio-integration-service-token"
	gateway := httptest.NewServer(testProviderGatewayHTTP(t, gatewayService, gatewayToken))
	defer gateway.Close()
	apiService := NewService(pool, vault)
	apiService.SetGateway(gateway.URL, gatewayToken)

	ttsResult, err := apiService.RecordProviderModelTest(ctx, orgID, userID, modelID, TestProviderModelRequest{
		TestType: "audio_tts_test",
		Input: mustJSON(map[string]any{
			"projectId": projectID, "input": "必须保留的中文台词", "voice": "alloy", "response_format": "wav",
		}),
	})
	if err != nil {
		t.Fatalf("audio_tts_test: %v", err)
	}
	if ttsResult.Status != "succeeded" || ttsResult.ProviderCallID == "" {
		t.Fatalf("tts result = %+v", ttsResult)
	}
	var ttsOutput GatewayAudioOutput
	if err := json.Unmarshal(ttsResult.NormalizedOutput, &ttsOutput); err != nil {
		t.Fatalf("decode tts output: %v", err)
	}
	if ttsOutput.ArtifactID == "" || ttsOutput.MediaFileID == "" || ttsOutput.StorageKey == "" || ttsOutput.MimeType != "audio/wav" {
		t.Fatalf("tts output = %+v", ttsOutput)
	}
	if _, ok := objectStorage.get(ttsOutput.StorageKey); !ok {
		t.Fatalf("tts object %q was not stored", ttsOutput.StorageKey)
	}

	asrResult, err := apiService.RecordProviderModelTest(ctx, orgID, userID, modelID, TestProviderModelRequest{
		TestType: "audio_transcription_test",
		Input: mustJSON(map[string]any{
			"projectId": projectID, "mediaFileId": sourceMediaID, "language": "zh",
			"response_format": "verbose_json", "timestamp_granularities": []string{"segment", "word"},
		}),
	})
	if err != nil {
		t.Fatalf("audio_transcription_test: %v", err)
	}
	if asrResult.Status != "succeeded" || asrResult.ProviderCallID == "" {
		t.Fatalf("asr result = %+v", asrResult)
	}
	var asrOutput GatewayASROutput
	if err := json.Unmarshal(asrResult.NormalizedOutput, &asrOutput); err != nil {
		t.Fatalf("decode asr output: %v", err)
	}
	if asrOutput.Text != "必须保留的中文台词" || len(asrOutput.Segments) != 1 {
		t.Fatalf("asr output = %+v", asrOutput)
	}

	for _, call := range []struct{ id, task string }{{ttsResult.ProviderCallID, TaskTypeAudioTTS}, {asrResult.ProviderCallID, TaskTypeAudioTranscribe}} {
		assertProviderCallPersisted(t, ctx, pool, call.id, call.task, modelID)
		assertCostRecordPersisted(t, ctx, pool, call.id)
	}
	assertSnapshotsDoNotLeakAPIKey(t, ctx, pool, ttsResult.ProviderCallID, ttsResult.TestRunID)
}

func openAICompatibleAudioMock(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+gatewayIntegrationAPIKey {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/v1/audio/speech":
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode speech request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if request["model"] != "audio-integration" || request["voice"] != "alloy" || request["input"] != "必须保留的中文台词" {
				t.Errorf("speech request = %#v", request)
			}
			w.Header().Set("Content-Type", "audio/wav")
			_, _ = w.Write([]byte("RIFF\x04\x00\x00\x00WAVE"))
		case "/v1/audio/transcriptions":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("parse transcription multipart: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Errorf("transcription file: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			body, _ := io.ReadAll(file)
			_ = file.Close()
			if string(body) != "registered-audio-source" || r.FormValue("model") != "audio-integration" || r.FormValue("language") != "zh" {
				t.Errorf("transcription input/model/language = %q/%q/%q", body, r.FormValue("model"), r.FormValue("language"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"text":"必须保留的中文台词","language":"zh","duration":2.5,"segments":[{"id":0,"text":"必须保留的中文台词","start":0,"end":2.5}]}`))
		default:
			http.NotFound(w, r)
		}
	})
}

func seedGatewayAudioIntegrationData(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vault *Vault, objectStorage *memoryObjectStorage, upstreamURL string) (string, string, string, string, string) {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var orgID, userID, workspaceID, projectID, connectorID, accountID, modelID, artifactID, mediaID string
	if err := pool.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ($1, $2) RETURNING id`, "Gateway Audio Integration", "gateway-audio-integration-"+suffix).Scan(&orgID); err != nil {
		t.Fatalf("insert organization: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email, display_name) VALUES ($1, 'Gateway Audio Test') RETURNING id`, "gateway-audio-"+suffix+"@example.test").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})
	_, _ = pool.Exec(ctx, `INSERT INTO organization_members(organization_id, user_id) VALUES ($1, $2)`, orgID, userID)
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(organization_id, name) VALUES ($1, 'Audio Workspace') RETURNING id`, orgID).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO projects(organization_id, workspace_id, name, created_by) VALUES ($1, $2, 'Audio Project', $3) RETURNING id`, orgID, workspaceID, userID).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	_, _ = pool.Exec(ctx, `INSERT INTO project_members(project_id, user_id) VALUES ($1, $2)`, projectID, userID)
	if err := pool.QueryRow(ctx, `INSERT INTO provider_connectors(connector_key, name, type, is_official, manifest, version) VALUES ($1, 'Audio Integration', 'openai_compatible', true, '{}', 'v1') RETURNING id`, "openai-compatible-audio-integration-"+suffix).Scan(&connectorID); err != nil {
		t.Fatalf("insert connector: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM provider_connectors WHERE id = $1`, connectorID)
	})
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_accounts(organization_id, connector_id, name, base_url, auth_type, status, config, created_by)
		VALUES ($1, $2, 'Audio Integration Account', $3, 'bearer', 'active',
		        '{"audioSpeechEndpoint":"/audio/speech","audioTranscriptionsEndpoint":"/audio/transcriptions","timeoutMs":3000}', $4)
		RETURNING id
	`, orgID, connectorID, upstreamURL, userID).Scan(&accountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	encrypted, err := vault.EncryptJSON(map[string]any{"apiKey": gatewayIntegrationAPIKey})
	if err != nil {
		t.Fatalf("encrypt credential: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_credentials(organization_id, provider_account_id, credential_key, credential_type, secret_ref, encrypted_payload, masked_preview, status, is_active, created_by)
		VALUES ($1, $2, 'default', 'api_key', 'local:aes-gcm:v1', $3, $4, 'active', true, $5)
	`, orgID, accountID, encrypted, MaskSecret(gatewayIntegrationAPIKey), userID); err != nil {
		t.Fatalf("insert credential: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status) VALUES ($1, 'audio-integration', 'Audio Integration', 'audio', 'active') RETURNING id`, accountID).Scan(&modelID); err != nil {
		t.Fatalf("insert model: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_model_capabilities(provider_model_id, task_types, input_limits, output_limits, quality_tiers, provider_options_schema, pricing_policy)
		VALUES ($1, '["audio.tts","audio.transcribe"]', '{}', '{}', '[]', '{"xCapabilities":{"supportsTTS":true,"supportsTranscription":true}}', '{"currency":"USD","characterPer1K":"0.01","audioMinute":"0.02"}')
	`, modelID); err != nil {
		t.Fatalf("insert capability: %v", err)
	}
	put, err := objectStorage.PutBytes(ctx, "org/"+orgID+"/project/"+projectID+"/registered/source.wav", []byte("registered-audio-source"), "audio/wav")
	if err != nil {
		t.Fatalf("seed audio object: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO artifacts(organization_id, project_id, type, storage_key, mime_type, content_hash, metadata) VALUES ($1, $2, 'source_audio', $3, 'audio/wav', $4, '{}') RETURNING id`, orgID, projectID, put.StorageKey, put.ContentHash).Scan(&artifactID); err != nil {
		t.Fatalf("insert source artifact: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO media_files(organization_id, project_id, artifact_id, storage_key, mime_type, byte_size, checksum, duration_seconds, metadata) VALUES ($1, $2, $3, $4, 'audio/wav', $5, $6, 2.5, '{}') RETURNING id`, orgID, projectID, artifactID, put.StorageKey, put.ByteSize, put.ContentHash).Scan(&mediaID); err != nil {
		t.Fatalf("insert source media: %v", err)
	}
	return orgID, userID, projectID, modelID, mediaID
}
