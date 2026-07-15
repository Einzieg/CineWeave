package storyboard

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

var ErrNoValidShotPlan = errors.New("no valid storyboard shot plan")

type CutKind string

const (
	CutSceneBoundary CutKind = "scene_boundary"
	CutForced        CutKind = "forced"
	CutBlockBoundary CutKind = "block_boundary"
	CutUnitBoundary  CutKind = "unit_boundary"
	CutSpeakerChange CutKind = "speaker_change"
	CutSentence      CutKind = "sentence"
	CutClause        CutKind = "clause"
	CutActionBeat    CutKind = "action_beat"
	CutDurationGuard CutKind = "duration_guard"
	CutPreferred     CutKind = "preferred"
)

type CutPoint struct {
	Tick       int64   `json:"tick"`
	Kind       CutKind `json:"kind"`
	Penalty    float64 `json:"penalty"`
	Forced     bool    `json:"forced"`
	SplitsUnit bool    `json:"splitsUnit"`
}

type PacingProfile struct {
	Key                      string  `json:"key"`
	MinimumShotTicks         int64   `json:"minimumShotTicks"`
	TargetShotTicks          int64   `json:"targetShotTicks"`
	MaximumTargetShotTicks   int64   `json:"maximumTargetShotTicks"`
	AbnormalMaximumShotTicks int64   `json:"abnormalMaximumShotTicks"`
	DurationDeviationWeight  float64 `json:"durationDeviationWeight"`
	SplitDialoguePenalty     float64 `json:"splitDialoguePenalty"`
	ExcessiveShotPenalty     float64 `json:"excessiveShotPenalty"`
}

func PacingProfileByKey(key string, timebase Timebase) PacingProfile {
	if timebase.TicksPerSecond == 0 {
		timebase = DefaultTimebase()
	}
	profile := PacingProfile{
		Key:                      "standard",
		MinimumShotTicks:         timebase.SecondsToFrameTicksCeil(1.5),
		TargetShotTicks:          timebase.SecondsToFrameTicksCeil(7),
		MaximumTargetShotTicks:   timebase.SecondsToFrameTicksCeil(8),
		AbnormalMaximumShotTicks: timebase.SecondsToFrameTicksCeil(18),
		DurationDeviationWeight:  1,
		SplitDialoguePenalty:     8,
		ExcessiveShotPenalty:     0.2,
	}
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "fast":
		profile.Key = "fast"
		profile.TargetShotTicks = timebase.SecondsToFrameTicksCeil(4)
		profile.MaximumTargetShotTicks = timebase.SecondsToFrameTicksCeil(5)
		profile.AbnormalMaximumShotTicks = timebase.SecondsToFrameTicksCeil(12)
	case "slow":
		profile.Key = "slow"
		profile.TargetShotTicks = timebase.SecondsToFrameTicksCeil(10)
		profile.MaximumTargetShotTicks = timebase.SecondsToFrameTicksCeil(12)
		profile.AbnormalMaximumShotTicks = timebase.SecondsToFrameTicksCeil(24)
	}
	return profile
}

func ScalePacingProfile(profile PacingProfile, scale float64, timebase Timebase) PacingProfile {
	if scale <= 0 {
		return profile
	}
	profile.MinimumShotTicks = timebase.QuantizeTickCeil(int64(math.Round(float64(profile.MinimumShotTicks) * scale)))
	profile.TargetShotTicks = timebase.QuantizeTickCeil(int64(math.Round(float64(profile.TargetShotTicks) * scale)))
	profile.MaximumTargetShotTicks = timebase.QuantizeTickCeil(int64(math.Round(float64(profile.MaximumTargetShotTicks) * scale)))
	profile.AbnormalMaximumShotTicks = timebase.QuantizeTickCeil(int64(math.Round(float64(profile.AbnormalMaximumShotTicks) * scale)))
	return profile
}

type TimingSpan struct {
	TimingUnitID string `json:"timingUnitId"`
	StartTick    int64  `json:"startTick"`
	EndTick      int64  `json:"endTick"`
	Ordinal      int    `json:"ordinal"`
}

type ShotDraft struct {
	Ordinal       int          `json:"ordinal"`
	StartTick     int64        `json:"startTick"`
	EndTick       int64        `json:"endTick"`
	DurationTicks int64        `json:"durationTicks"`
	OneTake       bool         `json:"oneTake"`
	Spans         []TimingSpan `json:"spans"`
}

type PlanOptions struct {
	Timebase        Timebase
	Pacing          PacingProfile
	OneTake         bool
	SafetyMaxShots  int
	UserShotBudget  int
	SemanticMinimum int
}

func BuildLegalCutPoints(blocks []TimingBlock, profile PacingProfile, timebase Timebase) []CutPoint {
	if timebase.TicksPerSecond == 0 {
		timebase = DefaultTimebase()
	}
	byTick := map[int64]CutPoint{}
	add := func(point CutPoint) {
		point.Tick = timebase.QuantizeTickNearest(point.Tick)
		if current, ok := byTick[point.Tick]; !ok || point.Forced || (!current.Forced && point.Penalty < current.Penalty) {
			byTick[point.Tick] = point
		}
	}
	if len(blocks) == 0 {
		return nil
	}
	add(CutPoint{Tick: blocks[0].StartTick, Kind: CutSceneBoundary, Forced: true})
	add(CutPoint{Tick: blocks[len(blocks)-1].EndTick, Kind: CutSceneBoundary, Forced: true})
	previousSpeaker := ""
	for _, block := range blocks {
		add(CutPoint{Tick: block.StartTick, Kind: CutBlockBoundary, Penalty: 0.5})
		add(CutPoint{Tick: block.EndTick, Kind: CutBlockBoundary, Penalty: 0.5})
		for _, unit := range block.Units {
			if unit.ForceBoundaryBefore {
				add(CutPoint{Tick: unit.StartTick, Kind: CutForced, Forced: true})
			} else {
				add(CutPoint{Tick: unit.StartTick, Kind: CutUnitBoundary, Penalty: 0.25})
			}
			if unit.ForceBoundaryAfter {
				add(CutPoint{Tick: unit.EndTick, Kind: CutForced, Forced: true})
			} else if unit.PreferBoundaryAfter {
				add(CutPoint{Tick: unit.EndTick, Kind: CutPreferred, Penalty: -0.75})
			} else {
				add(CutPoint{Tick: unit.EndTick, Kind: CutUnitBoundary, Penalty: 0.25})
			}
			if unit.Speaker != "" && previousSpeaker != "" && unit.Speaker != previousSpeaker {
				add(CutPoint{Tick: unit.StartTick, Kind: CutSpeakerChange, Penalty: 0.05})
			}
			if unit.Speaker != "" {
				previousSpeaker = unit.Speaker
			}
			for _, point := range punctuationCutPoints(unit, timebase) {
				add(point)
			}
			if (unit.Type == UnitAction || unit.Type == UnitReaction || unit.Type == UnitEstablishing || unit.Type == UnitAmbientHold) && profile.TargetShotTicks > 0 {
				for tick := unit.StartTick + profile.TargetShotTicks; tick < unit.EndTick; tick += profile.TargetShotTicks {
					add(CutPoint{Tick: tick, Kind: CutActionBeat, Penalty: 1.5, SplitsUnit: true})
				}
			}
			// Long voiceovers or unpunctuated lines can exceed the maximum
			// model-facing shot duration. Punctuation cuts remain cheaper, while
			// this guard guarantees that the planner still has a complete path.
			if unit.DurationTicks > profile.AbnormalMaximumShotTicks && profile.TargetShotTicks > 0 {
				for tick := unit.StartTick + profile.TargetShotTicks; tick < unit.EndTick; tick += profile.TargetShotTicks {
					add(CutPoint{Tick: tick, Kind: CutDurationGuard, Penalty: profile.SplitDialoguePenalty, SplitsUnit: true})
				}
			}
		}
	}
	// Timing data may contain parallel tracks or sparse blocks. A scene-wide
	// guard grid guarantees that the dynamic program can always bridge the
	// complete scene without exceeding the abnormal duration ceiling.
	if profile.TargetShotTicks > 0 && blocks[len(blocks)-1].EndTick-blocks[0].StartTick > profile.AbnormalMaximumShotTicks {
		for tick := blocks[0].StartTick + profile.TargetShotTicks; tick < blocks[len(blocks)-1].EndTick; tick += profile.TargetShotTicks {
			add(CutPoint{Tick: tick, Kind: CutDurationGuard, Penalty: profile.SplitDialoguePenalty, SplitsUnit: true})
		}
	}
	points := make([]CutPoint, 0, len(byTick))
	for _, point := range byTick {
		points = append(points, point)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Tick < points[j].Tick })
	return points
}

func PlanShotBoundaries(blocks []TimingBlock, options PlanOptions) ([]ShotDraft, error) {
	if len(blocks) == 0 {
		return nil, fmt.Errorf("%w: timing blocks are required", ErrNoValidShotPlan)
	}
	timebase := options.Timebase
	if timebase.TicksPerSecond == 0 {
		timebase = DefaultTimebase()
	}
	if err := timebase.Validate(); err != nil {
		return nil, err
	}
	for _, block := range blocks {
		if !timebase.IsFrameAligned(block.StartTick) || !timebase.IsFrameAligned(block.EndTick) {
			return nil, fmt.Errorf("timing block %s is not frame aligned", block.ID)
		}
		for _, unit := range block.Units {
			if !timebase.IsFrameAligned(unit.StartTick) || !timebase.IsFrameAligned(unit.EndTick) {
				return nil, fmt.Errorf("timing unit %s is not frame aligned", unit.ID)
			}
		}
	}
	profile := options.Pacing
	if profile.TargetShotTicks <= 0 {
		profile = PacingProfileByKey("standard", timebase)
	}
	units := flattenTimingUnits(blocks)
	startTick := blocks[0].StartTick
	endTick := blocks[len(blocks)-1].EndTick
	if options.OneTake {
		shots := []ShotDraft{{Ordinal: 0, StartTick: startTick, EndTick: endTick, DurationTicks: endTick - startTick, OneTake: true}}
		materializeTimingSpans(shots, units)
		if err := ValidateExactCoverage(shots, units, startTick, endTick); err != nil {
			return nil, err
		}
		return shots, nil
	}
	cuts := BuildLegalCutPoints(blocks, profile, timebase)
	if len(cuts) < 2 {
		return nil, fmt.Errorf("%w: fewer than two legal cut points", ErrNoValidShotPlan)
	}
	minimum := options.SemanticMinimum
	if minimum <= 0 {
		minimum = 1
	}
	if options.UserShotBudget > 0 && options.UserShotBudget < minimum {
		return nil, fmt.Errorf("DURATION_CONSTRAINT_CONFLICT: semantic minimum %d exceeds user budget %d", minimum, options.UserShotBudget)
	}
	if minimum > len(cuts)-1 {
		return nil, fmt.Errorf("DURATION_CONSTRAINT_CONFLICT: %d legal shot intervals cannot satisfy semantic minimum %d", len(cuts)-1, minimum)
	}

	// Keep an exact state for counts below the semantic minimum and one capped
	// state for paths that have reached it. A single cheapest path per cut can
	// otherwise discard a slightly more expensive path with the required number
	// of shots before the final constraint check.
	type pathState struct {
		cost              float64
		previousCut       int
		previousCount     int
		materializedCount int
		reachable         bool
	}
	states := make([][]pathState, len(cuts))
	for index := range states {
		states[index] = make([]pathState, minimum+1)
	}
	states[0][0] = pathState{cost: 0, previousCut: -1, previousCount: -1, reachable: true}
	for endIndex := 1; endIndex < len(cuts); endIndex++ {
		for startIndex := 0; startIndex < endIndex; startIndex++ {
			if crossesForcedCut(cuts, startIndex, endIndex) {
				continue
			}
			duration := cuts[endIndex].Tick - cuts[startIndex].Tick
			if duration <= 0 || duration > profile.AbnormalMaximumShotTicks {
				continue
			}
			if duration < profile.MinimumShotTicks && startIndex != 0 && endIndex != len(cuts)-1 && !cuts[startIndex].Forced && !cuts[endIndex].Forced {
				continue
			}
			edgeCost := shotEdgeCost(duration, cuts[endIndex], profile)
			for countState := 0; countState <= minimum; countState++ {
				previous := states[startIndex][countState]
				if !previous.reachable {
					continue
				}
				nextCountState := countState + 1
				if nextCountState > minimum {
					nextCountState = minimum
				}
				candidateCost := previous.cost + edgeCost
				candidateCount := previous.materializedCount + 1
				current := states[endIndex][nextCountState]
				if !current.reachable || candidateCost < current.cost || (candidateCost == current.cost && candidateCount < current.materializedCount) {
					states[endIndex][nextCountState] = pathState{
						cost:              candidateCost,
						previousCut:       startIndex,
						previousCount:     countState,
						materializedCount: candidateCount,
						reachable:         true,
					}
				}
			}
		}
	}
	finalState := states[len(cuts)-1][minimum]
	if !finalState.reachable {
		maximumReachable := 0
		for countState := minimum - 1; countState > 0; countState-- {
			if states[len(cuts)-1][countState].reachable {
				maximumReachable = countState
				break
			}
		}
		if maximumReachable > 0 {
			return nil, fmt.Errorf("DURATION_CONSTRAINT_CONFLICT: at most %d shots can be formed from legal cuts, below semantic minimum %d", maximumReachable, minimum)
		}
		return nil, fmt.Errorf("%w: no path covers %d ticks", ErrNoValidShotPlan, endTick-startTick)
	}
	indices := make([]int, 0, finalState.materializedCount+1)
	currentCut := len(cuts) - 1
	currentCount := minimum
	indices = append(indices, currentCut)
	for currentCut > 0 {
		state := states[currentCut][currentCount]
		if !state.reachable || state.previousCut < 0 {
			return nil, fmt.Errorf("%w: broken dynamic-programming path", ErrNoValidShotPlan)
		}
		currentCut = state.previousCut
		currentCount = state.previousCount
		indices = append(indices, currentCut)
	}
	for left, right := 0, len(indices)-1; left < right; left, right = left+1, right-1 {
		indices[left], indices[right] = indices[right], indices[left]
	}
	shots := make([]ShotDraft, 0, len(indices)-1)
	for index := 1; index < len(indices); index++ {
		start := cuts[indices[index-1]].Tick
		end := cuts[indices[index]].Tick
		shots = append(shots, ShotDraft{Ordinal: len(shots), StartTick: start, EndTick: end, DurationTicks: end - start})
	}
	if options.UserShotBudget > 0 && len(shots) > options.UserShotBudget {
		return nil, fmt.Errorf("DURATION_CONSTRAINT_CONFLICT: %d shots required but user budget is %d", len(shots), options.UserShotBudget)
	}
	safetyMax := options.SafetyMaxShots
	if safetyMax <= 0 {
		safetyMax = int(math.Ceil(timebase.TicksToSeconds(endTick-startTick) / 1.5))
	}
	if len(shots) > safetyMax {
		return nil, fmt.Errorf("SHOT_SAFETY_LIMIT_EXCEEDED: %d shots exceed dynamic safety limit %d", len(shots), safetyMax)
	}
	materializeTimingSpans(shots, units)
	if err := ValidateExactCoverage(shots, units, startTick, endTick); err != nil {
		return nil, err
	}
	return shots, nil
}

func ValidateExactCoverage(shots []ShotDraft, units []TimingUnit, expectedStart, expectedEnd int64) error {
	if len(shots) == 0 {
		return fmt.Errorf("shot coverage is empty")
	}
	cursor := expectedStart
	for index, shot := range shots {
		if shot.StartTick != cursor || shot.EndTick <= shot.StartTick {
			return fmt.Errorf("shot %d creates a gap, overlap, or non-positive interval at %d", index, cursor)
		}
		cursor = shot.EndTick
	}
	if cursor != expectedEnd {
		return fmt.Errorf("shot coverage ends at %d, want %d", cursor, expectedEnd)
	}
	spansByUnit := make(map[string][]TimingSpan, len(units))
	for _, shot := range shots {
		for _, span := range shot.Spans {
			if span.StartTick < shot.StartTick || span.EndTick > shot.EndTick || span.StartTick >= span.EndTick {
				return fmt.Errorf("timing span %s lies outside shot %d", span.TimingUnitID, shot.Ordinal)
			}
			spansByUnit[span.TimingUnitID] = append(spansByUnit[span.TimingUnitID], span)
		}
	}
	for _, unit := range units {
		spans := spansByUnit[unit.ID]
		sort.Slice(spans, func(i, j int) bool { return spans[i].StartTick < spans[j].StartTick })
		unitCursor := unit.StartTick
		for _, span := range spans {
			if span.StartTick != unitCursor || span.EndTick > unit.EndTick {
				return fmt.Errorf("timing unit %s has a gap or overlap at %d", unit.ID, unitCursor)
			}
			unitCursor = span.EndTick
		}
		if unitCursor != unit.EndTick {
			return fmt.Errorf("timing unit %s coverage ends at %d, want %d", unit.ID, unitCursor, unit.EndTick)
		}
	}
	return nil
}

func punctuationCutPoints(unit TimingUnit, timebase Timebase) []CutPoint {
	if unit.EndTick <= unit.StartTick || strings.TrimSpace(unit.SourceText) == "" {
		return nil
	}
	runes := []rune(unit.SourceText)
	if len(runes) < 2 {
		return nil
	}
	points := make([]CutPoint, 0)
	for index, value := range runes[:len(runes)-1] {
		kind := CutKind("")
		penalty := 0.0
		switch value {
		case '。', '？', '?', '！', '!':
			kind, penalty = CutSentence, 2
		case '，', '、', ',', '；', ';', '：', ':':
			kind, penalty = CutClause, 4
		case '…':
			kind, penalty = CutSentence, 3
		}
		if kind == "" {
			continue
		}
		ratio := float64(index+1) / float64(len(runes))
		tick := unit.StartTick + int64(math.Round(float64(unit.EndTick-unit.StartTick)*ratio))
		tick = timebase.QuantizeTickNearest(tick)
		if tick > unit.StartTick && tick < unit.EndTick {
			points = append(points, CutPoint{Tick: tick, Kind: kind, Penalty: penalty, SplitsUnit: true})
		}
	}
	return points
}

func flattenTimingUnits(blocks []TimingBlock) []TimingUnit {
	units := make([]TimingUnit, 0)
	for _, block := range blocks {
		units = append(units, block.Units...)
	}
	return units
}

func materializeTimingSpans(shots []ShotDraft, units []TimingUnit) {
	for shotIndex := range shots {
		shot := &shots[shotIndex]
		shot.Spans = shot.Spans[:0]
		for _, unit := range units {
			start := maxInt64(shot.StartTick, unit.StartTick)
			end := minInt64(shot.EndTick, unit.EndTick)
			if start < end {
				shot.Spans = append(shot.Spans, TimingSpan{TimingUnitID: unit.ID, StartTick: start, EndTick: end, Ordinal: len(shot.Spans)})
			}
		}
	}
}

func crossesForcedCut(cuts []CutPoint, startIndex, endIndex int) bool {
	for index := startIndex + 1; index < endIndex; index++ {
		if cuts[index].Forced {
			return true
		}
	}
	return false
}

func shotEdgeCost(duration int64, end CutPoint, profile PacingProfile) float64 {
	target := float64(profile.TargetShotTicks)
	cost := profile.DurationDeviationWeight * math.Abs(float64(duration)-target) / math.Max(target, 1)
	if duration > profile.MaximumTargetShotTicks {
		cost += float64(duration-profile.MaximumTargetShotTicks) / math.Max(target, 1)
	}
	cost += end.Penalty + profile.ExcessiveShotPenalty
	if end.SplitsUnit {
		cost += profile.SplitDialoguePenalty
	}
	return cost
}

func minInt64(left, right int64) int64 {
	if left < right {
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
