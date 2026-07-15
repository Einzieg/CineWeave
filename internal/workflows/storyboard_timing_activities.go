package workflows

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/provider"
	storyboardpkg "github.com/Einzieg/cineweave/internal/storyboard"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	nodeAnalyzeEpisodeTimingPrefix = "storyboard_timing_analyze"
)

type AnalyzeEpisodeTimingInput struct {
	OrganizationID      string `json:"organizationId"`
	ProjectID           string `json:"projectId"`
	WorkflowRunID       string `json:"workflowRunId"`
	CreatedBy           string `json:"createdBy"`
	ScriptID            string `json:"scriptId"`
	ScriptVersionID     string `json:"scriptVersionId"`
	ScriptEpisodeID     string `json:"scriptEpisodeId"`
	TargetDurationTicks *int64 `json:"targetDurationTicks,omitempty"`
}

type TimingAnalysisActivityOutput struct {
	AnalysisID             string                              `json:"analysisId"`
	ScriptID               string                              `json:"scriptId"`
	ScriptVersionID        string                              `json:"scriptVersionId"`
	ScriptEpisodeID        string                              `json:"scriptEpisodeId"`
	Revision               int                                 `json:"revision"`
	EstimatedDurationTicks int64                               `json:"estimatedDurationTicks"`
	MinimumDurationTicks   int64                               `json:"minimumDurationTicks"`
	TargetDurationTicks    *int64                              `json:"targetDurationTicks,omitempty"`
	TimelineTimebase       int64                               `json:"timelineTimebase"`
	FPSNumerator           int                                 `json:"fpsNumerator"`
	FPSDenominator         int                                 `json:"fpsDenominator"`
	Scenes                 []storyboardpkg.AnalyzedTimingScene `json:"scenes"`
	ProviderCallID         string                              `json:"providerCallId,omitempty"`
	ProviderCallIDs        []string                            `json:"providerCallIds,omitempty"`
	ModelID                string                              `json:"modelId,omitempty"`
	ModelIDs               []string                            `json:"modelIds,omitempty"`
	PromptVersionID        string                              `json:"promptVersionId,omitempty"`
	PromptVersionIDs       []string                            `json:"promptVersionIds,omitempty"`
	PromptHash             string                              `json:"promptHash,omitempty"`
	PromptHashes           []string                            `json:"promptHashes,omitempty"`
	BatchCount             int                                 `json:"batchCount"`
}

type AnalyzeEpisodeTimingWorkflowOptions struct {
	ScriptID              string   `json:"scriptId"`
	ScriptVersionID       string   `json:"scriptVersionId"`
	ScriptEpisodeID       string   `json:"scriptEpisodeId"`
	TargetDurationSeconds *float64 `json:"targetDurationSeconds,omitempty"`
}

func AnalyzeScriptEpisodeTimingWorkflow(ctx workflow.Context, input TextToStoryboardInput) (TimingAnalysisActivityOutput, error) {
	var options AnalyzeEpisodeTimingWorkflowOptions
	if err := json.Unmarshal(input.Input, &options); err != nil {
		return TimingAnalysisActivityOutput{}, temporal.NewNonRetryableApplicationError(err.Error(), "INVALID_REQUEST", err)
	}
	if strings.TrimSpace(options.ScriptID) == "" || strings.TrimSpace(options.ScriptVersionID) == "" || strings.TrimSpace(options.ScriptEpisodeID) == "" {
		return TimingAnalysisActivityOutput{}, temporal.NewNonRetryableApplicationError("scriptId, scriptVersionId, and scriptEpisodeId are required", "INVALID_REQUEST", nil)
	}
	var targetDurationTicks *int64
	if options.TargetDurationSeconds != nil && *options.TargetDurationSeconds > 0 {
		timebase := storyboardpkg.DefaultTimebase()
		value := timebase.SecondsToFrameTicksCeil(*options.TargetDurationSeconds)
		targetDurationTicks = &value
	}
	ctx = workflow.WithActivityOptions(ctx, providerTextActivityOptions())
	var output TimingAnalysisActivityOutput
	if err := workflow.ExecuteActivity(ctx, "AnalyzeEpisodeTiming", AnalyzeEpisodeTimingInput{
		OrganizationID:      input.OrganizationID,
		ProjectID:           input.ProjectID,
		WorkflowRunID:       input.WorkflowRunID,
		CreatedBy:           input.CreatedBy,
		ScriptID:            options.ScriptID,
		ScriptVersionID:     options.ScriptVersionID,
		ScriptEpisodeID:     options.ScriptEpisodeID,
		TargetDurationTicks: targetDurationTicks,
	}).Get(ctx, &output); err != nil {
		return TimingAnalysisActivityOutput{}, err
	}
	completionCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	if err := workflow.ExecuteActivity(completionCtx, "CompleteEpisodeTimingWorkflow", input, output).Get(completionCtx, nil); err != nil {
		return TimingAnalysisActivityOutput{}, err
	}
	return output, nil
}

func (a Activities) AnalyzeEpisodeTiming(ctx context.Context, input AnalyzeEpisodeTimingInput) (TimingAnalysisActivityOutput, error) {
	baseInput := TextToStoryboardInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		Prompt:         "storyboard_timing_analysis",
		CreatedBy:      input.CreatedBy,
	}
	if err := validateScriptWorkflowInput(input.OrganizationID, input.ProjectID, input.WorkflowRunID, input.ScriptID); err != nil {
		return TimingAnalysisActivityOutput{}, err
	}
	if strings.TrimSpace(input.ScriptEpisodeID) == "" || strings.TrimSpace(input.ScriptVersionID) == "" {
		return TimingAnalysisActivityOutput{}, fmt.Errorf("scriptEpisodeId and scriptVersionId are required")
	}
	nodeKey := nodeKeyForID(nodeAnalyzeEpisodeTimingPrefix, input.ScriptEpisodeID)
	if existing, ok, err := a.existingTimingAnalysisOutput(ctx, input.WorkflowRunID, nodeKey); err != nil {
		return TimingAnalysisActivityOutput{}, err
	} else if ok {
		return existing, nil
	}

	project, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return TimingAnalysisActivityOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, err)
	}
	episode, err := a.scriptStoryboardEpisode(ctx, input.ProjectID, input.ScriptID, input.ScriptVersionID, input.ScriptEpisodeID)
	if err != nil {
		return TimingAnalysisActivityOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, err)
	}
	if reusable, ok, err := a.reusableTimingAnalysisOutput(ctx, input, episode); err != nil {
		return TimingAnalysisActivityOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, err)
	} else if ok {
		reuseExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			NodeKey:        nodeKey,
			NodeType:       "agent.storyboard_timing_analyze",
			Input: mustJSON(map[string]any{
				"scriptId":         input.ScriptID,
				"scriptVersionId":  input.ScriptVersionID,
				"scriptEpisodeId":  input.ScriptEpisodeID,
				"reusedAnalysisId": reusable.AnalysisID,
			}),
		})
		if err != nil {
			return TimingAnalysisActivityOutput{}, err
		}
		if err := a.insertStoryboardPlanningEvent(ctx, input, reuseExecution, "storyboard.timing.reused", "script_timing_analysis", reusable.AnalysisID, map[string]any{
			"nodeRunId": reuseExecution.NodeRunID,
		}, mustJSON(reusable)); err != nil {
			return TimingAnalysisActivityOutput{}, err
		}
		return reusable, nil
	}
	scenes, err := a.storyboardScenesForEpisode(ctx, input.ProjectID, input.ScriptVersionID, input.ScriptEpisodeID)
	if err != nil {
		return TimingAnalysisActivityOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, err)
	}
	batches := splitEpisodeTimingBatches(episode.Content, scenes)
	if len(batches) == 0 {
		return TimingAnalysisActivityOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: provider.CodeInvalidRequest, Message: "script episode has no analyzable content", Retryable: false, RetryabilityKnown: true})
	}
	nodeExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKey,
		NodeType:       "agent.storyboard_timing_analyze",
		Input: mustJSON(map[string]any{
			"scriptId":            input.ScriptID,
			"scriptVersionId":     input.ScriptVersionID,
			"scriptEpisodeId":     input.ScriptEpisodeID,
			"targetDurationTicks": input.TargetDurationTicks,
			"modelProfileKey":     project.ScriptModelProfileKey,
			"promptTemplateKey":   promptKeyStoryboardTimingBatchAnalyzer,
			"batchCount":          len(batches),
			"batchConcurrency":    timingBatchConcurrency,
		}),
	})
	if err != nil {
		return TimingAnalysisActivityOutput{}, err
	}
	if err := a.insertStoryboardPlanningEvent(ctx, input, nodeExecution, "storyboard.timing.started", "script_episode", input.ScriptEpisodeID, map[string]any{
		"nodeRunId":  nodeExecution.NodeRunID,
		"batchCount": len(batches),
	}, nil); err != nil {
		return TimingAnalysisActivityOutput{}, a.failActivity(ctx, baseInput, nodeExecution, err)
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, project.ScriptModelProfileKey, []string{"text", "multimodal"}); err != nil {
		return TimingAnalysisActivityOutput{}, a.failActivity(ctx, baseInput, nodeExecution, err)
	}
	if a.gateway == nil {
		return TimingAnalysisActivityOutput{}, a.failActivity(ctx, baseInput, nodeExecution, workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
	}
	batchOutputs, err := a.analyzeEpisodeTimingBatches(ctx, input, project, episode, scenes, nodeExecution)
	if err != nil {
		return TimingAnalysisActivityOutput{}, a.retryOrFailTimingAnalysis(ctx, baseInput, nodeExecution, err)
	}
	semantic, provenance, err := mergeTimingBatchOutputs(batchOutputs, episode.EpisodeIndex)
	if err != nil {
		return TimingAnalysisActivityOutput{}, a.retryOrFailTimingAnalysis(ctx, baseInput, nodeExecution, workflowError{Code: "TIMING_BATCH_OUTPUT_INVALID", Message: err.Error(), Retryable: true, RetryabilityKnown: true})
	}
	calibration := a.timingCalibrationParameters(ctx, input.ProjectID)
	analysis, err := storyboardpkg.AnalyzeSemanticTiming(semantic, storyboardpkg.AnalyzeTimingOptions{
		Timebase: storyboardpkg.Timebase{
			TicksPerSecond: project.TimelineTimebase,
			FPSNumerator:   int64(project.FPSNumerator),
			FPSDenominator: int64(project.FPSDenominator),
		},
		EpisodeContent:        episode.Content,
		TargetDurationTicks:   input.TargetDurationTicks,
		PunctuationPauseScale: calibration.PunctuationPauseScale,
		ActionDurationScales:  calibration.ActionDurationScales,
	})
	if err != nil {
		return TimingAnalysisActivityOutput{}, a.failActivity(ctx, baseInput, nodeExecution, workflowError{Code: "DURATION_CONSTRAINT_CONFLICT", Message: err.Error(), Retryable: false, RetryabilityKnown: true})
	}
	output, err := a.storeEpisodeTimingAnalysis(ctx, input, episode, nodeExecution, provenance, analysis)
	if err != nil {
		return TimingAnalysisActivityOutput{}, a.failActivity(ctx, baseInput, nodeExecution, err)
	}
	return output, nil
}

func (a Activities) storeEpisodeTimingAnalysis(
	ctx context.Context,
	input AnalyzeEpisodeTimingInput,
	episode ScriptStoryboardEpisodeRecord,
	nodeExecution NodeExecution,
	provenance timingAnalysisProvenance,
	analysis storyboardpkg.TimingAnalysisResult,
) (TimingAnalysisActivityOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return TimingAnalysisActivityOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, nodeExecution); err != nil {
		return TimingAnalysisActivityOutput{}, err
	}
	var lockedEpisodeID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM script_episodes WHERE project_id = $1 AND id = $2 FOR UPDATE`, input.ProjectID, input.ScriptEpisodeID).Scan(&lockedEpisodeID); err != nil {
		return TimingAnalysisActivityOutput{}, err
	}
	var revision int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(revision), 0) + 1
		FROM script_timing_analyses
		WHERE script_episode_id = $1
	`, input.ScriptEpisodeID).Scan(&revision); err != nil {
		return TimingAnalysisActivityOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE script_timing_analyses
		SET status = 'archived'
		WHERE script_episode_id = $1 AND status = 'ready'
	`, input.ScriptEpisodeID); err != nil {
		return TimingAnalysisActivityOutput{}, err
	}
	var sourceChapterID sql.NullString
	if err := tx.QueryRow(ctx, `SELECT source_chapter_id::text FROM script_episodes WHERE id = $1`, input.ScriptEpisodeID).Scan(&sourceChapterID); err != nil {
		return TimingAnalysisActivityOutput{}, err
	}
	var analysisID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO script_timing_analyses(
			organization_id, project_id, script_id, script_version_id, script_episode_id,
			revision, status, estimated_duration_ticks, minimum_duration_ticks, target_duration_ticks,
			timeline_timebase, fps_numerator, fps_denominator, method_version,
			prompt_version_id, prompt_hash, provider_call_id, model_id, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, 'ready', $7, $8, $9, $10, $11, $12,
		        'semantic-agent-batched+deterministic-v2', NULLIF($13, '')::uuid, NULLIF($14, ''),
		        NULLIF($15, '')::uuid, NULLIF($16, '')::uuid, $17, NULLIF($18, '')::uuid)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, input.ScriptID, input.ScriptVersionID, input.ScriptEpisodeID,
		revision, analysis.EstimatedDurationTicks, analysis.MinimumDurationTicks, nullableInt64PtrWorkflow(analysis.TargetDurationTicks),
		analysis.Timebase.TicksPerSecond, analysis.Timebase.FPSNumerator, analysis.Timebase.FPSDenominator,
		firstTimingPromptVersion(provenance), firstTimingPromptHash(provenance), firstTimingProviderCall(provenance), firstTimingModel(provenance),
		mustJSON(map[string]any{
			"workflowRunId":    input.WorkflowRunID,
			"nodeRunId":        nodeExecution.NodeRunID,
			"sceneCount":       len(analysis.Scenes),
			"unitCount":        len(analysis.Units),
			"blockCount":       len(analysis.Blocks),
			"batchCount":       len(provenance.Batches),
			"providerCallIds":  provenance.ProviderCallIDs,
			"modelIds":         provenance.ModelIDs,
			"promptVersionIds": provenance.PromptVersionIDs,
			"promptHashes":     provenance.PromptHashes,
			"sourceHash":       timingSourceHash(episode.Content),
		}), input.CreatedBy).Scan(&analysisID); err != nil {
		return TimingAnalysisActivityOutput{}, err
	}

	unitIDByKey := make(map[string]string, len(analysis.Units))
	blockOrdinalByUnit := make(map[string]int, len(analysis.Units))
	for blockOrdinal, block := range analysis.Blocks {
		for _, unit := range block.Units {
			blockOrdinalByUnit[unit.ID] = blockOrdinal
		}
	}
	for unitIndex := range analysis.Units {
		unit := &analysis.Units[unitIndex]
		var unitID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO script_timing_units(
				timing_analysis_id, script_scene_id, source_chapter_id, unit_ordinal,
				unit_type, track, parallel_group, speaker, source_text, delivery,
				source_start_offset, source_end_offset, start_tick, end_tick,
				min_duration_ticks, max_duration_ticks, duration_source, confidence, metadata
			)
			VALUES ($1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4, $5, $6,
			        NULLIF($7, ''), NULLIF($8, ''), $9, NULLIF($10, ''), $11, $12,
			        $13, $14, $15, $16, $17, $18, $19)
			RETURNING id::text
		`, analysisID, unit.ScriptSceneID, nullableStringValue(sourceChapterID), unit.Ordinal,
			unit.Type, unit.Track, unit.ParallelGroup, unit.Speaker, unit.SourceText, unit.Delivery,
			nullableIntPtr(unit.SourceStartOffset), nullableIntPtr(unit.SourceEndOffset),
			unit.StartTick, unit.EndTick, unit.MinimumTicks, unit.MaximumTicks, unit.DurationSource, unit.Confidence,
			mustJSON(map[string]any{
				"sceneKey":            unit.SceneKey,
				"semanticUnitKey":     unit.ID,
				"blockOrdinal":        blockOrdinalByUnit[unit.ID],
				"forceBoundaryBefore": unit.ForceBoundaryBefore,
				"forceBoundaryAfter":  unit.ForceBoundaryAfter,
			})).Scan(&unitID); err != nil {
			return TimingAnalysisActivityOutput{}, err
		}
		unitIDByKey[unit.ID] = unitID
		unit.ID = unitID
	}
	for sceneIndex := range analysis.Scenes {
		for unitIndex := range analysis.Scenes[sceneIndex].Units {
			key := analysis.Scenes[sceneIndex].Units[unitIndex].ID
			analysis.Scenes[sceneIndex].Units[unitIndex].ID = unitIDByKey[key]
		}
		for blockIndex := range analysis.Scenes[sceneIndex].Blocks {
			for unitIndex := range analysis.Scenes[sceneIndex].Blocks[blockIndex].Units {
				key := analysis.Scenes[sceneIndex].Blocks[blockIndex].Units[unitIndex].ID
				analysis.Scenes[sceneIndex].Blocks[blockIndex].Units[unitIndex].ID = unitIDByKey[key]
			}
		}
	}
	output := TimingAnalysisActivityOutput{
		AnalysisID:             analysisID,
		ScriptID:               input.ScriptID,
		ScriptVersionID:        input.ScriptVersionID,
		ScriptEpisodeID:        input.ScriptEpisodeID,
		Revision:               revision,
		EstimatedDurationTicks: analysis.EstimatedDurationTicks,
		MinimumDurationTicks:   analysis.MinimumDurationTicks,
		TargetDurationTicks:    analysis.TargetDurationTicks,
		TimelineTimebase:       analysis.Timebase.TicksPerSecond,
		FPSNumerator:           int(analysis.Timebase.FPSNumerator),
		FPSDenominator:         int(analysis.Timebase.FPSDenominator),
		Scenes:                 analysis.Scenes,
		ProviderCallID:         firstTimingProviderCall(provenance),
		ProviderCallIDs:        provenance.ProviderCallIDs,
		ModelID:                firstTimingModel(provenance),
		ModelIDs:               provenance.ModelIDs,
		PromptVersionID:        firstTimingPromptVersion(provenance),
		PromptVersionIDs:       provenance.PromptVersionIDs,
		PromptHash:             firstTimingPromptHash(provenance),
		PromptHashes:           provenance.PromptHashes,
		BatchCount:             len(provenance.Batches),
	}
	if _, err := tx.Exec(ctx, `
		UPDATE script_timing_analyses
		SET metadata = metadata || jsonb_build_object('activityOutput', $2::jsonb)
		WHERE id = $1
	`, analysisID, mustJSON(output)); err != nil {
		return TimingAnalysisActivityOutput{}, err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.timing.completed", "script_timing_analysis", analysisID, mustJSON(map[string]any{
		"workflowRunId":          input.WorkflowRunID,
		"scriptEpisodeId":        input.ScriptEpisodeID,
		"analysisId":             analysisID,
		"estimatedDurationTicks": analysis.EstimatedDurationTicks,
		"minimumDurationTicks":   analysis.MinimumDurationTicks,
		"sceneCount":             len(analysis.Scenes),
		"unitCount":              len(analysis.Units),
		"batchCount":             len(provenance.Batches),
	})); err != nil {
		return TimingAnalysisActivityOutput{}, err
	}
	if _, err := completeNodeRunTx(ctx, tx, nodeExecution, mustJSON(output)); err != nil {
		return TimingAnalysisActivityOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TimingAnalysisActivityOutput{}, err
	}
	return output, nil
}

func (a Activities) retryOrFailTimingAnalysis(ctx context.Context, input TextToStoryboardInput, nodeExecution NodeExecution, cause error) error {
	code, message := workflowErrorFields(cause, codeActivityFailed)
	retryable := true
	var workflowErr workflowError
	if errors.As(cause, &workflowErr) && workflowErr.RetryabilityKnown {
		retryable = workflowErr.Retryable
	}
	if retryable && activity.GetInfo(ctx).Attempt < 3 {
		_ = ProgressNodeRun(ctx, a.db, nodeExecution, mustJSON(map[string]any{
			"status":  "retrying_failed_batches",
			"attempt": activity.GetInfo(ctx).Attempt,
			"code":    code,
			"message": message,
		}))
		return newWorkflowApplicationError(cause, code, message)
	}
	return a.failActivity(ctx, input, nodeExecution, cause)
}

func (a Activities) CompleteEpisodeTimingWorkflow(ctx context.Context, input TextToStoryboardInput, output TimingAnalysisActivityOutput) error {
	return a.completeSimpleWorkflow(ctx, input, output)
}

func (a Activities) existingTimingAnalysisOutput(ctx context.Context, workflowRunID, nodeKey string) (TimingAnalysisActivityOutput, bool, error) {
	var raw json.RawMessage
	err := a.db.QueryRow(ctx, `
		SELECT output
		FROM workflow_node_runs
		WHERE workflow_run_id = $1 AND node_key = $2 AND status = 'succeeded'
	`, workflowRunID, nodeKey).Scan(&raw)
	if err == pgx.ErrNoRows {
		return TimingAnalysisActivityOutput{}, false, nil
	}
	if err != nil {
		return TimingAnalysisActivityOutput{}, false, err
	}
	var output TimingAnalysisActivityOutput
	if err := json.Unmarshal(raw, &output); err != nil {
		return TimingAnalysisActivityOutput{}, false, err
	}
	return output, output.AnalysisID != "", nil
}

func (a Activities) reusableTimingAnalysisOutput(ctx context.Context, input AnalyzeEpisodeTimingInput, episode ScriptStoryboardEpisodeRecord) (TimingAnalysisActivityOutput, bool, error) {
	var raw json.RawMessage
	err := a.db.QueryRow(ctx, `
		SELECT analysis.metadata->'activityOutput'
		FROM script_timing_analyses analysis
		JOIN script_episodes episode ON episode.id = analysis.script_episode_id
		JOIN projects project ON project.id = analysis.project_id
		WHERE analysis.project_id = $1
		  AND analysis.script_id = $2
		  AND analysis.script_version_id = $3
		  AND analysis.script_episode_id = $4
		  AND analysis.status = 'ready'
		  AND analysis.method_version = 'semantic-agent-batched+deterministic-v2'
		  AND analysis.target_duration_ticks IS NOT DISTINCT FROM $6::bigint
		  AND analysis.timeline_timebase = project.timeline_timebase
		  AND analysis.fps_numerator = project.fps_numerator
		  AND analysis.fps_denominator = project.fps_denominator
		  AND (
		    analysis.metadata->>'sourceHash' = $5
		    OR (
		      NOT (analysis.metadata ? 'sourceHash')
		      AND analysis.created_at >= episode.updated_at
		    )
		  )
		  AND analysis.metadata ? 'activityOutput'
		ORDER BY analysis.revision DESC
		LIMIT 1
	`, input.ProjectID, input.ScriptID, input.ScriptVersionID, input.ScriptEpisodeID,
		timingSourceHash(episode.Content), nullableInt64PtrWorkflow(input.TargetDurationTicks)).Scan(&raw)
	if err == pgx.ErrNoRows {
		return TimingAnalysisActivityOutput{}, false, nil
	}
	if err != nil {
		return TimingAnalysisActivityOutput{}, false, err
	}
	var output TimingAnalysisActivityOutput
	if err := json.Unmarshal(raw, &output); err != nil {
		return TimingAnalysisActivityOutput{}, false, err
	}
	return output, output.AnalysisID != "", nil
}

func (a Activities) insertStoryboardPlanningEvent(ctx context.Context, input AnalyzeEpisodeTimingInput, nodeExecution NodeExecution, eventType, aggregateType, aggregateID string, payload map[string]any, completionOutput json.RawMessage) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, nodeExecution); err != nil {
		return err
	}
	payload["workflowRunId"] = input.WorkflowRunID
	payload["scriptEpisodeId"] = input.ScriptEpisodeID
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, eventType, aggregateType, aggregateID, mustJSON(payload)); err != nil {
		return err
	}
	if len(completionOutput) > 0 {
		if _, err := completeNodeRunTx(ctx, tx, nodeExecution, completionOutput); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func nullableInt64PtrWorkflow(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableIntPtr(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableStringValue(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}
