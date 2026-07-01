package workflows

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/exporter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type ExportProjectInput struct {
	OrganizationID string         `json:"organizationId"`
	ProjectID      string         `json:"projectId"`
	WorkflowRunID  string         `json:"workflowRunId"`
	ExportID       string         `json:"exportId"`
	ExportType     string         `json:"exportType"`
	Format         string         `json:"format"`
	Title          string         `json:"title"`
	Options        map[string]any `json:"options"`
	CreatedBy      string         `json:"createdBy"`
}

type ExportProjectOutput struct {
	ExportID    string `json:"exportId"`
	StorageKey  string `json:"storageKey"`
	ArtifactID  string `json:"artifactId,omitempty"`
	MediaFileID string `json:"mediaFileId,omitempty"`
	ByteSize    int64  `json:"byteSize,omitempty"`
	ContentHash string `json:"contentHash,omitempty"`
}

func ExportProjectWorkflow(ctx workflow.Context, input ExportProjectInput) (ExportProjectOutput, error) {
	options := defaultActivityOptions()
	options.StartToCloseTimeout = 30 * time.Minute
	ctx = workflow.WithActivityOptions(ctx, options)
	var output ExportProjectOutput
	if err := workflow.ExecuteActivity(ctx, "ExportProject", input).Get(ctx, &output); err != nil {
		return ExportProjectOutput{}, err
	}
	return output, nil
}

func (a Activities) ExportProject(ctx context.Context, input ExportProjectInput) (ExportProjectOutput, error) {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.ExportID) == "" {
		return ExportProjectOutput{}, fmt.Errorf("organizationId, projectId, workflowRunId, and exportId are required")
	}
	if _, err := a.db.Exec(ctx, `
		UPDATE project_exports
		SET status = 'running', started_at = COALESCE(started_at, now()), error_code = NULL, error_message = NULL
		WHERE id = $1 AND project_id = $2
	`, input.ExportID, input.ProjectID); err != nil {
		return ExportProjectOutput{}, err
	}
	if _, err := a.db.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'running', started_at = COALESCE(started_at, now()), error_code = NULL, error_message = NULL
		WHERE id = $1
	`, input.WorkflowRunID); err != nil {
		return ExportProjectOutput{}, err
	}
	objectStore, ok := a.storage.(exporter.ObjectStore)
	if !ok {
		err := fmt.Errorf("object storage does not support project export")
		_ = a.failProjectExport(ctx, input, codeActivityFailed, err.Error())
		return ExportProjectOutput{}, temporal.NewApplicationError(err.Error(), codeActivityFailed)
	}
	result, err := exporter.New(a.db, objectStore).Export(ctx, exporter.Request{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		ExportID:       input.ExportID,
		ExportType:     input.ExportType,
		Format:         input.Format,
		Title:          input.Title,
		Options:        input.Options,
		CreatedBy:      input.CreatedBy,
	})
	if err != nil {
		_ = a.failProjectExport(ctx, input, codeActivityFailed, err.Error())
		return ExportProjectOutput{}, temporal.NewApplicationError(err.Error(), codeActivityFailed)
	}
	artifactID, mediaFileID, err := a.ensureProjectExportMedia(ctx, input, result)
	if err != nil {
		_ = a.failProjectExport(ctx, input, codeActivityFailed, err.Error())
		return ExportProjectOutput{}, temporal.NewApplicationError(err.Error(), codeActivityFailed)
	}
	if artifactID != "" {
		result.ArtifactID = artifactID
	}
	if mediaFileID != "" {
		result.MediaFileID = mediaFileID
	}
	output := ExportProjectOutput{
		ExportID:    input.ExportID,
		StorageKey:  result.StorageKey,
		ArtifactID:  result.ArtifactID,
		MediaFileID: result.MediaFileID,
		ByteSize:    result.ByteSize,
		ContentHash: result.ContentHash,
	}
	outputJSON := mustJSON(map[string]any{
		"exportId":    output.ExportID,
		"storageKey":  output.StorageKey,
		"artifactId":  output.ArtifactID,
		"mediaFileId": output.MediaFileID,
		"byteSize":    output.ByteSize,
		"contentHash": output.ContentHash,
	})
	exportOutput := result.Output
	if exportOutput == nil {
		exportOutput = map[string]any{}
	}
	for key, value := range map[string]any{
		"artifactId":  output.ArtifactID,
		"mediaFileId": output.MediaFileID,
		"byteSize":    output.ByteSize,
		"contentHash": output.ContentHash,
	} {
		if value != "" && value != int64(0) {
			exportOutput[key] = value
		}
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ExportProjectOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE project_exports
		SET status = 'succeeded',
		    artifact_id = NULLIF($3, '')::uuid,
		    media_file_id = NULLIF($4, '')::uuid,
		    storage_key = NULLIF($5, ''),
		    byte_size = NULLIF($6, 0),
		    content_hash = NULLIF($7, ''),
		    output = $8,
		    completed_at = now()
		WHERE id = $1 AND project_id = $2
	`, input.ExportID, input.ProjectID, output.ArtifactID, output.MediaFileID, output.StorageKey, output.ByteSize, output.ContentHash, mustJSON(exportOutput)); err != nil {
		return ExportProjectOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'succeeded', output = $2, completed_at = now()
		WHERE id = $1
	`, input.WorkflowRunID, outputJSON); err != nil {
		return ExportProjectOutput{}, err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "project.export.completed", "project_export", input.ExportID, outputJSON); err != nil {
		return ExportProjectOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ExportProjectOutput{}, err
	}
	return output, nil
}

func (a Activities) ensureProjectExportMedia(ctx context.Context, input ExportProjectInput, result exporter.Result) (string, string, error) {
	if result.StorageKey == "" {
		return result.ArtifactID, result.MediaFileID, nil
	}
	if result.ArtifactID != "" || result.MediaFileID != "" {
		return result.ArtifactID, result.MediaFileID, nil
	}
	var artifactID string
	metadata := mustJSON(map[string]any{
		"exportId":   input.ExportID,
		"exportType": input.ExportType,
		"format":     input.Format,
	})
	if err := a.db.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, workflow_run_id, type, storage_key, mime_type, content_hash, metadata, created_by)
		VALUES ($1, $2, $3, 'project_export', $4, $5, NULLIF($6, ''), $7, NULLIF($8, '')::uuid)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, result.StorageKey, result.MimeType, result.ContentHash, metadata, input.CreatedBy).Scan(&artifactID); err != nil {
		return "", "", err
	}
	var mediaFileID string
	if err := a.db.QueryRow(ctx, `
		INSERT INTO media_files(organization_id, project_id, artifact_id, storage_key, mime_type, byte_size, checksum, metadata)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, 0), NULLIF($7, ''), $8)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, artifactID, result.StorageKey, result.MimeType, result.ByteSize, result.ContentHash, metadata).Scan(&mediaFileID); err != nil {
		return "", "", err
	}
	return artifactID, mediaFileID, nil
}

func (a Activities) failProjectExport(ctx context.Context, input ExportProjectInput, code, message string) error {
	if strings.TrimSpace(code) == "" {
		code = codeActivityFailed
	}
	output := mustJSON(map[string]any{"exportId": input.ExportID, "errorCode": code, "errorMessage": message})
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE project_exports
		SET status = 'failed', error_code = $3, error_message = $4, output = $5, completed_at = now()
		WHERE id = $1 AND project_id = $2
	`, input.ExportID, input.ProjectID, code, message, output); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'failed', error_code = $2, error_message = $3, output = $4, completed_at = now()
		WHERE id = $1
	`, input.WorkflowRunID, code, message, output); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "project.export.failed", "project_export", input.ExportID, output); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
