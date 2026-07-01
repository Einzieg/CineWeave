package provider

import (
	"encoding/json"
	"time"
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
	CreatedBy         string          `json:"createdBy"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
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

type Capability struct {
	ID                    string          `json:"id"`
	ProviderModelID       string          `json:"providerModelId"`
	TaskTypes             json.RawMessage `json:"taskTypes"`
	InputLimits           json.RawMessage `json:"inputLimits"`
	OutputLimits          json.RawMessage `json:"outputLimits"`
	QualityTiers          json.RawMessage `json:"qualityTiers"`
	ProviderOptionsSchema json.RawMessage `json:"providerOptionsSchema"`
	PricingPolicy         json.RawMessage `json:"pricingPolicy"`
	CreatedAt             time.Time       `json:"createdAt"`
}

type CapabilityInput struct {
	TaskTypes             json.RawMessage `json:"taskTypes"`
	InputLimits           json.RawMessage `json:"inputLimits"`
	OutputLimits          json.RawMessage `json:"outputLimits"`
	QualityTiers          json.RawMessage `json:"qualityTiers"`
	ProviderOptionsSchema json.RawMessage `json:"providerOptionsSchema"`
	PricingPolicy         json.RawMessage `json:"pricingPolicy"`
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
	OrganizationID       string
	ModelProfileKey      string
	TaskType             string
	Modality             string
	EstimatedInputTokens int
	MaxOutputTokens      int
	ImageSize            string
	ImageQuality         string
	VideoDurationSeconds float64
	VideoResolution      string
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
	createdAt             time.Time
	averageLatencyMS      float64
	hasLatency            bool
	estimatedCost         float64
}

type ModelProfileBinding struct {
	ID              string    `json:"id"`
	ModelProfileID  string    `json:"modelProfileId"`
	ProviderModelID string    `json:"providerModelId"`
	Priority        int       `json:"priority"`
	Weight          int       `json:"weight"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"createdAt"`
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
	ProviderModelID string `json:"providerModelId"`
	Priority        *int   `json:"priority"`
	Weight          *int   `json:"weight"`
	Enabled         *bool  `json:"enabled"`
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
	ID                    string          `json:"id"`
	OrganizationID        string          `json:"organizationId"`
	ProjectID             *string         `json:"projectId,omitempty"`
	WorkflowRunID         *string         `json:"workflowRunId,omitempty"`
	NodeRunID             *string         `json:"nodeRunId,omitempty"`
	ProviderAccountID     string          `json:"providerAccountId"`
	ProviderModelID       *string         `json:"providerModelId,omitempty"`
	CredentialID          *string         `json:"credentialId,omitempty"`
	ModelProfileID        *string         `json:"modelProfileId,omitempty"`
	ModelProfileBindingID *string         `json:"modelProfileBindingId,omitempty"`
	ModelProfileKey       *string         `json:"modelProfileKey,omitempty"`
	TaskType              string          `json:"taskType"`
	ExecutionMode         string          `json:"executionMode"`
	Status                string          `json:"status"`
	LatencyMS             *int            `json:"latencyMs,omitempty"`
	InputTokens           *int            `json:"inputTokens,omitempty"`
	OutputTokens          *int            `json:"outputTokens,omitempty"`
	EstimatedCost         *string         `json:"estimatedCost,omitempty"`
	Currency              *string         `json:"currency,omitempty"`
	ErrorCode             *string         `json:"errorCode,omitempty"`
	ErrorMessage          *string         `json:"errorMessage,omitempty"`
	UpstreamStatus        *int            `json:"upstreamStatus,omitempty"`
	UpstreamErrorCode     *string         `json:"upstreamErrorCode,omitempty"`
	RequestSnapshot       json.RawMessage `json:"requestSnapshot"`
	ResponseSnapshot      json.RawMessage `json:"responseSnapshot,omitempty"`
	NormalizedOutput      json.RawMessage `json:"normalizedOutput,omitempty"`
	ArtifactIDs           json.RawMessage `json:"artifactIds"`
	MediaFileIDs          json.RawMessage `json:"mediaFileIds"`
	CreatedAt             time.Time       `json:"createdAt"`
	StartedAt             *time.Time      `json:"startedAt,omitempty"`
	CompletedAt           *time.Time      `json:"completedAt,omitempty"`
}

type RecordCallRequest struct {
	ID                    string          `json:"id,omitempty"`
	OrganizationID        string          `json:"organizationId"`
	ProjectID             string          `json:"projectId"`
	WorkflowRunID         string          `json:"workflowRunId"`
	NodeRunID             string          `json:"nodeRunId"`
	ProviderAccountID     string          `json:"providerAccountId"`
	ProviderModelID       string          `json:"providerModelId"`
	CredentialID          string          `json:"credentialId"`
	ModelProfileID        string          `json:"modelProfileId"`
	ModelProfileBindingID string          `json:"modelProfileBindingId"`
	ModelProfileKey       string          `json:"modelProfileKey"`
	PromptVersionID       string          `json:"promptVersionId"`
	PromptHash            string          `json:"promptHash"`
	LeaseID               string          `json:"leaseId"`
	IdempotencyKey        string          `json:"idempotencyKey"`
	TaskType              string          `json:"taskType"`
	ExecutionMode         string          `json:"executionMode"`
	Status                string          `json:"status"`
	LatencyMS             *int            `json:"latencyMs"`
	InputTokens           *int            `json:"inputTokens"`
	OutputTokens          *int            `json:"outputTokens"`
	EstimatedCost         string          `json:"estimatedCost"`
	Currency              string          `json:"currency"`
	ErrorCode             string          `json:"errorCode"`
	ErrorMessage          string          `json:"errorMessage"`
	UpstreamStatus        *int            `json:"upstreamStatus"`
	UpstreamErrorCode     string          `json:"upstreamErrorCode"`
	RequestSnapshot       json.RawMessage `json:"requestSnapshot"`
	ResponseSnapshot      json.RawMessage `json:"responseSnapshot"`
	NormalizedOutput      json.RawMessage `json:"normalizedOutput"`
	ArtifactIDs           json.RawMessage `json:"artifactIds"`
	MediaFileIDs          json.RawMessage `json:"mediaFileIds"`
}

type CallLogFilters struct {
	ProjectID string
	Status    string
	Limit     int
}

type UsageSummary struct {
	TotalCalls  int64  `json:"totalCalls"`
	FailedCalls int64  `json:"failedCalls"`
	TotalCost   string `json:"totalCost"`
	Currency    string `json:"currency"`
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
	Models      []DiscoveredModel `json:"models"`
	Unsupported []any             `json:"unsupported"`
}

type GatewayTextOptions struct {
	TimeoutMS      int    `json:"timeoutMs"`
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
}

type GatewayTextRequest struct {
	OrganizationID    string             `json:"organizationId"`
	WorkspaceID       string             `json:"workspaceId,omitempty"`
	ProjectID         string             `json:"projectId,omitempty"`
	WorkflowRunID     string             `json:"workflowRunId,omitempty"`
	NodeRunID         string             `json:"nodeRunId,omitempty"`
	ModelProfileKey   string             `json:"modelProfileKey,omitempty"`
	ProviderModelID   string             `json:"providerModelId,omitempty"`
	PromptTemplateKey string             `json:"promptTemplateKey,omitempty"`
	PromptVersionID   string             `json:"promptVersionId,omitempty"`
	PromptHash        string             `json:"promptHash,omitempty"`
	PromptSource      string             `json:"promptSource,omitempty"`
	IdempotencyKey    string             `json:"idempotencyKey,omitempty"`
	Input             json.RawMessage    `json:"input"`
	Options           GatewayTextOptions `json:"options"`
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
	ProviderCallID string            `json:"providerCallId"`
	ModelID        string            `json:"modelId"`
	Status         string            `json:"status"`
	Output         GatewayTextOutput `json:"output"`
	Usage          GatewayUsage      `json:"usage"`
	Error          *StandardError    `json:"error,omitempty"`
	LatencyMS      int               `json:"latencyMs,omitempty"`
	Attempts       []GatewayAttempt  `json:"attempts,omitempty"`
}

type GatewayImageOptions struct {
	TimeoutMS      int    `json:"timeoutMs"`
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
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
	OrganizationID    string                  `json:"organizationId"`
	WorkspaceID       string                  `json:"workspaceId,omitempty"`
	ProjectID         string                  `json:"projectId,omitempty"`
	WorkflowRunID     string                  `json:"workflowRunId,omitempty"`
	NodeRunID         string                  `json:"nodeRunId,omitempty"`
	ModelProfileKey   string                  `json:"modelProfileKey,omitempty"`
	ProviderModelID   string                  `json:"providerModelId,omitempty"`
	PromptTemplateKey string                  `json:"promptTemplateKey,omitempty"`
	PromptVersionID   string                  `json:"promptVersionId,omitempty"`
	PromptHash        string                  `json:"promptHash,omitempty"`
	PromptSource      string                  `json:"promptSource,omitempty"`
	IdempotencyKey    string                  `json:"idempotencyKey,omitempty"`
	Input             json.RawMessage         `json:"input"`
	References        []GatewayImageReference `json:"references,omitempty"`
	Options           GatewayImageOptions     `json:"options"`
}

type GatewayImageOutput struct {
	ArtifactID  string          `json:"artifactId,omitempty"`
	MediaFileID string          `json:"mediaFileId,omitempty"`
	StorageKey  string          `json:"storageKey,omitempty"`
	URL         string          `json:"url,omitempty"`
	MimeType    string          `json:"mimeType,omitempty"`
	Width       *int            `json:"width,omitempty"`
	Height      *int            `json:"height,omitempty"`
	Raw         json.RawMessage `json:"raw,omitempty"`
}

type GatewayImageResponse struct {
	ProviderCallID string             `json:"providerCallId"`
	ModelID        string             `json:"modelId"`
	Status         string             `json:"status"`
	Output         GatewayImageOutput `json:"output"`
	Usage          GatewayUsage       `json:"usage"`
	Error          *StandardError     `json:"error,omitempty"`
	LatencyMS      int                `json:"latencyMs,omitempty"`
	Attempts       []GatewayAttempt   `json:"attempts,omitempty"`
}

type GatewayVideoOptions struct {
	TimeoutMS      int    `json:"timeoutMs"`
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	MaxPolls       int    `json:"maxPolls,omitempty"`
}

type GatewayVideoReference struct {
	Type        string          `json:"type"`
	AssetID     string          `json:"assetId,omitempty"`
	ArtifactID  string          `json:"artifactId,omitempty"`
	MediaFileID string          `json:"mediaFileId,omitempty"`
	URL         string          `json:"url,omitempty"`
	StorageKey  string          `json:"storageKey,omitempty"`
	MimeType    string          `json:"mimeType,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
}

type GatewayVideoCreateTaskRequest struct {
	OrganizationID    string                  `json:"organizationId"`
	WorkspaceID       string                  `json:"workspaceId,omitempty"`
	ProjectID         string                  `json:"projectId,omitempty"`
	WorkflowRunID     string                  `json:"workflowRunId,omitempty"`
	NodeRunID         string                  `json:"nodeRunId,omitempty"`
	ModelProfileKey   string                  `json:"modelProfileKey,omitempty"`
	ProviderModelID   string                  `json:"providerModelId,omitempty"`
	PromptTemplateKey string                  `json:"promptTemplateKey,omitempty"`
	PromptVersionID   string                  `json:"promptVersionId,omitempty"`
	PromptHash        string                  `json:"promptHash,omitempty"`
	PromptSource      string                  `json:"promptSource,omitempty"`
	IdempotencyKey    string                  `json:"idempotencyKey,omitempty"`
	Input             json.RawMessage         `json:"input"`
	References        []GatewayVideoReference `json:"references,omitempty"`
	Options           GatewayVideoOptions     `json:"options"`
}

type GatewayVideoCreateTaskResponse struct {
	ProviderCallID      string           `json:"providerCallId"`
	ProviderAsyncTaskID string           `json:"providerAsyncTaskId"`
	ExternalTaskID      string           `json:"externalTaskId,omitempty"`
	ModelID             string           `json:"modelId"`
	Status              string           `json:"status"`
	Error               *StandardError   `json:"error,omitempty"`
	LatencyMS           int              `json:"latencyMs,omitempty"`
	Attempts            []GatewayAttempt `json:"attempts,omitempty"`
}

type GatewayVideoPollTaskRequest struct {
	OrganizationID      string              `json:"organizationId"`
	ProviderAsyncTaskID string              `json:"providerAsyncTaskId,omitempty"`
	ExternalTaskID      string              `json:"externalTaskId,omitempty"`
	ProviderModelID     string              `json:"providerModelId,omitempty"`
	ProviderAccountID   string              `json:"providerAccountId,omitempty"`
	ProjectID           string              `json:"projectId,omitempty"`
	WorkflowRunID       string              `json:"workflowRunId,omitempty"`
	NodeRunID           string              `json:"nodeRunId,omitempty"`
	Options             GatewayVideoOptions `json:"options"`
}

type GatewayVideoOutput struct {
	ArtifactID      string          `json:"artifactId,omitempty"`
	MediaFileID     string          `json:"mediaFileId,omitempty"`
	StorageKey      string          `json:"storageKey,omitempty"`
	URL             string          `json:"url,omitempty"`
	MimeType        string          `json:"mimeType,omitempty"`
	ByteSize        *int64          `json:"byteSize,omitempty"`
	DurationSeconds *float64        `json:"durationSeconds,omitempty"`
	Width           *int            `json:"width,omitempty"`
	Height          *int            `json:"height,omitempty"`
	Raw             json.RawMessage `json:"raw,omitempty"`
}

type GatewayVideoPollTaskResponse struct {
	ProviderCallID      string             `json:"providerCallId"`
	ProviderAsyncTaskID string             `json:"providerAsyncTaskId"`
	ExternalTaskID      string             `json:"externalTaskId,omitempty"`
	ModelID             string             `json:"modelId,omitempty"`
	Status              string             `json:"status"`
	Output              GatewayVideoOutput `json:"output"`
	Usage               GatewayUsage       `json:"usage"`
	Error               *StandardError     `json:"error,omitempty"`
	LatencyMS           int                `json:"latencyMs,omitempty"`
}

type GatewayVideoCancelTaskRequest struct {
	OrganizationID      string `json:"organizationId"`
	ProviderAsyncTaskID string `json:"providerAsyncTaskId,omitempty"`
	ExternalTaskID      string `json:"externalTaskId,omitempty"`
	ProviderModelID     string `json:"providerModelId,omitempty"`
	ProviderAccountID   string `json:"providerAccountId,omitempty"`
}

type GatewayVideoCancelTaskResponse struct {
	ProviderCallID      string         `json:"providerCallId,omitempty"`
	ProviderAsyncTaskID string         `json:"providerAsyncTaskId,omitempty"`
	ExternalTaskID      string         `json:"externalTaskId,omitempty"`
	Status              string         `json:"status"`
	Error               *StandardError `json:"error,omitempty"`
}

type GatewayTextDelta struct {
	Text string `json:"text"`
}

type GatewayDiscoverModelsRequest struct {
	OrganizationID string `json:"organizationId"`
	AccountID      string `json:"accountId"`
	TestType       string `json:"testType,omitempty"`
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
}

type GatewayDiscoverModelsResponse struct {
	ProviderCallID string            `json:"providerCallId,omitempty"`
	Status         string            `json:"status"`
	Models         []DiscoveredModel `json:"models"`
	Unsupported    []any             `json:"unsupported"`
	Error          *StandardError    `json:"error,omitempty"`
	LatencyMS      int               `json:"latencyMs,omitempty"`
}

type GatewayManifestTestRunRequest struct {
	OrganizationID string                 `json:"organizationId"`
	UserID         string                 `json:"userId"`
	Request        ManifestTestRunRequest `json:"request"`
}
