package commerce

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/Einzieg/cineweave/internal/videoproduction"
)

var (
	ErrWorkflowTemplateUnavailable = errors.New("commerce workflow template unavailable")
	ErrProjectNotConfigured        = errors.New("commerce project is not configured")
)

const (
	CodeProjectKindMismatch    = "PROJECT_KIND_MISMATCH"
	CodeBindingMismatch        = "COMMERCE_BINDING_MISMATCH"
	CodeGenerationMismatch     = "COMMERCE_SCRIPT_UNIT_GENERATION_MISMATCH"
	CodeRevisionConflict       = "COMMERCE_REVISION_CONFLICT"
	CodeProjectNotConfigured   = "COMMERCE_PROJECT_NOT_CONFIGURED"
	CodeProjectLocked          = "COMMERCE_PROJECT_LOCKED"
	CodeProjectRebuildBlocked  = "COMMERCE_PROJECT_REBUILD_BLOCKED"
	CodeSetupIncomplete        = "COMMERCE_SETUP_INCOMPLETE"
	CodeSetupRevisionConflict  = "COMMERCE_SETUP_REVISION_CONFLICT"
	CodeSetupAbandoned         = "COMMERCE_SETUP_ALREADY_ABANDONED"
	CodeProductRequired        = "COMMERCE_PRODUCT_REQUIRED"
	CodeProductVersionStale    = "COMMERCE_PRODUCT_VERSION_STALE"
	CodeProductReconfigure     = "COMMERCE_PRODUCT_RECONFIGURATION_REQUIRED"
	CodeProductPrimaryImage    = "COMMERCE_PRODUCT_PRIMARY_IMAGE_REQUIRED"
	CodeScriptUnitRequired     = "COMMERCE_SCRIPT_UNIT_REQUIRED"
	CodeScriptUnitArchived     = "COMMERCE_SCRIPT_UNIT_ARCHIVED"
	CodeScriptUnitRevision     = "COMMERCE_SCRIPT_UNIT_REVISION_CONFLICT"
	CodeScriptRequired         = "COMMERCE_SCRIPT_REQUIRED"
	CodeLanguageRequired       = "COMMERCE_LANGUAGE_REQUIRED"
	CodeLanguageUnsupported    = "COMMERCE_LANGUAGE_UNSUPPORTED"
	CodeLanguageConfirmation   = "COMMERCE_LANGUAGE_CONFIRMATION_REQUIRED"
	CodeDurationExceeded       = "COMMERCE_SCRIPT_DURATION_EXCEEDED"
	CodeScriptVersionStale     = "COMMERCE_SCRIPT_VERSION_STALE"
	CodeScriptRebuildRequired  = "COMMERCE_SCRIPT_UNIT_REBUILD_REQUIRED"
	CodeScriptRebuildStale     = "COMMERCE_SCRIPT_UNIT_REBUILD_STALE"
	CodeScriptRebuildBlocked   = "COMMERCE_SCRIPT_UNIT_REBUILD_BLOCKED"
	CodeScriptOrganization     = "COMMERCE_SCRIPT_ORGANIZATION_INVALID"
	CodeScriptOrganizationBusy = "COMMERCE_SCRIPT_ORGANIZATION_IN_PROGRESS"
	CodeScriptOrganizationNeed = "COMMERCE_SCRIPT_ORGANIZATION_REQUIRED"
	CodeStoryboardPlanRequired = "COMMERCE_STORYBOARD_PLAN_REQUIRED"
	CodeStoryboardPlanStale    = "COMMERCE_STORYBOARD_PLAN_STALE"
	CodeStoryboardShotRequired = "COMMERCE_STORYBOARD_SHOT_REQUIRED"
	CodeStoryboardRevision     = "COMMERCE_STORYBOARD_REVISION_CONFLICT"
	CodeStoryboardInvalid      = "COMMERCE_STORYBOARD_INVALID"
	CodeImagePromptRequired    = "COMMERCE_IMAGE_PROMPT_REQUIRED"
)

type Error struct {
	Code      string
	Message   string
	Retryable bool
	Details   map[string]any
	Cause     error
}

func (e Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func (e Error) Unwrap() error { return e.Cause }

func AsError(err error) (Error, bool) {
	var typed Error
	if errors.As(err, &typed) {
		return typed, true
	}
	return Error{}, false
}

type WorkflowTemplateVersion struct {
	ID                      string
	TemplateID              string
	TemplateKey             string
	Version                 int
	ContentHash             string
	ConfigurationSnapshot   json.RawMessage
	PromptBindings          json.RawMessage
	AgentModelContracts     json.RawMessage
	LanguageContract        json.RawMessage
	ImageCapabilityContract json.RawMessage
	VideoCapabilityContract json.RawMessage
	VideoProfileKey         string
	VideoProfileVersion     int
}

type DraftProjectParams struct {
	OrganizationID      string
	WorkspaceID         string
	Name                string
	Description         *string
	AspectRatio         *string
	VideoRatio          string
	AudioStrategy       string
	AudioRequirement    string
	ImageQuality        string
	TimelineTimebase    int64
	FPSNumerator        int
	FPSDenominator      int
	Settings            json.RawMessage
	CreatedBy           string
	IdempotencyScope    string
	ClientRequestID     string
	RequestHash         string
	InputSnapshot       json.RawMessage
	SetupExpiresAt      time.Time
	WorkflowTemplateKey string
}

type DraftProjectResult struct {
	ProjectID                 string
	SetupSessionID            string
	SetupState                string
	WorkflowTemplateVersionID string
	SetupConfigurationHash    string
}

type InitialBindingParams struct {
	OrganizationID          string
	ProjectID               string
	WorkflowTemplateVersion string
	SourceGenerationID      string
	RebuildID               string
	CreatedBy               string
	CompatibilityPolicy     string
	VideoOverrides          json.RawMessage
	ProductionConfiguration videoproduction.ProductionConfigurationSnapshot
	ConfigurationSnapshot   json.RawMessage
	ModelRoutingSnapshot    json.RawMessage
	CapabilitySnapshot      json.RawMessage
}

type BindingPreparationState struct {
	ProjectRevision    int64
	ProductionState    string
	ProductionLocked   bool
	ActiveGenerationID *string
}

type InitialBindingResult struct {
	VideoBindingID            string
	VideoBindingRevision      int64
	VideoProfileVersionID     string
	VideoProfileSnapshot      json.RawMessage
	VideoProfileSnapshotHash  string
	CommerceBindingID         string
	CommerceBindingRevision   int64
	CommerceConfigurationHash string
	ProjectGenerationID       string
	ProjectGenerationNo       int64
}

type VideoBindingIdentity struct {
	ID                  string          `json:"id"`
	Revision            int64           `json:"revision"`
	Status              string          `json:"status"`
	ProfileVersionID    string          `json:"profileVersionId"`
	ProfileSnapshotHash string          `json:"profileSnapshotHash"`
	ProfileSnapshot     json.RawMessage `json:"profileSnapshot"`
}

type WorkflowBindingIdentity struct {
	ID                       string          `json:"id"`
	Revision                 int64           `json:"revision"`
	Status                   string          `json:"status"`
	TemplateVersionID        string          `json:"templateVersionId"`
	VideoBindingID           string          `json:"videoProductionBindingId"`
	VideoProfileSnapshotHash string          `json:"videoProfileSnapshotHash"`
	ConfigurationHash        string          `json:"configurationHash"`
	ConfigurationSnapshot    json.RawMessage `json:"configurationSnapshot"`
	ModelRoutingSnapshot     json.RawMessage `json:"modelRoutingSnapshot"`
	CapabilitySnapshot       json.RawMessage `json:"capabilitySnapshot"`
}

type ProjectGenerationIdentity struct {
	ID                string `json:"id"`
	GenerationNo      int64  `json:"generationNo"`
	Status            string `json:"status"`
	VideoBindingID    string `json:"videoProductionBindingId"`
	CommerceBindingID string `json:"commerceWorkflowBindingId"`
}

type ProductionContext struct {
	OrganizationID  string                    `json:"organizationId"`
	ProjectID       string                    `json:"projectId"`
	ProjectRevision int64                     `json:"projectRevision"`
	ProjectState    string                    `json:"projectState"`
	ProjectLocked   bool                      `json:"projectLocked"`
	Generation      ProjectGenerationIdentity `json:"generation"`
	VideoBinding    VideoBindingIdentity      `json:"videoBinding"`
	CommerceBinding WorkflowBindingIdentity   `json:"commerceBinding"`
}

type ExecutionIdentity struct {
	OrganizationID                  string `json:"organizationId"`
	ProjectID                       string `json:"projectId"`
	ProjectGenerationID             string `json:"projectGenerationId"`
	VideoProductionBindingID        string `json:"videoProductionBindingId"`
	VideoProductionBindingRevision  int64  `json:"videoProductionBindingRevision"`
	VideoProfileSnapshotHash        string `json:"videoProfileSnapshotHash"`
	CommerceWorkflowBindingID       string `json:"commerceWorkflowBindingId"`
	CommerceWorkflowBindingRevision int64  `json:"commerceWorkflowBindingRevision"`
	CommerceConfigurationHash       string `json:"commerceConfigurationHash"`
}

func (context ProductionContext) ExecutionIdentity() ExecutionIdentity {
	return ExecutionIdentity{
		OrganizationID:                  context.OrganizationID,
		ProjectID:                       context.ProjectID,
		ProjectGenerationID:             context.Generation.ID,
		VideoProductionBindingID:        context.VideoBinding.ID,
		VideoProductionBindingRevision:  context.VideoBinding.Revision,
		VideoProfileSnapshotHash:        context.VideoBinding.ProfileSnapshotHash,
		CommerceWorkflowBindingID:       context.CommerceBinding.ID,
		CommerceWorkflowBindingRevision: context.CommerceBinding.Revision,
		CommerceConfigurationHash:       context.CommerceBinding.ConfigurationHash,
	}
}

type UnitGenerationIdentity struct {
	ExecutionIdentity
	ProductID             string `json:"productId"`
	ScriptUnitID          string `json:"scriptUnitId"`
	ScriptUnitRevision    int64  `json:"scriptUnitRevision"`
	UnitGenerationID      string `json:"scriptUnitGenerationId"`
	UnitGenerationNo      int64  `json:"scriptUnitGenerationNo"`
	UnitConfigurationHash string `json:"unitConfigurationHash"`
}

// ScriptUnitPreparationIdentity freezes every mutable input needed to create a
// unit generation. It deliberately does not contain a UnitGeneration ID:
// localization approval and generation creation are one atomic commit at the
// end of the preparation workflow.
type ScriptUnitPreparationIdentity struct {
	ExecutionIdentity
	ProductID               string `json:"productId"`
	ProductVersionID        string `json:"productVersionId"`
	ProductFactsHash        string `json:"productFactsHash"`
	ScriptUnitID            string `json:"scriptUnitId"`
	ScriptUnitRevision      int64  `json:"scriptUnitRevision"`
	SourceScriptVersionID   string `json:"sourceScriptVersionId"`
	SourceScriptContentHash string `json:"sourceScriptContentHash"`
	ReferencePackID         string `json:"referencePackId"`
	ReferencePackHash       string `json:"referencePackHash"`
	RebuildID               string `json:"rebuildId,omitempty"`
	SourceUnitGenerationID  string `json:"sourceUnitGenerationId,omitempty"`
	TargetConfigurationHash string `json:"targetConfigurationHash,omitempty"`
}

type UnitGenerationContext struct {
	Identity              UnitGenerationIdentity `json:"identity"`
	Status                string                 `json:"status"`
	ProductVersionID      string                 `json:"productVersionId"`
	SourceScriptVersionID string                 `json:"sourceScriptVersionId"`
	LocalizationID        string                 `json:"localizationId"`
	ReferencePackID       string                 `json:"referencePackId"`
	ConfigurationSnapshot json.RawMessage        `json:"configurationSnapshot"`
}

type ProjectRebuildContext struct {
	ID                              string
	OrganizationID                  string
	ProjectID                       string
	Status                          string
	ProjectRevision                 int64
	ProjectState                    string
	ProjectLocked                   bool
	ActiveRebuildID                 string
	ActiveProjectGenerationID       string
	ExpectedProjectRevision         int64
	SourceVideoBindingID            string
	SourceProjectGenerationID       string
	SourceCommerceBindingID         string
	SourceCommerceConfigurationHash string
	TargetProfileVersionID          string
	TargetConfiguration             json.RawMessage
	TargetConfigurationHash         string
	TargetVideoBindingID            string
	TargetProjectGenerationID       string
	TargetCommerceBindingID         string
	TargetCommerceConfigurationHash string
	TargetPrepared                  *InitialBindingResult
	PreparedUnitCount               int
}

type ProjectRebuildUnitSeed struct {
	OrganizationID         string
	ProjectID              string
	ProductID              string
	ScriptUnitID           string
	ScriptUnitRevision     int64
	SourceUnitGenerationID string
	SourceUnitGenerationNo int64
	ProductVersionID       string
	SourceScriptVersionID  string
	LocalizationID         string
	ReferencePackID        string
	ConfigurationSnapshot  json.RawMessage
	ConfigurationHash      string
}

type ProjectRebuildUnitTarget struct {
	ProjectRebuildUnitSeed
	TargetUnitGenerationID  string
	TargetUnitGenerationNo  int64
	TargetConfiguration     json.RawMessage
	TargetConfigurationHash string
}

type PreparedProjectRebuild struct {
	RebuildID         string
	Source            ProductionContext
	Target            InitialBindingResult
	PreparedUnitCount int
}

type ProjectRebuildActivationResult struct {
	RebuildID         string
	ProjectRevision   int64
	ProjectGeneration ProjectGenerationIdentity
	VideoBinding      VideoBindingIdentity
	CommerceBinding   WorkflowBindingIdentity
	SwitchedUnitCount int
}
