package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestVideoProductionDiagnosticsAndManualPromptRevision(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run video production diagnostic API tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for video production diagnostic API tests")
	}
	t.Setenv(videoproduction.FeatureFlagEnvironmentVariable, "true")
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	authService := auth.NewService(pool, "video-production-diagnostics-secret", time.Hour, 24*time.Hour)
	handler := New(pool, authService, nil, nil, nil).Handler()
	suffix := uuid.NewString()
	registration, err := authService.Register(ctx, auth.RegisterRequest{
		Email:            "video-diagnostics-" + suffix + "@example.test",
		Username:         randomStorageSegment(),
		Password:         "Password123!",
		DisplayName:      "Video Diagnostics",
		OrganizationName: "Video Diagnostics " + suffix,
		WorkspaceName:    "Video Diagnostics Workspace",
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	if err != nil {
		t.Fatalf("register test user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, registration.OrganizationID)
		pool.Close()
	})
	var project Project
	doAPISuccess(t, handler, http.MethodPost, "/api/projects", registration.AccessToken, registration.OrganizationID, map[string]any{
		"workspaceId":               registration.WorkspaceID,
		"name":                      "Video Diagnostics Project " + suffix,
		"videoProductionProfileKey": videoproduction.ProfileSingleFrameI2V,
	}, &project)
	if project.VideoProductionBinding == nil || project.ProductionGeneration == nil {
		t.Fatalf("project production identity is missing: %#v", project)
	}

	seed := seedVideoProductionDiagnosticGraph(t, ctx, pool, project, registration.OrganizationID, registration.User.ID)
	storyboardSheetManifestID := seedStoryboardSheetDiagnosticGraph(t, ctx, pool, project, registration.OrganizationID, registration.User.ID, seed.shotID)
	base := "/api/projects/" + project.ID + "/storyboard-shots/" + seed.shotID
	var states struct {
		Items []StoryboardShotStateVersionDetail `json:"items"`
	}
	doAPISuccess(t, handler, http.MethodGet, base+"/state", registration.AccessToken, registration.OrganizationID, nil, &states)
	if len(states.Items) != 2 {
		t.Fatalf("state revisions = %d, want 2", len(states.Items))
	}
	var transitionResponse struct {
		Active *StoryboardShotTransitionDetail  `json:"active"`
		Items  []StoryboardShotTransitionDetail `json:"items"`
	}
	doAPISuccess(t, handler, http.MethodGet, base+"/transition", registration.AccessToken, registration.OrganizationID, nil, &transitionResponse)
	if transitionResponse.Active == nil || transitionResponse.Active.Revision != 1 {
		t.Fatalf("transition response = %#v", transitionResponse)
	}
	var anchors struct {
		Items []ShotVisualAnchorDetail `json:"items"`
	}
	doAPISuccess(t, handler, http.MethodGet, base+"/anchors", registration.AccessToken, registration.OrganizationID, nil, &anchors)
	approvedPlannedAnchor := false
	for _, anchor := range anchors.Items {
		if anchor.AnchorRole == "planned_first_frame" && anchor.ReviewStatus == "approved" {
			approvedPlannedAnchor = true
			break
		}
	}
	if !approvedPlannedAnchor {
		t.Fatalf("anchors = %#v", anchors.Items)
	}
	var referenceResponse struct {
		Pack    *ShotReferencePackDetail      `json:"pack"`
		Items   []ShotReferencePackItemDetail `json:"items"`
		History []ShotReferencePackDetail     `json:"history"`
	}
	doAPISuccess(t, handler, http.MethodGet, base+"/reference-pack", registration.AccessToken, registration.OrganizationID, nil, &referenceResponse)
	if referenceResponse.Pack == nil || referenceResponse.Pack.ID != seed.anchorReferencePackID ||
		referenceResponse.Pack.Purpose != "anchor" || len(referenceResponse.Items) != 1 ||
		referenceResponse.Pack.Status != "active" {
		t.Fatalf("reference response = %#v", referenceResponse)
	}
	var videoReferenceResponse struct {
		Pack    *ShotReferencePackDetail      `json:"pack"`
		Items   []ShotReferencePackItemDetail `json:"items"`
		History []ShotReferencePackDetail     `json:"history"`
	}
	doAPISuccess(t, handler, http.MethodGet, base+"/reference-pack?purpose=video", registration.AccessToken, registration.OrganizationID, nil, &videoReferenceResponse)
	if videoReferenceResponse.Pack == nil || videoReferenceResponse.Pack.ID != seed.videoReferencePackID ||
		videoReferenceResponse.Pack.Purpose != "video" || len(videoReferenceResponse.Items) != 1 ||
		videoReferenceResponse.Pack.Status != "active" {
		t.Fatalf("video reference response = %#v", videoReferenceResponse)
	}
	var storyboardSheetResponse struct {
		Active   *StoryboardSheetManifestDetail  `json:"active"`
		Manifest *StoryboardSheetManifestDetail  `json:"manifest"`
		Panels   []StoryboardSheetPanelDetail    `json:"panels"`
		History  []StoryboardSheetManifestDetail `json:"history"`
	}
	doAPISuccess(t, handler, http.MethodGet, base+"/storyboard-sheet", registration.AccessToken, registration.OrganizationID, nil, &storyboardSheetResponse)
	if storyboardSheetResponse.Active == nil || storyboardSheetResponse.Manifest == nil ||
		storyboardSheetResponse.Active.ID != storyboardSheetManifestID || len(storyboardSheetResponse.History) != 1 ||
		len(storyboardSheetResponse.Panels) != 3 {
		t.Fatalf("storyboard sheet response = %#v", storyboardSheetResponse)
	}
	for index, panel := range storyboardSheetResponse.Panels {
		if panel.Ordinal != index+1 || panel.ManifestID != storyboardSheetManifestID {
			t.Fatalf("storyboard sheet panel order = %#v", storyboardSheetResponse.Panels)
		}
	}
	var promptResponse struct {
		Active      *VideoPromptPlanDetail   `json:"active"`
		Items       []VideoPromptPlanDetail  `json:"items"`
		ContextPlan *PromptContextPlanDetail `json:"contextPlan"`
	}
	doAPISuccess(t, handler, http.MethodGet, base+"/video-prompt-plan", registration.AccessToken, registration.OrganizationID, nil, &promptResponse)
	if promptResponse.Active == nil || promptResponse.ContextPlan == nil || promptResponse.Active.Revision != 1 {
		t.Fatalf("prompt response = %#v", promptResponse)
	}
	var renderPlan VideoRenderPlanDetail
	doAPISuccess(t, handler, http.MethodGet, base+"/video-render-plan", registration.AccessToken, registration.OrganizationID, nil, &renderPlan)
	if renderPlan.ID != seed.renderPlanID || renderPlan.ProductionGenerationID != project.ProductionGeneration.ID || renderPlan.VideoPromptPlanID == nil {
		t.Fatalf("render plan provenance = %#v", renderPlan)
	}
	activityWorkflowRunID := seedWorkflowVideoProductionActivity(t, ctx, pool, project, registration.OrganizationID, registration.User.ID, seed)
	var activity WorkflowVideoProductionActivity
	doAPISuccess(t, handler, http.MethodGet, "/api/workflow-runs/"+activityWorkflowRunID+"/video-production", registration.AccessToken, registration.OrganizationID, nil, &activity)
	if activity.TotalItems != 1 || activity.FailedItems != 1 || len(activity.Checkpoints) != 1 {
		t.Fatalf("video production activity aggregate = %#v", activity)
	}
	checkpoint := activity.Checkpoints[0]
	if checkpoint.EpisodeIndex != 1 || checkpoint.EpisodeTitle != "第 1 集" || len(checkpoint.Batches) != 1 || len(checkpoint.Batches[0].Items) != 1 {
		t.Fatalf("video production activity checkpoint = %#v", checkpoint)
	}
	activityItem := checkpoint.Batches[0].Items[0]
	if activityItem.ReferencePackID == nil || *activityItem.ReferencePackID != seed.videoReferencePackID ||
		activityItem.VideoPromptPlanID == nil || *activityItem.VideoPromptPlanID != seed.promptPlanID ||
		activityItem.ProviderErrorMessage != nil || activityItem.ErrorCode == nil || *activityItem.ErrorCode != "UPSTREAM_TIMEOUT" ||
		!strings.Contains(string(activityItem.ErrorDetail), "上游视频任务超时") {
		t.Fatalf("video production activity item = %#v", activityItem)
	}
	assertWorkflowVideoProductionMediaLifecycle(
		t, ctx, pool, handler, registration.AccessToken, registration.OrganizationID,
		registration.User.ID, project, activityWorkflowRunID, seed,
	)

	var manual VideoPromptPlanDetail
	doAPISuccess(t, handler, http.MethodPost, base+"/video-prompt-plan/revisions", registration.AccessToken, registration.OrganizationID, map[string]any{
		"expectedRevision": 1,
		"renderedPrompt":   "手工修订后的镜头视频提示词，保留完整剧情与中文台词。",
		"reason":           "integration test",
	}, &manual)
	if manual.Revision != 2 || manual.Status != "approved" || !strings.Contains(manual.RenderedPrompt, "手工修订") {
		t.Fatalf("manual prompt revision = %#v", manual)
	}
	var staleRenderStatus string
	var activeRenderPlanID *string
	if err := pool.QueryRow(ctx, `
		SELECT plan.status, shot.active_video_render_plan_id::text
		FROM video_render_plans plan JOIN storyboard_shots shot ON shot.id = plan.storyboard_shot_id
		WHERE plan.id = $1
	`, seed.renderPlanID).Scan(&staleRenderStatus, &activeRenderPlanID); err != nil {
		t.Fatalf("verify stale render plan: %v", err)
	}
	if staleRenderStatus != "stale" || activeRenderPlanID != nil {
		t.Fatalf("render plan after manual revision = %s / %v", staleRenderStatus, activeRenderPlanID)
	}

	var rejected ShotVisualAnchorDetail
	doAPISuccess(t, handler, http.MethodPost, base+"/anchors/"+seed.anchorID+"/reject", registration.AccessToken, registration.OrganizationID, map[string]any{
		"expectedRevision": 1,
		"reason":           "构图不符合镜头状态",
	}, &rejected)
	if rejected.ReviewStatus != "rejected" {
		t.Fatalf("rejected anchor = %#v", rejected)
	}
	var anchorPackStatus, videoPackStatus, contextStatus, promptStatus string
	if err := pool.QueryRow(ctx, `
		SELECT (SELECT status FROM shot_reference_packs WHERE id = $1),
		       (SELECT status FROM shot_reference_packs WHERE id = $2),
		       (SELECT status FROM prompt_context_plans WHERE id = $3),
		       (SELECT status FROM video_prompt_plans WHERE id = $4)
	`, seed.anchorReferencePackID, seed.videoReferencePackID, seed.contextPlanID, manual.ID).Scan(
		&anchorPackStatus, &videoPackStatus, &contextStatus, &promptStatus,
	); err != nil {
		t.Fatalf("verify anchor review invalidation: %v", err)
	}
	if anchorPackStatus != "stale" || videoPackStatus != "stale" || contextStatus != "stale" || promptStatus != "stale" {
		t.Fatalf("invalidated statuses = %s/%s/%s/%s", anchorPackStatus, videoPackStatus, contextStatus, promptStatus)
	}

	var updatedTransition StoryboardShotTransitionDetail
	doAPISuccess(t, handler, http.MethodPatch, base+"/transition", registration.AccessToken, registration.OrganizationID, map[string]any{
		"expectedRevision": 1,
		"transitionType":   "same_scene_cut",
		"confidence":       0.8,
		"reason":           "manual continuity review",
	}, &updatedTransition)
	if updatedTransition.Revision != 2 || updatedTransition.Status != "active" {
		t.Fatalf("updated transition = %#v", updatedTransition)
	}
	var previousTransitionStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM storyboard_shot_transitions WHERE id = $1`, seed.transitionID).Scan(&previousTransitionStatus); err != nil {
		t.Fatalf("load previous transition: %v", err)
	}
	if previousTransitionStatus != "superseded" {
		t.Fatalf("previous transition status = %s", previousTransitionStatus)
	}
}

type videoProductionDiagnosticSeed struct {
	shotID                string
	episodeID             string
	shotStateHash         string
	transitionID          string
	anchorID              string
	anchorReferencePackID string
	videoReferencePackID  string
	contextPlanID         string
	promptPlanID          string
	renderPlanID          string
}

func seedStoryboardSheetDiagnosticGraph(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	project Project,
	organizationID, userID, shotID string,
) string {
	t.Helper()
	var generationID, stateVersionID, stateHash string
	var plannedDurationTicks, timelineTimebase int64
	var expectedState json.RawMessage
	if err := pool.QueryRow(ctx, `
		SELECT shot.production_generation_id::text, shot.planned_duration_ticks,
		       project.timeline_timebase, state.id::text, state.state, state.state_hash
		FROM storyboard_shots shot
		JOIN projects project ON project.id = shot.project_id
		JOIN storyboard_shot_state_versions state
		  ON state.storyboard_shot_id = shot.id
		 AND state.production_generation_id = shot.production_generation_id
		 AND state.state_role = 'planned_entry' AND state.status = 'approved'
		WHERE shot.id = $1 AND shot.project_id = $2
		ORDER BY state.revision DESC LIMIT 1
	`, shotID, project.ID).Scan(
		&generationID, &plannedDurationTicks, &timelineTimebase,
		&stateVersionID, &expectedState, &stateHash,
	); err != nil {
		t.Fatalf("load storyboard sheet diagnostic state: %v", err)
	}
	var sheetAnchorID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO shot_visual_anchors(
			organization_id, project_id, production_generation_id, storyboard_shot_id,
			shot_state_version_id, anchor_role, revision, status, review_status, metadata
		)
		VALUES ($1, $2, $3, $4, $5, 'storyboard_sheet', 1, 'ready', 'pending', '{"fixture":"storyboard_sheet_diagnostic"}')
		RETURNING id::text
	`, organizationID, project.ID, generationID, shotID, stateVersionID).Scan(&sheetAnchorID); err != nil {
		t.Fatalf("insert storyboard sheet diagnostic anchor: %v", err)
	}
	manifestRaw, err := json.Marshal(map[string]any{
		"contractVersion":  "storyboard-sheet-panel-manifest/v1",
		"storyboardShotId": shotID,
		"panelCount":       3,
		"rows":             3,
		"columns":          1,
	})
	if err != nil {
		t.Fatalf("encode storyboard sheet diagnostic manifest: %v", err)
	}
	manifestHash := strings.Repeat("d", 64)
	var manifestID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO storyboard_sheet_manifests(
			organization_id, project_id, production_generation_id, storyboard_shot_id,
			sheet_anchor_id, revision, contract_version, planned_duration_ticks,
			timeline_timebase, video_aspect_ratio, sheet_aspect_ratio,
			grid_rows, grid_columns, panel_count, entry_state_hash, exit_state_hash,
			manifest, manifest_hash, status, review_status, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, 1, 'storyboard-sheet-panel-manifest/v1', $6,
		        $7, '16:9', '2:3', 3, 1, 3, $8, $8, $9, $10,
		        'processing', 'pending', '{"fixture":"storyboard_sheet_diagnostic"}', $11)
		RETURNING id::text
	`, organizationID, project.ID, generationID, shotID, sheetAnchorID,
		plannedDurationTicks, timelineTimebase, stateHash, manifestRaw, manifestHash, userID).Scan(&manifestID); err != nil {
		t.Fatalf("insert storyboard sheet diagnostic manifest: %v", err)
	}
	for ordinal := 1; ordinal <= 3; ordinal++ {
		var panelAnchorID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO shot_visual_anchors(
				organization_id, project_id, production_generation_id, storyboard_shot_id,
				shot_state_version_id, anchor_role, revision, status, review_status, metadata
			)
			VALUES ($1, $2, $3, $4, $5, 'storyboard_panel', $6, 'draft', 'pending', jsonb_build_object('panelOrdinal', $6::integer))
			RETURNING id::text
		`, organizationID, project.ID, generationID, shotID, stateVersionID, ordinal).Scan(&panelAnchorID); err != nil {
			t.Fatalf("insert storyboard sheet diagnostic panel anchor %d: %v", ordinal, err)
		}
		stage := "middle"
		if ordinal == 1 {
			stage = "entry"
		} else if ordinal == 3 {
			stage = "exit"
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO storyboard_sheet_panels(
				organization_id, project_id, production_generation_id, storyboard_shot_id,
				manifest_id, visual_anchor_id, ordinal, grid_row, grid_column,
				time_tick, normalized_position, stage, action_stage,
				expected_state, expected_state_hash, status, review_status, metadata
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 0,
			        $9, $10, $11, $12, $13, $14, 'planned', 'pending', '{}')
		`, organizationID, project.ID, generationID, shotID, manifestID, panelAnchorID,
			ordinal, ordinal-1, int64(ordinal-1)*plannedDurationTicks/2, (ordinal-1)*500,
			stage, "动作阶段", expectedState, stateHash); err != nil {
			t.Fatalf("insert storyboard sheet diagnostic panel %d: %v", ordinal, err)
		}
	}
	return manifestID
}

func seedVideoProductionDiagnosticGraph(t *testing.T, ctx context.Context, pool *pgxpool.Pool, project Project, organizationID, userID string) videoProductionDiagnosticSeed {
	t.Helper()
	generationID := project.ProductionGeneration.ID
	bindingID := project.VideoProductionBinding.ID
	var profileVersionID, profileSnapshotHash string
	var bindingRevision int64
	var profileSnapshot json.RawMessage
	if err := pool.QueryRow(ctx, `
		SELECT profile_version_id::text, revision, profile_snapshot, profile_snapshot_hash
		FROM project_video_production_bindings WHERE id = $1
	`, bindingID).Scan(&profileVersionID, &bindingRevision, &profileSnapshot, &profileSnapshotHash); err != nil {
		t.Fatalf("load production binding: %v", err)
	}

	var scriptID, versionID, episodeID, timingID, planID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO scripts(organization_id, project_id, title, status, created_by)
		VALUES ($1, $2, 'Diagnostic Script', 'active', $3) RETURNING id::text
	`, organizationID, project.ID, userID).Scan(&scriptID); err != nil {
		t.Fatalf("insert script: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO script_versions(
			script_id, version_no, organization_id, project_id, version,
			content, content_format, source_type, status, created_by
		)
		VALUES ($1, 1, $2, $3, 1, '第一集剧本', 'markdown', 'manual', 'active', $4)
		RETURNING id::text
	`, scriptID, organizationID, project.ID, userID).Scan(&versionID); err != nil {
		t.Fatalf("insert script version: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE scripts SET current_version_id = $2 WHERE id = $1`, scriptID, versionID); err != nil {
		t.Fatalf("activate script version: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO script_episodes(
			organization_id, project_id, script_id, script_version_id,
			episode_index, episode_title, content, content_format,
			review_status, stale_state, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, 1, '第 1 集', '第一集剧本', 'markdown', 'approved', 'fresh', '{}', $5)
		RETURNING id::text
	`, organizationID, project.ID, scriptID, versionID, userID).Scan(&episodeID); err != nil {
		t.Fatalf("insert script episode: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO script_timing_analyses(
			organization_id, project_id, script_id, script_version_id, script_episode_id,
			revision, status, estimated_duration_ticks, minimum_duration_ticks,
			target_duration_ticks, timeline_timebase, fps_numerator, fps_denominator,
			method_version, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, 1, 'ready', 450000, 450000, 450000,
		        90000, 24, 1, 'diagnostic-test', '{}', $6)
		RETURNING id::text
	`, organizationID, project.ID, scriptID, versionID, episodeID, userID).Scan(&timingID); err != nil {
		t.Fatalf("insert timing analysis: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO storyboard_plans(
			organization_id, project_id, script_id, script_version_id, script_episode_id,
			timing_analysis_id, revision, status, target_duration_ticks,
			estimated_shot_count, actual_shot_count, active, stale_state,
			metadata, created_by, activated_at, production_generation_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, 1, 'ready', 450000,
		        1, 1, true, 'fresh', '{}', $7, now(), $8)
		RETURNING id::text
	`, organizationID, project.ID, scriptID, versionID, episodeID, timingID, userID, generationID).Scan(&planID); err != nil {
		t.Fatalf("insert storyboard plan: %v", err)
	}
	var shotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, storyboard_plan_id, script_id,
			script_version_id, script_episode_id, episode_index, episode_shot_index,
			shot_index, shot_no, title, visual, camera, motion, mood,
			image_prompt, video_prompt, image_prompt_status, video_prompt_status,
			status, review_status, start_tick, end_tick, duration_min_ticks,
			duration_max_ticks, production_generation_id, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, 1, 0, 0, 1, '诊断镜头',
		        '角色在场景中转身', '中景平视', '缓慢推进', '紧张',
		        '干净首帧', '原始视频提示词', 'succeeded', 'succeeded',
		        'image_succeeded', 'approved', 0, 450000, 450000, 450000, $7, '{}')
		RETURNING id::text
	`, organizationID, project.ID, planID, scriptID, versionID, episodeID, generationID).Scan(&shotID); err != nil {
		t.Fatalf("insert storyboard shot: %v", err)
	}

	sceneID := uuid.NewString()
	characterID := uuid.NewString()
	entry := diagnosticShotState(sceneID, characterID)
	exit := entry
	exit.Action = videoproduction.ActionState{Entry: "角色开始转身", Exit: "角色完成转身"}
	entryHash, err := videoproduction.HashShotState(entry)
	if err != nil {
		t.Fatalf("hash entry state: %v", err)
	}
	exitHash, err := videoproduction.HashShotState(exit)
	if err != nil {
		t.Fatalf("hash exit state: %v", err)
	}
	var entryStateID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO storyboard_shot_state_versions(
			organization_id, project_id, production_generation_id, storyboard_shot_id,
			state_role, revision, status, state, state_hash, source_type, created_by, approved_at
		)
		VALUES ($1, $2, $3, $4, 'planned_entry', 1, 'approved', $5, $6,
		        'diagnostic_test', $7, now()) RETURNING id::text
	`, organizationID, project.ID, generationID, shotID, mustMarshal(entry), entryHash, userID).Scan(&entryStateID); err != nil {
		t.Fatalf("insert entry state: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO storyboard_shot_state_versions(
			organization_id, project_id, production_generation_id, storyboard_shot_id,
			state_role, revision, status, state, state_hash, source_type, created_by, approved_at
		)
		VALUES ($1, $2, $3, $4, 'planned_exit', 1, 'approved', $5, $6,
		        'diagnostic_test', $7, now())
	`, organizationID, project.ID, generationID, shotID, mustMarshal(exit), exitHash, userID); err != nil {
		t.Fatalf("insert exit state: %v", err)
	}
	transition, err := videoproduction.ClassifyTransition(nil, entry, videoproduction.TransitionSuggestion{})
	if err != nil {
		t.Fatalf("classify transition: %v", err)
	}
	transitionHash, err := videoproduction.HashTransition(transition)
	if err != nil {
		t.Fatalf("hash transition: %v", err)
	}
	var transitionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO storyboard_shot_transitions(
			organization_id, project_id, production_generation_id, storyboard_plan_id,
			target_shot_id, transition_type, tail_policy, anchor_policy,
			carry_constraints, reset_constraints, confidence, revision, status,
			review_status, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 1,
		        'active', 'approved', jsonb_build_object('transitionHash', $12::text))
		RETURNING id::text
	`, organizationID, project.ID, generationID, planID, shotID,
		transition.TransitionType, transition.TailPolicy, transition.AnchorPolicy,
		mustMarshal(transition.Carry), mustMarshal(transition.Reset), transition.Confidence,
		transitionHash).Scan(&transitionID); err != nil {
		t.Fatalf("insert transition: %v", err)
	}

	var anchorID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO shot_visual_anchors(
			organization_id, project_id, production_generation_id, storyboard_shot_id,
			shot_state_version_id, anchor_role, revision, status, review_status,
			prompt, prompt_hash, metadata
		)
		VALUES ($1, $2, $3, $4, $5, 'planned_first_frame', 1, 'ready', 'approved',
		        '干净首帧', $6, '{}') RETURNING id::text
	`, organizationID, project.ID, generationID, shotID, entryStateID, strings.Repeat("a", 64)).Scan(&anchorID); err != nil {
		t.Fatalf("insert visual anchor: %v", err)
	}
	capabilitySnapshotHash := strings.Repeat("b", 64)
	anchorPackHash := strings.Repeat("c", 64)
	videoPackHash := strings.Repeat("4", 64)
	var anchorReferencePackID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO shot_reference_packs(
			organization_id, project_id, production_generation_id, storyboard_shot_id,
			profile_snapshot_hash, shot_state_hash, capability_snapshot_hash,
			purpose, manifest, manifest_hash, status
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'anchor', $8, $9, 'active')
		RETURNING id::text
	`, organizationID, project.ID, generationID, shotID, profileSnapshotHash, entryHash,
		capabilitySnapshotHash, mustMarshal(map[string]any{"profileKey": videoproduction.ProfileSingleFrameI2V, "purpose": "anchor"}),
		anchorPackHash).Scan(&anchorReferencePackID); err != nil {
		t.Fatalf("insert anchor reference pack: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO shot_reference_pack_items(
			reference_pack_id, reference_key, role, required, priority,
			source_type, source_id, media_type, semantics, content_hash, metadata
		)
		VALUES ($1, $2, 'first_frame', true, 1000, 'visual_anchor', $3,
		        'image', 'output_start_frame', $4, '{}')
	`, anchorReferencePackID, "planned_first_frame:"+anchorID, anchorID, strings.Repeat("d", 64)); err != nil {
		t.Fatalf("insert anchor reference item: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE shot_visual_anchors SET reference_pack_id = $2 WHERE id = $1`, anchorID, anchorReferencePackID); err != nil {
		t.Fatalf("bind anchor reference pack: %v", err)
	}
	var videoReferencePackID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO shot_reference_packs(
			organization_id, project_id, production_generation_id, storyboard_shot_id,
			profile_snapshot_hash, shot_state_hash, capability_snapshot_hash,
			purpose, manifest, manifest_hash, status
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'video', $8, $9, 'active')
		RETURNING id::text
	`, organizationID, project.ID, generationID, shotID, profileSnapshotHash, entryHash,
		capabilitySnapshotHash, mustMarshal(map[string]any{"profileKey": videoproduction.ProfileSingleFrameI2V, "purpose": "video"}),
		videoPackHash).Scan(&videoReferencePackID); err != nil {
		t.Fatalf("insert video reference pack: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO shot_reference_pack_items(
			reference_pack_id, reference_key, role, required, priority,
			source_type, source_id, media_type, semantics, content_hash, metadata
		)
		VALUES ($1, $2, 'first_frame', true, 1000, 'visual_anchor', $3,
		        'image', 'output_start_frame', $4, '{}')
	`, videoReferencePackID, "planned_first_frame:"+anchorID, anchorID, strings.Repeat("d", 64)); err != nil {
		t.Fatalf("insert video reference item: %v", err)
	}

	var contextPlanID string
	contextHash := strings.Repeat("e", 64)
	if err := pool.QueryRow(ctx, `
		INSERT INTO prompt_context_plans(
			organization_id, project_id, production_generation_id,
			video_production_binding_id, video_production_binding_revision,
			storyboard_plan_id, storyboard_shot_id, script_episode_id,
			revision, status, episode_continuity_digest, current_scene_script,
			adjacent_scene_summaries, current_shot_state, verbatim_dialogue_cues,
			model_context_limit, model_prompt_limit, budget_allocation,
			source_hashes, plan_hash, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, 'active',
		        '角色保持同一服装与方向', '角色在场景中转身', '[]', $9, '[]',
		        32768, 8192, '{"scene":2048,"shot":2048}',
		        jsonb_build_object('shotStateHash', $10::text), $11, $12)
		RETURNING id::text
	`, organizationID, project.ID, generationID, bindingID, bindingRevision, planID,
		shotID, episodeID, mustMarshal(entry), entryHash, contextHash, userID).Scan(&contextPlanID); err != nil {
		t.Fatalf("insert prompt context: %v", err)
	}
	var promptVersionID string
	if err := pool.QueryRow(ctx, `
		SELECT id::text FROM prompt_versions
		WHERE status = 'active' ORDER BY created_at LIMIT 1
	`).Scan(&promptVersionID); err != nil {
		t.Fatalf("load prompt version: %v", err)
	}
	var promptPlanID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO video_prompt_plans(
			organization_id, project_id, production_generation_id,
			video_production_binding_id, video_production_binding_revision,
			profile_version_id, storyboard_shot_id, prompt_context_plan_id,
			prompt_version_id, revision, status, rendered_prompt, prompt_hash,
			prompt_context_plan_hash, profile_snapshot_hash, shot_state_hash,
			transition_hash, reference_pack_hash, capability_snapshot_hash,
			input_contract_version, dialogue_cues, native_audio_required,
			audio_strategy, audio_requirement, reviewer_output, metadata,
			created_by, reviewed_at, approved_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, 'approved',
		        '原始视频提示词', $10, $11, $12, $13, $14, $15, $16,
		        'single_frame_i2v.v1', '[]', false, 'native_av', 'preferred',
		        '{"approved":true}', '{}', $17, now(), now())
		RETURNING id::text
	`, organizationID, project.ID, generationID, bindingID, bindingRevision, profileVersionID,
		shotID, contextPlanID, promptVersionID, strings.Repeat("f", 64), contextHash,
		profileSnapshotHash, entryHash, transitionHash, videoPackHash,
		capabilitySnapshotHash, userID).Scan(&promptPlanID); err != nil {
		t.Fatalf("insert video prompt plan: %v", err)
	}

	var connectorID, providerAccountID, providerModelID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM provider_connectors ORDER BY created_at LIMIT 1`).Scan(&connectorID); err != nil {
		t.Fatalf("load provider connector: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_accounts(organization_id, connector_id, name, base_url, auth_type, status, config, created_by)
		VALUES ($1, $2, 'Diagnostic Provider', 'https://example.test/v1', 'bearer', 'active', '{}', $3)
		RETURNING id::text
	`, organizationID, connectorID, userID).Scan(&providerAccountID); err != nil {
		t.Fatalf("insert provider account: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status)
		VALUES ($1, 'diagnostic-video', 'Diagnostic Video', 'video', 'active') RETURNING id::text
	`, providerAccountID).Scan(&providerModelID); err != nil {
		t.Fatalf("insert provider model: %v", err)
	}
	capabilitySnapshot := mustMarshal(map[string]any{"variantKey": "diagnostic", "requestMode": "async", "referenceMode": "first_frame"})
	var renderPlanID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO video_render_plans(
			organization_id, project_id, storyboard_plan_id, storyboard_shot_id,
			provider_account_id, provider_model_id, model_family, variant_key,
			capability_snapshot, capability_snapshot_hash, plan_key, status, active,
			target_duration_ticks, timeline_timebase, fps_numerator, fps_denominator,
			task_type, reference_mode, aspect_ratio, resolution,
			audio_strategy, audio_requirement, native_audio_status,
			production_readiness, expires_at, production_generation_id,
			video_production_binding_id, video_production_binding_revision,
			profile_version_id, production_profile_snapshot,
			production_profile_snapshot_hash, shot_state_revision, shot_state_hash,
			transition_snapshot, transition_hash, reference_pack_id,
			reference_pack_hash, initial_input_contract_snapshot,
			initial_input_contract_hash, prompt_context_plan_id,
			prompt_context_plan_hash, video_prompt_plan_id, dialogue_cues,
			native_audio_required, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, 'diagnostic-video', 'diagnostic',
		        $7, $8, $9, 'planned', true, 450000, 90000, 24, 1,
		        'video.image_to_video', 'first_frame', '16:9', '720p',
		        'native_av', 'preferred', 'not_requested', 'preview_only',
		        now() + interval '1 hour', $10, $11, $12, $13, $14, $15,
		        1, $16, $17, $18, $19, $20, '{"contractKey":"first_frame"}',
		        $21, $22, $23, $24, '[]', false, '{}')
		RETURNING id::text
	`, organizationID, project.ID, planID, shotID, providerAccountID, providerModelID,
		capabilitySnapshot, capabilitySnapshotHash, "diagnostic-"+uuid.NewString(),
		generationID, bindingID, bindingRevision, profileVersionID, profileSnapshot,
		profileSnapshotHash, entryHash, mustMarshal(transition), transitionHash,
		videoReferencePackID, videoPackHash, strings.Repeat("1", 64),
		contextPlanID, contextHash, promptPlanID).Scan(&renderPlanID); err != nil {
		t.Fatalf("insert render plan: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE storyboard_shots SET active_video_render_plan_id = $2 WHERE id = $1`, shotID, renderPlanID); err != nil {
		t.Fatalf("bind render plan: %v", err)
	}
	return videoProductionDiagnosticSeed{
		shotID: shotID, episodeID: episodeID, shotStateHash: entryHash,
		transitionID: transitionID, anchorID: anchorID,
		anchorReferencePackID: anchorReferencePackID, videoReferencePackID: videoReferencePackID,
		contextPlanID: contextPlanID, promptPlanID: promptPlanID, renderPlanID: renderPlanID,
	}
}

func seedWorkflowVideoProductionActivity(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	project Project,
	organizationID string,
	userID string,
	seed videoProductionDiagnosticSeed,
) string {
	t.Helper()
	var profileVersionID, profileSnapshotHash string
	var bindingRevision int64
	if err := pool.QueryRow(ctx, `
		SELECT profile_version_id::text, revision, profile_snapshot_hash
		FROM project_video_production_bindings WHERE id = $1
	`, project.VideoProductionBinding.ID).Scan(&profileVersionID, &bindingRevision, &profileSnapshotHash); err != nil {
		t.Fatalf("load activity binding: %v", err)
	}
	var workflowRunID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workflow_runs(
			organization_id, project_id, temporal_workflow_id, workflow_type, status,
			input, output, created_by, total_items, completed_items, failed_items,
			production_generation_id, video_production_binding_id,
			video_production_binding_revision, completed_at, terminalized_at, settled_at
		)
		VALUES ($1, $2, $3, 'batch_generate_shot_videos', 'partial_succeeded',
		        jsonb_build_object('scriptEpisodeId', $4::text), '{}', $5, 1, 0, 1,
		        $6, $7, $8, now(), now(), now())
		RETURNING id::text
	`, organizationID, project.ID, "diagnostic-video-activity-"+uuid.NewString(), seed.episodeID,
		userID, project.ProductionGeneration.ID, project.VideoProductionBinding.ID, bindingRevision).Scan(&workflowRunID); err != nil {
		t.Fatalf("insert activity workflow run: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_runs SET root_workflow_run_id = id WHERE id = $1`, workflowRunID); err != nil {
		t.Fatalf("set activity root workflow: %v", err)
	}
	var checkpointID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO episode_video_production_checkpoints(
			organization_id, project_id, production_generation_id,
			video_production_binding_id, video_production_binding_revision,
			script_episode_id, profile_version_id, profile_snapshot_hash,
			workflow_run_id, temporal_workflow_id, status, next_batch_ordinal,
			metadata, completed_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
		        'partial_succeeded', 1, '{}', now())
		RETURNING id::text
	`, organizationID, project.ID, project.ProductionGeneration.ID,
		project.VideoProductionBinding.ID, bindingRevision, seed.episodeID,
		profileVersionID, profileSnapshotHash, workflowRunID,
		"diagnostic-checkpoint-"+uuid.NewString()).Scan(&checkpointID); err != nil {
		t.Fatalf("insert activity checkpoint: %v", err)
	}
	var batchID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO episode_video_production_batches(
			checkpoint_id, ordinal, dependency_snapshot_hash, workflow_run_id,
			temporal_workflow_id, status, attempt, total_items,
			succeeded_items, failed_items, metadata, started_at, completed_at
		)
		VALUES ($1, 0, $2, $3, $4, 'partial_succeeded', 1, 1, 0, 1,
		        '{}', now(), now())
		RETURNING id::text
	`, checkpointID, strings.Repeat("9", 64), workflowRunID,
		"diagnostic-batch-"+uuid.NewString()).Scan(&batchID); err != nil {
		t.Fatalf("insert activity batch: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO episode_video_production_items(
			batch_id, storyboard_shot_id, shot_state_hash, reference_pack_id,
			video_prompt_plan_id, video_render_plan_id, status, attempt,
			error_code, error_detail, metadata, started_at, completed_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, 'failed', 1, 'UPSTREAM_TIMEOUT',
		        '{"message":"上游视频任务超时"}', '{}', now(), now())
	`, batchID, seed.shotID, seed.shotStateHash, seed.videoReferencePackID,
		seed.promptPlanID, seed.renderPlanID); err != nil {
		t.Fatalf("insert activity item: %v", err)
	}
	return workflowRunID
}

func assertWorkflowVideoProductionMediaLifecycle(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	handler http.Handler,
	accessToken string,
	organizationID string,
	userID string,
	project Project,
	workflowRunID string,
	seed videoProductionDiagnosticSeed,
) {
	t.Helper()
	storageKey := "tests/video-production-activity/" + uuid.NewString() + ".mp4"
	var artifactID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO artifacts(
			organization_id, project_id, workflow_run_id, type, storage_key,
			mime_type, content_hash, metadata, created_by
		)
		VALUES ($1, $2, $3, 'generated_video', $4, 'video/mp4', $5,
		        '{"fixture":"workflow_video_production_activity"}', $6)
		RETURNING id::text
	`, organizationID, project.ID, workflowRunID, storageKey, strings.Repeat("8", 64), userID).Scan(&artifactID); err != nil {
		t.Fatalf("insert activity artifact: %v", err)
	}
	var mediaFileID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_files(
			organization_id, project_id, artifact_id, storage_key, mime_type,
			byte_size, duration_seconds, checksum, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, 'video/mp4', 1024, 5, $5,
		        '{"fixture":"workflow_video_production_activity"}', $6)
		RETURNING id::text
	`, organizationID, project.ID, artifactID, storageKey, strings.Repeat("8", 64), userID).Scan(&mediaFileID); err != nil {
		t.Fatalf("insert activity media file: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE video_render_plans
		SET status = 'succeeded', output_artifact_id = $2, output_media_file_id = $3,
		    output_storage_key = $4, completed_at = now(), updated_at = now()
		WHERE id = $1
	`, seed.renderPlanID, artifactID, mediaFileID, storageKey); err != nil {
		t.Fatalf("complete historical activity render plan: %v", err)
	}

	currentRenderPlanID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		INSERT INTO video_render_plans
		SELECT (jsonb_populate_record(
			NULL::video_render_plans,
			to_jsonb(source) || jsonb_build_object(
				'id', $2::text,
				'workflow_run_id', $3::text,
				'plan_key', $4::text,
				'status', 'running',
				'active', false,
				'output_artifact_id', NULL,
				'output_media_file_id', NULL,
				'output_storage_key', NULL,
				'completed_at', NULL,
				'created_at', now() + interval '1 second',
				'updated_at', now()
			)
		)).*
		FROM video_render_plans source
		WHERE source.id = $1
	`, seed.renderPlanID, currentRenderPlanID, workflowRunID, "activity-current-"+uuid.NewString()); err != nil {
		t.Fatalf("insert current activity render plan: %v", err)
	}
	var providerCallID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_call_logs(
			organization_id, project_id, production_generation_id, workflow_run_id,
			provider_account_id, provider_model_id, task_type, execution_mode, status,
			request_snapshot, response_snapshot, normalized_output, artifact_ids, media_file_ids
		)
		SELECT plan.organization_id, plan.project_id, plan.production_generation_id, $2,
		       plan.provider_account_id, plan.provider_model_id, 'video.create_task', 'async_create', 'running',
		       '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '[]'::jsonb, '[]'::jsonb
		FROM video_render_plans plan WHERE plan.id = $1
		RETURNING id::text
	`, currentRenderPlanID, workflowRunID).Scan(&providerCallID); err != nil {
		t.Fatalf("insert activity provider call: %v", err)
	}
	segmentTaskIDs := make([]string, 0, 2)
	for segmentIndex := 0; segmentIndex < 2; segmentIndex++ {
		var segmentID string
		startTick := int64(segmentIndex) * 450000
		if err := pool.QueryRow(ctx, `
			INSERT INTO video_render_segments(
				organization_id, project_id, production_generation_id, video_render_plan_id,
				storyboard_shot_id, segment_index, planned_start_tick, planned_end_tick,
				requested_duration_seconds, continuity_mode, status, input_contract_key,
				input_contract_hash, source_video_prompt_plan_id, source_prompt_hash,
				production_readiness, prompt, execution_prompt_hash
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7::bigint, $7::bigint + 450000,
			        5, 'single_frame', $8, 'first_frame', $9, $10, $11,
			        'blocked', $12, $13)
			RETURNING id::text
		`, organizationID, project.ID, project.ProductionGeneration.ID, currentRenderPlanID,
			seed.shotID, segmentIndex, startTick,
			map[bool]string{true: "succeeded", false: "running"}[segmentIndex == 0],
			strings.Repeat("a", 64), seed.promptPlanID, strings.Repeat("b", 64),
			fmt.Sprintf("activity segment %d", segmentIndex+1), "sha256:"+strings.Repeat(fmt.Sprintf("%x", segmentIndex+3), 64)[:64]).Scan(&segmentID); err != nil {
			t.Fatalf("insert activity render segment %d: %v", segmentIndex, err)
		}
		var taskID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO provider_async_tasks(
				provider_call_id, organization_id, project_id, workflow_run_id,
				provider_account_id, provider_model_id, external_task_id, status, poll_count,
				production_generation_id, video_production_binding_id, video_production_binding_revision,
				video_render_plan_id, video_render_segment_id, request_hash, completed_at
			)
			SELECT $1, plan.organization_id, plan.project_id, $2,
			       plan.provider_account_id, plan.provider_model_id, $3, $4, $5,
			       plan.production_generation_id, plan.video_production_binding_id, plan.video_production_binding_revision,
			       plan.id, $6, $7, CASE WHEN $4 = 'succeeded' THEN now() ELSE NULL END
			FROM video_render_plans plan WHERE plan.id = $8
			RETURNING id::text
		`, providerCallID, workflowRunID, fmt.Sprintf("activity-task-%d", segmentIndex),
			map[bool]string{true: "succeeded", false: "running"}[segmentIndex == 0], segmentIndex+1,
			segmentID, strings.Repeat(fmt.Sprintf("%x", segmentIndex+1), 64)[:64], currentRenderPlanID).Scan(&taskID); err != nil {
			t.Fatalf("insert activity provider task %d: %v", segmentIndex, err)
		}
		if _, err := pool.Exec(ctx, `
			UPDATE video_render_segments
			SET provider_async_task_id = $2, status = $3, updated_at = now()
			WHERE id = $1
		`, segmentID, taskID, map[bool]string{true: "succeeded", false: "running"}[segmentIndex == 0]); err != nil {
			t.Fatalf("bind activity provider task %d: %v", segmentIndex, err)
		}
		segmentTaskIDs = append(segmentTaskIDs, taskID)
	}
	execForWorkflow := func(label string, statements ...string) {
		t.Helper()
		for _, statement := range statements {
			if _, err := pool.Exec(ctx, statement, workflowRunID); err != nil {
				t.Fatalf("%s: %v", label, err)
			}
		}
	}
	execForWorkflow("mark activity workflow running",
		`UPDATE workflow_runs
		 SET status = 'running', completed_items = 0, failed_items = 0,
		     completed_at = NULL, terminalized_at = NULL, settled_at = NULL
		 WHERE id = $1`,
		`UPDATE episode_video_production_checkpoints
		 SET status = 'running', completed_at = NULL WHERE workflow_run_id = $1`,
		`UPDATE episode_video_production_batches
		 SET status = 'running', succeeded_items = 0, failed_items = 0,
		     cancelled_items = 0, completed_at = NULL WHERE workflow_run_id = $1`,
		`UPDATE episode_video_production_items item
		 SET status = 'running', error_code = NULL, error_detail = '{}', completed_at = NULL
		 FROM episode_video_production_batches batch
		 WHERE batch.id = item.batch_id AND batch.workflow_run_id = $1`,
	)

	loadItem := func() EpisodeVideoProductionItem {
		var response WorkflowVideoProductionActivity
		doAPISuccess(t, handler, http.MethodGet, "/api/workflow-runs/"+workflowRunID+"/video-production", accessToken, organizationID, nil, &response)
		if response.TotalItems != 1 || len(response.Checkpoints) != 1 ||
			len(response.Checkpoints[0].Batches) != 1 || len(response.Checkpoints[0].Batches[0].Items) != 1 {
			t.Fatalf("activity lifecycle response = %#v", response)
		}
		return response.Checkpoints[0].Batches[0].Items[0]
	}

	running := loadItem()
	if running.VideoRenderPlanID == nil || *running.VideoRenderPlanID != currentRenderPlanID ||
		running.VideoRenderPlanStatus == nil || *running.VideoRenderPlanStatus != "running" ||
		running.MediaStatus != "pending" {
		t.Fatalf("running activity media projection = %#v", running)
	}
	if len(running.Segments) != 2 || len(running.Segments[0].ProviderTasks) != 1 || len(running.Segments[1].ProviderTasks) != 1 ||
		running.Segments[0].ProviderTasks[0].ID != segmentTaskIDs[0] || running.Segments[1].ProviderTasks[0].ID != segmentTaskIDs[1] {
		t.Fatalf("multi-segment provider task projection = %#v", running.Segments)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE video_render_plans
		SET status = 'succeeded', output_artifact_id = $2, output_media_file_id = $3,
		    output_storage_key = $4, completed_at = now(), updated_at = now()
		WHERE id = $1
	`, currentRenderPlanID, artifactID, mediaFileID, storageKey); err != nil {
		t.Fatalf("store current activity render output: %v", err)
	}
	beforeCommit := loadItem()
	if beforeCommit.MediaStatus == "stored" {
		t.Fatalf("activity media was marked stored before item completion: %#v", beforeCommit)
	}

	execForWorkflow("complete activity workflow",
		`UPDATE episode_video_production_items item
		 SET status = 'succeeded', completed_at = now()
		 FROM episode_video_production_batches batch
		 WHERE batch.id = item.batch_id AND batch.workflow_run_id = $1`,
		`UPDATE episode_video_production_batches
		 SET status = 'succeeded', succeeded_items = 1, completed_at = now()
		 WHERE workflow_run_id = $1`,
		`UPDATE episode_video_production_checkpoints
		 SET status = 'succeeded', completed_at = now() WHERE workflow_run_id = $1`,
		`UPDATE workflow_runs
		 SET status = 'succeeded', completed_items = 1, completed_at = now(),
		     terminalized_at = now(), settled_at = now()
		 WHERE id = $1`,
	)
	completed := loadItem()
	if completed.MediaStatus != "stored" {
		t.Fatalf("completed activity media projection = %#v", completed)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM video_render_plans WHERE id = $1`, currentRenderPlanID); err != nil {
		t.Fatalf("delete current activity render plan fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE video_render_plans
		SET status = 'planned', output_artifact_id = NULL, output_media_file_id = NULL,
		    output_storage_key = NULL, completed_at = NULL, updated_at = now()
		WHERE id = $1
	`, seed.renderPlanID); err != nil {
		t.Fatalf("restore historical activity render plan: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM media_files WHERE id = $1`, mediaFileID); err != nil {
		t.Fatalf("delete activity media fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM artifacts WHERE id = $1`, artifactID); err != nil {
		t.Fatalf("delete activity artifact fixture: %v", err)
	}
}

func diagnosticShotState(sceneID, characterID string) videoproduction.ShotState {
	return videoproduction.ShotState{
		Scene: videoproduction.SceneState{
			AssetID: sceneID, TimeOfDay: "day", Weather: "clear", Lighting: "soft daylight",
		},
		Characters: []videoproduction.CharacterState{{
			AssetID: characterID, Pose: "standing", Expression: "focused",
			Blocking: videoproduction.BlockingState{Horizontal: "center", Depth: "midground", Facing: "screen_right"},
		}},
		Camera: videoproduction.CameraState{
			ShotSize: "medium", Angle: "eye_level", AxisSide: "A", LensIntent: "normal", Movement: "dolly_in",
		},
		Action:          videoproduction.ActionState{Entry: "角色开始转身", Exit: "角色完成转身"},
		ScreenDirection: "left_to_right",
	}
}
