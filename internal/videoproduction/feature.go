package videoproduction

import (
	"os"
	"strings"
)

const FeatureFlagEnvironmentVariable = "VIDEO_PRODUCTION_PROFILES_ENABLED"

func FeatureEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(FeatureFlagEnvironmentVariable))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
