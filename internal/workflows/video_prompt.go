package workflows

import (
	"encoding/json"
	"fmt"
	"strings"
)

func selectVideoPrompt(storyboard json.RawMessage, fallback string, duration float64) string {
	shot := firstStoryboardShot(storyboard)
	scene := strings.TrimSpace(shot.VideoPrompt)
	if scene == "" {
		scene = strings.TrimSpace(shot.ImagePrompt)
	}
	if scene == "" {
		scene = strings.TrimSpace(shot.Visual)
	}
	if scene == "" {
		scene = strings.TrimSpace(fallback)
	}
	if scene == "" {
		scene = "A cinematic scene based on the reference image"
	}

	camera := strings.TrimSpace(shot.Camera)
	if camera == "" {
		camera = "slow push-in"
	}
	motion := strings.TrimSpace(shot.Motion)
	if motion == "" {
		motion = "subtle atmospheric movement"
	}
	mood := strings.TrimSpace(shot.Mood)
	if mood == "" {
		mood = "cinematic and coherent"
	}

	return strings.Join([]string{
		fmt.Sprintf("Create a %.0f-second cinematic video based on the reference image.", duration),
		"Keep the same character, scene layout, lighting, art style, and composition.",
		"Scene: " + scene + ".",
		"Camera: " + camera + ".",
		"Motion: " + motion + ".",
		"Mood: " + mood + ".",
		"Do not add subtitles, captions, title cards, watermarks, split-screen, collage, or extra characters.",
	}, "\n")
}

func firstStoryboardShot(storyboard json.RawMessage) storyboardShotForVideo {
	var decoded struct {
		Shots []storyboardShotForVideo `json:"shots"`
	}
	if err := json.Unmarshal(storyboard, &decoded); err != nil || len(decoded.Shots) == 0 {
		return storyboardShotForVideo{}
	}
	return decoded.Shots[0]
}

type storyboardShotForVideo struct {
	VideoPrompt string `json:"videoPrompt"`
	ImagePrompt string `json:"imagePrompt"`
	Visual      string `json:"visual"`
	Camera      string `json:"camera"`
	Motion      string `json:"motion"`
	Mood        string `json:"mood"`
}
