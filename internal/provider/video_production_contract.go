package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type videoPlanProductionContract struct {
	ProfileVersionID                  string
	ProfileSnapshot                   json.RawMessage
	ProfileSnapshotHash               string
	CompatibilityPolicy               string
	RequiredInitialInputContract      string
	AllowedContinuationInputContracts []string
	InputContractVersion              string
	ShotStateRevision                 int
	ShotStateHash                     string
	TransitionSnapshot                json.RawMessage
	TransitionHash                    string
	ReferencePackID                   string
	ReferencePackHash                 string
	PromptContextPlanID               string
	PromptContextPlanHash             string
	VideoPromptPlanID                 string
	VideoPromptHash                   string
	NativeAudioRequired               bool
	AudioStrategy                     string
	AudioRequirement                  string
	DialogueCues                      []GatewayVideoDialogueSpan
}

type videoProductionContractQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (s *Service) loadVideoPlanProductionContract(ctx context.Context, db videoProductionContractQueryer, req GatewayVideoPlanRequest) (videoPlanProductionContract, error) {
	var contract videoPlanProductionContract
	var allowedContinuationContracts []byte
	err := db.QueryRow(ctx, `
		SELECT binding.profile_version_id::text, binding.profile_snapshot,
		       binding.profile_snapshot_hash, binding.compatibility_policy,
		       COALESCE(version.capability_requirements->>'initialInputContract', version.capability_requirements->>'inputContract', ''),
		       COALESCE(version.capability_requirements->'allowedContinuationInputContracts', '[]'::jsonb),
		       version.input_contract_version,
		       state.revision, state.state_hash,
		       jsonb_build_object(
		         'transitionType', transition.transition_type,
		         'carry', transition.carry_constraints,
		         'reset', transition.reset_constraints,
		         'tailPolicy', transition.tail_policy,
		         'anchorPolicy', transition.anchor_policy,
		         'confidence', transition.confidence
		       ),
		       COALESCE(transition.metadata->>'transitionHash', ''),
		       reference_pack.id::text, reference_pack.manifest_hash,
		       context_plan.id::text, context_plan.plan_hash,
		       prompt_plan.id::text, prompt_plan.prompt_hash,
		       audio_contract.native_audio_required,
		       audio_contract.audio_strategy, audio_contract.audio_requirement
		FROM projects project
		JOIN project_video_production_generations generation
		  ON generation.id = project.active_video_production_generation_id
		 AND generation.project_id = project.id AND generation.status = 'active'
		JOIN project_video_production_bindings binding
		  ON binding.id = generation.binding_id AND binding.project_id = project.id
		 AND binding.status = 'active'
		JOIN video_production_profile_versions version ON version.id = binding.profile_version_id
		JOIN storyboard_shots shot
		  ON shot.id = $3 AND shot.project_id = project.id
		 AND shot.production_generation_id = generation.id AND shot.deleted_at IS NULL
		JOIN storyboard_shot_state_versions state
		  ON state.storyboard_shot_id = shot.id
		 AND state.production_generation_id = generation.id
		 AND state.state_role = 'planned_entry' AND state.status = 'approved'
		JOIN storyboard_shot_transitions transition
		  ON transition.target_shot_id = shot.id
		 AND transition.production_generation_id = generation.id
		 AND transition.status = 'active' AND transition.review_status = 'approved'
		JOIN shot_reference_packs reference_pack
		  ON reference_pack.storyboard_shot_id = shot.id
		 AND reference_pack.production_generation_id = generation.id
		 AND reference_pack.status = 'active'
		 AND reference_pack.manifest->>'purpose' = 'video'
		JOIN prompt_context_plans context_plan
		  ON context_plan.storyboard_shot_id = shot.id
		 AND context_plan.production_generation_id = generation.id
		 AND context_plan.status = 'active'
		JOIN video_prompt_plans prompt_plan
		  ON prompt_plan.storyboard_shot_id = shot.id
		 AND prompt_plan.production_generation_id = generation.id
		 AND prompt_plan.status = 'approved'
		 AND prompt_plan.prompt_context_plan_id = context_plan.id
		JOIN video_native_audio_contracts audio_contract
		  ON audio_contract.storyboard_shot_id = shot.id
		 AND audio_contract.production_generation_id = generation.id
		 AND audio_contract.video_prompt_plan_id = prompt_plan.id
		 AND audio_contract.status = 'active'
		WHERE project.id = $1 AND project.organization_id = $2
		  AND generation.id = NULLIF($4, '')::uuid
		  AND binding.id = NULLIF($5, '')::uuid
		  AND binding.revision = $6
		  AND reference_pack.profile_snapshot_hash = binding.profile_snapshot_hash
		  AND reference_pack.shot_state_hash = state.state_hash
		  AND context_plan.video_production_binding_id = binding.id
		  AND context_plan.video_production_binding_revision = binding.revision
		  AND prompt_plan.profile_version_id = binding.profile_version_id
		  AND prompt_plan.video_production_binding_id = binding.id
		  AND prompt_plan.video_production_binding_revision = binding.revision
		  AND prompt_plan.prompt_context_plan_hash = context_plan.plan_hash
		  AND prompt_plan.profile_snapshot_hash = binding.profile_snapshot_hash
		  AND prompt_plan.shot_state_hash = state.state_hash
		  AND prompt_plan.reference_pack_hash = reference_pack.manifest_hash
		  AND prompt_plan.input_contract_version = version.input_contract_version
	`, req.ProjectID, req.OrganizationID, req.StoryboardShotID,
		req.ProductionGenerationID, req.VideoProductionBindingID, req.VideoProductionBindingRevision).Scan(
		&contract.ProfileVersionID, &contract.ProfileSnapshot,
		&contract.ProfileSnapshotHash, &contract.CompatibilityPolicy,
		&contract.RequiredInitialInputContract, &allowedContinuationContracts, &contract.InputContractVersion,
		&contract.ShotStateRevision, &contract.ShotStateHash,
		&contract.TransitionSnapshot, &contract.TransitionHash,
		&contract.ReferencePackID, &contract.ReferencePackHash,
		&contract.PromptContextPlanID, &contract.PromptContextPlanHash,
		&contract.VideoPromptPlanID, &contract.VideoPromptHash,
		&contract.NativeAudioRequired, &contract.AudioStrategy, &contract.AudioRequirement,
	)
	if err != nil {
		return videoPlanProductionContract{}, &StandardErrorError{Standard: StandardError{
			Code: CodeRenderPlanReplanRequired, Message: "视频生产契约已变化或尚未完成审核，请重新生成视频提示词", Retryable: false,
		}}
	}
	if err := json.Unmarshal(allowedContinuationContracts, &contract.AllowedContinuationInputContracts); err != nil {
		return videoPlanProductionContract{}, &StandardErrorError{Standard: StandardError{
			Code: CodeProductionProfileIncompatible, Message: "项目视频生产方案的续接输入契约无效", Retryable: false,
		}}
	}
	contract.AllowedContinuationInputContracts = normalizeVideoStringSlice(contract.AllowedContinuationInputContracts)
	rows, err := db.Query(ctx, `
		SELECT COALESCE(timing_unit_id, ''), speaker, dialogue_text, COALESCE(delivery, ''), kind,
		       start_tick, end_tick, continues_from_previous, continues_to_next
		FROM video_prompt_plan_dialogue_cues
		WHERE video_prompt_plan_id = $1
		ORDER BY ordinal
	`, contract.VideoPromptPlanID)
	if err != nil {
		return videoPlanProductionContract{}, err
	}
	defer rows.Close()
	contract.DialogueCues = make([]GatewayVideoDialogueSpan, 0)
	for rows.Next() {
		var cue GatewayVideoDialogueSpan
		if err := rows.Scan(
			&cue.TimingUnitID, &cue.Speaker, &cue.Text, &cue.Delivery, &cue.Kind,
			&cue.StartTick, &cue.EndTick, &cue.ContinuesFromPrevious, &cue.ContinuesToNext,
		); err != nil {
			return videoPlanProductionContract{}, err
		}
		contract.DialogueCues = append(contract.DialogueCues, cue)
	}
	if err := rows.Err(); err != nil {
		return videoPlanProductionContract{}, err
	}
	if contract.RequiredInitialInputContract == "" || contract.InputContractVersion == "" {
		return videoPlanProductionContract{}, &StandardErrorError{Standard: StandardError{
			Code: CodeProductionProfileIncompatible, Message: "项目视频生产方案缺少输入契约", Retryable: false,
		}}
	}
	return contract, nil
}

func validateVideoPlanContractRequest(req *GatewayVideoPlanRequest, contract videoPlanProductionContract) error {
	if req == nil {
		return fmt.Errorf("%w: video plan request is required", ErrValidation)
	}
	checks := []struct {
		name     string
		actual   string
		expected string
	}{
		{"productionProfileVersionId", req.ProductionProfileVersionID, contract.ProfileVersionID},
		{"productionProfileSnapshotHash", cleanVideoContractHash(req.ProductionProfileSnapshotHash), cleanVideoContractHash(contract.ProfileSnapshotHash)},
		{"compatibilityPolicy", req.CompatibilityPolicy, contract.CompatibilityPolicy},
		{"requiredInitialInputContract", req.RequiredInitialInputContract, contract.RequiredInitialInputContract},
		{"inputContractVersion", req.InputContractVersion, contract.InputContractVersion},
		{"shotStateHash", cleanVideoContractHash(req.ShotStateHash), cleanVideoContractHash(contract.ShotStateHash)},
		{"transitionHash", cleanVideoContractHash(req.TransitionHash), cleanVideoContractHash(contract.TransitionHash)},
		{"referencePackId", req.ReferencePackID, contract.ReferencePackID},
		{"referencePackHash", cleanVideoContractHash(req.ReferencePackHash), cleanVideoContractHash(contract.ReferencePackHash)},
		{"promptContextPlanId", req.PromptContextPlanID, contract.PromptContextPlanID},
		{"promptContextPlanHash", cleanVideoContractHash(req.PromptContextPlanHash), cleanVideoContractHash(contract.PromptContextPlanHash)},
		{"videoPromptPlanId", req.VideoPromptPlanID, contract.VideoPromptPlanID},
		{"audioStrategy", req.AudioStrategy, contract.AudioStrategy},
		{"audioRequirement", req.AudioRequirement, contract.AudioRequirement},
	}
	for _, check := range checks {
		if strings.TrimSpace(check.actual) == "" || !strings.EqualFold(strings.TrimSpace(check.actual), strings.TrimSpace(check.expected)) {
			return &StandardErrorError{Standard: StandardError{
				Code: CodeRenderPlanReplanRequired, Message: "视频生产契约字段不一致：" + check.name, Retryable: false,
			}}
		}
	}
	if req.ShotStateRevision != contract.ShotStateRevision {
		return &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "镜头状态版本已变化，请重新生成视频提示词", Retryable: false}}
	}
	if !sameNormalizedVideoStringSlice(req.AllowedContinuationInputContracts, contract.AllowedContinuationInputContracts) {
		return &StandardErrorError{Standard: StandardError{Code: CodeRenderPlanReplanRequired, Message: "视频生产方案的续接输入契约已变化", Retryable: false}}
	}
	if req.NativeAudioRequired != contract.NativeAudioRequired {
		return &StandardErrorError{Standard: StandardError{Code: CodeVideoDialogueContractViolation, Message: "原生音频要求与已审核视频提示词不一致", Retryable: false}}
	}
	if !sameGatewayVideoDialogueCues(req.DialogueSpans, contract.DialogueCues) {
		return &StandardErrorError{Standard: StandardError{Code: CodeVideoDialogueContractViolation, Message: "视频规划请求中的对白与已审核中文台词不一致", Retryable: false}}
	}
	req.validatedContract = &contract
	return nil
}

func sameNormalizedVideoStringSlice(actual, expected []string) bool {
	left := normalizeVideoStringSlice(actual)
	right := normalizeVideoStringSlice(expected)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sameGatewayVideoDialogueCues(actual, expected []GatewayVideoDialogueSpan) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range actual {
		left, right := actual[index], expected[index]
		if strings.TrimSpace(left.TimingUnitID) != strings.TrimSpace(right.TimingUnitID) ||
			strings.TrimSpace(left.Speaker) != strings.TrimSpace(right.Speaker) ||
			strings.TrimSpace(left.Text) != strings.TrimSpace(right.Text) ||
			strings.TrimSpace(left.Delivery) != strings.TrimSpace(right.Delivery) ||
			normalizeVideoDialogueCueKind(left.Kind) != normalizeVideoDialogueCueKind(right.Kind) ||
			left.StartTick != right.StartTick || left.EndTick != right.EndTick ||
			left.ContinuesFromPrevious != right.ContinuesFromPrevious ||
			left.ContinuesToNext != right.ContinuesToNext {
			return false
		}
	}
	return true
}

func normalizeVideoDialogueCueKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "voiceover", "narration", "system":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "dialogue"
	}
}

func videoInputContractHash(contract VideoInputContract) (string, error) {
	raw, err := json.Marshal(contract)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func cleanVideoContractHash(value string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "sha256:")
}
