package provider

import (
	"context"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/storage"
)

func TestHydrateGatewayReferencesPresignsStoredMedia(t *testing.T) {
	service := &Service{objectStorage: referencePresignStorage{}}

	imageReferences, err := service.hydrateGatewayImageReferences(context.Background(), "org-1", []GatewayImageReference{{
		Type:       "image",
		StorageKey: "references/character.png",
	}})
	if err != nil {
		t.Fatalf("hydrateGatewayImageReferences: %v", err)
	}
	if len(imageReferences) != 1 || imageReferences[0].URL != "https://media.example/references/character.png" {
		t.Fatalf("image references = %+v", imageReferences)
	}

	videoReferences, err := service.hydrateGatewayVideoReferences(context.Background(), "org-1", []GatewayVideoReference{{
		Type:       "first_frame",
		StorageKey: "shots/shot-1.png",
	}})
	if err != nil {
		t.Fatalf("hydrateGatewayVideoReferences: %v", err)
	}
	if len(videoReferences) != 1 || videoReferences[0].URL != "https://media.example/shots/shot-1.png" {
		t.Fatalf("video references = %+v", videoReferences)
	}
}

func TestValidateOutboundProviderReferenceURL(t *testing.T) {
	t.Setenv("CINEWEAVE_ALLOW_PRIVATE_PROVIDER_REFERENCE_URLS", "false")
	for _, rawURL := range []string{
		"http://minio:9000/cineweave/frame.png",
		"http://localhost:19290/cineweave/frame.png",
		"http://127.0.0.1:19290/cineweave/frame.png",
		"http://10.0.0.12/frame.png",
	} {
		if err := validateOutboundProviderReferenceURL(rawURL, "video"); err == nil {
			t.Fatalf("validateOutboundProviderReferenceURL(%q) expected error", rawURL)
		}
	}
	if err := validateOutboundProviderReferenceURL("https://media.example/frame.png", "video"); err != nil {
		t.Fatalf("public reference rejected: %v", err)
	}

	t.Setenv("CINEWEAVE_ALLOW_PRIVATE_PROVIDER_REFERENCE_URLS", "true")
	if err := validateOutboundProviderReferenceURL("http://minio:9000/cineweave/frame.png", "video"); err != nil {
		t.Fatalf("private reference override rejected: %v", err)
	}
}

type referencePresignStorage struct{}

func (referencePresignStorage) PutBytes(context.Context, string, []byte, string) (storage.PutResult, error) {
	return storage.PutResult{}, nil
}

func (referencePresignStorage) PutFile(context.Context, string, string, string) (storage.PutResult, error) {
	return storage.PutResult{}, nil
}

func (referencePresignStorage) PresignGetObject(_ context.Context, key string, _ time.Duration) (storage.PresignedGetResult, error) {
	return storage.PresignedGetResult{StorageKey: key, URL: "https://media.example/" + key}, nil
}
