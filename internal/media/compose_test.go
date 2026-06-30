package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/storage"
)

func TestBuildFinalVideoStorageKey(t *testing.T) {
	key := BuildFinalVideoStorageKey(ComposeRequest{
		OrganizationID: "org/../bad",
		ProjectID:      "project-1",
		WorkflowRunID:  "workflow-1",
	}, time.Date(2026, 6, 30, 8, 9, 10, 0, time.UTC))
	if strings.Contains(key, "..") || strings.Contains(key, "./") || !strings.HasSuffix(key, "final-video-20260630T080910Z.mp4") {
		t.Fatalf("storage key = %q", key)
	}
}

func TestComposeClipsUploadsFinalVideo(t *testing.T) {
	requireFFmpeg(t)
	ctx := context.Background()
	tempDir := t.TempDir()
	clipA := filepath.Join(tempDir, "clip-a.mp4")
	clipB := filepath.Join(tempDir, "clip-b.mp4")
	writeTestClip(t, clipA, "testsrc=size=160x90:rate=24")
	writeTestClip(t, clipB, "testsrc2=size=160x90:rate=24")

	store := newComposeMemoryStore(t, map[string]string{
		"clips/a.mp4": clipA,
		"clips/b.mp4": clipB,
	})
	result, err := ComposeClipsWithStore(ctx, ComposeRequest{
		OrganizationID: "org",
		ProjectID:      "project",
		WorkflowRunID:  "workflow",
		Clips: []Clip{
			{ShotID: "shot-a", ShotIndex: 0, StorageKey: "clips/a.mp4", MimeType: "video/mp4"},
			{ShotID: "shot-b", ShotIndex: 1, StorageKey: "clips/b.mp4", MimeType: "video/mp4"},
		},
		AspectRatio:    "16:9",
		Resolution:     "720p",
		OutputMimeType: "video/mp4",
	}, store)
	if err != nil {
		t.Fatalf("ComposeClipsWithStore: %v", err)
	}
	if result.StorageKey == "" || result.MimeType != "video/mp4" || result.ByteSize <= 0 || !strings.HasPrefix(result.ContentHash, "sha256:") || result.Width != 1280 || result.Height != 720 {
		t.Fatalf("result = %+v", result)
	}
	if _, ok := store.objects[result.StorageKey]; !ok {
		t.Fatalf("final video was not uploaded to %q", result.StorageKey)
	}
}

func TestComposeClipsNoClips(t *testing.T) {
	if _, err := ComposeClipsWithStore(context.Background(), ComposeRequest{}, newComposeMemoryStore(t, nil)); err == nil || err != ErrNoVideoClips {
		t.Fatalf("err = %v, want ErrNoVideoClips", err)
	}
}

type composeMemoryStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newComposeMemoryStore(t *testing.T, files map[string]string) *composeMemoryStore {
	t.Helper()
	store := &composeMemoryStore{objects: map[string][]byte{}}
	for key, filePath := range files {
		body, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		store.objects[key] = body
	}
	return store
}

func (s *composeMemoryStore) GetObject(ctx context.Context, key string, maxBytes int64) ([]byte, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, ok := s.objects[key]
	if !ok {
		return nil, "", fmt.Errorf("object %s not found", key)
	}
	if maxBytes > 0 && int64(len(body)) > maxBytes {
		return nil, "", fmt.Errorf("object exceeds maxBytes")
	}
	return bytes.Clone(body), "video/mp4", nil
}

func (s *composeMemoryStore) PutFile(ctx context.Context, key, filePath, contentType string) (storage.PutResult, error) {
	body, err := os.ReadFile(filePath)
	if err != nil {
		return storage.PutResult{}, err
	}
	sum := sha256.Sum256(body)
	s.mu.Lock()
	s.objects[key] = bytes.Clone(body)
	s.mu.Unlock()
	return storage.PutResult{
		StorageKey:  key,
		ContentHash: "sha256:" + hex.EncodeToString(sum[:]),
		ByteSize:    int64(len(body)),
	}, nil
}
