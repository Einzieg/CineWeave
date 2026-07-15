package media

import (
	"math"
	"testing"
)

func TestParseProbeOutputCapturesVideoTimingAndAudioStreams(t *testing.T) {
	result, err := parseProbeOutput([]byte(`{
		"streams": [
			{"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"avg_frame_rate":"24000/1001","r_frame_rate":"24/1","nb_frames":"240","duration":"10.010000"},
			{"codec_type":"audio","codec_name":"aac","duration":"10.010000"},
			{"codec_type":"audio","codec_name":"opus","duration":"10.010000"}
		],
		"format":{"duration":"10.010000"}
	}`))
	if err != nil {
		t.Fatalf("parseProbeOutput: %v", err)
	}
	if result.DurationSeconds != 10.01 || result.Width != 1920 || result.Height != 1080 {
		t.Fatalf("dimensions/duration = %+v", result)
	}
	if result.FrameRateNumerator != 24000 || result.FrameRateDenominator != 1001 || math.Abs(result.FrameRate-23.976023976) > 0.000001 {
		t.Fatalf("frame rate = %+v", result)
	}
	if result.FrameCount != 240 || result.FrameCountEstimated {
		t.Fatalf("frame count = %+v", result)
	}
	if result.VideoStreamCount != 1 || result.AudioStreamCount != 2 || !result.HasAudio || result.VideoCodec != "h264" {
		t.Fatalf("stream observation = %+v", result)
	}
	if len(result.AudioCodecs) != 2 || result.AudioCodecs[0] != "aac" || result.AudioCodecs[1] != "opus" {
		t.Fatalf("audio codecs = %#v", result.AudioCodecs)
	}
}

func TestParseProbeOutputEstimatesMissingFrameCount(t *testing.T) {
	result, err := parseProbeOutput([]byte(`{
		"streams":[{"codec_type":"video","codec_name":"hevc","width":1280,"height":720,"avg_frame_rate":"24/1"}],
		"format":{"duration":"5.5"}
	}`))
	if err != nil {
		t.Fatalf("parseProbeOutput: %v", err)
	}
	if result.FrameCount != 132 || !result.FrameCountEstimated {
		t.Fatalf("frame count = %+v, want estimated 132", result)
	}
	if result.HasAudio || result.AudioStreamCount != 0 {
		t.Fatalf("audio observation = %+v", result)
	}
}

func TestParseProbeOutputAcceptsNumericAudioDurationTicks(t *testing.T) {
	result, err := parseProbeOutput([]byte(`{
		"streams":[
			{"codec_type":"video","avg_frame_rate":"24/1"},
			{"codec_type":"audio","codec_name":"aac","sample_rate":"48000","channels":2,"duration_ts":480000,"time_base":"1/48000"}
		],
		"format":{"duration":"10"}
	}`))
	if err != nil {
		t.Fatalf("parseProbeOutput: %v", err)
	}
	if result.AudioSampleRate != 48000 || result.AudioSampleCount != 480000 || !result.AudioSampleEstimated || result.AudioChannelCount != 2 {
		t.Fatalf("audio sample observation = %+v", result)
	}
}
