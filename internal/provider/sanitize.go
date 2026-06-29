package provider

import (
	"encoding/json"
	"strings"
)

var sensitiveKeyFragments = []string{
	"authorization",
	"cookie",
	"api_key",
	"apikey",
	"api-key",
	"token",
	"secret",
	"password",
	"credential",
	"access_token",
	"refresh_token",
}

func SanitizeRawJSON(raw json.RawMessage, fallback string) (json.RawMessage, error) {
	normalized, err := normalizeJSON(raw, fallback)
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(normalized, &value); err != nil {
		return nil, err
	}
	sanitized := redactValue(value)
	output, err := json.Marshal(sanitized)
	if err != nil {
		return nil, err
	}
	return output, nil
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if isSensitiveKey(key) {
				out[key] = "***redacted***"
				continue
			}
			out[key] = redactValue(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, redactValue(item))
		}
		return out
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, fragment := range sensitiveKeyFragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}
