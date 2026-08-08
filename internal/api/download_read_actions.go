package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/google/uuid"
)

type projectExportDownloadActionInput struct {
	ExportID       string `json:"exportId"`
	ExpiresSeconds int    `json:"expiresSeconds"`
}

type finalVideoDownloadActionInput struct {
	VersionID      string `json:"versionId"`
	ExpiresSeconds int    `json:"expiresSeconds"`
}

type downloadURLActionResult struct {
	ExportID            string    `json:"exportId,omitempty"`
	FinalVideoVersionID string    `json:"finalVideoVersionId,omitempty"`
	StorageKey          string    `json:"storageKey"`
	URL                 string    `json:"url"`
	Method              string    `json:"method"`
	ExpiresAt           time.Time `json:"expiresAt"`
}

func decodeProjectExportDownloadActionInput(raw json.RawMessage) (projectExportDownloadActionInput, error) {
	var input projectExportDownloadActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return input, err
	}
	return validateProjectExportDownloadActionInput(input)
}

func validateProjectExportDownloadActionInput(input projectExportDownloadActionInput) (projectExportDownloadActionInput, error) {
	input.ExportID = strings.TrimSpace(input.ExportID)
	if uuid.Validate(input.ExportID) != nil {
		return input, controlValidationError("exportId 无效")
	}
	if input.ExpiresSeconds < 0 || input.ExpiresSeconds > 3600 {
		return input, controlValidationError("expiresSeconds 必须在 0 到 3600 之间")
	}
	return input, nil
}

func decodeFinalVideoDownloadActionInput(raw json.RawMessage) (finalVideoDownloadActionInput, error) {
	var input finalVideoDownloadActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return input, err
	}
	return validateFinalVideoDownloadActionInput(input)
}

func validateFinalVideoDownloadActionInput(input finalVideoDownloadActionInput) (finalVideoDownloadActionInput, error) {
	input.VersionID = strings.TrimSpace(input.VersionID)
	if uuid.Validate(input.VersionID) != nil {
		return input, controlValidationError("versionId 无效")
	}
	if input.ExpiresSeconds < 0 || input.ExpiresSeconds > 3600 {
		return input, controlValidationError("expiresSeconds 必须在 0 到 3600 之间")
	}
	return input, nil
}

func (s *Server) createProjectExportDownloadURLAction(ctx context.Context, project Project, input projectExportDownloadActionInput) (downloadURLActionResult, error) {
	if s.storage == nil {
		return downloadURLActionResult{}, newAPIError(http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "对象存储尚未配置")
	}
	item, err := s.projectExportByID(requestWithContext(ctx), project.ID, input.ExportID)
	if err != nil {
		return downloadURLActionResult{}, err
	}
	if item.Status != "succeeded" || item.StorageKey == nil || strings.TrimSpace(*item.StorageKey) == "" {
		return downloadURLActionResult{}, newAPIError(http.StatusUnprocessableEntity, "EXPORT_NOT_READY", "导出文件尚未准备完成")
	}
	presigned, err := s.storage.PresignGetObject(ctx, *item.StorageKey, previewURLExpiry(input.ExpiresSeconds))
	if err != nil {
		return downloadURLActionResult{}, err
	}
	return downloadURLActionResult{
		ExportID: item.ID, StorageKey: presigned.StorageKey, URL: presigned.URL,
		Method: presigned.Method, ExpiresAt: presigned.ExpiresAt,
	}, nil
}

func (s *Server) createFinalVideoDownloadURLAction(ctx context.Context, project Project, input finalVideoDownloadActionInput) (downloadURLActionResult, error) {
	if s.storage == nil {
		return downloadURLActionResult{}, newAPIError(http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "对象存储尚未配置")
	}
	item, err := s.finalVideoVersionByID(requestWithContext(ctx), project.ID, input.VersionID)
	if err != nil {
		return downloadURLActionResult{}, err
	}
	if item.StorageKey == nil || strings.TrimSpace(*item.StorageKey) == "" {
		return downloadURLActionResult{}, newAPIError(http.StatusUnprocessableEntity, "FINAL_VIDEO_NOT_READY", "成片尚无可下载文件")
	}
	presigned, err := s.storage.PresignGetObject(ctx, *item.StorageKey, previewURLExpiry(input.ExpiresSeconds))
	if err != nil {
		return downloadURLActionResult{}, err
	}
	return downloadURLActionResult{
		FinalVideoVersionID: item.ID, StorageKey: presigned.StorageKey, URL: presigned.URL,
		Method: presigned.Method, ExpiresAt: presigned.ExpiresAt,
	}, nil
}

func (e *projectControlExecutor) exportDownloadURL(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, actionInput, arguments, err := e.decodeNativeAgentReadInput("export.download_url", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "export.download_url", projectID, authz.PermissionArtifactRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	input, err := decodeProjectExportDownloadActionInput(actionInput)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result, err := e.server.createProjectExportDownloadURLAction(ctx, project, input)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	return projectControlResultFromAgentTool(agentToolOK("export.download_url", arguments, "已创建导出文件的短期下载链接。", map[string]any{"download": result})), nil
}

func (e *projectControlExecutor) finalVideoDownloadURL(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	projectID, actionInput, arguments, err := e.decodeNativeAgentReadInput("final_video.download_url", raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedNativeProjectAction(ctx, identity.Principal, "final_video.download_url", projectID, authz.PermissionArtifactRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	input, err := decodeFinalVideoDownloadActionInput(actionInput)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result, err := e.server.createFinalVideoDownloadURLAction(ctx, project, input)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	return projectControlResultFromAgentTool(agentToolOK("final_video.download_url", arguments, "已创建成片的短期下载链接。", map[string]any{"download": result})), nil
}
