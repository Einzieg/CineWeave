package provider

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

type videoFallbackCandidate struct {
	ProviderModelID        string `json:"providerModelId"`
	ProviderAccountID      string `json:"providerAccountId"`
	ModelFamily            string `json:"modelFamily"`
	VariantKey             string `json:"variantKey"`
	CapabilitySnapshotHash string `json:"capabilitySnapshotHash"`
}

const maxVideoAttemptsForSingleCandidate = 3

// RetryVideoRenderSegment prepares a new provider attempt without changing the
// immutable narrative timing or discarding successful sibling segments.
func (s *Service) RetryVideoRenderSegment(ctx context.Context, req GatewayVideoRetrySegmentRequest) (GatewayVideoRetrySegmentResponse, error) {
	req.OrganizationID = strings.TrimSpace(req.OrganizationID)
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.WorkflowRunID = strings.TrimSpace(req.WorkflowRunID)
	req.NodeRunID = strings.TrimSpace(req.NodeRunID)
	req.NodeExecutionToken = strings.TrimSpace(req.NodeExecutionToken)
	req.ExecutionPlanID = strings.TrimSpace(req.ExecutionPlanID)
	req.RenderSegmentID = strings.TrimSpace(req.RenderSegmentID)
	if err := s.validateGatewayVideoProductionIdentity(
		ctx, req.OrganizationID, req.ProjectID, req.ProductionGenerationID, req.VideoProductionBindingID,
		req.VideoProductionBindingRevision, req.WorkflowRunID, req.NodeRunID,
	); err != nil {
		return GatewayVideoRetrySegmentResponse{}, err
	}
	if req.OrganizationID == "" || req.ProjectID == "" || req.WorkflowRunID == "" || req.NodeRunID == "" || req.NodeExecutionToken == "" || req.NodeAttemptGeneration <= 0 || req.ExecutionPlanID == "" || req.RenderSegmentID == "" {
		return GatewayVideoRetrySegmentResponse{}, fmt.Errorf("%w: organizationId, projectId, workflowRunId, node execution identity, executionPlanId, and renderSegmentId are required", ErrValidation)
	}
	if err := s.validateGatewayVideoNodeExecution(
		ctx,
		req.OrganizationID,
		req.ProjectID,
		req.WorkflowRunID,
		req.NodeRunID,
		req.NodeExecutionToken,
		req.NodeAttemptGeneration,
	); err != nil {
		return GatewayVideoRetrySegmentResponse{}, err
	}
	if !videoSegmentFailureRetryable(req.FailureCode) {
		return GatewayVideoRetrySegmentResponse{}, &StandardErrorError{Standard: StandardError{
			Code: CodeInvalidRequest, Message: "video render segment failure is not eligible for automatic provider retry", Retryable: false,
		}}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return GatewayVideoRetrySegmentResponse{}, err
	}
	defer tx.Rollback(ctx)
	if err := assertGatewayVideoProductionIdentityTx(ctx, tx, req.OrganizationID, req.ProjectID, videoProductionIdentity(
		req.ProductionGenerationID, req.VideoProductionBindingID, req.VideoProductionBindingRevision,
	)); err != nil {
		return GatewayVideoRetrySegmentResponse{}, err
	}
	if err := lockGatewayVideoNodeExecutionTx(ctx, tx, req.NodeRunID, req.NodeExecutionToken, req.NodeAttemptGeneration); err != nil {
		return GatewayVideoRetrySegmentResponse{}, err
	}

	var projectID, family, snapshotHash, variantKey, status string
	var currentModelID sql.NullString
	var retryGeneration int
	var fallbackRaw, metadataRaw []byte
	if err := tx.QueryRow(ctx, `
		SELECT plan.project_id::text, plan.model_family, plan.capability_snapshot_hash, plan.variant_key,
		       plan.fallback_candidates, segment.provider_model_id::text,
		       segment.retry_generation, segment.status, segment.metadata
		FROM video_render_plans plan
		JOIN video_render_segments segment ON segment.video_render_plan_id = plan.id
		WHERE plan.id = $1 AND segment.id = $2 AND plan.organization_id = $3 AND plan.project_id = $4
		  AND plan.active = true AND plan.status NOT IN ('stale', 'archived', 'cancelled', 'replan_required')
		FOR UPDATE OF plan, segment
	`, req.ExecutionPlanID, req.RenderSegmentID, req.OrganizationID, req.ProjectID).Scan(
		&projectID, &family, &snapshotHash, &variantKey, &fallbackRaw, &currentModelID, &retryGeneration, &status, &metadataRaw,
	); err != nil {
		return GatewayVideoRetrySegmentResponse{}, &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "video render plan or segment is no longer retryable", Retryable: false}}
	}
	if status != "failed" && status != "cancelled" {
		return GatewayVideoRetrySegmentResponse{}, fmt.Errorf("%w: only failed or cancelled render segments can be retried", ErrValidation)
	}

	var candidates []videoFallbackCandidate
	if err := json.Unmarshal(fallbackRaw, &candidates); err != nil {
		return GatewayVideoRetrySegmentResponse{}, fmt.Errorf("%w: render plan fallback candidates are invalid", ErrValidation)
	}
	attempted := attemptedProviderModels(metadataRaw)
	current := nullStringText(currentModelID)
	if current != "" {
		attempted[current] = true
	}

	selected := selectVideoRetryCandidate(
		candidates,
		attempted,
		current,
		retryGeneration,
		family,
		variantKey,
		snapshotHash,
		func(candidate videoFallbackCandidate) bool {
			return s.videoRetryCandidateActive(ctx, req.OrganizationID, candidate)
		},
	)
	if selected.ProviderModelID == "" {
		for _, candidate := range candidates {
			if attempted[candidate.ProviderModelID] || !s.videoRetryCandidateActive(ctx, req.OrganizationID, candidate) {
				continue
			}
			return GatewayVideoRetrySegmentResponse{}, &StandardErrorError{Standard: StandardError{
				Code: CodeRenderPlanReplanRequired, Message: "remaining video fallback candidates require a whole-shot render plan revision", Retryable: false,
			}}
		}
		return GatewayVideoRetrySegmentResponse{}, &StandardErrorError{Standard: StandardError{
			Code: CodeModelCapabilityUnavailable, Message: "no active video fallback candidate remains for this render segment", Retryable: false,
		}}
	}
	attempted[selected.ProviderModelID] = true
	attemptedList := make([]string, 0, len(attempted))
	for modelID := range attempted {
		attemptedList = append(attemptedList, modelID)
	}
	attemptedJSON, _ := json.Marshal(attemptedList)
	var nextGeneration int
	if err := tx.QueryRow(ctx, `
		UPDATE video_render_segments
		SET provider_model_id = $3,
		    status = 'planned', retry_generation = retry_generation + 1,
		    provider_async_task_id = NULL, provider_call_id = NULL, external_task_id = NULL,
		    artifact_id = NULL, media_file_id = NULL, storage_key = NULL,
		    native_audio_detected = NULL, audio_verification_status = CASE WHEN native_audio_requested THEN 'audio_unverified' ELSE 'not_requested' END,
		    production_readiness = 'blocked', error_code = NULL, error_message = NULL,
		    started_at = NULL, completed_at = NULL,
		    metadata = metadata || jsonb_build_object(
		      'attemptedProviderModelIds', $4::jsonb,
		      'lastFailureCode', NULLIF($5, ''),
		      'lastFailureMessage', NULLIF($6, ''),
		      'retryPreparedAt', now()
		    ),
		    updated_at = now()
		WHERE id = $1 AND video_render_plan_id = $2
		RETURNING retry_generation
	`, req.RenderSegmentID, req.ExecutionPlanID, selected.ProviderModelID, attemptedJSON, strings.TrimSpace(req.FailureCode), strings.TrimSpace(req.FailureMessage)).Scan(&nextGeneration); err != nil {
		return GatewayVideoRetrySegmentResponse{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE video_render_plans SET status = 'running', production_readiness = 'blocked', completed_at = NULL, updated_at = now()
		WHERE id = $1
	`, req.ExecutionPlanID); err != nil {
		return GatewayVideoRetrySegmentResponse{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots SET video_status = 'running', status = 'video_running', production_readiness = 'blocked',
		       video_error_code = NULL, video_error_message = NULL, updated_at = now()
		WHERE active_video_render_plan_id = $1
	`, req.ExecutionPlanID); err != nil {
		return GatewayVideoRetrySegmentResponse{}, err
	}
	if err := insertVideoRenderSegmentEvent(ctx, tx, req.OrganizationID, projectID, req.ExecutionPlanID, req.RenderSegmentID, "retry_planned", "", "", req.FailureMessage); err != nil {
		return GatewayVideoRetrySegmentResponse{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return GatewayVideoRetrySegmentResponse{}, err
	}
	return GatewayVideoRetrySegmentResponse{
		ExecutionPlanID: req.ExecutionPlanID, RenderSegmentID: req.RenderSegmentID,
		ProviderModelID: selected.ProviderModelID, ProviderAccountID: selected.ProviderAccountID,
		CapabilitySnapshotHash: snapshotHash, RetryGeneration: nextGeneration, RetryScope: "segment",
	}, nil
}

func selectVideoRetryCandidate(
	candidates []videoFallbackCandidate,
	attempted map[string]bool,
	current string,
	retryGeneration int,
	family string,
	variantKey string,
	snapshotHash string,
	isActive func(videoFallbackCandidate) bool,
) videoFallbackCandidate {
	currentCandidate := videoFallbackCandidate{}
	for _, candidate := range candidates {
		if candidate.ProviderModelID != current || !sameVideoCapabilityFamily(candidate, family, variantKey, snapshotHash) || !isActive(candidate) {
			continue
		}
		currentCandidate = candidate
		break
	}

	// The first retry stays on the selected model. This absorbs transient queue
	// and transport failures without invalidating the immutable render plan.
	if retryGeneration == 0 && currentCandidate.ProviderModelID != "" {
		return currentCandidate
	}
	for _, candidate := range candidates {
		if attempted[candidate.ProviderModelID] || !sameVideoCapabilityFamily(candidate, family, variantKey, snapshotHash) || !isActive(candidate) {
			continue
		}
		return candidate
	}
	// If this plan has no compatible fallback, spend the final bounded attempt
	// on the current model instead of failing after one transient retry.
	if retryGeneration < maxVideoAttemptsForSingleCandidate-1 && currentCandidate.ProviderModelID != "" {
		return currentCandidate
	}
	return videoFallbackCandidate{}
}

func sameVideoCapabilityFamily(candidate videoFallbackCandidate, family, variantKey, hash string) bool {
	return strings.EqualFold(strings.TrimSpace(candidate.ModelFamily), strings.TrimSpace(family)) &&
		candidate.VariantKey == variantKey && candidate.CapabilitySnapshotHash == hash
}

func (s *Service) videoRetryCandidateActive(ctx context.Context, organizationID string, candidate videoFallbackCandidate) bool {
	var count int
	err := s.db.QueryRow(ctx, `
		SELECT count(*)
		FROM provider_models model
		JOIN provider_accounts account ON account.id = model.provider_account_id
		WHERE model.id = $1 AND account.id = $2 AND account.organization_id = $3
		  AND model.status = 'active' AND account.status = 'active'
	`, candidate.ProviderModelID, candidate.ProviderAccountID, organizationID).Scan(&count)
	return err == nil && count == 1
}

func attemptedProviderModels(raw []byte) map[string]bool {
	result := map[string]bool{}
	var metadata map[string]json.RawMessage
	if json.Unmarshal(raw, &metadata) != nil {
		return result
	}
	var values []string
	if json.Unmarshal(metadata["attemptedProviderModelIds"], &values) == nil {
		for _, value := range values {
			if value = strings.TrimSpace(value); value != "" {
				result[value] = true
			}
		}
	}
	return result
}

func videoSegmentFailureRetryable(code string) bool {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "", CodeProviderRateLimited, CodeProviderConcurrencyLimited, CodeUpstreamTimeout, CodeUpstreamInternalError, CodeUpstreamStreamTruncated, CodeUpstreamOutputMismatch, CodePollingTimeout, "PROVIDER_VIDEO_POLLING_TIMEOUT", CodeMediaDownloadFailed, CodeProviderCircuitOpen:
		return true
	default:
		return false
	}
}

// VideoSegmentFailureRetryable is the shared retry policy for the workflow and
// Provider Gateway. Keeping one policy prevents deterministic contract errors
// from creating provider retry attempts.
func VideoSegmentFailureRetryable(code string) bool {
	return videoSegmentFailureRetryable(code)
}
