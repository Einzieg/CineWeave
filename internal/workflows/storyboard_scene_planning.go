package workflows

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Einzieg/cineweave/internal/provider"
	storyboardpkg "github.com/Einzieg/cineweave/internal/storyboard"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/sdk/temporal"
)

const (
	promptKeyStoryboardScenePlanner = "storyboard_scene_planner"
	nodePlanStoryboardScenePrefix   = "storyboard_scene_plan"
)

type PlanStoryboardSceneInput struct {
	OrganizationID   string                               `json:"organizationId"`
	ProjectID        string                               `json:"projectId"`
	WorkflowRunID    string                               `json:"workflowRunId"`
	CreatedBy        string                               `json:"createdBy"`
	ScriptID         string                               `json:"scriptId"`
	ScriptVersionID  string                               `json:"scriptVersionId"`
	ScriptEpisodeID  string                               `json:"scriptEpisodeId"`
	StoryboardPlanID string                               `json:"storyboardPlanId"`
	BlueprintID      string                               `json:"blueprintId"`
	ScenePlanID      string                               `json:"scenePlanId"`
	SceneKey         string                               `json:"sceneKey"`
	SceneOrdinal     int                                  `json:"sceneOrdinal"`
	RetryGeneration  int                                  `json:"retryGeneration"`
	SceneShotBudget  int                                  `json:"sceneShotBudget,omitempty"`
	CorrectionHints  []storyboardpkg.StoryboardCorrection `json:"correctionHints,omitempty"`
}

type PlannedStoryboardShot struct {
	ID                   string                             `json:"id"`
	ShotOrdinal          int                                `json:"shotOrdinal"`
	StartTick            int64                              `json:"startTick"`
	EndTick              int64                              `json:"endTick"`
	DurationTicks        int64                              `json:"durationTicks"`
	Title                string                             `json:"title,omitempty"`
	Visual               string                             `json:"visual"`
	Camera               string                             `json:"camera,omitempty"`
	Motion               string                             `json:"motion,omitempty"`
	Mood                 string                             `json:"mood,omitempty"`
	OneTake              bool                               `json:"oneTake"`
	TimingSpans          []storyboardpkg.TimingSpan         `json:"timingSpans"`
	ScriptDialogue       []StoryboardDialogueLine           `json:"scriptDialogue"`
	ImagePromptDirection string                             `json:"imagePromptDirection,omitempty"`
	VideoPromptDirection string                             `json:"videoPromptDirection,omitempty"`
	AssetRequirements    []ShotAssetRequirementRecord       `json:"assetRequirements,omitempty"`
	PlannedEntryState    videoproduction.ShotState          `json:"plannedEntryState"`
	PlannedExitState     videoproduction.ShotState          `json:"plannedExitState"`
	Transition           videoproduction.ShotTransition     `json:"transitionFromPrevious"`
	EntryStateHash       string                             `json:"entryStateHash"`
	ExitStateHash        string                             `json:"exitStateHash"`
	TransitionHash       string                             `json:"transitionHash"`
	ContractReview       videoproduction.ShotContractReview `json:"contractReview"`
}

type PlanStoryboardSceneOutput struct {
	ScenePlanID      string                  `json:"scenePlanId"`
	StoryboardPlanID string                  `json:"storyboardPlanId"`
	SceneKey         string                  `json:"sceneKey"`
	SceneOrdinal     int                     `json:"sceneOrdinal"`
	RetryGeneration  int                     `json:"retryGeneration"`
	Status           string                  `json:"status"`
	Shots            []PlannedStoryboardShot `json:"shots"`
	ProviderCallID   string                  `json:"providerCallId,omitempty"`
	ModelID          string                  `json:"modelId,omitempty"`
	PromptVersionID  string                  `json:"promptVersionId,omitempty"`
	PromptHash       string                  `json:"promptHash,omitempty"`
}

type scenePlanningRecord struct {
	ScenePlanID      string
	SceneKey         string
	SceneOrdinal     int
	ScriptSceneID    string
	StartTick        int64
	EndTick          int64
	Status           string
	RetryGeneration  int
	EntryState       json.RawMessage
	ExitState        json.RawMessage
	Blueprint        storyboardpkg.ContinuityBlueprintOutput
	TimingAnalysisID string
	EpisodeIndex     int
	EpisodeTitle     string
}

type sceneTimingUnitRecord struct {
	Unit              storyboardpkg.TimingUnit
	SourceStartOffset *int
	SourceEndOffset   *int
	MinimumTicks      int64
	MaximumTicks      int64
	Confidence        float64
	BlockOrdinal      int
}

func (a Activities) PlanStoryboardScene(ctx context.Context, input PlanStoryboardSceneInput) (PlanStoryboardSceneOutput, error) {
	if strings.TrimSpace(input.ScenePlanID) == "" || strings.TrimSpace(input.StoryboardPlanID) == "" || strings.TrimSpace(input.SceneKey) == "" {
		return PlanStoryboardSceneOutput{}, fmt.Errorf("scenePlanId, storyboardPlanId, and sceneKey are required")
	}
	nodeKey := storyboardScenePlanNodeKey(input.ScenePlanID, input.RetryGeneration)
	if existing, ok, err := a.existingScenePlanningOutput(ctx, input.WorkflowRunID, nodeKey); err != nil {
		return PlanStoryboardSceneOutput{}, err
	} else if ok {
		return existing, nil
	}
	record, err := a.loadScenePlanningRecord(ctx, input)
	if err != nil {
		return PlanStoryboardSceneOutput{}, err
	}
	if record.SceneKey != input.SceneKey || record.SceneOrdinal != input.SceneOrdinal {
		return PlanStoryboardSceneOutput{}, fmt.Errorf("scene plan identity does not match workflow input")
	}
	units, blocks, err := a.loadSceneTimingUnits(ctx, record.TimingAnalysisID, input.SceneKey)
	if err != nil {
		return PlanStoryboardSceneOutput{}, err
	}
	if len(units) == 0 || len(blocks) == 0 {
		return PlanStoryboardSceneOutput{}, fmt.Errorf("scene has no timing units")
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID, input.WorkflowRunID)
	if err != nil {
		return PlanStoryboardSceneOutput{}, err
	}
	assets, err := a.listCanonicalAssets(ctx, input.ProjectID)
	if err != nil {
		return PlanStoryboardSceneOutput{}, err
	}
	blueprintScene, ok := continuityBlueprintScene(record.Blueprint, input.SceneKey)
	if !ok {
		return PlanStoryboardSceneOutput{}, fmt.Errorf("continuity blueprint does not contain scene %s", input.SceneKey)
	}
	timebase := storyboardpkg.Timebase{TicksPerSecond: project.TimelineTimebase, FPSNumerator: int64(project.FPSNumerator), FPSDenominator: int64(project.FPSDenominator)}
	pacing := storyboardpkg.PacingProfileByKey(blueprintScene.PacingProfile, timebase)
	calibration := a.timingCalibrationParameters(ctx, input.ProjectID)
	pacing = storyboardpkg.ScalePacingProfile(pacing, calibration.ShotPacingScale, timebase)
	semanticMinimum := blueprintScene.SuggestedShotMinimum
	if semanticMinimum < 1 {
		semanticMinimum = 1
	}
	shots, err := storyboardpkg.PlanShotBoundaries(blocks, storyboardpkg.PlanOptions{
		Timebase:        timebase,
		Pacing:          pacing,
		OneTake:         blueprintScene.OneTake,
		SemanticMinimum: semanticMinimum,
		UserShotBudget:  input.SceneShotBudget,
	})
	if err != nil {
		return PlanStoryboardSceneOutput{}, a.failStoryboardSceneActivity(ctx, input, NodeExecution{}, "STORYBOARD_PLAN_INVALID", err.Error())
	}
	contextJSON := mustJSON(map[string]any{
		"project": map[string]any{
			"videoRatio":     project.VideoRatio,
			"artStyle":       project.ArtStyle,
			"directorManual": project.DirectorManual,
			"visualManual":   project.VisualManual,
		},
		"scene": map[string]any{
			"sceneKey":      input.SceneKey,
			"scriptSceneId": record.ScriptSceneID,
			"sceneOrdinal":  input.SceneOrdinal,
			"startTick":     record.StartTick,
			"endTick":       record.EndTick,
			"entryState":    json.RawMessage(record.EntryState),
			"exitState":     json.RawMessage(record.ExitState),
			"blueprint":     blueprintScene,
		},
		"expectedShotCount": len(shots),
		"shotSlots":         sceneShotSlotsForPrompt(shots, units),
		"assets":            assets,
		"corrections":       input.CorrectionHints,
	})
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyStoryboardScenePlanner, map[string]any{
		"context": map[string]any{"json": string(contextJSON)},
	})
	if err != nil {
		return PlanStoryboardSceneOutput{}, err
	}
	rendered, err = applyProfileStoryboardPlannerContract(rendered, project.VideoProductionProfileKey)
	if err != nil {
		return PlanStoryboardSceneOutput{}, err
	}
	nodeExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKey,
		NodeType:       "agent.storyboard_scene_plan",
		Input: mustJSON(map[string]any{
			"storyboardPlanId": input.StoryboardPlanID,
			"scenePlanId":      input.ScenePlanID,
			"sceneKey":         input.SceneKey,
			"retryGeneration":  input.RetryGeneration,
			"modelProfileKey":  project.ScriptModelProfileKey,
			"promptVersionId":  rendered.PromptVersionID,
			"promptHash":       rendered.RenderedHash,
		}),
	})
	if err != nil {
		return PlanStoryboardSceneOutput{}, err
	}
	if err := a.markScenePlanRunning(ctx, input, nodeExecution); err != nil {
		return PlanStoryboardSceneOutput{}, a.failStoryboardSceneActivity(ctx, input, nodeExecution, codeActivityFailed, err.Error())
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, project.ScriptModelProfileKey, []string{"text", "multimodal"}); err != nil {
		return PlanStoryboardSceneOutput{}, a.failStoryboardSceneActivity(ctx, input, nodeExecution, codeActivityFailed, err.Error())
	}
	if a.gateway == nil {
		return PlanStoryboardSceneOutput{}, a.failStoryboardSceneActivity(ctx, input, nodeExecution, provider.CodeProviderGatewayRequired, "provider gateway client is not configured")
	}
	gatewayResp, err := a.generateProviderText(ctx, nodeExecution, provider.GatewayTextRequest{
		OrganizationID:    input.OrganizationID,
		ProjectID:         input.ProjectID,
		WorkflowRunID:     input.WorkflowRunID,
		NodeRunID:         nodeExecution.NodeRunID,
		ModelProfileKey:   project.ScriptModelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		Input:             mustJSON(map[string]any{"prompt": rendered.RenderedText, "responseFormat": "json", "maxOutputTokens": 16_000}),
		Options:           providerTextGatewayOptions(),
	})
	if err != nil {
		workflowErr := workflowErrorFromProvider(err, codeActivityFailed)
		code, message := workflowErrorFields(workflowErr, codeActivityFailed)
		return PlanStoryboardSceneOutput{}, a.failStoryboardSceneActivity(ctx, input, nodeExecution, code, message)
	}
	validUnitIDs := make([]string, 0, len(units))
	for _, unit := range units {
		validUnitIDs = append(validUnitIDs, unit.Unit.ID)
	}
	plannerOutput, err := storyboardpkg.DecodeShotPlannerOutput(stripJSONFence(gatewayResp.Output.Text), validUnitIDs)
	if err != nil {
		return PlanStoryboardSceneOutput{}, a.failStoryboardSceneActivity(ctx, input, nodeExecution, provider.CodeInvalidRequest, err.Error())
	}
	plannerOutput, err = alignPlannerOutputToShotSlots(plannerOutput, shots)
	if err != nil {
		return PlanStoryboardSceneOutput{}, a.failStoryboardSceneActivity(ctx, input, nodeExecution, provider.CodeInvalidRequest, err.Error())
	}
	if err := storyboardpkg.ValidateShotPlannerOutput(plannerOutput, input.SceneKey, validUnitIDs); err != nil {
		return PlanStoryboardSceneOutput{}, a.failStoryboardSceneActivity(ctx, input, nodeExecution, provider.CodeInvalidRequest, err.Error())
	}
	plannerOutput = filterUnknownPlannerAssetReferences(plannerOutput, assets)
	if err := validateShotPlannerAssetReferences(plannerOutput, assets); err != nil {
		return PlanStoryboardSceneOutput{}, a.failStoryboardSceneActivity(ctx, input, nodeExecution, provider.CodeInvalidRequest, err.Error())
	}
	if err := validateShotPlannerImageDialogueIsolation(plannerOutput, units); err != nil {
		return PlanStoryboardSceneOutput{}, a.failStoryboardSceneActivity(ctx, input, nodeExecution, provider.CodeInvalidRequest, err.Error())
	}
	if err := storyboardpkg.ValidateShotStatePlannerOutput(plannerOutput); err != nil {
		return PlanStoryboardSceneOutput{}, a.failStoryboardSceneActivity(ctx, input, nodeExecution, provider.CodeInvalidRequest, err.Error())
	}
	planned := materializeSceneShotCreatives(shots, units, plannerOutput)
	planned, err = compileProfilePlannedShotContracts(
		planned, plannerOutput, assets, project.VideoProductionProfileKey, project.TimelineTimebase,
	)
	if err != nil {
		return PlanStoryboardSceneOutput{}, a.failStoryboardSceneActivity(ctx, input, nodeExecution, provider.CodeInvalidRequest, err.Error())
	}
	output, err := a.storeStoryboardScenePlan(ctx, input, record, project.VideoProductionProfileKey, nodeExecution, rendered.PromptVersionID, rendered.RenderedHash, gatewayResp, plannerOutput, planned)
	if err != nil {
		return PlanStoryboardSceneOutput{}, a.failStoryboardSceneActivity(ctx, input, nodeExecution, codeActivityFailed, err.Error())
	}
	return output, nil
}

func storyboardScenePlanNodeKey(scenePlanID string, retryGeneration int) string {
	return fmt.Sprintf("%s_retry_%d", nodeKeyForID(nodePlanStoryboardScenePrefix, scenePlanID), retryGeneration)
}

func (a Activities) loadScenePlanningRecord(ctx context.Context, input PlanStoryboardSceneInput) (scenePlanningRecord, error) {
	var record scenePlanningRecord
	var blueprintRaw json.RawMessage
	var scriptSceneID sql.NullString
	err := a.db.QueryRow(ctx, `
		SELECT scene_plan.id::text, scene_plan.scene_key, scene_plan.scene_ordinal,
		       scene_plan.script_scene_id::text, scene_plan.start_tick, scene_plan.end_tick,
		       scene_plan.status, scene_plan.retry_generation, scene_plan.entry_state, scene_plan.exit_state,
		       blueprint.blueprint, plan.timing_analysis_id::text,
		       episode.episode_index, episode.episode_title
		FROM storyboard_scene_plans scene_plan
		JOIN storyboard_plans plan ON plan.id = scene_plan.storyboard_plan_id
		JOIN episode_continuity_blueprints blueprint ON blueprint.id = scene_plan.blueprint_id
		JOIN script_episodes episode ON episode.id = plan.script_episode_id
		WHERE scene_plan.project_id = $1
		  AND scene_plan.storyboard_plan_id = $2
		  AND scene_plan.id = $3
	`, input.ProjectID, input.StoryboardPlanID, input.ScenePlanID).Scan(
		&record.ScenePlanID, &record.SceneKey, &record.SceneOrdinal, &scriptSceneID,
		&record.StartTick, &record.EndTick, &record.Status, &record.RetryGeneration,
		&record.EntryState, &record.ExitState, &blueprintRaw, &record.TimingAnalysisID,
		&record.EpisodeIndex, &record.EpisodeTitle,
	)
	if err != nil {
		return scenePlanningRecord{}, err
	}
	if scriptSceneID.Valid {
		record.ScriptSceneID = scriptSceneID.String
	}
	if err := json.Unmarshal(blueprintRaw, &record.Blueprint); err != nil {
		return scenePlanningRecord{}, err
	}
	return record, nil
}

func (a Activities) loadSceneTimingUnits(ctx context.Context, analysisID, sceneKey string) ([]sceneTimingUnitRecord, []storyboardpkg.TimingBlock, error) {
	rows, err := a.db.Query(ctx, `
		SELECT id::text, unit_ordinal, unit_type, track, COALESCE(parallel_group, ''),
		       COALESCE(speaker, ''), source_text, COALESCE(delivery, ''),
		       source_start_offset, source_end_offset, start_tick, end_tick,
		       COALESCE(min_duration_ticks, duration_ticks), COALESCE(max_duration_ticks, duration_ticks),
		       duration_source, COALESCE(confidence, 0)::float8,
		       COALESCE((metadata->>'blockOrdinal')::int, unit_ordinal),
		       COALESCE((metadata->>'forceBoundaryBefore')::boolean, false),
		       COALESCE((metadata->>'forceBoundaryAfter')::boolean, false)
		FROM script_timing_units
		WHERE timing_analysis_id = $1 AND metadata->>'sceneKey' = $2
		ORDER BY unit_ordinal
	`, analysisID, sceneKey)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	units := make([]sceneTimingUnitRecord, 0)
	for rows.Next() {
		var record sceneTimingUnitRecord
		var startOffset, endOffset sql.NullInt64
		if err := rows.Scan(
			&record.Unit.ID, &record.Unit.Ordinal, &record.Unit.Type, &record.Unit.Track,
			&record.Unit.ParallelGroup, &record.Unit.Speaker, &record.Unit.SourceText, &record.Unit.Delivery,
			&startOffset, &endOffset, &record.Unit.StartTick, &record.Unit.EndTick,
			&record.MinimumTicks, &record.MaximumTicks, &record.Unit.DurationSource,
			&record.Confidence, &record.BlockOrdinal, &record.Unit.ForceBoundaryBefore, &record.Unit.ForceBoundaryAfter,
		); err != nil {
			return nil, nil, err
		}
		record.Unit.SceneID = sceneKey
		record.Unit.DurationTicks = record.Unit.EndTick - record.Unit.StartTick
		if startOffset.Valid {
			value := int(startOffset.Int64)
			record.SourceStartOffset = &value
		}
		if endOffset.Valid {
			value := int(endOffset.Int64)
			record.SourceEndOffset = &value
		}
		units = append(units, record)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	blocks := timingBlocksFromStoredUnits(sceneKey, units)
	return units, blocks, nil
}

func timingBlocksFromStoredUnits(sceneKey string, units []sceneTimingUnitRecord) []storyboardpkg.TimingBlock {
	byOrdinal := map[int][]storyboardpkg.TimingUnit{}
	ordinals := make([]int, 0)
	seen := map[int]bool{}
	for _, record := range units {
		byOrdinal[record.BlockOrdinal] = append(byOrdinal[record.BlockOrdinal], record.Unit)
		if !seen[record.BlockOrdinal] {
			seen[record.BlockOrdinal] = true
			ordinals = append(ordinals, record.BlockOrdinal)
		}
	}
	sort.Ints(ordinals)
	blocks := make([]storyboardpkg.TimingBlock, 0, len(ordinals))
	for _, ordinal := range ordinals {
		blockUnits := byOrdinal[ordinal]
		start, end := blockUnits[0].StartTick, blockUnits[0].EndTick
		for _, unit := range blockUnits[1:] {
			if unit.StartTick < start {
				start = unit.StartTick
			}
			if unit.EndTick > end {
				end = unit.EndTick
			}
		}
		blocks = append(blocks, storyboardpkg.TimingBlock{
			ID:            fmt.Sprintf("%s:block-%d", sceneKey, ordinal),
			SceneID:       sceneKey,
			Ordinal:       ordinal,
			ParallelGroup: blockUnits[0].ParallelGroup,
			StartTick:     start,
			EndTick:       end,
			DurationTicks: end - start,
			Units:         blockUnits,
		})
	}
	return blocks
}

func sceneShotSlotsForPrompt(shots []storyboardpkg.ShotDraft, units []sceneTimingUnitRecord) []map[string]any {
	unitByID := make(map[string]sceneTimingUnitRecord, len(units))
	for _, unit := range units {
		unitByID[unit.Unit.ID] = unit
	}
	items := make([]map[string]any, 0, len(shots))
	for _, shot := range shots {
		spans := make([]map[string]any, 0, len(shot.Spans))
		for _, span := range shot.Spans {
			record := unitByID[span.TimingUnitID]
			spans = append(spans, map[string]any{
				"timingUnitId":          span.TimingUnitID,
				"type":                  record.Unit.Type,
				"track":                 record.Unit.Track,
				"speaker":               record.Unit.Speaker,
				"sourceText":            record.Unit.SourceText,
				"delivery":              record.Unit.Delivery,
				"spanStartTick":         span.StartTick,
				"spanEndTick":           span.EndTick,
				"continuesFromPrevious": span.StartTick > record.Unit.StartTick,
				"continuesToNext":       span.EndTick < record.Unit.EndTick,
			})
		}
		items = append(items, map[string]any{
			"slotKey":       storyboardShotSlotKey(shot.Ordinal),
			"shotOrdinal":   shot.Ordinal,
			"startTick":     shot.StartTick,
			"endTick":       shot.EndTick,
			"durationTicks": shot.DurationTicks,
			"oneTake":       shot.OneTake,
			"timingSpans":   spans,
		})
	}
	return items
}

func storyboardShotSlotKey(ordinal int) string {
	return fmt.Sprintf("slot_%03d", ordinal+1)
}

func alignPlannerOutputToShotSlots(output storyboardpkg.ShotPlannerOutput, shots []storyboardpkg.ShotDraft) (storyboardpkg.ShotPlannerOutput, error) {
	if len(output.Shots) != len(shots) {
		return storyboardpkg.ShotPlannerOutput{}, fmt.Errorf("%w: expected %d shot creatives, got %d", storyboardpkg.ErrInvalidShotPlannerOutput, len(shots), len(output.Shots))
	}
	byKey := make(map[string]storyboardpkg.ShotPlannerSuggestion, len(output.Shots))
	for _, suggestion := range output.Shots {
		byKey[strings.TrimSpace(suggestion.SuggestionKey)] = suggestion
	}
	allKeysPresent := true
	for _, shot := range shots {
		if _, ok := byKey[storyboardShotSlotKey(shot.Ordinal)]; !ok {
			allKeysPresent = false
			break
		}
	}
	aligned := make([]storyboardpkg.ShotPlannerSuggestion, 0, len(shots))
	visualOwner := map[string]string{}
	for index, shot := range shots {
		key := storyboardShotSlotKey(shot.Ordinal)
		suggestion := output.Shots[index]
		if allKeysPresent {
			suggestion = byKey[key]
		}
		suggestion.SuggestionKey = key
		suggestion.CutAfterTimingUnitID = ""
		suggestion.TimingUnitIDs = timingUnitIDsForShot(shot)
		visualKey := normalizedShotCreative(suggestion.Visual)
		if visualKey == "" {
			return storyboardpkg.ShotPlannerOutput{}, fmt.Errorf("%w: shot slot %s has no visible composition", storyboardpkg.ErrInvalidShotPlannerOutput, key)
		}
		if previous, exists := visualOwner[visualKey]; exists {
			return storyboardpkg.ShotPlannerOutput{}, fmt.Errorf("%w: shot slots %s and %s contain the same visual; every real shot needs a distinct composition or action phase", storyboardpkg.ErrInvalidShotPlannerOutput, previous, key)
		}
		visualOwner[visualKey] = key
		aligned = append(aligned, suggestion)
	}
	output.Shots = aligned
	return output, nil
}

func timingUnitIDsForShot(shot storyboardpkg.ShotDraft) []string {
	ids := make([]string, 0, len(shot.Spans))
	seen := map[string]bool{}
	for _, span := range shot.Spans {
		if !seen[span.TimingUnitID] {
			seen[span.TimingUnitID] = true
			ids = append(ids, span.TimingUnitID)
		}
	}
	return ids
}

func normalizedShotCreative(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), ""))
}

func materializeSceneShotCreatives(
	shots []storyboardpkg.ShotDraft,
	units []sceneTimingUnitRecord,
	planner storyboardpkg.ShotPlannerOutput,
) []PlannedStoryboardShot {
	unitByID := make(map[string]sceneTimingUnitRecord, len(units))
	for _, unit := range units {
		unitByID[unit.Unit.ID] = unit
	}
	result := make([]PlannedStoryboardShot, 0, len(shots))
	for _, shot := range shots {
		suggestion := plannerSuggestionForShotSlot(shot, planner.Shots)
		visual := strings.TrimSpace(suggestion.Visual)
		if visual == "" {
			visual = fallbackShotVisual(shot, unitByID)
		}
		result = append(result, PlannedStoryboardShot{
			ShotOrdinal:          shot.Ordinal,
			StartTick:            shot.StartTick,
			EndTick:              shot.EndTick,
			DurationTicks:        shot.DurationTicks,
			Title:                suggestion.Title,
			Visual:               visual,
			Camera:               suggestion.Camera,
			Motion:               suggestion.Motion,
			Mood:                 suggestion.Mood,
			OneTake:              shot.OneTake || suggestion.OneTake,
			TimingSpans:          shot.Spans,
			ScriptDialogue:       dialogueForShot(shot, unitByID),
			ImagePromptDirection: strings.TrimSpace(suggestion.ImagePromptDirection),
			VideoPromptDirection: strings.TrimSpace(suggestion.VideoPromptDirection),
			AssetRequirements:    plannerShotAssetRequirements(suggestion.AssetRequirements),
			PlannedEntryState:    suggestion.PlannedEntryState,
			PlannedExitState:     suggestion.PlannedExitState,
		})
	}
	return result
}

func plannerSuggestionForShotSlot(shot storyboardpkg.ShotDraft, suggestions []storyboardpkg.ShotPlannerSuggestion) storyboardpkg.ShotPlannerSuggestion {
	key := storyboardShotSlotKey(shot.Ordinal)
	for _, suggestion := range suggestions {
		if suggestion.SuggestionKey == key {
			return suggestion
		}
	}
	return storyboardpkg.ShotPlannerSuggestion{}
}

func fallbackShotVisual(shot storyboardpkg.ShotDraft, units map[string]sceneTimingUnitRecord) string {
	parts := make([]string, 0)
	for _, span := range shot.Spans {
		if text := strings.TrimSpace(units[span.TimingUnitID].Unit.SourceText); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

func dialogueForShot(shot storyboardpkg.ShotDraft, units map[string]sceneTimingUnitRecord) []StoryboardDialogueLine {
	lines := make([]StoryboardDialogueLine, 0)
	for _, span := range shot.Spans {
		record, ok := units[span.TimingUnitID]
		if !ok || !isSpeechUnitType(record.Unit.Type) {
			continue
		}
		line := StoryboardDialogueLine{
			TimingUnitID:          record.Unit.ID,
			Speaker:               record.Unit.Speaker,
			Text:                  record.Unit.SourceText,
			Delivery:              record.Unit.Delivery,
			Kind:                  string(record.Unit.Type),
			SpanStartTick:         span.StartTick,
			SpanEndTick:           span.EndTick,
			SourceStartOffset:     record.SourceStartOffset,
			SourceEndOffset:       record.SourceEndOffset,
			ContinuesFromPrevious: span.StartTick > record.Unit.StartTick,
			ContinuesToNext:       span.EndTick < record.Unit.EndTick,
		}
		lines = append(lines, storyboardDialogueLineForTimingSpan(
			line, record.Unit.SourceText, record.Unit.StartTick, record.Unit.EndTick,
		))
	}
	return lines
}

func isSpeechUnitType(value storyboardpkg.TimingUnitType) bool {
	return value == storyboardpkg.UnitDialogue || value == storyboardpkg.UnitVoiceover || value == storyboardpkg.UnitNarration || value == storyboardpkg.UnitSystem
}

func (a Activities) storeStoryboardScenePlan(
	ctx context.Context,
	input PlanStoryboardSceneInput,
	record scenePlanningRecord,
	profileKey string,
	nodeExecution NodeExecution,
	promptVersionID, promptHash string,
	gatewayResp provider.GatewayTextResponse,
	plannerOutput storyboardpkg.ShotPlannerOutput,
	shots []PlannedStoryboardShot,
) (PlanStoryboardSceneOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return PlanStoryboardSceneOutput{}, err
	}
	defer tx.Rollback(ctx)
	runCtx, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, nodeExecution)
	if err != nil {
		return PlanStoryboardSceneOutput{}, err
	}
	var currentStatus string
	var currentRetry int
	if err := tx.QueryRow(ctx, `
		SELECT status, retry_generation
		FROM storyboard_scene_plans
		WHERE id = $1 AND storyboard_plan_id = $2
		FOR UPDATE
	`, input.ScenePlanID, input.StoryboardPlanID).Scan(&currentStatus, &currentRetry); err != nil {
		return PlanStoryboardSceneOutput{}, err
	}
	if currentStatus == "ready" && currentRetry > input.RetryGeneration {
		return PlanStoryboardSceneOutput{}, fmt.Errorf("scene plan has a newer completed retry generation")
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM storyboard_shots
		WHERE storyboard_plan_id = $1 AND metadata->>'sceneKey' = $2
		  AND production_generation_id = $3
	`, input.StoryboardPlanID, input.SceneKey, runCtx.ProductionGenerationID); err != nil {
		return PlanStoryboardSceneOutput{}, err
	}
	previousShotID := ""
	for shotIndex := range shots {
		shot := &shots[shotIndex]
		var shotID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO storyboard_shots(
				organization_id, project_id, workflow_run_id, script_id, script_version_id,
				script_episode_id, episode_index, episode_shot_index, storyboard_plan_id, script_scene_id,
				shot_index, shot_no, title, visual, camera, motion, mood, script_dialogue,
				start_tick, end_tick, duration_min_ticks, duration_max_ticks, duration_source,
				timing_confidence, duration_locked, one_take, timing_revision,
				status, review_status, stale_state, metadata, production_generation_id
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, NULL, $8, NULLIF($9, '')::uuid,
			        $10, NULL, NULLIF($11, ''), $12, NULLIF($13, ''), NULLIF($14, ''), NULLIF($15, ''), $16,
			        $17, $18, $19, $19, 'rule_estimated', $20, false, $21, 1,
			        'storyboard_ready', 'pending', 'fresh', $22, $23)
			RETURNING id::text
		`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, input.ScriptID, input.ScriptVersionID,
			input.ScriptEpisodeID, record.EpisodeIndex, input.StoryboardPlanID, record.ScriptSceneID,
			record.SceneOrdinal*10_000+shot.ShotOrdinal,
			shot.Title, shot.Visual, shot.Camera, shot.Motion, shot.Mood, mustJSON(shot.ScriptDialogue),
			shot.StartTick, shot.EndTick, shot.DurationTicks, sceneShotTimingConfidence(*shot, record),
			shot.OneTake, mustJSON(map[string]any{
				"scenePlanId":            input.ScenePlanID,
				"sceneKey":               input.SceneKey,
				"sceneOrdinal":           input.SceneOrdinal,
				"planningShotOrdinal":    shot.ShotOrdinal,
				"retryGeneration":        input.RetryGeneration,
				"plannerPromptVersionId": promptVersionID,
				"plannerPromptHash":      promptHash,
				"plannerProviderCallId":  gatewayResp.ProviderCallID,
				"imagePromptDirection":   shot.ImagePromptDirection,
				"videoPromptDirection":   shot.VideoPromptDirection,
			}), runCtx.ProductionGenerationID).Scan(&shotID); err != nil {
			return PlanStoryboardSceneOutput{}, err
		}
		shot.ID = shotID
		if err := a.storeProfileShotContractsTx(ctx, tx, input, record, runCtx, nodeExecution, gatewayResp, promptVersionID, profileKey, previousShotID, *shot); err != nil {
			return PlanStoryboardSceneOutput{}, err
		}
		previousShotID = shotID
		for spanOrdinal, span := range shot.TimingSpans {
			if _, err := tx.Exec(ctx, `
				INSERT INTO storyboard_shot_timing_spans(
					storyboard_plan_id, storyboard_shot_id, timing_unit_id,
					span_start_tick, span_end_tick, ordinal, metadata
				)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
			`, input.StoryboardPlanID, shotID, span.TimingUnitID, span.StartTick, span.EndTick, spanOrdinal,
				mustJSON(map[string]any{"sceneKey": input.SceneKey})); err != nil {
				return PlanStoryboardSceneOutput{}, err
			}
		}
		for _, requirement := range shot.AssetRequirements {
			if _, err := tx.Exec(ctx, `
				INSERT INTO shot_asset_requirements(
					organization_id, project_id, workflow_run_id, storyboard_shot_id, asset_id,
					requirement_type, role_in_shot, costume, pose, expression, action,
					camera_relation, scene_state, prop_state, status, stale_state, metadata,
					production_generation_id
				)
				VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), NULLIF($8, ''),
				        NULLIF($9, ''), NULLIF($10, ''), NULLIF($11, ''), NULLIF($12, ''),
				        NULLIF($13, ''), NULLIF($14, ''), 'pending', 'fresh', $15, $16)
			`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, shotID, requirement.AssetID,
				requirement.RequirementType, requirement.RoleInShot, requirement.Costume,
				requirement.Pose, requirement.Expression, requirement.Action,
				requirement.CameraRelation, requirement.SceneState, requirement.PropState,
				mustJSON(map[string]any{
					"source": "storyboard_scene_planner", "sceneKey": input.SceneKey,
					"retryGeneration": input.RetryGeneration, "assetName": requirement.AssetName,
					"assetType": requirement.AssetType,
				}), runCtx.ProductionGenerationID); err != nil {
				return PlanStoryboardSceneOutput{}, err
			}
		}
	}
	output := PlanStoryboardSceneOutput{
		ScenePlanID:      input.ScenePlanID,
		StoryboardPlanID: input.StoryboardPlanID,
		SceneKey:         input.SceneKey,
		SceneOrdinal:     input.SceneOrdinal,
		RetryGeneration:  input.RetryGeneration,
		Status:           "ready",
		Shots:            shots,
		ProviderCallID:   gatewayResp.ProviderCallID,
		ModelID:          gatewayResp.ModelID,
		PromptVersionID:  promptVersionID,
		PromptHash:       promptHash,
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_scene_plans
		SET status = 'ready',
		    retry_generation = $2,
		    shot_count = $3,
		    planner_output = $4,
		    prompt_version_id = NULLIF($5, '')::uuid,
		    prompt_hash = NULLIF($6, ''),
		    provider_call_id = NULLIF($7, '')::uuid,
		    model_id = NULLIF($8, '')::uuid,
		    error_code = NULL,
		    error_message = NULL,
		    completed_at = now(),
		    metadata = metadata || jsonb_build_object('activityOutput', $9::jsonb, 'nodeRunId', $10::text)
		WHERE id = $1
	`, input.ScenePlanID, input.RetryGeneration, len(shots), mustJSON(plannerOutput), promptVersionID,
		promptHash, gatewayResp.ProviderCallID, gatewayResp.ModelID, mustJSON(output), nodeExecution.NodeRunID); err != nil {
		return PlanStoryboardSceneOutput{}, err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.scene.planning.completed", "storyboard_scene_plan", input.ScenePlanID, mustJSON(map[string]any{
		"workflowRunId":    input.WorkflowRunID,
		"storyboardPlanId": input.StoryboardPlanID,
		"scenePlanId":      input.ScenePlanID,
		"sceneKey":         input.SceneKey,
		"sceneOrdinal":     input.SceneOrdinal,
		"retryGeneration":  input.RetryGeneration,
		"shotCount":        len(shots),
		"shots":            shots,
	})); err != nil {
		return PlanStoryboardSceneOutput{}, err
	}
	if _, err := completeNodeRunTx(ctx, tx, nodeExecution, mustJSON(output)); err != nil {
		return PlanStoryboardSceneOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PlanStoryboardSceneOutput{}, err
	}
	return output, nil
}

func (a Activities) markScenePlanRunning(ctx context.Context, input PlanStoryboardSceneInput, nodeExecution NodeExecution) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, nodeExecution); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_scene_plans
		SET status = 'planning', retry_generation = GREATEST(retry_generation, $2),
		    error_code = NULL, error_message = NULL, completed_at = NULL,
		    metadata = metadata || jsonb_build_object('nodeRunId', $3::text)
		WHERE id = $1
	`, input.ScenePlanID, input.RetryGeneration, nodeExecution.NodeRunID); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.scene.planning.started", "storyboard_scene_plan", input.ScenePlanID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "storyboardPlanId": input.StoryboardPlanID,
		"sceneKey": input.SceneKey, "sceneOrdinal": input.SceneOrdinal, "retryGeneration": input.RetryGeneration,
	})); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) failStoryboardSceneActivity(ctx context.Context, input PlanStoryboardSceneInput, nodeExecution NodeExecution, code, message string) error {
	if strings.Contains(message, workflowWriteFenceMessage) {
		return discardWorkflowResult(ctx, a.db, nodeExecution, message)
	}
	if nodeExecution.valid() {
		tx, err := a.db.Begin(ctx)
		if err == nil {
			defer tx.Rollback(ctx)
			if _, err = lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, nodeExecution); err == nil {
				_, err = tx.Exec(ctx, `
			UPDATE storyboard_scene_plans
			SET status = 'failed', error_code = $2, error_message = $3, completed_at = now()
			WHERE id = $1
		`, input.ScenePlanID, code, message)
				if err == nil {
					err = insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.scene.planning.failed", "storyboard_scene_plan", input.ScenePlanID, mustJSON(map[string]any{
						"workflowRunId": input.WorkflowRunID, "storyboardPlanId": input.StoryboardPlanID,
						"sceneKey": input.SceneKey, "retryGeneration": input.RetryGeneration, "code": code, "message": message,
					}))
				}
				if err == nil {
					_, err = failNodeRunTx(ctx, tx, nodeExecution, code, message, mustJSON(map[string]any{
						"scenePlanId": input.ScenePlanID, "code": code, "message": message,
					}))
				}
				if err == nil {
					err = tx.Commit(ctx)
				}
			}
		}
	}
	return temporal.NewApplicationError(message, code)
}

func (a Activities) existingScenePlanningOutput(ctx context.Context, workflowRunID, nodeKey string) (PlanStoryboardSceneOutput, bool, error) {
	var raw json.RawMessage
	err := a.db.QueryRow(ctx, `
		SELECT output FROM workflow_node_runs
		WHERE workflow_run_id = $1 AND node_key = $2 AND status = 'succeeded'
	`, workflowRunID, nodeKey).Scan(&raw)
	if err == pgx.ErrNoRows {
		return PlanStoryboardSceneOutput{}, false, nil
	}
	if err != nil {
		return PlanStoryboardSceneOutput{}, false, err
	}
	var output PlanStoryboardSceneOutput
	if err := json.Unmarshal(raw, &output); err != nil {
		return PlanStoryboardSceneOutput{}, false, err
	}
	return output, output.ScenePlanID != "" && output.Status == "ready", nil
}

func continuityBlueprintScene(blueprint storyboardpkg.ContinuityBlueprintOutput, sceneKey string) (storyboardpkg.ContinuityBlueprintScene, bool) {
	for _, scene := range blueprint.Scenes {
		if scene.SceneKey == sceneKey {
			return scene, true
		}
	}
	return storyboardpkg.ContinuityBlueprintScene{}, false
}

func sceneShotTimingConfidence(_ PlannedStoryboardShot, _ scenePlanningRecord) float64 {
	return 0.8
}

func plannerShotAssetRequirements(requirements []storyboardpkg.ShotPlannerAssetRequirement) []ShotAssetRequirementRecord {
	result := make([]ShotAssetRequirementRecord, 0, len(requirements))
	for _, requirement := range requirements {
		result = append(result, ShotAssetRequirementRecord{
			AssetID: strings.TrimSpace(requirement.AssetID), RequirementType: strings.TrimSpace(requirement.RequirementType),
			RoleInShot: strings.TrimSpace(requirement.RoleInShot), Costume: strings.TrimSpace(requirement.Costume),
			Pose: strings.TrimSpace(requirement.Pose), Expression: strings.TrimSpace(requirement.Expression),
			Action: strings.TrimSpace(requirement.Action), CameraRelation: strings.TrimSpace(requirement.CameraRelation),
			SceneState: strings.TrimSpace(requirement.SceneState), PropState: strings.TrimSpace(requirement.PropState),
		})
	}
	return result
}

func validateShotPlannerAssetReferences(output storyboardpkg.ShotPlannerOutput, assets []CanonicalAssetRecord) error {
	known := make(map[string]CanonicalAssetRecord, len(assets))
	for _, asset := range assets {
		known[asset.ID] = asset
	}
	for shotIndex := range output.Shots {
		for requirementIndex := range output.Shots[shotIndex].AssetRequirements {
			requirement := &output.Shots[shotIndex].AssetRequirements[requirementIndex]
			if _, ok := known[strings.TrimSpace(requirement.AssetID)]; !ok {
				return fmt.Errorf("shot suggestion %s references an unknown canonical asset %s", output.Shots[shotIndex].SuggestionKey, requirement.AssetID)
			}
		}
	}
	return nil
}

func filterUnknownPlannerAssetReferences(output storyboardpkg.ShotPlannerOutput, assets []CanonicalAssetRecord) storyboardpkg.ShotPlannerOutput {
	known := make(map[string]bool, len(assets))
	for _, asset := range assets {
		known[asset.ID] = true
	}
	for shotIndex := range output.Shots {
		filtered := make([]storyboardpkg.ShotPlannerAssetRequirement, 0, len(output.Shots[shotIndex].AssetRequirements))
		for _, requirement := range output.Shots[shotIndex].AssetRequirements {
			if known[requirement.AssetID] {
				filtered = append(filtered, requirement)
			}
		}
		output.Shots[shotIndex].AssetRequirements = filtered
	}
	return output
}

func validateShotPlannerImageDialogueIsolation(output storyboardpkg.ShotPlannerOutput, units []sceneTimingUnitRecord) error {
	dialogue := make([]string, 0)
	for _, unit := range units {
		if !isSpeechUnitType(unit.Unit.Type) {
			continue
		}
		text := strings.TrimSpace(unit.Unit.SourceText)
		if len([]rune(text)) >= 2 {
			dialogue = append(dialogue, text)
		}
	}
	for _, suggestion := range output.Shots {
		visual := strings.TrimSpace(suggestion.Visual)
		imageDirection := strings.TrimSpace(suggestion.ImagePromptDirection)
		for _, line := range dialogue {
			if strings.Contains(visual, line) || strings.Contains(imageDirection, line) {
				return fmt.Errorf("shot suggestion %s leaks script dialogue into a still-image field", suggestion.SuggestionKey)
			}
		}
	}
	return nil
}
