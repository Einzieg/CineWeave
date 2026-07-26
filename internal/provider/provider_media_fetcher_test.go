package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/provider/outbound"
)

func TestProviderMediaRequestPolicyRequiresHostAndCIDR(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"mediaEgress":{"allowedPrivateHosts":["media.internal"]}}`),
		json.RawMessage(`{"mediaEgress":{"allowedPrivateCidrs":["10.0.0.0/8"]}}`),
	} {
		if _, err := providerMediaRequestPolicy(raw); err == nil {
			t.Fatalf("providerMediaRequestPolicy(%s) error = nil", raw)
		}
	}
}

func TestNormalizedGatewayMediaFailureReportsStorageStage(t *testing.T) {
	err := gatewayMediaStageFailure("storage", errors.New("connection reset"))
	code, message, standard := normalizedGatewayMediaFailure(err, "video")
	if code != CodeMediaDownloadFailed || message != "provider video object storage upload failed" || standard == nil || !standard.Retryable {
		t.Fatalf("failure = %s %q %#v", code, message, standard)
	}
}

func TestNormalizedGatewayMediaFailureReportsDownloadTimeout(t *testing.T) {
	err := gatewayMediaStageFailure("download", context.DeadlineExceeded)
	code, message, standard := normalizedGatewayMediaFailure(err, "video")
	if code != CodeMediaDownloadFailed || message != "provider video download timed out" || standard == nil || !standard.Retryable {
		t.Fatalf("failure = %s %q %#v", code, message, standard)
	}
}

func TestProviderMediaRequestPolicyParsesExactHostAndCIDR(t *testing.T) {
	policy, err := providerMediaRequestPolicy(json.RawMessage(`{
		"mediaEgress": {
			"allowedPrivateHosts": ["media.internal.example"],
			"allowedPrivateCidrs": ["10.20.30.44/24"]
		}
	}`))
	if err != nil {
		t.Fatalf("providerMediaRequestPolicy() error = %v", err)
	}
	if len(policy.AllowedPrivateHosts) != 1 || policy.AllowedPrivateHosts[0] != "media.internal.example" {
		t.Fatalf("hosts = %#v", policy.AllowedPrivateHosts)
	}
	if len(policy.AllowedPrivateCIDRs) != 1 || policy.AllowedPrivateCIDRs[0].String() != "10.20.30.0/24" {
		t.Fatalf("CIDRs = %#v", policy.AllowedPrivateCIDRs)
	}
}

func TestProviderMediaRequestPolicyRejectsInvalidCIDR(t *testing.T) {
	_, err := providerMediaRequestPolicy(json.RawMessage(`{
		"mediaEgress": {
			"allowedPrivateHosts": ["media.internal.example"],
			"allowedPrivateCidrs": ["not-a-cidr"]
		}
	}`))
	if err == nil {
		t.Fatal("providerMediaRequestPolicy() error = nil")
	}
}

func TestNormalizedGatewayMediaFailureDoesNotRetryBlockedURL(t *testing.T) {
	code, message, standard := normalizedGatewayMediaFailure(outbound.ErrBlockedAddress, "image")
	if code != CodeInvalidRequest || standard == nil || standard.Retryable {
		t.Fatalf("failure = %s %q %#v", code, message, standard)
	}
	if message != "provider returned a blocked image URL" {
		t.Fatalf("message = %q", message)
	}
}

func TestDownloadGatewayVideoURLUsesAuthenticatedNewAPIContentProxy(t *testing.T) {
	videoBody := []byte("\x00\x00\x00\x18ftypmp42")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/videos/task_123/content" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer channel-token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(videoBody)
	}))
	defer server.Close()

	service := &Service{}
	service.SetMediaFetcher(outbound.NewMediaFetcher(outbound.Config{}))
	baseURL := server.URL
	media, err := service.downloadGatewayVideoURL(context.Background(), gatewayModelSelection{
		Account: Account{
			BaseURL:  &baseURL,
			AuthType: "api_key",
			Config:   json.RawMessage(`{"runtime":"openai_compatible","videoProtocol":"new_api"}`),
		},
		APIKey: "channel-token",
	}, "task_123", "https://api.example/v1/videos/task_123/content", "video/mp4", time.Second)
	if err != nil {
		t.Fatalf("downloadGatewayVideoURL() error = %v", err)
	}
	defer media.close()
	if media.ByteSize != int64(len(videoBody)) || media.MimeType != "video/mp4" {
		t.Fatalf("media = %+v", media)
	}
}

func TestGatewayVideoMediaRequestDoesNotSendCredentialsToUnrelatedURL(t *testing.T) {
	baseURL := "http://new-api:3000"
	fetchURL, headers, authenticatedOrigin, err := gatewayVideoMediaRequest(gatewayModelSelection{
		Account: Account{
			BaseURL:  &baseURL,
			AuthType: "api_key",
			Config:   json.RawMessage(`{"runtime":"openai_compatible","videoProtocol":"new_api"}`),
		},
		APIKey: "channel-token",
	}, "task_123", "https://cdn.example/video.mp4")
	if err != nil {
		t.Fatalf("gatewayVideoMediaRequest() error = %v", err)
	}
	if fetchURL != "https://cdn.example/video.mp4" || len(headers) != 0 || authenticatedOrigin != "" {
		t.Fatalf("request = %q %#v %q", fetchURL, headers, authenticatedOrigin)
	}
}
