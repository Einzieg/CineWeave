package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/agent"
	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/jackc/pgx/v5"
)

type projectControlSyncAction func(
	context.Context,
	pgx.Tx,
	auth.Principal,
	Project,
	projectcontrol.Command,
	json.RawMessage,
) (agentToolResult, error)

var projectControlSharedSyncActionNames = map[string]struct{}{
	"adaptation.activate":                     {},
	"adaptation.create":                       {},
	"adaptation.review":                       {},
	"adaptation.update":                       {},
	"asset.delete":                            {},
	"asset.reference.create":                  {},
	"asset.reference.delete":                  {},
	"asset.reference.set_primary":             {},
	"asset.update":                            {},
	"character_voice.create":                  {},
	"character_voice.delete":                  {},
	"character_voice.update":                  {},
	"commerce.attachment.assign":              {},
	"commerce.product.rebuild":                {},
	"commerce.product.rebuild_impact":         {},
	"commerce.product.reference.archive":      {},
	"commerce.product.reference.set_primary":  {},
	"commerce.product.reference.update":       {},
	"commerce.product.update":                 {},
	"commerce.product.version.create":         {},
	"commerce.script.archive":                 {},
	"commerce.script.create":                  {},
	"commerce.script.create_language_variant": {},
	"commerce.script.defaults.update":         {},
	"commerce.script.duplicate":               {},
	"commerce.script.rebuild_impact":          {},
	"commerce.script.reference.archive":       {},
	"commerce.script.reorder":                 {},
	"commerce.script.update":                  {},
	"final_video.activate":                    {},
	"final_video.delete":                      {},
	"prompt.activate_version":                 {},
	"prompt.create_version":                   {},
	"review.apply_fix":                        {},
	"review.dismiss_fix":                      {},
	"review.ignore_item":                      {},
	"review.reopen_item":                      {},
	"review.resolve_item":                     {},
	"source.create":                           {},
	"source.delete":                           {},
	"source.delete_chapter":                   {},
	"source.update":                           {},
	"script.update":                           {},
	"script.update_episode":                   {},
	"script.delete":                           {},
	"script.create":                           {},
	"script.create_version":                   {},
	"script.activate_version":                 {},
	"script.archive_version":                  {},
	"script_scene.delete":                     {},
	"script_scene.review":                     {},
	"script_scene.update":                     {},
	"shot_asset.review_requirements":          {},
	"shot_asset.skip_requirement":             {},
	"shot_asset.update_requirement":           {},
	"shot.render_plan.verify_audio":           {},
	"shot.video_prompt.approve":               {},
	"shot.video_prompt.create_revision":       {},
	"shot.video_prompt.reject":                {},
	"storyboard.activate_plan":                {},
	"storyboard.approve_anchor":               {},
	"storyboard.create_shot":                  {},
	"storyboard.delete_shot":                  {},
	"storyboard.merge_shots":                  {},
	"storyboard.reject_anchor":                {},
	"storyboard.reorder":                      {},
	"storyboard.review_shot":                  {},
	"storyboard.split_shot":                   {},
	"storyboard.unlink_media":                 {},
	"timeline.clip.create":                    {},
	"timeline.clip.delete":                    {},
	"timeline.clip.reorder":                   {},
	"timeline.create":                         {},
	"timeline.delete":                         {},
	"timeline.update":                         {},
	"timeline.update_clip":                    {},
	"project.production_rebuild_impact":       {},
	"project.clear_production_content":        {},
	"project.update":                          {},
	"novel_event.review":                      {},
	"novel_event.update":                      {},
}

func projectControlHasSharedSyncAction(name string) bool {
	_, exists := projectControlSharedSyncActionNames[name]
	return exists
}

func projectControlSyncActionSet(server *Server) map[string]projectControlSyncAction {
	return map[string]projectControlSyncAction{
		"adaptation.activate":                     server.executeAdaptationPlanActivateSyncAction,
		"adaptation.create":                       server.executeAdaptationPlanCreateSyncAction,
		"adaptation.review":                       server.executeAdaptationPlanReviewSyncAction,
		"adaptation.update":                       server.executeAdaptationPlanUpdateSyncAction,
		"asset.delete":                            server.executeAssetDeleteSyncAction,
		"asset.reference.create":                  server.executeAssetReferenceCreateSyncAction,
		"asset.reference.delete":                  server.executeAssetReferenceDeleteSyncAction,
		"asset.reference.set_primary":             server.executeAssetReferenceSetPrimarySyncAction,
		"asset.update":                            server.executeAssetUpdateSyncAction,
		"character_voice.create":                  server.executeCharacterVoiceCreateSyncAction,
		"character_voice.delete":                  server.executeCharacterVoiceDeleteSyncAction,
		"character_voice.update":                  server.executeCharacterVoiceUpdateSyncAction,
		"commerce.attachment.assign":              server.executeCommerceAttachmentAssignSyncAction,
		"commerce.product.rebuild":                server.executeCommerceProductRebuildSyncAction,
		"commerce.product.rebuild_impact":         server.executeCommerceProductRebuildImpactSyncAction,
		"commerce.product.reference.archive":      server.executeCommerceProductReferenceArchiveSyncAction,
		"commerce.product.reference.set_primary":  server.executeCommerceProductReferenceSetPrimarySyncAction,
		"commerce.product.reference.update":       server.executeCommerceProductReferenceUpdateSyncAction,
		"commerce.product.update":                 server.executeCommerceProductUpdateSyncAction,
		"commerce.product.version.create":         server.executeCommerceProductVersionCreateSyncAction,
		"commerce.script.archive":                 server.executeCommerceScriptArchiveSyncAction,
		"commerce.script.create":                  server.executeCommerceScriptCreateSyncAction,
		"commerce.script.create_language_variant": server.executeCommerceScriptLanguageVariantSyncAction,
		"commerce.script.defaults.update":         server.executeCommerceScriptDefaultsUpdateSyncAction,
		"commerce.script.duplicate":               server.executeCommerceScriptDuplicateSyncAction,
		"commerce.script.rebuild_impact":          server.executeCommerceScriptRebuildImpactSyncAction,
		"commerce.script.reference.archive":       server.executeCommerceScriptReferenceArchiveSyncAction,
		"commerce.script.reorder":                 server.executeCommerceScriptReorderSyncAction,
		"commerce.script.update":                  server.executeCommerceScriptUpdateSyncAction,
		"final_video.activate":                    server.executeFinalVideoActivateSyncAction,
		"final_video.delete":                      server.executeFinalVideoDeleteSyncAction,
		"prompt.activate_version":                 server.executePromptVersionActivateSyncAction,
		"prompt.create_version":                   server.executePromptVersionCreateSyncAction,
		"review.apply_fix":                        server.executeReviewApplyFixSyncAction,
		"review.dismiss_fix":                      server.executeReviewDismissFixSyncAction,
		"review.ignore_item":                      server.executeReviewItemStatusSyncAction("ignored"),
		"review.reopen_item":                      server.executeReviewItemStatusSyncAction("open"),
		"review.resolve_item":                     server.executeReviewItemStatusSyncAction("resolved"),
		"source.create":                           server.executeSourceCreateSyncAction,
		"source.delete":                           server.executeSourceDeleteSyncAction,
		"source.delete_chapter":                   server.executeSourceDeleteChapterSyncAction,
		"source.update":                           server.executeSourceUpdateSyncAction,
		"script.update":                           server.executeScriptUpdateSyncAction,
		"script.update_episode":                   server.executeScriptEpisodeUpdateSyncAction,
		"script.delete":                           server.executeScriptDeleteSyncAction,
		"script.create":                           server.executeScriptCreateSyncAction,
		"script.create_version":                   server.executeScriptCreateVersionSyncAction,
		"script.activate_version":                 server.executeScriptActivateVersionSyncAction,
		"script.archive_version":                  server.executeScriptArchiveVersionSyncAction,
		"script_scene.delete":                     server.executeScriptSceneDeleteSyncAction,
		"script_scene.review":                     server.executeScriptSceneReviewSyncAction,
		"script_scene.update":                     server.executeScriptSceneUpdateSyncAction,
		"shot_asset.review_requirements":          server.executeShotAssetRequirementReviewSyncAction,
		"shot_asset.skip_requirement":             server.executeShotAssetRequirementSkipSyncAction,
		"shot_asset.update_requirement":           server.executeShotAssetRequirementUpdateSyncAction,
		"shot.render_plan.verify_audio":           server.executeShotRenderPlanVerifyAudioSyncAction,
		"shot.video_prompt.approve":               server.executeShotVideoPromptReviewSyncAction("approved"),
		"shot.video_prompt.create_revision":       server.executeShotVideoPromptCreateRevisionSyncAction,
		"shot.video_prompt.reject":                server.executeShotVideoPromptReviewSyncAction("rejected"),
		"storyboard.activate_plan":                server.executeStoryboardActivatePlanSyncAction,
		"storyboard.approve_anchor":               server.executeStoryboardReviewAnchorSyncAction("approved"),
		"storyboard.create_shot":                  server.executeStoryboardCreateShotSyncAction,
		"storyboard.delete_shot":                  server.executeStoryboardDeleteShotSyncAction,
		"storyboard.merge_shots":                  server.executeStoryboardMergeShotsSyncAction,
		"storyboard.reject_anchor":                server.executeStoryboardReviewAnchorSyncAction("rejected"),
		"storyboard.reorder":                      server.executeStoryboardReorderSyncAction,
		"storyboard.review_shot":                  server.executeStoryboardReviewShotSyncAction,
		"storyboard.split_shot":                   server.executeStoryboardSplitShotSyncAction,
		"storyboard.unlink_media":                 server.executeStoryboardUnlinkMediaSyncAction,
		"timeline.clip.create":                    server.executeTimelineClipCreateSyncAction,
		"timeline.clip.delete":                    server.executeTimelineClipDeleteSyncAction,
		"timeline.clip.reorder":                   server.executeTimelineClipReorderSyncAction,
		"timeline.create":                         server.executeTimelineCreateSyncAction,
		"timeline.delete":                         server.executeTimelineDeleteSyncAction,
		"timeline.update":                         server.executeTimelineUpdateSyncAction,
		"timeline.update_clip":                    server.executeTimelineClipUpdateSyncAction,
		"project.production_rebuild_impact":       server.executeProjectProductionRebuildImpactSyncAction,
		"project.clear_production_content":        server.executeProjectClearProductionContentSyncAction,
		"project.update":                          server.executeProjectUpdateSyncAction,
		"novel_event.review":                      server.executeNovelEventReviewSyncAction,
		"novel_event.update":                      server.executeNovelEventUpdateSyncAction,
	}
}

func (s *Server) executeNovelEventUpdateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateAdaptationActionCommand(command.ProjectID, command.ActorUserID, "novel_event.update"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeNovelEventUpdateActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	item, err := s.updateNovelEventActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode novel_event.update arguments: %w", err)
	}
	return novelEventUpdateAgentResult(arguments, item), nil
}

func (s *Server) executeNovelEventReviewSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateAdaptationActionCommand(command.ProjectID, command.ActorUserID, "novel_event.review"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeNovelEventReviewActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	item, review, err := s.reviewNovelEventActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode novel_event.review arguments: %w", err)
	}
	return novelEventReviewAgentResult(arguments, item, review), nil
}

func (s *Server) executeAdaptationPlanCreateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateAdaptationActionCommand(command.ProjectID, command.ActorUserID, "adaptation.create"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeAdaptationPlanCreateActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	item, err := s.createAdaptationPlanActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode adaptation.create arguments: %w", err)
	}
	return adaptationPlanCreateAgentResult(arguments, item), nil
}

func (s *Server) executeAdaptationPlanUpdateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateAdaptationActionCommand(command.ProjectID, command.ActorUserID, "adaptation.update"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeAdaptationPlanUpdateActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	item, err := s.updateAdaptationPlanActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode adaptation.update arguments: %w", err)
	}
	return adaptationPlanUpdateAgentResult(arguments, item), nil
}

func (s *Server) executeAdaptationPlanReviewSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateAdaptationActionCommand(command.ProjectID, command.ActorUserID, "adaptation.review"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeAdaptationPlanReviewActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	item, review, err := s.reviewAdaptationPlanActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode adaptation.review arguments: %w", err)
	}
	return adaptationPlanReviewAgentResult(arguments, item, review), nil
}

func (s *Server) executeAdaptationPlanActivateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateAdaptationActionCommand(command.ProjectID, command.ActorUserID, "adaptation.activate"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeAdaptationPlanActivateActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	item, err := s.activateAdaptationPlanActionTx(ctx, tx, project, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode adaptation.activate arguments: %w", err)
	}
	return adaptationPlanActivateAgentResult(arguments, item), nil
}

func (s *Server) executeCharacterVoiceCreateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateCharacterVoiceActionCommand(command.ProjectID, command.ActorUserID, "character_voice.create"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeCharacterVoiceCreateActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	item, err := s.createCharacterVoiceActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return characterVoiceAgentResult("character_voice.create", raw, "角色声音已创建。", item)
}

func (s *Server) executeCharacterVoiceUpdateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateCharacterVoiceActionCommand(command.ProjectID, command.ActorUserID, "character_voice.update"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeCharacterVoiceUpdateActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	item, err := s.updateCharacterVoiceActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return characterVoiceAgentResult("character_voice.update", raw, fmt.Sprintf("角色声音已更新，当前 revision 为 %d。", item.Revision), item)
}

func (s *Server) executeCharacterVoiceDeleteSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateCharacterVoiceActionCommand(command.ProjectID, command.ActorUserID, "character_voice.delete"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeCharacterVoiceDeleteActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	item, err := s.deleteCharacterVoiceActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return characterVoiceAgentResult("character_voice.delete", raw, "角色声音已归档。", item)
}

func (s *Server) executeFinalVideoActivateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateFinalVideoActionCommand(command, "final_video.activate"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeFinalVideoActivateActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	item, idempotent, err := s.activateFinalVideoActionTx(ctx, tx, project, command, input)
	if err != nil {
		return agentToolResult{}, finalVideoNotFoundOrConflict(err)
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode final_video.activate arguments: %w", err)
	}
	return finalVideoActivateAgentResult(arguments, item, idempotent), nil
}

func (s *Server) executeFinalVideoDeleteSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateFinalVideoActionCommand(command, "final_video.delete"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeFinalVideoDeleteActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	result, err := s.deleteFinalVideoActionTx(ctx, tx, project, command, input)
	if err != nil {
		return agentToolResult{}, finalVideoNotFoundOrConflict(err)
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode final_video.delete arguments: %w", err)
	}
	return finalVideoDeleteAgentResult(arguments, result), nil
}

func (s *Server) executeProjectUpdateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateProjectUpdateActionCommand(command.ProjectID, command.ActorUserID); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeProjectUpdateActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	item, err := s.updateProjectActionTx(ctx, tx, principal, project, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode project.update arguments: %w", err)
	}
	return projectUpdateAgentResult(arguments, item), nil
}

func (s *Server) executeProjectProductionRebuildImpactSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if strings.TrimSpace(command.ProjectID) == "" || strings.TrimSpace(command.ActorUserID) == "" {
		return agentToolResult{}, controlValidationError("project.production_rebuild_impact 缺少项目或执行用户")
	}
	input, err := decodeProjectVideoProductionRebuildImpactActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	result, err := s.projectVideoProductionRebuildImpactAction(ctx, tx, project, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode project.production_rebuild_impact arguments: %w", err)
	}
	return projectVideoProductionRebuildImpactAgentResult(arguments, result), nil
}

func (s *Server) executeAssetReferenceCreateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateAssetReferenceActionCommand(command.ProjectID, command.ActorUserID, "asset.reference.create"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeAssetReferenceCreateActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	outcome, err := s.createAssetReferenceActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode asset.reference.create arguments: %w", err)
	}
	return assetReferenceCreateAgentResult(arguments, outcome), nil
}

func (s *Server) executeAssetReferenceSetPrimarySyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateAssetReferenceActionCommand(command.ProjectID, command.ActorUserID, "asset.reference.set_primary"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeAssetReferenceSetPrimaryActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	outcome, err := s.setPrimaryAssetReferenceActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode asset.reference.set_primary arguments: %w", err)
	}
	return assetReferenceSetPrimaryAgentResult(arguments, outcome), nil
}

func (s *Server) executeAssetReferenceDeleteSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateAssetReferenceActionCommand(command.ProjectID, command.ActorUserID, "asset.reference.delete"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeAssetReferenceDeleteActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	outcome, err := s.deleteAssetReferenceActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode asset.reference.delete arguments: %w", err)
	}
	return assetReferenceDeleteAgentResult(arguments, outcome), nil
}

func (s *Server) executeScriptSceneUpdateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateScriptActionCommand(command.ProjectID, command.ActorUserID, "script_scene.update"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeScriptSceneUpdateActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	item, err := s.updateScriptSceneActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode script_scene.update arguments: %w", err)
	}
	return scriptSceneUpdateAgentResult(arguments, item), nil
}

func (s *Server) executeScriptSceneReviewSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateScriptActionCommand(command.ProjectID, command.ActorUserID, "script_scene.review"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeScriptSceneReviewActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	outcome, err := s.reviewScriptSceneActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode script_scene.review arguments: %w", err)
	}
	return scriptSceneReviewAgentResult(arguments, outcome), nil
}

func (s *Server) executeScriptSceneDeleteSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateScriptActionCommand(command.ProjectID, command.ActorUserID, "script_scene.delete"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeScriptSceneDeleteActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	outcome, err := s.deleteScriptSceneActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode script_scene.delete arguments: %w", err)
	}
	return scriptSceneDeleteAgentResult(arguments, outcome), nil
}

func (s *Server) executeScriptCreateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateScriptActionCommand(command.ProjectID, command.ActorUserID, "script.create"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeScriptCreateActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	outcome, err := s.createScriptActionTx(ctx, tx, principal, project, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode script.create arguments: %w", err)
	}
	return scriptCreateAgentResult(arguments, outcome), nil
}

func (s *Server) executeScriptCreateVersionSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateScriptActionCommand(command.ProjectID, command.ActorUserID, "script.create_version"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeScriptCreateVersionActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	outcome, err := s.createScriptVersionActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode script.create_version arguments: %w", err)
	}
	return scriptCreateVersionAgentResult(arguments, outcome), nil
}

func (s *Server) executeScriptActivateVersionSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateScriptActionCommand(command.ProjectID, command.ActorUserID, "script.activate_version"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeScriptActivateVersionActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	outcome, err := s.activateScriptVersionActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode script.activate_version arguments: %w", err)
	}
	return scriptActivateVersionAgentResult(arguments, outcome), nil
}

func (s *Server) executeScriptArchiveVersionSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateScriptActionCommand(command.ProjectID, command.ActorUserID, "script.archive_version"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeScriptArchiveVersionActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	outcome, err := s.archiveScriptVersionActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode script.archive_version arguments: %w", err)
	}
	return scriptArchiveVersionAgentResult(arguments, outcome), nil
}

func (s *Server) executeScriptUpdateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateScriptActionCommand(command.ProjectID, command.ActorUserID, "script.update"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeScriptUpdateActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	outcome, err := s.updateScriptActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode script.update arguments: %w", err)
	}
	return scriptUpdateAgentResult(arguments, outcome), nil
}

func (s *Server) executeScriptEpisodeUpdateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateScriptActionCommand(command.ProjectID, command.ActorUserID, "script.update_episode"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeScriptEpisodeUpdateActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	outcome, err := s.updateScriptEpisodeActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode script.update_episode arguments: %w", err)
	}
	return scriptEpisodeUpdateAgentResult(arguments, outcome), nil
}

func (s *Server) executeScriptDeleteSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateScriptActionCommand(command.ProjectID, command.ActorUserID, "script.delete"); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeScriptDeleteActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	outcome, err := s.deleteScriptActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode script.delete arguments: %w", err)
	}
	return scriptDeleteAgentResult(arguments, outcome), nil
}

func (s *Server) executeSourceCreateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if command.ProjectID == "" || command.ActorUserID == "" {
		return agentToolResult{}, fmt.Errorf("source.create command identity is incomplete")
	}
	input, err := decodeSourceCreateActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	outcome, err := s.createProjectSourceActionTx(ctx, tx, principal, project, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode source.create arguments: %w", err)
	}
	delete(arguments, "content")
	delete(arguments, "chapters")
	delete(arguments, "metadata")
	arguments["contentHash"] = outcome.Source.ContentHash
	return sourceCreateAgentResult(arguments, outcome), nil
}

func (s *Server) executeAssetDeleteSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if command.ProjectID == "" || command.ActorUserID == "" {
		return agentToolResult{}, fmt.Errorf("asset.delete command identity is incomplete")
	}
	input, err := decodeAssetDeleteActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	outcome, err := s.deleteCanonicalAssetActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode asset.delete arguments: %w", err)
	}
	return assetDeleteAgentResult(arguments, outcome), nil
}

func (s *Server) executeSourceDeleteChapterSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if command.ProjectID == "" || command.ActorUserID == "" {
		return agentToolResult{}, fmt.Errorf("source.delete_chapter command identity is incomplete")
	}
	input, err := decodeSourceDeleteChapterActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	outcome, err := s.deleteSourceChapterActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode source.delete_chapter arguments: %w", err)
	}
	return sourceDeleteChapterAgentResult(arguments, outcome), nil
}

func (s *Server) executeSourceDeleteSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if command.ProjectID == "" || command.ActorUserID == "" {
		return agentToolResult{}, fmt.Errorf("source.delete command identity is incomplete")
	}
	input, err := decodeSourceDeleteActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	outcome, err := s.deleteProjectSourceActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode source.delete arguments: %w", err)
	}
	return sourceDeleteAgentResult(arguments, outcome), nil
}

func (s *Server) executeAssetUpdateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if command.ProjectID == "" || command.ActorUserID == "" {
		return agentToolResult{}, fmt.Errorf("asset.update command identity is incomplete")
	}
	input, err := decodeAssetUpdateActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	outcome, err := s.updateCanonicalAssetActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode asset.update arguments: %w", err)
	}
	return assetUpdateAgentResult(arguments, outcome), nil
}

func (s *Server) executeSourceUpdateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateSourceUpdateCommand(command); err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeSourceUpdateActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	outcome, err := s.updateProjectSourceActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode source.update arguments: %w", err)
	}
	return sourceUpdateAgentResult(arguments, outcome), nil
}

func (e *projectControlExecutor) executeBoundedSyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	descriptor projectcontrol.Descriptor,
	controllerType projectcontrol.ControllerType,
	controlKeyID string,
	agentTaskID string,
	agentStepID string,
	actionInput json.RawMessage,
	idempotencyKey string,
) (projectcontrol.Command, agentToolResult, bool, error) {
	handler, exists := e.syncActions[descriptor.Name]
	if !exists {
		return projectcontrol.Command{}, agentToolResult{}, false, fmt.Errorf("shared sync action %s is not registered", descriptor.Name)
	}
	var executedResult agentToolResult
	command, replayed, err := e.repository.ExecuteSync(ctx, projectcontrol.CreateCommand{
		OrganizationID: project.OrganizationID,
		WorkspaceID:    project.WorkspaceID,
		ProjectID:      project.ID,
		ActorUserID:    principal.UserID,
		ControllerType: controllerType,
		ControlKeyID:   controlKeyID,
		AgentTaskID:    agentTaskID,
		AgentStepID:    agentStepID,
		Descriptor:     descriptor,
		Input:          actionInput,
		IdempotencyKey: idempotencyKey,
	}, func(ctx context.Context, tx pgx.Tx, command projectcontrol.Command) (json.RawMessage, error) {
		result, err := handler(ctx, tx, principal, project, command, actionInput)
		if err != nil {
			return nil, err
		}
		executedResult = result
		return json.Marshal(result)
	})
	if err != nil {
		return projectcontrol.Command{}, agentToolResult{}, false, err
	}
	if replayed {
		if err := json.Unmarshal(command.Output, &executedResult); err != nil {
			return projectcontrol.Command{}, agentToolResult{}, false, fmt.Errorf("decode replayed %s output: %w", descriptor.Name, err)
		}
	}
	return command, executedResult, replayed, nil
}

func (e *projectControlExecutor) executeManualSyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	actionName string,
	actionInput json.RawMessage,
	idempotencyKey string,
) (projectcontrol.Command, agentToolResult, bool, error) {
	descriptor, exists := e.registry.Get(actionName)
	if !exists || descriptor.ExecutionMode != projectcontrol.ExecutionModeSync {
		return projectcontrol.Command{}, agentToolResult{}, false, newAPIError(
			http.StatusInternalServerError, "PROJECT_CONTROL_ACTION_NOT_SYNC", "手动操作未绑定同步项目控制动作",
		)
	}
	tool, exists := e.agentTools[actionName]
	if !exists {
		return projectcontrol.Command{}, agentToolResult{}, false, fmt.Errorf("manual sync action %s has no shared tool contract", actionName)
	}
	if err := agent.ValidateToolInput(tool, actionInput); err != nil {
		return projectcontrol.Command{}, agentToolResult{}, false, controlValidationError(err.Error())
	}
	if !e.server.agentToolAllowedForProjectKind(string(project.ProjectKind), actionName) {
		return projectcontrol.Command{}, agentToolResult{}, false, newAPIError(http.StatusUnprocessableEntity, "PROJECT_KIND_MISMATCH", "当前项目类型不支持此操作")
	}
	if err := e.server.authorizeAgentToolPermissions(ctx, principal, project, tool); err != nil {
		return projectcontrol.Command{}, agentToolResult{}, false, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return projectcontrol.Command{}, agentToolResult{}, false, newAPIError(http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED", "该写操作需要 Idempotency-Key")
	}
	return e.executeBoundedSyncAction(
		ctx, principal, project, descriptor, projectcontrol.ControllerManual,
		"", "", "", actionInput, idempotencyKey,
	)
}
