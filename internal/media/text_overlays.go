package media

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultEndCardDurationSeconds = 2.0

func BurnTextOverlays(
	ctx context.Context,
	inputPath string,
	outputPath string,
	width int,
	height int,
	overlays []TextOverlay,
) error {
	valid := normalizeTextOverlays(overlays)
	if len(valid) == 0 {
		return fmt.Errorf("at least one valid text overlay is required")
	}
	assPath := filepath.Join(filepath.Dir(outputPath), strings.TrimSuffix(filepath.Base(outputPath), filepath.Ext(outputPath))+".ass")
	if err := os.WriteFile(assPath, []byte(buildASSDocument(width, height, valid)), 0o600); err != nil {
		return err
	}
	defer os.Remove(assPath)
	filter := "ass=filename='" + escapeFFmpegFilterPath(assPath) + "'"
	return runFFmpeg(ctx,
		"-hide_banner", "-loglevel", "error", "-y",
		"-i", inputPath,
		"-map", "0:v:0", "-map", "0:a?",
		"-vf", filter,
		"-c:v", "libx264", "-preset", "veryfast", "-pix_fmt", "yuv420p",
		"-c:a", "copy", "-movflags", "+faststart",
		outputPath,
	)
}

func CreateEndCardFromVideo(
	ctx context.Context,
	inputPath string,
	outputPath string,
	width int,
	height int,
	fpsNumerator int,
	fpsDenominator int,
	card EndCard,
) error {
	text := strings.TrimSpace(card.Text)
	if text == "" {
		return fmt.Errorf("end card text is required")
	}
	duration := card.DurationSeconds
	if duration <= 0 {
		duration = defaultEndCardDurationSeconds
	}
	if fpsNumerator <= 0 {
		fpsNumerator = defaultFPS
	}
	if fpsDenominator <= 0 {
		fpsDenominator = 1
	}
	probe, err := ProbeVideo(ctx, inputPath)
	if err != nil {
		return err
	}
	framePath := filepath.Join(filepath.Dir(outputPath), "cta-end-card-frame.png")
	if err := ExtractLastFrame(ctx, inputPath, framePath, probe.DurationSeconds); err != nil {
		return err
	}
	defer os.Remove(framePath)
	basePath := filepath.Join(filepath.Dir(outputPath), "cta-end-card-base.mp4")
	defer os.Remove(basePath)
	fpsExpression := fmt.Sprintf("%d/%d", fpsNumerator, fpsDenominator)
	filter := fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,boxblur=8:2,eq=brightness=-0.18,fps=%s,format=yuv420p",
		width, height, width, height, fpsExpression,
	)
	if err := runFFmpeg(ctx,
		"-hide_banner", "-loglevel", "error", "-y",
		"-loop", "1", "-i", framePath,
		"-f", "lavfi", "-i", "anullsrc=channel_layout=stereo:sample_rate=48000",
		"-t", formatSeconds(duration),
		"-map", "0:v:0", "-map", "1:a:0",
		"-vf", filter,
		"-r", fpsExpression,
		"-c:v", "libx264", "-preset", "veryfast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "192k", "-ar", "48000", "-ac", "2",
		"-shortest", "-movflags", "+faststart",
		basePath,
	); err != nil {
		return err
	}
	return BurnTextOverlays(ctx, basePath, outputPath, width, height, []TextOverlay{{
		Text: text, StartSeconds: 0, EndSeconds: duration, Position: "center",
	}})
}

func normalizeTextOverlays(overlays []TextOverlay) []TextOverlay {
	result := make([]TextOverlay, 0, len(overlays))
	for _, overlay := range overlays {
		overlay.Text = strings.TrimSpace(overlay.Text)
		if overlay.Text == "" || overlay.EndSeconds <= overlay.StartSeconds || overlay.StartSeconds < 0 {
			continue
		}
		switch overlay.Position {
		case "top", "center", "bottom":
		default:
			overlay.Position = "bottom"
		}
		result = append(result, overlay)
	}
	return result
}

func buildASSDocument(width int, height int, overlays []TextOverlay) string {
	if width <= 0 {
		width = 1280
	}
	if height <= 0 {
		height = 720
	}
	fontSize := height / 18
	if fontSize < 28 {
		fontSize = 28
	}
	marginV := height / 14
	var builder strings.Builder
	fmt.Fprintf(&builder, "[Script Info]\nScriptType: v4.00+\nPlayResX: %d\nPlayResY: %d\nWrapStyle: 0\nScaledBorderAndShadow: yes\n\n", width, height)
	builder.WriteString("[V4+ Styles]\n")
	builder.WriteString("Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	fmt.Fprintf(&builder, "Style: Default,Noto Sans CJK SC,%d,&H00FFFFFF,&H00FFFFFF,&H00101010,&H70000000,-1,0,0,0,100,100,0,0,1,3,1,2,48,48,%d,1\n\n", fontSize, marginV)
	builder.WriteString("[Events]\n")
	builder.WriteString("Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")
	for _, overlay := range overlays {
		alignment := "2"
		switch overlay.Position {
		case "top":
			alignment = "8"
		case "center":
			alignment = "5"
		}
		fmt.Fprintf(&builder, "Dialogue: 0,%s,%s,Default,,0,0,0,,{\\an%s}%s\n",
			assTimestamp(overlay.StartSeconds), assTimestamp(overlay.EndSeconds), alignment, escapeASSText(overlay.Text))
	}
	return builder.String()
}

func assTimestamp(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	duration := time.Duration(seconds * float64(time.Second))
	hours := int(duration / time.Hour)
	duration -= time.Duration(hours) * time.Hour
	minutes := int(duration / time.Minute)
	duration -= time.Duration(minutes) * time.Minute
	wholeSeconds := int(duration / time.Second)
	centiseconds := int((duration - time.Duration(wholeSeconds)*time.Second) / (10 * time.Millisecond))
	return fmt.Sprintf("%d:%02d:%02d.%02d", hours, minutes, wholeSeconds, centiseconds)
}

func escapeASSText(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "{", "\\{")
	value = strings.ReplaceAll(value, "}", "\\}")
	value = strings.ReplaceAll(value, "\r\n", "\\N")
	value = strings.ReplaceAll(value, "\n", "\\N")
	value = strings.ReplaceAll(value, "\r", "\\N")
	return value
}

func escapeFFmpegFilterPath(value string) string {
	value = filepath.ToSlash(value)
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, ":", "\\:")
	value = strings.ReplaceAll(value, "'", "\\'")
	return value
}
