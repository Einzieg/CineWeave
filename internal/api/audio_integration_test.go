package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/google/uuid"
)

func TestAudioProjectSettingsVoiceCRUDAndWorkflowStart(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run audio API integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for audio API integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)
	authService := auth.NewService(pool, "audio-api-test-secret", time.Hour, 24*time.Hour)
	vault, err := provider.NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	temporal := &fakeTemporalClient{}
	server := New(pool, authService, provider.NewService(pool, vault), nil, nil)
	server.temporal = temporal
	handler := server.Handler()
	suffix := uuid.NewString()
	owner, err := authService.Register(ctx, auth.RegisterRequest{
		Email: "audio-api-" + suffix + "@example.test", Username: randomStorageSegment(), Password: "Password123!", DisplayName: "Audio API",
		OrganizationName: "Audio API Org " + suffix,
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, owner.OrganizationID)
	})
	workspaceID := firstWorkspaceID(t, ctx, pool, owner.OrganizationID)

	var project Project
	doAPISuccess(t, handler, http.MethodPost, "/api/projects", owner.AccessToken, owner.OrganizationID, map[string]any{
		"workspaceId": workspaceID, "name": "Audio Project", "audioStrategy": "native_av", "audioRequirement": "preferred",
		"ttsModelProfileKey": "tts_generation_default", "asrModelProfileKey": "audio_transcription_default", "settings": map[string]any{},
	}, &project)
	if project.FPSNumerator != 24 || project.AudioStrategy != "native_av" || project.AudioRequirement != "preferred" || project.AudioConfigurationRevision != 1 {
		t.Fatalf("project defaults = %+v", project)
	}
	var updatedProject Project
	doAPISuccess(t, handler, http.MethodPatch, "/api/projects/"+project.ID, owner.AccessToken, owner.OrganizationID, map[string]any{
		"audioStrategy": "hybrid", "audioRequirement": "required",
	}, &updatedProject)
	if updatedProject.AudioStrategy != "hybrid" || updatedProject.AudioRequirement != "required" || updatedProject.AudioConfigurationRevision != 2 {
		t.Fatalf("updated audio settings = %+v", updatedProject)
	}

	var characterAssetID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO canonical_assets(organization_id, project_id, asset_type, name, description, status, source_script_ids, metadata, created_by)
		VALUES ($1, $2, 'character', '方源', '角色', 'prompt_ready', '[]', '{}', $3) RETURNING id::text
	`, owner.OrganizationID, project.ID, owner.User.ID).Scan(&characterAssetID); err != nil {
		t.Fatalf("insert character asset: %v", err)
	}
	var voice CharacterVoiceProfile
	doAPISuccess(t, handler, http.MethodPost, "/api/projects/"+project.ID+"/character-voices", owner.AccessToken, owner.OrganizationID, map[string]any{
		"canonicalAssetId": characterAssetID, "characterName": "方源", "displayName": "冷静青年男声",
		"language": "zh-CN", "modelProfileKey": "tts_generation_default", "voiceKey": "alloy", "instructions": "克制、清晰",
	}, &voice)
	if voice.CanonicalAssetID == nil || *voice.CanonicalAssetID != characterAssetID || voice.VoiceKey != "alloy" || !voice.IsDefault {
		t.Fatalf("voice = %+v", voice)
	}
	var updatedVoice CharacterVoiceProfile
	doAPISuccess(t, handler, http.MethodPatch, "/api/projects/"+project.ID+"/character-voices/"+voice.ID, owner.AccessToken, owner.OrganizationID, map[string]any{
		"displayName": "冷静成年男声", "voiceKey": "echo",
	}, &updatedVoice)
	if updatedVoice.DisplayName != "冷静成年男声" || updatedVoice.VoiceKey != "echo" {
		t.Fatalf("updated voice = %+v", updatedVoice)
	}
	var narratorVoice CharacterVoiceProfile
	doAPISuccess(t, handler, http.MethodPost, "/api/projects/"+project.ID+"/character-voices", owner.AccessToken, owner.OrganizationID, map[string]any{
		"characterName": "旁白", "displayName": "默认旁白", "language": "zh-CN",
		"modelProfileKey": "tts_generation_default", "voiceKey": "nova", "isDefault": true,
	}, &narratorVoice)
	if !narratorVoice.IsDefault {
		t.Fatalf("narrator voice should be default: %+v", narratorVoice)
	}
	var activeVoices struct {
		Items []CharacterVoiceProfile `json:"items"`
	}
	doAPISuccess(t, handler, http.MethodGet, "/api/projects/"+project.ID+"/character-voices", owner.AccessToken, owner.OrganizationID, nil, &activeVoices)
	if len(activeVoices.Items) != 2 || !activeVoices.Items[0].IsDefault || activeVoices.Items[0].ID != narratorVoice.ID || activeVoices.Items[1].IsDefault {
		t.Fatalf("active voices = %+v", activeVoices.Items)
	}
	deleteNarrator := doAPIRequest(t, handler, http.MethodDelete, "/api/projects/"+project.ID+"/character-voices/"+narratorVoice.ID, owner.AccessToken, owner.OrganizationID, nil)
	if deleteNarrator.Code != http.StatusNoContent {
		t.Fatalf("delete narrator status = %d body=%s", deleteNarrator.Code, deleteNarrator.Body.String())
	}
	activeVoices.Items = nil
	doAPISuccess(t, handler, http.MethodGet, "/api/projects/"+project.ID+"/character-voices", owner.AccessToken, owner.OrganizationID, nil, &activeVoices)
	if len(activeVoices.Items) != 1 || activeVoices.Items[0].ID != voice.ID || !activeVoices.Items[0].IsDefault {
		t.Fatalf("default voice was not promoted after archive: %+v", activeVoices.Items)
	}

	episodeID := seedAudioAPIScriptEpisode(t, ctx, pool, owner.OrganizationID, project.ID, owner.User.ID)
	var workflowRun WorkflowRun
	doAPISuccess(t, handler, http.MethodPost, "/api/projects/"+project.ID+"/script-episodes/"+episodeID+"/audio/produce", owner.AccessToken, owner.OrganizationID, map[string]any{
		"maxConcurrency": 5, "mixAfterTts": true,
	}, &workflowRun)
	if workflowRun.Status != "queued" || temporal.executeCount != 0 {
		t.Fatalf("workflow run=%+v temporal=%+v", workflowRun, temporal)
	}
	dispatchWorkflowStartsForTest(t, server)
	if temporal.executeCount != 1 || temporal.options.TaskQueue != workflows.AudioTaskQueue {
		t.Fatalf("workflow run=%+v temporal=%+v", workflowRun, temporal)
	}
	if len(temporal.args) != 1 {
		t.Fatalf("temporal args = %#v", temporal.args)
	}
	workflowInput, ok := temporal.args[0].(workflows.EpisodeAudioProductionInput)
	if !ok || workflowInput.ScriptEpisodeID != episodeID || workflowInput.MaxConcurrency != 5 || !workflowInput.MixAfterTTS {
		t.Fatalf("workflow input = %#v", temporal.args[0])
	}

	deleteRecorder := doAPIRequest(t, handler, http.MethodDelete, "/api/projects/"+project.ID+"/character-voices/"+voice.ID, owner.AccessToken, owner.OrganizationID, nil)
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete voice status = %d body=%s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	activeVoices.Items = nil
	doAPISuccess(t, handler, http.MethodGet, "/api/projects/"+project.ID+"/character-voices", owner.AccessToken, owner.OrganizationID, nil, &activeVoices)
	if len(activeVoices.Items) != 0 {
		t.Fatalf("active voices after archive = %+v", activeVoices.Items)
	}
	var archivedVoices struct {
		Items []CharacterVoiceProfile `json:"items"`
	}
	doAPISuccess(t, handler, http.MethodGet, "/api/projects/"+project.ID+"/character-voices?filter%5Bstatus%5D=archived", owner.AccessToken, owner.OrganizationID, nil, &archivedVoices)
	if len(archivedVoices.Items) != 2 || archivedVoices.Items[0].Status != "archived" || archivedVoices.Items[1].Status != "archived" {
		t.Fatalf("archived voices = %+v", archivedVoices.Items)
	}
}

func TestCharacterVoiceGenerationChangeInvalidatesAudioDownstream(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run audio API integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for audio API integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)
	authService := auth.NewService(pool, "audio-stale-test-secret", time.Hour, 24*time.Hour)
	vault, err := provider.NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	server := New(pool, authService, provider.NewService(pool, vault), nil, nil)
	handler := server.Handler()
	suffix := uuid.NewString()
	owner, err := authService.Register(ctx, auth.RegisterRequest{
		Email: "audio-stale-" + suffix + "@example.test", Username: randomStorageSegment(), Password: "Password123!", DisplayName: "Audio Stale",
		OrganizationName: "Audio Stale Org " + suffix,
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, owner.OrganizationID)
	})
	workspaceID := firstWorkspaceID(t, ctx, pool, owner.OrganizationID)
	var project Project
	doAPISuccess(t, handler, http.MethodPost, "/api/projects", owner.AccessToken, owner.OrganizationID, map[string]any{
		"workspaceId": workspaceID, "name": "Audio Stale Project", "settings": map[string]any{},
	}, &project)
	var voice CharacterVoiceProfile
	doAPISuccess(t, handler, http.MethodPost, "/api/projects/"+project.ID+"/character-voices", owner.AccessToken, owner.OrganizationID, map[string]any{
		"characterName": "旁白", "displayName": "默认旁白", "voiceKey": "alloy", "isDefault": true,
	}, &voice)
	var revisionAfterCreate int
	if err := pool.QueryRow(ctx, `SELECT audio_configuration_revision FROM projects WHERE id = $1`, project.ID).Scan(&revisionAfterCreate); err != nil {
		t.Fatalf("load project audio revision: %v", err)
	}

	fixture := seedAudioAPIScriptFixture(t, ctx, pool, owner.OrganizationID, project.ID, owner.User.ID)
	var timingAnalysisID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO script_timing_analyses(
			organization_id, project_id, script_id, script_version_id, script_episode_id, revision, status,
			estimated_duration_ticks, minimum_duration_ticks, timeline_timebase, fps_numerator, fps_denominator,
			method_version, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, 1, 'ready', 180000, 180000, 90000, 24, 1, 'tts-actual-v1',
		        jsonb_build_object('audioConfigurationRevision', $7::integer), $6)
		RETURNING id::text
	`, owner.OrganizationID, project.ID, fixture.ScriptID, fixture.VersionID, fixture.EpisodeID, owner.User.ID, revisionAfterCreate).Scan(&timingAnalysisID); err != nil {
		t.Fatalf("insert timing analysis: %v", err)
	}
	var timingUnitID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO script_timing_units(
			timing_analysis_id, unit_ordinal, unit_type, track, speaker, source_text,
			start_tick, end_tick, min_duration_ticks, max_duration_ticks, duration_source, confidence, metadata
		)
		VALUES ($1, 0, 'narration', 'audio', '旁白', '测试台词', 0, 180000, 180000, 180000, 'tts_actual', 1, '{}')
		RETURNING id::text
	`, timingAnalysisID).Scan(&timingUnitID); err != nil {
		t.Fatalf("insert timing unit: %v", err)
	}
	var clipID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO tts_audio_clips(
			organization_id, project_id, script_episode_id, timing_analysis_id, timing_unit_id,
			character_voice_profile_id, model_profile_key, source_text, speaker, language, voice_key,
			output_format, status, revision, audio_configuration_revision, active, duration_ticks,
			timeline_timebase, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, 'tts_generation_default', '测试台词', '旁白', 'zh-CN',
		        'alloy', 'wav', 'succeeded', 1, $7, true, 180000, 90000, '{}', $8)
		RETURNING id::text
	`, owner.OrganizationID, project.ID, fixture.EpisodeID, timingAnalysisID, timingUnitID, voice.ID, revisionAfterCreate, owner.User.ID).Scan(&clipID); err != nil {
		t.Fatalf("insert tts clip: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE script_timing_units SET source_tts_audio_clip_id = $2 WHERE id = $1`, timingUnitID, clipID); err != nil {
		t.Fatalf("link tts clip: %v", err)
	}
	var mixID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO audio_mix_versions(
			organization_id, project_id, script_episode_id, timing_analysis_id, revision,
			audio_configuration_revision, status, active, audio_strategy, timeline_timebase,
			duration_ticks, production_readiness, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, 1, $5, 'ready', true, 'tts_postdub', 90000, 180000, 'ready', '{}', $6)
		RETURNING id::text
	`, owner.OrganizationID, project.ID, fixture.EpisodeID, timingAnalysisID, revisionAfterCreate, owner.User.ID).Scan(&mixID); err != nil {
		t.Fatalf("insert audio mix: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE projects SET active_audio_mix_version_id = $2 WHERE id = $1`, project.ID, mixID); err != nil {
		t.Fatalf("activate audio mix: %v", err)
	}
	var storyboardPlanID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO storyboard_plans(
			organization_id, project_id, script_id, script_version_id, script_episode_id, timing_analysis_id,
			revision, status, pacing_profile, target_duration_ticks, estimated_shot_count, actual_shot_count,
			active, stale_state, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, 1, 'ready', '{}', 180000, 1, 1, true, 'fresh', '{}', $7)
		RETURNING id::text
	`, owner.OrganizationID, project.ID, fixture.ScriptID, fixture.VersionID, fixture.EpisodeID, timingAnalysisID, owner.User.ID).Scan(&storyboardPlanID); err != nil {
		t.Fatalf("insert storyboard plan: %v", err)
	}

	var displayOnly CharacterVoiceProfile
	doAPISuccess(t, handler, http.MethodPatch, "/api/projects/"+project.ID+"/character-voices/"+voice.ID, owner.AccessToken, owner.OrganizationID, map[string]any{
		"displayName": "旁白显示名",
	}, &displayOnly)
	var revisionAfterDisplay int
	_ = pool.QueryRow(ctx, `SELECT audio_configuration_revision FROM projects WHERE id = $1`, project.ID).Scan(&revisionAfterDisplay)
	if revisionAfterDisplay != revisionAfterCreate {
		t.Fatalf("display-only update changed audio revision: before=%d after=%d", revisionAfterCreate, revisionAfterDisplay)
	}

	var changedVoice CharacterVoiceProfile
	doAPISuccess(t, handler, http.MethodPatch, "/api/projects/"+project.ID+"/character-voices/"+voice.ID, owner.AccessToken, owner.OrganizationID, map[string]any{
		"voiceKey": "nova",
	}, &changedVoice)
	var projectRevision int
	var activeMixID *string
	if err := pool.QueryRow(ctx, `SELECT audio_configuration_revision, active_audio_mix_version_id::text FROM projects WHERE id = $1`, project.ID).Scan(&projectRevision, &activeMixID); err != nil {
		t.Fatalf("load invalidated project: %v", err)
	}
	if projectRevision != revisionAfterCreate+1 || activeMixID != nil {
		t.Fatalf("project revision=%d activeMix=%v", projectRevision, activeMixID)
	}
	var clipStatus string
	var clipActive bool
	var unitClipID *string
	if err := pool.QueryRow(ctx, `
		SELECT clip.status, clip.active, unit.source_tts_audio_clip_id::text
		FROM tts_audio_clips clip JOIN script_timing_units unit ON unit.id = clip.timing_unit_id
		WHERE clip.id = $1
	`, clipID).Scan(&clipStatus, &clipActive, &unitClipID); err != nil {
		t.Fatalf("load invalidated clip: %v", err)
	}
	if clipStatus != "stale" || clipActive || unitClipID != nil {
		t.Fatalf("clip status=%s active=%v unitClip=%v", clipStatus, clipActive, unitClipID)
	}
	var mixStatus, timingStatus, storyboardStatus, storyboardStale string
	var mixActive, storyboardActive bool
	if err := pool.QueryRow(ctx, `SELECT status, active FROM audio_mix_versions WHERE id = $1`, mixID).Scan(&mixStatus, &mixActive); err != nil {
		t.Fatalf("load invalidated mix: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM script_timing_analyses WHERE id = $1`, timingAnalysisID).Scan(&timingStatus); err != nil {
		t.Fatalf("load invalidated timing: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT status, active, stale_state FROM storyboard_plans WHERE id = $1`, storyboardPlanID).Scan(&storyboardStatus, &storyboardActive, &storyboardStale); err != nil {
		t.Fatalf("load invalidated storyboard: %v", err)
	}
	if mixStatus != "stale" || mixActive || timingStatus != "archived" || storyboardStatus != "archived" || storyboardActive || storyboardStale != "upstream_changed" {
		t.Fatalf("mix=%s/%v timing=%s storyboard=%s/%v/%s", mixStatus, mixActive, timingStatus, storyboardStatus, storyboardActive, storyboardStale)
	}
}

type audioAPIScriptFixture struct {
	ScriptID, VersionID, EpisodeID string
}

func seedAudioAPIScriptFixture(t *testing.T, ctx context.Context, pool dbQueryer, organizationID, projectID, userID string) audioAPIScriptFixture {
	t.Helper()
	var fixture audioAPIScriptFixture
	if err := pool.QueryRow(ctx, `INSERT INTO scripts(organization_id, project_id, title, status, created_by) VALUES ($1, $2, 'Audio Script', 'draft', $3) RETURNING id::text`, organizationID, projectID, userID).Scan(&fixture.ScriptID); err != nil {
		t.Fatalf("insert script: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO script_versions(organization_id, project_id, script_id, version_no, version, content, content_format, status, metadata, created_by) VALUES ($1, $2, $3, 1, 1, '角色：台词', 'markdown', 'active', '{}', $4) RETURNING id::text`, organizationID, projectID, fixture.ScriptID, userID).Scan(&fixture.VersionID); err != nil {
		t.Fatalf("insert script version: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE scripts SET current_version_id = $2, status = 'active' WHERE id = $1`, fixture.ScriptID, fixture.VersionID); err != nil {
		t.Fatalf("activate script: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO script_episodes(organization_id, project_id, script_id, script_version_id, episode_index, episode_title, content, content_format, metadata, created_by) VALUES ($1, $2, $3, $4, 1, '第一集', '角色：台词', 'markdown', '{}', $5) RETURNING id::text`, organizationID, projectID, fixture.ScriptID, fixture.VersionID, userID).Scan(&fixture.EpisodeID); err != nil {
		t.Fatalf("insert script episode: %v", err)
	}
	return fixture
}

func seedAudioAPIScriptEpisode(t *testing.T, ctx context.Context, pool dbQueryer, organizationID, projectID, userID string) string {
	return seedAudioAPIScriptFixture(t, ctx, pool, organizationID, projectID, userID).EpisodeID
}
