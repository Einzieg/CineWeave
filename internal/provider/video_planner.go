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
	Account               Account
	Model                 Model
	RoutingIndex          int
	FallbackStrategy      FallbackStrategy
}

type matchedVideoPlanCandidate struct {
	Candidate                 resolvedVideoPlanCandidate
	Variant                   VideoGenerationVariant
	ContinuationInputContract *VideoInputContract
	CapabilityAttestationID   string
	RecoverySourcePlanID      string
	SnapshotHash              string
	SelectionScore            int
	NativeAudioStatus         string
	ProductionReadiness       string
	Segments                  []GatewayVideoPlanSegment
}

type videoPlanShotState struct {
	StoryboardPlanID string
	DurationTicks    int64
	TimingRevision   int
}

func (s *Service) PlanVideo(ctx context.Context, req GatewayVideoPlanRequest) (GatewayVideoPlanResponse, error) {
	if err := s.validateGatewayVideoProductionIdentity(
		ctx, req.OrganizationID, req.ProjectID, req.ProductionGenerationID, req.VideoProductionBindingID,
		req.VideoProductionBindingRevision, req.WorkflowRunID, req.NodeRunID,
	); err != nil {
		return GatewayVideoPlanResponse{}, err
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
		return GatewayVideoPlanResponse{}, err
	}
	shotState, err := s.validateVideoPlanRequest(ctx, &req)
	if err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	if req.StoryboardPlanID == "" {
		req.StoryboardPlanID = shotState.StoryboardPlanID
	}
	if recovered, found, err := s.tryPlanGatewayVideoMediaRecovery(ctx, req, shotState); err != nil {
		return GatewayVideoPlanResponse{}, err
	} else if found {
		return recovered, nil
	}
	candidates, err := s.resolveVideoPlanCandidates(ctx, req)
	if err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	matchReq := videoVariantMatchRequest{
		TaskType: req.TaskType, ReferenceMode: req.ReferenceMode, AspectRatio: req.AspectRatio,
		Resolution: req.Resolution, PromptLanguage: req.PromptLanguage, DialogueLanguage: req.DialogueLanguage,
		HasDialogue: req.HasDialogue, AudioStrategy: req.AudioStrategy, AudioRequirement: req.AudioRequirement,
		RequiredInitialInputContract:      req.RequiredInitialInputContract,
		AllowedContinuationInputContracts: req.AllowedContinuationInputContracts,
		CompatibilityPolicy:               req.CompatibilityPolicy,
	}
	matches := make([]matchedVideoPlanCandidate, 0)
	var storyboardReplanErr error
	var adapterMappingErr error
	for _, candidate := range candidates {
		variants, err := videoGenerationVariants(candidate.Model.Capabilities, candidate.Model)
		if err != nil {
			return GatewayVideoPlanResponse{}, err
		}
		for _, variant := range variants {
			matched, selectionScore, audioStatus, readiness := matchVideoGenerationVariant(variant, matchReq)
			if !matched {
				continue
			}
			continuationContract, err := selectVideoContinuationInputContract(variant, req.AllowedContinuationInputContracts)
			if err != nil {
				return GatewayVideoPlanResponse{}, err
			}
			contracts := []VideoInputContract{variant.InputContract}
			if continuationContract != nil {
				contracts = append(contracts, *continuationContract)
			}
			if _, err := s.validateVideoInputContractsAdapterFixture(ctx, candidate.Account, candidate.Model, contracts); err != nil {
				adapterMappingErr = err
				continue
			}
			segments, err := planVideoSegmentsWithDialogue(
				req.TargetDurationTicks,
				req.TimelineTimebase,
				req.TimelineTimebase*req.FPSDenominator/req.FPSNumerator,
				variant,
				req.ReferenceMode,
				req.DialogueSpans,
				continuationContract,
			)
			if err != nil {
				var standard *StandardErrorError
				if errors.As(err, &standard) && standard.Standard.Code == CodeStoryboardReplanRequired {
					storyboardReplanErr = err
					continue
				}
				return GatewayVideoPlanResponse{}, err
			}
			if err := validateProfileVideoSegments(req.RequiredInitialInputContract, segments); err != nil {
				storyboardReplanErr = err
				continue
			}
			variant.NativeAudio.Support = normalizeVideoSupport(variant.NativeAudio.Support)
			hash, err := capabilitySnapshotHash(variant)
			if err != nil {
				return GatewayVideoPlanResponse{}, err
			}
			initialContractHash, err := videoInputContractHash(variant.InputContract)
			if err != nil {
				return GatewayVideoPlanResponse{}, err
			}
			continuationContractHash := ""
			if continuationContract != nil {
				continuationContractHash, err = videoInputContractHash(*continuationContract)
				if err != nil {
					return GatewayVideoPlanResponse{}, err
				}
			}
			for index := range segments {
				segments[index].InputContractKey = variant.InputContract.ContractKey
				segments[index].InputContractHash = initialContractHash
				if index > 0 && continuationContract != nil {
					segments[index].InputContractKey = continuationContract.ContractKey
					segments[index].InputContractHash = continuationContractHash
				}
			}
			matches = append(matches, matchedVideoPlanCandidate{
				Candidate: candidate, Variant: variant, SnapshotHash: hash, SelectionScore: selectionScore,
				ContinuationInputContract: continuationContract,
				NativeAudioStatus:         audioStatus, ProductionReadiness: readiness, Segments: segments,
			})
		}
	}
	if len(matches) == 0 {
		if storyboardReplanErr != nil {
			return GatewayVideoPlanResponse{}, storyboardReplanErr
		}
		if adapterMappingErr != nil {
			return GatewayVideoPlanResponse{}, adapterMappingErr
		}
		return GatewayVideoPlanResponse{}, &StandardErrorError{Standard: StandardError{
			Code: CodeModelCapabilityUnavailable, Message: "没有视频模型能够覆盖目标时长和分辨率", Retryable: false,
		}}
	}
	selected := matches[0]
	for _, candidate := range matches[1:] {
		if candidate.SelectionScore > selected.SelectionScore || (candidate.SelectionScore == selected.SelectionScore && candidate.Candidate.RoutingIndex < selected.Candidate.RoutingIndex) {
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

func validateProfileVideoSegments(requiredInitialInputContract string, segments []GatewayVideoPlanSegment) error {
	if strings.TrimSpace(requiredInitialInputContract) != VideoInputContractFirstLastFrames || len(segments) == 1 {
		return nil
	}
	return &StandardErrorError{Standard: StandardError{
		Code:      CodeStoryboardReplanRequired,
		Message:   "首尾帧衔接模式的一个镜头必须在视频模型单次时长内完成；当前镜头需要多个请求，请先在分镜层拆成多个镜头并为每个镜头重新生成首尾帧",
		Retryable: false,
	}}
}

func (s *Service) validateVideoPlanRequest(ctx context.Context, req *GatewayVideoPlanRequest) (videoPlanShotState, error) {
	req.OrganizationID = strings.TrimSpace(req.OrganizationID)
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.OperationID = strings.TrimSpace(req.OperationID)
	req.OperationItemID = strings.TrimSpace(req.OperationItemID)
	req.WorkflowRunID = strings.TrimSpace(req.WorkflowRunID)
	req.StoryboardShotID = strings.TrimSpace(req.StoryboardShotID)
	req.ModelProfileKey = strings.TrimSpace(req.ModelProfileKey)
	req.ProviderModelID = strings.TrimSpace(req.ProviderModelID)
	if req.OrganizationID == "" || req.ProjectID == "" || req.StoryboardShotID == "" {
		return videoPlanShotState{}, fmt.Errorf("%w: organizationId, projectId, and storyboardShotId are required", ErrValidation)
	}
	operationIdentityCount := 0
	if req.OperationID != "" {
		operationIdentityCount++
	}
	if req.OperationItemID != "" {
		operationIdentityCount++
	}
	if req.OperationItemAttempt > 0 {
		operationIdentityCount++
	}
	if operationIdentityCount != 0 && operationIdentityCount != 3 {
		return videoPlanShotState{}, fmt.Errorf("%w: operationId, operationItemId, and operationItemAttempt must be provided together", ErrValidation)
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
	contract, err := s.loadVideoPlanProductionContract(ctx, s.db, *req)
	if err != nil {
		return videoPlanShotState{}, err
	}
	if err := validateVideoPlanContractRequest(req, contract); err != nil {
		return videoPlanShotState{}, err
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
		return []resolvedVideoPlanCandidate{{ProviderAccountID: account.ID, Account: account, Model: model}}, nil
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
		account, err := s.GetAccount(ctx, req.OrganizationID, candidate.ProviderAccountID)
		if err != nil {
			return nil, err
		}
		result = append(result, resolvedVideoPlanCandidate{
			ModelProfileID: candidate.ModelProfileID, ModelProfileBindingID: candidate.ModelProfileBindingID,
			ModelProfileKey: candidate.ModelProfileKey, ProviderAccountID: candidate.ProviderAccountID,
			Account:      account,
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
	if req.validatedContract == nil {
		return GatewayVideoPlanResponse{}, &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "视频生产契约尚未完成校验", Retryable: false}}
	}
	contract := *req.validatedContract
	if req.OperationItemID == "" {
		var existingID string
		err := s.db.QueryRow(ctx, `
			SELECT id::text FROM video_render_plans
			WHERE project_id = $1 AND plan_key = $2 AND active = true
			  AND production_generation_id = $4
			  AND expires_at > now() + ($3 * interval '1 second')
		`, req.ProjectID, planKey, int(minimumReusableVideoPlanTTL/time.Second), req.ProductionGenerationID).Scan(&existingID)
		if err == nil {
			return s.loadVideoRenderPlan(ctx, req.OrganizationID, existingID)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return GatewayVideoPlanResponse{}, err
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	defer tx.Rollback(ctx)
	if err := assertGatewayVideoProductionIdentityTx(ctx, tx, req.OrganizationID, req.ProjectID, videoProductionIdentity(
		req.ProductionGenerationID, req.VideoProductionBindingID, req.VideoProductionBindingRevision,
	)); err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	if err := lockGatewayVideoNodeExecutionTx(ctx, tx, req.NodeRunID, req.NodeExecutionToken, req.NodeAttemptGeneration); err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	existingOperationPlanID, err := lockVideoOperationItemForPlanTx(ctx, tx, req)
	if err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	if existingOperationPlanID != "" {
		if err := tx.Commit(ctx); err != nil {
			return GatewayVideoPlanResponse{}, err
		}
		return s.loadVideoRenderPlan(ctx, req.OrganizationID, existingOperationPlanID)
	}
	currentContract, err := s.loadVideoPlanProductionContract(ctx, tx, req)
	if err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	if err := validateVideoPlanContractRequest(&req, currentContract); err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	contract = currentContract
	if _, err := tx.Exec(ctx, `
		UPDATE video_render_plans
		SET active = false,
		    status = CASE WHEN status IN ('planned', 'running') THEN 'stale' ELSE status END,
		    updated_at = now(), metadata = metadata || jsonb_build_object('supersededAt', now())
		WHERE storyboard_shot_id = $1 AND production_generation_id = $2 AND active = true
	`, req.StoryboardShotID, req.ProductionGenerationID); err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	snapshot, err := json.Marshal(selected.Variant)
	if err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	initialInputContractSnapshot, err := json.Marshal(selected.Variant.InputContract)
	if err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	initialInputContractHash, err := videoInputContractHash(selected.Variant.InputContract)
	if err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	var continuationInputContractSnapshot any
	continuationInputContractHash := ""
	if selected.ContinuationInputContract != nil {
		continuationInputContractSnapshot, err = json.Marshal(*selected.ContinuationInputContract)
		if err != nil {
			return GatewayVideoPlanResponse{}, err
		}
		continuationInputContractHash, err = videoInputContractHash(*selected.ContinuationInputContract)
		if err != nil {
			return GatewayVideoPlanResponse{}, err
		}
	}
	planMetadata := map[string]any{
		"hasDialogue": req.HasDialogue, "dialogueLanguage": req.DialogueLanguage,
		"promptLanguage": req.PromptLanguage, "previousExecutionPlanId": req.PreviousExecutionPlanID,
		"excludedProviderModelIds": req.ExcludeProviderModelIDs, "inputContractVersion": contract.InputContractVersion,
		"compatibilityPolicy": contract.CompatibilityPolicy, "videoPromptHash": contract.VideoPromptHash,
	}
	if selected.RecoverySourcePlanID != "" {
		planMetadata["mediaRecoverySourceExecutionPlanId"] = selected.RecoverySourcePlanID
		planMetadata["executionMode"] = "media_recovery"
	}
	var planID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO video_render_plans(
			organization_id, project_id, storyboard_plan_id, storyboard_shot_id, workflow_run_id, node_run_id,
			operation_item_id, operation_item_attempt,
			production_generation_id, video_production_binding_id, video_production_binding_revision,
			profile_version_id, production_profile_snapshot, production_profile_snapshot_hash,
			model_profile_id, model_profile_binding_id, model_profile_key,
			provider_account_id, provider_model_id, model_family, variant_key,
			capability_snapshot, capability_snapshot_hash,
			shot_state_revision, shot_state_hash, transition_snapshot, transition_hash,
			reference_pack_id, reference_pack_hash,
			initial_input_contract_snapshot, initial_input_contract_hash,
			continuation_input_contract_snapshot, continuation_input_contract_hash, capability_attestation_id,
			prompt_context_plan_id, prompt_context_plan_hash, video_prompt_plan_id,
			dialogue_cues, native_audio_required, fallback_candidates, plan_key,
			target_duration_ticks, timeline_timebase, fps_numerator, fps_denominator,
			task_type, reference_mode, aspect_ratio, resolution,
			audio_strategy, audio_requirement, native_audio_status, production_readiness,
			commerce_script_unit_id, commerce_script_unit_generation_id,
			commerce_product_id, product_version_id, localization_id,
			product_reference_pack_id, commerce_workflow_binding_id,
			localized_contract_hash, target_language, verbatim_voiceover_hash,
			timing_policy_version, language_capability_snapshot_hash,
			expires_at, metadata
		)
		VALUES (
			@organization_id, @project_id, NULLIF(@storyboard_plan_id, '')::uuid, @storyboard_shot_id,
			NULLIF(@workflow_run_id, '')::uuid, NULLIF(@node_run_id, '')::uuid,
			NULLIF(@operation_item_id, '')::uuid, NULLIF(@operation_item_attempt, 0),
			@production_generation_id::uuid, @binding_id::uuid, @binding_revision,
			@profile_version_id::uuid, @profile_snapshot::jsonb, @profile_snapshot_hash,
			NULLIF(@model_profile_id, '')::uuid, NULLIF(@model_profile_binding_id, '')::uuid, NULLIF(@model_profile_key, ''),
			@provider_account_id::uuid, @provider_model_id::uuid, @model_family, @variant_key,
			@capability_snapshot::jsonb, @capability_snapshot_hash,
			@shot_state_revision, @shot_state_hash, @transition_snapshot::jsonb, NULLIF(@transition_hash, ''),
			@reference_pack_id::uuid, @reference_pack_hash,
			@initial_input_contract_snapshot::jsonb, @initial_input_contract_hash,
			@continuation_input_contract_snapshot::jsonb, NULLIF(@continuation_input_contract_hash, ''), NULLIF(@capability_attestation_id, '')::uuid,
			@prompt_context_plan_id::uuid, @prompt_context_plan_hash, @video_prompt_plan_id::uuid,
			@dialogue_cues::jsonb, @native_audio_required, @fallback_candidates::jsonb, @plan_key,
			@target_duration_ticks, @timeline_timebase, @fps_numerator, @fps_denominator,
			@task_type, @reference_mode, @aspect_ratio, @resolution,
			@audio_strategy, @audio_requirement, @native_audio_status, @production_readiness,
			NULLIF(@commerce_script_unit_id, '')::uuid,
			NULLIF(@commerce_script_unit_generation_id, '')::uuid,
			NULLIF(@commerce_product_id, '')::uuid,
			NULLIF(@product_version_id, '')::uuid,
			NULLIF(@localization_id, '')::uuid,
			NULLIF(@product_reference_pack_id, '')::uuid,
			NULLIF(@commerce_workflow_binding_id, '')::uuid,
			NULLIF(@localized_contract_hash, ''),
			NULLIF(@target_language, ''),
			NULLIF(@verbatim_voiceover_hash, ''),
			NULLIF(@timing_policy_version, ''),
			NULLIF(@language_capability_snapshot_hash, ''),
			@expires_at,
			@metadata::jsonb
		)
		RETURNING id::text
	`, pgx.NamedArgs{
		"organization_id": req.OrganizationID, "project_id": req.ProjectID,
		"storyboard_plan_id": req.StoryboardPlanID, "storyboard_shot_id": req.StoryboardShotID,
		"workflow_run_id": req.WorkflowRunID, "node_run_id": req.NodeRunID,
		"operation_item_id": req.OperationItemID, "operation_item_attempt": req.OperationItemAttempt,
		"production_generation_id": req.ProductionGenerationID, "binding_id": req.VideoProductionBindingID,
		"binding_revision": req.VideoProductionBindingRevision, "profile_version_id": contract.ProfileVersionID,
		"profile_snapshot": contract.ProfileSnapshot, "profile_snapshot_hash": cleanVideoContractHash(contract.ProfileSnapshotHash),
		"model_profile_id": selected.Candidate.ModelProfileID, "model_profile_binding_id": selected.Candidate.ModelProfileBindingID,
		"model_profile_key": selected.Candidate.ModelProfileKey, "provider_account_id": selected.Candidate.ProviderAccountID,
		"provider_model_id": selected.Candidate.Model.ID, "model_family": selected.Variant.ModelFamily,
		"variant_key": selected.Variant.VariantKey, "capability_snapshot": snapshot,
		"capability_snapshot_hash": selected.SnapshotHash, "shot_state_revision": contract.ShotStateRevision,
		"shot_state_hash": cleanVideoContractHash(contract.ShotStateHash), "transition_snapshot": contract.TransitionSnapshot,
		"transition_hash": cleanVideoContractHash(contract.TransitionHash), "reference_pack_id": contract.ReferencePackID,
		"reference_pack_hash":                  cleanVideoContractHash(contract.ReferencePackHash),
		"initial_input_contract_snapshot":      initialInputContractSnapshot,
		"initial_input_contract_hash":          initialInputContractHash,
		"continuation_input_contract_snapshot": continuationInputContractSnapshot,
		"continuation_input_contract_hash":     continuationInputContractHash,
		"capability_attestation_id":            selected.CapabilityAttestationID,
		"prompt_context_plan_id":               contract.PromptContextPlanID,
		"prompt_context_plan_hash":             cleanVideoContractHash(contract.PromptContextPlanHash), "video_prompt_plan_id": contract.VideoPromptPlanID,
		"dialogue_cues": mustJSON(contract.DialogueCues), "native_audio_required": contract.NativeAudioRequired,
		"fallback_candidates": fallbackCandidates, "plan_key": planKey,
		"target_duration_ticks": req.TargetDurationTicks, "timeline_timebase": req.TimelineTimebase,
		"fps_numerator": req.FPSNumerator, "fps_denominator": req.FPSDenominator,
		"task_type": req.TaskType, "reference_mode": req.ReferenceMode, "aspect_ratio": req.AspectRatio,
		"resolution": req.Resolution, "audio_strategy": req.AudioStrategy, "audio_requirement": req.AudioRequirement,
		"native_audio_status": selected.NativeAudioStatus, "production_readiness": selected.ProductionReadiness,
		"commerce_script_unit_id":            contract.CommerceScriptUnitID,
		"commerce_script_unit_generation_id": contract.CommerceScriptUnitGenerationID,
		"commerce_product_id":                contract.CommerceProductID,
		"product_version_id":                 contract.ProductVersionID,
		"localization_id":                    contract.LocalizationID,
		"product_reference_pack_id":          contract.ProductReferencePackID,
		"commerce_workflow_binding_id":       contract.CommerceWorkflowBindingID,
		"localized_contract_hash":            cleanVideoContractHash(contract.LocalizedContractHash),
		"target_language":                    contract.TargetLanguage,
		"verbatim_voiceover_hash":            cleanVideoContractHash(contract.VerbatimVoiceoverHash),
		"timing_policy_version":              contract.TimingPolicyVersion,
		"language_capability_snapshot_hash":  cleanVideoContractHash(contract.LanguageCapabilitySnapshotHash),
		"expires_at":                         expiresAt, "metadata": mustJSON(planMetadata),
	}).Scan(&planID); err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	nativeAudioRequested := req.AudioStrategy == "native_av" && req.AudioRequirement != "disabled"
	for index := range selected.Segments {
		segment := &selected.Segments[index]
		if err := tx.QueryRow(ctx, `
			INSERT INTO video_render_segments(
				organization_id, project_id, video_render_plan_id, storyboard_shot_id, segment_index,
				production_generation_id,
				planned_start_tick, planned_end_tick, requested_duration_seconds, trim_end_tick, continuity_mode,
				native_audio_requested, audio_verification_status, production_readiness, provider_model_id, dialogue,
				input_contract_key, input_contract_hash, source_video_prompt_plan_id, source_prompt_hash, metadata
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, 0), $11, $12, $13, 'blocked', $14, $15::jsonb,
			        $16, $17, $18::uuid, $19,
			        jsonb_build_object('attemptedProviderModelIds', jsonb_build_array($14::uuid::text)))
			RETURNING id::text
		`, req.OrganizationID, req.ProjectID, planID, req.StoryboardShotID, segment.SegmentIndex, req.ProductionGenerationID,
			segment.PlannedStartTick, segment.PlannedEndTick, segment.RequestedDurationSeconds, segment.TrimEndTick,
			segment.ContinuityMode, nativeAudioRequested, selected.NativeAudioStatus, selected.Candidate.Model.ID, mustJSON(segment.DialogueSpans),
			segment.InputContractKey, segment.InputContractHash, contract.VideoPromptPlanID, cleanVideoContractHash(contract.VideoPromptHash)).Scan(&segment.SegmentID); err != nil {
			return GatewayVideoPlanResponse{}, err
		}
	}
	if req.OperationItemID != "" {
		command, err := tx.Exec(ctx, `
			UPDATE episode_video_production_items
			SET video_render_plan_id = $2,
			    execution_plan_bound_at = now(),
			    metadata = metadata || jsonb_build_object(
			      'executionPlanBoundAt', now(),
			      'executionPlanId', $2::uuid::text,
			      'executionIdentityVersion', 2
			    ),
			    updated_at = now(), revision = revision + 1
			WHERE id = $1 AND execution_identity_version = 2
			  AND attempt = $3 AND video_render_plan_id IS NULL
			  AND status IN ('queued', 'running')
		`, req.OperationItemID, planID, req.OperationItemAttempt)
		if err != nil {
			return GatewayVideoPlanResponse{}, err
		}
		if command.RowsAffected() != 1 {
			return GatewayVideoPlanResponse{}, &StandardErrorError{Standard: StandardError{
				Code: CodeRenderPlanReplanRequired, Message: "视频生产项已被其它执行绑定，请重新加载任务状态", Retryable: false,
			}}
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET active_video_render_plan_id = $2,
		    native_audio_status = $3,
		    production_readiness = 'blocked',
		    video_status = CASE WHEN video_status IN ('running', 'queued') THEN video_status ELSE 'not_started' END,
		    stale_state = 'needs_regeneration', updated_at = now()
		WHERE id = $1 AND project_id = $4 AND production_generation_id = $5
	`, req.StoryboardShotID, planID, selected.NativeAudioStatus, req.ProjectID, req.ProductionGenerationID); err != nil {
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
		"operationId":            req.OperationID,
		"operationItemId":        req.OperationItemID,
		"operationItemAttempt":   req.OperationItemAttempt,
	})); err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	if selected.RecoverySourcePlanID != "" {
		if err := events.AppendTx(ctx, tx, req.OrganizationID, req.ProjectID, "storyboard.shot.render_plan.execution_cloned", "storyboard_shot", req.StoryboardShotID, mustJSON(map[string]any{
			"shotId":                req.StoryboardShotID,
			"executionPlanId":       planID,
			"sourceExecutionPlanId": selected.RecoverySourcePlanID,
			"workflowRunId":         req.WorkflowRunID,
			"reason":                "provider_media_recovery",
		})); err != nil {
			return GatewayVideoPlanResponse{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	return GatewayVideoPlanResponse{
		ExecutionPlanID: planID, ProviderModelID: selected.Candidate.Model.ID, ProviderAccountID: selected.Candidate.ProviderAccountID,
		ModelFamily: selected.Variant.ModelFamily, VariantKey: selected.Variant.VariantKey, CapabilitySnapshot: selected.Variant,
		CapabilitySnapshotHash: selected.SnapshotHash, ExpiresAt: expiresAt.Format(time.RFC3339Nano),
		InitialInputContractSnapshot: selected.Variant.InputContract, InitialInputContractHash: initialInputContractHash,
		ContinuationInputContractSnapshot: selected.ContinuationInputContract, ContinuationInputContractHash: continuationInputContractHash,
		CapabilityAttestationID:    selected.CapabilityAttestationID,
		ProductionProfileVersionID: contract.ProfileVersionID, ProductionProfileSnapshotHash: cleanVideoContractHash(contract.ProfileSnapshotHash),
		CompatibilityPolicy: contract.CompatibilityPolicy, ShotStateRevision: contract.ShotStateRevision,
		ShotStateHash: cleanVideoContractHash(contract.ShotStateHash), TransitionHash: cleanVideoContractHash(contract.TransitionHash),
		ReferencePackID: contract.ReferencePackID, ReferencePackHash: cleanVideoContractHash(contract.ReferencePackHash),
		PromptContextPlanID: contract.PromptContextPlanID, PromptContextPlanHash: cleanVideoContractHash(contract.PromptContextPlanHash),
		VideoPromptPlanID: contract.VideoPromptPlanID, NativeAudioRequired: contract.NativeAudioRequired,
		TimelineTimebase: req.TimelineTimebase, FPSNumerator: req.FPSNumerator, FPSDenominator: req.FPSDenominator,
		AudioStrategy: req.AudioStrategy, AudioRequirement: req.AudioRequirement, NativeAudioStatus: selected.NativeAudioStatus,
		ProductionReadiness: selected.ProductionReadiness, Segments: selected.Segments,
	}, nil
}

func (s *Service) loadVideoRenderPlan(ctx context.Context, organizationID, planID string) (GatewayVideoPlanResponse, error) {
	var response GatewayVideoPlanResponse
	var snapshot []byte
	var initialInputContractSnapshot []byte
	var continuationInputContractSnapshot []byte
	var continuationInputContractHash, capabilityAttestationID *string
	var expiresAt time.Time
	if err := s.db.QueryRow(ctx, `
		SELECT id::text, provider_model_id::text, provider_account_id::text, model_family, variant_key,
		       capability_snapshot, capability_snapshot_hash, timeline_timebase, fps_numerator, fps_denominator, expires_at,
		       audio_strategy, audio_requirement, native_audio_status, production_readiness,
		       initial_input_contract_snapshot, initial_input_contract_hash,
		       continuation_input_contract_snapshot, continuation_input_contract_hash,
		       capability_attestation_id::text, profile_version_id::text,
		       production_profile_snapshot_hash, metadata->>'compatibilityPolicy',
		       shot_state_revision, shot_state_hash, COALESCE(transition_hash, ''),
		       reference_pack_id::text, reference_pack_hash,
		       prompt_context_plan_id::text, prompt_context_plan_hash,
		       video_prompt_plan_id::text, native_audio_required
		FROM video_render_plans
		WHERE id = $1 AND organization_id = $2
	`, planID, organizationID).Scan(
		&response.ExecutionPlanID, &response.ProviderModelID, &response.ProviderAccountID, &response.ModelFamily, &response.VariantKey,
		&snapshot, &response.CapabilitySnapshotHash, &response.TimelineTimebase, &response.FPSNumerator, &response.FPSDenominator, &expiresAt,
		&response.AudioStrategy, &response.AudioRequirement, &response.NativeAudioStatus, &response.ProductionReadiness,
		&initialInputContractSnapshot, &response.InitialInputContractHash,
		&continuationInputContractSnapshot, &continuationInputContractHash,
		&capabilityAttestationID, &response.ProductionProfileVersionID,
		&response.ProductionProfileSnapshotHash, &response.CompatibilityPolicy,
		&response.ShotStateRevision, &response.ShotStateHash, &response.TransitionHash,
		&response.ReferencePackID, &response.ReferencePackHash,
		&response.PromptContextPlanID, &response.PromptContextPlanHash,
		&response.VideoPromptPlanID, &response.NativeAudioRequired,
	); err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	if err := json.Unmarshal(snapshot, &response.CapabilitySnapshot); err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	if err := json.Unmarshal(initialInputContractSnapshot, &response.InitialInputContractSnapshot); err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	if len(continuationInputContractSnapshot) > 0 {
		var continuation VideoInputContract
		if err := json.Unmarshal(continuationInputContractSnapshot, &continuation); err != nil {
			return GatewayVideoPlanResponse{}, err
		}
		response.ContinuationInputContractSnapshot = &continuation
	}
	if continuationInputContractHash != nil {
		response.ContinuationInputContractHash = *continuationInputContractHash
	}
	if capabilityAttestationID != nil {
		response.CapabilityAttestationID = *capabilityAttestationID
	}
	response.ExpiresAt = expiresAt.Format(time.RFC3339Nano)
	rows, err := s.db.Query(ctx, `
		SELECT id::text, segment_index, planned_start_tick, planned_end_tick, planned_duration_ticks,
		       requested_duration_seconds::float8, continuity_mode, input_contract_key, input_contract_hash,
		       COALESCE(trim_end_tick, 0), dialogue
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
			&segment.PlannedDurationTicks, &segment.RequestedDurationSeconds, &segment.ContinuityMode,
			&segment.InputContractKey, &segment.InputContractHash, &segment.TrimEndTick, &dialogue); err != nil {
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
			"capabilitySnapshotHash": match.SnapshotHash, "selectionScore": match.SelectionScore,
			"capabilityAttestationId": match.CapabilityAttestationID,
			"initialInputContract":    match.Variant.InputContract.ContractKey,
			"continuationInputContract": func() string {
				if match.ContinuationInputContract == nil {
					return ""
				}
				return match.ContinuationInputContract.ContractKey
			}(),
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
		"productionProfileVersionId":        req.ProductionProfileVersionID,
		"productionProfileSnapshotHash":     cleanVideoContractHash(req.ProductionProfileSnapshotHash),
		"requiredInitialInputContract":      req.RequiredInitialInputContract,
		"allowedContinuationInputContracts": normalizeVideoStringSlice(req.AllowedContinuationInputContracts),
		"inputContractVersion":              req.InputContractVersion,
		"capabilityAttestationId":           selected.CapabilityAttestationID,
		"shotStateRevision":                 req.ShotStateRevision, "shotStateHash": cleanVideoContractHash(req.ShotStateHash),
		"transitionHash":  cleanVideoContractHash(req.TransitionHash),
		"referencePackId": req.ReferencePackID, "referencePackHash": cleanVideoContractHash(req.ReferencePackHash),
		"promptContextPlanId": req.PromptContextPlanID, "promptContextPlanHash": cleanVideoContractHash(req.PromptContextPlanHash),
		"videoPromptPlanId": req.VideoPromptPlanID, "nativeAudioRequired": req.NativeAudioRequired,
	}
	if req.Force {
		keyFields["forceWorkflowRunId"] = strings.TrimSpace(req.WorkflowRunID)
		keyFields["forceNodeAttemptGeneration"] = req.NodeAttemptGeneration
	}
	if req.OperationItemID != "" {
		keyFields["operationId"] = req.OperationID
		keyFields["operationItemId"] = req.OperationItemID
		keyFields["operationItemAttempt"] = req.OperationItemAttempt
	}
	raw, err := json.Marshal(keyFields)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func lockVideoOperationItemForPlanTx(ctx context.Context, tx pgx.Tx, req GatewayVideoPlanRequest) (string, error) {
	if req.OperationItemID == "" {
		return "", nil
	}
	var existingPlanID, itemStatus string
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(item.video_render_plan_id::text, ''), item.status
		FROM episode_video_production_items item
		JOIN episode_video_production_batches batch ON batch.id = item.batch_id
		JOIN episode_video_production_checkpoints checkpoint ON checkpoint.id = batch.checkpoint_id
		LEFT JOIN video_render_plans plan ON plan.id = item.video_render_plan_id
		WHERE item.id = $1 AND item.attempt = $2 AND item.execution_identity_version = 2
		  AND checkpoint.id = $3
		  AND checkpoint.organization_id = $4 AND checkpoint.project_id = $5
		  AND checkpoint.production_generation_id = $6
		  AND checkpoint.video_production_binding_id = $7
		  AND checkpoint.video_production_binding_revision = $8
		  AND checkpoint.workflow_run_id = $9
		  AND item.storyboard_shot_id = $10
		  AND (plan.id IS NULL OR (
		       plan.operation_item_id = item.id
		       AND plan.operation_item_attempt = item.attempt
		       AND plan.workflow_run_id = checkpoint.workflow_run_id
		       AND plan.production_generation_id = checkpoint.production_generation_id
		       AND plan.video_production_binding_id = checkpoint.video_production_binding_id
		       AND plan.video_production_binding_revision = checkpoint.video_production_binding_revision
		  ))
		FOR UPDATE OF checkpoint, batch, item
	`, req.OperationItemID, req.OperationItemAttempt, req.OperationID,
		req.OrganizationID, req.ProjectID, req.ProductionGenerationID,
		req.VideoProductionBindingID, req.VideoProductionBindingRevision,
		req.WorkflowRunID, req.StoryboardShotID).Scan(&existingPlanID, &itemStatus)
	if err != nil {
		return "", &StandardErrorError{Standard: StandardError{
			Code: CodeRenderPlanReplanRequired, Message: "视频生产项身份与当前执行上下文不一致", Retryable: false,
		}}
	}
	if existingPlanID == "" && itemStatus != "queued" && itemStatus != "running" {
		return "", &StandardErrorError{Standard: StandardError{
			Code: CodeRenderPlanReplanRequired, Message: "视频生产项已进入终态，不能创建新的执行计划", Retryable: false,
		}}
	}
	return existingPlanID, nil
}
