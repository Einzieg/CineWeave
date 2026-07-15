package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Einzieg/cineweave/internal/provider"
	storyboardpkg "github.com/Einzieg/cineweave/internal/storyboard"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStoryboardEpisodeV2ActivitiesPersistCompletePlan(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	orgID, userID, projectID, workflowRunID, textModelID, _ := seedWorkflowGatewayIntegrationData(t, ctx, pool)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID) })

	scriptID, versionID, episodeID, sceneIDs := seedStoryboardEpisodeV2Script(t, ctx, pool, orgID, projectID, userID)
	assetID := seedStoryboardEpisodeV2Asset(t, ctx, pool, orgID, projectID, userID)
	callIDs := seedStoryboardEpisodeV2ProviderCalls(t, ctx, pool, orgID, projectID, workflowRunID, textModelID, 5)

	var timingOutput TimingAnalysisActivityOutput
	gateway := httptest.NewServer(mockStoryboardEpisodeV2Gateway(t, textModelID, callIDs, assetID, sceneIDs, &timingOutput))
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

	assertStoryboardEpisodeV2Persistence(t, ctx, pool, projectID, workflowRunID, episodeID, blueprint.StoryboardPlanID)
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

func seedStoryboardEpisodeV2Asset(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, projectID, userID string) string {
	t.Helper()
	var assetID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO canonical_assets(
			organization_id, project_id, asset_type, name, description, status,
			base_prompt, consistency_prompt, created_by
		)
		VALUES ($1, $2, 'character', '方源', '雨中庭院里的方源', 'prompt_ready',
		        '方源角色四视图', '保持服装和面部一致', $3)
		RETURNING id::text
	`, orgID, projectID, userID).Scan(&assetID); err != nil {
		t.Fatalf("insert canonical asset: %v", err)
	}
	return assetID
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

func mockStoryboardEpisodeV2Gateway(t *testing.T, modelID string, callIDs []string, assetID string, sceneIDs []string, timing *TimingAnalysisActivityOutput) http.Handler {
	t.Helper()
	callIndex := 0
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			responseText = fmt.Sprintf(`{"sceneKey":"%s","shots":[{"suggestionKey":"scene-%d-shot-1","timingUnitIds":[%s],"cutReason":"完整动作节拍","title":"雨中对话","visual":"人物在雨中庭院完成动作","camera":"中景","motion":"缓慢推进","mood":"克制","oneTake":false,"continuityGroupKey":"rain-dialogue","imagePromptDirection":"雨中庭院，中景人物，保持角色外观一致","videoPromptDirection":"%s","assetRequirements":[{"assetId":"%s","requirementType":"character_appearance","roleInShot":"主体角色","pose":"站立"}]}]}`, timing.Scenes[sceneIndex].SceneKey, sceneIndex+1, strings.Join(unitIDs, ","), videoDirection, assetID)
		case promptKeyStoryboardPlanReviewer:
			responseText = `{"approved":true,"issues":[],"corrections":[]}`
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
