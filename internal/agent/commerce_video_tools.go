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
	return []AgentTool{
		readTool("commerce.project.read_summary", "带货项目摘要", "读取商品、活动参考图、广告脚本、脚本裂变批次和直生成视频任务摘要。", authz.PermissionProjectRead, emptyObjectSchema()),
		readTool("commerce.product.get", "读取商品配置", "读取当前商品事实、版本和修订号。", authz.PermissionAssetRead, emptyObjectSchema()),
		readTool("commerce.product.references.list", "商品参考图", "读取活动商品参考图、主图和修订号。", authz.PermissionAssetRead, objectSchema(map[string]any{
			"status": enumSchema("状态筛选。", []string{"active", "archived", "all"}),
		}, false)),
		writeTool("commerce.attachment.assign", "绑定助手图片", "把用户附加的图片绑定为商品公共参考图或指定广告脚本的自定义参考图。", authz.PermissionAssetWrite, objectSchemaRequired(map[string]any{
			"attachmentId":                stringSchema("任务约束中真实存在的助手图片附件 ID。"),
			"scope":                       enumSchema("绑定用途。", []string{"product_common", "script_custom"}),
			"scriptUnitId":                scriptUnitID,
			"stableOrdinal":               stableOrdinal,
			"expectedScriptUnitsRevision": scriptUnitsRevision,
			"referenceRole": enumSchema("商品参考图角色。", []string{
				"primary", "front", "back", "detail", "usage", "logo", "other",
			}),
			"setPrimary": booleanSchema("是否设为商品主图。"),
		}, "attachmentId", "scope"), true),
		writeTool("commerce.product.update", "修改商品配置", "修改当前商品事实，并遵守商品 revision 并发控制。", authz.PermissionAssetWrite, objectSchemaRequired(map[string]any{
			"expectedRevision": integerSchema("当前商品 revision。", 1, 1000000000),
			"patch":            freeformObjectSchema("商品字段补丁。"),
		}, "expectedRevision", "patch"), true),
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
			"languageMode":                enumSchema("语言模式。", []string{"auto", "explicit"}),
			"explicitTargetLanguage":      stringSchema("目标语言 BCP-47 标记。"),
			"targetDurationSeconds":       integerSchema("目标视频秒数。", 1, 3600),
			"targetPlatform":              stringSchema("目标平台。"),
		}, "expectedScriptUnitsRevision", "title", "content", "languageMode", "targetDurationSeconds", "targetPlatform"), true),
		writeTool("commerce.script.update", "修改广告脚本", "使用用户提供的完整替换正文或精确字段补丁更新广告脚本；自然语言改写应使用 commerce.script.revise。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptUnitId":                scriptUnitID,
			"stableOrdinal":               stableOrdinal,
			"expectedScriptUnitsRevision": scriptUnitsRevision,
			"expectedRevision":            integerSchema("当前脚本 revision。", 1, 1000000000),
			"patch":                       freeformObjectSchema("脚本字段补丁。"),
		}, "expectedRevision", "patch"), true),
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
