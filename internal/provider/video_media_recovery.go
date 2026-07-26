package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const gatewayVideoRecoverySourceTaskKey = "mediaRecoverySourceTaskId"

func (s *Service) tryPlanGatewayVideoMediaRecovery(
	ctx context.Context,
	req GatewayVideoPlanRequest,
	shotState videoPlanShotState,
) (GatewayVideoPlanResponse, bool, error) {
	selected, found, err := s.loadGatewayVideoMediaRecoveryPlanCandidate(ctx, req)
	if err != nil || !found {
		return GatewayVideoPlanResponse{}, found, err
	}
	planKey, err := videoRenderPlanKey(req, shotState.TimingRevision, selected)
	if err != nil {
		return GatewayVideoPlanResponse{}, false, err
	}
	expiresIn := req.ExpiresInSeconds
	if expiresIn <= 0 {
		expiresIn = int(defaultVideoRenderPlanTTL / time.Second)
	}
	if expiresIn > int(maximumVideoRenderPlanTTL/time.Second) {
		expiresIn = int(maximumVideoRenderPlanTTL / time.Second)
	}
	response, err := s.persistVideoRenderPlan(
		ctx,
		req,
		selected,
		videoPlanFallbackCandidates([]matchedVideoPlanCandidate{selected}),
		planKey,
		time.Now().UTC().Add(time.Duration(expiresIn)*time.Second),
	)
	if err != nil {
		return GatewayVideoPlanResponse{}, false, err
	}
	return response, true, nil
}

func (s *Service) loadGatewayVideoMediaRecoveryPlanCandidate(
	ctx context.Context,
	req GatewayVideoPlanRequest,
) (matchedVideoPlanCandidate, bool, error) {
	if req.validatedContract == nil {
		return matchedVideoPlanCandidate{}, false, nil
	}
	contract := *req.validatedContract
	var (
		sourcePlanID                  string
		modelProfileID                string
		modelProfileBindingID         string
		modelProfileKey               string
		providerAccountID             string
		providerModelID               string
		modelFamily                   string
		variantKey                    string
		capabilitySnapshot            []byte
		capabilitySnapshotHashValue   string
		initialInputContractHash      string
		continuationInputContract     []byte
		continuationInputContractHash string
		capabilityAttestationID       string
		nativeAudioStatus             string
		productionReadiness           string
	)
	err := s.db.QueryRow(ctx, `
		SELECT source_plan.id::text,
		       COALESCE(source_plan.model_profile_id::text, ''),
		       COALESCE(source_plan.model_profile_binding_id::text, ''),
		       COALESCE(source_plan.model_profile_key, ''),
		       source_plan.provider_account_id::text,
		       source_plan.provider_model_id::text,
		       source_plan.model_family,
		       source_plan.variant_key,
		       source_plan.capability_snapshot,
		       source_plan.capability_snapshot_hash,
		       source_plan.initial_input_contract_hash,
		       source_plan.continuation_input_contract_snapshot,
		       COALESCE(source_plan.continuation_input_contract_hash, ''),
		       COALESCE(source_plan.capability_attestation_id::text, ''),
		       source_plan.native_audio_status,
		       source_plan.production_readiness
		FROM video_render_plans source_plan
		JOIN provider_accounts source_account
		  ON source_account.id = source_plan.provider_account_id
		 AND source_account.organization_id = source_plan.organization_id
		JOIN provider_models source_model
		  ON source_model.id = source_plan.provider_model_id
		 AND source_model.provider_account_id = source_account.id
		WHERE source_plan.organization_id = @organization_id
		  AND source_plan.project_id = NULLIF(@project_id, '')::uuid
		  AND source_plan.production_generation_id = NULLIF(@production_generation_id, '')::uuid
		  AND source_plan.video_production_binding_id = NULLIF(@binding_id, '')::uuid
		  AND source_plan.video_production_binding_revision = @binding_revision
		  AND source_plan.storyboard_plan_id = NULLIF(@storyboard_plan_id, '')::uuid
		  AND source_plan.storyboard_shot_id = NULLIF(@storyboard_shot_id, '')::uuid
		  AND source_plan.profile_version_id = NULLIF(@profile_version_id, '')::uuid
		  AND source_plan.production_profile_snapshot_hash = @profile_snapshot_hash
		  AND source_plan.shot_state_revision = @shot_state_revision
		  AND source_plan.shot_state_hash = @shot_state_hash
		  AND source_plan.transition_hash IS NOT DISTINCT FROM NULLIF(@transition_hash, '')
		  AND source_plan.reference_pack_id = NULLIF(@reference_pack_id, '')::uuid
		  AND source_plan.reference_pack_hash = @reference_pack_hash
		  AND source_plan.prompt_context_plan_id = NULLIF(@prompt_context_plan_id, '')::uuid
		  AND source_plan.prompt_context_plan_hash = @prompt_context_plan_hash
		  AND source_plan.video_prompt_plan_id = NULLIF(@video_prompt_plan_id, '')::uuid
		  AND source_plan.dialogue_cues = @dialogue_cues::jsonb
		  AND source_plan.native_audio_required = @native_audio_required
		  AND source_plan.target_duration_ticks = @target_duration_ticks
		  AND source_plan.timeline_timebase = @timeline_timebase
		  AND source_plan.fps_numerator = @fps_numerator
		  AND source_plan.fps_denominator = @fps_denominator
		  AND source_plan.task_type = @task_type
		  AND source_plan.reference_mode = @reference_mode
		  AND source_plan.aspect_ratio = @aspect_ratio
		  AND source_plan.resolution = @resolution
		  AND source_plan.audio_strategy = @audio_strategy
		  AND source_plan.audio_requirement = @audio_requirement
		  AND source_plan.commerce_script_unit_id IS NOT DISTINCT FROM NULLIF(@commerce_script_unit_id, '')::uuid
		  AND source_plan.commerce_script_unit_generation_id IS NOT DISTINCT FROM NULLIF(@commerce_script_unit_generation_id, '')::uuid
		  AND source_plan.commerce_product_id IS NOT DISTINCT FROM NULLIF(@commerce_product_id, '')::uuid
		  AND source_plan.product_version_id IS NOT DISTINCT FROM NULLIF(@product_version_id, '')::uuid
		  AND source_plan.localization_id IS NOT DISTINCT FROM NULLIF(@localization_id, '')::uuid
		  AND source_plan.product_reference_pack_id IS NOT DISTINCT FROM NULLIF(@product_reference_pack_id, '')::uuid
		  AND source_plan.commerce_workflow_binding_id IS NOT DISTINCT FROM NULLIF(@commerce_workflow_binding_id, '')::uuid
		  AND source_plan.localized_contract_hash IS NOT DISTINCT FROM NULLIF(@localized_contract_hash, '')
		  AND source_plan.target_language IS NOT DISTINCT FROM NULLIF(@target_language, '')
		  AND source_plan.verbatim_voiceover_hash IS NOT DISTINCT FROM NULLIF(@verbatim_voiceover_hash, '')
		  AND source_plan.timing_policy_version IS NOT DISTINCT FROM NULLIF(@timing_policy_version, '')
		  AND source_plan.language_capability_snapshot_hash IS NOT DISTINCT FROM NULLIF(@language_capability_snapshot_hash, '')
		  AND source_plan.metadata->>'compatibilityPolicy' = @compatibility_policy
		  AND source_plan.metadata->>'inputContractVersion' = @input_contract_version
		  AND source_plan.metadata->>'videoPromptHash' = @video_prompt_hash
		  AND COALESCE(source_plan.metadata->>'hasDialogue', 'false')::boolean = @has_dialogue
		  AND COALESCE(source_plan.metadata->>'dialogueLanguage', '') = @dialogue_language
		  AND COALESCE(source_plan.metadata->>'promptLanguage', '') = @prompt_language
		  AND source_plan.initial_input_contract_snapshot->>'contractKey' = @required_initial_input_contract
		  AND EXISTS (
		    SELECT 1 FROM video_render_segments source_segment
		    WHERE source_segment.video_render_plan_id = source_plan.id
		  )
		  AND NOT EXISTS (
		    SELECT 1
		    FROM video_render_segments source_segment
		    WHERE source_segment.video_render_plan_id = source_plan.id
		      AND NOT EXISTS (
		        SELECT 1
		        FROM provider_async_tasks source_task
		        JOIN provider_credentials source_credential
		          ON source_credential.id = source_task.credential_id
		         AND source_credential.organization_id = source_task.organization_id
		         AND source_credential.provider_account_id = source_task.provider_account_id
		         AND source_credential.status = 'active'
		        WHERE source_task.video_render_plan_id = source_plan.id
		          AND source_task.video_render_segment_id = source_segment.id
		          AND source_task.organization_id = source_plan.organization_id
		          AND source_task.project_id = source_plan.project_id
		          AND source_task.production_generation_id = source_plan.production_generation_id
		          AND source_task.video_production_binding_id = source_plan.video_production_binding_id
		          AND source_task.video_production_binding_revision = source_plan.video_production_binding_revision
		          AND source_task.provider_account_id = source_plan.provider_account_id
		          AND source_task.provider_model_id = source_plan.provider_model_id
		          AND source_task.status = 'failed'
		          AND source_task.error_code = @media_download_failed
		          AND lower(COALESCE(
		            source_task.last_response_snapshot #>> '{status}',
		            source_task.last_response_snapshot #>> '{data,status}',
		            source_task.last_response_snapshot #>> '{data,data,status}',
		            ''
		          )) IN ('success', 'succeeded', 'completed')
		          AND COALESCE(
		            source_task.last_response_snapshot #>> '{videoUrl}',
		            source_task.last_response_snapshot #>> '{video_url}',
		            source_task.last_response_snapshot #>> '{data,videoUrl}',
		            source_task.last_response_snapshot #>> '{data,video_url}',
		            source_task.last_response_snapshot #>> '{data,data,videoUrl}',
		            source_task.last_response_snapshot #>> '{data,data,video_url}',
		            ''
		          ) <> ''
		          AND NOT EXISTS (
		            SELECT 1
		            FROM provider_async_tasks recovery_task
		            WHERE recovery_task.input->>@recovery_source_key = source_task.id::text
		              AND recovery_task.status IN ('queued', 'running', 'cancelling', 'succeeded')
		          )
		      )
		  )
		ORDER BY source_plan.created_at DESC
		LIMIT 1
	`, pgx.NamedArgs{
		"organization_id": req.OrganizationID, "project_id": req.ProjectID,
		"production_generation_id": req.ProductionGenerationID, "binding_id": req.VideoProductionBindingID,
		"binding_revision": req.VideoProductionBindingRevision, "storyboard_plan_id": req.StoryboardPlanID,
		"storyboard_shot_id": req.StoryboardShotID, "profile_version_id": contract.ProfileVersionID,
		"profile_snapshot_hash": cleanVideoContractHash(contract.ProfileSnapshotHash),
		"shot_state_revision":   contract.ShotStateRevision, "shot_state_hash": cleanVideoContractHash(contract.ShotStateHash),
		"transition_hash":   cleanVideoContractHash(contract.TransitionHash),
		"reference_pack_id": contract.ReferencePackID, "reference_pack_hash": cleanVideoContractHash(contract.ReferencePackHash),
		"prompt_context_plan_id": contract.PromptContextPlanID, "prompt_context_plan_hash": cleanVideoContractHash(contract.PromptContextPlanHash),
		"video_prompt_plan_id": contract.VideoPromptPlanID, "dialogue_cues": mustJSON(contract.DialogueCues),
		"native_audio_required": contract.NativeAudioRequired,
		"target_duration_ticks": req.TargetDurationTicks, "timeline_timebase": req.TimelineTimebase,
		"fps_numerator": req.FPSNumerator, "fps_denominator": req.FPSDenominator,
		"task_type": req.TaskType, "reference_mode": req.ReferenceMode, "aspect_ratio": req.AspectRatio,
		"resolution": req.Resolution, "audio_strategy": req.AudioStrategy, "audio_requirement": req.AudioRequirement,
		"commerce_script_unit_id":            contract.CommerceScriptUnitID,
		"commerce_script_unit_generation_id": contract.CommerceScriptUnitGenerationID,
		"commerce_product_id":                contract.CommerceProductID, "product_version_id": contract.ProductVersionID,
		"localization_id": contract.LocalizationID, "product_reference_pack_id": contract.ProductReferencePackID,
		"commerce_workflow_binding_id":      contract.CommerceWorkflowBindingID,
		"localized_contract_hash":           cleanVideoContractHash(contract.LocalizedContractHash),
		"target_language":                   contract.TargetLanguage,
		"verbatim_voiceover_hash":           cleanVideoContractHash(contract.VerbatimVoiceoverHash),
		"timing_policy_version":             contract.TimingPolicyVersion,
		"language_capability_snapshot_hash": cleanVideoContractHash(contract.LanguageCapabilitySnapshotHash),
		"compatibility_policy":              contract.CompatibilityPolicy, "input_contract_version": contract.InputContractVersion,
		"video_prompt_hash": cleanVideoContractHash(contract.VideoPromptHash),
		"has_dialogue":      req.HasDialogue, "dialogue_language": req.DialogueLanguage,
		"prompt_language": req.PromptLanguage, "required_initial_input_contract": contract.RequiredInitialInputContract,
		"media_download_failed": CodeMediaDownloadFailed, "recovery_source_key": gatewayVideoRecoverySourceTaskKey,
	}).Scan(
		&sourcePlanID,
		&modelProfileID,
		&modelProfileBindingID,
		&modelProfileKey,
		&providerAccountID,
		&providerModelID,
		&modelFamily,
		&variantKey,
		&capabilitySnapshot,
		&capabilitySnapshotHashValue,
		&initialInputContractHash,
		&continuationInputContract,
		&continuationInputContractHash,
		&capabilityAttestationID,
		&nativeAudioStatus,
		&productionReadiness,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return matchedVideoPlanCandidate{}, false, nil
		}
		return matchedVideoPlanCandidate{}, false, err
	}

	var variant VideoGenerationVariant
	if err := json.Unmarshal(capabilitySnapshot, &variant); err != nil {
		return matchedVideoPlanCandidate{}, false, gatewayVideoRecoveryInvalid("历史视频能力快照无法读取")
	}
	if variant.ModelFamily != modelFamily || variant.VariantKey != variantKey {
		return matchedVideoPlanCandidate{}, false, gatewayVideoRecoveryInvalid("历史视频能力快照身份不一致")
	}
	computedCapabilityHash, err := capabilitySnapshotHash(variant)
	if err != nil {
		return matchedVideoPlanCandidate{}, false, err
	}
	if computedCapabilityHash != capabilitySnapshotHashValue {
		return matchedVideoPlanCandidate{}, false, gatewayVideoRecoveryInvalid("历史视频能力快照校验失败")
	}
	computedInitialHash, err := videoInputContractHash(variant.InputContract)
	if err != nil {
		return matchedVideoPlanCandidate{}, false, err
	}
	if computedInitialHash != initialInputContractHash {
		return matchedVideoPlanCandidate{}, false, gatewayVideoRecoveryInvalid("历史视频输入契约校验失败")
	}
	var continuation *VideoInputContract
	if len(continuationInputContract) > 0 {
		var decoded VideoInputContract
		if err := json.Unmarshal(continuationInputContract, &decoded); err != nil {
			return matchedVideoPlanCandidate{}, false, gatewayVideoRecoveryInvalid("历史视频续接输入契约无法读取")
		}
		computedContinuationHash, err := videoInputContractHash(decoded)
		if err != nil {
			return matchedVideoPlanCandidate{}, false, err
		}
		if computedContinuationHash != continuationInputContractHash {
			return matchedVideoPlanCandidate{}, false, gatewayVideoRecoveryInvalid("历史视频续接输入契约校验失败")
		}
		continuation = &decoded
	}

	segments, err := s.loadGatewayVideoRecoveryPlanSegments(ctx, sourcePlanID, req.TimelineTimebase)
	if err != nil {
		return matchedVideoPlanCandidate{}, false, err
	}
	if len(segments) == 0 {
		return matchedVideoPlanCandidate{}, false, nil
	}
	return matchedVideoPlanCandidate{
		Candidate: resolvedVideoPlanCandidate{
			ModelProfileID: modelProfileID, ModelProfileBindingID: modelProfileBindingID,
			ModelProfileKey: modelProfileKey, ProviderAccountID: providerAccountID,
			Model: Model{ID: providerModelID, ProviderAccountID: providerAccountID},
		},
		Variant: variant, ContinuationInputContract: continuation,
		CapabilityAttestationID: capabilityAttestationID, RecoverySourcePlanID: sourcePlanID,
		SnapshotHash: capabilitySnapshotHashValue, NativeAudioStatus: nativeAudioStatus,
		ProductionReadiness: productionReadiness, Segments: segments,
	}, true, nil
}

func (s *Service) loadGatewayVideoRecoveryPlanSegments(
	ctx context.Context,
	sourcePlanID string,
	timelineTimebase int64,
) ([]GatewayVideoPlanSegment, error) {
	rows, err := s.db.Query(ctx, `
		SELECT segment_index, planned_start_tick, planned_end_tick, planned_duration_ticks,
		       requested_duration_seconds::float8, continuity_mode, input_contract_key,
		       input_contract_hash, COALESCE(trim_end_tick, 0), dialogue
		FROM video_render_segments
		WHERE video_render_plan_id = $1
		ORDER BY segment_index
	`, sourcePlanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	segments := make([]GatewayVideoPlanSegment, 0)
	for rows.Next() {
		var segment GatewayVideoPlanSegment
		var dialogue []byte
		if err := rows.Scan(
			&segment.SegmentIndex,
			&segment.PlannedStartTick,
			&segment.PlannedEndTick,
			&segment.PlannedDurationTicks,
			&segment.RequestedDurationSeconds,
			&segment.ContinuityMode,
			&segment.InputContractKey,
			&segment.InputContractHash,
			&segment.TrimEndTick,
			&dialogue,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(dialogue, &segment.DialogueSpans); err != nil {
			return nil, err
		}
		segment.PlannedDurationSeconds = float64(segment.PlannedDurationTicks) / float64(timelineTimebase)
		segments = append(segments, segment)
	}
	return segments, rows.Err()
}

// tryBindGatewayVideoMediaRecovery reuses an upstream task only when its full
// render contract is identical to the currently active segment. It creates a
// new local task identity so the historical workflow remains immutable.
func (s *Service) tryBindGatewayVideoMediaRecovery(
	ctx context.Context,
	req GatewayVideoCreateTaskRequest,
	input gatewayVideoInput,
	segment videoExecutionSegment,
	providerRequestID string,
	attemptGeneration int,
) (GatewayVideoCreateTaskResponse, bool, error) {
	source, found, err := s.findGatewayVideoMediaRecoverySource(ctx, req)
	if err != nil || !found {
		return GatewayVideoCreateTaskResponse{}, found, err
	}
	if source.ProviderAccountID != segment.ProviderAccountID || source.ProviderModelID != segment.ProviderModelID {
		return GatewayVideoCreateTaskResponse{}, false, nil
	}
	account, err := s.GetAccount(ctx, req.OrganizationID, source.ProviderAccountID)
	if err != nil {
		return GatewayVideoCreateTaskResponse{}, false, nil
	}
	model, err := s.GetModel(ctx, req.OrganizationID, source.ProviderModelID)
	if err != nil {
		return GatewayVideoCreateTaskResponse{}, false, nil
	}
	if strings.TrimSpace(source.CredentialID) == "" {
		return GatewayVideoCreateTaskResponse{}, false, nil
	}
	credential, credentialID, err := s.credentialPayloadByID(ctx, req.OrganizationID, account.ID, source.CredentialID)
	if err != nil {
		return GatewayVideoCreateTaskResponse{}, false, nil
	}
	apiKey, err := apiKeyFromCredential(credential)
	if err != nil {
		return GatewayVideoCreateTaskResponse{}, false, nil
	}
	selection := gatewayModelSelection{
		Account:               account,
		Model:                 model,
		CredentialID:          credentialID,
		Credential:            credential,
		APIKey:                apiKey,
		ModelProfileID:        segment.ModelProfileID,
		ModelProfileBindingID: segment.ModelProfileBindingID,
		ModelProfileKey:       segment.ModelProfileKey,
	}

	req.Input = withGatewayVideoRecoverySource(req.Input, source.ID)
	callID := uuid.NewString()
	externalTaskID := gatewayVideoRecoveryTaskExternalID(source.ID, req.NodeRunID, req.NodeAttemptGeneration)
	normalizedOutput := mustJSON(map[string]any{
		"status":                          "running",
		"mediaRecoveryStatus":             "bound",
		gatewayVideoRecoverySourceTaskKey: source.ID,
	})
	responseSnapshot := mustJSON(map[string]any{
		"status":                          "media_recovery_bound",
		gatewayVideoRecoverySourceTaskKey: source.ID,
	})
	call, taskID, err := s.recordVideoCreateTask(
		ctx,
		selection,
		req,
		providerRequestID,
		attemptGeneration,
		1,
		callID,
		"",
		externalTaskID,
		"running",
		0,
		"",
		"",
		nil,
		"",
		req.Input,
		responseSnapshot,
		normalizedOutput,
		GatewayUsage{EstimatedCost: "0.00000000", Currency: "USD"},
		nil,
		input,
	)
	if err != nil {
		return GatewayVideoCreateTaskResponse{}, false, err
	}
	return GatewayVideoCreateTaskResponse{
		ProviderRequestID:   providerRequestID,
		AttemptGeneration:   attemptGeneration,
		ProviderCallID:      call.ID,
		ProviderAsyncTaskID: taskID,
		ExternalTaskID:      externalTaskID,
		ModelID:             selection.Model.ID,
		Status:              "running",
		ExecutionPlanID:     req.ExecutionPlanID,
		RenderSegmentID:     req.RenderSegmentID,
	}, true, nil
}

func (s *Service) findGatewayVideoMediaRecoverySource(
	ctx context.Context,
	req GatewayVideoCreateTaskRequest,
) (gatewayVideoTask, bool, error) {
	if strings.TrimSpace(req.ExecutionPlanID) == "" || strings.TrimSpace(req.RenderSegmentID) == "" {
		return gatewayVideoTask{}, false, nil
	}
	return s.findGatewayVideoMediaRecoverySourceForIdentity(ctx, gatewayVideoRecoverySearch{
		OrganizationID:                 req.OrganizationID,
		ProjectID:                      req.ProjectID,
		ProductionGenerationID:         req.ProductionGenerationID,
		VideoProductionBindingID:       req.VideoProductionBindingID,
		VideoProductionBindingRevision: req.VideoProductionBindingRevision,
		ExecutionPlanID:                req.ExecutionPlanID,
		RenderSegmentID:                req.RenderSegmentID,
	})
}

type gatewayVideoRecoverySearch struct {
	OrganizationID                 string
	ProjectID                      string
	ProductionGenerationID         string
	VideoProductionBindingID       string
	VideoProductionBindingRevision int64
	ExecutionPlanID                string
	RenderSegmentID                string
	IgnoreRecoveryTaskID           string
}

func (s *Service) findGatewayVideoMediaRecoverySourceForIdentity(
	ctx context.Context,
	search gatewayVideoRecoverySearch,
) (gatewayVideoTask, bool, error) {
	var sourceID string
	err := s.db.QueryRow(ctx, `
		SELECT source_task.id::text
		FROM video_render_plans current_plan
		JOIN video_render_segments current_segment
		  ON current_segment.id = $2
		 AND current_segment.video_render_plan_id = current_plan.id
		JOIN video_render_plans source_plan
		  ON (
		    source_plan.organization_id,
		    source_plan.project_id,
		    source_plan.production_generation_id,
		    source_plan.video_production_binding_id,
		    source_plan.video_production_binding_revision,
		    source_plan.storyboard_shot_id,
		    source_plan.provider_account_id,
		    source_plan.provider_model_id,
		    source_plan.model_profile_id,
		    source_plan.model_profile_binding_id,
		    source_plan.model_profile_key,
		    source_plan.variant_key,
		    source_plan.capability_snapshot_hash,
		    source_plan.target_duration_ticks,
		    source_plan.timeline_timebase,
		    source_plan.fps_numerator,
		    source_plan.fps_denominator,
		    source_plan.reference_mode,
		    source_plan.aspect_ratio,
		    source_plan.resolution,
		    source_plan.audio_strategy,
		    source_plan.audio_requirement,
		    source_plan.production_profile_snapshot_hash,
		    source_plan.shot_state_revision,
		    source_plan.shot_state_hash,
		    source_plan.transition_hash,
		    source_plan.reference_pack_id,
		    source_plan.reference_pack_hash,
		    source_plan.prompt_context_plan_id,
		    source_plan.prompt_context_plan_hash,
		    source_plan.video_prompt_plan_id,
		    source_plan.dialogue_cues,
		    source_plan.native_audio_required,
		    source_plan.commerce_script_unit_id,
		    source_plan.commerce_script_unit_generation_id,
		    source_plan.commerce_product_id,
		    source_plan.product_version_id,
		    source_plan.localization_id,
		    source_plan.product_reference_pack_id,
		    source_plan.commerce_workflow_binding_id,
		    source_plan.localized_contract_hash,
		    source_plan.target_language,
		    source_plan.verbatim_voiceover_hash,
		    source_plan.timing_policy_version,
		    source_plan.language_capability_snapshot_hash
		  ) IS NOT DISTINCT FROM (
		    current_plan.organization_id,
		    current_plan.project_id,
		    current_plan.production_generation_id,
		    current_plan.video_production_binding_id,
		    current_plan.video_production_binding_revision,
		    current_plan.storyboard_shot_id,
		    current_plan.provider_account_id,
		    current_plan.provider_model_id,
		    current_plan.model_profile_id,
		    current_plan.model_profile_binding_id,
		    current_plan.model_profile_key,
		    current_plan.variant_key,
		    current_plan.capability_snapshot_hash,
		    current_plan.target_duration_ticks,
		    current_plan.timeline_timebase,
		    current_plan.fps_numerator,
		    current_plan.fps_denominator,
		    current_plan.reference_mode,
		    current_plan.aspect_ratio,
		    current_plan.resolution,
		    current_plan.audio_strategy,
		    current_plan.audio_requirement,
		    current_plan.production_profile_snapshot_hash,
		    current_plan.shot_state_revision,
		    current_plan.shot_state_hash,
		    current_plan.transition_hash,
		    current_plan.reference_pack_id,
		    current_plan.reference_pack_hash,
		    current_plan.prompt_context_plan_id,
		    current_plan.prompt_context_plan_hash,
		    current_plan.video_prompt_plan_id,
		    current_plan.dialogue_cues,
		    current_plan.native_audio_required,
		    current_plan.commerce_script_unit_id,
		    current_plan.commerce_script_unit_generation_id,
		    current_plan.commerce_product_id,
		    current_plan.product_version_id,
		    current_plan.localization_id,
		    current_plan.product_reference_pack_id,
		    current_plan.commerce_workflow_binding_id,
		    current_plan.localized_contract_hash,
		    current_plan.target_language,
		    current_plan.verbatim_voiceover_hash,
		    current_plan.timing_policy_version,
		    current_plan.language_capability_snapshot_hash
		  )
		JOIN video_render_segments source_segment
		  ON source_segment.video_render_plan_id = source_plan.id
		 AND (
		    source_segment.storyboard_shot_id,
		    source_segment.segment_index,
		    source_segment.planned_start_tick,
		    source_segment.planned_end_tick,
		    source_segment.requested_duration_seconds,
		    source_segment.trim_end_tick,
		    source_segment.continuity_mode,
		    source_segment.provider_model_id,
		    source_segment.prompt,
		    source_segment.dialogue,
		    source_segment.native_audio_requested,
		    source_segment.input_contract_key,
		    source_segment.input_contract_hash,
		    source_segment.source_video_prompt_plan_id,
		    source_segment.source_prompt_hash,
		    source_segment.execution_prompt_hash
		  ) IS NOT DISTINCT FROM (
		    current_segment.storyboard_shot_id,
		    current_segment.segment_index,
		    current_segment.planned_start_tick,
		    current_segment.planned_end_tick,
		    current_segment.requested_duration_seconds,
		    current_segment.trim_end_tick,
		    current_segment.continuity_mode,
		    current_segment.provider_model_id,
		    current_segment.prompt,
		    current_segment.dialogue,
		    current_segment.native_audio_requested,
		    current_segment.input_contract_key,
		    current_segment.input_contract_hash,
		    current_segment.source_video_prompt_plan_id,
		    current_segment.source_prompt_hash,
		    current_segment.execution_prompt_hash
		  )
		JOIN provider_async_tasks source_task
		  ON source_task.video_render_plan_id = source_plan.id
		 AND source_task.video_render_segment_id = source_segment.id
		 AND source_task.organization_id = current_plan.organization_id
		 AND source_task.project_id = current_plan.project_id
		 AND source_task.production_generation_id = current_plan.production_generation_id
		 AND source_task.video_production_binding_id = current_plan.video_production_binding_id
		 AND source_task.video_production_binding_revision = current_plan.video_production_binding_revision
		 AND source_task.provider_account_id = current_plan.provider_account_id
		 AND source_task.provider_model_id = current_plan.provider_model_id
		 AND source_task.status = 'failed'
		 AND source_task.error_code = $8
		WHERE current_plan.id = $1
		  AND current_plan.organization_id = $3
		  AND current_plan.project_id = NULLIF($4, '')::uuid
		  AND current_plan.production_generation_id = NULLIF($5, '')::uuid
		  AND current_plan.video_production_binding_id = NULLIF($6, '')::uuid
		  AND current_plan.video_production_binding_revision = $7
		  AND current_plan.active = true
		  AND current_plan.status NOT IN ('stale', 'archived', 'cancelled', 'replan_required')
		  AND ($10 = '' OR current_segment.provider_async_task_id = NULLIF($10, '')::uuid)
		  AND lower(COALESCE(
		    source_task.last_response_snapshot #>> '{status}',
		    source_task.last_response_snapshot #>> '{data,status}',
		    source_task.last_response_snapshot #>> '{data,data,status}',
		    ''
		  )) IN ('success', 'succeeded', 'completed')
		  AND COALESCE(
		    source_task.last_response_snapshot #>> '{videoUrl}',
		    source_task.last_response_snapshot #>> '{video_url}',
		    source_task.last_response_snapshot #>> '{data,videoUrl}',
		    source_task.last_response_snapshot #>> '{data,video_url}',
		    source_task.last_response_snapshot #>> '{data,data,videoUrl}',
		    source_task.last_response_snapshot #>> '{data,data,video_url}',
		    ''
		  ) <> ''
		  AND NOT EXISTS (
		    SELECT 1
		    FROM provider_async_tasks recovery_task
		    WHERE recovery_task.input->>$9 = source_task.id::text
		      AND ($10 = '' OR recovery_task.id <> NULLIF($10, '')::uuid)
		      AND recovery_task.status IN ('queued', 'running', 'cancelling', 'succeeded')
		  )
		ORDER BY source_task.completed_at DESC NULLS LAST, source_task.created_at DESC
		LIMIT 1
	`, search.ExecutionPlanID, search.RenderSegmentID, search.OrganizationID, search.ProjectID,
		search.ProductionGenerationID, search.VideoProductionBindingID, search.VideoProductionBindingRevision,
		CodeMediaDownloadFailed, gatewayVideoRecoverySourceTaskKey, search.IgnoreRecoveryTaskID).Scan(&sourceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gatewayVideoTask{}, false, nil
		}
		return gatewayVideoTask{}, false, err
	}
	source, err := s.getGatewayVideoTask(ctx, GatewayVideoPollTaskRequest{
		OrganizationID:      search.OrganizationID,
		ProviderAsyncTaskID: sourceID,
	})
	if err != nil {
		return gatewayVideoTask{}, false, err
	}
	return source, true, nil
}

func (s *Service) gatewayVideoPollSourceTask(ctx context.Context, task gatewayVideoTask) (gatewayVideoTask, error) {
	sourceID := gatewayVideoRecoverySourceTaskID(task.Input)
	if sourceID == "" {
		return task, nil
	}
	source, err := s.getGatewayVideoTask(ctx, GatewayVideoPollTaskRequest{
		OrganizationID:      task.OrganizationID,
		ProviderAsyncTaskID: sourceID,
	})
	if err != nil {
		return gatewayVideoTask{}, gatewayVideoRecoveryInvalid("历史视频任务不存在")
	}
	if source.ID == task.ID ||
		source.OrganizationID != task.OrganizationID ||
		source.ProjectID != task.ProjectID ||
		source.ProductionGenerationID != task.ProductionGenerationID ||
		source.VideoProductionBindingID != task.VideoProductionBindingID ||
		source.VideoProductionBindingRevision != task.VideoProductionBindingRevision ||
		source.ProviderAccountID != task.ProviderAccountID ||
		source.ProviderModelID != task.ProviderModelID ||
		source.CredentialID != task.CredentialID {
		return gatewayVideoTask{}, gatewayVideoRecoveryInvalid("历史视频任务身份不一致")
	}
	candidate, found, err := s.findGatewayVideoMediaRecoverySourceForIdentity(ctx, gatewayVideoRecoverySearch{
		OrganizationID:                 task.OrganizationID,
		ProjectID:                      task.ProjectID,
		ProductionGenerationID:         task.ProductionGenerationID,
		VideoProductionBindingID:       task.VideoProductionBindingID,
		VideoProductionBindingRevision: task.VideoProductionBindingRevision,
		ExecutionPlanID:                task.ExecutionPlanID,
		RenderSegmentID:                task.RenderSegmentID,
		IgnoreRecoveryTaskID:           task.ID,
	})
	if err != nil {
		return gatewayVideoTask{}, err
	}
	if !found || candidate.ID != source.ID {
		return gatewayVideoTask{}, gatewayVideoRecoveryInvalid("历史视频任务与当前镜头契约不一致")
	}
	return source, nil
}

func withGatewayVideoRecoverySource(input json.RawMessage, sourceTaskID string) json.RawMessage {
	var decoded map[string]any
	if err := json.Unmarshal(input, &decoded); err != nil || decoded == nil {
		decoded = map[string]any{}
	}
	decoded[gatewayVideoRecoverySourceTaskKey] = strings.TrimSpace(sourceTaskID)
	raw, err := json.Marshal(decoded)
	if err != nil {
		return input
	}
	return raw
}

func gatewayVideoRecoverySourceTaskID(input json.RawMessage) string {
	var decoded map[string]any
	if err := json.Unmarshal(input, &decoded); err != nil {
		return ""
	}
	value, _ := decoded[gatewayVideoRecoverySourceTaskKey].(string)
	return strings.TrimSpace(value)
}

func gatewayVideoRecoveryTaskExternalID(sourceTaskID, nodeRunID string, attemptGeneration int) string {
	name := fmt.Sprintf("%s|%s|%d", strings.TrimSpace(sourceTaskID), strings.TrimSpace(nodeRunID), attemptGeneration)
	return "recovery-" + uuid.NewSHA1(uuid.NameSpaceURL, []byte(name)).String()
}

func gatewayVideoTaskRecordingMode(input json.RawMessage) (callTaskType, callExecutionMode, taskExecutionMode, callStatus string) {
	if gatewayVideoRecoverySourceTaskID(input) != "" {
		return TaskTypeVideoPollTask, "async_poll", "media_recovery", "succeeded"
	}
	return TaskTypeVideoCreateTask, "async_create", "async_polling", ""
}

func gatewayVideoRecoveryInvalid(message string) error {
	return &StandardErrorError{Standard: StandardError{
		Code:      CodeRenderPlanReplanRequired,
		Message:   message,
		Retryable: false,
	}}
}
