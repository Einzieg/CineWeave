package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPlanVideoPersistsIdempotentRenderSegments(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run video planner integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for video planner integration tests")
	}
	ctx := context.Background()
	t.Setenv("CINEWEAVE_ALLOW_PRIVATE_PROVIDER_REFERENCE_URLS", "true")
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	vault, err := NewVault("")
	if err != nil {
		t.Fatal(err)
	}
	upstream := newVideoRuntimeMock(t)
	defer upstream.Close()
	orgID, userID, projectID, modelID := seedGatewayVideoIntegrationData(t, ctx, pool, vault, upstream.URL)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID) })
	if _, err := pool.Exec(ctx, `
		UPDATE provider_model_capabilities
		SET provider_options_schema = $2::jsonb
		WHERE provider_model_id = $1
	`, modelID, mustJSON(map[string]any{"xCapabilities": map[string]any{
		"supportsAsyncTask": true,
		"videoGenerationVariants": []map[string]any{{
			"variantKey": "image_to_video_native_audio_720p", "modelFamily": "integration-video",
			"when":        map[string]any{"taskTypes": []string{"video.image_to_video"}, "referenceModes": []string{"first_frame"}, "nativeAudioRequested": true},
			"duration":    map[string]any{"mode": "discrete", "values": []float64{4, 8}},
			"resolutions": []string{"720p"}, "aspectRatios": []string{"16:9"},
			"frameRate":                map[string]any{"mode": "fixed", "values": []int{24}},
			"supportedPromptLanguages": []string{"zh-CN"},
			"nativeAudio":              map[string]any{"support": "true", "supportsDialogue": true, "supportedDialogueLanguages": []string{"zh-CN"}},
			"continuation":             map[string]any{"supportsFirstFrame": true, "supportsLastFrame": true},
			"requestModes":             []string{"async_create", "poll"}, "source": "test", "capabilityVersion": "1",
		}},
	}})); err != nil {
		t.Fatalf("update video variants: %v", err)
	}
	shotID, storyboardPlanID := seedVideoPlannerShot(t, ctx, pool, orgID, projectID)
	service := NewService(pool, vault)
	service.EnableGatewayRuntime()
	service.SetStorage(newMemoryObjectStorage())
	service.SetVideoMediaProbe(func(context.Context, []byte, string) (GatewayVideoMediaProbe, error) {
		return GatewayVideoMediaProbe{
			DurationSeconds: 7.98, Width: 1280, Height: 720,
			FrameRateNumerator: 24, FrameRateDenominator: 1, FrameRate: 24, FrameCount: 192,
			VideoStreamCount: 1, AudioStreamCount: 1, HasAudio: true, VideoCodec: "h264", AudioCodecs: []string{"aac"},
		}, nil
	})
	request := GatewayVideoPlanRequest{
		OrganizationID: orgID, ProjectID: projectID, StoryboardPlanID: storyboardPlanID, StoryboardShotID: shotID,
		ProviderModelID: modelID, TaskType: "video.image_to_video",
		TargetDurationTicks: 10 * 90000, TimelineTimebase: 90000, FPSNumerator: 24, FPSDenominator: 1,
		AudioStrategy: "native_av", AudioRequirement: "preferred", DialogueLanguage: "zh-CN", HasDialogue: true,
		ReferenceMode: "first_frame", AspectRatio: "16:9", Resolution: "720p", PromptLanguage: "zh-CN",
		DialogueSpans: []GatewayVideoDialogueSpan{{TimingUnitID: "dialogue-1", Speaker: "方源", Text: "中文对白保持原文", StartTick: 0, EndTick: 6 * 90000}},
	}
	first, err := service.PlanVideo(ctx, request)
	if err != nil {
		t.Fatalf("PlanVideo: %v", err)
	}
	if first.ExecutionPlanID == "" || first.VariantKey != "image_to_video_native_audio_720p" || first.CapabilitySnapshotHash == "" || len(first.Segments) != 2 {
		t.Fatalf("plan = %+v", first)
	}
	if first.Segments[0].RequestedDurationSeconds != 8 || first.Segments[1].RequestedDurationSeconds != 4 || first.Segments[1].PlannedDurationTicks != 2*90000 {
		t.Fatalf("segments = %+v", first.Segments)
	}
	if first.NativeAudioStatus != "audio_unverified" || first.ProductionReadiness != "preview_only" {
		t.Fatalf("audio gate = %s/%s", first.NativeAudioStatus, first.ProductionReadiness)
	}
	second, err := service.PlanVideo(ctx, request)
	if err != nil {
		t.Fatalf("idempotent PlanVideo: %v", err)
	}
	if second.ExecutionPlanID != first.ExecutionPlanID {
		t.Fatalf("executionPlanId changed: %s != %s", second.ExecutionPlanID, first.ExecutionPlanID)
	}
	var activePlanID string
	var activePlans, segmentCount int
	if err := pool.QueryRow(ctx, `SELECT active_video_render_plan_id::text FROM storyboard_shots WHERE id = $1`, shotID).Scan(&activePlanID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM video_render_plans WHERE storyboard_shot_id = $1 AND active = true`, shotID).Scan(&activePlans); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM video_render_segments WHERE video_render_plan_id = $1`, first.ExecutionPlanID).Scan(&segmentCount); err != nil {
		t.Fatal(err)
	}
	if activePlanID != first.ExecutionPlanID || activePlans != 1 || segmentCount != 2 {
		t.Fatalf("persisted state = plan %s active=%d segments=%d", activePlanID, activePlans, segmentCount)
	}
	if _, err := pool.Exec(ctx, `UPDATE video_render_segments SET status = 'failed', error_code = 'UPSTREAM_TIMEOUT' WHERE id = $1`, first.Segments[0].SegmentID); err != nil {
		t.Fatal(err)
	}
	var workflowRunID, nodeRunID, staleExecutionToken, executionToken string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workflow_runs(
			organization_id, project_id, temporal_workflow_id, status, input, output,
			attempt_generation, started_at, created_by
		)
		VALUES ($1, $2, $3, 'running', '{}', '{}', 1, now(), $4)
		RETURNING id::text
	`, orgID, projectID, "video-retry-fence-"+fmt.Sprint(time.Now().UnixNano()), userID).Scan(&workflowRunID); err != nil {
		t.Fatalf("insert workflow run: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workflow_node_runs(
			organization_id, project_id, workflow_run_id, node_key, node_type,
			status, input, output, attempt_generation, started_at
		)
		VALUES ($1, $2, $3, 'video-retry-fence', 'video.retry', 'running', '{}', '{}', 1, now())
		RETURNING id::text, execution_token::text
	`, orgID, projectID, workflowRunID).Scan(&nodeRunID, &staleExecutionToken); err != nil {
		t.Fatalf("insert workflow node: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		UPDATE workflow_node_runs
		SET execution_token = gen_random_uuid(), revision = revision + 1, updated_at = now()
		WHERE id = $1
		RETURNING execution_token::text
	`, nodeRunID).Scan(&executionToken); err != nil {
		t.Fatalf("rotate workflow node token: %v", err)
	}
	requestIdentity := GatewayVideoRetrySegmentRequest{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		NodeRunID: nodeRunID, NodeExecutionToken: staleExecutionToken, NodeAttemptGeneration: 1,
		ExecutionPlanID: first.ExecutionPlanID, RenderSegmentID: first.Segments[0].SegmentID,
		FailureCode: CodeUpstreamTimeout, FailureMessage: "test timeout",
	}
	if _, err := service.RetryVideoRenderSegment(ctx, requestIdentity); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale retry execution error = %v, want ErrConflict", err)
	}
	var status string
	var retryGeneration int
	if err := pool.QueryRow(ctx, `SELECT status, retry_generation FROM video_render_segments WHERE id = $1`, first.Segments[0].SegmentID).Scan(&status, &retryGeneration); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || retryGeneration != 0 {
		t.Fatalf("stale retry mutated segment: status=%s generation=%d", status, retryGeneration)
	}
	requestIdentity.NodeExecutionToken = executionToken
	retry, err := service.RetryVideoRenderSegment(ctx, GatewayVideoRetrySegmentRequest{
		OrganizationID: requestIdentity.OrganizationID, ProjectID: requestIdentity.ProjectID, WorkflowRunID: requestIdentity.WorkflowRunID,
		NodeRunID: requestIdentity.NodeRunID, NodeExecutionToken: requestIdentity.NodeExecutionToken, NodeAttemptGeneration: requestIdentity.NodeAttemptGeneration,
		ExecutionPlanID: requestIdentity.ExecutionPlanID, RenderSegmentID: requestIdentity.RenderSegmentID,
		FailureCode: requestIdentity.FailureCode, FailureMessage: requestIdentity.FailureMessage,
	})
	if err != nil || retry.ProviderModelID != modelID || retry.RetryGeneration != 1 || retry.RetryScope != "segment" {
		t.Fatalf("same-model segment retry = %+v err=%v", retry, err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE video_render_segments SET status = 'planned', retry_generation = 0,
		       metadata = jsonb_build_object('attemptedProviderModelIds', jsonb_build_array($2::text)), error_code = NULL, error_message = NULL
		WHERE id = $1
	`, first.Segments[0].SegmentID, modelID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE video_render_plans SET status = 'planned', completed_at = NULL WHERE id = $1`, first.ExecutionPlanID); err != nil {
		t.Fatal(err)
	}

	firstCreate, err := service.CreateVideoTask(ctx, GatewayVideoCreateTaskRequest{
		OrganizationID: orgID, ProjectID: projectID,
		ExecutionPlanID: first.ExecutionPlanID, RenderSegmentID: first.Segments[0].SegmentID, CapabilitySnapshotHash: first.CapabilitySnapshotHash,
		IdempotencyKey: "render-segment-0",
		Input:          mustJSON(map[string]any{"prompt": "第一段，中文对白保持原文", "duration": 8, "aspectRatio": "16:9", "resolution": "720p", "mode": "image_to_video"}),
		References:     []GatewayVideoReference{{Type: "image", URL: upstream.URL + "/files/reference.png"}},
	})
	if err != nil || firstCreate.Status != "running" || firstCreate.RenderSegmentID != first.Segments[0].SegmentID {
		t.Fatalf("create first render segment = %+v err=%v", firstCreate, err)
	}
	firstOutput := pollVideoSegmentToSuccess(t, ctx, service, orgID, projectID, firstCreate.ProviderAsyncTaskID)
	secondCreate, err := service.CreateVideoTask(ctx, GatewayVideoCreateTaskRequest{
		OrganizationID: orgID, ProjectID: projectID,
		ExecutionPlanID: first.ExecutionPlanID, RenderSegmentID: first.Segments[1].SegmentID, CapabilitySnapshotHash: first.CapabilitySnapshotHash,
		IdempotencyKey: "render-segment-1",
		Input:          mustJSON(map[string]any{"prompt": "第二段，延续上一段动作", "duration": 4, "aspectRatio": "16:9", "resolution": "720p", "mode": "image_to_video"}),
		References:     []GatewayVideoReference{{Type: "video", ArtifactID: firstOutput.Output.ArtifactID, URL: upstream.URL + "/files/video.mp4"}},
	})
	if err != nil || secondCreate.Status != "running" || secondCreate.RenderSegmentID != first.Segments[1].SegmentID {
		t.Fatalf("create second render segment = %+v err=%v", secondCreate, err)
	}
	_ = pollVideoSegmentToSuccess(t, ctx, service, orgID, projectID, secondCreate.ProviderAsyncTaskID)
	var planStatus, readiness, audioStatus, shotVideoStatus string
	if err := pool.QueryRow(ctx, `
		SELECT plan.status, plan.production_readiness, plan.native_audio_status, shot.video_status
		FROM video_render_plans plan
		JOIN storyboard_shots shot ON shot.active_video_render_plan_id = plan.id
		WHERE plan.id = $1
	`, first.ExecutionPlanID).Scan(&planStatus, &readiness, &audioStatus, &shotVideoStatus); err != nil {
		t.Fatal(err)
	}
	if planStatus != "succeeded" || readiness != "preview_only" || audioStatus != "audio_unverified" || shotVideoStatus != "succeeded" {
		t.Fatalf("completed render plan = %s/%s/%s shot=%s", planStatus, readiness, audioStatus, shotVideoStatus)
	}
}

func pollVideoSegmentToSuccess(t *testing.T, ctx context.Context, service *Service, organizationID, projectID, providerTaskID string) GatewayVideoPollTaskResponse {
	t.Helper()
	for attempt := 0; attempt < 3; attempt++ {
		output, err := service.PollVideoTask(ctx, GatewayVideoPollTaskRequest{OrganizationID: organizationID, ProjectID: projectID, ProviderAsyncTaskID: providerTaskID})
		if err != nil {
			t.Fatalf("poll render segment: %v", err)
		}
		if output.Status == "succeeded" {
			return output
		}
	}
	t.Fatal("render segment did not succeed")
	return GatewayVideoPollTaskResponse{}
}

func seedVideoPlannerShot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, projectID string) (string, string) {
	t.Helper()
	var userID string
	if err := pool.QueryRow(ctx, `SELECT created_by::text FROM projects WHERE id = $1`, projectID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var scriptID, versionID, episodeID, analysisID, planID, shotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO scripts(organization_id, project_id, title, status, created_by)
		VALUES ($1, $2, $3, 'active', $4) RETURNING id::text
	`, orgID, projectID, "Video Planner "+suffix, userID).Scan(&scriptID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO script_versions(organization_id, project_id, script_id, version_no, version, content, content_format, status, metadata, created_by)
		VALUES ($1, $2, $3, 1, 1, 'scene', 'markdown', 'active', '{}', $4) RETURNING id::text
	`, orgID, projectID, scriptID, userID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE scripts SET current_version_id = $2 WHERE id = $1`, scriptID, versionID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO script_episodes(organization_id, project_id, script_id, script_version_id, episode_index, episode_title, content, content_format, review_status, stale_state, metadata, created_by)
		VALUES ($1, $2, $3, $4, 1, '第 1 集', 'scene', 'markdown', 'approved', 'fresh', '{}', $5) RETURNING id::text
	`, orgID, projectID, scriptID, versionID, userID).Scan(&episodeID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO script_timing_analyses(
			organization_id, project_id, script_id, script_version_id, script_episode_id, revision, status,
			estimated_duration_ticks, minimum_duration_ticks, target_duration_ticks,
			timeline_timebase, fps_numerator, fps_denominator, method_version, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, 1, 'ready', 900000, 900000, 900000, 90000, 24, 1, 'test', '{}', $6)
		RETURNING id::text
	`, orgID, projectID, scriptID, versionID, episodeID, userID).Scan(&analysisID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO storyboard_plans(
			organization_id, project_id, script_id, script_version_id, script_episode_id, timing_analysis_id,
			revision, status, target_duration_ticks, estimated_shot_count, actual_shot_count, active, stale_state, metadata, created_by, activated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, 1, 'ready', 900000, 1, 1, true, 'fresh', '{}', $7, now())
		RETURNING id::text
	`, orgID, projectID, scriptID, versionID, episodeID, analysisID, userID).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, script_id, script_version_id, script_episode_id, storyboard_plan_id,
			shot_index, shot_no, episode_shot_index, title, visual, image_prompt, video_prompt,
			start_tick, end_tick, duration_min_ticks, duration_max_ticks, duration_source, status, stale_state, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, 0, 1, 0, 'Shot 1', 'visual', 'image', 'video',
		        0, 900000, 900000, 900000, 'rule_estimated', 'pending', 'needs_regeneration', '{}')
		RETURNING id::text
	`, orgID, projectID, scriptID, versionID, episodeID, planID).Scan(&shotID); err != nil {
		t.Fatal(err)
	}
	return shotID, planID
}
