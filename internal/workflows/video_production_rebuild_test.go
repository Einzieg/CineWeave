package workflows

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestProjectVideoProductionRebuildWorkflowSkipsGenerationSwitchOnFailedItemRetry(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := projectVideoProductionRebuildTestInput()
	input.RetryFailed = true

	env.RegisterActivityWithOptions(func(context.Context, ProjectVideoProductionRebuildInput) (PrepareProjectVideoProductionRebuildOutput, error) {
		return PrepareProjectVideoProductionRebuildOutput{GenerationSwitched: true}, nil
	}, activity.RegisterOptions{Name: "PrepareProjectVideoProductionRebuild"})
	env.RegisterActivityWithOptions(func(context.Context, ProjectVideoProductionRebuildInput) ([]RebuildEpisodeWorkItem, error) {
		return nil, nil
	}, activity.RegisterOptions{Name: "ListProjectVideoProductionRebuildItems"})
	env.RegisterActivityWithOptions(func(context.Context, ProjectVideoProductionRebuildInput) (ProjectVideoProductionRebuildOutput, error) {
		return ProjectVideoProductionRebuildOutput{RebuildID: input.RebuildID, Status: "succeeded"}, nil
	}, activity.RegisterOptions{Name: "FinalizeProjectVideoProductionRebuild"})
	env.RegisterActivityWithOptions(func(context.Context, ProjectVideoProductionRebuildInput, string, string) error {
		return nil
	}, activity.RegisterOptions{Name: "FailProjectVideoProductionRebuild"})

	env.ExecuteWorkflow(ProjectVideoProductionRebuildWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

func TestProjectVideoProductionRebuildWorkflowSwitchesGenerationOnInitialRun(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := projectVideoProductionRebuildTestInput()
	drainCalled := false
	switchCalled := false

	env.RegisterActivityWithOptions(func(context.Context, ProjectVideoProductionRebuildInput) (PrepareProjectVideoProductionRebuildOutput, error) {
		return PrepareProjectVideoProductionRebuildOutput{}, nil
	}, activity.RegisterOptions{Name: "PrepareProjectVideoProductionRebuild"})
	env.RegisterActivityWithOptions(func(context.Context, ProjectVideoProductionRebuildInput) (ProjectVideoProductionDrainOutput, error) {
		drainCalled = true
		return ProjectVideoProductionDrainOutput{Drained: true}, nil
	}, activity.RegisterOptions{Name: "CheckProjectVideoProductionDrain"})
	env.RegisterActivityWithOptions(func(context.Context, ProjectVideoProductionRebuildInput) (SwitchProjectVideoProductionGenerationOutput, error) {
		switchCalled = true
		return SwitchProjectVideoProductionGenerationOutput{}, nil
	}, activity.RegisterOptions{Name: "SwitchProjectVideoProductionGeneration"})
	env.RegisterActivityWithOptions(func(context.Context, ProjectVideoProductionRebuildInput) ([]RebuildEpisodeWorkItem, error) {
		return nil, nil
	}, activity.RegisterOptions{Name: "ListProjectVideoProductionRebuildItems"})
	env.RegisterActivityWithOptions(func(context.Context, ProjectVideoProductionRebuildInput) (ProjectVideoProductionRebuildOutput, error) {
		return ProjectVideoProductionRebuildOutput{RebuildID: input.RebuildID, Status: "succeeded"}, nil
	}, activity.RegisterOptions{Name: "FinalizeProjectVideoProductionRebuild"})
	env.RegisterActivityWithOptions(func(context.Context, ProjectVideoProductionRebuildInput, string, string) error {
		return nil
	}, activity.RegisterOptions{Name: "FailProjectVideoProductionRebuild"})

	env.ExecuteWorkflow(ProjectVideoProductionRebuildWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.True(t, drainCalled)
	require.True(t, switchCalled)
}

func TestProjectVideoProductionRebuildWorkflowFailsWhenAllEpisodesFail(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := projectVideoProductionRebuildTestInput()
	failureFinalizerCalled := false

	env.RegisterActivityWithOptions(func(context.Context, ProjectVideoProductionRebuildInput) (PrepareProjectVideoProductionRebuildOutput, error) {
		return PrepareProjectVideoProductionRebuildOutput{GenerationSwitched: true}, nil
	}, activity.RegisterOptions{Name: "PrepareProjectVideoProductionRebuild"})
	env.RegisterActivityWithOptions(func(context.Context, ProjectVideoProductionRebuildInput) ([]RebuildEpisodeWorkItem, error) {
		return nil, nil
	}, activity.RegisterOptions{Name: "ListProjectVideoProductionRebuildItems"})
	env.RegisterActivityWithOptions(func(context.Context, ProjectVideoProductionRebuildInput) (ProjectVideoProductionRebuildOutput, error) {
		return ProjectVideoProductionRebuildOutput{
			RebuildID: input.RebuildID, Status: "storyboard_required",
			EpisodeCount: 2, FailedItems: 2,
		}, nil
	}, activity.RegisterOptions{Name: "FinalizeProjectVideoProductionRebuild"})
	env.RegisterActivityWithOptions(func(context.Context, ProjectVideoProductionRebuildInput, string, string) error {
		failureFinalizerCalled = true
		return nil
	}, activity.RegisterOptions{Name: "FailProjectVideoProductionRebuild"})

	env.ExecuteWorkflow(ProjectVideoProductionRebuildWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.True(t, failureFinalizerCalled)
}

func TestClassifyProjectVideoProductionRebuild(t *testing.T) {
	tests := []struct {
		name                            string
		episodeCount, succeeded, failed int
		rebuild, attempt, workflow      string
		projectState                    string
		locked                          bool
	}{
		{name: "zero episodes", rebuild: "succeeded", attempt: "succeeded", workflow: "succeeded", projectState: "storyboard_required"},
		{name: "all succeeded", episodeCount: 2, succeeded: 2, rebuild: "succeeded", attempt: "succeeded", workflow: "succeeded", projectState: "ready"},
		{name: "partial", episodeCount: 2, succeeded: 1, failed: 1, rebuild: "partial_succeeded", attempt: "partial_succeeded", workflow: "partial_succeeded", projectState: "storyboard_required", locked: true},
		{name: "all failed", episodeCount: 2, failed: 2, rebuild: "storyboard_required", attempt: "failed", workflow: "failed", projectState: "storyboard_required", locked: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := classifyProjectVideoProductionRebuild(test.episodeCount, test.succeeded, test.failed)
			require.NoError(t, err)
			require.Equal(t, test.rebuild, result.RebuildStatus)
			require.Equal(t, test.attempt, result.AttemptStatus)
			require.Equal(t, test.workflow, result.WorkflowStatus)
			require.Equal(t, test.projectState, result.ProjectState)
			require.Equal(t, test.locked, result.ProjectLocked)
		})
	}

	_, err := classifyProjectVideoProductionRebuild(2, 1, 0)
	require.Error(t, err)
}

func projectVideoProductionRebuildTestInput() ProjectVideoProductionRebuildInput {
	return ProjectVideoProductionRebuildInput{
		OrganizationID: "10000000-0000-4000-a000-000000000001",
		ProjectID:      "10000000-0000-4000-a000-000000000002",
		WorkflowRunID:  "10000000-0000-4000-a000-000000000003",
		RebuildID:      "10000000-0000-4000-a000-000000000004",
		RequestedBy:    "10000000-0000-4000-a000-000000000005",
	}
}
