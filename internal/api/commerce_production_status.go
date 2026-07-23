package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type commerceProjectProductionStatus struct {
	Overall             commerceProjectOverallStatus `json:"overall"`
	Product             commerceProductStageStatus   `json:"product"`
	ScriptUnitsRevision int64                        `json:"scriptUnitsRevision"`
}

type commerceProjectOverallStatus struct {
	Status                          string `json:"status"`
	ProjectGenerationID             string `json:"projectGenerationId,omitempty"`
	CommerceWorkflowBindingRevision int64  `json:"commerceWorkflowBindingRevision"`
	VideoProductionBindingRevision  int64  `json:"videoProductionBindingRevision"`
	ScriptUnitCount                 int    `json:"scriptUnitCount"`
	CompletedScriptUnitCount        int    `json:"completedScriptUnitCount"`
	RunningScriptUnitCount          int    `json:"runningScriptUnitCount"`
	FailedScriptUnitCount           int    `json:"failedScriptUnitCount"`
	NeedsReviewScriptUnitCount      int    `json:"needsReviewScriptUnitCount"`
}

type commerceProductStageStatus struct {
	Status           string `json:"status"`
	ProductVersionID string `json:"productVersionId,omitempty"`
	ReferenceCount   int    `json:"referenceCount"`
}

type commerceUnitProductionStatus struct {
	ScriptUnitID          string                       `json:"scriptUnitId"`
	UnitNo                int64                        `json:"unitNo"`
	SortOrder             int64                        `json:"sortOrder"`
	Title                 string                       `json:"title"`
	UnitGenerationID      string                       `json:"unitGenerationId,omitempty"`
	UnitGenerationNo      int64                        `json:"unitGenerationNo"`
	TargetLanguage        string                       `json:"targetLanguage,omitempty"`
	TargetDurationSeconds int                          `json:"targetDurationSeconds"`
	Status                string                       `json:"status"`
	Progress              int                          `json:"progress"`
	NextAction            string                       `json:"nextAction"`
	Stages                commerceUnitProductionStages `json:"stages"`
}

type commerceUnitProductionStages struct {
	Setup           commerceSetupStageStatus      `json:"setup"`
	Language        commerceLanguageStageStatus   `json:"language"`
	Script          commerceScriptStageStatus     `json:"script"`
	Storyboard      commerceStoryboardStageStatus `json:"storyboard"`
	ReferenceImages commerceCountStageStatus      `json:"referenceImages"`
	VideoPrompts    commercePromptStageStatus     `json:"videoPrompts"`
	ShotVideos      commerceCountStageStatus      `json:"shotVideos"`
	FinalVideo      commerceFinalStageStatus      `json:"finalVideo"`
}

type commerceSetupStageStatus struct {
	Status   string `json:"status"`
	Revision int64  `json:"revision"`
}

type commerceLanguageStageStatus struct {
	Status         string   `json:"status"`
	Mode           string   `json:"mode"`
	SourceLanguage string   `json:"sourceLanguage,omitempty"`
	TargetLanguage string   `json:"targetLanguage,omitempty"`
	Confidence     *float64 `json:"confidence,omitempty"`
}

type commerceScriptStageStatus struct {
	Status              string `json:"status"`
	SourceVersion       int    `json:"sourceVersion"`
	LocalizationVersion int    `json:"localizationVersion"`
}

type commerceStoryboardStageStatus struct {
	Status       string `json:"status"`
	PlanID       string `json:"planId,omitempty"`
	PlanRevision int64  `json:"planRevision"`
	ShotCount    int    `json:"shotCount"`
}

type commerceCountStageStatus struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Running   int `json:"running"`
}

type commercePromptStageStatus struct {
	Total    int `json:"total"`
	Approved int `json:"approved"`
	Failed   int `json:"failed"`
	Running  int `json:"running"`
}

type commerceFinalStageStatus struct {
	Status              string `json:"status"`
	TimelineID          string `json:"timelineId,omitempty"`
	FinalVideoVersionID string `json:"finalVideoVersionId,omitempty"`
}

type commerceUnitProductionSnapshot struct {
	ScriptUnitID          string
	UnitNo                int64
	SortOrder             int64
	Title                 string
	UnitStatus            string
	UnitRevision          int64
	LanguageMode          string
	ExplicitLanguage      string
	TargetDurationSeconds int
	UnitGenerationID      string
	UnitGenerationNo      int64
	SourceVersion         int
	LocalizationVersion   int
	LocalizationStatus    string
	ResolutionStatus      string
	SourceLanguage        string
	TargetLanguage        string
	Confidence            *float64
	PlanID                string
	PlanRevision          int64
	PlanStatus            string
	ShotCount             int
	ImageSucceeded        int
	ImageFailed           int
	ImageRunning          int
	PromptApproved        int
	PromptFailed          int
	PromptRunning         int
	VideoSucceeded        int
	VideoFailed           int
	VideoRunning          int
	ActiveRunCount        int
	FailedRunCount        int
	TimelineID            string
	FinalVideoVersionID   string
	FinalVideoStatus      string
	FinalVideoReadiness   string
	FinalVideoStaleState  string
}

func (s *Server) getCommerceProjectProductionStatus(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	item, err := s.loadCommerceProjectProductionStatus(r.Context(), project)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) getCommerceUnitProductionStatus(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	snapshots, err := s.loadCommerceUnitProductionSnapshots(r.Context(), project, []string{r.PathValue("scriptUnitId")})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if len(snapshots) == 0 {
		s.writeError(w, r, pgx.ErrNoRows)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, commerceUnitStatusFromSnapshot(snapshots[0]), nil)
}

func (s *Server) attachCommerceProductionSummaries(ctx context.Context, project Project, items []commercepkg.ScriptUnit) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	snapshots, err := s.loadCommerceUnitProductionSnapshots(ctx, project, ids)
	if err != nil {
		return err
	}
	byID := make(map[string]commercepkg.ScriptUnitProductionSummary, len(snapshots))
	for _, snapshot := range snapshots {
		status := commerceUnitStatusFromSnapshot(snapshot)
		failedCount := status.Stages.ReferenceImages.Failed + status.Stages.VideoPrompts.Failed + status.Stages.ShotVideos.Failed
		if failedCount == 0 {
			failedCount = snapshot.FailedRunCount
		}
		byID[snapshot.ScriptUnitID] = commercepkg.ScriptUnitProductionSummary{
			Status: status.Status, CurrentStage: commerceCurrentStage(status), Progress: status.Progress,
			FailedCount: failedCount, FinalVideoStatus: status.Stages.FinalVideo.Status,
		}
	}
	for index := range items {
		if summary, found := byID[items[index].ID]; found {
			items[index].ProductionSummary = &summary
		}
	}
	return nil
}

func (s *Server) loadCommerceUnitProductionSnapshots(ctx context.Context, project Project, ids []string) ([]commerceUnitProductionSnapshot, error) {
	uuidIDs := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return nil, commercepkg.Error{Code: commercepkg.CodeGenerationMismatch, Message: "脚本单元标识无效", Cause: err}
		}
		uuidIDs = append(uuidIDs, parsed)
	}
	rows, err := s.db.Query(ctx, commerceUnitProductionSnapshotSQL, project.OrganizationID, project.ID, uuidIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]commerceUnitProductionSnapshot, 0, len(ids))
	for rows.Next() {
		var item commerceUnitProductionSnapshot
		var explicitLanguage, generationID, sourceLanguage, targetLanguage sql.NullString
		var confidence sql.NullFloat64
		var planID, timelineID, finalVideoID sql.NullString
		if err := rows.Scan(
			&item.ScriptUnitID, &item.UnitNo, &item.SortOrder, &item.Title, &item.UnitStatus,
			&item.UnitRevision, &item.LanguageMode, &explicitLanguage, &item.TargetDurationSeconds,
			&generationID, &item.UnitGenerationNo, &item.SourceVersion, &item.LocalizationVersion,
			&item.LocalizationStatus, &item.ResolutionStatus, &sourceLanguage, &targetLanguage, &confidence,
			&planID, &item.PlanRevision, &item.PlanStatus, &item.ShotCount,
			&item.ImageSucceeded, &item.ImageFailed, &item.ImageRunning,
			&item.PromptApproved, &item.PromptFailed, &item.PromptRunning,
			&item.VideoSucceeded, &item.VideoFailed, &item.VideoRunning,
			&item.ActiveRunCount, &item.FailedRunCount, &timelineID, &finalVideoID,
			&item.FinalVideoStatus, &item.FinalVideoReadiness, &item.FinalVideoStaleState,
		); err != nil {
			return nil, err
		}
		item.ExplicitLanguage = explicitLanguage.String
		item.UnitGenerationID = generationID.String
		item.SourceLanguage = sourceLanguage.String
		item.TargetLanguage = targetLanguage.String
		if confidence.Valid {
			item.Confidence = &confidence.Float64
		}
		item.PlanID = planID.String
		item.TimelineID = timelineID.String
		item.FinalVideoVersionID = finalVideoID.String
		items = append(items, item)
	}
	return items, rows.Err()
}

func commerceUnitStatusFromSnapshot(item commerceUnitProductionSnapshot) commerceUnitProductionStatus {
	targetLanguage := item.TargetLanguage
	if targetLanguage == "" {
		targetLanguage = item.ExplicitLanguage
	}
	status := commerceUnitProductionStatus{
		ScriptUnitID: item.ScriptUnitID, UnitNo: item.UnitNo, SortOrder: item.SortOrder, Title: item.Title,
		UnitGenerationID: item.UnitGenerationID, UnitGenerationNo: item.UnitGenerationNo,
		TargetLanguage: targetLanguage, TargetDurationSeconds: item.TargetDurationSeconds,
		Stages: commerceUnitProductionStages{
			Setup: commerceSetupStageStatus{Status: setupStageStatus(item), Revision: item.UnitRevision},
			Language: commerceLanguageStageStatus{Status: defaultStatus(item.ResolutionStatus, "pending"), Mode: item.LanguageMode,
				SourceLanguage: item.SourceLanguage, TargetLanguage: targetLanguage, Confidence: item.Confidence},
			Script: commerceScriptStageStatus{Status: scriptStageStatus(item), SourceVersion: item.SourceVersion, LocalizationVersion: item.LocalizationVersion},
			Storyboard: commerceStoryboardStageStatus{Status: defaultStatus(item.PlanStatus, "pending"), PlanID: item.PlanID,
				PlanRevision: item.PlanRevision, ShotCount: item.ShotCount},
			ReferenceImages: commerceCountStageStatus{Total: item.ShotCount, Succeeded: item.ImageSucceeded, Failed: item.ImageFailed, Running: item.ImageRunning},
			VideoPrompts:    commercePromptStageStatus{Total: item.ShotCount, Approved: item.PromptApproved, Failed: item.PromptFailed, Running: item.PromptRunning},
			ShotVideos:      commerceCountStageStatus{Total: item.ShotCount, Succeeded: item.VideoSucceeded, Failed: item.VideoFailed, Running: item.VideoRunning},
			FinalVideo:      commerceFinalStageStatus{Status: finalStageStatus(item), TimelineID: item.TimelineID, FinalVideoVersionID: item.FinalVideoVersionID},
		},
	}
	status.Progress = commerceUnitProgress(status)
	status.NextAction = commerceNextAction(status, item.ActiveRunCount > 0)
	status.Status = commerceUnitOverallStatus(status, item)
	return status
}

func setupStageStatus(item commerceUnitProductionSnapshot) string {
	if item.UnitGenerationID != "" {
		return "completed"
	}
	if item.SourceVersion > 0 {
		return "ready"
	}
	return "pending"
}

func scriptStageStatus(item commerceUnitProductionSnapshot) string {
	if item.SourceVersion == 0 {
		return "pending"
	}
	if item.LocalizationStatus == "approved" {
		return "ready"
	}
	if item.LocalizationVersion > 0 {
		return item.LocalizationStatus
	}
	return "source_ready"
}

func finalStageStatus(item commerceUnitProductionSnapshot) string {
	if item.FinalVideoVersionID == "" {
		return "pending"
	}
	if item.FinalVideoStaleState != "" && item.FinalVideoStaleState != "fresh" {
		return "stale"
	}
	if item.FinalVideoReadiness != "ready" {
		return item.FinalVideoReadiness
	}
	return item.FinalVideoStatus
}

func commerceUnitProgress(item commerceUnitProductionStatus) int {
	progress := 0.0
	if item.Stages.Setup.Status == "completed" {
		progress += 10
	}
	if item.Stages.Language.Status == "confirmed" {
		progress += 10
	}
	if item.Stages.Script.Status == "ready" {
		progress += 10
	}
	if item.Stages.Storyboard.Status == "ready" {
		progress += 15
	}
	progress += 15 * completionRatio(item.Stages.ReferenceImages.Succeeded, item.Stages.ReferenceImages.Total)
	progress += 15 * completionRatio(item.Stages.VideoPrompts.Approved, item.Stages.VideoPrompts.Total)
	progress += 20 * completionRatio(item.Stages.ShotVideos.Succeeded, item.Stages.ShotVideos.Total)
	if item.Stages.FinalVideo.Status == "ready" || item.Stages.FinalVideo.Status == "active" {
		progress += 5
	}
	if progress > 100 {
		progress = 100
	}
	return int(progress + 0.5)
}

func completionRatio(completed, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(completed) / float64(total)
}

func commerceNextAction(item commerceUnitProductionStatus, running bool) string {
	if running {
		return "view_activity"
	}
	if item.Stages.Setup.Status != "completed" {
		return "prepare_script_unit"
	}
	if item.Stages.Language.Status != "confirmed" {
		return "confirm_language"
	}
	if item.Stages.Script.Status != "ready" {
		return "review_localization"
	}
	if item.Stages.Storyboard.Status != "ready" {
		return "generate_storyboard"
	}
	if item.Stages.ReferenceImages.Failed > 0 || item.Stages.VideoPrompts.Failed > 0 || item.Stages.ShotVideos.Failed > 0 {
		return "retry_failed"
	}
	if item.Stages.ReferenceImages.Succeeded < item.Stages.ReferenceImages.Total {
		return "generate_reference_images"
	}
	if item.Stages.VideoPrompts.Approved < item.Stages.VideoPrompts.Total {
		return "generate_video_prompts"
	}
	if item.Stages.ShotVideos.Succeeded < item.Stages.ShotVideos.Total {
		return "generate_shot_videos"
	}
	if item.Stages.FinalVideo.Status != "ready" && item.Stages.FinalVideo.Status != "active" {
		return "compose_final_video"
	}
	return "view_final_video"
}

func commerceUnitOverallStatus(status commerceUnitProductionStatus, snapshot commerceUnitProductionSnapshot) string {
	if snapshot.ActiveRunCount > 0 || status.Stages.ReferenceImages.Running > 0 || status.Stages.VideoPrompts.Running > 0 || status.Stages.ShotVideos.Running > 0 {
		return "running"
	}
	if status.Stages.FinalVideo.Status == "ready" || status.Stages.FinalVideo.Status == "active" {
		return "completed"
	}
	if status.Stages.Language.Status == "needs_confirmation" || status.Stages.Script.Status == "reviewing" || status.Stages.Script.Status == "changes_requested" {
		return "needs_review"
	}
	if status.Stages.ReferenceImages.Failed+status.Stages.VideoPrompts.Failed+status.Stages.ShotVideos.Failed > 0 || snapshot.FailedRunCount > 0 {
		return "failed"
	}
	return "pending"
}

func commerceCurrentStage(status commerceUnitProductionStatus) string {
	switch status.NextAction {
	case "prepare_script_unit":
		return "draft"
	case "confirm_language":
		return "language_resolution"
	case "review_localization":
		return "localization"
	case "generate_storyboard":
		return "storyboard"
	case "generate_reference_images":
		return "reference_images"
	case "generate_video_prompts":
		return "video_prompts"
	case "generate_shot_videos":
		return "shot_videos"
	case "compose_final_video":
		return "final_video"
	case "view_final_video":
		return "completed"
	case "retry_failed":
		return "failed"
	default:
		return "running"
	}
}

func defaultStatus(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (s *Server) loadCommerceProjectProductionStatus(ctx context.Context, project Project) (commerceProjectProductionStatus, error) {
	var item commerceProjectProductionStatus
	var generationID, productVersionID sql.NullString
	if err := s.db.QueryRow(ctx, commerceProjectProductionStatusSQL, project.OrganizationID, project.ID).Scan(
		&generationID, &item.Overall.CommerceWorkflowBindingRevision, &item.Overall.VideoProductionBindingRevision,
		&item.Product.Status, &productVersionID, &item.Product.ReferenceCount, &item.ScriptUnitsRevision,
		&item.Overall.ScriptUnitCount, &item.Overall.CompletedScriptUnitCount, &item.Overall.RunningScriptUnitCount,
		&item.Overall.FailedScriptUnitCount, &item.Overall.NeedsReviewScriptUnitCount,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return commerceProjectProductionStatus{}, commercepkg.Error{Code: commercepkg.CodeProjectKindMismatch, Message: "带货视频商品配置不存在", Cause: err}
		}
		return commerceProjectProductionStatus{}, err
	}
	item.Overall.ProjectGenerationID = generationID.String
	item.Product.ProductVersionID = productVersionID.String
	switch {
	case item.Overall.RunningScriptUnitCount > 0:
		item.Overall.Status = "running"
	case item.Overall.FailedScriptUnitCount > 0:
		item.Overall.Status = "failed"
	case item.Overall.NeedsReviewScriptUnitCount > 0:
		item.Overall.Status = "needs_review"
	case item.Overall.ScriptUnitCount > 0 && item.Overall.CompletedScriptUnitCount == item.Overall.ScriptUnitCount:
		item.Overall.Status = "completed"
	case item.Overall.ProjectGenerationID == "":
		item.Overall.Status = "unconfigured"
	default:
		item.Overall.Status = "pending"
	}
	return item, nil
}

const commerceUnitProductionSnapshotSQL = `
	SELECT unit.id::text, unit.unit_no, unit.sort_order, unit.title, unit.status, unit.revision,
	       unit.language_mode, unit.explicit_target_language, unit.target_duration_seconds,
	       generation.id::text, COALESCE(generation.unit_generation_no, 0),
	       COALESCE(source.version, 0), COALESCE(localization.version, 0),
	       COALESCE(localization.status, ''), COALESCE(resolution.status, ''),
	       resolution.source_language, COALESCE(localization.target_language, resolution.target_language), resolution.confidence,
	       plan.id::text, COALESCE(plan.edit_revision, 0), COALESCE(plan.status, ''),
	       COALESCE(shots.total, 0), COALESCE(shots.image_succeeded, 0), COALESCE(shots.image_failed, 0),
	       COALESCE(shots.image_running, 0), COALESCE(shots.prompt_approved, 0), COALESCE(shots.prompt_failed, 0),
	       COALESCE(shots.prompt_running, 0), COALESCE(shots.video_succeeded, 0), COALESCE(shots.video_failed, 0),
	       COALESCE(shots.video_running, 0), COALESCE(runs.active_count, 0), COALESCE(runs.failed_count, 0),
	       final.timeline_id::text, final.id::text, COALESCE(final.status, ''),
	       COALESCE(final.production_readiness, ''), COALESCE(final.metadata->>'staleState', 'fresh')
	FROM commerce_script_units unit
	LEFT JOIN commerce_script_unit_generations generation ON generation.id = unit.active_unit_generation_id AND generation.status = 'active'
	LEFT JOIN commerce_ad_script_versions source ON source.id = unit.current_source_version_id
	LEFT JOIN commerce_ad_script_localizations localization ON localization.id = unit.current_localization_id
	LEFT JOIN LATERAL (
		SELECT candidate.* FROM commerce_language_resolutions candidate
		WHERE candidate.script_unit_id = unit.id AND candidate.source_script_version_id = unit.current_source_version_id
		ORDER BY candidate.created_at DESC LIMIT 1
	) resolution ON true
	LEFT JOIN LATERAL (
		SELECT candidate.* FROM commerce_storyboard_plans candidate
		WHERE candidate.script_unit_id = unit.id AND candidate.script_unit_generation_id = generation.id
		  AND candidate.status <> 'archived'
		ORDER BY candidate.active DESC, candidate.edit_revision DESC, candidate.created_at DESC LIMIT 1
	) plan ON true
	LEFT JOIN LATERAL (
		SELECT count(*)::int AS total,
		       count(*) FILTER (WHERE shot.image_status = 'succeeded')::int AS image_succeeded,
		       count(*) FILTER (WHERE shot.image_status = 'failed')::int AS image_failed,
		       count(*) FILTER (WHERE shot.image_status IN ('queued', 'running'))::int AS image_running,
		       count(*) FILTER (WHERE shot.video_prompt_status = 'succeeded')::int AS prompt_approved,
		       count(*) FILTER (WHERE shot.video_prompt_status = 'failed')::int AS prompt_failed,
		       count(*) FILTER (WHERE shot.video_prompt_status IN ('queued', 'running'))::int AS prompt_running,
		       count(*) FILTER (WHERE shot.video_status = 'succeeded')::int AS video_succeeded,
		       count(*) FILTER (WHERE shot.video_status = 'failed')::int AS video_failed,
		       count(*) FILTER (WHERE shot.video_status IN ('queued', 'running'))::int AS video_running
		FROM storyboard_shots shot
		WHERE shot.commerce_storyboard_plan_id = plan.id AND shot.deleted_at IS NULL
	) shots ON true
	LEFT JOIN LATERAL (
		SELECT count(*) FILTER (WHERE latest.status IN ('queued', 'running', 'cancelling'))::int AS active_count,
		       count(*) FILTER (WHERE latest.status IN ('failed', 'partially_succeeded'))::int AS failed_count
		FROM (
			SELECT DISTINCT ON (run.run_type) run.status
			FROM commerce_production_runs run
			WHERE run.script_unit_id = unit.id AND run.script_unit_generation_id = generation.id
			ORDER BY run.run_type, run.created_at DESC, run.id DESC
		) latest
	) runs ON true
	LEFT JOIN LATERAL (
		SELECT version.* FROM final_video_versions version
		WHERE version.commerce_script_unit_id = unit.id
		  AND version.commerce_script_unit_generation_id = generation.id
		ORDER BY CASE version.status WHEN 'active' THEN 0 WHEN 'ready' THEN 1 ELSE 2 END, version.version DESC LIMIT 1
	) final ON true
	WHERE unit.organization_id = $1 AND unit.project_id = $2 AND unit.id = ANY($3::uuid[])
	ORDER BY unit.sort_order, unit.id
`

const commerceProjectProductionStatusSQL = `
	WITH unit_state AS (
		SELECT unit.id,
		       COALESCE(run_state.running, false) AS running,
		EXISTS (
			SELECT 1 FROM final_video_versions final
			WHERE final.commerce_script_unit_id = unit.id
			  AND final.commerce_script_unit_generation_id = generation.id
			  AND final.status IN ('ready', 'active') AND final.production_readiness = 'ready'
			  AND COALESCE(final.metadata->>'staleState', 'fresh') = 'fresh'
		) AS completed,
		COALESCE(run_state.failed, false) AS failed,
		COALESCE(resolution.status, '') = 'needs_confirmation'
		  OR (localization.id IS NOT NULL AND localization.status IN ('reviewing', 'rejected', 'changes_requested')) AS needs_review
		FROM commerce_script_units unit
		LEFT JOIN commerce_script_unit_generations generation ON generation.id = unit.active_unit_generation_id AND generation.status = 'active'
		LEFT JOIN commerce_ad_script_localizations localization ON localization.id = unit.current_localization_id
		LEFT JOIN LATERAL (
			SELECT candidate.status
			FROM commerce_language_resolutions candidate
			WHERE candidate.script_unit_id = unit.id AND candidate.source_script_version_id = unit.current_source_version_id
			ORDER BY candidate.created_at DESC LIMIT 1
		) resolution ON true
		LEFT JOIN LATERAL (
			SELECT bool_or(latest.status IN ('queued', 'running', 'cancelling')) AS running,
			       bool_or(latest.status IN ('failed', 'partially_succeeded')) AS failed
			FROM (
				SELECT DISTINCT ON (run.run_type) run.status
				FROM commerce_production_runs run
				WHERE run.script_unit_id = unit.id AND run.script_unit_generation_id = generation.id
				ORDER BY run.run_type, run.created_at DESC, run.id DESC
			) latest
		) run_state ON true
		WHERE unit.organization_id = $1 AND unit.project_id = $2 AND unit.status <> 'archived'
	), counts AS (
		SELECT count(*)::int AS total,
		       count(*) FILTER (WHERE completed)::int AS completed,
		       count(*) FILTER (WHERE running)::int AS running,
		       count(*) FILTER (WHERE failed AND NOT running)::int AS failed,
		       count(*) FILTER (WHERE needs_review AND NOT running)::int AS needs_review
		FROM unit_state
	)
	SELECT generation.id::text, COALESCE(commerce_binding.binding_revision, 0), COALESCE(video_binding.revision, 0),
	       product.status, product.current_version_id::text,
	       (SELECT count(*)::int FROM commerce_product_references reference WHERE reference.product_id = product.id AND reference.status = 'active'),
	       product.script_units_revision, counts.total, counts.completed, counts.running, counts.failed, counts.needs_review
	FROM projects project
	JOIN commerce_products product ON product.project_id = project.id AND product.organization_id = project.organization_id
	LEFT JOIN project_video_production_generations generation ON generation.id = project.active_video_production_generation_id
	LEFT JOIN project_video_production_bindings video_binding ON video_binding.id = generation.binding_id
	LEFT JOIN project_commerce_workflow_bindings commerce_binding ON commerce_binding.id = generation.commerce_workflow_binding_id
	CROSS JOIN counts
	WHERE project.organization_id = $1 AND project.id = $2 AND project.project_kind = 'commerce_video'
`
