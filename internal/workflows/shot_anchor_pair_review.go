package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
)

type firstLastAnchorPairMember struct {
	ID        string
	Role      string
	StateHash string
	State     videoproduction.ShotState
	Width     int
	Height    int
}

func reviewFirstLastAnchorPairTx(ctx context.Context, tx pgx.Tx, projectID, shotID string, timelineTimebase int64) (bool, error) {
	rows, err := tx.Query(ctx, `
		WITH latest AS (
			SELECT DISTINCT ON (anchor_role)
			       id, anchor_role, shot_state_version_id, media_file_id, status
			FROM shot_visual_anchors
			WHERE project_id = $1
			  AND storyboard_shot_id = $2
			  AND anchor_role IN ('planned_first_frame', 'planned_last_frame')
			  AND status <> 'archived'
			ORDER BY anchor_role, revision DESC
		)
		SELECT latest.id::text, latest.anchor_role, state.state, state.state_hash,
		       COALESCE(media.width, 0), COALESCE(media.height, 0)
		FROM latest
		JOIN storyboard_shot_state_versions state ON state.id = latest.shot_state_version_id
		LEFT JOIN media_files media ON media.id = latest.media_file_id
		WHERE latest.status = 'ready'
		ORDER BY latest.anchor_role
	`, projectID, shotID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	members := make(map[string]firstLastAnchorPairMember, 2)
	for rows.Next() {
		var member firstLastAnchorPairMember
		var stateRaw []byte
		if err := rows.Scan(&member.ID, &member.Role, &stateRaw, &member.StateHash, &member.Width, &member.Height); err != nil {
			return false, err
		}
		if err := json.Unmarshal(stateRaw, &member.State); err != nil {
			return false, fmt.Errorf("decode %s shot state: %w", member.Role, err)
		}
		actualHash, err := videoproduction.HashShotState(member.State)
		if err != nil {
			return false, err
		}
		if actualHash != member.StateHash {
			return false, workflowError{Code: "SHOT_STATE_HASH_MISMATCH", Message: "首尾帧锚点绑定的镜头状态与已保存 hash 不一致"}
		}
		members[member.Role] = member
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	first, firstReady := members[videoproduction.AnchorRolePlannedFirstFrame]
	last, lastReady := members[videoproduction.AnchorRolePlannedLastFrame]
	if !firstReady || !lastReady {
		return false, nil
	}
	if first.Width <= 0 || first.Height <= 0 || last.Width <= 0 || last.Height <= 0 {
		return false, workflowError{
			Code:              provider.CodeUpstreamOutputMismatch,
			Message:           "首尾帧媒体缺少可验证的宽高信息",
			Retryable:         false,
			RetryabilityKnown: true,
		}
	}
	if first.Width != last.Width || first.Height != last.Height {
		return false, workflowError{
			Code: provider.CodeUpstreamOutputMismatch,
			Message: fmt.Sprintf(
				"首尾帧尺寸不一致：首帧 %dx%d，尾帧 %dx%d",
				first.Width, first.Height, last.Width, last.Height,
			),
			Retryable:         false,
			RetryabilityKnown: true,
		}
	}
	var transition videoproduction.ShotTransition
	var carryRaw, resetRaw []byte
	var durationTicks int64
	if err := tx.QueryRow(ctx, `
		SELECT transition.transition_type, transition.tail_policy, transition.anchor_policy,
		       transition.carry_constraints, transition.reset_constraints,
		       transition.confidence::float8,
		       shot.planned_duration_ticks
		FROM storyboard_shots shot
		JOIN storyboard_shot_transitions transition
		  ON transition.target_shot_id = shot.id
		 AND transition.production_generation_id = shot.production_generation_id
		 AND transition.status = 'active'
		WHERE shot.project_id = $1 AND shot.id = $2 AND shot.deleted_at IS NULL
	`, projectID, shotID).Scan(
		&transition.TransitionType, &transition.TailPolicy, &transition.AnchorPolicy,
		&carryRaw, &resetRaw, &transition.Confidence, &durationTicks,
	); err != nil {
		return false, err
	}
	if err := json.Unmarshal(carryRaw, &transition.Carry); err != nil {
		return false, fmt.Errorf("decode first-last carry contract: %w", err)
	}
	if err := json.Unmarshal(resetRaw, &transition.Reset); err != nil {
		return false, fmt.Errorf("decode first-last reset contract: %w", err)
	}
	review := videoproduction.ReviewFirstLastFrameContract(
		first.State,
		last.State,
		transition,
		videoproduction.RequiredReferenceAssetIDs(first.State),
		durationTicks,
		timelineTimebase,
	)
	if !review.Approved {
		messages := make([]string, 0, len(review.Issues))
		for _, issue := range review.Issues {
			messages = append(messages, issue.Message)
		}
		return false, workflowError{
			Code:              videoproduction.CodeProfileIncompatible,
			Message:           "首尾帧契约审核失败：" + strings.Join(messages, "；"),
			Retryable:         false,
			RetryabilityKnown: true,
		}
	}
	evidence := mustJSON(map[string]any{
		"status":           "approved",
		"reviewSource":     "deterministic_first_last_pair_review",
		"firstAnchorId":    first.ID,
		"lastAnchorId":     last.ID,
		"width":            first.Width,
		"height":           first.Height,
		"durationTicks":    durationTicks,
		"timelineTimebase": timelineTimebase,
		"checks":           review.Checks,
		"reviewedAt":       "database_transaction_time",
	})
	if _, err := tx.Exec(ctx, `
		UPDATE shot_visual_anchors
		SET review_status = 'approved',
		    metadata = COALESCE(metadata, '{}'::jsonb)
		      || jsonb_build_object('firstLastPairReview', $3::jsonb, 'pairReviewedAt', now())
		WHERE id IN ($1::uuid, $2::uuid)
	`, first.ID, last.ID, evidence); err != nil {
		return false, err
	}
	return true, nil
}
