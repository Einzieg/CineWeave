package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type probeScalar string

func (value *probeScalar) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) || len(data) == 0 {
		*value = ""
		return nil
	}
	if data[0] == '"' {
		var decoded string
		if err := json.Unmarshal(data, &decoded); err != nil {
			return err
		}
		*value = probeScalar(decoded)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	*value = probeScalar(number.String())
	return nil
}

type ProbeResult struct {
	DurationSeconds      float64  `json:"durationSeconds,omitempty"`
	Width                int      `json:"width,omitempty"`
	Height               int      `json:"height,omitempty"`
	FrameRateNumerator   int64    `json:"frameRateNumerator,omitempty"`
	FrameRateDenominator int64    `json:"frameRateDenominator,omitempty"`
	FrameRate            float64  `json:"frameRate,omitempty"`
	FrameCount           int64    `json:"frameCount,omitempty"`
	FrameCountEstimated  bool     `json:"frameCountEstimated"`
	VideoStreamCount     int      `json:"videoStreamCount"`
	AudioStreamCount     int      `json:"audioStreamCount"`
	HasAudio             bool     `json:"hasAudio"`
	VideoCodec           string   `json:"videoCodec,omitempty"`
	AudioCodecs          []string `json:"audioCodecs,omitempty"`
	AudioSampleRate      int      `json:"audioSampleRate,omitempty"`
	AudioSampleCount     int64    `json:"audioSampleCount,omitempty"`
	AudioSampleEstimated bool     `json:"audioSampleCountEstimated"`
	AudioChannelCount    int      `json:"audioChannelCount,omitempty"`
}

func ProbeVideo(ctx context.Context, filePath string) (ProbeResult, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "stream=codec_type,codec_name,width,height,avg_frame_rate,r_frame_rate,nb_frames,duration,sample_rate,channels,duration_ts,time_base:format=duration",
		"-of", "json",
		filePath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ProbeResult{}, fmt.Errorf("ffprobe failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return parseProbeOutput(output)
}

func ProbeVideoBytes(ctx context.Context, body []byte, mimeType string) (ProbeResult, error) {
	suffix := probeFileSuffix(mimeType)
	file, err := os.CreateTemp("", "cineweave-video-probe-*"+suffix)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("create probe file: %w", err)
	}
	filePath := file.Name()
	defer os.Remove(filePath)
	if _, err := file.Write(body); err != nil {
		_ = file.Close()
		return ProbeResult{}, fmt.Errorf("write probe file: %w", err)
	}
	if err := file.Close(); err != nil {
		return ProbeResult{}, fmt.Errorf("close probe file: %w", err)
	}
	return ProbeVideo(ctx, filePath)
}

func parseProbeOutput(output []byte) (ProbeResult, error) {
	var decoded struct {
		Streams []struct {
			CodecType    string      `json:"codec_type"`
			CodecName    string      `json:"codec_name"`
			Width        int         `json:"width"`
			Height       int         `json:"height"`
			AvgFrameRate string      `json:"avg_frame_rate"`
			RFrameRate   string      `json:"r_frame_rate"`
			FrameCount   string      `json:"nb_frames"`
			Duration     string      `json:"duration"`
			SampleRate   string      `json:"sample_rate"`
			Channels     int         `json:"channels"`
			DurationTS   probeScalar `json:"duration_ts"`
			TimeBase     string      `json:"time_base"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &decoded); err != nil {
		return ProbeResult{}, fmt.Errorf("decode ffprobe output: %w", err)
	}
	result := ProbeResult{AudioCodecs: []string{}}
	result.DurationSeconds = positiveFloat(decoded.Format.Duration)
	for _, stream := range decoded.Streams {
		switch strings.ToLower(strings.TrimSpace(stream.CodecType)) {
		case "video":
			result.VideoStreamCount++
			if result.VideoStreamCount != 1 {
				continue
			}
			result.Width = stream.Width
			result.Height = stream.Height
			result.VideoCodec = strings.TrimSpace(stream.CodecName)
			if result.DurationSeconds <= 0 {
				result.DurationSeconds = positiveFloat(stream.Duration)
			}
			frameRate := stream.AvgFrameRate
			if !validFrameRate(frameRate) {
				frameRate = stream.RFrameRate
			}
			result.FrameRateNumerator, result.FrameRateDenominator, result.FrameRate = parseFrameRate(frameRate)
			result.FrameCount = positiveInt64(stream.FrameCount)
		case "audio":
			result.AudioStreamCount++
			if codec := strings.TrimSpace(stream.CodecName); codec != "" {
				result.AudioCodecs = append(result.AudioCodecs, codec)
			}
			if result.AudioStreamCount == 1 {
				result.AudioSampleRate = int(positiveInt64(stream.SampleRate))
				result.AudioChannelCount = stream.Channels
				audioDuration := positiveFloat(stream.Duration)
				if audioDuration <= 0 {
					audioDuration = durationFromTimeBase(string(stream.DurationTS), stream.TimeBase)
				}
				if audioDuration <= 0 {
					audioDuration = result.DurationSeconds
				}
				if result.AudioSampleRate > 0 && audioDuration > 0 {
					result.AudioSampleCount = int64(math.Round(audioDuration * float64(result.AudioSampleRate)))
					result.AudioSampleEstimated = result.AudioSampleCount > 0
				}
			}
		}
	}
	result.HasAudio = result.AudioStreamCount > 0
	if result.FrameCount <= 0 && result.DurationSeconds > 0 && result.FrameRate > 0 {
		result.FrameCount = int64(math.Round(result.DurationSeconds * result.FrameRate))
		result.FrameCountEstimated = result.FrameCount > 0
	}
	return result, nil
}

func durationFromTimeBase(durationTS, timeBase string) float64 {
	ticks := positiveInt64(durationTS)
	parts := strings.Split(strings.TrimSpace(timeBase), "/")
	if ticks <= 0 || len(parts) != 2 {
		return 0
	}
	numerator, err := strconv.ParseFloat(parts[0], 64)
	if err != nil || numerator <= 0 {
		return 0
	}
	denominator, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || denominator <= 0 {
		return 0
	}
	return float64(ticks) * numerator / denominator
}

func probeFileSuffix(mimeType string) string {
	if mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(mimeType)); err == nil {
		if extensions, _ := mime.ExtensionsByType(mediaType); len(extensions) > 0 {
			return extensions[0]
		}
	}
	if extension := filepath.Ext(strings.TrimSpace(mimeType)); extension != "" {
		return extension
	}
	return ".mp4"
}

func parseFrameRate(value string) (int64, int64, float64) {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 {
		return 0, 0, 0
	}
	numerator, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || numerator <= 0 {
		return 0, 0, 0
	}
	denominator, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || denominator <= 0 {
		return 0, 0, 0
	}
	return numerator, denominator, float64(numerator) / float64(denominator)
}

func validFrameRate(value string) bool {
	numerator, denominator, _ := parseFrameRate(value)
	return numerator > 0 && denominator > 0
}

func positiveFloat(value string) float64 {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

func positiveInt64(value string) int64 {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}
