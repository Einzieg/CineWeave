package assetprompts

import (
	"strings"
	"testing"
)

func TestRuntimeManualSummaryKeepsCoreRulesAndDropsExamples(t *testing.T) {
	manual := strings.Repeat("普通背景段落，不应占用运行时提示词。\n", 80) + `
---
name: art_prop
---
# 道具图像生成

## 提示词模板
古风道具设定图，3D渲染风格，PBR材质，电影级光影。

## 严禁项
- 禁止出现人物、手部、文字、水印。
- 必须保持正面、侧面、背面、细节视图一致。

## 完整生成示例
` + strings.Repeat("示例输出不应进入运行时提示词。\n", 200)

	summary := RuntimeManualSummary(manual, 320)
	if len([]rune(summary)) > 320 {
		t.Fatalf("summary length = %d, want <= 320: %q", len([]rune(summary)), summary)
	}
	for _, want := range []string{"道具图像生成", "3D渲染风格", "PBR材质", "禁止出现人物"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q: %q", want, summary)
		}
	}
	if strings.Contains(summary, "示例输出不应进入运行时提示词") {
		t.Fatalf("summary kept example text: %q", summary)
	}
}

func TestRuntimeImagePromptPreservesTailRequirements(t *testing.T) {
	prompt := strings.Repeat("asset detail with style constraints\n", 500) + "\nCineWeave基础角色资产图硬性要求：角色四视图设定图"
	compact := RuntimeImagePrompt(prompt)
	if len([]rune(compact)) > RuntimeCanonicalImagePromptMaxRunes {
		t.Fatalf("compact length = %d, want <= %d", len([]rune(compact)), RuntimeCanonicalImagePromptMaxRunes)
	}
	if !strings.Contains(compact, "CineWeave基础角色资产图硬性要求") || !strings.Contains(compact, "角色四视图设定图") {
		t.Fatalf("compact prompt did not preserve tail requirements: %q", compact)
	}
}
