package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maxScriptAgentToolCalls = 6

type scriptAgentPlan struct {
	Message   string          `json:"message"`
	ToolCalls []agentToolCall `json:"toolCalls"`
}

type agentToolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type agentToolResult struct {
	Name                string                `json:"name"`
	Label               string                `json:"label"`
	Status              string                `json:"status"`
	Summary             string                `json:"summary"`
	Arguments           map[string]any        `json:"arguments,omitempty"`
	Data                map[string]any        `json:"data,omitempty"`
	ChildWorkflowRunIDs []string              `json:"childWorkflowRunIds,omitempty"`
	Retryable           bool                  `json:"retryable,omitempty"`
	NextActions         []agentToolNextAction `json:"nextActions,omitempty"`
	ErrorCode           string                `json:"errorCode,omitempty"`
	ErrorMessage        string                `json:"errorMessage,omitempty"`
}

type agentToolNextAction struct {
	Label     string         `json:"label"`
	Reason    string         `json:"reason,omitempty"`
	Tool      string         `json:"tool,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

func buildScriptAgentToolPrompt(project Project, messages []AgentMessage) string {
	var builder strings.Builder
	builder.WriteString("你是 CineWeave 项目工作台里的 AI 助手。你可以通过后端白名单工具读取和控制当前项目。\n")
	builder.WriteString("必须只输出一个 JSON 对象，不能输出 Markdown、代码块或额外解释。\n")
	builder.WriteString("JSON 结构：{\"message\":\"给用户看的中文回复\",\"toolCalls\":[{\"name\":\"工具名\",\"arguments\":{}}]}。\n")
	builder.WriteString("如果只需要回答问题，toolCalls 输出空数组。如果需要操作项目，必须选择下列工具。\n\n")
	builder.WriteString("项目上下文：\n")
	builder.WriteString(fmt.Sprintf("- 项目ID：%s\n", project.ID))
	builder.WriteString(fmt.Sprintf("- 项目名称：%s\n", project.Name))
	builder.WriteString(fmt.Sprintf("- 内容类型：%s\n", stringValue(project.ContentType)))
	builder.WriteString(fmt.Sprintf("- 视频比例：%s\n", project.VideoRatio))
	builder.WriteString("\n可用工具：\n")
	builder.WriteString("- get_project_status：读取生产状态、阶段进度和下一步动作。\n")
	builder.WriteString("- list_sources：按阅读顺序列出原文/剧本来源摘要，返回 firstVolumeIndex。参数：limit。\n")
	builder.WriteString("- list_source_chapters：列出小说分集/章节摘要，返回 volumeIndex 和 sectionIndex。参数：sourceId, limit, offset。\n")
	builder.WriteString("- list_events：列出事件摘要。参数：sourceId, chapterId, limit。\n")
	builder.WriteString("- list_scripts：列出剧本摘要。参数：limit。\n")
	builder.WriteString("- list_assets：列出资产摘要。参数：limit。\n")
	builder.WriteString("- list_storyboard_shots：列出分镜镜头摘要。参数：limit。\n")
	builder.WriteString("- list_workflow_runs：列出最近工作流。参数：limit。\n")
	builder.WriteString("- start_production_action：启动生产动作。参数：action, sourceId, scriptId, options。action 可选 extract_events, generate_adaptation_plan, generate_script_from_plan, generate_script, parse_script_scenes, analyze_assets, generate_asset_images, generate_storyboard, analyze_shot_assets, generate_derived_asset_images, generate_shot_images, generate_shot_videos, compose_final_video, run_full_production。按章节提取事件时把 chapterIds 放进 options。\n")
	builder.WriteString("- cancel_workflow：取消运行中的工作流。参数：workflowRunId, reason。\n\n")
	builder.WriteString("执行规则：\n")
	builder.WriteString("- 只有用户明确要求开始、执行、生成、提取、取消、创建、运行、重跑时，才调用 start_production_action 或 cancel_workflow。\n")
	builder.WriteString("- 不要虚构 ID；缺少 sourceId、chapterId、scriptId 或 workflowRunId 时，先调用列表/状态工具查询。\n")
	builder.WriteString("- 需要分集/章节操作时，先通过 list_sources 和 list_source_chapters 找到正确 sourceId/chapterId；用户说第一卷第一节时必须匹配 volumeIndex=1 且 sectionIndex=1。\n")
	builder.WriteString("- 输出 message 要简短，说明你准备做什么或已经需要查询什么。\n\n")
	builder.WriteString("最近对话：\n")
	for _, message := range messages {
		role := message.Role
		switch role {
		case "assistant":
			role = "助手"
		case "user":
			role = "用户"
		case "system":
			role = "系统"
		case "tool":
			role = "工具"
		}
		content := strings.TrimSpace(message.Content)
		if len([]rune(content)) > 1600 {
			runes := []rune(content)
			content = string(runes[:1600]) + "..."
		}
		builder.WriteString(fmt.Sprintf("%s：%s\n", role, content))
	}
	return builder.String()
}

func buildScriptAgentFinalPrompt(project Project, messages []AgentMessage, plan scriptAgentPlan, results []agentToolResult) string {
	var builder strings.Builder
	builder.WriteString("你是 CineWeave 项目工作台里的 AI 助手。你刚刚通过后端工具读取或控制了项目。\n")
	builder.WriteString("请用中文回复最后一条用户消息，直接说明结果、下一步和需要用户注意的失败项。不要输出 JSON。\n\n")
	builder.WriteString("项目上下文：\n")
	builder.WriteString(fmt.Sprintf("- 项目名称：%s\n", project.Name))
	builder.WriteString(fmt.Sprintf("- 内容类型：%s\n", stringValue(project.ContentType)))
	builder.WriteString(fmt.Sprintf("- 视频比例：%s\n", project.VideoRatio))
	if strings.TrimSpace(plan.Message) != "" {
		builder.WriteString(fmt.Sprintf("- 规划说明：%s\n", strings.TrimSpace(plan.Message)))
	}
	builder.WriteString("\n最近对话：\n")
	for _, message := range messages {
		role := message.Role
		switch role {
		case "assistant":
			role = "助手"
		case "user":
			role = "用户"
		case "system":
			role = "系统"
		case "tool":
			role = "工具"
		}
		content := strings.TrimSpace(message.Content)
		if len([]rune(content)) > 1200 {
			runes := []rune(content)
			content = string(runes[:1200]) + "..."
		}
		builder.WriteString(fmt.Sprintf("%s：%s\n", role, content))
	}
	builder.WriteString("\n工具执行结果：\n")
	for _, result := range results {
		builder.WriteString(fmt.Sprintf("- %s：%s，%s\n", result.Label, result.Status, result.Summary))
		if len(result.Data) > 0 {
			encoded := string(mustMarshal(result.Data))
			if len([]rune(encoded)) > 1600 {
				runes := []rune(encoded)
				encoded = string(runes[:1600]) + "..."
			}
			builder.WriteString(fmt.Sprintf("  数据：%s\n", encoded))
		}
	}
	return builder.String()
}

func parseScriptAgentPlan(raw string) (scriptAgentPlan, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return scriptAgentPlan{}, fmt.Errorf("empty agent plan")
	}
	text = stripJSONFences(text)
	if !strings.HasPrefix(text, "{") && !strings.HasPrefix(text, "[") {
		if extracted, ok := extractJSONObject(text); ok {
			text = extracted
		}
	}
	if strings.HasPrefix(text, "[") {
		var calls []agentToolCall
		if err := json.Unmarshal([]byte(text), &calls); err != nil {
			return scriptAgentPlan{}, err
		}
		return scriptAgentPlan{ToolCalls: normalizeAgentToolCalls(calls)}, nil
	}
	var payload struct {
		Message        string          `json:"message"`
		ToolCalls      []agentToolCall `json:"toolCalls"`
		ToolCallsSnake []agentToolCall `json:"tool_calls"`
		Tools          []agentToolCall `json:"tools"`
		Actions        []agentToolCall `json:"actions"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return scriptAgentPlan{}, err
	}
	calls := payload.ToolCalls
	if len(calls) == 0 {
		calls = payload.ToolCallsSnake
	}
	if len(calls) == 0 {
		calls = payload.Tools
	}
	if len(calls) == 0 {
		calls = payload.Actions
	}
	return scriptAgentPlan{
		Message:   strings.TrimSpace(payload.Message),
		ToolCalls: normalizeAgentToolCalls(calls),
	}, nil
}

func stripJSONFences(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "```") {
		return text
	}
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```JSON")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}

func extractJSONObject(text string) (string, bool) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return "", false
	}
	return strings.TrimSpace(text[start : end+1]), true
}

func normalizeAgentToolCalls(calls []agentToolCall) []agentToolCall {
	out := make([]agentToolCall, 0, len(calls))
	for _, call := range calls {
		call.Name = strings.TrimSpace(call.Name)
		if call.Name == "" {
			continue
		}
		if call.Arguments == nil {
			call.Arguments = map[string]any{}
		}
		out = append(out, call)
	}
	if len(out) > maxScriptAgentToolCalls {
		out = out[:maxScriptAgentToolCalls]
	}
	return out
}

func (s *Server) executeScriptAgentTool(r *http.Request, principal auth.Principal, project Project, userMessage AgentMessage, allowMutations bool, call agentToolCall) agentToolResult {
	name := strings.TrimSpace(call.Name)
	args := call.Arguments
	if args == nil {
		args = map[string]any{}
	}
	label := scriptAgentToolLabel(name)
	if scriptAgentToolRequiresMutation(name) && !allowMutations {
		return agentToolResult{
			Name:         name,
			Label:        label,
			Status:       "failed",
			Summary:      "需要用户明确要求执行或取消后才能进行项目变更。",
			Arguments:    args,
			ErrorCode:    "MUTATION_CONFIRMATION_REQUIRED",
			ErrorMessage: "需要明确的执行意图",
		}
	}
	switch name {
	case "get_project_status":
		return s.agentToolProjectStatus(r, principal, project, args)
	case "list_sources":
		return s.agentToolListSources(r, principal, project, args)
	case "list_source_chapters":
		return s.agentToolListSourceChapters(r, principal, project, args)
	case "list_events":
		return s.agentToolListEvents(r, principal, project, args)
	case "list_scripts":
		return s.agentToolListScripts(r, principal, project, args)
	case "list_assets":
		return s.agentToolListAssets(r, principal, project, args)
	case "list_storyboard_shots":
		return s.agentToolListStoryboardShots(r, principal, project, args)
	case "list_workflow_runs":
		return s.agentToolListWorkflowRuns(r, principal, project, args)
	case "start_production_action":
		return s.agentToolStartProductionAction(r, principal, project, userMessage, args)
	case "cancel_workflow":
		return s.agentToolCancelWorkflow(r, principal, project, args)
	default:
		return agentToolResult{
			Name:         name,
			Label:        label,
			Status:       "failed",
			Summary:      "工具不在后端白名单中，未执行。",
			Arguments:    args,
			ErrorCode:    "UNKNOWN_TOOL",
			ErrorMessage: "unknown tool",
		}
	}
}

func (s *Server) agentToolProjectStatus(r *http.Request, principal auth.Principal, project Project, args map[string]any) agentToolResult {
	if err := s.authorizer.Authorize(r.Context(), principal, authz.PermissionProjectRead, authz.Resource{ProjectID: project.ID}); err != nil {
		return agentToolError("get_project_status", args, err)
	}
	status, err := s.productionStatus(r, project)
	if err != nil {
		return agentToolError("get_project_status", args, err)
	}
	gapSummary, err := s.agentProjectGapSummary(r.Context(), project, status)
	if err != nil {
		return agentToolError("get_project_status", args, err)
	}
	summary := gapSummary.Summary
	if strings.TrimSpace(summary) == "" {
		summary = fmt.Sprintf("当前阶段 %s，状态 %s，进度 %d%%。", status.Overall.Stage, status.Overall.Status, status.Overall.Progress)
	}
	return agentToolOK("get_project_status", args, summary, map[string]any{
		"productionStatus":  status,
		"projectGapSummary": gapSummary,
	})
}

func (s *Server) agentToolListSources(r *http.Request, principal auth.Principal, project Project, args map[string]any) agentToolResult {
	if err := s.authorizer.Authorize(r.Context(), principal, authz.PermissionSourceRead, authz.Resource{ProjectID: project.ID}); err != nil {
		return agentToolError("list_sources", args, err)
	}
	limit := agentIntArg(args, "limit", 20, 1, 100)
	sources, err := s.projectSourceList(r, project.ID, "active")
	if err != nil {
		return agentToolError("list_sources", args, err)
	}
	if len(sources) > limit {
		sources = sources[:limit]
	}
	items := make([]map[string]any, 0)
	for _, source := range sources {
		item := map[string]any{
			"id":            source.ID,
			"sourceType":    source.SourceType,
			"title":         source.Title,
			"contentFormat": source.ContentFormat,
			"status":        source.Status,
			"chapterCount":  source.ChapterCount,
			"createdAt":     source.CreatedAt,
			"updatedAt":     source.UpdatedAt,
		}
		if source.FirstVolumeIndex > 0 {
			item["firstVolumeIndex"] = source.FirstVolumeIndex
		}
		items = append(items, item)
	}
	return agentToolOK("list_sources", args, fmt.Sprintf("找到 %d 个原文/来源。", len(items)), map[string]any{"items": items})
}

func (s *Server) agentToolListSourceChapters(r *http.Request, principal auth.Principal, project Project, args map[string]any) agentToolResult {
	if err := s.authorizer.Authorize(r.Context(), principal, authz.PermissionSourceRead, authz.Resource{ProjectID: project.ID}); err != nil {
		return agentToolError("list_source_chapters", args, err)
	}
	sourceID := agentReferenceStringArg(args, "sourceId")
	if sourceID == "" {
		resolved, err := s.activeProductionSourceID(r, project.ID, "")
		if err != nil {
			return agentToolError("list_source_chapters", args, err)
		}
		sourceID = resolved
	}
	if sourceID == "" {
		return agentToolError("list_source_chapters", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "sourceId is required when the project has no source"))
	}
	source, err := s.projectSource(r, project.ID, sourceID)
	if err != nil {
		return agentToolError("list_source_chapters", args, err)
	}
	if source.SourceType != "novel" {
		return agentToolError("list_source_chapters", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "sourceType must be novel"))
	}
	limit := agentIntArg(args, "limit", 50, 1, 200)
	offset := agentIntArg(args, "offset", 0, 0, 100000)
	rows, err := s.db.Query(r.Context(), `
		WITH event_counts AS (
			SELECT chapter_id,
			       count(*) AS event_count,
			       count(*) FILTER (WHERE review_status = 'approved') AS approved_event_count,
			       count(*) FILTER (WHERE review_status <> 'approved') AS pending_event_review_count
			FROM novel_events
			WHERE project_id = $1
			GROUP BY chapter_id
		)
		SELECT c.id, c.source_id, c.chapter_index, c.volume_index, c.section_index, c.volume_title, c.chapter_title,
		       char_length(c.content), c.event_state, c.event_summary, c.error_message,
		       c.created_at, c.updated_at,
		       COALESCE(ec.event_count, 0),
		       COALESCE(ec.approved_event_count, 0),
		       COALESCE(ec.pending_event_review_count, 0)
		FROM novel_chapters c
		LEFT JOIN event_counts ec ON ec.chapter_id = c.id
		WHERE c.project_id = $1 AND c.source_id = $2
		ORDER BY COALESCE(c.volume_index, 0) ASC, COALESCE(c.section_index, c.chapter_index) ASC, c.chapter_index ASC
		LIMIT $3 OFFSET $4
	`, project.ID, sourceID, limit, offset)
	if err != nil {
		return agentToolError("list_source_chapters", args, err)
	}
	defer rows.Close()
	items := make([]NovelChapterSummary, 0)
	for rows.Next() {
		item, err := scanNovelChapterSummary(rows)
		if err != nil {
			return agentToolError("list_source_chapters", args, err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return agentToolError("list_source_chapters", args, err)
	}
	return agentToolOK("list_source_chapters", args, fmt.Sprintf("来源《%s》返回 %d 个分集/章节。", source.Title, len(items)), map[string]any{
		"sourceId": sourceID,
		"items":    items,
		"limit":    limit,
		"offset":   offset,
	})
}

func (s *Server) agentToolListEvents(r *http.Request, principal auth.Principal, project Project, args map[string]any) agentToolResult {
	if err := s.authorizer.Authorize(r.Context(), principal, authz.PermissionNovelEventRead, authz.Resource{ProjectID: project.ID}); err != nil {
		return agentToolError("list_events", args, err)
	}
	limit := agentIntArg(args, "limit", 30, 1, 100)
	sourceID := agentReferenceStringArg(args, "sourceId")
	chapterID := agentReferenceStringArg(args, "chapterId")
	rows, err := s.db.Query(r.Context(), `
		SELECT id::text, COALESCE(source_id::text, ''), COALESCE(chapter_id::text, ''), chapter_index, event_index, sequence_no,
		       title, summary, COALESCE(event_type, ''), importance, review_status
		FROM novel_events
		WHERE project_id = $1
		  AND ($2 = '' OR source_id::text = $2)
		  AND ($3 = '' OR chapter_id::text = $3)
		ORDER BY sequence_no ASC, chapter_index ASC, event_index ASC
		LIMIT $4
	`, project.ID, sourceID, chapterID, limit)
	if err != nil {
		return agentToolError("list_events", args, err)
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, source, chapter, title, summary, eventType, reviewStatus string
		var chapterIndex, eventIndex, sequenceNo, importance int
		if err := rows.Scan(&id, &source, &chapter, &chapterIndex, &eventIndex, &sequenceNo, &title, &summary, &eventType, &importance, &reviewStatus); err != nil {
			return agentToolError("list_events", args, err)
		}
		items = append(items, map[string]any{
			"id":           id,
			"sourceId":     source,
			"chapterId":    strings.TrimSpace(chapter),
			"chapterIndex": chapterIndex,
			"eventIndex":   eventIndex,
			"sequenceNo":   sequenceNo,
			"title":        title,
			"summary":      summary,
			"eventType":    eventType,
			"importance":   importance,
			"reviewStatus": reviewStatus,
		})
	}
	if err := rows.Err(); err != nil {
		return agentToolError("list_events", args, err)
	}
	return agentToolOK("list_events", args, fmt.Sprintf("找到 %d 个事件。", len(items)), map[string]any{"items": items})
}

func (s *Server) agentToolListScripts(r *http.Request, principal auth.Principal, project Project, args map[string]any) agentToolResult {
	if err := s.authorizer.Authorize(r.Context(), principal, authz.PermissionScriptRead, authz.Resource{ProjectID: project.ID}); err != nil {
		return agentToolError("list_scripts", args, err)
	}
	limit := agentIntArg(args, "limit", 20, 1, 100)
	rows, err := s.db.Query(r.Context(), `
		SELECT s.id::text, s.title, s.status, s.current_version_id::text,
		       COALESCE(sv.version, 0), COALESCE(char_length(sv.content), 0),
		       s.id = p.active_script_id, s.created_at, s.updated_at
		FROM scripts s
		JOIN projects p ON p.id = s.project_id
		LEFT JOIN script_versions sv ON sv.id = s.current_version_id
		WHERE s.project_id = $1 AND COALESCE(s.status, 'active') <> 'archived'
		ORDER BY CASE WHEN s.id = p.active_script_id THEN 0 ELSE 1 END,
		         CASE WHEN s.status = 'active' THEN 0 ELSE 1 END,
		         s.updated_at DESC, s.created_at DESC
		LIMIT $2
	`, project.ID, limit)
	if err != nil {
		return agentToolError("list_scripts", args, err)
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, title, status string
		var versionID sql.NullString
		var version, contentLength int
		var isCurrent bool
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &title, &status, &versionID, &version, &contentLength, &isCurrent, &createdAt, &updatedAt); err != nil {
			return agentToolError("list_scripts", args, err)
		}
		items = append(items, map[string]any{
			"id":               id,
			"title":            title,
			"status":           status,
			"currentVersionId": stringPtrFromNull(versionID),
			"version":          version,
			"contentLength":    contentLength,
			"isCurrent":        isCurrent,
			"createdAt":        createdAt,
			"updatedAt":        updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return agentToolError("list_scripts", args, err)
	}
	return agentToolOK("list_scripts", args, fmt.Sprintf("找到 %d 个剧本。", len(items)), map[string]any{"items": items})
}

func (s *Server) agentToolListAssets(r *http.Request, principal auth.Principal, project Project, args map[string]any) agentToolResult {
	if err := s.authorizer.Authorize(r.Context(), principal, authz.PermissionAssetRead, authz.Resource{ProjectID: project.ID}); err != nil {
		return agentToolError("list_assets", args, err)
	}
	limit := agentIntArg(args, "limit", 30, 1, 100)
	rows, err := s.db.Query(r.Context(), `
		SELECT id::text, asset_type, name, description, status, review_status, stale_state, updated_at
		FROM canonical_assets
		WHERE project_id = $1
		ORDER BY updated_at DESC, created_at DESC
		LIMIT $2
	`, project.ID, limit)
	if err != nil {
		return agentToolError("list_assets", args, err)
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, assetType, name, description, status, reviewStatus, staleState string
		var updatedAt time.Time
		if err := rows.Scan(&id, &assetType, &name, &description, &status, &reviewStatus, &staleState, &updatedAt); err != nil {
			return agentToolError("list_assets", args, err)
		}
		items = append(items, map[string]any{
			"id":           id,
			"assetType":    assetType,
			"name":         name,
			"description":  description,
			"status":       status,
			"reviewStatus": reviewStatus,
			"staleState":   staleState,
			"updatedAt":    updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return agentToolError("list_assets", args, err)
	}
	return agentToolOK("list_assets", args, fmt.Sprintf("找到 %d 个资产。", len(items)), map[string]any{"items": items})
}

func (s *Server) agentToolListStoryboardShots(r *http.Request, principal auth.Principal, project Project, args map[string]any) agentToolResult {
	if err := s.authorizer.Authorize(r.Context(), principal, authz.PermissionProjectRead, authz.Resource{ProjectID: project.ID}); err != nil {
		return agentToolError("list_storyboard_shots", args, err)
	}
	limit := agentIntArg(args, "limit", 50, 1, 200)
	rows, err := s.db.Query(r.Context(), storyboardShotSelectSQL(`
		WHERE s.project_id = $1
		  AND s.deleted_at IS NULL
		ORDER BY s.shot_index ASC
		LIMIT $2
	`), project.ID, limit)
	if err != nil {
		return agentToolError("list_storyboard_shots", args, err)
	}
	defer rows.Close()
	items := make([]StoryboardShot, 0)
	for rows.Next() {
		item, err := scanStoryboardShot(rows)
		if err != nil {
			return agentToolError("list_storyboard_shots", args, err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return agentToolError("list_storyboard_shots", args, err)
	}
	return agentToolOK("list_storyboard_shots", args, fmt.Sprintf("找到 %d 个分镜镜头。", len(items)), map[string]any{"items": items})
}

func (s *Server) agentToolListWorkflowRuns(r *http.Request, principal auth.Principal, project Project, args map[string]any) agentToolResult {
	if err := s.authorizer.Authorize(r.Context(), principal, authz.PermissionWorkflowRead, authz.Resource{ProjectID: project.ID}); err != nil {
		return agentToolError("list_workflow_runs", args, err)
	}
	limit := agentIntArg(args, "limit", 20, 1, 100)
	rows, err := s.db.Query(r.Context(), workflowRunSelectSQL(`
		WHERE project_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`), project.ID, limit)
	if err != nil {
		return agentToolError("list_workflow_runs", args, err)
	}
	defer rows.Close()
	items := make([]WorkflowRun, 0)
	for rows.Next() {
		item, err := scanWorkflowRun(rows)
		if err != nil {
			return agentToolError("list_workflow_runs", args, err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return agentToolError("list_workflow_runs", args, err)
	}
	return agentToolOK("list_workflow_runs", args, fmt.Sprintf("找到 %d 个最近工作流。", len(items)), map[string]any{"items": items})
}

func (s *Server) agentToolStartProductionAction(r *http.Request, principal auth.Principal, project Project, userMessage AgentMessage, args map[string]any) agentToolResult {
	action := agentStringArg(args, "action")
	if action == "" {
		return agentToolError("start_production_action", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "action is required"))
	}
	permission, ok := productionActionPermission(action)
	if !ok {
		return agentToolError("start_production_action", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "production action is not supported"))
	}
	if err := s.authorizer.Authorize(r.Context(), principal, permission, authz.Resource{ProjectID: project.ID}); err != nil {
		return agentToolError("start_production_action", args, err)
	}
	options := cleanAgentReferenceOptions(agentMapArg(args, "options"))
	if _, ok := options["chapterIds"]; !ok {
		if chapterIDs := agentReferenceStringSliceArg(args, "chapterIds"); len(chapterIDs) > 0 {
			options["chapterIds"] = chapterIDs
		}
	}
	if _, ok := options["force"]; !ok {
		if force, exists := agentBoolArg(args, "force"); exists {
			options["force"] = force
		}
	}
	req := ProductionActionRequest{
		Action:   action,
		SourceID: firstNonEmpty(agentReferenceStringArg(args, "sourceId"), productionOptionString(options, "sourceId")),
		ScriptID: firstNonEmpty(agentReferenceStringArg(args, "scriptId"), productionOptionString(options, "scriptId")),
		Options:  options,
	}
	if action == "generate_derived_asset_images" {
		result, err := s.createDerivedAssetBatchForAgentAction(r, principal, project, userMessage, req)
		if err != nil {
			return agentToolError("start_production_action", args, err)
		}
		return agentToolOK("start_production_action", args, fmt.Sprintf("已启动 %s，工作流 %s 当前状态 %s。", action, result.WorkflowRun.ID, result.WorkflowRun.Status), map[string]any{
			"action": action, "workflowRunId": result.WorkflowRun.ID, "workflowType": derivedAssetBatchWorkflowType,
			"status": result.WorkflowRun.Status, "input": options, "sourceMessageId": userMessage.ID,
			"operationId": result.OperationID, "derivedAssets": result.Batch,
		})
	}
	spec, err := s.productionActionWorkflowCore(r, project, action, req)
	if err != nil {
		return agentToolError("start_production_action", args, err)
	}
	run, err := s.startProjectWorkflowCore(r.Context(), principal, project, spec.WorkflowType, spec.Input, spec.WorkflowFunc)
	if err != nil {
		return agentToolError("start_production_action", args, err)
	}
	data := map[string]any{
		"action":          action,
		"workflowRunId":   run.ID,
		"workflowType":    spec.WorkflowType,
		"status":          run.Status,
		"input":           spec.Input,
		"sourceMessageId": userMessage.ID,
	}
	if spec.Note != "" {
		data["note"] = spec.Note
	}
	summary := fmt.Sprintf("已启动 %s，工作流 %s 当前状态 %s。", action, run.ID, run.Status)
	return agentToolOK("start_production_action", args, summary, data)
}

func (s *Server) agentToolCancelWorkflow(r *http.Request, principal auth.Principal, project Project, args map[string]any) agentToolResult {
	if err := s.authorizer.Authorize(r.Context(), principal, authz.PermissionWorkflowCancel, authz.Resource{ProjectID: project.ID}); err != nil {
		return agentToolError("cancel_workflow", args, err)
	}
	runID := agentReferenceStringArg(args, "workflowRunId")
	if runID == "" {
		return agentToolError("cancel_workflow", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "workflowRunId is required"))
	}
	item, err := scanWorkflowRun(s.db.QueryRow(r.Context(), workflowRunSelectSQL(`
		WHERE id = $1 AND project_id = $2
	`), runID, project.ID))
	if err != nil {
		return agentToolError("cancel_workflow", args, err)
	}
	if isTerminalWorkflowStatus(item.Status) {
		return agentToolOK("cancel_workflow", args, fmt.Sprintf("工作流 %s 已是终态 %s。", item.ID, item.Status), map[string]any{"workflowRun": item})
	}
	reason := agentStringArg(args, "reason")
	if reason == "" {
		reason = "AI assistant requested cancellation"
	}
	if err := workflows.MarkWorkflowCancelling(r.Context(), s.db, item.ID, reason); err != nil {
		return agentToolError("cancel_workflow", args, err)
	}
	if s.temporal != nil {
		if err := s.temporal.CancelWorkflow(r.Context(), item.TemporalWorkflowID, ""); err != nil {
			_ = s.insertWorkflowCancelWarning(r.Context(), item, reason, err)
		}
	}
	updated, err := scanWorkflowRun(s.db.QueryRow(r.Context(), workflowRunSelectSQL(`
		WHERE id = $1
	`), item.ID))
	if err != nil {
		return agentToolError("cancel_workflow", args, err)
	}
	return agentToolOK("cancel_workflow", args, fmt.Sprintf("已请求取消工作流 %s。", updated.ID), map[string]any{"workflowRun": updated})
}

func streamScriptAgentText(r *http.Request, project Project, prompt, templateKey, promptHash, idempotencyKey string, jsonResponse bool) (string, provider.GatewayTextResponse, error) {
	var streamed strings.Builder
	input := map[string]any{"prompt": prompt}
	if jsonResponse {
		input["responseFormat"] = "json"
	}
	resp, err := provider.NewGatewayClientFromEnv().StreamText(r.Context(), provider.GatewayTextRequest{
		OrganizationID:    project.OrganizationID,
		WorkspaceID:       project.WorkspaceID,
		ProjectID:         project.ID,
		ModelProfileKey:   project.ScriptModelProfileKey,
		PromptTemplateKey: templateKey,
		PromptHash:        promptHash,
		PromptSource:      "inline",
		Input:             mustMarshal(input),
		Options: provider.GatewayTextOptions{
			IdempotencyKey: idempotencyKey,
		},
	}, func(delta provider.GatewayTextDelta) error {
		streamed.WriteString(delta.Text)
		return nil
	})
	if err != nil {
		return "", provider.GatewayTextResponse{}, err
	}
	text := strings.TrimSpace(streamed.String())
	if text == "" {
		text = strings.TrimSpace(resp.Output.Text)
	}
	if text == "" {
		text = strings.TrimSpace(string(resp.Output.Raw))
	}
	return text, resp, nil
}

func agentToolOK(name string, args map[string]any, summary string, data map[string]any) agentToolResult {
	return agentToolResult{
		Name:      name,
		Label:     scriptAgentToolLabel(name),
		Status:    "succeeded",
		Summary:   summary,
		Arguments: args,
		Data:      data,
	}
}

func agentToolError(name string, args map[string]any, err error) agentToolResult {
	var appErr apiError
	code := "TOOL_FAILED"
	message := err.Error()
	retryable := false
	switch {
	case errors.As(err, &appErr):
		code = appErr.Code
		message = appErr.Message
		retryable = appErr.Retryable
	case errors.Is(err, pgx.ErrNoRows):
		code = "NOT_FOUND"
		message = "resource was not found"
	case errors.Is(err, authz.ErrAccessDenied):
		code = "ACCESS_DENIED"
		message = "access denied"
	}
	return agentToolResult{
		Name:         name,
		Label:        scriptAgentToolLabel(name),
		Status:       "failed",
		Summary:      message,
		Arguments:    args,
		Retryable:    retryable,
		NextActions:  agentToolErrorNextActions(name, code),
		ErrorCode:    code,
		ErrorMessage: message,
	}
}

func agentToolErrorNextActions(toolName, code string) []agentToolNextAction {
	code = strings.ToUpper(strings.TrimSpace(code))
	toolName = strings.TrimSpace(toolName)
	switch code {
	case "ACCESS_DENIED", "FORBIDDEN":
		return []agentToolNextAction{{Label: "联系管理员授予当前项目所需权限", Reason: "当前用户没有执行该工具的权限"}}
	case "NOT_FOUND":
		return []agentToolNextAction{{Label: "刷新项目数据后重新选择目标", Reason: "目标资源不存在或已被删除"}}
	case "VALIDATION_FAILED":
		return []agentToolNextAction{{Label: "检查工具参数并重新提交", Reason: "当前工具输入不符合后端 schema"}}
	case "TEMPORAL_UNAVAILABLE":
		return []agentToolNextAction{{Label: "检查 Temporal 服务后重试", Reason: "长任务调度服务不可用"}}
	case "PROVIDER_SERVICE_UNAVAILABLE", "PROVIDER_GATEWAY_ERROR", "UPSTREAM_TIMEOUT":
		return []agentToolNextAction{{Label: "检查供应商网关、模型绑定和超时设置后重试", Tool: "provider.list_status", Reason: "上游供应商或 Provider Gateway 当前不可用"}}
	case "NO_TARGET_SHOTS":
		return []agentToolNextAction{{Label: "先确认分镜和镜头生产状态", Tool: "shot.status", Reason: "没有符合条件的目标镜头"}}
	case "SHOT_IMAGES_NOT_READY":
		return []agentToolNextAction{{Label: "先生成缺失镜头图片", Tool: "shot.generate_missing_images", Reason: "镜头视频生成依赖图片完成"}}
	case "SHOT_ASSET_REQUIREMENT_REVIEW_REQUIRED":
		return []agentToolNextAction{{Label: "重新校验镜头资产需求", Tool: "shot_asset.review_requirements", Reason: "衍生资产只能基于已确认的镜头需求生成", Arguments: map[string]any{"reviewStatus": "approved"}}}
	case "SHOT_ASSET_REQUIREMENT_TYPE_MISMATCH", "CANONICAL_ASSET_ARCHIVED", "CANONICAL_ASSET_STALE":
		return []agentToolNextAction{{Label: "检查需求和可用资产后修正关联", Tool: "shot_asset.list_requirements", Reason: "当前镜头资产需求与核心资产状态不一致", Arguments: map[string]any{"reviewStatus": "needs_edit"}}}
	case "STORYBOARD_REGENERATION_REQUIRED":
		return []agentToolNextAction{{Label: "查看当前分镜并重新生成受影响分集", Tool: "storyboard.list", Reason: "镜头资产需求依赖的上游分镜已变化"}}
	case "AGENT_STEP_BLOCKED":
		return []agentToolNextAction{{Label: "查看步骤预览里的阻塞原因并调整任务约束", Reason: "监督层阻止了该步骤执行"}}
	case "AGENT_VERIFIER_FAILED":
		return []agentToolNextAction{{Label: "刷新目标状态并重试该步骤", Reason: "工具返回成功但执行后校验未通过"}}
	default:
		return []agentToolNextAction{{Label: "查看执行详情并根据错误信息重试", Reason: "工具执行失败"}}
	}
}

func scriptAgentToolLabel(name string) string {
	labels := map[string]string{
		"get_project_status":             "读取项目状态",
		"list_sources":                   "列出原文",
		"list_source_chapters":           "列出分集章节",
		"list_events":                    "列出事件",
		"list_scripts":                   "列出剧本",
		"list_assets":                    "列出资产",
		"asset.get":                      "读取资产卡",
		"asset.revise_prompt":            "修订资产提示词",
		"shot_asset.list_requirements":   "读取镜头资产需求",
		"shot_asset.review_requirements": "审核镜头资产需求",
		"shot_asset.update_requirement":  "修正镜头资产需求",
		"shot_asset.skip_requirement":    "跳过镜头资产需求",
		"list_storyboard_shots":          "列出分镜",
		"list_workflow_runs":             "列出任务",
		"start_production_action":        "启动生产动作",
		"cancel_workflow":                "取消任务",
	}
	if label, ok := labels[name]; ok {
		return label
	}
	return name
}

func scriptAgentToolRequiresMutation(name string) bool {
	return name == "start_production_action" || name == "cancel_workflow"
}

func scriptAgentAllowsMutation(content string) bool {
	text := strings.ToLower(strings.TrimSpace(content))
	if text == "" {
		return false
	}
	verbs := []string{
		"开始", "执行", "生成", "提取", "取消", "重跑", "创建", "导入", "解析", "制作", "运行", "应用", "修复",
		"start", "run", "generate", "extract", "cancel", "create", "execute", "apply", "fix", "produce",
	}
	for _, verb := range verbs {
		if strings.Contains(text, verb) {
			return true
		}
	}
	return false
}

func agentStringArg(args map[string]any, key string) string {
	value, ok := args[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	}
}

func agentReferenceStringArg(args map[string]any, key string) string {
	return cleanAgentReferenceString(agentStringArg(args, key))
}

func agentReferenceStringFromAny(value any) string {
	return cleanAgentReferenceString(stringValueFromAny(value))
}

func cleanAgentReferenceString(value string) string {
	trimmed := strings.TrimSpace(value)
	if isAgentReferencePlaceholder(trimmed) {
		return ""
	}
	if _, err := uuid.Parse(trimmed); err != nil {
		return ""
	}
	return trimmed
}

func isAgentReferencePlaceholder(value string) bool {
	trimmed := strings.TrimSpace(value)
	return (strings.Contains(trimmed, "{{") && strings.Contains(trimmed, "}}")) ||
		(strings.HasPrefix(trimmed, "<") && strings.HasSuffix(trimmed, ">"))
}

var agentUUIDArgumentKeys = map[string]bool{
	"accountId":       true,
	"artifactId":      true,
	"assetId":         true,
	"chapterId":       true,
	"clipId":          true,
	"episodeId":       true,
	"finalVideoId":    true,
	"fixId":           true,
	"itemId":          true,
	"modelId":         true,
	"planId":          true,
	"promptVersionId": true,
	"providerModelId": true,
	"requirementId":   true,
	"scriptId":        true,
	"scriptSceneId":   true,
	"scriptVersionId": true,
	"shotId":          true,
	"sourceId":        true,
	"templateId":      true,
	"timelineId":      true,
	"versionId":       true,
	"workflowRunId":   true,
}

var agentUUIDArrayArgumentKeys = map[string]bool{
	"artifactIds":      true,
	"assetIds":         true,
	"chapterIds":       true,
	"eventIds":         true,
	"scriptEpisodeIds": true,
	"shotIds":          true,
	"workflowRunIds":   true,
}

func validateAgentRuntimeArguments(args map[string]any) error {
	return validateAgentRuntimeValue(args, "args", "")
}

func validateAgentRuntimeValue(value any, path, key string) error {
	switch typed := value.(type) {
	case map[string]any:
		for childKey, child := range typed {
			if err := validateAgentRuntimeValue(child, path+"."+childKey, childKey); err != nil {
				return err
			}
		}
	case []any:
		for index, child := range typed {
			if err := validateAgentRuntimeValue(child, fmt.Sprintf("%s[%d]", path, index), key); err != nil {
				return err
			}
		}
	case []string:
		for index, child := range typed {
			if err := validateAgentRuntimeValue(child, fmt.Sprintf("%s[%d]", path, index), key); err != nil {
				return err
			}
		}
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil
		}
		if isAgentPlanningPlaceholder(trimmed) {
			return newAPIError(http.StatusUnprocessableEntity, "AGENT_ARGUMENT_UNRESOLVED", fmt.Sprintf("工具参数 %s 仍是规划占位文本，请改用真实值或语义选择条件", path))
		}
		if agentUUIDArgumentKeys[key] || agentUUIDArrayArgumentKeys[key] {
			if _, err := uuid.Parse(trimmed); err != nil {
				return newAPIError(http.StatusUnprocessableEntity, "AGENT_ARGUMENT_INVALID", fmt.Sprintf("工具参数 %s 必须是有效 UUID", path))
			}
		}
	}
	return nil
}

func isAgentPlanningPlaceholder(value string) bool {
	trimmed := strings.TrimSpace(value)
	if strings.Contains(trimmed, "{{") && strings.Contains(trimmed, "}}") {
		return false // Prompt templates may intentionally contain Go-style placeholders.
	}
	if !strings.HasPrefix(trimmed, "<") || !strings.HasSuffix(trimmed, ">") {
		return false
	}
	lower := strings.ToLower(trimmed)
	for _, marker := range []string{
		"上一步", "返回", "读取", "完整正文", "实际", "待填", "填入", "替换", "sourceid", "chapterid", "scriptid", "workflowrunid",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func agentIntArg(args map[string]any, key string, fallback, min, max int) int {
	value, ok := args[key]
	if !ok {
		return fallback
	}
	out := fallback
	switch typed := value.(type) {
	case int:
		out = typed
	case int64:
		out = int(typed)
	case float64:
		out = int(typed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			out = int(parsed)
		}
	case string:
		if parsed, err := fmt.Sscanf(strings.TrimSpace(typed), "%d", &out); err != nil || parsed != 1 {
			out = fallback
		}
	}
	if out < min {
		return min
	}
	if out > max {
		return max
	}
	return out
}

func agentMapArg(args map[string]any, key string) map[string]any {
	value, ok := args[key]
	if !ok || value == nil {
		return map[string]any{}
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func agentStringSliceArg(args map[string]any, key string) []string {
	value, ok := args[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if trimmed := strings.TrimSpace(fmt.Sprintf("%v", item)); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case string:
		parts := strings.Split(typed, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return nil
		}
		var out []string
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil
		}
		return out
	}
}

func agentReferenceStringSliceArg(args map[string]any, key string) []string {
	return cleanAgentReferenceStringSlice(agentStringSliceArg(args, key))
}

func cleanAgentReferenceStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if cleaned := cleanAgentReferenceString(value); cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return out
}

func cleanAgentReferenceOptions(options map[string]any) map[string]any {
	if options == nil {
		return map[string]any{}
	}
	out := cloneMap(options)
	for _, key := range []string{
		"sourceId",
		"scriptId",
		"chapterId",
		"planId",
		"scriptVersionId",
		"workflowRunId",
		"timelineId",
		"assetId",
		"requirementId",
		"shotId",
		"modelId",
		"accountId",
		"providerModelId",
		"finalVideoId",
		"versionId",
		"templateId",
		"fixId",
		"itemId",
	} {
		if cleaned := agentReferenceStringFromAny(out[key]); cleaned != "" {
			out[key] = cleaned
		} else if isAgentReferencePlaceholder(stringValueFromAny(out[key])) {
			delete(out, key)
		}
	}
	for _, key := range []string{"chapterIds", "eventIds", "shotIds", "assetIds", "workflowRunIds"} {
		if cleaned := cleanAgentReferenceStringSlice(stringSliceFromAny(out[key])); len(cleaned) > 0 {
			out[key] = cleaned
		} else if agentReferenceSliceHasPlaceholder(out[key]) {
			delete(out, key)
		}
	}
	return out
}

func agentReferenceSliceHasPlaceholder(value any) bool {
	for _, item := range stringSliceFromAny(value) {
		if isAgentReferencePlaceholder(strings.TrimSpace(item)) {
			return true
		}
	}
	return false
}

func agentBoolArg(args map[string]any, key string) (bool, bool) {
	value, ok := args[key]
	if !ok || value == nil {
		return false, false
	}
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes", "y", "force":
			return true, true
		case "false", "0", "no", "n":
			return false, true
		}
	}
	return false, false
}

func scriptAgentToolResultsSummary(results []agentToolResult) []map[string]any {
	out := make([]map[string]any, 0, len(results))
	for _, result := range results {
		item := map[string]any{
			"name":    result.Name,
			"label":   result.Label,
			"status":  result.Status,
			"summary": result.Summary,
		}
		if result.ErrorCode != "" {
			item["errorCode"] = result.ErrorCode
		}
		if len(result.Data) > 0 {
			keys := make([]string, 0, len(result.Data))
			for key := range result.Data {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			item["dataKeys"] = keys
		}
		out = append(out, item)
	}
	return out
}

func scriptAgentProviderCallIDs(responses ...provider.GatewayTextResponse) []string {
	out := make([]string, 0, len(responses))
	seen := map[string]bool{}
	for _, resp := range responses {
		id := strings.TrimSpace(resp.ProviderCallID)
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func errorStringOrEmpty(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
