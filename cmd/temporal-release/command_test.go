package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func TestParseCommandUsesEnvironmentAndFlagOverrides(t *testing.T) {
	t.Parallel()

	environment := map[string]string{
		"CINEWEAVE_TEMPORAL_ADDRESS":         "temporal.internal:7233",
		"CINEWEAVE_TEMPORAL_NAMESPACE":       "cineweave",
		"CINEWEAVE_TEMPORAL_DEPLOYMENT_NAME": "script-worker",
		"CINEWEAVE_RELEASE_ID":               "release-from-env",
	}
	cfg, err := parseCommand([]string{
		"ramp",
		"--release-id", "release-from-flag",
		"--percentage", "12.5",
		"--timeout", "45s",
	}, mapGetenv(environment), io.Discard)
	if err != nil {
		t.Fatalf("parse command: %v", err)
	}

	if cfg.address != "temporal.internal:7233" {
		t.Fatalf("address = %q", cfg.address)
	}
	if cfg.namespace != "cineweave" {
		t.Fatalf("namespace = %q", cfg.namespace)
	}
	if cfg.deploymentName != "script-worker" {
		t.Fatalf("deployment = %q", cfg.deploymentName)
	}
	if cfg.releaseID != "release-from-flag" {
		t.Fatalf("release ID = %q", cfg.releaseID)
	}
	if cfg.percentage != 12.5 {
		t.Fatalf("percentage = %v", cfg.percentage)
	}
	if cfg.timeout != 45*time.Second {
		t.Fatalf("timeout = %s", cfg.timeout)
	}
}

func TestParseCommandRejectsInvalidParameters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing subcommand", args: nil, want: "subcommand is required"},
		{name: "unknown subcommand", args: []string{"deploy"}, want: "unknown subcommand"},
		{name: "missing deployment", args: []string{"check", "--release-id", "r1"}, want: "deployment name is required"},
		{name: "missing release", args: []string{"check", "--deployment", "worker"}, want: "release ID is required"},
		{name: "invalid deployment", args: []string{"check", "--deployment", "worker.prod", "--release-id", "r1"}, want: "cannot contain '.'"},
		{name: "zero ramp", args: []string{"ramp", "--deployment", "worker", "--release-id", "r1", "--percentage", "0"}, want: "percentage must be greater"},
		{name: "excessive ramp", args: []string{"ramp", "--deployment", "worker", "--release-id", "r1", "--percentage", "101"}, want: "percentage must be greater"},
		{name: "invalid stable checks", args: []string{"drain", "--deployment", "worker", "--release-id", "r1", "--stable-checks", "0"}, want: "stable checks must be at least 1"},
		{name: "unexpected argument", args: []string{"check", "--deployment", "worker", "--release-id", "r1", "extra"}, want: "unexpected positional arguments"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseCommand(test.args, mapGetenv(nil), io.Discard)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestCheckReportsSafeReleaseWithoutMutation(t *testing.T) {
	t.Parallel()

	handle := &fakeDeploymentHandle{
		descriptions: []client.WorkerDeploymentDescribeResponse{
			deploymentDescription("token", "r2", "", 0, "r1", client.WorkerDeploymentVersionDrainageStatusDrained),
		},
		versions: []client.WorkerDeploymentVersionDescription{
			versionDescription("r1", client.WorkerDeploymentVersionDrainageStatusDrained),
		},
	}
	result := runApp(t, handle, []string{"check", "--deployment", "worker", "--release-id", "r1"})

	if result.Changed {
		t.Fatal("check unexpectedly reported a change")
	}
	if !result.Status.SafeToDecommission {
		t.Fatalf("status = %+v", result.Status)
	}
	if result.Status.RoutingReachability != "inactive" || result.Status.DrainageStatus != "drained" {
		t.Fatalf("status = %+v", result.Status)
	}
	if len(handle.currentCalls) != 0 || len(handle.rampingCalls) != 0 {
		t.Fatal("check invoked a routing mutation")
	}
}

func TestRampUsesConflictTokenAndSafetyOptions(t *testing.T) {
	t.Parallel()

	handle := &fakeDeploymentHandle{
		descriptions: []client.WorkerDeploymentDescribeResponse{
			deploymentDescription("before-token", "r1", "", 0, "r2", client.WorkerDeploymentVersionDrainageStatusUnspecified),
			deploymentDescription("after-token", "r1", "r2", 25, "r2", client.WorkerDeploymentVersionDrainageStatusUnspecified),
		},
		versions: []client.WorkerDeploymentVersionDescription{
			versionDescription("r2", client.WorkerDeploymentVersionDrainageStatusUnspecified),
			versionDescription("r2", client.WorkerDeploymentVersionDrainageStatusUnspecified),
		},
		setRampingResponses: []client.WorkerDeploymentSetRampingVersionResponse{{
			PreviousVersion:    deploymentVersion("r0"),
			PreviousPercentage: 5,
		}},
	}
	result := runApp(t, handle, []string{
		"ramp", "--deployment", "worker", "--release-id", "r2", "--percentage", "25",
		"--identity", "release-controller", "--allow-no-pollers", "--ignore-missing-task-queues",
	})

	if !result.Changed || result.PreviousRampingID != "r0" || result.PreviousPercentage != 5 {
		t.Fatalf("result = %+v", result)
	}
	if len(handle.rampingCalls) != 1 {
		t.Fatalf("ramping calls = %d", len(handle.rampingCalls))
	}
	call := handle.rampingCalls[0]
	if call.BuildID != "r2" || call.Percentage != 25 || string(call.ConflictToken) != "before-token" {
		t.Fatalf("ramp options = %+v", call)
	}
	if call.Identity != "release-controller" || !call.AllowNoPollers || !call.IgnoreMissingTaskQueues {
		t.Fatalf("ramp safety options = %+v", call)
	}
}

func TestRampRequiresExplicitReplacement(t *testing.T) {
	t.Parallel()

	handle := &fakeDeploymentHandle{
		descriptions: []client.WorkerDeploymentDescribeResponse{
			deploymentDescription("token", "r1", "other-canary", 10, "r2", client.WorkerDeploymentVersionDrainageStatusUnspecified),
		},
		versions: []client.WorkerDeploymentVersionDescription{
			versionDescription("r2", client.WorkerDeploymentVersionDrainageStatusUnspecified),
		},
	}
	app := testApplication(handle)
	err := app.run(context.Background(), []string{
		"ramp", "--deployment", "worker", "--release-id", "r2", "--percentage", "25",
	}, mapGetenv(nil))
	if err == nil || !strings.Contains(err.Error(), "--replace-ramping") {
		t.Fatalf("error = %v", err)
	}
	if len(handle.rampingCalls) != 0 {
		t.Fatal("ramp mutation happened without replacement approval")
	}
}

func TestPromoteRequiresFullRampAndForwardsConflictToken(t *testing.T) {
	t.Parallel()

	t.Run("rejects un-ramped cutover", func(t *testing.T) {
		handle := &fakeDeploymentHandle{
			descriptions: []client.WorkerDeploymentDescribeResponse{
				deploymentDescription("token", "r1", "r2", 50, "r2", client.WorkerDeploymentVersionDrainageStatusUnspecified),
			},
			versions: []client.WorkerDeploymentVersionDescription{
				versionDescription("r2", client.WorkerDeploymentVersionDrainageStatusUnspecified),
			},
		}
		err := testApplication(handle).run(context.Background(), []string{
			"promote", "--deployment", "worker", "--release-id", "r2",
		}, mapGetenv(nil))
		if err == nil || !strings.Contains(err.Error(), "ramping at 100 percent") {
			t.Fatalf("error = %v", err)
		}
		if len(handle.currentCalls) != 0 {
			t.Fatal("promotion mutation happened before a full ramp")
		}
	})

	t.Run("promotes fully ramped release", func(t *testing.T) {
		handle := &fakeDeploymentHandle{
			descriptions: []client.WorkerDeploymentDescribeResponse{
				deploymentDescription("before-token", "r1", "r2", 100, "r2", client.WorkerDeploymentVersionDrainageStatusUnspecified),
				deploymentDescription("after-token", "r2", "", 0, "r2", client.WorkerDeploymentVersionDrainageStatusUnspecified),
			},
			versions: []client.WorkerDeploymentVersionDescription{
				versionDescription("r2", client.WorkerDeploymentVersionDrainageStatusUnspecified),
				versionDescription("r2", client.WorkerDeploymentVersionDrainageStatusUnspecified),
			},
			setCurrentResponses: []client.WorkerDeploymentSetCurrentVersionResponse{{
				PreviousVersion: deploymentVersion("r1"),
			}},
		}
		result := runApp(t, handle, []string{"promote", "--deployment", "worker", "--release-id", "r2"})
		if !result.Changed || result.PreviousCurrentID != "r1" {
			t.Fatalf("result = %+v", result)
		}
		if len(handle.currentCalls) != 1 || string(handle.currentCalls[0].ConflictToken) != "before-token" {
			t.Fatalf("current calls = %+v", handle.currentCalls)
		}
	})
}

func TestRollbackRestoresTargetAndClearsDifferentRamp(t *testing.T) {
	t.Parallel()

	handle := &fakeDeploymentHandle{
		descriptions: []client.WorkerDeploymentDescribeResponse{
			deploymentDescription("before-token", "broken", "canary", 20, "stable", client.WorkerDeploymentVersionDrainageStatusUnspecified),
			deploymentDescription("after-token", "stable", "", 0, "stable", client.WorkerDeploymentVersionDrainageStatusUnspecified),
		},
		versions: []client.WorkerDeploymentVersionDescription{
			versionDescription("stable", client.WorkerDeploymentVersionDrainageStatusUnspecified),
			versionDescription("stable", client.WorkerDeploymentVersionDrainageStatusUnspecified),
		},
		setCurrentResponses: []client.WorkerDeploymentSetCurrentVersionResponse{{
			ConflictToken:   []byte("current-updated-token"),
			PreviousVersion: deploymentVersion("broken"),
		}},
		setRampingResponses: []client.WorkerDeploymentSetRampingVersionResponse{{}},
	}
	result := runApp(t, handle, []string{"rollback", "--deployment", "worker", "--release-id", "stable"})

	if !result.Changed || result.PreviousCurrentID != "broken" || result.PreviousRampingID != "canary" {
		t.Fatalf("result = %+v", result)
	}
	if len(handle.currentCalls) != 1 || string(handle.currentCalls[0].ConflictToken) != "before-token" {
		t.Fatalf("current calls = %+v", handle.currentCalls)
	}
	if len(handle.rampingCalls) != 1 {
		t.Fatalf("ramping calls = %d", len(handle.rampingCalls))
	}
	clear := handle.rampingCalls[0]
	if clear.BuildID != "" || clear.Percentage != 0 || string(clear.ConflictToken) != "current-updated-token" {
		t.Fatalf("clear ramp options = %+v", clear)
	}
}

func TestDrainWaitsForConsecutiveSafeObservations(t *testing.T) {
	t.Parallel()

	handle := &fakeDeploymentHandle{
		descriptions: []client.WorkerDeploymentDescribeResponse{
			deploymentDescription("1", "new", "", 0, "old", client.WorkerDeploymentVersionDrainageStatusDraining),
			deploymentDescription("2", "new", "", 0, "old", client.WorkerDeploymentVersionDrainageStatusDrained),
			deploymentDescription("3", "new", "", 0, "old", client.WorkerDeploymentVersionDrainageStatusDrained),
		},
		versions: []client.WorkerDeploymentVersionDescription{
			versionDescription("old", client.WorkerDeploymentVersionDrainageStatusDraining),
			versionDescription("old", client.WorkerDeploymentVersionDrainageStatusDrained),
			versionDescription("old", client.WorkerDeploymentVersionDrainageStatusDrained),
		},
	}
	waits := 0
	app := testApplication(handle)
	app.wait = func(context.Context, time.Duration) error {
		waits++
		return nil
	}
	result, err := app.execute(context.Background(), handle, commandConfig{
		name:           commandDrain,
		deploymentName: "worker",
		releaseID:      "old",
		pollInterval:   time.Millisecond,
		stableChecks:   2,
	})
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if waits != 2 {
		t.Fatalf("wait count = %d, want 2", waits)
	}
	if !result.Status.SafeToDecommission || result.Status.DrainageStatus != "drained" {
		t.Fatalf("result = %+v", result)
	}
	if len(handle.currentCalls) != 0 || len(handle.rampingCalls) != 0 {
		t.Fatal("drain mutated deployment routing")
	}
}

func TestDrainRejectsReachableRelease(t *testing.T) {
	t.Parallel()

	handle := &fakeDeploymentHandle{
		descriptions: []client.WorkerDeploymentDescribeResponse{
			deploymentDescription("token", "old", "", 0, "old", client.WorkerDeploymentVersionDrainageStatusUnspecified),
		},
		versions: []client.WorkerDeploymentVersionDescription{
			versionDescription("old", client.WorkerDeploymentVersionDrainageStatusUnspecified),
		},
	}
	waited := false
	app := testApplication(handle)
	app.wait = func(context.Context, time.Duration) error {
		waited = true
		return nil
	}
	_, err := app.execute(context.Background(), handle, commandConfig{
		name:           commandDrain,
		deploymentName: "worker",
		releaseID:      "old",
		pollInterval:   time.Millisecond,
		stableChecks:   1,
	})
	if err == nil || !strings.Contains(err.Error(), "still current") {
		t.Fatalf("error = %v", err)
	}
	if waited {
		t.Fatal("drain waited even though release remained reachable")
	}
}

func TestRunDoesNotOpenTemporalForInvalidArguments(t *testing.T) {
	t.Parallel()

	opened := false
	app := newApplication(io.Discard, io.Discard)
	app.openHandle = func(context.Context, commandConfig) (deploymentHandle, func(), error) {
		opened = true
		return nil, nil, errors.New("unexpected")
	}
	err := app.run(context.Background(), []string{"ramp", "--percentage", "10"}, mapGetenv(nil))
	if err == nil {
		t.Fatal("expected argument validation error")
	}
	if opened {
		t.Fatal("Temporal connection opened before argument validation")
	}
}

type fakeDeploymentHandle struct {
	descriptions        []client.WorkerDeploymentDescribeResponse
	versions            []client.WorkerDeploymentVersionDescription
	setCurrentResponses []client.WorkerDeploymentSetCurrentVersionResponse
	setRampingResponses []client.WorkerDeploymentSetRampingVersionResponse
	describeErr         error
	describeVersionErr  error
	setCurrentErr       error
	setRampingErr       error
	currentCalls        []client.WorkerDeploymentSetCurrentVersionOptions
	rampingCalls        []client.WorkerDeploymentSetRampingVersionOptions
}

func (f *fakeDeploymentHandle) Describe(context.Context, client.WorkerDeploymentDescribeOptions) (client.WorkerDeploymentDescribeResponse, error) {
	if f.describeErr != nil {
		return client.WorkerDeploymentDescribeResponse{}, f.describeErr
	}
	return takeResponse(&f.descriptions), nil
}

func (f *fakeDeploymentHandle) DescribeVersion(context.Context, client.WorkerDeploymentDescribeVersionOptions) (client.WorkerDeploymentVersionDescription, error) {
	if f.describeVersionErr != nil {
		return client.WorkerDeploymentVersionDescription{}, f.describeVersionErr
	}
	return takeResponse(&f.versions), nil
}

func (f *fakeDeploymentHandle) SetCurrentVersion(_ context.Context, options client.WorkerDeploymentSetCurrentVersionOptions) (client.WorkerDeploymentSetCurrentVersionResponse, error) {
	f.currentCalls = append(f.currentCalls, options)
	if f.setCurrentErr != nil {
		return client.WorkerDeploymentSetCurrentVersionResponse{}, f.setCurrentErr
	}
	return takeResponse(&f.setCurrentResponses), nil
}

func (f *fakeDeploymentHandle) SetRampingVersion(_ context.Context, options client.WorkerDeploymentSetRampingVersionOptions) (client.WorkerDeploymentSetRampingVersionResponse, error) {
	f.rampingCalls = append(f.rampingCalls, options)
	if f.setRampingErr != nil {
		return client.WorkerDeploymentSetRampingVersionResponse{}, f.setRampingErr
	}
	return takeResponse(&f.setRampingResponses), nil
}

func takeResponse[T any](responses *[]T) T {
	var zero T
	if len(*responses) == 0 {
		return zero
	}
	value := (*responses)[0]
	if len(*responses) > 1 {
		*responses = (*responses)[1:]
	}
	return value
}

func deploymentDescription(token, current, ramping string, percentage float32, releaseID string, drainage client.WorkerDeploymentVersionDrainageStatus) client.WorkerDeploymentDescribeResponse {
	description := client.WorkerDeploymentDescribeResponse{
		ConflictToken: []byte(token),
		Info: client.WorkerDeploymentInfo{
			Name: "worker",
			VersionSummaries: []client.WorkerDeploymentVersionSummary{{
				Version:        worker.WorkerDeploymentVersion{DeploymentName: "worker", BuildID: releaseID},
				DrainageStatus: drainage,
			}},
		},
	}
	if current != "" {
		description.Info.RoutingConfig.CurrentVersion = deploymentVersion(current)
	}
	if ramping != "" {
		description.Info.RoutingConfig.RampingVersion = deploymentVersion(ramping)
		description.Info.RoutingConfig.RampingVersionPercentage = percentage
	}
	return description
}

func versionDescription(releaseID string, drainage client.WorkerDeploymentVersionDrainageStatus) client.WorkerDeploymentVersionDescription {
	description := client.WorkerDeploymentVersionDescription{
		Info: client.WorkerDeploymentVersionInfo{
			Version: worker.WorkerDeploymentVersion{DeploymentName: "worker", BuildID: releaseID},
			TaskQueuesInfos: []client.WorkerDeploymentTaskQueueInfo{
				{Name: "cineweave-task-queue"},
			},
		},
	}
	if drainage != client.WorkerDeploymentVersionDrainageStatusUnspecified {
		description.Info.DrainageInfo = &client.WorkerDeploymentVersionDrainageInfo{
			DrainageStatus:  drainage,
			LastCheckedTime: time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC),
			LastChangedTime: time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC),
		}
	}
	return description
}

func deploymentVersion(buildID string) *worker.WorkerDeploymentVersion {
	return &worker.WorkerDeploymentVersion{DeploymentName: "worker", BuildID: buildID}
}

func testApplication(handle deploymentHandle) *application {
	return &application{
		out:    io.Discard,
		errOut: io.Discard,
		openHandle: func(context.Context, commandConfig) (deploymentHandle, func(), error) {
			return handle, func() {}, nil
		},
		wait: func(context.Context, time.Duration) error { return nil },
	}
}

func runApp(t *testing.T, handle deploymentHandle, args []string) commandResult {
	t.Helper()
	var output bytes.Buffer
	app := testApplication(handle)
	app.out = &output
	if err := app.run(context.Background(), args, mapGetenv(nil)); err != nil {
		t.Fatalf("run app: %v", err)
	}
	var result commandResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatalf("decode result %q: %v", output.String(), err)
	}
	return result
}

func mapGetenv(values map[string]string) getenvFunc {
	return func(key string) string {
		return values[key]
	}
}
