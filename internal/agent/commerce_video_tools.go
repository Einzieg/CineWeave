package agent

import "github.com/Einzieg/cineweave/internal/authz"

func CommerceVideoTools() []AgentTool {
	scriptUnitID := stringSchema("带货脚本 ID；已知 stableOrdinal 时可省略，由后端解析真实 ID。")
	stableOrdinal := integerSchema("commerce.script.list 返回的稳定序号；用户说第 N 条时必须传 N，禁止复制或拼接 UUID。", 1, 1000000)
	scriptUnitsRevision := integerSchema("commerce.script.list 返回的脚本集合 revision，用于阻止列表变化后读错脚本。", 1, 1000000000)
	variationSchema := objectSchemaRequired(map[string]any{
		"ordinal": integerSchema("变体顺序。", 1, 20),
		"key":     stringSchema("批次内唯一的变体 key。"),
		"label":   stringSchema("变体名称。"),
		"brief":   stringSchema("明确的变体说明。"),
	}, "ordinal", "key", "label", "brief")
	preserveSchema := arraySchema("必须保持不变的内容。", enumSchema("保持项。", []string{
		"product_facts", "selling_points", "prohibited_claims", "language", "cta", "approximate_duration",
	}))
	productVersionMutationSchema := objectSchemaRequired(map[string]any{
		"expectedRevision":  integerSchema("当前商品 revision。", 0, 1000000000),
		"name":              stringSchema("商品名称。"),
		"brand":             stringSchema("商品品牌。"),
		"sellingPoints":     arraySchema("核心卖点。", stringSchema("卖点。")),
		"immutableFeatures": freeformObjectSchema("不可改变的商品外观和事实特征。"),
		"prohibitedClaims":  arraySchema("禁止使用的宣传说法。", stringSchema("禁止说法。")),
		"metadata":          freeformObjectSchema("商品版本附加信息。"),
	}, "name")
	productRebuildImpactSchema := objectSchemaRequired(map[string]any{
		"targetProductVersionId":  stringSchema("目标商品版本 ID。"),
		"targetReferenceIds":      arraySchema("目标活动商品参考图 ID。", stringSchema("商品参考图 ID。")),
		"expectedProductRevision": integerSchema("当前商品 revision。", 1, 1000000000),
	}, "targetProductVersionId", "targetReferenceIds", "expectedProductRevision")
	productReferenceSchema := objectSchemaRequired(map[string]any{
		"referenceId":      stringSchema("商品参考图 ID。"),
		"expectedRevision": integerSchema("当前参考图 revision。", 1, 1000000000),
	}, "referenceId", "expectedRevision")
	scriptUnitSelectionSchema := objectSchemaRequired(map[string]any{
		"scriptUnitId":                scriptUnitID,
		"stableOrdinal":               stableOrdinal,
		"expectedScriptUnitsRevision": scriptUnitsRevision,
	}, "expectedScriptUnitsRevision")
	scriptReviseTool := writeTool(
		"commerce.script.revise",
		"按要求改写广告脚本",
		"后端读取广告脚本完整正文，按自然语言要求改写并以乐观锁更新；适用于压缩、润色、调整人物或场景等非精确替换。",
		authz.PermissionScriptWrite,
		objectSchemaRequired(map[string]any{
			"scriptUnitId":                scriptUnitID,
			"stableOrdinal":               stableOrdinal,
			"expectedScriptUnitsRevision": scriptUnitsRevision,
			"expectedRevision":            integerSchema("当前脚本 revision。", 1, 1000000000),
			"instruction":                 stringSchema("对当前完整脚本执行的改写要求。"),
			"targetMaxLength":             integerSchema("可选的更严格目标长度；最终结果始终受当前视频模型长度上限约束。", 1, 1000000),
			"targetLengthUnit":            enumSchema("目标长度单位。", []string{"characters", "utf8_bytes"}),
			"preserve":                    preserveSchema,
		}, "expectedRevision", "instruction"),
		true,
	)
	scriptReviseTool.Effects.MaySpendProvider = true
	attachmentAssignTool := writeTool("commerce.attachment.assign", "绑定助手图片", "把用户附加的图片绑定为商品公共参考图或指定广告脚本的自定义参考图。", authz.PermissionAssetWrite, objectSchemaRequired(map[string]any{
		"attachmentId":                stringSchema("任务约束中真实存在的助手图片附件 ID。"),
		"scope":                       enumSchema("绑定用途。", []string{"product_common", "script_custom"}),
		"scriptUnitId":                scriptUnitID,
		"stableOrdinal":               stableOrdinal,
		"expectedScriptUnitsRevision": scriptUnitsRevision,
		"referenceRole": enumSchema("商品参考图角色。", []string{
			"primary", "front", "back", "detail", "usage", "logo", "other",
		}),
		"setPrimary": booleanSchema("是否设为商品主图。"),
	}, "attachmentId", "scope"), true)
	exportAttachmentToMCP := false
	attachmentAssignTool.ExportToMCP = &exportAttachmentToMCP
	return []AgentTool{
		readTool("commerce.project.read_summary", "带货项目摘要", "读取商品、活动参考图、广告脚本、脚本裂变批次和直生成视频任务摘要。", authz.PermissionProjectRead, emptyObjectSchema()),
		readTool("commerce.product.get", "读取商品配置", "读取当前商品事实、版本和修订号。", authz.PermissionAssetRead, emptyObjectSchema()),
		readTool("commerce.product.versions.list", "商品版本", "读取不可变商品事实版本历史。", authz.PermissionAssetRead, emptyObjectSchema()),
		writeTool("commerce.product.version.create", "创建商品版本", "根据明确商品事实创建新的不可变商品版本，不自动切换现有生产代。", authz.PermissionAssetWrite, productVersionMutationSchema, true),
		writeTool("commerce.product.rebuild_impact", "商品换版影响", "计算并暂存切换商品版本和参考图集合对现有广告脚本生产代的影响。", authz.PermissionAssetWrite, productRebuildImpactSchema, false),
		destructiveTool("commerce.product.rebuild", "确认商品换版", "使用短期影响令牌切换商品版本和参考图集合，并归档受影响生产代。", authz.PermissionAssetWrite, objectSchemaRequired(map[string]any{
			"impactToken":             stringSchema("商品换版影响令牌。"),
			"expectedProductRevision": integerSchema("当前商品 revision。", 1, 1000000000),
		}, "impactToken", "expectedProductRevision")),
		readTool("commerce.product.references.list", "商品参考图", "读取活动商品参考图、主图和修订号。", authz.PermissionAssetRead, objectSchema(map[string]any{
			"status": enumSchema("状态筛选。", []string{"active", "archived", "all"}),
		}, false)),
		destructiveTool("commerce.product.reference.archive", "归档商品参考图", "归档商品参考图但保留既有生产快照中的不可变引用。", authz.PermissionAssetWrite, productReferenceSchema),
		writeTool("commerce.product.reference.set_primary", "设为商品主图", "把指定活动商品参考图设为主图。", authz.PermissionAssetWrite, productReferenceSchema, true),
		writeTool("commerce.product.reference.update", "修改商品参考图", "修改商品参考图角色、排序或主图状态。", authz.PermissionAssetWrite, objectSchemaRequired(map[string]any{
			"referenceId":      stringSchema("商品参考图 ID。"),
			"expectedRevision": integerSchema("当前参考图 revision。", 1, 1000000000),
			"referenceRole": enumSchema("参考图角色。", []string{
				"primary", "front", "back", "detail", "usage", "logo", "other",
			}),
			"ordinal":    integerSchema("参考图顺序。", 0, 1000000),
			"setPrimary": booleanSchema("是否设为商品主图。"),
		}, "referenceId", "expectedRevision"), true),
		attachmentAssignTool,
		writeTool("commerce.product.update", "修改商品配置", "基于当前商品事实创建新的不可变版本，并遵守商品 revision 并发控制。", authz.PermissionAssetWrite, objectSchemaRequired(map[string]any{
			"expectedRevision":  integerSchema("当前商品 revision。", 1, 1000000000),
			"name":              stringSchema("商品名称。"),
			"brand":             stringSchema("商品品牌。"),
			"sellingPoints":     arraySchema("核心卖点。", stringSchema("卖点。")),
			"immutableFeatures": freeformObjectSchema("不可改变的商品外观和事实特征。"),
			"prohibitedClaims":  arraySchema("禁止使用的宣传说法。", stringSchema("禁止说法。")),
			"metadata":          freeformObjectSchema("商品版本附加信息。"),
		}, "expectedRevision"), true),
		readTool("commerce.script.list", "列出广告脚本", "按稳定排序列出活动广告脚本和当前正文摘要。", authz.PermissionScriptRead, objectSchema(map[string]any{
			"status": enumSchema("状态筛选。", []string{"active", "archived", "all"}),
			"cursor": stringSchema("分页游标。"),
			"limit":  integerSchema("返回数量。", 1, 200),
		}, false)),
		readTool("commerce.script.get", "读取广告脚本", "读取指定广告脚本的当前正文和当前正文哈希。", authz.PermissionScriptRead, objectSchema(map[string]any{
			"scriptUnitId":                scriptUnitID,
			"stableOrdinal":               stableOrdinal,
			"expectedScriptUnitsRevision": scriptUnitsRevision,
		}, false)),
		scriptReviseTool,
		writeTool("commerce.script.create", "新增广告脚本", "创建一个独立广告脚本。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"expectedScriptUnitsRevision": integerSchema("当前脚本集合 revision。", 1, 1000000000),
			"title":                       stringSchema("脚本标题。"),
			"content":                     stringSchema("广告脚本正文。"),
			"sourceLanguageHint":          stringSchema("可选的脚本源语言 BCP-47 标记，例如 ms-MY；省略时由系统识别。"),
			"languageMode":                enumSchema("语言模式。", []string{"auto", "explicit"}),
			"explicitTargetLanguage":      stringSchema("目标语言 BCP-47 标记。"),
			"targetDurationSeconds":       integerSchema("目标视频秒数。", 1, 3600),
			"targetPlatform":              stringSchema("目标平台。"),
		}, "expectedScriptUnitsRevision", "title", "content", "languageMode", "targetDurationSeconds", "targetPlatform"), true),
		writeTool("commerce.script.defaults.update", "修改广告脚本默认值", "修改项目中新建广告脚本使用的时长、平台和语言默认值。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"expectedRevision":      integerSchema("当前默认配置 revision。", 1, 1000000000),
			"targetDurationSeconds": integerSchema("默认目标秒数。", 1, 3600),
			"targetPlatform":        stringSchema("默认目标平台。"),
			"languageMode":          enumSchema("语言模式。", []string{"auto", "explicit"}),
			"targetLanguage":        stringSchema("显式语言的 BCP-47 标记。"),
		}, "expectedRevision", "targetDurationSeconds", "targetPlatform", "languageMode"), true),
		writeTool("commerce.script.duplicate", "复制广告脚本", "复制指定广告脚本为独立可编辑脚本，不覆盖源脚本。", authz.PermissionScriptWrite, scriptUnitSelectionSchema, true),
		writeTool("commerce.script.create_language_variant", "创建多语言脚本", "基于指定广告脚本创建独立的 BCP-47 目标语言版本。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptUnitId":                scriptUnitID,
			"stableOrdinal":               stableOrdinal,
			"expectedScriptUnitsRevision": scriptUnitsRevision,
			"targetLanguage":              stringSchema("目标语言 BCP-47 标记。"),
		}, "expectedScriptUnitsRevision", "targetLanguage"), true),
		writeTool("commerce.script.reorder", "调整广告脚本顺序", "使用脚本集合 revision 原子更新全部活动广告脚本排序。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"expectedScriptUnitsRevision": scriptUnitsRevision,
			"items": arraySchema("新的完整排序。", objectSchemaRequired(map[string]any{
				"scriptUnitId": stringSchema("广告脚本 ID。"),
				"sortOrder":    integerSchema("从 1 开始的排序值。", 1, 1000000),
			}, "scriptUnitId", "sortOrder")),
		}, "expectedScriptUnitsRevision", "items"), true),
		draftTool("commerce.script.rebuild_impact", "广告脚本换代影响", "计算并暂存切换脚本版本、语言、时长、平台或生成方式的影响，返回短期令牌。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptUnitId":                scriptUnitID,
			"stableOrdinal":               stableOrdinal,
			"expectedScriptUnitsRevision": scriptUnitsRevision,
			"expectedRevision":            integerSchema("当前脚本 revision。", 1, 1000000000),
			"targetSourceScriptVersionId": stringSchema("目标脚本版本 ID。"),
			"targetLanguageMode":          enumSchema("语言模式。", []string{"auto", "explicit"}),
			"targetLanguage":              stringSchema("显式目标语言 BCP-47 标记。"),
			"targetDurationSeconds":       integerSchema("目标秒数。", 1, 3600),
			"targetPlatform":              stringSchema("目标平台。"),
			"targetStoryboardStrategy":    enumSchema("生成策略。", []string{"smart", "single_take"}),
		}, "expectedScriptUnitsRevision", "expectedRevision", "targetSourceScriptVersionId", "targetLanguageMode", "targetDurationSeconds", "targetPlatform", "targetStoryboardStrategy"), false),
		childWorkflowTool("commerce.script.rebuild", "确认广告脚本换代", "使用影响令牌原子创建新的广告脚本生产代，旧生产代在新代就绪前保持可用。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptUnitId":                scriptUnitID,
			"stableOrdinal":               stableOrdinal,
			"expectedScriptUnitsRevision": scriptUnitsRevision,
			"expectedRevision":            integerSchema("当前脚本 revision。", 1, 1000000000),
			"impactToken":                 stringSchema("影响分析返回的令牌。"),
		}, "expectedScriptUnitsRevision", "expectedRevision", "impactToken")),
		destructiveTool("commerce.script.reference.archive", "移除广告脚本参考图", "归档广告脚本自定义参考图，不影响商品公共参考图。", authz.PermissionAssetWrite, objectSchemaRequired(map[string]any{
			"scriptUnitId":                scriptUnitID,
			"stableOrdinal":               stableOrdinal,
			"expectedScriptUnitsRevision": scriptUnitsRevision,
			"referenceId":                 stringSchema("脚本参考图 ID。"),
			"expectedRevision":            integerSchema("当前参考图 revision。", 1, 1000000000),
		}, "expectedScriptUnitsRevision", "referenceId", "expectedRevision")),
		writeTool("commerce.script.update", "修改广告脚本", "使用用户提供的完整替换正文或精确字段更新广告脚本；自然语言改写应使用 commerce.script.revise。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptUnitId":                scriptUnitID,
			"stableOrdinal":               stableOrdinal,
			"expectedScriptUnitsRevision": scriptUnitsRevision,
			"expectedRevision":            integerSchema("当前脚本 revision。", 1, 1000000000),
			"title":                       stringSchema("脚本标题。"),
			"draftContent":                stringSchema("完整替换正文。"),
			"languageMode":                enumSchema("语言模式。", []string{"auto", "explicit"}),
			"explicitTargetLanguage":      stringSchema("显式目标语言 BCP-47 标记。"),
			"targetDurationSeconds":       integerSchema("目标视频秒数。", 1, 3600),
			"targetPlatform":              stringSchema("目标平台。"),
		}, "expectedRevision"), true),
		destructiveTool("commerce.script.archive", "归档广告脚本", "归档广告脚本，保留历史生产溯源。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptUnitId":                scriptUnitID,
			"stableOrdinal":               stableOrdinal,
			"expectedScriptUnitsRevision": scriptUnitsRevision,
			"expectedRevision":            integerSchema("当前脚本 revision。", 1, 1000000000),
			"reason":                      stringSchema("归档原因。"),
		}, "expectedRevision")),
		costedDraftTool("commerce.script.derive.preview", "预览脚本裂变", "根据源脚本当前正文生成结构化裂变候选，不创建脚本。", authz.PermissionScriptRead, objectSchemaRequired(map[string]any{
			"sourceScriptUnitId":          scriptUnitID,
			"stableOrdinal":               stableOrdinal,
			"expectedScriptUnitsRevision": scriptUnitsRevision,
			"count":                       integerSchema("变体数量。", 1, 20),
			"dimension":                   derivationDimensionSchema(),
			"instruction":                 stringSchema("裂变要求。"),
			"candidateValues":             arraySchema("用户指定的候选值。", stringSchema("候选值。")),
			"preserve":                    preserveSchema,
		}, "count", "dimension", "instruction"), true),
		withRequiredPermissions(costedChildWorkflowTool("commerce.script.derive.batch", "创建脚本裂变", "按完整变体计划创建裂变批次，每个条目生成一个独立广告脚本。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"sourceScriptUnitId":          scriptUnitID,
			"stableOrdinal":               stableOrdinal,
			"expectedScriptUnitsRevision": scriptUnitsRevision,
			"dimension":                   derivationDimensionSchema(),
			"instruction":                 stringSchema("裂变要求。"),
			"preserve":                    preserveSchema,
			"variations":                  arraySchema("最终变体计划。", variationSchema),
		}, "dimension", "instruction", "variations")), authz.PermissionWorkflowRun),
		readTool("commerce.script.derivation.get", "查看脚本裂变", "读取裂变批次、条目、重试谱系和输出脚本。", authz.PermissionScriptRead, objectSchemaRequired(map[string]any{
			"batchId": stringSchema("裂变批次 ID。"),
			"include": enumSchema("包含范围。", []string{"current", "lineage"}),
		}, "batchId")),
		withRequiredPermissions(costedChildWorkflowTool("commerce.script.derive.retry_failed", "重试失败变体", "创建只包含可重试失败条目的重试子批次。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"batchId": stringSchema("裂变批次 ID。"),
		}, "batchId")), authz.PermissionWorkflowRun),
		workflowTool("commerce.script.derive.cancel", "取消脚本裂变", "取消尚未完成的裂变批次和条目。", authz.PermissionWorkflowCancel, objectSchemaRequired(map[string]any{
			"batchId": stringSchema("裂变批次 ID。"),
			"reason":  stringSchema("取消原因。"),
		}, "batchId")),
		readTool("commerce.video.options", "视频生成选项", "读取当前模型可执行时长、分辨率、音频和参考图输入契约。", authz.PermissionWorkflowRun, objectSchema(map[string]any{
			"scriptUnitId":                scriptUnitID,
			"stableOrdinal":               stableOrdinal,
			"expectedScriptUnitsRevision": scriptUnitsRevision,
		}, false)),
		readTool("commerce.video.list", "视频任务列表", "列出直生成视频任务。", authz.PermissionWorkflowRead, objectSchema(map[string]any{
			"scriptUnitId": scriptUnitID,
			"status":       stringSchema("状态筛选。"),
			"limit":        integerSchema("返回数量。", 1, 200),
		}, false)),
		readTool("commerce.video.get", "查看视频任务", "读取一个直生成视频任务、真实进度和输出。", authz.PermissionWorkflowRead, objectSchemaRequired(map[string]any{
			"jobId": stringSchema("直生成视频任务 ID。"),
		}, "jobId")),
		costedChildWorkflowTool("commerce.video.generate", "生成带货视频", "使用广告脚本当前正文和商品或自定义参考图创建直生成视频任务。", authz.PermissionWorkflowRun, objectSchemaRequired(map[string]any{
			"scriptUnitId":                scriptUnitID,
			"stableOrdinal":               stableOrdinal,
			"expectedScriptUnitsRevision": scriptUnitsRevision,
			"durationSeconds":             integerSchema("视频秒数；省略时使用可执行时长中的最大值。", 1, 3600),
			"resolution":                  stringSchema("分辨率；省略时使用当前默认值。"),
			"generateAudio":               booleanSchema("是否生成原生音频。"),
			"references": arraySchema("参考图。", objectSchemaRequired(map[string]any{
				"sourceType": enumSchema("参考图来源。", []string{"product", "custom"}),
				"sourceId":   stringSchema("参考图或上传 ID。"),
			}, "sourceType", "sourceId")),
		})),
		workflowTool("commerce.video.cancel", "取消视频任务", "取消运行中的直生成视频任务。", authz.PermissionWorkflowCancel, objectSchemaRequired(map[string]any{
			"jobId":  stringSchema("直生成视频任务 ID。"),
			"reason": stringSchema("取消原因。"),
		}, "jobId")),
	}
}

func withRequiredPermissions(tool AgentTool, permissions ...string) AgentTool {
	tool.Permissions = append(tool.Permissions, permissions...)
	return tool
}

func costedChildWorkflowTool(name, label, description, permission string, schema []byte) AgentTool {
	tool := childWorkflowTool(name, label, description, permission, schema)
	tool.Effects.MaySpendProvider = true
	return tool
}

func derivationDimensionSchema() map[string]any {
	return enumSchema("裂变维度。", []string{"scene", "hook", "audience", "tone", "language", "cta", "custom"})
}
