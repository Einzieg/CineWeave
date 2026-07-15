package observability

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsHandlerExposesRuntimeMetrics(t *testing.T) {
	RecordProviderIdempotency("test_dedupe_hit")
	RecordProviderMediaDownload("test_image", 17, 2)
	RecordWorkflowStart("test_started", time.Millisecond)

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	MetricsHandler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	body := response.Body.String()
	for _, expected := range []string{
		`cineweave_provider_idempotency_outcomes_total{outcome="test_dedupe_hit"} 1`,
		`cineweave_provider_media_download_bytes_total{kind="test_image"} 17`,
		`cineweave_provider_media_redirects_total{kind="test_image"} 2`,
		`cineweave_workflow_start_attempts_total{result="test_started"} 1`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics output missing %q", expected)
		}
	}
}

func TestHTTPMiddlewarePreservesStreamingInterfaces(t *testing.T) {
	underlying := &interfaceWriter{header: make(http.Header)}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := HTTPMiddleware("test", logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Flusher); !ok {
			t.Fatal("http.Flusher was not preserved")
		}
		if _, ok := w.(http.Hijacker); !ok {
			t.Fatal("http.Hijacker was not preserved")
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("ok"))
	}))
	request := httptest.NewRequest(http.MethodGet, "/stream", nil)
	request.Header.Set(requestIDHeader, "request-test")
	handler.ServeHTTP(underlying, request)
	if underlying.status != http.StatusAccepted || underlying.body.String() != "ok" {
		t.Fatalf("response = status %d body %q", underlying.status, underlying.body.String())
	}
}

type interfaceWriter struct {
	header http.Header
	status int
	body   strings.Builder
}

func (w *interfaceWriter) Header() http.Header { return w.header }

func (w *interfaceWriter) WriteHeader(status int) { w.status = status }

func (w *interfaceWriter) Write(value []byte) (int, error) { return w.body.Write(value) }

func (w *interfaceWriter) Flush() {}

func (w *interfaceWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) { return nil, nil, nil }
