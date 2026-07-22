package assetprompts

import (
	"encoding/json"
	"strings"
)

type CanonicalCardSource struct {
	Description  string
	VisualTraits json.RawMessage
	SceneContext string
}

// BuildCanonicalCardSource separates reusable character identity facts from
// transient scene state before an LLM sees the asset-card request. The model
// still receives the extracted identity, but not injuries, combat actions, or
// scene prose that should be represented by derived shot assets.
func BuildCanonicalCardSource(assetType, description string, visualTraits json.RawMessage, sceneContext string) CanonicalCardSource {
	source := CanonicalCardSource{
		Description:  RuntimePromptField(description, 1200),
		VisualTraits: visualTraits,
		SceneContext: RuntimePromptField(sceneContext, RuntimeAssetSceneContextMaxRunes),
	}
	if strings.TrimSpace(assetType) != "character" {
		return source
	}
	source.Description = sanitizeCanonicalCharacterText(source.Description)
	source.VisualTraits = sanitizeCanonicalCharacterTraits(visualTraits)
	source.SceneContext = ""
	return source
}

var transientCharacterTraitKeys = map[string]struct{}{
	"action": {}, "actions": {}, "emotion": {}, "expression": {}, "injuries": {}, "injury": {},
	"location": {}, "pose": {}, "props": {}, "scene": {}, "state": {}, "weather": {},
	"weapon": {}, "weapons": {}, "wound": {}, "wounds": {},
}

func sanitizeCanonicalCharacterTraits(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return json.RawMessage(sanitizeCanonicalCharacterText(string(raw)))
	}
	value = sanitizeCanonicalCharacterValue(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return encoded
}

func sanitizeCanonicalCharacterValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cleaned := make(map[string]any, len(typed))
		for key, item := range typed {
			if _, transient := transientCharacterTraitKeys[strings.ToLower(strings.TrimSpace(key))]; transient {
				continue
			}
			cleaned[key] = sanitizeCanonicalCharacterValue(item)
		}
		return cleaned
	case []any:
		cleaned := make([]any, 0, len(typed))
		for _, item := range typed {
			cleaned = append(cleaned, sanitizeCanonicalCharacterValue(item))
		}
		return cleaned
	case string:
		return sanitizeCanonicalCharacterText(typed)
	default:
		return value
	}
}

func sanitizeCanonicalCharacterText(value string) string {
	replacements := []struct{ old, replacement string }{
		{"在魔窟外山巅被正道群雄围困", ""},
		{"脸色因失血而苍白", "肤色偏苍白"},
		{"脸色因失血苍白", "肤色偏苍白"},
		{"部分湿发贴于染血面颊与颈侧", "长发自然垂落于面颊与颈侧"},
		{"部分贴在染血面颊和颈侧", "自然垂落于面颊和颈侧"},
		{"残破的碧绿大袍，被鲜血浸透", "完整洁净的碧绿色古代长袍"},
		{"残破染血的素色碧绿古装长衫", "完整洁净的素色碧绿古装长衫"},
		{"遍体伤口、暗红血迹、浑身浴血却脊背挺直的状态", "脊背挺直、平静克制的中性状态"},
		{"被暗红鲜血浸透", "保持完整洁净"},
		{"被鲜血浸透", "保持完整洁净"},
		{"遍体鳞伤", ""}, {"遍体伤口", ""}, {"浑身浴血", ""},
		{"暗红血迹", ""}, {"鲜血淋漓", ""}, {"开放性伤口", ""},
		{"衣摆撕裂", ""}, {"残破磨损", ""}, {"染血", ""},
		{"流血", ""}, {"血迹", ""}, {"浴血", ""},
		{"blood-soaked", "clean"}, {"covered in blood", "clean"},
		{"fresh blood", ""}, {"bleeding", ""}, {"open wound", ""},
		{"fresh wound", ""}, {"battle wounds", ""}, {"gore", ""}, {"mutilated", ""},
	}
	for _, replacement := range replacements {
		value = strings.ReplaceAll(value, replacement.old, replacement.replacement)
	}
	for _, duplicate := range []string{"、、", "，，", "；；", "  ", "，、", "、，", "；，", "，；"} {
		for strings.Contains(value, duplicate) {
			value = strings.ReplaceAll(value, duplicate, string([]rune(duplicate)[0]))
		}
	}
	return strings.Trim(strings.TrimSpace(value), "，、；;,. ")
}
