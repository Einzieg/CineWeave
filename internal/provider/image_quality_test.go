package provider

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestValidateGatewayImageVisualQualityRejectsLargeBlackBottomBand(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 320, 180))
	for y := 0; y < 90; y++ {
		for x := 0; x < 320; x++ {
			source.Set(x, y, color.RGBA{R: uint8(40 + x%160), G: uint8(60 + y), B: 120, A: 255})
		}
	}
	media := gatewayImageMedia{Body: encodeQualityTestPNG(t, source), MimeType: "image/png"}
	err := validateGatewayImageVisualQuality(media)
	standard, ok := StandardErrorFromError(err)
	if !ok || standard.Code != CodeUpstreamOutputMismatch || !standard.Retryable {
		t.Fatalf("quality error = %#v, %v", standard, err)
	}
}

func TestValidateGatewayImageVisualQualityAllowsDarkDetailedImage(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 320, 180))
	for y := 0; y < 180; y++ {
		for x := 0; x < 320; x++ {
			value := uint8(9 + (x+y)%24)
			source.Set(x, y, color.RGBA{R: value, G: value + 2, B: value + 4, A: 255})
		}
	}
	media := gatewayImageMedia{Body: encodeQualityTestPNG(t, source), MimeType: "image/png"}
	if err := validateGatewayImageVisualQuality(media); err != nil {
		t.Fatalf("dark detailed image was rejected: %v", err)
	}
}

func TestValidateGatewayImageVisualQualityAllowsSmallLetterbox(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 320, 180))
	for y := 18; y < 162; y++ {
		for x := 0; x < 320; x++ {
			source.Set(x, y, color.RGBA{R: uint8(30 + x%180), G: uint8(40 + y%120), B: 100, A: 255})
		}
	}
	media := gatewayImageMedia{Body: encodeQualityTestPNG(t, source), MimeType: "image/png"}
	if err := validateGatewayImageVisualQuality(media); err != nil {
		t.Fatalf("small cinematic bars were rejected: %v", err)
	}
}

func encodeQualityTestPNG(t *testing.T, source image.Image) []byte {
	t.Helper()
	var body bytes.Buffer
	if err := png.Encode(&body, source); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return body.Bytes()
}
