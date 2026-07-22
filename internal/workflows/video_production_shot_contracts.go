package workflows

import (
	"context"
	"fmt"
	"strings"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	storyboardpkg "github.com/Einzieg/cineweave/internal/storyboard"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
)

func applyProfileStoryboardPlannerContract(rendered promptsvc.RenderedPrompt, profileKey string) (promptsvc.RenderedPrompt, error) {
	strategy, err := videoproduction.ProfileStrategyFor(profileKey)
	if err != nil {
		return promptsvc.RenderedPrompt{}, err
	}
	contract := strings.TrimSpace(strategy.Anchors().PlannerContract())
	if contract == "" {
		return promptsvc.RenderedPrompt{}, videoproduction.Error{
			Code:    videoproduction.CodeProfileIncompatible,
			Message: "视频生产方案缺少分镜规划契约：" + profileKey,
		}
	}
	rendered.RenderedText = strings.TrimSpace(rendered.RenderedText) + "\n\n" + contract
	rendered.RenderedHash = promptsvc.HashText(rendered.RenderedText)
	rendered.Source = firstNonEmptyString(rendered.Source, "system_active") + "+" + profileKey + "_shot_contract_v1"
	return rendered, nil
}

func (a Activities) storeProfileShotContractsTx(
	ctx context.Context,
	tx pgx.Tx,
	input PlanStoryboardSceneInput,
	record scenePlanningRecord,
	runCtx nodeRunContext,
	execution NodeExecution,
	gatewayResponse provider.GatewayTextResponse,
	promptVersionID string,
	profileKey string,
	previousShotID string,
	shot PlannedStoryboardShot,
) error {
	if shot.EntryStateHash == "" || shot.ExitStateHash == "" || shot.TransitionHash == "" {
		return fmt.Errorf("shot %s is missing compiled production-profile contracts", shot.ID)
	}
	var entryStateID, exitStateID string
	for _, state := range []struct {
		role   string
		value  videoproduction.ShotState
		hash   string
		target *string
	}{
		{role: videoproduction.StateRolePlannedEntry, value: shot.PlannedEntryState, hash: shot.EntryStateHash, target: &entryStateID},
		{role: videoproduction.StateRolePlannedExit, value: shot.PlannedExitState, hash: shot.ExitStateHash, target: &exitStateID},
	} {
		if err := tx.QueryRow(ctx, `
			INSERT INTO storyboard_shot_state_versions(
				organization_id, project_id, production_generation_id, storyboard_shot_id,
				state_role, revision, status, state, state_hash, source_type, source_id,
				prompt_version_id, provider_call_id, model_id, created_by, approved_at
			)
			VALUES ($1, $2, $3, $4, $5, 1, 'approved', $6, $7,
			        'deterministic_canonicalizer', NULLIF($8, '')::uuid,
			        NULLIF($9, '')::uuid, NULLIF($10, '')::uuid, NULLIF($11, '')::uuid,
			        NULLIF($12, '')::uuid, now())
			RETURNING id::text
		`, input.OrganizationID, input.ProjectID, runCtx.ProductionGenerationID, shot.ID,
			state.role, mustJSON(state.value), state.hash, execution.NodeRunID,
			promptVersionID, gatewayResponse.ProviderCallID, gatewayResponse.ModelID, input.CreatedBy).Scan(state.target); err != nil {
			return err
		}
	}
	var transitionID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO storyboard_shot_transitions(
			organization_id, project_id, production_generation_id, storyboard_plan_id,
			source_shot_id, target_shot_id, transition_type, tail_policy, anchor_policy,
			carry_constraints, reset_constraints, confidence, revision, status,
			review_status, metadata
		)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid, $6, $7, $8, $9,
		        $10, $11, $12, 1, 'active', 'approved', $13)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, runCtx.ProductionGenerationID, input.StoryboardPlanID,
		previousShotID, shot.ID, shot.Transition.TransitionType, shot.Transition.TailPolicy,
		shot.Transition.AnchorPolicy, mustJSON(shot.Transition.Carry), mustJSON(shot.Transition.Reset),
		shot.Transition.Confidence, mustJSON(map[string]any{
			"transitionHash": shot.TransitionHash,
			"contractReview": shot.ContractReview,
			"source":         "deterministic_transition_classifier_v1",
		})).Scan(&transitionID); err != nil {
		return err
	}
	strategy, err := videoproduction.ProfileStrategyFor(profileKey)
	if err != nil {
		return err
	}
	stateVersionIDs := map[string]string{
		videoproduction.StateRolePlannedEntry: entryStateID,
		videoproduction.StateRolePlannedExit:  exitStateID,
	}
	stateHashes := map[string]string{
		videoproduction.StateRolePlannedEntry: shot.EntryStateHash,
		videoproduction.StateRolePlannedExit:  shot.ExitStateHash,
	}
	anchorIDs := make(map[string][]string)
	for _, requirement := range strategy.Anchors().Requirements() {
		if !requirement.Required || requirement.Role == videoproduction.AnchorRoleStoryboardPanel {
			continue
		}
		count := requirement.Minimum
		if count < 1 {
			count = 1
		}
		stateRole := requirement.StateRole
		stateVersionID := stateVersionIDs[stateRole]
		if stateVersionID == "" {
			stateRole = videoproduction.StateRolePlannedEntry
			stateVersionID = entryStateID
		}
		for ordinal := 0; ordinal < count; ordinal++ {
			var anchorID string
			if err := tx.QueryRow(ctx, `
				INSERT INTO shot_visual_anchors(
					organization_id, project_id, production_generation_id, storyboard_shot_id,
					shot_state_version_id, anchor_role, revision, status, review_status, metadata
				)
				VALUES ($1, $2, $3, $4, $5, $6, $7, 'draft', 'pending', $8)
				RETURNING id::text
			`, input.OrganizationID, input.ProjectID, runCtx.ProductionGenerationID, shot.ID,
				stateVersionID, requirement.Role, ordinal+1, mustJSON(map[string]any{
					"profileKey":         profileKey,
					"bindingId":          runCtx.VideoProductionBindingID,
					"bindingRevision":    runCtx.VideoProductionBindingRevision,
					"stateRole":          stateRole,
					"stateHash":          stateHashes[stateRole],
					"entryStateHash":     shot.EntryStateHash,
					"exitStateHash":      shot.ExitStateHash,
					"transitionId":       transitionID,
					"transitionHash":     shot.TransitionHash,
					"sourceScenePlanId":  input.ScenePlanID,
					"sourceSceneOrdinal": record.SceneOrdinal,
					"workflowRunId":      input.WorkflowRunID,
					"anchorOrdinal":      ordinal,
				})).Scan(&anchorID); err != nil {
				return err
			}
			anchorIDs[requirement.Role] = append(anchorIDs[requirement.Role], anchorID)
		}
	}
	for _, requirement := range strategy.Anchors().Requirements() {
		if !requirement.Required || requirement.Role == videoproduction.AnchorRoleStoryboardPanel {
			continue
		}
		if count := len(anchorIDs[requirement.Role]); count < requirement.Minimum || (requirement.Maximum > 0 && count > requirement.Maximum) {
			return videoproduction.Error{Code: videoproduction.CodeReferencePackIncomplete, Message: fmt.Sprintf("计划锚点 %s 数量为 %d，要求 %d-%d", requirement.Role, count, requirement.Minimum, requirement.Maximum)}
		}
	}
	primaryAnchorID := ""
	if ids := anchorIDs[videoproduction.AnchorRolePlannedFirstFrame]; len(ids) > 0 {
		primaryAnchorID = ids[0]
	} else {
		for _, ids := range anchorIDs {
			if len(ids) > 0 {
				primaryAnchorID = ids[0]
				break
			}
		}
	}
	payload := map[string]any{
		"bindingId":              runCtx.VideoProductionBindingID,
		"bindingRevision":        runCtx.VideoProductionBindingRevision,
		"productionGenerationId": runCtx.ProductionGenerationID,
		"episodeId":              input.ScriptEpisodeID,
		"storyboardShotId":       shot.ID,
		"workflowRunId":          input.WorkflowRunID,
		"storyboardPlanId":       input.StoryboardPlanID,
		"entryStateVersionId":    entryStateID,
		"exitStateVersionId":     exitStateID,
		"entryStateHash":         shot.EntryStateHash,
		"exitStateHash":          shot.ExitStateHash,
		"transitionId":           transitionID,
		"transitionHash":         shot.TransitionHash,
		"transitionType":         shot.Transition.TransitionType,
		"visualAnchorId":         primaryAnchorID,
		"visualAnchorIds":        anchorIDs,
		"profileKey":             profileKey,
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"storyboard.shot.state.planned", "storyboard_shot_state_version", entryStateID, mustJSON(payload)); err != nil {
		return err
	}
	return insertEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"storyboard.shot.transition.planned", "storyboard_shot_transition", transitionID, mustJSON(payload))
}

func compileProfilePlannedShotContracts(
	shots []PlannedStoryboardShot,
	planner storyboardpkg.ShotPlannerOutput,
	assets []CanonicalAssetRecord,
	profileKey string,
	timelineTimebase int64,
) ([]PlannedStoryboardShot, error) {
	assetTypes := make(map[string]string, len(assets))
	for _, asset := range assets {
		assetTypes[strings.TrimSpace(asset.ID)] = strings.ToLower(strings.TrimSpace(asset.AssetType))
	}
	var previousExit *videoproduction.ShotState
	for index := range shots {
		suggestion := plannerSuggestionForShotSlot(
			storyboardpkg.ShotDraft{Ordinal: shots[index].ShotOrdinal},
			planner.Shots,
		)
		entry := videoproduction.NormalizeShotState(shots[index].PlannedEntryState)
		exit := videoproduction.NormalizeShotState(shots[index].PlannedExitState)
		requiredAssetIDs := make([]string, 0, len(shots[index].AssetRequirements))
		for _, requirement := range shots[index].AssetRequirements {
			if assetID := strings.TrimSpace(requirement.AssetID); assetID != "" {
				requiredAssetIDs = append(requiredAssetIDs, assetID)
			}
		}
		entry, exit = canonicalizeShotContractStates(entry, exit, requiredAssetIDs, assetTypes)
		transition, err := videoproduction.ClassifyTransition(previousExit, entry, suggestion.TransitionFromPrevious)
		if err != nil {
			return nil, fmt.Errorf("shot %d transition: %w", shots[index].ShotOrdinal+1, err)
		}
		var review videoproduction.ShotContractReview
		switch profileKey {
		case videoproduction.ProfileFirstLastFrame:
			review = videoproduction.ReviewFirstLastFrameContract(
				entry, exit, transition, requiredAssetIDs,
				shots[index].DurationTicks, timelineTimebase,
			)
		default:
			review = videoproduction.ReviewShotContract(entry, exit, transition, requiredAssetIDs)
		}
		if !review.Approved {
			return nil, fmt.Errorf("shot %d contract review rejected: %v", shots[index].ShotOrdinal+1, review.Issues)
		}
		entryHash, err := videoproduction.HashShotState(entry)
		if err != nil {
			return nil, err
		}
		exitHash, err := videoproduction.HashShotState(exit)
		if err != nil {
			return nil, err
		}
		transitionHash, err := videoproduction.HashTransition(transition)
		if err != nil {
			return nil, err
		}
		shots[index].PlannedEntryState = entry
		shots[index].PlannedExitState = exit
		shots[index].Transition = transition
		shots[index].EntryStateHash = entryHash
		shots[index].ExitStateHash = exitHash
		shots[index].TransitionHash = transitionHash
		shots[index].ContractReview = review
		exitCopy := exit
		previousExit = &exitCopy
	}
	return shots, nil
}

// canonicalizeShotContractStates turns the planner's asset requirements into
// an executable first-frame contract. The model supplies creative state, while
// this deterministic step owns visibility and the single-shot identity rules.
func canonicalizeShotContractStates(
	entry videoproduction.ShotState,
	exit videoproduction.ShotState,
	requiredAssetIDs []string,
	assetTypes map[string]string,
) (videoproduction.ShotState, videoproduction.ShotState) {
	entry = videoproduction.NormalizeShotState(entry)
	exit = videoproduction.NormalizeShotState(exit)

	for _, assetID := range requiredAssetIDs {
		assetID = strings.TrimSpace(assetID)
		if assetID == "" {
			continue
		}
		switch assetTypes[assetID] {
		case "scene":
			entry.Scene.AssetID = assetID
		case "character":
			if !shotStateHasCharacter(entry, assetID) {
				entry.Characters = append(entry.Characters, videoproduction.CharacterState{
					AssetID:    assetID,
					Pose:       "standing",
					Expression: "neutral",
					Blocking: videoproduction.BlockingState{
						Horizontal: "center",
						Depth:      "midground",
						Facing:     "camera",
					},
				})
			}
		case "prop":
			if !shotStateHasProp(entry, assetID) {
				entry.Props = append(entry.Props, videoproduction.PropState{AssetID: assetID, State: "present"})
			}
		}
	}

	entry = videoproduction.AlignShotStateVisibility(entry, requiredAssetIDs)
	exit.Scene.AssetID = entry.Scene.AssetID
	exit.Characters = alignExitCharactersToEntry(entry.Characters, exit.Characters)
	exit.Props = alignExitPropsToEntry(entry.Props, exit.Props)
	exit = videoproduction.AlignShotStateVisibility(exit, requiredAssetIDs)
	return videoproduction.NormalizeShotState(entry), videoproduction.NormalizeShotState(exit)
}

func shotStateHasCharacter(state videoproduction.ShotState, assetID string) bool {
	for _, character := range state.Characters {
		if character.AssetID == assetID {
			return true
		}
	}
	return false
}

func shotStateHasProp(state videoproduction.ShotState, assetID string) bool {
	for _, prop := range state.Props {
		if prop.AssetID == assetID {
			return true
		}
	}
	return false
}

func alignExitCharactersToEntry(entry, exit []videoproduction.CharacterState) []videoproduction.CharacterState {
	exitByID := make(map[string]videoproduction.CharacterState, len(exit))
	for _, character := range exit {
		exitByID[character.AssetID] = character
	}
	aligned := make([]videoproduction.CharacterState, 0, len(entry))
	for _, character := range entry {
		if current, ok := exitByID[character.AssetID]; ok {
			aligned = append(aligned, current)
			continue
		}
		aligned = append(aligned, character)
	}
	return aligned
}

func alignExitPropsToEntry(entry, exit []videoproduction.PropState) []videoproduction.PropState {
	exitByID := make(map[string]videoproduction.PropState, len(exit))
	for _, prop := range exit {
		exitByID[prop.AssetID] = prop
	}
	aligned := make([]videoproduction.PropState, 0, len(entry))
	for _, prop := range entry {
		if current, ok := exitByID[prop.AssetID]; ok {
			aligned = append(aligned, current)
			continue
		}
		aligned = append(aligned, prop)
	}
	return aligned
}

func anchorRoleCounts(values map[string][]string) map[string]int {
	result := make(map[string]int, len(values))
	for role, ids := range values {
		result[role] = len(ids)
	}
	return result
}
