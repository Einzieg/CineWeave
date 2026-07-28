package api

import (
	"context"

	"github.com/Einzieg/cineweave/internal/agent"
	"github.com/Einzieg/cineweave/internal/auth"
)

func (s *Server) authorizeAgentToolPermissions(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	tool agent.AgentTool,
) error {
	for _, permission := range tool.RequiredPermissions() {
		resource := agentToolPermissionResource(project, permission)
		if err := s.authorizer.Authorize(ctx, principal, permission, resource); err != nil {
			return err
		}
	}
	return nil
}
