package commerce

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestPlanStoryboardSegmentationSmartAvoidsFragmentedProviderRequests(t *testing.T) {
	input := segmentationTestInput(t, StoryboardStrategySmart, 15, []int{6, 10, 12, 16})
	input.Beats = segmentationTestBeats(t, 5, 2)

	plan, err := PlanStoryboardSegmentation(input)
	if err != nil {
		t.Fatalf("PlanStoryboardSegmentation() error = %v", err)
	}
	if len(plan.Shots) >= len(input.Beats) {
		t.Fatalf("shot count = %d, want fewer than %d beats", len(plan.Shots), len(input.Beats))
	}
	if got := totalEditDuration(plan.Shots); got != 15 {
		t.Fatalf("total edit duration = %d, want 15", got)
	}
	for _, shot := range plan.Shots {
		if shot.RequestedDurationSeconds < shot.EditDurationSeconds {
			t.Fatalf("shot %d request %d is shorter than edit %d", shot.ShotOrdinal, shot.RequestedDurationSeconds, shot.EditDurationSeconds)
		}
		if len(shot.EligibleRouteKeys) == 0 || len(shot.EligibleRouteSetHash) != 64 {
			t.Fatalf("shot %d has no frozen eligible route set", shot.ShotOrdinal)
		}
	}
}

func TestPlanStoryboardSegmentationSmartHonorsForcedSceneBoundary(t *testing.T) {
	input := segmentationTestInput(t, StoryboardStrategySmart, 15, []int{6, 10, 12, 16})
	input.Beats = segmentationTestBeats(t, 4, 2)
	input.Beats[1].ForceBoundaryAfter = true
	input.Beats[2].ForceBoundaryBefore = true
	input.Beats[0].Continuity = json.RawMessage(`{"sceneKey":"roadside","visualComplexity":1}`)
	input.Beats[1].Continuity = json.RawMessage(`{"sceneKey":"roadside","visualComplexity":1}`)
	input.Beats[2].Continuity = json.RawMessage(`{"sceneKey":"studio","visualComplexity":1}`)
	input.Beats[3].Continuity = json.RawMessage(`{"sceneKey":"studio","visualComplexity":1}`)

	plan, err := PlanStoryboardSegmentation(input)
	if err != nil {
		t.Fatalf("PlanStoryboardSegmentation() error = %v", err)
	}
	if len(plan.Shots) != 2 {
		t.Fatalf("shot count = %d, want 2", len(plan.Shots))
	}
	if got := totalEditDuration(plan.Shots); got != 15 {
		t.Fatalf("total edit duration = %d, want 15", got)
	}
	if len(plan.Shots[0].BeatOrdinals) != 2 || len(plan.Shots[1].BeatOrdinals) != 2 {
		t.Fatalf("forced scene groups = %#v", plan.Shots)
	}
}

func TestPlanStoryboardSegmentationSingleTakeUsesNearestProviderDuration(t *testing.T) {
	input := segmentationTestInput(t, StoryboardStrategySingleTake, 15, []int{6, 10, 12, 16})
	input.Beats = segmentationTestBeats(t, 5, 1)

	plan, err := PlanStoryboardSegmentation(input)
	if err != nil {
		t.Fatalf("PlanStoryboardSegmentation() error = %v", err)
	}
	if len(plan.Shots) != 1 {
		t.Fatalf("shot count = %d, want 1", len(plan.Shots))
	}
	shot := plan.Shots[0]
	if shot.EditDurationSeconds != 15 || shot.RequestedDurationSeconds != 16 || shot.TrimDurationSeconds != 1 {
		t.Fatalf("single-take timing = %#v", shot)
	}
}

func TestPlanStoryboardSegmentationVoiceoverOverflowIsAdvisory(t *testing.T) {
	input := segmentationTestInput(t, StoryboardStrategySingleTake, 15, []int{16})
	input.Beats = segmentationTestBeats(t, 2, 12)

	plan, err := PlanStoryboardSegmentation(input)
	if err != nil {
		t.Fatalf("PlanStoryboardSegmentation() error = %v", err)
	}
	if plan.VoiceoverOverflowTicks <= 0 || plan.TimingAdvisoryLevel == "none" {
		t.Fatalf("overflow advisory = %#v", plan)
	}
}

func TestPlanStoryboardSegmentationIsDeterministic(t *testing.T) {
	input := segmentationTestInput(t, StoryboardStrategySmart, 30, []int{6, 10, 12, 16})
	input.Beats = segmentationTestBeats(t, 8, 3)

	first, err := PlanStoryboardSegmentation(input)
	if err != nil {
		t.Fatalf("first PlanStoryboardSegmentation() error = %v", err)
	}
	second, err := PlanStoryboardSegmentation(input)
	if err != nil {
		t.Fatalf("second PlanStoryboardSegmentation() error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("deterministic plans differ:\nfirst=%#v\nsecond=%#v", first, second)
	}
}

func TestPlanStoryboardSegmentationNormalizesDurationCapabilities(t *testing.T) {
	input := segmentationTestInput(t, StoryboardStrategySingleTake, 15, []int{16, 6, 10, 6, 12, 16})
	input.Beats = segmentationTestBeats(t, 1, 3)

	plan, err := PlanStoryboardSegmentation(input)
	if err != nil {
		t.Fatalf("PlanStoryboardSegmentation() error = %v", err)
	}
	if got := plan.Shots[0].RequestedDurationSeconds; got != 16 {
		t.Fatalf("requested duration = %d, want 16", got)
	}
}

func TestPlanStoryboardSegmentationPreservesSourceCoverage(t *testing.T) {
	input := segmentationTestInput(t, StoryboardStrategySmart, 15, []int{6, 10, 12, 16})
	input.Beats = segmentationTestBeats(t, 5, 2)

	plan, err := PlanStoryboardSegmentation(input)
	if err != nil {
		t.Fatalf("PlanStoryboardSegmentation() error = %v", err)
	}
	seenLocalization := map[string]int{}
	seenSource := map[string]int{}
	seenOrdinal := map[int]int{}
	for _, shot := range plan.Shots {
		for _, value := range shot.LocalizationSegmentIDs {
			seenLocalization[value]++
		}
		for _, value := range shot.SourceSegmentIDs {
			seenSource[value]++
		}
		for _, value := range shot.BeatOrdinals {
			seenOrdinal[value]++
		}
	}
	for _, beat := range input.Beats {
		if seenLocalization[beat.LocalizationSegmentID] != 1 ||
			seenSource[beat.SourceSegmentID] != 1 ||
			seenOrdinal[beat.Ordinal] != 1 {
			t.Fatalf(
				"beat %d coverage = localization:%d source:%d ordinal:%d, want exactly once",
				beat.Ordinal,
				seenLocalization[beat.LocalizationSegmentID],
				seenSource[beat.SourceSegmentID],
				seenOrdinal[beat.Ordinal],
			)
		}
	}
}

func TestPlanStoryboardSegmentationFreezesOnlyRoutesSupportingSelectedRequest(t *testing.T) {
	input := segmentationTestInput(t, StoryboardStrategySingleTake, 9, []int{6, 10})
	input.Beats = segmentationTestBeats(t, 1, 2)
	routeB := input.VideoExecutionEnvelope.Routes[0]
	routeB.RouteKey = "route-b"
	routeB.ModelProfileID = "profile-b"
	routeB.ModelProfileBindingID = "binding-b"
	routeB.ProviderModelID = "model-b"
	routeB.ProviderAccountID = "account-b"
	routeB.ModelKey = "video-b"
	routeB.CapabilitySnapshotHash = strings.Repeat("c", 64)
	routeB.ExecutableDurationSeconds = []int{6, 12}
	input.VideoExecutionEnvelope.Routes = append(input.VideoExecutionEnvelope.Routes, routeB)
	input.VideoExecutionEnvelope.ExecutableDurationSeconds = []int{6, 10, 12}
	NormalizeVideoExecutionEnvelope(&input.VideoExecutionEnvelope)
	input.VideoExecutionEnvelopeHash, _ = hashStoryboardContract(input.VideoExecutionEnvelope)

	plan, err := PlanStoryboardSegmentation(input)
	if err != nil {
		t.Fatalf("PlanStoryboardSegmentation() error = %v", err)
	}
	shot := plan.Shots[0]
	if shot.RequestedDurationSeconds != 10 {
		t.Fatalf("requested duration = %d, want 10", shot.RequestedDurationSeconds)
	}
	if !reflect.DeepEqual(shot.EligibleRouteKeys, []string{"route-a"}) {
		t.Fatalf("eligible routes = %#v, want only route-a", shot.EligibleRouteKeys)
	}
}

func TestPlanStoryboardSegmentationCapabilityBoundaries(t *testing.T) {
	tests := []struct {
		name      string
		strategy  StoryboardStrategy
		target    int
		durations []int
		want      int
		wantError bool
	}{
		{name: "no exact duration uses next executable option", strategy: StoryboardStrategySingleTake, target: 7, durations: []int{6, 10}, want: 10},
		{name: "single duration option", strategy: StoryboardStrategySingleTake, target: 15, durations: []int{16}, want: 16},
		{name: "minimum target", strategy: StoryboardStrategySingleTake, target: 1, durations: []int{6}, want: 6},
		{name: "no legal route", strategy: StoryboardStrategySingleTake, target: 17, durations: []int{6, 10, 12, 16}, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := segmentationTestInput(t, test.strategy, test.target, test.durations)
			input.Beats = segmentationTestBeats(t, 1, 1)
			plan, err := PlanStoryboardSegmentation(input)
			if test.wantError {
				if err == nil {
					t.Fatal("PlanStoryboardSegmentation() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("PlanStoryboardSegmentation() error = %v", err)
			}
			if plan.Shots[0].RequestedDurationSeconds != test.want {
				t.Fatalf("requested duration = %d, want %d", plan.Shots[0].RequestedDurationSeconds, test.want)
			}
			if got := totalEditDuration(plan.Shots); got != test.target {
				t.Fatalf("total edit duration = %d, want %d", got, test.target)
			}
		})
	}
}

func TestPlanStoryboardSegmentationRejectsUnsupportedResolution(t *testing.T) {
	input := segmentationTestInput(t, StoryboardStrategySmart, 15, []int{6, 10, 12, 16})
	input.Beats = segmentationTestBeats(t, 2, 2)
	input.VideoExecutionEnvelope.TargetResolution = "1080p"
	input.VideoExecutionEnvelopeHash, _ = hashStoryboardContract(input.VideoExecutionEnvelope)

	if _, err := PlanStoryboardSegmentation(input); err == nil {
		t.Fatal("PlanStoryboardSegmentation() error = nil, want unsupported resolution")
	}
}

func TestVideoExecutionEnvelopeRejectsIncompleteFrozenIdentity(t *testing.T) {
	input := segmentationTestInput(t, StoryboardStrategySmart, 15, []int{6, 10, 12, 16})
	tests := []struct {
		name   string
		mutate func(*VideoExecutionEnvelope)
	}{
		{
			name: "invalid profile snapshot hash",
			mutate: func(envelope *VideoExecutionEnvelope) {
				envelope.VideoProductionProfileSnapshotHash = "not-a-contract-hash"
			},
		},
		{
			name: "missing route model profile id",
			mutate: func(envelope *VideoExecutionEnvelope) {
				envelope.Routes[0].ModelProfileID = ""
			},
		},
		{
			name: "missing route model key",
			mutate: func(envelope *VideoExecutionEnvelope) {
				envelope.Routes[0].ModelKey = ""
			},
		},
		{
			name: "route model profile mismatch",
			mutate: func(envelope *VideoExecutionEnvelope) {
				envelope.Routes[0].ModelProfileKey = "another_video_profile"
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			envelope := input.VideoExecutionEnvelope
			envelope.Routes = append([]VideoExecutionRoute(nil), envelope.Routes...)
			test.mutate(&envelope)
			if err := envelope.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want frozen identity validation error")
			}
		})
	}
}

func TestVideoExecutionEnvelopeProjectionMustEqualRouteDurationUnion(t *testing.T) {
	input := segmentationTestInput(t, StoryboardStrategySmart, 15, []int{6, 10, 12, 16})
	envelope := input.VideoExecutionEnvelope
	envelope.ExecutableDurationSeconds = append(envelope.ExecutableDurationSeconds, 20)

	if err := envelope.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-executable projected duration error")
	}
}

func TestCanonicalizeVideoExecutionEnvelopeProvidesStableProducerConsumerHash(t *testing.T) {
	input := segmentationTestInput(t, StoryboardStrategySmart, 15, []int{6, 10, 12, 16})
	envelope := input.VideoExecutionEnvelope
	envelope.TargetResolution = " 720P "
	envelope.AspectRatio = " 9:16 "
	envelope.ExecutableDurationSeconds = []int{16, 6, 10, 12, 6}
	envelope.Routes[0].RouteKey = " route-a "
	envelope.Routes[0].ExecutableDurationSeconds = []int{12, 6, 16, 10, 6}
	envelope.Routes[0].Resolutions = []string{"720P", " 720p "}
	envelope.Routes[0].AspectRatios = []string{" 9:16 "}

	canonical, hash, err := CanonicalizeVideoExecutionEnvelope(envelope)
	if err != nil {
		t.Fatalf("CanonicalizeVideoExecutionEnvelope() error = %v", err)
	}
	if envelope.TargetResolution != " 720P " || envelope.Routes[0].RouteKey != " route-a " {
		t.Fatal("CanonicalizeVideoExecutionEnvelope() mutated the caller-owned envelope")
	}
	replayed, replayedHash, err := CanonicalizeVideoExecutionEnvelope(canonical)
	if err != nil {
		t.Fatalf("replayed CanonicalizeVideoExecutionEnvelope() error = %v", err)
	}
	if replayedHash != hash || !reflect.DeepEqual(replayed, canonical) {
		t.Fatalf("canonical replay changed contract: hash %q -> %q", hash, replayedHash)
	}

	input.VideoExecutionEnvelope = envelope
	input.VideoExecutionEnvelopeHash = hash
	input.Beats = segmentationTestBeats(t, 2, 2)
	if _, err := PlanStoryboardSegmentation(input); err != nil {
		t.Fatalf("PlanStoryboardSegmentation() rejected producer hash: %v", err)
	}
}

func TestVideoExecutionEnvelopeTreatsAspectRatioAsAdvisory(t *testing.T) {
	input := segmentationTestInput(t, StoryboardStrategySmart, 15, []int{6, 10, 12, 16})
	envelope := input.VideoExecutionEnvelope
	envelope.Routes[0].AspectRatios = []string{"16:9"}

	if err := envelope.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want aspect ratio to be advisory", err)
	}
}

func segmentationTestInput(t *testing.T, strategy StoryboardStrategy, target int, durations []int) StoryboardSegmentationInput {
	t.Helper()
	envelope := VideoExecutionEnvelope{
		ContractVersion:                    CommerceVideoEnvelopeV1,
		ProjectProductionGenerationID:      "generation-1",
		VideoProductionBindingID:           "video-binding-1",
		VideoProductionBindingRevision:     1,
		VideoProductionProfileVersionID:    "profile-version-1",
		VideoProductionProfileSnapshotHash: strings.Repeat("a", 64),
		ModelProfileKey:                    "video_generation_default",
		TargetResolution:                   "720p",
		AspectRatio:                        "9:16",
		Routes: []VideoExecutionRoute{{
			RouteKey: "route-a", ModelProfileID: "profile-a", ModelProfileKey: "video_generation_default",
			ModelProfileBindingID: "binding-a", ProviderModelID: "model-a",
			ProviderAccountID: "account-a", ModelKey: "video-a",
			Priority: 100, Weight: 100, VariantKey: "default",
			CapabilitySnapshotHash:    strings.Repeat("b", 64),
			ExecutableDurationSeconds: append([]int(nil), durations...),
			Resolutions:               []string{"720p"}, AspectRatios: []string{"9:16"},
		}},
		ExecutableDurationSeconds: append([]int(nil), durations...),
	}
	NormalizeVideoExecutionEnvelope(&envelope)
	hash, err := hashStoryboardContract(envelope)
	if err != nil {
		t.Fatalf("hash envelope: %v", err)
	}
	return StoryboardSegmentationInput{
		Strategy: strategy, SegmentationPolicyVersion: CommerceSegmentationPolicyV2,
		TargetDurationSeconds: target, TimelineTimebase: 1000,
		VideoExecutionEnvelope: envelope, VideoExecutionEnvelopeHash: hash,
	}
}

func segmentationTestBeats(t *testing.T, count int, voiceoverSeconds int) []StoryboardBeatInput {
	t.Helper()
	result := make([]StoryboardBeatInput, 0, count)
	for index := 0; index < count; index++ {
		contentHash, err := hashStoryboardContract(map[string]any{"ordinal": index + 1})
		if err != nil {
			t.Fatalf("hash beat: %v", err)
		}
		result = append(result, StoryboardBeatInput{
			LocalizationSegmentID: "localized-" + string(rune('a'+index)),
			SourceSegmentID:       "source-" + string(rune('a'+index)),
			Ordinal:               index + 1, SalesBeat: "beat-" + string(rune('a'+index)),
			VoiceoverText: "旁白", VisualIntent: "同一场景连续展示商品",
			EstimatedVoiceoverTicks: int64(voiceoverSeconds * 1000),
			Required:                true,
			Continuity:              json.RawMessage(`{"sceneKey":"roadside","visualComplexity":1}`),
			ContentHash:             contentHash,
		})
	}
	return result
}

func totalEditDuration(shots []SegmentationShot) int {
	total := 0
	for _, shot := range shots {
		total += shot.EditDurationSeconds
	}
	return total
}
