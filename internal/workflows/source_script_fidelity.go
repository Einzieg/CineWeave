package workflows

import (
	"fmt"
	"strings"
	"unicode"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
)

const sourceScriptFidelityMaxAttempts = 3

type sourceScriptFidelityReport struct {
	InventedDialogue []string
}

func (r sourceScriptFidelityReport) Error() string {
	parts := make([]string, 0, 2)
	if len(r.InventedDialogue) > 0 {
		parts = append(parts, "存在无法在本集原文中逐字定位的台词："+strings.Join(previewFidelityItems(r.InventedDialogue), "；"))
	}
	if len(parts) == 0 {
		return "小说剧本忠实度校验失败"
	}
	return strings.Join(parts, "。")
}

func validateNovelScriptFidelity(sourceContent, scriptContent string) error {
	sourceNormalized := normalizeFidelityText(sourceContent)
	quotedPassages := normalizedQuotedPassages(sourceContent)
	dialogue := ExtractScriptDialogueLines(scriptContent)
	report := sourceScriptFidelityReport{}
	for _, line := range dialogue {
		text := strings.TrimSpace(line.Text)
		normalized := normalizeFidelityText(text)
		if normalized == "" {
			continue
		}
		if line.Kind == "narration" {
			report.InventedDialogue = append(report.InventedDialogue, formatFidelityDialogue(line.Speaker, text)+"（小说叙述不得整段转为旁白）")
			continue
		}
		if !strings.Contains(sourceNormalized, normalized) || (line.Kind == "dialogue" && !fidelityTextInPassages(normalized, quotedPassages)) {
			report.InventedDialogue = append(report.InventedDialogue, formatFidelityDialogue(line.Speaker, text))
		}
	}
	if len(report.InventedDialogue) > 0 {
		return report
	}
	return nil
}

func sourceScriptFidelityPrompt(base promptsvc.RenderedPrompt, attempt int, previous error) promptsvc.RenderedPrompt {
	if attempt <= 0 {
		attempt = 1
	}
	contract := strings.TrimSpace(`
<cineweave_novel_fidelity_contract>
这是强制入库契约，不是创作建议：
1. 台词零捏造：所有角色台词、心声、独白、旁白和画外音都必须逐字来自当前分集原文；不得新增、改写、缩写、概括或补全任何可听见文本。
2. 普通角色对白只能逐字取自当前分集原文引号中的直接话语；原文叙述不得改写成角色对白。
3. 禁止把小说叙述逐句复制成旁白、解说或画外音。叙述必须视觉化为画面、动作、表情、构图、场面调度和非语言音效。
4. 心声只用于角色明确的心理内容，必须逐字取自当前分集原文并控制数量，不能代替大段叙事。
5. 音效、环境声和音乐只写舞台说明，绝不能归到人物台词。
6. 输出前逐行核对：每一句可听见文本都必须满足上述来源和类型约束。
</cineweave_novel_fidelity_contract>`)
	if previous != nil {
		contract += "\n<cineweave_previous_rejection>\n上一版未通过确定性忠实度校验：" + previous.Error() + "\n请重新输出完整剧本正文，不要只输出修订片段。\n</cineweave_previous_rejection>"
	}
	next := base
	next.RenderedText = strings.TrimSpace(base.RenderedText) + "\n\n" + contract
	next.RenderedHash = promptsvc.HashText(next.RenderedText)
	next.Source = strings.TrimSpace(base.Source) + "+novel_fidelity_v1_attempt_" + fmt.Sprintf("%d", attempt)
	return next
}

func normalizeFidelityText(value string) string {
	var builder strings.Builder
	for _, current := range strings.ToLower(value) {
		if unicode.IsLetter(current) || unicode.IsNumber(current) {
			builder.WriteRune(current)
		}
	}
	return builder.String()
}

func normalizedQuotedPassages(content string) []string {
	pairs := [][2]rune{{'“', '”'}, {'「', '」'}, {'『', '』'}, {'"', '"'}}
	result := make([]string, 0)
	for _, pair := range pairs {
		runes := []rune(content)
		start := -1
		for index, current := range runes {
			if start < 0 {
				if current == pair[0] {
					start = index + 1
				}
				continue
			}
			if current != pair[1] {
				continue
			}
			if normalized := normalizeFidelityText(string(runes[start:index])); normalized != "" {
				result = append(result, normalized)
			}
			start = -1
		}
	}
	return result
}

func fidelityTextInPassages(text string, passages []string) bool {
	for _, passage := range passages {
		if strings.Contains(passage, text) {
			return true
		}
	}
	return false
}

func formatFidelityDialogue(speaker, text string) string {
	speaker = strings.TrimSpace(speaker)
	if speaker == "" {
		return strings.TrimSpace(text)
	}
	return speaker + "：" + strings.TrimSpace(text)
}

func previewFidelityItems(items []string) []string {
	const limit = 3
	result := make([]string, 0, limit)
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		runes := []rune(item)
		if len(runes) > 48 {
			item = string(runes[:48]) + "..."
		}
		result = append(result, item)
		if len(result) == limit {
			break
		}
	}
	return result
}
