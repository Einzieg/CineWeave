package provider

import (
	"encoding/json"
	"strings"
)

func gatewayAttemptFromCall(call CallLog, selection gatewayModelSelection, standard *StandardError, latencyMS int) GatewayAttempt {
	attempt := GatewayAttempt{
		ProviderCallID:        call.ID,
		ProviderModelID:       selection.Model.ID,
		ProviderAccountID:     selection.Account.ID,
		ModelProfileBindingID: selection.ModelProfileBindingID,
		Status:                call.Status,
		LatencyMS:             latencyMS,
	}
	if standard != nil {
		attempt.ErrorCode = standard.Code
		attempt.ErrorMessage = standard.Message
		attempt.Retryable = standard.Retryable
	} else {
		if call.ErrorCode != nil {
			attempt.ErrorCode = *call.ErrorCode
		}
		if call.ErrorMessage != nil {
			attempt.ErrorMessage = *call.ErrorMessage
		}
	}
	return attempt
}

func gatewayErrorCode(responseError *StandardError) string {
	if responseError == nil {
		return ""
	}
	return strings.TrimSpace(responseError.Code)
}

func withRoutingNormalizedOutput(raw json.RawMessage, selection gatewayModelSelection, attemptIndex, maxAttempts int, selectedBy string) json.RawMessage {
	routing := map[string]any{
		"modelProfileKey": selection.ModelProfileKey,
		"bindingId":       selection.ModelProfileBindingID,
		"attemptIndex":    attemptIndex,
		"maxAttempts":     maxAttempts,
		"selectedBy":      selectedBy,
	}
	var decoded map[string]any
	if len(raw) > 0 && json.Unmarshal(raw, &decoded) == nil && decoded != nil {
		decoded["routing"] = routing
		return mustJSON(decoded)
	}
	return mustJSON(map[string]any{
		"value":   rawJSONValue(raw),
		"routing": routing,
	})
}
