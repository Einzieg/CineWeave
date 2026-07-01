package workflows

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"testing"

	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExportProjectFinalVideoReusesActiveVersion(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	defer pool.Close()
	orgID, userID, projectID, workflowRunID, _, _ := seedWorkflowGatewayIntegrationData(t, ctx, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})

	timelineID := insertWorkflowProjectTimeline(t, ctx, pool, orgID, projectID)
	artifactID, mediaFileID := insertWorkflowFinalVideoMedia(t, ctx, pool, orgID, projectID, workflowRunID, "org/project/final.mp4")
	finalVideoVersionID := insertWorkflowFinalVideoVersion(t, ctx, pool, orgID, projectID, workflowRunID, timelineID, userID, artifactID, mediaFileID, "org/project/final.mp4")
	exportID := insertWorkflowProjectExport(t, ctx, pool, orgID, projectID, workflowRunID, userID, "final_video", "mp4")

	activities := NewActivities(pool, newWorkflowMemoryStorage(), nil)
	output, err := activities.ExportProject(ctx, ExportProjectInput{
		OrganizationID: orgID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		ExportID:       exportID,
		ExportType:     "final_video",
		Format:         "mp4",
		Title:          "Final",
		Options:        map[string]any{"finalVideoVersionId": finalVideoVersionID},
		CreatedBy:      userID,
	})
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}
	if output.StorageKey != "org/project/final.mp4" || output.ArtifactID != artifactID || output.MediaFileID != mediaFileID || output.ByteSize != 123 {
		t.Fatalf("output = %+v", output)
	}

	var exportStatus, workflowStatus, storedVersionID string
	if err := pool.QueryRow(ctx, `
		SELECT pe.status, wr.status, pe.output->>'finalVideoVersionId'
		FROM project_exports pe
		JOIN workflow_runs wr ON wr.id = pe.workflow_run_id
		WHERE pe.id = $1
	`, exportID).Scan(&exportStatus, &workflowStatus, &storedVersionID); err != nil {
		t.Fatalf("select export status: %v", err)
	}
	if exportStatus != "succeeded" || workflowStatus != "succeeded" || storedVersionID != finalVideoVersionID {
		t.Fatalf("status export=%s workflow=%s version=%s", exportStatus, workflowStatus, storedVersionID)
	}
	var projectExportArtifacts, completedEvents int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM artifacts WHERE project_id = $1 AND type = 'project_export'`, projectID).Scan(&projectExportArtifacts); err != nil {
		t.Fatalf("select project export artifacts: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox WHERE aggregate_type = 'project_export' AND aggregate_id = $1 AND event_type = 'project.export.completed'`, exportID).Scan(&completedEvents); err != nil {
		t.Fatalf("select export events: %v", err)
	}
	if projectExportArtifacts != 0 || completedEvents != 1 {
		t.Fatalf("projectExportArtifacts=%d completedEvents=%d", projectExportArtifacts, completedEvents)
	}
}

func insertWorkflowFinalVideoMedia(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, projectID, workflowRunID, storageKey string) (string, string) {
	t.Helper()
	var artifactID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, workflow_run_id, type, storage_key, mime_type, content_hash, metadata)
		VALUES ($1, $2, $3, 'final_video', $4, 'video/mp4', 'sha256:final', '{}')
		RETURNING id::text
	`, orgID, projectID, workflowRunID, storageKey).Scan(&artifactID); err != nil {
		t.Fatalf("insert final artifact: %v", err)
	}
	var mediaFileID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_files(organization_id, project_id, artifact_id, storage_key, mime_type, byte_size, checksum, metadata)
		VALUES ($1, $2, $3, $4, 'video/mp4', 123, 'sha256:final', '{}')
		RETURNING id::text
	`, orgID, projectID, artifactID, storageKey).Scan(&mediaFileID); err != nil {
		t.Fatalf("insert final media file: %v", err)
	}
	return artifactID, mediaFileID
}

func insertWorkflowFinalVideoVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, projectID, workflowRunID, timelineID, userID, artifactID, mediaFileID, storageKey string) string {
	t.Helper()
	var versionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO final_video_versions(
			organization_id, project_id, timeline_id, workflow_run_id, version, title, status, artifact_id, media_file_id, storage_key,
			duration_seconds, resolution, aspect_ratio, compose_settings, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, 1, 'Final', 'active', $5, $6, $7, 12.5, '720p', '16:9', '{}', '{}', $8)
		RETURNING id::text
	`, orgID, projectID, timelineID, workflowRunID, artifactID, mediaFileID, storageKey, userID).Scan(&versionID); err != nil {
		t.Fatalf("insert final video version: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE projects SET active_final_video_version_id = $2 WHERE id = $1`, projectID, versionID); err != nil {
		t.Fatalf("set active final video version: %v", err)
	}
	return versionID
}

func insertWorkflowProjectExport(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, projectID, workflowRunID, userID, exportType, format string) string {
	t.Helper()
	var exportID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO project_exports(organization_id, project_id, export_type, status, title, format, workflow_run_id, request, output, created_by)
		VALUES ($1, $2, $3, 'queued', 'Export', $4, $5, '{}', '{}', $6)
		RETURNING id::text
	`, orgID, projectID, exportType, format, workflowRunID, userID).Scan(&exportID); err != nil {
		t.Fatalf("insert project export: %v", err)
	}
	return exportID
}

func (s *workflowMemoryStorage) GetObject(ctx context.Context, key string, maxBytes int64) ([]byte, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, ok := s.objects[key]
	if !ok {
		return nil, "", fmt.Errorf("object %s not found", key)
	}
	if maxBytes > 0 && int64(len(body)) > maxBytes {
		return nil, "", fmt.Errorf("object exceeds maxBytes")
	}
	return bytes.Clone(body), "application/octet-stream", nil
}

func (s *workflowMemoryStorage) PutFile(ctx context.Context, key, filePath, contentType string) (storage.PutResult, error) {
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
