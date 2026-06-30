package media

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type ProbeResult struct {
	DurationSeconds float64
	Width           int
	Height          int
}

func ProbeVideo(ctx context.Context, filePath string) (ProbeResult, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-show_entries", "format=duration",
		"-of", "json",
		filePath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ProbeResult{}, fmt.Errorf("ffprobe failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var decoded struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &decoded); err != nil {
		return ProbeResult{}, err
	}
	result := ProbeResult{}
	if len(decoded.Streams) > 0 {
		result.Width = decoded.Streams[0].Width
		result.Height = decoded.Streams[0].Height
	}
	if decoded.Format.Duration != "" {
		if duration, err := strconv.ParseFloat(decoded.Format.Duration, 64); err == nil {
			result.DurationSeconds = duration
		}
	}
	return result, nil
}
