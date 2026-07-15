package api

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type ProjectManualBinding struct {
	ID              string    `json:"id"`
	OrganizationID  string    `json:"organizationId"`
	ProjectID       string    `json:"projectId"`
	ManualKind      string    `json:"manualKind"`
	PromptVersionID string    `json:"promptVersionId"`
	TemplateID      string    `json:"templateId"`
	TemplateKey     string    `json:"templateKey"`
	TemplateName    string    `json:"templateName"`
	Version         int       `json:"version"`
	Status          string    `json:"status"`
	ContentHash     string    `json:"contentHash"`
	CreatedBy       *string   `json:"createdBy,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type projectManualVersion struct {
	PromptVersionID string
	TemplateID      string
	TemplateKey     string
	TemplateName    string
	Purpose         string
	Version         int
	Status          string
	Content         string
	ContentHash     string
}

func (s *Server) listProjectManualTemplates(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionPromptRead, authz.Resource{OrganizationID: orgID}) {
		return
	}
	kind := normalizeProjectManualKind(r.URL.Query().Get("filter[kind]"))
	where := `
		WHERE (pt.organization_id IS NULL OR pt.organization_id = $1)
		  AND pt.purpose IN ('director_manual', 'visual_manual')
		  AND pt.status = 'active'
	`
	args := []any{orgID}
	if kind != "" {
		where += " AND pt.purpose = $2"
		args = append(args, projectManualPurpose(kind))
	}
	where += " ORDER BY pt.organization_id NULLS FIRST, pt.template_key"
	rows, err := s.db.Query(r.Context(), promptTemplateSelect(where), args...)
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
	if err := rows.Err(); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) listProjectManualBindings(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	rows, err := s.db.Query(r.Context(), projectManualBindingSelect(`
		WHERE b.project_id = $1
		  AND b.status = 'active'
		ORDER BY b.manual_kind
	`), project.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]ProjectManualBinding, 0)
	for rows.Next() {
		item, err := scanProjectManualBinding(rows)
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

func (s *Server) bindProjectManual(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	kind := normalizeProjectManualKind(r.PathValue("manualKind"))
	if kind == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "manualKind is invalid", nil, false)
		return
	}
	var req struct {
		PromptVersionID string `json:"promptVersionId"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.PromptVersionID = strings.TrimSpace(req.PromptVersionID)
	if req.PromptVersionID == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "promptVersionId is required", nil, false)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	bindingID, _, err := s.bindProjectManualTx(r.Context(), tx, project.OrganizationID, project.ID, kind, req.PromptVersionID, principal.UserID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := s.projectManualBinding(r.Context(), bindingID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) bindDefaultProjectManualTx(ctx context.Context, tx pgx.Tx, organizationID, projectID, manualKind, createdBy string) (string, error) {
	versionID, err := s.defaultProjectManualVersionIDTx(ctx, tx, manualKind)
	if err != nil {
		return "", err
	}
	_, content, err := s.bindProjectManualTx(ctx, tx, organizationID, projectID, manualKind, versionID, createdBy)
	return content, err
}

func (s *Server) defaultProjectManualVersionIDTx(ctx context.Context, tx pgx.Tx, manualKind string) (string, error) {
	templateKey := defaultProjectManualTemplateKey(manualKind)
	if templateKey == "" {
		return "", newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "manualKind is invalid")
	}
	var versionID string
	err := tx.QueryRow(ctx, `
		SELECT pv.id::text
		FROM prompt_templates pt
		JOIN prompt_versions pv ON pv.template_id = pt.id
		WHERE pt.organization_id IS NULL
		  AND pt.template_key = $1
		  AND pt.status = 'active'
		  AND pv.status = 'active'
		ORDER BY COALESCE(pv.activated_at, pv.created_at) DESC
		LIMIT 1
	`, templateKey).Scan(&versionID)
	if err != nil {
		return "", err
	}
	return versionID, nil
}

func (s *Server) bindProjectManualTx(ctx context.Context, tx pgx.Tx, organizationID, projectID, manualKind, promptVersionID, createdBy string) (string, string, error) {
	if _, err := lockProjectConfigurationTx(ctx, tx, projectID, organizationID); err != nil {
		return "", "", err
	}
	version, err := s.projectManualVersionTx(ctx, tx, promptVersionID)
	if err != nil {
		return "", "", err
	}
	if version.Status != "active" {
		return "", "", newAPIError(http.StatusUnprocessableEntity, "PROMPT_VERSION_NOT_ACTIVE", "manual prompt version must be active")
	}
	if version.Purpose != projectManualPurpose(manualKind) {
		return "", "", newAPIError(http.StatusUnprocessableEntity, "MANUAL_KIND_MISMATCH", "prompt version does not match manual kind")
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_manual_bindings
		SET status = 'disabled'
		WHERE project_id = $1
		  AND manual_kind = $2
		  AND status = 'active'
	`, projectID, manualKind); err != nil {
		return "", "", err
	}
	var bindingID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO project_manual_bindings(
			organization_id, project_id, manual_kind, prompt_version_id, status, created_by
		)
		VALUES ($1, $2, $3, $4, 'active', NULLIF($5, '')::uuid)
		RETURNING id
	`, organizationID, projectID, manualKind, promptVersionID, createdBy).Scan(&bindingID); err != nil {
		return "", "", err
	}
	if manualKind == "director" {
		if _, err := tx.Exec(ctx, `UPDATE projects SET director_manual = $2, revision = revision + 1, updated_at = now() WHERE id = $1`, projectID, version.Content); err != nil {
			return "", "", err
		}
	} else {
		if _, err := tx.Exec(ctx, `UPDATE projects SET visual_manual = $2, revision = revision + 1, updated_at = now() WHERE id = $1`, projectID, version.Content); err != nil {
			return "", "", err
		}
	}
	return bindingID, version.Content, nil
}

func (s *Server) projectManualVersionTx(ctx context.Context, tx pgx.Tx, promptVersionID string) (projectManualVersion, error) {
	var item projectManualVersion
	err := tx.QueryRow(ctx, `
		SELECT pv.id::text, pt.id::text, pt.template_key, pt.name, pt.purpose,
		       COALESCE(pv.version, pv.version_no), pv.status, pv.content, pv.content_hash
		FROM prompt_versions pv
		JOIN prompt_templates pt ON pt.id = COALESCE(pv.template_id, pv.prompt_template_id)
		WHERE pv.id = $1
	`, promptVersionID).Scan(
		&item.PromptVersionID,
		&item.TemplateID,
		&item.TemplateKey,
		&item.TemplateName,
		&item.Purpose,
		&item.Version,
		&item.Status,
		&item.Content,
		&item.ContentHash,
	)
	return item, err
}

func (s *Server) projectManualBinding(ctx context.Context, bindingID string) (ProjectManualBinding, error) {
	return scanProjectManualBinding(s.db.QueryRow(ctx, projectManualBindingSelect(`WHERE b.id = $1`), bindingID))
}

func (s *Server) disableManualBindingsForDirectEdit(ctx context.Context, projectID string, directorEdited, visualEdited bool) error {
	return disableManualBindingsForDirectEditTx(ctx, s.db, projectID, directorEdited, visualEdited)
}

type projectManualBindingExecer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func disableManualBindingsForDirectEditTx(ctx context.Context, db projectManualBindingExecer, projectID string, directorEdited, visualEdited bool) error {
	kinds := make([]string, 0, 2)
	if directorEdited {
		kinds = append(kinds, "director")
	}
	if visualEdited {
		kinds = append(kinds, "visual")
	}
	if len(kinds) == 0 {
		return nil
	}
	_, err := db.Exec(ctx, `
		UPDATE project_manual_bindings
		SET status = 'disabled'
		WHERE project_id = $1
		  AND status = 'active'
		  AND manual_kind = ANY($2::text[])
	`, projectID, kinds)
	return err
}

func projectManualBindingSelect(where string) string {
	return `
		SELECT b.id::text, b.organization_id::text, b.project_id::text, b.manual_kind,
		       b.prompt_version_id::text, pt.id::text, pt.template_key, pt.name,
		       COALESCE(pv.version, pv.version_no), b.status, pv.content_hash,
		       b.created_by::text, b.created_at, b.updated_at
		FROM project_manual_bindings b
		JOIN prompt_versions pv ON pv.id = b.prompt_version_id
		JOIN prompt_templates pt ON pt.id = COALESCE(pv.template_id, pv.prompt_template_id)
	` + where
}

func scanProjectManualBinding(row pgx.Row) (ProjectManualBinding, error) {
	var item ProjectManualBinding
	var createdBy sql.NullString
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.ProjectID,
		&item.ManualKind,
		&item.PromptVersionID,
		&item.TemplateID,
		&item.TemplateKey,
		&item.TemplateName,
		&item.Version,
		&item.Status,
		&item.ContentHash,
		&createdBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	item.CreatedBy = stringPtrFromNull(createdBy)
	return item, err
}

func normalizeProjectManualKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "director", "director_manual":
		return "director"
	case "visual", "visual_manual":
		return "visual"
	default:
		return ""
	}
}

func projectManualPurpose(manualKind string) string {
	switch manualKind {
	case "director":
		return "director_manual"
	case "visual":
		return "visual_manual"
	default:
		return ""
	}
}

func defaultProjectManualTemplateKey(manualKind string) string {
	switch manualKind {
	case "director":
		return "default_director_manual"
	case "visual":
		return "default_visual_manual"
	default:
		return ""
	}
}
