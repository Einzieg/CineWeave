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
	Priority        int    `json:"priority"`
	Weight          int    `json:"weight"`
	Enabled         *bool  `json:"enabled"`
}

type ProviderTestResult struct {
	TestRunID        string          `json:"testRunId"`
	ProviderCallID   string          `json:"providerCallId"`
	Status           string          `json:"status"`
	LatencyMS        int             `json:"latencyMs"`
	ErrorCode        *string         `json:"errorCode,omitempty"`
	ErrorMessage     *string         `json:"errorMessage,omitempty"`
	NormalizedOutput json.RawMessage `json:"normalizedOutput"`
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
	OrganizationID  string             `json:"organizationId"`
	WorkspaceID     string             `json:"workspaceId,omitempty"`
	ProjectID       string             `json:"projectId,omitempty"`
	WorkflowRunID   string             `json:"workflowRunId,omitempty"`
	NodeRunID       string             `json:"nodeRunId,omitempty"`
	ModelProfileKey string             `json:"modelProfileKey,omitempty"`
	ProviderModelID string             `json:"providerModelId,omitempty"`
	PromptVersionID string             `json:"promptVersionId,omitempty"`
	PromptHash      string             `json:"promptHash,omitempty"`
	IdempotencyKey  string             `json:"idempotencyKey,omitempty"`
	Input           json.RawMessage    `json:"input"`
	Options         GatewayTextOptions `json:"options"`
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

type GatewayTextResponse struct {
	ProviderCallID string            `json:"providerCallId"`
	ModelID        string            `json:"modelId"`
	Status         string            `json:"status"`
	Output         GatewayTextOutput `json:"output"`
	Usage          GatewayUsage      `json:"usage"`
	Error          *StandardError    `json:"error,omitempty"`
	LatencyMS      int               `json:"latencyMs,omitempty"`
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
