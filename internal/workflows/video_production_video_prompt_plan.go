package workflows

import (
	"context"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
)

func (a Activities) persistSingleFrameVideoPromptPlanTx(
	ctx context.Context,
	tx pgx.Tx,
	input PrepareShotVideoPromptInput,
	shot StoryboardShotRecord,
	project ProjectProductionSettings,
	contract shotProductionContractContext,
	nodeExecution NodeExecution,
	output PrepareShotVideoPromptOutput,
	review reviewedVideoPrompt,
	audioStrategy string,
	audioRequirement string,
) (string, error) {
	if output.GenerationContract == nil || output.ReviewContract == nil || output.DeterministicReview == nil {
		return "", workflowError{Code: videoproduction.CodePromptContractIncomplete, Message: "视频 Prompt Contract 或审核结果不完整"}
	}
	for field, value := range map[string]string{
		"promptContextPlanId": output.PromptContextPlanID,
		"referencePackId":     output.ReferencePackID,
		"profileVersionId":    project.VideoProductionProfileVersionID,
		"generationPrompt":    output.GenerationPromptVersion,
		"reviewPrompt":        output.ReviewPromptVersion,
	} {
		if strings.TrimSpace(value) == "" {
			return "", workflowError{Code: videoproduction.CodePromptContractIncomplete, Message: "视频提示词计划缺少 " + field}
		}
	}
	if strings.TrimSpace(audioStrategy) == "" || strings.TrimSpace(audioRequirement) == "" {
		return "", workflowError{Code: videoproduction.CodePromptContractIncomplete, Message: "视频原生音频策略不完整"}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE video_native_audio_contracts
		SET status = 'stale', stale_at = now()
		WHERE storyboard_shot_id = $1 AND status = 'active'
	`, shot.ID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE video_prompt_plans
		SET status = 'stale', stale_at = now()
		WHERE storyboard_shot_id = $1 AND status = 'approved'
	`, shot.ID); err != nil {
		return "", err
	}

	var revision int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(revision), 0) + 1
		FROM video_prompt_plans
		WHERE storyboard_shot_id = $1
	`, shot.ID).Scan(&revision); err != nil {
		return "", err
	}
	reviewerOutput := mustJSON(map[string]any{
		"agent":         review,
		"deterministic": output.DeterministicReview,
	})
	metadata := mustJSON(map[string]any{
		"generationContract": output.GenerationContract,
		"reviewContract":     output.ReviewContract,
		"promptMeasurements": output.PromptMeasurements,
		"modelCandidates":    output.ModelCandidates,
		"nonSpeechSoundCues": output.SoundCues,
	})
	localDialogue, err := shotLocalVideoDialogue(shot, 0, shot.PlannedDurationTicks, output.DialogueLines)
	if err != nil {
		return "", err
	}
	var planID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO video_prompt_plans(
			organization_id, project_id, production_generation_id,
			video_production_binding_id, video_production_binding_revision,
			profile_version_id, storyboard_shot_id, prompt_context_plan_id,
			prompt_version_id, reviewer_prompt_version_id,
			workflow_run_id, node_run_id, provider_call_id,
			reviewer_provider_call_id, provider_model_id,
			revision, status, rendered_prompt, prompt_hash,
			prompt_context_plan_hash, profile_snapshot_hash, shot_state_hash,
			transition_hash, reference_pack_hash, capability_snapshot_hash,
			input_contract_version, dialogue_cues, native_audio_required,
			audio_strategy, audio_requirement, reviewer_output, metadata,
			created_by, reviewed_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, NULLIF($10, '')::uuid,
			NULLIF($11, '')::uuid, NULLIF($12, '')::uuid,
			NULLIF($13, '')::uuid, NULLIF($14, '')::uuid, NULLIF($15, '')::uuid,
			$16, 'reviewing', $17, $18, $19, $20, $21,
			NULLIF($22, ''), $23, $24, $25, $26, $27, $28,
			$29, $30, $31, NULLIF($32, '')::uuid, now()
		)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, project.ProductionGenerationID,
		project.VideoProductionBindingID, project.VideoProductionBindingRevision,
		project.VideoProductionProfileVersionID, shot.ID, output.PromptContextPlanID,
		output.GenerationPromptVersion, output.ReviewPromptVersion,
		input.WorkflowRunID, nodeExecution.NodeRunID, output.GenerationProviderCallID,
		output.ReviewProviderCallID, output.GenerationModelID,
		revision, output.Prompt, cleanContractHash(output.PromptHash),
		cleanContractHash(output.PromptContextPlanHash), cleanContractHash(project.VideoProductionProfileHash), cleanContractHash(contract.EntryStateHash),
		cleanContractHash(contract.TransitionHash), cleanContractHash(output.ReferencePackHash), cleanContractHash(output.CapabilitySnapshotHash),
		project.VideoProductionInputContract, mustJSON(localDialogue), output.NativeAudioRequired,
		audioStrategy, audioRequirement, reviewerOutput, metadata, input.CreatedBy,
	).Scan(&planID); err != nil {
		return "", err
	}

	expectedDialogueDuration := int64(0)
	for ordinal, line := range localDialogue {
		if line.SpanStartTick < 0 || line.SpanEndTick <= line.SpanStartTick {
			return "", workflowError{
				Code:    videoproduction.CodePromptContractIncomplete,
				Message: fmt.Sprintf("第 %d 条视频台词缺少有效的帧对齐时间范围", ordinal+1),
			}
		}
		kind := normalizeVideoAudioCueKind(line.Kind)
		speaker := storedVideoAudioCueSpeaker(line.Speaker, kind)
		contentHash, err := videoproduction.HashCanonicalContract(map[string]any{
			"ordinal": ordinal, "timingUnitId": line.TimingUnitID, "speaker": speaker, "text": line.Text,
			"startTick": line.SpanStartTick, "endTick": line.SpanEndTick,
			"delivery": line.Delivery, "kind": kind,
			"continuesFromPrevious": line.ContinuesFromPrevious, "continuesToNext": line.ContinuesToNext,
		})
		if err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO video_prompt_plan_dialogue_cues(
				video_prompt_plan_id, ordinal, timing_unit_id, speaker, dialogue_text,
				start_tick, end_tick, language, delivery, kind,
				continues_from_previous, continues_to_next, required, content_hash
			)
			VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6, $7, 'zh-CN', NULLIF($8, ''), $9, $10, $11, true, $12)
		`, planID, ordinal, strings.TrimSpace(line.TimingUnitID), speaker, strings.TrimSpace(line.Text),
			line.SpanStartTick, line.SpanEndTick, strings.TrimSpace(line.Delivery), kind,
			line.ContinuesFromPrevious, line.ContinuesToNext, contentHash); err != nil {
			return "", err
		}
		if line.SpanEndTick > expectedDialogueDuration {
			expectedDialogueDuration = line.SpanEndTick
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE video_prompt_plans
		SET status = 'approved', approved_at = now()
		WHERE id = $1 AND status = 'reviewing'
	`, planID); err != nil {
		return "", err
	}

	dialogueHash, err := videoproduction.HashCanonicalContract(localDialogue)
	if err != nil {
		return "", err
	}
	capabilityRequirements := map[string]any{
		"nativeAudioRequired":      output.NativeAudioRequired,
		"modelSupportsNativeAudio": output.ModelSupportsNativeAudio,
		"requiresDialogue":         len(output.DialogueLines) > 0,
		"dialogueLanguage":         "zh-CN",
		"capabilitySnapshotHash":   cleanContractHash(output.CapabilitySnapshotHash),
	}
	audioContractValue := map[string]any{
		"videoPromptPlanId":             planID,
		"audioStrategy":                 audioStrategy,
		"audioRequirement":              audioRequirement,
		"nativeAudioRequired":           output.NativeAudioRequired,
		"dialogueLanguage":              "zh-CN",
		"dialogueCuesHash":              dialogueHash,
		"expectedDialogueDurationTicks": expectedDialogueDuration,
		"timelineTimebase":              project.TimelineTimebase,
		"capabilityRequirements":        capabilityRequirements,
	}
	audioContractHash, err := videoproduction.HashCanonicalContract(audioContractValue)
	if err != nil {
		return "", err
	}
	var audioRevision int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(revision), 0) + 1
		FROM video_native_audio_contracts
		WHERE storyboard_shot_id = $1
	`, shot.ID).Scan(&audioRevision); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO video_native_audio_contracts(
			organization_id, project_id, production_generation_id,
			storyboard_shot_id, video_prompt_plan_id, revision, status,
			audio_strategy, audio_requirement, native_audio_required,
			dialogue_language, dialogue_cues_hash, expected_dialogue_duration_ticks,
			timeline_timebase, capability_requirements, contract_hash
		)
		VALUES ($1, $2, $3, $4, $5, $6, 'active', $7, $8, $9,
		        'zh-CN', $10, $11, $12, $13, $14)
	`, input.OrganizationID, input.ProjectID, project.ProductionGenerationID,
		shot.ID, planID, audioRevision, audioStrategy, audioRequirement,
		output.NativeAudioRequired, dialogueHash, expectedDialogueDuration,
		project.TimelineTimebase, mustJSON(capabilityRequirements), audioContractHash); err != nil {
		return "", err
	}
	return planID, nil
}

func normalizeVideoAudioCueKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "voiceover", "narration", "system":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "dialogue"
	}
}

func storedVideoAudioCueSpeaker(value, kind string) string {
	if speaker := strings.TrimSpace(value); speaker != "" {
		return speaker
	}
	if normalizeVideoAudioCueKind(kind) == "system" {
		return "系统音频"
	}
	return "旁白"
}

type shotTimingWindowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func shotSourceTimingWindow(ctx context.Context, db shotTimingWindowQuerier, shotID string) (int64, int64, error) {
	var startTick, endTick int64
	var spanCount int
	if err := db.QueryRow(ctx, `
		SELECT COALESCE(MIN(span_start_tick), 0), COALESCE(MAX(span_end_tick), 0), COUNT(*)
		FROM storyboard_shot_timing_spans
		WHERE storyboard_shot_id = $1
	`, shotID).Scan(&startTick, &endTick, &spanCount); err != nil {
		return 0, 0, err
	}
	if spanCount == 0 || endTick <= startTick {
		return 0, 0, workflowError{
			Code:    videoproduction.CodePromptContractIncomplete,
			Message: "镜头缺少有效的剧本时间窗口，请重新生成分镜",
		}
	}
	return startTick, endTick, nil
}

func (a Activities) localizeStoryboardShotDialogue(ctx context.Context, shot StoryboardShotRecord) ([]StoryboardDialogueLine, error) {
	sourceWindowStart, sourceWindowEnd, err := shotSourceTimingWindow(ctx, a.db, shot.ID)
	if err != nil {
		return nil, err
	}
	return shotLocalVideoDialogue(shot, sourceWindowStart, sourceWindowEnd, shot.Dialogue)
}

func shotLocalVideoDialogue(shot StoryboardShotRecord, sourceWindowStart, sourceWindowEnd int64, lines []StoryboardDialogueLine) ([]StoryboardDialogueLine, error) {
	if sourceWindowStart < 0 || sourceWindowEnd <= sourceWindowStart {
		return nil, workflowError{
			Code:    videoproduction.CodePromptContractIncomplete,
			Message: "镜头缺少有效的剧本时间窗口，请重新生成分镜",
		}
	}
	sourceDuration := sourceWindowEnd - sourceWindowStart
	if shot.PlannedDurationTicks > 0 && sourceDuration != shot.PlannedDurationTicks {
		return nil, workflowError{
			Code:    videoproduction.CodePromptContractIncomplete,
			Message: "镜头时长与剧本时间窗口不一致，请重新生成分镜",
		}
	}
	lines = NormalizeStoryboardDialogue(lines)
	result := make([]StoryboardDialogueLine, 0, len(lines))
	for index, line := range lines {
		if line.SpanStartTick < sourceWindowStart || line.SpanEndTick > sourceWindowEnd || line.SpanEndTick <= line.SpanStartTick {
			return nil, workflowError{
				Code:    videoproduction.CodePromptContractIncomplete,
				Message: fmt.Sprintf("第 %d 条视频台词不在当前镜头对应的剧本时间范围内", index+1),
			}
		}
		line.SpanStartTick -= sourceWindowStart
		line.SpanEndTick -= sourceWindowStart
		result = append(result, line)
	}
	return result, nil
}

func cleanContractHash(value string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "sha256:")
}
