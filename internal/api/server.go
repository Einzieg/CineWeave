package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/client"
)

type Server struct {
	db        *pgxpool.Pool
	auth      *auth.Service
	providers *provider.Service
	storage   *storage.Client
	temporal  client.Client
}

type Organization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"createdAt"`
}

type Workspace struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	Name           string    `json:"name"`
	CreatedAt      time.Time `json:"createdAt"`
}

type Project struct {
	ID             string          `json:"id"`
	OrganizationID string          `json:"organizationId"`
	WorkspaceID    string          `json:"workspaceId"`
	Name           string          `json:"name"`
	Description    *string         `json:"description,omitempty"`
	ProjectType    *string         `json:"projectType,omitempty"`
	AspectRatio    *string         `json:"aspectRatio,omitempty"`
	Settings       json.RawMessage `json:"settings"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

func New(pool *pgxpool.Pool, authService *auth.Service, providerService *provider.Service, storageClient *storage.Client, temporalClient client.Client) *Server {
	return &Server{db: pool, auth: authService, providers: providerService, storage: storageClient, temporal: temporalClient}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", httpx.HealthHandler("api"))
	mux.HandleFunc("GET /readyz", httpx.HealthHandler("api"))

	mux.HandleFunc("POST /api/auth/register", s.register)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/refresh", s.refresh)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /api/auth/me", s.withAuth(s.me))
	mux.HandleFunc("POST /api/provider-webhooks/{providerAccountId}/{webhookSecret}", s.providerWebhook)

	mux.HandleFunc("GET /api/organizations", s.withAuth(s.listOrganizations))
	mux.HandleFunc("POST /api/organizations", s.withAuth(s.createOrganization))
	mux.HandleFunc("GET /api/organizations/{organizationId}", s.withAuth(s.getOrganization))

	mux.HandleFunc("GET /api/workspaces", s.withAuth(s.listWorkspaces))
	mux.HandleFunc("POST /api/workspaces", s.withAuth(s.createWorkspace))
	mux.HandleFunc("GET /api/workspaces/{workspaceId}", s.withAuth(s.getWorkspace))

	mux.HandleFunc("GET /api/projects", s.withAuth(s.listProjects))
	mux.HandleFunc("POST /api/projects", s.withAuth(s.createProject))
	mux.HandleFunc("GET /api/projects/{projectId}", s.withAuth(s.getProject))
	mux.HandleFunc("PATCH /api/projects/{projectId}", s.withAuth(s.updateProject))
	mux.HandleFunc("DELETE /api/projects/{projectId}", s.withAuth(s.deleteProject))
	mux.HandleFunc("GET /api/projects/{projectId}/assets", s.withAuth(s.listAssets))
	mux.HandleFunc("POST /api/projects/{projectId}/assets", s.withAuth(s.createAsset))
	mux.HandleFunc("POST /api/projects/{projectId}/assets/upload-url", s.withAuth(s.createAssetUploadURL))
	mux.HandleFunc("GET /api/projects/{projectId}/assets/{assetId}", s.withAuth(s.getAsset))
	mux.HandleFunc("PATCH /api/projects/{projectId}/assets/{assetId}", s.withAuth(s.updateAsset))
	mux.HandleFunc("DELETE /api/projects/{projectId}/assets/{assetId}", s.withAuth(s.deleteAsset))
	mux.HandleFunc("POST /api/projects/{projectId}/assets/{assetId}/variants", s.withAuth(s.createAssetVariant))

	mux.HandleFunc("GET /api/providers/connectors", s.withAuth(s.listProviderConnectors))
	mux.HandleFunc("POST /api/providers/connectors/import", s.withAuth(s.importProviderConnector))
	mux.HandleFunc("GET /api/providers/accounts", s.withAuth(s.listProviderAccounts))
	mux.HandleFunc("POST /api/providers/accounts", s.withAuth(s.createProviderAccount))
	mux.HandleFunc("GET /api/providers/accounts/{accountId}", s.withAuth(s.getProviderAccount))
	mux.HandleFunc("PATCH /api/providers/accounts/{accountId}", s.withAuth(s.updateProviderAccount))
	mux.HandleFunc("DELETE /api/providers/accounts/{accountId}", s.withAuth(s.deleteProviderAccount))
	mux.HandleFunc("POST /api/providers/accounts/{accountId}/credentials/rotate", s.withAuth(s.rotateProviderCredential))
	mux.HandleFunc("POST /api/providers/accounts/{accountId}/discover-models", s.withAuth(s.discoverProviderModels))
	mux.HandleFunc("GET /api/providers/accounts/{accountId}/models", s.withAuth(s.listProviderModels))
	mux.HandleFunc("POST /api/providers/accounts/{accountId}/models", s.withAuth(s.createProviderModel))
	mux.HandleFunc("PATCH /api/providers/models/{modelId}", s.withAuth(s.updateProviderModel))
	mux.HandleFunc("POST /api/providers/models/{modelId}/test", s.withAuth(s.testProviderModel))
	mux.HandleFunc("POST /api/providers/manifests/validate", s.withAuth(s.validateProviderManifest))
	mux.HandleFunc("POST /api/providers/manifests/test-run", s.withAuth(s.runProviderManifestTest))
	mux.HandleFunc("GET /api/model-profiles", s.withAuth(s.listModelProfiles))
	mux.HandleFunc("POST /api/model-profiles", s.withAuth(s.createModelProfile))
	mux.HandleFunc("PATCH /api/model-profiles/{profileId}", s.withAuth(s.updateModelProfile))
	mux.HandleFunc("POST /api/model-profiles/{profileId}/bindings", s.withAuth(s.createModelProfileBinding))
	mux.HandleFunc("DELETE /api/model-profiles/{profileId}/bindings/{bindingId}", s.withAuth(s.deleteModelProfileBinding))
	mux.HandleFunc("GET /api/provider-call-logs", s.withAuth(s.listProviderCallLogs))
	mux.HandleFunc("GET /api/provider-usage/summary", s.withAuth(s.getProviderUsageSummary))
	mux.HandleFunc("POST /api/workflow-runs", s.withAuth(s.createWorkflowRun))
	mux.HandleFunc("GET /api/workflow-runs", s.withAuth(s.listWorkflowRuns))
	mux.HandleFunc("GET /api/workflow-runs/{workflowRunId}", s.withAuth(s.getWorkflowRun))
	mux.HandleFunc("GET /api/workflow-runs/{workflowRunId}/nodes", s.withAuth(s.listWorkflowNodeRuns))
	mux.HandleFunc("GET /api/artifacts", s.withAuth(s.listArtifacts))
	mux.HandleFunc("GET /api/artifacts/{artifactId}", s.withAuth(s.getArtifact))
	mux.HandleFunc("POST /api/artifacts/{artifactId}/preview-url", s.withAuth(s.createArtifactPreviewURL))
	mux.HandleFunc("GET /api/media-files/{mediaFileId}", s.withAuth(s.getMediaFile))
	mux.HandleFunc("POST /api/media-files/{mediaFileId}/download-url", s.withAuth(s.createMediaFileDownloadURL))

	return httpx.WithCORS(httpx.WithRequestID(mux))
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var req auth.RegisterRequest
	if !decode(w, r, &req) {
		return
	}
	resp, err := s.auth.Register(r.Context(), req, r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, resp, nil)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req auth.LoginRequest
	if !decode(w, r, &req) {
		return
	}
	resp, err := s.auth.Login(r.Context(), req, r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, resp, nil)
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	var req auth.RefreshRequest
	if !decode(w, r, &req) {
		return
	}
	resp, err := s.auth.Refresh(r.Context(), req, r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, resp, nil)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	var req auth.RefreshRequest
	if !decode(w, r, &req) {
		return
	}
	if err := s.auth.Logout(r.Context(), req); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]bool{"ok": true}, nil)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	user, err := s.auth.Me(r.Context(), principal)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"user":           user,
		"organizationId": principal.OrganizationID,
	}, nil)
}

func (s *Server) listOrganizations(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	rows, err := s.db.Query(r.Context(), `
		SELECT o.id, o.name, o.slug, o.created_at
		FROM organizations o
		JOIN organization_members om ON om.organization_id = o.id
		WHERE om.user_id = $1 AND om.status = 'active'
		ORDER BY o.created_at
	`, principal.UserID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()

	items := make([]Organization, 0)
	for rows.Next() {
		var item Organization
		if err := rows.Scan(&item.ID, &item.Name, &item.Slug, &item.CreatedAt); err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createOrganization(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	orgID, err := s.auth.CreateOrganization(r.Context(), principal.UserID, req.Name)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	org, err := s.organization(r, orgID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, org, nil)
}

func (s *Server) getOrganization(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := r.PathValue("organizationId")
	if err := s.ensureOrganizationMember(r, principal.UserID, orgID); err != nil {
		s.writeError(w, r, err)
		return
	}
	org, err := s.organization(r, orgID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, org, nil)
}

func (s *Server) listWorkspaces(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if orgID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "ORGANIZATION_REQUIRED", "organization context is required", nil, false)
		return
	}
	if err := s.ensureOrganizationMember(r, principal.UserID, orgID); err != nil {
		s.writeError(w, r, err)
		return
	}

	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, name, created_at
		FROM workspaces
		WHERE organization_id = $1
		ORDER BY created_at
	`, orgID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()

	items := make([]Workspace, 0)
	for rows.Next() {
		var item Workspace
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.Name, &item.CreatedAt); err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createWorkspace(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req struct {
		OrganizationID string `json:"organizationId"`
		Name           string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	orgID := req.OrganizationID
	if orgID == "" {
		orgID = organizationID(r, principal)
	}
	if strings.TrimSpace(req.Name) == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "name is required", nil, false)
		return
	}
	if err := s.ensureOrganizationMember(r, principal.UserID, orgID); err != nil {
		s.writeError(w, r, err)
		return
	}

	var item Workspace
	err := s.db.QueryRow(r.Context(), `
		INSERT INTO workspaces(organization_id, name)
		VALUES ($1, $2)
		RETURNING id, organization_id, name, created_at
	`, orgID, strings.TrimSpace(req.Name)).Scan(&item.ID, &item.OrganizationID, &item.Name, &item.CreatedAt)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) getWorkspace(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var item Workspace
	err := s.db.QueryRow(r.Context(), `
		SELECT id, organization_id, name, created_at
		FROM workspaces
		WHERE id = $1
	`, r.PathValue("workspaceId")).Scan(&item.ID, &item.OrganizationID, &item.Name, &item.CreatedAt)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.ensureOrganizationMember(r, principal.UserID, item.OrganizationID); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if orgID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "ORGANIZATION_REQUIRED", "organization context is required", nil, false)
		return
	}
	if err := s.ensureOrganizationMember(r, principal.UserID, orgID); err != nil {
		s.writeError(w, r, err)
		return
	}
	workspaceID := r.URL.Query().Get("filter[workspaceId]")

	query := `
		SELECT id, organization_id, workspace_id, name, description, project_type, aspect_ratio, settings, created_at, updated_at
		FROM projects
		WHERE organization_id = $1
	`
	args := []any{orgID}
	if workspaceID != "" {
		query += " AND workspace_id = $2"
		args = append(args, workspaceID)
	}
	query += " ORDER BY created_at DESC LIMIT 100"

	rows, err := s.db.Query(r.Context(), query, args...)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()

	items := make([]Project, 0)
	for rows.Next() {
		item, err := scanProject(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req struct {
		WorkspaceID string          `json:"workspaceId"`
		Name        string          `json:"name"`
		Description *string         `json:"description"`
		ProjectType *string         `json:"projectType"`
		AspectRatio *string         `json:"aspectRatio"`
		Settings    json.RawMessage `json:"settings"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.WorkspaceID) == "" || strings.TrimSpace(req.Name) == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "workspaceId and name are required", nil, false)
		return
	}
	settings := req.Settings
	if len(settings) == 0 {
		settings = json.RawMessage(`{}`)
	}

	var orgID string
	err := s.db.QueryRow(r.Context(), `SELECT organization_id FROM workspaces WHERE id = $1`, req.WorkspaceID).Scan(&orgID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.ensureOrganizationMember(r, principal.UserID, orgID); err != nil {
		s.writeError(w, r, err)
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())

	var item Project
	err = tx.QueryRow(r.Context(), `
		INSERT INTO projects(organization_id, workspace_id, name, description, project_type, aspect_ratio, settings, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, organization_id, workspace_id, name, description, project_type, aspect_ratio, settings, created_at, updated_at
	`, orgID, req.WorkspaceID, strings.TrimSpace(req.Name), req.Description, req.ProjectType, req.AspectRatio, settings, principal.UserID).
		Scan(&item.ID, &item.OrganizationID, &item.WorkspaceID, &item.Name, &item.Description, &item.ProjectType, &item.AspectRatio, &item.Settings, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	if _, err := tx.Exec(r.Context(), `
		INSERT INTO project_members(project_id, user_id, status)
		VALUES ($1, $2, 'active')
	`, item.ID, principal.UserID); err != nil {
		s.writeError(w, r, err)
		return
	}

	var roleID string
	if err := tx.QueryRow(r.Context(), `
		SELECT id FROM roles
		WHERE organization_id IS NULL AND role_key = 'project_owner' AND scope = 'project'
	`).Scan(&roleID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO role_bindings(
			organization_id, role_id, subject_type, subject_user_id,
			resource_type, resource_project_id, created_by
		)
		VALUES ($1, $2, 'user', $3, 'project', $4, $3)
		ON CONFLICT DO NOTHING
	`, orgID, roleID, principal.UserID, item.ID); err != nil {
		s.writeError(w, r, err)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	item, err := s.project(r, r.PathValue("projectId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.ensureProjectMember(r, principal.UserID, item.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	projectID := r.PathValue("projectId")
	item, err := s.project(r, projectID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.ensureProjectMember(r, principal.UserID, item.ID); err != nil {
		s.writeError(w, r, err)
		return
	}

	var req struct {
		Name        *string         `json:"name"`
		Description *string         `json:"description"`
		ProjectType *string         `json:"projectType"`
		AspectRatio *string         `json:"aspectRatio"`
		Settings    json.RawMessage `json:"settings"`
	}
	if !decode(w, r, &req) {
		return
	}

	settings := item.Settings
	if len(req.Settings) > 0 {
		settings = req.Settings
	}
	err = s.db.QueryRow(r.Context(), `
		UPDATE projects
		SET
			name = COALESCE($2, name),
			description = COALESCE($3, description),
			project_type = COALESCE($4, project_type),
			aspect_ratio = COALESCE($5, aspect_ratio),
			settings = $6
		WHERE id = $1
		RETURNING id, organization_id, workspace_id, name, description, project_type, aspect_ratio, settings, created_at, updated_at
	`, projectID, req.Name, req.Description, req.ProjectType, req.AspectRatio, settings).
		Scan(&item.ID, &item.OrganizationID, &item.WorkspaceID, &item.Name, &item.Description, &item.ProjectType, &item.AspectRatio, &item.Settings, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	projectID := r.PathValue("projectId")
	item, err := s.project(r, projectID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.ensureProjectMember(r, principal.UserID, item.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := s.db.Exec(r.Context(), `DELETE FROM projects WHERE id = $1`, projectID); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]bool{"deleted": true}, nil)
}

func (s *Server) withAuth(next func(http.ResponseWriter, *http.Request, auth.Principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := s.auth.ParseBearer(r.Header.Get("Authorization"))
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		next(w, r, principal)
	}
}

func (s *Server) organization(r *http.Request, orgID string) (Organization, error) {
	var item Organization
	err := s.db.QueryRow(r.Context(), `
		SELECT id, name, slug, created_at
		FROM organizations
		WHERE id = $1
	`, orgID).Scan(&item.ID, &item.Name, &item.Slug, &item.CreatedAt)
	return item, err
}

func (s *Server) project(r *http.Request, projectID string) (Project, error) {
	row := s.db.QueryRow(r.Context(), `
		SELECT id, organization_id, workspace_id, name, description, project_type, aspect_ratio, settings, created_at, updated_at
		FROM projects
		WHERE id = $1
	`, projectID)
	return scanProject(row)
}

func (s *Server) ensureOrganizationMember(r *http.Request, userID, orgID string) error {
	if orgID == "" {
		return auth.ErrUnauthorized
	}
	var ok bool
	err := s.db.QueryRow(r.Context(), `
		SELECT EXISTS(
			SELECT 1 FROM organization_members
			WHERE organization_id = $1 AND user_id = $2 AND status = 'active'
		)
	`, orgID, userID).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return auth.ErrForbidden
	}
	return nil
}

func (s *Server) ensureProjectMember(r *http.Request, userID, projectID string) error {
	var ok bool
	err := s.db.QueryRow(r.Context(), `
		SELECT EXISTS(
			SELECT 1
			FROM project_members
			WHERE project_id = $1 AND user_id = $2 AND status = 'active'
		)
	`, projectID, userID).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return auth.ErrForbidden
	}
	return nil
}

func scanProject(row pgx.Row) (Project, error) {
	var item Project
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.WorkspaceID,
		&item.Name,
		&item.Description,
		&item.ProjectType,
		&item.AspectRatio,
		&item.Settings,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "request body is invalid", err.Error(), false)
		return false
	}
	return true
}

func organizationID(r *http.Request, principal auth.Principal) string {
	if header := strings.TrimSpace(r.Header.Get("X-Organization-Id")); header != "" {
		return header
	}
	return principal.OrganizationID
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var upstreamErr *provider.UpstreamError
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		httpx.WriteError(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", "email or password is invalid", nil, false)
	case errors.Is(err, auth.ErrEmailExists):
		httpx.WriteError(w, r, http.StatusConflict, "EMAIL_EXISTS", "email already exists", nil, false)
	case errors.Is(err, auth.ErrUnauthorized):
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication is required", nil, false)
	case errors.Is(err, auth.ErrForbidden):
		httpx.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "permission denied", nil, false)
	case errors.Is(err, provider.ErrValidation):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "request is invalid", fmt.Sprintf("%v", err), false)
	case errors.Is(err, provider.ErrConflict):
		httpx.WriteError(w, r, http.StatusConflict, "CONFLICT", "resource conflict", fmt.Sprintf("%v", err), false)
	case errors.Is(err, provider.ErrProviderGatewayRequired):
		httpx.WriteError(w, r, http.StatusServiceUnavailable, provider.CodeProviderGatewayRequired, "provider gateway is required", fmt.Sprintf("%v", err), false)
	case errors.As(err, &upstreamErr):
		standard := provider.NormalizeHTTPError(upstreamErr.Status, upstreamErr.Code)
		httpx.WriteError(w, r, http.StatusBadGateway, standard.Code, standard.Message, standard, standard.Retryable)
	case errors.Is(err, pgx.ErrNoRows):
		httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "resource was not found", nil, false)
	default:
		httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error", fmt.Sprintf("%v", err), false)
	}
}
