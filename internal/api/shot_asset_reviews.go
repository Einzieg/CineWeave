package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
)

const (
	shotAssetReviewValidationVersion = "shot_asset_requirement.v1"
	maxShotAssetReviewBatchItems     = 1000
)

type BatchReviewShotAssetRequirementsRequest struct {
	RequirementIDs  []string `json:"requirementIds,omitempty"`
	ScriptEpisodeID string   `json:"scriptEpisodeId,omitempty"`
	ReviewStatus    string   `json:"reviewStatus"`
	Note            string   `json:"note,omitempty"`
}

type ShotAssetRequirementReviewIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ShotAssetRequirementReviewItem struct {
	RequirementID        string                            `json:"requirementId"`
	StoryboardShotID     string                            `json:"storyboardShotId"`
	ShotNo               int                               `json:"shotNo"`
	AssetID              string                            `json:"assetId"`
	AssetType            string                            `json:"assetType"`
	AssetName            string                            `json:"assetName"`
	RequirementType      string                            `json:"requirementType"`
	Status               string                            `json:"status"`
	PreviousReviewStatus string                            `json:"previousReviewStatus"`
	ReviewStatus         string                            `json:"reviewStatus"`
	Eligible             bool                              `json:"eligible"`
	Issues               []ShotAssetRequirementReviewIssue `json:"issues"`
	Warnings             []ShotAssetRequirementReviewIssue `json:"warnings"`
	UpdatedAt            time.Time                         `json:"updatedAt"`
}

type BatchReviewShotAssetRequirementsResponse struct {
	ValidationVersion string                           `json:"validationVersion"`
	RequestedStatus   string                           `json:"requestedStatus"`
	TotalItems        int                              `json:"totalItems"`
	EligibleCount     int                              `json:"eligibleCount"`
	BlockedCount      int                              `json:"blockedCount"`
	ApprovedCount     int                              `json:"approvedCount"`
	NeedsEditCount    int                              `json:"needsEditCount"`
	RejectedCount     int                              `json:"rejectedCount"`
	UnchangedCount    int                              `json:"unchangedCount"`
	NotFoundIDs       []string                         `json:"notFoundIds"`
	Items             []ShotAssetRequirementReviewItem `json:"items"`
}

type shotAssetRequirementReviewCandidate struct {
	RequirementID      string
	StoryboardShotID   string
	ShotNo             int
	AssetID            string
	AssetType          string
	AssetName          string
	RequirementType    string
	Status             string
	ReviewStatus       string
	StaleState         string
	HasContext         bool
	ShotDeleted        bool
	ShotStaleState     string
	AssetStatus        string
	AssetReviewStatus  string
	AssetStaleState    string
	BaseReferenceReady bool
	UpdatedAt          time.Time
}

func (s *Server) batchReviewShotAssetRequirements(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite)
	if !ok {
		return
	}
	var req BatchReviewShotAssetRequirementsRequest
	if !decode(w, r, &req) {
		return
	}
	result, err := s.batchReviewShotAssetRequirementsCore(r.Context(), project, principal.UserID, "api_batch_review", req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, result, nil)
}

func (s *Server) batchReviewShotAssetRequirementsCore(
	ctx context.Context,
	project Project,
	userID string,
	source string,
	req BatchReviewShotAssetRequirementsRequest,
) (BatchReviewShotAssetRequirementsResponse, error) {
	req.ReviewStatus = strings.ToLower(strings.TrimSpace(req.ReviewStatus))
	req.Note = strings.TrimSpace(req.Note)
	req.ScriptEpisodeID = strings.TrimSpace(req.ScriptEpisodeID)
	req.RequirementIDs = uniqueTrimmedStrings(req.RequirementIDs)
	if req.ReviewStatus != "approved" && req.ReviewStatus != "needs_edit" && req.ReviewStatus != "rejected" {
		return BatchReviewShotAssetRequirementsResponse{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "reviewStatus must be approved, needs_edit, or rejected")
	}
	if len(req.RequirementIDs) > maxShotAssetReviewBatchItems {
		return BatchReviewShotAssetRequirementsResponse{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "too many shot asset requirements in one review batch")
	}
	if project.ProductionGeneration == nil || strings.TrimSpace(project.ProductionGeneration.ID) == "" {
		return BatchReviewShotAssetRequirementsResponse{}, newAPIError(http.StatusConflict, "PRODUCTION_GENERATION_MISMATCH", "项目没有活动的视频生产代")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return BatchReviewShotAssetRequirementsResponse{}, err
	}
	defer tx.Rollback(ctx)

	reviewFilter := ""
	if len(req.RequirementIDs) == 0 {
		reviewFilter = "pending"
	}
	candidates, err := loadShotAssetRequirementReviewCandidates(
		ctx,
		tx,
		project.ID,
		project.ProductionGeneration.ID,
		req.ScriptEpisodeID,
		req.RequirementIDs,
		reviewFilter,
		maxShotAssetReviewBatchItems,
		true,
	)
	if err != nil {
		return BatchReviewShotAssetRequirementsResponse{}, err
	}
	result := BatchReviewShotAssetRequirementsResponse{
		ValidationVersion: shotAssetReviewValidationVersion,
		RequestedStatus:   req.ReviewStatus,
		TotalItems:        len(candidates),
		NotFoundIDs:       missingRequestedIDs(req.RequirementIDs, candidates),
		Items:             make([]ShotAssetRequirementReviewItem, 0, len(candidates)),
	}
	for _, candidate := range candidates {
		issues, warnings := validateShotAssetRequirementReviewCandidate(candidate)
		eligible := len(issues) == 0
		if eligible {
			result.EligibleCount++
		} else {
			result.BlockedCount++
		}
		effectiveStatus := req.ReviewStatus
		if req.ReviewStatus == "approved" && !eligible {
			effectiveStatus = "needs_edit"
		}
		metadataPatch := map[string]any{
			"reviewStatus":             effectiveStatus,
			"reviewNote":               req.Note,
			"reviewedBy":               userID,
			"reviewedAt":               time.Now().UTC(),
			"reviewSource":             firstNonEmpty(strings.TrimSpace(source), "batch_review"),
			"reviewValidationVersion":  shotAssetReviewValidationVersion,
			"reviewValidationIssues":   issues,
			"reviewValidationWarnings": warnings,
		}
		var updatedAt time.Time
		if err := tx.QueryRow(ctx, `
			UPDATE shot_asset_requirements
			SET review_status = $4,
			    metadata = COALESCE(metadata, '{}'::jsonb) || $5::jsonb,
			    updated_at = now()
			WHERE id = $1
			  AND project_id = $2
			  AND production_generation_id = $3
			RETURNING updated_at
		`, candidate.RequirementID, project.ID, project.ProductionGeneration.ID, effectiveStatus, mustMarshal(metadataPatch)).Scan(&updatedAt); err != nil {
			return BatchReviewShotAssetRequirementsResponse{}, err
		}
		if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "shot_asset_requirement.updated", "shot_asset_requirement", candidate.RequirementID, mustRawJSON(map[string]any{
			"requirementId":        candidate.RequirementID,
			"reviewStatus":         effectiveStatus,
			"reviewSource":         metadataPatch["reviewSource"],
			"validationVersion":    shotAssetReviewValidationVersion,
			"validationIssueCount": len(issues),
		})); err != nil {
			return BatchReviewShotAssetRequirementsResponse{}, err
		}
		if candidate.ReviewStatus == effectiveStatus {
			result.UnchangedCount++
		}
		switch effectiveStatus {
		case "approved":
			result.ApprovedCount++
		case "needs_edit":
			result.NeedsEditCount++
		case "rejected":
			result.RejectedCount++
		}
		result.Items = append(result.Items, shotAssetRequirementReviewItem(candidate, effectiveStatus, eligible, issues, warnings, updatedAt))
	}
	if err := tx.Commit(ctx); err != nil {
		return BatchReviewShotAssetRequirementsResponse{}, err
	}
	return result, nil
}

func loadShotAssetRequirementReviewCandidates(
	ctx context.Context,
	db snapshotQuerier,
	projectID string,
	generationID string,
	scriptEpisodeID string,
	requirementIDs []string,
	reviewStatus string,
	limit int,
	forUpdate bool,
) ([]shotAssetRequirementReviewCandidate, error) {
	if limit < 1 || limit > maxShotAssetReviewBatchItems {
		limit = maxShotAssetReviewBatchItems
	}
	lockClause := ""
	if forUpdate {
		lockClause = " FOR UPDATE OF requirement"
	}
	rows, err := db.Query(ctx, `
		SELECT requirement.id::text,
		       requirement.storyboard_shot_id::text,
		       COALESCE(shot.shot_no, shot.shot_index + 1),
		       asset.id::text,
		       asset.asset_type,
		       asset.name,
		       requirement.requirement_type,
		       requirement.status,
		       requirement.review_status,
		       requirement.stale_state,
		       (
		         NULLIF(BTRIM(COALESCE(requirement.role_in_shot, '')), '') IS NOT NULL
		         OR NULLIF(BTRIM(COALESCE(requirement.costume, '')), '') IS NOT NULL
		         OR NULLIF(BTRIM(COALESCE(requirement.pose, '')), '') IS NOT NULL
		         OR NULLIF(BTRIM(COALESCE(requirement.expression, '')), '') IS NOT NULL
		         OR NULLIF(BTRIM(COALESCE(requirement.action, '')), '') IS NOT NULL
		         OR NULLIF(BTRIM(COALESCE(requirement.camera_relation, '')), '') IS NOT NULL
		         OR NULLIF(BTRIM(COALESCE(requirement.scene_state, '')), '') IS NOT NULL
		         OR NULLIF(BTRIM(COALESCE(requirement.prop_state, '')), '') IS NOT NULL
		         OR NULLIF(BTRIM(COALESCE(requirement.prompt, '')), '') IS NOT NULL
		       ),
		       shot.deleted_at IS NOT NULL,
		       shot.stale_state,
		       asset.status,
		       asset.review_status,
		       asset.stale_state,
		       (
		         asset.primary_reference_artifact_id IS NOT NULL
		         OR asset.primary_reference_media_file_id IS NOT NULL
		         OR COALESCE(asset.primary_reference_storage_key, '') <> ''
		         OR asset.reference_artifact_id IS NOT NULL
		         OR asset.reference_media_file_id IS NOT NULL
		         OR COALESCE(asset.reference_storage_key, '') <> ''
		       ),
		       requirement.updated_at
		FROM shot_asset_requirements requirement
		JOIN storyboard_shots shot
		  ON shot.id = requirement.storyboard_shot_id
		 AND shot.project_id = requirement.project_id
		 AND shot.production_generation_id = requirement.production_generation_id
		JOIN canonical_assets asset
		  ON asset.id = requirement.asset_id
		 AND asset.project_id = requirement.project_id
		WHERE requirement.project_id = $1
		  AND requirement.production_generation_id = $2
		  AND ($3 = '' OR shot.script_episode_id::text = $3)
		  AND (
		    $3 = ''
		    OR shot.storyboard_plan_id IS NULL
		    OR EXISTS (
		      SELECT 1
		      FROM storyboard_plans active_plan
		      WHERE active_plan.id = shot.storyboard_plan_id
		        AND active_plan.project_id = shot.project_id
		        AND active_plan.production_generation_id = shot.production_generation_id
		        AND active_plan.active = true
		        AND active_plan.status = 'ready'
		    )
		  )
		  AND (COALESCE(cardinality($4::text[]), 0) = 0 OR requirement.id::text = ANY($4::text[]))
		  AND ($5 = '' OR requirement.review_status = $5)
		ORDER BY COALESCE(shot.episode_index, 0), COALESCE(shot.episode_shot_index, shot.shot_index),
		         asset.asset_type, asset.name, requirement.id
		LIMIT $6`+lockClause,
		projectID, generationID, strings.TrimSpace(scriptEpisodeID), requirementIDs, strings.TrimSpace(reviewStatus), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]shotAssetRequirementReviewCandidate, 0)
	for rows.Next() {
		var item shotAssetRequirementReviewCandidate
		if err := rows.Scan(
			&item.RequirementID,
			&item.StoryboardShotID,
			&item.ShotNo,
			&item.AssetID,
			&item.AssetType,
			&item.AssetName,
			&item.RequirementType,
			&item.Status,
			&item.ReviewStatus,
			&item.StaleState,
			&item.HasContext,
			&item.ShotDeleted,
			&item.ShotStaleState,
			&item.AssetStatus,
			&item.AssetReviewStatus,
			&item.AssetStaleState,
			&item.BaseReferenceReady,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Server) agentToolListShotAssetRequirements(r *http.Request, principal auth.Principal, project Project, args map[string]any) agentToolResult {
	if err := s.authorizer.Authorize(r.Context(), principal, authz.PermissionAssetRead, authz.Resource{ProjectID: project.ID}); err != nil {
		return agentToolError("shot_asset.list_requirements", args, err)
	}
	if project.ProductionGeneration == nil || strings.TrimSpace(project.ProductionGeneration.ID) == "" {
		return agentToolError("shot_asset.list_requirements", args, newAPIError(http.StatusConflict, "PRODUCTION_GENERATION_MISMATCH", "项目没有活动的视频生产代"))
	}
	reviewStatus := strings.ToLower(strings.TrimSpace(agentStringArg(args, "reviewStatus")))
	scriptEpisodeID := agentReferenceStringArg(args, "scriptEpisodeId")
	if reviewStatus == "all" {
		reviewStatus = ""
	}
	if reviewStatus != "" && !validReviewStatus(reviewStatus) {
		return agentToolError("shot_asset.list_requirements", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "reviewStatus is invalid"))
	}
	limit := agentIntArg(args, "limit", 200, 1, maxShotAssetReviewBatchItems)
	candidates, err := loadShotAssetRequirementReviewCandidates(
		r.Context(), s.db, project.ID, project.ProductionGeneration.ID, scriptEpisodeID, nil, reviewStatus, limit, false,
	)
	if err != nil {
		return agentToolError("shot_asset.list_requirements", args, err)
	}
	items := make([]ShotAssetRequirementReviewItem, 0, len(candidates))
	eligibleCount := 0
	for _, candidate := range candidates {
		issues, warnings := validateShotAssetRequirementReviewCandidate(candidate)
		eligible := len(issues) == 0
		if eligible {
			eligibleCount++
		}
		items = append(items, shotAssetRequirementReviewItem(
			candidate, candidate.ReviewStatus, eligible, issues, warnings, candidate.UpdatedAt,
		))
	}
	return agentToolOK(
		"shot_asset.list_requirements",
		args,
		fmt.Sprintf("读取到 %d 个镜头资产需求，其中 %d 个通过结构化校验。", len(items), eligibleCount),
		map[string]any{
			"validationVersion": shotAssetReviewValidationVersion,
			"totalItems":        len(items),
			"eligibleCount":     eligibleCount,
			"blockedCount":      len(items) - eligibleCount,
			"items":             items,
		},
	)
}

func (s *Server) agentToolReviewShotAssetRequirements(r *http.Request, principal auth.Principal, project Project, args map[string]any) agentToolResult {
	if err := s.authorizer.Authorize(r.Context(), principal, authz.PermissionAssetWrite, authz.Resource{ProjectID: project.ID}); err != nil {
		return agentToolError("shot_asset.review_requirements", args, err)
	}
	req := BatchReviewShotAssetRequirementsRequest{
		RequirementIDs:  agentReferenceStringSliceArg(args, "requirementIds"),
		ScriptEpisodeID: agentReferenceStringArg(args, "scriptEpisodeId"),
		ReviewStatus:    agentStringArg(args, "reviewStatus"),
		Note:            agentStringArg(args, "note"),
	}
	result, err := s.batchReviewShotAssetRequirementsCore(r.Context(), project, principal.UserID, "project_agent", req)
	if err != nil {
		return agentToolError("shot_asset.review_requirements", args, err)
	}
	summary := fmt.Sprintf("已审核 %d 个镜头资产需求：批准 %d 个，需修改 %d 个。", result.TotalItems, result.ApprovedCount, result.NeedsEditCount)
	if result.BlockedCount > 0 {
		summary += fmt.Sprintf(" %d 个需求未通过结构化校验，未被批准。", result.BlockedCount)
	}
	return agentToolOK("shot_asset.review_requirements", args, summary, map[string]any{"report": result})
}

func (s *Server) agentToolUpdateShotAssetRequirement(r *http.Request, principal auth.Principal, project Project, args map[string]any) agentToolResult {
	const toolName = "shot_asset.update_requirement"
	if err := s.authorizer.Authorize(r.Context(), principal, authz.PermissionAssetWrite, authz.Resource{ProjectID: project.ID}); err != nil {
		return agentToolError(toolName, args, err)
	}
	requirementID := agentReferenceStringArg(args, "requirementId")
	if requirementID == "" {
		return agentToolError(toolName, args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "requirementId is required"))
	}
	patch := agentMapArg(args, "patch")
	req := updateShotAssetRequirementRequestFromPatch(patch)
	item, err := s.updateShotAssetRequirementCore(r.Context(), project, principal.UserID, "project_agent", requirementID, req)
	if err != nil {
		return agentToolError(toolName, args, err)
	}
	return agentToolOK(toolName, args, "已修正镜头资产需求；该需求需要重新审核后才能生成衍生图。", map[string]any{
		"requirement": item,
		"nextAction": map[string]any{
			"tool": "shot_asset.review_requirements",
			"args": map[string]any{
				"requirementIds": []string{item.ID},
				"reviewStatus":   "approved",
				"note":           "Project Agent 修正后重新执行结构化校验",
			},
		},
	})
}

func (s *Server) agentToolSkipShotAssetRequirement(r *http.Request, principal auth.Principal, project Project, args map[string]any) agentToolResult {
	const toolName = "shot_asset.skip_requirement"
	if err := s.authorizer.Authorize(r.Context(), principal, authz.PermissionAssetWrite, authz.Resource{ProjectID: project.ID}); err != nil {
		return agentToolError(toolName, args, err)
	}
	requirementID := agentReferenceStringArg(args, "requirementId")
	reason := agentStringArg(args, "reason")
	if requirementID == "" || reason == "" {
		return agentToolError(toolName, args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "requirementId and reason are required"))
	}
	item, err := s.skipShotAssetRequirementCore(r.Context(), project, principal.UserID, "project_agent", reason, requirementID)
	if err != nil {
		return agentToolError(toolName, args, err)
	}
	return agentToolOK(toolName, args, "已跳过不适用于当前镜头的资产需求，并保留审计记录。", map[string]any{"requirement": item})
}

func updateShotAssetRequirementRequestFromPatch(patch map[string]any) UpdateShotAssetRequirementRequest {
	req := UpdateShotAssetRequirementRequest{}
	set := func(key string, target **string) {
		value, ok := optionalStringPatch(patch, key)
		if ok {
			*target = &value
		}
	}
	set("assetId", &req.AssetID)
	set("requirementType", &req.RequirementType)
	set("costume", &req.Costume)
	set("pose", &req.Pose)
	set("expression", &req.Expression)
	set("action", &req.Action)
	set("cameraRelation", &req.CameraRelation)
	set("sceneState", &req.SceneState)
	set("propState", &req.PropState)
	set("prompt", &req.Prompt)
	return req
}

func (s *Server) previewShotAssetRequirementReview(ctx context.Context, project Project, args map[string]any) (map[string]any, error) {
	if project.ProductionGeneration == nil || strings.TrimSpace(project.ProductionGeneration.ID) == "" {
		return nil, newAPIError(http.StatusConflict, "PRODUCTION_GENERATION_MISMATCH", "项目没有活动的视频生产代")
	}
	reviewStatus := strings.ToLower(strings.TrimSpace(agentStringArg(args, "reviewStatus")))
	if reviewStatus != "approved" && reviewStatus != "needs_edit" && reviewStatus != "rejected" {
		return nil, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "reviewStatus must be approved, needs_edit, or rejected")
	}
	ids := uniqueTrimmedStrings(agentReferenceStringSliceArg(args, "requirementIds"))
	scriptEpisodeID := agentReferenceStringArg(args, "scriptEpisodeId")
	filter := ""
	if len(ids) == 0 {
		filter = "pending"
	}
	candidates, err := loadShotAssetRequirementReviewCandidates(
		ctx, s.db, project.ID, project.ProductionGeneration.ID, scriptEpisodeID, ids, filter, maxShotAssetReviewBatchItems, false,
	)
	if err != nil {
		return nil, err
	}
	eligibleCount := 0
	blocked := make([]map[string]any, 0)
	for _, candidate := range candidates {
		issues, _ := validateShotAssetRequirementReviewCandidate(candidate)
		if len(issues) == 0 {
			eligibleCount++
			continue
		}
		if len(blocked) < 20 {
			blocked = append(blocked, map[string]any{
				"requirementId": candidate.RequirementID,
				"shotNo":        candidate.ShotNo,
				"assetName":     candidate.AssetName,
				"issues":        issues,
			})
		}
	}
	return map[string]any{
		"reviewStatus":       reviewStatus,
		"targetCount":        len(candidates),
		"eligibleCount":      eligibleCount,
		"blockedCount":       len(candidates) - eligibleCount,
		"blockedItemsSample": blocked,
		"validationVersion":  shotAssetReviewValidationVersion,
	}, nil
}

func validateShotAssetRequirementReviewCandidate(candidate shotAssetRequirementReviewCandidate) ([]ShotAssetRequirementReviewIssue, []ShotAssetRequirementReviewIssue) {
	issues := make([]ShotAssetRequirementReviewIssue, 0)
	warnings := make([]ShotAssetRequirementReviewIssue, 0)
	addIssue := func(code, message string) {
		issues = append(issues, ShotAssetRequirementReviewIssue{Code: code, Message: message})
	}
	if candidate.Status == "skipped" {
		addIssue("SHOT_ASSET_REQUIREMENT_SKIPPED", "镜头资产需求已跳过")
	}
	if candidate.Status == "image_running" {
		addIssue("SHOT_ASSET_REQUIREMENT_IMAGE_RUNNING", "镜头衍生资产正在生成，暂不能修改审核状态")
	}
	if candidate.StaleState == "upstream_changed" {
		warnings = append(warnings, ShotAssetRequirementReviewIssue{
			Code:    "SHOT_ASSET_REQUIREMENT_UPSTREAM_CHANGED",
			Message: "关联核心资产的生产资料已变化；审核通过后需要重新生成镜头衍生资产和媒体",
		})
	} else if candidate.StaleState == "needs_regeneration" {
		warnings = append(warnings, ShotAssetRequirementReviewIssue{
			Code:    "SHOT_ASSET_REQUIREMENT_REGENERATION_PENDING",
			Message: "镜头资产需求已修正，审核通过后将重新生成衍生资产",
		})
	}
	if candidate.ShotDeleted {
		addIssue("STORYBOARD_SHOT_DELETED", "关联分镜已删除")
	} else if candidate.ShotStaleState != "fresh" {
		warnings = append(warnings, ShotAssetRequirementReviewIssue{
			Code:    "STORYBOARD_SHOT_MEDIA_REGENERATION_PENDING",
			Message: "关联分镜的媒体需要重新生成；这不会阻止当前需求先完成审核和衍生资产生产",
		})
	}
	if candidate.AssetStatus == "archived" {
		addIssue("CANONICAL_ASSET_ARCHIVED", "关联核心资产已归档")
	}
	if candidate.AssetStaleState != "fresh" {
		addIssue("CANONICAL_ASSET_STALE", "关联核心资产已过期，需要先更新资产")
	}
	if candidate.AssetType != "character" && candidate.AssetType != "scene" && candidate.AssetType != "prop" {
		addIssue("SHOT_ASSET_TYPE_INVALID", "关联资产类型无效")
	}
	requirementType := strings.TrimSpace(candidate.RequirementType)
	if requirementType == "" || requirementType == "shot_context" {
		addIssue("SHOT_ASSET_REQUIREMENT_TYPE_MISSING", "缺少明确的镜头资产需求类型")
	} else if candidate.AssetType == "character" || candidate.AssetType == "scene" || candidate.AssetType == "prop" {
		if !strings.HasPrefix(requirementType, candidate.AssetType+"_") {
			addIssue("SHOT_ASSET_REQUIREMENT_TYPE_MISMATCH", "镜头资产需求类型与核心资产类型不匹配")
		}
	}
	if !candidate.HasContext {
		addIssue("SHOT_ASSET_REQUIREMENT_CONTEXT_MISSING", "缺少服装、姿态、动作、表情、场景状态或道具状态等镜头上下文")
	}
	if !candidate.BaseReferenceReady {
		addIssue("DERIVED_ASSET_BASE_REFERENCE_REQUIRED", "基础资产没有可用参考图")
	}
	if candidate.AssetReviewStatus != "approved" {
		warnings = append(warnings, ShotAssetRequirementReviewIssue{
			Code:    "CANONICAL_ASSET_REVIEW_PENDING",
			Message: "核心资产尚未人工确认，但已有完整参考图，可继续生成镜头衍生资产",
		})
	}
	return issues, warnings
}

func shotAssetRequirementReviewItem(
	candidate shotAssetRequirementReviewCandidate,
	reviewStatus string,
	eligible bool,
	issues []ShotAssetRequirementReviewIssue,
	warnings []ShotAssetRequirementReviewIssue,
	updatedAt time.Time,
) ShotAssetRequirementReviewItem {
	return ShotAssetRequirementReviewItem{
		RequirementID:        candidate.RequirementID,
		StoryboardShotID:     candidate.StoryboardShotID,
		ShotNo:               candidate.ShotNo,
		AssetID:              candidate.AssetID,
		AssetType:            candidate.AssetType,
		AssetName:            candidate.AssetName,
		RequirementType:      candidate.RequirementType,
		Status:               candidate.Status,
		PreviousReviewStatus: candidate.ReviewStatus,
		ReviewStatus:         reviewStatus,
		Eligible:             eligible,
		Issues:               issues,
		Warnings:             warnings,
		UpdatedAt:            updatedAt,
	}
}

func missingRequestedIDs(requested []string, candidates []shotAssetRequirementReviewCandidate) []string {
	if len(requested) == 0 {
		return []string{}
	}
	found := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		found[candidate.RequirementID] = true
	}
	missing := make([]string, 0)
	for _, id := range requested {
		if !found[id] {
			missing = append(missing, id)
		}
	}
	return missing
}

func uniqueTrimmedStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
