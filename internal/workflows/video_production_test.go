package workflows

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestSelectVideoPromptPriority(t *testing.T) {
	storyboard := json.RawMessage(`{"shots":[{"videoPrompt":"direct video","imagePrompt":"image prompt","visual":"visual","camera":"pan","motion":"rain","mood":"noir"}]}`)
	if got := selectVideoPrompt(storyboard, "fallback", 5); !containsAll(got, []string{"direct video", "Camera: pan.", "Motion: rain.", "Mood: noir.", "Do not add subtitles"}) {
		t.Fatalf("video prompt = %q", got)
	}

	storyboard = json.RawMessage(`{"shots":[{"imagePrompt":"image prompt","camera":"tilt","motion":"mist","mood":"calm"}]}`)
	if got := selectVideoPrompt(storyboard, "fallback", 5); !containsAll(got, []string{"image prompt", "Camera: tilt.", "Motion: mist.", "Mood: calm."}) {
		t.Fatalf("image prompt fallback = %q", got)
	}

	storyboard = json.RawMessage(`{"shots":[{"visual":"visual only"}]}`)
	if got := selectVideoPrompt(storyboard, "fallback", 5); !containsAll(got, []string{"visual only", "slow push-in", "subtle atmospheric movement"}) {
		t.Fatalf("visual fallback = %q", got)
	}

	if got := selectVideoPrompt(json.RawMessage(`{"shots":[]}`), "workflow prompt", 5); !containsAll(got, []string{"workflow prompt", "5-second"}) {
		t.Fatalf("workflow fallback = %q", got)
	}
}

func TestResolveVideoProductionOptionsDefaultsAndOverrides(t *testing.T) {
	defaults := resolveVideoProductionOptions(nil)
	if defaults.Duration != 5 || defaults.AspectRatio != "16:9" || defaults.Resolution != "720p" || defaults.MaxPolls != 120 || defaults.MaxShots != 3 || defaults.SkipCompose || defaults.PollInterval.Seconds() != 5 {
		t.Fatalf("defaults = %+v", defaults)
	}

	overrides := resolveVideoProductionOptions(json.RawMessage(`{"duration":8,"aspectRatio":"9:16","resolution":"1080p","pollIntervalSeconds":2,"maxPolls":3,"maxShots":2,"skipCompose":true}`))
	if overrides.Duration != 8 || overrides.AspectRatio != "9:16" || overrides.Resolution != "1080p" || overrides.MaxPolls != 3 || overrides.MaxShots != 2 || !overrides.SkipCompose || overrides.PollInterval.Seconds() != 2 {
		t.Fatalf("overrides = %+v", overrides)
	}
}

func TestBuildVideoProductionOutput(t *testing.T) {
	output := BuildVideoProductionOutput(
		GenerateStoryboardTextOutput{StoryboardArtifactID: "storyboard", ProviderCallID: "text-call"},
		GenerateStoryboardImageOutput{ImageArtifactID: "image", ImageMediaFileID: "image-media", ImageStorageKey: "image-key", ProviderCallID: "image-call"},
		CreateStoryboardVideoTaskOutput{ProviderCallID: "video-create", ProviderAsyncTaskID: "async", ExternalTaskID: "external-create"},
		PollStoryboardVideoTaskOutput{ProviderCallID: "video-poll", ArtifactID: "video", MediaFileID: "video-media", StorageKey: "video-key", ExternalTaskID: "external-poll"},
	)
	if output.VideoArtifactID != "video" || output.VideoMediaFileID != "video-media" || output.VideoStorageKey != "video-key" || output.ProviderAsyncTaskID != "async" || output.ExternalTaskID != "external-poll" {
		t.Fatalf("output = %+v", output)
	}
	if output.ProviderCalls.Storyboard != "text-call" || output.ProviderCalls.Image != "image-call" || output.ProviderCalls.VideoCreate != "video-create" || output.ProviderCalls.VideoPoll != "video-poll" {
		t.Fatalf("provider calls = %+v", output.ProviderCalls)
	}
}

func TestVideoProductionWorkflowPollsUntilSucceeded(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var createCalls int
	var pollCalls int
	var composeCalls int
	var completed VideoProductionOutput
	shots := []StoryboardShotRecord{
		{ID: "shot-1", WorkflowRunID: "workflow", ShotIndex: 0, ShotNo: 1, Duration: 5, Visual: "wide station", ImagePrompt: "station image 1", VideoPrompt: "station video 1", Status: "storyboard_ready"},
		{ID: "shot-2", WorkflowRunID: "workflow", ShotIndex: 1, ShotNo: 2, Duration: 4, Visual: "close-up train", ImagePrompt: "station image 2", VideoPrompt: "station video 2", Status: "storyboard_ready"},
	}
	pollByTask := map[string]int{}

	env.RegisterActivityWithOptions(func(ctx context.Context, input GenerateStoryboardTextInput) (GenerateStoryboardTextOutput, error) {
		return GenerateStoryboardTextOutput{
			StoryboardArtifactID: "storyboard-artifact",
			ProviderCallID:       "text-call",
			Storyboard:           json.RawMessage(`{"shots":[{"imagePrompt":"station image","videoPrompt":"station video","camera":"push","motion":"mist","mood":"warm"}]}`),
			Shots:                shots,
		}, nil
	}, activity.RegisterOptions{Name: "GenerateStoryboardText"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input ListStoryboardShotsInput) ([]StoryboardShotRecord, error) {
		return shots, nil
	}, activity.RegisterOptions{Name: "ListStoryboardShots"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input GenerateShotImageInput) (GenerateShotImageOutput, error) {
		if input.ShotID == "" || input.WorkflowPrompt == "" {
			t.Fatalf("image input = %+v", input)
		}
		return GenerateShotImageOutput{
			NodeRunID:        "image-node-" + input.ShotID,
			ShotID:           input.ShotID,
			ImageArtifactID:  "image-artifact-" + input.ShotID,
			ImageMediaFileID: "image-media-" + input.ShotID,
			ImageStorageKey:  "image-key-" + input.ShotID,
			ProviderCallID:   "image-call-" + input.ShotID,
		}, nil
	}, activity.RegisterOptions{Name: "GenerateShotImage"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input CreateShotVideoTaskInput) (CreateShotVideoTaskOutput, error) {
		createCalls++
		if input.ShotID == "" || input.Duration <= 0 || input.AspectRatio != "16:9" || input.Resolution != "720p" {
			t.Fatalf("create input = %+v", input)
		}
		return CreateShotVideoTaskOutput{
			NodeRunID:           "video-node-" + input.ShotID,
			ShotID:              input.ShotID,
			ProviderCallID:      "video-create-call-" + input.ShotID,
			ProviderAsyncTaskID: "async-task-" + input.ShotID,
			ExternalTaskID:      "external-task-" + input.ShotID,
			Status:              "running",
			ModelID:             "video-model",
		}, nil
	}, activity.RegisterOptions{Name: "CreateShotVideoTask"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input PollShotVideoTaskInput) (PollShotVideoTaskOutput, error) {
		pollCalls++
		pollByTask[input.ProviderAsyncTaskID]++
		perTaskPoll := pollByTask[input.ProviderAsyncTaskID]
		if input.ProviderAsyncTaskID == "" || input.PollCount != perTaskPoll {
			t.Fatalf("poll input = %+v", input)
		}
		if perTaskPoll == 1 {
			return PollShotVideoTaskOutput{ProviderCallID: "video-poll-1-" + input.ShotID, ProviderAsyncTaskID: input.ProviderAsyncTaskID, ExternalTaskID: input.ExternalTaskID, Status: "running", PollCount: perTaskPoll}, nil
		}
		return PollShotVideoTaskOutput{
			ProviderCallID:      "video-poll-2-" + input.ShotID,
			ProviderAsyncTaskID: input.ProviderAsyncTaskID,
			ExternalTaskID:      input.ExternalTaskID,
			Status:              "succeeded",
			ArtifactID:          "video-artifact-" + input.ShotID,
			MediaFileID:         "video-media-" + input.ShotID,
			StorageKey:          "video-key-" + input.ShotID,
			MimeType:            "video/mp4",
			PollCount:           perTaskPoll,
		}, nil
	}, activity.RegisterOptions{Name: "PollShotVideoTask"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input ComposeFinalVideoInput) (ComposeFinalVideoOutput, error) {
		composeCalls++
		if input.WorkflowRunID != "workflow" || input.AspectRatio != "16:9" || input.Resolution != "720p" {
			t.Fatalf("compose input = %+v", input)
		}
		return ComposeFinalVideoOutput{
			NodeRunID:          "compose-node",
			ArtifactID:         "final-video-artifact",
			MediaFileID:        "final-video-media",
			StorageKey:         "final-video-key.mp4",
			MimeType:           "video/mp4",
			TimelineArtifactID: "timeline-artifact",
		}, nil
	}, activity.RegisterOptions{Name: "ComposeFinalVideo"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input TextToStoryboardInput, output VideoProductionOutput) error {
		completed = output
		return nil
	}, activity.RegisterOptions{Name: "CompleteVideoProductionWorkflow"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input TextToStoryboardInput, nodeRunID, code, message string) error {
		t.Fatalf("unexpected failure marker: %s %s %s", nodeRunID, code, message)
		return nil
	}, activity.RegisterOptions{Name: "FailVideoProductionWorkflow"})

	env.ExecuteWorkflow(VideoProductionWorkflow, TextToStoryboardInput{
		OrganizationID: "org",
		ProjectID:      "project",
		WorkflowRunID:  "workflow",
		Prompt:         "train station",
		CreatedBy:      "user",
		Input:          json.RawMessage(`{"maxPolls":3,"pollIntervalSeconds":1}`),
	})

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v error=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	if createCalls != 2 || pollCalls != 4 {
		t.Fatalf("createCalls=%d pollCalls=%d", createCalls, pollCalls)
	}
	if composeCalls != 1 {
		t.Fatalf("composeCalls=%d, want 1", composeCalls)
	}
	if len(completed.Shots) != 2 || completed.Shots[1].VideoArtifactID != "video-artifact-shot-2" || len(completed.ProviderCalls.VideoPolls) != 4 || completed.FinalVideoArtifactID != "final-video-artifact" || completed.FinalVideoMediaFileID != "final-video-media" || completed.FinalVideoStorageKey != "final-video-key.mp4" || completed.TimelineArtifactID != "timeline-artifact" {
		t.Fatalf("completed output = %+v", completed)
	}
}

func containsAll(value string, needles []string) bool {
	for _, needle := range needles {
		if !strings.Contains(value, needle) {
			return false
		}
	}
	return true
}
