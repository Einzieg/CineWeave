package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/google/uuid"
)

func TestGenerateAssetCardManualOverrideProtectionAndForce(t *testing.T) {
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
		if req.PromptTemplateKey != "asset_card_generation" {
			t.Fatalf("prompt template key = %s", req.PromptTemplateKey)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"data": provider.GatewayTextResponse{
			ProviderCallID: uuid.NewString(),
			ModelID:        "asset-card-model",
			Status:         "succeeded",
			Output: provider.GatewayTextOutput{Text: `{
				"profile": {"appearance": "new stable silhouette"},
				"basePrompt": "new base prompt",
				"consistencyPrompt": "new consistency prompt",
				"negativePrompt": "new negative prompt"
			}`},
			Usage:     provider.GatewayUsage{EstimatedCost: "0.00000000", Currency: "USD"},
			LatencyMS: 12,
		}}); err != nil {
			t.Fatalf("encode gateway response: %v", err)
		}
	}))
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "asset-card-test-token")

	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", "")
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE canonical_assets
		SET manual_override = true,
		    profile = '{"appearance":"old silhouette"}',
		    base_prompt = 'old base prompt',
		    consistency_prompt = 'old consistency prompt',
		    negative_prompt = 'old negative prompt'
		WHERE id = $1
	`, assetID); err != nil {
		t.Fatalf("mark asset manual: %v", err)
	}

	var protected GenerateAssetCardResponse
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID+"/generate-card", seed.ownerToken, seed.organizationID, map[string]any{
		"force": false,
	}, &protected)
	if protected.Applied {
		t.Fatalf("manual override response applied = true")
	}
	assertAssetCardFields(t, seed, assetID, true, "old base prompt", "old consistency prompt", "old negative prompt")

	var forced GenerateAssetCardResponse
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID+"/generate-card", seed.ownerToken, seed.organizationID, map[string]any{
		"force": true,
	}, &forced)
	if !forced.Applied {
		t.Fatalf("force response applied = false")
	}
	assertAssetCardFields(t, seed, assetID, false, "new base prompt", "new consistency prompt", "new negative prompt")
}

func TestSetPrimaryAssetReferenceClearsOtherPrimaries(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", "")
	var first AssetReference
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID+"/references", seed.ownerToken, seed.organizationID, map[string]any{
		"title":         "first",
		"storageKey":    "refs/first.png",
		"mimeType":      "image/png",
		"referenceType": "uploaded",
		"setPrimary":    true,
	}, &first)
	var second AssetReference
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID+"/references", seed.ownerToken, seed.organizationID, map[string]any{
		"title":         "second",
		"storageKey":    "refs/second.png",
		"mimeType":      "image/png",
		"referenceType": "uploaded",
		"setPrimary":    true,
	}, &second)
	assertOnlyPrimaryReference(t, seed, assetID, second.ID)

	var response struct {
		AssetID   string         `json:"assetId"`
		Reference AssetReference `json:"reference"`
	}
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID+"/references/"+first.ID+"/set-primary", seed.ownerToken, seed.organizationID, nil, &response)
	if response.AssetID != assetID || !response.Reference.IsPrimary {
		t.Fatalf("set-primary response = %+v", response)
	}
	assertOnlyPrimaryReference(t, seed, assetID, first.ID)
}

func TestGenerateCanonicalAssetImageWritesAssetReference(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "scene", "Station Platform", "approved", "")
	artifactID := seed.insertArtifact(t, "generated_image", "generated/station.png", "image/png")
	mediaFileID := seed.insertMediaFile(t, artifactID, "generated/station.png", "image/png")
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/image/generate" {
			http.NotFound(w, r)
			return
		}
		var req provider.GatewayImageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode gateway request: %v", err)
		}
		if req.PromptTemplateKey != "canonical_asset_image_prompt" {
			t.Fatalf("prompt template key = %s", req.PromptTemplateKey)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"data": provider.GatewayImageResponse{
			ProviderCallID: uuid.NewString(),
			ModelID:        "image-model",
			Status:         "succeeded",
			Output: provider.GatewayImageOutput{
				ArtifactID:  artifactID,
				MediaFileID: mediaFileID,
				StorageKey:  "generated/station.png",
				MimeType:    "image/png",
			},
			Usage:     provider.GatewayUsage{EstimatedCost: "0.00000000", Currency: "USD"},
			LatencyMS: 20,
		}}); err != nil {
			t.Fatalf("encode gateway response: %v", err)
		}
	}))
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "asset-image-test-token")

	var result struct {
		Asset          CanonicalAsset `json:"asset"`
		ProviderCallID string         `json:"providerCallId"`
	}
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID+"/generate-image", seed.ownerToken, seed.organizationID, map[string]any{}, &result)
	if result.Asset.PrimaryReferenceStorageKey == nil || *result.Asset.PrimaryReferenceStorageKey != "generated/station.png" {
		t.Fatalf("generated asset = %+v", result.Asset)
	}
	assertOnlyPrimaryReferenceStorage(t, seed, assetID, "generated/station.png")
}

func assertAssetCardFields(t *testing.T, seed *artifactPreviewSeed, assetID string, wantManual bool, wantBase, wantConsistency, wantNegative string) {
	t.Helper()
	var manual bool
	var base, consistency, negative string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT manual_override, COALESCE(base_prompt, ''), COALESCE(consistency_prompt, ''), COALESCE(negative_prompt, '')
		FROM canonical_assets
		WHERE id = $1 AND project_id = $2
	`, assetID, seed.projectID).Scan(&manual, &base, &consistency, &negative); err != nil {
		t.Fatalf("read asset card fields: %v", err)
	}
	if manual != wantManual || base != wantBase || consistency != wantConsistency || negative != wantNegative {
		t.Fatalf("asset card fields manual=%v base=%q consistency=%q negative=%q", manual, base, consistency, negative)
	}
}

func assertOnlyPrimaryReference(t *testing.T, seed *artifactPreviewSeed, assetID, wantReferenceID string) {
	t.Helper()
	var primaryCount int
	var primaryID string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT count(*) FILTER (WHERE is_primary), COALESCE(max(id::text) FILTER (WHERE is_primary), '')
		FROM asset_references
		WHERE asset_id = $1 AND project_id = $2
	`, assetID, seed.projectID).Scan(&primaryCount, &primaryID); err != nil {
		t.Fatalf("read primary references: %v", err)
	}
	if primaryCount != 1 || primaryID != wantReferenceID {
		t.Fatalf("primary count=%d id=%s want id=%s", primaryCount, primaryID, wantReferenceID)
	}
}

func assertOnlyPrimaryReferenceStorage(t *testing.T, seed *artifactPreviewSeed, assetID, wantStorageKey string) {
	t.Helper()
	var primaryCount int
	var storageKey string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT count(*) FILTER (WHERE is_primary), COALESCE(max(storage_key) FILTER (WHERE is_primary), '')
		FROM asset_references
		WHERE asset_id = $1 AND project_id = $2
	`, assetID, seed.projectID).Scan(&primaryCount, &storageKey); err != nil {
		t.Fatalf("read generated primary reference: %v", err)
	}
	if primaryCount != 1 || storageKey != wantStorageKey {
		t.Fatalf("primary count=%d storageKey=%s want %s", primaryCount, storageKey, wantStorageKey)
	}
}
