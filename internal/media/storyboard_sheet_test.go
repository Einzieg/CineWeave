package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/Einzieg/cineweave/internal/storage"
)

func TestCropStoryboardSheetProducesOrderedAspectCorrectPanels(t *testing.T) {
	store := &storyboardSheetMemoryStore{objects: map[string][]byte{}}
	source := image.NewRGBA(image.Rect(0, 0, 600, 900))
	colors := []color.RGBA{{R: 255, A: 255}, {G: 255, A: 255}, {B: 255, A: 255}}
	for index, panelColor := range colors {
		for y := index * 300; y < (index+1)*300; y++ {
			for x := 0; x < 600; x++ {
				source.SetRGBA(x, y, panelColor)
			}
		}
	}
	path := filepath.Join(t.TempDir(), "sheet.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, source); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutFile(context.Background(), "sheet.png", path, "image/png"); err != nil {
		t.Fatal(err)
	}
	result, err := CropStoryboardSheet(context.Background(), StoryboardSheetCropRequest{
		SourceStorageKey: "sheet.png", OutputPrefix: "panels", Rows: 3, Columns: 1,
		PanelCount: 3, PanelAspectRatio: "16:9",
	}, store)
	if err != nil {
		t.Fatalf("CropStoryboardSheet: %v", err)
	}
	if len(result.Panels) != 3 {
		t.Fatalf("panels = %+v", result.Panels)
	}
	for index, panel := range result.Panels {
		if panel.Ordinal != index+1 || panel.Width*9 != panel.Height*16 || panel.ContentHash == "" {
			t.Fatalf("panel %d = %+v", index, panel)
		}
	}
}

type storyboardSheetMemoryStore struct{ objects map[string][]byte }

func (store *storyboardSheetMemoryStore) GetObject(_ context.Context, key string, _ int64) ([]byte, string, error) {
	return store.objects[key], "image/png", nil
}

func (store *storyboardSheetMemoryStore) PutFile(_ context.Context, key, path, _ string) (storage.PutResult, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return storage.PutResult{}, err
	}
	store.objects[key] = body
	sum := sha256.Sum256(body)
	return storage.PutResult{StorageKey: key, ContentHash: "sha256:" + hex.EncodeToString(sum[:]), ByteSize: int64(len(body))}, nil
}
