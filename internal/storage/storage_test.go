package storage

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGetObjectDownloadsConcurrentRanges(t *testing.T) {
	want := bytes.Repeat([]byte("cineweave-range-download-"), 200000)
	var rangeRequests atomic.Int32
	server := newRangeObjectServer(t, want, &rangeRequests)
	defer server.Close()
	client, err := New(context.Background(), Config{
		Endpoint: server.URL, Region: "us-east-1", Bucket: "test-bucket",
		AccessKeyID: "test", SecretAccessKey: "test", UsePathStyle: true,
		DownloadPartSize: 256 << 10, DownloadConcurrency: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, contentType, err := client.GetObject(context.Background(), "path/object.bin", int64(len(want)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("downloaded body does not match: got %d bytes, want %d", len(got), len(want))
	}
	if contentType != "application/octet-stream" {
		t.Fatalf("content type = %q", contentType)
	}
	if rangeRequests.Load() < 2 {
		t.Fatalf("range requests = %d, want multiple parts", rangeRequests.Load())
	}
}

func TestGetObjectRejectsOversizedObjectBeforeDownload(t *testing.T) {
	want := bytes.Repeat([]byte("x"), 4096)
	var rangeRequests atomic.Int32
	server := newRangeObjectServer(t, want, &rangeRequests)
	defer server.Close()
	client, err := New(context.Background(), Config{
		Endpoint: server.URL, Region: "us-east-1", Bucket: "test-bucket",
		AccessKeyID: "test", SecretAccessKey: "test", UsePathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.GetObject(context.Background(), "path/object.bin", int64(len(want)-1)); err == nil || !strings.Contains(err.Error(), "exceeds maxBytes") {
		t.Fatalf("error = %v, want maxBytes rejection", err)
	}
	if rangeRequests.Load() != 0 {
		t.Fatalf("range requests = %d, want 0", rangeRequests.Load())
	}
}

func TestStorageHTTPClientDoesNotInheritEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:65530")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:65530")
	client, err := newStorageHTTPClient("")
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("storage transport must not inherit the provider proxy")
	}
}

func TestStorageHTTPClientUsesExplicitProxy(t *testing.T) {
	client, err := newStorageHTTPClient("http://proxy.example:8080")
	if err != nil {
		t.Fatal(err)
	}
	transport := client.Transport.(*http.Transport)
	req := httptest.NewRequest(http.MethodGet, "https://storage.example/object", nil)
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatal(err)
	}
	if proxyURL == nil || proxyURL.String() != "http://proxy.example:8080" {
		t.Fatalf("proxy URL = %v", proxyURL)
	}
}

func newRangeObjectServer(t *testing.T, body []byte, rangeRequests *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/test-bucket/path/object.bin" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("ETag", `"test-etag"`)
		w.Header().Set("Content-Type", "application/octet-stream")
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			var start, end int
			if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil || start < 0 || end < start || end >= len(body) {
				http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
				return
			}
			rangeRequests.Add(1)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(body)))
			w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(body[start : end+1])
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
}
