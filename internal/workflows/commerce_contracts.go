package workflows

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Einzieg/cineweave/internal/commerce"
	"github.com/google/uuid"
)

const (
	CommerceMaxAgentReviewRounds = 3

	CommerceLanguageResolutionContractVersion  = "commerce-language-resolution/v1"
	CommerceLocalizationContractVersion        = "commerce-script-localization/v1"
	CommerceReviewDecisionContractVersion      = "commerce-review-decision/v1"
	CommerceSalesScriptContractVersion         = "commerce-sales-script/v1"
	CommerceStoryboardPlanContractVersion      = "commerce-storyboard-plan/v1"
	CommerceStoryboardReviewContractVersion    = "commerce-storyboard-review/v1"
	CommerceImagePromptPlanContractVersion     = "commerce-image-prompt-plan/v1"
	CommerceImageFidelityReviewContractVersion = "commerce-image-fidelity-review/v1"
	CommerceVideoPromptPlanContractVersion     = "commerce-video-prompt-plan/v1"
	CommerceVideoPromptReviewContractVersion   = "commerce-video-prompt-review/v1"

	CommerceCodeWorkflowInputInvalid         = "COMMERCE_WORKFLOW_INPUT_INVALID"
	CommerceCodeActivityPortUnavailable      = "COMMERCE_ACTIVITY_PORT_UNAVAILABLE"
	CommerceCodeAgentOutputInvalid           = "COMMERCE_AGENT_OUTPUT_INVALID"
	CommerceCodeLanguageContractInvalid      = "COMMERCE_LANGUAGE_CONTRACT_INVALID"
	CommerceCodeLocalizationContractInvalid  = "COMMERCE_LOCALIZATION_CONTRACT_INVALID"
	CommerceCodeLocalizationReviewExhausted  = "COMMERCE_LOCALIZATION_REVIEW_EXHAUSTED"
	CommerceCodeStoryboardContractInvalid    = "COMMERCE_STORYBOARD_CONTRACT_INVALID"
	CommerceCodeStoryboardReplanRequired     = "COMMERCE_STORYBOARD_REPLAN_REQUIRED"
	CommerceCodeImagePromptContractInvalid   = "COMMERCE_IMAGE_PROMPT_CONTRACT_INVALID"
	CommerceCodeImageFidelityRejected        = "COMMERCE_IMAGE_FIDELITY_REJECTED"
	CommerceCodeVideoPromptContractInvalid   = "COMMERCE_VIDEO_PROMPT_CONTRACT_INVALID"
	CommerceCodeVideoPromptReviewExhausted   = "COMMERCE_VIDEO_PROMPT_REVIEW_EXHAUSTED"
	CommerceCodeVideoReferenceRequired       = "COMMERCE_VIDEO_REFERENCE_REQUIRED"
	CommerceCodeProductReferencePackStale    = "PRODUCT_REFERENCE_PACK_STALE"
	CommerceCodeScriptDurationExceeded       = "COMMERCE_SCRIPT_DURATION_EXCEEDED"
	CommerceCodeGenerationMismatch           = "COMMERCE_SCRIPT_UNIT_GENERATION_MISMATCH"
	CommerceCodeLanguageConfirmationRequired = "COMMERCE_LANGUAGE_CONFIRMATION_REQUIRED"
)

var (
	commerceLocalePattern       = regexp.MustCompile(`^[A-Za-z]{2,3}(?:-[A-Za-z0-9]{2,8})*$`)
	commerceSpeechMarkerPattern = regexp.MustCompile(
		`(?i)(?:旁白|配音|口播|解说|主播|voiceover|vo)\s*[:：]\s*`,
	)
	commerceOnscreenMarkerPattern = regexp.MustCompile(
		`(?i)(?:字幕|屏幕文字|画面文字|onscreen(?:\s+text)?)\s*[:：]\s*`,
	)
	commerceNonSpeechChannelPattern = regexp.MustCompile(
		`(?i)(?:音效|音乐|bgm|sfx|字幕|屏幕文字|画面文字|画面|镜头)\s*[:：]\s*`,
	)
	commerceAnyChannelPattern = regexp.MustCompile(
		`(?i)(?:旁白|配音|口播|解说|主播|voiceover|vo|音效|音乐|bgm|sfx|字幕|屏幕文字|画面文字|画面|镜头)\s*[:：]\s*`,
	)
	commerceVisualOnlyPrefixPattern = regexp.MustCompile(
		`(?i)^(?:镜头(?:[一二三四五六七八九十百零\d]+)?|画面|场景|动作|音效|音乐|字幕|屏幕文字|画面文字|sfx|bgm)\s*[:：]`,
	)
)

type CommerceReviewIssue struct {
	Code            string `json:"code"`
	Field           string `json:"field"`
	SourceSegmentID string `json:"sourceSegmentId,omitempty"`
	CandidateKey    string `json:"candidateKey,omitempty"`
	Message         string `json:"message"`
	Suggestion      string `json:"suggestion"`
}

type CommerceLanguageIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type CommerceLanguageResolutionContract struct {
	ContractVersion       string                  `json:"contractVersion"`
	SourceLanguage        string                  `json:"sourceLanguage"`
	TargetLanguage        string                  `json:"targetLanguage"`
	Confidence            float64                 `json:"confidence"`
	LanguageComposition   string                  `json:"languageComposition"`
	NeedsUserConfirmation bool                    `json:"needsUserConfirmation"`
	Reasoning             string                  `json:"reasoning"`
	Issues                []CommerceLanguageIssue `json:"issues"`
}

type CommerceLocalizationSegmentContract struct {
	Ordinal                 int      `json:"ordinal"`
	SourceSegmentID         string   `json:"sourceSegmentId"`
	SalesBeat               string   `json:"salesBeat"`
	SourceText              string   `json:"sourceText"`
	LocalizedText           string   `json:"localizedText"`
	VoiceoverText           string   `json:"voiceoverText"`
	OnscreenText            string   `json:"onscreenText"`
	ProductClaims           []string `json:"productClaims"`
	RequiredProductFeatures []string `json:"requiredProductFeatures"`
}

type CommerceLocalizationContract struct {
	ContractVersion string                                `json:"contractVersion"`
	SourceLanguage  string                                `json:"sourceLanguage"`
	TargetLanguage  string                                `json:"targetLanguage"`
	Segments        []CommerceLocalizationSegmentContract `json:"segments"`
	PreservedTerms  []string                              `json:"preservedTerms"`
	Warnings        []json.RawMessage                     `json:"warnings"`
}

type CommerceLocalizationReviewContract struct {
	ContractVersion   string                `json:"contractVersion"`
	Decision          string                `json:"decision"`
	Issues            []CommerceReviewIssue `json:"issues"`
	CheckedSegmentIDs []string              `json:"checkedSegmentIds"`
}

type CommerceSalesScriptSegmentContract struct {
	Ordinal                 int      `json:"ordinal"`
	SourceSegmentID         string   `json:"sourceSegmentId"`
	SalesBeat               string   `json:"salesBeat"`
	VoiceoverText           string   `json:"voiceoverText"`
	OnscreenText            string   `json:"onscreenText"`
	VisualIntent            string   `json:"visualIntent"`
	ProductClaims           []string `json:"productClaims"`
	RequiredProductFeatures []string `json:"requiredProductFeatures"`
	SoundEffects            []string `json:"soundEffects"`
	MusicCue                string   `json:"musicCue"`
}

type CommerceSalesScriptContract struct {
	ContractVersion        string                               `json:"contractVersion"`
	CommerceScriptUnitID   string                               `json:"commerceScriptUnitId"`
	ScriptUnitGenerationID string                               `json:"scriptUnitGenerationId"`
	ProductVersionID       string                               `json:"productVersionId"`
	TargetLocale           string                               `json:"targetLocale"`
	TargetDurationSeconds  int                                  `json:"targetDurationSeconds"`
	Segments               []CommerceSalesScriptSegmentContract `json:"segments"`
	Warnings               []json.RawMessage                    `json:"warnings"`
}

type CommerceStoryboardShotContract struct {
	CandidateKey            string          `json:"candidateKey"`
	ShotOrdinal             int             `json:"shotOrdinal"`
	SourceSegmentIDs        []string        `json:"sourceSegmentIds"`
	DurationSeconds         int             `json:"durationSeconds"`
	SalesBeat               string          `json:"salesBeat"`
	ShotPurpose             string          `json:"shotPurpose"`
	VisualAction            string          `json:"visualAction"`
	Camera                  json.RawMessage `json:"camera"`
	Composition             string          `json:"composition"`
	VoiceoverText           string          `json:"voiceoverText"`
	OnscreenText            string          `json:"onscreenText"`
	SoundEffects            []string        `json:"soundEffects"`
	MusicCue                string          `json:"musicCue"`
	ProductReferenceIDs     []string        `json:"productReferenceIds"`
	RequiredProductFeatures []string        `json:"requiredProductFeatures"`
}

type CommerceStoryboardPlanContract struct {
	ContractVersion           string                           `json:"contractVersion"`
	CommerceScriptUnitID      string                           `json:"commerceScriptUnitId"`
	ScriptUnitGenerationID    string                           `json:"scriptUnitGenerationId"`
	CommerceWorkflowBindingID string                           `json:"commerceWorkflowBindingId"`
	ProductVersionID          string                           `json:"productVersionId"`
	TargetLocale              string                           `json:"targetLocale"`
	TargetDurationSeconds     int                              `json:"targetDurationSeconds"`
	Shots                     []CommerceStoryboardShotContract `json:"shots"`
}

type CommerceStoryboardReviewContract struct {
	ContractVersion         string                `json:"contractVersion"`
	Decision                string                `json:"decision"`
	Issues                  []CommerceReviewIssue `json:"issues"`
	CheckedCandidateKeys    []string              `json:"checkedCandidateKeys"`
	SegmentCoverageComplete bool                  `json:"segmentCoverageComplete"`
	DurationTotalSeconds    int                   `json:"durationTotalSeconds"`
}

type CommerceSourceSegmentSnapshot struct {
	ID          string `json:"id"`
	Ordinal     int    `json:"ordinal"`
	Kind        string `json:"kind"`
	SourceText  string `json:"sourceText"`
	ContentHash string `json:"contentHash"`
	Required    bool   `json:"required"`
}

type CommerceLocalizedSegmentSnapshot struct {
	ID                      string   `json:"id"`
	SourceSegmentID         string   `json:"sourceSegmentId"`
	Ordinal                 int      `json:"ordinal"`
	SalesBeat               string   `json:"salesBeat"`
	LocalizedText           string   `json:"localizedText"`
	VoiceoverText           string   `json:"voiceoverText"`
	OnscreenText            string   `json:"onscreenText"`
	ProductClaims           []string `json:"productClaims"`
	RequiredProductFeatures []string `json:"requiredProductFeatures"`
	Required                bool     `json:"required"`
}

type CommerceProductReferenceSnapshot struct {
	PackItemID  string `json:"packItemId"`
	ReferenceID string `json:"referenceId"`
	Role        string `json:"role"`
	Ordinal     int    `json:"ordinal"`
	ContentHash string `json:"contentHash"`
	Required    bool   `json:"required"`
}

type CommerceAgentBinding struct {
	Role              string `json:"role"`
	TemplateKey       string `json:"templateKey"`
	PromptVersionID   string `json:"promptVersionId"`
	PromptContentHash string `json:"promptContentHash"`
	ModelProfileKey   string `json:"modelProfileKey"`
	ProviderModelID   string `json:"providerModelId"`
	MaxReviewRounds   int    `json:"maxReviewRounds"`
}

type CommercePreparationAgentBindings struct {
	LanguageResolver     CommerceAgentBinding `json:"languageResolver"`
	ScriptLocalizer      CommerceAgentBinding `json:"scriptLocalizer"`
	LocalizationReviewer CommerceAgentBinding `json:"localizationReviewer"`
}

type CommerceStoryboardAgentBindings struct {
	ScriptOrganizer    CommerceAgentBinding `json:"scriptOrganizer"`
	StoryboardPlanner  CommerceAgentBinding `json:"storyboardPlanner"`
	StoryboardReviewer CommerceAgentBinding `json:"storyboardReviewer"`
}

type CommerceReferenceImageAgentBindings struct {
	ImagePromptAgent      CommerceAgentBinding `json:"imagePromptAgent"`
	ImageFidelityReviewer CommerceAgentBinding `json:"imageFidelityReviewer"`
}

type CommerceVideoPromptAgentBindings struct {
	VideoPromptAgent    CommerceAgentBinding `json:"videoPromptAgent"`
	VideoPromptReviewer CommerceAgentBinding `json:"videoPromptReviewer"`
}

type CommerceMediaModelBinding struct {
	Role            string `json:"role"`
	ModelProfileKey string `json:"modelProfileKey"`
	ProviderModelID string `json:"providerModelId"`
}

type CommerceReferenceImageReference struct {
	PackItemID  string `json:"packItemId"`
	ReferenceID string `json:"referenceId"`
	Role        string `json:"role"`
	Ordinal     int    `json:"ordinal"`
	ArtifactID  string `json:"artifactId"`
	MediaFileID string `json:"mediaFileId"`
	StorageKey  string `json:"storageKey"`
	ContentHash string `json:"contentHash"`
	Required    bool   `json:"required"`
}

type CommerceReferenceImageShotSnapshot struct {
	Identity               commerce.UnitGenerationIdentity     `json:"identity"`
	InputHash              string                              `json:"inputHash"`
	StoryboardPlanID       string                              `json:"storyboardPlanId"`
	StoryboardPlanRevision int                                 `json:"storyboardPlanRevision"`
	StoryboardEditRevision int                                 `json:"storyboardEditRevision"`
	StoryboardShotID       string                              `json:"storyboardShotId"`
	ShotOrdinal            int                                 `json:"shotOrdinal"`
	ShotContractHash       string                              `json:"shotContractHash"`
	SalesBeat              string                              `json:"salesBeat"`
	VisualAction           string                              `json:"visualAction"`
	ProductPresentation    json.RawMessage                     `json:"productPresentation"`
	VoiceoverText          string                              `json:"voiceoverText"`
	OnscreenText           string                              `json:"onscreenText"`
	SoundEffects           []string                            `json:"soundEffects"`
	MusicCue               string                              `json:"musicCue"`
	ProductFacts           json.RawMessage                     `json:"productFacts"`
	ProductVersionID       string                              `json:"productVersionId"`
	LocalizationID         string                              `json:"localizationId"`
	ReferencePackID        string                              `json:"referencePackId"`
	ReferencePackHash      string                              `json:"referencePackHash"`
	TargetLocale           string                              `json:"targetLocale"`
	AspectRatio            string                              `json:"aspectRatio"`
	ImageQuality           string                              `json:"imageQuality"`
	MinimumReferences      int                                 `json:"minimumReferences"`
	MaximumReferences      int                                 `json:"maximumReferences"`
	References             []CommerceReferenceImageReference   `json:"references"`
	Bindings               CommerceReferenceImageAgentBindings `json:"bindings"`
	ImageModel             CommerceMediaModelBinding           `json:"imageModel"`
}

type CommerceImagePromptPlanContract struct {
	ContractVersion           string   `json:"contractVersion"`
	CommerceScriptUnitID      string   `json:"commerceScriptUnitId"`
	ScriptUnitGenerationID    string   `json:"scriptUnitGenerationId"`
	CommerceWorkflowBindingID string   `json:"commerceWorkflowBindingId"`
	ProductVersionID          string   `json:"productVersionId"`
	VisualPrompt              string   `json:"visualPrompt"`
	InstructionLanguage       string   `json:"instructionLanguage"`
	TargetLanguage            string   `json:"targetLanguage"`
	NegativePrompt            string   `json:"negativePrompt"`
	ReferenceIDs              []string `json:"referenceIds"`
	MustPreserve              []string `json:"mustPreserve"`
	MustNotRenderText         []string `json:"mustNotRenderText"`
	AspectRatio               string   `json:"aspectRatio"`
}

type CommerceImageFidelityChecks struct {
	ProductIdentity    bool `json:"productIdentity"`
	Packaging          bool `json:"packaging"`
	Color              bool `json:"color"`
	Shape              bool `json:"shape"`
	ReferenceOwnership bool `json:"referenceOwnership"`
	NoForbiddenText    bool `json:"noForbiddenText"`
	ShotAlignment      bool `json:"shotAlignment"`
}

type CommerceImageFidelityReviewContract struct {
	ContractVersion         string                      `json:"contractVersion"`
	Decision                string                      `json:"decision"`
	Issues                  []CommerceReviewIssue       `json:"issues"`
	Checks                  CommerceImageFidelityChecks `json:"checks"`
	RegenerationRecommended bool                        `json:"regenerationRecommended"`
}

type CommerceVideoFirstFrameReference struct {
	ImageVersionID string `json:"imageVersionId"`
	ArtifactID     string `json:"artifactId"`
	MediaFileID    string `json:"mediaFileId"`
	StorageKey     string `json:"storageKey"`
	ContentHash    string `json:"contentHash"`
}

type CommerceVideoPromptShotSnapshot struct {
	Identity                       commerce.UnitGenerationIdentity    `json:"identity"`
	InputHash                      string                             `json:"inputHash"`
	StoryboardPlanID               string                             `json:"storyboardPlanId"`
	StoryboardPlanRevision         int                                `json:"storyboardPlanRevision"`
	StoryboardEditRevision         int                                `json:"storyboardEditRevision"`
	StoryboardShotID               string                             `json:"storyboardShotId"`
	ShotOrdinal                    int                                `json:"shotOrdinal"`
	ShotContractHash               string                             `json:"shotContractHash"`
	VisualAction                   string                             `json:"visualAction"`
	SalesBeat                      string                             `json:"salesBeat"`
	ProductPresentation            json.RawMessage                    `json:"productPresentation"`
	FullLocalizedScript            string                             `json:"fullLocalizedScript"`
	LocalizedContentHash           string                             `json:"localizedContentHash"`
	LocalizedContractHash          string                             `json:"localizedContractHash"`
	TimingPolicyVersion            string                             `json:"timingPolicyVersion"`
	SourceSegmentIDs               []string                           `json:"sourceSegmentIds"`
	LocalizedSegments              []CommerceLocalizedSegmentSnapshot `json:"localizedSegments"`
	VoiceoverText                  string                             `json:"voiceoverText"`
	OnscreenText                   string                             `json:"onscreenText"`
	SoundEffects                   []string                           `json:"soundEffects"`
	MusicCue                       string                             `json:"musicCue"`
	TargetLocale                   string                             `json:"targetLocale"`
	InstructionLanguage            string                             `json:"instructionLanguage"`
	DurationSeconds                int                                `json:"durationSeconds"`
	DurationTicks                  int64                              `json:"durationTicks"`
	TimelineTimebase               int64                              `json:"timelineTimebase"`
	FPSNumerator                   int                                `json:"fpsNumerator"`
	FPSDenominator                 int                                `json:"fpsDenominator"`
	AspectRatio                    string                             `json:"aspectRatio"`
	ProductVersionID               string                             `json:"productVersionId"`
	LocalizationID                 string                             `json:"localizationId"`
	ReferencePackID                string                             `json:"referencePackId"`
	ProductFacts                   json.RawMessage                    `json:"productFacts"`
	FirstFrame                     CommerceVideoFirstFrameReference   `json:"firstFrame"`
	AudioStrategy                  string                             `json:"audioStrategy"`
	AudioRequirement               string                             `json:"audioRequirement"`
	NativeAudioRequested           bool                               `json:"nativeAudioRequested"`
	NativeAudioRequired            bool                               `json:"nativeAudioRequired"`
	AllowedDurations               []int                              `json:"allowedDurations"`
	SupportedPromptLanguages       []string                           `json:"supportedPromptLanguages"`
	NativeAudioLanguages           []string                           `json:"nativeAudioLanguages"`
	LanguageCapabilitySnapshotHash string                             `json:"languageCapabilitySnapshotHash"`
	VideoProfileKey                string                             `json:"videoProfileKey"`
	VideoProfileVersionID          string                             `json:"videoProfileVersionId"`
	VideoProfileSnapshotHash       string                             `json:"videoProfileSnapshotHash"`
	VideoInputContract             string                             `json:"videoInputContract"`
	AgentModelContextLimit         int                                `json:"agentModelContextLimit"`
	AgentModelPromptLimit          int                                `json:"agentModelPromptLimit"`
	Bindings                       CommerceVideoPromptAgentBindings   `json:"bindings"`
	VideoModel                     CommerceMediaModelBinding          `json:"videoModel"`
}

type CommerceVideoPromptPlanContract struct {
	ContractVersion           string   `json:"contractVersion"`
	CommerceScriptUnitID      string   `json:"commerceScriptUnitId"`
	ScriptUnitGenerationID    string   `json:"scriptUnitGenerationId"`
	CommerceWorkflowBindingID string   `json:"commerceWorkflowBindingId"`
	ProductVersionID          string   `json:"productVersionId"`
	SourceSegmentIDs          []string `json:"sourceSegmentIds"`
	InstructionLanguage       string   `json:"instructionLanguage"`
	SpokenLanguage            string   `json:"spokenLanguage"`
	VisualPrompt              string   `json:"visualPrompt"`
	VoiceoverText             string   `json:"voiceoverText"`
	OnscreenText              string   `json:"onscreenText"`
	SoundEffects              []string `json:"soundEffects"`
	MusicCue                  string   `json:"musicCue"`
	NativeAudioRequested      bool     `json:"nativeAudioRequested"`
	ReferencePackID           string   `json:"referencePackId"`
	ReferenceIDs              []string `json:"referenceIds"`
	DurationSeconds           int      `json:"durationSeconds"`
}

type CommerceVideoPromptReviewChecks struct {
	Identity                bool `json:"identity"`
	SingleFrameReachability bool `json:"singleFrameReachability"`
	VerbatimVoiceover       bool `json:"verbatimVoiceover"`
	AudioSeparation         bool `json:"audioSeparation"`
	OverlaySeparation       bool `json:"overlaySeparation"`
	ReferenceContract       bool `json:"referenceContract"`
	DurationCapability      bool `json:"durationCapability"`
	NativeAudioLanguage     bool `json:"nativeAudioLanguage"`
}

type CommerceVideoPromptReviewContract struct {
	ContractVersion string                          `json:"contractVersion"`
	Decision        string                          `json:"decision"`
	Issues          []CommerceReviewIssue           `json:"issues"`
	Checks          CommerceVideoPromptReviewChecks `json:"checks"`
}

type CommerceTimingPolicy struct {
	Version               string  `json:"version"`
	Unit                  string  `json:"unit"`
	NormalUnitsPerSecond  float64 `json:"normalUnitsPerSecond"`
	CommaPauseSeconds     float64 `json:"commaPauseSeconds"`
	SentencePauseSeconds  float64 `json:"sentencePauseSeconds"`
	SegmentGapSeconds     float64 `json:"segmentGapSeconds"`
	AllowedOverrunSeconds float64 `json:"allowedOverrunSeconds"`
}

type CommerceScriptUnitPreparationSnapshot struct {
	Identity                    commerce.ScriptUnitPreparationIdentity `json:"identity"`
	InputHash                   string                                 `json:"inputHash"`
	WorkflowTemplateVersionID   string                                 `json:"workflowTemplateVersionId"`
	WorkflowTemplateContentHash string                                 `json:"workflowTemplateContentHash"`
	ProductVersionID            string                                 `json:"productVersionId"`
	SourceScriptVersionID       string                                 `json:"sourceScriptVersionId"`
	ReferencePackID             string                                 `json:"referencePackId"`
	LanguageMode                string                                 `json:"languageMode"`
	ExplicitTargetLanguage      string                                 `json:"explicitTargetLanguage,omitempty"`
	SourceLanguageHint          string                                 `json:"sourceLanguageHint,omitempty"`
	AllowedLocales              []string                               `json:"allowedLocales"`
	LanguageConfidenceThreshold float64                                `json:"languageConfidenceThreshold"`
	TargetDurationSeconds       int                                    `json:"targetDurationSeconds"`
	TargetPlatform              string                                 `json:"targetPlatform"`
	SourceSegments              []CommerceSourceSegmentSnapshot        `json:"sourceSegments"`
	ProductFacts                json.RawMessage                        `json:"productFacts"`
	TimingPolicies              map[string]CommerceTimingPolicy        `json:"timingPolicies"`
	Bindings                    CommercePreparationAgentBindings       `json:"bindings"`
}

type CommerceStoryboardPlanningSnapshot struct {
	Identity              commerce.UnitGenerationIdentity    `json:"identity"`
	InputHash             string                             `json:"inputHash"`
	ProductVersionID      string                             `json:"productVersionId"`
	SourceScriptVersionID string                             `json:"sourceScriptVersionId"`
	LocalizationID        string                             `json:"localizationId"`
	ReferencePackID       string                             `json:"referencePackId"`
	TargetLocale          string                             `json:"targetLocale"`
	TargetDurationSeconds int                                `json:"targetDurationSeconds"`
	AspectRatio           string                             `json:"aspectRatio"`
	TimelineTimebase      int64                              `json:"timelineTimebase"`
	FPSNumerator          int                                `json:"fpsNumerator"`
	FPSDenominator        int                                `json:"fpsDenominator"`
	TimingPolicyVersion   string                             `json:"timingPolicyVersion"`
	LocalizedContentHash  string                             `json:"localizedContentHash"`
	LocalizedContractHash string                             `json:"localizedContractHash"`
	AllowedShotDurations  []int                              `json:"allowedShotDurations"`
	LocalizedSegments     []CommerceLocalizedSegmentSnapshot `json:"localizedSegments"`
	ProductReferences     []CommerceProductReferenceSnapshot `json:"productReferences"`
	ProductFacts          json.RawMessage                    `json:"productFacts"`
	LocalizationContract  json.RawMessage                    `json:"localizationContract"`
	Bindings              CommerceStoryboardAgentBindings    `json:"bindings"`
}

type CommerceTimingAnalysis struct {
	Locale                    string  `json:"locale"`
	PolicyVersion             string  `json:"policyVersion"`
	Unit                      string  `json:"unit"`
	Units                     int     `json:"units"`
	SpeechSeconds             float64 `json:"speechSeconds"`
	PauseSeconds              float64 `json:"pauseSeconds"`
	EstimatedVoiceoverSeconds float64 `json:"estimatedVoiceoverSeconds"`
	TargetDurationSeconds     int     `json:"targetDurationSeconds"`
	AllowedOverrunSeconds     float64 `json:"allowedOverrunSeconds"`
	Exceeded                  bool    `json:"exceeded"`
}

type CommerceShotSegmentProjection struct {
	CandidateKey          string `json:"candidateKey"`
	LocalizationID        string `json:"localizationId"`
	LocalizationSegmentID string `json:"localizationSegmentId"`
	SourceSegmentID       string `json:"sourceSegmentId"`
	Usage                 string `json:"usage"`
	Ordinal               int    `json:"ordinal"`
	VerbatimStart         *int   `json:"verbatimStart,omitempty"`
	VerbatimEnd           *int   `json:"verbatimEnd,omitempty"`
}

type CommerceShotProductReferenceProjection struct {
	CandidateKey       string `json:"candidateKey"`
	ProductReferenceID string `json:"productReferenceId"`
	SourcePackID       string `json:"sourcePackId"`
	SourcePackItemID   string `json:"sourcePackItemId"`
	Role               string `json:"role"`
	Ordinal            int    `json:"ordinal"`
	Required           bool   `json:"required"`
}

type CommerceStoryboardShotProjection struct {
	CandidateKey      string                                   `json:"candidateKey"`
	ShotOrdinal       int                                      `json:"shotOrdinal"`
	StartSeconds      int                                      `json:"startSeconds"`
	DurationSeconds   int                                      `json:"durationSeconds"`
	Contract          CommerceStoryboardShotContract           `json:"contract"`
	ContractHash      string                                   `json:"contractHash"`
	SegmentLinks      []CommerceShotSegmentProjection          `json:"segmentLinks"`
	ProductReferences []CommerceShotProductReferenceProjection `json:"productReferences"`
}

type CommerceStoryboardProjection struct {
	Identity              commerce.UnitGenerationIdentity    `json:"identity"`
	InputHash             string                             `json:"inputHash"`
	ProductVersionID      string                             `json:"productVersionId"`
	SourceScriptVersionID string                             `json:"sourceScriptVersionId"`
	LocalizationID        string                             `json:"localizationId"`
	ReferencePackID       string                             `json:"referencePackId"`
	TargetLocale          string                             `json:"targetLocale"`
	TargetDurationSeconds int                                `json:"targetDurationSeconds"`
	PlanHash              string                             `json:"planHash"`
	Shots                 []CommerceStoryboardShotProjection `json:"shots"`
}

func ParseCommerceLanguageResolution(raw string) (CommerceLanguageResolutionContract, error) {
	item, err := decodeCommerceContract[CommerceLanguageResolutionContract](raw)
	if err != nil {
		return item, fmt.Errorf("language resolution JSON: %w", err)
	}
	item.SourceLanguage, err = canonicalCommerceLocale(item.SourceLanguage)
	if err != nil {
		return item, fmt.Errorf("sourceLanguage: %w", err)
	}
	item.TargetLanguage, err = canonicalCommerceLocale(item.TargetLanguage)
	if err != nil {
		return item, fmt.Errorf("targetLanguage: %w", err)
	}
	return item, nil
}

func ValidateCommerceLanguageResolution(item CommerceLanguageResolutionContract, snapshot CommerceScriptUnitPreparationSnapshot) error {
	if item.ContractVersion != CommerceLanguageResolutionContractVersion {
		return fmt.Errorf("contractVersion must be %s", CommerceLanguageResolutionContractVersion)
	}
	if item.Confidence < 0 || item.Confidence > 1 {
		return errors.New("confidence must be between 0 and 1")
	}
	if strings.TrimSpace(item.Reasoning) == "" {
		return errors.New("reasoning is required")
	}
	if item.LanguageComposition != "single" && item.LanguageComposition != "mixed" && item.LanguageComposition != "undetermined" {
		return errors.New("languageComposition is invalid")
	}
	allowed, err := canonicalLocaleSet(snapshot.AllowedLocales)
	if err != nil {
		return err
	}
	if _, ok := allowed[item.SourceLanguage]; !ok {
		return fmt.Errorf("sourceLanguage %s is not allowed by the frozen template", item.SourceLanguage)
	}
	if _, ok := allowed[item.TargetLanguage]; !ok {
		return fmt.Errorf("targetLanguage %s is not allowed by the frozen template", item.TargetLanguage)
	}
	switch snapshot.LanguageMode {
	case "explicit":
		expected, err := canonicalCommerceLocale(snapshot.ExplicitTargetLanguage)
		if err != nil {
			return errors.New("explicit target language is invalid")
		}
		if item.TargetLanguage != expected {
			return fmt.Errorf("explicit targetLanguage changed from %s to %s", expected, item.TargetLanguage)
		}
	case "auto":
		needsConfirmation := item.Confidence < snapshot.LanguageConfidenceThreshold || item.LanguageComposition != "single"
		if needsConfirmation && !item.NeedsUserConfirmation {
			return errors.New("low-confidence or mixed language output must require user confirmation")
		}
	default:
		return errors.New("languageMode must be explicit or auto")
	}
	return validateLanguageIssues(item.Issues)
}

func ParseCommerceLocalization(raw string) (CommerceLocalizationContract, error) {
	item, err := decodeCommerceContract[CommerceLocalizationContract](raw)
	if err != nil {
		return item, fmt.Errorf("localization JSON: %w", err)
	}
	item.SourceLanguage, err = canonicalCommerceLocale(item.SourceLanguage)
	if err != nil {
		return item, fmt.Errorf("sourceLanguage: %w", err)
	}
	item.TargetLanguage, err = canonicalCommerceLocale(item.TargetLanguage)
	if err != nil {
		return item, fmt.Errorf("targetLanguage: %w", err)
	}
	return item, nil
}

func BuildCommerceIdentityLocalization(snapshot CommerceScriptUnitPreparationSnapshot, resolution CommerceLanguageResolutionContract) CommerceLocalizationContract {
	segments := make([]CommerceLocalizationSegmentContract, 0, len(snapshot.SourceSegments))
	for _, source := range snapshot.SourceSegments {
		voiceover, onscreen := splitCommerceIdentityChannels(source.SourceText)
		segments = append(segments, CommerceLocalizationSegmentContract{
			Ordinal: source.Ordinal, SourceSegmentID: source.ID, SalesBeat: source.Kind,
			SourceText: source.SourceText, LocalizedText: source.SourceText,
			VoiceoverText: voiceover, OnscreenText: onscreen,
			ProductClaims: []string{}, RequiredProductFeatures: []string{},
		})
	}
	return CommerceLocalizationContract{
		ContractVersion: CommerceLocalizationContractVersion,
		SourceLanguage:  resolution.SourceLanguage, TargetLanguage: resolution.TargetLanguage,
		Segments: segments, PreservedTerms: []string{}, Warnings: []json.RawMessage{},
	}
}

func ValidateCommerceLocalization(item CommerceLocalizationContract, snapshot CommerceScriptUnitPreparationSnapshot, resolution CommerceLanguageResolutionContract) error {
	if item.ContractVersion != CommerceLocalizationContractVersion {
		return fmt.Errorf("contractVersion must be %s", CommerceLocalizationContractVersion)
	}
	if item.SourceLanguage != resolution.SourceLanguage || item.TargetLanguage != resolution.TargetLanguage {
		return errors.New("localization language identity does not match the frozen resolution")
	}
	if len(item.Segments) != len(snapshot.SourceSegments) {
		return fmt.Errorf("localization segment count %d does not match source count %d", len(item.Segments), len(snapshot.SourceSegments))
	}
	for index, source := range snapshot.SourceSegments {
		segment := item.Segments[index]
		if segment.Ordinal != source.Ordinal || segment.SourceSegmentID != source.ID {
			return fmt.Errorf("localization segment %d does not preserve source identity", index+1)
		}
		if segment.SourceText != source.SourceText {
			return fmt.Errorf("localization segment %d changed sourceText", index+1)
		}
		if strings.TrimSpace(segment.LocalizedText) == "" {
			return fmt.Errorf("localization segment %d localizedText is empty", index+1)
		}
		if item.SourceLanguage == item.TargetLanguage {
			expectedVoiceover, expectedOnscreen := splitCommerceIdentityChannels(source.SourceText)
			if segment.LocalizedText != source.SourceText ||
				segment.VoiceoverText != expectedVoiceover ||
				segment.OnscreenText != expectedOnscreen {
				return fmt.Errorf("identity localization segment %d changed deterministic content channels", index+1)
			}
		}
		if containsExplicitAudioCue(segment.VoiceoverText) {
			return fmt.Errorf("localization segment %d voiceover contains an audio cue", index+1)
		}
	}
	return nil
}

func splitCommerceIdentityChannels(source string) (string, string) {
	source = strings.TrimSpace(source)
	voiceover := extractCommerceLabeledChannel(source, commerceSpeechMarkerPattern, commerceNonSpeechChannelPattern)
	onscreen := extractCommerceLabeledChannel(source, commerceOnscreenMarkerPattern, commerceAnyChannelPattern)
	if voiceover == "" && commerceSpeechMarkerPattern.FindStringIndex(source) == nil &&
		!commerceVisualOnlyPrefixPattern.MatchString(source) {
		voiceover = source
	}
	return voiceover, onscreen
}

func extractCommerceLabeledChannel(source string, marker, boundary *regexp.Regexp) string {
	location := marker.FindStringIndex(source)
	if location == nil {
		return ""
	}
	value := source[location[1]:]
	if next := boundary.FindStringIndex(value); next != nil {
		value = value[:next[0]]
	}
	return strings.TrimSpace(value)
}

func ParseCommerceLocalizationReview(raw string) (CommerceLocalizationReviewContract, error) {
	item, err := decodeCommerceContract[CommerceLocalizationReviewContract](raw)
	if err != nil {
		return item, fmt.Errorf("localization review JSON: %w", err)
	}
	return item, nil
}

func ValidateCommerceLocalizationReview(item CommerceLocalizationReviewContract, candidate CommerceLocalizationContract) error {
	if item.ContractVersion != CommerceReviewDecisionContractVersion {
		return fmt.Errorf("contractVersion must be %s", CommerceReviewDecisionContractVersion)
	}
	if err := validateReviewDecision(item.Decision, item.Issues); err != nil {
		return err
	}
	valid := make(map[string]struct{}, len(candidate.Segments))
	for _, segment := range candidate.Segments {
		valid[segment.SourceSegmentID] = struct{}{}
	}
	if err := validateCheckedIDs(item.CheckedSegmentIDs, valid, item.Decision == "approve"); err != nil {
		return fmt.Errorf("checkedSegmentIds: %w", err)
	}
	for _, issue := range item.Issues {
		if issue.SourceSegmentID != "" {
			if _, ok := valid[issue.SourceSegmentID]; !ok {
				return fmt.Errorf("review issue references unknown sourceSegmentId %s", issue.SourceSegmentID)
			}
		}
	}
	return nil
}

func ParseCommerceSalesScript(raw string) (CommerceSalesScriptContract, error) {
	item, err := decodeCommerceContract[CommerceSalesScriptContract](raw)
	if err != nil {
		return item, fmt.Errorf("sales script JSON: %w", err)
	}
	item.TargetLocale, err = canonicalCommerceLocale(item.TargetLocale)
	if err != nil {
		return item, fmt.Errorf("targetLocale: %w", err)
	}
	return item, nil
}

func ValidateCommerceSalesScript(item CommerceSalesScriptContract, snapshot CommerceStoryboardPlanningSnapshot) error {
	if item.ContractVersion != CommerceSalesScriptContractVersion {
		return fmt.Errorf("contractVersion must be %s", CommerceSalesScriptContractVersion)
	}
	if item.CommerceScriptUnitID != snapshot.Identity.ScriptUnitID || item.ScriptUnitGenerationID != snapshot.Identity.UnitGenerationID || item.ProductVersionID != snapshot.ProductVersionID {
		return errors.New("sales script identity does not match the frozen unit generation")
	}
	if item.TargetLocale != snapshot.TargetLocale || item.TargetDurationSeconds != snapshot.TargetDurationSeconds {
		return errors.New("sales script locale or duration does not match the frozen configuration")
	}
	if len(item.Segments) != len(snapshot.LocalizedSegments) {
		return errors.New("sales script must preserve every localization segment")
	}
	for index, localized := range snapshot.LocalizedSegments {
		segment := item.Segments[index]
		if segment.Ordinal != localized.Ordinal || segment.SourceSegmentID != localized.SourceSegmentID {
			return fmt.Errorf("sales script segment %d does not preserve localization identity", index+1)
		}
		if segment.VoiceoverText != localized.VoiceoverText || segment.OnscreenText != localized.OnscreenText {
			return fmt.Errorf("sales script segment %d changed approved voiceover or onscreen text", index+1)
		}
		if !validSalesBeat(segment.SalesBeat) || strings.TrimSpace(segment.VisualIntent) == "" {
			return fmt.Errorf("sales script segment %d has an invalid sales contract", index+1)
		}
		if audioCueLeaksIntoVoiceover(segment.VoiceoverText, segment.SoundEffects, segment.MusicCue) {
			return fmt.Errorf("sales script segment %d mixes audio cues into voiceover", index+1)
		}
	}
	return nil
}

func ParseCommerceStoryboardPlan(raw string) (CommerceStoryboardPlanContract, error) {
	item, err := decodeCommerceContract[CommerceStoryboardPlanContract](raw)
	if err != nil {
		return item, fmt.Errorf("storyboard plan JSON: %w", err)
	}
	item.TargetLocale, err = canonicalCommerceLocale(item.TargetLocale)
	if err != nil {
		return item, fmt.Errorf("targetLocale: %w", err)
	}
	return item, nil
}

func bindCommerceStoryboardPlanIdentity(
	snapshot CommerceStoryboardPlanningSnapshot,
	item CommerceStoryboardPlanContract,
) (CommerceStoryboardPlanContract, error) {
	fields := []struct {
		name     string
		actual   *string
		expected string
	}{
		{name: "commerceScriptUnitId", actual: &item.CommerceScriptUnitID, expected: snapshot.Identity.ScriptUnitID},
		{name: "scriptUnitGenerationId", actual: &item.ScriptUnitGenerationID, expected: snapshot.Identity.UnitGenerationID},
		{name: "commerceWorkflowBindingId", actual: &item.CommerceWorkflowBindingID, expected: snapshot.Identity.CommerceWorkflowBindingID},
		{name: "productVersionId", actual: &item.ProductVersionID, expected: snapshot.ProductVersionID},
	}
	for _, field := range fields {
		switch {
		case strings.TrimSpace(*field.actual) == "":
			*field.actual = field.expected
		case *field.actual != field.expected:
			return CommerceStoryboardPlanContract{}, fmt.Errorf("%s conflicts with the frozen unit generation", field.name)
		}
	}
	return item, nil
}

func ParseCommerceStoryboardReview(raw string) (CommerceStoryboardReviewContract, error) {
	item, err := decodeCommerceContract[CommerceStoryboardReviewContract](raw)
	if err != nil {
		return item, fmt.Errorf("storyboard review JSON: %w", err)
	}
	return item, nil
}

func ValidateCommerceStoryboardReview(item CommerceStoryboardReviewContract, plan CommerceStoryboardPlanContract) error {
	if item.ContractVersion != CommerceStoryboardReviewContractVersion {
		return fmt.Errorf("contractVersion must be %s", CommerceStoryboardReviewContractVersion)
	}
	if err := validateReviewDecision(item.Decision, item.Issues); err != nil {
		return err
	}
	valid := make(map[string]struct{}, len(plan.Shots))
	for _, shot := range plan.Shots {
		valid[shot.CandidateKey] = struct{}{}
	}
	if err := validateCheckedIDs(item.CheckedCandidateKeys, valid, item.Decision == "approve"); err != nil {
		return fmt.Errorf("checkedCandidateKeys: %w", err)
	}
	for _, issue := range item.Issues {
		if issue.CandidateKey != "" {
			if _, ok := valid[issue.CandidateKey]; !ok {
				return fmt.Errorf("review issue references unknown candidateKey %s", issue.CandidateKey)
			}
		}
	}
	if item.Decision == "approve" && (!item.SegmentCoverageComplete || item.DurationTotalSeconds != plan.TargetDurationSeconds) {
		return errors.New("approved storyboard review does not confirm coverage and duration")
	}
	return nil
}

func ParseCommerceImagePromptPlan(raw string) (CommerceImagePromptPlanContract, error) {
	item, err := decodeCommerceContract[CommerceImagePromptPlanContract](raw)
	if err != nil {
		return item, fmt.Errorf("image prompt plan JSON: %w", err)
	}
	return item, nil
}

func ValidateCommerceImagePromptPlan(item CommerceImagePromptPlanContract, snapshot CommerceReferenceImageShotSnapshot) error {
	if item.ContractVersion != CommerceImagePromptPlanContractVersion {
		return fmt.Errorf("contractVersion must be %s", CommerceImagePromptPlanContractVersion)
	}
	if item.CommerceScriptUnitID != snapshot.Identity.ScriptUnitID ||
		item.ScriptUnitGenerationID != snapshot.Identity.UnitGenerationID ||
		item.CommerceWorkflowBindingID != snapshot.Identity.CommerceWorkflowBindingID ||
		item.ProductVersionID != snapshot.ProductVersionID {
		return errors.New("image prompt plan identity does not match the frozen shot")
	}
	if strings.TrimSpace(item.VisualPrompt) == "" || strings.TrimSpace(item.NegativePrompt) == "" {
		return errors.New("visualPrompt and negativePrompt are required")
	}
	if item.TargetLanguage != snapshot.TargetLocale {
		return errors.New("image prompt targetLanguage does not match the frozen localization")
	}
	if item.AspectRatio != snapshot.AspectRatio {
		return errors.New("image prompt aspectRatio does not match the frozen project ratio")
	}
	if len(item.ReferenceIDs) < snapshot.MinimumReferences || len(item.ReferenceIDs) > snapshot.MaximumReferences {
		return fmt.Errorf("referenceIds count must be between %d and %d", snapshot.MinimumReferences, snapshot.MaximumReferences)
	}
	available := make(map[string]struct{}, len(snapshot.References))
	for _, reference := range snapshot.References {
		available[reference.ReferenceID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(item.ReferenceIDs))
	for _, referenceID := range item.ReferenceIDs {
		if _, ok := available[referenceID]; !ok {
			return fmt.Errorf("referenceId %s does not belong to the frozen reference pack", referenceID)
		}
		if _, duplicate := seen[referenceID]; duplicate {
			return fmt.Errorf("referenceId %s is duplicated", referenceID)
		}
		seen[referenceID] = struct{}{}
	}
	visual := strings.TrimSpace(item.VisualPrompt)
	for field, value := range map[string]string{
		"voiceoverText": snapshot.VoiceoverText,
		"onscreenText":  snapshot.OnscreenText,
		"musicCue":      snapshot.MusicCue,
	} {
		value = strings.TrimSpace(value)
		if utf8.RuneCountInString(value) >= 4 && strings.Contains(visual, value) {
			return fmt.Errorf("visualPrompt must not contain %s", field)
		}
	}
	for _, cue := range snapshot.SoundEffects {
		cue = strings.TrimSpace(cue)
		if utf8.RuneCountInString(cue) >= 2 && strings.Contains(visual, cue) {
			return errors.New("visualPrompt must not contain sound effects")
		}
	}
	if strings.TrimSpace(snapshot.OnscreenText) != "" && len(item.MustNotRenderText) == 0 {
		return errors.New("mustNotRenderText is required when the shot contains onscreen text")
	}
	return nil
}

func ParseCommerceImageFidelityReview(raw string) (CommerceImageFidelityReviewContract, error) {
	item, err := decodeCommerceContract[CommerceImageFidelityReviewContract](raw)
	if err != nil {
		return item, fmt.Errorf("image fidelity review JSON: %w", err)
	}
	return item, nil
}

func ValidateCommerceImageFidelityReview(item CommerceImageFidelityReviewContract) error {
	if item.ContractVersion != CommerceImageFidelityReviewContractVersion {
		return fmt.Errorf("contractVersion must be %s", CommerceImageFidelityReviewContractVersion)
	}
	if err := validateReviewDecision(item.Decision, item.Issues); err != nil {
		return err
	}
	if item.Decision == "approve" {
		checks := item.Checks
		if !checks.ProductIdentity || !checks.Packaging || !checks.Color || !checks.Shape ||
			!checks.ReferenceOwnership || !checks.NoForbiddenText || !checks.ShotAlignment {
			return errors.New("approved image fidelity review contains a failed check")
		}
	}
	return nil
}

func ParseCommerceVideoPromptPlan(raw string) (CommerceVideoPromptPlanContract, error) {
	item, err := decodeCommerceContract[CommerceVideoPromptPlanContract](raw)
	if err != nil {
		return item, fmt.Errorf("video prompt plan JSON: %w", err)
	}
	return item, nil
}

func ValidateCommerceVideoPromptPlan(item CommerceVideoPromptPlanContract, snapshot CommerceVideoPromptShotSnapshot) error {
	if item.ContractVersion != CommerceVideoPromptPlanContractVersion {
		return fmt.Errorf("contractVersion must be %s", CommerceVideoPromptPlanContractVersion)
	}
	if item.CommerceScriptUnitID != snapshot.Identity.ScriptUnitID ||
		item.ScriptUnitGenerationID != snapshot.Identity.UnitGenerationID ||
		item.CommerceWorkflowBindingID != snapshot.Identity.CommerceWorkflowBindingID ||
		item.ProductVersionID != snapshot.ProductVersionID {
		return errors.New("video prompt plan identity does not match the frozen shot")
	}
	if strings.TrimSpace(item.VisualPrompt) == "" {
		return errors.New("visualPrompt is required")
	}
	if item.SpokenLanguage != snapshot.TargetLocale {
		return errors.New("spokenLanguage does not match the frozen localization")
	}
	if !containsFold(snapshot.SupportedPromptLanguages, item.InstructionLanguage) {
		return errors.New("instructionLanguage is not supported by the frozen video capability")
	}
	if item.VoiceoverText != snapshot.VoiceoverText {
		return errors.New("voiceoverText must exactly match the linked localization segments")
	}
	if item.OnscreenText != snapshot.OnscreenText {
		return errors.New("onscreenText must exactly match the frozen shot overlay metadata")
	}
	if !sameTrimmedStrings(item.SoundEffects, snapshot.SoundEffects) || strings.TrimSpace(item.MusicCue) != strings.TrimSpace(snapshot.MusicCue) {
		return errors.New("sound effects or music cue do not match the frozen shot audio contract")
	}
	if item.ReferencePackID != snapshot.ReferencePackID || len(item.ReferenceIDs) != 1 || item.ReferenceIDs[0] != snapshot.FirstFrame.ImageVersionID {
		return errors.New("video prompt must use the approved commerce shot image as its only first frame")
	}
	if item.DurationSeconds != snapshot.DurationSeconds || !containsInt(snapshot.AllowedDurations, item.DurationSeconds) {
		return errors.New("durationSeconds does not match the frozen shot or approved model capability")
	}
	if !sameStrings(item.SourceSegmentIDs, snapshot.SourceSegmentIDs) {
		return errors.New("sourceSegmentIds do not match the frozen shot segment links")
	}
	if item.NativeAudioRequested != snapshot.NativeAudioRequested {
		return errors.New("nativeAudioRequested does not match the frozen audio strategy")
	}
	if item.NativeAudioRequested && !containsFold(snapshot.NativeAudioLanguages, snapshot.TargetLocale) {
		return errors.New("the frozen video capability does not approve native audio for the target locale")
	}
	visual := strings.TrimSpace(item.VisualPrompt)
	if overlay := strings.TrimSpace(item.OnscreenText); overlay != "" && strings.Contains(visual, overlay) {
		return errors.New("visualPrompt must not contain onscreenText")
	}
	if voiceover := strings.TrimSpace(item.VoiceoverText); utf8.RuneCountInString(voiceover) >= 4 && strings.Contains(visual, voiceover) {
		return errors.New("visualPrompt must not contain the verbatim voiceover")
	}
	for _, cue := range item.SoundEffects {
		cue = strings.TrimSpace(cue)
		if cue != "" && strings.Contains(item.VoiceoverText, cue) {
			return errors.New("voiceoverText must not contain sound effects")
		}
	}
	if music := strings.TrimSpace(item.MusicCue); music != "" && strings.Contains(item.VoiceoverText, music) {
		return errors.New("voiceoverText must not contain the music cue")
	}
	return nil
}

func ParseCommerceVideoPromptReview(raw string) (CommerceVideoPromptReviewContract, error) {
	item, err := decodeCommerceContract[CommerceVideoPromptReviewContract](raw)
	if err != nil {
		return item, fmt.Errorf("video prompt review JSON: %w", err)
	}
	return item, nil
}

func ValidateCommerceVideoPromptReview(item CommerceVideoPromptReviewContract) error {
	if item.ContractVersion != CommerceVideoPromptReviewContractVersion {
		return fmt.Errorf("contractVersion must be %s", CommerceVideoPromptReviewContractVersion)
	}
	if err := validateReviewDecision(item.Decision, item.Issues); err != nil {
		return err
	}
	if item.Decision == "approve" {
		checks := item.Checks
		if !checks.Identity || !checks.SingleFrameReachability || !checks.VerbatimVoiceover ||
			!checks.AudioSeparation || !checks.OverlaySeparation || !checks.ReferenceContract ||
			!checks.DurationCapability || !checks.NativeAudioLanguage {
			return errors.New("approved video prompt review contains a failed check")
		}
	}
	return nil
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sameTrimmedStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if strings.TrimSpace(left[index]) != strings.TrimSpace(right[index]) {
			return false
		}
	}
	return true
}

func AnalyzeCommerceTiming(localization CommerceLocalizationContract, policy CommerceTimingPolicy, targetDurationSeconds int) (CommerceTimingAnalysis, error) {
	if strings.TrimSpace(policy.Version) == "" || policy.NormalUnitsPerSecond <= 0 {
		return CommerceTimingAnalysis{}, errors.New("timing policy is invalid")
	}
	units := 0
	pause := 0.0
	for index, segment := range localization.Segments {
		voiceover := segment.VoiceoverText
		switch policy.Unit {
		case "han_character", "character", "mora", "syllable":
			for _, current := range voiceover {
				if unicode.IsLetter(current) || unicode.IsDigit(current) || unicode.Is(unicode.Han, current) || unicode.In(current, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
					units++
				}
				pause += commercePunctuationPause(current, policy)
			}
		case "word":
			units += len(strings.Fields(voiceover))
			for _, current := range voiceover {
				pause += commercePunctuationPause(current, policy)
			}
		default:
			return CommerceTimingAnalysis{}, fmt.Errorf("unsupported timing unit %q", policy.Unit)
		}
		if index > 0 {
			pause += policy.SegmentGapSeconds
		}
	}
	speech := float64(units) / policy.NormalUnitsPerSecond
	total := speech + pause
	return CommerceTimingAnalysis{
		Locale: localization.TargetLanguage, PolicyVersion: policy.Version, Unit: policy.Unit,
		Units: units, SpeechSeconds: speech, PauseSeconds: pause, EstimatedVoiceoverSeconds: total,
		TargetDurationSeconds: targetDurationSeconds, AllowedOverrunSeconds: policy.AllowedOverrunSeconds,
		Exceeded: total > float64(targetDurationSeconds)+policy.AllowedOverrunSeconds,
	}, nil
}

func BuildCommerceStoryboardProjection(snapshot CommerceStoryboardPlanningSnapshot, plan CommerceStoryboardPlanContract) (CommerceStoryboardProjection, error) {
	if err := validateCommerceStoryboardPlanShape(snapshot, plan); err != nil {
		return CommerceStoryboardProjection{}, err
	}
	segmentBySource := make(map[string]CommerceLocalizedSegmentSnapshot, len(snapshot.LocalizedSegments))
	segmentOrder := make(map[string]int, len(snapshot.LocalizedSegments))
	segmentCursor := make(map[string]int, len(snapshot.LocalizedSegments))
	segmentEnd := make(map[string]int, len(snapshot.LocalizedSegments))
	segmentCovered := make(map[string]bool, len(snapshot.LocalizedSegments))
	for _, segment := range snapshot.LocalizedSegments {
		segmentBySource[segment.SourceSegmentID] = segment
		segmentOrder[segment.SourceSegmentID] = segment.Ordinal
		start, end := trimmedRuneRange(segment.VoiceoverText)
		segmentCursor[segment.SourceSegmentID] = start
		segmentEnd[segment.SourceSegmentID] = end
	}
	referenceByID := make(map[string]CommerceProductReferenceSnapshot, len(snapshot.ProductReferences))
	for _, reference := range snapshot.ProductReferences {
		referenceByID[reference.ReferenceID] = reference
	}
	projection := CommerceStoryboardProjection{
		Identity: snapshot.Identity, InputHash: snapshot.InputHash, ProductVersionID: snapshot.ProductVersionID,
		SourceScriptVersionID: snapshot.SourceScriptVersionID, LocalizationID: snapshot.LocalizationID,
		ReferencePackID: snapshot.ReferencePackID, TargetLocale: snapshot.TargetLocale,
		TargetDurationSeconds: snapshot.TargetDurationSeconds, Shots: make([]CommerceStoryboardShotProjection, 0, len(plan.Shots)),
	}
	startSeconds := 0
	lastVoiceoverSegmentOrdinal := 0
	for _, shot := range plan.Shots {
		links, updatedOrdinal, err := projectCommerceShotSegmentLinks(
			snapshot.LocalizationID, shot, segmentBySource, segmentOrder, segmentCursor, segmentEnd,
			segmentCovered, lastVoiceoverSegmentOrdinal,
		)
		if err != nil {
			return CommerceStoryboardProjection{}, fmt.Errorf("shot %s: %w", shot.CandidateKey, err)
		}
		lastVoiceoverSegmentOrdinal = updatedOrdinal
		references := make([]CommerceShotProductReferenceProjection, 0, len(shot.ProductReferenceIDs))
		for index, referenceID := range shot.ProductReferenceIDs {
			reference := referenceByID[referenceID]
			references = append(references, CommerceShotProductReferenceProjection{
				CandidateKey: shot.CandidateKey, ProductReferenceID: reference.ReferenceID,
				SourcePackID: snapshot.ReferencePackID, SourcePackItemID: reference.PackItemID,
				Role: normalizeCommerceReferenceRole(reference.Role), Ordinal: index, Required: reference.Required,
			})
		}
		contractHash, err := commerceContractHash(map[string]any{
			"identity": snapshot.Identity, "productVersionId": snapshot.ProductVersionID,
			"localizationId": snapshot.LocalizationID, "referencePackId": snapshot.ReferencePackID,
			"targetLocale": snapshot.TargetLocale, "shot": shot, "segmentLinks": links, "productReferences": references,
		})
		if err != nil {
			return CommerceStoryboardProjection{}, err
		}
		projection.Shots = append(projection.Shots, CommerceStoryboardShotProjection{
			CandidateKey: shot.CandidateKey, ShotOrdinal: shot.ShotOrdinal, StartSeconds: startSeconds,
			DurationSeconds: shot.DurationSeconds, Contract: shot, ContractHash: contractHash,
			SegmentLinks: links, ProductReferences: references,
		})
		startSeconds += shot.DurationSeconds
	}
	for _, segment := range snapshot.LocalizedSegments {
		if !segment.Required {
			continue
		}
		if !segmentCovered[segment.SourceSegmentID] {
			return CommerceStoryboardProjection{}, fmt.Errorf("required segment %s is not covered", segment.SourceSegmentID)
		}
		if strings.TrimSpace(segment.VoiceoverText) != "" && segmentCursor[segment.SourceSegmentID] != segmentEnd[segment.SourceSegmentID] {
			return CommerceStoryboardProjection{}, fmt.Errorf("required segment %s voiceover is not fully covered", segment.SourceSegmentID)
		}
	}
	planHash, err := commerceContractHash(map[string]any{
		"identity": snapshot.Identity, "inputHash": snapshot.InputHash, "plan": plan, "shots": projection.Shots,
	})
	if err != nil {
		return CommerceStoryboardProjection{}, err
	}
	projection.PlanHash = planHash
	return projection, nil
}

func validateCommerceStoryboardPlanShape(snapshot CommerceStoryboardPlanningSnapshot, plan CommerceStoryboardPlanContract) error {
	if plan.ContractVersion != CommerceStoryboardPlanContractVersion {
		return fmt.Errorf("contractVersion must be %s", CommerceStoryboardPlanContractVersion)
	}
	if plan.CommerceScriptUnitID != snapshot.Identity.ScriptUnitID || plan.ScriptUnitGenerationID != snapshot.Identity.UnitGenerationID ||
		plan.CommerceWorkflowBindingID != snapshot.Identity.CommerceWorkflowBindingID || plan.ProductVersionID != snapshot.ProductVersionID {
		return errors.New("storyboard identity does not match the frozen unit generation")
	}
	if plan.TargetLocale != snapshot.TargetLocale || plan.TargetDurationSeconds != snapshot.TargetDurationSeconds {
		return errors.New("storyboard locale or duration does not match the frozen configuration")
	}
	if len(plan.Shots) == 0 {
		return errors.New("storyboard must contain at least one shot")
	}
	segments := make(map[string]CommerceLocalizedSegmentSnapshot, len(snapshot.LocalizedSegments))
	for _, segment := range snapshot.LocalizedSegments {
		segments[segment.SourceSegmentID] = segment
	}
	references := make(map[string]struct{}, len(snapshot.ProductReferences))
	for _, reference := range snapshot.ProductReferences {
		references[reference.ReferenceID] = struct{}{}
	}
	allowedDurations := make(map[int]struct{}, len(snapshot.AllowedShotDurations))
	for _, duration := range snapshot.AllowedShotDurations {
		allowedDurations[duration] = struct{}{}
	}
	seenCandidates := make(map[string]struct{}, len(plan.Shots))
	total := 0
	for index, shot := range plan.Shots {
		if strings.TrimSpace(shot.CandidateKey) == "" {
			return fmt.Errorf("shot %d candidateKey is required", index+1)
		}
		if _, exists := seenCandidates[shot.CandidateKey]; exists {
			return fmt.Errorf("candidateKey %s is duplicated", shot.CandidateKey)
		}
		seenCandidates[shot.CandidateKey] = struct{}{}
		if shot.ShotOrdinal != index+1 {
			return fmt.Errorf("shot ordinals must be contiguous from 1")
		}
		if shot.DurationSeconds <= 0 {
			return fmt.Errorf("shot %s duration must be a positive integer", shot.CandidateKey)
		}
		if len(allowedDurations) > 0 {
			if _, ok := allowedDurations[shot.DurationSeconds]; !ok {
				return fmt.Errorf("shot %s duration %d is not allowed by the frozen video capability", shot.CandidateKey, shot.DurationSeconds)
			}
		}
		if strings.TrimSpace(shot.SalesBeat) == "" || strings.TrimSpace(shot.ShotPurpose) == "" || strings.TrimSpace(shot.VisualAction) == "" || strings.TrimSpace(shot.Composition) == "" {
			return fmt.Errorf("shot %s visual contract is incomplete", shot.CandidateKey)
		}
		if err := validateJSONObjectRaw(shot.Camera); err != nil {
			return fmt.Errorf("shot %s camera: %w", shot.CandidateKey, err)
		}
		if len(shot.SourceSegmentIDs) == 0 {
			return fmt.Errorf("shot %s has no source segments", shot.CandidateKey)
		}
		seenSegments := make(map[string]struct{}, len(shot.SourceSegmentIDs))
		for _, segmentID := range shot.SourceSegmentIDs {
			if _, ok := segments[segmentID]; !ok {
				return fmt.Errorf("shot %s references unknown source segment %s", shot.CandidateKey, segmentID)
			}
			if _, duplicate := seenSegments[segmentID]; duplicate {
				return fmt.Errorf("shot %s repeats source segment %s", shot.CandidateKey, segmentID)
			}
			seenSegments[segmentID] = struct{}{}
		}
		if len(shot.ProductReferenceIDs) == 0 {
			return fmt.Errorf("shot %s has no product reference", shot.CandidateKey)
		}
		seenReferences := make(map[string]struct{}, len(shot.ProductReferenceIDs))
		for _, referenceID := range shot.ProductReferenceIDs {
			if _, ok := references[referenceID]; !ok {
				return fmt.Errorf("shot %s references product image %s outside the frozen pack", shot.CandidateKey, referenceID)
			}
			if _, duplicate := seenReferences[referenceID]; duplicate {
				return fmt.Errorf("shot %s repeats product reference %s", shot.CandidateKey, referenceID)
			}
			seenReferences[referenceID] = struct{}{}
		}
		if audioCueLeaksIntoVoiceover(shot.VoiceoverText, shot.SoundEffects, shot.MusicCue) {
			return fmt.Errorf("shot %s mixes sound effects or music into voiceover", shot.CandidateKey)
		}
		total += shot.DurationSeconds
	}
	if total != snapshot.TargetDurationSeconds {
		return fmt.Errorf("storyboard duration %d does not equal target duration %d", total, snapshot.TargetDurationSeconds)
	}
	return nil
}

func projectCommerceShotSegmentLinks(
	localizationID string,
	shot CommerceStoryboardShotContract,
	segments map[string]CommerceLocalizedSegmentSnapshot,
	segmentOrder map[string]int,
	cursors map[string]int,
	ends map[string]int,
	covered map[string]bool,
	lastVoiceoverOrdinal int,
) ([]CommerceShotSegmentProjection, int, error) {
	remaining := []rune(strings.TrimSpace(shot.VoiceoverText))
	links := make([]CommerceShotSegmentProjection, 0, len(shot.SourceSegmentIDs))
	linked := make(map[string]bool, len(shot.SourceSegmentIDs))
	voiceoverOrdinal := 0
	for _, sourceID := range shot.SourceSegmentIDs {
		segment := segments[sourceID]
		start, end := cursors[sourceID], ends[sourceID]
		if len(remaining) == 0 || start >= end {
			continue
		}
		segmentRunes := []rune(segment.VoiceoverText)
		available := segmentRunes[start:end]
		consumed := 0
		switch {
		case strings.HasPrefix(string(remaining), string(available)):
			consumed = len(available)
			remaining = trimLeadingCommerceSpaces(remaining[consumed:])
		case strings.HasPrefix(string(available), string(remaining)):
			consumed = len(remaining)
			remaining = nil
		default:
			continue
		}
		ordinal := segmentOrder[sourceID]
		if ordinal < lastVoiceoverOrdinal {
			return nil, lastVoiceoverOrdinal, errors.New("voiceover segments are reordered")
		}
		lastVoiceoverOrdinal = ordinal
		from, to := start, start+consumed
		voiceoverOrdinal++
		links = append(links, CommerceShotSegmentProjection{
			CandidateKey: shot.CandidateKey, LocalizationID: localizationID,
			LocalizationSegmentID: segment.ID, SourceSegmentID: sourceID,
			Usage: "voiceover", Ordinal: voiceoverOrdinal - 1,
			VerbatimStart: commerceIntPointer(from), VerbatimEnd: commerceIntPointer(to),
		})
		cursors[sourceID] = to
		covered[sourceID] = true
		linked[sourceID] = true
	}
	if len(remaining) != 0 {
		return nil, lastVoiceoverOrdinal, fmt.Errorf("voiceover cannot be reconstructed from linked localization segments: %q", string(remaining))
	}
	contextOrdinal := 0
	for _, sourceID := range shot.SourceSegmentIDs {
		if linked[sourceID] {
			continue
		}
		segment := segments[sourceID]
		links = append(links, CommerceShotSegmentProjection{
			CandidateKey: shot.CandidateKey, LocalizationID: localizationID,
			LocalizationSegmentID: segment.ID, SourceSegmentID: sourceID,
			Usage: "context", Ordinal: contextOrdinal,
		})
		contextOrdinal++
		covered[sourceID] = true
	}
	return links, lastVoiceoverOrdinal, nil
}

func ValidateCommercePreparationSnapshot(inputIdentity commerce.ScriptUnitPreparationIdentity, snapshot CommerceScriptUnitPreparationSnapshot) error {
	if err := ValidateCommerceScriptUnitPreparationIdentity(inputIdentity); err != nil {
		return err
	}
	if snapshot.Identity != inputIdentity {
		return errors.New("preparation snapshot identity does not match workflow input")
	}
	if !validCommerceHash(snapshot.InputHash) || !validCommerceHash(snapshot.WorkflowTemplateContentHash) {
		return errors.New("preparation snapshot hash is invalid")
	}
	for _, id := range []string{snapshot.WorkflowTemplateVersionID, snapshot.ProductVersionID, snapshot.SourceScriptVersionID, snapshot.ReferencePackID} {
		if err := validateCommerceUUID(id); err != nil {
			return err
		}
	}
	if snapshot.ProductVersionID != inputIdentity.ProductVersionID ||
		snapshot.SourceScriptVersionID != inputIdentity.SourceScriptVersionID ||
		snapshot.ReferencePackID != inputIdentity.ReferencePackID {
		return errors.New("preparation snapshot frozen resources do not match workflow input")
	}
	if snapshot.TargetDurationSeconds <= 0 || len(snapshot.SourceSegments) == 0 {
		return errors.New("preparation snapshot has no script or target duration")
	}
	if _, err := canonicalLocaleSet(snapshot.AllowedLocales); err != nil {
		return err
	}
	if snapshot.LanguageConfidenceThreshold <= 0 || snapshot.LanguageConfidenceThreshold > 1 {
		return errors.New("language confidence threshold is invalid")
	}
	if err := validateCommerceSourceSegments(snapshot.SourceSegments); err != nil {
		return err
	}
	if err := validateJSONObjectRaw(snapshot.ProductFacts); err != nil {
		return fmt.Errorf("product facts: %w", err)
	}
	for _, binding := range []CommerceAgentBinding{snapshot.Bindings.LanguageResolver, snapshot.Bindings.ScriptLocalizer, snapshot.Bindings.LocalizationReviewer} {
		if err := ValidateCommerceAgentBinding(binding); err != nil {
			return err
		}
	}
	return nil
}

func ValidateCommerceScriptUnitPreparationIdentity(identity commerce.ScriptUnitPreparationIdentity) error {
	if err := ValidateCommerceExecutionIdentity(identity.ExecutionIdentity); err != nil {
		return err
	}
	for _, id := range []string{
		identity.ProductID, identity.ProductVersionID, identity.ScriptUnitID,
		identity.SourceScriptVersionID, identity.ReferencePackID,
	} {
		if err := validateCommerceUUID(id); err != nil {
			return err
		}
	}
	if identity.ScriptUnitRevision <= 0 {
		return errors.New("commerce script unit preparation revision must be positive")
	}
	for _, hash := range []string{
		identity.ProductFactsHash, identity.SourceScriptContentHash, identity.ReferencePackHash,
	} {
		if !validCommerceHash(hash) {
			return errors.New("commerce script unit preparation hash is invalid")
		}
	}
	rebuildFields := []string{identity.RebuildID, identity.SourceUnitGenerationID, identity.TargetConfigurationHash}
	rebuildValues := 0
	for _, value := range rebuildFields {
		if strings.TrimSpace(value) != "" {
			rebuildValues++
		}
	}
	if rebuildValues != 0 && rebuildValues != len(rebuildFields) {
		return errors.New("commerce script unit rebuild identity is incomplete")
	}
	if rebuildValues == len(rebuildFields) {
		if err := validateCommerceUUID(identity.RebuildID); err != nil {
			return err
		}
		if err := validateCommerceUUID(identity.SourceUnitGenerationID); err != nil {
			return err
		}
		if !validCommerceHash(identity.TargetConfigurationHash) {
			return errors.New("commerce script unit rebuild configuration hash is invalid")
		}
	}
	return nil
}

func ValidateCommercePreparationCommitIdentity(
	preparation commerce.ScriptUnitPreparationIdentity,
	generation commerce.UnitGenerationIdentity,
) error {
	if err := ValidateCommerceScriptUnitPreparationIdentity(preparation); err != nil {
		return err
	}
	if err := ValidateCommerceUnitGenerationIdentity(generation); err != nil {
		return err
	}
	if generation.ExecutionIdentity != preparation.ExecutionIdentity ||
		generation.ProductID != preparation.ProductID ||
		generation.ScriptUnitID != preparation.ScriptUnitID ||
		generation.ScriptUnitRevision != preparation.ScriptUnitRevision+1 {
		return errors.New("committed unit generation does not derive from the preparation identity")
	}
	return nil
}

func ValidateCommerceExecutionIdentity(identity commerce.ExecutionIdentity) error {
	for _, id := range []string{
		identity.OrganizationID, identity.ProjectID, identity.ProjectGenerationID,
		identity.VideoProductionBindingID, identity.CommerceWorkflowBindingID,
	} {
		if err := validateCommerceUUID(id); err != nil {
			return err
		}
	}
	if identity.VideoProductionBindingRevision <= 0 || identity.CommerceWorkflowBindingRevision <= 0 {
		return errors.New("commerce execution binding revisions must be positive")
	}
	for _, hash := range []string{identity.VideoProfileSnapshotHash, identity.CommerceConfigurationHash} {
		if !validCommerceHash(hash) {
			return errors.New("commerce execution identity hash is invalid")
		}
	}
	return nil
}

func ValidateCommerceStoryboardSnapshot(inputIdentity commerce.UnitGenerationIdentity, snapshot CommerceStoryboardPlanningSnapshot) error {
	if err := ValidateCommerceUnitGenerationIdentity(inputIdentity); err != nil {
		return err
	}
	if snapshot.Identity != inputIdentity {
		return errors.New("storyboard snapshot identity does not match workflow input")
	}
	if !validCommerceHash(snapshot.InputHash) || !validCommerceHash(snapshot.LocalizedContentHash) || !validCommerceHash(snapshot.LocalizedContractHash) {
		return errors.New("storyboard snapshot hash is invalid")
	}
	for _, id := range []string{snapshot.ProductVersionID, snapshot.SourceScriptVersionID, snapshot.LocalizationID, snapshot.ReferencePackID} {
		if err := validateCommerceUUID(id); err != nil {
			return err
		}
	}
	locale, err := canonicalCommerceLocale(snapshot.TargetLocale)
	if err != nil || locale != snapshot.TargetLocale {
		return errors.New("storyboard target locale is not canonical")
	}
	if snapshot.TargetDurationSeconds <= 0 || strings.TrimSpace(snapshot.AspectRatio) == "" || snapshot.TimelineTimebase <= 0 || snapshot.FPSNumerator <= 0 || snapshot.FPSDenominator <= 0 {
		return errors.New("storyboard timing or aspect configuration is invalid")
	}
	if (snapshot.TimelineTimebase*int64(snapshot.FPSDenominator))%int64(snapshot.FPSNumerator) != 0 {
		return errors.New("storyboard timebase is not frame aligned")
	}
	if len(snapshot.LocalizedSegments) == 0 || len(snapshot.ProductReferences) == 0 {
		return errors.New("storyboard snapshot requires localized segments and product references")
	}
	if err := validateCommerceLocalizedSegments(snapshot.LocalizedSegments); err != nil {
		return err
	}
	if err := validateCommerceProductReferences(snapshot.ProductReferences); err != nil {
		return err
	}
	if err := validateJSONObjectRaw(snapshot.ProductFacts); err != nil {
		return fmt.Errorf("product facts: %w", err)
	}
	if err := validateJSONObjectRaw(snapshot.LocalizationContract); err != nil {
		return fmt.Errorf("localization contract: %w", err)
	}
	for _, binding := range []CommerceAgentBinding{snapshot.Bindings.ScriptOrganizer, snapshot.Bindings.StoryboardPlanner, snapshot.Bindings.StoryboardReviewer} {
		if err := ValidateCommerceAgentBinding(binding); err != nil {
			return err
		}
	}
	return nil
}

func ValidateCommerceUnitGenerationIdentity(identity commerce.UnitGenerationIdentity) error {
	if err := ValidateCommerceExecutionIdentity(identity.ExecutionIdentity); err != nil {
		return err
	}
	ids := []string{identity.ProductID, identity.ScriptUnitID, identity.UnitGenerationID}
	for _, id := range ids {
		if err := validateCommerceUUID(id); err != nil {
			return err
		}
	}
	if identity.ScriptUnitRevision <= 0 || identity.UnitGenerationNo <= 0 {
		return errors.New("commerce generation revisions must be positive")
	}
	for _, hash := range []string{identity.UnitConfigurationHash} {
		if !validCommerceHash(hash) {
			return errors.New("commerce generation hash is invalid")
		}
	}
	return nil
}

func ValidateCommerceAgentBinding(binding CommerceAgentBinding) error {
	if strings.TrimSpace(binding.Role) == "" || strings.TrimSpace(binding.TemplateKey) == "" || strings.TrimSpace(binding.ModelProfileKey) == "" {
		return errors.New("commerce agent binding is incomplete")
	}
	if err := validateCommerceUUID(binding.PromptVersionID); err != nil {
		return fmt.Errorf("agent %s prompt version: %w", binding.Role, err)
	}
	if err := validateCommerceUUID(binding.ProviderModelID); err != nil {
		return fmt.Errorf("agent %s provider model: %w", binding.Role, err)
	}
	if !validCommercePromptHash(binding.PromptContentHash) {
		return fmt.Errorf("agent %s prompt content hash is invalid", binding.Role)
	}
	if binding.MaxReviewRounds < 1 || binding.MaxReviewRounds > CommerceMaxAgentReviewRounds {
		return fmt.Errorf("agent %s review rounds must be between 1 and %d", binding.Role, CommerceMaxAgentReviewRounds)
	}
	return nil
}

func commerceReviewRounds(bindings ...CommerceAgentBinding) int {
	limit := CommerceMaxAgentReviewRounds
	for _, binding := range bindings {
		if binding.MaxReviewRounds > 0 && binding.MaxReviewRounds < limit {
			limit = binding.MaxReviewRounds
		}
	}
	if limit < 1 {
		return 1
	}
	return limit
}

func decodeCommerceContract[T any](raw string) (T, error) {
	var item T
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(stripJSONFence(raw))))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&item); err != nil {
		return item, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return item, errors.New("multiple JSON values are not allowed")
		}
		return item, err
	}
	return item, nil
}

func validateReviewDecision(decision string, issues []CommerceReviewIssue) error {
	if decision != "approve" && decision != "revise" && decision != "reject" {
		return errors.New("review decision must be approve, revise, or reject")
	}
	if decision == "approve" && len(issues) != 0 {
		return errors.New("approved review cannot contain issues")
	}
	if decision != "approve" && len(issues) == 0 {
		return errors.New("rejected review must contain structured issues")
	}
	for index, issue := range issues {
		if strings.TrimSpace(issue.Code) == "" || strings.TrimSpace(issue.Field) == "" || strings.TrimSpace(issue.Message) == "" || strings.TrimSpace(issue.Suggestion) == "" {
			return fmt.Errorf("review issue %d is incomplete", index+1)
		}
	}
	return nil
}

func validateCheckedIDs(ids []string, valid map[string]struct{}, requireComplete bool) error {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := valid[id]; !ok {
			return fmt.Errorf("unknown identity %s", id)
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("duplicate identity %s", id)
		}
		seen[id] = struct{}{}
	}
	if requireComplete && len(seen) != len(valid) {
		return errors.New("approved review did not check every identity")
	}
	return nil
}

func validateLanguageIssues(issues []CommerceLanguageIssue) error {
	for index, issue := range issues {
		if strings.TrimSpace(issue.Code) == "" || strings.TrimSpace(issue.Message) == "" {
			return fmt.Errorf("language issue %d is incomplete", index+1)
		}
	}
	return nil
}

func validateCommerceSourceSegments(items []CommerceSourceSegmentSnapshot) error {
	seen := make(map[string]struct{}, len(items))
	for index, item := range items {
		if err := validateCommerceUUID(item.ID); err != nil {
			return err
		}
		if item.Ordinal != index+1 || strings.TrimSpace(item.SourceText) == "" || !validCommerceHash(item.ContentHash) {
			return fmt.Errorf("source segment %d is invalid", index+1)
		}
		if _, duplicate := seen[item.ID]; duplicate {
			return fmt.Errorf("source segment %s is duplicated", item.ID)
		}
		seen[item.ID] = struct{}{}
	}
	return nil
}

func validateCommerceLocalizedSegments(items []CommerceLocalizedSegmentSnapshot) error {
	seenIDs := make(map[string]struct{}, len(items))
	seenSourceIDs := make(map[string]struct{}, len(items))
	for index, item := range items {
		if err := validateCommerceUUID(item.ID); err != nil {
			return err
		}
		if err := validateCommerceUUID(item.SourceSegmentID); err != nil {
			return err
		}
		if item.Ordinal != index+1 || strings.TrimSpace(item.LocalizedText) == "" {
			return fmt.Errorf("localized segment %d is invalid", index+1)
		}
		if _, duplicate := seenIDs[item.ID]; duplicate {
			return fmt.Errorf("localization segment %s is duplicated", item.ID)
		}
		if _, duplicate := seenSourceIDs[item.SourceSegmentID]; duplicate {
			return fmt.Errorf("source segment %s is duplicated in localization", item.SourceSegmentID)
		}
		seenIDs[item.ID] = struct{}{}
		seenSourceIDs[item.SourceSegmentID] = struct{}{}
	}
	return nil
}

func validateCommerceProductReferences(items []CommerceProductReferenceSnapshot) error {
	seenReferences := make(map[string]struct{}, len(items))
	seenPackItems := make(map[string]struct{}, len(items))
	for _, item := range items {
		if err := validateCommerceUUID(item.ReferenceID); err != nil {
			return err
		}
		if err := validateCommerceUUID(item.PackItemID); err != nil {
			return err
		}
		if !validCommerceHash(item.ContentHash) {
			return errors.New("product reference content hash is invalid")
		}
		if _, duplicate := seenReferences[item.ReferenceID]; duplicate {
			return fmt.Errorf("product reference %s is duplicated", item.ReferenceID)
		}
		if _, duplicate := seenPackItems[item.PackItemID]; duplicate {
			return fmt.Errorf("product reference pack item %s is duplicated", item.PackItemID)
		}
		seenReferences[item.ReferenceID] = struct{}{}
		seenPackItems[item.PackItemID] = struct{}{}
	}
	return nil
}

func canonicalLocaleSet(locales []string) (map[string]struct{}, error) {
	if len(locales) == 0 {
		return nil, errors.New("frozen workflow template has no supported locales")
	}
	result := make(map[string]struct{}, len(locales))
	for _, value := range locales {
		locale, err := canonicalCommerceLocale(value)
		if err != nil {
			return nil, err
		}
		if _, duplicate := result[locale]; duplicate {
			return nil, fmt.Errorf("locale %s is duplicated", locale)
		}
		result[locale] = struct{}{}
	}
	return result, nil
}

func canonicalCommerceLocale(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !commerceLocalePattern.MatchString(value) {
		return "", fmt.Errorf("locale %q is not a supported BCP 47 form", value)
	}
	parts := strings.Split(value, "-")
	parts[0] = strings.ToLower(parts[0])
	for index := 1; index < len(parts); index++ {
		switch {
		case len(parts[index]) == 2 && allCommerceLetters(parts[index]):
			parts[index] = strings.ToUpper(parts[index])
		case len(parts[index]) == 4 && allCommerceLetters(parts[index]):
			parts[index] = strings.ToUpper(parts[index][:1]) + strings.ToLower(parts[index][1:])
		default:
			parts[index] = strings.ToLower(parts[index])
		}
	}
	return strings.Join(parts, "-"), nil
}

func allCommerceLetters(value string) bool {
	for _, current := range value {
		if !unicode.IsLetter(current) {
			return false
		}
	}
	return value != ""
}

func validSalesBeat(value string) bool {
	switch value {
	case "hook", "pain_point", "feature", "demonstration", "proof", "cta":
		return true
	default:
		return false
	}
}

func validateJSONObjectRaw(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("JSON object is required")
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return err
	}
	if object == nil {
		return errors.New("JSON object cannot be null")
	}
	return nil
}

func validateCommerceUUID(value string) error {
	if _, err := uuid.Parse(strings.TrimSpace(value)); err != nil {
		return fmt.Errorf("invalid commerce identity %q", value)
	}
	return nil
}

func validCommerceHash(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validCommercePromptHash(value string) bool {
	return validCommerceHash(strings.TrimPrefix(strings.TrimSpace(value), "sha256:"))
}

func commerceContractHash(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	var canonical any
	if err := json.Unmarshal(raw, &canonical); err != nil {
		return "", err
	}
	raw, err = json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func commercePunctuationPause(current rune, policy CommerceTimingPolicy) float64 {
	switch current {
	case ',', '，', '、', ';', '；', ':', '：':
		return policy.CommaPauseSeconds
	case '.', '。', '!', '！', '?', '？':
		return policy.SentencePauseSeconds
	default:
		return 0
	}
}

func containsExplicitAudioCue(value string) bool {
	normalized := strings.ToLower(value)
	for _, marker := range []string{"[音效", "【音效", "（音效", "(音效", "sfx:", "sfx：", "bgm:", "bgm："} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func audioCueLeaksIntoVoiceover(voiceover string, soundEffects []string, musicCue string) bool {
	if containsExplicitAudioCue(voiceover) {
		return true
	}
	normalizedVoiceover := strings.ToLower(strings.TrimSpace(voiceover))
	for _, cue := range append(append([]string{}, soundEffects...), musicCue) {
		cue = strings.ToLower(strings.TrimSpace(cue))
		if cue != "" && utf8.RuneCountInString(cue) >= 2 && strings.Contains(normalizedVoiceover, cue) {
			return true
		}
	}
	return false
}

func normalizeCommerceReferenceRole(value string) string {
	switch strings.TrimSpace(value) {
	case "primary", "detail", "logo", "usage", "context":
		return strings.TrimSpace(value)
	default:
		return "context"
	}
}

func trimmedRuneRange(value string) (int, int) {
	runes := []rune(value)
	start := 0
	for start < len(runes) && unicode.IsSpace(runes[start]) {
		start++
	}
	end := len(runes)
	for end > start && unicode.IsSpace(runes[end-1]) {
		end--
	}
	return start, end
}

func trimLeadingCommerceSpaces(value []rune) []rune {
	for len(value) > 0 && unicode.IsSpace(value[0]) {
		value = value[1:]
	}
	return value
}

func commerceIntPointer(value int) *int { return &value }

func sortedCommerceIssueCopy(issues []CommerceReviewIssue) []CommerceReviewIssue {
	result := append([]CommerceReviewIssue(nil), issues...)
	sort.SliceStable(result, func(i, j int) bool {
		left := result[i].Code + "\x00" + result[i].Field + "\x00" + result[i].SourceSegmentID + "\x00" + result[i].CandidateKey
		right := result[j].Code + "\x00" + result[j].Field + "\x00" + result[j].SourceSegmentID + "\x00" + result[j].CandidateKey
		return left < right
	})
	return result
}
