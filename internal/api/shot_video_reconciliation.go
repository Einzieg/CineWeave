package api

import (
	"net/http"
)

func (s *Server) reconcileTerminalShotVideoStates(r *http.Request, projectID, generationID string) error {
	if _, err := s.db.Exec(r.Context(), `
		UPDATE storyboard_shots shot
		SET video_status = 'failed',
		    status = 'video_failed',
		    video_error_code = 'VIDEO_OUTPUT_MISSING',
		    video_error_message = '视频任务已结束，但没有可用的视频媒体，请重试生成',
		    video_completed_at = COALESCE(video_completed_at, now()),
		    stale_state = 'needs_regeneration',
		    updated_at = now()
		WHERE shot.project_id = $1
		  AND shot.production_generation_id = $2
		  AND shot.deleted_at IS NULL
		  AND shot.video_status = 'succeeded'
		  AND NOT EXISTS (
		    SELECT 1
		    FROM workflow_runs active_run
		    WHERE active_run.id = shot.video_workflow_run_id
		      AND active_run.status IN ('queued', 'running', 'cancelling')
		  )
		  AND NOT EXISTS (
		    SELECT 1
		    FROM artifacts artifact
		    JOIN media_files media
		      ON media.id = shot.video_media_file_id
		     AND media.project_id = shot.project_id
		     AND media.artifact_id = artifact.id
		    WHERE artifact.id = shot.video_artifact_id
		      AND artifact.project_id = shot.project_id
		      AND COALESCE(shot.video_storage_key, '') <> ''
		      AND COALESCE(media.storage_key, '') = COALESCE(shot.video_storage_key, '')
		  )
	`, projectID, generationID); err != nil {
		return err
	}
	_, err := s.db.Exec(r.Context(), `
		UPDATE storyboard_shots shot
		SET video_status = 'failed',
		    status = 'video_failed',
		    video_error_code = CASE WHEN run.status = 'cancelled' THEN 'USER_CANCELLED' ELSE 'WORKFLOW_TERMINAL' END,
		    video_error_message = CASE
		      WHEN run.status = 'cancelled' THEN '关联工作流已取消，视频生成未完成'
		      ELSE '关联工作流已结束，视频生成未完成'
		    END,
		    video_completed_at = COALESCE(video_completed_at, now()),
		    stale_state = 'needs_regeneration',
		    updated_at = now()
		FROM workflow_runs run
		WHERE shot.project_id = $1
		  AND shot.production_generation_id = $2
		  AND shot.deleted_at IS NULL
		  AND run.id = shot.video_workflow_run_id
		  AND shot.video_status IN ('queued', 'running')
		  AND run.status IN ('succeeded', 'partial_succeeded', 'failed', 'cancelled')
	`, projectID, generationID)
	return err
}
