package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
)

func (s *Server) listOrganizationMembers(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	organizationID := r.PathValue("organizationId")
	if !s.authorize(w, r, principal, authz.PermissionMemberRead, authz.Resource{OrganizationID: organizationID}) {
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	items, err := s.auth.ListOrganizationMembers(
		r.Context(), organizationID, r.URL.Query().Get("search"), r.URL.Query().Get("status"), page, pageSize,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, items, nil)
}

func (s *Server) getOrganizationMember(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	organizationID := r.PathValue("organizationId")
	if !s.authorize(w, r, principal, authz.PermissionMemberRead, authz.Resource{OrganizationID: organizationID}) {
		return
	}
	item, err := s.auth.GetOrganizationMember(r.Context(), organizationID, r.PathValue("userId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) updateOrganizationMember(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	organizationID := r.PathValue("organizationId")
	if !s.authorize(w, r, principal, authz.PermissionMemberManage, authz.Resource{OrganizationID: organizationID}) {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if !decode(w, r, &req) {
		return
	}
	item, err := s.auth.SetOrganizationMemberStatus(
		r.Context(), organizationID, r.PathValue("userId"), principal.UserID, strings.TrimSpace(req.Status),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) removeOrganizationMember(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	organizationID := r.PathValue("organizationId")
	if !s.authorize(w, r, principal, authz.PermissionMemberManage, authz.Resource{OrganizationID: organizationID}) {
		return
	}
	if err := s.auth.RemoveOrganizationMember(r.Context(), organizationID, r.PathValue("userId"), principal.UserID); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]bool{"removed": true}, nil)
}

func (s *Server) updateOrganizationMemberProfile(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	organizationID := r.PathValue("organizationId")
	if !s.authorize(w, r, principal, authz.PermissionMemberManage, authz.Resource{OrganizationID: organizationID}) {
		return
	}
	var req auth.UpdateProfileRequest
	if !decode(w, r, &req) {
		return
	}
	item, err := s.auth.UpdateOrganizationMemberProfile(
		r.Context(), organizationID, r.PathValue("userId"), principal.UserID, req,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) issueOrganizationMemberPasswordReset(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	w.Header().Set("Cache-Control", "no-store")
	organizationID := r.PathValue("organizationId")
	if !s.authorize(w, r, principal, authz.PermissionMemberManage, authz.Resource{OrganizationID: organizationID}) {
		return
	}
	reset, err := s.auth.IssueOrganizationMemberPasswordReset(
		r.Context(), organizationID, r.PathValue("userId"), principal.UserID,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, reset, nil)
}

func (s *Server) completePasswordReset(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var req auth.CompletePasswordResetRequest
	if !decode(w, r, &req) {
		return
	}
	if err := s.auth.CompletePasswordReset(r.Context(), req, r); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]bool{"completed": true}, nil)
}

func (s *Server) listOrganizationInvitations(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	organizationID := r.PathValue("organizationId")
	if !s.authorize(w, r, principal, authz.PermissionMemberRead, authz.Resource{OrganizationID: organizationID}) {
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	items, err := s.auth.ListInvitations(r.Context(), organizationID, page, pageSize)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, items, nil)
}

func (s *Server) createOrganizationInvitation(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	w.Header().Set("Cache-Control", "no-store")
	organizationID := r.PathValue("organizationId")
	if !s.authorize(w, r, principal, authz.PermissionMemberManage, authz.Resource{OrganizationID: organizationID}) {
		return
	}
	var req auth.CreateInvitationRequest
	if !decode(w, r, &req) {
		return
	}
	for _, binding := range req.Bindings {
		resource := authz.Resource{OrganizationID: organizationID, WorkspaceID: binding.WorkspaceID, ProjectID: binding.ProjectID}
		if !s.authorize(w, r, principal, authz.PermissionRoleManage, resource) {
			return
		}
		role, err := s.roleForBinding(r, organizationID, binding.RoleID)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		if err := s.validateRoleDelegation(r, principal, organizationID, role, resource); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	item, err := s.auth.CreateInvitation(r.Context(), organizationID, principal.UserID, req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) revokeOrganizationInvitation(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	organizationID := r.PathValue("organizationId")
	if !s.authorize(w, r, principal, authz.PermissionMemberManage, authz.Resource{OrganizationID: organizationID}) {
		return
	}
	if err := s.auth.RevokeInvitation(r.Context(), organizationID, r.PathValue("invitationId"), principal.UserID); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]bool{"revoked": true}, nil)
}

func (s *Server) resolveOrganizationInvitation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var req auth.ResolveInvitationRequest
	if !decode(w, r, &req) {
		return
	}
	item, err := s.auth.ResolveInvitation(r.Context(), req.InvitationToken, r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) acceptOrganizationInvitation(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	w.Header().Set("Cache-Control", "no-store")
	var req auth.AcceptInvitationRequest
	if !decode(w, r, &req) {
		return
	}
	response, err := s.auth.AcceptInvitation(r.Context(), principal, req.InvitationToken, r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, response, nil)
}

func (s *Server) registerWithOrganizationInvitation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var req auth.RegisterWithInvitationRequest
	if !decode(w, r, &req) {
		return
	}
	response, err := s.auth.RegisterWithInvitation(r.Context(), req, r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, response, nil)
}
