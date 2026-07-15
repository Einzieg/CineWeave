package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
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

func TestGenerateAssetCardLocksTypedVisualManualAndRepairsStyleDrift(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	bindDefaultProjectManualForPromptTest(t, seed, "visual", "toonflow_visual_manual_3d_chinese_traditional")
	var visualManualVersionID string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT prompt_version_id::text
		FROM project_manual_bindings
		WHERE project_id = $1 AND manual_kind = 'visual' AND status = 'active'
		ORDER BY updated_at DESC
		LIMIT 1
	`, seed.projectID).Scan(&visualManualVersionID); err != nil {
		t.Fatalf("read visual manual binding: %v", err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE projects
		SET art_style = '3d_chinese_traditional', director_manual = 'UNRELATED_DIRECTOR_MANUAL'
		WHERE id = $1
	`, seed.projectID); err != nil {
		t.Fatalf("update project style: %v", err)
	}
	assetID := seed.insertCanonicalAsset(t, "scene", "Insect Vault", "approved", "")
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE canonical_assets
		SET profile = '{"era":"CORRUPTED_MODERN_PROFILE"}',
		    base_prompt = 'CORRUPTED_OLD_LIVE_ACTION_PROMPT',
		    consistency_prompt = 'CORRUPTED_OLD_CONSISTENCY',
		    negative_prompt = 'CORRUPTED_OLD_NEGATIVE'
		WHERE id = $1
	`, assetID); err != nil {
		t.Fatalf("seed corrupt asset card: %v", err)
	}

	attempts := 0
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/text/generate" {
			http.NotFound(w, r)
			return
		}
		attempts++
		var req provider.GatewayTextRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode gateway request: %v", err)
		}
		var input map[string]any
		if err := json.Unmarshal(req.Input, &input); err != nil {
			t.Fatalf("decode gateway input: %v", err)
		}
		prompt, _ := input["prompt"].(string)
		for _, forbidden := range []string{"UNRELATED_DIRECTOR_MANUAL", "CORRUPTED_OLD_LIVE_ACTION_PROMPT", "CORRUPTED_MODERN_PROFILE"} {
			if strings.Contains(prompt, forbidden) {
				t.Fatalf("asset card prompt retained forbidden context %q: %s", forbidden, prompt)
			}
		}
		for _, required := range []string{visualManualVersionID, "toonflow_visual_3d_chinese_traditional_art_scene", "3d_chinese_traditional"} {
			if !strings.Contains(prompt, required) {
				t.Fatalf("asset card prompt missing visual snapshot %q: %s", required, prompt)
			}
		}
		if attempts == 2 && !strings.Contains(prompt, "混入真人摄影风格") {
			t.Fatalf("repair prompt missing validation feedback: %s", prompt)
		}
		output := `{
			"profile": {"era": "当代中国都市"},
			"basePrompt": "真人都市场景全景摄影，35mm全画幅摄影质感",
			"consistencyPrompt": "保持真实摄影画质",
			"negativePrompt": "3D渲染"
		}`
		if attempts == 2 {
			output = `{
				"profile": {"era": "古代山地氏族", "style": "国风3D"},
				"basePrompt": "国风3D场景设定图，高精度建模，PBR材质，古代山地氏族蛊室",
				"consistencyPrompt": "保持国风3D渲染、古代空间结构和PBR材质一致",
				"negativePrompt": "真人摄影，现代都市，人物，文字，水印"
			}`
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"data": provider.GatewayTextResponse{
			ProviderCallID: uuid.NewString(),
			ModelID:        "asset-card-model",
			Status:         "succeeded",
			Output:         provider.GatewayTextOutput{Text: output},
			Usage:          provider.GatewayUsage{EstimatedCost: "0.00000000", Currency: "USD"},
			LatencyMS:      12,
		}}); err != nil {
			t.Fatalf("encode gateway response: %v", err)
		}
	}))
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "asset-card-visual-contract-test-token")

	var result GenerateAssetCardResponse
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID+"/generate-card", seed.ownerToken, seed.organizationID, map[string]any{
		"visualManualPromptVersionId": visualManualVersionID,
	}, &result)
	if attempts != 2 {
		t.Fatalf("gateway attempts = %d, want 2", attempts)
	}
	if result.VisualManualPromptVersionID != visualManualVersionID || result.VisualStyleSlug != "3d_chinese_traditional" || result.AssetTypeTemplateKey != "toonflow_visual_3d_chinese_traditional_art_scene" {
		t.Fatalf("visual provenance response = %+v", result)
	}
	assertAssetCardFields(t, seed, assetID, false, "国风3D场景设定图，高精度建模，PBR材质，古代山地氏族蛊室", "保持国风3D渲染、古代空间结构和PBR材质一致", "真人摄影，现代都市，人物，文字，水印")

	var metadata map[string]any
	if err := seed.pool.QueryRow(seed.ctx, `SELECT metadata FROM canonical_assets WHERE id = $1`, assetID).Scan(&metadata); err != nil {
		t.Fatalf("read asset metadata: %v", err)
	}
	if metadata["visualManualPromptVersionId"] != visualManualVersionID || metadata["visualStyleSlug"] != "3d_chinese_traditional" || metadata["assetTypeTemplateKey"] != "toonflow_visual_3d_chinese_traditional_art_scene" || metadata["generationMode"] != "fresh_from_source" {
		t.Fatalf("asset visual provenance = %+v", metadata)
	}
	callIDs, _ := metadata["providerCallIds"].([]any)
	if len(callIDs) != 2 {
		t.Fatalf("provider call provenance = %+v", metadata["providerCallIds"])
	}
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

func TestDeleteAssetReferenceArchivesWithoutDeletingMedia(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", "")
	var ref AssetReference
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID+"/references", seed.ownerToken, seed.organizationID, map[string]any{
		"title":         "front",
		"storageKey":    "refs/front.png",
		"mimeType":      "image/png",
		"referenceType": "uploaded",
		"setPrimary":    true,
	}, &ref)
	if ref.ArtifactID == nil || ref.MediaFileID == nil || ref.StorageKey == nil || !ref.IsPrimary {
		t.Fatalf("created reference = %+v", ref)
	}

	var deleted struct {
		Deleted         bool   `json:"deleted"`
		Mode            string `json:"mode"`
		ReferenceID     string `json:"referenceId"`
		ArtifactDeleted bool   `json:"artifactDeleted"`
		MediaDeleted    bool   `json:"mediaDeleted"`
	}
	doAPISuccess(t, server, http.MethodDelete, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID+"/references/"+ref.ID, seed.ownerToken, seed.organizationID, nil, &deleted)
	if !deleted.Deleted || deleted.Mode != "archive" || deleted.ReferenceID != ref.ID || deleted.ArtifactDeleted || deleted.MediaDeleted {
		t.Fatalf("delete response = %+v", deleted)
	}

	var status string
	var primary bool
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT status, is_primary
		FROM asset_references
		WHERE id = $1 AND project_id = $2
	`, ref.ID, seed.projectID).Scan(&status, &primary); err != nil {
		t.Fatalf("read archived reference: %v", err)
	}
	if status != "archived" || primary {
		t.Fatalf("archived reference status=%s primary=%v", status, primary)
	}

	var artifactCount, mediaCount int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT
			(SELECT COUNT(*) FROM artifacts WHERE id = $1),
			(SELECT COUNT(*) FROM media_files WHERE id = $2)
	`, *ref.ArtifactID, *ref.MediaFileID).Scan(&artifactCount, &mediaCount); err != nil {
		t.Fatalf("read artifact/media counts: %v", err)
	}
	if artifactCount != 1 || mediaCount != 1 {
		t.Fatalf("artifactCount=%d mediaCount=%d, want both retained", artifactCount, mediaCount)
	}

	var primaryArtifactID, referenceArtifactID, primaryStorageKey, referenceStorageKey string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT COALESCE(primary_reference_artifact_id::text, ''), COALESCE(reference_artifact_id::text, ''),
		       COALESCE(primary_reference_storage_key, ''), COALESCE(reference_storage_key, '')
		FROM canonical_assets
		WHERE id = $1 AND project_id = $2
	`, assetID, seed.projectID).Scan(&primaryArtifactID, &referenceArtifactID, &primaryStorageKey, &referenceStorageKey); err != nil {
		t.Fatalf("read canonical asset references: %v", err)
	}
	if primaryArtifactID != "" || referenceArtifactID != "" || primaryStorageKey != "" || referenceStorageKey != "" {
		t.Fatalf("canonical asset reference fields not cleared: primary=%s reference=%s primaryStorage=%s referenceStorage=%s", primaryArtifactID, referenceArtifactID, primaryStorageKey, referenceStorageKey)
	}

	var list struct {
		Items []AssetReference `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID+"/references", seed.ownerToken, seed.organizationID, nil, &list)
	if len(list.Items) != 0 {
		t.Fatalf("archived reference appeared in default list: %+v", list.Items)
	}
}

func TestGenerateCanonicalAssetImageWritesAssetReference(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "scene", "Station Platform", "approved", "")
	markCanonicalAssetPromptReady(t, seed, assetID)
	bindDefaultProjectManualForPromptTest(t, seed, "director", "default_director_manual")
	bindDefaultProjectManualForPromptTest(t, seed, "visual", "default_visual_manual")
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE projects
		SET director_manual = 'BROKEN_DIRECTOR_MANUAL',
		    visual_manual = 'BROKEN_VISUAL_MANUAL'
		WHERE id = $1
	`, seed.projectID); err != nil {
		t.Fatalf("corrupt project manual columns: %v", err)
	}
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
		var input map[string]any
		if err := json.Unmarshal(req.Input, &input); err != nil {
			t.Fatalf("decode gateway input: %v", err)
		}
		prompt, _ := input["prompt"].(string)
		if strings.Contains(prompt, "BROKEN_") || strings.Contains(prompt, "????") {
			t.Fatalf("gateway prompt contains stale or garbled manual: %q", prompt)
		}
		if !strings.Contains(prompt, "默认导演手册") || !strings.Contains(prompt, "默认视觉手册") {
			t.Fatalf("gateway prompt did not include active manual binding content: %q", prompt)
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

func TestGenerateCanonicalCharacterImageUsesTurnaroundPromptAndLayout(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", "")
	markCanonicalAssetPromptReady(t, seed, assetID)
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE canonical_assets SET status = 'draft' WHERE id = $1`, assetID); err != nil {
		t.Fatalf("mark complete prompt card draft: %v", err)
	}
	artifactID := seed.insertArtifact(t, "generated_image", "generated/lin-chu-turnaround.png", "image/png")
	mediaFileID := seed.insertMediaFile(t, artifactID, "generated/lin-chu-turnaround.png", "image/png")
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/image/generate" {
			http.NotFound(w, r)
			return
		}
		var req provider.GatewayImageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode gateway request: %v", err)
		}
		var input map[string]any
		if err := json.Unmarshal(req.Input, &input); err != nil {
			t.Fatalf("decode gateway input: %v", err)
		}
		prompt, _ := input["prompt"].(string)
		if !strings.Contains(prompt, "角色四视图设定图") || !strings.Contains(prompt, "人像特写、正视图全身、侧视图全身、后视图全身") {
			t.Fatalf("turnaround prompt missing four-view requirements: %q", prompt)
		}
		if input["size"] != "2048x1152" || input["aspectRatio"] != "16:9" {
			t.Fatalf("turnaround layout input = %#v", input)
		}
		if req.PromptHash != promptsvc.HashText(prompt) {
			t.Fatalf("prompt hash = %s, want %s", req.PromptHash, promptsvc.HashText(prompt))
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"data": provider.GatewayImageResponse{
			ProviderCallID: uuid.NewString(),
			ModelID:        "image-model",
			Status:         "succeeded",
			Output: provider.GatewayImageOutput{
				ArtifactID:  artifactID,
				MediaFileID: mediaFileID,
				StorageKey:  "generated/lin-chu-turnaround.png",
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
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "asset-image-turnaround-test-token")

	var result struct {
		Asset          CanonicalAsset `json:"asset"`
		ProviderCallID string         `json:"providerCallId"`
	}
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID+"/generate-image", seed.ownerToken, seed.organizationID, map[string]any{}, &result)
	if result.Asset.PrimaryReferenceStorageKey == nil || *result.Asset.PrimaryReferenceStorageKey != "generated/lin-chu-turnaround.png" {
		t.Fatalf("generated asset = %+v", result.Asset)
	}
	assertOnlyPrimaryReferenceStorage(t, seed, assetID, "generated/lin-chu-turnaround.png")
}

func TestGenerateCanonicalLockedAssetImageSendsPrimaryReference(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", "")
	markCanonicalAssetPromptReady(t, seed, assetID)
	var ref AssetReference
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID+"/references", seed.ownerToken, seed.organizationID, map[string]any{
		"title":         "locked reference",
		"storageKey":    "refs/locked-lin-chu.png",
		"mimeType":      "image/png",
		"referenceType": "uploaded",
		"setPrimary":    true,
	}, &ref)
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE canonical_assets SET lock_reference = true WHERE id = $1`, assetID); err != nil {
		t.Fatalf("lock reference: %v", err)
	}
	artifactID := seed.insertArtifact(t, "generated_image", "generated/lin-chu-locked.png", "image/png")
	mediaFileID := seed.insertMediaFile(t, artifactID, "generated/lin-chu-locked.png", "image/png")
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/image/generate" {
			http.NotFound(w, r)
			return
		}
		var req provider.GatewayImageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode gateway request: %v", err)
		}
		if len(req.References) != 1 {
			t.Fatalf("references = %+v, want locked primary reference", req.References)
		}
		if req.References[0].AssetID != assetID || req.References[0].StorageKey != "refs/locked-lin-chu.png" || req.References[0].ArtifactID != *ref.ArtifactID {
			t.Fatalf("locked reference = %+v", req.References[0])
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"data": provider.GatewayImageResponse{
			ProviderCallID: uuid.NewString(),
			ModelID:        "image-model",
			Status:         "succeeded",
			Output: provider.GatewayImageOutput{
				ArtifactID:  artifactID,
				MediaFileID: mediaFileID,
				StorageKey:  "generated/lin-chu-locked.png",
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
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "asset-image-lock-reference-test-token")

	var result struct {
		Asset          CanonicalAsset `json:"asset"`
		ProviderCallID string         `json:"providerCallId"`
	}
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID+"/generate-image", seed.ownerToken, seed.organizationID, map[string]any{}, &result)
	assertOnlyPrimaryReferenceStorage(t, seed, assetID, "refs/locked-lin-chu.png")
}

func bindDefaultProjectManualForPromptTest(t *testing.T, seed *artifactPreviewSeed, manualKind, templateKey string) {
	t.Helper()
	var versionID string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT pv.id::text
		FROM prompt_templates pt
		JOIN prompt_versions pv ON pv.template_id = pt.id
		WHERE pt.organization_id IS NULL
		  AND pt.template_key = $1
		  AND pt.status = 'active'
		  AND pv.status = 'active'
		ORDER BY COALESCE(pv.activated_at, pv.created_at) DESC
		LIMIT 1
	`, templateKey).Scan(&versionID); err != nil {
		t.Fatalf("find default manual version %s: %v", templateKey, err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `
		INSERT INTO project_manual_bindings(organization_id, project_id, manual_kind, prompt_version_id, status, created_by)
		VALUES ($1, $2, $3, $4, 'active', $5)
	`, seed.organizationID, seed.projectID, manualKind, versionID, seed.ownerUserID); err != nil {
		t.Fatalf("bind default manual %s: %v", templateKey, err)
	}
}

func TestGenerateCanonicalAssetImageRejectsPromptNotReady(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "scene", "Station Platform", "approved", "")
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE canonical_assets
		SET status = 'prompt_ready',
		    base_prompt = 'initial base prompt',
		    consistency_prompt = NULL,
		    negative_prompt = NULL
		WHERE id = $1
	`, assetID); err != nil {
		t.Fatalf("mark asset incomplete: %v", err)
	}

	assertAPIErrorCode(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID+"/generate-image", seed.ownerToken, seed.organizationID, map[string]any{}, http.StatusUnprocessableEntity, "ASSET_PROMPT_NOT_READY")
}

func TestGenerateCanonicalAssetImagePersistsGatewayFailure(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "scene", "Station Platform", "approved", "")
	markCanonicalAssetPromptReady(t, seed, assetID)
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE canonical_assets
		SET metadata = jsonb_build_object(
			'imageFailedAt', now() - interval '1 hour',
			'imageFailedReason', 'previous failure'
		)
		WHERE id = $1
	`, assetID); err != nil {
		t.Fatalf("seed previous image failure: %v", err)
	}

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/image/generate" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"data": provider.GatewayImageResponse{
			ProviderCallID: uuid.NewString(),
			ModelID:        "image-model",
			Status:         "failed",
			Error: &provider.StandardError{
				Code:      provider.CodeMediaDownloadFailed,
				Message:   "provider image media could not be stored",
				Retryable: true,
			},
		}}); err != nil {
			t.Fatalf("encode gateway response: %v", err)
		}
	}))
	defer gateway.Close()
	t.Setenv("PROVIDER_GATEWAY_URL", gateway.URL)
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "asset-image-failure-test-token")

	assertAPIErrorCode(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID+"/generate-image", seed.ownerToken, seed.organizationID, map[string]any{}, http.StatusBadGateway, provider.CodeMediaDownloadFailed)

	var status string
	var metadata map[string]any
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT status, metadata
		FROM canonical_assets
		WHERE id = $1
	`, assetID).Scan(&status, &metadata); err != nil {
		t.Fatalf("read failed asset state: %v", err)
	}
	if status != "image_failed" {
		t.Fatalf("asset status = %q, want image_failed", status)
	}
	if metadata["imageFailedReason"] != "provider image media could not be stored" || metadata["imageFailedAt"] == nil {
		t.Fatalf("asset failure metadata = %+v", metadata)
	}
	if metadata["imageStartedAt"] == nil {
		t.Fatalf("asset start metadata = %+v", metadata)
	}
}

func TestUpdateCanonicalAssetManualPromptsMarksCardReady(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "prop", "Oil Lamp", "approved", "")
	var revision int64
	if err := seed.pool.QueryRow(seed.ctx, `SELECT revision FROM canonical_assets WHERE id = $1`, assetID).Scan(&revision); err != nil {
		t.Fatalf("read asset revision: %v", err)
	}
	var updated CanonicalAsset
	doAPISuccess(t, server, http.MethodPatch, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID, seed.ownerToken, seed.organizationID, map[string]any{
		"basePrompt":        "four-view oil lamp design",
		"consistencyPrompt": "preserve shape, material, and scale",
		"negativePrompt":    "people, hands, text",
		"expectedRevision":  revision,
	}, &updated)

	if updated.Status != "prompt_ready" || !updated.ManualOverride {
		t.Fatalf("updated asset status=%q manualOverride=%v", updated.Status, updated.ManualOverride)
	}
	assertAssetCardFields(t, seed, assetID, true, "four-view oil lamp design", "preserve shape, material, and scale", "people, hands, text")
}

func TestUpdateCanonicalAssetRevisionConflictReturnsCurrentRevision(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", "")
	var revision int64
	if err := seed.pool.QueryRow(seed.ctx, `SELECT revision FROM canonical_assets WHERE id = $1`, assetID).Scan(&revision); err != nil {
		t.Fatalf("read asset revision: %v", err)
	}
	var updated CanonicalAsset
	doAPISuccess(t, server, http.MethodPatch, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID, seed.ownerToken, seed.organizationID, map[string]any{
		"description":      "updated by another operation",
		"expectedRevision": revision,
	}, &updated)

	recorder := doAPIRequest(t, server, http.MethodPatch, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID, seed.ownerToken, seed.organizationID, map[string]any{
		"name":             "stale update",
		"expectedRevision": revision,
	})
	if recorder.Code != http.StatusConflict {
		t.Fatalf("stale update status = %d, want %d body=%s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
	var envelope struct {
		Error *struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode conflict response: %v", err)
	}
	if envelope.Error == nil || envelope.Error.Code != "ASSET_REVISION_CONFLICT" || envelope.Error.Message == "canonical asset was changed by another operation" {
		t.Fatalf("conflict error = %+v", envelope.Error)
	}
	currentRevision, ok := envelope.Error.Details["currentRevision"].(float64)
	if !ok {
		t.Fatalf("current revision details = %+v", envelope.Error.Details)
	}
	if got := int64(currentRevision); got != updated.Revision {
		t.Fatalf("current revision = %d, want %d", got, updated.Revision)
	}
}

func TestGenerateDerivedAssetImageWritesPromptProvenance(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", "")
	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	shotID := seed.insertProductionShot(t, workflowRunID, "", "", "approved", "image_succeeded")
	requirementID := seed.insertShotAssetRequirement(t, workflowRunID, shotID, assetID, "approved", "")
	artifactID := seed.insertArtifact(t, "generated_image", "derived/lin-chu.png", "image/png")
	mediaFileID := seed.insertMediaFile(t, artifactID, "derived/lin-chu.png", "image/png")
	providerCallID := uuid.NewString()
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/image/generate" {
			http.NotFound(w, r)
			return
		}
		var req provider.GatewayImageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode gateway request: %v", err)
		}
		if req.PromptTemplateKey != "derived_asset_image_prompt" || req.PromptHash == "" {
			t.Fatalf("gateway request = %+v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"data": provider.GatewayImageResponse{
			ProviderCallID: providerCallID,
			ModelID:        "derived-image-model",
			Status:         "succeeded",
			Output: provider.GatewayImageOutput{
				ArtifactID:  artifactID,
				MediaFileID: mediaFileID,
				StorageKey:  "derived/lin-chu.png",
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
	t.Setenv("CINEWEAVE_SERVICE_TOKEN", "derived-asset-image-test-token")

	var result struct {
		Requirement    ShotAssetRequirement `json:"requirement"`
		ProviderCallID string               `json:"providerCallId"`
	}
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/shot-asset-requirements/"+requirementID+"/generate-image", seed.ownerToken, seed.organizationID, map[string]any{}, &result)
	if result.Requirement.Prompt == nil || *result.Requirement.Prompt == "" || result.Requirement.DerivedStorageKey == nil || *result.Requirement.DerivedStorageKey != "derived/lin-chu.png" {
		t.Fatalf("requirement = %+v", result.Requirement)
	}
	var metadata map[string]any
	if err := seed.pool.QueryRow(seed.ctx, `SELECT metadata FROM shot_asset_requirements WHERE id = $1`, requirementID).Scan(&metadata); err != nil {
		t.Fatalf("read requirement metadata: %v", err)
	}
	if metadata["providerCallId"] != providerCallID || metadata["modelId"] != "derived-image-model" || metadata["promptTemplateKey"] != "derived_asset_image_prompt" || metadata["promptHash"] == "" {
		t.Fatalf("requirement metadata = %+v", metadata)
	}
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

func markCanonicalAssetPromptReady(t *testing.T, seed *artifactPreviewSeed, assetID string) {
	t.Helper()
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE canonical_assets
		SET status = 'prompt_ready',
		    base_prompt = 'ready base prompt',
		    consistency_prompt = 'ready consistency prompt',
		    negative_prompt = 'ready negative prompt'
		WHERE id = $1
	`, assetID); err != nil {
		t.Fatalf("mark asset prompt ready: %v", err)
	}
}

func TestArchiveCanonicalAssetHidesFromDefaultListAndKeepsLinks(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", "")
	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	shotID := seed.insertProductionShot(t, workflowRunID, "", "", "approved", "image_succeeded")
	requirementID := seed.insertShotAssetRequirement(t, workflowRunID, shotID, assetID, "approved", "")
	var reference AssetReference
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID+"/references", seed.ownerToken, seed.organizationID, map[string]any{
		"storageKey":    "refs/lin-chu.png",
		"mimeType":      "image/png",
		"referenceType": "uploaded",
		"title":         "Lin Chu reference",
	}, &reference)

	var impact OutputImpact
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID+"/impact", seed.ownerToken, seed.organizationID, nil, &impact)
	if !impact.CanDelete || impact.RecommendedMode != "archive" {
		t.Fatalf("impact = %+v", impact)
	}
	if !impactHasAffected(impact, "asset_reference", 1) || !impactHasAffected(impact, "shot_asset_requirement", 1) {
		t.Fatalf("impact affected = %+v", impact.Affected)
	}

	var deleted struct {
		Deleted bool   `json:"deleted"`
		Mode    string `json:"mode"`
		AssetID string `json:"assetId"`
	}
	doAPISuccess(t, server, http.MethodDelete, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID, seed.ownerToken, seed.organizationID, nil, &deleted)
	if !deleted.Deleted || deleted.Mode != "archive" || deleted.AssetID != assetID {
		t.Fatalf("deleted = %+v", deleted)
	}

	var defaultList struct {
		Items []CanonicalAsset `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/canonical-assets", seed.ownerToken, seed.organizationID, nil, &defaultList)
	for _, item := range defaultList.Items {
		if item.ID == assetID {
			t.Fatalf("archived asset returned in default list: %+v", defaultList.Items)
		}
	}
	var allList struct {
		Items []CanonicalAsset `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/canonical-assets?filter[status]=all", seed.ownerToken, seed.organizationID, nil, &allList)
	if len(allList.Items) != 1 || allList.Items[0].ID != assetID || allList.Items[0].Status != "archived" {
		t.Fatalf("all list = %+v", allList.Items)
	}
	var requirementCount, referenceCount int
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM shot_asset_requirements WHERE id = $1`, requirementID).Scan(&requirementCount); err != nil {
		t.Fatalf("count requirement: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM asset_references WHERE id = $1`, reference.ID).Scan(&referenceCount); err != nil {
		t.Fatalf("count reference: %v", err)
	}
	if requirementCount != 1 || referenceCount != 1 {
		t.Fatalf("requirement/reference counts = %d/%d", requirementCount, referenceCount)
	}
}

func impactHasAffected(impact OutputImpact, entityType string, count int) bool {
	for _, item := range impact.Affected {
		if item.EntityType == entityType && item.Count == count {
			return true
		}
	}
	return false
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
