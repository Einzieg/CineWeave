package provider

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const videoAspectRatioTolerance = 0.05

func validateVideoOutputLayout(expectedAspectRatio string, width, height int) error {
	expected, ok := parseVideoAspectRatio(expectedAspectRatio)
	if !ok || width <= 0 || height <= 0 {
		return nil
	}
	actual := float64(width) / float64(height)
	if math.Abs(actual-expected)/expected <= videoAspectRatioTolerance {
		return nil
	}
	standard := StandardError{
		Code:      CodeUpstreamOutputMismatch,
		Message:   fmt.Sprintf("provider returned video layout %dx%d, expected aspect ratio %s", width, height, strings.TrimSpace(expectedAspectRatio)),
		Retryable: true,
	}
	return &StandardErrorError{Standard: standard}
}

func parseVideoAspectRatio(value string) (float64, bool) {
	parts := strings.FieldsFunc(strings.TrimSpace(value), func(r rune) bool {
		return r == ':' || r == '/' || r == 'x' || r == '*'
	})
	if len(parts) != 2 {
		return 0, false
	}
	width, widthErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	height, heightErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return 0, false
	}
	return width / height, true
}

func parseVideoDimensions(value string) (int, int, bool) {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(value)), func(r rune) bool {
		return r == 'x' || r == '*'
	})
	if len(parts) != 2 {
		return 0, 0, false
	}
	width, widthErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, heightErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

func videoLayoutFailureOutput(raw json.RawMessage, standard *StandardError, requestedSize, providerSize string) json.RawMessage {
	output := map[string]any{}
	_ = json.Unmarshal(raw, &output)
	output["status"] = "failed"
	if standard != nil {
		output["errorCode"] = standard.Code
		output["errorMessage"] = standard.Message
	}
	if requestedSize = strings.TrimSpace(requestedSize); requestedSize != "" {
		output["requestedSize"] = requestedSize
	}
	if providerSize = strings.TrimSpace(providerSize); providerSize != "" {
		output["providerSize"] = providerSize
	}
	return mustJSON(output)
}
