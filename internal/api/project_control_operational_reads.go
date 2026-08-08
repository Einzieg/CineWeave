package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

const projectOperationalReadMaximumPageSize = 200

type projectSummaryActionResult struct {
	ProductionStatus  ProductionStatus       `json:"productionStatus"`
	ProjectGapSummary agentProjectGapSummary `json:"projectGapSummary"`
}

type storyboardListActionInput struct {
	ScriptEpisodeID string `json:"scriptEpisodeId"`
	WorkflowRunID   string `json:"workflowRunId"`
	Limit           int    `json:"limit"`
	Cursor          string `json:"cursor"`
}

type storyboardListActionPage struct {
	Items      []StoryboardShot `json:"items"`
	Limit      int              `json:"limit"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

type workflowRunListActionInput struct {
	Status       string `json:"status"`
	Limit        int    `json:"limit"`
	Cursor       string `json:"cursor"`
	ActivityView bool   `json:"-"`
	ActorUserID  string `json:"-"`
}

type workflowRunListActionPage struct {
	Items      []WorkflowRun `json:"items"`
	Limit      int           `json:"limit"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

type workflowRunChildrenActionInput struct {
	WorkflowRunID     string `json:"workflowRunId"`
	Limit             int    `json:"limit"`
	Cursor            string `json:"cursor"`
	IncludePreviewURL bool   `json:"includePreviewUrl"`
	PreviewExpires    int    `json:"previewExpiresSeconds"`
}

type workflowNodeListActionPage struct {
	Items      []WorkflowNodeRun `json:"items"`
	Limit      int               `json:"limit"`
	NextCursor string            `json:"nextCursor,omitempty"`
}

type workflowShotListActionPage struct {
	Items      []StoryboardShot `json:"items"`
	Limit      int              `json:"limit"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

type reviewItemListActionInput struct {
	Status     string `json:"status"`
	Severity   string `json:"severity"`
	Category   string `json:"category"`
	EntityType string `json:"entityType"`
	Limit      int    `json:"limit"`
	Cursor     string `json:"cursor"`
}

type reviewItemListActionPage struct {
	Items      []ReviewItem `json:"items"`
	Limit      int          `json:"limit"`
	NextCursor string       `json:"nextCursor,omitempty"`
}

type providerStatusActionResult struct {
	Accounts                        int                 `json:"accounts"`
	ActiveAccounts                  int                 `json:"activeAccounts"`
	DisabledAccounts                int                 `json:"disabledAccounts"`
	Models                          int                 `json:"models"`
	ActiveModels                    int                 `json:"activeModels"`
	DisabledModels                  int                 `json:"disabledModels"`
	RecentCalls24h                  int                 `json:"recentCalls24h"`
	FailedCalls24h                  int                 `json:"failedCalls24h"`
	ScriptProfileKey                string              `json:"scriptProfileKey"`
	ImageProfileKey                 string              `json:"imageProfileKey"`
	VideoProfileKey                 string              `json:"videoProfileKey"`
	ProductionProfile               string              `json:"productionProfile"`
	VideoCapabilityVariants         []map[string]any    `json:"videoCapabilityVariants"`
	VideoCapabilityErrors           []map[string]string `json:"videoCapabilityErrors"`
	PendingVideoCapabilityApprovals int                 `json:"pendingVideoCapabilityApprovals"`
}

type shotStatusActionInput struct {
	ScriptEpisodeID   string `json:"scriptEpisodeId"`
	ScriptSceneID     string `json:"scriptSceneId"`
	WorkflowRunID     string `json:"workflowRunId"`
	StoryboardPlanID  string `json:"storyboardPlanId"`
	IncludePreviewURL bool   `json:"includePreviewUrl"`
	Limit             int    `json:"limit"`
}

type shotStatusActionResult struct {
	Status  ShotProductionStatus `json:"status"`
	HasMore bool                 `json:"hasMore"`
	Total   int                  `json:"total"`
}

func (s *Server) readProjectSummaryAction(ctx context.Context, project Project) (projectSummaryActionResult, error) {
	status, err := s.productionStatus(requestWithContext(ctx), project)
	if err != nil {
		return projectSummaryActionResult{}, err
	}
	gaps, err := s.agentProjectGapSummary(ctx, project, status)
	if err != nil {
		return projectSummaryActionResult{}, err
	}
	return projectSummaryActionResult{ProductionStatus: status, ProjectGapSummary: gaps}, nil
}

func (s *Server) listStoryboardShotsAction(ctx context.Context, project Project, input storyboardListActionInput) (storyboardListActionPage, error) {
	limit, err := normalizeProjectControlPageLimit(input.Limit, 50, projectOperationalReadMaximumPageSize)
	if err != nil {
		return storyboardListActionPage{}, err
	}
	offset, err := decodeProjectControlOffsetCursor(input.Cursor)
	if err != nil {
		return storyboardListActionPage{}, err
	}
	input.ScriptEpisodeID = strings.TrimSpace(input.ScriptEpisodeID)
	input.WorkflowRunID = strings.TrimSpace(input.WorkflowRunID)
	if input.ScriptEpisodeID != "" && uuid.Validate(input.ScriptEpisodeID) != nil {
		return storyboardListActionPage{}, controlValidationError("scriptEpisodeId 无效")
	}
	if input.WorkflowRunID != "" {
		if uuid.Validate(input.WorkflowRunID) != nil {
			return storyboardListActionPage{}, controlValidationError("workflowRunId 无效")
		}
		if _, err := s.workflowRunForProjectContext(ctx, project.ID, input.WorkflowRunID); err != nil {
			return storyboardListActionPage{}, err
		}
	}
	rows, err := s.db.Query(ctx, storyboardShotSelectSQL(`
		WHERE s.project_id = $1
		  AND s.deleted_at IS NULL
		  AND ($2 = '' OR s.script_episode_id = NULLIF($2, '')::uuid)
		  AND ($3 = '' OR s.workflow_run_id = NULLIF($3, '')::uuid)
		  AND (
		    s.production_generation_id IS NULL
		    OR s.production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
		  )
		ORDER BY s.episode_index ASC NULLS LAST, s.episode_shot_index ASC NULLS LAST, s.shot_index ASC, s.id ASC
		LIMIT $4 OFFSET $5
	`), project.ID, input.ScriptEpisodeID, input.WorkflowRunID, limit+1, offset)
	if err != nil {
		return storyboardListActionPage{}, err
	}
	defer rows.Close()
	items := make([]StoryboardShot, 0, limit+1)
	for rows.Next() {
		item, err := scanStoryboardShot(rows)
		if err != nil {
			return storyboardListActionPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return storyboardListActionPage{}, err
	}
	page := storyboardListActionPage{Items: items, Limit: limit}
	if len(items) > limit {
		page.Items = items[:limit]
		page.NextCursor, err = encodeProjectControlOffsetCursor(offset + limit)
		if err != nil {
			return storyboardListActionPage{}, err
		}
	}
	return page, nil
}

func (s *Server) listProjectWorkflowRunsAction(ctx context.Context, project Project, input workflowRunListActionInput) (workflowRunListActionPage, error) {
	limit, err := normalizeProjectControlPageLimit(input.Limit, 20, 100)
	if err != nil {
		return workflowRunListActionPage{}, err
	}
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if status == "" {
		status = "all"
	}
	if status != "active" && status != "terminal" && status != "all" {
		return workflowRunListActionPage{}, controlValidationError("status 必须是 active、terminal 或 all")
	}
	var cursorCreatedAt any
	var cursorID any
	if strings.TrimSpace(input.Cursor) != "" {
		cursor, err := decodeWorkflowRunCursor(input.Cursor)
		if err != nil {
			return workflowRunListActionPage{}, controlValidationError("cursor 无效")
		}
		cursorCreatedAt = cursor.CreatedAt
		cursorID = cursor.ID
	}
	rows, err := s.db.Query(ctx, workflowRunSelectSQL(`
		WHERE organization_id = $1
		  AND project_id = $2
		  AND (
		    $3 = 'all'
		    OR ($3 = 'active' AND status NOT IN ('succeeded', 'partial_succeeded', 'completed', 'failed', 'cancelled', 'skipped'))
		    OR ($3 = 'terminal' AND status IN ('succeeded', 'partial_succeeded', 'completed', 'failed', 'cancelled', 'skipped'))
		  )
		  AND (
		    $4::timestamptz IS NULL
		    OR created_at < $4::timestamptz
		    OR (created_at = $4::timestamptz AND id < $5::uuid)
		  )
		  AND (
		    NOT $6::boolean
		    OR status NOT IN ('succeeded', 'partial_succeeded', 'completed', 'failed', 'cancelled', 'skipped')
		    OR COALESCE(completed_at, cancelled_at, updated_at) > COALESCE((
		      SELECT cleared_terminal_through
		      FROM workflow_activity_views
		      WHERE organization_id = $1 AND project_id = $2 AND user_id = NULLIF($7, '')::uuid
		    ), '-infinity'::timestamptz)
		  )
		ORDER BY created_at DESC, id DESC
		LIMIT $8
	`), project.OrganizationID, project.ID, status, cursorCreatedAt, cursorID, input.ActivityView, input.ActorUserID, limit+1)
	if err != nil {
		return workflowRunListActionPage{}, err
	}
	defer rows.Close()
	items := make([]WorkflowRun, 0, limit+1)
	for rows.Next() {
		item, err := scanWorkflowRun(rows)
		if err != nil {
			return workflowRunListActionPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return workflowRunListActionPage{}, err
	}
	page := workflowRunListActionPage{Items: items, Limit: limit}
	if len(items) > limit {
		page.Items = items[:limit]
		page.NextCursor = encodeWorkflowRunCursor(page.Items[len(page.Items)-1])
	}
	return page, nil
}

func (s *Server) listWorkflowNodesAction(ctx context.Context, project Project, input workflowRunChildrenActionInput) (workflowNodeListActionPage, error) {
	if err := validateWorkflowRunChildrenActionInput(&input, false); err != nil {
		return workflowNodeListActionPage{}, err
	}
	if _, err := s.workflowRunForProjectContext(ctx, project.ID, input.WorkflowRunID); err != nil {
		return workflowNodeListActionPage{}, err
	}
	offset, err := decodeProjectControlOffsetCursor(input.Cursor)
	if err != nil {
		return workflowNodeListActionPage{}, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, organization_id, project_id, workflow_run_id, node_key, node_type, status, input, output,
		       retry_count, error_code, error_message, started_at, completed_at, created_at, revision, updated_at
		FROM workflow_node_runs
		WHERE workflow_run_id = $1
		ORDER BY created_at ASC, id ASC
		LIMIT $2 OFFSET $3
	`, input.WorkflowRunID, input.Limit+1, offset)
	if err != nil {
		return workflowNodeListActionPage{}, err
	}
	defer rows.Close()
	items := make([]WorkflowNodeRun, 0, input.Limit+1)
	for rows.Next() {
		var item WorkflowNodeRun
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.ProjectID, &item.WorkflowRunID, &item.NodeKey,
			&item.NodeType, &item.Status, &item.Input, &item.Output, &item.RetryCount, &item.ErrorCode,
			&item.ErrorMessage, &item.StartedAt, &item.CompletedAt, &item.CreatedAt, &item.Revision, &item.UpdatedAt); err != nil {
			return workflowNodeListActionPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return workflowNodeListActionPage{}, err
	}
	page := workflowNodeListActionPage{Items: items, Limit: input.Limit}
	if len(items) > input.Limit {
		page.Items = items[:input.Limit]
		page.NextCursor, err = encodeProjectControlOffsetCursor(offset + input.Limit)
		if err != nil {
			return workflowNodeListActionPage{}, err
		}
	}
	return page, nil
}

func (s *Server) listWorkflowShotsAction(ctx context.Context, project Project, input workflowRunChildrenActionInput) (workflowShotListActionPage, error) {
	if err := validateWorkflowRunChildrenActionInput(&input, true); err != nil {
		return workflowShotListActionPage{}, err
	}
	if _, err := s.workflowRunForProjectContext(ctx, project.ID, input.WorkflowRunID); err != nil {
		return workflowShotListActionPage{}, err
	}
	if input.IncludePreviewURL && s.storage == nil {
		return workflowShotListActionPage{}, apiError{Status: http.StatusServiceUnavailable, Code: "STORAGE_UNAVAILABLE", Message: "对象存储尚未配置", Retryable: true}
	}
	offset, err := decodeProjectControlOffsetCursor(input.Cursor)
	if err != nil {
		return workflowShotListActionPage{}, err
	}
	rows, err := s.db.Query(ctx, storyboardShotSelectSQL(`
		WHERE s.workflow_run_id = $1
		  AND s.project_id = $2
		  AND s.deleted_at IS NULL
		ORDER BY s.episode_index ASC NULLS LAST, s.episode_shot_index ASC NULLS LAST, s.shot_index ASC, s.id ASC
		LIMIT $3 OFFSET $4
	`), input.WorkflowRunID, project.ID, input.Limit+1, offset)
	if err != nil {
		return workflowShotListActionPage{}, err
	}
	defer rows.Close()
	items := make([]StoryboardShot, 0, input.Limit+1)
	request := requestWithContext(ctx)
	for rows.Next() {
		item, err := scanStoryboardShot(rows)
		if err != nil {
			return workflowShotListActionPage{}, err
		}
		if input.IncludePreviewURL {
			if err := s.attachShotPreviewURLs(request, &item, previewURLExpiry(input.PreviewExpires)); err != nil {
				return workflowShotListActionPage{}, err
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return workflowShotListActionPage{}, err
	}
	page := workflowShotListActionPage{Items: items, Limit: input.Limit}
	if len(items) > input.Limit {
		page.Items = items[:input.Limit]
		page.NextCursor, err = encodeProjectControlOffsetCursor(offset + input.Limit)
		if err != nil {
			return workflowShotListActionPage{}, err
		}
	}
	return page, nil
}

func validateWorkflowRunChildrenActionInput(input *workflowRunChildrenActionInput, preview bool) error {
	input.WorkflowRunID = strings.TrimSpace(input.WorkflowRunID)
	if uuid.Validate(input.WorkflowRunID) != nil {
		return controlValidationError("workflowRunId 无效")
	}
	limit, err := normalizeProjectControlPageLimit(input.Limit, 100, projectOperationalReadMaximumPageSize)
	if err != nil {
		return err
	}
	input.Limit = limit
	if preview {
		if input.PreviewExpires == 0 {
			input.PreviewExpires = 900
		}
		if input.PreviewExpires < 60 || input.PreviewExpires > 3600 {
			return controlValidationError("previewExpiresSeconds 必须在 60 到 3600 之间")
		}
	}
	return nil
}

func (s *Server) listReviewItemsAction(ctx context.Context, project Project, input reviewItemListActionInput) (reviewItemListActionPage, error) {
	limit, err := normalizeProjectControlPageLimit(input.Limit, 50, projectOperationalReadMaximumPageSize)
	if err != nil {
		return reviewItemListActionPage{}, err
	}
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if status == "" {
		status = "open"
	}
	if status != "all" && status != "open" && status != "resolved" && status != "ignored" {
		return reviewItemListActionPage{}, controlValidationError("status 必须是 open、resolved、ignored 或 all")
	}
	offset, err := decodeProjectControlOffsetCursor(input.Cursor)
	if err != nil {
		return reviewItemListActionPage{}, err
	}
	rows, err := s.db.Query(ctx, reviewItemSelectSQL(`
		WHERE project_id = $1
		  AND ($2 = 'all' OR status = $2)
		  AND ($3 = '' OR severity = $3)
		  AND ($4 = '' OR category = $4)
		  AND ($5 = '' OR entity_type = $5)
		ORDER BY created_at DESC, id DESC
		LIMIT $6 OFFSET $7
	`), project.ID, status, strings.TrimSpace(input.Severity), strings.TrimSpace(input.Category), strings.TrimSpace(input.EntityType), limit+1, offset)
	if err != nil {
		return reviewItemListActionPage{}, err
	}
	defer rows.Close()
	items := make([]ReviewItem, 0, limit+1)
	for rows.Next() {
		item, err := scanReviewItem(rows)
		if err != nil {
			return reviewItemListActionPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return reviewItemListActionPage{}, err
	}
	page := reviewItemListActionPage{Items: items, Limit: limit}
	if len(items) > limit {
		page.Items = items[:limit]
		page.NextCursor, err = encodeProjectControlOffsetCursor(offset + limit)
		if err != nil {
			return reviewItemListActionPage{}, err
		}
	}
	return page, nil
}

func (s *Server) readProviderStatusAction(ctx context.Context, project Project) (providerStatusActionResult, error) {
	var result providerStatusActionResult
	err := s.db.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM provider_accounts WHERE organization_id = $1),
		  (SELECT count(*) FROM provider_accounts WHERE organization_id = $1 AND status = 'active'),
		  (SELECT count(*) FROM provider_accounts WHERE organization_id = $1 AND status = 'disabled'),
		  (SELECT count(*) FROM provider_models m JOIN provider_accounts a ON a.id = m.provider_account_id WHERE a.organization_id = $1),
		  (SELECT count(*) FROM provider_models m JOIN provider_accounts a ON a.id = m.provider_account_id WHERE a.organization_id = $1 AND m.status = 'active' AND a.status = 'active'),
		  (SELECT count(*) FROM provider_models m JOIN provider_accounts a ON a.id = m.provider_account_id WHERE a.organization_id = $1 AND m.status = 'disabled'),
		  (SELECT count(*) FROM provider_call_logs WHERE organization_id = $1 AND created_at >= now() - interval '24 hours'),
		  (SELECT count(*) FROM provider_call_logs WHERE organization_id = $1 AND created_at >= now() - interval '24 hours' AND status = 'failed')
	`, project.OrganizationID).Scan(&result.Accounts, &result.ActiveAccounts, &result.DisabledAccounts, &result.Models,
		&result.ActiveModels, &result.DisabledModels, &result.RecentCalls24h, &result.FailedCalls24h)
	if err != nil {
		return providerStatusActionResult{}, err
	}
	result.ScriptProfileKey = project.ScriptModelProfileKey
	result.ImageProfileKey = project.ImageModelProfileKey
	result.VideoProfileKey = project.VideoModelProfileKey
	result.ProductionProfile = project.VideoProductionBinding.ProfileKey
	result.VideoCapabilityVariants, result.VideoCapabilityErrors, err = s.agentVideoCapabilityVariantStatus(ctx, project)
	if err != nil {
		return providerStatusActionResult{}, err
	}
	for _, variant := range result.VideoCapabilityVariants {
		if variant["approvalState"] == "pending" || variant["approvalState"] == "rejected" {
			result.PendingVideoCapabilityApprovals++
		}
	}
	return result, nil
}

func (s *Server) readShotStatusAction(ctx context.Context, project Project, input shotStatusActionInput) (shotStatusActionResult, error) {
	limit, err := normalizeProjectControlPageLimit(input.Limit, 100, 1000)
	if err != nil {
		return shotStatusActionResult{}, err
	}
	status, err := s.loadShotProductionStatusForEpisode(
		requestWithContext(ctx), project.ID, strings.TrimSpace(input.ScriptSceneID), strings.TrimSpace(input.WorkflowRunID),
		strings.TrimSpace(input.ScriptEpisodeID), strings.TrimSpace(input.StoryboardPlanID), input.IncludePreviewURL,
	)
	if err != nil {
		return shotStatusActionResult{}, err
	}
	total := len(status.Shots)
	hasMore := total > limit
	if hasMore {
		status.Shots = status.Shots[:limit]
	}
	return shotStatusActionResult{Status: status, HasMore: hasMore, Total: total}, nil
}

func (s *Server) workflowRunForProjectContext(ctx context.Context, projectID, runID string) (WorkflowRun, error) {
	return scanWorkflowRun(s.db.QueryRow(ctx, workflowRunSelectSQL(`WHERE id = $1 AND project_id = $2`), runID, projectID))
}

func projectSummaryAgentResult(args map[string]any, result projectSummaryActionResult) agentToolResult {
	summary := result.ProjectGapSummary.Summary
	if strings.TrimSpace(summary) == "" {
		summary = fmt.Sprintf("当前阶段 %s，状态 %s，进度 %d%%。", result.ProductionStatus.Overall.Stage, result.ProductionStatus.Overall.Status, result.ProductionStatus.Overall.Progress)
	}
	return agentToolOK("project.read_summary", args, summary, projectOperationalReadData(result))
}

func storyboardListAgentResult(args map[string]any, page storyboardListActionPage) agentToolResult {
	return agentToolOK("storyboard.list", args, fmt.Sprintf("读取到 %d 个分镜镜头。", len(page.Items)), projectOperationalReadData(page))
}

func workflowRunListAgentResult(args map[string]any, page workflowRunListActionPage) agentToolResult {
	return agentToolOK("workflow.read_runs", args, fmt.Sprintf("读取到 %d 个最近工作流。", len(page.Items)), projectOperationalReadData(page))
}

func workflowNodeListAgentResult(args map[string]any, page workflowNodeListActionPage) agentToolResult {
	return agentToolOK("workflow.read_nodes", args, fmt.Sprintf("读取到 %d 个工作流节点。", len(page.Items)), projectOperationalReadData(page))
}

func workflowShotListAgentResult(args map[string]any, page workflowShotListActionPage) agentToolResult {
	return agentToolOK("workflow.read_shots", args, fmt.Sprintf("读取到 %d 个分镜镜头。", len(page.Items)), projectOperationalReadData(page))
}

func reviewItemListAgentResult(args map[string]any, page reviewItemListActionPage, status string) agentToolResult {
	return agentToolOK("review.list_items", args, fmt.Sprintf("读取到 %d 个审阅问题。", len(page.Items)), map[string]any{
		"items": page.Items, "limit": page.Limit, "nextCursor": page.NextCursor, "status": status,
	})
}

func providerStatusAgentResult(args map[string]any, result providerStatusActionResult) agentToolResult {
	return agentToolOK("provider.list_status", args,
		fmt.Sprintf("当前有 %d 个启用供应商、%d 个启用模型，%d 个视频能力快照需要处理。", result.ActiveAccounts, result.ActiveModels, result.PendingVideoCapabilityApprovals),
		projectOperationalReadData(result),
	)
}

func shotStatusAgentResult(args map[string]any, result shotStatusActionResult) agentToolResult {
	return agentToolOK("shot.status", args, fmt.Sprintf("读取到 %d 个镜头生产状态。", len(result.Status.Shots)), projectOperationalReadData(result))
}

func projectOperationalReadData(value any) map[string]any {
	data, ok := mapFromAny(value)
	if !ok {
		return map[string]any{}
	}
	return data
}
