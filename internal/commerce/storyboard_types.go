package commerce

import (
	"encoding/json"
	"time"
)

type StoryboardPlan struct {
	ID                         string             `json:"id"`
	OrganizationID             string             `json:"organizationId"`
	ProjectID                  string             `json:"projectId"`
	ProductID                  string             `json:"productId"`
	ProductVersionID           string             `json:"productVersionId"`
	ScriptUnitID               string             `json:"scriptUnitId"`
	SourceScriptVersionID      string             `json:"sourceScriptVersionId"`
	LocalizationID             string             `json:"localizationId"`
	ReferencePackID            string             `json:"referencePackId"`
	ProjectGenerationID        string             `json:"projectProductionGenerationId"`
	UnitGenerationID           string             `json:"scriptUnitGenerationId"`
	CommerceWorkflowBindingID  string             `json:"commerceWorkflowBindingId"`
	CommerceBindingRevision    int64              `json:"commerceWorkflowBindingRevision"`
	SalesScriptContractID      string             `json:"salesScriptContractId"`
	SalesScriptContractHash    string             `json:"salesScriptContractHash"`
	WorkflowRunID              *string            `json:"workflowRunId,omitempty"`
	PlanRevision               int                `json:"planRevision"`
	EditRevision               int64              `json:"revision"`
	Status                     string             `json:"status"`
	Active                     bool               `json:"active"`
	StaleState                 string             `json:"staleState"`
	TargetLanguage             string             `json:"targetLanguage"`
	TargetDurationSeconds      int                `json:"targetDurationSeconds"`
	AspectRatio                string             `json:"aspectRatio"`
	TimelineTimebase           int64              `json:"timelineTimebase"`
	FPSNumerator               int                `json:"fpsNumerator"`
	FPSDenominator             int                `json:"fpsDenominator"`
	AllowedShotDurations       []int              `json:"allowedShotDurations"`
	StoryboardStrategy         StoryboardStrategy `json:"storyboardStrategy,omitempty"`
	SegmentationPolicyVersion  string             `json:"segmentationPolicyVersion,omitempty"`
	SegmentationPlan           json.RawMessage    `json:"segmentationPlan,omitempty"`
	SegmentationPlanHash       string             `json:"segmentationPlanHash,omitempty"`
	VideoExecutionEnvelope     json.RawMessage    `json:"videoExecutionEnvelope,omitempty"`
	VideoExecutionEnvelopeHash string             `json:"videoExecutionEnvelopeHash,omitempty"`
	TimingAdvisory             json.RawMessage    `json:"timingAdvisory,omitempty"`
	PreviewHash                string             `json:"previewHash,omitempty"`
	ShotCount                  int                `json:"shotCount"`
	ReviewStatus               string             `json:"reviewStatus"`
	PlanHash                   string             `json:"planHash"`
	ProjectionHash             string             `json:"projectionHash"`
	CreatedAt                  time.Time          `json:"createdAt"`
	ActivatedAt                *time.Time         `json:"activatedAt,omitempty"`
}

type StoryboardShotSegmentLink struct {
	ID                    string `json:"id"`
	LocalizationSegmentID string `json:"localizationSegmentId"`
	SourceSegmentID       string `json:"sourceSegmentId"`
	Usage                 string `json:"usage"`
	Ordinal               int    `json:"ordinal"`
	VerbatimStart         *int   `json:"verbatimStart,omitempty"`
	VerbatimEnd           *int   `json:"verbatimEnd,omitempty"`
}

type StoryboardShotProductReference struct {
	ID                 string `json:"id"`
	ProductReferenceID string `json:"productReferenceId"`
	SourcePackID       string `json:"sourcePackId"`
	SourcePackItemID   string `json:"sourcePackItemId"`
	Role               string `json:"role"`
	Ordinal            int    `json:"ordinal"`
	Required           bool   `json:"required"`
	ArtifactID         string `json:"artifactId"`
	MediaFileID        string `json:"mediaFileId"`
	ContentHash        string `json:"contentHash"`
	PreviewURL         string `json:"previewUrl,omitempty"`
	StorageKey         string `json:"-"`
	MimeType           string `json:"-"`
}

type StoryboardShot struct {
	ID                                string                           `json:"id"`
	StoryboardPlanID                  string                           `json:"storyboardPlanId"`
	ScriptUnitID                      string                           `json:"scriptUnitId"`
	UnitGenerationID                  string                           `json:"scriptUnitGenerationId"`
	Revision                          int64                            `json:"revision"`
	ShotOrdinal                       int                              `json:"shotOrdinal"`
	Title                             string                           `json:"title"`
	DurationSeconds                   int                              `json:"durationSeconds"`
	StartTick                         int64                            `json:"startTick"`
	EndTick                           int64                            `json:"endTick"`
	SalesBeat                         string                           `json:"salesBeat"`
	VisualAction                      string                           `json:"visualAction"`
	ShotPurpose                       string                           `json:"shotPurpose"`
	Composition                       string                           `json:"composition"`
	Camera                            json.RawMessage                  `json:"camera"`
	VoiceoverText                     string                           `json:"voiceoverText"`
	OnscreenText                      string                           `json:"onscreenText"`
	TargetLanguage                    string                           `json:"targetLanguage"`
	SoundEffects                      json.RawMessage                  `json:"soundEffects"`
	MusicCue                          string                           `json:"musicCue"`
	CreativeDirection                 json.RawMessage                  `json:"creativeDirection"`
	EstimatedVoiceoverTicks           int64                            `json:"estimatedVoiceoverTicks"`
	VoiceoverOverflowTicks            int64                            `json:"voiceoverOverflowTicks"`
	TimingAdvisoryLevel               string                           `json:"timingAdvisoryLevel"`
	RecommendedRequestDurationSeconds int                              `json:"recommendedRequestDurationSeconds"`
	EligibleRouteSetHash              string                           `json:"eligibleRouteSetHash"`
	RequiredProductFeatures           []string                         `json:"requiredProductFeatures"`
	ReviewStatus                      string                           `json:"reviewStatus"`
	ManualOverride                    bool                             `json:"manualOverride"`
	StaleState                        string                           `json:"staleState"`
	ImagePrompt                       string                           `json:"imagePrompt,omitempty"`
	ImagePromptStatus                 string                           `json:"imagePromptStatus"`
	ImageStatus                       string                           `json:"imageStatus"`
	ImageArtifactID                   *string                          `json:"imageArtifactId,omitempty"`
	ImagePreviewURL                   *string                          `json:"imagePreviewUrl,omitempty"`
	VideoPrompt                       string                           `json:"videoPrompt,omitempty"`
	VideoPromptStatus                 string                           `json:"videoPromptStatus"`
	VideoRenderPlanID                 *string                          `json:"videoRenderPlanId,omitempty"`
	VideoRenderPlanStatus             *string                          `json:"videoRenderPlanStatus,omitempty"`
	VideoStatus                       string                           `json:"videoStatus"`
	VideoArtifactID                   *string                          `json:"videoArtifactId,omitempty"`
	VideoPreviewURL                   *string                          `json:"videoPreviewUrl,omitempty"`
	ImageErrorCode                    *string                          `json:"imageErrorCode,omitempty"`
	ImageErrorMessage                 *string                          `json:"imageErrorMessage,omitempty"`
	VideoErrorCode                    *string                          `json:"videoErrorCode,omitempty"`
	VideoErrorMessage                 *string                          `json:"videoErrorMessage,omitempty"`
	SegmentLinks                      []StoryboardShotSegmentLink      `json:"segmentLinks"`
	ProductReferences                 []StoryboardShotProductReference `json:"productReferences"`
	EditedBy                          *string                          `json:"editedBy,omitempty"`
	EditedAt                          *time.Time                       `json:"editedAt,omitempty"`

	ImageStorageKey string `json:"-"`
	ImageMimeType   string `json:"-"`
	VideoStorageKey string `json:"-"`
	VideoMimeType   string `json:"-"`
}

type StoryboardPlanDetail struct {
	Plan  StoryboardPlan   `json:"plan"`
	Shots []StoryboardShot `json:"shots"`
}

type UpdateStoryboardShotInput struct {
	ExpectedPlanRevision int64            `json:"expectedPlanRevision"`
	ExpectedShotRevision int64            `json:"expectedShotRevision"`
	VisualAction         *string          `json:"visualAction,omitempty"`
	ShotPurpose          *string          `json:"shotPurpose,omitempty"`
	Composition          *string          `json:"composition,omitempty"`
	Camera               *json.RawMessage `json:"camera,omitempty"`
	VoiceoverText        *string          `json:"voiceoverText,omitempty"`
	OnscreenText         *string          `json:"onscreenText,omitempty"`
	DurationSeconds      *int             `json:"durationSeconds,omitempty"`
	ProductReferenceIDs  *[]string        `json:"productReferenceIds,omitempty"`
}

type ReorderStoryboardShotItem struct {
	ShotID          string `json:"shotId"`
	DurationSeconds int    `json:"durationSeconds"`
}

type ReorderStoryboardShotsInput struct {
	ExpectedPlanRevision int64                       `json:"expectedPlanRevision"`
	Items                []ReorderStoryboardShotItem `json:"items"`
}
