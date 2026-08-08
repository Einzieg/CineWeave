package api

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

type projectControlDiagnosticsResponse struct {
	MCP              controlmcp.Diagnostics         `json:"mcp"`
	ActionMatrixHash string                         `json:"actionMatrixHash"`
	ReleaseID        string                         `json:"releaseId"`
	Runtime          projectcontrol.RuntimeSnapshot `json:"runtime"`
}

func (s *Server) getSystemProjectControlDiagnostics(
	w http.ResponseWriter,
	r *http.Request,
	principal auth.Principal,
) {
	if err := s.auth.RequireSystemAdministrator(r.Context(), principal.UserID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if s.projectControl == nil || s.projectControl.repository == nil {
		s.writeError(w, r, newAPIError(
			http.StatusServiceUnavailable,
			"PROJECT_CONTROL_RUNTIME_UNAVAILABLE",
			"项目控制运行时暂不可用",
		))
		return
	}
	snapshot, err := s.projectControl.repository.RuntimeSnapshot(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	mcpDiagnostics := controlmcp.Diagnostics{
		Enabled: false, RecentAuthenticationFailures: []controlmcp.AuthenticationFailureAggregate{},
	}
	if s.projectControlMCP != nil {
		mcpDiagnostics = s.projectControlMCP.Diagnostics(time.Now())
	}
	actionMatrixHash := strings.TrimSpace(os.Getenv("CINEWEAVE_PROJECT_CONTROL_ACTION_MATRIX_HASH"))
	if actionMatrixHash == "" {
		actionMatrixHash = projectcontrol.GeneratedActionMatrixHash
	}
	releaseID := strings.TrimSpace(os.Getenv("CINEWEAVE_RELEASE_ID"))
	if releaseID == "" {
		releaseID = strings.TrimSpace(os.Getenv("CINEWEAVE_BUILD_ID"))
	}
	if releaseID == "" {
		releaseID = "development"
	}
	httpx.WriteJSON(w, r, http.StatusOK, projectControlDiagnosticsResponse{
		MCP: mcpDiagnostics, ActionMatrixHash: actionMatrixHash,
		ReleaseID: releaseID, Runtime: snapshot,
	}, nil)
}
