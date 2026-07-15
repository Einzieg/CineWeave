package media

import (
	"bytes"
	"context"
	"image/png"
	"path/filepath"
	"testing"
)

func TestExtractContinuityTailFrameUploadsPNG(t *testing.T) {
	requireFFmpeg(t)
	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.mp4")
	writeTestClip(t, sourcePath, "testsrc2=size=160x90:rate=24")
	store := newComposeMemoryStore(t, map[string]string{"videos/source.mp4": sourcePath})

	result, err := ExtractContinuityTailFrame(context.Background(), ContinuityFrameRequest{
		SourceStorageKey: "videos/source.mp4",
		OutputStorageKey: "continuity/tail-frame.png",
	}, store)
	if err != nil {
		t.Fatalf("ExtractContinuityTailFrame: %v", err)
	}
	if result.MimeType != "image/png" || result.ByteSize <= 0 || result.ContentHash == "" || result.Width != 160 || result.Height != 90 || result.FrameTimeSeconds <= 0 {
		t.Fatalf("result = %+v", result)
	}
	body := store.objects[result.StorageKey]
	if !bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("uploaded frame is not a PNG")
	}
	decoded, err := png.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("decode extracted PNG: %v", err)
	}
	if decoded.Bounds().Dx() != 160 || decoded.Bounds().Dy() != 90 {
		t.Fatalf("decoded bounds = %v", decoded.Bounds())
	}
}
