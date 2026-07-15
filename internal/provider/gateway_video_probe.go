package provider

import (
	"context"

	mediapkg "github.com/Einzieg/cineweave/internal/media"
)

type VideoMediaProbeFunc func(context.Context, []byte, string) (GatewayVideoMediaProbe, error)
type VideoMediaFileProbeFunc func(context.Context, string) (GatewayVideoMediaProbe, error)

func defaultVideoMediaProbe(ctx context.Context, body []byte, mimeType string) (GatewayVideoMediaProbe, error) {
	probe, err := mediapkg.ProbeVideoBytes(ctx, body, mimeType)
	if err != nil {
		return GatewayVideoMediaProbe{}, err
	}
	return gatewayVideoProbeFromMediaProbe(probe), nil
}

func defaultVideoMediaFileProbe(ctx context.Context, filePath string) (GatewayVideoMediaProbe, error) {
	probe, err := mediapkg.ProbeVideo(ctx, filePath)
	if err != nil {
		return GatewayVideoMediaProbe{}, err
	}
	return gatewayVideoProbeFromMediaProbe(probe), nil
}

func gatewayVideoProbeFromMediaProbe(probe mediapkg.ProbeResult) GatewayVideoMediaProbe {
	return GatewayVideoMediaProbe{
		DurationSeconds:      probe.DurationSeconds,
		Width:                probe.Width,
		Height:               probe.Height,
		FrameRateNumerator:   probe.FrameRateNumerator,
		FrameRateDenominator: probe.FrameRateDenominator,
		FrameRate:            probe.FrameRate,
		FrameCount:           probe.FrameCount,
		FrameCountEstimated:  probe.FrameCountEstimated,
		VideoStreamCount:     probe.VideoStreamCount,
		AudioStreamCount:     probe.AudioStreamCount,
		HasAudio:             probe.HasAudio,
		VideoCodec:           probe.VideoCodec,
		AudioCodecs:          append([]string(nil), probe.AudioCodecs...),
		AudioSampleRate:      probe.AudioSampleRate,
		AudioSampleCount:     probe.AudioSampleCount,
		AudioSampleEstimated: probe.AudioSampleEstimated,
		AudioChannelCount:    probe.AudioChannelCount,
	}
}

func (s *Service) SetVideoMediaProbe(probe VideoMediaProbeFunc) {
	if probe == nil {
		s.videoMediaProbe = defaultVideoMediaProbe
		s.videoMediaFileProbe = defaultVideoMediaFileProbe
		return
	}
	s.videoMediaProbe = probe
	s.videoMediaFileProbe = nil
}
