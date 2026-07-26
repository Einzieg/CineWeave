package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *CommerceGenerationRuntime) BeginCommerceVideoItem(
	ctx context.Context,
	input CommerceVideoBatchInput,
	shotID string,
) (CommerceReferenceImageItemAttempt, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceReferenceImageItemAttempt{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := r.lockGenerationState(ctx, tx, input.Identity); err != nil {
		return CommerceReferenceImageItemAttempt{}, err
	}
	phase, err := commerceVideoPhase(input.Operation)
	if err != nil {
		return CommerceReferenceImageItemAttempt{}, err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowRunID, phase, input); err != nil {
		return CommerceReferenceImageItemAttempt{}, err
	}
	expectedHash, err := CommerceVideoSubjectHash(input, shotID)
	if err != nil {
		return CommerceReferenceImageItemAttempt{}, err
	}
	var itemID string
	if err := tx.QueryRow(ctx, `
		SELECT id::text
		FROM commerce_production_run_items
		WHERE organization_id = $1 AND project_id = $2
		  AND run_id = $3 AND storyboard_shot_id = $4
		  AND script_unit_id = $5 AND script_unit_generation_id = $6
		  AND subject_type = 'storyboard_shot' AND input_hash = $7
		FOR UPDATE
	`, input.Identity.OrganizationID, input.Identity.ProjectID, input.ProductionRunID,
		shotID, input.Identity.ScriptUnitID, input.Identity.UnitGenerationID, expectedHash).Scan(&itemID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CommerceReferenceImageItemAttempt{}, generationMismatch("视频生产项与冻结批次不匹配", err)
		}
		return CommerceReferenceImageItemAttempt{}, err
	}
	attempt, err := r.runs.StartAttempt(ctx, tx, commerce.StartProductionAttemptParams{
		OrganizationID: input.Identity.OrganizationID,
		ProjectID:      input.Identity.ProjectID,
		RunID:          input.ProductionRunID,
		ItemID:         itemID,
		InputHash:      expectedHash,
		WorkflowRunID:  input.WorkflowRunID,
	})
	if err != nil {
		return CommerceReferenceImageItemAttempt{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommerceReferenceImageItemAttempt{}, err
	}
	return CommerceReferenceImageItemAttempt{
		ItemID:        attempt.ItemID,
		AttemptID:     attempt.ID,
		AttemptNumber: attempt.AttemptNumber,
		InputHash:     attempt.InputHash,
	}, nil
}

func (r *CommerceGenerationRuntime) CommitCommerceVideoPromptPlan(
	ctx context.Context,
	input CommitCommerceVideoPromptPlanInput,
) (CommerceVideoPromptPlanState, error) {
	if err := ValidateCommerceVideoPromptPlan(input.Contract, input.Snapshot); err != nil {
		return CommerceVideoPromptPlanState{}, commerce.Error{Code: CommerceCodeVideoPromptContractInvalid, Message: "视频提示词契约无效", Cause: err}
	}
	if err := ValidateCommerceVideoPromptReview(input.Review); err != nil || input.Review.Decision != "approve" {
		return CommerceVideoPromptPlanState{}, commerce.Error{Code: CommerceCodeVideoPromptContractInvalid, Message: "视频提示词尚未通过审核", Cause: err}
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceVideoPromptPlanState{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := r.lockGenerationState(ctx, tx, input.WorkflowInput.Identity); err != nil {
		return CommerceVideoPromptPlanState{}, err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowInput.WorkflowRunID, CommercePhaseVideoPrompt, input.WorkflowInput); err != nil {
		return CommerceVideoPromptPlanState{}, err
	}
	if err := r.assertCurrentCommerceVideoSnapshotTx(ctx, tx, input.Snapshot); err != nil {
		return CommerceVideoPromptPlanState{}, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "commerce-video-prompt:"+input.Snapshot.StoryboardShotID); err != nil {
		return CommerceVideoPromptPlanState{}, err
	}

	if err := staleCommerceVideoContractsTx(ctx, tx, input.Snapshot.StoryboardShotID, input.Snapshot.Identity.ProjectGenerationID); err != nil {
		return CommerceVideoPromptPlanState{}, err
	}

	shotState := map[string]any{
		"contractVersion":     "commerce-shot-video-entry-state/v1",
		"storyboardShotId":    input.Snapshot.StoryboardShotID,
		"shotContractHash":    input.Snapshot.ShotContractHash,
		"visualAction":        input.Snapshot.VisualAction,
		"salesBeat":           input.Snapshot.SalesBeat,
		"productPresentation": input.Snapshot.ProductPresentation,
		"firstFrame":          input.Snapshot.FirstFrame,
		"durationSeconds":     input.Snapshot.DurationSeconds,
		"aspectRatio":         input.Snapshot.AspectRatio,
	}
	shotStateHash, err := commerceContractHash(shotState)
	if err != nil {
		return CommerceVideoPromptPlanState{}, err
	}
	var shotStateRevision int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(revision), 0) + 1
		FROM storyboard_shot_state_versions
		WHERE storyboard_shot_id = $1 AND state_role = 'planned_entry'
	`, input.Snapshot.StoryboardShotID).Scan(&shotStateRevision); err != nil {
		return CommerceVideoPromptPlanState{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO storyboard_shot_state_versions(
			organization_id, project_id, production_generation_id, storyboard_shot_id,
			state_role, revision, status, state, state_hash, source_type, source_id,
			prompt_version_id, provider_call_id, model_id, created_by, approved_at
		)
		VALUES ($1, $2, $3, $4, 'planned_entry', $5, 'approved', $6, $7,
		        'commerce_shot_contract', $4, NULLIF($8, '')::uuid,
		        NULLIF($9, '')::uuid, NULLIF($10, '')::uuid, NULLIF($11, '')::uuid, now())
	`, input.Snapshot.Identity.OrganizationID, input.Snapshot.Identity.ProjectID,
		input.Snapshot.Identity.ProjectGenerationID, input.Snapshot.StoryboardShotID,
		shotStateRevision, mustJSON(shotState), shotStateHash,
		input.Generation.PromptVersionID, input.Generation.ProviderCallID,
		input.Generation.ProviderModelID, input.WorkflowInput.CreatedBy); err != nil {
		return CommerceVideoPromptPlanState{}, err
	}

	referenceManifest := map[string]any{
		"contractVersion":  "commerce-video-reference-pack/v1",
		"purpose":          "video",
		"profileKey":       input.Snapshot.VideoProfileKey,
		"storyboardShotId": input.Snapshot.StoryboardShotID,
		"items": []map[string]any{{
			"referenceKey": "commerce-first-frame",
			"role":         "first_frame",
			"required":     true,
			"priority":     100,
			"sourceType":   "commerce_shot_image_version",
			"sourceId":     input.Snapshot.FirstFrame.ImageVersionID,
			"artifactId":   input.Snapshot.FirstFrame.ArtifactID,
			"mediaFileId":  input.Snapshot.FirstFrame.MediaFileID,
			"storageKey":   input.Snapshot.FirstFrame.StorageKey,
			"mediaType":    "image",
			"semantics":    "output_start_frame",
			"contentHash":  input.Snapshot.FirstFrame.ContentHash,
		}},
	}
	referenceManifestHash, err := commerceContractHash(referenceManifest)
	if err != nil {
		return CommerceVideoPromptPlanState{}, err
	}
	referencePackID := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		INSERT INTO shot_reference_packs(
			id, organization_id, project_id, production_generation_id, storyboard_shot_id,
			purpose, profile_snapshot_hash, shot_state_hash, capability_snapshot_hash,
			manifest, manifest_hash, status
		)
		VALUES ($1, $2, $3, $4, $5, 'video', $6, $7, $8, $9, $10, 'active')
	`, referencePackID, input.Snapshot.Identity.OrganizationID, input.Snapshot.Identity.ProjectID,
		input.Snapshot.Identity.ProjectGenerationID, input.Snapshot.StoryboardShotID,
		cleanContractHash(input.Snapshot.VideoProfileSnapshotHash), shotStateHash,
		cleanContractHash(input.Snapshot.LanguageCapabilitySnapshotHash),
		mustJSON(referenceManifest), referenceManifestHash); err != nil {
		return CommerceVideoPromptPlanState{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO shot_reference_pack_items(
			reference_pack_id, reference_key, role, required, priority,
			source_type, source_id, artifact_id, media_file_id, storage_key,
			media_type, semantics, content_hash, metadata
		)
		VALUES ($1, 'commerce-first-frame', 'first_frame', true, 100,
		        'commerce_shot_image_version', $2, $3, $4, $5,
		        'image', 'output_start_frame', $6, $7)
	`, referencePackID, input.Snapshot.FirstFrame.ImageVersionID,
		input.Snapshot.FirstFrame.ArtifactID, input.Snapshot.FirstFrame.MediaFileID,
		input.Snapshot.FirstFrame.StorageKey, cleanContractHash(input.Snapshot.FirstFrame.ContentHash),
		mustJSON(map[string]any{
			"sourceVersion":                  input.Snapshot.FirstFrame.ImageVersionID,
			"fidelityStatus":                 "approved",
			"commerceProductReferencePackId": input.Snapshot.ReferencePackID,
		})); err != nil {
		return CommerceVideoPromptPlanState{}, err
	}

	dialogueCues := commerceVideoDialogueCues(input.Snapshot)
	contextValue := map[string]any{
		"contractVersion":        "commerce-prompt-context-plan/v1",
		"identity":               input.Snapshot.Identity,
		"storyboardPlanId":       input.Snapshot.StoryboardPlanID,
		"storyboardEditRevision": input.Snapshot.StoryboardEditRevision,
		"storyboardShotId":       input.Snapshot.StoryboardShotID,
		"fullLocalizedScript":    input.Snapshot.FullLocalizedScript,
		"localizedContentHash":   input.Snapshot.LocalizedContentHash,
		"localizedContractHash":  input.Snapshot.LocalizedContractHash,
		"sourceSegmentIds":       input.Snapshot.SourceSegmentIDs,
		"shotStateHash":          shotStateHash,
		"referencePackHash":      referenceManifestHash,
		"voiceoverText":          input.Snapshot.VoiceoverText,
		"onscreenText":           input.Snapshot.OnscreenText,
		"soundEffects":           input.Snapshot.SoundEffects,
		"musicCue":               input.Snapshot.MusicCue,
		"videoModel":             input.Snapshot.VideoModel,
	}
	contextPlanHash, err := commerceContractHash(contextValue)
	if err != nil {
		return CommerceVideoPromptPlanState{}, err
	}
	var contextRevision int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(revision), 0) + 1
		FROM prompt_context_plans WHERE storyboard_shot_id = $1
	`, input.Snapshot.StoryboardShotID).Scan(&contextRevision); err != nil {
		return CommerceVideoPromptPlanState{}, err
	}
	contextPlanID := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		INSERT INTO prompt_context_plans(
			id, organization_id, project_id, production_generation_id,
			video_production_binding_id, video_production_binding_revision,
			storyboard_plan_id, storyboard_shot_id, script_episode_id, script_scene_id,
			commerce_storyboard_plan_id, commerce_product_id, commerce_script_unit_id,
			commerce_script_unit_generation_id, commerce_localization_id,
			revision, status, episode_continuity_digest, current_scene_script,
			adjacent_scene_summaries, current_shot_state, verbatim_dialogue_cues,
			model_context_limit, model_prompt_limit, budget_allocation,
			source_hashes, plan_hash, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6,
		        NULL, $7, NULL, NULL, $8, $9, $10, $11, $12,
		        $13, 'active', $14, $15, '[]'::jsonb, $16, $17,
		        $18, $19, $20, $21, $22, NULLIF($23, '')::uuid)
	`, contextPlanID, input.Snapshot.Identity.OrganizationID, input.Snapshot.Identity.ProjectID,
		input.Snapshot.Identity.ProjectGenerationID, input.Snapshot.Identity.VideoProductionBindingID,
		input.Snapshot.Identity.VideoProductionBindingRevision, input.Snapshot.StoryboardShotID,
		input.Snapshot.StoryboardPlanID, input.Snapshot.Identity.ProductID, input.Snapshot.Identity.ScriptUnitID,
		input.Snapshot.Identity.UnitGenerationID, input.Snapshot.LocalizationID,
		contextRevision, input.Snapshot.LocalizedContentHash, input.Snapshot.FullLocalizedScript,
		mustJSON(shotState), mustJSON(dialogueCues), input.Snapshot.AgentModelContextLimit,
		input.Snapshot.AgentModelPromptLimit, mustJSON(map[string]any{
			"strategy":                  "full_script_unit",
			"localizedScriptCharacters": len([]rune(input.Snapshot.FullLocalizedScript)),
		}), mustJSON(map[string]any{
			"localizedContentHash":  input.Snapshot.LocalizedContentHash,
			"localizedContractHash": input.Snapshot.LocalizedContractHash,
			"shotContractHash":      input.Snapshot.ShotContractHash,
			"firstFrameContentHash": input.Snapshot.FirstFrame.ContentHash,
		}), contextPlanHash, input.WorkflowInput.CreatedBy); err != nil {
		return CommerceVideoPromptPlanState{}, err
	}

	renderedPrompt := renderCommerceVideoProviderPrompt(input.Contract)
	promptHash := commerceTextHash(renderedPrompt)
	voiceoverHash := commerceTextHash(input.Contract.VoiceoverText)
	reviewerOutput := mustJSON(input.Review)
	metadata := mustJSON(map[string]any{
		"commerceInputHash":        input.Snapshot.InputHash,
		"generationContract":       input.Contract,
		"reviewContract":           input.Review,
		"generationProvenance":     input.Generation,
		"reviewerProvenance":       input.Reviewer,
		"onscreenText":             input.Contract.OnscreenText,
		"soundEffects":             input.Contract.SoundEffects,
		"musicCue":                 input.Contract.MusicCue,
		"firstFrameImageVersionId": input.Snapshot.FirstFrame.ImageVersionID,
		"firstFrameContentHash":    input.Snapshot.FirstFrame.ContentHash,
		"commerceReferencePackId":  input.Snapshot.ReferencePackID,
	})
	var plan CommerceVideoPromptPlanState
	plan.ID = uuid.NewString()
	plan.Status = "approved"
	plan.Prompt = renderedPrompt
	plan.PromptHash = promptHash
	plan.PromptContextPlanID = contextPlanID
	plan.ReferencePackID = referencePackID
	plan.ShotStateHash = shotStateHash
	if err := tx.QueryRow(ctx, `
		INSERT INTO video_prompt_plans(
			id, organization_id, project_id, production_generation_id,
			video_production_binding_id, video_production_binding_revision,
			profile_version_id, storyboard_shot_id, prompt_context_plan_id,
			prompt_version_id, reviewer_prompt_version_id, workflow_run_id,
			provider_call_id, reviewer_provider_call_id, provider_model_id,
			revision, status, rendered_prompt, prompt_hash,
			prompt_context_plan_hash, profile_snapshot_hash, shot_state_hash,
			transition_hash, reference_pack_hash, capability_snapshot_hash,
			input_contract_version, dialogue_cues, native_audio_required,
			audio_strategy, audio_requirement, reviewer_output, metadata,
			created_by, reviewed_at,
			commerce_script_unit_id, commerce_script_unit_generation_id,
			commerce_product_id, product_version_id, localization_id,
			product_reference_pack_id, commerce_workflow_binding_id,
			localized_contract_hash, target_language, verbatim_voiceover_hash,
			timing_policy_version, language_capability_snapshot_hash
		)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9,
		       $10, $11, $12, NULLIF($13, '')::uuid, NULLIF($14, '')::uuid,
		       NULLIF($15, '')::uuid, COALESCE(MAX(revision), 0) + 1,
		       'reviewing', $16, $17, $18, $19, $20, NULL, $21, $22,
		       $23, $24, $25, $26, $27, $28, $29,
		       NULLIF($30, '')::uuid, now(),
		       $31, $32, $33, $34, $35, $36, $37,
		       $38, $39, $40, $41, $42
		FROM video_prompt_plans
		WHERE storyboard_shot_id = $8
		RETURNING revision
	`, plan.ID, input.Snapshot.Identity.OrganizationID, input.Snapshot.Identity.ProjectID,
		input.Snapshot.Identity.ProjectGenerationID, input.Snapshot.Identity.VideoProductionBindingID,
		input.Snapshot.Identity.VideoProductionBindingRevision, input.Snapshot.VideoProfileVersionID,
		input.Snapshot.StoryboardShotID, contextPlanID,
		input.Generation.PromptVersionID, input.Reviewer.PromptVersionID,
		input.WorkflowInput.WorkflowRunID, input.Generation.ProviderCallID,
		input.Reviewer.ProviderCallID, input.Generation.ProviderModelID,
		renderedPrompt, promptHash, contextPlanHash,
		cleanContractHash(input.Snapshot.VideoProfileSnapshotHash), shotStateHash,
		referenceManifestHash, cleanContractHash(input.Snapshot.LanguageCapabilitySnapshotHash),
		input.Snapshot.VideoInputContract, mustJSON(dialogueCues), input.Snapshot.NativeAudioRequired,
		input.Snapshot.AudioStrategy, input.Snapshot.AudioRequirement,
		reviewerOutput, metadata, input.WorkflowInput.CreatedBy,
		input.Snapshot.Identity.ScriptUnitID, input.Snapshot.Identity.UnitGenerationID,
		input.Snapshot.Identity.ProductID, input.Snapshot.ProductVersionID,
		input.Snapshot.LocalizationID, input.Snapshot.ReferencePackID,
		input.Snapshot.Identity.CommerceWorkflowBindingID,
		input.Snapshot.LocalizedContractHash, input.Snapshot.TargetLocale,
		voiceoverHash, input.Snapshot.TimingPolicyVersion,
		cleanContractHash(input.Snapshot.LanguageCapabilitySnapshotHash),
	).Scan(&plan.Revision); err != nil {
		return CommerceVideoPromptPlanState{}, err
	}
	for ordinal, cue := range dialogueCues {
		cueHash, err := commerceContractHash(map[string]any{
			"ordinal": ordinal, "timingUnitId": cue.TimingUnitID,
			"speaker": cue.Speaker, "text": cue.Text, "delivery": cue.Delivery,
			"kind": cue.Kind, "startTick": cue.StartTick, "endTick": cue.EndTick,
		})
		if err != nil {
			return CommerceVideoPromptPlanState{}, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO video_prompt_plan_dialogue_cues(
				video_prompt_plan_id, ordinal, timing_unit_id, speaker, dialogue_text,
				start_tick, end_tick, language, delivery, kind,
				continues_from_previous, continues_to_next, required, content_hash
			)
			VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6, $7, $8,
			        NULLIF($9, ''), $10, $11, $12, true, $13)
		`, plan.ID, ordinal, cue.TimingUnitID, cue.Speaker, cue.Text,
			cue.StartTick, cue.EndTick, input.Snapshot.TargetLocale, cue.Delivery,
			cue.Kind, cue.ContinuesFromPrevious, cue.ContinuesToNext, cueHash); err != nil {
			return CommerceVideoPromptPlanState{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE video_prompt_plans
		SET status = 'approved', approved_at = now()
		WHERE id = $1 AND status = 'reviewing'
	`, plan.ID); err != nil {
		return CommerceVideoPromptPlanState{}, err
	}
	dialogueHash, err := commerceContractHash(dialogueCues)
	if err != nil {
		return CommerceVideoPromptPlanState{}, err
	}
	capabilityRequirements := map[string]any{
		"nativeAudioRequested":           input.Snapshot.NativeAudioRequested,
		"nativeAudioRequired":            input.Snapshot.NativeAudioRequired,
		"dialogueLanguage":               input.Snapshot.TargetLocale,
		"languageCapabilitySnapshotHash": input.Snapshot.LanguageCapabilitySnapshotHash,
		"providerModelId":                input.Snapshot.VideoModel.ProviderModelID,
		"soundEffects":                   input.Snapshot.SoundEffects,
		"musicCue":                       input.Snapshot.MusicCue,
	}
	audioContractHash, err := commerceContractHash(map[string]any{
		"videoPromptPlanId":             plan.ID,
		"audioStrategy":                 input.Snapshot.AudioStrategy,
		"audioRequirement":              input.Snapshot.AudioRequirement,
		"nativeAudioRequired":           input.Snapshot.NativeAudioRequired,
		"dialogueLanguage":              input.Snapshot.TargetLocale,
		"dialogueCuesHash":              dialogueHash,
		"expectedDialogueDurationTicks": commerceExpectedDialogueDuration(dialogueCues),
		"timelineTimebase":              input.Snapshot.TimelineTimebase,
		"capabilityRequirements":        capabilityRequirements,
	})
	if err != nil {
		return CommerceVideoPromptPlanState{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO video_native_audio_contracts(
			organization_id, project_id, production_generation_id,
			storyboard_shot_id, video_prompt_plan_id, revision, status,
			audio_strategy, audio_requirement, native_audio_required,
			dialogue_language, dialogue_cues_hash, expected_dialogue_duration_ticks,
			timeline_timebase, capability_requirements, contract_hash
		)
		SELECT $1, $2, $3, $4, $5, COALESCE(MAX(revision), 0) + 1, 'active',
		       $6, $7, $8, $9, $10, $11, $12, $13, $14
		FROM video_native_audio_contracts WHERE storyboard_shot_id = $4
	`, input.Snapshot.Identity.OrganizationID, input.Snapshot.Identity.ProjectID,
		input.Snapshot.Identity.ProjectGenerationID, input.Snapshot.StoryboardShotID,
		plan.ID, input.Snapshot.AudioStrategy, input.Snapshot.AudioRequirement,
		input.Snapshot.NativeAudioRequired, input.Snapshot.TargetLocale,
		dialogueHash, commerceExpectedDialogueDuration(dialogueCues),
		input.Snapshot.TimelineTimebase, mustJSON(capabilityRequirements), audioContractHash); err != nil {
		return CommerceVideoPromptPlanState{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET video_prompt = $2, video_prompt_status = 'succeeded',
		    video_prompt_error_code = NULL, video_prompt_error_message = NULL,
		    video_prompt_workflow_run_id = $3, video_prompt_updated_at = now(),
		    active_video_render_plan_id = NULL,
		    video_status = CASE WHEN video_artifact_id IS NULL THEN 'not_started' ELSE 'stale' END,
		    stale_state = 'needs_regeneration',
		    metadata = metadata || jsonb_build_object(
		      'commerceVideoPromptPlanId', $4::uuid::text,
		      'commerceVideoPromptPlanRevision', $5::integer,
		      'commerceVideoPromptHash', $6::text,
		      'commerceVideoFirstFrameVersionId', $7::uuid::text,
		      'commerceOnscreenText', $8::text,
		      'commerceSoundEffects', $9::jsonb,
		      'commerceMusicCue', $10::text
		    ), updated_at = now()
		WHERE id = $1 AND commerce_storyboard_plan_id = $11 AND deleted_at IS NULL
	`, input.Snapshot.StoryboardShotID, renderedPrompt, input.WorkflowInput.WorkflowRunID,
		plan.ID, plan.Revision, plan.PromptHash, input.Snapshot.FirstFrame.ImageVersionID,
		input.Contract.OnscreenText, mustJSON(input.Contract.SoundEffects), input.Contract.MusicCue,
		input.Snapshot.StoryboardPlanID); err != nil {
		return CommerceVideoPromptPlanState{}, err
	}
	if _, err := r.runs.CompleteAttempt(ctx, tx, commerce.CompleteProductionAttemptParams{
		OrganizationID: input.WorkflowInput.Identity.OrganizationID,
		ProjectID:      input.WorkflowInput.Identity.ProjectID,
		RunID:          input.WorkflowInput.ProductionRunID,
		ItemID:         input.Attempt.ItemID,
		AttemptID:      input.Attempt.AttemptID,
		Status:         commerce.ItemSucceeded,
		OutputSnapshot: mustJSON(map[string]any{
			"videoPromptPlanId":   plan.ID,
			"revision":            plan.Revision,
			"promptHash":          plan.PromptHash,
			"promptContextPlanId": contextPlanID,
			"referencePackId":     referencePackID,
		}),
		OutputVideoPromptPlanID: plan.ID,
		ProviderRequestID:       input.Generation.ProviderRequestID,
		ProviderCallID:          input.Generation.ProviderCallID,
	}); err != nil {
		return CommerceVideoPromptPlanState{}, err
	}
	if err := insertEvent(ctx, tx, input.Snapshot.Identity.OrganizationID, input.Snapshot.Identity.ProjectID,
		"commerce.shot.video_prompt.approved", "storyboard_shot", input.Snapshot.StoryboardShotID,
		commerceVideoEventPayload(input.WorkflowInput, input.Snapshot.StoryboardShotID, "approved", map[string]any{
			"videoPromptPlanId": plan.ID,
			"promptHash":        plan.PromptHash,
			"reviewRounds":      input.Reviewer.Round,
		})); err != nil {
		return CommerceVideoPromptPlanState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommerceVideoPromptPlanState{}, err
	}
	return plan, nil
}

func (r *CommerceGenerationRuntime) LoadCommerceVideoExecutionShot(
	ctx context.Context,
	input CommerceVideoBatchInput,
	shotID string,
) (CommerceVideoExecutionShot, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceVideoExecutionShot{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := r.lockGenerationState(ctx, tx, input.Identity); err != nil {
		return CommerceVideoExecutionShot{}, err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowRunID, CommercePhaseVideoRender, input); err != nil {
		return CommerceVideoExecutionShot{}, err
	}
	var shot CommerceVideoExecutionShot
	var planEditRevision int
	err = tx.QueryRow(ctx, `
		SELECT shot.id::text, shot.shot_index, shot.shot_no,
		       plan.aspect_ratio,
		       COALESCE(plan.video_execution_envelope->>'targetResolution', ''),
		       COALESCE(contract.recommended_request_duration_seconds::float8, 0),
		       plan.video_execution_envelope_hash,
		       COALESCE(contract.eligible_route_set_hash, ''),
		       prompt.audio_strategy, prompt.audio_requirement,
		       plan.edit_revision
		FROM commerce_storyboard_plans plan
		JOIN storyboard_shots shot
		  ON shot.commerce_storyboard_plan_id = plan.id AND shot.deleted_at IS NULL
		JOIN commerce_shot_contracts contract
		  ON contract.storyboard_shot_id = shot.id
		 AND contract.commerce_storyboard_plan_id = plan.id
		 AND contract.script_unit_id = plan.script_unit_id
		 AND contract.script_unit_generation_id = plan.script_unit_generation_id
		 AND contract.organization_id = plan.organization_id
		 AND contract.project_id = plan.project_id
		JOIN video_prompt_plans prompt
		  ON prompt.storyboard_shot_id = shot.id
		 AND prompt.production_generation_id = plan.project_production_generation_id
		 AND prompt.status = 'approved'
		JOIN storyboard_shot_state_versions state
		  ON state.storyboard_shot_id = shot.id
		 AND state.state_role = 'planned_entry' AND state.status = 'approved'
		JOIN shot_reference_packs reference_pack
		  ON reference_pack.storyboard_shot_id = shot.id
		 AND reference_pack.status = 'active' AND reference_pack.purpose = 'video'
		JOIN prompt_context_plans context_plan
		  ON context_plan.id = prompt.prompt_context_plan_id AND context_plan.status = 'active'
		JOIN video_native_audio_contracts audio
		  ON audio.video_prompt_plan_id = prompt.id AND audio.status = 'active'
		WHERE plan.organization_id = $1 AND plan.project_id = $2
		  AND plan.script_unit_id = $3 AND plan.script_unit_generation_id = $4
		  AND plan.id = $5 AND plan.active AND plan.status = 'ready'
		  AND shot.id = $6
		  AND prompt.commerce_script_unit_id = plan.script_unit_id
		  AND prompt.commerce_script_unit_generation_id = plan.script_unit_generation_id
		  AND prompt.commerce_workflow_binding_id = plan.commerce_workflow_binding_id
		  AND prompt.shot_state_hash = state.state_hash
		  AND prompt.reference_pack_hash = reference_pack.manifest_hash
		  AND prompt.prompt_context_plan_hash = context_plan.plan_hash
		FOR SHARE OF plan, shot, prompt, state, reference_pack, context_plan, audio
	`, input.Identity.OrganizationID, input.Identity.ProjectID, input.Identity.ScriptUnitID,
		input.Identity.UnitGenerationID, input.StoryboardPlanID, shotID).Scan(
		&shot.ShotID, &shot.ShotIndex, &shot.ShotNo, &shot.AspectRatio,
		&shot.Resolution, &shot.RequestedDurationSeconds, &shot.VideoExecutionEnvelopeHash,
		&shot.EligibleRouteSetHash, &shot.AudioStrategy, &shot.AudioRequirement, &planEditRevision,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommerceVideoExecutionShot{}, commerce.Error{
			Code:    CommerceCodeVideoPromptContractInvalid,
			Message: "镜头缺少匹配当前分镜与首帧的已审核视频提示词",
			Cause:   err,
		}
	}
	if err != nil {
		return CommerceVideoExecutionShot{}, err
	}
	if planEditRevision != input.PlanEditRevision {
		return CommerceVideoExecutionShot{}, generationMismatch("分镜已修改，请重新生成视频提示词", nil)
	}
	shot.Resolution = strings.ToLower(strings.TrimSpace(shot.Resolution))
	if shot.Resolution == "" || shot.RequestedDurationSeconds <= 0 ||
		strings.TrimSpace(shot.VideoExecutionEnvelopeHash) == "" ||
		strings.TrimSpace(shot.EligibleRouteSetHash) == "" {
		return CommerceVideoExecutionShot{}, commerce.Error{
			Code:    CommerceCodeVideoPromptContractInvalid,
			Message: "镜头缺少冻结的视频时长或分辨率执行契约，请重新生成分镜方案",
		}
	}
	return shot, nil
}

func (r *CommerceGenerationRuntime) CompleteCommerceShotVideoItem(
	ctx context.Context,
	input CompleteCommerceShotVideoItemInput,
) (CommerceVideoItemOutput, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceVideoItemOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := r.lockGenerationState(ctx, tx, input.WorkflowInput.Identity); err != nil {
		return CommerceVideoItemOutput{}, err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowInput.WorkflowRunID, CommercePhaseVideoRender, input.WorkflowInput); err != nil {
		return CommerceVideoItemOutput{}, err
	}
	if input.Result.Plan.ExecutionPlanID == "" || input.Result.Output.ArtifactID == "" || input.Result.Output.MediaFileID == "" {
		return CommerceVideoItemOutput{}, generationMismatch("镜头视频执行结果缺少 Render Plan 或媒体身份", nil)
	}
	var activePlanID string
	if err := tx.QueryRow(ctx, `
		SELECT active_video_render_plan_id::text
		FROM storyboard_shots
		WHERE id = $1 AND project_id = $2 AND production_generation_id = $3
		  AND commerce_storyboard_plan_id = $4 AND deleted_at IS NULL
		FOR UPDATE
	`, input.Shot.ShotID, input.WorkflowInput.Identity.ProjectID,
		input.WorkflowInput.Identity.ProjectGenerationID, input.WorkflowInput.StoryboardPlanID).Scan(&activePlanID); err != nil {
		return CommerceVideoItemOutput{}, err
	}
	if activePlanID != input.Result.Plan.ExecutionPlanID {
		return CommerceVideoItemOutput{}, generationMismatch("镜头活动 Render Plan 已变化，拒绝提交旧视频结果", nil)
	}
	providerRequestID, providerCallID, providerAsyncTaskID := commerceVideoProviderProvenance(input.Result)
	if _, err := r.runs.CompleteAttempt(ctx, tx, commerce.CompleteProductionAttemptParams{
		OrganizationID: input.WorkflowInput.Identity.OrganizationID,
		ProjectID:      input.WorkflowInput.Identity.ProjectID,
		RunID:          input.WorkflowInput.ProductionRunID,
		ItemID:         input.Attempt.ItemID,
		AttemptID:      input.Attempt.AttemptID,
		Status:         commerce.ItemSucceeded,
		OutputSnapshot: mustJSON(map[string]any{
			"videoRenderPlanId": input.Result.Plan.ExecutionPlanID,
			"artifactId":        input.Result.Output.ArtifactID,
			"mediaFileId":       input.Result.Output.MediaFileID,
			"storageKey":        input.Result.Output.StorageKey,
			"durationSeconds":   input.Result.Output.DurationSeconds,
			"nativeAudioStatus": input.Result.Output.NativeAudioStatus,
		}),
		OutputArtifactID:        input.Result.Output.ArtifactID,
		OutputMediaFileID:       input.Result.Output.MediaFileID,
		OutputVideoRenderPlanID: input.Result.Plan.ExecutionPlanID,
		ProviderRequestID:       providerRequestID,
		ProviderCallID:          providerCallID,
		ProviderAsyncTaskID:     providerAsyncTaskID,
	}); err != nil {
		return CommerceVideoItemOutput{}, err
	}
	if err := insertEvent(ctx, tx, input.WorkflowInput.Identity.OrganizationID, input.WorkflowInput.Identity.ProjectID,
		"commerce.shot.video.succeeded", "storyboard_shot", input.Shot.ShotID,
		commerceVideoEventPayload(input.WorkflowInput, input.Shot.ShotID, "succeeded", map[string]any{
			"videoRenderPlanId": input.Result.Plan.ExecutionPlanID,
			"artifactId":        input.Result.Output.ArtifactID,
			"mediaFileId":       input.Result.Output.MediaFileID,
		})); err != nil {
		return CommerceVideoItemOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommerceVideoItemOutput{}, err
	}
	return CommerceVideoItemOutput{
		ShotID:            input.Shot.ShotID,
		Status:            commerce.ItemSucceeded,
		VideoRenderPlanID: input.Result.Plan.ExecutionPlanID,
		ArtifactID:        input.Result.Output.ArtifactID,
		MediaFileID:       input.Result.Output.MediaFileID,
		StorageKey:        input.Result.Output.StorageKey,
	}, nil
}

func commerceVideoProviderProvenance(result ShotRenderExecutionResult) (string, string, string) {
	if strings.TrimSpace(result.LastSegment.ProviderCallID) != "" ||
		strings.TrimSpace(result.LastSegment.ProviderAsyncTaskID) != "" {
		return strings.TrimSpace(result.LastSegment.ProviderRequestID),
			strings.TrimSpace(result.LastSegment.ProviderCallID),
			strings.TrimSpace(result.LastSegment.ProviderAsyncTaskID)
	}
	for index := len(result.Polls) - 1; index >= 0; index-- {
		poll := result.Polls[index]
		if strings.TrimSpace(poll.ProviderCallID) == "" &&
			strings.TrimSpace(poll.ProviderAsyncTaskID) == "" {
			continue
		}
		return strings.TrimSpace(poll.ProviderRequestID),
			strings.TrimSpace(poll.ProviderCallID),
			strings.TrimSpace(poll.ProviderAsyncTaskID)
	}
	return "", "", ""
}

func (r *CommerceGenerationRuntime) FailCommerceVideoItem(
	ctx context.Context,
	input FailCommerceVideoItemInput,
) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := r.lockGenerationState(ctx, tx, input.WorkflowInput.Identity); err != nil {
		return err
	}
	phase, err := commerceVideoPhase(input.WorkflowInput.Operation)
	if err != nil {
		return err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowInput.WorkflowRunID, phase, input.WorkflowInput); err != nil {
		return err
	}
	if input.WorkflowInput.Operation == "generate_prompts" {
		if _, err := tx.Exec(ctx, `
			UPDATE storyboard_shots shot
			SET video_prompt_status = CASE WHEN EXISTS (
			      SELECT 1 FROM video_prompt_plans plan
			      WHERE plan.storyboard_shot_id = shot.id AND plan.status = 'approved'
			    ) THEN 'succeeded' ELSE 'failed' END,
			    video_prompt_error_code = $2, video_prompt_error_message = $3,
			    video_prompt_updated_at = now(), updated_at = now()
			WHERE shot.id = $1 AND shot.commerce_storyboard_plan_id IS NOT NULL
		`, input.ShotID, input.ErrorCode, input.ErrorMessage); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET video_status = CASE WHEN video_artifact_id IS NULL THEN 'failed' ELSE video_status END,
			    video_error_code = $2, video_error_message = $3,
			    video_completed_at = now(), updated_at = now()
			WHERE id = $1 AND commerce_storyboard_plan_id IS NOT NULL
		`, input.ShotID, input.ErrorCode, input.ErrorMessage); err != nil {
			return err
		}
	}
	status := commerce.ItemFailedTerminal
	if input.Retryable {
		status = commerce.ItemFailedRetryable
	}
	if _, err := r.runs.CompleteAttempt(ctx, tx, commerce.CompleteProductionAttemptParams{
		OrganizationID: input.WorkflowInput.Identity.OrganizationID,
		ProjectID:      input.WorkflowInput.Identity.ProjectID,
		RunID:          input.WorkflowInput.ProductionRunID,
		ItemID:         input.Attempt.ItemID,
		AttemptID:      input.Attempt.AttemptID,
		Status:         status,
		OutputSnapshot: mustJSON(map[string]any{"shotId": input.ShotID}),
		ErrorCode:      input.ErrorCode,
		ErrorMessage:   input.ErrorMessage,
		Retryable:      input.Retryable,
	}); err != nil {
		return err
	}
	eventType := "commerce.shot.video.failed"
	if input.WorkflowInput.Operation == "generate_prompts" {
		eventType = "commerce.shot.video_prompt.failed"
	}
	if err := insertEvent(ctx, tx, input.WorkflowInput.Identity.OrganizationID, input.WorkflowInput.Identity.ProjectID,
		eventType, "storyboard_shot", input.ShotID,
		commerceVideoEventPayload(input.WorkflowInput, input.ShotID, string(status), map[string]any{
			"errorCode":    input.ErrorCode,
			"errorMessage": input.ErrorMessage,
			"retryable":    input.Retryable,
		})); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *CommerceGenerationRuntime) FinalizeCommerceVideoBatch(
	ctx context.Context,
	input CommerceVideoBatchInput,
	output CommerceVideoBatchOutput,
) (CommerceVideoBatchOutput, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return output, err
	}
	defer tx.Rollback(ctx)
	if _, err := r.lockGenerationState(ctx, tx, input.Identity); err != nil {
		return output, err
	}
	phase, err := commerceVideoPhase(input.Operation)
	if err != nil {
		return output, err
	}
	workflowRun, err := loadCommerceWorkflowRunRecord(ctx, tx, input.WorkflowRunID)
	if err != nil {
		return output, err
	}
	if err := validateCommerceWorkflowRunRecord(workflowRun, input.WorkflowRunID, phase, input); err != nil {
		return output, err
	}
	run, err := r.repository.ReconcileProductionRun(ctx, tx, input.Identity.OrganizationID, input.Identity.ProjectID, input.ProductionRunID)
	if err != nil {
		return output, err
	}
	output.Status = run.Status
	output.Total = run.TotalItems
	output.Succeeded = run.CompletedItems
	output.Failed = run.FailedItems
	finalized, err := finalizeCommerceVideoWorkflowTx(ctx, tx, input, run, output)
	if err != nil {
		return output, err
	}
	if finalized {
		if err := insertEvent(ctx, tx, input.Identity.OrganizationID, input.Identity.ProjectID,
			commerceVideoRunEventType(input.Operation, run.Status), "commerce_production_run", input.ProductionRunID,
			commerceVideoEventPayload(input, "", string(run.Status), map[string]any{
				"totalItems":     run.TotalItems,
				"completedItems": run.CompletedItems,
				"failedItems":    run.FailedItems,
				"cancelledItems": run.CancelledItems,
			})); err != nil {
			return output, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return output, err
	}
	return output, nil
}

func (r *CommerceGenerationRuntime) FinalizeCommerceVideoFailure(
	ctx context.Context,
	input FinalizeCommerceVideoFailureInput,
) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	phase, err := commerceVideoPhase(input.WorkflowInput.Operation)
	if err != nil {
		return err
	}
	workflowRun, err := loadCommerceWorkflowRunRecord(ctx, tx, input.WorkflowInput.WorkflowRunID)
	if err != nil {
		return err
	}
	if err := validateCommerceWorkflowRunRecord(workflowRun, input.WorkflowInput.WorkflowRunID, phase, input.WorkflowInput); err != nil {
		return err
	}
	code := strings.TrimSpace(input.ErrorCode)
	if code == "" {
		code = codeActivityFailed
	}
	message := strings.TrimSpace(input.ErrorMessage)
	if message == "" {
		message = "视频生产批次执行失败"
	}
	var run commerce.ProductionRun
	if input.Cancelled {
		run, err = r.repository.CancelProductionRun(ctx, tx,
			input.WorkflowInput.Identity.OrganizationID, input.WorkflowInput.Identity.ProjectID,
			input.WorkflowInput.ProductionRunID, message)
	} else {
		run, err = r.repository.FailActiveProductionRunItems(ctx, tx,
			input.WorkflowInput.Identity.OrganizationID, input.WorkflowInput.Identity.ProjectID,
			input.WorkflowInput.ProductionRunID, code, message, true)
	}
	if err != nil {
		return err
	}
	input.Output.Status = run.Status
	input.Output.Total = run.TotalItems
	input.Output.Succeeded = run.CompletedItems
	input.Output.Failed = run.FailedItems
	finalized, err := finalizeCommerceVideoWorkflowTx(ctx, tx, input.WorkflowInput, run, input.Output)
	if err != nil {
		return err
	}
	if finalized {
		if err := insertEvent(ctx, tx, input.WorkflowInput.Identity.OrganizationID, input.WorkflowInput.Identity.ProjectID,
			commerceVideoRunEventType(input.WorkflowInput.Operation, run.Status), "commerce_production_run", input.WorkflowInput.ProductionRunID,
			commerceVideoEventPayload(input.WorkflowInput, "", string(run.Status), map[string]any{
				"totalItems":     run.TotalItems,
				"completedItems": run.CompletedItems,
				"failedItems":    run.FailedItems,
				"cancelledItems": run.CancelledItems,
				"errorCode":      code,
				"errorMessage":   message,
			})); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *CommerceGenerationRuntime) assertCurrentCommerceVideoSnapshotTx(
	ctx context.Context,
	tx pgx.Tx,
	snapshot CommerceVideoPromptShotSnapshot,
) error {
	var planRevision, editRevision int
	var planID, localizedContractHash, timingPolicyVersion, shotContractHash string
	var imageVersionID, mediaFileID, contentHash string
	if err := tx.QueryRow(ctx, `
		SELECT plan.id::text, plan.revision, plan.edit_revision,
		       plan.localized_contract_hash, plan.timing_policy_version,
		       contract.contract_hash,
		       image.id::text, image.media_file_id::text,
		       COALESCE(artifact.content_hash, media.checksum, image.input_hash)
		FROM commerce_storyboard_plans plan
		JOIN storyboard_shots shot ON shot.commerce_storyboard_plan_id = plan.id AND shot.deleted_at IS NULL
		JOIN commerce_shot_contracts contract ON contract.storyboard_shot_id = shot.id
		JOIN commerce_shot_image_versions image
		  ON image.id = shot.active_commerce_image_version_id
		 AND image.active AND image.status = 'succeeded' AND image.fidelity_status = 'approved'
		JOIN artifacts artifact ON artifact.id = image.artifact_id
		JOIN media_files media ON media.id = image.media_file_id
		WHERE plan.organization_id = $1 AND plan.project_id = $2
		  AND plan.script_unit_id = $3 AND plan.script_unit_generation_id = $4
		  AND plan.id = $5 AND plan.active AND plan.status = 'ready'
		  AND shot.id = $6
		FOR UPDATE OF plan, shot, contract, image
	`, snapshot.Identity.OrganizationID, snapshot.Identity.ProjectID,
		snapshot.Identity.ScriptUnitID, snapshot.Identity.UnitGenerationID,
		snapshot.StoryboardPlanID, snapshot.StoryboardShotID).Scan(
		&planID, &planRevision, &editRevision, &localizedContractHash,
		&timingPolicyVersion, &shotContractHash, &imageVersionID, &mediaFileID, &contentHash,
	); err != nil {
		return generationMismatch("当前分镜、镜头契约或首帧参考图已变化", err)
	}
	if planID != snapshot.StoryboardPlanID || planRevision != snapshot.StoryboardPlanRevision ||
		editRevision != snapshot.StoryboardEditRevision || localizedContractHash != snapshot.LocalizedContractHash ||
		timingPolicyVersion != snapshot.TimingPolicyVersion || shotContractHash != snapshot.ShotContractHash ||
		imageVersionID != snapshot.FirstFrame.ImageVersionID || mediaFileID != snapshot.FirstFrame.MediaFileID ||
		cleanContractHash(contentHash) != cleanContractHash(snapshot.FirstFrame.ContentHash) {
		return generationMismatch("当前分镜或视频首帧已变化，请重新提交视频提示词任务", nil)
	}
	return nil
}

func staleCommerceVideoContractsTx(ctx context.Context, tx pgx.Tx, shotID, generationID string) error {
	statements := []string{
		`UPDATE video_render_plans
		 SET active = false,
		     status = CASE WHEN status IN ('planned', 'running') THEN 'stale' ELSE status END,
		     updated_at = now(), metadata = metadata || jsonb_build_object('supersededAt', now())
		 WHERE storyboard_shot_id = $1 AND production_generation_id = $2 AND active`,
		`UPDATE video_native_audio_contracts
		 SET status = 'stale', stale_at = now()
		 WHERE storyboard_shot_id = $1 AND production_generation_id = $2 AND status = 'active'`,
		`UPDATE video_prompt_plans
		 SET status = 'stale', stale_at = now()
		 WHERE storyboard_shot_id = $1 AND production_generation_id = $2 AND status = 'approved'`,
		`UPDATE prompt_context_plans
		 SET status = 'stale', stale_at = now()
		 WHERE storyboard_shot_id = $1 AND production_generation_id = $2 AND status = 'active'`,
		`UPDATE shot_reference_packs
		 SET status = 'stale'
		 WHERE storyboard_shot_id = $1 AND production_generation_id = $2
		   AND purpose = 'video' AND status = 'active'`,
		`UPDATE storyboard_shot_state_versions
		 SET status = 'stale'
		 WHERE storyboard_shot_id = $1 AND production_generation_id = $2
		   AND state_role = 'planned_entry' AND status = 'approved'`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement, shotID, generationID); err != nil {
			return err
		}
	}
	return nil
}

func commerceVideoDialogueCues(snapshot CommerceVideoPromptShotSnapshot) []provider.GatewayVideoDialogueSpan {
	text := strings.TrimSpace(snapshot.VoiceoverText)
	if text == "" {
		return []provider.GatewayVideoDialogueSpan{}
	}
	return []provider.GatewayVideoDialogueSpan{{
		TimingUnitID: "commerce-shot-" + snapshot.StoryboardShotID,
		Speaker:      "旁白",
		Text:         text,
		Delivery:     "广告旁白",
		Kind:         "voiceover",
		StartTick:    0,
		EndTick:      snapshot.DurationTicks,
	}}
}

func commerceExpectedDialogueDuration(cues []provider.GatewayVideoDialogueSpan) int64 {
	var result int64
	for _, cue := range cues {
		if cue.EndTick > result {
			result = cue.EndTick
		}
	}
	return result
}

func renderCommerceVideoProviderPrompt(contract CommerceVideoPromptPlanContract) string {
	parts := []string{strings.TrimSpace(contract.VisualPrompt)}
	if len(contract.SoundEffects) > 0 {
		raw, _ := json.Marshal(contract.SoundEffects)
		parts = append(parts, "Non-speech sound design metadata follows. Render it only as environmental or Foley audio. Never vocalize, narrate, quote, translate, subtitle, or lip-sync these values:\n"+string(raw))
	}
	if music := strings.TrimSpace(contract.MusicCue); music != "" {
		parts = append(parts, "Instrumental music direction follows. Treat it only as background music and never as spoken language:\n"+music)
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func finalizeCommerceVideoWorkflowTx(
	ctx context.Context,
	tx pgx.Tx,
	input CommerceVideoBatchInput,
	run commerce.ProductionRun,
	output CommerceVideoBatchOutput,
) (bool, error) {
	targetStatus := "succeeded"
	errorCode := ""
	errorMessage := ""
	switch run.Status {
	case commerce.RunSucceeded:
	case commerce.RunPartiallySucceeded:
		targetStatus = "partial_succeeded"
		errorCode = "PARTIAL_FAILURE"
		errorMessage = "部分镜头未成功完成"
	case commerce.RunFailed:
		targetStatus = "failed"
		errorCode = "ALL_ITEMS_FAILED"
		errorMessage = "所有镜头均执行失败"
	case commerce.RunCancelled:
		targetStatus = "cancelled"
		errorCode = "WORKFLOW_CANCELLED"
		errorMessage = "用户取消视频生产批次"
	default:
		return false, generationMismatch("视频生产批次尚未进入可提交终态", nil)
	}
	raw, err := json.Marshal(output)
	if err != nil {
		return false, err
	}
	var currentStatus string
	var currentOutput json.RawMessage
	if err := tx.QueryRow(ctx, `
		SELECT status, output FROM workflow_runs WHERE id = $1 FOR UPDATE
	`, input.WorkflowRunID).Scan(&currentStatus, &currentOutput); err != nil {
		return false, err
	}
	if currentStatus == targetStatus {
		if targetStatus == "cancelled" {
			return false, nil
		}
		if err := assertCommerceSnapshotEqual(currentOutput, raw, "视频生产 Workflow 终态输出"); err != nil {
			return false, err
		}
		return false, nil
	}
	if currentStatus != "queued" && currentStatus != "running" && currentStatus != "waiting_review" && currentStatus != "cancelling" {
		return false, generationMismatch("视频生产 Workflow 已不再可提交", nil)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET status = $2, output = $3, error_code = NULLIF($4, ''),
		    error_message = NULLIF($5, ''), completed_at = now(),
		    updated_at = now(), revision = revision + 1
		WHERE id = $1 AND status IN ('queued', 'running', 'waiting_review', 'cancelling')
	`, input.WorkflowRunID, targetStatus, raw, errorCode, errorMessage)
	if err != nil || tag.RowsAffected() != 1 {
		return false, generationMismatch("视频生产 Workflow 已不再可提交", err)
	}
	return true, nil
}

func commerceVideoRunEventType(operation string, status commerce.ProductionRunStatus) string {
	if status == commerce.RunPartiallySucceeded {
		return "commerce.production.run.partially_succeeded"
	}
	if status == commerce.RunFailed {
		return "commerce.production.run.failed"
	}
	if status == commerce.RunCancelled {
		return "commerce.production.run.cancelled"
	}
	if operation == "generate_prompts" {
		return "commerce.production.video_prompt.completed"
	}
	return "commerce.production.video.completed"
}

func commerceVideoEventPayload(
	input CommerceVideoBatchInput,
	shotID string,
	status string,
	extra map[string]any,
) json.RawMessage {
	payload := map[string]any{
		"workflowRunId":            input.WorkflowRunID,
		"commerceProductionRunId":  input.ProductionRunID,
		"commerceScriptUnitId":     input.Identity.ScriptUnitID,
		"scriptUnitGenerationId":   input.Identity.UnitGenerationID,
		"commerceStoryboardPlanId": input.StoryboardPlanID,
		"operation":                input.Operation,
		"status":                   status,
	}
	if shotID != "" {
		payload["storyboardShotId"] = shotID
	}
	for key, value := range extra {
		if value != nil {
			payload[key] = value
		}
	}
	return mustJSON(payload)
}
