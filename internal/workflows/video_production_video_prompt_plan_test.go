package workflows

import (
	"strings"
	"testing"
)

func TestShotLocalVideoDialogueUsesSourceTimingWindow(t *testing.T) {
	shot := StoryboardShotRecord{
		StartTick:            9000,
		EndTick:              9900,
		PlannedDurationTicks: 900,
	}
	lines := []StoryboardDialogueLine{
		{Kind: "dialogue", Speaker: "方源", Text: "第一句", SpanStartTick: 1200, SpanEndTick: 1500},
		{Kind: "system", Text: "雨声渐强", SpanStartTick: 1600, SpanEndTick: 1800},
	}

	localized, err := shotLocalVideoDialogue(shot, 1000, 1900, lines)
	if err != nil {
		t.Fatalf("shotLocalVideoDialogue: %v", err)
	}
	if len(localized) != 2 {
		t.Fatalf("localized lines = %d, want 2", len(localized))
	}
	if localized[0].SpanStartTick != 200 || localized[0].SpanEndTick != 500 {
		t.Fatalf("first localized span = %d..%d, want 200..500", localized[0].SpanStartTick, localized[0].SpanEndTick)
	}
	if localized[1].SpanStartTick != 600 || localized[1].SpanEndTick != 800 {
		t.Fatalf("second localized span = %d..%d, want 600..800", localized[1].SpanStartTick, localized[1].SpanEndTick)
	}
}

func TestShotLocalVideoDialogueAcceptsAlreadyLocalizedCues(t *testing.T) {
	shot := StoryboardShotRecord{PlannedDurationTicks: 900}
	lines := []StoryboardDialogueLine{
		{Kind: "dialogue", Speaker: "方源", Text: "第一句", SpanStartTick: 200, SpanEndTick: 500},
		{Kind: "system", Text: "雨声渐强", SpanStartTick: 600, SpanEndTick: 800},
	}

	localized, err := shotLocalVideoDialogue(shot, 0, 900, lines)
	if err != nil {
		t.Fatalf("shotLocalVideoDialogue: %v", err)
	}
	if localized[0].SpanStartTick != 200 || localized[0].SpanEndTick != 500 ||
		localized[1].SpanStartTick != 600 || localized[1].SpanEndTick != 800 {
		t.Fatalf("localized cues changed unexpectedly: %+v", localized)
	}
}

func TestShotLocalVideoDialogueRejectsCueOutsideSourceTimingWindow(t *testing.T) {
	shot := StoryboardShotRecord{PlannedDurationTicks: 900}
	_, err := shotLocalVideoDialogue(shot, 1000, 1900, []StoryboardDialogueLine{
		{Kind: "dialogue", Speaker: "方源", Text: "越界台词", SpanStartTick: 900, SpanEndTick: 1200},
	})
	if err == nil || !strings.Contains(err.Error(), "不在当前镜头对应的剧本时间范围内") {
		t.Fatalf("error = %v, want source timing range rejection", err)
	}
}

func TestShotLocalVideoDialogueRejectsDurationMismatch(t *testing.T) {
	shot := StoryboardShotRecord{PlannedDurationTicks: 800}
	_, err := shotLocalVideoDialogue(shot, 1000, 1900, nil)
	if err == nil || !strings.Contains(err.Error(), "镜头时长与剧本时间窗口不一致") {
		t.Fatalf("error = %v, want duration mismatch", err)
	}
}
