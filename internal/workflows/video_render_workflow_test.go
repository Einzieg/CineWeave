package workflows

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Einzieg/cineweave/internal/provider"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestApplyCrossShotContinuityReferenceReplacesFirstFrame(t *testing.T) {
	context := ShotVideoReferenceContext{
		ReferenceMode: "custom",
		References: []provider.GatewayVideoReference{
			{Type: "first_frame", ArtifactID: "shot-image"},
			{Type: "image", ArtifactID: "asset-image"},
		},
		ConfiguredReferenceKeys: []string{"shot_image:shot-2", "asset:character-1"},
		ResolvedReferenceKeys:   []string{"shot_image:shot-2", "asset:character-1"},
	}
	result, err := applyCrossShotContinuityReference(context, ShotContinuityFrameReference{
		SourceShotID: "shot-1", SourceVideoArtifactID: "video-1", ArtifactID: "tail-1", MediaFileID: "tail-media-1", StorageKey: "tail/1.png",
	})
	if err != nil {
		t.Fatalf("applyCrossShotContinuityReference: %v", err)
	}
	if result.ReferenceMode != "custom" || len(result.References) != 2 || result.References[0].Type != "first_frame" || result.References[0].ArtifactID != "tail-1" || result.References[1].ArtifactID != "asset-image" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.ResolvedReferenceKeys) != 2 || result.ResolvedReferenceKeys[0] != "continuity_tail:shot-1" || result.ResolvedReferenceKeys[1] != "asset:character-1" {
		t.Fatalf("resolved keys = %+v", result.ResolvedReferenceKeys)
	}
	var metadata map[string]any
	if err := json.Unmarshal(result.References[0].Metadata, &metadata); err != nil || metadata["sourceType"] != "continuity_tail_frame" || metadata["sourceId"] != "shot-1" {
		t.Fatalf("metadata = %+v err=%v", metadata, err)
	}
}

func registerRenderSegmentMediaTestActivity(env *testsuite.TestWorkflowEnvironment) {
	env.RegisterActivityWithOptions(func(_ context.Context, input ProcessRenderSegmentMediaInput) (ProcessRenderSegmentMediaOutput, error) {
		return ProcessRenderSegmentMediaOutput{
			ExecutionPlanID:           input.ExecutionPlanID,
			RenderSegmentID:           input.RenderSegmentID,
			RawArtifactID:             input.RawArtifactID,
			RawMediaFileID:            input.RawMediaFileID,
			RawStorageKey:             input.RawStorageKey,
			MezzanineArtifactID:       "mezzanine-" + input.RenderSegmentID,
			MezzanineMediaFileID:      "mezzanine-media-" + input.RenderSegmentID,
			MezzanineStorageKey:       "mezzanine/" + input.RenderSegmentID + ".mp4",
			ExtractedAudioArtifactID:  "audio-" + input.RenderSegmentID,
			ExtractedAudioMediaFileID: "audio-media-" + input.RenderSegmentID,
			ExtractedAudioStorageKey:  "audio/" + input.RenderSegmentID + ".m4a",
		}, nil
	}, activity.RegisterOptions{Name: "ProcessRenderSegmentMedia"})
	env.RegisterActivityWithOptions(func(_ context.Context, input ComposeShotRenderPlanMediaInput) (ComposeShotRenderPlanMediaOutput, error) {
		return ComposeShotRenderPlanMediaOutput{
			ExecutionPlanID: input.ExecutionPlanID, ShotID: input.ShotID,
			ArtifactID: "shot-video-" + input.ShotID, MediaFileID: "shot-video-media-" + input.ShotID,
			StorageKey: "shot-video/" + input.ShotID + ".mp4", MimeType: "video/mp4",
			NativeAudioStatus: "audio_unverified", ProductionReadiness: "preview_only",
		}, nil
	}, activity.RegisterOptions{Name: "ComposeShotRenderPlanMedia"})
}

func registerShotVideoExecutionGroupsTestActivity(env *testsuite.TestWorkflowEnvironment) {
	env.RegisterWorkflow(BatchGenerateShotVideosWorkflow)
	env.RegisterWorkflow(ShotVideoContinuityGroupWorkflow)
	env.RegisterActivityWithOptions(func(_ context.Context, input PrepareShotVideoExecutionGroupsInput) ([]ShotVideoExecutionGroup, error) {
		groups := make([]ShotVideoExecutionGroup, 0, len(input.ShotIDs))
		for index, shotID := range input.ShotIDs {
			groups = append(groups, ShotVideoExecutionGroup{
				GroupKey: "shot-" + shotID,
				Shots:    []ShotVideoExecutionShot{{ShotID: shotID, ShotIndex: index, ShotNo: index + 1}},
			})
		}
		return groups, nil
	}, activity.RegisterOptions{Name: "PrepareShotVideoExecutionGroups"})
}
