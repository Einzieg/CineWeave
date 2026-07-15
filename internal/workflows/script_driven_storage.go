package workflows

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"

	"github.com/Einzieg/cineweave/internal/assetprompts"
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
			  AND COALESCE(sv.status, 'active') <> 'archived'
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
		       COALESCE(tts_model_profile_key, 'tts_generation_default'),
		       COALESCE(asr_model_profile_key, 'audio_transcription_default'),
		       COALESCE(audio_strategy, 'native_av'),
		       COALESCE(audio_requirement, 'preferred'),
		       COALESCE(image_quality, 'standard'),
		       COALESCE(production_mode, 'silent_video'),
		       timeline_timebase,
		       fps_numerator,
		       fps_denominator
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
		&item.TTSModelProfileKey,
		&item.ASRModelProfileKey,
		&item.AudioStrategy,
		&item.AudioRequirement,
		&item.ImageQuality,
		&item.ProductionMode,
		&item.TimelineTimebase,
		&item.FPSNumerator,
		&item.FPSDenominator,
	)
	if item.AspectRatio == "" {
		item.AspectRatio = item.VideoRatio
	}
	return item, err
}

func (a Activities) listCanonicalAssets(ctx context.Context, projectID string) ([]CanonicalAssetRecord, error) {
	rows, err := a.db.Query(ctx, `
		SELECT id::text, asset_type, name, description, COALESCE(base_prompt, ''),
		       profile, COALESCE(consistency_prompt, ''), COALESCE(negative_prompt, ''),
		       visual_traits, COALESCE(primary_reference_artifact_id::text, ''),
		       COALESCE(primary_reference_media_file_id::text, ''), COALESCE(primary_reference_storage_key, ''),
		       COALESCE(lock_reference, false), COALESCE(reference_artifact_id::text, ''),
		       COALESCE(reference_media_file_id::text, ''), COALESCE(reference_storage_key, ''),
		       status, COALESCE(manual_override, false), COALESCE(stale_state, 'fresh'), revision, prompt_revision
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
		       profile, COALESCE(consistency_prompt, ''), COALESCE(negative_prompt, ''),
		       visual_traits, COALESCE(primary_reference_artifact_id::text, ''),
		       COALESCE(primary_reference_media_file_id::text, ''), COALESCE(primary_reference_storage_key, ''),
		       COALESCE(lock_reference, false), COALESCE(reference_artifact_id::text, ''),
		       COALESCE(reference_media_file_id::text, ''), COALESCE(reference_storage_key, ''),
		       status, COALESCE(manual_override, false), COALESCE(stale_state, 'fresh'), revision, prompt_revision
		FROM canonical_assets
		WHERE project_id = $1 AND id = $2
	`, projectID, assetID))
}

func (a Activities) upsertCanonicalAssets(ctx context.Context, input AnalyzeScriptAssetsInput, script ScriptRecord, execution NodeExecution, scenes []ScriptSceneRecord, candidates []ScriptAssetCandidate, rendered promptsvc.RenderedPrompt, gatewayResp provider.GatewayTextResponse) (ScriptAssetsOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ScriptAssetsOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return ScriptAssetsOutput{}, err
	}
	items := make([]CanonicalAssetRecord, 0, len(candidates))
	for _, candidate := range candidates {
		item, err := scanCanonicalAssetRecord(tx.QueryRow(ctx, `
			INSERT INTO canonical_assets(
				organization_id, project_id, asset_type, name, description, base_prompt,
				visual_traits, status, source_script_ids, metadata, created_by
			)
			VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, 'draft', $8, $9, $10)
			ON CONFLICT (project_id, asset_type, name) DO UPDATE SET
				description = CASE WHEN canonical_assets.manual_override THEN canonical_assets.description ELSE EXCLUDED.description END,
				base_prompt = CASE WHEN canonical_assets.manual_override THEN canonical_assets.base_prompt ELSE EXCLUDED.base_prompt END,
				visual_traits = CASE WHEN canonical_assets.manual_override THEN canonical_assets.visual_traits ELSE EXCLUDED.visual_traits END,
				status = CASE
					WHEN canonical_assets.status IN ('image_running', 'image_succeeded') THEN canonical_assets.status
					WHEN canonical_assets.status = 'prompt_ready' THEN canonical_assets.status
					ELSE 'draft'
				END,
				stale_state = CASE WHEN canonical_assets.manual_override THEN canonical_assets.stale_state ELSE 'fresh' END,
				metadata = COALESCE(canonical_assets.metadata, '{}'::jsonb) ||
					CASE
						WHEN canonical_assets.manual_override THEN jsonb_build_object('agentLastSuggestion', EXCLUDED.metadata)
						ELSE EXCLUDED.metadata
					END,
				revision = canonical_assets.revision + CASE WHEN canonical_assets.manual_override THEN 0 ELSE 1 END,
				prompt_revision = canonical_assets.prompt_revision + CASE WHEN canonical_assets.manual_override THEN 0 ELSE 1 END,
				updated_at = now()
			RETURNING id::text, asset_type, name, description, COALESCE(base_prompt, ''),
			          profile, COALESCE(consistency_prompt, ''), COALESCE(negative_prompt, ''),
			          visual_traits, COALESCE(primary_reference_artifact_id::text, ''),
			          COALESCE(primary_reference_media_file_id::text, ''), COALESCE(primary_reference_storage_key, ''),
			          COALESCE(lock_reference, false), COALESCE(reference_artifact_id::text, ''),
			          COALESCE(reference_media_file_id::text, ''), COALESCE(reference_storage_key, ''),
			          status, COALESCE(manual_override, false), COALESCE(stale_state, 'fresh'), revision, prompt_revision
		`, input.OrganizationID, input.ProjectID, candidate.AssetType, candidate.Name, candidate.Description, candidate.BasePrompt,
			jsonOrDefault(candidate.VisualTraits, `{}`), mustJSON([]string{script.ID}), mustJSON(map[string]any{
				"source":            "script_asset_extraction",
				"scriptId":          script.ID,
				"scriptVersionId":   script.VersionID,
				"providerCallId":    gatewayResp.ProviderCallID,
				"promptTemplateKey": rendered.TemplateKey,
				"promptVersionId":   rendered.PromptVersionID,
				"promptHash":        rendered.RenderedHash,
			}), input.CreatedBy))
		if err != nil {
			return ScriptAssetsOutput{}, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO script_asset_links(organization_id, project_id, script_id, asset_id)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT DO NOTHING
		`, input.OrganizationID, input.ProjectID, script.ID, item.ID); err != nil {
			return ScriptAssetsOutput{}, err
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
			return ScriptAssetsOutput{}, err
		}
		items = append(items, item)
	}
	if err := upsertSceneAssetLinksTx(ctx, tx, input, scenes, items); err != nil {
		return ScriptAssetsOutput{}, err
	}
	output := ScriptAssetsOutput{
		ScriptID:        script.ID,
		ScriptVersionID: script.VersionID,
		Assets:          items,
		ProviderCallID:  gatewayResp.ProviderCallID,
		ModelID:         gatewayResp.ModelID,
	}
	if _, err := completeNodeRunTx(ctx, tx, execution, mustJSON(output)); err != nil {
		return ScriptAssetsOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ScriptAssetsOutput{}, err
	}
	return output, nil
}

func (a Activities) insertScriptStoryboardArtifactShotsAndRequirements(ctx context.Context, input GenerateStoryboardFromScriptInput, script ScriptRecord, project ProjectProductionSettings, execution NodeExecution, put storage.PutResult, gatewayResp provider.GatewayTextResponse, promptHash string, shots []StoryboardShot, requirements []ShotAssetRequirementRecord, storyboard json.RawMessage, parseError string, durationMetrics *StoryboardDurationMetrics) (string, []StoryboardShotRecord, []ShotAssetRequirementRecord, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return "", nil, nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return "", nil, nil, err
	}
	nodeRunID := execution.NodeRunID
	var artifactID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, prompt_hash, metadata, created_by)
		VALUES ($1, $2, $3, $4, 'storyboard_json', $5, 'application/json', $6, $7, $8, $9)
		RETURNING id
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, nodeRunID, put.StorageKey, put.ContentHash, promptHash, mustJSON(map[string]any{
		"source":          "script_to_storyboard",
		"scriptId":        script.ID,
		"scriptVersionId": script.VersionID,
		"scriptEpisodeId": input.ScriptEpisodeID,
		"episodeIndex":    input.EpisodeIndex,
		"episodeTitle":    input.EpisodeTitle,
		"providerCallId":  gatewayResp.ProviderCallID,
		"modelId":         gatewayResp.ModelID,
		"byteSize":        put.ByteSize,
		"shotCount":       len(shots),
		"durationMetrics": durationMetrics,
	}), input.CreatedBy).Scan(&artifactID); err != nil {
		return "", nil, nil, err
	}
	if input.ScriptEpisodeID != "" {
		tag, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET deleted_at = now(),
			    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
			      'supersededAt', now(),
			      'supersededByWorkflowRunId', $4
			    ),
			    updated_at = now()
			WHERE project_id = $1
			  AND script_version_id = $2
			  AND script_episode_id = $3
			  AND workflow_run_id IS DISTINCT FROM $4::uuid
			  AND deleted_at IS NULL
		`, input.ProjectID, script.VersionID, input.ScriptEpisodeID, input.WorkflowRunID)
		if err != nil {
			return "", nil, nil, err
		}
		if tag.RowsAffected() > 0 {
			if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.episode.superseded", "script_episode", input.ScriptEpisodeID, mustJSON(map[string]any{
				"scriptEpisodeId":           input.ScriptEpisodeID,
				"workflowRunId":             input.WorkflowRunID,
				"supersededStoryboardShots": tag.RowsAffected(),
			})); err != nil {
				return "", nil, nil, err
			}
		}
	}
	shotRecords := make([]StoryboardShotRecord, 0, len(shots))
	shotByNo := map[int]StoryboardShotRecord{}
	for episodeShotIndex, shot := range shots {
		shotIndex := episodeShotIndex
		if input.EpisodeIndex > 0 {
			shotIndex = (input.EpisodeIndex-1)*1000 + episodeShotIndex
		}
		var record StoryboardShotRecord
		err := tx.QueryRow(ctx, `
			INSERT INTO storyboard_shots(
				organization_id, project_id, workflow_run_id, storyboard_artifact_id,
				script_id, script_version_id, script_scene_id, script_episode_id, storyboard_source,
				episode_index, episode_shot_index, shot_index, shot_no, title,
				start_tick, end_tick, duration_min_ticks, duration_max_ticks, duration_source, timing_confidence,
				visual, camera, motion, mood, image_prompt, video_prompt, script_dialogue,
				status, metadata
			)
			VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '')::uuid, NULLIF($8, '')::uuid, 'script_agent',
			        NULLIF($9, 0), CASE WHEN NULLIF($8, '') IS NULL THEN NULL ELSE $10::integer END, $11, $12, NULLIF($13, ''),
			        $14, $15, $16, $17, $18, $19,
			        NULLIF($20, ''), NULLIF($21, ''), NULLIF($22, ''), NULLIF($23, ''), NULLIF($24, ''), NULLIF($25, ''), $26,
			        'storyboard_ready', $27)
			ON CONFLICT (workflow_run_id, shot_index)
				WHERE workflow_run_id IS NOT NULL AND deleted_at IS NULL
			DO UPDATE SET
				storyboard_artifact_id = EXCLUDED.storyboard_artifact_id,
				script_id = EXCLUDED.script_id,
				script_version_id = EXCLUDED.script_version_id,
				script_scene_id = EXCLUDED.script_scene_id,
				script_episode_id = EXCLUDED.script_episode_id,
				episode_index = EXCLUDED.episode_index,
				episode_shot_index = EXCLUDED.episode_shot_index,
				storyboard_source = EXCLUDED.storyboard_source,
				shot_no = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.shot_no ELSE EXCLUDED.shot_no END,
				title = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.title ELSE EXCLUDED.title END,
				start_tick = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.start_tick ELSE EXCLUDED.start_tick END,
				end_tick = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.end_tick ELSE EXCLUDED.end_tick END,
				duration_min_ticks = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.duration_min_ticks ELSE EXCLUDED.duration_min_ticks END,
				duration_max_ticks = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.duration_max_ticks ELSE EXCLUDED.duration_max_ticks END,
				duration_source = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.duration_source ELSE EXCLUDED.duration_source END,
				timing_confidence = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.timing_confidence ELSE EXCLUDED.timing_confidence END,
				visual = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.visual ELSE EXCLUDED.visual END,
				camera = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.camera ELSE EXCLUDED.camera END,
				motion = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.motion ELSE EXCLUDED.motion END,
				mood = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.mood ELSE EXCLUDED.mood END,
				image_prompt = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.image_prompt ELSE EXCLUDED.image_prompt END,
				image_prompt_status = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.image_prompt_status ELSE 'not_started' END,
				image_prompt_error_code = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.image_prompt_error_code ELSE NULL END,
				image_prompt_error_message = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.image_prompt_error_message ELSE NULL END,
				image_prompt_workflow_run_id = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.image_prompt_workflow_run_id ELSE NULL END,
				image_prompt_updated_at = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.image_prompt_updated_at ELSE now() END,
				video_prompt = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.video_prompt ELSE EXCLUDED.video_prompt END,
				script_dialogue = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.script_dialogue ELSE EXCLUDED.script_dialogue END,
				video_prompt_status = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.video_prompt_status ELSE 'not_started' END,
				video_prompt_error_code = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.video_prompt_error_code ELSE NULL END,
				video_prompt_error_message = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.video_prompt_error_message ELSE NULL END,
				video_prompt_workflow_run_id = CASE WHEN storyboard_shots.manual_override THEN storyboard_shots.video_prompt_workflow_run_id ELSE NULL END,
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
				COALESCE(script_episode_id::text, ''),
				COALESCE(episode_index, 0),
				COALESCE(episode_shot_index, shot_index),
				shot_index,
				COALESCE(shot_no, shot_index + 1),
				COALESCE(title, ''),
				start_tick,
				end_tick,
				planned_duration_ticks,
				planned_duration_ticks::float8 / $28::bigint,
				$28::bigint,
				$29::integer,
				$30::integer,
				duration_source,
				COALESCE(timing_confidence, 0)::float8,
				COALESCE(duration_locked, false),
				COALESCE(one_take, false),
				COALESCE(timing_revision, 1),
				COALESCE(visual, ''),
				COALESCE(camera, ''),
				COALESCE(motion, ''),
				COALESCE(mood, ''),
				COALESCE(image_prompt, ''),
				COALESCE(image_prompt_status, 'not_started'),
				COALESCE(image_prompt_error_code, ''),
				COALESCE(image_prompt_error_message, ''),
				COALESCE(image_prompt_workflow_run_id::text, ''),
				COALESCE(video_prompt, ''),
				COALESCE(script_dialogue, '[]'::jsonb),
				COALESCE(video_prompt_status, 'not_started'),
				COALESCE(video_prompt_error_code, ''),
				COALESCE(video_prompt_error_message, ''),
				COALESCE(video_prompt_workflow_run_id::text, ''),
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
		`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, artifactID, script.ID, script.VersionID, shot.ScriptSceneID, input.ScriptEpisodeID,
			input.EpisodeIndex, episodeShotIndex, shotIndex, shot.ShotNo, shot.Title,
			shot.StartTick, shot.EndTick, shot.DurationTicks, shot.DurationTicks, firstNonEmptyString(shot.DurationSource, "agent_estimated"), 0.5,
			shot.Visual, shot.Camera, shot.Motion, shot.Mood, shot.ImagePrompt, shot.VideoPrompt, mustJSON(shot.Dialogue), mustJSON(map[string]any{
				"source":               "script_to_storyboard",
				"storyboardArtifactId": artifactID,
				"scriptSceneId":        shot.ScriptSceneID,
				"scriptEpisodeId":      input.ScriptEpisodeID,
				"episodeIndex":         input.EpisodeIndex,
				"episodeShotIndex":     episodeShotIndex,
				"startTick":            shot.StartTick,
				"endTick":              shot.EndTick,
				"plannedDurationTicks": shot.DurationTicks,
				"timelineTimebase":     project.TimelineTimebase,
			}), project.TimelineTimebase, project.FPSNumerator, project.FPSDenominator).Scan(
			&record.ID,
			&record.WorkflowRunID,
			&record.ScriptSceneID,
			&record.ScriptEpisodeID,
			&record.EpisodeIndex,
			&record.EpisodeShotIndex,
			&record.ShotIndex,
			&record.ShotNo,
			&record.Title,
			&record.StartTick,
			&record.EndTick,
			&record.PlannedDurationTicks,
			&record.Duration,
			&record.TimelineTimebase,
			&record.FPSNumerator,
			&record.FPSDenominator,
			&record.DurationSource,
			&record.TimingConfidence,
			&record.DurationLocked,
			&record.OneTake,
			&record.TimingRevision,
			&record.Visual,
			&record.Camera,
			&record.Motion,
			&record.Mood,
			&record.ImagePrompt,
			&record.ImagePromptStatus,
			&record.ImagePromptErrorCode,
			&record.ImagePromptErrorMessage,
			&record.ImagePromptWorkflowRunID,
			&record.VideoPrompt,
			&record.Dialogue,
			&record.VideoPromptStatus,
			&record.VideoPromptErrorCode,
			&record.VideoPromptErrorMessage,
			&record.VideoPromptWorkflowRunID,
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
	if durationMetrics != nil {
		durationMetrics.recordStored(shotRecords)
		if _, err := tx.Exec(ctx, `
			UPDATE artifacts
			SET metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('durationMetrics', $2::jsonb)
			WHERE id = $1
		`, artifactID, mustJSON(durationMetrics)); err != nil {
			return "", nil, nil, err
		}
	}
	assets, err := a.listCanonicalAssets(ctx, input.ProjectID)
	if err != nil {
		return "", nil, nil, err
	}
	assetByKey := map[string]CanonicalAssetRecord{}
	assetByID := map[string]CanonicalAssetRecord{}
	assetByName := map[string][]CanonicalAssetRecord{}
	for _, asset := range assets {
		assetByKey[assetKey(asset.AssetType, asset.Name)] = asset
		assetByID[asset.ID] = asset
		nameKey := strings.ToLower(strings.TrimSpace(asset.Name))
		assetByName[nameKey] = append(assetByName[nameKey], asset)
	}
	requirementRecords := make([]ShotAssetRequirementRecord, 0)
	for _, req := range requirements {
		shot, ok := shotByNo[req.ShotNo]
		if !ok {
			continue
		}
		asset, ok := assetByID[req.AssetID]
		if !ok {
			asset, ok = assetByKey[assetKey(req.AssetType, req.AssetName)]
		}
		if !ok {
			matches := assetByName[strings.ToLower(strings.TrimSpace(req.AssetName))]
			if len(matches) == 1 {
				asset, ok = matches[0], true
			}
		}
		if !ok {
			continue
		}
		req.AssetID = asset.ID
		req.AssetName = asset.Name
		req.AssetType = asset.AssetType
		if req.RequirementType == "" || req.RequirementType == "shot_context" {
			req.RequirementType = defaultRequirementType(asset.AssetType)
		}
		req, err = upsertShotAssetRequirementRecord(ctx, tx, input, shot, asset, req)
		if err != nil {
			return "", nil, nil, err
		}
		requirementRecords = append(requirementRecords, req)
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shots.created", "workflow_run", input.WorkflowRunID, mustJSON(map[string]any{
		"workflowRunId":        input.WorkflowRunID,
		"scriptEpisodeId":      input.ScriptEpisodeID,
		"episodeIndex":         input.EpisodeIndex,
		"episodeTotal":         input.EpisodeTotal,
		"episodeTitle":         input.EpisodeTitle,
		"storyboardArtifactId": artifactID,
		"shotCount":            len(shotRecords),
		"requirementCount":     len(requirementRecords),
		"durationMetrics":      durationMetrics,
		"status":               "storyboard_ready",
	})); err != nil {
		return "", nil, nil, err
	}
	nodeOutput := ScriptStoryboardOutput{
		ScriptID:             script.ID,
		ScriptVersionID:      script.VersionID,
		ScriptEpisodeID:      input.ScriptEpisodeID,
		EpisodeIndex:         input.EpisodeIndex,
		EpisodeTotal:         input.EpisodeTotal,
		EpisodeTitle:         input.EpisodeTitle,
		StoryboardArtifactID: artifactID,
		StorageKey:           put.StorageKey,
		ProviderCallID:       gatewayResp.ProviderCallID,
		ModelID:              gatewayResp.ModelID,
		Storyboard:           storyboard,
		Shots:                shotRecords,
		Requirements:         requirementRecords,
		RawText:              gatewayResp.Output.Text,
		ParseError:           parseError,
	}
	if durationMetrics != nil {
		nodeOutput.DurationMetrics = *durationMetrics
	}
	if _, err := completeNodeRunTx(ctx, tx, execution, mustJSON(nodeOutput)); err != nil {
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
			s.id::text,
			COALESCE(s.workflow_run_id::text, ''),
			COALESCE(s.script_scene_id::text, ''),
			COALESCE(s.script_episode_id::text, ''),
			COALESCE(s.episode_index, 0),
			COALESCE(s.episode_shot_index, s.shot_index),
			s.shot_index,
			COALESCE(s.shot_no, s.shot_index + 1),
			COALESCE(s.title, ''),
			COALESCE(s.storyboard_plan_id::text, ''),
			s.start_tick,
			s.end_tick,
			s.planned_duration_ticks,
			s.planned_duration_ticks::float8 / p.timeline_timebase,
			p.timeline_timebase,
			p.fps_numerator,
			p.fps_denominator,
			s.duration_source,
			COALESCE(s.timing_confidence, 0)::float8,
			COALESCE(s.duration_locked, false),
			COALESCE(s.one_take, false),
			COALESCE(s.timing_revision, 1),
			COALESCE(s.visual, ''),
			COALESCE(s.camera, ''),
			COALESCE(s.motion, ''),
			COALESCE(s.mood, ''),
			COALESCE(s.image_prompt, ''),
			COALESCE(s.image_prompt_status, 'not_started'),
			COALESCE(s.image_prompt_error_code, ''),
			COALESCE(s.image_prompt_error_message, ''),
			COALESCE(s.image_prompt_workflow_run_id::text, ''),
			COALESCE(s.video_prompt, ''),
			COALESCE(s.script_dialogue, '[]'::jsonb),
			COALESCE(s.video_prompt_status, 'not_started'),
			COALESCE(s.video_prompt_error_code, ''),
			COALESCE(s.video_prompt_error_message, ''),
			COALESCE(s.video_prompt_workflow_run_id::text, ''),
			COALESCE(s.video_reference_mode, 'auto'),
			COALESCE(s.video_reference_keys, ARRAY[]::text[]),
			COALESCE(s.image_artifact_id::text, ''),
			COALESCE(s.image_media_file_id::text, ''),
			COALESCE(s.image_storage_key, ''),
			COALESCE(s.video_artifact_id::text, ''),
			COALESCE(s.video_media_file_id::text, ''),
			COALESCE(s.video_storage_key, ''),
			COALESCE(s.video_provider_async_task_id::text, ''),
			COALESCE(s.video_external_task_id, ''),
			COALESCE(s.status, 'pending'),
			COALESCE(s.manual_override, false),
			COALESCE(s.stale_state, 'fresh')
		FROM storyboard_shots s
		JOIN projects p ON p.id = s.project_id
		WHERE s.project_id = $1 AND s.id = $2 AND s.deleted_at IS NULL
	`, projectID, shotID))
}

type ShotAssetContext struct {
	AssetsSummary         string
	RequirementsSummary   string
	PromptAssets          []ShotVideoPromptAsset
	ImageReferences       []provider.GatewayImageReference
	AutoImageReferences   []provider.GatewayImageReference
	ImageReferenceMode    string
	ImageReferenceKeys    []string
	AutoReferenceKeys     []string
	ResolvedReferenceKeys []string
}

type ShotVideoPromptAsset struct {
	AssetID           string          `json:"assetId"`
	AssetType         string          `json:"assetType"`
	Name              string          `json:"name"`
	Description       string          `json:"description,omitempty"`
	Profile           json.RawMessage `json:"profile,omitempty"`
	ConsistencyPrompt string          `json:"consistencyPrompt,omitempty"`
	NegativePrompt    string          `json:"negativePrompt,omitempty"`
	Requirement       map[string]any  `json:"requirement"`
}

type rankedShotImageReference struct {
	Reference provider.GatewayImageReference
	Key       string
	Priority  int
}

func (a Activities) shotAssetContext(ctx context.Context, projectID, shotID string) (ShotAssetContext, error) {
	var referenceMode string
	var referenceKeys []string
	var shotTitle, shotBody string
	if err := a.db.QueryRow(ctx, `
		SELECT
			COALESCE(image_reference_mode, 'auto'),
			COALESCE(image_reference_keys, ARRAY[]::text[]),
			COALESCE(title, ''),
			concat_ws(E'\n', COALESCE(visual, ''), COALESCE(action, ''), COALESCE(dialogue, ''), COALESCE(script_dialogue::text, ''))
		FROM storyboard_shots
		WHERE project_id = $1 AND id = $2 AND deleted_at IS NULL
	`, projectID, shotID).Scan(&referenceMode, &referenceKeys, &shotTitle, &shotBody); err != nil {
		return ShotAssetContext{}, err
	}
	rows, err := a.db.Query(ctx, `
		SELECT
			r.id::text,
			a.id::text,
			a.asset_type,
			a.name,
			a.description,
			COALESCE(a.profile, '{}'::jsonb),
			COALESCE(a.consistency_prompt, ''),
			COALESCE(a.negative_prompt, ''),
			COALESCE(a.primary_reference_artifact_id::text, ''),
			COALESCE(a.primary_reference_storage_key, ''),
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
	promptAssets := []ShotVideoPromptAsset{}
	rankedAutoRefs := []rankedShotImageReference{}
	for rows.Next() {
		var requirementID, assetID, assetType, name, description string
		var profile []byte
		var consistencyPrompt, negativePrompt, primaryReferenceArtifactID, primaryReferenceStorageKey, referenceArtifactID, referenceStorageKey string
		var requirementType, role, costume, pose, expression, action, camera, sceneState, propState, prompt string
		var derivedArtifactID, derivedStorageKey string
		if err := rows.Scan(&requirementID, &assetID, &assetType, &name, &description, &profile, &consistencyPrompt, &negativePrompt, &primaryReferenceArtifactID, &primaryReferenceStorageKey, &referenceArtifactID, &referenceStorageKey, &requirementType, &role, &costume, &pose, &expression, &action, &camera, &sceneState, &propState, &prompt, &derivedArtifactID, &derivedStorageKey); err != nil {
			return ShotAssetContext{}, err
		}
		assetLines = append(assetLines, strings.Join(compactStrings([]string{
			assetType,
			name,
			description,
			"profile=" + string(jsonOrDefault(profile, `{}`)),
			"consistency=" + consistencyPrompt,
			"negative=" + negativePrompt,
		}), " | "))
		promptAssets = append(promptAssets, ShotVideoPromptAsset{
			AssetID:           assetID,
			AssetType:         assetType,
			Name:              name,
			Description:       description,
			Profile:           jsonOrDefault(profile, `{}`),
			ConsistencyPrompt: consistencyPrompt,
			NegativePrompt:    negativePrompt,
			Requirement: map[string]any{
				"id":             requirementID,
				"type":           requirementType,
				"role":           role,
				"costume":        costume,
				"pose":           pose,
				"expression":     expression,
				"action":         action,
				"cameraRelation": camera,
				"sceneState":     sceneState,
				"propState":      propState,
				"prompt":         prompt,
			},
		})
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
		refArtifactID := firstNonEmptyString(derivedArtifactID, primaryReferenceArtifactID, referenceArtifactID)
		refStorageKey := firstNonEmptyString(derivedStorageKey, primaryReferenceStorageKey, referenceStorageKey)
		if refArtifactID != "" || refStorageKey != "" {
			referenceKey := "asset_primary:" + assetID
			if derivedArtifactID != "" || derivedStorageKey != "" {
				referenceKey = "derived:" + requirementID
			}
			priority, priorityReasons := shotImageReferencePriority(shotTitle, shotBody, assetType, name, role, requirementType)
			rankedAutoRefs = append(rankedAutoRefs, rankedShotImageReference{Reference: provider.GatewayImageReference{
				Type:       "image",
				AssetID:    assetID,
				ArtifactID: refArtifactID,
				StorageKey: refStorageKey,
				Metadata: mustJSON(map[string]any{
					"referenceKey":      referenceKey,
					"requirementId":     requirementID,
					"requirementType":   requirementType,
					"roleInShot":        role,
					"assetType":         assetType,
					"assetName":         name,
					"referencePriority": priority,
					"priorityReasons":   priorityReasons,
				}),
			}, Key: referenceKey, Priority: priority})
		}
	}
	if err := rows.Err(); err != nil {
		return ShotAssetContext{}, err
	}
	rows.Close()
	sort.SliceStable(rankedAutoRefs, func(i, j int) bool {
		return rankedAutoRefs[i].Priority > rankedAutoRefs[j].Priority
	})
	autoRefs := make([]provider.GatewayImageReference, 0, len(rankedAutoRefs))
	autoReferenceKeys := make([]string, 0, len(rankedAutoRefs))
	for _, ranked := range rankedAutoRefs {
		autoRefs = append(autoRefs, ranked.Reference)
		autoReferenceKeys = append(autoReferenceKeys, ranked.Key)
	}
	refs := autoRefs
	resolvedReferenceKeys := autoReferenceKeys
	if referenceMode == "none" {
		refs = nil
		resolvedReferenceKeys = nil
	} else if referenceMode == "custom" {
		candidates, err := a.shotImageReferenceCandidates(ctx, projectID, shotID)
		if err != nil {
			return ShotAssetContext{}, err
		}
		refs = make([]provider.GatewayImageReference, 0, len(referenceKeys))
		resolvedReferenceKeys = make([]string, 0, len(referenceKeys))
		for _, key := range referenceKeys {
			if reference, ok := candidates[key]; ok {
				refs = append(refs, reference)
				resolvedReferenceKeys = append(resolvedReferenceKeys, key)
			}
		}
	}
	return ShotAssetContext{
		AssetsSummary:         strings.Join(assetLines, "\n"),
		RequirementsSummary:   strings.Join(requirementLines, "\n"),
		PromptAssets:          promptAssets,
		ImageReferences:       refs,
		AutoImageReferences:   autoRefs,
		ImageReferenceMode:    referenceMode,
		ImageReferenceKeys:    referenceKeys,
		AutoReferenceKeys:     autoReferenceKeys,
		ResolvedReferenceKeys: resolvedReferenceKeys,
	}, nil
}

func shotImageReferencePriority(shotTitle, shotBody, assetType, assetName, role, requirementType string) (int, []string) {
	title := strings.ToLower(strings.TrimSpace(shotTitle))
	body := strings.ToLower(strings.TrimSpace(shotBody))
	name := strings.ToLower(strings.TrimSpace(assetName))
	role = strings.ToLower(strings.TrimSpace(role))
	requirementType = strings.ToLower(strings.TrimSpace(requirementType))

	priority := 0
	reasons := make([]string, 0, 5)
	switch strings.ToLower(strings.TrimSpace(assetType)) {
	case "character":
		priority += 400
		reasons = append(reasons, "character")
	case "prop":
		priority += 250
		reasons = append(reasons, "prop")
	case "scene":
		priority += 100
		reasons = append(reasons, "scene")
	}
	if name != "" {
		switch {
		case strings.Contains(title, name):
			priority += 2400
			reasons = append(reasons, "asset_name_in_title")
		case containsReferenceNameFragment(title, name):
			priority += 1400
			reasons = append(reasons, "asset_name_fragment_in_title")
		}
		switch {
		case strings.Contains(body, name):
			priority += 1200
			reasons = append(reasons, "asset_name_in_shot")
		case containsReferenceNameFragment(body, name):
			priority += 500
			reasons = append(reasons, "asset_name_fragment_in_shot")
		}
	}
	if role != "" {
		if strings.Contains(title, role) || strings.Contains(body, role) {
			priority += 700
			reasons = append(reasons, "role_in_shot_text")
		} else if containsReferenceNameFragment(title+"\n"+body, role) {
			priority += 350
			reasons = append(reasons, "role_fragment_in_shot_text")
		}
		for _, marker := range []string{"lead", "principal", "protagonist", "主角", "首领", "核心", "主体"} {
			if strings.Contains(role, marker) {
				priority += 250
				reasons = append(reasons, "primary_role")
				break
			}
		}
	}
	if strings.Contains(requirementType, "character") || strings.Contains(requirementType, "appearance") {
		priority += 150
		reasons = append(reasons, "character_requirement")
	}
	return priority, reasons
}

func containsReferenceNameFragment(text, name string) bool {
	text = strings.TrimSpace(text)
	nameRunes := []rune(strings.TrimSpace(name))
	if text == "" || len(nameRunes) < 2 {
		return false
	}
	for length := len(nameRunes) - 1; length >= 2; length-- {
		for start := 0; start+length <= len(nameRunes); start++ {
			fragment := strings.TrimSpace(string(nameRunes[start : start+length]))
			if len([]rune(fragment)) >= 2 && strings.Contains(text, fragment) {
				return true
			}
		}
	}
	return false
}

func (a Activities) shotImageReferenceCandidates(ctx context.Context, projectID, shotID string) (map[string]provider.GatewayImageReference, error) {
	rows, err := a.db.Query(ctx, `
		SELECT reference_key, asset_id::text, artifact_id, storage_key, source_type, source_id
		FROM (
			SELECT
				'derived:' || r.id::text AS reference_key,
				r.asset_id,
				r.derived_artifact_id AS artifact_id,
				r.derived_storage_key AS storage_key,
				'derived_asset'::text AS source_type,
				r.id::text AS source_id
			FROM shot_asset_requirements r
			WHERE r.project_id = $1 AND r.storyboard_shot_id = $2
			  AND (r.derived_artifact_id IS NOT NULL OR COALESCE(r.derived_storage_key, '') <> '')
			UNION ALL
			SELECT
				'asset_reference:' || ar.id::text,
				r.asset_id,
				ar.artifact_id,
				ar.storage_key,
				'asset_reference'::text,
				ar.id::text
			FROM shot_asset_requirements r
			JOIN asset_references ar ON ar.asset_id = r.asset_id AND ar.project_id = r.project_id
			WHERE r.project_id = $1 AND r.storyboard_shot_id = $2
			  AND ar.status <> 'archived'
			  AND (ar.artifact_id IS NOT NULL OR COALESCE(ar.storage_key, '') <> '')
			UNION ALL
			SELECT
				'asset_primary:' || a.id::text,
				a.id,
				COALESCE(a.primary_reference_artifact_id, a.reference_artifact_id),
				COALESCE(NULLIF(a.primary_reference_storage_key, ''), a.reference_storage_key),
				'asset_primary'::text,
				a.id::text
			FROM canonical_assets a
			WHERE a.project_id = $1
			  AND COALESCE(a.status, 'draft') <> 'archived'
			  AND (
				a.primary_reference_artifact_id IS NOT NULL OR COALESCE(a.primary_reference_storage_key, '') <> ''
				OR a.reference_artifact_id IS NOT NULL OR COALESCE(a.reference_storage_key, '') <> ''
			  )
		) candidates
	`, projectID, shotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := map[string]provider.GatewayImageReference{}
	for rows.Next() {
		var key, assetID, sourceType, sourceID string
		var artifactID, storageKey sql.NullString
		if err := rows.Scan(&key, &assetID, &artifactID, &storageKey, &sourceType, &sourceID); err != nil {
			return nil, err
		}
		if _, exists := candidates[key]; exists {
			continue
		}
		candidates[key] = provider.GatewayImageReference{
			Type:       "image",
			AssetID:    assetID,
			ArtifactID: artifactID.String,
			StorageKey: storageKey.String,
			Metadata: mustJSON(map[string]any{
				"referenceKey": key,
				"sourceType":   sourceType,
				"sourceId":     sourceID,
			}),
		}
	}
	return candidates, rows.Err()
}

type ShotVideoReferenceContext struct {
	References              []provider.GatewayVideoReference
	ReferenceMode           string
	ConfiguredReferenceKeys []string
	ResolvedReferenceKeys   []string
}

func (a Activities) shotVideoReferenceContext(ctx context.Context, projectID string, shot StoryboardShotRecord, assets ShotAssetContext) (ShotVideoReferenceContext, error) {
	mode := strings.TrimSpace(shot.VideoReferenceMode)
	if mode != "custom" && mode != "none" {
		mode = "auto"
	}
	configuredKeys := compactStrings(shot.VideoReferenceKeys)
	context := ShotVideoReferenceContext{
		ReferenceMode:           mode,
		ConfiguredReferenceKeys: append([]string(nil), configuredKeys...),
	}
	if mode == "none" {
		return context, nil
	}

	shotImageKey := "shot_image:" + shot.ID
	shotImage := provider.GatewayVideoReference{
		Type:        "first_frame",
		ArtifactID:  shot.ImageArtifactID,
		MediaFileID: shot.ImageMediaFileID,
		StorageKey:  shot.ImageStorageKey,
		Metadata: mustJSON(map[string]any{
			"referenceKey": shotImageKey,
			"sourceType":   "shot_image",
			"sourceId":     shot.ID,
		}),
	}
	hasShotImage := shot.ImageArtifactID != "" || shot.ImageMediaFileID != "" || shot.ImageStorageKey != ""
	if mode == "auto" {
		if hasShotImage {
			context.References = []provider.GatewayVideoReference{shotImage}
			context.ResolvedReferenceKeys = []string{shotImageKey}
			return context, nil
		}
		context.References = make([]provider.GatewayVideoReference, 0, len(assets.AutoImageReferences))
		context.ResolvedReferenceKeys = append([]string(nil), assets.AutoReferenceKeys...)
		for _, reference := range assets.AutoImageReferences {
			context.References = append(context.References, videoReferenceFromImage(reference))
		}
		return context, nil
	}

	candidates, err := a.shotImageReferenceCandidates(ctx, projectID, shot.ID)
	if err != nil {
		return ShotVideoReferenceContext{}, err
	}
	videoCandidates := make(map[string]provider.GatewayVideoReference, len(candidates)+1)
	if hasShotImage {
		videoCandidates[shotImageKey] = shotImage
	}
	for key, reference := range candidates {
		videoCandidates[key] = videoReferenceFromImage(reference)
	}
	context.References = make([]provider.GatewayVideoReference, 0, len(configuredKeys))
	context.ResolvedReferenceKeys = make([]string, 0, len(configuredKeys))
	for _, key := range configuredKeys {
		reference, ok := videoCandidates[key]
		if !ok {
			return ShotVideoReferenceContext{}, workflowError{
				Code:    provider.CodeInvalidRequest,
				Message: "configured shot video reference is no longer available: " + key,
			}
		}
		context.References = append(context.References, reference)
		context.ResolvedReferenceKeys = append(context.ResolvedReferenceKeys, key)
	}
	if len(context.References) == 0 {
		return ShotVideoReferenceContext{}, workflowError{
			Code:    provider.CodeInvalidRequest,
			Message: "custom shot video references require at least one available reference",
		}
	}
	return context, nil
}

func videoReferenceFromImage(reference provider.GatewayImageReference) provider.GatewayVideoReference {
	return provider.GatewayVideoReference{
		Type:       "image",
		AssetID:    reference.AssetID,
		ArtifactID: reference.ArtifactID,
		URL:        reference.URL,
		StorageKey: reference.StorageKey,
		Metadata:   reference.Metadata,
	}
}

func (a Activities) completeCanonicalAssetImage(ctx context.Context, input GenerateCanonicalAssetImageInput, execution NodeExecution, asset CanonicalAssetRecord, rendered promptsvc.RenderedPrompt, output GenerateCanonicalAssetImageOutput) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return err
	}
	shouldPrimary := strings.TrimSpace(asset.PrimaryReferenceStorageKey) == "" &&
		strings.TrimSpace(asset.PrimaryReferenceArtifactID) == "" &&
		strings.TrimSpace(asset.PrimaryReferenceMediaFileID) == ""
	if _, err := tx.Exec(ctx, `
		UPDATE canonical_assets
		SET reference_artifact_id = NULLIF($2, '')::uuid,
		    reference_media_file_id = NULLIF($3, '')::uuid,
		    reference_storage_key = NULLIF($4, ''),
		    primary_reference_artifact_id = CASE WHEN $5 THEN NULLIF($2, '')::uuid ELSE primary_reference_artifact_id END,
		    primary_reference_media_file_id = CASE WHEN $5 THEN NULLIF($3, '')::uuid ELSE primary_reference_media_file_id END,
		    primary_reference_storage_key = CASE WHEN $5 THEN NULLIF($4, '') ELSE primary_reference_storage_key END,
		    status = 'image_succeeded',
		    stale_state = 'fresh',
		    updated_at = now()
		WHERE id = $1
	`, input.AssetID, output.ImageArtifactID, output.ImageMediaFileID, output.ImageStorageKey, shouldPrimary); err != nil {
		return err
	}
	var referenceID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO asset_references(
			organization_id, project_id, asset_id, reference_type, title, description,
			artifact_id, media_file_id, storage_key, prompt, prompt_version_id, prompt_hash,
			is_primary, metadata, created_by
		)
		VALUES ($1, $2, $3, 'generated', $4, $5, NULLIF($6, '')::uuid, NULLIF($7, '')::uuid, NULLIF($8, ''),
		        $9, NULLIF($10, '')::uuid, NULLIF($11, ''), $12, $13, $14)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, input.AssetID, "Generated reference", asset.Description, output.ImageArtifactID, output.ImageMediaFileID, output.ImageStorageKey,
		rendered.RenderedText, rendered.PromptVersionID, rendered.RenderedHash, shouldPrimary, mustJSON(map[string]any{
			"source":         "canonical_asset_image_prompt",
			"providerCallId": output.ProviderCallID,
		}), input.CreatedBy).Scan(&referenceID); err != nil {
		return err
	}
	if shouldPrimary {
		if _, err := tx.Exec(ctx, `
			UPDATE asset_references
			SET is_primary = false, updated_at = now()
			WHERE project_id = $1 AND asset_id = $2 AND id <> $3
		`, input.ProjectID, input.AssetID, referenceID); err != nil {
			return err
		}
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
	if _, err := completeNodeRunTx(ctx, tx, execution, mustJSON(output)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) withToonflowVisualPrompt(ctx context.Context, project ProjectProductionSettings, rendered promptsvc.RenderedPrompt, assetType string, derivative bool) (promptsvc.RenderedPrompt, error) {
	style := assetprompts.ToonflowStyleSlug(project.ArtStyle)
	if style == "" {
		return rendered, nil
	}
	suffix := assetprompts.ToonflowVisualTemplateSuffix(assetType, derivative)
	if suffix == "" {
		return rendered, nil
	}
	prefix, ok, err := a.systemPromptContent(ctx, "toonflow_visual_"+style+"_prefix")
	if err != nil || !ok {
		return rendered, err
	}
	target, ok, err := a.systemPromptContent(ctx, "toonflow_visual_"+style+"_"+suffix)
	if err != nil || !ok {
		return rendered, err
	}
	toonflowPrompt := strings.TrimSpace(strings.Join(compactStrings([]string{prefix, target}), "\n\n"))
	if toonflowPrompt == "" {
		return rendered, nil
	}
	rendered.RenderedText = toonflowPrompt + "\n\n" + strings.TrimSpace(rendered.RenderedText)
	rendered.RenderedHash = promptsvc.HashText(rendered.RenderedText)
	rendered.Source = firstNonEmptyString(rendered.Source, "system_active") + "+toonflow_visual"
	return rendered, nil
}

func withCanonicalAssetImageRequirements(rendered promptsvc.RenderedPrompt, assetType string) promptsvc.RenderedPrompt {
	rendered.RenderedText = assetprompts.CanonicalImagePrompt(rendered.RenderedText, assetType)
	rendered.RenderedHash = promptsvc.HashText(rendered.RenderedText)
	rendered.Source = firstNonEmptyString(rendered.Source, "system_active") + "+canonical_asset_layout"
	return rendered
}

func (a Activities) systemPromptContent(ctx context.Context, templateKey string) (string, bool, error) {
	var content string
	err := a.db.QueryRow(ctx, `
		SELECT pv.content
		FROM prompt_templates pt
		JOIN prompt_versions pv ON pv.template_id = pt.id
		WHERE pt.organization_id IS NULL
		  AND pt.template_key = $1
		  AND pt.status = 'active'
		  AND pv.status = 'active'
		ORDER BY COALESCE(pv.activated_at, pv.created_at) DESC
		LIMIT 1
	`, templateKey).Scan(&content)
	if err == pgx.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return content, true, nil
}

func lockedCanonicalAssetRecordImageReferences(asset CanonicalAssetRecord) []provider.GatewayImageReference {
	if !asset.LockReference {
		return nil
	}
	artifactID := firstNonEmptyString(asset.PrimaryReferenceArtifactID, asset.ReferenceArtifactID)
	storageKey := firstNonEmptyString(asset.PrimaryReferenceStorageKey, asset.ReferenceStorageKey)
	if artifactID == "" && storageKey == "" {
		return nil
	}
	return []provider.GatewayImageReference{{
		Type:       "image",
		AssetID:    asset.ID,
		ArtifactID: artifactID,
		StorageKey: storageKey,
		Metadata: mustJSON(map[string]any{
			"source":    "lock_reference",
			"isPrimary": asset.PrimaryReferenceArtifactID != "" || asset.PrimaryReferenceStorageKey != "",
		}),
	}}
}

func (a Activities) completeDerivedAssetImage(ctx context.Context, input GenerateDerivedAssetImageInput, execution NodeExecution, output GenerateDerivedAssetImageOutput) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE shot_asset_requirements
		SET derived_artifact_id = NULLIF($2, '')::uuid,
		    derived_media_file_id = NULLIF($3, '')::uuid,
		    derived_storage_key = NULLIF($4, ''),
		    status = 'image_succeeded',
		    stale_state = 'fresh',
		    updated_at = now()
		WHERE id = $1
	`, input.RequirementID, output.ImageArtifactID, output.ImageMediaFileID, output.ImageStorageKey); err != nil {
		return err
	}
	if _, err := completeNodeRunTx(ctx, tx, execution, mustJSON(output)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func scanCanonicalAssetRecord(row pgx.Row) (CanonicalAssetRecord, error) {
	var item CanonicalAssetRecord
	var profile, visualTraits []byte
	err := row.Scan(
		&item.ID,
		&item.AssetType,
		&item.Name,
		&item.Description,
		&item.BasePrompt,
		&profile,
		&item.ConsistencyPrompt,
		&item.NegativePrompt,
		&visualTraits,
		&item.PrimaryReferenceArtifactID,
		&item.PrimaryReferenceMediaFileID,
		&item.PrimaryReferenceStorageKey,
		&item.LockReference,
		&item.ReferenceArtifactID,
		&item.ReferenceMediaFileID,
		&item.ReferenceStorageKey,
		&item.Status,
		&item.ManualOverride,
		&item.StaleState,
		&item.Revision,
		&item.PromptRevision,
	)
	item.Profile = jsonOrDefault(profile, `{}`)
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
