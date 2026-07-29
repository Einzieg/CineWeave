package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/Einzieg/cineweave/internal/workflows"
)

type VideoRenderPlanDetail struct {
	ID                                string                     `json:"id"`
	ProductionGenerationID            string                     `json:"productionGenerationId"`
	VideoProductionBindingID          string                     `json:"videoProductionBindingId"`
	VideoProductionBindingRevision    int64                      `json:"videoProductionBindingRevision"`
	ProfileVersionID                  string                     `json:"profileVersionId"`
	ProductionProfileSnapshot         json.RawMessage            `json:"productionProfileSnapshot"`
	ProductionProfileSnapshotHash     string                     `json:"productionProfileSnapshotHash"`
	StoryboardPlanID                  *string                    `json:"storyboardPlanId,omitempty"`
	StoryboardShotID                  string                     `json:"storyboardShotId"`
	ProviderAccountID                 string                     `json:"providerAccountId"`
	ProviderModelID                   *string                    `json:"providerModelId,omitempty"`
	ModelFamily                       string                     `json:"modelFamily"`
	VariantKey                        string                     `json:"variantKey"`
	CapabilitySnapshot                json.RawMessage            `json:"capabilitySnapshot"`
	CapabilitySnapshotHash            string                     `json:"capabilitySnapshotHash"`
	CapabilityAttestationID           *string                    `json:"capabilityAttestationId,omitempty"`
	ShotStateRevision                 *int                       `json:"shotStateRevision,omitempty"`
	ShotStateHash                     *string                    `json:"shotStateHash,omitempty"`
	TransitionSnapshot                json.RawMessage            `json:"transitionSnapshot"`
	TransitionHash                    *string                    `json:"transitionHash,omitempty"`
	ReferencePackID                   *string                    `json:"referencePackId,omitempty"`
	ReferencePackHash                 *string                    `json:"referencePackHash,omitempty"`
	InitialInputContractSnapshot      json.RawMessage            `json:"initialInputContractSnapshot,omitempty"`
	InitialInputContractHash          *string                    `json:"initialInputContractHash,omitempty"`
	ContinuationInputContractSnapshot *json.RawMessage           `json:"continuationInputContractSnapshot,omitempty"`
	ContinuationInputContractHash     *string                    `json:"continuationInputContractHash,omitempty"`
	PromptContextPlanID               *string                    `json:"promptContextPlanId,omitempty"`
	PromptContextPlanHash             *string                    `json:"promptContextPlanHash,omitempty"`
	VideoPromptPlanID                 *string                    `json:"videoPromptPlanId,omitempty"`
	DialogueCues                      json.RawMessage            `json:"dialogueCues"`
	NativeAudioRequired               bool                       `json:"nativeAudioRequired"`
	Status                            string                     `json:"status"`
	Active                            bool                       `json:"active"`
	TargetDurationTicks               int64                      `json:"targetDurationTicks"`
	TargetDurationFrames              int64                      `json:"targetDurationFrames"`
	TargetDurationSeconds             float64                    `json:"targetDurationSeconds"`
	TimelineTimebase                  int64                      `json:"timelineTimebase"`
	FPSNumerator                      int                        `json:"fpsNumerator"`
	FPSDenominator                    int                        `json:"fpsDenominator"`
	TaskType                          string                     `json:"taskType"`
	ReferenceMode                     string                     `json:"referenceMode"`
	AspectRatio                       string                     `json:"aspectRatio"`
	Resolution                        string                     `json:"resolution"`
	AudioStrategy                     string                     `json:"audioStrategy"`
	AudioRequirement                  string                     `json:"audioRequirement"`
	NativeAudioStatus                 string                     `json:"nativeAudioStatus"`
	ProductionReadiness               string                     `json:"productionReadiness"`
	OutputArtifactID                  *string                    `json:"outputArtifactId,omitempty"`
	OutputMediaFileID                 *string                    `json:"outputMediaFileId,omitempty"`
	OutputStorageKey                  *string                    `json:"outputStorageKey,omitempty"`
	OutputPreviewURL                  *string                    `json:"outputPreviewUrl,omitempty"`
	AudioVerifiedBy                   *string                    `json:"audioVerifiedBy,omitempty"`
	AudioVerifiedAt                   *time.Time                 `json:"audioVerifiedAt,omitempty"`
	AudioVerificationNotes            *string                    `json:"audioVerificationNotes,omitempty"`
	Metadata                          json.RawMessage            `json:"metadata"`
	ExpiresAt                         time.Time                  `json:"expiresAt"`
	CreatedAt                         time.Time                  `json:"createdAt"`
	UpdatedAt                         time.Time                  `json:"updatedAt"`
	CompletedAt                       *time.Time                 `json:"completedAt,omitempty"`
	Segments                          []VideoRenderSegmentDetail `json:"segments"`
}

type VideoRenderSegmentDetail struct {
	ID                       string          `json:"id"`
	SegmentIndex             int             `json:"segmentIndex"`
	PlannedStartTick         int64           `json:"plannedStartTick"`
	PlannedEndTick           int64           `json:"plannedEndTick"`
	PlannedDurationTicks     int64           `json:"plannedDurationTicks"`
	PlannedDurationFrames    int64           `json:"plannedDurationFrames"`
	PlannedDurationSeconds   float64         `json:"plannedDurationSeconds"`
	RequestedDurationSeconds float64         `json:"requestedDurationSeconds"`
	TrimEndTick              *int64          `json:"trimEndTick,omitempty"`
	ContinuityMode           string          `json:"continuityMode"`
	Status                   string          `json:"status"`
	RetryGeneration          int             `json:"retryGeneration"`
	ProviderAsyncTaskID      *string         `json:"providerAsyncTaskId,omitempty"`
	ProviderCallID           *string         `json:"providerCallId,omitempty"`
	ProviderModelID          *string         `json:"providerModelId,omitempty"`
	ExternalTaskID           *string         `json:"externalTaskId,omitempty"`
	ArtifactID               *string         `json:"artifactId,omitempty"`
	MediaFileID              *string         `json:"mediaFileId,omitempty"`
	StorageKey               *string         `json:"storageKey,omitempty"`
	PreviewURL               *string         `json:"previewUrl,omitempty"`
	Prompt                   *string         `json:"prompt,omitempty"`
	Dialogue                 json.RawMessage `json:"dialogue"`
	NativeAudioRequested     bool            `json:"nativeAudioRequested"`
	NativeAudioDetected      *bool           `json:"nativeAudioDetected,omitempty"`
	AudioVerificationStatus  string          `json:"audioVerificationStatus"`
	ProductionReadiness      string          `json:"productionReadiness"`
	RawAVArtifactID          *string         `json:"rawAvArtifactId,omitempty"`
	MezzanineArtifactID      *string         `json:"mezzanineArtifactId,omitempty"`
	MezzaninePreviewURL      *string         `json:"mezzaninePreviewUrl,omitempty"`
	ExtractedAudioArtifactID *string         `json:"extractedAudioArtifactId,omitempty"`
	ExtractedAudioPreviewURL *string         `json:"extractedAudioPreviewUrl,omitempty"`
	AudioVerifiedBy          *string         `json:"audioVerifiedBy,omitempty"`
	AudioVerifiedAt          *time.Time      `json:"audioVerifiedAt,omitempty"`
	AudioVerificationNotes   *string         `json:"audioVerificationNotes,omitempty"`
	ErrorCode                *string         `json:"errorCode,omitempty"`
	ErrorMessage             *string         `json:"errorMessage,omitempty"`
	CreatedAt                time.Time       `json:"createdAt"`
	UpdatedAt                time.Time       `json:"updatedAt"`
	StartedAt                *time.Time      `json:"startedAt,omitempty"`
	CompletedAt              *time.Time      `json:"completedAt,omitempty"`
}

func (s *Server) getStoryboardShotRenderPlan(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	shot, err := s.storyboardShotByID(r, project.ID, r.PathValue("shotId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	plan, err := s.videoRenderPlanDetail(r.Context(), project.ID, shot.ID, "")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, plan, nil)
}

func (s *Server) createStoryboardShotRenderPlan(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{authz.PermissionStoryboardGenerate, authz.PermissionProjectWrite})
	if !ok {
		return
	}
	shot, err := s.storyboardShotByID(r, project.ID, r.PathValue("shotId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req struct {
		ModelProfileKey  string `json:"modelProfileKey"`
		ProviderModelID  string `json:"providerModelId"`
		AspectRatio      string `json:"aspectRatio"`
		Resolution       string `json:"resolution"`
		AudioStrategy    string `json:"audioStrategy"`
		AudioRequirement string `json:"audioRequirement"`
	}
	if !decode(w, r, &req) {
		return
	}
	requirements, err := s.storyboardShotRequirementDetails(r, project.ID, shot.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	projectReferenceOptions, err := s.projectCurrentAssetReferenceOptions(r, project.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	references := s.storyboardShotVideoReferenceOptions(r, shot, s.storyboardShotImageReferenceOptions(r, shot, requirements, projectReferenceOptions...))
	referenceMode := "none"
	taskType := "video.text_to_video"
	for _, reference := range references {
		if reference.Selected || reference.AutoSelected {
			referenceMode = "first_frame"
			taskType = "video.image_to_video"
			break
		}
	}
	modelProfileKey := firstNonEmptyString(strings.TrimSpace(req.ModelProfileKey), project.VideoModelProfileKey, "video_generation_default")
	aspectRatio := firstNonEmptyString(strings.TrimSpace(req.AspectRatio), project.VideoRatio, stringValue(project.AspectRatio), "16:9")
	resolution := firstNonEmptyString(strings.TrimSpace(req.Resolution), "720p")
	audioStrategy := firstNonEmptyString(strings.ToLower(strings.TrimSpace(req.AudioStrategy)), project.AudioStrategy)
	audioRequirement := firstNonEmptyString(strings.ToLower(strings.TrimSpace(req.AudioRequirement)), project.AudioRequirement)
	if !validProjectAudioSettings(audioStrategy, audioRequirement) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "audioStrategy or audioRequirement is invalid", nil, false)
		return
	}
	dialogueSpans, err := apiGatewayVideoDialogueSpans(shot)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	productionContext, err := videoproduction.LoadActiveContext(r.Context(), s.db, project.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	operationPermission := s.firstAuthorizedProjectPermission(
		r.Context(),
		principal,
		project.ID,
		authz.PermissionStoryboardGenerate,
		authz.PermissionProjectWrite,
	)
	plan, err := provider.NewGatewayClientFromEnv().PlanVideo(r.Context(), provider.GatewayVideoPlanRequest{
		GatewayBillingIdentity: gatewayBillingIdentityFromContext(
			r.Context(),
			operationPermission,
			provider.BillingContextReasonManualProvider,
		),
		OrganizationID: project.OrganizationID, ProjectID: project.ID,
		IdempotencyKey: gatewayProviderIdempotencyKey(
			r.Context(),
			provider.TaskTypeVideoCreateTask,
			project.ID,
			shot.ID,
			modelProfileKey,
			strings.TrimSpace(req.ProviderModelID),
		),
		ProductionGenerationID:         productionContext.Generation.ID,
		VideoProductionBindingID:       productionContext.Binding.ID,
		VideoProductionBindingRevision: productionContext.Binding.Revision,
		StoryboardPlanID:               stringValue(shot.StoryboardPlanID), StoryboardShotID: shot.ID,
		ModelProfileKey: modelProfileKey, ProviderModelID: strings.TrimSpace(req.ProviderModelID), TaskType: taskType,
		TargetDurationTicks: shot.PlannedDurationTicks, TimelineTimebase: project.TimelineTimebase,
		FPSNumerator: int64(project.FPSNumerator), FPSDenominator: int64(project.FPSDenominator),
		AudioStrategy: audioStrategy, AudioRequirement: audioRequirement, DialogueLanguage: "zh-CN", HasDialogue: len(dialogueSpans) > 0,
		ReferenceMode: referenceMode, AspectRatio: aspectRatio, Resolution: resolution, PromptLanguage: "zh-CN",
		DialogueSpans: dialogueSpans,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	detail, err := s.videoRenderPlanDetail(r.Context(), project.ID, shot.ID, plan.ExecutionPlanID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, detail, nil)
}

func apiGatewayVideoDialogueSpans(shot StoryboardShot) ([]provider.GatewayVideoDialogueSpan, error) {
	for _, line := range shot.ScriptDialogue {
		kind := strings.ToLower(strings.TrimSpace(line.Kind))
		if kind == "" {
			kind = "dialogue"
		}
		if kind == "dialogue" && strings.TrimSpace(line.Text) != "" && strings.TrimSpace(line.Speaker) == "" {
			return nil, &provider.StandardErrorError{Standard: provider.StandardError{Code: provider.CodeStoryboardReplanRequired, Message: "角色对白缺少说话人，需要先重新生成分镜计划", Retryable: false}}
		}
	}
	spokenDialogue := workflows.SpokenStoryboardDialogue(shot.ScriptDialogue)
	result := make([]provider.GatewayVideoDialogueSpan, 0, len(spokenDialogue))
	for _, line := range spokenDialogue {
		if strings.TrimSpace(line.Text) == "" {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(line.Kind))
		if kind == "" {
			kind = "dialogue"
		}
		if kind == "dialogue" && strings.TrimSpace(line.Speaker) == "" {
			return nil, &provider.StandardErrorError{Standard: provider.StandardError{Code: provider.CodeStoryboardReplanRequired, Message: "角色对白缺少说话人，需要先重新生成分镜计划", Retryable: false}}
		}
		if line.SpanEndTick <= line.SpanStartTick || line.SpanStartTick < shot.StartTick || line.SpanEndTick > shot.EndTick {
			return nil, &provider.StandardErrorError{Standard: provider.StandardError{Code: provider.CodeStoryboardReplanRequired, Message: "分镜台词缺少精确的帧级时间范围，需要先重新生成分镜计划", Retryable: false}}
		}
		result = append(result, provider.GatewayVideoDialogueSpan{
			TimingUnitID: line.TimingUnitID, Speaker: line.Speaker, Text: line.Text, Delivery: line.Delivery, Kind: kind,
			StartTick: line.SpanStartTick - shot.StartTick, EndTick: line.SpanEndTick - shot.StartTick,
			ContinuesFromPrevious: line.ContinuesFromPrevious, ContinuesToNext: line.ContinuesToNext,
		})
	}
	return result, nil
}

func (s *Server) videoRenderPlanDetail(ctx context.Context, projectID, shotID, planID string) (VideoRenderPlanDetail, error) {
	where := `plan.project_id = $1 AND plan.storyboard_shot_id = $2 AND plan.active = true`
	args := []any{projectID, shotID}
	if strings.TrimSpace(planID) != "" {
		where = `plan.project_id = $1 AND plan.storyboard_shot_id = $2 AND plan.id = $3`
		args = append(args, planID)
	}
	var item VideoRenderPlanDetail
	var storyboardPlanID *string
	if err := s.db.QueryRow(ctx, `
		SELECT plan.id::text, plan.production_generation_id::text,
		       plan.video_production_binding_id::text, plan.video_production_binding_revision,
		       plan.profile_version_id::text, plan.production_profile_snapshot,
		       plan.production_profile_snapshot_hash,
		       plan.storyboard_plan_id::text, plan.storyboard_shot_id::text,
		       plan.provider_account_id::text, plan.provider_model_id::text, plan.model_family, plan.variant_key,
		       plan.capability_snapshot, plan.capability_snapshot_hash, plan.capability_attestation_id::text,
		       plan.shot_state_revision, plan.shot_state_hash, plan.transition_snapshot, plan.transition_hash,
		       plan.reference_pack_id::text, plan.reference_pack_hash,
		       plan.initial_input_contract_snapshot, plan.initial_input_contract_hash,
		       plan.continuation_input_contract_snapshot, plan.continuation_input_contract_hash,
		       plan.prompt_context_plan_id::text, plan.prompt_context_plan_hash,
		       plan.video_prompt_plan_id::text, plan.dialogue_cues, plan.native_audio_required,
		       plan.status, plan.active,
		       plan.target_duration_ticks, plan.timeline_timebase, plan.fps_numerator, plan.fps_denominator,
		       plan.task_type, plan.reference_mode, plan.aspect_ratio, plan.resolution,
		       plan.audio_strategy, plan.audio_requirement, plan.native_audio_status, plan.production_readiness,
		       plan.output_artifact_id::text, plan.output_media_file_id::text, plan.output_storage_key,
		       plan.audio_verified_by::text, plan.audio_verified_at, plan.audio_verification_notes, plan.metadata,
		       plan.expires_at, plan.created_at, plan.updated_at, plan.completed_at
		FROM video_render_plans plan WHERE `+where,
		args...).Scan(
		&item.ID, &item.ProductionGenerationID, &item.VideoProductionBindingID,
		&item.VideoProductionBindingRevision, &item.ProfileVersionID,
		&item.ProductionProfileSnapshot, &item.ProductionProfileSnapshotHash,
		&storyboardPlanID, &item.StoryboardShotID, &item.ProviderAccountID, &item.ProviderModelID,
		&item.ModelFamily, &item.VariantKey, &item.CapabilitySnapshot, &item.CapabilitySnapshotHash,
		&item.CapabilityAttestationID, &item.ShotStateRevision, &item.ShotStateHash,
		&item.TransitionSnapshot, &item.TransitionHash, &item.ReferencePackID, &item.ReferencePackHash,
		&item.InitialInputContractSnapshot, &item.InitialInputContractHash,
		&item.ContinuationInputContractSnapshot, &item.ContinuationInputContractHash,
		&item.PromptContextPlanID, &item.PromptContextPlanHash, &item.VideoPromptPlanID,
		&item.DialogueCues, &item.NativeAudioRequired,
		&item.Status, &item.Active, &item.TargetDurationTicks, &item.TimelineTimebase, &item.FPSNumerator, &item.FPSDenominator,
		&item.TaskType, &item.ReferenceMode, &item.AspectRatio, &item.Resolution,
		&item.AudioStrategy, &item.AudioRequirement, &item.NativeAudioStatus, &item.ProductionReadiness,
		&item.OutputArtifactID, &item.OutputMediaFileID, &item.OutputStorageKey,
		&item.AudioVerifiedBy, &item.AudioVerifiedAt, &item.AudioVerificationNotes, &item.Metadata,
		&item.ExpiresAt, &item.CreatedAt, &item.UpdatedAt, &item.CompletedAt,
	); err != nil {
		return VideoRenderPlanDetail{}, err
	}
	item.StoryboardPlanID = storyboardPlanID
	frameTick := item.TimelineTimebase * int64(item.FPSDenominator) / int64(item.FPSNumerator)
	item.TargetDurationFrames = item.TargetDurationTicks / frameTick
	item.TargetDurationSeconds = float64(item.TargetDurationTicks) / float64(item.TimelineTimebase)
	if item.OutputStorageKey != nil {
		item.OutputPreviewURL = s.previewURLForStorageKeyRequest(ctx, *item.OutputStorageKey)
	}
	rows, err := s.db.Query(ctx, `
		SELECT segment.id::text, segment.segment_index, segment.planned_start_tick, segment.planned_end_tick,
		       segment.planned_duration_ticks, segment.requested_duration_seconds::float8, segment.trim_end_tick,
		       segment.continuity_mode, segment.status, segment.retry_generation,
		       segment.provider_async_task_id::text, segment.provider_call_id::text, segment.provider_model_id::text,
		       segment.external_task_id, segment.artifact_id::text, segment.media_file_id::text, segment.storage_key,
		       segment.prompt, segment.dialogue, segment.native_audio_requested, segment.native_audio_detected,
		       segment.audio_verification_status, segment.production_readiness,
		       segment.raw_av_artifact_id::text, segment.mezzanine_artifact_id::text, mezzanine.storage_key,
		       segment.extracted_audio_artifact_id::text, audio.storage_key,
		       segment.audio_verified_by::text, segment.audio_verified_at, segment.audio_verification_notes,
		       segment.error_code, segment.error_message, segment.created_at, segment.updated_at,
		       segment.started_at, segment.completed_at
		FROM video_render_segments segment
		LEFT JOIN artifacts mezzanine ON mezzanine.id = segment.mezzanine_artifact_id
		LEFT JOIN artifacts audio ON audio.id = segment.extracted_audio_artifact_id
		WHERE segment.video_render_plan_id = $1 ORDER BY segment.segment_index
	`, item.ID)
	if err != nil {
		return VideoRenderPlanDetail{}, err
	}
	defer rows.Close()
	item.Segments = make([]VideoRenderSegmentDetail, 0)
	for rows.Next() {
		var segment VideoRenderSegmentDetail
		var mezzanineStorageKey, extractedAudioStorageKey *string
		if err := rows.Scan(
			&segment.ID, &segment.SegmentIndex, &segment.PlannedStartTick, &segment.PlannedEndTick,
			&segment.PlannedDurationTicks, &segment.RequestedDurationSeconds, &segment.TrimEndTick,
			&segment.ContinuityMode, &segment.Status, &segment.RetryGeneration,
			&segment.ProviderAsyncTaskID, &segment.ProviderCallID, &segment.ProviderModelID,
			&segment.ExternalTaskID, &segment.ArtifactID, &segment.MediaFileID, &segment.StorageKey,
			&segment.Prompt, &segment.Dialogue, &segment.NativeAudioRequested, &segment.NativeAudioDetected,
			&segment.AudioVerificationStatus, &segment.ProductionReadiness,
			&segment.RawAVArtifactID, &segment.MezzanineArtifactID, &mezzanineStorageKey,
			&segment.ExtractedAudioArtifactID, &extractedAudioStorageKey,
			&segment.AudioVerifiedBy, &segment.AudioVerifiedAt, &segment.AudioVerificationNotes,
			&segment.ErrorCode, &segment.ErrorMessage, &segment.CreatedAt, &segment.UpdatedAt,
			&segment.StartedAt, &segment.CompletedAt,
		); err != nil {
			return VideoRenderPlanDetail{}, err
		}
		segment.PlannedDurationFrames = segment.PlannedDurationTicks / frameTick
		segment.PlannedDurationSeconds = float64(segment.PlannedDurationTicks) / float64(item.TimelineTimebase)
		if segment.StorageKey != nil {
			segment.PreviewURL = s.previewURLForStorageKeyRequest(ctx, *segment.StorageKey)
		}
		if mezzanineStorageKey != nil {
			segment.MezzaninePreviewURL = s.previewURLForStorageKeyRequest(ctx, *mezzanineStorageKey)
		}
		if extractedAudioStorageKey != nil {
			segment.ExtractedAudioPreviewURL = s.previewURLForStorageKeyRequest(ctx, *extractedAudioStorageKey)
		}
		item.Segments = append(item.Segments, segment)
	}
	return item, rows.Err()
}

func (s *Server) previewURLForStorageKeyRequest(ctx context.Context, storageKey string) *string {
	if s.storage == nil || strings.TrimSpace(storageKey) == "" {
		return nil
	}
	result, err := s.storage.PresignGetObject(ctx, storageKey, 15*time.Minute)
	if err != nil || strings.TrimSpace(result.URL) == "" {
		return nil
	}
	return &result.URL
}
