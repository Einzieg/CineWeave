package api

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/httpx"
)

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
