package workflows

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

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
	SourceID       string   `json:"sourceId"`
	ChapterIDs     []string `json:"chapterIds,omitempty"`
	Instruction    string   `json:"instruction,omitempty"`
	Title          string   `json:"title,omitempty"`
	MaxConcurrency int      `json:"maxConcurrency,omitempty"`
	AgentTaskID    string   `json:"agentTaskId,omitempty"`
	AgentStepID    string   `json:"agentStepId,omitempty"`
	IdempotencyKey string   `json:"idempotencyKey,omitempty"`
}

type ProjectSourceRecord struct {
	ID            string `json:"id"`
	SourceType    string `json:"sourceType"`
	Title         string `json:"title"`
	Content       string `json:"content"`
	ContentFormat string `json:"contentFormat"`
	Status        string `json:"status"`
}

type GenerateScriptFromSourceInput struct {
	OrganizationID string   `json:"organizationId"`
	ProjectID      string   `json:"projectId"`
	WorkflowRunID  string   `json:"workflowRunId"`
	CreatedBy      string   `json:"createdBy"`
	SourceID       string   `json:"sourceId"`
	ChapterIDs     []string `json:"chapterIds,omitempty"`
	Instruction    string   `json:"instruction,omitempty"`
	Title          string   `json:"title,omitempty"`
	AgentTaskID    string   `json:"agentTaskId,omitempty"`
	AgentStepID    string   `json:"agentStepId,omitempty"`
	IdempotencyKey string   `json:"idempotencyKey,omitempty"`
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
	Content          string   `json:"content"`
}

type SourceToScriptPlan struct {
	SourceID        string                     `json:"sourceId"`
	SourceType      string                     `json:"sourceType"`
	SourceTitle     string                     `json:"sourceTitle"`
	ScriptID        string                     `json:"scriptId"`
	ScriptVersionID string                     `json:"scriptVersionId"`
	Title           string                     `json:"title"`
	EpisodeTotal    int                        `json:"episodeTotal"`
	Chapters        []SourceToScriptChapterRef `json:"chapters,omitempty"`
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
	ID           string `json:"id,omitempty"`
	ChapterIndex int    `json:"chapterIndex,omitempty"`
	VolumeIndex  int    `json:"volumeIndex,omitempty"`
	SectionIndex int    `json:"sectionIndex,omitempty"`
	VolumeTitle  string `json:"volumeTitle,omitempty"`
	Title        string `json:"title,omitempty"`
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
	ScriptID          string                   `json:"scriptId"`
	ScriptVersionID   string                   `json:"scriptVersionId"`
	Instruction       string                   `json:"instruction,omitempty"`
	EpisodeIndex      int                      `json:"episodeIndex"`
	EpisodeTotal      int                      `json:"episodeTotal"`
	Chapter           SourceToScriptChapterRef `json:"chapter,omitempty"`
	AttemptGeneration int                      `json:"attemptGeneration"`
}

type SourceScriptEpisodeOutput struct {
	SourceID        string `json:"sourceId"`
	SourceChapterID string `json:"sourceChapterId,omitempty"`
	ScriptID        string `json:"scriptId"`
	ScriptVersionID string `json:"scriptVersionId"`
	EpisodeID       string `json:"episodeId"`
	EpisodeIndex    int    `json:"episodeIndex"`
	EpisodeTitle    string `json:"episodeTitle"`
	AgentRunID      string `json:"agentRunId,omitempty"`
	ProviderCallID  string `json:"providerCallId,omitempty"`
	ModelID         string `json:"modelId,omitempty"`
	PromptVersionID string `json:"promptVersionId,omitempty"`
	PromptHash      string `json:"promptHash,omitempty"`
	Content         string `json:"content,omitempty"`
	Skipped         bool   `json:"skipped,omitempty"`
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
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			CreatedBy:      input.CreatedBy,
			SourceID:       options.SourceID,
			ChapterIDs:     options.ChapterIDs,
			Instruction:    options.Instruction,
			Title:          options.Title,
			AgentTaskID:    options.AgentTaskID,
			AgentStepID:    options.AgentStepID,
			IdempotencyKey: options.IdempotencyKey,
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
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		SourceID:       options.SourceID,
		ChapterIDs:     options.ChapterIDs,
		Instruction:    options.Instruction,
		Title:          options.Title,
		AgentTaskID:    options.AgentTaskID,
		AgentStepID:    options.AgentStepID,
		IdempotencyKey: options.IdempotencyKey,
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
				ScriptID:          plan.ScriptID,
				ScriptVersionID:   plan.ScriptVersionID,
				Instruction:       options.Instruction,
				EpisodeIndex:      planIndex + 1,
				EpisodeTotal:      maxInt(1, plan.EpisodeTotal),
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
			CreatedBy: input.CreatedBy, SourceID: plan.SourceID, ScriptID: plan.ScriptID,
			ScriptVersionID: plan.ScriptVersionID, Instruction: input.Instruction,
			EpisodeIndex: planIndex + 1, EpisodeTotal: maxInt(1, plan.EpisodeTotal), Chapter: chapter,
			AttemptGeneration: attemptGeneration,
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO workflow_node_runs(
				organization_id, project_id, workflow_run_id, node_key, node_type,
				status, input, output, attempt_generation
			)
			VALUES ($1, $2, $3, $4, $7, 'queued', $5, '{}', $6)
			ON CONFLICT (workflow_run_id, node_key) DO NOTHING
		`, input.OrganizationID, input.ProjectID, input.WorkflowRunID,
			SourceToScriptEpisodeNodeKey(chapter.ID, planIndex+1), mustJSON(episode), attemptGeneration, SourceToScriptEpisodeNodeType); err != nil {
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
	return FailNodeRunWithOutput(ctx, a.db, execution, code, message, mustJSON(map[string]any{
		"episodeIndex": episode.EpisodeIndex, "sourceChapterId": episode.Chapter.ID,
		"errorCode": code, "errorMessage": message,
	}))
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
	source, err := a.projectSourceRecord(ctx, input.ProjectID, input.SourceID)
	if err != nil {
		return SourceToScriptPlan{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if source.Status == "archived" {
		return SourceToScriptPlan{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: provider.CodeInvalidRequest, Message: "archived source cannot be used for script generation"})
	}

	chapterRefs := []SourceToScriptChapterRef{}
	if source.SourceType == "novel" {
		chapters, err := a.loadNovelChapters(ctx, input.ProjectID, input.SourceID, input.ChapterIDs)
		if err != nil {
			return SourceToScriptPlan{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
		}
		if len(chapters) == 0 {
			return SourceToScriptPlan{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: provider.CodeInvalidRequest, Message: "source has no chapters to generate"})
		}
		if len(input.ChapterIDs) > 0 && len(chapters) != len(normalizeStringSlice(input.ChapterIDs)) {
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
			"sourceId":       input.SourceID,
			"chapterIds":     input.ChapterIDs,
			"idempotencyKey": input.IdempotencyKey,
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

	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = strings.TrimSpace(source.Title) + " 改编剧本"
	}
	scriptID, versionID, existing, err := a.sourceScriptDraftForIdempotency(ctx, tx, input.ProjectID, input.IdempotencyKey)
	if err != nil {
		return SourceToScriptPlan{}, err
	}
	if !existing {
		title, err = uniqueScriptTitle(ctx, tx, input.ProjectID, title)
		if err != nil {
			return SourceToScriptPlan{}, err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO scripts(organization_id, project_id, source_id, title, status, created_by)
			VALUES ($1, $2, $3, $4, 'active', NULLIF($5, '')::uuid)
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
	} else {
		if err := tx.QueryRow(ctx, `SELECT title FROM scripts WHERE project_id = $1 AND id = $2`, input.ProjectID, scriptID).Scan(&title); err != nil {
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
		"scriptId":        scriptID,
		"scriptVersionId": versionID,
		"sourceId":        source.ID,
		"workflowRunId":   input.WorkflowRunID,
		"episodeTotal":    maxInt(1, len(chapterRefs)),
		"idempotent":      existing,
	})); err != nil {
		return SourceToScriptPlan{}, err
	}
	plan := SourceToScriptPlan{
		SourceID:        source.ID,
		SourceType:      source.SourceType,
		SourceTitle:     source.Title,
		ScriptID:        scriptID,
		ScriptVersionID: versionID,
		Title:           title,
		EpisodeTotal:    maxInt(1, len(chapterRefs)),
		Chapters:        chapterRefs,
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
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.SourceID) == "" || strings.TrimSpace(input.ScriptID) == "" || strings.TrimSpace(input.ScriptVersionID) == "" {
		return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(workflowError{Code: provider.CodeInvalidRequest, Message: "organizationId, projectId, workflowRunId, sourceId, scriptId, and scriptVersionId are required"})
	}
	if existing, ok, err := a.completedSourceScriptEpisode(ctx, input); err != nil {
		return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(workflowError{Code: codeActivityFailed, Message: err.Error()})
	} else if ok {
		existing.Content = ""
		if err := a.completeReplayedSourceScriptEpisode(ctx, input, existing); err != nil {
			return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(err)
		}
		return existing, nil
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	source, err := a.projectSourceRecord(ctx, input.ProjectID, input.SourceID)
	if err != nil {
		return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if source.Status == "archived" {
		return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(workflowError{Code: provider.CodeInvalidRequest, Message: "archived source cannot be used for script generation"})
	}

	promptKey := promptKeyScriptAgentGenerate
	sourceVariables := map[string]any{
		"id":         source.ID,
		"title":      source.Title,
		"sourceType": source.SourceType,
		"content":    source.Content,
	}
	instruction := strings.TrimSpace(input.Instruction)
	episodeTitle := strings.TrimSpace(source.Title)
	sourceChapterID := ""
	if source.SourceType == "brief" {
		promptKey = promptKeyBriefToScript
	}
	if source.SourceType == "novel" {
		sourceChapterID = strings.TrimSpace(input.Chapter.ID)
		if sourceChapterID == "" {
			return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(workflowError{Code: provider.CodeInvalidRequest, Message: "chapter id is required for novel script episode generation"})
		}
		chapters, err := a.loadNovelChapters(ctx, input.ProjectID, input.SourceID, []string{sourceChapterID})
		if err != nil {
			return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(workflowError{Code: codeActivityFailed, Message: err.Error()})
		}
		if len(chapters) != 1 {
			return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(workflowError{Code: provider.CodeInvalidRequest, Message: "chapterId does not match source chapter"})
		}
		chapterContext := workflowScriptNovelChapterContexts(chapters)[0]
		episodeTitle = chapterContext.Title
		instruction = workflowScriptEpisodeInstruction(input.Instruction, input.EpisodeIndex, input.EpisodeTotal, chapterContext)
		sourceVariables["content"] = workflowScriptNovelCurrentText([]scriptNovelChapterContext{chapterContext})
		sourceVariables["chapterIds"] = []string{chapterContext.ID}
		sourceVariables["chapters"] = []scriptNovelChapterContext{chapterContext}
	}
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKey, map[string]any{
		"project": project.asPromptVariables(),
		"source":  sourceVariables,
		"input":   map[string]any{"instruction": instruction},
	})
	if err != nil {
		return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(err)
	}
	nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        SourceToScriptEpisodeNodeKey(sourceChapterID, input.EpisodeIndex),
		NodeType:       SourceToScriptEpisodeNodeType,
		Input: mustJSON(map[string]any{
			"sourceId":          input.SourceID,
			"sourceChapterId":   sourceChapterID,
			"scriptId":          input.ScriptID,
			"scriptVersionId":   input.ScriptVersionID,
			"episodeIndex":      input.EpisodeIndex,
			"episodeTotal":      input.EpisodeTotal,
			"modelProfileKey":   project.ScriptModelProfileKey,
			"promptTemplateKey": rendered.TemplateKey,
			"promptVersionId":   rendered.PromptVersionID,
			"promptHash":        rendered.RenderedHash,
			"promptSource":      rendered.Source,
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
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, project.ScriptModelProfileKey, []string{"text", "multimodal"}); err != nil {
		code, message := workflowErrorFields(err, codeActivityFailed)
		_ = a.failAgentRun(ctx, agentRunID, code, message)
		return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(err)
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
	idempotencyKey := stableProviderRequestKey(
		"source-script",
		executionIdentity,
		logicalNodeKey,
		strings.Join([]string{input.SourceID, sourceChapterID, rendered.RenderedHash}, ":"),
	)
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
	content := strings.TrimSpace(gatewayResp.Output.Text)
	if content == "" {
		content = strings.TrimSpace(string(gatewayResp.Output.Raw))
	}
	if content == "" {
		err := workflowError{Code: provider.CodeInvalidRequest, Message: "provider gateway returned empty script content"}
		_ = a.failAgentRun(ctx, agentRunID, err.Code, err.Message)
		return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(err)
	}
	output, err := a.upsertGeneratedScriptEpisodeFromSource(ctx, input, source, nodeRunID, sourceChapterID, episodeTitle, rendered, gatewayResp, agentRunID, content)
	if err != nil {
		return SourceScriptEpisodeOutput{}, sourceScriptEpisodeActivityError(workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	checkpoint := output
	checkpoint.Content = ""
	return checkpoint, nil
}

func (a Activities) FinalizeScriptFromSource(ctx context.Context, input GenerateScriptFromSourceInput, plan SourceToScriptPlan, finalization SourceToScriptFinalization) (SourceToScriptOutput, error) {
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
	scriptStatus := "draft"
	if generatedEpisodeCount > 0 {
		scriptStatus = "active"
	}
	if _, err := tx.Exec(ctx, `UPDATE scripts SET status = $4, current_version_id = $2, updated_at = now() WHERE project_id = $1 AND id = $3`, input.ProjectID, plan.ScriptVersionID, plan.ScriptID, scriptStatus); err != nil {
		return SourceToScriptOutput{}, err
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
		"scriptId":        plan.ScriptID,
		"scriptVersionId": plan.ScriptVersionID,
		"sourceId":        plan.SourceID,
		"workflowRunId":   input.WorkflowRunID,
		"status":          status,
		"episodeCount":    generatedEpisodeCount,
		"completedItems":  finalization.CompletedEpisodeCount,
		"failedItems":     finalization.FailedEpisodeCount,
		"failedEpisodes":  failedEpisodes,
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
		if generatedEpisodeCount > 0 || finalization.CompletedEpisodeCount > 0 {
			return "partial_succeeded"
		}
		return "failed"
	}
	if generatedEpisodeCount >= maxInt(1, episodeTotal) {
		return "succeeded"
	}
	if generatedEpisodeCount > 0 {
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

func (a Activities) sourceScriptDraftForIdempotency(ctx context.Context, tx pgx.Tx, projectID, idempotencyKey string) (string, string, bool, error) {
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
		  AND COALESCE(s.status, 'active') <> 'archived'
		  AND COALESCE(sv.status, 'active') <> 'archived'
		ORDER BY sv.created_at DESC
		LIMIT 1
	`, projectID, idempotencyKey).Scan(&scriptID, &versionID)
	if err == pgx.ErrNoRows {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return scriptID, versionID, true, nil
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
	})
	var episodeID string
	if err := tx.QueryRow(ctx, `
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
		ON CONFLICT (script_version_id, episode_index) DO UPDATE SET
			source_id = EXCLUDED.source_id,
			source_chapter_id = COALESCE(script_episodes.source_chapter_id, EXCLUDED.source_chapter_id),
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
	`, input.OrganizationID, input.ProjectID, input.ScriptID, input.ScriptVersionID, source.ID, sourceChapterID,
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
	project, err := a.projectProductionSettings(ctx, input.ProjectID)
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
	return nil
}

func (a Activities) projectSourceRecord(ctx context.Context, projectID, sourceID string) (ProjectSourceRecord, error) {
	var item ProjectSourceRecord
	err := a.db.QueryRow(ctx, `
		SELECT id::text, source_type, title, content, content_format, COALESCE(status, 'ready')
		FROM project_sources
		WHERE project_id = $1 AND id = $2
	`, projectID, sourceID).Scan(&item.ID, &item.SourceType, &item.Title, &item.Content, &item.ContentFormat, &item.Status)
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
