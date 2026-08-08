package agent

import (
	"encoding/json"

	"github.com/Einzieg/cineweave/internal/authz"
)

// extendedProjectControlTools contains explicit user-intent actions that were
// historically available only through page-specific REST handlers. Keeping
// these descriptors beside the embedded Agent registry gives the Agent, MCP
// and generated action matrix one typed contract.
func extendedProjectControlTools() []AgentTool {
	identifier := func(description string) map[string]any { return stringSchema(description) }
	revision := func(description string) map[string]any { return integerSchema(description, 1, 1000000000) }
	review := func(entityID, description string) map[string]any {
		return objectSchemaRequiredMap(map[string]any{
			entityID:       identifier(description),
			"reviewStatus": enumSchema("审核结论。", []string{"pending", "approved", "rejected", "needs_edit"}),
			"note":         stringSchema("审核说明。"),
		}, entityID, "reviewStatus")
	}

	return []AgentTool{
		writeTool("project.update", "修改项目", "使用项目 revision 修改名称、简介或其它允许直接保存的基础信息；生产配置变更必须使用受控换代动作。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"expectedRevision": revision("当前项目 revision。"),
			"name":             stringSchema("项目名称。"),
			"description":      stringSchema("项目简介。"),
		}, "expectedRevision"), true),
		readTool("project.deletion_impact", "项目删除影响", "读取项目删除范围、当前 revision 和确认 hash，不执行删除。", authz.PermissionProjectDelete, emptyObjectSchema()),
		destructiveTool("project.delete", "删除项目", "使用刚读取的项目名、revision 和影响 hash 创建可恢复的项目删除命令。", authz.PermissionProjectDelete, objectSchemaRequired(map[string]any{
			"projectName":             stringSchema("项目当前名称。"),
			"expectedProjectRevision": revision("项目当前 revision。"),
			"impactHash":              stringSchema("project.deletion_impact 返回的影响 hash。"),
		}, "projectName", "expectedProjectRevision", "impactHash")),
		writeTool("project.delete.retry", "重试项目删除", "重试失败但仍可恢复的项目删除请求。", authz.PermissionProjectDelete, objectSchemaRequired(map[string]any{
			"requestId": identifier("项目删除请求 ID。"),
		}, "requestId"), true),
		draftTool("project.production_rebuild_impact", "生产配置换代影响", "计算并暂存目标生产配置会归档的数据和需要重建的分集，不切换当前生产代。", authz.PermissionProjectVideoProductionRebuild, objectSchemaRequired(map[string]any{
			"targetProfileKey":     stringSchema("目标视频生产 Profile key。"),
			"targetProfileVersion": integerSchema("目标 Profile 版本。", 1, 1000000),
			"targetConfiguration":  freeformObjectSchema("完整目标生产配置。"),
		}, "targetProfileKey", "targetConfiguration"), false),
		workflowTool("project.production_rebuild", "确认生产配置换代", "使用影响令牌切换生产配置并按分集重建分镜；不自动生成图片和视频。", authz.PermissionProjectVideoProductionRebuild, objectSchemaRequired(map[string]any{
			"expectedProjectRevision": revision("项目当前 revision。"),
			"targetProfileKey":        stringSchema("目标视频生产 Profile key。"),
			"targetProfileVersion":    integerSchema("目标 Profile 版本。", 1, 1000000),
			"targetConfiguration":     freeformObjectSchema("完整目标生产配置。"),
			"impactToken":             stringSchema("影响分析返回的短期令牌。"),
		}, "expectedProjectRevision", "targetProfileKey", "targetConfiguration", "impactToken")),
		workflowTool("project.production_rebuild.retry_failed", "重试换代失败分集", "只重试生产配置换代中失败的分集。", authz.PermissionProjectVideoProductionRebuild, objectSchemaRequired(map[string]any{
			"rebuildId": identifier("生产换代 ID。"),
		}, "rebuildId")),
		writeTool("source.create", "创建项目内容", "直接创建小说、剧本原文或创意文案；小说可在入库时持久化拆分章节。", authz.PermissionSourceWrite, objectSchemaRequired(map[string]any{
			"sourceType":       enumSchema("内容类型。", []string{"novel", "script", "brief"}),
			"title":            stringSchema("内容标题。"),
			"content":          map[string]any{"type": "string", "maxLength": 49152, "description": "内联正文；更长内容应创建来源后使用 content.write.begin/chunk/commit。"},
			"contentFormat":    enumSchema("正文格式。", []string{"plain_text", "markdown"}),
			"originalFileName": stringSchema("可选原始文件名。"),
			"storageKey":       stringSchema("可选原始文件对象存储键。"),
			"splitChapters":    booleanSchema("小说是否自动分卷分章节。"),
			"createScript":     booleanSchema("剧本原文是否同时初始化项目剧本。"),
			"metadata":         freeformObjectSchema("内容元数据。"),
			"chapters": arraySchema("显式章节集合；提供后按持久化 ordinal 入库。", objectSchemaRequired(map[string]any{
				"chapterIndex": integerSchema("全局章节序号。", 1, 1000000),
				"volumeIndex":  integerSchema("卷序号。", 1, 1000000),
				"sectionIndex": integerSchema("卷内章节序号。", 1, 1000000),
				"volumeTitle":  stringSchema("卷标题。"),
				"chapterTitle": stringSchema("章节标题。"),
				"content":      stringSchema("章节正文。"),
			}, "content")),
		}, "sourceType", "title", "content"), true),
		writeTool("novel_event.update", "修改小说事件", "使用事件 revision 修改明确小说事件的结构化字段。", authz.PermissionNovelEventWrite, objectSchemaRequired(map[string]any{
			"eventId":          identifier("小说事件 ID。"),
			"expectedRevision": revision("事件读取结果中的当前 revision。"),
			"patch": objectSchemaRequiredMap(map[string]any{
				"title": stringSchema("事件标题。"), "summary": stringSchema("事件摘要。"),
				"eventType": stringSchema("事件类型。"), "importance": integerSchema("重要性。", 1, 5),
				"timelineHint": stringSchema("时间线提示。"), "locationHint": stringSchema("地点提示。"),
				"emotionalTone": stringSchema("情绪基调。"), "conflict": stringSchema("冲突。"),
				"outcome": stringSchema("结果。"), "adaptationHint": stringSchema("改编提示。"),
				"characters":   arraySchema("相关角色。", stringSchema("角色名。")),
				"scenes":       arraySchema("相关场景。", stringSchema("场景名。")),
				"props":        arraySchema("相关道具。", stringSchema("道具名。")),
				"keywords":     arraySchema("关键词。", stringSchema("关键词。")),
				"rawExcerpt":   stringSchema("原文摘录。"),
				"reviewStatus": enumSchema("审核状态。", []string{"pending", "approved", "rejected", "needs_edit"}),
			}),
		}, "eventId", "expectedRevision", "patch"), true),
		writeTool("novel_event.review", "审核小说事件", "使用事件 revision 审核明确的小说事件。", authz.PermissionNovelEventWrite, objectSchemaRequired(map[string]any{
			"eventId": identifier("小说事件 ID。"), "expectedRevision": revision("事件读取结果中的当前 revision。"),
			"reviewStatus": enumSchema("审核结论。", []string{"pending", "approved", "rejected", "needs_edit"}),
			"note":         stringSchema("审核说明。"),
		}, "eventId", "expectedRevision", "reviewStatus"), true),
		writeTool("adaptation.create", "创建改编计划", "创建新的改编计划草稿。", authz.PermissionAdaptationPlanWrite, objectSchemaRequired(map[string]any{
			"sourceId": identifier("关联原文 ID。"), "title": stringSchema("计划名称。"),
			"status":                enumSchema("计划状态。", []string{"draft", "active", "archived"}),
			"targetFormat":          stringSchema("目标格式。"),
			"targetDurationSeconds": integerSchema("目标时长秒数。", 0, 86400),
			"maxShots":              integerSchema("最大镜头数。", 0, 100000),
			"selectedEventIds":      arraySchema("选中的小说事件 ID。", identifier("小说事件 ID。")),
			"structure":             freeformObjectSchema("改编结构。"), "content": stringSchema("改编计划正文。"),
		}, "title"), true),
		writeTool("adaptation.update", "修改改编计划", "使用计划 revision 修改明确的改编计划。", authz.PermissionAdaptationPlanWrite, objectSchemaRequired(map[string]any{
			"planId": identifier("改编计划 ID。"), "expectedRevision": revision("计划读取结果中的当前 revision。"),
			"patch": objectSchemaRequiredMap(map[string]any{
				"title": stringSchema("计划名称。"), "status": enumSchema("计划状态。", []string{"draft", "active", "archived"}),
				"targetFormat":          stringSchema("目标格式。"),
				"targetDurationSeconds": integerSchema("目标时长秒数。", 0, 86400),
				"maxShots":              integerSchema("最大镜头数。", 0, 100000),
				"selectedEventIds":      arraySchema("选中的小说事件 ID。", identifier("小说事件 ID。")),
				"structure":             freeformObjectSchema("改编结构。"), "content": stringSchema("改编计划正文。"),
				"reviewStatus": enumSchema("审核状态。", []string{"pending", "approved", "rejected", "needs_edit"}),
			}),
		}, "planId", "expectedRevision", "patch"), true),
		writeTool("adaptation.review", "审核改编计划", "使用计划 revision 审核明确的改编计划。", authz.PermissionAdaptationPlanWrite, objectSchemaRequired(map[string]any{
			"planId": identifier("改编计划 ID。"), "expectedRevision": revision("计划读取结果中的当前 revision。"),
			"reviewStatus": enumSchema("审核结论。", []string{"pending", "approved", "rejected", "needs_edit"}),
			"note":         stringSchema("审核说明。"),
		}, "planId", "expectedRevision", "reviewStatus"), true),
		writeTool("adaptation.activate", "激活改编计划", "使用计划 revision 把明确的改编计划设为当前激活计划。", authz.PermissionAdaptationPlanWrite, objectSchemaRequired(map[string]any{
			"planId": identifier("改编计划 ID。"), "expectedRevision": revision("计划读取结果中的当前 revision。"),
		}, "planId", "expectedRevision"), true),
		costedWriteTool("adaptation.generate_script", "从改编计划生成剧本", "从明确改编计划生成剧本并立即写入结果。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"planId":      identifier("改编计划 ID。"),
			"title":       stringSchema("新剧本标题。"),
			"instruction": stringSchema("额外生成要求。"),
		}, "planId"), true),
		writeTool("script.create", "创建剧本", "创建项目剧本及初始版本。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"sourceId":      identifier("关联来源 ID。"),
			"title":         stringSchema("剧本标题。"),
			"content":       map[string]any{"type": "string", "maxLength": 49152, "description": "初始剧本正文；超长正文应按分集创建或使用长内容协议。"},
			"contentFormat": enumSchema("正文格式。", []string{"plain_text", "markdown"}),
			"sourceType":    stringSchema("版本来源类型。"),
			"metadata":      freeformObjectSchema("剧本元数据。"),
		}, "title"), true),
		writeTool("script.update", "修改剧本信息", "修改剧本标题、状态或来源关联；正文修改使用剧本版本或分集动作。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptId":         identifier("剧本 ID。"),
			"expectedRevision": integerSchema("script.get 或 script.list 返回的当前剧本 revision。", 1, 1000000000),
			"patch":            freeformObjectSchema("剧本字段补丁。"),
		}, "scriptId", "expectedRevision", "patch"), true),
		writeTool("script_scene.update", "修改剧本场景", "修改明确剧本场景的结构化内容。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"sceneId":          identifier("剧本场景 ID。"),
			"expectedRevision": integerSchema("场景读取结果中的当前 revision。", 1, 1000000000),
			"patch":            freeformObjectSchema("场景字段补丁。"),
		}, "sceneId", "expectedRevision", "patch"), true),
		destructiveTool("script_scene.delete", "删除剧本场景", "软删除明确剧本场景并标记下游需要重生成。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"sceneId":          identifier("剧本场景 ID。"),
			"expectedRevision": integerSchema("场景读取结果中的当前 revision。", 1, 1000000000),
			"reason":           stringSchema("归档原因。"),
		}, "sceneId", "expectedRevision")),
		writeTool("script_scene.review", "审核剧本场景", "审核明确的剧本场景。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"sceneId":          identifier("剧本场景 ID。"),
			"expectedRevision": integerSchema("场景读取结果中的当前 revision。", 1, 1000000000),
			"reviewStatus":     enumSchema("审核状态。", []string{"pending", "approved", "rejected", "needs_edit"}),
			"note":             stringSchema("审核说明。"),
		}, "sceneId", "expectedRevision", "reviewStatus"), true),

		writeTool("asset.reference.create", "添加资产参考图", "把已入库媒体作为核心资产参考图并可设为主图。", authz.PermissionAssetWrite, objectSchemaRequired(map[string]any{
			"assetId":          identifier("核心资产 ID。"),
			"expectedRevision": integerSchema("asset.get 或 asset.list 返回的当前资产 revision。", 1, 2147483647),
			"title":            stringSchema("参考图标题。"),
			"description":      stringSchema("参考图说明。"),
			"storageKey":       stringSchema("已入库对象存储 key。"),
			"mimeType":         stringSchema("媒体 MIME type。"),
			"referenceType":    enumSchema("参考类型。", []string{"generated", "uploaded", "derived", "selected"}),
			"setPrimary":       booleanSchema("是否设为主参考图。"),
			"metadata":         freeformObjectSchema("参考图元数据。"),
		}, "assetId", "expectedRevision", "storageKey", "mimeType"), true),
		destructiveTool("asset.reference.delete", "移除资产参考图", "解除资产参考图关联，不伪造对象存储物理删除。", authz.PermissionAssetWrite, objectSchemaRequired(map[string]any{
			"assetId":          identifier("核心资产 ID。"),
			"referenceId":      identifier("资产参考图 ID。"),
			"expectedRevision": integerSchema("asset.get 或 asset.list 返回的当前资产 revision。", 1, 2147483647),
			"reason":           stringSchema("解除关联原因。"),
		}, "assetId", "referenceId", "expectedRevision")),
		writeTool("asset.reference.set_primary", "设为资产主图", "把明确的历史参考图设为当前资产主图。", authz.PermissionAssetWrite, objectSchemaRequired(map[string]any{
			"assetId":          identifier("核心资产 ID。"),
			"referenceId":      identifier("资产参考图 ID。"),
			"expectedRevision": integerSchema("asset.get 或 asset.list 返回的当前资产 revision。", 1, 2147483647),
		}, "assetId", "referenceId", "expectedRevision"), true),
		childWorkflowTool("shot_asset.generate_derived_image", "生成镜头衍生资产图", "为明确的镜头资产需求生成衍生图并保留完整提示词与供应商溯源。", authz.PermissionAssetGenerate, objectSchemaRequired(map[string]any{
			"requirementId": identifier("镜头资产需求 ID。"),
		}, "requirementId")),

		writeTool("storyboard.activate_plan", "激活分镜方案", "激活明确分镜方案并使下游状态按统一规则更新。", authz.PermissionStoryboardGenerate, objectSchemaRequired(map[string]any{
			"planId": identifier("分镜方案 ID。"),
		}, "planId"), true),
		writeTool("storyboard.create_shot", "新增分镜镜头", "在当前生产代新增一个明确镜头。", authz.PermissionStoryboardGenerate, objectSchemaRequired(map[string]any{
			"shot": freeformObjectSchema("CreateStoryboardShotRequest 字段。"),
		}, "shot"), true),
		destructiveTool("storyboard.delete_shot", "删除分镜镜头", "删除明确镜头并标记相关媒体和成片需要重生成。", authz.PermissionStoryboardGenerate, objectSchemaRequired(map[string]any{
			"shotId": identifier("分镜镜头 ID。"),
		}, "shotId")),
		writeTool("storyboard.merge_shots", "合并分镜镜头", "按给定顺序合并相邻镜头。", authz.PermissionStoryboardGenerate, objectSchemaRequired(map[string]any{
			"shotIds": arraySchema("要合并的镜头 ID。", stringSchema("镜头 ID。")),
			"patch":   freeformObjectSchema("合并后镜头字段。"),
		}, "shotIds"), true),
		writeTool("storyboard.split_shot", "拆分分镜镜头", "在明确时间点拆分镜头。", authz.PermissionStoryboardGenerate, objectSchemaRequired(map[string]any{
			"shotId":     identifier("镜头 ID。"),
			"splitTick":  integerSchema("拆分 tick。", 0, 1000000000),
			"splitFrame": integerSchema("拆分帧。", 0, 1000000000),
			"rightTitle": stringSchema("右侧新镜头标题。"),
		}, "shotId"), true),
		childWorkflowTool("storyboard.replan_shot_state", "重算镜头状态", "依据当前剧本、资产和生产配置重新规划镜头结构化状态。", authz.PermissionStoryboardGenerate, objectSchemaRequired(map[string]any{
			"shotId": identifier("镜头 ID。"),
		}, "shotId")),
		childWorkflowTool("storyboard.generate_anchor", "生成镜头锚点图", "生成镜头首帧、尾帧或分镜版锚点。", authz.PermissionStoryboardGenerate, objectSchemaRequired(map[string]any{
			"shotId":     identifier("镜头 ID。"),
			"anchorRole": enumSchema("锚点类型。", []string{"planned_first_frame", "planned_last_frame", "storyboard_sheet", "storyboard_panel"}),
		}, "shotId")),
		writeTool("storyboard.approve_anchor", "批准镜头锚点", "批准指定 revision 的镜头锚点。", authz.PermissionStoryboardGenerate, reviewRevisionSchema("shotId", "anchorId"), true),
		writeTool("storyboard.reject_anchor", "拒绝镜头锚点", "拒绝指定 revision 的镜头锚点并保留原因。", authz.PermissionStoryboardGenerate, reviewRevisionSchema("shotId", "anchorId"), true),
		destructiveTool("storyboard.unlink_media", "解除镜头媒体", "解除镜头图片或视频绑定，不物理删除媒体文件。", authz.PermissionStoryboardGenerate, objectSchemaRequired(map[string]any{
			"shotId": identifier("镜头 ID。"),
			"kind":   enumSchema("媒体类型。", []string{"image", "video"}),
		}, "shotId", "kind")),
		writeTool("storyboard.review_shot", "审核镜头", "审核明确镜头。", authz.PermissionStoryboardGenerate, mustJSON(review("shotId", "镜头 ID。")), true),
		writeTool("shot.video_prompt.create_revision", "编辑视频提示词", "创建人工视频提示词 revision，不覆盖历史版本。", authz.PermissionStoryboardGenerate, objectSchemaRequired(map[string]any{
			"shotId":           identifier("镜头 ID。"),
			"expectedRevision": revision("当前提示词 revision。"),
			"renderedPrompt":   stringSchema("完整视频提示词。"),
			"reason":           stringSchema("修改原因。"),
		}, "shotId", "expectedRevision", "renderedPrompt"), true),
		writeTool("shot.video_prompt.approve", "批准视频提示词", "批准指定视频提示词 revision。", authz.PermissionStoryboardGenerate, reviewRevisionSchema("promptPlanId"), true),
		writeTool("shot.video_prompt.reject", "拒绝视频提示词", "拒绝指定视频提示词 revision。", authz.PermissionStoryboardGenerate, reviewRevisionSchema("promptPlanId"), true),
		writeTool("shot.render_plan.create", "创建视频执行计划", "按当前模型、比例、分辨率和音频策略创建镜头 Render Plan。", authz.PermissionWorkflowRun, objectSchemaRequired(map[string]any{
			"shotId":  identifier("镜头 ID。"),
			"request": freeformObjectSchema("CreateVideoRenderPlanRequest 字段。"),
		}, "shotId", "request"), true),
		writeTool("shot.render_plan.verify_audio", "核验镜头音频", "确定性批准或拒绝 Render Plan 音频契约。", authz.PermissionWorkflowRun, objectSchemaRequired(map[string]any{
			"shotId":   identifier("镜头 ID。"),
			"decision": enumSchema("核验结论。", []string{"approve", "reject"}),
			"notes":    stringSchema("核验说明。"),
		}, "shotId", "decision"), true),
		childWorkflowTool("shot.render_plan.review_audio", "运行原生音频审阅", "对镜头 Render Plan 启动原生音频审阅。", authz.PermissionWorkflowRun, objectSchemaRequired(map[string]any{
			"shotId":            identifier("镜头 ID。"),
			"videoRenderPlanId": identifier("Render Plan ID。"),
			"maxConcurrency":    integerSchema("最大并发。", 1, 16),
		}, "shotId", "videoRenderPlanId")),

		writeTool("timeline.create", "创建时间线", "创建项目时间线，可从当前分镜初始化片段。", authz.PermissionProjectWrite, objectSchema(map[string]any{
			"title":               stringSchema("时间线标题。"),
			"aspectRatio":         stringSchema("画面比例。"),
			"resolution":          stringSchema("输出分辨率。"),
			"fromStoryboardShots": booleanSchema("是否从当前分镜的已生成视频初始化片段。"),
		}, false), true),
		writeTool("timeline.update", "修改时间线", "修改时间线标题、状态、比例或分辨率。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"timelineId":       identifier("时间线 ID。"),
			"expectedRevision": integerSchema("当前时间线 revision。", 1, 2147483647),
			"patch": objectSchemaRequiredMap(map[string]any{
				"title":       stringSchema("时间线标题。"),
				"status":      enumSchema("时间线状态。", []string{"draft", "active", "archived"}),
				"aspectRatio": stringSchema("画面比例。"),
				"resolution":  stringSchema("输出分辨率。"),
			}),
		}, "timelineId", "expectedRevision", "patch"), true),
		destructiveTool("timeline.delete", "删除时间线", "删除明确时间线及其 clip 关联。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"timelineId":       identifier("时间线 ID。"),
			"expectedRevision": integerSchema("当前时间线 revision。", 1, 2147483647),
		}, "timelineId", "expectedRevision")),
		writeTool("timeline.clip.create", "新增时间线片段", "向明确时间线添加片段。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"timelineId":               identifier("时间线 ID。"),
			"expectedTimelineRevision": integerSchema("当前时间线 revision。", 1, 2147483647),
			"storyboardShotId":         identifier("关联分镜 ID。"),
			"videoArtifactId":          identifier("视频产物 ID。"),
			"videoMediaFileId":         identifier("视频媒体 ID。"),
			"clipIndex":                integerSchema("目标片段序号。", 0, 1000000),
			"title":                    stringSchema("片段标题。"),
			"enabled":                  booleanSchema("是否启用。"),
			"sourceStorageKey":         stringSchema("源媒体存储键。"),
			"sourceDurationTicks":      integerSchema("源媒体时长 tick。", 1, 2147483647),
			"trimStartTick":            integerSchema("裁切开始 tick。", 0, 2147483647),
			"trimEndTick":              integerSchema("裁切结束 tick。", 1, 2147483647),
			"durationTicks":            integerSchema("片段时长 tick。", 1, 2147483647),
			"notes":                    stringSchema("片段备注。"),
		}, "timelineId", "expectedTimelineRevision"), true),
		destructiveTool("timeline.clip.delete", "删除时间线片段", "从时间线删除明确片段。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"timelineId":               identifier("时间线 ID。"),
			"clipId":                   identifier("片段 ID。"),
			"expectedTimelineRevision": integerSchema("当前时间线 revision。", 1, 2147483647),
			"expectedRevision":         integerSchema("当前片段 revision。", 1, 2147483647),
		}, "timelineId", "clipId", "expectedTimelineRevision", "expectedRevision")),
		writeTool("timeline.clip.reorder", "重排时间线片段", "使用完整排序原子重排时间线 clips。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"timelineId":               identifier("时间线 ID。"),
			"expectedTimelineRevision": integerSchema("当前时间线 revision。", 1, 2147483647),
			"items": arraySchema("完整排序条目。", objectSchemaRequiredMap(map[string]any{
				"clipId":           identifier("片段 ID。"),
				"expectedRevision": integerSchema("当前片段 revision。", 1, 2147483647),
				"clipIndex":        integerSchema("目标片段序号。", 0, 1000000),
			}, "clipId", "expectedRevision", "clipIndex")),
		}, "timelineId", "expectedTimelineRevision", "items"), true),
		destructiveTool("final_video.delete", "删除成片版本", "删除明确成片版本并保留审计。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"versionId":        identifier("成片版本 ID。"),
			"expectedRevision": integerSchema("当前成片版本 revision。", 1, 2147483647),
			"confirmActive":    booleanSchema("删除当前激活成片时必须明确确认。"),
		}, "versionId", "expectedRevision")),
		readTool("final_video.download_url", "成片下载链接", "创建短期成片下载 URL。", authz.PermissionArtifactRead, objectSchemaRequired(map[string]any{
			"versionId":      identifier("成片版本 ID。"),
			"expiresSeconds": integerSchema("链接有效秒数。", 60, 3600),
		}, "versionId")),
		workflowTool("export.create", "创建项目导出", "创建成片、文档、资产包或项目归档导出任务。", authz.PermissionArtifactRead, objectSchemaRequired(map[string]any{
			"exportType": enumSchema("导出类型。", []string{"final_video", "documents", "asset_package", "project_archive"}),
			"format":     enumSchema("导出格式。", []string{"mp4", "json", "markdown", "zip"}),
			"title":      stringSchema("导出标题。"),
			"options":    freeformObjectSchema("导出选项。"),
		}, "exportType")),
		readTool("export.download_url", "导出下载链接", "创建短期项目导出下载 URL。", authz.PermissionArtifactRead, objectSchemaRequired(map[string]any{
			"exportId":       identifier("导出 ID。"),
			"expiresSeconds": integerSchema("链接有效秒数。", 60, 3600),
		}, "exportId")),
		writeTool("character_voice.create", "创建角色声音", "创建角色声音 Profile。", authz.PermissionProjectWrite, objectSchemaRequired(characterVoiceToolProperties(),
			"characterName", "displayName", "voiceKey",
		), true),
		writeTool("character_voice.update", "修改角色声音", "修改明确角色声音 Profile。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"voiceId":          identifier("角色声音 ID。"),
			"expectedRevision": integerSchema("当前角色声音 revision。", 1, 1000000000),
			"patch":            objectSchemaRequiredMap(characterVoiceToolProperties()),
		}, "voiceId", "expectedRevision", "patch"), true),
		destructiveTool("character_voice.delete", "删除角色声音", "删除明确角色声音 Profile。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"voiceId":          identifier("角色声音 ID。"),
			"expectedRevision": integerSchema("当前角色声音 revision。", 1, 1000000000),
		}, "voiceId", "expectedRevision")),
	}
}

func objectSchemaRequiredMap(properties map[string]any, requiredKeys ...string) map[string]any {
	var schema map[string]any
	_ = json.Unmarshal(objectSchemaRequired(properties, requiredKeys...), &schema)
	return schema
}

func freeformObjectObjectSchema(description string) []byte {
	return mustJSON(map[string]any{
		"type":                 "object",
		"description":          description,
		"additionalProperties": true,
	})
}

func characterVoiceToolProperties() map[string]any {
	return map[string]any{
		"canonicalAssetId":     stringSchema("关联的角色资产 ID；传空字符串可解除关联。"),
		"characterName":        stringSchema("角色名称。"),
		"displayName":          stringSchema("声音显示名称。"),
		"language":             stringSchema("BCP 47 语言标记，例如 zh-CN。"),
		"modelProfileKey":      stringSchema("TTS 业务模型 Profile key。"),
		"providerModelId":      stringSchema("可选的固定 TTS Provider Model ID；传空字符串可解除固定模型。"),
		"voiceKey":             stringSchema("上游声音标识。"),
		"instructions":         stringSchema("声音生成指令；传空字符串可清空。"),
		"referenceArtifactId":  stringSchema("可选的当前项目参考音频产物 ID；传空字符串可清空。"),
		"referenceMediaFileId": stringSchema("可选的当前项目参考媒体 ID；传空字符串可清空。"),
		"parameters":           freeformObjectSchema("结构化 TTS 参数。"),
		"isDefault":            booleanSchema("是否作为默认旁白声音。"),
	}
}

func reviewRevisionSchema(idKeys ...string) []byte {
	properties := map[string]any{
		"expectedRevision": integerSchema("当前 revision。", 1, 1000000000),
		"reason":           stringSchema("审核原因。"),
	}
	required := append([]string(nil), idKeys...)
	for _, key := range idKeys {
		properties[key] = stringSchema("目标 ID。")
	}
	required = append(required, "expectedRevision")
	return objectSchemaRequired(properties, required...)
}
