package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/commerce"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videoproduction"
)

type approvedShotVideoExecutionContract struct {
	ProductionProfileVersionID    string
	ProductionProfileSnapshotHash string
	CompatibilityPolicy           string
	RequiredInitialInputContract  string
	InputContractVersion          string
	ShotStateRevision             int
	ShotStateHash                 string
	TransitionHash                string
	ReferencePackID               string
	ReferencePackHash             string
	PromptContextPlanID           string
	PromptContextPlanHash         string
	VideoPromptPlanID             string
	VideoPromptHash               string
	Prompt                        string
	NativeAudioRequired           bool
	AudioStrategy                 string
	AudioRequirement              string
	DialogueCues                  []provider.GatewayVideoDialogueSpan
	References                    []provider.GatewayVideoReference
}

func (a Activities) materializeApprovedVideoPromptPlan(
	ctx context.Context,
	input EnsurePreparedShotVideoPlanInput,
	plan PlanShotVideoOutput,
) error {
	project, err := a.projectProductionSettings(ctx, input.ProjectID, input.WorkflowRunID)
	if err != nil {
		return err
	}
	shot, err := a.storyboardShot(ctx, input.ProjectID, input.WorkflowRunID, input.ShotID, input.ShotIndex)
	if err != nil {
		return err
	}
	contract, err := a.loadApprovedShotVideoExecutionContract(ctx, input.OrganizationID, project, shot)
	if err != nil {
		return err
	}
	if plan.VideoPromptPlanID != contract.VideoPromptPlanID || plan.PromptContextPlanID != contract.PromptContextPlanID || plan.ReferencePackID != contract.ReferencePackID {
		return preparedVideoPromptError("视频执行计划与已审核提示词契约不一致，请重新规划")
	}
	execution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKeyForID("video_prompt_plan_materialize_v2", plan.ExecutionPlanID),
		NodeType:       "video.prompt.plan_materialize",
		Input: mustJSON(map[string]any{
			"shotId": input.ShotID, "executionPlanId": plan.ExecutionPlanID,
			"videoPromptPlanId": contract.VideoPromptPlanID,
		}),
	})
	if err != nil {
		return err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `
		SELECT segment.id::text, segment.segment_index, segment.dialogue
		FROM video_render_segments segment
		JOIN video_render_plans plan ON plan.id = segment.video_render_plan_id
		WHERE segment.video_render_plan_id = $1 AND segment.storyboard_shot_id = $2
		  AND segment.project_id = $3 AND segment.production_generation_id = $4
		  AND plan.video_prompt_plan_id = $5 AND plan.active = true
		ORDER BY segment.segment_index
		FOR UPDATE OF segment
	`, plan.ExecutionPlanID, shot.ID, input.ProjectID, project.ProductionGenerationID, contract.VideoPromptPlanID)
	if err != nil {
		return err
	}
	type segmentRow struct {
		ID       string
		Index    int
		Dialogue []provider.GatewayVideoDialogueSpan
	}
	segments := make([]segmentRow, 0)
	for rows.Next() {
		var segment segmentRow
		var rawDialogue json.RawMessage
		if err := rows.Scan(&segment.ID, &segment.Index, &rawDialogue); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal(rawDialogue, &segment.Dialogue); err != nil {
			rows.Close()
			return err
		}
		segments = append(segments, segment)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(segments) == 0 {
		return preparedVideoPromptError("视频执行计划没有可执行片段")
	}
	for _, segment := range segments {
		lines := make([]StoryboardDialogueLine, 0, len(segment.Dialogue))
		for _, cue := range segment.Dialogue {
			lines = append(lines, StoryboardDialogueLine{
				TimingUnitID: cue.TimingUnitID, Speaker: cue.Speaker, Text: cue.Text,
				Delivery: cue.Delivery, Kind: cue.Kind,
				SpanStartTick: cue.StartTick, SpanEndTick: cue.EndTick,
				ContinuesFromPrevious: cue.ContinuesFromPrevious, ContinuesToNext: cue.ContinuesToNext,
			})
		}
		prompt := composeAuthoritativeVideoPrompt(stripAuthoritativeVideoPromptAudio(contract.Prompt), lines)
		promptHash := promptsvc.HashText(prompt)
		metadata := mustJSON(map[string]any{
			"status": "approved", "promptHash": promptHash,
			"promptSource":         "approved_video_prompt_plan",
			"videoPromptPlanId":    contract.VideoPromptPlanID,
			"promptContextPlanId":  contract.PromptContextPlanID,
			"referencePackId":      contract.ReferencePackID,
			"segmentDialogueCount": len(lines),
		})
		if _, err := tx.Exec(ctx, `
			UPDATE video_render_segments
			SET prompt = $2,
			    execution_prompt_hash = $3,
			    metadata = COALESCE(metadata, '{}'::jsonb)
			      || jsonb_build_object('promptStatus', 'succeeded', 'promptCompletedAt', now())
			      || jsonb_build_object('videoPromptAgent', $4::jsonb),
			    error_code = NULL, error_message = NULL, updated_at = now()
			WHERE id = $1 AND video_render_plan_id = $5 AND project_id = $6
		`, segment.ID, prompt, promptHash, metadata, plan.ExecutionPlanID, input.ProjectID); err != nil {
			return err
		}
	}
	promptWorkflowRunID := input.WorkflowRunID
	if err := a.finalizeShotVideoPromptPlanTx(ctx, tx, FinalizeShotVideoPromptPlanInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID,
		WorkflowRunID: input.WorkflowRunID, ShotID: shot.ID,
		ExecutionPlanID: plan.ExecutionPlanID, PromptWorkflowRunID: &promptWorkflowRunID,
		PromptSource: "approved_video_prompt_plan",
	}); err != nil {
		return err
	}
	if applied, err := completeNodeRunTx(ctx, tx, execution, mustJSON(map[string]any{
		"shotId": shot.ID, "executionPlanId": plan.ExecutionPlanID,
		"videoPromptPlanId": contract.VideoPromptPlanID, "status": "ready",
	})); err != nil {
		return err
	} else if !applied {
		return ErrWorkflowWriteFenced
	}
	return tx.Commit(ctx)
}

func (a Activities) loadApprovedShotVideoExecutionContract(
	ctx context.Context,
	organizationID string,
	project ProjectProductionSettings,
	shot StoryboardShotRecord,
) (approvedShotVideoExecutionContract, error) {
	var contract approvedShotVideoExecutionContract
	var storedDialogue json.RawMessage
	var projectKind string
	if err := a.db.QueryRow(ctx, `
		SELECT project_kind FROM projects
		WHERE id = $1 AND organization_id = $2
	`, project.ID, organizationID).Scan(&projectKind); err != nil {
		return approvedShotVideoExecutionContract{}, err
	}
	query := `
		SELECT prompt.profile_version_id::text, prompt.profile_snapshot_hash,
		       binding.compatibility_policy,
		       COALESCE(
		           version.capability_requirements->>'initialInputContract',
		           version.capability_requirements->>'inputContract',
		           ''
		       ),
		       prompt.input_contract_version,
		       state.revision, state.state_hash,
		       COALESCE(transition.metadata->>'transitionHash', ''),
		       reference_pack.id::text, reference_pack.manifest_hash,
		       context_plan.id::text, context_plan.plan_hash,
		       prompt.id::text, prompt.prompt_hash, prompt.rendered_prompt,
		       audio.native_audio_required, audio.audio_strategy, audio.audio_requirement,
		       prompt.dialogue_cues
		FROM video_prompt_plans prompt
		JOIN project_video_production_bindings binding
		  ON binding.id = prompt.video_production_binding_id AND binding.status = 'active'
		JOIN video_production_profile_versions version ON version.id = prompt.profile_version_id
		JOIN storyboard_shot_state_versions state
		  ON state.storyboard_shot_id = prompt.storyboard_shot_id
		 AND state.state_role = 'planned_entry' AND state.status = 'approved'
		JOIN storyboard_shot_transitions transition
		  ON transition.target_shot_id = prompt.storyboard_shot_id
		 AND transition.status = 'active' AND transition.review_status = 'approved'
		JOIN shot_reference_packs reference_pack
		  ON reference_pack.storyboard_shot_id = prompt.storyboard_shot_id
		 AND reference_pack.status = 'active' AND reference_pack.manifest->>'purpose' = 'video'
		JOIN prompt_context_plans context_plan ON context_plan.id = prompt.prompt_context_plan_id AND context_plan.status = 'active'
		JOIN video_native_audio_contracts audio
		  ON audio.video_prompt_plan_id = prompt.id AND audio.status = 'active'
		WHERE prompt.organization_id = $1 AND prompt.project_id = $2
		  AND prompt.storyboard_shot_id = $3 AND prompt.status = 'approved'
		  AND prompt.production_generation_id = $4
		  AND prompt.video_production_binding_id = $5
		  AND prompt.video_production_binding_revision = $6
		  AND prompt.profile_version_id = $7
		  AND prompt.profile_snapshot_hash = $8
		  AND prompt.shot_state_hash = state.state_hash
		  AND prompt.transition_hash = transition.metadata->>'transitionHash'
		  AND prompt.reference_pack_hash = reference_pack.manifest_hash
		  AND prompt.prompt_context_plan_hash = context_plan.plan_hash
		  AND prompt.input_contract_version = version.input_contract_version
	`
	if commerce.ProjectKind(projectKind).IsCommerce() {
		query = `
			SELECT prompt.profile_version_id::text, prompt.profile_snapshot_hash,
			       binding.compatibility_policy,
			       COALESCE(
			           version.capability_requirements->>'initialInputContract',
			           version.capability_requirements->>'inputContract',
			           ''
			       ),
			       prompt.input_contract_version,
			       state.revision, state.state_hash,
			       COALESCE(prompt.transition_hash, ''),
			       reference_pack.id::text, reference_pack.manifest_hash,
			       context_plan.id::text, context_plan.plan_hash,
			       prompt.id::text, prompt.prompt_hash, prompt.rendered_prompt,
			       audio.native_audio_required, audio.audio_strategy, audio.audio_requirement,
			       prompt.dialogue_cues
			FROM video_prompt_plans prompt
			JOIN project_video_production_bindings binding
			  ON binding.id = prompt.video_production_binding_id AND binding.status = 'active'
			JOIN video_production_profile_versions version ON version.id = prompt.profile_version_id
			JOIN storyboard_shots shot
			  ON shot.id = prompt.storyboard_shot_id
			 AND shot.commerce_storyboard_plan_id IS NOT NULL AND shot.deleted_at IS NULL
			JOIN storyboard_shot_state_versions state
			  ON state.storyboard_shot_id = prompt.storyboard_shot_id
			 AND state.state_role = 'planned_entry' AND state.status = 'approved'
			JOIN shot_reference_packs reference_pack
			  ON reference_pack.storyboard_shot_id = prompt.storyboard_shot_id
			 AND reference_pack.status = 'active' AND reference_pack.purpose = 'video'
			JOIN prompt_context_plans context_plan
			  ON context_plan.id = prompt.prompt_context_plan_id AND context_plan.status = 'active'
			JOIN video_native_audio_contracts audio
			  ON audio.video_prompt_plan_id = prompt.id AND audio.status = 'active'
			WHERE prompt.organization_id = $1 AND prompt.project_id = $2
			  AND prompt.storyboard_shot_id = $3 AND prompt.status = 'approved'
			  AND prompt.production_generation_id = $4
			  AND prompt.video_production_binding_id = $5
			  AND prompt.video_production_binding_revision = $6
			  AND prompt.profile_version_id = $7
			  AND prompt.profile_snapshot_hash = $8
			  AND prompt.commerce_script_unit_generation_id IS NOT NULL
			  AND prompt.shot_state_hash = state.state_hash
			  AND prompt.transition_hash IS NULL
			  AND prompt.reference_pack_hash = reference_pack.manifest_hash
			  AND prompt.prompt_context_plan_hash = context_plan.plan_hash
			  AND prompt.input_contract_version = version.input_contract_version
		`
	}
	err := a.db.QueryRow(ctx, query, organizationID, project.ID, shot.ID, project.ProductionGenerationID,
		project.VideoProductionBindingID, project.VideoProductionBindingRevision,
		project.VideoProductionProfileVersionID, cleanContractHash(project.VideoProductionProfileHash)).Scan(
		&contract.ProductionProfileVersionID, &contract.ProductionProfileSnapshotHash,
		&contract.CompatibilityPolicy, &contract.RequiredInitialInputContract,
		&contract.InputContractVersion, &contract.ShotStateRevision, &contract.ShotStateHash,
		&contract.TransitionHash, &contract.ReferencePackID, &contract.ReferencePackHash,
		&contract.PromptContextPlanID, &contract.PromptContextPlanHash,
		&contract.VideoPromptPlanID, &contract.VideoPromptHash, &contract.Prompt,
		&contract.NativeAudioRequired, &contract.AudioStrategy, &contract.AudioRequirement,
		&storedDialogue,
	)
	if err != nil {
		return approvedShotVideoExecutionContract{}, preparedVideoPromptError("没有可执行的已审核视频提示词契约，请先生成并审核视频提示词")
	}
	if contract.RequiredInitialInputContract == "" || contract.InputContractVersion == "" || strings.TrimSpace(contract.Prompt) == "" {
		return approvedShotVideoExecutionContract{}, preparedVideoPromptError("已审核视频提示词契约不完整，请重新生成视频提示词")
	}
	rows, err := a.db.Query(ctx, `
		SELECT COALESCE(cue.timing_unit_id, ''), cue.speaker, cue.dialogue_text, COALESCE(cue.delivery, ''), cue.kind,
		       cue.start_tick, cue.end_tick, cue.continues_from_previous, cue.continues_to_next
		FROM video_prompt_plan_dialogue_cues cue
		WHERE cue.video_prompt_plan_id = $1
		ORDER BY cue.ordinal
	`, contract.VideoPromptPlanID)
	if err != nil {
		return approvedShotVideoExecutionContract{}, err
	}
	contract.DialogueCues = make([]provider.GatewayVideoDialogueSpan, 0)
	for rows.Next() {
		var cue provider.GatewayVideoDialogueSpan
		if err := rows.Scan(
			&cue.TimingUnitID, &cue.Speaker, &cue.Text, &cue.Delivery, &cue.Kind,
			&cue.StartTick, &cue.EndTick, &cue.ContinuesFromPrevious, &cue.ContinuesToNext,
		); err != nil {
			rows.Close()
			return approvedShotVideoExecutionContract{}, err
		}
		contract.DialogueCues = append(contract.DialogueCues, cue)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return approvedShotVideoExecutionContract{}, err
	}
	rows.Close()

	rows, err = a.db.Query(ctx, `
		SELECT item.reference_key, item.role, item.required, item.priority,
		       item.source_type, COALESCE(item.source_id::text, ''),
		       item.media_type, item.semantics,
		       COALESCE(item.asset_id::text, ''), COALESCE(item.artifact_id::text, ''),
		       COALESCE(item.media_file_id::text, ''), COALESCE(item.storage_key, ''),
		       COALESCE(media.mime_type, ''), item.content_hash, item.metadata
		FROM shot_reference_pack_items item
		LEFT JOIN media_files media ON media.id = item.media_file_id
		WHERE item.reference_pack_id = $1
		ORDER BY item.required DESC, item.priority DESC, item.reference_key
	`, contract.ReferencePackID)
	if err != nil {
		return approvedShotVideoExecutionContract{}, err
	}
	contract.References = make([]provider.GatewayVideoReference, 0)
	items := make([]videoproduction.ReferencePackItem, 0)
	for rows.Next() {
		var reference provider.GatewayVideoReference
		var mediaType, semantics string
		if err := rows.Scan(
			&reference.ReferenceKey, &reference.Role, &reference.Required, &reference.Priority,
			&reference.SourceType, &reference.SourceID, &mediaType, &semantics,
			&reference.AssetID, &reference.ArtifactID, &reference.MediaFileID, &reference.StorageKey,
			&reference.MimeType, &reference.ContentHash, &reference.Metadata,
		); err != nil {
			rows.Close()
			return approvedShotVideoExecutionContract{}, err
		}
		reference.Type = mediaType
		reference.Semantics = semantics
		reference.SourceVersion = videoReferenceSourceVersion(reference.Metadata, reference.ContentHash)
		contract.References = append(contract.References, reference)
		items = append(items, videoproduction.ReferencePackItem{
			ReferenceKey: reference.ReferenceKey, Role: reference.Role, Required: reference.Required,
			Priority: reference.Priority, SourceType: reference.SourceType, SourceID: reference.SourceID,
			AssetID: reference.AssetID, ArtifactID: reference.ArtifactID, MediaFileID: reference.MediaFileID,
			StorageKey: reference.StorageKey, MediaType: mediaType, Semantics: semantics,
			ContentHash: reference.ContentHash,
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return approvedShotVideoExecutionContract{}, err
	}
	rows.Close()
	strategy, err := videoproduction.ProfileStrategyFor(project.VideoProductionProfileKey)
	if err != nil {
		return approvedShotVideoExecutionContract{}, err
	}
	if contract.RequiredInitialInputContract != strategy.InputAdapter().InitialContract() {
		return approvedShotVideoExecutionContract{}, preparedVideoPromptError("已审核视频提示词的输入契约与项目生产方案不一致")
	}
	for _, reference := range contract.References {
		if reference.ArtifactID == "" && reference.MediaFileID == "" && reference.StorageKey == "" {
			return approvedShotVideoExecutionContract{}, preparedVideoPromptError("视频参考包包含缺少媒体的引用")
		}
	}
	if err := strategy.References().Validate(videoproduction.ReferencePurposeVideo, items); err != nil {
		return approvedShotVideoExecutionContract{}, preparedVideoPromptError(err.Error())
	}
	if err := strategy.InputAdapter().ValidateReferenceRoles(items); err != nil {
		return approvedShotVideoExecutionContract{}, preparedVideoPromptError(err.Error())
	}
	if len(storedDialogue) > 0 && string(storedDialogue) != "null" {
		var source []StoryboardDialogueLine
		if err := json.Unmarshal(storedDialogue, &source); err != nil {
			return approvedShotVideoExecutionContract{}, fmt.Errorf("parse stored video dialogue: %w", err)
		}
		if len(source) != len(contract.DialogueCues) {
			return approvedShotVideoExecutionContract{}, preparedVideoPromptError("视频提示词台词契约已损坏，请重新生成视频提示词")
		}
	}
	return contract, nil
}

func videoReferenceSourceVersion(metadata json.RawMessage, contentHash string) string {
	var values map[string]any
	if len(metadata) > 0 && json.Unmarshal(metadata, &values) == nil {
		for _, key := range []string{"sourceVersion", "sourceVersionId"} {
			if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return strings.TrimSpace(contentHash)
}
