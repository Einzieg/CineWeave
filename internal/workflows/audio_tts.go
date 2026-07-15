package workflows

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	mediapkg "github.com/Einzieg/cineweave/internal/media"
	"github.com/Einzieg/cineweave/internal/production"
	"github.com/Einzieg/cineweave/internal/provider"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	defaultTTSConcurrency         = 5
	nodePrepareEpisodeTTS         = "audio_tts_prepare"
	nodeTTSPrefix                 = "audio_tts_generate"
	codeAudioConfigurationChanged = "AUDIO_CONFIGURATION_CHANGED"
)

type EpisodeAudioProductionInput struct {
	OrganizationID        string                    `json:"organizationId"`
	ProjectID             string                    `json:"projectId"`
	WorkflowRunID         string                    `json:"workflowRunId"`
	CreatedBy             string                    `json:"createdBy"`
	ScriptEpisodeID       string                    `json:"scriptEpisodeId"`
	TimingAnalysisID      string                    `json:"timingAnalysisId,omitempty"`
	DefaultVoiceProfileID string                    `json:"defaultVoiceProfileId,omitempty"`
	Force                 bool                      `json:"force,omitempty"`
	MaxConcurrency        int                       `json:"maxConcurrency,omitempty"`
	MixAfterTTS           bool                      `json:"mixAfterTts"`
	AdditionalTracks      []AudioMixAdditionalTrack `json:"additionalTracks,omitempty"`
}

type PrepareEpisodeTTSInput struct {
	EpisodeAudioProductionInput
}

type ResolveEpisodeAudioTimingInput struct {
	OrganizationID  string `json:"organizationId"`
	ProjectID       string `json:"projectId"`
	ScriptEpisodeID string `json:"scriptEpisodeId"`
}

type ResolveEpisodeAudioTimingOutput struct {
	ScriptID         string `json:"scriptId"`
	ScriptVersionID  string `json:"scriptVersionId"`
	TimingAnalysisID string `json:"timingAnalysisId,omitempty"`
}

type TTSGenerationJob struct {
	ClipID         string `json:"clipId"`
	TimingUnitID   string `json:"timingUnitId"`
	VoiceProfileID string `json:"voiceProfileId"`
	UnitOrdinal    int    `json:"unitOrdinal"`
	Speaker        string `json:"speaker"`
	Text           string `json:"text"`
	Delivery       string `json:"delivery,omitempty"`
	Revision       int    `json:"revision"`
}

type PrepareEpisodeTTSOutput struct {
	TimingAnalysisID           string             `json:"timingAnalysisId"`
	ScriptID                   string             `json:"scriptId"`
	ScriptVersionID            string             `json:"scriptVersionId"`
	TimelineTimebase           int64              `json:"timelineTimebase"`
	FPSNumerator               int                `json:"fpsNumerator"`
	FPSDenominator             int                `json:"fpsDenominator"`
	AudioConfigurationRevision int                `json:"audioConfigurationRevision"`
	Jobs                       []TTSGenerationJob `json:"jobs"`
	ExistingClipIDs            []string           `json:"existingClipIds"`
}

type GenerateTTSAudioInput struct {
	OrganizationID             string `json:"organizationId"`
	ProjectID                  string `json:"projectId"`
	WorkflowRunID              string `json:"workflowRunId"`
	CreatedBy                  string `json:"createdBy"`
	ClipID                     string `json:"clipId"`
	AudioConfigurationRevision int    `json:"audioConfigurationRevision"`
}

type GenerateTTSAudioOutput struct {
	ClipID                     string  `json:"clipId"`
	TimingUnitID               string  `json:"timingUnitId"`
	Status                     string  `json:"status"`
	AudioConfigurationRevision int     `json:"audioConfigurationRevision"`
	ProviderCallID             string  `json:"providerCallId,omitempty"`
	ModelID                    string  `json:"modelId,omitempty"`
	ArtifactID                 string  `json:"artifactId,omitempty"`
	MediaFileID                string  `json:"mediaFileId,omitempty"`
	StorageKey                 string  `json:"storageKey,omitempty"`
	MimeType                   string  `json:"mimeType,omitempty"`
	SampleRate                 int     `json:"sampleRate,omitempty"`
	SampleCount                int64   `json:"sampleCount,omitempty"`
	ChannelCount               int     `json:"channelCount,omitempty"`
	DurationTicks              int64   `json:"durationTicks,omitempty"`
	DurationSeconds            float64 `json:"durationSeconds,omitempty"`
	ErrorCode                  string  `json:"errorCode,omitempty"`
	ErrorMessage               string  `json:"errorMessage,omitempty"`
}

type CreateTTSTimingRevisionInput struct {
	OrganizationID             string `json:"organizationId"`
	ProjectID                  string `json:"projectId"`
	WorkflowRunID              string `json:"workflowRunId"`
	CreatedBy                  string `json:"createdBy"`
	ScriptEpisodeID            string `json:"scriptEpisodeId"`
	SourceAnalysisID           string `json:"sourceAnalysisId"`
	AudioConfigurationRevision int    `json:"audioConfigurationRevision"`
}

type CreateTTSTimingRevisionOutput struct {
	SourceAnalysisID           string `json:"sourceAnalysisId"`
	TimingAnalysisID           string `json:"timingAnalysisId"`
	Revision                   int    `json:"revision"`
	EstimatedDurationTicks     int64  `json:"estimatedDurationTicks"`
	TimelineTimebase           int64  `json:"timelineTimebase"`
	TTSUnitCount               int    `json:"ttsUnitCount"`
	StaleStoryboardCount       int64  `json:"staleStoryboardCount"`
	AudioConfigurationRevision int    `json:"audioConfigurationRevision"`
}

type EpisodeAudioProductionOutput struct {
	Status                 string                         `json:"status"`
	ScriptEpisodeID        string                         `json:"scriptEpisodeId"`
	SourceTimingAnalysisID string                         `json:"sourceTimingAnalysisId"`
	TimingRevision         *CreateTTSTimingRevisionOutput `json:"timingRevision,omitempty"`
	SucceededClipIDs       []string                       `json:"succeededClipIds"`
	FailedClipIDs          []string                       `json:"failedClipIds"`
	ClipOutputs            []GenerateTTSAudioOutput       `json:"clipOutputs"`
	Errors                 map[string]string              `json:"errors"`
	Mix                    *ComposeEpisodeAudioMixOutput  `json:"mix,omitempty"`
}

func EpisodeAudioProductionWorkflow(ctx workflow.Context, input EpisodeAudioProductionInput) (EpisodeAudioProductionOutput, error) {
	options := defaultActivityOptions()
	options.StartToCloseTimeout = 30 * time.Minute
	options.RetryPolicy = &temporal.RetryPolicy{InitialInterval: time.Second, BackoffCoefficient: 2, MaximumInterval: 30 * time.Second, MaximumAttempts: 3}
	ctx = workflow.WithActivityOptions(ctx, options)
	var timingSource ResolveEpisodeAudioTimingOutput
	if err := workflow.ExecuteActivity(ctx, "ResolveEpisodeAudioTiming", ResolveEpisodeAudioTimingInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, ScriptEpisodeID: input.ScriptEpisodeID,
	}).Get(ctx, &timingSource); err != nil {
		return EpisodeAudioProductionOutput{}, err
	}
	if strings.TrimSpace(input.TimingAnalysisID) == "" {
		input.TimingAnalysisID = timingSource.TimingAnalysisID
	}
	if strings.TrimSpace(input.TimingAnalysisID) == "" {
		var analysis TimingAnalysisActivityOutput
		if err := workflow.ExecuteActivity(ctx, "AnalyzeEpisodeTiming", AnalyzeEpisodeTimingInput{
			OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
			CreatedBy: input.CreatedBy, ScriptID: timingSource.ScriptID, ScriptVersionID: timingSource.ScriptVersionID,
			ScriptEpisodeID: input.ScriptEpisodeID,
		}).Get(ctx, &analysis); err != nil {
			return EpisodeAudioProductionOutput{}, err
		}
		input.TimingAnalysisID = analysis.AnalysisID
	}
	var prepared PrepareEpisodeTTSOutput
	if err := workflow.ExecuteActivity(ctx, "PrepareEpisodeTTS", PrepareEpisodeTTSInput{EpisodeAudioProductionInput: input}).Get(ctx, &prepared); err != nil {
		return EpisodeAudioProductionOutput{}, err
	}
	output := EpisodeAudioProductionOutput{
		Status: "running", ScriptEpisodeID: input.ScriptEpisodeID, SourceTimingAnalysisID: prepared.TimingAnalysisID,
		SucceededClipIDs: append([]string(nil), prepared.ExistingClipIDs...), Errors: map[string]string{},
	}
	concurrency := input.MaxConcurrency
	if concurrency <= 0 {
		concurrency = defaultTTSConcurrency
	}
	if concurrency > 20 {
		concurrency = 20
	}
	for start := 0; start < len(prepared.Jobs); start += concurrency {
		end := start + concurrency
		if end > len(prepared.Jobs) {
			end = len(prepared.Jobs)
		}
		futures := make([]workflow.Future, 0, end-start)
		for _, job := range prepared.Jobs[start:end] {
			futures = append(futures, workflow.ExecuteActivity(ctx, "GenerateTTSAudio", GenerateTTSAudioInput{
				OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
				CreatedBy: input.CreatedBy, ClipID: job.ClipID, AudioConfigurationRevision: prepared.AudioConfigurationRevision,
			}))
		}
		for index, future := range futures {
			job := prepared.Jobs[start+index]
			var item GenerateTTSAudioOutput
			if err := future.Get(ctx, &item); err != nil {
				item = GenerateTTSAudioOutput{ClipID: job.ClipID, TimingUnitID: job.TimingUnitID, Status: "failed", ErrorCode: codeActivityFailed, ErrorMessage: err.Error()}
			}
			output.ClipOutputs = append(output.ClipOutputs, item)
			if item.Status == "succeeded" {
				output.SucceededClipIDs = append(output.SucceededClipIDs, item.ClipID)
			} else {
				output.FailedClipIDs = append(output.FailedClipIDs, item.ClipID)
				output.Errors[item.ClipID] = firstNonEmptyString(item.ErrorMessage, "TTS generation failed")
			}
		}
	}
	if len(output.FailedClipIDs) > 0 {
		if len(output.SucceededClipIDs) > 0 {
			output.Status = "partial_succeeded"
		} else {
			output.Status = "failed"
		}
		if err := workflow.ExecuteActivity(ctx, "CompleteEpisodeAudioProductionWorkflow", input, output).Get(ctx, nil); err != nil {
			return EpisodeAudioProductionOutput{}, err
		}
		return output, nil
	}
	var revision CreateTTSTimingRevisionOutput
	if err := workflow.ExecuteActivity(ctx, "CreateTTSTimingRevision", CreateTTSTimingRevisionInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		CreatedBy: input.CreatedBy, ScriptEpisodeID: input.ScriptEpisodeID, SourceAnalysisID: prepared.TimingAnalysisID,
		AudioConfigurationRevision: prepared.AudioConfigurationRevision,
	}).Get(ctx, &revision); err != nil {
		return EpisodeAudioProductionOutput{}, err
	}
	output.TimingRevision = &revision
	if input.MixAfterTTS {
		var mix ComposeEpisodeAudioMixOutput
		if err := workflow.ExecuteActivity(ctx, "ComposeEpisodeAudioMix", ComposeEpisodeAudioMixInput{
			OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
			CreatedBy: input.CreatedBy, ScriptEpisodeID: input.ScriptEpisodeID, TimingAnalysisID: revision.TimingAnalysisID,
			AudioConfigurationRevision: prepared.AudioConfigurationRevision,
			AdditionalTracks:           input.AdditionalTracks,
		}).Get(ctx, &mix); err != nil {
			return EpisodeAudioProductionOutput{}, err
		}
		if mix.Status == "" && mix.ProductionReadiness == "ready" {
			mix.Status = "ready"
		}
		output.Mix = &mix
		if mix.Status != "ready" {
			output.Status = "failed"
			output.Errors["audioMix"] = "音频配置已变更，本次混音结果未进入生产链路"
			if err := workflow.ExecuteActivity(ctx, "CompleteEpisodeAudioProductionWorkflow", input, output).Get(ctx, nil); err != nil {
				return EpisodeAudioProductionOutput{}, err
			}
			return output, nil
		}
	}
	output.Status = "succeeded"
	if err := workflow.ExecuteActivity(ctx, "RefreshTimingCalibrationProfile", RefreshTimingCalibrationProfileInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
	}).Get(ctx, nil); err != nil {
		return EpisodeAudioProductionOutput{}, err
	}
	if err := workflow.ExecuteActivity(ctx, "CompleteEpisodeAudioProductionWorkflow", input, output).Get(ctx, nil); err != nil {
		return EpisodeAudioProductionOutput{}, err
	}
	return output, nil
}

func (a Activities) ResolveEpisodeAudioTiming(ctx context.Context, input ResolveEpisodeAudioTimingInput) (ResolveEpisodeAudioTimingOutput, error) {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.ScriptEpisodeID) == "" {
		return ResolveEpisodeAudioTimingOutput{}, fmt.Errorf("organizationId, projectId, and scriptEpisodeId are required")
	}
	var output ResolveEpisodeAudioTimingOutput
	err := a.db.QueryRow(ctx, `
		SELECT episode.script_id::text, episode.script_version_id::text,
		       COALESCE((SELECT analysis.id::text FROM script_timing_analyses analysis
		                 WHERE analysis.script_episode_id = episode.id AND analysis.status = 'ready'
		                 ORDER BY analysis.revision DESC LIMIT 1), '')
		FROM script_episodes episode
		WHERE episode.organization_id = $1 AND episode.project_id = $2 AND episode.id = $3
	`, input.OrganizationID, input.ProjectID, input.ScriptEpisodeID).Scan(&output.ScriptID, &output.ScriptVersionID, &output.TimingAnalysisID)
	return output, err
}

func (a Activities) PrepareEpisodeTTS(ctx context.Context, input PrepareEpisodeTTSInput) (PrepareEpisodeTTSOutput, error) {
	req := input.EpisodeAudioProductionInput
	if strings.TrimSpace(req.OrganizationID) == "" || strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.WorkflowRunID) == "" || strings.TrimSpace(req.ScriptEpisodeID) == "" {
		return PrepareEpisodeTTSOutput{}, fmt.Errorf("organizationId, projectId, workflowRunId, and scriptEpisodeId are required")
	}
	nodeExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: req.OrganizationID, ProjectID: req.ProjectID, WorkflowRunID: req.WorkflowRunID,
		NodeKey: nodePrepareEpisodeTTS, NodeType: "audio.tts.prepare", Input: mustJSON(req),
	})
	if err != nil {
		return PrepareEpisodeTTSOutput{}, err
	}
	analysisID := strings.TrimSpace(req.TimingAnalysisID)
	var output PrepareEpisodeTTSOutput
	query := `
		SELECT analysis.id::text, analysis.script_id::text, analysis.script_version_id::text,
		       analysis.timeline_timebase, analysis.fps_numerator, analysis.fps_denominator,
		       project.audio_configuration_revision
		FROM script_timing_analyses analysis
		JOIN projects project ON project.id = analysis.project_id
		WHERE analysis.organization_id = $1 AND analysis.project_id = $2 AND analysis.script_episode_id = $3
		  AND analysis.status = 'ready'`
	args := []any{req.OrganizationID, req.ProjectID, req.ScriptEpisodeID}
	if analysisID != "" {
		query += " AND analysis.id = $4"
		args = append(args, analysisID)
	}
	query += " ORDER BY analysis.revision DESC LIMIT 1"
	if err := a.db.QueryRow(ctx, query, args...).Scan(&output.TimingAnalysisID, &output.ScriptID, &output.ScriptVersionID, &output.TimelineTimebase, &output.FPSNumerator, &output.FPSDenominator, &output.AudioConfigurationRevision); err != nil {
		_ = FailNodeRun(ctx, a.db, nodeExecution, provider.CodeInvalidRequest, err.Error())
		return PrepareEpisodeTTSOutput{}, err
	}

	rows, err := a.db.Query(ctx, `
		SELECT unit.id::text, unit.unit_ordinal, COALESCE(unit.speaker, ''), unit.source_text, COALESCE(unit.delivery, ''),
		       COALESCE(voice.id::text, default_voice.id::text, ''),
		       COALESCE(active_clip.id::text, '')
		FROM script_timing_units unit
		LEFT JOIN character_voice_profiles voice
		  ON voice.project_id = $2 AND voice.status = 'active' AND lower(voice.character_name) = lower(COALESCE(unit.speaker, ''))
		LEFT JOIN character_voice_profiles default_voice
		  ON default_voice.project_id = $2 AND default_voice.status = 'active'
		 AND default_voice.id = COALESCE(
		       NULLIF($4, '')::uuid,
		       (SELECT fallback.id FROM character_voice_profiles fallback
		        WHERE fallback.project_id = $2 AND fallback.status = 'active' AND fallback.is_default = true
		        LIMIT 1)
		     )
		LEFT JOIN tts_audio_clips active_clip ON active_clip.timing_unit_id = unit.id AND active_clip.active = true
		 AND active_clip.status = 'succeeded' AND active_clip.audio_configuration_revision = $5
		WHERE unit.timing_analysis_id = $1 AND unit.track = 'audio'
		  AND unit.unit_type IN ('dialogue', 'voiceover', 'narration', 'system')
		  AND btrim(unit.source_text) <> ''
		ORDER BY unit.unit_ordinal
	`, output.TimingAnalysisID, req.ProjectID, req.OrganizationID, req.DefaultVoiceProfileID, output.AudioConfigurationRevision)
	if err != nil {
		_ = FailNodeRun(ctx, a.db, nodeExecution, codeActivityFailed, err.Error())
		return PrepareEpisodeTTSOutput{}, err
	}
	defer rows.Close()
	type pendingUnit struct {
		ID, Speaker, Text, Delivery, VoiceID, ActiveClipID string
		Ordinal                                            int
	}
	units := make([]pendingUnit, 0)
	missing := make([]string, 0)
	for rows.Next() {
		var unit pendingUnit
		if err := rows.Scan(&unit.ID, &unit.Ordinal, &unit.Speaker, &unit.Text, &unit.Delivery, &unit.VoiceID, &unit.ActiveClipID); err != nil {
			return PrepareEpisodeTTSOutput{}, err
		}
		if unit.VoiceID == "" {
			missing = append(missing, firstNonEmptyString(unit.Speaker, "未命名旁白"))
		}
		units = append(units, unit)
	}
	if err := rows.Err(); err != nil {
		return PrepareEpisodeTTSOutput{}, err
	}
	if len(units) == 0 {
		err := fmt.Errorf("%w: timing analysis has no speech units", provider.ErrValidation)
		_ = FailNodeRun(ctx, a.db, nodeExecution, provider.CodeInvalidRequest, err.Error())
		return PrepareEpisodeTTSOutput{}, err
	}
	missing = uniqueWorkflowStrings(missing)
	if len(missing) > 0 {
		err := fmt.Errorf("%w: voice profiles are missing for %s", provider.ErrValidation, strings.Join(missing, "、"))
		_ = FailNodeRun(ctx, a.db, nodeExecution, "VOICE_PROFILE_NOT_CONFIGURED", err.Error())
		return PrepareEpisodeTTSOutput{}, err
	}

	tx, err := a.db.Begin(ctx)
	if err != nil {
		return PrepareEpisodeTTSOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, req.WorkflowRunID, nodeExecution); err != nil {
		return PrepareEpisodeTTSOutput{}, err
	}
	for _, unit := range units {
		if unit.ActiveClipID != "" && !req.Force {
			output.ExistingClipIDs = append(output.ExistingClipIDs, unit.ActiveClipID)
			continue
		}
		var revision int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision), 0) + 1 FROM tts_audio_clips WHERE timing_unit_id = $1`, unit.ID).Scan(&revision); err != nil {
			return PrepareEpisodeTTSOutput{}, err
		}
		var clipID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO tts_audio_clips(
				organization_id, project_id, script_episode_id, timing_analysis_id, timing_unit_id,
				character_voice_profile_id, workflow_run_id, model_profile_key, provider_model_id, source_text, speaker,
				language, voice_key, output_format, status, revision, audio_configuration_revision, active,
				timeline_timebase, created_by, metadata
			)
			SELECT $1, $2, $3, $4, $5, voice.id, $6, voice.model_profile_key, voice.provider_model_id, $7, NULLIF($8, ''),
			       voice.language, voice.voice_key, COALESCE(NULLIF(voice.parameters->>'outputFormat', ''), 'wav'),
			       'queued', $9, $14, false, $10, NULLIF($11, '')::uuid,
			       jsonb_build_object(
			         'delivery', NULLIF($12, ''), 'instructions', voice.instructions,
			         'voiceParameters', voice.parameters, 'providerModelId', voice.provider_model_id,
			         'audioConfigurationRevision', $14::integer
			       )
			FROM character_voice_profiles voice
			WHERE voice.id = $13 AND voice.project_id = $2 AND voice.status = 'active'
			RETURNING id::text
		`, req.OrganizationID, req.ProjectID, req.ScriptEpisodeID, output.TimingAnalysisID, unit.ID,
			req.WorkflowRunID, unit.Text, unit.Speaker, revision, output.TimelineTimebase, req.CreatedBy, unit.Delivery,
			unit.VoiceID, output.AudioConfigurationRevision).Scan(&clipID); err != nil {
			return PrepareEpisodeTTSOutput{}, err
		}
		output.Jobs = append(output.Jobs, TTSGenerationJob{
			ClipID: clipID, TimingUnitID: unit.ID, VoiceProfileID: unit.VoiceID, UnitOrdinal: unit.Ordinal,
			Speaker: unit.Speaker, Text: unit.Text, Delivery: unit.Delivery, Revision: revision,
		})
	}
	if err := insertEvent(ctx, tx, req.OrganizationID, req.ProjectID, "audio.tts.prepared", "script_episode", req.ScriptEpisodeID, mustJSON(map[string]any{
		"workflowRunId": req.WorkflowRunID, "scriptEpisodeId": req.ScriptEpisodeID, "timingAnalysisId": output.TimingAnalysisID,
		"jobCount": len(output.Jobs), "existingCount": len(output.ExistingClipIDs),
		"audioConfigurationRevision": output.AudioConfigurationRevision,
	})); err != nil {
		return PrepareEpisodeTTSOutput{}, err
	}
	if _, err := completeNodeRunTx(ctx, tx, nodeExecution, mustJSON(output)); err != nil {
		return PrepareEpisodeTTSOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PrepareEpisodeTTSOutput{}, err
	}
	return output, nil
}

func (a Activities) GenerateTTSAudio(ctx context.Context, input GenerateTTSAudioInput) (GenerateTTSAudioOutput, error) {
	if strings.TrimSpace(input.ClipID) == "" {
		return GenerateTTSAudioOutput{}, fmt.Errorf("clipId is required")
	}
	type clipRecord struct {
		ID, ScriptEpisodeID, TimingUnitID, ModelProfileKey, ProviderModelID, Text, Speaker, Language, VoiceKey, OutputFormat, Instructions, Delivery string
		Parameters                                                                                                                                   json.RawMessage
		TimelineTimebase                                                                                                                             int64
		Revision, AudioConfigurationRevision, CurrentAudioConfigurationRevision                                                                      int
		Status, ArtifactID, MediaFileID, StorageKey, MimeType                                                                                        string
		DurationTicks                                                                                                                                int64
	}
	var clip clipRecord
	err := a.db.QueryRow(ctx, `
		SELECT clip.id::text, clip.script_episode_id::text, clip.timing_unit_id::text, clip.model_profile_key,
		       COALESCE(clip.provider_model_id::text, ''), clip.source_text, COALESCE(clip.speaker, ''), clip.language,
		       clip.voice_key, clip.output_format, COALESCE(clip.metadata->>'instructions', ''), COALESCE(clip.metadata->>'delivery', ''),
		       COALESCE(clip.metadata->'voiceParameters', '{}'::jsonb), clip.timeline_timebase, clip.revision,
		       clip.audio_configuration_revision, project.audio_configuration_revision, clip.status,
		       COALESCE(clip.artifact_id::text, ''), COALESCE(clip.media_file_id::text, ''), COALESCE(clip.storage_key, ''),
		       COALESCE(clip.mime_type, ''), COALESCE(clip.duration_ticks, 0)
		FROM tts_audio_clips clip
		JOIN projects project ON project.id = clip.project_id
		WHERE clip.organization_id = $1 AND clip.project_id = $2 AND clip.id = $3
	`, input.OrganizationID, input.ProjectID, input.ClipID).Scan(
		&clip.ID, &clip.ScriptEpisodeID, &clip.TimingUnitID, &clip.ModelProfileKey, &clip.ProviderModelID, &clip.Text, &clip.Speaker, &clip.Language,
		&clip.VoiceKey, &clip.OutputFormat, &clip.Instructions, &clip.Delivery, &clip.Parameters, &clip.TimelineTimebase, &clip.Revision,
		&clip.AudioConfigurationRevision, &clip.CurrentAudioConfigurationRevision, &clip.Status,
		&clip.ArtifactID, &clip.MediaFileID, &clip.StorageKey, &clip.MimeType, &clip.DurationTicks,
	)
	if err != nil {
		return GenerateTTSAudioOutput{}, err
	}
	if clip.AudioConfigurationRevision != clip.CurrentAudioConfigurationRevision ||
		(input.AudioConfigurationRevision > 0 && input.AudioConfigurationRevision != clip.AudioConfigurationRevision) {
		_, _ = a.db.Exec(ctx, `
			UPDATE tts_audio_clips
			SET status = 'stale', active = false, error_code = $2, error_message = $3,
			    metadata = metadata || jsonb_build_object('discardedAt', now(), 'currentAudioConfigurationRevision', $4::integer),
			    updated_at = now(), completed_at = COALESCE(completed_at, now())
			WHERE id = $1 AND status <> 'stale'
		`, clip.ID, codeAudioConfigurationChanged, "音频配置已变更，该 TTS 任务不再有效", clip.CurrentAudioConfigurationRevision)
		return GenerateTTSAudioOutput{ClipID: clip.ID, TimingUnitID: clip.TimingUnitID, Status: "stale",
			AudioConfigurationRevision: clip.AudioConfigurationRevision, ErrorCode: codeAudioConfigurationChanged,
			ErrorMessage: "音频配置已变更，该 TTS 任务不再有效"}, nil
	}
	if clip.Status == "succeeded" && clip.ArtifactID != "" && clip.MediaFileID != "" && clip.DurationTicks > 0 {
		return GenerateTTSAudioOutput{ClipID: clip.ID, TimingUnitID: clip.TimingUnitID, Status: clip.Status, AudioConfigurationRevision: clip.AudioConfigurationRevision, ArtifactID: clip.ArtifactID,
			MediaFileID: clip.MediaFileID, StorageKey: clip.StorageKey, MimeType: clip.MimeType, DurationTicks: clip.DurationTicks,
			DurationSeconds: float64(clip.DurationTicks) / float64(clip.TimelineTimebase)}, nil
	}
	nodeKey := nodeKeyForID(nodeTTSPrefix, clip.ID)
	nodeExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		NodeKey: nodeKey, NodeType: "audio.tts.generate", Input: mustJSON(map[string]any{"clipId": clip.ID, "timingUnitId": clip.TimingUnitID, "speaker": clip.Speaker}),
	})
	if err != nil {
		return GenerateTTSAudioOutput{}, err
	}
	runningTx, err := a.db.Begin(ctx)
	if err != nil {
		return GenerateTTSAudioOutput{}, err
	}
	defer runningTx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, runningTx, input.WorkflowRunID, nodeExecution); err != nil {
		return GenerateTTSAudioOutput{}, err
	}
	runningTag, err := runningTx.Exec(ctx, `
		UPDATE tts_audio_clips clip SET status = 'running', node_run_id = $2, updated_at = now()
		WHERE clip.id = $1 AND clip.status IN ('queued', 'running')
		  AND clip.audio_configuration_revision = (SELECT audio_configuration_revision FROM projects WHERE id = clip.project_id)
	`, clip.ID, nodeExecution.NodeRunID)
	if err != nil {
		return GenerateTTSAudioOutput{}, err
	}
	if runningTag.RowsAffected() == 0 {
		output := GenerateTTSAudioOutput{ClipID: clip.ID, TimingUnitID: clip.TimingUnitID, Status: "stale",
			AudioConfigurationRevision: clip.AudioConfigurationRevision, ErrorCode: codeAudioConfigurationChanged,
			ErrorMessage: "音频配置已变更，该 TTS 任务不再有效"}
		if _, err := failNodeRunTx(ctx, runningTx, nodeExecution, codeAudioConfigurationChanged, output.ErrorMessage, mustJSON(output)); err != nil {
			return GenerateTTSAudioOutput{}, err
		}
		if err := runningTx.Commit(ctx); err != nil {
			return GenerateTTSAudioOutput{}, err
		}
		return output, nil
	}
	if err := runningTx.Commit(ctx); err != nil {
		return GenerateTTSAudioOutput{}, err
	}
	if a.gateway == nil {
		return a.failTTSAudio(ctx, nodeExecution, input.WorkflowRunID, clip.ID, clip.TimingUnitID, provider.CodeProviderGatewayRequired, "provider gateway client is not configured")
	}
	requestInput := map[string]any{}
	_ = json.Unmarshal(clip.Parameters, &requestInput)
	requestInput["input"] = clip.Text
	requestInput["voice"] = clip.VoiceKey
	requestInput["response_format"] = clip.OutputFormat
	if clip.Instructions != "" {
		requestInput["instructions"] = clip.Instructions
	}
	response, err := a.gateway.GenerateSpeech(ctx, provider.GatewayTTSRequest{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, NodeRunID: nodeExecution.NodeRunID,
		ModelProfileKey: clip.ModelProfileKey, ProviderModelID: clip.ProviderModelID,
		IdempotencyKey: fmt.Sprintf("tts:%s:r%d:c%d", clip.TimingUnitID, clip.Revision, clip.AudioConfigurationRevision), TimelineTimebase: clip.TimelineTimebase,
		Input: mustJSON(requestInput), Options: provider.GatewayAudioOptions{TimeoutMS: gatewayAudioActivityTimeoutMS()},
	})
	if err != nil {
		code, message := workflowErrorFields(workflowErrorFromProvider(err, codeActivityFailed), codeActivityFailed)
		return a.failTTSAudio(ctx, nodeExecution, input.WorkflowRunID, clip.ID, clip.TimingUnitID, code, message)
	}
	objectStore, ok := a.storage.(mediapkg.ObjectStore)
	if !ok {
		return a.failTTSAudio(ctx, nodeExecution, input.WorkflowRunID, clip.ID, clip.TimingUnitID, codeActivityFailed, "object storage does not support audio reads")
	}
	body, mimeType, err := objectStore.GetObject(ctx, response.Output.StorageKey, mediapkg.DefaultMaxAudioTrackBytes)
	if err != nil {
		return a.failTTSAudio(ctx, nodeExecution, input.WorkflowRunID, clip.ID, clip.TimingUnitID, provider.CodeMediaDownloadFailed, err.Error())
	}
	if response.Output.MimeType != "" {
		mimeType = response.Output.MimeType
	}
	probe, err := mediapkg.ProbeVideoBytes(ctx, body, mimeType)
	if err != nil || !probe.HasAudio || probe.DurationSeconds <= 0 {
		message := "generated TTS audio could not be probed"
		if err != nil {
			message = err.Error()
		}
		return a.failTTSAudio(ctx, nodeExecution, input.WorkflowRunID, clip.ID, clip.TimingUnitID, "AUDIO_PROBE_FAILED", message)
	}
	durationTicks := actualAudioDurationTicks(probe, clip.TimelineTimebase)
	if durationTicks <= 0 {
		return a.failTTSAudio(ctx, nodeExecution, input.WorkflowRunID, clip.ID, clip.TimingUnitID, "AUDIO_PROBE_FAILED", "generated TTS audio has no measurable duration")
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return GenerateTTSAudioOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, nodeExecution); err != nil {
		return GenerateTTSAudioOutput{}, err
	}
	var currentAudioConfigurationRevision int
	if err := tx.QueryRow(ctx, `SELECT audio_configuration_revision FROM projects WHERE id = $1 FOR SHARE`, input.ProjectID).Scan(&currentAudioConfigurationRevision); err != nil {
		return GenerateTTSAudioOutput{}, err
	}
	if currentAudioConfigurationRevision != clip.AudioConfigurationRevision {
		if _, err := tx.Exec(ctx, `
			UPDATE tts_audio_clips
			SET status = 'stale', active = false, provider_call_id = $2, provider_model_id = $3,
			    artifact_id = $4, media_file_id = $5, storage_key = $6, mime_type = $7, byte_size = $8,
			    sample_rate = $9, sample_count = $10, channel_count = $11, duration_ticks = $12,
			    error_code = $13, error_message = $14,
			    metadata = metadata || jsonb_build_object(
			      'probe', $15::jsonb, 'durationSource', 'tts_actual', 'discardedAt', now(),
			      'currentAudioConfigurationRevision', $16::integer
			    ), updated_at = now(), completed_at = now()
			WHERE id = $1
		`, clip.ID, response.ProviderCallID, response.ModelID, response.Output.ArtifactID, response.Output.MediaFileID,
			response.Output.StorageKey, mimeType, response.Output.ByteSize, probe.AudioSampleRate, probe.AudioSampleCount,
			probe.AudioChannelCount, durationTicks, codeAudioConfigurationChanged, "音频配置已变更，已保留媒体但不会用于生产",
			mustJSON(probe), currentAudioConfigurationRevision); err != nil {
			return GenerateTTSAudioOutput{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE media_files SET duration_seconds = $2,
			       metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('audioProbe', $3::jsonb, 'staleAudioConfiguration', true)
			WHERE id = $1
		`, response.Output.MediaFileID, probe.DurationSeconds, mustJSON(probe)); err != nil {
			return GenerateTTSAudioOutput{}, err
		}
		if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "audio.tts.clip.discarded", "tts_audio_clip", clip.ID, mustJSON(map[string]any{
			"workflowRunId": input.WorkflowRunID, "clipId": clip.ID, "providerCallId": response.ProviderCallID,
			"audioConfigurationRevision":        clip.AudioConfigurationRevision,
			"currentAudioConfigurationRevision": currentAudioConfigurationRevision,
		})); err != nil {
			return GenerateTTSAudioOutput{}, err
		}
		output := GenerateTTSAudioOutput{
			ClipID: clip.ID, TimingUnitID: clip.TimingUnitID, Status: "stale", AudioConfigurationRevision: clip.AudioConfigurationRevision,
			ProviderCallID: response.ProviderCallID, ModelID: response.ModelID, ArtifactID: response.Output.ArtifactID,
			MediaFileID: response.Output.MediaFileID, StorageKey: response.Output.StorageKey, MimeType: mimeType,
			SampleRate: probe.AudioSampleRate, SampleCount: probe.AudioSampleCount, ChannelCount: probe.AudioChannelCount,
			DurationTicks: durationTicks, DurationSeconds: probe.DurationSeconds, ErrorCode: codeAudioConfigurationChanged,
			ErrorMessage: "音频配置已变更，已保留媒体但不会用于生产",
		}
		if _, err := failNodeRunTx(ctx, tx, nodeExecution, codeAudioConfigurationChanged, output.ErrorMessage, mustJSON(output)); err != nil {
			return GenerateTTSAudioOutput{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return GenerateTTSAudioOutput{}, err
		}
		return output, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE tts_audio_clips SET active = false, status = CASE WHEN status = 'succeeded' THEN 'stale' ELSE status END, updated_at = now() WHERE timing_unit_id = $1 AND id <> $2 AND active = true`, clip.TimingUnitID, clip.ID); err != nil {
		return GenerateTTSAudioOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE tts_audio_clips
		SET status = 'succeeded', active = true, provider_call_id = $2, provider_model_id = $3,
		    artifact_id = $4, media_file_id = $5, storage_key = $6, mime_type = $7, byte_size = $8,
		    sample_rate = $9, sample_count = $10, channel_count = $11, duration_ticks = $12,
		    error_code = NULL, error_message = NULL,
		    metadata = metadata || jsonb_build_object('probe', $13::jsonb, 'durationSource', 'tts_actual'),
		    updated_at = now(), completed_at = now()
		WHERE id = $1 AND audio_configuration_revision = $14
	`, clip.ID, response.ProviderCallID, response.ModelID, response.Output.ArtifactID, response.Output.MediaFileID,
		response.Output.StorageKey, mimeType, response.Output.ByteSize, probe.AudioSampleRate, probe.AudioSampleCount,
		probe.AudioChannelCount, durationTicks, mustJSON(probe), clip.AudioConfigurationRevision); err != nil {
		return GenerateTTSAudioOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE media_files SET duration_seconds = $2, metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('audioProbe', $3::jsonb)
		WHERE id = $1
	`, response.Output.MediaFileID, probe.DurationSeconds, mustJSON(probe)); err != nil {
		return GenerateTTSAudioOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO timing_calibration_samples(
			organization_id, project_id, script_episode_id, timing_unit_id, sample_kind, sample_key,
			source_kind, expected_ticks, actual_ticks, timeline_timebase, confidence, audio_configuration_revision, metadata
		)
		SELECT clip.organization_id, clip.project_id, clip.script_episode_id, clip.timing_unit_id,
		       'dialogue_duration', COALESCE(NULLIF(lower(clip.metadata->>'delivery'), ''), 'normal'),
		       'tts_actual', unit.duration_ticks, $2, clip.timeline_timebase, 1, clip.audio_configuration_revision,
		       jsonb_build_object('clipId', clip.id, 'text', clip.source_text, 'providerCallId', $3::uuid::text)
		FROM tts_audio_clips clip JOIN script_timing_units unit ON unit.id = clip.timing_unit_id
		WHERE clip.id = $1
	`, clip.ID, durationTicks, response.ProviderCallID); err != nil {
		return GenerateTTSAudioOutput{}, err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "audio.tts.clip.generated", "tts_audio_clip", clip.ID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "scriptEpisodeId": clip.ScriptEpisodeID, "clipId": clip.ID, "timingUnitId": clip.TimingUnitID,
		"providerCallId": response.ProviderCallID, "durationTicks": durationTicks, "durationSeconds": probe.DurationSeconds,
		"audioConfigurationRevision": clip.AudioConfigurationRevision,
	})); err != nil {
		return GenerateTTSAudioOutput{}, err
	}
	output := GenerateTTSAudioOutput{
		ClipID: clip.ID, TimingUnitID: clip.TimingUnitID, Status: "succeeded", AudioConfigurationRevision: clip.AudioConfigurationRevision, ProviderCallID: response.ProviderCallID,
		ModelID: response.ModelID, ArtifactID: response.Output.ArtifactID, MediaFileID: response.Output.MediaFileID,
		StorageKey: response.Output.StorageKey, MimeType: mimeType, SampleRate: probe.AudioSampleRate,
		SampleCount: probe.AudioSampleCount, ChannelCount: probe.AudioChannelCount, DurationTicks: durationTicks,
		DurationSeconds: float64(durationTicks) / float64(clip.TimelineTimebase),
	}
	if _, err := completeNodeRunTx(ctx, tx, nodeExecution, mustJSON(output)); err != nil {
		return GenerateTTSAudioOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return GenerateTTSAudioOutput{}, err
	}
	return output, nil
}

func (a Activities) failTTSAudio(ctx context.Context, execution NodeExecution, workflowRunID, clipID, timingUnitID, code, message string) (GenerateTTSAudioOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return GenerateTTSAudioOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, workflowRunID, execution); err != nil {
		return GenerateTTSAudioOutput{}, err
	}
	var status string
	dbErr := tx.QueryRow(ctx, `
		UPDATE tts_audio_clips clip
		SET status = CASE WHEN clip.audio_configuration_revision = project.audio_configuration_revision THEN 'failed' ELSE 'stale' END,
		    active = false,
		    error_code = CASE WHEN clip.audio_configuration_revision = project.audio_configuration_revision THEN $2 ELSE $4 END,
		    error_message = CASE WHEN clip.audio_configuration_revision = project.audio_configuration_revision THEN $3 ELSE $5 END,
		    metadata = CASE WHEN clip.audio_configuration_revision = project.audio_configuration_revision THEN clip.metadata
		                    ELSE clip.metadata || jsonb_build_object('discardedAt', now(), 'currentAudioConfigurationRevision', project.audio_configuration_revision) END,
		    updated_at = now(), completed_at = now()
		FROM projects project
		WHERE clip.id = $1 AND project.id = clip.project_id
		RETURNING clip.status
	`, clipID, code, message, codeAudioConfigurationChanged, "音频配置已变更，该 TTS 任务不再有效").Scan(&status)
	if dbErr != nil {
		return GenerateTTSAudioOutput{}, dbErr
	}
	if status == "stale" {
		code = codeAudioConfigurationChanged
		message = "音频配置已变更，该 TTS 任务不再有效"
	}
	output := GenerateTTSAudioOutput{ClipID: clipID, TimingUnitID: timingUnitID, Status: status, ErrorCode: code, ErrorMessage: message}
	if _, err := failNodeRunTx(ctx, tx, execution, code, message, mustJSON(output)); err != nil {
		return GenerateTTSAudioOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return GenerateTTSAudioOutput{}, err
	}
	return output, nil
}

func actualAudioDurationTicks(probe mediapkg.ProbeResult, timebase int64) int64 {
	if timebase <= 0 {
		return 0
	}
	if probe.AudioSampleRate > 0 && probe.AudioSampleCount > 0 {
		return int64(math.Round(float64(probe.AudioSampleCount) * float64(timebase) / float64(probe.AudioSampleRate)))
	}
	return int64(math.Round(probe.DurationSeconds * float64(timebase)))
}

type timingRevisionUnit struct {
	ID, ScriptSceneID, SourceChapterID, UnitType, Track, ParallelGroup, Speaker, SourceText, Delivery string
	Ordinal                                                                                           int
	SourceStartOffset, SourceEndOffset                                                                sql.NullInt64
	StartTick, EndTick, DurationTicks                                                                 int64
	MinDurationTicks, MaxDurationTicks                                                                sql.NullInt64
	DurationSource                                                                                    string
	Confidence                                                                                        sql.NullFloat64
	Metadata                                                                                          json.RawMessage
	TTSClipID                                                                                         string
}

func (a Activities) CreateTTSTimingRevision(ctx context.Context, input CreateTTSTimingRevisionInput) (CreateTTSTimingRevisionOutput, error) {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.SourceAnalysisID) == "" || strings.TrimSpace(input.ScriptEpisodeID) == "" || input.AudioConfigurationRevision <= 0 {
		return CreateTTSTimingRevisionOutput{}, fmt.Errorf("organizationId, projectId, workflowRunId, sourceAnalysisId, scriptEpisodeId, and audioConfigurationRevision are required")
	}
	execution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        fmt.Sprintf("create_tts_timing_revision_%s_c%d", input.SourceAnalysisID, input.AudioConfigurationRevision),
		NodeType:       "audio.tts.timing_revision",
		Input:          mustJSON(input),
	})
	if err != nil {
		return CreateTTSTimingRevisionOutput{}, err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return CreateTTSTimingRevisionOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return CreateTTSTimingRevisionOutput{}, err
	}
	var currentAudioConfigurationRevision int
	if err := tx.QueryRow(ctx, `SELECT audio_configuration_revision FROM projects WHERE id = $1 FOR SHARE`, input.ProjectID).Scan(&currentAudioConfigurationRevision); err != nil {
		return CreateTTSTimingRevisionOutput{}, err
	}
	if currentAudioConfigurationRevision != input.AudioConfigurationRevision {
		return CreateTTSTimingRevisionOutput{}, fmt.Errorf("%s: audio configuration changed before timing revision", codeAudioConfigurationChanged)
	}
	var scriptID, scriptVersionID string
	var timelineTimebase int64
	var fpsNumerator, fpsDenominator int
	var targetDuration sql.NullInt64
	if err := tx.QueryRow(ctx, `
		SELECT analysis.script_id::text, analysis.script_version_id::text, analysis.timeline_timebase,
		       analysis.fps_numerator, analysis.fps_denominator, analysis.target_duration_ticks
		FROM script_timing_analyses analysis
		WHERE analysis.organization_id = $1 AND analysis.project_id = $2 AND analysis.script_episode_id = $3 AND analysis.id = $4
		FOR UPDATE
	`, input.OrganizationID, input.ProjectID, input.ScriptEpisodeID, input.SourceAnalysisID).Scan(
		&scriptID, &scriptVersionID, &timelineTimebase, &fpsNumerator, &fpsDenominator, &targetDuration,
	); err != nil {
		return CreateTTSTimingRevisionOutput{}, err
	}
	rows, err := tx.Query(ctx, `
		SELECT unit.id::text, COALESCE(unit.script_scene_id::text, ''), COALESCE(unit.source_chapter_id::text, ''),
		       unit.unit_ordinal, unit.unit_type, unit.track, COALESCE(unit.parallel_group, ''), COALESCE(unit.speaker, ''),
		       unit.source_text, COALESCE(unit.delivery, ''), unit.source_start_offset, unit.source_end_offset,
		       unit.start_tick, unit.end_tick, unit.duration_ticks, unit.min_duration_ticks, unit.max_duration_ticks,
		       unit.duration_source, unit.confidence::float8, unit.metadata,
		       COALESCE(clip.id::text, '')
		FROM script_timing_units unit
		LEFT JOIN tts_audio_clips clip ON clip.timing_unit_id = unit.id AND clip.active = true
		 AND clip.status = 'succeeded' AND clip.audio_configuration_revision = $2
		WHERE unit.timing_analysis_id = $1
		ORDER BY unit.unit_ordinal
	`, input.SourceAnalysisID, input.AudioConfigurationRevision)
	if err != nil {
		return CreateTTSTimingRevisionOutput{}, err
	}
	units := make([]timingRevisionUnit, 0)
	for rows.Next() {
		var unit timingRevisionUnit
		if err := rows.Scan(&unit.ID, &unit.ScriptSceneID, &unit.SourceChapterID, &unit.Ordinal, &unit.UnitType, &unit.Track,
			&unit.ParallelGroup, &unit.Speaker, &unit.SourceText, &unit.Delivery, &unit.SourceStartOffset, &unit.SourceEndOffset,
			&unit.StartTick, &unit.EndTick, &unit.DurationTicks, &unit.MinDurationTicks, &unit.MaxDurationTicks,
			&unit.DurationSource, &unit.Confidence, &unit.Metadata, &unit.TTSClipID); err != nil {
			rows.Close()
			return CreateTTSTimingRevisionOutput{}, err
		}
		if unit.Track == "audio" && isTTSSpeechUnitType(unit.UnitType) {
			if unit.TTSClipID == "" {
				rows.Close()
				return CreateTTSTimingRevisionOutput{}, fmt.Errorf("%w: timing unit %s has no active TTS clip", provider.ErrValidation, unit.ID)
			}
			if err := tx.QueryRow(ctx, `SELECT duration_ticks FROM tts_audio_clips WHERE id = $1 AND audio_configuration_revision = $2`, unit.TTSClipID, input.AudioConfigurationRevision).Scan(&unit.DurationTicks); err != nil {
				rows.Close()
				return CreateTTSTimingRevisionOutput{}, err
			}
			unit.DurationSource = "tts_actual"
			unit.MinDurationTicks = sql.NullInt64{Int64: unit.DurationTicks, Valid: true}
			unit.MaxDurationTicks = sql.NullInt64{Int64: unit.DurationTicks, Valid: true}
			unit.Confidence = sql.NullFloat64{Float64: 1, Valid: true}
		}
		units = append(units, unit)
	}
	if err := rows.Err(); err != nil {
		return CreateTTSTimingRevisionOutput{}, err
	}
	rows.Close()
	if len(units) == 0 {
		return CreateTTSTimingRevisionOutput{}, fmt.Errorf("%w: timing analysis has no units", provider.ErrValidation)
	}
	totalTicks, minimumTicks := reflowTTSTimingUnits(units)
	var revision int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision), 0) + 1 FROM script_timing_analyses WHERE script_episode_id = $1`, input.ScriptEpisodeID).Scan(&revision); err != nil {
		return CreateTTSTimingRevisionOutput{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE script_timing_analyses SET status = 'archived' WHERE script_episode_id = $1 AND status = 'ready'`, input.ScriptEpisodeID); err != nil {
		return CreateTTSTimingRevisionOutput{}, err
	}
	var newAnalysisID string
	var retainedTarget any
	if targetDuration.Valid && targetDuration.Int64 >= totalTicks {
		retainedTarget = targetDuration.Int64
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO script_timing_analyses(
			organization_id, project_id, script_id, script_version_id, script_episode_id,
			revision, status, estimated_duration_ticks, minimum_duration_ticks, target_duration_ticks,
			timeline_timebase, fps_numerator, fps_denominator, method_version, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, 'ready', $7, $8, $9, $10, $11, $12, 'tts-actual-v1',
		        jsonb_build_object('sourceTimingAnalysisId', $13::uuid::text, 'workflowRunId', NULLIF($14, '')::uuid::text,
		                           'targetExceeded', $15::boolean, 'audioConfigurationRevision', $17::integer), NULLIF($16, '')::uuid)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, scriptID, scriptVersionID, input.ScriptEpisodeID, revision,
		totalTicks, minimumTicks, retainedTarget, timelineTimebase, fpsNumerator, fpsDenominator, input.SourceAnalysisID,
		input.WorkflowRunID, targetDuration.Valid && targetDuration.Int64 < totalTicks, input.CreatedBy, input.AudioConfigurationRevision).Scan(&newAnalysisID); err != nil {
		return CreateTTSTimingRevisionOutput{}, err
	}
	ttsUnitCount := 0
	for _, unit := range units {
		metadata := mergeTimingUnitMetadata(unit.Metadata, map[string]any{"sourceTimingUnitId": unit.ID})
		if unit.TTSClipID != "" {
			ttsUnitCount++
			metadata = mergeTimingUnitMetadata(metadata, map[string]any{"sourceTtsAudioClipId": unit.TTSClipID})
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO script_timing_units(
				timing_analysis_id, script_scene_id, source_chapter_id, unit_ordinal, unit_type, track,
				parallel_group, speaker, source_text, delivery, source_start_offset, source_end_offset,
				start_tick, end_tick, min_duration_ticks, max_duration_ticks, duration_source, confidence,
				metadata, source_tts_audio_clip_id
			)
			VALUES ($1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4, $5, $6, NULLIF($7, ''), NULLIF($8, ''), $9,
			        NULLIF($10, ''), $11, $12, $13, $14, $15, $16, $17, $18, $19, NULLIF($20, '')::uuid)
		`, newAnalysisID, unit.ScriptSceneID, unit.SourceChapterID, unit.Ordinal, unit.UnitType, unit.Track,
			unit.ParallelGroup, unit.Speaker, unit.SourceText, unit.Delivery, nullableSQLInt64(unit.SourceStartOffset), nullableSQLInt64(unit.SourceEndOffset),
			unit.StartTick, unit.EndTick, nullableSQLInt64(unit.MinDurationTicks), nullableSQLInt64(unit.MaxDurationTicks),
			unit.DurationSource, nullableSQLFloat64(unit.Confidence), metadata, unit.TTSClipID); err != nil {
			return CreateTTSTimingRevisionOutput{}, err
		}
		if unit.TTSClipID != "" {
			if _, err := tx.Exec(ctx, `UPDATE tts_audio_clips SET applied_timing_analysis_id = $2, updated_at = now() WHERE id = $1 AND audio_configuration_revision = $3`, unit.TTSClipID, newAnalysisID, input.AudioConfigurationRevision); err != nil {
				return CreateTTSTimingRevisionOutput{}, err
			}
		}
	}
	staleTag, err := tx.Exec(ctx, `
		UPDATE storyboard_plans SET stale_state = 'needs_regeneration', metadata = metadata || jsonb_build_object('ttsTimingChangedAt', now(), 'replacementTimingAnalysisId', $2::uuid::text)
		WHERE project_id = $1 AND timing_analysis_id = $3 AND stale_state <> 'needs_regeneration'
	`, input.ProjectID, newAnalysisID, input.SourceAnalysisID)
	if err != nil {
		return CreateTTSTimingRevisionOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots SET stale_state = 'needs_regeneration', image_status = CASE WHEN image_artifact_id IS NOT NULL THEN 'stale' ELSE image_status END,
		       video_status = CASE WHEN video_artifact_id IS NOT NULL THEN 'stale' ELSE video_status END, updated_at = now()
		WHERE project_id = $1 AND storyboard_plan_id IN (SELECT id FROM storyboard_plans WHERE timing_analysis_id = $2)
	`, input.ProjectID, input.SourceAnalysisID); err != nil {
		return CreateTTSTimingRevisionOutput{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE video_render_plans SET status = 'stale', active = false, updated_at = now() WHERE project_id = $1 AND storyboard_plan_id IN (SELECT id FROM storyboard_plans WHERE timing_analysis_id = $2) AND status NOT IN ('archived', 'cancelled')`, input.ProjectID, input.SourceAnalysisID); err != nil {
		return CreateTTSTimingRevisionOutput{}, err
	}
	if err := production.MarkFinalVideoStale(ctx, tx, input.ProjectID, ""); err != nil {
		return CreateTTSTimingRevisionOutput{}, err
	}
	output := CreateTTSTimingRevisionOutput{
		SourceAnalysisID: input.SourceAnalysisID, TimingAnalysisID: newAnalysisID, Revision: revision,
		EstimatedDurationTicks: totalTicks, TimelineTimebase: timelineTimebase, TTSUnitCount: ttsUnitCount,
		StaleStoryboardCount: staleTag.RowsAffected(), AudioConfigurationRevision: input.AudioConfigurationRevision,
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "audio.tts.timing_revision.created", "script_timing_analysis", newAnalysisID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "scriptEpisodeId": input.ScriptEpisodeID,
		"sourceTimingAnalysisId": input.SourceAnalysisID, "timingAnalysisId": newAnalysisID,
		"durationTicks": totalTicks, "ttsUnitCount": ttsUnitCount, "audioConfigurationRevision": input.AudioConfigurationRevision,
	})); err != nil {
		return CreateTTSTimingRevisionOutput{}, err
	}
	if _, err := completeNodeRunTx(ctx, tx, execution, mustJSON(output)); err != nil {
		return CreateTTSTimingRevisionOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CreateTTSTimingRevisionOutput{}, err
	}
	return output, nil
}

func reflowTTSTimingUnits(units []timingRevisionUnit) (totalTicks, minimumTicks int64) {
	cursor := int64(0)
	for index := 0; index < len(units); {
		group := strings.TrimSpace(units[index].ParallelGroup)
		sceneID := units[index].ScriptSceneID
		end := index + 1
		if group != "" {
			for end < len(units) && units[end].ScriptSceneID == sceneID && strings.TrimSpace(units[end].ParallelGroup) == group {
				end++
			}
		}
		blockDuration := int64(0)
		blockMinimum := int64(0)
		for unitIndex := index; unitIndex < end; unitIndex++ {
			unit := &units[unitIndex]
			unit.StartTick = cursor
			unit.EndTick = cursor + unit.DurationTicks
			if unit.DurationTicks > blockDuration {
				blockDuration = unit.DurationTicks
			}
			minimum := unit.DurationTicks
			if unit.MinDurationTicks.Valid {
				minimum = unit.MinDurationTicks.Int64
			}
			if minimum > blockMinimum {
				blockMinimum = minimum
			}
		}
		cursor += blockDuration
		minimumTicks += blockMinimum
		index = end
	}
	return cursor, minimumTicks
}

func mergeTimingUnitMetadata(raw json.RawMessage, values map[string]any) json.RawMessage {
	metadata := map[string]any{}
	_ = json.Unmarshal(raw, &metadata)
	for key, value := range values {
		metadata[key] = value
	}
	return mustJSON(metadata)
}

func isTTSSpeechUnitType(value string) bool {
	switch value {
	case "dialogue", "voiceover", "narration", "system":
		return true
	default:
		return false
	}
}

func nullableSQLInt64(value sql.NullInt64) any {
	if value.Valid {
		return value.Int64
	}
	return nil
}

func nullableSQLFloat64(value sql.NullFloat64) any {
	if value.Valid {
		return value.Float64
	}
	return nil
}

func uniqueWorkflowStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func gatewayAudioActivityTimeoutMS() int {
	return 10 * 60 * 1000
}

func (a Activities) CompleteEpisodeAudioProductionWorkflow(ctx context.Context, input EpisodeAudioProductionInput, output EpisodeAudioProductionOutput) error {
	status := output.Status
	if status != "succeeded" && status != "partial_succeeded" && status != "failed" {
		status = "failed"
	}
	code, message := "", ""
	if status == "failed" {
		code, message = "AUDIO_PRODUCTION_FAILED", "分集音频生产失败"
	}
	return TransitionWorkflowRun(ctx, a.db, input.WorkflowRunID, status, code, message, mustJSON(output))
}
