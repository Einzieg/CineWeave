package outbound

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMediaFetcherPinsValidatedDNSAddress(t *testing.T) {
	pngBody := []byte("\x89PNG\r\n\x1a\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "media.example" {
			t.Fatalf("Host = %q, want media.example", r.Host)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBody)
	}))
	defer server.Close()

	resolver := &sequenceResolver{answers: [][]netip.Addr{
		{netip.MustParseAddr("203.0.113.10")},
		{netip.MustParseAddr("127.0.0.1")},
	}}
	dialer := &mappingDialer{target: server.Listener.Addr().String()}
	fetcher := NewMediaFetcher(Config{Resolver: resolver, Dialer: dialer})

	result, err := fetcher.FetchToTempFile(context.Background(), "http://media.example/image.png", FetchOptions{
		Kind:                "image",
		MaxBytes:            1024,
		Timeout:             time.Second,
		AllowedMIMEPrefixes: []string{"image/"},
	})
	if err != nil {
		t.Fatalf("FetchToTempFile() error = %v", err)
	}
	defer result.Close()
	if resolver.calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", resolver.calls)
	}
	if got := dialer.lastAddress(); !strings.HasPrefix(got, "203.0.113.10:") {
		t.Fatalf("dial address = %q, want pinned public address", got)
	}
	if body, readErr := os.ReadFile(result.Path); readErr != nil || string(body) != string(pngBody) {
		t.Fatalf("downloaded body = %q, error = %v", body, readErr)
	}
}

func TestMediaFetcherRejectsPrivateAddressAfterRedirect(t *testing.T) {
	privateServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("private redirect target must not be requested")
	}))
	defer privateServer.Close()
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, privateServer.URL+"/secret", http.StatusFound)
	}))
	defer redirectServer.Close()

	fetcher := NewMediaFetcher(Config{
		Resolver: staticResolver{addresses: []netip.Addr{netip.MustParseAddr("203.0.113.11")}},
		Dialer:   &mappingDialer{target: redirectServer.Listener.Addr().String()},
	})
	_, err := fetcher.FetchToTempFile(context.Background(), "http://media.example/start", FetchOptions{
		Kind:     "video",
		MaxBytes: 1024,
		Timeout:  time.Second,
	})
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("FetchToTempFile() error = %v, want ErrBlockedAddress", err)
	}
}

func TestMediaFetcherRejectsAnyPrivateDNSAnswer(t *testing.T) {
	fetcher := NewMediaFetcher(Config{
		Resolver: staticResolver{addresses: []netip.Addr{
			netip.MustParseAddr("203.0.113.12"),
			netip.MustParseAddr("100.64.0.4"),
		}},
		Dialer: &mappingDialer{target: "127.0.0.1:1"},
	})
	_, err := fetcher.FetchToTempFile(context.Background(), "http://media.example/file", FetchOptions{
		MaxBytes: 1024,
		Timeout:  time.Second,
	})
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("FetchToTempFile() error = %v, want ErrBlockedAddress", err)
	}
}

func TestMediaFetcherAllowsExplicitHostAndCIDRPair(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("\x00\x00\x00\x18ftypmp42"))
	}))
	defer server.Close()

	fetcher := NewMediaFetcher(Config{
		Resolver: staticResolver{addresses: []netip.Addr{netip.MustParseAddr("10.20.30.40")}},
		Dialer:   &mappingDialer{target: server.Listener.Addr().String()},
	})
	result, err := fetcher.FetchToTempFile(context.Background(), "http://private-media.example/video.mp4", FetchOptions{
		Kind:                "video",
		MaxBytes:            1024,
		Timeout:             time.Second,
		AllowedMIMEPrefixes: []string{"video/"},
		Policy: RequestPolicy{
			AllowedPrivateHosts: []string{"private-media.example"},
			AllowedPrivateCIDRs: []netip.Prefix{netip.MustParsePrefix("10.20.30.0/24")},
		},
	})
	if err != nil {
		t.Fatalf("FetchToTempFile() error = %v", err)
	}
	defer result.Close()
}

func TestMediaFetcherRejectsOversizedResponseAndRemovesTempFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", "9")
		_, _ = w.Write([]byte("123456789"))
	}))
	defer server.Close()

	tempDir := t.TempDir()
	fetcher := NewMediaFetcher(Config{
		Resolver: staticResolver{addresses: []netip.Addr{netip.MustParseAddr("203.0.113.13")}},
		Dialer:   &mappingDialer{target: server.Listener.Addr().String()},
		TempDir:  tempDir,
	})
	_, err := fetcher.FetchToTempFile(context.Background(), "http://media.example/video.mp4", FetchOptions{
		Kind:     "video",
		MaxBytes: 8,
		Timeout:  time.Second,
	})
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("FetchToTempFile() error = %v, want ErrResponseTooLarge", err)
	}
	entries, readErr := os.ReadDir(tempDir)
	if readErr != nil {
		t.Fatalf("ReadDir() error = %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary files = %d, want 0", len(entries))
	}
}

func TestMediaFetcherRejectsDeclaredImageWithTextBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("this is not an image"))
	}))
	defer server.Close()

	fetcher := NewMediaFetcher(Config{
		Resolver: staticResolver{addresses: []netip.Addr{netip.MustParseAddr("203.0.113.14")}},
		Dialer:   &mappingDialer{target: server.Listener.Addr().String()},
	})
	_, err := fetcher.FetchToTempFile(context.Background(), "http://media.example/image.png", FetchOptions{
		Kind:                "image",
		MaxBytes:            1024,
		Timeout:             time.Second,
		AllowedMIMEPrefixes: []string{"image/"},
	})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("FetchToTempFile() error = %v, want MIME mismatch", err)
	}
}

func TestMediaFetcherSpoolsTrustedAudioBodyWithSameLimits(t *testing.T) {
	fetcher := NewMediaFetcher(Config{TempDir: t.TempDir()})
	result, err := fetcher.SpoolToTempFile(context.Background(), strings.NewReader("RIFF\x04\x00\x00\x00WAVE"), FetchOptions{
		Kind:                "audio",
		MaxBytes:            1024,
		Timeout:             time.Second,
		UpstreamMIMEType:    "audio/wav",
		AllowedMIMEPrefixes: []string{"audio/"},
	})
	if err != nil {
		t.Fatalf("SpoolToTempFile() error = %v", err)
	}
	if result.ByteSize != 12 || !strings.HasPrefix(result.ContentHash, "sha256:") {
		t.Fatalf("result = %+v", result)
	}
	path := result.Path
	if err := result.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary file still exists: %v", err)
	}
}

type staticResolver struct {
	addresses []netip.Addr
}

func (r staticResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return append([]netip.Addr(nil), r.addresses...), nil
}

type sequenceResolver struct {
	answers [][]netip.Addr
	calls   int
}

func (r *sequenceResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	if r.calls >= len(r.answers) {
		return nil, fmt.Errorf("unexpected lookup %d", r.calls+1)
	}
	answer := append([]netip.Addr(nil), r.answers[r.calls]...)
	r.calls++
	return answer, nil
}

type mappingDialer struct {
	target    string
	mu        sync.Mutex
	addresses []string
}

func (d *mappingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d.mu.Lock()
	d.addresses = append(d.addresses, address)
	d.mu.Unlock()
	return (&net.Dialer{}).DialContext(ctx, network, d.target)
}

func (d *mappingDialer) lastAddress() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.addresses) == 0 {
		return ""
	}
	return d.addresses[len(d.addresses)-1]
}
