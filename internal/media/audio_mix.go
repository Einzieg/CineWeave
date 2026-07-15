package media

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Einzieg/cineweave/internal/storage"
)

const DefaultMaxAudioTrackBytes int64 = 256 << 20

type AudioMixTrack struct {
	ID                    string  `json:"id"`
	Kind                  string  `json:"kind"`
	StorageKey            string  `json:"storageKey"`
	MimeType              string  `json:"mimeType"`
	StartSeconds          float64 `json:"startSeconds"`
	SourceDurationSeconds float64 `json:"sourceDurationSeconds"`
	TrimStartSeconds      float64 `json:"trimStartSeconds,omitempty"`
	TrimEndSeconds        float64 `json:"trimEndSeconds,omitempty"`
	GainDB                float64 `json:"gainDb,omitempty"`
	FadeInSeconds         float64 `json:"fadeInSeconds,omitempty"`
	FadeOutSeconds        float64 `json:"fadeOutSeconds,omitempty"`
}

type AudioMixRequest struct {
	OrganizationID   string          `json:"organizationId"`
	ProjectID        string          `json:"projectId"`
	WorkflowRunID    string          `json:"workflowRunId"`
	Tracks           []AudioMixTrack `json:"tracks"`
	DurationSeconds  float64         `json:"durationSeconds"`
	SampleRate       int             `json:"sampleRate"`
	ChannelCount     int             `json:"channelCount"`
	OutputStorageKey string          `json:"outputStorageKey"`
}

type AudioMixResult struct {
	Put   storage.PutResult `json:"put"`
	Probe ProbeResult       `json:"probe"`
}

func MixAudioTracksWithStore(ctx context.Context, req AudioMixRequest, objectStore ObjectStore) (AudioMixResult, error) {
	if objectStore == nil {
		return AudioMixResult{}, fmt.Errorf("object storage is required")
	}
	if len(req.Tracks) == 0 {
		return AudioMixResult{}, fmt.Errorf("at least one audio track is required")
	}
	if req.SampleRate <= 0 {
		req.SampleRate = 48000
	}
	if req.ChannelCount <= 0 {
		req.ChannelCount = 2
	}
	if req.DurationSeconds <= 0 {
		for _, track := range req.Tracks {
			duration := track.SourceDurationSeconds
			if track.TrimEndSeconds > track.TrimStartSeconds {
				duration = track.TrimEndSeconds - track.TrimStartSeconds
			}
			if end := track.StartSeconds + duration; end > req.DurationSeconds {
				req.DurationSeconds = end
			}
		}
	}
	if req.DurationSeconds <= 0 {
		return AudioMixResult{}, fmt.Errorf("audio mix duration must be positive")
	}
	if strings.TrimSpace(req.OutputStorageKey) == "" {
		return AudioMixResult{}, fmt.Errorf("output storage key is required")
	}

	tempDir, err := os.MkdirTemp("", "cineweave-audio-mix-*")
	if err != nil {
		return AudioMixResult{}, err
	}
	defer os.RemoveAll(tempDir)

	inputPaths := make([]string, 0, len(req.Tracks))
	for index, track := range req.Tracks {
		if strings.TrimSpace(track.StorageKey) == "" {
			return AudioMixResult{}, fmt.Errorf("audio track %d storage key is required", index)
		}
		body, mimeType, err := objectStore.GetObject(ctx, track.StorageKey, DefaultMaxAudioTrackBytes)
		if err != nil {
			return AudioMixResult{}, fmt.Errorf("read audio track %s: %w", track.ID, err)
		}
		if track.MimeType != "" {
			mimeType = track.MimeType
		}
		inputPath := filepath.Join(tempDir, fmt.Sprintf("input-%03d%s", index, probeFileSuffix(mimeType)))
		if err := os.WriteFile(inputPath, body, 0o600); err != nil {
			return AudioMixResult{}, err
		}
		inputPaths = append(inputPaths, inputPath)
	}

	outputPath := filepath.Join(tempDir, "mix.m4a")
	if err := mixAudioFiles(ctx, inputPaths, req.Tracks, outputPath, req.DurationSeconds, req.SampleRate, req.ChannelCount); err != nil {
		return AudioMixResult{}, err
	}
	probe, err := ProbeVideo(ctx, outputPath)
	if err != nil {
		return AudioMixResult{}, err
	}
	put, err := objectStore.PutFile(ctx, req.OutputStorageKey, outputPath, "audio/mp4")
	if err != nil {
		return AudioMixResult{}, err
	}
	return AudioMixResult{Put: put, Probe: probe}, nil
}

func mixAudioFiles(ctx context.Context, inputPaths []string, tracks []AudioMixTrack, outputPath string, durationSeconds float64, sampleRate, channelCount int) error {
	if len(inputPaths) != len(tracks) || len(inputPaths) == 0 {
		return fmt.Errorf("audio inputs and tracks must have matching non-zero lengths")
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	for _, inputPath := range inputPaths {
		args = append(args, "-i", inputPath)
	}
	filters := make([]string, 0, len(tracks)+1)
	labels := make([]string, 0, len(tracks))
	for index, track := range tracks {
		trimStart := maxFloat64(track.TrimStartSeconds, 0)
		trimDuration := track.SourceDurationSeconds
		if track.TrimEndSeconds > trimStart {
			trimDuration = track.TrimEndSeconds - trimStart
		}
		if trimDuration <= 0 {
			trimDuration = maxFloat64(durationSeconds-track.StartSeconds, 0.001)
		}
		fadeIn := minFloat64(maxFloat64(track.FadeInSeconds, 0), trimDuration)
		fadeOut := minFloat64(maxFloat64(track.FadeOutSeconds, 0), trimDuration)
		delayMS := int64(maxFloat64(track.StartSeconds, 0)*1000 + 0.5)
		filter := fmt.Sprintf(
			"[%d:a:0]aresample=%d,aformat=sample_fmts=fltp:channel_layouts=%s,atrim=start=%s:duration=%s,asetpts=PTS-STARTPTS,volume=%sdB",
			index, sampleRate, audioChannelLayout(channelCount), ffmpegFloat(trimStart), ffmpegFloat(trimDuration), ffmpegFloat(track.GainDB),
		)
		if fadeIn > 0 {
			filter += fmt.Sprintf(",afade=t=in:st=0:d=%s", ffmpegFloat(fadeIn))
		}
		if fadeOut > 0 {
			filter += fmt.Sprintf(",afade=t=out:st=%s:d=%s", ffmpegFloat(maxFloat64(trimDuration-fadeOut, 0)), ffmpegFloat(fadeOut))
		}
		filter += fmt.Sprintf(",adelay=%d:all=1[a%d]", delayMS, index)
		filters = append(filters, filter)
		labels = append(labels, fmt.Sprintf("[a%d]", index))
	}
	filters = append(filters, fmt.Sprintf(
		"%samix=inputs=%d:duration=longest:dropout_transition=0:normalize=0,alimiter=limit=0.95,atrim=duration=%s[aout]",
		strings.Join(labels, ""), len(labels), ffmpegFloat(durationSeconds),
	))
	args = append(args,
		"-filter_complex", strings.Join(filters, ";"),
		"-map", "[aout]", "-vn", "-c:a", "aac", "-b:a", "192k",
		"-ar", strconv.Itoa(sampleRate), "-ac", strconv.Itoa(channelCount),
		"-movflags", "+faststart", outputPath,
	)
	return runFFmpeg(ctx, args...)
}

func audioChannelLayout(channelCount int) string {
	if channelCount == 1 {
		return "mono"
	}
	return "stereo"
}

func ffmpegFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 6, 64)
}

func maxFloat64(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func minFloat64(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}
