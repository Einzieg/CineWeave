package storyboard

import (
	"fmt"
	"math"
	"strings"
	"unicode"
)

type TimingUnitType string

const (
	UnitDialogue     TimingUnitType = "dialogue"
	UnitVoiceover    TimingUnitType = "voiceover"
	UnitNarration    TimingUnitType = "narration"
	UnitSystem       TimingUnitType = "system"
	UnitAction       TimingUnitType = "action"
	UnitReaction     TimingUnitType = "reaction"
	UnitEstablishing TimingUnitType = "establishing"
	UnitPause        TimingUnitType = "pause"
	UnitAmbientHold  TimingUnitType = "ambient_hold"
	UnitTransition   TimingUnitType = "transition"
)

type TimingTrack string

const (
	TrackAudio  TimingTrack = "audio"
	TrackVisual TimingTrack = "visual"
)

type TimingUnit struct {
	ID                  string         `json:"id"`
	SceneID             string         `json:"sceneId,omitempty"`
	Ordinal             int            `json:"ordinal"`
	Type                TimingUnitType `json:"type"`
	Track               TimingTrack    `json:"track"`
	ParallelGroup       string         `json:"parallelGroup,omitempty"`
	Speaker             string         `json:"speaker,omitempty"`
	SourceText          string         `json:"sourceText,omitempty"`
	Delivery            string         `json:"delivery,omitempty"`
	DurationTicks       int64          `json:"durationTicks"`
	StartTick           int64          `json:"startTick"`
	EndTick             int64          `json:"endTick"`
	DurationSource      string         `json:"durationSource,omitempty"`
	ForceBoundaryBefore bool           `json:"forceBoundaryBefore,omitempty"`
	ForceBoundaryAfter  bool           `json:"forceBoundaryAfter,omitempty"`
	PreferBoundaryAfter bool           `json:"preferBoundaryAfter,omitempty"`
}

type TimingBlock struct {
	ID            string       `json:"id"`
	SceneID       string       `json:"sceneId,omitempty"`
	Ordinal       int          `json:"ordinal"`
	ParallelGroup string       `json:"parallelGroup,omitempty"`
	StartTick     int64        `json:"startTick"`
	EndTick       int64        `json:"endTick"`
	DurationTicks int64        `json:"durationTicks"`
	Units         []TimingUnit `json:"units"`
}

type DialogueEstimateInput struct {
	Text                  string
	Delivery              string
	Language              string
	SpeakerChanged        bool
	Timebase              Timebase
	PunctuationPauseScale float64
}

type DialogueEstimate struct {
	SpokenCharacterCount    int     `json:"spokenCharacterCount"`
	SubtitleCharacterCount  int     `json:"subtitleCharacterCount"`
	CharactersPerSecond     float64 `json:"charactersPerSecond"`
	SpeechSeconds           float64 `json:"speechSeconds"`
	PunctuationPauseSeconds float64 `json:"punctuationPauseSeconds"`
	DeliveryPauseSeconds    float64 `json:"deliveryPauseSeconds"`
	ReadabilityFloorSeconds float64 `json:"readabilityFloorSeconds"`
	DurationSeconds         float64 `json:"durationSeconds"`
	DurationTicks           int64   `json:"durationTicks"`
}

func EstimateChineseDialogue(input DialogueEstimateInput) (DialogueEstimate, error) {
	timebase := input.Timebase
	if timebase.TicksPerSecond == 0 {
		timebase = DefaultTimebase()
	}
	if err := timebase.Validate(); err != nil {
		return DialogueEstimate{}, err
	}
	language := strings.ToLower(strings.TrimSpace(input.Language))
	if language != "" && language != "zh" && language != "zh-cn" && language != "zh-hans" {
		return DialogueEstimate{}, fmt.Errorf("Chinese dialogue estimator cannot estimate language %q", input.Language)
	}
	rate, deliveryPause := dialogueDeliveryProfile(input.Delivery)
	spokenCount := CountChineseSpokenCharacters(input.Text)
	visibleCount := countSubtitleCharacters(input.Text)
	speechSeconds := float64(spokenCount) / rate
	pauseScale := input.PunctuationPauseScale
	if pauseScale <= 0 {
		pauseScale = 1
	}
	punctuationPause := punctuationPauseSeconds(input.Text) * pauseScale
	if input.SpeakerChanged {
		punctuationPause += 0.25
	}
	readabilityFloor := float64(visibleCount) / 9.0
	duration := math.Max(0.8, math.Max(speechSeconds+punctuationPause+deliveryPause, readabilityFloor))
	durationTicks := timebase.SecondsToFrameTicksCeil(duration)
	return DialogueEstimate{
		SpokenCharacterCount:    spokenCount,
		SubtitleCharacterCount:  visibleCount,
		CharactersPerSecond:     rate,
		SpeechSeconds:           speechSeconds,
		PunctuationPauseSeconds: punctuationPause,
		DeliveryPauseSeconds:    deliveryPause,
		ReadabilityFloorSeconds: readabilityFloor,
		DurationSeconds:         timebase.TicksToSeconds(durationTicks),
		DurationTicks:           durationTicks,
	}, nil
}

func CountChineseSpokenCharacters(text string) int {
	runes := []rune(strings.TrimSpace(text))
	count := 0
	for index, value := range runes {
		switch {
		case unicode.Is(unicode.Han, value):
			count++
		case unicode.IsDigit(value):
			count++
		case value == '.' && index > 0 && index+1 < len(runes) && unicode.IsDigit(runes[index-1]) && unicode.IsDigit(runes[index+1]):
			count++
		case value == '%':
			count += 3
		case unicode.IsLetter(value):
			count++
		}
	}
	return count
}

type ActionKind string

const (
	ActionMicro        ActionKind = "micro"
	ActionSimple       ActionKind = "simple"
	ActionMovement     ActionKind = "movement"
	ActionEstablishing ActionKind = "establishing"
	ActionCombat       ActionKind = "combat"
	ActionTransition   ActionKind = "transition"
)

type ActionEstimate struct {
	Kind            ActionKind `json:"kind"`
	MinimumSeconds  float64    `json:"minimumSeconds"`
	MaximumSeconds  float64    `json:"maximumSeconds"`
	DurationSeconds float64    `json:"durationSeconds"`
	DurationTicks   int64      `json:"durationTicks"`
	OutOfRange      bool       `json:"outOfRange"`
}

func EstimateActionDuration(kind ActionKind, suggestedSeconds float64, exceptionReason string, timebase Timebase) (ActionEstimate, error) {
	return EstimateActionDurationCalibrated(kind, suggestedSeconds, exceptionReason, timebase, 1)
}

func EstimateActionDurationCalibrated(kind ActionKind, suggestedSeconds float64, exceptionReason string, timebase Timebase, scale float64) (ActionEstimate, error) {
	if timebase.TicksPerSecond == 0 {
		timebase = DefaultTimebase()
	}
	minimum, maximum, ok := actionRange(kind)
	if !ok {
		return ActionEstimate{}, fmt.Errorf("unsupported action kind %q", kind)
	}
	if scale <= 0 {
		scale = 1
	}
	minimum *= scale
	maximum *= scale
	if suggestedSeconds > 0 {
		suggestedSeconds *= scale
	}
	if suggestedSeconds <= 0 {
		suggestedSeconds = (minimum + maximum) / 2
	}
	outOfRange := suggestedSeconds < minimum || suggestedSeconds > maximum
	if outOfRange && strings.TrimSpace(exceptionReason) == "" {
		return ActionEstimate{}, fmt.Errorf("action duration %.3fs is outside %.3f-%.3fs without an exception reason", suggestedSeconds, minimum, maximum)
	}
	minimumTicks := timebase.SecondsToFrameTicksCeil(minimum)
	durationTicks := timebase.QuantizeTickNearest(timebase.SecondsToTicks(suggestedSeconds))
	if durationTicks < minimumTicks {
		durationTicks = minimumTicks
	}
	return ActionEstimate{
		Kind:            kind,
		MinimumSeconds:  minimum,
		MaximumSeconds:  maximum,
		DurationSeconds: timebase.TicksToSeconds(durationTicks),
		DurationTicks:   durationTicks,
		OutOfRange:      outOfRange,
	}, nil
}

func BuildTimingBlocks(units []TimingUnit) ([]TimingBlock, error) {
	blocks := make([]TimingBlock, 0, len(units))
	cursor := int64(0)
	for index := 0; index < len(units); {
		group := strings.TrimSpace(units[index].ParallelGroup)
		end := index + 1
		if group != "" {
			for end < len(units) && strings.TrimSpace(units[end].ParallelGroup) == group {
				end++
			}
		}
		blockUnits := append([]TimingUnit(nil), units[index:end]...)
		blockDuration := int64(0)
		for unitIndex := range blockUnits {
			unit := &blockUnits[unitIndex]
			if strings.TrimSpace(unit.ID) == "" || unit.DurationTicks <= 0 {
				return nil, fmt.Errorf("timing unit id and positive duration are required at ordinal %d", unit.Ordinal)
			}
			if unit.Track != TrackAudio && unit.Track != TrackVisual {
				return nil, fmt.Errorf("timing unit %s has invalid track %q", unit.ID, unit.Track)
			}
			unit.StartTick = cursor
			unit.EndTick = cursor + unit.DurationTicks
			if unit.DurationTicks > blockDuration {
				blockDuration = unit.DurationTicks
			}
		}
		block := TimingBlock{
			ID:            fmt.Sprintf("block-%d", len(blocks)+1),
			SceneID:       blockUnits[0].SceneID,
			Ordinal:       len(blocks),
			ParallelGroup: group,
			StartTick:     cursor,
			EndTick:       cursor + blockDuration,
			DurationTicks: blockDuration,
			Units:         blockUnits,
		}
		blocks = append(blocks, block)
		cursor = block.EndTick
		index = end
	}
	return blocks, nil
}

func dialogueDeliveryProfile(delivery string) (float64, float64) {
	value := strings.ToLower(strings.TrimSpace(delivery))
	switch {
	case value == "slow", strings.Contains(value, "低语"), strings.Contains(value, "whisper"):
		return 3.0, 0.10
	case strings.Contains(value, "哭"), strings.Contains(value, "哽咽"), strings.Contains(value, "cry"):
		return 3.0, 0.30
	case strings.Contains(value, "庄重"), strings.Contains(value, "缓慢"), strings.Contains(value, "solemn"):
		return 3.0, 0.15
	case strings.Contains(value, "高喊"), strings.Contains(value, "shout"):
		return 3.5, 0.10
	case value == "fast", strings.Contains(value, "急促"), strings.Contains(value, "快节奏"):
		return 4.0, 0
	case strings.Contains(value, "旁白"), strings.Contains(value, "narration"):
		return 3.5, 0.10
	default:
		return 3.5, 0
	}
}

func punctuationPauseSeconds(text string) float64 {
	runes := []rune(text)
	seconds := 0.0
	for index := 0; index < len(runes); index++ {
		switch runes[index] {
		case '，', '、', ',', '；', ';', '：', ':':
			seconds += 0.15
		case '。', '？', '?', '！', '!':
			seconds += 0.35
		case '…':
			seconds += 0.70
			for index+1 < len(runes) && runes[index+1] == '…' {
				index++
			}
		case '.':
			end := index
			for end+1 < len(runes) && runes[end+1] == '.' {
				end++
			}
			if end-index >= 2 {
				seconds += 0.70
				index = end
			} else if index == 0 || index+1 >= len(runes) || !unicode.IsDigit(runes[index-1]) || !unicode.IsDigit(runes[index+1]) {
				seconds += 0.35
			}
		case '\n':
			seconds += 0.25
		}
	}
	return seconds
}

func countSubtitleCharacters(text string) int {
	count := 0
	for _, value := range text {
		if !unicode.IsSpace(value) && !unicode.IsControl(value) {
			count++
		}
	}
	return count
}

func actionRange(kind ActionKind) (float64, float64, bool) {
	switch kind {
	case ActionMicro:
		return 0.8, 2.0, true
	case ActionSimple:
		return 1.2, 3.0, true
	case ActionMovement:
		return 2.0, 5.0, true
	case ActionEstablishing:
		return 2.5, 6.0, true
	case ActionCombat:
		return 1.5, 4.0, true
	case ActionTransition:
		return 0.3, 1.5, true
	default:
		return 0, 0, false
	}
}
