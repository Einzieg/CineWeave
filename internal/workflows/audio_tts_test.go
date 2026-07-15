package workflows

import (
	"context"
	"errors"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestEpisodeAudioProductionWorkflowPreservesPartialSuccess(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := episodeAudioWorkflowTestInput()
	registerEpisodeAudioTimingSource(env, "analysis-1")
	env.RegisterActivityWithOptions(func(_ context.Context, prepared PrepareEpisodeTTSInput) (PrepareEpisodeTTSOutput, error) {
		if prepared.TimingAnalysisID != "analysis-1" {
			t.Fatalf("timing analysis = %q", prepared.TimingAnalysisID)
		}
		return PrepareEpisodeTTSOutput{TimingAnalysisID: "analysis-1", TimelineTimebase: 90_000, FPSNumerator: 24, FPSDenominator: 1, Jobs: []TTSGenerationJob{
			{ClipID: "clip-1", TimingUnitID: "unit-1"}, {ClipID: "clip-2", TimingUnitID: "unit-2"}, {ClipID: "clip-3", TimingUnitID: "unit-3"},
		}}, nil
	}, activity.RegisterOptions{Name: "PrepareEpisodeTTS"})
	env.RegisterActivityWithOptions(func(_ context.Context, request GenerateTTSAudioInput) (GenerateTTSAudioOutput, error) {
		if request.ClipID == "clip-2" {
			return GenerateTTSAudioOutput{ClipID: request.ClipID, TimingUnitID: "unit-2", Status: "failed", ErrorCode: "UPSTREAM_TIMEOUT", ErrorMessage: "provider timed out"}, nil
		}
		return GenerateTTSAudioOutput{ClipID: request.ClipID, TimingUnitID: "unit-" + request.ClipID[len(request.ClipID)-1:], Status: "succeeded"}, nil
	}, activity.RegisterOptions{Name: "GenerateTTSAudio"})
	var completed EpisodeAudioProductionOutput
	env.RegisterActivityWithOptions(func(_ context.Context, _ EpisodeAudioProductionInput, output EpisodeAudioProductionOutput) error {
		completed = output
		return nil
	}, activity.RegisterOptions{Name: "CompleteEpisodeAudioProductionWorkflow"})

	env.ExecuteWorkflow(EpisodeAudioProductionWorkflow, input)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var output EpisodeAudioProductionOutput
	if err := env.GetWorkflowResult(&output); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if output.Status != "partial_succeeded" || len(output.SucceededClipIDs) != 2 || len(output.FailedClipIDs) != 1 || output.FailedClipIDs[0] != "clip-2" {
		t.Fatalf("output = %+v", output)
	}
	if completed.Status != output.Status || completed.Errors["clip-2"] != "provider timed out" {
		t.Fatalf("completed output = %+v", completed)
	}
}

func TestEpisodeAudioProductionWorkflowCreatesTimingAndMixWhenAnalysisMissing(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := episodeAudioWorkflowTestInput()
	registerEpisodeAudioTimingSource(env, "")
	env.RegisterActivityWithOptions(func(_ context.Context, request AnalyzeEpisodeTimingInput) (TimingAnalysisActivityOutput, error) {
		if request.ScriptID != "script-1" || request.ScriptVersionID != "version-1" || request.ScriptEpisodeID != input.ScriptEpisodeID {
			t.Fatalf("analysis request = %+v", request)
		}
		return TimingAnalysisActivityOutput{AnalysisID: "analysis-generated", ScriptID: request.ScriptID, ScriptVersionID: request.ScriptVersionID, ScriptEpisodeID: request.ScriptEpisodeID}, nil
	}, activity.RegisterOptions{Name: "AnalyzeEpisodeTiming"})
	env.RegisterActivityWithOptions(func(_ context.Context, prepared PrepareEpisodeTTSInput) (PrepareEpisodeTTSOutput, error) {
		if prepared.TimingAnalysisID != "analysis-generated" {
			return PrepareEpisodeTTSOutput{}, errors.New("generated timing analysis was not forwarded")
		}
		return PrepareEpisodeTTSOutput{TimingAnalysisID: prepared.TimingAnalysisID, TimelineTimebase: 90_000, FPSNumerator: 24, FPSDenominator: 1, Jobs: []TTSGenerationJob{{ClipID: "clip-1", TimingUnitID: "unit-1"}}}, nil
	}, activity.RegisterOptions{Name: "PrepareEpisodeTTS"})
	env.RegisterActivityWithOptions(func(context.Context, GenerateTTSAudioInput) (GenerateTTSAudioOutput, error) {
		return GenerateTTSAudioOutput{ClipID: "clip-1", TimingUnitID: "unit-1", Status: "succeeded", DurationTicks: 180_000}, nil
	}, activity.RegisterOptions{Name: "GenerateTTSAudio"})
	env.RegisterActivityWithOptions(func(_ context.Context, request CreateTTSTimingRevisionInput) (CreateTTSTimingRevisionOutput, error) {
		if request.SourceAnalysisID != "analysis-generated" {
			t.Fatalf("timing revision request = %+v", request)
		}
		return CreateTTSTimingRevisionOutput{SourceAnalysisID: request.SourceAnalysisID, TimingAnalysisID: "analysis-tts", Revision: 2, EstimatedDurationTicks: 180_000, TimelineTimebase: 90_000, TTSUnitCount: 1}, nil
	}, activity.RegisterOptions{Name: "CreateTTSTimingRevision"})
	env.RegisterActivityWithOptions(func(_ context.Context, request ComposeEpisodeAudioMixInput) (ComposeEpisodeAudioMixOutput, error) {
		if request.TimingAnalysisID != "analysis-tts" {
			t.Fatalf("mix request = %+v", request)
		}
		return ComposeEpisodeAudioMixOutput{AudioMixVersionID: "mix-1", Revision: 1, TrackCount: 1, ProductionReadiness: "ready"}, nil
	}, activity.RegisterOptions{Name: "ComposeEpisodeAudioMix"})
	env.RegisterActivityWithOptions(func(context.Context, RefreshTimingCalibrationProfileInput) error { return nil }, activity.RegisterOptions{Name: "RefreshTimingCalibrationProfile"})
	env.RegisterActivityWithOptions(func(context.Context, EpisodeAudioProductionInput, EpisodeAudioProductionOutput) error { return nil }, activity.RegisterOptions{Name: "CompleteEpisodeAudioProductionWorkflow"})

	env.ExecuteWorkflow(EpisodeAudioProductionWorkflow, input)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var output EpisodeAudioProductionOutput
	if err := env.GetWorkflowResult(&output); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if output.Status != "succeeded" || output.TimingRevision == nil || output.TimingRevision.TimingAnalysisID != "analysis-tts" || output.Mix == nil || output.Mix.AudioMixVersionID != "mix-1" {
		t.Fatalf("output = %+v", output)
	}
}

func registerEpisodeAudioTimingSource(env *testsuite.TestWorkflowEnvironment, analysisID string) {
	env.RegisterActivityWithOptions(func(context.Context, ResolveEpisodeAudioTimingInput) (ResolveEpisodeAudioTimingOutput, error) {
		return ResolveEpisodeAudioTimingOutput{ScriptID: "script-1", ScriptVersionID: "version-1", TimingAnalysisID: analysisID}, nil
	}, activity.RegisterOptions{Name: "ResolveEpisodeAudioTiming"})
}

func episodeAudioWorkflowTestInput() EpisodeAudioProductionInput {
	return EpisodeAudioProductionInput{
		OrganizationID: "org-1", ProjectID: "project-1", WorkflowRunID: "run-1", CreatedBy: "user-1",
		ScriptEpisodeID: "episode-1", MaxConcurrency: 2, MixAfterTTS: true,
	}
}
