package projectcontrol

import (
	"context"
	"log/slog"
	"strings"

	"github.com/Einzieg/cineweave/internal/observability"
)

func withCommandLogContext(
	ctx context.Context,
	command Command,
	releaseID string,
	attemptNumber int,
	operation string,
) context.Context {
	logger := observability.LoggerFromContext(ctx)
	if logger == nil {
		return ctx
	}
	args := []any{
		"component", "project_control",
		"operation", operation,
		"commandId", command.ID,
		"actionName", command.ActionName,
		"actionVersion", command.ActionVersion,
		"actorUserId", command.ActorUserID,
		"organizationId", command.OrganizationID,
		"controllerType", command.ControllerType,
		"attemptNumber", attemptNumber,
		"releaseId", strings.TrimSpace(releaseID),
	}
	if command.WorkspaceID != "" {
		args = append(args, "workspaceId", command.WorkspaceID)
	}
	if command.ProjectID != "" {
		args = append(args, "projectId", command.ProjectID)
	}
	if command.AgentTaskID != "" {
		args = append(args, "agentTaskId", command.AgentTaskID)
	}
	if command.AgentStepID != "" {
		args = append(args, "agentStepId", command.AgentStepID)
	}
	return observability.WithLogger(ctx, logger.With(args...))
}

func logCommandInfo(ctx context.Context, message string, args ...any) {
	observability.Log(ctx, slog.LevelInfo, message, args...)
}

func logCommandWarning(ctx context.Context, message string, args ...any) {
	observability.Log(ctx, slog.LevelWarn, message, args...)
}

func logCommandError(ctx context.Context, message string, args ...any) {
	observability.Log(ctx, slog.LevelError, message, args...)
}

func workflowLogFields(links []WorkflowLink) []any {
	temporalWorkflowIDs := make([]string, 0, len(links))
	workflowRunIDs := make([]string, 0, len(links))
	for _, link := range links {
		if link.TemporalWorkflowID != "" {
			temporalWorkflowIDs = append(temporalWorkflowIDs, link.TemporalWorkflowID)
		}
		if link.WorkflowRunID != "" {
			workflowRunIDs = append(workflowRunIDs, link.WorkflowRunID)
		}
	}
	fields := []any{"workflowCount", len(links), "temporalWorkflowIds", temporalWorkflowIDs}
	if len(workflowRunIDs) > 0 {
		fields = append(fields, "workflowRunIds", workflowRunIDs)
	}
	return fields
}
