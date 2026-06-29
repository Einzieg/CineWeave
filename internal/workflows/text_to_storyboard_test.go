package workflows

import (
	"encoding/json"
	"testing"
)

func TestParseStoryboardTextJSONSuccess(t *testing.T) {
	storyboard, parseError := parseStoryboardText(`{"title":"Train","shots":[{"shotNo":1,"imagePrompt":"wide station"}]}`)
	if parseError != "" {
		t.Fatalf("parseError = %q, want empty", parseError)
	}
	var decoded struct {
		Title string `json:"title"`
		Shots []struct {
			ImagePrompt string `json:"imagePrompt"`
		} `json:"shots"`
	}
	if err := json.Unmarshal(storyboard, &decoded); err != nil {
		t.Fatalf("storyboard JSON invalid: %v", err)
	}
	if decoded.Title != "Train" || len(decoded.Shots) != 1 || decoded.Shots[0].ImagePrompt != "wide station" {
		t.Fatalf("decoded storyboard = %+v", decoded)
	}
}

func TestParseStoryboardTextFailureKeepsRawText(t *testing.T) {
	storyboard, parseError := parseStoryboardText(`not json`)
	if parseError == "" {
		t.Fatal("parseError is empty, want parse failure")
	}
	var decoded map[string]string
	if err := json.Unmarshal(storyboard, &decoded); err != nil {
		t.Fatalf("storyboard JSON invalid: %v", err)
	}
	if decoded["rawText"] != "not json" {
		t.Fatalf("rawText = %q, want original text", decoded["rawText"])
	}
}

func TestSelectImagePromptPriority(t *testing.T) {
	withImagePrompt := json.RawMessage(`{"shots":[{"imagePrompt":"first image","visual":"first visual"}]}`)
	if got := selectImagePrompt(withImagePrompt, "fallback"); got != "first image" {
		t.Fatalf("image prompt = %q, want first image", got)
	}
	withVisual := json.RawMessage(`{"shots":[{"visual":"first visual"}]}`)
	if got := selectImagePrompt(withVisual, "fallback"); got != "first visual" {
		t.Fatalf("image prompt = %q, want first visual", got)
	}
	if got := selectImagePrompt(json.RawMessage(`{"shots":[]}`), "fallback"); got != "fallback" {
		t.Fatalf("image prompt = %q, want fallback", got)
	}
}

func TestBuildTextToStoryboardOutput(t *testing.T) {
	output := BuildTextToStoryboardOutput(
		GenerateStoryboardTextOutput{
			StoryboardArtifactID: "storyboard-artifact",
			ProviderCallID:       "text-call",
		},
		GenerateStoryboardImageOutput{
			ImageArtifactID:  "image-artifact",
			ImageMediaFileID: "media-file",
			ImageStorageKey:  "storage-key",
			ProviderCallID:   "image-call",
		},
	)
	if output.StoryboardArtifactID != "storyboard-artifact" || output.ImageArtifactID != "image-artifact" || output.ImageMediaFileID != "media-file" || output.ImageStorageKey != "storage-key" {
		t.Fatalf("output = %+v", output)
	}
	if output.ProviderCalls["storyboard"] != "text-call" || output.ProviderCalls["image"] != "image-call" {
		t.Fatalf("provider calls = %+v", output.ProviderCalls)
	}
}
