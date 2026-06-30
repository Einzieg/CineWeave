package api

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/httpx"
)

type SetupStateResponse struct {
	NeedsSetup        bool `json:"needsSetup"`
	UserCount         int  `json:"userCount"`
	OrganizationCount int  `json:"organizationCount"`
}

func (s *Server) systemStatus(w http.ResponseWriter, r *http.Request) {
	statuses := map[string]string{
		"database":        "unknown",
		"temporal":        "unknown",
		"storage":         "unknown",
		"providerGateway": "unknown",
	}
	ctx := r.Context()
	if s.db != nil {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := s.db.Ping(pingCtx); err == nil {
			statuses["database"] = "ok"
		}
	}
	if s.temporal != nil {
		statuses["temporal"] = "ok"
	}
	if s.storage != nil {
		statuses["storage"] = "ok"
	}
	if strings.TrimSpace(os.Getenv("PROVIDER_GATEWAY_URL")) != "" {
		statuses["providerGateway"] = "ok"
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"status":   "ok",
		"services": statuses,
		"version":  "0.1.0",
	}, nil)
}

func (s *Server) systemSetupState(w http.ResponseWriter, r *http.Request) {
	state, err := s.setupState(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, state, nil)
}

func (s *Server) systemSetup(w http.ResponseWriter, r *http.Request) {
	var req auth.RegisterRequest
	if !decode(w, r, &req) {
		return
	}
	resp, err := s.auth.Setup(r.Context(), req, r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, resp, nil)
}

func (s *Server) setupState(r *http.Request) (SetupStateResponse, error) {
	var state SetupStateResponse
	if err := s.db.QueryRow(r.Context(), `
		SELECT
			(SELECT count(*) FROM users),
			(SELECT count(*) FROM organizations)
	`).Scan(&state.UserCount, &state.OrganizationCount); err != nil {
		return SetupStateResponse{}, err
	}
	state.NeedsSetup = state.UserCount == 0
	return state, nil
}
