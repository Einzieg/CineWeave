package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Einzieg/cineweave/internal/videocontracts"
)

func validateGatewayVideoReferenceManifest(req GatewayVideoCreateTaskRequest, segment videoExecutionSegment) error {
	if req.ReferenceManifest == nil {
		if segment.InputContractKey == VideoInputContractFirstFramePlusReferences {
			return videoReferenceManifestError("多模态续接必须携带 ReferenceManifestV2")
		}
		return nil
	}
	manifestHash, err := videocontracts.ValidateReferenceManifestV2(*req.ReferenceManifest)
	if err != nil {
		return videoReferenceManifestError("视频引用清单无效：" + err.Error())
	}
	if !strings.EqualFold(cleanVideoContractHash(req.ReferenceManifestHash), cleanVideoContractHash(manifestHash)) {
		return videoReferenceManifestError("视频引用清单哈希不一致")
	}
	if !strings.EqualFold(string(req.ReferenceManifest.ContractKey), segment.InputContractKey) {
		return videoReferenceManifestError("视频引用清单输入契约与 Render Plan 不一致")
	}
	if !strings.EqualFold(cleanVideoContractHash(req.ReferenceManifest.CapabilitySnapshotHash), cleanVideoContractHash(segment.CapabilitySnapshotHash)) {
		return videoReferenceManifestError("视频引用清单能力快照与 Render Plan 不一致")
	}
	if len(req.ReferenceManifest.Items) != len(req.References) {
		return videoReferenceManifestError("视频引用清单与实际引用数量不一致")
	}
	for index, item := range req.ReferenceManifest.Items {
		reference := req.References[index]
		checks := []struct {
			name     string
			actual   string
			expected string
		}{
			{"referenceKey", reference.ReferenceKey, item.ReferenceKey},
			{"role", gatewayVideoReferenceRole(reference), item.Role},
			{"mediaType", gatewayVideoReferenceMediaType(reference), item.MediaType},
			{"semantics", reference.Semantics, item.Semantics},
			{"sourceType", reference.SourceType, item.SourceType},
			{"sourceId", reference.SourceID, item.SourceID},
			{"sourceVersion", reference.SourceVersion, item.SourceVersion},
			{"assetId", reference.AssetID, item.AssetID},
			{"artifactId", reference.ArtifactID, item.ArtifactID},
			{"mediaFileId", reference.MediaFileID, item.MediaFileID},
			{"contentHash", cleanVideoContractHash(reference.ContentHash), cleanVideoContractHash(item.ContentHash)},
			{"generatedAt", reference.GeneratedAt, item.GeneratedAt},
		}
		for _, check := range checks {
			if !strings.EqualFold(strings.TrimSpace(check.actual), strings.TrimSpace(check.expected)) {
				return videoReferenceManifestError(fmt.Sprintf("视频引用清单第 %d 项的 %s 与实际引用不一致", index+1, check.name))
			}
		}
		if reference.Required != item.Required {
			return videoReferenceManifestError(fmt.Sprintf("视频引用清单第 %d 项的 required 与实际引用不一致", index+1))
		}
		if reference.Priority != item.Priority {
			return videoReferenceManifestError(fmt.Sprintf("视频引用清单第 %d 项的 priority 与实际引用不一致", index+1))
		}
	}
	return nil
}

func (s *Service) validateGatewayVideoReferenceManifestSources(ctx context.Context, req GatewayVideoCreateTaskRequest, segment videoExecutionSegment) error {
	if req.ReferenceManifest == nil {
		return nil
	}
	type expectedReference struct {
		ReferenceKey string
		Role         string
		Required     bool
		Priority     int
	}
	expected := make([]expectedReference, 0, len(req.ReferenceManifest.Items))
	rows, err := s.db.Query(ctx, `
		SELECT reference_key, role, required, priority
		FROM shot_reference_pack_items
		WHERE reference_pack_id = $1
	`, segment.ReferencePackID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var reference expectedReference
		if err := rows.Scan(&reference.ReferenceKey, &reference.Role, &reference.Required, &reference.Priority); err != nil {
			rows.Close()
			return err
		}
		if segment.SegmentIndex > 0 && videocontracts.RequiresPreviousTailFrame(segment.InputContractKey) && (reference.Role == "first_frame" || reference.Role == "last_frame") {
			continue
		}
		if segment.SegmentIndex > 0 && videocontracts.RequiresPreviousVideo(segment.InputContractKey) {
			continue
		}
		expected = append(expected, reference)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	sort.SliceStable(expected, func(left, right int) bool {
		return videocontracts.CanonicalReferenceOrderLess(segment.InputContractKey,
			videocontracts.ReferenceOrderKey{
				Role: expected[left].Role, Required: expected[left].Required,
				Priority: expected[left].Priority, ReferenceKey: expected[left].ReferenceKey,
			},
			videocontracts.ReferenceOrderKey{
				Role: expected[right].Role, Required: expected[right].Required,
				Priority: expected[right].Priority, ReferenceKey: expected[right].ReferenceKey,
			},
		)
	})
	expectedKeys := make([]string, 0, len(expected))
	for _, reference := range expected {
		expectedKeys = append(expectedKeys, reference.ReferenceKey)
	}
	actualKeys := make([]string, 0, len(req.ReferenceManifest.Items))
	for _, item := range req.ReferenceManifest.Items {
		switch item.SourceType {
		case "video_render_segment_tail_anchor", "video_render_segment":
			continue
		}
		actualKeys = append(actualKeys, item.ReferenceKey)
		var valid bool
		err := s.db.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM shot_reference_pack_items item
				WHERE item.reference_pack_id = $1
				  AND item.reference_key = $2
				  AND item.role = $3
				  AND item.required = $4
				  AND item.priority = $5
				  AND item.source_type = $6
				  AND COALESCE(item.source_id::text, '') = $7
				  AND COALESCE(item.asset_id::text, '') = $8
				  AND COALESCE(item.artifact_id::text, '') = $9
				  AND COALESCE(item.media_file_id::text, '') = $10
				  AND item.media_type = $11
				  AND item.semantics = $12
				  AND lower(item.content_hash) = $13
				  AND COALESCE(
				        item.metadata->>'sourceVersion',
				        item.metadata->>'sourceVersionId',
				        item.content_hash
				      ) = $14
			)
		`, segment.ReferencePackID, item.ReferenceKey, item.Role, item.Required, item.Priority,
			item.SourceType, item.SourceID, item.AssetID, item.ArtifactID, item.MediaFileID,
			item.MediaType, item.Semantics, cleanVideoContractHash(item.ContentHash), item.SourceVersion).Scan(&valid)
		if err != nil {
			return err
		}
		if !valid {
			return videoReferenceManifestError("视频引用清单包含未冻结在 Render Plan reference pack 中的引用：" + item.ReferenceKey)
		}
	}
	if len(actualKeys) != len(expectedKeys) {
		return videoReferenceManifestError("视频引用清单没有完整保留 Render Plan reference pack")
	}
	for index := range expectedKeys {
		if actualKeys[index] != expectedKeys[index] {
			return videoReferenceManifestError("视频引用清单顺序与 Render Plan reference pack 不一致")
		}
	}
	return nil
}

func videoReferenceManifestError(message string) error {
	return &StandardErrorError{Standard: StandardError{
		Code: CodeModelInputContractUnsupported, Message: message, Retryable: false,
	}}
}
