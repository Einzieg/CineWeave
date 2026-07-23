package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Einzieg/cineweave/internal/commerce"
	"github.com/jackc/pgx/v5"
)

type commerceImageCapabilityContract struct {
	ProfileKey     string `json:"profileKey"`
	ReferenceInput struct {
		Required bool `json:"required"`
		Minimum  int  `json:"minimum"`
		Maximum  int  `json:"maximum"`
	} `json:"referenceInput"`
	AspectRatios []string `json:"aspectRatios"`
	Qualities    []string `json:"qualities"`
}

func (r *CommerceGenerationRuntime) LoadCommerceReferenceImageShot(
	ctx context.Context,
	input CommerceReferenceImageBatchInput,
	shotID string,
) (CommerceReferenceImageShotSnapshot, error) {
	if strings.TrimSpace(shotID) == "" {
		return CommerceReferenceImageShotSnapshot{}, commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "参考图镜头 ID 不能为空"}
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceReferenceImageShotSnapshot{}, err
	}
	defer tx.Rollback(ctx)
	state, err := r.lockGenerationState(ctx, tx, input.Identity)
	if err != nil {
		return CommerceReferenceImageShotSnapshot{}, err
	}
	phase, err := commerceReferenceImagePhase(input.Operation)
	if err != nil {
		return CommerceReferenceImageShotSnapshot{}, err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowRunID, phase, input); err != nil {
		return CommerceReferenceImageShotSnapshot{}, err
	}

	var snapshot CommerceReferenceImageShotSnapshot
	var productPresentation, soundEffects json.RawMessage
	err = tx.QueryRow(ctx, `
		SELECT plan.id::text, plan.revision, plan.edit_revision,
		       shot.id::text, shot.shot_index,
		       contract.contract_hash, contract.sales_beat, contract.visual_action,
		       contract.product_presentation, contract.voiceover_text,
		       contract.onscreen_text, contract.sound_effects, contract.music_cue
		FROM commerce_storyboard_plans plan
		JOIN storyboard_shots shot
		  ON shot.commerce_storyboard_plan_id = plan.id
		 AND shot.deleted_at IS NULL
		JOIN commerce_shot_contracts contract
		  ON contract.storyboard_shot_id = shot.id
		 AND contract.commerce_storyboard_plan_id = plan.id
		WHERE plan.organization_id = $1 AND plan.project_id = $2
		  AND plan.script_unit_id = $3
		  AND plan.script_unit_generation_id = $4
		  AND plan.active AND plan.status = 'ready'
		  AND shot.id = $5
		FOR SHARE OF plan, shot, contract
	`, input.Identity.OrganizationID, input.Identity.ProjectID, input.Identity.ScriptUnitID,
		input.Identity.UnitGenerationID, shotID).Scan(
		&snapshot.StoryboardPlanID, &snapshot.StoryboardPlanRevision, &snapshot.StoryboardEditRevision,
		&snapshot.StoryboardShotID, &snapshot.ShotOrdinal,
		&snapshot.ShotContractHash, &snapshot.SalesBeat, &snapshot.VisualAction,
		&productPresentation, &snapshot.VoiceoverText, &snapshot.OnscreenText,
		&soundEffects, &snapshot.MusicCue,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommerceReferenceImageShotSnapshot{}, commerce.Error{Code: commerce.CodeStoryboardShotRequired, Message: "当前活动分镜方案中不存在该镜头", Cause: err}
	}
	if err != nil {
		return CommerceReferenceImageShotSnapshot{}, err
	}
	if err := json.Unmarshal(soundEffects, &snapshot.SoundEffects); err != nil {
		return CommerceReferenceImageShotSnapshot{}, generationMismatch("镜头音效契约无法解析", err)
	}
	snapshot.ProductPresentation = append(json.RawMessage(nil), productPresentation...)

	rows, err := tx.Query(ctx, `
		SELECT reference.source_pack_item_id::text,
		       reference.product_reference_id::text,
		       reference.role, reference.ordinal,
		       item.artifact_id::text, item.media_file_id::text,
		       media.storage_key, item.content_hash, reference.required
		FROM commerce_shot_product_references reference
		JOIN commerce_product_reference_pack_items item
		  ON item.id = reference.source_pack_item_id
		 AND item.reference_pack_id = reference.source_pack_id
		 AND item.product_reference_id = reference.product_reference_id
		JOIN media_files media
		  ON media.id = item.media_file_id
		 AND media.organization_id = reference.organization_id
		 AND media.project_id = reference.project_id
		WHERE reference.organization_id = $1 AND reference.project_id = $2
		  AND reference.storyboard_shot_id = $3
		  AND reference.script_unit_generation_id = $4
		ORDER BY reference.ordinal, reference.id
	`, input.Identity.OrganizationID, input.Identity.ProjectID, shotID, input.Identity.UnitGenerationID)
	if err != nil {
		return CommerceReferenceImageShotSnapshot{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var reference CommerceReferenceImageReference
		if err := rows.Scan(
			&reference.PackItemID, &reference.ReferenceID, &reference.Role, &reference.Ordinal,
			&reference.ArtifactID, &reference.MediaFileID, &reference.StorageKey,
			&reference.ContentHash, &reference.Required,
		); err != nil {
			return CommerceReferenceImageShotSnapshot{}, err
		}
		snapshot.References = append(snapshot.References, reference)
	}
	if err := rows.Err(); err != nil {
		return CommerceReferenceImageShotSnapshot{}, err
	}

	var capability commerceImageCapabilityContract
	if err := json.Unmarshal(state.Template.ImageCapabilityContract, &capability); err != nil {
		return CommerceReferenceImageShotSnapshot{}, generationMismatch("冻结图片能力契约无法解析", err)
	}
	if capability.ReferenceInput.Minimum < 1 || capability.ReferenceInput.Maximum < capability.ReferenceInput.Minimum {
		return CommerceReferenceImageShotSnapshot{}, generationMismatch("冻结图片参考图数量契约无效", nil)
	}
	if len(snapshot.References) < capability.ReferenceInput.Minimum {
		return CommerceReferenceImageShotSnapshot{}, commerce.Error{Code: CommerceCodeProductReferencePackStale, Message: "当前镜头缺少满足图片模型要求的商品参考图"}
	}
	imageModel, err := resolveFrozenCommerceMediaModelBinding(
		"imageGenerator", state.ModelContracts, state.Production.CommerceBinding.ModelRoutingSnapshot,
	)
	if err != nil {
		return CommerceReferenceImageShotSnapshot{}, err
	}
	snapshot.Identity = state.Generation.Identity
	snapshot.ProductFacts = append(json.RawMessage(nil), state.ProductVersion.FactsSnapshot...)
	snapshot.ProductVersionID = state.ProductVersion.ID
	snapshot.LocalizationID = state.Localization.ID
	snapshot.ReferencePackID = state.ReferencePack.ID
	snapshot.ReferencePackHash = state.ReferencePack.PackHash
	snapshot.TargetLocale = state.Localization.TargetLanguage
	snapshot.AspectRatio = state.BindingConfig.ProductionConfiguration.AspectRatio
	snapshot.ImageQuality = state.BindingConfig.ProductionConfiguration.ImageQuality
	if snapshot.ImageQuality == "" {
		snapshot.ImageQuality = "standard"
	}
	snapshot.MinimumReferences = capability.ReferenceInput.Minimum
	snapshot.MaximumReferences = capability.ReferenceInput.Maximum
	snapshot.Bindings = CommerceReferenceImageAgentBindings{
		ImagePromptAgent:      state.AgentBindings["imagePromptAgent"],
		ImageFidelityReviewer: state.AgentBindings["imageFidelityReviewer"],
	}
	snapshot.ImageModel = imageModel
	snapshot.InputHash, err = commerceContractHash(map[string]any{
		"identity": snapshot.Identity, "storyboardPlanId": snapshot.StoryboardPlanID,
		"storyboardPlanRevision": snapshot.StoryboardPlanRevision,
		"storyboardEditRevision": snapshot.StoryboardEditRevision,
		"storyboardShotId":       snapshot.StoryboardShotID, "shotContractHash": snapshot.ShotContractHash,
		"productVersionId": snapshot.ProductVersionID, "productFacts": snapshot.ProductFacts,
		"localizationId": snapshot.LocalizationID, "referencePackId": snapshot.ReferencePackID,
		"referencePackHash": snapshot.ReferencePackHash, "references": snapshot.References,
		"aspectRatio": snapshot.AspectRatio, "imageQuality": snapshot.ImageQuality,
		"bindings": snapshot.Bindings, "imageModel": snapshot.ImageModel,
	})
	if err != nil {
		return CommerceReferenceImageShotSnapshot{}, err
	}
	return snapshot, nil
}

func commerceReferenceImagePhase(operation string) (CommerceWorkflowPhase, error) {
	switch operation {
	case "generate_prompts":
		return CommercePhaseImagePrompt, nil
	case "generate_images":
		return CommercePhaseImageFidelity, nil
	default:
		return "", commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "参考图批次操作无效"}
	}
}

func resolveFrozenCommerceMediaModelBinding(
	role string,
	models map[string]commerceSetupModelContract,
	routingRaw json.RawMessage,
) (CommerceMediaModelBinding, error) {
	model, ok := models[role]
	if !ok || strings.TrimSpace(model.ProfileKey) == "" {
		return CommerceMediaModelBinding{}, generationMismatch("Commerce Binding 缺少冻结媒体模型契约："+role, nil)
	}
	var routing map[string]struct {
		Candidates []struct {
			ProviderModelID string `json:"providerModelId"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(routingRaw, &routing); err != nil {
		return CommerceMediaModelBinding{}, generationMismatch("Commerce Binding 媒体模型路由快照无法解析", err)
	}
	route, ok := routing[role]
	if !ok || len(route.Candidates) == 0 || strings.TrimSpace(route.Candidates[0].ProviderModelID) == "" {
		return CommerceMediaModelBinding{}, generationMismatch("Commerce Binding 缺少冻结媒体模型路由："+role, nil)
	}
	return CommerceMediaModelBinding{
		Role: role, ModelProfileKey: model.ProfileKey,
		ProviderModelID: strings.TrimSpace(route.Candidates[0].ProviderModelID),
	}, nil
}
