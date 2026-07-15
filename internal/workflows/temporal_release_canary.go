package workflows

import (
	"fmt"

	"go.temporal.io/sdk/workflow"
)

const (
	TemporalReleaseCanaryCompleteSignal = "cineweave.release_canary.complete"
	TemporalReleaseCanaryStateQuery     = "cineweave.release_canary.state"
)

type TemporalReleaseCanaryInput struct {
	ReleaseMarker string `json:"releaseMarker"`
}

type TemporalReleaseCanaryState struct {
	Status         string `json:"status"`
	ReleaseMarker  string `json:"releaseMarker"`
	StartedBuildID string `json:"startedBuildId"`
}

type TemporalReleaseCanaryOutput struct {
	ReleaseMarker    string `json:"releaseMarker"`
	StartedBuildID   string `json:"startedBuildId"`
	CompletedBuildID string `json:"completedBuildId"`
	PatchVersion     int    `json:"patchVersion"`
}

// TemporalReleaseCanaryWorkflow is an operational release probe. It has no
// external side effects and remains open until explicitly signalled.
func TemporalReleaseCanaryWorkflow(ctx workflow.Context, input TemporalReleaseCanaryInput) (TemporalReleaseCanaryOutput, error) {
	patchVersion := workflow.GetVersion(ctx, "temporal-release-canary-v1", workflow.DefaultVersion, 1)
	state := TemporalReleaseCanaryState{
		Status:         "waiting",
		ReleaseMarker:  input.ReleaseMarker,
		StartedBuildID: workflow.GetInfo(ctx).GetCurrentBuildID(),
	}
	if err := workflow.SetQueryHandler(ctx, TemporalReleaseCanaryStateQuery, func() (TemporalReleaseCanaryState, error) {
		return state, nil
	}); err != nil {
		return TemporalReleaseCanaryOutput{}, fmt.Errorf("register release canary query: %w", err)
	}
	var completionMarker string
	workflow.GetSignalChannel(ctx, TemporalReleaseCanaryCompleteSignal).Receive(ctx, &completionMarker)
	if completionMarker != input.ReleaseMarker {
		return TemporalReleaseCanaryOutput{}, fmt.Errorf("release canary completion marker mismatch")
	}
	return TemporalReleaseCanaryOutput{
		ReleaseMarker:    input.ReleaseMarker,
		StartedBuildID:   state.StartedBuildID,
		CompletedBuildID: workflow.GetInfo(ctx).GetCurrentBuildID(),
		PatchVersion:     int(patchVersion),
	}, nil
}
