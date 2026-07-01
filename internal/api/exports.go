package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/workflows"
	"go.temporal.io/sdk/client"
)

type ProjectExport struct {
	ID             string          `json:"id"`
	OrganizationID string          `json:"organizationId"`
	ProjectID      string          `json:"projectId"`
	ExportType     string          `json:"exportType"`
	Status         string          `json:"status"`
	Title          string          `json:"title"`
	Format         string          `json:"format"`
	WorkflowRunID  *string         `json:"workflowRunId,omitempty"`
	ArtifactID     *string         `json:"artifactId,omitempty"`
	MediaFileID    *string         `json:"mediaFileId,omitempty"`
	StorageKey     *string         `json:"storageKey,omitempty"`
	ByteSize       *int64          `json:"byteSize,omitempty"`
	ContentHash    *string         `json:"contentHash,omitempty"`
	Request        json.RawMessage `json:"request"`
	Output         json.RawMessage `json:"output"`
	ErrorCode      *string         `json:"errorCode,omitempty"`
	ErrorMessage   *string         `json:"errorMessage,omitempty"`
	CreatedBy      *string         `json:"createdBy,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	StartedAt      *time.Time      `json:"startedAt,omitempty"`
	CompletedAt    *time.Time      `json:"completedAt,omitempty"`
}

type CreateProjectExportResponse struct {
	ExportID      string `json:"exportId"`
	WorkflowRunID string `json:"workflowRunId"`
	Status        string `json:"status"`
}

func (s *Server) listProjectExports(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, export_type, status, title, format, workflow_run_id::text,
		       artifact_id::text, media_file_id::text, storage_key, byte_size, content_hash,
		       request, output, error_code, error_message, created_by::text, created_at, started_at, completed_at
		FROM project_exports
		WHERE project_id = $1
		ORDER BY created_at DESC
		LIMIT 100
	`, project.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]ProjectExport, 0)
	for rows.Next() {
		item, err := scanProjectExport(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createProjectExport(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	if s.temporal == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "Temporal client is not configured", nil, true)
		return
	}
	var req struct {
		ExportType string         `json:"exportType"`
		Format     string         `json:"format"`
		Title      string         `json:"title"`
		Options    map[string]any `json:"options"`
	}
	if !decode(w, r, &req) {
		return
	}
	exportType := strings.TrimSpace(req.ExportType)
	format := defaultExportFormat(exportType, req.Format)
	if !validProjectExport(exportType, format) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "exportType or format is invalid", nil, false)
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = defaultExportTitle(project.Name, exportType)
	}
	if req.Options == nil {
		req.Options = map[string]any{}
	}
	requestJSON := mustMarshal(map[string]any{
		"exportType": exportType,
		"format":     format,
		"title":      title,
		"options":    req.Options,
	})
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var exportItem ProjectExport
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO project_exports(organization_id, project_id, export_type, status, title, format, request, output, created_by)
		VALUES ($1, $2, $3, 'queued', $4, $5, $6, '{}', $7)
		RETURNING id, organization_id, project_id, export_type, status, title, format, workflow_run_id::text,
		          artifact_id::text, media_file_id::text, storage_key, byte_size, content_hash,
		          request, output, error_code, error_message, created_by::text, created_at, started_at, completed_at
	`, project.OrganizationID, project.ID, exportType, title, format, requestJSON, principal.UserID).Scan(
		&exportItem.ID, &exportItem.OrganizationID, &exportItem.ProjectID, &exportItem.ExportType, &exportItem.Status, &exportItem.Title, &exportItem.Format, &exportItem.WorkflowRunID,
		&exportItem.ArtifactID, &exportItem.MediaFileID, &exportItem.StorageKey, &exportItem.ByteSize, &exportItem.ContentHash,
		&exportItem.Request, &exportItem.Output, &exportItem.ErrorCode, &exportItem.ErrorMessage, &exportItem.CreatedBy, &exportItem.CreatedAt, &exportItem.StartedAt, &exportItem.CompletedAt,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	workflowInput := workflows.ExportProjectInput{
		OrganizationID: project.OrganizationID,
		ProjectID:      project.ID,
		ExportID:       exportItem.ID,
		ExportType:     exportType,
		Format:         format,
		Title:          title,
		Options:        req.Options,
		CreatedBy:      principal.UserID,
	}
	runInput := json.RawMessage(mustMarshal(map[string]any{
		"prompt":       "",
		"workflowType": "export_project",
		"input":        workflowInput,
	}))
	var run WorkflowRun
	if err := tx.QueryRow(r.Context(), `
		WITH new_run AS (SELECT gen_random_uuid() AS id)
		INSERT INTO workflow_runs(id, organization_id, project_id, temporal_workflow_id, status, input, output, created_by)
		SELECT id, $1, $2, 'workflow-' || id::text, 'queued', $3, '{}', $4
		FROM new_run
		RETURNING id, organization_id, project_id, template_id, temporal_workflow_id, status, input, output, error_code, error_message, created_by, created_at, started_at, completed_at, cancelled_at
	`, project.OrganizationID, project.ID, runInput, principal.UserID).Scan(
		&run.ID, &run.OrganizationID, &run.ProjectID, &run.TemplateID, &run.TemporalWorkflowID, &run.Status, &run.Input, &run.Output,
		&run.ErrorCode, &run.ErrorMessage, &run.CreatedBy, &run.CreatedAt, &run.StartedAt, &run.CompletedAt, &run.CancelledAt,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	workflowInput.WorkflowRunID = run.ID
	runInputWithWorkflowID := json.RawMessage(mustMarshal(map[string]any{
		"prompt":       "",
		"workflowType": "export_project",
		"input":        workflowInput,
	}))
	if _, err := tx.Exec(r.Context(), `UPDATE workflow_runs SET input = $2 WHERE id = $1`, run.ID, runInputWithWorkflowID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE project_exports
		SET workflow_run_id = $2, request = jsonb_set(request, '{workflowRunId}', to_jsonb($2::text), true)
		WHERE id = $1
	`, exportItem.ID, run.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := s.temporal.ExecuteWorkflow(r.Context(), client.StartWorkflowOptions{
		ID:        run.TemporalWorkflowID,
		TaskQueue: workflows.MediaTaskQueue,
	}, workflows.ExportProjectWorkflow, workflowInput); err != nil {
		_, _ = s.db.Exec(r.Context(), `
			UPDATE workflow_runs
			SET status = 'failed', error_code = 'TEMPORAL_START_FAILED', error_message = $2, completed_at = now()
			WHERE id = $1
		`, run.ID, err.Error())
		_, _ = s.db.Exec(r.Context(), `
			UPDATE project_exports
			SET status = 'failed', error_code = 'TEMPORAL_START_FAILED', error_message = $2, completed_at = now()
			WHERE id = $1
		`, exportItem.ID, err.Error())
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, CreateProjectExportResponse{ExportID: exportItem.ID, WorkflowRunID: run.ID, Status: "queued"}, nil)
}

func (s *Server) getProjectExport(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	item, err := s.projectExportByID(r, project.ID, r.PathValue("exportId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) createProjectExportDownloadURL(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	if s.storage == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "object storage is not configured", nil, true)
		return
	}
	item, err := s.projectExportByID(r, project.ID, r.PathValue("exportId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req struct {
		ExpiresSeconds int `json:"expiresSeconds"`
	}
	if !decode(w, r, &req) {
		return
	}
	if item.Status != "succeeded" || item.StorageKey == nil || strings.TrimSpace(*item.StorageKey) == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "EXPORT_NOT_READY", "export is not ready for download", nil, false)
		return
	}
	presigned, err := s.storage.PresignGetObject(r.Context(), *item.StorageKey, previewURLExpiry(req.ExpiresSeconds))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"exportId":   item.ID,
		"storageKey": presigned.StorageKey,
		"url":        presigned.URL,
		"method":     presigned.Method,
		"expiresAt":  presigned.ExpiresAt,
	}, nil)
}

func (s *Server) createFinalVideoDownloadURL(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	if s.storage == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "object storage is not configured", nil, true)
		return
	}
	version, err := s.finalVideoVersionByID(r, project.ID, r.PathValue("versionId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req struct {
		ExpiresSeconds int `json:"expiresSeconds"`
	}
	if !decode(w, r, &req) {
		return
	}
	if version.StorageKey == nil || strings.TrimSpace(*version.StorageKey) == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "FINAL_VIDEO_NOT_READY", "final video has no storage object", nil, false)
		return
	}
	presigned, err := s.storage.PresignGetObject(r.Context(), *version.StorageKey, previewURLExpiry(req.ExpiresSeconds))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"finalVideoVersionId": version.ID,
		"storageKey":          presigned.StorageKey,
		"url":                 presigned.URL,
		"method":              presigned.Method,
		"expiresAt":           presigned.ExpiresAt,
	}, nil)
}

func (s *Server) projectExportByID(r *http.Request, projectID, exportID string) (ProjectExport, error) {
	return scanProjectExport(s.db.QueryRow(r.Context(), `
		SELECT id, organization_id, project_id, export_type, status, title, format, workflow_run_id::text,
		       artifact_id::text, media_file_id::text, storage_key, byte_size, content_hash,
		       request, output, error_code, error_message, created_by::text, created_at, started_at, completed_at
		FROM project_exports
		WHERE project_id = $1 AND id = $2
	`, projectID, exportID))
}

func scanProjectExport(row rowScan) (ProjectExport, error) {
	var item ProjectExport
	var workflowRunID, artifactID, mediaFileID, storageKey, contentHash, errorCode, errorMessage, createdBy sql.NullString
	var byteSize sql.NullInt64
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.ExportType, &item.Status, &item.Title, &item.Format, &workflowRunID,
		&artifactID, &mediaFileID, &storageKey, &byteSize, &contentHash,
		&item.Request, &item.Output, &errorCode, &errorMessage, &createdBy, &item.CreatedAt, &item.StartedAt, &item.CompletedAt,
	)
	item.WorkflowRunID = stringPtrFromNull(workflowRunID)
	item.ArtifactID = stringPtrFromNull(artifactID)
	item.MediaFileID = stringPtrFromNull(mediaFileID)
	item.StorageKey = stringPtrFromNull(storageKey)
	item.ContentHash = stringPtrFromNull(contentHash)
	item.ErrorCode = stringPtrFromNull(errorCode)
	item.ErrorMessage = stringPtrFromNull(errorMessage)
	item.CreatedBy = stringPtrFromNull(createdBy)
	if byteSize.Valid {
		item.ByteSize = &byteSize.Int64
	}
	return item, err
}

func defaultExportFormat(exportType, requested string) string {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		return requested
	}
	switch exportType {
	case "final_video":
		return "mp4"
	case "documents":
		return "json"
	case "asset_package", "project_archive":
		return "zip"
	default:
		return ""
	}
}

func validProjectExport(exportType, format string) bool {
	switch exportType {
	case "final_video":
		return format == "mp4"
	case "documents":
		return format == "json" || format == "markdown"
	case "asset_package", "project_archive":
		return format == "zip"
	default:
		return false
	}
}

func defaultExportTitle(projectName, exportType string) string {
	projectName = strings.TrimSpace(projectName)
	if projectName == "" {
		projectName = "CineWeave Project"
	}
	switch exportType {
	case "final_video":
		return projectName + " final video"
	case "documents":
		return projectName + " documents"
	case "asset_package":
		return projectName + " asset package"
	case "project_archive":
		return projectName + " project archive"
	default:
		return projectName + " export"
	}
}
