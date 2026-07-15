package api

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/workflows"
)

func TestAPIGatewayVideoDialogueSpansPreservesSpeakerlessSystemAudio(t *testing.T) {
	shot := StoryboardShot{
		StartTick: 90000,
		EndTick:   6 * 90000,
		ScriptDialogue: []workflows.StoryboardDialogueLine{
			{TimingUnitID: "system-1", Text: "一声清越蝉鸣，骤然响彻天地。", Kind: "system", SpanStartTick: 3 * 90000, SpanEndTick: 6 * 90000},
		},
	}
	spans, err := apiGatewayVideoDialogueSpans(shot)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 1 || spans[0].Speaker != "" || spans[0].Kind != "system" || spans[0].StartTick != 2*90000 || spans[0].EndTick != 5*90000 {
		t.Fatalf("system audio span = %+v", spans)
	}
}

func TestAPIGatewayVideoDialogueSpansRejectsSpeakerlessDialogue(t *testing.T) {
	_, err := apiGatewayVideoDialogueSpans(StoryboardShot{
		StartTick: 0,
		EndTick:   5 * 90000,
		ScriptDialogue: []workflows.StoryboardDialogueLine{
			{Text: "缺少说话人", Kind: "dialogue", SpanStartTick: 0, SpanEndTick: 90000},
		},
	})
	var standard *provider.StandardErrorError
	if !errors.As(err, &standard) || standard.Standard.Code != provider.CodeStoryboardReplanRequired {
		t.Fatalf("error = %v, want %s", err, provider.CodeStoryboardReplanRequired)
	}
}

func TestGetStoryboardShotRenderPlanRestoresPersistentSegmentState(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	workflowRunID := seed.insertWorkflowRun(t, "running")
	imageArtifactID := seed.insertArtifact(t, "generated_image", "org/project/render-plan-shot.png", "image/png")
	videoArtifactID := seed.insertArtifact(t, "generated_video", "org/project/render-plan-shot.mp4", "video/mp4")
	shotID := seed.insertStoryboardShot(t, workflowRunID, imageArtifactID, videoArtifactID)

	var connectorID, accountID, modelID, planID, segmentID string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT id::text FROM provider_connectors ORDER BY created_at LIMIT 1`).Scan(&connectorID); err != nil {
		t.Fatalf("load provider connector: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO provider_accounts(organization_id, connector_id, name, base_url, auth_type, status, config, created_by)
		VALUES ($1, $2, 'Render Plan Test', 'https://example.test/v1', 'bearer', 'active', '{}', $3)
		RETURNING id::text
	`, seed.organizationID, connectorID, seed.ownerUserID).Scan(&accountID); err != nil {
		t.Fatalf("insert provider account: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status)
		VALUES ($1, 'video-test', 'Video Test', 'video', 'active') RETURNING id::text
	`, accountID).Scan(&modelID); err != nil {
		t.Fatalf("insert provider model: %v", err)
	}
	var durationTicks, timebase int64
	var fpsNumerator, fpsDenominator int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT shot.planned_duration_ticks, project.timeline_timebase, project.fps_numerator, project.fps_denominator
		FROM storyboard_shots shot JOIN projects project ON project.id = shot.project_id WHERE shot.id = $1
	`, shotID).Scan(&durationTicks, &timebase, &fpsNumerator, &fpsDenominator); err != nil {
		t.Fatalf("load shot timing: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO video_render_plans(
		  organization_id, project_id, storyboard_shot_id, workflow_run_id,
		  provider_account_id, provider_model_id, model_family, variant_key,
		  capability_snapshot, capability_snapshot_hash, plan_key,
		  target_duration_ticks, timeline_timebase, fps_numerator, fps_denominator,
		  task_type, reference_mode, aspect_ratio, resolution,
		  audio_strategy, audio_requirement, native_audio_status, production_readiness, expires_at, status
		)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6::uuid, 'video-test', 'native-720p',
		  '{"variantKey":"native-720p","when":{},"duration":{"mode":"fixed","values":[5]},"frameRate":{"mode":"fixed","values":[24]},"nativeAudio":{"support":"true","audioTrackSeparable":false},"continuation":{"supportsExtension":false,"supportsFirstFrame":true,"supportsLastFrame":false,"supportsVideoReference":false}}',
		  'sha256:test', 'test-plan-' || $3::text, $7, $8, $9, $10,
		  'video.image_to_video', 'first_frame', '16:9', '720p',
		  'native_av', 'preferred', 'audio_unverified', 'preview_only', now() + interval '15 minutes', 'running'
		) RETURNING id::text
	`, seed.organizationID, seed.projectID, shotID, workflowRunID, accountID, modelID, durationTicks, timebase, fpsNumerator, fpsDenominator).Scan(&planID); err != nil {
		t.Fatalf("insert render plan: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO video_render_segments(
		  organization_id, project_id, video_render_plan_id, storyboard_shot_id, segment_index,
		  planned_start_tick, planned_end_tick, requested_duration_seconds, continuity_mode, status,
		  provider_model_id, storage_key, dialogue, native_audio_requested, native_audio_detected,
		  audio_verification_status, production_readiness
		)
		VALUES ($1, $2, $3, $4, 0, 0, $5, 5, 'first_frame', 'succeeded', $6,
		        'org/project/render-segment.mp4', '[{"speaker":"方源","text":"开始。"}]', true, true,
		        'audio_unverified', 'preview_only') RETURNING id::text
	`, seed.organizationID, seed.projectID, planID, shotID, durationTicks, modelID).Scan(&segmentID); err != nil {
		t.Fatalf("insert render segment: %v", err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE storyboard_shots SET active_video_render_plan_id = $2, native_audio_status = 'audio_unverified', production_readiness = 'preview_only' WHERE id = $1
	`, shotID, planID); err != nil {
		t.Fatalf("bind render plan: %v", err)
	}

	assertAPIErrorCode(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/storyboard-shots/"+shotID+"/render-plan", seed.otherToken, seed.organizationID, nil, http.StatusForbidden, "ACCESS_DENIED")
	var detail VideoRenderPlanDetail
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/storyboard-shots/"+shotID+"/render-plan", seed.ownerToken, seed.organizationID, nil, &detail)
	if detail.ID != planID || detail.VariantKey != "native-720p" || detail.NativeAudioStatus != "audio_unverified" || detail.ProductionReadiness != "preview_only" || len(detail.Segments) != 1 {
		t.Fatalf("render plan detail = %+v", detail)
	}
	if detail.Segments[0].ID != segmentID || detail.Segments[0].PreviewURL == nil || detail.Segments[0].PlannedDurationFrames <= 0 {
		t.Fatalf("render segment detail = %+v", detail.Segments[0])
	}

	assertAPIErrorCode(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/storyboard-shots/"+shotID+"/render-plan/audio-verification", seed.otherToken, seed.organizationID, map[string]any{"decision": "approve"}, http.StatusForbidden, "ACCESS_DENIED")
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/storyboard-shots/"+shotID+"/render-plan/audio-verification", seed.ownerToken, seed.organizationID, map[string]any{"decision": "approve", "notes": "对白与音画同步"}, &detail)
	if detail.NativeAudioStatus != "audio_verified" || detail.ProductionReadiness != "ready" || detail.Segments[0].AudioVerificationStatus != "audio_verified" {
		t.Fatalf("approved render plan = %+v", detail)
	}
	var shotAudioStatus, shotReadiness string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT native_audio_status, production_readiness FROM storyboard_shots WHERE id = $1`, shotID).Scan(&shotAudioStatus, &shotReadiness); err != nil {
		t.Fatalf("load approved shot state: %v", err)
	}
	if shotAudioStatus != "audio_verified" || shotReadiness != "ready" {
		t.Fatalf("approved shot state = %s/%s", shotAudioStatus, shotReadiness)
	}

	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/storyboard-shots/"+shotID+"/render-plan/audio-verification", seed.ownerToken, seed.organizationID, map[string]any{"decision": "reject", "notes": "台词不匹配"}, &detail)
	if detail.NativeAudioStatus != "needs_audio_retry" || detail.ProductionReadiness != "blocked" || detail.Segments[0].AudioVerificationStatus != "needs_audio_retry" {
		t.Fatalf("rejected render plan = %+v", detail)
	}
}
