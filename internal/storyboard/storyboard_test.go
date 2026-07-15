package storyboard

import (
	"fmt"
	"math"
	"testing"
)

func TestDefaultTimebaseUsesFilmFrameRate(t *testing.T) {
	timebase := DefaultTimebase()
	frame, err := timebase.TicksPerFrame()
	if err != nil {
		t.Fatalf("TicksPerFrame: %v", err)
	}
	if timebase.TicksPerSecond != 90_000 || timebase.FPSNumerator != 24 || timebase.FPSDenominator != 1 || frame != 3_750 {
		t.Fatalf("timebase = %+v frame=%d", timebase, frame)
	}
	if got := timebase.SecondsToFrameTicksCeil(1.01); got != 93_750 {
		t.Fatalf("ceil frame ticks = %d, want 93750", got)
	}
}

func TestEstimateChineseDialogueUsesConfirmedRatesAndPauses(t *testing.T) {
	timebase := DefaultTimebase()
	normal, err := EstimateChineseDialogue(DialogueEstimateInput{Text: "你终于来了。", Delivery: "正常", Language: "zh-CN", Timebase: timebase})
	if err != nil {
		t.Fatalf("normal dialogue: %v", err)
	}
	if normal.SpokenCharacterCount != 5 || normal.CharactersPerSecond != 3.5 || math.Abs(normal.PunctuationPauseSeconds-0.35) > 0.000001 {
		t.Fatalf("normal estimate = %+v", normal)
	}
	if !timebase.IsFrameAligned(normal.DurationTicks) || normal.DurationSeconds < 5.0/3.5+0.35 {
		t.Fatalf("normal duration = %+v", normal)
	}
	fast, err := EstimateChineseDialogue(DialogueEstimateInput{Text: "快走！别回头！", Delivery: "急促", Language: "zh-CN", Timebase: timebase})
	if err != nil {
		t.Fatalf("fast dialogue: %v", err)
	}
	if fast.CharactersPerSecond != 4.0 {
		t.Fatalf("fast rate = %f", fast.CharactersPerSecond)
	}
	slow, err := EstimateChineseDialogue(DialogueEstimateInput{Text: "我不想忘记你……", Delivery: "哽咽", Language: "zh-CN", Timebase: timebase})
	if err != nil {
		t.Fatalf("slow dialogue: %v", err)
	}
	if slow.CharactersPerSecond != 3.0 || slow.DeliveryPauseSeconds != 0.30 || slow.PunctuationPauseSeconds != 0.70 {
		t.Fatalf("slow estimate = %+v", slow)
	}
}

func TestBuildTimingBlocksUsesParallelTrackMaximum(t *testing.T) {
	timebase := DefaultTimebase()
	blocks, err := BuildTimingBlocks([]TimingUnit{
		{ID: "dialogue-1", Ordinal: 0, Type: UnitDialogue, Track: TrackAudio, ParallelGroup: "beat-1", DurationTicks: timebase.SecondsToTicks(4.8)},
		{ID: "action-1", Ordinal: 1, Type: UnitAction, Track: TrackVisual, ParallelGroup: "beat-1", DurationTicks: timebase.SecondsToTicks(3)},
		{ID: "pause-1", Ordinal: 2, Type: UnitPause, Track: TrackAudio, DurationTicks: timebase.SecondsToTicks(1)},
	})
	if err != nil {
		t.Fatalf("BuildTimingBlocks: %v", err)
	}
	if len(blocks) != 2 || blocks[0].DurationTicks != timebase.SecondsToTicks(4.8) || blocks[1].StartTick != blocks[0].EndTick {
		t.Fatalf("blocks = %+v", blocks)
	}
	if got := blocks[1].EndTick; got != timebase.SecondsToTicks(5.8) {
		t.Fatalf("episode duration ticks = %d", got)
	}
}

func TestPlanShotBoundariesPreservesExactCoverageAndDialogueSlices(t *testing.T) {
	timebase := DefaultTimebase()
	dialogueDuration := timebase.SecondsToTicks(25)
	blocks, err := BuildTimingBlocks([]TimingUnit{
		{ID: "dialogue-1", Ordinal: 0, Type: UnitDialogue, Track: TrackAudio, SourceText: "第一句很重要。第二句必须保留。第三句也不能重复。", DurationTicks: dialogueDuration},
		{ID: "action-1", Ordinal: 1, Type: UnitAction, Track: TrackVisual, DurationTicks: timebase.SecondsToTicks(7)},
	})
	if err != nil {
		t.Fatalf("BuildTimingBlocks: %v", err)
	}
	shots, err := PlanShotBoundaries(blocks, PlanOptions{Timebase: timebase, Pacing: PacingProfileByKey("standard", timebase)})
	if err != nil {
		t.Fatalf("PlanShotBoundaries: %v", err)
	}
	if len(shots) < 3 {
		t.Fatalf("shot count = %d, want at least 3 for 32 seconds", len(shots))
	}
	if err := ValidateExactCoverage(shots, flattenTimingUnits(blocks), 0, blocks[len(blocks)-1].EndTick); err != nil {
		t.Fatalf("ValidateExactCoverage: %v", err)
	}
	spanCount := 0
	covered := int64(0)
	for _, shot := range shots {
		if !timebase.IsFrameAligned(shot.StartTick) || !timebase.IsFrameAligned(shot.EndTick) {
			t.Fatalf("shot is not frame aligned: %+v", shot)
		}
		for _, span := range shot.Spans {
			if span.TimingUnitID == "dialogue-1" {
				spanCount++
				covered += span.EndTick - span.StartTick
			}
		}
	}
	if spanCount < 2 || covered != dialogueDuration {
		t.Fatalf("dialogue spans=%d covered=%d want=%d", spanCount, covered, dialogueDuration)
	}
}

func TestPlanShotBoundariesIncludesSemanticMinimumInOptimization(t *testing.T) {
	timebase := DefaultTimebase()
	unitDuration := timebase.SecondsToFrameTicksCeil(7)
	units := make([]TimingUnit, 0, 4)
	for index := 0; index < 4; index++ {
		start := int64(index) * unitDuration
		units = append(units, TimingUnit{
			ID:            fmt.Sprintf("beat-%d", index+1),
			Ordinal:       index,
			Type:          UnitAction,
			Track:         TrackVisual,
			StartTick:     start,
			EndTick:       start + unitDuration,
			DurationTicks: unitDuration,
		})
	}
	blocks := []TimingBlock{{
		ID:            "scene-1",
		Ordinal:       0,
		StartTick:     0,
		EndTick:       int64(len(units)) * unitDuration,
		DurationTicks: int64(len(units)) * unitDuration,
		Units:         units,
	}}
	profile := PacingProfileByKey("slow", timebase)

	baseline, err := PlanShotBoundaries(blocks, PlanOptions{Timebase: timebase, Pacing: profile})
	if err != nil {
		t.Fatalf("unconstrained plan: %v", err)
	}
	if len(baseline) >= 4 {
		t.Fatalf("baseline shot count = %d, test fixture must reproduce a cheaper sub-minimum path", len(baseline))
	}

	shots, err := PlanShotBoundaries(blocks, PlanOptions{
		Timebase:        timebase,
		Pacing:          profile,
		SemanticMinimum: 4,
	})
	if err != nil {
		t.Fatalf("semantic-minimum plan: %v", err)
	}
	if len(shots) < 4 {
		t.Fatalf("shot count = %d, want at least semantic minimum 4", len(shots))
	}
	if err := ValidateExactCoverage(shots, units, 0, blocks[0].EndTick); err != nil {
		t.Fatalf("ValidateExactCoverage: %v", err)
	}
}

func TestPlanShotBoundariesDoesNotApplyLegacyShotOrDurationCaps(t *testing.T) {
	timebase := DefaultTimebase()
	units := make([]TimingUnit, 0, 64)
	frameTicks, err := timebase.TicksPerFrame()
	if err != nil {
		t.Fatalf("TicksPerFrame: %v", err)
	}
	for index := 0; index < 64; index++ {
		frames := int64(153)
		if index < 48 {
			frames = 154
		}
		units = append(units, TimingUnit{
			ID:            "unit-" + string(rune('A'+index%26)) + string(rune('a'+index/26)),
			Ordinal:       index,
			Type:          UnitAction,
			Track:         TrackVisual,
			DurationTicks: frames * frameTicks,
		})
	}
	blocks, err := BuildTimingBlocks(units)
	if err != nil {
		t.Fatalf("BuildTimingBlocks: %v", err)
	}
	shots, err := PlanShotBoundaries(blocks, PlanOptions{Timebase: timebase, Pacing: PacingProfileByKey("standard", timebase)})
	if err != nil {
		t.Fatalf("PlanShotBoundaries: %v", err)
	}
	if len(shots) <= 24 {
		t.Fatalf("shot count = %d, legacy 24-shot cap appears active", len(shots))
	}
	total := int64(0)
	for _, shot := range shots {
		total += shot.DurationTicks
	}
	if math.Abs(timebase.TicksToSeconds(total)-timebase.TicksToSeconds(blocks[len(blocks)-1].EndTick)) > 0.000001 {
		t.Fatalf("planned duration = %f, block duration = %f", timebase.TicksToSeconds(total), timebase.TicksToSeconds(blocks[len(blocks)-1].EndTick))
	}
}

func TestPlanShotBoundariesSplitsLongUnpunctuatedVoiceover(t *testing.T) {
	timebase := DefaultTimebase()
	duration := timebase.SecondsToFrameTicksCeil(25)
	unit := TimingUnit{
		ID: "voiceover-long", Ordinal: 0, Type: UnitVoiceover, Track: TrackAudio,
		SourceText: "这是一段没有标点但必须完整保留并适配视频模型时长的长旁白",
		StartTick:  0, EndTick: duration, DurationTicks: duration,
	}
	blocks := []TimingBlock{{
		ID: "block-1", Ordinal: 0, StartTick: 0, EndTick: duration, DurationTicks: duration, Units: []TimingUnit{unit},
	}}
	shots, err := PlanShotBoundaries(blocks, PlanOptions{Timebase: timebase, Pacing: PacingProfileByKey("standard", timebase)})
	if err != nil {
		t.Fatalf("PlanShotBoundaries: %v", err)
	}
	if len(shots) < 2 {
		t.Fatalf("shot count = %d, want a duration-safe split", len(shots))
	}
	if shots[0].StartTick != 0 || shots[len(shots)-1].EndTick != duration {
		t.Fatalf("shot coverage = %d..%d, want 0..%d", shots[0].StartTick, shots[len(shots)-1].EndTick, duration)
	}
}

func TestPlanShotBoundariesKeepsAdjacentPreferencesFeasible(t *testing.T) {
	timebase := DefaultTimebase()
	unitDuration := timebase.SecondsToFrameTicksCeil(0.9)
	units := make([]TimingUnit, 0, 40)
	for index := 0; index < 40; index++ {
		start := int64(index) * unitDuration
		units = append(units, TimingUnit{
			ID: "preferred-" + fmt.Sprint(index), Ordinal: index, Type: UnitAction, Track: TrackVisual,
			StartTick: start, EndTick: start + unitDuration, DurationTicks: unitDuration, PreferBoundaryAfter: true,
		})
	}
	blocks := []TimingBlock{{
		ID: "long-scene", Ordinal: 0, StartTick: 0, EndTick: int64(len(units)) * unitDuration,
		DurationTicks: int64(len(units)) * unitDuration, Units: units,
	}}
	shots, err := PlanShotBoundaries(blocks, PlanOptions{Timebase: timebase, Pacing: PacingProfileByKey("standard", timebase)})
	if err != nil {
		t.Fatalf("PlanShotBoundaries: %v", err)
	}
	if len(shots) == 0 || shots[len(shots)-1].EndTick != blocks[0].EndTick {
		t.Fatalf("shots do not cover scene: %+v", shots)
	}
}
