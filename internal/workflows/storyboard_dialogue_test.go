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
