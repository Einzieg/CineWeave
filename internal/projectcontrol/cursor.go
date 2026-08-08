package projectcontrol

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

func EncodeCommandCursor(cursor CommandCursor) (string, error) {
	if cursor.CreatedAt.IsZero() || uuid.Validate(strings.TrimSpace(cursor.ID)) != nil {
		return "", fmt.Errorf("command cursor is incomplete")
	}
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode command cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func DecodeCommandCursor(value string) (*CommandCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode command cursor: %w", err)
	}
	var cursor CommandCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return nil, fmt.Errorf("decode command cursor: %w", err)
	}
	if cursor.CreatedAt.IsZero() || uuid.Validate(strings.TrimSpace(cursor.ID)) != nil {
		return nil, fmt.Errorf("command cursor is incomplete")
	}
	return &cursor, nil
}
