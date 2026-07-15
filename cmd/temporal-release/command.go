package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

const (
	defaultTemporalAddress   = "127.0.0.1:7233"
	defaultTemporalNamespace = "default"
	defaultIdentity          = "cineweave-temporal-release"
)

type commandName string

const (
	commandCheck    commandName = "check"
	commandRamp     commandName = "ramp"
	commandPromote  commandName = "promote"
	commandDrain    commandName = "drain"
	commandRollback commandName = "rollback"
)

type getenvFunc func(string) string

type commandConfig struct {
	name                    commandName
	address                 string
	namespace               string
	deploymentName          string
	releaseID               string
	identity                string
	timeout                 time.Duration
	pollInterval            time.Duration
	stableChecks            int
	percentage              float64
	allowNoPollers          bool
	ignoreMissingTaskQueues bool
	replaceRamping          bool
	force                   bool
}

type deploymentHandle interface {
	Describe(context.Context, client.WorkerDeploymentDescribeOptions) (client.WorkerDeploymentDescribeResponse, error)
	DescribeVersion(context.Context, client.WorkerDeploymentDescribeVersionOptions) (client.WorkerDeploymentVersionDescription, error)
	SetCurrentVersion(context.Context, client.WorkerDeploymentSetCurrentVersionOptions) (client.WorkerDeploymentSetCurrentVersionResponse, error)
	SetRampingVersion(context.Context, client.WorkerDeploymentSetRampingVersionOptions) (client.WorkerDeploymentSetRampingVersionResponse, error)
}

type handleFactory func(context.Context, commandConfig) (deploymentHandle, func(), error)
type waitFunc func(context.Context, time.Duration) error

type application struct {
	out        io.Writer
	errOut     io.Writer
	openHandle handleFactory
	wait       waitFunc
}

func newApplication(out, errOut io.Writer) *application {
	return &application{
		out:        out,
		errOut:     errOut,
		openHandle: openSDKHandle,
		wait:       waitFor,
	}
}

type releaseStatus struct {
	DeploymentName       string  `json:"deploymentName"`
	ReleaseID            string  `json:"releaseId"`
	CurrentReleaseID     string  `json:"currentReleaseId,omitempty"`
	RampingReleaseID     string  `json:"rampingReleaseId,omitempty"`
	RampingPercentage    float32 `json:"rampingPercentage,omitempty"`
	RoutingReachability  string  `json:"routingReachability"`
	DrainageStatus       string  `json:"drainageStatus"`
	SafeToDecommission   bool    `json:"safeToDecommission"`
	DrainageLastChecked  string  `json:"drainageLastChecked,omitempty"`
	DrainageLastChanged  string  `json:"drainageLastChanged,omitempty"`
	RegisteredTaskQueues int     `json:"registeredTaskQueues"`
}

type commandResult struct {
	Command            commandName   `json:"command"`
	Changed            bool          `json:"changed"`
	PreviousCurrentID  string        `json:"previousCurrentReleaseId,omitempty"`
	PreviousRampingID  string        `json:"previousRampingReleaseId,omitempty"`
	PreviousPercentage float32       `json:"previousRampingPercentage,omitempty"`
	Status             releaseStatus `json:"status"`
}

func (a *application) run(ctx context.Context, args []string, getenv getenvFunc) error {
	cfg, err := parseCommand(args, getenv, a.errOut)
	if err != nil {
		return err
	}

	commandCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	handle, closeHandle, err := a.openHandle(commandCtx, cfg)
	if err != nil {
		return fmt.Errorf("connect to Temporal at %q: %w", cfg.address, err)
	}
	if closeHandle != nil {
		defer closeHandle()
	}

	result, err := a.execute(commandCtx, handle, cfg)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(a.out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return fmt.Errorf("write command result: %w", err)
	}
	return nil
}

func (a *application) execute(ctx context.Context, handle deploymentHandle, cfg commandConfig) (commandResult, error) {
	switch cfg.name {
	case commandCheck:
		status, _, err := inspectRelease(ctx, handle, cfg)
		return commandResult{Command: cfg.name, Status: status}, err
	case commandRamp:
		return a.ramp(ctx, handle, cfg)
	case commandPromote:
		return a.promote(ctx, handle, cfg)
	case commandDrain:
		return a.drain(ctx, handle, cfg)
	case commandRollback:
		return a.rollback(ctx, handle, cfg)
	default:
		return commandResult{}, fmt.Errorf("unsupported command %q", cfg.name)
	}
}

func parseCommand(args []string, getenv getenvFunc, errOut io.Writer) (commandConfig, error) {
	if len(args) == 0 {
		return commandConfig{}, errors.New("a subcommand is required: check, ramp, promote, drain, or rollback")
	}

	name := commandName(strings.TrimSpace(args[0]))
	switch name {
	case commandCheck, commandRamp, commandPromote, commandDrain, commandRollback:
	default:
		return commandConfig{}, fmt.Errorf("unknown subcommand %q; expected check, ramp, promote, drain, or rollback", args[0])
	}

	cfg := commandConfig{
		name:           name,
		address:        envOrDefault(getenv, "CINEWEAVE_TEMPORAL_ADDRESS", defaultTemporalAddress),
		namespace:      envOrDefault(getenv, "CINEWEAVE_TEMPORAL_NAMESPACE", defaultTemporalNamespace),
		deploymentName: strings.TrimSpace(getenv("CINEWEAVE_TEMPORAL_DEPLOYMENT_NAME")),
		releaseID:      strings.TrimSpace(getenv("CINEWEAVE_RELEASE_ID")),
		identity:       envOrDefault(getenv, "CINEWEAVE_TEMPORAL_RELEASE_IDENTITY", defaultIdentity),
		timeout:        30 * time.Second,
		pollInterval:   15 * time.Second,
		stableChecks:   2,
	}
	if name == commandDrain {
		cfg.timeout = 24 * time.Hour
	}

	flags := flag.NewFlagSet(string(name), flag.ContinueOnError)
	flags.SetOutput(errOut)
	flags.StringVar(&cfg.address, "address", cfg.address, "Temporal frontend address")
	flags.StringVar(&cfg.namespace, "namespace", cfg.namespace, "Temporal namespace")
	flags.StringVar(&cfg.deploymentName, "deployment", cfg.deploymentName, "Worker Deployment name")
	flags.StringVar(&cfg.releaseID, "release-id", cfg.releaseID, "immutable Worker Deployment Build ID")
	flags.StringVar(&cfg.identity, "identity", cfg.identity, "release controller identity")
	flags.DurationVar(&cfg.timeout, "timeout", cfg.timeout, "overall command timeout")

	switch name {
	case commandRamp:
		flags.Float64Var(&cfg.percentage, "percentage", 0, "traffic percentage to ramp to this release (0, 100]")
		flags.BoolVar(&cfg.replaceRamping, "replace-ramping", false, "replace a different active ramping release")
		addRoutingSafetyFlags(flags, &cfg)
	case commandPromote:
		flags.BoolVar(&cfg.force, "force", false, "promote without first ramping this release to 100 percent")
		addRoutingSafetyFlags(flags, &cfg)
	case commandRollback:
		addRoutingSafetyFlags(flags, &cfg)
	case commandDrain:
		flags.DurationVar(&cfg.pollInterval, "poll-interval", cfg.pollInterval, "interval between drainage checks")
		flags.IntVar(&cfg.stableChecks, "stable-checks", cfg.stableChecks, "consecutive safe observations required")
	}

	if err := flags.Parse(args[1:]); err != nil {
		return commandConfig{}, err
	}
	if flags.NArg() != 0 {
		return commandConfig{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}

	trimCommandConfig(&cfg)
	if err := validateCommand(cfg); err != nil {
		return commandConfig{}, err
	}
	return cfg, nil
}

func addRoutingSafetyFlags(flags *flag.FlagSet, cfg *commandConfig) {
	flags.BoolVar(&cfg.allowNoPollers, "allow-no-pollers", false, "allow routing to a release with no observed pollers")
	flags.BoolVar(&cfg.ignoreMissingTaskQueues, "ignore-missing-task-queues", false, "allow routing to a release that does not poll all expected task queues")
}

func trimCommandConfig(cfg *commandConfig) {
	cfg.address = strings.TrimSpace(cfg.address)
	cfg.namespace = strings.TrimSpace(cfg.namespace)
	cfg.deploymentName = strings.TrimSpace(cfg.deploymentName)
	cfg.releaseID = strings.TrimSpace(cfg.releaseID)
	cfg.identity = strings.TrimSpace(cfg.identity)
}

func validateCommand(cfg commandConfig) error {
	if cfg.address == "" {
		return errors.New("Temporal address is required (--address or CINEWEAVE_TEMPORAL_ADDRESS)")
	}
	if cfg.namespace == "" {
		return errors.New("Temporal namespace is required (--namespace or CINEWEAVE_TEMPORAL_NAMESPACE)")
	}
	if cfg.deploymentName == "" {
		return errors.New("deployment name is required (--deployment or CINEWEAVE_TEMPORAL_DEPLOYMENT_NAME)")
	}
	if strings.Contains(cfg.deploymentName, ".") {
		return fmt.Errorf("deployment name %q is invalid: Worker Deployment names cannot contain '.'", cfg.deploymentName)
	}
	if cfg.releaseID == "" {
		return errors.New("release ID is required (--release-id or CINEWEAVE_RELEASE_ID)")
	}
	if cfg.identity == "" {
		return errors.New("release controller identity cannot be empty")
	}
	if cfg.timeout <= 0 {
		return errors.New("timeout must be greater than zero")
	}
	if cfg.name == commandRamp && (math.IsNaN(cfg.percentage) || math.IsInf(cfg.percentage, 0) || cfg.percentage <= 0 || cfg.percentage > 100) {
		return errors.New("ramp percentage must be greater than 0 and no greater than 100")
	}
	if cfg.name == commandDrain {
		if cfg.pollInterval <= 0 {
			return errors.New("drain poll interval must be greater than zero")
		}
		if cfg.stableChecks < 1 {
			return errors.New("drain stable checks must be at least 1")
		}
	}
	return nil
}

func envOrDefault(getenv getenvFunc, key, fallback string) string {
	if value := strings.TrimSpace(getenv(key)); value != "" {
		return value
	}
	return fallback
}

func inspectRelease(ctx context.Context, handle deploymentHandle, cfg commandConfig) (releaseStatus, client.WorkerDeploymentDescribeResponse, error) {
	description, err := handle.Describe(ctx, client.WorkerDeploymentDescribeOptions{})
	if err != nil {
		return releaseStatus{}, client.WorkerDeploymentDescribeResponse{}, fmt.Errorf("describe deployment %q: %w", cfg.deploymentName, err)
	}
	version, err := handle.DescribeVersion(ctx, client.WorkerDeploymentDescribeVersionOptions{BuildID: cfg.releaseID})
	if err != nil {
		return releaseStatus{}, client.WorkerDeploymentDescribeResponse{}, fmt.Errorf("describe release %q in deployment %q: %w", cfg.releaseID, cfg.deploymentName, err)
	}

	status := buildReleaseStatus(description, version, cfg)
	return status, description, nil
}

func buildReleaseStatus(description client.WorkerDeploymentDescribeResponse, version client.WorkerDeploymentVersionDescription, cfg commandConfig) releaseStatus {
	routing := description.Info.RoutingConfig
	status := releaseStatus{
		DeploymentName:       cfg.deploymentName,
		ReleaseID:            cfg.releaseID,
		RoutingReachability:  "inactive",
		DrainageStatus:       "unspecified",
		RegisteredTaskQueues: len(version.Info.TaskQueuesInfos),
	}
	if routing.CurrentVersion != nil {
		status.CurrentReleaseID = routing.CurrentVersion.BuildID
	}
	if routing.RampingVersion != nil {
		status.RampingReleaseID = routing.RampingVersion.BuildID
		status.RampingPercentage = routing.RampingVersionPercentage
	}
	if status.CurrentReleaseID == cfg.releaseID {
		status.RoutingReachability = "current"
	} else if status.RampingReleaseID == cfg.releaseID {
		status.RoutingReachability = "ramping"
	}

	if version.Info.DrainageInfo != nil {
		status.DrainageStatus = drainageStatusName(version.Info.DrainageInfo.DrainageStatus)
		status.DrainageLastChecked = formatTime(version.Info.DrainageInfo.LastCheckedTime)
		status.DrainageLastChanged = formatTime(version.Info.DrainageInfo.LastChangedTime)
	} else {
		for _, summary := range description.Info.VersionSummaries {
			if summary.Version.BuildID == cfg.releaseID {
				status.DrainageStatus = drainageStatusName(summary.DrainageStatus)
				break
			}
		}
	}
	status.SafeToDecommission = status.RoutingReachability == "inactive" && status.DrainageStatus == "drained"
	return status
}

func drainageStatusName(status client.WorkerDeploymentVersionDrainageStatus) string {
	switch status {
	case client.WorkerDeploymentVersionDrainageStatusDraining:
		return "draining"
	case client.WorkerDeploymentVersionDrainageStatusDrained:
		return "drained"
	default:
		return "unspecified"
	}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func sameRelease(versionID, releaseID string) bool {
	return versionID != "" && versionID == releaseID
}

func currentReleaseID(description client.WorkerDeploymentDescribeResponse) string {
	if description.Info.RoutingConfig.CurrentVersion == nil {
		return ""
	}
	return description.Info.RoutingConfig.CurrentVersion.BuildID
}

func rampingReleaseID(description client.WorkerDeploymentDescribeResponse) string {
	if description.Info.RoutingConfig.RampingVersion == nil {
		return ""
	}
	return description.Info.RoutingConfig.RampingVersion.BuildID
}

func (a *application) ramp(ctx context.Context, handle deploymentHandle, cfg commandConfig) (commandResult, error) {
	status, description, err := inspectRelease(ctx, handle, cfg)
	if err != nil {
		return commandResult{}, err
	}
	if sameRelease(currentReleaseID(description), cfg.releaseID) {
		return commandResult{}, fmt.Errorf("release %q is already current and cannot be ramping", cfg.releaseID)
	}
	previousRamping := rampingReleaseID(description)
	if previousRamping != "" && previousRamping != cfg.releaseID && !cfg.replaceRamping {
		return commandResult{}, fmt.Errorf("release %q is already ramping; pass --replace-ramping to replace it explicitly", previousRamping)
	}
	if previousRamping == cfg.releaseID && floatEqual(description.Info.RoutingConfig.RampingVersionPercentage, float32(cfg.percentage)) {
		return commandResult{Command: cfg.name, Status: status}, nil
	}

	response, err := handle.SetRampingVersion(ctx, client.WorkerDeploymentSetRampingVersionOptions{
		BuildID:                 cfg.releaseID,
		Percentage:              float32(cfg.percentage),
		ConflictToken:           description.ConflictToken,
		Identity:                cfg.identity,
		IgnoreMissingTaskQueues: cfg.ignoreMissingTaskQueues,
		AllowNoPollers:          cfg.allowNoPollers,
	})
	if err != nil {
		return commandResult{}, fmt.Errorf("ramp release %q to %.2f percent: %w", cfg.releaseID, cfg.percentage, err)
	}
	status, _, err = inspectRelease(ctx, handle, cfg)
	if err != nil {
		return commandResult{}, fmt.Errorf("verify ramp operation: %w", err)
	}
	return commandResult{
		Command:            cfg.name,
		Changed:            true,
		PreviousRampingID:  buildID(response.PreviousVersion),
		PreviousPercentage: response.PreviousPercentage,
		Status:             status,
	}, nil
}

func (a *application) promote(ctx context.Context, handle deploymentHandle, cfg commandConfig) (commandResult, error) {
	status, description, err := inspectRelease(ctx, handle, cfg)
	if err != nil {
		return commandResult{}, err
	}
	previousCurrent := currentReleaseID(description)
	if sameRelease(previousCurrent, cfg.releaseID) {
		return commandResult{Command: cfg.name, Status: status}, nil
	}
	if previousCurrent != "" && !cfg.force {
		routing := description.Info.RoutingConfig
		if routing.RampingVersion == nil || routing.RampingVersion.BuildID != cfg.releaseID || !floatEqual(routing.RampingVersionPercentage, 100) {
			return commandResult{}, fmt.Errorf("release %q must be ramping at 100 percent before promotion; pass --force for an explicit direct cutover", cfg.releaseID)
		}
	}

	response, err := handle.SetCurrentVersion(ctx, client.WorkerDeploymentSetCurrentVersionOptions{
		BuildID:                 cfg.releaseID,
		ConflictToken:           description.ConflictToken,
		Identity:                cfg.identity,
		IgnoreMissingTaskQueues: cfg.ignoreMissingTaskQueues,
		AllowNoPollers:          cfg.allowNoPollers,
	})
	if err != nil {
		return commandResult{}, fmt.Errorf("promote release %q: %w", cfg.releaseID, err)
	}
	status, _, err = inspectRelease(ctx, handle, cfg)
	if err != nil {
		return commandResult{}, fmt.Errorf("verify promote operation: %w", err)
	}
	return commandResult{
		Command:           cfg.name,
		Changed:           true,
		PreviousCurrentID: buildID(response.PreviousVersion),
		Status:            status,
	}, nil
}

func (a *application) rollback(ctx context.Context, handle deploymentHandle, cfg commandConfig) (commandResult, error) {
	status, description, err := inspectRelease(ctx, handle, cfg)
	if err != nil {
		return commandResult{}, err
	}
	previousCurrent := currentReleaseID(description)
	previousRamping := rampingReleaseID(description)
	conflictToken := description.ConflictToken
	changed := false

	if previousCurrent != cfg.releaseID {
		response, setErr := handle.SetCurrentVersion(ctx, client.WorkerDeploymentSetCurrentVersionOptions{
			BuildID:                 cfg.releaseID,
			ConflictToken:           conflictToken,
			Identity:                cfg.identity,
			IgnoreMissingTaskQueues: cfg.ignoreMissingTaskQueues,
			AllowNoPollers:          cfg.allowNoPollers,
		})
		if setErr != nil {
			return commandResult{}, fmt.Errorf("roll back current release to %q: %w", cfg.releaseID, setErr)
		}
		conflictToken = response.ConflictToken
		changed = true
	}

	if previousRamping != "" && previousRamping != cfg.releaseID {
		_, clearErr := handle.SetRampingVersion(ctx, client.WorkerDeploymentSetRampingVersionOptions{
			BuildID:       "",
			Percentage:    0,
			ConflictToken: conflictToken,
			Identity:      cfg.identity,
		})
		if clearErr != nil {
			return commandResult{}, fmt.Errorf("current release was rolled back to %q but clearing ramping release %q failed: %w", cfg.releaseID, previousRamping, clearErr)
		}
		changed = true
	}

	if !changed {
		return commandResult{Command: cfg.name, Status: status}, nil
	}
	status, _, err = inspectRelease(ctx, handle, cfg)
	if err != nil {
		return commandResult{}, fmt.Errorf("verify rollback operation: %w", err)
	}
	return commandResult{
		Command:           cfg.name,
		Changed:           true,
		PreviousCurrentID: previousCurrent,
		PreviousRampingID: previousRamping,
		Status:            status,
	}, nil
}

func (a *application) drain(ctx context.Context, handle deploymentHandle, cfg commandConfig) (commandResult, error) {
	stable := 0
	var lastStatus releaseStatus
	for {
		status, _, err := inspectRelease(ctx, handle, cfg)
		if err != nil {
			return commandResult{}, err
		}
		lastStatus = status
		if status.RoutingReachability != "inactive" {
			return commandResult{}, fmt.Errorf("release %q is still %s; route current and ramping traffic away before draining", cfg.releaseID, status.RoutingReachability)
		}
		if status.SafeToDecommission {
			stable++
			if stable >= cfg.stableChecks {
				return commandResult{Command: cfg.name, Status: status}, nil
			}
		} else {
			stable = 0
		}

		_, _ = fmt.Fprintf(a.errOut, "waiting for release %s: drainage=%s safe-observations=%d/%d\n", cfg.releaseID, status.DrainageStatus, stable, cfg.stableChecks)
		if err := a.wait(ctx, cfg.pollInterval); err != nil {
			return commandResult{}, fmt.Errorf("wait for release %q to drain (last drainage=%s, reachability=%s): %w", cfg.releaseID, lastStatus.DrainageStatus, lastStatus.RoutingReachability, err)
		}
	}
}

func waitFor(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func floatEqual(left, right float32) bool {
	return math.Abs(float64(left-right)) < 0.0001
}

func buildID(version *worker.WorkerDeploymentVersion) string {
	if version == nil {
		return ""
	}
	return version.BuildID
}
