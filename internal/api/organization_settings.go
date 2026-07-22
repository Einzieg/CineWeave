package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auditlog"
	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
)

type AuditActor struct {
	ID          string `json:"id"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
}

type AuditLog struct {
	ID             string          `json:"id"`
	OrganizationID string          `json:"organizationId"`
	ActorUserID    *string         `json:"actorUserId,omitempty"`
	Actor          *AuditActor     `json:"actor,omitempty"`
	Action         string          `json:"action"`
	ResourceType   string          `json:"resourceType"`
	ResourceID     *string         `json:"resourceId,omitempty"`
	Metadata       json.RawMessage `json:"metadata"`
	CreatedAt      time.Time       `json:"createdAt"`
}

var organizationContextPermissionKeys = []string{
	authz.PermissionAdminManage,
	authz.PermissionOrganizationRead,
	authz.PermissionOrganizationUpdate,
	authz.PermissionMemberRead,
	authz.PermissionMemberManage,
	authz.PermissionTeamRead,
	authz.PermissionTeamManage,
	authz.PermissionRoleRead,
	authz.PermissionRoleManage,
	authz.PermissionAuditRead,
}

func (s *Server) currentOrganizationPermissions(ctx context.Context, principal auth.Principal) ([]string, error) {
	permissions := make([]string, 0, len(organizationContextPermissionKeys))
	for _, permission := range organizationContextPermissionKeys {
		err := s.authorizer.Authorize(ctx, principal, permission, authz.Resource{OrganizationID: principal.OrganizationID})
		if err == nil {
			permissions = append(permissions, permission)
			continue
		}
		if !errors.Is(err, authz.ErrAccessDenied) {
			return nil, err
		}
	}
	return permissions, nil
}

func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req auth.UpdateProfileRequest
	if !decode(w, r, &req) {
		return
	}
	user, err := s.auth.UpdateProfile(r.Context(), principal, req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, user, nil)
}

func (s *Server) updateOrganization(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	organizationID := r.PathValue("organizationId")
	if !s.authorize(w, r, principal, authz.PermissionOrganizationUpdate, authz.Resource{OrganizationID: organizationID}) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 100 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "organization name must contain 1 to 100 characters", nil, false)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var item Organization
	var previousName string
	if err := tx.QueryRow(r.Context(), `SELECT name FROM organizations WHERE id = $1 FOR UPDATE`, organizationID).Scan(&previousName); err != nil {
		s.writeError(w, r, err)
		return
	}
	err = tx.QueryRow(r.Context(), `
		UPDATE organizations
		SET name = $2
		WHERE id = $1
		RETURNING id, name, slug, created_at
	`, organizationID, name).Scan(&item.ID, &item.Name, &item.Slug, &item.CreatedAt)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := auditlog.Append(r.Context(), tx, organizationID, principal.UserID, auditlog.ActionOrganizationUpdated, "organization", organizationID, map[string]any{
		"previousName": previousName,
		"name":         name,
	}); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) leaveOrganization(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	organizationID := r.PathValue("organizationId")
	if organizationID == "" || organizationID != principal.OrganizationID {
		s.writeError(w, r, auth.ErrForbidden)
		return
	}
	if err := s.auth.LeaveOrganization(r.Context(), organizationID, principal.UserID); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]bool{"left": true}, nil)
}

func (s *Server) listOrganizationAuditLogs(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	organizationID := r.PathValue("organizationId")
	if !s.authorize(w, r, principal, authz.PermissionAuditRead, authz.Resource{OrganizationID: organizationID}) {
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 25
	}
	if pageSize > 100 {
		pageSize = 100
	}
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	resourceType := strings.TrimSpace(r.URL.Query().Get("resourceType"))
	actorUserID := strings.TrimSpace(r.URL.Query().Get("actorUserId"))
	filter := `al.organization_id = $1
		AND ($2 = '' OR al.action = $2)
		AND ($3 = '' OR al.resource_type = $3)
		AND ($4 = '' OR al.actor_user_id::text = $4)`
	var total int
	if err := s.db.QueryRow(r.Context(), `SELECT count(*) FROM audit_logs al WHERE `+filter,
		organizationID, action, resourceType, actorUserID).Scan(&total); err != nil {
		s.writeError(w, r, err)
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT al.id, al.organization_id, al.actor_user_id::text,
		       COALESCE(u.username, ''), COALESCE(u.display_name, ''), COALESCE(u.avatar_url, ''),
		       al.action, al.resource_type, al.resource_id::text, al.metadata, al.created_at
		FROM audit_logs al
		LEFT JOIN users u ON u.id = al.actor_user_id
		WHERE `+filter+`
		ORDER BY al.created_at DESC, al.id DESC
		LIMIT $5 OFFSET $6
	`, organizationID, action, resourceType, actorUserID, pageSize, (page-1)*pageSize)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]AuditLog, 0)
	for rows.Next() {
		var item AuditLog
		var actorUserID *string
		var actorUsername, actorDisplayName, actorAvatarURL string
		if err := rows.Scan(
			&item.ID, &item.OrganizationID, &actorUserID,
			&actorUsername, &actorDisplayName, &actorAvatarURL,
			&item.Action, &item.ResourceType, &item.ResourceID, &item.Metadata, &item.CreatedAt,
		); err != nil {
			s.writeError(w, r, err)
			return
		}
		item.ActorUserID = actorUserID
		if actorUserID != nil {
			item.Actor = &AuditActor{ID: *actorUserID, Username: actorUsername, DisplayName: actorDisplayName, AvatarURL: actorAvatarURL}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"items":           items,
		"page":            page,
		"pageSize":        pageSize,
		"total":           total,
		"retentionPolicy": "organization_lifetime",
	}, nil)
}
