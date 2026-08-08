package api

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

type projectControlOffsetCursor struct {
	Version int `json:"v"`
	Offset  int `json:"offset"`
}

func encodeProjectControlOffsetCursor(offset int) (string, error) {
	payload, err := json.Marshal(projectControlOffsetCursor{Version: 1, Offset: offset})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeProjectControlOffsetCursor(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return 0, controlValidationError("cursor 无效")
	}
	var cursor projectControlOffsetCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.Version != 1 || cursor.Offset < 0 {
		return 0, controlValidationError("cursor 无效")
	}
	return cursor.Offset, nil
}

func normalizeProjectControlPageLimit(value, defaultValue, maximum int) (int, error) {
	if value == 0 {
		return defaultValue, nil
	}
	if value < 1 || value > maximum {
		return 0, controlValidationError("limit 超出允许范围")
	}
	return value, nil
}
