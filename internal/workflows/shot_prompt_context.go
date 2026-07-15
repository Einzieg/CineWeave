package workflows

import "context"

type shotPromptNarrativeContext struct {
	Source shotVideoPromptSource
	Script shotVideoPromptScript
	Scene  shotVideoPromptScene
}

func (a Activities) loadShotPromptNarrativeContext(ctx context.Context, projectID, shotID string) (shotPromptNarrativeContext, error) {
	var result shotPromptNarrativeContext
	var characters []byte
	err := a.db.QueryRow(ctx, `
		SELECT
			COALESCE(ps.id::text, ''),
			COALESCE(ps.title, ''),
			COALESCE(nc.id::text, ''),
			COALESCE(nc.chapter_title, nc.title, ''),
			COALESCE(nc.volume_index, 0),
			COALESCE(nc.section_index, nc.chapter_index, 0),
			COALESCE(nc.content, ''),
			COALESCE(s.id::text, ''),
			COALESCE(se.id::text, ''),
			COALESCE(se.episode_index, ss.episode_index, 0),
			COALESCE(se.episode_title, ''),
			COALESCE(se.content, ''),
			COALESCE(sc.id::text, ''),
			COALESCE(sc.scene_no, 0),
			COALESCE(sc.title, ''),
			COALESCE(sc.summary, ''),
			COALESCE(sc.location, ''),
			COALESCE(sc.time_of_day, ''),
			COALESCE(sc.atmosphere, ''),
			COALESCE(sc.characters, '[]'::jsonb),
			COALESCE(sc.action, ''),
			COALESCE(sc.dialogue, ''),
			COALESCE(sc.visual_goal, ''),
			COALESCE(sc.emotional_tone, ''),
			COALESCE(sc.content, '')
		FROM storyboard_shots ss
		LEFT JOIN script_scenes sc ON sc.id = ss.script_scene_id
		LEFT JOIN script_episodes se ON se.id = COALESCE(ss.script_episode_id, sc.script_episode_id)
		LEFT JOIN scripts s ON s.id = COALESCE(ss.script_id, sc.script_id, se.script_id)
		LEFT JOIN novel_chapters nc ON nc.id = se.source_chapter_id
		LEFT JOIN project_sources ps ON ps.id = COALESCE(se.source_id, s.source_id, nc.source_id)
		WHERE ss.project_id = $1 AND ss.id = $2 AND ss.deleted_at IS NULL
	`, projectID, shotID).Scan(
		&result.Source.SourceID,
		&result.Source.SourceTitle,
		&result.Source.ChapterID,
		&result.Source.ChapterTitle,
		&result.Source.VolumeIndex,
		&result.Source.SectionIndex,
		&result.Source.Content,
		&result.Script.ScriptID,
		&result.Script.EpisodeID,
		&result.Script.Episode,
		&result.Script.EpisodeTitle,
		&result.Script.Content,
		&result.Scene.SceneID,
		&result.Scene.SceneNo,
		&result.Scene.Title,
		&result.Scene.Summary,
		&result.Scene.Location,
		&result.Scene.TimeOfDay,
		&result.Scene.Atmosphere,
		&characters,
		&result.Scene.Action,
		&result.Scene.Dialogue,
		&result.Scene.VisualGoal,
		&result.Scene.EmotionalTone,
		&result.Scene.Content,
	)
	if err != nil {
		return shotPromptNarrativeContext{}, err
	}
	result.Scene.Characters = jsonOrDefault(characters, `[]`)
	return result, nil
}
