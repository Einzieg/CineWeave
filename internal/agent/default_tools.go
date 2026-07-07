package agent

import (
	"encoding/json"

	"github.com/Einzieg/cineweave/internal/authz"
)

func DefaultRegistry() (*Registry, error) {
	return NewRegistry(DefaultTools()...)
}

func DefaultTools() []AgentTool {
	return []AgentTool{
		readTool("project.read_summary", "项目摘要", "读取项目来源、剧本、资产、分镜、工作流和审阅摘要。", authz.PermissionProjectRead, emptyObjectSchema()),
		readTool("source.list", "原文列表", "列出当前项目的小说或剧本来源。", authz.PermissionSourceRead, limitSchema()),
		readTool("source.list_chapters", "分集章节", "列出小说来源的分卷、分集和章节。", authz.PermissionSourceRead, objectSchema(map[string]any{
			"sourceId": stringSchema("来源 ID。为空时由执行器选择项目默认来源。"),
			"limit":    integerSchema("返回数量。", 1, 200),
			"offset":   integerSchema("偏移量。", 0, 100000),
		}, false)),
		readTool("script.list", "剧本列表", "列出项目剧本和当前版本摘要。", authz.PermissionScriptRead, limitSchema()),
		readTool("script.get", "读取剧本", "读取指定剧本和版本内容。", authz.PermissionScriptRead, objectSchemaRequired(map[string]any{
			"scriptId":  stringSchema("剧本 ID。"),
			"versionId": stringSchema("版本 ID。为空时读取当前版本。"),
		}, "scriptId")),
		readTool("asset.list", "资产列表", "列出核心资产、状态和参考图摘要。", authz.PermissionAssetRead, limitSchema()),
		readTool("storyboard.list", "分镜列表", "列出分镜镜头和生产状态摘要。", authz.PermissionProjectRead, limitSchema()),
		readTool("workflow.read_runs", "任务列表", "读取最近 workflow runs。", authz.PermissionWorkflowRead, limitSchema()),
		readTool("workflow.read_nodes", "任务节点", "读取指定 workflow 的节点运行详情。", authz.PermissionWorkflowRead, workflowRunSchema()),
		readTool("workflow.read_shots", "任务镜头", "读取指定 workflow 的镜头和预览状态。", authz.PermissionWorkflowRead, workflowRunSchema()),
		readTool("review.list_items", "审阅问题", "读取项目 open review items。", authz.PermissionProjectRead, limitSchema()),
		readTool("artifact.list", "成果列表", "列出项目 artifacts。", authz.PermissionArtifactRead, limitSchema()),
		readTool("artifact.preview_url", "成果预览", "生成短期 artifact 预览 URL。", authz.PermissionArtifactRead, objectSchemaRequired(map[string]any{
			"artifactId":     stringSchema("Artifact ID。"),
			"expiresSeconds": integerSchema("预览 URL 有效秒数。", 60, 86400),
		}, "artifactId")),
		readTool("provider.list_status", "供应商状态", "读取供应商、模型、限额、熔断和成本摘要。", authz.PermissionProviderRead, emptyObjectSchema()),

		draftTool("review.run", "运行审阅", "运行 deterministic review 和可选 agent review。", authz.PermissionProjectRead, objectSchema(map[string]any{
			"reviewType":                 enumSchema("审阅类型。", []string{"project", "workflow", "asset", "storyboard", "timeline", "final_video"}),
			"includeAgent":               booleanSchema("是否启用 agent review。"),
			"includeDeterministicChecks": booleanSchema("是否运行 deterministic checks。"),
		}, false), false),
		draftTool("review.generate_fix", "生成修复建议", "生成 review fix 草稿，不直接应用。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"itemId":      stringSchema("Review item ID。"),
			"mode":        enumSchema("修复模式。", []string{"deterministic", "agent"}),
			"instruction": stringSchema("给 agent fix 的额外要求。"),
		}, "itemId"), false),
		draftTool("prompt.render_test", "提示词测试", "渲染 prompt 并执行低风险测试。", authz.PermissionPromptRead, objectSchemaRequired(map[string]any{
			"templateKey": stringSchema("Prompt template key。"),
			"variables":   objectSchema(nil, false),
			"input":       objectSchema(nil, false),
		}, "templateKey"), false),
		draftTool("script.rewrite_preview", "剧本改写预览", "生成改写预览，不写入剧本版本。", authz.PermissionScriptRead, objectSchemaRequired(map[string]any{
			"scriptId":      stringSchema("剧本 ID。"),
			"versionId":     stringSchema("剧本版本 ID。为空时使用当前版本。"),
			"instruction":   stringSchema("改写要求。"),
			"sourceSceneId": stringSchema("可选场景 ID。"),
		}, "scriptId", "instruction"), false),

		writeTool("script.generate_from_source", "生成剧本", "从来源或改编计划生成剧本版本。", authz.PermissionScriptWrite, objectSchema(map[string]any{
			"sourceId":    stringSchema("来源 ID。为空时使用项目最新来源。"),
			"planId":      stringSchema("改编计划 ID。"),
			"title":       stringSchema("剧本标题。"),
			"instruction": stringSchema("生成要求。"),
		}, false), true),
		writeTool("script.rewrite", "改写剧本", "改写剧本并创建新版本。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptId":    stringSchema("剧本 ID。"),
			"versionId":   stringSchema("剧本版本 ID。为空时使用当前版本。"),
			"instruction": stringSchema("改写要求。"),
			"activate":    booleanSchema("是否创建后激活。"),
		}, "scriptId", "instruction"), true),
		writeTool("script.create_version", "创建剧本版本", "创建剧本新版本。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptId":      stringSchema("剧本 ID。"),
			"content":       stringSchema("版本内容。"),
			"contentFormat": enumSchema("内容格式。", []string{"plain_text", "markdown"}),
			"sourceType":    stringSchema("版本来源类型。"),
			"metadata":      objectSchema(nil, false),
			"activate":      booleanSchema("是否创建后激活。"),
		}, "scriptId", "content"), true),
		writeTool("script.activate_version", "激活剧本版本", "切换剧本当前版本。", authz.PermissionScriptWrite, objectSchemaRequired(map[string]any{
			"scriptId":  stringSchema("剧本 ID。"),
			"versionId": stringSchema("版本 ID。"),
		}, "scriptId", "versionId"), true),
		writeTool("asset.update", "更新资产", "修改资产描述、prompt、traits 或审核状态。", authz.PermissionAssetWrite, objectSchemaRequired(map[string]any{
			"assetId": stringSchema("资产 ID。"),
			"patch":   objectSchema(nil, false),
		}, "assetId", "patch"), true),
		writeTool("storyboard.update_shot", "更新分镜", "修改分镜镜头字段。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"shotId": stringSchema("镜头 ID。"),
			"patch":  objectSchema(nil, false),
		}, "shotId", "patch"), true),
		writeTool("storyboard.reorder", "重排分镜", "调整分镜镜头顺序。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"shotIds": arraySchema("按目标顺序排列的镜头 ID。", stringSchema("镜头 ID。")),
		}, "shotIds"), true),
		writeTool("timeline.update_clip", "更新时间线片段", "修改时间线片段字段。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"clipId": stringSchema("时间线片段 ID。"),
			"patch":  objectSchema(nil, false),
		}, "clipId", "patch"), true),
		writeTool("review.apply_fix", "应用修复", "应用已批准的 review fix。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"fixId":               stringSchema("Review fix ID。"),
			"resolveReviewItem":   booleanSchema("应用后是否同时解决对应审阅问题。"),
			"triggerRegeneration": booleanSchema("是否在应用后触发再生成。Project Agent 默认不会自动再生成。"),
		}, "fixId"), true),
		writeTool("review.dismiss_fix", "忽略修复", "忽略 review fix 草稿。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"fixId": stringSchema("Review fix ID。"),
		}, "fixId"), true),

		workflowTool("workflow.start", "启动任务", "启动受控 workflow。", authz.PermissionWorkflowRun, objectSchemaRequired(map[string]any{
			"workflowType": enumSchema("Workflow 类型。", []string{"extract_novel_events", "generate_adaptation_plan", "adaptation_plan_to_script", "source_to_script", "parse_script_scenes", "script_to_assets", "script_to_storyboard", "script_to_video", "full_production", "compose_timeline"}),
			"input":        objectSchema(nil, false),
		}, "workflowType")),
		workflowTool("workflow.cancel", "取消任务", "取消运行中的 workflow。", authz.PermissionWorkflowCancel, objectSchemaRequired(map[string]any{
			"workflowRunId": stringSchema("Workflow run ID。"),
			"reason":        stringSchema("取消原因。"),
		}, "workflowRunId")),
		workflowTool("shot.status", "镜头状态", "读取镜头生产状态。", authz.PermissionWorkflowRead, limitSchema()),
		workflowTool("shot.generate_missing_images", "生成缺失图片", "为缺图镜头启动图片生成。", authz.PermissionWorkflowRun, objectSchema(map[string]any{
			"maxConcurrency": integerSchema("最大并发。", 1, 16),
		}, false)),
		workflowTool("shot.generate_missing_videos", "生成缺失视频", "为缺视频镜头启动视频生成。", authz.PermissionWorkflowRun, objectSchema(map[string]any{
			"maxConcurrency": integerSchema("最大并发。", 1, 8),
		}, false)),
		workflowTool("shot.cancel_running_videos", "取消镜头视频", "取消运行中的镜头视频任务。", authz.PermissionWorkflowCancel, emptyObjectSchema()),
		workflowTool("timeline.compose", "合成时间线", "触发时间线合成。", authz.PermissionWorkflowRun, objectSchema(map[string]any{
			"timelineId": stringSchema("时间线 ID。"),
		}, false)),
		workflowTool("final_video.activate", "激活成片", "激活最终视频版本。", authz.PermissionProjectWrite, objectSchemaRequired(map[string]any{
			"finalVideoId": stringSchema("成片 ID。"),
		}, "finalVideoId")),

		adminTool("provider.test_model", "测试模型", "测试供应商模型可用性。", authz.PermissionProviderManage, objectSchemaRequired(map[string]any{
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
}

func readTool(name, label, description, permission string, schema json.RawMessage) AgentTool {
	return AgentTool{Name: name, Label: label, Description: description, Risk: ToolRiskRead, Permission: permission, InputSchema: schema}
}

func draftTool(name, label, description, permission string, schema json.RawMessage, requiresApproval bool) AgentTool {
	return AgentTool{Name: name, Label: label, Description: description, Risk: ToolRiskDraft, Permission: permission, InputSchema: schema, RequiresApproval: requiresApproval}
}

func writeTool(name, label, description, permission string, schema json.RawMessage, requiresApproval bool) AgentTool {
	return AgentTool{Name: name, Label: label, Description: description, Risk: ToolRiskWrite, Permission: permission, InputSchema: schema, RequiresApproval: requiresApproval}
}

func workflowTool(name, label, description, permission string, schema json.RawMessage) AgentTool {
	return AgentTool{Name: name, Label: label, Description: description, Risk: ToolRiskWorkflow, Permission: permission, InputSchema: schema, RequiresApproval: true}
}

func adminTool(name, label, description, permission string, schema json.RawMessage) AgentTool {
	return AgentTool{Name: name, Label: label, Description: description, Risk: ToolRiskAdmin, Permission: permission, InputSchema: schema, RequiresApproval: true}
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
