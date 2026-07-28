package agent

import (
	"fmt"
	"strings"
)

const (
	ProjectKindNarrative     = "narrative"
	ProjectKindCommerceVideo = "commerce_video"
)

type ProjectKindPolicy struct {
	ProjectKind     string
	PlannerRules    []string
	QuickCommandIDs []string
	tools           func() []AgentTool
}

func (p ProjectKindPolicy) Tools() []AgentTool {
	if p.tools == nil {
		return nil
	}
	return append([]AgentTool(nil), p.tools()...)
}

func PolicyForProjectKind(projectKind string) (ProjectKindPolicy, error) {
	switch strings.TrimSpace(projectKind) {
	case ProjectKindNarrative:
		return ProjectKindPolicy{
			ProjectKind: ProjectKindNarrative,
			PlannerRules: []string{
				"只使用叙事项目的原文、剧本、资产、分镜、视频和成片工具。",
				"不得调用带货视频商品、脚本裂变或直生成视频工具。",
			},
			QuickCommandIDs: []string{"inspect_project", "review_fixes", "missing_images", "missing_videos", "storyboard", "final_preview", "cancel_workflow"},
			tools: func() []AgentTool {
				return append(CommonTools(), NarrativeTools()...)
			},
		}, nil
	case ProjectKindCommerceVideo:
		return ProjectKindPolicy{
			ProjectKind: ProjectKindCommerceVideo,
			PlannerRules: []string{
				"当前项目是带货视频，只使用商品、广告脚本、脚本裂变和直生成视频工具。",
				"不得规划小说事件、改编计划、叙事剧本、分镜、镜头图片、镜头视频、时间线或成片合成工具。",
				"只有需要向用户展示、分析或引用脚本正文时才使用 commerce.script.get；自然语言改写使用 commerce.script.revise，由后端读取完整正文。不得根据标题猜测脚本身份。",
				"用户用“第 N 条”或“第 N 条脚本”指定广告脚本时，先读取 commerce.script.list，后续工具传 stableOrdinal=N 和返回的 scriptUnitsRevision；写入工具还必须传所选列表项的 revision 作为 expectedRevision，后端负责解析真实脚本 ID。",
				"禁止按标题、创建时间或 UUID 猜测脚本身份，也禁止复制或拼接 UUID。stableOrdinal 不存在、列表分页未覆盖目标或文字描述匹配多个脚本时，必须调用 agent.ask_user，给出候选脚本并允许用户自定义。",
			},
			QuickCommandIDs: []string{"commerce_product", "commerce_scripts", "commerce_create_script", "commerce_update_script", "commerce_derive_script", "commerce_generate_video", "commerce_batch_generate_video", "commerce_video_tasks", "commerce_cancel_video"},
			tools: func() []AgentTool {
				return append(CommonTools(), CommerceVideoTools()...)
			},
		}, nil
	default:
		return ProjectKindPolicy{}, fmt.Errorf("unsupported project kind %q", projectKind)
	}
}

func ToolAllowedForProjectKind(projectKind, toolName string) bool {
	policy, err := PolicyForProjectKind(projectKind)
	if err != nil {
		return false
	}
	for _, tool := range policy.Tools() {
		if tool.Name == toolName {
			return true
		}
	}
	return false
}

func ToolBelongsToDifferentProjectKind(projectKind, toolName string) bool {
	if ToolAllowedForProjectKind(projectKind, toolName) {
		return false
	}
	for _, kind := range []string{ProjectKindNarrative, ProjectKindCommerceVideo} {
		if kind != projectKind && ToolAllowedForProjectKind(kind, toolName) {
			return true
		}
	}
	return false
}
