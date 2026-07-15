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

func containsAnyFold(value string, candidates []string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, strings.ToLower(candidate)) {
			return true
		}
	}
	return false
}
