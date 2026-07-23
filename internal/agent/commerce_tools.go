package agent

import "github.com/Einzieg/cineweave/internal/authz"

func commerceDefaultTools() []AgentTool {
	scriptUnitID := func() map[string]any {
		return stringSchema("带货脚本单元 ID；无法唯一确定时必须先询问用户。")
	}
	productionSelection := func() map[string]any {
		return map[string]any{
			"scriptUnitId":             scriptUnitID(),
			"planId":                   stringSchema("当前启用的分镜方案 ID。"),
			"expectedPlanRevision":     integerSchema("当前分镜方案 revision。", 1, 1000000000),
			"expectedUnitGenerationId": stringSchema("当前脚本单元生产代 ID。"),
			"shotIds":                  arraySchema("可选镜头 ID；为空时由后端选择该阶段的全部可执行镜头。", stringSchema("镜头 ID。")),
			"force":                    booleanSchema("是否重新生成已经完成的目标。"),
			"concurrency":              integerSchema("单元内最大并发数。", 1, 16),
		}
	}
	return []AgentTool{
		readTool("commerce.product.get", "读取商品资料", "读取当前商品、活动版本和 revision。", authz.PermissionProjectRead, emptyObjectSchema()),
		readTool("commerce.product.version.list", "商品版本列表", "列出商品资料的历史版本。", authz.PermissionProjectRead, emptyObjectSchema()),
		writeTool("commerce.product.version.create", "创建商品版本", "基于当前商品 revision 创建新的结构化商品版本。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"expectedRevision":  integerSchema("当前商品 revision。", 1, 1000000000),
			"name":              stringSchema("商品名称。"),
			"brand":             stringSchema("品牌。"),
			"sellingPoints":     arraySchema("核心卖点。", stringSchema("卖点。")),
			"immutableFeatures": arraySchema("生成时不可篡改的商品事实。", stringSchema("商品事实。")),
			"prohibitedClaims":  arraySchema("禁用宣传表述。", stringSchema("禁用表述。")),
			"metadata":          freeformObjectSchema("商品版本扩展信息。"),
		}, "expectedRevision", "name", "sellingPoints"), true),
		draftTool("commerce.product.rebuild_impact", "检查商品换版影响", "冻结目标商品版本与参考图集合，并返回受影响脚本和确认令牌。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"targetProductVersionId":  stringSchema("目标商品版本 ID。"),
			"targetReferenceIds":      arraySchema("目标活动商品参考图 ID。", stringSchema("商品参考图 ID。")),
			"expectedProductRevision": integerSchema("当前商品 revision。", 1, 1000000000),
		}, "targetProductVersionId", "targetReferenceIds", "expectedProductRevision"), false),
		destructiveTool("commerce.product.rebuild", "执行商品换版", "按已确认的影响令牌切换商品版本并归档旧脚本生产代。", authz.PermissionWorkflowRun, objectSchemaRequired(map[string]any{
			"impactToken":             stringSchema("商品换版影响确认令牌。"),
			"expectedProductRevision": integerSchema("影响检查时的商品 revision。", 1, 1000000000),
		}, "impactToken", "expectedProductRevision")),

		readTool("commerce.product.reference.list", "商品参考图列表", "列出商品参考图、主图和 revision。", authz.PermissionAssetRead, objectSchema(map[string]any{
			"status": enumSchema("状态筛选。", []string{"active", "archived", "all"}),
		}, false)),
		writeTool("commerce.product.reference.add", "登记商品参考图", "完成一个已经上传到对象存储的商品图片凭据；文件上传本身由用户界面完成。", authz.PermissionAssetWrite, objectSchemaRequired(map[string]any{
			"uploadId":      stringSchema("已完成 PUT 上传的 upload ID。"),
			"referenceRole": enumSchema("参考图用途。", []string{"primary", "front", "back", "side", "detail", "usage", "packaging", "other"}),
			"setPrimary":    booleanSchema("是否设为主图。"),
		}, "uploadId", "referenceRole"), true),
		destructiveTool("commerce.product.reference.archive", "归档商品参考图", "按当前 revision 归档商品参考图，保留历史生产溯源。", authz.PermissionAssetDelete, objectSchemaRequired(map[string]any{
			"referenceId":      stringSchema("商品参考图 ID。"),
			"expectedRevision": integerSchema("当前参考图 revision。", 1, 1000000000),
		}, "referenceId", "expectedRevision")),
		writeTool("commerce.product.reference.set_primary", "设置商品主图", "按当前 revision 将指定活动参考图设为主图。", authz.PermissionAssetWrite, objectSchemaRequired(map[string]any{
			"referenceId":      stringSchema("商品参考图 ID。"),
			"expectedRevision": integerSchema("当前参考图 revision。", 1, 1000000000),
		}, "referenceId", "expectedRevision"), true),

		readTool("commerce.script_unit.list", "带货脚本列表", "列出带货脚本单元及生产摘要。", authz.PermissionScriptRead, objectSchema(map[string]any{
			"status": enumSchema("状态筛选。", []string{"active", "archived", "all"}),
			"cursor": stringSchema("分页游标。"),
			"limit":  integerSchema("返回数量。", 1, 200),
		}, false)),
		readTool("commerce.script_unit.get", "读取带货脚本", "读取一个明确的带货脚本单元；不能根据最近创建记录自动选择。", authz.PermissionScriptRead, objectSchemaRequired(map[string]any{
			"scriptUnitId": scriptUnitID(),
		}, "scriptUnitId")),
		writeTool("commerce.script_unit.create", "创建带货脚本", "以项目脚本集合 revision 创建一个独立脚本单元。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"expectedScriptUnitsRevision": integerSchema("当前脚本集合 revision。", 1, 1000000000),
			"title":                       stringSchema("脚本标题。"),
			"content":                     stringSchema("用户广告脚本正文。"),
			"languageMode":                enumSchema("语言模式。", []string{"auto", "explicit"}),
			"explicitTargetLanguage":      stringSchema("明确指定的目标语言 BCP-47 标记。"),
			"targetDurationSeconds":       integerSchema("目标成片秒数。", 1, 3600),
			"targetPlatform":              stringSchema("目标发布平台。"),
			"sourceLanguageHint":          stringSchema("可选源语言提示。"),
		}, "expectedScriptUnitsRevision", "title", "content", "languageMode", "targetDurationSeconds", "targetPlatform"), true),
		writeTool("commerce.script_unit.duplicate", "复制带货脚本", "复制指定脚本为新的独立脚本单元。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptUnitId":                scriptUnitID(),
			"expectedScriptUnitsRevision": integerSchema("当前脚本集合 revision。", 1, 1000000000),
		}, "scriptUnitId", "expectedScriptUnitsRevision"), true),
		writeTool("commerce.script_unit.create_language_variant", "创建语言版本脚本", "复制指定脚本并创建目标语言变体。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptUnitId":                scriptUnitID(),
			"expectedScriptUnitsRevision": integerSchema("当前脚本集合 revision。", 1, 1000000000),
			"targetLanguage":              stringSchema("目标语言 BCP-47 标记。"),
		}, "scriptUnitId", "expectedScriptUnitsRevision", "targetLanguage"), true),
		writeTool("commerce.script_unit.reorder", "重排带货脚本", "按脚本集合 revision 更新脚本顺序。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"expectedScriptUnitsRevision": integerSchema("当前脚本集合 revision。", 1, 1000000000),
			"items": arraySchema("完整的新顺序。", objectSchemaRequired(map[string]any{
				"scriptUnitId": scriptUnitID(),
				"sortOrder":    integerSchema("排序值。", 0, 1000000000),
			}, "scriptUnitId", "sortOrder")),
		}, "expectedScriptUnitsRevision", "items"), true),
		destructiveTool("commerce.script_unit.archive", "归档带货脚本", "按当前 revision 归档一个脚本单元及其活动生产入口。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptUnitId":     scriptUnitID(),
			"expectedRevision": integerSchema("当前脚本单元 revision。", 1, 1000000000),
		}, "scriptUnitId", "expectedRevision")),

		readTool("commerce.script_unit.version.list", "脚本版本列表", "列出指定带货脚本的版本。", authz.PermissionScriptRead, objectSchemaRequired(map[string]any{
			"scriptUnitId": scriptUnitID(),
		}, "scriptUnitId")),
		writeTool("commerce.script_unit.version.create", "创建脚本版本", "按脚本单元 revision 创建新脚本版本，可显式激活。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptUnitId":       scriptUnitID(),
			"expectedRevision":   integerSchema("当前脚本单元 revision。", 1, 1000000000),
			"content":            stringSchema("完整广告脚本正文。"),
			"sourceLanguageHint": stringSchema("可选源语言提示。"),
			"activate":           booleanSchema("是否立即激活。"),
		}, "scriptUnitId", "expectedRevision", "content"), true),
		writeTool("commerce.script_unit.version.activate", "激活脚本版本", "按脚本单元 revision 激活指定脚本版本并使下游旧代失效。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptUnitId":     scriptUnitID(),
			"versionId":        stringSchema("脚本版本 ID。"),
			"expectedRevision": integerSchema("当前脚本单元 revision。", 1, 1000000000),
		}, "scriptUnitId", "versionId", "expectedRevision"), true),

		readTool("commerce.script_unit.language.get", "读取脚本语言", "读取语言解析、目标语言和确认状态。", authz.PermissionScriptRead, objectSchemaRequired(map[string]any{
			"scriptUnitId": scriptUnitID(),
		}, "scriptUnitId")),
		writeTool("commerce.script_unit.language.set", "设置脚本语言", "按脚本单元 revision 设置自动判断或明确目标语言。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptUnitId":           scriptUnitID(),
			"expectedRevision":       integerSchema("当前脚本单元 revision。", 1, 1000000000),
			"languageMode":           enumSchema("语言模式。", []string{"auto", "explicit"}),
			"explicitTargetLanguage": stringSchema("明确目标语言 BCP-47 标记。"),
		}, "scriptUnitId", "expectedRevision", "languageMode"), true),
		writeTool("commerce.script_unit.language.confirm", "确认脚本语言", "确认 Agent 检测出的语言及最终目标语言。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptUnitId":         scriptUnitID(),
			"languageResolutionId": stringSchema("语言解析记录 ID。"),
			"targetLanguage":       stringSchema("确认的目标语言 BCP-47 标记。"),
		}, "scriptUnitId", "languageResolutionId", "targetLanguage"), true),

		readTool("commerce.script_unit.localization.list", "本地化版本列表", "列出指定脚本单元的多语言本地化版本。", authz.PermissionScriptRead, objectSchemaRequired(map[string]any{
			"scriptUnitId": scriptUnitID(),
		}, "scriptUnitId")),
		writeTool("commerce.script_unit.localization.create", "创建本地化版本", "创建保留商品事实和 CTA 语义的结构化本地化脚本。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptUnitId":          scriptUnitID(),
			"sourceScriptVersionId": stringSchema("源脚本版本 ID。"),
			"languageResolutionId":  stringSchema("已确认语言解析 ID。"),
			"sourceLanguage":        stringSchema("源语言。"),
			"targetLanguage":        stringSchema("目标语言。"),
			"localizedContent":      stringSchema("完整本地化脚本。"),
			"structuredContract":    freeformObjectSchema("本地化结构契约。"),
			"reviewerOutput":        freeformObjectSchema("语言审核输出。"),
			"approve":               booleanSchema("是否通过审核。"),
		}, "scriptUnitId", "sourceScriptVersionId", "languageResolutionId", "sourceLanguage", "targetLanguage", "localizedContent", "structuredContract", "reviewerOutput"), true),
		writeTool("commerce.script_unit.localization.activate", "激活本地化版本", "按本地化 revision 激活指定版本。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptUnitId":     scriptUnitID(),
			"localizationId":   stringSchema("本地化版本 ID。"),
			"expectedRevision": integerSchema("当前本地化 revision。", 1, 1000000000),
		}, "scriptUnitId", "localizationId", "expectedRevision"), true),

		childWorkflowTool("commerce.script_unit.storyboard.generate", "生成带货分镜", "根据冻结的商品、脚本和语言上下文生成独立分镜方案。", authz.PermissionStoryboardGenerate, objectSchemaRequired(map[string]any{
			"scriptUnitId":             scriptUnitID(),
			"expectedUnitGenerationId": stringSchema("当前脚本单元生产代 ID。"),
		}, "scriptUnitId", "expectedUnitGenerationId")),
		readTool("commerce.script_unit.storyboard.list", "带货分镜列表", "列出脚本单元的分镜方案；传 planId 时返回完整镜头。", authz.PermissionScriptRead, objectSchemaRequired(map[string]any{
			"scriptUnitId": scriptUnitID(),
			"planId":       stringSchema("可选分镜方案 ID。"),
			"status":       enumSchema("方案状态筛选。", []string{"active", "ready", "stale", "all"}),
		}, "scriptUnitId")),
		writeTool("commerce.script_unit.storyboard.update_shot", "修改带货镜头", "按分镜和镜头 revision 修改动作、构图、旁白、字幕、时长及商品参考图。", authz.PermissionStoryboardGenerate, objectSchemaRequired(map[string]any{
			"scriptUnitId":         scriptUnitID(),
			"shotId":               stringSchema("镜头 ID。"),
			"expectedPlanRevision": integerSchema("当前分镜方案 revision。", 1, 1000000000),
			"expectedShotRevision": integerSchema("当前镜头 revision。", 1, 1000000000),
			"visualAction":         stringSchema("画面动作。"),
			"shotPurpose":          stringSchema("镜头目的。"),
			"composition":          stringSchema("构图。"),
			"camera":               freeformObjectSchema("结构化机位与运动。"),
			"voiceoverText":        stringSchema("旁白或角色口播。"),
			"onscreenText":         stringSchema("画面文字。"),
			"durationSeconds":      integerSchema("镜头秒数。", 1, 300),
			"productReferenceIds":  arraySchema("镜头使用的商品参考图 ID。", stringSchema("商品参考图 ID。")),
		}, "scriptUnitId", "shotId", "expectedPlanRevision", "expectedShotRevision"), true),
		writeTool("commerce.script_unit.storyboard.reorder", "重排带货镜头", "按分镜 revision 更新镜头顺序和整数秒时长。", authz.PermissionStoryboardGenerate, objectSchemaRequired(map[string]any{
			"scriptUnitId":         scriptUnitID(),
			"planId":               stringSchema("分镜方案 ID。"),
			"expectedPlanRevision": integerSchema("当前分镜方案 revision。", 1, 1000000000),
			"items": arraySchema("完整镜头顺序。", objectSchemaRequired(map[string]any{
				"shotId":          stringSchema("镜头 ID。"),
				"durationSeconds": integerSchema("镜头秒数。", 1, 300),
			}, "shotId", "durationSeconds")),
		}, "scriptUnitId", "planId", "expectedPlanRevision", "items"), true),

		childWorkflowTool("commerce.script_unit.reference_images.generate", "生成带货参考图", "并发生成选定镜头的参考图提示词或商品一致性参考图。", authz.PermissionStoryboardGenerate, objectSchemaRequired(withCommerceReferenceOperation(productionSelection()), "scriptUnitId", "operation", "planId", "expectedPlanRevision", "expectedUnitGenerationId", "shotIds")),
		childWorkflowTool("commerce.script_unit.reference_images.retry_failed", "重试失败参考图", "只重试参考图批次中的失败项。", authz.PermissionStoryboardGenerate, commerceRetryRunSchema(scriptUnitID)),
		childWorkflowTool("commerce.script_unit.video_prompts.generate", "生成带货视频提示词", "并发生成并审核选定镜头的视频提示词契约。", authz.PermissionStoryboardGenerate, objectSchemaRequired(productionSelection(), "scriptUnitId", "planId", "expectedPlanRevision", "expectedUnitGenerationId", "shotIds")),
		childWorkflowTool("commerce.script_unit.video_prompts.retry_failed", "重试失败视频提示词", "只重试视频提示词批次中的失败项。", authz.PermissionStoryboardGenerate, commerceRetryRunSchema(scriptUnitID)),
		childWorkflowTool("commerce.script_unit.shot_videos.generate", "生成带货镜头视频", "并发生成已有已审核提示词和参考图的镜头视频。", authz.PermissionStoryboardGenerate, objectSchemaRequired(withCommerceResolution(productionSelection()), "scriptUnitId", "planId", "expectedPlanRevision", "expectedUnitGenerationId", "shotIds")),
		childWorkflowTool("commerce.script_unit.shot_videos.retry_failed", "重试失败镜头视频", "只重试镜头视频批次中的失败项。", authz.PermissionStoryboardGenerate, commerceRetryRunSchema(scriptUnitID)),
		workflowTool("commerce.script_unit.shot_videos.cancel", "取消镜头视频批次", "取消指定镜头视频生产批次及其上游异步任务。", authz.PermissionWorkflowCancel, objectSchemaRequired(map[string]any{
			"scriptUnitId": scriptUnitID(),
			"runId":        stringSchema("生产批次 ID。"),
			"reason":       stringSchema("取消原因。"),
		}, "scriptUnitId", "runId")),

		readTool("commerce.script_unit.timeline.get", "读取带货时间线", "列出时间线；传 timelineId 时返回片段、字幕叠层和成片版本。", authz.PermissionProjectRead, objectSchemaRequired(map[string]any{
			"scriptUnitId": scriptUnitID(),
			"timelineId":   stringSchema("可选时间线 ID。"),
		}, "scriptUnitId")),
		writeTool("commerce.script_unit.timeline.update", "修改带货时间线", "按时间线 revision 修改标题和字幕/CTA 叠层。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"scriptUnitId":     scriptUnitID(),
			"timelineId":       stringSchema("时间线 ID。"),
			"expectedRevision": integerSchema("当前时间线 revision。", 1, 1000000000),
			"title":            stringSchema("时间线标题。"),
			"overlays": arraySchema("完整叠层列表。", objectSchemaRequired(map[string]any{
				"timelineClipId":   stringSchema("时间线片段 ID。"),
				"storyboardShotId": stringSchema("镜头 ID。"),
				"role":             enumSchema("叠层用途。", []string{"onscreen_text", "cta"}),
				"ordinal":          integerSchema("同类顺序。", 0, 1000000),
				"text":             stringSchema("显示文本。"),
				"startTick":        integerSchema("开始 tick。", 0, 2000000000),
				"endTick":          integerSchema("结束 tick。", 1, 2000000000),
				"style":            freeformObjectSchema("叠层样式。"),
			}, "role", "ordinal", "text", "startTick", "endTick")),
		}, "scriptUnitId", "timelineId", "expectedRevision"), true),

		readTool("commerce.script_unit.final.list", "带货成片列表", "列出指定脚本单元当前生产代的成片版本。", authz.PermissionProjectRead, objectSchemaRequired(map[string]any{
			"scriptUnitId": scriptUnitID(),
		}, "scriptUnitId")),
		childWorkflowTool("commerce.script_unit.final.compose", "合成带货成片", "根据冻结时间线合成带字幕、CTA 和原生音频的最终视频。", authz.PermissionWorkflowRun, objectSchemaRequired(map[string]any{
			"scriptUnitId":             scriptUnitID(),
			"timelineId":               stringSchema("时间线 ID。"),
			"expectedTimelineRevision": integerSchema("当前时间线 revision。", 1, 1000000000),
			"expectedUnitGenerationId": stringSchema("当前脚本单元生产代 ID。"),
			"title":                    stringSchema("成片标题。"),
			"resolution":               enumSchema("输出清晰度。", []string{"720p", "1080p", "1440p", "2160p"}),
		}, "scriptUnitId", "timelineId", "expectedTimelineRevision", "expectedUnitGenerationId")),
		writeTool("commerce.script_unit.final.activate", "激活带货成片", "激活已经通过生产校验且未过期的成片版本。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"scriptUnitId":        scriptUnitID(),
			"finalVideoVersionId": stringSchema("成片版本 ID。"),
		}, "scriptUnitId", "finalVideoVersionId"), true),

		childWorkflowTool("commerce.script_unit.batch.advance", "批量推进带货脚本", "将多个明确脚本单元独立推进到同一目标阶段；每个单元保留独立生产批次。", authz.PermissionWorkflowRun, commerceBatchAdvanceSchema(scriptUnitID)),
		childWorkflowTool("commerce.script_unit.batch.retry_failed", "重试批量推进失败项", "只重新调度跨脚本协调批次中的失败单元。", authz.PermissionWorkflowRun, objectSchemaRequired(map[string]any{
			"coordinatorId":  stringSchema("跨脚本协调批次 ID。"),
			"scriptUnitIds":  arraySchema("可选失败脚本单元 ID。", scriptUnitID()),
			"maxConcurrency": integerSchema("跨脚本最大并发。", 1, 16),
		}, "coordinatorId")),
		workflowTool("commerce.script_unit.batch.cancel", "取消批量推进", "取消跨脚本协调批次中仍在运行的单元任务。", authz.PermissionWorkflowCancel, objectSchemaRequired(map[string]any{
			"coordinatorId": stringSchema("跨脚本协调批次 ID。"),
			"reason":        stringSchema("取消原因。"),
		}, "coordinatorId")),
	}
}

func commerceRetryRunSchema(scriptUnitID func() map[string]any) []byte {
	return objectSchemaRequired(map[string]any{
		"scriptUnitId": scriptUnitID(),
		"runId":        stringSchema("原生产批次 ID。"),
		"itemIds":      arraySchema("可选失败项 ID；为空时重试全部可重试失败项。", stringSchema("生产项 ID。")),
		"concurrency":  integerSchema("重试并发数。", 1, 16),
	}, "scriptUnitId", "runId")
}

func withCommerceResolution(properties map[string]any) map[string]any {
	properties["resolution"] = enumSchema("生成分辨率。", []string{"720p", "1080p", "1440p", "2160p"})
	return properties
}

func withCommerceReferenceOperation(properties map[string]any) map[string]any {
	properties["operation"] = enumSchema("参考图阶段操作。", []string{"generate_prompts", "generate_images"})
	return properties
}

func commerceBatchAdvanceSchema(scriptUnitID func() map[string]any) []byte {
	return objectSchemaRequired(map[string]any{
		"targetStage": enumSchema("目标生产阶段。", []string{"storyboard", "reference_images", "video_prompts", "shot_videos", "final_compose"}),
		"items": arraySchema("每个脚本单元的冻结执行参数。", objectSchemaRequired(map[string]any{
			"scriptUnitId":             scriptUnitID(),
			"expectedUnitGenerationId": stringSchema("当前脚本单元生产代 ID。"),
			"planId":                   stringSchema("需要镜头阶段时的启用分镜方案 ID。"),
			"expectedPlanRevision":     integerSchema("当前分镜方案 revision。", 1, 1000000000),
			"timelineId":               stringSchema("成片阶段使用的时间线 ID。"),
			"expectedTimelineRevision": integerSchema("当前时间线 revision。", 1, 1000000000),
			"shotIds":                  arraySchema("可选镜头 ID。", stringSchema("镜头 ID。")),
			"force":                    booleanSchema("是否强制重生成。"),
			"resolution":               enumSchema("生成或输出清晰度。", []string{"720p", "1080p", "1440p", "2160p"}),
		}, "scriptUnitId", "expectedUnitGenerationId")),
		"unitConcurrency": integerSchema("单元内最大并发。", 1, 16),
		"maxConcurrency":  integerSchema("跨脚本最大并发。", 1, 16),
	}, "targetStage", "items")
}
