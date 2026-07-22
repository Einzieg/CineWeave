package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/production"
	sourceutil "github.com/Einzieg/cineweave/internal/sources"
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

func (s *Server) agentToolClearProjectProductionContent(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	if agentStringArg(args, "confirmation") != "preserve_novel_sources" {
		return agentToolError("project.clear_production_content", args, newAPIError(http.StatusUnprocessableEntity, "PROJECT_CLEAR_CONFIRMATION_REQUIRED", "必须明确确认保留小说原文后才能清空生产内容"))
	}
	result, err := s.clearProjectProductionContent(r, principal, project, task, step, agentStringArg(args, "reason"))
	if err != nil {
		return agentToolError("project.clear_production_content", args, err)
	}
	return agentToolOK("project.clear_production_content", args, "已保留小说原文和分卷分集，并清空其余生产内容。", map[string]any{
		"previousGenerationId":   result.PreviousGenerationID,
		"activeGenerationId":     result.ActiveGenerationID,
		"activeGenerationNo":     result.ActiveGenerationNo,
		"novelSourceCount":       result.NovelSourceCount,
		"novelChapterCount":      result.NovelChapterCount,
		"deletedSourceCount":     result.DeletedSourceCount,
		"deletedEventCount":      result.DeletedEventCount,
		"deletedPlanCount":       result.DeletedPlanCount,
		"deletedScriptCount":     result.DeletedScriptCount,
		"deletedAssetCount":      result.DeletedAssetCount,
		"deletedStoryboardCount": result.DeletedStoryboardCount,
		"deletedReviewCount":     result.DeletedReviewCount,
		"deletedExportCount":     result.DeletedExportCount,
	})
}

func (s *Server) clearProjectProductionContent(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, reason string) (clearProjectProductionContentResult, error) {
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return clearProjectProductionContentResult{}, err
	}
	defer tx.Rollback(r.Context())

	var activeWorkflowCount, activeProviderTaskCount int
	if err := tx.QueryRow(r.Context(), `
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

	previousGenerationID, generation, err := videoproduction.ResetActiveGeneration(r.Context(), tx, project.ID)
	if err != nil {
		return clearProjectProductionContentResult{}, err
	}
	result := clearProjectProductionContentResult{
		PreviousGenerationID: previousGenerationID,
		ActiveGenerationID:   generation.ID,
		ActiveGenerationNo:   generation.GenerationNo,
	}
	if err := tx.QueryRow(r.Context(), `
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
		tag, execErr := tx.Exec(r.Context(), statement, project.ID)
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
	if _, err := tx.Exec(r.Context(), `
		UPDATE novel_chapters chapter
		SET event_state = 'pending', event_summary = NULL, error_message = NULL, updated_at = now()
		FROM project_sources source
		WHERE source.id = chapter.source_id
		  AND source.project_id = $1
		  AND source.source_type = 'novel'
	`, project.ID); err != nil {
		return clearProjectProductionContentResult{}, err
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE project_sources
		SET metadata = COALESCE(metadata, '{}'::jsonb) - 'sourceChangedAt' - 'downstreamStaleAt' - 'changedFields',
		    updated_at = now()
		WHERE project_id = $1 AND source_type = 'novel'
	`, project.ID); err != nil {
		return clearProjectProductionContentResult{}, err
	}
	payload := mustRawJSON(map[string]any{
		"agentTaskId":          task.ID,
		"agentStepId":          step.ID,
		"clearedBy":            nullableMetadataValue(principal.UserID),
		"reason":               reason,
		"previousGenerationId": result.PreviousGenerationID,
		"activeGenerationId":   result.ActiveGenerationID,
		"counts":               result,
	})
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "project.production_content.cleared", "project", project.ID, payload); err != nil {
		return clearProjectProductionContentResult{}, err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return clearProjectProductionContentResult{}, err
	}
	return result, nil
}

func (s *Server) agentToolUpdateSource(r *http.Request, principal auth.Principal, project Project, args map[string]any) agentToolResult {
	sourceID := agentReferenceStringArg(args, "sourceId")
	if sourceID == "" {
		return agentToolError("source.update", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "sourceId is required"))
	}
	patch := agentMapArg(args, "patch")
	if len(patch) == 0 {
		return agentToolError("source.update", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "patch is required"))
	}
	current, err := s.projectSource(r, project.ID, sourceID)
	if err != nil {
		return agentToolError("source.update", args, err)
	}
	sourceType := firstNonEmpty(agentStringFromMap(patch, "sourceType"), current.SourceType)
	title := firstNonEmpty(agentStringFromMap(patch, "title"), current.Title)
	content := current.Content
	if value, exists := patch["content"]; exists {
		content = strings.TrimSpace(stringValueFromAny(value))
	}
	contentFormat := firstNonEmpty(agentStringFromMap(patch, "contentFormat"), current.ContentFormat)
	status := firstNonEmpty(agentStringFromMap(patch, "status"), current.Status)
	if !validSourceType(sourceType) || title == "" || content == "" || !validContentFormat(contentFormat) || !validSourceStatus(status) {
		return agentToolError("source.update", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "source fields are invalid"))
	}
	metadata := current.Metadata
	if raw, exists := patch["metadata"]; exists {
		metadata = mustRawJSON(agentObjectFromAny(raw))
	}
	splitChapters, splitSpecified := boolValueFromMap(patch, "splitChapters")
	chaptersChanged := sourceType == "novel" && patchContains(patch, "content") && (!splitSpecified || splitChapters)
	changedFields := sourceChangedFields(current, sourceType, content, contentFormat, chaptersChanged)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return agentToolError("source.update", args, err)
	}
	defer tx.Rollback(r.Context())
	item, err := scanProjectSource(tx.QueryRow(r.Context(), `
		UPDATE project_sources
		SET source_type = $3,
		    title = $4,
		    content = $5,
		    content_format = $6,
		    status = $7,
		    metadata = $8,
		    updated_at = now()
		WHERE id = $1 AND project_id = $2
		RETURNING id, organization_id, project_id, source_type, title, content, content_format,
		          original_file_name, storage_key, status, metadata, created_by, created_at, updated_at
	`, current.ID, project.ID, sourceType, title, content, contentFormat, status, metadata))
	if err != nil {
		return agentToolError("source.update", args, err)
	}
	if chaptersChanged {
		chapterDrafts := make([]sourceChapterRequest, 0)
		for _, draft := range sourceutil.SplitNovelChapters(content) {
			chapterDrafts = append(chapterDrafts, sourceChapterRequest{
				ChapterIndex: &draft.Index,
				VolumeIndex:  intPtrOrNil(draft.VolumeIndex),
				SectionIndex: intPtrOrNil(draft.SectionIndex),
				VolumeTitle:  stringPtrOrNil(draft.VolumeTitle),
				ChapterTitle: stringPtrOrNil(draft.Title),
				Content:      draft.Content,
			})
		}
		chapters, err := s.replaceSourceChapters(r, tx, project, item.ID, chapterDrafts)
		if err != nil {
			return agentToolError("source.update", args, err)
		}
		item.Chapters = chapters
	}
	if len(changedFields) > 0 {
		if err := s.markProjectSourceDownstreamStaleTx(r, tx, project, item.ID, changedFields, principal.UserID); err != nil {
			return agentToolError("source.update", args, err)
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		return agentToolError("source.update", args, err)
	}
	return agentToolOK("source.update", args, "已覆盖原文并同步下游状态。", map[string]any{
		"sourceId":      item.ID,
		"title":         item.Title,
		"status":        item.Status,
		"changedFields": changedFields,
		"chapterCount":  len(item.Chapters),
	})
}

func (s *Server) agentToolDeleteSource(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	sourceID := agentReferenceStringArg(args, "sourceId")
	if sourceID == "" {
		return agentToolError("source.delete", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "sourceId is required"))
	}
	source, err := s.projectSource(r, project.ID, sourceID)
	if err != nil {
		return agentToolError("source.delete", args, err)
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return agentToolError("source.delete", args, err)
	}
	defer tx.Rollback(r.Context())
	metadata := mustRawJSON(map[string]any{
		"archivedAt":  time.Now().UTC().Format(time.RFC3339),
		"archivedBy":  nullableMetadataValue(principal.UserID),
		"agentTaskId": task.ID,
		"agentStepId": step.ID,
		"reason":      agentStringArg(args, "reason"),
	})
	if _, err := tx.Exec(r.Context(), `
		UPDATE project_sources
		SET status = 'archived',
		    metadata = COALESCE(metadata, '{}'::jsonb) || $3::jsonb,
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
	`, project.ID, source.ID, metadata); err != nil {
		return agentToolError("source.delete", args, err)
	}
	if err := s.markProjectSourceDownstreamStaleTx(r, tx, project, source.ID, []string{"status"}, principal.UserID); err != nil {
		return agentToolError("source.delete", args, err)
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "source.archived", "project_source", source.ID, metadata); err != nil {
		return agentToolError("source.delete", args, err)
	}
	if err := tx.Commit(r.Context()); err != nil {
		return agentToolError("source.delete", args, err)
	}
	return agentToolOK("source.delete", args, "已归档原文。", map[string]any{"sourceId": source.ID, "mode": "archive"})
}

func (s *Server) agentToolDeleteSourceChapter(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	sourceID, err := s.resolveAgentChapterSourceID(r, project, args)
	if err != nil {
		return agentToolError("source.delete_chapter", args, err)
	}
	chapter, err := s.resolveAgentSourceChapter(r, project, sourceID, args)
	if err != nil {
		return agentToolError("source.delete_chapter", args, err)
	}
	result, err := s.deleteSourceChapterCore(r, project, sourceID, chapter.ID, principal.UserID, map[string]any{
		"agentTaskId": task.ID,
		"agentStepId": step.ID,
		"reason":      agentStringArg(args, "reason"),
	})
	if err != nil {
		return agentToolError("source.delete_chapter", args, err)
	}
	return agentToolOK("source.delete_chapter", args, "已删除原文章节并同步下游过期状态。", map[string]any{
		"deleted":               result.Deleted,
		"mode":                  result.Mode,
		"sourceId":              result.SourceID,
		"chapterId":             result.ChapterID,
		"deletedChapterIndex":   result.DeletedChapterIndex,
		"remainingChapterCount": result.RemainingChapterCount,
		"chapterTitle":          nullableMetadataValue(chapter.ChapterTitle),
		"volumeIndex":           nullableMetadataValue(chapter.VolumeIndex),
		"sectionIndex":          nullableMetadataValue(chapter.SectionIndex),
	})
}

func (s *Server) resolveAgentChapterSourceID(r *http.Request, project Project, args map[string]any) (string, error) {
	if sourceID := agentReferenceStringArg(args, "sourceId"); sourceID != "" {
		source, err := s.projectSource(r, project.ID, sourceID)
		if err != nil {
			return "", err
		}
		if source.SourceType != "novel" || source.Status == "archived" {
			return "", newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "source must be an active novel")
		}
		return source.ID, nil
	}

	sourceTitle := strings.TrimSpace(agentStringArg(args, "sourceTitle"))
	rows, err := s.db.Query(r.Context(), `
		SELECT id::text
		FROM project_sources
		WHERE project_id = $1
		  AND source_type = 'novel'
		  AND COALESCE(status, 'ready') <> 'archived'
		  AND ($2 = '' OR lower(btrim(title)) = lower($2))
		ORDER BY updated_at DESC, created_at DESC
		LIMIT 2
	`, project.ID, sourceTitle)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	items := make([]string, 0, 2)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		items = append(items, id)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", newAPIError(http.StatusNotFound, "SOURCE_NOT_FOUND", "没有找到匹配的小说原文")
	}
	if len(items) > 1 {
		return "", newAPIError(http.StatusUnprocessableEntity, "SOURCE_SELECTOR_AMBIGUOUS", "项目中有多个小说原文，请提供 sourceId 或准确 sourceTitle")
	}
	return items[0], nil
}

func (s *Server) resolveAgentSourceChapter(r *http.Request, project Project, sourceID string, args map[string]any) (NovelChapter, error) {
	if chapterID := agentReferenceStringArg(args, "chapterId"); chapterID != "" {
		return s.sourceChapter(r, project.ID, sourceID, chapterID)
	}
	chapterIndex, hasChapterIndex := agentPositiveIntSelector(args, "chapterIndex")
	volumeIndex, hasVolumeIndex := agentPositiveIntSelector(args, "volumeIndex")
	sectionIndex, hasSectionIndex := agentPositiveIntSelector(args, "sectionIndex")
	chapterTitle := strings.TrimSpace(agentStringArg(args, "chapterTitle"))
	if !hasChapterIndex && !hasVolumeIndex && !hasSectionIndex && chapterTitle == "" {
		return NovelChapter{}, newAPIError(http.StatusUnprocessableEntity, "CHAPTER_SELECTOR_REQUIRED", "请提供 chapterId、chapterIndex、卷/节序号或准确章节标题")
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, source_id, chapter_index, volume_index, section_index, volume_title, chapter_title, content,
		       event_state, event_summary, error_message, created_at, updated_at
		FROM novel_chapters
		WHERE project_id = $1
		  AND source_id = $2
		  AND ($3 = 0 OR chapter_index = $3)
		  AND ($4 = 0 OR volume_index = $4)
		  AND ($5 = 0 OR section_index = $5)
		  AND ($6 = '' OR lower(btrim(COALESCE(chapter_title, ''))) = lower($6))
		ORDER BY chapter_index ASC
		LIMIT 2
	`, project.ID, sourceID, chapterIndex, volumeIndex, sectionIndex, chapterTitle)
	if err != nil {
		return NovelChapter{}, err
	}
	defer rows.Close()
	items := make([]NovelChapter, 0, 2)
	for rows.Next() {
		item, err := scanNovelChapter(rows)
		if err != nil {
			return NovelChapter{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return NovelChapter{}, err
	}
	if len(items) == 0 {
		return NovelChapter{}, pgx.ErrNoRows
	}
	if len(items) > 1 {
		return NovelChapter{}, newAPIError(http.StatusUnprocessableEntity, "CHAPTER_SELECTOR_AMBIGUOUS", "章节条件匹配到多条记录，请补充 chapterId、chapterIndex 或卷/节序号")
	}
	return items[0], nil
}

func agentPositiveIntSelector(args map[string]any, key string) (int, bool) {
	if _, exists := args[key]; !exists {
		return 0, false
	}
	value := agentIntArg(args, key, 0, 0, 1000000)
	return value, value > 0
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
		          prompt_version_id, prompt_hash, provider_call_id, review_status, manual_override, stale_state,
		          metadata, created_by, edited_by, created_at, updated_at, edited_at
	`, project.ID, current.ID, title, content, contentFormat, reviewStatus, staleState, metadata, principal.UserID))
	if err != nil {
		return agentToolError("script.update_episode", args, err)
	}
	if err := rebuildScriptVersionContentFromEpisodesTx(r, tx, project.ID, item.ScriptVersionID); err != nil {
		return agentToolError("script.update_episode", args, err)
	}
	if err := markScriptVersionDownstreamStale(r, tx, project.ID, item.ScriptVersionID); err != nil {
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
		if err := markScriptVersionDownstreamStale(r, tx, project.ID, versionID); err != nil {
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

func (s *Server) agentToolDeleteCanonicalAsset(r *http.Request, principal auth.Principal, project Project, args map[string]any) agentToolResult {
	assetID := agentReferenceStringArg(args, "assetId")
	if assetID == "" {
		return agentToolError("asset.delete", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "assetId is required"))
	}
	asset, err := s.canonicalAsset(r, project.ID, assetID)
	if err != nil {
		return agentToolError("asset.delete", args, err)
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return agentToolError("asset.delete", args, err)
	}
	defer tx.Rollback(r.Context())
	metadata := mustRawJSON(map[string]any{
		"archivedAt": time.Now().UTC().Format(time.RFC3339),
		"archivedBy": nullableMetadataValue(principal.UserID),
		"reason":     agentStringArg(args, "reason"),
	})
	if _, err := tx.Exec(r.Context(), `
		UPDATE canonical_assets
		SET status = 'archived',
		    metadata = COALESCE(metadata, '{}'::jsonb) || $3::jsonb,
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
	`, project.ID, asset.ID, metadata); err != nil {
		return agentToolError("asset.delete", args, err)
	}
	if err := production.MarkAssetDownstreamStale(r.Context(), tx, project.ID, asset.ID); err != nil {
		return agentToolError("asset.delete", args, err)
	}
	if err := production.MarkFinalVideoStale(r.Context(), tx, project.ID, ""); err != nil {
		return agentToolError("asset.delete", args, err)
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "canonical_asset.archived", "canonical_asset", asset.ID, mustRawJSON(map[string]any{
		"assetId":     asset.ID,
		"assetType":   asset.AssetType,
		"archivedBy":  nullableMetadataValue(principal.UserID),
		"agentEdited": true,
	})); err != nil {
		return agentToolError("asset.delete", args, err)
	}
	if err := tx.Commit(r.Context()); err != nil {
		return agentToolError("asset.delete", args, err)
	}
	return agentToolOK("asset.delete", args, "已归档核心资产并标记下游需要重生成。", map[string]any{"assetId": asset.ID, "mode": "archive"})
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
