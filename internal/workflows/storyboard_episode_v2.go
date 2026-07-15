package workflows

import (
	"fmt"
	"sort"
	"strings"
	"time"

	storyboardpkg "github.com/Einzieg/cineweave/internal/storyboard"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const storyboardPlanMaximumReviewAttempts = 3

type ScriptEpisodeToStoryboardInput struct {
	OrganizationID    string                  `json:"organizationId"`
	ProjectID         string                  `json:"projectId"`
	WorkflowRunID     string                  `json:"workflowRunId"`
	CreatedBy         string                  `json:"createdBy"`
	ScriptID          string                  `json:"scriptId"`
	ScriptVersionID   string                  `json:"scriptVersionId"`
	ScriptEpisodeID   string                  `json:"scriptEpisodeId"`
	EpisodeIndex      int                     `json:"episodeIndex"`
	EpisodeTotal      int                     `json:"episodeTotal"`
	EpisodeTitle      string                  `json:"episodeTitle"`
	TimelineTimebase  int64                   `json:"timelineTimebase"`
	FPSNumerator      int                     `json:"fpsNumerator"`
	FPSDenominator    int                     `json:"fpsDenominator"`
	ProductionOptions ScriptProductionOptions `json:"productionOptions"`
}

type StoryboardScenePlanWorkflowInput struct {
	PlanStoryboardSceneInput
}

func StoryboardScenePlanWorkflow(ctx workflow.Context, input StoryboardScenePlanWorkflowInput) (PlanStoryboardSceneOutput, error) {
	ctx = workflow.WithActivityOptions(ctx, providerTextActivityOptions())
	var output PlanStoryboardSceneOutput
	if err := workflow.ExecuteActivity(ctx, "PlanStoryboardScene", input.PlanStoryboardSceneInput).Get(ctx, &output); err != nil {
		return PlanStoryboardSceneOutput{}, err
	}
	return output, nil
}

func ScriptEpisodeToStoryboardWorkflow(ctx workflow.Context, input ScriptEpisodeToStoryboardInput) (ScriptStoryboardOutput, error) {
	timebase, err := storyboardEpisodeTimebase(input)
	if err != nil {
		return ScriptStoryboardOutput{}, temporal.NewNonRetryableApplicationError(err.Error(), "INVALID_TIMEBASE", err)
	}
	providerCtx := workflow.WithActivityOptions(ctx, providerTextActivityOptions())
	activationCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())

	var targetDurationTicks *int64
	if input.ProductionOptions.TargetDurationSeconds != nil && *input.ProductionOptions.TargetDurationSeconds > 0 {
		value := timebase.SecondsToFrameTicksCeil(*input.ProductionOptions.TargetDurationSeconds)
		targetDurationTicks = &value
	}

	var timing TimingAnalysisActivityOutput
	if err := workflow.ExecuteActivity(providerCtx, "AnalyzeEpisodeTiming", AnalyzeEpisodeTimingInput{
		OrganizationID:      input.OrganizationID,
		ProjectID:           input.ProjectID,
		WorkflowRunID:       input.WorkflowRunID,
		CreatedBy:           input.CreatedBy,
		ScriptID:            input.ScriptID,
		ScriptVersionID:     input.ScriptVersionID,
		ScriptEpisodeID:     input.ScriptEpisodeID,
		TargetDurationTicks: targetDurationTicks,
	}).Get(providerCtx, &timing); err != nil {
		return ScriptStoryboardOutput{}, err
	}

	var blueprint ContinuityBlueprintActivityOutput
	if err := workflow.ExecuteActivity(providerCtx, "BuildEpisodeContinuityBlueprint", BuildEpisodeContinuityBlueprintInput{
		OrganizationID:  input.OrganizationID,
		ProjectID:       input.ProjectID,
		WorkflowRunID:   input.WorkflowRunID,
		CreatedBy:       input.CreatedBy,
		ScriptID:        input.ScriptID,
		ScriptVersionID: input.ScriptVersionID,
		ScriptEpisodeID: input.ScriptEpisodeID,
		PacingProfile:   input.ProductionOptions.PacingProfile,
		Timing:          timing,
	}).Get(providerCtx, &blueprint); err != nil {
		return ScriptStoryboardOutput{}, err
	}

	providerCallIDs := append([]string(nil), timing.ProviderCallIDs...)
	providerCallIDs = appendProviderCallID(providerCallIDs, timing.ProviderCallID)
	providerCallIDs = appendProviderCallID(providerCallIDs, blueprint.ProviderCallID)
	initialOutputs, err := executeStoryboardScenePlans(ctx, input, blueprint, blueprint.ScenePlans, 0, nil)
	if err != nil {
		return ScriptStoryboardOutput{}, err
	}
	providerCallIDs = appendSceneProviderCallIDs(providerCallIDs, initialOutputs)

	for reviewAttempt := 1; reviewAttempt <= storyboardPlanMaximumReviewAttempts; reviewAttempt++ {
		var review ReviewStoryboardPlanOutput
		if err := workflow.ExecuteActivity(providerCtx, "ReviewStoryboardPlan", ReviewStoryboardPlanInput{
			OrganizationID:   input.OrganizationID,
			ProjectID:        input.ProjectID,
			WorkflowRunID:    input.WorkflowRunID,
			CreatedBy:        input.CreatedBy,
			ScriptEpisodeID:  input.ScriptEpisodeID,
			StoryboardPlanID: blueprint.StoryboardPlanID,
			ReviewAttempt:    reviewAttempt,
		}).Get(providerCtx, &review); err != nil {
			return ScriptStoryboardOutput{}, err
		}
		providerCallIDs = appendProviderCallID(providerCallIDs, review.ProviderCallID)
		if review.Approved {
			var output ScriptStoryboardOutput
			if err := workflow.ExecuteActivity(activationCtx, "ActivateStoryboardPlan", ActivateStoryboardPlanActivityInput{
				OrganizationID:   input.OrganizationID,
				ProjectID:        input.ProjectID,
				WorkflowRunID:    input.WorkflowRunID,
				CreatedBy:        input.CreatedBy,
				ScriptID:         input.ScriptID,
				ScriptVersionID:  input.ScriptVersionID,
				ScriptEpisodeID:  input.ScriptEpisodeID,
				EpisodeIndex:     input.EpisodeIndex,
				EpisodeTotal:     input.EpisodeTotal,
				EpisodeTitle:     input.EpisodeTitle,
				StoryboardPlanID: blueprint.StoryboardPlanID,
			}).Get(activationCtx, &output); err != nil {
				return ScriptStoryboardOutput{}, err
			}
			output.ProviderCallIDs = providerCallIDs
			if len(providerCallIDs) > 0 {
				output.ProviderCallID = providerCallIDs[0]
			}
			if output.ModelID == "" {
				output.ModelID = blueprint.ModelID
			}
			return output, nil
		}
		if len(review.NeedsRevisionSceneKeys) == 0 {
			return ScriptStoryboardOutput{}, temporal.NewNonRetryableApplicationError(
				"storyboard reviewer rejected the plan without identifying a scene to revise",
				"STORYBOARD_REVIEW_UNACTIONABLE",
				nil,
			)
		}
		if reviewAttempt == storyboardPlanMaximumReviewAttempts {
			break
		}
		retryScenes, err := storyboardScenePlansByKey(blueprint.ScenePlans, review.NeedsRevisionSceneKeys)
		if err != nil {
			return ScriptStoryboardOutput{}, temporal.NewNonRetryableApplicationError(err.Error(), "STORYBOARD_REVIEW_INVALID", err)
		}
		corrections := correctionsByScene(review.Corrections)
		retryOutputs, err := executeStoryboardScenePlans(ctx, input, blueprint, retryScenes, reviewAttempt, corrections)
		if err != nil {
			return ScriptStoryboardOutput{}, err
		}
		providerCallIDs = appendSceneProviderCallIDs(providerCallIDs, retryOutputs)
	}

	return ScriptStoryboardOutput{}, temporal.NewNonRetryableApplicationError(
		fmt.Sprintf("storyboard plan was not approved after %d review attempts", storyboardPlanMaximumReviewAttempts),
		"STORYBOARD_REVIEW_EXHAUSTED",
		nil,
	)
}

func storyboardEpisodeTimebase(input ScriptEpisodeToStoryboardInput) (storyboardpkg.Timebase, error) {
	timebase := storyboardpkg.Timebase{
		TicksPerSecond: input.TimelineTimebase,
		FPSNumerator:   int64(input.FPSNumerator),
		FPSDenominator: int64(input.FPSDenominator),
	}
	if timebase.TicksPerSecond == 0 {
		timebase = storyboardpkg.DefaultTimebase()
	}
	return timebase, timebase.Validate()
}

func executeStoryboardScenePlans(
	ctx workflow.Context,
	input ScriptEpisodeToStoryboardInput,
	blueprint ContinuityBlueprintActivityOutput,
	targets []StoryboardScenePlanRef,
	retryGeneration int,
	corrections map[string][]storyboardpkg.StoryboardCorrection,
) ([]PlanStoryboardSceneOutput, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	maxConcurrency := input.ProductionOptions.MaxSceneConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 3
	}
	targetByKey := make(map[string]StoryboardScenePlanRef, len(targets))
	for _, scene := range targets {
		targetByKey[scene.SceneKey] = scene
	}
	sceneShotBudgets, err := storyboardSceneShotBudgets(blueprint.Blueprint, input.ProductionOptions.ShotBudget)
	if err != nil {
		return nil, temporal.NewNonRetryableApplicationError(err.Error(), "DURATION_CONSTRAINT_CONFLICT", err)
	}
	dependencies := storyboardSceneDependencies(blueprint.Blueprint)
	completed := make(map[string]bool, len(blueprint.ScenePlans))
	for _, scene := range blueprint.ScenePlans {
		if _, selected := targetByKey[scene.SceneKey]; !selected {
			completed[scene.SceneKey] = true
		}
	}
	pending := make(map[string]bool, len(targets))
	for key := range targetByKey {
		pending[key] = true
	}
	outputByKey := make(map[string]PlanStoryboardSceneOutput, len(targets))

	for len(pending) > 0 {
		ready := make([]StoryboardScenePlanRef, 0, len(pending))
		for key := range pending {
			if storyboardDependenciesComplete(dependencies[key], completed) {
				ready = append(ready, targetByKey[key])
			}
		}
		sort.Slice(ready, func(left, right int) bool {
			if ready[left].SceneOrdinal == ready[right].SceneOrdinal {
				return ready[left].SceneKey < ready[right].SceneKey
			}
			return ready[left].SceneOrdinal < ready[right].SceneOrdinal
		})
		if len(ready) == 0 {
			return nil, temporal.NewNonRetryableApplicationError(
				"storyboard scene dependency graph has no runnable scene",
				"STORYBOARD_DEPENDENCY_DEADLOCK",
				nil,
			)
		}
		if len(ready) > maxConcurrency {
			ready = ready[:maxConcurrency]
		}

		type runningScene struct {
			scene  StoryboardScenePlanRef
			future workflow.ChildWorkflowFuture
		}
		running := make([]runningScene, 0, len(ready))
		for _, scene := range ready {
			childOptions := storyboardSceneChildWorkflowOptions(ctx, blueprint.StoryboardPlanID, scene, retryGeneration)
			childCtx := workflow.WithChildOptions(ctx, childOptions)
			future := workflow.ExecuteChildWorkflow(childCtx, StoryboardScenePlanWorkflow, StoryboardScenePlanWorkflowInput{
				PlanStoryboardSceneInput: PlanStoryboardSceneInput{
					OrganizationID:   input.OrganizationID,
					ProjectID:        input.ProjectID,
					WorkflowRunID:    input.WorkflowRunID,
					CreatedBy:        input.CreatedBy,
					ScriptID:         input.ScriptID,
					ScriptVersionID:  input.ScriptVersionID,
					ScriptEpisodeID:  input.ScriptEpisodeID,
					StoryboardPlanID: blueprint.StoryboardPlanID,
					BlueprintID:      blueprint.BlueprintID,
					ScenePlanID:      scene.ID,
					SceneKey:         scene.SceneKey,
					SceneOrdinal:     scene.SceneOrdinal,
					RetryGeneration:  retryGeneration,
					SceneShotBudget:  sceneShotBudgets[scene.SceneKey],
					CorrectionHints:  corrections[scene.SceneKey],
				},
			})
			running = append(running, runningScene{scene: scene, future: future})
		}

		var firstErr error
		for _, child := range running {
			var output PlanStoryboardSceneOutput
			if err := child.future.Get(ctx, &output); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			outputByKey[child.scene.SceneKey] = output
			completed[child.scene.SceneKey] = true
			delete(pending, child.scene.SceneKey)
		}
		if firstErr != nil {
			return nil, firstErr
		}
	}

	outputs := make([]PlanStoryboardSceneOutput, 0, len(targets))
	sortedTargets := append([]StoryboardScenePlanRef(nil), targets...)
	sort.Slice(sortedTargets, func(left, right int) bool {
		return sortedTargets[left].SceneOrdinal < sortedTargets[right].SceneOrdinal
	})
	for _, scene := range sortedTargets {
		outputs = append(outputs, outputByKey[scene.SceneKey])
	}
	return outputs, nil
}

func storyboardSceneShotBudgets(blueprint storyboardpkg.ContinuityBlueprintOutput, episodeBudget int) (map[string]int, error) {
	budgets := make(map[string]int, len(blueprint.Scenes))
	if episodeBudget <= 0 {
		return budgets, nil
	}
	minimumTotal := 0
	for _, scene := range blueprint.Scenes {
		minimum := scene.SuggestedShotMinimum
		if minimum < 1 {
			minimum = 1
		}
		budgets[scene.SceneKey] = minimum
		minimumTotal += minimum
	}
	if episodeBudget < minimumTotal {
		return nil, fmt.Errorf("shot budget %d is below the semantic minimum %d", episodeBudget, minimumTotal)
	}
	remaining := episodeBudget - minimumTotal
	for remaining > 0 {
		allocated := false
		for _, scene := range blueprint.Scenes {
			maximum := scene.SuggestedShotMaximum
			if maximum < budgets[scene.SceneKey] {
				maximum = budgets[scene.SceneKey]
			}
			if budgets[scene.SceneKey] >= maximum {
				continue
			}
			budgets[scene.SceneKey]++
			remaining--
			allocated = true
			if remaining == 0 {
				break
			}
		}
		if !allocated {
			break
		}
	}
	return budgets, nil
}

func storyboardSceneChildWorkflowOptions(
	ctx workflow.Context,
	planID string,
	scene StoryboardScenePlanRef,
	retryGeneration int,
) workflow.ChildWorkflowOptions {
	parentID := workflow.GetInfo(ctx).WorkflowExecution.ID
	return workflow.ChildWorkflowOptions{
		WorkflowID:               fmt.Sprintf("%s:scene:%s:%d", parentID, scene.ID, retryGeneration),
		WorkflowExecutionTimeout: 2 * time.Hour,
		WorkflowRunTimeout:       90 * time.Minute,
		WaitForCancellation:      true,
		ParentClosePolicy:        enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
		Memo: map[string]any{
			"storyboardPlanId": planID,
			"scenePlanId":      scene.ID,
			"sceneKey":         scene.SceneKey,
			"sceneOrdinal":     scene.SceneOrdinal,
			"retryGeneration":  retryGeneration,
		},
	}
}

func storyboardEpisodeChildWorkflowOptions(ctx workflow.Context, episode ScriptStoryboardEpisodeRef) workflow.ChildWorkflowOptions {
	parentID := workflow.GetInfo(ctx).WorkflowExecution.ID
	return workflow.ChildWorkflowOptions{
		WorkflowID:               fmt.Sprintf("%s:episode:%s", parentID, episode.ID),
		WorkflowExecutionTimeout: 12 * time.Hour,
		WorkflowRunTimeout:       12 * time.Hour,
		WaitForCancellation:      true,
		ParentClosePolicy:        enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
		Memo: map[string]any{
			"scriptEpisodeId": episode.ID,
			"episodeIndex":    episode.EpisodeIndex,
		},
	}
}

func storyboardSceneDependencies(blueprint storyboardpkg.ContinuityBlueprintOutput) map[string][]string {
	dependencies := make(map[string][]string, len(blueprint.Scenes))
	seen := make(map[string]map[string]bool, len(blueprint.Scenes))
	add := func(before, after string) {
		before = strings.TrimSpace(before)
		after = strings.TrimSpace(after)
		if before == "" || after == "" || before == after {
			return
		}
		if seen[after] == nil {
			seen[after] = map[string]bool{}
		}
		if !seen[after][before] {
			dependencies[after] = append(dependencies[after], before)
			seen[after][before] = true
		}
	}
	for _, dependency := range blueprint.Dependencies {
		add(dependency.FromSceneKey, dependency.ToSceneKey)
	}
	for _, group := range blueprint.SerialGroups {
		for index := 1; index < len(group); index++ {
			add(group[index-1], group[index])
		}
	}
	for key := range dependencies {
		sort.Strings(dependencies[key])
	}
	return dependencies
}

func storyboardDependenciesComplete(dependencies []string, completed map[string]bool) bool {
	for _, dependency := range dependencies {
		if !completed[dependency] {
			return false
		}
	}
	return true
}

func storyboardScenePlansByKey(all []StoryboardScenePlanRef, keys []string) ([]StoryboardScenePlanRef, error) {
	wanted := make(map[string]bool, len(keys))
	for _, key := range keys {
		wanted[strings.TrimSpace(key)] = true
	}
	selected := make([]StoryboardScenePlanRef, 0, len(wanted))
	for _, scene := range all {
		if wanted[scene.SceneKey] {
			selected = append(selected, scene)
			delete(wanted, scene.SceneKey)
		}
	}
	if len(wanted) > 0 {
		unknown := make([]string, 0, len(wanted))
		for key := range wanted {
			unknown = append(unknown, key)
		}
		sort.Strings(unknown)
		return nil, fmt.Errorf("review requested unknown storyboard scenes: %s", strings.Join(unknown, ", "))
	}
	return selected, nil
}

func correctionsByScene(corrections []storyboardpkg.StoryboardCorrection) map[string][]storyboardpkg.StoryboardCorrection {
	result := make(map[string][]storyboardpkg.StoryboardCorrection)
	for _, correction := range corrections {
		if strings.TrimSpace(correction.SceneKey) == "" {
			continue
		}
		result[correction.SceneKey] = append(result[correction.SceneKey], correction)
	}
	return result
}

func appendProviderCallID(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendSceneProviderCallIDs(values []string, outputs []PlanStoryboardSceneOutput) []string {
	for _, output := range outputs {
		values = appendProviderCallID(values, output.ProviderCallID)
	}
	return values
}
