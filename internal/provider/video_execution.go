package provider

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/events"
	"github.com/jackc/pgx/v5"
)

type videoExecutionSegment struct {
	ExecutionPlanID        string
	RenderSegmentID        string
	ProviderAccountID      string
	ProviderModelID        string
	ModelProfileID         string
	ModelProfileBindingID  string
	ModelProfileKey        string
	VariantKey             string
	CapabilitySnapshotHash string
	RequestedDuration      float64
	SegmentIndex           int
	ContinuityMode         string
	ReferenceMode          string
	AspectRatio            string
	Resolution             string
	AudioRequirement       string
	Status                 string
	ProviderAsyncTaskID    string
	ProviderCallID         string
	ExternalTaskID         string
	ProviderTaskStatus     string
}

func (s *Service) validateVideoExecutionRequest(ctx context.Context, req *GatewayVideoCreateTaskRequest, input gatewayVideoInput) (*videoExecutionSegment, error) {
	planID := strings.TrimSpace(req.ExecutionPlanID)
	segmentID := strings.TrimSpace(req.RenderSegmentID)
	hash := strings.TrimSpace(req.CapabilitySnapshotHash)
	if planID == "" && segmentID == "" && hash == "" {
		if strings.TrimSpace(req.WorkflowRunID) != "" || strings.TrimSpace(req.NodeRunID) != "" {
			return nil, &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "production video tasks require executionPlanId, renderSegmentId, and capabilitySnapshotHash", Retryable: false}}
		}
		return nil, nil
	}
	if planID == "" || segmentID == "" || hash == "" {
		return nil, &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "executionPlanId, renderSegmentId, and capabilitySnapshotHash must be provided together", Retryable: false}}
	}
	var segment videoExecutionSegment
	var modelProfileID, bindingID, modelProfileKey, providerTaskID, providerCallID, externalTaskID, providerTaskStatus sql.NullString
	var expiresAt time.Time
	if err := s.db.QueryRow(ctx, `
		SELECT plan.id::text, segment.id::text, model.provider_account_id::text, model.id::text,
		       plan.model_profile_id::text, plan.model_profile_binding_id::text, plan.model_profile_key,
		       plan.variant_key, plan.capability_snapshot_hash, segment.requested_duration_seconds::float8,
		       segment.segment_index, segment.continuity_mode,
		       plan.reference_mode, plan.aspect_ratio, plan.resolution, plan.audio_requirement,
		       plan.expires_at, segment.status,
		       segment.provider_async_task_id::text, segment.provider_call_id::text, segment.external_task_id,
		       task.status
		FROM video_render_plans plan
		JOIN video_render_segments segment ON segment.video_render_plan_id = plan.id
		JOIN provider_models model ON model.id = COALESCE(segment.provider_model_id, plan.provider_model_id)
		JOIN provider_accounts account ON account.id = model.provider_account_id
		LEFT JOIN provider_async_tasks task ON task.id = segment.provider_async_task_id
		WHERE plan.id = $1 AND segment.id = $2 AND plan.organization_id = $3
		  AND plan.active = true AND plan.status NOT IN ('stale', 'archived', 'cancelled', 'replan_required')
		  AND account.status = 'active' AND model.status = 'active'
	`, planID, segmentID, req.OrganizationID).Scan(
		&segment.ExecutionPlanID, &segment.RenderSegmentID, &segment.ProviderAccountID, &segment.ProviderModelID,
		&modelProfileID, &bindingID, &modelProfileKey, &segment.VariantKey, &segment.CapabilitySnapshotHash,
		&segment.RequestedDuration, &segment.SegmentIndex, &segment.ContinuityMode,
		&segment.ReferenceMode, &segment.AspectRatio, &segment.Resolution,
		&segment.AudioRequirement, &expiresAt, &segment.Status,
		&providerTaskID, &providerCallID, &externalTaskID, &providerTaskStatus,
	); err != nil {
		return nil, &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "video execution plan or segment is no longer active", Retryable: false}}
	}
	segment.ModelProfileID = nullStringText(modelProfileID)
	segment.ModelProfileBindingID = nullStringText(bindingID)
	segment.ModelProfileKey = nullStringText(modelProfileKey)
	segment.ProviderAsyncTaskID = nullStringText(providerTaskID)
	segment.ProviderCallID = nullStringText(providerCallID)
	segment.ExternalTaskID = nullStringText(externalTaskID)
	segment.ProviderTaskStatus = nullStringText(providerTaskStatus)
	if time.Now().UTC().After(expiresAt) {
		return nil, &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "video execution plan expired before task creation", Retryable: false}}
	}
	if hash != segment.CapabilitySnapshotHash {
		return nil, &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "video capability snapshot hash does not match the execution plan", Retryable: false}}
	}
	model, err := s.GetModel(ctx, req.OrganizationID, segment.ProviderModelID)
	if err != nil {
		return nil, err
	}
	variants, err := videoGenerationVariants(model.Capabilities, model)
	if err != nil {
		return nil, err
	}
	currentHash := ""
	for _, variant := range variants {
		if variant.VariantKey != segment.VariantKey {
			continue
		}
		variant.NativeAudio.Support = normalizeVideoSupport(variant.NativeAudio.Support)
		currentHash, err = capabilitySnapshotHash(variant)
		if err != nil {
			return nil, err
		}
		break
	}
	if currentHash == "" || currentHash != segment.CapabilitySnapshotHash {
		return nil, &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "video model capabilities changed after the execution plan was created", Retryable: false}}
	}
	if math.Abs(input.DurationSeconds-segment.RequestedDuration) > 0.001 || !equalVideoOption(input.AspectRatio, segment.AspectRatio) || !equalVideoOption(input.Resolution, segment.Resolution) {
		return nil, &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "video task input does not match the planned segment capability snapshot", Retryable: false}}
	}
	expectedMode := "image_to_video"
	if segment.ReferenceMode == "none" {
		expectedMode = "text_to_video"
	}
	if input.Mode != "" && !equalVideoOption(input.Mode, expectedMode) {
		return nil, &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "video task mode does not match the planned reference mode", Retryable: false}}
	}
	if segment.ContinuityMode != "none" && len(req.References) == 0 {
		return nil, &StandardErrorError{Standard: StandardError{Code: CodeInvalidRequest, Message: "planned video segment requires a continuity reference", Retryable: false}}
	}
	if segment.SegmentIndex > 0 && segment.ContinuityMode != "none" {
		var previousArtifactID sql.NullString
		if err := s.db.QueryRow(ctx, `
			SELECT artifact_id::text
			FROM video_render_segments
			WHERE video_render_plan_id = $1 AND segment_index = $2 AND status = 'succeeded'
		`, segment.ExecutionPlanID, segment.SegmentIndex-1).Scan(&previousArtifactID); err != nil || !videoReferencesContainArtifact(req.References, nullStringText(previousArtifactID)) {
			return nil, &StandardErrorError{Standard: StandardError{Code: CodeInvalidRequest, Message: "continuation segment requires the succeeded previous segment artifact as a reference", Retryable: false}}
		}
	}
	if strings.TrimSpace(req.ProviderModelID) != "" && req.ProviderModelID != segment.ProviderModelID {
		return nil, &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "providerModelId does not match the execution plan", Retryable: false}}
	}
	req.ProviderModelID = segment.ProviderModelID
	req.ModelProfileKey = segment.ModelProfileKey
	return &segment, nil
}

func equalVideoOption(actual, planned string) bool {
	return strings.EqualFold(strings.TrimSpace(actual), strings.TrimSpace(planned))
}

func videoReferencesContainArtifact(references []GatewayVideoReference, artifactID string) bool {
	if strings.TrimSpace(artifactID) == "" {
		return false
	}
	for _, reference := range references {
		if strings.TrimSpace(reference.ArtifactID) == artifactID {
			return true
		}
	}
	return false
}

func updateVideoRenderSegmentCreateTx(ctx context.Context, tx pgx.Tx, req GatewayVideoCreateTaskRequest, providerCallID, providerTaskID, externalTaskID, status, errorCode, errorMessage string, stored *gatewayStoredVideo) error {
	if strings.TrimSpace(req.RenderSegmentID) == "" || strings.TrimSpace(req.ExecutionPlanID) == "" {
		return nil
	}
	segmentStatus := normalizeVideoRenderSegmentStatus(status)
	var artifactID, mediaFileID, storageKey any
	if stored != nil {
		artifactID = nullString(stored.ArtifactID)
		mediaFileID = nullString(stored.MediaFileID)
		storageKey = nullString(stored.Output.StorageKey)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE video_render_segments
		SET status = $3,
		    provider_async_task_id = NULLIF($4, '')::uuid,
		    provider_call_id = NULLIF($5, '')::uuid,
		    provider_model_id = NULLIF($6, '')::uuid,
		    external_task_id = NULLIF($7, ''),
		    artifact_id = COALESCE($8::uuid, artifact_id),
		    media_file_id = COALESCE($9::uuid, media_file_id),
		    storage_key = COALESCE($10::text, storage_key),
		    error_code = NULLIF($11, ''), error_message = NULLIF($12, ''),
		    started_at = COALESCE(started_at, now()),
		    completed_at = CASE WHEN $3 IN ('succeeded', 'failed', 'cancelled') THEN now() ELSE NULL END,
		    updated_at = now()
		WHERE id = $1 AND video_render_plan_id = $2
	`, req.RenderSegmentID, req.ExecutionPlanID, segmentStatus, providerTaskID, providerCallID, req.ProviderModelID,
		externalTaskID, artifactID, mediaFileID, storageKey, errorCode, errorMessage); err != nil {
		return err
	}
	if err := insertVideoRenderSegmentEvent(ctx, tx, req.OrganizationID, req.ProjectID, req.ExecutionPlanID, req.RenderSegmentID, segmentStatus, providerTaskID, providerCallID, errorMessage); err != nil {
		return err
	}
	return refreshVideoRenderPlanStateTx(ctx, tx, req.ExecutionPlanID)
}

func updateVideoRenderSegmentPollTx(ctx context.Context, tx pgx.Tx, task gatewayVideoTask, providerCallID, status, errorCode, errorMessage string, stored *gatewayStoredVideo) error {
	if task.RenderSegmentID == "" || task.ExecutionPlanID == "" {
		return nil
	}
	segmentStatus := normalizeVideoRenderSegmentStatus(status)
	var artifactID, mediaFileID, storageKey any
	var audioDetected any
	if stored != nil {
		artifactID = nullString(stored.ArtifactID)
		mediaFileID = nullString(stored.MediaFileID)
		storageKey = nullString(stored.Output.StorageKey)
		if stored.Output.MediaProbe != nil {
			audioDetected = stored.Output.MediaProbe.HasAudio
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE video_render_segments segment
		SET status = $3,
		    provider_call_id = NULLIF($4, '')::uuid,
		    artifact_id = COALESCE($5::uuid, artifact_id),
		    media_file_id = COALESCE($6::uuid, media_file_id),
		    storage_key = COALESCE($7::text, storage_key),
		    native_audio_detected = COALESCE($8::boolean, native_audio_detected),
		    audio_verification_status = CASE
		      WHEN $3 <> 'succeeded' THEN audio_verification_status
		      WHEN NOT segment.native_audio_requested THEN 'not_requested'
		      WHEN COALESCE($8::boolean, false) THEN 'audio_unverified'
		      WHEN plan.audio_requirement = 'required' THEN 'needs_audio_retry'
		      ELSE 'native_audio_unavailable'
		    END,
		    production_readiness = CASE
		      WHEN $3 <> 'succeeded' THEN 'blocked'
		      WHEN NOT segment.native_audio_requested THEN 'ready'
		      ELSE 'preview_only'
		    END,
		    error_code = NULLIF($9, ''), error_message = NULLIF($10, ''),
		    completed_at = CASE WHEN $3 IN ('succeeded', 'failed', 'cancelled') THEN COALESCE(segment.completed_at, now()) ELSE segment.completed_at END,
		    updated_at = now()
		FROM video_render_plans plan
		WHERE segment.id = $1 AND segment.video_render_plan_id = $2 AND plan.id = segment.video_render_plan_id
	`, task.RenderSegmentID, task.ExecutionPlanID, segmentStatus, providerCallID, artifactID, mediaFileID, storageKey, audioDetected, errorCode, errorMessage); err != nil {
		return err
	}
	if err := insertVideoRenderSegmentEvent(ctx, tx, task.OrganizationID, task.ProjectID, task.ExecutionPlanID, task.RenderSegmentID, segmentStatus, task.ID, providerCallID, errorMessage); err != nil {
		return err
	}
	return refreshVideoRenderPlanStateTx(ctx, tx, task.ExecutionPlanID)
}

func updateVideoRenderSegmentCancelTx(ctx context.Context, tx pgx.Tx, task gatewayVideoTask, providerCallID, status, errorMessage string) error {
	if task.RenderSegmentID == "" || task.ExecutionPlanID == "" {
		return nil
	}
	segmentStatus := "cancelled"
	if status != "cancelled" {
		segmentStatus = "running"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE video_render_segments
		SET status = $3, provider_call_id = NULLIF($4, '')::uuid,
		    error_code = CASE WHEN $3 = 'cancelled' THEN NULL ELSE 'PROVIDER_CANCEL_FAILED' END,
		    error_message = NULLIF($5, ''),
		    completed_at = CASE WHEN $3 = 'cancelled' THEN COALESCE(completed_at, now()) ELSE completed_at END,
		    updated_at = now()
		WHERE id = $1 AND video_render_plan_id = $2
	`, task.RenderSegmentID, task.ExecutionPlanID, segmentStatus, providerCallID, errorMessage); err != nil {
		return err
	}
	if err := insertVideoRenderSegmentEvent(ctx, tx, task.OrganizationID, task.ProjectID, task.ExecutionPlanID, task.RenderSegmentID, segmentStatus, task.ID, providerCallID, errorMessage); err != nil {
		return err
	}
	return refreshVideoRenderPlanStateTx(ctx, tx, task.ExecutionPlanID)
}

func refreshVideoRenderPlanStateTx(ctx context.Context, tx pgx.Tx, planID string) error {
	var status, readiness, audioStatus string
	if err := tx.QueryRow(ctx, `
		WITH stats AS (
		  SELECT
		    count(*)::integer AS total,
		    count(*) FILTER (WHERE status = 'succeeded')::integer AS succeeded,
		    count(*) FILTER (WHERE status IN ('failed', 'cancelled'))::integer AS failed,
		    count(*) FILTER (WHERE status IN ('queued', 'running'))::integer AS running,
		    count(*) FILTER (WHERE production_readiness = 'ready')::integer AS ready,
		    count(*) FILTER (WHERE production_readiness = 'preview_only')::integer AS preview,
		    count(*) FILTER (WHERE audio_verification_status = 'needs_audio_retry')::integer AS audio_retry,
		    count(*) FILTER (WHERE audio_verification_status = 'native_audio_unavailable')::integer AS audio_unavailable,
		    count(*) FILTER (WHERE audio_verification_status = 'audio_unverified')::integer AS audio_unverified,
		    count(*) FILTER (WHERE audio_verification_status = 'audio_verified')::integer AS audio_verified
		  FROM video_render_segments WHERE video_render_plan_id = $1
		), resolved AS (
		  SELECT *,
		    CASE
		      WHEN total > 0 AND succeeded = total THEN 'succeeded'
		      WHEN failed > 0 AND succeeded > 0 AND succeeded + failed = total THEN 'partial_succeeded'
		      WHEN total > 0 AND failed = total THEN 'failed'
		      WHEN running > 0 OR succeeded > 0 THEN 'running'
		      ELSE 'planned'
		    END AS next_status,
		    CASE
		      WHEN total > 0 AND ready = total THEN 'ready'
		      WHEN total > 0 AND succeeded = total AND preview > 0 THEN 'preview_only'
		      WHEN failed > 0 AND succeeded > 0 THEN 'partial'
		      ELSE 'blocked'
		    END AS next_readiness,
		    CASE
		      WHEN audio_retry > 0 THEN 'needs_audio_retry'
		      WHEN audio_unavailable > 0 THEN 'native_audio_unavailable'
		      WHEN audio_unverified > 0 THEN 'audio_unverified'
		      WHEN audio_verified > 0 THEN 'audio_verified'
		      ELSE 'not_requested'
		    END AS next_audio_status
		  FROM stats
		)
		UPDATE video_render_plans plan
		SET status = resolved.next_status,
		    production_readiness = resolved.next_readiness,
		    native_audio_status = resolved.next_audio_status,
		    completed_at = CASE WHEN resolved.next_status IN ('succeeded', 'partial_succeeded', 'failed', 'cancelled') THEN COALESCE(completed_at, now()) ELSE NULL END,
		    updated_at = now()
		FROM resolved
		WHERE plan.id = $1
		RETURNING plan.status, plan.production_readiness, plan.native_audio_status
	`, planID).Scan(&status, &readiness, &audioStatus); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE storyboard_shots shot
		SET video_status = CASE
		      WHEN $2 = 'succeeded' THEN 'succeeded'
		      WHEN $2 = 'partial_succeeded' THEN 'partial_succeeded'
		      WHEN $2 = 'failed' THEN 'failed'
		      WHEN $2 IN ('running', 'planned') THEN 'running'
		      ELSE video_status
		    END,
		    status = CASE
		      WHEN $2 = 'succeeded' THEN 'video_succeeded'
		      WHEN $2 IN ('partial_succeeded', 'failed') THEN 'video_failed'
		      WHEN $2 IN ('running', 'planned') THEN 'video_running'
		      ELSE status
		    END,
		    production_readiness = $3,
		    native_audio_status = $4,
		    stale_state = CASE WHEN $2 = 'succeeded' THEN 'fresh' ELSE 'needs_regeneration' END,
		    updated_at = now()
		WHERE active_video_render_plan_id = $1
	`, planID, status, readiness, audioStatus)
	return err
}

func insertVideoRenderSegmentEvent(ctx context.Context, tx pgx.Tx, organizationID, projectID, planID, segmentID, status, providerTaskID, providerCallID, errorMessage string) error {
	eventType, err := videoRenderSegmentEventType(status)
	if err != nil {
		return err
	}
	var workflowRunID sql.NullString
	if err := tx.QueryRow(ctx, `SELECT workflow_run_id::text FROM video_render_plans WHERE id = $1`, planID).Scan(&workflowRunID); err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"executionPlanId":     planID,
		"renderSegmentId":     segmentID,
		"status":              status,
		"providerAsyncTaskId": providerTaskID,
		"providerCallId":      providerCallID,
		"errorMessage":        errorMessage,
		"workflowRunId":       workflowRunID.String,
	})
	if err != nil {
		return err
	}
	return events.AppendTx(ctx, tx, organizationID, projectID, eventType, "video_render_segment", segmentID, payload)
}

func videoRenderSegmentEventType(status string) (string, error) {
	switch strings.TrimSpace(status) {
	case "planned":
		return "storyboard.segment.planned", nil
	case "retry_planned":
		return "storyboard.segment.retry_planned", nil
	case "queued":
		return "storyboard.segment.queued", nil
	case "running":
		return "storyboard.segment.running", nil
	case "succeeded":
		return "storyboard.segment.succeeded", nil
	case "failed":
		return "storyboard.segment.failed", nil
	case "cancelled":
		return "storyboard.segment.cancelled", nil
	default:
		return "", fmt.Errorf("%w: unsupported video render segment event status %q", ErrValidation, status)
	}
}

func normalizeVideoRenderSegmentStatus(status string) string {
	switch normalizeGatewayVideoStatus(status) {
	case "queued":
		return "queued"
	case "running":
		return "running"
	case "succeeded":
		return "succeeded"
	case "cancelled":
		return "cancelled"
	default:
		return "failed"
	}
}
