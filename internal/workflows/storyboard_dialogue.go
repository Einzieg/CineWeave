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
	boldDialogueHeaderPattern = regexp.MustCompile(`^\*\*([^*]+)\*\*(?:（([^）]*)）|\(([^)]*)\))?\s*[：:]?\s*(.*)$`)
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
		if match := boldDialogueHeaderPattern.FindStringSubmatch(line); match != nil {
			speaker := strings.TrimSpace(match[1])
			delivery := strings.TrimSpace(firstNonEmptyString(match[2], match[3]))
			inlineText := strings.TrimSpace(match[4])
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

func NormalizeStoryboardDialogue(lines []StoryboardDialogueLine) []StoryboardDialogueLine {
	result := make([]StoryboardDialogueLine, 0, len(lines))
	for _, line := range lines {
		line.Speaker = strings.TrimSpace(line.Speaker)
		line.Text = strings.TrimSpace(line.Text)
		line.Delivery = strings.TrimSpace(line.Delivery)
		line.Kind = normalizeDialogueKind(line.Kind, line.Speaker)
		if line.Speaker == "" || line.Text == "" {
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
	_, found := map[string]struct{}{
		"画面": {}, "动作": {}, "音效": {}, "镜头": {}, "字幕": {}, "字幕提示": {},
		"画幅": {}, "风格": {}, "氛围": {}, "主要人物": {}, "主要地点": {}, "场景": {},
		"地点": {}, "时间": {}, "冲突": {}, "结果": {}, "情绪": {}, "dialogue": {},
		"visual": {}, "action": {}, "sound": {}, "camera": {}, "location": {}, "time": {},
	}[normalized]
	return found
}

func dialogueLineKey(line StoryboardDialogueLine) string {
	return strings.TrimSpace(line.Speaker) + "\x00" + strings.TrimSpace(line.Text)
}
