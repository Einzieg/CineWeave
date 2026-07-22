package api

import (
	"net/http"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/httpx"
)

func (s *Server) listSystemOrganizations(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	organizations, err := s.auth.ListSystemOrganizations(
		r.Context(),
		principal.UserID,
		r.URL.Query().Get("search"),
		queryInt(r, "page", 1),
		queryInt(r, "pageSize", 25),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, organizations, nil)
}

func (s *Server) createSystemOrganization(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	if err := s.auth.RequireSystemAdministrator(r.Context(), principal.UserID); err != nil {
		s.writeError(w, r, err)
		return
	}
	var req auth.CreateSystemOrganizationRequest
	if !decode(w, r, &req) {
		return
	}
	organization, err := s.auth.CreateSystemOrganization(r.Context(), principal.UserID, req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, organization, nil)
}
