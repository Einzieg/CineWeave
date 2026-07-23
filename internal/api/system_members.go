package api

import (
	"net/http"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/httpx"
)

func (s *Server) listSystemOrganizationMembers(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	items, err := s.auth.ListSystemOrganizationMembers(
		r.Context(),
		principal.UserID,
		r.PathValue("organizationId"),
		r.URL.Query().Get("search"),
		r.URL.Query().Get("status"),
		queryInt(r, "page", 1),
		queryInt(r, "pageSize", 25),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, items, nil)
}

func (s *Server) createSystemOrganizationMember(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req auth.CreateSystemOrganizationMemberRequest
	if !decode(w, r, &req) {
		return
	}
	member, err := s.auth.CreateSystemOrganizationMember(
		r.Context(),
		principal.UserID,
		r.PathValue("organizationId"),
		req,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, member, nil)
}

func (s *Server) updateSystemOrganizationMember(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req auth.UpdateSystemOrganizationMemberRequest
	if !decode(w, r, &req) {
		return
	}
	member, err := s.auth.UpdateSystemOrganizationMember(
		r.Context(),
		principal.UserID,
		r.PathValue("organizationId"),
		r.PathValue("userId"),
		req,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, member, nil)
}
