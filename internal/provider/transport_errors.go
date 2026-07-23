package provider

import (
	"errors"
	"io"
	"net"
	"strings"
)

func isProviderTransportTimeout(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "tls handshake timeout") ||
		strings.Contains(message, "i/o timeout") ||
		strings.Contains(message, "connection timed out")
}

func isTransientProviderTransportError(err error) bool {
	if err == nil {
		return false
	}
	if isProviderTransportTimeout(err) {
		return true
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
