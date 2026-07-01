package api

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/workflows"
)

func TestCreateProjectExportStartsWorkflow(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	temporal := &fakeTemporalClient{}
	server := New(seed.pool, seed.authService, nil, nil, nil)
	server.temporal = temporal

	var response CreateProjectExportResponse
	doAPISuccess(t, server.Handler(), http.MethodPost, "/api/projects/"+seed.projectID+"/exports", seed.ownerToken, seed.organizationID, map[string]any{
		"exportType": "project_archive",
		"format":     "zip",
		"title":      "Archive",
		"options": map[string]any{
			"includeFinalVideos": true,
		},
	}, &response)

	if response.ExportID == "" || response.WorkflowRunID == "" || response.Status != "queued" {
		t.Fatalf("response = %+v", response)
	}
	if temporal.executeCount != 1 || temporal.options.TaskQueue != workflows.MediaTaskQueue {
		t.Fatalf("temporal calls=%d options=%+v", temporal.executeCount, temporal.options)
	}
	input, ok := temporal.args[0].(workflows.ExportProjectInput)
	if !ok {
		t.Fatalf("workflow input type = %T", temporal.args[0])
	}
	if input.ExportID != response.ExportID || input.WorkflowRunID != response.WorkflowRunID || input.ExportType != "project_archive" || input.Format != "zip" {
		t.Fatalf("workflow input = %+v response=%+v", input, response)
	}
	var exportStatus, workflowRunID, workflowType string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT pe.status, pe.workflow_run_id::text, wr.input->>'workflowType'
		FROM project_exports pe
		JOIN workflow_runs wr ON wr.id = pe.workflow_run_id
		WHERE pe.id = $1
	`, response.ExportID).Scan(&exportStatus, &workflowRunID, &workflowType); err != nil {
		t.Fatalf("select project export: %v", err)
	}
	if exportStatus != "queued" || workflowRunID != response.WorkflowRunID || workflowType != "export_project" {
		t.Fatalf("stored export status=%s workflowRunID=%s workflowType=%s", exportStatus, workflowRunID, workflowType)
	}
}

func TestProjectExportAccessAndDownloadURL(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	temporal := &fakeTemporalClient{}
	createServer := New(seed.pool, seed.authService, nil, nil, nil)
	createServer.temporal = temporal
	assertAPIErrorCode(t, createServer.Handler(), http.MethodPost, "/api/projects/"+seed.projectID+"/exports", seed.otherToken, seed.organizationID, map[string]any{
		"exportType": "documents",
		"format":     "json",
	}, http.StatusForbidden, "ACCESS_DENIED")

	readyID := seed.insertProjectExport(t, "succeeded", "project_archive", "zip", "org/project/exports/archive.zip")
	queuedID := seed.insertProjectExport(t, "queued", "documents", "json", "")
	assertAPIErrorCode(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/exports/"+readyID+"/download-url", seed.otherToken, seed.organizationID, map[string]any{"expiresSeconds": 900}, http.StatusForbidden, "ACCESS_DENIED")
	assertAPIErrorCode(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/exports/"+queuedID+"/download-url", seed.ownerToken, seed.organizationID, map[string]any{"expiresSeconds": 900}, http.StatusUnprocessableEntity, "EXPORT_NOT_READY")

	var download struct {
		ExportID   string    `json:"exportId"`
		StorageKey string    `json:"storageKey"`
		URL        string    `json:"url"`
		Method     string    `json:"method"`
		ExpiresAt  time.Time `json:"expiresAt"`
	}
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/exports/"+readyID+"/download-url", seed.ownerToken, seed.organizationID, map[string]any{"expiresSeconds": 7200}, &download)
	if download.ExportID != readyID || download.StorageKey != "org/project/exports/archive.zip" || download.URL == "" || download.Method != "GET" {
		t.Fatalf("download = %+v", download)
	}
	if time.Until(download.ExpiresAt) > time.Hour+5*time.Second {
		t.Fatalf("expiresAt was not clamped to one hour: %s", download.ExpiresAt)
	}
}

func TestFinalVideoDownloadURL(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	timelineID := insertProjectTimeline(t, seed)
	versionID := seed.insertStoredFinalVideoVersion(t, timelineID, "org/project/final-v1.mp4")

	var download struct {
		FinalVideoVersionID string    `json:"finalVideoVersionId"`
		StorageKey          string    `json:"storageKey"`
		URL                 string    `json:"url"`
		Method              string    `json:"method"`
		ExpiresAt           time.Time `json:"expiresAt"`
	}
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/final-videos/"+versionID+"/download-url", seed.ownerToken, seed.organizationID, map[string]any{"expiresSeconds": 900}, &download)
	if download.FinalVideoVersionID != versionID || download.StorageKey != "org/project/final-v1.mp4" || download.URL == "" || download.Method != "GET" {
		t.Fatalf("download = %+v", download)
	}
}

func (s *artifactPreviewSeed) insertProjectExport(t *testing.T, status, exportType, format, storageKey string) string {
	t.Helper()
	var id string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO project_exports(organization_id, project_id, export_type, status, title, format, storage_key, request, output, created_by)
		VALUES ($1, $2, $3, $4, 'Export', $5, NULLIF($6, ''), '{}', '{}', $7)
		RETURNING id::text
	`, s.organizationID, s.projectID, exportType, status, format, storageKey, s.ownerUserID).Scan(&id); err != nil {
		t.Fatalf("insert project export: %v", err)
	}
	return id
}

func (s *artifactPreviewSeed) insertStoredFinalVideoVersion(t *testing.T, timelineID, storageKey string) string {
	t.Helper()
	var artifactID string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO artifacts(organization_id, project_id, type, storage_key, mime_type, content_hash, metadata, created_by)
		VALUES ($1, $2, 'final_video', $3, 'video/mp4', 'sha256:final', '{}', $4)
		RETURNING id::text
	`, s.organizationID, s.projectID, storageKey, s.ownerUserID).Scan(&artifactID); err != nil {
		t.Fatalf("insert final artifact: %v", err)
	}
	var mediaFileID string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO media_files(organization_id, project_id, artifact_id, storage_key, mime_type, byte_size, checksum)
		VALUES ($1, $2, $3, $4, 'video/mp4', 123, 'sha256:final')
		RETURNING id::text
	`, s.organizationID, s.projectID, artifactID, storageKey).Scan(&mediaFileID); err != nil {
		t.Fatalf("insert final media file: %v", err)
	}
	var versionID string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO final_video_versions(
			organization_id, project_id, timeline_id, version, title, status, artifact_id, media_file_id, storage_key,
			resolution, aspect_ratio, compose_settings, metadata, created_by
		)
		VALUES ($1, $2, $3, 1, 'Final', 'active', $4, $5, $6, '720p', '16:9', '{}', '{}', $7)
		RETURNING id::text
	`, s.organizationID, s.projectID, timelineID, artifactID, mediaFileID, storageKey, s.ownerUserID).Scan(&versionID); err != nil {
		if strings.Contains(err.Error(), "final_video_versions_project_version_unique") {
			t.Fatalf("duplicate final video test data: %v", err)
		}
		t.Fatalf("insert final video version: %v", err)
	}
	if _, err := s.pool.Exec(s.ctx, `UPDATE projects SET active_final_video_version_id = $2 WHERE id = $1`, s.projectID, sql.NullString{String: versionID, Valid: versionID != ""}); err != nil {
		t.Fatalf("set active final video: %v", err)
	}
	return versionID
}
