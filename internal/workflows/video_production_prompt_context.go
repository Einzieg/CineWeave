package workflows

import (
	"context"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
)

type shotPromptContextSource struct {
	EpisodeID          string
	EpisodeScript      string
	ContinuityDigest   string
	SceneID            string
	SceneOrdinal       int
	CurrentSceneScript string
	Adjacent           []videoproduction.AdjacentSceneSummary
}

type persistedPromptContextPlan struct {
	ID       string
	Revision int
	Plan     videoproduction.PromptContextPlan
}

func (a Activities) compileShotPromptContextPlan(
	ctx context.Context,
	project ProjectProductionSettings,
	shot StoryboardShotRecord,
	state videoproduction.ShotState,
	modelConstraints []provider.GatewayModelConstraintCandidate,
) (videoproduction.PromptContextPlan, error) {
	source, err := a.loadShotPromptContextSource(ctx, project.ID, shot.ID)
	if err != nil {
		return videoproduction.PromptContextPlan{}, err
	}
	contextLimit, promptLimit := promptContextLimits(modelConstraints)
	spokenDialogue := SpokenStoryboardDialogue(shot.Dialogue)
	dialogue := make([]videoproduction.DialogueCue, 0, len(spokenDialogue))
	for _, line := range spokenDialogue {
		dialogue = append(dialogue, videoproduction.DialogueCue{
			TimingUnitID:          line.TimingUnitID,
			Speaker:               line.Speaker,
			Text:                  line.Text,
			Delivery:              line.Delivery,
			Kind:                  line.Kind,
			StartTick:             line.SpanStartTick,
			EndTick:               line.SpanEndTick,
			ContinuesFromPrevious: line.ContinuesFromPrevious,
			ContinuesToNext:       line.ContinuesToNext,
		})
	}
	return videoproduction.CompilePromptContextPlan(videoproduction.PromptContextCompileInput{
		EpisodeScript:           source.EpisodeScript,
		EpisodeContinuityDigest: source.ContinuityDigest,
		CurrentSceneScript:      source.CurrentSceneScript,
		AdjacentSceneSummaries:  source.Adjacent,
		CurrentShotState:        state,
		VerbatimDialogueCues:    dialogue,
		ModelContextLimit:       contextLimit,
		ModelPromptLimit:        promptLimit,
	})
}

func promptContextLimits(candidates []provider.GatewayModelConstraintCandidate) (int, int) {
	const defaultContextLimit = 32768
	const defaultPromptLimit = 16000
	contextLimit := 0
	promptLimit := 0
	for _, candidate := range candidates {
		if candidate.ContextWindow > 0 && (contextLimit == 0 || candidate.ContextWindow < contextLimit) {
			contextLimit = candidate.ContextWindow
		}
		limit := candidate.Prompt.MaxLength
		if candidate.Prompt.Unit == provider.PromptLengthUnitUTF8Bytes && limit > 0 {
			// A three-byte CJK rune is the conservative common case.
			limit /= 3
		}
		if limit > 0 && (promptLimit == 0 || limit < promptLimit) {
			promptLimit = limit
		}
	}
	if contextLimit == 0 {
		contextLimit = defaultContextLimit
	}
	if promptLimit == 0 {
		promptLimit = defaultPromptLimit
	}
	return contextLimit, promptLimit
}

func (a Activities) loadShotPromptContextSource(ctx context.Context, projectID, shotID string) (shotPromptContextSource, error) {
	var result shotPromptContextSource
	err := a.db.QueryRow(ctx, `
		SELECT episode.id::text,
		       COALESCE(episode.content, ''),
		       COALESCE(episode.metadata->>'continuityDigest', ''),
		       COALESCE(scene.id::text, ''),
		       COALESCE(scene.scene_index, 0),
		       concat_ws(E'\n',
		         NULLIF(scene.title, ''), NULLIF(scene.summary, ''),
		         NULLIF(scene.location, ''), NULLIF(scene.time_of_day, ''),
		         NULLIF(scene.atmosphere, ''), NULLIF(scene.action, ''),
		         NULLIF(scene.dialogue, ''), NULLIF(scene.visual_goal, ''),
		         NULLIF(scene.emotional_tone, ''), NULLIF(scene.content, '')
		       )
		FROM storyboard_shots shot
		JOIN script_episodes episode ON episode.id = shot.script_episode_id
		LEFT JOIN script_scenes scene ON scene.id = shot.script_scene_id
		WHERE shot.project_id = $1 AND shot.id = $2 AND shot.deleted_at IS NULL
	`, projectID, shotID).Scan(
		&result.EpisodeID, &result.EpisodeScript, &result.ContinuityDigest,
		&result.SceneID, &result.SceneOrdinal, &result.CurrentSceneScript,
	)
	if err != nil {
		return shotPromptContextSource{}, err
	}
	rows, err := a.db.Query(ctx, `
		SELECT id::text, scene_index,
		       concat_ws(' ', NULLIF(title, ''), NULLIF(summary, ''),
		         NULLIF(location, ''), NULLIF(time_of_day, ''),
		         NULLIF(action, ''), NULLIF(dialogue, ''))
		FROM script_scenes
		WHERE project_id = $1 AND script_episode_id = $2
		  AND id::text <> NULLIF($3, '')
		  AND deleted_at IS NULL
		ORDER BY abs(scene_index - $4), scene_index
		LIMIT 4
	`, projectID, result.EpisodeID, result.SceneID, result.SceneOrdinal)
	if err != nil {
		return shotPromptContextSource{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item videoproduction.AdjacentSceneSummary
		if err := rows.Scan(&item.SceneID, &item.Ordinal, &item.Summary); err != nil {
			return shotPromptContextSource{}, err
		}
		if item.Ordinal < result.SceneOrdinal {
			item.Relation = "previous"
		} else {
			item.Relation = "next"
		}
		result.Adjacent = append(result.Adjacent, item)
	}
	return result, rows.Err()
}

func (a Activities) persistPromptContextPlan(
	ctx context.Context,
	organizationID, projectID, workflowRunID, createdBy string,
	execution NodeExecution,
	project ProjectProductionSettings,
	shot StoryboardShotRecord,
	plan videoproduction.PromptContextPlan,
) (persistedPromptContextPlan, error) {
	if strings.TrimSpace(shot.StoryboardPlanID) == "" || strings.TrimSpace(shot.ScriptEpisodeID) == "" {
		return persistedPromptContextPlan{}, fmt.Errorf("storyboard plan and script episode are required for prompt context")
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return persistedPromptContextPlan{}, err
	}
	defer tx.Rollback(ctx)
	runContext, err := lockNodeBusinessWrite(ctx, tx, workflowRunID, execution)
	if err != nil {
		return persistedPromptContextPlan{}, err
	}
	if runContext.ProductionGenerationID != project.ProductionGenerationID || runContext.VideoProductionBindingID != project.VideoProductionBindingID || runContext.VideoProductionBindingRevision != project.VideoProductionBindingRevision {
		return persistedPromptContextPlan{}, ErrWorkflowWriteFenced
	}
	var existing persistedPromptContextPlan
	err = tx.QueryRow(ctx, `
		SELECT id::text, revision, plan_hash
		FROM prompt_context_plans
		WHERE storyboard_shot_id = $1 AND status = 'active'
		FOR UPDATE
	`, shot.ID).Scan(&existing.ID, &existing.Revision, &existing.Plan.PlanHash)
	if err != nil && err != pgx.ErrNoRows {
		return persistedPromptContextPlan{}, err
	}
	if err == nil && existing.Plan.PlanHash == plan.PlanHash {
		existing.Plan = plan
		if err := tx.Commit(ctx); err != nil {
			return persistedPromptContextPlan{}, err
		}
		return existing, nil
	}
	if err == nil {
		if _, err := tx.Exec(ctx, `
			UPDATE prompt_context_plans
			SET status = 'stale', stale_at = now()
			WHERE id = $1
		`, existing.ID); err != nil {
			return persistedPromptContextPlan{}, err
		}
	}
	var revision int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision), 0) + 1 FROM prompt_context_plans WHERE storyboard_shot_id = $1`, shot.ID).Scan(&revision); err != nil {
		return persistedPromptContextPlan{}, err
	}
	var planID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO prompt_context_plans(
			organization_id, project_id, production_generation_id,
			video_production_binding_id, video_production_binding_revision,
			storyboard_plan_id, storyboard_shot_id, script_episode_id, script_scene_id,
			revision, status, episode_continuity_digest, current_scene_script,
			adjacent_scene_summaries, current_shot_state, verbatim_dialogue_cues,
			model_context_limit, model_prompt_limit, budget_allocation,
			source_hashes, plan_hash, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, '')::uuid,
		        $10, 'active', $11, $12, $13, $14, $15,
		        $16, $17, $18, $19, $20, NULLIF($21, '')::uuid)
		RETURNING id::text
	`, organizationID, projectID, project.ProductionGenerationID,
		project.VideoProductionBindingID, project.VideoProductionBindingRevision,
		shot.StoryboardPlanID, shot.ID, shot.ScriptEpisodeID, shot.ScriptSceneID,
		revision, plan.EpisodeContinuityDigest, plan.CurrentSceneScript,
		mustJSON(plan.AdjacentSceneSummaries), mustJSON(plan.CurrentShotState), mustJSON(plan.VerbatimDialogueCues),
		plan.ModelContextLimit, plan.ModelPromptLimit, mustJSON(plan.BudgetAllocation),
		mustJSON(plan.SourceHashes), plan.PlanHash, createdBy).Scan(&planID); err != nil {
		return persistedPromptContextPlan{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return persistedPromptContextPlan{}, err
	}
	return persistedPromptContextPlan{ID: planID, Revision: revision, Plan: plan}, nil
}

func (a Activities) loadRenderSegmentPromptContextIdentity(
	ctx context.Context,
	project ProjectProductionSettings,
	shot StoryboardShotRecord,
	executionPlanID, renderSegmentID, compiledPlanHash string,
) (persistedPromptContextPlan, error) {
	var result persistedPromptContextPlan
	err := a.db.QueryRow(ctx, `
		SELECT context_plan.id::text, context_plan.revision, context_plan.plan_hash
		FROM video_render_plans plan
		JOIN video_render_segments segment
		  ON segment.video_render_plan_id = plan.id
		 AND segment.id = $2
		 AND segment.storyboard_shot_id = plan.storyboard_shot_id
		 AND segment.production_generation_id = plan.production_generation_id
		JOIN prompt_context_plans context_plan
		  ON context_plan.id = plan.prompt_context_plan_id
		 AND context_plan.status = 'active'
		 AND context_plan.plan_hash = plan.prompt_context_plan_hash
		WHERE plan.id = $1
		  AND plan.project_id = $3
		  AND plan.storyboard_shot_id = $4
		  AND plan.production_generation_id = $5
		  AND plan.video_production_binding_id = $6
		  AND plan.video_production_binding_revision = $7
		  AND plan.active = true
		  AND plan.status NOT IN ('stale', 'archived', 'cancelled', 'replan_required')
	`, executionPlanID, renderSegmentID, project.ID, shot.ID, project.ProductionGenerationID,
		project.VideoProductionBindingID, project.VideoProductionBindingRevision).Scan(
		&result.ID, &result.Revision, &result.Plan.PlanHash,
	)
	if err == pgx.ErrNoRows {
		return persistedPromptContextPlan{}, workflowError{
			Code:    provider.CodeRenderPlanReplanRequired,
			Message: "视频分段引用的镜头提示词上下文已失效，请重新生成视频提示词",
		}
	}
	if err != nil {
		return persistedPromptContextPlan{}, err
	}
	if cleanContractHash(compiledPlanHash) != cleanContractHash(result.Plan.PlanHash) {
		return persistedPromptContextPlan{}, workflowError{
			Code:    provider.CodeRenderPlanReplanRequired,
			Message: "镜头提示词上下文已变化，请重新生成视频提示词",
		}
	}
	return result, nil
}
