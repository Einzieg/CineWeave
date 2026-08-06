package workflows

import (
	"strings"
	"testing"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
)

func TestValidateNovelScriptFidelityAcceptsVerbatimDialogue(t *testing.T) {
	source := `方源望向门口。“哥哥，你来了。”他心中一凛：“这一步不能后退。”`
	script := `# 第一集

方源望向门口。

**方源**：
哥哥，你来了。

**方源（心声）**：
这一步不能后退。`
	if err := validateNovelScriptFidelity(source, script); err != nil {
		t.Fatalf("validate faithful script: %v", err)
	}
}

func TestValidateNovelScriptFidelityRejectsInventedDialogue(t *testing.T) {
	source := `方源望向门口。“哥哥，你来了。”`
	script := `**方源**：哥哥，你来了。

**方源**：这一世由我改写。`
	err := validateNovelScriptFidelity(source, script)
	if err == nil || !strings.Contains(err.Error(), "这一世由我改写") {
		t.Fatalf("error = %v, want invented dialogue rejection", err)
	}
}

func TestValidateNovelScriptFidelityDoesNotRequireNarrativeQuoteAsSpeech(t *testing.T) {
	source := `方源望向门口。“哥哥，你怎么站在窗边淋雨？”`
	script := `方源沉默地关上窗户。`
	if err := validateNovelScriptFidelity(source, script); err != nil {
		t.Fatalf("narrative quote should be reviewed semantically, not forced into speech: %v", err)
	}
}

func TestValidateNovelScriptFidelityIgnoresColonRichVisualDirections(t *testing.T) {
	source := `方源望向青山与落日，轻声一笑：“青山落日，秋月春风。”`
	script := `**画面**

无声记忆叠化而来：前世地球中的岁月、华夏学子的身影与陌生天地。

一瞬间，回溯的光影掠过：山巅的血色、燃烧的晚霞与飞散的岁月。

**对白**

方源：青山落日，秋月春风。`

	if err := validateNovelScriptFidelity(source, script); err != nil {
		t.Fatalf("visual directions should not be validated as speech: %v", err)
	}
}

func TestValidateNovelScriptFidelityRejectsCopiedNovelNarration(t *testing.T) {
	source := `夜雨敲窗，方源站在窗边。`
	script := `**旁白**：夜雨敲窗，方源站在窗边。`
	err := validateNovelScriptFidelity(source, script)
	if err == nil || !strings.Contains(err.Error(), "小说叙述不得整段转为旁白") {
		t.Fatalf("error = %v, want copied narration rejection", err)
	}
}

func TestValidateNovelScriptFidelityRejectsUnquotedProseAsDialogue(t *testing.T) {
	source := `方源知道自己已经重生。`
	script := `**方源**：自己已经重生。`
	err := validateNovelScriptFidelity(source, script)
	if err == nil || !strings.Contains(err.Error(), "自己已经重生") {
		t.Fatalf("error = %v, want unquoted prose dialogue rejection", err)
	}
}

func TestValidateNovelScriptFidelityAllowsVerbatimInnerVoice(t *testing.T) {
	source := `方源心中一凛，这一步不能后退。`
	script := `**方源（心声）**：这一步不能后退。`
	if err := validateNovelScriptFidelity(source, script); err != nil {
		t.Fatalf("validate verbatim inner voice: %v", err)
	}
}

func TestSourceScriptFidelityPromptIncludesRetryFeedback(t *testing.T) {
	base := promptsvc.RenderedPrompt{RenderedText: "base", RenderedHash: "old", Source: "system_active"}
	rendered := sourceScriptFidelityPrompt(base, 2, sourceScriptFidelityReport{InventedDialogue: []string{"方源：新增台词"}})
	if rendered.RenderedHash == "old" || !strings.Contains(rendered.Source, "novel_fidelity_v1_attempt_2") {
		t.Fatalf("rendered provenance = %#v", rendered)
	}
	for _, expected := range []string{"台词零捏造", "上一版未通过确定性忠实度校验", "方源：新增台词"} {
		if !strings.Contains(rendered.RenderedText, expected) {
			t.Fatalf("rendered prompt missing %q: %s", expected, rendered.RenderedText)
		}
	}
}

func TestWorkflowScriptEpisodeInstructionRequiresVerbatimDialogue(t *testing.T) {
	instruction := workflowScriptEpisodeInstruction("忠实改编", 1, 1, scriptNovelChapterContext{Title: "第一卷 第一节"})
	for _, expected := range []string{"台词零捏造", "逐字取自当前分集原文", "环境音、动作音和音乐只能写成舞台说明"} {
		if !strings.Contains(instruction, expected) {
			t.Fatalf("instruction missing %q: %s", expected, instruction)
		}
	}
}
