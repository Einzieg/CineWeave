package workflows

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Einzieg/cineweave/internal/provider"
	storyboardpkg "github.com/Einzieg/cineweave/internal/storyboard"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStoryboardEpisodeV2ActivitiesPersistCompletePlan(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	orgID, userID, projectID, workflowRunID, textModelID, _ := seedWorkflowGatewayIntegrationData(t, ctx, pool)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID) })
	if _, err := pool.Exec(ctx, `UPDATE projects SET audio_strategy = 'native_av', audio_requirement = 'required' WHERE id = $1`, projectID); err != nil {
		t.Fatalf("require native audio: %v", err)
	}

	scriptID, versionID, episodeID, sceneIDs := seedStoryboardEpisodeV2Script(t, ctx, pool, orgID, projectID, userID)
	assets := seedStoryboardEpisodeV2Assets(t, ctx, pool, orgID, projectID, userID)
	callIDs := seedStoryboardEpisodeV2ProviderCalls(t, ctx, pool, orgID, projectID, workflowRunID, textModelID, 13)

	var timingOutput TimingAnalysisActivityOutput
	gateway := httptest.NewServer(mockStoryboardEpisodeV2Gateway(t, textModelID, callIDs, assets, sceneIDs, &timingOutput))
	defer gateway.Close()
	activities := NewActivities(pool, newWorkflowMemoryStorage(), &provider.GatewayClient{
		BaseURL: gateway.URL, Token: "workflow-service-token", Client: gateway.Client(),
	})

	var err error
	timingOutput, err = activities.AnalyzeEpisodeTiming(ctx, AnalyzeEpisodeTimingInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ScriptID: scriptID, ScriptVersionID: versionID, ScriptEpisodeID: episodeID,
	})
	if err != nil {
		t.Fatalf("AnalyzeEpisodeTiming: %v", err)
	}
	if len(timingOutput.Scenes) != 2 || timingOutput.EstimatedDurationTicks <= 0 {
		t.Fatalf("timing output = %+v", timingOutput)
	}
	blueprint, err := activities.BuildEpisodeContinuityBlueprint(ctx, BuildEpisodeContinuityBlueprintInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ScriptID: scriptID, ScriptVersionID: versionID,
		ScriptEpisodeID: episodeID, PacingProfile: "standard", Timing: timingOutput,
	})
	if err != nil {
		t.Fatalf("BuildEpisodeContinuityBlueprint: %v", err)
	}
	if len(blueprint.ScenePlans) != 2 || len(blueprint.Blueprint.Dependencies) != 1 {
		t.Fatalf("blueprint output = %+v", blueprint)
	}
	for index, scene := range blueprint.ScenePlans {
		output, err := activities.PlanStoryboardScene(ctx, PlanStoryboardSceneInput{
			OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
			CreatedBy: userID, ScriptID: scriptID, ScriptVersionID: versionID,
			ScriptEpisodeID: episodeID, StoryboardPlanID: blueprint.StoryboardPlanID,
			BlueprintID: blueprint.BlueprintID, ScenePlanID: scene.ID,
			SceneKey: scene.SceneKey, SceneOrdinal: scene.SceneOrdinal,
		})
		if err != nil {
			t.Fatalf("PlanStoryboardScene %d: %v", index, err)
		}
		if output.Status != "ready" || len(output.Shots) == 0 {
			t.Fatalf("scene output %d = %+v", index, output)
		}
		persistStoryboardEpisodeV2ReferencePacks(t, ctx, pool, activities, orgID, projectID, workflowRunID, userID, output.Shots)
		for _, shot := range output.Shots {
			prepared, err := activities.PrepareShotImagePrompt(ctx, PrepareShotImagePromptInput{
				OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
				CreatedBy: userID, ShotID: shot.ID, ShotIndex: shot.ShotOrdinal,
				ShotNo: shot.ShotOrdinal + 1, WorkflowPrompt: "生成当前镜头首帧提示词",
				AspectRatio: "16:9", Size: "1536x1024",
			})
			if err != nil {
				t.Fatalf("PrepareShotImagePrompt %s: %v", shot.ID, err)
			}
			if prepared.GenerationTemplateKey != "video_profile.single_frame_i2v.anchor.generate" ||
				prepared.ReviewTemplateKey != "video_profile.single_frame_i2v.anchor.review" ||
				prepared.GenerationContract == nil || prepared.ReviewContract == nil ||
				prepared.PromptContextPlanID == "" || prepared.ReferencePackID == "" {
				t.Fatalf("prepared image prompt contract = %+v", prepared)
			}
			approveStoryboardEpisodeV2Anchor(t, ctx, pool, orgID, projectID, userID, shot.ID, shot.ShotOrdinal)
			videoPrepared, err := activities.PrepareShotVideoPrompt(ctx, PrepareShotVideoPromptInput{
				OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
				CreatedBy: userID, ShotID: shot.ID, ShotIndex: shot.ShotOrdinal,
				ShotNo: shot.ShotOrdinal + 1, WorkflowPrompt: "生成当前镜头视频提示词",
				AspectRatio: "16:9", Resolution: "720p",
			})
			if err != nil {
				t.Fatalf("PrepareShotVideoPrompt %s: %v", shot.ID, err)
			}
			if videoPrepared.GenerationTemplateKey != "video_profile.single_frame_i2v.video.generate" ||
				videoPrepared.ReviewTemplateKey != "video_profile.single_frame_i2v.video.review" ||
				videoPrepared.GenerationContract == nil || videoPrepared.ReviewContract == nil ||
				videoPrepared.PromptContextPlanID == "" || videoPrepared.ReferencePackID == "" ||
				videoPrepared.VideoPromptPlanID == "" || !videoPrepared.NativeAudioRequired ||
				!videoPrepared.ModelSupportsNativeAudio {
				t.Fatalf("prepared video prompt contract = %+v", videoPrepared)
			}
		}
	}
	review, err := activities.ReviewStoryboardPlan(ctx, ReviewStoryboardPlanInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ScriptEpisodeID: episodeID,
		StoryboardPlanID: blueprint.StoryboardPlanID, ReviewAttempt: 1,
	})
	if err != nil {
		t.Fatalf("ReviewStoryboardPlan: %v", err)
	}
	if !review.Approved || !review.DeterministicReport.Valid {
		t.Fatalf("review output = %+v", review)
	}
	activated, err := activities.ActivateStoryboardPlan(ctx, ActivateStoryboardPlanActivityInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ScriptID: scriptID, ScriptVersionID: versionID,
		ScriptEpisodeID: episodeID, EpisodeIndex: 1, EpisodeTotal: 1,
		EpisodeTitle: "第一集", StoryboardPlanID: blueprint.StoryboardPlanID,
	})
	if err != nil {
		t.Fatalf("ActivateStoryboardPlan: %v", err)
	}
	if len(activated.Shots) < 2 || len(activated.Requirements) < 2 || activated.StoryboardArtifactID == "" {
		t.Fatalf("activation output = %+v", activated)
	}
	videoModelID := seedStoryboardEpisodeV2VideoModel(t, ctx, pool, textModelID)
	assertStoryboardEpisodeV2VideoPlans(t, ctx, pool, activities, orgID, projectID, workflowRunID, videoModelID, activated.Shots)

	assertStoryboardEpisodeV2Persistence(t, ctx, pool, projectID, workflowRunID, episodeID, blueprint.StoryboardPlanID)
}

func TestFirstLastFrameProfilePersistsAndExecutesTwoApprovedAnchors(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	orgID, userID, projectID, workflowRunID, textModelID, imageModelID := seedWorkflowGatewayIntegrationDataForProfile(
		t, ctx, pool, videoproduction.ProfileFirstLastFrame,
	)
	if _, err := pool.Exec(ctx, `UPDATE projects SET audio_strategy = 'native_av', audio_requirement = 'required' WHERE id = $1`, projectID); err != nil {
		t.Fatalf("require native audio: %v", err)
	}
	scriptID, versionID, episodeID, sceneIDs := seedStoryboardEpisodeV2Script(t, ctx, pool, orgID, projectID, userID)
	assets := seedStoryboardEpisodeV2Assets(t, ctx, pool, orgID, projectID, userID)
	callIDs := seedStoryboardEpisodeV2ProviderCalls(t, ctx, pool, orgID, projectID, workflowRunID, textModelID, 9)
	anchorRoles := []string{
		videoproduction.AnchorRolePlannedFirstFrame,
		videoproduction.AnchorRolePlannedLastFrame,
	}
	imageOutputs := seedStoryboardEpisodeV2AnchorImageOutputs(
		t, ctx, pool, orgID, projectID, workflowRunID, userID, imageModelID, anchorRoles,
	)

	var timingOutput TimingAnalysisActivityOutput
	gateway := httptest.NewServer(mockStoryboardEpisodeV2GatewayWithConfig(
		t, textModelID, callIDs, assets, sceneIDs, &timingOutput,
		storyboardEpisodeV2GatewayConfig{FirstLast: true, ImageOutput: imageOutputs},
	))
	defer gateway.Close()
	activities := NewActivities(pool, newWorkflowMemoryStorage(), &provider.GatewayClient{
		BaseURL: gateway.URL, Token: "workflow-service-token", Client: gateway.Client(),
	})

	var err error
	timingOutput, err = activities.AnalyzeEpisodeTiming(ctx, AnalyzeEpisodeTimingInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ScriptID: scriptID, ScriptVersionID: versionID, ScriptEpisodeID: episodeID,
	})
	if err != nil {
		t.Fatalf("AnalyzeEpisodeTiming: %v", err)
	}
	blueprint, err := activities.BuildEpisodeContinuityBlueprint(ctx, BuildEpisodeContinuityBlueprintInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ScriptID: scriptID, ScriptVersionID: versionID,
		ScriptEpisodeID: episodeID, PacingProfile: "standard", Timing: timingOutput,
	})
	if err != nil {
		t.Fatalf("BuildEpisodeContinuityBlueprint: %v", err)
	}
	planned, err := activities.PlanStoryboardScene(ctx, PlanStoryboardSceneInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ScriptID: scriptID, ScriptVersionID: versionID,
		ScriptEpisodeID: episodeID, StoryboardPlanID: blueprint.StoryboardPlanID,
		BlueprintID: blueprint.BlueprintID, ScenePlanID: blueprint.ScenePlans[0].ID,
		SceneKey: blueprint.ScenePlans[0].SceneKey, SceneOrdinal: blueprint.ScenePlans[0].SceneOrdinal,
	})
	if err != nil {
		t.Fatalf("PlanStoryboardScene: %v", err)
	}
	if len(planned.Shots) != 1 {
		t.Fatalf("planned shots = %d, want 1", len(planned.Shots))
	}
	shot := mustLoadStoryboardShot(t, ctx, activities, projectID, planned.Shots[0].ID)

	rows, err := pool.Query(ctx, `
		SELECT anchor.anchor_role, state.state_role
		FROM shot_visual_anchors anchor
		JOIN storyboard_shot_state_versions state ON state.id = anchor.shot_state_version_id
		WHERE anchor.storyboard_shot_id = $1 AND anchor.status <> 'archived'
		ORDER BY anchor.anchor_role
	`, shot.ID)
	if err != nil {
		t.Fatalf("load planned anchors: %v", err)
	}
	stateRoles := map[string]string{}
	for rows.Next() {
		var role, stateRole string
		if err := rows.Scan(&role, &stateRole); err != nil {
			rows.Close()
			t.Fatalf("scan planned anchor: %v", err)
		}
		stateRoles[role] = stateRole
	}
	rows.Close()
	if stateRoles[videoproduction.AnchorRolePlannedFirstFrame] != videoproduction.StateRolePlannedEntry ||
		stateRoles[videoproduction.AnchorRolePlannedLastFrame] != videoproduction.StateRolePlannedExit {
		t.Fatalf("anchor state roles = %+v", stateRoles)
	}
	if _, err := pool.Exec(ctx, `UPDATE storyboard_shots SET stale_state = 'needs_regeneration' WHERE id = $1`, shot.ID); err != nil {
		t.Fatalf("mark shot stale before anchor regeneration: %v", err)
	}

	for _, role := range anchorRoles {
		prepared, err := activities.PrepareShotImagePrompt(ctx, PrepareShotImagePromptInput{
			OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
			CreatedBy: userID, ShotID: shot.ID, ShotIndex: shot.ShotIndex, ShotNo: shot.ShotNo,
			AnchorRole: role, WorkflowPrompt: "生成首尾帧锚点", AspectRatio: "16:9", Size: "1536x864", Force: true,
		})
		if err != nil {
			t.Fatalf("PrepareShotImagePrompt %s: %v", role, err)
		}
		if prepared.AnchorRole != role || prepared.GenerationTemplateKey != "video_profile.first_last_frame.anchor.generate" ||
			prepared.ReviewTemplateKey != "video_profile.first_last_frame.anchor.review" || prepared.PromptContextPlanID == "" || prepared.ReferencePackID == "" {
			t.Fatalf("prepared %s prompt = %+v", role, prepared)
		}
		generated, err := activities.GenerateShotImage(ctx, GenerateShotImageInput{
			OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
			CreatedBy: userID, ShotID: shot.ID, ShotIndex: shot.ShotIndex, ShotNo: shot.ShotNo,
			AnchorRole: role, ProfileKey: videoproduction.ProfileFirstLastFrame,
			WorkflowPrompt: "生成首尾帧锚点", AspectRatio: "16:9", Force: true,
		})
		if err != nil {
			t.Fatalf("GenerateShotImage %s: %v", role, err)
		}
		if generated.AnchorRole != role || generated.VisualAnchorID == "" || generated.ImageArtifactID == "" || generated.ImageMediaFileID == "" {
			t.Fatalf("generated %s anchor = %+v", role, generated)
		}
	}
	var staleState string
	if err := pool.QueryRow(ctx, `SELECT stale_state FROM storyboard_shots WHERE id = $1`, shot.ID).Scan(&staleState); err != nil {
		t.Fatalf("load regenerated shot stale state: %v", err)
	}
	if staleState != "fresh" {
		t.Fatalf("regenerated shot stale state = %q, want fresh", staleState)
	}

	var approvedAnchors, pairReviewedAnchors int
	if err := pool.QueryRow(ctx, `
		WITH latest AS (
			SELECT DISTINCT ON (anchor_role) status, review_status, metadata
			FROM shot_visual_anchors
			WHERE storyboard_shot_id = $1
			  AND anchor_role IN ('planned_first_frame', 'planned_last_frame')
			ORDER BY anchor_role, revision DESC
		)
		SELECT COUNT(*) FILTER (WHERE status = 'ready' AND review_status = 'approved'),
		       COUNT(*) FILTER (WHERE metadata ? 'firstLastPairReview')
		FROM latest
	`, shot.ID).Scan(&approvedAnchors, &pairReviewedAnchors); err != nil {
		t.Fatalf("verify approved anchor pair: %v", err)
	}
	if approvedAnchors != 2 || pairReviewedAnchors != 2 {
		t.Fatalf("approved anchors=%d pair reviewed=%d", approvedAnchors, pairReviewedAnchors)
	}

	videoPrepared, err := activities.PrepareShotVideoPrompt(ctx, PrepareShotVideoPromptInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ShotID: shot.ID, ShotIndex: shot.ShotIndex, ShotNo: shot.ShotNo,
		WorkflowPrompt: "生成首尾帧视频提示词", AspectRatio: "16:9", Resolution: "720p", Force: true,
	})
	if err != nil {
		t.Fatalf("PrepareShotVideoPrompt: %v", err)
	}
	if videoPrepared.GenerationTemplateKey != "video_profile.first_last_frame.video.generate" ||
		videoPrepared.ReviewTemplateKey != "video_profile.first_last_frame.video.review" ||
		videoPrepared.ReferencePackID == "" || videoPrepared.VideoPromptPlanID == "" {
		t.Fatalf("prepared first-last video prompt = %+v", videoPrepared)
	}
	var videoReferenceCount, firstReferenceCount, lastReferenceCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE role = 'first_frame'),
		       COUNT(*) FILTER (WHERE role = 'last_frame')
		FROM shot_reference_pack_items WHERE reference_pack_id = $1
	`, videoPrepared.ReferencePackID).Scan(&videoReferenceCount, &firstReferenceCount, &lastReferenceCount); err != nil {
		t.Fatalf("verify first-last video reference pack: %v", err)
	}
	if videoReferenceCount != 2 || firstReferenceCount != 1 || lastReferenceCount != 1 {
		t.Fatalf("video references total=%d first=%d last=%d", videoReferenceCount, firstReferenceCount, lastReferenceCount)
	}

	videoModelID := seedStoryboardEpisodeV2FirstLastVideoModel(t, ctx, pool, textModelID)
	project, err := activities.projectProductionSettings(ctx, projectID)
	if err != nil {
		t.Fatalf("load project production settings: %v", err)
	}
	contract, err := activities.loadApprovedShotVideoExecutionContract(ctx, orgID, project, shot)
	if err != nil {
		t.Fatalf("load approved first-last contract: %v", err)
	}
	if len(contract.References) != 2 || contract.RequiredInitialInputContract != provider.VideoInputContractFirstLastFrames {
		t.Fatalf("approved first-last contract = %+v", contract)
	}
	vault, err := provider.NewVault("")
	if err != nil {
		t.Fatalf("create provider vault: %v", err)
	}
	service := provider.NewService(pool, vault)
	service.EnableGatewayRuntime()
	execution, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		NodeKey: "provider-plan-first-last-" + shot.ID, NodeType: "video.plan",
	})
	if err != nil {
		t.Fatalf("start first-last video plan node: %v", err)
	}
	request := provider.GatewayVideoPlanRequest{
		OrganizationID: orgID, ProjectID: projectID,
		ProductionGenerationID:            execution.ProductionGenerationID,
		VideoProductionBindingID:          execution.VideoProductionBindingID,
		VideoProductionBindingRevision:    execution.VideoProductionBindingRevision,
		ProductionProfileVersionID:        project.VideoProductionProfileVersionID,
		ProductionProfileSnapshotHash:     project.VideoProductionProfileHash,
		CompatibilityPolicy:               project.VideoProductionCompatibilityPolicy,
		RequiredInitialInputContract:      project.VideoProductionRequiredInitialInputContract,
		AllowedContinuationInputContracts: project.VideoProductionAllowedContinuationInputContracts,
		InputContractVersion:              project.VideoProductionInputContract,
		ShotStateRevision:                 contract.ShotStateRevision,
		ShotStateHash:                     contract.ShotStateHash,
		TransitionHash:                    contract.TransitionHash,
		ReferencePackID:                   contract.ReferencePackID,
		ReferencePackHash:                 contract.ReferencePackHash,
		PromptContextPlanID:               contract.PromptContextPlanID,
		PromptContextPlanHash:             contract.PromptContextPlanHash,
		VideoPromptPlanID:                 contract.VideoPromptPlanID,
		NativeAudioRequired:               contract.NativeAudioRequired,
		WorkflowRunID:                     workflowRunID,
		NodeRunID:                         execution.NodeRunID,
		NodeExecutionToken:                execution.ExecutionToken,
		NodeAttemptGeneration:             execution.AttemptGeneration,
		StoryboardPlanID:                  shot.StoryboardPlanID,
		StoryboardShotID:                  shot.ID,
		ProviderModelID:                   videoModelID,
		TaskType:                          "video.image_to_video",
		TargetDurationTicks:               shot.PlannedDurationTicks,
		TimelineTimebase:                  shot.TimelineTimebase,
		FPSNumerator:                      int64(shot.FPSNumerator),
		FPSDenominator:                    int64(shot.FPSDenominator),
		AudioStrategy:                     contract.AudioStrategy,
		AudioRequirement:                  contract.AudioRequirement,
		DialogueLanguage:                  "zh-CN",
		HasDialogue:                       len(contract.DialogueCues) > 0,
		ReferenceMode:                     provider.VideoInputContractFirstLastFrames,
		AspectRatio:                       "16:9",
		Resolution:                        "720p",
		PromptLanguage:                    "zh-CN",
		DialogueSpans:                     contract.DialogueCues,
	}
	plan, err := service.PlanVideo(ctx, request)
	if err != nil {
		t.Fatalf("PlanVideo first-last: %v", err)
	}
	if plan.InitialInputContractSnapshot.ContractKey != provider.VideoInputContractFirstLastFrames || len(plan.Segments) != 1 {
		t.Fatalf("first-last plan = %+v", plan)
	}

	longDurationTicks := int64(31) * shot.TimelineTimebase
	if _, err := pool.Exec(ctx, `
		UPDATE storyboard_shots
		SET end_tick = start_tick + $2, updated_at = now()
		WHERE id = $1
	`, shot.ID, longDurationTicks); err != nil {
		t.Fatalf("extend shot duration for contract rejection: %v", err)
	}
	longExecution, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		NodeKey: "provider-plan-first-last-too-long-" + shot.ID, NodeType: "video.plan",
	})
	if err != nil {
		t.Fatalf("start long first-last video plan node: %v", err)
	}
	request.NodeRunID = longExecution.NodeRunID
	request.NodeExecutionToken = longExecution.ExecutionToken
	request.NodeAttemptGeneration = longExecution.AttemptGeneration
	request.TargetDurationTicks = longDurationTicks
	_, err = service.PlanVideo(ctx, request)
	var standard *provider.StandardErrorError
	if !errors.As(err, &standard) || standard.Standard.Code != provider.CodeStoryboardReplanRequired || standard.Standard.Retryable {
		t.Fatalf("long first-last plan error = %v, want non-retryable %s", err, provider.CodeStoryboardReplanRequired)
	}
}

func TestMultimodalReferenceProfilePersistsTypedVideoReferencePack(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	orgID, userID, projectID, workflowRunID, textModelID, imageModelID := seedWorkflowGatewayIntegrationDataForProfile(
		t, ctx, pool, videoproduction.ProfileMultimodalReference,
	)
	if _, err := pool.Exec(ctx, `UPDATE projects SET audio_strategy = 'native_av', audio_requirement = 'required' WHERE id = $1`, projectID); err != nil {
		t.Fatalf("require native audio: %v", err)
	}
	scriptID, versionID, episodeID, sceneIDs := seedStoryboardEpisodeV2Script(t, ctx, pool, orgID, projectID, userID)
	assets := seedStoryboardEpisodeV2Assets(t, ctx, pool, orgID, projectID, userID)
	callIDs := seedStoryboardEpisodeV2ProviderCalls(t, ctx, pool, orgID, projectID, workflowRunID, textModelID, 7)
	imageOutputs := seedStoryboardEpisodeV2AnchorImageOutputs(
		t, ctx, pool, orgID, projectID, workflowRunID, userID, imageModelID,
		[]string{videoproduction.AnchorRolePlannedFirstFrame},
	)
	var timingOutput TimingAnalysisActivityOutput
	gateway := httptest.NewServer(mockStoryboardEpisodeV2GatewayWithConfig(
		t, textModelID, callIDs, assets, sceneIDs, &timingOutput,
		storyboardEpisodeV2GatewayConfig{Multimodal: true, ImageOutput: imageOutputs},
	))
	defer gateway.Close()
	activities := NewActivities(pool, newWorkflowMemoryStorage(), &provider.GatewayClient{
		BaseURL: gateway.URL, Token: "workflow-service-token", Client: gateway.Client(),
	})

	var err error
	timingOutput, err = activities.AnalyzeEpisodeTiming(ctx, AnalyzeEpisodeTimingInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ScriptID: scriptID, ScriptVersionID: versionID, ScriptEpisodeID: episodeID,
	})
	if err != nil {
		t.Fatalf("AnalyzeEpisodeTiming: %v", err)
	}
	blueprint, err := activities.BuildEpisodeContinuityBlueprint(ctx, BuildEpisodeContinuityBlueprintInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ScriptID: scriptID, ScriptVersionID: versionID,
		ScriptEpisodeID: episodeID, PacingProfile: "standard", Timing: timingOutput,
	})
	if err != nil {
		t.Fatalf("BuildEpisodeContinuityBlueprint: %v", err)
	}
	planned, err := activities.PlanStoryboardScene(ctx, PlanStoryboardSceneInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ScriptID: scriptID, ScriptVersionID: versionID,
		ScriptEpisodeID: episodeID, StoryboardPlanID: blueprint.StoryboardPlanID,
		BlueprintID: blueprint.BlueprintID, ScenePlanID: blueprint.ScenePlans[0].ID,
		SceneKey: blueprint.ScenePlans[0].SceneKey, SceneOrdinal: blueprint.ScenePlans[0].SceneOrdinal,
	})
	if err != nil || len(planned.Shots) != 1 {
		t.Fatalf("PlanStoryboardScene: shots=%d err=%v", len(planned.Shots), err)
	}
	shot := mustLoadStoryboardShot(t, ctx, activities, projectID, planned.Shots[0].ID)
	prepared, err := activities.PrepareShotImagePrompt(ctx, PrepareShotImagePromptInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ShotID: shot.ID, ShotIndex: shot.ShotIndex, ShotNo: shot.ShotNo,
		AnchorRole:     videoproduction.AnchorRolePlannedFirstFrame,
		WorkflowPrompt: "生成多模态主构图锚点", AspectRatio: "16:9", Size: "1536x864", Force: true,
	})
	if err != nil {
		t.Fatalf("PrepareShotImagePrompt: %v", err)
	}
	if prepared.GenerationTemplateKey != "video_profile.multimodal_reference.anchor.generate" || prepared.ReviewTemplateKey != "video_profile.multimodal_reference.anchor.review" {
		t.Fatalf("multimodal image prompt contract = %+v", prepared)
	}
	if _, err := activities.GenerateShotImage(ctx, GenerateShotImageInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ShotID: shot.ID, ShotIndex: shot.ShotIndex, ShotNo: shot.ShotNo,
		AnchorRole: videoproduction.AnchorRolePlannedFirstFrame, ProfileKey: videoproduction.ProfileMultimodalReference,
		WorkflowPrompt: "生成多模态主构图锚点", AspectRatio: "16:9", Force: true,
	}); err != nil {
		t.Fatalf("GenerateShotImage: %v", err)
	}
	videoPrepared, err := activities.PrepareShotVideoPrompt(ctx, PrepareShotVideoPromptInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ShotID: shot.ID, ShotIndex: shot.ShotIndex, ShotNo: shot.ShotNo,
		WorkflowPrompt: "生成多模态视频提示词", AspectRatio: "16:9", Resolution: "720p", Force: true,
	})
	if err != nil {
		t.Fatalf("PrepareShotVideoPrompt: %v", err)
	}
	if videoPrepared.GenerationTemplateKey != "video_profile.multimodal_reference.video.generate" ||
		videoPrepared.ReviewTemplateKey != "video_profile.multimodal_reference.video.review" || videoPrepared.ReferencePackID == "" {
		t.Fatalf("multimodal video prompt contract = %+v", videoPrepared)
	}
	rows, err := pool.Query(ctx, `
		SELECT role, media_type, semantics, required
		FROM shot_reference_pack_items
		WHERE reference_pack_id = $1
		ORDER BY priority DESC, reference_key
	`, videoPrepared.ReferencePackID)
	if err != nil {
		t.Fatalf("load multimodal references: %v", err)
	}
	typedRoles := map[string]struct {
		MediaType, Semantics string
		Required             bool
	}{}
	for rows.Next() {
		var role string
		var item struct {
			MediaType, Semantics string
			Required             bool
		}
		if err := rows.Scan(&role, &item.MediaType, &item.Semantics, &item.Required); err != nil {
			rows.Close()
			t.Fatalf("scan multimodal reference: %v", err)
		}
		typedRoles[role] = item
	}
	rows.Close()
	for _, role := range []string{
		videoproduction.ReferenceRoleFirstFrame,
		videoproduction.ReferenceRoleCharacterIdentity,
		videoproduction.ReferenceRoleSceneIdentity,
		videoproduction.ReferenceRolePropIdentity,
	} {
		item, ok := typedRoles[role]
		if !ok || item.MediaType != "image" || item.Semantics == "" || !item.Required {
			t.Fatalf("typed multimodal role %s = %+v, all=%+v", role, item, typedRoles)
		}
	}

	videoModelID := seedStoryboardEpisodeV2MultimodalVideoModel(t, ctx, pool, textModelID)
	plan := planStoryboardEpisodeV2VideoForContract(
		t, ctx, pool, activities, orgID, projectID, workflowRunID, videoModelID, shot,
		provider.VideoInputContractFirstFramePlusReferences,
	)
	if plan.InitialInputContractSnapshot.ContractKey != provider.VideoInputContractFirstFramePlusReferences || len(plan.Segments) != 1 {
		t.Fatalf("multimodal render plan = %+v", plan)
	}
	assertMultimodalReferenceManifestRejectedBeforeUpstream(
		t, ctx, pool, activities, orgID, userID, projectID, workflowRunID, videoModelID, shot, plan,
	)
}

func assertMultimodalReferenceManifestRejectedBeforeUpstream(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	activities Activities,
	organizationID, userID, projectID, workflowRunID, videoModelID string,
	shot StoryboardShotRecord,
	plan provider.GatewayVideoPlanResponse,
) {
	t.Helper()
	if err := activities.materializeApprovedVideoPromptPlan(ctx, EnsurePreparedShotVideoPlanInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ShotID: shot.ID, ShotIndex: shot.ShotIndex,
		AspectRatio: "16:9", Resolution: "720p", AudioStrategy: "native_av", AudioRequirement: "required",
	}, PlanShotVideoOutput{GatewayVideoPlanResponse: plan}); err != nil {
		t.Fatalf("materialize multimodal execution prompt: %v", err)
	}
	project, err := activities.projectProductionSettings(ctx, projectID, workflowRunID)
	if err != nil {
		t.Fatalf("load multimodal project settings: %v", err)
	}
	contract, err := activities.loadApprovedShotVideoExecutionContract(ctx, organizationID, project, shot)
	if err != nil {
		t.Fatalf("load multimodal execution contract: %v", err)
	}
	if len(contract.References) < 3 {
		t.Fatalf("multimodal reference pack contains %d references, want at least 3", len(contract.References))
	}
	tamperedReferences := append([]provider.GatewayVideoReference(nil), contract.References...)
	removed := false
	for index := len(tamperedReferences) - 1; index >= 0; index-- {
		if tamperedReferences[index].Role == videoproduction.ReferenceRoleFirstFrame {
			continue
		}
		tamperedReferences = append(tamperedReferences[:index], tamperedReferences[index+1:]...)
		removed = true
		break
	}
	if !removed {
		t.Fatal("multimodal reference pack has no semantic reference to remove")
	}
	tamperedReferences, manifest, manifestHash, err := activities.prepareVideoReferenceManifest(
		ctx,
		organizationID,
		plan.Segments[0].InputContractKey,
		plan.CapabilitySnapshotHash,
		tamperedReferences,
	)
	if err != nil {
		t.Fatalf("build intentionally incomplete reference manifest: %v", err)
	}
	var segmentPrompt, segmentPromptHash string
	if err := pool.QueryRow(ctx, `
		SELECT prompt, execution_prompt_hash
		FROM video_render_segments
		WHERE id = $1 AND video_render_plan_id = $2
	`, plan.Segments[0].SegmentID, plan.ExecutionPlanID).Scan(&segmentPrompt, &segmentPromptHash); err != nil {
		t.Fatalf("load materialized multimodal segment prompt: %v", err)
	}
	node, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		NodeKey: "b08-reference-manifest-gateway-rejection-" + shot.ID, NodeType: "video.create_task",
	})
	if err != nil {
		t.Fatalf("start B08 create node: %v", err)
	}
	var upstreamCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		http.Error(w, "unexpected upstream call", http.StatusInternalServerError)
	}))
	defer upstream.Close()
	if _, err := pool.Exec(ctx, `
		UPDATE provider_accounts
		SET base_url = $2,
		    config = COALESCE(config, '{}'::jsonb) || '{"runtime":"openai_compatible"}'::jsonb
		WHERE id = (SELECT provider_account_id FROM provider_models WHERE id = $1)
	`, videoModelID, upstream.URL); err != nil {
		t.Fatalf("point multimodal provider at guarded upstream: %v", err)
	}
	type sideEffectCounts struct {
		Requests, Tasks, Calls, Costs, Leases int
	}
	loadCounts := func() sideEffectCounts {
		var counts sideEffectCounts
		if err := pool.QueryRow(ctx, `
			SELECT
			  (SELECT count(*) FROM provider_requests WHERE project_id = $1 AND task_type = 'video.create_task'),
			  (SELECT count(*) FROM provider_async_tasks WHERE project_id = $1),
			  (SELECT count(*) FROM provider_call_logs WHERE project_id = $1 AND task_type = 'video.create_task'),
			  (SELECT count(*) FROM cost_records WHERE project_id = $1),
			  (SELECT count(*) FROM provider_leases WHERE organization_id = $2 AND provider_model_id = $3 AND task_type = 'video.create_task')
		`, projectID, organizationID, videoModelID).Scan(
			&counts.Requests, &counts.Tasks, &counts.Calls, &counts.Costs, &counts.Leases,
		); err != nil {
			t.Fatalf("count provider side effects: %v", err)
		}
		return counts
	}
	before := loadCounts()
	vault, err := provider.NewVault("")
	if err != nil {
		t.Fatalf("create provider vault: %v", err)
	}
	service := provider.NewService(pool, vault)
	service.EnableGatewayRuntime()
	objectStorage, ok := activities.storage.(*workflowMemoryStorage)
	if !ok {
		t.Fatalf("test storage type = %T, want *workflowMemoryStorage", activities.storage)
	}
	service.SetStorage(objectStorage)
	idempotencyKey := "b08-reference-manifest-" + shot.ID
	response, err := service.CreateVideoTask(ctx, provider.GatewayVideoCreateTaskRequest{
		OrganizationID: organizationID, ProjectID: projectID,
		ProductionGenerationID: node.ProductionGenerationID, VideoProductionBindingID: node.VideoProductionBindingID,
		VideoProductionBindingRevision: node.VideoProductionBindingRevision,
		StoryboardShotID:               shot.ID, ProductionProfileVersionID: contract.ProductionProfileVersionID,
		ProductionProfileSnapshotHash: contract.ProductionProfileSnapshotHash,
		InputContractKey:              plan.Segments[0].InputContractKey, InputContractHash: plan.Segments[0].InputContractHash,
		InputContractVersion: contract.InputContractVersion,
		ShotStateRevision:    contract.ShotStateRevision, ShotStateHash: contract.ShotStateHash,
		TransitionHash: contract.TransitionHash, ReferencePackID: contract.ReferencePackID,
		ReferencePackHash: contract.ReferencePackHash, PromptContextPlanID: contract.PromptContextPlanID,
		PromptContextPlanHash: contract.PromptContextPlanHash, VideoPromptPlanID: contract.VideoPromptPlanID,
		NativeAudioRequired: contract.NativeAudioRequired, DialogueCues: plan.Segments[0].DialogueSpans,
		WorkflowRunID: workflowRunID, NodeRunID: node.NodeRunID, NodeExecutionToken: node.ExecutionToken,
		NodeAttemptGeneration: node.AttemptGeneration, ProviderModelID: videoModelID,
		PromptHash: segmentPromptHash, PromptSource: "approved_video_prompt_plan", IdempotencyKey: idempotencyKey,
		ExecutionPlanID: plan.ExecutionPlanID, RenderSegmentID: plan.Segments[0].SegmentID,
		CapabilitySnapshotHash: plan.CapabilitySnapshotHash,
		ReferenceManifest:      manifest, ReferenceManifestHash: manifestHash,
		Input: mustJSON(map[string]any{
			"prompt": segmentPrompt, "duration": plan.Segments[0].RequestedDurationSeconds,
			"aspectRatio": "16:9", "resolution": "720p", "mode": "image_to_video",
		}),
		References: tamperedReferences,
		Options:    provider.GatewayVideoOptions{IdempotencyKey: idempotencyKey},
	})
	if err != nil {
		t.Fatalf("CreateVideoTask returned transport error: %v", err)
	}
	if response.Status != "failed" || response.Error == nil || response.Error.Code != provider.CodeModelInputContractUnsupported {
		if response.Error == nil {
			t.Fatalf("incomplete frozen reference response = %+v", response)
		}
		t.Fatalf("incomplete frozen reference response code=%s message=%s response=%+v", response.Error.Code, response.Error.Message, response)
	}
	after := loadCounts()
	if upstreamCalls.Load() != 0 {
		t.Fatalf("upstream calls = %d, want 0", upstreamCalls.Load())
	}
	if after.Requests-before.Requests != 1 || after.Tasks != before.Tasks || after.Calls != before.Calls ||
		after.Costs != before.Costs || after.Leases != before.Leases {
		t.Fatalf("rejected manifest side effects before=%+v after=%+v", before, after)
	}
}

func TestStoryboardSheetProfilePersistsManifestCropsPanelsAndPlansVideo(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	orgID, userID, projectID, workflowRunID, textModelID, imageModelID := seedWorkflowGatewayIntegrationDataForProfile(
		t, ctx, pool, videoproduction.ProfileStoryboardSheet,
	)
	if _, err := pool.Exec(ctx, `UPDATE projects SET audio_strategy = 'native_av', audio_requirement = 'required' WHERE id = $1`, projectID); err != nil {
		t.Fatalf("configure storyboard sheet audio: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE provider_models SET model_key = 'gpt-image-2', display_name = 'GPT Image 2' WHERE id = $1`, imageModelID); err != nil {
		t.Fatalf("configure storyboard sheet image model: %v", err)
	}
	scriptID, versionID, episodeID, sceneIDs := seedStoryboardEpisodeV2Script(t, ctx, pool, orgID, projectID, userID)
	assets := seedStoryboardEpisodeV2Assets(t, ctx, pool, orgID, projectID, userID)
	callIDs := seedStoryboardEpisodeV2ProviderCalls(t, ctx, pool, orgID, projectID, workflowRunID, textModelID, 12)
	imageOutputs := seedStoryboardEpisodeV2AnchorImageOutputs(
		t, ctx, pool, orgID, projectID, workflowRunID, userID, imageModelID,
		[]string{videoproduction.AnchorRoleStoryboardSheet},
	)
	memoryStorage := newWorkflowMemoryStorage()
	memoryStorage.putObject(imageOutputs[0].Output.StorageKey, storyboardSheetFixturePNG(t, 1024, 1536, 3, 1))

	var timingOutput TimingAnalysisActivityOutput
	gateway := httptest.NewServer(mockStoryboardEpisodeV2GatewayWithConfig(
		t, textModelID, callIDs, assets, sceneIDs, &timingOutput,
		storyboardEpisodeV2GatewayConfig{
			StoryboardSheet: true, ImageModelID: imageModelID, ImageOutput: imageOutputs,
		},
	))
	defer gateway.Close()
	activities := NewActivities(pool, memoryStorage, &provider.GatewayClient{
		BaseURL: gateway.URL, Token: "workflow-service-token", Client: gateway.Client(),
	})

	var err error
	timingOutput, err = activities.AnalyzeEpisodeTiming(ctx, AnalyzeEpisodeTimingInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ScriptID: scriptID, ScriptVersionID: versionID, ScriptEpisodeID: episodeID,
	})
	if err != nil {
		t.Fatalf("AnalyzeEpisodeTiming: %v", err)
	}
	blueprint, err := activities.BuildEpisodeContinuityBlueprint(ctx, BuildEpisodeContinuityBlueprintInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ScriptID: scriptID, ScriptVersionID: versionID,
		ScriptEpisodeID: episodeID, PacingProfile: "standard", Timing: timingOutput,
	})
	if err != nil {
		t.Fatalf("BuildEpisodeContinuityBlueprint: %v", err)
	}
	planned, err := activities.PlanStoryboardScene(ctx, PlanStoryboardSceneInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ScriptID: scriptID, ScriptVersionID: versionID,
		ScriptEpisodeID: episodeID, StoryboardPlanID: blueprint.StoryboardPlanID,
		BlueprintID: blueprint.BlueprintID, ScenePlanID: blueprint.ScenePlans[0].ID,
		SceneKey: blueprint.ScenePlans[0].SceneKey, SceneOrdinal: blueprint.ScenePlans[0].SceneOrdinal,
	})
	if err != nil || len(planned.Shots) != 1 {
		t.Fatalf("PlanStoryboardScene: shots=%d err=%v", len(planned.Shots), err)
	}
	shotID := planned.Shots[0].ID
	if _, err := pool.Exec(ctx, `
		UPDATE storyboard_shots
		SET end_tick = start_tick + 450000
		WHERE id = $1
	`, shotID); err != nil {
		t.Fatalf("set deterministic sheet duration: %v", err)
	}
	shot := mustLoadStoryboardShot(t, ctx, activities, projectID, shotID)
	prepared, err := activities.PrepareShotImagePrompt(ctx, PrepareShotImagePromptInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ShotID: shot.ID, ShotIndex: shot.ShotIndex, ShotNo: shot.ShotNo,
		AnchorRole:     videoproduction.AnchorRoleStoryboardSheet,
		WorkflowPrompt: "生成当前镜头三格分镜板", AspectRatio: "16:9", Force: true,
	})
	if err != nil {
		t.Fatalf("PrepareShotImagePrompt: %v", err)
	}
	if prepared.GenerationTemplateKey != "video_profile.storyboard_sheet.anchor.generate" ||
		prepared.ReviewTemplateKey != "video_profile.storyboard_sheet.anchor.review" ||
		prepared.ImageProviderModelID != imageModelID {
		t.Fatalf("storyboard sheet prompt contract = %+v", prepared)
	}
	var manifestID, manifestHash, manifestStatus, manifestReview string
	var panelCount int
	if err := pool.QueryRow(ctx, `
		SELECT id::text, manifest_hash, panel_count, status, review_status
		FROM storyboard_sheet_manifests
		WHERE storyboard_shot_id = $1 AND status = 'draft'
	`, shot.ID).Scan(&manifestID, &manifestHash, &panelCount, &manifestStatus, &manifestReview); err != nil {
		t.Fatalf("load compiled PanelManifest: %v", err)
	}
	if panelCount != 3 || len(manifestHash) != 64 || manifestStatus != "draft" || manifestReview != "pending" {
		t.Fatalf("compiled PanelManifest = id=%s hash=%s count=%d status=%s/%s", manifestID, manifestHash, panelCount, manifestStatus, manifestReview)
	}
	var panelAnchorCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM shot_visual_anchors
		WHERE storyboard_shot_id = $1 AND anchor_role = 'storyboard_panel' AND status = 'draft'
	`, shot.ID).Scan(&panelAnchorCount); err != nil || panelAnchorCount != panelCount {
		t.Fatalf("planned panel anchors = %d err=%v", panelAnchorCount, err)
	}

	generated, err := activities.GenerateShotImage(ctx, GenerateShotImageInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ShotID: shot.ID, ShotIndex: shot.ShotIndex, ShotNo: shot.ShotNo,
		AnchorRole: videoproduction.AnchorRoleStoryboardSheet, ProfileKey: videoproduction.ProfileStoryboardSheet,
		WorkflowPrompt: "生成当前镜头三格分镜板", AspectRatio: "16:9", Force: true,
	})
	if err != nil {
		t.Fatalf("GenerateShotImage: %v", err)
	}
	processed, err := activities.ProcessStoryboardSheetPanels(ctx, ProcessStoryboardSheetPanelsInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID, CreatedBy: userID,
		ShotID: shot.ID, SheetAnchorID: generated.VisualAnchorID,
		SheetArtifactID: generated.ImageArtifactID, SheetMediaFileID: generated.ImageMediaFileID,
		SheetStorageKey: generated.ImageStorageKey,
	})
	if err != nil {
		t.Fatalf("ProcessStoryboardSheetPanels: %v", err)
	}
	if processed.PanelManifestID != manifestID || len(processed.Panels) != panelCount || processed.SourceWidth != 1024 || processed.SourceHeight != 1536 {
		t.Fatalf("processed storyboard sheet = %+v", processed)
	}
	reviewed, err := activities.ReviewStoryboardSheetOutput(ctx, ReviewStoryboardSheetOutputInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID, CreatedBy: userID,
		ShotID: shot.ID, SheetAnchorID: generated.VisualAnchorID,
		SheetArtifactID: generated.ImageArtifactID, SheetMediaFileID: generated.ImageMediaFileID,
		SheetStorageKey: generated.ImageStorageKey, PanelManifestID: manifestID,
	})
	if err != nil {
		t.Fatalf("ReviewStoryboardSheetOutput: %v", err)
	}
	if !reviewed.Approved || reviewed.PanelManifestHash != manifestHash {
		t.Fatalf("storyboard sheet review = %+v", reviewed)
	}
	var approvedPanels, approvedPanelAnchors int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM storyboard_sheet_panels WHERE manifest_id = $1 AND status = 'cropped' AND review_status = 'approved'`, manifestID).Scan(&approvedPanels); err != nil {
		t.Fatalf("count approved panels: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM shot_visual_anchors WHERE storyboard_shot_id = $1 AND anchor_role = 'storyboard_panel' AND status = 'ready' AND review_status = 'approved'`, shot.ID).Scan(&approvedPanelAnchors); err != nil {
		t.Fatalf("count approved panel anchors: %v", err)
	}
	if approvedPanels != panelCount || approvedPanelAnchors != panelCount {
		t.Fatalf("approved panels=%d anchors=%d want=%d", approvedPanels, approvedPanelAnchors, panelCount)
	}

	videoPrepared, err := activities.PrepareShotVideoPrompt(ctx, PrepareShotVideoPromptInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ShotID: shot.ID, ShotIndex: shot.ShotIndex, ShotNo: shot.ShotNo,
		WorkflowPrompt: "根据已审核分镜板生成视频提示词", AspectRatio: "16:9", Resolution: "720p", Force: true,
	})
	if err != nil {
		t.Fatalf("PrepareShotVideoPrompt: %v", err)
	}
	if videoPrepared.GenerationTemplateKey != "video_profile.storyboard_sheet.video.generate" ||
		videoPrepared.ReviewTemplateKey != "video_profile.storyboard_sheet.video.review" ||
		videoPrepared.ReferencePackID == "" || !videoPrepared.ModelSupportsNativeAudio {
		t.Fatalf("storyboard sheet video prompt = %+v", videoPrepared)
	}
	var role, mediaType, semantics, packedManifestID, packedManifestHash string
	if err := pool.QueryRow(ctx, `
		SELECT role, media_type, semantics,
		       COALESCE(metadata->>'panelManifestId', ''), COALESCE(metadata->>'panelManifestHash', '')
		FROM shot_reference_pack_items
		WHERE reference_pack_id = $1
	`, videoPrepared.ReferencePackID).Scan(&role, &mediaType, &semantics, &packedManifestID, &packedManifestHash); err != nil {
		t.Fatalf("load storyboard sheet ReferencePack: %v", err)
	}
	if role != videoproduction.ReferenceRoleStoryboardSheet || mediaType != "image" ||
		semantics != videoproduction.ReferenceSemanticsForRole(videoproduction.ReferenceRoleStoryboardSheet) || packedManifestID != manifestID || packedManifestHash != manifestHash {
		t.Fatalf("storyboard sheet ReferencePack item = %s %s %s %s %s", role, mediaType, semantics, packedManifestID, packedManifestHash)
	}

	videoModelID := seedStoryboardEpisodeV2StoryboardSheetVideoModel(t, ctx, pool, textModelID)
	plan := planStoryboardEpisodeV2VideoForContract(
		t, ctx, pool, activities, orgID, projectID, workflowRunID, videoModelID, shot,
		provider.VideoInputContractStoryboardSheetReference,
	)
	if plan.InitialInputContractSnapshot.ContractKey != provider.VideoInputContractStoryboardSheetReference || len(plan.Segments) != 1 {
		t.Fatalf("storyboard sheet render plan = %+v", plan)
	}
}

func seedStoryboardEpisodeV2VideoModel(t *testing.T, ctx context.Context, pool *pgxpool.Pool, textModelID string) string {
	return seedStoryboardEpisodeV2VideoModelForContract(t, ctx, pool, textModelID, provider.VideoInputContractFirstFrame)
}

func seedStoryboardEpisodeV2FirstLastVideoModel(t *testing.T, ctx context.Context, pool *pgxpool.Pool, textModelID string) string {
	return seedStoryboardEpisodeV2VideoModelForContract(t, ctx, pool, textModelID, provider.VideoInputContractFirstLastFrames)
}

func seedStoryboardEpisodeV2MultimodalVideoModel(t *testing.T, ctx context.Context, pool *pgxpool.Pool, textModelID string) string {
	return seedStoryboardEpisodeV2VideoModelForContract(t, ctx, pool, textModelID, provider.VideoInputContractFirstFramePlusReferences)
}

func seedStoryboardEpisodeV2StoryboardSheetVideoModel(t *testing.T, ctx context.Context, pool *pgxpool.Pool, textModelID string) string {
	return seedStoryboardEpisodeV2VideoModelForContract(t, ctx, pool, textModelID, provider.VideoInputContractStoryboardSheetReference)
}

func seedStoryboardEpisodeV2VideoModelForContract(t *testing.T, ctx context.Context, pool *pgxpool.Pool, textModelID, contractKey string) string {
	t.Helper()
	var accountID, modelID string
	if err := pool.QueryRow(ctx, `SELECT provider_account_id::text FROM provider_models WHERE id = $1`, textModelID).Scan(&accountID); err != nil {
		t.Fatalf("load provider account: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE provider_accounts
		SET config = COALESCE(config, '{}'::jsonb) || '{"runtime":"openai_compatible"}'::jsonb
		WHERE id = $1
	`, accountID); err != nil {
		t.Fatalf("configure video adapter fixture: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status)
		VALUES ($1, $2, 'Storyboard V2 Video Model', 'video', 'active')
		RETURNING id::text
	`, accountID, "storyboard-v2-video-"+uuid.NewString()).Scan(&modelID); err != nil {
		t.Fatalf("insert video model: %v", err)
	}
	variantKey := "single-frame-native-av"
	referenceModes := []string{"first_frame"}
	taskTypes := []string{"video.image_to_video"}
	continuation := map[string]any{"supportsFirstFrame": true}
	slots := []map[string]any{{
		"role": "first_frame", "mediaType": "image", "semantics": "output_start_frame",
		"min": 1, "max": 1, "ordered": true,
	}}
	switch contractKey {
	case provider.VideoInputContractFirstLastFrames:
		variantKey = "first-last-native-av"
		referenceModes = []string{"first_last_frames"}
		continuation["supportsLastFrame"] = true
		slots = append(slots, map[string]any{
			"role": "last_frame", "mediaType": "image", "semantics": "output_end_frame",
			"min": 1, "max": 1, "ordered": true,
		})
	case provider.VideoInputContractFirstFramePlusReferences:
		variantKey = "multimodal-native-av"
		referenceModes = []string{"first_frame_plus_references"}
		continuation["supportsVideoReference"] = true
		slots = append(slots,
			map[string]any{"role": "semantic_reference", "mediaType": "image", "semantics": "identity_scene_style_guidance", "min": 1, "max": 8, "ordered": false},
			map[string]any{"role": "video_reference", "mediaType": "video", "semantics": "motion_guidance", "min": 0, "max": 2, "ordered": false},
			map[string]any{"role": "audio_reference", "mediaType": "audio", "semantics": "audio_guidance", "min": 0, "max": 1, "ordered": false},
		)
	case provider.VideoInputContractStoryboardSheetReference:
		variantKey = "storyboard-sheet-native-av"
		referenceModes = []string{"storyboard_sheet_reference"}
		taskTypes = []string{"video.reference_to_video"}
		continuation = map[string]any{}
		slots = []map[string]any{{
			"role": "storyboard_sheet", "mediaType": "image", "semantics": "ordered_keyframe_sheet",
			"min": 1, "max": 1, "ordered": true,
		}}
	}
	capabilities := map[string]any{"xCapabilities": map[string]any{
		"videoGenerationVariants": []map[string]any{{
			"variantKey": variantKey, "modelFamily": "integration-video",
			"when": map[string]any{
				"taskTypes": taskTypes, "referenceModes": referenceModes,
				"nativeAudioRequested": true,
			},
			"duration":    map[string]any{"mode": "continuous_range", "minSeconds": 1, "maxSeconds": 30},
			"resolutions": []string{"720p"}, "aspectRatios": []string{"16:9"},
			"frameRate":                map[string]any{"mode": "fixed", "values": []int{24}},
			"supportedPromptLanguages": []string{"zh-CN"},
			"nativeAudio": map[string]any{
				"support": "true", "supportsDialogue": true, "supportsLipSync": true,
				"supportedDialogueLanguages": []string{"zh-CN"},
			},
			"continuation": continuation,
			"inputContract": map[string]any{
				"contractKey": contractKey, "requestMode": "async_create", "slots": slots,
			},
			"requestModes": []string{"async_create", "poll", "cancel"},
			"source":       "test", "verificationStatus": "tested", "capabilityVersion": "1",
		}},
	}}
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_model_capabilities(
			provider_model_id, task_types, input_limits, output_limits,
			quality_tiers, provider_options_schema, pricing_policy
		)
		VALUES ($1, '["video.create_task","video.poll_task","video.cancel_task"]', '{}', '{}',
		        '["standard"]', $2::jsonb, '{}')
	`, modelID, mustJSON(capabilities)); err != nil {
		t.Fatalf("insert video capabilities: %v", err)
	}
	return modelID
}

func planStoryboardEpisodeV2VideoForContract(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	activities Activities,
	organizationID, projectID, workflowRunID, videoModelID string,
	shot StoryboardShotRecord,
	referenceMode string,
) provider.GatewayVideoPlanResponse {
	t.Helper()
	project, err := activities.projectProductionSettings(ctx, projectID)
	if err != nil {
		t.Fatalf("load project production settings: %v", err)
	}
	contract, err := activities.loadApprovedShotVideoExecutionContract(ctx, organizationID, project, shot)
	if err != nil {
		t.Fatalf("load approved video contract: %v", err)
	}
	execution, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		NodeKey: "provider-plan-" + referenceMode + "-" + shot.ID, NodeType: "video.plan",
	})
	if err != nil {
		t.Fatalf("start video plan node: %v", err)
	}
	vault, err := provider.NewVault("")
	if err != nil {
		t.Fatalf("create provider vault: %v", err)
	}
	service := provider.NewService(pool, vault)
	service.EnableGatewayRuntime()
	taskType := "video.image_to_video"
	if referenceMode == provider.VideoInputContractStoryboardSheetReference {
		taskType = "video.reference_to_video"
	}
	plan, err := service.PlanVideo(ctx, provider.GatewayVideoPlanRequest{
		OrganizationID: organizationID, ProjectID: projectID,
		ProductionGenerationID:            execution.ProductionGenerationID,
		VideoProductionBindingID:          execution.VideoProductionBindingID,
		VideoProductionBindingRevision:    execution.VideoProductionBindingRevision,
		ProductionProfileVersionID:        project.VideoProductionProfileVersionID,
		ProductionProfileSnapshotHash:     project.VideoProductionProfileHash,
		CompatibilityPolicy:               project.VideoProductionCompatibilityPolicy,
		RequiredInitialInputContract:      project.VideoProductionRequiredInitialInputContract,
		AllowedContinuationInputContracts: project.VideoProductionAllowedContinuationInputContracts,
		InputContractVersion:              project.VideoProductionInputContract,
		ShotStateRevision:                 contract.ShotStateRevision,
		ShotStateHash:                     contract.ShotStateHash,
		TransitionHash:                    contract.TransitionHash,
		ReferencePackID:                   contract.ReferencePackID,
		ReferencePackHash:                 contract.ReferencePackHash,
		PromptContextPlanID:               contract.PromptContextPlanID,
		PromptContextPlanHash:             contract.PromptContextPlanHash,
		VideoPromptPlanID:                 contract.VideoPromptPlanID,
		NativeAudioRequired:               contract.NativeAudioRequired,
		WorkflowRunID:                     workflowRunID,
		NodeRunID:                         execution.NodeRunID,
		NodeExecutionToken:                execution.ExecutionToken,
		NodeAttemptGeneration:             execution.AttemptGeneration,
		StoryboardPlanID:                  shot.StoryboardPlanID,
		StoryboardShotID:                  shot.ID,
		ProviderModelID:                   videoModelID,
		TaskType:                          taskType,
		TargetDurationTicks:               shot.PlannedDurationTicks,
		TimelineTimebase:                  shot.TimelineTimebase,
		FPSNumerator:                      int64(shot.FPSNumerator),
		FPSDenominator:                    int64(shot.FPSDenominator),
		AudioStrategy:                     contract.AudioStrategy,
		AudioRequirement:                  contract.AudioRequirement,
		DialogueLanguage:                  "zh-CN",
		HasDialogue:                       len(contract.DialogueCues) > 0,
		ReferenceMode:                     referenceMode,
		AspectRatio:                       "16:9",
		Resolution:                        "720p",
		PromptLanguage:                    "zh-CN",
		DialogueSpans:                     contract.DialogueCues,
	})
	if err != nil {
		t.Fatalf("PlanVideo %s: %v", referenceMode, err)
	}
	return plan
}

func assertStoryboardEpisodeV2VideoPlans(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	activities Activities,
	organizationID string,
	projectID string,
	workflowRunID string,
	videoModelID string,
	shots []StoryboardShotRecord,
) {
	t.Helper()
	vault, err := provider.NewVault("")
	if err != nil {
		t.Fatalf("create provider vault: %v", err)
	}
	service := provider.NewService(pool, vault)
	service.EnableGatewayRuntime()
	project, err := activities.projectProductionSettings(ctx, projectID)
	if err != nil {
		t.Fatalf("load project production settings: %v", err)
	}
	for _, shot := range shots {
		contract, err := activities.loadApprovedShotVideoExecutionContract(ctx, organizationID, project, shot)
		if err != nil {
			t.Fatalf("load approved video contract %s: %v", shot.ID, err)
		}
		execution, err := StartNodeRun(ctx, pool, NodeRunInput{
			OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
			NodeKey: "provider-plan-" + shot.ID, NodeType: "video.plan",
		})
		if err != nil {
			t.Fatalf("start video plan node %s: %v", shot.ID, err)
		}
		plan, err := service.PlanVideo(ctx, provider.GatewayVideoPlanRequest{
			OrganizationID: organizationID, ProjectID: projectID,
			ProductionGenerationID:            execution.ProductionGenerationID,
			VideoProductionBindingID:          execution.VideoProductionBindingID,
			VideoProductionBindingRevision:    execution.VideoProductionBindingRevision,
			ProductionProfileVersionID:        project.VideoProductionProfileVersionID,
			ProductionProfileSnapshotHash:     project.VideoProductionProfileHash,
			CompatibilityPolicy:               project.VideoProductionCompatibilityPolicy,
			RequiredInitialInputContract:      project.VideoProductionRequiredInitialInputContract,
			AllowedContinuationInputContracts: project.VideoProductionAllowedContinuationInputContracts,
			InputContractVersion:              project.VideoProductionInputContract,
			ShotStateRevision:                 contract.ShotStateRevision, ShotStateHash: contract.ShotStateHash,
			TransitionHash: contract.TransitionHash, ReferencePackID: contract.ReferencePackID,
			ReferencePackHash: contract.ReferencePackHash, PromptContextPlanID: contract.PromptContextPlanID,
			PromptContextPlanHash: contract.PromptContextPlanHash, VideoPromptPlanID: contract.VideoPromptPlanID,
			NativeAudioRequired: contract.NativeAudioRequired,
			WorkflowRunID:       workflowRunID, NodeRunID: execution.NodeRunID,
			NodeExecutionToken: execution.ExecutionToken, NodeAttemptGeneration: execution.AttemptGeneration,
			StoryboardPlanID: shot.StoryboardPlanID, StoryboardShotID: shot.ID,
			ProviderModelID: videoModelID, TaskType: "video.image_to_video",
			TargetDurationTicks: shot.PlannedDurationTicks, TimelineTimebase: shot.TimelineTimebase,
			FPSNumerator: int64(shot.FPSNumerator), FPSDenominator: int64(shot.FPSDenominator),
			AudioStrategy: contract.AudioStrategy, AudioRequirement: contract.AudioRequirement,
			DialogueLanguage: "zh-CN", HasDialogue: len(contract.DialogueCues) > 0,
			ReferenceMode: "first_frame", AspectRatio: "16:9", Resolution: "720p",
			PromptLanguage: "zh-CN", DialogueSpans: contract.DialogueCues,
		})
		if err != nil {
			t.Fatalf("plan video %s: %v", shot.ID, err)
		}
		if plan.InitialInputContractSnapshot.ContractKey != provider.VideoInputContractFirstFrame ||
			plan.ReferencePackID != contract.ReferencePackID || plan.VideoPromptPlanID != contract.VideoPromptPlanID ||
			plan.PromptContextPlanID != contract.PromptContextPlanID || !plan.NativeAudioRequired {
			t.Fatalf("video plan contract = %+v", plan)
		}
		var persistedContract, persistedPromptPlanID, persistedReferencePackID string
		if err := pool.QueryRow(ctx, `
			SELECT initial_input_contract_snapshot->>'contractKey', video_prompt_plan_id::text, reference_pack_id::text
			FROM video_render_plans WHERE id = $1
		`, plan.ExecutionPlanID).Scan(&persistedContract, &persistedPromptPlanID, &persistedReferencePackID); err != nil {
			t.Fatalf("load persisted render plan: %v", err)
		}
		if persistedContract != "first_frame" || persistedPromptPlanID != contract.VideoPromptPlanID || persistedReferencePackID != contract.ReferencePackID {
			t.Fatalf("persisted render contract = %s/%s/%s", persistedContract, persistedPromptPlanID, persistedReferencePackID)
		}
	}
}

func TestStoryboardActivationFailureLeavesWorkflowRetryable(t *testing.T) {
	ctx, pool, organizationID, projectID, workflowRunID := seedNodeRunIntegrationTest(t)
	execution, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		NodeKey:        "storyboard-activation-failure",
		NodeType:       "storyboard.plan_activate",
	})
	if err != nil {
		t.Fatalf("start activation node: %v", err)
	}
	activities := NewActivities(pool, newWorkflowMemoryStorage(), nil)
	err = activities.failStoryboardActivation(ctx, TextToStoryboardInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		Prompt:         "plan-1",
	}, execution, fmt.Errorf("artifact persistence failed"))
	if err == nil {
		t.Fatal("activation failure did not return an activity error")
	}

	var workflowStatus, nodeStatus, nodeError string
	if err := pool.QueryRow(ctx, `
		SELECT run.status, node.status, COALESCE(node.error_message, '')
		FROM workflow_runs run
		JOIN workflow_node_runs node ON node.workflow_run_id = run.id
		WHERE run.id = $1 AND node.id = $2
	`, workflowRunID, execution.NodeRunID).Scan(&workflowStatus, &nodeStatus, &nodeError); err != nil {
		t.Fatalf("load failed activation state: %v", err)
	}
	if workflowStatus != "running" || nodeStatus != "failed" || !strings.Contains(nodeError, "artifact persistence failed") {
		t.Fatalf("workflow=%s node=%s error=%q", workflowStatus, nodeStatus, nodeError)
	}

	retryExecution, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		NodeKey:        "storyboard-activation-failure",
		NodeType:       "storyboard.plan_activate",
	})
	if err != nil {
		t.Fatalf("restart failed activation node: %v", err)
	}
	if retryExecution.NodeRunID != execution.NodeRunID || retryExecution.ExecutionToken == execution.ExecutionToken {
		t.Fatalf("retry execution = %+v, original = %+v", retryExecution, execution)
	}
}

func seedStoryboardEpisodeV2Script(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, projectID, userID string) (string, string, string, []string) {
	t.Helper()
	content := "方源：你终于来了\n方源抬头望向雨幕\n白凝冰：雨快停了\n白凝冰收起伞"
	var scriptID, versionID, episodeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO scripts(organization_id, project_id, title, status, created_by)
		VALUES ($1, $2, 'Timing Script', 'active', $3) RETURNING id::text
	`, orgID, projectID, userID).Scan(&scriptID); err != nil {
		t.Fatalf("insert script: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO script_versions(organization_id, project_id, script_id, version_no, version, content, content_format, metadata, created_by)
		VALUES ($1, $2, $3, 1, 1, $4, 'markdown', '{}', $5) RETURNING id::text
	`, orgID, projectID, scriptID, content, userID).Scan(&versionID); err != nil {
		t.Fatalf("insert script version: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE scripts SET current_version_id = $2 WHERE id = $1`, scriptID, versionID); err != nil {
		t.Fatalf("activate script version: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO script_episodes(
			organization_id, project_id, script_id, script_version_id,
			episode_index, episode_title, content, content_format, created_by
		)
		VALUES ($1, $2, $3, $4, 1, '第一集', $5, 'markdown', $6) RETURNING id::text
	`, orgID, projectID, scriptID, versionID, content, userID).Scan(&episodeID); err != nil {
		t.Fatalf("insert script episode: %v", err)
	}
	sceneIDs := make([]string, 2)
	for index, scene := range []struct{ title, action, dialogue string }{
		{"雨幕重逢", "方源抬头望向雨幕", "你终于来了"},
		{"收伞", "白凝冰收起伞", "雨快停了"},
	} {
		if err := pool.QueryRow(ctx, `
			INSERT INTO script_scenes(
				organization_id, project_id, script_id, script_version_id, script_episode_id,
				scene_index, scene_no, title, summary, location, characters, action, dialogue,
				content, content_format, review_status, stale_state, metadata, created_by
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8, '雨中庭院', '["方源","白凝冰"]',
			        $9, $10, $11, 'markdown', 'approved', 'fresh', '{}', $12)
			RETURNING id::text
		`, orgID, projectID, scriptID, versionID, episodeID, index, index+1,
			scene.title, scene.action, scene.dialogue, scene.action+"\n"+scene.dialogue, userID).Scan(&sceneIDs[index]); err != nil {
			t.Fatalf("insert script scene %d: %v", index, err)
		}
	}
	return scriptID, versionID, episodeID, sceneIDs
}

type storyboardEpisodeV2Assets struct {
	CharacterIDs []string
	SceneID      string
	PropID       string
}

func seedStoryboardEpisodeV2Assets(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, projectID, userID string) storyboardEpisodeV2Assets {
	t.Helper()
	result := storyboardEpisodeV2Assets{CharacterIDs: make([]string, 2)}
	for index, name := range []string{"方源", "白凝冰"} {
		if err := pool.QueryRow(ctx, `
			INSERT INTO canonical_assets(
				organization_id, project_id, asset_type, name, description, status,
				base_prompt, consistency_prompt, created_by
			)
			VALUES ($1, $2, 'character', $3, $3 || '在雨中庭院', 'prompt_ready',
			        $3 || '角色四视图', '保持服装和面部一致', $4)
			RETURNING id::text
		`, orgID, projectID, name, userID).Scan(&result.CharacterIDs[index]); err != nil {
			t.Fatalf("insert character asset %s: %v", name, err)
		}
		seedStoryboardEpisodeV2AssetReference(t, ctx, pool, orgID, projectID, userID, result.CharacterIDs[index], fmt.Sprintf("character-%d", index+1))
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO canonical_assets(
			organization_id, project_id, asset_type, name, description, status,
			base_prompt, consistency_prompt, created_by
		)
		VALUES ($1, $2, 'scene', '雨中庭院', '夜色中的雨中庭院', 'prompt_ready',
		        '雨中庭院场景四视图', '保持庭院空间关系和冷色逆光', $3)
		RETURNING id::text
	`, orgID, projectID, userID).Scan(&result.SceneID); err != nil {
		t.Fatalf("insert scene asset: %v", err)
	}
	seedStoryboardEpisodeV2AssetReference(t, ctx, pool, orgID, projectID, userID, result.SceneID, "scene")
	if err := pool.QueryRow(ctx, `
		INSERT INTO canonical_assets(
			organization_id, project_id, asset_type, name, description, status,
			base_prompt, consistency_prompt, created_by
		)
		VALUES ($1, $2, 'prop', '油纸伞', '雨中使用的油纸伞', 'prompt_ready',
		        '油纸伞道具四视图', '保持伞面纹理和结构一致', $3)
		RETURNING id::text
	`, orgID, projectID, userID).Scan(&result.PropID); err != nil {
		t.Fatalf("insert prop asset: %v", err)
	}
	seedStoryboardEpisodeV2AssetReference(t, ctx, pool, orgID, projectID, userID, result.PropID, "prop")
	return result
}

func seedStoryboardEpisodeV2AssetReference(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, projectID, userID, assetID, key string) {
	t.Helper()
	var artifactID string
	contentHash := strings.Repeat(fmt.Sprintf("%x", len(key)%16), 64)
	if err := pool.QueryRow(ctx, `
		INSERT INTO artifacts(
			organization_id, project_id, type, storage_key, mime_type,
			content_hash, metadata, created_by
		)
		VALUES ($1, $2, 'asset_reference_image', $3, 'image/png', $4,
		        jsonb_build_object('fixture', 'storyboard_episode_v2'), $5)
		RETURNING id::text
	`, orgID, projectID, "tests/storyboard-v2/"+key+".png", contentHash, userID).Scan(&artifactID); err != nil {
		t.Fatalf("insert asset reference artifact %s: %v", key, err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE canonical_assets
		SET primary_reference_artifact_id = $2,
		    primary_reference_storage_key = $3,
		    stale_state = 'fresh'
		WHERE id = $1
	`, assetID, artifactID, "tests/storyboard-v2/"+key+".png"); err != nil {
		t.Fatalf("link asset reference %s: %v", key, err)
	}
}

func seedStoryboardEpisodeV2AnchorImageOutputs(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, projectID, workflowRunID, userID, imageModelID string,
	roles []string,
) []provider.GatewayImageResponse {
	t.Helper()
	var accountID string
	if err := pool.QueryRow(ctx, `SELECT provider_account_id::text FROM provider_models WHERE id = $1`, imageModelID).Scan(&accountID); err != nil {
		t.Fatalf("load image provider account: %v", err)
	}
	outputs := make([]provider.GatewayImageResponse, 0, len(roles))
	for index, role := range roles {
		callID := uuid.NewString()
		if _, err := pool.Exec(ctx, `
			INSERT INTO provider_call_logs(
				id, organization_id, project_id, workflow_run_id, provider_account_id,
				provider_model_id, task_type, execution_mode, status, started_at, completed_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, 'image.generate', 'sync', 'succeeded', now(), now())
		`, callID, orgID, projectID, workflowRunID, accountID, imageModelID); err != nil {
			t.Fatalf("insert image provider call %d: %v", index, err)
		}
		storageKey := fmt.Sprintf("tests/storyboard-v2/first-last-%d-%s.png", index+1, role)
		contentHash := fmt.Sprintf("%064x", index+500)
		var artifactID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO artifacts(
				organization_id, project_id, workflow_run_id, type, storage_key,
				mime_type, content_hash, metadata, created_by
			)
			VALUES ($1, $2, $3, 'generated_image', $4, 'image/png', $5,
			        jsonb_build_object('fixture', 'first_last_anchor', 'anchorRole', $6::text), $7)
			RETURNING id::text
		`, orgID, projectID, workflowRunID, storageKey, contentHash, role, userID).Scan(&artifactID); err != nil {
			t.Fatalf("insert anchor artifact %d: %v", index, err)
		}
		var mediaFileID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO media_files(
				organization_id, project_id, artifact_id, storage_key, mime_type,
				byte_size, width, height, checksum, metadata, created_by
			)
			VALUES ($1, $2, $3, $4, 'image/png', 1024, 1536, 864, $5,
			        jsonb_build_object('fixture', 'first_last_anchor', 'anchorRole', $6::text), $7)
			RETURNING id::text
		`, orgID, projectID, artifactID, storageKey, contentHash, role, userID).Scan(&mediaFileID); err != nil {
			t.Fatalf("insert anchor media %d: %v", index, err)
		}
		width, height, aspectRatio := 1536, 864, "16:9"
		if role == videoproduction.AnchorRoleStoryboardSheet {
			width, height, aspectRatio = 1024, 1536, "2:3"
			if _, err := pool.Exec(ctx, `
				UPDATE media_files SET width = $2, height = $3 WHERE id = $1
			`, mediaFileID, width, height); err != nil {
				t.Fatalf("update storyboard sheet dimensions: %v", err)
			}
		}
		outputs = append(outputs, provider.GatewayImageResponse{
			ProviderRequestID: uuid.NewString(), ProviderCallID: callID, ModelID: imageModelID, Status: "succeeded",
			Output: provider.GatewayImageOutput{
				ArtifactID: artifactID, MediaFileID: mediaFileID, StorageKey: storageKey,
				MimeType: "image/png", Width: &width, Height: &height, AspectRatio: aspectRatio,
			},
		})
	}
	return outputs
}

func storyboardSheetFixturePNG(t *testing.T, width, height, rows, columns int) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	palette := []color.RGBA{
		{R: 42, G: 66, B: 92, A: 255},
		{R: 76, G: 94, B: 112, A: 255},
		{R: 112, G: 126, B: 136, A: 255},
		{R: 148, G: 150, B: 144, A: 255},
		{R: 178, G: 168, B: 146, A: 255},
		{R: 204, G: 188, B: 158, A: 255},
	}
	cellWidth, cellHeight := width/columns, height/rows
	ordinal := 0
	for row := 0; row < rows; row++ {
		for column := 0; column < columns; column++ {
			cell := image.Rect(column*cellWidth, row*cellHeight, (column+1)*cellWidth, (row+1)*cellHeight)
			draw.Draw(canvas, cell, &image.Uniform{C: palette[ordinal%len(palette)]}, image.Point{}, draw.Src)
			ordinal++
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, canvas); err != nil {
		t.Fatalf("encode storyboard sheet fixture: %v", err)
	}
	return buffer.Bytes()
}

func approveStoryboardEpisodeV2Anchor(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, projectID, userID, shotID string, ordinal int) {
	t.Helper()
	storageKey := fmt.Sprintf("tests/storyboard-v2/shot-%d-first-frame.png", ordinal+1)
	contentHash := fmt.Sprintf("%064x", ordinal+101)
	var artifactID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO artifacts(
			organization_id, project_id, type, storage_key, mime_type,
			content_hash, metadata, created_by
		)
		VALUES ($1, $2, 'storyboard_image', $3, 'image/png', $4,
		        jsonb_build_object('fixture', 'approved_planned_first_frame'), $5)
		RETURNING id::text
	`, orgID, projectID, storageKey, contentHash, userID).Scan(&artifactID); err != nil {
		t.Fatalf("insert approved first-frame artifact: %v", err)
	}
	result, err := pool.Exec(ctx, `
		UPDATE shot_visual_anchors
		SET status = 'ready', review_status = 'approved', artifact_id = $2,
		    storage_key = $3,
		    metadata = metadata || jsonb_build_object('fixtureReview', 'approved')
		WHERE storyboard_shot_id = $1
		  AND anchor_role = 'planned_first_frame'
		  AND status = 'draft'
	`, shotID, artifactID, storageKey)
	if err != nil {
		t.Fatalf("approve first-frame anchor: %v", err)
	}
	if result.RowsAffected() != 1 {
		t.Fatalf("approve first-frame anchor affected %d rows", result.RowsAffected())
	}
}

func persistStoryboardEpisodeV2ReferencePacks(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	activities Activities,
	orgID, projectID, workflowRunID, userID string,
	shots []PlannedStoryboardShot,
) {
	t.Helper()
	project, err := activities.projectProductionSettings(ctx, projectID)
	if err != nil {
		t.Fatalf("load project production settings: %v", err)
	}
	constraints := []provider.GatewayModelConstraintCandidate{{
		ProviderModelID: uuid.NewString(),
		ModelKey:        "fixture-image-model",
		Modality:        "image",
		Prompt:          provider.PromptLengthConstraint{MaxLength: 16000, Unit: provider.PromptLengthUnitCharacters},
		References:      provider.ReferenceConstraint{Supported: true, MaxReferences: 8},
	}}
	for _, shot := range shots {
		execution, err := StartNodeRun(ctx, pool, NodeRunInput{
			OrganizationID: orgID,
			ProjectID:      projectID,
			WorkflowRunID:  workflowRunID,
			NodeKey:        "test-reference-pack-" + shot.ID,
			NodeType:       "test.reference_pack.compile",
		})
		if err != nil {
			t.Fatalf("start reference pack node: %v", err)
		}
		contract, err := activities.loadShotProductionContract(ctx, projectID, shot.ID)
		if err != nil {
			t.Fatalf("load shot contract %s: %v", shot.ID, err)
		}
		candidates, err := activities.loadShotReferenceCandidates(ctx, projectID, shot.ID, contract.EntryState)
		if err != nil {
			t.Fatalf("load shot references %s: %v", shot.ID, err)
		}
		pack, err := resolveAnchorReferencePack(project, contract, candidates, constraints)
		if err != nil {
			t.Fatalf("resolve shot reference pack %s: %v", shot.ID, err)
		}
		packID, err := activities.persistShotReferencePack(ctx, PrepareShotImagePromptInput{
			OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
			CreatedBy: userID, ShotID: shot.ID,
		}, execution, project, contract, pack)
		if err != nil || packID == "" {
			t.Fatalf("persist shot reference pack %s: id=%q err=%v", shot.ID, packID, err)
		}
		contextPlan, err := activities.compileShotPromptContextPlan(ctx, project, mustLoadStoryboardShot(t, ctx, activities, projectID, shot.ID), contract.EntryState, constraints)
		if err != nil {
			t.Fatalf("compile prompt context plan %s: %v", shot.ID, err)
		}
		persistedContext, err := activities.persistPromptContextPlan(
			ctx, orgID, projectID, workflowRunID, userID, execution, project,
			mustLoadStoryboardShot(t, ctx, activities, projectID, shot.ID), contextPlan,
		)
		if err != nil || persistedContext.ID == "" {
			t.Fatalf("persist prompt context plan %s: id=%q err=%v", shot.ID, persistedContext.ID, err)
		}
		if err := CompleteNodeRun(ctx, pool, execution, mustJSON(map[string]any{"referencePackId": packID, "promptContextPlanId": persistedContext.ID})); err != nil {
			t.Fatalf("complete reference pack node: %v", err)
		}
	}
}

func mustLoadStoryboardShot(t *testing.T, ctx context.Context, activities Activities, projectID, shotID string) StoryboardShotRecord {
	t.Helper()
	shot, err := activities.storyboardShotByID(ctx, projectID, shotID)
	if err != nil {
		t.Fatalf("load storyboard shot %s: %v", shotID, err)
	}
	return shot
}

func seedStoryboardEpisodeV2ProviderCalls(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, projectID, workflowRunID, modelID string, count int) []string {
	t.Helper()
	var accountID string
	if err := pool.QueryRow(ctx, `SELECT provider_account_id::text FROM provider_models WHERE id = $1`, modelID).Scan(&accountID); err != nil {
		t.Fatalf("read provider account: %v", err)
	}
	ids := make([]string, count)
	for index := range ids {
		ids[index] = uuid.NewString()
		if _, err := pool.Exec(ctx, `
			INSERT INTO provider_call_logs(
				id, organization_id, project_id, workflow_run_id, provider_account_id,
				provider_model_id, task_type, execution_mode, status, started_at, completed_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, 'text.stream', 'stream', 'succeeded', now(), now())
		`, ids[index], orgID, projectID, workflowRunID, accountID, modelID); err != nil {
			t.Fatalf("insert provider call %d: %v", index, err)
		}
	}
	return ids
}

type storyboardEpisodeV2GatewayConfig struct {
	FirstLast       bool
	Multimodal      bool
	StoryboardSheet bool
	ImageModelID    string
	ImageOutput     []provider.GatewayImageResponse
	VideoPlanner    func(context.Context, provider.GatewayVideoPlanRequest) (provider.GatewayVideoPlanResponse, error)
}

func mockStoryboardEpisodeV2Gateway(t *testing.T, modelID string, callIDs []string, assets storyboardEpisodeV2Assets, sceneIDs []string, timing *TimingAnalysisActivityOutput) http.Handler {
	return mockStoryboardEpisodeV2GatewayWithConfig(t, modelID, callIDs, assets, sceneIDs, timing, storyboardEpisodeV2GatewayConfig{})
}

func mockStoryboardEpisodeV2GatewayWithConfig(t *testing.T, modelID string, callIDs []string, assets storyboardEpisodeV2Assets, sceneIDs []string, timing *TimingAnalysisActivityOutput, config storyboardEpisodeV2GatewayConfig) http.Handler {
	t.Helper()
	callIndex := 0
	imageIndex := 0
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/provider/video/plan" && config.VideoPlanner != nil {
			var req provider.GatewayVideoPlanRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode video plan request: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			output, err := config.VideoPlanner(r.Context(), req)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeWorkflowGatewayEnvelope(t, w, output)
			return
		}
		if r.URL.Path == "/internal/provider/image/generate" {
			var req provider.GatewayImageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode image gateway request: %v", err)
			}
			if imageIndex >= len(config.ImageOutput) {
				t.Fatalf("unexpected image gateway call %d", imageIndex)
			}
			output := config.ImageOutput[imageIndex]
			imageIndex++
			writeWorkflowGatewayEnvelope(t, w, output)
			return
		}
		if r.URL.Path == "/internal/provider/models/constraints" {
			var req provider.GatewayModelConstraintsRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode constraints request: %v", err)
			}
			candidate := provider.GatewayModelConstraintCandidate{
				ProviderModelID: modelID,
				ModelKey:        "fixture-model",
				Modality:        req.Modality,
				Prompt:          provider.PromptLengthConstraint{MaxLength: 16000, Unit: provider.PromptLengthUnitCharacters},
				ContextWindow:   32768,
				References:      provider.ReferenceConstraint{Supported: req.Modality != "text", MaxReferences: 8},
				NativeAudio:     provider.NativeAudioConstraint{Support: provider.VideoSupportUnknown},
			}
			if config.StoryboardSheet && req.Modality == "image" {
				candidate.ProviderModelID = config.ImageModelID
				candidate.ModelKey = "gpt-image-2"
				candidate.References = provider.ReferenceConstraint{
					Supported: true, MaxReferences: 8, MaxImageReferences: 8,
					SupportsSemanticReferenceImages: true,
				}
			}
			if req.Modality == "video" {
				candidate.References = provider.ReferenceConstraint{
					Supported: true, MaxReferences: 1, SupportsFirstFrame: true,
				}
				if config.FirstLast {
					candidate.References.MaxReferences = 2
					candidate.References.MaxImageReferences = 2
					candidate.References.SupportsLastFrame = true
					candidate.References.InputContracts = []string{provider.VideoInputContractFirstLastFrames}
				}
				if config.Multimodal {
					candidate.References.MaxReferences = 12
					candidate.References.MaxImageReferences = 9
					candidate.References.MaxVideoReferences = 2
					candidate.References.MaxAudioReferences = 1
					candidate.References.SupportsSemanticReferenceImages = true
					candidate.References.SupportsVideoReference = true
					candidate.References.SupportsAudioReference = true
					candidate.References.InputContracts = []string{provider.VideoInputContractFirstFramePlusReferences}
				}
				if config.StoryboardSheet {
					candidate.References = provider.ReferenceConstraint{
						Supported: true, MaxReferences: 1, MaxImageReferences: 1,
						SupportsStoryboardSheetReference: true,
						InputContracts:                   []string{provider.VideoInputContractStoryboardSheetReference},
					}
				}
				candidate.NativeAudio = provider.NativeAudioConstraint{
					Support: provider.VideoSupportTrue, SupportsDialogue: true, SupportsLipSync: true,
				}
			}
			writeWorkflowGatewayEnvelope(t, w, provider.GatewayModelConstraintsResponse{
				ModelProfileKey: req.ModelProfileKey, TaskType: req.TaskType,
				Modality: req.Modality, Candidates: []provider.GatewayModelConstraintCandidate{candidate},
			})
			return
		}
		if r.URL.Path == "/internal/provider/text/stream" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path != "/internal/provider/text/generate" {
			http.NotFound(w, r)
			return
		}
		var req provider.GatewayTextRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode gateway request: %v", err)
		}
		var promptInput struct {
			Prompt string `json:"prompt"`
		}
		if err := json.Unmarshal(req.Input, &promptInput); err != nil {
			t.Fatalf("decode prompt input: %v", err)
		}
		responseText := ""
		switch req.PromptTemplateKey {
		case promptKeyStoryboardTimingBatchAnalyzer:
			responseText = fmt.Sprintf(`{"scenes":[{"sceneKey":"scene-1","scriptSceneId":"%s","sceneOrdinal":0,"units":[{"unitKey":"unit-1","unitOrdinal":0,"type":"dialogue","track":"audio","speaker":"方源","text":"你终于来了","delivery":"normal","language":"zh"},{"unitKey":"unit-2","unitOrdinal":1,"type":"action","track":"visual","text":"方源抬头望向雨幕","actionKind":"simple","suggestedSeconds":2}]},{"sceneKey":"scene-2","scriptSceneId":"%s","sceneOrdinal":1,"units":[{"unitKey":"unit-3","unitOrdinal":2,"type":"dialogue","track":"audio","speaker":"白凝冰","text":"雨快停了","delivery":"normal","language":"zh"},{"unitKey":"unit-4","unitOrdinal":3,"type":"action","track":"visual","text":"白凝冰收起伞","actionKind":"simple","suggestedSeconds":2}]}]}`, sceneIDs[0], sceneIDs[1])
		case promptKeyStoryboardContinuityBlueprint:
			if timing == nil || len(timing.Scenes) != 2 {
				t.Fatalf("timing output unavailable for continuity blueprint")
			}
			firstKey := timing.Scenes[0].SceneKey
			secondKey := timing.Scenes[1].SceneKey
			responseText = fmt.Sprintf(`{"scenes":[{"sceneKey":"%s","sceneOrdinal":0,"pacingProfile":"standard","suggestedShotMinimum":1,"suggestedShotMaximum":3,"entryState":{},"exitState":{"gaze":"right"},"continuityNotes":["雨势连续"]},{"sceneKey":"%s","sceneOrdinal":1,"pacingProfile":"standard","suggestedShotMinimum":1,"suggestedShotMaximum":3,"entryState":{"gaze":"right"},"exitState":{},"continuityNotes":["承接雨势"]}],"dependencies":[{"fromSceneKey":"%s","toSceneKey":"%s","reason":"动作连续","strong":true}],"serialGroups":[["%s","%s"]],"parallelGroups":[]}`, firstKey, secondKey, firstKey, secondKey, firstKey, secondKey)
		case promptKeyStoryboardScenePlanner:
			sceneIndex := 0
			if timing != nil && len(timing.Scenes) > 1 && strings.Contains(promptInput.Prompt, `"sceneKey":"`+timing.Scenes[1].SceneKey+`"`) {
				sceneIndex = 1
			}
			if timing == nil || len(timing.Scenes) <= sceneIndex {
				t.Fatalf("timing output unavailable for scene planner")
			}
			unitIDs := make([]string, 0, len(timing.Scenes[sceneIndex].Units))
			for _, unit := range timing.Scenes[sceneIndex].Units {
				unitIDs = append(unitIDs, `"`+unit.ID+`"`)
			}
			videoDirection := "人物完成动作，保持雨势连续"
			if sceneIndex == 0 {
				videoDirection = "方源用中文说：你终于来了。随后抬头望向雨幕"
			} else {
				videoDirection = "白凝冰用中文说：雨快停了。随后收起伞"
			}
			characterID := assets.CharacterIDs[sceneIndex]
			entryAction := "人物准备开口"
			exitAction := "人物完成台词和动作"
			responseText = fmt.Sprintf(`{"sceneKey":"%s","shots":[{"suggestionKey":"scene-%d-shot-1","timingUnitIds":[%s],"cutReason":"完整动作节拍","title":"雨中对话","visual":"人物在雨中庭院完成动作","camera":"中景","motion":"缓慢推进","mood":"克制","oneTake":false,"imagePromptDirection":"雨中庭院，中景人物，保持角色外观一致，无文字","videoPromptDirection":"%s","assetRequirements":[{"assetId":"%s","requirementType":"character_appearance","roleInShot":"主体角色","pose":"站立"},{"assetId":"%s","requirementType":"scene_environment","roleInShot":"当前场景"}],"plannedEntryState":{"scene":{"assetId":"%s","timeOfDay":"night","weather":"light_rain","lighting":"cool_backlight"},"characters":[{"assetId":"%s","pose":"standing","expression":"guarded","blocking":{"horizontal":"center","depth":"midground","facing":"camera"}}],"props":[],"camera":{"shotSize":"medium","angle":"eye_level","axisSide":"A","lensIntent":"normal","movement":"dolly_in"},"action":{"entry":"%s","exit":"%s"},"screenDirection":"static"},"plannedExitState":{"scene":{"assetId":"%s","timeOfDay":"night","weather":"light_rain","lighting":"cool_backlight"},"characters":[{"assetId":"%s","pose":"standing","expression":"calm","blocking":{"horizontal":"center","depth":"midground","facing":"camera"}}],"props":[],"camera":{"shotSize":"medium","angle":"eye_level","axisSide":"A","lensIntent":"normal","movement":"dolly_in"},"action":{"entry":"%s","exit":"%s"},"screenDirection":"static"},"transitionFromPrevious":{"transitionType":"same_scene_cut","confidence":0.9}}]}`,
				timing.Scenes[sceneIndex].SceneKey, sceneIndex+1, strings.Join(unitIDs, ","), videoDirection,
				characterID, assets.SceneID, assets.SceneID, characterID, entryAction, exitAction,
				assets.SceneID, characterID, entryAction, exitAction)
			if config.Multimodal {
				responseText = strings.Replace(responseText, `],"plannedEntryState"`, fmt.Sprintf(`,{"assetId":"%s","requirementType":"prop_identity","roleInShot":"关键道具"}],"plannedEntryState"`, assets.PropID), 1)
				responseText = strings.ReplaceAll(responseText, `"props":[]`, fmt.Sprintf(`"props":[{"assetId":"%s","state":"placed"}]`, assets.PropID))
			}
		case promptKeyStoryboardPlanReviewer:
			responseText = `{"approved":true,"issues":[],"corrections":[]}`
		case "video_profile.single_frame_i2v.anchor.generate":
			responseText = `{"prompt":"电影感雨中庭院，冷色逆光，中景单人构图，角色站在画面中央，面部和服装与参考保持一致，动作停留在开口前一刻，画面纯净","negativePrompt":"多余人物，错误场景，身份漂移，字幕，水印，标志，界面","dialogueLines":[],"sourceAnchors":[],"assetAnchors":[],"conflictsResolved":[]}`
		case "video_profile.single_frame_i2v.anchor.review":
			responseText = `{"approved":true,"prompt":"电影感雨中庭院，冷色逆光，中景单人构图，角色站在画面中央，面部和服装与参考保持一致，动作停留在开口前一刻，画面纯净","finalPrompt":"电影感雨中庭院，冷色逆光，中景单人构图，角色站在画面中央，面部和服装与参考保持一致，动作停留在开口前一刻，画面纯净","negativePrompt":"多余人物，错误场景，身份漂移，字幕，水印，标志，界面","dialogueLines":[],"sourceAnchors":[],"issues":[],"changes":[]}`
		case "video_profile.first_last_frame.anchor.generate":
			if strings.Contains(promptInput.Prompt, videoproduction.AnchorRolePlannedLastFrame) {
				responseText = `{"prompt":"电影感雨中庭院，冷色逆光，中景单人构图，角色保持身份服装一致，完成台词和动作后的计划尾帧，构图稳定，无文字","negativePrompt":"多余人物，错误场景，身份漂移，字幕，水印，标志，界面","dialogueLines":[],"sourceAnchors":[],"assetAnchors":[],"conflictsResolved":[]}`
			} else {
				responseText = `{"prompt":"电影感雨中庭院，冷色逆光，中景单人构图，角色保持身份服装一致，动作开始前的计划首帧，构图稳定，无文字","negativePrompt":"多余人物，错误场景，身份漂移，字幕，水印，标志，界面","dialogueLines":[],"sourceAnchors":[],"assetAnchors":[],"conflictsResolved":[]}`
			}
		case "video_profile.first_last_frame.anchor.review":
			if strings.Contains(promptInput.Prompt, "计划尾帧") || strings.Contains(promptInput.Prompt, videoproduction.AnchorRolePlannedLastFrame) {
				responseText = `{"approved":true,"prompt":"电影感雨中庭院，冷色逆光，中景单人构图，角色保持身份服装一致，完成台词和动作后的计划尾帧，构图稳定，无文字","finalPrompt":"电影感雨中庭院，冷色逆光，中景单人构图，角色保持身份服装一致，完成台词和动作后的计划尾帧，构图稳定，无文字","negativePrompt":"多余人物，错误场景，身份漂移，字幕，水印，标志，界面","dialogueLines":[],"sourceAnchors":[],"issues":[],"changes":[]}`
			} else {
				responseText = `{"approved":true,"prompt":"电影感雨中庭院，冷色逆光，中景单人构图，角色保持身份服装一致，动作开始前的计划首帧，构图稳定，无文字","finalPrompt":"电影感雨中庭院，冷色逆光，中景单人构图，角色保持身份服装一致，动作开始前的计划首帧，构图稳定，无文字","negativePrompt":"多余人物，错误场景，身份漂移，字幕，水印，标志，界面","dialogueLines":[],"sourceAnchors":[],"issues":[],"changes":[]}`
			}
		case "video_profile.multimodal_reference.anchor.generate":
			responseText = `{"prompt":"电影感雨中庭院，冷色逆光，中景人物构图，方源身份只采用角色引用，庭院空间只采用场景引用，计划首帧动作尚未开始，无文字","negativePrompt":"引用串位，多余人物，错误场景，错误道具，字幕，水印，标志，界面","dialogueLines":[],"sourceAnchors":[],"assetAnchors":[],"conflictsResolved":[]}`
		case "video_profile.multimodal_reference.anchor.review":
			responseText = `{"approved":true,"prompt":"电影感雨中庭院，冷色逆光，中景人物构图，方源身份只采用角色引用，庭院空间只采用场景引用，计划首帧动作尚未开始，无文字","finalPrompt":"电影感雨中庭院，冷色逆光，中景人物构图，方源身份只采用角色引用，庭院空间只采用场景引用，计划首帧动作尚未开始，无文字","negativePrompt":"引用串位，多余人物，错误场景，错误道具，字幕，水印，标志，界面","dialogueLines":[],"sourceAnchors":[],"issues":[],"changes":[]}`
		case "video_profile.storyboard_sheet.anchor.generate":
			responseText = `{"prompt":"单张竖向三格分镜板，严格三行一列，从上到下依次表现人物准备开口、说出台词并抬头、动作完成；每格人物身份服装、雨中庭院、冷色逆光和机位轴线一致，画面中不得出现任何文字数字编号字幕水印或界面","negativePrompt":"文字，数字，编号，字幕，对话框，水印，标志，界面，额外画格，动作乱序，身份漂移，错误场景","dialogueLines":[],"sourceAnchors":[],"assetAnchors":[],"conflictsResolved":[]}`
		case "video_profile.storyboard_sheet.anchor.review":
			responseText = `{"approved":true,"prompt":"单张竖向三格分镜板，严格三行一列，从上到下依次表现人物准备开口、说出台词并抬头、动作完成；每格人物身份服装、雨中庭院、冷色逆光和机位轴线一致，画面中不得出现任何文字数字编号字幕水印或界面","finalPrompt":"单张竖向三格分镜板，严格三行一列，从上到下依次表现人物准备开口、说出台词并抬头、动作完成；每格人物身份服装、雨中庭院、冷色逆光和机位轴线一致，画面中不得出现任何文字数字编号字幕水印或界面","negativePrompt":"文字，数字，编号，字幕，对话框，水印，标志，界面，额外画格，动作乱序，身份漂移，错误场景","dialogueLines":[],"sourceAnchors":[],"issues":[],"changes":[]}`
		case "video_profile.storyboard_sheet.output.review":
			responseText = `{"approved":true,"panelCountObserved":3,"ordered":true,"noVisibleText":true,"identityConsistent":true,"sceneConsistent":true,"actionSequenceValid":true,"issues":[]}`
		case "video_profile.single_frame_i2v.video.generate":
			dialogue := "雨快停了"
			speaker := "白凝冰"
			if strings.Contains(promptInput.Prompt, "你终于来了") {
				dialogue = "你终于来了"
				speaker = "方源"
			}
			responseText = fmt.Sprintf(`{"prompt":"电影感雨中庭院，冷色逆光，角色保持首帧身份和站位，镜头缓慢推进，%s用中文逐字说：%s，随后完成剧本动作","negativePrompt":"身份漂移，错误场景，字幕，水印","dialogueLines":[{"speaker":"%s","text":"%s","kind":"dialogue"}],"sourceAnchors":[],"notes":[]}`, speaker, dialogue, speaker, dialogue)
		case "video_profile.single_frame_i2v.video.review":
			dialogue := "雨快停了"
			speaker := "白凝冰"
			if strings.Contains(promptInput.Prompt, "你终于来了") {
				dialogue = "你终于来了"
				speaker = "方源"
			}
			finalPrompt := fmt.Sprintf("电影感雨中庭院，冷色逆光，角色保持首帧身份和站位，镜头缓慢推进，%s用中文逐字说：%s，随后完成剧本动作", speaker, dialogue)
			responseText = fmt.Sprintf(`{"approved":true,"prompt":%q,"finalPrompt":%q,"negativePrompt":"身份漂移，错误场景，字幕，水印","dialogueLines":[{"speaker":"%s","text":"%s","kind":"dialogue"}],"issues":[],"changes":[]}`, finalPrompt, finalPrompt, speaker, dialogue)
		case "video_profile.first_last_frame.video.generate":
			dialogue := "你终于来了"
			speaker := "方源"
			if strings.Contains(promptInput.Prompt, "雨快停了") {
				dialogue = "雨快停了"
				speaker = "白凝冰"
			}
			responseText = fmt.Sprintf(`{"prompt":"角色从计划首帧自然运动到计划尾帧，身份、服装、道具和空间轴线保持一致，%s用中文逐字说：%s，动作在镜头时长内可达","negativePrompt":"身份漂移，错误场景，动作跳变，字幕，水印","dialogueLines":[{"speaker":"%s","text":"%s","kind":"dialogue"}],"sourceAnchors":[],"notes":[]}`, speaker, dialogue, speaker, dialogue)
		case "video_profile.first_last_frame.video.review":
			dialogue := "你终于来了"
			speaker := "方源"
			if strings.Contains(promptInput.Prompt, "雨快停了") {
				dialogue = "雨快停了"
				speaker = "白凝冰"
			}
			finalPrompt := fmt.Sprintf("角色从计划首帧自然运动到计划尾帧，身份、服装、道具和空间轴线保持一致，%s用中文逐字说：%s，动作在镜头时长内可达", speaker, dialogue)
			responseText = fmt.Sprintf(`{"approved":true,"prompt":%q,"finalPrompt":%q,"negativePrompt":"身份漂移，错误场景，动作跳变，字幕，水印","dialogueLines":[{"speaker":"%s","text":"%s","kind":"dialogue"}],"issues":[],"changes":[]}`, finalPrompt, finalPrompt, speaker, dialogue)
		case "video_profile.multimodal_reference.video.generate":
			responseText = `{"prompt":"以当前首帧为唯一动作起点，方源身份只使用 character_identity，雨中庭院只使用 scene_identity，镜头缓慢推进，方源用中文逐字说：你终于来了，随后抬头望向雨幕","negativePrompt":"引用串位，身份漂移，错误场景，新增人物，字幕，水印","dialogueLines":[{"speaker":"方源","text":"你终于来了","kind":"dialogue"}],"sourceAnchors":[],"referenceUsage":[],"notes":[]}`
		case "video_profile.multimodal_reference.video.review":
			finalPrompt := "以当前首帧为唯一动作起点，方源身份只使用 character_identity，雨中庭院只使用 scene_identity，镜头缓慢推进，方源用中文逐字说：你终于来了，随后抬头望向雨幕"
			responseText = fmt.Sprintf(`{"approved":true,"prompt":%q,"finalPrompt":%q,"negativePrompt":"引用串位，身份漂移，错误场景，新增人物，字幕，水印","dialogueLines":[{"speaker":"方源","text":"你终于来了","kind":"dialogue"}],"issues":[],"changes":[]}`, finalPrompt, finalPrompt)
		case "video_profile.storyboard_sheet.video.generate":
			responseText = `{"prompt":"把 storyboard_sheet 仅作为 ordered_keyframe_sheet 动作指导，严格从上到下执行三格动作：方源准备开口，方源用中文逐字说：你终于来了，随后抬头望向雨幕并完成动作；人物身份服装、雨中庭院、冷色逆光、空间轴线和机位连续","negativePrompt":"把分镜板当首帧，动作乱序，身份漂移，错误场景，新增人物，字幕，水印","dialogueLines":[{"speaker":"方源","text":"你终于来了","kind":"dialogue"}],"sourceAnchors":[],"notes":[]}`
		case "video_profile.storyboard_sheet.video.review":
			finalPrompt := "把 storyboard_sheet 仅作为 ordered_keyframe_sheet 动作指导，严格从上到下执行三格动作：方源准备开口，方源用中文逐字说：你终于来了，随后抬头望向雨幕并完成动作；人物身份服装、雨中庭院、冷色逆光、空间轴线和机位连续"
			responseText = fmt.Sprintf(`{"approved":true,"prompt":%q,"finalPrompt":%q,"negativePrompt":"把分镜板当首帧，动作乱序，身份漂移，错误场景，新增人物，字幕，水印","dialogueLines":[{"speaker":"方源","text":"你终于来了","kind":"dialogue"}],"issues":[],"changes":[]}`, finalPrompt, finalPrompt)
		default:
			t.Fatalf("unexpected prompt template %q", req.PromptTemplateKey)
		}
		if callIndex >= len(callIDs) {
			t.Fatalf("unexpected provider call index %d", callIndex)
		}
		writeWorkflowGatewayEnvelope(t, w, provider.GatewayTextResponse{
			ProviderCallID: callIDs[callIndex], ModelID: modelID, Status: "succeeded",
			Output: provider.GatewayTextOutput{Text: responseText}, LatencyMS: 10,
		})
		callIndex++
	})
}

func assertStoryboardEpisodeV2Persistence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, projectID, workflowRunID, episodeID, planID string) {
	t.Helper()
	var analysisCount, sceneCount, shotCount, spanCount, requirementCount, reviewCount, artifactCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM script_timing_analyses WHERE project_id = $1 AND script_episode_id = $2 AND status = 'ready'`, projectID, episodeID).Scan(&analysisCount); err != nil {
		t.Fatalf("count timing analyses: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM storyboard_scene_plans WHERE storyboard_plan_id = $1 AND status = 'ready'`, planID).Scan(&sceneCount); err != nil {
		t.Fatalf("count scene plans: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM storyboard_shots WHERE storyboard_plan_id = $1 AND deleted_at IS NULL`, planID).Scan(&shotCount); err != nil {
		t.Fatalf("count shots: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM storyboard_shot_timing_spans WHERE storyboard_plan_id = $1`, planID).Scan(&spanCount); err != nil {
		t.Fatalf("count timing spans: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM shot_asset_requirements requirement JOIN storyboard_shots shot ON shot.id = requirement.storyboard_shot_id WHERE shot.storyboard_plan_id = $1`, planID).Scan(&requirementCount); err != nil {
		t.Fatalf("count requirements: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM storyboard_plan_reviews WHERE storyboard_plan_id = $1 AND approved`, planID).Scan(&reviewCount); err != nil {
		t.Fatalf("count reviews: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM artifacts WHERE workflow_run_id = $1 AND metadata->>'storyboardPlanId' = $2`, workflowRunID, planID).Scan(&artifactCount); err != nil {
		t.Fatalf("count artifacts: %v", err)
	}
	if analysisCount != 1 || sceneCount != 2 || shotCount < 2 || spanCount < 4 || requirementCount < 2 || reviewCount != 1 || artifactCount != 1 {
		t.Fatalf("persisted counts analysis=%d scenes=%d shots=%d spans=%d requirements=%d reviews=%d artifacts=%d", analysisCount, sceneCount, shotCount, spanCount, requirementCount, reviewCount, artifactCount)
	}
	var active, valid bool
	if err := pool.QueryRow(ctx, `SELECT active, status = 'ready' FROM storyboard_plans WHERE id = $1`, planID).Scan(&active, &valid); err != nil {
		t.Fatalf("read plan state: %v", err)
	}
	if !active || !valid {
		t.Fatalf("plan active=%v ready=%v", active, valid)
	}
	var dialogueLeakCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM storyboard_shots
		WHERE storyboard_plan_id = $1
		  AND COALESCE(metadata->>'imagePromptDirection', '') ~ '(你终于来了|雨快停了)'
	`, planID).Scan(&dialogueLeakCount); err != nil {
		t.Fatalf("check image dialogue leakage: %v", err)
	}
	if dialogueLeakCount != 0 {
		t.Fatalf("image prompt dialogue leak count = %d", dialogueLeakCount)
	}
	var stateCount, transitionCount, anchorCount, contractEventCount, referencePackCount, referenceItemCount, linkedAnchorCount, contextPlanCount, promptProvenanceCount int
	var videoPromptPlanCount, videoPromptCueCount, nativeAudioContractCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM storyboard_shot_state_versions state
		JOIN storyboard_shots shot ON shot.id = state.storyboard_shot_id
		WHERE shot.storyboard_plan_id = $1
		  AND state.status = 'approved'
		  AND state.state_role IN ('planned_entry', 'planned_exit')
		  AND state.state_hash ~ '^[0-9a-f]{64}$'
	`, planID).Scan(&stateCount); err != nil {
		t.Fatalf("count shot states: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM storyboard_shot_transitions transition
		WHERE transition.storyboard_plan_id = $1
		  AND transition.status = 'active'
		  AND transition.review_status = 'approved'
		  AND transition.tail_policy IN ('soft', 'none')
		  AND transition.metadata->>'transitionHash' ~ '^[0-9a-f]{64}$'
	`, planID).Scan(&transitionCount); err != nil {
		t.Fatalf("count shot transitions: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM shot_visual_anchors anchor
		JOIN storyboard_shots shot ON shot.id = anchor.storyboard_shot_id
		WHERE shot.storyboard_plan_id = $1
		  AND anchor.anchor_role = 'planned_first_frame'
		  AND anchor.status = 'ready'
		  AND anchor.review_status = 'approved'
	`, planID).Scan(&anchorCount); err != nil {
		t.Fatalf("count shot anchors: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM event_outbox
		WHERE project_id = $1
		  AND event_type IN ('storyboard.shot.state.planned', 'storyboard.shot.transition.planned')
		  AND payload->>'workflowRunId' = $2
	`, projectID, workflowRunID).Scan(&contractEventCount); err != nil {
		t.Fatalf("count shot contract events: %v", err)
	}
	if stateCount != shotCount*2 || transitionCount != shotCount || anchorCount != shotCount || contractEventCount != shotCount*2 {
		t.Fatalf("shot contracts states=%d transitions=%d anchors=%d events=%d shots=%d", stateCount, transitionCount, anchorCount, contractEventCount, shotCount)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(item_count), 0)
		FROM (
			SELECT pack.id, COUNT(item.id) AS item_count
			FROM shot_reference_packs pack
			JOIN storyboard_shots shot ON shot.id = pack.storyboard_shot_id
			LEFT JOIN shot_reference_pack_items item ON item.reference_pack_id = pack.id
			WHERE shot.storyboard_plan_id = $1
			  AND pack.status = 'active'
			  AND pack.manifest->>'purpose' = 'video'
			  AND item.role = 'first_frame'
			GROUP BY pack.id
		) packs
	`, planID).Scan(&referencePackCount, &referenceItemCount); err != nil {
		t.Fatalf("count reference packs: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM shot_visual_anchors anchor
		JOIN storyboard_shots shot ON shot.id = anchor.storyboard_shot_id
		WHERE shot.storyboard_plan_id = $1
		  AND anchor.anchor_role = 'planned_first_frame'
		  AND anchor.reference_pack_id IS NOT NULL
	`, planID).Scan(&linkedAnchorCount); err != nil {
		t.Fatalf("count linked anchors: %v", err)
	}
	if referencePackCount != shotCount || referenceItemCount != shotCount || linkedAnchorCount != shotCount {
		t.Fatalf("reference packs=%d items=%d linked anchors=%d shots=%d", referencePackCount, referenceItemCount, linkedAnchorCount, shotCount)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM prompt_context_plans context_plan
		JOIN storyboard_shots shot ON shot.id = context_plan.storyboard_shot_id
		WHERE shot.storyboard_plan_id = $1
		  AND context_plan.status = 'active'
		  AND context_plan.plan_hash ~ '^[0-9a-f]{64}$'
		  AND jsonb_array_length(context_plan.verbatim_dialogue_cues) > 0
		  AND context_plan.current_scene_script <> ''
		  AND context_plan.episode_continuity_digest <> ''
	`, planID).Scan(&contextPlanCount); err != nil {
		t.Fatalf("count prompt context plans: %v", err)
	}
	if contextPlanCount != shotCount {
		t.Fatalf("prompt context plans=%d shots=%d", contextPlanCount, shotCount)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM shot_visual_anchors anchor
		JOIN storyboard_shots shot ON shot.id = anchor.storyboard_shot_id
		JOIN prompt_versions prompt_version ON prompt_version.id = anchor.prompt_version_id
		WHERE shot.storyboard_plan_id = $1
		  AND anchor.anchor_role = 'planned_first_frame'
		  AND anchor.prompt_hash ~ '^[0-9a-f]{64}$'
		  AND prompt_version.version = 2
		  AND anchor.metadata->'generationContract'->>'contractHash' ~ '^[0-9a-f]{64}$'
		  AND anchor.metadata->'reviewContract'->>'contractHash' ~ '^[0-9a-f]{64}$'
		  AND anchor.metadata->>'promptContextPlanHash' ~ '^[0-9a-f]{64}$'
	`, planID).Scan(&promptProvenanceCount); err != nil {
		t.Fatalf("count anchor prompt provenance: %v", err)
	}
	if promptProvenanceCount != shotCount {
		t.Fatalf("anchor prompt provenance=%d shots=%d", promptProvenanceCount, shotCount)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(cue_count), 0)
		FROM (
			SELECT plan.id, COUNT(cue.id) AS cue_count
			FROM video_prompt_plans plan
			JOIN storyboard_shots shot ON shot.id = plan.storyboard_shot_id
			LEFT JOIN video_prompt_plan_dialogue_cues cue ON cue.video_prompt_plan_id = plan.id
			WHERE shot.storyboard_plan_id = $1
			  AND plan.status = 'approved'
			  AND plan.native_audio_required
			  AND plan.prompt_hash ~ '^[0-9a-f]{64}$'
			  AND plan.prompt_context_plan_hash ~ '^[0-9a-f]{64}$'
			  AND plan.profile_snapshot_hash ~ '^[0-9a-f]{64}$'
			  AND plan.shot_state_hash ~ '^[0-9a-f]{64}$'
			  AND plan.transition_hash ~ '^[0-9a-f]{64}$'
			  AND plan.reference_pack_hash ~ '^[0-9a-f]{64}$'
			  AND plan.capability_snapshot_hash ~ '^[0-9a-f]{64}$'
			  AND plan.rendered_prompt LIKE '%cineweave_authoritative_audio_timeline%'
			GROUP BY plan.id
		) plans
	`, planID).Scan(&videoPromptPlanCount, &videoPromptCueCount); err != nil {
		t.Fatalf("count approved video prompt plans: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM video_native_audio_contracts contract
		JOIN storyboard_shots shot ON shot.id = contract.storyboard_shot_id
		WHERE shot.storyboard_plan_id = $1
		  AND contract.status = 'active'
		  AND contract.native_audio_required
		  AND contract.dialogue_cues_hash ~ '^[0-9a-f]{64}$'
		  AND contract.contract_hash ~ '^[0-9a-f]{64}$'
	`, planID).Scan(&nativeAudioContractCount); err != nil {
		t.Fatalf("count native audio contracts: %v", err)
	}
	if videoPromptPlanCount != shotCount || videoPromptCueCount != shotCount || nativeAudioContractCount != shotCount {
		t.Fatalf("video prompt plans=%d cues=%d audio contracts=%d shots=%d", videoPromptPlanCount, videoPromptCueCount, nativeAudioContractCount, shotCount)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin validation transaction: %v", err)
	}
	defer tx.Rollback(ctx)
	report, err := storyboardpkg.ValidateStoryboardPlanTx(ctx, tx, projectID, episodeID, planID)
	if err != nil || !report.Valid {
		t.Fatalf("validate persisted plan report=%+v err=%v", report, err)
	}
}
