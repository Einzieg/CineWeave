package workflows

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/commerce"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

func TestCommerceVideoPromptBatchUsesBoundedConcurrencyAndKeepsPartialSuccess(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := testCommerceVideoBatchInput("generate_prompts", 2, []string{"shot-1", "shot-2", "shot-3", "shot-4"})

	var mu sync.Mutex
	active := 0
	maxActive := 0
	calls := make(map[string]int)
	release := make(chan struct{})
	var releaseOnce sync.Once
	env.RegisterActivityWithOptions(func(ctx context.Context, _ CommerceVideoBatchInput, shotID string) (CommerceVideoItemOutput, error) {
		mu.Lock()
		active++
		calls[shotID]++
		if active > maxActive {
			maxActive = active
		}
		if active >= input.Concurrency {
			releaseOnce.Do(func() { close(release) })
		}
		mu.Unlock()
		select {
		case <-release:
		case <-time.After(time.Second):
			return CommerceVideoItemOutput{}, errors.New("video prompt activities did not overlap")
		case <-ctx.Done():
			return CommerceVideoItemOutput{}, ctx.Err()
		}
		time.Sleep(15 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		if shotID == "shot-3" {
			return CommerceVideoItemOutput{}, errors.New("review rejected the prompt")
		}
		return CommerceVideoItemOutput{ShotID: shotID, Status: commerce.ItemSucceeded}, nil
	}, activity.RegisterOptions{Name: GenerateCommerceVideoPromptItemActivityName})
	env.RegisterActivityWithOptions(func(_ context.Context, _ CommerceVideoBatchInput, output CommerceVideoBatchOutput) (CommerceVideoBatchOutput, error) {
		return output, nil
	}, activity.RegisterOptions{Name: FinalizeCommerceVideoBatchActivityName})
	env.RegisterActivityWithOptions(func(context.Context, FinalizeCommerceVideoFailureInput) error { return nil }, activity.RegisterOptions{Name: FinalizeCommerceVideoFailureActivityName})

	env.ExecuteWorkflow(CommerceVideoPromptBatchWorkflow, input)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var output CommerceVideoBatchOutput
	require.NoError(t, env.GetWorkflowResult(&output))
	require.Equal(t, commerce.RunPartiallySucceeded, output.Status)
	require.Equal(t, 3, output.Succeeded)
	require.Equal(t, 1, output.Failed)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 2, maxActive)
	require.Equal(t, 1, calls["shot-3"], "video prompt item must be retried only by an explicit production-run retry")
}

func TestCommerceShotVideoWorkflowKeepsItemFailureInsideBatchScope(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := testCommerceVideoBatchInput("generate_videos", 1, []string{"shot-1"})

	var failureScope string
	failItemCalled := false
	env.RegisterActivityWithOptions(func(context.Context, CommerceVideoBatchInput, string) (CommerceReferenceImageItemAttempt, error) {
		return CommerceReferenceImageItemAttempt{
			ItemID:    "00000000-0000-4000-8000-000000000021",
			AttemptID: "00000000-0000-4000-8000-000000000022",
		}, nil
	}, activity.RegisterOptions{Name: BeginCommerceVideoItemActivityName})
	env.RegisterActivityWithOptions(func(context.Context, CommerceVideoBatchInput, string) (CommerceVideoExecutionShot, error) {
		return CommerceVideoExecutionShot{
			ShotID: "shot-1", ShotIndex: 0, ShotNo: 1, AspectRatio: "16:9",
			Resolution: "720p", AudioStrategy: "native_av", AudioRequirement: "preferred",
		}, nil
	}, activity.RegisterOptions{Name: LoadCommerceVideoExecutionShotActivityName})
	env.RegisterActivityWithOptions(func(_ context.Context, planInput EnsurePreparedShotVideoPlanInput) (LoadPreparedShotVideoPlanOutput, error) {
		failureScope = planInput.FailureScope
		return LoadPreparedShotVideoPlanOutput{}, temporal.NewNonRetryableApplicationError(
			"render plan contract changed",
			"RENDER_PLAN_REPLAN_REQUIRED",
			nil,
		)
	}, activity.RegisterOptions{Name: "EnsurePreparedShotVideoPlan"})
	env.RegisterActivityWithOptions(func(context.Context, FailCommerceVideoItemInput) error {
		failItemCalled = true
		return nil
	}, activity.RegisterOptions{Name: FailCommerceVideoItemActivityName})

	env.ExecuteWorkflow(CommerceShotVideoWorkflow, input, "shot-1")

	require.Error(t, env.GetWorkflowError())
	require.Equal(t, workflowFailureScopeBatchItem, failureScope)
	require.True(t, failItemCalled)
}

func TestValidateCommerceVideoPromptPlanSeparatesVisualOverlayAndAudio(t *testing.T) {
	identity := testCommerceReferenceImageIdentity()
	snapshot := CommerceVideoPromptShotSnapshot{
		Identity: identity, ProductVersionID: "product-version", ReferencePackID: "reference-pack",
		SourceSegmentIDs: []string{"segment-1"}, InstructionLanguage: "en-US", TargetLocale: "zh-CN",
		SupportedPromptLanguages: []string{"en-US"}, NativeAudioLanguages: []string{"zh-CN"},
		VoiceoverText: "现在下单立即享受优惠", OnscreenText: "限时优惠",
		SoundEffects: []string{"包装开启声"}, MusicCue: "轻快器乐",
		FirstFrame:      CommerceVideoFirstFrameReference{ImageVersionID: "image-version"},
		DurationSeconds: 5, AllowedDurations: []int{5, 10}, NativeAudioRequested: true,
	}
	base := CommerceVideoPromptPlanContract{
		ContractVersion:      CommerceVideoPromptPlanContractVersion,
		CommerceScriptUnitID: identity.ScriptUnitID, ScriptUnitGenerationID: identity.UnitGenerationID,
		CommerceWorkflowBindingID: identity.CommerceWorkflowBindingID, ProductVersionID: snapshot.ProductVersionID,
		SourceSegmentIDs: []string{"segment-1"}, InstructionLanguage: "en-US", SpokenLanguage: "zh-CN",
		VisualPrompt:  "A close product demonstration with exact packaging and a smooth push-in camera move.",
		VoiceoverText: snapshot.VoiceoverText, OnscreenText: snapshot.OnscreenText,
		SoundEffects: append([]string(nil), snapshot.SoundEffects...), MusicCue: snapshot.MusicCue,
		NativeAudioRequested: true, ReferencePackID: snapshot.ReferencePackID,
		ReferenceIDs: []string{snapshot.FirstFrame.ImageVersionID}, DurationSeconds: snapshot.DurationSeconds,
	}
	require.NoError(t, ValidateCommerceVideoPromptPlan(base, snapshot))

	for name, mutate := range map[string]func(*CommerceVideoPromptPlanContract){
		"overlay in visual prompt":   func(item *CommerceVideoPromptPlanContract) { item.VisualPrompt += " " + snapshot.OnscreenText },
		"voiceover in visual prompt": func(item *CommerceVideoPromptPlanContract) { item.VisualPrompt += " " + snapshot.VoiceoverText },
		"sound effect in voiceover":  func(item *CommerceVideoPromptPlanContract) { item.VoiceoverText += snapshot.SoundEffects[0] },
		"music in voiceover":         func(item *CommerceVideoPromptPlanContract) { item.VoiceoverText += snapshot.MusicCue },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := base
			mutate(&candidate)
			require.Error(t, ValidateCommerceVideoPromptPlan(candidate, snapshot))
		})
	}

	rendered := renderCommerceVideoProviderPrompt(base)
	require.NotContains(t, rendered, snapshot.VoiceoverText)
	require.NotContains(t, rendered, snapshot.OnscreenText)
	require.Contains(t, rendered, snapshot.SoundEffects[0])
	require.Contains(t, rendered, snapshot.MusicCue)
	require.Contains(t, rendered, "Never vocalize")
}

func TestCommerceVideoDialogueCuesUseOnlyExactVoiceover(t *testing.T) {
	snapshot := CommerceVideoPromptShotSnapshot{
		StoryboardShotID: "shot-1", VoiceoverText: "逐字旁白",
		SoundEffects: []string{"雷声", "提示音"}, MusicCue: "紧张配乐", DurationTicks: 120,
	}
	cues := commerceVideoDialogueCues(snapshot)
	require.Len(t, cues, 1)
	require.Equal(t, "逐字旁白", cues[0].Text)
	require.Equal(t, "voiceover", cues[0].Kind)
	require.NotContains(t, cues[0].Text, "雷声")
	require.NotContains(t, cues[0].Text, "紧张配乐")
}

func TestCommerceVideoReviewRoundLimitNeverExceedsThree(t *testing.T) {
	bindings := CommerceVideoPromptAgentBindings{
		VideoPromptAgent:    CommerceAgentBinding{MaxReviewRounds: 8},
		VideoPromptReviewer: CommerceAgentBinding{MaxReviewRounds: 2},
	}
	require.Equal(t, 2, commerceVideoReviewRoundLimit(bindings))
	bindings.VideoPromptAgent.MaxReviewRounds = 0
	bindings.VideoPromptReviewer.MaxReviewRounds = 0
	require.Equal(t, CommerceMaxAgentReviewRounds, commerceVideoReviewRoundLimit(bindings))
}

func TestCommerceVideoProviderProvenanceUsesFinalSuccessfulPoll(t *testing.T) {
	requestID, callID, taskID := commerceVideoProviderProvenance(ShotRenderExecutionResult{
		Polls: []PollShotVideoTaskOutput{
			{
				ProviderRequestID:   "request-failed",
				ProviderCallID:      "call-failed",
				ProviderAsyncTaskID: "task-failed",
				Status:              "failed",
			},
			{
				ProviderRequestID:   "request-success",
				ProviderCallID:      "call-success",
				ProviderAsyncTaskID: "task-success",
				Status:              "succeeded",
			},
		},
		LastSegment: PollShotVideoTaskOutput{
			ProviderRequestID:   "request-success",
			ProviderCallID:      "call-success",
			ProviderAsyncTaskID: "task-success",
			Status:              "succeeded",
		},
	})

	require.Equal(t, "request-success", requestID)
	require.Equal(t, "call-success", callID)
	require.Equal(t, "task-success", taskID)
}

func testCommerceVideoBatchInput(operation string, concurrency int, shots []string) CommerceVideoBatchInput {
	return CommerceVideoBatchInput{
		Identity: testCommerceReferenceImageIdentity(), WorkflowRunID: "00000000-0000-4000-8000-000000000011",
		ProductionRunID: "00000000-0000-4000-8000-000000000012", StoryboardPlanID: "00000000-0000-4000-8000-000000000013",
		PlanEditRevision: 1, Operation: operation, ShotIDs: shots, Concurrency: concurrency,
		Resolution: "1080p", CreatedBy: "00000000-0000-4000-8000-000000000014", AttemptGeneration: 1,
	}
}
