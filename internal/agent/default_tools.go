package agent

import (
	"encoding/json"

	"github.com/Einzieg/cineweave/internal/authz"
)

func DefaultRegistry() (*Registry, error) {
	return NewRegistry(DefaultTools()...)
}

func DefaultTools() []AgentTool {
	return append(CommonTools(), NarrativeTools()...)
}

func CommonTools() []AgentTool {
	return filterAgentTools(allNarrativeTools(), func(tool AgentTool) bool {
		_, ok := commonToolNames[tool.Name]
		return ok
	})
}

func NarrativeTools() []AgentTool {
	return filterAgentTools(allNarrativeTools(), func(tool AgentTool) bool {
		_, common := commonToolNames[tool.Name]
		return !common
	})
}

var commonToolNames = map[string]struct{}{
	"agent.ask_user":                          {},
	"artifact.get":                            {},
	"artifact.list":                           {},
	"artifact.preview_url":                    {},
	"project.delete":                          {},
	"project.delete.retry":                    {},
	"project.deletion_impact":                 {},
	"project.production_rebuild":              {},
	"project.production_rebuild_impact":       {},
	"project.production_rebuild.retry_failed": {},
	"project.update":                          {},
	"provider.list_status":                    {},
	"workflow.cancel":                         {},
	"workflow.read_nodes":                     {},
	"workflow.read_runs":                      {},
}

func filterAgentTools(tools []AgentTool, include func(AgentTool) bool) []AgentTool {
	filtered := make([]AgentTool, 0, len(tools))
	for _, tool := range tools {
		if include(tool) {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

func allNarrativeTools() []AgentTool {
	tools := []AgentTool{
		draftTool("agent.ask_user", "询问用户", "当目标存在多种合理路径或缺少关键偏好时，暂停任务并向用户提出一个可选择的问题。", authz.PermissionProjectRead, objectSchemaRequired(map[string]any{
			"question": stringSchema("需要用户回答的问题。"),
			"options": arraySchema("建议选项，通常 2 到 4 个。", objectSchemaRequired(map[string]any{
				"id":          stringSchema("选项稳定 ID。"),
				"label":       stringSchema("选项展示文案。"),
				"description": stringSchema("选项说明。"),
				"nextGoal":    stringSchema("选择该项后建议继续执行的目标。"),
				"value":       objectSchema(nil, false),
			}, "id", "label")),
			"allowCustom":     booleanSchema("是否允许用户自定义下一步。"),
			"defaultOptionId": stringSchema("推荐默认选项 ID。"),
		}, "question"), true),
		readTool("project.read_summary", "项目摘要", "读取项目来源、剧本、资产、分镜、工作流和审阅摘要。", authz.PermissionProjectRead, emptyObjectSchema()),
		readTool("source.list", "原文列表", "按稳定顺序分页列出项目来源及其 revision。", authz.PermissionSourceRead, objectSchema(map[string]any{
			"status": enumSchema("来源状态筛选。", []string{"active", "archived", "all"}),
			"limit":  integerSchema("单页数量。", 1, 50),
			"cursor": stringSchema("上一页返回的 nextCursor。"),
		}, false)),
		readTool("source.list_chapters", "分集章节", "分页列出指定小说来源的分卷、分集和章节；必须使用 source.list 返回的准确来源 ID。", authz.PermissionSourceRead, objectSchemaRequired(map[string]any{
			"sourceId": stringSchema("source.list 返回的准确来源 ID。"),
			"limit":    integerSchema("单页数量。", 1, 50),
			"cursor":   stringSchema("上一页返回的 nextCursor。"),
		}, "sourceId")),
		readTool("script.list", "剧本列表", "按稳定顺序分页列出项目剧本、revision 和当前版本摘要。", authz.PermissionScriptRead, objectSchema(map[string]any{
			"status": enumSchema("剧本状态筛选。", []string{"active", "archived", "all"}),
			"limit":  integerSchema("单页数量。", 1, 50),
			"cursor": stringSchema("上一页返回的 nextCursor。"),
		}, false)),
		readTool("script.get", "读取剧本", "读取指定剧本、版本和分集摘要；正文使用返回的 contentTarget 调用 content.read。", authz.PermissionScriptRead, objectSchemaRequired(map[string]any{
			"scriptId":      stringSchema("script.list 返回的准确剧本 ID。"),
			"versionId":     stringSchema("版本 ID。为空时读取当前版本。"),
			"episodeLimit":  integerSchema("单页分集数量。", 1, 50),
			"episodeCursor": stringSchema("上一页返回的 nextEpisodeCursor。"),
		}, "scriptId")),
		readTool("asset.list", "资产列表", "按稳定顺序分页列出核心资产、revision、提示词和参考图摘要。", authz.PermissionAssetRead, objectSchema(map[string]any{
			"assetType":    enumSchema("资产类型筛选。", []string{"character", "scene", "prop"}),
			"status":       enumSchema("资产状态筛选。", []string{"active", "archived", "all"}),
			"reviewStatus": enumSchema("审核状态筛选。", []string{"pending", "approved", "rejected", "needs_edit"}),
			"staleState":   enumSchema("过期状态筛选。", []string{"fresh", "upstream_changed", "needs_regeneration"}),
			"promptReady":  booleanSchema("只返回提示词已就绪或未就绪的资产。"),
			"limit":        integerSchema("单页数量。", 1, 100),
			"cursor":       stringSchema("上一页返回的 nextCursor。"),
		}, false)),
		readTool("asset.get", "读取资产卡", "按资产 ID 或准确名称读取完整资产卡、当前提示词和生成状态。", authz.PermissionAssetRead, objectSchema(map[string]any{
			"assetId":           stringSchema("asset.list 返回的准确资产 ID；优先使用。"),
			"assetName":         stringSchema("资产准确名称；同名时会要求改用 assetId。"),
			"includePreviewUrl": booleanSchema("是否为参考图生成短期预览 URL。"),
		}, false)),
		readTool("asset.impact", "资产归档影响", "读取归档核心资产会影响的场景、参考图、镜头需求和媒体数量。", authz.PermissionAssetRead, objectSchemaRequired(map[string]any{
			"assetId": stringSchema("asset.list 返回的准确资产 ID。"),
		}, "assetId")),
		readTool("asset.reference.list", "资产参考图版本", "分页读取资产当前、历史和失败的参考图版本，可选生成短期预览 URL。", authz.PermissionAssetRead, objectSchemaRequired(map[string]any{
			"assetId":           stringSchema("asset.list 返回的准确资产 ID。"),
			"status":            enumSchema("参考图状态筛选。", []string{"active", "archived", "failed", "all"}),
			"includePreviewUrl": booleanSchema("是否生成短期预览 URL。"),
			"limit":             integerSchema("单页数量。", 1, 100),
			"cursor":            stringSchema("上一页返回的 nextCursor。"),
		}, "assetId")),
		readTool("shot_asset.list_requirements", "镜头资产需求", "列出当前生产代的镜头资产需求、衍生图状态和结构化校验问题。", authz.PermissionAssetRead, objectSchema(map[string]any{
			"storyboardShotId": stringSchema("可选分镜镜头 ID。"),
			"scriptEpisodeId":  stringSchema("可选剧本分集 ID；提供后只返回该分集的镜头资产需求。"),
			"reviewStatus":     enumSchema("审核状态筛选。", []string{"all", "pending", "approved", "needs_edit", "rejected"}),
			"limit":            integerSchema("返回数量。", 1, 1000),
			"cursor":           stringSchema("上一页返回的 nextCursor。"),
		}, false)),
		readTool("storyboard.list", "分镜列表", "分页列出当前生产代的分镜镜头。", authz.PermissionProjectRead, objectSchema(map[string]any{
			"scriptEpisodeId": stringSchema("可选剧本分集 ID。"),
			"workflowRunId":   stringSchema("可选来源工作流 ID。"),
			"limit":           integerSchema("单页数量。", 1, 200),
			"cursor":          stringSchema("上一页返回的 nextCursor。"),
		}, false)),
		readTool("workflow.read_runs", "任务列表", "按状态分页读取最近 workflow runs。", authz.PermissionWorkflowRead, objectSchema(map[string]any{
			"status": enumSchema("任务状态范围。", []string{"active", "terminal", "all"}),
			"limit":  integerSchema("单页数量。", 1, 100),
			"cursor": stringSchema("上一页返回的 nextCursor。"),
		}, false)),
		readTool("workflow.read_nodes", "任务节点", "分页读取指定 workflow 的节点运行详情。", authz.PermissionWorkflowRead, workflowRunPageSchema(false)),
		readTool("workflow.read_shots", "任务镜头", "分页读取指定 workflow 的镜头和可选预览状态。", authz.PermissionWorkflowRead, workflowRunPageSchema(true)),
		readTool("review.list_items", "审阅问题", "按状态和类型分页读取项目审阅问题。", authz.PermissionProjectRead, objectSchema(map[string]any{
			"status":     enumSchema("审阅问题状态。", []string{"open", "resolved", "ignored", "all"}),
			"severity":   stringSchema("可选严重等级。"),
			"category":   stringSchema("可选分类。"),
			"entityType": stringSchema("可选实体类型。"),
			"limit":      integerSchema("单页数量。", 1, 200),
			"cursor":     stringSchema("上一页返回的 nextCursor。"),
		}, false)),
		readTool("artifact.list", "成果列表", "按稳定顺序分页列出项目成果及其来源和媒体元数据。", authz.PermissionArtifactRead, objectSchema(map[string]any{
			"type":                  stringSchema("成果类型筛选。"),
			"includePreviewUrl":     booleanSchema("是否为可预览成果附加短期 URL。"),
			"previewExpiresSeconds": integerSchema("列表预览 URL 有效秒数。", 60, 3600),
			"limit":                 integerSchema("单页数量。", 1, 100),
			"cursor":                stringSchema("上一页返回的 nextCursor。"),
		}, false)),
		readTool("artifact.get", "读取成果", "按准确 Artifact ID 读取项目成果元数据。", authz.PermissionArtifactRead, objectSchemaRequired(map[string]any{
			"artifactId": stringSchema("artifact.list 返回的准确 Artifact ID。"),
		}, "artifactId")),
		readTool("artifact.preview_url", "成果预览", "生成短期 artifact 预览 URL。", authz.PermissionArtifactRead, objectSchemaRequired(map[string]any{
			"artifactId":     stringSchema("Artifact ID。"),
			"expiresSeconds": integerSchema("预览 URL 有效秒数。", 60, 86400),
		}, "artifactId")),
		readTool("provider.list_status", "供应商状态", "读取供应商、模型、限额、熔断和成本摘要。", authz.PermissionProviderRead, emptyObjectSchema()),
		destructiveTool("project.clear_production_content", "清空生产内容", "清空项目中除小说原文及其分卷分集外的生产内容。该操作会创建新的空白生产代，移除剧本、事件、改编计划、资产、分镜、时间线、成片和审阅数据；供应商、模型、项目设置及手册绑定保持不变。", authz.PermissionProjectDelete, objectSchemaRequired(map[string]any{
			"confirmation": enumSchema("必须明确确认保留小说原文。", []string{"preserve_novel_sources"}),
			"reason":       stringSchema("清空原因。"),
		}, "confirmation")),

		costedDraftTool("review.run", "运行审阅", "运行 deterministic review 和可选 agent review。", authz.PermissionProjectRead, objectSchema(map[string]any{
			"reviewType":                 enumSchema("审阅类型。", []string{"project", "workflow", "asset", "storyboard", "timeline", "final_video"}),
			"includeAgent":               booleanSchema("是否启用 agent review。"),
			"includeDeterministicChecks": booleanSchema("是否运行 deterministic checks。"),
		}, false), false),
		costedDraftTool("review.generate_fix", "生成修复建议", "生成 review fix 草稿，不直接应用。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"itemId":      stringSchema("Review item ID。"),
			"mode":        enumSchema("修复模式。", []string{"deterministic", "agent"}),
			"instruction": stringSchema("给 agent fix 的额外要求。"),
		}, "itemId"), false),
		readTool("prompt.render_test", "提示词渲染测试", "使用当前项目绑定和变量渲染 prompt，不调用供应商。", authz.PermissionPromptRead, objectSchemaRequired(map[string]any{
			"templateKey": stringSchema("Prompt template key。"),
			"variables":   objectSchema(nil, false),
			"input":       objectSchema(nil, false),
		}, "templateKey")),
		costedDraftTool("script.rewrite_preview", "剧本改写预览", "生成改写预览，不写入剧本版本。", authz.PermissionScriptRead, objectSchemaRequired(map[string]any{
			"scriptId":      stringSchema("剧本 ID。"),
			"versionId":     stringSchema("剧本版本 ID。为空时使用当前版本。"),
			"instruction":   stringSchema("改写要求。"),
			"sourceSceneId": stringSchema("可选场景 ID。"),
		}, "scriptId", "instruction"), false),

		writeTool("source.update", "覆盖原文", "更新原文标题、正文、格式、状态或重新拆分章节。", authz.PermissionSourceWrite, objectSchemaRequired(map[string]any{
			"sourceId":         stringSchema("原文 ID。"),
			"expectedRevision": integerSchema("source.get 或 source.list 返回的当前原文 revision。", 1, 1000000000),
			"patch": objectSchema(map[string]any{
				"sourceType":       enumSchema("原文类型。", []string{"novel", "script", "brief"}),
				"title":            stringSchema("标题。"),
				"content":          stringSchema("正文内容；大正文应使用 content.write.begin/chunk/commit。"),
				"contentFormat":    enumSchema("正文格式。", []string{"plain_text", "markdown"}),
				"originalFileName": stringSchema("原始文件名。"),
				"storageKey":       stringSchema("原始文件对象存储键。"),
				"status":           enumSchema("状态。", []string{"ready", "processing", "processed", "failed", "archived"}),
				"metadata":         objectSchema(nil, false),
				"chapters": arraySchema("小说的完整章节集合。", objectSchemaRequired(map[string]any{
					"id":           stringSchema("已有章节 ID；保留章节身份时传入。"),
					"chapterIndex": integerSchema("全局章节序号。", 1, 1000000),
					"volumeIndex":  integerSchema("卷序号。", 1, 1000000),
					"sectionIndex": integerSchema("卷内章节序号。", 1, 1000000),
					"volumeTitle":  stringSchema("卷标题。"),
					"chapterTitle": stringSchema("章节标题。"),
					"content":      stringSchema("章节正文。"),
				}, "content")),
				"splitChapters": booleanSchema("小说正文更新后是否重新自动拆分章节。"),
			}, false),
		}, "sourceId", "expectedRevision", "patch"), true),
		writeTool("script.update_episode", "覆盖剧本分集", "更新指定剧本分集标题、正文、格式或审核状态，并标记下游需要重生成。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"episodeId":        stringSchema("剧本分集 ID。"),
			"expectedRevision": integerSchema("script.get 返回的当前分集 revision。", 1, 1000000000),
			"patch": objectSchema(map[string]any{
				"episodeTitle":  stringSchema("分集标题。"),
				"content":       stringSchema("分集剧本正文。"),
				"contentFormat": enumSchema("正文格式。", []string{"plain_text", "markdown"}),
				"reviewStatus":  enumSchema("审核状态。", []string{"pending", "approved", "rejected", "needs_edit"}),
				"staleState":    enumSchema("过期状态。", []string{"fresh", "needs_regeneration", "upstream_changed"}),
				"metadata":      objectSchema(nil, false),
			}, false),
		}, "episodeId", "expectedRevision", "patch"), true),
		asyncWriteTool("script.generate_from_source", "生成剧本", "从来源分集逐集生成剧本；默认追加或更新来源对应的项目当前剧本，一条小说分集只对应一条剧本分集。只有用户明确要求另一版剧本时才设置 createNewScript。", authz.PermissionScriptWrite, objectSchema(map[string]any{
			"sourceId":        stringSchema("来源 ID。为空时使用项目最新未归档来源。"),
			"scriptId":        stringSchema("要追加分集的现有剧本 ID。为空时使用该来源对应的项目当前剧本。"),
			"createNewScript": booleanSchema("是否显式创建另一套剧本。不能与 scriptId 同时使用。"),
			"planId":          stringSchema("改编计划 ID。"),
			"title":           stringSchema("仅新建剧本时使用的标题。"),
			"instruction":     stringSchema("生成要求。"),
			"chapterIds":      arraySchema("要改编的小说分集 ID 列表。每个 ID 会生成独立剧本分集。", stringSchema("小说分集 ID。")),
			"chapterRange":    stringSchema("自然语言分集范围，例如：第一卷1-10节、1-10集、前十节。"),
			"maxConcurrency":  integerSchema("分集并发数，范围 1-4。", 1, 4),
		}, false), true),
		costedWriteTool("script.rewrite", "改写剧本", "改写剧本并创建新版本。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptId":         stringSchema("剧本 ID。"),
			"versionId":        stringSchema("剧本版本 ID。为空时使用当前版本。"),
			"expectedRevision": integerSchema("script.get 或 script.list 返回的当前剧本 revision。", 1, 1000000000),
			"instruction":      stringSchema("改写要求。"),
			"activate":         booleanSchema("是否创建后激活。"),
		}, "scriptId", "instruction"), true),
		writeTool("script.create_version", "创建剧本版本", "创建剧本新版本。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptId":         stringSchema("剧本 ID。"),
			"expectedRevision": integerSchema("script.get 或 script.list 返回的当前剧本 revision。", 1, 1000000000),
			"content":          map[string]any{"type": "string", "maxLength": 49152, "description": "版本内容；超长正文应按分集创建或使用长内容协议。"},
			"contentFormat":    enumSchema("内容格式。", []string{"plain_text", "markdown"}),
			"sourceType":       stringSchema("版本来源类型。"),
			"metadata":         objectSchema(nil, false),
			"activate":         booleanSchema("是否创建后激活。"),
		}, "scriptId", "expectedRevision", "content"), true),
		writeTool("script.activate_version", "激活剧本版本", "切换剧本当前版本。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptId":         stringSchema("剧本 ID。"),
			"versionId":        stringSchema("版本 ID。"),
			"expectedRevision": integerSchema("script.get 或 script.list 返回的当前剧本 revision。", 1, 1000000000),
		}, "scriptId", "versionId", "expectedRevision"), true),
		destructiveTool("script.archive_version", "归档剧本版本", "归档明确的非当前剧本版本，并标记其下游需要重生成。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptId":         stringSchema("剧本 ID。"),
			"versionId":        stringSchema("要归档的非当前版本 ID。"),
			"expectedRevision": integerSchema("script.get 或 script.list 返回的当前剧本 revision。", 1, 1000000000),
			"reason":           stringSchema("归档原因。"),
		}, "scriptId", "versionId", "expectedRevision")),
		destructiveTool("script.delete", "删除剧本", "归档整个剧本及其版本，并标记下游分镜、镜头和成片需要重生成。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptId":         stringSchema("剧本 ID。"),
			"expectedRevision": integerSchema("script.get 或 script.list 返回的当前剧本 revision。", 1, 1000000000),
			"reason":           stringSchema("删除原因。"),
		}, "scriptId", "expectedRevision")),
		writeTool("asset.update", "更新资产", "精确替换资产卡字段；编辑提示词时使用 basePrompt、consistencyPrompt、negativePrompt。", authz.PermissionAssetWrite, objectSchemaRequired(map[string]any{
			"assetId":          stringSchema("资产 ID。"),
			"expectedRevision": integerSchema("读取资产时返回的 revision；并发修改后旧 revision 会被拒绝。", 1, 2147483647),
			"patch": objectSchema(map[string]any{
				"assetType":         enumSchema("资产类型。", []string{"character", "scene", "prop"}),
				"name":              stringSchema("资产名称。"),
				"description":       stringSchema("资产描述。"),
				"profile":           freeformObjectSchema("结构化资产外观和稳定设定。"),
				"basePrompt":        stringSchema("基础生图提示词。"),
				"consistencyPrompt": stringSchema("跨镜头一致性提示词。"),
				"negativePrompt":    stringSchema("负向提示词。"),
				"lockReference":     booleanSchema("是否锁定当前主参考图。"),
				"visualTraits":      freeformObjectSchema("结构化视觉特征。"),
				"metadata":          freeformObjectSchema("资产扩展元数据。"),
				"status":            enumSchema("资产状态。", []string{"draft", "prompt_ready", "image_running", "image_succeeded", "image_failed", "archived"}),
			}, false),
		}, "assetId", "expectedRevision", "patch"), true),
		writeTool("asset.revise_prompt", "修订资产提示词", "根据自然语言要求读取并修改现有资产提示词，保留未要求改变的设定。可直接按资产准确名称执行。", authz.PermissionAssetWrite, objectSchemaRequired(map[string]any{
			"assetId":     stringSchema("资产 ID；与 assetName 至少提供一个。"),
			"assetName":   stringSchema("资产准确名称；与 assetId 至少提供一个。"),
			"instruction": stringSchema("用户对提示词的具体修改要求。"),
			"fields": arraySchema("限定修改字段；为空时修订全部提示词字段。", enumSchema("提示词字段。", []string{
				"basePrompt", "consistencyPrompt", "negativePrompt",
			})),
		}, "instruction"), true),
		childWorkflowTool("asset.batch_generate_prompts", "批量生成资产提示词", "使用不可变项目与视觉手册快照，并发生成指定核心资产的完整提示词；单项失败不会终止整批。", authz.PermissionAssetGenerate, objectSchemaRequired(map[string]any{
			"assetIds":       arraySchema("目标核心资产 ID，最多 500 个。", stringSchema("核心资产 ID。")),
			"maxConcurrency": integerSchema("最大并发。", 1, 16),
			"force":          booleanSchema("是否强制重新生成已就绪提示词。"),
		}, "assetIds")),
		childWorkflowTool("asset.batch_generate_images", "批量生成资产图片", "使用不可变项目、视觉手册、模型和提示词快照，并发生成指定核心资产参考图；支持部分完成和失败项独立重试。", authz.PermissionAssetGenerate, objectSchemaRequired(map[string]any{
			"assetIds":       arraySchema("目标核心资产 ID，最多 500 个。", stringSchema("核心资产 ID。")),
			"maxConcurrency": integerSchema("最大并发。", 1, 16),
			"force":          booleanSchema("是否强制重新生成已有参考图。"),
		}, "assetIds")),
		writeTool("shot_asset.review_requirements", "审核镜头资产需求", "按当前生产代批量校验并审核镜头资产需求。请求批准时只会批准结构化校验通过的需求，未通过项会转为需修改并返回原因。", authz.PermissionAssetWrite, objectSchemaRequired(map[string]any{
			"requirementIds":  arraySchema("目标需求 ID；为空时处理全部待审核需求。", stringSchema("镜头资产需求 ID。")),
			"scriptEpisodeId": stringSchema("可选剧本分集 ID；提供后只审核该分集内匹配的需求。"),
			"reviewStatus":    enumSchema("目标审核状态。", []string{"approved", "needs_edit", "rejected"}),
			"note":            stringSchema("审核说明。"),
		}, "reviewStatus"), true),
		writeTool("shot_asset.update_requirement", "修正镜头资产需求", "修正当前生产代中的单个镜头资产需求。可重新关联核心资产、修正需求类型和补充镜头状态；保存后必须重新审核。", authz.PermissionAssetWrite, objectSchemaRequired(map[string]any{
			"requirementId": stringSchema("镜头资产需求 ID。"),
			"patch": objectSchema(map[string]any{
				"assetId":         stringSchema("重新关联的核心资产 ID。"),
				"requirementType": stringSchema("与资产类型匹配的需求类型，例如 character_appearance、scene_environment、prop_state。"),
				"costume":         stringSchema("角色服装状态。"),
				"pose":            stringSchema("角色姿态。"),
				"expression":      stringSchema("角色表情。"),
				"action":          stringSchema("镜头动作。"),
				"cameraRelation":  stringSchema("资产与机位、景别或构图的关系。"),
				"sceneState":      stringSchema("场景在当前镜头中的状态。"),
				"propState":       stringSchema("道具在当前镜头中的状态。"),
				"prompt":          stringSchema("镜头衍生资产提示词。"),
			}, false),
		}, "requirementId", "patch"), true),
		destructiveTool("shot_asset.skip_requirement", "跳过镜头资产需求", "确认某项镜头资产需求不应参与当前镜头生产时，将其标记为跳过并保留审计记录。", authz.PermissionAssetWrite, objectSchemaRequired(map[string]any{
			"requirementId": stringSchema("镜头资产需求 ID。"),
			"reason":        stringSchema("跳过原因。"),
		}, "requirementId", "reason")),
		destructiveTool("source.delete", "删除原文", "归档原文并阻止其继续作为生产入口。", authz.PermissionSourceWrite, objectSchemaRequired(map[string]any{
			"sourceId":         stringSchema("原文 ID。"),
			"expectedRevision": integerSchema("读取原文时返回的 revision。", 1, 2147483647),
			"reason":           stringSchema("删除原因。"),
		}, "sourceId", "expectedRevision")),
		destructiveTool("source.delete_chapter", "删除原文章节", "删除已通过读取动作精确解析的小说章节，重排后续序号并标记下游需要重新生成。", authz.PermissionSourceWrite, objectSchemaRequired(map[string]any{
			"sourceId":         stringSchema("小说来源 UUID。"),
			"chapterId":        stringSchema("通过章节列表解析得到的章节 UUID。"),
			"expectedRevision": integerSchema("读取原文时返回的 revision。", 1, 2147483647),
			"reason":           stringSchema("删除原因。"),
		}, "sourceId", "chapterId", "expectedRevision")),
		destructiveTool("asset.delete", "删除资产", "归档核心资产，并标记分镜、镜头和成片需要重生成。", authz.PermissionAssetWrite, objectSchemaRequired(map[string]any{
			"assetId":          stringSchema("核心资产 ID。"),
			"expectedRevision": integerSchema("读取资产时返回的 revision。", 1, 2147483647),
			"reason":           stringSchema("删除原因。"),
		}, "assetId", "expectedRevision")),
		writeTool("storyboard.update_shot", "更新分镜", "修改分镜镜头字段。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"shotId": stringSchema("镜头 ID。"),
			"patch": map[string]any{
				"type": "object", "minProperties": 1, "additionalProperties": false,
				"properties": map[string]any{
					"visual":               stringSchema("画面描述。"),
					"camera":               stringSchema("机位与运镜。"),
					"motion":               stringSchema("主体动作。"),
					"mood":                 stringSchema("氛围。"),
					"plannedDurationTicks": integerSchema("与项目帧率对齐的镜头时长 tick。", 1, 2147483647),
					"imagePrompt":          stringSchema("手工镜头图片提示词。"),
					"videoPrompt":          stringSchema("手工镜头视频提示词。"),
				},
			},
		}, "shotId", "patch"), true),
		writeTool("storyboard.reorder", "重排分镜", "调整分镜镜头顺序。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"shotIds": arraySchema("按目标顺序排列的镜头 ID。", stringSchema("镜头 ID。")),
		}, "shotIds"), true),
		writeTool("timeline.update_clip", "更新时间线片段", "修改时间线片段字段。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"timelineId":               stringSchema("时间线 ID。"),
			"clipId":                   stringSchema("时间线片段 ID。"),
			"expectedTimelineRevision": integerSchema("当前时间线 revision。", 1, 2147483647),
			"expectedRevision":         integerSchema("当前片段 revision。", 1, 2147483647),
			"patch":                    objectSchema(nil, false),
		}, "timelineId", "clipId", "expectedTimelineRevision", "expectedRevision", "patch"), true),
		writeTool("review.resolve_item", "解决审阅项", "将审阅项标记为已解决。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"itemId": stringSchema("Review item ID。"),
			"note":   stringSchema("解决说明。"),
		}, "itemId"), true),
		writeTool("review.ignore_item", "忽略审阅项", "将审阅项标记为已忽略。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"itemId": stringSchema("Review item ID。"),
			"note":   stringSchema("忽略说明。"),
		}, "itemId"), true),
		writeTool("review.reopen_item", "重新打开审阅项", "将已解决或已忽略的审阅项重新设为待处理。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"itemId": stringSchema("Review item ID。"),
		}, "itemId"), true),
		writeTool("review.apply_fix", "应用修复", "应用已批准的 review fix。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"fixId":               stringSchema("Review fix ID。"),
			"resolveReviewItem":   booleanSchema("应用后是否同时解决对应审阅问题。"),
			"triggerRegeneration": booleanSchema("是否在应用后触发再生成。Project Agent 默认不会自动再生成。"),
		}, "fixId"), true),
		writeTool("review.dismiss_fix", "忽略修复", "忽略 review fix 草稿。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"fixId": stringSchema("Review fix ID。"),
		}, "fixId"), true),

		childWorkflowTool("workflow.start", "启动任务", "启动受控 workflow。", authz.PermissionWorkflowRun, objectSchemaRequired(map[string]any{
			"workflowType": enumSchema("Workflow 类型。", []string{"extract_novel_events", "generate_adaptation_plan", "adaptation_plan_to_script", "source_to_script", "parse_script_scenes", "script_to_assets", "script_to_storyboard", "batch_generate_derived_asset_images", "script_to_video", "full_production", "compose_timeline"}),
			"input":        objectSchema(nil, false),
		}, "workflowType")),
		workflowTool("workflow.cancel", "取消任务", "取消运行中的 workflow。", authz.PermissionWorkflowCancel, objectSchemaRequired(map[string]any{
			"workflowRunId": stringSchema("Workflow run ID。"),
			"reason":        stringSchema("取消原因。"),
		}, "workflowRunId")),
		readTool("shot.status", "镜头状态", "读取镜头生产状态。", authz.PermissionWorkflowRead, objectSchema(map[string]any{
			"scriptEpisodeId":   stringSchema("可选剧本分集 ID；提供后只读取该分集当前激活分镜方案。"),
			"scriptSceneId":     stringSchema("可选剧本场景 ID。"),
			"workflowRunId":     stringSchema("可选来源工作流 ID。"),
			"storyboardPlanId":  stringSchema("可选分镜方案 ID。"),
			"includePreviewUrl": booleanSchema("是否生成镜头媒体短期预览 URL。"),
			"limit":             integerSchema("最大返回数量。", 1, 200),
		}, false)),
		childWorkflowTool("shot.generate_image_prompts", "生成图片提示词", "为指定或缺少图片提示词的镜头并发生成提示词。", authz.PermissionWorkflowRun, objectSchema(map[string]any{
			"shotIds":         arraySchema("目标镜头 ID；为空时选择所有缺少图片提示词的镜头。", stringSchema("镜头 ID。")),
			"scriptEpisodeId": stringSchema("可选剧本分集 ID；提供后只处理该分集当前激活分镜方案。"),
			"scriptSceneId":   stringSchema("可选剧本场景 ID。"),
			"workflowRunId":   stringSchema("可选来源工作流 ID。"),
			"maxConcurrency":  integerSchema("最大并发。", 1, 16),
		}, false)),
		childWorkflowTool("shot.generate_video_prompts", "生成视频提示词", "为指定或缺少已审核契约的镜头并发生成并审核视频提示词。", authz.PermissionWorkflowRun, objectSchema(map[string]any{
			"shotIds":         arraySchema("目标镜头 ID；为空时选择所有缺少可执行提示词的镜头。", stringSchema("镜头 ID。")),
			"scriptEpisodeId": stringSchema("可选剧本分集 ID；提供后只处理该分集当前激活分镜方案。"),
			"scriptSceneId":   stringSchema("可选剧本场景 ID。"),
			"workflowRunId":   stringSchema("可选来源工作流 ID。"),
			"maxConcurrency":  integerSchema("最大并发。", 1, 16),
		}, false)),
		childWorkflowTool("shot.generate_missing_images", "生成缺失图片", "为缺图镜头启动图片生成。", authz.PermissionWorkflowRun, objectSchema(map[string]any{
			"shotIds":         arraySchema("可选目标镜头 ID。", stringSchema("镜头 ID。")),
			"scriptEpisodeId": stringSchema("可选剧本分集 ID；提供后只处理该分集当前激活分镜方案。"),
			"scriptSceneId":   stringSchema("可选剧本场景 ID。"),
			"workflowRunId":   stringSchema("可选来源工作流 ID。"),
			"maxConcurrency":  integerSchema("最大并发。", 1, 16),
		}, false)),
		childWorkflowTool("shot.generate_missing_videos", "生成缺失视频", "为缺视频镜头启动视频生成。", authz.PermissionWorkflowRun, objectSchema(map[string]any{
			"shotIds":         arraySchema("可选目标镜头 ID。", stringSchema("镜头 ID。")),
			"scriptEpisodeId": stringSchema("可选剧本分集 ID；提供后只处理该分集当前激活分镜方案。"),
			"scriptSceneId":   stringSchema("可选剧本场景 ID。"),
			"workflowRunId":   stringSchema("可选来源工作流 ID。"),
			"maxConcurrency":  integerSchema("最大并发。", 1, 8),
		}, false)),
		childWorkflowTool("shot.cancel_running_videos", "取消镜头视频", "取消运行中的镜头视频任务。", authz.PermissionWorkflowCancel, emptyObjectSchema()),
		childWorkflowTool("timeline.compose", "合成时间线", "触发时间线合成。", authz.PermissionWorkflowRun, objectSchema(map[string]any{
			"timelineId": stringSchema("时间线 ID。"),
		}, false)),
		writeTool("final_video.activate", "激活成片", "激活最终视频版本。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"versionId":        stringSchema("成片版本 ID。"),
			"expectedRevision": integerSchema("当前成片版本 revision。", 1, 2147483647),
		}, "versionId", "expectedRevision"), true),

		costedAdminTool("provider.test_model", "测试模型", "测试供应商模型可用性。", authz.PermissionProviderManage, objectSchemaRequired(map[string]any{
			"modelId":  stringSchema("供应商模型 ID。"),
			"testType": enumSchema("测试类型。", []string{"connection_test", "auth_test", "model_discovery_test", "text_generation_test", "streaming_test", "image_generation_test", "video_generation_test"}),
			"prompt":   stringSchema("测试 prompt。"),
			"input":    objectSchema(nil, false),
		}, "modelId", "prompt")),
		adminTool("provider.install_catalog_preset", "安装渠道预设", "安装供应商 catalog preset。", authz.PermissionProviderManage, objectSchemaRequired(map[string]any{
			"providerKey": stringSchema("Catalog provider key。"),
			"name":        stringSchema("供应商账号名称。"),
			"setup":       objectSchema(nil, false),
			"credential":  objectSchema(nil, false),
			"models":      arraySchema("要安装的模型列表。", objectSchema(nil, false)),
			"bindProfiles": arraySchema("要创建的业务模型绑定。", objectSchema(map[string]any{
				"profileKey": stringSchema("业务模型 profile key。"),
				"modelKey":   stringSchema("模型 key。"),
				"priority":   integerSchema("优先级。", 0, 10000),
				"weight":     integerSchema("权重。", 0, 10000),
			}, false)),
		}, "providerKey")),
		adminTool("provider.update_account", "更新供应商", "更新供应商账号配置。", authz.PermissionProviderManage, objectSchemaRequired(map[string]any{
			"accountId": stringSchema("供应商账号 ID。"),
			"patch": objectSchema(map[string]any{
				"name":     stringSchema("供应商名称。"),
				"baseUrl":  stringSchema("API 地址。"),
				"authType": stringSchema("认证类型。"),
				"status":   enumSchema("状态。", []string{"active", "disabled", "error"}),
				"config":   objectSchema(nil, false),
			}, false),
		}, "accountId", "patch")),
		adminTool("provider.update_model", "更新模型", "更新供应商模型配置。", authz.PermissionProviderManage, objectSchemaRequired(map[string]any{
			"modelId": stringSchema("供应商模型 ID。"),
			"patch": objectSchema(map[string]any{
				"modelKey":     stringSchema("模型标识。"),
				"displayName":  stringSchema("显示名称。"),
				"modality":     enumSchema("模态。", []string{"text", "image", "video", "audio", "embedding", "multimodal"}),
				"status":       enumSchema("状态。", []string{"active", "disabled", "deprecated", "error"}),
				"capabilities": objectSchema(nil, false),
			}, false),
		}, "modelId", "patch")),
		adminTool("provider.attest_video_capability", "审批视频模型能力", "批准或拒绝当前视频模型能力快照；仅用于当前模型和当前能力 hash。", authz.PermissionProviderManage, objectSchemaRequired(map[string]any{
			"modelId":                stringSchema("供应商模型 ID。"),
			"variantKey":             stringSchema("视频能力变体 key。"),
			"capabilitySnapshotHash": stringSchema("当前能力快照 hash。"),
			"decision":               enumSchema("审批结论。", []string{"approved", "rejected"}),
			"reason":                 stringSchema("审批原因。"),
			"evidence":               objectSchema(nil, false),
		}, "modelId", "variantKey", "capabilitySnapshotHash", "decision", "reason")),
		adminTool("provider.verify_video_capability", "验证视频模型能力", "通过 Adapter 契约测试验证当前视频模型能力快照。", authz.PermissionProviderManage, objectSchemaRequired(map[string]any{
			"modelId":                stringSchema("供应商模型 ID。"),
			"variantKey":             stringSchema("视频能力变体 key。"),
			"capabilitySnapshotHash": stringSchema("当前能力快照 hash。"),
			"verificationMode":       enumSchema("验证方式。", []string{"adapter_contract_test"}),
			"providerTestRunId":      stringSchema("可选供应商测试运行 ID。"),
			"reason":                 stringSchema("验证原因。"),
		}, "modelId", "variantKey", "capabilitySnapshotHash", "verificationMode")),
		adminTool("prompt.create_version", "创建提示词版本", "创建 prompt version。", authz.PermissionPromptManage, objectSchemaRequired(map[string]any{
			"templateId":      stringSchema("Prompt template ID。"),
			"title":           stringSchema("版本标题。"),
			"content":         stringSchema("Prompt 内容。"),
			"contentFormat":   enumSchema("内容格式。", []string{"text", "markdown"}),
			"variablesSchema": objectSchema(nil, false),
			"metadata":        objectSchema(nil, false),
			"activate":        booleanSchema("是否创建后激活。"),
		}, "templateId", "content")),
		adminTool("prompt.activate_version", "激活提示词版本", "激活 prompt version。", authz.PermissionPromptManage, objectSchemaRequired(map[string]any{
			"versionId": stringSchema("Prompt version ID。"),
		}, "versionId")),
	}
	return append(tools, extendedProjectControlTools()...)
}

func readTool(name, label, description, permission string, schema json.RawMessage) AgentTool {
	return AgentTool{Name: name, Label: label, Description: description, Risk: ToolRiskRead, Permission: permission, InputSchema: schema}
}

func draftTool(name, label, description, permission string, schema json.RawMessage, requiresApproval bool) AgentTool {
	return AgentTool{Name: name, Label: label, Description: description, Risk: ToolRiskDraft, Permission: permission, InputSchema: schema, RequiresApproval: requiresApproval}
}

func costedDraftTool(name, label, description, permission string, schema json.RawMessage, requiresApproval bool) AgentTool {
	tool := draftTool(name, label, description, permission, schema, requiresApproval)
	tool.Effects.MaySpendProvider = true
	return tool
}

func writeTool(name, label, description, permission string, schema json.RawMessage, requiresApproval bool) AgentTool {
	return AgentTool{Name: name, Label: label, Description: description, Risk: ToolRiskWrite, Permission: permission, InputSchema: schema, RequiresApproval: requiresApproval, Effects: ToolEffects{WritesProject: true}}
}

func costedWriteTool(name, label, description, permission string, schema json.RawMessage, requiresApproval bool) AgentTool {
	tool := writeTool(name, label, description, permission, schema, requiresApproval)
	tool.Effects.MaySpendProvider = true
	return tool
}

func workflowTool(name, label, description, permission string, schema json.RawMessage) AgentTool {
	return AgentTool{Name: name, Label: label, Description: description, Risk: ToolRiskWorkflow, Permission: permission, InputSchema: schema, RequiresApproval: true, Effects: ToolEffects{WritesProject: true}}
}

func childWorkflowTool(name, label, description, permission string, schema json.RawMessage) AgentTool {
	tool := workflowTool(name, label, description, permission, schema)
	tool.Effects.MaySpendProvider = true
	tool.Effects.StartsWorkflow = true
	tool.StartsWorkflow = true
	return tool
}

func asyncWriteTool(name, label, description, permission string, schema json.RawMessage, requiresApproval bool) AgentTool {
	tool := writeTool(name, label, description, permission, schema, requiresApproval)
	tool.Effects.MaySpendProvider = true
	tool.Effects.StartsWorkflow = true
	tool.StartsWorkflow = true
	return tool
}

func destructiveTool(name, label, description, permission string, schema json.RawMessage) AgentTool {
	return AgentTool{Name: name, Label: label, Description: description, Risk: ToolRiskDestructive, Permission: permission, InputSchema: schema, RequiresApproval: true, Effects: ToolEffects{WritesProject: true, Destructive: true}}
}

func adminTool(name, label, description, permission string, schema json.RawMessage) AgentTool {
	return AgentTool{Name: name, Label: label, Description: description, Risk: ToolRiskAdmin, Permission: permission, InputSchema: schema, RequiresApproval: true, Effects: ToolEffects{WritesProject: true}}
}

func costedAdminTool(name, label, description, permission string, schema json.RawMessage) AgentTool {
	tool := adminTool(name, label, description, permission, schema)
	tool.Effects.MaySpendProvider = true
	return tool
}

func emptyObjectSchema() json.RawMessage {
	return objectSchema(nil, false)
}

func limitSchema() json.RawMessage {
	return objectSchema(map[string]any{
		"limit": integerSchema("返回数量。", 1, 200),
	}, false)
}

func workflowRunSchema() json.RawMessage {
	return objectSchema(map[string]any{
		"workflowRunId": stringSchema("Workflow run ID。"),
	}, true)
}

func workflowRunPageSchema(includePreview bool) json.RawMessage {
	properties := map[string]any{
		"workflowRunId": stringSchema("Workflow run ID。"),
		"limit":         integerSchema("单页数量。", 1, 200),
		"cursor":        stringSchema("上一页返回的 nextCursor。"),
	}
	if includePreview {
		properties["includePreviewUrl"] = booleanSchema("是否生成镜头媒体短期预览 URL。")
		properties["previewExpiresSeconds"] = integerSchema("预览 URL 有效秒数。", 60, 3600)
	}
	return objectSchemaRequired(properties, "workflowRunId")
}

func objectSchema(properties map[string]any, required bool) json.RawMessage {
	if properties == nil {
		properties = map[string]any{}
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if required {
		requiredKeys := make([]string, 0, len(properties))
		for key := range properties {
			requiredKeys = append(requiredKeys, key)
		}
		schema["required"] = requiredKeys
	}
	return mustJSON(schema)
}

func objectSchemaRequired(properties map[string]any, requiredKeys ...string) json.RawMessage {
	if properties == nil {
		properties = map[string]any{}
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(requiredKeys) > 0 {
		schema["required"] = requiredKeys
	}
	return mustJSON(schema)
}

func freeformObjectSchema(description string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"description":          description,
		"additionalProperties": true,
	}
}

func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func integerSchema(description string, min, max int) map[string]any {
	return map[string]any{"type": "integer", "description": description, "minimum": min, "maximum": max}
}

func booleanSchema(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func enumSchema(description string, values []string) map[string]any {
	return map[string]any{"type": "string", "description": description, "enum": values}
}

func arraySchema(description string, items any) map[string]any {
	return map[string]any{"type": "array", "description": description, "items": items}
}

func mustJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}
