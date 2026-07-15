package provider

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestStoreGatewayImageMediaRejectsProviderRatioMismatchWithoutWriting(t *testing.T) {
	objectStorage := newMemoryObjectStorage()
	service := &Service{}
	service.SetStorage(objectStorage)
	sourceBody := providerLayoutPNG(t, 160, 100)
	sourceMedia := finalizeGatewayImageMedia(sourceBody, "image/png")

	_, err := service.storeGatewayImageMedia(
		context.Background(),
		"call-1",
		GatewayImageRequest{OrganizationID: "org-1", ProjectID: "project-1"},
		gatewayModelSelection{Model: Model{ID: "model-1"}},
		imageGenerationResult{NormalizedOutput: []byte(`{"status":"succeeded"}`)},
		sourceMedia,
		gatewayImageInput{Prompt: "shot", Size: "1536x864", AspectRatio: "16:9", N: 1},
	)
	standard, ok := StandardErrorFromError(err)
	if !ok || standard.Code != CodeUpstreamOutputMismatch {
		t.Fatalf("error = %#v, standard = %+v", err, standard)
	}
	objectStorage.mu.Lock()
	objectCount := len(objectStorage.objects)
	objectStorage.mu.Unlock()
	if objectCount != 0 {
		t.Fatalf("stored objects = %d, want 0", objectCount)
	}
}

func TestStoreGatewayImageMediaStoresProviderNativeExactRatio(t *testing.T) {
	objectStorage := newMemoryObjectStorage()
	service := &Service{}
	service.SetStorage(objectStorage)
	sourceBody := providerLayoutPNG(t, 160, 90)

	stored, err := service.storeGatewayImageMedia(
		context.Background(),
		"call-1",
		GatewayImageRequest{OrganizationID: "org-1", ProjectID: "project-1"},
		gatewayModelSelection{Model: Model{ID: "model-1"}},
		imageGenerationResult{NormalizedOutput: []byte(`{"status":"succeeded"}`)},
		finalizeGatewayImageMedia(sourceBody, "image/png"),
		gatewayImageInput{Prompt: "shot", Size: "1536x864", AspectRatio: "16:9", N: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Output.Width == nil || stored.Output.Height == nil || *stored.Output.Width != 160 || *stored.Output.Height != 90 {
		t.Fatalf("provider dimensions = %#v/%#v", stored.Output.Width, stored.Output.Height)
	}
	if stored.Output.AspectRatio != "16:9" || !stored.Layout.Validated {
		t.Fatalf("provider layout = %+v / %+v", stored.Output, stored.Layout)
	}
	objectStorage.mu.Lock()
	objectCount := len(objectStorage.objects)
	_, storedNative := objectStorage.objects[stored.Output.StorageKey]
	objectStorage.mu.Unlock()
	if objectCount != 1 || !storedNative {
		t.Fatalf("stored objects = %d native=%v", objectCount, storedNative)
	}
}

func TestParseGatewayImageInputValidatesAspectRatio(t *testing.T) {
	input, err := parseGatewayImageInput([]byte(`{"prompt":"shot","size":"1536x864","aspectRatio":"16:9"}`))
	if err != nil {
		t.Fatal(err)
	}
	if input.AspectRatio != "16:9" {
		t.Fatalf("aspect ratio = %q", input.AspectRatio)
	}
	if _, err := parseGatewayImageInput([]byte(`{"prompt":"shot","aspectRatio":"wide"}`)); err == nil {
		t.Fatal("invalid aspect ratio was accepted")
	}
}

func providerLayoutPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			canvas.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, canvas); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
