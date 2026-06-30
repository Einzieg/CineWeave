package media

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestConcatFileListEscapesPaths(t *testing.T) {
	list := ConcatFileList([]string{`C:\tmp\clip one.mp4`, "/tmp/clip's two.mp4"})
	if !strings.Contains(list, "C:/tmp/clip one.mp4") {
		t.Fatalf("concat list did not normalize slashes: %q", list)
	}
	if !strings.Contains(list, "clip'\\''s two.mp4") {
		t.Fatalf("concat list did not escape single quote: %q", list)
	}
	if strings.Count(list, "file '") != 2 {
		t.Fatalf("concat list = %q", list)
	}
}

func TestResolveDimensions(t *testing.T) {
	tests := []struct {
		aspectRatio string
		resolution  string
		wantWidth   int
		wantHeight  int
	}{
		{"16:9", "720p", 1280, 720},
		{"9:16", "720p", 720, 1280},
		{"1:1", "720p", 720, 720},
		{"16:9", "1080p", 1920, 1080},
	}
	for _, test := range tests {
		width, height := ResolveDimensions(test.aspectRatio, test.resolution)
		if width != test.wantWidth || height != test.wantHeight {
			t.Fatalf("ResolveDimensions(%q, %q) = %dx%d, want %dx%d", test.aspectRatio, test.resolution, width, height, test.wantWidth, test.wantHeight)
		}
	}
}

func TestNormalizeAndConcatWithFFmpeg(t *testing.T) {
	requireFFmpeg(t)
	ctx := context.Background()
	tempDir := t.TempDir()
	inputA := filepath.Join(tempDir, "input-a.mp4")
	inputB := filepath.Join(tempDir, "input-b.mp4")
	writeTestClip(t, inputA, "testsrc=size=160x90:rate=24")
	writeTestClip(t, inputB, "testsrc2=size=160x90:rate=24")

	normalizedA := filepath.Join(tempDir, "normalized-a.mp4")
	normalizedB := filepath.Join(tempDir, "normalized-b.mp4")
	if err := NormalizeClip(ctx, inputA, normalizedA, 320, 180, 24); err != nil {
		t.Fatalf("NormalizeClip A: %v", err)
	}
	if err := NormalizeClip(ctx, inputB, normalizedB, 320, 180, 24); err != nil {
		t.Fatalf("NormalizeClip B: %v", err)
	}
	output := filepath.Join(tempDir, "final.mp4")
	if err := ConcatClips(ctx, []string{normalizedA, normalizedB}, output); err != nil {
		t.Fatalf("ConcatClips: %v", err)
	}
	probe, err := ProbeVideo(ctx, output)
	if err != nil {
		t.Fatalf("ProbeVideo: %v", err)
	}
	if probe.Width != 320 || probe.Height != 180 || probe.DurationSeconds <= 0 {
		t.Fatalf("probe = %+v", probe)
	}
}

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
}

func writeTestClip(t *testing.T, outputPath, source string) {
	t.Helper()
	_ = os.Remove(outputPath)
	cmd := exec.Command(
		"ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-f", "lavfi",
		"-i", source,
		"-t", "0.25",
		"-pix_fmt", "yuv420p",
		outputPath,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create test clip: %v: %s", err, strings.TrimSpace(string(output)))
	}
}
