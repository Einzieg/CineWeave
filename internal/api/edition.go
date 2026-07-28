package api

import (
	"context"
	"net/http"

	"github.com/Einzieg/cineweave/internal/auth"
	editionpkg "github.com/Einzieg/cineweave/internal/edition"
	"github.com/Einzieg/cineweave/internal/httpx"
)

func (s *Server) SetEditionRuntime(runtime *editionpkg.Runtime) error {
	if err := runtime.Validate(context.Background()); err != nil {
		return err
	}
	s.editionRuntime = runtime
	return nil
}

func (s *Server) systemEdition(w http.ResponseWriter, r *http.Request) {
	status, err := s.currentEditionRuntime().SystemEdition(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, status, nil)
}

func (s *Server) meEntitlements(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	snapshot, err := s.currentEditionRuntime().Entitlements.Evaluate(r.Context(), editionpkg.EntitlementRequest{
		Subject: editionpkg.EntitlementSubject{
			UserID:         principal.UserID,
			OrganizationID: principal.OrganizationID,
		},
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, snapshot, nil)
}

func (s *Server) currentEditionRuntime() *editionpkg.Runtime {
	if s.editionRuntime == nil {
		s.editionRuntime = editionpkg.MustCommunityRuntime()
	}
	return s.editionRuntime
}
