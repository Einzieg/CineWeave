package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/workflows"
)

type ProjectAgentActivities struct {
	server *Server
}

func NewProjectAgentActivities(server *Server) *ProjectAgentActivities {
	return &ProjectAgentActivities{server: server}
}

func (a *ProjectAgentActivities) PlanTask(ctx context.Context, input workflows.ProjectAgentWorkflowInput) (workflows.ProjectAgentWorkflowState, error) {
	return a.server.projectAgentPlanTaskActivity(ctx, input)
}

func (a *ProjectAgentActivities) ExecuteReadySteps(ctx context.Context, input workflows.ProjectAgentWorkflowInput) (workflows.ProjectAgentWorkflowState, error) {
	return a.server.projectAgentExecuteReadyStepsActivity(ctx, input)
}

func (a *ProjectAgentActivities) ApproveStep(ctx context.Context, input workflows.ProjectAgentWorkflowInput, signal workflows.ProjectAgentStepDecisionSignal) (workflows.ProjectAgentWorkflowState, error) {
	return a.server.projectAgentStepDecisionActivity(ctx, input, signal, "approved")
}

func (a *ProjectAgentActivities) RejectStep(ctx context.Context, input workflows.ProjectAgentWorkflowInput, signal workflows.ProjectAgentStepDecisionSignal) (workflows.ProjectAgentWorkflowState, error) {
	return a.server.projectAgentStepDecisionActivity(ctx, input, signal, "rejected")
}

func (a *ProjectAgentActivities) CancelTask(ctx context.Context, input workflows.ProjectAgentWorkflowInput, signal workflows.ProjectAgentCancelSignal) (workflows.ProjectAgentWorkflowState, error) {
	return a.server.projectAgentCancelTaskActivity(ctx, input, signal)
}

func (a *ProjectAgentActivities) ModifyConstraints(ctx context.Context, input workflows.ProjectAgentWorkflowInput, signal workflows.ProjectAgentModifyConstraintsSignal) (workflows.ProjectAgentWorkflowState, error) {
	return a.server.projectAgentModifyConstraintsActivity(ctx, input, signal)
}

func (s *Server) projectAgentPlanTaskActivity(ctx context.Context, input workflows.ProjectAgentWorkflowInput) (workflows.ProjectAgentWorkflowState, error) {
	r, principal, project, task, err := s.projectAgentActivityContext(ctx, input)
	if err != nil {
		return workflows.ProjectAgentWorkflowState{}, err
	}
	if isTerminalAgentTaskStatus(task.Status) {
		return projectAgentTaskState(task), nil
	}
	steps, err := s.listAgentTaskSteps(r, task.ID)
	if err != nil {
		return workflows.ProjectAgentWorkflowState{}, err
	}
	if len(steps) > 0 || (task.Status != "queued" && task.Status != "planning") {
		return projectAgentTaskState(task), nil
	}
	planned, err := s.planAgentTask(r, principal, project, task)
	if err != nil {
		return workflows.ProjectAgentWorkflowState{}, err
	}
	return projectAgentTaskState(planned), nil
}

func (s *Server) projectAgentExecuteReadyStepsActivity(ctx context.Context, input workflows.ProjectAgentWorkflowInput) (workflows.ProjectAgentWorkflowState, error) {
	r, principal, project, task, err := s.projectAgentActivityContext(ctx, input)
	if err != nil {
		return workflows.ProjectAgentWorkflowState{}, err
	}
	if isTerminalAgentTaskStatus(task.Status) || task.Mode == "plan_only" {
		return projectAgentTaskState(task), nil
	}
	updated, err := s.executeAgentTaskReadySteps(r, principal, project, task.ID)
	if err != nil {
		return workflows.ProjectAgentWorkflowState{}, err
	}
	return projectAgentTaskState(updated), nil
}

func (s *Server) projectAgentStepDecisionActivity(ctx context.Context, input workflows.ProjectAgentWorkflowInput, signal workflows.ProjectAgentStepDecisionSignal, decision string) (workflows.ProjectAgentWorkflowState, error) {
	r, _, project, task, err := s.projectAgentActivityContext(ctx, input)
	if err != nil {
		return workflows.ProjectAgentWorkflowState{}, err
	}
	userID := strings.TrimSpace(signal.UserID)
	if userID == "" {
		userID = input.UserID
	}
	principal := auth.Principal{UserID: userID, OrganizationID: project.OrganizationID}
	if _, err := s.decideAgentStepApprovalCore(ctx, principal, project, task.ID, strings.TrimSpace(signal.StepID), decision, agentStepApprovalRequest{
		ApprovalID: strings.TrimSpace(signal.ApprovalID),
		Note:       strings.TrimSpace(signal.Note),
	}); err != nil {
		return workflows.ProjectAgentWorkflowState{}, err
	}
	updated, err := s.agentTask(r, project.ID, task.ID)
	if err != nil {
		return workflows.ProjectAgentWorkflowState{}, err
	}
	return projectAgentTaskState(updated), nil
}

func (s *Server) projectAgentCancelTaskActivity(ctx context.Context, input workflows.ProjectAgentWorkflowInput, signal workflows.ProjectAgentCancelSignal) (workflows.ProjectAgentWorkflowState, error) {
	_, _, project, task, err := s.projectAgentActivityContext(ctx, input)
	if err != nil {
		return workflows.ProjectAgentWorkflowState{}, err
	}
	reason := strings.TrimSpace(signal.Reason)
	if reason == "" {
		reason = "Project Agent task cancelled"
	}
	cancelled, err := s.cancelAgentTaskCore(ctx, project, task.ID, reason)
	if err != nil {
		return workflows.ProjectAgentWorkflowState{}, err
	}
	return projectAgentTaskState(cancelled), nil
}

func (s *Server) projectAgentModifyConstraintsActivity(ctx context.Context, input workflows.ProjectAgentWorkflowInput, signal workflows.ProjectAgentModifyConstraintsSignal) (workflows.ProjectAgentWorkflowState, error) {
	r, _, project, task, err := s.projectAgentActivityContext(ctx, input)
	if err != nil {
		return workflows.ProjectAgentWorkflowState{}, err
	}
	patch := signal.Constraints
	if patch == nil {
		patch = map[string]any{}
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE agent_tasks
		SET constraints = COALESCE(constraints, '{}'::jsonb) || $2::jsonb,
		    status = CASE WHEN status IN ('blocked', 'failed', 'waiting_approval') THEN 'queued' ELSE status END,
		    error_code = CASE WHEN status IN ('blocked', 'failed') THEN NULL ELSE error_code END,
		    error_message = CASE WHEN status IN ('blocked', 'failed') THEN NULL ELSE error_message END,
		    summary = jsonb_set(COALESCE(summary, '{}'::jsonb), '{lastConstraintChange}', $3::jsonb, true)
		WHERE id = $1 AND project_id = $4
	`, task.ID, mustMarshal(patch), mustMarshal(map[string]any{
		"userId": signal.UserID,
		"note":   strings.TrimSpace(signal.Note),
		"patch":  patch,
	}), project.ID); err != nil {
		return workflows.ProjectAgentWorkflowState{}, err
	}
	updated, err := s.agentTask(r, project.ID, task.ID)
	if err != nil {
		return workflows.ProjectAgentWorkflowState{}, err
	}
	return projectAgentTaskState(updated), nil
}

func (s *Server) projectAgentActivityContext(ctx context.Context, input workflows.ProjectAgentWorkflowInput) (*http.Request, auth.Principal, Project, AgentTask, error) {
	r := requestWithContext(ctx)
	project, err := s.project(r, input.ProjectID)
	if err != nil {
		return nil, auth.Principal{}, Project{}, AgentTask{}, err
	}
	if strings.TrimSpace(input.OrganizationID) != "" && project.OrganizationID != strings.TrimSpace(input.OrganizationID) {
		return nil, auth.Principal{}, Project{}, AgentTask{}, auth.ErrForbidden
	}
	task, err := s.agentTask(r, project.ID, input.TaskID)
	if err != nil {
		return nil, auth.Principal{}, Project{}, AgentTask{}, err
	}
	principal := auth.Principal{UserID: input.UserID, OrganizationID: project.OrganizationID}
	return r, principal, project, task, nil
}

func projectAgentTaskState(task AgentTask) workflows.ProjectAgentWorkflowState {
	message := ""
	if task.ErrorMessage != nil {
		message = *task.ErrorMessage
	} else if len(task.Summary) > 0 {
		var summary struct {
			Summary string `json:"summary"`
		}
		if err := json.Unmarshal(task.Summary, &summary); err == nil {
			message = summary.Summary
		}
	}
	return workflows.ProjectAgentWorkflowState{TaskID: task.ID, Status: task.Status, Message: message}
}
