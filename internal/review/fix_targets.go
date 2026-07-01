package review

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5"
)

type QueryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func LoadReviewFixTarget(ctx context.Context, db QueryRower, projectID, entityType, entityID string) (ReviewFixTarget, error) {
	entityType = strings.TrimSpace(entityType)
	entityID = strings.TrimSpace(entityID)
	if !SupportedFixTarget(entityType) {
		return ReviewFixTarget{}, ErrUnsupportedFixTarget
	}
	if entityID == "" {
		return ReviewFixTarget{}, pgx.ErrNoRows
	}

	var query string
	switch entityType {
	case "script_scene":
		query = `
			SELECT jsonb_build_object(
			  'title', title,
			  'summary', summary,
			  'location', location,
			  'timeOfDay', time_of_day,
			  'atmosphere', atmosphere,
			  'characters', characters,
			  'scenes', scenes,
			  'props', props,
			  'action', action,
			  'dialogue', dialogue,
			  'visualGoal', visual_goal,
			  'emotionalTone', emotional_tone,
			  'conflict', conflict,
			  'outcome', outcome,
			  'content', content
			)
			FROM script_scenes
			WHERE project_id = $1 AND id = $2`
	case "canonical_asset":
		query = `
			SELECT jsonb_build_object(
			  'name', name,
			  'description', description,
			  'profile', profile,
			  'basePrompt', base_prompt,
			  'consistencyPrompt', consistency_prompt,
			  'negativePrompt', negative_prompt,
			  'lockReference', lock_reference
			)
			FROM canonical_assets
			WHERE project_id = $1 AND id = $2`
	case "storyboard_shot":
		query = `
			SELECT jsonb_build_object(
			  'visual', visual,
			  'camera', camera,
			  'motion', motion,
			  'mood', mood,
			  'durationSeconds', duration_seconds,
			  'imagePrompt', image_prompt,
			  'videoPrompt', video_prompt
			)
			FROM storyboard_shots
			WHERE project_id = $1 AND id = $2 AND deleted_at IS NULL`
	case "shot_asset_requirement":
		query = `
			SELECT jsonb_build_object(
			  'costume', costume,
			  'pose', pose,
			  'expression', expression,
			  'action', action,
			  'cameraRelation', camera_relation,
			  'sceneState', scene_state,
			  'propState', prop_state,
			  'prompt', prompt
			)
			FROM shot_asset_requirements
			WHERE project_id = $1 AND id = $2`
	case "timeline_clip":
		query = `
			SELECT jsonb_build_object(
			  'title', title,
			  'enabled', enabled,
			  'trimStartSeconds', trim_start_seconds,
			  'trimEndSeconds', trim_end_seconds,
			  'targetDurationSeconds', target_duration_seconds,
			  'notes', notes
			)
			FROM timeline_clips
			WHERE project_id = $1 AND id = $2`
	case "project_timeline":
		query = `
			SELECT jsonb_build_object(
			  'title', title,
			  'aspectRatio', aspect_ratio,
			  'resolution', resolution,
			  'metadata', metadata
			)
			FROM project_timelines
			WHERE project_id = $1 AND id = $2`
	}

	var raw []byte
	if err := db.QueryRow(ctx, query, projectID, entityID).Scan(&raw); err != nil {
		return ReviewFixTarget{}, err
	}
	snapshot := map[string]any{}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return ReviewFixTarget{}, err
	}
	return ReviewFixTarget{
		EntityType:     entityType,
		EntityID:       entityID,
		Snapshot:       snapshot,
		EditableFields: EditableFieldsForEntity(entityType),
	}, nil
}
