package provider

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/events"
	"github.com/jackc/pgx/v5"
)

type videoExecutionSegment struct {
	ExecutionPlanID                string
	RenderSegmentID                string
	OperationID                    string
	OperationItemID                string
	OperationItemAttempt           int
	OrganizationID                 string
	ProjectID                      string
	ProductionGenerationID         string
	VideoProductionBindingID       string
	VideoProductionBindingRevision int64
	StoryboardShotID               string
	ProviderAccountID              string
	ProviderModelID                string
	ModelProfileID                 string
	ModelProfileBindingID          string
	ModelProfileKey                string
	VariantKey                     string
	CapabilitySnapshotHash         string
	CapabilityAttestationID        string
	RequestedDuration              float64
	SegmentIndex                   int
	ContinuityMode                 string
	ReferenceMode                  string
	AspectRatio                    string
	Resolution                     string
	AudioRequirement               string
	Status                         string
	ProviderAsyncTaskID            string
	ProviderCallID                 string
	ExternalTaskID                 string
	ProviderTaskStatus             string
	ProductionProfileVersionID     string
	ProductionProfileSnapshotHash  string
	InputContractVersion           string
	InputContractKey               string
	InputContract                  VideoInputContract
	InputContractHash              string
	ShotStateRevision              int
	ShotStateHash                  string
	TransitionHash                 string
	ReferencePackID                string
	ReferencePackHash              string
	PromptContextPlanID            string
	PromptContextPlanHash          string
	VideoPromptPlanID              string
	NativeAudioRequired            bool
	Prompt                         string
	PromptHash                     string
	DialogueCues                   []GatewayVideoDialogueSpan
}

func (s *Service) validateVideoExecutionRequest(ctx context.Context, req *GatewayVideoCreateTaskRequest, input gatewayVideoInput) (*videoExecutionSegment, error) {
	if strings.TrimSpace(req.CommerceDirectVideoJobID) != "" {
		if err := s.validateCommerceDirectVideoExecutionRequest(ctx, *req, input); err != nil {
			return nil, err
		}
		return nil, nil
	}
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
	operationIdentityCount := 0
	if strings.TrimSpace(req.OperationID) != "" {
		operationIdentityCount++
	}
	if strings.TrimSpace(req.OperationItemID) != "" {
		operationIdentityCount++
	}
	if req.OperationItemAttempt > 0 {
		operationIdentityCount++
	}
	if operationIdentityCount != 0 && operationIdentityCount != 3 {
		return nil, &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "operationId、operationItemId 与 operationItemAttempt 必须同时提供", Retryable: false}}
	}
	var segment videoExecutionSegment
	var modelProfileID, bindingID, modelProfileKey, providerTaskID, providerCallID, externalTaskID, providerTaskStatus, capabilityAttestationID sql.NullString
	var expiresAt time.Time
	var inputContractSnapshot, dialogueCues []byte
	if err := s.db.QueryRow(ctx, `
		SELECT plan.id::text, segment.id::text,
		       COALESCE(checkpoint.id::text, ''), COALESCE(plan.operation_item_id::text, ''), COALESCE(plan.operation_item_attempt, 0),
		       plan.organization_id::text, plan.project_id::text,
		       plan.production_generation_id::text, plan.video_production_binding_id::text,
		       plan.video_production_binding_revision, segment.storyboard_shot_id::text,
		       model.provider_account_id::text, model.id::text,
		       plan.model_profile_id::text, plan.model_profile_binding_id::text, plan.model_profile_key,
		       plan.variant_key, plan.capability_snapshot_hash, plan.capability_attestation_id::text,
		       segment.requested_duration_seconds::float8,
		       segment.segment_index, segment.continuity_mode,
		       plan.reference_mode, plan.aspect_ratio, plan.resolution, plan.audio_requirement,
		       plan.expires_at, segment.status,
		       segment.provider_async_task_id::text, segment.provider_call_id::text, segment.external_task_id,
		       task.status,
		       plan.profile_version_id::text, plan.production_profile_snapshot_hash,
		       COALESCE(plan.metadata->>'inputContractVersion', ''),
		       CASE WHEN segment.segment_index = 0
		            THEN plan.initial_input_contract_snapshot
		            ELSE plan.continuation_input_contract_snapshot END,
		       segment.input_contract_key, segment.input_contract_hash,
		       plan.shot_state_revision, plan.shot_state_hash, COALESCE(plan.transition_hash, ''),
		       plan.reference_pack_id::text, plan.reference_pack_hash,
		       plan.prompt_context_plan_id::text, plan.prompt_context_plan_hash,
		       plan.video_prompt_plan_id::text, plan.native_audio_required,
		       COALESCE(segment.prompt, ''),
		       COALESCE(segment.execution_prompt_hash, ''),
		       segment.dialogue
		FROM video_render_plans plan
		JOIN video_render_segments segment ON segment.video_render_plan_id = plan.id
		JOIN provider_models model ON model.id = COALESCE(segment.provider_model_id, plan.provider_model_id)
		JOIN provider_accounts account ON account.id = model.provider_account_id
		JOIN storyboard_shots shot
		  ON shot.id = segment.storyboard_shot_id
		 AND shot.project_id = plan.project_id
		 AND shot.production_generation_id = plan.production_generation_id
		 AND shot.deleted_at IS NULL
		LEFT JOIN provider_async_tasks task ON task.id = segment.provider_async_task_id
		LEFT JOIN episode_video_production_items item
		  ON item.id = plan.operation_item_id
		 AND item.video_render_plan_id = plan.id
		 AND item.execution_identity_version = 2
		 AND item.attempt = plan.operation_item_attempt
		LEFT JOIN episode_video_production_batches batch ON batch.id = item.batch_id
		LEFT JOIN episode_video_production_checkpoints checkpoint ON checkpoint.id = batch.checkpoint_id
		WHERE plan.id = $1 AND segment.id = $2 AND plan.organization_id = $3
		  AND plan.project_id::text = $4
		  AND plan.production_generation_id::text = $5
		  AND plan.video_production_binding_id::text = $6
		  AND plan.video_production_binding_revision = $7
		  AND segment.project_id = plan.project_id
		  AND segment.production_generation_id = plan.production_generation_id
		  AND segment.storyboard_shot_id::text = $8
		  AND (
		    (plan.operation_item_id IS NULL AND $9 = '' AND $10 = '' AND $11 = 0)
		    OR (
		      plan.operation_item_id::text = $9 AND plan.operation_item_attempt = $11
		      AND checkpoint.id::text = $10
		      AND checkpoint.organization_id = plan.organization_id
		      AND checkpoint.project_id = plan.project_id
		      AND checkpoint.production_generation_id = plan.production_generation_id
		      AND checkpoint.video_production_binding_id = plan.video_production_binding_id
		      AND checkpoint.video_production_binding_revision = plan.video_production_binding_revision
		      AND checkpoint.workflow_run_id = plan.workflow_run_id
		      AND checkpoint.workflow_run_id::text = $12
		    )
		  )
		  AND plan.active = true AND plan.status NOT IN ('stale', 'archived', 'cancelled', 'replan_required')
		  AND account.status = 'active' AND model.status = 'active'
	`, planID, segmentID, req.OrganizationID, strings.TrimSpace(req.ProjectID),
		strings.TrimSpace(req.ProductionGenerationID), strings.TrimSpace(req.VideoProductionBindingID),
		req.VideoProductionBindingRevision, strings.TrimSpace(req.StoryboardShotID),
		strings.TrimSpace(req.OperationItemID), strings.TrimSpace(req.OperationID), req.OperationItemAttempt,
		strings.TrimSpace(req.WorkflowRunID)).Scan(
		&segment.ExecutionPlanID, &segment.RenderSegmentID,
		&segment.OperationID, &segment.OperationItemID, &segment.OperationItemAttempt,
		&segment.OrganizationID, &segment.ProjectID,
		&segment.ProductionGenerationID, &segment.VideoProductionBindingID,
		&segment.VideoProductionBindingRevision, &segment.StoryboardShotID,
		&segment.ProviderAccountID, &segment.ProviderModelID,
		&modelProfileID, &bindingID, &modelProfileKey, &segment.VariantKey, &segment.CapabilitySnapshotHash,
		&capabilityAttestationID,
		&segment.RequestedDuration, &segment.SegmentIndex, &segment.ContinuityMode,
		&segment.ReferenceMode, &segment.AspectRatio, &segment.Resolution,
		&segment.AudioRequirement, &expiresAt, &segment.Status,
		&providerTaskID, &providerCallID, &externalTaskID, &providerTaskStatus,
		&segment.ProductionProfileVersionID, &segment.ProductionProfileSnapshotHash,
		&segment.InputContractVersion, &inputContractSnapshot, &segment.InputContractKey, &segment.InputContractHash,
		&segment.ShotStateRevision, &segment.ShotStateHash, &segment.TransitionHash,
		&segment.ReferencePackID, &segment.ReferencePackHash,
		&segment.PromptContextPlanID, &segment.PromptContextPlanHash,
		&segment.VideoPromptPlanID, &segment.NativeAudioRequired,
		&segment.Prompt, &segment.PromptHash, &dialogueCues,
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
	segment.CapabilityAttestationID = nullStringText(capabilityAttestationID)
	if err := json.Unmarshal(inputContractSnapshot, &segment.InputContract); err != nil {
		return nil, &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "视频执行计划的输入契约已损坏", Retryable: false}}
	}
	if err := json.Unmarshal(dialogueCues, &segment.DialogueCues); err != nil {
		return nil, &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "视频执行计划的台词契约已损坏", Retryable: false}}
	}
	if time.Now().UTC().After(expiresAt) {
		return nil, &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "video execution plan expired before task creation", Retryable: false}}
	}
	if hash != segment.CapabilitySnapshotHash {
		return nil, &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "video capability snapshot hash does not match the execution plan", Retryable: false}}
	}
	if strings.TrimSpace(req.StoryboardShotID) == "" || req.StoryboardShotID != segment.StoryboardShotID {
		return nil, &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "storyboardShotId does not match the execution plan", Retryable: false}}
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
	account, err := s.GetAccount(ctx, req.OrganizationID, model.ProviderAccountID)
	if err != nil {
		return nil, err
	}
	if _, err := s.validateVideoInputContractsAdapterFixture(ctx, account, model, []VideoInputContract{segment.InputContract}); err != nil {
		return nil, err
	}
	if err := validateVideoCreateProductionContract(*req, input, segment); err != nil {
		return nil, err
	}
	if err := validateGatewayVideoReferenceManifest(*req, segment); err != nil {
		return nil, err
	}
	if err := s.validateGatewayVideoReferenceManifestSources(ctx, *req, segment); err != nil {
		return nil, err
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
		var previousSegmentID, previousArtifact string
		if err := s.db.QueryRow(ctx, `
			SELECT id::text, COALESCE(artifact_id::text, '')
			FROM video_render_segments
			WHERE video_render_plan_id = $1 AND segment_index = $2 AND status = 'succeeded'
		`, segment.ExecutionPlanID, segment.SegmentIndex-1).Scan(&previousSegmentID, &previousArtifact); err != nil {
			return nil, &StandardErrorError{Standard: StandardError{Code: CodeInvalidRequest, Message: "同镜头续接片段缺少已成功的前一片段", Retryable: false}}
		}
		switch segment.InputContractKey {
		case VideoInputContractVideoExtension:
			if len(req.References) != 1 || gatewayVideoReferenceRole(req.References[0]) != "video_extension_source" || strings.TrimSpace(req.References[0].ArtifactID) != previousArtifact {
				return nil, &StandardErrorError{Standard: StandardError{Code: CodeModelInputContractUnsupported, Message: "视频延长必须且只能使用同镜头前一成功片段", Retryable: false}}
			}
		case VideoInputContractFirstFrame, VideoInputContractFirstFramePlusReferences:
			if len(req.References) == 0 || gatewayVideoReferenceRole(req.References[0]) != "first_frame" {
				return nil, &StandardErrorError{Standard: StandardError{Code: CodeModelInputContractUnsupported, Message: "尾帧续接必须使用前一片段提取的 fresh 首帧输入", Retryable: false}}
			}
			if req.References[0].SourceType != "video_render_segment_tail_anchor" || req.References[0].SourceVersion != previousSegmentID {
				return nil, &StandardErrorError{Standard: StandardError{Code: CodeModelInputContractUnsupported, Message: "尾帧续接来源版本与前一片段不一致", Retryable: false}}
			}
			if segment.InputContractKey == VideoInputContractFirstFrame && len(req.References) != 1 {
				return nil, &StandardErrorError{Standard: StandardError{Code: CodeModelInputContractUnsupported, Message: "first_frame 续接只能携带一个 fresh 首帧", Retryable: false}}
			}
			var valid bool
			if err := s.db.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1
					FROM shot_visual_anchors anchor
					JOIN artifacts artifact ON artifact.id = anchor.artifact_id
					WHERE anchor.storyboard_shot_id = $1
					  AND anchor.source_render_segment_id = $2
					  AND anchor.source_video_artifact_id = $3
					  AND anchor.artifact_id = $4
					  AND anchor.id::text = $5
					  AND lower(COALESCE(artifact.content_hash, '')) = $6
					  AND anchor.created_at = $7::timestamptz
					  AND anchor.anchor_role = 'observed_tail_frame'
					  AND anchor.source_role = 'previous_segment_tail'
					  AND anchor.status = 'ready' AND anchor.review_status = 'approved'
				)
			`, req.StoryboardShotID, previousSegmentID, previousArtifact, req.References[0].ArtifactID,
				req.References[0].SourceID, cleanVideoContractHash(req.References[0].ContentHash), req.References[0].GeneratedAt).Scan(&valid); err != nil || !valid {
				return nil, &StandardErrorError{Standard: StandardError{Code: CodeModelInputContractUnsupported, Message: "尾帧续接引用不是前一片段的 fresh 尾帧", Retryable: false}}
			}
		default:
			return nil, &StandardErrorError{Standard: StandardError{Code: CodeModelInputContractUnsupported, Message: "当前模型没有可执行的同镜头续接契约", Retryable: false}}
		}
	}
	if strings.TrimSpace(req.ProviderModelID) != "" && req.ProviderModelID != segment.ProviderModelID {
		return nil, &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "providerModelId does not match the execution plan", Retryable: false}}
	}
	req.ProviderModelID = segment.ProviderModelID
	req.ModelProfileKey = segment.ModelProfileKey
	return &segment, nil
}

func validateVideoCreateProductionContract(req GatewayVideoCreateTaskRequest, input gatewayVideoInput, segment videoExecutionSegment) error {
	checks := []struct {
		name     string
		actual   string
		expected string
	}{
		{"organizationId", req.OrganizationID, segment.OrganizationID},
		{"projectId", req.ProjectID, segment.ProjectID},
		{"productionGenerationId", req.ProductionGenerationID, segment.ProductionGenerationID},
		{"videoProductionBindingId", req.VideoProductionBindingID, segment.VideoProductionBindingID},
		{"productionProfileVersionId", req.ProductionProfileVersionID, segment.ProductionProfileVersionID},
		{"productionProfileSnapshotHash", cleanVideoContractHash(req.ProductionProfileSnapshotHash), cleanVideoContractHash(segment.ProductionProfileSnapshotHash)},
		{"inputContractKey", req.InputContractKey, segment.InputContractKey},
		{"inputContractHash", cleanVideoContractHash(req.InputContractHash), cleanVideoContractHash(segment.InputContractHash)},
		{"inputContractVersion", req.InputContractVersion, segment.InputContractVersion},
		{"shotStateHash", cleanVideoContractHash(req.ShotStateHash), cleanVideoContractHash(segment.ShotStateHash)},
		{"referencePackId", req.ReferencePackID, segment.ReferencePackID},
		{"referencePackHash", cleanVideoContractHash(req.ReferencePackHash), cleanVideoContractHash(segment.ReferencePackHash)},
		{"promptContextPlanId", req.PromptContextPlanID, segment.PromptContextPlanID},
		{"promptContextPlanHash", cleanVideoContractHash(req.PromptContextPlanHash), cleanVideoContractHash(segment.PromptContextPlanHash)},
		{"videoPromptPlanId", req.VideoPromptPlanID, segment.VideoPromptPlanID},
	}
	for _, check := range checks {
		if strings.TrimSpace(check.actual) == "" || !strings.EqualFold(strings.TrimSpace(check.actual), strings.TrimSpace(check.expected)) {
			return &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "视频创建契约字段不一致：" + check.name, Retryable: false}}
		}
	}
	actualTransition := cleanVideoContractHash(req.TransitionHash)
	expectedTransition := cleanVideoContractHash(segment.TransitionHash)
	if (expectedTransition != "" && actualTransition != expectedTransition) || (expectedTransition == "" && actualTransition != "") {
		return &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "视频创建契约字段不一致：transitionHash", Retryable: false}}
	}
	if req.ShotStateRevision != segment.ShotStateRevision {
		return &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "镜头状态版本已变化，请重新规划视频", Retryable: false}}
	}
	if req.VideoProductionBindingRevision <= 0 || req.VideoProductionBindingRevision != segment.VideoProductionBindingRevision {
		return &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "视频生产配置版本与 Render Plan 不一致", Retryable: false}}
	}
	if req.NativeAudioRequired != segment.NativeAudioRequired {
		return &StandardErrorError{Standard: StandardError{Code: CodeVideoDialogueContractViolation, Message: "原生音频要求与 Render Plan 不一致", Retryable: false}}
	}
	if strings.TrimSpace(segment.Prompt) == "" {
		return &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "已审核视频执行提示词为空，请重新生成视频提示词", Retryable: false}}
	}
	if strings.TrimSpace(segment.PromptHash) == "" || videoPromptTextHash(segment.Prompt) != segment.PromptHash {
		return &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "已审核视频执行提示词的完整性校验失败，请重新生成视频提示词", Retryable: false}}
	}
	if input.Prompt != segment.Prompt {
		return &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "实际发送的视频提示词文本与已审核版本不一致", Retryable: false}}
	}
	if videoPromptTextHash(input.Prompt) != segment.PromptHash || req.PromptHash != segment.PromptHash {
		return &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "实际发送的视频提示词哈希与已审核版本不一致", Retryable: false}}
	}
	if !sameGatewayVideoDialogueCues(req.DialogueCues, segment.DialogueCues) {
		return &StandardErrorError{Standard: StandardError{Code: CodeVideoDialogueContractViolation, Message: "实际发送的逐段台词与 Render Plan 不一致", Retryable: false}}
	}
	for _, cue := range segment.DialogueCues {
		if !strings.Contains(input.Prompt, strings.TrimSpace(cue.Text)) {
			return &StandardErrorError{Standard: StandardError{Code: CodeVideoDialogueContractViolation, Message: "实际发送的视频提示词丢失中文台词：" + cue.Text, Retryable: false}}
		}
	}
	if err := validateGatewayVideoReferencesForContract(req.References, segment.InputContract); err != nil {
		return err
	}
	return nil
}

func validateGatewayVideoReferencesForContract(references []GatewayVideoReference, contract VideoInputContract) error {
	declared := make(map[string]VideoInputSlot, len(contract.Slots))
	for _, slot := range contract.Slots {
		declared[strings.ToLower(strings.TrimSpace(slot.Role))] = slot
	}
	counts := make(map[string]int)
	for _, reference := range references {
		role := gatewayVideoReferenceRole(reference)
		if role == "" {
			return &StandardErrorError{Standard: StandardError{Code: CodeModelInputContractUnsupported, Message: "视频参考输入缺少 canonical role", Retryable: false}}
		}
		mediaType := gatewayVideoReferenceMediaType(reference)
		slotRole := videoReferenceInputSlotRole(role, mediaType, declared)
		slot, ok := declared[slotRole]
		if !ok {
			return &StandardErrorError{Standard: StandardError{Code: CodeModelInputContractUnsupported, Message: fmt.Sprintf("输入契约 %s 不支持 %s", contract.ContractKey, role), Retryable: false}}
		}
		if mediaType == "" || !strings.EqualFold(mediaType, slot.MediaType) {
			return &StandardErrorError{Standard: StandardError{Code: CodeModelInputContractUnsupported, Message: fmt.Sprintf("输入 %s 的媒体类型必须为 %s", role, slot.MediaType), Retryable: false}}
		}
		counts[slotRole]++
	}
	for role, count := range counts {
		slot, ok := declared[role]
		if !ok || count > slot.Max {
			return &StandardErrorError{Standard: StandardError{Code: CodeModelInputContractUnsupported, Message: fmt.Sprintf("输入契约 %s 不支持 %s x%d", contract.ContractKey, role, count), Retryable: false}}
		}
	}
	for role, slot := range declared {
		if counts[role] < slot.Min {
			return &StandardErrorError{Standard: StandardError{Code: CodeModelInputContractUnsupported, Message: fmt.Sprintf("输入契约 %s 缺少必需输入 %s", contract.ContractKey, role), Retryable: false}}
		}
	}
	for _, group := range contract.MutuallyExclusiveRoles {
		present := 0
		for _, role := range group {
			if counts[strings.ToLower(strings.TrimSpace(role))] > 0 {
				present++
			}
		}
		if present > 1 {
			return &StandardErrorError{Standard: StandardError{Code: CodeModelInputContractUnsupported, Message: "视频请求包含互斥的参考输入角色", Retryable: false}}
		}
	}
	return nil
}

func videoReferenceInputSlotRole(role, mediaType string, declared map[string]VideoInputSlot) string {
	role = strings.ToLower(strings.TrimSpace(role))
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if _, ok := declared[role]; ok {
		return role
	}
	if mediaType == "image" {
		switch role {
		case "character_identity", "character_costume", "scene_identity", "scene_spatial", "prop_identity", "continuity_hint", "style_reference", "motion_reference":
			if _, ok := declared["semantic_reference"]; ok {
				return "semantic_reference"
			}
		}
	}
	if mediaType == "video" && role == "motion_reference" {
		if _, ok := declared["video_reference"]; ok {
			return "video_reference"
		}
	}
	return role
}

func gatewayVideoReferenceMediaType(reference GatewayVideoReference) string {
	mimeType := strings.ToLower(strings.TrimSpace(reference.MimeType))
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	case strings.HasPrefix(mimeType, "audio/"):
		return "audio"
	}
	typeName := strings.ToLower(strings.TrimSpace(reference.Type))
	switch {
	case typeName == "image" || strings.Contains(typeName, "image") || strings.Contains(typeName, "frame"):
		return "image"
	case typeName == "video" || strings.Contains(typeName, "video"):
		return "video"
	case typeName == "audio" || strings.Contains(typeName, "audio"):
		return "audio"
	default:
		return ""
	}
}

func gatewayVideoReferenceRole(reference GatewayVideoReference) string {
	role := strings.ToLower(strings.TrimSpace(reference.Role))
	if role != "" {
		return role
	}
	role = strings.ToLower(strings.TrimSpace(reference.Type))
	switch role {
	case "image":
		return "first_frame"
	case "video":
		return "video_reference"
	default:
		return role
	}
}

func videoPromptTextHash(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return "sha256:" + hex.EncodeToString(sum[:])
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
