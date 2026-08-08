package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/production"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
)

type clearProjectProductionContentResult struct {
	PreviousGenerationID   string `json:"previousGenerationId"`
	ActiveGenerationID     string `json:"activeGenerationId"`
	ActiveGenerationNo     int64  `json:"activeGenerationNo"`
	NovelSourceCount       int64  `json:"novelSourceCount"`
	NovelChapterCount      int64  `json:"novelChapterCount"`
	DeletedSourceCount     int64  `json:"deletedSourceCount"`
	DeletedEventCount      int64  `json:"deletedEventCount"`
	DeletedPlanCount       int64  `json:"deletedPlanCount"`
	DeletedScriptCount     int64  `json:"deletedScriptCount"`
	DeletedAssetCount      int64  `json:"deletedAssetCount"`
	DeletedStoryboardCount int64  `json:"deletedStoryboardCount"`
	DeletedReviewCount     int64  `json:"deletedReviewCount"`
	DeletedExportCount     int64  `json:"deletedExportCount"`
}

type clearProjectProductionContentActionInput struct {
	Confirmation string `json:"confirmation"`
	Reason       string `json:"reason"`
}

func (s *Server) executeProjectClearProductionContentSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	var input clearProjectProductionContentActionInput
	if err := decodeWorkflowActionInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	if strings.TrimSpace(input.Confirmation) != "preserve_novel_sources" {
		return agentToolResult{}, newAPIError(http.StatusUnprocessableEntity, "PROJECT_CLEAR_CONFIRMATION_REQUIRED", "必须明确确认保留小说原文后才能清空生产内容")
	}
	result, err := s.clearProjectProductionContentActionTx(ctx, tx, project, principal.UserID, strings.TrimSpace(input.Reason), map[string]any{
		"projectControlCommandId": command.ID,
		"controllerType":          command.ControllerType,
	})
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("project.clear_production_content", workflowActionArguments(raw), "已保留小说原文和分卷分集，并清空其余生产内容。", map[string]any{
		"result": result,
	}), nil
}

func (s *Server) clearProjectProductionContentActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorID string,
	reason string,
	provenance map[string]any,
) (clearProjectProductionContentResult, error) {

	var activeWorkflowCount, activeProviderTaskCount int
	if err := tx.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM workflow_runs
		   WHERE project_id = $1
		     AND status IN ('queued', 'running', 'cancelling')
		     AND workflow_type <> 'project_agent'),
		  (SELECT count(*) FROM provider_async_tasks
		   WHERE project_id = $1 AND status IN ('queued', 'running', 'cancelling'))
	`, project.ID).Scan(&activeWorkflowCount, &activeProviderTaskCount); err != nil {
		return clearProjectProductionContentResult{}, err
	}
	if activeWorkflowCount > 0 || activeProviderTaskCount > 0 {
		return clearProjectProductionContentResult{}, newAPIError(http.StatusConflict, "PROJECT_PRODUCTION_BUSY", "项目仍有生产任务运行，请先取消或等待任务结束后再清空")
	}

	previousGenerationID, generation, err := videoproduction.ResetActiveGeneration(ctx, tx, project.ID)
	if err != nil {
		return clearProjectProductionContentResult{}, err
	}
	result := clearProjectProductionContentResult{
		PreviousGenerationID: previousGenerationID,
		ActiveGenerationID:   generation.ID,
		ActiveGenerationNo:   generation.GenerationNo,
	}
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE source_type = 'novel'),
		       (SELECT count(*) FROM novel_chapters chapter
		        JOIN project_sources source ON source.id = chapter.source_id
		        WHERE source.project_id = $1 AND source.source_type = 'novel')
		FROM project_sources
		WHERE project_id = $1
	`, project.ID).Scan(&result.NovelSourceCount, &result.NovelChapterCount); err != nil {
		return clearProjectProductionContentResult{}, err
	}

	deleteCount := func(statement string) (int64, error) {
		tag, execErr := tx.Exec(ctx, statement, project.ID)
		if execErr != nil {
			return 0, execErr
		}
		return tag.RowsAffected(), nil
	}
	if result.DeletedReviewCount, err = deleteCount(`DELETE FROM review_fixes WHERE project_id = $1`); err != nil {
		return clearProjectProductionContentResult{}, err
	}
	for _, statement := range []string{
		`DELETE FROM review_items WHERE project_id = $1`,
		`DELETE FROM review_runs WHERE project_id = $1`,
		`DELETE FROM review_tasks WHERE project_id = $1`,
	} {
		count, execErr := deleteCount(statement)
		if execErr != nil {
			return clearProjectProductionContentResult{}, execErr
		}
		result.DeletedReviewCount += count
	}
	if result.DeletedPlanCount, err = deleteCount(`DELETE FROM adaptation_plans WHERE project_id = $1`); err != nil {
		return clearProjectProductionContentResult{}, err
	}
	if result.DeletedEventCount, err = deleteCount(`DELETE FROM novel_events WHERE project_id = $1`); err != nil {
		return clearProjectProductionContentResult{}, err
	}
	if result.DeletedStoryboardCount, err = deleteCount(`DELETE FROM storyboard_shots WHERE project_id = $1`); err != nil {
		return clearProjectProductionContentResult{}, err
	}
	storyboardCount, err := deleteCount(`DELETE FROM storyboards WHERE project_id = $1`)
	if err != nil {
		return clearProjectProductionContentResult{}, err
	}
	result.DeletedStoryboardCount += storyboardCount
	if result.DeletedScriptCount, err = deleteCount(`DELETE FROM scripts WHERE project_id = $1`); err != nil {
		return clearProjectProductionContentResult{}, err
	}
	if result.DeletedAssetCount, err = deleteCount(`DELETE FROM canonical_assets WHERE project_id = $1`); err != nil {
		return clearProjectProductionContentResult{}, err
	}
	legacyAssets, err := deleteCount(`DELETE FROM assets WHERE project_id = $1`)
	if err != nil {
		return clearProjectProductionContentResult{}, err
	}
	result.DeletedAssetCount += legacyAssets
	if result.DeletedExportCount, err = deleteCount(`DELETE FROM project_exports WHERE project_id = $1`); err != nil {
		return clearProjectProductionContentResult{}, err
	}
	if result.DeletedSourceCount, err = deleteCount(`DELETE FROM project_sources WHERE project_id = $1 AND source_type <> 'novel'`); err != nil {
		return clearProjectProductionContentResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE novel_chapters chapter
		SET event_state = 'pending', event_summary = NULL, error_message = NULL, updated_at = now()
		FROM project_sources source
		WHERE source.id = chapter.source_id
		  AND source.project_id = $1
		  AND source.source_type = 'novel'
	`, project.ID); err != nil {
		return clearProjectProductionContentResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_sources
		SET metadata = COALESCE(metadata, '{}'::jsonb) - 'sourceChangedAt' - 'downstreamStaleAt' - 'changedFields',
		    updated_at = now()
		WHERE project_id = $1 AND source_type = 'novel'
	`, project.ID); err != nil {
		return clearProjectProductionContentResult{}, err
	}
	payloadValues := map[string]any{
		"clearedBy":            nullableMetadataValue(actorID),
		"reason":               reason,
		"previousGenerationId": result.PreviousGenerationID,
		"activeGenerationId":   result.ActiveGenerationID,
		"counts":               result,
	}
	for key, value := range provenance {
		payloadValues[key] = value
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "project.production_content.cleared", "project", project.ID, mustRawJSON(payloadValues)); err != nil {
		return clearProjectProductionContentResult{}, err
	}
	return result, nil
}

func (s *Server) agentToolUpdateScriptEpisode(r *http.Request, principal auth.Principal, project Project, args map[string]any) agentToolResult {
	episodeID := agentReferenceStringArg(args, "episodeId")
	if episodeID == "" {
		return agentToolError("script.update_episode", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "episodeId is required"))
	}
	patch := agentMapArg(args, "patch")
	if len(patch) == 0 {
		return agentToolError("script.update_episode", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "patch is required"))
	}
	current, err := s.scriptEpisode(r, project.ID, episodeID)
	if err != nil {
		return agentToolError("script.update_episode", args, err)
	}
	title := firstNonEmpty(agentStringFromMap(patch, "episodeTitle"), current.EpisodeTitle)
	content := current.Content
	if value, exists := patch["content"]; exists {
		content = strings.TrimSpace(stringValueFromAny(value))
	}
	contentFormat := firstNonEmpty(agentStringFromMap(patch, "contentFormat"), current.ContentFormat)
	reviewStatus := firstNonEmpty(agentStringFromMap(patch, "reviewStatus"), current.ReviewStatus)
	staleState := firstNonEmpty(agentStringFromMap(patch, "staleState"), current.StaleState)
	if title == "" || !validScriptContentFormat(contentFormat) || !validScriptReviewStatus(reviewStatus) || !validScriptStaleState(staleState) {
		return agentToolError("script.update_episode", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "episode fields are invalid"))
	}
	metadata := current.Metadata
	if raw, exists := patch["metadata"]; exists {
		metadata = mustRawJSON(agentObjectFromAny(raw))
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return agentToolError("script.update_episode", args, err)
	}
	defer tx.Rollback(r.Context())
	item, err := scanScriptEpisode(tx.QueryRow(r.Context(), `
		UPDATE script_episodes
		SET episode_title = $3,
		    content = $4,
		    content_format = $5,
		    review_status = $6,
		    stale_state = $7,
		    metadata = $8,
		    manual_override = true,
		    edited_by = $9,
		    edited_at = now()
		WHERE project_id = $1 AND id = $2
		RETURNING id, organization_id, project_id, script_id, script_version_id, source_id, source_chapter_id,
		          episode_index, volume_index, section_index, volume_title, episode_title, content, content_format,
		          revision, content_hash,
		          prompt_version_id, prompt_hash, provider_call_id, review_status, manual_override, stale_state,
		          metadata, created_by, edited_by, created_at, updated_at, edited_at
	`, project.ID, current.ID, title, content, contentFormat, reviewStatus, staleState, metadata, principal.UserID))
	if err != nil {
		return agentToolError("script.update_episode", args, err)
	}
	if err := rebuildScriptVersionContentFromEpisodesTx(r, tx, project.ID, item.ScriptVersionID); err != nil {
		return agentToolError("script.update_episode", args, err)
	}
	if err := markScriptVersionDownstreamStale(r.Context(), tx, project.ID, item.ScriptVersionID); err != nil {
		return agentToolError("script.update_episode", args, err)
	}
	if err := production.MarkFinalVideoStale(r.Context(), tx, project.ID, ""); err != nil {
		return agentToolError("script.update_episode", args, err)
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "script.episode.updated", "script_episode", item.ID, mustRawJSON(map[string]any{
		"scriptId":        item.ScriptID,
		"scriptVersionId": item.ScriptVersionID,
		"episodeIndex":    item.EpisodeIndex,
		"agentEdited":     true,
	})); err != nil {
		return agentToolError("script.update_episode", args, err)
	}
	if err := tx.Commit(r.Context()); err != nil {
		return agentToolError("script.update_episode", args, err)
	}
	return agentToolOK("script.update_episode", args, "已覆盖剧本分集并标记下游需要重生成。", map[string]any{
		"episodeId":       item.ID,
		"scriptId":        item.ScriptID,
		"scriptVersionId": item.ScriptVersionID,
		"episodeIndex":    item.EpisodeIndex,
	})
}

func (s *Server) agentToolDeleteScript(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	scriptID := agentReferenceStringArg(args, "scriptId")
	if scriptID == "" {
		return agentToolError("script.delete", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "scriptId is required"))
	}
	script, err := s.script(r, project.ID, scriptID)
	if err != nil {
		return agentToolError("script.delete", args, err)
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return agentToolError("script.delete", args, err)
	}
	defer tx.Rollback(r.Context())
	rows, err := tx.Query(r.Context(), `
		SELECT id::text
		FROM script_versions
		WHERE project_id = $1 AND script_id = $2 AND COALESCE(status, 'active') <> 'archived'
	`, project.ID, script.ID)
	if err != nil {
		return agentToolError("script.delete", args, err)
	}
	versionIDs := make([]string, 0)
	for rows.Next() {
		var versionID string
		if err := rows.Scan(&versionID); err != nil {
			rows.Close()
			return agentToolError("script.delete", args, err)
		}
		versionIDs = append(versionIDs, versionID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return agentToolError("script.delete", args, err)
	}
	rows.Close()
	for _, versionID := range versionIDs {
		if err := markScriptVersionDownstreamStale(r.Context(), tx, project.ID, versionID); err != nil {
			return agentToolError("script.delete", args, err)
		}
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE script_scenes
		SET deleted_at = COALESCE(deleted_at, now()),
		    stale_state = 'needs_regeneration',
		    updated_at = now()
		WHERE project_id = $1 AND script_id = $2 AND deleted_at IS NULL
	`, project.ID, script.ID); err != nil {
		return agentToolError("script.delete", args, err)
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE script_versions
		SET status = 'archived'
		WHERE project_id = $1 AND script_id = $2 AND COALESCE(status, 'active') <> 'archived'
	`, project.ID, script.ID); err != nil {
		return agentToolError("script.delete", args, err)
	}
	tag, err := tx.Exec(r.Context(), `
		UPDATE scripts
		SET status = 'archived',
		    updated_at = now()
		WHERE project_id = $1 AND id = $2 AND COALESCE(status, 'active') <> 'archived'
	`, project.ID, script.ID)
	if err != nil {
		return agentToolError("script.delete", args, err)
	}
	if tag.RowsAffected() == 0 && script.Status == "archived" {
		return agentToolOK("script.delete", args, "剧本已归档。", map[string]any{"scriptId": script.ID, "mode": "archive"})
	}
	if err := production.MarkFinalVideoStale(r.Context(), tx, project.ID, ""); err != nil {
		return agentToolError("script.delete", args, err)
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "script.archived", "script", script.ID, mustRawJSON(map[string]any{
		"scriptId":       script.ID,
		"versionIds":     versionIDs,
		"archivedBy":     nullableMetadataValue(principal.UserID),
		"agentTaskId":    task.ID,
		"agentStepId":    step.ID,
		"reason":         agentStringArg(args, "reason"),
		"previousStatus": script.Status,
	})); err != nil {
		return agentToolError("script.delete", args, err)
	}
	if err := tx.Commit(r.Context()); err != nil {
		return agentToolError("script.delete", args, err)
	}
	return agentToolOK("script.delete", args, "已归档剧本及其版本。", map[string]any{
		"scriptId":     script.ID,
		"mode":         "archive",
		"versionCount": len(versionIDs),
	})
}

func agentStringFromMap(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(stringValueFromAny(value))
}

func boolValueFromMap(values map[string]any, key string) (bool, bool) {
	value, ok := values[key]
	if !ok {
		return false, false
	}
	return boolValueFromAny(value), true
}

func patchContains(values map[string]any, key string) bool {
	_, ok := values[key]
	return ok
}
