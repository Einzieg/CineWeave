package provider

import (
	"fmt"
	"math"
	"strings"
)

const imageAspectRatioTolerance = 0.001

func validateImageOutputLayout(expectedAspectRatio string, width, height int) error {
	if strings.TrimSpace(expectedAspectRatio) == "" {
		return nil
	}
	expected, ok := parseVideoAspectRatio(expectedAspectRatio)
	if !ok {
		return &StandardErrorError{Standard: StandardError{
			Code:      CodeInvalidRequest,
			Message:   "requested image aspect ratio is invalid",
			Retryable: false,
		}}
	}
	if width <= 0 || height <= 0 {
		return &StandardErrorError{Standard: StandardError{
			Code:      CodeUpstreamOutputMismatch,
			Message:   "provider image dimensions could not be determined",
			Retryable: true,
		}}
	}
	actual := float64(width) / float64(height)
	if math.Abs(actual-expected)/expected <= imageAspectRatioTolerance {
		return nil
	}
	return &StandardErrorError{Standard: StandardError{
		Code: CodeUpstreamOutputMismatch,
		Message: fmt.Sprintf(
			"provider returned image layout %dx%d, expected aspect ratio %s; no local crop or resize was applied",
			width,
			height,
			strings.TrimSpace(expectedAspectRatio),
		),
		Retryable: true,
	}}
}
