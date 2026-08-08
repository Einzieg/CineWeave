package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
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
	if temporal.executeCount != 0 {
		t.Fatalf("HTTP request started Temporal directly: calls=%d", temporal.executeCount)
	}
	dispatchWorkflowStartsForTest(t, server)
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

func TestProjectControlExportCreateReusesRunAfterDispatchCrash(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	seed.apiServer.temporal = &fakeTemporalClient{}

	identity := projectControlTestCodexIdentity(t, seed)
	created := executeProjectControlTestAction(t, seed, identity, "export.create", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "export-create-crash-recovery",
		"exportType": "project_archive", "format": "zip", "title": "Codex archive",
	})
	command, err := seed.apiServer.projectControl.repository.Get(seed.ctx, created.CommandID)
	if err != nil {
		t.Fatalf("read export command: %v", err)
	}
	project, err := projectByIDForControl(seed.ctx, seed.apiServer, seed.projectID)
	if err != nil {
		t.Fatalf("read project: %v", err)
	}
	principal := auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID}

	first, err := seed.apiServer.executeExportCreateAsyncAction(seed.ctx, principal, project, command, command.Input)
	if err != nil {
		t.Fatalf("create export before simulated crash: %v", err)
	}
	firstRunID := workflowRunIDFromAgentResult(t, first)
	dispatchProjectControlCommand(t, seed)

	var runCount, exportCount, linkCount int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT COUNT(*)
		FROM workflow_runs
		WHERE project_id = $1
		  AND workflow_type = 'export_project'
		  AND input->'input'->>'projectControlCommandId' = $2
	`, seed.projectID, created.CommandID).Scan(&runCount); err != nil {
		t.Fatalf("count export workflow runs: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT COUNT(*) FROM project_exports WHERE project_id = $1 AND workflow_run_id = $2
	`, seed.projectID, firstRunID).Scan(&exportCount); err != nil {
		t.Fatalf("count exports: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT COUNT(*) FROM project_control_command_workflows WHERE command_id = $1 AND workflow_run_id = $2
	`, created.CommandID, firstRunID).Scan(&linkCount); err != nil {
		t.Fatalf("count export workflow links: %v", err)
	}
	if runCount != 1 || exportCount != 1 || linkCount != 1 {
		t.Fatalf("export runs=%d exports=%d links=%d, want 1/1/1", runCount, exportCount, linkCount)
	}
}

func TestFinalVideoExportBlocksUnverifiedNativeAudio(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	temporal := &fakeTemporalClient{}
	server := New(seed.pool, seed.authService, nil, nil, nil)
	server.temporal = temporal
	configureTimelineTestProject(t, server.Handler(), seed, "Final Video Export Project")
	timelineID := insertProjectTimeline(t, seed)
	versionID := insertFinalVideoVersion(t, seed, timelineID, 1, "ready")
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE final_video_versions SET native_audio_status = 'audio_unverified', production_readiness = 'preview_only' WHERE id = $1`, versionID); err != nil {
		t.Fatalf("mark final video preview only: %v", err)
	}
	assertAPIErrorCode(t, server.Handler(), http.MethodPost, "/api/projects/"+seed.projectID+"/exports", seed.ownerToken, seed.organizationID, map[string]any{
		"exportType": "final_video", "format": "mp4", "options": map[string]any{"finalVideoVersionId": versionID},
	}, http.StatusConflict, "AUDIO_VERIFICATION_REQUIRED")
	if temporal.executeCount != 0 {
		t.Fatalf("unverified final video started %d workflows", temporal.executeCount)
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
	assertAPIErrorCode(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/exports/"+readyID+"/download-url", seed.ownerToken, seed.organizationID, map[string]any{"expiresSeconds": 7200}, http.StatusUnprocessableEntity, "VALIDATION_FAILED")

	var download struct {
		ExportID   string    `json:"exportId"`
		StorageKey string    `json:"storageKey"`
		URL        string    `json:"url"`
		Method     string    `json:"method"`
		ExpiresAt  time.Time `json:"expiresAt"`
	}
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/exports/"+readyID+"/download-url", seed.ownerToken, seed.organizationID, map[string]any{"expiresSeconds": 3600}, &download)
	if download.ExportID != readyID || download.StorageKey != "org/project/exports/archive.zip" || download.URL == "" || download.Method != "GET" {
		t.Fatalf("download = %+v", download)
	}
	if time.Until(download.ExpiresAt) > time.Hour+5*time.Second {
		t.Fatalf("expiresAt exceeds one hour: %s", download.ExpiresAt)
	}

	identity := controlmcp.Identity{
		Principal:      auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID},
		ControllerType: projectcontrol.ControllerCodexMCP,
		Key:            auth.ControlKeyMetadata{ID: "download-test-key"},
	}
	codexResult := executeProjectControlTestAction(t, seed, identity, "export.download_url", map[string]any{
		"projectId": seed.projectID, "exportId": readyID, "expiresSeconds": 900,
	})
	if codexResult.CommandID != "" {
		t.Fatalf("read-only download action created command %s", codexResult.CommandID)
	}
	var codexData projectControlDownloadEnvelope
	if err := json.Unmarshal(codexResult.Data, &codexData); err != nil {
		t.Fatalf("decode Codex export download: %v", err)
	}
	if codexData.Result.Data.Download.ExportID != readyID || codexData.Result.Data.Download.StorageKey != download.StorageKey || codexData.Result.Data.Download.Method != download.Method {
		t.Fatalf("Codex export download = %+v, REST = %+v", codexData.Result.Data.Download, download)
	}
}

func TestFinalVideoDownloadURL(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	configureTimelineTestProject(t, server, seed, "Final Video Download Project")

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

	identity := controlmcp.Identity{
		Principal:      auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID},
		ControllerType: projectcontrol.ControllerCodexMCP,
		Key:            auth.ControlKeyMetadata{ID: "download-test-key"},
	}
	codexResult := executeProjectControlTestAction(t, seed, identity, "final_video.download_url", map[string]any{
		"projectId": seed.projectID, "versionId": versionID, "expiresSeconds": 900,
	})
	var codexData projectControlDownloadEnvelope
	if err := json.Unmarshal(codexResult.Data, &codexData); err != nil {
		t.Fatalf("decode Codex final video download: %v", err)
	}
	if codexData.Result.Data.Download.FinalVideoVersionID != versionID || codexData.Result.Data.Download.StorageKey != download.StorageKey || codexData.Result.Data.Download.Method != download.Method {
		t.Fatalf("Codex final video download = %+v, REST = %+v", codexData.Result.Data.Download, download)
	}
}

type projectControlDownloadEnvelope struct {
	Result struct {
		Data struct {
			Download downloadURLActionResult `json:"download"`
		} `json:"data"`
	} `json:"result"`
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
		INSERT INTO artifacts(
			organization_id, project_id, production_generation_id,
			type, storage_key, mime_type, content_hash, metadata, created_by
		)
		SELECT $1, $2, project.active_video_production_generation_id,
		       'final_video', $3, 'video/mp4', 'sha256:final', '{}', $4
		FROM projects project
		WHERE project.id = $2
		RETURNING id::text
	`, s.organizationID, s.projectID, storageKey, s.ownerUserID).Scan(&artifactID); err != nil {
		t.Fatalf("insert final artifact: %v", err)
	}
	var mediaFileID string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO media_files(
			organization_id, project_id, production_generation_id,
			artifact_id, storage_key, mime_type, byte_size, checksum
		)
		SELECT $1, $2, project.active_video_production_generation_id,
		       $3, $4, 'video/mp4', 123, 'sha256:final'
		FROM projects project
		WHERE project.id = $2
		RETURNING id::text
	`, s.organizationID, s.projectID, artifactID, storageKey).Scan(&mediaFileID); err != nil {
		t.Fatalf("insert final media file: %v", err)
	}
	var versionID string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO final_video_versions(
			organization_id, project_id, production_generation_id,
			timeline_id, version, title, status, artifact_id, media_file_id, storage_key,
			resolution, aspect_ratio, compose_settings, metadata, created_by
		)
		SELECT $1, $2, project.active_video_production_generation_id,
		       $3, 1, 'Final', 'active', $4, $5, $6, '720p', '16:9', '{}', '{}', $7
		FROM projects project
		WHERE project.id = $2
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
