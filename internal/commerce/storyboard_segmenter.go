package commerce

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

type StoryboardSegmentationInput struct {
	Strategy                   StoryboardStrategy     `json:"strategy"`
	SegmentationPolicyVersion  string                 `json:"segmentationPolicyVersion"`
	TargetDurationSeconds      int                    `json:"targetDurationSeconds"`
	TimelineTimebase           int64                  `json:"timelineTimebase"`
	VideoExecutionEnvelope     VideoExecutionEnvelope `json:"videoExecutionEnvelope"`
	VideoExecutionEnvelopeHash string                 `json:"videoExecutionEnvelopeHash"`
	Beats                      []StoryboardBeatInput  `json:"beats"`
}

type storyboardSegmentationPolicy struct {
	MaxIndependentActionsPerShot int
	MaxVisualComplexityPerShot   int
	RequestCountWeight           int64
	RequestDurationWeight        int64
	TrimWeight                   int64
	VoiceoverOverflowWeight      int64
	AllocationDeviationWeight    int64
	SalesBeatMergeWeight         int64
	ComplexityWeight             int64
}

func commerceStoryboardSegmentationPolicy(version string) (storyboardSegmentationPolicy, error) {
	if strings.TrimSpace(version) != CommerceSegmentationPolicyV2 {
		return storyboardSegmentationPolicy{}, fmt.Errorf("unsupported segmentation policy %q", version)
	}
	return storyboardSegmentationPolicy{
		MaxIndependentActionsPerShot: 3,
		MaxVisualComplexityPerShot:   8,
		RequestCountWeight:           250,
		RequestDurationWeight:        10,
		TrimWeight:                   30,
		VoiceoverOverflowWeight:      120,
		AllocationDeviationWeight:    4,
		SalesBeatMergeWeight:         20,
		ComplexityWeight:             15,
	}, nil
}

func PlanStoryboardSegmentation(input StoryboardSegmentationInput) (SegmentationPlan, error) {
	strategy, err := ParseStoryboardStrategy(string(input.Strategy))
	if err != nil {
		return SegmentationPlan{}, err
	}
	if strategy == StoryboardStrategyManual {
		return SegmentationPlan{}, Error{Code: CodeStoryboardInvalid, Message: "手动切分尚未开放"}
	}
	policy, err := commerceStoryboardSegmentationPolicy(input.SegmentationPolicyVersion)
	if err != nil {
		return SegmentationPlan{}, Error{Code: CodeStoryboardInvalid, Message: "分镜切分策略版本无效", Cause: err}
	}
	if input.TargetDurationSeconds <= 0 || input.TimelineTimebase <= 0 {
		return SegmentationPlan{}, Error{Code: CodeStoryboardInvalid, Message: "分镜目标时长或时间基准无效"}
	}
	canonicalEnvelope, envelopeHash, err := CanonicalizeVideoExecutionEnvelope(input.VideoExecutionEnvelope)
	if err != nil {
		return SegmentationPlan{}, Error{Code: CodeStoryboardInvalid, Message: "视频执行能力快照无效", Cause: err}
	}
	if envelopeHash != input.VideoExecutionEnvelopeHash {
		return SegmentationPlan{}, Error{Code: CodeStoryboardInvalid, Message: "视频执行能力快照已变化"}
	}
	input.VideoExecutionEnvelope = canonicalEnvelope
	beats, err := normalizeStoryboardBeatInputs(input.Beats)
	if err != nil {
		return SegmentationPlan{}, err
	}
	if strategy == StoryboardStrategySingleTake {
		shot, ok, err := buildSegmentationShot(
			beats,
			1,
			input.TargetDurationSeconds,
			input.TimelineTimebase,
			input.VideoExecutionEnvelope,
		)
		if err != nil {
			return SegmentationPlan{}, err
		}
		if !ok {
			return SegmentationPlan{}, Error{
				Code:    CodeStoryboardInvalid,
				Message: "当前视频模型无法用单段覆盖用户选择的目标时长，请使用智能切分",
			}
		}
		return finalizeSegmentationPlan(input, []SegmentationShot{shot}), nil
	}
	shots, err := planSmartStoryboardSegmentation(input, beats, policy)
	if err != nil {
		return SegmentationPlan{}, err
	}
	return finalizeSegmentationPlan(input, shots), nil
}

type storyboardSegmentationState struct {
	Score     int64
	Shots     []SegmentationShot
	Signature string
}

func planSmartStoryboardSegmentation(
	input StoryboardSegmentationInput,
	beats []StoryboardBeatInput,
	policy storyboardSegmentationPolicy,
) ([]SegmentationShot, error) {
	type stateKey struct {
		BeatIndex      int
		ElapsedSeconds int
	}
	states := map[stateKey]storyboardSegmentationState{
		{}: {Score: 0, Shots: []SegmentationShot{}, Signature: ""},
	}
	totalWeight := storyboardBeatRangeWeight(beats, input.TimelineTimebase)
	for start := 0; start < len(beats); start++ {
		for elapsed := 0; elapsed < input.TargetDurationSeconds; elapsed++ {
			current, ok := states[stateKey{BeatIndex: start, ElapsedSeconds: elapsed}]
			if !ok {
				continue
			}
			for end := start + 1; end <= len(beats); end++ {
				group := beats[start:end]
				mergePenalty, continuityPenalty, complexityPenalty, mergeOK := storyboardBeatGroupCompatibility(group, policy)
				if !mergeOK {
					break
				}
				remainingBeatCount := len(beats) - end
				maxEdit := input.TargetDurationSeconds - elapsed
				if remainingBeatCount > 0 {
					maxEdit -= 1
				}
				for editSeconds := 1; editSeconds <= maxEdit; editSeconds++ {
					shot, executable, err := buildSegmentationShot(
						group,
						len(current.Shots)+1,
						editSeconds,
						input.TimelineTimebase,
						input.VideoExecutionEnvelope,
					)
					if err != nil {
						return nil, err
					}
					if !executable {
						continue
					}
					shot.SemanticBoundaryPenalty = mergePenalty
					shot.ContinuityPenalty = continuityPenalty
					shot.ComplexityPenalty = complexityPenalty
					groupWeight := storyboardBeatRangeWeight(group, input.TimelineTimebase)
					allocationDeviation := absInt64(
						int64(editSeconds)*totalWeight -
							int64(input.TargetDurationSeconds)*groupWeight,
					)
					overflowSeconds := ceilTicksToSeconds(shot.VoiceoverOverflowTicks, input.TimelineTimebase)
					edgeScore :=
						policy.RequestCountWeight +
							int64(shot.RequestedDurationSeconds)*policy.RequestDurationWeight +
							int64(shot.TrimDurationSeconds)*policy.TrimWeight +
							overflowSeconds*policy.VoiceoverOverflowWeight +
							allocationDeviation*policy.AllocationDeviationWeight +
							int64(mergePenalty)*policy.SalesBeatMergeWeight +
							int64(complexityPenalty+continuityPenalty)*policy.ComplexityWeight
					nextShots := append(append([]SegmentationShot(nil), current.Shots...), shot)
					signature := segmentationSignature(nextShots)
					nextKey := stateKey{BeatIndex: end, ElapsedSeconds: elapsed + editSeconds}
					next := storyboardSegmentationState{
						Score: current.Score + edgeScore, Shots: nextShots, Signature: signature,
					}
					existing, exists := states[nextKey]
					if !exists || betterSegmentationState(next, existing) {
						states[nextKey] = next
					}
				}
			}
		}
	}
	result, ok := states[stateKey{BeatIndex: len(beats), ElapsedSeconds: input.TargetDurationSeconds}]
	if !ok {
		return nil, Error{
			Code:    CodeStoryboardInvalid,
			Message: "当前脚本无法在用户选择的时长和视频模型档位内完成智能切分",
		}
	}
	for index := range result.Shots {
		result.Shots[index].ShotOrdinal = index + 1
	}
	return result.Shots, nil
}

func buildSegmentationShot(
	beats []StoryboardBeatInput,
	ordinal int,
	editSeconds int,
	timebase int64,
	envelope VideoExecutionEnvelope,
) (SegmentationShot, bool, error) {
	requestSeconds, routeKeys, ok := eligibleRoutesForEditDuration(envelope, editSeconds)
	if !ok {
		return SegmentationShot{}, false, nil
	}
	routeSetHash, err := hashStoryboardContract(map[string]any{
		"videoExecutionEnvelope":   envelope,
		"editDurationSeconds":      editSeconds,
		"requestedDurationSeconds": requestSeconds,
		"eligibleRouteKeys":        routeKeys,
	})
	if err != nil {
		return SegmentationShot{}, false, err
	}
	shot := SegmentationShot{
		ShotOrdinal:         ordinal,
		EditDurationSeconds: editSeconds, RequestedDurationSeconds: requestSeconds,
		TrimDurationSeconds: requestSeconds - editSeconds,
		EligibleRouteKeys:   routeKeys, EligibleRouteSetHash: routeSetHash,
	}
	for _, beat := range beats {
		shot.BeatOrdinals = append(shot.BeatOrdinals, beat.Ordinal)
		shot.LocalizationSegmentIDs = append(shot.LocalizationSegmentIDs, beat.LocalizationSegmentID)
		shot.SourceSegmentIDs = append(shot.SourceSegmentIDs, beat.SourceSegmentID)
		shot.EstimatedVoiceoverTicks += beat.EstimatedVoiceoverTicks
	}
	capacityTicks := int64(editSeconds) * timebase
	if shot.EstimatedVoiceoverTicks > capacityTicks {
		shot.VoiceoverOverflowTicks = shot.EstimatedVoiceoverTicks - capacityTicks
	}
	shot.TimingAdvisoryLevel = storyboardTimingAdvisoryLevel(
		shot.VoiceoverOverflowTicks,
		capacityTicks,
	)
	return shot, true, nil
}

func eligibleRoutesForEditDuration(envelope VideoExecutionEnvelope, editSeconds int) (int, []string, bool) {
	bestRequest := 0
	routeRequest := make(map[string]int, len(envelope.Routes))
	for _, route := range envelope.Routes {
		request := 0
		for _, duration := range route.ExecutableDurationSeconds {
			if duration >= editSeconds {
				request = duration
				break
			}
		}
		if request == 0 {
			continue
		}
		routeRequest[route.RouteKey] = request
		if bestRequest == 0 || request < bestRequest {
			bestRequest = request
		}
	}
	if bestRequest == 0 {
		return 0, nil, false
	}
	keys := make([]string, 0, len(routeRequest))
	for routeKey, request := range routeRequest {
		if request == bestRequest {
			keys = append(keys, routeKey)
		}
	}
	sort.Strings(keys)
	return bestRequest, keys, len(keys) > 0
}

func normalizeStoryboardBeatInputs(beats []StoryboardBeatInput) ([]StoryboardBeatInput, error) {
	if len(beats) == 0 {
		return nil, Error{Code: CodeStoryboardInvalid, Message: "分镜切分缺少脚本节拍"}
	}
	result := append([]StoryboardBeatInput(nil), beats...)
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Ordinal != result[j].Ordinal {
			return result[i].Ordinal < result[j].Ordinal
		}
		return result[i].LocalizationSegmentID < result[j].LocalizationSegmentID
	})
	seenLocalization := make(map[string]struct{}, len(result))
	lastOrdinal := 0
	for index := range result {
		beat := &result[index]
		if strings.TrimSpace(beat.LocalizationSegmentID) == "" ||
			strings.TrimSpace(beat.SourceSegmentID) == "" ||
			beat.Ordinal <= lastOrdinal ||
			beat.EstimatedVoiceoverTicks < 0 ||
			!isContractHash(beat.ContentHash) {
			return nil, Error{Code: CodeStoryboardInvalid, Message: "脚本节拍身份、顺序或时长无效"}
		}
		if _, exists := seenLocalization[beat.LocalizationSegmentID]; exists {
			return nil, Error{Code: CodeStoryboardInvalid, Message: "脚本节拍来源重复"}
		}
		seenLocalization[beat.LocalizationSegmentID] = struct{}{}
		lastOrdinal = beat.Ordinal
		beat.Continuity = normalizeJSONObject(beat.Continuity)
	}
	return result, nil
}

func storyboardBeatGroupCompatibility(
	beats []StoryboardBeatInput,
	policy storyboardSegmentationPolicy,
) (semanticPenalty int, continuityPenalty int, complexityPenalty int, ok bool) {
	if len(beats) == 0 {
		return 0, 0, 0, false
	}
	independentActions := 0
	visualComplexity := 0
	for index, beat := range beats {
		continuity := parseStoryboardContinuity(beat.Continuity)
		independentActions += intFromAny(continuity["independentAction"])
		visualComplexity += maxInt(1, intFromAny(continuity["visualComplexity"]))
		if index == 0 {
			continue
		}
		previous := beats[index-1]
		if previous.ForceBoundaryAfter || beat.ForceBoundaryBefore {
			return 0, 0, 0, false
		}
		previousContinuity := parseStoryboardContinuity(previous.Continuity)
		previousScene := normalizedAnyString(previousContinuity["sceneKey"])
		currentScene := normalizedAnyString(continuity["sceneKey"])
		if previousScene != "" && currentScene != "" && previousScene != currentScene {
			return 0, 0, 0, false
		}
		previousFinal := normalizedAnyString(previousContinuity["finalStateKey"])
		currentInitial := normalizedAnyString(continuity["initialStateKey"])
		if previousFinal != "" && currentInitial != "" && previousFinal != currentInitial {
			return 0, 0, 0, false
		}
		if !strings.EqualFold(strings.TrimSpace(previous.SalesBeat), strings.TrimSpace(beat.SalesBeat)) {
			semanticPenalty++
		}
		if previousScene == "" || currentScene == "" {
			continuityPenalty++
		}
	}
	if independentActions > policy.MaxIndependentActionsPerShot ||
		visualComplexity > policy.MaxVisualComplexityPerShot {
		return 0, 0, 0, false
	}
	if len(beats) > 1 {
		complexityPenalty = maxInt(0, visualComplexity-len(beats))
	}
	return semanticPenalty, continuityPenalty, complexityPenalty, true
}

func parseStoryboardContinuity(raw json.RawMessage) map[string]any {
	var result map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &result) != nil || result == nil {
		return map[string]any{}
	}
	return result
}

func normalizeJSONObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`{}`)
	}
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil || value == nil {
		return json.RawMessage(`{}`)
	}
	normalized, _ := json.Marshal(value)
	return normalized
}

func storyboardBeatRangeWeight(beats []StoryboardBeatInput, timebase int64) int64 {
	var total int64
	for _, beat := range beats {
		voiceSeconds := ceilTicksToSeconds(beat.EstimatedVoiceoverTicks, timebase)
		total += maxInt64(1, voiceSeconds)
	}
	return maxInt64(1, total)
}

func finalizeSegmentationPlan(input StoryboardSegmentationInput, shots []SegmentationShot) SegmentationPlan {
	plan := SegmentationPlan{
		ContractVersion: CommerceSegmentationPlanV1,
		Strategy:        input.Strategy, SegmentationPolicyVersion: input.SegmentationPolicyVersion,
		TargetDurationSeconds: input.TargetDurationSeconds, TimelineTimebase: input.TimelineTimebase,
		VideoExecutionEnvelopeHash: input.VideoExecutionEnvelopeHash,
		Shots:                      shots,
	}
	for _, shot := range shots {
		plan.TotalRequestedSeconds += shot.RequestedDurationSeconds
		plan.TotalTrimSeconds += shot.TrimDurationSeconds
		plan.EstimatedVoiceoverTicks += shot.EstimatedVoiceoverTicks
		plan.VoiceoverOverflowTicks += shot.VoiceoverOverflowTicks
	}
	plan.TimingAdvisoryLevel = storyboardTimingAdvisoryLevel(
		plan.VoiceoverOverflowTicks,
		int64(input.TargetDurationSeconds)*input.TimelineTimebase,
	)
	return plan
}

func betterSegmentationState(candidate, current storyboardSegmentationState) bool {
	if candidate.Score != current.Score {
		return candidate.Score < current.Score
	}
	if len(candidate.Shots) != len(current.Shots) {
		return len(candidate.Shots) < len(current.Shots)
	}
	return candidate.Signature < current.Signature
}

func segmentationSignature(shots []SegmentationShot) string {
	parts := make([]string, 0, len(shots))
	for _, shot := range shots {
		parts = append(parts, fmt.Sprintf(
			"%04d:%04d:%s",
			shot.EditDurationSeconds,
			shot.RequestedDurationSeconds,
			strings.Join(shot.EligibleRouteKeys, ","),
		))
	}
	return strings.Join(parts, "|")
}

func storyboardTimingAdvisoryLevel(overflowTicks, capacityTicks int64) string {
	if overflowTicks <= 0 {
		return "none"
	}
	if capacityTicks <= 0 {
		return "critical"
	}
	ratio := float64(overflowTicks) / float64(capacityTicks)
	switch {
	case ratio > 0.5:
		return "critical"
	case ratio > 0.15:
		return "warning"
	default:
		return "info"
	}
}

func hashStoryboardContract(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func ceilTicksToSeconds(ticks, timebase int64) int64 {
	if ticks <= 0 || timebase <= 0 {
		return 0
	}
	return int64(math.Ceil(float64(ticks) / float64(timebase)))
}

func normalizedAnyString(value any) string {
	text, _ := value.(string)
	return strings.ToLower(strings.TrimSpace(text))
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(math.Round(typed))
	case bool:
		if typed {
			return 1
		}
	}
	return 0
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
