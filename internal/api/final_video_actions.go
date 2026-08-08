package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type finalVideoActivateActionInput struct {
	VersionID        string `json:"versionId"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

type finalVideoDeleteActionInput struct {
	VersionID        string `json:"versionId"`
	ExpectedRevision int64  `json:"expectedRevision"`
	ConfirmActive    bool   `json:"confirmActive,omitempty"`
}

type finalVideoDeleteActionResult struct {
	Deleted          bool   `json:"deleted"`
	VersionID        string `json:"versionId"`
	ExpectedRevision int64  `json:"expectedRevision"`
	PreviousStatus   string `json:"previousStatus"`
}

func decodeFinalVideoActivateActionInput(raw json.RawMessage) (finalVideoActivateActionInput, error) {
	var input finalVideoActivateActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return input, err
	}
	input.VersionID = strings.TrimSpace(input.VersionID)
	if err := validateFinalVideoMutationInput(input.VersionID, input.ExpectedRevision); err != nil {
		return input, err
	}
	return input, nil
}

func decodeFinalVideoDeleteActionInput(raw json.RawMessage) (finalVideoDeleteActionInput, error) {
	var input finalVideoDeleteActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return input, err
	}
	input.VersionID = strings.TrimSpace(input.VersionID)
	if err := validateFinalVideoMutationInput(input.VersionID, input.ExpectedRevision); err != nil {
		return input, err
	}
	return input, nil
}

func validateFinalVideoMutationInput(versionID string, expectedRevision int64) error {
	if strings.TrimSpace(versionID) == "" || expectedRevision < 1 {
		return controlValidationError("versionId 和 expectedRevision 为必填项")
	}
	if _, err := uuid.Parse(versionID); err != nil {
		return controlValidationError("versionId 必须为有效 UUID")
	}
	return nil
}

func validateFinalVideoActionCommand(command projectcontrol.Command, action string) error {
	if strings.TrimSpace(command.ProjectID) == "" || strings.TrimSpace(command.ActorUserID) == "" || strings.TrimSpace(command.ID) == "" {
		return newAPIError(http.StatusUnprocessableEntity, "PROJECT_CONTROL_CONTEXT_INVALID", action+" 缺少项目控制上下文")
	}
	return nil
}

func (s *Server) activateFinalVideoActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	command projectcontrol.Command,
	input finalVideoActivateActionInput,
) (FinalVideoVersion, bool, error) {
	productionContext, err := lockActiveVideoProductionContext(ctx, tx, project.ID)
	if err != nil {
		return FinalVideoVersion{}, false, err
	}
	current, err := finalVideoVersionByIDTx(ctx, tx, project.ID, input.VersionID, productionContext.Generation.ID, true)
	if err != nil {
		return FinalVideoVersion{}, false, err
	}
	if current.Revision != input.ExpectedRevision {
		return FinalVideoVersion{}, false, revisionConflictError(
			"FINAL_VIDEO_REVISION_CONFLICT", "成片版本已被其他操作修改", input.ExpectedRevision, current.Revision,
		)
	}
	if current.Status != "ready" && current.Status != "active" {
		return FinalVideoVersion{}, false, newAPIError(
			http.StatusUnprocessableEntity,
			"FINAL_VIDEO_NOT_ACTIVATABLE",
			"只有就绪或已激活的成片版本可以设为当前成片",
		)
	}
	if current.ProductionReadiness != "ready" {
		return FinalVideoVersion{}, false, finalVideoProductionReadinessError(current.ID, current.NativeAudioStatus, current.ProductionReadiness)
	}

	var activeVersionID string
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(active_final_video_version_id::text, '')
		FROM projects
		WHERE id = $1
		FOR UPDATE
	`, project.ID).Scan(&activeVersionID); err != nil {
		return FinalVideoVersion{}, false, err
	}
	if current.Status == "active" && activeVersionID == current.ID {
		return current, true, nil
	}

	if _, err := tx.Exec(ctx, `
		UPDATE final_video_versions
		SET status = 'ready'
		WHERE project_id = $1 AND status = 'active' AND id <> $2 AND production_generation_id = $3
	`, project.ID, current.ID, productionContext.Generation.ID); err != nil {
		return FinalVideoVersion{}, false, err
	}
	metadata := mustRawJSON(map[string]any{
		"lastActivatedBy":               command.ActorUserID,
		"lastActivatedControlCommandId": command.ID,
		"lastActivatedControllerType":   command.ControllerType,
	})
	tag, err := tx.Exec(ctx, `
		UPDATE final_video_versions
		SET status = 'active',
		    metadata = COALESCE(metadata, '{}'::jsonb)
		        || $5::jsonb
		        || jsonb_build_object('lastActivatedAt', now())
		WHERE project_id = $1 AND id = $2 AND production_generation_id = $3 AND revision = $4
	`, project.ID, current.ID, productionContext.Generation.ID, input.ExpectedRevision, metadata)
	if err != nil {
		return FinalVideoVersion{}, false, err
	}
	if tag.RowsAffected() == 0 {
		latest, latestErr := finalVideoVersionByIDTx(ctx, tx, project.ID, input.VersionID, productionContext.Generation.ID, false)
		if latestErr == nil {
			return FinalVideoVersion{}, false, revisionConflictError(
				"FINAL_VIDEO_REVISION_CONFLICT", "成片版本已被其他操作修改", input.ExpectedRevision, latest.Revision,
			)
		}
		return FinalVideoVersion{}, false, pgx.ErrNoRows
	}
	if _, err := tx.Exec(ctx, `
		UPDATE projects SET active_final_video_version_id = $2 WHERE id = $1
	`, project.ID, current.ID); err != nil {
		return FinalVideoVersion{}, false, err
	}
	updated, err := finalVideoVersionByIDTx(ctx, tx, project.ID, current.ID, productionContext.Generation.ID, false)
	if err != nil {
		return FinalVideoVersion{}, false, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "final_video.activated", "final_video_version", current.ID, mustRawJSON(map[string]any{
		"finalVideoVersionId": current.ID,
		"revision":            updated.Revision,
		"controlCommandId":    command.ID,
		"controllerType":      command.ControllerType,
	})); err != nil {
		return FinalVideoVersion{}, false, err
	}
	return updated, false, nil
}

func (s *Server) deleteFinalVideoActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	command projectcontrol.Command,
	input finalVideoDeleteActionInput,
) (finalVideoDeleteActionResult, error) {
	productionContext, err := lockActiveVideoProductionContext(ctx, tx, project.ID)
	if err != nil {
		return finalVideoDeleteActionResult{}, err
	}
	current, err := finalVideoVersionByIDTx(ctx, tx, project.ID, input.VersionID, productionContext.Generation.ID, true)
	if err != nil {
		return finalVideoDeleteActionResult{}, err
	}
	if current.Revision != input.ExpectedRevision {
		return finalVideoDeleteActionResult{}, revisionConflictError(
			"FINAL_VIDEO_REVISION_CONFLICT", "成片版本已被其他操作修改", input.ExpectedRevision, current.Revision,
		)
	}
	if current.Status == "active" && !input.ConfirmActive {
		return finalVideoDeleteActionResult{}, newAPIError(
			http.StatusUnprocessableEntity,
			"ACTIVE_FINAL_VIDEO_REQUIRES_CONFIRMATION",
			"删除当前成片版本需要明确确认",
		)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE projects
		SET active_final_video_version_id = NULL
		WHERE id = $1 AND active_final_video_version_id = $2
	`, project.ID, current.ID); err != nil {
		return finalVideoDeleteActionResult{}, err
	}
	tag, err := tx.Exec(ctx, `
		DELETE FROM final_video_versions
		WHERE project_id = $1 AND id = $2 AND production_generation_id = $3 AND revision = $4
	`, project.ID, current.ID, productionContext.Generation.ID, input.ExpectedRevision)
	if err != nil {
		return finalVideoDeleteActionResult{}, err
	}
	if tag.RowsAffected() == 0 {
		return finalVideoDeleteActionResult{}, revisionConflictError(
			"FINAL_VIDEO_REVISION_CONFLICT", "成片版本已被其他操作修改", input.ExpectedRevision, current.Revision,
		)
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "final_video.deleted", "final_video_version", current.ID, mustRawJSON(map[string]any{
		"finalVideoVersionId": current.ID,
		"revision":            current.Revision,
		"previousStatus":      current.Status,
		"controlCommandId":    command.ID,
		"controllerType":      command.ControllerType,
	})); err != nil {
		return finalVideoDeleteActionResult{}, err
	}
	return finalVideoDeleteActionResult{
		Deleted: true, VersionID: current.ID, ExpectedRevision: input.ExpectedRevision, PreviousStatus: current.Status,
	}, nil
}

func finalVideoVersionByIDTx(
	ctx context.Context,
	tx pgx.Tx,
	projectID string,
	versionID string,
	productionGenerationID string,
	lock bool,
) (FinalVideoVersion, error) {
	query := `
		SELECT version_row.id, version_row.organization_id, version_row.project_id, version_row.timeline_id,
		       version_row.workflow_run_id::text, version_row.version, version_row.revision, version_row.title, version_row.status,
		       version_row.artifact_id::text, version_row.media_file_id::text, version_row.storage_key, version_row.duration_ticks,
		       timeline.timeline_timebase, timeline.fps_numerator, timeline.fps_denominator,
		       version_row.resolution, version_row.aspect_ratio, version_row.native_audio_status, version_row.production_readiness,
		       version_row.compose_settings, version_row.metadata,
		       version_row.created_by::text, version_row.created_at
		FROM final_video_versions version_row
		JOIN project_timelines timeline ON timeline.id = version_row.timeline_id
		WHERE version_row.project_id = $1 AND version_row.id = $2
		  AND version_row.production_generation_id = $3
		  AND timeline.production_generation_id = version_row.production_generation_id
	`
	if lock {
		query += " FOR UPDATE OF version_row"
	}
	return scanFinalVideoVersion(tx.QueryRow(ctx, query, projectID, versionID, productionGenerationID))
}

func finalVideoActivateAgentResult(arguments map[string]any, item FinalVideoVersion, idempotent bool) agentToolResult {
	summary := fmt.Sprintf("成片版本已激活，当前 revision 为 %d。", item.Revision)
	if idempotent {
		summary = "成片版本已是当前激活版本。"
	}
	return agentToolOK("final_video.activate", arguments, summary, map[string]any{
		"finalVideo": item, "versionId": item.ID, "status": item.Status,
		"revision": item.Revision, "idempotent": idempotent,
	})
}

func finalVideoDeleteAgentResult(arguments map[string]any, result finalVideoDeleteActionResult) agentToolResult {
	return agentToolOK("final_video.delete", arguments, "成片版本已删除。", map[string]any{
		"deleted": result.Deleted, "versionId": result.VersionID,
		"revision": result.ExpectedRevision, "previousStatus": result.PreviousStatus,
	})
}

func finalVideoNotFoundOrConflict(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return newAPIError(http.StatusNotFound, "FINAL_VIDEO_NOT_FOUND", "成片版本不存在或不属于当前视频生产代")
	}
	return err
}
