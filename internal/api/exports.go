package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

type createProjectExportRequest struct {
	ExportType string         `json:"exportType"`
	Format     string         `json:"format"`
	Title      string         `json:"title"`
	Options    map[string]any `json:"options"`
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
	var req createProjectExportRequest
	if !decode(w, r, &req) {
		return
	}
	response, _, _, err := s.createProjectExportCore(r.Context(), principal, project, req, "")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, response, nil)
}

func (s *Server) createProjectExportCore(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	req createProjectExportRequest,
	projectControlCommandID string,
) (CreateProjectExportResponse, WorkflowRun, bool, error) {
	exportType := strings.TrimSpace(req.ExportType)
	format := defaultExportFormat(exportType, req.Format)
	if !validProjectExport(exportType, format) {
		return CreateProjectExportResponse{}, WorkflowRun{}, false, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "exportType or format is invalid")
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = defaultExportTitle(project.Name, exportType)
	}
	if req.Options == nil {
		req.Options = map[string]any{}
	}
	if exportType == "final_video" {
		if _, err := s.requireFinalVideoProductionReady(ctx, project.ID, finalVideoOptionString(req.Options, "finalVideoVersionId")); err != nil {
			return CreateProjectExportResponse{}, WorkflowRun{}, false, err
		}
	}
	commandID := strings.TrimSpace(projectControlCommandID)
	if commandID != "" {
		existing, found, err := s.workflowRunForProjectControlCommand(ctx, project.ID, "export_project", commandID)
		if err != nil {
			return CreateProjectExportResponse{}, WorkflowRun{}, false, err
		}
		if found {
			exportItem, err := s.projectExportByWorkflowRun(ctx, project.ID, existing.ID)
			if err != nil {
				return CreateProjectExportResponse{}, WorkflowRun{}, false, err
			}
			return CreateProjectExportResponse{ExportID: exportItem.ID, WorkflowRunID: existing.ID, Status: exportItem.Status}, existing, true, nil
		}
	}
	if project.VideoProductionBinding == nil || project.ProductionGeneration == nil {
		return CreateProjectExportResponse{}, WorkflowRun{}, false, videoproduction.NewError(videoproduction.CodeGenerationMismatch, "项目没有活动的视频生产代", false)
	}
	exportID := uuid.NewString()
	workflowInput := workflows.ExportProjectInput{
		OrganizationID:          project.OrganizationID,
		ProjectID:               project.ID,
		ExportID:                exportID,
		ExportType:              exportType,
		Format:                  format,
		Title:                   title,
		Options:                 req.Options,
		CreatedBy:               principal.UserID,
		ProjectControlCommandID: commandID,
	}
	storedInput := map[string]any{
		"exportId": exportID, "exportType": exportType, "format": format,
		"title": title, "options": req.Options,
	}
	if commandID != "" {
		storedInput["projectControlCommandId"] = commandID
		storedInput["idempotencyKey"] = "project-control-command:" + commandID
	}
	runInput := json.RawMessage(mustMarshal(map[string]any{
		"prompt": "", "workflowType": "export_project", "input": storedInput,
	}))
	run, err := s.enqueueProjectWorkflowWithHook(
		ctx, principal, project, "export_project", runInput, workflows.MediaTaskQueue, workflows.ExportProjectWorkflow,
		func(run WorkflowRun) any {
			input := workflowInput
			input.WorkflowRunID = run.ID
			return input
		},
		func(ctx context.Context, tx pgx.Tx, run WorkflowRun) error {
			input := workflowInput
			input.WorkflowRunID = run.ID
			fullRunInput := json.RawMessage(mustMarshal(map[string]any{
				"prompt": "", "workflowType": "export_project", "input": input,
			}))
			if _, err := tx.Exec(ctx, `UPDATE workflow_runs SET input = $2 WHERE id = $1`, run.ID, fullRunInput); err != nil {
				return err
			}
			requestJSON := mustMarshal(map[string]any{
				"exportType": exportType, "format": format, "title": title,
				"options": req.Options, "workflowRunId": run.ID,
			})
			_, err := tx.Exec(ctx, `
				INSERT INTO project_exports(
					id, organization_id, project_id, export_type, status, title, format,
					workflow_run_id, request, output, created_by
				)
				VALUES ($1, $2, $3, $4, 'queued', $5, $6, $7, $8, '{}', $9)
			`, exportID, project.OrganizationID, project.ID, exportType, title, format, run.ID, requestJSON, principal.UserID)
			return err
		},
	)
	if err != nil {
		return CreateProjectExportResponse{}, WorkflowRun{}, false, err
	}
	return CreateProjectExportResponse{ExportID: exportID, WorkflowRunID: run.ID, Status: "queued"}, run, false, nil
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
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionArtifactRead)
	if !ok {
		return
	}
	var req struct {
		ExpiresSeconds int `json:"expiresSeconds"`
	}
	if !decode(w, r, &req) {
		return
	}
	input, err := validateProjectExportDownloadActionInput(projectExportDownloadActionInput{
		ExportID: r.PathValue("exportId"), ExpiresSeconds: req.ExpiresSeconds,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	result, err := s.createProjectExportDownloadURLAction(r.Context(), project, input)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, result, nil)
}

func (s *Server) createFinalVideoDownloadURL(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionArtifactRead)
	if !ok {
		return
	}
	var req struct {
		ExpiresSeconds int `json:"expiresSeconds"`
	}
	if !decode(w, r, &req) {
		return
	}
	input, err := validateFinalVideoDownloadActionInput(finalVideoDownloadActionInput{
		VersionID: r.PathValue("versionId"), ExpiresSeconds: req.ExpiresSeconds,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	result, err := s.createFinalVideoDownloadURLAction(r.Context(), project, input)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, result, nil)
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

func (s *Server) projectExportByWorkflowRun(ctx context.Context, projectID, workflowRunID string) (ProjectExport, error) {
	return scanProjectExport(s.db.QueryRow(ctx, `
		SELECT id, organization_id, project_id, export_type, status, title, format, workflow_run_id::text,
		       artifact_id::text, media_file_id::text, storage_key, byte_size, content_hash,
		       request, output, error_code, error_message, created_by::text, created_at, started_at, completed_at
		FROM project_exports
		WHERE project_id = $1 AND workflow_run_id = $2
	`, projectID, workflowRunID))
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
