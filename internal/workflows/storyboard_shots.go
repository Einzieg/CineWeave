package workflows

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	defaultMaxStoryboardShots = 3
	defaultShotDuration       = 5.0
	maxShotDuration           = 15.0
)

type StoryboardShot struct {
	ShotNo      int     `json:"shotNo"`
	Duration    float64 `json:"duration"`
	Visual      string  `json:"visual"`
	Camera      string  `json:"camera"`
	Motion      string  `json:"motion"`
	Mood        string  `json:"mood"`
	ImagePrompt string  `json:"imagePrompt"`
	VideoPrompt string  `json:"videoPrompt"`
	Title       string  `json:"title,omitempty"`
}

type StoryboardShotRecord struct {
	ID                       string  `json:"shotId"`
	WorkflowRunID            string  `json:"workflowRunId,omitempty"`
	ShotIndex                int     `json:"shotIndex"`
	ShotNo                   int     `json:"shotNo"`
	Title                    string  `json:"title,omitempty"`
	Duration                 float64 `json:"duration"`
	Visual                   string  `json:"visual,omitempty"`
	Camera                   string  `json:"camera,omitempty"`
	Motion                   string  `json:"motion,omitempty"`
	Mood                     string  `json:"mood,omitempty"`
	ImagePrompt              string  `json:"imagePrompt,omitempty"`
	VideoPrompt              string  `json:"videoPrompt,omitempty"`
	ImageArtifactID          string  `json:"imageArtifactId,omitempty"`
	ImageMediaFileID         string  `json:"imageMediaFileId,omitempty"`
	ImageStorageKey          string  `json:"imageStorageKey,omitempty"`
	VideoArtifactID          string  `json:"videoArtifactId,omitempty"`
	VideoMediaFileID         string  `json:"videoMediaFileId,omitempty"`
	VideoStorageKey          string  `json:"videoStorageKey,omitempty"`
	VideoProviderAsyncTaskID string  `json:"providerAsyncTaskId,omitempty"`
	VideoExternalTaskID      string  `json:"externalTaskId,omitempty"`
	Status                   string  `json:"status"`
	ManualOverride           bool    `json:"manualOverride,omitempty"`
	StaleState               string  `json:"staleState,omitempty"`
}

func ParseStoryboardShots(raw json.RawMessage) ([]StoryboardShot, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" {
		return nil, nil
	}
	var decoded struct {
		Shots []StoryboardShot `json:"shots"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	return decoded.Shots, nil
}

func NormalizeStoryboardShots(shots []StoryboardShot, fallbackPrompt string) []StoryboardShot {
	return NormalizeStoryboardShotsWithLimit(shots, fallbackPrompt, defaultMaxStoryboardShots)
}

func NormalizeStoryboardShotsWithLimit(shots []StoryboardShot, fallbackPrompt string, maxShots int) []StoryboardShot {
	if maxShots <= 0 || maxShots > defaultMaxStoryboardShots {
		maxShots = defaultMaxStoryboardShots
	}
	if len(shots) == 0 {
		shots = []StoryboardShot{{
			ShotNo:      1,
			Duration:    defaultShotDuration,
			Visual:      strings.TrimSpace(fallbackPrompt),
			ImagePrompt: strings.TrimSpace(fallbackPrompt),
			VideoPrompt: strings.TrimSpace(fallbackPrompt),
		}}
	}
	if len(shots) > maxShots {
		shots = shots[:maxShots]
	}
	out := make([]StoryboardShot, 0, len(shots))
	for i, shot := range shots {
		shot.Title = strings.TrimSpace(shot.Title)
		shot.Visual = strings.TrimSpace(shot.Visual)
		shot.Camera = strings.TrimSpace(shot.Camera)
		shot.Motion = strings.TrimSpace(shot.Motion)
		shot.Mood = strings.TrimSpace(shot.Mood)
		shot.ImagePrompt = strings.TrimSpace(shot.ImagePrompt)
		shot.VideoPrompt = strings.TrimSpace(shot.VideoPrompt)
		if shot.ShotNo <= 0 {
			shot.ShotNo = i + 1
		}
		if shot.Duration <= 0 {
			shot.Duration = defaultShotDuration
		}
		if shot.Duration > maxShotDuration {
			shot.Duration = maxShotDuration
		}
		if shot.Visual == "" {
			shot.Visual = strings.TrimSpace(fallbackPrompt)
		}
		if shot.ImagePrompt == "" {
			shot.ImagePrompt = firstNonEmptyString(shot.Visual, fallbackPrompt)
		}
		if shot.VideoPrompt == "" {
			shot.VideoPrompt = buildFallbackVideoPrompt(shot, fallbackPrompt)
		}
		out = append(out, shot)
	}
	if len(out) == 0 {
		return NormalizeStoryboardShots(nil, fallbackPrompt)
	}
	return out
}

func resolveWorkflowMaxShots(raw json.RawMessage) int {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" {
		return defaultMaxStoryboardShots
	}
	var decoded struct {
		MaxShots int `json:"maxShots"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return defaultMaxStoryboardShots
	}
	if decoded.MaxShots <= 0 || decoded.MaxShots > defaultMaxStoryboardShots {
		return defaultMaxStoryboardShots
	}
	return decoded.MaxShots
}

func buildFallbackVideoPrompt(shot StoryboardShot, fallbackPrompt string) string {
	parts := []string{}
	if value := strings.TrimSpace(shot.Visual); value != "" {
		parts = append(parts, value)
	} else if value := strings.TrimSpace(fallbackPrompt); value != "" {
		parts = append(parts, value)
	}
	if value := strings.TrimSpace(shot.Camera); value != "" {
		parts = append(parts, "Camera: "+value)
	}
	if value := strings.TrimSpace(shot.Motion); value != "" {
		parts = append(parts, "Motion: "+value)
	}
	if value := strings.TrimSpace(shot.Mood); value != "" {
		parts = append(parts, "Mood: "+value)
	}
	if len(parts) == 0 {
		return "A cinematic scene based on the reference image"
	}
	return strings.Join(parts, ". ")
}

func storyboardShotRecordFromShot(shot StoryboardShot, shotID, workflowRunID string, shotIndex int) StoryboardShotRecord {
	return StoryboardShotRecord{
		ID:            shotID,
		WorkflowRunID: workflowRunID,
		ShotIndex:     shotIndex,
		ShotNo:        shot.ShotNo,
		Title:         shot.Title,
		Duration:      shot.Duration,
		Visual:        shot.Visual,
		Camera:        shot.Camera,
		Motion:        shot.Motion,
		Mood:          shot.Mood,
		ImagePrompt:   shot.ImagePrompt,
		VideoPrompt:   shot.VideoPrompt,
		Status:        "storyboard_ready",
	}
}

func storyboardShotEventPayload(workflowRunID string, shot StoryboardShotRecord, status string) json.RawMessage {
	if status == "" {
		status = shot.Status
	}
	return mustJSON(map[string]any{
		"workflowRunId": workflowRunID,
		"shotId":        shot.ID,
		"shotIndex":     shot.ShotIndex,
		"shotNo":        shot.ShotNo,
		"status":        status,
	})
}

func nodeKeyForShot(prefix string, shotIndex int) string {
	return fmt.Sprintf("%s_%d", prefix, shotIndex)
}
