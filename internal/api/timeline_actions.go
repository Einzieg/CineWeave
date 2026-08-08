package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/production"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type timelineCreateActionInput struct {
	Title               string `json:"title,omitempty"`
	AspectRatio         string `json:"aspectRatio,omitempty"`
	Resolution          string `json:"resolution,omitempty"`
	FromStoryboardShots bool   `json:"fromStoryboardShots,omitempty"`
}

type timelineUpdatePatch struct {
	Title       *string `json:"title,omitempty"`
	Status      *string `json:"status,omitempty"`
	AspectRatio *string `json:"aspectRatio,omitempty"`
	Resolution  *string `json:"resolution,omitempty"`
}

type timelineUpdateActionInput struct {
	TimelineID       string              `json:"timelineId"`
	ExpectedRevision int64               `json:"expectedRevision"`
	Patch            timelineUpdatePatch `json:"patch"`
}

type timelineDeleteActionInput struct {
	TimelineID       string `json:"timelineId"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

type timelineDeleteActionResult struct {
	Deleted          bool   `json:"deleted"`
	TimelineID       string `json:"timelineId"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

type timelineClipCreateFields struct {
	StoryboardShotID    string `json:"storyboardShotId,omitempty"`
	VideoArtifactID     string `json:"videoArtifactId,omitempty"`
	VideoMediaFileID    string `json:"videoMediaFileId,omitempty"`
	ClipIndex           *int   `json:"clipIndex,omitempty"`
	Title               string `json:"title,omitempty"`
	Enabled             *bool  `json:"enabled,omitempty"`
	SourceStorageKey    string `json:"sourceStorageKey,omitempty"`
	SourceDurationTicks *int64 `json:"sourceDurationTicks,omitempty"`
	TrimStartTick       *int64 `json:"trimStartTick,omitempty"`
	TrimEndTick         *int64 `json:"trimEndTick,omitempty"`
	DurationTicks       *int64 `json:"durationTicks,omitempty"`
	Notes               string `json:"notes,omitempty"`
}

type timelineClipCreateActionInput struct {
	TimelineID               string `json:"timelineId"`
	ExpectedTimelineRevision int64  `json:"expectedTimelineRevision"`
	timelineClipCreateFields
}

type timelineClipUpdateActionInput struct {
	TimelineID               string                     `json:"timelineId"`
	ClipID                   string                     `json:"clipId"`
	ExpectedTimelineRevision int64                      `json:"expectedTimelineRevision"`
	ExpectedRevision         int64                      `json:"expectedRevision"`
	Patch                    map[string]json.RawMessage `json:"patch"`
}

type timelineClipDeleteActionInput struct {
	TimelineID               string `json:"timelineId"`
	ClipID                   string `json:"clipId"`
	ExpectedTimelineRevision int64  `json:"expectedTimelineRevision"`
	ExpectedRevision         int64  `json:"expectedRevision"`
}

type timelineClipDeleteActionResult struct {
	Deleted          bool   `json:"deleted"`
	TimelineID       string `json:"timelineId"`
	ClipID           string `json:"clipId"`
	TimelineRevision int64  `json:"timelineRevision"`
}

type timelineClipReorderItem struct {
	ClipID           string `json:"clipId"`
	ExpectedRevision int64  `json:"expectedRevision"`
	ClipIndex        int    `json:"clipIndex"`
}

type timelineClipReorderActionInput struct {
	TimelineID               string                    `json:"timelineId"`
	ExpectedTimelineRevision int64                     `json:"expectedTimelineRevision"`
	Items                    []timelineClipReorderItem `json:"items"`
}

type timelineClipReorderActionResult struct {
	TimelineID       string         `json:"timelineId"`
	TimelineRevision int64          `json:"timelineRevision"`
	Items            []TimelineClip `json:"items"`
}

func decodeTimelineCreateActionInput(raw json.RawMessage) (timelineCreateActionInput, error) {
	var input timelineCreateActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return input, err
	}
	input.Title = strings.TrimSpace(input.Title)
	input.AspectRatio = strings.TrimSpace(input.AspectRatio)
	input.Resolution = strings.TrimSpace(input.Resolution)
	return input, nil
}

func decodeTimelineUpdateActionInput(raw json.RawMessage) (timelineUpdateActionInput, error) {
	var input timelineUpdateActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return input, err
	}
	input.TimelineID = strings.TrimSpace(input.TimelineID)
	if err := validateTimelineMutationIdentity(input.TimelineID, input.ExpectedRevision); err != nil {
		return input, err
	}
	if input.Patch.Title == nil && input.Patch.Status == nil && input.Patch.AspectRatio == nil && input.Patch.Resolution == nil {
		return input, controlValidationError("patch 至少需要一个时间线字段")
	}
	if input.Patch.Status != nil {
		value := strings.TrimSpace(*input.Patch.Status)
		if !validTimelineStatus(value) {
			return input, controlValidationError("时间线状态无效")
		}
		input.Patch.Status = &value
	}
	return input, nil
}

func decodeTimelineDeleteActionInput(raw json.RawMessage) (timelineDeleteActionInput, error) {
	var input timelineDeleteActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return input, err
	}
	input.TimelineID = strings.TrimSpace(input.TimelineID)
	if err := validateTimelineMutationIdentity(input.TimelineID, input.ExpectedRevision); err != nil {
		return input, err
	}
	return input, nil
}

func decodeTimelineClipCreateActionInput(raw json.RawMessage) (timelineClipCreateActionInput, error) {
	var input timelineClipCreateActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return input, err
	}
	input.TimelineID = strings.TrimSpace(input.TimelineID)
	if err := validateTimelineMutationIdentity(input.TimelineID, input.ExpectedTimelineRevision); err != nil {
		return input, err
	}
	if err := validateOptionalUUIDs(map[string]string{
		"storyboardShotId": input.StoryboardShotID,
		"videoArtifactId":  input.VideoArtifactID,
		"videoMediaFileId": input.VideoMediaFileID,
	}); err != nil {
		return input, err
	}
	return input, nil
}

func decodeTimelineClipUpdateActionInput(raw json.RawMessage) (timelineClipUpdateActionInput, error) {
	var input timelineClipUpdateActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return input, err
	}
	input.TimelineID = strings.TrimSpace(input.TimelineID)
	input.ClipID = strings.TrimSpace(input.ClipID)
	if err := validateTimelineMutationIdentity(input.TimelineID, input.ExpectedTimelineRevision); err != nil {
		return input, err
	}
	if err := validateTimelineMutationIdentity(input.ClipID, input.ExpectedRevision); err != nil {
		return input, controlValidationError("clipId 和 expectedRevision 为必填项")
	}
	if len(input.Patch) == 0 {
		return input, controlValidationError("patch 至少需要一个片段字段")
	}
	return input, nil
}

func decodeTimelineClipDeleteActionInput(raw json.RawMessage) (timelineClipDeleteActionInput, error) {
	var input timelineClipDeleteActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return input, err
	}
	input.TimelineID = strings.TrimSpace(input.TimelineID)
	input.ClipID = strings.TrimSpace(input.ClipID)
	if err := validateTimelineMutationIdentity(input.TimelineID, input.ExpectedTimelineRevision); err != nil {
		return input, err
	}
	if err := validateTimelineMutationIdentity(input.ClipID, input.ExpectedRevision); err != nil {
		return input, controlValidationError("clipId 和 expectedRevision 为必填项")
	}
	return input, nil
}

func decodeTimelineClipReorderActionInput(raw json.RawMessage) (timelineClipReorderActionInput, error) {
	var input timelineClipReorderActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return input, err
	}
	input.TimelineID = strings.TrimSpace(input.TimelineID)
	if err := validateTimelineMutationIdentity(input.TimelineID, input.ExpectedTimelineRevision); err != nil {
		return input, err
	}
	if len(input.Items) == 0 {
		return input, controlValidationError("items 不能为空")
	}
	seenIDs := make(map[string]struct{}, len(input.Items))
	seenIndices := make(map[int]struct{}, len(input.Items))
	for index := range input.Items {
		item := &input.Items[index]
		item.ClipID = strings.TrimSpace(item.ClipID)
		if err := validateTimelineMutationIdentity(item.ClipID, item.ExpectedRevision); err != nil {
			return input, controlValidationError("每个排序项都必须包含有效 clipId 和 expectedRevision")
		}
		if item.ClipIndex < 0 || item.ClipIndex >= len(input.Items) {
			return input, controlValidationError("clipIndex 必须覆盖从 0 开始的连续序号")
		}
		if _, exists := seenIDs[item.ClipID]; exists {
			return input, controlValidationError("排序项包含重复 clipId")
		}
		if _, exists := seenIndices[item.ClipIndex]; exists {
			return input, controlValidationError("排序项包含重复 clipIndex")
		}
		seenIDs[item.ClipID] = struct{}{}
		seenIndices[item.ClipIndex] = struct{}{}
	}
	return input, nil
}

func validateTimelineMutationIdentity(id string, expectedRevision int64) error {
	if strings.TrimSpace(id) == "" || expectedRevision < 1 {
		return controlValidationError("资源 ID 和 expectedRevision 为必填项")
	}
	if _, err := uuid.Parse(id); err != nil {
		return controlValidationError("资源 ID 必须为有效 UUID")
	}
	return nil
}

func validateOptionalUUIDs(values map[string]string) error {
	for field, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, err := uuid.Parse(value); err != nil {
			return controlValidationError(field + " 必须为有效 UUID")
		}
	}
	return nil
}

func validateTimelineActionCommand(command projectcontrol.Command, action string) error {
	if strings.TrimSpace(command.ProjectID) == "" || strings.TrimSpace(command.ActorUserID) == "" || strings.TrimSpace(command.ID) == "" {
		return newAPIError(http.StatusUnprocessableEntity, "PROJECT_CONTROL_CONTEXT_INVALID", action+" 缺少项目控制上下文")
	}
	return nil
}

func (s *Server) createTimelineActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	command projectcontrol.Command,
	input timelineCreateActionInput,
) (ProjectTimeline, error) {
	productionContext, err := lockActiveVideoProductionContext(ctx, tx, project.ID)
	if err != nil {
		return ProjectTimeline{}, err
	}
	title := defaultAPIString(input.Title, "默认时间线")
	aspectRatio := defaultAPIString(input.AspectRatio, project.VideoRatio, stringValue(project.AspectRatio), "16:9")
	resolution := defaultAPIString(input.Resolution, "720p")
	metadata := mustRawJSON(map[string]any{
		"createdControlCommandId": command.ID,
		"createdControllerType":   command.ControllerType,
	})
	item, err := scanProjectTimeline(tx.QueryRow(ctx, `
		INSERT INTO project_timelines(
			organization_id, project_id, title, status, aspect_ratio, resolution,
			timeline_timebase, fps_numerator, fps_denominator, metadata, created_by,
			production_generation_id
		)
		VALUES ($1, $2, $3, 'draft', $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, organization_id, project_id, revision, workflow_run_id::text, title, status, aspect_ratio, resolution,
		          timeline_timebase, fps_numerator, fps_denominator,
		          metadata, created_by::text, edited_by::text, created_at, updated_at, edited_at
	`, project.OrganizationID, project.ID, title, aspectRatio, resolution,
		project.TimelineTimebase, project.FPSNumerator, project.FPSDenominator, metadata, command.ActorUserID, productionContext.Generation.ID))
	if err != nil {
		return ProjectTimeline{}, err
	}
	if input.FromStoryboardShots {
		if err := s.createTimelineClipsFromStoryboard(ctx, tx, project, item.ID); err != nil {
			return ProjectTimeline{}, err
		}
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "timeline.created", "project_timeline", item.ID, mustRawJSON(map[string]any{
		"timelineId": item.ID, "revision": item.Revision, "fromStoryboardShots": input.FromStoryboardShots,
		"controlCommandId": command.ID, "controllerType": command.ControllerType,
	})); err != nil {
		return ProjectTimeline{}, err
	}
	return item, nil
}

func (s *Server) updateTimelineActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	command projectcontrol.Command,
	input timelineUpdateActionInput,
) (ProjectTimeline, error) {
	productionContext, err := lockActiveVideoProductionContext(ctx, tx, project.ID)
	if err != nil {
		return ProjectTimeline{}, err
	}
	current, err := timelineByIDTx(ctx, tx, project.ID, input.TimelineID, productionContext.Generation.ID, true)
	if err != nil {
		return ProjectTimeline{}, timelineActionNotFound(err)
	}
	if current.Revision != input.ExpectedRevision {
		return ProjectTimeline{}, revisionConflictError("TIMELINE_REVISION_CONFLICT", "时间线已被其他操作修改", input.ExpectedRevision, current.Revision)
	}
	metadata := mustRawJSON(map[string]any{
		"lastEditedControlCommandId": command.ID,
		"lastEditedControllerType":   command.ControllerType,
	})
	tag, err := tx.Exec(ctx, `
		UPDATE project_timelines
		SET title = COALESCE(NULLIF($4, ''), title),
		    status = COALESCE(NULLIF($5, ''), status),
		    aspect_ratio = COALESCE(NULLIF($6, ''), aspect_ratio),
		    resolution = COALESCE(NULLIF($7, ''), resolution),
		    manual_override = true,
		    stale_state = 'fresh',
		    edited_by = $8,
		    edited_at = now(),
		    metadata = COALESCE(metadata, '{}'::jsonb) || $9::jsonb
		WHERE project_id = $1 AND id = $2 AND production_generation_id = $3 AND revision = $10
	`, project.ID, current.ID, productionContext.Generation.ID,
		optionalTrimmedValue(input.Patch.Title), optionalTrimmedValue(input.Patch.Status),
		optionalTrimmedValue(input.Patch.AspectRatio), optionalTrimmedValue(input.Patch.Resolution),
		command.ActorUserID, metadata, input.ExpectedRevision)
	if err != nil {
		return ProjectTimeline{}, err
	}
	if tag.RowsAffected() == 0 {
		return ProjectTimeline{}, revisionConflictError("TIMELINE_REVISION_CONFLICT", "时间线已被其他操作修改", input.ExpectedRevision, current.Revision)
	}
	if input.Patch.Status != nil || input.Patch.AspectRatio != nil || input.Patch.Resolution != nil {
		if err := production.MarkFinalVideoStale(ctx, tx, project.ID, ""); err != nil {
			return ProjectTimeline{}, err
		}
	}
	updated, err := timelineByIDTx(ctx, tx, project.ID, current.ID, productionContext.Generation.ID, false)
	if err != nil {
		return ProjectTimeline{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "timeline.updated", "project_timeline", current.ID, mustRawJSON(map[string]any{
		"timelineId": current.ID, "revision": updated.Revision,
		"controlCommandId": command.ID, "controllerType": command.ControllerType,
	})); err != nil {
		return ProjectTimeline{}, err
	}
	return updated, nil
}

func (s *Server) deleteTimelineActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	command projectcontrol.Command,
	input timelineDeleteActionInput,
) (timelineDeleteActionResult, error) {
	productionContext, err := lockActiveVideoProductionContext(ctx, tx, project.ID)
	if err != nil {
		return timelineDeleteActionResult{}, err
	}
	current, err := timelineByIDTx(ctx, tx, project.ID, input.TimelineID, productionContext.Generation.ID, true)
	if err != nil {
		return timelineDeleteActionResult{}, timelineActionNotFound(err)
	}
	if current.Revision != input.ExpectedRevision {
		return timelineDeleteActionResult{}, revisionConflictError("TIMELINE_REVISION_CONFLICT", "时间线已被其他操作修改", input.ExpectedRevision, current.Revision)
	}
	tag, err := tx.Exec(ctx, `
		DELETE FROM project_timelines
		WHERE project_id = $1 AND id = $2 AND production_generation_id = $3 AND revision = $4
	`, project.ID, current.ID, productionContext.Generation.ID, input.ExpectedRevision)
	if err != nil {
		return timelineDeleteActionResult{}, err
	}
	if tag.RowsAffected() == 0 {
		return timelineDeleteActionResult{}, revisionConflictError("TIMELINE_REVISION_CONFLICT", "时间线已被其他操作修改", input.ExpectedRevision, current.Revision)
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "timeline.deleted", "project_timeline", current.ID, mustRawJSON(map[string]any{
		"timelineId": current.ID, "revision": current.Revision,
		"controlCommandId": command.ID, "controllerType": command.ControllerType,
	})); err != nil {
		return timelineDeleteActionResult{}, err
	}
	return timelineDeleteActionResult{Deleted: true, TimelineID: current.ID, ExpectedRevision: current.Revision}, nil
}

func (s *Server) createTimelineClipActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	command projectcontrol.Command,
	input timelineClipCreateActionInput,
) (TimelineClip, error) {
	productionContext, err := lockActiveVideoProductionContext(ctx, tx, project.ID)
	if err != nil {
		return TimelineClip{}, err
	}
	timeline, err := timelineByIDTx(ctx, tx, project.ID, input.TimelineID, productionContext.Generation.ID, true)
	if err != nil {
		return TimelineClip{}, timelineActionNotFound(err)
	}
	if timeline.Revision != input.ExpectedTimelineRevision {
		return TimelineClip{}, revisionConflictError("TIMELINE_REVISION_CONFLICT", "时间线已被其他操作修改", input.ExpectedTimelineRevision, timeline.Revision)
	}
	timebase, err := projectTimelineTimebase(timeline)
	if err != nil {
		return TimelineClip{}, newAPIError(http.StatusUnprocessableEntity, "INVALID_TIMELINE_TIMEBASE", err.Error())
	}
	if _, err := tx.Exec(ctx, `SET CONSTRAINTS timeline_clips_timeline_index_unique DEFERRED`); err != nil {
		return TimelineClip{}, err
	}
	var clipCount int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM timeline_clips WHERE timeline_id = $1`, timeline.ID).Scan(&clipCount); err != nil {
		return TimelineClip{}, err
	}
	clipIndex := clipCount
	if input.ClipIndex != nil {
		clipIndex = *input.ClipIndex
	}
	if clipIndex < 0 || clipIndex > clipCount {
		return TimelineClip{}, controlValidationError("clipIndex 超出时间线范围")
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	title := strings.TrimSpace(input.Title)
	videoArtifactID := strings.TrimSpace(input.VideoArtifactID)
	videoMediaFileID := strings.TrimSpace(input.VideoMediaFileID)
	sourceStorageKey := strings.TrimSpace(input.SourceStorageKey)
	sourceDurationTicks := input.SourceDurationTicks
	var shotDurationTicks *int64
	storyboardShotID := strings.TrimSpace(input.StoryboardShotID)
	if storyboardShotID != "" {
		var shotTitle sql.NullString
		var mediaDuration sql.NullFloat64
		var plannedDuration int64
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(video_artifact_id::text, ''), COALESCE(video_media_file_id::text, ''),
			       COALESCE(video_storage_key, mf.storage_key, va.storage_key, ''),
			       mf.duration_seconds::float8, planned_duration_ticks, COALESCE(title, visual, '')
			FROM storyboard_shots s
			LEFT JOIN media_files mf ON mf.id = s.video_media_file_id
			LEFT JOIN artifacts va ON va.id = s.video_artifact_id
			WHERE s.project_id = $1 AND s.id = $2 AND s.production_generation_id = $3 AND s.deleted_at IS NULL
		`, project.ID, storyboardShotID, productionContext.Generation.ID).Scan(
			&videoArtifactID, &videoMediaFileID, &sourceStorageKey, &mediaDuration, &plannedDuration, &shotTitle,
		); err != nil {
			return TimelineClip{}, timelineClipReferenceError(err)
		}
		shotDurationTicks = &plannedDuration
		if sourceDurationTicks == nil {
			value := plannedDuration
			if mediaDuration.Valid && mediaDuration.Float64 > 0 {
				value = timebase.QuantizeTickNearest(timebase.SecondsToTicks(mediaDuration.Float64))
			}
			sourceDurationTicks = &value
		}
		if title == "" && shotTitle.Valid {
			title = strings.TrimSpace(shotTitle.String)
		}
	}
	if err := validateTimelineClipMediaOwnership(ctx, tx, project.ID, videoArtifactID, videoMediaFileID); err != nil {
		return TimelineClip{}, err
	}
	if title == "" {
		title = "镜头片段"
	}
	trimStartTick := int64(0)
	if input.TrimStartTick != nil {
		trimStartTick = *input.TrimStartTick
	}
	durationTicks := input.DurationTicks
	if durationTicks == nil && shotDurationTicks != nil {
		durationTicks = shotDurationTicks
	}
	sourceDurationTicks, trimStartTick, trimEndTick, resolvedDurationTicks, err := resolveTimelineClipTiming(
		timebase, sourceDurationTicks, trimStartTick, input.TrimEndTick, durationTicks,
	)
	if err != nil {
		return TimelineClip{}, controlValidationError(err.Error())
	}
	if clipIndex < clipCount {
		if _, err := tx.Exec(ctx, `
			UPDATE timeline_clips SET clip_index = clip_index + 1
			WHERE timeline_id = $1 AND clip_index >= $2
		`, timeline.ID, clipIndex); err != nil {
			return TimelineClip{}, err
		}
	}
	metadata := mustRawJSON(map[string]any{
		"createdControlCommandId": command.ID,
		"createdControllerType":   command.ControllerType,
	})
	var clipID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO timeline_clips(
			organization_id, project_id, timeline_id, storyboard_shot_id, video_artifact_id, video_media_file_id,
			clip_index, title, enabled, source_storage_key, source_duration_ticks,
			trim_start_tick, trim_end_tick, start_tick, end_tick, notes, metadata, manual_override, stale_state, edited_by, edited_at,
			production_generation_id
		)
		VALUES ($1, $2, $3, NULLIF($4, '')::uuid, NULLIF($5, '')::uuid, NULLIF($6, '')::uuid,
		        $7, $8, $9, NULLIF($10, ''), $11, $12, $13, 0, $14, NULLIF($15, ''), $16, true, 'fresh', $17, now(), $18)
		RETURNING id::text
	`, project.OrganizationID, project.ID, timeline.ID, storyboardShotID, videoArtifactID, videoMediaFileID,
		clipIndex, title, enabled, sourceStorageKey, nullableInt64Ptr(sourceDurationTicks), trimStartTick,
		nullableInt64Ptr(trimEndTick), resolvedDurationTicks, strings.TrimSpace(input.Notes), metadata,
		command.ActorUserID, productionContext.Generation.ID).Scan(&clipID); err != nil {
		return TimelineClip{}, err
	}
	if err := reflowTimelineClipTicks(ctx, tx, timeline.ID); err != nil {
		return TimelineClip{}, err
	}
	if err := markTimelineEdited(ctx, tx, project.ID, timeline.ID, command.ActorUserID); err != nil {
		return TimelineClip{}, err
	}
	item, err := timelineClipByIDTx(ctx, tx, project.ID, timeline.ID, clipID, productionContext.Generation.ID, false)
	if err != nil {
		return TimelineClip{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "timeline.clip.created", "timeline_clip", item.ID, mustRawJSON(map[string]any{
		"timelineId": timeline.ID, "clipId": item.ID, "revision": item.Revision, "timelineRevision": item.TimelineRevision,
		"controlCommandId": command.ID, "controllerType": command.ControllerType,
	})); err != nil {
		return TimelineClip{}, err
	}
	return item, nil
}

func (s *Server) updateTimelineClipActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	command projectcontrol.Command,
	input timelineClipUpdateActionInput,
) (TimelineClip, error) {
	productionContext, err := lockActiveVideoProductionContext(ctx, tx, project.ID)
	if err != nil {
		return TimelineClip{}, err
	}
	timeline, err := timelineByIDTx(ctx, tx, project.ID, input.TimelineID, productionContext.Generation.ID, true)
	if err != nil {
		return TimelineClip{}, timelineActionNotFound(err)
	}
	if timeline.Revision != input.ExpectedTimelineRevision {
		return TimelineClip{}, revisionConflictError("TIMELINE_REVISION_CONFLICT", "时间线已被其他操作修改", input.ExpectedTimelineRevision, timeline.Revision)
	}
	current, err := timelineClipByIDTx(ctx, tx, project.ID, timeline.ID, input.ClipID, productionContext.Generation.ID, true)
	if err != nil {
		return TimelineClip{}, timelineClipActionNotFound(err)
	}
	if current.Revision != input.ExpectedRevision {
		return TimelineClip{}, revisionConflictError("TIMELINE_CLIP_REVISION_CONFLICT", "时间线片段已被其他操作修改", input.ExpectedRevision, current.Revision)
	}
	if err := applyTimelineClipPatch(&current, input.Patch); err != nil {
		return TimelineClip{}, err
	}
	timebase, err := projectTimelineTimebase(timeline)
	if err != nil {
		return TimelineClip{}, newAPIError(http.StatusUnprocessableEntity, "INVALID_TIMELINE_TIMEBASE", err.Error())
	}
	_, trimStartTick, trimEndTick, durationTicks, err := resolveTimelineClipTiming(
		timebase, current.SourceDurationTicks, current.TrimStartTick, current.TrimEndTick, &current.DurationTicks,
	)
	if err != nil {
		return TimelineClip{}, controlValidationError(err.Error())
	}
	metadata := mustRawJSON(map[string]any{
		"lastEditedControlCommandId": command.ID,
		"lastEditedControllerType":   command.ControllerType,
	})
	tag, err := tx.Exec(ctx, `
		UPDATE timeline_clips
		SET title = $6, enabled = $7, trim_start_tick = $8, trim_end_tick = $9,
		    end_tick = start_tick + $10, notes = $11, manual_override = true,
		    stale_state = 'fresh', edited_by = $12, edited_at = now(),
		    metadata = COALESCE(metadata, '{}'::jsonb) || $13::jsonb
		WHERE project_id = $1 AND timeline_id = $2 AND id = $3
		  AND production_generation_id = $4 AND revision = $5
	`, project.ID, timeline.ID, current.ID, productionContext.Generation.ID, input.ExpectedRevision,
		current.Title, current.Enabled, trimStartTick, nullableInt64Ptr(trimEndTick), durationTicks,
		nullableStringPtr(current.Notes), command.ActorUserID, metadata)
	if err != nil {
		return TimelineClip{}, err
	}
	if tag.RowsAffected() == 0 {
		return TimelineClip{}, revisionConflictError("TIMELINE_CLIP_REVISION_CONFLICT", "时间线片段已被其他操作修改", input.ExpectedRevision, current.Revision)
	}
	if err := reflowTimelineClipTicks(ctx, tx, timeline.ID); err != nil {
		return TimelineClip{}, err
	}
	if err := markTimelineEdited(ctx, tx, project.ID, timeline.ID, command.ActorUserID); err != nil {
		return TimelineClip{}, err
	}
	updated, err := timelineClipByIDTx(ctx, tx, project.ID, timeline.ID, current.ID, productionContext.Generation.ID, false)
	if err != nil {
		return TimelineClip{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "timeline.clip.updated", "timeline_clip", updated.ID, mustRawJSON(map[string]any{
		"timelineId": timeline.ID, "clipId": updated.ID, "revision": updated.Revision, "timelineRevision": updated.TimelineRevision,
		"controlCommandId": command.ID, "controllerType": command.ControllerType,
	})); err != nil {
		return TimelineClip{}, err
	}
	return updated, nil
}

func (s *Server) deleteTimelineClipActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	command projectcontrol.Command,
	input timelineClipDeleteActionInput,
) (timelineClipDeleteActionResult, error) {
	productionContext, err := lockActiveVideoProductionContext(ctx, tx, project.ID)
	if err != nil {
		return timelineClipDeleteActionResult{}, err
	}
	timeline, err := timelineByIDTx(ctx, tx, project.ID, input.TimelineID, productionContext.Generation.ID, true)
	if err != nil {
		return timelineClipDeleteActionResult{}, timelineActionNotFound(err)
	}
	if timeline.Revision != input.ExpectedTimelineRevision {
		return timelineClipDeleteActionResult{}, revisionConflictError("TIMELINE_REVISION_CONFLICT", "时间线已被其他操作修改", input.ExpectedTimelineRevision, timeline.Revision)
	}
	current, err := timelineClipByIDTx(ctx, tx, project.ID, timeline.ID, input.ClipID, productionContext.Generation.ID, true)
	if err != nil {
		return timelineClipDeleteActionResult{}, timelineClipActionNotFound(err)
	}
	if current.Revision != input.ExpectedRevision {
		return timelineClipDeleteActionResult{}, revisionConflictError("TIMELINE_CLIP_REVISION_CONFLICT", "时间线片段已被其他操作修改", input.ExpectedRevision, current.Revision)
	}
	tag, err := tx.Exec(ctx, `
		DELETE FROM timeline_clips
		WHERE project_id = $1 AND timeline_id = $2 AND id = $3
		  AND production_generation_id = $4 AND revision = $5
	`, project.ID, timeline.ID, current.ID, productionContext.Generation.ID, input.ExpectedRevision)
	if err != nil {
		return timelineClipDeleteActionResult{}, err
	}
	if tag.RowsAffected() == 0 {
		return timelineClipDeleteActionResult{}, revisionConflictError("TIMELINE_CLIP_REVISION_CONFLICT", "时间线片段已被其他操作修改", input.ExpectedRevision, current.Revision)
	}
	if err := reindexTimelineClips(ctx, tx, timeline.ID); err != nil {
		return timelineClipDeleteActionResult{}, err
	}
	if err := reflowTimelineClipTicks(ctx, tx, timeline.ID); err != nil {
		return timelineClipDeleteActionResult{}, err
	}
	if err := markTimelineEdited(ctx, tx, project.ID, timeline.ID, command.ActorUserID); err != nil {
		return timelineClipDeleteActionResult{}, err
	}
	updatedTimeline, err := timelineByIDTx(ctx, tx, project.ID, timeline.ID, productionContext.Generation.ID, false)
	if err != nil {
		return timelineClipDeleteActionResult{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "timeline.clip.deleted", "timeline_clip", current.ID, mustRawJSON(map[string]any{
		"timelineId": timeline.ID, "clipId": current.ID, "revision": current.Revision, "timelineRevision": updatedTimeline.Revision,
		"controlCommandId": command.ID, "controllerType": command.ControllerType,
	})); err != nil {
		return timelineClipDeleteActionResult{}, err
	}
	return timelineClipDeleteActionResult{Deleted: true, TimelineID: timeline.ID, ClipID: current.ID, TimelineRevision: updatedTimeline.Revision}, nil
}

func (s *Server) reorderTimelineClipsActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	command projectcontrol.Command,
	input timelineClipReorderActionInput,
) (timelineClipReorderActionResult, error) {
	productionContext, err := lockActiveVideoProductionContext(ctx, tx, project.ID)
	if err != nil {
		return timelineClipReorderActionResult{}, err
	}
	timeline, err := timelineByIDTx(ctx, tx, project.ID, input.TimelineID, productionContext.Generation.ID, true)
	if err != nil {
		return timelineClipReorderActionResult{}, timelineActionNotFound(err)
	}
	if timeline.Revision != input.ExpectedTimelineRevision {
		return timelineClipReorderActionResult{}, revisionConflictError("TIMELINE_REVISION_CONFLICT", "时间线已被其他操作修改", input.ExpectedTimelineRevision, timeline.Revision)
	}
	currentItems, err := timelineClipsTx(ctx, tx, project.ID, timeline.ID, productionContext.Generation.ID, true)
	if err != nil {
		return timelineClipReorderActionResult{}, err
	}
	if len(currentItems) != len(input.Items) {
		return timelineClipReorderActionResult{}, newAPIError(http.StatusConflict, "TIMELINE_CLIP_SET_CHANGED", "时间线片段集合已变化，请刷新后重试")
	}
	currentByID := make(map[string]TimelineClip, len(currentItems))
	for _, item := range currentItems {
		currentByID[item.ID] = item
	}
	for _, item := range input.Items {
		current, exists := currentByID[item.ClipID]
		if !exists {
			return timelineClipReorderActionResult{}, newAPIError(http.StatusConflict, "TIMELINE_CLIP_SET_CHANGED", "时间线片段集合已变化，请刷新后重试")
		}
		if current.Revision != item.ExpectedRevision {
			return timelineClipReorderActionResult{}, revisionConflictError("TIMELINE_CLIP_REVISION_CONFLICT", "时间线片段已被其他操作修改", item.ExpectedRevision, current.Revision)
		}
	}
	if _, err := tx.Exec(ctx, `SET CONSTRAINTS timeline_clips_timeline_index_unique DEFERRED`); err != nil {
		return timelineClipReorderActionResult{}, err
	}
	ordered := append([]timelineClipReorderItem(nil), input.Items...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ClipIndex < ordered[j].ClipIndex })
	for _, item := range ordered {
		tag, err := tx.Exec(ctx, `
			UPDATE timeline_clips SET clip_index = $4
			WHERE project_id = $1 AND timeline_id = $2 AND id = $3
			  AND production_generation_id = $5 AND revision = $6
		`, project.ID, timeline.ID, item.ClipID, item.ClipIndex, productionContext.Generation.ID, item.ExpectedRevision)
		if err != nil {
			return timelineClipReorderActionResult{}, err
		}
		if tag.RowsAffected() == 0 {
			return timelineClipReorderActionResult{}, revisionConflictError("TIMELINE_CLIP_REVISION_CONFLICT", "时间线片段已被其他操作修改", item.ExpectedRevision, currentByID[item.ClipID].Revision)
		}
	}
	if err := reflowTimelineClipTicks(ctx, tx, timeline.ID); err != nil {
		return timelineClipReorderActionResult{}, err
	}
	if err := markTimelineEdited(ctx, tx, project.ID, timeline.ID, command.ActorUserID); err != nil {
		return timelineClipReorderActionResult{}, err
	}
	items, err := timelineClipsTx(ctx, tx, project.ID, timeline.ID, productionContext.Generation.ID, false)
	if err != nil {
		return timelineClipReorderActionResult{}, err
	}
	updatedTimeline, err := timelineByIDTx(ctx, tx, project.ID, timeline.ID, productionContext.Generation.ID, false)
	if err != nil {
		return timelineClipReorderActionResult{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "timeline.clip.reordered", "project_timeline", timeline.ID, mustRawJSON(map[string]any{
		"timelineId": timeline.ID, "revision": updatedTimeline.Revision, "clipCount": len(items),
		"controlCommandId": command.ID, "controllerType": command.ControllerType,
	})); err != nil {
		return timelineClipReorderActionResult{}, err
	}
	return timelineClipReorderActionResult{TimelineID: timeline.ID, TimelineRevision: updatedTimeline.Revision, Items: items}, nil
}

func timelineByIDTx(ctx context.Context, tx pgx.Tx, projectID, timelineID, generationID string, lock bool) (ProjectTimeline, error) {
	query := `
		SELECT id, organization_id, project_id, revision, workflow_run_id::text, title, status, aspect_ratio, resolution,
		       timeline_timebase, fps_numerator, fps_denominator,
		       metadata, created_by::text, edited_by::text, created_at, updated_at, edited_at
		FROM project_timelines
		WHERE project_id = $1 AND id = $2 AND production_generation_id = $3
	`
	if lock {
		query += " FOR UPDATE"
	}
	return scanProjectTimeline(tx.QueryRow(ctx, query, projectID, timelineID, generationID))
}

func timelineClipByIDTx(ctx context.Context, tx pgx.Tx, projectID, timelineID, clipID, generationID string, lock bool) (TimelineClip, error) {
	query := `
		SELECT ` + timelineClipColumns("c", "t") + `
		FROM timeline_clips c
		JOIN project_timelines t ON t.id = c.timeline_id
		WHERE c.project_id = $1 AND c.timeline_id = $2 AND c.id = $3
		  AND c.production_generation_id = $4 AND t.production_generation_id = c.production_generation_id
	`
	if lock {
		query += " FOR UPDATE OF c"
	}
	return scanTimelineClip(tx.QueryRow(ctx, query, projectID, timelineID, clipID, generationID))
}

func timelineClipsTx(ctx context.Context, tx pgx.Tx, projectID, timelineID, generationID string, lock bool) ([]TimelineClip, error) {
	query := `
		SELECT ` + timelineClipColumns("c", "t") + `
		FROM timeline_clips c
		JOIN project_timelines t ON t.id = c.timeline_id
		WHERE c.project_id = $1 AND c.timeline_id = $2
		  AND c.production_generation_id = $3 AND t.production_generation_id = c.production_generation_id
		ORDER BY c.clip_index ASC, c.id ASC
	`
	if lock {
		query += " FOR UPDATE OF c"
	}
	rows, err := tx.Query(ctx, query, projectID, timelineID, generationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]TimelineClip, 0)
	for rows.Next() {
		item, err := scanTimelineClip(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func applyTimelineClipPatch(current *TimelineClip, patch map[string]json.RawMessage) error {
	allowed := map[string]struct{}{
		"title": {}, "enabled": {}, "trimStartTick": {}, "trimEndTick": {}, "durationTicks": {}, "notes": {},
	}
	for field := range patch {
		if _, exists := allowed[field]; !exists {
			return controlValidationError("不支持修改时间线片段字段：" + field)
		}
	}
	if raw, ok := patch["title"]; ok {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return controlValidationError("title 必须为字符串")
		}
		current.Title = strings.TrimSpace(value)
	}
	if raw, ok := patch["enabled"]; ok {
		if err := json.Unmarshal(raw, &current.Enabled); err != nil {
			return controlValidationError("enabled 必须为布尔值")
		}
	}
	if raw, ok := patch["trimStartTick"]; ok {
		if err := json.Unmarshal(raw, &current.TrimStartTick); err != nil {
			return controlValidationError("trimStartTick 必须为整数")
		}
	}
	if raw, ok := patch["trimEndTick"]; ok {
		if isJSONNull(raw) {
			current.TrimEndTick = nil
		} else {
			var value int64
			if err := json.Unmarshal(raw, &value); err != nil {
				return controlValidationError("trimEndTick 必须为整数或 null")
			}
			current.TrimEndTick = &value
		}
	}
	if raw, ok := patch["durationTicks"]; ok {
		if err := json.Unmarshal(raw, &current.DurationTicks); err != nil {
			return controlValidationError("durationTicks 必须为整数")
		}
	}
	if raw, ok := patch["notes"]; ok {
		if isJSONNull(raw) {
			current.Notes = nil
		} else {
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return controlValidationError("notes 必须为字符串或 null")
			}
			current.Notes = stringPtrFromValue(strings.TrimSpace(value))
		}
	}
	return nil
}

func validateTimelineClipMediaOwnership(ctx context.Context, tx pgx.Tx, projectID, artifactID, mediaFileID string) error {
	if strings.TrimSpace(artifactID) != "" {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM artifacts WHERE id = $1 AND project_id = $2)`, artifactID, projectID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return newAPIError(http.StatusUnprocessableEntity, "TIMELINE_CLIP_ARTIFACT_INVALID", "视频产物不属于当前项目")
		}
	}
	if strings.TrimSpace(mediaFileID) != "" {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM media_files WHERE id = $1 AND project_id = $2)`, mediaFileID, projectID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return newAPIError(http.StatusUnprocessableEntity, "TIMELINE_CLIP_MEDIA_INVALID", "视频媒体不属于当前项目")
		}
	}
	return nil
}

func optionalTrimmedValue(value *string) any {
	if value == nil {
		return nil
	}
	return strings.TrimSpace(*value)
}

func timelineActionNotFound(err error) error {
	if err == pgx.ErrNoRows {
		return newAPIError(http.StatusNotFound, "TIMELINE_NOT_FOUND", "时间线不存在或不属于当前视频生产代")
	}
	return err
}

func timelineClipActionNotFound(err error) error {
	if err == pgx.ErrNoRows {
		return newAPIError(http.StatusNotFound, "TIMELINE_CLIP_NOT_FOUND", "时间线片段不存在或不属于当前视频生产代")
	}
	return err
}

func timelineClipReferenceError(err error) error {
	if err == pgx.ErrNoRows {
		return newAPIError(http.StatusUnprocessableEntity, "STORYBOARD_SHOT_NOT_FOUND", "分镜不存在或不属于当前视频生产代")
	}
	return err
}

func timelineAgentResult(name string, arguments map[string]any, item ProjectTimeline, summary string) agentToolResult {
	return agentToolOK(name, arguments, summary, map[string]any{
		"timeline": item, "timelineId": item.ID, "revision": item.Revision,
	})
}

func timelineClipAgentResult(name string, arguments map[string]any, item TimelineClip, summary string) agentToolResult {
	return agentToolOK(name, arguments, summary, map[string]any{
		"clip": item, "clipId": item.ID, "revision": item.Revision,
		"timelineId": item.TimelineID, "timelineRevision": item.TimelineRevision,
	})
}

func timelineDeleteAgentResult(arguments map[string]any, result timelineDeleteActionResult) agentToolResult {
	return agentToolOK("timeline.delete", arguments, "时间线已删除。", map[string]any{
		"deleted": result.Deleted, "timelineId": result.TimelineID, "revision": result.ExpectedRevision,
	})
}

func timelineClipDeleteAgentResult(arguments map[string]any, result timelineClipDeleteActionResult) agentToolResult {
	return agentToolOK("timeline.clip.delete", arguments, "时间线片段已删除。", map[string]any{
		"deleted": result.Deleted, "timelineId": result.TimelineID, "clipId": result.ClipID,
		"timelineRevision": result.TimelineRevision,
	})
}

func timelineClipReorderAgentResult(arguments map[string]any, result timelineClipReorderActionResult) agentToolResult {
	return agentToolOK("timeline.clip.reorder", arguments, fmt.Sprintf("已重排 %d 个时间线片段。", len(result.Items)), map[string]any{
		"timelineId": result.TimelineID, "timelineRevision": result.TimelineRevision, "items": result.Items,
	})
}

func (s *Server) executeTimelineCreateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateTimelineActionCommand(command, "timeline.create"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeTimelineCreateActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	item, err := s.createTimelineActionTx(ctx, tx, project, command, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return timelineAgentResult("timeline.create", rawArguments(raw), item, "时间线已创建。"), nil
}

func (s *Server) executeTimelineUpdateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateTimelineActionCommand(command, "timeline.update"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeTimelineUpdateActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	item, err := s.updateTimelineActionTx(ctx, tx, project, command, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return timelineAgentResult("timeline.update", rawArguments(raw), item, "时间线已更新。"), nil
}

func (s *Server) executeTimelineDeleteSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateTimelineActionCommand(command, "timeline.delete"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeTimelineDeleteActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	result, err := s.deleteTimelineActionTx(ctx, tx, project, command, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return timelineDeleteAgentResult(rawArguments(raw), result), nil
}

func (s *Server) executeTimelineClipCreateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateTimelineActionCommand(command, "timeline.clip.create"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeTimelineClipCreateActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	item, err := s.createTimelineClipActionTx(ctx, tx, project, command, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return timelineClipAgentResult("timeline.clip.create", rawArguments(raw), item, "时间线片段已创建。"), nil
}

func (s *Server) executeTimelineClipUpdateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateTimelineActionCommand(command, "timeline.update_clip"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeTimelineClipUpdateActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	item, err := s.updateTimelineClipActionTx(ctx, tx, project, command, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return timelineClipAgentResult("timeline.update_clip", rawArguments(raw), item, "时间线片段已更新。"), nil
}

func (s *Server) executeTimelineClipDeleteSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateTimelineActionCommand(command, "timeline.clip.delete"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeTimelineClipDeleteActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	result, err := s.deleteTimelineClipActionTx(ctx, tx, project, command, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return timelineClipDeleteAgentResult(rawArguments(raw), result), nil
}

func (s *Server) executeTimelineClipReorderSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateTimelineActionCommand(command, "timeline.clip.reorder"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeTimelineClipReorderActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	result, err := s.reorderTimelineClipsActionTx(ctx, tx, project, command, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return timelineClipReorderAgentResult(rawArguments(raw), result), nil
}

func rawArguments(raw json.RawMessage) map[string]any {
	arguments := make(map[string]any)
	_ = json.Unmarshal(raw, &arguments)
	return arguments
}

func (s *Server) createTimelineClip(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	var input timelineClipCreateActionInput
	if !decode(w, r, &input) {
		return
	}
	input.TimelineID = r.PathValue("timelineId")
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "timeline.clip.create", mustRawJSON(input), idempotencyKey(r, ""),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := decodeAgentToolResultValue[TimelineClip](result, "clip")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) updateTimelineClip(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	var body map[string]json.RawMessage
	if !decode(w, r, &body) {
		return
	}
	expectedTimelineRevision, err := takeRequiredInt64(body, "expectedTimelineRevision")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	expectedRevision, err := takeRequiredInt64(body, "expectedRevision")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	input := timelineClipUpdateActionInput{
		TimelineID: r.PathValue("timelineId"), ClipID: r.PathValue("clipId"),
		ExpectedTimelineRevision: expectedTimelineRevision, ExpectedRevision: expectedRevision, Patch: body,
	}
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "timeline.update_clip", mustRawJSON(input), idempotencyKey(r, ""),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := decodeAgentToolResultValue[TimelineClip](result, "clip")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) deleteTimelineClip(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedTimelineRevision int64 `json:"expectedTimelineRevision"`
		ExpectedRevision         int64 `json:"expectedRevision"`
	}
	if !decode(w, r, &req) {
		return
	}
	input := timelineClipDeleteActionInput{
		TimelineID: r.PathValue("timelineId"), ClipID: r.PathValue("clipId"),
		ExpectedTimelineRevision: req.ExpectedTimelineRevision, ExpectedRevision: req.ExpectedRevision,
	}
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "timeline.clip.delete", mustRawJSON(input), idempotencyKey(r, ""),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	deleted, err := decodeAgentToolResultValue[bool](result, "deleted")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	timelineRevision, err := decodeAgentToolResultValue[int64](result, "timelineRevision")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"deleted": deleted, "clipId": r.PathValue("clipId"), "timelineRevision": timelineRevision,
	}, nil)
}

func (s *Server) reorderTimelineClips(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	var input timelineClipReorderActionInput
	if !decode(w, r, &input) {
		return
	}
	input.TimelineID = r.PathValue("timelineId")
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "timeline.clip.reorder", mustRawJSON(input), idempotencyKey(r, ""),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	items, err := decodeAgentToolResultValue[[]TimelineClip](result, "items")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	timelineRevision, err := decodeAgentToolResultValue[int64](result, "timelineRevision")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, timelineClipReorderActionResult{
		TimelineID: r.PathValue("timelineId"), TimelineRevision: timelineRevision, Items: items,
	}, nil)
}

func takeRequiredInt64(body map[string]json.RawMessage, field string) (int64, error) {
	raw, exists := body[field]
	if !exists {
		return 0, controlValidationError(field + " 为必填项")
	}
	delete(body, field)
	var value int64
	if err := json.Unmarshal(raw, &value); err != nil || value < 1 {
		return 0, controlValidationError(field + " 必须为正整数")
	}
	return value, nil
}
