package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/audioquality"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type NativeAudioReviewWorkflowInput struct {
	OrganizationID    string `json:"organizationId"`
	ProjectID         string `json:"projectId"`
	WorkflowRunID     string `json:"workflowRunId"`
	CreatedBy         string `json:"createdBy"`
	StoryboardShotID  string `json:"storyboardShotId"`
	VideoRenderPlanID string `json:"videoRenderPlanId,omitempty"`
	MaxConcurrency    int    `json:"maxConcurrency,omitempty"`
}

type NativeAudioReviewJob struct {
	ReviewID        string `json:"reviewId"`
	RenderPlanID    string `json:"renderPlanId"`
	RenderSegmentID string `json:"renderSegmentId"`
	SegmentIndex    int    `json:"segmentIndex"`
	AudioArtifactID string `json:"audioArtifactId"`
}

type PrepareNativeAudioReviewOutput struct {
	RenderPlanID      string                 `json:"renderPlanId"`
	Jobs              []NativeAudioReviewJob `json:"jobs"`
	SkippedSegmentIDs []string               `json:"skippedSegmentIds"`
}

type ReviewNativeAudioSegmentInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId"`
	CreatedBy      string `json:"createdBy"`
	ReviewID       string `json:"reviewId"`
}

type ReviewNativeAudioSegmentOutput struct {
	ReviewID            string  `json:"reviewId"`
	RenderSegmentID     string  `json:"renderSegmentId"`
	Status              string  `json:"status"`
	Transcript          string  `json:"transcript,omitempty"`
	Language            string  `json:"language,omitempty"`
	DialogueCoverage    float64 `json:"dialogueCoverage"`
	TextAccuracy        float64 `json:"textAccuracy"`
	TimingAccuracy      float64 `json:"timingAccuracy"`
	SpeakerTurnAccuracy float64 `json:"speakerTurnAccuracy"`
	ProviderCallID      string  `json:"providerCallId,omitempty"`
	ModelID             string  `json:"modelId,omitempty"`
	ErrorCode           string  `json:"errorCode,omitempty"`
	ErrorMessage        string  `json:"errorMessage,omitempty"`
}

type NativeAudioReviewWorkflowOutput struct {
	Status            string                           `json:"status"`
	RenderPlanID      string                           `json:"renderPlanId"`
	PassedSegmentIDs  []string                         `json:"passedSegmentIds"`
	FailedSegmentIDs  []string                         `json:"failedSegmentIds"`
	SkippedSegmentIDs []string                         `json:"skippedSegmentIds"`
	Reviews           []ReviewNativeAudioSegmentOutput `json:"reviews"`
	Errors            map[string]string                `json:"errors"`
}

type nativeAudioReviewRecord struct {
	ID, PlanID, SegmentID, ShotID, AudioArtifactID, ASRProfileKey, Status string
	Expected                                                              json.RawMessage
	Timebase                                                              int64
	PlannedDurationTicks                                                  int64
	AudioDurationSeconds                                                  float64
	AudioConfigurationRevision, CurrentAudioConfigurationRevision         int
}

func NativeAudioReviewWorkflow(ctx workflow.Context, input NativeAudioReviewWorkflowInput) (NativeAudioReviewWorkflowOutput, error) {
	options := defaultActivityOptions()
	options.StartToCloseTimeout = 30 * time.Minute
	options.RetryPolicy = &temporal.RetryPolicy{InitialInterval: time.Second, BackoffCoefficient: 2, MaximumInterval: 30 * time.Second, MaximumAttempts: 3}
	ctx = workflow.WithActivityOptions(ctx, options)
	var prepared PrepareNativeAudioReviewOutput
	if err := workflow.ExecuteActivity(ctx, "PrepareNativeAudioReview", input).Get(ctx, &prepared); err != nil {
		return NativeAudioReviewWorkflowOutput{}, err
	}
	output := NativeAudioReviewWorkflowOutput{
		Status: "running", RenderPlanID: prepared.RenderPlanID, SkippedSegmentIDs: prepared.SkippedSegmentIDs, Errors: map[string]string{},
	}
	concurrency := input.MaxConcurrency
	if concurrency <= 0 {
		concurrency = 5
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
			futures = append(futures, workflow.ExecuteActivity(ctx, "ReviewNativeAudioSegment", ReviewNativeAudioSegmentInput{
				OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
				CreatedBy: input.CreatedBy, ReviewID: job.ReviewID,
			}))
		}
		for index, future := range futures {
			job := prepared.Jobs[start+index]
			var review ReviewNativeAudioSegmentOutput
			if err := future.Get(ctx, &review); err != nil {
				review = ReviewNativeAudioSegmentOutput{ReviewID: job.ReviewID, RenderSegmentID: job.RenderSegmentID, Status: "failed", ErrorCode: codeActivityFailed, ErrorMessage: err.Error()}
			}
			output.Reviews = append(output.Reviews, review)
			if review.Status == "passed" {
				output.PassedSegmentIDs = append(output.PassedSegmentIDs, review.RenderSegmentID)
			} else {
				output.FailedSegmentIDs = append(output.FailedSegmentIDs, review.RenderSegmentID)
				output.Errors[review.RenderSegmentID] = firstNonEmptyString(review.ErrorMessage, "原生音轨对白审核未通过")
			}
		}
	}
	if len(output.FailedSegmentIDs) > 0 && len(output.PassedSegmentIDs) > 0 {
		output.Status = "partial_succeeded"
	} else if len(output.FailedSegmentIDs) > 0 {
		output.Status = "failed"
	} else {
		output.Status = "succeeded"
	}
	if err := workflow.ExecuteActivity(ctx, "RefreshTimingCalibrationProfile", RefreshTimingCalibrationProfileInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
	}).Get(ctx, nil); err != nil {
		return NativeAudioReviewWorkflowOutput{}, err
	}
	if err := workflow.ExecuteActivity(ctx, "CompleteNativeAudioReviewWorkflow", input, output).Get(ctx, nil); err != nil {
		return NativeAudioReviewWorkflowOutput{}, err
	}
	return output, nil
}

func (a Activities) PrepareNativeAudioReview(ctx context.Context, input NativeAudioReviewWorkflowInput) (PrepareNativeAudioReviewOutput, error) {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.StoryboardShotID) == "" {
		return PrepareNativeAudioReviewOutput{}, fmt.Errorf("organizationId, projectId, workflowRunId, and storyboardShotId are required")
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return PrepareNativeAudioReviewOutput{}, err
	}
	defer tx.Rollback(ctx)
	runCtx, err := lockWorkflowBusinessWrite(ctx, tx, input.WorkflowRunID)
	if err != nil {
		return PrepareNativeAudioReviewOutput{}, err
	}
	planID := strings.TrimSpace(input.VideoRenderPlanID)
	query := `SELECT id::text FROM video_render_plans WHERE organization_id = $1 AND project_id = $2 AND storyboard_shot_id = $3 AND production_generation_id = $4`
	args := []any{input.OrganizationID, input.ProjectID, input.StoryboardShotID, runCtx.ProductionGenerationID}
	if planID != "" {
		query += " AND id = $5"
		args = append(args, planID)
	} else {
		query += " AND active = true"
	}
	query += " ORDER BY created_at DESC LIMIT 1"
	if err := tx.QueryRow(ctx, query, args...).Scan(&planID); err != nil {
		return PrepareNativeAudioReviewOutput{}, err
	}
	var audioConfigurationRevision int
	if err := tx.QueryRow(ctx, `SELECT audio_configuration_revision FROM projects WHERE organization_id = $1 AND id = $2`, input.OrganizationID, input.ProjectID).Scan(&audioConfigurationRevision); err != nil {
		return PrepareNativeAudioReviewOutput{}, err
	}
	rows, err := a.db.Query(ctx, `
		SELECT segment.id::text, segment.segment_index, segment.extracted_audio_artifact_id::text, segment.dialogue,
		       COALESCE(review.id::text, '')
		FROM video_render_segments segment
		LEFT JOIN LATERAL (
		  SELECT id FROM native_audio_reviews existing
		  WHERE existing.video_render_segment_id = segment.id AND existing.audio_configuration_revision = $2
		  ORDER BY revision DESC LIMIT 1
		) review ON true
		WHERE segment.video_render_plan_id = $1 AND segment.status = 'succeeded'
		  AND segment.production_generation_id = $3
		  AND segment.native_audio_requested = true AND COALESCE(segment.native_audio_detected, false) = true
		  AND segment.extracted_audio_artifact_id IS NOT NULL
		ORDER BY segment.segment_index
	`, planID, audioConfigurationRevision, runCtx.ProductionGenerationID)
	if err != nil {
		return PrepareNativeAudioReviewOutput{}, err
	}
	defer rows.Close()
	output := PrepareNativeAudioReviewOutput{RenderPlanID: planID}
	for rows.Next() {
		var segmentID, artifactID, existingReviewID string
		var segmentIndex int
		var dialogue json.RawMessage
		if err := rows.Scan(&segmentID, &segmentIndex, &artifactID, &dialogue, &existingReviewID); err != nil {
			return PrepareNativeAudioReviewOutput{}, err
		}
		if existingReviewID != "" {
			var status string
			if err := tx.QueryRow(ctx, `SELECT status FROM native_audio_reviews WHERE id = $1`, existingReviewID).Scan(&status); err == nil && (status == "passed" || status == "manual_override") {
				output.SkippedSegmentIDs = append(output.SkippedSegmentIDs, segmentID)
				continue
			}
		}
		var revision int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision), 0) + 1 FROM native_audio_reviews WHERE video_render_segment_id = $1`, segmentID).Scan(&revision); err != nil {
			return PrepareNativeAudioReviewOutput{}, err
		}
		var reviewID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO native_audio_reviews(
				organization_id, project_id, video_render_plan_id, video_render_segment_id, workflow_run_id,
				revision, audio_configuration_revision, status, expected_dialogue, metadata,
				production_generation_id
			)
			VALUES ($1, $2, $3, $4, $5, $6, $10, 'pending', $7,
			        jsonb_build_object('audioArtifactId', $8::uuid::text, 'segmentIndex', $9::integer,
			                           'audioConfigurationRevision', $10::integer), $11)
			RETURNING id::text
		`, input.OrganizationID, input.ProjectID, planID, segmentID, input.WorkflowRunID, revision, dialogue, artifactID,
			segmentIndex, audioConfigurationRevision, runCtx.ProductionGenerationID).Scan(&reviewID); err != nil {
			return PrepareNativeAudioReviewOutput{}, err
		}
		output.Jobs = append(output.Jobs, NativeAudioReviewJob{ReviewID: reviewID, RenderPlanID: planID, RenderSegmentID: segmentID, SegmentIndex: segmentIndex, AudioArtifactID: artifactID})
	}
	if err := rows.Err(); err != nil {
		return PrepareNativeAudioReviewOutput{}, err
	}
	if len(output.Jobs) == 0 && len(output.SkippedSegmentIDs) == 0 {
		return PrepareNativeAudioReviewOutput{}, fmt.Errorf("no generated native audio segments are available for review")
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.audio.review.prepared", "video_render_plan", planID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "storyboardShotId": input.StoryboardShotID, "planId": planID,
		"jobCount": len(output.Jobs), "skippedCount": len(output.SkippedSegmentIDs), "audioConfigurationRevision": audioConfigurationRevision,
	})); err != nil {
		return PrepareNativeAudioReviewOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PrepareNativeAudioReviewOutput{}, err
	}
	return output, nil
}

func (a Activities) ReviewNativeAudioSegment(ctx context.Context, input ReviewNativeAudioSegmentInput) (ReviewNativeAudioSegmentOutput, error) {
	project, err := a.projectProductionSettings(ctx, input.ProjectID, input.WorkflowRunID)
	if err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	var record nativeAudioReviewRecord
	err = a.db.QueryRow(ctx, `
		SELECT review.id::text, review.video_render_plan_id::text, review.video_render_segment_id::text,
		       segment.storyboard_shot_id::text, review.expected_dialogue,
		       review.metadata->>'audioArtifactId',
		       plan.timeline_timebase, segment.planned_duration_ticks,
		       COALESCE(media.duration_seconds::float8, 0), review.audio_configuration_revision,
		       project.audio_configuration_revision, review.status
		FROM native_audio_reviews review
		JOIN video_render_segments segment ON segment.id = review.video_render_segment_id
		JOIN video_render_plans plan ON plan.id = review.video_render_plan_id
		JOIN projects project ON project.id = review.project_id
		LEFT JOIN artifacts audio ON audio.id = NULLIF(review.metadata->>'audioArtifactId', '')::uuid
		LEFT JOIN LATERAL (SELECT duration_seconds FROM media_files WHERE artifact_id = audio.id ORDER BY created_at DESC LIMIT 1) media ON true
		WHERE review.organization_id = $1 AND review.project_id = $2 AND review.id = $3
		  AND review.production_generation_id = $4
	`, input.OrganizationID, input.ProjectID, input.ReviewID, project.ProductionGenerationID).Scan(&record.ID, &record.PlanID, &record.SegmentID, &record.ShotID,
		&record.Expected, &record.AudioArtifactID, &record.Timebase, &record.PlannedDurationTicks,
		&record.AudioDurationSeconds, &record.AudioConfigurationRevision, &record.CurrentAudioConfigurationRevision, &record.Status)
	if err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	record.ASRProfileKey = project.ASRModelProfileKey
	if record.Status == "stale" || record.AudioConfigurationRevision != record.CurrentAudioConfigurationRevision {
		if record.Status != "stale" {
			_, _ = a.db.Exec(ctx, `
				UPDATE native_audio_reviews
				SET status = 'stale', error_code = $2, error_message = $3,
				    metadata = metadata || jsonb_build_object('discardedAt', now(), 'currentAudioConfigurationRevision', $4::integer),
				    updated_at = now(), completed_at = COALESCE(completed_at, now())
				WHERE id = $1
			`, record.ID, codeAudioConfigurationChanged, "音频配置已变更，该审核任务不再有效", record.CurrentAudioConfigurationRevision)
		}
		return ReviewNativeAudioSegmentOutput{ReviewID: record.ID, RenderSegmentID: record.SegmentID, Status: "stale",
			ErrorCode: codeAudioConfigurationChanged, ErrorMessage: "音频配置已变更，该审核任务不再有效"}, nil
	}
	nodeExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		NodeKey: nodeKeyForID("native_audio_review", record.SegmentID), NodeType: "audio.asr.review",
		Input: mustJSON(map[string]any{"reviewId": record.ID, "renderSegmentId": record.SegmentID}),
	})
	if err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	runningTx, err := a.db.Begin(ctx)
	if err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	defer runningTx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, runningTx, input.WorkflowRunID, nodeExecution); err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	runningTag, err := runningTx.Exec(ctx, `
		UPDATE native_audio_reviews review SET status = 'running', node_run_id = $2, updated_at = now()
		WHERE review.id = $1 AND review.status IN ('pending', 'running')
		  AND review.audio_configuration_revision = (SELECT audio_configuration_revision FROM projects WHERE id = review.project_id)
	`, record.ID, nodeExecution.NodeRunID)
	if err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	if runningTag.RowsAffected() == 0 {
		output := ReviewNativeAudioSegmentOutput{ReviewID: record.ID, RenderSegmentID: record.SegmentID, Status: "stale",
			ErrorCode: codeAudioConfigurationChanged, ErrorMessage: "音频配置已变更，该审核任务不再有效"}
		if _, err := failNodeRunTx(ctx, runningTx, nodeExecution, output.ErrorCode, output.ErrorMessage, mustJSON(output)); err != nil {
			return ReviewNativeAudioSegmentOutput{}, err
		}
		if err := runningTx.Commit(ctx); err != nil {
			return ReviewNativeAudioSegmentOutput{}, err
		}
		return output, nil
	}
	if err := runningTx.Commit(ctx); err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	var expected []provider.GatewayVideoDialogueSpan
	if err := json.Unmarshal(record.Expected, &expected); err != nil {
		return a.failNativeAudioReview(ctx, nodeExecution, input, record, provider.CodeInvalidRequest, err.Error())
	}
	var gatewayResponse provider.GatewayASRResponse
	if len(expected) > 0 {
		if a.gateway == nil {
			return a.failNativeAudioReview(ctx, nodeExecution, input, record, provider.CodeProviderGatewayRequired, "provider gateway client is not configured")
		}
		gatewayResponse, err = a.gateway.TranscribeAudio(ctx, provider.GatewayASRRequest{
			OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, NodeRunID: nodeExecution.NodeRunID,
			ModelProfileKey: record.ASRProfileKey, IdempotencyKey: fmt.Sprintf("asr:%s:c%d", record.SegmentID, record.AudioConfigurationRevision),
			Source:  provider.GatewayAudioSource{ArtifactID: record.AudioArtifactID, FileName: record.SegmentID + ".m4a"},
			Input:   mustJSON(map[string]any{"language": "zh", "response_format": "verbose_json", "timestamp_granularities": []string{"segment", "word"}}),
			Options: provider.GatewayAudioOptions{TimeoutMS: gatewayAudioActivityTimeoutMS()},
		})
		if err != nil {
			code, message := workflowErrorFields(workflowErrorFromProvider(err, codeActivityFailed), codeActivityFailed)
			return a.failNativeAudioReview(ctx, nodeExecution, input, record, code, message)
		}
	}
	expectedLines := make([]audioquality.ExpectedLine, 0, len(expected))
	for _, line := range expected {
		expectedLines = append(expectedLines, audioquality.ExpectedLine{Speaker: line.Speaker, Text: line.Text, StartTick: line.StartTick, EndTick: line.EndTick})
	}
	transcriptSegments := make([]audioquality.TranscriptSegment, 0, len(gatewayResponse.Output.Segments))
	for _, segment := range gatewayResponse.Output.Segments {
		transcriptSegments = append(transcriptSegments, audioquality.TranscriptSegment{Speaker: segment.Speaker, Text: segment.Text, Start: segment.Start, End: segment.End})
	}
	metrics := audioquality.Metrics{DialogueCoverage: 1, TextAccuracy: 1, TimingAccuracy: 1, SpeakerTurnAccuracy: 1, Passed: true}
	if len(expected) > 0 {
		metrics = audioquality.Review(expectedLines, gatewayResponse.Output.Text, transcriptSegments, record.Timebase)
	}
	status := "passed"
	errorCode, errorMessage := "", ""
	if !metrics.Passed {
		status = "failed"
		errorCode = "NATIVE_DIALOGUE_VERIFICATION_FAILED"
		errorMessage = "原生音轨中的中文台词、说话人轮次或时间窗口未通过自动审核"
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, nodeExecution); err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	var currentAudioConfigurationRevision int
	if err := tx.QueryRow(ctx, `SELECT audio_configuration_revision FROM projects WHERE id = $1 FOR SHARE`, input.ProjectID).Scan(&currentAudioConfigurationRevision); err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	if currentAudioConfigurationRevision != record.AudioConfigurationRevision {
		output := ReviewNativeAudioSegmentOutput{
			ReviewID: record.ID, RenderSegmentID: record.SegmentID, Status: "stale", Transcript: gatewayResponse.Output.Text,
			Language: gatewayResponse.Output.Language, DialogueCoverage: metrics.DialogueCoverage, TextAccuracy: metrics.TextAccuracy,
			TimingAccuracy: metrics.TimingAccuracy, SpeakerTurnAccuracy: metrics.SpeakerTurnAccuracy,
			ProviderCallID: gatewayResponse.ProviderCallID, ModelID: gatewayResponse.ModelID,
			ErrorCode: codeAudioConfigurationChanged, ErrorMessage: "音频配置已变更，审核结果已保留但不会用于生产",
		}
		if _, err := tx.Exec(ctx, `
			UPDATE native_audio_reviews
			SET status = 'stale', provider_call_id = NULLIF($2, '')::uuid, provider_model_id = NULLIF($3, '')::uuid,
			    transcript = NULLIF($4, ''), language = NULLIF($5, ''), alignment = $6,
			    dialogue_coverage = $7, text_accuracy = $8, timing_accuracy = $9, speaker_turn_accuracy = $10,
			    error_code = $11, error_message = $12,
			    metadata = metadata || jsonb_build_object('discardedAt', now(), 'currentAudioConfigurationRevision', $13::integer),
			    updated_at = now(), completed_at = now()
			WHERE id = $1
		`, record.ID, gatewayResponse.ProviderCallID, gatewayResponse.ModelID, gatewayResponse.Output.Text,
			gatewayResponse.Output.Language, mustJSON(gatewayResponse.Output.Segments), metrics.DialogueCoverage,
			metrics.TextAccuracy, metrics.TimingAccuracy, metrics.SpeakerTurnAccuracy, codeAudioConfigurationChanged,
			output.ErrorMessage, currentAudioConfigurationRevision); err != nil {
			return ReviewNativeAudioSegmentOutput{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE video_render_segments
			SET audio_verification_status = 'audio_unverified', production_readiness = 'preview_only',
			    audio_verified_by = NULL, audio_verified_at = NULL,
			    audio_verification_notes = $2, updated_at = now()
			WHERE id = $1
		`, record.SegmentID, output.ErrorMessage); err != nil {
			return ReviewNativeAudioSegmentOutput{}, err
		}
		if err := refreshWorkflowRenderPlanAudioState(ctx, tx, record.PlanID, record.ShotID); err != nil {
			return ReviewNativeAudioSegmentOutput{}, err
		}
		if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.audio.review.discarded", "native_audio_review", record.ID, mustJSON(map[string]any{
			"workflowRunId": input.WorkflowRunID, "storyboardShotId": record.ShotID,
			"audioConfigurationRevision":        record.AudioConfigurationRevision,
			"currentAudioConfigurationRevision": currentAudioConfigurationRevision, "output": output,
		})); err != nil {
			return ReviewNativeAudioSegmentOutput{}, err
		}
		if _, err := failNodeRunTx(ctx, tx, nodeExecution, codeAudioConfigurationChanged, output.ErrorMessage, mustJSON(output)); err != nil {
			return ReviewNativeAudioSegmentOutput{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return ReviewNativeAudioSegmentOutput{}, err
		}
		return output, nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE native_audio_reviews
		SET status = $2, provider_call_id = NULLIF($3, '')::uuid, provider_model_id = NULLIF($4, '')::uuid,
		    transcript = NULLIF($5, ''), language = NULLIF($6, ''), alignment = $7,
		    dialogue_coverage = $8, text_accuracy = $9, timing_accuracy = $10, speaker_turn_accuracy = $11,
		    error_code = NULLIF($12, ''), error_message = NULLIF($13, ''), updated_at = now(), completed_at = now()
		WHERE id = $1
	`, record.ID, status, gatewayResponse.ProviderCallID, gatewayResponse.ModelID, gatewayResponse.Output.Text,
		gatewayResponse.Output.Language, mustJSON(gatewayResponse.Output.Segments), metrics.DialogueCoverage, metrics.TextAccuracy,
		metrics.TimingAccuracy, metrics.SpeakerTurnAccuracy, errorCode, errorMessage); err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	verificationStatus, readiness := "audio_verified", "ready"
	if !metrics.Passed {
		verificationStatus, readiness = "needs_audio_retry", "blocked"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE video_render_segments SET audio_verification_status = $2, production_readiness = $3,
		       audio_verified_by = CASE WHEN $2 = 'audio_verified' THEN NULLIF($4, '')::uuid ELSE NULL END,
		       audio_verified_at = CASE WHEN $2 = 'audio_verified' THEN now() ELSE NULL END,
		       audio_verification_notes = $5, updated_at = now()
		WHERE id = $1
	`, record.SegmentID, verificationStatus, readiness, input.CreatedBy, firstNonEmptyString(errorMessage, "ASR/强制对齐自动审核通过")); err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	if err := refreshWorkflowRenderPlanAudioState(ctx, tx, record.PlanID, record.ShotID); err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	actualTicks := int64(math.Round(record.AudioDurationSeconds * float64(record.Timebase)))
	if actualTicks > 0 {
		sampleKind, sampleKey := "shot_pacing", "native_audio"
		if len(expected) == 0 {
			sampleKind, sampleKey = "action_duration", "ambient_hold"
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO timing_calibration_samples(
				organization_id, project_id, storyboard_shot_id, video_render_segment_id,
				sample_kind, sample_key, source_kind, expected_ticks, actual_ticks, timeline_timebase, confidence,
				audio_configuration_revision, metadata
			)
			VALUES ($1, $2, $3, $4, $5, $6, 'asr_alignment', $7, $8, $9, $10, $13,
			        jsonb_build_object('reviewId', $11::uuid::text, 'providerCallId', NULLIF($12, '')::uuid::text))
		`, input.OrganizationID, input.ProjectID, record.ShotID, record.SegmentID, sampleKind, sampleKey,
			record.PlannedDurationTicks, actualTicks, record.Timebase, metrics.TimingAccuracy, record.ID,
			gatewayResponse.ProviderCallID, record.AudioConfigurationRevision); err != nil {
			return ReviewNativeAudioSegmentOutput{}, err
		}
	}
	if err := insertPunctuationCalibrationSample(ctx, tx, input, record, expected, gatewayResponse, metrics); err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	output := ReviewNativeAudioSegmentOutput{
		ReviewID: record.ID, RenderSegmentID: record.SegmentID, Status: status, Transcript: gatewayResponse.Output.Text,
		Language: gatewayResponse.Output.Language, DialogueCoverage: metrics.DialogueCoverage, TextAccuracy: metrics.TextAccuracy,
		TimingAccuracy: metrics.TimingAccuracy, SpeakerTurnAccuracy: metrics.SpeakerTurnAccuracy,
		ProviderCallID: gatewayResponse.ProviderCallID, ModelID: gatewayResponse.ModelID, ErrorCode: errorCode, ErrorMessage: errorMessage,
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.audio.review.completed", "native_audio_review", record.ID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "storyboardShotId": record.ShotID, "renderPlanId": record.PlanID, "renderSegmentId": record.SegmentID, "output": output,
	})); err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	if status == "passed" {
		if _, err := completeNodeRunTx(ctx, tx, nodeExecution, mustJSON(output)); err != nil {
			return ReviewNativeAudioSegmentOutput{}, err
		}
	} else if _, err := failNodeRunTx(ctx, tx, nodeExecution, errorCode, errorMessage, mustJSON(output)); err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	return output, nil
}

func (a Activities) failNativeAudioReview(ctx context.Context, execution NodeExecution, input ReviewNativeAudioSegmentInput, record nativeAudioReviewRecord, code, message string) (ReviewNativeAudioSegmentOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	var currentAudioConfigurationRevision int
	if err := tx.QueryRow(ctx, `SELECT audio_configuration_revision FROM projects WHERE id = $1 FOR SHARE`, input.ProjectID).Scan(&currentAudioConfigurationRevision); err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	if currentAudioConfigurationRevision != record.AudioConfigurationRevision {
		output := ReviewNativeAudioSegmentOutput{ReviewID: record.ID, RenderSegmentID: record.SegmentID, Status: "stale",
			ErrorCode: codeAudioConfigurationChanged, ErrorMessage: "音频配置已变更，该审核任务不再有效"}
		if _, err := tx.Exec(ctx, `
			UPDATE native_audio_reviews
			SET status = 'stale', error_code = $2, error_message = $3,
			    metadata = metadata || jsonb_build_object('discardedAt', now(), 'currentAudioConfigurationRevision', $4::integer),
			    updated_at = now(), completed_at = COALESCE(completed_at, now())
			WHERE id = $1
		`, record.ID, output.ErrorCode, output.ErrorMessage, currentAudioConfigurationRevision); err != nil {
			return ReviewNativeAudioSegmentOutput{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE video_render_segments SET audio_verification_status = 'audio_unverified', production_readiness = 'preview_only',
			       audio_verified_by = NULL, audio_verified_at = NULL, audio_verification_notes = $2, updated_at = now()
			WHERE id = $1
		`, record.SegmentID, output.ErrorMessage); err != nil {
			return ReviewNativeAudioSegmentOutput{}, err
		}
		if err := refreshWorkflowRenderPlanAudioState(ctx, tx, record.PlanID, record.ShotID); err != nil {
			return ReviewNativeAudioSegmentOutput{}, err
		}
		if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.audio.review.discarded", "native_audio_review", record.ID, mustJSON(map[string]any{
			"workflowRunId": input.WorkflowRunID, "storyboardShotId": record.ShotID,
			"audioConfigurationRevision":        record.AudioConfigurationRevision,
			"currentAudioConfigurationRevision": currentAudioConfigurationRevision, "output": output,
		})); err != nil {
			return ReviewNativeAudioSegmentOutput{}, err
		}
		if _, err := failNodeRunTx(ctx, tx, execution, output.ErrorCode, output.ErrorMessage, mustJSON(output)); err != nil {
			return ReviewNativeAudioSegmentOutput{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return ReviewNativeAudioSegmentOutput{}, err
		}
		return output, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE native_audio_reviews SET status = 'failed', error_code = $2, error_message = $3, updated_at = now(), completed_at = now() WHERE id = $1`, record.ID, code, message); err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE video_render_segments
		SET audio_verification_status = 'needs_audio_retry', production_readiness = 'blocked',
		    audio_verified_by = NULL, audio_verified_at = NULL, audio_verification_notes = $2, updated_at = now()
		WHERE id = $1
	`, record.SegmentID, message); err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	if err := refreshWorkflowRenderPlanAudioState(ctx, tx, record.PlanID, record.ShotID); err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	output := ReviewNativeAudioSegmentOutput{ReviewID: record.ID, RenderSegmentID: record.SegmentID, Status: "failed", ErrorCode: code, ErrorMessage: message}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.audio.review.completed", "native_audio_review", record.ID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "storyboardShotId": record.ShotID, "renderPlanId": record.PlanID, "renderSegmentId": record.SegmentID, "output": output,
	})); err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	if _, err := failNodeRunTx(ctx, tx, execution, code, message, mustJSON(output)); err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ReviewNativeAudioSegmentOutput{}, err
	}
	return output, nil
}

func refreshWorkflowRenderPlanAudioState(ctx context.Context, tx pgx.Tx, planID, shotID string) error {
	var audioStatus, readiness string
	if err := tx.QueryRow(ctx, `
		WITH stats AS (
		  SELECT count(*)::integer AS total,
		         count(*) FILTER (WHERE production_readiness = 'ready')::integer AS ready,
		         count(*) FILTER (WHERE production_readiness = 'preview_only')::integer AS preview,
		         count(*) FILTER (WHERE production_readiness = 'partial')::integer AS partial,
		         count(*) FILTER (WHERE production_readiness = 'blocked')::integer AS blocked,
		         count(*) FILTER (WHERE audio_verification_status = 'needs_audio_retry')::integer AS retry,
		         count(*) FILTER (WHERE audio_verification_status = 'native_audio_unavailable')::integer AS unavailable,
		         count(*) FILTER (WHERE audio_verification_status = 'audio_unverified')::integer AS unverified,
		         count(*) FILTER (WHERE audio_verification_status = 'audio_verified')::integer AS verified
		  FROM video_render_segments WHERE video_render_plan_id = $1
		), resolved AS (
		  SELECT CASE WHEN retry > 0 THEN 'needs_audio_retry' WHEN unavailable > 0 THEN 'native_audio_unavailable'
		              WHEN unverified > 0 THEN 'audio_unverified' WHEN verified > 0 THEN 'audio_verified' ELSE 'not_requested' END AS audio_status,
		         CASE WHEN blocked > 0 THEN 'blocked' WHEN partial > 0 THEN 'partial'
		              WHEN total > 0 AND ready = total THEN 'ready' WHEN preview > 0 THEN 'preview_only' ELSE 'blocked' END AS readiness
		  FROM stats
		)
		UPDATE video_render_plans plan SET native_audio_status = resolved.audio_status, production_readiness = resolved.readiness, updated_at = now()
		FROM resolved WHERE plan.id = $1 RETURNING plan.native_audio_status, plan.production_readiness
	`, planID).Scan(&audioStatus, &readiness); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE storyboard_shots SET native_audio_status = $2, production_readiness = $3, updated_at = now() WHERE id = $1 AND active_video_render_plan_id = $4`, shotID, audioStatus, readiness, planID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		WITH affected AS (
		  SELECT DISTINCT version.id, version.timeline_id FROM final_video_versions version
		  JOIN timeline_clips clip ON clip.timeline_id = version.timeline_id WHERE clip.storyboard_shot_id = $1 AND clip.enabled = true
		), stats AS (
		  SELECT affected.id,
		    count(*) FILTER (WHERE COALESCE(shot.production_readiness, 'ready') = 'blocked') AS blocked,
		    count(*) FILTER (WHERE COALESCE(shot.production_readiness, 'ready') = 'partial') AS partial,
		    count(*) FILTER (WHERE COALESCE(shot.production_readiness, 'ready') = 'preview_only') AS preview,
		    count(*) FILTER (WHERE COALESCE(shot.native_audio_status, 'not_requested') = 'needs_audio_retry') AS retry,
		    count(*) FILTER (WHERE COALESCE(shot.native_audio_status, 'not_requested') = 'native_audio_unavailable') AS unavailable,
		    count(*) FILTER (WHERE COALESCE(shot.native_audio_status, 'not_requested') = 'audio_unverified') AS unverified,
		    count(*) FILTER (WHERE COALESCE(shot.native_audio_status, 'not_requested') = 'audio_verified') AS verified
		  FROM affected JOIN timeline_clips clip ON clip.timeline_id = affected.timeline_id AND clip.enabled = true
		  LEFT JOIN storyboard_shots shot ON shot.id = clip.storyboard_shot_id GROUP BY affected.id
		), resolved AS (
		  SELECT id, CASE WHEN blocked > 0 THEN 'blocked' WHEN partial > 0 THEN 'partial' WHEN preview > 0 THEN 'preview_only' ELSE 'ready' END AS readiness,
		    CASE WHEN retry > 0 THEN 'needs_audio_retry' WHEN unavailable > 0 THEN 'native_audio_unavailable'
		         WHEN unverified > 0 THEN 'audio_unverified' WHEN verified > 0 THEN 'audio_verified' ELSE 'not_requested' END AS audio_status FROM stats
		)
		UPDATE final_video_versions version SET production_readiness = resolved.readiness, native_audio_status = resolved.audio_status,
		  metadata = version.metadata || jsonb_build_object('productionReadiness', resolved.readiness, 'nativeAudioStatus', resolved.audio_status)
		FROM resolved WHERE version.id = resolved.id
	`, shotID)
	return err
}

func insertPunctuationCalibrationSample(ctx context.Context, tx pgx.Tx, input ReviewNativeAudioSegmentInput, record nativeAudioReviewRecord, expected []provider.GatewayVideoDialogueSpan, response provider.GatewayASRResponse, metrics audioquality.Metrics) error {
	if len(expected) == 0 || response.Output.Duration <= 0 {
		return nil
	}
	var text strings.Builder
	expectedPauseSeconds := 0.0
	for _, line := range expected {
		text.WriteString(line.Text)
		estimate, err := estimateDialogueForCalibration(line.Text, line.Delivery)
		if err == nil {
			expectedPauseSeconds += estimate
		}
	}
	if expectedPauseSeconds <= 0 {
		return nil
	}
	spokenSeconds := float64(countCalibrationCharacters(text.String())) / 3.5
	actualPauseSeconds := math.Max(response.Output.Duration-spokenSeconds, 0.01)
	expectedTicks := int64(math.Round(expectedPauseSeconds * float64(record.Timebase)))
	actualTicks := int64(math.Round(actualPauseSeconds * float64(record.Timebase)))
	if expectedTicks <= 0 || actualTicks <= 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO timing_calibration_samples(
			organization_id, project_id, storyboard_shot_id, video_render_segment_id,
			sample_kind, sample_key, source_kind, expected_ticks, actual_ticks, timeline_timebase, confidence,
			audio_configuration_revision, metadata
		)
		VALUES ($1, $2, $3, $4, 'punctuation_pause', 'aggregate', 'asr_alignment', $5, $6, $7, $8, $11,
		        jsonb_build_object('reviewId', $9::uuid::text, 'text', $10::text))
	`, input.OrganizationID, input.ProjectID, record.ShotID, record.SegmentID, expectedTicks, actualTicks, record.Timebase,
		metrics.TextAccuracy, record.ID, text.String(), record.AudioConfigurationRevision)
	return err
}

func estimateDialogueForCalibration(text, delivery string) (float64, error) {
	// Keep 3/3.5/4 characters per second unchanged; only the punctuation component is calibrated.
	spoken := countCalibrationCharacters(text)
	if spoken == 0 {
		return 0, nil
	}
	rate := 3.5
	lower := strings.ToLower(delivery)
	if strings.Contains(lower, "slow") || strings.Contains(delivery, "缓慢") || strings.Contains(delivery, "低语") {
		rate = 3
	} else if strings.Contains(lower, "fast") || strings.Contains(delivery, "急促") {
		rate = 4
	}
	totalWindow := float64(len([]rune(strings.TrimSpace(text)))) / rate
	spokenWindow := float64(spoken) / rate
	return math.Max(totalWindow-spokenWindow, 0.15), nil
}

func countCalibrationCharacters(text string) int {
	count := 0
	for _, value := range text {
		if (value >= '0' && value <= '9') || (value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z') || (value >= '\u4e00' && value <= '\u9fff') {
			count++
		}
	}
	return count
}

func (a Activities) CompleteNativeAudioReviewWorkflow(ctx context.Context, input NativeAudioReviewWorkflowInput, output NativeAudioReviewWorkflowOutput) error {
	status := output.Status
	if status != "succeeded" && status != "partial_succeeded" && status != "failed" {
		status = "failed"
	}
	code, message := "", ""
	if status == "failed" {
		code, message = "AUDIO_REVIEW_FAILED", "原生音频审查失败"
	}
	return TransitionWorkflowRun(ctx, a.db, input.WorkflowRunID, status, code, message, mustJSON(output))
}
