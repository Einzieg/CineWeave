package observability

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

const requestIDHeader = "X-Request-Id"

func HTTPMiddleware(service string, logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := strings.TrimSpace(r.Header.Get(requestIDHeader))
		requestLogger := logger
		if requestLogger != nil {
			requestLogger = requestLogger.With("requestId", requestID)
			r = r.WithContext(WithLogger(r.Context(), requestLogger))
		}
		recorder := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		if recorder.status == 0 {
			recorder.status = http.StatusOK
		}
		duration := time.Since(started)
		RecordHTTPRequest(service, r.Method, recorder.status, duration)

		if requestLogger == nil {
			return
		}
		route := strings.TrimSpace(r.Pattern)
		if route == "" {
			route = "unmatched"
		}
		args := []any{
			"method", r.Method,
			"route", route,
			"status", recorder.status,
			"durationMs", duration.Milliseconds(),
			"responseBytes", recorder.bytes,
		}
		if route == "/healthz" || route == "/readyz" || route == "/metrics" {
			requestLogger.DebugContext(r.Context(), "http request completed", args...)
			return
		}
		requestLogger.InfoContext(r.Context(), "http request completed", args...)
	})
}

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *responseRecorder) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseRecorder) Write(content []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	written, err := w.ResponseWriter.Write(content)
	w.bytes += int64(written)
	return written, err
}

func (w *responseRecorder) Flush() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func (w *responseRecorder) Push(target string, options *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, options)
}

func (w *responseRecorder) ReadFrom(reader io.Reader) (int64, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if readerFrom, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		written, err := readerFrom.ReadFrom(reader)
		w.bytes += written
		return written, err
	}
	return io.Copy(struct{ io.Writer }{w}, reader)
}

func (w *responseRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
