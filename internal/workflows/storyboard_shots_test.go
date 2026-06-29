package workflows

import (
	"encoding/json"
	"testing"
)

func TestParseStoryboardShots(t *testing.T) {
	raw := json.RawMessage(`{"shots":[{"shotNo":1,"duration":4,"visual":"wide station"},{"shotNo":2,"visual":"close-up"}]}`)
	shots, err := ParseStoryboardShots(raw)
	if err != nil {
		t.Fatalf("ParseStoryboardShots: %v", err)
	}
	if len(shots) != 2 || shots[0].Visual != "wide station" || shots[1].Visual != "close-up" {
		t.Fatalf("shots = %+v", shots)
	}
}

func TestNormalizeStoryboardShotsDefaults(t *testing.T) {
	shots := NormalizeStoryboardShots([]StoryboardShot{{Visual: "mist", Camera: "push", Motion: "drifting", Mood: "calm"}}, "fallback")
	if len(shots) != 1 {
		t.Fatalf("len = %d", len(shots))
	}
	shot := shots[0]
	if shot.ShotNo != 1 || shot.Duration != defaultShotDuration || shot.ImagePrompt != "mist" {
		t.Fatalf("shot defaults = %+v", shot)
	}
	if !containsAll(shot.VideoPrompt, []string{"mist", "Camera: push", "Motion: drifting", "Mood: calm"}) {
		t.Fatalf("video prompt = %q", shot.VideoPrompt)
	}
}

func TestNormalizeStoryboardShotsInvalidJSONFallback(t *testing.T) {
	parsed, err := ParseStoryboardShots(json.RawMessage(`not json`))
	if err == nil {
		t.Fatal("ParseStoryboardShots error is nil")
	}
	shots := NormalizeStoryboardShots(parsed, "fallback scene")
	if len(shots) != 1 || shots[0].Visual != "fallback scene" || shots[0].Duration != defaultShotDuration {
		t.Fatalf("fallback shots = %+v", shots)
	}
}

func TestNormalizeStoryboardShotsMaxThree(t *testing.T) {
	shots := NormalizeStoryboardShots([]StoryboardShot{
		{Visual: "1"},
		{Visual: "2"},
		{Visual: "3"},
		{Visual: "4"},
	}, "fallback")
	if len(shots) != 3 {
		t.Fatalf("len = %d, want 3", len(shots))
	}
}

func TestNormalizeStoryboardShotsPromptFallbacks(t *testing.T) {
	shots := NormalizeStoryboardShots([]StoryboardShot{{Duration: 20}}, "fallback scene")
	shot := shots[0]
	if shot.Duration != maxShotDuration || shot.Visual != "fallback scene" || shot.ImagePrompt != "fallback scene" || shot.VideoPrompt != "fallback scene" {
		t.Fatalf("fallbacks = %+v", shot)
	}
}
