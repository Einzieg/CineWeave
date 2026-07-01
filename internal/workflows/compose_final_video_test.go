package workflows

import "testing"

func TestBuildTimelineManifest(t *testing.T) {
	trimEnd := 4.25
	targetDuration := 3.5
	manifest := buildTimelineManifest(ComposeFinalVideoInput{
		ProjectID:     "project",
		WorkflowRunID: "workflow",
		TimelineID:    "timeline-1",
		Title:         "Version A",
		AspectRatio:   "9:16",
		Resolution:    "1080p",
	}, []composeClipRecord{{
		TimelineClipID:        "clip-1",
		ShotID:                "shot-1",
		ShotNo:                1,
		ShotIndex:             0,
		ClipIndex:             2,
		Title:                 "Opening",
		Enabled:               true,
		VideoArtifactID:       "artifact-1",
		VideoMediaFileID:      "media-1",
		StorageKey:            "clips/shot-1.mp4",
		DurationSeconds:       5,
		TrimStartSeconds:      1.25,
		TrimEndSeconds:        &trimEnd,
		TargetDurationSeconds: &targetDuration,
	}})
	if manifest.WorkflowRunID != "workflow" || manifest.ProjectID != "project" || manifest.TimelineID != "timeline-1" || len(manifest.Clips) != 1 {
		t.Fatalf("manifest = %+v", manifest)
	}
	if manifest.Compose["aspectRatio"] != "9:16" || manifest.Compose["resolution"] != "1080p" || manifest.Compose["format"] != "mp4" || manifest.Compose["title"] != "Version A" {
		t.Fatalf("compose settings = %+v", manifest.Compose)
	}
	clip := manifest.Clips[0]
	if clip.TimelineClipID != "clip-1" || clip.ShotID != "shot-1" || clip.ShotNo != 1 || clip.ClipIndex != 2 || !clip.Enabled || clip.VideoArtifactID != "artifact-1" || clip.VideoMediaFileID != "media-1" || clip.StorageKey != "clips/shot-1.mp4" {
		t.Fatalf("clip = %+v", clip)
	}
	if clip.TrimStartSeconds != 1.25 || clip.TrimEndSeconds == nil || *clip.TrimEndSeconds != trimEnd || clip.TargetDurationSeconds == nil || *clip.TargetDurationSeconds != targetDuration {
		t.Fatalf("clip = %+v", clip)
	}
}

func TestComposeFinalVideoNoClipsCode(t *testing.T) {
	if codeNoVideoClipsToCompose != "NO_VIDEO_CLIPS_TO_COMPOSE" {
		t.Fatalf("codeNoVideoClipsToCompose = %q", codeNoVideoClipsToCompose)
	}
}
