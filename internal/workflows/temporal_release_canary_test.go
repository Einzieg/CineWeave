package workflows

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
)

func TestTemporalReleaseCanaryWorkflowCompletesAfterMatchingSignal(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	environment := suite.NewTestWorkflowEnvironment()
	environment.RegisterDelayedCallback(func() {
		environment.SignalWorkflow(TemporalReleaseCanaryCompleteSignal, "release-1")
	}, 0)
	environment.ExecuteWorkflow(TemporalReleaseCanaryWorkflow, TemporalReleaseCanaryInput{ReleaseMarker: "release-1"})
	require.True(t, environment.IsWorkflowCompleted())
	require.NoError(t, environment.GetWorkflowError())
	var output TemporalReleaseCanaryOutput
	require.NoError(t, environment.GetWorkflowResult(&output))
	require.Equal(t, "release-1", output.ReleaseMarker)
}

func TestTemporalReleaseCanaryHistoryFixtureReplays(t *testing.T) {
	fixture := filepath.Join("testdata", "temporal_release_canary_history.json")
	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflow(TemporalReleaseCanaryWorkflow)
	require.NoError(t, replayer.ReplayWorkflowHistoryFromJSONFile(nil, fixture))
}
