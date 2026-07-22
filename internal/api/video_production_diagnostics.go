package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/jackc/pgx/v5"
)

type StoryboardShotStateVersionDetail struct {
	ID                     string          `json:"id"`
	ProductionGenerationID string          `json:"productionGenerationId"`
	StoryboardShotID       string          `json:"storyboardShotId"`
	StateRole              string          `json:"stateRole"`
	Revision               int             `json:"revision"`
	Status                 string          `json:"status"`
	State                  json.RawMessage `json:"state"`
	StateHash              string          `json:"stateHash"`
	SourceType             string          `json:"sourceType"`
	SourceID               *string         `json:"sourceId,omitempty"`
	PromptVersionID        *string         `json:"promptVersionId,omitempty"`
	ProviderCallID         *string         `json:"providerCallId,omitempty"`
	ModelID                *string         `json:"modelId,omitempty"`
	CreatedBy              *string         `json:"createdBy,omitempty"`
	CreatedAt              time.Time       `json:"createdAt"`
	ApprovedAt             *time.Time      `json:"approvedAt,omitempty"`
}

type StoryboardShotTransitionDetail struct {
	ID                     string          `json:"id"`
	ProductionGenerationID string          `json:"productionGenerationId"`
	StoryboardPlanID       string          `json:"storyboardPlanId"`
	SourceShotID           *string         `json:"sourceShotId,omitempty"`
	TargetShotID           string          `json:"targetShotId"`
	TransitionType         string          `json:"transitionType"`
	TailPolicy             string          `json:"tailPolicy"`
	AnchorPolicy           string          `json:"anchorPolicy"`
	CarryConstraints       json.RawMessage `json:"carryConstraints"`
	ResetConstraints       json.RawMessage `json:"resetConstraints"`
	Confidence             float64         `json:"confidence"`
	Revision               int             `json:"revision"`
	Status                 string          `json:"status"`
	ReviewStatus           string          `json:"reviewStatus"`
	Metadata               json.RawMessage `json:"metadata"`
	CreatedAt              time.Time       `json:"createdAt"`
	UpdatedAt              time.Time       `json:"updatedAt"`
}

type ShotVisualAnchorDetail struct {
	ID                     string          `json:"id"`
	ProductionGenerationID string          `json:"productionGenerationId"`
	StoryboardShotID       string          `json:"storyboardShotId"`
	ShotStateVersionID     *string         `json:"shotStateVersionId,omitempty"`
	AnchorRole             string          `json:"anchorRole"`
	Revision               int             `json:"revision"`
	Status                 string          `json:"status"`
	ReviewStatus           string          `json:"reviewStatus"`
	ArtifactID             *string         `json:"artifactId,omitempty"`
	MediaFileID            *string         `json:"mediaFileId,omitempty"`
	StorageKey             *string         `json:"storageKey,omitempty"`
	PreviewURL             *string         `json:"previewUrl,omitempty"`
	Prompt                 *string         `json:"prompt,omitempty"`
	PromptVersionID        *string         `json:"promptVersionId,omitempty"`
	PromptHash             *string         `json:"promptHash,omitempty"`
	ProviderCallID         *string         `json:"providerCallId,omitempty"`
	ModelID                *string         `json:"modelId,omitempty"`
	ReferencePackID        *string         `json:"referencePackId,omitempty"`
	Metadata               json.RawMessage `json:"metadata"`
	CreatedAt              time.Time       `json:"createdAt"`
	UpdatedAt              time.Time       `json:"updatedAt"`
}

type ShotReferencePackDetail struct {
	ID                     string          `json:"id"`
	ProductionGenerationID string          `json:"productionGenerationId"`
	StoryboardShotID       string          `json:"storyboardShotId"`
	Purpose                string          `json:"purpose"`
	ProfileSnapshotHash    string          `json:"profileSnapshotHash"`
	ShotStateHash          string          `json:"shotStateHash"`
	CapabilitySnapshotHash string          `json:"capabilitySnapshotHash"`
	Manifest               json.RawMessage `json:"manifest"`
	ManifestHash           string          `json:"manifestHash"`
	Status                 string          `json:"status"`
	CreatedAt              time.Time       `json:"createdAt"`
}

type ShotReferencePackItemDetail struct {
	ID           string          `json:"id"`
	ReferenceKey string          `json:"referenceKey"`
	Role         string          `json:"role"`
	MediaType    string          `json:"mediaType"`
	Semantics    string          `json:"semantics"`
	Required     bool            `json:"required"`
	Priority     int             `json:"priority"`
	SourceType   string          `json:"sourceType"`
	SourceID     *string         `json:"sourceId,omitempty"`
	AssetID      *string         `json:"assetId,omitempty"`
	ArtifactID   *string         `json:"artifactId,omitempty"`
	MediaFileID  *string         `json:"mediaFileId,omitempty"`
	StorageKey   *string         `json:"storageKey,omitempty"`
	PreviewURL   *string         `json:"previewUrl,omitempty"`
	ContentHash  string          `json:"contentHash"`
	Metadata     json.RawMessage `json:"metadata"`
}

type StoryboardSheetManifestDetail struct {
	ID                      string          `json:"id"`
	ProductionGenerationID  string          `json:"productionGenerationId"`
	StoryboardShotID        string          `json:"storyboardShotId"`
	SheetAnchorID           string          `json:"sheetAnchorId"`
	SheetPreviewURL         *string         `json:"sheetPreviewUrl,omitempty"`
	Revision                int             `json:"revision"`
	ContractVersion         string          `json:"contractVersion"`
	PlannedDurationTicks    int64           `json:"plannedDurationTicks"`
	TimelineTimebase        int64           `json:"timelineTimebase"`
	VideoAspectRatio        string          `json:"videoAspectRatio"`
	SheetAspectRatio        string          `json:"sheetAspectRatio"`
	GridRows                int             `json:"gridRows"`
	GridColumns             int             `json:"gridColumns"`
	PanelCount              int             `json:"panelCount"`
	EntryStateHash          string          `json:"entryStateHash"`
	ExitStateHash           string          `json:"exitStateHash"`
	Manifest                json.RawMessage `json:"manifest"`
	ManifestHash            string          `json:"manifestHash"`
	Status                  string          `json:"status"`
	ReviewStatus            string          `json:"reviewStatus"`
	ReviewerPromptVersionID *string         `json:"reviewerPromptVersionId,omitempty"`
	ReviewerProviderCallID  *string         `json:"reviewerProviderCallId,omitempty"`
	ReviewerModelID         *string         `json:"reviewerModelId,omitempty"`
	ReviewerOutput          json.RawMessage `json:"reviewerOutput"`
	Metadata                json.RawMessage `json:"metadata"`
	CreatedAt               time.Time       `json:"createdAt"`
	UpdatedAt               time.Time       `json:"updatedAt"`
	ReviewedAt              *time.Time      `json:"reviewedAt,omitempty"`
}

type StoryboardSheetPanelDetail struct {
	ID                     string          `json:"id"`
	ProductionGenerationID string          `json:"productionGenerationId"`
	StoryboardShotID       string          `json:"storyboardShotId"`
	ManifestID             string          `json:"manifestId"`
	VisualAnchorID         string          `json:"visualAnchorId"`
	Ordinal                int             `json:"ordinal"`
	GridRow                int             `json:"gridRow"`
	GridColumn             int             `json:"gridColumn"`
	TimeTick               int64           `json:"timeTick"`
	NormalizedPosition     int             `json:"normalizedPosition"`
	Stage                  string          `json:"stage"`
	ActionStage            string          `json:"actionStage"`
	ExpectedState          json.RawMessage `json:"expectedState"`
	ExpectedStateHash      string          `json:"expectedStateHash"`
	Status                 string          `json:"status"`
	ReviewStatus           string          `json:"reviewStatus"`
	ArtifactID             *string         `json:"artifactId,omitempty"`
	MediaFileID            *string         `json:"mediaFileId,omitempty"`
	StorageKey             *string         `json:"storageKey,omitempty"`
	PreviewURL             *string         `json:"previewUrl,omitempty"`
	ContentHash            *string         `json:"contentHash,omitempty"`
	Crop                   json.RawMessage `json:"crop"`
	Metadata               json.RawMessage `json:"metadata"`
	CreatedAt              time.Time       `json:"createdAt"`
	UpdatedAt              time.Time       `json:"updatedAt"`
}

type PromptContextPlanDetail struct {
	ID                             string          `json:"id"`
	ProductionGenerationID         string          `json:"productionGenerationId"`
	VideoProductionBindingID       string          `json:"videoProductionBindingId"`
	VideoProductionBindingRevision int64           `json:"videoProductionBindingRevision"`
	StoryboardPlanID               string          `json:"storyboardPlanId"`
	StoryboardShotID               string          `json:"storyboardShotId"`
	ScriptEpisodeID                string          `json:"scriptEpisodeId"`
	ScriptSceneID                  *string         `json:"scriptSceneId,omitempty"`
	Revision                       int             `json:"revision"`
	Status                         string          `json:"status"`
	EpisodeContinuityDigest        string          `json:"episodeContinuityDigest"`
	CurrentSceneScript             string          `json:"currentSceneScript"`
	AdjacentSceneSummaries         json.RawMessage `json:"adjacentSceneSummaries"`
	CurrentShotState               json.RawMessage `json:"currentShotState"`
	VerbatimDialogueCues           json.RawMessage `json:"verbatimDialogueCues"`
	ModelContextLimit              int             `json:"modelContextLimit"`
	ModelPromptLimit               int             `json:"modelPromptLimit"`
	BudgetAllocation               json.RawMessage `json:"budgetAllocation"`
	SourceHashes                   json.RawMessage `json:"sourceHashes"`
	PlanHash                       string          `json:"planHash"`
	CreatedAt                      time.Time       `json:"createdAt"`
	StaleAt                        *time.Time      `json:"staleAt,omitempty"`
	ArchivedAt                     *time.Time      `json:"archivedAt,omitempty"`
}

type VideoPromptPlanDetail struct {
	ID                             string          `json:"id"`
	ProductionGenerationID         string          `json:"productionGenerationId"`
	VideoProductionBindingID       string          `json:"videoProductionBindingId"`
	VideoProductionBindingRevision int64           `json:"videoProductionBindingRevision"`
	ProfileVersionID               string          `json:"profileVersionId"`
	StoryboardShotID               string          `json:"storyboardShotId"`
	PromptContextPlanID            string          `json:"promptContextPlanId"`
	PromptVersionID                string          `json:"promptVersionId"`
	ReviewerPromptVersionID        *string         `json:"reviewerPromptVersionId,omitempty"`
	WorkflowRunID                  *string         `json:"workflowRunId,omitempty"`
	NodeRunID                      *string         `json:"nodeRunId,omitempty"`
	ProviderCallID                 *string         `json:"providerCallId,omitempty"`
	ReviewerProviderCallID         *string         `json:"reviewerProviderCallId,omitempty"`
	ProviderModelID                *string         `json:"providerModelId,omitempty"`
	Revision                       int             `json:"revision"`
	Status                         string          `json:"status"`
	RenderedPrompt                 string          `json:"renderedPrompt"`
	PromptHash                     string          `json:"promptHash"`
	PromptContextPlanHash          string          `json:"promptContextPlanHash"`
	ProfileSnapshotHash            string          `json:"profileSnapshotHash"`
	ShotStateHash                  string          `json:"shotStateHash"`
	TransitionHash                 *string         `json:"transitionHash,omitempty"`
	ReferencePackHash              string          `json:"referencePackHash"`
	CapabilitySnapshotHash         string          `json:"capabilitySnapshotHash"`
	InputContractVersion           string          `json:"inputContractVersion"`
	DialogueCues                   json.RawMessage `json:"dialogueCues"`
	NativeAudioRequired            bool            `json:"nativeAudioRequired"`
	AudioStrategy                  string          `json:"audioStrategy"`
	AudioRequirement               string          `json:"audioRequirement"`
	ReviewerOutput                 json.RawMessage `json:"reviewerOutput"`
	Metadata                       json.RawMessage `json:"metadata"`
	CreatedAt                      time.Time       `json:"createdAt"`
	ReviewedAt                     *time.Time      `json:"reviewedAt,omitempty"`
	ApprovedAt                     *time.Time      `json:"approvedAt,omitempty"`
	StaleAt                        *time.Time      `json:"staleAt,omitempty"`
	ArchivedAt                     *time.Time      `json:"archivedAt,omitempty"`
}

func (s *Server) getStoryboardShotState(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
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
		SELECT id::text, production_generation_id::text, storyboard_shot_id::text,
		       state_role, revision, status, state, state_hash, source_type,
		       source_id::text, prompt_version_id::text, provider_call_id::text,
		       model_id::text, created_by::text, created_at, approved_at
		FROM storyboard_shot_state_versions
		WHERE project_id = $1 AND storyboard_shot_id = $2
		ORDER BY state_role, revision DESC
	`, project.ID, shot.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]StoryboardShotStateVersionDetail, 0)
	for rows.Next() {
		var item StoryboardShotStateVersionDetail
		if err := rows.Scan(
			&item.ID, &item.ProductionGenerationID, &item.StoryboardShotID,
			&item.StateRole, &item.Revision, &item.Status, &item.State, &item.StateHash,
			&item.SourceType, &item.SourceID, &item.PromptVersionID, &item.ProviderCallID,
			&item.ModelID, &item.CreatedBy, &item.CreatedAt, &item.ApprovedAt,
		); err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) getStoryboardShotTransition(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	shot, err := s.storyboardShotByID(r, project.ID, r.PathValue("shotId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	items, err := s.storyboardShotTransitions(r, project.ID, shot.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var active *StoryboardShotTransitionDetail
	for index := range items {
		if items[index].Status == "active" {
			active = &items[index]
			break
		}
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"active": active, "items": items}, nil)
}

func (s *Server) storyboardShotTransitions(r *http.Request, projectID, shotID string) ([]StoryboardShotTransitionDetail, error) {
	rows, err := s.db.Query(r.Context(), `
		SELECT id::text, production_generation_id::text, storyboard_plan_id::text,
		       source_shot_id::text, target_shot_id::text, transition_type,
		       tail_policy, anchor_policy, carry_constraints, reset_constraints,
		       confidence::float8, revision, status, review_status, metadata,
		       created_at, updated_at
		FROM storyboard_shot_transitions
		WHERE project_id = $1 AND target_shot_id = $2
		ORDER BY revision DESC
	`, projectID, shotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]StoryboardShotTransitionDetail, 0)
	for rows.Next() {
		var item StoryboardShotTransitionDetail
		if err := rows.Scan(
			&item.ID, &item.ProductionGenerationID, &item.StoryboardPlanID,
			&item.SourceShotID, &item.TargetShotID, &item.TransitionType,
			&item.TailPolicy, &item.AnchorPolicy, &item.CarryConstraints,
			&item.ResetConstraints, &item.Confidence, &item.Revision, &item.Status,
			&item.ReviewStatus, &item.Metadata, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) listStoryboardShotAnchors(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
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
		SELECT id::text, production_generation_id::text, storyboard_shot_id::text,
		       shot_state_version_id::text, anchor_role, revision, status, review_status,
		       artifact_id::text, media_file_id::text, storage_key, prompt,
		       prompt_version_id::text, prompt_hash, provider_call_id::text,
		       model_id::text, reference_pack_id::text, metadata, created_at, updated_at
		FROM shot_visual_anchors
		WHERE project_id = $1 AND storyboard_shot_id = $2
		ORDER BY anchor_role, revision DESC
	`, project.ID, shot.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]ShotVisualAnchorDetail, 0)
	for rows.Next() {
		var item ShotVisualAnchorDetail
		if err := rows.Scan(
			&item.ID, &item.ProductionGenerationID, &item.StoryboardShotID,
			&item.ShotStateVersionID, &item.AnchorRole, &item.Revision, &item.Status,
			&item.ReviewStatus, &item.ArtifactID, &item.MediaFileID, &item.StorageKey,
			&item.Prompt, &item.PromptVersionID, &item.PromptHash, &item.ProviderCallID,
			&item.ModelID, &item.ReferencePackID, &item.Metadata, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			s.writeError(w, r, err)
			return
		}
		if item.StorageKey != nil {
			item.PreviewURL = s.previewURLForStorageKeyRequest(r.Context(), *item.StorageKey)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) getStoryboardShotReferencePack(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	shot, err := s.storyboardShotByID(r, project.ID, r.PathValue("shotId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	purpose := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("purpose")))
	if purpose == "" {
		purpose = "anchor"
	}
	if purpose != "anchor" && purpose != "video" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "参考包用途必须是 anchor 或 video", nil, false)
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id::text, production_generation_id::text, storyboard_shot_id::text,
		       purpose,
		       profile_snapshot_hash, shot_state_hash, capability_snapshot_hash,
		       manifest, manifest_hash, status, created_at
		FROM shot_reference_packs
		WHERE project_id = $1 AND storyboard_shot_id = $2 AND purpose = $3
		ORDER BY CASE WHEN status = 'active' THEN 0 ELSE 1 END, created_at DESC
	`, project.ID, shot.ID, purpose)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	history := make([]ShotReferencePackDetail, 0)
	for rows.Next() {
		var item ShotReferencePackDetail
		if err := rows.Scan(
			&item.ID, &item.ProductionGenerationID, &item.StoryboardShotID,
			&item.Purpose,
			&item.ProfileSnapshotHash, &item.ShotStateHash, &item.CapabilitySnapshotHash,
			&item.Manifest, &item.ManifestHash, &item.Status, &item.CreatedAt,
		); err != nil {
			s.writeError(w, r, err)
			return
		}
		history = append(history, item)
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, r, err)
		return
	}
	var pack *ShotReferencePackDetail
	if len(history) > 0 {
		pack = &history[0]
	}
	references := make([]ShotReferencePackItemDetail, 0)
	if pack != nil {
		itemRows, err := s.db.Query(r.Context(), `
			SELECT id::text, reference_key, role, media_type, semantics, required, priority, source_type,
			       source_id::text, asset_id::text, artifact_id::text, media_file_id::text,
			       storage_key, content_hash, metadata
			FROM shot_reference_pack_items
			WHERE reference_pack_id = $1
			ORDER BY required DESC, priority DESC, reference_key
		`, pack.ID)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		defer itemRows.Close()
		for itemRows.Next() {
			var item ShotReferencePackItemDetail
			if err := itemRows.Scan(
				&item.ID, &item.ReferenceKey, &item.Role, &item.MediaType, &item.Semantics, &item.Required, &item.Priority,
				&item.SourceType, &item.SourceID, &item.AssetID, &item.ArtifactID,
				&item.MediaFileID, &item.StorageKey, &item.ContentHash, &item.Metadata,
			); err != nil {
				s.writeError(w, r, err)
				return
			}
			if item.StorageKey != nil {
				item.PreviewURL = s.previewURLForStorageKeyRequest(r.Context(), *item.StorageKey)
			}
			references = append(references, item)
		}
		if err := itemRows.Err(); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"pack": pack, "items": references, "history": history}, nil)
}

func (s *Server) getStoryboardShotStoryboardSheet(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
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
		SELECT manifest.id::text, manifest.production_generation_id::text,
		       manifest.storyboard_shot_id::text, manifest.sheet_anchor_id::text,
		       manifest.revision, manifest.contract_version, manifest.planned_duration_ticks,
		       manifest.timeline_timebase, manifest.video_aspect_ratio, manifest.sheet_aspect_ratio,
		       manifest.grid_rows, manifest.grid_columns, manifest.panel_count,
		       manifest.entry_state_hash, manifest.exit_state_hash, manifest.manifest,
		       manifest.manifest_hash, manifest.status, manifest.review_status,
		       manifest.reviewer_prompt_version_id::text, manifest.reviewer_provider_call_id::text,
		       manifest.reviewer_model_id::text, manifest.reviewer_output, manifest.metadata,
		       manifest.created_at, manifest.updated_at, manifest.reviewed_at,
		       anchor.storage_key
		FROM storyboard_sheet_manifests manifest
		JOIN shot_visual_anchors anchor ON anchor.id = manifest.sheet_anchor_id
		WHERE manifest.project_id = $1 AND manifest.storyboard_shot_id = $2
		ORDER BY CASE WHEN manifest.status IN ('draft', 'processing', 'ready') THEN 0 ELSE 1 END,
		         manifest.revision DESC
	`, project.ID, shot.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	history := make([]StoryboardSheetManifestDetail, 0)
	var active *StoryboardSheetManifestDetail
	for rows.Next() {
		var item StoryboardSheetManifestDetail
		var sheetStorageKey *string
		if err := rows.Scan(
			&item.ID, &item.ProductionGenerationID, &item.StoryboardShotID, &item.SheetAnchorID,
			&item.Revision, &item.ContractVersion, &item.PlannedDurationTicks, &item.TimelineTimebase,
			&item.VideoAspectRatio, &item.SheetAspectRatio, &item.GridRows, &item.GridColumns,
			&item.PanelCount, &item.EntryStateHash, &item.ExitStateHash, &item.Manifest,
			&item.ManifestHash, &item.Status, &item.ReviewStatus, &item.ReviewerPromptVersionID,
			&item.ReviewerProviderCallID, &item.ReviewerModelID, &item.ReviewerOutput, &item.Metadata,
			&item.CreatedAt, &item.UpdatedAt, &item.ReviewedAt, &sheetStorageKey,
		); err != nil {
			s.writeError(w, r, err)
			return
		}
		if sheetStorageKey != nil {
			item.SheetPreviewURL = s.previewURLForStorageKeyRequest(r.Context(), *sheetStorageKey)
		}
		history = append(history, item)
		if active == nil && (item.Status == "draft" || item.Status == "processing" || item.Status == "ready") {
			copy := item
			active = &copy
		}
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, r, err)
		return
	}
	var selected *StoryboardSheetManifestDetail
	if active != nil {
		selected = active
	} else if len(history) > 0 {
		copy := history[0]
		selected = &copy
	}
	panels := make([]StoryboardSheetPanelDetail, 0)
	if selected != nil {
		panelRows, err := s.db.Query(r.Context(), `
			SELECT id::text, production_generation_id::text, storyboard_shot_id::text,
			       manifest_id::text, visual_anchor_id::text, ordinal, grid_row, grid_column,
			       time_tick, normalized_position, stage, action_stage, expected_state,
			       expected_state_hash, status, review_status, artifact_id::text,
			       media_file_id::text, storage_key, content_hash, crop, metadata,
			       created_at, updated_at
			FROM storyboard_sheet_panels
			WHERE manifest_id = $1
			ORDER BY ordinal
		`, selected.ID)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		defer panelRows.Close()
		for panelRows.Next() {
			var item StoryboardSheetPanelDetail
			if err := panelRows.Scan(
				&item.ID, &item.ProductionGenerationID, &item.StoryboardShotID,
				&item.ManifestID, &item.VisualAnchorID, &item.Ordinal, &item.GridRow,
				&item.GridColumn, &item.TimeTick, &item.NormalizedPosition, &item.Stage,
				&item.ActionStage, &item.ExpectedState, &item.ExpectedStateHash, &item.Status,
				&item.ReviewStatus, &item.ArtifactID, &item.MediaFileID, &item.StorageKey,
				&item.ContentHash, &item.Crop, &item.Metadata, &item.CreatedAt, &item.UpdatedAt,
			); err != nil {
				s.writeError(w, r, err)
				return
			}
			if item.StorageKey != nil {
				item.PreviewURL = s.previewURLForStorageKeyRequest(r.Context(), *item.StorageKey)
			}
			panels = append(panels, item)
		}
		if err := panelRows.Err(); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"active": active, "manifest": selected, "panels": panels, "history": history,
	}, nil)
}

func (s *Server) getStoryboardShotVideoPromptPlan(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	shot, err := s.storyboardShotByID(r, project.ID, r.PathValue("shotId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	items, err := s.videoPromptPlans(r, project.ID, shot.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var active *VideoPromptPlanDetail
	for index := range items {
		if items[index].Status == "approved" {
			active = &items[index]
			break
		}
	}
	var contextPlan *PromptContextPlanDetail
	contextPlanID := ""
	if active != nil {
		contextPlanID = active.PromptContextPlanID
	} else if len(items) > 0 {
		contextPlanID = items[0].PromptContextPlanID
	}
	if contextPlanID != "" {
		contextPlan, err = s.promptContextPlan(r, project.ID, shot.ID, contextPlanID)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"active": active, "items": items, "contextPlan": contextPlan}, nil)
}

func (s *Server) videoPromptPlans(r *http.Request, projectID, shotID string) ([]VideoPromptPlanDetail, error) {
	rows, err := s.db.Query(r.Context(), `
		SELECT id::text, production_generation_id::text, video_production_binding_id::text,
		       video_production_binding_revision, profile_version_id::text,
		       storyboard_shot_id::text, prompt_context_plan_id::text, prompt_version_id::text,
		       reviewer_prompt_version_id::text, workflow_run_id::text, node_run_id::text,
		       provider_call_id::text, reviewer_provider_call_id::text, provider_model_id::text,
		       revision, status, rendered_prompt, prompt_hash, prompt_context_plan_hash,
		       profile_snapshot_hash, shot_state_hash, transition_hash, reference_pack_hash,
		       capability_snapshot_hash, input_contract_version, dialogue_cues,
		       native_audio_required, audio_strategy, audio_requirement, reviewer_output,
		       metadata, created_at, reviewed_at, approved_at, stale_at, archived_at
		FROM video_prompt_plans
		WHERE project_id = $1 AND storyboard_shot_id = $2
		ORDER BY revision DESC
	`, projectID, shotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]VideoPromptPlanDetail, 0)
	for rows.Next() {
		var item VideoPromptPlanDetail
		if err := rows.Scan(
			&item.ID, &item.ProductionGenerationID, &item.VideoProductionBindingID,
			&item.VideoProductionBindingRevision, &item.ProfileVersionID,
			&item.StoryboardShotID, &item.PromptContextPlanID, &item.PromptVersionID,
			&item.ReviewerPromptVersionID, &item.WorkflowRunID, &item.NodeRunID,
			&item.ProviderCallID, &item.ReviewerProviderCallID, &item.ProviderModelID,
			&item.Revision, &item.Status, &item.RenderedPrompt, &item.PromptHash,
			&item.PromptContextPlanHash, &item.ProfileSnapshotHash, &item.ShotStateHash,
			&item.TransitionHash, &item.ReferencePackHash, &item.CapabilitySnapshotHash,
			&item.InputContractVersion, &item.DialogueCues, &item.NativeAudioRequired,
			&item.AudioStrategy, &item.AudioRequirement, &item.ReviewerOutput, &item.Metadata,
			&item.CreatedAt, &item.ReviewedAt, &item.ApprovedAt, &item.StaleAt, &item.ArchivedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) promptContextPlan(r *http.Request, projectID, shotID, planID string) (*PromptContextPlanDetail, error) {
	var item PromptContextPlanDetail
	err := s.db.QueryRow(r.Context(), `
		SELECT id::text, production_generation_id::text, video_production_binding_id::text,
		       video_production_binding_revision, storyboard_plan_id::text,
		       storyboard_shot_id::text, script_episode_id::text, script_scene_id::text,
		       revision, status, episode_continuity_digest, current_scene_script,
		       adjacent_scene_summaries, current_shot_state, verbatim_dialogue_cues,
		       model_context_limit, model_prompt_limit, budget_allocation, source_hashes,
		       plan_hash, created_at, stale_at, archived_at
		FROM prompt_context_plans
		WHERE project_id = $1 AND storyboard_shot_id = $2 AND id = $3
	`, projectID, shotID, planID).Scan(
		&item.ID, &item.ProductionGenerationID, &item.VideoProductionBindingID,
		&item.VideoProductionBindingRevision, &item.StoryboardPlanID, &item.StoryboardShotID,
		&item.ScriptEpisodeID, &item.ScriptSceneID, &item.Revision, &item.Status,
		&item.EpisodeContinuityDigest, &item.CurrentSceneScript, &item.AdjacentSceneSummaries,
		&item.CurrentShotState, &item.VerbatimDialogueCues, &item.ModelContextLimit,
		&item.ModelPromptLimit, &item.BudgetAllocation, &item.SourceHashes, &item.PlanHash,
		&item.CreatedAt, &item.StaleAt, &item.ArchivedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}
