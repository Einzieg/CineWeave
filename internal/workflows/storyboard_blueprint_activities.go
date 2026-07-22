package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	storyboardpkg "github.com/Einzieg/cineweave/internal/storyboard"
	"github.com/jackc/pgx/v5"
)

const (
	promptKeyStoryboardContinuityBlueprint = "storyboard_continuity_blueprint"
	nodeBuildContinuityBlueprintPrefix     = "storyboard_continuity_blueprint"
)

type BuildEpisodeContinuityBlueprintInput struct {
	OrganizationID  string                       `json:"organizationId"`
	ProjectID       string                       `json:"projectId"`
	WorkflowRunID   string                       `json:"workflowRunId"`
	CreatedBy       string                       `json:"createdBy"`
	ScriptID        string                       `json:"scriptId"`
	ScriptVersionID string                       `json:"scriptVersionId"`
	ScriptEpisodeID string                       `json:"scriptEpisodeId"`
	PacingProfile   string                       `json:"pacingProfile"`
	Timing          TimingAnalysisActivityOutput `json:"timing"`
}

type StoryboardScenePlanRef struct {
	ID            string         `json:"id"`
	SceneKey      string         `json:"sceneKey"`
	ScriptSceneID string         `json:"scriptSceneId,omitempty"`
	SceneOrdinal  int            `json:"sceneOrdinal"`
	StartTick     int64          `json:"startTick"`
	EndTick       int64          `json:"endTick"`
	Status        string         `json:"status"`
	EntryState    map[string]any `json:"entryState"`
	ExitState     map[string]any `json:"exitState"`
}

type ContinuityBlueprintActivityOutput struct {
	BlueprintID       string                                  `json:"blueprintId"`
	BlueprintRevision int                                     `json:"blueprintRevision"`
	StoryboardPlanID  string                                  `json:"storyboardPlanId"`
	PlanRevision      int                                     `json:"planRevision"`
	Blueprint         storyboardpkg.ContinuityBlueprintOutput `json:"blueprint"`
	ScenePlans        []StoryboardScenePlanRef                `json:"scenePlans"`
	ProviderCallID    string                                  `json:"providerCallId,omitempty"`
	ModelID           string                                  `json:"modelId,omitempty"`
	PromptVersionID   string                                  `json:"promptVersionId,omitempty"`
	PromptHash        string                                  `json:"promptHash,omitempty"`
}

func (a Activities) BuildEpisodeContinuityBlueprint(ctx context.Context, input BuildEpisodeContinuityBlueprintInput) (ContinuityBlueprintActivityOutput, error) {
	baseInput := TextToStoryboardInput{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, Prompt: "storyboard_continuity_blueprint", CreatedBy: input.CreatedBy}
	if strings.TrimSpace(input.Timing.AnalysisID) == "" || strings.TrimSpace(input.ScriptEpisodeID) == "" {
		return ContinuityBlueprintActivityOutput{}, fmt.Errorf("timing analysis and scriptEpisodeId are required")
	}
	nodeKey := nodeKeyForID(nodeBuildContinuityBlueprintPrefix, input.ScriptEpisodeID)
	if existing, ok, err := a.existingContinuityBlueprintOutput(ctx, input.WorkflowRunID, nodeKey); err != nil {
		return ContinuityBlueprintActivityOutput{}, err
	} else if ok {
		return existing, nil
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID, input.WorkflowRunID)
	if err != nil {
		return ContinuityBlueprintActivityOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, err)
	}
	scriptScenes, err := a.storyboardScenesForEpisode(ctx, input.ProjectID, input.ScriptVersionID, input.ScriptEpisodeID)
	if err != nil {
		return ContinuityBlueprintActivityOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, err)
	}
	sceneContext := make([]map[string]any, 0, len(input.Timing.Scenes))
	sceneKeys := make([]string, 0, len(input.Timing.Scenes))
	for _, scene := range input.Timing.Scenes {
		sceneKeys = append(sceneKeys, scene.SceneKey)
		blocks := make([]map[string]any, 0, len(scene.Blocks))
		for _, block := range scene.Blocks {
			unitKinds := make([]string, 0, len(block.Units))
			speakers := make([]string, 0, len(block.Units))
			for _, unit := range block.Units {
				unitKinds = append(unitKinds, string(unit.Type))
				if strings.TrimSpace(unit.Speaker) != "" {
					speakers = append(speakers, unit.Speaker)
				}
			}
			blocks = append(blocks, map[string]any{
				"startTick":     block.StartTick,
				"endTick":       block.EndTick,
				"durationTicks": block.DurationTicks,
				"unitKinds":     unitKinds,
				"speakers":      normalizeStringSlice(speakers),
			})
		}
		sceneContext = append(sceneContext, map[string]any{
			"sceneKey":      scene.SceneKey,
			"scriptSceneId": scene.ScriptSceneID,
			"sceneOrdinal":  scene.SceneOrdinal,
			"startTick":     scene.StartTick,
			"endTick":       scene.EndTick,
			"durationTicks": scene.EndTick - scene.StartTick,
			"summary":       scriptSceneSummary(scriptScenes, scene.ScriptSceneID),
			"blocks":        blocks,
		})
	}
	contextJSON := mustJSON(map[string]any{
		"project": map[string]any{
			"videoRatio":       project.VideoRatio,
			"directorManual":   project.DirectorManual,
			"visualManual":     project.VisualManual,
			"timelineTimebase": input.Timing.TimelineTimebase,
			"fpsNumerator":     input.Timing.FPSNumerator,
			"fpsDenominator":   input.Timing.FPSDenominator,
		},
		"episode": map[string]any{
			"id":                     input.ScriptEpisodeID,
			"estimatedDurationTicks": input.Timing.EstimatedDurationTicks,
			"minimumDurationTicks":   input.Timing.MinimumDurationTicks,
		},
		"pacingProfile": defaultStoryboardPacingProfile(input.PacingProfile),
		"scenes":        sceneContext,
	})
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyStoryboardContinuityBlueprint, map[string]any{
		"context": map[string]any{"json": string(contextJSON)},
	})
	if err != nil {
		return ContinuityBlueprintActivityOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, err)
	}
	nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKey,
		NodeType:       "agent.storyboard_continuity_blueprint",
		Input: mustJSON(map[string]any{
			"scriptEpisodeId":   input.ScriptEpisodeID,
			"timingAnalysisId":  input.Timing.AnalysisID,
			"pacingProfile":     defaultStoryboardPacingProfile(input.PacingProfile),
			"modelProfileKey":   project.ScriptModelProfileKey,
			"promptTemplateKey": rendered.TemplateKey,
			"promptVersionId":   rendered.PromptVersionID,
			"promptHash":        rendered.RenderedHash,
		}),
	})
	if err != nil {
		return ContinuityBlueprintActivityOutput{}, err
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, project.ScriptModelProfileKey, []string{"text", "multimodal"}); err != nil {
		return ContinuityBlueprintActivityOutput{}, a.failActivity(ctx, baseInput, nodeRunID, err)
	}
	if a.gateway == nil {
		return ContinuityBlueprintActivityOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
	}
	gatewayResp, err := a.generateProviderText(ctx, nodeRunID, provider.GatewayTextRequest{
		OrganizationID:    input.OrganizationID,
		ProjectID:         input.ProjectID,
		WorkflowRunID:     input.WorkflowRunID,
		NodeRunID:         nodeRunID.NodeRunID,
		ModelProfileKey:   project.ScriptModelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		Input:             mustJSON(map[string]any{"prompt": rendered.RenderedText, "responseFormat": "json", "maxOutputTokens": 12_000}),
		Options:           providerTextGatewayOptions(),
	})
	if err != nil {
		return ContinuityBlueprintActivityOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowErrorFromProvider(err, codeActivityFailed))
	}
	blueprint, err := storyboardpkg.ParseContinuityBlueprint(stripJSONFence(gatewayResp.Output.Text), sceneKeys)
	if err != nil {
		return ContinuityBlueprintActivityOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeInvalidRequest, Message: err.Error()})
	}
	output, err := a.storeContinuityBlueprintAndPlan(ctx, input, nodeRunID, rendered, gatewayResp, blueprint)
	if err != nil {
		return ContinuityBlueprintActivityOutput{}, a.failActivity(ctx, baseInput, nodeRunID, err)
	}
	return output, nil
}

func (a Activities) storeContinuityBlueprintAndPlan(
	ctx context.Context,
	input BuildEpisodeContinuityBlueprintInput,
	execution NodeExecution,
	rendered promptsvc.RenderedPrompt,
	gatewayResp provider.GatewayTextResponse,
	blueprint storyboardpkg.ContinuityBlueprintOutput,
) (ContinuityBlueprintActivityOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ContinuityBlueprintActivityOutput{}, err
	}
	defer tx.Rollback(ctx)
	runCtx, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution)
	if err != nil {
		return ContinuityBlueprintActivityOutput{}, err
	}
	nodeRunID := execution.NodeRunID
	var lockedEpisodeID string
	if err := tx.QueryRow(ctx, `
		SELECT id::text FROM script_episodes WHERE project_id = $1 AND id = $2 FOR UPDATE
	`, input.ProjectID, input.ScriptEpisodeID).Scan(&lockedEpisodeID); err != nil {
		return ContinuityBlueprintActivityOutput{}, err
	}
	var blueprintRevision, planRevision int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision), 0) + 1 FROM episode_continuity_blueprints WHERE script_episode_id = $1`, input.ScriptEpisodeID).Scan(&blueprintRevision); err != nil {
		return ContinuityBlueprintActivityOutput{}, err
	}
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision), 0) + 1 FROM storyboard_plans WHERE script_episode_id = $1`, input.ScriptEpisodeID).Scan(&planRevision); err != nil {
		return ContinuityBlueprintActivityOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE episode_continuity_blueprints SET status = 'archived'
		WHERE script_episode_id = $1 AND status = 'ready'
	`, input.ScriptEpisodeID); err != nil {
		return ContinuityBlueprintActivityOutput{}, err
	}
	var blueprintID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO episode_continuity_blueprints(
			organization_id, project_id, script_id, script_version_id, script_episode_id,
			timing_analysis_id, revision, status, blueprint, dependencies, serial_groups, parallel_groups,
			prompt_version_id, prompt_hash, provider_call_id, model_id, metadata, created_by, completed_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'ready', $8, $9, $10, $11,
		        NULLIF($12, '')::uuid, NULLIF($13, ''), NULLIF($14, '')::uuid, NULLIF($15, '')::uuid,
		        $16, NULLIF($17, '')::uuid, now())
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, input.ScriptID, input.ScriptVersionID, input.ScriptEpisodeID,
		input.Timing.AnalysisID, blueprintRevision, mustJSON(blueprint), mustJSON(blueprint.Dependencies),
		mustJSON(blueprint.SerialGroups), mustJSON(blueprint.ParallelGroups), rendered.PromptVersionID, rendered.RenderedHash,
		gatewayResp.ProviderCallID, gatewayResp.ModelID, mustJSON(map[string]any{"workflowRunId": input.WorkflowRunID, "nodeRunId": nodeRunID}), input.CreatedBy).Scan(&blueprintID); err != nil {
		return ContinuityBlueprintActivityOutput{}, err
	}
	targetDurationTicks := input.Timing.EstimatedDurationTicks
	if input.Timing.TargetDurationTicks != nil {
		targetDurationTicks = *input.Timing.TargetDurationTicks
	}
	estimatedShotCount := 0
	for _, scene := range blueprint.Scenes {
		estimatedShotCount += (scene.SuggestedShotMinimum + scene.SuggestedShotMaximum + 1) / 2
	}
	var planID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO storyboard_plans(
			organization_id, project_id, script_id, script_version_id, script_episode_id,
			timing_analysis_id, revision, status, pacing_profile, target_duration_ticks,
			estimated_shot_count, actual_shot_count, active, stale_state, metadata, created_by,
			production_generation_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'planning', $8, $9, $10, 0, false, 'fresh', $11, NULLIF($12, '')::uuid, $13)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, input.ScriptID, input.ScriptVersionID, input.ScriptEpisodeID,
		input.Timing.AnalysisID, planRevision, mustJSON(map[string]any{"key": defaultStoryboardPacingProfile(input.PacingProfile)}),
		targetDurationTicks, estimatedShotCount, mustJSON(map[string]any{
			"workflowRunId":     input.WorkflowRunID,
			"blueprintId":       blueprintID,
			"blueprintRevision": blueprintRevision,
		}), input.CreatedBy, runCtx.ProductionGenerationID).Scan(&planID); err != nil {
		return ContinuityBlueprintActivityOutput{}, err
	}
	timingSceneByKey := make(map[string]storyboardpkg.AnalyzedTimingScene, len(input.Timing.Scenes))
	for _, scene := range input.Timing.Scenes {
		timingSceneByKey[scene.SceneKey] = scene
	}
	groupByScene := continuityDependencyGroupByScene(blueprint)
	refs := make([]StoryboardScenePlanRef, 0, len(blueprint.Scenes))
	for _, blueprintScene := range blueprint.Scenes {
		timingScene := timingSceneByKey[blueprintScene.SceneKey]
		var scenePlanID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO storyboard_scene_plans(
				organization_id, project_id, storyboard_plan_id, blueprint_id, script_scene_id,
				scene_key, scene_ordinal, dependency_group, status, start_tick, end_tick,
				planner_input, entry_state, exit_state, metadata, created_by
			)
			VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid, $6, $7, NULLIF($8, ''), 'pending', $9, $10,
			        $11, $12, $13, $14, NULLIF($15, '')::uuid)
			RETURNING id::text
		`, input.OrganizationID, input.ProjectID, planID, blueprintID, timingScene.ScriptSceneID,
			blueprintScene.SceneKey, blueprintScene.SceneOrdinal, groupByScene[blueprintScene.SceneKey],
			timingScene.StartTick, timingScene.EndTick,
			mustJSON(map[string]any{"pacingProfile": blueprintScene.PacingProfile, "oneTake": blueprintScene.OneTake}),
			mustJSON(blueprintScene.EntryState), mustJSON(blueprintScene.ExitState),
			mustJSON(map[string]any{"workflowRunId": input.WorkflowRunID}), input.CreatedBy).Scan(&scenePlanID); err != nil {
			return ContinuityBlueprintActivityOutput{}, err
		}
		refs = append(refs, StoryboardScenePlanRef{
			ID:            scenePlanID,
			SceneKey:      blueprintScene.SceneKey,
			ScriptSceneID: timingScene.ScriptSceneID,
			SceneOrdinal:  blueprintScene.SceneOrdinal,
			StartTick:     timingScene.StartTick,
			EndTick:       timingScene.EndTick,
			Status:        "pending",
			EntryState:    blueprintScene.EntryState,
			ExitState:     blueprintScene.ExitState,
		})
	}
	output := ContinuityBlueprintActivityOutput{
		BlueprintID:       blueprintID,
		BlueprintRevision: blueprintRevision,
		StoryboardPlanID:  planID,
		PlanRevision:      planRevision,
		Blueprint:         blueprint,
		ScenePlans:        refs,
		ProviderCallID:    gatewayResp.ProviderCallID,
		ModelID:           gatewayResp.ModelID,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
	}
	if _, err := tx.Exec(ctx, `
		UPDATE episode_continuity_blueprints
		SET metadata = metadata || jsonb_build_object('activityOutput', $2::jsonb)
		WHERE id = $1
	`, blueprintID, mustJSON(output)); err != nil {
		return ContinuityBlueprintActivityOutput{}, err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.blueprint.completed", "episode_continuity_blueprint", blueprintID, mustJSON(map[string]any{
		"workflowRunId":    input.WorkflowRunID,
		"scriptEpisodeId":  input.ScriptEpisodeID,
		"blueprintId":      blueprintID,
		"storyboardPlanId": planID,
		"sceneCount":       len(refs),
	})); err != nil {
		return ContinuityBlueprintActivityOutput{}, err
	}
	if _, err := completeNodeRunTx(ctx, tx, execution, mustJSON(output)); err != nil {
		return ContinuityBlueprintActivityOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ContinuityBlueprintActivityOutput{}, err
	}
	return output, nil
}

func (a Activities) existingContinuityBlueprintOutput(ctx context.Context, workflowRunID, nodeKey string) (ContinuityBlueprintActivityOutput, bool, error) {
	var raw json.RawMessage
	err := a.db.QueryRow(ctx, `
		SELECT output FROM workflow_node_runs
		WHERE workflow_run_id = $1 AND node_key = $2 AND status = 'succeeded'
	`, workflowRunID, nodeKey).Scan(&raw)
	if err == pgx.ErrNoRows {
		return ContinuityBlueprintActivityOutput{}, false, nil
	}
	if err != nil {
		return ContinuityBlueprintActivityOutput{}, false, err
	}
	var output ContinuityBlueprintActivityOutput
	if err := json.Unmarshal(raw, &output); err != nil {
		return ContinuityBlueprintActivityOutput{}, false, err
	}
	return output, output.StoryboardPlanID != "", nil
}

func scriptSceneSummary(scenes []ScriptSceneRecord, sceneID string) map[string]any {
	for _, scene := range scenes {
		if scene.ID == sceneID {
			return map[string]any{
				"id":            scene.ID,
				"sceneNo":       scene.SceneNo,
				"title":         scene.Title,
				"summary":       scene.Summary,
				"location":      scene.Location,
				"timeOfDay":     scene.TimeOfDay,
				"atmosphere":    scene.Atmosphere,
				"characters":    scene.Characters,
				"visualGoal":    scene.VisualGoal,
				"emotionalTone": scene.EmotionalTone,
			}
		}
	}
	return map[string]any{}
}

func continuityDependencyGroupByScene(blueprint storyboardpkg.ContinuityBlueprintOutput) map[string]string {
	groups := map[string]string{}
	for index, group := range blueprint.SerialGroups {
		for _, sceneKey := range group {
			groups[sceneKey] = fmt.Sprintf("serial-%d", index+1)
		}
	}
	for index, group := range blueprint.ParallelGroups {
		for _, sceneKey := range group {
			if groups[sceneKey] == "" {
				groups[sceneKey] = fmt.Sprintf("parallel-%d", index+1)
			}
		}
	}
	return groups
}

func defaultStoryboardPacingProfile(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "fast", "slow":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "standard"
	}
}
