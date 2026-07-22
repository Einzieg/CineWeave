package assetprompts

import (
	"fmt"
	"strings"
)

func VisualStyleFamily(styleSlug string) string {
	styleSlug = ToonflowStyleSlug(styleSlug)
	switch {
	case strings.HasPrefix(styleSlug, "3d_"):
		return "3d"
	case strings.HasPrefix(styleSlug, "2d_"):
		return "2d"
	case strings.HasPrefix(styleSlug, "realpeople_"):
		return "live_action"
	default:
		return ""
	}
}

func ValidateGeneratedCardStyle(styleSlug, basePrompt, consistencyPrompt string) error {
	family := VisualStyleFamily(styleSlug)
	if family == "" {
		return nil
	}
	positive := strings.ToLower(strings.TrimSpace(basePrompt + "\n" + consistencyPrompt))
	switch family {
	case "3d":
		if !containsAnyFold(positive, []string{"3d", "三维"}) {
			return fmt.Errorf("视觉手册要求 3D 风格，但生成结果没有明确 3D 风格锚点")
		}
		if containsAnyFold(positive, []string{"真人都市", "真人实拍", "真实摄影画质", "35mm全画幅摄影", "live-action photography"}) {
			return fmt.Errorf("视觉手册要求 3D 风格，但生成结果混入真人摄影风格")
		}
	case "2d":
		if !containsAnyFold(positive, []string{"2d", "二维", "动画", "插画", "anime"}) {
			return fmt.Errorf("视觉手册要求 2D 风格，但生成结果没有明确 2D 风格锚点")
		}
		if containsAnyFold(positive, []string{"真人实拍", "真实摄影画质", "35mm全画幅摄影", "3d渲染", "3d rendered"}) {
			return fmt.Errorf("视觉手册要求 2D 风格，但生成结果混入真人摄影或 3D 风格")
		}
	case "live_action":
		if !containsAnyFold(positive, []string{"真人", "实拍", "摄影", "live-action", "cinematic photography"}) {
			return fmt.Errorf("视觉手册要求真人风格，但生成结果没有明确真人摄影风格锚点")
		}
		if containsAnyFold(positive, []string{"3d渲染", "3d rendered", "二次元", "赛璐璐", "cel shading"}) {
			return fmt.Errorf("视觉手册要求真人风格，但生成结果混入 3D 或 2D 动画风格")
		}
	}
	return nil
}

// ValidateCanonicalAssetBaseline keeps temporary story states out of reusable
// character references. Injuries, blood, combat poses, and held props belong to
// shot-level derived assets rather than the canonical identity card.
func ValidateCanonicalAssetBaseline(assetType, basePrompt, consistencyPrompt string) error {
	if strings.TrimSpace(assetType) != "character" {
		return nil
	}
	positive := canonicalPositiveAssertions(basePrompt + "\n" + consistencyPrompt)
	forbidden := []string{
		"浑身浴血", "遍体鳞伤", "遍体伤口", "流血", "鲜血淋漓", "鲜血浸透",
		"染血", "血迹", "血泊", "开放性伤口", "断肢", "尸体",
		"blood-soaked", "covered in blood", "fresh blood", "bleeding", "open wound",
		"fresh wound", "battle wounds", "gore", "mutilated",
	}
	for _, term := range forbidden {
		if strings.Contains(positive, term) {
			return fmt.Errorf("核心角色资产包含剧情瞬时伤情 %q；请恢复为无血迹、无开放伤口的中性基础设定，并把受伤状态留给镜头衍生资产", term)
		}
	}
	return nil
}

func canonicalPositiveAssertions(value string) string {
	clauses := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		switch r {
		case '\n', '\r', '。', '！', '？', '；', ';', '，', ',':
			return true
		default:
			return false
		}
	})
	positive := make([]string, 0, len(clauses))
	negations := []string{
		"无血", "无伤", "无战损", "无泥污", "无汗", "无泪", "没有", "不得", "禁止",
		"排除", "不包含", "不固化", "不出现", "避免", "移除", "without", " no ", "exclude", "avoid",
	}
	for _, clause := range clauses {
		clause = strings.TrimSpace(clause)
		if clause == "" || containsAnyFold(" "+clause+" ", negations) {
			continue
		}
		positive = append(positive, clause)
	}
	return strings.Join(positive, "\n")
}

func containsAnyFold(value string, candidates []string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, strings.ToLower(candidate)) {
			return true
		}
	}
	return false
}
