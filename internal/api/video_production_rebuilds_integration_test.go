package api

import (
	"bytes"
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
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	temporalclient "go.temporal.io/sdk/client"
)

func TestProjectVideoProductionRebuildSupportsImpactSwitchPartialAndFailedItemRetry(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run video production rebuild API tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for video production rebuild API tests")
	}
	t.Setenv(videoproduction.FeatureFlagEnvironmentVariable, "true")

	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	require.NoError(t, err)
	temporalClient, err := temporalclient.Dial(temporalclient.Options{
		HostPort:  integrationTestEnvironment("TEMPORAL_ADDRESS", "temporal:7233"),
		Namespace: integrationTestEnvironment("TEMPORAL_NAMESPACE", "default"),
	})
	require.NoError(t, err)
	authService := auth.NewService(pool, "video-production-rebuild-test-secret", time.Hour, 24*time.Hour)
	handler := New(pool, authService, nil, nil, temporalClient).Handler()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	registration, err := authService.Register(ctx, auth.RegisterRequest{
		Email:            "video-rebuild-" + suffix + "@example.test",
		Username:         randomStorageSegment(),
		Password:         "Password123!",
		DisplayName:      "Video Rebuild Test",
		OrganizationName: "Video Rebuild Org " + suffix,
		WorkspaceName:    "Video Rebuild Workspace",
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	require.NoError(t, err)

	seededTargetProfileKey, targetProfileID := seedVideoProductionRebuildTargetProfile(t, ctx, pool, suffix)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, registration.OrganizationID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM video_production_profile_versions WHERE id = $1`, targetProfileID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM video_production_profiles WHERE profile_key = $1`, seededTargetProfileKey)
		pool.Close()
		temporalClient.Close()
	})
	seedVideoProductionRebuildModelBinding(t, ctx, pool, registration.OrganizationID, registration.User.ID)

	var project Project
	doAPISuccess(t, handler, http.MethodPost, "/api/projects", registration.AccessToken, registration.OrganizationID, map[string]any{
		"workspaceId":                   registration.WorkspaceID,
		"name":                          "Video Rebuild Project " + suffix,
		"videoProductionProfileKey":     videoproduction.ProfileSingleFrameI2V,
		"videoProductionProfileVersion": 1,
		"compatibilityPolicy":           videoproduction.CompatibilityStrict,
	}, &project)
	require.NotNil(t, project.VideoProductionBinding)
	require.NotNil(t, project.ProductionGeneration)

	seed := seedVideoProductionRebuildProjectData(t, ctx, pool, registration, project)
	targetProfileKey := videoproduction.ProfileSingleFrameI2V
	targetProfileVersion := project.VideoProductionBinding.ProfileVersion
	targetConfiguration := videoProductionRebuildConfiguration(project, "4:3")
	_, err = pool.Exec(ctx, `UPDATE scripts SET status = 'archived' WHERE id = (SELECT script_id FROM script_episodes WHERE id = $1)`, seed.FirstEpisodeID)
	require.NoError(t, err)
	archivedScriptImpact := doVideoProductionRebuildRequest(t, handler, http.MethodPost,
		fmt.Sprintf("/api/projects/%s/video-production/rebuild-impact", project.ID),
		registration.AccessToken, registration.OrganizationID, "", map[string]any{
			"targetProfileKey":     targetProfileKey,
			"targetProfileVersion": targetProfileVersion,
			"targetConfiguration":  targetConfiguration,
		})
	assertVideoProductionAPIError(t, archivedScriptImpact, http.StatusConflict, videoproduction.CodeRebuildConflict)
	_, err = pool.Exec(ctx, `UPDATE scripts SET status = 'active' WHERE id = (SELECT script_id FROM script_episodes WHERE id = $1)`, seed.FirstEpisodeID)
	require.NoError(t, err)
	var impactResponse struct {
		Impact        videoproduction.RebuildImpact `json:"impact"`
		Compatibility struct {
			Compatible bool `json:"compatible"`
		} `json:"compatibility"`
	}
	doAPISuccess(t, handler, http.MethodPost,
		fmt.Sprintf("/api/projects/%s/video-production/rebuild-impact", project.ID),
		registration.AccessToken, registration.OrganizationID, map[string]any{
			"targetProfileKey":     targetProfileKey,
			"targetProfileVersion": targetProfileVersion,
			"targetConfiguration":  targetConfiguration,
		}, &impactResponse)
	require.True(t, impactResponse.Compatibility.Compatible)
	require.Equal(t, 2, impactResponse.Impact.Counts.Episodes)
	require.Equal(t, 1, impactResponse.Impact.Counts.StoryboardPlans)
	require.Equal(t, 1, impactResponse.Impact.Counts.StoryboardShots)
	require.Equal(t, 1, impactResponse.Impact.Counts.RetainedAssets)
	require.Equal(t, "configuration_change", impactResponse.Impact.Reason)
	require.Equal(t, "4:3", impactResponse.Impact.TargetConfiguration.VideoRatio)
	require.Len(t, impactResponse.Impact.TargetConfigurationHash, 64)
	require.Len(t, impactResponse.Impact.ImpactToken, 64)

	stale := doVideoProductionRebuildRequest(t, handler, http.MethodPost,
		fmt.Sprintf("/api/projects/%s/video-production/rebuilds", project.ID),
		registration.AccessToken, registration.OrganizationID, "stale-"+suffix, map[string]any{
			"expectedProjectRevision": impactResponse.Impact.ExpectedProjectRevision,
			"targetProfileKey":        targetProfileKey,
			"targetProfileVersion":    targetProfileVersion,
			"targetConfiguration":     targetConfiguration,
			"impactToken":             strings.Repeat("0", 64),
		})
	assertVideoProductionAPIError(t, stale, http.StatusConflict, videoproduction.CodeRebuildImpactStale)

	created := doVideoProductionRebuildRequest(t, handler, http.MethodPost,
		fmt.Sprintf("/api/projects/%s/video-production/rebuilds", project.ID),
		registration.AccessToken, registration.OrganizationID, "rebuild-"+suffix, map[string]any{
			"expectedProjectRevision": impactResponse.Impact.ExpectedProjectRevision,
			"targetProfileKey":        targetProfileKey,
			"targetProfileVersion":    targetProfileVersion,
			"targetConfiguration":     targetConfiguration,
			"impactToken":             impactResponse.Impact.ImpactToken,
		})
	require.Equal(t, http.StatusAccepted, created.Code, created.Body.String())
	rebuild := decodeVideoProductionRebuildData(t, created)
	require.NotNil(t, rebuild.WorkflowRunID)
	require.Equal(t, "approved", rebuild.Status)
	require.Equal(t, impactResponse.Impact.TargetConfigurationHash, rebuild.TargetConfigurationHash)
	currentResponse := doVideoProductionRebuildRequest(t, handler, http.MethodGet,
		fmt.Sprintf("/api/projects/%s/video-production/rebuilds/current", project.ID),
		registration.AccessToken, registration.OrganizationID, "", nil)
	require.Equal(t, http.StatusOK, currentResponse.Code, currentResponse.Body.String())
	currentRebuild := decodeNullableVideoProductionRebuildData(t, currentResponse)
	require.NotNil(t, currentRebuild)
	require.Equal(t, rebuild.ID, currentRebuild.ID)
	var ratioBeforeSwitch string
	require.NoError(t, pool.QueryRow(ctx, `SELECT video_ratio FROM projects WHERE id = $1`, project.ID).Scan(&ratioBeforeSwitch))
	require.NotEqual(t, "4:3", ratioBeforeSwitch)

	activities := workflows.NewActivities(pool, nil, nil)
	workflowInput := workflows.ProjectVideoProductionRebuildInput{
		OrganizationID: registration.OrganizationID,
		ProjectID:      project.ID,
		WorkflowRunID:  *rebuild.WorkflowRunID,
		RebuildID:      rebuild.ID,
		RequestedBy:    registration.User.ID,
	}
	prepared, err := activities.PrepareProjectVideoProductionRebuild(ctx, workflowInput)
	require.NoError(t, err)
	require.False(t, prepared.GenerationSwitched)
	drain, err := activities.CheckProjectVideoProductionDrain(ctx, workflowInput)
	require.NoError(t, err)
	require.True(t, drain.Drained)
	switched, err := activities.SwitchProjectVideoProductionGeneration(ctx, workflowInput)
	require.NoError(t, err)
	require.Equal(t, int64(2), switched.Generation.GenerationNo)
	require.Equal(t, int64(2), switched.Binding.Revision)
	require.Equal(t, videoproduction.ProfileSingleFrameI2V, switched.Binding.ProfileKey)
	activeConfiguration, err := videoproduction.DecodeProductionConfiguration(switched.Binding.ProfileSnapshot)
	require.NoError(t, err)
	require.Equal(t, "4:3", activeConfiguration.VideoRatio)
	var ratioAfterSwitch string
	require.NoError(t, pool.QueryRow(ctx, `SELECT video_ratio FROM projects WHERE id = $1`, project.ID).Scan(&ratioAfterSwitch))
	require.Equal(t, "4:3", ratioAfterSwitch)

	lockedManualResponse := doAPIRequest(t, handler, http.MethodPost,
		fmt.Sprintf("/api/projects/%s/storyboard-shots", project.ID),
		registration.AccessToken, registration.OrganizationID, map[string]any{
			"visual": "must not be inserted while the production generation is rebuilding",
		})
	assertVideoProductionAPIError(t, lockedManualResponse, http.StatusConflict, videoproduction.CodeProjectLocked)
	lockedEpisodeResponse := doAPIRequest(t, handler, http.MethodPatch,
		fmt.Sprintf("/api/projects/%s/script-episodes/%s", project.ID, seed.FirstEpisodeID),
		registration.AccessToken, registration.OrganizationID, map[string]any{
			"content": "must not replace the confirmed episode snapshot while rebuilding",
		})
	assertVideoProductionAPIError(t, lockedEpisodeResponse, http.StatusConflict, videoproduction.CodeProjectLocked)

	var oldPlanStatus string
	var oldPlanActive bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT status, active FROM storyboard_plans WHERE id = $1`, seed.StoryboardPlanID).Scan(&oldPlanStatus, &oldPlanActive))
	require.Equal(t, "archived", oldPlanStatus)
	require.False(t, oldPlanActive)
	var oldShotStale string
	require.NoError(t, pool.QueryRow(ctx, `SELECT stale_state FROM storyboard_shots WHERE id = $1`, seed.StoryboardShotID).Scan(&oldShotStale))
	require.Equal(t, "needs_regeneration", oldShotStale)
	var productionStatus ProductionStatus
	doAPISuccess(t, handler, http.MethodGet,
		fmt.Sprintf("/api/projects/%s/production/status", project.ID),
		registration.AccessToken, registration.OrganizationID, nil, &productionStatus)
	require.Equal(t, 0, productionStatus.Stages.Storyboard.ShotCount)
	require.Equal(t, "not_started", productionStatus.Stages.Storyboard.Status)
	require.Equal(t, 0, productionStatus.Stages.ShotAssets.RequirementCount)
	var shotStatus ShotProductionStatus
	doAPISuccess(t, handler, http.MethodGet,
		fmt.Sprintf("/api/projects/%s/shot-production/status", project.ID),
		registration.AccessToken, registration.OrganizationID, nil, &shotStatus)
	require.Zero(t, shotStatus.Summary.Total)
	var requirements struct {
		Items []ShotAssetRequirement `json:"items"`
	}
	doAPISuccess(t, handler, http.MethodGet,
		fmt.Sprintf("/api/projects/%s/shot-asset-requirements", project.ID),
		registration.AccessToken, registration.OrganizationID, nil, &requirements)
	require.Empty(t, requirements.Items)
	var artifacts struct {
		Items []Artifact `json:"items"`
	}
	doAPISuccess(t, handler, http.MethodGet,
		fmt.Sprintf("/api/artifacts?filter[projectId]=%s", project.ID),
		registration.AccessToken, registration.OrganizationID, nil, &artifacts)
	require.False(t, artifactListContains(artifacts.Items, seed.OldProductionArtifactID))
	require.True(t, artifactListContains(artifacts.Items, seed.RetainedAssetArtifactID))
	var retainedAssets int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM canonical_assets WHERE id = $1 AND status <> 'archived'`, seed.CanonicalAssetID).Scan(&retainedAssets))
	require.Equal(t, 1, retainedAssets)

	items, err := activities.ListProjectVideoProductionRebuildItems(ctx, workflowInput)
	require.NoError(t, err)
	require.Len(t, items, 2)
	authorizedItem, err := activities.StartProjectVideoProductionRebuildItem(ctx, workflowInput, items[0].ItemID)
	require.NoError(t, err)
	authorizedExecution, err := workflows.StartNodeRun(ctx, pool, workflows.NodeRunInput{
		OrganizationID: registration.OrganizationID,
		ProjectID:      project.ID,
		WorkflowRunID:  authorizedItem.WorkflowRunID,
		NodeKey:        "rebuild-lock-authorization",
		NodeType:       "test.rebuild_lock_authorization",
	})
	require.NoError(t, err)
	require.NoError(t, workflows.CancelNodeRun(ctx, pool, authorizedExecution.NodeRunID, json.RawMessage(`{"cancelled":true}`), "integration test complete"))
	_, err = pool.Exec(ctx, `UPDATE workflow_runs SET status = 'running', started_at = now() WHERE id = $1`, workflowInput.WorkflowRunID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		UPDATE project_video_production_rebuild_items
		SET status = CASE WHEN episode_ordinal = 1 THEN 'succeeded' ELSE 'failed' END,
		    completed_at = now(), failure_code = CASE WHEN episode_ordinal = 2 THEN 'TEST_FAILURE' END
		WHERE rebuild_id = $1
	`, rebuild.ID)
	require.NoError(t, err)
	partial, err := activities.FinalizeProjectVideoProductionRebuild(ctx, workflowInput)
	require.NoError(t, err)
	require.Equal(t, "partial_succeeded", partial.Status)
	require.Equal(t, 1, partial.SucceededItems)
	require.Equal(t, 1, partial.FailedItems)
	partialCurrentResponse := doVideoProductionRebuildRequest(t, handler, http.MethodGet,
		fmt.Sprintf("/api/projects/%s/video-production/rebuilds/current", project.ID),
		registration.AccessToken, registration.OrganizationID, "", nil)
	require.Equal(t, http.StatusOK, partialCurrentResponse.Code, partialCurrentResponse.Body.String())
	partialCurrent := decodeNullableVideoProductionRebuildData(t, partialCurrentResponse)
	require.NotNil(t, partialCurrent)
	require.Equal(t, rebuild.ID, partialCurrent.ID)
	require.Equal(t, "partial_succeeded", partialCurrent.Status)
	_, err = pool.Exec(ctx, `
		UPDATE script_episodes SET content = content || E'\nexternal snapshot drift'
		WHERE id = $1
	`, seed.SecondEpisodeID)
	require.NoError(t, err)
	staleRetryResponse := doVideoProductionRebuildRequest(t, handler, http.MethodPost,
		fmt.Sprintf("/api/projects/%s/video-production/rebuilds/%s/retry-failed", project.ID, rebuild.ID),
		registration.AccessToken, registration.OrganizationID, "stale-retry-"+suffix, map[string]any{})
	assertVideoProductionAPIError(t, staleRetryResponse, http.StatusConflict, videoproduction.CodeRebuildImpactStale)
	_, err = pool.Exec(ctx, `
		UPDATE project_video_production_rebuild_items item
		SET script_episode_revision = episode.revision,
		    script_episode_content_hash = episode.content_hash
		FROM script_episodes episode
		WHERE item.rebuild_id = $1 AND item.script_episode_id = episode.id AND episode.id = $2
	`, rebuild.ID, seed.SecondEpisodeID)
	require.NoError(t, err)

	retryResponse := doVideoProductionRebuildRequest(t, handler, http.MethodPost,
		fmt.Sprintf("/api/projects/%s/video-production/rebuilds/%s/retry-failed", project.ID, rebuild.ID),
		registration.AccessToken, registration.OrganizationID, "retry-"+suffix, map[string]any{})
	require.Equal(t, http.StatusAccepted, retryResponse.Code, retryResponse.Body.String())
	retried := decodeVideoProductionRebuildData(t, retryResponse)
	require.NotNil(t, retried.WorkflowRunID)
	require.NotEqual(t, workflowInput.WorkflowRunID, *retried.WorkflowRunID)
	_, err = activities.PrepareProjectVideoProductionRebuild(ctx, workflowInput)
	require.ErrorIs(t, err, workflows.ErrWorkflowWriteFenced)

	retryInput := workflowInput
	retryInput.WorkflowRunID = *retried.WorkflowRunID
	retryInput.RetryFailed = true
	retryPreparation, err := activities.PrepareProjectVideoProductionRebuild(ctx, retryInput)
	require.NoError(t, err)
	require.True(t, retryPreparation.GenerationSwitched)
	retryItems, err := activities.ListProjectVideoProductionRebuildItems(ctx, retryInput)
	require.NoError(t, err)
	require.Len(t, retryItems, 1)

	_, err = pool.Exec(ctx, `UPDATE workflow_runs SET status = 'running', started_at = now() WHERE id = $1`, retryInput.WorkflowRunID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		UPDATE project_video_production_rebuild_items
		SET status = 'succeeded', completed_at = now(), failure_code = NULL, failure_message = NULL
		WHERE rebuild_id = $1 AND status = 'failed'
	`, rebuild.ID)
	require.NoError(t, err)
	completed, err := activities.FinalizeProjectVideoProductionRebuild(ctx, retryInput)
	require.NoError(t, err)
	require.Equal(t, "succeeded", completed.Status)
	require.Equal(t, 2, completed.SucceededItems)
	require.Zero(t, completed.FailedItems)

	var projectLocked bool
	var projectState string
	var activeGenerationID string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT video_production_locked, video_production_state, active_video_production_generation_id::text
		FROM projects WHERE id = $1
	`, project.ID).Scan(&projectLocked, &projectState, &activeGenerationID))
	require.False(t, projectLocked)
	require.Equal(t, "ready", projectState)
	require.Equal(t, switched.Generation.ID, activeGenerationID)
	completedCurrentResponse := doVideoProductionRebuildRequest(t, handler, http.MethodGet,
		fmt.Sprintf("/api/projects/%s/video-production/rebuilds/current", project.ID),
		registration.AccessToken, registration.OrganizationID, "", nil)
	require.Equal(t, http.StatusOK, completedCurrentResponse.Code, completedCurrentResponse.Body.String())
	require.Nil(t, decodeNullableVideoProductionRebuildData(t, completedCurrentResponse))

	staleWorkflowResponse := doAPIRequest(t, handler, http.MethodPost,
		fmt.Sprintf("/api/projects/%s/storyboard-shots", project.ID),
		registration.AccessToken, registration.OrganizationID, map[string]any{
			"workflowRunId": seed.OldWorkflowRunID,
			"visual":        "must not be inserted into the new generation",
		})
	assertVideoProductionAPIError(t, staleWorkflowResponse, http.StatusConflict, videoproduction.CodeGenerationMismatch)
	var staleWorkflowShots int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM storyboard_shots WHERE workflow_run_id = $1`, seed.OldWorkflowRunID).Scan(&staleWorkflowShots))
	require.Zero(t, staleWorkflowShots)

	var manualShot StoryboardShot
	doAPISuccess(t, handler, http.MethodPost,
		fmt.Sprintf("/api/projects/%s/storyboard-shots", project.ID),
		registration.AccessToken, registration.OrganizationID, map[string]any{
			"visual": "new generation manual shot",
		}, &manualShot)
	var shotGenerationID, workflowGenerationID, workflowBindingID string
	var workflowBindingRevision int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT shot.production_generation_id::text, run.production_generation_id::text,
		       run.video_production_binding_id::text, run.video_production_binding_revision
		FROM storyboard_shots shot
		JOIN workflow_runs run ON run.id = shot.workflow_run_id
		WHERE shot.id = $1
	`, manualShot.ID).Scan(&shotGenerationID, &workflowGenerationID, &workflowBindingID, &workflowBindingRevision))
	require.Equal(t, switched.Generation.ID, shotGenerationID)
	require.Equal(t, switched.Generation.ID, workflowGenerationID)
	require.Equal(t, switched.Binding.ID, workflowBindingID)
	require.Equal(t, switched.Binding.Revision, workflowBindingRevision)

	var firstAttemptStatus, retryAttemptStatus string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status FROM project_video_production_rebuild_attempts WHERE workflow_run_id = $1
	`, workflowInput.WorkflowRunID).Scan(&firstAttemptStatus))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status FROM project_video_production_rebuild_attempts WHERE workflow_run_id = $1
	`, retryInput.WorkflowRunID).Scan(&retryAttemptStatus))
	require.Equal(t, "partial_succeeded", firstAttemptStatus)
	require.Equal(t, "succeeded", retryAttemptStatus)

	assertVideoProductionEventPayload(t, ctx, pool, project.ID, rebuild.ID,
		"video.production.rebuild.requested", workflowInput.WorkflowRunID)
	assertVideoProductionEventPayload(t, ctx, pool, project.ID, rebuild.ID,
		"video.production.rebuild.requested", retryInput.WorkflowRunID)
	assertVideoProductionEventPayload(t, ctx, pool, project.ID, rebuild.ID,
		"video.production.rebuild.started", workflowInput.WorkflowRunID)
	assertVideoProductionEventPayload(t, ctx, pool, project.ID, rebuild.ID,
		"video.production.rebuild.started", retryInput.WorkflowRunID)
	assertVideoProductionEventPayload(t, ctx, pool, project.ID, rebuild.ID,
		"video.production.rebuild.partial", workflowInput.WorkflowRunID)
	assertVideoProductionEventPayload(t, ctx, pool, project.ID, rebuild.ID,
		"video.production.rebuild.completed", retryInput.WorkflowRunID)
}

func videoProductionRebuildConfiguration(project Project, ratio string) map[string]any {
	projectType := ""
	if project.ProjectType != nil {
		projectType = *project.ProjectType
	}
	contentType := ""
	if project.ContentType != nil {
		contentType = *project.ContentType
	}
	return map[string]any{
		"projectType":           projectType,
		"contentType":           contentType,
		"aspectRatio":           ratio,
		"videoRatio":            ratio,
		"artStyle":              project.ArtStyle,
		"imageModelProfileKey":  project.ImageModelProfileKey,
		"videoModelProfileKey":  project.VideoModelProfileKey,
		"scriptModelProfileKey": project.ScriptModelProfileKey,
		"ttsModelProfileKey":    project.TTSModelProfileKey,
		"asrModelProfileKey":    project.ASRModelProfileKey,
		"audioStrategy":         project.AudioStrategy,
		"audioRequirement":      project.AudioRequirement,
		"imageQuality":          project.ImageQuality,
		"timelineTimebase":      project.TimelineTimebase,
		"fpsNumerator":          project.FPSNumerator,
		"fpsDenominator":        project.FPSDenominator,
		"settings":              json.RawMessage(project.Settings),
	}
}

func assertVideoProductionEventPayload(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	projectID string,
	rebuildID string,
	eventType string,
	workflowRunID string,
) {
	t.Helper()
	var raw []byte
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT payload
		FROM event_outbox
		WHERE project_id = $1
		  AND aggregate_id = $2
		  AND event_type = $3
		  AND payload->>'workflowRunId' = $4
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, projectID, rebuildID, eventType, workflowRunID).Scan(&raw), "missing lifecycle event %s for workflow %s", eventType, workflowRunID)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(raw, &payload))
	for _, field := range []string{"bindingId", "bindingRevision", "productionGenerationId", "rebuildId", "workflowRunId"} {
		require.NotEmpty(t, payload[field], "%s payload is missing %s", eventType, field)
	}
}

func integrationTestEnvironment(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

type videoProductionRebuildProjectSeed struct {
	FirstEpisodeID          string
	SecondEpisodeID         string
	CanonicalAssetID        string
	StoryboardPlanID        string
	StoryboardShotID        string
	ShotAssetRequirementID  string
	OldProductionArtifactID string
	RetainedAssetArtifactID string
	OldWorkflowRunID        string
}

func seedVideoProductionRebuildTargetProfile(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) (string, string) {
	t.Helper()
	key := "video_rebuild_test_" + suffix
	var profileID, versionID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO video_production_profiles(profile_key, name, strategy_family, description)
		VALUES ($1, 'Video Rebuild Test Profile', 'single_frame_i2v_test', 'integration test profile')
		RETURNING id::text
	`, key).Scan(&profileID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO video_production_profile_versions(
			profile_id, version, lifecycle_state, implementation_state,
			configuration, capability_requirements, prompt_contract,
			input_contract_version, configuration_hash, prompt_contract_hash, published_at
		)
		SELECT $1, 1, 'published', 'available', configuration,
		       capability_requirements, prompt_contract, input_contract_version,
		       configuration_hash, prompt_contract_hash, now()
		FROM video_production_profile_versions source
		JOIN video_production_profiles profile ON profile.id = source.profile_id
		WHERE profile.profile_key = $2 AND source.version = 1
		RETURNING id::text
	`, profileID, videoproduction.ProfileSingleFrameI2V).Scan(&versionID))
	return key, versionID
}

func seedVideoProductionRebuildModelBinding(t *testing.T, ctx context.Context, pool *pgxpool.Pool, organizationID, userID string) {
	t.Helper()
	var connectorID, accountID, modelID, profileID string
	require.NoError(t, pool.QueryRow(ctx, `SELECT id::text FROM provider_connectors ORDER BY is_official DESC, created_at LIMIT 1`).Scan(&connectorID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO provider_accounts(organization_id, connector_id, name, base_url, auth_type, status, created_by)
		VALUES ($1, $2, 'Video Rebuild Test', 'https://example.invalid/v1', 'none', 'active', $3)
		RETURNING id::text
	`, organizationID, connectorID, userID).Scan(&accountID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status)
		VALUES ($1, 'video-rebuild-test', 'Video Rebuild Test', 'video', 'active')
		RETURNING id::text
	`, accountID).Scan(&modelID))
	_, err := pool.Exec(ctx, `
		INSERT INTO provider_model_capabilities(provider_model_id, task_types, provider_options_schema)
		VALUES ($1, '["video.image_to_video"]', '{"xCapabilities":{"supportsFirstFrame":true,"supportsReferenceImages":true,"maxReferenceImages":1,"referenceTypes":["first_frame"]}}')
	`, modelID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO model_profiles(organization_id, profile_key, name, purpose)
		VALUES ($1, 'video_generation_default', 'Video Generation Default', 'integration test')
		RETURNING id::text
	`, organizationID).Scan(&profileID))
	_, err = pool.Exec(ctx, `
		INSERT INTO model_profile_bindings(model_profile_id, provider_model_id, priority, weight, enabled)
		VALUES ($1, $2, 1, 100, true)
	`, profileID, modelID)
	require.NoError(t, err)
}

func seedVideoProductionRebuildProjectData(t *testing.T, ctx context.Context, pool *pgxpool.Pool, registration auth.TokenResponse, project Project) videoProductionRebuildProjectSeed {
	t.Helper()
	var scriptID, versionID, firstEpisodeID, secondEpisodeID, timingID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO scripts(organization_id, project_id, title, created_by, status)
		VALUES ($1, $2, 'Rebuild Test Script', $3, 'active') RETURNING id::text
	`, registration.OrganizationID, project.ID, registration.User.ID).Scan(&scriptID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO script_versions(script_id, version_no, organization_id, project_id, version, content, content_format, status, created_by)
		VALUES ($1, 1, $2, $3, 1, 'episode content', 'markdown', 'active', $4) RETURNING id::text
	`, scriptID, registration.OrganizationID, project.ID, registration.User.ID).Scan(&versionID))
	_, err := pool.Exec(ctx, `UPDATE scripts SET current_version_id = $2 WHERE id = $1`, scriptID, versionID)
	require.NoError(t, err)
	for episodeIndex := 1; episodeIndex <= 2; episodeIndex++ {
		var episodeID string
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO script_episodes(
				organization_id, project_id, script_id, script_version_id,
				episode_index, episode_title, content, review_status, created_by
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'approved', $8)
			RETURNING id::text
		`, registration.OrganizationID, project.ID, scriptID, versionID, episodeIndex,
			fmt.Sprintf("Episode %d", episodeIndex), fmt.Sprintf("Episode %d content", episodeIndex), registration.User.ID).Scan(&episodeID))
		if episodeIndex == 1 {
			firstEpisodeID = episodeID
		} else if episodeIndex == 2 {
			secondEpisodeID = episodeID
		}
	}
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO script_timing_analyses(
			organization_id, project_id, script_id, script_version_id, script_episode_id,
			revision, status, estimated_duration_ticks, minimum_duration_ticks,
			target_duration_ticks, method_version, created_by
		)
		VALUES ($1, $2, $3, $4, $5, 1, 'ready', 90000, 90000, 90000, 'integration-test', $6)
		RETURNING id::text
	`, registration.OrganizationID, project.ID, scriptID, versionID, firstEpisodeID, registration.User.ID).Scan(&timingID))
	var planID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO storyboard_plans(
			organization_id, project_id, script_id, script_version_id, script_episode_id,
			timing_analysis_id, revision, status, target_duration_ticks,
			estimated_shot_count, actual_shot_count, active, created_by, production_generation_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, 1, 'ready', 90000, 1, 1, true, $7, $8)
		RETURNING id::text
	`, registration.OrganizationID, project.ID, scriptID, versionID, firstEpisodeID, timingID,
		registration.User.ID, project.ProductionGeneration.ID).Scan(&planID))
	var shotID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, script_id, script_version_id, script_episode_id,
			storyboard_plan_id, shot_index, shot_no, episode_index, episode_shot_index,
			title, visual, start_tick, end_tick, status, production_generation_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, 1, 1, 1, 1,
		        'Old Shot', 'Old visual', 0, 90000, 'ready', $7)
		RETURNING id::text
	`, registration.OrganizationID, project.ID, scriptID, versionID, firstEpisodeID, planID,
		project.ProductionGeneration.ID).Scan(&shotID))
	var oldWorkflowRunID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO workflow_runs(
			organization_id, project_id, temporal_workflow_id, workflow_type, status,
			input, output, created_by, production_generation_id,
			video_production_binding_id, video_production_binding_revision
		)
		VALUES ($1, $2, $3, 'script_to_storyboard', 'failed', '{}', '{}', $4, $5, $6, $7)
		RETURNING id::text
	`, registration.OrganizationID, project.ID, "old-manual-storyboard-"+uuid.NewString(), registration.User.ID,
		project.ProductionGeneration.ID, project.VideoProductionBinding.ID, project.VideoProductionBinding.Revision).Scan(&oldWorkflowRunID))
	var assetID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO canonical_assets(organization_id, project_id, asset_type, name, description, status, created_by)
		VALUES ($1, $2, 'character', 'Retained Character', 'must survive rebuild', 'prompt_ready', $3)
		RETURNING id::text
	`, registration.OrganizationID, project.ID, registration.User.ID).Scan(&assetID))
	var requirementID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO shot_asset_requirements(
			organization_id, project_id, workflow_run_id, storyboard_shot_id, asset_id,
			requirement_type, status, production_generation_id
		)
		VALUES ($1, $2, $3, $4, $5, 'character', 'pending', $6)
		RETURNING id::text
	`, registration.OrganizationID, project.ID, oldWorkflowRunID, shotID, assetID,
		project.ProductionGeneration.ID).Scan(&requirementID))
	var oldProductionArtifactID, retainedAssetArtifactID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO artifacts(
			organization_id, project_id, workflow_run_id, type, storage_key, mime_type,
			production_generation_id, created_by
		)
		VALUES ($1, $2, $3, 'shot_image', 'old-generation/shot.png', 'image/png', $4, $5)
		RETURNING id::text
	`, registration.OrganizationID, project.ID, oldWorkflowRunID,
		project.ProductionGeneration.ID, registration.User.ID).Scan(&oldProductionArtifactID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO artifacts(
			organization_id, project_id, workflow_run_id, type, storage_key, mime_type,
			production_generation_id, created_by
		)
		VALUES ($1, $2, $3, 'asset_reference', 'retained-assets/character.png', 'image/png', $4, $5)
		RETURNING id::text
	`, registration.OrganizationID, project.ID, oldWorkflowRunID,
		project.ProductionGeneration.ID, registration.User.ID).Scan(&retainedAssetArtifactID))
	_, err = pool.Exec(ctx, `
		INSERT INTO asset_references(
			organization_id, project_id, asset_id, reference_type, artifact_id, storage_key,
			is_primary, status, created_by
		)
		VALUES ($1, $2, $3, 'generated', $4, 'retained-assets/character.png', true, 'ready', $5)
	`, registration.OrganizationID, project.ID, assetID, retainedAssetArtifactID, registration.User.ID)
	require.NoError(t, err)
	return videoProductionRebuildProjectSeed{
		FirstEpisodeID:          firstEpisodeID,
		SecondEpisodeID:         secondEpisodeID,
		CanonicalAssetID:        assetID,
		StoryboardPlanID:        planID,
		StoryboardShotID:        shotID,
		ShotAssetRequirementID:  requirementID,
		OldProductionArtifactID: oldProductionArtifactID,
		RetainedAssetArtifactID: retainedAssetArtifactID,
		OldWorkflowRunID:        oldWorkflowRunID,
	}
}

func artifactListContains(items []Artifact, artifactID string) bool {
	for _, item := range items {
		if item.ID == artifactID {
			return true
		}
	}
	return false
}

func doVideoProductionRebuildRequest(t *testing.T, handler http.Handler, method, path, token, organizationID, idempotencyKey string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	request := httptest.NewRequest(method, path, bytes.NewReader(raw))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Organization-Id", organizationID)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", idempotencyKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decodeVideoProductionRebuildData(t *testing.T, recorder *httptest.ResponseRecorder) projectVideoProductionRebuild {
	t.Helper()
	var envelope struct {
		Data projectVideoProductionRebuild `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	return envelope.Data
}

func decodeNullableVideoProductionRebuildData(t *testing.T, recorder *httptest.ResponseRecorder) *projectVideoProductionRebuild {
	t.Helper()
	var envelope struct {
		Data *projectVideoProductionRebuild `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	return envelope.Data
}
