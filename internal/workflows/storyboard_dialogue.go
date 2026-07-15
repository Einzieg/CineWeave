package workflows

import (
	"fmt"
	"regexp"
	"strings"
)

type StoryboardDialogueLine struct {
	TimingUnitID          string `json:"timingUnitId,omitempty"`
	Speaker               string `json:"speaker"`
	Text                  string `json:"text"`
	Delivery              string `json:"delivery,omitempty"`
	Kind                  string `json:"kind,omitempty"`
	SpanStartTick         int64  `json:"spanStartTick,omitempty"`
	SpanEndTick           int64  `json:"spanEndTick,omitempty"`
	SourceStartOffset     *int   `json:"sourceStartOffset,omitempty"`
	SourceEndOffset       *int   `json:"sourceEndOffset,omitempty"`
	ContinuesFromPrevious bool   `json:"continuesFromPrevious,omitempty"`
	ContinuesToNext       bool   `json:"continuesToNext,omitempty"`
}

var (
	boldDialogueHeaderPattern = regexp.MustCompile(`^\*\*([^*]+)\*\*(.*)$`)
	speakerDeliveryPattern    = regexp.MustCompile(`^(.+?)(?:（([^）]*)）|\(([^)]*)\))$`)
	plainDialoguePattern      = regexp.MustCompile(`^([^：:]{1,40})[：:]\s*(.+)$`)
)

func ExtractScriptDialogueLines(content string) []StoryboardDialogueLine {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	result := make([]StoryboardDialogueLine, 0)
	pendingSpeaker := ""
	pendingDelivery := ""

	for _, rawLine := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "  "))
		if line == "" {
			pendingSpeaker = ""
			pendingDelivery = ""
			continue
		}
		if strings.HasPrefix(line, "#") || line == "---" {
			pendingSpeaker = ""
			pendingDelivery = ""
			continue
		}
		if speaker, delivery, inlineText, matched := parseBoldDialogueHeader(line); matched {
			if isScreenplayFieldLabel(speaker) {
				pendingSpeaker = ""
				pendingDelivery = ""
				if strings.EqualFold(strings.TrimSpace(speaker), "dialogue") && inlineText != "" {
					if parsed, ok := parsePlainDialogueLine(inlineText); ok {
						result = append(result, parsed)
					}
				}
				continue
			}
			pendingSpeaker = speaker
			pendingDelivery = delivery
			if inlineText != "" {
				result = append(result, newStoryboardDialogueLine(speaker, inlineText, delivery))
			}
			continue
		}
		if pendingSpeaker != "" {
			text := strings.TrimSpace(strings.TrimPrefix(line, "- "))
			if text != "" {
				result = append(result, newStoryboardDialogueLine(pendingSpeaker, text, pendingDelivery))
			}
			continue
		}
		if parsed, ok := parsePlainDialogueLine(line); ok {
			result = append(result, parsed)
		}
	}
	return result
}

func parseBoldDialogueHeader(line string) (string, string, string, bool) {
	match := boldDialogueHeaderPattern.FindStringSubmatch(strings.TrimSpace(line))
	if match == nil {
		return "", "", "", false
	}
	speaker, delivery := normalizeDialogueSpeaker(match[1])
	remainder := strings.TrimSpace(match[2])
	if suffixDelivery, rest, ok := consumeLeadingDialogueDelivery(remainder); ok {
		delivery = firstNonEmptyString(suffixDelivery, delivery)
		remainder = rest
	}
	remainder = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(remainder), "：:"))
	return speaker, delivery, remainder, true
}

func consumeLeadingDialogueDelivery(value string) (string, string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", value, false
	}
	pairs := []struct {
		open  string
		close string
	}{{"（", "）"}, {"(", ")"}}
	for _, pair := range pairs {
		if !strings.HasPrefix(value, pair.open) {
			continue
		}
		end := strings.Index(value[len(pair.open):], pair.close)
		if end < 0 {
			return "", value, false
		}
		end += len(pair.open)
		delivery := strings.TrimSpace(value[len(pair.open):end])
		rest := strings.TrimSpace(value[end+len(pair.close):])
		return delivery, rest, true
	}
	return "", value, false
}

func normalizeDialogueSpeaker(value string) (string, string) {
	value = strings.TrimSpace(value)
	value = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "**"), "**"))
	value = strings.TrimSpace(strings.TrimRight(value, "：:"))
	match := speakerDeliveryPattern.FindStringSubmatch(value)
	if match == nil {
		return value, ""
	}
	return strings.TrimSpace(match[1]), strings.TrimSpace(firstNonEmptyString(match[2], match[3]))
}

func NormalizeStoryboardDialogue(lines []StoryboardDialogueLine) []StoryboardDialogueLine {
	result := make([]StoryboardDialogueLine, 0, len(lines))
	for _, line := range lines {
		var extractedDelivery string
		line.Speaker, extractedDelivery = normalizeDialogueSpeaker(line.Speaker)
		line.Text = strings.TrimSpace(line.Text)
		line.Delivery = strings.TrimSpace(firstNonEmptyString(line.Delivery, extractedDelivery))
		line.Kind = normalizeDialogueKind(line.Kind, line.Speaker)
		if line.Text == "" || (line.Speaker == "" && line.Kind == "dialogue") {
			continue
		}
		result = append(result, line)
	}
	return result
}

func ValidateStoryboardDialogueCoverage(shots []StoryboardShot, scriptContent string, required []StoryboardDialogueLine) error {
	required = NormalizeStoryboardDialogue(required)
	if len(required) == 0 {
		return nil
	}
	actualCounts := map[string]int{}
	for shotIndex := range shots {
		shots[shotIndex].Dialogue = NormalizeStoryboardDialogue(shots[shotIndex].Dialogue)
		for _, line := range shots[shotIndex].Dialogue {
			if !strings.Contains(scriptContent, line.Text) {
				return fmt.Errorf("storyboard shot %d contains dialogue not found verbatim in the script: %s: %s", shots[shotIndex].ShotNo, line.Speaker, line.Text)
			}
			actualCounts[dialogueLineKey(line)]++
		}
	}
	missing := make([]string, 0)
	for _, line := range required {
		key := dialogueLineKey(line)
		if actualCounts[key] > 0 {
			actualCounts[key]--
			continue
		}
		missing = append(missing, line.Speaker+"："+line.Text)
	}
	if len(missing) == 0 {
		return nil
	}
	preview := missing
	if len(preview) > 3 {
		preview = preview[:3]
	}
	return fmt.Errorf("storyboard omitted %d script dialogue lines; first missing: %s", len(missing), strings.Join(preview, " | "))
}

func newStoryboardDialogueLine(speaker, text, delivery string) StoryboardDialogueLine {
	return StoryboardDialogueLine{
		Speaker:  strings.TrimSpace(speaker),
		Text:     strings.TrimSpace(text),
		Delivery: strings.TrimSpace(delivery),
		Kind:     normalizeDialogueKind("", speaker),
	}
}

func parsePlainDialogueLine(line string) (StoryboardDialogueLine, bool) {
	match := plainDialoguePattern.FindStringSubmatch(strings.TrimSpace(line))
	if match == nil {
		return StoryboardDialogueLine{}, false
	}
	speaker := strings.TrimSpace(match[1])
	text := strings.TrimSpace(match[2])
	if speaker == "" || text == "" || isScreenplayFieldLabel(speaker) {
		return StoryboardDialogueLine{}, false
	}
	return newStoryboardDialogueLine(speaker, text, ""), true
}

func normalizeDialogueKind(kind, speaker string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "dialogue", "voiceover", "narration", "system":
		return kind
	}
	normalizedSpeaker := strings.ToLower(strings.TrimSpace(speaker))
	switch {
	case strings.Contains(normalizedSpeaker, "旁白") || strings.Contains(normalizedSpeaker, "narrator"):
		return "narration"
	case strings.Contains(normalizedSpeaker, "内心") || strings.Contains(normalizedSpeaker, "心声") || strings.Contains(normalizedSpeaker, "vo") || strings.Contains(normalizedSpeaker, "os"):
		return "voiceover"
	case strings.Contains(normalizedSpeaker, "系统") || strings.Contains(normalizedSpeaker, "播报"):
		return "system"
	default:
		return "dialogue"
	}
}

func isScreenplayFieldLabel(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(normalized, "**"), "**"))
	normalized = strings.TrimSpace(strings.TrimRight(normalized, "：:"))
	_, found := map[string]struct{}{
		"画面": {}, "动作": {}, "音效": {}, "环境音": {}, "音乐": {}, "镜头": {}, "字幕": {}, "字幕提示": {},
		"画幅": {}, "风格": {}, "氛围": {}, "人物": {}, "主要人物": {}, "主要地点": {}, "场景": {},
		"地点": {}, "时间": {}, "时间流逝": {}, "冲突": {}, "结果": {}, "情绪": {}, "备注": {}, "dialogue": {},
		"visual": {}, "action": {}, "sound": {}, "sfx": {}, "music": {}, "camera": {}, "location": {}, "time": {}, "characters": {},
	}[normalized]
	return found
}

func dialogueLineKey(line StoryboardDialogueLine) string {
	speaker, _ := normalizeDialogueSpeaker(line.Speaker)
	return speaker + "\x00" + normalizeDialogueTextForComparison(line.Text)
}

func normalizeDialogueTextForComparison(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}
