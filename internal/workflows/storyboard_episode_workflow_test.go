package workflows

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	storyboardpkg "github.com/Einzieg/cineweave/internal/storyboard"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func TestScriptToStoryboardWorkflowGeneratesEpisodesSequentially(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := TextToStoryboardInput{
		OrganizationID: "org-1",
		ProjectID:      "project-1",
		WorkflowRunID:  "workflow-1",
		CreatedBy:      "user-1",
		Input:          json.RawMessage(`{"scriptId":"script-1","maxShots":3}`),
	}
	episodes := []ScriptStoryboardEpisodeRef{
		{ID: "episode-1", EpisodeIndex: 1, EpisodeTitle: "第一集"},
		{ID: "episode-2", EpisodeIndex: 2, EpisodeTitle: "第二集"},
		{ID: "episode-3", EpisodeIndex: 3, EpisodeTitle: "第三集"},
	}
	callOrder := make([]string, 0, len(episodes))

	env.RegisterWorkflow(ScriptToStoryboardWorkflow)
	env.RegisterActivityWithOptions(func(context.Context, PrepareScriptStoryboardInput) (ScriptStoryboardPlan, error) {
		return ScriptStoryboardPlan{
			ScriptID:         "script-1",
			ScriptVersionID:  "version-1",
			EpisodeTotal:     len(episodes),
			TimelineTimebase: 90_000,
			FPSNumerator:     24,
			FPSDenominator:   1,
			Episodes:         episodes,
		}, nil
	}, activity.RegisterOptions{Name: "PrepareScriptStoryboard"})
	env.RegisterWorkflowWithOptions(func(_ workflow.Context, episodeInput ScriptEpisodeToStoryboardInput) (ScriptStoryboardOutput, error) {
		callOrder = append(callOrder, episodeInput.ScriptEpisodeID)
		if episodeInput.EpisodeIndex != len(callOrder) || episodeInput.EpisodeTotal != len(episodes) {
			t.Fatalf("episode activity input = %+v", episodeInput)
		}
		return ScriptStoryboardOutput{
			ScriptID:             episodeInput.ScriptID,
			ScriptVersionID:      "version-1",
			ScriptEpisodeID:      episodeInput.ScriptEpisodeID,
			EpisodeIndex:         episodeInput.EpisodeIndex,
			EpisodeTotal:         episodeInput.EpisodeTotal,
			EpisodeTitle:         episodeInput.EpisodeTitle,
			StoryboardArtifactID: "artifact-" + episodeInput.ScriptEpisodeID,
			StorageKey:           "storyboard/" + episodeInput.ScriptEpisodeID + ".json",
			ProviderCallID:       "call-" + episodeInput.ScriptEpisodeID,
			Shots: []StoryboardShotRecord{{
				ID:               "shot-" + episodeInput.ScriptEpisodeID,
				ScriptEpisodeID:  episodeInput.ScriptEpisodeID,
				EpisodeIndex:     episodeInput.EpisodeIndex,
				EpisodeShotIndex: 0,
				ShotNo:           1,
			}},
		}, nil
	}, workflow.RegisterOptions{Name: "ScriptEpisodeToStoryboardWorkflow"})
	env.RegisterActivityWithOptions(func(context.Context, TextToStoryboardInput, ScriptStoryboardOutput) error {
		return nil
	}, activity.RegisterOptions{Name: "CompleteScriptStoryboardWorkflow"})

	env.ExecuteWorkflow(ScriptToStoryboardWorkflow, input)

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v err=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	if !reflect.DeepEqual(callOrder, []string{"episode-1", "episode-2", "episode-3"}) {
		t.Fatalf("episode call order = %v", callOrder)
	}
	var output ScriptStoryboardOutput
	if err := env.GetWorkflowResult(&output); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if output.EpisodeCount != 3 || len(output.Episodes) != 3 || len(output.Shots) != 3 {
		t.Fatalf("workflow output = %+v", output)
	}
}

func TestScriptToStoryboardWorkflowRecordsChildFailure(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := TextToStoryboardInput{
		OrganizationID: "org-1", ProjectID: "project-1", WorkflowRunID: "workflow-1", CreatedBy: "user-1",
		Input: json.RawMessage(`{"scriptId":"script-1","scriptEpisodeIds":["episode-1"]}`),
	}
	recordedCode := ""
	recordedMessage := ""
	env.RegisterWorkflow(ScriptToStoryboardWorkflow)
	env.RegisterActivityWithOptions(func(context.Context, PrepareScriptStoryboardInput) (ScriptStoryboardPlan, error) {
		return ScriptStoryboardPlan{
			ScriptID: "script-1", ScriptVersionID: "version-1", TimelineTimebase: 90_000, FPSNumerator: 24, FPSDenominator: 1,
			Episodes: []ScriptStoryboardEpisodeRef{{ID: "episode-1", EpisodeIndex: 1, EpisodeTitle: "第一集"}},
		}, nil
	}, activity.RegisterOptions{Name: "PrepareScriptStoryboard"})
	env.RegisterWorkflowWithOptions(func(workflow.Context, ScriptEpisodeToStoryboardInput) (ScriptStoryboardOutput, error) {
		return ScriptStoryboardOutput{}, temporal.NewNonRetryableApplicationError("review exhausted", "STORYBOARD_REVIEW_EXHAUSTED", nil)
	}, workflow.RegisterOptions{Name: "ScriptEpisodeToStoryboardWorkflow"})
	env.RegisterActivityWithOptions(func(_ context.Context, _ TextToStoryboardInput, code, message string) error {
		recordedCode = code
		recordedMessage = message
		return nil
	}, activity.RegisterOptions{Name: "FailScriptStoryboardWorkflow"})

	env.ExecuteWorkflow(ScriptToStoryboardWorkflow, input)
	if env.GetWorkflowError() == nil {
		t.Fatal("expected child workflow failure")
	}
	if recordedCode != "STORYBOARD_REVIEW_EXHAUSTED" || !strings.Contains(recordedMessage, "review exhausted") {
		t.Fatalf("recorded failure = %q/%q", recordedCode, recordedMessage)
	}
}

func TestStoryboardRetryNodeKeysIncludeGenerationAndAttempt(t *testing.T) {
	sceneID := "6ddf1e70-7505-4cbb-b717-1b1d881c33d1"
	if storyboardScenePlanNodeKey(sceneID, 0) == storyboardScenePlanNodeKey(sceneID, 1) {
		t.Fatal("scene retry generation was truncated from the node key")
	}
	planID := "2f43c9f3-61bb-4cf5-a461-f842ac87a62d"
	if storyboardReviewNodeKey(planID, 1) == storyboardReviewNodeKey(planID, 2) {
		t.Fatal("review attempt was truncated from the node key")
	}
}

func TestScriptEpisodeToStoryboardWorkflowRespectsDependenciesAndConcurrency(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	starts := map[string]time.Time{}
	finishes := map[string]time.Time{}
	registerStoryboardEpisodeTestActivities(env, func(input ReviewStoryboardPlanInput) ReviewStoryboardPlanOutput {
		return ReviewStoryboardPlanOutput{StoryboardPlanID: input.StoryboardPlanID, ReviewAttempt: input.ReviewAttempt, Approved: true}
	})
	env.RegisterWorkflow(ScriptEpisodeToStoryboardWorkflow)
	env.RegisterWorkflowWithOptions(func(ctx workflow.Context, input StoryboardScenePlanWorkflowInput) (PlanStoryboardSceneOutput, error) {
		starts[input.SceneKey] = workflow.Now(ctx)
		if err := workflow.Sleep(ctx, 5*time.Minute); err != nil {
			return PlanStoryboardSceneOutput{}, err
		}
		finishes[input.SceneKey] = workflow.Now(ctx)
		return PlanStoryboardSceneOutput{
			ScenePlanID:      input.ScenePlanID,
			StoryboardPlanID: input.StoryboardPlanID,
			SceneKey:         input.SceneKey,
			SceneOrdinal:     input.SceneOrdinal,
			RetryGeneration:  input.RetryGeneration,
			Status:           "ready",
		}, nil
	}, workflow.RegisterOptions{Name: "StoryboardScenePlanWorkflow"})

	env.ExecuteWorkflow(ScriptEpisodeToStoryboardWorkflow, storyboardEpisodeTestInput())
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if !starts["scene-a"].Equal(starts["scene-b"]) {
		t.Fatalf("independent scenes did not start together: a=%v b=%v", starts["scene-a"], starts["scene-b"])
	}
	if starts["scene-c"].Before(finishes["scene-a"]) {
		t.Fatalf("dependent scene started before scene-a completed: c=%v a-finished=%v", starts["scene-c"], finishes["scene-a"])
	}
}

func TestScriptEpisodeToStoryboardWorkflowRetriesOnlyReviewedScene(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	invocations := map[string][]int{}
	registerStoryboardEpisodeTestActivities(env, func(input ReviewStoryboardPlanInput) ReviewStoryboardPlanOutput {
		if input.ReviewAttempt == 1 {
			return ReviewStoryboardPlanOutput{
				StoryboardPlanID:       input.StoryboardPlanID,
				ReviewAttempt:          input.ReviewAttempt,
				Approved:               false,
				NeedsRevisionSceneKeys: []string{"scene-b"},
				Corrections: []storyboardpkg.StoryboardCorrection{{
					Type: "boundary", SceneKey: "scene-b", TimingUnitIDs: []string{"unit-b"}, Reason: "split the beat",
				}},
			}
		}
		return ReviewStoryboardPlanOutput{StoryboardPlanID: input.StoryboardPlanID, ReviewAttempt: input.ReviewAttempt, Approved: true}
	})
	env.RegisterWorkflow(ScriptEpisodeToStoryboardWorkflow)
	env.RegisterWorkflowWithOptions(func(_ workflow.Context, input StoryboardScenePlanWorkflowInput) (PlanStoryboardSceneOutput, error) {
		invocations[input.SceneKey] = append(invocations[input.SceneKey], input.RetryGeneration)
		if input.RetryGeneration == 1 && input.SceneKey == "scene-b" && len(input.CorrectionHints) != 1 {
			t.Fatalf("review correction hints = %+v", input.CorrectionHints)
		}
		return PlanStoryboardSceneOutput{
			ScenePlanID:      input.ScenePlanID,
			StoryboardPlanID: input.StoryboardPlanID,
			SceneKey:         input.SceneKey,
			SceneOrdinal:     input.SceneOrdinal,
			RetryGeneration:  input.RetryGeneration,
			Status:           "ready",
		}, nil
	}, workflow.RegisterOptions{Name: "StoryboardScenePlanWorkflow"})

	env.ExecuteWorkflow(ScriptEpisodeToStoryboardWorkflow, storyboardEpisodeTestInput())
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if !reflect.DeepEqual(invocations["scene-a"], []int{0}) ||
		!reflect.DeepEqual(invocations["scene-b"], []int{0, 1}) ||
		!reflect.DeepEqual(invocations["scene-c"], []int{0}) {
		t.Fatalf("scene retry generations = %#v", invocations)
	}
}

func registerStoryboardEpisodeTestActivities(
	env *testsuite.TestWorkflowEnvironment,
	review func(ReviewStoryboardPlanInput) ReviewStoryboardPlanOutput,
) {
	env.RegisterActivityWithOptions(func(context.Context, AnalyzeEpisodeTimingInput) (TimingAnalysisActivityOutput, error) {
		return TimingAnalysisActivityOutput{
			AnalysisID:             "analysis-1",
			ScriptID:               "script-1",
			ScriptVersionID:        "version-1",
			ScriptEpisodeID:        "episode-1",
			EstimatedDurationTicks: 900_000,
			MinimumDurationTicks:   720_000,
			TimelineTimebase:       90_000,
			FPSNumerator:           24,
			FPSDenominator:         1,
		}, nil
	}, activity.RegisterOptions{Name: "AnalyzeEpisodeTiming"})
	env.RegisterActivityWithOptions(func(context.Context, BuildEpisodeContinuityBlueprintInput) (ContinuityBlueprintActivityOutput, error) {
		return storyboardEpisodeTestBlueprint(), nil
	}, activity.RegisterOptions{Name: "BuildEpisodeContinuityBlueprint"})
	env.RegisterActivityWithOptions(func(_ context.Context, input ReviewStoryboardPlanInput) (ReviewStoryboardPlanOutput, error) {
		return review(input), nil
	}, activity.RegisterOptions{Name: "ReviewStoryboardPlan"})
	env.RegisterActivityWithOptions(func(_ context.Context, input ActivateStoryboardPlanActivityInput) (ScriptStoryboardOutput, error) {
		return ScriptStoryboardOutput{
			ScriptID:             input.ScriptID,
			ScriptVersionID:      input.ScriptVersionID,
			ScriptEpisodeID:      input.ScriptEpisodeID,
			EpisodeIndex:         input.EpisodeIndex,
			EpisodeTotal:         input.EpisodeTotal,
			EpisodeTitle:         input.EpisodeTitle,
			StoryboardArtifactID: "artifact-1",
			StorageKey:           "storyboard/episode-1.json",
		}, nil
	}, activity.RegisterOptions{Name: "ActivateStoryboardPlan"})
}

func storyboardEpisodeTestInput() ScriptEpisodeToStoryboardInput {
	return ScriptEpisodeToStoryboardInput{
		OrganizationID:   "org-1",
		ProjectID:        "project-1",
		WorkflowRunID:    "workflow-1",
		CreatedBy:        "user-1",
		ScriptID:         "script-1",
		ScriptVersionID:  "version-1",
		ScriptEpisodeID:  "episode-1",
		EpisodeIndex:     1,
		EpisodeTotal:     1,
		EpisodeTitle:     "第一集",
		TimelineTimebase: 90_000,
		FPSNumerator:     24,
		FPSDenominator:   1,
		ProductionOptions: ScriptProductionOptions{
			PacingProfile:       "standard",
			MaxSceneConcurrency: 2,
		},
	}
}

func storyboardEpisodeTestBlueprint() ContinuityBlueprintActivityOutput {
	return ContinuityBlueprintActivityOutput{
		BlueprintID:      "blueprint-1",
		StoryboardPlanID: "plan-1",
		Blueprint: storyboardpkg.ContinuityBlueprintOutput{
			Scenes: []storyboardpkg.ContinuityBlueprintScene{
				{SceneKey: "scene-a", SceneOrdinal: 0, SuggestedShotMinimum: 1, SuggestedShotMaximum: 2},
				{SceneKey: "scene-b", SceneOrdinal: 1, SuggestedShotMinimum: 1, SuggestedShotMaximum: 2},
				{SceneKey: "scene-c", SceneOrdinal: 2, SuggestedShotMinimum: 1, SuggestedShotMaximum: 2},
			},
			Dependencies: []storyboardpkg.ContinuityBlueprintDependency{{FromSceneKey: "scene-a", ToSceneKey: "scene-c", Strong: true}},
		},
		ScenePlans: []StoryboardScenePlanRef{
			{ID: "scene-plan-a", SceneKey: "scene-a", SceneOrdinal: 0},
			{ID: "scene-plan-b", SceneKey: "scene-b", SceneOrdinal: 1},
			{ID: "scene-plan-c", SceneKey: "scene-c", SceneOrdinal: 2},
		},
	}
}

func TestStoryboardMaxOutputTokens(t *testing.T) {
	tests := []struct {
		maxShots int
		want     int
	}{
		{maxShots: 0, want: 9600},
		{maxShots: 1, want: 1900},
		{maxShots: 2, want: 2600},
		{maxShots: 3, want: 3300},
		{maxShots: 12, want: 9600},
		{maxShots: 24, want: 18000},
	}
	for _, test := range tests {
		if got := storyboardMaxOutputTokens(test.maxShots); got != test.want {
			t.Fatalf("storyboardMaxOutputTokens(%d) = %d, want %d", test.maxShots, got, test.want)
		}
	}
}

func TestStoryboardSceneShotBudgetsRespectSemanticMinimum(t *testing.T) {
	blueprint := storyboardEpisodeTestBlueprint().Blueprint
	budgets, err := storyboardSceneShotBudgets(blueprint, 5)
	if err != nil {
		t.Fatalf("allocate scene budgets: %v", err)
	}
	if budgets["scene-a"]+budgets["scene-b"]+budgets["scene-c"] != 5 {
		t.Fatalf("scene budgets = %#v", budgets)
	}
	if _, err := storyboardSceneShotBudgets(blueprint, 2); err == nil {
		t.Fatal("expected budget below semantic minimum to fail")
	}
}
