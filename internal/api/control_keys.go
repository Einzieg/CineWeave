package api

import (
	"errors"
	"net/http"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/httpx"
)

type controlKeyStatusResponse struct {
	Key           *auth.ControlKeyMetadata `json:"key,omitempty"`
	RequiresSetup bool                     `json:"requiresSetup"`
}

type controlKeySecretResponse struct {
	CodexControlKey auth.ControlKeySecret `json:"codexControlKey"`
}

func (s *Server) getCodexControlKey(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	item, err := s.auth.GetControlKey(r.Context(), principal.UserID)
	if errors.Is(err, auth.ErrControlKeyNotFound) {
		httpx.WriteJSON(w, r, http.StatusOK, controlKeyStatusResponse{RequiresSetup: true}, nil)
		return
	}
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, controlKeyStatusResponse{Key: &item}, nil)
}

func (s *Server) createCodexControlKey(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	item, err := s.auth.CreateControlKey(r.Context(), principal.UserID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, controlKeySecretResponse{CodexControlKey: item}, nil)
}

func (s *Server) rotateCodexControlKey(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	item, err := s.auth.RotateControlKey(r.Context(), principal.UserID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, controlKeySecretResponse{CodexControlKey: item}, nil)
}

func (s *Server) revokeCodexControlKey(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	if err := s.auth.RevokeControlKey(r.Context(), principal.UserID); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
