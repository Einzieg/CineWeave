package api

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMediaFileDownloadURLSecurity(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	artifactID := seed.insertArtifact(t, "generated_video", "org/project/video.mp4", "video/mp4")
	mediaFileID := seed.insertMediaFile(t, artifactID, "org/project/video.mp4", "video/mp4")

	assertAPIErrorCode(t, server, http.MethodPost, "/api/media-files/"+mediaFileID+"/download-url", seed.otherToken, seed.organizationID, map[string]any{"expiresSeconds": 900}, http.StatusForbidden, "FORBIDDEN")

	var download struct {
		MediaFileID string    `json:"mediaFileId"`
		StorageKey  string    `json:"storageKey"`
		URL         string    `json:"url"`
		Method      string    `json:"method"`
		ExpiresAt   time.Time `json:"expiresAt"`
	}
	doAPISuccess(t, server, http.MethodPost, "/api/media-files/"+mediaFileID+"/download-url", seed.ownerToken, seed.organizationID, map[string]any{"expiresSeconds": 7200}, &download)
	if download.MediaFileID != mediaFileID || download.StorageKey == "" || download.URL == "" || download.Method != "GET" {
		t.Fatalf("download response = %+v", download)
	}
	if time.Until(download.ExpiresAt) > time.Hour+5*time.Second {
		t.Fatalf("expiresAt was not clamped to one hour: %s", download.ExpiresAt)
	}
	if !strings.Contains(download.URL, "localhost:9000") {
		t.Fatalf("download URL did not use public endpoint: %s", download.URL)
	}
}

func (s *artifactPreviewSeed) insertMediaFile(t *testing.T, artifactID, storageKey, mimeType string) string {
	t.Helper()
	var mediaFileID string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO media_files(organization_id, project_id, artifact_id, storage_key, mime_type, byte_size, checksum, metadata)
		VALUES ($1, $2, $3, $4, $5, 128, 'sha256:test', '{}')
		RETURNING id
	`, s.organizationID, s.projectID, artifactID, storageKey, mimeType).Scan(&mediaFileID); err != nil {
		t.Fatalf("insert media file: %v", err)
	}
	return mediaFileID
}
