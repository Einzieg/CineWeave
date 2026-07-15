package media

import (
	"context"
	"math"
	"path/filepath"
	"strconv"
	"testing"
)

func TestMixAudioTracksWithStoreProducesNormalizedStereoMix(t *testing.T) {
	requireFFmpeg(t)
	ctx := context.Background()
	tempDir := t.TempDir()
	dialogue := filepath.Join(tempDir, "dialogue.wav")
	ambience := filepath.Join(tempDir, "ambience.wav")
	writeTestAudio(t, dialogue, 440, 2)
	writeTestAudio(t, ambience, 660, 1)
	store := newComposeMemoryStore(t, map[string]string{"audio/dialogue.wav": dialogue, "audio/ambience.wav": ambience})

	result, err := MixAudioTracksWithStore(ctx, AudioMixRequest{
		OrganizationID: "org", ProjectID: "project", WorkflowRunID: "workflow",
		DurationSeconds: 2, SampleRate: 48_000, ChannelCount: 2, OutputStorageKey: "audio/mix.m4a",
		Tracks: []AudioMixTrack{
			{ID: "dialogue", Kind: "dialogue", StorageKey: "audio/dialogue.wav", MimeType: "audio/wav", SourceDurationSeconds: 2, GainDB: 0},
			{ID: "ambience", Kind: "ambience", StorageKey: "audio/ambience.wav", MimeType: "audio/wav", SourceDurationSeconds: 1, StartSeconds: 0.5, GainDB: -12, FadeInSeconds: 0.1, FadeOutSeconds: 0.1},
		},
	}, store)
	if err != nil {
		t.Fatalf("MixAudioTracksWithStore: %v", err)
	}
	if result.Put.ByteSize <= 0 || result.Put.StorageKey != "audio/mix.m4a" {
		t.Fatalf("put = %+v", result.Put)
	}
	if !result.Probe.HasAudio || result.Probe.AudioStreamCount != 1 || result.Probe.AudioSampleRate != 48_000 || result.Probe.AudioChannelCount != 2 {
		t.Fatalf("probe = %+v", result.Probe)
	}
	if math.Abs(result.Probe.DurationSeconds-2) > 0.08 {
		t.Fatalf("duration = %f, want 2 seconds", result.Probe.DurationSeconds)
	}
	if _, ok := store.objects[result.Put.StorageKey]; !ok {
		t.Fatalf("mix was not uploaded to %q", result.Put.StorageKey)
	}
}

func writeTestAudio(t *testing.T, outputPath string, frequency int, durationSeconds float64) {
	t.Helper()
	if err := runFFmpeg(context.Background(),
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency="+strconv.Itoa(frequency)+":sample_rate=48000:duration="+ffmpegFloat(durationSeconds),
		"-c:a", "pcm_s16le", outputPath,
	); err != nil {
		t.Fatalf("write test audio: %v", err)
	}
}
