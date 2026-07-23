package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/jackc/pgx/v5"
)

type commerceVideoCapabilityContract struct {
	ProfileKey         string   `json:"profileKey"`
	TaskType           string   `json:"taskType"`
	AspectRatios       []string `json:"aspectRatios"`
	SupportedPrompts   []string `json:"supportedPromptLanguages"`
	NativeAudioLocales []string `json:"nativeAudioLanguages"`
	Request            struct {
		AsyncTaskRequired     bool `json:"asyncTaskRequired"`
		PollingRequired       bool `json:"pollingRequired"`
		FirstFrameRequired    bool `json:"firstFrameRequired"`
		LastFrameAllowed      bool `json:"lastFrameAllowed"`
		VideoReferenceAllowed bool `json:"videoReferenceAllowed"`
		MinimumReferences     int  `json:"minimumReferenceImages"`
		MaximumReferences     int  `json:"maximumReferenceImages"`
	} `json:"request"`
	VideoProductionProfile struct {
		ProfileKey       string `json:"profileKey"`
		ProfileVersionID string `json:"profileVersionId"`
	} `json:"videoProductionProfile"`
}

type commerceVideoSegmentLink struct {
	SourceSegmentID string
	Usage           string
	Ordinal         int
	VoiceoverText   string
	VerbatimStart   *int
	VerbatimEnd     *int
}

func (r *CommerceGenerationRuntime) LoadCommerceVideoPromptShot(
	ctx context.Context,
	input CommerceVideoBatchInput,
	shotID string,
) (CommerceVideoPromptShotSnapshot, error) {
	if strings.TrimSpace(shotID) == "" {
		return CommerceVideoPromptShotSnapshot{}, commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "视频提示词镜头 ID 不能为空"}
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceVideoPromptShotSnapshot{}, err
	}
	defer tx.Rollback(ctx)
	state, err := r.lockGenerationState(ctx, tx, input.Identity)
	if err != nil {
		return CommerceVideoPromptShotSnapshot{}, err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowRunID, CommercePhaseVideoPrompt, input); err != nil {
		return CommerceVideoPromptShotSnapshot{}, err
	}

	var snapshot CommerceVideoPromptShotSnapshot
	var productPresentation, soundEffects json.RawMessage
	err = tx.QueryRow(ctx, `
		SELECT plan.id::text, plan.revision, plan.edit_revision,
		       plan.localized_contract_hash, plan.timing_policy_version,
		       shot.id::text, shot.shot_index, shot.planned_duration_ticks,
		       contract.contract_hash, contract.sales_beat, contract.visual_action,
		       contract.product_presentation, contract.voiceover_text,
		       contract.onscreen_text, contract.sound_effects, contract.music_cue,
		       image.id::text, image.artifact_id::text, image.media_file_id::text,
		       image.storage_key, COALESCE(artifact.content_hash, media.checksum, image.input_hash)
		FROM commerce_storyboard_plans plan
		JOIN storyboard_shots shot
		  ON shot.commerce_storyboard_plan_id = plan.id AND shot.deleted_at IS NULL
		JOIN commerce_shot_contracts contract
		  ON contract.storyboard_shot_id = shot.id
		 AND contract.commerce_storyboard_plan_id = plan.id
		JOIN commerce_shot_image_versions image
		  ON image.id = shot.active_commerce_image_version_id
		 AND image.storyboard_shot_id = shot.id
		 AND image.active AND image.status = 'succeeded' AND image.fidelity_status = 'approved'
		JOIN artifacts artifact ON artifact.id = image.artifact_id
		JOIN media_files media ON media.id = image.media_file_id
		WHERE plan.organization_id = $1 AND plan.project_id = $2
		  AND plan.script_unit_id = $3 AND plan.script_unit_generation_id = $4
		  AND plan.active AND plan.status = 'ready' AND shot.id = $5
		FOR SHARE OF plan, shot, contract, image, artifact, media
	`, input.Identity.OrganizationID, input.Identity.ProjectID, input.Identity.ScriptUnitID,
		input.Identity.UnitGenerationID, shotID).Scan(
		&snapshot.StoryboardPlanID, &snapshot.StoryboardPlanRevision, &snapshot.StoryboardEditRevision,
		&snapshot.LocalizedContractHash, &snapshot.TimingPolicyVersion,
		&snapshot.StoryboardShotID, &snapshot.ShotOrdinal, &snapshot.DurationTicks,
		&snapshot.ShotContractHash, &snapshot.SalesBeat, &snapshot.VisualAction,
		&productPresentation, &snapshot.VoiceoverText, &snapshot.OnscreenText,
		&soundEffects, &snapshot.MusicCue,
		&snapshot.FirstFrame.ImageVersionID, &snapshot.FirstFrame.ArtifactID,
		&snapshot.FirstFrame.MediaFileID, &snapshot.FirstFrame.StorageKey,
		&snapshot.FirstFrame.ContentHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommerceVideoPromptShotSnapshot{}, commerce.Error{
			Code:    CommerceCodeVideoReferenceRequired,
			Message: "当前镜头缺少已通过商品保真审核的首帧参考图",
			Cause:   err,
		}
	}
	if err != nil {
		return CommerceVideoPromptShotSnapshot{}, err
	}
	if err := json.Unmarshal(soundEffects, &snapshot.SoundEffects); err != nil {
		return CommerceVideoPromptShotSnapshot{}, generationMismatch("镜头音效契约无法解析", err)
	}
	snapshot.ProductPresentation = append(json.RawMessage(nil), productPresentation...)

	localized, links, err := loadCommerceVideoLocalizedContext(ctx, tx, state.Localization.ID, shotID)
	if err != nil {
		return CommerceVideoPromptShotSnapshot{}, err
	}
	snapshot.LocalizedSegments = localized
	snapshot.SourceSegmentIDs = uniqueCommerceVideoSourceSegments(links)
	verbatim, err := reconstructCommerceVideoVoiceover(links)
	if err != nil {
		return CommerceVideoPromptShotSnapshot{}, err
	}
	if verbatim != snapshot.VoiceoverText {
		return CommerceVideoPromptShotSnapshot{}, commerce.Error{
			Code:    CommerceCodeStoryboardReplanRequired,
			Message: "镜头旁白与冻结脚本段落不一致，请重新生成分镜",
		}
	}
	if len(snapshot.SourceSegmentIDs) == 0 {
		return CommerceVideoPromptShotSnapshot{}, commerce.Error{
			Code:    CommerceCodeStoryboardReplanRequired,
			Message: "镜头缺少脚本段落关联，请重新生成分镜",
		}
	}

	var capability commerceVideoCapabilityContract
	if err := json.Unmarshal(state.Template.VideoCapabilityContract, &capability); err != nil {
		return CommerceVideoPromptShotSnapshot{}, generationMismatch("冻结视频能力契约无法解析", err)
	}
	if capability.VideoProductionProfile.ProfileKey != "single_frame_i2v" ||
		!capability.Request.AsyncTaskRequired || !capability.Request.PollingRequired ||
		!capability.Request.FirstFrameRequired || capability.Request.MinimumReferences != 1 ||
		capability.Request.MaximumReferences != 1 || capability.Request.LastFrameAllowed ||
		capability.Request.VideoReferenceAllowed {
		return CommerceVideoPromptShotSnapshot{}, generationMismatch("冻结视频能力契约不是可执行的单首帧图生视频契约", nil)
	}
	allowedDurations := state.StoryboardConfig.AllowedDurations
	if len(allowedDurations) == 0 {
		return CommerceVideoPromptShotSnapshot{}, generationMismatch("冻结视频模型没有可执行的整数时长集合", nil)
	}
	if state.BindingConfig.ProductionConfiguration.TimelineTimebase <= 0 || snapshot.DurationTicks <= 0 ||
		snapshot.DurationTicks%state.BindingConfig.ProductionConfiguration.TimelineTimebase != 0 {
		return CommerceVideoPromptShotSnapshot{}, generationMismatch("镜头时长不是模型可执行的整数秒", nil)
	}
	snapshot.DurationSeconds = int(snapshot.DurationTicks / state.BindingConfig.ProductionConfiguration.TimelineTimebase)
	if !containsInt(allowedDurations, snapshot.DurationSeconds) {
		return CommerceVideoPromptShotSnapshot{}, generationMismatch("镜头时长不属于冻结视频模型支持集合", nil)
	}
	if !containsFold(capability.AspectRatios, state.BindingConfig.ProductionConfiguration.AspectRatio) {
		return CommerceVideoPromptShotSnapshot{}, generationMismatch("项目画幅不在冻结视频模型能力集合中", nil)
	}
	videoModel, err := resolveFrozenCommerceMediaModelBinding(
		"videoGenerator", state.ModelContracts, state.Production.CommerceBinding.ModelRoutingSnapshot,
	)
	if err != nil {
		return CommerceVideoPromptShotSnapshot{}, err
	}
	var profileKey, inputContractVersion string
	if err := tx.QueryRow(ctx, `
		SELECT profile.profile_key, version.input_contract_version
		FROM video_production_profile_versions version
		JOIN video_production_profiles profile ON profile.id = version.profile_id
		WHERE version.id = $1
	`, state.Production.VideoBinding.ProfileVersionID).Scan(&profileKey, &inputContractVersion); err != nil {
		return CommerceVideoPromptShotSnapshot{}, err
	}
	if profileKey != capability.VideoProductionProfile.ProfileKey ||
		state.Production.VideoBinding.ProfileVersionID != capability.VideoProductionProfile.ProfileVersionID {
		return CommerceVideoPromptShotSnapshot{}, generationMismatch("冻结 Commerce 视频能力与项目视频方案不一致", nil)
	}

	languageCapabilityHash, err := commerceContractHash(map[string]any{
		"commerceCapabilitySnapshot": state.Production.CommerceBinding.CapabilitySnapshot,
		"videoCapabilityContract":    state.Template.VideoCapabilityContract,
		"videoModel":                 videoModel,
	})
	if err != nil {
		return CommerceVideoPromptShotSnapshot{}, err
	}
	snapshot.Identity = state.Generation.Identity
	snapshot.FullLocalizedScript = state.Localization.LocalizedContent
	snapshot.LocalizedContentHash = state.Localization.LocalizedContentHash
	snapshot.ProductVersionID = state.ProductVersion.ID
	snapshot.LocalizationID = state.Localization.ID
	snapshot.ReferencePackID = state.ReferencePack.ID
	snapshot.ProductFacts = append(json.RawMessage(nil), state.ProductVersion.FactsSnapshot...)
	snapshot.TargetLocale = state.Localization.TargetLanguage
	snapshot.AspectRatio = state.BindingConfig.ProductionConfiguration.AspectRatio
	snapshot.TimelineTimebase = state.BindingConfig.ProductionConfiguration.TimelineTimebase
	snapshot.FPSNumerator = state.BindingConfig.ProductionConfiguration.FPSNumerator
	snapshot.FPSDenominator = state.BindingConfig.ProductionConfiguration.FPSDenominator
	snapshot.AudioStrategy = state.BindingConfig.ProductionConfiguration.AudioStrategy
	snapshot.AudioRequirement = state.BindingConfig.ProductionConfiguration.AudioRequirement
	snapshot.NativeAudioRequested = (snapshot.AudioStrategy == "native_av" || snapshot.AudioStrategy == "hybrid") && snapshot.AudioRequirement != "disabled"
	snapshot.NativeAudioRequired = snapshot.AudioStrategy == "native_av" && snapshot.AudioRequirement == "required"
	snapshot.AllowedDurations = allowedDurations
	snapshot.SupportedPromptLanguages = append([]string(nil), capability.SupportedPrompts...)
	snapshot.NativeAudioLanguages = append([]string(nil), capability.NativeAudioLocales...)
	snapshot.InstructionLanguage = preferredCommerceInstructionLanguage(snapshot.TargetLocale, snapshot.SupportedPromptLanguages)
	snapshot.LanguageCapabilitySnapshotHash = languageCapabilityHash
	snapshot.VideoProfileKey = profileKey
	snapshot.VideoProfileVersionID = state.Production.VideoBinding.ProfileVersionID
	snapshot.VideoProfileSnapshotHash = state.Production.VideoBinding.ProfileSnapshotHash
	snapshot.VideoInputContract = inputContractVersion
	snapshot.Bindings = CommerceVideoPromptAgentBindings{
		VideoPromptAgent:    state.AgentBindings["videoPromptAgent"],
		VideoPromptReviewer: state.AgentBindings["videoPromptReviewer"],
	}
	snapshot.AgentModelContextLimit, snapshot.AgentModelPromptLimit, err = frozenCommercePromptAgentLimits(
		state.Production.CommerceBinding.CapabilitySnapshot,
		snapshot.Bindings.VideoPromptAgent.Role,
		snapshot.Bindings.VideoPromptAgent.ProviderModelID,
	)
	if err != nil {
		return CommerceVideoPromptShotSnapshot{}, err
	}
	snapshot.VideoModel = videoModel
	snapshot.InputHash, err = commerceContractHash(map[string]any{
		"identity": snapshot.Identity, "storyboardPlanId": snapshot.StoryboardPlanID,
		"storyboardPlanRevision": snapshot.StoryboardPlanRevision,
		"storyboardEditRevision": snapshot.StoryboardEditRevision,
		"storyboardShotId":       snapshot.StoryboardShotID, "shotContractHash": snapshot.ShotContractHash,
		"localizedContentHash": snapshot.LocalizedContentHash, "localizedContractHash": snapshot.LocalizedContractHash,
		"sourceSegmentIds": snapshot.SourceSegmentIDs, "voiceoverText": snapshot.VoiceoverText,
		"onscreenText": snapshot.OnscreenText, "soundEffects": snapshot.SoundEffects, "musicCue": snapshot.MusicCue,
		"firstFrame": snapshot.FirstFrame, "durationSeconds": snapshot.DurationSeconds,
		"productVersionId": snapshot.ProductVersionID, "referencePackId": snapshot.ReferencePackID,
		"languageCapabilitySnapshotHash": snapshot.LanguageCapabilitySnapshotHash,
		"videoProfileSnapshotHash":       snapshot.VideoProfileSnapshotHash,
		"bindings":                       snapshot.Bindings, "videoModel": snapshot.VideoModel,
		"agentModelContextLimit": snapshot.AgentModelContextLimit,
		"agentModelPromptLimit":  snapshot.AgentModelPromptLimit,
	})
	if err != nil {
		return CommerceVideoPromptShotSnapshot{}, err
	}
	return snapshot, nil
}

func frozenCommercePromptAgentLimits(
	capabilitySnapshot json.RawMessage,
	role string,
	providerModelID string,
) (int, int, error) {
	var roles map[string]struct {
		ProviderModelID string                `json:"providerModelId"`
		Capabilities    []provider.Capability `json:"capabilities"`
	}
	if err := json.Unmarshal(capabilitySnapshot, &roles); err != nil {
		return 0, 0, generationMismatch("Commerce Binding 模型能力快照无法解析", err)
	}
	frozen, ok := roles[strings.TrimSpace(role)]
	if !ok {
		return 0, 0, generationMismatch("Commerce Binding 缺少冻结模型能力："+role, nil)
	}
	if strings.TrimSpace(frozen.ProviderModelID) == "" ||
		strings.TrimSpace(frozen.ProviderModelID) != strings.TrimSpace(providerModelID) {
		return 0, 0, generationMismatch("Commerce Binding 视频提示词模型身份不一致", nil)
	}
	contextLimit, promptLimit := promptContextLimits([]provider.GatewayModelConstraintCandidate{{
		ProviderModelID: frozen.ProviderModelID,
		ContextWindow:   provider.ModelContextWindow(frozen.Capabilities),
		Prompt:          provider.ModelPromptLengthConstraint(frozen.Capabilities),
	}})
	return contextLimit, promptLimit, nil
}

func loadCommerceVideoLocalizedContext(
	ctx context.Context,
	tx pgx.Tx,
	localizationID string,
	shotID string,
) ([]CommerceLocalizedSegmentSnapshot, []commerceVideoSegmentLink, error) {
	rows, err := tx.Query(ctx, `
		SELECT segment.id::text, segment.source_segment_id::text, segment.segment_no,
		       segment.sales_beat, segment.localized_text, segment.voiceover_text,
		       segment.onscreen_text, segment.product_claims,
		       segment.required_product_features, source.required
		FROM commerce_localization_segments segment
		JOIN commerce_ad_script_segments source ON source.id = segment.source_segment_id
		WHERE segment.localization_id = $1
		ORDER BY segment.segment_no, segment.id
	`, localizationID)
	if err != nil {
		return nil, nil, err
	}
	localized := make([]CommerceLocalizedSegmentSnapshot, 0)
	for rows.Next() {
		var item CommerceLocalizedSegmentSnapshot
		var claims, features json.RawMessage
		if err := rows.Scan(
			&item.ID, &item.SourceSegmentID, &item.Ordinal, &item.SalesBeat,
			&item.LocalizedText, &item.VoiceoverText, &item.OnscreenText,
			&claims, &features, &item.Required,
		); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if err := json.Unmarshal(claims, &item.ProductClaims); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if err := json.Unmarshal(features, &item.RequiredProductFeatures); err != nil {
			rows.Close()
			return nil, nil, err
		}
		localized = append(localized, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, err
	}
	rows.Close()

	rows, err = tx.Query(ctx, `
		SELECT segment.source_segment_id::text, link.usage, link.ordinal,
		       segment.voiceover_text, link.verbatim_start, link.verbatim_end
		FROM commerce_shot_segment_links link
		JOIN commerce_localization_segments segment ON segment.id = link.localization_segment_id
		WHERE link.storyboard_shot_id = $1 AND link.localization_id = $2
		ORDER BY CASE link.usage WHEN 'voiceover' THEN 0 ELSE 1 END, link.ordinal, link.id
	`, shotID, localizationID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	links := make([]commerceVideoSegmentLink, 0)
	for rows.Next() {
		var item commerceVideoSegmentLink
		if err := rows.Scan(&item.SourceSegmentID, &item.Usage, &item.Ordinal,
			&item.VoiceoverText, &item.VerbatimStart, &item.VerbatimEnd); err != nil {
			return nil, nil, err
		}
		links = append(links, item)
	}
	return localized, links, rows.Err()
}

func uniqueCommerceVideoSourceSegments(links []commerceVideoSegmentLink) []string {
	seen := make(map[string]struct{}, len(links))
	result := make([]string, 0, len(links))
	for _, link := range links {
		if _, ok := seen[link.SourceSegmentID]; ok {
			continue
		}
		seen[link.SourceSegmentID] = struct{}{}
		result = append(result, link.SourceSegmentID)
	}
	return result
}

func reconstructCommerceVideoVoiceover(links []commerceVideoSegmentLink) (string, error) {
	parts := make([]string, 0)
	for _, link := range links {
		if link.Usage != "voiceover" {
			continue
		}
		if link.VerbatimStart == nil || link.VerbatimEnd == nil {
			return "", commerce.Error{Code: CommerceCodeStoryboardReplanRequired, Message: "旁白段落缺少逐字范围，请重新生成分镜"}
		}
		runes := []rune(link.VoiceoverText)
		if *link.VerbatimStart < 0 || *link.VerbatimEnd <= *link.VerbatimStart || *link.VerbatimEnd > len(runes) {
			return "", commerce.Error{Code: CommerceCodeStoryboardReplanRequired, Message: "旁白逐字范围已失效，请重新生成分镜"}
		}
		parts = append(parts, string(runes[*link.VerbatimStart:*link.VerbatimEnd]))
	}
	return strings.TrimSpace(strings.Join(parts, "")), nil
}

func preferredCommerceInstructionLanguage(target string, supported []string) string {
	if containsFold(supported, target) {
		return target
	}
	if containsFold(supported, "en-US") {
		return "en-US"
	}
	if len(supported) > 0 {
		return strings.TrimSpace(supported[0])
	}
	return target
}

func CommerceVideoSubjectHash(input CommerceVideoBatchInput, shotID string) (string, error) {
	return commerceContractHash(map[string]any{
		"identity":         input.Identity,
		"storyboardPlanId": input.StoryboardPlanID, "planEditRevision": input.PlanEditRevision,
		"operation": input.Operation, "shotId": shotID, "force": input.Force,
		"resolution": input.Resolution,
	})
}

func commerceVideoPhase(operation string) (CommerceWorkflowPhase, error) {
	switch operation {
	case "generate_prompts":
		return CommercePhaseVideoPrompt, nil
	case "generate_videos":
		return CommercePhaseVideoRender, nil
	default:
		return "", commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "视频生产批次操作无效"}
	}
}

func validateCommerceVideoSnapshot(snapshot CommerceVideoPromptShotSnapshot) error {
	if strings.TrimSpace(snapshot.FirstFrame.ImageVersionID) == "" || strings.TrimSpace(snapshot.FirstFrame.MediaFileID) == "" {
		return commerce.Error{Code: CommerceCodeVideoReferenceRequired, Message: "视频首帧参考图不可用"}
	}
	if snapshot.DurationSeconds < 1 || snapshot.DurationTicks != int64(snapshot.DurationSeconds)*snapshot.TimelineTimebase {
		return fmt.Errorf("commerce shot duration is invalid")
	}
	return nil
}
