package workflows

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

const (
	ProjectAgentApproveStepSignalName       = "agent.approve_step"
	ProjectAgentRejectStepSignalName        = "agent.reject_step"
	ProjectAgentCancelTaskSignalName        = "agent.cancel_task"
	ProjectAgentModifyConstraintsSignalName = "agent.modify_constraints"
	ProjectAgentResumeTaskSignalName        = "agent.resume_task"

	projectAgentPlanActivity              = "ProjectAgentPlanTask"
	projectAgentExecuteReadyStepsActivity = "ProjectAgentExecuteReadySteps"
	projectAgentApproveStepActivity       = "ProjectAgentApproveStep"
	projectAgentRejectStepActivity        = "ProjectAgentRejectStep"
	projectAgentCancelActivity            = "ProjectAgentCancelTask"
	projectAgentModifyConstraintsActivity = "ProjectAgentModifyConstraints"

	projectAgentPollInterval = 10 * time.Second
)

type ProjectAgentWorkflowInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	TaskID         string `json:"taskId"`
	UserID         string `json:"userId"`
}

type ProjectAgentWorkflowState struct {
	TaskID  string `json:"taskId"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type ProjectAgentStepDecisionSignal struct {
	TaskID     string `json:"taskId"`
	StepID     string `json:"stepId"`
	ApprovalID string `json:"approvalId,omitempty"`
	UserID     string `json:"userId"`
	Note       string `json:"note,omitempty"`
}

type ProjectAgentCancelSignal struct {
	TaskID string `json:"taskId"`
	UserID string `json:"userId"`
	Reason string `json:"reason,omitempty"`
}

type ProjectAgentModifyConstraintsSignal struct {
	TaskID      string         `json:"taskId"`
	UserID      string         `json:"userId"`
	Constraints map[string]any `json:"constraints,omitempty"`
	Note        string         `json:"note,omitempty"`
}

func ProjectAgentWorkflow(ctx workflow.Context, input ProjectAgentWorkflowInput) (ProjectAgentWorkflowState, error) {
	activityOptions := defaultActivityOptions()
	activityOptions.StartToCloseTimeout = 10 * time.Minute
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	var state ProjectAgentWorkflowState
	if err := workflow.ExecuteActivity(ctx, projectAgentPlanActivity, input).Get(ctx, &state); err != nil {
		return ProjectAgentWorkflowState{}, err
	}
	if projectAgentWorkflowTerminal(state.Status) {
		return state, nil
	}

	for {
		if err := workflow.ExecuteActivity(ctx, projectAgentExecuteReadyStepsActivity, input).Get(ctx, &state); err != nil {
			return ProjectAgentWorkflowState{}, err
		}
		if projectAgentWorkflowTerminal(state.Status) {
			return state, nil
		}

		selector := workflow.NewSelector(ctx)
		approveCh := workflow.GetSignalChannel(ctx, ProjectAgentApproveStepSignalName)
		rejectCh := workflow.GetSignalChannel(ctx, ProjectAgentRejectStepSignalName)
		cancelCh := workflow.GetSignalChannel(ctx, ProjectAgentCancelTaskSignalName)
		modifyCh := workflow.GetSignalChannel(ctx, ProjectAgentModifyConstraintsSignalName)
		resumeCh := workflow.GetSignalChannel(ctx, ProjectAgentResumeTaskSignalName)

		var cancelled bool
		var timerFired bool
		var cancelSignal ProjectAgentCancelSignal
		var signalErr error
		selector.AddReceive(approveCh, func(c workflow.ReceiveChannel, more bool) {
			var signal ProjectAgentStepDecisionSignal
			c.Receive(ctx, &signal)
			signalErr = workflow.ExecuteActivity(ctx, projectAgentApproveStepActivity, input, signal).Get(ctx, &state)
		})
		selector.AddReceive(rejectCh, func(c workflow.ReceiveChannel, more bool) {
			var signal ProjectAgentStepDecisionSignal
			c.Receive(ctx, &signal)
			signalErr = workflow.ExecuteActivity(ctx, projectAgentRejectStepActivity, input, signal).Get(ctx, &state)
		})
		selector.AddReceive(cancelCh, func(c workflow.ReceiveChannel, more bool) {
			c.Receive(ctx, &cancelSignal)
			cancelled = true
		})
		selector.AddReceive(modifyCh, func(c workflow.ReceiveChannel, more bool) {
			var signal ProjectAgentModifyConstraintsSignal
			c.Receive(ctx, &signal)
			signalErr = workflow.ExecuteActivity(ctx, projectAgentModifyConstraintsActivity, input, signal).Get(ctx, &state)
		})
		selector.AddReceive(resumeCh, func(c workflow.ReceiveChannel, more bool) {
			var signal ProjectAgentStepDecisionSignal
			c.Receive(ctx, &signal)
		})
		if state.Status == "running" {
			selector.AddFuture(workflow.NewTimer(ctx, projectAgentPollInterval), func(workflow.Future) {
				timerFired = true
			})
		}
		selector.Select(ctx)

		if signalErr != nil {
			return ProjectAgentWorkflowState{}, signalErr
		}
		if timerFired {
			continue
		}
		if cancelled {
			if err := workflow.ExecuteActivity(ctx, projectAgentCancelActivity, input, cancelSignal).Get(ctx, &state); err != nil {
				return ProjectAgentWorkflowState{}, err
			}
			return state, nil
		}
	}
}

func projectAgentWorkflowTerminal(status string) bool {
	switch status {
	case "succeeded", "failed", "cancelled":
		return true
	default:
		return false
	}
}
