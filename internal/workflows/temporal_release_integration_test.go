package workflows

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestTemporalPinnedReleaseSurvivesPromotionAndReplaysHistory(t *testing.T) {
	if os.Getenv("CINEWEAVE_TEMPORAL_RELEASE_INTEGRATION") != "1" {
		t.Skip("set CINEWEAVE_TEMPORAL_RELEASE_INTEGRATION=1 to run against Temporal")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	address := integrationEnvironment("TEMPORAL_ADDRESS", "127.0.0.1:7233")
	namespace := integrationEnvironment("TEMPORAL_NAMESPACE", "default")
	temporalClient, err := client.Dial(client.Options{HostPort: address, Namespace: namespace})
	if err != nil {
		t.Fatalf("dial Temporal: %v", err)
	}
	defer temporalClient.Close()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	deploymentName := "cineweave-release-it-" + suffix
	taskQueue := deploymentName + "-queue"
	oldBuildID := "old-" + suffix
	newBuildID := "new-" + suffix
	handle := temporalClient.WorkerDeploymentClient().GetHandle(deploymentName)

	oldWorker := startReleaseCanaryWorker(t, temporalClient, taskQueue, deploymentName, oldBuildID)
	defer oldWorker.Stop()
	waitForDeploymentVersion(t, ctx, handle, oldBuildID)
	setCurrentDeploymentVersion(t, ctx, handle, oldBuildID)

	oldWorkflowID := deploymentName + "-old"
	oldRun, err := temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: oldWorkflowID, TaskQueue: taskQueue}, TemporalReleaseCanaryWorkflow, TemporalReleaseCanaryInput{ReleaseMarker: oldBuildID})
	if err != nil {
		t.Fatalf("start old canary: %v", err)
	}
	waitForCanaryBuild(t, ctx, temporalClient, oldWorkflowID, oldRun.GetRunID(), oldBuildID)

	newWorker := startReleaseCanaryWorker(t, temporalClient, taskQueue, deploymentName, newBuildID)
	defer newWorker.Stop()
	waitForDeploymentVersion(t, ctx, handle, newBuildID)
	setCurrentDeploymentVersion(t, ctx, handle, newBuildID)

	if err := temporalClient.SignalWorkflow(ctx, oldWorkflowID, oldRun.GetRunID(), TemporalReleaseCanaryCompleteSignal, oldBuildID); err != nil {
		t.Fatalf("signal old canary: %v", err)
	}
	var oldOutput TemporalReleaseCanaryOutput
	if err := oldRun.Get(ctx, &oldOutput); err != nil {
		t.Fatalf("complete old canary: %v", err)
	}
	if oldOutput.StartedBuildID != oldBuildID || oldOutput.CompletedBuildID != oldBuildID {
		t.Fatalf("old pinned workflow crossed builds: %+v", oldOutput)
	}

	newWorkflowID := deploymentName + "-new"
	newRun, err := temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: newWorkflowID, TaskQueue: taskQueue}, TemporalReleaseCanaryWorkflow, TemporalReleaseCanaryInput{ReleaseMarker: newBuildID})
	if err != nil {
		t.Fatalf("start new canary: %v", err)
	}
	waitForCanaryBuild(t, ctx, temporalClient, newWorkflowID, newRun.GetRunID(), newBuildID)
	if err := temporalClient.SignalWorkflow(ctx, newWorkflowID, newRun.GetRunID(), TemporalReleaseCanaryCompleteSignal, newBuildID); err != nil {
		t.Fatalf("signal new canary: %v", err)
	}
	var newOutput TemporalReleaseCanaryOutput
	if err := newRun.Get(ctx, &newOutput); err != nil {
		t.Fatalf("complete new canary: %v", err)
	}
	if newOutput.StartedBuildID != newBuildID || newOutput.CompletedBuildID != newBuildID {
		t.Fatalf("new workflow did not use promoted build: %+v", newOutput)
	}

	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflow(TemporalReleaseCanaryWorkflow)
	if err := replayer.ReplayWorkflowExecution(ctx, temporalClient.WorkflowService(), nil, namespace, workflow.Execution{ID: oldWorkflowID, RunID: oldRun.GetRunID()}); err != nil {
		t.Fatalf("replay old canary history: %v", err)
	}
	if fixturePath := os.Getenv("CINEWEAVE_TEMPORAL_HISTORY_FIXTURE"); fixturePath != "" {
		writeTemporalHistoryFixture(t, ctx, temporalClient, oldWorkflowID, oldRun.GetRunID(), fixturePath)
	}
}

func startReleaseCanaryWorker(t *testing.T, temporalClient client.Client, taskQueue, deploymentName, buildID string) worker.Worker {
	t.Helper()
	temporalWorker := worker.New(temporalClient, taskQueue, worker.Options{
		DeploymentOptions: worker.DeploymentOptions{
			UseVersioning:             true,
			Version:                   worker.WorkerDeploymentVersion{DeploymentName: deploymentName, BuildID: buildID},
			DefaultVersioningBehavior: workflow.VersioningBehaviorPinned,
		},
	})
	temporalWorker.RegisterWorkflow(TemporalReleaseCanaryWorkflow)
	if err := temporalWorker.Start(); err != nil {
		t.Fatalf("start canary worker %s: %v", buildID, err)
	}
	return temporalWorker
}

func waitForDeploymentVersion(t *testing.T, ctx context.Context, handle client.WorkerDeploymentHandle, buildID string) {
	t.Helper()
	for {
		description, err := handle.Describe(ctx, client.WorkerDeploymentDescribeOptions{})
		if err == nil {
			for _, summary := range description.Info.VersionSummaries {
				if summary.Version.BuildID == buildID {
					return
				}
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for deployment version %s: %v", buildID, ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func setCurrentDeploymentVersion(t *testing.T, ctx context.Context, handle client.WorkerDeploymentHandle, buildID string) {
	t.Helper()
	description, err := handle.Describe(ctx, client.WorkerDeploymentDescribeOptions{})
	if err != nil {
		t.Fatalf("describe deployment before promotion: %v", err)
	}
	if _, err := handle.SetCurrentVersion(ctx, client.WorkerDeploymentSetCurrentVersionOptions{
		BuildID:       buildID,
		ConflictToken: description.ConflictToken,
		Identity:      "cineweave-release-integration-test",
	}); err != nil {
		t.Fatalf("promote %s: %v", buildID, err)
	}
}

func waitForCanaryBuild(t *testing.T, ctx context.Context, temporalClient client.Client, workflowID, runID, expectedBuildID string) {
	t.Helper()
	for {
		response, err := temporalClient.QueryWorkflow(ctx, workflowID, runID, TemporalReleaseCanaryStateQuery)
		if err == nil {
			var state TemporalReleaseCanaryState
			if decodeErr := response.Get(&state); decodeErr == nil && state.Status == "waiting" {
				if state.StartedBuildID != expectedBuildID {
					t.Fatalf("canary started on %q, want %q", state.StartedBuildID, expectedBuildID)
				}
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for canary %s: %v", workflowID, ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func writeTemporalHistoryFixture(t *testing.T, ctx context.Context, temporalClient client.Client, workflowID, runID, outputPath string) {
	t.Helper()
	iterator := temporalClient.GetWorkflowHistory(ctx, workflowID, runID, false, 0)
	history := &historypb.History{}
	for iterator.HasNext() {
		event, err := iterator.Next()
		if err != nil {
			t.Fatalf("read workflow history: %v", err)
		}
		history.Events = append(history.Events, event)
	}
	encoded, err := protojson.MarshalOptions{UseProtoNames: true, Indent: "  "}.Marshal(history)
	if err != nil {
		t.Fatalf("marshal workflow history: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		t.Fatalf("create history fixture directory: %v", err)
	}
	if err := os.WriteFile(outputPath, append(encoded, '\n'), 0o644); err != nil {
		t.Fatalf("write history fixture: %v", err)
	}
}

func integrationEnvironment(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
