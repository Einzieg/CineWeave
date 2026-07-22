package workflows

import (
	"context"
	"database/sql"
	"fmt"
	"path"
	"strings"

	mediapkg "github.com/Einzieg/cineweave/internal/media"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/google/uuid"
)

type AudioMixAdditionalTrack struct {
	TrackKind     string   `json:"trackKind"`
	ArtifactID    string   `json:"artifactId,omitempty"`
	MediaFileID   string   `json:"mediaFileId,omitempty"`
	StorageKey    string   `json:"storageKey,omitempty"`
	StartTick     int64    `json:"startTick"`
	EndTick       int64    `json:"endTick"`
	TrimStartTick int64    `json:"trimStartTick,omitempty"`
	TrimEndTick   *int64   `json:"trimEndTick,omitempty"`
	GainDB        *float64 `json:"gainDb,omitempty"`
	FadeInTicks   int64    `json:"fadeInTicks,omitempty"`
	FadeOutTicks  int64    `json:"fadeOutTicks,omitempty"`
}

type ComposeEpisodeAudioMixInput struct {
	OrganizationID             string                    `json:"organizationId"`
	ProjectID                  string                    `json:"projectId"`
	WorkflowRunID              string                    `json:"workflowRunId"`
	CreatedBy                  string                    `json:"createdBy"`
	ScriptEpisodeID            string                    `json:"scriptEpisodeId"`
	TimingAnalysisID           string                    `json:"timingAnalysisId"`
	AudioConfigurationRevision int                       `json:"audioConfigurationRevision"`
	AdditionalTracks           []AudioMixAdditionalTrack `json:"additionalTracks,omitempty"`
}

type ComposeEpisodeAudioMixOutput struct {
	Status                     string  `json:"status"`
	AudioConfigurationRevision int     `json:"audioConfigurationRevision"`
	AudioMixVersionID          string  `json:"audioMixVersionId"`
	Revision                   int     `json:"revision"`
	ArtifactID                 string  `json:"artifactId"`
	MediaFileID                string  `json:"mediaFileId"`
	StorageKey                 string  `json:"storageKey"`
	MimeType                   string  `json:"mimeType"`
	DurationTicks              int64   `json:"durationTicks"`
	DurationSeconds            float64 `json:"durationSeconds"`
	SampleRate                 int     `json:"sampleRate"`
	ChannelCount               int     `json:"channelCount"`
	TrackCount                 int     `json:"trackCount"`
	ProductionReadiness        string  `json:"productionReadiness"`
}

type audioMixClipRecord struct {
	ID, TrackKind, SourceKind, TimingUnitID, TTSClipID, RenderSegmentID string
	ArtifactID, MediaFileID, StorageKey, MimeType                       string
	Ordinal                                                             int
	StartTick, EndTick, TrimStartTick                                   int64
	TrimEndTick                                                         sql.NullInt64
	GainDB                                                              float64
	FadeInTicks, FadeOutTicks                                           int64
	SourceDurationSeconds                                               float64
}

func (a Activities) ComposeEpisodeAudioMix(ctx context.Context, input ComposeEpisodeAudioMixInput) (_ ComposeEpisodeAudioMixOutput, err error) {
	var execution NodeExecution
	defer func() {
		err = finalizeWorkflowActivityError(ctx, a.db, execution, err)
	}()
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.ScriptEpisodeID) == "" || strings.TrimSpace(input.TimingAnalysisID) == "" {
		return ComposeEpisodeAudioMixOutput{}, fmt.Errorf("organizationId, projectId, workflowRunId, scriptEpisodeId, and timingAnalysisId are required")
	}
	objectStore, ok := a.storage.(mediapkg.ObjectStore)
	if !ok {
		return ComposeEpisodeAudioMixOutput{}, fmt.Errorf("object storage does not support audio mixing")
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID, input.WorkflowRunID)
	if err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	}
	var timebase, durationTicks int64
	var audioConfigurationRevision int
	if err := a.db.QueryRow(ctx, `
		SELECT analysis.timeline_timebase, analysis.estimated_duration_ticks,
		       project.audio_configuration_revision
		FROM script_timing_analyses analysis JOIN projects project ON project.id = analysis.project_id
		WHERE analysis.organization_id = $1 AND analysis.project_id = $2 AND analysis.script_episode_id = $3 AND analysis.id = $4 AND analysis.status = 'ready'
	`, input.OrganizationID, input.ProjectID, input.ScriptEpisodeID, input.TimingAnalysisID).Scan(&timebase, &durationTicks, &audioConfigurationRevision); err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	}
	if timebase != project.TimelineTimebase {
		return ComposeEpisodeAudioMixOutput{}, fmt.Errorf("%s: timing analysis belongs to a different production configuration", videoproduction.CodeGenerationMismatch)
	}
	audioStrategy := project.AudioStrategy
	if input.AudioConfigurationRevision <= 0 {
		input.AudioConfigurationRevision = audioConfigurationRevision
	}
	if input.AudioConfigurationRevision != audioConfigurationRevision {
		return ComposeEpisodeAudioMixOutput{}, fmt.Errorf("%s: audio configuration changed before mixing", codeAudioConfigurationChanged)
	}
	clips, err := a.episodeTTSAudioMixClips(ctx, input, timebase, audioConfigurationRevision)
	if err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	}
	additional, err := a.resolveAdditionalAudioMixClips(ctx, input, timebase, durationTicks, len(clips))
	if err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	}
	clips = append(clips, additional...)
	if len(clips) == 0 {
		return ComposeEpisodeAudioMixOutput{}, fmt.Errorf("no audio tracks are available for this episode")
	}
	execution, err = StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKeyForID(fmt.Sprintf("audio_mix_r%d", audioConfigurationRevision), input.ScriptEpisodeID),
		NodeType:       "audio.mix.compose",
		Input:          mustJSON(input),
	})
	if err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	}
	var revision int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision), 0) + 1 FROM audio_mix_versions WHERE project_id = $1 AND script_episode_id = $2`, input.ProjectID, input.ScriptEpisodeID).Scan(&revision); err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	}
	var storyboardPlanID sql.NullString
	_ = tx.QueryRow(ctx, `SELECT id::text FROM storyboard_plans WHERE project_id = $1 AND script_episode_id = $2 AND active = true ORDER BY revision DESC LIMIT 1`, input.ProjectID, input.ScriptEpisodeID).Scan(&storyboardPlanID)
	var mixID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO audio_mix_versions(
			organization_id, project_id, script_episode_id, storyboard_plan_id, timing_analysis_id,
			workflow_run_id, revision, audio_configuration_revision, status, active, audio_strategy, timeline_timebase,
			duration_ticks, sample_rate, channel_count, production_readiness, created_by, metadata
		)
		VALUES ($1, $2, $3, NULLIF($4, '')::uuid, $5, NULLIF($6, '')::uuid, $7, $12, 'mixing', false,
		        CASE WHEN $8 = 'native_av' THEN 'hybrid' ELSE $8 END, $9, $10, 48000, 2, 'blocked', NULLIF($11, '')::uuid,
		        jsonb_build_object('sourceAudioStrategy', $8::text, 'audioConfigurationRevision', $12::integer))
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, input.ScriptEpisodeID, nullableStringValue(storyboardPlanID), input.TimingAnalysisID,
		input.WorkflowRunID, revision, audioStrategy, timebase, durationTicks, input.CreatedBy, audioConfigurationRevision).Scan(&mixID); err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	}
	for index := range clips {
		clip := &clips[index]
		clip.ID = uuid.NewString()
		clip.Ordinal = index
		if _, err := tx.Exec(ctx, `
			INSERT INTO audio_mix_clips(
				id, audio_mix_version_id, track_kind, source_kind, timing_unit_id, tts_audio_clip_id,
				video_render_segment_id, artifact_id, media_file_id, storage_key, ordinal,
				start_tick, end_tick, trim_start_tick, trim_end_tick, gain_db, fade_in_ticks, fade_out_ticks,
				metadata
			)
			VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid, NULLIF($6, '')::uuid, NULLIF($7, '')::uuid,
			        NULLIF($8, '')::uuid, NULLIF($9, '')::uuid, NULLIF($10, ''), $11, $12, $13, $14, $15,
			        $16, $17, $18, jsonb_build_object('mimeType', $19::text, 'sourceDurationSeconds', $20::float8))
		`, clip.ID, mixID, clip.TrackKind, clip.SourceKind, clip.TimingUnitID, clip.TTSClipID, clip.RenderSegmentID,
			clip.ArtifactID, clip.MediaFileID, clip.StorageKey, clip.Ordinal, clip.StartTick, clip.EndTick,
			clip.TrimStartTick, nullableSQLInt64(clip.TrimEndTick), clip.GainDB, clip.FadeInTicks, clip.FadeOutTicks,
			clip.MimeType, clip.SourceDurationSeconds); err != nil {
			return ComposeEpisodeAudioMixOutput{}, err
		}
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "audio.mix.started", "audio_mix_version", mixID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "scriptEpisodeId": input.ScriptEpisodeID, "trackCount": len(clips),
	})); err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	}
	if applied, err := progressNodeRunTx(ctx, tx, execution, mustJSON(map[string]any{"audioMixVersionId": mixID, "status": "mixing"})); err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	} else if !applied {
		return ComposeEpisodeAudioMixOutput{}, ErrWorkflowWriteFenced
	}
	if err := tx.Commit(ctx); err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	}

	mediaTracks := make([]mediapkg.AudioMixTrack, 0, len(clips))
	for _, clip := range clips {
		trimEndSeconds := 0.0
		if clip.TrimEndTick.Valid {
			trimEndSeconds = float64(clip.TrimEndTick.Int64) / float64(timebase)
		}
		mediaTracks = append(mediaTracks, mediapkg.AudioMixTrack{
			ID: clip.ID, Kind: clip.TrackKind, StorageKey: clip.StorageKey, MimeType: clip.MimeType,
			StartSeconds: float64(clip.StartTick) / float64(timebase), SourceDurationSeconds: clip.SourceDurationSeconds,
			TrimStartSeconds: float64(clip.TrimStartTick) / float64(timebase), TrimEndSeconds: trimEndSeconds,
			GainDB: clip.GainDB, FadeInSeconds: float64(clip.FadeInTicks) / float64(timebase),
			FadeOutSeconds: float64(clip.FadeOutTicks) / float64(timebase),
		})
	}
	storageKey := path.Join("org", input.OrganizationID, "project", input.ProjectID, "audio-mixes", mixID+".m4a")
	result, err := mediapkg.MixAudioTracksWithStore(ctx, mediapkg.AudioMixRequest{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		Tracks: mediaTracks, DurationSeconds: float64(durationTicks) / float64(timebase), SampleRate: 48000,
		ChannelCount: 2, OutputStorageKey: storageKey,
	}, objectStore)
	if err != nil {
		_ = a.failAudioMix(ctx, input, execution, mixID, err)
		return ComposeEpisodeAudioMixOutput{}, err
	}
	artifactID := uuid.NewString()
	mediaFileID := uuid.NewString()
	tx, err = a.db.Begin(ctx)
	if err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	}
	var currentAudioConfigurationRevision int
	if err := tx.QueryRow(ctx, `SELECT audio_configuration_revision FROM projects WHERE id = $1 FOR SHARE`, input.ProjectID).Scan(&currentAudioConfigurationRevision); err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	}
	metadata := mustJSON(map[string]any{
		"audioMixVersionId": mixID, "scriptEpisodeId": input.ScriptEpisodeID, "timingAnalysisId": input.TimingAnalysisID,
		"trackCount": len(clips), "audioProbe": result.Probe, "audioConfigurationRevision": audioConfigurationRevision,
	})
	if _, err := tx.Exec(ctx, `
		INSERT INTO artifacts(id, organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, metadata, created_by)
		VALUES ($1, $2, $3, NULLIF($4, '')::uuid, $5, 'episode_audio_mix', $6, 'audio/mp4', $7, $8, NULLIF($9, '')::uuid)
	`, artifactID, input.OrganizationID, input.ProjectID, input.WorkflowRunID, execution.NodeRunID, result.Put.StorageKey, result.Put.ContentHash, metadata, input.CreatedBy); err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO media_files(id, organization_id, project_id, artifact_id, storage_key, mime_type, byte_size, duration_seconds, checksum, metadata, created_by)
		VALUES ($1, $2, $3, $4, $5, 'audio/mp4', $6, $7, $8, $9, NULLIF($10, '')::uuid)
	`, mediaFileID, input.OrganizationID, input.ProjectID, artifactID, result.Put.StorageKey, result.Put.ByteSize,
		result.Probe.DurationSeconds, result.Put.ContentHash, metadata, input.CreatedBy); err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	}
	if currentAudioConfigurationRevision != audioConfigurationRevision {
		if _, err := tx.Exec(ctx, `
			UPDATE audio_mix_versions
			SET status = 'stale', active = false, artifact_id = $2, media_file_id = $3,
			    storage_key = $4, mime_type = 'audio/mp4', production_readiness = 'blocked',
			    track_summary = $5,
			    metadata = metadata || jsonb_build_object(
			      'audioProbe', $6::jsonb, 'discardedAt', now(),
			      'currentAudioConfigurationRevision', $7::integer
			    ), updated_at = now(), completed_at = now()
			WHERE id = $1
		`, mixID, artifactID, mediaFileID, result.Put.StorageKey, audioMixTrackSummary(clips), mustJSON(result.Probe), currentAudioConfigurationRevision); err != nil {
			return ComposeEpisodeAudioMixOutput{}, err
		}
		output := ComposeEpisodeAudioMixOutput{
			Status: "stale", AudioConfigurationRevision: audioConfigurationRevision,
			AudioMixVersionID: mixID, Revision: revision, ArtifactID: artifactID, MediaFileID: mediaFileID,
			StorageKey: result.Put.StorageKey, MimeType: "audio/mp4", DurationTicks: durationTicks,
			DurationSeconds: result.Probe.DurationSeconds, SampleRate: result.Probe.AudioSampleRate,
			ChannelCount: result.Probe.AudioChannelCount, TrackCount: len(clips), ProductionReadiness: "blocked",
		}
		if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "audio.mix.discarded", "audio_mix_version", mixID, mustJSON(map[string]any{
			"workflowRunId": input.WorkflowRunID, "scriptEpisodeId": input.ScriptEpisodeID,
			"audioConfigurationRevision":        audioConfigurationRevision,
			"currentAudioConfigurationRevision": currentAudioConfigurationRevision, "output": output,
		})); err != nil {
			return ComposeEpisodeAudioMixOutput{}, err
		}
		if applied, err := completeNodeRunTx(ctx, tx, execution, mustJSON(output)); err != nil {
			return ComposeEpisodeAudioMixOutput{}, err
		} else if !applied {
			return ComposeEpisodeAudioMixOutput{}, ErrWorkflowWriteFenced
		}
		if err := tx.Commit(ctx); err != nil {
			return ComposeEpisodeAudioMixOutput{}, err
		}
		return output, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE audio_mix_versions SET active = false, status = CASE WHEN status = 'ready' THEN 'archived' ELSE status END, updated_at = now() WHERE project_id = $1 AND script_episode_id = $2 AND id <> $3 AND active = true`, input.ProjectID, input.ScriptEpisodeID, mixID); err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE audio_mix_versions SET status = 'ready', active = true, artifact_id = $2, media_file_id = $3,
		       storage_key = $4, mime_type = 'audio/mp4', production_readiness = 'ready',
		       track_summary = $5, metadata = metadata || jsonb_build_object('audioProbe', $6::jsonb),
		       updated_at = now(), completed_at = now() WHERE id = $1
	`, mixID, artifactID, mediaFileID, result.Put.StorageKey, audioMixTrackSummary(clips), mustJSON(result.Probe)); err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE projects SET active_audio_mix_version_id = $2, updated_at = now() WHERE id = $1`, input.ProjectID, mixID); err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	}
	output := ComposeEpisodeAudioMixOutput{
		Status: "ready", AudioConfigurationRevision: audioConfigurationRevision,
		AudioMixVersionID: mixID, Revision: revision, ArtifactID: artifactID, MediaFileID: mediaFileID,
		StorageKey: result.Put.StorageKey, MimeType: "audio/mp4", DurationTicks: durationTicks,
		DurationSeconds: result.Probe.DurationSeconds, SampleRate: result.Probe.AudioSampleRate,
		ChannelCount: result.Probe.AudioChannelCount, TrackCount: len(clips), ProductionReadiness: "ready",
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "audio.mix.completed", "audio_mix_version", mixID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "scriptEpisodeId": input.ScriptEpisodeID, "output": output,
	})); err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	}
	if applied, err := completeNodeRunTx(ctx, tx, execution, mustJSON(output)); err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	} else if !applied {
		return ComposeEpisodeAudioMixOutput{}, ErrWorkflowWriteFenced
	}
	if err := tx.Commit(ctx); err != nil {
		return ComposeEpisodeAudioMixOutput{}, err
	}
	return output, nil
}

func (a Activities) failAudioMix(ctx context.Context, input ComposeEpisodeAudioMixInput, execution NodeExecution, mixID string, cause error) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE audio_mix_versions
		SET status = 'failed', active = false,
		    metadata = metadata || jsonb_build_object('errorMessage', $2::text),
		    updated_at = now(), completed_at = now()
		WHERE id = $1 AND status = 'mixing'
	`, mixID, cause.Error()); err != nil {
		return err
	}
	if applied, err := failNodeRunTx(ctx, tx, execution, codeActivityFailed, cause.Error(), mustJSON(map[string]any{"audioMixVersionId": mixID})); err != nil {
		return err
	} else if !applied {
		return ErrWorkflowWriteFenced
	}
	return tx.Commit(ctx)
}

func (a Activities) episodeTTSAudioMixClips(ctx context.Context, input ComposeEpisodeAudioMixInput, timebase int64, audioConfigurationRevision int) ([]audioMixClipRecord, error) {
	rows, err := a.db.Query(ctx, `
		SELECT unit.id::text, unit.unit_type, unit.start_tick, unit.end_tick,
		       clip.id::text, clip.artifact_id::text, clip.media_file_id::text, clip.storage_key, clip.mime_type,
		       clip.duration_ticks, clip.timeline_timebase
		FROM script_timing_units unit
		JOIN tts_audio_clips clip ON clip.id = unit.source_tts_audio_clip_id AND clip.status = 'succeeded'
		 AND clip.active = true AND clip.audio_configuration_revision = $2
		WHERE unit.timing_analysis_id = $1 AND unit.track = 'audio'
		ORDER BY unit.unit_ordinal
	`, input.TimingAnalysisID, audioConfigurationRevision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]audioMixClipRecord, 0)
	for rows.Next() {
		var clip audioMixClipRecord
		var durationTicks, clipTimebase int64
		if err := rows.Scan(&clip.TimingUnitID, &clip.TrackKind, &clip.StartTick, &clip.EndTick,
			&clip.TTSClipID, &clip.ArtifactID, &clip.MediaFileID, &clip.StorageKey, &clip.MimeType,
			&durationTicks, &clipTimebase); err != nil {
			return nil, err
		}
		clip.SourceKind = "tts_clip"
		clip.GainDB = defaultAudioTrackGain(clip.TrackKind)
		clip.SourceDurationSeconds = float64(durationTicks) / float64(clipTimebase)
		result = append(result, clip)
	}
	return result, rows.Err()
}

func (a Activities) resolveAdditionalAudioMixClips(ctx context.Context, input ComposeEpisodeAudioMixInput, timebase, mixDurationTicks int64, ordinalOffset int) ([]audioMixClipRecord, error) {
	result := make([]audioMixClipRecord, 0, len(input.AdditionalTracks))
	for index, source := range input.AdditionalTracks {
		if !validAdditionalAudioTrackKind(source.TrackKind) || source.EndTick <= source.StartTick || source.StartTick < 0 || source.EndTick > mixDurationTicks || source.TrimStartTick < 0 || source.FadeInTicks < 0 || source.FadeOutTicks < 0 {
			return nil, fmt.Errorf("additional audio track %d is invalid", index)
		}
		if source.TrimEndTick != nil && *source.TrimEndTick <= source.TrimStartTick {
			return nil, fmt.Errorf("additional audio track %d trim range is invalid", index)
		}
		var clip audioMixClipRecord
		clip.TrackKind = source.TrackKind
		clip.SourceKind = "artifact"
		clip.Ordinal = ordinalOffset + index
		clip.StartTick, clip.EndTick, clip.TrimStartTick = source.StartTick, source.EndTick, source.TrimStartTick
		if source.TrimEndTick != nil {
			clip.TrimEndTick = sql.NullInt64{Int64: *source.TrimEndTick, Valid: true}
		}
		clip.FadeInTicks, clip.FadeOutTicks = source.FadeInTicks, source.FadeOutTicks
		clip.GainDB = defaultAudioTrackGain(source.TrackKind)
		if source.GainDB != nil {
			clip.GainDB = *source.GainDB
		}
		switch {
		case source.MediaFileID != "":
			err := a.db.QueryRow(ctx, `SELECT id::text, COALESCE(artifact_id::text, ''), storage_key, mime_type, COALESCE(duration_seconds::float8, 0) FROM media_files WHERE organization_id = $1 AND project_id = $2 AND id = $3`, input.OrganizationID, input.ProjectID, source.MediaFileID).
				Scan(&clip.MediaFileID, &clip.ArtifactID, &clip.StorageKey, &clip.MimeType, &clip.SourceDurationSeconds)
			if err != nil {
				return nil, err
			}
		case source.ArtifactID != "":
			err := a.db.QueryRow(ctx, `
				SELECT artifact.id::text, COALESCE(media.id::text, ''), artifact.storage_key, artifact.mime_type, COALESCE(media.duration_seconds::float8, 0)
				FROM artifacts artifact LEFT JOIN LATERAL (SELECT * FROM media_files WHERE artifact_id = artifact.id ORDER BY created_at DESC LIMIT 1) media ON true
				WHERE artifact.organization_id = $1 AND artifact.project_id = $2 AND artifact.id = $3 AND artifact.storage_key IS NOT NULL
			`, input.OrganizationID, input.ProjectID, source.ArtifactID).Scan(&clip.ArtifactID, &clip.MediaFileID, &clip.StorageKey, &clip.MimeType, &clip.SourceDurationSeconds)
			if err != nil {
				return nil, err
			}
		case source.StorageKey != "":
			err := a.db.QueryRow(ctx, `SELECT COALESCE(artifact_id::text, ''), id::text, storage_key, mime_type, COALESCE(duration_seconds::float8, 0) FROM media_files WHERE organization_id = $1 AND project_id = $2 AND storage_key = $3 ORDER BY created_at DESC LIMIT 1`, input.OrganizationID, input.ProjectID, source.StorageKey).
				Scan(&clip.ArtifactID, &clip.MediaFileID, &clip.StorageKey, &clip.MimeType, &clip.SourceDurationSeconds)
			if err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("additional audio track %d has no registered media source", index)
		}
		if clip.SourceDurationSeconds <= 0 {
			clip.SourceDurationSeconds = float64(source.EndTick-source.StartTick) / float64(timebase)
		}
		sourceDurationTicks := int64(clip.SourceDurationSeconds * float64(timebase))
		if source.TrimStartTick >= sourceDurationTicks || (source.TrimEndTick != nil && *source.TrimEndTick > sourceDurationTicks) {
			return nil, fmt.Errorf("additional audio track %d trim range exceeds the registered media duration", index)
		}
		result = append(result, clip)
	}
	return result, nil
}

func validAdditionalAudioTrackKind(value string) bool {
	switch value {
	case "ambience", "sfx", "music", "native":
		return true
	default:
		return false
	}
}

func defaultAudioTrackGain(kind string) float64 {
	switch kind {
	case "ambience":
		return -12
	case "sfx":
		return -6
	case "music":
		return -16
	case "native":
		return -8
	default:
		return 0
	}
}

func audioMixTrackSummary(clips []audioMixClipRecord) map[string]int {
	result := map[string]int{}
	for _, clip := range clips {
		result[clip.TrackKind]++
	}
	return result
}
