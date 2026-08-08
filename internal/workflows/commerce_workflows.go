package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/commerce"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	CommerceProjectSetupWorkflowName          = "CommerceProjectSetupWorkflow"
	CommerceScriptUnitPreparationWorkflowName = "CommerceScriptUnitPreparationWorkflow"
	CommerceStoryboardPlanningWorkflowName    = "CommerceStoryboardPlanningWorkflow"
	CommerceReferenceImageBatchWorkflowName   = "CommerceReferenceImageBatchWorkflow"
	CommerceVideoPromptBatchWorkflowName      = "CommerceVideoPromptBatchWorkflow"
	CommerceShotVideoBatchWorkflowName        = "CommerceShotVideoBatchWorkflow"
	CommerceShotVideoWorkflowName             = "CommerceShotVideoWorkflow"
	CommerceFinalComposeWorkflowName          = "CommerceFinalComposeWorkflow"

	ExecuteCommerceProjectSetupActivityName         = "ExecuteCommerceProjectSetup"
	FailCommerceProjectSetupActivityName            = "FailCommerceProjectSetup"
	LoadCommerceScriptUnitPreparationActivityName   = "LoadCommerceScriptUnitPreparation"
	ResolveCommerceLanguageActivityName             = "ResolveCommerceLanguage"
	PersistCommerceLanguageResolutionActivityName   = "PersistCommerceLanguageResolution"
	ConfirmCommerceLanguageActivityName             = "ConfirmCommerceLanguage"
	LocalizeCommerceScriptActivityName              = "LocalizeCommerceScript"
	ReviewCommerceLocalizationActivityName          = "ReviewCommerceLocalization"
	CommitCommerceScriptUnitPreparationActivityName = "CommitCommerceScriptUnitPreparation"
	FailCommerceGenerationWorkflowActivityName      = "FailCommerceGenerationWorkflow"
	LoadCommerceStoryboardPlanningActivityName      = "LoadCommerceStoryboardPlanning"
	OrganizeCommerceScriptActivityName              = "OrganizeCommerceScript"
	PlanCommerceStoryboardActivityName              = "PlanCommerceStoryboard"
	ReviewCommerceStoryboardActivityName            = "ReviewCommerceStoryboard"
	CommitCommerceStoryboardPlanActivityName        = "CommitCommerceStoryboardPlan"

	CommerceLanguageConfirmationSignalName      = "commerce_language_confirmation"
	CommerceSetupLanguageConfirmationSignalName = "commerce_setup_language_confirmation"
)

type CommerceWorkflowRegistrar interface {
	RegisterWorkflowWithOptions(interface{}, workflow.RegisterOptions)
}

type CommerceActivityRegistrar interface {
	RegisterActivityWithOptions(interface{}, activity.RegisterOptions)
}

type CommerceWorkflowPhase string

const (
	CommercePhasePreparation        CommerceWorkflowPhase = "script_unit_preparation"
	CommercePhaseScriptOrganization CommerceWorkflowPhase = "script_organization"
	CommercePhaseStoryboard         CommerceWorkflowPhase = "storyboard_planning"
	CommercePhaseImagePrompt        CommerceWorkflowPhase = "reference_image_prompt"
	CommercePhaseImageFidelity      CommerceWorkflowPhase = "reference_image_fidelity"
	CommercePhaseVideoPrompt        CommerceWorkflowPhase = "video_prompt"
	CommercePhaseVideoRender        CommerceWorkflowPhase = "video_render"
	CommercePhaseFinalCompose       CommerceWorkflowPhase = "final_compose"
)

type CommerceProjectSetupInput struct {
	OrganizationID            string `json:"organizationId"`
	ProjectID                 string `json:"projectId"`
	SetupSessionID            string `json:"setupSessionId"`
	ExpectedSessionRevision   int64  `json:"expectedSessionRevision"`
	WorkflowTemplateVersionID string `json:"workflowTemplateVersionId"`
	ProductID                 string `json:"productId"`
	ProductVersionID          string `json:"productVersionId"`
	ScriptUnitID              string `json:"scriptUnitId"`
	SourceScriptVersionID     string `json:"sourceScriptVersionId"`
	RequestedBy               string `json:"requestedBy"`
	LanguageResolutionID      string `json:"languageResolutionId,omitempty"`
	ConfirmedTargetLanguage   string `json:"confirmedTargetLanguage,omitempty"`
}

type CommerceProjectSetupOutput struct {
	Identity                        commerce.UnitGenerationIdentity `json:"identity"`
	SetupSessionID                  string                          `json:"setupSessionId"`
	ProjectGenerationID             string                          `json:"projectGenerationId"`
	VideoProductionBindingID        string                          `json:"videoProductionBindingId"`
	VideoProductionBindingRevision  int64                           `json:"videoProductionBindingRevision"`
	CommerceWorkflowBindingID       string                          `json:"commerceWorkflowBindingId"`
	CommerceWorkflowBindingRevision int64                           `json:"commerceWorkflowBindingRevision"`
	ScriptUnitGenerationID          string                          `json:"scriptUnitGenerationId"`
	ScriptUnitGenerationNo          int64                           `json:"scriptUnitGenerationNo"`
	LocalizationID                  string                          `json:"localizationId"`
	ReferencePackID                 string                          `json:"referencePackId"`
	ProductionWorkflowRunID         string                          `json:"productionWorkflowRunId,omitempty"`
	LanguageResolutionID            string                          `json:"languageResolutionId,omitempty"`
	SuggestedTargetLanguage         string                          `json:"suggestedTargetLanguage,omitempty"`
	NeedsUserConfirmation           bool                            `json:"needsUserConfirmation"`
	Status                          string                          `json:"status"`
}

type CommerceProjectSetupFailureInput struct {
	WorkflowInput CommerceProjectSetupInput `json:"workflowInput"`
	ErrorCode     string                    `json:"errorCode"`
	ErrorMessage  string                    `json:"errorMessage"`
}

type CommerceSetupLanguageConfirmationSignal struct {
	SetupSessionID       string `json:"setupSessionId"`
	LanguageResolutionID string `json:"languageResolutionId"`
	TargetLanguage       string `json:"targetLanguage"`
}

type CommerceScriptUnitPreparationInput struct {
	Identity                commerce.ScriptUnitPreparationIdentity `json:"identity"`
	WorkflowRunID           string                                 `json:"workflowRunId"`
	CreatedBy               string                                 `json:"createdBy"`
	AttemptGeneration       int                                    `json:"attemptGeneration"`
	ProjectControlCommandID string                                 `json:"projectControlCommandId,omitempty"`
}

type CommerceStoryboardPlanningInput struct {
	Identity          commerce.UnitGenerationIdentity `json:"identity"`
	WorkflowRunID     string                          `json:"workflowRunId"`
	CreatedBy         string                          `json:"createdBy"`
	AttemptGeneration int                             `json:"attemptGeneration"`
}

type CommerceReferenceImageBatchInput struct {
	Identity                   commerce.UnitGenerationIdentity `json:"identity"`
	WorkflowRunID              string                          `json:"workflowRunId"`
	ProductionRunID            string                          `json:"productionRunId"`
	StoryboardPlanID           string                          `json:"storyboardPlanId"`
	PlanEditRevision           int                             `json:"planEditRevision"`
	Operation                  string                          `json:"operation"`
	ShotIDs                    []string                        `json:"shotIds"`
	Force                      bool                            `json:"force"`
	ReuseGeneratedMedia        bool                            `json:"reuseGeneratedMedia,omitempty"`
	ReuseGeneratedMediaShotIDs []string                        `json:"reuseGeneratedMediaShotIds,omitempty"`
	Concurrency                int                             `json:"concurrency"`
	CreatedBy                  string                          `json:"createdBy"`
	AttemptGeneration          int                             `json:"attemptGeneration"`
}

type CommerceVideoBatchInput struct {
	Identity          commerce.UnitGenerationIdentity `json:"identity"`
	WorkflowRunID     string                          `json:"workflowRunId"`
	ProductionRunID   string                          `json:"productionRunId"`
	StoryboardPlanID  string                          `json:"storyboardPlanId"`
	PlanEditRevision  int                             `json:"planEditRevision"`
	Operation         string                          `json:"operation"`
	ShotIDs           []string                        `json:"shotIds"`
	Force             bool                            `json:"force"`
	Concurrency       int                             `json:"concurrency"`
	Resolution        string                          `json:"resolution"`
	CreatedBy         string                          `json:"createdBy"`
	AttemptGeneration int                             `json:"attemptGeneration"`
}

type CommerceAgentProvenance struct {
	Role              string `json:"role"`
	Round             int    `json:"round"`
	NodeRunID         string `json:"nodeRunId"`
	ProviderRequestID string `json:"providerRequestId"`
	ProviderCallID    string `json:"providerCallId"`
	ProviderModelID   string `json:"providerModelId"`
	PromptTemplateKey string `json:"promptTemplateKey"`
	PromptVersionID   string `json:"promptVersionId"`
	PromptHash        string `json:"promptHash"`
}

type CommerceAgentCallInput struct {
	PreparationIdentity *commerce.ScriptUnitPreparationIdentity `json:"preparationIdentity,omitempty"`
	GenerationIdentity  *commerce.UnitGenerationIdentity        `json:"unitGenerationIdentity,omitempty"`
	WorkflowRunID       string                                  `json:"workflowRunId"`
	AttemptGeneration   int                                     `json:"attemptGeneration"`
	Phase               CommerceWorkflowPhase                   `json:"phase"`
	SubjectKey          string                                  `json:"subjectKey,omitempty"`
	Round               int                                     `json:"round"`
	Binding             CommerceAgentBinding                    `json:"binding"`
	InputLanguage       string                                  `json:"inputLanguage,omitempty"`
	OutputLanguage      string                                  `json:"outputLanguage,omitempty"`
	Context             json.RawMessage                         `json:"context"`
	ReviewerIssues      []CommerceReviewIssue                   `json:"reviewerIssues,omitempty"`
	References          []provider.GatewayImageReference        `json:"references,omitempty"`
}

type CommerceAgentCallOutput struct {
	RawOutput  string                  `json:"rawOutput"`
	Provenance CommerceAgentProvenance `json:"provenance"`
}

type CommerceLanguageResolutionState struct {
	ResolutionID string                             `json:"resolutionId"`
	Revision     int64                              `json:"revision"`
	InputHash    string                             `json:"inputHash"`
	Status       string                             `json:"status"`
	Contract     CommerceLanguageResolutionContract `json:"contract"`
}

type CommerceLanguageConfirmationSignal struct {
	Identity         commerce.ScriptUnitPreparationIdentity `json:"identity"`
	ResolutionID     string                                 `json:"resolutionId"`
	ExpectedRevision int64                                  `json:"expectedRevision"`
	InputHash        string                                 `json:"inputHash"`
	TargetLanguage   string                                 `json:"targetLanguage"`
}

type PersistCommerceLanguageResolutionInput struct {
	WorkflowInput CommerceScriptUnitPreparationInput    `json:"workflowInput"`
	Snapshot      CommerceScriptUnitPreparationSnapshot `json:"snapshot"`
	Contract      CommerceLanguageResolutionContract    `json:"contract"`
	Provenance    CommerceAgentProvenance               `json:"provenance"`
}

type ConfirmCommerceLanguageInput struct {
	WorkflowInput CommerceScriptUnitPreparationInput    `json:"workflowInput"`
	Snapshot      CommerceScriptUnitPreparationSnapshot `json:"snapshot"`
	Current       CommerceLanguageResolutionState       `json:"current"`
	Signal        CommerceLanguageConfirmationSignal    `json:"signal"`
}

type CommerceScriptUnitPreparationCommit struct {
	WorkflowInput      CommerceScriptUnitPreparationInput    `json:"workflowInput"`
	Snapshot           CommerceScriptUnitPreparationSnapshot `json:"snapshot"`
	LanguageResolution CommerceLanguageResolutionState       `json:"languageResolution"`
	Localization       CommerceLocalizationContract          `json:"localization"`
	LocalizationReview CommerceLocalizationReviewContract    `json:"localizationReview"`
	Timing             CommerceTimingAnalysis                `json:"timing"`
	AgentCalls         []CommerceAgentProvenance             `json:"agentCalls"`
}

type CommerceScriptUnitPreparationCommitResult struct {
	Identity                commerce.UnitGenerationIdentity `json:"identity"`
	LocalizationID          string                          `json:"localizationId"`
	ProductionWorkflowRunID string                          `json:"productionWorkflowRunId"`
	Status                  string                          `json:"status"`
	InputHash               string                          `json:"inputHash"`
}

type CommerceScriptUnitPreparationOutput struct {
	Identity                commerce.UnitGenerationIdentity           `json:"identity"`
	LanguageResolution      CommerceLanguageResolutionState           `json:"languageResolution"`
	Localization            CommerceLocalizationContract              `json:"localization"`
	LocalizationReview      CommerceLocalizationReviewContract        `json:"localizationReview"`
	Timing                  CommerceTimingAnalysis                    `json:"timing"`
	Commit                  CommerceScriptUnitPreparationCommitResult `json:"commit"`
	AgentCalls              []CommerceAgentProvenance                 `json:"agentCalls"`
	ProductionWorkflowRunID string                                    `json:"productionWorkflowRunId"`
}

type CommerceGenerationWorkflowFailureInput struct {
	PreparationInput  *CommerceScriptUnitPreparationInput `json:"preparationInput,omitempty"`
	OrganizationInput *CommerceScriptOrganizationInput    `json:"organizationInput,omitempty"`
	StoryboardInput   *CommerceStoryboardPlanningInput    `json:"storyboardInput,omitempty"`
	Cancelled         bool                                `json:"cancelled"`
	ErrorCode         string                              `json:"errorCode"`
	ErrorMessage      string                              `json:"errorMessage"`
}

type CommerceStoryboardPlanCommit struct {
	WorkflowInput           CommerceStoryboardPlanningInput     `json:"workflowInput"`
	Snapshot                CommerceStoryboardPlanningSnapshot  `json:"snapshot"`
	SalesScriptContractID   string                              `json:"salesScriptContractId"`
	SalesScriptContractHash string                              `json:"salesScriptContractHash"`
	SalesScript             CommerceSalesScriptContract         `json:"salesScript"`
	DeterministicPlan       CommerceStoryboardDeterministicPlan `json:"deterministicPlan"`
	Plan                    CommerceStoryboardPlanContract      `json:"plan"`
	Review                  CommerceStoryboardReviewContract    `json:"review"`
	Projection              CommerceStoryboardProjection        `json:"projection"`
	AgentCalls              []CommerceAgentProvenance           `json:"agentCalls"`
}

type CommerceStoryboardPlanCommitResult struct {
	Identity         commerce.UnitGenerationIdentity `json:"identity"`
	StoryboardPlanID string                          `json:"storyboardPlanId"`
	PlanRevision     int                             `json:"planRevision"`
	ShotCount        int                             `json:"shotCount"`
	Status           string                          `json:"status"`
	PlanHash         string                          `json:"planHash"`
}

type CommerceStoryboardPlanningOutput struct {
	Identity                commerce.UnitGenerationIdentity     `json:"identity"`
	SalesScriptContractID   string                              `json:"salesScriptContractId"`
	SalesScriptContractHash string                              `json:"salesScriptContractHash"`
	SalesScript             CommerceSalesScriptContract         `json:"salesScript"`
	DeterministicPlan       CommerceStoryboardDeterministicPlan `json:"deterministicPlan"`
	Plan                    CommerceStoryboardPlanContract      `json:"plan"`
	Review                  CommerceStoryboardReviewContract    `json:"review"`
	Projection              CommerceStoryboardProjection        `json:"projection"`
	Commit                  CommerceStoryboardPlanCommitResult  `json:"commit"`
	AgentCalls              []CommerceAgentProvenance           `json:"agentCalls"`
}

// CommerceWorkflowActivityPorts is the persistence boundary required by the
// commerce workflows. Implementations must perform each commit atomically and
// enforce the full UnitGeneration identity with compare-and-swap semantics.
// Workflows and activities intentionally contain no commerce SQL.
type CommerceWorkflowActivityPorts interface {
	AssertCommerceWorkflowIdentity(context.Context, CommerceAgentCallInput) error
	LoadScriptUnitPreparation(context.Context, CommerceScriptUnitPreparationInput) (CommerceScriptUnitPreparationSnapshot, error)
	PersistLanguageResolution(context.Context, PersistCommerceLanguageResolutionInput) (CommerceLanguageResolutionState, error)
	ConfirmLanguage(context.Context, ConfirmCommerceLanguageInput) (CommerceLanguageResolutionState, error)
	CommitScriptUnitPreparation(context.Context, CommerceScriptUnitPreparationCommit) (CommerceScriptUnitPreparationCommitResult, error)
	FailCommerceGenerationWorkflow(context.Context, CommerceGenerationWorkflowFailureInput) error
	ClaimSalesScriptContract(context.Context, CommerceSalesScriptContractClaimInput) (CommerceSalesScriptContractClaimResult, error)
	CommitSalesScriptContract(context.Context, CommerceSalesScriptContractCommitInput) (CommerceSalesScriptContractState, error)
	LoadStoryboardPlanning(context.Context, CommerceStoryboardPlanningInput) (CommerceStoryboardPlanningSnapshot, error)
	CommitStoryboardPlan(context.Context, CommerceStoryboardPlanCommit) (CommerceStoryboardPlanCommitResult, error)
}

type CommerceAgentReplayPort interface {
	FindCommerceAgentReplay(context.Context, CommerceAgentCallInput) (CommerceAgentCallOutput, bool, error)
}

type CommerceProjectSetupPort interface {
	ExecuteCommerceProjectSetup(context.Context, CommerceProjectSetupInput) (CommerceProjectSetupOutput, error)
	FailCommerceProjectSetup(context.Context, CommerceProjectSetupFailureInput) error
}

type CommerceActivities struct {
	Core      Activities
	Ports     CommerceWorkflowActivityPorts
	SetupPort CommerceProjectSetupPort
}

func NewCommerceActivities(core Activities, ports CommerceWorkflowActivityPorts) CommerceActivities {
	activities := CommerceActivities{Core: core, Ports: ports}
	if setupPort, ok := ports.(CommerceProjectSetupPort); ok {
		activities.SetupPort = setupPort
	}
	return activities
}

func NewCommerceSetupActivities(core Activities, setupPort CommerceProjectSetupPort) CommerceActivities {
	return CommerceActivities{Core: core, SetupPort: setupPort}
}

func RegisterCommerceSetupWorkflow(registrar CommerceWorkflowRegistrar) {
	registrar.RegisterWorkflowWithOptions(CommerceProjectSetupWorkflow, workflow.RegisterOptions{Name: CommerceProjectSetupWorkflowName})
}

func RegisterCommerceSetupActivity(registrar CommerceActivityRegistrar, activities CommerceActivities) {
	registrar.RegisterActivityWithOptions(activities.ExecuteCommerceProjectSetup, activity.RegisterOptions{Name: ExecuteCommerceProjectSetupActivityName})
	registrar.RegisterActivityWithOptions(activities.FailCommerceProjectSetup, activity.RegisterOptions{Name: FailCommerceProjectSetupActivityName})
}

func RegisterCommerceWorkflows(registrar CommerceWorkflowRegistrar) {
	RegisterCommerceSetupWorkflow(registrar)
	RegisterCommerceGenerationWorkflows(registrar)
}

func RegisterCommerceGenerationWorkflows(registrar CommerceWorkflowRegistrar) {
	registrar.RegisterWorkflowWithOptions(CommerceScriptDerivationBatchWorkflow, workflow.RegisterOptions{Name: CommerceScriptDerivationBatchWorkflowName})
	registrar.RegisterWorkflowWithOptions(CommerceScriptDerivationItemWorkflow, workflow.RegisterOptions{Name: CommerceScriptDerivationItemWorkflowName})
	registrar.RegisterWorkflowWithOptions(CommerceScriptUnitPreparationWorkflow, workflow.RegisterOptions{Name: CommerceScriptUnitPreparationWorkflowName})
	registrar.RegisterWorkflowWithOptions(CommerceScriptOrganizationWorkflow, workflow.RegisterOptions{Name: CommerceScriptOrganizationWorkflowName})
	registrar.RegisterWorkflowWithOptions(CommerceStoryboardPlanningWorkflow, workflow.RegisterOptions{Name: CommerceStoryboardPlanningWorkflowName})
	RegisterCommerceReferenceImageWorkflow(registrar)
	RegisterCommerceVideoWorkflows(registrar)
	registrar.RegisterWorkflowWithOptions(CommerceDirectVideoWorkflow, workflow.RegisterOptions{Name: CommerceDirectVideoWorkflowName})
	RegisterCommerceFinalWorkflow(registrar)
	RegisterCommerceScriptUnitBatchCoordinatorWorkflow(registrar)
}

func RegisterCommerceActivities(registrar CommerceActivityRegistrar, activities CommerceActivities) {
	RegisterCommerceSetupActivity(registrar, activities)
	RegisterCommerceGenerationActivities(registrar, activities)
}

func RegisterCommerceGenerationActivities(registrar CommerceActivityRegistrar, activities CommerceActivities) {
	registrar.RegisterActivityWithOptions(activities.StartCommerceScriptDerivationBatch, activity.RegisterOptions{Name: StartCommerceScriptDerivationBatchActivity})
	registrar.RegisterActivityWithOptions(activities.LoadCommerceScriptDerivationItem, activity.RegisterOptions{Name: LoadCommerceScriptDerivationItemActivity})
	registrar.RegisterActivityWithOptions(activities.CallCommerceScriptDerivationAgent, activity.RegisterOptions{Name: CallCommerceScriptDerivationAgentActivity})
	registrar.RegisterActivityWithOptions(activities.CommitCommerceScriptDerivationItem, activity.RegisterOptions{Name: CommitCommerceScriptDerivationItemActivity})
	registrar.RegisterActivityWithOptions(activities.FailCommerceScriptDerivationItem, activity.RegisterOptions{Name: FailCommerceScriptDerivationItemActivity})
	registrar.RegisterActivityWithOptions(activities.FinalizeCommerceScriptDerivationBatch, activity.RegisterOptions{Name: FinalizeCommerceScriptDerivationBatchActivity})
	registrar.RegisterActivityWithOptions(activities.CancelCommerceScriptDerivationBatch, activity.RegisterOptions{Name: CancelCommerceScriptDerivationBatchActivity})
	registrar.RegisterActivityWithOptions(activities.LoadCommerceScriptUnitPreparation, activity.RegisterOptions{Name: LoadCommerceScriptUnitPreparationActivityName})
	registrar.RegisterActivityWithOptions(activities.ResolveCommerceLanguage, activity.RegisterOptions{Name: ResolveCommerceLanguageActivityName})
	registrar.RegisterActivityWithOptions(activities.PersistCommerceLanguageResolution, activity.RegisterOptions{Name: PersistCommerceLanguageResolutionActivityName})
	registrar.RegisterActivityWithOptions(activities.ConfirmCommerceLanguage, activity.RegisterOptions{Name: ConfirmCommerceLanguageActivityName})
	registrar.RegisterActivityWithOptions(activities.LocalizeCommerceScript, activity.RegisterOptions{Name: LocalizeCommerceScriptActivityName})
	registrar.RegisterActivityWithOptions(activities.ReviewCommerceLocalization, activity.RegisterOptions{Name: ReviewCommerceLocalizationActivityName})
	registrar.RegisterActivityWithOptions(activities.CommitCommerceScriptUnitPreparation, activity.RegisterOptions{Name: CommitCommerceScriptUnitPreparationActivityName})
	registrar.RegisterActivityWithOptions(activities.FailCommerceGenerationWorkflow, activity.RegisterOptions{Name: FailCommerceGenerationWorkflowActivityName})
	registrar.RegisterActivityWithOptions(activities.ClaimCommerceSalesScriptContract, activity.RegisterOptions{Name: ClaimCommerceSalesScriptContractActivityName})
	registrar.RegisterActivityWithOptions(activities.CommitCommerceSalesScriptContract, activity.RegisterOptions{Name: CommitCommerceSalesScriptContractActivityName})
	registrar.RegisterActivityWithOptions(activities.LoadCommerceStoryboardPlanning, activity.RegisterOptions{Name: LoadCommerceStoryboardPlanningActivityName})
	registrar.RegisterActivityWithOptions(activities.OrganizeCommerceScript, activity.RegisterOptions{Name: OrganizeCommerceScriptActivityName})
	registrar.RegisterActivityWithOptions(activities.PlanCommerceStoryboard, activity.RegisterOptions{Name: PlanCommerceStoryboardActivityName})
	registrar.RegisterActivityWithOptions(activities.ReviewCommerceStoryboard, activity.RegisterOptions{Name: ReviewCommerceStoryboardActivityName})
	registrar.RegisterActivityWithOptions(activities.CommitCommerceStoryboardPlan, activity.RegisterOptions{Name: CommitCommerceStoryboardPlanActivityName})
	RegisterCommerceReferenceImageActivities(registrar, activities)
	RegisterCommerceVideoActivities(registrar, activities)
	registrar.RegisterActivityWithOptions(activities.Core.CreateCommerceDirectVideoTask, activity.RegisterOptions{Name: CreateCommerceDirectVideoActivity})
	registrar.RegisterActivityWithOptions(activities.Core.PollCommerceDirectVideoTask, activity.RegisterOptions{Name: PollCommerceDirectVideoActivity})
	registrar.RegisterActivityWithOptions(activities.Core.CompleteCommerceDirectVideo, activity.RegisterOptions{Name: CompleteCommerceDirectVideoActivity})
	registrar.RegisterActivityWithOptions(activities.Core.FailCommerceDirectVideo, activity.RegisterOptions{Name: FailCommerceDirectVideoActivity})
	registrar.RegisterActivityWithOptions(activities.Core.CancelCommerceDirectVideo, activity.RegisterOptions{Name: CancelCommerceDirectVideoActivity})
	RegisterCommerceFinalActivities(registrar, activities)
	RegisterCommerceScriptUnitBatchCoordinatorActivities(registrar, activities)
}

func CommerceProjectSetupWorkflow(ctx workflow.Context, input CommerceProjectSetupInput) (CommerceProjectSetupOutput, error) {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" ||
		strings.TrimSpace(input.SetupSessionID) == "" || input.ExpectedSessionRevision <= 0 ||
		strings.TrimSpace(input.WorkflowTemplateVersionID) == "" || strings.TrimSpace(input.ProductID) == "" ||
		strings.TrimSpace(input.ProductVersionID) == "" || strings.TrimSpace(input.ScriptUnitID) == "" ||
		strings.TrimSpace(input.SourceScriptVersionID) == "" || strings.TrimSpace(input.RequestedBy) == "" {
		return CommerceProjectSetupOutput{}, commerceWorkflowError(CommerceCodeWorkflowInputInvalid, errors.New("commerce project setup identity is incomplete"))
	}
	activityCtx := workflow.WithActivityOptions(ctx, commerceProjectSetupActivityOptions())
	var output CommerceProjectSetupOutput
	if err := workflow.ExecuteActivity(activityCtx, ExecuteCommerceProjectSetupActivityName, input).Get(activityCtx, &output); err != nil {
		code, message := commerceProjectSetupErrorFields(err)
		failureCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
		_ = workflow.ExecuteActivity(failureCtx, FailCommerceProjectSetupActivityName, CommerceProjectSetupFailureInput{
			WorkflowInput: input, ErrorCode: code, ErrorMessage: message,
		}).Get(failureCtx, nil)
		return CommerceProjectSetupOutput{}, temporal.NewNonRetryableApplicationError(message, code, err)
	}
	if err := validateCommerceProjectSetupOutput(input, output); err != nil {
		return CommerceProjectSetupOutput{}, commerceWorkflowError(CommerceCodeGenerationMismatch, errors.New("commerce project setup commit returned an incomplete identity"))
	}
	return output, nil
}

func commerceProjectSetupErrorFields(err error) (string, string) {
	var timeoutErr *temporal.TimeoutError
	if errors.As(err, &timeoutErr) {
		return provider.CodeUpstreamTimeout, "项目准备等待模型响应超时，请重试；已完成的供应商请求会自动复用"
	}
	return workflowErrorFields(err, codeActivityFailed)
}

func validateCommerceProjectSetupOutput(input CommerceProjectSetupInput, output CommerceProjectSetupOutput) error {
	if err := ValidateCommerceUnitGenerationIdentity(output.Identity); err != nil {
		return err
	}
	identity := output.Identity
	if output.SetupSessionID != input.SetupSessionID || output.Status != "completed" ||
		strings.TrimSpace(output.ProductionWorkflowRunID) == "" ||
		identity.OrganizationID != input.OrganizationID || identity.ProjectID != input.ProjectID ||
		identity.ProductID != input.ProductID || identity.ScriptUnitID != input.ScriptUnitID ||
		output.ProjectGenerationID != identity.ProjectGenerationID ||
		output.VideoProductionBindingID != identity.VideoProductionBindingID ||
		output.VideoProductionBindingRevision != identity.VideoProductionBindingRevision ||
		output.CommerceWorkflowBindingID != identity.CommerceWorkflowBindingID ||
		output.CommerceWorkflowBindingRevision != identity.CommerceWorkflowBindingRevision ||
		output.ScriptUnitGenerationID != identity.UnitGenerationID ||
		output.ScriptUnitGenerationNo != identity.UnitGenerationNo {
		return errors.New("commerce project setup output identity fields do not match")
	}
	return nil
}

func CommerceScriptUnitPreparationWorkflow(ctx workflow.Context, input CommerceScriptUnitPreparationInput) (output CommerceScriptUnitPreparationOutput, resultErr error) {
	if err := validateCommercePreparationWorkflowInput(input); err != nil {
		return output, commerceWorkflowError(CommerceCodeWorkflowInputInvalid, err)
	}
	defer finalizeCommerceGenerationWorkflowFailure(ctx, CommerceGenerationWorkflowFailureInput{PreparationInput: &input}, &resultErr)
	defaultCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	agentCtx := workflow.WithActivityOptions(ctx, commerceAgentActivityOptions())
	var snapshot CommerceScriptUnitPreparationSnapshot
	if err := workflow.ExecuteActivity(defaultCtx, LoadCommerceScriptUnitPreparationActivityName, input).Get(defaultCtx, &snapshot); err != nil {
		return CommerceScriptUnitPreparationOutput{}, err
	}
	if err := ValidateCommercePreparationSnapshot(input.Identity, snapshot); err != nil {
		return CommerceScriptUnitPreparationOutput{}, commerceWorkflowError(CommerceCodeGenerationMismatch, err)
	}

	agentCalls := make([]CommerceAgentProvenance, 0, 7)
	resolution, resolutionCall, err := resolveCommerceLanguageInWorkflow(agentCtx, input, snapshot)
	if err != nil {
		return CommerceScriptUnitPreparationOutput{}, err
	}
	agentCalls = append(agentCalls, resolutionCall)
	var resolutionState CommerceLanguageResolutionState
	if err := workflow.ExecuteActivity(defaultCtx, PersistCommerceLanguageResolutionActivityName, PersistCommerceLanguageResolutionInput{
		WorkflowInput: input, Snapshot: snapshot, Contract: resolution, Provenance: resolutionCall,
	}).Get(defaultCtx, &resolutionState); err != nil {
		return CommerceScriptUnitPreparationOutput{}, err
	}
	if err := validateCommerceLanguageResolutionState(snapshot, resolutionState); err != nil {
		return CommerceScriptUnitPreparationOutput{}, commerceWorkflowError(CommerceCodeLanguageContractInvalid, err)
	}

	localization, review, localizationCalls, err := prepareCommerceLocalizationInWorkflow(agentCtx, input, snapshot, resolutionState.Contract)
	if err != nil {
		return CommerceScriptUnitPreparationOutput{}, err
	}
	agentCalls = append(agentCalls, localizationCalls...)
	policy := commerceAdvisoryTimingPolicy(resolutionState.Contract.TargetLanguage)
	timing, err := AnalyzeCommerceTiming(localization, policy, snapshot.TargetDurationSeconds)
	if err != nil {
		return CommerceScriptUnitPreparationOutput{}, commerceWorkflowError(CommerceCodeLocalizationContractInvalid, err)
	}
	commitInput := CommerceScriptUnitPreparationCommit{
		WorkflowInput: input, Snapshot: snapshot, LanguageResolution: resolutionState,
		Localization: localization, LocalizationReview: review, Timing: timing, AgentCalls: agentCalls,
	}
	var committed CommerceScriptUnitPreparationCommitResult
	if err := workflow.ExecuteActivity(defaultCtx, CommitCommerceScriptUnitPreparationActivityName, commitInput).Get(defaultCtx, &committed); err != nil {
		return CommerceScriptUnitPreparationOutput{}, err
	}
	if err := ValidateCommercePreparationCommitIdentity(input.Identity, committed.Identity); err != nil ||
		committed.InputHash != snapshot.InputHash || committed.Status != "ready" ||
		(input.Identity.RebuildID == "" && strings.TrimSpace(committed.ProductionWorkflowRunID) == "") ||
		(input.Identity.RebuildID != "" && strings.TrimSpace(committed.ProductionWorkflowRunID) != "") {
		return CommerceScriptUnitPreparationOutput{}, commerceWorkflowError(CommerceCodeGenerationMismatch, errors.New("preparation commit returned a different generation identity"))
	}
	return CommerceScriptUnitPreparationOutput{
		Identity: committed.Identity, LanguageResolution: resolutionState, Localization: localization,
		LocalizationReview: review, Timing: timing, Commit: committed, AgentCalls: agentCalls,
		ProductionWorkflowRunID: committed.ProductionWorkflowRunID,
	}, nil
}

func CommerceStoryboardPlanningWorkflow(ctx workflow.Context, input CommerceStoryboardPlanningInput) (output CommerceStoryboardPlanningOutput, resultErr error) {
	if err := validateCommerceStoryboardWorkflowInput(input); err != nil {
		return output, commerceWorkflowError(CommerceCodeWorkflowInputInvalid, err)
	}
	defer finalizeCommerceGenerationWorkflowFailure(ctx, CommerceGenerationWorkflowFailureInput{StoryboardInput: &input}, &resultErr)
	defaultCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	agentCtx := workflow.WithActivityOptions(ctx, commerceAgentActivityOptions())
	owner := CommerceSalesScriptOwner{StoryboardInput: &input}
	salesScriptState, snapshot, organizerCalls, err := ensureCommerceSalesScriptContract(ctx, owner)
	if err != nil {
		return CommerceStoryboardPlanningOutput{}, err
	}
	salesScript := salesScriptState.Contract
	deterministicPlan, plan, review, projection, planningCalls, err := planCommerceStoryboardInWorkflow(agentCtx, input, snapshot, salesScript)
	if err != nil {
		return CommerceStoryboardPlanningOutput{}, err
	}
	outputAgentCalls := append(append([]CommerceAgentProvenance(nil), organizerCalls...), planningCalls...)
	commitInput := CommerceStoryboardPlanCommit{
		WorkflowInput: input, Snapshot: snapshot,
		SalesScriptContractID: salesScriptState.ContractID, SalesScriptContractHash: salesScriptState.ContractHash,
		SalesScript:       salesScript,
		DeterministicPlan: deterministicPlan,
		Plan:              plan, Review: review, Projection: projection, AgentCalls: planningCalls,
	}
	var committed CommerceStoryboardPlanCommitResult
	if err := workflow.ExecuteActivity(defaultCtx, CommitCommerceStoryboardPlanActivityName, commitInput).Get(defaultCtx, &committed); err != nil {
		return CommerceStoryboardPlanningOutput{}, err
	}
	if committed.Identity != input.Identity || committed.PlanHash != projection.PlanHash || committed.ShotCount != len(projection.Shots) || committed.Status != "ready" {
		return CommerceStoryboardPlanningOutput{}, commerceWorkflowError(CommerceCodeGenerationMismatch, errors.New("storyboard commit returned a different generation identity or plan hash"))
	}
	return CommerceStoryboardPlanningOutput{
		Identity: input.Identity, SalesScriptContractID: salesScriptState.ContractID,
		SalesScriptContractHash: salesScriptState.ContractHash,
		SalesScript:             salesScript, DeterministicPlan: deterministicPlan,
		Plan: plan, Review: review,
		Projection: projection, Commit: committed, AgentCalls: outputAgentCalls,
	}, nil
}

func finalizeCommerceGenerationWorkflowFailure(
	ctx workflow.Context,
	input CommerceGenerationWorkflowFailureInput,
	resultErr *error,
) {
	if resultErr == nil || *resultErr == nil {
		return
	}
	input.Cancelled = temporal.IsCanceledError(*resultErr)
	input.ErrorCode, input.ErrorMessage = workflowErrorFields(*resultErr, codeActivityFailed)
	failureCtx, _ := workflow.NewDisconnectedContext(ctx)
	failureCtx = workflow.WithActivityOptions(failureCtx, defaultActivityOptions())
	if err := workflow.ExecuteActivity(failureCtx, FailCommerceGenerationWorkflowActivityName, input).Get(failureCtx, nil); err != nil {
		workflow.GetLogger(ctx).Error("failed to persist commerce workflow terminal state", "error", err)
	}
}

func resolveCommerceLanguageInWorkflow(ctx workflow.Context, input CommerceScriptUnitPreparationInput, snapshot CommerceScriptUnitPreparationSnapshot) (CommerceLanguageResolutionContract, CommerceAgentProvenance, error) {
	var lastErr error
	feedback := []CommerceReviewIssue{}
	limit := commerceReviewRounds(snapshot.Bindings.LanguageResolver)
	for round := 1; round <= limit; round++ {
		callInput := CommerceAgentCallInput{
			PreparationIdentity: &input.Identity, WorkflowRunID: input.WorkflowRunID, AttemptGeneration: input.AttemptGeneration,
			Phase: CommercePhasePreparation, Round: round, Binding: snapshot.Bindings.LanguageResolver,
			InputLanguage: snapshot.SourceLanguageHint, OutputLanguage: snapshot.ExplicitTargetLanguage,
			Context: mustJSON(map[string]any{"snapshot": snapshot, "reviewerIssues": feedback}), ReviewerIssues: feedback,
		}
		var call CommerceAgentCallOutput
		if err := workflow.ExecuteActivity(ctx, ResolveCommerceLanguageActivityName, callInput).Get(ctx, &call); err != nil {
			return CommerceLanguageResolutionContract{}, CommerceAgentProvenance{}, err
		}
		contract, err := ParseCommerceLanguageResolution(call.RawOutput)
		if err == nil {
			contract.NeedsUserConfirmation = false
			err = ValidateCommerceLanguageResolution(contract, snapshot)
		}
		if err == nil {
			return contract, call.Provenance, nil
		}
		lastErr = err
		feedback = commerceValidationFeedback(CommerceCodeLanguageContractInvalid, "languageResolution", err)
	}
	return CommerceLanguageResolutionContract{}, CommerceAgentProvenance{}, commerceWorkflowError(CommerceCodeLanguageContractInvalid, lastErr)
}

func prepareCommerceLocalizationInWorkflow(ctx workflow.Context, input CommerceScriptUnitPreparationInput, snapshot CommerceScriptUnitPreparationSnapshot, resolution CommerceLanguageResolutionContract) (CommerceLocalizationContract, CommerceLocalizationReviewContract, []CommerceAgentProvenance, error) {
	if resolution.SourceLanguage == resolution.TargetLanguage {
		candidate := BuildCommerceIdentityLocalization(snapshot, resolution)
		if err := ValidateCommerceLocalization(candidate, snapshot, resolution); err != nil {
			return CommerceLocalizationContract{}, CommerceLocalizationReviewContract{}, nil,
				commerceWorkflowError(CommerceCodeLocalizationContractInvalid, err)
		}
		review := BuildCommerceIdentityLocalizationReview(candidate)
		if err := ValidateCommerceLocalizationReview(review, candidate); err != nil {
			return CommerceLocalizationContract{}, CommerceLocalizationReviewContract{}, nil,
				commerceWorkflowError(CommerceCodeLocalizationContractInvalid, err)
		}
		return candidate, review, []CommerceAgentProvenance{}, nil
	}

	limit := commerceReviewRounds(snapshot.Bindings.ScriptLocalizer)
	feedback := []CommerceReviewIssue{}
	calls := make([]CommerceAgentProvenance, 0, limit*2)
	var lastErr error
	for round := 1; round <= limit; round++ {
		var call CommerceAgentCallOutput
		if err := workflow.ExecuteActivity(ctx, LocalizeCommerceScriptActivityName, CommerceAgentCallInput{
			PreparationIdentity: &input.Identity, WorkflowRunID: input.WorkflowRunID, AttemptGeneration: input.AttemptGeneration,
			Phase: CommercePhasePreparation, Round: round, Binding: snapshot.Bindings.ScriptLocalizer,
			InputLanguage: resolution.SourceLanguage, OutputLanguage: resolution.TargetLanguage,
			Context: mustJSON(map[string]any{"snapshot": snapshot, "languageResolution": resolution, "reviewerIssues": feedback}), ReviewerIssues: feedback,
		}).Get(ctx, &call); err != nil {
			return CommerceLocalizationContract{}, CommerceLocalizationReviewContract{}, nil, err
		}
		calls = append(calls, call.Provenance)
		candidate, err := ParseCommerceLocalization(call.RawOutput)
		if err == nil {
			err = ValidateCommerceLocalization(candidate, snapshot, resolution)
		}
		if err != nil {
			lastErr = err
			feedback = commerceValidationFeedback(CommerceCodeLocalizationContractInvalid, "localization", err)
			continue
		}
		if err := ValidateCommerceLocalization(candidate, snapshot, resolution); err != nil {
			lastErr = err
			feedback = commerceValidationFeedback(CommerceCodeLocalizationContractInvalid, "localization", err)
			continue
		}
		var reviewCall CommerceAgentCallOutput
		if err := workflow.ExecuteActivity(ctx, ReviewCommerceLocalizationActivityName, CommerceAgentCallInput{
			PreparationIdentity: &input.Identity, WorkflowRunID: input.WorkflowRunID, AttemptGeneration: input.AttemptGeneration,
			Phase: CommercePhasePreparation, Round: round, Binding: snapshot.Bindings.LocalizationReviewer,
			InputLanguage: resolution.SourceLanguage, OutputLanguage: resolution.TargetLanguage,
			Context: mustJSON(map[string]any{"snapshot": snapshot, "candidate": candidate}),
		}).Get(ctx, &reviewCall); err != nil {
			return candidate, BuildCommerceIdentityLocalizationReview(candidate), calls, nil
		}
		calls = append(calls, reviewCall.Provenance)
		review, err := ParseCommerceLocalizationReview(reviewCall.RawOutput)
		if err == nil {
			review = CanonicalizeCommerceLocalizationReviewCoverage(review, candidate)
			err = ValidateCommerceLocalizationReview(review, candidate)
		}
		if err != nil {
			return candidate, BuildCommerceIdentityLocalizationReview(candidate), calls, nil
		}
		candidate, review, approved := ApplyCommerceLocalizationReviewPolicy(candidate, review)
		if approved {
			return candidate, review, calls, nil
		}
	}
	return CommerceLocalizationContract{}, CommerceLocalizationReviewContract{}, calls, commerceWorkflowError(CommerceCodeLocalizationContractInvalid, lastErr)
}

func planCommerceStoryboardInWorkflow(ctx workflow.Context, input CommerceStoryboardPlanningInput, snapshot CommerceStoryboardPlanningSnapshot, salesScript CommerceSalesScriptContract) (CommerceStoryboardDeterministicPlan, CommerceStoryboardPlanContract, CommerceStoryboardReviewContract, CommerceStoryboardProjection, []CommerceAgentProvenance, error) {
	deterministicPlan, err := BuildCommerceStoryboardDeterministicPlan(snapshot, salesScript)
	if err != nil {
		return CommerceStoryboardDeterministicPlan{}, CommerceStoryboardPlanContract{}, CommerceStoryboardReviewContract{}, CommerceStoryboardProjection{}, nil,
			commerceWorkflowError(CommerceCodeStoryboardContractInvalid, err)
	}
	agentSnapshot, err := buildCommerceStoryboardAgentSnapshot(snapshot, salesScript)
	if err != nil {
		return CommerceStoryboardDeterministicPlan{}, CommerceStoryboardPlanContract{}, CommerceStoryboardReviewContract{}, CommerceStoryboardProjection{}, nil,
			commerceWorkflowError(CommerceCodeStoryboardContractInvalid, err)
	}
	plannerSnapshot, plannerSalesScript, sourceAliases, err := aliasCommerceStoryboardPlannerInput(agentSnapshot, salesScript)
	if err != nil {
		return CommerceStoryboardDeterministicPlan{}, CommerceStoryboardPlanContract{}, CommerceStoryboardReviewContract{}, CommerceStoryboardProjection{}, nil,
			commerceWorkflowError(CommerceCodeStoryboardContractInvalid, err)
	}
	plannerSkeleton, err := aliasCommerceStoryboardCreativeSkeleton(deterministicPlan.Skeleton, sourceAliases)
	if err != nil {
		return CommerceStoryboardDeterministicPlan{}, CommerceStoryboardPlanContract{}, CommerceStoryboardReviewContract{}, CommerceStoryboardProjection{}, nil,
			commerceWorkflowError(CommerceCodeStoryboardContractInvalid, err)
	}
	limit := commerceReviewRounds(snapshot.Bindings.StoryboardPlanner)
	feedback := []CommerceReviewIssue{}
	calls := make([]CommerceAgentProvenance, 0, limit*2)
	var lastErr error
	for round := 1; round <= limit; round++ {
		var planCall CommerceAgentCallOutput
		if err := workflow.ExecuteActivity(ctx, PlanCommerceStoryboardActivityName, CommerceAgentCallInput{
			GenerationIdentity: &input.Identity, WorkflowRunID: input.WorkflowRunID, AttemptGeneration: input.AttemptGeneration,
			Phase: CommercePhaseStoryboard, Round: round, Binding: snapshot.Bindings.StoryboardPlanner,
			InputLanguage: snapshot.TargetLocale, OutputLanguage: snapshot.TargetLocale,
			Context: mustJSON(map[string]any{
				"snapshot": plannerSnapshot, "salesScript": plannerSalesScript,
				"salesBeatAuthority": agentSnapshot.SalesBeatAuthority,
				"segmentationPlan":   deterministicPlan.Segmentation,
				"frozenShotPlan":     plannerSkeleton,
				"reviewerIssues":     feedback,
			}), ReviewerIssues: feedback,
		}).Get(ctx, &planCall); err != nil {
			return CommerceStoryboardDeterministicPlan{}, CommerceStoryboardPlanContract{}, CommerceStoryboardReviewContract{}, CommerceStoryboardProjection{}, nil, err
		}
		calls = append(calls, planCall.Provenance)
		plan, err := ParseCommerceStoryboardPlan(planCall.RawOutput)
		var projection CommerceStoryboardProjection
		if err == nil {
			plan, err = resolveCommerceStoryboardSourceSegmentAliases(plan, sourceAliases)
		}
		if err == nil {
			plan, err = applyCommerceStoryboardCreativeDirection(deterministicPlan.Skeleton, plan)
		}
		if err == nil {
			plan, err = bindCommerceStoryboardPlanIdentity(snapshot, plan)
		}
		if err == nil {
			plan, err = reconcileCommerceStoryboardVoiceover(snapshot, plan)
		}
		if err == nil {
			plan, err = reconcileCommerceStoryboardSalesBeats(salesScript, plan)
		}
		if err == nil {
			projection, err = BuildCommerceStoryboardProjection(snapshot, plan)
		}
		if err != nil {
			lastErr = err
			feedback = commerceValidationFeedback(CommerceCodeStoryboardContractInvalid, "storyboardPlan", err)
			continue
		}
		var reviewCall CommerceAgentCallOutput
		if err := workflow.ExecuteActivity(ctx, ReviewCommerceStoryboardActivityName, CommerceAgentCallInput{
			GenerationIdentity: &input.Identity, WorkflowRunID: input.WorkflowRunID, AttemptGeneration: input.AttemptGeneration,
			Phase: CommercePhaseStoryboard, Round: round, Binding: snapshot.Bindings.StoryboardReviewer,
			InputLanguage: snapshot.TargetLocale, OutputLanguage: snapshot.TargetLocale,
			Context: mustJSON(map[string]any{
				"snapshot": agentSnapshot, "salesScript": salesScript,
				"salesBeatAuthority": agentSnapshot.SalesBeatAuthority,
				"segmentationPlan":   deterministicPlan.Segmentation,
				"candidate":          plan, "projection": projection,
			}),
		}).Get(ctx, &reviewCall); err != nil {
			return deterministicPlan, plan, BuildCommerceAdvisoryStoryboardReview(plan), projection, calls, nil
		}
		calls = append(calls, reviewCall.Provenance)
		review, err := ParseCommerceStoryboardReview(reviewCall.RawOutput)
		if err == nil {
			review = reconcileCommerceStoryboardReview(review, plan)
			err = ValidateCommerceStoryboardReview(review, plan)
		}
		if err != nil {
			return deterministicPlan, plan, BuildCommerceAdvisoryStoryboardReview(plan), projection, calls, nil
		}
		return deterministicPlan, plan, review, projection, calls, nil
	}
	return CommerceStoryboardDeterministicPlan{}, CommerceStoryboardPlanContract{}, CommerceStoryboardReviewContract{}, CommerceStoryboardProjection{}, calls, commerceWorkflowError(CommerceCodeStoryboardContractInvalid, lastErr)
}

func commerceAgentActivityOptions() workflow.ActivityOptions {
	return providerTextActivityOptions()
}

func validateCommercePreparationWorkflowInput(input CommerceScriptUnitPreparationInput) error {
	if err := ValidateCommerceScriptUnitPreparationIdentity(input.Identity); err != nil {
		return err
	}
	if strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.CreatedBy) == "" || input.AttemptGeneration <= 0 {
		return errors.New("workflowRunId, createdBy, and attemptGeneration are required")
	}
	return nil
}

func validateCommerceStoryboardWorkflowInput(input CommerceStoryboardPlanningInput) error {
	if err := ValidateCommerceUnitGenerationIdentity(input.Identity); err != nil {
		return err
	}
	if strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.CreatedBy) == "" || input.AttemptGeneration <= 0 {
		return errors.New("workflowRunId, createdBy, and attemptGeneration are required")
	}
	return nil
}

func validateCommerceLanguageResolutionState(snapshot CommerceScriptUnitPreparationSnapshot, state CommerceLanguageResolutionState) error {
	if strings.TrimSpace(state.ResolutionID) == "" || state.Revision <= 0 || state.InputHash != snapshot.InputHash {
		return errors.New("language resolution persistence identity is invalid")
	}
	if state.Status != "confirmed" && state.Status != "needs_confirmation" {
		return errors.New("language resolution status is invalid")
	}
	if err := ValidateCommerceLanguageResolution(state.Contract, snapshot); err != nil {
		return err
	}
	if state.Status == "confirmed" && state.Contract.NeedsUserConfirmation {
		return errors.New("confirmed language resolution still requires confirmation")
	}
	return nil
}

func commerceValidationFeedback(code, field string, err error) []CommerceReviewIssue {
	var contractIssue *commerceContractValidationIssue
	if errors.As(err, &contractIssue) {
		return []CommerceReviewIssue{contractIssue.reviewIssue(code)}
	}
	message := "contract validation failed"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = err.Error()
	}
	return []CommerceReviewIssue{{
		Code: code, Field: field, Message: message,
		Suggestion: "按冻结身份和结构化输出契约修正后重新生成",
	}}
}

func commerceWorkflowError(code string, err error) error {
	message := code
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = err.Error()
	}
	return temporal.NewNonRetryableApplicationError(message, code, err)
}

func (a CommerceActivities) LoadCommerceScriptUnitPreparation(ctx context.Context, input CommerceScriptUnitPreparationInput) (CommerceScriptUnitPreparationSnapshot, error) {
	if a.Ports == nil {
		return CommerceScriptUnitPreparationSnapshot{}, commerceActivityPortError()
	}
	snapshot, err := a.Ports.LoadScriptUnitPreparation(ctx, input)
	if err != nil {
		return CommerceScriptUnitPreparationSnapshot{}, commercePortError(err)
	}
	if err := ValidateCommercePreparationSnapshot(input.Identity, snapshot); err != nil {
		return CommerceScriptUnitPreparationSnapshot{}, temporal.NewNonRetryableApplicationError(err.Error(), CommerceCodeGenerationMismatch, err)
	}
	return snapshot, nil
}

func (a CommerceActivities) ExecuteCommerceProjectSetup(ctx context.Context, input CommerceProjectSetupInput) (CommerceProjectSetupOutput, error) {
	stopHeartbeat := startWorkflowActivityHeartbeat(ctx, map[string]any{
		"phase":          "commerce_project_setup",
		"projectId":      input.ProjectID,
		"setupSessionId": input.SetupSessionID,
	})
	defer stopHeartbeat()

	port := a.SetupPort
	if port == nil {
		if fallback, ok := a.Ports.(CommerceProjectSetupPort); ok {
			port = fallback
		}
	}
	if port == nil {
		return CommerceProjectSetupOutput{}, commerceActivityPortError()
	}
	output, err := port.ExecuteCommerceProjectSetup(ctx, input)
	if err != nil {
		return CommerceProjectSetupOutput{}, commercePortError(err)
	}
	return output, nil
}

func (a CommerceActivities) FailCommerceProjectSetup(ctx context.Context, input CommerceProjectSetupFailureInput) error {
	port := a.SetupPort
	if port == nil {
		if fallback, ok := a.Ports.(CommerceProjectSetupPort); ok {
			port = fallback
		}
	}
	if port == nil {
		return commerceActivityPortError()
	}
	if err := port.FailCommerceProjectSetup(ctx, input); err != nil {
		return commercePortError(err)
	}
	return nil
}

func (a CommerceActivities) ResolveCommerceLanguage(ctx context.Context, input CommerceAgentCallInput) (CommerceAgentCallOutput, error) {
	return a.runCommerceTextAgent(ctx, input)
}

func (a CommerceActivities) LocalizeCommerceScript(ctx context.Context, input CommerceAgentCallInput) (CommerceAgentCallOutput, error) {
	return a.runCommerceTextAgent(ctx, input)
}

func (a CommerceActivities) ReviewCommerceLocalization(ctx context.Context, input CommerceAgentCallInput) (CommerceAgentCallOutput, error) {
	return a.runCommerceTextAgent(ctx, input)
}

func (a CommerceActivities) OrganizeCommerceScript(ctx context.Context, input CommerceAgentCallInput) (CommerceAgentCallOutput, error) {
	return a.runCommerceTextAgent(ctx, input)
}

func (a CommerceActivities) PlanCommerceStoryboard(ctx context.Context, input CommerceAgentCallInput) (CommerceAgentCallOutput, error) {
	return a.runCommerceTextAgent(ctx, input)
}

func (a CommerceActivities) ReviewCommerceStoryboard(ctx context.Context, input CommerceAgentCallInput) (CommerceAgentCallOutput, error) {
	return a.runCommerceTextAgent(ctx, input)
}

func (a CommerceActivities) PersistCommerceLanguageResolution(ctx context.Context, input PersistCommerceLanguageResolutionInput) (CommerceLanguageResolutionState, error) {
	if a.Ports == nil {
		return CommerceLanguageResolutionState{}, commerceActivityPortError()
	}
	item, err := a.Ports.PersistLanguageResolution(ctx, input)
	if err != nil {
		return CommerceLanguageResolutionState{}, commercePortError(err)
	}
	return item, nil
}

func (a CommerceActivities) ConfirmCommerceLanguage(ctx context.Context, input ConfirmCommerceLanguageInput) (CommerceLanguageResolutionState, error) {
	if a.Ports == nil {
		return CommerceLanguageResolutionState{}, commerceActivityPortError()
	}
	item, err := a.Ports.ConfirmLanguage(ctx, input)
	if err != nil {
		return CommerceLanguageResolutionState{}, commercePortError(err)
	}
	return item, nil
}

func (a CommerceActivities) CommitCommerceScriptUnitPreparation(ctx context.Context, input CommerceScriptUnitPreparationCommit) (CommerceScriptUnitPreparationCommitResult, error) {
	if a.Ports == nil {
		return CommerceScriptUnitPreparationCommitResult{}, commerceActivityPortError()
	}
	item, err := a.Ports.CommitScriptUnitPreparation(ctx, input)
	if err != nil {
		return CommerceScriptUnitPreparationCommitResult{}, commercePortError(err)
	}
	return item, nil
}

func (a CommerceActivities) FailCommerceGenerationWorkflow(ctx context.Context, input CommerceGenerationWorkflowFailureInput) error {
	if a.Ports == nil {
		return commerceActivityPortError()
	}
	if err := a.Ports.FailCommerceGenerationWorkflow(ctx, input); err != nil {
		return commercePortError(err)
	}
	return nil
}

func (a CommerceActivities) LoadCommerceStoryboardPlanning(ctx context.Context, input CommerceStoryboardPlanningInput) (CommerceStoryboardPlanningSnapshot, error) {
	if a.Ports == nil {
		return CommerceStoryboardPlanningSnapshot{}, commerceActivityPortError()
	}
	snapshot, err := a.Ports.LoadStoryboardPlanning(ctx, input)
	if err != nil {
		return CommerceStoryboardPlanningSnapshot{}, commercePortError(err)
	}
	if err := ValidateCommerceStoryboardSnapshot(input.Identity, snapshot); err != nil {
		return CommerceStoryboardPlanningSnapshot{}, temporal.NewNonRetryableApplicationError(err.Error(), CommerceCodeGenerationMismatch, err)
	}
	return snapshot, nil
}

func (a CommerceActivities) CommitCommerceStoryboardPlan(ctx context.Context, input CommerceStoryboardPlanCommit) (CommerceStoryboardPlanCommitResult, error) {
	if a.Ports == nil {
		return CommerceStoryboardPlanCommitResult{}, commerceActivityPortError()
	}
	item, err := a.Ports.CommitStoryboardPlan(ctx, input)
	if err != nil {
		return CommerceStoryboardPlanCommitResult{}, commercePortError(err)
	}
	return item, nil
}

func (a CommerceActivities) runCommerceTextAgent(ctx context.Context, input CommerceAgentCallInput) (CommerceAgentCallOutput, error) {
	if a.Ports == nil {
		return CommerceAgentCallOutput{}, commerceActivityPortError()
	}
	executionIdentity, _, identityValue, err := commerceAgentIdentity(input)
	if err != nil {
		return CommerceAgentCallOutput{}, temporal.NewNonRetryableApplicationError(err.Error(), CommerceCodeWorkflowInputInvalid, err)
	}
	if err := ValidateCommerceAgentBinding(input.Binding); err != nil {
		return CommerceAgentCallOutput{}, temporal.NewNonRetryableApplicationError(err.Error(), CommerceCodeWorkflowInputInvalid, err)
	}
	if input.Round < 1 || input.Round > CommerceMaxAgentReviewRounds || len(input.Context) == 0 {
		return CommerceAgentCallOutput{}, temporal.NewNonRetryableApplicationError("commerce agent round or context is invalid", CommerceCodeWorkflowInputInvalid, nil)
	}
	if err := a.Ports.AssertCommerceWorkflowIdentity(ctx, input); err != nil {
		return CommerceAgentCallOutput{}, commercePortError(err)
	}
	if replayPort, ok := a.Ports.(CommerceAgentReplayPort); ok {
		if replay, found, err := replayPort.FindCommerceAgentReplay(ctx, input); err != nil {
			return CommerceAgentCallOutput{}, commercePortError(err)
		} else if found {
			return replay, nil
		}
	}
	if a.Core.gateway == nil || a.Core.db == nil {
		return CommerceAgentCallOutput{}, temporal.NewNonRetryableApplicationError("Provider Gateway 或工作流数据库未配置", provider.CodeProviderGatewayRequired, nil)
	}
	rendered, err := a.Core.renderWorkflowPromptVersion(ctx, executionIdentity.OrganizationID, executionIdentity.ProjectID, input.Binding.TemplateKey, input.Binding.PromptVersionID, map[string]any{
		"input": map[string]any{"context": string(input.Context)},
	})
	if err != nil {
		return CommerceAgentCallOutput{}, err
	}
	rendered = withCommerceOutputContract(rendered)
	nodeExecution, err := StartNodeRun(ctx, a.Core.db, NodeRunInput{
		OrganizationID: executionIdentity.OrganizationID, ProjectID: executionIdentity.ProjectID,
		WorkflowRunID: input.WorkflowRunID, NodeKey: commerceAgentNodeKey(input),
		NodeType: "agent.commerce." + input.Binding.Role, AttemptGeneration: input.AttemptGeneration,
		Input: mustJSON(map[string]any{
			"identity": identityValue, "phase": input.Phase, "round": input.Round,
			"role": input.Binding.Role, "promptVersionId": rendered.PromptVersionID,
			"promptHash": rendered.RenderedHash, "reviewerIssues": input.ReviewerIssues,
		}),
	})
	if err != nil {
		return CommerceAgentCallOutput{}, err
	}
	if nodeExecution.ProductionGenerationID != executionIdentity.ProjectGenerationID ||
		nodeExecution.VideoProductionBindingID != executionIdentity.VideoProductionBindingID ||
		nodeExecution.VideoProductionBindingRevision != executionIdentity.VideoProductionBindingRevision {
		_ = FailNodeRun(ctx, a.Core.db, nodeExecution, CommerceCodeGenerationMismatch, "工作流节点身份与带货视频生产代不一致")
		return CommerceAgentCallOutput{}, temporal.NewNonRetryableApplicationError("工作流节点身份与带货视频生产代不一致", CommerceCodeGenerationMismatch, nil)
	}
	idempotencyKey := commerceAgentIdempotencyKey(input)
	response, err := a.Core.generateProviderText(ctx, nodeExecution, provider.GatewayTextRequest{
		OrganizationID: executionIdentity.OrganizationID, ProjectID: executionIdentity.ProjectID,
		WorkflowRunID: input.WorkflowRunID, NodeRunID: nodeExecution.NodeRunID,
		ModelProfileKey: input.Binding.ModelProfileKey, ProviderModelID: input.Binding.ProviderModelID,
		PromptTemplateKey: rendered.TemplateKey, PromptVersionID: rendered.PromptVersionID,
		PromptHash: rendered.RenderedHash, PromptSource: rendered.Source,
		IdempotencyKey: idempotencyKey,
		Input:          mustJSON(map[string]any{"prompt": rendered.RenderedText, "responseFormat": "json", "maxOutputTokens": 16000}),
		References:     input.References,
		Options:        provider.GatewayTextOptions{TimeoutMS: providerTextGatewayTimeoutMS, IdempotencyKey: idempotencyKey},
	})
	if err != nil {
		workflowErr := workflowErrorFromProvider(err, codeActivityFailed)
		code, message := workflowErrorFields(workflowErr, codeActivityFailed)
		_ = FailNodeRun(ctx, a.Core.db, nodeExecution, code, message)
		return CommerceAgentCallOutput{}, temporal.NewApplicationError(message, code, err)
	}
	if err := a.Ports.AssertCommerceWorkflowIdentity(ctx, input); err != nil {
		_ = FailNodeRun(ctx, a.Core.db, nodeExecution, CommerceCodeGenerationMismatch, err.Error())
		return CommerceAgentCallOutput{}, commercePortError(err)
	}
	output := CommerceAgentCallOutput{
		RawOutput: response.Output.Text,
		Provenance: CommerceAgentProvenance{
			Role: input.Binding.Role, Round: input.Round, NodeRunID: nodeExecution.NodeRunID,
			ProviderRequestID: response.ProviderRequestID, ProviderCallID: response.ProviderCallID,
			ProviderModelID: response.ModelID, PromptTemplateKey: rendered.TemplateKey,
			PromptVersionID: rendered.PromptVersionID, PromptHash: rendered.RenderedHash,
		},
	}
	if err := CompleteNodeRun(ctx, a.Core.db, nodeExecution, mustJSON(output)); err != nil {
		return CommerceAgentCallOutput{}, err
	}
	return output, nil
}

func withCommerceOutputContract(rendered promptsvc.RenderedPrompt) promptsvc.RenderedPrompt {
	return promptsvc.WithOutputContract(rendered)
}

func commerceAgentNodeKey(input CommerceAgentCallInput) string {
	_, subjectID, _, _ := commerceAgentIdentity(input)
	return fmt.Sprintf("commerce_%s_%s_%s_round_%d", subjectID, commerceAgentSubjectKey(input.SubjectKey), input.Binding.Role, input.Round)
}

func commerceAgentIdempotencyKey(input CommerceAgentCallInput) string {
	_, subjectID, _, _ := commerceAgentIdentity(input)
	return fmt.Sprintf("commerce:%s:%s:%s:%s:g%d:r%d", input.WorkflowRunID, subjectID, commerceAgentSubjectKey(input.SubjectKey), input.Binding.Role, input.AttemptGeneration, input.Round)
}

func commerceAgentSubjectKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unit"
	}
	return strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(value)
}

func commerceAgentIdentity(input CommerceAgentCallInput) (commerce.ExecutionIdentity, string, any, error) {
	switch input.Phase {
	case CommercePhasePreparation:
		if input.PreparationIdentity == nil || input.GenerationIdentity != nil {
			return commerce.ExecutionIdentity{}, "", nil, errors.New("preparation agent requires exactly one preparation identity")
		}
		if err := ValidateCommerceScriptUnitPreparationIdentity(*input.PreparationIdentity); err != nil {
			return commerce.ExecutionIdentity{}, "", nil, err
		}
		return input.PreparationIdentity.ExecutionIdentity, input.PreparationIdentity.ScriptUnitID, *input.PreparationIdentity, nil
	case CommercePhaseScriptOrganization,
		CommercePhaseStoryboard,
		CommercePhaseImagePrompt,
		CommercePhaseImageFidelity,
		CommercePhaseVideoPrompt:
		if input.GenerationIdentity == nil || input.PreparationIdentity != nil {
			return commerce.ExecutionIdentity{}, "", nil, errors.New("generation agent requires exactly one unit generation identity")
		}
		if err := ValidateCommerceUnitGenerationIdentity(*input.GenerationIdentity); err != nil {
			return commerce.ExecutionIdentity{}, "", nil, err
		}
		return input.GenerationIdentity.ExecutionIdentity, input.GenerationIdentity.UnitGenerationID, *input.GenerationIdentity, nil
	default:
		return commerce.ExecutionIdentity{}, "", nil, errors.New("commerce agent phase is invalid")
	}
}

func commerceActivityPortError() error {
	return temporal.NewNonRetryableApplicationError("commerce workflow activity port is not configured", CommerceCodeActivityPortUnavailable, nil)
}

func commercePortError(err error) error {
	if err == nil {
		return nil
	}
	if item, ok := commerce.AsError(err); ok {
		if item.Retryable {
			return temporal.NewApplicationError(item.Error(), item.Code, item.Cause, item.Details)
		}
		return temporal.NewNonRetryableApplicationError(item.Error(), item.Code, item.Cause, item.Details)
	}
	return err
}
