package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
)

type Asset struct {
	ID                string          `json:"id"`
	OrganizationID    string          `json:"organizationId"`
	ProjectID         string          `json:"projectId"`
	AssetType         string          `json:"assetType"`
	Name              string          `json:"name"`
	Description       *string         `json:"description,omitempty"`
	CurrentArtifactID *string         `json:"currentArtifactId,omitempty"`
	Metadata          json.RawMessage `json:"metadata"`
	CreatedBy         *string         `json:"createdBy,omitempty"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
}

type AssetVariant struct {
	ID          string          `json:"id"`
	MediaFileID string          `json:"mediaFileId"`
	VariantType string          `json:"variantType"`
	StorageKey  string          `json:"storageKey"`
	MimeType    string          `json:"mimeType"`
	Metadata    json.RawMessage `json:"metadata"`
	CreatedAt   time.Time       `json:"createdAt"`
}

func (s *Server) listAssets(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetRead)
	if !ok {
		return
	}
	assetType := strings.TrimSpace(r.URL.Query().Get("filter[type]"))
	limit := queryInt(r, "limit", 20)
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, asset_type, name, description, current_artifact_id, metadata, created_by, created_at, updated_at
		FROM assets
		WHERE project_id = $1
		  AND ($2 = '' OR asset_type = $2)
		ORDER BY created_at DESC
		LIMIT $3
	`, project.ID, assetType, limit)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()

	items := make([]Asset, 0)
	for rows.Next() {
		item, err := scanAsset(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, map[string]any{"limit": limit})
}

func (s *Server) createAsset(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite)
	if !ok {
		return
	}
	var req struct {
		AssetType         string          `json:"assetType"`
		Name              string          `json:"name"`
		Description       *string         `json:"description"`
		CurrentArtifactID string          `json:"currentArtifactId"`
		Metadata          json.RawMessage `json:"metadata"`
		IdempotencyKey    string          `json:"idempotencyKey,omitempty"`
	}
	if !decode(w, r, &req) {
		return
	}
	assetType := strings.TrimSpace(req.AssetType)
	name := strings.TrimSpace(req.Name)
	if assetType == "" || name == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "assetType and name are required", nil, false)
		return
	}
	metadata, ok := jsonObjectOrDefault(w, r, req.Metadata)
	if !ok {
		return
	}
	currentArtifactID := strings.TrimSpace(req.CurrentArtifactID)
	if currentArtifactID != "" && !s.artifactBelongsToProject(r, project, currentArtifactID) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "currentArtifactId is not in this project", nil, false)
		return
	}

	idempotency := idempotencyKey(r, req.IdempotencyKey)
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId":         project.ID,
		"assetType":         assetType,
		"name":              name,
		"description":       req.Description,
		"currentArtifactId": currentArtifactID,
		"metadata":          metadata,
	})
	idempotencyState, ok := s.prepareIdempotency(w, r, project.OrganizationID, "assets:create:"+project.ID, idempotency, requestHash)
	if !ok {
		return
	}

	item, err := scanAsset(s.db.QueryRow(r.Context(), `
		INSERT INTO assets(organization_id, project_id, asset_type, name, description, current_artifact_id, metadata, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, organization_id, project_id, asset_type, name, description, current_artifact_id, metadata, created_by, created_at, updated_at
	`, project.OrganizationID, project.ID, assetType, name, req.Description, nullableString(currentArtifactID), metadata, principal.UserID))
	if err != nil {
		s.failIdempotency(r.Context(), idempotencyState)
		s.writeError(w, r, err)
		return
	}
	if err := s.completeIdempotency(r.Context(), idempotencyState, item); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) getAsset(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetRead)
	if !ok {
		return
	}
	item, err := s.asset(r, project.ID, r.PathValue("assetId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) updateAsset(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite)
	if !ok {
		return
	}
	current, err := s.asset(r, project.ID, r.PathValue("assetId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req struct {
		AssetType         *string         `json:"assetType"`
		Name              *string         `json:"name"`
		Description       *string         `json:"description"`
		CurrentArtifactID *string         `json:"currentArtifactId"`
		Metadata          json.RawMessage `json:"metadata"`
	}
	if !decode(w, r, &req) {
		return
	}
	assetType := current.AssetType
	if req.AssetType != nil {
		assetType = strings.TrimSpace(*req.AssetType)
	}
	name := current.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	if assetType == "" || name == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "assetType and name cannot be empty", nil, false)
		return
	}
	currentArtifactID := current.CurrentArtifactID
	if req.CurrentArtifactID != nil {
		value := strings.TrimSpace(*req.CurrentArtifactID)
		if value != "" && !s.artifactBelongsToProject(r, project, value) {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "currentArtifactId is not in this project", nil, false)
			return
		}
		currentArtifactID = stringPtrFromValue(value)
	}
	metadata := current.Metadata
	if len(req.Metadata) > 0 {
		var ok bool
		metadata, ok = jsonObjectOrDefault(w, r, req.Metadata)
		if !ok {
			return
		}
	}
	item, err := scanAsset(s.db.QueryRow(r.Context(), `
		UPDATE assets
		SET asset_type = $3,
		    name = $4,
		    description = COALESCE($5, description),
		    current_artifact_id = $6,
		    metadata = $7
		WHERE id = $1 AND project_id = $2
		RETURNING id, organization_id, project_id, asset_type, name, description, current_artifact_id, metadata, created_by, created_at, updated_at
	`, current.ID, project.ID, assetType, name, req.Description, currentArtifactID, metadata))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) deleteAsset(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetDelete)
	if !ok {
		return
	}
	if _, err := s.asset(r, project.ID, r.PathValue("assetId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := s.db.Exec(r.Context(), `DELETE FROM assets WHERE id = $1 AND project_id = $2`, r.PathValue("assetId"), project.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]bool{"deleted": true}, nil)
}

func (s *Server) createAssetUploadURL(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite)
	if !ok {
		return
	}
	if s.storage == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "object storage is not configured", nil, true)
		return
	}
	var req struct {
		FileName       string `json:"fileName"`
		MimeType       string `json:"mimeType"`
		AssetType      string `json:"assetType"`
		ExpiresSeconds int    `json:"expiresSeconds"`
	}
	if !decode(w, r, &req) {
		return
	}
	fileName := cleanFileName(req.FileName)
	mimeType := strings.TrimSpace(req.MimeType)
	if fileName == "" || mimeType == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "fileName and mimeType are required", nil, false)
		return
	}
	expires := time.Duration(req.ExpiresSeconds) * time.Second
	if expires <= 0 {
		expires = 15 * time.Minute
	}
	if expires > time.Hour {
		expires = time.Hour
	}
	storageKey := fmt.Sprintf("uploads/%s/%s/%s/%s", project.OrganizationID, project.ID, randomStorageSegment(), fileName)
	put, err := s.storage.PresignPutObject(r.Context(), storageKey, mimeType, expires)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, map[string]any{
		"storageKey": put.StorageKey,
		"uploadUrl":  put.URL,
		"method":     put.Method,
		"headers":    put.Headers,
		"expiresAt":  put.ExpiresAt,
	}, nil)
}

func (s *Server) createAssetVariant(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite)
	if !ok {
		return
	}
	asset, err := s.asset(r, project.ID, r.PathValue("assetId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req struct {
		VariantType     string          `json:"variantType"`
		StorageKey      string          `json:"storageKey"`
		MimeType        string          `json:"mimeType"`
		ByteSize        *int64          `json:"byteSize"`
		Width           *int            `json:"width"`
		Height          *int            `json:"height"`
		DurationSeconds *float64        `json:"durationSeconds"`
		Checksum        string          `json:"checksum"`
		Metadata        json.RawMessage `json:"metadata"`
		IdempotencyKey  string          `json:"idempotencyKey,omitempty"`
	}
	if !decode(w, r, &req) {
		return
	}
	variantType := strings.TrimSpace(req.VariantType)
	if variantType == "" {
		variantType = "original"
	}
	storageKey := strings.TrimSpace(req.StorageKey)
	mimeType := strings.TrimSpace(req.MimeType)
	if storageKey == "" || mimeType == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "storageKey and mimeType are required", nil, false)
		return
	}
	metadata, ok := jsonObjectOrDefault(w, r, req.Metadata)
	if !ok {
		return
	}
	idempotency := idempotencyKey(r, req.IdempotencyKey)
	requestHash := idempotencyRequestHash(map[string]any{
		"assetId":         asset.ID,
		"variantType":     variantType,
		"storageKey":      storageKey,
		"mimeType":        mimeType,
		"byteSize":        req.ByteSize,
		"width":           req.Width,
		"height":          req.Height,
		"durationSeconds": req.DurationSeconds,
		"checksum":        strings.TrimSpace(req.Checksum),
		"metadata":        metadata,
	})
	idempotencyState, ok := s.prepareIdempotency(w, r, project.OrganizationID, "assets:variants:"+asset.ID, idempotency, requestHash)
	if !ok {
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.failIdempotency(r.Context(), idempotencyState)
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())

	var artifactID string
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO artifacts(organization_id, project_id, type, storage_key, mime_type, content_hash, metadata, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`, project.OrganizationID, project.ID, asset.AssetType, storageKey, mimeType, nullableString(strings.TrimSpace(req.Checksum)), metadata, principal.UserID).Scan(&artifactID); err != nil {
		s.failIdempotency(r.Context(), idempotencyState)
		s.writeError(w, r, err)
		return
	}
	var mediaFileID string
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO media_files(organization_id, project_id, artifact_id, storage_key, mime_type, byte_size, width, height, duration_seconds, checksum)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id
	`, project.OrganizationID, project.ID, artifactID, storageKey, mimeType, req.ByteSize, req.Width, req.Height, req.DurationSeconds, nullableString(strings.TrimSpace(req.Checksum))).Scan(&mediaFileID); err != nil {
		s.failIdempotency(r.Context(), idempotencyState)
		s.writeError(w, r, err)
		return
	}
	var variant AssetVariant
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO media_variants(media_file_id, variant_type, storage_key, mime_type, metadata)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, media_file_id, variant_type, storage_key, mime_type, metadata, created_at
	`, mediaFileID, variantType, storageKey, mimeType, metadata).Scan(&variant.ID, &variant.MediaFileID, &variant.VariantType, &variant.StorageKey, &variant.MimeType, &variant.Metadata, &variant.CreatedAt); err != nil {
		s.failIdempotency(r.Context(), idempotencyState)
		s.writeError(w, r, err)
		return
	}
	var updated Asset
	updated, err = scanAsset(tx.QueryRow(r.Context(), `
		UPDATE assets
		SET current_artifact_id = $3
		WHERE id = $1 AND project_id = $2
		RETURNING id, organization_id, project_id, asset_type, name, description, current_artifact_id, metadata, created_by, created_at, updated_at
	`, asset.ID, project.ID, artifactID))
	if err != nil {
		s.failIdempotency(r.Context(), idempotencyState)
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.failIdempotency(r.Context(), idempotencyState)
		s.writeError(w, r, err)
		return
	}
	response := map[string]any{
		"asset":       updated,
		"artifactId":  artifactID,
		"mediaFileId": mediaFileID,
		"variant":     variant,
	}
	if err := s.completeIdempotency(r.Context(), idempotencyState, response); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, response, nil)
}

func (s *Server) requireProjectAccess(w http.ResponseWriter, r *http.Request, principal auth.Principal, projectID, permission string) (Project, bool) {
	project, err := s.project(r, projectID)
	if err != nil {
		s.writeError(w, r, err)
		return Project{}, false
	}
	if !s.authorize(w, r, principal, permission, authz.Resource{ProjectID: project.ID}) {
		return Project{}, false
	}
	return project, true
}

func (s *Server) asset(r *http.Request, projectID, assetID string) (Asset, error) {
	return scanAsset(s.db.QueryRow(r.Context(), `
		SELECT id, organization_id, project_id, asset_type, name, description, current_artifact_id, metadata, created_by, created_at, updated_at
		FROM assets
		WHERE project_id = $1 AND id = $2
	`, projectID, assetID))
}

func (s *Server) artifactBelongsToProject(r *http.Request, project Project, artifactID string) bool {
	var ok bool
	err := s.db.QueryRow(r.Context(), `
		SELECT EXISTS(
			SELECT 1
			FROM artifacts
			WHERE id = $1 AND organization_id = $2 AND project_id = $3
		)
	`, artifactID, project.OrganizationID, project.ID).Scan(&ok)
	return err == nil && ok
}

type rowScan interface {
	Scan(...any) error
}

func scanAsset(row rowScan) (Asset, error) {
	var item Asset
	var description, currentArtifactID, createdBy sql.NullString
	var metadata []byte
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.ProjectID,
		&item.AssetType,
		&item.Name,
		&description,
		&currentArtifactID,
		&metadata,
		&createdBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	item.Description = stringPtrFromNull(description)
	item.CurrentArtifactID = stringPtrFromNull(currentArtifactID)
	item.CreatedBy = stringPtrFromNull(createdBy)
	item.Metadata = rawOrDefaultBytes(metadata, "{}")
	return item, err
}

func jsonObjectOrDefault(w http.ResponseWriter, r *http.Request, raw json.RawMessage) (json.RawMessage, bool) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), true
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "metadata must be a JSON object", nil, false)
		return nil, false
	}
	return raw, true
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func stringPtrFromNull(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func stringPtrFromValue(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	value = strings.TrimSpace(value)
	return &value
}

func rawOrDefaultBytes(raw []byte, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return json.RawMessage(raw)
}

func cleanFileName(fileName string) string {
	cleaned := path.Base(strings.TrimSpace(strings.ReplaceAll(fileName, "\\", "/")))
	if cleaned == "." || cleaned == "/" {
		return ""
	}
	return cleaned
}

func randomStorageSegment() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
