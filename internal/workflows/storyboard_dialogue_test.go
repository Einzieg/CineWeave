package workflows

import (
	"strings"
	"testing"
)

func TestExtractScriptDialogueLinesPreservesChineseVerbatim(t *testing.T) {
	content := `## 场景一

**画面**：
山巅风声呼啸。

**正道首领甲**（厉声）：
方源，乖乖交出春秋蝉，我给你个痛快！

**方源内心声**：
来生，还是要做邪魔。

**正道群雄**（混乱）：
退！
拦住他！`

	lines := ExtractScriptDialogueLines(content)
	if len(lines) != 4 {
		t.Fatalf("dialogue lines = %d, want 4: %+v", len(lines), lines)
	}
	if lines[0].Speaker != "正道首领甲" || lines[0].Text != "方源，乖乖交出春秋蝉，我给你个痛快！" || lines[0].Delivery != "厉声" {
		t.Fatalf("first line = %+v", lines[0])
	}
	if lines[1].Kind != "voiceover" {
		t.Fatalf("voiceover kind = %q", lines[1].Kind)
	}
}

func TestExtractScriptDialogueLinesSupportsFullyBoldHeadersAndSkipsFields(t *testing.T) {
	content := `## 场景一

**人物：** 方源、正道群雄
**氛围：** 山风怒号，残阳如血。

**正道蛊师甲（厉喝）：**
方源，乖乖交出春秋蝉，我给你个痛快！

**方源（轻笑，望着远山）：**
青山落日，秋月春风。
当真是……朝如青丝暮成雪，是非成败转头空。

**音效：** 山风卷过。`

	lines := ExtractScriptDialogueLines(content)
	if len(lines) != 3 {
		t.Fatalf("dialogue lines = %d, want 3: %+v", len(lines), lines)
	}
	if lines[0].Speaker != "正道蛊师甲" || lines[0].Delivery != "厉喝" {
		t.Fatalf("first line = %+v", lines[0])
	}
	if lines[1].Speaker != "方源" || lines[1].Delivery != "轻笑，望着远山" || lines[1].Text != "青山落日，秋月春风。" {
		t.Fatalf("second line = %+v", lines[1])
	}
	if lines[2].Speaker != "方源" || lines[2].Text != "当真是……朝如青丝暮成雪，是非成败转头空。" {
		t.Fatalf("third line = %+v", lines[2])
	}
}

func TestExtractScriptDialogueLinesSkipsColonTerminatedStageDirections(t *testing.T) {
	content := `**环境**：夜雨绵密，油灯摇曳。

镜头在两人之间保持距离：方源立于窗前，方正站在门影里。

镜头自方源背后缓缓拉远：少年孤身立于窗前。

**方源**：你退下罢。`
	lines := ExtractScriptDialogueLines(content)
	if len(lines) != 1 || lines[0].Speaker != "方源" || lines[0].Text != "你退下罢。" {
		t.Fatalf("dialogue = %#v, want only the character line", lines)
	}
}

func TestExtractScriptDialogueLinesClassifiesParenthesizedInnerVoice(t *testing.T) {
	lines := ExtractScriptDialogueLines(`**方源（心声）**：这一步不能后退。`)
	if len(lines) != 1 || lines[0].Speaker != "方源" || lines[0].Delivery != "心声" || lines[0].Kind != "voiceover" {
		t.Fatalf("dialogue = %#v, want voiceover", lines)
	}
}

func TestNormalizeStoryboardDialoguePreservesSpeakerlessSystemAudio(t *testing.T) {
	lines := NormalizeStoryboardDialogue([]StoryboardDialogueLine{{
		Kind: "system",
		Text: "一声清越蝉鸣，骤然响彻天地。",
	}})
	if len(lines) != 1 || lines[0].Kind != "system" || lines[0].Text != "一声清越蝉鸣，骤然响彻天地。" {
		t.Fatalf("system audio = %+v", lines)
	}
}

func TestStoryboardAudioPartitionKeepsSystemCuesOutOfSpeech(t *testing.T) {
	lines := []StoryboardDialogueLine{
		{Speaker: "方源", Text: "这句必须说出来。", Kind: "dialogue"},
		{Text: "【音效：山风穿过崖壁】", Kind: "system"},
	}
	spoken := SpokenStoryboardDialogue(lines)
	sounds := NonSpeechStoryboardAudioCues(lines)
	if len(spoken) != 1 || spoken[0].Text != "这句必须说出来。" {
		t.Fatalf("spoken dialogue = %+v", spoken)
	}
	if len(sounds) != 1 || sounds[0].Kind != "system" {
		t.Fatalf("non-speech cues = %+v", sounds)
	}
}

func TestValidateStoryboardDialogueCoverageRejectsOmissionAndTranslation(t *testing.T) {
	content := "**方源**：\n青山落日，秋月春风。\n"
	required := ExtractScriptDialogueLines(content)
	shots := []StoryboardShot{{ShotNo: 1, Dialogue: []StoryboardDialogueLine{{Speaker: "方源", Text: "Green mountains and sunset."}}}}
	if err := ValidateStoryboardDialogueCoverage(shots, content, required); err == nil || !strings.Contains(err.Error(), "not found verbatim") {
		t.Fatalf("expected verbatim validation error, got %v", err)
	}
	shots[0].Dialogue = []StoryboardDialogueLine{{Speaker: "方源", Text: "青山落日，秋月春风。"}}
	if err := ValidateStoryboardDialogueCoverage(shots, content, required); err != nil {
		t.Fatalf("valid dialogue rejected: %v", err)
	}
}

func TestStoryboardDialogueLineForTimingSpanSplitsVerbatimAtPunctuation(t *testing.T) {
	text := "魔头，三百年前你侮辱了我，夺走了我的清白之身，杀光我全家，诛了我的九族。从那刻起，我恨不得吃你肉，喝你的血！今天，我要让你生不如死！！"
	first := storyboardDialogueLineForTimingSpan(StoryboardDialogueLine{
		Kind: "dialogue", SpanStartTick: 0, SpanEndTick: 9 * 90000, ContinuesToNext: true,
	}, text, 0, 18*90000)
	second := storyboardDialogueLineForTimingSpan(StoryboardDialogueLine{
		Kind: "dialogue", SpanStartTick: 9 * 90000, SpanEndTick: 18 * 90000, ContinuesFromPrevious: true,
	}, text, 0, 18*90000)
	if first.Text == text || second.Text == text || first.Text+second.Text != text {
		t.Fatalf("split dialogue = %q + %q", first.Text, second.Text)
	}
	if !strings.ContainsAny(first.Text[len(first.Text)-3:], "，。！？；：,.!?;:") {
		t.Fatalf("first segment did not end near punctuation: %q", first.Text)
	}
}

func TestStoryboardDialogueLineForTimingSpanKeepsSystemCueDescription(t *testing.T) {
	text := "【音效：凛冽山风穿过崖壁，兵刃轻颤】"
	line := storyboardDialogueLineForTimingSpan(StoryboardDialogueLine{
		Kind: "system", SpanStartTick: 0, SpanEndTick: 90000, ContinuesToNext: true,
	}, text, 0, 2*90000)
	if line.Text != text {
		t.Fatalf("system cue = %q", line.Text)
	}
}

func TestStoryboardDialogueEquivalentDetectsPromptPlanTextDrift(t *testing.T) {
	persisted := []StoryboardDialogueLine{{
		TimingUnitID: "unit-1", Speaker: "正道群雄", Text: "魔头，三百年前你侮辱了我。", Kind: "dialogue",
		SpanStartTick: 5403750, SpanEndTick: 6296250, ContinuesToNext: true,
	}}
	relative := []StoryboardDialogueLine{{
		TimingUnitID: "unit-1", Speaker: "正道群雄", Text: "魔头，三百年前你侮辱了我。", Kind: "dialogue",
		SpanStartTick: 0, SpanEndTick: 892500, ContinuesToNext: true,
	}}
	if !storyboardDialogueEquivalent(persisted, relative) {
		t.Fatal("equivalent per-shot dialogue should ignore absolute versus relative ticks")
	}
	combined := append([]StoryboardDialogueLine(nil), relative...)
	combined[0].Text += "从那刻起，我恨不得吃你肉，喝你的血！"
	if storyboardDialogueEquivalent(persisted, combined) {
		t.Fatal("full timing-unit text must not match a persisted per-shot dialogue slice")
	}
}

func TestVideoPromptContextContractCurrentRequiresActiveMatchingIdentity(t *testing.T) {
	hash := strings.Repeat("a", 64)
	if !videoPromptContextContractCurrent("active", hash, "sha256:"+hash) {
		t.Fatal("active context with the same normalized hash should remain executable")
	}
	if videoPromptContextContractCurrent("stale", hash, hash) {
		t.Fatal("stale context must force video prompt regeneration")
	}
	if videoPromptContextContractCurrent("active", hash, strings.Repeat("b", 64)) {
		t.Fatal("context hash drift must force video prompt regeneration")
	}
}
