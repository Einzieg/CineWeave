package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const commerceFinalComposeHandler = "commerce_final_compose"

type CommerceTimeline struct {
	ID                     string          `json:"id"`
	OrganizationID         string          `json:"organizationId"`
	ProjectID              string          `json:"projectId"`
	ProductionGenerationID string          `json:"projectGenerationId"`
	ScriptUnitID           string          `json:"scriptUnitId"`
	UnitGenerationID       string          `json:"unitGenerationId"`
	WorkflowRunID          *string         `json:"workflowRunId,omitempty"`
	Revision               int64           `json:"revision"`
	Title                  string          `json:"title"`
	Status                 string          `json:"status"`
	AspectRatio            string          `json:"aspectRatio"`
	Resolution             string          `json:"resolution"`
	TimelineTimebase       int64           `json:"timelineTimebase"`
	FPSNumerator           int             `json:"fpsNumerator"`
	FPSDenominator         int             `json:"fpsDenominator"`
	Metadata               json.RawMessage `json:"metadata"`
	CreatedAt              time.Time       `json:"createdAt"`
	UpdatedAt              time.Time       `json:"updatedAt"`
}

type CommerceTimelineOverlay struct {
	ID               string          `json:"id"`
	TimelineID       string          `json:"timelineId"`
	TimelineClipID   *string         `json:"timelineClipId,omitempty"`
	StoryboardShotID *string         `json:"storyboardShotId,omitempty"`
	Role             string          `json:"role"`
	Ordinal          int             `json:"ordinal"`
	Text             string          `json:"text"`
	StartTick        int64           `json:"startTick"`
	EndTick          int64           `json:"endTick"`
	Style            json.RawMessage `json:"style"`
	ContentHash      string          `json:"contentHash"`
}

type CommerceTimelineDetail struct {
	Timeline           CommerceTimeline            `json:"timeline"`
	Clips              []TimelineClipDetail        `json:"clips"`
	Overlays           []CommerceTimelineOverlay   `json:"overlays"`
	FinalVideoVersions []CommerceFinalVideoVersion `json:"finalVideoVersions"`
}

type CommerceFinalVideoVersion struct {
	FinalVideoVersion
	ScriptUnitID     string `json:"scriptUnitId"`
	UnitGenerationID string `json:"unitGenerationId"`
}

type commerceTimelinePrepareRequest struct {
	StoryboardPlanID         string `json:"storyboardPlanId"`
	ExpectedPlanRevision     int64  `json:"expectedPlanRevision"`
	ExpectedUnitGenerationID string `json:"expectedUnitGenerationId"`
	Title                    string `json:"title"`
	Resolution               string `json:"resolution"`
}

type commerceTimelineOverlayRequest struct {
	TimelineClipID   string          `json:"timelineClipId"`
	StoryboardShotID string          `json:"storyboardShotId"`
	Role             string          `json:"role"`
	Ordinal          int             `json:"ordinal"`
	Text             string          `json:"text"`
	StartTick        int64           `json:"startTick"`
	EndTick          int64           `json:"endTick"`
	Style            json.RawMessage `json:"style"`
}

type commerceTimelineUpdateRequest struct {
	ExpectedRevision int64                             `json:"expectedRevision"`
	Title            *string                           `json:"title"`
	Overlays         *[]commerceTimelineOverlayRequest `json:"overlays"`
}

type commerceFinalComposeRequest struct {
	TimelineID               string `json:"timelineId"`
	ExpectedTimelineRevision int64  `json:"expectedTimelineRevision"`
	ExpectedUnitGenerationID string `json:"expectedUnitGenerationId"`
	Title                    string `json:"title"`
	Resolution               string `json:"resolution"`
}

type commerceFinalComposeSnapshot struct {
	TimelineID       string `json:"timelineId"`
	TimelineRevision int64  `json:"timelineRevision"`
	Title            string `json:"title"`
	Resolution       string `json:"resolution"`
	AspectRatio      string `json:"aspectRatio"`
	RetryOfRunID     string `json:"retryOfRunId,omitempty"`
}

type commerceTimelineSourceShot struct {
	ID                  string
	ShotIndex           int
	Title               string
	DurationTicks       int64
	VideoArtifactID     string
	VideoMediaFileID    string
	VideoStorageKey     string
	VideoStatus         string
	ProductionReadiness string
	OnscreenText        string
	SalesBeat           string
	ContractHash        string
}

func (s *Server) listCommerceTimelines(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	identity, err := s.activeCommerceTimelineIdentity(r.Context(), project, r.PathValue("scriptUnitId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	rows, err := s.db.Query(r.Context(), commerceTimelineSelectSQL+`
		WHERE timeline.organization_id = $1 AND timeline.project_id = $2
		  AND timeline.commerce_script_unit_id = $3
		  AND timeline.commerce_script_unit_generation_id = $4
		  AND timeline.status <> 'archived'
		ORDER BY CASE timeline.status WHEN 'active' THEN 0 ELSE 1 END, timeline.created_at DESC
	`, project.OrganizationID, project.ID, identity.ScriptUnitID, identity.UnitGenerationID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]CommerceTimeline, 0)
	for rows.Next() {
		item, scanErr := scanCommerceTimeline(rows)
		if scanErr != nil {
			s.writeError(w, r, scanErr)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items, "unitGenerationId": identity.UnitGenerationID}, nil)
}

func (s *Server) getCommerceTimeline(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	identity, err := s.activeCommerceTimelineIdentity(r.Context(), project, r.PathValue("scriptUnitId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	detail, err := s.commerceTimelineDetail(r, project, identity, r.PathValue("timelineId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, detail, nil)
}

func (s *Server) prepareCommerceTimeline(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	var req commerceTimelinePrepareRequest
	if !decode(w, r, &req) {
		return
	}
	req.StoryboardPlanID = strings.TrimSpace(req.StoryboardPlanID)
	req.ExpectedUnitGenerationID = strings.TrimSpace(req.ExpectedUnitGenerationID)
	req.Title = strings.TrimSpace(req.Title)
	req.Resolution = strings.ToLower(strings.TrimSpace(req.Resolution))
	if _, err := uuid.Parse(req.StoryboardPlanID); err != nil || req.ExpectedPlanRevision < 1 {
		s.writeError(w, r, commercepkg.Error{Code: commercepkg.CodeStoryboardRevision, Message: "分镜方案标识或版本无效"})
		return
	}
	if _, err := uuid.Parse(req.ExpectedUnitGenerationID); err != nil {
		s.writeError(w, r, commercepkg.Error{Code: commercepkg.CodeGenerationMismatch, Message: "脚本单元生产代标识无效"})
		return
	}
	if req.Resolution == "" {
		req.Resolution = "1080p"
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED", "准备成片时间线需要请求标识", nil, false)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	identity, err := s.commerceCatalog.LockActiveStoryboardGeneration(r.Context(), tx, project.OrganizationID, project.ID, r.PathValue("scriptUnitId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if identity.UnitGenerationID != req.ExpectedUnitGenerationID {
		s.writeError(w, r, commercepkg.Error{Code: commercepkg.CodeGenerationMismatch, Message: "脚本单元生产代已变化，请刷新后重试"})
		return
	}
	shots, aspectRatio, err := loadCommerceTimelineSourceShots(r.Context(), tx, identity, req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	sourceHash := idempotencyRequestHash(map[string]any{
		"contractVersion": "commerce-timeline-source/v1", "identity": identity,
		"storyboardPlanId": req.StoryboardPlanID, "planRevision": req.ExpectedPlanRevision,
		"resolution": req.Resolution, "aspectRatio": aspectRatio, "shots": shots,
	})
	scope := "commerce_timeline_prepare:" + identity.ScriptUnitID
	requestHash := idempotencyRequestHash(map[string]any{
		"sourceHash": sourceHash, "title": req.Title,
	})
	claim, err := claimIdempotencyTx(r.Context(), tx, project.OrganizationID, scope, idempotencyKey, requestHash)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if len(claim.replaySnapshot) > 0 {
		var replay CommerceTimeline
		if err := json.Unmarshal(claim.replaySnapshot, &replay); err != nil {
			s.writeError(w, r, err)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			s.writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, r, http.StatusOK, replay, map[string]any{"idempotentReplay": true})
		return
	}
	if existing, found, err := findCommerceTimelineBySourceHash(r.Context(), tx, identity, sourceHash); err != nil {
		s.writeError(w, r, err)
		return
	} else if found {
		if err := completeIdempotencyTxWithStatus(r.Context(), tx, claim.state, http.StatusOK, existing); err != nil {
			s.writeError(w, r, err)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			s.writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, r, http.StatusOK, existing, map[string]any{"sourceReplay": true})
		return
	}
	item, err := insertCommerceTimeline(r.Context(), tx, project, principal.UserID, identity, req, aspectRatio, sourceHash, shots)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := completeIdempotencyTxWithStatus(r.Context(), tx, claim.state, http.StatusCreated, item); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "commerce.timeline.updated", "project_timeline", item.ID, mustRawJSON(map[string]any{
		"commerceScriptUnitId": identity.ScriptUnitID, "scriptUnitGenerationId": identity.UnitGenerationID,
		"timelineId": item.ID, "revision": item.Revision, "status": item.Status,
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) updateCommerceTimeline(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	var req commerceTimelineUpdateRequest
	if !decode(w, r, &req) {
		return
	}
	if req.ExpectedRevision < 1 {
		s.writeError(w, r, commercepkg.Error{Code: commercepkg.CodeRevisionConflict, Message: "时间线版本无效"})
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	identity, err := s.commerceCatalog.LockActiveStoryboardGeneration(r.Context(), tx, project.OrganizationID, project.ID, r.PathValue("scriptUnitId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := scanCommerceTimeline(tx.QueryRow(r.Context(), commerceTimelineSelectSQL+`
		WHERE timeline.id = $1 AND timeline.organization_id = $2 AND timeline.project_id = $3
		  AND timeline.commerce_script_unit_id = $4
		  AND timeline.commerce_script_unit_generation_id = $5
		  AND timeline.revision = $6 AND timeline.status IN ('draft', 'active')
		FOR UPDATE
	`, r.PathValue("timelineId"), project.OrganizationID, project.ID, identity.ScriptUnitID,
		identity.UnitGenerationID, req.ExpectedRevision))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = commercepkg.Error{Code: commercepkg.CodeRevisionConflict, Message: "时间线已变化，请刷新后重试"}
		}
		s.writeError(w, r, err)
		return
	}
	if req.Overlays != nil {
		if err := replaceCommerceTimelineOverlays(r.Context(), tx, principal.UserID, identity, item.ID, *req.Overlays); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	title := item.Title
	if req.Title != nil {
		title = strings.TrimSpace(*req.Title)
		if title == "" {
			title = item.Title
		}
	}
	item, err = updateCommerceTimelineRow(r.Context(), tx, item.ID, title, principal.UserID, req.ExpectedRevision)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE final_video_versions
		SET status = CASE WHEN status = 'active' THEN 'ready' ELSE status END,
		    metadata = metadata || jsonb_build_object('staleState', 'upstream_changed', 'timelineRevision', $2)
		WHERE timeline_id = $1 AND commerce_script_unit_id = $3
	`, item.ID, item.Revision, identity.ScriptUnitID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "commerce.timeline.updated", "project_timeline", item.ID, mustRawJSON(map[string]any{
		"commerceScriptUnitId": identity.ScriptUnitID, "scriptUnitGenerationId": identity.UnitGenerationID,
		"timelineId": item.ID, "revision": item.Revision, "status": item.Status,
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) composeCommerceFinalVideo(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowRun)
	if !ok {
		return
	}
	if s.temporal == nil {
		s.writeError(w, r, apiError{Status: http.StatusServiceUnavailable, Code: "TEMPORAL_UNAVAILABLE", Message: "工作流服务暂不可用", Retryable: true})
		return
	}
	var req commerceFinalComposeRequest
	if !decode(w, r, &req) {
		return
	}
	if err := normalizeCommerceFinalComposeRequest(&req); err != nil {
		s.writeError(w, r, err)
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED", "成片合成需要请求标识", nil, false)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	scope := "commerce_final_compose:" + r.PathValue("scriptUnitId")
	requestHash := idempotencyRequestHash(map[string]any{"projectId": project.ID, "scriptUnitId": r.PathValue("scriptUnitId"), "request": req})
	claim, err := claimIdempotencyTx(r.Context(), tx, project.OrganizationID, scope, idempotencyKey, requestHash)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if len(claim.replaySnapshot) > 0 {
		var replay commercepkg.ProductionRun
		if err := json.Unmarshal(claim.replaySnapshot, &replay); err != nil {
			s.writeError(w, r, err)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			s.writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, r, http.StatusAccepted, replay, map[string]any{"idempotentReplay": true})
		return
	}
	run, created, err := s.createCommerceFinalComposeRunTx(r.Context(), tx, project, principal.UserID, r.PathValue("scriptUnitId"), req, scope, idempotencyKey, "")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := completeIdempotencyTxWithStatus(r.Context(), tx, claim.state, http.StatusAccepted, run); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, run, map[string]any{"created": created})
}

func (s *Server) listCommerceFinalVideos(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	identity, err := s.activeCommerceTimelineIdentity(r.Context(), project, r.PathValue("scriptUnitId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	items, err := s.commerceFinalVideoVersions(r, project.ID, identity.ScriptUnitID, identity.UnitGenerationID, "")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items, "unitGenerationId": identity.UnitGenerationID}, nil)
}

func (s *Server) getCommerceFinalVideo(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	identity, err := s.activeCommerceTimelineIdentity(r.Context(), project, r.PathValue("scriptUnitId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	items, err := s.commerceFinalVideoVersions(r, project.ID, identity.ScriptUnitID, identity.UnitGenerationID, r.PathValue("finalVideoVersionId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if len(items) == 0 {
		s.writeError(w, r, pgx.ErrNoRows)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, items[0], nil)
}

func (s *Server) activateCommerceFinalVideo(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	identity, err := s.commerceCatalog.LockActiveStoryboardGeneration(r.Context(), tx, project.OrganizationID, project.ID, r.PathValue("scriptUnitId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var readiness, staleState string
	if err := tx.QueryRow(r.Context(), `
		SELECT production_readiness, COALESCE(metadata->>'staleState', 'fresh')
		FROM final_video_versions
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND production_generation_id = $4
		  AND commerce_script_unit_id = $5
		  AND commerce_script_unit_generation_id = $6
		  AND status IN ('ready', 'active')
		FOR UPDATE
	`, r.PathValue("finalVideoVersionId"), project.OrganizationID, project.ID,
		identity.ProjectGenerationID, identity.ScriptUnitID, identity.UnitGenerationID).Scan(&readiness, &staleState); err != nil {
		s.writeError(w, r, err)
		return
	}
	if readiness != "ready" || staleState != "fresh" {
		s.writeError(w, r, commercepkg.Error{Code: commercepkg.CodeGenerationMismatch, Message: "该成片尚未通过生产校验或已过期，不能启用"})
		return
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE final_video_versions SET status = 'ready'
		WHERE project_id = $1 AND commerce_script_unit_id = $2 AND status = 'active' AND id <> $3
	`, project.ID, identity.ScriptUnitID, r.PathValue("finalVideoVersionId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE final_video_versions SET status = 'active' WHERE id = $1`, r.PathValue("finalVideoVersionId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "commerce.final_video.activated", "final_video_version", r.PathValue("finalVideoVersionId"), mustRawJSON(map[string]any{
		"commerceScriptUnitId": identity.ScriptUnitID, "scriptUnitGenerationId": identity.UnitGenerationID,
		"finalVideoVersionId": r.PathValue("finalVideoVersionId"), "status": "active",
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"activated": true}, nil)
}

func (s *Server) activeCommerceTimelineIdentity(ctx context.Context, project Project, scriptUnitID string) (commercepkg.UnitGenerationIdentity, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return commercepkg.UnitGenerationIdentity{}, err
	}
	defer tx.Rollback(ctx)
	identity, err := s.commerceCatalog.LockActiveStoryboardGeneration(ctx, tx, project.OrganizationID, project.ID, scriptUnitID)
	if err != nil {
		return commercepkg.UnitGenerationIdentity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return commercepkg.UnitGenerationIdentity{}, err
	}
	return identity, nil
}

func loadCommerceTimelineSourceShots(ctx context.Context, tx pgx.Tx, identity commercepkg.UnitGenerationIdentity, req commerceTimelinePrepareRequest) ([]commerceTimelineSourceShot, string, error) {
	var aspectRatio string
	var editRevision int64
	if err := tx.QueryRow(ctx, `
		SELECT aspect_ratio, edit_revision
		FROM commerce_storyboard_plans
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND script_unit_id = $4 AND script_unit_generation_id = $5
		  AND active AND status = 'ready' AND stale_state = 'fresh'
		FOR SHARE
	`, req.StoryboardPlanID, identity.OrganizationID, identity.ProjectID,
		identity.ScriptUnitID, identity.UnitGenerationID).Scan(&aspectRatio, &editRevision); err != nil {
		return nil, "", commercepkg.Error{Code: commercepkg.CodeStoryboardPlanStale, Message: "当前分镜方案未启用或已过期", Cause: err}
	}
	if editRevision != req.ExpectedPlanRevision {
		return nil, "", commercepkg.Error{Code: commercepkg.CodeStoryboardRevision, Message: "分镜方案已变化，请刷新后重试"}
	}
	rows, err := tx.Query(ctx, `
		SELECT shot.id::text, shot.shot_index, COALESCE(NULLIF(shot.title, ''), '镜头 ' || (shot.shot_index + 1)),
		       COALESCE(shot.planned_duration_ticks, shot.end_tick - shot.start_tick),
		       COALESCE(shot.video_artifact_id::text, ''), COALESCE(shot.video_media_file_id::text, ''),
		       COALESCE(shot.video_storage_key, media.storage_key, ''),
		       COALESCE(shot.video_status, ''), COALESCE(shot.production_readiness, 'ready'),
		       contract.onscreen_text, contract.sales_beat, contract.contract_hash
		FROM storyboard_shots shot
		JOIN commerce_shot_contracts contract
		  ON contract.storyboard_shot_id = shot.id
		 AND contract.commerce_storyboard_plan_id = shot.commerce_storyboard_plan_id
		 AND contract.organization_id = shot.organization_id
		 AND contract.project_id = shot.project_id
		LEFT JOIN media_files media ON media.id = shot.video_media_file_id
		WHERE shot.commerce_storyboard_plan_id = $1 AND shot.project_id = $2
		  AND shot.production_generation_id = $3
		  AND contract.script_unit_id = $4
		  AND contract.script_unit_generation_id = $5
		  AND shot.deleted_at IS NULL
		ORDER BY shot.shot_index
		FOR SHARE OF shot, contract
	`, req.StoryboardPlanID, identity.ProjectID, identity.ProjectGenerationID,
		identity.ScriptUnitID, identity.UnitGenerationID)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	shots := make([]commerceTimelineSourceShot, 0)
	for rows.Next() {
		var shot commerceTimelineSourceShot
		if err := rows.Scan(&shot.ID, &shot.ShotIndex, &shot.Title, &shot.DurationTicks,
			&shot.VideoArtifactID, &shot.VideoMediaFileID, &shot.VideoStorageKey,
			&shot.VideoStatus, &shot.ProductionReadiness, &shot.OnscreenText,
			&shot.SalesBeat, &shot.ContractHash); err != nil {
			return nil, "", err
		}
		if shot.DurationTicks <= 0 || shot.VideoStatus != "succeeded" || shot.VideoArtifactID == "" ||
			shot.VideoMediaFileID == "" || shot.VideoStorageKey == "" || shot.ProductionReadiness == "blocked" {
			return nil, "", commercepkg.Error{Code: commercepkg.CodeGenerationMismatch, Message: "仍有镜头视频未完成或未通过生产校验"}
		}
		shots = append(shots, shot)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if len(shots) == 0 {
		return nil, "", commercepkg.Error{Code: commercepkg.CodeGenerationMismatch, Message: "当前脚本单元没有可合成的镜头视频"}
	}
	return shots, aspectRatio, nil
}

func insertCommerceTimeline(ctx context.Context, tx pgx.Tx, project Project, createdBy string, identity commercepkg.UnitGenerationIdentity, req commerceTimelinePrepareRequest, aspectRatio, sourceHash string, shots []commerceTimelineSourceShot) (CommerceTimeline, error) {
	if _, err := tx.Exec(ctx, `
		UPDATE project_timelines SET status = 'archived', updated_at = now()
		WHERE project_id = $1 AND commerce_script_unit_id = $2
		  AND commerce_script_unit_generation_id = $3 AND status = 'draft'
	`, project.ID, identity.ScriptUnitID, identity.UnitGenerationID); err != nil {
		return CommerceTimeline{}, err
	}
	title := req.Title
	if title == "" {
		title = "脚本单元成片时间线"
	}
	metadata := mustRawJSON(map[string]any{
		"contractVersion": "commerce-timeline/v1", "sourceHash": sourceHash,
		"storyboardPlanId": req.StoryboardPlanID, "storyboardPlanRevision": req.ExpectedPlanRevision,
		"overlayPolicy": "deterministic_post_compose", "ctaEndCardSeconds": 2,
	})
	item, err := scanCommerceTimeline(tx.QueryRow(ctx, commerceTimelineInsertSQL, project.OrganizationID,
		project.ID, title, aspectRatio, req.Resolution, project.TimelineTimebase,
		project.FPSNumerator, project.FPSDenominator, metadata, createdBy,
		identity.ProjectGenerationID, identity.ScriptUnitID, identity.UnitGenerationID))
	if err != nil {
		return CommerceTimeline{}, err
	}
	currentTick := int64(0)
	for index, shot := range shots {
		var clipID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO timeline_clips(
				organization_id, project_id, timeline_id, storyboard_shot_id,
				video_artifact_id, video_media_file_id, clip_index, title, enabled,
				source_storage_key, start_tick, end_tick, source_duration_ticks,
				trim_start_tick, trim_end_tick, metadata, production_generation_id
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, true,
			        $9, $10, $11, $12, 0, $12, $13, $14)
			RETURNING id::text
		`, project.OrganizationID, project.ID, item.ID, shot.ID, shot.VideoArtifactID,
			shot.VideoMediaFileID, index, shot.Title, shot.VideoStorageKey,
			currentTick, currentTick+shot.DurationTicks, shot.DurationTicks,
			mustRawJSON(map[string]any{"commerceShotContractHash": shot.ContractHash}),
			identity.ProjectGenerationID).Scan(&clipID); err != nil {
			return CommerceTimeline{}, err
		}
		if text := strings.TrimSpace(shot.OnscreenText); text != "" {
			if err := insertCommerceTimelineOverlay(ctx, tx, createdBy, identity, item.ID, clipID, shot.ID,
				"onscreen_text", index, text, currentTick, currentTick+shot.DurationTicks,
				mustRawJSON(map[string]any{"position": "bottom"})); err != nil {
				return CommerceTimeline{}, err
			}
		}
		currentTick += shot.DurationTicks
	}
	for index := len(shots) - 1; index >= 0; index-- {
		shot := shots[index]
		if shot.SalesBeat != "cta" || strings.TrimSpace(shot.OnscreenText) == "" {
			continue
		}
		if err := insertCommerceTimelineOverlay(ctx, tx, createdBy, identity, item.ID, "", shot.ID,
			"cta_end_card", 0, strings.TrimSpace(shot.OnscreenText), currentTick,
			currentTick+2*project.TimelineTimebase, mustRawJSON(map[string]any{"position": "center"})); err != nil {
			return CommerceTimeline{}, err
		}
		break
	}
	return item, nil
}

func insertCommerceTimelineOverlay(ctx context.Context, tx pgx.Tx, createdBy string, identity commercepkg.UnitGenerationIdentity, timelineID, clipID, shotID, role string, ordinal int, text string, startTick, endTick int64, style json.RawMessage) error {
	hash := idempotencyRequestHash(map[string]any{"role": role, "ordinal": ordinal, "text": text, "startTick": startTick, "endTick": endTick, "style": style})
	_, err := tx.Exec(ctx, `
		INSERT INTO commerce_timeline_overlays(
			organization_id, project_id, production_generation_id, timeline_id,
			timeline_clip_id, commerce_script_unit_id, commerce_script_unit_generation_id,
			storyboard_shot_id, role, ordinal, text_content, start_tick, end_tick,
			style, content_hash, created_by
		)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid, $6, $7,
		        NULLIF($8, '')::uuid, $9, $10, $11, $12, $13, $14, $15, NULLIF($16, '')::uuid)
	`, identity.OrganizationID, identity.ProjectID, identity.ProjectGenerationID, timelineID,
		clipID, identity.ScriptUnitID, identity.UnitGenerationID, shotID,
		role, ordinal, text, startTick, endTick, style, hash, createdBy)
	return err
}

func replaceCommerceTimelineOverlays(ctx context.Context, tx pgx.Tx, createdBy string, identity commercepkg.UnitGenerationIdentity, timelineID string, overlays []commerceTimelineOverlayRequest) error {
	var exists int
	if err := tx.QueryRow(ctx, `
		SELECT 1 FROM project_timelines
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND commerce_script_unit_id = $4 AND commerce_script_unit_generation_id = $5
		  AND status IN ('draft', 'active')
		FOR UPDATE
	`, timelineID, identity.OrganizationID, identity.ProjectID, identity.ScriptUnitID, identity.UnitGenerationID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return commercepkg.Error{Code: commercepkg.CodeGenerationMismatch, Message: "时间线不属于当前脚本单元版本"}
		}
		return err
	}
	seen := make(map[string]struct{}, len(overlays))
	for index := range overlays {
		overlay := &overlays[index]
		overlay.Role = strings.TrimSpace(overlay.Role)
		overlay.Text = strings.TrimSpace(overlay.Text)
		if overlay.Role != "onscreen_text" && overlay.Role != "cta_end_card" {
			return commercepkg.Error{Code: commercepkg.CodeRevisionConflict, Message: "时间线叠加层类型无效"}
		}
		if overlay.Text == "" || overlay.StartTick < 0 || overlay.EndTick <= overlay.StartTick || overlay.Ordinal < 0 {
			return commercepkg.Error{Code: commercepkg.CodeRevisionConflict, Message: "时间线叠加层内容或时长无效"}
		}
		key := overlay.Role + ":" + strconv.Itoa(overlay.Ordinal)
		if _, exists := seen[key]; exists {
			return commercepkg.Error{Code: commercepkg.CodeRevisionConflict, Message: "时间线叠加层顺序重复"}
		}
		seen[key] = struct{}{}
		if overlay.Role == "onscreen_text" {
			if _, err := uuid.Parse(overlay.TimelineClipID); err != nil {
				return commercepkg.Error{Code: commercepkg.CodeRevisionConflict, Message: "屏幕文字缺少有效镜头片段"}
			}
			if _, err := uuid.Parse(overlay.StoryboardShotID); err != nil {
				return commercepkg.Error{Code: commercepkg.CodeRevisionConflict, Message: "屏幕文字缺少有效分镜"}
			}
		} else {
			overlay.TimelineClipID = ""
		}
		if len(overlay.Style) == 0 {
			overlay.Style = json.RawMessage(`{}`)
		}
		var object map[string]any
		if err := json.Unmarshal(overlay.Style, &object); err != nil || object == nil {
			return commercepkg.Error{Code: commercepkg.CodeRevisionConflict, Message: "时间线叠加层样式无效"}
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM commerce_timeline_overlays WHERE timeline_id = $1`, timelineID); err != nil {
		return err
	}
	for _, overlay := range overlays {
		if err := insertCommerceTimelineOverlay(ctx, tx, createdBy, identity, timelineID,
			overlay.TimelineClipID, overlay.StoryboardShotID, overlay.Role, overlay.Ordinal,
			overlay.Text, overlay.StartTick, overlay.EndTick, overlay.Style); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) createCommerceFinalComposeRunTx(ctx context.Context, tx pgx.Tx, project Project, createdBy, scriptUnitID string, req commerceFinalComposeRequest, idempotencyScope, idempotencyKey, retryOfRunID string) (commercepkg.ProductionRun, bool, error) {
	identity, err := s.commerceCatalog.LockActiveStoryboardGeneration(ctx, tx, project.OrganizationID, project.ID, scriptUnitID)
	if err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	if identity.UnitGenerationID != req.ExpectedUnitGenerationID {
		return commercepkg.ProductionRun{}, false, commercepkg.Error{Code: commercepkg.CodeGenerationMismatch, Message: "脚本单元生产代已变化，请刷新后重试"}
	}
	timeline, err := scanCommerceTimeline(tx.QueryRow(ctx, commerceTimelineSelectSQL+`
		WHERE timeline.id = $1 AND timeline.organization_id = $2 AND timeline.project_id = $3
		  AND timeline.commerce_script_unit_id = $4
		  AND timeline.commerce_script_unit_generation_id = $5
		  AND timeline.revision = $6 AND timeline.status IN ('draft', 'active')
		FOR UPDATE
	`, req.TimelineID, project.OrganizationID, project.ID, identity.ScriptUnitID,
		identity.UnitGenerationID, req.ExpectedTimelineRevision))
	if err != nil {
		return commercepkg.ProductionRun{}, false, commercepkg.Error{Code: commercepkg.CodeGenerationMismatch, Message: "时间线已变化，请刷新后重试", Cause: err}
	}
	input := workflows.CommerceFinalComposeInput{
		Identity: identity, TimelineID: timeline.ID, ExpectedTimelineRevision: timeline.Revision,
		Title: defaultAPIString(req.Title, timeline.Title), Resolution: defaultAPIString(req.Resolution, timeline.Resolution, "1080p"),
		AspectRatio: timeline.AspectRatio, CreatedBy: createdBy, AttemptGeneration: 1,
	}
	if err := validateCommerceTimelineForComposeTx(ctx, tx, input); err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	inputHash, err := workflows.CommerceFinalComposeSubjectHash(input)
	if err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	snapshot := commerceFinalComposeSnapshot{TimelineID: timeline.ID, TimelineRevision: timeline.Revision,
		Title: input.Title, Resolution: input.Resolution, AspectRatio: input.AspectRatio, RetryOfRunID: retryOfRunID}
	run, created, err := s.commerceCatalog.CreateProductionRun(ctx, tx, commercepkg.CreateProductionRunParams{
		Identity: identity, RunType: commercepkg.RunTypeFinalCompose,
		IdempotencyScope: idempotencyScope, IdempotencyKey: idempotencyKey,
		InputSnapshot: mustRawJSON(snapshot), Subjects: []commercepkg.ProductionSubject{{
			Type: commercepkg.SubjectFinalCompose, Key: timeline.ID, InputHash: inputHash,
		}}, CreatedBy: createdBy,
	})
	if err != nil || !created {
		return run, created, err
	}
	input.WorkflowRunID = uuid.NewString()
	input.ProductionRunID = run.ID
	temporalWorkflowID := "commerce-final-compose-" + run.ID
	raw, _, err := marshalWorkflowStartInput(input)
	if err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO workflow_runs(
			id, organization_id, project_id, temporal_workflow_id, workflow_type,
			status, input, output, created_by, production_generation_id,
			video_production_binding_id, video_production_binding_revision,
			attempt_generation
		)
		VALUES ($1, $2, $3, $4, 'commerce_final_compose', 'queued', $5, '{}', $6, $7, $8, $9, 1)
	`, input.WorkflowRunID, identity.OrganizationID, identity.ProjectID, temporalWorkflowID,
		raw, createdBy, identity.ProjectGenerationID, identity.VideoProductionBindingID,
		identity.VideoProductionBindingRevision); err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	if err := s.enqueueWorkflowStartTx(ctx, tx, input.WorkflowRunID, "", identity.OrganizationID,
		identity.ProjectID, identity.ProjectGenerationID, "commerce_final_compose",
		commerceFinalComposeHandler, temporalWorkflowID, workflows.ScriptTaskQueue, input); err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	if err := s.commerceCatalog.AttachProductionRunWorkflow(ctx, tx, identity.OrganizationID, identity.ProjectID, run.ID, input.WorkflowRunID); err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	workflowRun, err := scanWorkflowRun(tx.QueryRow(ctx, workflowRunSelectSQL(`WHERE id = $1`), input.WorkflowRunID))
	if err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	if err := insertWorkflowQueuedEventTx(ctx, tx, workflowRun, workflowRun.WorkflowType); err != nil {
		return commercepkg.ProductionRun{}, false, err
	}
	run.WorkflowRunID = input.WorkflowRunID
	return run, true, nil
}

func validateCommerceTimelineForComposeTx(ctx context.Context, tx pgx.Tx, input workflows.CommerceFinalComposeInput) error {
	var total, invalid int
	if err := tx.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE
			shot.id IS NULL OR contract.storyboard_shot_id IS NULL
			OR contract.script_unit_id IS DISTINCT FROM $3::uuid
			OR contract.script_unit_generation_id IS DISTINCT FROM $4::uuid
			OR COALESCE(shot.video_status, '') <> 'succeeded'
			OR COALESCE(clip.source_storage_key, shot.video_storage_key, '') = ''
		)
		FROM timeline_clips clip
		LEFT JOIN storyboard_shots shot ON shot.id = clip.storyboard_shot_id AND shot.deleted_at IS NULL
		LEFT JOIN commerce_shot_contracts contract
		  ON contract.storyboard_shot_id = shot.id
		 AND contract.commerce_storyboard_plan_id = shot.commerce_storyboard_plan_id
		 AND contract.organization_id = shot.organization_id
		 AND contract.project_id = shot.project_id
		WHERE clip.timeline_id = $1 AND clip.project_id = $2 AND clip.enabled
	`, input.TimelineID, input.Identity.ProjectID, input.Identity.ScriptUnitID, input.Identity.UnitGenerationID).Scan(&total, &invalid); err != nil {
		return err
	}
	if total == 0 || invalid > 0 {
		return commercepkg.Error{Code: commercepkg.CodeGenerationMismatch, Message: "时间线仍有未完成或身份不一致的镜头视频"}
	}
	return nil
}

func normalizeCommerceFinalComposeRequest(req *commerceFinalComposeRequest) error {
	req.TimelineID = strings.TrimSpace(req.TimelineID)
	req.ExpectedUnitGenerationID = strings.TrimSpace(req.ExpectedUnitGenerationID)
	req.Title = strings.TrimSpace(req.Title)
	req.Resolution = strings.ToLower(strings.TrimSpace(req.Resolution))
	if _, err := uuid.Parse(req.TimelineID); err != nil || req.ExpectedTimelineRevision < 1 {
		return commercepkg.Error{Code: commercepkg.CodeRevisionConflict, Message: "成片时间线标识或版本无效"}
	}
	if _, err := uuid.Parse(req.ExpectedUnitGenerationID); err != nil {
		return commercepkg.Error{Code: commercepkg.CodeGenerationMismatch, Message: "脚本单元生产代标识无效"}
	}
	if req.Resolution == "" {
		req.Resolution = "1080p"
	}
	return nil
}

func (s *Server) commerceTimelineDetail(r *http.Request, project Project, identity commercepkg.UnitGenerationIdentity, timelineID string) (CommerceTimelineDetail, error) {
	timeline, err := scanCommerceTimeline(s.db.QueryRow(r.Context(), commerceTimelineSelectSQL+`
		WHERE timeline.id = $1 AND timeline.organization_id = $2 AND timeline.project_id = $3
		  AND timeline.commerce_script_unit_id = $4
		  AND timeline.commerce_script_unit_generation_id = $5
	`, timelineID, project.OrganizationID, project.ID, identity.ScriptUnitID, identity.UnitGenerationID))
	if err != nil {
		return CommerceTimelineDetail{}, err
	}
	clips, err := s.timelineClipDetails(r, project.ID, timeline.ID)
	if err != nil {
		return CommerceTimelineDetail{}, err
	}
	overlays, err := s.commerceTimelineOverlays(r.Context(), timeline.ID)
	if err != nil {
		return CommerceTimelineDetail{}, err
	}
	versions, err := s.commerceFinalVideoVersions(r, project.ID, identity.ScriptUnitID, identity.UnitGenerationID, "")
	if err != nil {
		return CommerceTimelineDetail{}, err
	}
	return CommerceTimelineDetail{Timeline: timeline, Clips: clips, Overlays: overlays, FinalVideoVersions: versions}, nil
}

func (s *Server) commerceTimelineOverlays(ctx context.Context, timelineID string) ([]CommerceTimelineOverlay, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text, timeline_id::text, timeline_clip_id::text, storyboard_shot_id::text,
		       role, ordinal, text_content, start_tick, end_tick, style, content_hash
		FROM commerce_timeline_overlays
		WHERE timeline_id = $1
		ORDER BY CASE role WHEN 'onscreen_text' THEN 0 ELSE 1 END, ordinal
	`, timelineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CommerceTimelineOverlay, 0)
	for rows.Next() {
		var item CommerceTimelineOverlay
		var clipID, shotID sql.NullString
		if err := rows.Scan(&item.ID, &item.TimelineID, &clipID, &shotID, &item.Role,
			&item.Ordinal, &item.Text, &item.StartTick, &item.EndTick, &item.Style, &item.ContentHash); err != nil {
			return nil, err
		}
		item.TimelineClipID = stringPtrFromNull(clipID)
		item.StoryboardShotID = stringPtrFromNull(shotID)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) commerceFinalVideoVersions(r *http.Request, projectID, scriptUnitID, unitGenerationID, versionID string) ([]CommerceFinalVideoVersion, error) {
	query := `
		SELECT version_row.id, version_row.organization_id, version_row.project_id, version_row.timeline_id,
		       version_row.workflow_run_id::text, version_row.version, version_row.title, version_row.status,
		       version_row.artifact_id::text, version_row.media_file_id::text, version_row.storage_key, version_row.duration_ticks,
		       timeline.timeline_timebase, timeline.fps_numerator, timeline.fps_denominator,
		       version_row.resolution, version_row.aspect_ratio, version_row.native_audio_status, version_row.production_readiness,
		       version_row.compose_settings, version_row.metadata, version_row.created_by::text, version_row.created_at,
		       version_row.commerce_script_unit_id::text, version_row.commerce_script_unit_generation_id::text
		FROM final_video_versions version_row
		JOIN project_timelines timeline ON timeline.id = version_row.timeline_id
		WHERE version_row.project_id = $1 AND version_row.commerce_script_unit_id = $2
		  AND version_row.commerce_script_unit_generation_id = $3
		  AND timeline.commerce_script_unit_id = $2
		  AND timeline.commerce_script_unit_generation_id = $3
	`
	args := []any{projectID, scriptUnitID, unitGenerationID}
	if versionID != "" {
		query += " AND version_row.id = $4"
		args = append(args, versionID)
	}
	query += " ORDER BY CASE version_row.status WHEN 'active' THEN 0 WHEN 'ready' THEN 1 ELSE 2 END, version_row.version DESC"
	rows, err := s.db.Query(r.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CommerceFinalVideoVersion, 0)
	for rows.Next() {
		item, err := scanCommerceFinalVideoVersion(rows)
		if err != nil {
			return nil, err
		}
		s.attachFinalVideoPreview(r, &item.FinalVideoVersion)
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanCommerceFinalVideoVersion(row rowScan) (CommerceFinalVideoVersion, error) {
	var item CommerceFinalVideoVersion
	var workflowRunID, artifactID, mediaFileID, storageKey, createdBy sql.NullString
	var duration sql.NullInt64
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.TimelineID, &workflowRunID,
		&item.Version, &item.Title, &item.Status, &artifactID, &mediaFileID, &storageKey, &duration,
		&item.TimelineTimebase, &item.FPSNumerator, &item.FPSDenominator,
		&item.Resolution, &item.AspectRatio, &item.NativeAudioStatus, &item.ProductionReadiness,
		&item.ComposeSettings, &item.Metadata, &createdBy, &item.CreatedAt,
		&item.ScriptUnitID, &item.UnitGenerationID,
	)
	item.WorkflowRunID = stringPtrFromNull(workflowRunID)
	item.ArtifactID = stringPtrFromNull(artifactID)
	item.MediaFileID = stringPtrFromNull(mediaFileID)
	item.StorageKey = stringPtrFromNull(storageKey)
	item.CreatedBy = stringPtrFromNull(createdBy)
	if duration.Valid {
		item.DurationTicks = &duration.Int64
		seconds := float64(duration.Int64) / float64(item.TimelineTimebase)
		item.DurationSeconds = &seconds
	}
	return item, err
}

func findCommerceTimelineBySourceHash(ctx context.Context, tx pgx.Tx, identity commercepkg.UnitGenerationIdentity, sourceHash string) (CommerceTimeline, bool, error) {
	item, err := scanCommerceTimeline(tx.QueryRow(ctx, commerceTimelineSelectSQL+`
		WHERE timeline.organization_id = $1 AND timeline.project_id = $2
		  AND timeline.commerce_script_unit_id = $3
		  AND timeline.commerce_script_unit_generation_id = $4
		  AND timeline.status IN ('draft', 'active')
		  AND timeline.metadata->>'sourceHash' = $5
		ORDER BY timeline.created_at DESC LIMIT 1 FOR UPDATE
	`, identity.OrganizationID, identity.ProjectID, identity.ScriptUnitID, identity.UnitGenerationID, sourceHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return CommerceTimeline{}, false, nil
	}
	return item, err == nil, err
}

func updateCommerceTimelineRow(ctx context.Context, tx pgx.Tx, timelineID, title, editedBy string, expectedRevision int64) (CommerceTimeline, error) {
	var id string
	if err := tx.QueryRow(ctx, `
		UPDATE project_timelines
		SET title = $2, revision = revision + 1, edited_by = $3, edited_at = now(), updated_at = now()
		WHERE id = $1 AND revision = $4
		RETURNING id::text
	`, timelineID, title, editedBy, expectedRevision).Scan(&id); err != nil {
		return CommerceTimeline{}, err
	}
	return scanCommerceTimeline(tx.QueryRow(ctx, commerceTimelineSelectSQL+` WHERE timeline.id = $1`, id))
}

const commerceTimelineSelectSQL = `
	SELECT timeline.id::text, timeline.organization_id::text, timeline.project_id::text,
	       timeline.production_generation_id::text, timeline.commerce_script_unit_id::text,
	       timeline.commerce_script_unit_generation_id::text, timeline.workflow_run_id::text,
	       timeline.revision, timeline.title, timeline.status, timeline.aspect_ratio,
	       timeline.resolution, timeline.timeline_timebase, timeline.fps_numerator,
	       timeline.fps_denominator, timeline.metadata, timeline.created_at, timeline.updated_at
	FROM project_timelines timeline
`

const commerceTimelineInsertSQL = `
	INSERT INTO project_timelines(
		organization_id, project_id, title, status, aspect_ratio, resolution,
		timeline_timebase, fps_numerator, fps_denominator, metadata, created_by,
		production_generation_id, commerce_script_unit_id, commerce_script_unit_generation_id
	)
	VALUES ($1, $2, $3, 'draft', $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	RETURNING id::text, organization_id::text, project_id::text,
	          production_generation_id::text, commerce_script_unit_id::text,
	          commerce_script_unit_generation_id::text, workflow_run_id::text,
	          revision, title, status, aspect_ratio, resolution, timeline_timebase,
	          fps_numerator, fps_denominator, metadata, created_at, updated_at
`

func scanCommerceTimeline(row rowScan) (CommerceTimeline, error) {
	var item CommerceTimeline
	var workflowRunID sql.NullString
	err := row.Scan(&item.ID, &item.OrganizationID, &item.ProjectID,
		&item.ProductionGenerationID, &item.ScriptUnitID, &item.UnitGenerationID,
		&workflowRunID, &item.Revision, &item.Title, &item.Status, &item.AspectRatio,
		&item.Resolution, &item.TimelineTimebase, &item.FPSNumerator,
		&item.FPSDenominator, &item.Metadata, &item.CreatedAt, &item.UpdatedAt)
	item.WorkflowRunID = stringPtrFromNull(workflowRunID)
	return item, err
}
