package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/jackc/pgx/v5"
)

func (s *Server) verifyStoryboardShotRenderPlanAudio(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
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
		Decision string `json:"decision"`
		Notes    string `json:"notes"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Decision = strings.ToLower(strings.TrimSpace(req.Decision))
	if req.Decision != "approve" && req.Decision != "reject" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "decision must be approve or reject", nil, false)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var planID, requirement, workflowRunID string
	if err := tx.QueryRow(r.Context(), `
		SELECT id::text, audio_requirement, COALESCE(workflow_run_id::text, '')
		FROM video_render_plans
		WHERE project_id = $1 AND storyboard_shot_id = $2 AND active = true
		FOR UPDATE
	`, project.ID, shot.ID).Scan(&planID, &requirement, &workflowRunID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if req.Decision == "approve" {
		if _, err := tx.Exec(r.Context(), `
			UPDATE video_render_segments
			SET audio_verification_status = CASE
			      WHEN NOT native_audio_requested THEN 'not_requested'
			      WHEN COALESCE(native_audio_detected, false) THEN 'audio_verified'
			      WHEN $3 = 'required' THEN 'needs_audio_retry'
			      ELSE 'native_audio_unavailable'
			    END,
			    production_readiness = CASE
			      WHEN NOT native_audio_requested THEN 'ready'
			      WHEN COALESCE(native_audio_detected, false) THEN 'ready'
			      WHEN $3 = 'required' THEN 'blocked'
			      ELSE 'preview_only'
			    END,
			    audio_verified_by = CASE WHEN COALESCE(native_audio_detected, false) THEN NULLIF($4, '')::uuid ELSE NULL END,
			    audio_verified_at = CASE WHEN COALESCE(native_audio_detected, false) THEN now() ELSE NULL END,
			    audio_verification_notes = NULLIF($5, ''), updated_at = now()
			WHERE video_render_plan_id = $1 AND storyboard_shot_id = $2 AND status = 'succeeded'
		`, planID, shot.ID, requirement, principal.UserID, strings.TrimSpace(req.Notes)); err != nil {
			s.writeError(w, r, err)
			return
		}
	} else {
		if _, err := tx.Exec(r.Context(), `
			UPDATE video_render_segments
			SET audio_verification_status = CASE WHEN native_audio_requested THEN 'needs_audio_retry' ELSE 'not_requested' END,
			    production_readiness = CASE WHEN native_audio_requested THEN 'blocked' ELSE 'ready' END,
			    audio_verified_by = NULL, audio_verified_at = NULL,
			    audio_verification_notes = NULLIF($3, ''), updated_at = now()
			WHERE video_render_plan_id = $1 AND storyboard_shot_id = $2 AND status = 'succeeded'
		`, planID, shot.ID, strings.TrimSpace(req.Notes)); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if err := refreshRenderPlanAudioStateTx(r.Context(), tx, planID, shot.ID, principal.UserID, strings.TrimSpace(req.Notes)); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := refreshFinalVideoReadinessForShotTx(r.Context(), tx, shot.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "storyboard.audio.verification.completed", "video_render_plan", planID, mustRawJSON(map[string]any{
		"planId": planID, "shotId": shot.ID, "workflowRunId": workflowRunID, "decision": req.Decision, "notes": strings.TrimSpace(req.Notes), "verifiedBy": principal.UserID,
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	detail, err := s.videoRenderPlanDetail(r.Context(), project.ID, shot.ID, planID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, detail, nil)
}

func refreshRenderPlanAudioStateTx(ctx context.Context, tx pgx.Tx, planID, shotID, verifiedBy, notes string) error {
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
		  SELECT CASE WHEN retry > 0 THEN 'needs_audio_retry'
		              WHEN unavailable > 0 THEN 'native_audio_unavailable'
		              WHEN unverified > 0 THEN 'audio_unverified'
		              WHEN verified > 0 THEN 'audio_verified'
		              ELSE 'not_requested' END AS audio_status,
		         CASE WHEN blocked > 0 THEN 'blocked'
		              WHEN partial > 0 THEN 'partial'
		              WHEN total > 0 AND ready = total THEN 'ready'
		              WHEN preview > 0 THEN 'preview_only'
		              ELSE 'blocked' END AS readiness
		  FROM stats
		)
		UPDATE video_render_plans plan
		SET native_audio_status = resolved.audio_status, production_readiness = resolved.readiness,
		    audio_verified_by = CASE WHEN resolved.audio_status = 'audio_verified' THEN NULLIF($2, '')::uuid ELSE NULL END,
		    audio_verified_at = CASE WHEN resolved.audio_status = 'audio_verified' THEN now() ELSE NULL END,
		    audio_verification_notes = NULLIF($3, ''), updated_at = now()
		FROM resolved WHERE plan.id = $1
		RETURNING plan.native_audio_status, plan.production_readiness
	`, planID, verifiedBy, notes).Scan(&audioStatus, &readiness); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET native_audio_status = $2, production_readiness = $3, updated_at = now()
		WHERE id = $1 AND active_video_render_plan_id = $4
	`, shotID, audioStatus, readiness, planID)
	return err
}

func refreshFinalVideoReadinessForShotTx(ctx context.Context, tx pgx.Tx, shotID string) error {
	_, err := tx.Exec(ctx, `
		WITH affected AS (
		  SELECT DISTINCT version.id, version.timeline_id
		  FROM final_video_versions version
		  JOIN timeline_clips changed ON changed.timeline_id = version.timeline_id
		  WHERE changed.storyboard_shot_id = $1 AND changed.enabled = true
		), stats AS (
		  SELECT affected.id,
		         count(*) FILTER (WHERE COALESCE(shot.production_readiness, 'ready') = 'blocked') AS blocked,
		         count(*) FILTER (WHERE COALESCE(shot.production_readiness, 'ready') = 'partial') AS partial,
		         count(*) FILTER (WHERE COALESCE(shot.production_readiness, 'ready') = 'preview_only') AS preview,
		         count(*) FILTER (WHERE COALESCE(shot.native_audio_status, 'not_requested') = 'needs_audio_retry') AS retry,
		         count(*) FILTER (WHERE COALESCE(shot.native_audio_status, 'not_requested') = 'native_audio_unavailable') AS unavailable,
		         count(*) FILTER (WHERE COALESCE(shot.native_audio_status, 'not_requested') = 'audio_unverified') AS unverified,
		         count(*) FILTER (WHERE COALESCE(shot.native_audio_status, 'not_requested') = 'audio_verified') AS verified
		  FROM affected
		  JOIN timeline_clips clip ON clip.timeline_id = affected.timeline_id AND clip.enabled = true
		  LEFT JOIN storyboard_shots shot ON shot.id = clip.storyboard_shot_id
		  GROUP BY affected.id
		), resolved AS (
		  SELECT id,
		         CASE WHEN blocked > 0 THEN 'blocked' WHEN partial > 0 THEN 'partial' WHEN preview > 0 THEN 'preview_only' ELSE 'ready' END AS readiness,
		         CASE WHEN retry > 0 THEN 'needs_audio_retry' WHEN unavailable > 0 THEN 'native_audio_unavailable'
		              WHEN unverified > 0 THEN 'audio_unverified' WHEN verified > 0 THEN 'audio_verified' ELSE 'not_requested' END AS audio_status
		  FROM stats
		)
		UPDATE final_video_versions version
		SET production_readiness = resolved.readiness, native_audio_status = resolved.audio_status,
		    metadata = version.metadata || jsonb_build_object('productionReadiness', resolved.readiness, 'nativeAudioStatus', resolved.audio_status)
		FROM resolved WHERE version.id = resolved.id
	`, shotID)
	return err
}
