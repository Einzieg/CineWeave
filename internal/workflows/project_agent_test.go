package workflows

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestProjectAgentWorkflowApproveSignalContinuesExecution(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := ProjectAgentWorkflowInput{
		OrganizationID: "org-1",
		ProjectID:      "project-1",
		TaskID:         "task-1",
		UserID:         "user-1",
	}

	env.RegisterWorkflow(ProjectAgentWorkflow)
	registerProjectAgentTestActivities(env)
	env.OnActivity(projectAgentPlanActivity, mock.Anything, input).Return(ProjectAgentWorkflowState{TaskID: input.TaskID, Status: "waiting_approval"}, nil).Once()
	env.OnActivity(projectAgentExecuteReadyStepsActivity, mock.Anything, input).Return(ProjectAgentWorkflowState{TaskID: input.TaskID, Status: "waiting_approval"}, nil).Once()
	env.OnActivity(projectAgentApproveStepActivity, mock.Anything, input, ProjectAgentStepDecisionSignal{
		TaskID: input.TaskID,
		StepID: "step-1",
		UserID: input.UserID,
	}).Return(ProjectAgentWorkflowState{TaskID: input.TaskID, Status: "queued"}, nil).Once()
	env.OnActivity(projectAgentExecuteReadyStepsActivity, mock.Anything, input).Return(ProjectAgentWorkflowState{TaskID: input.TaskID, Status: "succeeded"}, nil).Once()
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ProjectAgentApproveStepSignalName, ProjectAgentStepDecisionSignal{
			TaskID: input.TaskID,
			StepID: "step-1",
			UserID: input.UserID,
		})
	}, time.Millisecond)

	env.ExecuteWorkflow(ProjectAgentWorkflow, input)

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v err=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	var output ProjectAgentWorkflowState
	if err := env.GetWorkflowResult(&output); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if output.Status != "succeeded" {
		t.Fatalf("workflow status = %s, want succeeded", output.Status)
	}
	env.AssertExpectations(t)
}

func TestProjectAgentWorkflowCancelSignalRunsCancelActivity(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := ProjectAgentWorkflowInput{
		OrganizationID: "org-1",
		ProjectID:      "project-1",
		TaskID:         "task-1",
		UserID:         "user-1",
	}
	signal := ProjectAgentCancelSignal{TaskID: input.TaskID, UserID: input.UserID, Reason: "stop"}

	env.RegisterWorkflow(ProjectAgentWorkflow)
	registerProjectAgentTestActivities(env)
	env.OnActivity(projectAgentPlanActivity, mock.Anything, input).Return(ProjectAgentWorkflowState{TaskID: input.TaskID, Status: "waiting_approval"}, nil).Once()
	env.OnActivity(projectAgentExecuteReadyStepsActivity, mock.Anything, input).Return(ProjectAgentWorkflowState{TaskID: input.TaskID, Status: "waiting_approval"}, nil).Once()
	env.OnActivity(projectAgentCancelActivity, mock.Anything, input, signal).Return(ProjectAgentWorkflowState{TaskID: input.TaskID, Status: "cancelled"}, nil).Once()
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ProjectAgentCancelTaskSignalName, signal)
	}, time.Millisecond)

	env.ExecuteWorkflow(ProjectAgentWorkflow, input)

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v err=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	var output ProjectAgentWorkflowState
	if err := env.GetWorkflowResult(&output); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if output.Status != "cancelled" {
		t.Fatalf("workflow status = %s, want cancelled", output.Status)
	}
	env.AssertExpectations(t)
}

func TestProjectAgentWorkflowRetriesTransientPlanActivityFailure(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := ProjectAgentWorkflowInput{
		OrganizationID: "org-1",
		ProjectID:      "project-1",
		TaskID:         "task-1",
		UserID:         "user-1",
	}

	env.RegisterWorkflow(ProjectAgentWorkflow)
	registerProjectAgentTestActivities(env)
	env.OnActivity(projectAgentPlanActivity, mock.Anything, input).Return(ProjectAgentWorkflowState{}, errors.New("transient planner failure")).Once()
	env.OnActivity(projectAgentPlanActivity, mock.Anything, input).Return(ProjectAgentWorkflowState{TaskID: input.TaskID, Status: "succeeded"}, nil).Once()

	env.ExecuteWorkflow(ProjectAgentWorkflow, input)

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v err=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	var output ProjectAgentWorkflowState
	if err := env.GetWorkflowResult(&output); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if output.Status != "succeeded" {
		t.Fatalf("workflow status = %s, want succeeded", output.Status)
	}
	env.AssertExpectations(t)
}

func TestProjectAgentWorkflowReturnsFailedTerminalState(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := ProjectAgentWorkflowInput{
		OrganizationID: "org-1",
		ProjectID:      "project-1",
		TaskID:         "task-1",
		UserID:         "user-1",
	}

	env.RegisterWorkflow(ProjectAgentWorkflow)
	registerProjectAgentTestActivities(env)
	env.OnActivity(projectAgentPlanActivity, mock.Anything, input).Return(ProjectAgentWorkflowState{TaskID: input.TaskID, Status: "queued"}, nil).Once()
	env.OnActivity(projectAgentExecuteReadyStepsActivity, mock.Anything, input).Return(ProjectAgentWorkflowState{TaskID: input.TaskID, Status: "failed", Message: "tool failed"}, nil).Once()

	env.ExecuteWorkflow(ProjectAgentWorkflow, input)

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v err=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	var output ProjectAgentWorkflowState
	if err := env.GetWorkflowResult(&output); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if output.Status != "failed" || output.Message != "tool failed" {
		t.Fatalf("workflow state = %+v, want failed terminal state", output)
	}
	env.AssertExpectations(t)
}

func TestProjectAgentWorkflowPollsRunningState(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := ProjectAgentWorkflowInput{
		OrganizationID: "org-1",
		ProjectID:      "project-1",
		TaskID:         "task-1",
		UserID:         "user-1",
	}

	env.RegisterWorkflow(ProjectAgentWorkflow)
	registerProjectAgentTestActivities(env)
	env.OnActivity(projectAgentPlanActivity, mock.Anything, input).Return(ProjectAgentWorkflowState{TaskID: input.TaskID, Status: "queued"}, nil).Once()
	env.OnActivity(projectAgentExecuteReadyStepsActivity, mock.Anything, input).Return(ProjectAgentWorkflowState{TaskID: input.TaskID, Status: "running", Message: "waiting child workflow"}, nil).Once()
	env.OnActivity(projectAgentExecuteReadyStepsActivity, mock.Anything, input).Return(ProjectAgentWorkflowState{TaskID: input.TaskID, Status: "succeeded"}, nil).Once()

	env.ExecuteWorkflow(ProjectAgentWorkflow, input)

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v err=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	var output ProjectAgentWorkflowState
	if err := env.GetWorkflowResult(&output); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if output.Status != "succeeded" {
		t.Fatalf("workflow status = %s, want succeeded", output.Status)
	}
	env.AssertExpectations(t)
}

func TestProjectAgentWorkflowResumeSignalContinuesExecution(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := ProjectAgentWorkflowInput{
		OrganizationID: "org-1",
		ProjectID:      "project-1",
		TaskID:         "task-1",
		UserID:         "user-1",
	}

	env.RegisterWorkflow(ProjectAgentWorkflow)
	registerProjectAgentTestActivities(env)
	env.OnActivity(projectAgentPlanActivity, mock.Anything, input).Return(ProjectAgentWorkflowState{TaskID: input.TaskID, Status: "waiting_approval"}, nil).Once()
	env.OnActivity(projectAgentExecuteReadyStepsActivity, mock.Anything, input).Return(ProjectAgentWorkflowState{TaskID: input.TaskID, Status: "waiting_approval"}, nil).Once()
	env.OnActivity(projectAgentExecuteReadyStepsActivity, mock.Anything, input).Return(ProjectAgentWorkflowState{TaskID: input.TaskID, Status: "succeeded"}, nil).Once()
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ProjectAgentResumeTaskSignalName, ProjectAgentStepDecisionSignal{
			TaskID: input.TaskID,
			UserID: input.UserID,
		})
	}, time.Millisecond)

	env.ExecuteWorkflow(ProjectAgentWorkflow, input)

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v err=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	var output ProjectAgentWorkflowState
	if err := env.GetWorkflowResult(&output); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if output.Status != "succeeded" {
		t.Fatalf("workflow status = %s, want succeeded", output.Status)
	}
	env.AssertExpectations(t)
}

func TestProjectAgentWorkflowModifyConstraintsSignalContinuesExecution(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := ProjectAgentWorkflowInput{
		OrganizationID: "org-1",
		ProjectID:      "project-1",
		TaskID:         "task-1",
		UserID:         "user-1",
	}
	signal := ProjectAgentModifyConstraintsSignal{
		TaskID:      input.TaskID,
		UserID:      input.UserID,
		Constraints: map[string]any{"allowVideoGeneration": false},
		Note:        "image only",
	}

	env.RegisterWorkflow(ProjectAgentWorkflow)
	registerProjectAgentTestActivities(env)
	env.OnActivity(projectAgentPlanActivity, mock.Anything, input).Return(ProjectAgentWorkflowState{TaskID: input.TaskID, Status: "waiting_approval"}, nil).Once()
	env.OnActivity(projectAgentExecuteReadyStepsActivity, mock.Anything, input).Return(ProjectAgentWorkflowState{TaskID: input.TaskID, Status: "waiting_approval"}, nil).Once()
	env.OnActivity(projectAgentModifyConstraintsActivity, mock.Anything, input, signal).Return(ProjectAgentWorkflowState{TaskID: input.TaskID, Status: "queued"}, nil).Once()
	env.OnActivity(projectAgentExecuteReadyStepsActivity, mock.Anything, input).Return(ProjectAgentWorkflowState{TaskID: input.TaskID, Status: "succeeded"}, nil).Once()
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ProjectAgentModifyConstraintsSignalName, signal)
	}, time.Millisecond)

	env.ExecuteWorkflow(ProjectAgentWorkflow, input)

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v err=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	var output ProjectAgentWorkflowState
	if err := env.GetWorkflowResult(&output); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if output.Status != "succeeded" {
		t.Fatalf("workflow status = %s, want succeeded", output.Status)
	}
	env.AssertExpectations(t)
}

func registerProjectAgentTestActivities(env *testsuite.TestWorkflowEnvironment) {
	env.RegisterActivityWithOptions(func(context.Context, ProjectAgentWorkflowInput) (ProjectAgentWorkflowState, error) {
		return ProjectAgentWorkflowState{}, nil
	}, activity.RegisterOptions{Name: projectAgentPlanActivity})
	env.RegisterActivityWithOptions(func(context.Context, ProjectAgentWorkflowInput) (ProjectAgentWorkflowState, error) {
		return ProjectAgentWorkflowState{}, nil
	}, activity.RegisterOptions{Name: projectAgentExecuteReadyStepsActivity})
	env.RegisterActivityWithOptions(func(context.Context, ProjectAgentWorkflowInput, ProjectAgentStepDecisionSignal) (ProjectAgentWorkflowState, error) {
		return ProjectAgentWorkflowState{}, nil
	}, activity.RegisterOptions{Name: projectAgentApproveStepActivity})
	env.RegisterActivityWithOptions(func(context.Context, ProjectAgentWorkflowInput, ProjectAgentStepDecisionSignal) (ProjectAgentWorkflowState, error) {
		return ProjectAgentWorkflowState{}, nil
	}, activity.RegisterOptions{Name: projectAgentRejectStepActivity})
	env.RegisterActivityWithOptions(func(context.Context, ProjectAgentWorkflowInput, ProjectAgentCancelSignal) (ProjectAgentWorkflowState, error) {
		return ProjectAgentWorkflowState{}, nil
	}, activity.RegisterOptions{Name: projectAgentCancelActivity})
	env.RegisterActivityWithOptions(func(context.Context, ProjectAgentWorkflowInput, ProjectAgentModifyConstraintsSignal) (ProjectAgentWorkflowState, error) {
		return ProjectAgentWorkflowState{}, nil
	}, activity.RegisterOptions{Name: projectAgentModifyConstraintsActivity})
}
