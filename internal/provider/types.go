package provider

import (
	"encoding/json"
	"time"

	"github.com/Einzieg/cineweave/internal/videocontracts"
)

type Connector struct {
	ID           string          `json:"id"`
	ConnectorKey string          `json:"connectorKey"`
	Name         string          `json:"name"`
	Type         string          `json:"type"`
	IsOfficial   bool            `json:"isOfficial"`
	Manifest     json.RawMessage `json:"manifest"`
	Version      string          `json:"version"`
	CreatedAt    time.Time       `json:"createdAt"`
}

type ImportConnectorRequest struct {
	ConnectorKey string          `json:"connectorKey"`
	Name         string          `json:"name"`
	Type         string          `json:"type"`
	IsOfficial   bool            `json:"isOfficial"`
	Manifest     json.RawMessage `json:"manifest"`
	ManifestText string          `json:"manifestText"`
	Version      string          `json:"version"`
}

type CatalogEntry struct {
	ID                 string          `json:"id"`
	ProviderKey        string          `json:"providerKey"`
	Name               string          `json:"name"`
	DisplayName        string          `json:"displayName"`
	Description        *string         `json:"description,omitempty"`
	ProviderType       string          `json:"providerType"`
	Category           string          `json:"category"`
	LogoKey            *string         `json:"logoKey,omitempty"`
	DocsURL            *string         `json:"docsUrl,omitempty"`
	DefaultBaseURL     *string         `json:"defaultBaseUrl,omitempty"`
	DefaultAuthType    string          `json:"defaultAuthType"`
	ConnectorManifest  json.RawMessage `json:"connectorManifest"`
	ModelTemplates     json.RawMessage `json:"modelTemplates"`
	SupportedTaskTypes json.RawMessage `json:"supportedTaskTypes"`
	SetupSchema        json.RawMessage `json:"setupSchema"`
	Enabled            bool            `json:"enabled"`
	IsOfficial         bool            `json:"isOfficial"`
	InstalledCount     int             `json:"installedCount,omitempty"`
	CreatedAt          time.Time       `json:"createdAt"`
	UpdatedAt          time.Time       `json:"updatedAt"`
}

type CatalogModelTemplate struct {
	ModelKey              string          `json:"modelKey"`
	DisplayName           string          `json:"displayName"`
	Modality              string          `json:"modality"`
	TaskTypes             []string        `json:"taskTypes"`
	ExecutionMode         string          `json:"executionMode,omitempty"`
	SupportsJsonOutput    bool            `json:"supportsJsonOutput,omitempty"`
	SupportsToolCalls     bool            `json:"supportsToolCalls,omitempty"`
	SupportsReasoning     bool            `json:"supportsReasoning,omitempty"`
	InputLimits           json.RawMessage `json:"inputLimits,omitempty"`
	OutputLimits          json.RawMessage `json:"outputLimits,omitempty"`
	QualityTiers          json.RawMessage `json:"qualityTiers,omitempty"`
	ProviderOptionsSchema json.RawMessage `json:"providerOptionsSchema,omitempty"`
	PricingPolicy         json.RawMessage `json:"pricingPolicy,omitempty"`
}

type CatalogInstallModel struct {
	ModelKey              string          `json:"modelKey"`
	DisplayName           string          `json:"displayName"`
	Modality              string          `json:"modality"`
	TaskTypes             []string        `json:"taskTypes"`
	ExecutionMode         string          `json:"executionMode,omitempty"`
	SupportsJsonOutput    *bool           `json:"supportsJsonOutput,omitempty"`
	SupportsToolCalls     *bool           `json:"supportsToolCalls,omitempty"`
	SupportsReasoning     *bool           `json:"supportsReasoning,omitempty"`
	InputLimits           json.RawMessage `json:"inputLimits,omitempty"`
	OutputLimits          json.RawMessage `json:"outputLimits,omitempty"`
	QualityTiers          json.RawMessage `json:"qualityTiers,omitempty"`
	ProviderOptionsSchema json.RawMessage `json:"providerOptionsSchema,omitempty"`
	PricingPolicy         json.RawMessage `json:"pricingPolicy,omitempty"`
}

type CatalogInstallProfileBinding struct {
	ProfileKey string `json:"profileKey"`
	ModelKey   string `json:"modelKey"`
	Priority   *int   `json:"priority,omitempty"`
	Weight     *int   `json:"weight,omitempty"`
	Enabled    *bool  `json:"enabled,omitempty"`
}

type InstallCatalogRequest struct {
	OrganizationID string                         `json:"organizationId"`
	Name           string                         `json:"name"`
	BaseURL        string                         `json:"baseUrl"`
	APIKey         string                         `json:"apiKey"`
	AuthType       string                         `json:"authType"`
	Setup          map[string]any                 `json:"setup"`
	Config         json.RawMessage                `json:"config"`
	Models         []CatalogInstallModel          `json:"models"`
	BindProfiles   []CatalogInstallProfileBinding `json:"bindProfiles"`
}

type CatalogProfileBindingResult struct {
	ProfileID  string `json:"profileId"`
	ProfileKey string `json:"profileKey"`
	ModelID    string `json:"modelId"`
	BindingID  string `json:"bindingId"`
}

type InstallCatalogResponse struct {
	ProviderKey string                        `json:"providerKey"`
	Connector   Connector                     `json:"connector"`
	Account     Account                       `json:"account"`
	Models      []Model                       `json:"models"`
	Bindings    []CatalogProfileBindingResult `json:"bindings"`
}

type Account struct {
	ID                string          `json:"id"`
	OrganizationID    string          `json:"organizationId"`
	ConnectorID       string          `json:"connectorId"`
	ConnectorKey      string          `json:"connectorKey"`
	Name              string          `json:"name"`
	BaseURL           *string         `json:"baseUrl,omitempty"`
	AuthType          string          `json:"authType"`
	Status            string          `json:"status"`
	Config            json.RawMessage `json:"config"`
	CredentialPreview *string         `json:"credentialPreview,omitempty"`
	CredentialCount   int             `json:"credentialCount"`
	CreatedBy         string          `json:"createdBy"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
}

type Credential struct {
	ID                  string     `json:"id"`
	OrganizationID      string     `json:"organizationId"`
	ProviderAccountID   string     `json:"providerAccountId"`
	CredentialKey       string     `json:"credentialKey"`
	CredentialType      string     `json:"credentialType"`
	MaskedPreview       string     `json:"maskedPreview"`
	Status              string     `json:"status"`
	IsActive            bool       `json:"isActive"`
	AvailableModelCount int        `json:"availableModelCount"`
	LastDiscoveredAt    *time.Time `json:"lastDiscoveredAt,omitempty"`
	CreatedBy           *string    `json:"createdBy,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
	ExpiresAt           *time.Time `json:"expiresAt,omitempty"`
	RotatedAt           *time.Time `json:"rotatedAt,omitempty"`
}

type CreateAccountRequest struct {
	OrganizationID string          `json:"organizationId"`
	ConnectorKey   string          `json:"connectorKey"`
	Name           string          `json:"name"`
	BaseURL        string          `json:"baseUrl"`
	AuthType       string          `json:"authType"`
	Credential     map[string]any  `json:"credential"`
	Config         json.RawMessage `json:"config"`
}

type UpdateAccountRequest struct {
	Name     *string         `json:"name"`
	BaseURL  *string         `json:"baseUrl"`
	AuthType *string         `json:"authType"`
	Status   *string         `json:"status"`
	Config   json.RawMessage `json:"config"`
}

type RotateCredentialRequest struct {
	CredentialKey string         `json:"credentialKey"`
	Credential    map[string]any `json:"credential"`
}

type CreateCredentialRequest struct {
	CredentialKey  string         `json:"credentialKey"`
	CredentialType string         `json:"credentialType"`
	Credential     map[string]any `json:"credential"`
}

type Model struct {
	ID                string       `json:"id"`
	ProviderAccountID string       `json:"providerAccountId"`
	ModelKey          string       `json:"modelKey"`
	DisplayName       string       `json:"displayName"`
	Modality          string       `json:"modality"`
	Status            string       `json:"status"`
	Capabilities      []Capability `json:"capabilities"`
	CreatedAt         time.Time    `json:"createdAt"`
	UpdatedAt         time.Time    `json:"updatedAt"`
}

type AvailableModel struct {
	ID              string       `json:"id"`
	ModelKey        string       `json:"modelKey"`
	DisplayName     string       `json:"displayName"`
	Modality        string       `json:"modality"`
	Status          string       `json:"status"`
	ManagementScope string       `json:"managementScope"`
	Capabilities    []Capability `json:"capabilities"`
	CreatedAt       time.Time    `json:"createdAt"`
	UpdatedAt       time.Time    `json:"updatedAt"`
}

type Capability struct {
	ID                            string          `json:"id"`
	ProviderModelID               string          `json:"providerModelId"`
	TaskTypes                     json.RawMessage `json:"taskTypes"`
	InputLimits                   json.RawMessage `json:"inputLimits"`
	OutputLimits                  json.RawMessage `json:"outputLimits"`
	QualityTiers                  json.RawMessage `json:"qualityTiers"`
	ProviderOptionsSchema         json.RawMessage `json:"providerOptionsSchema"`
	PricingPolicy                 json.RawMessage `json:"pricingPolicy"`
	SupportedInputLanguages       []string        `json:"supportedInputLanguages"`
	SupportedOutputLanguages      []string        `json:"supportedOutputLanguages"`
	SupportedPromptLanguages      []string        `json:"supportedPromptLanguages"`
	SupportedNativeAudioLanguages []string        `json:"supportedNativeAudioLanguages"`
	Source                        string          `json:"source"`
	ApprovalStatus                string          `json:"approvalStatus"`
	CreatedAt                     time.Time       `json:"createdAt"`
}

type CapabilityInput struct {
	TaskTypes                     json.RawMessage `json:"taskTypes"`
	InputLimits                   json.RawMessage `json:"inputLimits"`
	OutputLimits                  json.RawMessage `json:"outputLimits"`
	QualityTiers                  json.RawMessage `json:"qualityTiers"`
	ProviderOptionsSchema         json.RawMessage `json:"providerOptionsSchema"`
	PricingPolicy                 json.RawMessage `json:"pricingPolicy"`
	SupportedInputLanguages       []string        `json:"supportedInputLanguages"`
	SupportedOutputLanguages      []string        `json:"supportedOutputLanguages"`
	SupportedPromptLanguages      []string        `json:"supportedPromptLanguages"`
	SupportedNativeAudioLanguages []string        `json:"supportedNativeAudioLanguages"`
	Source                        string          `json:"source"`
	ApprovalStatus                string          `json:"approvalStatus"`
}

type CreateModelRequest struct {
	ModelKey     string           `json:"modelKey"`
	DisplayName  string           `json:"displayName"`
	Modality     string           `json:"modality"`
	Status       string           `json:"status"`
	Capabilities *CapabilityInput `json:"capabilities"`
}

type UpdateModelRequest struct {
	ModelKey     *string          `json:"modelKey"`
	DisplayName  *string          `json:"displayName"`
	Modality     *string          `json:"modality"`
	Status       *string          `json:"status"`
	Capabilities *CapabilityInput `json:"capabilities"`
}

// UpdateAvailableModelRequest exposes only organization-safe model metadata.
// Provider identity, routing availability, and lifecycle remain platform-owned.
type UpdateAvailableModelRequest struct {
	DisplayName  *string          `json:"displayName"`
	Modality     *string          `json:"modality"`
	Capabilities *CapabilityInput `json:"capabilities"`
}

type VideoCapabilityAttestation struct {
	ID                      string          `json:"id"`
	OrganizationID          string          `json:"organizationId"`
	ProviderModelID         string          `json:"providerModelId"`
	VariantKey              string          `json:"variantKey"`
	CapabilitySnapshotHash  string          `json:"capabilitySnapshotHash"`
	VerificationStatus      string          `json:"verificationStatus"`
	EvidenceType            string          `json:"evidenceType"`
	Evidence                json.RawMessage `json:"evidence"`
	Decision                string          `json:"decision"`
	Reason                  string          `json:"reason"`
	DecidedBy               *string         `json:"decidedBy,omitempty"`
	DecidedAt               time.Time       `json:"decidedAt"`
	SupersedesAttestationID *string         `json:"supersedesAttestationId,omitempty"`
	RevokedBy               *string         `json:"revokedBy,omitempty"`
	RevokedAt               *time.Time      `json:"revokedAt,omitempty"`
	CurrentSnapshot         bool            `json:"currentSnapshot"`
	Active                  bool            `json:"active"`
	CreatedAt               time.Time       `json:"createdAt"`
}

type VideoCapabilityVariantStatus struct {
	VariantKey             string                      `json:"variantKey"`
	CapabilitySnapshotHash string                      `json:"capabilitySnapshotHash"`
	VerificationStatus     string                      `json:"verificationStatus"`
	Source                 string                      `json:"source,omitempty"`
	SourceURL              string                      `json:"sourceUrl,omitempty"`
	VerifiedAt             string                      `json:"verifiedAt,omitempty"`
	InitialInputContract   VideoInputContract          `json:"initialInputContract"`
	ContinuationContracts  []VideoInputContract        `json:"continuationInputContracts"`
	NativeAudio            VideoNativeAudioCapability  `json:"nativeAudio"`
	Duration               VideoDurationCapability     `json:"duration"`
	CurrentAttestation     *VideoCapabilityAttestation `json:"currentAttestation,omitempty"`
}

type VideoCapabilityAttestationList struct {
	Variants     []VideoCapabilityVariantStatus `json:"variants"`
	Attestations []VideoCapabilityAttestation   `json:"attestations"`
}

type CreateVideoCapabilityAttestationRequest struct {
	VariantKey             string          `json:"variantKey"`
	CapabilitySnapshotHash string          `json:"capabilitySnapshotHash"`
	Decision               string          `json:"decision"`
	Reason                 string          `json:"reason"`
	Evidence               json.RawMessage `json:"evidence,omitempty"`
}

type VerifyVideoCapabilityRequest struct {
	VariantKey             string `json:"variantKey"`
	CapabilitySnapshotHash string `json:"capabilitySnapshotHash"`
	VerificationMode       string `json:"verificationMode"`
	ProviderTestRunID      string `json:"providerTestRunId,omitempty"`
	Reason                 string `json:"reason,omitempty"`
}

type RevokeVideoCapabilityAttestationRequest struct {
	CapabilitySnapshotHash string `json:"capabilitySnapshotHash"`
	Reason                 string `json:"reason"`
}

type ModelProfile struct {
	ID               string                `json:"id"`
	OrganizationID   string                `json:"organizationId"`
	ProfileKey       string                `json:"profileKey"`
	Name             string                `json:"name"`
	Purpose          string                `json:"purpose"`
	RoutingStrategy  string                `json:"routingStrategy"`
	FallbackStrategy json.RawMessage       `json:"fallbackStrategy"`
	Bindings         []ModelProfileBinding `json:"bindings"`
	CreatedAt        time.Time             `json:"createdAt"`
	UpdatedAt        time.Time             `json:"updatedAt"`
}

type RoutingStrategy string

const (
	RoutingPriority             RoutingStrategy = "priority"
	RoutingPriorityWithFallback RoutingStrategy = "priority_with_fallback"
	RoutingWeighted             RoutingStrategy = "weighted"
	RoutingCostOptimized        RoutingStrategy = "cost_optimized"
	RoutingLatencyOptimized     RoutingStrategy = "latency_optimized"
)

type FallbackStrategy struct {
	Enabled     bool     `json:"enabled"`
	MaxAttempts int      `json:"maxAttempts"`
	FallbackOn  []string `json:"fallbackOn"`
	StopOn      []string `json:"stopOn"`
}

type RoutingRequest struct {
	OrganizationID                      string
	ModelProfileKey                     string
	TaskType                            string
	Modality                            string
	EstimatedInputTokens                int
	MaxOutputTokens                     int
	ImageSize                           string
	ImageQuality                        string
	VideoDurationSeconds                float64
	VideoResolution                     string
	InputLanguage                       string
	OutputLanguage                      string
	PromptLanguage                      string
	NativeAudioLanguage                 string
	RequireApprovedLanguageCapabilities bool
}

type RoutingCandidate struct {
	ModelProfileID        string
	ModelProfileKey       string
	ModelProfileBindingID string
	ProviderModelID       string
	ProviderAccountID     string
	Priority              int
	Weight                int
	ModelKey              string
	Modality              string
	Capabilities          []Capability
	RoutingStrategy       string
	FallbackStrategy      FallbackStrategy
	RuntimeOptions        ModelProfileBindingRuntimeOptions
	createdAt             time.Time
	averageLatencyMS      float64
	hasLatency            bool
	estimatedCost         float64
}

type ModelProfileBindingRuntimeOptions struct {
	ReasoningLevel string `json:"reasoningLevel,omitempty"`
}

type ModelProfileBinding struct {
	ID              string                            `json:"id"`
	ModelProfileID  string                            `json:"modelProfileId"`
	ProviderModelID string                            `json:"providerModelId"`
	Priority        int                               `json:"priority"`
	Weight          int                               `json:"weight"`
	Enabled         bool                              `json:"enabled"`
	RuntimeOptions  ModelProfileBindingRuntimeOptions `json:"runtimeOptions"`
	CreatedAt       time.Time                         `json:"createdAt"`
}

type CreateModelProfileRequest struct {
	ProfileKey       string          `json:"profileKey"`
	Name             string          `json:"name"`
	Purpose          string          `json:"purpose"`
	RoutingStrategy  string          `json:"routingStrategy"`
	FallbackStrategy json.RawMessage `json:"fallbackStrategy"`
}

type UpdateModelProfileRequest struct {
	ProfileKey       *string         `json:"profileKey"`
	Name             *string         `json:"name"`
	Purpose          *string         `json:"purpose"`
	RoutingStrategy  *string         `json:"routingStrategy"`
	FallbackStrategy json.RawMessage `json:"fallbackStrategy"`
}

type CreateModelProfileBindingRequest struct {
	ProviderModelID string                             `json:"providerModelId"`
	Priority        *int                               `json:"priority"`
	Weight          *int                               `json:"weight"`
	Enabled         *bool                              `json:"enabled"`
	RuntimeOptions  *ModelProfileBindingRuntimeOptions `json:"runtimeOptions"`
}

type UpdateModelProfileBindingRequest struct {
	Priority       *int                               `json:"priority"`
	Weight         *int                               `json:"weight"`
	Enabled        *bool                              `json:"enabled"`
	RuntimeOptions *ModelProfileBindingRuntimeOptions `json:"runtimeOptions"`
}

type ProviderTestResult struct {
	TestRunID        string           `json:"testRunId"`
	ProviderCallID   string           `json:"providerCallId"`
	Status           string           `json:"status"`
	LatencyMS        int              `json:"latencyMs"`
	ErrorCode        *string          `json:"errorCode,omitempty"`
	ErrorMessage     *string          `json:"errorMessage,omitempty"`
	NormalizedOutput json.RawMessage  `json:"normalizedOutput"`
	Attempts         []GatewayAttempt `json:"attempts,omitempty"`
}

type TestProviderModelRequest struct {
	TestType       string          `json:"testType"`
	Input          json.RawMessage `json:"input"`
	IdempotencyKey string          `json:"idempotencyKey,omitempty"`
}

type CallLog struct {
	ID                       string          `json:"id"`
	ProviderRequestID        *string         `json:"providerRequestId,omitempty"`
	AttemptGeneration        int             `json:"attemptGeneration"`
	AttemptSequence          int             `json:"attemptSequence"`
	OrganizationID           string          `json:"organizationId"`
	ProjectID                *string         `json:"projectId,omitempty"`
	ProductionGenerationID   *string         `json:"productionGenerationId,omitempty"`
	WorkflowRunID            *string         `json:"workflowRunId,omitempty"`
	NodeRunID                *string         `json:"nodeRunId,omitempty"`
	ProviderAccountID        string          `json:"providerAccountId"`
	ProviderModelID          *string         `json:"providerModelId,omitempty"`
	CredentialID             *string         `json:"credentialId,omitempty"`
	BillingContextID         *string         `json:"billingContextId,omitempty"`
	ProviderExternalLogID    *string         `json:"providerExternalLogId,omitempty"`
	ModelProfileID           *string         `json:"modelProfileId,omitempty"`
	ModelProfileBindingID    *string         `json:"modelProfileBindingId,omitempty"`
	ModelProfileKey          *string         `json:"modelProfileKey,omitempty"`
	TaskType                 string          `json:"taskType"`
	ExecutionMode            string          `json:"executionMode"`
	Status                   string          `json:"status"`
	LatencyMS                *int            `json:"latencyMs,omitempty"`
	InputTokens              *int            `json:"inputTokens,omitempty"`
	OutputTokens             *int            `json:"outputTokens,omitempty"`
	RequestedDurationSeconds *float64        `json:"requestedDurationSeconds,omitempty"`
	ActualDurationSeconds    *float64        `json:"actualDurationSeconds,omitempty"`
	MediaProbe               json.RawMessage `json:"mediaProbe,omitempty"`
	EstimatedCost            *string         `json:"estimatedCost,omitempty"`
	Currency                 *string         `json:"currency,omitempty"`
	ErrorCode                *string         `json:"errorCode,omitempty"`
	ErrorMessage             *string         `json:"errorMessage,omitempty"`
	UpstreamStatus           *int            `json:"upstreamStatus,omitempty"`
	UpstreamErrorCode        *string         `json:"upstreamErrorCode,omitempty"`
	RequestSnapshot          json.RawMessage `json:"requestSnapshot"`
	ResponseSnapshot         json.RawMessage `json:"responseSnapshot,omitempty"`
	NormalizedOutput         json.RawMessage `json:"normalizedOutput,omitempty"`
	ArtifactIDs              json.RawMessage `json:"artifactIds"`
	MediaFileIDs             json.RawMessage `json:"mediaFileIds"`
	CreatedAt                time.Time       `json:"createdAt"`
	StartedAt                *time.Time      `json:"startedAt,omitempty"`
	CompletedAt              *time.Time      `json:"completedAt,omitempty"`
}

type RecordCallRequest struct {
	ID                       string          `json:"id,omitempty"`
	ProviderRequestID        string          `json:"providerRequestId,omitempty"`
	AttemptGeneration        int             `json:"attemptGeneration,omitempty"`
	AttemptSequence          int             `json:"attemptSequence,omitempty"`
	OrganizationID           string          `json:"organizationId"`
	ProjectID                string          `json:"projectId"`
	ProductionGenerationID   string          `json:"productionGenerationId"`
	OperationID              string          `json:"operationId,omitempty"`
	OperationItemID          string          `json:"operationItemId,omitempty"`
	OperationItemAttempt     int             `json:"operationItemAttempt,omitempty"`
	ExecutionPlanID          string          `json:"executionPlanId,omitempty"`
	RenderSegmentID          string          `json:"renderSegmentId,omitempty"`
	WorkflowRunID            string          `json:"workflowRunId"`
	NodeRunID                string          `json:"nodeRunId"`
	ProviderAccountID        string          `json:"providerAccountId"`
	ProviderModelID          string          `json:"providerModelId"`
	CredentialID             string          `json:"credentialId"`
	BillingContextID         string          `json:"billingContextId,omitempty"`
	ProviderExternalLogID    string          `json:"providerExternalLogId,omitempty"`
	ModelProfileID           string          `json:"modelProfileId"`
	ModelProfileBindingID    string          `json:"modelProfileBindingId"`
	ModelProfileKey          string          `json:"modelProfileKey"`
	PromptVersionID          string          `json:"promptVersionId"`
	PromptHash               string          `json:"promptHash"`
	LeaseID                  string          `json:"leaseId"`
	IdempotencyKey           string          `json:"idempotencyKey"`
	TaskType                 string          `json:"taskType"`
	ExecutionMode            string          `json:"executionMode"`
	Status                   string          `json:"status"`
	LatencyMS                *int            `json:"latencyMs"`
	InputTokens              *int            `json:"inputTokens"`
	OutputTokens             *int            `json:"outputTokens"`
	RequestedDurationSeconds *float64        `json:"requestedDurationSeconds"`
	ActualDurationSeconds    *float64        `json:"actualDurationSeconds"`
	MediaProbe               json.RawMessage `json:"mediaProbe"`
	EstimatedCost            string          `json:"estimatedCost"`
	Currency                 string          `json:"currency"`
	ErrorCode                string          `json:"errorCode"`
	ErrorMessage             string          `json:"errorMessage"`
	UpstreamStatus           *int            `json:"upstreamStatus"`
	UpstreamErrorCode        string          `json:"upstreamErrorCode"`
	RequestSnapshot          json.RawMessage `json:"requestSnapshot"`
	ResponseSnapshot         json.RawMessage `json:"responseSnapshot"`
	NormalizedOutput         json.RawMessage `json:"normalizedOutput"`
	ArtifactIDs              json.RawMessage `json:"artifactIds"`
	MediaFileIDs             json.RawMessage `json:"mediaFileIds"`
}

type CallLogFilters struct {
	ProjectID string
	Status    string
	Limit     int
}

type UsageSummary struct {
	TotalCalls       int64  `json:"totalCalls"`
	FailedCalls      int64  `json:"failedCalls"`
	EstimatedCost    string `json:"estimatedCost"`
	EstimateCurrency string `json:"estimateCurrency"`
	Authoritative    bool   `json:"authoritative"`
	SourceSemantics  string `json:"sourceSemantics"`
}

type ProviderLimitPolicy struct {
	ID                     string    `json:"id"`
	OrganizationID         string    `json:"organizationId"`
	ProviderAccountID      *string   `json:"providerAccountId,omitempty"`
	ProviderModelID        *string   `json:"providerModelId,omitempty"`
	TaskType               string    `json:"taskType"`
	MaxConcurrency         *int      `json:"maxConcurrency,omitempty"`
	RequestsPerMinute      *int      `json:"requestsPerMinute,omitempty"`
	RequestsPerDay         *int      `json:"requestsPerDay,omitempty"`
	DailyBudget            *string   `json:"dailyBudget,omitempty"`
	MonthlyBudget          *string   `json:"monthlyBudget,omitempty"`
	Currency               string    `json:"currency"`
	FailureThreshold       *int      `json:"failureThreshold,omitempty"`
	FailureWindowSeconds   *int      `json:"failureWindowSeconds,omitempty"`
	CircuitCooldownSeconds *int      `json:"circuitCooldownSeconds,omitempty"`
	Enabled                bool      `json:"enabled"`
	CreatedBy              *string   `json:"createdBy,omitempty"`
	CreatedAt              time.Time `json:"createdAt"`
	UpdatedAt              time.Time `json:"updatedAt"`
}

type CreateProviderLimitPolicyRequest struct {
	OrganizationID         string  `json:"organizationId"`
	ProviderAccountID      *string `json:"providerAccountId"`
	ProviderModelID        *string `json:"providerModelId"`
	TaskType               string  `json:"taskType"`
	MaxConcurrency         *int    `json:"maxConcurrency"`
	RequestsPerMinute      *int    `json:"requestsPerMinute"`
	RequestsPerDay         *int    `json:"requestsPerDay"`
	DailyBudget            *string `json:"dailyBudget"`
	MonthlyBudget          *string `json:"monthlyBudget"`
	Currency               string  `json:"currency"`
	FailureThreshold       *int    `json:"failureThreshold"`
	FailureWindowSeconds   *int    `json:"failureWindowSeconds"`
	CircuitCooldownSeconds *int    `json:"circuitCooldownSeconds"`
	Enabled                *bool   `json:"enabled"`
}

type UpdateProviderLimitPolicyRequest struct {
	ProviderAccountID      *string `json:"providerAccountId"`
	ProviderModelID        *string `json:"providerModelId"`
	TaskType               *string `json:"taskType"`
	MaxConcurrency         *int    `json:"maxConcurrency"`
	RequestsPerMinute      *int    `json:"requestsPerMinute"`
	RequestsPerDay         *int    `json:"requestsPerDay"`
	DailyBudget            *string `json:"dailyBudget"`
	MonthlyBudget          *string `json:"monthlyBudget"`
	Currency               *string `json:"currency"`
	FailureThreshold       *int    `json:"failureThreshold"`
	FailureWindowSeconds   *int    `json:"failureWindowSeconds"`
	CircuitCooldownSeconds *int    `json:"circuitCooldownSeconds"`
	Enabled                *bool   `json:"enabled"`
}

type ProviderCircuitState struct {
	ID                string     `json:"id"`
	OrganizationID    string     `json:"organizationId"`
	ProviderAccountID string     `json:"providerAccountId"`
	ProviderModelID   *string    `json:"providerModelId,omitempty"`
	TaskType          string     `json:"taskType"`
	State             string     `json:"state"`
	FailureCount      int        `json:"failureCount"`
	SuccessCount      int        `json:"successCount"`
	OpenedAt          *time.Time `json:"openedAt,omitempty"`
	HalfOpenAt        *time.Time `json:"halfOpenAt,omitempty"`
	NextAttemptAt     *time.Time `json:"nextAttemptAt,omitempty"`
	LastErrorCode     *string    `json:"lastErrorCode,omitempty"`
	LastErrorMessage  *string    `json:"lastErrorMessage,omitempty"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type DiscoveredModel struct {
	ModelKey    string `json:"modelKey"`
	DisplayName string `json:"displayName"`
	Modality    string `json:"modality"`
	Status      string `json:"status"`
}

type ModelDiscoveryResult struct {
	CredentialID  string             `json:"credentialId,omitempty"`
	CredentialKey string             `json:"credentialKey,omitempty"`
	Models        []DiscoveredModel  `json:"models"`
	Unsupported   []any              `json:"unsupported"`
	Sync          ModelDiscoverySync `json:"sync"`
}

type ModelDiscoverySync struct {
	DiscoveredCount      int `json:"discoveredCount"`
	CreatedCount         int `json:"createdCount"`
	ExistingCount        int `json:"existingCount"`
	SkippedDisabledCount int `json:"skippedDisabledCount"`
	IgnoredCount         int `json:"ignoredCount"`
}

type GatewayTextOptions struct {
	TimeoutMS      int    `json:"timeoutMs"`
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	Retry          bool   `json:"retry,omitempty"`
}

type GatewayBillingIdentity struct {
	RequestedByUserID          string `json:"requestedByUserId,omitempty"`
	BillingContextID           string `json:"billingContextId,omitempty"`
	BillingContextRevision     int64  `json:"billingContextRevision,omitempty"`
	BillingContextSnapshotHash string `json:"billingContextSnapshotHash,omitempty"`
	BillingOperationPermission string `json:"billingOperationPermission,omitempty"`
	BillingContextReason       string `json:"billingContextReason,omitempty"`
}

const (
	BillingContextReasonWorkflowStart  = "workflow_start"
	BillingContextReasonAgentAction    = "agent_action"
	BillingContextReasonBatchStart     = "batch_start"
	BillingContextReasonManualProvider = "manual_provider_request"
	BillingContextReasonExplicitRetry  = "explicit_retry"
)

type GatewayTextRequest struct {
	GatewayBillingIdentity
	OrganizationID                      string                  `json:"organizationId"`
	WorkspaceID                         string                  `json:"workspaceId,omitempty"`
	ProjectID                           string                  `json:"projectId,omitempty"`
	WorkflowRunID                       string                  `json:"workflowRunId,omitempty"`
	NodeRunID                           string                  `json:"nodeRunId,omitempty"`
	ModelProfileKey                     string                  `json:"modelProfileKey,omitempty"`
	ProviderModelID                     string                  `json:"providerModelId,omitempty"`
	PromptTemplateKey                   string                  `json:"promptTemplateKey,omitempty"`
	PromptVersionID                     string                  `json:"promptVersionId,omitempty"`
	PromptHash                          string                  `json:"promptHash,omitempty"`
	PromptSource                        string                  `json:"promptSource,omitempty"`
	InputLanguage                       string                  `json:"inputLanguage,omitempty"`
	OutputLanguage                      string                  `json:"outputLanguage,omitempty"`
	RequireApprovedLanguageCapabilities bool                    `json:"requireApprovedLanguageCapabilities,omitempty"`
	IdempotencyKey                      string                  `json:"idempotencyKey,omitempty"`
	Input                               json.RawMessage         `json:"input"`
	References                          []GatewayImageReference `json:"references,omitempty"`
	Options                             GatewayTextOptions      `json:"options"`
}

type GatewayTextOutput struct {
	Text string          `json:"text"`
	Raw  json.RawMessage `json:"raw,omitempty"`
}

type GatewayUsage struct {
	InputTokens   int    `json:"inputTokens,omitempty"`
	OutputTokens  int    `json:"outputTokens,omitempty"`
	TotalTokens   int    `json:"totalTokens,omitempty"`
	EstimatedCost string `json:"estimatedCost"`
	Currency      string `json:"currency,omitempty"`
}

type GatewayAttempt struct {
	ProviderCallID        string `json:"providerCallId,omitempty"`
	ProviderModelID       string `json:"providerModelId,omitempty"`
	ProviderAccountID     string `json:"providerAccountId,omitempty"`
	ModelProfileBindingID string `json:"modelProfileBindingId,omitempty"`
	Status                string `json:"status"`
	ErrorCode             string `json:"errorCode,omitempty"`
	ErrorMessage          string `json:"errorMessage,omitempty"`
	Retryable             bool   `json:"retryable"`
	LatencyMS             int    `json:"latencyMs,omitempty"`
}

type GatewayTextResponse struct {
	SchemaVersion      int               `json:"schemaVersion,omitempty"`
	ProviderRequestID  string            `json:"providerRequestId"`
	AttemptGeneration  int               `json:"attemptGeneration"`
	AttemptSequence    int               `json:"attemptSequence,omitempty"`
	ProviderCallID     string            `json:"providerCallId"`
	ModelID            string            `json:"modelId"`
	Status             string            `json:"status"`
	Output             GatewayTextOutput `json:"output"`
	Usage              GatewayUsage      `json:"usage"`
	Error              *StandardError    `json:"error,omitempty"`
	LatencyMS          int               `json:"latencyMs,omitempty"`
	Attempts           []GatewayAttempt  `json:"attempts,omitempty"`
	requestDisposition string            `json:"-"`
}

type GatewayImageOptions struct {
	TimeoutMS      int    `json:"timeoutMs"`
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	Retry          bool   `json:"retry,omitempty"`
}

type GatewayImageReference struct {
	Type       string          `json:"type"`
	AssetID    string          `json:"assetId,omitempty"`
	ArtifactID string          `json:"artifactId,omitempty"`
	URL        string          `json:"url,omitempty"`
	StorageKey string          `json:"storageKey,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
}

type GatewayImageRequest struct {
	GatewayBillingIdentity
	OrganizationID                      string                  `json:"organizationId"`
	WorkspaceID                         string                  `json:"workspaceId,omitempty"`
	ProjectID                           string                  `json:"projectId,omitempty"`
	WorkflowRunID                       string                  `json:"workflowRunId,omitempty"`
	NodeRunID                           string                  `json:"nodeRunId,omitempty"`
	ModelProfileKey                     string                  `json:"modelProfileKey,omitempty"`
	ProviderModelID                     string                  `json:"providerModelId,omitempty"`
	PromptTemplateKey                   string                  `json:"promptTemplateKey,omitempty"`
	PromptVersionID                     string                  `json:"promptVersionId,omitempty"`
	PromptHash                          string                  `json:"promptHash,omitempty"`
	PromptSource                        string                  `json:"promptSource,omitempty"`
	PromptLanguage                      string                  `json:"promptLanguage,omitempty"`
	RequireApprovedLanguageCapabilities bool                    `json:"requireApprovedLanguageCapabilities,omitempty"`
	IdempotencyKey                      string                  `json:"idempotencyKey,omitempty"`
	Input                               json.RawMessage         `json:"input"`
	References                          []GatewayImageReference `json:"references,omitempty"`
	Options                             GatewayImageOptions     `json:"options"`
}

type GatewayImageOutput struct {
	ArtifactID  string          `json:"artifactId,omitempty"`
	MediaFileID string          `json:"mediaFileId,omitempty"`
	StorageKey  string          `json:"storageKey,omitempty"`
	URL         string          `json:"url,omitempty"`
	MimeType    string          `json:"mimeType,omitempty"`
	Width       *int            `json:"width,omitempty"`
	Height      *int            `json:"height,omitempty"`
	AspectRatio string          `json:"aspectRatio,omitempty"`
	Raw         json.RawMessage `json:"raw,omitempty"`
}

type GatewayImageResponse struct {
	ProviderRequestID string             `json:"providerRequestId"`
	AttemptGeneration int                `json:"attemptGeneration"`
	ProviderCallID    string             `json:"providerCallId"`
	ModelID           string             `json:"modelId"`
	Status            string             `json:"status"`
	Output            GatewayImageOutput `json:"output"`
	Usage             GatewayUsage       `json:"usage"`
	Error             *StandardError     `json:"error,omitempty"`
	LatencyMS         int                `json:"latencyMs,omitempty"`
	Attempts          []GatewayAttempt   `json:"attempts,omitempty"`
}

type GatewayAudioOptions struct {
	TimeoutMS      int    `json:"timeoutMs"`
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	Retry          bool   `json:"retry,omitempty"`
}

type GatewayTTSRequest struct {
	GatewayBillingIdentity
	OrganizationID   string              `json:"organizationId"`
	WorkspaceID      string              `json:"workspaceId,omitempty"`
	ProjectID        string              `json:"projectId,omitempty"`
	WorkflowRunID    string              `json:"workflowRunId,omitempty"`
	NodeRunID        string              `json:"nodeRunId,omitempty"`
	ModelProfileKey  string              `json:"modelProfileKey,omitempty"`
	ProviderModelID  string              `json:"providerModelId,omitempty"`
	IdempotencyKey   string              `json:"idempotencyKey,omitempty"`
	TimelineTimebase int64               `json:"timelineTimebase,omitempty"`
	Input            json.RawMessage     `json:"input"`
	Options          GatewayAudioOptions `json:"options"`
}

type GatewayAudioOutput struct {
	ArtifactID  string          `json:"artifactId,omitempty"`
	MediaFileID string          `json:"mediaFileId,omitempty"`
	StorageKey  string          `json:"storageKey,omitempty"`
	MimeType    string          `json:"mimeType,omitempty"`
	ByteSize    int64           `json:"byteSize,omitempty"`
	ContentHash string          `json:"contentHash,omitempty"`
	Raw         json.RawMessage `json:"raw,omitempty"`
}

type GatewayTTSResponse struct {
	ProviderRequestID string             `json:"providerRequestId"`
	AttemptGeneration int                `json:"attemptGeneration"`
	ProviderCallID    string             `json:"providerCallId"`
	ModelID           string             `json:"modelId"`
	Status            string             `json:"status"`
	Output            GatewayAudioOutput `json:"output"`
	Usage             GatewayUsage       `json:"usage"`
	Error             *StandardError     `json:"error,omitempty"`
	LatencyMS         int                `json:"latencyMs,omitempty"`
	Attempts          []GatewayAttempt   `json:"attempts,omitempty"`
}

type GatewayAudioSource struct {
	ArtifactID  string `json:"artifactId,omitempty"`
	MediaFileID string `json:"mediaFileId,omitempty"`
	StorageKey  string `json:"storageKey,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
	FileName    string `json:"fileName,omitempty"`
}

type GatewayASRRequest struct {
	GatewayBillingIdentity
	OrganizationID  string              `json:"organizationId"`
	WorkspaceID     string              `json:"workspaceId,omitempty"`
	ProjectID       string              `json:"projectId,omitempty"`
	WorkflowRunID   string              `json:"workflowRunId,omitempty"`
	NodeRunID       string              `json:"nodeRunId,omitempty"`
	ModelProfileKey string              `json:"modelProfileKey,omitempty"`
	ProviderModelID string              `json:"providerModelId,omitempty"`
	IdempotencyKey  string              `json:"idempotencyKey,omitempty"`
	Source          GatewayAudioSource  `json:"source"`
	Input           json.RawMessage     `json:"input"`
	Options         GatewayAudioOptions `json:"options"`
}

type GatewayASRWord struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

type GatewayASRSegment struct {
	ID      int              `json:"id,omitempty"`
	Speaker string           `json:"speaker,omitempty"`
	Text    string           `json:"text"`
	Start   float64          `json:"start"`
	End     float64          `json:"end"`
	Words   []GatewayASRWord `json:"words,omitempty"`
}

type GatewayASROutput struct {
	Text     string              `json:"text"`
	Language string              `json:"language,omitempty"`
	Duration float64             `json:"duration,omitempty"`
	Segments []GatewayASRSegment `json:"segments,omitempty"`
	Words    []GatewayASRWord    `json:"words,omitempty"`
	Raw      json.RawMessage     `json:"raw,omitempty"`
}

type GatewayASRResponse struct {
	ProviderRequestID string           `json:"providerRequestId"`
	AttemptGeneration int              `json:"attemptGeneration"`
	ProviderCallID    string           `json:"providerCallId"`
	ModelID           string           `json:"modelId"`
	Status            string           `json:"status"`
	Output            GatewayASROutput `json:"output"`
	Usage             GatewayUsage     `json:"usage"`
	Error             *StandardError   `json:"error,omitempty"`
	LatencyMS         int              `json:"latencyMs,omitempty"`
	Attempts          []GatewayAttempt `json:"attempts,omitempty"`
}

type GatewayVideoOptions struct {
	TimeoutMS      int    `json:"timeoutMs"`
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	MaxPolls       int    `json:"maxPolls,omitempty"`
	Retry          bool   `json:"retry,omitempty"`
}

type GatewayVideoReference struct {
	ReferenceKey  string          `json:"referenceKey,omitempty"`
	Role          string          `json:"role,omitempty"`
	Required      bool            `json:"required,omitempty"`
	Priority      int             `json:"priority,omitempty"`
	Type          string          `json:"type"`
	Semantics     string          `json:"semantics,omitempty"`
	SourceType    string          `json:"sourceType,omitempty"`
	SourceID      string          `json:"sourceId,omitempty"`
	SourceVersion string          `json:"sourceVersion,omitempty"`
	AssetID       string          `json:"assetId,omitempty"`
	ArtifactID    string          `json:"artifactId,omitempty"`
	MediaFileID   string          `json:"mediaFileId,omitempty"`
	ContentHash   string          `json:"contentHash,omitempty"`
	GeneratedAt   string          `json:"generatedAt,omitempty"`
	URL           string          `json:"url,omitempty"`
	StorageKey    string          `json:"storageKey,omitempty"`
	MimeType      string          `json:"mimeType,omitempty"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
}

type GatewayVideoCreateTaskRequest struct {
	GatewayBillingIdentity
	OrganizationID                 string                              `json:"organizationId"`
	WorkspaceID                    string                              `json:"workspaceId,omitempty"`
	ProjectID                      string                              `json:"projectId,omitempty"`
	OperationID                    string                              `json:"operationId,omitempty"`
	OperationItemID                string                              `json:"operationItemId,omitempty"`
	OperationItemAttempt           int                                 `json:"operationItemAttempt,omitempty"`
	CommerceDirectVideoJobID       string                              `json:"commerceDirectVideoJobId,omitempty"`
	StoryboardShotID               string                              `json:"storyboardShotId,omitempty"`
	ProductionGenerationID         string                              `json:"productionGenerationId,omitempty"`
	VideoProductionBindingID       string                              `json:"videoProductionBindingId,omitempty"`
	VideoProductionBindingRevision int64                               `json:"videoProductionBindingRevision,omitempty"`
	ProductionProfileVersionID     string                              `json:"productionProfileVersionId,omitempty"`
	ProductionProfileSnapshotHash  string                              `json:"productionProfileSnapshotHash,omitempty"`
	InputContractKey               string                              `json:"inputContractKey,omitempty"`
	InputContractHash              string                              `json:"inputContractHash,omitempty"`
	InputContractVersion           string                              `json:"inputContractVersion,omitempty"`
	ShotStateRevision              int                                 `json:"shotStateRevision,omitempty"`
	ShotStateHash                  string                              `json:"shotStateHash,omitempty"`
	TransitionHash                 string                              `json:"transitionHash,omitempty"`
	ReferencePackID                string                              `json:"referencePackId,omitempty"`
	ReferencePackHash              string                              `json:"referencePackHash,omitempty"`
	PromptContextPlanID            string                              `json:"promptContextPlanId,omitempty"`
	PromptContextPlanHash          string                              `json:"promptContextPlanHash,omitempty"`
	VideoPromptPlanID              string                              `json:"videoPromptPlanId,omitempty"`
	NativeAudioRequired            bool                                `json:"nativeAudioRequired,omitempty"`
	DialogueCues                   []GatewayVideoDialogueSpan          `json:"dialogueCues,omitempty"`
	WorkflowRunID                  string                              `json:"workflowRunId,omitempty"`
	NodeRunID                      string                              `json:"nodeRunId,omitempty"`
	NodeExecutionToken             string                              `json:"nodeExecutionToken,omitempty"`
	NodeAttemptGeneration          int                                 `json:"nodeAttemptGeneration,omitempty"`
	ModelProfileKey                string                              `json:"modelProfileKey,omitempty"`
	ProviderModelID                string                              `json:"providerModelId,omitempty"`
	PromptTemplateKey              string                              `json:"promptTemplateKey,omitempty"`
	PromptVersionID                string                              `json:"promptVersionId,omitempty"`
	PromptHash                     string                              `json:"promptHash,omitempty"`
	PromptSource                   string                              `json:"promptSource,omitempty"`
	IdempotencyKey                 string                              `json:"idempotencyKey,omitempty"`
	ExecutionPlanID                string                              `json:"executionPlanId,omitempty"`
	RenderSegmentID                string                              `json:"renderSegmentId,omitempty"`
	CapabilitySnapshotHash         string                              `json:"capabilitySnapshotHash,omitempty"`
	ReferenceManifest              *videocontracts.ReferenceManifestV2 `json:"referenceManifest,omitempty"`
	ReferenceManifestHash          string                              `json:"referenceManifestHash,omitempty"`
	Input                          json.RawMessage                     `json:"input"`
	References                     []GatewayVideoReference             `json:"references,omitempty"`
	Options                        GatewayVideoOptions                 `json:"options"`
}

type GatewayVideoCreateTaskResponse struct {
	ProviderRequestID   string           `json:"providerRequestId"`
	AttemptGeneration   int              `json:"attemptGeneration"`
	ProviderCallID      string           `json:"providerCallId"`
	ProviderAsyncTaskID string           `json:"providerAsyncTaskId"`
	ExternalTaskID      string           `json:"externalTaskId,omitempty"`
	ModelID             string           `json:"modelId"`
	ExecutionPlanID     string           `json:"executionPlanId,omitempty"`
	RenderSegmentID     string           `json:"renderSegmentId,omitempty"`
	Status              string           `json:"status"`
	Error               *StandardError   `json:"error,omitempty"`
	LatencyMS           int              `json:"latencyMs,omitempty"`
	Attempts            []GatewayAttempt `json:"attempts,omitempty"`
}

type GatewayVideoPollTaskRequest struct {
	OrganizationID                 string              `json:"organizationId"`
	ProviderAsyncTaskID            string              `json:"providerAsyncTaskId,omitempty"`
	ExternalTaskID                 string              `json:"externalTaskId,omitempty"`
	ProviderModelID                string              `json:"providerModelId,omitempty"`
	ProviderAccountID              string              `json:"providerAccountId,omitempty"`
	ProjectID                      string              `json:"projectId,omitempty"`
	ProductionGenerationID         string              `json:"productionGenerationId,omitempty"`
	VideoProductionBindingID       string              `json:"videoProductionBindingId,omitempty"`
	VideoProductionBindingRevision int64               `json:"videoProductionBindingRevision,omitempty"`
	WorkflowRunID                  string              `json:"workflowRunId,omitempty"`
	NodeRunID                      string              `json:"nodeRunId,omitempty"`
	NodeExecutionToken             string              `json:"nodeExecutionToken,omitempty"`
	NodeAttemptGeneration          int                 `json:"nodeAttemptGeneration,omitempty"`
	Options                        GatewayVideoOptions `json:"options"`
}

type GatewayVideoOutputWarning struct {
	Code                string `json:"code"`
	Message             string `json:"message"`
	Category            string `json:"category"`
	ExpectedAspectRatio string `json:"expectedAspectRatio,omitempty"`
	ActualAspectRatio   string `json:"actualAspectRatio,omitempty"`
	RequestedSize       string `json:"requestedSize,omitempty"`
	ProviderSize        string `json:"providerSize,omitempty"`
	Width               int    `json:"width,omitempty"`
	Height              int    `json:"height,omitempty"`
}

type GatewayVideoMediaProbe struct {
	Status               string                      `json:"status"`
	Error                string                      `json:"error,omitempty"`
	DurationSeconds      float64                     `json:"durationSeconds,omitempty"`
	Width                int                         `json:"width,omitempty"`
	Height               int                         `json:"height,omitempty"`
	FrameRateNumerator   int64                       `json:"frameRateNumerator,omitempty"`
	FrameRateDenominator int64                       `json:"frameRateDenominator,omitempty"`
	FrameRate            float64                     `json:"frameRate,omitempty"`
	FrameCount           int64                       `json:"frameCount,omitempty"`
	FrameCountEstimated  bool                        `json:"frameCountEstimated"`
	VideoStreamCount     int                         `json:"videoStreamCount"`
	AudioStreamCount     int                         `json:"audioStreamCount"`
	HasAudio             bool                        `json:"hasAudio"`
	VideoCodec           string                      `json:"videoCodec,omitempty"`
	AudioCodecs          []string                    `json:"audioCodecs,omitempty"`
	AudioSampleRate      int                         `json:"audioSampleRate,omitempty"`
	AudioSampleCount     int64                       `json:"audioSampleCount,omitempty"`
	AudioSampleEstimated bool                        `json:"audioSampleCountEstimated"`
	AudioChannelCount    int                         `json:"audioChannelCount,omitempty"`
	Warnings             []GatewayVideoOutputWarning `json:"warnings,omitempty"`
}

type GatewayVideoOutput struct {
	ArtifactID               string                      `json:"artifactId,omitempty"`
	MediaFileID              string                      `json:"mediaFileId,omitempty"`
	StorageKey               string                      `json:"storageKey,omitempty"`
	URL                      string                      `json:"url,omitempty"`
	MimeType                 string                      `json:"mimeType,omitempty"`
	ByteSize                 *int64                      `json:"byteSize,omitempty"`
	DurationSeconds          *float64                    `json:"durationSeconds,omitempty"`
	RequestedDurationSeconds *float64                    `json:"requestedDurationSeconds,omitempty"`
	ProviderDurationSeconds  *float64                    `json:"providerDurationSeconds,omitempty"`
	ActualDurationSeconds    *float64                    `json:"actualDurationSeconds,omitempty"`
	DurationSource           string                      `json:"durationSource,omitempty"`
	Width                    *int                        `json:"width,omitempty"`
	Height                   *int                        `json:"height,omitempty"`
	MediaProbe               *GatewayVideoMediaProbe     `json:"mediaProbe,omitempty"`
	Warnings                 []GatewayVideoOutputWarning `json:"warnings,omitempty"`
	Raw                      json.RawMessage             `json:"raw,omitempty"`
}

type GatewayVideoPollTaskResponse struct {
	ProviderRequestID   string             `json:"providerRequestId"`
	AttemptGeneration   int                `json:"attemptGeneration"`
	ProviderCallID      string             `json:"providerCallId"`
	ProviderAsyncTaskID string             `json:"providerAsyncTaskId"`
	ExternalTaskID      string             `json:"externalTaskId,omitempty"`
	ModelID             string             `json:"modelId,omitempty"`
	Status              string             `json:"status"`
	ExecutionPlanID     string             `json:"executionPlanId,omitempty"`
	RenderSegmentID     string             `json:"renderSegmentId,omitempty"`
	Output              GatewayVideoOutput `json:"output"`
	Usage               GatewayUsage       `json:"usage"`
	Error               *StandardError     `json:"error,omitempty"`
	LatencyMS           int                `json:"latencyMs,omitempty"`
}

type GatewayVideoCancelTaskRequest struct {
	OrganizationID      string              `json:"organizationId"`
	ProviderAsyncTaskID string              `json:"providerAsyncTaskId,omitempty"`
	ExternalTaskID      string              `json:"externalTaskId,omitempty"`
	ProviderModelID     string              `json:"providerModelId,omitempty"`
	ProviderAccountID   string              `json:"providerAccountId,omitempty"`
	IdempotencyKey      string              `json:"idempotencyKey,omitempty"`
	Options             GatewayVideoOptions `json:"options"`
}

type GatewayVideoCancelTaskResponse struct {
	ProviderRequestID   string         `json:"providerRequestId,omitempty"`
	AttemptGeneration   int            `json:"attemptGeneration,omitempty"`
	ProviderCallID      string         `json:"providerCallId,omitempty"`
	ProviderAsyncTaskID string         `json:"providerAsyncTaskId,omitempty"`
	ExternalTaskID      string         `json:"externalTaskId,omitempty"`
	Status              string         `json:"status"`
	ExecutionPlanID     string         `json:"executionPlanId,omitempty"`
	RenderSegmentID     string         `json:"renderSegmentId,omitempty"`
	Error               *StandardError `json:"error,omitempty"`
}

const GatewayTextStreamSchemaVersion = 2

const (
	GatewayTextEventAttemptStarted = "provider.attempt.started"
	GatewayTextEventDelta          = "provider.delta"
	GatewayTextEventAttemptFailed  = "provider.attempt.failed"
	GatewayTextEventCompleted      = "provider.completed"
	GatewayTextEventFailed         = "provider.failed"
	GatewayTextEventReplayed       = "provider.replayed"
)

type GatewayTextDelta struct {
	SchemaVersion     int     `json:"schemaVersion"`
	ProviderRequestID string  `json:"providerRequestId"`
	ProviderCallID    string  `json:"providerCallId"`
	AttemptGeneration int     `json:"attemptGeneration"`
	AttemptSequence   int     `json:"attemptSequence"`
	Sequence          int64   `json:"sequence"`
	Text              string  `json:"text"`
	FinishReason      *string `json:"finishReason"`
}

type GatewayTextAttemptEvent struct {
	SchemaVersion     int            `json:"schemaVersion"`
	ProviderRequestID string         `json:"providerRequestId"`
	ProviderCallID    string         `json:"providerCallId"`
	AttemptGeneration int            `json:"attemptGeneration"`
	AttemptSequence   int            `json:"attemptSequence"`
	ProviderModelID   string         `json:"providerModelId,omitempty"`
	Status            string         `json:"status"`
	Error             *StandardError `json:"error,omitempty"`
}

type GatewayTextReplayEvent struct {
	SchemaVersion     int               `json:"schemaVersion"`
	ProviderRequestID string            `json:"providerRequestId"`
	ProviderCallID    string            `json:"providerCallId"`
	AttemptGeneration int               `json:"attemptGeneration"`
	AttemptSequence   int               `json:"attemptSequence"`
	ModelID           string            `json:"modelId"`
	Status            string            `json:"status"`
	Output            GatewayTextOutput `json:"output"`
	Usage             GatewayUsage      `json:"usage"`
	LatencyMS         int               `json:"latencyMs,omitempty"`
}

type GatewayTextFailureEvent struct {
	SchemaVersion     int            `json:"schemaVersion"`
	ProviderRequestID string         `json:"providerRequestId,omitempty"`
	ProviderCallID    string         `json:"providerCallId,omitempty"`
	AttemptGeneration int            `json:"attemptGeneration,omitempty"`
	AttemptSequence   int            `json:"attemptSequence,omitempty"`
	Error             *StandardError `json:"error"`
}

type GatewayTextStreamEvent struct {
	Type     string
	Attempt  *GatewayTextAttemptEvent
	Delta    *GatewayTextDelta
	Response *GatewayTextResponse
	Replay   *GatewayTextReplayEvent
	Failure  *GatewayTextFailureEvent
}

func (event GatewayTextStreamEvent) Payload() any {
	switch event.Type {
	case GatewayTextEventAttemptStarted, GatewayTextEventAttemptFailed:
		return event.Attempt
	case GatewayTextEventDelta:
		return event.Delta
	case GatewayTextEventCompleted:
		return event.Response
	case GatewayTextEventReplayed:
		return event.Replay
	case GatewayTextEventFailed:
		return event.Failure
	default:
		return nil
	}
}

type GatewayDiscoverModelsRequest struct {
	OrganizationID string `json:"organizationId"`
	AccountID      string `json:"accountId"`
	CredentialID   string `json:"credentialId,omitempty"`
	TestType       string `json:"testType,omitempty"`
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	Retry          bool   `json:"retry,omitempty"`
}

type GatewayDiscoverModelsResponse struct {
	ProviderRequestID string            `json:"providerRequestId,omitempty"`
	AttemptGeneration int               `json:"attemptGeneration,omitempty"`
	ProviderCallID    string            `json:"providerCallId,omitempty"`
	Status            string            `json:"status"`
	Models            []DiscoveredModel `json:"models"`
	Unsupported       []any             `json:"unsupported"`
	Error             *StandardError    `json:"error,omitempty"`
	LatencyMS         int               `json:"latencyMs,omitempty"`
}

type GatewayManifestTestRunRequest struct {
	OrganizationID string                 `json:"organizationId"`
	UserID         string                 `json:"userId"`
	Request        ManifestTestRunRequest `json:"request"`
}
