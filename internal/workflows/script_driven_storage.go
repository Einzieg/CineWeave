package workflows

import (
	"context"
	"encoding/json"
	"strings"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/jackc/pgx/v5"
)

func (a Activities) activeScript(ctx context.Context, projectID, scriptID string) (ScriptRecord, error) {
	var script ScriptRecord
	err := a.db.QueryRow(ctx, `
		SELECT
			s.id::text,
			v.id::text,
			COALESCE(v.version, v.version_no),
			COALESCE(v.content, ''),
			COALESCE(v.content_format, 'markdown'),
			s.title
		FROM scripts s
		JOIN LATERAL (
			SELECT sv.*
			FROM script_versions sv
			WHERE sv.script_id = s.id
			  AND (s.current_version_id IS NULL OR sv.id = s.current_version_id)
			ORDER BY CASE WHEN sv.id = s.current_version_id THEN 0 ELSE 1 END,
			         COALESCE(sv.version, sv.version_no) DESC
			LIMIT 1
		) v ON true
		WHERE s.project_id = $1 AND s.id = $2
	`, projectID, scriptID).Scan(&script.ID, &script.VersionID, &script.Version, &script.Content, &script.ContentFormat, &script.Title)
	return script, err
}

func (a Activities) projectProductionSettings(ctx context.Context, projectID string) (ProjectProductionSettings, error) {
	var item ProjectProductionSettings
	err := a.db.QueryRow(ctx, `
		SELECT id::text,
		       COALESCE(project_type, ''),
		       COALESCE(content_type, ''),
		       COALESCE(aspect_ratio, ''),
		       COALESCE(video_ratio, '16:9'),
		       COALESCE(art_style, ''),
		       COALESCE(director_manual, ''),
		       COALESCE(visual_manual, ''),
		       COALESCE(image_model_profile_key, 'image_generation_default'),
		       COALESCE(video_model_profile_key, 'video_generation_default'),
		       COALESCE(script_model_profile_key, 'script_agent_default'),
		       COALESCE(image_quality, 'standard'),
		       COALESCE(production_mode, 'silent_video')
		FROM projects
		WHERE id = $1
	`, projectID).Scan(
		&item.ID,
		&item.ProjectType,
		&item.ContentType,
		&item.AspectRatio,
		&item.VideoRatio,
		&item.ArtStyle,
		&item.DirectorManual,
		&item.VisualManual,
		&item.ImageModelProfileKey,
		&item.VideoModelProfileKey,
		&item.ScriptModelProfileKey,
		&item.ImageQuality,
		&item.ProductionMode,
	)
	if item.AspectRatio == "" {
		item.AspectRatio = item.VideoRatio
	}
	return item, err
}

func (a Activities) listCanonicalAssets(ctx context.Context, projectID string) ([]CanonicalAssetRecord, error) {
	rows, err := a.db.Query(ctx, `
		SELECT id::text, asset_type, name, description, COALESCE(base_prompt, ''),
		       visual_traits, COALESCE(reference_artifact_id::text, ''),
		       COALESCE(reference_media_file_id::text, ''), COALESCE(reference_storage_key, ''),
		       status, COALESCE(manual_override, false), COALESCE(stale_state, 'fresh')
		FROM canonical_assets
		WHERE project_id = $1
		ORDER BY asset_type, name
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CanonicalAssetRecord, 0)
	for rows.Next() {
		item, err := scanCanonicalAssetRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a Activities) canonicalAssetByID(ctx context.Context, projectID, assetID string) (CanonicalAssetRecord, error) {
	return scanCanonicalAssetRecord(a.db.QueryRow(ctx, `
		SELECT id::text, asset_type, name, description, COALESCE(base_prompt, ''),
		       visual_traits, COALESCE(reference_artifact_id::text, ''),
		       COALESCE(reference_media_file_id::text, ''), COALESCE(reference_storage_key, ''),
		       status, COALESCE(manual_override, false), COALESCE(stale_state, 'fresh')
		FROM canonical_assets
		WHERE project_id = $1 AND id = $2
	`, projectID, assetID))
}

func (a Activities) upsertCanonicalAssets(ctx context.Context, input AnalyzeScriptAssetsInput, script ScriptRecord, candidates []ScriptAssetCandidate, rendered promptsvc.RenderedPrompt, providerCallID string) ([]CanonicalAssetRecord, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	items := make([]CanonicalAssetRecord, 0, len(candidates))
	for _, candidate := range candidates {
		item, err := scanCanonicalAssetRecord(tx.QueryRow(ctx, `
			INSERT INTO canonical_assets(
				organization_id, project_id, asset_type, name, description, base_prompt,
				visual_traits, status, source_script_ids, metadata, created_by
			)
			VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, 'prompt_ready', $8, $9, $10)
			ON CONFLICT (project_id, asset_type, name) DO UPDATE SET
				description = CASE WHEN canonical_assets.manual_override THEN canonical_assets.description ELSE EXCLUDED.description END,
				base_prompt = CASE WHEN canonical_assets.manual_override THEN canonical_assets.base_prompt ELSE EXCLUDED.base_prompt END,
				visual_traits = CASE WHEN canonical_assets.manual_override THEN canonical_assets.visual_traits ELSE EXCLUDED.visual_traits END,
				status = CASE
					WHEN canonical_assets.status IN ('image_running', 'image_succeeded') THEN canonical_assets.status
					ELSE 'prompt_ready'
				END,
				stale_state = CASE WHEN canonical_assets.manual_override THEN canonical_assets.stale_state ELSE 'fresh' END,
				metadata = COALESCE(canonical_assets.metadata, '{}'::jsonb) ||
					CASE
						WHEN canonical_assets.manual_override THEN jsonb_build_object('agentLastSuggestion', EXCLUDED.metadata)
						ELSE EXCLUDED.metadata
					END,
				updated_at = now()
			RETURNING id::text, asset_type, name, description, COALESCE(base_prompt, ''),
			          visual_traits, COALESCE(reference_artifact_id::text, ''),
			          COALESCE(reference_media_file_id::text, ''), COALESCE(reference_storage_key, ''),
			          status, COALESCE(manual_override, false), COALESCE(stale_state, 'fresh')
		`, input.OrganizationID, input.ProjectID, candidate.AssetType, candidate.Name, candidate.Description, candidate.BasePrompt,
			jsonOrDefault(candidate.VisualTraits, `{}`), mustJSON([]string{script.ID}), mustJSON(map[string]any{
				"source":            "script_asset_extraction",
				"scriptId":          script.ID,
				"scriptVersionId":   script.VersionID,
				"providerCallId":    providerCallID,
				"promptTemplateKey": rendered.TemplateKey,
				"promptVersionId":   rendered.PromptVersionID,
				"promptHash":        rendered.RenderedHash,
			}), input.CreatedBy))
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO script_asset_links(organization_id, project_id, script_id, asset_id)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT DO NOTHING
		`, input.OrganizationID, input.ProjectID, script.ID, item.ID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO asset_versions(
				organization_id, project_id, asset_id, version, description, base_prompt,
				visual_traits, prompt_version_id, prompt_hash, metadata, created_by
			)
			SELECT $1, $2, $3, COALESCE(MAX(version), 0) + 1, $4, NULLIF($5, ''), $6,
			       NULLIF($7, '')::uuid, NULLIF($8, ''), $9, $10
			FROM asset_versions
			WHERE asset_id = $3
		`, input.OrganizationID, input.ProjectID, item.ID, item.Description, item.BasePrompt,
			jsonOrDefault(item.VisualTraits, `{}`), rendered.PromptVersionID, rendered.RenderedHash,
			mustJSON(map[string]any{"source": "script_asset_extraction", "scriptId": script.ID}), input.CreatedBy); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return items, nil
}

func (a Activities) insertScriptStoryboardArtifactShotsAndRequirements(ctx context.Context, input GenerateStoryboardFromScriptInput, script ScriptRecord, nodeRunID string, put storage.PutResult, gatewayResp provider.GatewayTextResponse, promptHash string, shots []StoryboardShot, requirements []ShotAssetRequirementRecord) (string, []StoryboardShotRecord, []ShotAssetRequirementRecord, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return "", nil, nil, err
	}
	defer tx.Rollback(ctx)
	var artifactID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, prompt_hash, metadata, created_by)
		VALUES ($1, $2, $3, $4, 'storyboard_json', $5, 'application/json', $6, $7, $8, $9)
		RETURNING id
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, nodeRunID, put.StorageKey, put.ContentHash, promptHash, mustJSON(map[string]any{
		"source":          "script_to_storyboard",
		"scriptId":        script.ID,
		"scriptVersionId": script.VersionID,
		"providerCallId":  gatewayResp.ProviderCallID,
		"modelId":         gatewayResp.ModelID,
		"byteSize":        put.ByteSize,
		"shotCount":       len(shots),
	}), input.CreatedBy).Scan(&artifactID); err != nil {
		return "", nil, nil, err
	}
	shotRecords := make([]StoryboardShotRecord, 0, len(shots))
	shotByNo := map[int]StoryboardShotRecord{}
	for shotIndex, shot := range shots {
		var record StoryboardShotRecord
		err := tx.QueryRow(ctx, `
			INSERT INTO storyboard_shots(
				organization_id, project_id, workflow_run_id, storyboard_artifact_id,
				script_id, script_version_id, script_scene_id, storyboard_source,
				shot_index, shot_no, title, duration_seconds,
				visual, camera, motion, mood, image_prompt, video_prompt,
				status, metadata
			)
			VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '')::uuid, 'script_agent', $8, $9, NULLIF($10, ''), $11,
			        NULLIF($12, ''), NULLIF($13, ''), NULLIF($14, ''), NULLIF($15, ''), NULLIF($16, ''), NULLIF($17, ''),
			        'storyboard_ready', $18)
			ON CONFLICT (workflow_run_id, shot_index) DO UPDATE SET
				storyboard_artifact_id = EXCLUDED.storyboard_artifact_id,
				script_id = EXCLUDED.script_id,
				script_version_id = EXCLUDED.script_version_id,
				script_scene_id = EXCLUDED.script_scene_id,
				storyboard_source = EXCLUDED.storyboard_source,
				shot_no = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.shot_no ELSE EXCLUDED.shot_no END,
				title = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.title ELSE EXCLUDED.title END,
				duration_seconds = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.duration_seconds ELSE EXCLUDED.duration_seconds END,
				visual = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.visual ELSE EXCLUDED.visual END,
				camera = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.camera ELSE EXCLUDED.camera END,
				motion = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.motion ELSE EXCLUDED.motion END,
				mood = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.mood ELSE EXCLUDED.mood END,
				image_prompt = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.image_prompt ELSE EXCLUDED.image_prompt END,
				video_prompt = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.video_prompt ELSE EXCLUDED.video_prompt END,
				status = 'storyboard_ready',
				stale_state = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.stale_state ELSE 'fresh' END,
				metadata = COALESCE(storyboard_shots.metadata, '{}'::jsonb) ||
					CASE
						WHEN storyboard_shots.manual_override THEN jsonb_build_object('agentLastSuggestion', EXCLUDED.metadata)
						ELSE EXCLUDED.metadata
					END,
				updated_at = now()
			RETURNING
				id::text,
				COALESCE(workflow_run_id::text, ''),
				COALESCE(script_scene_id::text, ''),
				shot_index,
				COALESCE(shot_no, shot_index + 1),
				COALESCE(title, ''),
				COALESCE(duration_seconds, 0)::float8,
				COALESCE(visual, ''),
				COALESCE(camera, ''),
				COALESCE(motion, ''),
				COALESCE(mood, ''),
				COALESCE(image_prompt, ''),
				COALESCE(video_prompt, ''),
				COALESCE(image_artifact_id::text, ''),
				COALESCE(image_media_file_id::text, ''),
				COALESCE(image_storage_key, ''),
				COALESCE(video_artifact_id::text, ''),
				COALESCE(video_media_file_id::text, ''),
				COALESCE(video_storage_key, ''),
				COALESCE(video_provider_async_task_id::text, ''),
				COALESCE(video_external_task_id, ''),
				status,
				COALESCE(manual_override, false),
				COALESCE(stale_state, 'fresh')
		`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, artifactID, script.ID, script.VersionID, shot.ScriptSceneID,
			shotIndex, shot.ShotNo, shot.Title, shot.Duration, shot.Visual, shot.Camera, shot.Motion, shot.Mood,
			shot.ImagePrompt, shot.VideoPrompt, mustJSON(map[string]any{
				"source":               "script_to_storyboard",
				"storyboardArtifactId": artifactID,
				"scriptSceneId":        shot.ScriptSceneID,
			})).Scan(
			&record.ID,
			&record.WorkflowRunID,
			&record.ScriptSceneID,
			&record.ShotIndex,
			&record.ShotNo,
			&record.Title,
			&record.Duration,
			&record.Visual,
			&record.Camera,
			&record.Motion,
			&record.Mood,
			&record.ImagePrompt,
			&record.VideoPrompt,
			&record.ImageArtifactID,
			&record.ImageMediaFileID,
			&record.ImageStorageKey,
			&record.VideoArtifactID,
			&record.VideoMediaFileID,
			&record.VideoStorageKey,
			&record.VideoProviderAsyncTaskID,
			&record.VideoExternalTaskID,
			&record.Status,
			&record.ManualOverride,
			&record.StaleState,
		)
		if err != nil {
			return "", nil, nil, err
		}
		shotRecords = append(shotRecords, record)
		shotByNo[record.ShotNo] = record
	}
	assets, err := a.listCanonicalAssets(ctx, input.ProjectID)
	if err != nil {
		return "", nil, nil, err
	}
	assetByKey := map[string]CanonicalAssetRecord{}
	for _, asset := range assets {
		assetByKey[assetKey(asset.AssetType, asset.Name)] = asset
	}
	requirementRecords := make([]ShotAssetRequirementRecord, 0)
	for _, req := range requirements {
		shot, ok := shotByNo[req.ShotNo]
		if !ok {
			continue
		}
		asset, ok := assetByKey[assetKey(req.AssetType, req.AssetName)]
		if !ok {
			continue
		}
		req, err = upsertShotAssetRequirementRecord(ctx, tx, input, shot, asset, req)
		if err != nil {
			return "", nil, nil, err
		}
		requirementRecords = append(requirementRecords, req)
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shots.created", "workflow_run", input.WorkflowRunID, mustJSON(map[string]any{
		"workflowRunId":        input.WorkflowRunID,
		"storyboardArtifactId": artifactID,
		"shotCount":            len(shotRecords),
		"requirementCount":     len(requirementRecords),
		"status":               "storyboard_ready",
	})); err != nil {
		return "", nil, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", nil, nil, err
	}
	return artifactID, shotRecords, requirementRecords, nil
}

func upsertShotAssetRequirementRecord(ctx context.Context, tx pgx.Tx, input GenerateStoryboardFromScriptInput, shot StoryboardShotRecord, asset CanonicalAssetRecord, req ShotAssetRequirementRecord) (ShotAssetRequirementRecord, error) {
	metadata := mustJSON(map[string]any{"source": "storyboard_from_script", "assetName": req.AssetName, "assetType": req.AssetType})
	args := []any{shot.ID, asset.ID, req.RequirementType}
	var existingID string
	var manualOverride bool
	err := tx.QueryRow(ctx, `
		SELECT id::text, COALESCE(manual_override, false)
		FROM shot_asset_requirements
		WHERE storyboard_shot_id = $1 AND asset_id = $2 AND requirement_type = $3
		ORDER BY created_at ASC
		LIMIT 1
	`, args...).Scan(&existingID, &manualOverride)
	if err != nil && err != pgx.ErrNoRows {
		return ShotAssetRequirementRecord{}, err
	}
	if err == pgx.ErrNoRows {
		record := req
		if err := tx.QueryRow(ctx, `
			INSERT INTO shot_asset_requirements(
				organization_id, project_id, workflow_run_id, storyboard_shot_id, asset_id,
				requirement_type, role_in_shot, costume, pose, expression, action,
				camera_relation, scene_state, prop_state, prompt, status, stale_state, metadata
			)
			VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''),
			        NULLIF($10, ''), NULLIF($11, ''), NULLIF($12, ''), NULLIF($13, ''), NULLIF($14, ''),
			        NULLIF($15, ''), 'pending', 'fresh', $16)
			RETURNING id::text, status, COALESCE(manual_override, false), COALESCE(stale_state, 'fresh')
		`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, shot.ID, asset.ID,
			req.RequirementType, req.RoleInShot, req.Costume, req.Pose, req.Expression, req.Action,
			req.CameraRelation, req.SceneState, req.PropState, req.Prompt, metadata).Scan(&record.ID, &record.Status, &record.ManualOverride, &record.StaleState); err != nil {
			return ShotAssetRequirementRecord{}, err
		}
		record.StoryboardShotID = shot.ID
		record.AssetID = asset.ID
		record.AssetType = asset.AssetType
		record.AssetName = asset.Name
		return record, nil
	}
	if manualOverride {
		var record ShotAssetRequirementRecord
		if err := tx.QueryRow(ctx, `
			UPDATE shot_asset_requirements
			SET metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('agentLastSuggestion', $2::jsonb),
			    updated_at = now()
			WHERE id = $1
			RETURNING id::text, storyboard_shot_id::text, asset_id::text, requirement_type,
			          COALESCE(role_in_shot, ''), COALESCE(costume, ''), COALESCE(pose, ''),
			          COALESCE(expression, ''), COALESCE(action, ''), COALESCE(camera_relation, ''),
			          COALESCE(scene_state, ''), COALESCE(prop_state, ''), COALESCE(prompt, ''),
			          COALESCE(derived_artifact_id::text, ''), COALESCE(derived_media_file_id::text, ''),
			          COALESCE(derived_storage_key, ''), status, COALESCE(manual_override, false), COALESCE(stale_state, 'fresh')
		`, existingID, metadata).Scan(
			&record.ID,
			&record.StoryboardShotID,
			&record.AssetID,
			&record.RequirementType,
			&record.RoleInShot,
			&record.Costume,
			&record.Pose,
			&record.Expression,
			&record.Action,
			&record.CameraRelation,
			&record.SceneState,
			&record.PropState,
			&record.Prompt,
			&record.DerivedArtifactID,
			&record.DerivedMediaFileID,
			&record.DerivedStorageKey,
			&record.Status,
			&record.ManualOverride,
			&record.StaleState,
		); err != nil {
			return ShotAssetRequirementRecord{}, err
		}
		record.AssetType = asset.AssetType
		record.AssetName = asset.Name
		return record, nil
	}
	record := req
	if err := tx.QueryRow(ctx, `
		UPDATE shot_asset_requirements
		SET workflow_run_id = $2,
		    role_in_shot = NULLIF($3, ''),
		    costume = NULLIF($4, ''),
		    pose = NULLIF($5, ''),
		    expression = NULLIF($6, ''),
		    action = NULLIF($7, ''),
		    camera_relation = NULLIF($8, ''),
		    scene_state = NULLIF($9, ''),
		    prop_state = NULLIF($10, ''),
		    prompt = NULLIF($11, ''),
		    status = 'pending',
		    stale_state = 'fresh',
		    metadata = COALESCE(metadata, '{}'::jsonb) || $12::jsonb,
		    updated_at = now()
		WHERE id = $1
		RETURNING id::text, status, COALESCE(manual_override, false), COALESCE(stale_state, 'fresh')
	`, existingID, input.WorkflowRunID, req.RoleInShot, req.Costume, req.Pose, req.Expression, req.Action,
		req.CameraRelation, req.SceneState, req.PropState, req.Prompt, metadata).Scan(&record.ID, &record.Status, &record.ManualOverride, &record.StaleState); err != nil {
		return ShotAssetRequirementRecord{}, err
	}
	record.StoryboardShotID = shot.ID
	record.AssetID = asset.ID
	record.AssetType = asset.AssetType
	record.AssetName = asset.Name
	return record, nil
}

func (a Activities) shotAssetRequirementByID(ctx context.Context, projectID, requirementID string) (ShotAssetRequirementRecord, error) {
	var item ShotAssetRequirementRecord
	err := a.db.QueryRow(ctx, `
		SELECT r.id::text, r.storyboard_shot_id::text, r.asset_id::text,
		       a.asset_type, a.name, r.requirement_type,
		       COALESCE(r.role_in_shot, ''), COALESCE(r.costume, ''), COALESCE(r.pose, ''),
		       COALESCE(r.expression, ''), COALESCE(r.action, ''), COALESCE(r.camera_relation, ''),
		       COALESCE(r.scene_state, ''), COALESCE(r.prop_state, ''), COALESCE(r.prompt, ''),
		       COALESCE(r.derived_artifact_id::text, ''), COALESCE(r.derived_media_file_id::text, ''),
		       COALESCE(r.derived_storage_key, ''), r.status,
		       COALESCE(r.manual_override, false), COALESCE(r.stale_state, 'fresh')
		FROM shot_asset_requirements r
		JOIN canonical_assets a ON a.id = r.asset_id
		WHERE r.project_id = $1 AND r.id = $2
	`, projectID, requirementID).Scan(
		&item.ID,
		&item.StoryboardShotID,
		&item.AssetID,
		&item.AssetType,
		&item.AssetName,
		&item.RequirementType,
		&item.RoleInShot,
		&item.Costume,
		&item.Pose,
		&item.Expression,
		&item.Action,
		&item.CameraRelation,
		&item.SceneState,
		&item.PropState,
		&item.Prompt,
		&item.DerivedArtifactID,
		&item.DerivedMediaFileID,
		&item.DerivedStorageKey,
		&item.Status,
		&item.ManualOverride,
		&item.StaleState,
	)
	return item, err
}

func (a Activities) storyboardShotByID(ctx context.Context, projectID, shotID string) (StoryboardShotRecord, error) {
	return scanStoryboardShotRecord(a.db.QueryRow(ctx, `
		SELECT
			id::text,
			COALESCE(workflow_run_id::text, ''),
			COALESCE(script_scene_id::text, ''),
			shot_index,
			COALESCE(shot_no, shot_index + 1),
			COALESCE(title, ''),
			COALESCE(duration_seconds, 0)::float8,
			COALESCE(visual, ''),
			COALESCE(camera, ''),
			COALESCE(motion, ''),
			COALESCE(mood, ''),
			COALESCE(image_prompt, ''),
			COALESCE(video_prompt, ''),
			COALESCE(image_artifact_id::text, ''),
			COALESCE(image_media_file_id::text, ''),
			COALESCE(image_storage_key, ''),
			COALESCE(video_artifact_id::text, ''),
			COALESCE(video_media_file_id::text, ''),
			COALESCE(video_storage_key, ''),
			COALESCE(video_provider_async_task_id::text, ''),
			COALESCE(video_external_task_id, ''),
			COALESCE(status, 'pending'),
			COALESCE(manual_override, false),
			COALESCE(stale_state, 'fresh')
		FROM storyboard_shots
		WHERE project_id = $1 AND id = $2
	`, projectID, shotID))
}

type ShotAssetContext struct {
	AssetsSummary       string
	RequirementsSummary string
	ImageReferences     []provider.GatewayImageReference
}

func (a Activities) shotAssetContext(ctx context.Context, projectID, shotID string) (ShotAssetContext, error) {
	rows, err := a.db.Query(ctx, `
		SELECT
			r.id::text,
			a.id::text,
			a.asset_type,
			a.name,
			a.description,
			COALESCE(a.reference_artifact_id::text, ''),
			COALESCE(a.reference_storage_key, ''),
			COALESCE(r.requirement_type, ''),
			COALESCE(r.role_in_shot, ''),
			COALESCE(r.costume, ''),
			COALESCE(r.pose, ''),
			COALESCE(r.expression, ''),
			COALESCE(r.action, ''),
			COALESCE(r.camera_relation, ''),
			COALESCE(r.scene_state, ''),
			COALESCE(r.prop_state, ''),
			COALESCE(r.prompt, ''),
			COALESCE(r.derived_artifact_id::text, ''),
			COALESCE(r.derived_storage_key, '')
		FROM shot_asset_requirements r
		JOIN canonical_assets a ON a.id = r.asset_id
		WHERE r.project_id = $1 AND r.storyboard_shot_id = $2
		ORDER BY a.asset_type, a.name, r.created_at
	`, projectID, shotID)
	if err != nil {
		return ShotAssetContext{}, err
	}
	defer rows.Close()
	assetLines := []string{}
	requirementLines := []string{}
	refs := []provider.GatewayImageReference{}
	for rows.Next() {
		var requirementID, assetID, assetType, name, description, referenceArtifactID, referenceStorageKey string
		var requirementType, role, costume, pose, expression, action, camera, sceneState, propState, prompt string
		var derivedArtifactID, derivedStorageKey string
		if err := rows.Scan(&requirementID, &assetID, &assetType, &name, &description, &referenceArtifactID, &referenceStorageKey, &requirementType, &role, &costume, &pose, &expression, &action, &camera, &sceneState, &propState, &prompt, &derivedArtifactID, &derivedStorageKey); err != nil {
			return ShotAssetContext{}, err
		}
		assetLines = append(assetLines, strings.Join(compactStrings([]string{assetType, name, description}), " | "))
		requirementLines = append(requirementLines, strings.Join(compactStrings([]string{
			name + " (" + requirementType + ")",
			"role=" + role,
			"costume=" + costume,
			"pose=" + pose,
			"expression=" + expression,
			"action=" + action,
			"camera=" + camera,
			"scene=" + sceneState,
			"prop=" + propState,
			"prompt=" + prompt,
		}), "; "))
		refArtifactID := firstNonEmptyString(derivedArtifactID, referenceArtifactID)
		refStorageKey := firstNonEmptyString(derivedStorageKey, referenceStorageKey)
		if refArtifactID != "" || refStorageKey != "" {
			refs = append(refs, provider.GatewayImageReference{
				Type:       "image",
				AssetID:    assetID,
				ArtifactID: refArtifactID,
				StorageKey: refStorageKey,
				Metadata: mustJSON(map[string]any{
					"requirementId": requirementID,
					"assetType":     assetType,
					"assetName":     name,
				}),
			})
		}
	}
	if err := rows.Err(); err != nil {
		return ShotAssetContext{}, err
	}
	return ShotAssetContext{
		AssetsSummary:       strings.Join(assetLines, "\n"),
		RequirementsSummary: strings.Join(requirementLines, "\n"),
		ImageReferences:     refs,
	}, nil
}

func (a Activities) completeCanonicalAssetImage(ctx context.Context, input GenerateCanonicalAssetImageInput, asset CanonicalAssetRecord, rendered promptsvc.RenderedPrompt, output GenerateCanonicalAssetImageOutput) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE canonical_assets
		SET reference_artifact_id = NULLIF($2, '')::uuid,
		    reference_media_file_id = NULLIF($3, '')::uuid,
		    reference_storage_key = NULLIF($4, ''),
		    status = 'image_succeeded',
		    stale_state = 'fresh',
		    updated_at = now()
		WHERE id = $1
	`, input.AssetID, output.ImageArtifactID, output.ImageMediaFileID, output.ImageStorageKey); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO asset_versions(
			organization_id, project_id, asset_id, version, description, base_prompt,
			visual_traits, reference_artifact_id, reference_media_file_id, reference_storage_key,
			prompt_version_id, prompt_hash, metadata, created_by
		)
		SELECT $1, $2, $3, COALESCE(MAX(version), 0) + 1, $4, NULLIF($5, ''), $6,
		       NULLIF($7, '')::uuid, NULLIF($8, '')::uuid, NULLIF($9, ''),
		       NULLIF($10, '')::uuid, NULLIF($11, ''), $12, $13
		FROM asset_versions
		WHERE asset_id = $3
	`, input.OrganizationID, input.ProjectID, input.AssetID, asset.Description, asset.BasePrompt, jsonOrDefault(asset.VisualTraits, `{}`),
		output.ImageArtifactID, output.ImageMediaFileID, output.ImageStorageKey, rendered.PromptVersionID, rendered.RenderedHash,
		mustJSON(map[string]any{"source": "canonical_asset_image_prompt", "providerCallId": output.ProviderCallID}), input.CreatedBy); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) completeDerivedAssetImage(ctx context.Context, input GenerateDerivedAssetImageInput, output GenerateDerivedAssetImageOutput) error {
	_, err := a.db.Exec(ctx, `
		UPDATE shot_asset_requirements
		SET derived_artifact_id = NULLIF($2, '')::uuid,
		    derived_media_file_id = NULLIF($3, '')::uuid,
		    derived_storage_key = NULLIF($4, ''),
		    status = 'image_succeeded',
		    stale_state = 'fresh',
		    updated_at = now()
		WHERE id = $1
	`, input.RequirementID, output.ImageArtifactID, output.ImageMediaFileID, output.ImageStorageKey)
	return err
}

func scanCanonicalAssetRecord(row pgx.Row) (CanonicalAssetRecord, error) {
	var item CanonicalAssetRecord
	var visualTraits []byte
	err := row.Scan(
		&item.ID,
		&item.AssetType,
		&item.Name,
		&item.Description,
		&item.BasePrompt,
		&visualTraits,
		&item.ReferenceArtifactID,
		&item.ReferenceMediaFileID,
		&item.ReferenceStorageKey,
		&item.Status,
		&item.ManualOverride,
		&item.StaleState,
	)
	item.VisualTraits = jsonOrDefault(visualTraits, `{}`)
	return item, err
}

func assetKey(assetType, name string) string {
	return strings.TrimSpace(assetType) + "\x00" + strings.ToLower(strings.TrimSpace(name))
}

func jsonOrDefault(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}

func nodeKeyForID(prefix, id string) string {
	id = strings.ReplaceAll(strings.TrimSpace(id), "-", "")
	if len(id) > 12 {
		id = id[:12]
	}
	if id == "" {
		id = "unknown"
	}
	return prefix + "_" + id
}

func storyboardShotSummary(shot StoryboardShotRecord) string {
	return strings.Join(compactStrings([]string{
		shot.Title,
		shot.Visual,
		shot.Camera,
		shot.Motion,
		shot.Mood,
		shot.ImagePrompt,
	}), "\n")
}

func shotRequirementSummary(req ShotAssetRequirementRecord) string {
	return strings.Join(compactStrings([]string{
		"Asset: " + req.AssetName,
		"Type: " + req.RequirementType,
		"Role: " + req.RoleInShot,
		"Costume: " + req.Costume,
		"Pose: " + req.Pose,
		"Expression: " + req.Expression,
		"Action: " + req.Action,
		"Camera: " + req.CameraRelation,
		"Scene state: " + req.SceneState,
		"Prop state: " + req.PropState,
		"Prompt: " + req.Prompt,
	}), "\n")
}
