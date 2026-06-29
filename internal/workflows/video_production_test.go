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
	if defaults.Duration != 5 || defaults.AspectRatio != "16:9" || defaults.Resolution != "720p" || defaults.MaxPolls != 120 || defaults.PollInterval.Seconds() != 5 {
		t.Fatalf("defaults = %+v", defaults)
	}

	overrides := resolveVideoProductionOptions(json.RawMessage(`{"duration":8,"aspectRatio":"9:16","resolution":"1080p","pollIntervalSeconds":2,"maxPolls":3}`))
	if overrides.Duration != 8 || overrides.AspectRatio != "9:16" || overrides.Resolution != "1080p" || overrides.MaxPolls != 3 || overrides.PollInterval.Seconds() != 2 {
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
	if output.ProviderCalls["storyboard"] != "text-call" || output.ProviderCalls["image"] != "image-call" || output.ProviderCalls["videoCreate"] != "video-create" || output.ProviderCalls["videoPoll"] != "video-poll" {
		t.Fatalf("provider calls = %+v", output.ProviderCalls)
	}
}

func TestVideoProductionWorkflowPollsUntilSucceeded(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var createCalls int
	var pollCalls int
	var completed VideoProductionOutput

	env.RegisterActivityWithOptions(func(ctx context.Context, input GenerateStoryboardTextInput) (GenerateStoryboardTextOutput, error) {
		return GenerateStoryboardTextOutput{
			StoryboardArtifactID: "storyboard-artifact",
			ProviderCallID:       "text-call",
			Storyboard:           json.RawMessage(`{"shots":[{"imagePrompt":"station image","videoPrompt":"station video","camera":"push","motion":"mist","mood":"warm"}]}`),
		}, nil
	}, activity.RegisterOptions{Name: "GenerateStoryboardText"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input GenerateStoryboardImageInput) (GenerateStoryboardImageOutput, error) {
		return GenerateStoryboardImageOutput{
			ImageArtifactID:  "image-artifact",
			ImageMediaFileID: "image-media",
			ImageStorageKey:  "image-key",
			ProviderCallID:   "image-call",
		}, nil
	}, activity.RegisterOptions{Name: "GenerateStoryboardImage"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input CreateStoryboardVideoTaskInput) (CreateStoryboardVideoTaskOutput, error) {
		createCalls++
		if input.VideoPrompt == "" || input.Duration != 5 || input.AspectRatio != "16:9" || input.Resolution != "720p" {
			t.Fatalf("create input = %+v", input)
		}
		return CreateStoryboardVideoTaskOutput{
			NodeRunID:           "video-node",
			ProviderCallID:      "video-create-call",
			ProviderAsyncTaskID: "async-task",
			ExternalTaskID:      "external-task",
			Status:              "running",
			ModelID:             "video-model",
		}, nil
	}, activity.RegisterOptions{Name: "CreateStoryboardVideoTask"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input PollStoryboardVideoTaskInput) (PollStoryboardVideoTaskOutput, error) {
		pollCalls++
		if input.ProviderAsyncTaskID != "async-task" || input.PollCount != pollCalls {
			t.Fatalf("poll input = %+v", input)
		}
		if pollCalls == 1 {
			return PollStoryboardVideoTaskOutput{ProviderCallID: "video-poll-1", ProviderAsyncTaskID: "async-task", ExternalTaskID: "external-task", Status: "running", PollCount: pollCalls}, nil
		}
		return PollStoryboardVideoTaskOutput{
			ProviderCallID:      "video-poll-2",
			ProviderAsyncTaskID: "async-task",
			ExternalTaskID:      "external-task",
			Status:              "succeeded",
			ArtifactID:          "video-artifact",
			MediaFileID:         "video-media",
			StorageKey:          "video-key",
			MimeType:            "video/mp4",
			PollCount:           pollCalls,
		}, nil
	}, activity.RegisterOptions{Name: "PollStoryboardVideoTask"})
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
	if createCalls != 1 || pollCalls != 2 {
		t.Fatalf("createCalls=%d pollCalls=%d", createCalls, pollCalls)
	}
	if completed.VideoArtifactID != "video-artifact" || completed.VideoStorageKey != "video-key" || completed.ProviderCalls["videoPoll"] != "video-poll-2" {
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
