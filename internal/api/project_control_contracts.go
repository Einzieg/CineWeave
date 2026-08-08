package api

import (
	"context"
	"fmt"
	"sort"

	editionpkg "github.com/Einzieg/cineweave/internal/edition"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

// ProjectControlActionContracts exposes the exact source inventory used to
// generate the MCP catalog and action coverage matrix. Every exported Core
// action must resolve to a native project-control or shared-domain
// implementation; manual REST operation IDs identify adapters to that same
// implementation.
func ProjectControlActionContracts() ([]projectcontrol.ActionContract, error) {
	descriptors, agentTools, err := projectControlDescriptorSet()
	if err != nil {
		return nil, err
	}
	contracts := make([]projectcontrol.ActionContract, 0, len(descriptors))
	for _, descriptor := range descriptors {
		implementation, implementationKind, native := projectControlNativeImplementation(descriptor.Name)
		_, exportedToAgent := agentTools[descriptor.Name]
		if !native {
			return nil, fmt.Errorf("project control action %s has no shared implementation inventory", descriptor.Name)
		}
		restOperationIDs := append([]string(nil), projectControlRESTOperations[descriptor.Name]...)
		agentToolNames := []string{}
		if exportedToAgent {
			agentToolNames = append(agentToolNames, descriptor.Name)
		}
		contracts = append(contracts, projectcontrol.ActionContract{
			Descriptor: descriptor, AgentToolNames: agentToolNames,
			RESTOperationIDs:    restOperationIDs,
			ImplementationEntry: implementation,
			ImplementationKind:  implementationKind,
			ExportToAgent:       exportedToAgent, ExportToManual: len(restOperationIDs) > 0,
			MigrationStatus: projectcontrol.MigrationStatusMigrated,
		})
	}
	sort.Slice(contracts, func(i, j int) bool {
		return contracts[i].Descriptor.Name < contracts[j].Descriptor.Name
	})
	return contracts, nil
}

// ProjectControlActionContractsForRuntime returns the assembled Core and
// Commercial action inventory used by private release contract generation.
func ProjectControlActionContractsForRuntime(
	ctx context.Context,
	runtime *editionpkg.Runtime,
) ([]projectcontrol.ActionContract, error) {
	contracts, err := ProjectControlActionContracts()
	if err != nil {
		return nil, err
	}
	actions, err := projectControlEditionActionSet(ctx, runtime)
	if err != nil {
		return nil, err
	}
	for _, action := range actions {
		descriptor := action.registration.Descriptor.Clone()
		contracts = append(contracts, projectcontrol.ActionContract{
			Descriptor:            descriptor,
			AgentToolNames:        []string{descriptor.Name},
			CommercialActionNames: []string{action.registration.APIOperationID},
			ImplementationEntry:   "internal/api.(*projectControlExecutor).invokeEditionAction",
			ImplementationKind:    projectcontrol.ImplementationEditionHTTPAdapter,
			ExportToAgent:         true,
			ExportToManual:        true,
			MigrationStatus:       projectcontrol.MigrationStatusAdapterBacked,
		})
	}
	sort.Slice(contracts, func(i, j int) bool {
		return contracts[i].Descriptor.Name < contracts[j].Descriptor.Name
	})
	return contracts, nil
}

func projectControlNativeImplementation(actionName string) (string, projectcontrol.ImplementationKind, bool) {
	if actionName == "agent.ask_user" {
		return "internal/api.(*Server).agentToolAskUser", projectcontrol.ImplementationNativeProjectControl, true
	}
	if actionName == "artifact.list" {
		return "internal/api.(*Server).listProjectArtifactsAction", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "artifact.get" {
		return "internal/api.(*Server).getProjectArtifactAction", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "artifact.preview_url" {
		return "internal/api.(*Server).createProjectArtifactPreviewAction", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "export.download_url" {
		return "internal/api.(*Server).createProjectExportDownloadURLAction", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "final_video.download_url" {
		return "internal/api.(*Server).createFinalVideoDownloadURLAction", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "final_video.activate" {
		return "internal/api.(*Server).activateFinalVideoActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "final_video.delete" {
		return "internal/api.(*Server).deleteFinalVideoActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "timeline.create" {
		return "internal/api.(*Server).createTimelineActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "timeline.update" {
		return "internal/api.(*Server).updateTimelineActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "timeline.delete" {
		return "internal/api.(*Server).deleteTimelineActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "timeline.clip.create" {
		return "internal/api.(*Server).createTimelineClipActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "timeline.update_clip" {
		return "internal/api.(*Server).updateTimelineClipActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "timeline.clip.delete" {
		return "internal/api.(*Server).deleteTimelineClipActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "timeline.clip.reorder" {
		return "internal/api.(*Server).reorderTimelineClipsActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "project.read_summary" {
		return "internal/api.(*Server).readProjectSummaryAction", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "project.deletion_impact" {
		return "internal/api.(*Server).projectDeletionImpact", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "project.production_rebuild_impact" {
		return "internal/api.(*Server).projectVideoProductionRebuildImpactAction", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "project.update" {
		return "internal/api.(*Server).updateProjectActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "project.clear_production_content" {
		return "internal/api.(*Server).clearProjectProductionContentActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "storyboard.list" {
		return "internal/api.(*Server).listStoryboardShotsAction", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "workflow.read_runs" {
		return "internal/api.(*Server).listProjectWorkflowRunsAction", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "workflow.read_nodes" {
		return "internal/api.(*Server).listWorkflowNodesAction", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "workflow.read_shots" {
		return "internal/api.(*Server).listWorkflowShotsAction", projectcontrol.ImplementationSharedDomain, true
	}
	sharedWorkflowActions := map[string]string{
		"asset.batch_generate_images":             "createAssetBatchRun",
		"asset.batch_generate_prompts":            "createAssetBatchRun",
		"export.create":                           "createProjectExportCore",
		"project.production_rebuild":              "createProjectVideoProductionRebuildCore",
		"project.production_rebuild.retry_failed": "retryProjectVideoProductionRebuildCore",
		"script.generate_from_source":             "generateScriptFromSourceCore",
		"shot.render_plan.create":                 "createStoryboardShotRenderPlanCore",
		"shot.render_plan.review_audio":           "startNativeAudioReviewCore",
		"shot.cancel_running_videos":              "runShotProductionActionCore",
		"shot.generate_image_prompts":             "runShotProductionActionCore",
		"shot.generate_missing_images":            "runShotProductionActionCore",
		"shot.generate_missing_videos":            "runShotProductionActionCore",
		"shot.generate_video_prompts":             "runShotProductionActionCore",
		"storyboard.generate_anchor":              "generateStoryboardShotAnchorCore",
		"storyboard.replan_shot_state":            "replanStoryboardShotStateCore",
		"workflow.start":                          "executeWorkflowStartAsyncAction",
		"workflow.cancel":                         "cancelWorkflowRunItem",
		"timeline.compose":                        "composeTimelineAction",
	}
	if method, exists := sharedWorkflowActions[actionName]; exists {
		return "internal/api.(*Server)." + method, projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "review.list_items" {
		return "internal/api.(*Server).listReviewItemsAction", projectcontrol.ImplementationSharedDomain, true
	}
	sharedReviewActions := map[string]string{
		"review.run":          "runProjectReviewCore",
		"review.generate_fix": "generateReviewFixCore",
		"review.apply_fix":    "applyReviewFixActionTx",
		"review.dismiss_fix":  "dismissReviewFixActionTx",
		"review.resolve_item": "updateReviewItemStatusActionTx",
		"review.ignore_item":  "updateReviewItemStatusActionTx",
		"review.reopen_item":  "updateReviewItemStatusActionTx",
	}
	if method, exists := sharedReviewActions[actionName]; exists {
		return "internal/api.(*Server)." + method, projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "provider.list_status" {
		return "internal/api.(*Server).readProviderStatusAction", projectcontrol.ImplementationSharedDomain, true
	}
	sharedProviderActions := map[string]string{
		"provider.attest_video_capability": "provider.Service.CreateVideoCapabilityAttestation",
		"provider.install_catalog_preset":  "provider.Service.InstallCatalogEntry",
		"provider.test_model":              "provider.Service.RecordProviderModelTest",
		"provider.update_account":          "provider.Service.UpdateAccount",
		"provider.update_model":            "provider.Service.UpdateModel",
		"provider.verify_video_capability": "provider.Service.VerifyVideoCapability",
	}
	if method, exists := sharedProviderActions[actionName]; exists {
		return "internal/" + method, projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "prompt.render_test" {
		return "internal/api.(*Server).renderPromptAction", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "project.delete" {
		return "internal/api.(*Server).createProjectDeletionRequestCore", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "project.delete.retry" {
		return "internal/api.(*Server).retryProjectDeletionRequestCore", projectcontrol.ImplementationSharedDomain, true
	}
	sharedCommerceReadActions := map[string]string{
		"commerce.project.read_summary":    "executeCommerceProjectSummaryReadAction",
		"commerce.product.get":             "executeCommerceProductGetReadAction",
		"commerce.product.references.list": "executeCommerceProductReferencesReadAction",
		"commerce.product.versions.list":   "executeCommerceProductVersionsReadAction",
		"commerce.script.derivation.get":   "executeCommerceScriptDerivationGetReadAction",
		"commerce.script.get":              "executeCommerceScriptGetReadAction",
		"commerce.script.list":             "executeCommerceScriptListReadAction",
		"commerce.video.get":               "executeCommerceVideoGetReadAction",
		"commerce.video.list":              "executeCommerceVideoListReadAction",
		"commerce.video.options":           "executeCommerceVideoOptionsReadAction",
	}
	if method, exists := sharedCommerceReadActions[actionName]; exists {
		return "internal/api.(*Server)." + method, projectcontrol.ImplementationSharedDomain, true
	}
	sharedCommerceProductActions := map[string]string{
		"commerce.attachment.assign":             "assignCommerceAttachmentActionTx",
		"commerce.product.rebuild":               "executeCommerceProductRebuildActionTx",
		"commerce.product.rebuild_impact":        "planCommerceProductRebuildActionTx",
		"commerce.product.reference.archive":     "archiveCommerceProductReferenceActionTx",
		"commerce.product.reference.set_primary": "setPrimaryCommerceProductReferenceActionTx",
		"commerce.product.reference.update":      "updateCommerceProductReferenceActionTx",
		"commerce.product.update":                "updateCommerceProductActionTx",
		"commerce.product.version.create":        "createCommerceProductVersionActionTx",
	}
	if method, exists := sharedCommerceProductActions[actionName]; exists {
		return "internal/api.(*Server)." + method, projectcontrol.ImplementationSharedDomain, true
	}
	sharedCommerceScriptActions := map[string]string{
		"commerce.script.archive":                 "archiveCommerceScriptUnitActionTx",
		"commerce.script.create":                  "createCommerceScriptUnitActionTx",
		"commerce.script.create_language_variant": "duplicateCommerceScriptUnitActionTx",
		"commerce.script.defaults.update":         "updateCommerceScriptDefaultsActionTx",
		"commerce.script.duplicate":               "duplicateCommerceScriptUnitActionTx",
		"commerce.script.rebuild_impact":          "planCommerceScriptRebuildActionTx",
		"commerce.script.derive.preview":          "executeCommerceScriptDerivationPreviewAsyncAction",
		"commerce.script.derive.batch":            "executeCommerceScriptDerivationBatchAsyncAction",
		"commerce.script.derive.retry_failed":     "executeCommerceScriptDerivationRetryAsyncAction",
		"commerce.script.derive.cancel":           "executeCommerceScriptDerivationCancelAsyncAction",
		"commerce.script.rebuild":                 "executeCommerceScriptRebuildAsyncAction",
		"commerce.video.generate":                 "executeCommerceDirectVideoGenerateAsyncAction",
		"commerce.video.cancel":                   "executeCommerceDirectVideoCancelAsyncAction",
		"commerce.script.reference.archive":       "archiveCommerceScriptReferenceActionTx",
		"commerce.script.reorder":                 "reorderCommerceScriptUnitsActionTx",
		"commerce.script.revise":                  "executeCommerceScriptReviseAsyncAction",
		"commerce.script.update":                  "updateCommerceScriptUnitActionTx",
	}
	if method, exists := sharedCommerceScriptActions[actionName]; exists {
		return "internal/api.(*Server)." + method, projectcontrol.ImplementationSharedDomain, true
	}
	sharedPromptVersionActions := map[string]string{
		"prompt.create_version":   "createPromptVersionActionTx",
		"prompt.activate_version": "activatePromptVersionActionTx",
	}
	if method, exists := sharedPromptVersionActions[actionName]; exists {
		return "internal/api.(*Server)." + method, projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "shot.status" {
		return "internal/api.(*Server).readShotStatusAction", projectcontrol.ImplementationSharedDomain, true
	}
	sharedStoryboardActions := map[string]string{
		"storyboard.activate_plan":  "activateStoryboardPlanActionTx",
		"storyboard.approve_anchor": "reviewStoryboardShotAnchorActionTx",
		"storyboard.create_shot":    "createStoryboardShotActionTx",
		"storyboard.delete_shot":    "deleteStoryboardShotActionTx",
		"storyboard.merge_shots":    "mergeStoryboardShotsActionTx",
		"storyboard.reject_anchor":  "reviewStoryboardShotAnchorActionTx",
		"storyboard.reorder":        "reorderStoryboardShotsActionTx",
		"storyboard.review_shot":    "reviewStoryboardShotActionTx",
		"storyboard.split_shot":     "splitStoryboardShotActionTx",
		"storyboard.unlink_media":   "unlinkStoryboardShotMediaActionTx",
		"storyboard.update_shot":    "updateStoryboardShotAction",
	}
	if method, exists := sharedStoryboardActions[actionName]; exists {
		return "internal/api.(*Server)." + method, projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "shot.render_plan.verify_audio" {
		return "internal/api.(*Server).verifyStoryboardShotRenderPlanAudioActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "shot.video_prompt.approve" || actionName == "shot.video_prompt.reject" {
		return "internal/api.(*Server).reviewVideoPromptPlanActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "shot.video_prompt.create_revision" {
		return "internal/api.(*Server).createManualVideoPromptPlanRevisionActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "shot_asset.list_requirements" {
		return "internal/api.(*Server).listShotAssetRequirementsAction", projectcontrol.ImplementationSharedDomain, true
	}
	sharedShotAssetRequirementActions := map[string]string{
		"shot_asset.generate_derived_image": "createDerivedAssetImageAction",
		"shot_asset.review_requirements":    "batchReviewShotAssetRequirementsActionTx",
		"shot_asset.update_requirement":     "updateShotAssetRequirementActionTx",
		"shot_asset.skip_requirement":       "skipShotAssetRequirementActionTx",
	}
	if method, exists := sharedShotAssetRequirementActions[actionName]; exists {
		return "internal/api.(*Server)." + method, projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "asset.list" {
		return "internal/api.(*Server).listCanonicalAssetsAction", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "asset.get" {
		return "internal/api.(*Server).getCanonicalAssetAction", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "asset.impact" {
		return "internal/api.(*Server).getCanonicalAssetImpactAction", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "asset.reference.list" {
		return "internal/api.(*Server).listAssetReferencesAction", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "asset.reference.create" {
		return "internal/api.(*Server).createAssetReferenceActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "asset.reference.set_primary" {
		return "internal/api.(*Server).setPrimaryAssetReferenceActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "asset.reference.delete" {
		return "internal/api.(*Server).deleteAssetReferenceActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "asset.delete" {
		return "internal/api.(*Server).deleteCanonicalAssetActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "asset.update" {
		return "internal/api.(*Server).updateCanonicalAssetActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "asset.revise_prompt" {
		return "internal/api.(*Server).reviseCanonicalAssetPromptCore", projectcontrol.ImplementationSharedDomain, true
	}
	sharedCharacterVoiceActions := map[string]string{
		"character_voice.create": "createCharacterVoiceActionTx",
		"character_voice.update": "updateCharacterVoiceActionTx",
		"character_voice.delete": "deleteCharacterVoiceActionTx",
	}
	if method, exists := sharedCharacterVoiceActions[actionName]; exists {
		return "internal/api.(*Server)." + method, projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "source.delete" {
		return "internal/api.(*Server).deleteProjectSourceActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "source.create" {
		return "internal/api.(*Server).createProjectSourceActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "source.delete_chapter" {
		return "internal/api.(*Server).deleteSourceChapterActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "source.list" {
		return "internal/api.(*Server).listProjectSourcesAction", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "source.list_chapters" {
		return "internal/api.(*Server).listSourceChaptersAction", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "source.update" {
		return "internal/api.(*Server).updateProjectSourceActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "script.list" {
		return "internal/api.(*Server).listScriptsAction", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "script.get" {
		return "internal/api.(*Server).getScriptAction", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "script.update" {
		return "internal/api.(*Server).updateScriptActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "script.update_episode" {
		return "internal/api.(*Server).updateScriptEpisodeActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "script.delete" {
		return "internal/api.(*Server).deleteScriptActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "script.create" {
		return "internal/api.(*Server).createScriptActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "script.create_version" {
		return "internal/api.(*Server).createScriptVersionActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "script.activate_version" {
		return "internal/api.(*Server).activateScriptVersionActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "script.archive_version" {
		return "internal/api.(*Server).archiveScriptVersionActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "script.rewrite" {
		return "internal/api.(*Server).rewriteScriptCore", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "script.rewrite_preview" {
		return "internal/api.(*Server).rewriteScriptPreviewCore", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "script_scene.update" {
		return "internal/api.(*Server).updateScriptSceneActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "script_scene.review" {
		return "internal/api.(*Server).reviewScriptSceneActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "script_scene.delete" {
		return "internal/api.(*Server).deleteScriptSceneActionTx", projectcontrol.ImplementationSharedDomain, true
	}
	sharedAdaptationActions := map[string]string{
		"novel_event.update":  "updateNovelEventActionTx",
		"novel_event.review":  "reviewNovelEventActionTx",
		"adaptation.create":   "createAdaptationPlanActionTx",
		"adaptation.update":   "updateAdaptationPlanActionTx",
		"adaptation.review":   "reviewAdaptationPlanActionTx",
		"adaptation.activate": "activateAdaptationPlanActionTx",
	}
	if method, exists := sharedAdaptationActions[actionName]; exists {
		return "internal/api.(*Server)." + method, projectcontrol.ImplementationSharedDomain, true
	}
	if actionName == "adaptation.generate_script" {
		return "internal/api.(*Server).generateScriptFromAdaptationPlanCore", projectcontrol.ImplementationSharedDomain, true
	}
	implementations := map[string]string{
		"identity.me": "identityMe", "organization.list": "organizationList",
		"workspace.list": "workspaceList", "project.list": "projectList", "project.get": "projectGet",
		"project.context": "projectContext", "project.capabilities": "projectCapabilities",
		"project.production_status": "projectProductionStatus", "project.task_activity": "projectTaskActivity",
		"content.describe": "contentDescribe", "content.read": "contentRead",
		"content.write.begin": "contentWriteBegin", "content.write.chunk": "contentWriteChunk",
		"content.write.commit": "contentWriteCommit", "content.write.abort": "contentWriteAbort",
		"control.command.list": "commandList", "control.command.get": "commandGet",
		"control.command.events": "commandEvents", "control.command.wait": "commandWait",
		"control.command.cancel": "commandCancel", "control.command.retry": "commandRetry",
		"control.command.resolve": "commandResolve",
	}
	method, exists := implementations[actionName]
	if !exists {
		return "", "", false
	}
	return "internal/api.(*projectControlExecutor)." + method, projectcontrol.ImplementationNativeProjectControl, true
}

var projectControlRESTOperations = map[string][]string{
	"adaptation.activate":                     {"activateAdaptationPlan"},
	"adaptation.create":                       {"createAdaptationPlan"},
	"adaptation.generate_script":              {"generateScriptFromAdaptationPlan"},
	"adaptation.review":                       {"reviewAdaptationPlan"},
	"adaptation.update":                       {"updateAdaptationPlan"},
	"artifact.list":                           {"listArtifacts"},
	"artifact.get":                            {"getArtifact"},
	"artifact.preview_url":                    {"createArtifactPreviewUrl"},
	"asset.batch_generate_images":             {"createAssetBatch", "generateAssetImage", "generateCanonicalAssetImage"},
	"asset.batch_generate_prompts":            {"generateAssetCard"},
	"asset.delete":                            {"deleteCanonicalAsset"},
	"asset.get":                               {"getCanonicalAsset"},
	"asset.impact":                            {"getCanonicalAssetImpact"},
	"asset.list":                              {"listCanonicalAssets"},
	"asset.reference.list":                    {"listAssetReferences"},
	"asset.reference.create":                  {"createAssetReference"},
	"asset.reference.delete":                  {"deleteAssetReference"},
	"asset.reference.set_primary":             {"setPrimaryAssetReference"},
	"asset.update":                            {"updateCanonicalAsset"},
	"character_voice.create":                  {"createCharacterVoice"},
	"character_voice.delete":                  {"deleteCharacterVoice"},
	"character_voice.update":                  {"updateCharacterVoice"},
	"commerce.attachment.assign":              {"assignAgentImageAttachment"},
	"commerce.product.get":                    {"getCommerceProduct"},
	"commerce.product.rebuild":                {"createCommerceProductRebuild"},
	"commerce.product.rebuild_impact":         {"getCommerceProductRebuildImpact"},
	"commerce.product.reference.archive":      {"archiveCommerceProductReference"},
	"commerce.product.reference.update":       {"updateCommerceProductReference"},
	"commerce.product.references.list":        {"listCommerceProductReferences"},
	"commerce.product.update":                 {"updateCommerceProduct"},
	"commerce.product.version.create":         {"createCommerceProductVersion"},
	"commerce.product.versions.list":          {"listCommerceProductVersions"},
	"commerce.project.read_summary":           {"getCommerceProjectProductionStatus"},
	"commerce.script.archive":                 {"archiveCommerceScriptUnit"},
	"commerce.script.create":                  {"createCommerceScriptUnit"},
	"commerce.script.create_language_variant": {"createCommerceScriptLanguageVariant"},
	"commerce.script.defaults.update":         {"updateCommerceScriptUnitDefaults"},
	"commerce.script.derivation.get":          {"getCommerceScriptDerivation"},
	"commerce.script.derive.batch":            {"createCommerceScriptDerivation"},
	"commerce.script.derive.cancel":           {"cancelCommerceScriptDerivation"},
	"commerce.script.derive.retry_failed":     {"retryCommerceScriptDerivation"},
	"commerce.script.duplicate":               {"duplicateCommerceScriptUnit"},
	"commerce.script.get":                     {"getCommerceScriptUnit"},
	"commerce.script.list":                    {"listCommerceScriptUnits"},
	"commerce.script.reorder":                 {"reorderCommerceScriptUnits"},
	"commerce.script.rebuild":                 {"createCommerceScriptUnitRebuild"},
	"commerce.script.rebuild_impact":          {"getCommerceScriptUnitRebuildImpact"},
	"commerce.script.reference.archive":       {"archiveCommerceScriptReference"},
	"commerce.script.update":                  {"updateCommerceScriptUnit"},
	"commerce.video.cancel":                   {"cancelCommerceDirectVideo"},
	"commerce.video.generate":                 {"createCommerceDirectVideo"},
	"commerce.video.get":                      {"getCommerceDirectVideo"},
	"commerce.video.list":                     {"listCommerceDirectVideos"},
	"commerce.video.options":                  {"getCommerceDirectVideoOptions"},
	"control.command.cancel":                  {"cancelProjectControlCommand"},
	"control.command.events":                  {"listProjectControlCommandEvents"},
	"control.command.get":                     {"getProjectControlCommand"},
	"control.command.list":                    {"listProjectControlCommands"},
	"control.command.resolve":                 {"resolveProjectControlCommand"},
	"control.command.retry":                   {"retryProjectControlCommand"},
	"control.command.wait":                    {"waitProjectControlCommand"},
	"export.create":                           {"createProjectExport"},
	"export.download_url":                     {"createProjectExportDownloadUrl"},
	"final_video.activate":                    {"activateFinalVideo"},
	"final_video.delete":                      {"deleteFinalVideo"},
	"final_video.download_url":                {"createFinalVideoDownloadUrl"},
	"novel_event.review":                      {"reviewNovelEvent"},
	"novel_event.update":                      {"updateNovelEvent"},
	"project.delete":                          {"createProjectDeletionRequest"},
	"project.delete.retry":                    {"retryProjectDeletionRequest"},
	"project.deletion_impact":                 {"getProjectDeletionImpact"},
	"project.get":                             {"getProject"},
	"project.list":                            {"listProjects"},
	"project.production_status":               {"getProductionStatus"},
	"project.production_rebuild":              {"createProjectVideoProductionRebuild"},
	"project.production_rebuild_impact":       {"getProjectVideoProductionRebuildImpact"},
	"project.production_rebuild.retry_failed": {"retryFailedProjectVideoProductionRebuildItems"},
	"project.update":                          {"updateProject"},
	"prompt.activate_version":                 {"activatePromptVersion"},
	"prompt.create_version":                   {"createPromptVersion"},
	"prompt.render_test":                      {"renderPromptTest"},
	"provider.attest_video_capability":        {"createProviderModelVideoCapabilityAttestation"},
	"provider.install_catalog_preset":         {"installProviderCatalogEntry"},
	"provider.test_model":                     {"testProviderModel"},
	"provider.update_account":                 {"updateProviderAccount"},
	"provider.update_model":                   {"updateProviderModel"},
	"provider.verify_video_capability":        {"verifyProviderModelVideoCapabilities"},
	"review.apply_fix":                        {"applyProjectReviewFix"},
	"review.dismiss_fix":                      {"dismissProjectReviewFix"},
	"review.generate_fix":                     {"generateProjectReviewFix"},
	"review.ignore_item":                      {"ignoreProjectReviewItem"},
	"review.list_items":                       {"listProjectReviewItems"},
	"review.reopen_item":                      {"reopenProjectReviewItem"},
	"review.resolve_item":                     {"resolveProjectReviewItem"},
	"review.run":                              {"runProjectReview"},
	"script.activate_version":                 {"activateScriptVersion"},
	"script.create":                           {"createScript"},
	"script.create_version":                   {"createScriptVersion"},
	"script.delete":                           {"deleteScript"},
	"script.archive_version":                  {"deleteScriptVersion"},
	"script.get":                              {"getScript"},
	"script.list":                             {"listScripts"},
	"script.rewrite":                          {"rewriteScriptFromAgent"},
	"script.update":                           {"updateScript"},
	"script.update_episode":                   {"updateScriptEpisode"},
	"script_scene.delete":                     {"deleteScriptScene"},
	"script_scene.review":                     {"reviewScriptScene"},
	"script_scene.update":                     {"updateScriptScene"},
	"shot.render_plan.create":                 {"createStoryboardShotRenderPlan"},
	"shot.render_plan.review_audio":           {"reviewStoryboardShotRenderPlanAudio"},
	"shot.render_plan.verify_audio":           {"verifyStoryboardShotRenderPlanAudio"},
	"shot.video_prompt.approve":               {"approveVideoPromptPlan"},
	"shot.video_prompt.create_revision":       {"createManualVideoPromptPlanRevision"},
	"shot.video_prompt.reject":                {"rejectVideoPromptPlan"},
	"shot_asset.generate_derived_image":       {"generateDerivedAssetImage"},
	"shot_asset.list_requirements":            {"listShotAssetRequirements"},
	"shot_asset.review_requirements":          {"batchReviewShotAssetRequirements", "reviewShotAssetRequirement"},
	"shot_asset.skip_requirement":             {"skipShotAssetRequirement"},
	"shot_asset.update_requirement":           {"updateShotAssetRequirement"},
	"source.delete":                           {"deleteProjectSource"},
	"source.delete_chapter":                   {"deleteSourceChapter"},
	"source.create":                           {"createProjectSource"},
	"source.list":                             {"listProjectSources"},
	"source.list_chapters":                    {"listSourceChapters"},
	"source.update":                           {"updateProjectSource"},
	"storyboard.activate_plan":                {"activateStoryboardPlan"},
	"storyboard.approve_anchor":               {"approveStoryboardShotAnchor"},
	"storyboard.create_shot":                  {"createStoryboardShot"},
	"storyboard.delete_shot":                  {"deleteStoryboardShot"},
	"storyboard.generate_anchor":              {"generateStoryboardShotAnchor"},
	"storyboard.merge_shots":                  {"mergeStoryboardShots"},
	"storyboard.reject_anchor":                {"rejectStoryboardShotAnchor"},
	"storyboard.reorder":                      {"reorderStoryboardShots"},
	"storyboard.replan_shot_state":            {"replanStoryboardShotState"},
	"storyboard.review_shot":                  {"reviewStoryboardShot"},
	"storyboard.split_shot":                   {"splitStoryboardShot"},
	"storyboard.unlink_media":                 {"unlinkStoryboardShotMedia"},
	"storyboard.update_shot":                  {"updateStoryboardShot", "updateStoryboardShotTiming", "updateStoryboardShotTransition"},
	"timeline.clip.create":                    {"createTimelineClip"},
	"timeline.clip.delete":                    {"deleteTimelineClip"},
	"timeline.clip.reorder":                   {"reorderTimelineClips"},
	"timeline.compose":                        {"composeTimeline"},
	"timeline.create":                         {"createProjectTimeline"},
	"timeline.delete":                         {"deleteProjectTimeline"},
	"timeline.update":                         {"updateProjectTimeline"},
	"timeline.update_clip":                    {"updateTimelineClip"},
	"workflow.cancel":                         {"cancelWorkflowRun"},
	"workflow.read_nodes":                     {"listWorkflowNodeRuns"},
	"workflow.read_runs":                      {"listWorkflowRuns"},
	"workflow.read_shots":                     {"listWorkflowRunShots"},
	"workflow.start":                          {"analyzeScriptAssets", "analyzeScriptEpisodeTiming", "createWorkflowRun", "extractNovelEvents", "generateAdaptationPlan", "generateScriptStoryboard", "parseScriptScenes", "produceScriptEpisodeAudio", "runProductionAction", "runShotProductionAction"},
	"shot.generate_video_prompts":             {"generateVideoPromptsBatch"},
	"shot.generate_missing_videos":            {"generateShotVideosBatch"},
}
