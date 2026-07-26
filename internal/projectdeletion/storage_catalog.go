package projectdeletion

import "strings"

// StorageCandidateUnion returns the canonical project-owned storage reference
// catalog. Callers use the same catalog for impact analysis and deletion
// manifests so schema changes cannot silently diverge between the two paths.
func StorageCandidateUnion(projectPlaceholder string) string {
	return strings.ReplaceAll(storageCandidateUnionTemplate, "$PROJECT_ID", sqlPlaceholder(projectPlaceholder))
}

// SharedStorageReferenceQuery reports whether another project still references
// a storage key. Objects with shared references must be unlinked, not deleted.
func SharedStorageReferenceQuery(storageKeyPlaceholder, projectPlaceholder string) string {
	query := strings.ReplaceAll(sharedStorageReferenceQueryTemplate, "$STORAGE_KEY", sqlPlaceholder(storageKeyPlaceholder))
	return strings.ReplaceAll(query, "$PROJECT_ID", sqlPlaceholder(projectPlaceholder))
}

func sqlPlaceholder(value string) string {
	switch value {
	case "$1", "$2":
		return value
	default:
		panic("project deletion SQL placeholder must be $1 or $2")
	}
}

const storageCandidateUnionTemplate = `
	SELECT 'artifact'::text AS source_kind, storage_key, NULL::bigint AS byte_size
	FROM artifacts
	WHERE project_id = $PROJECT_ID AND NULLIF(btrim(storage_key), '') IS NOT NULL
	UNION ALL
	SELECT 'business_reference', storage_key, NULL::bigint
	FROM asset_references
	WHERE project_id = $PROJECT_ID AND NULLIF(btrim(storage_key), '') IS NOT NULL
	UNION ALL
	SELECT 'business_reference', reference_storage_key, NULL::bigint
	FROM asset_versions
	WHERE project_id = $PROJECT_ID AND NULLIF(btrim(reference_storage_key), '') IS NOT NULL
	UNION ALL
	SELECT 'business_reference', clip.storage_key, NULL::bigint
	FROM audio_mix_clips clip
	JOIN audio_mix_versions mix ON mix.id = clip.audio_mix_version_id
	WHERE mix.project_id = $PROJECT_ID AND NULLIF(btrim(clip.storage_key), '') IS NOT NULL
	UNION ALL
	SELECT 'business_reference', storage_key, NULL::bigint
	FROM audio_mix_versions
	WHERE project_id = $PROJECT_ID AND NULLIF(btrim(storage_key), '') IS NOT NULL
	UNION ALL
	SELECT 'business_reference', reference_storage_key, NULL::bigint
	FROM canonical_assets
	WHERE project_id = $PROJECT_ID AND NULLIF(btrim(reference_storage_key), '') IS NOT NULL
	UNION ALL
	SELECT 'business_reference', primary_reference_storage_key, NULL::bigint
	FROM canonical_assets
	WHERE project_id = $PROJECT_ID AND NULLIF(btrim(primary_reference_storage_key), '') IS NOT NULL
	UNION ALL
	SELECT 'business_reference', storage_key, NULL::bigint
	FROM final_video_versions
	WHERE project_id = $PROJECT_ID AND NULLIF(btrim(storage_key), '') IS NOT NULL
	UNION ALL
	SELECT 'media_file', storage_key, byte_size
	FROM media_files
	WHERE project_id = $PROJECT_ID AND NULLIF(btrim(storage_key), '') IS NOT NULL
	UNION ALL
	SELECT 'media_variant', variant.storage_key, NULL::bigint
	FROM media_variants variant
	JOIN media_files media ON media.id = variant.media_file_id
	WHERE media.project_id = $PROJECT_ID AND NULLIF(btrim(variant.storage_key), '') IS NOT NULL
	UNION ALL
	SELECT 'business_reference', storage_key, byte_size
	FROM project_exports
	WHERE project_id = $PROJECT_ID AND NULLIF(btrim(storage_key), '') IS NOT NULL
	UNION ALL
	SELECT 'business_reference', storage_key, NULL::bigint
	FROM project_sources
	WHERE project_id = $PROJECT_ID AND NULLIF(btrim(storage_key), '') IS NOT NULL
	UNION ALL
	SELECT 'business_reference', item.storage_key, NULL::bigint
	FROM shot_reference_pack_items item
	JOIN shot_reference_packs pack ON pack.id = item.reference_pack_id
	WHERE pack.project_id = $PROJECT_ID AND NULLIF(btrim(item.storage_key), '') IS NOT NULL
	UNION ALL
	SELECT 'business_reference', derived_storage_key, NULL::bigint
	FROM shot_asset_requirements
	WHERE project_id = $PROJECT_ID AND NULLIF(btrim(derived_storage_key), '') IS NOT NULL
	UNION ALL
	SELECT 'business_reference', storage_key, NULL::bigint
	FROM shot_visual_anchors
	WHERE project_id = $PROJECT_ID AND NULLIF(btrim(storage_key), '') IS NOT NULL
	UNION ALL
	SELECT 'business_reference', image_storage_key, NULL::bigint
	FROM storyboard_shots
	WHERE project_id = $PROJECT_ID AND NULLIF(btrim(image_storage_key), '') IS NOT NULL
	UNION ALL
	SELECT 'business_reference', video_storage_key, NULL::bigint
	FROM storyboard_shots
	WHERE project_id = $PROJECT_ID AND NULLIF(btrim(video_storage_key), '') IS NOT NULL
	UNION ALL
	SELECT 'business_reference', source_storage_key, NULL::bigint
	FROM timeline_clips
	WHERE project_id = $PROJECT_ID AND NULLIF(btrim(source_storage_key), '') IS NOT NULL
	UNION ALL
	SELECT 'business_reference', storage_key, byte_size
	FROM tts_audio_clips
	WHERE project_id = $PROJECT_ID AND NULLIF(btrim(storage_key), '') IS NOT NULL
	UNION ALL
	SELECT 'business_reference', output_storage_key, NULL::bigint
	FROM video_render_plans
	WHERE project_id = $PROJECT_ID AND NULLIF(btrim(output_storage_key), '') IS NOT NULL
	UNION ALL
	SELECT 'business_reference', storage_key, NULL::bigint
	FROM video_render_segments
	WHERE project_id = $PROJECT_ID AND NULLIF(btrim(storage_key), '') IS NOT NULL
`

const sharedStorageReferenceQueryTemplate = `
	SELECT EXISTS(
		SELECT 1 FROM artifacts
		WHERE storage_key = $STORAGE_KEY AND project_id IS DISTINCT FROM $PROJECT_ID::uuid
		UNION ALL
		SELECT 1 FROM asset_references
		WHERE storage_key = $STORAGE_KEY AND project_id <> $PROJECT_ID
		UNION ALL
		SELECT 1 FROM asset_versions
		WHERE reference_storage_key = $STORAGE_KEY AND project_id <> $PROJECT_ID
		UNION ALL
		SELECT 1
		FROM audio_mix_clips clip
		JOIN audio_mix_versions mix ON mix.id = clip.audio_mix_version_id
		WHERE clip.storage_key = $STORAGE_KEY AND mix.project_id <> $PROJECT_ID
		UNION ALL
		SELECT 1 FROM audio_mix_versions
		WHERE storage_key = $STORAGE_KEY AND project_id <> $PROJECT_ID
		UNION ALL
		SELECT 1 FROM canonical_assets
		WHERE (reference_storage_key = $STORAGE_KEY OR primary_reference_storage_key = $STORAGE_KEY)
		  AND project_id <> $PROJECT_ID
		UNION ALL
		SELECT 1 FROM final_video_versions
		WHERE storage_key = $STORAGE_KEY AND project_id <> $PROJECT_ID
		UNION ALL
		SELECT 1 FROM media_files
		WHERE storage_key = $STORAGE_KEY AND project_id IS DISTINCT FROM $PROJECT_ID::uuid
		UNION ALL
		SELECT 1
		FROM media_variants variant
		JOIN media_files media ON media.id = variant.media_file_id
		WHERE variant.storage_key = $STORAGE_KEY AND media.project_id IS DISTINCT FROM $PROJECT_ID::uuid
		UNION ALL
		SELECT 1 FROM project_exports
		WHERE storage_key = $STORAGE_KEY AND project_id <> $PROJECT_ID
		UNION ALL
		SELECT 1 FROM project_sources
		WHERE storage_key = $STORAGE_KEY AND project_id <> $PROJECT_ID
		UNION ALL
		SELECT 1
		FROM shot_reference_pack_items item
		JOIN shot_reference_packs pack ON pack.id = item.reference_pack_id
		WHERE item.storage_key = $STORAGE_KEY AND pack.project_id <> $PROJECT_ID
		UNION ALL
		SELECT 1 FROM shot_asset_requirements
		WHERE derived_storage_key = $STORAGE_KEY AND project_id <> $PROJECT_ID
		UNION ALL
		SELECT 1 FROM shot_visual_anchors
		WHERE storage_key = $STORAGE_KEY AND project_id <> $PROJECT_ID
		UNION ALL
		SELECT 1 FROM storyboard_shots
		WHERE (image_storage_key = $STORAGE_KEY OR video_storage_key = $STORAGE_KEY)
		  AND project_id <> $PROJECT_ID
		UNION ALL
		SELECT 1 FROM timeline_clips
		WHERE source_storage_key = $STORAGE_KEY AND project_id <> $PROJECT_ID
		UNION ALL
		SELECT 1 FROM tts_audio_clips
		WHERE storage_key = $STORAGE_KEY AND project_id <> $PROJECT_ID
		UNION ALL
		SELECT 1 FROM video_render_plans
		WHERE output_storage_key = $STORAGE_KEY AND project_id <> $PROJECT_ID
		UNION ALL
		SELECT 1 FROM video_render_segments
		WHERE storage_key = $STORAGE_KEY AND project_id <> $PROJECT_ID
	)
`
