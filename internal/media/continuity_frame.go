package media

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/Einzieg/cineweave/internal/storage"
)

type VideoTailFrameRequest struct {
	SourceStorageKey string `json:"sourceStorageKey"`
	SourceMimeType   string `json:"sourceMimeType,omitempty"`
	OutputStorageKey string `json:"outputStorageKey"`
}

type VideoTailFrameResult struct {
	StorageKey       string            `json:"storageKey"`
	MimeType         string            `json:"mimeType"`
	ByteSize         int64             `json:"byteSize"`
	ContentHash      string            `json:"contentHash"`
	Width            int               `json:"width"`
	Height           int               `json:"height"`
	FrameTimeSeconds float64           `json:"frameTimeSeconds"`
	SourceProbe      ProbeResult       `json:"sourceProbe"`
	FrameProbe       ProbeResult       `json:"frameProbe"`
	Put              storage.PutResult `json:"-"`
}

func ExtractVideoTailFrame(ctx context.Context, req VideoTailFrameRequest, objectStore ObjectStore) (VideoTailFrameResult, error) {
	if objectStore == nil {
		return VideoTailFrameResult{}, fmt.Errorf("object storage is required")
	}
	if strings.TrimSpace(req.SourceStorageKey) == "" || strings.TrimSpace(req.OutputStorageKey) == "" {
		return VideoTailFrameResult{}, fmt.Errorf("source and output storage keys are required")
	}
	body, _, err := objectStore.GetObject(ctx, req.SourceStorageKey, DefaultMaxClipBytes)
	if err != nil {
		return VideoTailFrameResult{}, fmt.Errorf("download source video: %w", err)
	}
	tempDir, err := os.MkdirTemp("", "cineweave-continuity-frame-*")
	if err != nil {
		return VideoTailFrameResult{}, err
	}
	defer os.RemoveAll(tempDir)

	sourcePath := filepath.Join(tempDir, "source-video")
	framePath := filepath.Join(tempDir, "tail-frame.png")
	if err := os.WriteFile(sourcePath, body, 0o600); err != nil {
		return VideoTailFrameResult{}, fmt.Errorf("write source video: %w", err)
	}
	sourceProbe, err := ProbeVideo(ctx, sourcePath)
	if err != nil {
		return VideoTailFrameResult{}, fmt.Errorf("probe source video: %w", err)
	}
	if sourceProbe.VideoStreamCount == 0 || sourceProbe.Width <= 0 || sourceProbe.Height <= 0 {
		return VideoTailFrameResult{}, fmt.Errorf("source has no decodable video stream")
	}
	if err := ExtractLastFrame(ctx, sourcePath, framePath, sourceProbe.DurationSeconds); err != nil {
		return VideoTailFrameResult{}, fmt.Errorf("extract video tail frame: %w", err)
	}
	frameProbe, err := ProbeVideo(ctx, framePath)
	if err != nil {
		return VideoTailFrameResult{}, fmt.Errorf("probe video tail frame: %w", err)
	}
	put, err := objectStore.PutFile(ctx, req.OutputStorageKey, framePath, "image/png")
	if err != nil {
		return VideoTailFrameResult{}, fmt.Errorf("upload video tail frame: %w", err)
	}
	frameTime := sourceProbe.DurationSeconds
	if sourceProbe.FrameRate > 0 {
		frameTime = math.Max(0, sourceProbe.DurationSeconds-(1/sourceProbe.FrameRate))
	}
	return VideoTailFrameResult{
		StorageKey: put.StorageKey, MimeType: "image/png", ByteSize: put.ByteSize, ContentHash: put.ContentHash,
		Width: frameProbe.Width, Height: frameProbe.Height, FrameTimeSeconds: frameTime,
		SourceProbe: sourceProbe, FrameProbe: frameProbe, Put: put,
	}, nil
}
