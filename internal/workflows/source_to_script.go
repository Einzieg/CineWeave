package workflows

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Einzieg/cineweave/internal/production"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/jackc/pgx/v5"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/workflow"
)

const (
	nodePrepareScriptFromSourceKey  = "prepare_script_from_source"
	nodeGenerateScriptFromSourceKey = "generate_script_from_source"
	nodeFinalizeScriptFromSourceKey = "finalize_script_from_source"
	promptKeyScriptAgentGenerate    = "script_agent_generate"
	promptKeyBriefToScript          = "brief_to_script"
	defaultSourceEpisodesPerRun     = 20
	SourceToScriptEpisodeNodeType   = "agent.script_generate_episode"
)

const SourceToScriptPrepareNodeKey = nodePrepareScriptFromSourceKey

type SourceToScriptOptions struct {
	SourceID        string   `json:"sourceId"`
	TargetScriptID  string   `json:"scriptId,omitempty"`
	CreateNewScript bool     `json:"createNewScript,omitempty"`
	ChapterIDs      []string `json:"chapterIds,omitempty"`
	Instruction     string   `json:"instruction,omitempty"`
	Title           string   `json:"title,omitempty"`
	MaxConcurrency  int      `json:"maxConcurrency,omitempty"`
	AgentTaskID     string   `json:"agentTaskId,omitempty"`
	AgentStepID     string   `json:"agentStepId,omitempty"`
	IdempotencyKey  string   `json:"idempotencyKey,omitempty"`
}

type ProjectSourceRecord struct {
	ID              string `json:"id"`
	SourceType      string `json:"sourceType"`
	Title           string `json:"title"`
	Content         string `json:"content"`
	ContentFormat   string `json:"contentFormat"`
	Status          string `json:"status"`
	ContentRevision int64  `json:"contentRevision"`
	ContentHash     string `json:"contentHash"`
}

type GenerateScriptFromSourceInput struct {
	OrganizationID  string   `json:"organizationId"`
	ProjectID       string   `json:"projectId"`
	WorkflowRunID   string   `json:"workflowRunId"`
	CreatedBy       string   `json:"createdBy"`
	SourceID        string   `json:"sourceId"`
	TargetScriptID  string   `json:"scriptId,omitempty"`
	CreateNewScript bool     `json:"createNewScript,omitempty"`
	ChapterIDs      []string `json:"chapterIds,omitempty"`
	Instruction     string   `json:"instruction,omitempty"`
	Title           string   `json:"title,omitempty"`
	AgentTaskID     string   `json:"agentTaskId,omitempty"`
	AgentStepID     string   `json:"agentStepId,omitempty"`
	IdempotencyKey  string   `json:"idempotencyKey,omitempty"`
}

type SourceToScriptOutput struct {
	Status           string   `json:"status"`
	SourceID         string   `json:"sourceId"`
	AdaptationPlanID string   `json:"adaptationPlanId,omitempty"`
	ScriptID         string   `json:"scriptId"`
	ScriptVersionID  string   `json:"scriptVersionId"`
	AgentRunID       string   `json:"agentRunId,omitempty"`
	ProviderCallID   string   `json:"providerCallId,omitempty"`
	ProviderCallIDs  []string `json:"providerCallIds,omitempty"`
	ModelID          string   `json:"modelId,omitempty"`
	ModelIDs         []string `json:"modelIds,omitempty"`
	EpisodeCount     int      `json:"episodeCount,omitempty"`
	TotalItems       int      `json:"totalItems"`
	CompletedItems   int      `json:"completedItems"`
	FailedItems      int      `json:"failedItems"`
	FailedEpisodes   []int    `json:"failedEpisodes,omitempty"`
	MissingItems     int      `json:"missingItems,omitempty"`
	Activated        bool     `json:"activated"`
	Content          string   `json:"content"`
}

type SourceToScriptPlan struct {
	GenerationID            string                     `json:"generationId"`
	RootGenerationID        string                     `json:"rootGenerationId"`
	AttemptGeneration       int                        `json:"attemptGeneration"`
	SourceID                string                     `json:"sourceId"`
	SourceType              string                     `json:"sourceType"`
	SourceTitle             string                     `json:"sourceTitle"`
	SourceRevision          int64                      `json:"sourceRevision"`
	SourceContentHash       string                     `json:"sourceContentHash"`
	SourceSnapshotHash      string                     `json:"sourceSnapshotHash"`
	ManifestHash            string                     `json:"manifestHash"`
	ScriptID                string                     `json:"scriptId"`
	ScriptVersionID         string                     `json:"scriptVersionId,omitempty"`
	BaseScriptVersionID     string                     `json:"baseScriptVersionId,omitempty"`
	ExpectedScriptRevision  int64                      `json:"expectedScriptRevision"`
	PreviousScriptVersionID string                     `json:"previousScriptVersionId,omitempty"`
	PreviousActiveScriptID  string                     `json:"previousActiveScriptId,omitempty"`
	PromptTemplateKey       string                     `json:"promptTemplateKey"`
	PromptVersionID         string                     `json:"promptVersionId"`
	PromptContentHash       string                     `json:"promptContentHash"`
	ModelProfileKey         string                     `json:"modelProfileKey"`
	ProviderModelID         string                     `json:"providerModelId"`
	Title                   string                     `json:"title"`
	EpisodeTotal            int                        `json:"episodeTotal"`
	SeriesEpisodeTotal      int                        `json:"seriesEpisodeTotal,omitempty"`
	Chapters                []SourceToScriptChapterRef `json:"chapters,omitempty"`
}

type SourceToScriptWorkflowState struct {
	Initialized           bool               `json:"initialized"`
	Plan                  SourceToScriptPlan `json:"plan"`
	EpisodeIndexes        []int              `json:"episodeIndexes,omitempty"`
	NextEpisodeIndex      int                `json:"nextEpisodeIndex"`
	CompletedEpisodeCount int                `json:"completedEpisodeCount"`
	FailedEpisodeCount    int                `json:"failedEpisodeCount"`
	ContinueCount         int                `json:"continueCount"`
	AttemptGeneration     int                `json:"attemptGeneration"`
}

type SourceToScriptChapterRef struct {
	ID              string `json:"id,omitempty"`
	ItemKey         string `json:"itemKey"`
	ManifestOrdinal int    `json:"manifestOrdinal"`
	ChapterIndex    int    `json:"chapterIndex,omitempty"`
	VolumeIndex     int    `json:"volumeIndex,omitempty"`
	SectionIndex    int    `json:"sectionIndex,omitempty"`
	VolumeTitle     string `json:"volumeTitle,omitempty"`
	Title           string `json:"title,omitempty"`
	ContentRevision int64  `json:"contentRevision"`
	ContentHash     string `json:"contentHash"`
}

type PrepareScriptFromSourceInput struct {
	GenerateScriptFromSourceInput
}

type GenerateSourceScriptEpisodeInput struct {
	OrganizationID    string                   `json:"organizationId"`
	ProjectID         string                   `json:"projectId"`
	WorkflowRunID     string                   `json:"workflowRunId"`
	CreatedBy         string                   `json:"createdBy"`
	SourceID          string                   `json:"sourceId"`
	GenerationID      string                   `json:"generationId"`
	ItemKey           string                   `json:"itemKey"`
	ScriptID          string                   `json:"scriptId"`
	ScriptVersionID   string                   `json:"scriptVersionId,omitempty"`
	Instruction       string                   `json:"instruction,omitempty"`
	EpisodeIndex      int                      `json:"episodeIndex"`
	EpisodeTotal      int                      `json:"episodeTotal"`
	BatchIndex        int                      `json:"batchIndex,omitempty"`
	BatchTotal        int                      `json:"batchTotal,omitempty"`
	Chapter           SourceToScriptChapterRef `json:"chapter,omitempty"`
	AttemptGeneration int                      `json:"attemptGeneration"`
}

type SourceScriptEpisodeOutput struct {
	SourceID           string `json:"sourceId"`
	SourceChapterID    string `json:"sourceChapterId,omitempty"`
	ScriptID           string `json:"scriptId"`
	ScriptVersionID    string `json:"scriptVersionId,omitempty"`
	GenerationID       string `json:"generationId"`
	GenerationResultID string `json:"generationResultId"`
	EpisodeID          string `json:"episodeId,omitempty"`
	EpisodeIndex       int    `json:"episodeIndex"`
	EpisodeTitle       string `json:"episodeTitle"`
	AgentRunID         string `json:"agentRunId,omitempty"`
	ProviderCallID     string `json:"providerCallId,omitempty"`
	ModelID            string `json:"modelId,omitempty"`
	PromptVersionID    string `json:"promptVersionId,omitempty"`
	PromptHash         string `json:"promptHash,omitempty"`
	Content            string `json:"content,omitempty"`
	Skipped            bool   `json:"skipped,omitempty"`
}

type SourceToScriptFinalization struct {
	RequestedEpisodeCount int `json:"requestedEpisodeCount"`
	CompletedEpisodeCount int `json:"completedEpisodeCount"`
	FailedEpisodeCount    int `json:"failedEpisodeCount"`
}

type FailSourceScriptEpisodeInput struct {
	Episode      GenerateSourceScriptEpisodeInput `json:"episode"`
	ErrorCode    string                           `json:"errorCode"`
	ErrorMessage string                           `json:"errorMessage"`
}

func GenerateSourceScriptEpisodeWorkflow(ctx workflow.Context, input GenerateSourceScriptEpisodeInput) (SourceScriptEpisodeOutput, error) {
	activityCtx := workflow.WithActivityOptions(ctx, scriptEpisodeGenerationActivityOptions())
	var output SourceScriptEpisodeOutput
	if err := workflow.ExecuteActivity(activityCtx, "GenerateSourceScriptEpisode", input).Get(ctx, &output); err != nil {
		if isWorkflowCancellationError(err) {
			return SourceScriptEpisodeOutput{}, err
		}
		code, message := workflowExecutionError(err)
		finalizeCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
		_ = workflow.ExecuteActivity(finalizeCtx, "FailSourceScriptEpisode", FailSourceScriptEpisodeInput{
			Episode: input, ErrorCode: code, ErrorMessage: message,
		}).Get(ctx, nil)
		return SourceScriptEpisodeOutput{}, err
	}
	return output, nil
}

func SourceToScriptWorkflow(ctx workflow.Context, input TextToStoryboardInput) (SourceToScriptOutput, error) {
	options := resolveSourceToScriptOptions(input.Input)
	prepareCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	finalizeCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())

	state := SourceToScriptWorkflowState{}
	if input.SourceToScriptState != nil {
		state = *input.SourceToScriptState
	}
	if !state.Initialized {
		if err := workflow.ExecuteActivity(prepareCtx, "PrepareScriptFromSource", PrepareScriptFromSourceInput{GenerateScriptFromSourceInput: GenerateScriptFromSourceInput{
			OrganizationID:  input.OrganizationID,
			ProjectID:       input.ProjectID,
			WorkflowRunID:   input.WorkflowRunID,
			CreatedBy:       input.CreatedBy,
			SourceID:        options.SourceID,
			TargetScriptID:  options.TargetScriptID,
			CreateNewScript: options.CreateNewScript,
			ChapterIDs:      options.ChapterIDs,
			Instruction:     options.Instruction,
			Title:           options.Title,
			AgentTaskID:     options.AgentTaskID,
			AgentStepID:     options.AgentStepID,
			IdempotencyKey:  options.IdempotencyKey,
		}}).Get(ctx, &state.Plan); err != nil {
			return SourceToScriptOutput{}, err
		}
		state.Initialized = true
	}
	plan := state.Plan
	episodeTotal := plan.EpisodeTotal
	if episodeTotal <= 0 {
		episodeTotal = 1
	}
	if len(state.EpisodeIndexes) == 0 {
		state.EpisodeIndexes = sourceToScriptEpisodeIndexes(episodeTotal)
	}
	if state.AttemptGeneration <= 0 {
		state.AttemptGeneration = 1
	}
	if err := validateSourceToScriptEpisodeIndexes(state.EpisodeIndexes, episodeTotal); err != nil {
		return SourceToScriptOutput{}, err
	}
	if state.NextEpisodeIndex < 0 || state.NextEpisodeIndex > len(state.EpisodeIndexes) {
		return SourceToScriptOutput{}, fmt.Errorf("source_to_script checkpoint index %d is outside workset size %d", state.NextEpisodeIndex, len(state.EpisodeIndexes))
	}
	pageEnd := min(len(state.EpisodeIndexes), state.NextEpisodeIndex+defaultSourceEpisodesPerRun)
	completedInPage, failedInPage, err := runSourceScriptEpisodePage(ctx, input, options, plan, state.EpisodeIndexes, state.AttemptGeneration, state.NextEpisodeIndex, pageEnd)
	if err != nil {
		return SourceToScriptOutput{}, err
	}
	state.CompletedEpisodeCount += completedInPage
	state.FailedEpisodeCount += failedInPage
	state.NextEpisodeIndex = pageEnd
	if state.NextEpisodeIndex < len(state.EpisodeIndexes) {
		state.ContinueCount++
		nextInput := input
		nextInput.SourceToScriptState = &state
		return SourceToScriptOutput{}, workflow.NewContinueAsNewError(ctx, SourceToScriptWorkflow, nextInput)
	}

	var output SourceToScriptOutput
	if err := workflow.ExecuteActivity(finalizeCtx, "FinalizeScriptFromSource", GenerateScriptFromSourceInput{
		OrganizationID:  input.OrganizationID,
		ProjectID:       input.ProjectID,
		WorkflowRunID:   input.WorkflowRunID,
		CreatedBy:       input.CreatedBy,
		SourceID:        options.SourceID,
		TargetScriptID:  options.TargetScriptID,
		CreateNewScript: options.CreateNewScript,
		ChapterIDs:      options.ChapterIDs,
		Instruction:     options.Instruction,
		Title:           options.Title,
		AgentTaskID:     options.AgentTaskID,
		AgentStepID:     options.AgentStepID,
		IdempotencyKey:  options.IdempotencyKey,
	}, plan, SourceToScriptFinalization{
		RequestedEpisodeCount: len(state.EpisodeIndexes),
		CompletedEpisodeCount: state.CompletedEpisodeCount,
		FailedEpisodeCount:    state.FailedEpisodeCount,
	}).Get(ctx, &output); err != nil {
		return SourceToScriptOutput{}, err
	}
	if err := workflow.ExecuteActivity(finalizeCtx, "CompleteSourceToScriptWorkflow", input, output).Get(ctx, nil); err != nil {
		return SourceToScriptOutput{}, err
	}
	return output, nil
}

func runSourceScriptEpisodePage(
	ctx workflow.Context,
	input TextToStoryboardInput,
	options SourceToScriptOptions,
	plan SourceToScriptPlan,
	episodeIndexes []int,
	attemptGeneration int,
	startIndex int,
	endIndex int,
) (int, int, error) {
	maxConcurrency := NormalizeSourceToScriptConcurrency(options.MaxConcurrency)
	next := startIndex
	completed := 0
	failed := 0
	type inFlightEpisode struct {
		index  int
		future workflow.ChildWorkflowFuture
	}
	inFlight := make([]inFlightEpisode, 0, maxConcurrency)
	for next < endIndex || len(inFlight) > 0 {
		for next < endIndex && len(inFlight) < maxConcurrency {
			planIndex := episodeIndexes[next]
			chapter := SourceToScriptChapterRef{}
			if len(plan.Chapters) > 0 {
				chapter = plan.Chapters[planIndex]
			}
			episodeInput := GenerateSourceScriptEpisodeInput{
				OrganizationID:    input.OrganizationID,
				ProjectID:         input.ProjectID,
				WorkflowRunID:     input.WorkflowRunID,
				CreatedBy:         input.CreatedBy,
				SourceID:          plan.SourceID,
				GenerationID:      plan.GenerationID,
				ItemKey:           chapter.ItemKey,
				ScriptID:          plan.ScriptID,
				ScriptVersionID:   plan.BaseScriptVersionID,
				Instruction:       options.Instruction,
				EpisodeIndex:      SourceToScriptEpisodeNumber(plan, planIndex),
				EpisodeTotal:      SourceToScriptSeriesEpisodeTotal(plan),
				BatchIndex:        planIndex + 1,
				BatchTotal:        maxInt(1, plan.EpisodeTotal),
				Chapter:           chapter,
				AttemptGeneration: attemptGeneration,
			}
			entityID := firstNonEmptyString(chapter.ID, strconv.Itoa(planIndex+1))
			childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
				WorkflowID:          fmt.Sprintf("%s/source-script/%s/g%d", input.WorkflowRunID, entityID, attemptGeneration),
				ParentClosePolicy:   enumspb.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
				WaitForCancellation: true,
			})
			future := workflow.ExecuteChildWorkflow(childCtx, GenerateSourceScriptEpisodeWorkflow, episodeInput)
			inFlight = append(inFlight, inFlightEpisode{index: planIndex, future: future})
			next++
		}
		selector := workflow.NewSelector(ctx)
		selected := -1
		for i := range inFlight {
			i := i
			selector.AddFuture(inFlight[i].future, func(workflow.Future) { selected = i })
		}
		selector.Select(ctx)
		if selected < 0 {
			return completed, failed, fmt.Errorf("source_to_script episode selector did not choose a future")
		}
		item := inFlight[selected]
		var episodeOutput SourceScriptEpisodeOutput
		if err := item.future.Get(ctx, &episodeOutput); err != nil {
			if isWorkflowCancellationError(err) {
				return completed, failed, err
			}
			failed++
		} else {
			completed++
		}
		inFlight = append(inFlight[:selected], inFlight[selected+1:]...)
	}
	return completed, failed, nil
}

func sourceToScriptEpisodeIndexes(episodeTotal int) []int {
	indexes := make([]int, episodeTotal)
	for index := range indexes {
		indexes[index] = index
	}
	return indexes
}

func validateSourceToScriptEpisodeIndexes(indexes []int, episodeTotal int) error {
	seen := make(map[int]struct{}, len(indexes))
	for _, index := range indexes {
		if index < 0 || index >= episodeTotal {
			return fmt.Errorf("source_to_script episode index %d is outside episode total %d", index, episodeTotal)
		}
		if _, ok := seen[index]; ok {
			return fmt.Errorf("source_to_script episode index %d is duplicated", index)
		}
		seen[index] = struct{}{}
	}
	return nil
}

// SourceToScriptEpisodeNumber returns the durable series ordinal for a selected chapter.
// A generation batch may contain only chapter 2 (or any sparse subset), so the batch
// position must never be used as the persisted episode number.
func SourceToScriptEpisodeNumber(plan SourceToScriptPlan, planIndex int) int {
	if planIndex >= 0 && planIndex < len(plan.Chapters) && plan.Chapters[planIndex].ManifestOrdinal > 0 {
		return plan.Chapters[planIndex].ManifestOrdinal
	}
	return planIndex + 1
}

func SourceToScriptSeriesEpisodeTotal(plan SourceToScriptPlan) int {
	total := plan.SeriesEpisodeTotal
	for index := range plan.Chapters {
		total = maxInt(total, SourceToScriptEpisodeNumber(plan, index))
	}
	if total <= 0 {
		total = plan.EpisodeTotal
	}
	return maxInt(1, total)
}

func SourceToScriptEpisodeNodeKey(sourceChapterID string, episodeIndex int) string {
	return nodeKeyForID(nodeGenerateScriptFromSourceKey, firstNonEmptyString(strings.TrimSpace(sourceChapterID), strconv.Itoa(episodeIndex)))
}

func queueSourceScriptEpisodeNodesTx(ctx context.Context, tx pgx.Tx, input GenerateScriptFromSourceInput, plan SourceToScriptPlan) error {
	var attemptGeneration int
	if err := tx.QueryRow(ctx, `SELECT attempt_generation FROM workflow_runs WHERE id = $1`, input.WorkflowRunID).Scan(&attemptGeneration); err != nil {
		return err
	}
	for planIndex := 0; planIndex < maxInt(1, plan.EpisodeTotal); planIndex++ {
		chapter := SourceToScriptChapterRef{}
		if len(plan.Chapters) > 0 {
			chapter = plan.Chapters[planIndex]
		}
		episode := GenerateSourceScriptEpisodeInput{
			OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
			CreatedBy: input.CreatedBy, SourceID: plan.SourceID, GenerationID: plan.GenerationID,
			ItemKey: chapter.ItemKey, ScriptID: plan.ScriptID,
			ScriptVersionID: plan.BaseScriptVersionID, Instruction: input.Instruction,
			EpisodeIndex: SourceToScriptEpisodeNumber(plan, planIndex), EpisodeTotal: SourceToScriptSeriesEpisodeTotal(plan),
			BatchIndex: planIndex + 1, BatchTotal: maxInt(1, plan.EpisodeTotal), Chapter: chapter,
			AttemptGeneration: attemptGeneration,
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO workflow_node_runs(
				organization_id, project_id, workflow_run_id, node_key, node_type,
				status, input, output, attempt_generation, production_generation_id
			)
			SELECT $1, $2, $3, $4, $7, 'queued', $5, '{}', $6, run.production_generation_id
			FROM workflow_runs run
			WHERE run.id = $3 AND run.project_id = $2
			ON CONFLICT (workflow_run_id, node_key) DO NOTHING
		`, input.OrganizationID, input.ProjectID, input.WorkflowRunID,
			SourceToScriptEpisodeNodeKey(chapter.ID, episode.EpisodeIndex), mustJSON(episode), attemptGeneration, SourceToScriptEpisodeNodeType); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET total_items = $2, revision = revision + 1, updated_at = now()
		WHERE id = $1 AND status IN ('pending', 'queued', 'running')
	`, input.WorkflowRunID, maxInt(1, plan.EpisodeTotal))
	return err
}

func sourceScriptEpisodeActivityError(cause error) error {
	if isWorkflowWriteFenced(cause) {
		return ErrWorkflowWriteFenced
	}
	code, message := workflowErrorFields(cause, codeActivityFailed)
	return newWorkflowApplicationError(cause, code, message)
}

func (a Activities) completeReplayedSourceScriptEpisode(ctx context.Context, input GenerateSourceScriptEpisodeInput, output SourceScriptEpisodeOutput) error {
	execution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		NodeKey:  SourceToScriptEpisodeNodeKey(input.Chapter.ID, input.EpisodeIndex),
		NodeType: SourceToScriptEpisodeNodeType, Input: mustJSON(input), AttemptGeneration: input.AttemptGeneration,
	})
	if err != nil {
		if !isWorkflowWriteFenced(err) {
			return err
		}
		var nodeStatus string
		queryErr := a.db.QueryRow(ctx, `
			SELECT status
			FROM workflow_node_runs
			WHERE workflow_run_id = $1 AND node_key = $2
		`, input.WorkflowRunID, SourceToScriptEpisodeNodeKey(input.Chapter.ID, input.EpisodeIndex)).Scan(&nodeStatus)
		if queryErr == nil && nodeStatus == "succeeded" {
			return nil
		}
		return err
	}
	return CompleteNodeRun(ctx, a.db, execution, mustJSON(output))
}

func (a Activities) FailSourceScriptEpisode(ctx context.Context, input FailSourceScriptEpisodeInput) error {
	code := strings.TrimSpace(input.ErrorCode)
	if code == "" {
		code = codeActivityFailed
	}
	message := strings.TrimSpace(input.ErrorMessage)
	if message == "" {
		message = "source script episode generation failed"
	}
	episode := input.Episode
	execution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: episode.OrganizationID, ProjectID: episode.ProjectID, WorkflowRunID: episode.WorkflowRunID,
		NodeKey:  SourceToScriptEpisodeNodeKey(episode.Chapter.ID, episode.EpisodeIndex),
		NodeType: SourceToScriptEpisodeNodeType, Input: mustJSON(episode), AttemptGeneration: episode.AttemptGeneration,
	})
	if err != nil {
		if isWorkflowWriteFenced(err) {
			var status string
			if queryErr := a.db.QueryRow(ctx, `
				SELECT status FROM workflow_node_runs WHERE workflow_run_id = $1 AND node_key = $2
			`, episode.WorkflowRunID, SourceToScriptEpisodeNodeKey(episode.Chapter.ID, episode.EpisodeIndex)).Scan(&status); queryErr == nil && isTerminalSourceScriptNodeStatus(status) {
				return nil
			}
		}
		return err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, episode.WorkflowRunID, execution); err != nil {
		return err
	}
	if err := storeSourceScriptGenerationFailureTx(ctx, tx, episode, code, message); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE source_to_script_generations
		SET status = CASE WHEN status = 'prepared' THEN 'running' ELSE status END
		WHERE id = $1 AND workflow_run_id = $2 AND status IN ('prepared', 'running')
	`, episode.GenerationID, episode.WorkflowRunID); err != nil {
		return err
	}
	if _, err := failNodeRunTx(ctx, tx, execution, code, message, mustJSON(map[string]any{
		"generationId": episode.GenerationID, "itemKey": episode.ItemKey,
		"episodeIndex": episode.EpisodeIndex, "sourceChapterId": episode.Chapter.ID,
		"errorCode": code, "errorMessage": message,
	})); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func isTerminalSourceScriptNodeStatus(status string) bool {
	switch status {
	case "succeeded", "failed", "cancelled", "skipped":
		return true
	default:
		return false
	}
}

func (a Activities) PrepareScriptFromSource(ctx context.Context, input PrepareScriptFromSourceInput) (SourceToScriptPlan, error) {
	baseInput := TextToStoryboardInput{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, Prompt: "source_to_script", CreatedBy: input.CreatedBy}
	if err := validateSourceToScriptInput(input.GenerateScriptFromSourceInput); err != nil {
		return SourceToScriptPlan{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: provider.CodeInvalidRequest, Message: err.Error()})
	}
	if existing, ok, err := a.completedSourceToScriptPlan(ctx, input.WorkflowRunID); err != nil {
		return SourceToScriptPlan{}, err
	} else if ok {
		return existing, nil
	}
	execution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodePrepareScriptFromSourceKey,
		NodeType:       "workflow.script_prepare",
		Input: mustJSON(map[string]any{
			"sourceId": input.SourceID, "scriptId": input.TargetScriptID,
			"createNewScript": input.CreateNewScript, "chapterIds": input.ChapterIDs,
			"idempotencyKey": input.IdempotencyKey,
		}),
	})
	if err != nil {
		if existing, ok, replayErr := a.completedSourceToScriptPlan(ctx, input.WorkflowRunID); replayErr == nil && ok {
			return existing, nil
		}
		return SourceToScriptPlan{}, err
	}
	plan, err := a.prepareScriptFromSourceGeneration(ctx, input, execution)
	if err != nil {
		code, message := workflowErrorFields(err, codeActivityFailed)
		_ = FailNodeRun(ctx, a.db, execution, code, message)
		return SourceToScriptPlan{}, err
	}
	return plan, nil
}

func (a Activities) completedSourceToScriptPlan(ctx context.Context, workflowRunID string) (SourceToScriptPlan, bool, error) {
	var raw json.RawMessage
	err := a.db.QueryRow(ctx, `
		SELECT output
		FROM workflow_node_runs
		WHERE workflow_run_id = $1 AND node_key = $2 AND status = 'succeeded'
	`, workflowRunID, nodePrepareScriptFromSourceKey).Scan(&raw)
	if err == pgx.ErrNoRows {
		return SourceToScriptPlan{}, false, nil
	}
	if err != nil {
		return SourceToScriptPlan{}, false, err
	}
	var plan SourceToScriptPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return SourceToScriptPlan{}, false, err
	}
	return plan, true, nil
}

func (a Activities) prepareScriptFromSourceLegacy(ctx context.Context, input PrepareScriptFromSourceInput) (SourceToScriptPlan, error) {
	baseInput := TextToStoryboardInput{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, Prompt: "source_to_script", CreatedBy: input.CreatedBy}
	if err := validateSourceToScriptInput(input.GenerateScriptFromSourceInput); err != nil {
		return SourceToScriptPlan{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: provider.CodeInvalidRequest, Message: err.Error()})
	}
	source, err := a.projectSourceRecord(ctx, input.ProjectID, input.SourceID)
	if err != nil {
		return SourceToScriptPlan{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if source.Status == "archived" {
		return SourceToScriptPlan{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: provider.CodeInvalidRequest, Message: "archived source cannot be used for script generation"})
	}

	chapterRefs := []SourceToScriptChapterRef{}
	seriesEpisodeTotal := 1
	if source.SourceType == "novel" {
		allChapters, err := a.loadNovelChapters(ctx, input.ProjectID, input.SourceID, nil)
		if err != nil {
			return SourceToScriptPlan{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
		}
		for _, chapter := range allChapters {
			seriesEpisodeTotal = maxInt(seriesEpisodeTotal, chapter.ChapterIndex)
		}
		chapters := allChapters
		chapterIDs := normalizeStringSlice(input.ChapterIDs)
		if len(chapterIDs) > 0 {
			selected := make(map[string]struct{}, len(chapterIDs))
			for _, chapterID := range chapterIDs {
				selected[chapterID] = struct{}{}
			}
			chapters = make([]novelChapterRecord, 0, len(chapterIDs))
			for _, chapter := range allChapters {
				if _, ok := selected[chapter.ID]; ok {
					chapters = append(chapters, chapter)
				}
			}
		}
		if len(chapters) == 0 {
			return SourceToScriptPlan{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: provider.CodeInvalidRequest, Message: "source has no chapters to generate"})
		}
		if len(chapterIDs) > 0 && len(chapters) != len(chapterIDs) {
			return SourceToScriptPlan{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: provider.CodeInvalidRequest, Message: "chapterIds do not match source chapters"})
		}
		chapterRefs = sourceToScriptChapterRefs(chapters)
	}
	nodeExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodePrepareScriptFromSourceKey,
		NodeType:       "workflow.script_prepare",
		Input: mustJSON(map[string]any{
			"sourceId":        input.SourceID,
			"scriptId":        input.TargetScriptID,
			"createNewScript": input.CreateNewScript,
			"chapterIds":      input.ChapterIDs,
			"idempotencyKey":  input.IdempotencyKey,
		}),
	})
	if err != nil {
		return SourceToScriptPlan{}, err
	}

	tx, err := a.db.Begin(ctx)
	if err != nil {
		return SourceToScriptPlan{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, nodeExecution); err != nil {
		return SourceToScriptPlan{}, err
	}
	var activeScriptID string
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(active_script_id::text, '')
		FROM projects
		WHERE id = $1 AND organization_id = $2
		FOR UPDATE
	`, input.ProjectID, input.OrganizationID).Scan(&activeScriptID); err != nil {
		return SourceToScriptPlan{}, err
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = strings.TrimSpace(source.Title) + " 改编剧本"
	}
	scriptID, versionID, existing, err := a.sourceScriptDraftForIdempotency(ctx, tx, input.ProjectID, source.ID, input.IdempotencyKey)
	if err != nil {
		return SourceToScriptPlan{}, err
	}
	reusedScript := existing
	previousScriptVersionID := ""
	if !existing && !input.CreateNewScript {
		var found bool
		scriptID, versionID, title, found, err = reusableSourceScriptTx(ctx, tx, input.ProjectID, source.ID, input.TargetScriptID, activeScriptID)
		if err != nil {
			return SourceToScriptPlan{}, err
		}
		reusedScript = found
		if found {
			previousScriptVersionID = versionID
			versionID, err = forkSourceScriptVersionTx(ctx, tx, input.GenerateScriptFromSourceInput, source, scriptID, versionID, chapterRefs)
			if err != nil {
				return SourceToScriptPlan{}, err
			}
		}
	}
	if !existing && !reusedScript {
		title, err = uniqueScriptTitle(ctx, tx, input.ProjectID, title)
		if err != nil {
			return SourceToScriptPlan{}, err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO scripts(organization_id, project_id, source_id, title, status, created_by)
			VALUES ($1, $2, $3, $4, 'draft', NULLIF($5, '')::uuid)
			RETURNING id::text
		`, input.OrganizationID, input.ProjectID, source.ID, title, input.CreatedBy).Scan(&scriptID); err != nil {
			return SourceToScriptPlan{}, err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO script_versions(
				organization_id, project_id, script_id, version_no, version, content,
				content_format, status, source_type, metadata, created_by
			)
			VALUES ($1, $2, $3, 1, 1, '', 'markdown', 'active', 'agent_generated', $4, NULLIF($5, '')::uuid)
			RETURNING id::text
		`, input.OrganizationID, input.ProjectID, scriptID, mustJSON(map[string]any{
			"source":           "source_to_script",
			"sourceId":         source.ID,
			"sourceType":       source.SourceType,
			"sourceTitle":      source.Title,
			"sourceChapterIds": input.ChapterIDs,
			"agentTaskId":      input.AgentTaskID,
			"agentStepId":      input.AgentStepID,
			"idempotencyKey":   input.IdempotencyKey,
			"generationStatus": "running",
		}), input.CreatedBy).Scan(&versionID); err != nil {
			return SourceToScriptPlan{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE scripts SET current_version_id = $2 WHERE id = $1`, scriptID, versionID); err != nil {
			return SourceToScriptPlan{}, err
		}
	} else if existing {
		if err := tx.QueryRow(ctx, `
			SELECT title, COALESCE(current_version_id::text, '')
			FROM scripts
			WHERE project_id = $1 AND id = $2
		`, input.ProjectID, scriptID).Scan(&title, &previousScriptVersionID); err != nil {
			return SourceToScriptPlan{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_sources
		SET status = CASE WHEN status <> 'archived' THEN 'processing' ELSE status END,
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
	`, input.ProjectID, source.ID); err != nil {
		return SourceToScriptPlan{}, err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "script.generation.prepared", "script", scriptID, mustJSON(map[string]any{
		"scriptId":                scriptID,
		"scriptVersionId":         versionID,
		"sourceId":                source.ID,
		"workflowRunId":           input.WorkflowRunID,
		"episodeTotal":            maxInt(1, len(chapterRefs)),
		"seriesEpisodeTotal":      seriesEpisodeTotal,
		"idempotent":              existing,
		"reusedScript":            reusedScript,
		"previousActiveScriptId":  activeScriptID,
		"previousScriptVersionId": previousScriptVersionID,
	})); err != nil {
		return SourceToScriptPlan{}, err
	}
	plan := SourceToScriptPlan{
		SourceID:                source.ID,
		SourceType:              source.SourceType,
		SourceTitle:             source.Title,
		ScriptID:                scriptID,
		ScriptVersionID:         versionID,
		PreviousScriptVersionID: previousScriptVersionID,
		PreviousActiveScriptID:  activeScriptID,
		Title:                   title,
		EpisodeTotal:            maxInt(1, len(chapterRefs)),
		SeriesEpisodeTotal:      seriesEpisodeTotal,
		Chapters:                chapterRefs,
	}
	if err := queueSourceScriptEpisodeNodesTx(ctx, tx, input.GenerateScriptFromSourceInput, plan); err != nil {
		return SourceToScriptPlan{}, err
	}
	if _, err := completeNodeRunTx(ctx, tx, nodeExecution, mustJSON(plan)); err != nil {
		return SourceToScriptPlan{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SourceToScriptPlan{}, err
	}
	return plan, nil
}

func (a Activities) GenerateSourceScriptEpisode(ctx context.Context, input GenerateSourceScriptEpisodeInput) (SourceScriptEpisodeOutput, error) {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.SourceID) == "" || strings.TrimSpace(input.GenerationID) == "" || strings.TrimSpace(input.ScriptID) == "" {
		return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(workflowError{Code: provider.CodeInvalidRequest, Message: "organizationId, projectId, workflowRunId, sourceId, generationId, and scriptId are required"})
	}
	if existing, ok, err := a.completedSourceScriptGenerationResult(ctx, input); err != nil {
		return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(workflowError{Code: codeActivityFailed, Message: err.Error()})
	} else if ok {
		existing.Content = ""
		if err := a.completeReplayedSourceScriptEpisode(ctx, input, existing); err != nil {
			return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(err)
		}
		return existing, nil
	}
	generation, generationItem, err := a.loadSourceToScriptGenerationItem(ctx, input)
	if err != nil {
		return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if err := a.ensureSourceToScriptGenerationItemCurrent(ctx, generation, generationItem); err != nil {
		return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(err)
	}
	project := generation.Project
	source := ProjectSourceRecord{
		ID: generation.SourceID, SourceType: generation.SourceType,
		Title: generation.Manifest.SourceTitle, Content: generationItem.SourceContent,
		ContentFormat: "plain_text", Status: "processing",
		ContentRevision: generation.SourceRevision, ContentHash: generation.SourceContentHash,
	}

	promptKey := generation.PromptTemplateKey
	sourceVariables := map[string]any{
		"id":         source.ID,
		"title":      source.Title,
		"sourceType": source.SourceType,
		"content":    generationItem.SourceContent,
	}
	instruction := strings.TrimSpace(input.Instruction)
	sourceChapterID := generationItem.SourceChapterID
	sourceChapterContent := generationItem.SourceContent
	if source.SourceType == "novel" {
		chapterContext := scriptNovelChapterContext{
			ID: sourceChapterID, ChapterIndex: generationItem.ManifestOrdinal,
			VolumeIndex: generationItem.VolumeIndex, SectionIndex: generationItem.SectionIndex,
			Title: generationItem.ChapterTitle, Content: generationItem.SourceContent,
		}
		instruction = workflowScriptEpisodeInstruction(input.Instruction, input.EpisodeIndex, input.EpisodeTotal, chapterContext)
		sourceVariables["content"] = workflowScriptNovelCurrentText([]scriptNovelChapterContext{chapterContext})
		sourceVariables["chapterIds"] = []string{chapterContext.ID}
		sourceVariables["chapters"] = []scriptNovelChapterContext{chapterContext}
	}
	rendered, err := a.renderWorkflowPromptVersion(ctx, input.OrganizationID, input.ProjectID, promptKey, generation.PromptVersionID, map[string]any{
		"project": project.asPromptVariables(),
		"source":  sourceVariables,
		"input":   map[string]any{"instruction": instruction},
	})
	if err != nil {
		return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(err)
	}
	if normalizePromptContentHash(rendered.ContentHash) != generation.PromptContentHash {
		return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(workflowError{Code: codeSourceToScriptReplanRequired, Message: "剧本 Prompt 版本内容已变化，请重新创建任务"})
	}
	baseRendered := rendered
	if source.SourceType == "novel" {
		rendered = sourceScriptFidelityPrompt(baseRendered, 1, nil)
	}
	nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        SourceToScriptEpisodeNodeKey(sourceChapterID, input.EpisodeIndex),
		NodeType:       SourceToScriptEpisodeNodeType,
		Input: mustJSON(map[string]any{
			"sourceId":            input.SourceID,
			"sourceChapterId":     sourceChapterID,
			"scriptId":            input.ScriptID,
			"generationId":        generation.ID,
			"baseScriptVersionId": generation.BaseScriptVersionID,
			"episodeIndex":        input.EpisodeIndex,
			"episodeTotal":        input.EpisodeTotal,
			"modelProfileKey":     project.ScriptModelProfileKey,
			"promptTemplateKey":   rendered.TemplateKey,
			"promptVersionId":     rendered.PromptVersionID,
			"promptHash":          rendered.RenderedHash,
			"promptSource":        rendered.Source,
		}),
		AttemptGeneration: input.AttemptGeneration,
	})
	if err != nil {
		return SourceScriptEpisodeOutput{}, err
	}
	agentRunID, err := a.startScriptAgentRun(ctx, GenerateScriptFromSourceInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		SourceID:       input.SourceID,
		Instruction:    instruction,
	}, source, rendered)
	if err != nil {
		return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if a.gateway == nil {
		err := workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"}
		_ = a.failAgentRun(ctx, agentRunID, err.Code, err.Message)
		return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(err)
	}
	executionIdentity, err := loadWorkflowExecutionIdentity(ctx, a.db, input.WorkflowRunID)
	if err != nil {
		return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	logicalNodeKey := SourceToScriptEpisodeNodeKey(sourceChapterID, input.EpisodeIndex)
	maxAttempts := 1
	if source.SourceType == "novel" {
		maxAttempts = sourceScriptFidelityMaxAttempts
	}
	var gatewayResp provider.GatewayTextResponse
	var content string
	var fidelityErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if source.SourceType == "novel" && attempt > 1 {
			rendered = sourceScriptFidelityPrompt(baseRendered, attempt, fidelityErr)
		}
		idempotencyKey := stableProviderRequestKey(
			"source-script",
			executionIdentity,
			logicalNodeKey,
			strings.Join([]string{input.SourceID, sourceChapterID, rendered.RenderedHash, strconv.Itoa(attempt)}, ":"),
		)
		gatewayResp, err = a.generateProviderText(ctx, nodeRunID, provider.GatewayTextRequest{
			OrganizationID:    input.OrganizationID,
			ProjectID:         input.ProjectID,
			WorkflowRunID:     input.WorkflowRunID,
			NodeRunID:         nodeRunID.NodeRunID,
			ModelProfileKey:   project.ScriptModelProfileKey,
			ProviderModelID:   generation.ProviderModelID,
			PromptTemplateKey: rendered.TemplateKey,
			PromptVersionID:   rendered.PromptVersionID,
			PromptHash:        rendered.RenderedHash,
			PromptSource:      rendered.Source,
			IdempotencyKey:    idempotencyKey,
			Input:             mustJSON(map[string]any{"prompt": rendered.RenderedText}),
			Options: provider.GatewayTextOptions{
				TimeoutMS: providerTextGatewayTimeoutMS, IdempotencyKey: idempotencyKey,
			},
		})
		if err != nil {
			cause := workflowErrorFromProvider(err, codeActivityFailed)
			code, message := workflowErrorFields(cause, codeActivityFailed)
			_ = a.failAgentRun(ctx, agentRunID, code, message)
			return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(cause)
		}
		content = strings.TrimSpace(gatewayResp.Output.Text)
		if content == "" {
			content = strings.TrimSpace(string(gatewayResp.Output.Raw))
		}
		if content == "" {
			err := workflowError{Code: provider.CodeInvalidRequest, Message: "provider gateway returned empty script content"}
			_ = a.failAgentRun(ctx, agentRunID, err.Code, err.Message)
			return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(err)
		}
		if source.SourceType != "novel" {
			break
		}
		fidelityErr = validateNovelScriptFidelity(sourceChapterContent, content)
		if fidelityErr == nil {
			break
		}
		if attempt == maxAttempts {
			err := workflowError{Code: provider.CodeInvalidRequest, Message: fmt.Sprintf("小说剧本在 %d 轮生成后仍未通过忠实度校验：%s", maxAttempts, fidelityErr.Error())}
			_ = a.failAgentRun(ctx, agentRunID, err.Code, err.Message)
			return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(err)
		}
	}
	output, err := a.storeSourceScriptGenerationResult(ctx, input, generation, generationItem, nodeRunID, rendered, gatewayResp, agentRunID, content)
	if err != nil {
		return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	checkpoint := output
	checkpoint.Content = ""
	return checkpoint, nil
}

func (a Activities) FinalizeScriptFromSource(ctx context.Context, input GenerateScriptFromSourceInput, plan SourceToScriptPlan, finalization SourceToScriptFinalization) (SourceToScriptOutput, error) {
	if existing, ok, err := a.completedSourceToScriptOutput(ctx, input.WorkflowRunID); err != nil {
		return SourceToScriptOutput{}, err
	} else if ok {
		return existing, nil
	}
	execution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeFinalizeScriptFromSourceKey,
		NodeType:       "workflow.script_finalize",
		Input: mustJSON(map[string]any{
			"generationId": plan.GenerationID, "sourceId": plan.SourceID,
			"scriptId": plan.ScriptID, "baseScriptVersionId": plan.BaseScriptVersionID,
			"requestedEpisodeCount": finalization.RequestedEpisodeCount,
			"completedEpisodeCount": finalization.CompletedEpisodeCount,
			"failedEpisodeCount":    finalization.FailedEpisodeCount,
		}),
	})
	if err != nil {
		if existing, ok, replayErr := a.completedSourceToScriptOutput(ctx, input.WorkflowRunID); replayErr == nil && ok {
			return existing, nil
		}
		return SourceToScriptOutput{}, err
	}
	output, err := a.finalizeScriptFromSourceGeneration(ctx, input, plan, finalization, execution)
	if err != nil {
		return SourceToScriptOutput{}, err
	}
	return output, nil
}

func (a Activities) completedSourceToScriptOutput(ctx context.Context, workflowRunID string) (SourceToScriptOutput, bool, error) {
	var raw json.RawMessage
	err := a.db.QueryRow(ctx, `
		SELECT output
		FROM workflow_node_runs
		WHERE workflow_run_id = $1 AND node_key = $2 AND status = 'succeeded'
	`, workflowRunID, nodeFinalizeScriptFromSourceKey).Scan(&raw)
	if err == pgx.ErrNoRows {
		return SourceToScriptOutput{}, false, nil
	}
	if err != nil {
		return SourceToScriptOutput{}, false, err
	}
	var output SourceToScriptOutput
	if err := json.Unmarshal(raw, &output); err != nil {
		return SourceToScriptOutput{}, false, err
	}
	return output, true, nil
}

func (a Activities) finalizeScriptFromSourceLegacy(ctx context.Context, input GenerateScriptFromSourceInput, plan SourceToScriptPlan, finalization SourceToScriptFinalization) (SourceToScriptOutput, error) {
	baseInput := TextToStoryboardInput{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, Prompt: "source_to_script", CreatedBy: input.CreatedBy}
	nodeExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeFinalizeScriptFromSourceKey,
		NodeType:       "workflow.script_finalize",
		Input: mustJSON(map[string]any{
			"sourceId":              plan.SourceID,
			"scriptId":              plan.ScriptID,
			"scriptVersionId":       plan.ScriptVersionID,
			"requestedEpisodeCount": finalization.RequestedEpisodeCount,
			"completedEpisodeCount": finalization.CompletedEpisodeCount,
			"failedEpisodeCount":    finalization.FailedEpisodeCount,
		}),
	})
	if err != nil {
		return SourceToScriptOutput{}, err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return SourceToScriptOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, nodeExecution); err != nil {
		return SourceToScriptOutput{}, err
	}

	rows, err := tx.Query(ctx, `
		SELECT content, COALESCE(provider_call_id::text, ''), COALESCE(metadata->>'modelId', '')
		FROM script_episodes
		WHERE project_id = $1 AND script_version_id = $2
		ORDER BY episode_index ASC
	`, input.ProjectID, plan.ScriptVersionID)
	if err != nil {
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	defer rows.Close()
	parts := []string{}
	providerCallIDs := []string{}
	modelIDs := []string{}
	for rows.Next() {
		var content, providerCallID, modelID string
		if err := rows.Scan(&content, &providerCallID, &modelID); err != nil {
			return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
		}
		if strings.TrimSpace(content) != "" {
			parts = append(parts, strings.TrimSpace(content))
		}
		if providerCallID != "" {
			providerCallIDs = append(providerCallIDs, providerCallID)
		}
		modelIDs = appendUniqueString(modelIDs, modelID)
	}
	if err := rows.Err(); err != nil {
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	rows.Close()
	content, err := a.rebuildScriptVersionContentTx(ctx, tx, input.ProjectID, plan.ScriptVersionID)
	if err != nil {
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if strings.TrimSpace(content) == "" && len(parts) > 0 {
		content = strings.Join(parts, "\n\n")
	}
	var generatedEpisodeCount int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM script_episodes
		WHERE project_id = $1 AND script_version_id = $2 AND COALESCE(content, '') <> ''
	`, input.ProjectID, plan.ScriptVersionID).Scan(&generatedEpisodeCount); err != nil {
		return SourceToScriptOutput{}, err
	}
	status := sourceToScriptFinalStatus(plan.EpisodeTotal, generatedEpisodeCount, finalization)
	failedEpisodes, err := sourceToScriptFailedEpisodeIndexes(ctx, tx, input.WorkflowRunID)
	if err != nil {
		return SourceToScriptOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE script_versions
		SET metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'generationStatus', $3::text,
		      'completedAt', now(),
		      'episodeCount', $4::int,
		      'requestedEpisodeCount', $5::int,
		      'completedEpisodeCount', $6::int,
		      'failedEpisodeCount', $7::int,
		      'failedEpisodes', $8::jsonb,
		      'providerCallId', NULLIF($9, ''),
		      'providerCallIds', $10::jsonb,
		      'modelId', NULLIF($11, ''),
		      'modelIds', $12::jsonb
		    )
		WHERE project_id = $1 AND id = $2
	`, input.ProjectID, plan.ScriptVersionID, status, generatedEpisodeCount,
		finalization.RequestedEpisodeCount, finalization.CompletedEpisodeCount, finalization.FailedEpisodeCount,
		mustJSON(failedEpisodes), firstStringValue(providerCallIDs), mustJSON(providerCallIDs), firstStringValue(modelIDs), mustJSON(modelIDs)); err != nil {
		return SourceToScriptOutput{}, err
	}
	activated := false
	projectActivated := false
	if finalization.CompletedEpisodeCount > 0 {
		var currentVersionID string
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(current_version_id::text, '')
			FROM scripts
			WHERE project_id = $1 AND id = $2
			FOR UPDATE
		`, input.ProjectID, plan.ScriptID).Scan(&currentVersionID); err != nil {
			return SourceToScriptOutput{}, err
		}
		if currentVersionID != plan.ScriptVersionID && currentVersionID != plan.PreviousScriptVersionID {
			return SourceToScriptOutput{}, newWorkflowApplicationError(
				workflowError{Code: "SCRIPT_VERSION_CONFLICT", Message: "剧本版本已发生变化，生成结果未覆盖用户的新版本"},
				"SCRIPT_VERSION_CONFLICT",
				"剧本版本已发生变化，生成结果未覆盖用户的新版本",
			)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE scripts
			SET status = 'active', current_version_id = $2, updated_at = now()
			WHERE project_id = $1 AND id = $3
		`, input.ProjectID, plan.ScriptVersionID, plan.ScriptID); err != nil {
			return SourceToScriptOutput{}, err
		}
		activated = true
		tag, err := tx.Exec(ctx, `
			UPDATE projects
			SET active_script_id = $2, updated_at = now()
			WHERE id = $1
			  AND organization_id = $4
			  AND (
			    active_script_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid
			    OR active_script_id = $2
			  )
		`, input.ProjectID, plan.ScriptID, plan.PreviousActiveScriptID, input.OrganizationID)
		if err != nil {
			return SourceToScriptOutput{}, err
		}
		projectActivated = tag.RowsAffected() > 0
		if plan.PreviousScriptVersionID != "" && plan.PreviousScriptVersionID != plan.ScriptVersionID {
			if err := production.MarkScriptVersionDownstreamStale(ctx, tx, input.ProjectID, plan.PreviousScriptVersionID); err != nil {
				return SourceToScriptOutput{}, err
			}
		}
		if projectActivated {
			if err := production.MarkFinalVideoStale(ctx, tx, input.ProjectID, ""); err != nil {
				return SourceToScriptOutput{}, err
			}
		}
	}
	sourceStatus := "processing"
	if status == "succeeded" {
		sourceStatus = "processed"
	} else if status == "failed" {
		sourceStatus = "ready"
	}
	if _, err := tx.Exec(ctx, `UPDATE project_sources SET status = $3, updated_at = now() WHERE project_id = $1 AND id = $2 AND status <> 'archived'`, input.ProjectID, plan.SourceID, sourceStatus); err != nil {
		return SourceToScriptOutput{}, err
	}
	output := SourceToScriptOutput{
		Status:          status,
		SourceID:        plan.SourceID,
		ScriptID:        plan.ScriptID,
		ScriptVersionID: plan.ScriptVersionID,
		ProviderCallID:  firstStringValue(providerCallIDs),
		ProviderCallIDs: providerCallIDs,
		ModelID:         firstStringValue(modelIDs),
		ModelIDs:        modelIDs,
		EpisodeCount:    generatedEpisodeCount,
		TotalItems:      finalization.RequestedEpisodeCount,
		CompletedItems:  finalization.CompletedEpisodeCount,
		FailedItems:     finalization.FailedEpisodeCount,
		FailedEpisodes:  failedEpisodes,
		Content:         content,
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "script.generated", "script", plan.ScriptID, mustJSON(map[string]any{
		"scriptId":         plan.ScriptID,
		"scriptVersionId":  plan.ScriptVersionID,
		"sourceId":         plan.SourceID,
		"workflowRunId":    input.WorkflowRunID,
		"status":           status,
		"episodeCount":     generatedEpisodeCount,
		"completedItems":   finalization.CompletedEpisodeCount,
		"failedItems":      finalization.FailedEpisodeCount,
		"failedEpisodes":   failedEpisodes,
		"activated":        activated,
		"projectActivated": projectActivated,
	})); err != nil {
		return SourceToScriptOutput{}, err
	}
	if _, err := completeNodeRunTx(ctx, tx, nodeExecution, mustJSON(output)); err != nil {
		return SourceToScriptOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SourceToScriptOutput{}, err
	}
	return output, nil
}

func sourceToScriptFinalStatus(episodeTotal, generatedEpisodeCount int, finalization SourceToScriptFinalization) string {
	if finalization.FailedEpisodeCount > 0 {
		if finalization.CompletedEpisodeCount > 0 {
			return "partial_succeeded"
		}
		return "failed"
	}
	requestedEpisodeCount := finalization.RequestedEpisodeCount
	if requestedEpisodeCount <= 0 {
		requestedEpisodeCount = maxInt(1, episodeTotal)
	}
	completedEpisodeCount := finalization.CompletedEpisodeCount
	if finalization.RequestedEpisodeCount <= 0 && completedEpisodeCount == 0 {
		completedEpisodeCount = minInt(generatedEpisodeCount, requestedEpisodeCount)
	}
	if completedEpisodeCount >= requestedEpisodeCount {
		return "succeeded"
	}
	if completedEpisodeCount > 0 {
		return "partial_succeeded"
	}
	return "failed"
}

func sourceToScriptFailedEpisodeIndexes(ctx context.Context, tx pgx.Tx, workflowRunID string) ([]int, error) {
	rows, err := tx.Query(ctx, `
		SELECT input->>'episodeIndex'
		FROM workflow_node_runs
		WHERE workflow_run_id = $1
		  AND node_type = $2
		  AND status = 'failed'
		ORDER BY created_at, node_key
	`, workflowRunID, SourceToScriptEpisodeNodeType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	indexes := make([]int, 0)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		index, err := strconv.Atoi(strings.TrimSpace(raw))
		if err == nil && index > 0 {
			indexes = append(indexes, index)
		}
	}
	return indexes, rows.Err()
}

func sourceToScriptChapterRefs(chapters []novelChapterRecord) []SourceToScriptChapterRef {
	out := make([]SourceToScriptChapterRef, 0, len(chapters))
	for _, chapter := range chapters {
		out = append(out, SourceToScriptChapterRef{
			ID:           chapter.ID,
			ChapterIndex: chapter.ChapterIndex,
			VolumeIndex:  chapter.VolumeIndex,
			SectionIndex: chapter.SectionIndex,
			VolumeTitle:  chapter.VolumeTitle,
			Title:        chapterTitle(chapter),
		})
	}
	return out
}

func (a Activities) sourceScriptDraftForIdempotency(ctx context.Context, tx pgx.Tx, projectID, sourceID, idempotencyKey string) (string, string, bool, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return "", "", false, nil
	}
	var scriptID, versionID string
	err := tx.QueryRow(ctx, `
		SELECT s.id::text, sv.id::text
		FROM script_versions sv
		JOIN scripts s ON s.id = sv.script_id
		WHERE sv.project_id = $1
		  AND sv.metadata->>'source' = 'source_to_script'
		  AND sv.metadata->>'idempotencyKey' = $2
		  AND s.source_id = $3
		  AND COALESCE(s.status, 'active') <> 'archived'
		  AND COALESCE(sv.status, 'active') <> 'archived'
		ORDER BY sv.created_at DESC
		LIMIT 1
	`, projectID, idempotencyKey, sourceID).Scan(&scriptID, &versionID)
	if err == pgx.ErrNoRows {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return scriptID, versionID, true, nil
}

func reusableSourceScriptTx(ctx context.Context, tx pgx.Tx, projectID, sourceID, targetScriptID, activeScriptID string) (string, string, string, bool, error) {
	load := func(scriptID string) (string, string, string, bool, error) {
		scriptID = strings.TrimSpace(scriptID)
		if scriptID == "" {
			return "", "", "", false, nil
		}
		var id, versionID, title string
		err := tx.QueryRow(ctx, `
			SELECT s.id::text, sv.id::text, s.title
			FROM scripts s
			JOIN script_versions sv ON sv.id = s.current_version_id AND sv.script_id = s.id
			WHERE s.project_id = $1
			  AND s.source_id = $2
			  AND s.id = $3
			  AND COALESCE(s.status, 'active') <> 'archived'
			  AND COALESCE(sv.status, 'active') <> 'archived'
		`, projectID, sourceID, scriptID).Scan(&id, &versionID, &title)
		if err == pgx.ErrNoRows {
			return "", "", "", false, nil
		}
		if err != nil {
			return "", "", "", false, err
		}
		return id, versionID, title, true, nil
	}

	if strings.TrimSpace(targetScriptID) != "" {
		scriptID, versionID, title, found, err := load(targetScriptID)
		if err != nil {
			return "", "", "", false, err
		}
		if !found {
			return "", "", "", false, newWorkflowApplicationError(
				workflowError{Code: provider.CodeInvalidRequest, Message: "target script is not the active script for this source"},
				provider.CodeInvalidRequest,
				"target script is not the active script for this source",
			)
		}
		return scriptID, versionID, title, true, nil
	}

	if scriptID, versionID, title, found, err := load(activeScriptID); err != nil || found {
		return scriptID, versionID, title, found, err
	}

	var scriptID, versionID, title string
	err := tx.QueryRow(ctx, `
		SELECT s.id::text, sv.id::text, s.title
		FROM scripts s
		JOIN script_versions sv ON sv.id = s.current_version_id AND sv.script_id = s.id
		WHERE s.project_id = $1
		  AND s.source_id = $2
		  AND COALESCE(s.status, 'active') <> 'archived'
		  AND COALESCE(sv.status, 'active') <> 'archived'
		ORDER BY s.updated_at DESC, s.created_at DESC, s.id
		LIMIT 1
	`, projectID, sourceID).Scan(&scriptID, &versionID, &title)
	if err == pgx.ErrNoRows {
		return "", "", "", false, nil
	}
	if err != nil {
		return "", "", "", false, err
	}
	return scriptID, versionID, title, true, nil
}

func forkSourceScriptVersionTx(
	ctx context.Context,
	tx pgx.Tx,
	input GenerateScriptFromSourceInput,
	source ProjectSourceRecord,
	scriptID string,
	baseVersionID string,
	chapters []SourceToScriptChapterRef,
) (string, error) {
	var lockedCurrentVersionID string
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(current_version_id::text, '')
		FROM scripts
		WHERE project_id = $1 AND id = $2
		FOR UPDATE
	`, input.ProjectID, scriptID).Scan(&lockedCurrentVersionID); err != nil {
		return "", err
	}
	if lockedCurrentVersionID != baseVersionID {
		return "", newWorkflowApplicationError(
			workflowError{Code: "SCRIPT_VERSION_CONFLICT", Message: "剧本版本已发生变化，请基于最新版本重新生成"},
			"SCRIPT_VERSION_CONFLICT",
			"剧本版本已发生变化，请基于最新版本重新生成",
		)
	}

	var nextVersion int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(COALESCE(version, version_no)), 0) + 1
		FROM script_versions
		WHERE script_id = $1
	`, scriptID).Scan(&nextVersion); err != nil {
		return "", err
	}
	targetChapterIDs := make([]string, 0, len(chapters))
	targetEpisodeIndexes := make([]int32, 0, maxInt(1, len(chapters)))
	for index, chapter := range chapters {
		if strings.TrimSpace(chapter.ID) != "" {
			targetChapterIDs = append(targetChapterIDs, chapter.ID)
		}
		episodeIndex := chapter.ChapterIndex
		if episodeIndex <= 0 {
			episodeIndex = index + 1
		}
		targetEpisodeIndexes = append(targetEpisodeIndexes, int32(episodeIndex))
	}
	if source.SourceType != "novel" {
		targetEpisodeIndexes = []int32{1}
	}

	var versionID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO script_versions(
			organization_id, project_id, script_id, version_no, version, content,
			content_format, status, source_type, prompt_version_id, prompt_hash, metadata, created_by
		)
		SELECT organization_id, project_id, script_id, $4, $4, content,
		       content_format, 'active', source_type, NULL, NULL,
		       COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		         'source', 'source_to_script',
		         'sourceId', $5::text,
		         'sourceType', $6::text,
		         'sourceTitle', $7::text,
		         'sourceChapterIds', $8::jsonb,
		         'agentTaskId', NULLIF($9, ''),
		         'agentStepId', NULLIF($10, ''),
		         'idempotencyKey', NULLIF($11, ''),
		         'generationStatus', 'running',
		         'baseVersionId', $3::text,
		         'createdAt', now()
		       ),
		       NULLIF($12, '')::uuid
		FROM script_versions
		WHERE project_id = $1 AND script_id = $2 AND id = $3::uuid
		RETURNING id::text
	`, input.ProjectID, scriptID, baseVersionID, nextVersion, source.ID, source.SourceType, source.Title,
		mustJSON(targetChapterIDs), input.AgentTaskID, input.AgentStepID, input.IdempotencyKey, input.CreatedBy).Scan(&versionID); err != nil {
		return "", err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO script_episodes(
			organization_id, project_id, script_id, script_version_id, source_id, source_chapter_id,
			episode_index, volume_index, section_index, volume_title, episode_title, content, content_format,
			prompt_version_id, prompt_hash, provider_call_id, review_status, manual_override, stale_state,
			metadata, created_by, edited_by, edited_at
		)
		SELECT organization_id, project_id, script_id, $3, source_id, source_chapter_id,
		       episode_index, volume_index, section_index, volume_title, episode_title, content, content_format,
		       prompt_version_id, prompt_hash, provider_call_id, review_status, manual_override, stale_state,
		       COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		         'copiedFromVersionId', $2::text,
		         'copiedFromEpisodeId', id
		       ),
		       created_by, edited_by, edited_at
		FROM script_episodes
		WHERE project_id = $1
		  AND script_version_id = $2::uuid
		  AND NOT (episode_index = ANY($4::int[]))
		  AND (
		    cardinality($5::text[]) = 0
		    OR source_chapter_id IS NULL
		    OR NOT (source_chapter_id::text = ANY($5::text[]))
		  )
	`, input.ProjectID, baseVersionID, versionID, targetEpisodeIndexes, targetChapterIDs); err != nil {
		return "", err
	}
	return versionID, nil
}

func (a Activities) completedSourceScriptEpisode(ctx context.Context, input GenerateSourceScriptEpisodeInput) (SourceScriptEpisodeOutput, bool, error) {
	var (
		id              string
		episodeTitle    string
		content         string
		providerCallID  sql.NullString
		promptVersionID sql.NullString
		promptHash      sql.NullString
		agentRunID      sql.NullString
		modelID         sql.NullString
		sourceChapterID sql.NullString
	)
	var err error
	if strings.TrimSpace(input.Chapter.ID) != "" {
		err = a.db.QueryRow(ctx, `
			SELECT id::text, episode_title, content, provider_call_id::text, prompt_version_id::text,
			       prompt_hash, metadata->>'agentRunId', metadata->>'modelId', source_chapter_id::text
			FROM script_episodes
			WHERE project_id = $1 AND script_version_id = $2 AND source_chapter_id = $3
			  AND COALESCE(content, '') <> ''
			LIMIT 1
		`, input.ProjectID, input.ScriptVersionID, input.Chapter.ID).Scan(&id, &episodeTitle, &content, &providerCallID, &promptVersionID, &promptHash, &agentRunID, &modelID, &sourceChapterID)
	} else {
		err = a.db.QueryRow(ctx, `
			SELECT id::text, episode_title, content, provider_call_id::text, prompt_version_id::text,
			       prompt_hash, metadata->>'agentRunId', metadata->>'modelId', source_chapter_id::text
			FROM script_episodes
			WHERE project_id = $1 AND script_version_id = $2 AND episode_index = $3
			  AND COALESCE(content, '') <> ''
			LIMIT 1
		`, input.ProjectID, input.ScriptVersionID, input.EpisodeIndex).Scan(&id, &episodeTitle, &content, &providerCallID, &promptVersionID, &promptHash, &agentRunID, &modelID, &sourceChapterID)
	}
	if err == pgx.ErrNoRows {
		return SourceScriptEpisodeOutput{}, false, nil
	}
	if err != nil {
		return SourceScriptEpisodeOutput{}, false, err
	}
	return SourceScriptEpisodeOutput{
		SourceID:        input.SourceID,
		SourceChapterID: sourceChapterID.String,
		ScriptID:        input.ScriptID,
		ScriptVersionID: input.ScriptVersionID,
		EpisodeID:       id,
		EpisodeIndex:    input.EpisodeIndex,
		EpisodeTitle:    episodeTitle,
		AgentRunID:      agentRunID.String,
		ProviderCallID:  providerCallID.String,
		ModelID:         modelID.String,
		PromptVersionID: promptVersionID.String,
		PromptHash:      promptHash.String,
		Content:         content,
		Skipped:         true,
	}, true, nil
}

func (a Activities) upsertGeneratedScriptEpisodeFromSource(ctx context.Context, input GenerateSourceScriptEpisodeInput, source ProjectSourceRecord, execution NodeExecution, sourceChapterID, episodeTitle string, rendered promptsvc.RenderedPrompt, gatewayResp provider.GatewayTextResponse, agentRunID, content string) (SourceScriptEpisodeOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return SourceScriptEpisodeOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return SourceScriptEpisodeOutput{}, err
	}
	episodeIndex := input.EpisodeIndex
	if episodeIndex <= 0 {
		episodeIndex = 1
	}
	episodeTitle = strings.TrimSpace(episodeTitle)
	if episodeTitle == "" {
		episodeTitle = "第 " + strconv.Itoa(episodeIndex) + " 集"
	}
	volumeIndex := input.Chapter.VolumeIndex
	sectionIndex := input.Chapter.SectionIndex
	volumeTitle := strings.TrimSpace(input.Chapter.VolumeTitle)
	metadata := mustJSON(map[string]any{
		"agentRunId":         agentRunID,
		"source":             "source_to_script",
		"promptTemplateKey":  rendered.TemplateKey,
		"promptVersionId":    rendered.PromptVersionID,
		"promptHash":         rendered.RenderedHash,
		"promptSource":       rendered.Source,
		"providerCallId":     gatewayResp.ProviderCallID,
		"modelId":            gatewayResp.ModelID,
		"sourceChapterId":    sourceChapterID,
		"sourceChapterTitle": episodeTitle,
		"batchIndex":         input.BatchIndex,
		"batchTotal":         input.BatchTotal,
	})
	conflictTarget := "(script_version_id, episode_index)"
	conflictIdentityUpdate := "source_chapter_id = COALESCE(script_episodes.source_chapter_id, EXCLUDED.source_chapter_id),"
	if strings.TrimSpace(sourceChapterID) != "" {
		conflictTarget = "(script_version_id, source_chapter_id) WHERE source_chapter_id IS NOT NULL"
		conflictIdentityUpdate = "episode_index = EXCLUDED.episode_index, source_chapter_id = EXCLUDED.source_chapter_id,"
	}
	var episodeID string
	query := fmt.Sprintf(`
		INSERT INTO script_episodes(
			organization_id, project_id, script_id, script_version_id, source_id, source_chapter_id,
			episode_index, volume_index, section_index, volume_title, episode_title, content, content_format,
			prompt_version_id, prompt_hash, provider_call_id, review_status, stale_state, metadata, created_by
		)
		VALUES (
			$1, $2, $3, $4, $5, NULLIF($6, '')::uuid,
			$7, NULLIF($8, 0), NULLIF($9, 0), NULLIF($10, ''), $11, $12, 'markdown',
			NULLIF($13, '')::uuid, NULLIF($14, ''),
			(SELECT id FROM provider_call_logs WHERE id = NULLIF($15, '')::uuid),
			'pending', 'fresh', $16, NULLIF($17, '')::uuid
		)
		ON CONFLICT %s DO UPDATE SET
			source_id = EXCLUDED.source_id,
			%s
			volume_index = EXCLUDED.volume_index,
			section_index = EXCLUDED.section_index,
			volume_title = EXCLUDED.volume_title,
			episode_title = EXCLUDED.episode_title,
			content = EXCLUDED.content,
			content_format = EXCLUDED.content_format,
			prompt_version_id = EXCLUDED.prompt_version_id,
			prompt_hash = EXCLUDED.prompt_hash,
			provider_call_id = EXCLUDED.provider_call_id,
			review_status = 'pending',
			stale_state = 'fresh',
			metadata = COALESCE(script_episodes.metadata, '{}'::jsonb) || EXCLUDED.metadata,
			updated_at = now()
		RETURNING id::text
	`, conflictTarget, conflictIdentityUpdate)
	if err := tx.QueryRow(ctx, query, input.OrganizationID, input.ProjectID, input.ScriptID, input.ScriptVersionID, source.ID, sourceChapterID,
		episodeIndex, volumeIndex, sectionIndex, volumeTitle, episodeTitle, strings.TrimSpace(content),
		rendered.PromptVersionID, rendered.RenderedHash, gatewayResp.ProviderCallID, metadata, input.CreatedBy).Scan(&episodeID); err != nil {
		return SourceScriptEpisodeOutput{}, err
	}
	if _, err := a.rebuildScriptVersionContentTx(ctx, tx, input.ProjectID, input.ScriptVersionID); err != nil {
		return SourceScriptEpisodeOutput{}, err
	}
	output := SourceScriptEpisodeOutput{
		SourceID:        source.ID,
		SourceChapterID: sourceChapterID,
		ScriptID:        input.ScriptID,
		ScriptVersionID: input.ScriptVersionID,
		EpisodeID:       episodeID,
		EpisodeIndex:    episodeIndex,
		EpisodeTitle:    episodeTitle,
		AgentRunID:      agentRunID,
		ProviderCallID:  gatewayResp.ProviderCallID,
		ModelID:         gatewayResp.ModelID,
		PromptVersionID: rendered.PromptVersionID,
		PromptHash:      rendered.RenderedHash,
		Content:         strings.TrimSpace(content),
	}
	if _, err := tx.Exec(ctx, `
		UPDATE agent_runs
		SET status = 'succeeded', output = $2, provider_call_id = NULLIF($3, '')::uuid,
		    prompt_version_id = NULLIF($4, '')::uuid, prompt_hash = NULLIF($5, ''), completed_at = now()
		WHERE id = $1
	`, agentRunID, mustJSON(output), gatewayResp.ProviderCallID, rendered.PromptVersionID, rendered.RenderedHash); err != nil {
		return SourceScriptEpisodeOutput{}, err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "script.episode.generated", "script_episode", episodeID, mustJSON(map[string]any{
		"scriptId":        input.ScriptID,
		"scriptVersionId": input.ScriptVersionID,
		"sourceId":        source.ID,
		"sourceChapterId": sourceChapterID,
		"episodeIndex":    episodeIndex,
		"workflowRunId":   input.WorkflowRunID,
		"agentRunId":      agentRunID,
		"providerCallId":  gatewayResp.ProviderCallID,
	})); err != nil {
		return SourceScriptEpisodeOutput{}, err
	}
	checkpoint := output
	checkpoint.Content = ""
	if _, err := completeNodeRunTx(ctx, tx, execution, mustJSON(checkpoint)); err != nil {
		return SourceScriptEpisodeOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SourceScriptEpisodeOutput{}, err
	}
	return output, nil
}

func (a Activities) rebuildScriptVersionContentTx(ctx context.Context, tx pgx.Tx, projectID, versionID string) (string, error) {
	rows, err := tx.Query(ctx, `
		SELECT episode_index, volume_index, section_index, volume_title, episode_title, content
		FROM script_episodes
		WHERE project_id = $1 AND script_version_id = $2
		ORDER BY episode_index ASC
	`, projectID, versionID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	parts := []string{}
	for rows.Next() {
		var episodeIndex int
		var volumeIndex, sectionIndex sql.NullInt32
		var volumeTitle sql.NullString
		var episodeTitle, content string
		if err := rows.Scan(&episodeIndex, &volumeIndex, &sectionIndex, &volumeTitle, &episodeTitle, &content); err != nil {
			return "", err
		}
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}
		titleParts := []string{}
		if volumeIndex.Valid && volumeIndex.Int32 > 0 {
			titleParts = append(titleParts, "第 "+strconv.Itoa(int(volumeIndex.Int32))+"卷")
		}
		if sectionIndex.Valid && sectionIndex.Int32 > 0 {
			titleParts = append(titleParts, "第 "+strconv.Itoa(int(sectionIndex.Int32))+"节")
		} else if episodeIndex > 0 {
			titleParts = append(titleParts, "第 "+strconv.Itoa(episodeIndex)+"集")
		}
		if volumeTitle.Valid && strings.TrimSpace(volumeTitle.String) != "" {
			titleParts = append(titleParts, strings.TrimSpace(volumeTitle.String))
		}
		if strings.TrimSpace(episodeTitle) != "" {
			titleParts = append(titleParts, strings.TrimSpace(episodeTitle))
		}
		title := strings.Join(titleParts, " ")
		if title == "" {
			title = "第 " + strconv.Itoa(episodeIndex) + " 集"
		}
		parts = append(parts, "## "+title+"\n\n"+content)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	content := strings.TrimSpace(strings.Join(parts, "\n\n"))
	_, err = tx.Exec(ctx, `
		UPDATE script_versions
		SET content = $3,
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'generatedEpisodeCount',
		      (SELECT count(*) FROM script_episodes WHERE project_id = $1 AND script_version_id = $2 AND COALESCE(content, '') <> ''),
		      'lastEpisodeGeneratedAt', now()
		    )
		WHERE project_id = $1 AND id = $2
	`, projectID, versionID, content)
	return content, err
}

func NormalizeSourceToScriptConcurrency(value int) int {
	if value <= 0 {
		return 2
	}
	if value > 4 {
		return 4
	}
	return value
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (a Activities) GenerateScriptFromSource(ctx context.Context, input GenerateScriptFromSourceInput) (SourceToScriptOutput, error) {
	baseInput := TextToStoryboardInput{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, Prompt: "source_to_script", CreatedBy: input.CreatedBy}
	if err := validateSourceToScriptInput(input); err != nil {
		return SourceToScriptOutput{}, err
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID, input.WorkflowRunID)
	if err != nil {
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	source, err := a.projectSourceRecord(ctx, input.ProjectID, input.SourceID)
	if err != nil {
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if source.SourceType == "novel" {
		return a.generateScriptFromNovelSource(ctx, input, source)
	}
	promptKey := promptKeyScriptAgentGenerate
	if source.SourceType == "brief" {
		promptKey = promptKeyBriefToScript
	}
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKey, map[string]any{
		"project": project.asPromptVariables(),
		"source": map[string]any{
			"id":         source.ID,
			"title":      source.Title,
			"sourceType": source.SourceType,
			"content":    source.Content,
		},
		"input": map[string]any{"instruction": strings.TrimSpace(input.Instruction)},
	})
	if err != nil {
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, err)
	}
	nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeGenerateScriptFromSourceKey,
		NodeType:       "agent.script_generate",
		Input: mustJSON(map[string]any{
			"sourceId":          input.SourceID,
			"modelProfileKey":   project.ScriptModelProfileKey,
			"promptTemplateKey": rendered.TemplateKey,
			"promptVersionId":   rendered.PromptVersionID,
			"promptHash":        rendered.RenderedHash,
			"promptSource":      rendered.Source,
		}),
	})
	if err != nil {
		return SourceToScriptOutput{}, err
	}
	agentRunID, err := a.startScriptAgentRun(ctx, input, source, rendered)
	if err != nil {
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, project.ScriptModelProfileKey, []string{"text", "multimodal"}); err != nil {
		code, message := workflowErrorFields(err, codeActivityFailed)
		_ = a.failAgentRun(ctx, agentRunID, code, message)
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, nodeRunID, err)
	}
	if a.gateway == nil {
		err := workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"}
		_ = a.failAgentRun(ctx, agentRunID, err.Code, err.Message)
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, nodeRunID, err)
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
		Input:             mustJSON(map[string]any{"prompt": rendered.RenderedText}),
		Options:           providerTextGatewayOptions(),
	})
	if err != nil {
		cause := workflowErrorFromProvider(err, codeActivityFailed)
		code, message := workflowErrorFields(cause, codeActivityFailed)
		_ = a.failAgentRun(ctx, agentRunID, code, message)
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, nodeRunID, cause)
	}
	content := strings.TrimSpace(gatewayResp.Output.Text)
	if content == "" {
		content = strings.TrimSpace(string(gatewayResp.Output.Raw))
	}
	if content == "" {
		err := workflowError{Code: provider.CodeInvalidRequest, Message: "provider gateway returned empty script content"}
		_ = a.failAgentRun(ctx, agentRunID, err.Code, err.Message)
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, nodeRunID, err)
	}
	output, err := a.createGeneratedScriptFromSource(ctx, input, source, nodeRunID, rendered, gatewayResp, agentRunID, content)
	if err != nil {
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	return output, nil
}

func (a Activities) generateScriptFromNovelSource(ctx context.Context, input GenerateScriptFromSourceInput, source ProjectSourceRecord) (SourceToScriptOutput, error) {
	baseInput := TextToStoryboardInput{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, Prompt: "source_to_script", CreatedBy: input.CreatedBy}
	eventCount, err := a.countNovelEventsForSource(ctx, input.ProjectID, source.ID)
	if err != nil {
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if eventCount == 0 {
		if _, err := a.ExtractNovelEvents(ctx, ExtractNovelEventsInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			CreatedBy:      input.CreatedBy,
			SourceID:       source.ID,
		}); err != nil {
			return SourceToScriptOutput{}, err
		}
	}
	plan, err := a.GenerateAdaptationPlan(ctx, GenerateAdaptationPlanInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		SourceID:       source.ID,
		Instruction:    input.Instruction,
	})
	if err != nil {
		return SourceToScriptOutput{}, err
	}
	script, err := a.GenerateScriptFromAdaptationPlan(ctx, GenerateScriptFromPlanInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		PlanID:         plan.PlanID,
		Title:          input.Title,
		Instruction:    input.Instruction,
	})
	if err != nil {
		return SourceToScriptOutput{}, err
	}
	return SourceToScriptOutput{
		SourceID:         source.ID,
		AdaptationPlanID: plan.PlanID,
		ScriptID:         script.ScriptID,
		ScriptVersionID:  script.ScriptVersionID,
		ProviderCallID:   script.ProviderCallID,
		ModelID:          script.ModelID,
		Content:          script.Content,
	}, nil
}

func (a Activities) CompleteSourceToScriptWorkflow(ctx context.Context, input TextToStoryboardInput, output SourceToScriptOutput) error {
	status := output.Status
	if status != "succeeded" && status != "partial_succeeded" && status != "failed" {
		status = "failed"
	}
	errorCode, errorMessage := "", ""
	if status == "failed" {
		errorCode = "SOURCE_SCRIPT_ALL_FAILED"
		errorMessage = "all selected script episodes failed"
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, applied, err := transitionWorkflowRunTx(ctx, tx, input.WorkflowRunID, status, errorCode, errorMessage, mustJSON(output))
	if err != nil {
		return err
	}
	if applied {
		if _, err := tx.Exec(ctx, `
			UPDATE workflow_runs
			SET total_items = $2, completed_items = $3, failed_items = $4, updated_at = now()
			WHERE id = $1
		`, input.WorkflowRunID, output.TotalItems, output.CompletedItems, output.FailedItems); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func resolveSourceToScriptOptions(raw json.RawMessage) SourceToScriptOptions {
	var options SourceToScriptOptions
	if len(raw) == 0 {
		return options
	}
	_ = json.Unmarshal(raw, &options)
	options.SourceID = strings.TrimSpace(options.SourceID)
	options.TargetScriptID = strings.TrimSpace(options.TargetScriptID)
	options.Instruction = strings.TrimSpace(options.Instruction)
	options.Title = strings.TrimSpace(options.Title)
	options.ChapterIDs = normalizeStringSlice(options.ChapterIDs)
	options.AgentTaskID = strings.TrimSpace(options.AgentTaskID)
	options.AgentStepID = strings.TrimSpace(options.AgentStepID)
	options.IdempotencyKey = strings.TrimSpace(options.IdempotencyKey)
	return options
}

func validateSourceToScriptInput(input GenerateScriptFromSourceInput) error {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.SourceID) == "" {
		return fmt.Errorf("organizationId, projectId, workflowRunId, and sourceId are required")
	}
	if input.CreateNewScript && strings.TrimSpace(input.TargetScriptID) != "" {
		return fmt.Errorf("scriptId and createNewScript cannot be used together")
	}
	return nil
}

func (a Activities) projectSourceRecord(ctx context.Context, projectID, sourceID string) (ProjectSourceRecord, error) {
	var item ProjectSourceRecord
	err := a.db.QueryRow(ctx, `
		SELECT id::text, source_type, title, content, content_format, COALESCE(status, 'ready'),
		       content_revision, content_hash
		FROM project_sources
		WHERE project_id = $1 AND id = $2
	`, projectID, sourceID).Scan(
		&item.ID, &item.SourceType, &item.Title, &item.Content, &item.ContentFormat, &item.Status,
		&item.ContentRevision, &item.ContentHash,
	)
	return item, err
}

func (a Activities) startScriptAgentRun(ctx context.Context, input GenerateScriptFromSourceInput, source ProjectSourceRecord, rendered promptsvc.RenderedPrompt) (string, error) {
	var runID string
	err := a.db.QueryRow(ctx, `
		INSERT INTO agent_runs(
			organization_id, project_id, agent_type, task_type, status,
			input, prompt_version_id, prompt_hash, created_by, started_at
		)
		VALUES ($1, $2, 'script_agent', 'generate_script', 'running', $3, NULLIF($4, '')::uuid, NULLIF($5, ''), $6, now())
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, mustJSON(map[string]any{
		"source": map[string]any{
			"id":         source.ID,
			"title":      source.Title,
			"sourceType": source.SourceType,
		},
		"input": map[string]any{"instruction": strings.TrimSpace(input.Instruction)},
	}), rendered.PromptVersionID, rendered.RenderedHash, input.CreatedBy).Scan(&runID)
	return runID, err
}

func (a Activities) failAgentRun(ctx context.Context, runID string, codeAndMessage ...string) error {
	if strings.TrimSpace(runID) == "" {
		return nil
	}
	code := codeActivityFailed
	message := "script agent run failed"
	if len(codeAndMessage) > 0 && strings.TrimSpace(codeAndMessage[0]) != "" {
		code = strings.TrimSpace(codeAndMessage[0])
	}
	if len(codeAndMessage) > 1 && strings.TrimSpace(codeAndMessage[1]) != "" {
		message = strings.TrimSpace(codeAndMessage[1])
	}
	_, err := a.db.Exec(ctx, `
		UPDATE agent_runs
		SET status = 'failed', error_code = $2, error_message = $3, completed_at = now()
		WHERE id = $1
	`, runID, code, message)
	return err
}

func (a Activities) createGeneratedScriptFromSource(ctx context.Context, input GenerateScriptFromSourceInput, source ProjectSourceRecord, execution NodeExecution, rendered promptsvc.RenderedPrompt, gatewayResp provider.GatewayTextResponse, agentRunID, content string) (SourceToScriptOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return SourceToScriptOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return SourceToScriptOutput{}, err
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = strings.TrimSpace(source.Title) + " Script"
	}
	var scriptID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO scripts(organization_id, project_id, source_id, title, status, created_by)
		VALUES ($1, $2, $3, $4, 'active', $5)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, source.ID, title, input.CreatedBy).Scan(&scriptID); err != nil {
		return SourceToScriptOutput{}, err
	}
	var versionID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO script_versions(
			organization_id, project_id, script_id, version_no, version, content,
			content_format, source_type, prompt_version_id, prompt_hash, metadata, created_by
		)
		VALUES ($1, $2, $3, 1, 1, $4, 'markdown', 'agent_generated', NULLIF($5, '')::uuid, NULLIF($6, ''), $7, $8)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, scriptID, content, rendered.PromptVersionID, rendered.RenderedHash, mustJSON(map[string]any{
		"source":          "source_to_script",
		"sourceId":        source.ID,
		"providerCallId":  gatewayResp.ProviderCallID,
		"modelId":         gatewayResp.ModelID,
		"promptTemplate":  rendered.TemplateKey,
		"promptVersionId": rendered.PromptVersionID,
		"promptHash":      rendered.RenderedHash,
	}), input.CreatedBy).Scan(&versionID); err != nil {
		return SourceToScriptOutput{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE scripts SET current_version_id = $2 WHERE id = $1`, scriptID, versionID); err != nil {
		return SourceToScriptOutput{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE projects SET active_script_id = $2, updated_at = now() WHERE id = $1`, input.ProjectID, scriptID); err != nil {
		return SourceToScriptOutput{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE project_sources SET status = 'processed' WHERE id = $1`, source.ID); err != nil {
		return SourceToScriptOutput{}, err
	}
	output := SourceToScriptOutput{
		SourceID:        source.ID,
		ScriptID:        scriptID,
		ScriptVersionID: versionID,
		AgentRunID:      agentRunID,
		ProviderCallID:  gatewayResp.ProviderCallID,
		ModelID:         gatewayResp.ModelID,
		Content:         content,
	}
	if _, err := tx.Exec(ctx, `
		UPDATE agent_runs
		SET status = 'succeeded', output = $2, provider_call_id = NULLIF($3, '')::uuid,
		    prompt_version_id = NULLIF($4, '')::uuid, prompt_hash = NULLIF($5, ''), completed_at = now()
		WHERE id = $1
	`, agentRunID, mustJSON(output), gatewayResp.ProviderCallID, rendered.PromptVersionID, rendered.RenderedHash); err != nil {
		return SourceToScriptOutput{}, err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "script.generated", "script", scriptID, mustJSON(map[string]any{
		"scriptId":        scriptID,
		"scriptVersionId": versionID,
		"sourceId":        source.ID,
		"workflowRunId":   input.WorkflowRunID,
		"agentRunId":      agentRunID,
	})); err != nil {
		return SourceToScriptOutput{}, err
	}
	if _, err := completeNodeRunTx(ctx, tx, execution, mustJSON(output)); err != nil {
		return SourceToScriptOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SourceToScriptOutput{}, err
	}
	return output, nil
}
