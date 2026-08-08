package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/google/uuid"
)

type shotAssetRequirementListActionInput struct {
	StoryboardShotID string `json:"storyboardShotId"`
	ScriptEpisodeID  string `json:"scriptEpisodeId"`
	ReviewStatus     string `json:"reviewStatus"`
	Limit            int    `json:"limit"`
	Cursor           string `json:"cursor"`
}

type shotAssetRequirementListActionPage struct {
	ValidationVersion string                           `json:"validationVersion"`
	Items             []ShotAssetRequirement           `json:"items"`
	ReviewItems       []ShotAssetRequirementReviewItem `json:"reviewItems"`
	EligibleCount     int                              `json:"eligibleCount"`
	BlockedCount      int                              `json:"blockedCount"`
	Limit             int                              `json:"limit"`
	NextCursor        string                           `json:"nextCursor,omitempty"`
}

func (s *Server) listShotAssetRequirementsAction(ctx context.Context, project Project, input shotAssetRequirementListActionInput) (shotAssetRequirementListActionPage, error) {
	if project.ProductionGeneration == nil || strings.TrimSpace(project.ProductionGeneration.ID) == "" {
		return shotAssetRequirementListActionPage{}, videoproduction.NewError(videoproduction.CodeGenerationMismatch, "项目没有活动的视频生产代", false)
	}
	limit, err := normalizeProjectControlPageLimit(input.Limit, 200, maxShotAssetReviewBatchItems)
	if err != nil {
		return shotAssetRequirementListActionPage{}, err
	}
	offset, err := decodeProjectControlOffsetCursor(input.Cursor)
	if err != nil {
		return shotAssetRequirementListActionPage{}, err
	}
	input.StoryboardShotID = strings.TrimSpace(input.StoryboardShotID)
	input.ScriptEpisodeID = strings.TrimSpace(input.ScriptEpisodeID)
	input.ReviewStatus = strings.ToLower(strings.TrimSpace(input.ReviewStatus))
	if input.ReviewStatus == "all" {
		input.ReviewStatus = ""
	}
	if input.StoryboardShotID != "" && uuid.Validate(input.StoryboardShotID) != nil {
		return shotAssetRequirementListActionPage{}, controlValidationError("storyboardShotId 无效")
	}
	if input.ScriptEpisodeID != "" && uuid.Validate(input.ScriptEpisodeID) != nil {
		return shotAssetRequirementListActionPage{}, controlValidationError("scriptEpisodeId 无效")
	}
	if input.ReviewStatus != "" && !validReviewStatus(input.ReviewStatus) {
		return shotAssetRequirementListActionPage{}, controlValidationError("reviewStatus 必须是 pending、approved、needs_edit、rejected 或 all")
	}
	rows, err := s.db.Query(ctx, shotAssetRequirementSelectSQL(`
		JOIN storyboard_shots shot
		  ON shot.id = r.storyboard_shot_id
		 AND shot.project_id = r.project_id
		 AND shot.production_generation_id = r.production_generation_id
		WHERE r.project_id = $1
		  AND r.production_generation_id = $2
		  AND ($3 = '' OR r.storyboard_shot_id = NULLIF($3, '')::uuid)
		  AND ($4 = '' OR shot.script_episode_id = NULLIF($4, '')::uuid)
		  AND ($5 = '' OR r.review_status = $5)
		  AND shot.deleted_at IS NULL
		  AND (
		    $4 = ''
		    OR shot.storyboard_plan_id IS NULL
		    OR EXISTS (
		      SELECT 1 FROM storyboard_plans active_plan
		      WHERE active_plan.id = shot.storyboard_plan_id
		        AND active_plan.project_id = shot.project_id
		        AND active_plan.production_generation_id = shot.production_generation_id
		        AND active_plan.active = true
		        AND active_plan.status = 'ready'
		    )
		  )
		ORDER BY COALESCE(shot.episode_index, 0), COALESCE(shot.episode_shot_index, shot.shot_index),
		         a.asset_type, a.name, r.id
		LIMIT $6 OFFSET $7
	`), project.ID, project.ProductionGeneration.ID, input.StoryboardShotID, input.ScriptEpisodeID, input.ReviewStatus, limit+1, offset)
	if err != nil {
		return shotAssetRequirementListActionPage{}, err
	}
	defer rows.Close()
	items := make([]ShotAssetRequirement, 0, limit+1)
	for rows.Next() {
		item, err := scanShotAssetRequirement(rows)
		if err != nil {
			return shotAssetRequirementListActionPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return shotAssetRequirementListActionPage{}, err
	}
	page := shotAssetRequirementListActionPage{
		ValidationVersion: shotAssetReviewValidationVersion,
		Items:             items,
		ReviewItems:       []ShotAssetRequirementReviewItem{},
		Limit:             limit,
	}
	if len(items) > limit {
		page.Items = items[:limit]
		page.NextCursor, err = encodeProjectControlOffsetCursor(offset + limit)
		if err != nil {
			return shotAssetRequirementListActionPage{}, err
		}
	}
	ids := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		ids = append(ids, item.ID)
	}
	if len(ids) == 0 {
		return page, nil
	}
	candidates, err := loadShotAssetRequirementReviewCandidates(
		ctx, s.db, project.ID, project.ProductionGeneration.ID, "", ids, "", len(ids), false,
	)
	if err != nil {
		return shotAssetRequirementListActionPage{}, err
	}
	byID := make(map[string]shotAssetRequirementReviewCandidate, len(candidates))
	for _, candidate := range candidates {
		byID[candidate.RequirementID] = candidate
	}
	for _, requirement := range page.Items {
		candidate, exists := byID[requirement.ID]
		if !exists {
			continue
		}
		issues, warnings := validateShotAssetRequirementReviewCandidate(candidate)
		eligible := len(issues) == 0
		if eligible {
			page.EligibleCount++
		} else {
			page.BlockedCount++
		}
		page.ReviewItems = append(page.ReviewItems, shotAssetRequirementReviewItem(
			candidate, candidate.ReviewStatus, eligible, issues, warnings, candidate.UpdatedAt,
		))
	}
	return page, nil
}

func shotAssetRequirementListAgentResult(args map[string]any, page shotAssetRequirementListActionPage) agentToolResult {
	return agentToolOK(
		"shot_asset.list_requirements",
		args,
		fmt.Sprintf("读取到 %d 个镜头资产需求，其中 %d 个通过结构化校验。", len(page.ReviewItems), page.EligibleCount),
		map[string]any{
			"validationVersion": page.ValidationVersion,
			"totalItems":        len(page.ReviewItems),
			"eligibleCount":     page.EligibleCount,
			"blockedCount":      page.BlockedCount,
			"items":             page.ReviewItems,
			"limit":             page.Limit,
			"nextCursor":        page.NextCursor,
		},
	)
}
