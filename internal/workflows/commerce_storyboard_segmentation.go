package workflows

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Einzieg/cineweave/internal/commerce"
)

var commerceStoryboardSceneHeaderPattern = regexp.MustCompile(
	`(?i)^\s*(?:scene|shot|场景|镜头)\s*(?:\d+|[一二三四五六七八九十百零]+)(?:\s*[\(（][^)\r\n）]+[)）])?\s*[:：]?\s*$`,
)

type CommerceStoryboardDeterministicPlan struct {
	Beats                []commerce.StoryboardBeatInput    `json:"beats"`
	Segmentation         commerce.SegmentationPlan         `json:"segmentation"`
	SegmentationPlanHash string                            `json:"segmentationPlanHash"`
	Skeleton             CommerceStoryboardPlanContract    `json:"skeleton"`
	TimingAdvisory       commerce.StoryboardTimingAdvisory `json:"timingAdvisory"`
	PreviewHash          string                            `json:"previewHash"`
}

type CommerceStoryboardPlanningPreview struct {
	Identity                    commerce.UnitGenerationIdentity   `json:"identity"`
	InputHash                   string                            `json:"inputHash"`
	StoryboardStrategy          commerce.StoryboardStrategy       `json:"storyboardStrategy"`
	SegmentationPolicyVersion   string                            `json:"segmentationPolicyVersion"`
	TargetDurationSeconds       int                               `json:"targetDurationSeconds"`
	EstimatedVoiceoverSeconds   float64                           `json:"estimatedVoiceoverSeconds"`
	VoiceoverOverflowSeconds    float64                           `json:"voiceoverOverflowSeconds"`
	VoiceoverExceeded           bool                              `json:"voiceoverExceeded"`
	ProviderDurationOptions     []int                             `json:"providerDurationOptions"`
	RecommendedShotCount        int                               `json:"recommendedShotCount"`
	PlannedEditDurations        []int                             `json:"plannedEditDurations"`
	RecommendedRequestDurations []int                             `json:"recommendedRequestDurations"`
	EstimatedTrimSeconds        int                               `json:"estimatedTrimSeconds"`
	VideoExecutionEnvelopeHash  string                            `json:"videoExecutionEnvelopeHash"`
	SegmentationPlanHash        string                            `json:"segmentationPlanHash"`
	PreviewHash                 string                            `json:"previewHash"`
	TimingAdvisory              commerce.StoryboardTimingAdvisory `json:"timingAdvisory"`
	Segmentation                commerce.SegmentationPlan         `json:"segmentation"`
}

func NewCommerceStoryboardPlanningPreview(
	snapshot CommerceStoryboardPlanningSnapshot,
	plan CommerceStoryboardDeterministicPlan,
) CommerceStoryboardPlanningPreview {
	editDurations := make([]int, 0, len(plan.Segmentation.Shots))
	requestDurations := make([]int, 0, len(plan.Segmentation.Shots))
	trimSeconds := 0
	for _, shot := range plan.Segmentation.Shots {
		editDurations = append(editDurations, shot.EditDurationSeconds)
		requestDurations = append(requestDurations, shot.RequestedDurationSeconds)
		trimSeconds += shot.TrimDurationSeconds
	}
	return CommerceStoryboardPlanningPreview{
		Identity: snapshot.Identity, InputHash: snapshot.InputHash,
		StoryboardStrategy:          snapshot.StoryboardStrategy,
		SegmentationPolicyVersion:   snapshot.SegmentationPolicyVersion,
		TargetDurationSeconds:       snapshot.TargetDurationSeconds,
		EstimatedVoiceoverSeconds:   plan.TimingAdvisory.EstimatedVoiceoverSeconds,
		VoiceoverOverflowSeconds:    plan.TimingAdvisory.VoiceoverOverflowSeconds,
		VoiceoverExceeded:           plan.TimingAdvisory.Exceeded,
		ProviderDurationOptions:     append([]int(nil), snapshot.VideoExecutionEnvelope.ExecutableDurationSeconds...),
		RecommendedShotCount:        len(plan.Segmentation.Shots),
		PlannedEditDurations:        editDurations,
		RecommendedRequestDurations: requestDurations,
		EstimatedTrimSeconds:        trimSeconds,
		VideoExecutionEnvelopeHash:  snapshot.VideoExecutionEnvelopeHash,
		SegmentationPlanHash:        plan.SegmentationPlanHash,
		PreviewHash:                 plan.PreviewHash,
		TimingAdvisory:              plan.TimingAdvisory,
		Segmentation:                plan.Segmentation,
	}
}

func BuildCommerceStoryboardDeterministicPlan(
	snapshot CommerceStoryboardPlanningSnapshot,
	salesScript CommerceSalesScriptContract,
) (CommerceStoryboardDeterministicPlan, error) {
	if err := ValidateCommerceStoryboardSnapshot(snapshot.Identity, snapshot); err != nil {
		return CommerceStoryboardDeterministicPlan{}, err
	}
	if err := ValidateCommerceSalesScript(salesScript, snapshot); err != nil {
		return CommerceStoryboardDeterministicPlan{}, err
	}
	beats, err := buildCommerceStoryboardBeatInputs(snapshot, salesScript)
	if err != nil {
		return CommerceStoryboardDeterministicPlan{}, err
	}
	segmentation, err := commerce.PlanStoryboardSegmentation(commerce.StoryboardSegmentationInput{
		Strategy: snapshot.StoryboardStrategy, SegmentationPolicyVersion: snapshot.SegmentationPolicyVersion,
		TargetDurationSeconds: snapshot.TargetDurationSeconds, TimelineTimebase: snapshot.TimelineTimebase,
		VideoExecutionEnvelope:     snapshot.VideoExecutionEnvelope,
		VideoExecutionEnvelopeHash: snapshot.VideoExecutionEnvelopeHash,
		Beats:                      beats,
	})
	if err != nil {
		return CommerceStoryboardDeterministicPlan{}, err
	}
	segmentationHash, err := commerceContractHash(segmentation)
	if err != nil {
		return CommerceStoryboardDeterministicPlan{}, err
	}
	skeleton, err := buildCommerceStoryboardCreativeSkeleton(snapshot, salesScript, beats, segmentation)
	if err != nil {
		return CommerceStoryboardDeterministicPlan{}, err
	}
	advisory := buildCommerceStoryboardTimingAdvisory(snapshot, segmentation)
	previewHash, err := commerceContractHash(map[string]any{
		"identity": snapshot.Identity, "inputHash": snapshot.InputHash,
		"storyboardStrategy":         snapshot.StoryboardStrategy,
		"segmentationPolicyVersion":  snapshot.SegmentationPolicyVersion,
		"videoExecutionEnvelopeHash": snapshot.VideoExecutionEnvelopeHash,
		"beats":                      beats, "segmentationPlanHash": segmentationHash,
		"targetDurationSeconds": snapshot.TargetDurationSeconds,
		"aspectRatio":           snapshot.AspectRatio, "timelineTimebase": snapshot.TimelineTimebase,
		"fpsNumerator": snapshot.FPSNumerator, "fpsDenominator": snapshot.FPSDenominator,
	})
	if err != nil {
		return CommerceStoryboardDeterministicPlan{}, err
	}
	return CommerceStoryboardDeterministicPlan{
		Beats: beats, Segmentation: segmentation, SegmentationPlanHash: segmentationHash,
		Skeleton: skeleton, TimingAdvisory: advisory, PreviewHash: previewHash,
	}, nil
}

func buildCommerceStoryboardBeatInputs(
	snapshot CommerceStoryboardPlanningSnapshot,
	salesScript CommerceSalesScriptContract,
) ([]commerce.StoryboardBeatInput, error) {
	salesBySource := make(map[string]CommerceSalesScriptSegmentContract, len(salesScript.Segments))
	for _, segment := range salesScript.Segments {
		salesBySource[segment.SourceSegmentID] = segment
	}
	beats := make([]commerce.StoryboardBeatInput, 0, len(snapshot.LocalizedSegments))
	for _, localized := range snapshot.LocalizedSegments {
		sales, ok := salesBySource[localized.SourceSegmentID]
		if !ok {
			return nil, fmt.Errorf("sales script is missing source segment %s", localized.SourceSegmentID)
		}
		estimatedTicks, err := estimateCommerceVoiceoverTicks(
			localized.VoiceoverText,
			snapshot.TargetLocale,
			snapshot.TimingPolicy,
			snapshot.TimelineTimebase,
		)
		if err != nil {
			return nil, err
		}
		forceBoundary := commerceStoryboardExplicitSceneBoundary(sales.SalesBeat, sales.VisualIntent)
		continuity, err := json.Marshal(map[string]any{
			"sceneKey":          commerceStoryboardSceneKey(sales.VisualIntent),
			"independentAction": strings.TrimSpace(sales.VisualIntent) != "",
			"visualComplexity":  commerceStoryboardVisualComplexity(sales.VisualIntent),
			"initialStateKey":   "",
			"finalStateKey":     "",
		})
		if err != nil {
			return nil, err
		}
		beat := commerce.StoryboardBeatInput{
			LocalizationSegmentID: localized.ID, SourceSegmentID: localized.SourceSegmentID,
			Ordinal: localized.Ordinal, SalesBeat: sales.SalesBeat,
			LocalizedText: localized.LocalizedText, VoiceoverText: localized.VoiceoverText,
			OnscreenText: localized.OnscreenText, VisualIntent: sales.VisualIntent,
			ProductClaims:           append([]string(nil), localized.ProductClaims...),
			RequiredProductFeatures: append([]string(nil), localized.RequiredProductFeatures...),
			SoundEffects:            append([]string(nil), sales.SoundEffects...), MusicCue: sales.MusicCue,
			EstimatedVoiceoverTicks: estimatedTicks, Required: localized.Required,
			ForceBoundaryBefore: forceBoundary, Continuity: continuity,
		}
		beat.ContentHash, err = hashCommerceStoryboardBeat(beat)
		if err != nil {
			return nil, err
		}
		beats = append(beats, beat)
	}
	sort.SliceStable(beats, func(i, j int) bool { return beats[i].Ordinal < beats[j].Ordinal })
	return arrangeCommerceStoryboardTimedBeats(beats)
}

func arrangeCommerceStoryboardTimedBeats(
	beats []commerce.StoryboardBeatInput,
) ([]commerce.StoryboardBeatInput, error) {
	sceneGroups := make([][]commerce.StoryboardBeatInput, 0)
	prefixVoiceover := make([]commerce.StoryboardBeatInput, 0)
	currentScene := -1
	for _, beat := range beats {
		if isCommerceStoryboardSceneHeader(beat) {
			currentScene = len(sceneGroups)
			sceneGroups = append(sceneGroups, []commerce.StoryboardBeatInput{beat})
			continue
		}
		if strings.TrimSpace(beat.VoiceoverText) == "" {
			continue
		}
		if currentScene < 0 {
			prefixVoiceover = append(prefixVoiceover, beat)
			continue
		}
		sceneGroups[currentScene] = append(sceneGroups[currentScene], beat)
	}

	if len(sceneGroups) == 0 {
		timed := prefixVoiceover
		if len(timed) == 0 {
			timed = append([]commerce.StoryboardBeatInput(nil), beats...)
		}
		return reindexCommerceStoryboardBeats(timed)
	}

	prefixByGroup := make([][]commerce.StoryboardBeatInput, len(sceneGroups))
	for index, beat := range prefixVoiceover {
		groupIndex := index * len(sceneGroups) / maxInt(1, len(prefixVoiceover))
		if groupIndex >= len(sceneGroups) {
			groupIndex = len(sceneGroups) - 1
		}
		prefixByGroup[groupIndex] = append(prefixByGroup[groupIndex], beat)
	}
	for groupIndex := range sceneGroups {
		group := sceneGroups[groupIndex]
		combined := make([]commerce.StoryboardBeatInput, 0, len(group)+len(prefixByGroup[groupIndex]))
		combined = append(combined, group[0])
		combined = append(combined, prefixByGroup[groupIndex]...)
		combined = append(combined, group[1:]...)
		sceneGroups[groupIndex] = combined
	}

	timed := make([]commerce.StoryboardBeatInput, 0, len(sceneGroups)+len(prefixVoiceover))
	for groupIndex, group := range sceneGroups {
		for beatIndex := range group {
			group[beatIndex].ForceBoundaryBefore = beatIndex == 0 && groupIndex > 0
			group[beatIndex].ForceBoundaryAfter = beatIndex == len(group)-1 && groupIndex < len(sceneGroups)-1
		}
		timed = append(timed, group...)
	}
	return reindexCommerceStoryboardBeats(timed)
}

func reindexCommerceStoryboardBeats(
	beats []commerce.StoryboardBeatInput,
) ([]commerce.StoryboardBeatInput, error) {
	result := append([]commerce.StoryboardBeatInput(nil), beats...)
	for index := range result {
		result[index].Ordinal = index + 1
		hash, err := hashCommerceStoryboardBeat(result[index])
		if err != nil {
			return nil, err
		}
		result[index].ContentHash = hash
	}
	return result, nil
}

func hashCommerceStoryboardBeat(beat commerce.StoryboardBeatInput) (string, error) {
	return commerceContractHash(map[string]any{
		"localizationSegmentId": beat.LocalizationSegmentID,
		"sourceSegmentId":       beat.SourceSegmentID, "ordinal": beat.Ordinal,
		"salesBeat": beat.SalesBeat, "localizedText": beat.LocalizedText,
		"voiceoverText": beat.VoiceoverText, "onscreenText": beat.OnscreenText,
		"visualIntent": beat.VisualIntent, "productClaims": beat.ProductClaims,
		"requiredProductFeatures": beat.RequiredProductFeatures,
		"soundEffects":            beat.SoundEffects, "musicCue": beat.MusicCue,
		"estimatedVoiceoverTicks": beat.EstimatedVoiceoverTicks,
		"required":                beat.Required, "continuity": beat.Continuity,
	})
}

func isCommerceStoryboardSceneHeader(beat commerce.StoryboardBeatInput) bool {
	return commerceStoryboardSceneHeaderPattern.MatchString(strings.TrimSpace(beat.LocalizedText)) ||
		commerceStoryboardSceneHeaderPattern.MatchString(strings.TrimSpace(beat.VisualIntent))
}

func buildCommerceStoryboardCreativeSkeleton(
	snapshot CommerceStoryboardPlanningSnapshot,
	salesScript CommerceSalesScriptContract,
	beats []commerce.StoryboardBeatInput,
	segmentation commerce.SegmentationPlan,
) (CommerceStoryboardPlanContract, error) {
	beatByOrdinal := make(map[int]commerce.StoryboardBeatInput, len(beats))
	for _, beat := range beats {
		beatByOrdinal[beat.Ordinal] = beat
	}
	referenceIDs := make([]string, 0, len(snapshot.ProductReferences))
	for _, reference := range snapshot.ProductReferences {
		if reference.Required {
			referenceIDs = append(referenceIDs, reference.ReferenceID)
		}
	}
	if len(referenceIDs) == 0 && len(snapshot.ProductReferences) > 0 {
		referenceIDs = append(referenceIDs, snapshot.ProductReferences[0].ReferenceID)
	}
	plan := CommerceStoryboardPlanContract{
		ContractVersion:           CommerceStoryboardPlanContractVersion,
		CommerceScriptUnitID:      snapshot.Identity.ScriptUnitID,
		ScriptUnitGenerationID:    snapshot.Identity.UnitGenerationID,
		CommerceWorkflowBindingID: snapshot.Identity.CommerceWorkflowBindingID,
		ProductVersionID:          snapshot.ProductVersionID, TargetLocale: snapshot.TargetLocale,
		TargetDurationSeconds: snapshot.TargetDurationSeconds,
	}
	for _, segmentedShot := range segmentation.Shots {
		group := make([]commerce.StoryboardBeatInput, 0, len(segmentedShot.BeatOrdinals))
		for _, ordinal := range segmentedShot.BeatOrdinals {
			beat, ok := beatByOrdinal[ordinal]
			if !ok {
				return CommerceStoryboardPlanContract{}, fmt.Errorf("segmentation references unknown beat ordinal %d", ordinal)
			}
			group = append(group, beat)
		}
		plan.Shots = append(plan.Shots, CommerceStoryboardShotContract{
			CandidateKey:        fmt.Sprintf("shot-%03d", segmentedShot.ShotOrdinal),
			ShotOrdinal:         segmentedShot.ShotOrdinal,
			SourceSegmentIDs:    append([]string(nil), segmentedShot.SourceSegmentIDs...),
			DurationSeconds:     segmentedShot.EditDurationSeconds,
			SalesBeat:           firstCommerceBeatValue(group),
			ShotPurpose:         "根据冻结节拍生成镜头目的",
			VisualAction:        joinCommerceBeatValues(group, func(beat commerce.StoryboardBeatInput) string { return beat.VisualIntent }),
			Camera:              json.RawMessage(`{}`),
			Composition:         "根据商品参考图和连续动作设计构图",
			VoiceoverText:       joinCommerceBeatValues(group, func(beat commerce.StoryboardBeatInput) string { return beat.VoiceoverText }),
			OnscreenText:        joinCommerceBeatValues(group, func(beat commerce.StoryboardBeatInput) string { return beat.OnscreenText }),
			SoundEffects:        uniqueCommerceBeatStrings(group, func(beat commerce.StoryboardBeatInput) []string { return beat.SoundEffects }),
			MusicCue:            firstNonEmptyCommerceBeatValue(group, func(beat commerce.StoryboardBeatInput) string { return beat.MusicCue }),
			ProductReferenceIDs: append([]string(nil), referenceIDs...),
			RequiredProductFeatures: uniqueCommerceBeatStrings(group, func(beat commerce.StoryboardBeatInput) []string {
				return beat.RequiredProductFeatures
			}),
		})
	}
	plan, err := reconcileCommerceStoryboardRequiredContextCoverage(snapshot, plan)
	if err != nil {
		return CommerceStoryboardPlanContract{}, err
	}
	plan, err = reconcileCommerceStoryboardSalesBeats(salesScript, plan)
	if err != nil {
		return CommerceStoryboardPlanContract{}, err
	}
	return reconcileCommerceStoryboardVoiceover(snapshot, plan)
}

func applyCommerceStoryboardCreativeDirection(
	skeleton CommerceStoryboardPlanContract,
	candidate CommerceStoryboardPlanContract,
) (CommerceStoryboardPlanContract, error) {
	if len(candidate.Shots) != len(skeleton.Shots) {
		return CommerceStoryboardPlanContract{}, fmt.Errorf(
			"storyboard planner changed deterministic shot count from %d to %d",
			len(skeleton.Shots),
			len(candidate.Shots),
		)
	}
	candidateByOrdinal := make(map[int]CommerceStoryboardShotContract, len(candidate.Shots))
	for _, shot := range candidate.Shots {
		if shot.ShotOrdinal <= 0 {
			return CommerceStoryboardPlanContract{}, fmt.Errorf("storyboard planner returned an invalid shot ordinal")
		}
		if _, exists := candidateByOrdinal[shot.ShotOrdinal]; exists {
			return CommerceStoryboardPlanContract{}, fmt.Errorf("storyboard planner duplicated shot ordinal %d", shot.ShotOrdinal)
		}
		candidateByOrdinal[shot.ShotOrdinal] = shot
	}
	result := skeleton
	result.Shots = make([]CommerceStoryboardShotContract, 0, len(skeleton.Shots))
	for _, frozen := range skeleton.Shots {
		creative, ok := candidateByOrdinal[frozen.ShotOrdinal]
		if !ok {
			return CommerceStoryboardPlanContract{}, fmt.Errorf("storyboard planner omitted frozen shot %s", frozen.CandidateKey)
		}
		frozen.ShotPurpose = strings.TrimSpace(creative.ShotPurpose)
		frozen.VisualAction = strings.TrimSpace(creative.VisualAction)
		frozen.Camera = append(json.RawMessage(nil), creative.Camera...)
		frozen.Composition = strings.TrimSpace(creative.Composition)
		if len(creative.ProductReferenceIDs) > 0 {
			frozen.ProductReferenceIDs = append([]string(nil), creative.ProductReferenceIDs...)
		}
		if frozen.ShotPurpose == "" || frozen.VisualAction == "" || frozen.Composition == "" {
			return CommerceStoryboardPlanContract{}, fmt.Errorf("storyboard planner returned incomplete creative fields for %s", frozen.CandidateKey)
		}
		if err := validateJSONObjectRaw(frozen.Camera); err != nil {
			return CommerceStoryboardPlanContract{}, fmt.Errorf("storyboard planner camera for %s: %w", frozen.CandidateKey, err)
		}
		result.Shots = append(result.Shots, frozen)
	}
	return result, nil
}

func aliasCommerceStoryboardCreativeSkeleton(
	plan CommerceStoryboardPlanContract,
	aliases commerceStoryboardSourceAliases,
) (CommerceStoryboardPlanContract, error) {
	aliased := plan
	aliased.Shots = append([]CommerceStoryboardShotContract(nil), plan.Shots...)
	for shotIndex := range aliased.Shots {
		aliased.Shots[shotIndex].SourceSegmentIDs = append(
			[]string(nil),
			plan.Shots[shotIndex].SourceSegmentIDs...,
		)
		for sourceIndex, sourceID := range aliased.Shots[shotIndex].SourceSegmentIDs {
			alias, ok := aliases.actualToAlias[sourceID]
			if !ok {
				return CommerceStoryboardPlanContract{}, fmt.Errorf("missing source alias for %s", sourceID)
			}
			aliased.Shots[shotIndex].SourceSegmentIDs[sourceIndex] = alias
		}
	}
	return aliased, nil
}

func estimateCommerceVoiceoverTicks(
	voiceover string,
	locale string,
	policy CommerceTimingPolicy,
	timebase int64,
) (int64, error) {
	if strings.TrimSpace(voiceover) == "" {
		return 0, nil
	}
	analysis, err := AnalyzeCommerceTiming(CommerceLocalizationContract{
		TargetLanguage: locale,
		Segments:       []CommerceLocalizationSegmentContract{{VoiceoverText: voiceover}},
	}, policy, 1)
	if err != nil {
		return 0, err
	}
	return int64(math.Ceil(analysis.EstimatedVoiceoverSeconds * float64(timebase))), nil
}

func buildCommerceStoryboardTimingAdvisory(
	snapshot CommerceStoryboardPlanningSnapshot,
	segmentation commerce.SegmentationPlan,
) commerce.StoryboardTimingAdvisory {
	estimatedSeconds := float64(segmentation.EstimatedVoiceoverTicks) / float64(snapshot.TimelineTimebase)
	overflowSeconds := math.Max(0, estimatedSeconds-float64(snapshot.TargetDurationSeconds))
	advisory := commerce.StoryboardTimingAdvisory{
		TargetDurationSeconds:     snapshot.TargetDurationSeconds,
		EstimatedVoiceoverSeconds: roundCommerceSeconds(estimatedSeconds),
		VoiceoverOverflowSeconds:  roundCommerceSeconds(overflowSeconds),
		Exceeded:                  overflowSeconds > 0,
		Level:                     segmentation.TimingAdvisoryLevel,
	}
	if advisory.Exceeded {
		advisory.Message = fmt.Sprintf(
			"预计旁白 %.1f 秒，超过用户选择的 %d 秒；仍可继续生成，建议缩短旁白或选择更长时长。",
			advisory.EstimatedVoiceoverSeconds,
			advisory.TargetDurationSeconds,
		)
	} else {
		advisory.Message = "旁白预计时长在用户选择的目标时长内。"
	}
	return advisory
}

func commerceStoryboardExplicitSceneBoundary(salesBeat, visualIntent string) bool {
	value := strings.ToLower(strings.TrimSpace(salesBeat + " " + visualIntent))
	for _, marker := range []string{
		"场景切换", "切换场景", "转场", "另一个场景", "new scene", "scene change", "cut to",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func commerceStoryboardSceneKey(visualIntent string) string {
	value := strings.ToLower(strings.TrimSpace(visualIntent))
	for _, separator := range []string{"场景：", "场景:", "scene:", "location:"} {
		index := strings.Index(value, separator)
		if index < 0 {
			continue
		}
		start := index + len(separator)
		value = value[start:]
		for _, end := range []string{"，", ",", "。", ".", "；", ";", "\n"} {
			if endIndex := strings.Index(value, end); endIndex >= 0 {
				value = value[:endIndex]
			}
		}
		return strings.TrimSpace(value)
	}
	return ""
}

func commerceStoryboardVisualComplexity(visualIntent string) int {
	value := strings.TrimSpace(visualIntent)
	if value == "" {
		return 1
	}
	complexity := 1
	for _, separator := range []string{"，", ",", "；", ";", "然后", "随后", "同时", " and ", " then "} {
		complexity += strings.Count(strings.ToLower(value), separator)
	}
	if utf8.RuneCountInString(value) > 120 {
		complexity++
	}
	if complexity > 3 {
		return 3
	}
	return complexity
}

func firstCommerceBeatValue(beats []commerce.StoryboardBeatInput) string {
	for _, beat := range beats {
		if value := strings.TrimSpace(beat.SalesBeat); value != "" {
			return value
		}
	}
	return "product_demo"
}

func joinCommerceBeatValues(beats []commerce.StoryboardBeatInput, value func(commerce.StoryboardBeatInput) string) string {
	parts := make([]string, 0, len(beats))
	for _, beat := range beats {
		if text := strings.TrimSpace(value(beat)); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

func firstNonEmptyCommerceBeatValue(beats []commerce.StoryboardBeatInput, value func(commerce.StoryboardBeatInput) string) string {
	for _, beat := range beats {
		if text := strings.TrimSpace(value(beat)); text != "" {
			return text
		}
	}
	return ""
}

func uniqueCommerceBeatStrings(beats []commerce.StoryboardBeatInput, values func(commerce.StoryboardBeatInput) []string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, beat := range beats {
		for _, value := range values(beat) {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			key := strings.ToLower(value)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}

func roundCommerceSeconds(value float64) float64 {
	return math.Round(value*10) / 10
}

func commerceSegmentationShotByOrdinal(
	plan commerce.SegmentationPlan,
	ordinal int,
) (commerce.SegmentationShot, bool) {
	for _, shot := range plan.Shots {
		if shot.ShotOrdinal == ordinal {
			return shot, true
		}
	}
	return commerce.SegmentationShot{}, false
}
