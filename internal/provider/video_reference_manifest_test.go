package provider

import (
	"strings"
	"testing"

	"github.com/Einzieg/cineweave/internal/videocontracts"
)

func TestGatewayVideoReferenceManifestMatchesOrderedRequest(t *testing.T) {
	capabilityHash := strings.Repeat("a", 64)
	references := []GatewayVideoReference{
		{
			ReferenceKey: "tail:segment-1", Role: "first_frame", Required: true, Type: "image",
			SourceType: "video_render_segment_tail_anchor", SourceID: "anchor-1", SourceVersion: "segment-1",
			ArtifactID: "artifact-tail", MediaFileID: "media-tail", ContentHash: strings.Repeat("b", 64),
			GeneratedAt: "2026-07-21T12:00:00Z",
		},
		{
			ReferenceKey: "character:asset-1", Role: "character_identity", Required: true, Priority: 100,
			Type: "image", Semantics: "character_identity", SourceType: "canonical_asset", SourceID: "asset-1",
			SourceVersion: "asset-version-1", AssetID: "asset-1", ArtifactID: "artifact-character",
			ContentHash: strings.Repeat("c", 64),
		},
	}
	items := make([]videocontracts.ReferenceManifestItemV2, 0, len(references))
	for index, reference := range references {
		items = append(items, videocontracts.ReferenceManifestItemV2{
			Order: index, ReferenceKey: reference.ReferenceKey, Role: reference.Role,
			Required: reference.Required, Priority: reference.Priority, MediaType: gatewayVideoReferenceMediaType(reference),
			Semantics: reference.Semantics, SourceType: reference.SourceType, SourceID: reference.SourceID,
			SourceVersion: reference.SourceVersion, AssetID: reference.AssetID, ArtifactID: reference.ArtifactID,
			MediaFileID: reference.MediaFileID, ContentHash: reference.ContentHash, GeneratedAt: reference.GeneratedAt,
		})
	}
	manifest, manifestHash, err := videocontracts.BuildReferenceManifestV2(VideoInputContractFirstFramePlusReferences, capabilityHash, items)
	if err != nil {
		t.Fatal(err)
	}
	req := GatewayVideoCreateTaskRequest{References: references, ReferenceManifest: &manifest, ReferenceManifestHash: manifestHash}
	segment := videoExecutionSegment{InputContractKey: VideoInputContractFirstFramePlusReferences, CapabilitySnapshotHash: capabilityHash}
	if err := validateGatewayVideoReferenceManifest(req, segment); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}

	tampered := req
	tampered.References = append([]GatewayVideoReference(nil), references...)
	tampered.References[1].ArtifactID = "artifact-character-v2"
	if err := validateGatewayVideoReferenceManifest(tampered, segment); err == nil {
		t.Fatal("reference version drift must be rejected before provider execution")
	}
	missing := GatewayVideoCreateTaskRequest{References: references}
	if err := validateGatewayVideoReferenceManifest(missing, segment); err == nil {
		t.Fatal("plus-references request without ReferenceManifestV2 must be rejected")
	}
}

func TestGatewayVideoReferenceManifestAndCapabilityRejectReferenceLimitBeforeExecution(t *testing.T) {
	contract := VideoInputContract{
		ContractKey: VideoInputContractFirstFramePlusReferences,
		Slots: []VideoInputSlot{
			{Role: "first_frame", MediaType: "image", Min: 1, Max: 1, Ordered: true},
			{Role: "semantic_reference", MediaType: "image", Min: 0, Max: 1},
		},
	}
	references := []GatewayVideoReference{
		{Role: "first_frame", Type: "image", ArtifactID: "tail"},
		{Role: "character_identity", Type: "image", ArtifactID: "character"},
		{Role: "scene_identity", Type: "image", ArtifactID: "scene"},
	}
	if err := validateGatewayVideoReferencesForContract(references, contract); err == nil {
		t.Fatal("semantic reference count above the approved capability snapshot must be rejected")
	}
}

func TestGatewayVideoRequestHashIncludesFrozenReferenceVersion(t *testing.T) {
	capabilityHash := strings.Repeat("a", 64)
	references := []GatewayVideoReference{
		{
			ReferenceKey: "first-frame:shot-1", Role: "first_frame", Required: true, Priority: 200,
			Type: "image", Semantics: "output_start_frame", SourceType: "shot_visual_anchor", SourceID: "anchor-1",
			SourceVersion: "anchor-version-1", ArtifactID: "artifact-first-frame", ContentHash: strings.Repeat("d", 64),
		},
		{
			ReferenceKey: "character:asset-1", Role: "character_identity", Required: true, Priority: 100,
			Type: "image", Semantics: "character_identity", SourceType: "canonical_asset", SourceID: "asset-1",
			SourceVersion: "asset-version-1", AssetID: "asset-1", ArtifactID: "artifact-character-v1",
			ContentHash: strings.Repeat("b", 64),
		},
	}
	items := make([]videocontracts.ReferenceManifestItemV2, 0, len(references))
	for index, reference := range references {
		items = append(items, videocontracts.ReferenceManifestItemV2{
			Order: index, ReferenceKey: reference.ReferenceKey, Role: reference.Role,
			Required: reference.Required, Priority: reference.Priority, MediaType: "image", Semantics: reference.Semantics,
			SourceType: reference.SourceType, SourceID: reference.SourceID,
			SourceVersion: reference.SourceVersion, AssetID: reference.AssetID,
			ArtifactID: reference.ArtifactID, ContentHash: reference.ContentHash,
		})
	}
	manifest, manifestHash, err := videocontracts.BuildReferenceManifestV2(
		VideoInputContractFirstFramePlusReferences,
		capabilityHash,
		items,
	)
	if err != nil {
		t.Fatal(err)
	}
	req := GatewayVideoCreateTaskRequest{
		OrganizationID: "organization", ProjectID: "project", ExecutionPlanID: "plan", RenderSegmentID: "segment",
		CapabilitySnapshotHash: capabilityHash, ReferenceManifest: &manifest, ReferenceManifestHash: manifestHash,
		Input: mustJSON(map[string]any{"prompt": "approved prompt", "duration": 5}), References: references,
	}
	baseHash, err := gatewayRequestHash(req)
	if err != nil {
		t.Fatal(err)
	}
	repeatedHash, err := gatewayRequestHash(req)
	if err != nil {
		t.Fatal(err)
	}
	if repeatedHash != baseHash {
		t.Fatalf("same frozen request hash changed: %s != %s", repeatedHash, baseHash)
	}

	changed := req
	changed.References = append([]GatewayVideoReference(nil), req.References...)
	changed.References[1].SourceVersion = "asset-version-2"
	changed.References[1].ArtifactID = "artifact-character-v2"
	changed.References[1].ContentHash = strings.Repeat("c", 64)
	changedItems := append([]videocontracts.ReferenceManifestItemV2(nil), items...)
	changedItems[1].SourceVersion = changed.References[1].SourceVersion
	changedItems[1].ArtifactID = changed.References[1].ArtifactID
	changedItems[1].ContentHash = changed.References[1].ContentHash
	changedManifest, changedManifestHash, err := videocontracts.BuildReferenceManifestV2(
		VideoInputContractFirstFramePlusReferences,
		capabilityHash,
		changedItems,
	)
	if err != nil {
		t.Fatal(err)
	}
	changed.ReferenceManifest = &changedManifest
	changed.ReferenceManifestHash = changedManifestHash
	changedHash, err := gatewayRequestHash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if changedHash == baseHash {
		t.Fatal("reference source version/content drift must change the provider request hash")
	}
}
