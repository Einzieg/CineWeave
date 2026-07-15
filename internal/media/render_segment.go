package media

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Einzieg/cineweave/internal/storage"
)

type RenderSegmentMediaRequest struct {
	SourceStorageKey       string
	SourceMimeType         string
	MezzanineStorageKey    string
	AudioStorageKey        string
	AspectRatio            string
	Resolution             string
	FPSNumerator           int
	FPSDenominator         int
	PlannedDurationSeconds float64
}

type RenderSegmentMediaResult struct {
	SourceProbe    ProbeResult
	Mezzanine      storage.PutResult
	MezzanineProbe ProbeResult
	Audio          *storage.PutResult
	AudioProbe     *ProbeResult
}

func ProcessRenderSegmentMedia(ctx context.Context, req RenderSegmentMediaRequest, objectStore ObjectStore) (RenderSegmentMediaResult, error) {
	if objectStore == nil || req.SourceStorageKey == "" || req.MezzanineStorageKey == "" {
		return RenderSegmentMediaResult{}, fmt.Errorf("render segment source, mezzanine key, and object storage are required")
	}
	body, mimeType, err := objectStore.GetObject(ctx, req.SourceStorageKey, 512<<20)
	if err != nil {
		return RenderSegmentMediaResult{}, fmt.Errorf("download render segment source: %w", err)
	}
	if mimeType == "" {
		mimeType = req.SourceMimeType
	}
	tempDir, err := os.MkdirTemp("", "cineweave-render-segment-*")
	if err != nil {
		return RenderSegmentMediaResult{}, err
	}
	defer os.RemoveAll(tempDir)
	sourcePath := filepath.Join(tempDir, "source.mp4")
	mezzaninePath := filepath.Join(tempDir, "mezzanine.mp4")
	audioPath := filepath.Join(tempDir, "audio.m4a")
	if err := os.WriteFile(sourcePath, body, 0o600); err != nil {
		return RenderSegmentMediaResult{}, err
	}
	sourceProbe, err := ProbeVideo(ctx, sourcePath)
	if err != nil {
		return RenderSegmentMediaResult{}, err
	}
	width, height := ResolveDimensions(req.AspectRatio, req.Resolution)
	var trimEnd *float64
	if req.PlannedDurationSeconds > 0 {
		value := req.PlannedDurationSeconds
		trimEnd = &value
	}
	if err := NormalizeClipWithTrimRate(ctx, sourcePath, mezzaninePath, width, height, req.FPSNumerator, req.FPSDenominator, 0, trimEnd); err != nil {
		return RenderSegmentMediaResult{}, err
	}
	mezzanine, err := objectStore.PutFile(ctx, req.MezzanineStorageKey, mezzaninePath, "video/mp4")
	if err != nil {
		return RenderSegmentMediaResult{}, err
	}
	mezzanineProbe, err := ProbeVideo(ctx, mezzaninePath)
	if err != nil {
		return RenderSegmentMediaResult{}, err
	}
	result := RenderSegmentMediaResult{SourceProbe: sourceProbe, Mezzanine: mezzanine, MezzanineProbe: mezzanineProbe}
	if !sourceProbe.HasAudio || req.AudioStorageKey == "" {
		return result, nil
	}
	if err := ExtractAudioTrack(ctx, sourcePath, audioPath, 0, trimEnd); err != nil {
		return RenderSegmentMediaResult{}, err
	}
	audio, err := objectStore.PutFile(ctx, req.AudioStorageKey, audioPath, "audio/mp4")
	if err != nil {
		return RenderSegmentMediaResult{}, err
	}
	audioProbe, err := ProbeVideo(ctx, audioPath)
	if err != nil {
		return RenderSegmentMediaResult{}, err
	}
	result.Audio = &audio
	result.AudioProbe = &audioProbe
	return result, nil
}
