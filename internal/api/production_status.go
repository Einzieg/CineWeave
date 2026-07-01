package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/jackc/pgx/v5"
)

type ProductionStatus struct {
	ProjectID string                   `json:"projectId"`
	Project   ProductionProjectSummary `json:"project"`
	Overall   ProductionOverall        `json:"overall"`
	Stages    ProductionStages         `json:"stages"`
}

type ProductionProjectSummary struct {
	Name        string `json:"name"`
	ProjectType string `json:"projectType"`
	ContentType string `json:"contentType"`
	VideoRatio  string `json:"videoRatio"`
	ArtStyle    string `json:"artStyle"`
}

type ProductionOverall struct {
	Stage    string `json:"stage"`
	Progress int    `json:"progress"`
	Status   string `json:"status"`
}

type ProductionStages struct {
	Source     ProductionSourceStage     `json:"source"`
	Assets     ProductionAssetsStage     `json:"assets"`
	Storyboard ProductionStoryboardStage `json:"storyboard"`
	ShotAssets ProductionShotAssetsStage `json:"shotAssets"`
	ShotImages ProductionShotMediaStage  `json:"shotImages"`
	ShotVideos ProductionShotMediaStage  `json:"shotVideos"`
	FinalVideo ProductionFinalVideoStage `json:"finalVideo"`
}

type ProductionSourceStage struct {
	Status                   string   `json:"status"`
	NovelSourceCount         int      `json:"novelSourceCount"`
	ScriptSourceCount        int      `json:"scriptSourceCount"`
	ChapterCount             int      `json:"chapterCount"`
	EventCount               int      `json:"eventCount"`
	ApprovedEventCount       int      `json:"approvedEventCount"`
	PendingEventReviewCount  int      `json:"pendingEventReviewCount"`
	AdaptationPlanCount      int      `json:"adaptationPlanCount"`
	ActiveAdaptationPlanID   *string  `json:"activeAdaptationPlanId"`
	ActiveAdaptationTitle    *string  `json:"activeAdaptationTitle"`
	ActiveAdaptationStatus   *string  `json:"activeAdaptationStatus"`
	ActiveScriptID           *string  `json:"activeScriptId"`
	ActiveScriptTitle        *string  `json:"activeScriptTitle"`
	ScriptSceneCount         int      `json:"scriptSceneCount"`
	ApprovedScriptSceneCount int      `json:"approvedScriptSceneCount"`
	PendingScriptSceneCount  int      `json:"pendingScriptSceneCount"`
	StaleScriptSceneCount    int      `json:"staleScriptSceneCount"`
	Summary                  []string `json:"summary"`
}

type ProductionAssetsStage struct {
	Status                     string              `json:"status"`
	CharacterCount             int                 `json:"characterCount"`
	SceneCount                 int                 `json:"sceneCount"`
	PropCount                  int                 `json:"propCount"`
	ReferenceImageCount        int                 `json:"referenceImageCount"`
	MissingReferenceImageCount int                 `json:"missingReferenceImageCount"`
	ApprovedCount              int                 `json:"approvedCount"`
	PendingReviewCount         int                 `json:"pendingReviewCount"`
	ManualOverrideCount        int                 `json:"manualOverrideCount"`
	StaleCount                 int                 `json:"staleCount"`
	DownstreamStaleCount       int                 `json:"downstreamStaleCount"`
	Summary                    map[string][]string `json:"summary"`
}

type ProductionStoryboardStage struct {
	Status              string   `json:"status"`
	ShotCount           int      `json:"shotCount"`
	ConfirmedShotCount  int      `json:"confirmedShotCount"`
	PendingReviewCount  int      `json:"pendingReviewCount"`
	ManualOverrideCount int      `json:"manualOverrideCount"`
	StaleShotCount      int      `json:"staleShotCount"`
	Summary             []string `json:"summary"`
}

type ProductionShotAssetsStage struct {
	Status                    string   `json:"status"`
	RequirementCount          int      `json:"requirementCount"`
	CharacterRequirementCount int      `json:"characterRequirementCount"`
	SceneRequirementCount     int      `json:"sceneRequirementCount"`
	PropRequirementCount      int      `json:"propRequirementCount"`
	DerivedImageCount         int      `json:"derivedImageCount"`
	MissingDerivedImageCount  int      `json:"missingDerivedImageCount"`
	ApprovedCount             int      `json:"approvedCount"`
	PendingReviewCount        int      `json:"pendingReviewCount"`
	ManualOverrideCount       int      `json:"manualOverrideCount"`
	StaleRequirementCount     int      `json:"staleRequirementCount"`
	Summary                   []string `json:"summary"`
}

type ProductionShotMediaStage struct {
	Status    string `json:"status"`
	Total     int    `json:"total"`
	Succeeded int    `json:"succeeded"`
	Failed    int    `json:"failed"`
	Running   int    `json:"running"`
	Pending   int    `json:"pending"`
	Stale     int    `json:"stale"`
}

type ProductionFinalVideoStage struct {
	Status              string  `json:"status"`
	ArtifactID          *string `json:"artifactId"`
	MediaFileID         *string `json:"mediaFileId"`
	PreviewURL          *string `json:"previewUrl"`
	StorageKey          *string `json:"storageKey"`
	WorkflowRunID       *string `json:"workflowRunId,omitempty"`
	SourceWorkflowRunID *string `json:"sourceWorkflowRunId,omitempty"`
	Stale               bool    `json:"stale"`
}

type ProductionActionRequest struct {
	Action   string         `json:"action"`
	SourceID string         `json:"sourceId"`
	ScriptID string         `json:"scriptId"`
	Options  map[string]any `json:"options"`
}

type ProductionActionResponse struct {
	Action        string `json:"action"`
	WorkflowRunID string `json:"workflowRunId"`
	Status        string `json:"status"`
	WorkflowType  string `json:"workflowType"`
	Note          string `json:"note,omitempty"`
}

type ReviewRequest struct {
	ReviewStatus string `json:"reviewStatus"`
	Note         string `json:"note"`
}

type ReviewResponse struct {
	ID           string    `json:"id"`
	ReviewStatus string    `json:"reviewStatus"`
	Note         *string   `json:"note,omitempty"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type productionWorkflowState struct {
	Running      bool
	Failed       bool
	LatestID     string
	LatestStatus string
}

func (s *Server) getProductionStatus(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	status, err := s.productionStatus(r, project)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, status, nil)
}

func (s *Server) runProductionAction(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req ProductionActionRequest
	if !decode(w, r, &req) {
		return
	}
	action := strings.TrimSpace(req.Action)
	permission, ok := productionActionPermission(action)
	if !ok {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "production action is not supported", nil, false)
		return
	}
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), permission)
	if !ok {
		return
	}
	if req.Options == nil {
		req.Options = map[string]any{}
	}
	workflowType, input, workflowFunc, note, ok := s.productionActionWorkflow(w, r, project, action, req)
	if !ok {
		return
	}
	run, ok := s.startProjectWorkflow(w, r, principal, project, workflowType, input, workflowFunc)
	if !ok {
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, ProductionActionResponse{
		Action:        action,
		WorkflowRunID: run.ID,
		Status:        run.Status,
		WorkflowType:  workflowType,
		Note:          note,
	}, nil)
}

func (s *Server) reviewCanonicalAsset(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite)
	if !ok {
		return
	}
	resp, ok := s.updateReviewStatus(w, r, "canonical_assets", project.ID, r.PathValue("assetId"), principal.UserID)
	if !ok {
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, resp, nil)
}

func (s *Server) reviewStoryboardShot(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	resp, ok := s.updateReviewStatus(w, r, "storyboard_shots", project.ID, r.PathValue("shotId"), principal.UserID)
	if !ok {
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, resp, nil)
}

func (s *Server) reviewShotAssetRequirement(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite)
	if !ok {
		return
	}
	resp, ok := s.updateReviewStatus(w, r, "shot_asset_requirements", project.ID, r.PathValue("requirementId"), principal.UserID)
	if !ok {
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, resp, nil)
}

func (s *Server) productionStatus(r *http.Request, project Project) (ProductionStatus, error) {
	workflowsByType, err := s.loadProductionWorkflowState(r, project.ID)
	if err != nil {
		return ProductionStatus{}, err
	}
	source, err := s.productionSourceStage(r, project.ID)
	if err != nil {
		return ProductionStatus{}, err
	}
	assets, err := s.productionAssetsStage(r, project.ID)
	if err != nil {
		return ProductionStatus{}, err
	}
	storyboard, err := s.productionStoryboardStage(r, project.ID, workflowsByType)
	if err != nil {
		return ProductionStatus{}, err
	}
	shotAssets, err := s.productionShotAssetsStage(r, project.ID)
	if err != nil {
		return ProductionStatus{}, err
	}
	shotImages, err := s.productionShotMediaStage(r, project.ID, "image")
	if err != nil {
		return ProductionStatus{}, err
	}
	shotVideos, err := s.productionShotMediaStage(r, project.ID, "video")
	if err != nil {
		return ProductionStatus{}, err
	}
	finalVideo, err := s.productionFinalVideoStage(r, project.ID, workflowsByType)
	if err != nil {
		return ProductionStatus{}, err
	}
	stages := ProductionStages{
		Source:     source,
		Assets:     assets,
		Storyboard: storyboard,
		ShotAssets: shotAssets,
		ShotImages: shotImages,
		ShotVideos: shotVideos,
		FinalVideo: finalVideo,
	}
	return ProductionStatus{
		ProjectID: project.ID,
		Project: ProductionProjectSummary{
			Name:        project.Name,
			ProjectType: stringValue(project.ProjectType),
			ContentType: stringValue(project.ContentType),
			VideoRatio:  firstNonEmpty(project.VideoRatio, stringValue(project.AspectRatio), "16:9"),
			ArtStyle:    project.ArtStyle,
		},
		Overall: productionOverall(stages),
		Stages:  stages,
	}, nil
}

func (s *Server) productionSourceStage(r *http.Request, projectID string) (ProductionSourceStage, error) {
	var novelCount, scriptSourceCount, chapterCount, eventCount, approvedEventCount, adaptationPlanCount int
	if err := s.db.QueryRow(r.Context(), `
		SELECT
			COUNT(*) FILTER (WHERE source_type = 'novel'),
			COUNT(*) FILTER (WHERE source_type = 'script'),
			(
				SELECT COUNT(*)
				FROM novel_chapters nc
				WHERE nc.project_id = $1 AND nc.source_id IS NOT NULL
			),
			(
				SELECT COUNT(*)
				FROM novel_events ne
				WHERE ne.project_id = $1
			),
			(
				SELECT COUNT(*)
				FROM novel_events ne
				WHERE ne.project_id = $1 AND ne.review_status = 'approved'
			),
			(
				SELECT COUNT(*)
				FROM adaptation_plans ap
				WHERE ap.project_id = $1
			)
		FROM project_sources
		WHERE project_id = $1
	`, projectID).Scan(&novelCount, &scriptSourceCount, &chapterCount, &eventCount, &approvedEventCount, &adaptationPlanCount); err != nil {
		return ProductionSourceStage{}, err
	}
	activeScriptID, activeScriptTitle, err := s.activeProductionScript(r, projectID, "")
	if err != nil {
		return ProductionSourceStage{}, err
	}
	var scriptSceneCount, approvedScriptSceneCount, pendingScriptSceneCount, staleScriptSceneCount int
	if activeScriptID != "" {
		if err := s.db.QueryRow(r.Context(), `
			SELECT
				COUNT(sc.id),
				COUNT(sc.id) FILTER (WHERE sc.review_status = 'approved'),
				COUNT(sc.id) FILTER (WHERE sc.review_status <> 'approved'),
				COUNT(sc.id) FILTER (WHERE sc.stale_state <> 'fresh')
			FROM scripts s
			LEFT JOIN script_scenes sc ON sc.script_version_id = s.current_version_id
			WHERE s.project_id = $1 AND s.id = $2
		`, projectID, activeScriptID).Scan(&scriptSceneCount, &approvedScriptSceneCount, &pendingScriptSceneCount, &staleScriptSceneCount); err != nil {
			return ProductionSourceStage{}, err
		}
	}
	activePlanID, activePlanTitle, activePlanStatus, err := s.activeProductionAdaptationPlan(r, projectID, "")
	if err != nil {
		return ProductionSourceStage{}, err
	}
	pendingEventReviewCount := maxInt(eventCount-approvedEventCount, 0)
	status := "not_started"
	switch {
	case activeScriptID != "" && scriptSceneCount == 0:
		status = "scenes_pending_parse"
	case activeScriptID != "" && pendingScriptSceneCount > 0:
		status = "scenes_pending_review"
	case activeScriptID != "":
		status = "scenes_ready"
	case novelCount+scriptSourceCount == 0:
		status = "not_started"
	case novelCount > 0 && chapterCount > 0 && eventCount == 0:
		status = "events_pending_extraction"
	case novelCount > 0 && eventCount > 0 && pendingEventReviewCount > 0:
		status = "events_pending_review"
	case novelCount > 0 && adaptationPlanCount == 0:
		status = "adaptation_plan_pending"
	default:
		status = "imported"
	}
	summary := make([]string, 0, 2)
	if novelCount > 0 {
		summary = append(summary, "Novel source imported")
	}
	if scriptSourceCount > 0 {
		summary = append(summary, "Script source imported")
	}
	if chapterCount > 0 {
		summary = append(summary, fmt.Sprintf("Chapters: %d", chapterCount))
	}
	if eventCount > 0 {
		summary = append(summary, fmt.Sprintf("Events: %d, approved: %d", eventCount, approvedEventCount))
	}
	if activePlanTitle != "" {
		summary = append(summary, "Active adaptation plan: "+activePlanTitle)
	}
	if activeScriptTitle != "" {
		summary = append(summary, "Active script: "+activeScriptTitle)
	}
	if scriptSceneCount > 0 {
		summary = append(summary, fmt.Sprintf("Script scenes: %d, approved: %d", scriptSceneCount, approvedScriptSceneCount))
	}
	return ProductionSourceStage{
		Status:                   status,
		NovelSourceCount:         novelCount,
		ScriptSourceCount:        scriptSourceCount,
		ChapterCount:             chapterCount,
		EventCount:               eventCount,
		ApprovedEventCount:       approvedEventCount,
		PendingEventReviewCount:  pendingEventReviewCount,
		AdaptationPlanCount:      adaptationPlanCount,
		ActiveAdaptationPlanID:   stringPtrFromValue(activePlanID),
		ActiveAdaptationTitle:    stringPtrFromValue(activePlanTitle),
		ActiveAdaptationStatus:   stringPtrFromValue(activePlanStatus),
		ActiveScriptID:           stringPtrFromValue(activeScriptID),
		ActiveScriptTitle:        stringPtrFromValue(activeScriptTitle),
		ScriptSceneCount:         scriptSceneCount,
		ApprovedScriptSceneCount: approvedScriptSceneCount,
		PendingScriptSceneCount:  pendingScriptSceneCount,
		StaleScriptSceneCount:    staleScriptSceneCount,
		Summary:                  summary,
	}, nil
}

func (s *Server) productionAssetsStage(r *http.Request, projectID string) (ProductionAssetsStage, error) {
	var characterCount, sceneCount, propCount, referenceCount, runningCount, failedCount, approvedCount, pendingReviewCount, manualOverrideCount, staleCount, downstreamStaleCount int
	if err := s.db.QueryRow(r.Context(), `
		SELECT
			COUNT(*) FILTER (WHERE asset_type = 'character'),
			COUNT(*) FILTER (WHERE asset_type = 'scene'),
			COUNT(*) FILTER (WHERE asset_type = 'prop'),
			COUNT(*) FILTER (WHERE reference_artifact_id IS NOT NULL OR reference_media_file_id IS NOT NULL OR COALESCE(reference_storage_key, '') <> ''),
			COUNT(*) FILTER (WHERE status = 'image_running'),
			COUNT(*) FILTER (WHERE status = 'image_failed'),
			COUNT(*) FILTER (WHERE review_status = 'approved'),
			COUNT(*) FILTER (WHERE review_status <> 'approved'),
			COUNT(*) FILTER (WHERE manual_override),
			COUNT(*) FILTER (WHERE stale_state <> 'fresh'),
			(
				SELECT COUNT(*)
				FROM shot_asset_requirements r
				WHERE r.project_id = $1
				  AND r.stale_state <> 'fresh'
			)
		FROM canonical_assets
		WHERE project_id = $1
	`, projectID).Scan(&characterCount, &sceneCount, &propCount, &referenceCount, &runningCount, &failedCount, &approvedCount, &pendingReviewCount, &manualOverrideCount, &staleCount, &downstreamStaleCount); err != nil {
		return ProductionAssetsStage{}, err
	}
	total := characterCount + sceneCount + propCount
	missingReferenceCount := maxInt(total-referenceCount, 0)
	status := "not_started"
	switch {
	case total == 0:
		status = "not_started"
	case runningCount > 0:
		status = "running"
	case failedCount > 0:
		status = "failed"
	case pendingReviewCount > 0:
		status = "needs_review"
	case staleCount+downstreamStaleCount > 0:
		status = "needs_regeneration"
	case missingReferenceCount > 0:
		status = "ready"
	default:
		status = "completed"
	}
	summary, err := s.productionAssetSummary(r, projectID)
	if err != nil {
		return ProductionAssetsStage{}, err
	}
	return ProductionAssetsStage{
		Status:                     status,
		CharacterCount:             characterCount,
		SceneCount:                 sceneCount,
		PropCount:                  propCount,
		ReferenceImageCount:        referenceCount,
		MissingReferenceImageCount: missingReferenceCount,
		ApprovedCount:              approvedCount,
		PendingReviewCount:         pendingReviewCount,
		ManualOverrideCount:        manualOverrideCount,
		StaleCount:                 staleCount,
		DownstreamStaleCount:       downstreamStaleCount,
		Summary:                    summary,
	}, nil
}

func (s *Server) productionStoryboardStage(r *http.Request, projectID string, workflowsByType map[string]productionWorkflowState) (ProductionStoryboardStage, error) {
	var shotCount, confirmedCount, pendingReviewCount, manualOverrideCount, staleShotCount int
	if err := s.db.QueryRow(r.Context(), `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE review_status = 'approved'),
			COUNT(*) FILTER (WHERE review_status <> 'approved'),
			COUNT(*) FILTER (WHERE manual_override),
			COUNT(*) FILTER (WHERE stale_state <> 'fresh')
		FROM storyboard_shots
		WHERE project_id = $1
	`, projectID).Scan(&shotCount, &confirmedCount, &pendingReviewCount, &manualOverrideCount, &staleShotCount); err != nil {
		return ProductionStoryboardStage{}, err
	}
	state := mergedWorkflowState(workflowsByType, "script_to_storyboard", "script_to_video", "full_production")
	status := "not_started"
	switch {
	case state.Running:
		status = "running"
	case shotCount == 0 && state.Failed:
		status = "failed"
	case shotCount == 0:
		status = "not_started"
	case pendingReviewCount > 0:
		status = "needs_review"
	case staleShotCount > 0:
		status = "needs_regeneration"
	default:
		status = "ready"
	}
	summary, err := s.productionShotSummary(r, projectID)
	if err != nil {
		return ProductionStoryboardStage{}, err
	}
	return ProductionStoryboardStage{
		Status:              status,
		ShotCount:           shotCount,
		ConfirmedShotCount:  confirmedCount,
		PendingReviewCount:  pendingReviewCount,
		ManualOverrideCount: manualOverrideCount,
		StaleShotCount:      staleShotCount,
		Summary:             summary,
	}, nil
}

func (s *Server) productionShotAssetsStage(r *http.Request, projectID string) (ProductionShotAssetsStage, error) {
	var total, characterCount, sceneCount, propCount, derivedCount, runningCount, failedCount, approvedCount, pendingReviewCount, manualOverrideCount, staleRequirementCount int
	if err := s.db.QueryRow(r.Context(), `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE a.asset_type = 'character'),
			COUNT(*) FILTER (WHERE a.asset_type = 'scene'),
			COUNT(*) FILTER (WHERE a.asset_type = 'prop'),
			COUNT(*) FILTER (WHERE r.derived_artifact_id IS NOT NULL OR r.derived_media_file_id IS NOT NULL OR COALESCE(r.derived_storage_key, '') <> ''),
			COUNT(*) FILTER (WHERE r.status = 'image_running'),
			COUNT(*) FILTER (WHERE r.status = 'image_failed'),
			COUNT(*) FILTER (WHERE r.review_status = 'approved'),
			COUNT(*) FILTER (WHERE r.review_status <> 'approved'),
			COUNT(*) FILTER (WHERE r.manual_override),
			COUNT(*) FILTER (WHERE r.stale_state <> 'fresh')
		FROM shot_asset_requirements r
		LEFT JOIN canonical_assets a ON a.id = r.asset_id
		WHERE r.project_id = $1
	`, projectID).Scan(&total, &characterCount, &sceneCount, &propCount, &derivedCount, &runningCount, &failedCount, &approvedCount, &pendingReviewCount, &manualOverrideCount, &staleRequirementCount); err != nil {
		return ProductionShotAssetsStage{}, err
	}
	missing := maxInt(total-derivedCount, 0)
	status := "not_started"
	switch {
	case total == 0:
		status = "not_started"
	case runningCount > 0:
		status = "running"
	case failedCount > 0:
		status = "failed"
	case pendingReviewCount > 0:
		status = "needs_review"
	case staleRequirementCount > 0:
		status = "needs_regeneration"
	case missing > 0:
		status = "ready"
	default:
		status = "completed"
	}
	summary, err := s.productionRequirementSummary(r, projectID)
	if err != nil {
		return ProductionShotAssetsStage{}, err
	}
	return ProductionShotAssetsStage{
		Status:                    status,
		RequirementCount:          total,
		CharacterRequirementCount: characterCount,
		SceneRequirementCount:     sceneCount,
		PropRequirementCount:      propCount,
		DerivedImageCount:         derivedCount,
		MissingDerivedImageCount:  missing,
		ApprovedCount:             approvedCount,
		PendingReviewCount:        pendingReviewCount,
		ManualOverrideCount:       manualOverrideCount,
		StaleRequirementCount:     staleRequirementCount,
		Summary:                   summary,
	}, nil
}

func (s *Server) productionShotMediaStage(r *http.Request, projectID, mediaKind string) (ProductionShotMediaStage, error) {
	var total, succeeded, failed, running, stale int
	var err error
	if mediaKind == "image" {
		err = s.db.QueryRow(r.Context(), `
			SELECT
				COUNT(*),
				COUNT(*) FILTER (WHERE image_artifact_id IS NOT NULL OR image_media_file_id IS NOT NULL OR COALESCE(image_storage_key, '') <> '' OR status IN ('image_succeeded', 'video_running', 'video_succeeded')),
				COUNT(*) FILTER (WHERE status = 'image_failed'),
				COUNT(*) FILTER (WHERE status = 'image_running'),
				COUNT(*) FILTER (WHERE stale_state = 'needs_regeneration')
			FROM storyboard_shots
			WHERE project_id = $1
		`, projectID).Scan(&total, &succeeded, &failed, &running, &stale)
	} else {
		err = s.db.QueryRow(r.Context(), `
			SELECT
				COUNT(*),
				COUNT(*) FILTER (WHERE video_artifact_id IS NOT NULL OR video_media_file_id IS NOT NULL OR COALESCE(video_storage_key, '') <> '' OR status = 'video_succeeded'),
				COUNT(*) FILTER (WHERE status = 'video_failed'),
				COUNT(*) FILTER (WHERE status = 'video_running'),
				COUNT(*) FILTER (WHERE stale_state = 'needs_regeneration')
			FROM storyboard_shots
			WHERE project_id = $1
		`, projectID).Scan(&total, &succeeded, &failed, &running, &stale)
	}
	if err != nil {
		return ProductionShotMediaStage{}, err
	}
	pending := maxInt(total-succeeded-failed-running, 0)
	status := productionMediaStatus(total, succeeded, failed, running)
	if stale > 0 && status != "running" && status != "failed" {
		status = "needs_regeneration"
	}
	return ProductionShotMediaStage{
		Status:    status,
		Total:     total,
		Succeeded: succeeded,
		Failed:    failed,
		Running:   running,
		Pending:   pending,
		Stale:     stale,
	}, nil
}

func (s *Server) productionFinalVideoStage(r *http.Request, projectID string, workflowsByType map[string]productionWorkflowState) (ProductionFinalVideoStage, error) {
	var artifactID, mediaFileID, storageKey, mimeType, workflowRunID, sourceWorkflowRunID, staleState sql.NullString
	err := s.db.QueryRow(r.Context(), `
		SELECT a.id, mf.id, a.storage_key, a.mime_type, a.workflow_run_id::text,
		       a.metadata->>'sourceWorkflowRunId',
		       COALESCE(a.metadata->>'staleState', 'fresh')
		FROM artifacts a
		LEFT JOIN media_files mf ON mf.artifact_id = a.id
		WHERE a.project_id = $1 AND a.type = 'final_video'
		ORDER BY a.created_at DESC
		LIMIT 1
	`, projectID).Scan(&artifactID, &mediaFileID, &storageKey, &mimeType, &workflowRunID, &sourceWorkflowRunID, &staleState)
	if err != nil && err != pgx.ErrNoRows {
		return ProductionFinalVideoStage{}, err
	}
	var staleShotCount int
	if err := s.db.QueryRow(r.Context(), `
		SELECT COUNT(*)
		FROM storyboard_shots
		WHERE project_id = $1 AND stale_state <> 'fresh'
	`, projectID).Scan(&staleShotCount); err != nil {
		return ProductionFinalVideoStage{}, err
	}
	if artifactID.Valid {
		stale := staleShotCount > 0 || staleState.String == "needs_regeneration"
		status := "ready"
		if stale {
			status = "needs_regeneration"
		}
		stage := ProductionFinalVideoStage{
			Status:              status,
			ArtifactID:          stringPtrFromNull(artifactID),
			MediaFileID:         stringPtrFromNull(mediaFileID),
			StorageKey:          stringPtrFromNull(storageKey),
			WorkflowRunID:       stringPtrFromNull(workflowRunID),
			SourceWorkflowRunID: stringPtrFromNull(sourceWorkflowRunID),
			Stale:               stale,
		}
		if s.storage != nil && storageKey.Valid && mimeType.Valid && canPreviewMimeType(mimeType.String) {
			if presigned, err := s.storage.PresignGetObject(r.Context(), storageKey.String, 15*time.Minute); err == nil {
				stage.PreviewURL = &presigned.URL
			}
		}
		return stage, nil
	}
	state := mergedWorkflowState(workflowsByType, "script_to_video", "full_production", "video_production", "regenerate_final_video")
	status := "not_started"
	if state.Running {
		status = "running"
	} else if state.Failed {
		status = "failed"
	}
	return ProductionFinalVideoStage{Status: status}, nil
}

func (s *Server) loadProductionWorkflowState(r *http.Request, projectID string) (map[string]productionWorkflowState, error) {
	rows, err := s.db.Query(r.Context(), `
		SELECT id, status, input
		FROM workflow_runs
		WHERE project_id = $1
		ORDER BY created_at DESC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]productionWorkflowState{}
	for rows.Next() {
		var id, status string
		var input json.RawMessage
		if err := rows.Scan(&id, &status, &input); err != nil {
			return nil, err
		}
		workflowType := workflowTypeFromInput(input)
		if workflowType == "" {
			continue
		}
		state := out[workflowType]
		if state.LatestID == "" {
			state.LatestID = id
			state.LatestStatus = status
		}
		if isRunningWorkflowStatus(status) {
			state.Running = true
		}
		if status == "failed" {
			state.Failed = true
		}
		out[workflowType] = state
	}
	return out, rows.Err()
}

func (s *Server) productionAssetSummary(r *http.Request, projectID string) (map[string][]string, error) {
	rows, err := s.db.Query(r.Context(), `
		SELECT asset_type, name
		FROM canonical_assets
		WHERE project_id = $1
		ORDER BY asset_type, name
		LIMIT 18
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{"character": {}, "scene": {}, "prop": {}}
	for rows.Next() {
		var assetType, name string
		if err := rows.Scan(&assetType, &name); err != nil {
			return nil, err
		}
		out[assetType] = append(out[assetType], name)
	}
	return out, rows.Err()
}

func (s *Server) productionShotSummary(r *http.Request, projectID string) ([]string, error) {
	rows, err := s.db.Query(r.Context(), `
		SELECT COALESCE(shot_no, shot_index + 1), COALESCE(visual, title, '')
		FROM storyboard_shots
		WHERE project_id = $1
		ORDER BY created_at DESC, shot_index ASC
		LIMIT 5
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var shotNo int
		var visual string
		if err := rows.Scan(&shotNo, &visual); err != nil {
			return nil, err
		}
		out = append(out, "Shot "+itoa(shotNo)+": "+firstNonEmpty(visual, "No visual description"))
	}
	return out, rows.Err()
}

func (s *Server) productionRequirementSummary(r *http.Request, projectID string) ([]string, error) {
	rows, err := s.db.Query(r.Context(), `
		SELECT COALESCE(a.name, ''), r.requirement_type, COALESCE(r.costume, r.pose, r.expression, r.action, r.scene_state, r.prop_state, r.prompt, '')
		FROM shot_asset_requirements r
		LEFT JOIN canonical_assets a ON a.id = r.asset_id
		WHERE r.project_id = $1
		ORDER BY r.created_at DESC
		LIMIT 5
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name, requirementType, detail string
		if err := rows.Scan(&name, &requirementType, &detail); err != nil {
			return nil, err
		}
		out = append(out, strings.TrimSpace(firstNonEmpty(name, "Asset")+" - "+firstNonEmpty(requirementType, "requirement")+" - "+detail))
	}
	return out, rows.Err()
}

func (s *Server) productionActionWorkflow(w http.ResponseWriter, r *http.Request, project Project, action string, req ProductionActionRequest) (string, map[string]any, any, string, bool) {
	options := req.Options
	maxShots := productionOptionInt(options, "maxShots", 3)
	if maxShots <= 0 {
		maxShots = 3
	}
	if maxShots > 3 {
		maxShots = 3
	}
	switch action {
	case "extract_events":
		sourceID, err := s.activeProductionSourceID(r, project.ID, firstNonEmpty(req.SourceID, productionOptionString(options, "sourceId")))
		if err != nil {
			s.writeError(w, r, err)
			return "", nil, nil, "", false
		}
		if sourceID == "" {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "sourceId is required when the project has no source", nil, false)
			return "", nil, nil, "", false
		}
		return "extract_novel_events", map[string]any{
			"sourceId": sourceID,
			"force":    productionOptionBool(options, "force", false),
		}, workflows.ExtractNovelEventsWorkflow, "", true
	case "generate_adaptation_plan":
		sourceID, err := s.activeProductionSourceID(r, project.ID, firstNonEmpty(req.SourceID, productionOptionString(options, "sourceId")))
		if err != nil {
			s.writeError(w, r, err)
			return "", nil, nil, "", false
		}
		if sourceID == "" {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "sourceId is required when the project has no source", nil, false)
			return "", nil, nil, "", false
		}
		return "generate_adaptation_plan", map[string]any{
			"sourceId":              sourceID,
			"eventIds":              productionOptionStringSlice(options, "eventIds"),
			"targetFormat":          firstNonEmpty(productionOptionString(options, "targetFormat"), "short_video"),
			"targetDurationSeconds": productionOptionInt(options, "targetDurationSeconds", 0),
			"maxShots":              productionOptionInt(options, "maxShots", 0),
			"instruction":           productionOptionString(options, "instruction"),
		}, workflows.GenerateAdaptationPlanWorkflow, "", true
	case "generate_script_from_plan":
		planID, _, _, err := s.activeProductionAdaptationPlan(r, project.ID, productionOptionString(options, "planId"))
		if err != nil {
			s.writeError(w, r, err)
			return "", nil, nil, "", false
		}
		if planID == "" {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "planId is required when the project has no adaptation plan", nil, false)
			return "", nil, nil, "", false
		}
		return "adaptation_plan_to_script", map[string]any{
			"planId":      planID,
			"title":       productionOptionString(options, "title"),
			"instruction": productionOptionString(options, "instruction"),
		}, workflows.AdaptationPlanToScriptWorkflow, "", true
	case "generate_script":
		sourceID, err := s.activeProductionSourceID(r, project.ID, firstNonEmpty(req.SourceID, productionOptionString(options, "sourceId")))
		if err != nil {
			s.writeError(w, r, err)
			return "", nil, nil, "", false
		}
		if sourceID == "" {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "sourceId is required when the project has no source", nil, false)
			return "", nil, nil, "", false
		}
		return "source_to_script", map[string]any{"sourceId": sourceID}, workflows.SourceToScriptWorkflow, "", true
	case "parse_script_scenes":
		scriptID, ok := s.requireProductionScript(w, r, project.ID, req.ScriptID)
		if !ok {
			return "", nil, nil, "", false
		}
		input := map[string]any{"scriptId": scriptID, "force": productionOptionBool(options, "force", false)}
		if versionID := productionOptionString(options, "scriptVersionId"); versionID != "" {
			input["scriptVersionId"] = versionID
		}
		return "parse_script_scenes", input, workflows.ParseScriptScenesWorkflow, "", true
	case "analyze_assets":
		scriptID, ok := s.requireProductionScript(w, r, project.ID, req.ScriptID)
		if !ok {
			return "", nil, nil, "", false
		}
		return "script_to_assets", map[string]any{"scriptId": scriptID, "mergeExisting": true, "generateImages": false}, workflows.ScriptToAssetsWorkflow, "", true
	case "generate_asset_images":
		scriptID, ok := s.requireProductionScript(w, r, project.ID, req.ScriptID)
		if !ok {
			return "", nil, nil, "", false
		}
		return "script_to_assets", map[string]any{"scriptId": scriptID, "mergeExisting": true, "generateImages": true}, workflows.ScriptToAssetsWorkflow, "This reuses script_to_assets with generateImages=true for missing canonical references.", true
	case "generate_storyboard":
		scriptID, ok := s.requireProductionScript(w, r, project.ID, req.ScriptID)
		if !ok {
			return "", nil, nil, "", false
		}
		return "script_to_storyboard", map[string]any{"scriptId": scriptID, "maxShots": maxShots, "generateDerivedAssets": false}, workflows.ScriptToStoryboardWorkflow, "", true
	case "analyze_shot_assets":
		scriptID, ok := s.requireProductionScript(w, r, project.ID, req.ScriptID)
		if !ok {
			return "", nil, nil, "", false
		}
		return "script_to_storyboard", map[string]any{"scriptId": scriptID, "maxShots": maxShots, "generateDerivedAssets": true}, workflows.ScriptToStoryboardWorkflow, "This reuses script_to_storyboard with derived asset analysis enabled.", true
	case "generate_derived_asset_images":
		scriptID, ok := s.requireProductionScript(w, r, project.ID, req.ScriptID)
		if !ok {
			return "", nil, nil, "", false
		}
		return "script_to_storyboard", map[string]any{"scriptId": scriptID, "maxShots": maxShots, "generateDerivedAssets": true}, workflows.ScriptToStoryboardWorkflow, "This reuses script_to_storyboard because derived image generation is currently part of that workflow.", true
	case "generate_shot_images":
		scriptID, ok := s.requireProductionScript(w, r, project.ID, req.ScriptID)
		if !ok {
			return "", nil, nil, "", false
		}
		return "script_to_video", scriptVideoInput(scriptID, maxShots, true), workflows.VideoProductionWorkflow, "This reuses script_to_video and skips final composition.", true
	case "generate_shot_videos":
		scriptID, ok := s.requireProductionScript(w, r, project.ID, req.ScriptID)
		if !ok {
			return "", nil, nil, "", false
		}
		return "script_to_video", scriptVideoInput(scriptID, maxShots, true), workflows.VideoProductionWorkflow, "", true
	case "compose_final_video":
		scriptID, ok := s.requireProductionScript(w, r, project.ID, req.ScriptID)
		if !ok {
			return "", nil, nil, "", false
		}
		return "script_to_video", scriptVideoInput(scriptID, maxShots, false), workflows.VideoProductionWorkflow, "This reuses script_to_video until a standalone compose workflow exists.", true
	case "run_full_production":
		scriptID, _, err := s.activeProductionScript(r, project.ID, req.ScriptID)
		if err != nil {
			s.writeError(w, r, err)
			return "", nil, nil, "", false
		}
		if scriptID == "" {
			return "video_production", map[string]any{
				"duration":            productionOptionInt(options, "duration", 5),
				"aspectRatio":         firstNonEmpty(productionOptionString(options, "aspectRatio"), project.VideoRatio, stringValue(project.AspectRatio), "16:9"),
				"resolution":          firstNonEmpty(productionOptionString(options, "resolution"), "720p"),
				"pollIntervalSeconds": productionOptionInt(options, "pollIntervalSeconds", 5),
				"maxPolls":            productionOptionInt(options, "maxPolls", 120),
				"maxShots":            maxShots,
				"skipCompose":         productionOptionBool(options, "skipCompose", false),
			}, workflows.VideoProductionWorkflow, "No active script was found, so this keeps the existing prompt-only video_production path.", true
		}
		return "full_production", scriptVideoInput(scriptID, maxShots, false), workflows.VideoProductionWorkflow, "", true
	default:
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "production action is not supported", nil, false)
		return "", nil, nil, "", false
	}
}

func (s *Server) requireProductionScript(w http.ResponseWriter, r *http.Request, projectID, explicitScriptID string) (string, bool) {
	scriptID, _, err := s.activeProductionScript(r, projectID, explicitScriptID)
	if err != nil {
		s.writeError(w, r, err)
		return "", false
	}
	if scriptID == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "scriptId is required when the project has no active script", nil, false)
		return "", false
	}
	return scriptID, true
}

func (s *Server) activeProductionScript(r *http.Request, projectID, explicitScriptID string) (string, string, error) {
	explicitScriptID = strings.TrimSpace(explicitScriptID)
	var id, title string
	if explicitScriptID != "" {
		err := s.db.QueryRow(r.Context(), `
			SELECT id, title
			FROM scripts
			WHERE project_id = $1 AND id = $2
		`, projectID, explicitScriptID).Scan(&id, &title)
		return id, title, err
	}
	err := s.db.QueryRow(r.Context(), `
		SELECT id, title
		FROM scripts
		WHERE project_id = $1 AND current_version_id IS NOT NULL
		ORDER BY CASE WHEN status = 'active' THEN 0 ELSE 1 END, updated_at DESC, created_at DESC
		LIMIT 1
	`, projectID).Scan(&id, &title)
	if err == pgx.ErrNoRows {
		return "", "", nil
	}
	return id, title, err
}

func (s *Server) activeProductionSourceID(r *http.Request, projectID, explicitSourceID string) (string, error) {
	explicitSourceID = strings.TrimSpace(explicitSourceID)
	var id string
	if explicitSourceID != "" {
		err := s.db.QueryRow(r.Context(), `
			SELECT id
			FROM project_sources
			WHERE project_id = $1 AND id = $2
		`, projectID, explicitSourceID).Scan(&id)
		return id, err
	}
	err := s.db.QueryRow(r.Context(), `
		SELECT id
		FROM project_sources
		WHERE project_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, projectID).Scan(&id)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return id, err
}

func (s *Server) activeProductionAdaptationPlan(r *http.Request, projectID, explicitPlanID string) (string, string, string, error) {
	explicitPlanID = strings.TrimSpace(explicitPlanID)
	var id, title, status string
	if explicitPlanID != "" {
		err := s.db.QueryRow(r.Context(), `
			SELECT id, title, status
			FROM adaptation_plans
			WHERE project_id = $1 AND id = $2
		`, projectID, explicitPlanID).Scan(&id, &title, &status)
		return id, title, status, err
	}
	err := s.db.QueryRow(r.Context(), `
		SELECT id, title, status
		FROM adaptation_plans
		WHERE project_id = $1
		ORDER BY CASE WHEN status = 'active' THEN 0 ELSE 1 END, updated_at DESC, created_at DESC
		LIMIT 1
	`, projectID).Scan(&id, &title, &status)
	if err == pgx.ErrNoRows {
		return "", "", "", nil
	}
	return id, title, status, err
}

func (s *Server) updateReviewStatus(w http.ResponseWriter, r *http.Request, tableName, projectID, id, userID string) (ReviewResponse, bool) {
	var req ReviewRequest
	if !decode(w, r, &req) {
		return ReviewResponse{}, false
	}
	status := strings.TrimSpace(req.ReviewStatus)
	if !validReviewStatus(status) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "reviewStatus is invalid", nil, false)
		return ReviewResponse{}, false
	}
	note := strings.TrimSpace(req.Note)
	var resp ReviewResponse
	query := `
		UPDATE ` + tableName + `
		SET review_status = $3,
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'reviewStatus', $3,
		      'reviewNote', $4,
		      'reviewedBy', $5,
		      'reviewedAt', now()
		    )
		WHERE project_id = $1 AND id = $2
		RETURNING id, review_status, updated_at
	`
	if err := s.db.QueryRow(r.Context(), query, projectID, id, status, note, userID).Scan(&resp.ID, &resp.ReviewStatus, &resp.UpdatedAt); err != nil {
		s.writeError(w, r, err)
		return ReviewResponse{}, false
	}
	resp.Note = stringPtrFromValue(note)
	return resp, true
}

func productionActionPermission(action string) (string, bool) {
	switch action {
	case "extract_events":
		return authz.PermissionNovelEventWrite, true
	case "generate_adaptation_plan", "generate_script_from_plan":
		return authz.PermissionAdaptationPlanWrite, true
	case "generate_script":
		return authz.PermissionScriptWrite, true
	case "parse_script_scenes":
		return authz.PermissionScriptWrite, true
	case "analyze_assets":
		return authz.PermissionAssetAnalyze, true
	case "generate_asset_images", "generate_derived_asset_images":
		return authz.PermissionAssetGenerate, true
	case "generate_storyboard", "analyze_shot_assets":
		return authz.PermissionStoryboardGenerate, true
	case "generate_shot_images", "generate_shot_videos", "compose_final_video", "run_full_production":
		return authz.PermissionWorkflowRun, true
	default:
		return "", false
	}
}

func productionOverall(stages ProductionStages) ProductionOverall {
	stageStatuses := []string{
		stages.Source.Status,
		stages.Assets.Status,
		stages.Storyboard.Status,
		stages.ShotAssets.Status,
		stages.ShotImages.Status,
		stages.ShotVideos.Status,
		stages.FinalVideo.Status,
	}
	progress := 0
	for _, status := range stageStatuses {
		progress += productionStageProgress(status)
	}
	progress = progress / len(stageStatuses)
	overallStatus := "pending"
	if hasStatus(stageStatuses, "running") {
		overallStatus = "running"
	} else if hasStatus(stageStatuses, "failed") {
		overallStatus = "failed"
	} else if hasStatus(stageStatuses, "needs_review") {
		overallStatus = "needs_review"
	} else if stages.FinalVideo.Status == "ready" {
		overallStatus = "ready"
	}
	return ProductionOverall{
		Stage:    productionCurrentStage(stages),
		Progress: progress,
		Status:   overallStatus,
	}
}

func productionCurrentStage(stages ProductionStages) string {
	if !sourceStageReady(stages.Source.Status) {
		return "source"
	}
	if stages.Assets.Status != "completed" {
		return "assets"
	}
	if stages.Storyboard.Status != "ready" {
		return "storyboard"
	}
	if stages.ShotAssets.Status != "completed" {
		return "shot_assets"
	}
	if stages.ShotImages.Status != "ready" {
		return "shot_images"
	}
	if stages.ShotVideos.Status != "ready" {
		return "shot_videos"
	}
	return "final_video"
}

func productionStageProgress(status string) int {
	switch status {
	case "ready", "completed":
		return 100
	case "scenes_ready":
		return 100
	case "scenes_pending_review":
		return 75
	case "scenes_pending_parse":
		return 65
	case "events_pending_review":
		return 55
	case "adaptation_plan_pending":
		return 60
	case "needs_review":
		return 70
	case "partial":
		return 65
	case "running":
		return 50
	case "failed":
		return 40
	case "imported":
		return 35
	case "events_pending_extraction":
		return 45
	default:
		return 0
	}
}

func sourceStageReady(status string) bool {
	return status == "ready" || status == "scenes_ready"
}

func productionMediaStatus(total, succeeded, failed, running int) string {
	switch {
	case total == 0:
		return "not_started"
	case running > 0:
		return "running"
	case failed > 0 && succeeded == 0:
		return "failed"
	case failed > 0 || (succeeded > 0 && succeeded < total):
		return "partial"
	case succeeded >= total:
		return "ready"
	default:
		return "not_started"
	}
}

func mergedWorkflowState(states map[string]productionWorkflowState, workflowTypes ...string) productionWorkflowState {
	out := productionWorkflowState{}
	for _, workflowType := range workflowTypes {
		state := states[workflowType]
		if out.LatestID == "" && state.LatestID != "" {
			out.LatestID = state.LatestID
			out.LatestStatus = state.LatestStatus
		}
		out.Running = out.Running || state.Running
		out.Failed = out.Failed || state.Failed
	}
	return out
}

func workflowTypeFromInput(raw json.RawMessage) string {
	var value struct {
		WorkflowType string `json:"workflowType"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return strings.TrimSpace(value.WorkflowType)
}

func scriptVideoInput(scriptID string, maxShots int, skipCompose bool) map[string]any {
	return map[string]any{
		"scriptId":              scriptID,
		"maxShots":              maxShots,
		"generateImages":        true,
		"generateDerivedAssets": true,
		"skipCompose":           skipCompose,
	}
}

func productionOptionString(options map[string]any, key string) string {
	if value, ok := options[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func productionOptionInt(options map[string]any, key string, fallback int) int {
	switch value := options[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		if parsed, err := value.Int64(); err == nil {
			return int(parsed)
		}
	}
	return fallback
}

func productionOptionBool(options map[string]any, key string, fallback bool) bool {
	if value, ok := options[key].(bool); ok {
		return value
	}
	return fallback
}

func productionOptionStringSlice(options map[string]any, key string) []string {
	raw, ok := options[key]
	if !ok {
		return nil
	}
	switch values := raw.(type) {
	case []string:
		return values
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, strings.TrimSpace(text))
			}
		}
		return out
	default:
		return nil
	}
}

func validReviewStatus(value string) bool {
	return value == "pending" || value == "approved" || value == "rejected" || value == "needs_edit"
}

func hasStatus(statuses []string, target string) bool {
	for _, status := range statuses {
		if status == target {
			return true
		}
	}
	return false
}

func isRunningWorkflowStatus(status string) bool {
	return status == "queued" || status == "running" || status == "cancelling"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func maxInt(value, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var digits [20]byte
	i := len(digits)
	for value > 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
