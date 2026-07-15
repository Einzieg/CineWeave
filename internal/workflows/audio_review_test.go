package workflows

import (
	"context"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestNativeAudioReviewWorkflowPreservesPassedSegmentsWhenOneFails(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := NativeAudioReviewWorkflowInput{
		OrganizationID: "org-1", ProjectID: "project-1", WorkflowRunID: "run-1", CreatedBy: "user-1",
		StoryboardShotID: "shot-1", VideoRenderPlanID: "plan-1", MaxConcurrency: 2,
	}
	env.RegisterActivityWithOptions(func(context.Context, NativeAudioReviewWorkflowInput) (PrepareNativeAudioReviewOutput, error) {
		return PrepareNativeAudioReviewOutput{RenderPlanID: "plan-1", Jobs: []NativeAudioReviewJob{
			{ReviewID: "review-1", RenderSegmentID: "segment-1"},
			{ReviewID: "review-2", RenderSegmentID: "segment-2"},
		}}, nil
	}, activity.RegisterOptions{Name: "PrepareNativeAudioReview"})
	env.RegisterActivityWithOptions(func(_ context.Context, request ReviewNativeAudioSegmentInput) (ReviewNativeAudioSegmentOutput, error) {
		if request.ReviewID == "review-1" {
			return ReviewNativeAudioSegmentOutput{ReviewID: request.ReviewID, RenderSegmentID: "segment-1", Status: "passed", DialogueCoverage: 1, TextAccuracy: 1, TimingAccuracy: 1, SpeakerTurnAccuracy: 1}, nil
		}
		return ReviewNativeAudioSegmentOutput{ReviewID: request.ReviewID, RenderSegmentID: "segment-2", Status: "failed", ErrorCode: "DIALOGUE_MISMATCH", ErrorMessage: "对白缺失"}, nil
	}, activity.RegisterOptions{Name: "ReviewNativeAudioSegment"})
	env.RegisterActivityWithOptions(func(context.Context, RefreshTimingCalibrationProfileInput) error { return nil }, activity.RegisterOptions{Name: "RefreshTimingCalibrationProfile"})
	var completed NativeAudioReviewWorkflowOutput
	env.RegisterActivityWithOptions(func(_ context.Context, _ NativeAudioReviewWorkflowInput, output NativeAudioReviewWorkflowOutput) error {
		completed = output
		return nil
	}, activity.RegisterOptions{Name: "CompleteNativeAudioReviewWorkflow"})

	env.ExecuteWorkflow(NativeAudioReviewWorkflow, input)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var output NativeAudioReviewWorkflowOutput
	if err := env.GetWorkflowResult(&output); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if output.Status != "partial_succeeded" || len(output.PassedSegmentIDs) != 1 || output.PassedSegmentIDs[0] != "segment-1" || len(output.FailedSegmentIDs) != 1 || output.FailedSegmentIDs[0] != "segment-2" {
		t.Fatalf("output = %+v", output)
	}
	if completed.Status != "partial_succeeded" || completed.Errors["segment-2"] != "对白缺失" {
		t.Fatalf("completed output = %+v", completed)
	}
}
