package workflows

import "testing"

func TestBuildTimelineManifest(t *testing.T) {
	manifest := buildTimelineManifest(ComposeFinalVideoInput{
		ProjectID:     "project",
		WorkflowRunID: "workflow",
		AspectRatio:   "9:16",
		Resolution:    "1080p",
	}, []composeClipRecord{{
		ShotID:           "shot-1",
		ShotNo:           1,
		ShotIndex:        0,
		VideoArtifactID:  "artifact-1",
		VideoMediaFileID: "media-1",
		StorageKey:       "clips/shot-1.mp4",
		DurationSeconds:  5,
	}})
	if manifest.WorkflowRunID != "workflow" || manifest.ProjectID != "project" || len(manifest.Clips) != 1 {
		t.Fatalf("manifest = %+v", manifest)
	}
	if manifest.Compose["aspectRatio"] != "9:16" || manifest.Compose["resolution"] != "1080p" || manifest.Compose["format"] != "mp4" {
		t.Fatalf("compose settings = %+v", manifest.Compose)
	}
	clip := manifest.Clips[0]
	if clip.ShotID != "shot-1" || clip.ShotNo != 1 || clip.VideoArtifactID != "artifact-1" || clip.VideoMediaFileID != "media-1" || clip.StorageKey != "clips/shot-1.mp4" {
		t.Fatalf("clip = %+v", clip)
	}
}

func TestComposeFinalVideoNoClipsCode(t *testing.T) {
	if codeNoVideoClipsToCompose != "NO_VIDEO_CLIPS_TO_COMPOSE" {
		t.Fatalf("codeNoVideoClipsToCompose = %q", codeNoVideoClipsToCompose)
	}
}
