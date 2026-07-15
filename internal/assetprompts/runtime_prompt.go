package assetprompts

import "strings"

const (
	RuntimeDirectorManualMaxRunes          = 800
	RuntimeVisualManualMaxRunes            = 1200
	RuntimeToonflowPrefixMaxRunes          = 900
	RuntimeToonflowTemplateMaxRunes        = 1800
	RuntimeAssetProfileMaxRunes            = 1400
	RuntimeAssetBasePromptMaxRunes         = 2600
	RuntimeAssetConsistencyMaxRunes        = 1400
	RuntimeAssetNegativeMaxRunes           = 900
	RuntimeAssetVisualTraitsMaxRunes       = 700
	RuntimeAssetCardVisualPrefixMaxRunes   = 1400
	RuntimeAssetCardVisualTemplateMaxRunes = 3000
	RuntimeAssetCardManualFallbackMaxRunes = 3200
	RuntimeAssetSceneContextMaxRunes       = 6000
	RuntimeCanonicalImagePromptMaxRunes    = 9000
)

func RuntimePromptField(value string, maxRunes int) string {
	return trimRunes(normalizePromptText(value), maxRunes)
}

func RuntimeManualSummary(value string, maxRunes int) string {
	value = stripLeadingFrontMatter(normalizePromptText(value))
	value = removeFencedBlocks(value)
	value = dropAfterFirstMarker(value, []string{
		"## 完整生成示例",
		"## 快速参考卡",
		"## 使用方式",
		"## 文件结构",
		"### 示例输出",
		"### 输入",
	})
	selected := selectRuntimeManualLines(value)
	if strings.TrimSpace(selected) == "" {
		selected = value
	}
	return RuntimePromptField(selected, maxRunes)
}

func RuntimeImagePrompt(value string) string {
	value = normalizePromptText(value)
	if runeCount(value) <= RuntimeCanonicalImagePromptMaxRunes {
		return value
	}
	return headTailRunes(value, 6000, 2900)
}

func normalizePromptText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	normalized := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if !blank {
				normalized = append(normalized, "")
			}
			blank = true
			continue
		}
		normalized = append(normalized, line)
		blank = false
	}
	return strings.TrimSpace(strings.Join(normalized, "\n"))
}

func stripLeadingFrontMatter(value string) string {
	if !strings.HasPrefix(value, "---\n") {
		return value
	}
	if end := strings.Index(value[4:], "\n---"); end >= 0 {
		return strings.TrimSpace(value[end+8:])
	}
	return value
}

func removeFencedBlocks(value string) string {
	lines := strings.Split(value, "\n")
	filtered := make([]string, 0, len(lines))
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}

func dropAfterFirstMarker(value string, markers []string) string {
	cut := -1
	for _, marker := range markers {
		if index := strings.Index(value, marker); index > 0 && (cut < 0 || index < cut) {
			cut = index
		}
	}
	if cut < 0 {
		return value
	}
	return strings.TrimSpace(value[:cut])
}

func selectRuntimeManualLines(value string) string {
	keywords := []string{
		"风格", "质感", "色彩", "光影", "构图", "材质", "比例",
		"视图", "正面", "侧面", "背面", "细节", "提示词模板", "固定",
		"必须", "必守", "严禁", "禁止", "不得", "保持", "参考",
		"人物", "角色", "场景", "道具", "PBR", "3D", "cinematic", "no ",
	}
	lines := strings.Split(value, "\n")
	selected := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(selected) > 0 && selected[len(selected)-1] != "" {
				selected = append(selected, "")
			}
			continue
		}
		if strings.Contains(trimmed, "仅输出提示词正文") || strings.Contains(trimmed, "不得附加任何解释") {
			continue
		}
		if isMarkdownTableSeparator(trimmed) {
			continue
		}
		if len([]rune(trimmed)) > 220 {
			trimmed = trimRunes(trimmed, 220)
		}
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "-") || containsAny(trimmed, keywords) {
			selected = append(selected, trimmed)
		}
	}
	return normalizePromptText(strings.Join(selected, "\n"))
}

func isMarkdownTableSeparator(value string) bool {
	if !strings.Contains(value, "|") {
		return false
	}
	for _, r := range value {
		switch r {
		case '|', '-', ':', ' ':
			continue
		default:
			return false
		}
	}
	return true
}

func containsAny(value string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(value, keyword) {
			return true
		}
	}
	return false
}

func trimRunes(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return strings.TrimSpace(string(runes[:maxRunes]))
}

func headTailRunes(value string, headRunes, tailRunes int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= headRunes+tailRunes {
		return string(runes)
	}
	head := strings.TrimSpace(string(runes[:headRunes]))
	tail := strings.TrimSpace(string(runes[len(runes)-tailRunes:]))
	return strings.TrimSpace(head + "\n\n" + tail)
}

func runeCount(value string) int {
	return len([]rune(value))
}
