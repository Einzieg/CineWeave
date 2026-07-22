package workflows

import (
	"context"
	"errors"
	"strings"

	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
)

type shotVisualAnchorRuntime struct {
	ID           string
	Role         string
	Revision     int
	Status       string
	ReviewStatus string
	ArtifactID   string
	MediaFileID  string
	StorageKey   string
}

type ResolveShotAnchorWorkItemsInput struct {
	ProjectID     string   `json:"projectId"`
	WorkflowRunID string   `json:"workflowRunId"`
	ShotIDs       []string `json:"shotIds"`
}

type ShotAnchorWorkItem struct {
	ShotID     string `json:"shotId"`
	ShotIndex  int    `json:"shotIndex"`
	ShotNo     int    `json:"shotNo"`
	AnchorRole string `json:"anchorRole"`
}

func (a Activities) ResolveShotAnchorWorkItems(ctx context.Context, input ResolveShotAnchorWorkItemsInput) ([]ShotAnchorWorkItem, error) {
	project, err := a.projectProductionSettings(ctx, input.ProjectID, input.WorkflowRunID)
	if err != nil {
		return nil, err
	}
	roles, err := requiredProfileAnchorRoles(project.VideoProductionProfileKey)
	if err != nil {
		return nil, err
	}
	shotIDs := cleanBatchStringList(input.ShotIDs)
	if len(shotIDs) == 0 {
		return []ShotAnchorWorkItem{}, nil
	}
	rows, err := a.db.Query(ctx, `
		SELECT id::text, shot_index, COALESCE(shot_no, shot_index + 1)
		FROM storyboard_shots
		WHERE project_id = $1
		  AND id = ANY($2::uuid[])
		  AND production_generation_id = $3
		  AND deleted_at IS NULL
	`, input.ProjectID, shotIDs, project.ProductionGenerationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type shotPosition struct{ Index, No int }
	positions := make(map[string]shotPosition, len(shotIDs))
	for rows.Next() {
		var shotID string
		var position shotPosition
		if err := rows.Scan(&shotID, &position.Index, &position.No); err != nil {
			return nil, err
		}
		positions[shotID] = position
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items := make([]ShotAnchorWorkItem, 0, len(shotIDs)*len(roles))
	for _, shotID := range shotIDs {
		position, ok := positions[shotID]
		if !ok {
			return nil, videoproduction.Error{Code: videoproduction.CodePromptContractIncomplete, Message: "批量任务中的镜头不存在或已删除：" + shotID}
		}
		for _, role := range roles {
			items = append(items, ShotAnchorWorkItem{
				ShotID: shotID, ShotIndex: position.Index, ShotNo: position.No, AnchorRole: role,
			})
		}
	}
	return items, nil
}

func (a Activities) latestShotVisualAnchor(ctx context.Context, projectID, shotID, anchorRole string) (shotVisualAnchorRuntime, error) {
	var anchor shotVisualAnchorRuntime
	err := a.db.QueryRow(ctx, `
		SELECT id::text, anchor_role, revision, status, review_status,
		       COALESCE(artifact_id::text, ''), COALESCE(media_file_id::text, ''), COALESCE(storage_key, '')
		FROM shot_visual_anchors
		WHERE project_id = $1 AND storyboard_shot_id = $2 AND anchor_role = $3
		  AND status <> 'archived'
		ORDER BY revision DESC
		LIMIT 1
	`, projectID, shotID, anchorRole).Scan(
		&anchor.ID, &anchor.Role, &anchor.Revision, &anchor.Status, &anchor.ReviewStatus,
		&anchor.ArtifactID, &anchor.MediaFileID, &anchor.StorageKey,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return shotVisualAnchorRuntime{}, videoproduction.Error{
			Code:    videoproduction.CodePromptContractIncomplete,
			Message: "当前镜头缺少视觉锚点，请先重新生成分镜",
		}
	}
	return anchor, err
}

func requiredProfileAnchorRoles(profileKey string) ([]string, error) {
	strategy, err := videoproduction.ProfileStrategyFor(profileKey)
	if err != nil {
		return nil, err
	}
	roles := make([]string, 0, len(strategy.Anchors().Requirements()))
	for _, requirement := range strategy.Anchors().Requirements() {
		if !requirement.Required || strings.TrimSpace(requirement.Role) == "" || requirement.Role == videoproduction.AnchorRoleStoryboardPanel {
			continue
		}
		roles = append(roles, requirement.Role)
	}
	return roles, nil
}

func profileAnchorStateRole(profileKey, anchorRole string) (string, error) {
	strategy, err := videoproduction.ProfileStrategyFor(profileKey)
	if err != nil {
		return "", err
	}
	for _, requirement := range strategy.Anchors().Requirements() {
		if requirement.Role == strings.TrimSpace(anchorRole) {
			return requirement.StateRole, nil
		}
	}
	return "", videoproduction.Error{
		Code:    videoproduction.CodeProfileIncompatible,
		Message: "当前视频生产方案不支持锚点角色：" + strings.TrimSpace(anchorRole),
	}
}

func profileRequiredAnchorsReadyTx(ctx context.Context, tx pgx.Tx, shotID, profileKey string) (bool, error) {
	strategy, err := videoproduction.ProfileStrategyFor(profileKey)
	if err != nil {
		return false, err
	}
	rows, err := tx.Query(ctx, `
		SELECT anchor_role, COUNT(*)
		FROM shot_visual_anchors
		WHERE storyboard_shot_id = $1
		  AND status = 'ready' AND review_status = 'approved'
		  AND artifact_id IS NOT NULL AND media_file_id IS NOT NULL
		  AND COALESCE(storage_key, '') <> ''
		GROUP BY anchor_role
	`, shotID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var role string
		var count int
		if err := rows.Scan(&role, &count); err != nil {
			return false, err
		}
		counts[role] = count
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	for _, requirement := range strategy.Anchors().Requirements() {
		if !requirement.Required {
			continue
		}
		count := counts[requirement.Role]
		if count < requirement.Minimum || (requirement.Maximum > 0 && count > requirement.Maximum) {
			return false, nil
		}
	}
	return true, nil
}

func inputProfileKey(input GenerateShotImageInput) string {
	if profileKey := strings.TrimSpace(input.ProfileKey); profileKey != "" {
		return profileKey
	}
	if strings.TrimSpace(input.AnchorRole) == videoproduction.AnchorRolePlannedLastFrame {
		return videoproduction.ProfileFirstLastFrame
	}
	return videoproduction.ProfileSingleFrameI2V
}
