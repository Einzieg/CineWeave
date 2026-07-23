package httpx

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
)

type requestIDContextKey struct{}

func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get(requestIDHeader)
		if requestID == "" {
			requestID = randomID()
			r.Header.Set(requestIDHeader, requestID)
		}
		w.Header().Set(requestIDHeader, requestID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, requestID)))
	})
}

func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	requestID, _ := ctx.Value(requestIDContextKey{}).(string)
	return requestID
}

func WithRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.ErrorContext(
					r.Context(),
					"http handler panic",
					"requestId", RequestIDFromContext(r.Context()),
					"method", r.Method,
					"path", r.URL.Path,
					"panic", fmt.Sprint(recovered),
					"stack", string(debug.Stack()),
				)
				WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "服务器内部错误，请稍后重试", nil, false)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func WithCORS(next http.Handler) http.Handler {
	allowedOrigins := corsAllowedOrigins()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if originAllowed(origin, allowedOrigins) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Cache-Control, Content-Type, Idempotency-Key, If-Match, X-Organization-Id, X-Request-Id, Last-Event-ID")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Expose-Headers", "X-Request-Id, X-CineWeave-Stream-High-Watermark")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func corsAllowedOrigins() []string {
	raw := strings.TrimSpace(os.Getenv("CINEWEAVE_CORS_ORIGINS"))
	if raw == "" {
		return []string{
			"http://localhost:19285",
			"http://127.0.0.1:19285",
		}
	}
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		if origin := strings.TrimSpace(part); origin != "" {
			origins = append(origins, origin)
		}
	}
	return origins
}

func originAllowed(origin string, allowedOrigins []string) bool {
	if origin == "" {
		return false
	}
	for _, allowed := range allowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	return false
}

func HealthHandler(service string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
			return
		}
		WriteJSON(w, r, http.StatusOK, map[string]string{
			"service": service,
			"status":  "ok",
		}, nil)
	}
}

type ReadinessCheck func(context.Context) error

func ReadyHandler(service string, checks map[string]ReadinessCheck) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
			return
		}
		statuses := make(map[string]string, len(checks))
		ready := true
		for name, check := range checks {
			if check == nil {
				statuses[name] = "not_configured"
				ready = false
				continue
			}
			if err := check(r.Context()); err != nil {
				statuses[name] = err.Error()
				ready = false
				continue
			}
			statuses[name] = "ok"
		}
		status := http.StatusOK
		state := "ready"
		if !ready {
			status = http.StatusServiceUnavailable
			state = "not_ready"
		}
		WriteJSON(w, r, status, map[string]any{
			"service": service,
			"status":  state,
			"checks":  statuses,
		}, nil)
	}
}

func NotImplemented(feature string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, http.StatusNotImplemented, "NOT_IMPLEMENTED", feature+" is not implemented yet", nil, false)
	}
}
