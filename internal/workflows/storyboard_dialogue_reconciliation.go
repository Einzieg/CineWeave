package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const codeVideoPromptRegenerationRequired = "VIDEO_PROMPT_REGENERATION_REQUIRED"

type ReconcileStoryboardDialogueAssignmentsInput struct {
	OrganizationID string   `json:"organizationId"`
	ProjectID      string   `json:"projectId"`
	WorkflowRunID  string   `json:"workflowRunId"`
	ShotIDs        []string `json:"shotIds"`
}

type ReconcileStoryboardDialogueAssignmentsOutput struct {
	ChangedShotIDs []string `json:"changedShotIds,omitempty"`
}

type storedStoryboardDialogueShot struct {
	ID        string
	StartTick int64
	EndTick   int64
	Lines     []StoryboardDialogueLine
}

type storyboardDialogueOccurrence struct {
	ShotIndex int
	LineIndex int
	Line      StoryboardDialogueLine
}

func (a Activities) ReconcileStoryboardDialogueAssignments(
	ctx context.Context,
	input ReconcileStoryboardDialogueAssignmentsInput,
) (ReconcileStoryboardDialogueAssignmentsOutput, error) {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" {
		return ReconcileStoryboardDialogueAssignmentsOutput{}, fmt.Errorf("organizationId, projectId, and workflowRunId are required")
	}
	if len(input.ShotIDs) == 0 {
		return ReconcileStoryboardDialogueAssignmentsOutput{}, nil
	}
	execution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        "reconcile_storyboard_dialogue_assignments",
		NodeType:       "workflow.storyboard_dialogue.reconcile",
		Input:          mustJSON(map[string]any{"shotIds": input.ShotIDs}),
	})
	if err != nil {
		return ReconcileStoryboardDialogueAssignmentsOutput{}, err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ReconcileStoryboardDialogueAssignmentsOutput{}, err
	}
	defer tx.Rollback(ctx)
	runCtx, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution)
	if err != nil {
		return ReconcileStoryboardDialogueAssignmentsOutput{}, err
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text, start_tick, end_tick, script_dialogue
		FROM storyboard_shots
		WHERE project_id = $1
		  AND storyboard_plan_id IN (
		    SELECT DISTINCT storyboard_plan_id
		    FROM storyboard_shots
		    WHERE project_id = $1 AND id = ANY($2::uuid[]) AND storyboard_plan_id IS NOT NULL
		  )
		  AND production_generation_id = $3
		  AND deleted_at IS NULL
		ORDER BY episode_index, episode_shot_index, shot_index, id
		FOR UPDATE
	`, input.ProjectID, input.ShotIDs, runCtx.ProductionGenerationID)
	if err != nil {
		return ReconcileStoryboardDialogueAssignmentsOutput{}, err
	}
	shots := make([]storedStoryboardDialogueShot, 0, len(input.ShotIDs))
	for rows.Next() {
		var shot storedStoryboardDialogueShot
		var raw json.RawMessage
		if err := rows.Scan(&shot.ID, &shot.StartTick, &shot.EndTick, &raw); err != nil {
			rows.Close()
			return ReconcileStoryboardDialogueAssignmentsOutput{}, err
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &shot.Lines); err != nil {
				rows.Close()
				return ReconcileStoryboardDialogueAssignmentsOutput{}, fmt.Errorf("decode storyboard shot %s dialogue: %w", shot.ID, err)
			}
		}
		shots = append(shots, shot)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ReconcileStoryboardDialogueAssignmentsOutput{}, err
	}
	rows.Close()

	groups := make(map[string][]storyboardDialogueOccurrence)
	shotIndexByID := make(map[string]int, len(shots))
	for shotIndex := range shots {
		shotIndexByID[shots[shotIndex].ID] = shotIndex
		for lineIndex, line := range shots[shotIndex].Lines {
			key := strings.TrimSpace(line.TimingUnitID)
			if key == "" || strings.EqualFold(strings.TrimSpace(line.Kind), "system") {
				continue
			}
			groups[key] = append(groups[key], storyboardDialogueOccurrence{ShotIndex: shotIndex, LineIndex: lineIndex, Line: line})
		}
	}
	changed := make(map[int]bool)
	changeReason := make(map[int]string)
	for _, occurrences := range groups {
		if len(occurrences) < 2 {
			continue
		}
		sort.SliceStable(occurrences, func(left, right int) bool {
			if occurrences[left].Line.SpanStartTick == occurrences[right].Line.SpanStartTick {
				return occurrences[left].Line.SpanEndTick < occurrences[right].Line.SpanEndTick
			}
			return occurrences[left].Line.SpanStartTick < occurrences[right].Line.SpanStartTick
		})
		unitStart, unitEnd := occurrences[0].Line.SpanStartTick, occurrences[0].Line.SpanEndTick
		for _, occurrence := range occurrences[1:] {
			if occurrence.Line.SpanStartTick < unitStart {
				unitStart = occurrence.Line.SpanStartTick
			}
			if occurrence.Line.SpanEndTick > unitEnd {
				unitEnd = occurrence.Line.SpanEndTick
			}
		}
		sourceText := storyboardDialogueOccurrenceSourceText(occurrences)
		if sourceText == "" || unitEnd <= unitStart {
			continue
		}
		baseStartOffset, baseEndOffset := storyboardDialogueOccurrenceSourceOffsets(occurrences)
		for _, occurrence := range occurrences {
			line := occurrence.Line
			if baseStartOffset != nil && baseEndOffset != nil {
				start, end := *baseStartOffset, *baseEndOffset
				line.SourceStartOffset = &start
				line.SourceEndOffset = &end
			}
			resolved := storyboardDialogueLineForTimingSpan(line, sourceText, unitStart, unitEnd)
			current := shots[occurrence.ShotIndex].Lines[occurrence.LineIndex]
			if storyboardDialogueLineEquivalent(current, resolved) {
				continue
			}
			shots[occurrence.ShotIndex].Lines[occurrence.LineIndex] = resolved
			changed[occurrence.ShotIndex] = true
			changeReason[occurrence.ShotIndex] = "cross_shot_dialogue_reconciled"
		}
	}

	promptRows, err := tx.Query(ctx, `
		SELECT DISTINCT ON (prompt.storyboard_shot_id)
		       prompt.storyboard_shot_id::text, prompt.dialogue_cues,
		       COALESCE(context_plan.status, ''),
		       COALESCE(context_plan.plan_hash, ''),
		       prompt.prompt_context_plan_hash
		FROM video_prompt_plans prompt
		LEFT JOIN prompt_context_plans context_plan ON context_plan.id = prompt.prompt_context_plan_id
		WHERE prompt.storyboard_shot_id = ANY($1::uuid[])
		  AND prompt.status IN ('approved', 'reviewing')
		ORDER BY prompt.storyboard_shot_id, prompt.revision DESC, prompt.created_at DESC
	`, storyboardDialogueShotIDs(shots))
	if err != nil {
		return ReconcileStoryboardDialogueAssignmentsOutput{}, err
	}
	for promptRows.Next() {
		var shotID string
		var raw json.RawMessage
		var contextStatus, contextPlanHash, expectedContextPlanHash string
		if err := promptRows.Scan(&shotID, &raw, &contextStatus, &contextPlanHash, &expectedContextPlanHash); err != nil {
			promptRows.Close()
			return ReconcileStoryboardDialogueAssignmentsOutput{}, err
		}
		shotIndex, ok := shotIndexByID[shotID]
		if !ok {
			continue
		}
		if !videoPromptContextContractCurrent(contextStatus, contextPlanHash, expectedContextPlanHash) {
			changed[shotIndex] = true
			if changeReason[shotIndex] == "" {
				changeReason[shotIndex] = "video_prompt_context_contract_inactive"
			}
		}
		var planned []StoryboardDialogueLine
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &planned); err != nil {
				promptRows.Close()
				return ReconcileStoryboardDialogueAssignmentsOutput{}, fmt.Errorf("decode video prompt plan dialogue for shot %s: %w", shotID, err)
			}
		}
		if storyboardDialogueEquivalent(planned, SpokenStoryboardDialogue(shots[shotIndex].Lines)) {
			continue
		}
		changed[shotIndex] = true
		if changeReason[shotIndex] == "" {
			changeReason[shotIndex] = "video_prompt_dialogue_contract_mismatch"
		}
	}
	if err := promptRows.Err(); err != nil {
		promptRows.Close()
		return ReconcileStoryboardDialogueAssignmentsOutput{}, err
	}
	promptRows.Close()

	output := ReconcileStoryboardDialogueAssignmentsOutput{ChangedShotIDs: make([]string, 0, len(changed))}
	for shotIndex := range shots {
		if !changed[shotIndex] {
			continue
		}
		shot := shots[shotIndex]
		if _, err := tx.Exec(ctx, `
			UPDATE video_prompt_plans
			SET status = 'stale', stale_at = COALESCE(stale_at, now())
			WHERE storyboard_shot_id = $1 AND status IN ('approved', 'reviewing')
		`, shot.ID); err != nil {
			return ReconcileStoryboardDialogueAssignmentsOutput{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE video_native_audio_contracts
			SET status = 'stale', stale_at = COALESCE(stale_at, now())
			WHERE storyboard_shot_id = $1 AND status = 'active'
		`, shot.ID); err != nil {
			return ReconcileStoryboardDialogueAssignmentsOutput{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE video_render_plans
			SET status = 'stale', active = false, updated_at = now()
			WHERE storyboard_shot_id = $1 AND status NOT IN ('archived', 'cancelled', 'stale')
		`, shot.ID); err != nil {
			return ReconcileStoryboardDialogueAssignmentsOutput{}, err
		}
		reason := changeReason[shotIndex]
		message := "视频提示词对白契约与当前镜头不一致，请重新生成视频提示词"
		changedFields := []string{"videoPromptPlan"}
		if reason == "cross_shot_dialogue_reconciled" {
			message = "跨镜头对白已按时间范围拆分，请重新生成视频提示词"
			changedFields = append([]string{"scriptDialogue"}, changedFields...)
		} else if reason == "video_prompt_context_contract_inactive" {
			message = "视频提示词上下文契约已失效，请重新生成视频提示词"
			changedFields = append([]string{"promptContextPlan"}, changedFields...)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET script_dialogue = $2,
			    active_video_render_plan_id = NULL,
			    video_prompt_status = 'failed',
			    video_prompt_error_code = $3,
			    video_prompt_error_message = $6,
			    video_prompt_updated_at = now(),
			    stale_state = 'needs_regeneration',
			    updated_at = now()
			WHERE id = $1 AND project_id = $4 AND production_generation_id = $5
		`, shot.ID, mustJSON(shot.Lines), codeVideoPromptRegenerationRequired, input.ProjectID, runCtx.ProductionGenerationID, message); err != nil {
			return ReconcileStoryboardDialogueAssignmentsOutput{}, err
		}
		if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.updated", "storyboard_shot", shot.ID, mustJSON(map[string]any{
			"workflowRunId": input.WorkflowRunID,
			"changedFields": changedFields,
			"reason":        reason,
		})); err != nil {
			return ReconcileStoryboardDialogueAssignmentsOutput{}, err
		}
		output.ChangedShotIDs = append(output.ChangedShotIDs, shot.ID)
	}
	targetCount := len(reconciledStoryboardDialogueTargetIDs(input.ShotIDs, output.ChangedShotIDs))
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET total_items = GREATEST(total_items, $2),
		    revision = revision + 1,
		    updated_at = now()
		WHERE id = $1 AND status IN ('pending', 'queued', 'running')
	`, input.WorkflowRunID, targetCount); err != nil {
		return ReconcileStoryboardDialogueAssignmentsOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ReconcileStoryboardDialogueAssignmentsOutput{}, err
	}
	if err := CompleteNodeRun(ctx, a.db, execution, mustJSON(output)); err != nil {
		return ReconcileStoryboardDialogueAssignmentsOutput{}, err
	}
	return output, nil
}

func reconciledStoryboardDialogueTargetIDs(requested, changed []string) []string {
	result := append([]string(nil), requested...)
	result = append(result, changed...)
	return uniqueNonEmptyStrings(result)
}

func videoPromptContextContractCurrent(status, actualHash, expectedHash string) bool {
	actualHash = cleanContractHash(actualHash)
	return status == "active" && actualHash != "" && actualHash == cleanContractHash(expectedHash)
}

func storyboardDialogueShotIDs(shots []storedStoryboardDialogueShot) []string {
	ids := make([]string, 0, len(shots))
	for _, shot := range shots {
		ids = append(ids, shot.ID)
	}
	return ids
}

func storyboardDialogueOccurrenceSourceText(occurrences []storyboardDialogueOccurrence) string {
	if len(occurrences) == 0 {
		return ""
	}
	first := strings.TrimSpace(occurrences[0].Line.Text)
	allEqual := first != ""
	for _, occurrence := range occurrences[1:] {
		if strings.TrimSpace(occurrence.Line.Text) != first {
			allEqual = false
			break
		}
	}
	if allEqual {
		return first
	}
	var combined strings.Builder
	for _, occurrence := range occurrences {
		combined.WriteString(strings.TrimSpace(occurrence.Line.Text))
	}
	return combined.String()
}

func storyboardDialogueOccurrenceSourceOffsets(occurrences []storyboardDialogueOccurrence) (*int, *int) {
	var start, end int
	found := false
	for _, occurrence := range occurrences {
		if occurrence.Line.SourceStartOffset == nil || occurrence.Line.SourceEndOffset == nil {
			continue
		}
		if !found || *occurrence.Line.SourceStartOffset < start {
			start = *occurrence.Line.SourceStartOffset
		}
		if !found || *occurrence.Line.SourceEndOffset > end {
			end = *occurrence.Line.SourceEndOffset
		}
		found = true
	}
	if !found {
		return nil, nil
	}
	return &start, &end
}

func storyboardDialogueLineEquivalent(left, right StoryboardDialogueLine) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}
