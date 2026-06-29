package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/jackc/pgx/v5"
)

type MediaFile struct {
	ID              string          `json:"id"`
	OrganizationID  string          `json:"organizationId"`
	ProjectID       *string         `json:"projectId,omitempty"`
	ArtifactID      *string         `json:"artifactId,omitempty"`
	StorageKey      string          `json:"storageKey"`
	MimeType        string          `json:"mimeType"`
	ByteSize        *int64          `json:"byteSize,omitempty"`
	Width           *int            `json:"width,omitempty"`
	Height          *int            `json:"height,omitempty"`
	DurationSeconds *float64        `json:"durationSeconds,omitempty"`
	Checksum        *string         `json:"checksum,omitempty"`
	Metadata        json.RawMessage `json:"metadata"`
	CreatedAt       time.Time       `json:"createdAt"`
}

func (s *Server) getArtifact(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	artifact, ok := s.requireArtifactAccess(w, r, principal, r.PathValue("artifactId"))
	if !ok {
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, artifact, nil)
}

func (s *Server) createArtifactPreviewURL(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	artifact, ok := s.requireArtifactAccess(w, r, principal, r.PathValue("artifactId"))
	if !ok {
		return
	}
	if s.storage == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "object storage is not configured", nil, true)
		return
	}
	var req struct {
		ExpiresSeconds int `json:"expiresSeconds"`
	}
	if !decode(w, r, &req) {
		return
	}
	if artifact.StorageKey == nil || strings.TrimSpace(*artifact.StorageKey) == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "ARTIFACT_HAS_NO_STORAGE_OBJECT", "artifact has no storage object", nil, false)
		return
	}
	if !artifactCanPreview(artifact) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "UNSUPPORTED_PREVIEW_TYPE", "artifact mime type cannot be previewed", nil, false)
		return
	}
	presigned, err := s.storage.PresignGetObject(r.Context(), *artifact.StorageKey, previewURLExpiry(req.ExpiresSeconds))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"artifactId": artifact.ID,
		"storageKey": presigned.StorageKey,
		"url":        presigned.URL,
		"method":     presigned.Method,
		"expiresAt":  presigned.ExpiresAt,
	}, nil)
}

func (s *Server) getMediaFile(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	mediaFile, ok := s.requireMediaFileAccess(w, r, principal, r.PathValue("mediaFileId"))
	if !ok {
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, mediaFile, nil)
}

func (s *Server) createMediaFileDownloadURL(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	mediaFile, ok := s.requireMediaFileAccess(w, r, principal, r.PathValue("mediaFileId"))
	if !ok {
		return
	}
	if s.storage == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "object storage is not configured", nil, true)
		return
	}
	var req struct {
		ExpiresSeconds int `json:"expiresSeconds"`
	}
	if !decode(w, r, &req) {
		return
	}
	presigned, err := s.storage.PresignGetObject(r.Context(), mediaFile.StorageKey, previewURLExpiry(req.ExpiresSeconds))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"mediaFileId": mediaFile.ID,
		"storageKey":  presigned.StorageKey,
		"url":         presigned.URL,
		"method":      presigned.Method,
		"expiresAt":   presigned.ExpiresAt,
	}, nil)
}

func (s *Server) requireArtifactAccess(w http.ResponseWriter, r *http.Request, principal auth.Principal, artifactID string) (Artifact, bool) {
	artifact, err := s.artifact(r, artifactID)
	if err != nil {
		s.writeError(w, r, err)
		return Artifact{}, false
	}
	if !s.authorizeObjectAccess(w, r, principal, artifact.OrganizationID, artifact.ProjectID) {
		return Artifact{}, false
	}
	return artifact, true
}

func (s *Server) requireMediaFileAccess(w http.ResponseWriter, r *http.Request, principal auth.Principal, mediaFileID string) (MediaFile, bool) {
	mediaFile, err := s.mediaFile(r, mediaFileID)
	if err != nil {
		s.writeError(w, r, err)
		return MediaFile{}, false
	}
	if !s.authorizeObjectAccess(w, r, principal, mediaFile.OrganizationID, mediaFile.ProjectID) {
		return MediaFile{}, false
	}
	return mediaFile, true
}

func (s *Server) authorizeObjectAccess(w http.ResponseWriter, r *http.Request, principal auth.Principal, objectOrganizationID string, projectID *string) bool {
	if organizationID(r, principal) != objectOrganizationID {
		s.writeError(w, r, auth.ErrForbidden)
		return false
	}
	var err error
	if projectID != nil && strings.TrimSpace(*projectID) != "" {
		err = s.ensureProjectMember(r, principal.UserID, *projectID)
	} else {
		err = s.ensureOrganizationMember(r, principal.UserID, objectOrganizationID)
	}
	if err != nil {
		s.writeError(w, r, err)
		return false
	}
	return true
}

func (s *Server) artifact(r *http.Request, artifactID string) (Artifact, error) {
	return scanArtifact(s.db.QueryRow(r.Context(), `
		SELECT id, organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, prompt_hash, model_id, metadata, created_at
		FROM artifacts
		WHERE id = $1
	`, artifactID))
}

func (s *Server) mediaFile(r *http.Request, mediaFileID string) (MediaFile, error) {
	return scanMediaFile(s.db.QueryRow(r.Context(), `
		SELECT id, organization_id, project_id, artifact_id, storage_key, mime_type, byte_size, width, height, duration_seconds, checksum, metadata, created_at
		FROM media_files
		WHERE id = $1
	`, mediaFileID))
}

func scanArtifact(row pgx.Row) (Artifact, error) {
	var item Artifact
	var projectID, workflowRunID, nodeRunID, storageKey, mimeType, contentHash, promptHash, modelID sql.NullString
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&projectID,
		&workflowRunID,
		&nodeRunID,
		&item.Type,
		&storageKey,
		&mimeType,
		&contentHash,
		&promptHash,
		&modelID,
		&item.Metadata,
		&item.CreatedAt,
	)
	item.ProjectID = stringPtrFromNull(projectID)
	item.WorkflowRunID = stringPtrFromNull(workflowRunID)
	item.NodeRunID = stringPtrFromNull(nodeRunID)
	item.StorageKey = stringPtrFromNull(storageKey)
	item.MimeType = stringPtrFromNull(mimeType)
	item.ContentHash = stringPtrFromNull(contentHash)
	item.PromptHash = stringPtrFromNull(promptHash)
	item.ModelID = stringPtrFromNull(modelID)
	return item, err
}

func scanMediaFile(row pgx.Row) (MediaFile, error) {
	var item MediaFile
	var projectID, artifactID, checksum sql.NullString
	var byteSize sql.NullInt64
	var width, height sql.NullInt32
	var durationSeconds sql.NullFloat64
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&projectID,
		&artifactID,
		&item.StorageKey,
		&item.MimeType,
		&byteSize,
		&width,
		&height,
		&durationSeconds,
		&checksum,
		&item.Metadata,
		&item.CreatedAt,
	)
	item.ProjectID = stringPtrFromNull(projectID)
	item.ArtifactID = stringPtrFromNull(artifactID)
	item.Checksum = stringPtrFromNull(checksum)
	if byteSize.Valid {
		item.ByteSize = &byteSize.Int64
	}
	if width.Valid {
		value := int(width.Int32)
		item.Width = &value
	}
	if height.Valid {
		value := int(height.Int32)
		item.Height = &value
	}
	if durationSeconds.Valid {
		item.DurationSeconds = &durationSeconds.Float64
	}
	return item, err
}

func artifactCanPreview(item Artifact) bool {
	if item.MimeType == nil {
		return false
	}
	return canPreviewMimeType(*item.MimeType)
}

func canPreviewMimeType(mimeType string) bool {
	value := strings.ToLower(strings.TrimSpace(mimeType))
	return strings.HasPrefix(value, "image/") ||
		strings.HasPrefix(value, "video/") ||
		strings.HasPrefix(value, "audio/") ||
		strings.HasPrefix(value, "text/") ||
		value == "application/json"
}

func previewURLExpiryFromRequest(r *http.Request) time.Duration {
	seconds, _ := strconv.Atoi(r.URL.Query().Get("previewExpiresSeconds"))
	return previewURLExpiry(seconds)
}

func previewURLExpiry(expiresSeconds int) time.Duration {
	if expiresSeconds <= 0 {
		return 15 * time.Minute
	}
	expires := time.Duration(expiresSeconds) * time.Second
	if expires > time.Hour {
		return time.Hour
	}
	return expires
}
