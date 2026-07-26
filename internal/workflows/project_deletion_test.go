package workflows

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestProjectDeletionWorkflowDrainTimeoutIsRetryableAndStopsBeforeStorage(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	environment := suite.NewTestWorkflowEnvironment()
	input := ProjectDeletionInput{
		OrganizationID:   "10000000-0000-4000-a000-000000000001",
		WorkspaceID:      "10000000-0000-4000-a000-000000000002",
		ProjectID:        "10000000-0000-4000-a000-000000000003",
		RequestID:        "10000000-0000-4000-a000-000000000004",
		DeletionRevision: 1,
		RequestedBy:      "10000000-0000-4000-a000-000000000005",
	}
	failureCodes := make([]string, 0, 2)

	environment.RegisterActivityWithOptions(
		func(context.Context, ProjectDeletionInput) (PrepareProjectDeletionOutput, error) {
			return PrepareProjectDeletionOutput{DrainDeadlineAt: time.Unix(1, 0)}, nil
		},
		activity.RegisterOptions{Name: PrepareProjectDeletionActivityName},
	)
	environment.RegisterActivityWithOptions(
		func(context.Context, ProjectDeletionInput) error { return nil },
		activity.RegisterOptions{Name: CancelProjectProviderTasksActivityName},
	)
	environment.RegisterActivityWithOptions(
		func(context.Context, ProjectDeletionInput) (ProjectDeletionDrainOutput, error) {
			return ProjectDeletionDrainOutput{ActiveWorkflowCount: 1}, nil
		},
		activity.RegisterOptions{Name: CheckProjectDeletionDrainActivityName},
	)
	environment.RegisterActivityWithOptions(
		func(_ context.Context, _ ProjectDeletionInput, code, _ string, _ bool) error {
			failureCodes = append(failureCodes, code)
			return nil
		},
		activity.RegisterOptions{Name: FailProjectDeletionActivityName},
	)

	environment.ExecuteWorkflow(ProjectDeletionWorkflow, input)

	require.True(t, environment.IsWorkflowCompleted())
	require.Error(t, environment.GetWorkflowError())
	require.Contains(t, failureCodes, CodeProjectDeletionDrainTimeout)
}
