package workflows

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/commerce"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	BeginCommerceFinalComposeActivityName  = "BeginCommerceFinalCompose"
	CommitCommerceFinalComposeActivityName = "CommitCommerceFinalCompose"
	FailCommerceFinalComposeActivityName   = "FailCommerceFinalCompose"
)

type CommerceFinalComposeInput struct {
	Identity                 commerce.UnitGenerationIdentity `json:"identity"`
	WorkflowRunID            string                          `json:"workflowRunId"`
	ProductionRunID          string                          `json:"productionRunId"`
	TimelineID               string                          `json:"timelineId"`
	ExpectedTimelineRevision int64                           `json:"expectedTimelineRevision"`
	Title                    string                          `json:"title"`
	Resolution               string                          `json:"resolution"`
	AspectRatio              string                          `json:"aspectRatio"`
	CreatedBy                string                          `json:"createdBy"`
	AttemptGeneration        int                             `json:"attemptGeneration"`
}

type CommerceFinalComposeOutput struct {
	Identity            commerce.UnitGenerationIdentity `json:"identity"`
	ProductionRunID     string                          `json:"productionRunId"`
	TimelineID          string                          `json:"timelineId"`
	FinalVideoVersionID string                          `json:"finalVideoVersionId"`
	ArtifactID          string                          `json:"artifactId"`
	MediaFileID         string                          `json:"mediaFileId"`
	StorageKey          string                          `json:"storageKey"`
	Status              commerce.ProductionRunStatus    `json:"status"`
}

type CommitCommerceFinalComposeInput struct {
	WorkflowInput CommerceFinalComposeInput         `json:"workflowInput"`
	Attempt       CommerceReferenceImageItemAttempt `json:"attempt"`
	Result        ComposeFinalVideoOutput           `json:"result"`
}

type FailCommerceFinalComposeInput struct {
	WorkflowInput CommerceFinalComposeInput         `json:"workflowInput"`
	Attempt       CommerceReferenceImageItemAttempt `json:"attempt"`
	ErrorCode     string                            `json:"errorCode"`
	ErrorMessage  string                            `json:"errorMessage"`
	Retryable     bool                              `json:"retryable"`
	Cancelled     bool                              `json:"cancelled"`
}

type CommerceFinalComposePort interface {
	BeginCommerceFinalCompose(context.Context, CommerceFinalComposeInput) (CommerceReferenceImageItemAttempt, error)
	CommitCommerceFinalCompose(context.Context, CommitCommerceFinalComposeInput) (CommerceFinalComposeOutput, error)
	FailCommerceFinalCompose(context.Context, FailCommerceFinalComposeInput) error
}

func RegisterCommerceFinalWorkflow(registrar CommerceWorkflowRegistrar) {
	registrar.RegisterWorkflowWithOptions(CommerceFinalComposeWorkflow, workflow.RegisterOptions{Name: CommerceFinalComposeWorkflowName})
}

func RegisterCommerceFinalActivities(registrar CommerceActivityRegistrar, activities CommerceActivities) {
	registrar.RegisterActivityWithOptions(activities.BeginCommerceFinalCompose, activity.RegisterOptions{Name: BeginCommerceFinalComposeActivityName})
	registrar.RegisterActivityWithOptions(activities.CommitCommerceFinalCompose, activity.RegisterOptions{Name: CommitCommerceFinalComposeActivityName})
	registrar.RegisterActivityWithOptions(activities.FailCommerceFinalCompose, activity.RegisterOptions{Name: FailCommerceFinalComposeActivityName})
}

func CommerceFinalComposeWorkflow(ctx workflow.Context, input CommerceFinalComposeInput) (result CommerceFinalComposeOutput, resultErr error) {
	if err := validateCommerceFinalComposeInput(input); err != nil {
		return CommerceFinalComposeOutput{}, commerceWorkflowError(CommerceCodeWorkflowInputInvalid, err)
	}
	activityCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var attempt CommerceReferenceImageItemAttempt
	if err := workflow.ExecuteActivity(activityCtx, BeginCommerceFinalComposeActivityName, input).Get(activityCtx, &attempt); err != nil {
		return CommerceFinalComposeOutput{}, err
	}
	defer func() {
		if resultErr == nil {
			return
		}
		code, message := workflowExecutionError(resultErr)
		cancelled := temporal.IsCanceledError(resultErr)
		if cancelled {
			code, message = "WORKFLOW_CANCELLED", "用户取消成片合成任务"
		}
		disconnected, _ := workflow.NewDisconnectedContext(ctx)
		disconnected = workflow.WithActivityOptions(disconnected, defaultActivityOptions())
		if err := workflow.ExecuteActivity(disconnected, FailCommerceFinalComposeActivityName, FailCommerceFinalComposeInput{
			WorkflowInput: input, Attempt: attempt, ErrorCode: code, ErrorMessage: message,
			Retryable: !cancelled && code != CommerceCodeGenerationMismatch, Cancelled: cancelled,
		}).Get(disconnected, nil); err != nil {
			workflow.GetLogger(ctx).Error("failed to finalize commerce final compose", "error", err)
		}
	}()

	composeOptions := defaultActivityOptions()
	composeOptions.TaskQueue = MediaTaskQueue
	composeOptions.StartToCloseTimeout = 30 * time.Minute
	composeOptions.RetryPolicy = &temporal.RetryPolicy{MaximumAttempts: 1}
	composeCtx := workflow.WithActivityOptions(ctx, composeOptions)
	var composed ComposeFinalVideoOutput
	if err := workflow.ExecuteActivity(composeCtx, "ComposeFinalVideo", ComposeFinalVideoInput{
		OrganizationID:           input.Identity.OrganizationID,
		ProjectID:                input.Identity.ProjectID,
		WorkflowRunID:            input.WorkflowRunID,
		CreatedBy:                input.CreatedBy,
		TimelineID:               input.TimelineID,
		Title:                    input.Title,
		AspectRatio:              input.AspectRatio,
		Resolution:               input.Resolution,
		CommerceIdentity:         &input.Identity,
		ExpectedTimelineRevision: input.ExpectedTimelineRevision,
	}).Get(composeCtx, &composed); err != nil {
		return CommerceFinalComposeOutput{}, err
	}
	if err := workflow.ExecuteActivity(activityCtx, CommitCommerceFinalComposeActivityName, CommitCommerceFinalComposeInput{
		WorkflowInput: input, Attempt: attempt, Result: composed,
	}).Get(activityCtx, &result); err != nil {
		return CommerceFinalComposeOutput{}, err
	}
	return result, nil
}

func (a CommerceActivities) BeginCommerceFinalCompose(ctx context.Context, input CommerceFinalComposeInput) (CommerceReferenceImageItemAttempt, error) {
	port, ok := a.finalComposePort()
	if !ok {
		return CommerceReferenceImageItemAttempt{}, commerceActivityPortError()
	}
	return port.BeginCommerceFinalCompose(ctx, input)
}

func (a CommerceActivities) CommitCommerceFinalCompose(ctx context.Context, input CommitCommerceFinalComposeInput) (CommerceFinalComposeOutput, error) {
	port, ok := a.finalComposePort()
	if !ok {
		return CommerceFinalComposeOutput{}, commerceActivityPortError()
	}
	return port.CommitCommerceFinalCompose(ctx, input)
}

func (a CommerceActivities) FailCommerceFinalCompose(ctx context.Context, input FailCommerceFinalComposeInput) error {
	port, ok := a.finalComposePort()
	if !ok {
		return commerceActivityPortError()
	}
	return port.FailCommerceFinalCompose(ctx, input)
}

func (a CommerceActivities) finalComposePort() (CommerceFinalComposePort, bool) {
	port, ok := a.Ports.(CommerceFinalComposePort)
	return port, ok && port != nil
}

func CommerceFinalComposeSubjectHash(input CommerceFinalComposeInput) (string, error) {
	return commerceContractHash(map[string]any{
		"contractVersion":  "commerce-final-compose-subject/v1",
		"identity":         input.Identity,
		"timelineId":       input.TimelineID,
		"timelineRevision": input.ExpectedTimelineRevision,
		"title":            strings.TrimSpace(input.Title),
		"resolution":       strings.TrimSpace(input.Resolution),
		"aspectRatio":      strings.TrimSpace(input.AspectRatio),
	})
}

func validateCommerceFinalComposeInput(input CommerceFinalComposeInput) error {
	if err := ValidateCommerceUnitGenerationIdentity(input.Identity); err != nil {
		return err
	}
	if strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.ProductionRunID) == "" ||
		strings.TrimSpace(input.TimelineID) == "" || input.ExpectedTimelineRevision < 1 ||
		strings.TrimSpace(input.CreatedBy) == "" || input.AttemptGeneration < 1 {
		return errors.New("commerce final compose identity is incomplete")
	}
	if strings.TrimSpace(input.AspectRatio) == "" || strings.TrimSpace(input.Resolution) == "" {
		return errors.New("commerce final compose output settings are required")
	}
	return nil
}
