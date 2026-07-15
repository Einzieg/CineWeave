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

func TestNormalizeStoryboardDialoguePreservesSpeakerlessSystemAudio(t *testing.T) {
	lines := NormalizeStoryboardDialogue([]StoryboardDialogueLine{{
		Kind: "system",
		Text: "一声清越蝉鸣，骤然响彻天地。",
	}})
	if len(lines) != 1 || lines[0].Kind != "system" || lines[0].Text != "一声清越蝉鸣，骤然响彻天地。" {
		t.Fatalf("system audio = %+v", lines)
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
