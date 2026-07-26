package workflows

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Einzieg/cineweave/internal/commerce"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *CommerceGenerationRuntime) BeginCommerceReferenceImageItem(
	ctx context.Context,
	input CommerceReferenceImageBatchInput,
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
	phase, err := commerceReferenceImagePhase(input.Operation)
	if err != nil {
		return CommerceReferenceImageItemAttempt{}, err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowRunID, phase, input); err != nil {
		return CommerceReferenceImageItemAttempt{}, err
	}
	expectedHash, err := CommerceReferenceImageSubjectHash(input, shotID)
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
			return CommerceReferenceImageItemAttempt{}, generationMismatch("参考图生产项与冻结批次不匹配", err)
		}
		return CommerceReferenceImageItemAttempt{}, err
	}
	attempt, err := r.runs.StartAttempt(ctx, tx, commerce.StartProductionAttemptParams{
		OrganizationID: input.Identity.OrganizationID, ProjectID: input.Identity.ProjectID,
		RunID: input.ProductionRunID, ItemID: itemID, InputHash: expectedHash,
		WorkflowRunID: input.WorkflowRunID,
	})
	if err != nil {
		return CommerceReferenceImageItemAttempt{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommerceReferenceImageItemAttempt{}, err
	}
	return CommerceReferenceImageItemAttempt{
		ItemID: attempt.ItemID, AttemptID: attempt.ID,
		AttemptNumber: attempt.AttemptNumber, InputHash: attempt.InputHash,
	}, nil
}

func (r *CommerceGenerationRuntime) CommitCommerceImagePromptPlan(
	ctx context.Context,
	input CommitCommerceImagePromptPlanInput,
) (CommerceImagePromptPlanState, error) {
	if err := ValidateCommerceImagePromptPlan(input.Contract, input.Snapshot); err != nil {
		return CommerceImagePromptPlanState{}, commerce.Error{Code: CommerceCodeImagePromptContractInvalid, Message: "图片提示词契约无效", Cause: err}
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceImagePromptPlanState{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := r.lockGenerationState(ctx, tx, input.WorkflowInput.Identity); err != nil {
		return CommerceImagePromptPlanState{}, err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowInput.WorkflowRunID, CommercePhaseImagePrompt, input.WorkflowInput); err != nil {
		return CommerceImagePromptPlanState{}, err
	}
	if err := r.assertCurrentReferenceImageSnapshotTx(ctx, tx, input.Snapshot); err != nil {
		return CommerceImagePromptPlanState{}, err
	}
	selected, err := selectCommerceImagePromptReferences(input.Snapshot.References, input.Contract.ReferenceIDs)
	if err != nil {
		return CommerceImagePromptPlanState{}, err
	}
	referenceRaw, err := json.Marshal(selected)
	if err != nil {
		return CommerceImagePromptPlanState{}, err
	}
	referenceHash, err := commerceContractHash(selected)
	if err != nil {
		return CommerceImagePromptPlanState{}, err
	}
	promptHash := commerceTextHash(input.Contract.VisualPrompt)
	if !input.WorkflowInput.Force {
		if existing, found, err := loadActiveCommerceImagePromptPlanTx(ctx, tx, input.Snapshot.StoryboardShotID); err != nil {
			return CommerceImagePromptPlanState{}, err
		} else if found && existing.InputHash == input.Snapshot.InputHash && existing.PromptHash == promptHash && existing.ReferenceHash == referenceHash {
			if _, err := r.runs.CompleteAttempt(ctx, tx, commerce.CompleteProductionAttemptParams{
				OrganizationID: input.WorkflowInput.Identity.OrganizationID,
				ProjectID:      input.WorkflowInput.Identity.ProjectID,
				RunID:          input.WorkflowInput.ProductionRunID, ItemID: input.Attempt.ItemID,
				AttemptID: input.Attempt.AttemptID, Status: commerce.ItemSucceeded,
				OutputSnapshot: mustJSON(map[string]any{"imagePromptPlanId": existing.ID, "reused": true}),
			}); err != nil {
				return CommerceImagePromptPlanState{}, err
			}
			if err := tx.Commit(ctx); err != nil {
				return CommerceImagePromptPlanState{}, err
			}
			return existing, nil
		}
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "commerce-image-prompt:"+input.Snapshot.StoryboardShotID); err != nil {
		return CommerceImagePromptPlanState{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_image_prompt_plans
		SET active = false, status = CASE WHEN status = 'approved' THEN 'stale' ELSE status END,
		    superseded_at = COALESCE(superseded_at, now())
		WHERE storyboard_shot_id = $1 AND active
	`, input.Snapshot.StoryboardShotID); err != nil {
		return CommerceImagePromptPlanState{}, err
	}
	var plan CommerceImagePromptPlanState
	plan.ID = uuid.NewString()
	plan.Status = "approved"
	plan.Prompt = strings.TrimSpace(input.Contract.VisualPrompt)
	plan.NegativePrompt = strings.TrimSpace(input.Contract.NegativePrompt)
	plan.PromptHash = promptHash
	plan.InputHash = input.Snapshot.InputHash
	plan.ReferenceHash = referenceHash
	plan.References = selected
	plan.ImageProviderModelID = input.Snapshot.ImageModel.ProviderModelID
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_image_prompt_plans(
			id, organization_id, project_id, storyboard_shot_id,
			commerce_storyboard_plan_id, script_unit_id, script_unit_generation_id,
			product_id, product_version_id, localization_id, product_reference_pack_id,
			commerce_workflow_binding_id, revision, status, active,
			prompt, negative_prompt, reference_snapshot, reference_snapshot_hash,
			shot_contract_hash, input_hash, prompt_hash,
			generation_prompt_version_id, generation_provider_call_id,
			generation_provider_model_id, image_provider_model_id,
			review_round, reviewer_output, rejection_reasons, created_by, approved_at
		)
		SELECT $1, $2, $3, $4, $5, $6, $7,
		       $8, $9, $10, $11, $12,
		       COALESCE(max(revision), 0) + 1, 'approved', true,
		       $13, $14, $15, $16, $17, $18, $19,
		       $20, NULLIF($21, '')::uuid, NULLIF($22, '')::uuid,
		       NULLIF($23, '')::uuid, 1,
		       jsonb_build_object('decision', 'approved', 'validation', 'deterministic_contract'),
		       '[]'::jsonb, NULLIF($24, '')::uuid, now()
		FROM commerce_image_prompt_plans
		WHERE storyboard_shot_id = $4
		RETURNING revision
	`, plan.ID, input.Snapshot.Identity.OrganizationID, input.Snapshot.Identity.ProjectID,
		input.Snapshot.StoryboardShotID, input.Snapshot.StoryboardPlanID,
		input.Snapshot.Identity.ScriptUnitID, input.Snapshot.Identity.UnitGenerationID,
		input.Snapshot.Identity.ProductID, input.Snapshot.ProductVersionID,
		input.Snapshot.LocalizationID, input.Snapshot.ReferencePackID,
		input.Snapshot.Identity.CommerceWorkflowBindingID,
		plan.Prompt, plan.NegativePrompt, referenceRaw, plan.ReferenceHash,
		input.Snapshot.ShotContractHash, plan.InputHash, plan.PromptHash,
		input.Provenance.PromptVersionID, input.Provenance.ProviderCallID,
		input.Provenance.ProviderModelID, plan.ImageProviderModelID,
		input.WorkflowInput.CreatedBy,
	).Scan(&plan.Revision); err != nil {
		return CommerceImagePromptPlanState{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET active_commerce_image_prompt_plan_id = $2::uuid,
		    image_prompt = $3, image_prompt_status = 'succeeded',
		    image_prompt_error_code = NULL, image_prompt_error_message = NULL,
		    image_prompt_workflow_run_id = $4, image_prompt_updated_at = now(),
		    metadata = metadata || jsonb_build_object(
		      'commerceImagePromptPlanId', $2::uuid::text,
		      'commerceImagePromptPlanRevision', $5::integer,
		      'commerceImagePromptHash', $6::text,
		      'commerceImagePromptReferenceHash', $7::text
		    ), updated_at = now()
		WHERE id = $1 AND commerce_storyboard_plan_id = $8 AND deleted_at IS NULL
	`, input.Snapshot.StoryboardShotID, plan.ID, plan.Prompt,
		input.WorkflowInput.WorkflowRunID, plan.Revision, plan.PromptHash,
		plan.ReferenceHash, input.Snapshot.StoryboardPlanID); err != nil {
		return CommerceImagePromptPlanState{}, err
	}
	if _, err := r.runs.CompleteAttempt(ctx, tx, commerce.CompleteProductionAttemptParams{
		OrganizationID: input.WorkflowInput.Identity.OrganizationID,
		ProjectID:      input.WorkflowInput.Identity.ProjectID,
		RunID:          input.WorkflowInput.ProductionRunID, ItemID: input.Attempt.ItemID,
		AttemptID: input.Attempt.AttemptID, Status: commerce.ItemSucceeded,
		OutputSnapshot:    mustJSON(map[string]any{"imagePromptPlanId": plan.ID, "revision": plan.Revision, "promptHash": plan.PromptHash}),
		ProviderRequestID: input.Provenance.ProviderRequestID,
		ProviderCallID:    input.Provenance.ProviderCallID,
	}); err != nil {
		return CommerceImagePromptPlanState{}, err
	}
	if err := insertEvent(ctx, tx, input.Snapshot.Identity.OrganizationID, input.Snapshot.Identity.ProjectID,
		"commerce.shot.image_prompt.succeeded", "storyboard_shot", input.Snapshot.StoryboardShotID,
		commerceReferenceImageEventPayload(input.WorkflowInput, input.Snapshot.StoryboardShotID, "succeeded", map[string]any{
			"imagePromptPlanId": plan.ID,
			"promptHash":        plan.PromptHash,
		})); err != nil {
		return CommerceImagePromptPlanState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommerceImagePromptPlanState{}, err
	}
	return plan, nil
}

func (r *CommerceGenerationRuntime) LoadApprovedCommerceImagePromptPlan(
	ctx context.Context,
	input CommerceReferenceImageBatchInput,
	snapshot CommerceReferenceImageShotSnapshot,
) (CommerceImagePromptPlanState, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceImagePromptPlanState{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := r.lockGenerationState(ctx, tx, input.Identity); err != nil {
		return CommerceImagePromptPlanState{}, err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowRunID, CommercePhaseImageFidelity, input); err != nil {
		return CommerceImagePromptPlanState{}, err
	}
	if err := r.assertCurrentReferenceImageSnapshotTx(ctx, tx, snapshot); err != nil {
		return CommerceImagePromptPlanState{}, err
	}
	plan, found, err := loadActiveCommerceImagePromptPlanTx(ctx, tx, snapshot.StoryboardShotID)
	if err != nil {
		return CommerceImagePromptPlanState{}, err
	}
	if !found || plan.Status != "approved" || plan.InputHash != snapshot.InputHash {
		return CommerceImagePromptPlanState{}, commerce.Error{Code: CommerceCodeImagePromptContractInvalid, Message: "镜头缺少匹配当前分镜与商品引用的已审核图片提示词"}
	}
	return plan, nil
}

func (r *CommerceGenerationRuntime) BeginCommerceShotImageVersion(
	ctx context.Context,
	input BeginCommerceShotImageVersionInput,
) (CommerceShotImageVersionState, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceShotImageVersionState{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := r.lockGenerationState(ctx, tx, input.WorkflowInput.Identity); err != nil {
		return CommerceShotImageVersionState{}, err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowInput.WorkflowRunID, CommercePhaseImageFidelity, input.WorkflowInput); err != nil {
		return CommerceShotImageVersionState{}, err
	}
	if err := r.assertCurrentReferenceImageSnapshotTx(ctx, tx, input.Snapshot); err != nil {
		return CommerceShotImageVersionState{}, err
	}
	if commerceReferenceImageMayReuseGeneratedMedia(input.WorkflowInput, input.Snapshot.StoryboardShotID) {
		if current, found, err := loadActiveCommerceShotImageVersionTx(ctx, tx, input.Snapshot.StoryboardShotID, input.PromptPlan.ID); err != nil {
			return CommerceShotImageVersionState{}, err
		} else if found && current.Status == "succeeded" {
			if _, err := r.runs.CompleteAttempt(ctx, tx, commerce.CompleteProductionAttemptParams{
				OrganizationID: input.WorkflowInput.Identity.OrganizationID,
				ProjectID:      input.WorkflowInput.Identity.ProjectID,
				RunID:          input.WorkflowInput.ProductionRunID, ItemID: input.Attempt.ItemID,
				AttemptID: input.Attempt.AttemptID, Status: commerce.ItemSucceeded,
				OutputSnapshot:   mustJSON(map[string]any{"imageVersionId": current.ID, "reused": true}),
				OutputArtifactID: current.ArtifactID, OutputMediaFileID: current.MediaFileID,
				ProviderCallID: current.ProviderCallID,
			}); err != nil {
				return CommerceShotImageVersionState{}, err
			}
			if err := tx.Commit(ctx); err != nil {
				return CommerceShotImageVersionState{}, err
			}
			return current, nil
		}
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "commerce-shot-image:"+input.Snapshot.StoryboardShotID); err != nil {
		return CommerceShotImageVersionState{}, err
	}
	version := CommerceShotImageVersionState{ID: uuid.NewString(), Status: "running", FidelityStatus: "pending"}
	if commerceReferenceImageMayReuseGeneratedMedia(input.WorkflowInput, input.Snapshot.StoryboardShotID) {
		reusable, found, err := loadReusableCommerceShotImageVersionTx(
			ctx, tx, input.Snapshot.StoryboardShotID, input.PromptPlan.ID,
			input.Snapshot.InputHash, input.PromptPlan.ReferenceHash,
		)
		if err != nil {
			return CommerceShotImageVersionState{}, err
		}
		if found {
			version.ArtifactID = reusable.ArtifactID
			version.MediaFileID = reusable.MediaFileID
			version.StorageKey = reusable.StorageKey
			version.ProviderRequestID = reusable.ProviderRequestID
			version.ProviderCallID = reusable.ProviderCallID
			version.ProviderModelID = reusable.ProviderModelID
			version.ReusedFromVersionID = reusable.ID
		}
	}
	versionMetadata := mustJSON(map[string]any{})
	if version.ReusedFromVersionID != "" {
		versionMetadata = mustJSON(map[string]any{
			"reusedGeneratedMedia":     true,
			"reusedFromImageVersionId": version.ReusedFromVersionID,
		})
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_shot_image_versions(
			id, organization_id, project_id, storyboard_shot_id,
			script_unit_id, script_unit_generation_id, image_prompt_plan_id,
			revision, status, active, input_hash, reference_snapshot_hash,
			provider_request_id, provider_call_id, provider_model_id,
			artifact_id, media_file_id, storage_key, fidelity_status, metadata,
			created_by, started_at
		)
		SELECT $1, $2, $3, $4, $5, $6, $7,
		       COALESCE(max(revision), 0) + 1, 'running', false, $8, $9,
		       NULLIF($10, '')::uuid, NULLIF($11, '')::uuid, NULLIF($12, '')::uuid,
		       NULLIF($13, '')::uuid, NULLIF($14, '')::uuid, NULLIF($15, ''),
		       'pending', $16::jsonb, NULLIF($17, '')::uuid, now()
		FROM commerce_shot_image_versions
		WHERE storyboard_shot_id = $4
		RETURNING revision
	`, version.ID, input.Snapshot.Identity.OrganizationID, input.Snapshot.Identity.ProjectID,
		input.Snapshot.StoryboardShotID, input.Snapshot.Identity.ScriptUnitID,
		input.Snapshot.Identity.UnitGenerationID, input.PromptPlan.ID,
		input.Snapshot.InputHash, input.PromptPlan.ReferenceHash,
		version.ProviderRequestID, version.ProviderCallID, version.ProviderModelID,
		version.ArtifactID, version.MediaFileID, version.StorageKey, versionMetadata,
		input.WorkflowInput.CreatedBy).Scan(&version.Revision); err != nil {
		return CommerceShotImageVersionState{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET image_status = 'running', image_error_code = NULL, image_error_message = NULL,
		    image_started_at = now(), image_completed_at = NULL,
		    image_workflow_run_id = $2, updated_at = now()
		WHERE id = $1 AND active_commerce_image_prompt_plan_id = $3 AND deleted_at IS NULL
	`, input.Snapshot.StoryboardShotID, input.WorkflowInput.WorkflowRunID, input.PromptPlan.ID); err != nil {
		return CommerceShotImageVersionState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommerceShotImageVersionState{}, err
	}
	return version, nil
}

func (r *CommerceGenerationRuntime) RecordCommerceShotImageGenerated(
	ctx context.Context,
	input RecordCommerceShotImageGeneratedInput,
) (CommerceShotImageVersionState, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceShotImageVersionState{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := r.lockGenerationState(ctx, tx, input.WorkflowInput.Identity); err != nil {
		return CommerceShotImageVersionState{}, err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowInput.WorkflowRunID, CommercePhaseImageFidelity, input.WorkflowInput); err != nil {
		return CommerceShotImageVersionState{}, err
	}
	if input.Gateway.Status != "succeeded" || strings.TrimSpace(input.Gateway.Output.ArtifactID) == "" || strings.TrimSpace(input.Gateway.Output.MediaFileID) == "" || strings.TrimSpace(input.Gateway.Output.StorageKey) == "" {
		return CommerceShotImageVersionState{}, commerce.Error{Code: CommerceCodeImageFidelityRejected, Message: "图片供应商输出不完整"}
	}
	tag, err := tx.Exec(ctx, `
		UPDATE commerce_shot_image_versions
		SET provider_request_id = NULLIF($7, '')::uuid,
		    provider_call_id = NULLIF($8, '')::uuid,
		    provider_model_id = NULLIF($9, '')::uuid,
		    artifact_id = $10, media_file_id = $11, storage_key = $12,
		    metadata = metadata || jsonb_build_object(
		      'providerRequestId', $7, 'providerCallId', $8,
		      'providerModelId', $9, 'generatedAt', now()
		    )
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND storyboard_shot_id = $4 AND script_unit_generation_id = $5
		  AND image_prompt_plan_id = $6 AND status = 'running'
	`, input.ImageVersion.ID, input.Snapshot.Identity.OrganizationID,
		input.Snapshot.Identity.ProjectID, input.Snapshot.StoryboardShotID,
		input.Snapshot.Identity.UnitGenerationID, input.PromptPlan.ID,
		input.Gateway.ProviderRequestID, input.Gateway.ProviderCallID, input.Gateway.ModelID,
		input.Gateway.Output.ArtifactID, input.Gateway.Output.MediaFileID,
		input.Gateway.Output.StorageKey)
	if err != nil || tag.RowsAffected() != 1 {
		return CommerceShotImageVersionState{}, commerceReferenceImageWriteConflict(err, "镜头图片版本已不再可写")
	}
	input.ImageVersion.ProviderRequestID = input.Gateway.ProviderRequestID
	input.ImageVersion.ProviderCallID = input.Gateway.ProviderCallID
	input.ImageVersion.ProviderModelID = input.Gateway.ModelID
	input.ImageVersion.ArtifactID = input.Gateway.Output.ArtifactID
	input.ImageVersion.MediaFileID = input.Gateway.Output.MediaFileID
	input.ImageVersion.StorageKey = input.Gateway.Output.StorageKey
	if err := tx.Commit(ctx); err != nil {
		return CommerceShotImageVersionState{}, err
	}
	return input.ImageVersion, nil
}

func (r *CommerceGenerationRuntime) CompleteCommerceShotImageVersion(
	ctx context.Context,
	input CompleteCommerceShotImageVersionInput,
) (CommerceShotImageVersionState, error) {
	if err := ValidateCommerceImageFidelityReview(input.Fidelity); err != nil {
		return CommerceShotImageVersionState{}, commerce.Error{Code: CommerceCodeImageFidelityRejected, Message: "商品保真审核契约无效", Cause: err}
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceShotImageVersionState{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := r.lockGenerationState(ctx, tx, input.WorkflowInput.Identity); err != nil {
		return CommerceShotImageVersionState{}, err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowInput.WorkflowRunID, CommercePhaseImageFidelity, input.WorkflowInput); err != nil {
		return CommerceShotImageVersionState{}, err
	}
	issues, err := json.Marshal(input.Fidelity.Issues)
	if err != nil {
		return CommerceShotImageVersionState{}, err
	}
	reviewerOutput, err := json.Marshal(input.Fidelity)
	if err != nil {
		return CommerceShotImageVersionState{}, err
	}
	reviewStatus := "rejected"
	if input.Fidelity.Decision == "approve" {
		reviewStatus = "approved"
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO commerce_product_fidelity_reviews(
			organization_id, project_id, storyboard_shot_id,
			script_unit_generation_id, shot_image_version_id, image_prompt_plan_id,
			review_round, status, issues, reviewer_output,
			prompt_version_id, provider_call_id, provider_model_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, 1, $7, $8, $9,
		        NULLIF($10, '')::uuid, NULLIF($11, '')::uuid, NULLIF($12, '')::uuid)
	`, input.Snapshot.Identity.OrganizationID, input.Snapshot.Identity.ProjectID,
		input.Snapshot.StoryboardShotID, input.Snapshot.Identity.UnitGenerationID,
		input.ImageVersion.ID, input.PromptPlan.ID, reviewStatus, issues, reviewerOutput,
		input.Provenance.PromptVersionID, input.Provenance.ProviderCallID,
		input.Provenance.ProviderModelID); err != nil {
		return CommerceShotImageVersionState{}, err
	}
	if reviewStatus == "approved" {
		if _, err := tx.Exec(ctx, `
			UPDATE commerce_shot_image_versions
			SET active = false, status = 'stale', superseded_at = now()
			WHERE storyboard_shot_id = $1 AND active AND id <> $2
		`, input.Snapshot.StoryboardShotID, input.ImageVersion.ID); err != nil {
			return CommerceShotImageVersionState{}, err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE commerce_shot_image_versions
			SET status = 'succeeded', active = true, fidelity_status = 'approved',
			    completed_at = now(), error_code = NULL, error_message = NULL,
			    metadata = metadata || jsonb_build_object(
			      'fidelityReviewProviderCallId', NULLIF($7, '')::uuid::text,
			      'fidelityReviewPromptVersionId', NULLIF($8, '')::uuid::text,
			      'fidelityReview', $9::jsonb
			    )
			WHERE id = $1 AND organization_id = $2 AND project_id = $3
			  AND storyboard_shot_id = $4 AND script_unit_generation_id = $5
			  AND image_prompt_plan_id = $6 AND status = 'running'
		`, input.ImageVersion.ID, input.Snapshot.Identity.OrganizationID,
			input.Snapshot.Identity.ProjectID, input.Snapshot.StoryboardShotID,
			input.Snapshot.Identity.UnitGenerationID, input.PromptPlan.ID,
			input.Provenance.ProviderCallID, input.Provenance.PromptVersionID,
			reviewerOutput)
		if err != nil || tag.RowsAffected() != 1 {
			return CommerceShotImageVersionState{}, commerceReferenceImageWriteConflict(err, "镜头图片版本已不再可激活")
		}
		if _, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET active_commerce_image_version_id = $2::uuid,
			    image_artifact_id = $3, image_media_file_id = $4,
			    image_storage_key = $5, image_status = 'succeeded',
			    image_error_code = NULL, image_error_message = NULL,
			    image_completed_at = now(), stale_state = 'fresh',
			    metadata = metadata || jsonb_build_object(
			      'commerceImageVersionId', $2::uuid::text,
			      'commerceImageFidelityStatus', 'approved'
			    ), updated_at = now()
			WHERE id = $1 AND active_commerce_image_prompt_plan_id = $6
		`, input.Snapshot.StoryboardShotID, input.ImageVersion.ID,
			input.Gateway.Output.ArtifactID, input.Gateway.Output.MediaFileID,
			input.Gateway.Output.StorageKey, input.PromptPlan.ID); err != nil {
			return CommerceShotImageVersionState{}, err
		}
		input.ImageVersion.Status = "succeeded"
		input.ImageVersion.FidelityStatus = "approved"
		if _, err := r.runs.CompleteAttempt(ctx, tx, commerce.CompleteProductionAttemptParams{
			OrganizationID: input.WorkflowInput.Identity.OrganizationID,
			ProjectID:      input.WorkflowInput.Identity.ProjectID,
			RunID:          input.WorkflowInput.ProductionRunID, ItemID: input.Attempt.ItemID,
			AttemptID: input.Attempt.AttemptID, Status: commerce.ItemSucceeded,
			OutputSnapshot:    mustJSON(map[string]any{"imageVersionId": input.ImageVersion.ID, "fidelityStatus": "approved"}),
			OutputArtifactID:  input.Gateway.Output.ArtifactID,
			OutputMediaFileID: input.Gateway.Output.MediaFileID,
			ProviderRequestID: input.Gateway.ProviderRequestID,
			ProviderCallID:    input.Gateway.ProviderCallID,
		}); err != nil {
			return CommerceShotImageVersionState{}, err
		}
	} else {
		tag, err := tx.Exec(ctx, `
			UPDATE commerce_shot_image_versions
			SET status = 'fidelity_rejected', active = false, fidelity_status = 'rejected',
			    error_code = $7, error_message = $8, completed_at = now(),
			    metadata = metadata || jsonb_build_object('fidelityReview', $9::jsonb)
			WHERE id = $1 AND organization_id = $2 AND project_id = $3
			  AND storyboard_shot_id = $4 AND script_unit_generation_id = $5
			  AND image_prompt_plan_id = $6 AND status = 'running'
		`, input.ImageVersion.ID, input.Snapshot.Identity.OrganizationID,
			input.Snapshot.Identity.ProjectID, input.Snapshot.StoryboardShotID,
			input.Snapshot.Identity.UnitGenerationID, input.PromptPlan.ID,
			CommerceCodeImageFidelityRejected, "商品保真审核未通过", reviewerOutput)
		if err != nil || tag.RowsAffected() != 1 {
			return CommerceShotImageVersionState{}, commerceReferenceImageWriteConflict(err, "镜头图片保真审核状态已变化")
		}
		if _, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET image_status = CASE WHEN active_commerce_image_version_id IS NULL THEN 'failed' ELSE 'succeeded' END,
			    image_error_code = $2, image_error_message = $3,
			    image_completed_at = now(),
			    metadata = metadata || jsonb_build_object(
			      'latestCommerceImageFailureVersionId', $4::text,
			      'commerceImageFidelityStatus', 'rejected'
			    ), updated_at = now()
			WHERE id = $1
		`, input.Snapshot.StoryboardShotID, CommerceCodeImageFidelityRejected,
			"商品保真审核未通过", input.ImageVersion.ID); err != nil {
			return CommerceShotImageVersionState{}, err
		}
		input.ImageVersion.Status = "fidelity_rejected"
		input.ImageVersion.FidelityStatus = "rejected"
		if _, err := r.runs.CompleteAttempt(ctx, tx, commerce.CompleteProductionAttemptParams{
			OrganizationID: input.WorkflowInput.Identity.OrganizationID,
			ProjectID:      input.WorkflowInput.Identity.ProjectID,
			RunID:          input.WorkflowInput.ProductionRunID, ItemID: input.Attempt.ItemID,
			AttemptID: input.Attempt.AttemptID, Status: commerce.ItemFailedTerminal,
			OutputSnapshot:    mustJSON(map[string]any{"imageVersionId": input.ImageVersion.ID, "fidelityStatus": "rejected", "issues": input.Fidelity.Issues}),
			OutputArtifactID:  input.Gateway.Output.ArtifactID,
			OutputMediaFileID: input.Gateway.Output.MediaFileID,
			ProviderRequestID: input.Gateway.ProviderRequestID,
			ProviderCallID:    input.Gateway.ProviderCallID,
			ErrorCode:         CommerceCodeImageFidelityRejected,
			ErrorMessage:      "商品保真审核未通过",
		}); err != nil {
			return CommerceShotImageVersionState{}, err
		}
	}
	eventType := "commerce.shot.reference_image.failed"
	if input.ImageVersion.Status == "succeeded" {
		eventType = "commerce.shot.reference_image.succeeded"
	}
	if err := insertEvent(ctx, tx, input.Snapshot.Identity.OrganizationID, input.Snapshot.Identity.ProjectID,
		eventType, "storyboard_shot", input.Snapshot.StoryboardShotID,
		commerceReferenceImageEventPayload(input.WorkflowInput, input.Snapshot.StoryboardShotID, input.ImageVersion.Status, map[string]any{
			"imagePromptPlanId": input.PromptPlan.ID,
			"imageVersionId":    input.ImageVersion.ID,
			"artifactId":        input.Gateway.Output.ArtifactID,
			"fidelityStatus":    input.ImageVersion.FidelityStatus,
		})); err != nil {
		return CommerceShotImageVersionState{}, err
	}
	input.ImageVersion.ProviderCallID = input.Gateway.ProviderCallID
	input.ImageVersion.ArtifactID = input.Gateway.Output.ArtifactID
	input.ImageVersion.MediaFileID = input.Gateway.Output.MediaFileID
	input.ImageVersion.StorageKey = input.Gateway.Output.StorageKey
	if err := tx.Commit(ctx); err != nil {
		return CommerceShotImageVersionState{}, err
	}
	return input.ImageVersion, nil
}

func (r *CommerceGenerationRuntime) FailCommerceReferenceImageItem(
	ctx context.Context,
	input FailCommerceReferenceImageItemInput,
) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := r.lockGenerationState(ctx, tx, input.WorkflowInput.Identity); err != nil {
		return err
	}
	phase, err := commerceReferenceImagePhase(input.WorkflowInput.Operation)
	if err != nil {
		return err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowInput.WorkflowRunID, phase, input.WorkflowInput); err != nil {
		return err
	}
	if input.ImageVersionID != "" {
		if _, err := tx.Exec(ctx, `
			UPDATE commerce_shot_image_versions
			SET status = 'failed', active = false, fidelity_status = CASE WHEN artifact_id IS NULL THEN 'not_reviewed' ELSE fidelity_status END,
			    error_code = $2, error_message = $3, completed_at = now()
			WHERE id = $1 AND status IN ('queued', 'running')
		`, input.ImageVersionID, input.ErrorCode, input.ErrorMessage); err != nil {
			return err
		}
	}
	if input.WorkflowInput.Operation == "generate_prompts" {
		if _, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET image_prompt_status = 'failed', image_prompt_error_code = $2,
			    image_prompt_error_message = $3, image_prompt_updated_at = now(), updated_at = now()
			WHERE id = $1 AND commerce_storyboard_plan_id IS NOT NULL
		`, input.ShotID, input.ErrorCode, input.ErrorMessage); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET image_status = CASE WHEN active_commerce_image_version_id IS NULL THEN 'failed' ELSE 'succeeded' END,
			    image_error_code = $2, image_error_message = $3,
			    image_completed_at = now(), updated_at = now()
			WHERE id = $1 AND commerce_storyboard_plan_id IS NOT NULL
		`, input.ShotID, input.ErrorCode, input.ErrorMessage); err != nil {
			return err
		}
	}
	status := commerce.ItemFailedTerminal
	if input.Retryable {
		status = commerce.ItemFailedRetryable
	}
	_, err = r.runs.CompleteAttempt(ctx, tx, commerce.CompleteProductionAttemptParams{
		OrganizationID: input.WorkflowInput.Identity.OrganizationID,
		ProjectID:      input.WorkflowInput.Identity.ProjectID,
		RunID:          input.WorkflowInput.ProductionRunID, ItemID: input.Attempt.ItemID,
		AttemptID: input.Attempt.AttemptID, Status: status,
		OutputSnapshot: mustJSON(map[string]any{"shotId": input.ShotID, "imageVersionId": input.ImageVersionID}),
		ErrorCode:      input.ErrorCode, ErrorMessage: input.ErrorMessage, Retryable: input.Retryable,
	})
	if err != nil {
		return err
	}
	eventType := "commerce.shot.reference_image.failed"
	if input.WorkflowInput.Operation == "generate_prompts" {
		eventType = "commerce.shot.image_prompt.failed"
	}
	if err := insertEvent(ctx, tx, input.WorkflowInput.Identity.OrganizationID, input.WorkflowInput.Identity.ProjectID,
		eventType, "storyboard_shot", input.ShotID,
		commerceReferenceImageEventPayload(input.WorkflowInput, input.ShotID, string(status), map[string]any{
			"imageVersionId": input.ImageVersionID,
			"errorCode":      input.ErrorCode,
			"errorMessage":   input.ErrorMessage,
			"retryable":      input.Retryable,
		})); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *CommerceGenerationRuntime) FinalizeCommerceReferenceImageBatch(
	ctx context.Context,
	input CommerceReferenceImageBatchInput,
	output CommerceReferenceImageBatchOutput,
) (CommerceReferenceImageBatchOutput, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return output, err
	}
	defer tx.Rollback(ctx)
	if _, err := r.lockGenerationState(ctx, tx, input.Identity); err != nil {
		return output, err
	}
	phase, err := commerceReferenceImagePhase(input.Operation)
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
	finalized, err := finalizeCommerceReferenceImageWorkflowTx(ctx, tx, input, run, output)
	if err != nil {
		return output, err
	}
	if !finalized {
		if err := tx.Commit(ctx); err != nil {
			return output, err
		}
		return output, nil
	}
	eventType := commerceReferenceImageRunEventType(run.Status)
	if err := insertEvent(ctx, tx, input.Identity.OrganizationID, input.Identity.ProjectID,
		eventType, "commerce_production_run", input.ProductionRunID,
		commerceReferenceImageEventPayload(input, "", string(run.Status), map[string]any{
			"totalItems": run.TotalItems, "completedItems": run.CompletedItems,
			"failedItems": run.FailedItems, "cancelledItems": run.CancelledItems,
		})); err != nil {
		return output, err
	}
	if err := tx.Commit(ctx); err != nil {
		return output, err
	}
	return output, nil
}

func (r *CommerceGenerationRuntime) FinalizeCommerceReferenceImageFailure(
	ctx context.Context,
	input FinalizeCommerceReferenceImageFailureInput,
) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	phase, err := commerceReferenceImagePhase(input.WorkflowInput.Operation)
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
		message = "参考图生产批次执行失败"
	}
	var run commerce.ProductionRun
	if input.Cancelled {
		run, err = r.repository.CancelProductionRun(
			ctx, tx, input.WorkflowInput.Identity.OrganizationID, input.WorkflowInput.Identity.ProjectID,
			input.WorkflowInput.ProductionRunID, message,
		)
	} else {
		run, err = r.repository.FailActiveProductionRunItems(
			ctx, tx, input.WorkflowInput.Identity.OrganizationID, input.WorkflowInput.Identity.ProjectID,
			input.WorkflowInput.ProductionRunID, code, message, true,
		)
	}
	if err != nil {
		return err
	}
	input.Output.Status = run.Status
	input.Output.Total = run.TotalItems
	input.Output.Succeeded = run.CompletedItems
	input.Output.Failed = run.FailedItems
	finalized, err := finalizeCommerceReferenceImageWorkflowTx(ctx, tx, input.WorkflowInput, run, input.Output)
	if err != nil {
		return err
	}
	if finalized {
		if err := insertEvent(ctx, tx, input.WorkflowInput.Identity.OrganizationID, input.WorkflowInput.Identity.ProjectID,
			commerceReferenceImageRunEventType(run.Status), "commerce_production_run", input.WorkflowInput.ProductionRunID,
			commerceReferenceImageEventPayload(input.WorkflowInput, "", string(run.Status), map[string]any{
				"totalItems": run.TotalItems, "completedItems": run.CompletedItems,
				"failedItems": run.FailedItems, "cancelledItems": run.CancelledItems,
				"errorCode": code, "errorMessage": message,
			})); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *CommerceGenerationRuntime) assertCurrentReferenceImageSnapshotTx(
	ctx context.Context,
	tx pgx.Tx,
	snapshot CommerceReferenceImageShotSnapshot,
) error {
	var planRevision, editRevision int
	var contractHash, activePlanID string
	if err := tx.QueryRow(ctx, `
		SELECT plan.id::text, plan.revision, plan.edit_revision, contract.contract_hash
		FROM commerce_storyboard_plans plan
		JOIN commerce_shot_contracts contract
		  ON contract.commerce_storyboard_plan_id = plan.id
		 AND contract.storyboard_shot_id = $5
		WHERE plan.organization_id = $1 AND plan.project_id = $2
		  AND plan.script_unit_generation_id = $3 AND plan.script_unit_id = $4
		  AND plan.active AND plan.status = 'ready'
		FOR UPDATE OF plan, contract
	`, snapshot.Identity.OrganizationID, snapshot.Identity.ProjectID,
		snapshot.Identity.UnitGenerationID, snapshot.Identity.ScriptUnitID,
		snapshot.StoryboardShotID).Scan(&activePlanID, &planRevision, &editRevision, &contractHash); err != nil {
		return generationMismatch("当前活动分镜已变化", err)
	}
	if activePlanID != snapshot.StoryboardPlanID || planRevision != snapshot.StoryboardPlanRevision ||
		editRevision != snapshot.StoryboardEditRevision || contractHash != snapshot.ShotContractHash {
		return generationMismatch("当前分镜或镜头契约已变化，请重新提交参考图任务", nil)
	}
	return nil
}

func loadActiveCommerceImagePromptPlanTx(ctx context.Context, tx pgx.Tx, shotID string) (CommerceImagePromptPlanState, bool, error) {
	var plan CommerceImagePromptPlanState
	var referenceRaw json.RawMessage
	var providerModel sql.NullString
	err := tx.QueryRow(ctx, `
		SELECT id::text, revision, status, prompt, negative_prompt,
		       prompt_hash, input_hash, reference_snapshot_hash,
		       reference_snapshot, image_provider_model_id::text
		FROM commerce_image_prompt_plans
		WHERE storyboard_shot_id = $1 AND active AND status = 'approved'
		ORDER BY revision DESC
		LIMIT 1
		FOR UPDATE
	`, shotID).Scan(&plan.ID, &plan.Revision, &plan.Status, &plan.Prompt,
		&plan.NegativePrompt, &plan.PromptHash, &plan.InputHash,
		&plan.ReferenceHash, &referenceRaw, &providerModel)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommerceImagePromptPlanState{}, false, nil
	}
	if err != nil {
		return CommerceImagePromptPlanState{}, false, err
	}
	if providerModel.Valid {
		plan.ImageProviderModelID = providerModel.String
	}
	if err := json.Unmarshal(referenceRaw, &plan.References); err != nil {
		return CommerceImagePromptPlanState{}, false, err
	}
	return plan, true, nil
}

func loadActiveCommerceShotImageVersionTx(ctx context.Context, tx pgx.Tx, shotID, promptPlanID string) (CommerceShotImageVersionState, bool, error) {
	var item CommerceShotImageVersionState
	var artifactID, mediaFileID, storageKey sql.NullString
	var providerRequestID, providerCallID, providerModelID sql.NullString
	err := tx.QueryRow(ctx, `
		SELECT id::text, revision, status, fidelity_status,
		       artifact_id::text, media_file_id::text, storage_key,
		       provider_request_id::text, provider_call_id::text, provider_model_id::text
		FROM commerce_shot_image_versions
		WHERE storyboard_shot_id = $1 AND image_prompt_plan_id = $2
		  AND active AND status = 'succeeded'
		ORDER BY revision DESC LIMIT 1
		FOR UPDATE
	`, shotID, promptPlanID).Scan(&item.ID, &item.Revision, &item.Status,
		&item.FidelityStatus, &artifactID, &mediaFileID, &storageKey,
		&providerRequestID, &providerCallID, &providerModelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommerceShotImageVersionState{}, false, nil
	}
	if err != nil {
		return CommerceShotImageVersionState{}, false, err
	}
	item.ArtifactID = nullableString(artifactID)
	item.MediaFileID = nullableString(mediaFileID)
	item.StorageKey = nullableString(storageKey)
	item.ProviderRequestID = nullableString(providerRequestID)
	item.ProviderCallID = nullableString(providerCallID)
	item.ProviderModelID = nullableString(providerModelID)
	return item, true, nil
}

func loadReusableCommerceShotImageVersionTx(
	ctx context.Context,
	tx pgx.Tx,
	shotID string,
	promptPlanID string,
	inputHash string,
	referenceHash string,
) (CommerceShotImageVersionState, bool, error) {
	var item CommerceShotImageVersionState
	var providerRequestID, providerCallID, providerModelID sql.NullString
	err := tx.QueryRow(ctx, `
		SELECT id::text, revision, status, fidelity_status,
		       artifact_id::text, media_file_id::text, storage_key,
		       provider_request_id::text, provider_call_id::text, provider_model_id::text
		FROM commerce_shot_image_versions
		WHERE storyboard_shot_id = $1 AND image_prompt_plan_id = $2
		  AND input_hash = $3 AND reference_snapshot_hash = $4
		  AND status = 'failed' AND fidelity_status IN ('pending', 'not_reviewed')
		  AND artifact_id IS NOT NULL AND media_file_id IS NOT NULL
		  AND storage_key IS NOT NULL AND trim(storage_key) <> ''
		ORDER BY revision DESC LIMIT 1
		FOR UPDATE
	`, shotID, promptPlanID, inputHash, referenceHash).Scan(
		&item.ID, &item.Revision, &item.Status, &item.FidelityStatus,
		&item.ArtifactID, &item.MediaFileID, &item.StorageKey,
		&providerRequestID, &providerCallID, &providerModelID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommerceShotImageVersionState{}, false, nil
	}
	if err != nil {
		return CommerceShotImageVersionState{}, false, err
	}
	item.ProviderRequestID = nullableString(providerRequestID)
	item.ProviderCallID = nullableString(providerCallID)
	item.ProviderModelID = nullableString(providerModelID)
	return item, true, nil
}

func selectCommerceImagePromptReferences(all []CommerceReferenceImageReference, ids []string) ([]CommerceReferenceImageReference, error) {
	byID := make(map[string]CommerceReferenceImageReference, len(all))
	for _, reference := range all {
		byID[reference.ReferenceID] = reference
	}
	selected := make([]CommerceReferenceImageReference, 0, len(ids))
	for _, id := range ids {
		reference, ok := byID[id]
		if !ok {
			return nil, generationMismatch("图片提示词引用了冻结包之外的商品图片", nil)
		}
		selected = append(selected, reference)
	}
	return selected, nil
}

func commerceTextHash(value string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(digest[:])
}

func nullableString(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func commerceReferenceImageWriteConflict(err error, message string) error {
	if err != nil {
		return err
	}
	return generationMismatch(message, nil)
}

func finalizeCommerceReferenceImageWorkflowTx(
	ctx context.Context,
	tx pgx.Tx,
	input CommerceReferenceImageBatchInput,
	run commerce.ProductionRun,
	output CommerceReferenceImageBatchOutput,
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
		errorMessage = "用户取消参考图生产批次"
	default:
		return false, generationMismatch("参考图生产批次尚未进入可提交终态", nil)
	}
	raw, err := json.Marshal(output)
	if err != nil {
		return false, err
	}
	var currentStatus string
	var currentOutput json.RawMessage
	if err := tx.QueryRow(ctx, `
		SELECT status, output
		FROM workflow_runs
		WHERE id = $1
		FOR UPDATE
	`, input.WorkflowRunID).Scan(&currentStatus, &currentOutput); err != nil {
		return false, err
	}
	if currentStatus == targetStatus {
		if targetStatus == "cancelled" {
			return false, nil
		}
		if err := assertCommerceSnapshotEqual(currentOutput, raw, "参考图 Workflow 终态输出"); err != nil {
			return false, err
		}
		return false, nil
	}
	if currentStatus != "queued" && currentStatus != "running" && currentStatus != "waiting_review" && currentStatus != "cancelling" {
		return false, generationMismatch("参考图 Workflow 已不再可提交", nil)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET status = $2, output = $3, error_code = NULLIF($4, ''),
		    error_message = NULLIF($5, ''), completed_at = now(),
		    updated_at = now(), revision = revision + 1
		WHERE id = $1 AND status IN ('queued', 'running', 'waiting_review', 'cancelling')
	`, input.WorkflowRunID, targetStatus, raw, errorCode, errorMessage)
	if err != nil || tag.RowsAffected() != 1 {
		return false, commerceReferenceImageWriteConflict(err, "参考图 Workflow 已不再可提交")
	}
	return true, nil
}

func commerceReferenceImageRunEventType(status commerce.ProductionRunStatus) string {
	switch status {
	case commerce.RunPartiallySucceeded:
		return "commerce.production.run.partially_succeeded"
	case commerce.RunFailed:
		return "commerce.production.run.failed"
	case commerce.RunCancelled:
		return "commerce.production.run.cancelled"
	default:
		return "commerce.production.run.completed"
	}
}

func commerceReferenceImageEventPayload(
	input CommerceReferenceImageBatchInput,
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
