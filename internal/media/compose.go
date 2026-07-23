package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultMaxClipBytes = 512 << 20
)

var ErrNoVideoClips = errors.New("NO_VIDEO_CLIPS_TO_COMPOSE")

func ComposeClipsWithStore(ctx context.Context, req ComposeRequest, objectStore ObjectStore) (ComposeResult, error) {
	if len(req.Clips) == 0 {
		return ComposeResult{}, ErrNoVideoClips
	}
	if objectStore == nil {
		return ComposeResult{}, fmt.Errorf("object storage is required")
	}
	mimeType := strings.TrimSpace(req.OutputMimeType)
	if mimeType == "" {
		mimeType = "video/mp4"
	}
	if mimeType != "video/mp4" {
		return ComposeResult{}, fmt.Errorf("unsupported output mime type %q", mimeType)
	}

	tempDir, err := os.MkdirTemp("", "cineweave-compose-*")
	if err != nil {
		return ComposeResult{}, err
	}
	defer os.RemoveAll(tempDir)

	width, height := ResolveDimensions(req.AspectRatio, req.Resolution)
	normalizedPaths := make([]string, 0, len(req.Clips))
	for index, clip := range req.Clips {
		if strings.TrimSpace(clip.StorageKey) == "" {
			return ComposeResult{}, fmt.Errorf("clip %d storageKey is required", index)
		}
		body, _, err := objectStore.GetObject(ctx, clip.StorageKey, DefaultMaxClipBytes)
		if err != nil {
			return ComposeResult{}, fmt.Errorf("download clip %d: %w", index, err)
		}
		inputPath := filepath.Join(tempDir, fmt.Sprintf("clip-%03d.mp4", index))
		if err := os.WriteFile(inputPath, body, 0o600); err != nil {
			return ComposeResult{}, err
		}
		normalizedPath := filepath.Join(tempDir, fmt.Sprintf("normalized-%03d.mp4", index))
		if err := NormalizeClipWithTrimRateAV(
			ctx,
			inputPath,
			normalizedPath,
			width,
			height,
			req.FPSNumerator,
			req.FPSDenominator,
			clip.TrimStartSeconds,
			clip.TrimEndSeconds,
		); err != nil {
			return ComposeResult{}, fmt.Errorf("normalize clip %d: %w", index, err)
		}
		if len(clip.TextOverlays) > 0 {
			overlaidPath := filepath.Join(tempDir, fmt.Sprintf("overlaid-%03d.mp4", index))
			if err := BurnTextOverlays(ctx, normalizedPath, overlaidPath, width, height, clip.TextOverlays); err != nil {
				return ComposeResult{}, fmt.Errorf("render clip %d overlays: %w", index, err)
			}
			normalizedPath = overlaidPath
		}
		normalizedPaths = append(normalizedPaths, normalizedPath)
	}
	if req.EndCard != nil && strings.TrimSpace(req.EndCard.Text) != "" && len(normalizedPaths) > 0 {
		endCardPath := filepath.Join(tempDir, "cta-end-card.mp4")
		if err := CreateEndCardFromVideo(
			ctx,
			normalizedPaths[len(normalizedPaths)-1],
			endCardPath,
			width,
			height,
			req.FPSNumerator,
			req.FPSDenominator,
			*req.EndCard,
		); err != nil {
			return ComposeResult{}, fmt.Errorf("render CTA end card: %w", err)
		}
		normalizedPaths = append(normalizedPaths, endCardPath)
	}

	outputPath := filepath.Join(tempDir, "final.mp4")
	if err := ConcatClips(ctx, normalizedPaths, outputPath); err != nil {
		return ComposeResult{}, fmt.Errorf("concat clips: %w", err)
	}
	probe, err := ProbeVideo(ctx, outputPath)
	if err != nil {
		return ComposeResult{}, err
	}
	storageKey := strings.TrimSpace(req.OutputStorageKey)
	if storageKey == "" {
		storageKey = BuildFinalVideoStorageKey(req, time.Now().UTC())
	}
	put, err := objectStore.PutFile(ctx, storageKey, outputPath, mimeType)
	if err != nil {
		return ComposeResult{}, err
	}
	return ComposeResult{
		StorageKey:      put.StorageKey,
		MimeType:        mimeType,
		ByteSize:        put.ByteSize,
		ContentHash:     put.ContentHash,
		DurationSeconds: probe.DurationSeconds,
		Width:           probe.Width,
		Height:          probe.Height,
		Probe:           probe,
	}, nil
}

func BuildFinalVideoStorageKey(req ComposeRequest, createdAt time.Time) string {
	return fmt.Sprintf(
		"org/%s/project/%s/workflow/%s/final/final-video-%s.mp4",
		storageSegment(req.OrganizationID),
		storageSegment(req.ProjectID),
		storageSegment(req.WorkflowRunID),
		createdAt.UTC().Format("20060102T150405Z"),
	)
}

func storageSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteByte('-')
		}
	}
	segment := strings.Trim(builder.String(), "-_")
	if segment == "" {
		return "unknown"
	}
	return segment
}
