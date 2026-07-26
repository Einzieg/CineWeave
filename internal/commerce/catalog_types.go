package commerce

import (
	"encoding/json"
	"time"

	"github.com/Einzieg/cineweave/internal/videoproduction"
)

type SetupSession struct {
	ID                        string          `json:"id"`
	OrganizationID            string          `json:"organizationId"`
	WorkspaceID               string          `json:"workspaceId"`
	ProjectID                 string          `json:"projectId"`
	WorkflowTemplateVersionID string          `json:"workflowTemplateVersionId"`
	ClientRequestID           string          `json:"clientRequestId"`
	ScopeType                 string          `json:"scopeType"`
	State                     string          `json:"state"`
	Step                      string          `json:"step"`
	Revision                  int64           `json:"revision"`
	InputSnapshot             json.RawMessage `json:"inputSnapshot"`
	SetupAttempt              int             `json:"setupAttempt"`
	SetupWorkflowRunID        *string         `json:"setupWorkflowRunId,omitempty"`
	ProductionWorkflowRunID   *string         `json:"productionWorkflowRunId,omitempty"`
	ProductID                 *string         `json:"productId,omitempty"`
	ScriptUnitID              *string         `json:"scriptUnitId,omitempty"`
	SourceScriptVersionID     *string         `json:"sourceScriptVersionId,omitempty"`
	LocalizationID            *string         `json:"localizationId,omitempty"`
	LastErrorCode             *string         `json:"lastErrorCode,omitempty"`
	LastErrorMessage          *string         `json:"lastErrorMessage,omitempty"`
	CreatedAt                 time.Time       `json:"createdAt"`
	UpdatedAt                 time.Time       `json:"updatedAt"`
	ExpiresAt                 time.Time       `json:"expiresAt"`
	CompletedAt               *time.Time      `json:"completedAt,omitempty"`
}

type SetupPreparation struct {
	Session             SetupSession       `json:"setupSession"`
	Product             Product            `json:"product"`
	ProductVersion      ProductVersion     `json:"productVersion"`
	ScriptUnit          ScriptUnit         `json:"scriptUnit"`
	SourceScriptVersion ScriptVersion      `json:"sourceScriptVersion"`
	References          []ProductReference `json:"references"`
}

type SetupRun struct {
	ID                 string          `json:"id"`
	OrganizationID     string          `json:"organizationId"`
	ProjectID          string          `json:"projectId"`
	SetupSessionID     string          `json:"setupSessionId"`
	AttemptNo          int             `json:"attemptNo"`
	TemporalWorkflowID string          `json:"temporalWorkflowId"`
	Status             string          `json:"status"`
	Input              json.RawMessage `json:"input"`
	Output             json.RawMessage `json:"output"`
	ErrorCode          *string         `json:"errorCode,omitempty"`
	ErrorMessage       *string         `json:"errorMessage,omitempty"`
	CreatedAt          time.Time       `json:"createdAt"`
	UpdatedAt          time.Time       `json:"updatedAt"`
	StartedAt          *time.Time      `json:"startedAt,omitempty"`
	CompletedAt        *time.Time      `json:"completedAt,omitempty"`
	Revision           int64           `json:"revision"`
}

type InitialSetupCommitParams struct {
	OrganizationID            string
	ProjectID                 string
	SetupSessionID            string
	SetupRunID                string
	WorkflowTemplateVersionID string
	ProductID                 string
	ProductVersionID          string
	ProductReferenceIDs       []string
	ScriptUnitID              string
	SourceScriptVersionID     string
	LocalizationID            string
	CreatedBy                 string
	ProductionConfiguration   videoproduction.ProductionConfigurationSnapshot
	CommerceConfiguration     json.RawMessage
	ModelRoutingSnapshot      json.RawMessage
	CapabilitySnapshot        json.RawMessage
	PreparationInputHash      string
	PreparationAgentCalls     json.RawMessage
}

type InitialSetupCommitResult struct {
	Session          SetupSession           `json:"setupSession"`
	Bindings         InitialBindingResult   `json:"bindings"`
	Identity         UnitGenerationIdentity `json:"identity"`
	UnitGenerationID string                 `json:"unitGenerationId"`
	UnitGenerationNo int64                  `json:"unitGenerationNo"`
	LocalizationID   string                 `json:"localizationId"`
	ReferencePackID  string                 `json:"referencePackId"`
}

type Product struct {
	ID                  string          `json:"id"`
	OrganizationID      string          `json:"organizationId"`
	ProjectID           string          `json:"projectId"`
	CurrentVersionID    *string         `json:"currentVersionId,omitempty"`
	Status              string          `json:"status"`
	Revision            int64           `json:"revision"`
	ScriptUnitsRevision int64           `json:"scriptUnitsRevision"`
	Metadata            json.RawMessage `json:"metadata"`
	CurrentVersion      *ProductVersion `json:"currentVersion,omitempty"`
	CreatedAt           time.Time       `json:"createdAt"`
	UpdatedAt           time.Time       `json:"updatedAt"`
}

type ProductVersion struct {
	ID                string          `json:"id"`
	OrganizationID    string          `json:"organizationId"`
	ProjectID         string          `json:"projectId"`
	ProductID         string          `json:"productId"`
	Version           int             `json:"version"`
	Name              string          `json:"name"`
	Brand             string          `json:"brand"`
	SellingPoints     json.RawMessage `json:"sellingPoints"`
	ImmutableFeatures json.RawMessage `json:"immutableFeatures"`
	ProhibitedClaims  json.RawMessage `json:"prohibitedClaims"`
	FactsSnapshot     json.RawMessage `json:"factsSnapshot"`
	FactsHash         string          `json:"factsHash"`
	SourceVersionID   *string         `json:"sourceVersionId,omitempty"`
	CreatedAt         time.Time       `json:"createdAt"`
}

type ProductVersionInput struct {
	Name              string          `json:"name"`
	Brand             string          `json:"brand"`
	SellingPoints     json.RawMessage `json:"sellingPoints"`
	ImmutableFeatures json.RawMessage `json:"immutableFeatures"`
	ProhibitedClaims  json.RawMessage `json:"prohibitedClaims"`
	Metadata          json.RawMessage `json:"metadata"`
}

type ProductMutationResult struct {
	Product         Product        `json:"product"`
	Version         ProductVersion `json:"version"`
	Activated       bool           `json:"activated"`
	RequiresRebuild bool           `json:"requiresRebuild"`
}

type ProductRebuildUnitImpact struct {
	ScriptUnitID           string `json:"scriptUnitId"`
	UnitNo                 int64  `json:"unitNo"`
	Title                  string `json:"title"`
	SourceUnitGenerationID string `json:"sourceUnitGenerationId"`
	SourceReferencePackID  string `json:"sourceReferencePackId"`
}

type ProductRebuildImpact struct {
	ProjectID               string                     `json:"projectId"`
	ProjectGenerationID     string                     `json:"projectGenerationId"`
	ProductID               string                     `json:"productId"`
	SourceProductVersionID  string                     `json:"sourceProductVersionId"`
	TargetProductVersionID  string                     `json:"targetProductVersionId"`
	ExpectedProductRevision int64                      `json:"expectedProductRevision"`
	TargetReferenceIDs      []string                   `json:"targetReferenceIds"`
	TargetReferenceSetHash  string                     `json:"targetReferenceSetHash"`
	ImpactToken             string                     `json:"impactToken"`
	ExpiresAt               time.Time                  `json:"expiresAt"`
	AffectedUnits           []ProductRebuildUnitImpact `json:"affectedUnits"`
	ReusableArtifactCount   int                        `json:"reusableArtifactCount"`
	Blockers                []string                   `json:"blockers"`
}

type ProductRebuildResult struct {
	RebuildID         string `json:"rebuildId"`
	Status            string `json:"status"`
	ProductVersionID  string `json:"productVersionId"`
	ReferencePackID   string `json:"referencePackId"`
	AffectedUnitCount int    `json:"affectedUnitCount"`
	IdempotentReplay  bool   `json:"idempotentReplay"`
}

type CreateProductReferenceParams struct {
	OrganizationID string
	ProjectID      string
	ProductID      string
	StorageKey     string
	MimeType       string
	ContentHash    string
	ByteSize       int64
	Width          int
	Height         int
	ReferenceRole  string
	SetPrimary     bool
	QualityReview  json.RawMessage
	CreatedBy      string
}

type ProductReference struct {
	ID             string          `json:"id"`
	OrganizationID string          `json:"organizationId"`
	ProjectID      string          `json:"projectId"`
	ProductID      string          `json:"productId"`
	ArtifactID     string          `json:"artifactId"`
	MediaFileID    string          `json:"mediaFileId"`
	ReferenceRole  string          `json:"referenceRole"`
	Ordinal        int             `json:"ordinal"`
	IsPrimary      bool            `json:"isPrimary"`
	Status         string          `json:"status"`
	Width          int             `json:"width"`
	Height         int             `json:"height"`
	MimeType       string          `json:"mimeType"`
	ContentHash    string          `json:"contentHash"`
	QualityReview  json.RawMessage `json:"qualityReview"`
	Revision       int64           `json:"revision"`
	PreviewURL     string          `json:"previewUrl,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	ArchivedAt     *time.Time      `json:"archivedAt,omitempty"`
}

type ProductReferenceUpload struct {
	ID                string     `json:"id"`
	OrganizationID    string     `json:"organizationId"`
	ProjectID         string     `json:"projectId"`
	ProductID         string     `json:"productId"`
	SetupSessionID    *string    `json:"setupSessionId,omitempty"`
	StorageKey        string     `json:"-"`
	RequestedMimeType string     `json:"mimeType"`
	OriginalFileName  string     `json:"fileName"`
	Status            string     `json:"status"`
	IdempotencyKey    string     `json:"-"`
	ReferenceID       *string    `json:"referenceId,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	ExpiresAt         time.Time  `json:"expiresAt"`
	CompletedAt       *time.Time `json:"completedAt,omitempty"`
	AbandonedAt       *time.Time `json:"abandonedAt,omitempty"`
}

type ProductReferencePack struct {
	ID               string                     `json:"id"`
	OrganizationID   string                     `json:"organizationId"`
	ProjectID        string                     `json:"projectId"`
	ProductID        string                     `json:"productId"`
	ProductVersionID string                     `json:"productVersionId"`
	ProductFactsHash string                     `json:"productFactsHash"`
	ReferenceSetHash string                     `json:"referenceSetHash"`
	PackHash         string                     `json:"packHash"`
	Status           string                     `json:"status"`
	WorkflowRunID    *string                    `json:"workflowRunId,omitempty"`
	CreatedAt        time.Time                  `json:"createdAt"`
	StaleAt          *time.Time                 `json:"staleAt,omitempty"`
	ArchivedAt       *time.Time                 `json:"archivedAt,omitempty"`
	Items            []ProductReferencePackItem `json:"items"`
}

type ProductReferencePackItem struct {
	ID                 string    `json:"id"`
	ReferencePackID    string    `json:"referencePackId"`
	ProductReferenceID string    `json:"productReferenceId"`
	Ordinal            int       `json:"ordinal"`
	ReferenceRole      string    `json:"referenceRole"`
	ArtifactID         string    `json:"artifactId"`
	MediaFileID        string    `json:"mediaFileId"`
	ContentHash        string    `json:"contentHash"`
	PreviewURL         string    `json:"previewUrl,omitempty"`
	CreatedAt          time.Time `json:"createdAt"`
}

type ScriptUnit struct {
	ID                      string                       `json:"id"`
	OrganizationID          string                       `json:"organizationId"`
	ProjectID               string                       `json:"projectId"`
	ProductID               string                       `json:"productId"`
	UnitNo                  int64                        `json:"unitNo"`
	Title                   string                       `json:"title"`
	SortOrder               int64                        `json:"sortOrder"`
	Status                  string                       `json:"status"`
	CurrentSourceVersionID  *string                      `json:"currentSourceVersionId,omitempty"`
	CurrentLocalizationID   *string                      `json:"currentLocalizationId,omitempty"`
	LanguageMode            string                       `json:"languageMode"`
	ExplicitTargetLanguage  *string                      `json:"explicitTargetLanguage,omitempty"`
	TargetDurationSeconds   int                          `json:"targetDurationSeconds"`
	TargetPlatform          string                       `json:"targetPlatform"`
	DraftContent            string                       `json:"draftContent"`
	DraftContentHash        *string                      `json:"draftContentHash,omitempty"`
	DraftUpdatedAt          *time.Time                   `json:"draftUpdatedAt,omitempty"`
	ActiveUnitGenerationID  *string                      `json:"activeUnitGenerationId,omitempty"`
	UnitGenerationNo        int64                        `json:"unitGenerationNo"`
	StoryboardStrategy      StoryboardStrategy           `json:"storyboardStrategy,omitempty"`
	DerivedFromScriptUnitID *string                      `json:"derivedFromScriptUnitId,omitempty"`
	DerivationKind          *string                      `json:"derivationKind,omitempty"`
	Revision                int64                        `json:"revision"`
	Metadata                json.RawMessage              `json:"metadata"`
	CurrentSourceVersion    *ScriptVersion               `json:"currentSourceVersion,omitempty"`
	CurrentLocalization     *ScriptLocalization          `json:"currentLocalization,omitempty"`
	LanguageResolution      *LanguageResolution          `json:"languageResolution,omitempty"`
	ProductionSummary       *ScriptUnitProductionSummary `json:"productionSummary,omitempty"`
	CreatedAt               time.Time                    `json:"createdAt"`
	UpdatedAt               time.Time                    `json:"updatedAt"`
	ArchivedAt              *time.Time                   `json:"archivedAt,omitempty"`
}

type ScriptUnitProductionSummary struct {
	Status           string `json:"status"`
	CurrentStage     string `json:"currentStage"`
	Progress         int    `json:"progress"`
	FailedCount      int    `json:"failedCount"`
	FinalVideoStatus string `json:"finalVideoStatus"`
}

// ScriptUnitDefaults are project-level creation defaults. They are copied into
// new ScriptUnits and never mutate units that already exist.
type ScriptUnitDefaults struct {
	TargetDurationSeconds int     `json:"targetDurationSeconds"`
	TargetPlatform        string  `json:"targetPlatform"`
	LanguageMode          string  `json:"languageMode"`
	TargetLanguage        *string `json:"targetLanguage"`
}

type CreateScriptUnitInput struct {
	Title                   string  `json:"title"`
	Content                 string  `json:"content"`
	LanguageMode            string  `json:"languageMode"`
	ExplicitTargetLanguage  *string `json:"explicitTargetLanguage,omitempty"`
	TargetDurationSeconds   int     `json:"targetDurationSeconds"`
	TargetPlatform          string  `json:"targetPlatform"`
	SourceLanguageHint      *string `json:"sourceLanguageHint,omitempty"`
	DerivedFromScriptUnitID *string `json:"derivedFromScriptUnitId,omitempty"`
	DerivationKind          *string `json:"derivationKind,omitempty"`
}

type UpdateScriptUnitInput struct {
	Title                  *string `json:"title,omitempty"`
	DraftContent           *string `json:"draftContent,omitempty"`
	LanguageMode           *string `json:"languageMode,omitempty"`
	ExplicitTargetLanguage *string `json:"explicitTargetLanguage,omitempty"`
	TargetDurationSeconds  *int    `json:"targetDurationSeconds,omitempty"`
	TargetPlatform         *string `json:"targetPlatform,omitempty"`
}

type ReorderScriptUnitItem struct {
	ScriptUnitID string `json:"scriptUnitId"`
	SortOrder    int64  `json:"sortOrder"`
}

type ScriptVersionMutation struct {
	ScriptUnit      ScriptUnit    `json:"scriptUnit"`
	Version         ScriptVersion `json:"version"`
	Activated       bool          `json:"activated"`
	RequiresRebuild bool          `json:"requiresRebuild"`
}

type ScriptUnitRebuildTarget struct {
	ExpectedRevision            int64              `json:"expectedRevision"`
	TargetSourceScriptVersionID string             `json:"targetSourceScriptVersionId"`
	TargetLanguageMode          string             `json:"targetLanguageMode"`
	TargetLanguage              *string            `json:"targetLanguage,omitempty"`
	TargetDurationSeconds       int                `json:"targetDurationSeconds"`
	TargetPlatform              string             `json:"targetPlatform"`
	TargetStoryboardStrategy    StoryboardStrategy `json:"targetStoryboardStrategy"`
}

type ScriptUnitRebuildAffectedCounts struct {
	StoryboardPlans int `json:"storyboardPlans"`
	StoryboardShots int `json:"storyboardShots"`
	ReferenceImages int `json:"referenceImages"`
	VideoPrompts    int `json:"videoPrompts"`
	ShotVideos      int `json:"shotVideos"`
	Timelines       int `json:"timelines"`
	FinalVideos     int `json:"finalVideos"`
}

type ScriptUnitRebuildImpact struct {
	ProjectID                   string                          `json:"projectId"`
	ProjectGenerationID         string                          `json:"projectGenerationId"`
	ScriptUnitID                string                          `json:"scriptUnitId"`
	SourceUnitGenerationID      string                          `json:"sourceUnitGenerationId"`
	SourceScriptVersionID       string                          `json:"sourceScriptVersionId"`
	TargetSourceScriptVersionID string                          `json:"targetSourceScriptVersionId"`
	ExpectedRevision            int64                           `json:"expectedRevision"`
	TargetLanguageMode          string                          `json:"targetLanguageMode"`
	TargetLanguage              *string                         `json:"targetLanguage,omitempty"`
	TargetDurationSeconds       int                             `json:"targetDurationSeconds"`
	TargetPlatform              string                          `json:"targetPlatform"`
	TargetStoryboardStrategy    StoryboardStrategy              `json:"targetStoryboardStrategy"`
	TargetConfigurationHash     string                          `json:"targetConfigurationHash"`
	ImpactToken                 string                          `json:"impactToken"`
	ExpiresAt                   time.Time                       `json:"expiresAt"`
	Affected                    ScriptUnitRebuildAffectedCounts `json:"affected"`
	EstimatedAgentCalls         int                             `json:"estimatedAgentCalls"`
	Blockers                    []string                        `json:"blockers"`
}

type ScriptUnitRebuildExecution struct {
	RebuildID                   string                        `json:"rebuildId"`
	Status                      string                        `json:"status"`
	WorkflowRunID               string                        `json:"workflowRunId,omitempty"`
	ScriptUnitID                string                        `json:"scriptUnitId"`
	SourceUnitGenerationID      string                        `json:"sourceUnitGenerationId"`
	TargetSourceScriptVersionID string                        `json:"targetSourceScriptVersionId"`
	TargetConfigurationHash     string                        `json:"targetConfigurationHash"`
	PreparationIdentity         ScriptUnitPreparationIdentity `json:"preparationIdentity"`
	IdempotentReplay            bool                          `json:"idempotentReplay"`
}

type SalesScriptContractRecord struct {
	ID                     string          `json:"id"`
	ScriptUnitID           string          `json:"scriptUnitId"`
	ScriptUnitGenerationID string          `json:"scriptUnitGenerationId"`
	Status                 string          `json:"status"`
	AttemptGeneration      int             `json:"attemptGeneration"`
	CurrentWorkflowRunID   string          `json:"currentWorkflowRunId"`
	InputHash              string          `json:"inputHash"`
	ContractVersion        string          `json:"contractVersion,omitempty"`
	Contract               json.RawMessage `json:"contract,omitempty"`
	ContractHash           string          `json:"contractHash,omitempty"`
	PromptVersionID        string          `json:"promptVersionId,omitempty"`
	ProviderRequestID      string          `json:"providerRequestId,omitempty"`
	ProviderCallID         string          `json:"providerCallId,omitempty"`
	ProviderModelID        string          `json:"providerModelId,omitempty"`
	AcceptedRound          int             `json:"acceptedRound,omitempty"`
	ErrorCode              string          `json:"errorCode,omitempty"`
	ErrorMessage           string          `json:"errorMessage,omitempty"`
	CreatedAt              time.Time       `json:"createdAt"`
	UpdatedAt              time.Time       `json:"updatedAt"`
}

type LocalizationInput struct {
	SourceScriptVersionID string          `json:"sourceScriptVersionId"`
	LanguageResolutionID  string          `json:"languageResolutionId"`
	SourceLanguage        string          `json:"sourceLanguage"`
	TargetLanguage        string          `json:"targetLanguage"`
	LocalizedContent      string          `json:"localizedContent"`
	StructuredContract    json.RawMessage `json:"structuredContract"`
	ReviewerOutput        json.RawMessage `json:"reviewerOutput"`
	Approve               bool            `json:"approve"`
}

type ScriptVersion struct {
	ID                     string    `json:"id"`
	OrganizationID         string    `json:"organizationId"`
	ProjectID              string    `json:"projectId"`
	ProductID              string    `json:"productId"`
	ScriptUnitID           string    `json:"scriptUnitId"`
	Version                int       `json:"version"`
	Content                string    `json:"content"`
	ContentHash            string    `json:"contentHash"`
	SourceLanguageHint     *string   `json:"sourceLanguageHint,omitempty"`
	DetectedSourceLanguage *string   `json:"detectedSourceLanguage,omitempty"`
	ManualOverride         bool      `json:"manualOverride"`
	SourceVersionID        *string   `json:"sourceVersionId,omitempty"`
	CreatedAt              time.Time `json:"createdAt"`
}

type ScriptSegment struct {
	ID              string    `json:"id"`
	ScriptVersionID string    `json:"scriptVersionId"`
	SegmentNo       int       `json:"segmentNo"`
	SegmentKind     string    `json:"segmentKind"`
	SourceText      string    `json:"sourceText"`
	ContentHash     string    `json:"contentHash"`
	Required        bool      `json:"required"`
	CreatedAt       time.Time `json:"createdAt"`
}

type LanguageResolution struct {
	ID                    string     `json:"id"`
	ScriptUnitID          string     `json:"scriptUnitId"`
	SourceScriptVersionID string     `json:"sourceScriptVersionId"`
	LanguageMode          string     `json:"languageMode"`
	SourceLanguage        *string    `json:"sourceLanguage,omitempty"`
	TargetLanguage        *string    `json:"targetLanguage,omitempty"`
	Confidence            *float64   `json:"confidence,omitempty"`
	Reasoning             string     `json:"reasoning"`
	NeedsUserConfirmation bool       `json:"needsUserConfirmation"`
	Status                string     `json:"status"`
	InputHash             string     `json:"inputHash"`
	Revision              int64      `json:"revision"`
	ConfirmedAt           *time.Time `json:"confirmedAt,omitempty"`
	CreatedAt             time.Time  `json:"createdAt"`
	UpdatedAt             time.Time  `json:"updatedAt"`
}

type ScriptLocalization struct {
	ID                        string          `json:"id"`
	ScriptUnitID              string          `json:"scriptUnitId"`
	SourceScriptVersionID     string          `json:"sourceScriptVersionId"`
	LanguageResolutionID      string          `json:"languageResolutionId"`
	Version                   int             `json:"version"`
	SourceLanguage            string          `json:"sourceLanguage"`
	TargetLanguage            string          `json:"targetLanguage"`
	LocalizedContent          string          `json:"localizedContent"`
	LocalizedContentHash      string          `json:"localizedContentHash"`
	StructuredContract        json.RawMessage `json:"structuredContract"`
	EstimatedVoiceoverSeconds float64         `json:"estimatedVoiceoverSeconds"`
	TimingAnalysis            json.RawMessage `json:"timingAnalysis"`
	TimingPolicyVersion       string          `json:"timingPolicyVersion"`
	ReviewStatus              string          `json:"reviewStatus"`
	ReviewerOutput            json.RawMessage `json:"reviewerOutput"`
	Status                    string          `json:"status"`
	Revision                  int64           `json:"revision"`
	CreatedAt                 time.Time       `json:"createdAt"`
	ApprovedAt                *time.Time      `json:"approvedAt,omitempty"`
	ArchivedAt                *time.Time      `json:"archivedAt,omitempty"`
}

type ScriptUnitList struct {
	Items               []ScriptUnit `json:"items"`
	NextCursor          string       `json:"nextCursor,omitempty"`
	HasMore             bool         `json:"hasMore"`
	ScriptUnitsRevision int64        `json:"scriptUnitsRevision"`
}

type TimingEstimate struct {
	Locale                    string  `json:"locale"`
	PolicyVersion             string  `json:"policyVersion"`
	Units                     int     `json:"units"`
	UnitsPerSecond            float64 `json:"unitsPerSecond"`
	EstimatedVoiceoverSeconds float64 `json:"estimatedVoiceoverSeconds"`
	TargetDurationSeconds     int     `json:"targetDurationSeconds"`
	Exceeded                  bool    `json:"exceeded"`
}

type LocalizationTimingPolicy struct {
	Version               string  `json:"version"`
	Unit                  string  `json:"unit"`
	NormalUnitsPerSecond  float64 `json:"normalUnitsPerSecond"`
	CommaPauseSeconds     float64 `json:"commaPauseSeconds"`
	SentencePauseSeconds  float64 `json:"sentencePauseSeconds"`
	SegmentGapSeconds     float64 `json:"segmentGapSeconds"`
	AllowedOverrunSeconds float64 `json:"allowedOverrunSeconds"`
}

type ProjectLanguageOption struct {
	Locale               string   `json:"locale"`
	Label                string   `json:"label"`
	TextAvailable        bool     `json:"textAvailable"`
	ImagePromptAvailable bool     `json:"imagePromptAvailable"`
	VideoPromptAvailable bool     `json:"videoPromptAvailable"`
	NativeAudioAvailable bool     `json:"nativeAudioAvailable"`
	Blockers             []string `json:"blockers"`
}

type ProjectModelRequirement struct {
	Role               string `json:"role"`
	Label              string `json:"label"`
	ProfileKey         string `json:"profileKey"`
	TaskType           string `json:"taskType"`
	Modality           string `json:"modality"`
	UsesInputLanguage  bool   `json:"usesInputLanguage"`
	UsesOutputLanguage bool   `json:"usesOutputLanguage"`
	UsesPromptLanguage bool   `json:"usesPromptLanguage"`
	UsesNativeAudio    bool   `json:"usesNativeAudio"`
	Ready              bool   `json:"ready"`
	CandidateCount     int    `json:"candidateCount"`
	Blocker            string `json:"blocker,omitempty"`
}

type ProjectOptions struct {
	WorkflowTemplateVersionID     string                    `json:"workflowTemplateVersionId"`
	WorkflowTemplateVersion       int                       `json:"workflowTemplateVersion"`
	TemplateContentHash           string                    `json:"templateContentHash"`
	VideoProductionProfileKey     string                    `json:"videoProductionProfileKey"`
	VideoProductionProfileVersion int                       `json:"videoProductionProfileVersion"`
	Available                     bool                      `json:"available"`
	Blockers                      []string                  `json:"blockers"`
	Durations                     []int                     `json:"durations"`
	AspectRatios                  []string                  `json:"aspectRatios"`
	ImageQualities                []string                  `json:"imageQualities"`
	LanguageModes                 []string                  `json:"languageModes"`
	AudioStrategies               []string                  `json:"audioStrategies"`
	AudioRequirements             []string                  `json:"audioRequirements"`
	Languages                     []ProjectLanguageOption   `json:"languages"`
	ModelRequirements             []ProjectModelRequirement `json:"modelRequirements"`
}
