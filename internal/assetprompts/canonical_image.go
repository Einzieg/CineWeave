package assetprompts

import "strings"

const (
	defaultCanonicalImageSize        = "1024x1024"
	characterTurnaroundImageSize     = "2048x1152"
	characterTurnaroundAspectRatio   = "16:9"
	defaultCanonicalImageAspectRatio = "1:1"
)

func CanonicalImagePrompt(basePrompt, assetType string) string {
	basePrompt = strings.TrimSpace(basePrompt)
	requirements := canonicalImageRequirements(assetType)
	if requirements == "" {
		return basePrompt
	}
	if basePrompt == "" {
		return requirements
	}
	return basePrompt + "\n\n" + requirements
}

func CanonicalImageInput(prompt, assetType, quality string) map[string]any {
	size, aspectRatio := CanonicalImageLayout(assetType)
	input := map[string]any{
		"prompt":      strings.TrimSpace(prompt),
		"size":        size,
		"aspectRatio": aspectRatio,
		"n":           1,
	}
	if strings.TrimSpace(quality) != "" {
		input["quality"] = strings.TrimSpace(quality)
	}
	return input
}

func CanonicalImageLayout(assetType string) (size string, aspectRatio string) {
	switch normalizedAssetType(assetType) {
	case "character":
		return characterTurnaroundImageSize, characterTurnaroundAspectRatio
	default:
		return defaultCanonicalImageSize, defaultCanonicalImageAspectRatio
	}
}

func ToonflowStyleSlug(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "toonflow_visual_")
	value = strings.TrimPrefix(value, "toonflow:")
	if value == "" {
		return ""
	}
	builder := strings.Builder{}
	lastUnderscore := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastUnderscore = false
		case r == '_' || r == '-' || r == ' ' || r == '/':
			if !lastUnderscore {
				builder.WriteByte('_')
				lastUnderscore = true
			}
		default:
			return ""
		}
	}
	return strings.Trim(builder.String(), "_")
}

func ToonflowVisualTemplateSuffix(assetType string, derivative bool) string {
	switch normalizedAssetType(assetType) {
	case "character":
		if derivative {
			return "art_character_derivative"
		}
		return "art_character"
	case "scene":
		if derivative {
			return "art_scene_derivative"
		}
		return "art_scene"
	case "prop":
		if derivative {
			return "art_prop_derivative"
		}
		return "art_prop"
	default:
		return ""
	}
}

func canonicalImageRequirements(assetType string) string {
	switch normalizedAssetType(assetType) {
	case "character":
		return strings.TrimSpace(`CineWeave基础角色资产图硬性要求：
- 生成角色四视图设定图，不生成单张肖像或剧情镜头。
- 同一画面从左到右并排四栏：人像特写、正视图全身、侧视图全身、后视图全身。
- 四个视图必须是同一角色，面容、发型、体型、肤色、基础服装、配饰必须一致。
- 人像特写必须从头顶到锁骨完整展示；全身视图必须从头顶到脚底完整展示，严禁裁切。
- 使用纯净中性灰或透明感棚拍背景，均匀柔光，无复杂场景、无剧情动作、无夸张表情。
- 图中不要出现任何文字、标签、编号、水印、字幕、对话框或说明。`)
	case "prop":
		return strings.TrimSpace(`CineWeave基础道具资产图硬性要求：
- 生成可复用道具设定图，不生成剧情镜头。
- 同一画面展示正面、侧面、背面或结构细节，形状、材质、颜色保持一致。
- 背景简洁干净，道具完整居中，关键结构可检查。
- 图中不要出现任何文字、标签、编号、水印、字幕、对话框或说明。`)
	case "scene":
		return strings.TrimSpace(`CineWeave基础场景资产图硬性要求：
- 生成可复用场景环境设定图，不生成剧情镜头。
- 画面必须清楚呈现场景空间结构、时代质感、主色调、关键陈设和光影氛围。
- 不要出现人物、角色剪影、可识别文本、字幕、水印或说明。
- 保持环境可检查、可复用，避免过度电影化遮挡主体结构。`)
	default:
		return strings.TrimSpace(`CineWeave基础资产图硬性要求：
- 生成可复用资产设定图，不生成剧情镜头。
- 主体完整、居中、可检查，背景简洁干净。
- 图中不要出现任何文字、标签、编号、水印、字幕、对话框或说明。`)
	}
}

func normalizedAssetType(assetType string) string {
	return strings.ToLower(strings.TrimSpace(assetType))
}
