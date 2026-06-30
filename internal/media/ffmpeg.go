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
	if fps <= 0 {
		fps = defaultFPS
	}
	filter := fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,fps=%d,format=yuv420p",
		width,
		height,
		width,
		height,
		fps,
	)
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", inputPath,
		"-vf", filter,
		"-r", strconv.Itoa(fps),
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-pix_fmt", "yuv420p",
		"-an",
		"-movflags", "+faststart",
		outputPath,
	}
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
