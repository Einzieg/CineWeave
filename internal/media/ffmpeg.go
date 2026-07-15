package media

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const defaultFPS = 24

func ResolveDimensions(aspectRatio, resolution string) (int, int) {
	longEdge := 1280
	switch strings.ToLower(strings.TrimSpace(resolution)) {
	case "480p":
		longEdge = 854
	case "720p", "":
		longEdge = 1280
	case "1080p":
		longEdge = 1920
	default:
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(resolution)), "p") {
			if parsed, err := strconv.Atoi(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(resolution)), "p")); err == nil && parsed > 0 {
				longEdge = even(parsed * 16 / 9)
			}
		}
	}

	switch strings.TrimSpace(aspectRatio) {
	case "9:16":
		return even(longEdge * 9 / 16), even(longEdge)
	case "1:1":
		shortEdge := 720
		if longEdge >= 1920 {
			shortEdge = 1080
		} else if longEdge <= 854 {
			shortEdge = 480
		}
		return even(shortEdge), even(shortEdge)
	default:
		height := 720
		if longEdge >= 1920 {
			height = 1080
		} else if longEdge <= 854 {
			height = 480
		}
		return even(longEdge), even(height)
	}
}

func NormalizeClip(ctx context.Context, inputPath, outputPath string, width, height, fps int) error {
	return NormalizeClipWithTrim(ctx, inputPath, outputPath, width, height, fps, 0, nil)
}

func NormalizeClipWithTrim(ctx context.Context, inputPath, outputPath string, width, height, fps int, trimStartSeconds float64, trimEndSeconds *float64) error {
	if fps <= 0 {
		fps = defaultFPS
	}
	return NormalizeClipWithTrimRate(ctx, inputPath, outputPath, width, height, fps, 1, trimStartSeconds, trimEndSeconds)
}

func NormalizeClipWithTrimRate(ctx context.Context, inputPath, outputPath string, width, height, fpsNumerator, fpsDenominator int, trimStartSeconds float64, trimEndSeconds *float64) error {
	if fpsNumerator <= 0 {
		fpsNumerator = defaultFPS
	}
	if fpsDenominator <= 0 {
		fpsDenominator = 1
	}
	if trimStartSeconds < 0 {
		trimStartSeconds = 0
	}
	fpsExpression := fmt.Sprintf("%d/%d", fpsNumerator, fpsDenominator)
	filter := fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,fps=%s,format=yuv420p",
		width,
		height,
		width,
		height,
		fpsExpression,
	)
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
	}
	if trimStartSeconds > 0 {
		args = append(args, "-ss", formatSeconds(trimStartSeconds))
	}
	args = append(args,
		"-i", inputPath,
	)
	if trimEndSeconds != nil && *trimEndSeconds > trimStartSeconds {
		args = append(args, "-t", formatSeconds(*trimEndSeconds-trimStartSeconds))
	}
	args = append(args,
		"-vf", filter,
		"-r", fpsExpression,
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-pix_fmt", "yuv420p",
		"-an",
		"-movflags", "+faststart",
		outputPath,
	)
	return runFFmpeg(ctx, args...)
}

func NormalizeClipWithTrimRateAV(ctx context.Context, inputPath, outputPath string, width, height, fpsNumerator, fpsDenominator int, trimStartSeconds float64, trimEndSeconds *float64) error {
	if fpsNumerator <= 0 {
		fpsNumerator = defaultFPS
	}
	if fpsDenominator <= 0 {
		fpsDenominator = 1
	}
	if trimStartSeconds < 0 {
		trimStartSeconds = 0
	}
	probe, err := ProbeVideo(ctx, inputPath)
	if err != nil {
		return err
	}
	fpsExpression := fmt.Sprintf("%d/%d", fpsNumerator, fpsDenominator)
	filter := fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,fps=%s,format=yuv420p",
		width, height, width, height, fpsExpression,
	)
	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	if trimStartSeconds > 0 {
		args = append(args, "-ss", formatSeconds(trimStartSeconds))
	}
	args = append(args, "-i", inputPath)
	if !probe.HasAudio {
		args = append(args, "-f", "lavfi", "-i", "anullsrc=channel_layout=stereo:sample_rate=48000")
	}
	if trimEndSeconds != nil && *trimEndSeconds > trimStartSeconds {
		args = append(args, "-t", formatSeconds(*trimEndSeconds-trimStartSeconds))
	}
	args = append(args, "-map", "0:v:0")
	if probe.HasAudio {
		args = append(args, "-map", "0:a:0", "-af", "aresample=48000,apad")
	} else {
		args = append(args, "-map", "1:a:0")
	}
	args = append(args,
		"-vf", filter,
		"-r", fpsExpression,
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-b:a", "192k",
		"-ar", "48000",
		"-ac", "2",
		"-shortest",
		"-movflags", "+faststart",
		outputPath,
	)
	return runFFmpeg(ctx, args...)
}

func ConcatClips(ctx context.Context, clipPaths []string, outputPath string) error {
	listPath, err := writeConcatList(filepath.Dir(outputPath), clipPaths)
	if err != nil {
		return err
	}
	defer os.Remove(listPath)
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", listPath,
		"-c", "copy",
		"-movflags", "+faststart",
		outputPath,
	}
	return runFFmpeg(ctx, args...)
}

func ExtractAudioTrack(ctx context.Context, inputPath, outputPath string, trimStartSeconds float64, trimEndSeconds *float64) error {
	if trimStartSeconds < 0 {
		trimStartSeconds = 0
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	if trimStartSeconds > 0 {
		args = append(args, "-ss", formatSeconds(trimStartSeconds))
	}
	args = append(args, "-i", inputPath)
	if trimEndSeconds != nil && *trimEndSeconds > trimStartSeconds {
		args = append(args, "-t", formatSeconds(*trimEndSeconds-trimStartSeconds))
	}
	args = append(args, "-vn", "-c:a", "aac", "-b:a", "192k", "-ar", "48000", "-ac", "2", "-movflags", "+faststart", outputPath)
	return runFFmpeg(ctx, args...)
}

// ExtractLastFrame decodes a bounded tail window and reverses it so the first
// emitted PNG is the final decodable frame, rather than an earlier keyframe.
func ExtractLastFrame(ctx context.Context, inputPath, outputPath string, durationSeconds float64) error {
	if strings.TrimSpace(inputPath) == "" || strings.TrimSpace(outputPath) == "" {
		return fmt.Errorf("input and output paths are required")
	}
	tailWindowSeconds := 2.0
	startSeconds := durationSeconds - tailWindowSeconds
	if startSeconds < 0 {
		startSeconds = 0
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	if startSeconds > 0 {
		args = append(args, "-ss", formatSeconds(startSeconds))
	}
	args = append(args,
		"-i", inputPath,
		"-map", "0:v:0",
		"-vf", "reverse",
		"-frames:v", "1",
		"-an",
		outputPath,
	)
	if err := runFFmpeg(ctx, args...); err != nil {
		return err
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		return fmt.Errorf("stat extracted frame: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("extracted frame is empty")
	}
	return nil
}

func ConcatFileList(clipPaths []string) string {
	var builder strings.Builder
	for _, clipPath := range clipPaths {
		builder.WriteString("file '")
		builder.WriteString(EscapeConcatPath(clipPath))
		builder.WriteString("'\n")
	}
	return builder.String()
}

func EscapeConcatPath(clipPath string) string {
	pathValue := filepath.ToSlash(clipPath)
	return strings.ReplaceAll(pathValue, "'", "'\\''")
}

func writeConcatList(dir string, clipPaths []string) (string, error) {
	file, err := os.CreateTemp(dir, "concat-*.txt")
	if err != nil {
		return "", err
	}
	defer file.Close()
	if _, err := file.WriteString(ConcatFileList(clipPaths)); err != nil {
		_ = os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
}

func runFFmpeg(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func even(value int) int {
	if value <= 0 {
		return 2
	}
	if value%2 == 0 {
		return value
	}
	return value - 1
}

func formatSeconds(value float64) string {
	return strconv.FormatFloat(value, 'f', 3, 64)
}
