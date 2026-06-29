package provider

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGatewayImageRuntimeIntegration(t *testing.T) {
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

	upstream := httptest.NewServer(openAICompatibleImageMock(t))
	defer upstream.Close()

	vault, err := NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	orgID, userID, projectID, modelID := seedGatewayImageIntegrationData(t, ctx, pool, vault, upstream.URL)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})

	objectStorage := newMemoryObjectStorage()
	gatewayService := NewService(pool, vault)
	gatewayService.EnableGatewayRuntime()
	gatewayService.SetStorage(objectStorage)
	gatewayToken := "integration-service-token"
	gateway := httptest.NewServer(testProviderGatewayHTTP(t, gatewayService, gatewayToken))
	defer gateway.Close()

	apiService := NewService(pool, vault)
	apiService.SetGateway(gateway.URL, gatewayToken)

	input := mustJSON(map[string]any{
		"prompt":    "A cinematic sunrise train station, high detail",
		"size":      "1024x1024",
		"quality":   "hd",
		"projectId": projectID,
	})
	result, err := apiService.RecordProviderModelTest(ctx, orgID, userID, modelID, TestProviderModelRequest{
		TestType: "image_generation_test",
		Input:    input,
	})
	if err != nil {
		t.Fatalf("image_generation_test: %v", err)
	}
	output := assertGatewayImageProviderTestResult(t, result)
	assertGatewayImageObjectStored(t, objectStorage, output.StorageKey)
	assertGatewayImageRowsPersisted(t, ctx, pool, result.ProviderCallID, output, projectID, modelID)
	assertImageCostRecordPersisted(t, ctx, pool, result.ProviderCallID)
	assertSnapshotsDoNotLeakAPIKey(t, ctx, pool, result.ProviderCallID, result.TestRunID)
}

func openAICompatibleImageMock(t *testing.T) http.Handler {
	t.Helper()
	pngBody := testPNGBytes(t)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+gatewayIntegrationAPIKey {
			t.Errorf("Authorization header = %q, want bearer test key", r.Header.Get("Authorization"))
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/images/generations" {
			http.NotFound(w, r)
			return
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode upstream request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if request["model"] != "gpt-image-integration" || request["size"] != "1024x1024" || request["quality"] != "hd" {
			t.Errorf("unexpected upstream image request: %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{
				"b64_json":       base64.StdEncoding.EncodeToString(pngBody),
				"mime_type":      "image/png",
				"revised_prompt": "A cinematic sunrise train station, high detail",
			}},
		})
	})
}

func seedGatewayImageIntegrationData(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vault *Vault, upstreamURL string) (string, string, string, string) {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var orgID, userID, workspaceID, projectID, connectorID, accountID, modelID string
	if err := pool.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ($1, $2) RETURNING id`, "Gateway Image Integration", "gateway-image-integration-"+suffix).Scan(&orgID); err != nil {
		t.Fatalf("insert organization: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email, display_name) VALUES ($1, $2) RETURNING id`, "gateway-image-"+suffix+"@example.test", "Gateway Image Test").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organization_members(organization_id, user_id) VALUES ($1, $2)`, orgID, userID); err != nil {
		t.Fatalf("insert organization member: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(organization_id, name) VALUES ($1, 'Image Workspace') RETURNING id`, orgID).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects(organization_id, workspace_id, name, created_by)
		VALUES ($1, $2, 'Image Project', $3)
		RETURNING id
	`, orgID, workspaceID, userID).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO project_members(project_id, user_id) VALUES ($1, $2)`, projectID, userID); err != nil {
		t.Fatalf("insert project member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_connectors(connector_key, name, type, is_official, manifest, version)
		VALUES ($1, 'OpenAI Compatible Image Integration', 'openai_compatible', true, '{}', 'v1')
		RETURNING id
	`, "openai-compatible-image-integration-"+suffix).Scan(&connectorID); err != nil {
		t.Fatalf("insert provider connector: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM provider_connectors WHERE id = $1`, connectorID)
	})
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_accounts(organization_id, connector_id, name, base_url, auth_type, status, config, created_by)
		VALUES ($1, $2, 'Image Integration Account', $3, 'bearer', 'active', '{"imagesGenerationsEndpoint":"/images/generations","timeoutMs":3000}', $4)
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
		VALUES ($1, 'gpt-image-integration', 'GPT Image Integration', 'image', 'active')
		RETURNING id
	`, accountID).Scan(&modelID); err != nil {
		t.Fatalf("insert provider model: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_model_capabilities(
			provider_model_id, task_types, input_limits, output_limits, quality_tiers, provider_options_schema, pricing_policy
		)
		VALUES ($1, '["image.generate"]', '{}', '{}', '["standard","hd"]', '{}', '{"currency":"USD","imageCost":"0.0050","imageCostBySize":{"1024x1024":"0.0100"},"imageCostByQuality":{"hd":"0.0200"}}')
	`, modelID); err != nil {
		t.Fatalf("insert provider model capability: %v", err)
	}
	return orgID, userID, projectID, modelID
}

func assertGatewayImageProviderTestResult(t *testing.T, result ProviderTestResult) GatewayImageOutput {
	t.Helper()
	if result.Status != "succeeded" {
		t.Fatalf("status = %q, want succeeded; error=%v", result.Status, result.ErrorMessage)
	}
	if strings.TrimSpace(result.ProviderCallID) == "" {
		t.Fatal("providerCallId is empty")
	}
	var output GatewayImageOutput
	if err := json.Unmarshal(result.NormalizedOutput, &output); err != nil {
		t.Fatalf("decode normalized output: %v", err)
	}
	if output.ArtifactID == "" || output.MediaFileID == "" || output.StorageKey == "" {
		t.Fatalf("output missing persisted ids: %+v", output)
	}
	if output.MimeType != "image/png" {
		t.Fatalf("mimeType = %q, want image/png", output.MimeType)
	}
	return output
}

func assertGatewayImageObjectStored(t *testing.T, objectStorage *memoryObjectStorage, storageKey string) {
	t.Helper()
	object, ok := objectStorage.get(storageKey)
	if !ok {
		t.Fatalf("storage key %q was not written", storageKey)
	}
	if object.contentType != "image/png" || len(object.body) == 0 {
		t.Fatalf("stored object = %+v", object)
	}
}

func assertGatewayImageRowsPersisted(t *testing.T, ctx context.Context, pool *pgxpool.Pool, providerCallID string, output GatewayImageOutput, projectID, modelID string) {
	t.Helper()
	var mediaArtifactID, mediaStorageKey, mediaMimeType, mediaChecksum, mediaSource, mediaCallID string
	var mediaByteSize int64
	var mediaWidth, mediaHeight int
	if err := pool.QueryRow(ctx, `
		SELECT artifact_id::text, storage_key, mime_type, byte_size, checksum, width, height, metadata->>'source', metadata->>'providerCallId'
		FROM media_files
		WHERE id = $1
	`, output.MediaFileID).Scan(&mediaArtifactID, &mediaStorageKey, &mediaMimeType, &mediaByteSize, &mediaChecksum, &mediaWidth, &mediaHeight, &mediaSource, &mediaCallID); err != nil {
		t.Fatalf("select media_files: %v", err)
	}
	if mediaArtifactID != output.ArtifactID || mediaStorageKey != output.StorageKey || mediaMimeType != "image/png" || mediaByteSize == 0 {
		t.Fatalf("media row mismatch: artifact=%s key=%s mime=%s bytes=%d", mediaArtifactID, mediaStorageKey, mediaMimeType, mediaByteSize)
	}
	if mediaWidth != 1 || mediaHeight != 1 || mediaSource != "provider_gateway" || mediaCallID != providerCallID || !strings.HasPrefix(mediaChecksum, "sha256:") {
		t.Fatalf("media row metadata mismatch: width=%d height=%d source=%s call=%s checksum=%s", mediaWidth, mediaHeight, mediaSource, mediaCallID, mediaChecksum)
	}

	var artifactProjectID, artifactType, artifactStorageKey, artifactModelID, artifactMediaID, artifactCallID string
	if err := pool.QueryRow(ctx, `
		SELECT project_id::text, type, storage_key, model_id::text, metadata->>'mediaFileId', metadata->>'providerCallId'
		FROM artifacts
		WHERE id = $1
	`, output.ArtifactID).Scan(&artifactProjectID, &artifactType, &artifactStorageKey, &artifactModelID, &artifactMediaID, &artifactCallID); err != nil {
		t.Fatalf("select artifacts: %v", err)
	}
	if artifactProjectID != projectID || artifactType != "generated_image" || artifactStorageKey != output.StorageKey || artifactModelID != modelID || artifactMediaID != output.MediaFileID || artifactCallID != providerCallID {
		t.Fatalf("artifact row mismatch: project=%s type=%s key=%s model=%s media=%s call=%s", artifactProjectID, artifactType, artifactStorageKey, artifactModelID, artifactMediaID, artifactCallID)
	}

	var taskType, callModelID, artifactIDsRaw, mediaFileIDsRaw string
	if err := pool.QueryRow(ctx, `
		SELECT task_type, provider_model_id::text, artifact_ids::text, media_file_ids::text
		FROM provider_call_logs
		WHERE id = $1
	`, providerCallID).Scan(&taskType, &callModelID, &artifactIDsRaw, &mediaFileIDsRaw); err != nil {
		t.Fatalf("select provider_call_logs: %v", err)
	}
	if taskType != "image.generate" || callModelID != modelID {
		t.Fatalf("call log mismatch: taskType=%s model=%s", taskType, callModelID)
	}
	if !jsonArrayContains(artifactIDsRaw, output.ArtifactID) || !jsonArrayContains(mediaFileIDsRaw, output.MediaFileID) {
		t.Fatalf("call log ids mismatch: artifact_ids=%s media_file_ids=%s", artifactIDsRaw, mediaFileIDsRaw)
	}
}

func assertImageCostRecordPersisted(t *testing.T, ctx context.Context, pool *pgxpool.Pool, providerCallID string) {
	t.Helper()
	var costType, unit, currency, imageCount, size, quality string
	var amount, quantity float64
	if err := pool.QueryRow(ctx, `
		SELECT cost_type, unit, currency, amount::float8, quantity::float8, metadata->>'imageCount', metadata->>'size', metadata->>'quality'
		FROM cost_records
		WHERE provider_call_id = $1
	`, providerCallID).Scan(&costType, &unit, &currency, &amount, &quantity, &imageCount, &size, &quality); err != nil {
		t.Fatalf("select cost_records: %v", err)
	}
	if costType != "image.generate" || unit != "image" || currency != "USD" || amount != 0.02 || quantity != 1 || imageCount != "1" || size != "1024x1024" || quality != "hd" {
		t.Fatalf("cost row mismatch: type=%s unit=%s currency=%s amount=%f quantity=%f imageCount=%s size=%s quality=%s", costType, unit, currency, amount, quantity, imageCount, size, quality)
	}
}

func jsonArrayContains(raw, want string) bool {
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return false
	}
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func testPNGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var body strings.Builder
	encoder := base64.NewEncoder(base64.StdEncoding, &body)
	if err := png.Encode(encoder, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatalf("close encoder: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(body.String())
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	return decoded
}

type memoryObjectStorage struct {
	mu      sync.Mutex
	objects map[string]memoryObject
}

type memoryObject struct {
	body        []byte
	contentType string
}

func newMemoryObjectStorage() *memoryObjectStorage {
	return &memoryObjectStorage{objects: map[string]memoryObject{}}
}

func (s *memoryObjectStorage) PutBytes(ctx context.Context, key string, body []byte, contentType string) (storage.PutResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bodyCopy := append([]byte(nil), body...)
	s.objects[key] = memoryObject{body: bodyCopy, contentType: contentType}
	sum := sha256.Sum256(bodyCopy)
	return storage.PutResult{
		StorageKey:  key,
		ContentHash: "sha256:" + hex.EncodeToString(sum[:]),
		ByteSize:    int64(len(bodyCopy)),
	}, nil
}

func (s *memoryObjectStorage) get(key string) (memoryObject, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	object, ok := s.objects[key]
	return object, ok
}
