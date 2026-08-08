package api

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/workflows"
)

type CharacterVoiceProfile struct {
	ID                   string          `json:"id"`
	OrganizationID       string          `json:"organizationId"`
	ProjectID            string          `json:"projectId"`
	CanonicalAssetID     *string         `json:"canonicalAssetId,omitempty"`
	CharacterName        string          `json:"characterName"`
	DisplayName          string          `json:"displayName"`
	Language             string          `json:"language"`
	ModelProfileKey      string          `json:"modelProfileKey"`
	ProviderModelID      *string         `json:"providerModelId,omitempty"`
	VoiceKey             string          `json:"voiceKey"`
	Instructions         *string         `json:"instructions,omitempty"`
	ReferenceArtifactID  *string         `json:"referenceArtifactId,omitempty"`
	ReferenceMediaFileID *string         `json:"referenceMediaFileId,omitempty"`
	Parameters           json.RawMessage `json:"parameters"`
	IsDefault            bool            `json:"isDefault"`
	Status               string          `json:"status"`
	Metadata             json.RawMessage `json:"metadata"`
	Revision             int64           `json:"revision"`
	CreatedBy            *string         `json:"createdBy,omitempty"`
	CreatedAt            time.Time       `json:"createdAt"`
	UpdatedAt            time.Time       `json:"updatedAt"`
}

type TTSAudioClip struct {
	ID                         string          `json:"id"`
	TimingAnalysisID           string          `json:"timingAnalysisId"`
	TimingUnitID               string          `json:"timingUnitId"`
	AppliedTimingAnalysisID    *string         `json:"appliedTimingAnalysisId,omitempty"`
	VoiceProfileID             *string         `json:"voiceProfileId,omitempty"`
	ProviderModelID            *string         `json:"providerModelId,omitempty"`
	ProviderCallID             *string         `json:"providerCallId,omitempty"`
	SourceText                 string          `json:"sourceText"`
	Speaker                    *string         `json:"speaker,omitempty"`
	Language                   string          `json:"language"`
	VoiceKey                   string          `json:"voiceKey"`
	OutputFormat               string          `json:"outputFormat"`
	Status                     string          `json:"status"`
	Revision                   int             `json:"revision"`
	AudioConfigurationRevision int             `json:"audioConfigurationRevision"`
	Active                     bool            `json:"active"`
	ArtifactID                 *string         `json:"artifactId,omitempty"`
	MediaFileID                *string         `json:"mediaFileId,omitempty"`
	StorageKey                 *string         `json:"storageKey,omitempty"`
	PreviewURL                 *string         `json:"previewUrl,omitempty"`
	MimeType                   *string         `json:"mimeType,omitempty"`
	SampleRate                 *int            `json:"sampleRate,omitempty"`
	SampleCount                *int64          `json:"sampleCount,omitempty"`
	ChannelCount               *int            `json:"channelCount,omitempty"`
	DurationTicks              *int64          `json:"durationTicks,omitempty"`
	TimelineTimebase           int64           `json:"timelineTimebase"`
	DurationSeconds            *float64        `json:"durationSeconds,omitempty"`
	ErrorCode                  *string         `json:"errorCode,omitempty"`
	ErrorMessage               *string         `json:"errorMessage,omitempty"`
	Metadata                   json.RawMessage `json:"metadata"`
	CreatedAt                  time.Time       `json:"createdAt"`
	UpdatedAt                  time.Time       `json:"updatedAt"`
	CompletedAt                *time.Time      `json:"completedAt,omitempty"`
}

type AudioMixVersion struct {
	ID                         string          `json:"id"`
	ScriptEpisodeID            *string         `json:"scriptEpisodeId,omitempty"`
	StoryboardPlanID           *string         `json:"storyboardPlanId,omitempty"`
	TimingAnalysisID           *string         `json:"timingAnalysisId,omitempty"`
	WorkflowRunID              *string         `json:"workflowRunId,omitempty"`
	Revision                   int             `json:"revision"`
	AudioConfigurationRevision int             `json:"audioConfigurationRevision"`
	Status                     string          `json:"status"`
	Active                     bool            `json:"active"`
	AudioStrategy              string          `json:"audioStrategy"`
	TimelineTimebase           int64           `json:"timelineTimebase"`
	DurationTicks              *int64          `json:"durationTicks,omitempty"`
	DurationSeconds            *float64        `json:"durationSeconds,omitempty"`
	SampleRate                 int             `json:"sampleRate"`
	ChannelCount               int             `json:"channelCount"`
	ArtifactID                 *string         `json:"artifactId,omitempty"`
	MediaFileID                *string         `json:"mediaFileId,omitempty"`
	StorageKey                 *string         `json:"storageKey,omitempty"`
	PreviewURL                 *string         `json:"previewUrl,omitempty"`
	MimeType                   *string         `json:"mimeType,omitempty"`
	ProductionReadiness        string          `json:"productionReadiness"`
	TrackSummary               json.RawMessage `json:"trackSummary"`
	Metadata                   json.RawMessage `json:"metadata"`
	CreatedAt                  time.Time       `json:"createdAt"`
	UpdatedAt                  time.Time       `json:"updatedAt"`
	CompletedAt                *time.Time      `json:"completedAt,omitempty"`
}

type NativeAudioReview struct {
	ID                         string          `json:"id"`
	VideoRenderPlanID          string          `json:"videoRenderPlanId"`
	VideoRenderSegmentID       string          `json:"videoRenderSegmentId"`
	WorkflowRunID              *string         `json:"workflowRunId,omitempty"`
	ProviderCallID             *string         `json:"providerCallId,omitempty"`
	ProviderModelID            *string         `json:"providerModelId,omitempty"`
	Revision                   int             `json:"revision"`
	AudioConfigurationRevision int             `json:"audioConfigurationRevision"`
	Status                     string          `json:"status"`
	ExpectedDialogue           json.RawMessage `json:"expectedDialogue"`
	Transcript                 *string         `json:"transcript,omitempty"`
	Language                   *string         `json:"language,omitempty"`
	Alignment                  json.RawMessage `json:"alignment"`
	DialogueCoverage           *float64        `json:"dialogueCoverage,omitempty"`
	TextAccuracy               *float64        `json:"textAccuracy,omitempty"`
	TimingAccuracy             *float64        `json:"timingAccuracy,omitempty"`
	SpeakerTurnAccuracy        *float64        `json:"speakerTurnAccuracy,omitempty"`
	ErrorCode                  *string         `json:"errorCode,omitempty"`
	ErrorMessage               *string         `json:"errorMessage,omitempty"`
	Metadata                   json.RawMessage `json:"metadata"`
	CreatedAt                  time.Time       `json:"createdAt"`
	UpdatedAt                  time.Time       `json:"updatedAt"`
	CompletedAt                *time.Time      `json:"completedAt,omitempty"`
}

func (s *Server) listCharacterVoices(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("filter[status]")))
	if status == "" {
		status = "active"
	}
	if status != "active" && status != "archived" && status != "all" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "filter[status] must be active, archived, or all", nil, false)
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT `+characterVoiceSelectColumns+`
		FROM character_voice_profiles
		WHERE project_id = $1 AND ($2 = 'all' OR status = $2)
		ORDER BY status, is_default DESC, character_name, created_at
	`, project.ID, status)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]CharacterVoiceProfile, 0)
	for rows.Next() {
		var item CharacterVoiceProfile
		if err := scanCharacterVoice(rows, &item); err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

type characterVoiceRequest struct {
	ExpectedRevision     int64           `json:"expectedRevision"`
	CanonicalAssetID     *string         `json:"canonicalAssetId"`
	CharacterName        *string         `json:"characterName"`
	DisplayName          *string         `json:"displayName"`
	Language             *string         `json:"language"`
	ModelProfileKey      *string         `json:"modelProfileKey"`
	ProviderModelID      *string         `json:"providerModelId"`
	VoiceKey             *string         `json:"voiceKey"`
	Instructions         *string         `json:"instructions"`
	ReferenceArtifactID  *string         `json:"referenceArtifactId"`
	ReferenceMediaFileID *string         `json:"referenceMediaFileId"`
	Parameters           json.RawMessage `json:"parameters"`
	IsDefault            *bool           `json:"isDefault"`
}

func (s *Server) createCharacterVoice(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	var req characterVoiceRequest
	if !decode(w, r, &req) {
		return
	}
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "character_voice.create", mustRawJSON(characterVoiceCreateActionInput{
			CanonicalAssetID: trimmedStringPtr(req.CanonicalAssetID), CharacterName: trimmedStringPtr(req.CharacterName),
			DisplayName: trimmedStringPtr(req.DisplayName), Language: trimmedStringPtr(req.Language),
			ModelProfileKey: trimmedStringPtr(req.ModelProfileKey), ProviderModelID: trimmedStringPtr(req.ProviderModelID),
			VoiceKey: trimmedStringPtr(req.VoiceKey), Instructions: trimmedStringPtr(req.Instructions),
			ReferenceArtifactID: trimmedStringPtr(req.ReferenceArtifactID), ReferenceMediaFileID: trimmedStringPtr(req.ReferenceMediaFileID),
			Parameters: req.Parameters, IsDefault: req.IsDefault,
		}), idempotencyKey(r, ""),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := decodeAgentToolResultValue[CharacterVoiceProfile](result, "voice")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) updateCharacterVoice(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	var req characterVoiceRequest
	if !decode(w, r, &req) {
		return
	}
	var parameters *json.RawMessage
	if len(req.Parameters) > 0 {
		value := req.Parameters
		parameters = &value
	}
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "character_voice.update", mustRawJSON(characterVoiceUpdateActionInput{
			VoiceID: r.PathValue("voiceId"), ExpectedRevision: req.ExpectedRevision,
			Patch: characterVoicePatch{
				CanonicalAssetID: req.CanonicalAssetID, CharacterName: req.CharacterName, DisplayName: req.DisplayName,
				Language: req.Language, ModelProfileKey: req.ModelProfileKey, ProviderModelID: req.ProviderModelID,
				VoiceKey: req.VoiceKey, Instructions: req.Instructions, ReferenceArtifactID: req.ReferenceArtifactID,
				ReferenceMediaFileID: req.ReferenceMediaFileID, Parameters: parameters, IsDefault: req.IsDefault,
			},
		}), idempotencyKey(r, ""),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := decodeAgentToolResultValue[CharacterVoiceProfile](result, "voice")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) deleteCharacterVoice(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	if !decode(w, r, &req) {
		return
	}
	command, _, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "character_voice.delete", mustRawJSON(characterVoiceDeleteActionInput{
			VoiceID: r.PathValue("voiceId"), ExpectedRevision: req.ExpectedRevision,
		}), idempotencyKey(r, ""),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) characterVoice(ctx context.Context, projectID, voiceID string) (CharacterVoiceProfile, error) {
	var item CharacterVoiceProfile
	err := scanCharacterVoice(s.db.QueryRow(ctx, `
		SELECT `+characterVoiceSelectColumns+`
		FROM character_voice_profiles WHERE project_id = $1 AND id = $2
	`, projectID, voiceID), &item)
	return item, err
}

const characterVoiceSelectColumns = `id::text, organization_id::text, project_id::text, canonical_asset_id::text,
       character_name, display_name, language, model_profile_key, provider_model_id::text,
       voice_key, instructions, reference_artifact_id::text, reference_media_file_id::text,
       parameters, is_default, status, metadata, revision, created_by::text, created_at, updated_at`

type characterVoiceScanner interface {
	Scan(dest ...any) error
}

func scanCharacterVoice(scanner characterVoiceScanner, item *CharacterVoiceProfile) error {
	return scanner.Scan(&item.ID, &item.OrganizationID, &item.ProjectID, &item.CanonicalAssetID,
		&item.CharacterName, &item.DisplayName, &item.Language, &item.ModelProfileKey, &item.ProviderModelID,
		&item.VoiceKey, &item.Instructions, &item.ReferenceArtifactID, &item.ReferenceMediaFileID,
		&item.Parameters, &item.IsDefault, &item.Status, &item.Metadata, &item.Revision,
		&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt)
}

func (s *Server) produceEpisodeAudio(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{authz.PermissionScriptWrite, authz.PermissionProjectWrite})
	if !ok {
		return
	}
	episode, err := s.scriptEpisode(r, project.ID, r.PathValue("episodeId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req struct {
		TimingAnalysisID      string                              `json:"timingAnalysisId"`
		DefaultVoiceProfileID string                              `json:"defaultVoiceProfileId"`
		Force                 bool                                `json:"force"`
		MaxConcurrency        int                                 `json:"maxConcurrency"`
		MixAfterTTS           *bool                               `json:"mixAfterTts"`
		AdditionalTracks      []workflows.AudioMixAdditionalTrack `json:"additionalTracks"`
	}
	if !decode(w, r, &req) {
		return
	}
	mixAfter := true
	if req.MixAfterTTS != nil {
		mixAfter = *req.MixAfterTTS
	}
	run, err := s.startTypedProjectWorkflow(r.Context(), principal, project, "episode_audio_production", map[string]any{
		"scriptEpisodeId": episode.ID, "timingAnalysisId": req.TimingAnalysisID, "force": req.Force,
		"maxConcurrency": req.MaxConcurrency, "mixAfterTts": mixAfter, "additionalTracks": req.AdditionalTracks,
	}, workflows.AudioTaskQueue, workflows.EpisodeAudioProductionWorkflow, func(run WorkflowRun) any {
		return workflows.EpisodeAudioProductionInput{
			OrganizationID: project.OrganizationID, ProjectID: project.ID, WorkflowRunID: run.ID, CreatedBy: principal.UserID,
			ScriptEpisodeID: episode.ID, TimingAnalysisID: req.TimingAnalysisID, DefaultVoiceProfileID: req.DefaultVoiceProfileID,
			Force: req.Force, MaxConcurrency: req.MaxConcurrency, MixAfterTTS: mixAfter, AdditionalTracks: req.AdditionalTracks,
		}
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, run, nil)
}

func (s *Server) getEpisodeAudio(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	episode, err := s.scriptEpisode(r, project.ID, r.PathValue("episodeId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	clips, err := s.listTTSAudioClips(r.Context(), project.ID, episode.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	mixes, err := s.listAudioMixVersions(r.Context(), project.ID, episode.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"clips": clips, "mixes": mixes}, nil)
}

func (s *Server) listTTSAudioClips(ctx context.Context, projectID, episodeID string) ([]TTSAudioClip, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text, timing_analysis_id::text, timing_unit_id::text, applied_timing_analysis_id::text,
		       character_voice_profile_id::text, provider_model_id::text, provider_call_id::text, source_text, speaker,
		       language, voice_key, output_format, status, revision, audio_configuration_revision, active, artifact_id::text, media_file_id::text,
		       storage_key, mime_type, sample_rate, sample_count, channel_count, duration_ticks, timeline_timebase,
		       error_code, error_message, metadata, created_at, updated_at, completed_at
		FROM tts_audio_clips WHERE project_id = $1 AND script_episode_id = $2 ORDER BY created_at DESC
	`, projectID, episodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]TTSAudioClip, 0)
	for rows.Next() {
		var item TTSAudioClip
		if err := rows.Scan(&item.ID, &item.TimingAnalysisID, &item.TimingUnitID, &item.AppliedTimingAnalysisID,
			&item.VoiceProfileID, &item.ProviderModelID, &item.ProviderCallID, &item.SourceText, &item.Speaker,
			&item.Language, &item.VoiceKey, &item.OutputFormat, &item.Status, &item.Revision, &item.AudioConfigurationRevision, &item.Active,
			&item.ArtifactID, &item.MediaFileID, &item.StorageKey, &item.MimeType, &item.SampleRate, &item.SampleCount,
			&item.ChannelCount, &item.DurationTicks, &item.TimelineTimebase, &item.ErrorCode, &item.ErrorMessage,
			&item.Metadata, &item.CreatedAt, &item.UpdatedAt, &item.CompletedAt); err != nil {
			return nil, err
		}
		if item.DurationTicks != nil && item.TimelineTimebase > 0 {
			seconds := float64(*item.DurationTicks) / float64(item.TimelineTimebase)
			item.DurationSeconds = &seconds
		}
		if item.StorageKey != nil {
			item.PreviewURL = s.previewURLForStorageKeyRequest(ctx, *item.StorageKey)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) listAudioMixVersions(ctx context.Context, projectID, episodeID string) ([]AudioMixVersion, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text, script_episode_id::text, storyboard_plan_id::text, timing_analysis_id::text, workflow_run_id::text,
		       revision, audio_configuration_revision, status, active, audio_strategy, timeline_timebase, duration_ticks, sample_rate, channel_count,
		       artifact_id::text, media_file_id::text, storage_key, mime_type, production_readiness,
		       track_summary, metadata, created_at, updated_at, completed_at
		FROM audio_mix_versions WHERE project_id = $1 AND script_episode_id = $2 ORDER BY revision DESC
	`, projectID, episodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AudioMixVersion, 0)
	for rows.Next() {
		var item AudioMixVersion
		if err := rows.Scan(&item.ID, &item.ScriptEpisodeID, &item.StoryboardPlanID, &item.TimingAnalysisID, &item.WorkflowRunID,
			&item.Revision, &item.AudioConfigurationRevision, &item.Status, &item.Active, &item.AudioStrategy, &item.TimelineTimebase, &item.DurationTicks,
			&item.SampleRate, &item.ChannelCount, &item.ArtifactID, &item.MediaFileID, &item.StorageKey, &item.MimeType,
			&item.ProductionReadiness, &item.TrackSummary, &item.Metadata, &item.CreatedAt, &item.UpdatedAt, &item.CompletedAt); err != nil {
			return nil, err
		}
		if item.DurationTicks != nil && item.TimelineTimebase > 0 {
			seconds := float64(*item.DurationTicks) / float64(item.TimelineTimebase)
			item.DurationSeconds = &seconds
		}
		if item.StorageKey != nil {
			item.PreviewURL = s.previewURLForStorageKeyRequest(ctx, *item.StorageKey)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) startNativeAudioReview(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{authz.PermissionStoryboardGenerate, authz.PermissionProjectWrite})
	if !ok {
		return
	}
	var req nativeAudioReviewRequest
	if !decode(w, r, &req) {
		return
	}
	req.ShotID = r.PathValue("shotId")
	run, _, err := s.startNativeAudioReviewCore(r.Context(), principal, project, req, "")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, run, nil)
}

type nativeAudioReviewRequest struct {
	ShotID            string `json:"shotId,omitempty"`
	VideoRenderPlanID string `json:"videoRenderPlanId"`
	MaxConcurrency    int    `json:"maxConcurrency"`
}

func (s *Server) startNativeAudioReviewCore(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	req nativeAudioReviewRequest,
	projectControlCommandID string,
) (WorkflowRun, bool, error) {
	req.ShotID = strings.TrimSpace(req.ShotID)
	if req.ShotID == "" {
		return WorkflowRun{}, false, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "shotId is required")
	}
	shot, err := s.storyboardShotByIDContext(ctx, project.ID, req.ShotID)
	if err != nil {
		return WorkflowRun{}, false, err
	}
	commandID := strings.TrimSpace(projectControlCommandID)
	if commandID != "" {
		existing, found, err := s.workflowRunForProjectControlCommand(ctx, project.ID, "native_audio_review", commandID)
		if err != nil || found {
			return existing, found, err
		}
	}
	requestInput := map[string]any{
		"storyboardShotId": shot.ID, "videoRenderPlanId": strings.TrimSpace(req.VideoRenderPlanID), "maxConcurrency": req.MaxConcurrency,
	}
	if commandID != "" {
		requestInput["projectControlCommandId"] = commandID
		requestInput["idempotencyKey"] = "project-control-command:" + commandID
	}
	run, err := s.startTypedProjectWorkflow(ctx, principal, project, "native_audio_review", requestInput, workflows.AudioTaskQueue, workflows.NativeAudioReviewWorkflow, func(run WorkflowRun) any {
		return workflows.NativeAudioReviewWorkflowInput{
			OrganizationID: project.OrganizationID, ProjectID: project.ID, WorkflowRunID: run.ID, CreatedBy: principal.UserID,
			StoryboardShotID: shot.ID, VideoRenderPlanID: strings.TrimSpace(req.VideoRenderPlanID), MaxConcurrency: req.MaxConcurrency,
		}
	})
	return run, false, err
}

func (s *Server) listNativeAudioReviews(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	shot, err := s.storyboardShotByID(r, project.ID, r.PathValue("shotId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT review.id::text, review.video_render_plan_id::text, review.video_render_segment_id::text,
		       review.workflow_run_id::text, review.provider_call_id::text, review.provider_model_id::text,
		       review.revision, review.audio_configuration_revision, review.status, review.expected_dialogue, review.transcript, review.language,
		       review.alignment, review.dialogue_coverage::float8, review.text_accuracy::float8,
		       review.timing_accuracy::float8, review.speaker_turn_accuracy::float8,
		       review.error_code, review.error_message, review.metadata, review.created_at, review.updated_at, review.completed_at
		FROM native_audio_reviews review JOIN video_render_segments segment ON segment.id = review.video_render_segment_id
		WHERE review.project_id = $1 AND segment.storyboard_shot_id = $2 ORDER BY review.created_at DESC
	`, project.ID, shot.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]NativeAudioReview, 0)
	for rows.Next() {
		var item NativeAudioReview
		if err := rows.Scan(&item.ID, &item.VideoRenderPlanID, &item.VideoRenderSegmentID, &item.WorkflowRunID,
			&item.ProviderCallID, &item.ProviderModelID, &item.Revision, &item.AudioConfigurationRevision, &item.Status, &item.ExpectedDialogue,
			&item.Transcript, &item.Language, &item.Alignment, &item.DialogueCoverage, &item.TextAccuracy,
			&item.TimingAccuracy, &item.SpeakerTurnAccuracy, &item.ErrorCode, &item.ErrorMessage,
			&item.Metadata, &item.CreatedAt, &item.UpdatedAt, &item.CompletedAt); err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) startTypedProjectWorkflow(ctx context.Context, principal auth.Principal, project Project, workflowType string, requestInput any, taskQueue string, workflowFunc any, buildInput func(WorkflowRun) any) (WorkflowRun, error) {
	runInput := json.RawMessage(mustMarshal(map[string]any{"prompt": "", "workflowType": workflowType, "input": requestInput}))
	return s.enqueueProjectWorkflow(ctx, principal, project, workflowType, runInput, taskQueue, workflowFunc, buildInput)
}

func trimmedStringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
func derefOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func audioJSONEquivalent(left, right json.RawMessage) bool {
	var leftValue, rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return string(left) == string(right)
	}
	return reflect.DeepEqual(leftValue, rightValue)
}
