package workflows

import (
	"context"
	"testing"
)

func TestFailNativeAudioReviewBlocksSegmentPlanAndShot(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	orgID, userID, projectID, workflowRunID, _, providerModelID := seedWorkflowGatewayIntegrationData(t, ctx, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})

	var providerAccountID string
	if err := pool.QueryRow(ctx, `SELECT provider_account_id::text FROM provider_models WHERE id = $1`, providerModelID).Scan(&providerAccountID); err != nil {
		t.Fatalf("load provider account: %v", err)
	}
	var shotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, workflow_run_id, shot_index, shot_no,
			start_tick, end_tick, duration_min_ticks, duration_max_ticks,
			visual, image_prompt, video_prompt, status, native_audio_status, production_readiness, metadata
		)
		VALUES ($1, $2, $3, 0, 1, 0, 450000, 450000, 450000,
		        '测试镜头', '测试图片', '测试视频', 'video_succeeded', 'audio_unverified', 'preview_only', '{}')
		RETURNING id::text
	`, orgID, projectID, workflowRunID).Scan(&shotID); err != nil {
		t.Fatalf("insert storyboard shot: %v", err)
	}
	var planID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO video_render_plans(
			organization_id, project_id, storyboard_shot_id, workflow_run_id,
			provider_account_id, provider_model_id, model_family, variant_key,
			capability_snapshot, capability_snapshot_hash, plan_key,
			target_duration_ticks, timeline_timebase, fps_numerator, fps_denominator,
			task_type, reference_mode, aspect_ratio, resolution,
			audio_strategy, audio_requirement, native_audio_status, production_readiness,
			expires_at, status, active
		)
		VALUES ($1, $2, $3::uuid, $4, $5, $6, 'integration-video', 'native-audio',
		        '{}', 'sha256:integration', 'audio-review-' || $3::text,
		        450000, 90000, 24, 1, 'video.image_to_video', 'first_frame', '16:9', '720p',
		        'native_av', 'required', 'audio_unverified', 'preview_only', now() + interval '15 minutes', 'running', true)
		RETURNING id::text
	`, orgID, projectID, shotID, workflowRunID, providerAccountID, providerModelID).Scan(&planID); err != nil {
		t.Fatalf("insert render plan: %v", err)
	}
	var segmentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO video_render_segments(
			organization_id, project_id, video_render_plan_id, storyboard_shot_id, segment_index,
			planned_start_tick, planned_end_tick, requested_duration_seconds, continuity_mode, status,
			provider_model_id, dialogue, native_audio_requested, native_audio_detected,
			audio_verification_status, production_readiness
		)
		VALUES ($1, $2, $3, $4, 0, 0, 450000, 5, 'first_frame', 'succeeded', $5,
		        '[{"speaker":"方源","text":"开始。","startTick":0,"endTick":90000}]', true, true,
		        'audio_unverified', 'preview_only')
		RETURNING id::text
	`, orgID, projectID, planID, shotID, providerModelID).Scan(&segmentID); err != nil {
		t.Fatalf("insert render segment: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE storyboard_shots SET active_video_render_plan_id = $2 WHERE id = $1`, shotID, planID); err != nil {
		t.Fatalf("bind render plan: %v", err)
	}
	var reviewID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO native_audio_reviews(
			organization_id, project_id, video_render_plan_id, video_render_segment_id, workflow_run_id,
			revision, status, expected_dialogue, metadata
		)
		VALUES ($1, $2, $3, $4, $5, 1, 'running', '[]', '{}') RETURNING id::text
	`, orgID, projectID, planID, segmentID, workflowRunID).Scan(&reviewID); err != nil {
		t.Fatalf("insert review: %v", err)
	}
	nodeExecution, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		NodeKey: "native-audio-review-test", NodeType: "audio.asr.review", Input: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("start node run: %v", err)
	}

	activities := NewActivities(pool, nil, nil)
	output, err := activities.failNativeAudioReview(ctx, nodeExecution, ReviewNativeAudioSegmentInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID, CreatedBy: userID, ReviewID: reviewID,
	}, nativeAudioReviewRecord{ID: reviewID, PlanID: planID, SegmentID: segmentID, ShotID: shotID, AudioConfigurationRevision: 1}, "UPSTREAM_TIMEOUT", "语音识别服务超时")
	if err != nil {
		t.Fatalf("fail native audio review: %v", err)
	}
	if output.Status != "failed" || output.ErrorCode != "UPSTREAM_TIMEOUT" {
		t.Fatalf("output = %+v", output)
	}

	var reviewStatus, segmentAudioStatus, segmentReadiness, planAudioStatus, planReadiness, shotAudioStatus, shotReadiness, nodeStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM native_audio_reviews WHERE id = $1`, reviewID).Scan(&reviewStatus); err != nil {
		t.Fatalf("load review: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT audio_verification_status, production_readiness FROM video_render_segments WHERE id = $1`, segmentID).Scan(&segmentAudioStatus, &segmentReadiness); err != nil {
		t.Fatalf("load segment: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT native_audio_status, production_readiness FROM video_render_plans WHERE id = $1`, planID).Scan(&planAudioStatus, &planReadiness); err != nil {
		t.Fatalf("load plan: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT native_audio_status, production_readiness FROM storyboard_shots WHERE id = $1`, shotID).Scan(&shotAudioStatus, &shotReadiness); err != nil {
		t.Fatalf("load shot: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM workflow_node_runs WHERE id = $1`, nodeExecution.NodeRunID).Scan(&nodeStatus); err != nil {
		t.Fatalf("load node run: %v", err)
	}
	if reviewStatus != "failed" || segmentAudioStatus != "needs_audio_retry" || segmentReadiness != "blocked" ||
		planAudioStatus != "needs_audio_retry" || planReadiness != "blocked" || shotAudioStatus != "needs_audio_retry" ||
		shotReadiness != "blocked" || nodeStatus != "failed" {
		t.Fatalf("states review=%s segment=%s/%s plan=%s/%s shot=%s/%s node=%s",
			reviewStatus, segmentAudioStatus, segmentReadiness, planAudioStatus, planReadiness, shotAudioStatus, shotReadiness, nodeStatus)
	}
	var eventCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox WHERE project_id = $1 AND event_type = 'storyboard.audio.review.completed' AND aggregate_id = $2`, projectID, reviewID).Scan(&eventCount); err != nil {
		t.Fatalf("load review event: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("review completion events = %d", eventCount)
	}
}
