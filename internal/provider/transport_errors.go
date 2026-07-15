package provider

import (
	"errors"
	"io"
	"strings"
)

func isTransientProviderTransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "eof") ||
		strings.Contains(message, "connection reset") ||
		strings.Contains(message, "server closed idle connection") ||
		strings.Contains(message, "connection was closed")
}
