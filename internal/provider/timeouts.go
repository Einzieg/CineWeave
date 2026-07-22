package provider

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultOpenAICompatibleTimeoutMS    = 30000
	defaultOpenAIVideoPollTimeoutMS     = 30000
	defaultGatewayTextTimeoutMS         = 10 * 60 * 1000
	defaultGatewayImageTimeoutMS        = 10 * 60 * 1000
	defaultGatewayImageRequestTimeoutMS = 20 * 60 * 1000
	defaultGatewayVideoMediaTimeoutMS   = 10 * 60 * 1000
	defaultGatewayClientTimeoutMS       = 21 * 60 * 1000
)

func gatewayTextTimeoutMSFromEnv() int {
	return envDurationMilliseconds("CINEWEAVE_PROVIDER_TEXT_TIMEOUT_MS", defaultGatewayTextTimeoutMS)
}

func gatewayImageTimeoutMSFromEnv() int {
	return envDurationMilliseconds("CINEWEAVE_PROVIDER_IMAGE_TIMEOUT_MS", defaultGatewayImageTimeoutMS)
}

func gatewayImageRequestTimeoutMSFromEnv() int {
	return envDurationMilliseconds("CINEWEAVE_PROVIDER_IMAGE_REQUEST_TIMEOUT_MS", defaultGatewayImageRequestTimeoutMS)
}

func gatewayAudioTimeoutMSFromEnv() int {
	return envDurationMilliseconds("CINEWEAVE_PROVIDER_AUDIO_TIMEOUT_MS", 10*60*1000)
}

func gatewayVideoMediaTimeoutFromEnv() time.Duration {
	return time.Duration(envDurationMilliseconds("CINEWEAVE_PROVIDER_VIDEO_MEDIA_TIMEOUT_MS", defaultGatewayVideoMediaTimeoutMS)) * time.Millisecond
}

func gatewayClientTimeoutFromEnv() time.Duration {
	return time.Duration(envDurationMilliseconds("CINEWEAVE_PROVIDER_GATEWAY_CLIENT_TIMEOUT_MS", defaultGatewayClientTimeoutMS)) * time.Millisecond
}

func envDurationMilliseconds(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	if ms, err := strconv.Atoi(value); err == nil && ms > 0 {
		return ms
	}
	if duration, err := time.ParseDuration(value); err == nil && duration > 0 {
		return int(duration / time.Millisecond)
	}
	return fallback
}
