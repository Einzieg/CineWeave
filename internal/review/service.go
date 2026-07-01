package review

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

func RunDeterministicProjectChecks(ctx context.Context, db *pgxpool.Pool, projectID string) ([]ReviewItemDraft, error) {
	runner := deterministicRunner{db: db, projectID: projectID}
	return runner.run(ctx)
}

func BuildProjectReviewContext(ctx context.Context, db *pgxpool.Pool, projectID string) (map[string]any, error) {
	queryJSON := func(query string, args ...any) (json.RawMessage, error) {
		var raw []byte
		if err := db.QueryRow(ctx, query, args...).Scan(&raw); err != nil {
			return nil, err
		}
		return json.RawMessage(raw), nil
	}
	project, err := queryJSON(`
		SELECT COALESCE(to_jsonb(p), '{}'::jsonb)
		FROM (
			SELECT id, name, description, content_type, video_ratio, art_style, production_mode, active_final_video_version_id
			FROM projects
			WHERE id = $1
		) p
	`, projectID)
	if err != nil {
		return nil, err
	}
	scripts, err := queryJSON(`
		SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.updated_at DESC, t.created_at DESC), '[]'::jsonb)
		FROM (
			SELECT id, title, status, current_version_id, created_at, updated_at
			FROM scripts
			WHERE project_id = $1
			LIMIT 20
		) t
	`, projectID)
	if err != nil {
		return nil, err
	}
	scenes, err := queryJSON(`
		SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.scene_index, t.id), '[]'::jsonb)
		FROM (
			SELECT id, script_id, script_version_id, scene_no, title, summary, review_status, manual_override, stale_state
			FROM script_scenes
			WHERE project_id = $1
			LIMIT 80
		) t
	`, projectID)
	if err != nil {
		return nil, err
	}
	assets, err := queryJSON(`
		SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.asset_type, t.name), '[]'::jsonb)
		FROM (
			SELECT id, asset_type, name, description, status, review_status, stale_state,
			       primary_reference_storage_key, reference_storage_key
			FROM canonical_assets
			WHERE project_id = $1
			LIMIT 80
		) t
	`, projectID)
	if err != nil {
		return nil, err
	}
	shots, err := queryJSON(`
		SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.shot_index, t.id), '[]'::jsonb)
		FROM (
			SELECT id, script_scene_id, shot_no, visual, duration_seconds, review_status, stale_state,
			       image_status, video_status, image_artifact_id, video_artifact_id
			FROM storyboard_shots
			WHERE project_id = $1 AND deleted_at IS NULL
			LIMIT 120
		) t
	`, projectID)
	if err != nil {
		return nil, err
	}
	requirements, err := queryJSON(`
		SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.created_at, t.id), '[]'::jsonb)
		FROM (
			SELECT id, storyboard_shot_id, asset_id, requirement_type, role_in_shot, pose, expression, action,
			       status, review_status, stale_state, derived_storage_key
			FROM shot_asset_requirements
			WHERE project_id = $1
			LIMIT 160
		) t
	`, projectID)
	if err != nil {
		return nil, err
	}
	timelines, err := queryJSON(`
		SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.created_at DESC, t.id), '[]'::jsonb)
		FROM (
			SELECT id, title, status, resolution, aspect_ratio, created_at
			FROM project_timelines
			WHERE project_id = $1
			LIMIT 20
		) t
	`, projectID)
	if err != nil {
		return nil, err
	}
	finalVideos, err := queryJSON(`
		SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.version DESC, t.created_at DESC), '[]'::jsonb)
		FROM (
			SELECT id, timeline_id, version, title, status, storage_key, created_at
			FROM final_video_versions
			WHERE project_id = $1
			LIMIT 20
		) t
	`, projectID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"project":         project,
		"scripts":         scripts,
		"scriptScenes":    scenes,
		"assets":          assets,
		"storyboardShots": shots,
		"requirements":    requirements,
		"timelines":       timelines,
		"finalVideos":     finalVideos,
	}, nil
}
