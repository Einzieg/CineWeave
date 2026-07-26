package commerce

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type StoryboardStrategy string

const (
	StoryboardStrategySmart      StoryboardStrategy = "smart"
	StoryboardStrategySingleTake StoryboardStrategy = "single_take"
	StoryboardStrategyManual     StoryboardStrategy = "manual"

	CommerceSegmentationPolicyV2 = "commerce-smart-v2"
	CommerceVideoEnvelopeV1      = "commerce-video-execution-envelope/v1"
	CommerceSegmentationPlanV1   = "commerce-storyboard-segmentation/v1"
)

func ParseStoryboardStrategy(value string) (StoryboardStrategy, error) {
	strategy := StoryboardStrategy(strings.ToLower(strings.TrimSpace(value)))
	switch strategy {
	case StoryboardStrategySmart, StoryboardStrategySingleTake, StoryboardStrategyManual:
		return strategy, nil
	default:
		return "", Error{
			Code:    CodeStoryboardInvalid,
			Message: "分镜切分方式无效",
			Cause:   fmt.Errorf("unsupported storyboard strategy %q", value),
		}
	}
}

type StoryboardBeatInput struct {
	LocalizationSegmentID   string          `json:"localizationSegmentId"`
	SourceSegmentID         string          `json:"sourceSegmentId"`
	Ordinal                 int             `json:"ordinal"`
	SalesBeat               string          `json:"salesBeat"`
	LocalizedText           string          `json:"localizedText"`
	VoiceoverText           string          `json:"voiceoverText"`
	OnscreenText            string          `json:"onscreenText"`
	VisualIntent            string          `json:"visualIntent"`
	ProductClaims           []string        `json:"productClaims"`
	RequiredProductFeatures []string        `json:"requiredProductFeatures"`
	SoundEffects            []string        `json:"soundEffects"`
	MusicCue                string          `json:"musicCue"`
	EstimatedVoiceoverTicks int64           `json:"estimatedVoiceoverTicks"`
	Required                bool            `json:"required"`
	ForceBoundaryBefore     bool            `json:"forceBoundaryBefore"`
	ForceBoundaryAfter      bool            `json:"forceBoundaryAfter"`
	Continuity              json.RawMessage `json:"continuity"`
	ContentHash             string          `json:"contentHash"`
}

type VideoExecutionRoute struct {
	RouteKey                    string   `json:"routeKey"`
	ModelProfileID              string   `json:"modelProfileId"`
	ModelProfileKey             string   `json:"modelProfileKey"`
	ModelProfileBindingID       string   `json:"modelProfileBindingId"`
	ProviderModelID             string   `json:"providerModelId"`
	ProviderAccountID           string   `json:"providerAccountId"`
	ModelKey                    string   `json:"modelKey"`
	Priority                    int      `json:"priority"`
	Weight                      int      `json:"weight"`
	VariantKey                  string   `json:"variantKey"`
	CapabilitySnapshotHash      string   `json:"capabilitySnapshotHash"`
	ExecutableDurationSeconds   []int    `json:"executableDurationSeconds"`
	Resolutions                 []string `json:"resolutions"`
	AspectRatios                []string `json:"aspectRatios"`
	SupportsContinuousExtension bool     `json:"supportsContinuousExtension"`
}

type VideoExecutionEnvelope struct {
	ContractVersion                    string                `json:"contractVersion"`
	ProjectProductionGenerationID      string                `json:"projectProductionGenerationId"`
	VideoProductionBindingID           string                `json:"videoProductionBindingId"`
	VideoProductionBindingRevision     int64                 `json:"videoProductionBindingRevision"`
	VideoProductionProfileVersionID    string                `json:"videoProductionProfileVersionId"`
	VideoProductionProfileSnapshotHash string                `json:"videoProductionProfileSnapshotHash"`
	ModelProfileKey                    string                `json:"modelProfileKey"`
	TargetResolution                   string                `json:"targetResolution"`
	AspectRatio                        string                `json:"aspectRatio"`
	Routes                             []VideoExecutionRoute `json:"routes"`
	ExecutableDurationSeconds          []int                 `json:"executableDurationSeconds"`
}

type SegmentationShot struct {
	ShotOrdinal              int      `json:"shotOrdinal"`
	BeatOrdinals             []int    `json:"beatOrdinals"`
	LocalizationSegmentIDs   []string `json:"localizationSegmentIds"`
	SourceSegmentIDs         []string `json:"sourceSegmentIds"`
	EditDurationSeconds      int      `json:"editDurationSeconds"`
	RequestedDurationSeconds int      `json:"requestedDurationSeconds"`
	TrimDurationSeconds      int      `json:"trimDurationSeconds"`
	EstimatedVoiceoverTicks  int64    `json:"estimatedVoiceoverTicks"`
	VoiceoverOverflowTicks   int64    `json:"voiceoverOverflowTicks"`
	TimingAdvisoryLevel      string   `json:"timingAdvisoryLevel"`
	EligibleRouteKeys        []string `json:"eligibleRouteKeys"`
	EligibleRouteSetHash     string   `json:"eligibleRouteSetHash"`
	SemanticBoundaryPenalty  int      `json:"semanticBoundaryPenalty"`
	ContinuityPenalty        int      `json:"continuityPenalty"`
	ComplexityPenalty        int      `json:"complexityPenalty"`
}

type SegmentationPlan struct {
	ContractVersion            string             `json:"contractVersion"`
	Strategy                   StoryboardStrategy `json:"strategy"`
	SegmentationPolicyVersion  string             `json:"segmentationPolicyVersion"`
	TargetDurationSeconds      int                `json:"targetDurationSeconds"`
	TimelineTimebase           int64              `json:"timelineTimebase"`
	VideoExecutionEnvelopeHash string             `json:"videoExecutionEnvelopeHash"`
	Shots                      []SegmentationShot `json:"shots"`
	TotalRequestedSeconds      int                `json:"totalRequestedSeconds"`
	TotalTrimSeconds           int                `json:"totalTrimSeconds"`
	EstimatedVoiceoverTicks    int64              `json:"estimatedVoiceoverTicks"`
	VoiceoverOverflowTicks     int64              `json:"voiceoverOverflowTicks"`
	TimingAdvisoryLevel        string             `json:"timingAdvisoryLevel"`
}

type StoryboardTimingAdvisory struct {
	TargetDurationSeconds     int     `json:"targetDurationSeconds"`
	EstimatedVoiceoverSeconds float64 `json:"estimatedVoiceoverSeconds"`
	VoiceoverOverflowSeconds  float64 `json:"voiceoverOverflowSeconds"`
	Exceeded                  bool    `json:"exceeded"`
	Level                     string  `json:"level"`
	Message                   string  `json:"message"`
}

func (envelope VideoExecutionEnvelope) Validate() error {
	if envelope.ContractVersion != CommerceVideoEnvelopeV1 {
		return fmt.Errorf("video execution envelope contractVersion must be %s", CommerceVideoEnvelopeV1)
	}
	if strings.TrimSpace(envelope.ProjectProductionGenerationID) == "" ||
		strings.TrimSpace(envelope.VideoProductionBindingID) == "" ||
		envelope.VideoProductionBindingRevision <= 0 ||
		strings.TrimSpace(envelope.VideoProductionProfileVersionID) == "" ||
		!isContractHash(envelope.VideoProductionProfileSnapshotHash) ||
		strings.TrimSpace(envelope.ModelProfileKey) == "" ||
		strings.TrimSpace(envelope.TargetResolution) == "" ||
		strings.TrimSpace(envelope.AspectRatio) == "" {
		return fmt.Errorf("video execution envelope identity is incomplete")
	}
	if len(envelope.Routes) == 0 || len(envelope.ExecutableDurationSeconds) == 0 {
		return fmt.Errorf("video execution envelope has no executable routes")
	}
	routeKeys := make(map[string]struct{}, len(envelope.Routes))
	durationSet := make(map[int]struct{}, len(envelope.ExecutableDurationSeconds))
	lastDuration := 0
	for _, duration := range envelope.ExecutableDurationSeconds {
		if duration <= 0 || duration <= lastDuration {
			return fmt.Errorf("video execution envelope durations must be sorted unique positive integers")
		}
		durationSet[duration] = struct{}{}
		lastDuration = duration
	}
	projectedDurationSet := make(map[int]struct{}, len(envelope.ExecutableDurationSeconds))
	for _, route := range envelope.Routes {
		if strings.TrimSpace(route.RouteKey) == "" ||
			strings.TrimSpace(route.ModelProfileID) == "" ||
			strings.TrimSpace(route.ModelProfileKey) == "" ||
			strings.TrimSpace(route.ModelProfileBindingID) == "" ||
			strings.TrimSpace(route.ProviderModelID) == "" ||
			strings.TrimSpace(route.ProviderAccountID) == "" ||
			strings.TrimSpace(route.ModelKey) == "" ||
			strings.TrimSpace(route.VariantKey) == "" ||
			!isContractHash(route.CapabilitySnapshotHash) ||
			len(route.ExecutableDurationSeconds) == 0 {
			return fmt.Errorf("video execution route %q is incomplete", route.RouteKey)
		}
		if route.ModelProfileKey != envelope.ModelProfileKey {
			return fmt.Errorf(
				"video execution route %q model profile %q does not match envelope model profile %q",
				route.RouteKey,
				route.ModelProfileKey,
				envelope.ModelProfileKey,
			)
		}
		if _, exists := routeKeys[route.RouteKey]; exists {
			return fmt.Errorf("video execution route %q is duplicated", route.RouteKey)
		}
		routeKeys[route.RouteKey] = struct{}{}
		last := 0
		for _, duration := range route.ExecutableDurationSeconds {
			if duration <= 0 || duration <= last {
				return fmt.Errorf("video execution route %q durations must be sorted unique positive integers", route.RouteKey)
			}
			if _, exists := durationSet[duration]; !exists {
				return fmt.Errorf("video execution route %q duration %d is missing from the envelope projection", route.RouteKey, duration)
			}
			projectedDurationSet[duration] = struct{}{}
			last = duration
		}
		if !containsNormalizedValue(route.Resolutions, envelope.TargetResolution) {
			return fmt.Errorf("video execution route %q does not support resolution %s", route.RouteKey, envelope.TargetResolution)
		}
	}
	for _, duration := range envelope.ExecutableDurationSeconds {
		if _, exists := projectedDurationSet[duration]; !exists {
			return fmt.Errorf("video execution envelope duration %d is not executable by any route", duration)
		}
	}
	return nil
}

func NormalizeVideoExecutionEnvelope(envelope *VideoExecutionEnvelope) {
	if envelope == nil {
		return
	}
	envelope.ContractVersion = strings.TrimSpace(envelope.ContractVersion)
	envelope.ProjectProductionGenerationID = strings.TrimSpace(envelope.ProjectProductionGenerationID)
	envelope.VideoProductionBindingID = strings.TrimSpace(envelope.VideoProductionBindingID)
	envelope.VideoProductionProfileVersionID = strings.TrimSpace(envelope.VideoProductionProfileVersionID)
	envelope.VideoProductionProfileSnapshotHash = strings.ToLower(strings.TrimSpace(envelope.VideoProductionProfileSnapshotHash))
	envelope.ModelProfileKey = strings.TrimSpace(envelope.ModelProfileKey)
	envelope.TargetResolution = strings.ToLower(strings.TrimSpace(envelope.TargetResolution))
	envelope.AspectRatio = strings.ToLower(strings.TrimSpace(envelope.AspectRatio))
	envelope.ExecutableDurationSeconds = sortedUniquePositiveInts(envelope.ExecutableDurationSeconds)
	for index := range envelope.Routes {
		route := &envelope.Routes[index]
		route.RouteKey = strings.TrimSpace(route.RouteKey)
		route.ModelProfileID = strings.TrimSpace(route.ModelProfileID)
		route.ModelProfileKey = strings.TrimSpace(route.ModelProfileKey)
		route.ModelProfileBindingID = strings.TrimSpace(route.ModelProfileBindingID)
		route.ProviderModelID = strings.TrimSpace(route.ProviderModelID)
		route.ProviderAccountID = strings.TrimSpace(route.ProviderAccountID)
		route.ModelKey = strings.TrimSpace(route.ModelKey)
		route.VariantKey = strings.TrimSpace(route.VariantKey)
		route.CapabilitySnapshotHash = strings.ToLower(strings.TrimSpace(route.CapabilitySnapshotHash))
		route.ExecutableDurationSeconds = sortedUniquePositiveInts(route.ExecutableDurationSeconds)
		route.Resolutions = sortedNormalizedValues(route.Resolutions)
		route.AspectRatios = sortedNormalizedValues(route.AspectRatios)
	}
	sort.SliceStable(envelope.Routes, func(i, j int) bool {
		left, right := envelope.Routes[i], envelope.Routes[j]
		if left.Priority != right.Priority {
			return left.Priority < right.Priority
		}
		if left.Weight != right.Weight {
			return left.Weight > right.Weight
		}
		return left.RouteKey < right.RouteKey
	})
}

// CanonicalizeVideoExecutionEnvelope returns an immutable canonical projection and
// its contract hash. Both producers and consumers must use this function so the
// hash cannot depend on Go struct field order or map round-trips.
func CanonicalizeVideoExecutionEnvelope(envelope VideoExecutionEnvelope) (VideoExecutionEnvelope, string, error) {
	canonical := envelope
	canonical.ExecutableDurationSeconds = append([]int(nil), envelope.ExecutableDurationSeconds...)
	canonical.Routes = append([]VideoExecutionRoute(nil), envelope.Routes...)
	for index := range canonical.Routes {
		canonical.Routes[index].ExecutableDurationSeconds = append(
			[]int(nil),
			envelope.Routes[index].ExecutableDurationSeconds...,
		)
		canonical.Routes[index].Resolutions = append([]string(nil), envelope.Routes[index].Resolutions...)
		canonical.Routes[index].AspectRatios = append([]string(nil), envelope.Routes[index].AspectRatios...)
	}
	NormalizeVideoExecutionEnvelope(&canonical)
	if err := canonical.Validate(); err != nil {
		return VideoExecutionEnvelope{}, "", err
	}
	hash, err := hashStoryboardContract(canonical)
	if err != nil {
		return VideoExecutionEnvelope{}, "", err
	}
	return canonical, hash, nil
}

func sortedUniquePositiveInts(values []int) []int {
	seen := make(map[int]struct{}, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}

func sortedNormalizedValues(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func containsNormalizedValue(values []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == target {
			return true
		}
	}
	return false
}

func isContractHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
