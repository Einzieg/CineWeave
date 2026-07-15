package storyboard

import "testing"

func TestAnalyzeSemanticTimingUsesParallelMaximumAndExactSourceOffsets(t *testing.T) {
	content := "方源走到窗边。方源：今天该结束了。"
	output := TimingAnalyzerOutput{Scenes: []TimingAnalyzerScene{{
		SceneKey:     "scene-1",
		SceneOrdinal: 0,
		Units: []TimingAnalyzerUnit{
			{UnitKey: "action-1", UnitOrdinal: 0, Type: UnitAction, Track: TrackVisual, ParallelGroup: "block-1", Text: "方源走到窗边。", ActionKind: ActionMovement, SuggestedSeconds: 3},
			{UnitKey: "dialogue-1", UnitOrdinal: 1, Type: UnitDialogue, Track: TrackAudio, ParallelGroup: "block-1", Speaker: "方源", Text: "今天该结束了。", Language: "zh-CN"},
		},
	}}}
	result, err := AnalyzeSemanticTiming(output, AnalyzeTimingOptions{EpisodeContent: content, Timebase: DefaultTimebase()})
	if err != nil {
		t.Fatalf("analyze timing: %v", err)
	}
	if len(result.Blocks) != 1 || len(result.Units) != 2 {
		t.Fatalf("result = %+v", result)
	}
	want := result.Units[0].DurationTicks
	if result.Units[1].DurationTicks > want {
		want = result.Units[1].DurationTicks
	}
	if result.EstimatedDurationTicks != want {
		t.Fatalf("estimated ticks = %d, want parallel max %d", result.EstimatedDurationTicks, want)
	}
	for _, unit := range result.Units {
		if unit.SourceStartOffset == nil || unit.SourceEndOffset == nil {
			t.Fatalf("unit offsets missing: %+v", unit)
		}
	}
}

func TestAnalyzeSemanticTimingExtendsLongerTargetWithExplicitHold(t *testing.T) {
	output := TimingAnalyzerOutput{Scenes: []TimingAnalyzerScene{{
		SceneKey:     "scene-1",
		SceneOrdinal: 0,
		Units: []TimingAnalyzerUnit{{
			UnitKey:     "dialogue-1",
			UnitOrdinal: 0,
			Type:        UnitDialogue,
			Track:       TrackAudio,
			Speaker:     "甲",
			Text:        "你好。",
			Language:    "zh-CN",
		}},
	}}}
	target := int64(900_000)
	result, err := AnalyzeSemanticTiming(output, AnalyzeTimingOptions{EpisodeContent: "甲：你好。", Timebase: DefaultTimebase(), TargetDurationTicks: &target})
	if err != nil {
		t.Fatalf("analyze target timing: %v", err)
	}
	if result.EstimatedDurationTicks != target || result.Units[len(result.Units)-1].Type != UnitAmbientHold {
		t.Fatalf("target result = %+v", result)
	}
}

func TestAnalyzeSemanticTimingMatchesMarkdownListAsContinuousSourceText(t *testing.T) {
	content := "### 前世浮影\n\n- 现代地球，学生翻书。\n- 车流、霓虹、校园铃声。\n- 一道光影吞没他。"
	text := "现代地球，学生翻书。车流、霓虹、校园铃声。一道光影吞没他。"
	output := TimingAnalyzerOutput{Scenes: []TimingAnalyzerScene{{
		SceneKey:     "scene-1",
		SceneOrdinal: 0,
		Units: []TimingAnalyzerUnit{{
			UnitKey:     "montage-1",
			UnitOrdinal: 0,
			Type:        UnitTransition,
			Track:       TrackVisual,
			Text:        text,
			ActionKind:  ActionTransition,
		}},
	}}}
	result, err := AnalyzeSemanticTiming(output, AnalyzeTimingOptions{EpisodeContent: content, Timebase: DefaultTimebase()})
	if err != nil {
		t.Fatalf("analyze markdown timing: %v", err)
	}
	unit := result.Units[0]
	if unit.SourceStartOffset == nil || unit.SourceEndOffset == nil {
		t.Fatalf("unit offsets missing: %+v", unit)
	}
	matched := []rune(content)[*unit.SourceStartOffset:*unit.SourceEndOffset]
	if string(matched) != "现代地球，学生翻书。\n- 车流、霓虹、校园铃声。\n- 一道光影吞没他。" {
		t.Fatalf("matched source = %q", string(matched))
	}
}

func TestAnalyzeSemanticTimingMatchesMarkdownBlockquoteAsContinuousSourceText(t *testing.T) {
	content := "**氛围：** 庄严肃穆，香烟袅袅。\n\n> **画面：**\n> 祠堂中央，黑漆台案高立。\n> 两侧赤铜香炉中，青烟盘旋而上。"
	text := "庄严肃穆，香烟袅袅。祠堂中央，黑漆台案高立。两侧赤铜香炉中，青烟盘旋而上。"
	output := TimingAnalyzerOutput{Scenes: []TimingAnalyzerScene{{
		SceneKey:     "scene-1",
		SceneOrdinal: 0,
		Units: []TimingAnalyzerUnit{{
			UnitKey:     "establishing-1",
			UnitOrdinal: 0,
			Type:        UnitEstablishing,
			Track:       TrackVisual,
			Text:        text,
			ActionKind:  ActionEstablishing,
		}},
	}}}
	result, err := AnalyzeSemanticTiming(output, AnalyzeTimingOptions{EpisodeContent: content, Timebase: DefaultTimebase()})
	if err != nil {
		t.Fatalf("analyze markdown blockquote timing: %v", err)
	}
	unit := result.Units[0]
	if unit.SourceStartOffset == nil || unit.SourceEndOffset == nil {
		t.Fatalf("unit offsets missing: %+v", unit)
	}
	matched := []rune(content)[*unit.SourceStartOffset:*unit.SourceEndOffset]
	if string(matched) != "庄严肃穆，香烟袅袅。\n\n> **画面：**\n> 祠堂中央，黑漆台案高立。\n> 两侧赤铜香炉中，青烟盘旋而上。" {
		t.Fatalf("matched source = %q", string(matched))
	}
}
