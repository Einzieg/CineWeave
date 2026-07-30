package provider

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const videoAspectRatioTolerance = 0.05

const GatewayVideoWarningCodeLayoutMismatch = "PROVIDER_VIDEO_LAYOUT_MISMATCH"

func detectVideoOutputLayoutWarning(expectedAspectRatio string, width, height int) *GatewayVideoOutputWarning {
	expected, ok := parseVideoAspectRatio(expectedAspectRatio)
	if !ok || width <= 0 || height <= 0 {
		return nil
	}
	actual := float64(width) / float64(height)
	if math.Abs(actual-expected)/expected <= videoAspectRatioTolerance {
		return nil
	}
	return &GatewayVideoOutputWarning{
		Code:                GatewayVideoWarningCodeLayoutMismatch,
		Message:             fmt.Sprintf("provider returned video layout %dx%d, expected aspect ratio %s", width, height, strings.TrimSpace(expectedAspectRatio)),
		Category:            "provider_capability",
		ExpectedAspectRatio: strings.TrimSpace(expectedAspectRatio),
		ActualAspectRatio:   fmt.Sprintf("%d:%d", width, height),
		ProviderSize:        fmt.Sprintf("%dx%d", width, height),
		Width:               width,
		Height:              height,
	}
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

func videoLayoutWarningOutput(raw json.RawMessage, warning *GatewayVideoOutputWarning, requestedSize, providerSize string) json.RawMessage {
	output := map[string]any{}
	_ = json.Unmarshal(raw, &output)
	if warning == nil {
		delete(output, "requestedSize")
		delete(output, "providerSize")
	}
	if requestedSize = strings.TrimSpace(requestedSize); requestedSize != "" {
		output["requestedSize"] = requestedSize
		if warning != nil {
			warning.RequestedSize = requestedSize
		}
	}
	if providerSize = strings.TrimSpace(providerSize); providerSize != "" {
		output["providerSize"] = providerSize
		if warning != nil {
			warning.ProviderSize = providerSize
		}
	}
	warnings := videoOutputWarnings(raw)
	warnings = removeVideoOutputWarning(warnings, GatewayVideoWarningCodeLayoutMismatch)
	if warning != nil {
		output["warnings"] = append(warnings, *warning)
	} else if len(warnings) > 0 {
		output["warnings"] = warnings
	} else {
		delete(output, "warnings")
	}
	return mustJSON(output)
}

func videoOutputWarnings(raw json.RawMessage) []GatewayVideoOutputWarning {
	var output struct {
		Warnings []GatewayVideoOutputWarning `json:"warnings"`
	}
	if err := json.Unmarshal(raw, &output); err != nil {
		return nil
	}
	return append([]GatewayVideoOutputWarning(nil), output.Warnings...)
}

func removeVideoOutputWarning(warnings []GatewayVideoOutputWarning, code string) []GatewayVideoOutputWarning {
	filtered := make([]GatewayVideoOutputWarning, 0, len(warnings))
	for _, warning := range warnings {
		if warning.Code != code {
			filtered = append(filtered, warning)
		}
	}
	return filtered
}
