package workflows

import (
	"encoding/json"
	"fmt"
	"strings"

	storyboardtiming "github.com/Einzieg/cineweave/internal/storyboard"
)

const (
	plannerBatchMaxShots = 12
	defaultShotDuration  = 5.0
)

type StoryboardShot struct {
	ShotNo         int                      `json:"shotNo"`
	Duration       float64                  `json:"duration"`
	Visual         string                   `json:"visual"`
	Camera         string                   `json:"camera"`
	Motion         string                   `json:"motion"`
	Mood           string                   `json:"mood"`
	Dialogue       []StoryboardDialogueLine `json:"dialogue,omitempty"`
	ImagePrompt    string                   `json:"imagePrompt"`
	VideoPrompt    string                   `json:"videoPrompt"`
	Title          string                   `json:"title,omitempty"`
	ScriptSceneID  string                   `json:"scriptSceneId,omitempty"`
	SceneNo        int                      `json:"sceneNo,omitempty"`
	SourceSceneNo  int                      `json:"sourceSceneNo,omitempty"`
	StartTick      int64                    `json:"startTick,omitempty"`
	EndTick        int64                    `json:"endTick,omitempty"`
	DurationTicks  int64                    `json:"durationTicks,omitempty"`
	DurationSource string                   `json:"durationSource,omitempty"`
}

type StoryboardShotRecord struct {
	ID                       string                   `json:"shotId"`
	WorkflowRunID            string                   `json:"workflowRunId,omitempty"`
	ScriptSceneID            string                   `json:"scriptSceneId,omitempty"`
	ScriptEpisodeID          string                   `json:"scriptEpisodeId,omitempty"`
	EpisodeIndex             int                      `json:"episodeIndex,omitempty"`
	EpisodeShotIndex         int                      `json:"episodeShotIndex,omitempty"`
	ShotIndex                int                      `json:"shotIndex"`
	ShotNo                   int                      `json:"shotNo"`
	Title                    string                   `json:"title,omitempty"`
	Duration                 float64                  `json:"duration"`
	StoryboardPlanID         string                   `json:"storyboardPlanId,omitempty"`
	StartTick                int64                    `json:"startTick"`
	EndTick                  int64                    `json:"endTick"`
	PlannedDurationTicks     int64                    `json:"plannedDurationTicks"`
	TimelineTimebase         int64                    `json:"timelineTimebase"`
	FPSNumerator             int                      `json:"fpsNumerator"`
	FPSDenominator           int                      `json:"fpsDenominator"`
	DurationSource           string                   `json:"durationSource,omitempty"`
	TimingConfidence         float64                  `json:"timingConfidence,omitempty"`
	DurationLocked           bool                     `json:"durationLocked,omitempty"`
	OneTake                  bool                     `json:"oneTake,omitempty"`
	TimingRevision           int                      `json:"timingRevision,omitempty"`
	Visual                   string                   `json:"visual,omitempty"`
	Camera                   string                   `json:"camera,omitempty"`
	Motion                   string                   `json:"motion,omitempty"`
	Mood                     string                   `json:"mood,omitempty"`
	Dialogue                 []StoryboardDialogueLine `json:"dialogue,omitempty"`
	ImagePrompt              string                   `json:"imagePrompt,omitempty"`
	ImagePromptStatus        string                   `json:"imagePromptStatus,omitempty"`
	ImagePromptErrorCode     string                   `json:"imagePromptErrorCode,omitempty"`
	ImagePromptErrorMessage  string                   `json:"imagePromptErrorMessage,omitempty"`
	ImagePromptWorkflowRunID string                   `json:"imagePromptWorkflowRunId,omitempty"`
	VideoPrompt              string                   `json:"videoPrompt,omitempty"`
	VideoPromptStatus        string                   `json:"videoPromptStatus,omitempty"`
	VideoPromptErrorCode     string                   `json:"videoPromptErrorCode,omitempty"`
	VideoPromptErrorMessage  string                   `json:"videoPromptErrorMessage,omitempty"`
	VideoPromptWorkflowRunID string                   `json:"videoPromptWorkflowRunId,omitempty"`
	VideoReferenceMode       string                   `json:"videoReferenceMode,omitempty"`
	VideoReferenceKeys       []string                 `json:"videoReferenceKeys,omitempty"`
	ImageArtifactID          string                   `json:"imageArtifactId,omitempty"`
	ImageMediaFileID         string                   `json:"imageMediaFileId,omitempty"`
	ImageStorageKey          string                   `json:"imageStorageKey,omitempty"`
	ImageStatus              string                   `json:"imageStatus,omitempty"`
	VideoArtifactID          string                   `json:"videoArtifactId,omitempty"`
	VideoMediaFileID         string                   `json:"videoMediaFileId,omitempty"`
	VideoStorageKey          string                   `json:"videoStorageKey,omitempty"`
	VideoProviderAsyncTaskID string                   `json:"providerAsyncTaskId,omitempty"`
	VideoExternalTaskID      string                   `json:"externalTaskId,omitempty"`
	Status                   string                   `json:"status"`
	ManualOverride           bool                     `json:"manualOverride,omitempty"`
	StaleState               string                   `json:"staleState,omitempty"`
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
	if len(shots) == 0 {
		shots = []StoryboardShot{{
			ShotNo:      1,
			Duration:    defaultShotDuration,
			Visual:      strings.TrimSpace(fallbackPrompt),
			ImagePrompt: strings.TrimSpace(fallbackPrompt),
			VideoPrompt: strings.TrimSpace(fallbackPrompt),
		}}
	}
	out := make([]StoryboardShot, 0, len(shots))
	for i, shot := range shots {
		shot.Title = strings.TrimSpace(shot.Title)
		shot.Visual = strings.TrimSpace(shot.Visual)
		shot.Camera = strings.TrimSpace(shot.Camera)
		shot.Motion = strings.TrimSpace(shot.Motion)
		shot.Mood = strings.TrimSpace(shot.Mood)
		shot.Dialogue = NormalizeStoryboardDialogue(shot.Dialogue)
		shot.ImagePrompt = strings.TrimSpace(shot.ImagePrompt)
		shot.VideoPrompt = strings.TrimSpace(shot.VideoPrompt)
		if shot.ShotNo <= 0 {
			shot.ShotNo = i + 1
		}
		if shot.Duration <= 0 {
			shot.Duration = defaultShotDuration
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
		return []StoryboardShot{{ShotNo: 1, Duration: defaultShotDuration, Visual: strings.TrimSpace(fallbackPrompt), ImagePrompt: strings.TrimSpace(fallbackPrompt), VideoPrompt: strings.TrimSpace(fallbackPrompt)}}
	}
	return out
}

func QuantizeStoryboardShotCandidates(shots []StoryboardShot, project ProjectProductionSettings) ([]StoryboardShot, error) {
	timebase := storyboardtiming.Timebase{
		TicksPerSecond: project.TimelineTimebase,
		FPSNumerator:   int64(project.FPSNumerator),
		FPSDenominator: int64(project.FPSDenominator),
	}
	if err := timebase.Validate(); err != nil {
		return nil, err
	}
	out := append([]StoryboardShot(nil), shots...)
	cursor := int64(0)
	for index := range out {
		duration := out[index].Duration
		if duration <= 0 {
			duration = defaultShotDuration
		}
		durationTicks := timebase.SecondsToFrameTicksCeil(duration)
		out[index].StartTick = cursor
		out[index].EndTick = cursor + durationTicks
		out[index].DurationTicks = durationTicks
		out[index].Duration = timebase.TicksToSeconds(durationTicks)
		out[index].DurationSource = "agent_estimated"
		cursor = out[index].EndTick
	}
	return out, nil
}

func resolveWorkflowMaxShots(raw json.RawMessage) int {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" {
		return 0
	}
	var decoded struct {
		MaxShots int `json:"maxShots"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return 0
	}
	if decoded.MaxShots <= 0 {
		return 0
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
		ID:                   shotID,
		WorkflowRunID:        workflowRunID,
		ScriptSceneID:        shot.ScriptSceneID,
		ShotIndex:            shotIndex,
		ShotNo:               shot.ShotNo,
		Title:                shot.Title,
		Duration:             shot.Duration,
		StartTick:            shot.StartTick,
		EndTick:              shot.EndTick,
		PlannedDurationTicks: shot.DurationTicks,
		DurationSource:       shot.DurationSource,
		Visual:               shot.Visual,
		Camera:               shot.Camera,
		Motion:               shot.Motion,
		Mood:                 shot.Mood,
		Dialogue:             append([]StoryboardDialogueLine(nil), shot.Dialogue...),
		ImagePrompt:          shot.ImagePrompt,
		VideoPrompt:          shot.VideoPrompt,
		Status:               "storyboard_ready",
	}
}

func assignScriptScenesToShots(shots []StoryboardShot, scenes []ScriptSceneRecord) []StoryboardShot {
	if len(shots) == 0 || len(scenes) == 0 {
		return shots
	}
	sceneByID := map[string]ScriptSceneRecord{}
	sceneByNo := map[int]ScriptSceneRecord{}
	for _, scene := range scenes {
		sceneByID[scene.ID] = scene
		sceneByNo[scene.SceneNo] = scene
	}
	out := make([]StoryboardShot, 0, len(shots))
	for i, shot := range shots {
		if scene, ok := sceneByID[strings.TrimSpace(shot.ScriptSceneID)]; ok {
			shot.ScriptSceneID = scene.ID
		} else if scene, ok := sceneByNo[firstPositiveInt(shot.SourceSceneNo, shot.SceneNo)]; ok {
			shot.ScriptSceneID = scene.ID
		} else {
			sceneIndex := 0
			if len(shots) > 0 {
				sceneIndex = i * len(scenes) / len(shots)
			}
			if sceneIndex >= len(scenes) {
				sceneIndex = len(scenes) - 1
			}
			shot.ScriptSceneID = scenes[sceneIndex].ID
		}
		out = append(out, shot)
	}
	return out
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
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
