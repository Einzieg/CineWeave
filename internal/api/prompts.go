package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/jackc/pgx/v5"
)

type PromptTemplate struct {
	ID             string                `json:"id"`
	OrganizationID *string               `json:"organizationId,omitempty"`
	TemplateKey    string                `json:"templateKey"`
	Name           string                `json:"name"`
	Description    *string               `json:"description,omitempty"`
	Purpose        string                `json:"purpose"`
	Modality       string                `json:"modality"`
	TaskType       string                `json:"taskType"`
	Scope          string                `json:"scope"`
	Status         string                `json:"status"`
	IsSystem       bool                  `json:"isSystem"`
	ActiveVersion  *PromptVersionSummary `json:"activeVersion,omitempty"`
	CreatedBy      *string               `json:"createdBy,omitempty"`
	CreatedAt      time.Time             `json:"createdAt"`
	UpdatedAt      *time.Time            `json:"updatedAt,omitempty"`
}

type PromptVersionSummary struct {
	ID          string     `json:"id"`
	Version     int        `json:"version"`
	Status      string     `json:"status"`
	Title       *string    `json:"title,omitempty"`
	ContentHash string     `json:"contentHash"`
	CreatedAt   *time.Time `json:"createdAt,omitempty"`
	ActivatedAt *time.Time `json:"activatedAt,omitempty"`
}

type PromptVersion struct {
	ID              string          `json:"id"`
	TemplateID      string          `json:"templateId"`
	Version         int             `json:"version"`
	Status          string          `json:"status"`
	Title           *string         `json:"title,omitempty"`
	Content         string          `json:"content"`
	ContentFormat   string          `json:"contentFormat"`
	VariablesSchema json.RawMessage `json:"variablesSchema"`
	Metadata        json.RawMessage `json:"metadata"`
	ContentHash     string          `json:"contentHash"`
	CreatedBy       *string         `json:"createdBy,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	ActivatedAt     *time.Time      `json:"activatedAt,omitempty"`
}

type PromptBinding struct {
	ID              string    `json:"id"`
	OrganizationID  string    `json:"organizationId"`
	ProjectID       *string   `json:"projectId,omitempty"`
	TemplateKey     string    `json:"templateKey"`
	PromptVersionID string    `json:"promptVersionId"`
	Status          string    `json:"status"`
	CreatedBy       *string   `json:"createdBy,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

func (s *Server) listPromptTemplates(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionPromptRead, authz.Resource{OrganizationID: orgID}) {
		return
	}
	rows, err := s.db.Query(r.Context(), promptTemplateSelect(`
		WHERE pt.organization_id IS NULL OR pt.organization_id = $1
		ORDER BY pt.organization_id NULLS FIRST, pt.template_key
	`), orgID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]PromptTemplate, 0)
	for rows.Next() {
		item, err := scanPromptTemplate(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createPromptTemplate(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req struct {
		OrganizationID string  `json:"organizationId"`
		TemplateKey    string  `json:"templateKey"`
		Name           string  `json:"name"`
		Description    *string `json:"description"`
		Purpose        string  `json:"purpose"`
		Modality       string  `json:"modality"`
		TaskType       string  `json:"taskType"`
	}
	if !decode(w, r, &req) {
		return
	}
	orgID := firstNonEmptyString(req.OrganizationID, organizationID(r, principal))
	if !s.authorize(w, r, principal, authz.PermissionPromptManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	req.TemplateKey = strings.TrimSpace(req.TemplateKey)
	req.Name = strings.TrimSpace(req.Name)
	req.Purpose = strings.TrimSpace(req.Purpose)
	req.Modality = strings.TrimSpace(req.Modality)
	req.TaskType = strings.TrimSpace(req.TaskType)
	if req.TemplateKey == "" || req.Name == "" || req.Purpose == "" || req.Modality == "" || req.TaskType == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "templateKey, name, purpose, modality, and taskType are required", nil, false)
		return
	}
	var templateID string
	err := s.db.QueryRow(r.Context(), `
		INSERT INTO prompt_templates(
			organization_id, template_key, name, description, purpose, modality, task_type,
			scope, status, is_system, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'organization', 'active', false, $8)
		ON CONFLICT (organization_id, template_key) DO NOTHING
		RETURNING id
	`, orgID, req.TemplateKey, req.Name, req.Description, req.Purpose, req.Modality, req.TaskType, principal.UserID).Scan(&templateID)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteError(w, r, http.StatusConflict, "CONFLICT", "prompt template already exists", nil, false)
		return
	}
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := s.promptTemplate(r.Context(), templateID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) getPromptTemplate(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	item, err := s.promptTemplate(r.Context(), r.PathValue("templateId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorizePromptTemplate(w, r, principal, authz.PermissionPromptRead, item) {
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) updatePromptTemplate(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	current, err := s.promptTemplate(r.Context(), r.PathValue("templateId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorizePromptTemplate(w, r, principal, authz.PermissionPromptManage, current) {
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Purpose     *string `json:"purpose"`
		Modality    *string `json:"modality"`
		TaskType    *string `json:"taskType"`
		Status      *string `json:"status"`
	}
	if !decode(w, r, &req) {
		return
	}
	name := current.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	description := current.Description
	if req.Description != nil {
		description = req.Description
	}
	purpose := current.Purpose
	if req.Purpose != nil {
		purpose = strings.TrimSpace(*req.Purpose)
	}
	modality := current.Modality
	if req.Modality != nil {
		modality = strings.TrimSpace(*req.Modality)
	}
	taskType := current.TaskType
	if req.TaskType != nil {
		taskType = strings.TrimSpace(*req.TaskType)
	}
	status := current.Status
	if req.Status != nil {
		status = strings.TrimSpace(*req.Status)
	}
	if name == "" || purpose == "" || modality == "" || taskType == "" || (status != "active" && status != "archived") {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "template fields are invalid", nil, false)
		return
	}
	if _, err := s.db.Exec(r.Context(), `
		UPDATE prompt_templates
		SET name = $2, description = $3, purpose = $4, modality = $5, task_type = $6, status = $7
		WHERE id = $1
	`, current.ID, name, description, purpose, modality, taskType, status); err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := s.promptTemplate(r.Context(), current.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) listPromptVersions(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	template, err := s.promptTemplate(r.Context(), r.PathValue("templateId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorizePromptTemplate(w, r, principal, authz.PermissionPromptRead, template) {
		return
	}
	rows, err := s.db.Query(r.Context(), promptVersionSelect(`
		WHERE COALESCE(template_id, prompt_template_id) = $1
		ORDER BY COALESCE(version, version_no) DESC
	`), template.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]PromptVersion, 0)
	for rows.Next() {
		item, err := scanPromptVersion(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createPromptVersion(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	template, err := s.promptTemplate(r.Context(), r.PathValue("templateId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorizePromptTemplate(w, r, principal, authz.PermissionPromptManage, template) {
		return
	}
	var req struct {
		Title           *string         `json:"title"`
		Content         string          `json:"content"`
		ContentFormat   string          `json:"contentFormat"`
		VariablesSchema json.RawMessage `json:"variablesSchema"`
		Metadata        json.RawMessage `json:"metadata"`
		Activate        bool            `json:"activate"`
	}
	if !decode(w, r, &req) {
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "content is required", nil, false)
		return
	}
	contentFormat := strings.TrimSpace(req.ContentFormat)
	if contentFormat == "" {
		contentFormat = "text"
	}
	if contentFormat != "text" && contentFormat != "markdown" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "contentFormat is invalid", nil, false)
		return
	}
	variablesSchema, ok := jsonObjectRaw(w, r, req.VariablesSchema, "variablesSchema")
	if !ok {
		return
	}
	metadata, ok := jsonObjectRaw(w, r, req.Metadata, "metadata")
	if !ok {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var versionNo int
	if err := tx.QueryRow(r.Context(), `
		SELECT COALESCE(MAX(COALESCE(version, version_no)), 0) + 1
		FROM prompt_versions
		WHERE COALESCE(template_id, prompt_template_id) = $1
	`, template.ID).Scan(&versionNo); err != nil {
		s.writeError(w, r, err)
		return
	}
	status := "draft"
	activatedAt := any(nil)
	if req.Activate {
		status = "active"
		activatedAt = time.Now().UTC()
		if _, err := tx.Exec(r.Context(), `UPDATE prompt_versions SET status = 'archived' WHERE template_id = $1 AND status = 'active'`, template.ID); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	var versionID string
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO prompt_versions(
			prompt_template_id, template_id, version_no, version, status, title, content, content_format,
			variables_schema, metadata, content_hash, created_by, activated_at
		)
		VALUES ($1, $1, $2, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id
	`, template.ID, versionNo, status, req.Title, content, contentFormat, variablesSchema, metadata, promptsvc.HashText(content), principal.UserID, activatedAt).Scan(&versionID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := s.promptVersion(r.Context(), versionID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) activatePromptVersion(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	version, err := s.promptVersion(r.Context(), r.PathValue("versionId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	template, err := s.promptTemplate(r.Context(), version.TemplateID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorizePromptTemplate(w, r, principal, authz.PermissionPromptManage, template) {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `UPDATE prompt_versions SET status = 'archived' WHERE template_id = $1 AND status = 'active'`, template.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE prompt_versions SET status = 'active', activated_at = now() WHERE id = $1`, version.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	updated, err := s.promptVersion(r.Context(), version.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, updated, nil)
}

func (s *Server) listPromptBindings(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	projectID := r.URL.Query().Get("filter[projectId]")
	if projectID != "" {
		if !s.authorize(w, r, principal, authz.PermissionPromptRead, authz.Resource{ProjectID: projectID}) {
			return
		}
	} else if !s.authorize(w, r, principal, authz.PermissionPromptRead, authz.Resource{OrganizationID: orgID}) {
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, template_key, prompt_version_id, status, created_by, created_at, updated_at
		FROM prompt_bindings
		WHERE organization_id = $1
		  AND ($2 = '' OR project_id = $2::uuid)
		ORDER BY created_at DESC
	`, orgID, projectID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]PromptBinding, 0)
	for rows.Next() {
		item, err := scanPromptBinding(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createPromptBinding(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req struct {
		OrganizationID  string `json:"organizationId"`
		ProjectID       string `json:"projectId"`
		TemplateKey     string `json:"templateKey"`
		PromptVersionID string `json:"promptVersionId"`
		Status          string `json:"status"`
	}
	if !decode(w, r, &req) {
		return
	}
	orgID := firstNonEmptyString(req.OrganizationID, organizationID(r, principal))
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.TemplateKey = strings.TrimSpace(req.TemplateKey)
	req.PromptVersionID = strings.TrimSpace(req.PromptVersionID)
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "active"
	}
	if req.PromptVersionID == "" || (status != "active" && status != "disabled") {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "promptVersionId and valid status are required", nil, false)
		return
	}
	version, err := s.promptVersion(r.Context(), req.PromptVersionID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	template, err := s.promptTemplate(r.Context(), version.TemplateID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if req.TemplateKey == "" {
		req.TemplateKey = template.TemplateKey
	}
	resource := authz.Resource{OrganizationID: orgID}
	if req.ProjectID != "" {
		resource = authz.Resource{ProjectID: req.ProjectID}
	}
	if !s.authorize(w, r, principal, authz.PermissionPromptManage, resource) {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if status == "active" {
		if _, err := tx.Exec(r.Context(), `
			UPDATE prompt_bindings
			SET status = 'disabled'
			WHERE organization_id = $1
			  AND template_key = $2
			  AND status = 'active'
			  AND (($3 = '' AND project_id IS NULL) OR project_id = NULLIF($3, '')::uuid)
		`, orgID, req.TemplateKey, req.ProjectID); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	var bindingID string
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO prompt_bindings(organization_id, project_id, template_key, prompt_version_id, status, created_by)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, $6)
		RETURNING id
	`, orgID, req.ProjectID, req.TemplateKey, req.PromptVersionID, status, principal.UserID).Scan(&bindingID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := s.promptBinding(r.Context(), bindingID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) deletePromptBinding(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	item, err := s.promptBinding(r.Context(), r.PathValue("bindingId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	resource := authz.Resource{OrganizationID: item.OrganizationID}
	if item.ProjectID != nil {
		resource = authz.Resource{ProjectID: *item.ProjectID}
	}
	if !s.authorize(w, r, principal, authz.PermissionPromptManage, resource) {
		return
	}
	if _, err := s.db.Exec(r.Context(), `DELETE FROM prompt_bindings WHERE id = $1`, item.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]bool{"deleted": true}, nil)
}

func (s *Server) renderPromptTest(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req struct {
		OrganizationID string         `json:"organizationId"`
		ProjectID      string         `json:"projectId"`
		TemplateKey    string         `json:"templateKey"`
		Variables      map[string]any `json:"variables"`
	}
	if !decode(w, r, &req) {
		return
	}
	orgID := firstNonEmptyString(req.OrganizationID, organizationID(r, principal))
	resource := authz.Resource{OrganizationID: orgID}
	if strings.TrimSpace(req.ProjectID) != "" {
		resource = authz.Resource{ProjectID: strings.TrimSpace(req.ProjectID)}
	}
	if !s.authorize(w, r, principal, authz.PermissionPromptRead, resource) {
		return
	}
	resolved, err := promptsvc.NewService(s.db).Resolve(r.Context(), promptsvc.ResolveRequest{
		OrganizationID: orgID,
		ProjectID:      req.ProjectID,
		TemplateKey:    req.TemplateKey,
	})
	if err != nil {
		s.writePromptError(w, r, err)
		return
	}
	rendered, err := promptsvc.Render(resolved, req.Variables)
	if err != nil {
		s.writePromptError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"templateKey":     rendered.TemplateKey,
		"promptVersionId": rendered.PromptVersionID,
		"renderedHash":    rendered.RenderedHash,
		"contentHash":     rendered.ContentHash,
		"promptSource":    rendered.Source,
		"text":            rendered.RenderedText,
	}, nil)
}

func (s *Server) authorizePromptTemplate(w http.ResponseWriter, r *http.Request, principal auth.Principal, permission string, item PromptTemplate) bool {
	orgID := organizationID(r, principal)
	if item.OrganizationID != nil {
		orgID = *item.OrganizationID
	}
	return s.authorize(w, r, principal, permission, authz.Resource{OrganizationID: orgID})
}

func (s *Server) promptTemplate(ctx context.Context, templateID string) (PromptTemplate, error) {
	return scanPromptTemplate(s.db.QueryRow(ctx, promptTemplateSelect(`WHERE pt.id = $1`), templateID))
}

func (s *Server) promptVersion(ctx context.Context, versionID string) (PromptVersion, error) {
	return scanPromptVersion(s.db.QueryRow(ctx, promptVersionSelect(`WHERE id = $1`), versionID))
}

func (s *Server) promptBinding(ctx context.Context, bindingID string) (PromptBinding, error) {
	return scanPromptBinding(s.db.QueryRow(ctx, `
		SELECT id, organization_id, project_id, template_key, prompt_version_id, status, created_by, created_at, updated_at
		FROM prompt_bindings
		WHERE id = $1
	`, bindingID))
}

func promptTemplateSelect(where string) string {
	return `
		SELECT
			pt.id::text, pt.organization_id::text, pt.template_key, pt.name, pt.description, pt.purpose,
			pt.modality, pt.task_type, pt.scope, pt.status, pt.is_system, pt.created_by::text, pt.created_at, pt.updated_at,
			pv.id::text, pv.version_no, pv.status, pv.title, pv.content_hash, pv.created_at, pv.activated_at
		FROM prompt_templates pt
		LEFT JOIN LATERAL (
			SELECT id, COALESCE(version, version_no) AS version_no, status, title, content_hash, created_at, activated_at
			FROM prompt_versions
			WHERE COALESCE(template_id, prompt_template_id) = pt.id
			  AND status = 'active'
			ORDER BY COALESCE(activated_at, created_at) DESC
			LIMIT 1
		) pv ON true
	` + where
}

func promptVersionSelect(where string) string {
	return `
		SELECT id::text, COALESCE(template_id, prompt_template_id)::text, COALESCE(version, version_no), status,
		       title, content, content_format, variables_schema, metadata, content_hash,
		       created_by::text, created_at, activated_at
		FROM prompt_versions
	` + where
}

func scanPromptTemplate(row pgx.Row) (PromptTemplate, error) {
	var item PromptTemplate
	var organizationID, description, createdBy sql.NullString
	var updatedAt sql.NullTime
	var activeID, activeStatus, activeTitle, activeContentHash sql.NullString
	var activeVersion sql.NullInt64
	var activeCreatedAt, activeActivatedAt sql.NullTime
	err := row.Scan(
		&item.ID,
		&organizationID,
		&item.TemplateKey,
		&item.Name,
		&description,
		&item.Purpose,
		&item.Modality,
		&item.TaskType,
		&item.Scope,
		&item.Status,
		&item.IsSystem,
		&createdBy,
		&item.CreatedAt,
		&updatedAt,
		&activeID,
		&activeVersion,
		&activeStatus,
		&activeTitle,
		&activeContentHash,
		&activeCreatedAt,
		&activeActivatedAt,
	)
	item.OrganizationID = stringPtrFromNull(organizationID)
	item.Description = stringPtrFromNull(description)
	item.CreatedBy = stringPtrFromNull(createdBy)
	if updatedAt.Valid {
		item.UpdatedAt = &updatedAt.Time
	}
	if activeID.Valid {
		item.ActiveVersion = &PromptVersionSummary{
			ID:          activeID.String,
			Version:     int(activeVersion.Int64),
			Status:      activeStatus.String,
			Title:       stringPtrFromNull(activeTitle),
			ContentHash: activeContentHash.String,
			CreatedAt:   timePtrFromNull(activeCreatedAt),
			ActivatedAt: timePtrFromNull(activeActivatedAt),
		}
	}
	return item, err
}

func scanPromptVersion(row pgx.Row) (PromptVersion, error) {
	var item PromptVersion
	var title, createdBy sql.NullString
	var activatedAt sql.NullTime
	var variablesSchema, metadata []byte
	err := row.Scan(
		&item.ID,
		&item.TemplateID,
		&item.Version,
		&item.Status,
		&title,
		&item.Content,
		&item.ContentFormat,
		&variablesSchema,
		&metadata,
		&item.ContentHash,
		&createdBy,
		&item.CreatedAt,
		&activatedAt,
	)
	item.Title = stringPtrFromNull(title)
	item.CreatedBy = stringPtrFromNull(createdBy)
	item.VariablesSchema = rawOrDefaultBytes(variablesSchema, "{}")
	item.Metadata = rawOrDefaultBytes(metadata, "{}")
	item.ActivatedAt = timePtrFromNull(activatedAt)
	return item, err
}

func scanPromptBinding(row pgx.Row) (PromptBinding, error) {
	var item PromptBinding
	var projectID, createdBy sql.NullString
	err := row.Scan(&item.ID, &item.OrganizationID, &projectID, &item.TemplateKey, &item.PromptVersionID, &item.Status, &createdBy, &item.CreatedAt, &item.UpdatedAt)
	item.ProjectID = stringPtrFromNull(projectID)
	item.CreatedBy = stringPtrFromNull(createdBy)
	return item, err
}

func jsonObjectRaw(w http.ResponseWriter, r *http.Request, raw json.RawMessage, field string) (json.RawMessage, bool) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return json.RawMessage(`{}`), true
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", field+" must be a JSON object", nil, false)
		return nil, false
	}
	return raw, true
}

func (s *Server) writePromptError(w http.ResponseWriter, r *http.Request, err error) {
	var promptErr promptsvc.Error
	if errors.As(err, &promptErr) {
		status := http.StatusUnprocessableEntity
		if promptErr.Code == promptsvc.CodePromptTemplateNotFound || promptErr.Code == promptsvc.CodePromptVersionNotFound {
			status = http.StatusNotFound
		}
		httpx.WriteError(w, r, status, promptErr.Code, promptErr.Message, nil, false)
		return
	}
	s.writeError(w, r, err)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func timePtrFromNull(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}
