package videoproduction

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	ProfileSingleFrameI2V      = "single_frame_i2v"
	ProfileFirstLastFrame      = "first_last_frame"
	ProfileMultimodalReference = "multimodal_reference"
	ProfileStoryboardSheet     = "storyboard_sheet"

	LifecyclePublished      = "published"
	ImplementationAvailable = "available"
	ImplementationReserved  = "reserved"

	CompatibilityStrict             = "strict"
	CompatibilityCompatibleFallback = "compatible_fallback"
)

const (
	CodeProfileNotFound              = "VIDEO_PRODUCTION_PROFILE_NOT_FOUND"
	CodeProfileUnavailable           = "VIDEO_PRODUCTION_PROFILE_UNAVAILABLE"
	CodeGenerationMismatch           = "PRODUCTION_GENERATION_MISMATCH"
	CodeRebuildConflict              = "PRODUCTION_PROFILE_REBUILD_CONFLICT"
	CodeRebuildImpactStale           = "PRODUCTION_PROFILE_REBUILD_IMPACT_STALE"
	CodeConfigurationRebuildRequired = "VIDEO_PRODUCTION_RECONFIGURATION_REQUIRED"
	CodeProjectLocked                = "PROJECT_VIDEO_PRODUCTION_LOCKED"
	CodePromptContractIncomplete     = "VIDEO_PRODUCTION_PROMPT_CONTRACT_INCOMPLETE"
	CodeProfileIncompatible          = "PRODUCTION_PROFILE_INCOMPATIBLE"
)

const ProductionConfigurationSnapshotVersion = 2

type Error struct {
	Code      string
	Message   string
	Retryable bool
	Cause     error
}

func (e Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func (e Error) Unwrap() error { return e.Cause }

func NewError(code, message string, retryable bool) error {
	return Error{Code: code, Message: message, Retryable: retryable}
}

func AsError(err error) (Error, bool) {
	if err == nil {
		return Error{}, false
	}
	if typed, ok := err.(Error); ok {
		return typed, true
	}
	if typed, ok := err.(*Error); ok && typed != nil {
		return *typed, true
	}
	return Error{}, false
}

type Profile struct {
	ID             string    `json:"id"`
	Key            string    `json:"key"`
	Name           string    `json:"name"`
	StrategyFamily string    `json:"strategyFamily"`
	Description    string    `json:"description"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type ProfileVersion struct {
	ID                     string          `json:"id"`
	ProfileID              string          `json:"profileId"`
	ProfileKey             string          `json:"profileKey"`
	ProfileName            string          `json:"profileName"`
	StrategyFamily         string          `json:"strategyFamily"`
	Description            string          `json:"description"`
	Version                int             `json:"version"`
	LifecycleState         string          `json:"lifecycleState"`
	ImplementationState    string          `json:"implementationState"`
	Configuration          json.RawMessage `json:"configuration"`
	CapabilityRequirements json.RawMessage `json:"capabilityRequirements"`
	PromptContract         json.RawMessage `json:"promptContract"`
	InputContractVersion   string          `json:"inputContractVersion"`
	ConfigurationHash      string          `json:"configurationHash"`
	PromptContractHash     string          `json:"promptContractHash"`
	CreatedAt              time.Time       `json:"createdAt"`
	PublishedAt            *time.Time      `json:"publishedAt,omitempty"`
	RetiredAt              *time.Time      `json:"retiredAt,omitempty"`
}

func (v ProfileVersion) Available() bool {
	return v.LifecycleState == LifecyclePublished && v.ImplementationState == ImplementationAvailable
}

type Binding struct {
	ID                  string          `json:"id"`
	ProjectID           string          `json:"projectId"`
	ProfileVersionID    string          `json:"profileVersionId"`
	ProfileKey          string          `json:"profileKey"`
	ProfileName         string          `json:"profileName"`
	ProfileVersion      int             `json:"profileVersion"`
	LifecycleState      string          `json:"lifecycleState"`
	ImplementationState string          `json:"implementationState"`
	Status              string          `json:"status"`
	CompatibilityPolicy string          `json:"compatibilityPolicy"`
	Overrides           json.RawMessage `json:"overrides"`
	ProfileSnapshot     json.RawMessage `json:"profileSnapshot"`
	ProfileSnapshotHash string          `json:"profileSnapshotHash"`
	Revision            int64           `json:"revision"`
	CreatedAt           time.Time       `json:"createdAt"`
	SupersededAt        *time.Time      `json:"supersededAt,omitempty"`
}

type Generation struct {
	ID                 string     `json:"id"`
	OrganizationID     string     `json:"organizationId"`
	ProjectID          string     `json:"projectId"`
	BindingID          string     `json:"bindingId"`
	GenerationNo       int64      `json:"generationNo"`
	Status             string     `json:"status"`
	SourceGenerationID *string    `json:"sourceGenerationId,omitempty"`
	RebuildID          *string    `json:"rebuildId,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	ActivatedAt        *time.Time `json:"activatedAt,omitempty"`
	SupersededAt       *time.Time `json:"supersededAt,omitempty"`
}

type Context struct {
	Binding    Binding    `json:"binding"`
	Generation Generation `json:"generation"`
	Locked     bool       `json:"locked"`
	State      string     `json:"state"`
}

type ModelCapability struct {
	TaskTypes             json.RawMessage `json:"taskTypes"`
	ProviderOptionsSchema json.RawMessage `json:"providerOptionsSchema"`
}

type CompatibilityIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ModelCompatibility struct {
	Compatible bool                 `json:"compatible"`
	Issues     []CompatibilityIssue `json:"issues"`
}

type Identity struct {
	ProjectID    string
	BindingID    string
	GenerationID string
}

type ProductionConfigurationSnapshot struct {
	SchemaVersion         int                              `json:"schemaVersion"`
	ProjectType           string                           `json:"projectType"`
	ContentType           string                           `json:"contentType"`
	AspectRatio           string                           `json:"aspectRatio"`
	VideoRatio            string                           `json:"videoRatio"`
	ArtStyle              string                           `json:"artStyle"`
	DirectorManual        string                           `json:"directorManual"`
	VisualManual          string                           `json:"visualManual"`
	ImageModelProfileKey  string                           `json:"imageModelProfileKey"`
	VideoModelProfileKey  string                           `json:"videoModelProfileKey"`
	ScriptModelProfileKey string                           `json:"scriptModelProfileKey"`
	TTSModelProfileKey    string                           `json:"ttsModelProfileKey"`
	ASRModelProfileKey    string                           `json:"asrModelProfileKey"`
	AudioStrategy         string                           `json:"audioStrategy"`
	AudioRequirement      string                           `json:"audioRequirement"`
	ImageQuality          string                           `json:"imageQuality"`
	TimelineTimebase      int64                            `json:"timelineTimebase"`
	FPSNumerator          int                              `json:"fpsNumerator"`
	FPSDenominator        int                              `json:"fpsDenominator"`
	Settings              json.RawMessage                  `json:"settings"`
	ManualBindings        map[string]ManualBindingSnapshot `json:"manualBindings"`
}

type InitialBindingParams struct {
	Identity            Identity
	OrganizationID      string
	CreatedBy           string
	ProfileVersion      ProfileVersion
	CompatibilityPolicy string
	Overrides           json.RawMessage
	Configuration       ProductionConfigurationSnapshot
}

type RebuildEpisodeImpact struct {
	ScriptEpisodeID        string  `json:"scriptEpisodeId"`
	EpisodeOrdinal         int     `json:"episodeOrdinal"`
	ScriptEpisodeRevision  int64   `json:"scriptEpisodeRevision"`
	ScriptEpisodeHash      string  `json:"scriptEpisodeContentHash"`
	SourceStoryboardPlanID *string `json:"sourceStoryboardPlanId,omitempty"`
}

type RebuildImpactCounts struct {
	Episodes         int `json:"episodes"`
	StoryboardPlans  int `json:"storyboardPlans"`
	StoryboardShots  int `json:"storyboardShots"`
	ShotRequirements int `json:"shotRequirements"`
	ShotImages       int `json:"shotImages"`
	ShotVideos       int `json:"shotVideos"`
	VideoRenderPlans int `json:"videoRenderPlans"`
	Timelines        int `json:"timelines"`
	TimelineClips    int `json:"timelineClips"`
	FinalVideos      int `json:"finalVideos"`
	RetainedAssets   int `json:"retainedAssets"`
}

type RebuildImpact struct {
	ProjectID               string                          `json:"projectId"`
	ExpectedProjectRevision int64                           `json:"expectedProjectRevision"`
	SourceBindingID         string                          `json:"sourceBindingId"`
	SourceBindingRevision   int64                           `json:"sourceBindingRevision"`
	SourceGenerationID      string                          `json:"sourceGenerationId"`
	SourceGenerationNo      int64                           `json:"sourceGenerationNo"`
	TargetProfileVersionID  string                          `json:"targetProfileVersionId"`
	TargetProfileKey        string                          `json:"targetProfileKey"`
	TargetProfileVersion    int                             `json:"targetProfileVersion"`
	Reason                  string                          `json:"reason"`
	TargetConfiguration     ProductionConfigurationSnapshot `json:"targetConfiguration"`
	TargetConfigurationHash string                          `json:"targetConfigurationHash"`
	ScriptID                string                          `json:"scriptId"`
	ScriptVersionID         string                          `json:"scriptVersionId"`
	Episodes                []RebuildEpisodeImpact          `json:"episodes"`
	Counts                  RebuildImpactCounts             `json:"counts"`
	ImpactToken             string                          `json:"impactToken"`
}

type RebuildSwitchParams struct {
	RebuildID      string
	OrganizationID string
	ProjectID      string
	CreatedBy      string
	Source         Context
	Target         ProfileVersion
	Configuration  ProductionConfigurationSnapshot
}

type PromptVersionSnapshot struct {
	TemplateKey     string `json:"templateKey"`
	PromptVersionID string `json:"promptVersionId"`
	ContentHash     string `json:"contentHash"`
}

type ManualBindingSnapshot struct {
	PromptVersionID string `json:"promptVersionId"`
	TemplateKey     string `json:"templateKey"`
	ContentHash     string `json:"contentHash"`
}

func validateCompatibilityPolicy(value string) error {
	switch value {
	case CompatibilityStrict, CompatibilityCompatibleFallback:
		return nil
	default:
		return fmt.Errorf("unsupported compatibility policy %q", value)
	}
}
