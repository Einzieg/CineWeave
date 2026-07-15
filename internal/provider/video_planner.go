package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/events"
	"github.com/jackc/pgx/v5"
)

const (
	defaultVideoRenderPlanTTL   = 6 * time.Hour
	maximumVideoRenderPlanTTL   = 24 * time.Hour
	minimumReusableVideoPlanTTL = 5 * time.Minute
)

type resolvedVideoPlanCandidate struct {
	ModelProfileID        string
	ModelProfileBindingID string
	ModelProfileKey       string
	ProviderAccountID     string
	Model                 Model
	RoutingIndex          int
	FallbackStrategy      FallbackStrategy
}

type matchedVideoPlanCandidate struct {
	Candidate           resolvedVideoPlanCandidate
	Variant             VideoGenerationVariant
	SnapshotHash        string
	AudioScore          int
	NativeAudioStatus   string
	ProductionReadiness string
	Segments            []GatewayVideoPlanSegment
}

type videoPlanShotState struct {
	StoryboardPlanID string
	DurationTicks    int64
	TimingRevision   int
}

func (s *Service) PlanVideo(ctx context.Context, req GatewayVideoPlanRequest) (GatewayVideoPlanResponse, error) {
	if err := s.validateGatewayVideoNodeExecution(
		ctx,
		req.OrganizationID,
		req.ProjectID,
		req.WorkflowRunID,
		req.NodeRunID,
		req.NodeExecutionToken,
		req.NodeAttemptGeneration,
	); err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	shotState, err := s.validateVideoPlanRequest(ctx, &req)
	if err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	if req.StoryboardPlanID == "" {
		req.StoryboardPlanID = shotState.StoryboardPlanID
	}
	candidates, err := s.resolveVideoPlanCandidates(ctx, req)
	if err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	matchReq := videoVariantMatchRequest{
		TaskType: req.TaskType, ReferenceMode: req.ReferenceMode, AspectRatio: req.AspectRatio,
		Resolution: req.Resolution, PromptLanguage: req.PromptLanguage, DialogueLanguage: req.DialogueLanguage,
		HasDialogue: req.HasDialogue, AudioStrategy: req.AudioStrategy, AudioRequirement: req.AudioRequirement,
	}
	matches := make([]matchedVideoPlanCandidate, 0)
	var storyboardReplanErr error
	for _, candidate := range candidates {
		variants, err := videoGenerationVariants(candidate.Model.Capabilities, candidate.Model)
		if err != nil {
			return GatewayVideoPlanResponse{}, err
		}
		for _, variant := range variants {
			matched, audioScore, audioStatus, readiness := matchVideoGenerationVariant(variant, matchReq)
			if !matched {
				continue
			}
			segments, err := planVideoSegmentsWithDialogue(
				req.TargetDurationTicks,
				req.TimelineTimebase,
				req.TimelineTimebase*req.FPSDenominator/req.FPSNumerator,
				variant,
				req.ReferenceMode,
				req.DialogueSpans,
			)
			if err != nil {
				var standard *StandardErrorError
				if errors.As(err, &standard) && standard.Standard.Code == CodeStoryboardReplanRequired {
					storyboardReplanErr = err
					continue
				}
				return GatewayVideoPlanResponse{}, err
			}
			variant.NativeAudio.Support = normalizeVideoSupport(variant.NativeAudio.Support)
			hash, err := capabilitySnapshotHash(variant)
			if err != nil {
				return GatewayVideoPlanResponse{}, err
			}
			matches = append(matches, matchedVideoPlanCandidate{
				Candidate: candidate, Variant: variant, SnapshotHash: hash, AudioScore: audioScore,
				NativeAudioStatus: audioStatus, ProductionReadiness: readiness, Segments: segments,
			})
		}
	}
	if len(matches) == 0 {
		if storyboardReplanErr != nil {
			return GatewayVideoPlanResponse{}, storyboardReplanErr
		}
		return GatewayVideoPlanResponse{}, &StandardErrorError{Standard: StandardError{
			Code: CodeModelCapabilityUnavailable, Message: "no video model variant satisfies the requested duration, references, resolution, language, and audio requirements", Retryable: false,
		}}
	}
	selected := matches[0]
	for _, candidate := range matches[1:] {
		if candidate.AudioScore > selected.AudioScore || (candidate.AudioScore == selected.AudioScore && candidate.Candidate.RoutingIndex < selected.Candidate.RoutingIndex) {
			selected = candidate
		}
	}
	fallbackCandidates := videoPlanFallbackCandidates(matches)
	planKey, err := videoRenderPlanKey(req, shotState.TimingRevision, selected)
	if err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	expiresIn := req.ExpiresInSeconds
	if expiresIn <= 0 {
		expiresIn = int(defaultVideoRenderPlanTTL / time.Second)
	}
	if expiresIn > int(maximumVideoRenderPlanTTL/time.Second) {
		expiresIn = int(maximumVideoRenderPlanTTL / time.Second)
	}
	expiresAt := time.Now().UTC().Add(time.Duration(expiresIn) * time.Second)
	return s.persistVideoRenderPlan(ctx, req, selected, fallbackCandidates, planKey, expiresAt)
}

func (s *Service) validateVideoPlanRequest(ctx context.Context, req *GatewayVideoPlanRequest) (videoPlanShotState, error) {
	req.OrganizationID = strings.TrimSpace(req.OrganizationID)
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.WorkflowRunID = strings.TrimSpace(req.WorkflowRunID)
	req.StoryboardShotID = strings.TrimSpace(req.StoryboardShotID)
	req.ModelProfileKey = strings.TrimSpace(req.ModelProfileKey)
	req.ProviderModelID = strings.TrimSpace(req.ProviderModelID)
	if req.OrganizationID == "" || req.ProjectID == "" || req.StoryboardShotID == "" {
		return videoPlanShotState{}, fmt.Errorf("%w: organizationId, projectId, and storyboardShotId are required", ErrValidation)
	}
	if req.Force && req.WorkflowRunID == "" {
		return videoPlanShotState{}, fmt.Errorf("%w: workflowRunId is required when force is true", ErrValidation)
	}
	if req.ModelProfileKey == "" && req.ProviderModelID == "" {
		return videoPlanShotState{}, fmt.Errorf("%w: modelProfileKey or providerModelId is required", ErrValidation)
	}
	if req.TimelineTimebase <= 0 || req.FPSNumerator <= 0 || req.FPSDenominator <= 0 || (req.TimelineTimebase*req.FPSDenominator)%req.FPSNumerator != 0 {
		return videoPlanShotState{}, fmt.Errorf("%w: timeline timebase and frame rate must form an exact positive frame duration", ErrValidation)
	}
	frameTick := req.TimelineTimebase * req.FPSDenominator / req.FPSNumerator
	if req.TargetDurationTicks <= 0 || req.TargetDurationTicks%frameTick != 0 {
		return videoPlanShotState{}, fmt.Errorf("%w: targetDurationTicks must be positive and frame-aligned", ErrValidation)
	}
	if req.HasDialogue && len(req.DialogueSpans) == 0 {
		return videoPlanShotState{}, &StandardErrorError{Standard: StandardError{Code: CodeStoryboardReplanRequired, Message: "storyboard shot dialogue requires exact frame-aligned timing spans before video planning", Retryable: false}}
	}
	if len(req.DialogueSpans) > 0 {
		normalizedDialogue, err := validateGatewayVideoDialogueSpans(req.DialogueSpans, req.TargetDurationTicks, frameTick)
		if err != nil {
			return videoPlanShotState{}, err
		}
		req.DialogueSpans = normalizedDialogue
		req.HasDialogue = true
	}
	req.ReferenceMode = normalizeReferenceMode(req.ReferenceMode)
	if req.TaskType == "" {
		if req.ReferenceMode == "none" {
			req.TaskType = "video.text_to_video"
		} else {
			req.TaskType = "video.image_to_video"
		}
	}
	req.AudioStrategy = strings.ToLower(strings.TrimSpace(req.AudioStrategy))
	if req.AudioStrategy == "" {
		req.AudioStrategy = "native_av"
	}
	if req.AudioStrategy != "native_av" && req.AudioStrategy != "hybrid" && req.AudioStrategy != "tts_postdub" {
		return videoPlanShotState{}, fmt.Errorf("%w: audioStrategy must be native_av, hybrid, or tts_postdub", ErrValidation)
	}
	req.AudioRequirement = strings.ToLower(strings.TrimSpace(req.AudioRequirement))
	if req.AudioRequirement == "" {
		req.AudioRequirement = "preferred"
	}
	if req.AudioRequirement != "preferred" && req.AudioRequirement != "required" && req.AudioRequirement != "disabled" {
		return videoPlanShotState{}, fmt.Errorf("%w: audioRequirement must be preferred, required, or disabled", ErrValidation)
	}
	if req.PromptLanguage == "" {
		req.PromptLanguage = "zh-CN"
	}
	var state videoPlanShotState
	var planID *string
	if err := s.db.QueryRow(ctx, `
		SELECT shot.storyboard_plan_id::text, shot.planned_duration_ticks, shot.timing_revision
		FROM storyboard_shots shot
		JOIN projects project ON project.id = shot.project_id
		WHERE shot.id = $1 AND shot.project_id = $2 AND shot.organization_id = $3
		  AND shot.deleted_at IS NULL
		  AND project.timeline_timebase = $4 AND project.fps_numerator = $5 AND project.fps_denominator = $6
	`, req.StoryboardShotID, req.ProjectID, req.OrganizationID, req.TimelineTimebase, req.FPSNumerator, req.FPSDenominator).Scan(&planID, &state.DurationTicks, &state.TimingRevision); err != nil {
		return videoPlanShotState{}, err
	}
	if planID != nil {
		state.StoryboardPlanID = *planID
	}
	if strings.TrimSpace(req.StoryboardPlanID) != "" && req.StoryboardPlanID != state.StoryboardPlanID {
		return videoPlanShotState{}, &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "storyboard plan changed before video planning", Retryable: false}}
	}
	if req.TargetDurationTicks != state.DurationTicks {
		return videoPlanShotState{}, &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "storyboard shot timing changed before video planning", Retryable: false}}
	}
	return state, nil
}

func (s *Service) resolveVideoPlanCandidates(ctx context.Context, req GatewayVideoPlanRequest) ([]resolvedVideoPlanCandidate, error) {
	excluded := make(map[string]bool, len(req.ExcludeProviderModelIDs))
	for _, modelID := range req.ExcludeProviderModelIDs {
		if modelID = strings.TrimSpace(modelID); modelID != "" {
			excluded[modelID] = true
		}
	}
	if req.ProviderModelID != "" {
		if excluded[req.ProviderModelID] {
			return nil, &StandardErrorError{Standard: StandardError{Code: CodeModelCapabilityUnavailable, Message: "selected provider model is excluded from this render plan revision", Retryable: false}}
		}
		model, err := s.GetModel(ctx, req.OrganizationID, req.ProviderModelID)
		if err != nil {
			return nil, err
		}
		if model.Status != "active" || (model.Modality != "video" && model.Modality != "multimodal") || !modelSupportsTaskType(model, TaskTypeVideoCreateTask) {
			return nil, &StandardErrorError{Standard: StandardError{Code: CodeModelCapabilityUnavailable, Message: "selected provider model is not active or does not support video generation", Retryable: false}}
		}
		account, err := s.GetAccount(ctx, req.OrganizationID, model.ProviderAccountID)
		if err != nil {
			return nil, err
		}
		if account.Status != "active" {
			return nil, &StandardErrorError{Standard: StandardError{Code: CodeModelCapabilityUnavailable, Message: "selected provider account is not active", Retryable: false}}
		}
		return []resolvedVideoPlanCandidate{{ProviderAccountID: account.ID, Model: model}}, nil
	}
	candidates, err := s.ResolveRoutingCandidates(ctx, RoutingRequest{
		OrganizationID: req.OrganizationID, ModelProfileKey: req.ModelProfileKey,
		TaskType: TaskTypeVideoCreateTask, Modality: "video",
		VideoDurationSeconds: float64(req.TargetDurationTicks) / float64(req.TimelineTimebase), VideoResolution: req.Resolution,
	})
	if err != nil {
		return nil, err
	}
	result := make([]resolvedVideoPlanCandidate, 0, len(candidates))
	for index, candidate := range candidates {
		if excluded[candidate.ProviderModelID] {
			continue
		}
		result = append(result, resolvedVideoPlanCandidate{
			ModelProfileID: candidate.ModelProfileID, ModelProfileBindingID: candidate.ModelProfileBindingID,
			ModelProfileKey: candidate.ModelProfileKey, ProviderAccountID: candidate.ProviderAccountID,
			Model:        Model{ID: candidate.ProviderModelID, ProviderAccountID: candidate.ProviderAccountID, ModelKey: candidate.ModelKey, Modality: candidate.Modality, Status: "active", Capabilities: candidate.Capabilities},
			RoutingIndex: index, FallbackStrategy: candidate.FallbackStrategy,
		})
	}
	if len(result) == 0 {
		return nil, &StandardErrorError{Standard: StandardError{Code: CodeModelCapabilityUnavailable, Message: "no remaining video model candidates are available after previous render failures", Retryable: false}}
	}
	return result, nil
}

func (s *Service) persistVideoRenderPlan(ctx context.Context, req GatewayVideoPlanRequest, selected matchedVideoPlanCandidate, fallbackCandidates json.RawMessage, planKey string, expiresAt time.Time) (GatewayVideoPlanResponse, error) {
	var existingID string
	err := s.db.QueryRow(ctx, `
		SELECT id::text FROM video_render_plans
		WHERE project_id = $1 AND plan_key = $2 AND active = true
		  AND expires_at > now() + ($3 * interval '1 second')
	`, req.ProjectID, planKey, int(minimumReusableVideoPlanTTL/time.Second)).Scan(&existingID)
	if err == nil {
		return s.loadVideoRenderPlan(ctx, req.OrganizationID, existingID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return GatewayVideoPlanResponse{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	defer tx.Rollback(ctx)
	if err := lockGatewayVideoNodeExecutionTx(ctx, tx, req.NodeRunID, req.NodeExecutionToken, req.NodeAttemptGeneration); err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE video_render_plans
		SET active = false,
		    status = CASE WHEN status IN ('planned', 'running') THEN 'stale' ELSE status END,
		    updated_at = now(), metadata = metadata || jsonb_build_object('supersededAt', now())
		WHERE storyboard_shot_id = $1 AND active = true
	`, req.StoryboardShotID); err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	snapshot, err := json.Marshal(selected.Variant)
	if err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	var planID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO video_render_plans(
			organization_id, project_id, storyboard_plan_id, storyboard_shot_id, workflow_run_id, node_run_id,
			model_profile_id, model_profile_binding_id, model_profile_key,
			provider_account_id, provider_model_id, model_family, variant_key,
			capability_snapshot, capability_snapshot_hash, fallback_candidates, plan_key,
			target_duration_ticks, timeline_timebase, fps_numerator, fps_denominator,
			task_type, reference_mode, aspect_ratio, resolution,
			audio_strategy, audio_requirement, native_audio_status, production_readiness, expires_at, metadata
		)
		VALUES (
			$1, $2, NULLIF($3, '')::uuid, $4, NULLIF($5, '')::uuid, NULLIF($6, '')::uuid,
			NULLIF($7, '')::uuid, NULLIF($8, '')::uuid, NULLIF($9, ''),
			$10, $11, $12, $13, $14::jsonb, $15, $16::jsonb, $17,
			$18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30,
			jsonb_build_object('hasDialogue', $31::boolean, 'dialogueLanguage', $32::text, 'promptLanguage', $33::text,
			                   'previousExecutionPlanId', NULLIF($34, ''), 'excludedProviderModelIds', $35::jsonb)
		)
		RETURNING id::text
	`, req.OrganizationID, req.ProjectID, req.StoryboardPlanID, req.StoryboardShotID, req.WorkflowRunID, req.NodeRunID,
		selected.Candidate.ModelProfileID, selected.Candidate.ModelProfileBindingID, selected.Candidate.ModelProfileKey,
		selected.Candidate.ProviderAccountID, selected.Candidate.Model.ID, selected.Variant.ModelFamily, selected.Variant.VariantKey,
		snapshot, selected.SnapshotHash, fallbackCandidates, planKey,
		req.TargetDurationTicks, req.TimelineTimebase, req.FPSNumerator, req.FPSDenominator,
		req.TaskType, req.ReferenceMode, req.AspectRatio, req.Resolution,
		req.AudioStrategy, req.AudioRequirement, selected.NativeAudioStatus, selected.ProductionReadiness, expiresAt,
		req.HasDialogue, req.DialogueLanguage, req.PromptLanguage, req.PreviousExecutionPlanID, mustJSON(req.ExcludeProviderModelIDs)).Scan(&planID); err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	nativeAudioRequested := req.AudioStrategy == "native_av" && req.AudioRequirement != "disabled"
	for index := range selected.Segments {
		segment := &selected.Segments[index]
		if err := tx.QueryRow(ctx, `
			INSERT INTO video_render_segments(
				organization_id, project_id, video_render_plan_id, storyboard_shot_id, segment_index,
				planned_start_tick, planned_end_tick, requested_duration_seconds, trim_end_tick, continuity_mode,
				native_audio_requested, audio_verification_status, production_readiness, provider_model_id, dialogue, metadata
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, 0), $10, $11, $12, 'blocked', $13, $14::jsonb,
			        jsonb_build_object('attemptedProviderModelIds', jsonb_build_array($13::uuid::text)))
			RETURNING id::text
		`, req.OrganizationID, req.ProjectID, planID, req.StoryboardShotID, segment.SegmentIndex,
			segment.PlannedStartTick, segment.PlannedEndTick, segment.RequestedDurationSeconds, segment.TrimEndTick,
			segment.ContinuityMode, nativeAudioRequested, selected.NativeAudioStatus, selected.Candidate.Model.ID, mustJSON(segment.DialogueSpans)).Scan(&segment.SegmentID); err != nil {
			return GatewayVideoPlanResponse{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET active_video_render_plan_id = $2,
		    native_audio_status = $3,
		    production_readiness = 'blocked',
		    video_status = CASE WHEN video_status IN ('running', 'queued') THEN video_status ELSE 'not_started' END,
		    stale_state = 'needs_regeneration', updated_at = now()
		WHERE id = $1 AND project_id = $4
	`, req.StoryboardShotID, planID, selected.NativeAudioStatus, req.ProjectID); err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	if err := events.AppendTx(ctx, tx, req.OrganizationID, req.ProjectID, "storyboard.shot.render_plan.created", "storyboard_shot", req.StoryboardShotID, mustJSON(map[string]any{
		"shotId":                 req.StoryboardShotID,
		"executionPlanId":        planID,
		"variantKey":             selected.Variant.VariantKey,
		"providerModelId":        selected.Candidate.Model.ID,
		"segmentCount":           len(selected.Segments),
		"capabilitySnapshotHash": selected.SnapshotHash,
		"workflowRunId":          req.WorkflowRunID,
	})); err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	return GatewayVideoPlanResponse{
		ExecutionPlanID: planID, ProviderModelID: selected.Candidate.Model.ID, ProviderAccountID: selected.Candidate.ProviderAccountID,
		ModelFamily: selected.Variant.ModelFamily, VariantKey: selected.Variant.VariantKey, CapabilitySnapshot: selected.Variant,
		CapabilitySnapshotHash: selected.SnapshotHash, ExpiresAt: expiresAt.Format(time.RFC3339Nano),
		TimelineTimebase: req.TimelineTimebase, FPSNumerator: req.FPSNumerator, FPSDenominator: req.FPSDenominator,
		AudioStrategy: req.AudioStrategy, AudioRequirement: req.AudioRequirement, NativeAudioStatus: selected.NativeAudioStatus,
		ProductionReadiness: selected.ProductionReadiness, Segments: selected.Segments,
	}, nil
}

func (s *Service) loadVideoRenderPlan(ctx context.Context, organizationID, planID string) (GatewayVideoPlanResponse, error) {
	var response GatewayVideoPlanResponse
	var snapshot []byte
	var expiresAt time.Time
	if err := s.db.QueryRow(ctx, `
		SELECT id::text, provider_model_id::text, provider_account_id::text, model_family, variant_key,
		       capability_snapshot, capability_snapshot_hash, timeline_timebase, fps_numerator, fps_denominator, expires_at,
		       audio_strategy, audio_requirement, native_audio_status, production_readiness
		FROM video_render_plans
		WHERE id = $1 AND organization_id = $2
	`, planID, organizationID).Scan(
		&response.ExecutionPlanID, &response.ProviderModelID, &response.ProviderAccountID, &response.ModelFamily, &response.VariantKey,
		&snapshot, &response.CapabilitySnapshotHash, &response.TimelineTimebase, &response.FPSNumerator, &response.FPSDenominator, &expiresAt,
		&response.AudioStrategy, &response.AudioRequirement, &response.NativeAudioStatus, &response.ProductionReadiness,
	); err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	if err := json.Unmarshal(snapshot, &response.CapabilitySnapshot); err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	response.ExpiresAt = expiresAt.Format(time.RFC3339Nano)
	rows, err := s.db.Query(ctx, `
		SELECT id::text, segment_index, planned_start_tick, planned_end_tick, planned_duration_ticks,
		       requested_duration_seconds::float8, continuity_mode, COALESCE(trim_end_tick, 0), dialogue
		FROM video_render_segments
		WHERE video_render_plan_id = $1
		ORDER BY segment_index
	`, planID)
	if err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	defer rows.Close()
	response.Segments = make([]GatewayVideoPlanSegment, 0)
	for rows.Next() {
		var segment GatewayVideoPlanSegment
		var dialogue []byte
		if err := rows.Scan(&segment.SegmentID, &segment.SegmentIndex, &segment.PlannedStartTick, &segment.PlannedEndTick,
			&segment.PlannedDurationTicks, &segment.RequestedDurationSeconds, &segment.ContinuityMode, &segment.TrimEndTick, &dialogue); err != nil {
			return GatewayVideoPlanResponse{}, err
		}
		if err := json.Unmarshal(dialogue, &segment.DialogueSpans); err != nil {
			return GatewayVideoPlanResponse{}, err
		}
		segment.PlannedDurationSeconds = float64(segment.PlannedDurationTicks) / float64(response.TimelineTimebase)
		response.Segments = append(response.Segments, segment)
	}
	return response, rows.Err()
}

func videoPlanFallbackCandidates(matches []matchedVideoPlanCandidate) json.RawMessage {
	items := make([]map[string]any, 0, len(matches))
	seen := map[string]bool{}
	for _, match := range matches {
		key := match.Candidate.Model.ID + ":" + match.Variant.VariantKey
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, map[string]any{
			"providerModelId": match.Candidate.Model.ID, "providerAccountId": match.Candidate.ProviderAccountID,
			"modelFamily": match.Variant.ModelFamily, "variantKey": match.Variant.VariantKey,
			"capabilitySnapshotHash": match.SnapshotHash, "audioScore": match.AudioScore,
		})
	}
	raw, _ := json.Marshal(items)
	return raw
}

func videoRenderPlanKey(req GatewayVideoPlanRequest, timingRevision int, selected matchedVideoPlanCandidate) (string, error) {
	keyFields := map[string]any{
		"shotId": req.StoryboardShotID, "timingRevision": timingRevision,
		"providerModelId": selected.Candidate.Model.ID, "variantKey": selected.Variant.VariantKey,
		"capabilitySnapshotHash": selected.SnapshotHash, "targetDurationTicks": req.TargetDurationTicks,
		"taskType": req.TaskType, "referenceMode": req.ReferenceMode, "aspectRatio": req.AspectRatio, "resolution": req.Resolution,
		"audioStrategy": req.AudioStrategy, "audioRequirement": req.AudioRequirement,
		"dialogueLanguage": req.DialogueLanguage, "hasDialogue": req.HasDialogue,
	}
	if req.Force {
		keyFields["forceWorkflowRunId"] = strings.TrimSpace(req.WorkflowRunID)
	}
	raw, err := json.Marshal(keyFields)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
