package videocontracts

import "testing"

func TestRequiresPreviousTailFrameCoversFrameContinuationContracts(t *testing.T) {
	for _, key := range []string{string(InputContractFirstFrame), string(InputContractFirstFramePlusReferences)} {
		if !RequiresPreviousTailFrame(key) {
			t.Fatalf("%s must require the previous tail frame", key)
		}
	}
	for _, key := range []string{string(InputContractVideoExtension), string(InputContractFirstLastFrames), "unknown"} {
		if RequiresPreviousTailFrame(key) {
			t.Fatalf("%s must not require the previous tail frame", key)
		}
	}
}

func TestReferenceManifestV2HashChangesWithReferenceVersion(t *testing.T) {
	items := []ReferenceManifestItemV2{
		{
			Order: 0, ReferenceKey: "tail:segment-1", Role: "first_frame", Required: true,
			MediaType: "image", SourceType: "video_render_segment_tail_anchor", SourceID: "segment-1",
			SourceVersion: "artifact-v1", ArtifactID: "artifact-v1", MediaFileID: "media-v1",
			ContentHash: hash("a"), GeneratedAt: "2026-07-21T12:00:00Z",
		},
		{
			Order: 1, ReferenceKey: "character:asset-1", Role: "character_identity", Required: true,
			MediaType: "image", SourceType: "canonical_asset", SourceID: "asset-1",
			SourceVersion: "reference-v1", AssetID: "asset-1", ArtifactID: "artifact-character-v1",
			ContentHash: hash("b"),
		},
	}
	manifest, firstHash, err := BuildReferenceManifestV2(string(InputContractFirstFramePlusReferences), hash("c"), items)
	if err != nil {
		t.Fatal(err)
	}
	_, replayHash, err := BuildReferenceManifestV2(string(manifest.ContractKey), manifest.CapabilitySnapshotHash, manifest.Items)
	if err != nil {
		t.Fatal(err)
	}
	if replayHash != firstHash {
		t.Fatalf("same manifest hash = %s, want %s", replayHash, firstHash)
	}
	items[1].SourceVersion = "reference-v2"
	items[1].ArtifactID = "artifact-character-v2"
	items[1].ContentHash = hash("d")
	_, changedHash, err := BuildReferenceManifestV2(string(InputContractFirstFramePlusReferences), hash("c"), items)
	if err != nil {
		t.Fatal(err)
	}
	if changedHash == firstHash {
		t.Fatal("reference version change must change manifest hash")
	}
}

func TestReferenceManifestV2RejectsUnstableRoleOrder(t *testing.T) {
	_, _, err := BuildReferenceManifestV2(string(InputContractFirstFramePlusReferences), hash("a"), []ReferenceManifestItemV2{
		{Order: 0, ReferenceKey: "character", Role: "character_identity", MediaType: "image", SourceType: "canonical_asset", SourceVersion: "v1", ArtifactID: "artifact-1", ContentHash: hash("b")},
		{Order: 1, ReferenceKey: "tail", Role: "first_frame", MediaType: "image", SourceType: "tail_anchor", SourceVersion: "v1", ArtifactID: "artifact-2", ContentHash: hash("c")},
	})
	if err == nil {
		t.Fatal("manifest with first_frame after semantic references must be rejected")
	}
}

func TestReferenceManifestV2RejectsUnstablePriorityOrderWithinRole(t *testing.T) {
	_, _, err := BuildReferenceManifestV2(string(InputContractSemanticReferences), hash("a"), []ReferenceManifestItemV2{
		{Order: 0, ReferenceKey: "scene", Role: "scene_identity", Required: true, Priority: 10, MediaType: "image", SourceType: "canonical_asset", SourceVersion: "v1", ArtifactID: "artifact-1", ContentHash: hash("b")},
		{Order: 1, ReferenceKey: "character", Role: "character_identity", Required: true, Priority: 100, MediaType: "image", SourceType: "canonical_asset", SourceVersion: "v1", ArtifactID: "artifact-2", ContentHash: hash("c")},
	})
	if err == nil {
		t.Fatal("manifest with lower-priority semantic reference first must be rejected")
	}
}

func hash(character string) string {
	result := ""
	for len(result) < 64 {
		result += character
	}
	return result[:64]
}
