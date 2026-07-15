package workflows

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Einzieg/cineweave/internal/provider"
)

func TestGenerateTTSAudioDoesNotActivateResultAfterConfigurationChange(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	orgID, userID, projectID, workflowRunID, providerModelID, _ := seedWorkflowGatewayIntegrationData(t, ctx, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})
	if _, err := pool.Exec(ctx, `UPDATE projects SET audio_configuration_revision = 1 WHERE id = $1`, projectID); err != nil {
		t.Fatalf("initialize project audio configuration revision: %v", err)
	}

	var scriptID, versionID, episodeID, analysisID, unitID, voiceID, clipID string
	if err := pool.QueryRow(ctx, `INSERT INTO scripts(organization_id, project_id, title, status, created_by) VALUES ($1, $2, 'TTS Race', 'draft', $3) RETURNING id::text`, orgID, projectID, userID).Scan(&scriptID); err != nil {
		t.Fatalf("insert script: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO script_versions(organization_id, project_id, script_id, version_no, version, content, content_format, status, metadata, created_by) VALUES ($1, $2, $3, 1, 1, '旁白：测试', 'markdown', 'active', '{}', $4) RETURNING id::text`, orgID, projectID, scriptID, userID).Scan(&versionID); err != nil {
		t.Fatalf("insert script version: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE scripts SET current_version_id = $2, status = 'active' WHERE id = $1`, scriptID, versionID); err != nil {
		t.Fatalf("activate script: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO script_episodes(organization_id, project_id, script_id, script_version_id, episode_index, episode_title, content, content_format, metadata, created_by) VALUES ($1, $2, $3, $4, 1, '第一集', '旁白：测试', 'markdown', '{}', $5) RETURNING id::text`, orgID, projectID, scriptID, versionID, userID).Scan(&episodeID); err != nil {
		t.Fatalf("insert script episode: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO script_timing_analyses(
			organization_id, project_id, script_id, script_version_id, script_episode_id, revision, status,
			estimated_duration_ticks, minimum_duration_ticks, timeline_timebase, fps_numerator, fps_denominator,
			method_version, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, 1, 'ready', 90000, 90000, 90000, 24, 1, 'rules-v1', '{}', $6)
		RETURNING id::text
	`, orgID, projectID, scriptID, versionID, episodeID, userID).Scan(&analysisID); err != nil {
		t.Fatalf("insert timing analysis: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO script_timing_units(
			timing_analysis_id, unit_ordinal, unit_type, track, speaker, source_text,
			start_tick, end_tick, min_duration_ticks, max_duration_ticks, duration_source, confidence, metadata
		)
		VALUES ($1, 0, 'narration', 'audio', '旁白', '测试', 0, 90000, 90000, 90000, 'rule_estimated', 1, '{}')
		RETURNING id::text
	`, analysisID).Scan(&unitID); err != nil {
		t.Fatalf("insert timing unit: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO character_voice_profiles(
			organization_id, project_id, character_name, display_name, model_profile_key,
			voice_key, is_default, status, parameters, metadata, created_by
		)
		VALUES ($1, $2, '旁白', '默认旁白', 'tts_generation_default', 'alloy', true, 'active', '{}', '{}', $3)
		RETURNING id::text
	`, orgID, projectID, userID).Scan(&voiceID); err != nil {
		t.Fatalf("insert voice: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO tts_audio_clips(
			organization_id, project_id, script_episode_id, timing_analysis_id, timing_unit_id,
			character_voice_profile_id, workflow_run_id, model_profile_key, source_text, speaker,
			language, voice_key, output_format, status, revision, audio_configuration_revision,
			active, timeline_timebase, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'tts_generation_default', '测试', '旁白',
		        'zh-CN', 'alloy', 'wav', 'queued', 1, 1, false, 90000,
		        '{"voiceParameters":{},"instructions":""}', $8)
		RETURNING id::text
	`, orgID, projectID, episodeID, analysisID, unitID, voiceID, workflowRunID, userID).Scan(&clipID); err != nil {
		t.Fatalf("insert tts clip: %v", err)
	}

	storageClient := newWorkflowMemoryStorage()
	storageKey := "tts/race.wav"
	wav := silentPCM16WAV(8000, 1)
	storageClient.mu.Lock()
	storageClient.objects[storageKey] = wav
	storageClient.mu.Unlock()
	var providerAccountID string
	if err := pool.QueryRow(ctx, `SELECT provider_account_id::text FROM provider_models WHERE id = $1`, providerModelID).Scan(&providerAccountID); err != nil {
		t.Fatalf("load provider account: %v", err)
	}

	gatewayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request provider.GatewayTTSRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var callID, artifactID, mediaFileID string
		if err := pool.QueryRow(r.Context(), `
			INSERT INTO provider_call_logs(
				organization_id, project_id, workflow_run_id, node_run_id, provider_account_id,
				provider_model_id, task_type, status, request_snapshot, artifact_ids, media_file_ids
			)
			VALUES ($1, $2, $3, $4, $5, $6, 'audio.tts', 'succeeded', '{}', '[]', '[]')
			RETURNING id::text
		`, orgID, projectID, workflowRunID, request.NodeRunID, providerAccountID, providerModelID).Scan(&callID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := pool.QueryRow(r.Context(), `
			INSERT INTO artifacts(organization_id, project_id, workflow_run_id, type, storage_key, mime_type, content_hash, metadata, created_by)
			VALUES ($1, $2, $3, 'tts_audio', $4, 'audio/wav', 'sha256:race', '{}', $5) RETURNING id::text
		`, orgID, projectID, workflowRunID, storageKey, userID).Scan(&artifactID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := pool.QueryRow(r.Context(), `
			INSERT INTO media_files(organization_id, project_id, artifact_id, storage_key, mime_type, byte_size, checksum, metadata, created_by)
			VALUES ($1, $2, $3, $4, 'audio/wav', $5, 'sha256:race', '{}', $6) RETURNING id::text
		`, orgID, projectID, artifactID, storageKey, len(wav), userID).Scan(&mediaFileID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := pool.Exec(r.Context(), `UPDATE projects SET audio_configuration_revision = 2 WHERE id = $1`, projectID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": provider.GatewayTTSResponse{
			ProviderCallID: callID, ModelID: providerModelID, Status: "succeeded",
			Output: provider.GatewayAudioOutput{ArtifactID: artifactID, MediaFileID: mediaFileID, StorageKey: storageKey, MimeType: "audio/wav", ByteSize: int64(len(wav))},
		}})
	}))
	defer gatewayServer.Close()

	activities := NewActivities(pool, storageClient, &provider.GatewayClient{BaseURL: gatewayServer.URL, Client: gatewayServer.Client()})
	output, err := activities.GenerateTTSAudio(ctx, GenerateTTSAudioInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID, CreatedBy: userID,
		ClipID: clipID, AudioConfigurationRevision: 1,
	})
	if err != nil {
		t.Fatalf("generate TTS audio: %v", err)
	}
	if output.Status != "stale" || output.ErrorCode != codeAudioConfigurationChanged || output.ArtifactID == "" || output.MediaFileID == "" {
		t.Fatalf("output = %+v", output)
	}
	var status string
	var active bool
	var artifactID, mediaFileID *string
	if err := pool.QueryRow(ctx, `SELECT status, active, artifact_id::text, media_file_id::text FROM tts_audio_clips WHERE id = $1`, clipID).Scan(&status, &active, &artifactID, &mediaFileID); err != nil {
		t.Fatalf("load TTS clip: %v", err)
	}
	if status != "stale" || active || artifactID == nil || mediaFileID == nil {
		t.Fatalf("clip status=%s active=%v artifact=%v media=%v", status, active, artifactID, mediaFileID)
	}
	var calibrationCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM timing_calibration_samples WHERE project_id = $1`, projectID).Scan(&calibrationCount); err != nil {
		t.Fatalf("load calibration samples: %v", err)
	}
	if calibrationCount != 0 {
		t.Fatalf("stale TTS result wrote %d calibration samples", calibrationCount)
	}
	mixClips, err := activities.episodeTTSAudioMixClips(ctx, ComposeEpisodeAudioMixInput{
		OrganizationID: orgID, ProjectID: projectID, ScriptEpisodeID: episodeID, TimingAnalysisID: analysisID,
	}, 90_000, 2)
	if err != nil {
		t.Fatalf("resolve current-revision mix clips: %v", err)
	}
	if len(mixClips) != 0 {
		t.Fatalf("stale revision contributed %d clips to the current mix", len(mixClips))
	}
}

func silentPCM16WAV(sampleRate, seconds int) []byte {
	sampleCount := sampleRate * seconds
	dataSize := sampleCount * 2
	buffer := bytes.NewBuffer(make([]byte, 0, 44+dataSize))
	buffer.WriteString("RIFF")
	_ = binary.Write(buffer, binary.LittleEndian, uint32(36+dataSize))
	buffer.WriteString("WAVEfmt ")
	_ = binary.Write(buffer, binary.LittleEndian, uint32(16))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(1))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(1))
	_ = binary.Write(buffer, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(buffer, binary.LittleEndian, uint32(sampleRate*2))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(2))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(16))
	buffer.WriteString("data")
	_ = binary.Write(buffer, binary.LittleEndian, uint32(dataSize))
	buffer.Write(make([]byte, dataSize))
	return buffer.Bytes()
}
