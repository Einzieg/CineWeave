package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

const requestIDHeader = "X-Request-Id"

type Envelope struct {
	RequestID string     `json:"requestId"`
	Data      any        `json:"data,omitempty"`
	Meta      any        `json:"meta,omitempty"`
	Error     *ErrorBody `json:"error,omitempty"`
}

type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Details   any    `json:"details,omitempty"`
	Retryable bool   `json:"retryable"`
}

func WriteJSON(w http.ResponseWriter, r *http.Request, status int, data any, meta any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{
		RequestID: RequestID(r),
		Data:      data,
		Meta:      meta,
	})
}

func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string, details any, retryable bool) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{
		RequestID: RequestID(r),
		Error: &ErrorBody{
			Code:      code,
			Message:   message,
			Details:   details,
			Retryable: retryable,
		},
	})
}

func RequestID(r *http.Request) string {
	if requestID := r.Header.Get(requestIDHeader); requestID != "" {
		return requestID
	}
	return randomID()
}

func randomID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req_unknown"
	}
	return "req_" + hex.EncodeToString(b[:])
}
