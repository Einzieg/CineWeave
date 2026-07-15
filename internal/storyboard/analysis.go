package storyboard

import (
	"fmt"
	"strings"
	"unicode"
)

type AnalyzeTimingOptions struct {
	Timebase              Timebase
	EpisodeContent        string
	TargetDurationTicks   *int64
	PunctuationPauseScale float64
	ActionDurationScales  map[string]float64
}

type AnalyzedTimingUnit struct {
	TimingUnit
	SceneKey          string  `json:"sceneKey"`
	ScriptSceneID     string  `json:"scriptSceneId,omitempty"`
	SourceStartOffset *int    `json:"sourceStartOffset,omitempty"`
	SourceEndOffset   *int    `json:"sourceEndOffset,omitempty"`
	MinimumTicks      int64   `json:"minimumTicks"`
	MaximumTicks      int64   `json:"maximumTicks"`
	Confidence        float64 `json:"confidence"`
}

type AnalyzedTimingScene struct {
	SceneKey      string               `json:"sceneKey"`
	ScriptSceneID string               `json:"scriptSceneId,omitempty"`
	SceneOrdinal  int                  `json:"sceneOrdinal"`
	StartTick     int64                `json:"startTick"`
	EndTick       int64                `json:"endTick"`
	Units         []AnalyzedTimingUnit `json:"units"`
	Blocks        []TimingBlock        `json:"blocks"`
}

type TimingAnalysisResult struct {
	Scenes                 []AnalyzedTimingScene `json:"scenes"`
	Units                  []AnalyzedTimingUnit  `json:"units"`
	Blocks                 []TimingBlock         `json:"blocks"`
	EstimatedDurationTicks int64                 `json:"estimatedDurationTicks"`
	MinimumDurationTicks   int64                 `json:"minimumDurationTicks"`
	TargetDurationTicks    *int64                `json:"targetDurationTicks,omitempty"`
	Timebase               Timebase              `json:"timebase"`
}

func AnalyzeSemanticTiming(output TimingAnalyzerOutput, options AnalyzeTimingOptions) (TimingAnalysisResult, error) {
	if err := ValidateTimingAnalyzerOutput(output); err != nil {
		return TimingAnalysisResult{}, err
	}
	timebase := options.Timebase
	if timebase.TicksPerSecond == 0 {
		timebase = DefaultTimebase()
	}
	if err := timebase.Validate(); err != nil {
		return TimingAnalysisResult{}, err
	}
	contentRunes := []rune(options.EpisodeContent)
	searchCursor := 0
	globalCursor := int64(0)
	minimumTotal := int64(0)
	previousSpeaker := ""
	result := TimingAnalysisResult{Timebase: timebase, Scenes: make([]AnalyzedTimingScene, 0, len(output.Scenes))}
	for _, semanticScene := range output.Scenes {
		sceneUnits := make([]AnalyzedTimingUnit, 0, len(semanticScene.Units))
		minimumByUnit := make(map[string]int64, len(semanticScene.Units))
		for _, semanticUnit := range semanticScene.Units {
			durationTicks, minimumTicks, maximumTicks, confidence, err := estimateSemanticUnit(
				semanticUnit,
				timebase,
				previousSpeaker != "" && semanticUnit.Speaker != "" && semanticUnit.Speaker != previousSpeaker,
				options,
			)
			if err != nil {
				return TimingAnalysisResult{}, fmt.Errorf("timing unit %s: %w", semanticUnit.UnitKey, err)
			}
			if isSpeechTimingUnit(semanticUnit.Type) && semanticUnit.Speaker != "" {
				previousSpeaker = semanticUnit.Speaker
			}
			startOffset, endOffset, nextCursor, err := resolveSemanticSourceOffsets(contentRunes, searchCursor, semanticUnit)
			if err != nil {
				return TimingAnalysisResult{}, err
			}
			searchCursor = nextCursor
			unit := AnalyzedTimingUnit{
				TimingUnit: TimingUnit{
					ID:                  semanticUnit.UnitKey,
					SceneID:             semanticScene.SceneKey,
					Ordinal:             semanticUnit.UnitOrdinal,
					Type:                semanticUnit.Type,
					Track:               semanticUnit.Track,
					ParallelGroup:       semanticUnit.ParallelGroup,
					Speaker:             semanticUnit.Speaker,
					SourceText:          semanticUnit.Text,
					Delivery:            semanticUnit.Delivery,
					DurationTicks:       durationTicks,
					DurationSource:      "rule_estimated",
					ForceBoundaryBefore: semanticUnit.ForceBoundaryBefore,
					ForceBoundaryAfter:  semanticUnit.ForceBoundaryAfter,
				},
				SceneKey:          semanticScene.SceneKey,
				ScriptSceneID:     semanticScene.ScriptSceneID,
				SourceStartOffset: startOffset,
				SourceEndOffset:   endOffset,
				MinimumTicks:      minimumTicks,
				MaximumTicks:      maximumTicks,
				Confidence:        confidence,
			}
			sceneUnits = append(sceneUnits, unit)
			minimumByUnit[unit.ID] = minimumTicks
		}

		plainUnits := make([]TimingUnit, len(sceneUnits))
		for index := range sceneUnits {
			plainUnits[index] = sceneUnits[index].TimingUnit
		}
		blocks, err := BuildTimingBlocks(plainUnits)
		if err != nil {
			return TimingAnalysisResult{}, err
		}
		sceneMinimum := int64(0)
		for blockIndex := range blocks {
			block := &blocks[blockIndex]
			block.StartTick += globalCursor
			block.EndTick += globalCursor
			minimumBlock := int64(0)
			for unitIndex := range block.Units {
				unit := &block.Units[unitIndex]
				unit.StartTick += globalCursor
				unit.EndTick += globalCursor
				if minimumByUnit[unit.ID] > minimumBlock {
					minimumBlock = minimumByUnit[unit.ID]
				}
				for sceneUnitIndex := range sceneUnits {
					if sceneUnits[sceneUnitIndex].ID == unit.ID {
						sceneUnits[sceneUnitIndex].StartTick = unit.StartTick
						sceneUnits[sceneUnitIndex].EndTick = unit.EndTick
					}
				}
			}
			sceneMinimum += minimumBlock
		}
		sceneStart := globalCursor
		if len(blocks) > 0 {
			globalCursor = blocks[len(blocks)-1].EndTick
		}
		minimumTotal += sceneMinimum
		scene := AnalyzedTimingScene{
			SceneKey:      semanticScene.SceneKey,
			ScriptSceneID: semanticScene.ScriptSceneID,
			SceneOrdinal:  semanticScene.SceneOrdinal,
			StartTick:     sceneStart,
			EndTick:       globalCursor,
			Units:         sceneUnits,
			Blocks:        blocks,
		}
		result.Scenes = append(result.Scenes, scene)
		result.Units = append(result.Units, sceneUnits...)
		result.Blocks = append(result.Blocks, blocks...)
	}

	result.EstimatedDurationTicks = globalCursor
	result.MinimumDurationTicks = minimumTotal
	if options.TargetDurationTicks != nil {
		target := timebase.QuantizeTickCeil(*options.TargetDurationTicks)
		if target < minimumTotal || target < globalCursor {
			return TimingAnalysisResult{}, fmt.Errorf(
				"DURATION_CONSTRAINT_CONFLICT: target %d ticks is below deterministic narrative duration %d ticks (hard minimum %d)",
				target,
				globalCursor,
				minimumTotal,
			)
		}
		if target > globalCursor {
			if err := appendTargetDurationHold(&result, target-globalCursor); err != nil {
				return TimingAnalysisResult{}, err
			}
			result.EstimatedDurationTicks = target
		}
		result.TargetDurationTicks = &target
	}
	return result, nil
}

func estimateSemanticUnit(unit TimingAnalyzerUnit, timebase Timebase, speakerChanged bool, options AnalyzeTimingOptions) (duration, minimum, maximum int64, confidence float64, err error) {
	if isSpeechTimingUnit(unit.Type) {
		estimate, estimateErr := EstimateChineseDialogue(DialogueEstimateInput{
			Text:                  unit.Text,
			Delivery:              unit.Delivery,
			Language:              unit.Language,
			SpeakerChanged:        speakerChanged,
			Timebase:              timebase,
			PunctuationPauseScale: options.PunctuationPauseScale,
		})
		if estimateErr != nil {
			return 0, 0, 0, 0, estimateErr
		}
		return estimate.DurationTicks, estimate.DurationTicks, estimate.DurationTicks, 0.92, nil
	}
	kind := unit.ActionKind
	if kind == "" {
		kind = defaultActionKind(unit.Type)
	}
	scale := 1.0
	if options.ActionDurationScales != nil && options.ActionDurationScales[string(kind)] > 0 {
		scale = options.ActionDurationScales[string(kind)]
	}
	estimate, estimateErr := EstimateActionDurationCalibrated(kind, unit.SuggestedSeconds, unit.ExceptionReason, timebase, scale)
	if estimateErr != nil {
		return 0, 0, 0, 0, estimateErr
	}
	minimum = timebase.SecondsToFrameTicksCeil(estimate.MinimumSeconds)
	maximum = timebase.QuantizeTickFloor(timebase.SecondsToTicks(estimate.MaximumSeconds))
	if maximum < minimum {
		maximum = minimum
	}
	return estimate.DurationTicks, minimum, maximum, 0.78, nil
}

func defaultActionKind(unitType TimingUnitType) ActionKind {
	switch unitType {
	case UnitReaction:
		return ActionMicro
	case UnitEstablishing, UnitAmbientHold:
		return ActionEstablishing
	case UnitTransition, UnitPause:
		return ActionTransition
	default:
		return ActionSimple
	}
}

func resolveSemanticSourceOffsets(content []rune, cursor int, unit TimingAnalyzerUnit) (*int, *int, int, error) {
	text := strings.TrimSpace(unit.Text)
	if text == "" {
		return nil, nil, cursor, nil
	}
	textRunes := []rune(text)
	if unit.SourceStartOffset != nil && unit.SourceEndOffset != nil {
		start, end := *unit.SourceStartOffset, *unit.SourceEndOffset
		if start >= cursor && start >= 0 && end <= len(content) && end > start && strings.TrimSpace(string(content[start:end])) == text {
			return &start, &end, maxInt(cursor, end), nil
		}
	}
	start := indexRunes(content, textRunes, cursor)
	if start >= 0 {
		end := start + len(textRunes)
		return &start, &end, end, nil
	}
	start, end := indexMarkdownNormalizedRunes(content, textRunes, cursor)
	if start >= 0 {
		return &start, &end, end, nil
	}
	return nil, nil, cursor, fmt.Errorf("timing unit %s source text was not found in episode content", unit.UnitKey)
}

func indexMarkdownNormalizedRunes(content, needle []rune, cursor int) (int, int) {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(content) {
		return -1, -1
	}
	normalizedContent, sourceIndexes := normalizeMarkdownRunes(content[cursor:])
	normalizedNeedle, _ := normalizeMarkdownRunes(needle)
	if len(normalizedNeedle) == 0 || len(normalizedContent) < len(normalizedNeedle) {
		return -1, -1
	}
	match := indexRunes(normalizedContent, normalizedNeedle, 0)
	if match < 0 {
		return -1, -1
	}
	start := cursor + sourceIndexes[match]
	end := cursor + sourceIndexes[match+len(normalizedNeedle)-1] + 1
	return start, end
}

func normalizeMarkdownRunes(value []rune) ([]rune, []int) {
	normalized := make([]rune, 0, len(value))
	indexes := make([]int, 0, len(value))
	atLineStart := true
	for index := 0; index < len(value); index++ {
		current := value[index]
		if current == '\r' || current == '\n' {
			atLineStart = true
			continue
		}
		if unicode.IsSpace(current) {
			continue
		}
		if atLineStart {
			if markerLength := timingMarkdownStructuralMarkerLength(value[index:]); markerLength > 0 {
				index += markerLength - 1
				atLineStart = false
				continue
			}
			if current == '#' || current == '>' {
				continue
			}
			if (current == '-' || current == '+' || current == '*') && index+1 < len(value) && unicode.IsSpace(value[index+1]) {
				continue
			}
			atLineStart = false
		}
		if current == '*' || current == '_' || current == '`' {
			continue
		}
		normalized = append(normalized, current)
		indexes = append(indexes, index)
	}
	return normalized, indexes
}

func timingMarkdownStructuralMarkerLength(value []rune) int {
	for _, marker := range []string{
		"**人物：**",
		"**氛围：**",
		"**画面：**",
		"**音效：**",
		"**时间流逝：**",
	} {
		markerRunes := []rune(marker)
		if len(value) < len(markerRunes) {
			continue
		}
		matched := true
		for index := range markerRunes {
			if value[index] != markerRunes[index] {
				matched = false
				break
			}
		}
		if matched {
			return len(markerRunes)
		}
	}
	return 0
}

func appendTargetDurationHold(result *TimingAnalysisResult, durationTicks int64) error {
	if result == nil || len(result.Scenes) == 0 || durationTicks <= 0 {
		return fmt.Errorf("cannot append target duration hold")
	}
	lastScene := &result.Scenes[len(result.Scenes)-1]
	ordinal := len(result.Units)
	unit := AnalyzedTimingUnit{
		TimingUnit: TimingUnit{
			ID:             fmt.Sprintf("target-hold-%d", ordinal),
			SceneID:        lastScene.SceneKey,
			Ordinal:        ordinal,
			Type:           UnitAmbientHold,
			Track:          TrackVisual,
			SourceText:     "",
			DurationTicks:  durationTicks,
			StartTick:      result.EstimatedDurationTicks,
			EndTick:        result.EstimatedDurationTicks + durationTicks,
			DurationSource: "rule_estimated",
		},
		SceneKey:      lastScene.SceneKey,
		ScriptSceneID: lastScene.ScriptSceneID,
		MinimumTicks:  durationTicks,
		MaximumTicks:  durationTicks,
		Confidence:    1,
	}
	block := TimingBlock{
		ID:            fmt.Sprintf("block-%d", len(result.Blocks)+1),
		SceneID:       lastScene.SceneKey,
		Ordinal:       len(result.Blocks),
		StartTick:     unit.StartTick,
		EndTick:       unit.EndTick,
		DurationTicks: durationTicks,
		Units:         []TimingUnit{unit.TimingUnit},
	}
	lastScene.Units = append(lastScene.Units, unit)
	lastScene.Blocks = append(lastScene.Blocks, block)
	lastScene.EndTick = unit.EndTick
	result.Units = append(result.Units, unit)
	result.Blocks = append(result.Blocks, block)
	return nil
}

func indexRunes(haystack, needle []rune, start int) int {
	if len(needle) == 0 {
		return start
	}
	for index := maxInt(start, 0); index+len(needle) <= len(haystack); index++ {
		match := true
		for offset := range needle {
			if haystack[index+offset] != needle[offset] {
				match = false
				break
			}
		}
		if match {
			return index
		}
	}
	return -1
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
